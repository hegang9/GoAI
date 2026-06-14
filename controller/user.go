package controller

import (
	"GopherAI/converter"
	"GopherAI/dto"
	"GopherAI/service/user"

	"github.com/gin-gonic/gin"
)

// Login 用户登录
func Login(c *gin.Context, req dto.LoginRequest) {
	userBO, errCode := user.Login(req.Email, req.Password)
	JSON(c, converter.UserLoginResponse(userBO), errCode)
}

// Register 用户注册
func Register(c *gin.Context, req dto.RegisterRequest) {
	userBO, errCode := user.Register(req.Email, req.Password, req.Captcha)
	JSON(c, converter.UserRegisterResponse(userBO), errCode)
}

// HandleCaptcha 获取验证码
func HandleCaptcha(c *gin.Context, req dto.CaptchaRequest) {
	errCode := user.SendCaptcha(req.Email)
	JSON(c, dto.CaptchaResponse{}, errCode)
}
