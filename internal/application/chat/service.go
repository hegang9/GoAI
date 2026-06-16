// Package chat 是会话应用服务：编排会话创建、消息收发、流式对话与历史查询等用例。
//
// 它依赖领域会话管理器（chat.Manager）与会话仓储端口（chat.SessionRepository），
// 由 bootstrap 注入，自身不感知模型 SDK 与持久化细节。
package chat

import (
	"context"

	domainchat "GopherAI/internal/domain/chat"
	"GopherAI/pkg/code"
	"GopherAI/pkg/id"
	"GopherAI/pkg/logger"
)

// SessionView 会话列表项的应用层视图。
type SessionView struct {
	SessionID string
	Title     string
}

// AIResult AI 回复的应用层结果。
type AIResult struct {
	SessionID string
	Content   string
}

// MessageView 历史消息的应用层视图。
type MessageView struct {
	IsUser  bool
	Content string
}

// Service 会话应用服务。
type Service struct {
	manager     *domainchat.Manager
	sessionRepo domainchat.SessionRepository
}

// NewService 创建会话应用服务。
func NewService(manager *domainchat.Manager, sessionRepo domainchat.SessionRepository) *Service {
	return &Service{manager: manager, sessionRepo: sessionRepo}
}

// modelParams 构建创建模型所需的参数。当前仅需账号编号（RAG/MCP 依赖）。
func modelParams(accountNo string) map[string]any {
	return map[string]any{"account_no": accountNo}
}

// GetUserSessions 获取指定账号已持久化的会话列表。
func (s *Service) GetUserSessions(ctx context.Context, accountNo string) ([]SessionView, code.Code) {
	sessions, err := s.sessionRepo.ListByAccount(ctx, accountNo)
	if err != nil {
		logger.Error("GetUserSessions ListByAccount failed", "accountNo", accountNo, "err", err)
		return nil, code.CodeServerBusy
	}
	result := make([]SessionView, 0, len(sessions))
	for _, sess := range sessions {
		result = append(result, SessionView{SessionID: sess.ID, Title: sess.Title})
	}
	logger.Info("GetUserSessions success", "accountNo", accountNo, "count", len(result))
	return result, code.CodeSuccess
}

// CreateSessionAndSend 创建新会话并发送首条消息，返回 AI 回复与会话 ID。
func (s *Service) CreateSessionAndSend(ctx context.Context, accountNo, question, modelType string) (AIResult, code.Code) {
	session, errCode := s.createSession(ctx, accountNo, question)
	if errCode != code.CodeSuccess {
		return AIResult{}, errCode
	}

	conv, err := s.manager.GetOrCreate(ctx, accountNo, session.ID, modelType, modelParams(accountNo))
	if err != nil {
		logger.Error("CreateSessionAndSend GetOrCreate failed", "err", err)
		return AIResult{}, code.AIModelFail
	}

	content, err := conv.Generate(ctx, accountNo, question)
	if err != nil {
		logger.Error("CreateSessionAndSend Generate failed", "err", err)
		return AIResult{}, code.AIModelFail
	}
	return AIResult{SessionID: session.ID, Content: content}, code.CodeSuccess
}

// CreateStreamSession 为流式对话预创建会话并返回新会话 ID。
func (s *Service) CreateStreamSession(ctx context.Context, accountNo, question string) (string, code.Code) {
	session, errCode := s.createSession(ctx, accountNo, question)
	if errCode != code.CodeSuccess {
		return "", errCode
	}
	return session.ID, code.CodeSuccess
}

// StreamToSession 向已有会话发送消息并以流式方式产出 AI 回复。
func (s *Service) StreamToSession(ctx context.Context, accountNo, sessionID, question, modelType string, onChunk func(chunk string)) code.Code {
	conv, err := s.manager.GetOrCreate(ctx, accountNo, sessionID, modelType, modelParams(accountNo))
	if err != nil {
		logger.Error("StreamToSession GetOrCreate failed", "err", err)
		return code.AIModelFail
	}
	if _, err := conv.Stream(ctx, accountNo, question, onChunk); err != nil {
		logger.Error("StreamToSession Stream failed", "err", err)
		return code.AIModelFail
	}
	return code.CodeSuccess
}

// ChatSend 向已有会话发送单轮消息并返回完整 AI 回复。
func (s *Service) ChatSend(ctx context.Context, accountNo, sessionID, question, modelType string) (AIResult, code.Code) {
	conv, err := s.manager.GetOrCreate(ctx, accountNo, sessionID, modelType, modelParams(accountNo))
	if err != nil {
		logger.Error("ChatSend GetOrCreate failed", "err", err)
		return AIResult{}, code.AIModelFail
	}
	content, err := conv.Generate(ctx, accountNo, question)
	if err != nil {
		logger.Error("ChatSend Generate failed", "err", err)
		return AIResult{}, code.AIModelFail
	}
	return AIResult{Content: content}, code.CodeSuccess
}

// GetChatHistory 获取指定会话当前维护的聊天历史。
func (s *Service) GetChatHistory(ctx context.Context, accountNo, sessionID string) ([]MessageView, code.Code) {
	conv, exists := s.manager.Get(accountNo, sessionID)
	if !exists {
		return nil, code.CodeRecordNotFound
	}
	messages := conv.Messages()
	history := make([]MessageView, 0, len(messages))
	for _, msg := range messages {
		history = append(history, MessageView{IsUser: msg.IsUser, Content: msg.Content})
	}
	return history, code.CodeSuccess
}

// createSession 创建一条会话记录，默认以首条用户问题作为标题。
func (s *Service) createSession(ctx context.Context, accountNo, question string) (domainchat.Session, code.Code) {
	created, err := s.sessionRepo.Create(ctx, domainchat.Session{
		ID:        id.GenerateUUID(),
		AccountNo: accountNo,
		Title:     question,
	})
	if err != nil {
		logger.Error("createSession failed", "accountNo", accountNo, "err", err)
		return domainchat.Session{}, code.CodeServerBusy
	}
	return created, code.CodeSuccess
}
