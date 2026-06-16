// Package rag 是检索增强适配层：基于 eino + Redis 向量库实现 domain/rag.Indexer 端口，
// 并向 infrastructure/ai 提供检索与提示词构造能力。
package rag

import (
	"fmt"
	"os"

	"github.com/cloudwego/eino/schema"
)

// loadDocuments 读取磁盘文件并转换为可索引的文档集合。
//
// TODO: 后续可在此引入文本切块（chunking），按段落/token 拆分以提升召回质量。
func loadDocuments(filePath string) ([]*schema.Document, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	doc := &schema.Document{
		ID:      "doc_1",
		Content: string(content),
		MetaData: map[string]any{
			"source": filePath,
		},
	}
	return []*schema.Document{doc}, nil
}

// buildPrompt 将检索到的文档拼装为带上下文的提示词。
// 无检索结果时直接返回原始查询。
func buildPrompt(query string, docs []*schema.Document) string {
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
