package chat

import "context"

// ContextSnapshot 是单个会话的派生记忆快照，即“有界上下文视图”。
//
// 原始消息始终保存在 messages 表中；本快照只保存为了模型调用而生成的有界视图，
// 因此可以在摘要损坏、策略升级或模型更换后从原始消息重新构建。
type ContextSnapshot struct {
	AccountNo string
	SessionID string
	// 会话级长期记忆,需要在后续对话中持续可见的稳定信息，例如用户偏好、长期约束等
	CoreMemory string
	// 保存较早对话的压缩结果
	Summary string
	// 表示 CoreMemory + Summary 已经覆盖到哪一条原始消息
	CoveredMessageID string
	// 快照版本，用于日志观测，为后续多实例CAS乐观锁预留基础
	Version uint64
}

// ContextRepository 定义会话摘要与核心记忆的持久化端口。
// 基础设施层负责具体数据库实现，AI 适配层只依赖该领域接口。
type ContextRepository interface {
	// Get 按账号和会话读取快照。found=false 表示尚未产生过摘要，并非错误。
	Get(ctx context.Context, accountNo, sessionID string) (snapshot ContextSnapshot, found bool, err error)
	// Save 保存最新快照。最小版本先采用按会话覆盖并递增版本的语义；
	// 后续多实例部署可以在不改变调用方的前提下升级为 CAS。
	Save(ctx context.Context, snapshot ContextSnapshot) error
}
