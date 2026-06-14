package session

import (
	providerpkg "GopherAI/common/aihelper/provider"
	"GopherAI/common/logger"
	"GopherAI/common/rabbitmq"
	"GopherAI/mapper"
	"GopherAI/model"
	"context"
	"sync"
)

// AIHelper 表示单个会话绑定的 AI 助手实例。
type AIHelper struct {
	// model 表示当前会话使用的模型实现。
	model providerpkg.AIModel
	// messages 保存当前会话的消息历史。
	messages []*model.Message
	// mu 保护消息历史的并发读写。
	mu sync.RWMutex
	// SessionID 表示当前助手绑定的会话标识。
	SessionID string
	// saveFunc 表示消息持久化回调。
	saveFunc func(*model.Message) (*model.Message, error)
}

// NewAIHelper 创建新的 AI 助手实例。
func NewAIHelper(aiModel providerpkg.AIModel, sessionID string) *AIHelper {
	logger.Info("NewAIHelper success", "sessionID", sessionID, "modelType", aiModel.GetModelType())
	return &AIHelper{
		model:    aiModel,
		messages: make([]*model.Message, 0),
		saveFunc: func(msg *model.Message) (*model.Message, error) {
			data := rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.AccountNo, msg.IsUser)
			err := rabbitmq.RMQMessage.Publish(data)
			if err != nil {
				logger.Error("AIHelper default saveFunc publish failed", "sessionID", msg.SessionID, "err", err)
			}
			return msg, err
		},
		SessionID: sessionID,
	}
}

// AddMessage 添加消息到内存中并在需要时触发持久化。
func (a *AIHelper) AddMessage(content string, accountNo string, isUser bool, save bool) {
	userMsg := model.Message{
		SessionID: a.SessionID,
		Content:   content,
		AccountNo: accountNo,
		IsUser:    isUser,
	}
	// 这里统一加锁，避免流式和同步路径同时追加消息时产生竞态。
	a.mu.Lock()
	a.messages = append(a.messages, &userMsg)
	a.mu.Unlock()
	if save {
		if _, err := a.saveFunc(&userMsg); err != nil {
			logger.Error("AIHelper AddMessage save failed", "sessionID", a.SessionID, "err", err)
		}
	}
}

// SetSaveFunc 设置自定义消息持久化回调。
func (a *AIHelper) SetSaveFunc(saveFunc func(*model.Message) (*model.Message, error)) {
	a.saveFunc = saveFunc
}

// GetMessages 获取所有消息历史的拷贝。
func (a *AIHelper) GetMessages() []*model.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// GenerateResponse 生成同步响应。
func (a *AIHelper) GenerateResponse(accountNo string, ctx context.Context, userQuestion string) (*model.Message, error) {
	a.AddMessage(userQuestion, accountNo, true, true)

	a.mu.RLock()
	messages := mapper.ConvertToSchemaMessages(a.messages)
	a.mu.RUnlock()

	schemaMsg, err := a.model.GenerateResponse(ctx, messages)
	if err != nil {
		logger.Error("AIHelper GenerateResponse model failed", "sessionID", a.SessionID, "err", err)
		return nil, err
	}

	modelMsg := mapper.ConvertToModelMessage(a.SessionID, accountNo, schemaMsg)
	a.AddMessage(modelMsg.Content, accountNo, false, true)
	return modelMsg, nil
}

// StreamResponse 生成流式响应。
func (a *AIHelper) StreamResponse(accountNo string, ctx context.Context, cb providerpkg.StreamCallback, userQuestion string) (*model.Message, error) {
	a.AddMessage(userQuestion, accountNo, true, true)

	a.mu.RLock()
	messages := mapper.ConvertToSchemaMessages(a.messages)
	a.mu.RUnlock()

	content, err := a.model.StreamResponse(ctx, messages, cb)
	if err != nil {
		logger.Error("AIHelper StreamResponse model failed", "sessionID", a.SessionID, "err", err)
		return nil, err
	}
	modelMsg := &model.Message{
		SessionID: a.SessionID,
		AccountNo: accountNo,
		Content:   content,
		IsUser:    false,
	}
	a.AddMessage(modelMsg.Content, accountNo, false, true)
	return modelMsg, nil
}

// GetModelType 获取当前模型类型。
func (a *AIHelper) GetModelType() string {
	return a.model.GetModelType()
}
