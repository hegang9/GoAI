package jwt

import (
	"GopherAI/auth"
	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/controller"
	"strings"

	"github.com/gin-gonic/gin"
)

// Auth 校验请求中的 JWT，并将解析出的用户名写入 Gin 上下文。
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 复用统一响应结构，生成 token 无效时的返回体。
		res := new(controller.Response)

		var token string
		authHeader := c.GetHeader("Authorization")
		// 优先读取标准请求头：Authorization: Bearer <token>
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// 兼容旧调用方式：允许从 URL query 中读取 token。
			token = c.Query("token")
		}

		// 没有 token 说明请求未认证，直接返回并终止后续处理链。
		if token == "" {
			logger.Error("token is empty")
			c.JSON(code.CodeInvalidToken.HTTPStatus(), res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		// 只记录 token 前缀，避免完整凭证泄露到日志。
		logger.Debug("JWT token received", "token_prefix", token[:min(10, len(token))])
		// 校验 token，并从中解析出当前登录用户。
		userName, ok := auth.ParseToken(token)
		if !ok {
			c.JSON(code.CodeInvalidToken.HTTPStatus(), res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		// 鉴权通过后，把用户名写入 Gin 上下文，供后续 controller 读取。
		c.Set("userName", userName)
		// 放行，让后面的中间件或业务 handler 继续执行。
		c.Next()
	}
}
