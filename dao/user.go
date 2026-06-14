package dao

import (
	"GopherAI/auth"
	"GopherAI/common/logger"
	"GopherAI/common/mysql"
	"GopherAI/model"
	"errors"

	"gorm.io/gorm"
)

const (
	CodeMsg      = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	AccountNoMsg = "GopherAI的内部账号编号如下，仅用于账号识别和问题排查，请妥善保管 "
)

const defaultNickname = "GopherAI用户"

func getUserByEmail(email string) (*model.User, error) {
	user := new(model.User)
	err := mysql.DB.Where("email=?", email).First(user).Error
	return user, err
}

func IsExistUserByEmail(email string) (*model.User, bool) {
	user, err := getUserByEmail(email)
	if errors.Is(err, gorm.ErrRecordNotFound) || user == nil {
		logger.Warn("email not found", "email", email)
		return nil, false
	}
	if err != nil {
		logger.Error("IsExistUserByEmail failed", "email", email, "err", err)
		return nil, false
	}
	return user, true
}

// getUserByAccountNo 根据内部账号编号查询用户。
func getUserByAccountNo(accountNo string) (*model.User, error) {
	user := new(model.User)
	err := mysql.DB.Where("account_no = ?", accountNo).First(user).Error
	return user, err
}

func createUser(user *model.User) (*model.User, error) {
	err := mysql.DB.Create(user).Error
	return user, err
}

// IsExistUserByAccountNo 判断内部账号编号是否已存在。
func IsExistUserByAccountNo(accountNo string) (*model.User, bool) {
	user, err := getUserByAccountNo(accountNo)
	if errors.Is(err, gorm.ErrRecordNotFound) || user == nil {
		logger.Warn("account no not found", "accountNo", accountNo)
		return nil, false
	}
	if err != nil {
		logger.Error("IsExistUserByAccountNo failed", "accountNo", accountNo, "err", err)
		return nil, false
	}
	return user, true
}

// Register 创建用户，Name 仅作为可重复昵称，AccountNo 才是唯一内部账号编号。
func Register(accountNo, email, password string) (*model.User, bool) {
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		logger.Error("Register hash password failed", "email", email, "err", err)
		return nil, false
	}

	user, err := createUser(&model.User{
		Email:     email,
		Name:      defaultNickname,
		AccountNo: accountNo,
		Password:  passwordHash,
	})
	if err != nil {
		logger.Error("Register create user failed", "email", email, "accountNo", accountNo, "err", err)
		return nil, false
	}

	return user, true
}
