package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterUserRouter(r *gin.RouterGroup) {
	{
		r.POST("/register", Handler(controller.Register))
		r.POST("/login", Handler(controller.Login))
		r.POST("/captcha", Handler(controller.HandleCaptcha))
	}
}
