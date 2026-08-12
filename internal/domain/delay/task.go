package delay

import (
	"errors"
	"fmt"
	"strings"

	messageDomain "GopherAI/internal/domain/message"
)

var ErrInvalidTask = errors.New("invalid delay task")

// Status 表示延迟任务在持久化等待和 Level MQ 转交阶段的生命周期状态。
// 它只描述领域状态，不暴露数据库字段值或消息队列实现细节。
type Status uint8

const (
	// StatusPending 表示任务仍由持久化存储持有，等待 Poller 抢占并投递到 Level MQ。
	StatusPending Status = 1
	// StatusLevelQueued 表示 Level MQ 已确认接管任务，持久化记录仅用于查询和审计。
	StatusLevelQueued Status = 2
	// StatusDispatching 表示任务已被 Poller 租约抢占，正在向 Level MQ 转交所有权。
	StatusDispatching Status = 3
	// StatusCancelled 表示任务在允许取消的阶段被终止，不应继续向目标 MQ 投递。
	StatusCancelled Status = 4
)

func (s Status) valid() bool {
	return s >= StatusPending && s <= StatusCancelled
}

// Task 描述在指定时间把稳定业务消息投递到逻辑目标的一次调度。
type Task struct {
	// ID 是 schedule_id，同一消费者组的同一次重试必须保持不变。
	ID        string
	AccountNo string
	Message   messageDomain.Message
	Target    messageDomain.Target
	// RetryAttempt 为 0 表示普通 Topic 投递，大于 0 表示业务消费者重试次数。
	RetryAttempt uint32
	// TargetAt 是 UTC Unix 毫秒绝对目标时间。
	TargetAt int64
	Version  int64
	Status   Status
}

// NewTask 创建处于 Pending、版本为 1 的新调度任务。
func NewTask(
	id string,
	accountNo string,
	message messageDomain.Message,
	target messageDomain.Target,
	retryAttempt uint32,
	targetAt int64,
) (Task, error) {
	return newTask(id, accountNo, message, target, retryAttempt, targetAt, 1, StatusPending)
}

// RestoreTask 从持久化数据恢复任务，并校验持久化状态是否合法。
func RestoreTask(
	id string,
	accountNo string,
	message messageDomain.Message,
	target messageDomain.Target,
	retryAttempt uint32,
	targetAt int64,
	version int64,
	status Status,
) (Task, error) {
	return newTask(id, accountNo, message, target, retryAttempt, targetAt, version, status)
}

func newTask(
	id string,
	accountNo string,
	message messageDomain.Message,
	target messageDomain.Target,
	retryAttempt uint32,
	targetAt int64,
	version int64,
	status Status,
) (Task, error) {
	task := Task{
		ID:           id,
		AccountNo:    accountNo,
		Message:      message.Clone(),
		Target:       target,
		RetryAttempt: retryAttempt,
		TargetAt:     targetAt,
		Version:      version,
		Status:       status,
	}
	if err := task.Validate(); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (t Task) Validate() error {
	switch {
	case strings.TrimSpace(t.ID) == "":
		return fmt.Errorf("%w: schedule id is empty", ErrInvalidTask)
	case strings.TrimSpace(t.AccountNo) == "":
		return fmt.Errorf("%w: account number is empty", ErrInvalidTask)
	case t.TargetAt <= 0:
		return fmt.Errorf("%w: target time must be positive", ErrInvalidTask)
	case t.Version <= 0:
		return fmt.Errorf("%w: version must be positive", ErrInvalidTask)
	case !t.Status.valid():
		return fmt.Errorf("%w: status %d is invalid", ErrInvalidTask, t.Status)
	}
	if err := t.Message.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTask, err)
	}
	if err := t.Target.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTask, err)
	}
	switch t.Target.Kind {
	case messageDomain.TargetTopic:
		if t.RetryAttempt != 0 {
			return fmt.Errorf("%w: topic target cannot have retry attempt", ErrInvalidTask)
		}
	case messageDomain.TargetConsumerGroup:
		if t.RetryAttempt == 0 {
			return fmt.Errorf("%w: consumer group target requires retry attempt", ErrInvalidTask)
		}
	}
	return nil
}
