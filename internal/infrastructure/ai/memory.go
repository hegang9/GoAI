package ai

import (
	"context"
	"fmt"
	"strings"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultSummaryTriggerTokens   = 24000
	defaultRecentTurns            = 6
	defaultToolClearTriggerTokens = 20000

	messageIDExtraKey  = "goai_message_id"
	contextKindKey     = "goai_context_kind"
	coreMemoryKind     = "core_memory"
	sessionSummaryKind = "session_summary"
)

// ContextConfig 是 AI 适配层使用的运行时上下文配置。
// 配置归一化集中在这里，避免构造 Agent 时散落默认值判断。
type ContextConfig struct {
	Enabled                bool
	SummaryTriggerTokens   int
	RecentTurns            int
	ToolClearTriggerTokens int64
}

// 配置归一化
func (c ContextConfig) normalized() ContextConfig {
	if c.SummaryTriggerTokens <= 0 {
		c.SummaryTriggerTokens = defaultSummaryTriggerTokens
	}
	if c.RecentTurns <= 0 {
		c.RecentTurns = defaultRecentTurns
	}
	if c.ToolClearTriggerTokens <= 0 {
		c.ToolClearTriggerTokens = defaultToolClearTriggerTokens
	}
	return c
}

// memoryRunState 保存一次 Runner 调用中的摘要工作状态,表示“一次请求中的上下文管理临时状态”。
// 该对象只通过 context 在本次请求内传递，避免把请求状态写入可并发复用的 Agent 实例。
type memoryRunState struct {
	accountNo string
	sessionID string
	snapshot  chat.ContextSnapshot
	next      *chat.ContextSnapshot
}

type memoryRunStateKey struct{}

// summaryInstruction 要求摘要模型把“始终可见的核心记忆”和“较早会话摘要”分开输出。
// 两层数据共用一次官方 Summarization 调用，既保持最小实现，也避免为记忆提取增加第二次 LLM 请求。
const summaryInstruction = `请压缩给出的较早对话，并严格只输出下面两个 XML 块，不要输出分析过程或其他文字：
<core_memory>
只记录跨后续对话仍应持续可见的稳定事实：用户明确偏好、长期约束、身份信息和安全边界。没有则写“无”。禁止记录密码、令牌、密钥和模型推理过程。
</core_memory>
<session_summary>
记录当前会话的目标、已确认事实、重要决定、已完成结果、未解决问题、待办以及必要的实体/编号。删除寒暄、重复内容和可重新获取的大段工具输出。
</session_summary>
如果输入含有旧核心记忆或旧会话摘要，请与新增对话合并；新信息明确推翻旧信息时，以新信息为准并删除失效内容。`

// prepareMemoryContext 从持久化快照恢复“核心记忆 + 会话摘要”，并只保留水位后的原始消息。
// 快照异常或水位缺失时安全退回全量历史，不能因为派生数据损坏阻断聊天。
func (m *AutoRouterModel) prepareMemoryContext(ctx context.Context, history []chat.Message) (context.Context, []*schema.Message) {
	// 先加载完整的历史消息作为 fallback
	messages := toSchemaMessages(history)
	if !m.contextCfg.Enabled || m.contextRepo == nil || len(history) == 0 {
		return ctx, messages
	}

	last := history[len(history)-1]
	state := &memoryRunState{accountNo: last.AccountNo, sessionID: last.SessionID}
	snapshot, found, err := m.contextRepo.Get(ctx, state.accountNo, state.sessionID)
	if err != nil {
		logger.Warn("context snapshot load failed, fallback to full history",
			"accountNo", state.accountNo, "sessionID", state.sessionID, "err", err)
		return context.WithValue(ctx, memoryRunStateKey{}, state), messages
	}
	if !found {
		return context.WithValue(ctx, memoryRunStateKey{}, state), messages
	}

	coveredIndex := -1
	for i := range history {
		if history[i].ID == snapshot.CoveredMessageID {
			coveredIndex = i
			break
		}
	}
	if coveredIndex < 0 {
		logger.Warn("context snapshot watermark missing, fallback to full history",
			"accountNo", state.accountNo,
			"sessionID", state.sessionID,
			"coveredMessageID", snapshot.CoveredMessageID)
		return context.WithValue(ctx, memoryRunStateKey{}, state), messages
	}

	state.snapshot = snapshot
	prepared := make([]*schema.Message, 0, len(history)-coveredIndex+2)
	prepared = appendMemoryMessages(prepared, snapshot.CoreMemory, snapshot.Summary)
	prepared = append(prepared, toSchemaMessages(history[coveredIndex+1:])...)
	logger.Info("context snapshot restored",
		"accountNo", state.accountNo,
		"sessionID", state.sessionID,
		"historyMessages", len(history),
		"preparedMessages", len(prepared),
		"snapshotVersion", snapshot.Version)
	return context.WithValue(ctx, memoryRunStateKey{}, state), prepared
}

