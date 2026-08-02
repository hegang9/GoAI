package rabbitmq

import (
	"context"
	"errors"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestHandleWithLocalRetrySucceedsAfterTransientFailures(t *testing.T) {
	t.Parallel()

	client := &Client{config: Config{LocalRetryDelaysMs: []int{1, 1}}}
	calls := 0

	err := client.handleWithLocalRetry(func() error {
		calls++
		if calls < 3 {
			return errors.New("temporary database failure")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("handleWithLocalRetry() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("handleWithLocalRetry() calls = %d, want 3", calls)
	}
}

func TestHandleWithLocalRetryReturnsLastTransientErrorWhenExhausted(t *testing.T) {
	t.Parallel()

	client := &Client{config: Config{LocalRetryDelaysMs: []int{1, 1}}}
	wantErr := errors.New("database remains unavailable")
	calls := 0

	err := client.handleWithLocalRetry(func() error {
		calls++
		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("handleWithLocalRetry() error = %v, want %v", err, wantErr)
	}
	if calls != 3 {
		t.Fatalf("handleWithLocalRetry() calls = %d, want 3", calls)
	}
}

func TestHandleWithLocalRetryStopsForNonRetryableFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		wrap func(error) error
	}{
		{name: "permanent", wrap: permanentError},
		{name: "abort", wrap: abortError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{config: Config{LocalRetryDelaysMs: []int{1, 1}}}
			wantErr := errors.New("non-retryable failure")
			calls := 0

			err := client.handleWithLocalRetry(func() error {
				calls++
				return tt.wrap(wantErr)
			})

			if !errors.Is(err, wantErr) {
				t.Fatalf("handleWithLocalRetry() error = %v, want %v", err, wantErr)
			}
			if calls != 1 {
				t.Fatalf("handleWithLocalRetry() calls = %d, want 1", calls)
			}
		})
	}
}

func TestPublishConfirmedRejectsNilChannel(t *testing.T) {
	t.Parallel()

	err := publishConfirmed(
		context.Background(),
		nil,
		make(chan amqp.Return, 1),
		"exchange",
		"routing-key",
		amqp.Publishing{},
	)
	if err == nil || !strings.Contains(err.Error(), "channel is not available") {
		t.Fatalf("publishConfirmed() error = %v, want unavailable channel error", err)
	}
}

func TestDiscardStaleReturns(t *testing.T) {
	t.Parallel()

	returns := make(chan amqp.Return, 2)
	returns <- amqp.Return{ReplyCode: 312}
	returns <- amqp.Return{ReplyCode: 312}

	if err := discardStaleReturns(returns); err != nil {
		t.Fatalf("discardStaleReturns() error = %v", err)
	}
	if got := len(returns); got != 0 {
		t.Fatalf("discardStaleReturns() remaining returns = %d, want 0", got)
	}
}

func TestDiscardStaleReturnsRejectsUnavailableListener(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		returns <-chan amqp.Return
	}{
		{name: "nil listener"},
		{name: "closed listener", returns: closedReturnChannel()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := discardStaleReturns(tt.returns); err == nil {
				t.Fatal("discardStaleReturns() error = nil, want error")
			}
		})
	}
}

func closedReturnChannel() <-chan amqp.Return {
	returns := make(chan amqp.Return)
	close(returns)
	return returns
}

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*Config)
		wantErrSub string
	}{
		{name: "valid"},
		{
			name: "missing main topology",
			mutate: func(cfg *Config) {
				cfg.MainExchange = ""
			},
			wantErrSub: "main topology",
		},
		{
			name: "retry count mismatch",
			mutate: func(cfg *Config) {
				cfg.MaxRetries = 2
			},
			wantErrSub: "does not match",
		},
		{
			name: "jitter exceeds policy",
			mutate: func(cfg *Config) {
				cfg.RetryJitterPercent = 26
			},
			wantErrSub: "between 0 and 25",
		},
		{
			name: "invalid prefetch",
			mutate: func(cfg *Config) {
				cfg.PrefetchCount = 0
			},
			wantErrSub: "prefetch",
		},
		{
			name: "invalid tier delay",
			mutate: func(cfg *Config) {
				cfg.RetryTiers[0].DelayMs = 0
			},
			wantErrSub: "delay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validTestConfig()
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}

			err := validateConfig(cfg)
			if tt.wantErrSub == "" {
				if err != nil {
					t.Fatalf("validateConfig() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("validateConfig() error = %v, want substring %q", err, tt.wantErrSub)
			}
		})
	}
}

func validTestConfig() Config {
	return Config{
		Host:     "127.0.0.1",
		Port:     5672,
		Username: "app",
		Vhost:    "/",

		MainExchange:   "gopherai.chat",
		MainQueue:      "gopherai.chat.persist.v1",
		MainRoutingKey: "chat.message.persist.v1",

		RetryExchange: "gopherai.chat.retry",
		RetryTiers: []RetryTier{
			{Queue: "retry.1", RoutingKey: "retry.1", DelayMs: 10000},
		},
		RetryJitterPercent: 25,
		MaxRetries:         1,

		DeadLetterExchange:   "gopherai.chat.dlx",
		DeadLetterQueue:      "gopherai.chat.persist.dlq.v1",
		DeadLetterRoutingKey: "chat.message.persist.dead.v1",

		PrefetchCount:           20,
		PublishConfirmTimeoutMs: 3000,
	}
}
