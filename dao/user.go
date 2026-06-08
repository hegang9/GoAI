package dao

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
	"GopherAI/utils"

	"gorm.io/gorm"
)

const (
	CodeMsg     = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	UserNameMsg = "GopherAI的账号如下，请保留好，后续可以用账号进行登录 "
)

func getUserByUsername(username string) (*model.User, error) {
	user := new(model.User)
	err := mysql.DB.Where("username = ?", username).First(user).Error
	return user, err
}

func createUser(user *model.User) (*model.User, error) {
	err := mysql.DB.Create(user).Error
	return user, err
}

func IsExistUser(username string) (bool, *model.User) {
	user, err := getUserByUsername(username)
	if err == gorm.ErrRecordNotFound || user == nil {
		return false, nil
	}
	return true, user
}

func Register(username, email, password string) (*model.User, bool) {
	passwordHash, err := utils.HashPassword(password)
	if err != nil {
		return nil, false
	}

	user, err := createUser(&model.User{
		Email:    email,
		Name:     username,
		Username: username,
		Password: passwordHash,
	})
	if err != nil {
		return nil, false
	}

	return user, true
}