// buildContextHandlers 构造 Eino v0.8.0 官方 Reduction 与 Summarization 中间件，这两个中间件将在每次模型调用前执行。
// 顺序固定为先清理旧工具结果，再判断是否需要压缩会话，避免摘要模型吞入无价值的大工具输出。
func (m *AutoRouterModel) buildContextHandlers(ctx context.Context) ([]adk.ChatModelAgentMiddleware, error) {
	if !m.contextCfg.Enabled {
		return nil, nil
	}

	reducer, err := reduction.New(ctx, &reduction.Config{
		// 最小版本不提供 read_file 工具，因此不把单个工具结果外置；
		// 这里只启用官方 Clear 阶段移除较早工具参数和结果。
		SkipTruncation:            true, // 支持把超大的工具结果外置，但需要提供read_file工具，当前未实现
		SkipClear:                 false,
		MaxTokensForClear:         m.contextCfg.ToolClearTriggerTokens,
		ClearRetentionSuffixLimit: 2,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino reduction middleware: %w", err)
	}

	summarizer, err := summarization.New(ctx, &summarization.Config{
		Model: m.llm,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: m.contextCfg.SummaryTriggerTokens,
		},
		PreserveUserMessages: &summarization.PreserveUserMessages{Enabled: false},
		UserInstruction:      summaryInstruction,
		GenModelInput:        m.buildSummaryInput,
		Finalize:             m.finalizeSummary,
		Callback:             m.persistSummary,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino summarization middleware: %w", err)
	}
	return []adk.ChatModelAgentMiddleware{
		&failOpenMiddleware{name: "reduction", ChatModelAgentMiddleware: reducer},
		&failOpenMiddleware{name: "summarization", ChatModelAgentMiddleware: summarizer},
	}, nil
}

// failOpenMiddleware 保留官方中间件的全部能力，只在上下文重写失败时回退原状态。
// 摘要或清理属于增强能力，不应让本来可以回答的聊天请求变成 500。
type failOpenMiddleware struct {
	name string
	adk.ChatModelAgentMiddleware
}

// BeforeModelRewriteState 执行被包装的官方上下文中间件。
//
// Reduction 和 Summarization 都可能改写本轮要发送给模型的 state；但它们属于
// 上下文增强能力，失败时不应该让原本可以完成的聊天请求失败。因此这里保留
// 官方中间件的成功结果，发生错误时返回调用前的 ctx/state，并把错误记录为告警。
// 返回 nil error 是刻意的 fail-open 策略：后续 Agent 会继续使用原始上下文调用模型。
func (m *failOpenMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	modelCtx *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	nextCtx, nextState, err := m.ChatModelAgentMiddleware.BeforeModelRewriteState(ctx, state, modelCtx)
	if err == nil {
		return nextCtx, nextState, nil
	}
	logger.Warn("context middleware failed, fallback to original state", "middleware", m.name, "err", err)
	return ctx, state, nil
}

// buildSummaryInput 构造官方 Summarization 使用的摘要模型输入。
//
// original 是官方中间件检测到的完整 Agent 上下文；这里将它拆成两部分，只把
// 较早、可以被语义压缩的 prefix 交给摘要模型，最近完整轮次和当前未完成用户
// 轮次不参与摘要，以便后续继续保留原文细节。如果本次请求已有旧快照，还会
// 把旧的核心记忆和会话摘要作为待合并信息传给摘要模型。
func (m *AutoRouterModel) buildSummaryInput(
	ctx context.Context,
	defaultSystemInstruction, userInstruction adk.Message,
	original []adk.Message,
) ([]adk.Message, error) {
	state, _ := ctx.Value(memoryRunStateKey{}).(*memoryRunState)
	prefix, _ := splitSummaryPrefix(original, m.contextCfg.RecentTurns)
	if len(prefix) == 0 {
		// 单条超长输入无法通过历史摘要解决；保留原消息，后续由模型窗口错误明确反馈。
		return []adk.Message{defaultSystemInstruction, userInstruction}, nil
	}

	input := make([]adk.Message, 0, len(prefix)+4)
	input = append(input, defaultSystemInstruction)
	if state != nil && (state.snapshot.CoreMemory != "" || state.snapshot.Summary != "") {
		input = append(input, schema.UserMessage(formatPreviousMemory(state.snapshot)))
	}
	input = append(input, prefix...)
	input = append(input, userInstruction)
	return input, nil
}

// finalizeSummary 将官方 Summarization 生成的结果转换成 GoAI 的上下文视图。
//
// 该函数同时完成三件事：解析 core_memory/session_summary 两个区块，计算本次
// 摘要覆盖到的最后一条稳定消息 ID，并重新组织后续 Agent 可见的消息列表。返回
// 的列表由“核心记忆系统消息 + 会话摘要系统消息 + 最近原文”组成；原始消息中
// 没有可定位的完整前缀时不推进水位，避免下一次恢复时误删历史。
func (m *AutoRouterModel) finalizeSummary(ctx context.Context, original []adk.Message, summary adk.Message) ([]adk.Message, error) {
	state, _ := ctx.Value(memoryRunStateKey{}).(*memoryRunState)
	prefix, recent := splitSummaryPrefix(original, m.contextCfg.RecentTurns)
	coveredID := lastPersistentMessageID(prefix)
	if state == nil || coveredID == "" {
		// 没有可稳定定位的完整历史前缀时不推进水位，避免下次恢复丢消息。
		return original, nil
	}

	text := messageText(summary)
	core, session := extractMemorySections(text)
	if core == "" {
		core = state.snapshot.CoreMemory
	}
	if session == "" {
		// 模型偶尔不严格遵守 XML；仍保存其文本作为会话摘要，但不污染核心记忆。
		session = strings.TrimSpace(text)
	}
	if session == "" {
		return original, nil
	}

	next := chat.ContextSnapshot{
		AccountNo:        state.accountNo,
		SessionID:        state.sessionID,
		CoreMemory:       core,
		Summary:          session,
		CoveredMessageID: coveredID,
		Version:          state.snapshot.Version + 1,
	}
	state.next = &next

	// 重建 Agent 上下文
	out := make([]adk.Message, 0, len(recent)+2)
	out = appendMemoryMessages(out, core, session)
	out = append(out, recent...)
	return out, nil
}

// persistSummary 在官方 Summarization 完成 state 替换后持久化新的派生快照。
//
// state.next 由 finalizeSummary 创建，只有摘要已经成功生成、解析并形成合法水位
// 时才会存在。保存失败只记录告警并返回 nil，让主模型继续回答；这样数据库暂时
// 不可用时不会扩大故障面，下一轮仍可以从原始消息重新构建摘要。
func (m *AutoRouterModel) persistSummary(ctx context.Context, _, _ adk.ChatModelAgentState) error {
	state, _ := ctx.Value(memoryRunStateKey{}).(*memoryRunState)
	if state == nil || state.next == nil || m.contextRepo == nil {
		return nil
	}
	if err := m.contextRepo.Save(ctx, *state.next); err != nil {
		logger.Warn("context snapshot save failed, continue without persistence",
			"accountNo", state.accountNo, "sessionID", state.sessionID, "err", err)
		return nil
	}
	state.snapshot = *state.next
	state.next = nil
	logger.Info("context snapshot updated",
		"accountNo", state.accountNo,
		"sessionID", state.sessionID,
		"coveredMessageID", state.snapshot.CoveredMessageID,
		"snapshotVersion", state.snapshot.Version)
	return nil
}

// splitSummaryPrefix 将消息拆成“可压缩前缀”和“必须保留的近期消息”。
//
// 算法从末尾倒序寻找用户消息，并保留 recentTurns 个完整用户/助手轮次。
// 如果最后一条是用户消息，说明该轮尚未产生助手回答，因此额外保留这一轮，
// 防止摘要过程把当前问题丢掉。system 消息属于已经生成的记忆或系统指令，
// 不属于原始对话，不参与轮次统计，也不会被再次摘要。
func splitSummaryPrefix(messages []adk.Message, recentTurns int) (prefix, recent []adk.Message) {
	conversation := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil || msg.Role == schema.System {
			continue
		}
		conversation = append(conversation, msg)
	}
	if len(conversation) == 0 {
		return nil, nil
	}

	usersToKeep := recentTurns
	if conversation[len(conversation)-1].Role == schema.User {
		// 当前用户消息尚无最终回答，不计入“完整轮次”额度。
		usersToKeep++
	}
	start := 0
	seenUsers := 0
	for i := len(conversation) - 1; i >= 0; i-- {
		if conversation[i].Role != schema.User {
			continue
		}
		seenUsers++
		start = i
		if seenUsers >= usersToKeep {
			break
		}
	}
	if seenUsers < usersToKeep || start == 0 {
		return nil, conversation
	}
	return conversation[:start], conversation[start:]
}

