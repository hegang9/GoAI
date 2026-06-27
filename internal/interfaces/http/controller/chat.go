package controller

import (
	chatapp "GopherAI/internal/application/chat"
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"
	"GopherAI/internal/interfaces/http/sse"
	"GopherAI/pkg/code"

	"github.com/gin-gonic/gin"
)

// GetUserSessions 返回当前登录账号的会话列表。
func (h *Handlers) GetUserSessions(c *gin.Context) {
	accountNo := c.GetString("accountNo")
	sessions, errCode := h.Chat.GetUserSessions(c.Request.Context(), accountNo)
	httpx.JSON(c, &dto.GetUserSessionsResponse{Sessions: toSessionInfoDTO(sessions)}, errCode)
}

// CreateSessionAndSendMessage 创建新会话并返回首条 AI 回复。
func (h *Handlers) CreateSessionAndSendMessage(c *gin.Context, req dto.CreateSessionRequest) {
	accountNo := c.GetString("accountNo")
	result, errCode := h.Chat.CreateSessionAndSend(c.Request.Context(), accountNo, req.UserQuestion, req.ModelType)
	httpx.JSON(c, &dto.CreateSessionResponse{AiInformation: result.Content, SessionID: result.SessionID}, errCode)
}

// CreateStreamSessionAndSendMessage 创建新会话并通过 SSE 推送流式回复。
func (h *Handlers) CreateStreamSessionAndSendMessage(c *gin.Context, req dto.CreateSessionRequest) {
	accountNo := c.GetString("accountNo")

	writer, ok := sse.NewWriter(c)
	if !ok {
		httpx.JSON(c, nil, code.CodeServerBusy)
		return
	}

	sessionID, errCode := h.Chat.CreateStreamSession(c.Request.Context(), accountNo, req.UserQuestion)
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to create session"})
		return
	}

	if err := writer.SendSessionID(sessionID); err != nil {
		return
	}

	errCode = h.Chat.StreamToSession(c.Request.Context(), accountNo, sessionID, req.UserQuestion, req.ModelType, writer.Chunk())
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}

	_ = writer.SendDone()
}

// ChatSend 在已有会话中发送消息并返回完整 AI 回复（非 SSE）。
func (h *Handlers) ChatSend(c *gin.Context, req dto.ChatSendRequest) {
	accountNo := c.GetString("accountNo")
	result, errCode := h.Chat.ChatSend(c.Request.Context(), accountNo, req.SessionID, req.UserQuestion, req.ModelType)
	httpx.JSON(c, &dto.ChatSendResponse{AiInformation: result.Content}, errCode)
}

// ChatStreamSend 在已有会话中发送消息并通过 SSE 推送流式回复。
func (h *Handlers) ChatStreamSend(c *gin.Context, req dto.ChatSendRequest) {
	accountNo := c.GetString("accountNo")

	writer, ok := sse.NewWriter(c)
	if !ok {
		httpx.JSON(c, nil, code.CodeServerBusy)
		return
	}

	errCode := h.Chat.StreamToSession(c.Request.Context(), accountNo, req.SessionID, req.UserQuestion, req.ModelType, writer.Chunk())
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}

	_ = writer.SendDone()
}

// ChatHistory 返回指定会话的历史消息。
func (h *Handlers) ChatHistory(c *gin.Context, req dto.ChatHistoryRequest) {
	accountNo := c.GetString("accountNo")
	history, errCode := h.Chat.GetChatHistory(c.Request.Context(), accountNo, req.SessionID)
	httpx.JSON(c, &dto.ChatHistoryResponse{History: toHistoryDTO(history)}, errCode)
}

// toSessionInfoDTO 将会话视图列表映射为 DTO。
func toSessionInfoDTO(views []chatapp.SessionView) []dto.SessionInfo {
	out := make([]dto.SessionInfo, 0, len(views))
	for _, v := range views {
		out = append(out, dto.SessionInfo{SessionID: v.SessionID, Title: v.Title})
	}
	return out
}

// toHistoryDTO 将历史消息视图列表映射为 DTO。
func toHistoryDTO(views []chatapp.MessageView) []dto.History {
	out := make([]dto.History, 0, len(views))
	for _, v := range views {
		out = append(out, dto.History{IsUser: v.IsUser, Content: v.Content})
	}
	return out
}
