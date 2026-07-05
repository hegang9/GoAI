package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"GopherAI/internal/domain/chat"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/internal/infrastructure/storage"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// rewriteHistoryWindow 改写检索 query 时回溯的最近消息条数上限。
const rewriteHistoryWindow = 6

// RAGModel 在普通对话外层叠加检索增强，实现 domain/chat.Model 端口。
type RAGModel struct {
	llm                einomodel.ToolCallingChatModel
	accountNo          string
	engine             *raginfra.Engine
	enableRewrite      bool // 是否在多轮对话中用 LLM 改写检索 query
	enableFilterIntent bool // 无显式过滤参数时是否用 LLM 从对话解析过滤意图
}

// NewRAGModel 创建 RAG 模型实例。
//
// apiKey 来自统一配置；enableRewrite 控制是否启用多轮 query 改写。
func NewRAGModel(ctx context.Context, accountNo, modelName, baseURL, apiKey string, enableRewrite, enableFilterIntent bool, engine *raginfra.Engine) (*RAGModel, error) {
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	})
	if err != nil {
		logger.Error("NewRAGModel failed", "accountNo", accountNo, "err", err)
		return nil, fmt.Errorf("create rag model failed: %v", err)
	}
	logger.Info("NewRAGModel success", "accountNo", accountNo, "model", modelName,
		"enableRewrite", enableRewrite, "enableFilterIntent", enableFilterIntent)
	return &RAGModel{llm: llm, accountNo: accountNo, engine: engine,
		enableRewrite: enableRewrite, enableFilterIntent: enableFilterIntent}, nil
}

// 编译期断言：RAGModel 必须满足领域模型端口。
var _ chat.Model = (*RAGModel)(nil)

// Generate 生成同步回复，并在可用时引入检索增强上下文。
func (o *RAGModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	messages := o.buildRAGMessages(ctx, toSchemaMessages(history))
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		logger.Error("RAGModel Generate failed", "accountNo", o.accountNo, "err", err)
		return "", fmt.Errorf("rag generate failed: %v", err)
	}
	return resp.Content, nil
}

// Stream 生成流式回复，并在可用时引入检索增强上下文。
func (o *RAGModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	messages := o.buildRAGMessages(ctx, toSchemaMessages(history))
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		logger.Error("RAGModel Stream start failed", "accountNo", o.accountNo, "err", err)
		return "", fmt.Errorf("rag stream failed: %v", err)
	}
	defer stream.Close()

	var full strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("RAGModel Stream recv failed", "accountNo", o.accountNo, "err", err)
			return "", fmt.Errorf("rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return full.String(), nil
}

// buildRAGMessages 在账号存在文档时检索相关上下文并替换最后一条用户消息。
//
// 处理流程（任一环节失败或无相关内容时回退到原始消息，保证对话仍可继续）：
//  1. 账号无文档 → 跳过检索（避免对空知识库做无谓检索，也不污染普通对话）；
//  2. 可选地用 LLM 把多轮追问改写为自包含检索 query；
//  3. 检索；若无通过阈值的相关内容（RAG 路由）→ 回退原始消息；
//  4. 用增强 prompt 替换最后一条用户消息。
func (o *RAGModel) buildRAGMessages(ctx context.Context, messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}

	// 账号无任何文档时直接走普通对话。
	if !storage.HasUserDocs(o.accountNo) {
		logger.Info("RAGModel skip: no docs", "accountNo", o.accountNo)
		return messages
	}

	query := messages[len(messages)-1].Content
	if o.enableRewrite && len(messages) > 1 {
		query = o.rewriteQuery(ctx, messages)
	}

	// 过滤意图来源：① ctx 携带的显式过滤参数（由 controller/service 传入）优先；
	// ② 显式为空且开启意图解析时，用 LLM 从对话解析兜底；③ 两者都空则不过滤。
	// 领域 RAGFilter 在此转换为 infrastructure 的 RetrieveFilter，保持分层解耦。
	filter := chat.RetrieveFilterFromCtx(ctx)
	if filter.IsEmpty() && o.enableFilterIntent {
		filter = o.parseFilterIntent(ctx, query)
	}
	engineFilter := raginfra.RetrieveFilter{StoredName: filter.StoredName, Headers: filter.Headers}

	prompt, hasContext, err := o.engine.Retrieve(ctx, o.accountNo, query, engineFilter)
	if err != nil {
		logger.Warn("RAGModel retrieve failed", "accountNo", o.accountNo, "err", err)
		return messages
	}
	if !hasContext {
		// 无相关内容：不注入参考文档，避免污染普通对话。
		return messages
	}

	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{Role: schema.User, Content: prompt}
	return ragMessages
}

