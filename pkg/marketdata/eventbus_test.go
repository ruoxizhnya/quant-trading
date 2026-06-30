package marketdata

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// assertNotFired waits up to `timeout` for `done` to close; if it does, the
// test fails with `msg`. Used to assert that a handler is NOT called, replacing
// time.Sleep-based "assert no event received" patterns. The handler should
// close `done` (guarded by sync.Once to avoid double-close panics).
func assertNotFired(t *testing.T, done <-chan struct{}, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case <-done:
		t.Fatal(msg)
	case <-time.After(timeout):
	}
}

func TestEventBusSubscribeAndPublish(t *testing.T) {
	eb := NewEventBus(4)
	defer eb.Close()

	received := make([]DataEvent, 0)
	var mu sync.Mutex
	done := make(chan struct{})  // closes on 1st event
	fired := make(chan struct{}) // closes on 2nd event (must never happen after unsub)
	var once1, once2 sync.Once

	unsub := eb.Subscribe(EventTypeOHLCV, func(event DataEvent) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, event)
		switch len(received) {
		case 1:
			once1.Do(func() { close(done) })
		case 2:
			once2.Do(func() { close(fired) })
		}
	})

	eb.Publish(DataEvent{
		Type:    EventTypeOHLCV,
		Symbol:  "000001.SZ",
		Payload: "test-data",
		Source:  "test",
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first event")
	}

	mu.Lock()
	assert.Len(t, received, 1)
	assert.Equal(t, "000001.SZ", received[0].Symbol)
	assert.Equal(t, "test", received[0].Source)
	mu.Unlock()

	unsub()

	eb.Publish(DataEvent{
		Type:   EventTypeOHLCV,
		Symbol: "000002.SZ",
	})

	// After unsubscribe, the handler must NOT receive a second event. If it
	// does, `fired` closes and the test fails immediately.
	assertNotFired(t, fired, 50*time.Millisecond,
		"unsubscribed handler should not receive new events")
	mu.Lock()
	assert.Len(t, received, 1, "unsubscribed handler should not receive new events")
	mu.Unlock()
}

func TestEventBusPublishSync(t *testing.T) {
	eb := NewEventBus(4)
	defer eb.Close()

	received := false
	eb.Subscribe(EventTypeError, func(event DataEvent) {
		received = true
	})

	eb.PublishSync(DataEvent{
		Type:   EventTypeError,
		Symbol: "test",
	})

	assert.True(t, received, "PublishSync should deliver synchronously")
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	eb := NewEventBus(4)
	defer eb.Close()

	var mu sync.Mutex
	count1 := 0
	count2 := 0
	// Both subscribers must fire for all 5 events → total 10 callbacks.
	done := make(chan struct{})
	var once sync.Once

	eb.Subscribe(EventTypeFundamental, func(event DataEvent) {
		mu.Lock()
		count1++
		if count1+count2 == 10 {
			once.Do(func() { close(done) })
		}
		mu.Unlock()
	})
	eb.Subscribe(EventTypeFundamental, func(event DataEvent) {
		mu.Lock()
		count2++
		if count1+count2 == 10 {
			once.Do(func() { close(done) })
		}
		mu.Unlock()
	})

	for i := 0; i < 5; i++ {
		eb.Publish(DataEvent{Type: EventTypeFundamental})
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for all subscribers to receive events")
	}

	mu.Lock()
	assert.Equal(t, 5, count1)
	assert.Equal(t, 5, count2)
	mu.Unlock()
}

func TestEventBusEventTypeIsolation(t *testing.T) {
	eb := NewEventBus(4)
	defer eb.Close()

	var mu sync.Mutex
	ohlcvReceived := false
	errorReceived := false
	done := make(chan struct{})
	var once sync.Once

	eb.Subscribe(EventTypeOHLCV, func(event DataEvent) {
		mu.Lock()
		defer mu.Unlock()
		ohlcvReceived = true
	})
	eb.Subscribe(EventTypeError, func(event DataEvent) {
		mu.Lock()
		defer mu.Unlock()
		errorReceived = true
		once.Do(func() { close(done) })
	})

	eb.Publish(DataEvent{Type: EventTypeError})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Error event")
	}

	mu.Lock()
	assert.False(t, ohlcvReceived, "OHLCV subscriber should not receive Error events")
	assert.True(t, errorReceived)
	mu.Unlock()
}

func TestEventBusClose(t *testing.T) {
	eb := NewEventBus(2)

	fired := make(chan struct{})
	var once sync.Once
	eb.Subscribe(EventTypeOHLCV, func(event DataEvent) {
		once.Do(func() { close(fired) })
	})

	eb.Close()

	eb.Publish(DataEvent{Type: EventTypeOHLCV})

	// Events after Close are dropped synchronously in Publish (selects closeCh).
	// Wait briefly to confirm the handler does not fire.
	assertNotFired(t, fired, 30*time.Millisecond, "events after Close should be dropped")
}

func TestEventBusUnsubscribeAfterClose(t *testing.T) {
	eb := NewEventBus(2)
	eb.Close()

	fired := make(chan struct{})
	var once sync.Once
	unsub := eb.Subscribe(EventTypeOHLCV, func(event DataEvent) {
		once.Do(func() { close(fired) })
	})

	eb.Publish(DataEvent{Type: EventTypeOHLCV})

	assertNotFired(t, fired, 30*time.Millisecond, "handler should not be called after Close")
	unsub()
}

func TestEventBusHighThroughput(t *testing.T) {
	eb := NewEventBus(8)
	defer eb.Close()

	var mu sync.Mutex
	count := 0
	done := make(chan struct{})
	var once sync.Once

	eb.Subscribe(EventTypeOHLCV, func(event DataEvent) {
		mu.Lock()
		count++
		if count == 1000 {
			once.Do(func() { close(done) })
		}
		mu.Unlock()
	})

	for i := 0; i < 1000; i++ {
		eb.Publish(DataEvent{
			Type:   EventTypeOHLCV,
			Symbol: "000001.SZ",
		})
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for all 1000 events to be delivered")
	}

	mu.Lock()
	assert.Equal(t, 1000, count, "all events should be delivered")
	mu.Unlock()
}
