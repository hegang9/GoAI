package persistence

import (
	"GopherAI/internal/domain/delay"
	"context"
	"time"

	"crypto/sha256"
	"encoding/binary"
	"hash"

	"gorm.io/gorm"
)

type DelayTaskRepository struct {
	db *gorm.DB
}

func NewDelayTaskRepository(db *gorm.DB) *DelayTaskRepository {
	return &DelayTaskRepository{db: db}
}

// 编译期断言：DelayTaskRepository 必须满足领域端口。
var _ delay.DelayTaskRepository = (*DelayTaskRepository)(nil)

// Create 幂等创建任务。created 仅在本次调用新建记录时为 true；
// 相同 ID 和相同内容应返回已有任务，相同 ID 但内容不同应返回 ErrConfli
func (r *DelayTaskRepository) Create(ctx context.Context, task delay.Task) (stored delay.Task, created bool, err error) {
	po := DelayTaskPO{
		ID:          task.ID,
		AccountNo:   task.AccountNo,
		Destination: task.Destination,
		TargetAt:    task.TargetAt,
		Payload:     append([]byte{}, task.Payload...),
		PayLoadHash: hashPayLoad(task),
		Version:     task.Version,
		Status:      uint8(task.Status),
	}
	// 判断任务是否已经存在
	println(po)
	return delay.Task{}, false, nil

}

// Get 按账号和任务 ID 查询任务，accountNo 同时承担数据归属校验。
func (r *DelayTaskRepository) Get(ctx context.Context, accountNo string, taskID string) (delay.Task, error) {
	panic("not implemented") // TODO: Implement
}

// ClaimDue 批量抢占 target time 不晚于 ahead 的待调度任务，并为其设置 owner 和 leaseUntil。
// now 用于识别已经过期的租约；返回任务应已进入 StatusDispatching。
func (r *DelayTaskRepository) ClaimDue(ctx context.Context, now time.Time, ahead time.Time, leaseUntil time.Time, limit int, owner string) ([]delay.Task, error) {
	panic("not implemented") // TODO: Implement
}

// MarkLevelQueued 在 Level MQ 明确确认接管后，将匹配 owner 和 version 的任务标记为 StatusLevelQueued。
// ACK 结果未知时不得假定转交失败，允许租约恢复后使用同一任务 ID 重投并产生重复消息。
func (r *DelayTaskRepository) MarkLevelQueued(ctx context.Context, taskID string, owner string, version int64) error {
	panic("not implemented") // TODO: Implement
}

// Release 在明确确认 Level MQ 未接管任务时释放租约，使任务重新进入可抢占状态。
// cause 用于保留最近一次失败原因，不应作为幂等判断依据。
func (r *DelayTaskRepository) Release(ctx context.Context, taskID string, owner string, cause error) error {
	panic("not implemented") // TODO: Implement
}

// Cancel 使用 expectedVersion 执行乐观锁取消；任务已进入不可撤回阶段时返回 ErrTooLate。
func (r *DelayTaskRepository) Cancel(ctx context.Context, accountNo string, taskID string, expectedVersion int64) error {
	panic("not implemented") // TODO: Implement
}

// 计算 task 的 SHA-256 哈希值，用于幂等判断。
// 哈希了一下内容 destination + target_at_ms + payload
func hashPayLoad(task delay.Task) []byte {
	h := sha256.New()

	writeHashPart := func(h hash.Hash, value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(value)
	}

	writeHashPart(h, []byte(task.Destination))

	var targetAt [8]byte
	binary.BigEndian.PutUint64(
		targetAt[:],
		uint64(task.TargetAt),
	)
	_, _ = h.Write(targetAt[:])

	writeHashPart(h, task.Payload)

	return h.Sum(nil)
}
