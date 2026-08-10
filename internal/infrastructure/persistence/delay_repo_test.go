package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"GopherAI/internal/domain/delay"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDelayTaskRepositoryCreate(t *testing.T) {
	t.Parallel()
	repo, mock := newDelayTaskRepositoryForTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `delay_tasks`").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	task := delay.Task{
		ID:          "task-create",
		AccountNo:   "account-1",
		Destination: "notification",
		TargetAt:    time.Now().Add(time.Hour).UnixMilli(),
		Payload:     []byte(`{"message":"hello"}`),
		Version:     1,
		Status:      delay.StatusPending,
	}

	stored, created, err := repo.Create(context.Background(), task)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created {
		t.Fatal("Create() created = false, want true")
	}
	if stored.ID != task.ID || stored.Status != delay.StatusPending {
		t.Fatalf("Create() stored = %+v", stored)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDelayTaskRepositoryClaimDue(t *testing.T) {
	t.Parallel()
	repo, mock := newDelayTaskRepositoryForTest(t)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `delay_tasks`.*FOR UPDATE SKIP LOCKED").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "account_no", "destination", "target_at", "payload", "version", "status",
		}).AddRow(
			"task-due", "account-1", "notification", now.UnixMilli(), []byte("payload"), int64(1), uint8(delay.StatusPending),
		))
	mock.ExpectExec("UPDATE `delay_tasks` SET .*WHERE id IN").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claimed, err := repo.ClaimDue(
		context.Background(), now, now.Add(time.Minute), now.Add(30*time.Second), 10, "poller-1",
	)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() count = %d, want 1", len(claimed))
	}
	if claimed[0].Status != delay.StatusDispatching || claimed[0].Version != 2 {
		t.Fatalf("ClaimDue() task = %+v", claimed[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDelayTaskRepositoryStateTransitions(t *testing.T) {
	t.Parallel()
	repo, mock := newDelayTaskRepositoryForTest(t)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `delay_tasks` SET .*lease_owner.*version").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.MarkLevelQueued(context.Background(), "task-1", "poller-1", 2); err != nil {
		t.Fatalf("MarkLevelQueued() error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `delay_tasks` SET .*lease_owner.*version").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.Release(context.Background(), "task-2", "poller-1", 2, errors.New("publish nack")); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `delay_tasks` SET .*account_no.*version").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.Cancel(context.Background(), "account-1", "task-3", 1); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestErrorSummary(t *testing.T) {
	t.Parallel()
	message := make([]rune, 1025)
	for i := range message {
		message[i] = '错'
	}
	if got := []rune(errorSummary(errors.New(string(message)))); len(got) != 1024 {
		t.Fatalf("errorSummary() rune count = %d, want 1024", len(got))
	}
}

func newDelayTaskRepositoryForTest(t *testing.T) (*DelayTaskRepository, sqlmock.Sqlmock) {
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
	return NewDelayTaskRepository(db), mock
}
