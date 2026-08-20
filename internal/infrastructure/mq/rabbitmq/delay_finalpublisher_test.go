package rabbitmq

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestFinalPublisherPublishRejectsNilPublisher(t *testing.T) {
	var publisher *FinalPublisher

	if err := publisher.Publish(context.Background(), delay.Task{}); err == nil {
		t.Fatal("Publish() error = nil, want nil publisher error")
	}
}

func TestFinalPublisherPublishRoutesTopicAndPreservesBusinessMessage(t *testing.T) {
	task := validFinalPublisherTask(t, messageDomain.TopicTarget(), 0)
	channel := &finalPublishChannelStub{confirmation: finalConfirmationStub{acked: true}}
	publisher := finalPublisherForTest(channel)

	if err := publisher.Publish(context.Background(), task); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if channel.exchange != "gopherai.events" {
		t.Fatalf("Publish() exchange = %q, want %q", channel.exchange, "gopherai.events")
	}
	if channel.routingKey != task.Message.Topic {
		t.Fatalf("Publish() routing key = %q, want %q", channel.routingKey, task.Message.Topic)
	}
	if !channel.mandatory || channel.immediate {
		t.Fatalf("Publish() flags mandatory=%t immediate=%t, want true/false", channel.mandatory, channel.immediate)
	}
	if channel.message.MessageId != task.Message.ID {
		t.Fatalf("Publish() message ID = %q, want business ID %q", channel.message.MessageId, task.Message.ID)
	}
	if !bytes.Equal(channel.message.Body, task.Message.Body) {
		t.Fatalf("Publish() body = %q, want %q", channel.message.Body, task.Message.Body)
	}
	if !channel.message.Timestamp.Equal(task.Message.Timestamp) {
		t.Fatalf("Publish() timestamp = %s, want %s", channel.message.Timestamp, task.Message.Timestamp)
	}
	wantHeaders := amqp.Table{
		"trace_id":        "trace-1",
		"x-goai-topic":    task.Message.Topic,
		"x-retry-attempt": int64(0),
	}
	if !reflect.DeepEqual(channel.message.Headers, wantHeaders) {
		t.Fatalf("Publish() headers = %#v, want %#v", channel.message.Headers, wantHeaders)
	}
	if _, exists := task.Message.Headers["x-goai-topic"]; exists {
		t.Fatal("Publish() mutated task message headers")
	}
}

func TestFinalPublisherPublishRoutesConsumerGroupToRedriveExchange(t *testing.T) {
	target, err := messageDomain.ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatalf("ConsumerGroupTarget() error = %v", err)
	}
	task := validFinalPublisherTask(t, target, 2)
	channel := &finalPublishChannelStub{confirmation: finalConfirmationStub{acked: true}}
	publisher := finalPublisherForTest(channel)

	if err = publisher.Publish(context.Background(), task); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if channel.exchange != "gopherai.events.redrive" {
		t.Fatalf("Publish() exchange = %q, want redrive exchange", channel.exchange)
	}
	if channel.routingKey != "analytics" {
		t.Fatalf("Publish() routing key = %q, want %q", channel.routingKey, "analytics")
	}
	if got := channel.message.Headers["x-retry-attempt"]; got != int64(2) {
		t.Fatalf("Publish() retry header = %#v, want int64(2)", got)
	}
}

func TestFinalPublisherPublishRejectsUnknownTopicBeforeAMQP(t *testing.T) {
	task := validFinalPublisherTask(t, messageDomain.TopicTarget(), 0)
	task.Message.Topic = "unknown.topic.v1"
	channel := &finalPublishChannelStub{confirmation: finalConfirmationStub{acked: true}}
	publisher := finalPublisherForTest(channel)

	err := publisher.Publish(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "is not allowed") {
		t.Fatalf("Publish() error = %v, want topic-not-allowed error", err)
	}
	if channel.publishCalls != 0 {
		t.Fatalf("Publish() AMQP calls = %d, want 0", channel.publishCalls)
	}
}

