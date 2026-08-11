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

// PublishRejectedError 表示下游明确没有接管消息，例如发布前校验失败、
// mandatory 消息被退回或者 Broker 返回 NACK。它与 confirm 超时、连接中断等
// “结果未知”错误不同：调用方只有识别到本错误时，才可以安全释放上游持有的任务。
type PublishRejectedError struct {
	Cause error
}

// Error 返回底层发布失败原因。
func (e *PublishRejectedError) Error() string {
	if e == nil || e.Cause == nil {
		return "delay message was definitely not accepted"
	}
	return e.Cause.Error()
}

// Unwrap 支持 errors.Is/errors.As 继续检查底层错误。
func (e *PublishRejectedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewPublishRejectedError 把基础设施层确认的“下游未接管”错误包装为领域语义。
// RabbitMQ 适配器不能把 confirm 超时或连接中断包装成该错误，因为消息可能已经入队。
func NewPublishRejectedError(cause error) error {
	return &PublishRejectedError{Cause: cause}
}

// IsPublishRejected 判断错误是否明确表示下游没有接管消息。
func IsPublishRejected(err error) bool {
	var rejected *PublishRejectedError
	return errors.As(err, &rejected)
}
