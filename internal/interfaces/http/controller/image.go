package controller

import (
	"io"

	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
)

// RecognizeImage 接收上传图片并返回图像识别结果。
func (h *Handlers) RecognizeImage(c *gin.Context) {
	// 从 multipart 请求中提取图片文件。
	file, err := c.FormFile("image")
	if err != nil {
		logger.Error("FormFile", "err", err)
		httpx.JSON(c, nil, code.CodeInvalidParams)
		return
	}

	src, err := file.Open()
	if err != nil {
		logger.Error("file open", "err", err)
		httpx.JSON(c, nil, code.CodeServerBusy)
		return
	}
	defer src.Close()

	buf, err := io.ReadAll(src)
	if err != nil {
		logger.Error("io.ReadAll", "err", err)
		httpx.JSON(c, nil, code.CodeServerBusy)
		return
	}

	className, errCode := h.Image.Recognize(buf)
	httpx.JSON(c, &dto.RecognizeImageResponse{ClassName: className}, errCode)
}
