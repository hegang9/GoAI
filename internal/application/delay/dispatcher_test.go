package delay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	delayDomain "GopherAI/internal/domain/delay"
)

func TestNewDispatcher(t *testing.T) {
	publisher := &finalPublisherStub{}
	dispatcher, err := NewDispatcher(publisher, 32)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if dispatcher.publisher != publisher {
		t.Fatal("NewDispatcher() did not retain publisher")
	}
	if dispatcher.ready == nil || cap(dispatcher.ready) != 32 {
		t.Fatalf("NewDispatcher() ready capacity = %d, want 32", cap(dispatcher.ready))
	}
	if dispatcher.wheel == nil {
		t.Fatal("NewDispatcher() wheel = nil")
	}
}

func TestNewDispatcherRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name          string
		publisher     delayDomain.FinalPublisher
		readyCapacity int
		wantError     string
	}{
		{name: "nil publisher", readyCapacity: 1, wantError: "publisher is nil"},
		{name: "zero capacity", publisher: &finalPublisherStub{}, wantError: "capacity must be positive"},
		{name: "negative capacity", publisher: &finalPublisherStub{}, readyCapacity: -1, wantError: "capacity must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDispatcher(tt.publisher, tt.readyCapacity)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("NewDispatcher() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestDispatcherRunPublishesThenACKs(t *testing.T) {
	published := false
	publisher := &finalPublisherStub{publish: func(_ context.Context, task delayDomain.Task) error {
		published = true
		return nil
	}}
	dispatcher, err := NewDispatcher(publisher, 1)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- dispatcher.Run(ctx) }()

	acked := make(chan struct{})
	task := wheelTask(t, "schedule-run-success", time.Now().Add(-time.Second).UnixMilli())
	if err := dispatcher.Submit(ctx, task, func() error {
		if !published {
			t.Error("Dispatcher ACKed before FinalPublisher succeeded")
		}
		close(acked)
		return nil
	}); err != nil {
		cancel()
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case <-acked:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for ACK")
	}
	cancel()
	if err := <-runResult; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestDispatcherRunDoesNotACKPublishFailure(t *testing.T) {
	publishErr := errors.New("broker confirm timeout")
	publisher := &finalPublisherStub{publish: func(context.Context, delayDomain.Task) error {
		return publishErr
	}}
	dispatcher, err := NewDispatcher(publisher, 1)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	acked := false
	task := wheelTask(t, "schedule-run-publish-error", time.Now().Add(-time.Second).UnixMilli())
	if err := dispatcher.Submit(context.Background(), task, func() error {
		acked = true
		return nil
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	err = dispatcher.Run(context.Background())
	if !errors.Is(err, publishErr) {
		t.Fatalf("Run() error = %v, want publish error", err)
	}
	if acked {
		t.Fatal("Dispatcher ACKed after FinalPublisher failure")
	}
}

func TestDispatcherRunReturnsACKFailure(t *testing.T) {
	ackErr := errors.New("delivery ACK failed")
	dispatcher, err := NewDispatcher(&finalPublisherStub{}, 1)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	task := wheelTask(t, "schedule-run-ack-error", time.Now().Add(-time.Second).UnixMilli())
	if err := dispatcher.Submit(context.Background(), task, func() error { return ackErr }); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	err = dispatcher.Run(context.Background())
	if !errors.Is(err, ackErr) {
		t.Fatalf("Run() error = %v, want ACK error", err)
	}
}

func TestDispatcherRunCancelsInFlightPublish(t *testing.T) {
	publishStarted := make(chan struct{})
	publisher := &finalPublisherStub{publish: func(ctx context.Context, _ delayDomain.Task) error {
		close(publishStarted)
		<-ctx.Done()
		return ctx.Err()
	}}
	dispatcher, err := NewDispatcher(publisher, 1)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- dispatcher.Run(ctx) }()
	task := wheelTask(t, "schedule-run-cancel", time.Now().Add(-time.Second).UnixMilli())
	if err := dispatcher.Submit(ctx, task, func() error {
		t.Error("Dispatcher ACKed a cancelled publish")
		return nil
	}); err != nil {
		cancel()
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case <-publishStarted:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for publish")
	}
	cancel()
	select {
	case err := <-runResult:
		if err != nil {
			t.Fatalf("Run() shutdown error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestDispatcherRunRejectsUnexpectedReadyClose(t *testing.T) {
	dispatcher, err := NewDispatcher(&finalPublisherStub{}, 1)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	close(dispatcher.ready)

	err = dispatcher.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "closed unexpectedly") {
		t.Fatalf("Run() error = %v, want ready-closed error", err)
	}
}

func TestDispatcherSubmitExpiredTaskToReady(t *testing.T) {
	ready := make(chan dispatchItem, 1)
	dispatcher := &Dispatcher{
		publisher: &finalPublisherStub{},
		ready:     ready,
		wheel:     newWheel(ready),
	}
	acked := false
	task := wheelTask(t, "schedule-submit-expired", time.Now().Add(-time.Second).UnixMilli())

	err := dispatcher.Submit(context.Background(), task, func() error {
		acked = true
		return nil
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case item := <-ready:
		if item.task.ID != task.ID {
			t.Fatalf("ready task = %q, want %q", item.task.ID, task.ID)
		}
		if acked {
			t.Fatal("Submit() called ACK")
		}
		if item.ack == nil {
			t.Fatal("Submit() lost ACK callback")
		}
	default:
		t.Fatal("Submit() did not enqueue expired task")
	}
}

func TestDispatcherSubmitExpiredTaskHonorsCancellation(t *testing.T) {
	ready := make(chan dispatchItem)
	dispatcher := &Dispatcher{
		publisher: &finalPublisherStub{},
		ready:     ready,
		wheel:     newWheel(ready),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := dispatcher.Submit(
		ctx,
		wheelTask(t, "schedule-submit-cancelled", time.Now().Add(-time.Second).UnixMilli()),
		func() error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Submit() error = %v, want context cancellation", err)
	}
}

func TestDispatcherSubmitFutureTaskToWheel(t *testing.T) {
	ready := make(chan dispatchItem, 1)
	dispatcher := &Dispatcher{
		publisher: &finalPublisherStub{},
		ready:     ready,
		wheel:     newWheel(ready),
	}
	ctx, cancel := context.WithCancel(context.Background())
	wheelDone := make(chan struct{})
	go func() {
		dispatcher.wheel.run(ctx)
		close(wheelDone)
	}()

	targetAt := time.Now().Add(30 * time.Millisecond).UnixMilli()
	task := wheelTask(t, "schedule-submit-future", targetAt)
	if err := dispatcher.Submit(ctx, task, func() error { return nil }); err != nil {
		cancel()
		<-wheelDone
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case item := <-ready:
		if item.task.ID != task.ID {
			t.Fatalf("ready task = %q, want %q", item.task.ID, task.ID)
		}
		if time.Now().UnixMilli() < targetAt {
			t.Fatal("Submit() allowed future task to trigger early")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for future task")
	}

	cancel()
	select {
	case <-wheelDone:
	case <-time.After(time.Second):
		t.Fatal("wheel did not stop")
	}
}

func TestDispatcherSubmitRejectsInvalidInput(t *testing.T) {
	ready := make(chan dispatchItem, 1)
	dispatcher := &Dispatcher{
		publisher: &finalPublisherStub{},
		ready:     ready,
		wheel:     newWheel(ready),
	}
	validTask := wheelTask(t, "schedule-submit-valid", time.Now().UnixMilli())

	tests := []struct {
		name       string
		dispatcher *Dispatcher
		ctx        context.Context
		ack        func() error
		wantError  string
	}{
		{name: "nil dispatcher", ctx: context.Background(), ack: func() error { return nil }, wantError: "dispatcher is nil"},
		{name: "nil context", dispatcher: dispatcher, ack: func() error { return nil }, wantError: "context is nil"},
		{name: "nil ACK", dispatcher: dispatcher, ctx: context.Background(), wantError: "ACK callback is nil"},
		{name: "nil ready", dispatcher: &Dispatcher{publisher: &finalPublisherStub{}, wheel: newWheel(ready)}, ctx: context.Background(), ack: func() error { return nil }, wantError: "ready channel is nil"},
		{name: "nil wheel", dispatcher: &Dispatcher{publisher: &finalPublisherStub{}, ready: ready}, ctx: context.Background(), ack: func() error { return nil }, wantError: "wheel is nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dispatcher.Submit(tt.ctx, validTask, tt.ack)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Submit() error = %v, want %q", err, tt.wantError)
			}
		})
	}

	invalidTask := validTask
	invalidTask.ID = ""
	err := dispatcher.Submit(context.Background(), invalidTask, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "validate") {
		t.Fatalf("Submit() invalid task error = %v", err)
	}
}

type finalPublisherStub struct {
	publish func(context.Context, delayDomain.Task) error
}

func (p *finalPublisherStub) Publish(ctx context.Context, task delayDomain.Task) error {
	if p.publish == nil {
		return nil
	}
	return p.publish(ctx, task)
}
