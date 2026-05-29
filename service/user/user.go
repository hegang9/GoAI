package user

import (
	"GopherAI/bo"
	"GopherAI/common/code"
	myemail "GopherAI/common/email"
	myredis "GopherAI/common/redis"
	"GopherAI/converter"
	"GopherAI/dao/user"
	"GopherAI/model"
	"GopherAI/utils"
	"GopherAI/utils/myjwt"
)

func Login(username, password string) (bo.UserBO, code.Code) {
	var userInformation *model.User
	var ok bool

	if ok, userInformation = user.IsExistUser(username); !ok {
		return bo.UserBO{}, code.CodeUserNotExist
	}

	if userInformation.Password != utils.MD5(password) {
		return bo.UserBO{}, code.CodeInvalidPassword
	}

	token, err := myjwt.GenerateToken(userInformation.ID, userInformation.Username)
	if err != nil {
		return bo.UserBO{}, code.CodeServerBusy
	}

	return converter.UserModelToBO(userInformation, token), code.CodeSuccess
}

func Register(email, password, captcha string) (bo.UserBO, code.Code) {
	var ok bool
	var userInformation *model.User

	if ok, _ := user.IsExistUser(email); ok {
		return bo.UserBO{}, code.CodeUserExist
	}

	if ok, _ := myredis.CheckCaptchaForEmail(email, captcha); !ok {
		return bo.UserBO{}, code.CodeInvalidCaptcha
	}

	username := utils.GetRandomNumbers(11)

	if userInformation, ok = user.Register(username, email, password); !ok {
		return bo.UserBO{}, code.CodeServerBusy
	}

	if err := myemail.SendCaptcha(email, username, user.UserNameMsg); err != nil {
		return bo.UserBO{}, code.CodeServerBusy
	}

	token, err := myjwt.GenerateToken(userInformation.ID, userInformation.Username)
	if err != nil {
		return bo.UserBO{}, code.CodeServerBusy
	}

	return converter.UserModelToBO(userInformation, token), code.CodeSuccess
}

func SendCaptcha(email_ string) code.Code {
	send_code := utils.GetRandomNumbers(6)

	if err := myredis.SetCaptchaForEmail(email_, send_code); err != nil {
		return code.CodeServerBusy
	}

	if err := myemail.SendCaptcha(email_, send_code, myemail.CodeMsg); err != nil {
		return code.CodeServerBusy
	}

	return code.CodeSuccess
}
