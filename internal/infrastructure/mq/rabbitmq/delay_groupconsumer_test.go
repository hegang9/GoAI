package rabbitmq

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	chatDomain "GopherAI/internal/domain/chat"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestNewGroupConsumerRejectsInvalidInput(t *testing.T) {
	handle := func(context.Context, chatDomain.Message) error { return nil }
	tests := []struct {
		name          string
		client        *Client
		consumerGroup string
		queue         string
		prefetch      int
		handle        func(context.Context, chatDomain.Message) error
		wantError     string
	}{
		{name: "empty consumer group", queue: "events.persistence", prefetch: 1, handle: handle, wantError: "consumer group is empty"},
		{name: "empty queue", consumerGroup: "persistence", prefetch: 1, handle: handle, wantError: "queue is empty"},
		{name: "zero prefetch", consumerGroup: "persistence", queue: "events.persistence", handle: handle, wantError: "prefetch count must be positive"},
		{name: "nil handler", consumerGroup: "persistence", queue: "events.persistence", prefetch: 1, wantError: "handler is nil"},
		{name: "nil client", consumerGroup: "persistence", queue: "events.persistence", prefetch: 1, handle: handle, wantError: "client is unavailable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewGroupConsumer(
				test.client,
				test.consumerGroup,
				test.queue,
				test.prefetch,
				test.handle,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewGroupConsumer() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodeGroupDeliveryRestoresBusinessAndRetryMessages(t *testing.T) {
	delivery := validGroupDelivery()

	businessMessage, retryMessage, attempt, err := decodeGroupDelivery(delivery)
	if err != nil {
		t.Fatalf("decodeGroupDelivery() error = %v", err)
	}
	if businessMessage.ID != "message-1" ||
		businessMessage.SessionID != "session-1" ||
		businessMessage.AccountNo != "account-1" {
		t.Fatalf("business message = %+v", businessMessage)
	}
	if retryMessage.ID != delivery.MessageId ||
		retryMessage.Topic != delivery.Type ||
		retryMessage.Headers["trace-id"] != "trace-1" ||
		string(retryMessage.Body) != string(delivery.Body) ||
		!retryMessage.Timestamp.Equal(delivery.Timestamp) {
		t.Fatalf("retry message = %+v", retryMessage)
	}
	if _, exists := retryMessage.Headers[messageTopicHeader]; exists {
		t.Fatal("retry message retained system topic header")
	}
	if _, exists := retryMessage.Headers[retryAttemptHeader]; exists {
		t.Fatal("retry message retained system attempt header")
	}
	if attempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", attempt)
	}
}

func TestDecodeGroupDeliveryRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*amqp.Delivery)
		wantError string
	}{
		{
			name: "message ID mismatch",
			change: func(delivery *amqp.Delivery) {
				delivery.MessageId = "message-2"
			},
			wantError: "message ID mismatch",
		},
		{
			name: "topic mismatch",
			change: func(delivery *amqp.Delivery) {
				delivery.Headers[messageTopicHeader] = "chat.message.updated.v1"
			},
			wantError: "topic mismatch",
		},
		{
			name: "invalid retry attempt",
			change: func(delivery *amqp.Delivery) {
				delivery.Headers[retryAttemptHeader] = "2"
			},
			wantError: "invalid x-retry-attempt type",
		},
		{
			name: "unsupported business header",
			change: func(delivery *amqp.Delivery) {
				delivery.Headers["trace-id"] = int64(1)
			},
			wantError: "unsupported type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delivery := validGroupDelivery()
			test.change(&delivery)

			_, _, _, err := decodeGroupDelivery(delivery)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("decodeGroupDelivery() error = %v, want %q", err, test.wantError)
			}
			if classifyError(err) != FailurePermanent {
				t.Fatalf("failure kind = %s, want permanent", classifyError(err))
			}
		})
	}
}

func TestGroupConsumerHandleDeliveryACKsSuccess(t *testing.T) {
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger
	delivery.DeliveryTag = 7

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(_ context.Context, message chatDomain.Message) error {
			if message.ID != delivery.MessageId {
				t.Fatalf("handler message ID = %q, want %q", message.ID, delivery.MessageId)
			}
			return nil
		},
	}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if !acknowledger.acked || acknowledger.tag != 7 || acknowledger.multiple {
		t.Fatalf("ACK = %+v", acknowledger)
	}
}

func TestGroupConsumerHandleDeliveryDoesNotACKHandlerFailure(t *testing.T) {
	handlerError := errors.New("database unavailable")
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger
	delivery.DeliveryTag = 8

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(context.Context, chatDomain.Message) error {
			return handlerError
		},
	}

	err := consumer.handleDelivery(context.Background(), delivery)
	if !errors.Is(err, handlerError) {
		t.Fatalf("handleDelivery() error = %v, want handler error", err)
	}
	if acknowledger.acked {
		t.Fatal("handler failure was ACKed")
	}
}

func TestGroupConsumerCloseWithoutChannel(t *testing.T) {
	var consumer *GroupConsumer
	if err := consumer.Close(); err != nil {
		t.Fatalf("nil Close() error = %v", err)
	}
	consumer = &GroupConsumer{}
	if err := consumer.Close(); err != nil {
		t.Fatalf("empty Close() error = %v", err)
	}
}

func validGroupDelivery() amqp.Delivery {
	topic := "chat.message.created.v1"
	return amqp.Delivery{
		Headers: amqp.Table{
			messageTopicHeader: "chat.message.created.v1",
			retryAttemptHeader: int64(2),
			"trace-id":         "trace-1",
		},
		MessageId: "message-1",
		Timestamp: time.Date(2026, time.August, 27, 1, 0, 0, 0, time.UTC),
		Type:      topic,
		Body: []byte(`{
			"schema_version": 1,
			"message_id": "message-1",
			"session_id": "session-1",
			"content": "hello",
			"account_no": "account-1",
			"is_user": true
		}`),
	}
}

type groupAcknowledger struct {
	acked    bool
	tag      uint64
	multiple bool
}

func (a *groupAcknowledger) Ack(tag uint64, multiple bool) error {
	a.acked = true
	a.tag = tag
	a.multiple = multiple
	return nil
}

func (*groupAcknowledger) Nack(uint64, bool, bool) error { return nil }

func (*groupAcknowledger) Reject(uint64, bool) error { return nil }
