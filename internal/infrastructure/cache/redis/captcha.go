package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainuser "GopherAI/internal/domain/user"

	redisCli "github.com/redis/go-redis/v9"
)

// captchaKeyPrefix 验证码 key 模板。代码级约定，集中定义避免散落。
const captchaKeyPrefix = "captcha:%s"

// captchaTTL 验证码有效期。
const captchaTTL = 2 * time.Minute

// CaptchaStore 基于 Redis 实现 domain/user.CaptchaStore 端口。
type CaptchaStore struct {
	client *redisCli.Client
}

// NewCaptchaStore 创建验证码存储。
func NewCaptchaStore(client *redisCli.Client) *CaptchaStore {
	return &CaptchaStore{client: client}
}

// 编译期断言：CaptchaStore 必须满足领域端口。
var _ domainuser.CaptchaStore = (*CaptchaStore)(nil)

// captchaKey 生成邮箱验证码的 Redis key。
func captchaKey(email string) string {
	return fmt.Sprintf(captchaKeyPrefix, email)
}

// Set 写入邮箱验证码并设置有效期。
func (s *CaptchaStore) Set(ctx context.Context, email, captcha string) error {
	return s.client.Set(ctx, captchaKey(email), captcha, captchaTTL).Err()
}

// Check 校验验证码是否匹配，匹配成功后一次性消费（删除）。
// key 不存在（过期或未发送）视作验证码错误，返回 (false, nil)。
func (s *CaptchaStore) Check(ctx context.Context, email, input string) (bool, error) {
	key := captchaKey(email)
	stored, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if err == redisCli.Nil {
			return false, nil
		}
		return false, err
	}

	if strings.EqualFold(stored, input) {
		// 删除失败不影响验证结果（key 会自动过期）。
		_ = s.client.Del(ctx, key).Err()
		return true, nil
	}
	return false, nil
}
