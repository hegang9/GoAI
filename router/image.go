package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterImageRouter(r *gin.RouterGroup) {

	r.POST("/recognize", controller.RecognizeImage)
}
