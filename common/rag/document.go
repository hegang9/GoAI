package rag

import (
	"fmt"
	"os"

	"github.com/cloudwego/eino/schema"
)

// LoadDocuments 读取磁盘文件并转换为可索引的文档集合。
//
// 该函数承载“文档加载/存储”职责，与“向量索引”职责解耦：
// 索引器只负责把文档写入向量库，文档如何从磁盘读取、如何切块由这里决定。
//
// TODO: 后续可在此引入文本切块（chunking），将长文档按段落/token
// 拆分为多个带重叠窗口的文档块，以提升检索召回质量。当前简单处理为单个文档。
func LoadDocuments(filePath string) ([]*schema.Document, error) {
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
