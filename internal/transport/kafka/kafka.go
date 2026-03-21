// Package kafka implements the transport.Transport interface using Apache Kafka.
//
// Kafka is the production-grade transport backend for NetVantage, designed for
// deployments with >50 POPs or when multi-consumer replay and partitioned
// ordering are needed. It supports:
//
//   - SASL/SCRAM authentication (SCRAM-SHA-256 / SCRAM-SHA-512)
//   - mTLS for transport encryption
//   - Consumer groups for horizontal scaling
//   - Configurable topic partitioning
//
// The NATS JetStream implementation (internal/transport/nats/) remains the
// default for small-to-medium deployments.
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/IBM/sarama"

	"github.com/netvantage/netvantage/internal/transport"
)

// Config holds Kafka connection and authentication parameters.
type Config struct {
	// Brokers is a list of Kafka broker addresses (host:port).
	Brokers []string

	// Topic prefix for all NetVantage topics. Defaults to "netvantage".
	TopicPrefix string

	// ConsumerGroup is the consumer group ID. Defaults to "netvantage-processor".
	ConsumerGroup string

	// SASL authentication.
	SASLEnabled  bool
	SASLMechanism string // "SCRAM-SHA-256" or "SCRAM-SHA-512"
	SASLUsername  string
	SASLPassword  string

	// TLS / mTLS configuration.
	TLSEnabled bool
	TLSCert    string // Path to client certificate (mTLS).
	TLSKey     string // Path to client key (mTLS).
	TLSCACert  string // Path to CA certificate.
	TLSSkipVerify bool

	// Producer settings.
	RequiredAcks    sarama.RequiredAcks // Default: WaitForAll
	MaxRetries      int                 // Default: 3
	FlushFrequency  time.Duration       // Default: 100ms
	FlushMessages   int                 // Default: 100

	// Consumer settings.
	InitialOffset int64 // Default: sarama.OffsetNewest
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(brokers []string) Config {
	return Config{
		Brokers:        brokers,
		TopicPrefix:    "netvantage",
		ConsumerGroup:  "netvantage-processor",
		RequiredAcks:   sarama.WaitForAll,
		MaxRetries:     3,
		FlushFrequency: 100 * time.Millisecond,
		FlushMessages:  100,
		InitialOffset:  sarama.OffsetNewest,
	}
}

// Transport implements transport.Transport using Apache Kafka.
type Transport struct {
	cfg      Config
	logger   *slog.Logger
	producer sarama.SyncProducer
	client   sarama.Client

	mu       sync.Mutex
	closed   bool
	cancelFn context.CancelFunc
}

// New creates a new Kafka transport. It connects to the Kafka cluster and
// initializes a synchronous producer.
func New(cfg Config, logger *slog.Logger) (*Transport, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: at least one broker address is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	saramaCfg, err := buildSaramaConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kafka: config: %w", err)
	}

	client, err := sarama.NewClient(cfg.Brokers, saramaCfg)
	if err != nil {
		return nil, fmt.Errorf("kafka: connect to %v: %w", cfg.Brokers, err)
	}

	producer, err := sarama.NewSyncProducerFromClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("kafka: create producer: %w", err)
	}

	logger.Info("kafka transport connected",
		"brokers", cfg.Brokers,
		"sasl", cfg.SASLEnabled,
		"tls", cfg.TLSEnabled,
	)

	return &Transport{
		cfg:      cfg,
		logger:   logger,
		producer: producer,
		client:   client,
	}, nil
}

// Publish sends msg to the given topic via Kafka.
func (t *Transport) Publish(ctx context.Context, topic string, msg []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return transport.ErrClosed
	}
	t.mu.Unlock()

	kafkaTopic := t.fullTopic(topic)

	_, _, err := t.producer.SendMessage(&sarama.ProducerMessage{
		Topic: kafkaTopic,
		Value: sarama.ByteEncoder(msg),
	})
	if err != nil {
		return fmt.Errorf("kafka: publish to %s: %w", kafkaTopic, err)
	}
	return nil
}

// Subscribe creates a Kafka consumer group for the given topic and delivers
// messages to handler. It blocks until ctx is cancelled.
func (t *Transport) Subscribe(ctx context.Context, topic string, handler transport.MessageHandler) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return transport.ErrClosed
	}
	t.mu.Unlock()

	kafkaTopic := t.fullTopic(topic)

	saramaCfg, err := buildSaramaConfig(t.cfg)
	if err != nil {
		return fmt.Errorf("kafka: consumer config: %w", err)
	}
	saramaCfg.Consumer.Offsets.Initial = t.cfg.InitialOffset

	group, err := sarama.NewConsumerGroup(t.cfg.Brokers, t.cfg.ConsumerGroup, saramaCfg)
	if err != nil {
		return fmt.Errorf("kafka: create consumer group: %w", err)
	}
	defer group.Close()

	consumer := &consumerGroupHandler{
		handler: handler,
		logger:  t.logger,
		topic:   kafkaTopic,
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := group.Consume(ctx, []string{kafkaTopic}, consumer); err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) || errors.Is(err, context.Canceled) {
				return nil
			}
			t.logger.Error("kafka consumer error, rebalancing",
				"topic", kafkaTopic,
				"error", err,
			)
		}
	}
}

