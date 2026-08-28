package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"GopherAI/config"
	delayapp "GopherAI/internal/application/delay"
	chatdomain "GopherAI/internal/domain/chat"
	messagedomain "GopherAI/internal/domain/message"
	"GopherAI/internal/infrastructure/mq/rabbitmq"
	"GopherAI/internal/infrastructure/persistence"
	"GopherAI/pkg/id"
	"GopherAI/pkg/logger"

	"gorm.io/gorm"
)

// delayRuntimeConfig 是配置文件到各延迟组件构造参数的集中映射结果。
type delayRuntimeConfig struct {
	rabbit  rabbitmq.DelayConfig
	service delayapp.DelayServiceConfig
	poller  delayapp.PollerConfig
	final   rabbitmq.FinalPublisherConfig

	dispatcherQueue    string
	dispatcherPrefetch int
	dispatcherReady    int
	groupPrefetch      int
	consumerGroups     []config.DelayConsumerGroupConfig
}

// mapDelayRuntimeConfig 在联网前完成单位转换和跨组件约束校验。
func mapDelayRuntimeConfig(
	source config.DelayConfig,
	groupPrefetch int,
	pollerOwner string,
) (delayRuntimeConfig, error) {
	if source.MaxLevel < 1 || source.MaxLevel > 60 {
		return delayRuntimeConfig{}, fmt.Errorf("delay max level %d is outside 1..60", source.MaxLevel)
	}
	if source.ShortThresholdMs != int64(source.MaxLevel)*int64(time.Second/time.Millisecond) {
		return delayRuntimeConfig{}, errors.New("delay short threshold must equal max level coverage")
	}
	if source.PollerMaxLevel > source.MaxLevel {
		return delayRuntimeConfig{}, errors.New("delay poller max level exceeds delay topology")
	}
	if groupPrefetch <= 0 {
		return delayRuntimeConfig{}, errors.New("delay group consumer prefetch must be positive")
	}

	groups := make([]rabbitmq.DelayConsumerGroupConfig, 0, len(source.ConsumerGroups))
	consumerGroups := make([]config.DelayConsumerGroupConfig, 0, len(source.ConsumerGroups))
	retryPolicies := make(map[string]delayapp.RetryPolicy, len(source.ConsumerGroups))
	groupNames := make([]string, 0, len(source.ConsumerGroups))
	topics := make([]string, 0)
	seenGroups := make(map[string]struct{}, len(source.ConsumerGroups))
	seenTopics := make(map[string]struct{})

	for _, configuredGroup := range source.ConsumerGroups {
		group := config.DelayConsumerGroupConfig{
			Name:                 strings.TrimSpace(configuredGroup.Name),
			Queue:                strings.TrimSpace(configuredGroup.Queue),
			DeadLetterQueue:      strings.TrimSpace(configuredGroup.DeadLetterQueue),
			DeadLetterRoutingKey: strings.TrimSpace(configuredGroup.DeadLetterRoutingKey),
			RetryDelaysMs:        append([]int(nil), configuredGroup.RetryDelaysMs...),
			Topics:               make([]string, 0, len(configuredGroup.Topics)),
		}
		for _, configuredTopic := range configuredGroup.Topics {
			topic := strings.TrimSpace(configuredTopic)
			group.Topics = append(group.Topics, topic)
			if _, exists := seenTopics[topic]; !exists {
				seenTopics[topic] = struct{}{}
				topics = append(topics, topic)
			}
		}

		if _, exists := seenGroups[group.Name]; exists {
			return delayRuntimeConfig{}, fmt.Errorf("delay consumer group %q is duplicated", group.Name)
		}
		seenGroups[group.Name] = struct{}{}

		delays := make([]time.Duration, len(group.RetryDelaysMs))
		for index, delayMs := range group.RetryDelaysMs {
			delays[index] = time.Duration(delayMs) * time.Millisecond
		}
		retryPolicies[group.Name] = delayapp.RetryPolicy{Delays: delays}
		groupNames = append(groupNames, group.Name)
		consumerGroups = append(consumerGroups, group)
		groups = append(groups, rabbitmq.DelayConsumerGroupConfig{
			Name:                 group.Name,
			Queue:                group.Queue,
			Topics:               append([]string(nil), group.Topics...),
			DeadLetterQueue:      group.DeadLetterQueue,
			DeadLetterRoutingKey: group.DeadLetterRoutingKey,
		})
	}
	if _, exists := seenTopics[chatdomain.MessageCreatedTopic]; !exists {
		return delayRuntimeConfig{}, fmt.Errorf(
			"delay consumer groups do not subscribe to required topic %q",
			chatdomain.MessageCreatedTopic,
		)
	}

	confirmTimeout := time.Duration(source.ConfirmTimeoutMs) * time.Millisecond
	rabbitConfig := rabbitmq.DelayConfig{
		LevelExchange:        strings.TrimSpace(source.LevelExchange),
		LevelQueuePrefix:     strings.TrimSpace(source.LevelQueuePrefix),
		LevelRoutingPrefix:   strings.TrimSpace(source.LevelRoutingPrefix),
		DispatcherExchange:   strings.TrimSpace(source.DispatcherExchange),
		DispatcherQueue:      strings.TrimSpace(source.DispatcherQueue),
		DispatcherRoutingKey: strings.TrimSpace(source.DispatcherRoutingKey),
		TopicExchange:        strings.TrimSpace(source.TopicExchange),
		RedriveExchange:      strings.TrimSpace(source.RedriveExchange),
		DeadLetterExchange:   strings.TrimSpace(source.DeadLetterExchange),
		ConsumerGroups:       groups,
		MaxLevel:             source.MaxLevel,
		ConfirmTimeout:       confirmTimeout,
	}

	return delayRuntimeConfig{
		rabbit: rabbitConfig,
		service: delayapp.DelayServiceConfig{
			ShortThreshold: time.Duration(source.ShortThresholdMs) * time.Millisecond,
			MaxDelay:       time.Duration(source.MaxDelayHours) * time.Hour,
			RetryPolicies:  retryPolicies,
		},
		poller: delayapp.PollerConfig{
			Owner:          pollerOwner,
			Interval:       time.Duration(source.PollIntervalMs) * time.Millisecond,
			Ahead:          time.Duration(source.PollAheadMs) * time.Millisecond,
			LeaseDuration:  time.Duration(source.LeaseDurationMs) * time.Millisecond,
			BatchSize:      source.PollBatchSize,
			PublishWorkers: source.PublishWorkers,
			MaxLevel:       source.PollerMaxLevel,
		},
		final: rabbitmq.FinalPublisherConfig{
			TopicExchange:   rabbitConfig.TopicExchange,
			RedriveExchange: rabbitConfig.RedriveExchange,
			ConfirmTimeout:  confirmTimeout,
			Topics:          topics,
			ConsumerGroups:  groupNames,
		},
		dispatcherQueue:    rabbitConfig.DispatcherQueue,
		dispatcherPrefetch: source.DispatcherPrefetchCount,
		dispatcherReady:    source.DispatcherReadyCapacity,
		groupPrefetch:      groupPrefetch,
		consumerGroups:     consumerGroups,
	}, nil
}

