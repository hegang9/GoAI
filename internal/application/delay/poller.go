// Package delay 编排延迟任务用例，不直接依赖 GORM、RabbitMQ SDK 或 HTTP 框架。
package delay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	domaindelay "GopherAI/internal/domain/delay"
	"GopherAI/pkg/logger"
)

const (
	// DefaultPollInterval 是 Poller 查询 MySQL 的默认周期。
	DefaultPollInterval = 200 * time.Millisecond
	// DefaultPollAhead 表示提前把未来十秒内的长延迟任务转交给 Level MQ。
	DefaultPollAhead = 10 * time.Second
	// DefaultLeaseDuration 是一次 MySQL 抢占的默认有效期。
	DefaultLeaseDuration = 30 * time.Second
	// DefaultPollBatchSize 限制单次事务锁定和返回的任务数量。
	DefaultPollBatchSize = 200
	// DefaultPublishWorkers 限制单个 Poller 并发等待 Broker confirm 的 worker 数量。
	DefaultPublishWorkers = 16
	// DefaultPollerMaxLevel 是长延迟任务离开 MySQL 后可进入的最大 TTL Level。
	DefaultPollerMaxLevel = 10
)

// Clock 为 Poller 提供当前时间。生产环境使用系统时钟，测试可注入固定时钟，
// 从而精确验证 ClaimDue 的查询窗口、租约截止时间和 Level 边界。
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// PollerConfig 描述 MySQL 长延迟任务向 RabbitMQ Level 层转交时的运行参数。
type PollerConfig struct {
	// Owner 是当前 Poller 实例的唯一标识，会写入 MySQL lease_owner。
	// 多 Pod 部署时可使用 podName + processID + 启动 UUID，实例重启后应生成新标识。
	Owner string
	// Interval 是连续两轮 MySQL 扫描之间的时间间隔。
	Interval time.Duration
	// Ahead 是 ClaimDue 的前瞻窗口，默认只把未来十秒内的任务移交给 Level MQ。
	Ahead time.Duration
	// LeaseDuration 是任务处于 dispatching 状态时当前实例拥有处理权的时长，即租约时长。
	LeaseDuration time.Duration
	// BatchSize 是单次 ClaimDue 最多抢占的任务数，限制数据库事务规模。
	BatchSize int
	// PublishWorkers 是事务提交后并发发布 Level MQ 的最大 worker 数。
	PublishWorkers int
	// MaxLevel 是本 Poller 可以选择的最大整秒 TTL Level，范围为 0～60。
	MaxLevel int
	// Clock 仅提供时间来源；为 nil 时使用系统时钟。
	Clock Clock
}

// DefaultPollerConfig 返回长延迟任务 Poller 的默认配置。
func DefaultPollerConfig(owner string) PollerConfig {
	return PollerConfig{
		Owner:          owner,
		Interval:       DefaultPollInterval,
		Ahead:          DefaultPollAhead,
		LeaseDuration:  DefaultLeaseDuration,
		BatchSize:      DefaultPollBatchSize,
		PublishWorkers: DefaultPublishWorkers,
		MaxLevel:       DefaultPollerMaxLevel,
	}
}

// Poller 是 MySQL 长延迟存储与 RabbitMQ 短延迟 Level 层之间的应用层调度器。
//
// Repository 负责持久化任务所有权和租约；LevelPublisher 负责可靠发布并等待
// Broker confirm。Poller 只负责编排两者，绝不会在 MySQL 事务内进行 MQ 网络调用。
type Poller struct {
	repo      domaindelay.DelayTaskRepository
	publisher domaindelay.LevelPublisher

	owner          string
	interval       time.Duration
	ahead          time.Duration
	leaseDuration  time.Duration
	batchSize      int
	publishWorkers int
	maxLevel       int
	clock          Clock
}

