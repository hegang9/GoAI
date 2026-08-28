// Package persistence 是持久化适配层：封装 GORM 连接与迁移，
// 并以「持久化对象（PO）+ 仓储实现」的方式落地领域层定义的仓储端口。
// PO（Persistence Object）仅带 gorm 标签服务于数据库映射；HTTP JSON 契约由 interfaces/http/dto 承担。
// 仓储实现负责在 PO 与领域实体之间转换，从而让领域层不感知数据库细节。
package persistence

import (
	"time"

	"gorm.io/gorm"
)

// UserPO 用户持久化对象，对应数据库 users 表。
type UserPO struct {

	// ID 主键。
	ID int64 `gorm:"primaryKey;column:id"`

	// Name 昵称/显示名，允许重复。
	Name string `gorm:"type:varchar(50);column:name"`

	// Email 邮箱，唯一索引。
	Email string `gorm:"type:varchar(100);uniqueIndex;not null;column:email"`

	// AccountNo 内部账号编号，唯一索引。
	AccountNo string `gorm:"type:varchar(50);uniqueIndex;not null;column:account_no"`

	// Password 密码哈希；json:"-" 防止调试序列化时意外泄露。
	Password string `gorm:"type:varchar(255);column:pass_word" json:"-"`

	// CreatedAt 创建时间，GORM 按字段名约定自动填充。
	CreatedAt time.Time

	// UpdatedAt 更新时间，GORM 按字段名约定自动填充。
	UpdatedAt time.Time

	// DeletedAt 软删除标记。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 显式指定用户表名。
func (UserPO) TableName() string { return "users" }

// SessionPO 会话持久化对象，对应数据库 sessions 表。
type SessionPO struct {

	// ID 会话唯一标识（UUID）。
	ID string `gorm:"primaryKey;type:varchar(36);column:id"`

	// AccountNo 会话归属的内部账号编号。
	AccountNo string `gorm:"index;not null;column:account_no;column:account_no"`

	// Title 会话标题。
	Title string `gorm:"type:varchar(100);column:title"`

	// CreatedAt 创建时间，GORM 按字段名约定自动填充。
	CreatedAt time.Time

	// UpdatedAt 更新时间，GORM 按字段名约定自动填充。
	UpdatedAt time.Time

	// DeletedAt 软删除标记。
	DeletedAt gorm.DeletedAt `gorm:"index;" json:"-"`
}

// TableName 显式指定会话表名。
func (SessionPO) TableName() string { return "sessions" }

// MessagePO 消息持久化对象，对应数据库 messages 表。
type MessagePO struct {

	// ID 自增主键。
	ID uint `gorm:"primaryKey;autoIncrement;column:id;index:idx_message_replay"`

	// MessageID 是跨 RabbitMQ 投递保持不变的消息唯一标识
	MessageID string `gorm:"type:varchar(36);not null;column:message_id;uniqueIndex:uk_messages_message_id"`

	// SessionID 所属会话标识，建立索引以加速按会话查询。
	SessionID string `gorm:"index;not null;type:varchar(36);column:session_id;index:idx_message_replay"`

	// AccountNo 消息归属的内部账号编号。
	AccountNo string `gorm:"type:varchar(50);column:account_no"`

	// Content 消息正文。
	Content string `gorm:"type:text;column:content"`

	// IsUser 标识来源：true 用户、false AI。
	IsUser bool `gorm:"not null;column:is_user"`

	// CreatedAt 创建时间，GORM 自动填充。
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at;index:idx_message_replay"`
}

// TableName 显式指定消息表名。
func (MessagePO) TableName() string { return "messages" }

// ConversationContextPO 保存会话历史的派生记忆视图。
// 原始消息仍由 MessagePO 完整保存；这里的摘要和核心记忆均可被重新生成。
type ConversationContextPO struct {
	// SessionID 同时作为主键，确保一个会话只有一份当前快照。
	SessionID string `gorm:"primaryKey;type:varchar(36);column:session_id"`
	// AccountNo 参与所有读写条件，防止不同账号之间发生记忆串线。
	AccountNo string `gorm:"type:varchar(50);not null;index;column:account_no"`
	// CoreMemory 保存少量、需要持续可见的稳定事实和用户约束。
	CoreMemory string `gorm:"type:text;column:core_memory"`
	// Summary 保存较早会话的任务状态、决定、结果和待办摘要。
	Summary string `gorm:"type:mediumtext;column:summary"`
	// CoveredMessageID 标记摘要已经覆盖到哪条原始消息。
	CoveredMessageID string `gorm:"type:varchar(36);column:covered_message_id"`
	// Version 每次覆盖保存时递增，为后续升级 CAS 乐观锁保留数据基础。
	Version   uint64 `gorm:"not null;default:1;column:version"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName 显式固定表名，避免领域命名调整影响数据库契约。
func (ConversationContextPO) TableName() string { return "conversation_contexts" }

// DelayTaskPO 延迟任务持久化对象，对应数据库 delay_tasks 表。
type DelayTaskPO struct {
	// ID 是全链路稳定的任务幂等键。
	ID string `gorm:"primaryKey;type:varchar(64);column:id;index:idx_delay_due,priority:3;index:idx_delay_lease,priority:3"`
	// AccountNo 用于任务归属和账号隔离。
	AccountNo string `gorm:"index;not null;column:account_no"`
	// TargetAt 是绝对目标时间，单位为 UTC Unix 毫秒。
	TargetAt int64 `gorm:"not null;column:target_at;index:idx_delay_due,priority:2"`
	// Version 标识当前状态转换版本，用于拒绝迟到回调。
	Version int64 `gorm:"not null;column:version"`
	// Status 表示任务当前所处的持有和转交阶段。
	Status uint8 `gorm:"not null;column:status;index:idx_delay_due,priority:1;index:idx_delay_lease,priority:1"`
	// TaskHash 是不可变任务内容的 SHA-256，用于幂等创建校验。
	TaskHash []byte `gorm:"type:binary(32);not null;column:task_hash"`
	// LeaseOwner 是当前持有任务租约的 Poller 实例标识。
	LeaseOwner string `gorm:"type:varchar(64);not null;default:'';column:lease_owner"`
	// LeaseUntilMs 是租约到期时间，单位为 UTC Unix 毫秒。
	LeaseUntilMs int64 `gorm:"not null;default:0;column:lease_until_ms;index:idx_delay_lease,priority:2"`
	// DispatchAttempts 是 Poller 抢占任务并尝试转交 Level MQ 的累计次数。
	DispatchAttempts int `gorm:"not null;default:0;column:attempts"`
	// LastError 保存最近一次明确投递失败的错误摘要。
	LastError string `gorm:"type:varchar(1024);not null;default:'';column:last_error"`
	// LevelQueuedAt 是 Level MQ 返回 Broker confirm 的时间。
	LevelQueuedAt *time.Time `gorm:"column:level_queued_at"`
	// CreatedAt 是任务记录的创建时间。
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at"`
	// UpdatedAt 是任务记录的最后更新时间。
	UpdatedAt time.Time `gorm:"autoUpdateTime;column:updated_at"`

	// Message 展开字段
	MessageID          string `gorm:"type:varchar(128);not null;column:message_id"`
	MessageTopic       string `gorm:"type:varchar(255);not null;column:message_topic"`
	MessageHeaders     []byte `gorm:"type:json;not null;column:message_headers"`
	MessageBody        []byte `gorm:"type:mediumblob;not null;column:message_body"`
	MessageTimestampMs int64  `gorm:"not null;column:message_timestamp_ms"`

	// Target 展开字段
	TargetKind          uint8  `gorm:"not null;column:target_kind"`
	TargetConsumerGroup string `gorm:"type:varchar(255);not null;column:target_consumer_group"`

	// RetryAttempt 是业务消费者当前重试次数。
	RetryAttempt uint32 `gorm:"not null;default:0;column:retry_times"`
}

// TableName 显式指定延迟任务表名。
func (DelayTaskPO) TableName() string { return "delay_tasks" }
