package live

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// SimulatedDataFeed implements a simulated real-time data feed for testing
type SimulatedDataFeed struct {
	mu        sync.RWMutex
	subscribed map[string]bool
	quotes     map[string]Quote
	callback   func(Quote)
	running    bool
	stopCh     chan struct{}
}

// NewSimulatedDataFeed creates a new simulated data feed
func NewSimulatedDataFeed() *SimulatedDataFeed {
	return &SimulatedDataFeed{
		subscribed: make(map[string]bool),
		quotes:     make(map[string]Quote),
		stopCh:     make(chan struct{}),
	}
}

// Subscribe subscribes to symbols
func (df *SimulatedDataFeed) Subscribe(symbols []string) error {
	df.mu.Lock()
	defer df.mu.Unlock()

	for _, symbol := range symbols {
		df.subscribed[symbol] = true
		// Initialize with default quote if not exists
		if _, exists := df.quotes[symbol]; !exists {
			df.quotes[symbol] = Quote{
				Symbol:    symbol,
				Timestamp: time.Now(),
				Open:      100.0,
				High:      101.0,
				Low:       99.0,
				Close:     100.0,
				Volume:    1000,
				Bid:       99.9,
				Ask:       100.1,
			}
		}
	}

	if !df.running {
		df.running = true
		go df.generateQuotes()
	}

	return nil
}

// Unsubscribe unsubscribes from symbols
func (df *SimulatedDataFeed) Unsubscribe(symbols []string) error {
	df.mu.Lock()
	defer df.mu.Unlock()

	for _, symbol := range symbols {
		delete(df.subscribed, symbol)
	}

	return nil
}

// GetQuote returns the latest quote for a symbol
func (df *SimulatedDataFeed) GetQuote(symbol string) (Quote, error) {
	df.mu.RLock()
	defer df.mu.RUnlock()

	quote, exists := df.quotes[symbol]
	if !exists {
		return Quote{}, fmt.Errorf("no quote for symbol: %s", symbol)
	}

	return quote, nil
}

// SetCallback sets the callback function for new quotes
func (df *SimulatedDataFeed) SetCallback(callback func(Quote)) {
	df.mu.Lock()
	defer df.mu.Unlock()
	df.callback = callback
}

// SetQuote manually sets a quote for a symbol (for testing)
func (df *SimulatedDataFeed) SetQuote(quote Quote) {
	df.mu.Lock()
	defer df.mu.Unlock()
	df.quotes[quote.Symbol] = quote

	if df.callback != nil && df.subscribed[quote.Symbol] {
		go df.callback(quote)
	}
}

// Stop stops the data feed
func (df *SimulatedDataFeed) Stop() {
	df.mu.Lock()
	defer df.mu.Unlock()

	if df.running {
		close(df.stopCh)
		df.running = false
	}
}

func (df *SimulatedDataFeed) generateQuotes() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-df.stopCh:
			return
		case <-ticker.C:
			df.mu.RLock()
			symbols := make([]string, 0, len(df.subscribed))
			for symbol := range df.subscribed {
				symbols = append(symbols, symbol)
			}
			callback := df.callback
			df.mu.RUnlock()

			for _, symbol := range symbols {
				quote := df.simulatePriceMovement(symbol)
				if callback != nil {
					callback(quote)
				}
			}
		}
	}
}

func (df *SimulatedDataFeed) simulatePriceMovement(symbol string) Quote {
	df.mu.Lock()
	defer df.mu.Unlock()

	quote := df.quotes[symbol]
	
	// Simulate small random price movement
	change := (float64(time.Now().UnixNano()%100) - 50.0) / 1000.0
	quote.Close = math.Max(0.01, quote.Close+change)
	quote.High = math.Max(quote.High, quote.Close)
	quote.Low = math.Min(quote.Low, quote.Close)
	quote.Bid = quote.Close - 0.1
	quote.Ask = quote.Close + 0.1
	quote.Volume += int64(time.Now().UnixNano() % 100)
	quote.Timestamp = time.Now()

	df.quotes[symbol] = quote
	return quote
}
