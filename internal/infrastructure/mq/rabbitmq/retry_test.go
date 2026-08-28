package rabbitmq

import (
	"errors"
	"fmt"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestFailureKindString(t *testing.T) {
	tests := []struct {
		kind FailureKind
		want string
	}{
		{kind: FailureTransient, want: "transient"},
		{kind: FailurePermanent, want: "permanent"},
		{kind: FailureAbort, want: "abort"},
		{kind: FailureKind(99), want: "unknown(99)"},
	}
	for _, test := range tests {
		if got := test.kind.String(); got != test.want {
			t.Fatalf("FailureKind.String() = %q, want %q", got, test.want)
		}
	}
}

func TestClassifyError(t *testing.T) {
	transientErr := errors.New("database timeout")
	permanentErr := permanentError(errors.New("invalid payload"))
	abortedErr := abortError(errors.New("database schema mismatch"))

	tests := []struct {
		err  error
		want FailureKind
	}{
		{want: FailureTransient},
		{err: transientErr, want: FailureTransient},
		{err: permanentErr, want: FailurePermanent},
		{err: fmt.Errorf("consume failed: %w", permanentErr), want: FailurePermanent},
		{err: abortedErr, want: FailureAbort},
	}
	for _, test := range tests {
		if got := classifyError(test.err); got != test.want {
			t.Fatalf("classifyError() = %s, want %s", got, test.want)
		}
	}
}

func TestCopyHeadersDoesNotMutateSource(t *testing.T) {
	source := amqp.Table{"trace_id": "trace-1"}
	copied := copyHeaders(source)
	copied["new"] = "value"

	if _, exists := source["new"]; exists {
		t.Fatal("copyHeaders() mutation leaked into source")
	}
	if got := copied["trace_id"]; got != "trace-1" {
		t.Fatalf("copyHeaders() trace_id = %v, want trace-1", got)
	}
}
