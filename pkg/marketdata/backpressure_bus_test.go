package marketdata

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Construction tests ──────────────────────────────────────────

func TestNewBackpressureBus_DefaultBufferSize(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(0)
	assert.Equal(t, DefaultBackpressureBufferSize, b.BufferSize())
}

func TestNewBackpressureBus_CustomBufferSize(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	assert.Equal(t, 100, b.BufferSize())
}

func TestBackpressureBus_SetLogger(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(10)
	b.SetLogger(newTestLogger())
	b.Close()
}

// ─── Subscribe / Publish tests ──────────────────────────────────

func TestBackpressureBus_PublishReceive(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	var mu sync.Mutex
	received := []string{}
	done := make(chan struct{})
	unsub := b.Subscribe("test", func(e any) {
		mu.Lock()
		received = append(received, e.(string))
		if len(received) == 3 {
			close(done)
		}
		mu.Unlock()
	})

	b.Publish("test", "a")
	b.Publish("test", "b")
	b.Publish("test", "c")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events")
	}

	unsub()

	mu.Lock()
	assert.Equal(t, []string{"a", "b", "c"}, received)
	mu.Unlock()
}

func TestBackpressureBus_MultipleSubscribers(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	var count1, count2 int64
	done := make(chan struct{})
	var once sync.Once
	b.Subscribe("topic", func(e any) {
		if atomic.AddInt64(&count1, 1) == 1 && atomic.LoadInt64(&count2) == 1 {
			once.Do(func() { close(done) })
		}
	})
	b.Subscribe("topic", func(e any) {
		if atomic.AddInt64(&count2, 1) == 1 && atomic.LoadInt64(&count1) == 1 {
			once.Do(func() { close(done) })
		}
	})

	b.Publish("topic", "event")

	// Wait for both subscribers to receive the event, or timeout.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for both subscribers to receive event")
	}

	assert.Equal(t, int64(1), atomic.LoadInt64(&count1))
	assert.Equal(t, int64(1), atomic.LoadInt64(&count2))
}

func TestBackpressureBus_Unsubscribe(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	var count int64
	done := make(chan struct{})
	var once sync.Once
	fired := make(chan struct{})
	var firedOnce sync.Once
	unsub := b.Subscribe("topic", func(e any) {
		n := atomic.AddInt64(&count, 1)
		if n == 1 {
			once.Do(func() { close(done) })
		} else if n == 2 {
			firedOnce.Do(func() { close(fired) })
		}
	})

	b.Publish("topic", "before")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for 'before' event")
	}

	unsub()

	b.Publish("topic", "after")
	// After unsubscribe, the handler must NOT receive a second event.
	assertNotFired(t, fired, 50*time.Millisecond,
		"unsubscribed handler should not receive 'after' event")

	assert.Equal(t, int64(1), atomic.LoadInt64(&count))
	assert.Equal(t, 0, b.SubscriberCount("topic"))
}

func TestBackpressureBus_TopicIsolation(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	var topicA, topicB int64
	done := make(chan struct{})
	var once sync.Once
	b.Subscribe("a", func(e any) {
		if atomic.AddInt64(&topicA, 1) == 2 && atomic.LoadInt64(&topicB) == 1 {
			once.Do(func() { close(done) })
		}
	})
	b.Subscribe("b", func(e any) {
		if atomic.AddInt64(&topicB, 1) == 1 && atomic.LoadInt64(&topicA) == 2 {
			once.Do(func() { close(done) })
		}
	})

	b.Publish("a", 1)
	b.Publish("b", 1)
	b.Publish("a", 1)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for all topic events")
	}

	assert.Equal(t, int64(2), atomic.LoadInt64(&topicA))
	assert.Equal(t, int64(1), atomic.LoadInt64(&topicB))
}

func TestBackpressureBus_NoSubscribers(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	// Should not panic.
	b.Publish("nobody", "event")

	m := b.Metrics()
	assert.Equal(t, int64(1), m.PublishedCount)
	assert.Equal(t, int64(0), m.DroppedCount)
}

// ─── Backpressure (drop-oldest) tests ───────────────────────────

