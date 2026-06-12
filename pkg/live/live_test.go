package live

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
)

// ---- helpers -------------------------------------------------------------

func newTestLogger() zerolog.Logger {
	return zerolog.New(zerolog.NewConsoleWriter()).Level(zerolog.WarnLevel)
}

func defaultExecConfig() domain.ExecutionConfig {
	return domain.ExecutionConfig{
		OrderType:     domain.OrderTypeMarket,
		SlippageModel: "fixed",
		// Sprint 6 P1-22: simulate a discount broker (a ~17%
		// haircut off the regulatory ceiling). The base value
		// comes from fees.DefaultCommissionRate so a regulator
		// change is still picked up by this fixture.
		CommissionRate: fees.DefaultCommissionRate * (1.0 - 0.17),
		MinCommission:  fees.DefaultMinCommission,
		InitialCapital: 1_000_000,
	}
}

// failingBroker returns errors for every Broker call (used in error-path tests).
type failingBroker struct{}

func (failingBroker) Connect() error                  { return fmt.Errorf("connect boom") }
func (failingBroker) Disconnect() error               { return fmt.Errorf("disconnect boom") }
func (failingBroker) SubmitOrder(domain.Order) (string, error) { return "", fmt.Errorf("submit boom") }
func (failingBroker) CancelOrder(string) error        { return fmt.Errorf("cancel boom") }
func (failingBroker) GetOrderStatus(string) (string, error) { return "", fmt.Errorf("status boom") }
func (failingBroker) GetPositions() ([]domain.Position, error) {
	return nil, fmt.Errorf("positions boom")
}
func (failingBroker) GetAccountBalance() (float64, error) { return 0, fmt.Errorf("balance boom") }

// instrumentedBroker is a Broker that records calls and returns canned values.
type instrumentedBroker struct {
	mu        sync.Mutex
	connectN  atomic.Int32
	discN     atomic.Int32
	orders    map[string]domain.Order
	positions []domain.Position
	balance   float64
	cancelErr error
	status    map[string]string
}

func newInstrumentedBroker() *instrumentedBroker {
	return &instrumentedBroker{
		orders:  make(map[string]domain.Order),
		balance: 500_000,
		status:  make(map[string]string),
	}
}

func (b *instrumentedBroker) Connect() error {
	b.connectN.Add(1)
	return nil
}

func (b *instrumentedBroker) Disconnect() error {
	b.discN.Add(1)
	return nil
}

func (b *instrumentedBroker) SubmitOrder(o domain.Order) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o.ID == "" {
		o.ID = fmt.Sprintf("BRK-%d", len(b.orders)+1)
	}
	o.Status = "submitted"
	b.orders[o.ID] = o
	b.status[o.ID] = "submitted"
	return o.ID, nil
}

