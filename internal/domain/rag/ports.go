// Package rag 是检索增强（RAG）领域：定义向量索引相关的端口接口。
//
// 该包只声明能力契约，不依赖任何外层（Redis / eino / 文件系统等），
// 具体实现位于 infrastructure/rag。
package rag

import "context"

// Indexer 定义“文档向量索引”端口：建立与删除某个文档的向量索引。
//
// storedName 为文档在存储中的唯一文件名（账号隔离后唯一），localPath 为其磁盘路径。
type Indexer interface {
	// Index 读取并向量化指定文档，写入向量库。
	Index(ctx context.Context, storedName, localPath string) error
	// Delete 删除指定文档对应的向量索引。
	Delete(ctx context.Context, storedName string) error
}
