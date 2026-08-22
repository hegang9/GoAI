package rabbitmq

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"GopherAI/internal/domain/delay"
	"GopherAI/internal/domain/message"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestFinalPublisherRoute(t *testing.T) {
	publisher := finalPublisherForTest()

	topicTask := validFinalPublisherTask(t, message.TopicTarget(), 0)
	exchange, routingKey, err := publisher.route(topicTask)
	if err != nil {
		t.Fatalf("route topic error = %v", err)
	}
	if exchange != "gopherai.events" || routingKey != topicTask.Message.Topic {
		t.Fatalf("route topic = %q/%q", exchange, routingKey)
	}

	groupTarget, err := message.ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatalf("ConsumerGroupTarget() error = %v", err)
	}
	groupTask := validFinalPublisherTask(t, groupTarget, 2)
	exchange, routingKey, err = publisher.route(groupTask)
	if err != nil {
		t.Fatalf("route consumer group error = %v", err)
	}
	if exchange != "gopherai.events.redrive" || routingKey != "analytics" {
		t.Fatalf("route consumer group = %q/%q", exchange, routingKey)
	}
}

func TestBuildFinalPublishingPreservesBusinessMessage(t *testing.T) {
	target, err := message.ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatalf("ConsumerGroupTarget() error = %v", err)
	}
	task := validFinalPublisherTask(t, target, 2)
	publishing := buildFinalPublishing(task)

	if publishing.MessageId != task.Message.ID {
		t.Fatalf("message ID = %q, want %q", publishing.MessageId, task.Message.ID)
	}
	if publishing.CorrelationId != task.ID {
		t.Fatalf("correlation ID = %q, want %q", publishing.CorrelationId, task.ID)
	}
	if publishing.DeliveryMode != amqp.Persistent {
		t.Fatalf("delivery mode = %d, want persistent", publishing.DeliveryMode)
	}
	if !bytes.Equal(publishing.Body, task.Message.Body) {
		t.Fatalf("body = %q, want %q", publishing.Body, task.Message.Body)
	}
	if !publishing.Timestamp.Equal(task.Message.Timestamp) {
		t.Fatalf("timestamp = %s, want %s", publishing.Timestamp, task.Message.Timestamp)
	}
	wantHeaders := amqp.Table{
		"trace_id":        "trace-1",
		"x-goai-topic":    task.Message.Topic,
		"x-retry-attempt": int64(2),
	}
	if !reflect.DeepEqual(publishing.Headers, wantHeaders) {
		t.Fatalf("headers = %#v, want %#v", publishing.Headers, wantHeaders)
	}
	if _, exists := task.Message.Headers["x-goai-topic"]; exists {
		t.Fatal("buildFinalPublishing() mutated task headers")
	}
}

func TestFinalPublisherPublishRejectsBeforeAMQP(t *testing.T) {
	t.Run("nil publisher", func(t *testing.T) {
		var publisher *FinalPublisher
		if err := publisher.Publish(context.Background(), delay.Task{}); err == nil {
			t.Fatal("Publish() error = nil")
		}
	})

	t.Run("unknown topic", func(t *testing.T) {
		publisher := finalPublisherForTest()
		task := validFinalPublisherTask(t, message.TopicTarget(), 0)
		task.Message.Topic = "unknown.topic.v1"

		err := publisher.Publish(context.Background(), task)
		if err == nil || !strings.Contains(err.Error(), "is not allowed") {
			t.Fatalf("Publish() error = %v, want topic-not-allowed error", err)
		}
	})

	t.Run("channel unavailable", func(t *testing.T) {
		publisher := finalPublisherForTest()
		task := validFinalPublisherTask(t, message.TopicTarget(), 0)

		err := publisher.Publish(context.Background(), task)
		if err == nil || !strings.Contains(err.Error(), "channel is unavailable") {
			t.Fatalf("Publish() error = %v, want unavailable-channel error", err)
		}
	})
}

func TestFinalPublisherCloseWithoutChannel(t *testing.T) {
	publisher := finalPublisherForTest()
	if err := publisher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func finalPublisherForTest() *FinalPublisher {
	return &FinalPublisher{
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
	target message.Target,
	retryAttempt uint32,
) delay.Task {
	t.Helper()

	msg, err := message.New(
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
		msg,
		target,
		retryAttempt,
		time.Date(2026, 8, 20, 9, 31, 0, 0, time.FixedZone("CST", 8*60*60)).UnixMilli(),
	)
	if err != nil {
		t.Fatalf("delay.NewTask() error = %v", err)
	}
	return task
}
