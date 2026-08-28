package rabbitmq

import (
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// FailureKind 描述业务处理失败后应该重试、进入 DLQ，还是立即停止消费。
type FailureKind uint8

const (
	FailureTransient FailureKind = iota
	FailurePermanent
	FailureAbort
)

func (kind FailureKind) String() string {
	switch kind {
	case FailureTransient:
		return "transient"
	case FailurePermanent:
		return "permanent"
	case FailureAbort:
		return "abort"
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}

// ProcessingError 显式标记确定性或系统性异常，未包装错误默认按瞬时异常处理。
type ProcessingError struct {
	Kind FailureKind
	Err  error
}

func (e *ProcessingError) Error() string { return e.Err.Error() }
func (e *ProcessingError) Unwrap() error { return e.Err }

func permanentError(err error) error {
	if err == nil {
		return nil
	}
	return &ProcessingError{Kind: FailurePermanent, Err: err}
}

func abortError(err error) error {
	if err == nil {
		return nil
	}
	return &ProcessingError{Kind: FailureAbort, Err: err}
}

func classifyError(err error) FailureKind {
	if err == nil {
		return FailureTransient
	}
	var processingErr *ProcessingError
	if errors.As(err, &processingErr) {
		return processingErr.Kind
	}
	return FailureTransient
}

func copyHeaders(source amqp.Table) amqp.Table {
	target := make(amqp.Table, len(source)+8)
	for key, value := range source {
		target[key] = value
	}
	return target
}
