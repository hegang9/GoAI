package converter

import (
	"GopherAI/bo"
	"GopherAI/dto"
	"GopherAI/model"

	"gorm.io/gorm"
)

// UserModelToBO User模型转业务对象
func UserModelToBO(user *model.User, token string) bo.UserBO {
	setIsDeleted := func(u *model.User) bool {
		if u == nil {
			return false
		}
		return u.DeletedAt.Valid
	}
	return bo.UserBO{
		ID:          user.ID,
		Name:        user.Name,
		Email:       user.Email,
		Username:    user.Username,
		Password:    user.Password,
		CreateTime:  user.CreatedAt,
		UpdateTime:  user.UpdatedAt,
		IsDeleted:   setIsDeleted(user),
		DeletedTime: user.DeletedAt.Time,
		Token:       token,
	}
}

// UserBOToModel 业务对象转User模型
func UserBOToModel(user bo.UserBO) model.User {
	return model.User{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		Username:  user.Username,
		Password:  user.Password,
		CreatedAt: user.CreateTime,
		UpdatedAt: user.UpdateTime,
		DeletedAt: gorm.DeletedAt{Time: user.DeletedTime},
	}
}

// UserLoginResponse 登录响应
func UserLoginResponse(userBO bo.UserBO) dto.LoginResponse {
	return dto.LoginResponse{Token: userBO.Token}
}

// UserRegisterResponse 注册响应
func UserRegisterResponse(userBO bo.UserBO) dto.RegisterResponse {
	return dto.RegisterResponse{Token: userBO.Token}
}
