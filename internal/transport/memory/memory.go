// Package memory provides an in-memory transport implementation for unit tests.
//
// This transport delivers messages synchronously within the same process.
// It is NOT suitable for production use — use NATS or Kafka instead.
package memory

import (
	"context"
	"sync"

	"github.com/netvantage/netvantage/internal/transport"
)

// Transport is an in-memory implementation of transport.Transport for testing.
type Transport struct {
	mu       sync.RWMutex
	handlers map[string][]transport.MessageHandler
	closed   bool
}

// New creates a new in-memory transport.
func New() *Transport {
	return &Transport{
		handlers: make(map[string][]transport.MessageHandler),
	}
}

// Publish delivers msg to all handlers subscribed to topic. Blocks until
// all handlers have been called. Returns transport.ErrClosed if the
// transport has been closed.
func (t *Transport) Publish(ctx context.Context, topic string, msg []byte) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.closed {
		return transport.ErrClosed
	}

	handlers, ok := t.handlers[topic]
	if !ok {
		return nil // No subscribers — message is silently dropped.
	}

	data := make([]byte, len(msg))
	copy(data, msg)

	for _, h := range handlers {
		if err := h(ctx, data); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe registers handler for messages on topic. Unlike production
// transports, this returns immediately (does not block).
func (t *Transport) Subscribe(_ context.Context, topic string, handler transport.MessageHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return transport.ErrClosed
	}

	t.handlers[topic] = append(t.handlers[topic], handler)
	return nil
}

// Close marks the transport as closed. Subsequent Publish and Subscribe
// calls will return transport.ErrClosed.
func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}
