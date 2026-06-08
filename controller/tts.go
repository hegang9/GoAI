package controller

import (
	"GopherAI/bo"
	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/common/tts"
	"GopherAI/converter"
	"GopherAI/dto"

	"github.com/gin-gonic/gin"
)

type TTSServices struct {
	ttsService *tts.TTSService
}

func NewTTSServices() *TTSServices {
	return &TTSServices{
		ttsService: tts.NewTTSService(),
	}
}

func CreateTTSTask(c *gin.Context) {
	req, ok := BindJSON[dto.TTSRequest](c)
	if !ok {
		return
	}

	if req.Text == "" {
		JSON(c, nil, code.CodeInvalidParams)
		return
	}

	tts := NewTTSServices()
	taskID, err := tts.ttsService.CreateTTS(c, req.Text)
	if err != nil {
		JSON(c, nil, code.TTSFail)
		return
	}

	JSON(c, converter.TTSResultBOToResponse(bo.TTSResultBO{TaskID: taskID}), code.CodeSuccess)
}

func QueryTTSTask(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		JSON(c, nil, code.CodeInvalidParams)
		return
	}

	tts := NewTTSServices()
	TTSQueryResponse, err := tts.ttsService.QueryTTSFull(c, taskID)
	if err != nil {
		logger.Error("语音合成失败", "err", err)
		JSON(c, nil, code.TTSFail)
		return
	}

	if len(TTSQueryResponse.TasksInfo) == 0 {
		JSON(c, nil, code.TTSFail)
		return
	}

	result := bo.TTSResultBO{TaskID: TTSQueryResponse.TasksInfo[0].TaskID, TaskStatus: TTSQueryResponse.TasksInfo[0].TaskStatus}
	if TTSQueryResponse.TasksInfo[0].TaskResult != nil {
		result.SpeechURL = TTSQueryResponse.TasksInfo[0].TaskResult.SpeechURL
	}

	JSON(c, converter.TTSResultBOToQueryResponse(result), code.CodeSuccess)
}
