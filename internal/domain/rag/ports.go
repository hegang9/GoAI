// Package rag 是检索增强（RAG）领域：定义向量索引相关的端口接口。
//
// 该包只声明能力契约，不依赖任何外层（Redis / eino / 文件系统等），
// 具体实现位于 infrastructure/rag。
package rag

import "context"

// Indexer 定义“文档向量索引”端口：建立与删除某个文档的向量索引。
//
// 多文档知识库语义：索引按账号（accountNo）聚合，同一账号可索引多个文档；
// storedName 为文档在存储中的唯一文件名（账号隔离后唯一），localPath 为其磁盘路径。
type Indexer interface {
	// Index 读取并向量化指定文档，写入该账号的向量库。
	Index(ctx context.Context, accountNo, storedName, localPath string) error
	// Delete 删除指定账号下某个文档对应的向量数据。
	Delete(ctx context.Context, accountNo, storedName string) error
}
