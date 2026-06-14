package model

import (
	"time"

	"gorm.io/gorm"
)

// Session 代表一次用户与 AI 的对话会话，对应数据库中的 sessions 表。
// 每个用户可以有多个 Session，每个 Session 下关联多条消息（参见 Message 模型）。
//
// 与 User 模型的不同点：
//   - User.ID 是 int64 自增主键，Session.ID 是 varchar(36) 手动指定，
//     通常填入 UUID（如 "550e8400-e29b-41d4-a716-446655440000"），
//     适合分布式场景：多个服务生成 ID 不会冲突。
//   - AccountNo 通过字符串关联 User.AccountNo，而非外键，这是一种
//     简单的"逻辑外键"设计，比数据库外键更灵活但缺少数据库级约束。
type Session struct {
	// ID 是会话的唯一标识，使用 UUID v4 字符串（36 字符包含连字符）。
	// 不用自增 ID 的好处：分布式友好、避免被遍历猜测，前端可先生成 ID。
	ID string `gorm:"primaryKey;type:varchar(36)" json:"id"`

	// AccountNo 记录该会话属于哪个内部账号，通过 User.AccountNo 逻辑关联用户。
	// index 创建普通索引，加速"查某用户的所有会话"这类查询。
	// not null 在数据库层面强制该字段必须有值。
	AccountNo string `gorm:"index;not null;column:account_no" json:"account_no"`

	// Title 是会话的标题/摘要，比如首条消息的截断内容，用于在会话列表中展示。
	Title string `gorm:"type:varchar(100)" json:"title"`

	// CreatedAt 会话创建时间，GORM 自动填充。（GORM通过 字段名+字段类型 来识别是否需要自动填充）
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 会话最后活跃时间（有新增消息时更新），GORM 自动填充。
	UpdatedAt time.Time `json:"updated_at"`

	// DeletedAt 软删除标记：用户删除会话时不真正删数据，方便误删恢复和数据审计。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
