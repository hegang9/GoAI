// common.go 公共控制器，包含通用的HTTP逻辑处理函数

package controller

import (
	"fmt"

	"GopherAI/common/code"
	"GopherAI/common/logger"
	"GopherAI/dto"

	"github.com/gin-gonic/gin"
)

// Response 保留以兼容尚未迁移的 controller（阶段 4 清理时移除）。
type Response = dto.Response

// BindJSON 统一 JSON 参数绑定 + 校验请求参数，失败时保持现有 JSON 错误响应行为。
// 成功返回 req 和 nil，失败时已自动写入错误响应并中止 Gin 处理链，调用方直接 return。
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

// JSON 统一业务结果响应。根据 errCode 决定返回成功数据还是错误信息。
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
