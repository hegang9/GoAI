package persistence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"GopherAI/internal/domain/delay"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
// 相同 ID 和相同内容返回已有任务，相同 ID 但内容不同返回 ErrConflict。
func (r *DelayTaskRepository) Create(ctx context.Context, task delay.Task) (stored delay.Task, created bool, err error) {
	po := delayTaskToPO(task)

	// 唯一键冲突时保留原记录，再通过任务哈希区分幂等重试和内容冲突。
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&po)
	if result.Error != nil {
		return delay.Task{}, false, result.Error
	}
	if result.RowsAffected == 1 {
		return delayTaskToDomain(po), true, nil
	}
	if result.RowsAffected != 0 {
		return delay.Task{}, false, fmt.Errorf("create delay task: unexpected rows affected: %d", result.RowsAffected)
	}

	var existing DelayTaskPO
	if err := r.db.WithContext(ctx).Where("id = ?", po.ID).Take(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return delay.Task{}, false, fmt.Errorf(
				"create delay task: insert skipped but task %q was not found",
				po.ID,
			)
		}
		return delay.Task{}, false, err
	}
	if existing.AccountNo != po.AccountNo || !compareTaskHash(existing, po) {
		return delay.Task{}, false, delay.ErrConflict
	}
	return delayTaskToDomain(existing), false, nil
}

// Get 按账号和任务 ID 查询任务，accountNo 同时承担数据归属校验。
func (r *DelayTaskRepository) Get(ctx context.Context, accountNo string, taskID string) (delay.Task, error) {
	var po DelayTaskPO
	// account_no 参与查询，避免仅凭任务 ID 读取其他账号的数据。
	err := r.db.WithContext(ctx).
		Where("id = ? AND account_no = ?", taskID, accountNo).
		Take(&po).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return delay.Task{}, delay.ErrNotFound
	}
	if err != nil {
		return delay.Task{}, err
	}
	return delayTaskToDomain(po), nil
}

