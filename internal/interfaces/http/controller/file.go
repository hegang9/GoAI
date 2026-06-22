package controller

import (
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
)

// UploadRagFile 接收知识库文件并返回上传结果。
func (h *Handlers) UploadRagFile(c *gin.Context) {
	// 读取上传文件元数据。
	uploaded, err := c.FormFile("file")
	if err != nil {
		logger.Error("FormFile", "err", err)
		httpx.JSON(c, nil, code.CodeInvalidParams)
		return
	}

	// accountNo 来自 JWT 中间件。
	accountNo := c.GetString("accountNo")
	if accountNo == "" {
		logger.Error("AccountNo not found in context")
		httpx.JSON(c, nil, code.CodeInvalidToken)
		return
	}

	// 打开文件内容交给应用层处理。
	src, err := uploaded.Open()
	if err != nil {
		logger.Error("open uploaded file failed", "err", err)
		httpx.JSON(c, nil, code.CodeServerBusy)
		return
	}
	defer src.Close()

	filePath, errCode := h.File.UploadRagFile(c.Request.Context(), accountNo, uploaded.Filename, src)
	httpx.JSON(c, dto.UploadFileResponse{FilePath: filePath}, errCode)
}

// ListRagFiles 返回当前登录账号已上传的知识库文档文件名列表。
func (h *Handlers) ListRagFiles(c *gin.Context) {
	// accountNo 来自 JWT 中间件。
	accountNo := c.GetString("accountNo")
	if accountNo == "" {
		logger.Error("AccountNo not found in context")
		httpx.JSON(c, nil, code.CodeInvalidToken)
		return
	}

	files, errCode := h.File.ListRagFiles(accountNo)
	// 归一化为非 nil 切片，避免响应出现 null。
	if files == nil {
		files = []string{}
	}
	httpx.JSON(c, dto.ListRagFilesResponse{Files: files}, errCode)
}

// DeleteRagFiles 批量删除当前登录账号的若干知识库文档及其向量数据。
func (h *Handlers) DeleteRagFiles(c *gin.Context, req dto.DeleteRagFilesRequest) {
	// accountNo 来自 JWT 中间件。
	accountNo := c.GetString("accountNo")
	if accountNo == "" {
		logger.Error("AccountNo not found in context")
		httpx.JSON(c, nil, code.CodeInvalidToken)
		return
	}

	deleted, errCode := h.File.DeleteRagFiles(c.Request.Context(), accountNo, req.Filenames)
	httpx.JSON(c, dto.DeleteRagFilesResponse{Deleted: deleted}, errCode)
}
