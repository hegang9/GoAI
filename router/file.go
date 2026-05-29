package router

import (
	"GopherAI/controller/file"

	"github.com/gin-gonic/gin"
)

func RegisterFileRouter(r *gin.RouterGroup) {
	r.POST("/upload", file.UploadRagFile)
}