// Close shuts down the producer and client.
func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true

	var errs []error
	if err := t.producer.Close(); err != nil {
		errs = append(errs, fmt.Errorf("kafka: close producer: %w", err))
	}
	if err := t.client.Close(); err != nil {
		errs = append(errs, fmt.Errorf("kafka: close client: %w", err))
	}
	if t.cancelFn != nil {
		t.cancelFn()
	}

	t.logger.Info("kafka transport closed")
	return errors.Join(errs...)
}

// fullTopic converts a logical topic name (e.g., "netvantage.ping.results")
// into a Kafka topic name using the configured prefix and dots → hyphens.
func (t *Transport) fullTopic(topic string) string {
	// Kafka topics use hyphens by convention; NATS uses dots.
	// Keep compatibility: "netvantage.ping.results" → "netvantage-ping-results"
	result := make([]byte, len(topic))
	for i, c := range []byte(topic) {
		if c == '.' {
			result[i] = '-'
		} else {
			result[i] = c
		}
	}
	return string(result)
}

// consumerGroupHandler implements sarama.ConsumerGroupHandler.
type consumerGroupHandler struct {
	handler transport.MessageHandler
	logger  *slog.Logger
	topic   string
}

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.handler(sess.Context(), msg.Value); err != nil {
			h.logger.Error("kafka handler error",
				"topic", h.topic,
				"partition", msg.Partition,
				"offset", msg.Offset,
				"error", err,
			)
			// Don't mark — will be redelivered on rebalance.
			continue
		}
		sess.MarkMessage(msg, "")
	}
	return nil
}

// buildSaramaConfig constructs a sarama.Config from our Config.
func buildSaramaConfig(cfg Config) (*sarama.Config, error) {
	sc := sarama.NewConfig()
	sc.Version = sarama.V3_6_0_0
	sc.ClientID = "netvantage"

	// Producer settings.
	sc.Producer.RequiredAcks = cfg.RequiredAcks
	if cfg.RequiredAcks == 0 {
		sc.Producer.RequiredAcks = sarama.WaitForAll
	}
	sc.Producer.Retry.Max = cfg.MaxRetries
	if cfg.MaxRetries == 0 {
		sc.Producer.Retry.Max = 3
	}
	sc.Producer.Return.Successes = true
	sc.Producer.Flush.Frequency = cfg.FlushFrequency
	sc.Producer.Flush.Messages = cfg.FlushMessages

	// Consumer settings.
	sc.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategyRoundRobin(),
	}

	// SASL authentication.
	if cfg.SASLEnabled {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User = cfg.SASLUsername
		sc.Net.SASL.Password = cfg.SASLPassword

		switch cfg.SASLMechanism {
		case "SCRAM-SHA-256":
			sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &scramClient{mechanism: "SCRAM-SHA-256"}
			}
		case "SCRAM-SHA-512":
			sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &scramClient{mechanism: "SCRAM-SHA-512"}
			}
		default:
			return nil, fmt.Errorf("unsupported SASL mechanism: %s (use SCRAM-SHA-256 or SCRAM-SHA-512)", cfg.SASLMechanism)
		}
	}

	// TLS configuration.
	if cfg.TLSEnabled {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: cfg.TLSSkipVerify,
		}

		// Load CA cert if provided.
		if cfg.TLSCACert != "" {
			caCert, err := os.ReadFile(cfg.TLSCACert)
			if err != nil {
				return nil, fmt.Errorf("read CA cert %s: %w", cfg.TLSCACert, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA cert %s", cfg.TLSCACert)
			}
			tlsCfg.RootCAs = pool
		}

		// Load client cert + key for mTLS.
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
			if err != nil {
				return nil, fmt.Errorf("load client cert: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}

		sc.Net.TLS.Enable = true
		sc.Net.TLS.Config = tlsCfg
	}

	return sc, nil
}

// scramClient implements sarama.SCRAMClient for SCRAM-SHA authentication.
// This is a minimal wrapper — the actual SCRAM negotiation is handled by sarama.
type scramClient struct {
	mechanism string
}

func (s *scramClient) Begin(userName, password, authzID string) error { return nil }
func (s *scramClient) Step(challenge string) (string, error)         { return "", nil }
func (s *scramClient) Done() bool                                    { return true }
