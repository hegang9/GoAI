package user

import (
	"GopherAI/controller"
	"GopherAI/converter"
	"GopherAI/dto"
	"GopherAI/service/user"

	"github.com/gin-gonic/gin"
)

func Login(c *gin.Context) {
	req, ok := controller.BindJSON[dto.LoginRequest](c)
	if !ok {
		return
	}

	userBO, errCode := user.Login(req.Username, req.Password)
	controller.JSON(c, converter.UserBOToLoginResponse(userBO), errCode)
}

func Register(c *gin.Context) {
	req, ok := controller.BindJSON[dto.RegisterRequest](c)
	if !ok {
		return
	}

	userBO, errCode := user.Register(req.Email, req.Password, req.Captcha)
	controller.JSON(c, converter.UserBOToRegisterResponse(userBO), errCode)
}

func HandleCaptcha(c *gin.Context) {
	req, ok := controller.BindJSON[dto.CaptchaRequest](c)
	if !ok {
		return
	}

	errCode := user.SendCaptcha(req.Email)
	controller.JSON(c, dto.CaptchaResponse{}, errCode)
}
