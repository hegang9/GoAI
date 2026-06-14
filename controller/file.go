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

	// accountNo 来自 JWT 中间件，表示系统内部账号编号，而不是用户昵称。
	accountNo := c.GetString("accountNo")
	if accountNo == "" {
		logger.Error("AccountNo not found in context")
		JSON(c, nil, code.CodeInvalidToken)
		return
	}

	fileBO, err := file.UploadRagFile(accountNo, uploadedFile)
	if err != nil {
		logger.Error("UploadFile", "err", err)
		JSON(c, nil, code.CodeServerBusy)
		return
	}

	JSON(c, converter.FileBOToUploadResponse(fileBO), code.CodeSuccess)
}
