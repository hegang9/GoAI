package controller

import (
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"

	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
)

// Login 用户登录。
func (h *Handlers) Login(c *gin.Context, req dto.LoginRequest) {
	token, errCode := h.User.Login(c.Request.Context(), req.Email, req.Password)
	httpx.JSON(c, &dto.LoginResponse{Token: token}, errCode)
}

// Register 用户注册。
func (h *Handlers) Register(c *gin.Context, req dto.RegisterRequest) {
	logger.Info("Register request", "email:", req.Email)
	token, errCode := h.User.Register(c.Request.Context(), req.Email, req.Password, req.Captcha)
	httpx.JSON(c, &dto.RegisterResponse{Token: token}, errCode)
}

// HandleCaptcha 获取验证码。
func (h *Handlers) HandleCaptcha(c *gin.Context, req dto.CaptchaRequest) {
	errCode := h.User.SendCaptcha(c.Request.Context(), req.Email)
	// 验证码接口无业务数据，成功时 data 为 null。
	httpx.JSON(c, nil, errCode)
}
