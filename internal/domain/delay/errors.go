package delay

import "errors"

var (
	// ErrNotFound 表示指定账号下不存在对应延迟任务。
	ErrNotFound = errors.New("delay task not found")
	// ErrConflict 表示相同任务 ID 已存在，但不可变任务内容与本次请求不一致。
	ErrConflict = errors.New("delay task already exists,and the payload is different")
	// ErrTooLate 表示任务已经进入 Level MQ 或最终发布阶段，无法保证取消生效。
	ErrTooLate = errors.New("delay task is already queued or publishing")
)