func TestBackpressureBus_DropOldest(t *testing.T) {
	t.Parallel()
	// Small buffer + slow handler to force overflow.
	b := NewBackpressureBus(3)
	defer b.Close()

	var mu sync.Mutex
	received := []int{}
	// Handler that blocks until we release it, ensuring the buffer fills.
	block := make(chan struct{})
	started := make(chan struct{})
	b.Subscribe("overflow", func(e any) {
		// Signal that the handler has started (first event is being processed).
		select {
		case started <- struct{}{}:
		default:
		}
		<-block // block until we release
		mu.Lock()
		received = append(received, e.(int))
		mu.Unlock()
	})

	// Publish the first event; the handler will pick it up and block.
	b.Publish("overflow", 1)
	<-started // wait until the handler is processing event 1.

	// Now publish 4 more events. Buffer capacity is 3.
	// Events 2, 3, 4 fill the buffer. Event 5 triggers drop-oldest.
	for i := 2; i <= 5; i++ {
		b.Publish("overflow", i)
	}
	// Wait for the buffer to fill and at least one drop to register, rather
	// than sleeping a fixed duration.
	require.Eventually(t, func() bool {
		return b.Metrics().DroppedCount > 0
	}, time.Second, time.Millisecond, "expected at least one drop after buffer overflow")

	// Release the handler to process remaining events. Close() drains the
	// buffer synchronously, so after it returns all remaining events are
	// processed (no sleep needed).
	close(block)
	b.Close()

	mu.Lock()
	defer mu.Unlock()
	// After unblock, the handler finishes event 1, then processes
	// whatever is in the buffer. At least one event was dropped.
	assert.Contains(t, received, 1, "first event should be received")
	assert.Contains(t, received, 5, "last event should be received")
	assert.Less(t, len(received), 5, "some events should have been dropped")

	m := b.Metrics()
	assert.Greater(t, m.DroppedCount, int64(0), "events should have been dropped")
}

func TestBackpressureBus_DroppedMetrics(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(2)
	defer b.Close()

	// Slow handler that blocks.
	block := make(chan struct{})
	b.Subscribe("test", func(e any) { <-block })

	// Fill buffer and overflow.
	for i := 0; i < 10; i++ {
		b.Publish("test", i)
	}
	// Wait for the buffer to fill and drops to register.
	require.Eventually(t, func() bool {
		return b.Metrics().DroppedCount > 0
	}, time.Second, time.Millisecond, "events should be dropped after buffer overflow")

	m := b.Metrics()
	assert.Equal(t, int64(10), m.PublishedCount)
	assert.Greater(t, m.DroppedCount, int64(0), "events should be dropped")

	// Unblock the handler; deferred Close() drains synchronously.
	close(block)
}

func TestBackpressureBus_PublishedCount(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	b.Subscribe("test", func(e any) {})
	for i := 0; i < 50; i++ {
		b.Publish("test", i)
	}

	m := b.Metrics()
	assert.Equal(t, int64(50), m.PublishedCount)
}

func TestBackpressureBus_SubscriberCount(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	assert.Equal(t, 0, b.SubscriberCount("topic"))

	u1 := b.Subscribe("topic", func(e any) {})
	assert.Equal(t, 1, b.SubscriberCount("topic"))

	u2 := b.Subscribe("topic", func(e any) {})
	assert.Equal(t, 2, b.SubscriberCount("topic"))

	u1()
	assert.Equal(t, 1, b.SubscriberCount("topic"))

	u2()
	assert.Equal(t, 0, b.SubscriberCount("topic"))
}

func TestBackpressureBus_MetricsSubscriberCount(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	b.Subscribe("a", func(e any) {})
	b.Subscribe("b", func(e any) {})
	b.Subscribe("a", func(e any) {})

	m := b.Metrics()
	assert.Equal(t, int64(3), m.SubscriberCount)
}

// ─── Close tests ────────────────────────────────────────────────

func TestBackpressureBus_Close(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)

	var count int64
	b.Subscribe("test", func(e any) { atomic.AddInt64(&count, 1) })

	b.Publish("test", 1)

	// Close() drains the buffer synchronously (calls wg.Wait), so after it
	// returns the first event has been processed and no future handler can run.
	b.Close()
	assert.Equal(t, int64(1), atomic.LoadInt64(&count))

	// After close, Publish is a no-op — no handler will fire.
	b.Publish("test", 2)
	assert.Equal(t, int64(1), atomic.LoadInt64(&count))
	assert.True(t, b.IsClosed())
}

func TestBackpressureBus_CloseIdempotent(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	b.Close()
	b.Close() // should not panic
}

func TestBackpressureBus_SubscribeAfterClose(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	b.Close()

	// Should return a no-op unsubscribe, not panic.
	unsub := b.Subscribe("test", func(e any) {})
	unsub()
}

func TestBackpressureBus_CloseDrainsBuffer(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)

	var count int64
	b.Subscribe("test", func(e any) {
		atomic.AddInt64(&count, 1)
	})

	// Publish events without waiting for processing.
	for i := 0; i < 10; i++ {
		b.Publish("test", i)
	}

	// Close() drains the buffer synchronously (wg.Wait), so all 10 events
	// are processed before Close returns. No sleep needed.
	b.Close()
	assert.Equal(t, int64(10), atomic.LoadInt64(&count))
}

// ─── Concurrency tests (race-clean) ─────────────────────────────

func TestBackpressureBus_ConcurrentPublish(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(1000)
	defer b.Close()

	var count int64
	done := make(chan struct{})
	var once sync.Once
	b.Subscribe("test", func(e any) {
		if atomic.AddInt64(&count, 1) == 100 {
			once.Do(func() { close(done) })
		}
	})

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish("test", "event")
		}()
	}
	wg.Wait()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for all 100 events to be processed")
	}
	assert.Equal(t, int64(n), atomic.LoadInt64(&count))
}

