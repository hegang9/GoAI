package jwt

import (
	"GopherAI/auth"
	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/controller"
	"strings"

	"github.com/gin-gonic/gin"
)

// 读取jwt
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		res := new(controller.Response)

		var token string
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// 兼容 URL 参数传 token
			token = c.Query("token")
		}

		if token == "" {
			c.JSON(code.CodeInvalidToken.HTTPStatus(), res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		logger.Debug("JWT token received", "token_prefix", token[:min(10, len(token))])
		userName, ok := auth.ParseToken(token)
		if !ok {
			c.JSON(code.CodeInvalidToken.HTTPStatus(), res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		c.Set("userName", userName)
		c.Next()
	}
}
