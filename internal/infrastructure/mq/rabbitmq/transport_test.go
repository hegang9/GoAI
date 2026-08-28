package rabbitmq

import (
	"context"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

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

	closed := make(chan amqp.Return)
	close(closed)
	for name, returns := range map[string]<-chan amqp.Return{
		"nil":    nil,
		"closed": closed,
	} {
		t.Run(name, func(t *testing.T) {
			if err := discardStaleReturns(returns); err == nil {
				t.Fatal("discardStaleReturns() error = nil, want error")
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError string
	}{
		{
			name: "valid",
			config: Config{
				Host: "127.0.0.1", Port: 5672, Username: "app", Vhost: "/",
			},
		},
		{name: "empty host", config: Config{Port: 5672, Username: "app"}, wantError: "host"},
		{name: "invalid port", config: Config{Host: "127.0.0.1", Username: "app"}, wantError: "port"},
		{name: "empty username", config: Config{Host: "127.0.0.1", Port: 5672}, wantError: "username"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateConfig(test.config)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateConfig() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateConfig() error = %v, want %q", err, test.wantError)
			}
		})
	}
}
