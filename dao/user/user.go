package user

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
	"GopherAI/utils"
	"context"

	"gorm.io/gorm"
)

const (
	CodeMsg     = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	UserNameMsg = "GopherAI的账号如下，请保留好，后续可以用账号进行登录 "
)

var ctx = context.Background()

// getUserByUsername 根据用户名查询用户记录。
func getUserByUsername(username string) (*model.User, error) {
	user := new(model.User)
	err := mysql.DB.Where("username = ?", username).First(user).Error
	return user, err
}

// insertUser 向 users 表插入一条新用户记录。
func insertUser(user *model.User) (*model.User, error) {
	err := mysql.DB.Create(user).Error
	return user, err
}

// 这边只能通过账号进行登录
func IsExistUser(username string) (bool, *model.User) {

	user, err := getUserByUsername(username)

	if err == gorm.ErrRecordNotFound || user == nil {
		return false, nil
	}

	return true, user
}

func Register(username, email, password string) (*model.User, bool) {
	if user, err := insertUser(&model.User{
		Email:    email,
		Name:     username,
		Username: username,
		Password: utils.MD5(password),
	}); err != nil {
		return nil, false
	} else {
		return user, true
	}
}
