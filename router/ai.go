package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterAIRouter(r *gin.RouterGroup) {

	// 聊天相关接口
	{
		r.GET("/chat/sessions", controller.GetUserSessionsByAccountNo)
		r.POST("/chat/send-new-session", Handler(controller.CreateSessionAndSendMessage))
		r.POST("/chat/send", Handler(controller.ChatSend))
		r.POST("/chat/history", Handler(controller.ChatHistory))

		// TTS相关接口
		r.POST("/tts", Handler(controller.CreateTTSTask))
		r.GET("/tts/query", controller.QueryTTSTask)

		r.POST("/chat/send-stream-new-session", Handler(controller.CreateStreamSessionAndSendMessage))
		r.POST("/chat/send-stream", Handler(controller.ChatStreamSend))
	}
}
