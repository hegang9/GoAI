package rabbitmq

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"GopherAI/internal/domain/chat"
)

func TestBuildTopicPublishing(t *testing.T) {
	timestamp := time.Date(2026, 8, 28, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	message, err := buildTopicPublishing(chat.Message{
		ID:        "message-1",
		SessionID: "session-1",
		Content:   "hello",
		AccountNo: "account-1",
		IsUser:    true,
	}, "chat.message.created.v1", timestamp)
	if err != nil {
		t.Fatalf("buildTopicPublishing() error = %v", err)
	}

	if message.MessageId != "message-1" ||
		message.Type != "chat.message.created.v1" ||
		message.ContentType != "application/json" ||
		message.Timestamp != timestamp.UTC() {
		t.Fatalf("buildTopicPublishing() message = %+v", message)
	}
	if got := message.Headers[messageTopicHeader]; got != "chat.message.created.v1" {
		t.Fatalf("topic header = %#v, want chat.message.created.v1", got)
	}
	if got := message.Headers[retryAttemptHeader]; got != int64(0) {
		t.Fatalf("retry attempt header = %#v, want int64(0)", got)
	}
	if _, exists := message.Headers["x-retry-count"]; exists {
		t.Fatal("legacy retry count header must not be published")
	}

	var body payload
	if err := json.Unmarshal(message.Body, &body); err != nil {
		t.Fatalf("decode publishing body: %v", err)
	}
	if body.SchemaVersion != payloadSchemaVersion ||
		body.MessageID != "message-1" ||
		body.SessionID != "session-1" ||
		body.Content != "hello" ||
		body.AccountNo != "account-1" ||
		!body.IsUser {
		t.Fatalf("publishing body = %+v", body)
	}
}

func TestValidatePublisherConfig(t *testing.T) {
	tests := []struct {
		name      string
		exchange  string
		topic     string
		timeout   time.Duration
		wantError string
	}{
		{name: "valid", exchange: "gopherai.events", topic: "chat.message.created.v1", timeout: 3 * time.Second},
		{name: "empty exchange", topic: "chat.message.created.v1", timeout: time.Second, wantError: "exchange"},
		{name: "empty topic", exchange: "gopherai.events", timeout: time.Second, wantError: "topic"},
		{name: "invalid timeout", exchange: "gopherai.events", topic: "chat.message.created.v1", wantError: "timeout"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePublisherConfig(test.exchange, test.topic, test.timeout)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validatePublisherConfig() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validatePublisherConfig() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodeMessage(t *testing.T) {
	body := []byte(`{
		"schema_version": 1,
		"message_id": "message-1",
		"session_id": "session-1",
		"content": "hello",
		"account_no": "account-1",
		"is_user": true
	}`)

	message, err := decodeMessage(body)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if message.ID != "message-1" ||
		message.SessionID != "session-1" ||
		message.Content != "hello" ||
		message.AccountNo != "account-1" ||
		!message.IsUser {
		t.Fatalf("decodeMessage() message = %+v", message)
	}
}

func TestDecodeMessageRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name      string
		body      []byte
		wantError string
	}{
		{
			name:      "invalid JSON",
			body:      []byte(`{`),
			wantError: "decode payload failed",
		},
		{
			name: "unsupported schema version",
			body: []byte(`{
				"schema_version": 2,
				"message_id": "message-1"
			}`),
			wantError: "unsupported payload schema version",
		},
		{
			name: "empty message ID",
			body: []byte(`{
				"schema_version": 1
			}`),
			wantError: "message_id is empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeMessage(test.body)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("decodeMessage() error = %v, want %q", err, test.wantError)
			}
			if classifyError(err) != FailurePermanent {
				t.Fatalf(
					"decodeMessage() failure kind = %s, want permanent",
					classifyError(err),
				)
			}
		})
	}
}
