package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

func RegisterFileRouter(r *gin.RouterGroup) {
	r.POST("/upload", controller.UploadRagFile)
}