// delayRuntime 持有统一延迟链路的长期组件及其关闭顺序。
type delayRuntime struct {
	poller             *delayapp.Poller
	dispatcher         *delayapp.Dispatcher
	dispatcherConsumer *rabbitmq.DispatcherConsumer
	groupConsumers     []*rabbitmq.GroupConsumer
	groupNames         []string
	levelPublisher     *rabbitmq.LevelPublisher
	finalPublisher     *rabbitmq.FinalPublisher

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	done    <-chan struct{}
}

func newDelayRuntime(
	source config.DelayConfig,
	groupPrefetch int,
	rabbit *rabbitmq.Client,
	db *gorm.DB,
	handlers map[string]func(context.Context, chatdomain.Message) error,
) (_ *delayRuntime, err error) {
	if !source.Enabled {
		return nil, nil
	}
	if rabbit == nil {
		return nil, errors.New("new delay runtime: rabbitmq client is nil")
	}
	if db == nil {
		return nil, errors.New("new delay runtime: database is nil")
	}

	runtimeConfig, err := mapDelayRuntimeConfig(
		source,
		groupPrefetch,
		"delay-"+id.GenerateUUID(),
	)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: map config: %w", err)
	}
	for _, group := range runtimeConfig.consumerGroups {
		if handlers[group.Name] == nil {
			return nil, fmt.Errorf("new delay runtime: consumer group %q handler is missing", group.Name)
		}
	}

	runtime := &delayRuntime{}
	initialized := false
	defer func() {
		if !initialized {
			err = errors.Join(err, runtime.closeComponents())
		}
	}()

	levelPublisher, err := rabbitmq.NewLevelPublisher(rabbit, runtimeConfig.rabbit)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: level publisher: %w", err)
	}
	runtime.levelPublisher = levelPublisher

	repository := persistence.NewDelayTaskRepository(db)
	delayService, err := delayapp.NewDelayService(repository, levelPublisher, runtimeConfig.service)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: service: %w", err)
	}

	poller, err := delayapp.NewPoller(repository, levelPublisher, runtimeConfig.poller)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: poller: %w", err)
	}
	runtime.poller = poller

	finalPublisher, err := rabbitmq.NewFinalPublisher(rabbit, runtimeConfig.final)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: final publisher: %w", err)
	}
	runtime.finalPublisher = finalPublisher

	dispatcher, err := delayapp.NewDispatcher(finalPublisher, runtimeConfig.dispatcherReady)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: dispatcher: %w", err)
	}
	runtime.dispatcher = dispatcher

	dispatcherConsumer, err := rabbitmq.NewDispatcherConsumer(
		rabbit,
		runtimeConfig.dispatcherQueue,
		runtimeConfig.dispatcherPrefetch,
		dispatcher.Submit,
	)
	if err != nil {
		return nil, fmt.Errorf("new delay runtime: dispatcher consumer: %w", err)
	}
	runtime.dispatcherConsumer = dispatcherConsumer

	scheduleRetry := func(
		ctx context.Context,
		accountNo string,
		consumerGroup string,
		message messagedomain.Message,
		currentAttempt uint32,
	) (bool, error) {
		_, scheduleErr := delayService.ScheduleRetry(ctx, delayapp.RetryCommand{
			AccountNo:      accountNo,
			ConsumerGroup:  consumerGroup,
			Message:        message,
			CurrentAttempt: currentAttempt,
		})
		if errors.Is(scheduleErr, delayapp.ErrRetryExhausted) {
			return true, nil
		}
		return false, scheduleErr
	}

	for _, group := range runtimeConfig.consumerGroups {
		consumer, err := rabbitmq.NewGroupConsumer(
			rabbit,
			group.Name,
			group.Queue,
			runtimeConfig.groupPrefetch,
			runtimeConfig.rabbit.DeadLetterExchange,
			group.DeadLetterRoutingKey,
			runtimeConfig.rabbit.ConfirmTimeout,
			handlers[group.Name],
			scheduleRetry,
		)
		if err != nil {
			return nil, fmt.Errorf("new delay runtime: consumer group %q: %w", group.Name, err)
		}
		runtime.groupConsumers = append(runtime.groupConsumers, consumer)
		runtime.groupNames = append(runtime.groupNames, group.Name)
	}

	initialized = true
	return runtime, nil
}

