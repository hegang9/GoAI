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
		c.AbortWithStatusJSON(code.CodeInvalidParams.HTTPStatus(), dto.NewResponse(code.CodeInvalidParams, nil))
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

// JSON 以统一信封输出业务结果：成功与失败走同一套构建逻辑，
// 仅业务数据来源不同——成功时 data 为业务数据，失败时无业务数据故 data 为 null。
func JSON(c *gin.Context, data any, errCode code.Code) {
	// 错误响应不携带业务数据，统一回填 data 为 null，保证响应结构与成功分支一致。
	if errCode != code.CodeSuccess {
		data = nil
	}
	c.JSON(errCode.HTTPStatus(), dto.NewResponse(errCode, data))
}
