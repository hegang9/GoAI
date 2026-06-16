// Package user 是用户领域：定义用户实体、领域错误以及该领域依赖的端口接口。
//
// 该包属于领域层核心，不依赖任何外层（gin / gorm / redis / config 等），
// 只通过端口接口（由 infrastructure 实现）与外部协作。
package user

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound 表示按条件未查询到用户，是领域层统一的“未找到”语义，
// 由仓储实现（infrastructure/persistence）在底层 ErrRecordNotFound 时返回。
var ErrNotFound = errors.New("user not found")

// User 是用户领域实体，承载与持久化细节无关的业务字段。
// 它与数据库模型（持久化对象）解耦：仓储实现负责两者之间的转换。
type User struct {
	// ID 用户主键。
	ID int64
	// Name 昵称/显示名，允许重复，不可作为唯一标识。
	Name string
	// Email 注册邮箱，唯一，用于登录与验证码。
	Email string
	// AccountNo 系统内部账号编号，唯一，用于 JWT、会话归属与文件隔离。
	AccountNo string
	// PasswordHash 已经过 bcrypt 哈希的密码，绝不保存明文。
	PasswordHash string
	// CreatedAt 创建时间。
	CreatedAt time.Time
	// UpdatedAt 更新时间。
	UpdatedAt time.Time
}

// Repository 定义用户持久化端口，由 infrastructure 层实现。
type Repository interface {
	// FindByEmail 按邮箱查询用户，未找到时返回 ErrNotFound。
	FindByEmail(ctx context.Context, email string) (*User, error)
	// FindByAccountNo 按内部账号编号查询用户，未找到时返回 ErrNotFound。
	FindByAccountNo(ctx context.Context, accountNo string) (*User, error)
	// Create 创建用户记录并回填生成的字段（如自增 ID）。
	Create(ctx context.Context, u *User) (*User, error)
}

// PasswordHasher 定义密码哈希端口，隔离具体哈希算法（如 bcrypt）。
type PasswordHasher interface {
	// Hash 对明文密码做不可逆哈希。
	Hash(plain string) (string, error)
	// Compare 校验明文密码与哈希是否匹配。
	Compare(plain, hash string) bool
}

// TokenIssuer 定义身份令牌端口，隔离具体的 JWT 实现与密钥来源。
type TokenIssuer interface {
	// Issue 为指定用户签发携带账号编号的令牌。
	Issue(id int64, accountNo string) (string, error)
	// Parse 校验令牌并返回其中的账号编号；非法时返回 ok=false。
	Parse(token string) (accountNo string, ok bool)
}

// CaptchaStore 定义验证码存储端口（通常由 Redis 实现）。
type CaptchaStore interface {
	// Set 写入邮箱验证码并设置有效期。
	Set(ctx context.Context, email, captcha string) error
	// Check 校验验证码是否匹配（匹配成功通常会一次性消费）。
	Check(ctx context.Context, email, input string) (bool, error)
}

// Mailer 定义邮件发送端口（通常由 SMTP 实现）。
type Mailer interface {
	// Send 向指定邮箱发送一段以 prefix 为前缀的内容（验证码或账号编号）。
	Send(email, content, prefix string) error
}
