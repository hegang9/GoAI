package delay

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	delayDomain "GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"
)

const (
	// DefaultShortDelayThreshold 是 RabbitMQ Level 层直接持有任务的默认上限。
	DefaultShortDelayThreshold = 60 * time.Second
	// DefaultMaxDelay 是单个延迟任务允许配置的默认最大时长，7天。
	DefaultMaxDelay = 7 * 24 * time.Hour
	// MaxLevel 与固定 Level 1～60 Queue 的最大档位一致。
	MaxLevel = 60
)

var (
	// ErrDelayTooLong 表示任务目标时间超过系统允许的最大延迟。
	ErrDelayTooLong = errors.New("delay exceeds maximum")
	// ErrRetryPolicyNotFound 表示消费者组没有配置重试策略。
	ErrRetryPolicyNotFound = errors.New("retry policy not found")
	// ErrRetryExhausted 表示业务消息已经用完当前消费者组的全部重试机会。
	ErrRetryExhausted = errors.New("retry attempts exhausted")
)

// RetryPolicy 按顺序定义消费者组每次业务重试的等待时间。
type RetryPolicy struct {
	Delays []time.Duration
}

// DelayServiceConfig 描述长短延迟分界、最大延迟和消费者组重试策略。
type DelayServiceConfig struct {
	// ShortThreshold 以内的任务直接进入 Level MQ。
	ShortThreshold time.Duration
	// MaxDelay 限制单个任务允许设置的最大延迟。
	MaxDelay time.Duration
	// Clock 允许测试注入固定时间；为空时使用系统时钟。
	Clock Clock
	// RetryPolicies 以 consumer group 为键保存静态重试间隔。
	RetryPolicies map[string]RetryPolicy
}

// DefaultDelayServiceConfig 返回 60 秒长短分界和 7 天最大延迟的默认配置。
func DefaultDelayServiceConfig() DelayServiceConfig {
	return DelayServiceConfig{
		ShortThreshold: DefaultShortDelayThreshold,
		MaxDelay:       DefaultMaxDelay,
	}
}

// DelayService 统一创建普通延迟和消费重试任务，并完成长短延迟分流。
type DelayService struct {
	// repo 负责可靠持有超过 shortThreshold 的长延迟任务。
	repo delayDomain.DelayTaskRepository
	// publisher 负责把短延迟任务可靠发布到 Level 0～maxLevel。
	publisher delayDomain.LevelPublisher
	// clock 提供可替换的当前时间，确保边界计算可以被确定性测试。
	clock Clock

	// shortThreshold 是 Level MQ 与 MySQL 的长短延迟分界。
	shortThreshold time.Duration
	// maxDelay 是单个任务允许设置的最大延迟。
	maxDelay time.Duration
	// maxLevel 是由 shortThreshold 推导出的最大整秒 Level。
	maxLevel int
	// retryPolicies 保存各 consumer group 的静态重试间隔。
	retryPolicies map[string]RetryPolicy
}

// NewDelayService 创建统一延迟调度服务，并在启动阶段拒绝不合法配置。
func NewDelayService(
	repo delayDomain.DelayTaskRepository,
	publisher delayDomain.LevelPublisher,
	config DelayServiceConfig,
) (*DelayService, error) {
	if repo == nil {
		return nil, errors.New("new delay service: repository is nil")
	}
	if publisher == nil {
		return nil, errors.New("new delay service: level publisher is nil")
	}
	if config.ShortThreshold <= 0 ||
		config.ShortThreshold > MaxLevel*time.Second ||
		config.ShortThreshold%time.Second != 0 {
		return nil, fmt.Errorf(
			"new delay service: short threshold must be a whole second in range 1s..%ds",
			MaxLevel,
		)
	}
	if config.MaxDelay < config.ShortThreshold {
		return nil, errors.New("new delay service: max delay is shorter than short threshold")
	}

	// 复制配置切片，避免调用方在 Service 启动后修改运行中的重试策略。
	retryPolicies := make(map[string]RetryPolicy, len(config.RetryPolicies))
	for consumerGroup, policy := range config.RetryPolicies {
		if strings.TrimSpace(consumerGroup) == "" {
			return nil, errors.New("new delay service: retry policy consumer group is empty")
		}
		delays := append([]time.Duration(nil), policy.Delays...)
		for _, delay := range delays {
			if delay <= 0 || delay > config.MaxDelay {
				return nil, fmt.Errorf(
					"new delay service: retry delay for group %q must be in range 1ns..%s",
					consumerGroup,
					config.MaxDelay,
				)
			}
		}
		retryPolicies[consumerGroup] = RetryPolicy{Delays: delays}
	}

	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &DelayService{
		repo:           repo,
		publisher:      publisher,
		clock:          clock,
		shortThreshold: config.ShortThreshold,
		maxDelay:       config.MaxDelay,
		maxLevel:       int(config.ShortThreshold / time.Second),
		retryPolicies:  retryPolicies,
	}, nil
}

