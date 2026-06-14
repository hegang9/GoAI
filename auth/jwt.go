package auth

import (
	"GopherAI/config"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Claims 定义 JWT payload，包含业务字段和标准注册声明。
type Claims struct {
	ID                   int64  `json:"id"`
	Username             string `json:"username"`
	jwt.RegisteredClaims        // JWT 标准字段
}

// GenerateToken 根据Claims生成一个携带用户信息的 HS256 JWT。
func GenerateToken(id int64, username string) (string, error) {
	now := time.Now()
	claims := Claims{
		ID:       id,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			// 令牌过期时间，超过后解析会失败。
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(config.GetConfig().ExpireDuration) * time.Hour)),
			// Issuer 和 Subject 用于标记这张 token 是谁签的、用于什么场景。
			Issuer:   config.GetConfig().Issuer,
			Subject:  config.GetConfig().Subject,
			IssuedAt: jwt.NewNumericDate(now),
		},
	}

	// 使用配置中的密钥按 HS256 算法签名。
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.GetConfig().Key))
}

// ParseToken 校验 token 并返回其中保存的用户名。
func ParseToken(token string) (string, bool) {
	claims := new(Claims)
	t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(config.GetConfig().Key), nil
	})
	if err != nil || t == nil || !t.Valid {
		return "", false
	}
	return claims.Username, true
}
