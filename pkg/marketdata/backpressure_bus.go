package marketdata

import (
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// DefaultBackpressureBufferSize is the default per-subscriber buffer
// size (10,000 events). When a subscriber's buffer fills up, the
// oldest event is dropped to make room for the new one.
const DefaultBackpressureBufferSize = 10000

// BackpressureBus is a high-performance event bus with drop-oldest
// backpressure (P2-28, AR-018).
//
// Each subscriber gets its own goroutine and buffered channel. When a
// subscriber's buffer is full, the oldest event is dropped (removed
// from the front of the buffer) to make room for the new event. This
// ensures that:
//
//   - Publishers never block (Publish is always non-blocking).
//   - Slow subscribers don't stall fast ones (each has its own buffer).
//   - The most recent events are prioritised over the oldest ones
//     (drop-oldest, not drop-newest).
//
// The bus is safe for concurrent use. A sync.RWMutex protects the
// subscriber map; per-subscriber channels provide the backpressure.
// Close blocks until all subscriber goroutines have exited, ensuring
// no handler runs after Close returns.
type BackpressureBus struct {
	mu          sync.RWMutex
	subscribers map[string][]*bpSubscriber
	nextID      uint64
	bufferSize  int
	closed      bool
	closeCh     chan struct{}

	// Bus-level metrics (atomic).
	publishedCount int64
	droppedCount   int64

	// wg tracks all subscriber goroutines so Close can wait for them.
	wg sync.WaitGroup

	log zerolog.Logger
}

// bpSubscriber is a single subscription with its own buffer and goroutine.
type bpSubscriber struct {
	id       uint64
	topic    string
	buffer   chan any
	handler  func(any)
	stopCh   chan struct{}
	stopOnce sync.Once

	// Per-subscriber dropped count (atomic).
	dropped int64
}

// BackpressureMetrics is a point-in-time snapshot of bus metrics.
type BackpressureMetrics struct {
	PublishedCount  int64 `json:"published_count"`
	DroppedCount    int64 `json:"dropped_count"`
	SubscriberCount int64 `json:"subscriber_count"`
}

// NewBackpressureBus creates a new BackpressureBus with the given
// per-subscriber buffer size. A size ≤ 0 uses the default (10,000).
func NewBackpressureBus(bufferSize int) *BackpressureBus {
	if bufferSize <= 0 {
		bufferSize = DefaultBackpressureBufferSize
	}
	return &BackpressureBus{
		subscribers: make(map[string][]*bpSubscriber),
		bufferSize:  bufferSize,
		closeCh:     make(chan struct{}),
		log:         zerolog.Nop(),
	}
}

// SetLogger installs a logger for bus diagnostics.
func (b *BackpressureBus) SetLogger(l zerolog.Logger) {
	b.log = l
}

// Subscribe registers a handler for the given topic. Returns an
// unsubscribe function; calling it stops the subscriber's goroutine
// and removes it from the bus. The handler is called from a dedicated
// goroutine; it should be safe to call concurrently with other handlers.
//
// If the bus is closed, Subscribe returns a no-op unsubscribe function.
func (b *BackpressureBus) Subscribe(topic string, handler func(any)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return func() {}
	}

	b.nextID++
	sub := &bpSubscriber{
		id:      b.nextID,
		topic:   topic,
		buffer:  make(chan any, b.bufferSize),
		handler: handler,
		stopCh:  make(chan struct{}),
	}
	b.subscribers[topic] = append(b.subscribers[topic], sub)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.subscriberLoop(sub)
	}()

	return func() { b.unsubscribe(sub) }
}

// subscriberLoop is the per-subscriber goroutine that reads events
// from the buffer and calls the handler. It exits when the subscriber
// is stopped (via unsubscribe or bus Close).
func (b *BackpressureBus) subscriberLoop(sub *bpSubscriber) {
	for {
		select {
		case event := <-sub.buffer:
			b.safeHandle(sub, event)
		case <-sub.stopCh:
			// Drain remaining events before exiting.
			for {
				select {
				case event := <-sub.buffer:
					b.safeHandle(sub, event)
				default:
					return
				}
			}
		}
	}
}