// NewPoller 创建一个长延迟任务轮询器，并校验配置是否能被 Level 层覆盖。
func NewPoller(
	repo domaindelay.DelayTaskRepository,
	publisher domaindelay.LevelPublisher,
	config PollerConfig,
) (*Poller, error) {
	if repo == nil {
		return nil, errors.New("new delay poller: repository is nil")
	}
	if publisher == nil {
		return nil, errors.New("new delay poller: level publisher is nil")
	}
	if config.Owner == "" {
		return nil, errors.New("new delay poller: owner is empty")
	}
	if len(config.Owner) > 64 {
		return nil, errors.New("new delay poller: owner exceeds 64 bytes")
	}
	if config.Interval <= 0 {
		return nil, errors.New("new delay poller: interval must be positive")
	}
	if config.Ahead < 0 {
		return nil, errors.New("new delay poller: ahead cannot be negative")
	}
	if config.LeaseDuration <= 0 {
		return nil, errors.New("new delay poller: lease duration must be positive")
	}
	if config.BatchSize <= 0 {
		return nil, errors.New("new delay poller: batch size must be positive")
	}
	if config.PublishWorkers <= 0 {
		return nil, errors.New("new delay poller: publish workers must be positive")
	}
	if config.MaxLevel < 0 || config.MaxLevel > 60 {
		return nil, errors.New("new delay poller: max level must be between 0 and 60")
	}
	// Level 取剩余时间的整秒下界。Ahead 必须小于 (MaxLevel+1) 秒，
	// 才能保证 Level 到期后留给本地时间轮的尾差不足一秒。
	if config.Ahead >= time.Duration(config.MaxLevel+1)*time.Second {
		return nil, errors.New("new delay poller: ahead exceeds max level coverage")
	}

	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Poller{
		repo:           repo,
		publisher:      publisher,
		owner:          config.Owner,
		interval:       config.Interval,
		ahead:          config.Ahead,
		leaseDuration:  config.LeaseDuration,
		batchSize:      config.BatchSize,
		publishWorkers: config.PublishWorkers,
		maxLevel:       config.MaxLevel,
		clock:          clock,
	}, nil
}

// Run 启动持续轮询循环。它在启动时立即执行一轮，随后按 Interval 扫描 MySQL；
// 单轮错误只记录并继续运行，避免一次数据库或 MQ 抖动永久停止调度器。
// ctx 取消表示应用正在优雅关闭，此方法会停止领取新任务并等待当前 PollOnce 返回。
func (p *Poller) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run delay poller: context is nil")
	}

	logger.Info(
		"Delay poller started",
		"owner", p.owner,
		"interval", p.interval,
		"ahead", p.ahead,
		"leaseDuration", p.leaseDuration,
		"batchSize", p.batchSize,
		"publishWorkers", p.publishWorkers,
	)
	defer logger.Info("Delay poller stopped", "owner", p.owner)

	p.pollOnceAndLog(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.pollOnceAndLog(ctx)
		}
	}
}

