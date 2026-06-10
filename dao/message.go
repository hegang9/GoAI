package dao

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
)

func GetMessagesBySessionID(sessionID string) ([]model.Message, error) {
	var msgs []model.Message
	err := mysql.DB.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error
	return msgs, err
}

func GetMessagesBySessionIDs(sessionIDs []string) ([]model.Message, error) {
	var msgs []model.Message
	if len(sessionIDs) == 0 {
		return msgs, nil
	}
	err := mysql.DB.Where("session_id IN ?", sessionIDs).Order("created_at asc").Find(&msgs).Error
	return msgs, err
}

func CreateMessage(message *model.Message) (*model.Message, error) {
	err := mysql.DB.Create(message).Error
	return message, err
}

// GetAllMessages 从数据库读取所有历史消息
func GetAllMessages() ([]model.Message, error) {
	var msgs []model.Message
	// 用 GORM 从数据库查询 message 表里的所有记录，按创建时间升序排序，查到的结果放进 msgs 切片，最后把执行错误取出来赋给 err
	err := mysql.DB.Order("created_at asc").Find(&msgs).Error
	return msgs, err
}
