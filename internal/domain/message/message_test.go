package message

import (
	"testing"
	"time"
)

func TestNewCopiesMutableDataAndNormalizesTimestamp(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"trace-id": "trace-1"}
	body := []byte("payload")
	timestamp := time.Date(2026, 8, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	got, err := New("message-1", "chat.message.created.v1", headers, body, timestamp)
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

func TestNewRejectsInvalidMessage(t *testing.T) {
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
			if _, err := New(tt.id, tt.topic, nil, tt.body, tt.timestamp); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}
}
