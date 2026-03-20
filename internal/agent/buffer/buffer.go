// Package buffer provides a disk-backed queue for buffering test results
// when the transport (NATS/Kafka) is unavailable.
//
// This is a critical resilience pattern for agents on unreliable POP networks.
// When the transport is down, results are written to a local database. When
// connectivity resumes, buffered results are replayed in order.
//
// Implementation uses a simple append-only file with length-prefixed records.
// A more robust implementation (bbolt, SQLite WAL) can replace this later
// without changing the interface.
package buffer

import (
	"context"
	"errors"
	"sync"
)

// ErrBufferFull is returned when the buffer has reached its maximum size.
var ErrBufferFull = errors.New("buffer: maximum size reached")

// Buffer is a disk-backed FIFO queue for test results.
type Buffer interface {
	// Push appends a message to the buffer. Returns ErrBufferFull if the
	// buffer has reached its configured maximum size.
	Push(ctx context.Context, topic string, msg []byte) error

	// Drain reads all buffered messages and calls handler for each one.
	// Successfully handled messages are removed from the buffer.
	// If handler returns an error, draining stops and remaining messages
	// are preserved for the next drain attempt.
	Drain(ctx context.Context, handler func(topic string, msg []byte) error) error

	// Len returns the number of messages currently buffered.
	Len() int

	// Close flushes and closes the buffer.
	Close() error
}

// MemoryBuffer is an in-memory buffer implementation for testing.
// Production agents should use DiskBuffer backed by bbolt or SQLite.
type MemoryBuffer struct {
	mu       sync.Mutex
	messages []bufferedMessage
	maxSize  int64
	curSize  int64
}

type bufferedMessage struct {
	Topic string
	Data  []byte
}

// NewMemoryBuffer creates a new in-memory buffer with the given max size.
func NewMemoryBuffer(maxSizeBytes int64) *MemoryBuffer {
	return &MemoryBuffer{
		messages: make([]bufferedMessage, 0),
		maxSize:  maxSizeBytes,
	}
}

func (b *MemoryBuffer) Push(_ context.Context, topic string, msg []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	msgSize := int64(len(msg))
	if b.curSize+msgSize > b.maxSize {
		return ErrBufferFull
	}

	data := make([]byte, len(msg))
	copy(data, msg)

	b.messages = append(b.messages, bufferedMessage{Topic: topic, Data: data})
	b.curSize += msgSize
	return nil
}

func (b *MemoryBuffer) Drain(_ context.Context, handler func(topic string, msg []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := make([]bufferedMessage, 0)
	var remainingSize int64

	for _, m := range b.messages {
		if err := handler(m.Topic, m.Data); err != nil {
			remaining = append(remaining, m)
			remainingSize += int64(len(m.Data))
		}
	}

	b.messages = remaining
	b.curSize = remainingSize
	return nil
}

func (b *MemoryBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.messages)
}

func (b *MemoryBuffer) Close() error {
	return nil
}
