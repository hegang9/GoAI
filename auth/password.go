package auth

import (
	"GopherAI/common/logger"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword 使用 bcrypt 算法对用户明文密码进行不可逆哈希。
// 返回值可安全存入数据库；bcrypt 会自动生成随机盐并写入哈希结果中。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("hash password failed", "err", err)
		return "", err
	}
	return string(hash), nil
}

// CheckPasswordHash 校验用户输入的明文密码是否匹配数据库中的 bcrypt 哈希值。
// 匹配成功返回 true；密码错误或哈希格式非法时均返回 false。
func CheckPasswordHash(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
