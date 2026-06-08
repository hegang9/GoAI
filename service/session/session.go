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
	"net/http"

	"github.com/google/uuid"
)

var ctx = context.Background()

func buildAIHelperConfig(userName string) map[string]any {
	conf := config.GetConfig()
	return map[string]any{
		"apiKey":   conf.AIModelConfig.APIKey,
		"username": userName,
	}
}

func GetUserSessionsByUserName(userName string) ([]bo.SessionInfoBO, code.Code) {
	manager := aihelper.GetGlobalManager()
	sessions := manager.GetUserSessions(userName)

	result := make([]bo.SessionInfoBO, 0, len(sessions))
	for _, sess := range sessions {
		result = append(result, bo.SessionInfoBO{
			SessionID: sess,
			Title:     sess,
		})
	}

	return result, code.CodeSuccess
}

func CreateSessionAndSendMessage(userName, userQuestion, modelType string) (bo.AIResponseBO, code.Code) {
	newSession := &model.Session{
		ID:       uuid.New().String(),
		UserName: userName,
		Title:    userQuestion,
	}
	createdSession, err := dao.CreateSession(newSession)
	if err != nil {
		logger.Error("CreateSessionAndSendMessage CreateSession", "err", err)
		return bo.AIResponseBO{}, code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userName, createdSession.ID, modelType, buildAIHelperConfig(userName))
	if err != nil {
		logger.Error("CreateSessionAndSendMessage GetOrCreateAIHelper", "err", err)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	aiResponse, err_ := helper.GenerateResponse(userName, ctx, userQuestion)
	if err_ != nil {
		logger.Error("CreateSessionAndSendMessage GenerateResponse", "err", err_)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	return bo.AIResponseBO{SessionID: createdSession.ID, Content: aiResponse.Content}, code.CodeSuccess
}

func CreateStreamSessionOnly(userName, userQuestion string) (string, code.Code) {
	newSession := &model.Session{
		ID:       uuid.New().String(),
		UserName: userName,
		Title:    userQuestion,
	}
	createdSession, err := dao.CreateSession(newSession)
	if err != nil {
		logger.Error("CreateStreamSessionOnly CreateSession", "err", err)
		return "", code.CodeServerBusy
	}
	return createdSession.ID, code.CodeSuccess
}

func StreamMessageToExistingSession(userName, sessionID, userQuestion, modelType string, writer http.ResponseWriter) code.Code {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		logger.Error("StreamMessageToExistingSession: streaming unsupported")
		return code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userName, sessionID, modelType, buildAIHelperConfig(userName))
	if err != nil {
		logger.Error("StreamMessageToExistingSession GetOrCreateAIHelper", "err", err)
		return code.AIModelFail
	}

	cb := func(msg string) {
		logger.Debug("SSE sending chunk", "content", msg, "len", len(msg))
		_, err := writer.Write([]byte("data: " + msg + "\n\n"))
		if err != nil {
			logger.Error("SSE write error", "err", err)
			return
		}
		flusher.Flush()
	}

	_, err_ := helper.StreamResponse(userName, ctx, cb, userQuestion)
	if err_ != nil {
		logger.Error("StreamMessageToExistingSession StreamResponse", "err", err_)
		return code.AIModelFail
	}

	_, err = writer.Write([]byte("data: [DONE]\n\n"))
	if err != nil {
		logger.Error("StreamMessageToExistingSession write DONE", "err", err)
		return code.AIModelFail
	}
	flusher.Flush()

	return code.CodeSuccess
}

func ChatSend(userName, sessionID, userQuestion, modelType string) (bo.AIResponseBO, code.Code) {
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userName, sessionID, modelType, buildAIHelperConfig(userName))
	if err != nil {
		logger.Error("ChatSend GetOrCreateAIHelper", "err", err)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	aiResponse, err_ := helper.GenerateResponse(userName, ctx, userQuestion)
	if err_ != nil {
		logger.Error("ChatSend GenerateResponse", "err", err_)
		return bo.AIResponseBO{}, code.AIModelFail
	}

	return bo.AIResponseBO{Content: aiResponse.Content}, code.CodeSuccess
}

func GetChatHistory(userName, sessionID string) ([]bo.MessageBO, code.Code) {
	manager := aihelper.GetGlobalManager()
	helper, exists := manager.GetAIHelper(userName, sessionID)
	if !exists {
		return nil, code.CodeRecordNotFound
	}

	messages := helper.GetMessages()
	history := make([]bo.MessageBO, 0, len(messages))

	for i, msg := range messages {
		isUser := i%2 == 0
		history = append(history, bo.MessageBO{
			IsUser:  isUser,
			Content: msg.Content,
		})
	}

	return history, code.CodeSuccess
}

func ChatStreamSend(userName, sessionID, userQuestion, modelType string, writer http.ResponseWriter) code.Code {
	return StreamMessageToExistingSession(userName, sessionID, userQuestion, modelType, writer)
}
