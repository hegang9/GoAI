package controller

import (
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"
	"GopherAI/pkg/code"

	"github.com/gin-gonic/gin"
)

// CreateTTSTask 接收文本转语音请求，校验参数后委派应用层处理。
func (h *Handlers) CreateTTSTask(c *gin.Context, req dto.TTSRequest) {
	if req.Text == "" {
		httpx.JSON(c, nil, code.CodeInvalidParams)
		return
	}
	result, errCode := h.TTS.CreateTask(c.Request.Context(), req.Text)
	httpx.JSON(c, &dto.TTSResponse{TaskID: result.TaskID}, errCode)
}

// QueryTTSTask 查询文本转语音任务状态。
func (h *Handlers) QueryTTSTask(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		httpx.JSON(c, nil, code.CodeInvalidParams)
		return
	}
	result, errCode := h.TTS.QueryTask(c.Request.Context(), taskID)
	httpx.JSON(c, &dto.QueryTTSResponse{
		TaskID:     result.TaskID,
		TaskStatus: result.Status,
		TaskResult: result.SpeechURL,
	}, errCode)
}
