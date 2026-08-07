package delay

import (
	"context"
	"time"
)

// DelayTaskRepository 定义长延迟任务所需的可靠持有、租约抢占和状态转换能力。
// 实现方必须保证状态转换具备持久性，并通过 owner 与 version 避免旧实例覆盖新租约。
type DelayTaskRepository interface {
	// Create 幂等创建任务。created 仅在本次调用新建记录时为 true；
	// 相同 ID 和相同内容应返回已有任务，相同 ID 但内容不同应返回 ErrConflict。
	Create(ctx context.Context, task Task) (stored Task, created bool, err error)
	// Get 按账号和任务 ID 查询任务，accountNo 同时承担数据归属校验。
	Get(ctx context.Context, accountNo, taskID string) (Task, error)
	// ClaimDue 批量抢占 target time 不晚于 ahead 的待调度任务，并为其设置 owner 和 leaseUntil。
	// now 用于识别已经过期的租约；返回任务应已进入 StatusDispatching。
	ClaimDue(ctx context.Context, now, ahead, leaseUntil time.Time, limit int, owner string) ([]Task, error)
	// MarkLevelQueued 在 Level MQ 明确确认接管后，将匹配 owner 和 version 的任务标记为 StatusLevelQueued。
	// ACK 结果未知时不得假定转交失败，允许租约恢复后使用同一任务 ID 重投并产生重复消息。
	MarkLevelQueued(ctx context.Context, taskID, owner string, version int64) error
	// Release 在明确确认 Level MQ 未接管任务时释放租约，使任务重新进入可抢占状态。
	// cause 用于保留最近一次失败原因，不应作为幂等判断依据。
	Release(ctx context.Context, taskID, owner string, cause error) error
	// Cancel 使用 expectedVersion 执行乐观锁取消；任务已进入不可撤回阶段时返回 ErrTooLate。
	Cancel(ctx context.Context, accountNo, taskID string, expectedVersion int64) error
}

// LevelPublisher 将任务转交给 0～60 秒的 Level 延迟层。
// Publish 只有在下游已经可靠接管任务时才能返回 nil；level 0 表示直接进入 Dispatcher Inbox。
type LevelPublisher interface {
	// Publish 使用稳定任务 ID 发布到指定 Level，重试时不得改变 Task.ID 或 Task.TargetAt。
	Publish(ctx context.Context, level int, task Task) error
}

// FinalPublisher 将成熟任务直接投递到最终业务消息通道。
// Publish 返回 nil 表示目标消息系统已经确认接管，调用方随后才可以 ACK Dispatcher Inbox 原消息。
type FinalPublisher interface {
	// Publish 将任务发送到受控的逻辑 destination，并保留 Task.ID 作为最终消息幂等键。
	Publish(ctx context.Context, destination string, task Task) error
}
