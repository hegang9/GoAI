// Package httpx 提供 HTTP 接口层通用的请求绑定与响应封装工具。
//
// 它被 controller（写响应）与 router（绑定包装器）复用，自身只依赖 dto 与 pkg，
// 避免 controller 与 router 之间产生循环依赖。
package httpx

import (
	"fmt"

	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
)

// BindJSON 统一 JSON 参数绑定与校验，失败时写入错误响应并中止处理链。
func BindJSON[T any](c *gin.Context) (T, error) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(code.CodeInvalidParams.HTTPStatus(), dto.Response{
			StatusCode: code.CodeInvalidParams,
			StatusMsg:  code.CodeInvalidParams.Msg(),
		})
		return req, err
	}
	return req, nil
}

// Handler 为需要 JSON 绑定的路由提供通用包装，避免重复处理参数绑定错误。
func Handler[T any](fn func(c *gin.Context, req T)) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, err := BindJSON[T](c)
		if err != nil {
			logger.Error("BindJSON failed",
				"type", fmt.Sprintf("%T", req),
				"err", err,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"contentType", c.ContentType(),
				"clientIP", c.ClientIP(),
			)
			return
		}
		fn(c, req)
	}
}

// JSON 统一业务结果响应：成功返回数据，失败返回错误码与消息。
func JSON(c *gin.Context, data any, errCode code.Code) {
	if errCode != code.CodeSuccess {
		c.JSON(errCode.HTTPStatus(), dto.Response{
			StatusCode: errCode,
			StatusMsg:  errCode.Msg(),
		})
		return
	}
	c.JSON(errCode.HTTPStatus(), data)
}