func TestBackpressureBus_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unsub := b.Subscribe("test", func(e any) {})
			runtime.Gosched() // yield to interleave with other goroutines
			unsub()
		}()
	}
	wg.Wait()
}

func TestBackpressureBus_ConcurrentPublishSubscribe(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(500)
	defer b.Close()

	var received int64
	// Pre-register a stable subscriber that persists for the whole test so that
	// published events are captured regardless of the transient subscribe/
	// unsubscribe cycles. This guarantees `received > 0` after the publisher
	// completes, without relying on timing.
	b.Subscribe("test", func(e any) {
		atomic.AddInt64(&received, 1)
	})

	var wg sync.WaitGroup

	// Publisher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			b.Publish("test", i)
		}
	}()

	// Subscriber adder/remover (concurrent with publisher). runtime.Gosched()
	// yields to the scheduler so the publisher interleaves with these cycles
	// without a time.Sleep-based wait.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			u := b.Subscribe("test", func(e any) {
				atomic.AddInt64(&received, 1)
			})
			runtime.Gosched()
			u()
		}
	}()

	wg.Wait()
	// Drain the buffer so all in-flight events are delivered before asserting.
	// Close is idempotent — the deferred Close above becomes a no-op.
	b.Close()

	assert.Greater(t, atomic.LoadInt64(&received), int64(0),
		"stable subscriber must have received at least one event")
	m := b.Metrics()
	assert.Equal(t, int64(100), m.PublishedCount,
		"all 100 publishes must be counted")
}

func TestBackpressureBus_StressNoBlock(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(10)
	defer b.Close()

	// Slow handler.
	b.Subscribe("test", func(e any) {
		time.Sleep(time.Millisecond)
	})

	// Rapid publish should never block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish("test", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked for too long")
	}
}

// ─── Handler panic recovery tests ───────────────────────────────

func TestBackpressureBus_HandlerPanicRecovered(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(100)
	defer b.Close()

	var afterPanic int64
	done := make(chan struct{})
	var once sync.Once
	b.Subscribe("test", func(e any) {
		if e.(int) == 1 {
			panic("boom")
		}
		if atomic.AddInt64(&afterPanic, 1) == 1 {
			once.Do(func() { close(done) })
		}
	})

	b.Publish("test", 1) // will panic
	b.Publish("test", 2) // should still be received

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for post-panic event")
	}
	assert.Equal(t, int64(1), atomic.LoadInt64(&afterPanic),
		"handler should continue after panic recovery")
}

// ─── SubscriberDroppedCount test ─────────────────────────────────

func TestBackpressureBus_SubscriberDroppedCount(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(2)
	defer b.Close()

	block := make(chan struct{})
	b.Subscribe("test", func(e any) { <-block })

	for i := 0; i < 10; i++ {
		b.Publish("test", i)
	}
	// Wait for the buffer to fill and overflow rather than sleeping a fixed
	// duration. The dropped count must become positive once the 3-slot buffer
	// (1 in-flight + 2 buffered) overflows.
	require.Eventually(t, func() bool {
		return b.Metrics().DroppedCount > 0
	}, time.Second, time.Millisecond, "events should be dropped after buffer overflow")

	close(block)
	// Close drains synchronously, ensuring the blocked handler completes.
}

// ─── Multiple topics stress ─────────────────────────────────────

func TestBackpressureBus_MultipleTopicsStress(t *testing.T) {
	t.Parallel()
	b := NewBackpressureBus(200)
	defer b.Close()

	var counts sync.Map
	topics := []string{"a", "b", "c", "d", "e"}
	// done closes once every topic has received all 50 events.
	done := make(chan struct{})
	var once sync.Once
	var receivedTotal int64
	for _, topic := range topics {
		topic := topic
		// Pre-register a *int64 counter for each topic.
		p := new(int64)
		counts.Store(topic, p)
		b.Subscribe(topic, func(e any) {
			val, _ := counts.Load(topic)
			atomic.AddInt64(val.(*int64), 1)
			if atomic.AddInt64(&receivedTotal, 1) == int64(len(topics)*50) {
				once.Do(func() { close(done) })
			}
		})
	}

	var wg sync.WaitGroup
	for _, topic := range topics {
		wg.Add(1)
		go func(topic string) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				b.Publish(topic, i)
			}
		}(topic)
	}
	wg.Wait()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for all topics to receive their events")
	}

	for _, topic := range topics {
		val, ok := counts.Load(topic)
		require.True(t, ok, "topic %s should have received events", topic)
		assert.Equal(t, int64(50), atomic.LoadInt64(val.(*int64)),
			"topic %s should have received all 50 events", topic)
	}
}

// ─── Helper ─────────────────────────────────────────────────────

func newTestLogger() zerolog.Logger {
	return zerolog.Nop()
}
