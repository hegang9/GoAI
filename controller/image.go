package controller

import (
	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/converter"
	"GopherAI/service/image"

	"github.com/gin-gonic/gin"
)

func RecognizeImage(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		logger.Error("FormFile", "err", err)
		JSON(c, nil, code.CodeInvalidParams)
		return
	}

	imageBO, err := image.RecognizeImage(file)
	if err != nil {
		logger.Error("RecognizeImage", "err", err)
		JSON(c, nil, code.CodeServerBusy)
		return
	}

	JSON(c, converter.ImageResultBOToResponse(imageBO), code.CodeSuccess)
}
