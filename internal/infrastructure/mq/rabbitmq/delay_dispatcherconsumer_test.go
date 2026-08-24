package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	delaydomain "GopherAI/internal/domain/delay"
	"GopherAI/internal/domain/message"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestNewDispatcherConsumerRejectsInvalidInput(t *testing.T) {
	submit := func(context.Context, delaydomain.Task, func() error) error { return nil }

	tests := []struct {
		name      string
		client    *Client
		queue     string
		prefetch  int
		submit    func(context.Context, delaydomain.Task, func() error) error
		wantError string
	}{
		{name: "empty queue", prefetch: 1, submit: submit, wantError: "queue is empty"},
		{name: "zero prefetch", queue: "delay.dispatcher", submit: submit, wantError: "prefetch count must be positive"},
		{name: "nil submit", queue: "delay.dispatcher", prefetch: 1, wantError: "submit callback is nil"},
		{name: "nil client", queue: "delay.dispatcher", prefetch: 1, submit: submit, wantError: "client is unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDispatcherConsumer(tt.client, tt.queue, tt.prefetch, tt.submit)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("NewDispatcherConsumer() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestDispatcherConsumerSubmitDeliveryDefersACK(t *testing.T) {
	task := validFinalPublisherTask(t, message.TopicTarget(), 0)
	delivery := dispatcherDelivery(t, task)
	acknowledger := &dispatcherAcknowledger{}
	delivery.Acknowledger = acknowledger
	delivery.DeliveryTag = 7

	var ack func() error
	consumer := &DispatcherConsumer{
		submit: func(_ context.Context, got delaydomain.Task, gotACK func() error) error {
			if got.ID != task.ID {
				t.Fatalf("submitted task = %q, want %q", got.ID, task.ID)
			}
			ack = gotACK
			return nil
		},
	}

	if err := consumer.submitDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("submitDelivery() error = %v", err)
	}
	if acknowledger.ackCalls != 0 {
		t.Fatal("submitDelivery() ACKed before Dispatcher published the task")
	}
	if ack == nil {
		t.Fatal("submitDelivery() did not pass ACK callback")
	}
	if err := ack(); err != nil {
		t.Fatalf("ACK callback error = %v", err)
	}
	if acknowledger.ackCalls != 1 || acknowledger.tag != 7 || acknowledger.multiple {
		t.Fatalf(
			"ACK = calls:%d tag:%d multiple:%t, want calls:1 tag:7 multiple:false",
			acknowledger.ackCalls,
			acknowledger.tag,
			acknowledger.multiple,
		)
	}
}

func TestDispatcherConsumerSubmitDeliveryRejectsMalformedTask(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		consumer := &DispatcherConsumer{
			submit: func(context.Context, delaydomain.Task, func() error) error {
				t.Fatal("submit callback was called")
				return nil
			},
		}
		err := consumer.submitDelivery(context.Background(), amqp.Delivery{
			MessageId: "schedule-invalid-json",
			Body:      []byte("{"),
		})
		if err == nil || !strings.Contains(err.Error(), "decode dispatcher delivery") {
			t.Fatalf("submitDelivery() error = %v, want decode error", err)
		}
	})

	t.Run("invalid domain task", func(t *testing.T) {
		task := validFinalPublisherTask(t, message.TopicTarget(), 0)
		envelope, err := encodeTask(task)
		if err != nil {
			t.Fatalf("encodeTask() error = %v", err)
		}
		envelope.TaskID = ""
		body, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}

		consumer := &DispatcherConsumer{
			submit: func(context.Context, delaydomain.Task, func() error) error {
				t.Fatal("submit callback was called")
				return nil
			},
		}
		err = consumer.submitDelivery(context.Background(), amqp.Delivery{Body: body})
		if err == nil || !strings.Contains(err.Error(), "validate dispatcher task") {
			t.Fatalf("submitDelivery() error = %v, want validation error", err)
		}
	})
}

func TestDispatcherConsumerSubmitDeliveryReturnsSubmitError(t *testing.T) {
	submitErr := errors.New("dispatcher stopped")
	task := validFinalPublisherTask(t, message.TopicTarget(), 0)
	consumer := &DispatcherConsumer{
		submit: func(context.Context, delaydomain.Task, func() error) error {
			return submitErr
		},
	}

	err := consumer.submitDelivery(context.Background(), dispatcherDelivery(t, task))
	if !errors.Is(err, submitErr) {
		t.Fatalf("submitDelivery() error = %v, want submit error", err)
	}
}

func TestDispatcherConsumerCloseWithoutChannel(t *testing.T) {
	var consumer *DispatcherConsumer
	if err := consumer.Close(); err != nil {
		t.Fatalf("nil Close() error = %v", err)
	}
	consumer = &DispatcherConsumer{}
	if err := consumer.Close(); err != nil {
		t.Fatalf("empty Close() error = %v", err)
	}
}

func dispatcherDelivery(t *testing.T, task delaydomain.Task) amqp.Delivery {
	t.Helper()
	envelope, err := encodeTask(task)
	if err != nil {
		t.Fatalf("encodeTask() error = %v", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return amqp.Delivery{
		MessageId: task.ID,
		Body:      body,
	}
}

type dispatcherAcknowledger struct {
	ackCalls int
	tag      uint64
	multiple bool
}

func (a *dispatcherAcknowledger) Ack(tag uint64, multiple bool) error {
	a.ackCalls++
	a.tag = tag
	a.multiple = multiple
	return nil
}

func (*dispatcherAcknowledger) Nack(uint64, bool, bool) error { return nil }

func (*dispatcherAcknowledger) Reject(uint64, bool) error { return nil }
