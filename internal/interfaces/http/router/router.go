// Package router 负责装配 Gin 路由：把处理器（controller.Handlers）与中间件挂载到路由树。
package router

import (
	"net/http"

	domainuser "GopherAI/internal/domain/user"
	"GopherAI/internal/interfaces/http/controller"
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"
	"GopherAI/internal/interfaces/http/middleware"
	"GopherAI/pkg/code"

	"github.com/gin-gonic/gin"
)

// New 初始化 Gin 引擎并挂载公开接口与鉴权接口。
//
// h 为业务处理器集合，issuer 为 JWT 校验端口（用于鉴权中间件），均由 bootstrap 注入。
func New(h *controller.Handlers, issuer domainuser.TokenIssuer) *gin.Engine {
	r := gin.Default()
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, dto.Response{
			StatusCode: code.CodeRecordNotFound,
			StatusMsg:  http.StatusText(http.StatusNotFound),
		})
	})
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, dto.Response{
			StatusCode: code.CodeInvalidParams,
			StatusMsg:  http.StatusText(http.StatusMethodNotAllowed),
		})
	})

	v1 := r.Group("/api/v1")

	// 公开路由，不需要鉴权。
	registerUserRoutes(v1.Group("/user"), h)

	// JWT 鉴权路由组：中间件只挂载一次，子组自动继承。
	auth := v1.Group("")
	auth.Use(middleware.JWTAuth(issuer))
	{
		registerAIRoutes(auth.Group("/ai"), h)
		registerImageRoutes(auth.Group("/image"), h)
		registerFileRoutes(auth.Group("/file"), h)
	}

	return r
}

// registerUserRoutes 注册用户认证与验证码接口。
func registerUserRoutes(r *gin.RouterGroup, h *controller.Handlers) {
	r.POST("/register", httpx.Handler(h.Register))
	r.POST("/login", httpx.Handler(h.Login))
	r.POST("/captcha", httpx.Handler(h.HandleCaptcha))
}

// registerAIRoutes 注册 AI 聊天与语音接口。
func registerAIRoutes(r *gin.RouterGroup, h *controller.Handlers) {
	r.GET("/chat/sessions", h.GetUserSessions)
	r.POST("/chat/send-new-session", httpx.Handler(h.CreateSessionAndSendMessage))
	r.POST("/chat/send", httpx.Handler(h.ChatSend))
	r.POST("/chat/history", httpx.Handler(h.ChatHistory))

	r.POST("/tts", httpx.Handler(h.CreateTTSTask))
	r.GET("/tts/query", h.QueryTTSTask)

	r.POST("/chat/send-stream-new-session", httpx.Handler(h.CreateStreamSessionAndSendMessage))
	r.POST("/chat/send-stream", httpx.Handler(h.ChatStreamSend))
}

// registerImageRoutes 注册图像识别接口。
func registerImageRoutes(r *gin.RouterGroup, h *controller.Handlers) {
	r.POST("/recognize", h.RecognizeImage)
}

// registerFileRoutes 注册知识库文件上传接口。
func registerFileRoutes(r *gin.RouterGroup, h *controller.Handlers) {
	r.POST("/upload", h.UploadRagFile)
}
