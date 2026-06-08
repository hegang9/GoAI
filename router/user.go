package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterUserRouter(r *gin.RouterGroup) {
	{
		r.POST("/register", controller.Register)
		r.POST("/login", controller.Login)
		r.POST("/captcha", controller.HandleCaptcha)
	}
}