// safeHandle calls the subscriber's handler, recovering from panics
// so a buggy handler doesn't crash the goroutine.
func (b *BackpressureBus) safeHandle(sub *bpSubscriber, event any) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error().Interface("panic", r).Str("topic", sub.topic).
				Uint64("sub_id", sub.id).Msg("backpressure bus: handler panic recovered")
		}
	}()
	sub.handler(event)
}

// unsubscribe removes a subscriber and stops its goroutine.
func (b *BackpressureBus) unsubscribe(sub *bpSubscriber) {
	b.mu.Lock()
	subs := b.subscribers[sub.topic]
	for i, s := range subs {
		if s.id == sub.id {
			b.subscribers[sub.topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(b.subscribers[sub.topic]) == 0 {
		delete(b.subscribers, sub.topic)
	}
	b.mu.Unlock()

	sub.stop()
}

// stop signals the subscriber's goroutine to exit. Idempotent.
func (s *bpSubscriber) stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Publish sends an event to all subscribers of the given topic.
// This is non-blocking: if a subscriber's buffer is full, the oldest
// event is dropped (drop-oldest backpressure). The event is never
// delivered to subscribers that subscribed after Publish returns.
func (b *BackpressureBus) Publish(topic string, event any) {
	atomic.AddInt64(&b.publishedCount, 1)

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	subs := b.subscribers[topic]
	// Copy subscriber pointers to avoid holding the lock during send.
	subCopy := make([]*bpSubscriber, len(subs))
	copy(subCopy, subs)
	b.mu.RUnlock()

	for _, sub := range subCopy {
		b.publishToSubscriber(sub, event)
	}
}

// publishToSubscriber delivers an event to a single subscriber,
// applying the drop-oldest backpressure strategy.
func (b *BackpressureBus) publishToSubscriber(sub *bpSubscriber, event any) {
	// Fast path: non-blocking send.
	select {
	case sub.buffer <- event:
		return
	default:
	}

	// Slow path: buffer is full. Drop the oldest event to make room.
	select {
	case <-sub.buffer:
		atomic.AddInt64(&sub.dropped, 1)
		atomic.AddInt64(&b.droppedCount, 1)
	default:
		// Buffer was drained between selects (another consumer read
		// the last event). Fall through to retry the send.
	}

	// Retry send after dropping.
	select {
	case sub.buffer <- event:
		return
	default:
		// Still full (multiple producers racing): drop this event.
		atomic.AddInt64(&sub.dropped, 1)
		atomic.AddInt64(&b.droppedCount, 1)
	}
}

// Close shuts down the bus. All subscriber goroutines are stopped.
// Pending events in each subscriber's buffer are drained (handlers
// are called for each). After Close, Subscribe is a no-op and Publish
// drops events silently. Close blocks until all goroutines have exited.
// Close is idempotent.
func (b *BackpressureBus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	// Stop all subscribers.
	for _, subs := range b.subscribers {
		for _, sub := range subs {
			sub.stop()
		}
	}
	b.subscribers = nil
	b.mu.Unlock()
	close(b.closeCh)
	// Wait for all goroutines to finish so no handler runs after Close.
	b.wg.Wait()
}

// Metrics returns a point-in-time snapshot of bus-wide metrics.
func (b *BackpressureBus) Metrics() BackpressureMetrics {
	b.mu.RLock()
	subCount := 0
	for _, subs := range b.subscribers {
		subCount += len(subs)
	}
	b.mu.RUnlock()

	return BackpressureMetrics{
		PublishedCount:  atomic.LoadInt64(&b.publishedCount),
		DroppedCount:    atomic.LoadInt64(&b.droppedCount),
		SubscriberCount: int64(subCount),
	}
}

// SubscriberDroppedCount returns the number of events dropped for a
// specific subscriber. Useful for per-subscriber backpressure
// monitoring. Returns 0 if the subscriber doesn't exist.
func (b *BackpressureBus) SubscriberDroppedCount(topic string, subID uint64) int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subscribers[topic] {
		if sub.id == subID {
			return atomic.LoadInt64(&sub.dropped)
		}
	}
	return 0
}

// SubscriberCount returns the number of active subscribers for a topic.
func (b *BackpressureBus) SubscriberCount(topic string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[topic])
}

// IsClosed reports whether the bus has been closed.
func (b *BackpressureBus) IsClosed() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed
}

// BufferSize returns the configured per-subscriber buffer size.
func (b *BackpressureBus) BufferSize() int {
	return b.bufferSize
}