// rewriteQuery 结合最近若干轮对话历史，用 LLM 把最后一条追问改写为自包含检索 query。
// 改写失败时回退到最后一条用户消息原文，保证检索仍可进行。
func (o *RAGModel) rewriteQuery(ctx context.Context, messages []*schema.Message) string {
	original := messages[len(messages)-1].Content

	// 取最近窗口内的历史，拼成可读上下文。
	start := len(messages) - rewriteHistoryWindow
	if start < 0 {
		start = 0
	}
	var history strings.Builder
	for _, m := range messages[start:] {
		role := "用户"
		if m.Role == schema.Assistant {
			role = "助手"
		}
		history.WriteString(fmt.Sprintf("%s：%s\n", role, m.Content))
	}

	rewritePrompt := fmt.Sprintf(`你是检索 query 改写器。请根据下面的多轮对话历史，把用户的最后一句改写成一个语义自包含、可独立用于文档检索的查询。
要求：只输出改写后的查询本身，不要解释，不要加引号。若最后一句已自包含，则原样输出。

对话历史：
%s
改写后的检索查询：`, history.String())

	resp, err := o.llm.Generate(ctx, []*schema.Message{
		{Role: schema.User, Content: rewritePrompt},
	})
	if err != nil {
		logger.Warn("RAGModel rewriteQuery failed, fallback to original", "accountNo", o.accountNo, "err", err)
		return original
	}
	rewritten := strings.TrimSpace(resp.Content)
	if rewritten == "" {
		return original
	}
	logger.Info("RAGModel rewriteQuery", "accountNo", o.accountNo, "original", original, "rewritten", rewritten)
	return rewritten
}


// parseFilterIntent 用 LLM 从用户查询中解析检索过滤意图（来源文档名 / 章节关键字）。
//
// 用于显式过滤参数缺失时的兜底：用户在自然语言中可能指明"基于 report.docx 回答"，
// LLM 提取为结构化 {storedName, headers}。解析失败或 LLM 不可用时返回零值（不过滤），
// 保证不中断 RAG 链路。输出要求严格 JSON，避免污染查询语法。
func (o *RAGModel) parseFilterIntent(ctx context.Context, query string) chat.RAGFilter {
	intentPrompt := `你是检索过滤意图解析器。从用户的查询中提取"来源文档名"和"章节关键字"两个过滤意图。
规则：
- 只输出一个 JSON 对象，不要解释，不要加 markdown 代码块标记。
- 字段：storedName（来源文档文件名，含扩展名，如 report.md；无则空串）、headers（章节路径关键字，如"安装指南"；无则空串）。
- 若用户未指明来源或章节，两个字段都输出空串。
- 文档名只在实际被点名时填写，不要臆测。

用户查询：%s

输出 JSON：`

	resp, err := o.llm.Generate(ctx, []*schema.Message{
		{Role: schema.User, Content: fmt.Sprintf(intentPrompt, query)},
	})
	if err != nil {
		logger.Warn("RAGModel parseFilterIntent failed, fallback to no filter", "accountNo", o.accountNo, "err", err)
		return chat.RAGFilter{}
	}

	// 容错解析：LLM 可能输出多余文本，提取首个 { 到 } 的子串作为 JSON。
	body := strings.TrimSpace(resp.Content)
	lo := strings.Index(body, "{")
	hi := strings.LastIndex(body, "}")
	if lo < 0 || hi <= lo {
		logger.Warn("RAGModel parseFilterIntent no JSON found", "accountNo", o.accountNo, "raw", body)
		return chat.RAGFilter{}
	}
	var parsed struct {
		StoredName string `json:"storedName"`
		Headers    string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(body[lo:hi+1]), &parsed); err != nil {
		logger.Warn("RAGModel parseFilterIntent unmarshal failed", "accountNo", o.accountNo, "err", err)
		return chat.RAGFilter{}
	}
	logger.Info("RAGModel parseFilterIntent", "accountNo", o.accountNo,
		"query", query, "storedName", parsed.StoredName, "headers", parsed.Headers)
	return chat.RAGFilter{StoredName: parsed.StoredName, Headers: parsed.Headers}
}

// Type 返回模型类型标识。
func (o *RAGModel) Type() string { return "2" }
