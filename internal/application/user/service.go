// Package user 是用户应用服务：编排登录、注册、发送验证码等用例。
//
// 它依赖领域端口（user.Repository / PasswordHasher / TokenIssuer / CaptchaStore / Mailer），
// 由 bootstrap 注入具体实现，自身不感知数据库、Redis、SMTP 等基础设施细节。
package user

import (
	"context"
	"errors"
	"sync"

	domainuser "GopherAI/internal/domain/user"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"
	"GopherAI/pkg/random"
)

const (
	// defaultNickname 注册时的默认昵称。
	defaultNickname = "GopherAI用户"
	// maxAccountNoGenerateRetry 生成唯一账号编号的最大重试次数。
	maxAccountNoGenerateRetry = 5

	// captchaPrefix 验证码邮件正文前缀。
	captchaPrefix = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	// accountNoPrefix 账号编号邮件正文前缀。
	accountNoPrefix = "GopherAI的内部账号编号如下，仅用于账号识别和问题排查，请妥善保管 "
)

// Service 用户应用服务。
type Service struct {
	repo    domainuser.Repository
	hasher  domainuser.PasswordHasher
	issuer  domainuser.TokenIssuer
	captcha domainuser.CaptchaStore
	mailer  domainuser.Mailer

	// accountNoCache 进程内缓存已占用/预占的账号编号，减少注册重试的数据库查询。
	accountNoCache map[string]struct{}
	accountNoMu    sync.RWMutex
}

// NewService 创建用户应用服务。
func NewService(
	repo domainuser.Repository,
	hasher domainuser.PasswordHasher,
	issuer domainuser.TokenIssuer,
	captcha domainuser.CaptchaStore,
	mailer domainuser.Mailer,
) *Service {
	return &Service{
		repo:           repo,
		hasher:         hasher,
		issuer:         issuer,
		captcha:        captcha,
		mailer:         mailer,
		accountNoCache: make(map[string]struct{}),
	}
}

// Login 使用邮箱和密码完成登录认证，成功后以 AccountNo 签发内部身份 token。
func (s *Service) Login(ctx context.Context, email, password string) (string, code.Code) {
	u, err := s.repo.FindByEmail(ctx, email)
	if errors.Is(err, domainuser.ErrNotFound) {
		logger.Warn("Login email not found", "email", email)
		return "", code.CodeUserNotExist
	}
	if err != nil {
		logger.Error("Login FindByEmail failed", "email", email, "err", err)
		return "", code.CodeServerBusy
	}

	if !s.hasher.Compare(password, u.PasswordHash) {
		logger.Warn("Login invalid password", "email", email)
		return "", code.CodeInvalidPassword
	}

	token, err := s.issuer.Issue(u.ID, u.AccountNo)
	if err != nil {
		logger.Error("Login issue token failed", "email", email, "accountNo", u.AccountNo, "err", err)
		return "", code.CodeServerBusy
	}
	return token, code.CodeSuccess
}