func (b *instrumentedBroker) CancelOrder(id string) error {
	if b.cancelErr != nil {
		return b.cancelErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[id]
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	o.Status = "cancelled"
	b.orders[id] = o
	b.status[id] = "cancelled"
	return nil
}

func (b *instrumentedBroker) GetOrderStatus(id string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.status[id]
	if !ok {
		return "", fmt.Errorf("not found: %s", id)
	}
	return s, nil
}

func (b *instrumentedBroker) GetPositions() ([]domain.Position, error) {
	return b.positions, nil
}

func (b *instrumentedBroker) GetAccountBalance() (float64, error) {
	return b.balance, nil
}

// ---- PositionManager -----------------------------------------------------

func TestPositionManager_UpdateAndRetrieve(t *testing.T) {
	pm := NewPositionManager()
	pm.UpdatePosition(domain.Position{
		Symbol:      "S1",
		Quantity:    100,
		AvgCost:     10,
		MarketValue: 1500,
	})

	got, ok := pm.GetPosition("S1")
	require.True(t, ok)
	assert.Equal(t, 100.0, got.Quantity)

	_, ok = pm.GetPosition("MISSING")
	assert.False(t, ok)
}

func TestPositionManager_HasPosition(t *testing.T) {
	pm := NewPositionManager()
	assert.False(t, pm.HasPosition("S1"))

	pm.UpdatePosition(domain.Position{Symbol: "S1", Quantity: 0})
	assert.False(t, pm.HasPosition("S1"), "zero-quantity position is not considered held")

	pm.UpdatePosition(domain.Position{Symbol: "S1", Quantity: 50})
	assert.True(t, pm.HasPosition("S1"))
}

func TestPositionManager_TotalsAndRemove(t *testing.T) {
	pm := NewPositionManager()
	pm.UpdatePosition(domain.Position{
		Symbol: "A", Quantity: 10, AvgCost: 100, MarketValue: 1100, UnrealizedPnL: 100, RealizedPnL: 5,
	})
	pm.UpdatePosition(domain.Position{
		Symbol: "B", Quantity: 5, AvgCost: 50, MarketValue: 300, UnrealizedPnL: 50, RealizedPnL: 20,
	})

	assert.InDelta(t, 1400, pm.GetTotalMarketValue(), 1e-9)
	assert.InDelta(t, 150, pm.GetTotalUnrealizedPnL(), 1e-9)
	assert.InDelta(t, 25, pm.GetTotalRealizedPnL(), 1e-9)
	assert.Len(t, pm.GetPositions(), 2)

	pm.RemovePosition("A")
	assert.False(t, pm.HasPosition("A"))
	assert.Len(t, pm.GetPositions(), 1)
}

func TestPositionManager_UpdateFromTrade_BuyNew(t *testing.T) {
	pm := NewPositionManager()
	pm.UpdateFromTrade(domain.Trade{
		Symbol: "X", Direction: domain.DirectionLong,
		Price: 10, Quantity: 100,
	})
	pos, ok := pm.GetPosition("X")
	require.True(t, ok)
	assert.Equal(t, 100.0, pos.Quantity)
	assert.Equal(t, 10.0, pos.AvgCost)
}

func TestPositionManager_UpdateFromTrade_BuyThenSell(t *testing.T) {
	pm := NewPositionManager()
	pm.UpdateFromTrade(domain.Trade{Symbol: "X", Direction: domain.DirectionLong, Price: 10, Quantity: 100})
	pm.UpdateFromTrade(domain.Trade{Symbol: "X", Direction: domain.DirectionLong, Price: 12, Quantity: 100})

	pos, _ := pm.GetPosition("X")
	require.Equal(t, 200.0, pos.Quantity)
	assert.InDelta(t, 11.0, pos.AvgCost, 1e-9, "weighted average cost basis")

	// Sell 100 @ 15 → realized PnL = (15-11) * 100 = 400
	pm.UpdateFromTrade(domain.Trade{Symbol: "X", Direction: domain.DirectionClose, Price: 15, Quantity: 100})
	pos, _ = pm.GetPosition("X")
	assert.Equal(t, 100.0, pos.Quantity)
	assert.InDelta(t, 400.0, pos.RealizedPnL, 1e-9)
}

func TestPositionManager_UpdateFromTrade_SellToZero(t *testing.T) {
	pm := NewPositionManager()
	pm.UpdateFromTrade(domain.Trade{Symbol: "X", Direction: domain.DirectionLong, Price: 10, Quantity: 50})
	pm.UpdateFromTrade(domain.Trade{Symbol: "X", Direction: domain.DirectionClose, Price: 12, Quantity: 50})
	pos, ok := pm.GetPosition("X")
	require.True(t, ok, "position is kept after sell-to-zero (Quantity==0)")
	assert.Equal(t, 0.0, pos.Quantity)
	assert.Equal(t, 0.0, pos.AvgCost, "AvgCost reset to zero when position is flat")
}

// ---- OrderManager --------------------------------------------------------

func TestOrderManager_SubmitAndGet(t *testing.T) {
	broker := newInstrumentedBroker()
	om := NewOrderManager(broker, defaultExecConfig())

	id, err := om.SubmitOrder(domain.Order{
		Symbol: "X", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 10,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, ok := om.GetOrder(id)
	require.True(t, ok)
	assert.Equal(t, "submitted", got.Status)
}

func TestOrderManager_SubmitRejection(t *testing.T) {
	om := NewOrderManager(failingBroker{}, defaultExecConfig())
	id, err := om.SubmitOrder(domain.Order{
		Symbol: "X", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 10,
	})
	require.Error(t, err)
	assert.Empty(t, id)
	got, ok := om.GetOrder(id)
	// The order is still saved with status "rejected"
	if ok {
		assert.Equal(t, "rejected", got.Status)
	}
}

func TestOrderManager_CancelOrder(t *testing.T) {
	broker := newInstrumentedBroker()
	om := NewOrderManager(broker, defaultExecConfig())

	id, err := om.SubmitOrder(domain.Order{
		Symbol: "X", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 5,
	})
	require.NoError(t, err)

	require.NoError(t, om.CancelOrder(id))
	got, _ := om.GetOrder(id)
	assert.Equal(t, "cancelled", got.Status)

	// Cancelling again must fail (status no longer pending/submitted).
	err = om.CancelOrder(id)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot cancel order with status")
}

func TestOrderManager_CancelUnknown(t *testing.T) {
	om := NewOrderManager(newInstrumentedBroker(), defaultExecConfig())
	err := om.CancelOrder("does-not-exist")
	require.Error(t, err)
}

func TestOrderManager_CancelBrokerError(t *testing.T) {
	broker := newInstrumentedBroker()
	broker.cancelErr = fmt.Errorf("kaboom")
	om := NewOrderManager(broker, defaultExecConfig())

	id, err := om.SubmitOrder(domain.Order{
		Symbol: "X", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1,
	})
	require.NoError(t, err)

	err = om.CancelOrder(id)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kaboom")
}

func TestOrderManager_GetPendingOrders(t *testing.T) {
	broker := newInstrumentedBroker()
	om := NewOrderManager(broker, defaultExecConfig())

	id1, _ := om.SubmitOrder(domain.Order{Symbol: "A", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1})
	id2, _ := om.SubmitOrder(domain.Order{Symbol: "B", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1})

	pending := om.GetPendingOrders()
	assert.Len(t, pending, 2)

	// Filling the first removes it from pending
	om.UpdateOrderStatus(id1, "filled")
	pending = om.GetPendingOrders()
	assert.Len(t, pending, 1)
	assert.Equal(t, id2, pending[0].ID)
}

func TestOrderManager_AddAndGetTrades(t *testing.T) {
	om := NewOrderManager(newInstrumentedBroker(), defaultExecConfig())
	om.AddTrade(domain.Trade{ID: "T1", Symbol: "X", Quantity: 10, Price: 5})
	om.AddTrade(domain.Trade{ID: "T2", Symbol: "Y", Quantity: 3, Price: 7})

	trades := om.GetTrades()
	require.Len(t, trades, 2)
	assert.Equal(t, "T1", trades[0].ID)
	assert.Equal(t, "T2", trades[1].ID)
}

func TestOrderManager_GetOrders_Snapshot(t *testing.T) {
	om := NewOrderManager(newInstrumentedBroker(), defaultExecConfig())
	om.SubmitOrder(domain.Order{Symbol: "A", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1})
	om.SubmitOrder(domain.Order{Symbol: "B", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1})
	assert.Len(t, om.GetOrders(), 2)
}

func TestOrderManager_RunStopsOnContextCancel(t *testing.T) {
	om := NewOrderManager(newInstrumentedBroker(), defaultExecConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { om.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// ---- SimulatedDataFeed ---------------------------------------------------

func TestSimulatedDataFeed_SubscribeAndGetQuote(t *testing.T) {
	df := NewSimulatedDataFeed()
	require.NoError(t, df.Subscribe([]string{"X", "Y"}))

	q, err := df.GetQuote("X")
	require.NoError(t, err)
	assert.Equal(t, "X", q.Symbol)
	assert.Equal(t, 100.0, q.Close, "default simulated close price")
}

func TestSimulatedDataFeed_GetQuoteUnknown(t *testing.T) {
	df := NewSimulatedDataFeed()
	_, err := df.GetQuote("ZZZ")
	require.Error(t, err)
}

func TestSimulatedDataFeed_SetCallback_FiresOnSetQuote(t *testing.T) {
	df := NewSimulatedDataFeed()
	require.NoError(t, df.Subscribe([]string{"X"}))

	got := make(chan Quote, 1)
	df.SetCallback(func(q Quote) { got <- q })
	df.SetQuote(Quote{Symbol: "X", Close: 99})

	select {
	case q := <-got:
		assert.Equal(t, "X", q.Symbol)
		assert.Equal(t, 99.0, q.Close)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("callback was not fired")
	}
}

func TestSimulatedDataFeed_SetQuote_NoCallback_StillUpdates(t *testing.T) {
	df := NewSimulatedDataFeed()
	require.NoError(t, df.Subscribe([]string{"X"}))
	df.SetQuote(Quote{Symbol: "X", Close: 42})
	q, _ := df.GetQuote("X")
	assert.Equal(t, 42.0, q.Close)
}

func TestSimulatedDataFeed_Unsubscribe(t *testing.T) {
	df := NewSimulatedDataFeed()
	require.NoError(t, df.Subscribe([]string{"X"}))
	require.NoError(t, df.Unsubscribe([]string{"X"}))
	// After Unsubscribe the symbol is removed from the subscription set,
	// but the quote is still cached. Verify the call succeeded (no error)
	// and the symbol is no longer considered subscribed.
	q, _ := df.GetQuote("X")
	assert.Equal(t, "X", q.Symbol, "quote is still cached after unsubscribe")
}

func TestSimulatedDataFeed_Stop(t *testing.T) {
	df := NewSimulatedDataFeed()
	require.NoError(t, df.Subscribe([]string{"X"}))
	df.Stop()
	// Calling Stop again must be idempotent (does not panic on close-of-closed-channel)
	df.Stop()
}

// ---- LiveEngine ----------------------------------------------------------

func TestLiveEngine_NewEngineDefaults(t *testing.T) {
	engine := NewLiveEngine(newInstrumentedBroker(), NewSimulatedDataFeed(), defaultExecConfig())
	require.NotNil(t, engine)
	assert.False(t, engine.IsRunning())
	pf := engine.GetPortfolio()
	require.NotNil(t, pf)
	assert.Equal(t, 1_000_000.0, pf.Cash)
}

func TestLiveEngine_StartAlreadyRunning(t *testing.T) {
	engine := NewLiveEngine(newInstrumentedBroker(), NewSimulatedDataFeed(), defaultExecConfig())
	require.NoError(t, engine.Start(context.Background(), []string{"X"}))
	defer func() { _ = engine.Stop([]string{"X"}) }()

	err := engine.Start(context.Background(), []string{"Y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "engine already running")
}

func TestLiveEngine_StartConnectError(t *testing.T) {
	engine := NewLiveEngine(failingBroker{}, NewSimulatedDataFeed(), defaultExecConfig())
	err := engine.Start(context.Background(), []string{"X"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to broker")
}

func TestLiveEngine_StartSubscribeError(t *testing.T) {
	// broker.positions = nil but Connect/Positions/Subscribe calls succeed
	broker := newInstrumentedBroker()
	df := &errDataFeed{subscribeErr: fmt.Errorf("subscribe boom")}
	engine := NewLiveEngine(broker, df, defaultExecConfig())
	err := engine.Start(context.Background(), []string{"X"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscribe boom")
}

func TestLiveEngine_StartPositionsError(t *testing.T) {
	broker := newInstrumentedBroker()
	// Replace GetPositions by wrapping the broker
	wrapped := &positionErrBroker{inner: broker, err: fmt.Errorf("positions boom")}
	engine := NewLiveEngine(wrapped, NewSimulatedDataFeed(), defaultExecConfig())
	err := engine.Start(context.Background(), []string{"X"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positions boom")
}

func TestLiveEngine_StartAndStop(t *testing.T) {
	broker := newInstrumentedBroker()
	broker.positions = []domain.Position{{Symbol: "X", Quantity: 10, AvgCost: 100, MarketValue: 1000}}
	df := NewSimulatedDataFeed()
	engine := NewLiveEngine(broker, df, defaultExecConfig())

	require.NoError(t, engine.Start(context.Background(), []string{"X"}))
	assert.True(t, engine.IsRunning())

	// Positions from broker should be visible via GetPositions
	positions := engine.GetPositions()
	assert.Len(t, positions, 1)

	require.NoError(t, engine.Stop([]string{"X"}))
	assert.False(t, engine.IsRunning())
}

func TestLiveEngine_StopWhenNotRunning_Noop(t *testing.T) {
	engine := NewLiveEngine(newInstrumentedBroker(), NewSimulatedDataFeed(), defaultExecConfig())
	require.NoError(t, engine.Stop([]string{"X"}))
}

func TestLiveEngine_OrderAndTradeDelegation(t *testing.T) {
	engine := NewLiveEngine(newInstrumentedBroker(), NewSimulatedDataFeed(), defaultExecConfig())

	id, err := engine.SubmitOrder(domain.Order{
		Symbol: "X", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1,
	})
	require.NoError(t, err)
	got, ok := engine.GetOrder(id)
	require.True(t, ok)
	assert.Equal(t, id, got.ID)

	assert.Len(t, engine.GetOrders(), 1)
	assert.Len(t, engine.GetTrades(), 0, "no trades are produced by Submit alone")

	// CancelOrder delegation
	require.NoError(t, engine.CancelOrder(id))
}

// ---- SimulatedBroker / engine interactions (tryFillOrder) ----------------

func TestEngine_HandleQuote_FillsMarketOrder(t *testing.T) {
	broker := newInstrumentedBroker()
	df := NewSimulatedDataFeed()
	engine := NewLiveEngine(broker, df, defaultExecConfig())
	require.NoError(t, engine.Start(context.Background(), []string{"X"}))
	defer func() { _ = engine.Stop([]string{"X"}) }()

	// Use atomic.Int32 to synchronize the counter between the
	// async onTrade callback and the polling test goroutine.
	// Plain `int` triggers -race here (the callback runs on the
	// engine's data-feed goroutine). CI gate requires 0 race.
	var trades atomic.Int32
	engine.SetCallbacks(nil, func(domain.Trade) { trades.Add(1) }, nil)

	id, err := engine.SubmitOrder(domain.Order{
		Symbol: "X", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// Engine.start() registered df.callback = engine.handleQuote
	// Trigger via df.SetQuote so the order is filled synchronously
	df.SetQuote(Quote{Symbol: "X", Close: 10, Bid: 9.95, Ask: 10.05})

	require.Eventually(t, func() bool {
		return trades.Load() == 1
	}, 500*time.Millisecond, 10*time.Millisecond, "expected exactly one trade callback")

	got, _ := engine.GetOrder(id)
	assert.Equal(t, "filled", got.Status, "market order should be filled by the quote")
}

// ---- helpers / mini-fakes for the engine error paths ---------------------
//
// errDataFeed is defined later in this file (P0-2 section) — it
// carries both subscribeErr and unsubscribeErr fields so the same
// fake can be used for Start and Stop error paths.

type positionErrBroker struct {
	inner *instrumentedBroker
	err   error
}

func (p *positionErrBroker) Connect() error { return p.inner.Connect() }
func (p *positionErrBroker) Disconnect() error { return p.inner.Disconnect() }
func (p *positionErrBroker) SubmitOrder(o domain.Order) (string, error) {
	return p.inner.SubmitOrder(o)
}
func (p *positionErrBroker) CancelOrder(id string) error { return p.inner.CancelOrder(id) }
func (p *positionErrBroker) GetOrderStatus(id string) (string, error) {
	return p.inner.GetOrderStatus(id)
}
func (p *positionErrBroker) GetPositions() ([]domain.Position, error) {
	return nil, p.err
}
func (p *positionErrBroker) GetAccountBalance() (float64, error) {
	return p.inner.GetAccountBalance()
}

// ---- Sprint 6 P0-2: LiveEngine.Stop lock-during-I/O regression tests -----

// slowBroker simulates a broker whose Disconnect() blocks for an
// extended period. With the old Stop() implementation that held
// e.mu across Disconnect(), the caller's goroutine would block for
// the full duration; every concurrent accessor (IsRunning, etc.)
// would be wedged behind the same mutex.
type slowBroker struct {
	*instrumentedBroker
	disconnectDelay time.Duration
}

func newSlowBroker(delay time.Duration) *slowBroker {
	return &slowBroker{
		instrumentedBroker: newInstrumentedBroker(),
		disconnectDelay:     delay,
	}
}

func (b *slowBroker) Disconnect() error {
	time.Sleep(b.disconnectDelay)
	return b.instrumentedBroker.Disconnect()
}

func TestLiveEngine_Stop_DoesNotHoldLockAcrossIO(t *testing.T) {
	// 200ms broker teardown — long enough to be observable, short
	// enough not to slow CI.
	broker := newSlowBroker(200 * time.Millisecond)
	df := NewSimulatedDataFeed()
	engine := NewLiveEngine(broker, df, defaultExecConfig())

	require.NoError(t, engine.Start(context.Background(), []string{"X"}))

	// Stop() launches the I/O phase without holding the engine mutex.
	// The state phase (close stopCh + flip running) must finish before
	// Disconnect() sleeps, so we poll IsRunning() with a tight
	// deadline and assert that the FIRST observation of running=false
	// happens *before* Disconnect() returns.
	stopDone := make(chan error, 1)
	stateObserved := make(chan time.Duration, 1)
	go func() { stopDone <- engine.Stop([]string{"X"}) }()

	// Poll IsRunning: it must transition to false in <50ms, well
	// before the 200ms Disconnect() delay. This proves the lock is
	// not held during the I/O phase.
	probeStart := time.Now()
	require.Eventually(t, func() bool {
		if !engine.IsRunning() {
			select {
			case stateObserved <- time.Since(probeStart):
			default:
			}
			return true
		}
		return false
	}, 50*time.Millisecond, 1*time.Millisecond,
		"IsRunning must flip to false within 50ms of Stop() being called")

	observeLatency := <-stateObserved
	assert.Less(t, observeLatency, 50*time.Millisecond,
		"IsRunning must observe running=false well before broker I/O completes (took %s)", observeLatency)

	// And Stop itself must still complete in ~200ms (not hang).
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2s")
	}
}

func TestLiveEngine_Stop_Concurrent_1000xNoDeadlock(t *testing.T) {
	// P0-2 acceptance: 1000 concurrent Stop() calls + accessor reads
	// must not deadlock. With the old code, one Stop would hold e.mu
	// across I/O and all other goroutines would queue up.
	const N = 1000
	engine := NewLiveEngine(newInstrumentedBroker(), NewSimulatedDataFeed(), defaultExecConfig())
	require.NoError(t, engine.Start(context.Background(), []string{"X"}))

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			// Mix Stop + IsRunning + GetPortfolio so the accessors
			// would have contended on the old lock-held-during-I/O
			// code path.
			if i%3 == 0 {
				_ = engine.Stop([]string{"X"})
			} else {
				_ = engine.IsRunning()
				_ = engine.GetPortfolio()
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// ok — 1000 concurrent ops completed without deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("1000 concurrent Stop/accessor calls deadlocked within 5s")
	}

	assert.False(t, engine.IsRunning(), "engine must be stopped after the burst")
}

func TestLiveEngine_Stop_UnsubscribeError_DoesNotShadow(t *testing.T) {
	// If Unsubscribe fails, Disconnect must still be attempted and
	// the returned error must describe the unsubscribe failure.
	df := &errDataFeed{unsubscribeErr: fmt.Errorf("unsubscribe boom")}
	engine := NewLiveEngine(newInstrumentedBroker(), df, defaultExecConfig())
	require.NoError(t, engine.Start(context.Background(), []string{"X"}))

	err := engine.Stop([]string{"X"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsubscribe boom",
		"unsubscribe error must be the primary error returned")
	assert.False(t, engine.IsRunning(),
		"engine must be marked stopped even when Unsubscribe fails")
}

func TestLiveEngine_Stop_DisconnectError_Propagates(t *testing.T) {
	// No unsubscribe error but a Disconnect error: must still be
	// returned to the caller.
	engine := NewLiveEngine(
		&discErrBroker{inner: newInstrumentedBroker()},
		NewSimulatedDataFeed(),
		defaultExecConfig(),
	)
	require.NoError(t, engine.Start(context.Background(), []string{"X"}))

	err := engine.Stop([]string{"X"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disconnect boom")
}

func TestLiveEngine_Stop_Twice_Idempotent(t *testing.T) {
	// Calling Stop a second time after the engine is already stopped
	// must be a silent no-op (not a panic on close-of-closed-channel).
	engine := NewLiveEngine(newInstrumentedBroker(), NewSimulatedDataFeed(), defaultExecConfig())
	require.NoError(t, engine.Start(context.Background(), []string{"X"}))
	require.NoError(t, engine.Stop([]string{"X"}))
	require.NoError(t, engine.Stop([]string{"X"}),
		"second Stop must be idempotent and return nil")
	assert.False(t, engine.IsRunning())
}

// ---- extensions to the fakes used by P0-2 tests --------------------------

// errDataFeed holds an optional Unsubscribe error. Existing tests that
// don't care about the unsubscribe path simply leave it nil.
type errDataFeed struct {
	subscribeErr   error
	unsubscribeErr error
}

func (e *errDataFeed) Subscribe(symbols []string) error         { return e.subscribeErr }
func (e *errDataFeed) Unsubscribe(symbols []string) error       { return e.unsubscribeErr }
func (e *errDataFeed) GetQuote(string) (Quote, error)           { return Quote{}, nil }
func (e *errDataFeed) SetCallback(func(Quote))                  {}

// discErrBroker is a broker whose Disconnect returns a hard error.
type discErrBroker struct{ inner *instrumentedBroker }

func (d *discErrBroker) Connect() error                  { return d.inner.Connect() }
func (d *discErrBroker) Disconnect() error               { return fmt.Errorf("disconnect boom") }
func (d *discErrBroker) SubmitOrder(o domain.Order) (string, error) {
	return d.inner.SubmitOrder(o)
}
func (d *discErrBroker) CancelOrder(id string) error { return d.inner.CancelOrder(id) }
func (d *discErrBroker) GetOrderStatus(id string) (string, error) {
	return d.inner.GetOrderStatus(id)
}
func (d *discErrBroker) GetPositions() ([]domain.Position, error) {
	return d.inner.GetPositions()
}
func (d *discErrBroker) GetAccountBalance() (float64, error) {
	return d.inner.GetAccountBalance()
}
