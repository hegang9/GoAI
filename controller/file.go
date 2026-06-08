package controller

import (
	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/converter"
	"GopherAI/service/file"

	"github.com/gin-gonic/gin"
)

func UploadRagFile(c *gin.Context) {
	uploadedFile, err := c.FormFile("file")
	if err != nil {
		logger.Error("FormFile", "err", err)
		JSON(c, nil, code.CodeInvalidParams)
		return
	}

	username := c.GetString("userName")
	if username == "" {
		logger.Error("Username not found in context")
		JSON(c, nil, code.CodeInvalidToken)
		return
	}

	fileBO, err := file.UploadRagFile(username, uploadedFile)
	if err != nil {
		logger.Error("UploadFile", "err", err)
		JSON(c, nil, code.CodeServerBusy)
		return
	}

	JSON(c, converter.FileBOToUploadResponse(fileBO), code.CodeSuccess)
}
