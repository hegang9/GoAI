package persistence

import (
	"context"
	"errors"

	domainuser "GopherAI/internal/domain/user"

	"gorm.io/gorm"
)

// UserRepository 基于 GORM 实现 domain/user.Repository 端口。
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户仓储。
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// 编译期断言：UserRepository 必须满足领域端口。
var _ domainuser.Repository = (*UserRepository)(nil)

// FindByEmail 按邮箱查询用户，未找到时返回 domain 层的 ErrNotFound。
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domainuser.User, error) {
	var po UserPO
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainuser.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userToDomain(&po), nil
}

// FindByAccountNo 按内部账号编号查询用户，未找到时返回 ErrNotFound。
func (r *UserRepository) FindByAccountNo(ctx context.Context, accountNo string) (*domainuser.User, error) {
	var po UserPO
	err := r.db.WithContext(ctx).Where("account_no = ?", accountNo).First(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainuser.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userToDomain(&po), nil
}

// Create 创建用户记录并回填自增 ID 等字段。
func (r *UserRepository) Create(ctx context.Context, u *domainuser.User) (*domainuser.User, error) {
	po := UserPO{
		Name:      u.Name,
		Email:     u.Email,
		AccountNo: u.AccountNo,
		Password:  u.PasswordHash,
	}
	if err := r.db.WithContext(ctx).Create(&po).Error; err != nil {
		return nil, err
	}
	return userToDomain(&po), nil
}

// userToDomain 把用户 PO 转换为领域实体。
func userToDomain(po *UserPO) *domainuser.User {
	return &domainuser.User{
		ID:           po.ID,
		Name:         po.Name,
		Email:        po.Email,
		AccountNo:    po.AccountNo,
		PasswordHash: po.Password,
		CreatedAt:    po.CreatedAt,
		UpdatedAt:    po.UpdatedAt,
	}
}
