// Package tts 提供文本转语音（TTS）的业务服务层。
//
// 该层位于 controller 与基础设施（common/tts）之间，恢复统一的
// controller -> service -> infra 分层：
//   - controller 只负责参数解析与响应封装；
//   - service/tts 负责编排业务流程、把基础设施返回转换为 bo 与错误码；
//   - common/tts 只负责与百度 TTS 接口交互的传输细节。
package tts

import (
	"GopherAI/bo"
	"GopherAI/common/code"
	"GopherAI/common/logger"
	commontts "GopherAI/common/tts"
	"context"
)

// ttsClient 持有底层 TTS 基础设施客户端（百度 TTS）。
// 当前实现无状态，进程内复用同一实例即可。
var ttsClient = commontts.NewTTSService()

// CreateTTSTask 提交文本转语音任务，成功时返回包含任务 ID 的业务对象。
func CreateTTSTask(ctx context.Context, text string) (bo.TTSResultBO, code.Code) {
	taskID, err := ttsClient.CreateTTS(ctx, text)
	if err != nil {
		logger.Error("CreateTTSTask CreateTTS failed", "err", err)
		return bo.TTSResultBO{}, code.TTSFail
	}

	logger.Info("CreateTTSTask success", "taskID", taskID)
	return bo.TTSResultBO{TaskID: taskID}, code.CodeSuccess
}

// QueryTTSTask 查询文本转语音任务的状态与结果。
func QueryTTSTask(ctx context.Context, taskID string) (bo.TTSResultBO, code.Code) {
	resp, err := ttsClient.QueryTTSFull(ctx, taskID)
	if err != nil {
		logger.Error("QueryTTSTask QueryTTSFull failed", "taskID", taskID, "err", err)
		return bo.TTSResultBO{}, code.TTSFail
	}

	if len(resp.TasksInfo) == 0 {
		logger.Warn("QueryTTSTask empty tasks_info", "taskID", taskID)
		return bo.TTSResultBO{}, code.TTSFail
	}

	// 当前每次只查询单个任务，取第一条结果即可。
	info := resp.TasksInfo[0]
	result := bo.TTSResultBO{TaskID: info.TaskID, TaskStatus: info.TaskStatus}
	if info.TaskResult != nil {
		result.SpeechURL = info.TaskResult.SpeechURL
	}

	return result, code.CodeSuccess
}
