package rabbitmq

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	chatDomain "GopherAI/internal/domain/chat"
	messageDomain "GopherAI/internal/domain/message"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestNewGroupConsumerRejectsInvalidInput(t *testing.T) {
	handle := func(context.Context, chatDomain.Message) error { return nil }
	scheduleRetry := func(
		context.Context,
		string,
		string,
		messageDomain.Message,
		uint32,
	) (bool, error) {
		return false, nil
	}
	tests := []struct {
		name          string
		client        *Client
		consumerGroup string
		queue         string
		prefetch      int
		handle        func(context.Context, chatDomain.Message) error
		scheduleRetry func(context.Context, string, string, messageDomain.Message, uint32) (bool, error)
		wantError     string
	}{
		{name: "empty consumer group", queue: "events.persistence", prefetch: 1, handle: handle, scheduleRetry: scheduleRetry, wantError: "consumer group is empty"},
		{name: "empty queue", consumerGroup: "persistence", prefetch: 1, handle: handle, scheduleRetry: scheduleRetry, wantError: "queue is empty"},
		{name: "zero prefetch", consumerGroup: "persistence", queue: "events.persistence", handle: handle, scheduleRetry: scheduleRetry, wantError: "prefetch count must be positive"},
		{name: "nil handler", consumerGroup: "persistence", queue: "events.persistence", prefetch: 1, scheduleRetry: scheduleRetry, wantError: "handler is nil"},
		{name: "nil schedule retry", consumerGroup: "persistence", queue: "events.persistence", prefetch: 1, handle: handle, wantError: "schedule retry is nil"},
		{name: "nil client", consumerGroup: "persistence", queue: "events.persistence", prefetch: 1, handle: handle, scheduleRetry: scheduleRetry, wantError: "client is unavailable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewGroupConsumer(
				test.client,
				test.consumerGroup,
				test.queue,
				test.prefetch,
				"gopherai.events.dlx",
				"persistence",
				time.Second,
				test.handle,
				test.scheduleRetry,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewGroupConsumer() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestNewGroupConsumerRejectsInvalidDeadLetterConfig(t *testing.T) {
	handle := func(context.Context, chatDomain.Message) error { return nil }
	scheduleRetry := func(
		context.Context,
		string,
		string,
		messageDomain.Message,
		uint32,
	) (bool, error) {
		return false, nil
	}
	tests := []struct {
		name       string
		exchange   string
		routingKey string
		timeout    time.Duration
		wantError  string
	}{
		{name: "empty exchange", routingKey: "persistence", timeout: time.Second, wantError: "dead letter exchange is empty"},
		{name: "empty routing key", exchange: "gopherai.events.dlx", timeout: time.Second, wantError: "dead letter routing key is empty"},
		{name: "zero confirm timeout", exchange: "gopherai.events.dlx", routingKey: "persistence", wantError: "confirm timeout must be positive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewGroupConsumer(
				nil,
				"persistence",
				"events.persistence",
				1,
				test.exchange,
				test.routingKey,
				test.timeout,
				handle,
				scheduleRetry,
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

func TestGroupConsumerHandleDeliveryACKsScheduledRetry(t *testing.T) {
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
		scheduleRetry: func(
			_ context.Context,
			accountNo string,
			consumerGroup string,
			message messageDomain.Message,
			currentAttempt uint32,
		) (bool, error) {
			if accountNo != "account-1" || consumerGroup != "persistence" {
				t.Fatalf("retry target = %q/%q", accountNo, consumerGroup)
			}
			if message.ID != delivery.MessageId || currentAttempt != 2 {
				t.Fatalf("retry message = %q attempt=%d", message.ID, currentAttempt)
			}
			return false, nil
		},
	}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if !acknowledger.acked {
		t.Fatal("scheduled retry was not ACKed")
	}
}

func TestGroupConsumerHandleDeliveryDoesNotACKScheduleFailure(t *testing.T) {
	scheduleError := errors.New("delay service unavailable")
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(context.Context, chatDomain.Message) error {
			return errors.New("database unavailable")
		},
		scheduleRetry: func(context.Context, string, string, messageDomain.Message, uint32) (bool, error) {
			return false, scheduleError
		},
	}

	err := consumer.handleDelivery(context.Background(), delivery)
	if !errors.Is(err, scheduleError) {
		t.Fatalf("handleDelivery() error = %v, want schedule error", err)
	}
	if acknowledger.acked {
		t.Fatal("schedule failure was ACKed")
	}
}

func TestGroupConsumerHandleDeliveryACKsExhaustedRetryAfterDLQ(t *testing.T) {
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger
	dlqCalled := false

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(context.Context, chatDomain.Message) error {
			return errors.New("database unavailable")
		},
		scheduleRetry: func(context.Context, string, string, messageDomain.Message, uint32) (bool, error) {
			return true, nil
		},
		publishDeadLetter: func(_ context.Context, got amqp.Delivery, kind FailureKind, cause error) error {
			dlqCalled = true
			if got.MessageId != delivery.MessageId || kind != FailureTransient {
				t.Fatalf("DLQ delivery=%q kind=%s", got.MessageId, kind)
			}
			if cause == nil || !strings.Contains(cause.Error(), "retry exhausted") {
				t.Fatalf("DLQ cause = %v", cause)
			}
			return nil
		},
	}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if !dlqCalled || !acknowledger.acked {
		t.Fatalf("DLQ called=%t ACK=%t", dlqCalled, acknowledger.acked)
	}
}

func TestGroupConsumerHandleDeliveryACKsPermanentFailureAfterDLQ(t *testing.T) {
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger
	dlqCalled := false

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(context.Context, chatDomain.Message) error {
			return permanentError(errors.New("invalid business message"))
		},
		publishDeadLetter: func(_ context.Context, got amqp.Delivery, kind FailureKind, cause error) error {
			dlqCalled = true
			if got.MessageId != delivery.MessageId || kind != FailurePermanent || cause == nil {
				t.Fatalf("DLQ delivery=%q kind=%s cause=%v", got.MessageId, kind, cause)
			}
			return nil
		},
	}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if !dlqCalled || !acknowledger.acked {
		t.Fatalf("DLQ called=%t ACK=%t", dlqCalled, acknowledger.acked)
	}
}

func TestGroupConsumerHandleDeliveryACKsInvalidDeliveryAfterDLQ(t *testing.T) {
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Headers[retryAttemptHeader] = "invalid"
	delivery.Acknowledger = acknowledger
	dlqCalled := false

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		publishDeadLetter: func(_ context.Context, got amqp.Delivery, kind FailureKind, cause error) error {
			dlqCalled = true
			if got.MessageId != delivery.MessageId || kind != FailurePermanent || cause == nil {
				t.Fatalf("DLQ delivery=%q kind=%s cause=%v", got.MessageId, kind, cause)
			}
			return nil
		},
	}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if !dlqCalled || !acknowledger.acked {
		t.Fatalf("DLQ called=%t ACK=%t", dlqCalled, acknowledger.acked)
	}
}

func TestGroupConsumerHandleDeliveryDoesNotACKAbort(t *testing.T) {
	abortCause := errors.New("consumer shutting down")
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(context.Context, chatDomain.Message) error {
			return abortError(abortCause)
		},
	}

	err := consumer.handleDelivery(context.Background(), delivery)
	if !errors.Is(err, abortCause) {
		t.Fatalf("handleDelivery() error = %v, want abort cause", err)
	}
	if acknowledger.acked {
		t.Fatal("abort failure was ACKed")
	}
}

