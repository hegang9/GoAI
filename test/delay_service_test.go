package test

import (
	"context"
	"errors"
	"testing"
	"time"

	delayApp "GopherAI/internal/application/delay"
	delayDomain "GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"
)

func TestDelayServiceScheduleRoutesShortTasksToLevel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		targetAt  time.Time
		wantLevel int
	}{
		{name: "expired", targetAt: now.Add(-time.Second), wantLevel: 0},
		{name: "fraction floors", targetAt: now.Add(9738 * time.Millisecond), wantLevel: 9},
		{name: "threshold", targetAt: now.Add(60 * time.Second), wantLevel: 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotLevel int
			publisher := &delayServicePublisherStub{
				publish: func(_ context.Context, level int, _ delayDomain.Task) error {
					gotLevel = level
					return nil
				},
			}
			service := newDelayServiceForTest(t, now, &delayServiceRepositoryStub{}, publisher, nil)
			task := delayServiceTopicTask(t, "schedule-"+tt.name, tt.targetAt)

			stored, err := service.Schedule(context.Background(), task)
			if err != nil {
				t.Fatalf("Schedule() error = %v", err)
			}
			if stored.ID != task.ID || gotLevel != tt.wantLevel {
				t.Fatalf("Schedule() task=%q level=%d, want task=%q level=%d", stored.ID, gotLevel, task.ID, tt.wantLevel)
			}
		})
	}
}

func TestDelayServiceScheduleStoresLongTask(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	task := delayServiceTopicTask(t, "schedule-long", now.Add(61*time.Second))
	var created bool
	repo := &delayServiceRepositoryStub{
		create: func(_ context.Context, got delayDomain.Task) (delayDomain.Task, bool, error) {
			created = true
			return got, true, nil
		},
	}
	publisher := &delayServicePublisherStub{
		publish: func(context.Context, int, delayDomain.Task) error {
			t.Fatal("LevelPublisher called for long task")
			return nil
		},
	}
	service := newDelayServiceForTest(t, now, repo, publisher, nil)

	stored, err := service.Schedule(context.Background(), task)
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if !created || stored.ID != task.ID {
		t.Fatalf("Schedule() created=%v stored=%+v", created, stored)
	}
}

func TestDelayServiceScheduleRejectsTooLongDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	service := newDelayServiceForTest(
		t,
		now,
		&delayServiceRepositoryStub{},
		&delayServicePublisherStub{},
		nil,
	)
	task := delayServiceTopicTask(t, "schedule-too-long", now.Add(7*24*time.Hour+time.Millisecond))

	if _, err := service.Schedule(context.Background(), task); !errors.Is(err, delayApp.ErrDelayTooLong) {
		t.Fatalf("Schedule() error = %v, want ErrDelayTooLong", err)
	}
}

func TestDelayServiceSchedulePropagatesPublisherFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	publishErr := errors.New("publish failed")
	service := newDelayServiceForTest(
		t,
		now,
		&delayServiceRepositoryStub{},
		&delayServicePublisherStub{
			publish: func(context.Context, int, delayDomain.Task) error {
				return publishErr
			},
		},
		nil,
	)

	_, err := service.Schedule(
		context.Background(),
		delayServiceTopicTask(t, "schedule-publish-failed", now.Add(time.Second)),
	)
	if !errors.Is(err, publishErr) {
		t.Fatalf("Schedule() error = %v, want publisher error", err)
	}
}

func TestDelayServiceScheduleRetryUsesGroupPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	var published []delayDomain.Task
	publisher := &delayServicePublisherStub{
		publish: func(_ context.Context, level int, task delayDomain.Task) error {
			if level != 10 {
				t.Fatalf("Publish() level = %d, want 10", level)
			}
			published = append(published, task)
			return nil
		},
	}
	service := newDelayServiceForTest(
		t,
		now,
		&delayServiceRepositoryStub{},
		publisher,
		map[string]delayApp.RetryPolicy{
			"analytics": {Delays: []time.Duration{10 * time.Second, time.Minute}},
		},
	)
	message := delayServiceMessage(t)
	command := delayApp.RetryCommand{
		AccountNo:      "account-1",
		ConsumerGroup:  "analytics",
		Message:        message,
		CurrentAttempt: 0,
	}

	first, err := service.ScheduleRetry(context.Background(), command)
	if err != nil {
		t.Fatalf("ScheduleRetry() error = %v", err)
	}
	second, err := service.ScheduleRetry(context.Background(), command)
	if err != nil {
		t.Fatalf("ScheduleRetry() second error = %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("ScheduleRetry() IDs = %q/%q, want deterministic", first.ID, second.ID)
	}
	if first.RetryAttempt != 1 ||
		first.Target.Kind != messageDomain.TargetConsumerGroup ||
		first.Target.ConsumerGroup != "analytics" ||
		first.TargetAt != now.Add(10*time.Second).UnixMilli() ||
		first.Message.ID != message.ID {
		t.Fatalf("ScheduleRetry() task = %+v", first)
	}
	if len(published) != 2 {
		t.Fatalf("ScheduleRetry() publish count = %d, want 2", len(published))
	}
}

