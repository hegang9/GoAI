package persistence

import (
	"context"
	"testing"

	"GopherAI/internal/domain/chat"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestContextRepositoryGetUsesTenantAndSession(t *testing.T) {
	t.Parallel()
	repo, mock := newContextRepositoryForTest(t)
	mock.ExpectQuery("SELECT \\* FROM `conversation_contexts` WHERE account_no = \\? AND session_id = \\?").
		WithArgs("account-1", "session-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "account_no", "core_memory", "summary", "covered_message_id", "version",
		}).AddRow("session-1", "account-1", "偏好简洁回答", "已经完成设计", "message-8", uint64(3)))

	snapshot, found, err := repo.Get(context.Background(), "account-1", "session-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("Get() found = false, want true")
	}
	if snapshot.CoreMemory != "偏好简洁回答" || snapshot.CoveredMessageID != "message-8" || snapshot.Version != 3 {
		t.Fatalf("Get() snapshot = %+v", snapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestContextRepositorySaveUpsertsSnapshot(t *testing.T) {
	t.Parallel()
	repo, mock := newContextRepositoryForTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `conversation_contexts`").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.Save(context.Background(), chat.ContextSnapshot{
		AccountNo:        "account-1",
		SessionID:        "session-1",
		CoreMemory:       "偏好简洁回答",
		Summary:          "已经完成设计",
		CoveredMessageID: "message-8",
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newContextRepositoryForTest(t *testing.T) (*ContextRepository, sqlmock.Sqlmock) {
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
	return NewContextRepository(db), mock
}
