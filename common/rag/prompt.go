package rag

import (
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// BuildRAGPrompt 承载“prompt 构造”职责：
// 将检索到的文档拼装为带上下文的提示词，供下游 LLM 生成更准确的回答。
// 若没有检索到任何文档，则直接返回原始查询，由模型按通用知识回答。
func BuildRAGPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}

	contextText := ""
	for i, doc := range docs {
		contextText += fmt.Sprintf("[文档 %d]: %s\n\n", i+1, doc.Content)
	}

	return fmt.Sprintf(`基于以下参考文档回答用户的问题。如果文档中没有相关信息，请说明无法找到相关信息。

参考文档：
%s

用户问题：%s

请提供准确、完整的回答：`, contextText, query)
}