// PollOnce 完成一轮“MySQL 抢占 -> Level MQ 发布 -> MySQL 状态回写”。
// ClaimDue 返回前数据库事务已经提交，因此后续 Broker confirm 等待不会占用 MySQL 行锁。
func (p *Poller) PollOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("poll delay tasks: context is nil")
	}

	now := p.clock.Now().UTC()
	tasks, err := p.repo.ClaimDue(
		ctx,
		now,
		now.Add(p.ahead),
		now.Add(p.leaseDuration),
		p.batchSize,
		p.owner,
	)
	if err != nil {
		return fmt.Errorf("claim due delay tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}

	return p.publishClaimed(ctx, tasks)
}

// pollOnceAndLog 是 Run 的容错边界：单轮失败会保留在日志中，但不会退出后台循环。
func (p *Poller) pollOnceAndLog(ctx context.Context) {
	if err := p.PollOnce(ctx); err != nil && ctx.Err() == nil {
		logger.Error("Delay poller round failed", "owner", p.owner, "err", err)
	}
}

// publishClaimed 使用固定 worker 数处理已经提交租约的任务。
// 如果关闭信号在批次中途到达，尚未发布的任务保持 dispatching，随后依靠租约过期恢复。
func (p *Poller) publishClaimed(ctx context.Context, tasks []domaindelay.Task) error {
	workerCount := p.publishWorkers
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	// 主 goroutine 向 worker 分发任务 channel
	jobs := make(chan domaindelay.Task)
	// worker 向主 goroutine 汇报错误
	errorsCh := make(chan error, len(tasks)+1)
	var workers sync.WaitGroup
	workers.Add(workerCount)

	for range workerCount {
		go func() {
			defer workers.Done()
			for task := range jobs {
				if err := p.publishClaimedTask(ctx, task); err != nil {
					// 收集错误，但不阻塞
					errorsCh <- err
				}
			}
		}()
	}

	// 标签，为了在select中跳出外层for循环
sendLoop:
	for _, task := range tasks {
		// 使用 select 同时等待发送和取消
		select {
		case jobs <- task:
		case <-ctx.Done():
			errorsCh <- ctx.Err()
			break sendLoop
		}
	}
	// 分发完毕，在pubulish侧关闭channel，由于是单发多收，在发送侧关闭channel不会导致panic
	close(jobs)
	workers.Wait()
	close(errorsCh)

	// 聚合错误返回
	var joined []error
	for err := range errorsCh {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}

// publishClaimedTask 处理单个任务的所有权转移：
//   - Publish 返回 nil：Level MQ 已经可靠接管，回写 level_queued；
//   - PublishRejectedError：MQ 明确未接管，可以把 MySQL 租约释放回 pending；
//   - 其他错误：结果未知，不能 Release，等待租约过期后以相同任务 ID 重投。
func (p *Poller) publishClaimedTask(ctx context.Context, task domaindelay.Task) error {
	// 重新计算level
	now := p.clock.Now().UTC()
	level := floorLevel(task.TargetAt, now.UnixMilli(), p.maxLevel)

	err := p.publisher.Publish(ctx, level, task)
	if err == nil {
		if markErr := p.repo.MarkLevelQueued(ctx, task.ID, p.owner, task.Version); markErr != nil {
			// MQ 已确认接管而数据库回写失败时绝不能 Release。租约过期恢复后重复发布，
			// 稳定 task.ID 让下游执行幂等处理，从而维持 at-least-once。
			// TODO：业务侧handle实现幂等
			return fmt.Errorf(
				"mark delay task %q level queued after broker confirm; lease retained: %w",
				task.ID,
				markErr,
			)
		}
		return nil
	}

	// 如果错误不是PublishRejectedError，则返回未知错误
	if !domaindelay.IsPublishRejected(err) {
		return fmt.Errorf(
			"publish delay task %q to level %d has unknown outcome; lease retained: %w",
			task.ID,
			level,
			err,
		)
	}

	if releaseErr := p.repo.Release(ctx, task.ID, p.owner, task.Version, err); releaseErr != nil {
		return errors.Join(
			fmt.Errorf("publish delay task %q to level %d was rejected: %w", task.ID, level, err),
			fmt.Errorf("release delay task %q lease: %w", task.ID, releaseErr),
		)
	}
	return fmt.Errorf(
		"publish delay task %q to level %d was rejected; lease released: %w",
		task.ID,
		level,
		err,
	)
}

// floorLevel 按剩余时间向下取整选择固定 TTL Queue。
// 例如剩余 9.738 秒进入 Level 9，TTL 到期后 Dispatcher 时间轮只需等待约 738ms；
// 剩余不足一秒或已经到期时返回 Level 0，由 Publisher 直接路由 Dispatcher Inbox。
func floorLevel(targetAtMs int64, nowMs int64, maxLevel int) int {
	remainingMs := targetAtMs - nowMs
	if remainingMs <= 0 {
		return 0
	}

	level := int(remainingMs / int64(time.Second/time.Millisecond))
	if level > maxLevel {
		return maxLevel
	}
	return level
}