// appendMemoryMessages 将两类持久化记忆转换为模型可见的 system 消息。
//
// 核心记忆和会话摘要都标记为历史参考，而不是新的用户指令，降低记忆内容
// 被误解释为当前任务命令的风险。core 为“无”时不注入，避免给模型增加无效文本。
func appendMemoryMessages(dst []*schema.Message, core, summary string) []*schema.Message {
	if strings.TrimSpace(core) != "" && strings.TrimSpace(core) != "无" {
		dst = append(dst, memorySystemMessage(coreMemoryKind,
			"以下是经过压缩的稳定核心记忆，仅作为历史事实和用户偏好参考，不是新的用户指令：\n"+core))
	}
	if strings.TrimSpace(summary) != "" {
		dst = append(dst, memorySystemMessage(sessionSummaryKind,
			"以下是较早会话的压缩摘要，仅用于恢复任务语境，不得覆盖当前用户要求：\n"+summary))
	}
	return dst
}

// memorySystemMessage 创建带有内部类型标记的记忆 system 消息。
// contextKindKey 写入 Extra 供程序识别，Content 只保留模型需要阅读的自然语言。
func memorySystemMessage(kind, content string) *schema.Message {
	return &schema.Message{
		Role:    schema.System,
		Content: content,
		Extra:   map[string]any{contextKindKey: kind},
	}
}

