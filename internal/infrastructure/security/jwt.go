package security

import (
	"time"

	domainuser "GopherAI/internal/domain/user"

	"github.com/golang-jwt/jwt/v4"
)

// JWTConfig 描述签发/校验 JWT 所需的参数，由 bootstrap 从应用配置注入。
// 相比旧实现直接读取全局 config，这里改为显式注入，便于测试与替换密钥来源。
type JWTConfig struct {
	Key            string
	Issuer         string
	Subject        string
	ExpireDuration int // 小时
}

// claims 定义 JWT payload。
type claims struct {
	ID        int64  `json:"id"`
	AccountNo string `json:"account_no"`
	jwt.RegisteredClaims
}

// JWTIssuer 基于 HS256 实现 domain/user.TokenIssuer 端口。
type JWTIssuer struct {
	cfg JWTConfig
}

// NewJWTIssuer 创建 JWT 令牌签发器。
func NewJWTIssuer(cfg JWTConfig) *JWTIssuer {
	return &JWTIssuer{cfg: cfg}
}

// 编译期断言：JWTIssuer 必须满足领域端口。
var _ domainuser.TokenIssuer = (*JWTIssuer)(nil)

// Issue 为指定用户签发携带账号编号的 HS256 JWT。
func (j *JWTIssuer) Issue(id int64, accountNo string) (string, error) {
	now := time.Now()
	c := claims{
		ID:        id,
		AccountNo: accountNo,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(j.cfg.ExpireDuration) * time.Hour)),
			Issuer:    j.cfg.Issuer,
			Subject:   j.cfg.Subject,
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString([]byte(j.cfg.Key))
}

// Parse 校验 token 并返回其中保存的账号编号。
func (j *JWTIssuer) Parse(token string) (string, bool) {
	c := new(claims)
	t, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (interface{}, error) {
		return []byte(j.cfg.Key), nil
	})
	if err != nil || t == nil || !t.Valid {
		return "", false
	}
	return c.AccountNo, true
}
