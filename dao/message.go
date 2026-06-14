package dao

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
)

func GetMessagesBySessionID(sessionID string) ([]model.Message, error) {
	var msgs []model.Message
	// 以 created_at 升序、id 升序排序：id 为自增主键、与插入顺序单调一致，
	// 作为同一时刻（毫秒级 created_at 可能相同）下的稳定 tiebreaker，避免消息顺序错乱。
	err := mysql.DB.Where("session_id = ?", sessionID).Order("created_at asc, id asc").Find(&msgs).Error
	return msgs, err
}

func GetMessagesBySessionIDs(sessionIDs []string) ([]model.Message, error) {
	var msgs []model.Message
	if len(sessionIDs) == 0 {
		return msgs, nil
	}
	err := mysql.DB.Where("session_id IN ?", sessionIDs).Order("created_at asc, id asc").Find(&msgs).Error
	return msgs, err
}

func CreateMessage(message *model.Message) (*model.Message, error) {
	err := mysql.DB.Create(message).Error
	return message, err
}

// GetAllMessages 从数据库读取所有历史消息。
// 按 created_at 升序、id 升序排序：启动回放依赖该顺序重建会话上下文，
// 追加 id 作为稳定 tiebreaker，确保同一会话内用户/AI 消息顺序与插入顺序一致。
func GetAllMessages() ([]model.Message, error) {
	var msgs []model.Message
	err := mysql.DB.Order("created_at asc, id asc").Find(&msgs).Error
	return msgs, err
}