func TestGroupConsumerHandleDeliveryDoesNotACKDLQFailure(t *testing.T) {
	dlqError := errors.New("DLQ unavailable")
	acknowledger := &groupAcknowledger{}
	delivery := validGroupDelivery()
	delivery.Acknowledger = acknowledger

	consumer := &GroupConsumer{
		consumerGroup: "persistence",
		handle: func(context.Context, chatDomain.Message) error {
			return permanentError(errors.New("invalid business message"))
		},
		publishDeadLetter: func(context.Context, amqp.Delivery, FailureKind, error) error {
			return dlqError
		},
	}

	err := consumer.handleDelivery(context.Background(), delivery)
	if !errors.Is(err, dlqError) {
		t.Fatalf("handleDelivery() error = %v, want DLQ error", err)
	}
	if acknowledger.acked {
		t.Fatal("DLQ publish failure was ACKed")
	}
}

func TestBuildGroupDeadLetterPublishing(t *testing.T) {
	delivery := validGroupDelivery()
	delivery.Exchange = "gopherai.events"
	delivery.RoutingKey = "chat.message.created.v1"
	delivery.Expiration = "60000"
	originalBody := append([]byte(nil), delivery.Body...)
	deadLetteredAt := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)

	message, err := buildGroupDeadLetterPublishing(
		"persistence",
		delivery,
		FailurePermanent,
		errors.New("invalid payload"),
		deadLetteredAt,
	)
	if err != nil {
		t.Fatalf("buildGroupDeadLetterPublishing() error = %v", err)
	}
	if message.Headers["x-consumer-group"] != "persistence" ||
		message.Headers["x-failure-kind"] != "permanent" ||
		message.Headers["x-original-exchange"] != delivery.Exchange ||
		message.Headers["x-original-routing-key"] != delivery.RoutingKey {
		t.Fatalf("DLQ headers = %+v", message.Headers)
	}
	if message.Expiration != "" || message.DeliveryMode != amqp.Persistent {
		t.Fatalf("DLQ expiration=%q deliveryMode=%d", message.Expiration, message.DeliveryMode)
	}
	if message.MessageId != delivery.MessageId || string(message.Body) != string(originalBody) {
		t.Fatalf("DLQ message ID=%q body=%q", message.MessageId, message.Body)
	}
	message.Body[0] = 'x'
	if string(delivery.Body) != string(originalBody) {
		t.Fatal("DLQ publishing shares delivery body storage")
	}
	if _, exists := delivery.Headers["x-final-error"]; exists {
		t.Fatal("DLQ builder mutated delivery headers")
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