func (r *delayRuntime) Start() {
	if r == nil {
		return
	}

	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	r.started = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done
	r.mu.Unlock()

	var group sync.WaitGroup
	run := func(name string, stopRuntime bool, runner func(context.Context) error) {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := runner(ctx); err != nil && ctx.Err() == nil {
				logger.Error("delay runtime component stopped", "component", name, "err", err)
				if stopRuntime {
					cancel()
				}
			}
		}()
	}

	run("poller", true, r.poller.Run)
	run("dispatcher", true, r.dispatcher.Run)
	run("dispatcherConsumer", true, r.dispatcherConsumer.Run)
	for index, consumer := range r.groupConsumers {
		// 单个组的系统性错误只暂停该组，不能中断其他消费者组和核心调度链路。
		run("groupConsumer["+r.groupNames[index]+"]", false, consumer.Run)
	}
	go func() {
		group.Wait()
		close(done)
	}()
	logger.Info("delay runtime started", "groupConsumers", len(r.groupConsumers))
}

func (r *delayRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("shutdown delay runtime: context is nil")
	}

	r.mu.Lock()
	r.stopped = true
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var waitErr error
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			waitErr = fmt.Errorf("shutdown delay runtime: wait components: %w", ctx.Err())
		}
	}

	return errors.Join(waitErr, r.closeComponents())
}

func (r *delayRuntime) closeComponents() error {
	if r == nil {
		return nil
	}

	var joined []error
	for _, consumer := range r.groupConsumers {
		if err := consumer.Close(); err != nil {
			joined = append(joined, err)
		}
	}
	if err := r.dispatcherConsumer.Close(); err != nil {
		joined = append(joined, err)
	}
	if err := r.finalPublisher.Close(); err != nil {
		joined = append(joined, err)
	}
	if err := r.levelPublisher.Close(); err != nil {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}
