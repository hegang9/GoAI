package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterUserRouter(r *gin.RouterGroup) {
	{
		r.POST("/register", controller.Handler(controller.Register))
		r.POST("/login", controller.Handler(controller.Login))
		r.POST("/captcha", controller.Handler(controller.HandleCaptcha))
	}
}
