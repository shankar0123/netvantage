// Package transport defines the message transport abstraction for NetVantage.
//
// All communication between the agent and metrics processor goes through
// these interfaces, allowing the underlying transport to be swapped without
// changing business logic. Default: NATS JetStream (M1–M8). Kafka available
// as a production backend from M9+.
package transport

import (
	"context"
	"errors"
)

// ErrClosed is returned when publishing to or subscribing on a closed transport.
var ErrClosed = errors.New("transport: connection closed")

// Publisher sends messages to a named topic.
type Publisher interface {
	// Publish sends msg to the given topic. It blocks until the message is
	// acknowledged by the transport or the context is cancelled.
	Publish(ctx context.Context, topic string, msg []byte) error

	// Close drains in-flight publishes and releases resources.
	Close() error
}

// Consumer receives messages from named topics.
type Consumer interface {
	// Subscribe registers handler for messages on topic. It blocks until
	// ctx is cancelled or an unrecoverable error occurs.
	Subscribe(ctx context.Context, topic string, handler MessageHandler) error

	// Close stops all subscriptions and releases resources.
	Close() error
}

// MessageHandler processes a single message. Returning nil acknowledges the
// message; returning an error triggers redelivery (transport-dependent).
type MessageHandler func(ctx context.Context, msg []byte) error

// Transport combines Publisher and Consumer into a single connection.
// Implementations that support both directions (e.g., NATS) can implement
// this interface directly.
type Transport interface {
	Publisher
	Consumer
}
