package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterAIRouter(r *gin.RouterGroup) {

	// 聊天相关接口
	{
		r.GET("/chat/sessions", controller.GetUserSessionsByUserName)
		r.POST("/chat/send-new-session", controller.Handler(controller.CreateSessionAndSendMessage))
		r.POST("/chat/send", controller.Handler(controller.ChatSend))
		r.POST("/chat/history", controller.Handler(controller.ChatHistory))

		// TTS相关接口
		r.POST("/tts", controller.Handler(controller.CreateTTSTask))
		r.GET("/tts/query", controller.QueryTTSTask)

		r.POST("/chat/send-stream-new-session", controller.Handler(controller.CreateStreamSessionAndSendMessage))
		r.POST("/chat/send-stream", controller.Handler(controller.ChatStreamSend))
	}
}
