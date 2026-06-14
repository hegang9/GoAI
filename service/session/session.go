package session

import (
	"GopherAI/bo"
	"GopherAI/common/aihelper"
	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/config"
	"GopherAI/dao"
	"GopherAI/model"
	"context"

	"github.com/google/uuid"
)

var ctx = context.Background()

func buildAIHelperConfig(accountNo string) map[string]any {
	conf := config.GetConfig()
	return map[string]any{
		"apiKey":     conf.AIModelConfig.APIKey,
		"account_no": accountNo,
	}
}

func GetUserSessionsByAccountNo(accountNo string) ([]bo.SessionInfoBO, code.Code) {
	manager := aihelper.GetGlobalManager()
	sessions := manager.GetUserSessions(accountNo)

	result := make([]bo.SessionInfoBO, 0, len(sessions))
	for _, sess := range sessions {
		result = append(result, bo.SessionInfoBO{
			SessionID: sess,
			Title:     sess,
		})
	}

	return result, code.CodeSuccess
}

func CreateSessionAndSendMessage(accountNo, userQuestion, modelType string) (bo.AIResponseBO, code.Code) {
	newSession := &model.Session{
		ID:        uuid.New().String(),
		AccountNo: accountNo,
		Title:     userQuestion,
	}
	createdSession, err := dao.CreateSession(newSession)
	if err != nil {
		logger.Error("CreateSessionAndSendMessage CreateSession", "err", err)
		return bo.AIResponseBO{}, code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(accountNo, createdSession.ID, modelType, buildAIHelperConfig(accountNo))
	if err != nil {
		logger.Error("CreateSessionAndSendMessage GetOrCreateAIHelper", "err", err)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	aiResponse, err_ := helper.GenerateResponse(accountNo, ctx, userQuestion)
	if err_ != nil {
		logger.Error("CreateSessionAndSendMessage GenerateResponse", "err", err_)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	return bo.AIResponseBO{SessionID: createdSession.ID, Content: aiResponse.Content}, code.CodeSuccess
}

func CreateStreamSessionOnly(accountNo, userQuestion string) (string, code.Code) {
	newSession := &model.Session{
		ID:        uuid.New().String(),
		AccountNo: accountNo,
		Title:     userQuestion,
	}
	createdSession, err := dao.CreateSession(newSession)
	if err != nil {
		logger.Error("CreateStreamSessionOnly CreateSession", "err", err)
		return "", code.CodeServerBusy
	}
	return createdSession.ID, code.CodeSuccess
}

// StreamMessageToExistingSession 向已存在的会话发送消息并以流式方式产出 AI 回复。
// onChunk 为内容分片回调，由调用方（controller / streaming adapter）决定如何编码与传输；
// service 层只负责驱动 AI 流式生成，不再依赖任何 HTTP 传输细节。
func StreamMessageToExistingSession(accountNo, sessionID, userQuestion, modelType string, onChunk func(chunk string)) code.Code {
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(accountNo, sessionID, modelType, buildAIHelperConfig(accountNo))
	if err != nil {
		logger.Error("StreamMessageToExistingSession GetOrCreateAIHelper", "err", err)
		return code.AIModelFail
	}

	if _, err := helper.StreamResponse(accountNo, ctx, onChunk, userQuestion); err != nil {
		logger.Error("StreamMessageToExistingSession StreamResponse", "err", err)
		return code.AIModelFail
	}

	return code.CodeSuccess
}

func ChatSend(accountNo, sessionID, userQuestion, modelType string) (bo.AIResponseBO, code.Code) {
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(accountNo, sessionID, modelType, buildAIHelperConfig(accountNo))
	if err != nil {
		logger.Error("ChatSend GetOrCreateAIHelper", "err", err)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	aiResponse, err_ := helper.GenerateResponse(accountNo, ctx, userQuestion)
	if err_ != nil {
		logger.Error("ChatSend GenerateResponse", "err", err_)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	return bo.AIResponseBO{Content: aiResponse.Content}, code.CodeSuccess
}

func GetChatHistory(accountNo, sessionID string) ([]bo.MessageBO, code.Code) {
	manager := aihelper.GetGlobalManager()
	helper, exists := manager.GetAIHelper(accountNo, sessionID)
	if !exists {
		return nil, code.CodeRecordNotFound
	}

	messages := helper.GetMessages()
	history := make([]bo.MessageBO, 0, len(messages))

	// 直接读取持久化的 IsUser 字段，而非用下标奇偶推断角色。
	// 历史消息在内存中始终携带真实来源（用户/AI），下标推断在消息缺失或乱序时会错乱。
	for _, msg := range messages {
		history = append(history, bo.MessageBO{
			IsUser:  msg.IsUser,
			Content: msg.Content,
		})
	}

	return history, code.CodeSuccess
}

// ChatStreamSend 在已有会话上发起流式对话，是 StreamMessageToExistingSession 的语义化别名。
func ChatStreamSend(accountNo, sessionID, userQuestion, modelType string, onChunk func(chunk string)) code.Code {
	return StreamMessageToExistingSession(accountNo, sessionID, userQuestion, modelType, onChunk)
}