// ClaimDue 批量抢占 target time 不晚于 ahead 的待调度任务，并为其设置 owner 和 leaseUntil。
// now 用于识别已经过期的租约；返回任务应已进入 StatusDispatching。
func (r *DelayTaskRepository) ClaimDue(ctx context.Context, now time.Time, ahead time.Time, leaseUntil time.Time, limit int, owner string) ([]delay.Task, error) {
	if limit <= 0 {
		return []delay.Task{}, nil
	}
	if owner == "" {
		return nil, errors.New("claim delay tasks: owner is empty")
	}
	if !leaseUntil.After(now) {
		return nil, errors.New("claim delay tasks: leaseUntil must be after now")
	}

	var claimed []delay.Task

	// 短事务锁定候选记录并写入租约；MQ 发布必须在事务提交后执行。
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pos []DelayTaskPO

		// 使用行锁查询任务，FOR UPDATE 对查询到的记录加排他行锁，SKIP LOCKED 跳过被锁定的记录，这样可以支持多个poller并行工作
		// 查询两种记录：第一类是即将到期但还没被处理的任务；第二类是已经被某个实例抢占但租约过期的任务，用于自动恢复
		err := tx.Clauses(clause.Locking{
			Strength: "UPDATE",
			Options:  "SKIP LOCKED",
		}).Where(
			"(status = ? AND target_at <= ?) OR (status = ? AND lease_until_ms < ?)",
			uint8(delay.StatusPending), ahead.UnixMilli(),
			uint8(delay.StatusDispatching), now.UnixMilli(),
		).Order("target_at ASC, id ASC").Limit(limit).Find(&pos).Error

		if err != nil || len(pos) == 0 {
			return err
		}

		ids := make([]string, len(pos))
		for i, po := range pos {
			ids[i] = po.ID
		}

		// 写入租约
		result := tx.Model(&DelayTaskPO{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":         uint8(delay.StatusDispatching),
				"lease_owner":    owner,
				"lease_until_ms": leaseUntil.UnixMilli(),
				"attempts":       gorm.Expr("attempts + 1"),
				"version":        gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(pos)) {
			return delay.ErrConflict
		}

		// 返回值携带抢占后的状态和版本，供后续条件更新使用。
		claimed = make([]delay.Task, len(pos))
		for i := range pos {
			pos[i].Status = uint8(delay.StatusDispatching)
			pos[i].Version++
			claimed[i] = delayTaskToDomain(pos[i])
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// MarkLevelQueued 在 Level MQ 明确确认接管后，将匹配 owner 和 version 的任务标记为 StatusLevelQueued。
// ACK 结果未知时不得假定转交失败，允许租约恢复后使用同一任务 ID 重投并产生重复消息。
func (r *DelayTaskRepository) MarkLevelQueued(ctx context.Context, taskID string, owner string, version int64) error {
	queuedAt := time.Now().UTC()
	// 只有当前租约持有者和版本能够确认所有权已经转移给 Level MQ。
	result := r.db.WithContext(ctx).
		Model(&DelayTaskPO{}).
		Where(
			"id = ? AND status = ? AND lease_owner = ? AND version = ?",
			taskID, uint8(delay.StatusDispatching), owner, version,
		).
		Updates(map[string]any{
			"status":          uint8(delay.StatusLevelQueued),
			"version":         gorm.Expr("version + 1"),
			"lease_owner":     "",
			"lease_until_ms":  0,
			"last_error":      "",
			"level_queued_at": queuedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	// 没有匹配到数据库的记录，需要判断是否有其他错误
	po, err := r.getByID(ctx, taskID)
	if err != nil {
		return err
	}
	// 数据库响应丢失后重复调用时，目标状态已经成立即可视为成功。
	if delay.Status(po.Status) == delay.StatusLevelQueued {
		return nil
	}
	return delay.ErrConflict
}

// Release 在明确确认 Level MQ 未接管任务时释放租约，使任务重新进入可抢占状态。
// cause 用于保留最近一次失败原因，不应作为幂等判断依据。
func (r *DelayTaskRepository) Release(ctx context.Context, taskID string, owner string, version int64, cause error) error {
	// 只有明确确认 MQ 未接管时才释放当前版本的租约，未知结果应等待租约过期。
	result := r.db.WithContext(ctx).
		Model(&DelayTaskPO{}).
		Where(
			"id = ? AND status = ? AND lease_owner = ? AND version = ?",
			taskID, uint8(delay.StatusDispatching), owner, version,
		).
		Updates(map[string]any{
			"status":         uint8(delay.StatusPending),
			"version":        gorm.Expr("version + 1"),
			"lease_owner":    "",
			"lease_until_ms": 0,
			"last_error":     errorSummary(cause),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	po, err := r.getByID(ctx, taskID)
	if err != nil {
		return err
	}
	switch delay.Status(po.Status) {
	case delay.StatusPending:
		return nil
	case delay.StatusLevelQueued, delay.StatusCancelled:
		return delay.ErrTooLate
	default:
		return delay.ErrConflict
	}
}

// Cancel 使用 expectedVersion 执行乐观锁取消；任务已进入不可撤回阶段时返回 ErrTooLate。
func (r *DelayTaskRepository) Cancel(ctx context.Context, accountNo string, taskID string, expectedVersion int64) error {
	// 仅 pending 状态允许取消，version 防止覆盖已经发生的抢占。
	result := r.db.WithContext(ctx).
		Model(&DelayTaskPO{}).
		Where(
			"id = ? AND account_no = ? AND status = ? AND version = ?",
			taskID, accountNo, uint8(delay.StatusPending), expectedVersion,
		).
		Updates(map[string]any{
			"status":  uint8(delay.StatusCancelled),
			"version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	var po DelayTaskPO
	err := r.db.WithContext(ctx).
		Select("id", "status", "version").
		Where("id = ? AND account_no = ?", taskID, accountNo).
		Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return delay.ErrNotFound
	}
	if err != nil {
		return err
	}
	switch delay.Status(po.Status) {
	case delay.StatusCancelled:
		return nil
	case delay.StatusDispatching, delay.StatusLevelQueued:
		return delay.ErrTooLate
	default:
		return delay.ErrConflict
	}
}

// === 以下是辅助函数 ===

// hashTask 计算不可变任务内容的 SHA-256，用于判断创建请求是否为幂等重试。
func hashTask(task delay.Task) []byte {
	h := sha256.New()

	writeHashPart := func(value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(value)
	}

	writeHashPart([]byte(task.Destination))

	var targetAt [8]byte
	binary.BigEndian.PutUint64(
		targetAt[:],
		uint64(task.TargetAt),
	)
	_, _ = h.Write(targetAt[:])

	writeHashPart(task.Payload)

	return h.Sum(nil)
}

// compareTaskHash 比较数据库记录和新任务的规范化内容哈希。
func compareTaskHash(storedTask DelayTaskPO, newTask DelayTaskPO) bool {
	return bytes.Equal(storedTask.TaskHash, newTask.TaskHash)
}

// delayTaskToPO 将领域任务转换为只在持久化层使用的 GORM 对象。
func delayTaskToPO(task delay.Task) DelayTaskPO {
	return DelayTaskPO{
		ID:          task.ID,
		AccountNo:   task.AccountNo,
		Destination: task.Destination,
		TargetAt:    task.TargetAt,
		Payload:     append([]byte(nil), task.Payload...),
		TaskHash:    hashTask(task),
		Version:     task.Version,
		Status:      uint8(task.Status),
	}
}

// delayTaskToDomain 将数据库记录转换为不包含 GORM 细节的领域任务。
func delayTaskToDomain(po DelayTaskPO) delay.Task {
	return delay.Task{
		ID:          po.ID,
		AccountNo:   po.AccountNo,
		Destination: po.Destination,
		TargetAt:    po.TargetAt,
		Payload:     append([]byte(nil), po.Payload...),
		Version:     po.Version,
		Status:      delay.Status(po.Status),
	}
}

// getByID 查询状态转换失败后的当前记录，并把未命中翻译为领域错误。
func (r *DelayTaskRepository) getByID(ctx context.Context, taskID string) (DelayTaskPO, error) {
	var po DelayTaskPO
	err := r.db.WithContext(ctx).
		Select("id", "status", "version", "lease_owner").
		Where("id = ?", taskID).
		Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DelayTaskPO{}, delay.ErrNotFound
	}
	return po, err
}

// errorSummary 截断错误摘要，防止异常文本超过数据库字段长度。
func errorSummary(cause error) string {
	if cause == nil {
		return ""
	}
	runes := []rune(cause.Error())
	const maxRunes = 1024
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}
