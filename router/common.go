package router

import (
	"GopherAI/controller"

	"github.com/gin-gonic/gin"
)

// Handler 为 router 包提供简短包装函数，内部复用 controller 层的 JSON 绑定包装器。
func Handler[T any](fn func(c *gin.Context, req T)) gin.HandlerFunc {
	return controller.Handler(fn)
}
