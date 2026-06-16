package controller

import (
	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"

	"github.com/gin-gonic/gin"
)

// Login 用户登录。
func (h *Handlers) Login(c *gin.Context, req dto.LoginRequest) {
	token, errCode := h.User.Login(c.Request.Context(), req.Email, req.Password)
	httpx.JSON(c, dto.LoginResponse{Token: token}, errCode)
}

// Register 用户注册。
func (h *Handlers) Register(c *gin.Context, req dto.RegisterRequest) {
	token, errCode := h.User.Register(c.Request.Context(), req.Email, req.Password, req.Captcha)
	httpx.JSON(c, dto.RegisterResponse{Token: token}, errCode)
}

// HandleCaptcha 获取验证码。
func (h *Handlers) HandleCaptcha(c *gin.Context, req dto.CaptchaRequest) {
	errCode := h.User.SendCaptcha(c.Request.Context(), req.Email)
	httpx.JSON(c, dto.CaptchaResponse{}, errCode)
}
