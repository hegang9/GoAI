package delay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domaindelay "GopherAI/internal/domain/delay"
	domainmessage "GopherAI/internal/domain/message"
)

func TestPollerPollOnceMarksConfirmedTask(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	task := delayTaskForPoller("task-confirmed", now.Add(9738*time.Millisecond))

	var claimed bool
	var marked bool
	repo := &pollerRepositoryStub{
		claimDue: func(_ context.Context, gotNow, ahead, leaseUntil time.Time, limit int, owner string) ([]domaindelay.Task, error) {
			claimed = true
			if !gotNow.Equal(now) || !ahead.Equal(now.Add(10*time.Second)) {
				t.Fatalf("ClaimDue() window = %v..%v", gotNow, ahead)
			}
			if !leaseUntil.Equal(now.Add(30*time.Second)) || limit != 200 || owner != "poller-test" {
				t.Fatalf("ClaimDue() lease=%v limit=%d owner=%q", leaseUntil, limit, owner)
			}
			return []domaindelay.Task{task}, nil
		},
		markLevelQueued: func(_ context.Context, taskID, owner string, version int64) error {
			marked = true
			if taskID != task.ID || owner != "poller-test" || version != task.Version {
				t.Fatalf("MarkLevelQueued() task=%q owner=%q version=%d", taskID, owner, version)
			}
			return nil
		},
	}
	publisher := &levelPublisherStub{
		publish: func(_ context.Context, level int, got domaindelay.Task) error {
			if level != 9 || got.ID != task.ID {
				t.Fatalf("Publish() level=%d task=%q", level, got.ID)
			}
			return nil
		},
	}

	poller := newTestPoller(t, now, repo, publisher)
	if err := poller.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce() error = %v", err)
	}
	if !claimed || !marked {
		t.Fatalf("PollOnce() claimed=%v marked=%v", claimed, marked)
	}
}

func TestPollerPollOnceReleasesDefinitelyRejectedTask(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	task := delayTaskForPoller("task-rejected", now.Add(5*time.Second))
	rejected := errors.New("broker nack")

	var released bool
	repo := &pollerRepositoryStub{
		claimDue: func(context.Context, time.Time, time.Time, time.Time, int, string) ([]domaindelay.Task, error) {
			return []domaindelay.Task{task}, nil
		},
		release: func(_ context.Context, taskID, owner string, version int64, cause error) error {
			released = true
			if taskID != task.ID || owner != "poller-test" || version != task.Version {
				t.Fatalf("Release() task=%q owner=%q version=%d", taskID, owner, version)
			}
			if !errors.Is(cause, rejected) {
				t.Fatalf("Release() cause = %v", cause)
			}
			return nil
		},
	}
	publisher := &levelPublisherStub{
		publish: func(context.Context, int, domaindelay.Task) error {
			return domaindelay.NewPublishRejectedError(rejected)
		},
	}

	err := newTestPoller(t, now, repo, publisher).PollOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "lease released") {
		t.Fatalf("PollOnce() error = %v, want released error", err)
	}
	if !released {
		t.Fatal("PollOnce() did not release definitely rejected task")
	}
}

func TestPollerPollOnceKeepsLeaseForUnknownPublishOutcome(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	task := delayTaskForPoller("task-unknown", now.Add(5*time.Second))

	repo := &pollerRepositoryStub{
		claimDue: func(context.Context, time.Time, time.Time, time.Time, int, string) ([]domaindelay.Task, error) {
			return []domaindelay.Task{task}, nil
		},
		release: func(context.Context, string, string, int64, error) error {
			t.Fatal("Release() called for unknown publish outcome")
			return nil
		},
	}
	publisher := &levelPublisherStub{
		publish: func(context.Context, int, domaindelay.Task) error {
			return errors.New("confirm timeout")
		},
	}

	err := newTestPoller(t, now, repo, publisher).PollOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown outcome; lease retained") {
		t.Fatalf("PollOnce() error = %v, want unknown outcome", err)
	}
}

