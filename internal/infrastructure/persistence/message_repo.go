package persistence

import (
	"context"

	"GopherAI/internal/domain/chat"

	"gorm.io/gorm"
)

// MessageRepository 基于 GORM 实现 domain/chat.MessageRepository 端口。
type MessageRepository struct {
	db *gorm.DB
}

// NewMessageRepository 创建消息仓储。
func NewMessageRepository(db *gorm.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// 编译期断言：MessageRepository 必须满足领域端口。
var _ chat.MessageRepository = (*MessageRepository)(nil)

// Create 持久化一条消息。
func (r *MessageRepository) Create(ctx context.Context, msg chat.Message) error {
	po := MessagePO{
		SessionID: msg.SessionID,
		AccountNo: msg.AccountNo,
		Content:   msg.Content,
		IsUser:    msg.IsUser,
	}
	return r.db.WithContext(ctx).Create(&po).Error
}

// ListAll 读取全部历史消息。
// 以 created_at 升序、id 升序排序：id 为自增主键，作为同一时刻的稳定 tiebreaker，
// 确保回放顺序与插入顺序一致。
func (r *MessageRepository) ListAll(ctx context.Context) ([]chat.Message, error) {
	var pos []MessagePO
	if err := r.db.WithContext(ctx).Order("created_at asc, id asc").Find(&pos).Error; err != nil {
		return nil, err
	}
	msgs := make([]chat.Message, 0, len(pos))
	for _, po := range pos {
		msgs = append(msgs, chat.Message{
			SessionID: po.SessionID,
			AccountNo: po.AccountNo,
			Content:   po.Content,
			IsUser:    po.IsUser,
		})
	}
	return msgs, nil
}
