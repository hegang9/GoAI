package test

import (
	"context"
	"strings"
	"testing"
	"time"

	delayDomain "GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"
	"GopherAI/internal/infrastructure/persistence"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMessageNewCopiesMutableDataAndNormalizesTimestamp(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"trace-id": "trace-1"}
	body := []byte("payload")
	timestamp := time.Date(2026, 8, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	got, err := messageDomain.New("message-1", "chat.message.created.v1", headers, body, timestamp)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	headers["trace-id"] = "changed"
	body[0] = 'X'

	if got.Headers["trace-id"] != "trace-1" {
		t.Fatalf("New() Headers = %v, want defensive copy", got.Headers)
	}
	if string(got.Body) != "payload" {
		t.Fatalf("New() Body = %q, want defensive copy", got.Body)
	}
	if got.Timestamp.Location() != time.UTC {
		t.Fatalf("New() Timestamp location = %v, want UTC", got.Timestamp.Location())
	}
}

func TestMessageNewRejectsInvalidMessage(t *testing.T) {
	t.Parallel()

	validTime := time.Now().UTC()
	tests := []struct {
		name      string
		id        string
		topic     string
		body      []byte
		timestamp time.Time
	}{
		{name: "empty id", topic: "topic", body: []byte{}, timestamp: validTime},
		{name: "empty topic", id: "message-1", body: []byte{}, timestamp: validTime},
		{name: "nil body", id: "message-1", topic: "topic", timestamp: validTime},
		{name: "zero timestamp", id: "message-1", topic: "topic", body: []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := messageDomain.New(tt.id, tt.topic, nil, tt.body, tt.timestamp); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}
}

func TestMessageTargetValidation(t *testing.T) {
	t.Parallel()

	if err := messageDomain.TopicTarget().Validate(); err != nil {
		t.Fatalf("TopicTarget().Validate() error = %v", err)
	}

	groupTarget, err := messageDomain.ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatalf("ConsumerGroupTarget() error = %v", err)
	}
	if groupTarget.Kind != messageDomain.TargetConsumerGroup || groupTarget.ConsumerGroup != "analytics" {
		t.Fatalf("ConsumerGroupTarget() = %+v", groupTarget)
	}

	invalid := []messageDomain.Target{
		{},
		{Kind: messageDomain.TargetTopic, ConsumerGroup: "unexpected"},
		{Kind: messageDomain.TargetConsumerGroup},
		{Kind: messageDomain.TargetKind(99)},
	}
	for _, target := range invalid {
		if err := target.Validate(); err == nil {
			t.Fatalf("Target.Validate() error = nil for %+v", target)
		}
	}
}

func TestDelayNewTaskUsesCanonicalMessageAndDefaults(t *testing.T) {
	t.Parallel()

	msg := validDelayMessage(t)
	task, err := delayDomain.NewTask(
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

	if task.Version != 1 || task.Status != delayDomain.StatusPending {
		t.Fatalf(
			"NewTask() version/status = %d/%d, want 1/%d",
			task.Version,
			task.Status,
			delayDomain.StatusPending,
		)
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

func TestDelayNewTaskValidatesTargetAndAttemptTogether(t *testing.T) {
	t.Parallel()

	msg := validDelayMessage(t)
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
			if _, err := delayDomain.NewTask(
				"schedule-1",
				"account-1",
				msg,
				tt.target,
				tt.attempt,
				targetAt,
			); err == nil {
				t.Fatal("NewTask() error = nil, want validation error")
			}
		})
	}
}

func TestDelayRestoreTaskValidatesPersistedState(t *testing.T) {
	t.Parallel()

	msg := validDelayMessage(t)
	targetAt := time.Now().Add(time.Minute).UnixMilli()

	task, err := delayDomain.RestoreTask(
		"schedule-1",
		"account-1",
		msg,
		messageDomain.TopicTarget(),
		0,
		targetAt,
		3,
		delayDomain.StatusDispatching,
	)
	if err != nil {
		t.Fatalf("RestoreTask() error = %v", err)
	}
	if task.Version != 3 || task.Status != delayDomain.StatusDispatching {
		t.Fatalf("RestoreTask() version/status = %d/%d", task.Version, task.Status)
	}

	if _, err := delayDomain.RestoreTask(
		"schedule-1",
		"account-1",
		msg,
		messageDomain.TopicTarget(),
		0,
		targetAt,
		1,
		delayDomain.Status(99),
	); err == nil {
		t.Fatal("RestoreTask() error = nil for invalid status")
	}
}

func TestDelayStatusPersistentValues(t *testing.T) {
	t.Parallel()

	if delayDomain.StatusPending != 1 ||
		delayDomain.StatusLevelQueued != 2 ||
		delayDomain.StatusDispatching != 3 ||
		delayDomain.StatusCancelled != 4 {
		t.Fatalf(
			"status values = pending:%d level:%d dispatching:%d cancelled:%d",
			delayDomain.StatusPending,
			delayDomain.StatusLevelQueued,
			delayDomain.StatusDispatching,
			delayDomain.StatusCancelled,
		)
	}
}

func validDelayMessage(t *testing.T) messageDomain.Message {
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

func TestDelayTaskRepositoryCreatePreservesDomainMessageAndTarget(t *testing.T) {
	t.Parallel()

	repo, mock := newDelayTaskRepository(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `delay_tasks`").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	timestamp := time.Date(2026, 8, 11, 12, 0, 0, 123000000, time.UTC)
	message, err := messageDomain.New(
		"message-1",
		"chat.message.created.v1",
		map[string]string{"trace-id": "trace-1"},
		[]byte("payload"),
		timestamp,
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := messageDomain.ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatal(err)
	}
	task, err := delayDomain.RestoreTask(
		"schedule-1",
		"account-1",
		message,
		target,
		2,
		time.Now().Add(time.Minute).UnixMilli(),
		3,
		delayDomain.StatusDispatching,
	)
	if err != nil {
		t.Fatal(err)
	}

	stored, created, err := repo.Create(context.Background(), task)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created {
		t.Fatal("Create() created = false, want true")
	}
	if stored.ID != task.ID ||
		stored.Message.ID != task.Message.ID ||
		stored.Message.Topic != task.Message.Topic ||
		stored.Message.Headers["trace-id"] != "trace-1" ||
		string(stored.Message.Body) != "payload" ||
		!stored.Message.Timestamp.Equal(timestamp) ||
		stored.Target.Kind != messageDomain.TargetConsumerGroup ||
		stored.Target.ConsumerGroup != "analytics" ||
		stored.RetryAttempt != 2 {
		t.Fatalf("Create() stored = %+v, want full domain round trip", stored)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDelayTaskRepositoryGetRejectsMalformedHeaders(t *testing.T) {
	t.Parallel()

	repo, mock := newDelayTaskRepository(t)
	mock.ExpectQuery("SELECT \\* FROM `delay_tasks`").
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"account_no",
			"target_at",
			"version",
			"status",
			"message_id",
			"message_topic",
			"message_headers",
			"message_body",
			"message_timestamp_ms",
			"target_kind",
			"target_consumer_group",
			"retry_times",
		}).AddRow(
			"schedule-1",
			"account-1",
			time.Now().Add(time.Minute).UnixMilli(),
			int64(1),
			uint8(delayDomain.StatusPending),
			"message-1",
			"chat.message.created.v1",
			[]byte("{invalid"),
			[]byte("payload"),
			time.Now().UnixMilli(),
			uint8(messageDomain.TargetTopic),
			"",
			uint32(0),
		))

	_, err := repo.Get(context.Background(), "account-1", "schedule-1")
	if err == nil || !strings.Contains(err.Error(), "decode message headers") {
		t.Fatalf("Get() error = %v, want malformed headers error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newDelayTaskRepository(t *testing.T) (*persistence.DelayTaskRepository, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return persistence.NewDelayTaskRepository(db), mock
}
