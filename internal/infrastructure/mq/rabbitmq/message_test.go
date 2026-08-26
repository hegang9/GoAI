package rabbitmq

import (
	"context"
	"errors"
	"strings"
	"testing"

	"GopherAI/internal/domain/chat"
)

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

func TestConsumerDecodeDelegatesToHandler(t *testing.T) {
	handlerError := errors.New("store message failed")
	called := false
	consumer := &Consumer{
		handle: func(_ context.Context, message chat.Message) error {
			called = true
			if message.ID != "message-1" {
				t.Fatalf("handler message ID = %q, want message-1", message.ID)
			}
			return handlerError
		},
	}

	err := consumer.decode([]byte(`{
		"schema_version": 1,
		"message_id": "message-1"
	}`))
	if !errors.Is(err, handlerError) {
		t.Fatalf("Consumer.decode() error = %v, want handler error", err)
	}
	if !called {
		t.Fatal("Consumer.decode() did not call handler")
	}
}
