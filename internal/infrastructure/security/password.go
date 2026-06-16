// Package security 是安全适配层：实现领域层定义的密码哈希（PasswordHasher）
// 与身份令牌（TokenIssuer）端口。
package security

import (
	domainuser "GopherAI/internal/domain/user"
	"GopherAI/pkg/logger"

	"golang.org/x/crypto/bcrypt"
)

// BcryptHasher 基于 bcrypt 实现 domain/user.PasswordHasher 端口。
type BcryptHasher struct{}

// NewBcryptHasher 创建 bcrypt 密码哈希器。
func NewBcryptHasher() *BcryptHasher { return &BcryptHasher{} }

// 编译期断言：BcryptHasher 必须满足领域端口。
var _ domainuser.PasswordHasher = (*BcryptHasher)(nil)

// Hash 使用 bcrypt 对明文密码做不可逆哈希，结果自带随机盐。
func (h *BcryptHasher) Hash(plain string) (string, error) {
	out, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("hash password failed", "err", err)
		return "", err
	}
	return string(out), nil
}

// Compare 校验明文密码与 bcrypt 哈希是否匹配。
func (h *BcryptHasher) Compare(plain, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
