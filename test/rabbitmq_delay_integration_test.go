package test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"GopherAI/config"
	delayapp "GopherAI/internal/application/delay"
	chatdomain "GopherAI/internal/domain/chat"
	delaydomain "GopherAI/internal/domain/delay"
	messagedomain "GopherAI/internal/domain/message"
	"GopherAI/internal/infrastructure/mq/rabbitmq"
	"GopherAI/pkg/id"

	"github.com/BurntSushi/toml"
	amqp "github.com/rabbitmq/amqp091-go"
)

const rabbitMQIntegrationEnv = "GOAI_RABBITMQ_INTEGRATION"

func TestRabbitMQTopicFanoutAndConsumerGroupCompetition(t *testing.T) {
	connectionConfig, uri := rabbitMQIntegrationConfig(t)
	topology := newIntegrationTopology(1, 2)

	client, err := rabbitmq.Connect(connectionConfig)
	if err != nil {
		t.Fatalf("connect RabbitMQ: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	registerTopologyCleanup(t, uri, topology)
	levelPublisher, err := rabbitmq.NewLevelPublisher(client, topology)
	if err != nil {
		t.Fatalf("declare integration topology: %v", err)
	}
	t.Cleanup(func() { _ = levelPublisher.Close() })

	type handled struct {
		group    string
		instance int
		message  string
	}
	handledMessages := make(chan handled, 128)
	neverRetry := func(
		context.Context,
		string,
		string,
		messagedomain.Message,
		uint32,
	) (bool, error) {
		return false, errors.New("integration handler unexpectedly requested retry")
	}

	consumers := make([]*rabbitmq.GroupConsumer, 0, 3)
	for groupIndex, group := range topology.ConsumerGroups {
		instances := 1
		if groupIndex == 1 {
			instances = 2
		}
		for instance := 0; instance < instances; instance++ {
			groupName := group.Name
			instanceID := instance
			consumer, createErr := rabbitmq.NewGroupConsumer(
				client,
				group.Name,
				group.Queue,
				1,
				topology.DeadLetterExchange,
				group.DeadLetterRoutingKey,
				topology.ConfirmTimeout,
				func(_ context.Context, message chatdomain.Message) error {
					if groupIndex == 1 {
						time.Sleep(5 * time.Millisecond)
					}
					handledMessages <- handled{
						group: groupName, instance: instanceID, message: message.ID,
					}
					return nil
				},
				neverRetry,
			)
			if createErr != nil {
				t.Fatalf("new group consumer %q: %v", group.Name, createErr)
			}
			consumers = append(consumers, consumer)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, len(consumers))
	for index, consumer := range consumers {
		runIntegrationComponent(ctx, fmt.Sprintf("group-consumer-%d", index), consumer.Run, errCh)
	}
	t.Cleanup(func() {
		cancel()
		for _, consumer := range consumers {
			_ = consumer.Close()
		}
	})
	waitForQueueConsumers(t, uri, topology.ConsumerGroups[0].Queue, 1)
	waitForQueueConsumers(t, uri, topology.ConsumerGroups[1].Queue, 2)

	publisher, err := rabbitmq.NewPublisher(
		client,
		topology.TopicExchange,
		chatdomain.MessageCreatedTopic,
		topology.ConfirmTimeout,
	)
	if err != nil {
		t.Fatalf("new topic publisher: %v", err)
	}

	const messages = 12
	for index := 0; index < messages; index++ {
		messageID := fmt.Sprintf("integration-message-%02d", index)
		if err := publisher.Save(chatdomain.Message{
			ID: messageID, SessionID: "session-1", AccountNo: "account-1", Content: "hello",
		}); err != nil {
			t.Fatalf("publish %q: %v", messageID, err)
		}
	}

	counts := make(map[string]map[string]int)
	instanceCounts := [2]int{}
	for received := 0; received < messages*2; received++ {
		select {
		case event := <-handledMessages:
			if counts[event.group] == nil {
				counts[event.group] = make(map[string]int)
			}
			counts[event.group][event.message]++
			if event.group == topology.ConsumerGroups[1].Name {
				instanceCounts[event.instance]++
			}
		case componentErr := <-errCh:
			t.Fatal(componentErr)
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out after receiving %d/%d deliveries", received, messages*2)
		}
	}

	for _, group := range topology.ConsumerGroups {
		if len(counts[group.Name]) != messages {
			t.Fatalf("group %q handled %d messages, want %d", group.Name, len(counts[group.Name]), messages)
		}
		for messageID, count := range counts[group.Name] {
			if count != 1 {
				t.Fatalf("group %q message %q handled %d times, want 1", group.Name, messageID, count)
			}
		}
	}
	if instanceCounts[0] == 0 || instanceCounts[1] == 0 {
		t.Fatalf("competing consumers handled %v messages, want both instances active", instanceCounts)
	}
}

func TestRabbitMQRetryReturnsOnlyToOriginalConsumerGroup(t *testing.T) {
	connectionConfig, uri := rabbitMQIntegrationConfig(t)
	topology := newIntegrationTopology(2, 2)

	client, err := rabbitmq.Connect(connectionConfig)
	if err != nil {
		t.Fatalf("connect RabbitMQ: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	registerTopologyCleanup(t, uri, topology)

	levelPublisher, err := rabbitmq.NewLevelPublisher(client, topology)
	if err != nil {
		t.Fatalf("new level publisher: %v", err)
	}
	t.Cleanup(func() { _ = levelPublisher.Close() })
	finalPublisher, err := rabbitmq.NewFinalPublisher(client, rabbitmq.FinalPublisherConfig{
		TopicExchange:   topology.TopicExchange,
		RedriveExchange: topology.RedriveExchange,
		ConfirmTimeout:  topology.ConfirmTimeout,
		Topics:          []string{chatdomain.MessageCreatedTopic},
		ConsumerGroups: []string{
			topology.ConsumerGroups[0].Name,
			topology.ConsumerGroups[1].Name,
		},
	})
	if err != nil {
		t.Fatalf("new final publisher: %v", err)
	}
	t.Cleanup(func() { _ = finalPublisher.Close() })
	dispatcher, err := delayapp.NewDispatcher(finalPublisher, 16)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	dispatcherConsumer, err := rabbitmq.NewDispatcherConsumer(
		client,
		topology.DispatcherQueue,
		16,
		dispatcher.Submit,
	)
	if err != nil {
		t.Fatalf("new dispatcher consumer: %v", err)
	}
	t.Cleanup(func() { _ = dispatcherConsumer.Close() })

	group := topology.ConsumerGroups[0]
	delayService, err := delayapp.NewDelayService(
		unexpectedDelayRepository{},
		levelPublisher,
		delayapp.DelayServiceConfig{
			ShortThreshold: 2 * time.Second,
			MaxDelay:       time.Hour,
			RetryPolicies: map[string]delayapp.RetryPolicy{
				group.Name: {Delays: []time.Duration{1500 * time.Millisecond}},
			},
		},
	)
	if err != nil {
		t.Fatalf("new delay service: %v", err)
	}

	var attempts atomic.Int32
	handledAt := make(chan time.Time, 2)
	retryAttempts := make(chan uint32, 1)
	groupConsumer, err := rabbitmq.NewGroupConsumer(
		client,
		group.Name,
		group.Queue,
		1,
		topology.DeadLetterExchange,
		group.DeadLetterRoutingKey,
		topology.ConfirmTimeout,
		func(context.Context, chatdomain.Message) error {
			handledAt <- time.Now()
			if attempts.Add(1) == 1 {
				return errors.New("temporary persistence failure")
			}
			return nil
		},
		func(
			ctx context.Context,
			accountNo string,
			consumerGroup string,
			message messagedomain.Message,
			currentAttempt uint32,
		) (bool, error) {
			retryAttempts <- currentAttempt
			_, scheduleErr := delayService.ScheduleRetry(ctx, delayapp.RetryCommand{
				AccountNo: accountNo, ConsumerGroup: consumerGroup,
				Message: message, CurrentAttempt: currentAttempt,
			})
			if errors.Is(scheduleErr, delayapp.ErrRetryExhausted) {
				return true, nil
			}
			return false, scheduleErr
		},
	)
	if err != nil {
		t.Fatalf("new group consumer: %v", err)
	}
	t.Cleanup(func() { _ = groupConsumer.Close() })
	otherGroup := topology.ConsumerGroups[1]
	var otherAttempts atomic.Int32
	otherHandled := make(chan struct{}, 1)
	otherConsumer, err := rabbitmq.NewGroupConsumer(
		client,
		otherGroup.Name,
		otherGroup.Queue,
		1,
		topology.DeadLetterExchange,
		otherGroup.DeadLetterRoutingKey,
		topology.ConfirmTimeout,
		func(context.Context, chatdomain.Message) error {
			otherAttempts.Add(1)
			otherHandled <- struct{}{}
			return nil
		},
		func(
			context.Context,
			string,
			string,
			messagedomain.Message,
			uint32,
		) (bool, error) {
			return false, errors.New("successful group unexpectedly requested retry")
		},
	)
	if err != nil {
		t.Fatalf("new other group consumer: %v", err)
	}
	t.Cleanup(func() { _ = otherConsumer.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 4)
	runIntegrationComponent(ctx, "dispatcher", dispatcher.Run, errCh)
	runIntegrationComponent(ctx, "dispatcher-consumer", dispatcherConsumer.Run, errCh)
	runIntegrationComponent(ctx, "group-consumer", groupConsumer.Run, errCh)
	runIntegrationComponent(ctx, "other-group-consumer", otherConsumer.Run, errCh)
	t.Cleanup(cancel)
	waitForQueueConsumers(t, uri, topology.DispatcherQueue, 1)
	waitForQueueConsumers(t, uri, group.Queue, 1)
	waitForQueueConsumers(t, uri, otherGroup.Queue, 1)

	publisher, err := rabbitmq.NewPublisher(
		client,
		topology.TopicExchange,
		chatdomain.MessageCreatedTopic,
		topology.ConfirmTimeout,
	)
	if err != nil {
		t.Fatalf("new topic publisher: %v", err)
	}
	if err := publisher.Save(chatdomain.Message{
		ID: "retry-message-1", SessionID: "session-1", AccountNo: "account-1", Content: "retry me",
	}); err != nil {
		t.Fatalf("publish retry integration message: %v", err)
	}
	select {
	case <-otherHandled:
	case componentErr := <-errCh:
		t.Fatal(componentErr)
	case <-time.After(5 * time.Second):
		t.Fatal("other consumer group did not receive initial topic message")
	}

	first := waitHandledAt(t, handledAt, errCh)
	second := waitHandledAt(t, handledAt, errCh)
	if got := <-retryAttempts; got != 0 {
		t.Fatalf("current retry attempt = %d, want 0", got)
	}
	if elapsed := second.Sub(first); elapsed < 1200*time.Millisecond {
		t.Fatalf("retry returned after %s, want Level TTL plus wheel wait", elapsed)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("handler attempts = %d, want 2", got)
	}
	time.Sleep(200 * time.Millisecond)
	if got := otherAttempts.Load(); got != 1 {
		t.Fatalf("other group attempts = %d, want only the initial topic delivery", got)
	}
}

func TestRabbitMQConsumerGroupsUseIndependentDeadLetterQueues(t *testing.T) {
	connectionConfig, uri := rabbitMQIntegrationConfig(t)
	topology := newIntegrationTopology(1, 2)

	client, err := rabbitmq.Connect(connectionConfig)
	if err != nil {
		t.Fatalf("connect RabbitMQ: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	registerTopologyCleanup(t, uri, topology)
	levelPublisher, err := rabbitmq.NewLevelPublisher(client, topology)
	if err != nil {
		t.Fatalf("declare integration topology: %v", err)
	}
	t.Cleanup(func() { _ = levelPublisher.Close() })

	consumers := make([]*rabbitmq.GroupConsumer, 0, len(topology.ConsumerGroups))
	for _, group := range topology.ConsumerGroups {
		consumer, createErr := rabbitmq.NewGroupConsumer(
			client,
			group.Name,
			group.Queue,
			1,
			topology.DeadLetterExchange,
			group.DeadLetterRoutingKey,
			topology.ConfirmTimeout,
			func(context.Context, chatdomain.Message) error {
				return errors.New("retry policy exhausted")
			},
			func(
				context.Context,
				string,
				string,
				messagedomain.Message,
				uint32,
			) (bool, error) {
				return true, nil
			},
		)
		if createErr != nil {
			t.Fatalf("new group consumer %q: %v", group.Name, createErr)
		}
		consumers = append(consumers, consumer)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, len(consumers))
	for index, consumer := range consumers {
		runIntegrationComponent(ctx, fmt.Sprintf("dlq-consumer-%d", index), consumer.Run, errCh)
	}
	t.Cleanup(func() {
		cancel()
		for _, consumer := range consumers {
			_ = consumer.Close()
		}
	})
	for _, group := range topology.ConsumerGroups {
		waitForQueueConsumers(t, uri, group.Queue, 1)
	}

	publisher, err := rabbitmq.NewPublisher(
		client,
		topology.TopicExchange,
		chatdomain.MessageCreatedTopic,
		topology.ConfirmTimeout,
	)
	if err != nil {
		t.Fatalf("new topic publisher: %v", err)
	}
	if err := publisher.Save(chatdomain.Message{
		ID: "dead-letter-message-1", SessionID: "session-1",
		AccountNo: "account-1", Content: "dead letter me",
	}); err != nil {
		t.Fatalf("publish dead-letter integration message: %v", err)
	}

	for _, group := range topology.ConsumerGroups {
		waitForQueueMessages(t, uri, group.DeadLetterQueue, 1, errCh)
		delivery := getOneMessage(t, uri, group.DeadLetterQueue)
		if delivery.MessageId != "dead-letter-message-1" {
			t.Fatalf("group %q DLQ message ID = %q", group.Name, delivery.MessageId)
		}
		if got := delivery.Headers["x-consumer-group"]; got != group.Name {
			t.Fatalf("group %q DLQ consumer header = %#v", group.Name, got)
		}
	}
}

func rabbitMQIntegrationConfig(t *testing.T) (rabbitmq.Config, string) {
	t.Helper()
	if os.Getenv(rabbitMQIntegrationEnv) != "1" {
		t.Skipf("set %s=1 to run against config/config.toml", rabbitMQIntegrationEnv)
	}

	var projectConfig config.Config
	configPath := filepath.Join("..", "config", "config.toml")
	if _, err := toml.DecodeFile(configPath, &projectConfig); err != nil {
		t.Fatalf("decode %s: %v", configPath, err)
	}
	connectionConfig := rabbitmq.Config{
		Host: projectConfig.RabbitmqHost, Port: projectConfig.RabbitmqPort,
		Username: projectConfig.RabbitmqUsername, Password: projectConfig.RabbitmqPassword,
		Vhost: projectConfig.RabbitmqVhost,
	}
	if connectionConfig.Vhost == "" {
		connectionConfig.Vhost = "/"
	}
	uri := amqp.URI{
		Scheme: "amqp", Host: connectionConfig.Host, Port: connectionConfig.Port,
		Username: connectionConfig.Username, Password: connectionConfig.Password,
		Vhost: connectionConfig.Vhost,
	}.String()
	return connectionConfig, uri
}

func newIntegrationTopology(maxLevel int, groups int) rabbitmq.DelayConfig {
	suffix := strings.ReplaceAll(id.GenerateUUID(), "-", "")
	prefix := "goai.it." + suffix
	consumerGroups := make([]rabbitmq.DelayConsumerGroupConfig, 0, groups)
	for index := 0; index < groups; index++ {
		name := fmt.Sprintf("group-%d-%s", index, suffix)
		consumerGroups = append(consumerGroups, rabbitmq.DelayConsumerGroupConfig{
			Name: name, Queue: prefix + fmt.Sprintf(".group.%d", index),
			Topics:               []string{chatdomain.MessageCreatedTopic},
			DeadLetterQueue:      prefix + fmt.Sprintf(".group.%d.dlq", index),
			DeadLetterRoutingKey: name,
		})
	}
	return rabbitmq.DelayConfig{
		LevelExchange: prefix + ".level", LevelQueuePrefix: prefix + ".level.queue",
		LevelRoutingPrefix: prefix + ".level.route",
		DispatcherExchange: prefix + ".dispatcher", DispatcherQueue: prefix + ".dispatcher.queue",
		DispatcherRoutingKey: prefix + ".dispatcher.route",
		TopicExchange:        prefix + ".events", RedriveExchange: prefix + ".redrive",
		DeadLetterExchange: prefix + ".dlx", ConsumerGroups: consumerGroups,
		MaxLevel: maxLevel, ConfirmTimeout: 3 * time.Second,
	}
}

func registerTopologyCleanup(t *testing.T, uri string, topology rabbitmq.DelayConfig) {
	t.Helper()
	t.Cleanup(func() {
		connection, err := amqp.Dial(uri)
		if err != nil {
			t.Errorf("cleanup RabbitMQ dial: %v", err)
			return
		}
		defer connection.Close()
		channel, err := connection.Channel()
		if err != nil {
			t.Errorf("cleanup RabbitMQ channel: %v", err)
			return
		}
		defer channel.Close()

		queues := []string{topology.DispatcherQueue}
		for level := 1; level <= topology.MaxLevel; level++ {
			queues = append(queues, fmt.Sprintf("%s.%d", topology.LevelQueuePrefix, level))
		}
		for _, group := range topology.ConsumerGroups {
			queues = append(queues, group.Queue, group.DeadLetterQueue)
		}
		for _, queue := range queues {
			if _, err := channel.QueueDelete(queue, false, false, false); err != nil {
				t.Errorf("cleanup queue %q: %v", queue, err)
				return
			}
		}
		for _, exchange := range []string{
			topology.LevelExchange, topology.DispatcherExchange, topology.TopicExchange,
			topology.RedriveExchange, topology.DeadLetterExchange,
		} {
			if err := channel.ExchangeDelete(exchange, false, false); err != nil {
				t.Errorf("cleanup exchange %q: %v", exchange, err)
				return
			}
		}
	})
}

func waitForQueueConsumers(t *testing.T, uri string, queue string, expected int) {
	t.Helper()
	connection, err := amqp.Dial(uri)
	if err != nil {
		t.Fatalf("inspect RabbitMQ dial: %v", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("inspect RabbitMQ channel: %v", err)
	}
	defer channel.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, inspectErr := channel.QueueInspect(queue)
		if inspectErr != nil {
			t.Fatalf("inspect queue %q: %v", queue, inspectErr)
		}
		if state.Consumers >= expected {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("queue %q did not reach %d consumers", queue, expected)
}

func waitForQueueMessages(
	t *testing.T,
	uri string,
	queue string,
	expected int,
	componentErrors <-chan error,
) {
	t.Helper()
	connection, err := amqp.Dial(uri)
	if err != nil {
		t.Fatalf("inspect RabbitMQ dial: %v", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("inspect RabbitMQ channel: %v", err)
	}
	defer channel.Close()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case componentErr := <-componentErrors:
			t.Fatal(componentErr)
		default:
		}
		state, inspectErr := channel.QueueInspect(queue)
		if inspectErr != nil {
			t.Fatalf("inspect queue %q: %v", queue, inspectErr)
		}
		if state.Messages >= expected {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("queue %q did not reach %d ready messages", queue, expected)
}

func getOneMessage(t *testing.T, uri string, queue string) amqp.Delivery {
	t.Helper()
	connection, err := amqp.Dial(uri)
	if err != nil {
		t.Fatalf("get RabbitMQ message dial: %v", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("get RabbitMQ message channel: %v", err)
	}
	defer channel.Close()

	delivery, ok, err := channel.Get(queue, true)
	if err != nil {
		t.Fatalf("get queue %q message: %v", queue, err)
	}
	if !ok {
		t.Fatalf("queue %q has no message", queue)
	}
	return delivery
}

func runIntegrationComponent(
	ctx context.Context,
	name string,
	run func(context.Context) error,
	errorsChannel chan<- error,
) {
	go func() {
		if err := run(ctx); err != nil && ctx.Err() == nil {
			errorsChannel <- fmt.Errorf("%s: %w", name, err)
		}
	}()
}

func waitHandledAt(t *testing.T, handled <-chan time.Time, componentErrors <-chan error) time.Time {
	t.Helper()
	select {
	case timestamp := <-handled:
		return timestamp
	case err := <-componentErrors:
		t.Fatal(err)
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for group handler")
	}
	return time.Time{}
}

type unexpectedDelayRepository struct{}

func (unexpectedDelayRepository) Create(
	context.Context,
	delaydomain.Task,
) (delaydomain.Task, bool, error) {
	return delaydomain.Task{}, false, errors.New("integration retry unexpectedly used long-delay repository")
}

func (unexpectedDelayRepository) Get(context.Context, string, string) (delaydomain.Task, error) {
	return delaydomain.Task{}, errors.New("unexpected Get")
}

func (unexpectedDelayRepository) ClaimDue(
	context.Context,
	time.Time,
	time.Time,
	time.Time,
	int,
	string,
) ([]delaydomain.Task, error) {
	return nil, errors.New("unexpected ClaimDue")
}

func (unexpectedDelayRepository) MarkLevelQueued(context.Context, string, string, int64) error {
	return errors.New("unexpected MarkLevelQueued")
}

func (unexpectedDelayRepository) Release(context.Context, string, string, int64, error) error {
	return errors.New("unexpected Release")
}

func (unexpectedDelayRepository) Cancel(context.Context, string, string, int64) error {
	return errors.New("unexpected Cancel")
}

var _ delaydomain.DelayTaskRepository = unexpectedDelayRepository{}
