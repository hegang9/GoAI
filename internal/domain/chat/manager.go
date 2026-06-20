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
