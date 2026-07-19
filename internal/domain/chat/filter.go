package chat

import "context"

// RAGFilter 描述 RAG 检索期的元数据过滤范围，用于把检索限定在特定文档或章节内。
//
// 它定义在领域层，不依赖 infrastructure/rag 的具体类型；检索增强路径
// （infrastructure/ai 的 RetrievalModifier）负责把它转换为 engine 实际使用的 RetrieveFilter。
// 两个字段可同时设置（AND 关系），也可都为空（不过滤，行为与无过滤时一致）。
type RAGFilter struct {
	// StoredName 限定只检索某个来源文档；为空时不限来源。
	StoredName string
	// Headers 限定章节路径关键字；为空时不限章节。
	Headers string
}

// IsEmpty 判断是否为"不过滤"。为零值时调用方可据此跳过过滤逻辑。
func (f RAGFilter) IsEmpty() bool {
	return f.StoredName == "" && f.Headers == ""
}

// filterCtxKey 是 context value 的键类型，用未导出结构体避免与其他包冲突。
type filterCtxKey struct{}

// WithRetrieveFilter 把 RAGFilter 携带进 ctx，返回派生的新 context。
// 供 Conversation.Generate/Stream 在调用 model 前，把过滤意图塞进 ctx，
// 由检索增强路径从 ctx 取出，避免修改通用 Model 端口签名。
func WithRetrieveFilter(ctx context.Context, f RAGFilter) context.Context {
	return context.WithValue(ctx, filterCtxKey{}, f)
}

// RetrieveFilterFromCtx 从 ctx 取出 RAGFilter；不存在或类型不符时返回零值（不过滤）。
func RetrieveFilterFromCtx(ctx context.Context) RAGFilter {
	f, _ := ctx.Value(filterCtxKey{}).(RAGFilter)
	return f
}
