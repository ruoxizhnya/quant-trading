package marketdata

import (
	"fmt"
	"sync"

	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// RealtimeProvider provides real-time market data
type RealtimeProvider struct {
	mu          sync.RWMutex
	subscribers map[string][]chan live.Quote
	quotes      map[string]live.Quote
	dataFeed    DataFeedSource
}

// DataFeedSource interface for underlying data feed
type DataFeedSource interface {
	Subscribe(symbols []string) error
	Unsubscribe(symbols []string) error
	SetCallback(callback func(live.Quote))
}

// NewRealtimeProvider creates a new real-time data provider
func NewRealtimeProvider(dataFeed DataFeedSource) *RealtimeProvider {
	rp := &RealtimeProvider{
		subscribers: make(map[string][]chan live.Quote),
		quotes:      make(map[string]live.Quote),
		dataFeed:    dataFeed,
	}

	// Set up callback to receive quotes
	dataFeed.SetCallback(rp.handleQuote)

	return rp
}

// Subscribe subscribes to real-time quotes for symbols
func (rp *RealtimeProvider) Subscribe(symbols []string) (<-chan live.Quote, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Create a channel for this subscriber
	ch := make(chan live.Quote, 100)

	for _, symbol := range symbols {
		rp.subscribers[symbol] = append(rp.subscribers[symbol], ch)
	}

	// Subscribe to underlying data feed
	if err := rp.dataFeed.Subscribe(symbols); err != nil {
		return nil, fmt.Errorf("failed to subscribe to data feed: %w", err)
	}

	return ch, nil
}

// Unsubscribe unsubscribes from real-time quotes
func (rp *RealtimeProvider) Unsubscribe(symbols []string, ch <-chan live.Quote) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	for _, symbol := range symbols {
		subscribers := rp.subscribers[symbol]
		for i, sub := range subscribers {
			if sub == ch {
				rp.subscribers[symbol] = append(subscribers[:i], subscribers[i+1:]...)
				break
			}
		}
	}

	return rp.dataFeed.Unsubscribe(symbols)
}

// GetLatestQuote returns the latest quote for a symbol
func (rp *RealtimeProvider) GetLatestQuote(symbol string) (live.Quote, error) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	quote, exists := rp.quotes[symbol]
	if !exists {
		return live.Quote{}, fmt.Errorf("no quote available for symbol: %s", symbol)
	}

	return quote, nil
}

// GetAllQuotes returns all latest quotes
func (rp *RealtimeProvider) GetAllQuotes() map[string]live.Quote {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	result := make(map[string]live.Quote)
	for symbol, quote := range rp.quotes {
		result[symbol] = quote
	}
	return result
}

func (rp *RealtimeProvider) handleQuote(quote live.Quote) {
	rp.mu.Lock()
	rp.quotes[quote.Symbol] = quote
	subscribers := rp.subscribers[quote.Symbol]
	rp.mu.Unlock()

	// Broadcast to all subscribers
	for _, ch := range subscribers {
		select {
		case ch <- quote:
		default:
			// Channel full, skip this update
		}
	}
}

// Close closes the provider and all subscriber channels
func (rp *RealtimeProvider) Close() {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	for _, subscribers := range rp.subscribers {
		for _, ch := range subscribers {
			close(ch)
		}
	}
	rp.subscribers = make(map[string][]chan live.Quote)
}
