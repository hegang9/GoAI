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

// recentSessionRow 是 ListRecent 联表查询的行结构，仅用于 Scan 映射，不对应独立数据表。
type recentSessionRow struct {
	// ID 会话唯一标识（UUID）。
	ID string `gorm:"column:id"`
	// AccountNo 会话归属的内部账号编号。
	AccountNo string `gorm:"column:account_no"`
	// Title 会话标题。
	Title string `gorm:"column:title"`
}

// ListRecent 查询全局最近活跃的会话，用于启动阶段策略 B 的内存预热。
//
// 「活跃」定义为该会话最后一条消息的 created_at（子查询 MAX(created_at)）；
// 仅返回在 messages 表中有记录且 sessions 未软删除的会话；
// 结果按 last_active 降序排列，取前 limit 条。
func (r *SessionRepository) ListRecent(ctx context.Context, limit int) ([]chat.Session, error) {
	if limit <= 0 {
		return []chat.Session{}, nil
	}

	// 子查询：聚合每个 session_id 的最后消息时间，作为活跃度排序依据。
	subQuery := r.db.WithContext(ctx).
		Model(&MessagePO{}).
		Select("session_id, MAX(created_at) AS last_active").
		Group("session_id")

	var rows []recentSessionRow
	if err := r.db.WithContext(ctx).
		Table("(?) AS m", subQuery).
		Joins("INNER JOIN sessions s ON s.id = m.session_id AND s.deleted_at IS NULL").
		Select("s.id, s.account_no, s.title").
		Order("m.last_active DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	sessions := sessionToDomain(rows)
	return sessions, nil
}

func sessionToDomain(pos []recentSessionRow) []chat.Session {
	sess := make([]chat.Session, 0, len(pos))
	for _, po := range pos {
		sess = append(sess, chat.Session{
			ID:        po.ID,
			AccountNo: po.AccountNo,
			Title:     po.Title,
		})
	}
	return sess
}