// Register 使用邮箱验证码注册用户，并生成唯一内部账号编号。
func (s *Service) Register(ctx context.Context, email, password, captcha string) (string, code.Code) {
	// 邮箱是否已注册。
	if _, err := s.repo.FindByEmail(ctx, email); err == nil {
		logger.Warn("Register email already exists", "email", email)
		return "", code.CodeUserExist
	} else if !errors.Is(err, domainuser.ErrNotFound) {
		logger.Error("Register FindByEmail failed", "email", email, "err", err)
		return "", code.CodeServerBusy
	}

	// 校验邮箱验证码。
	if ok, _ := s.captcha.Check(ctx, email, captcha); !ok {
		logger.Warn("Register invalid captcha", "email", email)
		return "", code.CodeInvalidCaptcha
	}

	// 生成唯一内部账号编号。
	accountNo, ok := s.generateUniqueAccountNo(ctx)
	if !ok {
		return "", code.CodeServerBusy
	}

	// 哈希密码并落库。
	hash, err := s.hasher.Hash(password)
	if err != nil {
		logger.Error("Register hash password failed", "email", email, "err", err)
		s.uncacheAccountNo(accountNo)
		return "", code.CodeServerBusy
	}
	created, err := s.repo.Create(ctx, &domainuser.User{
		Email:        email,
		Name:         defaultNickname,
		AccountNo:    accountNo,
		PasswordHash: hash,
	})
	if err != nil {
		logger.Error("Register create user failed", "email", email, "accountNo", accountNo, "err", err)
		s.uncacheAccountNo(accountNo)
		return "", code.CodeServerBusy
	}

	// 发送账号编号邮件。
	if err := s.mailer.Send(email, accountNo, accountNoPrefix); err != nil {
		logger.Error("Register send account no email failed", "email", email, "accountNo", accountNo, "err", err)
		return "", code.CodeServerBusy
	}

	token, err := s.issuer.Issue(created.ID, created.AccountNo)
	if err != nil {
		logger.Error("Register issue token failed", "email", email, "err", err)
		return "", code.CodeServerBusy
	}
	return token, code.CodeSuccess
}

// SendCaptcha 生成并发送邮箱验证码，同时写入缓存供注册校验。
func (s *Service) SendCaptcha(ctx context.Context, email string) code.Code {
	sendCode := random.GetRandomNumbers(6)
	if err := s.captcha.Set(ctx, email, sendCode); err != nil {
		logger.Error("SendCaptcha set failed", "email", email, "err", err)
		return code.CodeServerBusy
	}
	if err := s.mailer.Send(email, sendCode, captchaPrefix); err != nil {
		logger.Error("SendCaptcha send mail failed", "email", email, "err", err)
		return code.CodeServerBusy
	}
	return code.CodeSuccess
}

// generateUniqueAccountNo 生成唯一内部账号编号，避免随机数偶发冲突导致注册失败。
func (s *Service) generateUniqueAccountNo(ctx context.Context) (string, bool) {
	for retry := 1; retry <= maxAccountNoGenerateRetry; retry++ {
		accountNo := random.GetRandomNumbers(11)
		if s.isAccountNoCached(accountNo) {
			logger.Warn("Register account no cache collision", "accountNo", accountNo, "retry", retry)
			continue
		}

		if _, err := s.repo.FindByAccountNo(ctx, accountNo); errors.Is(err, domainuser.ErrNotFound) {
			if s.reserveAccountNoIfAbsent(accountNo) {
				return accountNo, true
			}
			logger.Warn("Register account no reserved by concurrent request", "accountNo", accountNo, "retry", retry)
			continue
		}

		s.cacheAccountNo(accountNo)
		logger.Warn("Register account no collision", "accountNo", accountNo, "retry", retry)
	}
	logger.Error("Register generate unique account no exhausted", "maxRetry", maxAccountNoGenerateRetry)
	return "", false
}

func (s *Service) isAccountNoCached(accountNo string) bool {
	s.accountNoMu.RLock()
	defer s.accountNoMu.RUnlock()
	_, exists := s.accountNoCache[accountNo]
	return exists
}

func (s *Service) cacheAccountNo(accountNo string) {
	s.accountNoMu.Lock()
	defer s.accountNoMu.Unlock()
	s.accountNoCache[accountNo] = struct{}{}
}

func (s *Service) uncacheAccountNo(accountNo string) {
	s.accountNoMu.Lock()
	defer s.accountNoMu.Unlock()
	delete(s.accountNoCache, accountNo)
}

func (s *Service) reserveAccountNoIfAbsent(accountNo string) bool {
	s.accountNoMu.Lock()
	defer s.accountNoMu.Unlock()
	if _, exists := s.accountNoCache[accountNo]; exists {
		return false
	}
	s.accountNoCache[accountNo] = struct{}{}
	return true
}
