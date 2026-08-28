package chat

import (
	"context"
	"sync"
)

// Manager 是会话生命周期管理器：按「账号 -> 会话 -> Conversation」维度管理会话聚合的生命周期。
//
// 它取代了旧的 common/aihelper/manager 全局单例，改为由 bootstrap 显式构造并注入，
// 依赖 ModelFactory 端口创建模型、依赖 MessageSink 端口持久化消息。
type Manager struct {
	// factory 模型工厂端口，用于按类型创建模型。
	factory ModelFactory
	// sink 消息持久化端口，注入到每个新建会话聚合中。
	sink MessageSink
	// conversations 两级映射记录每个账号下的会话聚合实例。
	conversations map[string]map[string]*Conversation
	// mu 保护映射表的并发访问。
	mu sync.RWMutex
}

// NewManager 创建会话管理器。
func NewManager(factory ModelFactory, sink MessageSink) *Manager {
	return &Manager{
		factory:       factory,
		sink:          sink,
		conversations: make(map[string]map[string]*Conversation),
	}
}

// GetOrCreate 获取或创建指定账号会话的聚合实例。
func (m *Manager) GetOrCreate(ctx context.Context, accountNo, sessionID, modelType string, params map[string]any) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 先确保当前账号拥有独立的会话映射表。
	userConvs, exists := m.conversations[accountNo]
	if !exists {
		userConvs = make(map[string]*Conversation)
		m.conversations[accountNo] = userConvs
	}

	// 复用已存在的会话聚合，避免重复创建模型实例。
	if conv, ok := userConvs[sessionID]; ok {
		return conv, nil
	}

	// 通过工厂创建模型，再组装为会话聚合挂回映射表。
	model, err := m.factory.Create(ctx, modelType, params)
	if err != nil {
		return nil, err
	}
	conv := NewConversation(model, sessionID, m.sink)
	userConvs[sessionID] = conv
	return conv, nil
}

// Get 获取指定账号会话的聚合实例。
func (m *Manager) Get(accountNo, sessionID string) (*Conversation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userConvs, exists := m.conversations[accountNo]
	if !exists {
		return nil, false
	}
	conv, ok := userConvs[sessionID]
	return conv, ok
}

// ReplayMessages 将数据库中的历史消息回放到内存会话，用于启动预热或运行时懒加载。
//
// 回放语义：
//   - 每条消息均以 persist=false 追加，仅重建内存上下文，不触发 MessageSink，避免二次落库；
//   - 消息顺序由调用方保证（通常按 created_at、id 升序传入）；
//   - 若会话已在内存中则直接返回 nil（幂等，防止重复回放导致消息重复）。
//
// 参数说明：
//   - accountNo：消息归属的内部账号编号，用于会话映射与模型参数；
//   - sessionID：目标会话标识；
//   - modelType：创建 Conversation 时使用的模型类型（当前统一为 "auto"）；
//   - params：传给 ModelFactory 的附加参数（auto 需要 account_no）；
//   - msgs：待回放的历史消息列表，为空时直接返回。
func (m *Manager) ReplayMessages(
	ctx context.Context,
	accountNo, sessionID, modelType string,
	params map[string]any,
	msgs []Message,
) error {
	// 会话已存在说明此前已回放或已有对话，跳过以避免重复追加。
	if _, ok := m.Get(accountNo, sessionID); ok {
		return nil
	}
	if len(msgs) == 0 {
		return nil
	}

	// 创建会话聚合并绑定模型，随后逐条写入历史。
	conv, err := m.GetOrCreate(ctx, accountNo, sessionID, modelType, params)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		// 保留数据库中的稳定消息 ID，使摘要覆盖水位在冷加载后仍然可定位。
		conv.RestoreMessage(msg)
	}
	return nil
}
