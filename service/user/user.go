package user

import (
	"GopherAI/auth"
	"GopherAI/bo"
	"GopherAI/common/code"
	myemail "GopherAI/common/email"
	"GopherAI/common/logger"
	myredis "GopherAI/common/redis"
	"GopherAI/converter"
	"GopherAI/dao"
	"GopherAI/model"
	"GopherAI/random"
)

// Login 使用邮箱和密码完成登录认证，认证通过后仍使用 AccountNo 签发内部身份 token。
func Login(email, password string) (bo.UserBO, code.Code) {
	var userInformation *model.User
	var ok bool

	// 检查邮箱是否属于已注册用户，邮箱是用户登录凭证。
	if userInformation, ok = dao.IsExistUserByEmail(email); !ok {
		logger.Warn("Login email not found", "email", email)
		return bo.UserBO{}, code.CodeUserNotExist
	}

	if !auth.CheckPasswordHash(password, userInformation.Password) {
		logger.Warn("Login invalid password", "email", email)
		return bo.UserBO{}, code.CodeInvalidPassword
	}

	// AccountNo 是系统内部账号编号，用于 JWT、会话归属和文件隔离，不再作为登录输入。
	token, err := auth.GenerateToken(userInformation.ID, userInformation.AccountNo)
	if err != nil {
		logger.Error("Login generate token failed", "email", email, "accountNo", userInformation.AccountNo, "err", err)
		return bo.UserBO{}, code.CodeServerBusy
	}

	return converter.UserModelToBO(userInformation, token), code.CodeSuccess
}

// Register 使用邮箱验证码注册用户，并生成唯一内部账号编号。
func Register(email, password, captcha string) (bo.UserBO, code.Code) {
	var ok bool
	var userInformation *model.User

	if _, ok := dao.IsExistUserByEmail(email); ok {
		logger.Warn("Register email already exists", "email", email)
		return bo.UserBO{}, code.CodeUserExist
	}

	if ok, _ := myredis.CheckCaptchaForEmail(email, captcha); !ok {
		logger.Warn("Register invalid captcha", "email", email)
		return bo.UserBO{}, code.CodeInvalidCaptcha
	}

	// accountNo 是系统内部账号编号，与可重复昵称 Name 区分开。
	accountNo := random.GetRandomNumbers(11)

	if userInformation, ok = dao.Register(accountNo, email, password); !ok {
		return bo.UserBO{}, code.CodeServerBusy
	}

	if err := myemail.SendCaptcha(email, accountNo, dao.AccountNoMsg); err != nil {
		logger.Error("Register send account no email failed", "email", email, "accountNo", accountNo, "err", err)
		return bo.UserBO{}, code.CodeServerBusy
	}

	token, err := auth.GenerateToken(userInformation.ID, userInformation.AccountNo)
	if err != nil {
		return bo.UserBO{}, code.CodeServerBusy
	}

	return converter.UserModelToBO(userInformation, token), code.CodeSuccess
}

func SendCaptcha(email_ string) code.Code {
	send_code := random.GetRandomNumbers(6)

	if err := myredis.SetCaptchaForEmail(email_, send_code); err != nil {
		return code.CodeServerBusy
	}

	if err := myemail.SendCaptcha(email_, send_code, myemail.CodeMsg); err != nil {
		return code.CodeServerBusy
	}

	return code.CodeSuccess
}
