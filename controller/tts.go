package controller

import (
	"GopherAI/common/code"
	"GopherAI/converter"
	"GopherAI/dto"
	ttssvc "GopherAI/service/tts"

	"github.com/gin-gonic/gin"
)

// CreateTTSTask 接收文本转语音请求，校验参数后委派给 service/tts 处理。
func CreateTTSTask(c *gin.Context, req dto.TTSRequest) {
	if req.Text == "" {
		JSON(c, nil, code.CodeInvalidParams)
		return
	}

	result, errCode := ttssvc.CreateTTSTask(c, req.Text)
	JSON(c, converter.TTSResultBOToResponse(result), errCode)
}

// QueryTTSTask 查询文本转语音任务状态，校验参数后委派给 service/tts 处理。
func QueryTTSTask(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		JSON(c, nil, code.CodeInvalidParams)
		return
	}

	result, errCode := ttssvc.QueryTTSTask(c, taskID)
	JSON(c, converter.TTSResultBOToQueryResponse(result), errCode)
}
