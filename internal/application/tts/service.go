// Package tts 是文本转语音应用服务：编排创建任务与查询任务用例。
//
// 它依赖语音合成端口（tts.Synthesizer），由 bootstrap 注入具体实现（如百度 TTS）。
package tts

import (
	"context"

	domaintts "GopherAI/internal/domain/tts"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"
)

// Service 文本转语音应用服务。
type Service struct {
	synth domaintts.Synthesizer
}

// NewService 创建文本转语音应用服务。
func NewService(synth domaintts.Synthesizer) *Service {
	return &Service{synth: synth}
}

// CreateTask 提交文本转语音任务，成功时返回任务结果（含任务 ID）。
func (s *Service) CreateTask(ctx context.Context, text string) (domaintts.TaskResult, code.Code) {
	taskID, err := s.synth.Create(ctx, text)
	if err != nil {
		logger.Error("CreateTask failed", "err", err)
		return domaintts.TaskResult{}, code.TTSFail
	}
	logger.Info("CreateTask success", "taskID", taskID)
	return domaintts.TaskResult{TaskID: taskID}, code.CodeSuccess
}

// QueryTask 查询文本转语音任务的状态与结果。
func (s *Service) QueryTask(ctx context.Context, taskID string) (domaintts.TaskResult, code.Code) {
	result, err := s.synth.Query(ctx, taskID)
	if err != nil {
		logger.Error("QueryTask failed", "taskID", taskID, "err", err)
		return domaintts.TaskResult{}, code.TTSFail
	}
	return result, code.CodeSuccess
}
