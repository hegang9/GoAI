package delay

import (
	"context"
	"testing"
	"time"

	delayDomain "GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"
)

func TestWheelShardInsertCalculatesSlotAndRounds(t *testing.T) {
	const nowMs int64 = 1_000_000

	tests := []struct {
		name          string
		remainingMs   int64
		wantSlot      int
		wantRounds    int64
		wantImmediate bool
	}{
		{name: "expired", remainingMs: 0, wantImmediate: true},
		{name: "one millisecond", remainingMs: 1, wantSlot: 21},
		{name: "one tick", remainingMs: 10, wantSlot: 21},
		{name: "two ticks", remainingMs: 11, wantSlot: 22},
		{name: "almost one round", remainingMs: 999, wantSlot: 20},
		{name: "one round", remainingMs: 1000, wantSlot: 20},
		{name: "one round and one tick", remainingMs: 1001, wantSlot: 21, wantRounds: 1},
		{name: "one round and twenty ticks", remainingMs: 1200, wantSlot: 40, wantRounds: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shard := &wheelShard{cursor: 20}
			item := dispatchItem{task: wheelTask(t, "schedule-slot", nowMs+tt.remainingMs)}

			if got := shard.insert(item, nowMs); got != tt.wantImmediate {
				t.Fatalf("insert() immediate = %t, want %t", got, tt.wantImmediate)
			}
			if tt.wantImmediate {
				return
			}

			entries := shard.slots[tt.wantSlot]
			if len(entries) != 1 {
				t.Fatalf("slot %d entries = %d, want 1", tt.wantSlot, len(entries))
			}
			if entries[0].rounds != tt.wantRounds {
				t.Fatalf("rounds = %d, want %d", entries[0].rounds, tt.wantRounds)
			}
		})
	}
}

func TestWheelShardAdvanceReinsertsEarlyTask(t *testing.T) {
	const nowMs int64 = 1_000_000
	shard := &wheelShard{cursor: 20}
	item := dispatchItem{task: wheelTask(t, "schedule-early", nowMs+10)}

	if shard.insert(item, nowMs) {
		t.Fatal("insert() reported future task as immediate")
	}
	if due := shard.advance(nowMs + 5); len(due) != 0 {
		t.Fatalf("advance() emitted %d early tasks", len(due))
	}
	if len(shard.slots[22]) != 1 {
		t.Fatalf("reinserted slot entries = %d, want 1", len(shard.slots[22]))
	}

	due := shard.advance(nowMs + 10)
	if len(due) != 1 || due[0].task.ID != item.task.ID {
		t.Fatalf("advance() due = %#v, want task %q", due, item.task.ID)
	}
}

func TestWheelShardAdvanceHonorsRounds(t *testing.T) {
	const nowMs int64 = 1_000_000
	shard := &wheelShard{cursor: 20}
	item := dispatchItem{task: wheelTask(t, "schedule-rounds", nowMs+1200)}

	if shard.insert(item, nowMs) {
		t.Fatal("insert() reported future task as immediate")
	}
	for tick := int64(1); tick < 120; tick++ {
		if due := shard.advance(nowMs + tick*10); len(due) != 0 {
			t.Fatalf("advance() emitted task at tick %d", tick)
		}
	}

	due := shard.advance(nowMs + 1200)
	if len(due) != 1 || due[0].task.ID != item.task.ID {
		t.Fatalf("advance() due = %#v, want task %q", due, item.task.ID)
	}
}

func TestWheelShardIndexIsStable(t *testing.T) {
	tests := map[string]int{
		"schedule-1":                6,
		"schedule-2":                3,
		"retry:analytics:message-1": 8,
	}

	for taskID, want := range tests {
		if got := wheelShardIndex(taskID); got != want {
			t.Fatalf("wheelShardIndex(%q) = %d, want %d", taskID, got, want)
		}
	}
}

func TestWheelRunDeliversExpiredTaskAndStops(t *testing.T) {
	ready := make(chan dispatchItem, 1)
	wheel := newWheel(ready)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		wheel.run(ctx)
		close(done)
	}()

	acked := false
	item := dispatchItem{
		task: wheelTask(t, "schedule-expired", time.Now().Add(-time.Second).UnixMilli()),
		ack: func() error {
			acked = true
			return nil
		},
	}
	if err := wheel.add(ctx, item); err != nil {
		t.Fatalf("add() error = %v", err)
	}

	select {
	case got := <-ready:
		if got.task.ID != item.task.ID {
			t.Fatalf("ready task = %q, want %q", got.task.ID, item.task.ID)
		}
		if acked {
			t.Fatal("wheel called ACK before Dispatcher")
		}
		if err := got.ack(); err != nil {
			t.Fatalf("ack() error = %v", err)
		}
		if !acked {
			t.Fatal("ready item did not preserve ACK callback")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for expired task")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wheel did not stop after context cancellation")
	}
}

func TestWheelRunStopsWhileReadyChannelIsBlocked(t *testing.T) {
	ready := make(chan dispatchItem)
	wheel := newWheel(ready)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		wheel.run(ctx)
		close(done)
	}()

	item := dispatchItem{
		task: wheelTask(t, "schedule-backpressure", time.Now().Add(-time.Second).UnixMilli()),
		ack:  func() error { return nil },
	}
	if err := wheel.add(ctx, item); err != nil {
		t.Fatalf("add() error = %v", err)
	}

	// Shard 正在等待下游接收 ready，取消后仍必须及时退出。
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wheel did not stop while ready channel was blocked")
	}
}

func TestWheelAddRejectsMissingACK(t *testing.T) {
	wheel := newWheel(make(chan dispatchItem, 1))
	err := wheel.add(context.Background(), dispatchItem{
		task: wheelTask(t, "schedule-no-ack", time.Now().UnixMilli()),
	})
	if err == nil {
		t.Fatal("add() error = nil, want missing ACK error")
	}
}

func wheelTask(t *testing.T, id string, targetAt int64) delayDomain.Task {
	t.Helper()

	msg, err := messageDomain.New(
		"message-1",
		"chat.message.created.v1",
		nil,
		[]byte(`{"text":"hello"}`),
		time.UnixMilli(1_000).UTC(),
	)
	if err != nil {
		t.Fatalf("message.New() error = %v", err)
	}
	task, err := delayDomain.NewTask(
		id,
		"account-1",
		msg,
		messageDomain.TopicTarget(),
		0,
		targetAt,
	)
	if err != nil {
		t.Fatalf("delay.NewTask() error = %v", err)
	}
	return task
}
