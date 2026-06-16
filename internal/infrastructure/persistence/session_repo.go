package persistence

import (
	"context"

	"GopherAI/internal/domain/chat"

	"gorm.io/gorm"
)

// SessionRepository 基于 GORM 实现 domain/chat.SessionRepository 端口。
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 创建会话仓储。
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// 编译期断言：SessionRepository 必须满足领域端口。
var _ chat.SessionRepository = (*SessionRepository)(nil)

// Create 持久化创建一条会话。
func (r *SessionRepository) Create(ctx context.Context, s chat.Session) (chat.Session, error) {
	po := SessionPO{
		ID:        s.ID,
		AccountNo: s.AccountNo,
		Title:     s.Title,
	}
	if err := r.db.WithContext(ctx).Create(&po).Error; err != nil {
		return chat.Session{}, err
	}
	return chat.Session{ID: po.ID, AccountNo: po.AccountNo, Title: po.Title}, nil
}

// ListByAccount 查询指定账号的全部会话。
func (r *SessionRepository) ListByAccount(ctx context.Context, accountNo string) ([]chat.Session, error) {
	var pos []SessionPO
	if err := r.db.WithContext(ctx).Where("account_no = ?", accountNo).Find(&pos).Error; err != nil {
		return nil, err
	}
	sessions := make([]chat.Session, 0, len(pos))
	for _, po := range pos {
		sessions = append(sessions, chat.Session{ID: po.ID, AccountNo: po.AccountNo, Title: po.Title})
	}
	return sessions, nil
}
