package session

import (
	"GopherAI/common/code"
	"GopherAI/controller"
	"GopherAI/converter"
	"GopherAI/dto"
	"GopherAI/service/session"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetUserSessionsByUserName(c *gin.Context) {
	userName := c.GetString("userName")

	sessions, errCode := session.GetUserSessionsByUserName(userName)
	controller.JSON(c, dto.GetUserSessionsResponse{Sessions: converter.SessionInfoBOsToDTO(sessions)}, errCode)
}

func CreateSessionAndSendMessage(c *gin.Context) {
	req, ok := controller.BindJSON[dto.CreateSessionRequest](c)
	if !ok {
		return
	}

	userName := c.GetString("userName")
	result, errCode := session.CreateSessionAndSendMessage(userName, req.UserQuestion, req.ModelType)
	controller.JSON(c, converter.AIResponseBOToCreateSessionResponse(result), errCode)
}

func CreateStreamSessionAndSendMessage(c *gin.Context) {
	req, ok := controller.BindJSON[dto.CreateSessionRequest](c)
	if !ok {
		return
	}

	userName := c.GetString("userName")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	sessionID, errCode := session.CreateStreamSessionOnly(userName, req.UserQuestion)
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to create session"})
		return
	}

	fmt.Fprintf(c.Writer, "data: {\"sessionId\": \"%s\"}\n\n", sessionID)
	c.Writer.Flush()

	errCode = session.StreamMessageToExistingSession(userName, sessionID, req.UserQuestion, req.ModelType, http.ResponseWriter(c.Writer))
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}
}

func ChatSend(c *gin.Context) {
	req, ok := controller.BindJSON[dto.ChatSendRequest](c)
	if !ok {
		return
	}

	userName := c.GetString("userName")
	result, errCode := session.ChatSend(userName, req.SessionID, req.UserQuestion, req.ModelType)
	controller.JSON(c, converter.AIResponseBOToChatSendResponse(result), errCode)
}

func ChatStreamSend(c *gin.Context) {
	req, ok := controller.BindJSON[dto.ChatSendRequest](c)
	if !ok {
		return
	}

	userName := c.GetString("userName")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	errCode := session.ChatStreamSend(userName, req.SessionID, req.UserQuestion, req.ModelType, http.ResponseWriter(c.Writer))
	if errCode != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}
}

func ChatHistory(c *gin.Context) {
	req, ok := controller.BindJSON[dto.ChatHistoryRequest](c)
	if !ok {
		return
	}

	userName := c.GetString("userName")
	history, errCode := session.GetChatHistory(userName, req.SessionID)
	controller.JSON(c, dto.ChatHistoryResponse{History: converter.MessageBOsToHistoryDTO(history)}, errCode)
}
