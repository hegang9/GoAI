package router

import (
	"GopherAI/controller/image"

	"github.com/gin-gonic/gin"
)

func RegisterImageRouter(r *gin.RouterGroup) {

	r.POST("/recognize", image.RecognizeImage)
}
