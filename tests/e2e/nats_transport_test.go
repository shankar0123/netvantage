//go:build e2e

package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	natstransport "github.com/netvantage/netvantage/internal/transport/nats"
)

// TestNATSTransport_PublishSubscribe verifies that the NATS JetStream transport
// can publish a message and deliver it to a subscriber using a real NATS server.
func TestNATSTransport_PublishSubscribe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	transport, err := natstransport.New(natstransport.Config{URL: sharedNATSURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create NATS transport: %v", err)
	}
	defer transport.Close()

	// Use a unique topic to avoid collisions with other tests on the shared NATS.
	topic := "netvantage.e2e.pubsub"
	payload := []byte(`{"test_id":"e2e-1","agent_id":"agent-e2e","success":true}`)

	var (
		received []byte
		mu       sync.Mutex
		done     = make(chan struct{})
	)

	subCtx, subCancel := context.WithCancel(ctx)
	go func() {
		_ = transport.Subscribe(subCtx, topic, func(_ context.Context, msg []byte) error {
			mu.Lock()
			received = make([]byte, len(msg))
			copy(received, msg)
			mu.Unlock()
			close(done)
			return nil
		})
	}()

	// Give the JetStream subscription time to establish.
	time.Sleep(1 * time.Second)

	if err := transport.Publish(ctx, topic, payload); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	select {
	case <-done:
		// Success.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for NATS message delivery")
	}

	subCancel()

	mu.Lock()
	defer mu.Unlock()
	if string(received) != string(payload) {
		t.Errorf("received message mismatch:\n  got:  %s\n  want: %s", received, payload)
	}
}

// TestNATSTransport_MultipleSubscribers verifies that multiple subscribers on
// different topics each receive their own messages independently.
func TestNATSTransport_MultipleSubscribers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	transport, err := natstransport.New(natstransport.Config{URL: sharedNATSURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create NATS transport: %v", err)
	}
	defer transport.Close()

	topics := []string{"netvantage.e2e.multi1", "netvantage.e2e.multi2"}
	var (
		mu       sync.Mutex
		received = make(map[string][]byte)
		wg       sync.WaitGroup
	)

	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()

	for _, topic := range topics {
		topic := topic
		wg.Add(1)
		go func() {
			_ = transport.Subscribe(subCtx, topic, func(_ context.Context, msg []byte) error {
				mu.Lock()
				received[topic] = msg
				mu.Unlock()
				wg.Done()
				return nil
			})
		}()
	}

	time.Sleep(1 * time.Second)

	for _, topic := range topics {
		payload := []byte(`{"topic":"` + topic + `"}`)
		if err := transport.Publish(ctx, topic, payload); err != nil {
			t.Fatalf("publish to %s failed: %v", topic, err)
		}
	}

	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Success.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for multi-topic delivery")
	}

	mu.Lock()
	defer mu.Unlock()

	for _, topic := range topics {
		if _, ok := received[topic]; !ok {
			t.Errorf("no message received for topic %s", topic)
		}
	}
}

// TestNATSTransport_ClosePreventsFurtherPublish verifies that closing the
// transport returns an error on subsequent publish attempts.
func TestNATSTransport_ClosePreventsFurtherPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create a separate transport for this test since we need to close it.
	transport, err := natstransport.New(natstransport.Config{URL: sharedNATSURL}, testLogger())
	if err != nil {
		t.Fatalf("failed to create NATS transport: %v", err)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	err = transport.Publish(ctx, "netvantage.e2e.closed", []byte(`{}`))
	if err == nil {
		t.Error("expected error publishing to closed transport, got nil")
	}
}