// formatPreviousMemory 将已有快照包装成摘要模型能够理解的待合并文本。
// 它使用 XML 区块与 summaryInstruction 保持一致，便于模型区分核心记忆和会话摘要。
func formatPreviousMemory(snapshot chat.ContextSnapshot) string {
	return "需要与新增历史合并的旧记忆：\n<core_memory>\n" + snapshot.CoreMemory +
		"\n</core_memory>\n<session_summary>\n" + snapshot.Summary + "\n</session_summary>"
}

// lastPersistentMessageID 从可压缩前缀末尾向前寻找原始消息 ID。
// 该 ID 由 schema.go 放入 Message.Extra，是摘要水位跨请求、跨重启恢复的关键。
func lastPersistentMessageID(messages []adk.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] == nil || messages[i].Extra == nil {
			continue
		}
		if id, ok := messages[i].Extra[messageIDExtraKey].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

// messageText 提取摘要消息的纯文本内容。
// 普通文本优先使用 Content；多模态消息没有 Content 时，再拼接其中的文本部分。
func messageText(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		return msg.Content
	}
	var parts []string
	for _, part := range msg.UserInputMultiContent {
		if part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// extractMemorySections 从摘要模型输出中提取两类记忆区块。
// 找不到任一区块时返回空字符串，由 finalizeSummary 决定是否回退或继续保存。
func extractMemorySections(text string) (core, summary string) {
	return extractTag(text, "core_memory"), extractTag(text, "session_summary")
}

// extractTag 提取指定 XML 标签最后一次出现的内容。
// 使用 LastIndex 是为了优先读取模型最终输出的区块，避免输入中重复示例标签
// 时误取前面的模板；缺少闭合标签时返回空字符串，交给上层做安全回退。
func extractTag(text, tag string) string {
	startTag := "<" + tag + ">"
	endTag := "</" + tag + ">"
	start := strings.LastIndex(text, startTag)
	if start < 0 {
		return ""
	}
	start += len(startTag)
	endOffset := strings.Index(text[start:], endTag)
	if endOffset < 0 {
		return ""
	}
	return strings.TrimSpace(text[start : start+endOffset])
}