func TestDelayServiceScheduleRetryRejectsMissingOrExhaustedPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	service := newDelayServiceForTest(
		t,
		now,
		&delayServiceRepositoryStub{},
		&delayServicePublisherStub{},
		map[string]delayApp.RetryPolicy{
			"analytics": {Delays: []time.Duration{time.Second}},
		},
	)
	message := delayServiceMessage(t)

	_, err := service.ScheduleRetry(context.Background(), delayApp.RetryCommand{
		AccountNo:      "account-1",
		ConsumerGroup:  "missing",
		Message:        message,
		CurrentAttempt: 0,
	})
	if !errors.Is(err, delayApp.ErrRetryPolicyNotFound) {
		t.Fatalf("ScheduleRetry() missing policy error = %v", err)
	}

	_, err = service.ScheduleRetry(context.Background(), delayApp.RetryCommand{
		AccountNo:      "account-1",
		ConsumerGroup:  "analytics",
		Message:        message,
		CurrentAttempt: 1,
	})
	if !errors.Is(err, delayApp.ErrRetryExhausted) {
		t.Fatalf("ScheduleRetry() exhausted error = %v", err)
	}
}

func newDelayServiceForTest(
	t *testing.T,
	now time.Time,
	repo delayDomain.DelayTaskRepository,
	publisher delayDomain.LevelPublisher,
	policies map[string]delayApp.RetryPolicy,
) *delayApp.DelayService {
	t.Helper()
	config := delayApp.DefaultDelayServiceConfig()
	config.Clock = delayServiceClock{now: now}
	config.RetryPolicies = policies
	service, err := delayApp.NewDelayService(repo, publisher, config)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func delayServiceTopicTask(t *testing.T, id string, targetAt time.Time) delayDomain.Task {
	t.Helper()
	task, err := delayDomain.NewTask(
		id,
		"account-1",
		delayServiceMessage(t),
		messageDomain.TopicTarget(),
		0,
		targetAt.UnixMilli(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func delayServiceMessage(t *testing.T) messageDomain.Message {
	t.Helper()
	message, err := messageDomain.New(
		"message-1",
		"chat.message.created.v1",
		map[string]string{"trace-id": "trace-1"},
		[]byte("payload"),
		time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

type delayServiceClock struct {
	now time.Time
}

func (c delayServiceClock) Now() time.Time {
	return c.now
}

type delayServicePublisherStub struct {
	publish func(context.Context, int, delayDomain.Task) error
}

func (p *delayServicePublisherStub) Publish(
	ctx context.Context,
	level int,
	task delayDomain.Task,
) error {
	if p.publish == nil {
		return errors.New("unexpected Publish call")
	}
	return p.publish(ctx, level, task)
}

type delayServiceRepositoryStub struct {
	create func(context.Context, delayDomain.Task) (delayDomain.Task, bool, error)
}

func (r *delayServiceRepositoryStub) Create(
	ctx context.Context,
	task delayDomain.Task,
) (delayDomain.Task, bool, error) {
	if r.create == nil {
		return delayDomain.Task{}, false, errors.New("unexpected Create call")
	}
	return r.create(ctx, task)
}

func (*delayServiceRepositoryStub) Get(
	context.Context,
	string,
	string,
) (delayDomain.Task, error) {
	return delayDomain.Task{}, errors.New("unexpected Get call")
}

func (*delayServiceRepositoryStub) ClaimDue(
	context.Context,
	time.Time,
	time.Time,
	time.Time,
	int,
	string,
) ([]delayDomain.Task, error) {
	return nil, errors.New("unexpected ClaimDue call")
}

func (*delayServiceRepositoryStub) MarkLevelQueued(
	context.Context,
	string,
	string,
	int64,
) error {
	return errors.New("unexpected MarkLevelQueued call")
}

func (*delayServiceRepositoryStub) Release(
	context.Context,
	string,
	string,
	int64,
	error,
) error {
	return errors.New("unexpected Release call")
}

func (*delayServiceRepositoryStub) Cancel(
	context.Context,
	string,
	string,
	int64,
) error {
	return errors.New("unexpected Cancel call")
}
