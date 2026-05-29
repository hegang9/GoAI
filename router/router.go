package router

import (
	"GopherAI/common/code"
	"GopherAI/dto"
	"GopherAI/middleware/jwt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {
	// 创建gin引擎，自带Logger + Recovery 中间件
	r := gin.Default()
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, dto.Response{
			StatusCode: code.CodeRecordNotFound,
			StatusMsg:  http.StatusText(http.StatusNotFound),
		})
	})
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, dto.Response{
			StatusCode: code.CodeInvalidParams,
			StatusMsg:  http.StatusText(http.StatusMethodNotAllowed),
		})
	})

	v1 := r.Group("/api/v1")

	// 公开路由
	RegisterUserRouter(v1.Group("/user"))

	// JWT 鉴权路由组 — 中间件只挂载一次，所有子组自动继承
	auth := v1.Group("")
	auth.Use(jwt.Auth())
	{
		RegisterAIRouter(auth.Group("/ai"))
		RegisterImageRouter(auth.Group("/image"))
		RegisterFileRouter(auth.Group("/file"))
	}

	return r
}
