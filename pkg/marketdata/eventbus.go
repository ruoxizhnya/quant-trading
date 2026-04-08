package marketdata

import (
	"sync"
)

type EventType string

const (
	EventTypeOHLCV        EventType = "ohlcv"
	EventTypeFundamental  EventType = "fundamental"
	EventTypeTradeCal     EventType = "trade_cal"
	EventTypeError        EventType = "error"
	EventTypeSourceSwitch EventType = "source_switch"
	EventTypeBatchDone    EventType = "batch_done"
)

type DataEvent struct {
	Type      EventType
	Symbol    string
	Timestamp int64
	Payload   interface{}
	Source    string
}

type EventHandler func(event DataEvent)

type subscription struct {
	handler EventHandler
	id      uint64
}

type EventBus struct {
	mu           sync.RWMutex
	handlers     map[EventType][]subscription
	nextID       uint64
	closed       bool
	closeCh      chan struct{}
	eventCh      chan DataEvent
	asyncWorkers int
}

func NewEventBus(asyncWorkers int) *EventBus {
	if asyncWorkers <= 0 {
		asyncWorkers = 4
	}
	eb := &EventBus{
		handlers:     make(map[EventType][]subscription),
		nextID:       1,
		closed:       false,
		closeCh:      make(chan struct{}),
		eventCh:      make(chan DataEvent, 4096),
		asyncWorkers: asyncWorkers,
	}
	for i := 0; i < eb.asyncWorkers; i++ {
		go eb.dispatchLoop()
	}
	return eb
}

func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) func() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return func() {}
	}

	id := eb.nextID
	eb.nextID++
	sub := subscription{handler: handler, id: id}
	eb.handlers[eventType] = append(eb.handlers[eventType], sub)

	unsub := func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		subs := eb.handlers[eventType]
		for i, s := range subs {
			if s.id == id {
				eb.handlers[eventType] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
	return unsub
}

func (eb *EventBus) Publish(event DataEvent) {
	select {
	case eb.eventCh <- event:
	case <-eb.closeCh:
	}
}

func (eb *EventBus) PublishSync(event DataEvent) {
	eb.mu.RLock()
	subs := eb.handlers[event.Type]
	handlersCopy := make([]EventHandler, 0, len(subs))
	for _, sub := range subs {
		handlersCopy = append(handlersCopy, sub.handler)
	}
	eb.mu.RUnlock()

	for _, h := range handlersCopy {
		h(event)
	}
}

func (eb *EventBus) dispatchLoop() {
	for {
		select {
		case event := <-eb.eventCh:
			eb.mu.RLock()
			isClosed := eb.closed
			subs := eb.handlers[event.Type]
			handlersCopy := make([]EventHandler, 0, len(subs))
			for _, sub := range subs {
				handlersCopy = append(handlersCopy, sub.handler)
			}
			eb.mu.RUnlock()

			if isClosed {
				return
			}

			for _, h := range handlersCopy {
				h(event)
			}
		case <-eb.closeCh:
			return
		}
	}
}

func (eb *EventBus) Close() {
	eb.mu.Lock()
	if eb.closed {
		eb.mu.Unlock()
		return
	}
	eb.closed = true
	eb.mu.Unlock()
	close(eb.closeCh)
}