func TestPollerPollOnceKeepsLeaseWhenMarkFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	task := delayTaskForPoller("task-mark-failed", now.Add(5*time.Second))

	repo := &pollerRepositoryStub{
		claimDue: func(context.Context, time.Time, time.Time, time.Time, int, string) ([]domaindelay.Task, error) {
			return []domaindelay.Task{task}, nil
		},
		markLevelQueued: func(context.Context, string, string, int64) error {
			return errors.New("mysql unavailable")
		},
		release: func(context.Context, string, string, int64, error) error {
			t.Fatal("Release() called after Broker confirm")
			return nil
		},
	}
	publisher := &levelPublisherStub{
		publish: func(context.Context, int, domaindelay.Task) error { return nil },
	}

	err := newTestPoller(t, now, repo, publisher).PollOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "after broker confirm; lease retained") {
		t.Fatalf("PollOnce() error = %v, want mark failure", err)
	}
}

func TestFloorLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		remainingMs int64
		maxLevel    int
		want        int
	}{
		{name: "expired", remainingMs: -1, maxLevel: 10, want: 0},
		{name: "sub_second", remainingMs: 999, maxLevel: 10, want: 0},
		{name: "exact_second", remainingMs: 1000, maxLevel: 10, want: 1},
		{name: "floor_fraction", remainingMs: 9738, maxLevel: 10, want: 9},
		{name: "clamp", remainingMs: 15000, maxLevel: 10, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := floorLevel(tt.remainingMs, 0, tt.maxLevel); got != tt.want {
				t.Fatalf("floorLevel() = %d, want %d", got, tt.want)
			}
		})
	}
}

type fixedPollerClock struct {
	now time.Time
}

func (c fixedPollerClock) Now() time.Time { return c.now }

type levelPublisherStub struct {
	publish func(context.Context, int, domaindelay.Task) error
}

func (p *levelPublisherStub) Publish(ctx context.Context, level int, task domaindelay.Task) error {
	return p.publish(ctx, level, task)
}

type pollerRepositoryStub struct {
	claimDue        func(context.Context, time.Time, time.Time, time.Time, int, string) ([]domaindelay.Task, error)
	markLevelQueued func(context.Context, string, string, int64) error
	release         func(context.Context, string, string, int64, error) error
}

func (r *pollerRepositoryStub) Create(context.Context, domaindelay.Task) (domaindelay.Task, bool, error) {
	return domaindelay.Task{}, false, errors.New("unexpected Create call")
}

func (r *pollerRepositoryStub) Get(context.Context, string, string) (domaindelay.Task, error) {
	return domaindelay.Task{}, errors.New("unexpected Get call")
}

func (r *pollerRepositoryStub) ClaimDue(ctx context.Context, now, ahead, leaseUntil time.Time, limit int, owner string) ([]domaindelay.Task, error) {
	return r.claimDue(ctx, now, ahead, leaseUntil, limit, owner)
}

func (r *pollerRepositoryStub) MarkLevelQueued(ctx context.Context, taskID, owner string, version int64) error {
	if r.markLevelQueued == nil {
		return errors.New("unexpected MarkLevelQueued call")
	}
	return r.markLevelQueued(ctx, taskID, owner, version)
}

func (r *pollerRepositoryStub) Release(ctx context.Context, taskID, owner string, version int64, cause error) error {
	if r.release == nil {
		return errors.New("unexpected Release call")
	}
	return r.release(ctx, taskID, owner, version, cause)
}

func (r *pollerRepositoryStub) Cancel(context.Context, string, string, int64) error {
	return errors.New("unexpected Cancel call")
}

func newTestPoller(t *testing.T, now time.Time, repo domaindelay.DelayTaskRepository, publisher domaindelay.LevelPublisher) *Poller {
	t.Helper()
	config := DefaultPollerConfig("poller-test")
	config.PublishWorkers = 1
	config.Clock = fixedPollerClock{now: now}
	poller, err := NewPoller(repo, publisher, config)
	if err != nil {
		t.Fatal(err)
	}
	return poller
}

func delayTaskForPoller(id string, targetAt time.Time) domaindelay.Task {
	return domaindelay.Task{
		ID:        id,
		AccountNo: "account-1",
		Message: domainmessage.Message{
			ID:        "message-" + id,
			Topic:     "notification.created.v1",
			Headers:   map[string]string{},
			Body:      []byte(`{"message":"hello"}`),
			Timestamp: targetAt.Add(-time.Second),
		},
		Target:   domainmessage.TopicTarget(),
		TargetAt: targetAt.UnixMilli(),
		Version:  2,
		Status:   domaindelay.StatusDispatching,
	}
}