func TestFinalPublisherPublishFailureSemantics(t *testing.T) {
	task := validFinalPublisherTask(t, messageDomain.TopicTarget(), 0)
	closingChannel := &finalPublishChannelStub{}
	closingChannel.confirmation = closingFinalConfirmationStub{channel: closingChannel}

	tests := []struct {
		name            string
		channel         *finalPublishChannelStub
		returns         []amqp.Return
		wantError       string
		wantInvalidated bool
	}{
		{
			name:            "publish error has unknown outcome",
			channel:         &finalPublishChannelStub{publishErr: errors.New("connection lost")},
			wantError:       "unknown outcome",
			wantInvalidated: true,
		},
		{
			name:            "missing confirmation has unknown outcome",
			channel:         &finalPublishChannelStub{},
			wantError:       "confirm is unavailable",
			wantInvalidated: true,
		},
		{
			name: "confirm wait error has unknown outcome",
			channel: &finalPublishChannelStub{confirmation: finalConfirmationStub{
				err: errors.New("confirm timeout"),
			}},
			wantError:       "unknown outcome",
			wantInvalidated: true,
		},
		{
			name:            "broker nack is explicit rejection",
			channel:         &finalPublishChannelStub{confirmation: finalConfirmationStub{acked: false}},
			wantError:       "negatively acknowledged",
			wantInvalidated: true,
		},
		{
			name:            "channel close is not broker nack",
			channel:         closingChannel,
			wantError:       "outcome unknown",
			wantInvalidated: true,
		},
		{
			name:    "mandatory return is explicit routing rejection",
			channel: &finalPublishChannelStub{confirmation: finalConfirmationStub{acked: true}},
			returns: []amqp.Return{{
				ReplyCode:     312,
				ReplyText:     "NO_ROUTE",
				Exchange:      "gopherai.events",
				RoutingKey:    task.Message.Topic,
				MessageId:     task.Message.ID,
				CorrelationId: task.ID,
			}},
			wantError:       "was returned",
			wantInvalidated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := finalPublisherForTest(tt.channel)
			returns := make(chan amqp.Return, len(tt.returns)+1)
			for _, returned := range tt.returns {
				returns <- returned
			}
			publisher.returns = returns

			err := publisher.Publish(context.Background(), task)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Publish() error = %v, want error containing %q", err, tt.wantError)
			}
			if got := publisher.channel == nil; got != tt.wantInvalidated {
				t.Fatalf("Publish() invalidated channel = %t, want %t", got, tt.wantInvalidated)
			}
		})
	}
}

func TestFinalPublisherCloseIsIdempotent(t *testing.T) {
	channel := &finalPublishChannelStub{}
	publisher := finalPublisherForTest(channel)

	if err := publisher.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if channel.closeCalls != 1 {
		t.Fatalf("Close() channel calls = %d, want 1", channel.closeCalls)
	}
}

func TestFinalPublisherCloseDropsChannelAfterCloseError(t *testing.T) {
	channel := &finalPublishChannelStub{closeErr: errors.New("socket closed")}
	publisher := finalPublisherForTest(channel)

	err := publisher.Close()
	if err == nil || !strings.Contains(err.Error(), "socket closed") {
		t.Fatalf("Close() error = %v, want socket close error", err)
	}
	if publisher.channel != nil {
		t.Fatal("Close() retained an unusable channel")
	}
	if err = publisher.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

type finalPublishChannelStub struct {
	confirmation finalConfirmation
	publishErr   error
	publishCalls int
	closed       bool
	closeErr     error
	closeCalls   int

	exchange   string
	routingKey string
	mandatory  bool
	immediate  bool
	message    amqp.Publishing
}

func (c *finalPublishChannelStub) PublishWithDeferredConfirmWithContext(
	_ context.Context,
	exchange string,
	key string,
	mandatory bool,
	immediate bool,
	message amqp.Publishing,
) (finalConfirmation, error) {
	c.publishCalls++
	c.exchange = exchange
	c.routingKey = key
	c.mandatory = mandatory
	c.immediate = immediate
	c.message = message
	return c.confirmation, c.publishErr
}

func (c *finalPublishChannelStub) IsClosed() bool { return c.closed }

func (c *finalPublishChannelStub) Close() error {
	c.closeCalls++
	c.closed = true
	return c.closeErr
}

type finalConfirmationStub struct {
	acked bool
	err   error
}

type closingFinalConfirmationStub struct {
	channel *finalPublishChannelStub
}

func (c closingFinalConfirmationStub) WaitContext(context.Context) (bool, error) {
	c.channel.closed = true
	return false, nil
}

func (c finalConfirmationStub) WaitContext(context.Context) (bool, error) {
	return c.acked, c.err
}

func finalPublisherForTest(channel finalPublishChannel) *FinalPublisher {
	return &FinalPublisher{
		channel: channel,
		returns: make(chan amqp.Return, 1),
		closes:  make(chan *amqp.Error, 1),
		config: FinalPublisherConfig{
			TopicExchange:   "gopherai.events",
			RedriveExchange: "gopherai.events.redrive",
			ConfirmTimeout:  time.Second,
		},
		topics: map[string]struct{}{
			"chat.message.created.v1": {},
		},
		consumerGroups: map[string]struct{}{
			"analytics": {},
		},
	}
}

func validFinalPublisherTask(
	t *testing.T,
	target messageDomain.Target,
	retryAttempt uint32,
) delay.Task {
	t.Helper()

	message, err := messageDomain.New(
		"message-1",
		"chat.message.created.v1",
		map[string]string{"trace_id": "trace-1"},
		[]byte(`{"text":"hello"}`),
		time.Date(2026, 8, 20, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60)),
	)
	if err != nil {
		t.Fatalf("message.New() error = %v", err)
	}

	task, err := delay.NewTask(
		"schedule-1",
		"account-1",
		message,
		target,
		retryAttempt,
		time.Date(2026, 8, 20, 9, 31, 0, 0, time.FixedZone("CST", 8*60*60)).UnixMilli(),
	)
	if err != nil {
		t.Fatalf("delay.NewTask() error = %v", err)
	}
	return task
}
