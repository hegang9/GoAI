package controller

import (
	"GopherAI/converter"
	"GopherAI/dto"
	"GopherAI/service/user"

	"github.com/gin-gonic/gin"
)

func Login(c *gin.Context, req dto.LoginRequest) {
	userBO, errCode := user.Login(req.Username, req.Password)
	JSON(c, converter.UserLoginResponse(userBO), errCode)
}

func Register(c *gin.Context, req dto.RegisterRequest) {
	userBO, errCode := user.Register(req.Email, req.Password, req.Captcha)
	JSON(c, converter.UserRegisterResponse(userBO), errCode)
}

func HandleCaptcha(c *gin.Context, req dto.CaptchaRequest) {
	errCode := user.SendCaptcha(req.Email)
	JSON(c, dto.CaptchaResponse{}, errCode)
}
