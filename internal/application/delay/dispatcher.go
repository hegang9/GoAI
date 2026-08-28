package delay

import (
	"context"
	"errors"
	"fmt"
	"time"

	delaydomain "GopherAI/internal/domain/delay"
	"GopherAI/pkg/logger"
)

// Dispatcher 串联时间轮和 FinalPublisher
type Dispatcher struct {
	publisher delaydomain.FinalPublisher
	ready     chan dispatchItem
	wheel     *wheel
}

// NewDispatcher 创建延迟任务调度器，readyCapacity 决定到期任务等待最终发布时的缓冲上限。
func NewDispatcher(publisher delaydomain.FinalPublisher, readyCapacity int) (*Dispatcher, error) {
	if publisher == nil {
		return nil, errors.New("new delay dispatcher: final publisher is nil")
	}
	if readyCapacity <= 0 {
		return nil, errors.New("new delay dispatcher: ready capacity must be positive")
	}

	ready := make(chan dispatchItem, readyCapacity)
	return &Dispatcher{
		publisher: publisher,
		ready:     ready,
		wheel:     newWheel(ready),
	}, nil
}

// Submit 接收 Dispatcher Inbox 任务：已到期任务直接进入 ready，未到期任务进入时间轮。
func (d *Dispatcher) Submit(ctx context.Context, task delaydomain.Task, ack func() error) error {
	if ctx == nil {
		return errors.New("submit delay task: context is nil")
	}
	if err := task.Validate(); err != nil {
		return fmt.Errorf("submit delay task %q: validate: %w", task.ID, err)
	}
	if ack == nil {
		return errors.New("submit delay task: ACK callback is nil")
	}
	if err := validate(d); err != nil {
		return fmt.Errorf("Dispatcher error:%w", err)
	}

	item := dispatchItem{
		task: task,
		ack:  ack,
	}

	// 已到期任务直接进入发布队列；ready 满时通过 ctx 保证关闭流程可以退出。
	if task.TargetAt <= time.Now().UnixMilli() {
		select {
		case d.ready <- item:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("submit expired delay task %q: %w", task.ID, ctx.Err())
		}
	}

	// 未到期任务进入时间轮，进行尾差等待
	if err := d.wheel.add(ctx, item); err != nil {
		return fmt.Errorf("submit delay task %q to wheel: %w", task.ID, err)
	}
	return nil
}

// Run 运行时间轮和发布循环
func (d *Dispatcher) Run(ctx context.Context) error {
	if err := validate(d); err != nil {
		return fmt.Errorf("Run dispatcher instance error:%w", err)
	}
	if ctx == nil {
		return errors.New("run delay dispatcher: context is nil")
	}

	//外部 ctx 只表示应用生命周期，但 Dispatcher 还可能因为 FinalPublisher 失败而主动退出
	// 子 Context 同时控制时间轮和 FinalPublisher。
	// FinalPublisher与时间轮同时停止，如果直接把外部 ctx 传给时间轮，Run 返回时外部 Context 可能仍然有效，16个 Shard 就会继续运行，造成 goroutine 泄漏
	runCtx, cancel := context.WithCancel(ctx)

	wheelDone := make(chan struct{})
	go func() {
		defer close(wheelDone)
		d.wheel.run(runCtx)
	}()

	// 无论是正常关闭还是发布失败，都需要先停止并等待时间轮退出
	defer func() {
		cancel()
		<-wheelDone
	}()

	logger.Info("Delay dispatcher started")
	defer logger.Info("Delay dispatcher stopped")

	for {
		select {
		case <-runCtx.Done():
			// 外部关闭属于正常生命周期，不作为运行错误返回。
			return nil

		case item, ok := <-d.ready:
			if !ok {
				// ready 由 Dispatcher 持有，正常运行期间不应该被关闭。
				return errors.New(
					"run delay dispatcher: ready channel closed unexpectedly",
				)
			}
			if item.ack == nil {
				return fmt.Errorf(
					"run delay dispatcher: task %q ACK callback is nil",
					item.task.ID,
				)
			}

			// 同步等待 Broker confirm。
			// 发布期间不继续取 ready，让有界 channel 自然产生背压。
			if err := d.publisher.Publish(runCtx, item.task); err != nil {
				if ctx.Err() != nil {
					// 关闭过程中发布被取消，原 Delivery 保持未 ACK。
					return nil
				}
				return fmt.Errorf(
					"dispatch final task %q: %w",
					item.task.ID,
					err,
				)
			}

			// FinalPublisher 返回 nil 表示最终消息系统已经可靠接管。
			// 必须在这个边界之后才能释放 Dispatcher Inbox 所有权。
			if err := item.ack(); err != nil {
				return fmt.Errorf(
					"ACK dispatched task %q: %w",
					item.task.ID,
					err,
				)
			}
		}
	}
}

func validate(d *Dispatcher) error {
	if d == nil {
		return errors.New("run delay dispatcher: dispatcher is nil")
	}
	if d.publisher == nil {
		return errors.New("run delay dispatcher: final publisher is nil")
	}
	if d.wheel == nil {
		return errors.New("run delay dispatcher: wheel is nil")
	}
	if d.ready == nil {
		return errors.New("run delay dispatcher: ready channel is nil")
	}
	return nil
}
