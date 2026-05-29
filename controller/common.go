package controller

import (
	"GopherAI/common/code"
	"GopherAI/dto"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 保留以兼容尚未迁移的 controller（阶段 4 清理时移除）。
type Response = dto.Response

// BindJSON 统一 JSON 参数绑定 + 校验失败响应。
// 成功返回 req 和 true，失败时已自动写入错误响应，调用方直接 return。
func BindJSON[T any](c *gin.Context) (T, bool) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, dto.Response{
			StatusCode: code.CodeInvalidParams,
			StatusMsg:  code.CodeInvalidParams.Msg(),
		})
		return req, false
	}
	return req, true
}

// JSON 统一业务结果响应。根据 errCode 决定返回成功数据还是错误信息。
func JSON(c *gin.Context, data any, errCode code.Code) {
	if errCode != code.CodeSuccess {
		c.JSON(http.StatusOK, dto.Response{
			StatusCode: errCode,
			StatusMsg:  errCode.Msg(),
		})
		return
	}
	c.JSON(http.StatusOK, data)
}
