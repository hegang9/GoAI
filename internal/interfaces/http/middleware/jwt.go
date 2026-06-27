// Package middleware 提供 HTTP 接口层的中间件。
//
// JWT 鉴权中间件依赖 domain/user.TokenIssuer 端口校验令牌，不再反向依赖 controller，
// 从而消除「中间件 -> controller」的反向依赖。
package middleware

import (
	"strings"

	domainuser "GopherAI/internal/domain/user"
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
)

// JWTAuth 返回校验 JWT 的中间件：解析令牌后把账号编号写入 Gin 上下文。
// issuer 为令牌校验端口，由 bootstrap 注入具体实现。
func JWTAuth(issuer domainuser.TokenIssuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string
		authHeader := c.GetHeader("Authorization")
		// 优先读取标准请求头：Authorization: Bearer <token>
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// 兼容旧调用方式：允许从 URL query 中读取 token。
			token = c.Query("token")
		}

		if token == "" {
			logger.Error("token is empty")
			c.JSON(code.CodeInvalidToken.HTTPStatus(), dto.NewResponse(code.CodeInvalidToken, nil))
			c.Abort()
			return
		}

		// 只记录 token 前缀，避免完整凭证泄露到日志。
		logger.Debug("JWT token received", "token_prefix", token[:min(10, len(token))])
		accountNo, ok := issuer.Parse(token)
		if !ok {
			c.JSON(code.CodeInvalidToken.HTTPStatus(), dto.NewResponse(code.CodeInvalidToken, nil))
			c.Abort()
			return
		}

		// 鉴权通过后写入账号编号，供后续 controller 读取。
		c.Set("accountNo", accountNo)
		c.Next()
	}
}
