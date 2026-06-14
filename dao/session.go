package dao

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
)

func GetSessionsByAccountNo(accountNo string) ([]model.Session, error) {
	var sessions []model.Session
	err := mysql.DB.Where("account_no = ?", accountNo).Find(&sessions).Error
	return sessions, err
}

func CreateSession(session *model.Session) (*model.Session, error) {
	err := mysql.DB.Create(session).Error
	return session, err
}

func GetSessionByID(sessionID string) (*model.Session, error) {
	var session model.Session
	err := mysql.DB.Where("id = ?", sessionID).First(&session).Error
	return &session, err
}
