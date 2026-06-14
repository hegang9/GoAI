package controller

import (
	"GopherAI/common/code"
	"GopherAI/converter"
	"GopherAI/dto"
	"GopherAI/service/session"

	"github.com/gin-gonic/gin"
)

func GetUserSessionsByUserName(c *gin.Context) {
	userName := c.GetString("userName")

	sessions, errCode := session.GetUserSessionsByUserName(userName)
	JSON(c, dto.GetUserSessionsResponse{Sessions: converter.SessionInfoBOsToDTO(sessions)}, errCode)
}

func CreateSessionAndSendMessage(c *gin.Context, req dto.CreateSessionRequest) {
	userName := c.GetString("userName")
	result, errCode := session.CreateSessionAndSendMessage(userName, req.UserQuestion, req.ModelType)
	JSON(c, converter.AIResponseBOToCreateSessionResponse(result), errCode)
}

func CreateStreamSessionAndSendMessage(c *gin.Context, req dto.CreateSessionRequest) {
	userName := c.GetString("userName")

	// 由 SSE 适配器统一接管响应头与流式传输细节。
	sse, ok := NewSSEWriter(c)
	if !ok {
		JSON(c, nil, code.CodeServerBusy)
		return
	}

	sessionID, errCode := session.CreateStreamSessionOnly(userName, req.UserQuestion)
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to create session"})
		return
	}

	// 先告知客户端本次流对应的会话 ID。
	if err := sse.SendSessionID(sessionID); err != nil {
		return
	}

	errCode = session.StreamMessageToExistingSession(userName, sessionID, req.UserQuestion, req.ModelType, sse.Chunk())
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}

	_ = sse.SendDone()
}

func ChatSend(c *gin.Context, req dto.ChatSendRequest) {
	userName := c.GetString("userName")
	result, errCode := session.ChatSend(userName, req.SessionID, req.UserQuestion, req.ModelType)
	JSON(c, converter.AIResponseBOToChatSendResponse(result), errCode)
}

func ChatStreamSend(c *gin.Context, req dto.ChatSendRequest) {
	userName := c.GetString("userName")

	// 由 SSE 适配器统一接管响应头与流式传输细节。
	sse, ok := NewSSEWriter(c)
	if !ok {
		JSON(c, nil, code.CodeServerBusy)
		return
	}

	errCode := session.ChatStreamSend(userName, req.SessionID, req.UserQuestion, req.ModelType, sse.Chunk())
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}

	_ = sse.SendDone()
}

func ChatHistory(c *gin.Context, req dto.ChatHistoryRequest) {
	userName := c.GetString("userName")
	history, errCode := session.GetChatHistory(userName, req.SessionID)
	JSON(c, dto.ChatHistoryResponse{History: converter.MessageBOsToHistoryDTO(history)}, errCode)
}
