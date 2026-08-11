package delay

import (
	"testing"
	"time"

	messageDomain "GopherAI/internal/domain/message"
)

func TestNewTaskUsesCanonicalMessageAndDefaults(t *testing.T) {
	t.Parallel()

	msg := validMessage(t)
	task, err := NewTask(
		"schedule-1",
		"account-1",
		msg,
		messageDomain.TopicTarget(),
		0,
		time.Now().Add(time.Minute).UnixMilli(),
	)
	if err != nil {
		t.Fatalf("NewTask() error = %v", err)
	}

	if task.Version != 1 || task.Status != StatusPending {
		t.Fatalf("NewTask() version/status = %d/%d, want 1/%d", task.Version, task.Status, StatusPending)
	}
	if task.Message.ID != msg.ID || string(task.Message.Body) != string(msg.Body) {
		t.Fatalf("NewTask() Message = %+v, want canonical message", task.Message)
	}

	msg.Headers["trace-id"] = "changed"
	msg.Body[0] = 'X'
	if task.Message.Headers["trace-id"] != "trace-1" || string(task.Message.Body) != "payload" {
		t.Fatalf("NewTask() retained mutable message aliases: %+v", task.Message)
	}
}

func TestNewTaskValidatesTargetAndAttemptTogether(t *testing.T) {
	t.Parallel()

	msg := validMessage(t)
	targetAt := time.Now().Add(time.Minute).UnixMilli()
	groupTarget, err := messageDomain.ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		target  messageDomain.Target
		attempt uint32
	}{
		{name: "topic cannot be retry attempt", target: messageDomain.TopicTarget(), attempt: 1},
		{name: "consumer group must be retry attempt", target: groupTarget, attempt: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewTask("schedule-1", "account-1", msg, tt.target, tt.attempt, targetAt); err == nil {
				t.Fatal("NewTask() error = nil, want validation error")
			}
		})
	}
}

func TestRestoreTaskValidatesPersistedState(t *testing.T) {
	t.Parallel()

	msg := validMessage(t)
	targetAt := time.Now().Add(time.Minute).UnixMilli()

	task, err := RestoreTask(
		"schedule-1",
		"account-1",
		msg,
		messageDomain.TopicTarget(),
		0,
		targetAt,
		3,
		StatusDispatching,
	)
	if err != nil {
		t.Fatalf("RestoreTask() error = %v", err)
	}
	if task.Version != 3 || task.Status != StatusDispatching {
		t.Fatalf("RestoreTask() version/status = %d/%d", task.Version, task.Status)
	}

	if _, err := RestoreTask(
		"schedule-1",
		"account-1",
		msg,
		messageDomain.TopicTarget(),
		0,
		targetAt,
		1,
		Status(99),
	); err == nil {
		t.Fatal("RestoreTask() error = nil for invalid status")
	}
}

func TestStatusPersistentValues(t *testing.T) {
	t.Parallel()

	if StatusPending != 1 ||
		StatusLevelQueued != 2 ||
		StatusDispatching != 3 ||
		StatusCancelled != 4 {
		t.Fatalf(
			"status values = pending:%d level:%d dispatching:%d cancelled:%d",
			StatusPending,
			StatusLevelQueued,
			StatusDispatching,
			StatusCancelled,
		)
	}
}

func validMessage(t *testing.T) messageDomain.Message {
	t.Helper()
	msg, err := messageDomain.New(
		"message-1",
		"chat.message.created.v1",
		map[string]string{"trace-id": "trace-1"},
		[]byte("payload"),
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}
