package manager

import (
	factorypkg "GopherAI/common/aihelper/factory"
	sessionpkg "GopherAI/common/aihelper/session"
	"GopherAI/common/logger"
	"context"
	"sync"
)

var ctx = context.Background()

// AIHelperManager 管理用户到会话助手的映射关系。
type AIHelperManager struct {
	// helpers 记录每个用户下的会话助手实例。
	helpers map[string]map[string]*sessionpkg.AIHelper
	// mu 保护映射表的并发访问。
	mu sync.RWMutex
}

// NewAIHelperManager 创建新的管理器实例。
func NewAIHelperManager() *AIHelperManager {
	logger.Info("NewAIHelperManager success")
	return &AIHelperManager{helpers: make(map[string]map[string]*sessionpkg.AIHelper)}
}

// GetOrCreateAIHelper 获取或创建指定用户会话的助手实例。
func (m *AIHelperManager) GetOrCreateAIHelper(accountNo string, sessionID string, modelType string, config map[string]interface{}) (*sessionpkg.AIHelper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[accountNo]
	if !exists {
		userHelpers = make(map[string]*sessionpkg.AIHelper)
		m.helpers[accountNo] = userHelpers
	}

	helper, exists := userHelpers[sessionID]
	if exists {
		return helper, nil
	}

	factory := factorypkg.GetGlobalFactory()
	helper, err := factory.CreateAIHelper(ctx, modelType, sessionID, config)
	if err != nil {
		logger.Error("AIHelperManager GetOrCreateAIHelper create failed", "accountNo", accountNo, "sessionID", sessionID, "err", err)
		return nil, err
	}
	userHelpers[sessionID] = helper
	return helper, nil
}

// GetAIHelper 获取指定用户会话的助手实例。
func (m *AIHelperManager) GetAIHelper(accountNo string, sessionID string) (*sessionpkg.AIHelper, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[accountNo]
	if !exists {
		return nil, false
	}
	helper, exists := userHelpers[sessionID]
	return helper, exists
}

// RemoveAIHelper 删除指定用户会话的助手实例。
func (m *AIHelperManager) RemoveAIHelper(accountNo string, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[accountNo]
	if !exists {
		return
	}
	delete(userHelpers, sessionID)
	if len(userHelpers) == 0 {
		delete(m.helpers, accountNo)
	}
	logger.Info("AIHelperManager RemoveAIHelper success", "accountNo", accountNo, "sessionID", sessionID)
}

// GetUserSessions 获取指定用户的所有会话 ID。
func (m *AIHelperManager) GetUserSessions(accountNo string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[accountNo]
	if !exists {
		return []string{}
	}

	sessionIDs := make([]string, 0, len(userHelpers))
	for sessionID := range userHelpers {
		sessionIDs = append(sessionIDs, sessionID)
	}
	return sessionIDs
}

var globalManager *AIHelperManager
var once sync.Once

// GetGlobalManager 获取全局管理器实例。
func GetGlobalManager() *AIHelperManager {
	once.Do(func() {
		globalManager = NewAIHelperManager()
	})
	return globalManager
}