// Schedule 将短延迟任务可靠发布到 Level MQ，将长延迟任务交给 Repository 持有。
func (s *DelayService) Schedule(
	ctx context.Context,
	task delayDomain.Task,
) (delayDomain.Task, error) {
	nowMs := s.clock.Now().UnixMilli()
	remainingMs := task.TargetAt - nowMs
	// 过期任务允许走 Level 0，但未来任务不能突破最大延迟边界。
	if remainingMs > s.maxDelay.Milliseconds() {
		return delayDomain.Task{}, fmt.Errorf(
			"%w: remaining=%dms max=%dms",
			ErrDelayTooLong,
			remainingMs,
			s.maxDelay.Milliseconds(),
		)
	}
	// Level 使用整秒 TTL，floorLevel 会把不足一秒或已到期任务路由到 Level 0。
	// LevelPublisher 对 level=0 必须特殊处理：绕过 Level TTL Queue，直接可靠发布到 Dispatcher Inbox。
	if remainingMs <= s.shortThreshold.Milliseconds() {
		level := floorLevel(task.TargetAt, nowMs, s.maxLevel)
		if err := s.publisher.Publish(ctx, level, task); err != nil {
			return delayDomain.Task{}, fmt.Errorf(
				"schedule delay task %q to level %d: %w",
				task.ID,
				level,
				err,
			)
		}
		return task, nil
	}

	// 长延迟由 MySQL 接管；Repository 负责相同 schedule ID 的幂等创建。
	stored, _, err := s.repo.Create(ctx, task)
	if err != nil {
		return delayDomain.Task{}, fmt.Errorf("store delay task %q: %w", task.ID, err)
	}
	return stored, nil
}

// RetryCommand 描述一次消费失败后创建下一次重试任务所需的稳定输入。
type RetryCommand struct {
	// AccountNo 用于延迟任务的数据归属和隔离。
	AccountNo string
	// ConsumerGroup 决定重试策略和到期后的精确回投目标。
	ConsumerGroup string
	// Message 是需要保持 message ID 不变的原始业务消息。
	Message messageDomain.Message
	// CurrentAttempt 是刚刚失败的业务消费重试次数。
	CurrentAttempt uint32
}

// ScheduleRetry 根据消费者组策略构造下一次重试任务，并复用 Schedule 完成长短延迟分流。
func (s *DelayService) ScheduleRetry(
	ctx context.Context,
	command RetryCommand,
) (delayDomain.Task, error) {
	// 重试策略只由服务端静态配置决定，消息本身不能覆盖。
	policy, ok := s.retryPolicies[command.ConsumerGroup]
	if !ok {
		return delayDomain.Task{}, fmt.Errorf(
			"%w: consumer_group=%q",
			ErrRetryPolicyNotFound,
			command.ConsumerGroup,
		)
	}
	if command.CurrentAttempt == math.MaxUint32 {
		return delayDomain.Task{}, ErrRetryExhausted
	}
	nextAttempt := command.CurrentAttempt + 1
	// Delays 的长度即最大重试次数，下标与 RetryAttempt 一一对应。
	if nextAttempt > uint32(len(policy.Delays)) {
		return delayDomain.Task{}, fmt.Errorf(
			"%w: consumer_group=%q current_attempt=%d",
			ErrRetryExhausted,
			command.ConsumerGroup,
			command.CurrentAttempt,
		)
	}

	// 重试任务只回原消费者组，不能重新发布到共享 Topic Exchange。
	target, err := messageDomain.ConsumerGroupTarget(command.ConsumerGroup)
	if err != nil {
		return delayDomain.Task{}, fmt.Errorf("build retry target: %w", err)
	}
	task, err := delayDomain.NewTask(
		retryScheduleID(command.ConsumerGroup, command.Message.ID, nextAttempt),
		command.AccountNo,
		command.Message,
		target,
		nextAttempt,
		s.clock.Now().Add(policy.Delays[nextAttempt-1]).UnixMilli(),
	)
	if err != nil {
		return delayDomain.Task{}, fmt.Errorf("build retry task: %w", err)
	}
	// 统一复用 Schedule，避免重试场景复制一套长短延迟分流规则。
	return s.Schedule(ctx, task)
}

// retryScheduleID 为同一消费者组、消息和重试次数生成稳定的调度幂等键。
func retryScheduleID(consumerGroup, messageID string, attempt uint32) string {
	h := sha256.New()
	// 零字节分隔可避免字符串直接拼接产生边界歧义。
	_, _ = h.Write([]byte(consumerGroup))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(messageID))
	_, _ = h.Write([]byte{0})
	var encodedAttempt [8]byte
	binary.BigEndian.PutUint64(encodedAttempt[:], uint64(attempt))
	_, _ = h.Write(encodedAttempt[:])
	return hex.EncodeToString(h.Sum(nil))
}
