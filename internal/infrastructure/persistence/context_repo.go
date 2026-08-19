package persistence

import (
	"context"
	"errors"

	"GopherAI/internal/domain/chat"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ContextRepository 用 MySQL 保存会话摘要和核心记忆。
// 它只实现派生快照的读写，不参与原始消息的追加和展示。
type ContextRepository struct {
	db *gorm.DB
}

// NewContextRepository 创建会话上下文仓储。
func NewContextRepository(db *gorm.DB) *ContextRepository {
	return &ContextRepository{db: db}
}

var _ chat.ContextRepository = (*ContextRepository)(nil)

// Get 按 account_no + session_id 读取快照，保证租户与会话双重隔离。
func (r *ContextRepository) Get(ctx context.Context, accountNo, sessionID string) (chat.ContextSnapshot, bool, error) {
	var po ConversationContextPO
	err := r.db.WithContext(ctx).
		Where("account_no = ? AND session_id = ?", accountNo, sessionID).
		First(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return chat.ContextSnapshot{}, false, nil
	}
	if err != nil {
		return chat.ContextSnapshot{}, false, err
	}
	return contextToDomain(po), true, nil
}

// Save 以会话为单位覆盖派生快照，并原子递增 version。
// 当前 Conversation 已在进程内串行化完整 turn；多实例并发控制后续可升级为带期望版本的 CAS。
func (r *ContextRepository) Save(ctx context.Context, snapshot chat.ContextSnapshot) error {
	po := ConversationContextPO{
		SessionID:        snapshot.SessionID,
		AccountNo:        snapshot.AccountNo,
		CoreMemory:       snapshot.CoreMemory,
		Summary:          snapshot.Summary,
		CoveredMessageID: snapshot.CoveredMessageID,
		Version:          1,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"account_no":         snapshot.AccountNo,
			"core_memory":        snapshot.CoreMemory,
			"summary":            snapshot.Summary,
			"covered_message_id": snapshot.CoveredMessageID,
			"version":            gorm.Expr("version + 1"),
			"updated_at":         gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}).Create(&po).Error
}

func contextToDomain(po ConversationContextPO) chat.ContextSnapshot {
	return chat.ContextSnapshot{
		AccountNo:        po.AccountNo,
		SessionID:        po.SessionID,
		CoreMemory:       po.CoreMemory,
		Summary:          po.Summary,
		CoveredMessageID: po.CoveredMessageID,
		Version:          po.Version,
	}
}
