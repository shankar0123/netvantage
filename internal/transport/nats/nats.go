// Package nats implements the transport.Transport interface using NATS JetStream.
//
// NATS JetStream is the default transport for M1–M8. It provides:
//   - Persistent streams with at-least-once delivery
//   - Single binary server (no JVM, no ZooKeeper)
//   - Trivial Docker setup
//   - Built-in consumer groups for horizontal scaling
//
// The Kafka implementation (internal/transport/kafka/) is available from M9+
// for production deployments with >50 POPs or when multi-consumer replay
// is needed.
package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/netvantage/netvantage/internal/transport"
)

// Transport implements transport.Transport using NATS JetStream.
type Transport struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	logger *slog.Logger
}

// Config holds NATS connection parameters.
type Config struct {
	URL       string
	TLSCert   string
	TLSKey    string
	TLSCACert string
}

// New creates a new NATS JetStream transport. It connects to the NATS server
// and ensures the required JetStream streams exist.
func New(cfg Config, logger *slog.Logger) (*Transport, error) {
	opts := []nats.Option{
		nats.Name("netvantage-agent"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1), // Reconnect forever.
	}

	// TODO: Add TLS options when cfg.TLSCert is set.

	conn, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect to %s: %w", cfg.URL, err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nats: jetstream context: %w", err)
	}

	t := &Transport{
		conn:   conn,
		js:     js,
		logger: logger,
	}

	// Ensure the netvantage stream exists.
	if err := t.ensureStream(); err != nil {
		conn.Close()
		return nil, err
	}

	return t, nil
}

// ensureStream creates the netvantage JetStream stream if it doesn't exist.
func (t *Transport) ensureStream() error {
	streamName := "NETVANTAGE"
	_, err := t.js.StreamInfo(streamName)
	if err == nil {
		return nil // Stream already exists.
	}

	_, err = t.js.AddStream(&nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{"netvantage.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    24 * time.Hour, // Retain messages for 24h.
		Replicas: 1,
	})
	if err != nil {
		return fmt.Errorf("nats: create stream %s: %w", streamName, err)
	}

	t.logger.Info("created JetStream stream", "name", streamName)
	return nil
}

// Publish sends msg to the given topic via JetStream.
func (t *Transport) Publish(ctx context.Context, topic string, msg []byte) error {
	if t.conn.IsClosed() {
		return transport.ErrClosed
	}

	_, err := t.js.Publish(topic, msg, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("nats: publish to %s: %w", topic, err)
	}
	return nil
}

// Subscribe creates a durable JetStream consumer for the given topic
// and delivers messages to handler. It blocks until ctx is cancelled.
func (t *Transport) Subscribe(ctx context.Context, topic string, handler transport.MessageHandler) error {
	if t.conn.IsClosed() {
		return transport.ErrClosed
	}

	// Create a durable consumer name from the topic.
	consumerName := "netvantage-" + topic

	sub, err := t.js.Subscribe(topic, func(m *nats.Msg) {
		if err := handler(ctx, m.Data); err != nil {
			t.logger.Error("handler error, will redeliver",
				"topic", topic,
				"error", err,
			)
			_ = m.Nak()
			return
		}
		_ = m.Ack()
	}, nats.Durable(consumerName), nats.ManualAck())
	if err != nil {
		return fmt.Errorf("nats: subscribe to %s: %w", topic, err)
	}

	// Block until context is cancelled.
	<-ctx.Done()
	_ = sub.Unsubscribe()
	return nil
}

// Close drains the connection and disconnects from NATS.
func (t *Transport) Close() error {
	if t.conn.IsClosed() {
		return nil
	}
	return t.conn.Drain()
}
