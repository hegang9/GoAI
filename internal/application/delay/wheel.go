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
	wheelShardCount = 16
	wheelSlotCount  = 100
	wheelTick       = 10 * time.Millisecond
	wheelTickMs     = int64(wheelTick / time.Millisecond)
)

// dispatchItem 同时保存任务和原 Dispatcher Delivery 的 ACK 操作。
// 时间轮只转交该操作，只有 FinalPublisher 成功后 Dispatcher 才会调用它。
type dispatchItem struct {
	task delayDomain.Task
	ack  func() error
}

type wheelEntry struct {
	item   dispatchItem
	rounds int64
}

// wheel 将任务按稳定 schedule ID 分散到 16 个独立 Shard。
type wheel struct {
	shards [wheelShardCount]*wheelShard
	ready  chan<- dispatchItem
}

// wheelShard 由单个 goroutine 独占，因此 slots 和 cursor 不需要加锁。
type wheelShard struct {
	input  chan dispatchItem
	slots  [wheelSlotCount][]wheelEntry
	cursor int
}

func newWheel(ready chan<- dispatchItem) *wheel {
	w := &wheel{ready: ready}
	for index := range w.shards {
		w.shards[index] = &wheelShard{input: make(chan dispatchItem)}
	}
	return w
}

// run 启动所有 Shard，并在 ctx 取消后等待它们全部退出。
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
			if s.insert(item, time.Now().UnixMilli()) && !sendReady(ctx, ready, item) {
				return
			}

		case now := <-ticker.C:
			for _, item := range s.advance(now.UnixMilli()) {
				if !sendReady(ctx, ready, item) {
					return
				}
			}
		}
	}
}

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

	// 向上取整，保证任务不会因为落入较近槽位而提前触发。
	ticks := (remainingMs-1)/wheelTickMs + 1
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
	entries := s.slots[s.cursor]
	s.slots[s.cursor] = nil

	var due []dispatchItem
	for _, entry := range entries {
		if entry.rounds > 0 {
			entry.rounds--
			s.slots[s.cursor] = append(s.slots[s.cursor], entry)
			continue
		}

		if entry.item.task.TargetAt > nowMs {
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
