package rabbitmq

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestFailureKindString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind FailureKind
		want string
	}{
		{name: "transient", kind: FailureTransient, want: "transient"},
		{name: "permanent", kind: FailurePermanent, want: "permanent"},
		{name: "abort", kind: FailureAbort, want: "abort"},
		{name: "unknown", kind: FailureKind(99), want: "unknown(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.kind.String(); got != tt.want {
				t.Fatalf("FailureKind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	transientErr := errors.New("database timeout")
	permanentErr := permanentError(errors.New("invalid payload"))
	abortedErr := abortError(errors.New("database schema mismatch"))

	tests := []struct {
		name string
		err  error
		want FailureKind
	}{
		{name: "nil defaults to transient", want: FailureTransient},
		{name: "unmarked defaults to transient", err: transientErr, want: FailureTransient},
		{name: "permanent", err: permanentErr, want: FailurePermanent},
		{name: "wrapped permanent", err: fmt.Errorf("consume failed: %w", permanentErr), want: FailurePermanent},
		{name: "abort", err: abortedErr, want: FailureAbort},
		{name: "wrapped abort", err: fmt.Errorf("consume failed: %w", abortedErr), want: FailureAbort},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyError(tt.err); got != tt.want {
				t.Fatalf("classifyError() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRetryCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    amqp.Table
		want       int
		wantErrSub string
	}{
		{name: "nil headers", want: 0},
		{name: "missing header", headers: amqp.Table{"trace_id": "trace-1"}, want: 0},
		{name: "int8", headers: amqp.Table{retryCountHeader: int8(1)}, want: 1},
		{name: "int16", headers: amqp.Table{retryCountHeader: int16(2)}, want: 2},
		{name: "int32", headers: amqp.Table{retryCountHeader: int32(3)}, want: 3},
		{name: "int64", headers: amqp.Table{retryCountHeader: int64(4)}, want: 4},
		{name: "int", headers: amqp.Table{retryCountHeader: 5}, want: 5},
		{
			name:       "negative",
			headers:    amqp.Table{retryCountHeader: int32(-1)},
			wantErrSub: "negative retry count",
		},
		{
			name:       "unsupported type",
			headers:    amqp.Table{retryCountHeader: "1"},
			wantErrSub: "invalid x-retry-count type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := retryCount(tt.headers)
			if tt.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("retryCount() error = %v, want substring %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("retryCount() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("retryCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCopyHeadersDoesNotMutateSource(t *testing.T) {
	t.Parallel()

	source := amqp.Table{"trace_id": "trace-1"}
	copied := copyHeaders(source)
	copied[retryCountHeader] = int32(1)

	if _, exists := source[retryCountHeader]; exists {
		t.Fatal("copyHeaders() mutation leaked into source")
	}
	if got := copied["trace_id"]; got != "trace-1" {
		t.Fatalf("copyHeaders() trace_id = %v, want trace-1", got)
	}
}

func TestSelectRetryTier(t *testing.T) {
	t.Parallel()

	tiers := []RetryTier{
		{Queue: "retry.1", RoutingKey: "retry.1", DelayMs: 10000},
		{Queue: "retry.2", RoutingKey: "retry.2", DelayMs: 30000},
	}

	tests := []struct {
		name       string
		count      int
		wantQueue  string
		wantErrSub string
	}{
		{name: "first tier", count: 0, wantQueue: "retry.1"},
		{name: "second tier", count: 1, wantQueue: "retry.2"},
		{name: "negative count", count: -1, wantErrSub: "invalid retry count"},
		{name: "exhausted", count: 2, wantErrSub: "invalid retry count"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := selectRetryTier(tiers, tt.count)
			if tt.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("selectRetryTier() error = %v, want substring %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectRetryTier() error = %v", err)
			}
			if got.Queue != tt.wantQueue {
				t.Fatalf("selectRetryTier() queue = %q, want %q", got.Queue, tt.wantQueue)
			}
		})
	}
}

func TestRetryDelayMs(t *testing.T) {
	t.Parallel()

	if got := retryDelayMs(10000, 0); got != 10000 {
		t.Fatalf("retryDelayMs() without jitter = %d, want 10000", got)
	}

	for range 1000 {
		got := retryDelayMs(10000, 25)
		if got < 10000 || got > 12500 {
			t.Fatalf("retryDelayMs() = %d, want range [10000, 12500]", got)
		}
	}
}

func TestBuildDLQPublishing(t *testing.T) {
	t.Parallel()

	deadLetteredAt := time.Date(2026, time.August, 2, 12, 30, 0, 0, time.UTC)
	originalTimestamp := time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)
	originalHeaders := amqp.Table{"trace_id": "trace-1"}
	delivery := amqp.Delivery{
		Headers:         originalHeaders,
		ContentType:     "application/json",
		ContentEncoding: "utf-8",
		DeliveryMode:    amqp.Transient,
		Priority:        3,
		CorrelationId:   "correlation-1",
		ReplyTo:         "reply.queue",
		MessageId:       "message-1",
		Timestamp:       originalTimestamp,
		Type:            "chat.message",
		AppId:           "gopherai",
		Expiration:      "10000",
		Body:            []byte(`{"schema_version":1}`),
		Exchange:        "gopherai.chat",
		RoutingKey:      "chat.message.persist.v1",
	}

	message, err := buildDLQPublishing(
		delivery,
		5,
		FailureTransient,
		errors.New("database timeout"),
		deadLetteredAt,
	)
	if err != nil {
		t.Fatalf("buildDLQPublishing() error = %v", err)
	}

	if message.DeliveryMode != amqp.Persistent {
		t.Fatalf("DeliveryMode = %d, want persistent", message.DeliveryMode)
	}
	if message.Expiration != "" {
		t.Fatalf("Expiration = %q, want empty", message.Expiration)
	}
	if message.MessageId != delivery.MessageId || message.CorrelationId != delivery.CorrelationId {
		t.Fatalf("message identity was not preserved: %#v", message)
	}
	if message.Timestamp != originalTimestamp {
		t.Fatalf("Timestamp = %v, want %v", message.Timestamp, originalTimestamp)
	}
	if !reflect.DeepEqual(message.Body, delivery.Body) {
		t.Fatalf("Body = %q, want %q", message.Body, delivery.Body)
	}

	wantHeaders := amqp.Table{
		"trace_id":               "trace-1",
		retryCountHeader:         int32(5),
		"x-final-error":          "database timeout",
		"x-failure-kind":         "transient",
		"x-dead-lettered-at":     deadLetteredAt.Format(time.RFC3339Nano),
		"x-original-exchange":    delivery.Exchange,
		"x-original-routing-key": delivery.RoutingKey,
	}
	if !reflect.DeepEqual(message.Headers, wantHeaders) {
		t.Fatalf("Headers = %#v, want %#v", message.Headers, wantHeaders)
	}
	if _, mutated := originalHeaders[retryCountHeader]; mutated {
		t.Fatal("buildDLQPublishing() mutated delivery headers")
	}
}

func TestBuildDLQPublishingRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		retryCount int
		processErr error
		wantErrSub string
	}{
		{
			name:       "negative retry count",
			retryCount: -1,
			processErr: errors.New("failed"),
			wantErrSub: "negative retry count",
		},
		{
			name:       "nil process error",
			retryCount: 0,
			wantErrSub: "error is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := buildDLQPublishing(
				amqp.Delivery{},
				tt.retryCount,
				FailurePermanent,
				tt.processErr,
				time.Now(),
			)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("buildDLQPublishing() error = %v, want substring %q", err, tt.wantErrSub)
			}
		})
	}
}
