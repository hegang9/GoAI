package delay

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	delayDomain "GopherAI/internal/domain/delay"
)

const (
	// wheelShardCount 用多个独立事件循环分散任务，避免所有槽位共享一把锁。
	wheelShardCount = 16
	// wheelSlotCount 与 wheelTick 共同决定单圈覆盖 1 秒。
	wheelSlotCount = 100
	wheelTick      = 10 * time.Millisecond
	// Task.TargetAt 使用 Unix 毫秒，因此提前保存同单位的 Tick 长度。
	wheelTickMs = int64(wheelTick / time.Millisecond)
)

// dispatchItem 同时保存任务和原 Dispatcher Delivery 的 ACK 操作。
// 时间轮只转交该操作，只有 FinalPublisher 成功后 Dispatcher 才会调用它。
type dispatchItem struct {
	task delayDomain.Task
	ack  func() error
}

// wheelEntry 记录任务所在槽位还需要完整经过多少圈。
type wheelEntry struct {
	item   dispatchItem
	rounds int64
}

// wheel 将任务按稳定 schedule ID 分散到 16 个独立 Shard。
type wheel struct {
	// 相同 schedule ID 始终路由到同一个 Shard。
	shards [wheelShardCount]*wheelShard
	// ready 由 Dispatcher 创建和消费，时间轮只写入且不负责关闭。
	ready chan<- dispatchItem
}

// wheelShard 由单个 goroutine 独占，因此 slots 和 cursor 不需要加锁。
type wheelShard struct {
	// 无缓冲 input 让 ready 堵塞产生的背压继续传递给 Dispatcher。
	input chan dispatchItem
	// 每个槽位可以保存多个在相近时间到期的任务。
	slots [wheelSlotCount][]wheelEntry
	// cursor 表示最近一次已经处理的槽位，下一个 Tick 处理 cursor+1。
	cursor int
}

// newWheel 只初始化数据结构；调用方需要单独启动 run。
func newWheel(ready chan<- dispatchItem) *wheel {
	w := &wheel{ready: ready}
	for index := range w.shards {
		w.shards[index] = &wheelShard{input: make(chan dispatchItem)}
	}
	return w
}

// run 启动所有 Shard，并在 ctx 取消后等待它们全部退出。
// 退出时轮内任务直接丢弃内存状态，但对应 RabbitMQ Delivery 仍保持未 ACK。
func (w *wheel) run(ctx context.Context) {
	var group sync.WaitGroup
	group.Add(len(w.shards))
	for _, shard := range w.shards {
		go func() {
			defer group.Done()
			shard.run(ctx, w.ready)
		}()
	}
	group.Wait()
}

// add 把任务交给固定 Shard；无缓冲 input 会把下游压力传回 Dispatcher。
func (w *wheel) add(ctx context.Context, item dispatchItem) error {
	if w == nil {
		return errors.New("delay wheel is nil")
	}
	if ctx == nil {
		return errors.New("delay wheel context is nil")
	}
	if err := item.task.Validate(); err != nil {
		return fmt.Errorf("validate delay wheel task %q: %w", item.task.ID, err)
	}
	if item.ack == nil {
		return errors.New("delay wheel ACK callback is nil")
	}

	shard := w.shards[wheelShardIndex(item.task.ID)]
	select {
	case shard.input <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *wheelShard) run(ctx context.Context, ready chan<- dispatchItem) {
	ticker := time.NewTicker(wheelTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case item := <-s.input:
			// 已到期任务绕过槽位，直接交给 Dispatcher 的发布循环。
			if s.insert(item, time.Now().UnixMilli()) && !sendReady(ctx, ready, item) {
				return
			}

		case now := <-ticker.C:
			// Tick goroutine 只做内存调度，绝不在这里调用 FinalPublisher。
			for _, item := range s.advance(now.UnixMilli()) {
				if !sendReady(ctx, ready, item) {
					return
				}
			}
		}
	}
}

// sendReady 在下游背压时阻塞，但允许关闭流程通过 ctx 及时解除阻塞。
func sendReady(ctx context.Context, ready chan<- dispatchItem, item dispatchItem) bool {
	select {
	case ready <- item:
		return true
	case <-ctx.Done():
		return false
	}
}

// insert 返回 true 表示任务已经到期，应立即进入 ready channel。
func (s *wheelShard) insert(item dispatchItem, nowMs int64) bool {
	remainingMs := item.task.TargetAt - nowMs
	if remainingMs <= 0 {
		return true
	}

	// Tick 数向上取整，保证任务不会因为落入较近槽位而提前触发。
	ticks := (remainingMs-1)/wheelTickMs + 1
	// slot 决定落在哪个槽位；rounds 决定经过几次完整旋转后才能触发。
	slot := (s.cursor + int(ticks%wheelSlotCount)) % wheelSlotCount
	rounds := (ticks - 1) / wheelSlotCount
	s.slots[slot] = append(s.slots[slot], wheelEntry{
		item:   item,
		rounds: rounds,
	})
	return false
}

// advance 推进一个槽位，返回已经到达绝对目标时间的任务。
func (s *wheelShard) advance(nowMs int64) []dispatchItem {
	s.cursor = (s.cursor + 1) % wheelSlotCount
	// 先摘下当前槽位，避免遍历期间重新插入任务破坏当前迭代。
	entries := s.slots[s.cursor]
	s.slots[s.cursor] = nil

	var due []dispatchItem
	for _, entry := range entries {
		if entry.rounds > 0 {
			// 留在同一槽位，下一整圈再次经过这里时继续判断。
			entry.rounds--
			s.slots[s.cursor] = append(s.slots[s.cursor], entry)
			continue
		}

		if entry.item.task.TargetAt > nowMs {
			// Ticker 可能略早触发，重新按绝对目标时间计算槽位，禁止提前发布。
			s.insert(entry.item, nowMs)
			continue
		}
		due = append(due, entry.item)
	}
	return due
}

// wheelShardIndex 使用固定 FNV-1a 算法，保证相同 schedule ID 始终进入同一 Shard。
func wheelShardIndex(taskID string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(taskID))
	return int(hash.Sum32() % wheelShardCount)
}
