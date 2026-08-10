package delay

// Status 表示延迟任务在持久化等待和 Level MQ 转交阶段的生命周期状态。
// 它只描述领域状态，不暴露数据库字段值或消息队列实现细节。
type Status uint8

const (
	// StatusPending 表示任务仍由持久化存储持有，等待 Poller 抢占并投递到 Level MQ。
	StatusPending Status = iota + 1
	// StatusLevelQueued 表示 Level MQ 已确认接管任务，持久化记录仅用于查询和审计。
	StatusLevelQueued
	// StatusDispatching 表示任务已被 Poller 租约抢占，正在向 Level MQ 转交所有权。
	StatusDispatching
	// StatusCancelled 表示任务在允许取消的阶段被终止，不应继续向目标 MQ 投递。
	StatusCancelled
)

// Task 是延迟链路中的稳定任务模型。
// 同一任务经过持久化存储、Level MQ、Dispatcher 和目标 MQ 时必须保持相同的 ID 和 TargetAt，
// 以便在 ACK 丢失或进程重启导致重复投递时执行幂等判断。
type Task struct {
	// ID 是全链路唯一幂等标识，重试和重新调度时不得重新生成。
	ID string
	// AccountNo 标识任务所属账号，用于数据隔离和权限校验。
	AccountNo string
	// Destination 是目标逻辑名称，由基础设施适配器映射到受控的最终消息路由。
	Destination string
	// TargetAt 是 UTC Unix 毫秒绝对目标时间；进入各级延迟链路后不得按接收时间重新计算。
	TargetAt int64
	// Payload 是需要在目标时间投递的原始业务载荷，任务创建后应保持不可变。
	Payload []byte
	// Version 用于状态转换和取消操作的乐观并发控制。
	Version int64
	// Status 是任务当前生命周期状态。
	Status Status
}
