// Package tts 是文本转语音领域：定义语音合成端口与任务结果值对象。
//
// 该包只声明契约，不依赖具体厂商实现（百度 TTS）；实现位于 infrastructure/tts。
package tts

import "context"

// TaskResult 表示一次 TTS 任务的查询结果。
type TaskResult struct {
	// TaskID 任务标识。
	TaskID string
	// Status 任务状态。
	Status string
	// SpeechURL 合成完成后的音频地址，未完成时为空。
	SpeechURL string
}

// Synthesizer 定义文本转语音端口。
type Synthesizer interface {
	// Create 提交文本转语音任务，返回任务 ID。
	Create(ctx context.Context, text string) (taskID string, err error)
	// Query 查询任务状态与结果。
	Query(ctx context.Context, taskID string) (TaskResult, error)
}
