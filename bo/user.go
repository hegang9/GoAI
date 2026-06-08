package bo

import (
	"time"
)

type UserBO struct {
	ID          int64
	Name        string
	Email       string
	Username    string
	Password    string
	CreateTime  time.Time
	UpdateTime  time.Time
	IsDeleted   bool
	DeletedTime time.Time
	Token       string // 登录token
}
