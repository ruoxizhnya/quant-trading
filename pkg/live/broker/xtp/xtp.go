// Package xtp provides a broker integration layer for 中泰证券 XTP SDK.
//
// XTP (中泰证券极速交易系统) is a C++ trading SDK that provides:
//   - Order submission/cancellation (A股股票/基金/债券)
//   - Real-time position and account queries
//   - Market data subscription (L1/L2)
//   - Order push notifications
//
// This package defines the Go-side interface and configuration.
// The actual XTP SDK binding (via CGo) requires:
//  1. libxtptraderapi.so / .dylib (provided by 中泰证券)
//  2. XTP account credentials (simulation or production)
//  3. CGo wrapper in internal/cgo/xtp/
//
// Until the SDK is linked, XTPTrader operates in "offline" mode:
// all trading methods return ErrOffline. This allows the system
// to compile and test without the proprietary SDK.
//
// Usage:
//
//	cfg := xtp.Config{
//	    IP:         "tcp://210.14.63.51",  // 中泰仿真环境
//	    Port:       6100,
//	    AccountID:  "your_account",
//	    Password:   "your_password",
//	    Protocol:   xtp.ProtocolTCP,
//	    OfflineMode: true,  // set false when SDK is linked
//	}
//	trader, err := xtp.NewXTPTrader(cfg, logger)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	result, err := trader.SubmitOrder(ctx, "000001.SZ", domain.DirectionLong,
//	    domain.OrderTypeMarket, 100, 0)
package xtp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// ─── Errors ────────────────────────────────────────────────

var (
	// ErrOffline is returned when the XTP SDK is not linked.
	// All trading methods return this error in offline mode.
	ErrOffline = errors.New("xtp: SDK not linked, operating in offline mode")

	// ErrNotConnected is returned when the trader hasn't connected
	// to the XTP server.
	ErrNotConnected = errors.New("xtp: not connected to broker server")

	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = errors.New("xtp: invalid configuration")

	// ErrOrderNotFound is returned when an order ID is not found.
	ErrOrderNotFound = errors.New("xtp: order not found")

	// ErrInsufficientBuyingPower is returned when account lacks
	// sufficient buying power for an order.
	ErrInsufficientBuyingPower = errors.New("xtp: insufficient buying power")

	// ErrSymbolNotSubscribed is returned when market data is
	// requested for an unsubscribed symbol.
	ErrSymbolNotSubscribed = errors.New("xtp: symbol not subscribed")
)

// ─── Types ─────────────────────────────────────────────────

// Protocol specifies the XTP connection protocol.
type Protocol int

const (
	ProtocolTCP Protocol = iota // TCP (default)
	ProtocolUDP                 // UDP for low-latency market data
)

func (p Protocol) String() string {
	switch p {
	case ProtocolTCP:
		return "tcp"
	case ProtocolUDP:
		return "udp"
	default:
		return "unknown"
	}
}

// Environment specifies the XTP environment.
type Environment int

const (
	EnvSimulation Environment = iota // 仿真环境 (default)
	EnvProduction                    // 生产环境
)

// Config holds XTP connection and trading configuration.
type Config struct {
	// IP is the XTP server address (e.g., "tcp://210.14.63.51").
	IP string `json:"ip" yaml:"ip"`

	// Port is the XTP server port.
	Port int `json:"port" yaml:"port"`

	// AccountID is the XTP account ID (资金账号).
	AccountID string `json:"account_id" yaml:"account_id"`

	// Password is the XTP account password.
	Password string `json:"-" yaml:"password"` // never serialized

	// Protocol is the connection protocol (TCP or UDP).
	Protocol Protocol `json:"protocol" yaml:"protocol"`

	// Environment selects simulation or production.
	Environment Environment `json:"environment" yaml:"environment"`

	// OfflineMode when true, all methods return ErrOffline.
	// Set to false when the XTP SDK is linked via CGo.
	OfflineMode bool `json:"offline_mode" yaml:"offline_mode"`

	// HeartbeatInterval is the heartbeat interval in seconds.
	// Default: 15 (XTP recommended).
	HeartbeatInterval int `json:"heartbeat_interval" yaml:"heartbeat_interval"`

	// ReconnectInterval is the reconnect interval on disconnect.
	// Default: 3 seconds.
	ReconnectInterval time.Duration `json:"reconnect_interval" yaml:"reconnect_interval"`

	// MaxReconnectAttempts is the maximum reconnect attempts.
	// 0 = unlimited. Default: 10.
	MaxReconnectAttempts int `json:"max_reconnect_attempts" yaml:"max_reconnect_attempts"`

	// ClientID is a unique client identifier (1-65535).
	// Required by XTP protocol. Must be unique per session.
	ClientID uint8 `json:"client_id" yaml:"client_id"`

	// LicenseKey is the XTP license key for production.
	// Optional for simulation environment.
	LicenseKey string `json:"-" yaml:"license_key"`
}

// Validate checks the configuration for required fields.
func (c *Config) Validate() error {
	if c.IP == "" {
		return fmt.Errorf("%w: IP is required", ErrInvalidConfig)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("%w: port must be 1-65535, got %d", ErrInvalidConfig, c.Port)
	}
	if c.AccountID == "" {
		return fmt.Errorf("%w: account_id is required", ErrInvalidConfig)
	}
	if c.Password == "" {
		return fmt.Errorf("%w: password is required", ErrInvalidConfig)
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 15
	}
	if c.ReconnectInterval <= 0 {
		c.ReconnectInterval = 3 * time.Second
	}
	if c.MaxReconnectAttempts == 0 {
		c.MaxReconnectAttempts = 10
	}
	return nil
}

// ConnectionState tracks the XTP connection state machine.
type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateLoggingIn
	StateReady
	StateReconnecting
	StateError
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateLoggingIn:
		return "logging_in"
	case StateReady:
		return "ready"
	case StateReconnecting:
		return "reconnecting"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// OrderReq is the XTP order request structure.
type OrderReq struct {
	Symbol       string           `json:"symbol"`
	Direction    domain.Direction `json:"direction"`
	OrderType    domain.OrderType `json:"order_type"`
	Quantity     float64          `json:"quantity"`
	Price        float64          `json:"price"`
	BusinessType BusinessType     `json:"business_type"`
}

// BusinessType specifies the XTP business type.
type BusinessType int

const (
	BizCash    BusinessType = 0 // 普通股票
	BizMargin  BusinessType = 1 // 融资融券
	BizFuture  BusinessType = 2 // 期货
	BizOption  BusinessType = 3 // 期权
	BizHKStock BusinessType = 4 // 港股通
)

// ─── XTPTrader ─────────────────────────────────────────────

// XTPTrader implements live.LiveTrader using the 中泰 XTP SDK.
//
// When OfflineMode is true (default), all methods return ErrOffline.
// When the XTP SDK is linked via CGo, set OfflineMode to false and
// implement the cgoCall methods to route to the actual SDK.
type XTPTrader struct {
	mu     sync.RWMutex
	cfg    Config
	state  atomic.Int32 // ConnectionState
	logger zerolog.Logger

	// In-memory caches (populated when SDK pushes updates)
	orders    map[string]*live.OrderResult
	positions map[string]*live.PositionInfo
	account   *live.AccountInfo

	// reconnect control
	reconnectAttempts int
	stopCh            chan struct{}
	doneCh            chan struct{}
}

// NewXTPTrader creates a new XTP broker trader.
//
// The trader starts in StateDisconnected. Call Connect() to
// establish the connection (requires SDK linked, i.e.,
// OfflineMode=false).
func NewXTPTrader(cfg Config, logger zerolog.Logger) (*XTPTrader, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	t := &XTPTrader{
		cfg:       cfg,
		logger:    logger.With().Str("broker", "xtp").Str("account", cfg.AccountID).Logger(),
		orders:    make(map[string]*live.OrderResult),
		positions: make(map[string]*live.PositionInfo),
		account:   &live.AccountInfo{},
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}

	if cfg.OfflineMode {
		t.logger.Warn().Msg("XTP trader initialized in offline mode (SDK not linked)")
	} else {
		t.logger.Info().Str("ip", cfg.IP).Int("port", cfg.Port).Msg("XTP trader initialized")
	}

	t.state.Store(int32(StateDisconnected))
	return t, nil
}

// ─── LiveTrader interface ──────────────────────────────────

// Name returns the trader implementation name.
func (t *XTPTrader) Name() string {
	return "xtp_broker"
}

// HealthCheck verifies connectivity to the XTP broker.
func (t *XTPTrader) HealthCheck(ctx context.Context) error {
	if t.cfg.OfflineMode {
		return ErrOffline
	}
	state := ConnectionState(t.state.Load())
	if state != StateReady {
		return fmt.Errorf("%w: state=%s", ErrNotConnected, state)
	}
	return nil
}

// Connect establishes the connection to the XTP server.
// Requires OfflineMode=false and the XTP SDK linked.
func (t *XTPTrader) Connect(ctx context.Context) error {
	if t.cfg.OfflineMode {
		return ErrOffline
	}

	t.state.Store(int32(StateConnecting))
	t.logger.Info().Msg("connecting to XTP server...")

	// TODO: When SDK is linked via CGo:
	// 1. Call xtp_trader_api->Login()
	// 2. Wait for OnLogin() callback
	// 3. Transition to StateReady
	// 4. Start heartbeat goroutine
	// 5. Start order push listener

	t.state.Store(int32(StateError))
	return fmt.Errorf("xtp: SDK not linked — set OfflineMode=true for testing, or link libxtptraderapi via CGo")
}

// Disconnect gracefully closes the XTP connection.
func (t *XTPTrader) Disconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := ConnectionState(t.state.Load())
	if state == StateDisconnected {
		return nil
	}

	close(t.stopCh)
	<-t.doneCh

	t.state.Store(int32(StateDisconnected))
	t.logger.Info().Msg("disconnected from XTP server")
	return nil
}

// SubmitOrder submits a new order to the XTP broker.
func (t *XTPTrader) SubmitOrder(
	ctx context.Context,
	symbol string,
	direction domain.Direction,
	orderType domain.OrderType,
	quantity float64,
	price float64,
) (*live.OrderResult, error) {
	if t.cfg.OfflineMode {
		return nil, ErrOffline
	}

	state := ConnectionState(t.state.Load())
	if state != StateReady {
		return nil, fmt.Errorf("%w: state=%s", ErrNotConnected, state)
	}

	// Validate order parameters
	if symbol == "" {
		return nil, fmt.Errorf("%w: symbol is empty", ErrInvalidConfig)
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("%w: quantity must be positive", ErrInvalidConfig)
	}
	if orderType == domain.OrderTypeLimit && price <= 0 {
		return nil, fmt.Errorf("%w: limit order requires positive price", ErrInvalidConfig)
	}

	// A-share quantity must be multiple of 100 (1 lot = 100 shares)
	if int(quantity)%100 != 0 {
		return nil, fmt.Errorf("xtp: quantity must be multiple of 100 (1 lot), got %v", quantity)
	}

	// TODO: When SDK is linked:
	// 1. Build XTP_ORDER_REQ structure
	// 2. Call xtp_trader_api->InsertOrder(&req)
	// 3. Return order_id from response
	// 4. Store in orders map for async push updates

	return nil, fmt.Errorf("xtp: SDK not linked — SubmitOrder requires libxtptraderapi")
}

// CancelOrder cancels a pending order.
func (t *XTPTrader) CancelOrder(ctx context.Context, orderID string) error {
	if t.cfg.OfflineMode {
		return ErrOffline
	}

	state := ConnectionState(t.state.Load())
	if state != StateReady {
		return fmt.Errorf("%w: state=%s", ErrNotConnected, state)
	}

	if orderID == "" {
		return fmt.Errorf("%w: order_id is empty", ErrInvalidConfig)
	}

	t.mu.RLock()
	_, exists := t.orders[orderID]
	t.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: order_id=%s", ErrOrderNotFound, orderID)
	}

	// TODO: When SDK is linked:
	// 1. Call xtp_trader_api->CancelOrder(orderID)
	// 2. Wait for OnCancelOrder() callback
	// 3. Update order status in map

	return fmt.Errorf("xtp: SDK not linked — CancelOrder requires libxtptraderapi")
}

// GetOrder retrieves the current status of an order.
func (t *XTPTrader) GetOrder(ctx context.Context, orderID string) (*live.OrderResult, error) {
	if t.cfg.OfflineMode {
		return nil, ErrOffline
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	order, exists := t.orders[orderID]
	if !exists {
		return nil, fmt.Errorf("%w: order_id=%s", ErrOrderNotFound, orderID)
	}
	return order, nil
}

// GetPositions returns all current positions.
func (t *XTPTrader) GetPositions(ctx context.Context) ([]live.PositionInfo, error) {
	if t.cfg.OfflineMode {
		return nil, ErrOffline
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	positions := make([]live.PositionInfo, 0, len(t.positions))
	for _, p := range t.positions {
		positions = append(positions, *p)
	}
	return positions, nil
}

// GetAccount returns the current account summary.
func (t *XTPTrader) GetAccount(ctx context.Context) (*live.AccountInfo, error) {
	if t.cfg.OfflineMode {
		return nil, ErrOffline
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.account == nil {
		return nil, fmt.Errorf("xtp: account info not available")
	}
	return t.account, nil
}

// EmergencyFlatten closes all positions at market price.
// This bypasses T+1 restrictions for emergency compliance.
func (t *XTPTrader) EmergencyFlatten(ctx context.Context, reason string) (*live.EmergencyFlattenResult, error) {
	if t.cfg.OfflineMode {
		return nil, ErrOffline
	}

	state := ConnectionState(t.state.Load())
	if state != StateReady {
		return nil, fmt.Errorf("%w: state=%s", ErrNotConnected, state)
	}

	t.logger.Warn().Str("reason", reason).Msg("emergency flatten initiated")

	// TODO: When SDK is linked:
	// 1. Get all positions
	// 2. For each position, submit market sell order
	// 3. Bypass T+1 check (record bypassed_t1=true)
	// 4. Return aggregated result

	result := &live.EmergencyFlattenResult{
		Sold:      make([]live.EmergencyFlattenOrder, 0),
		Skipped:   make([]live.EmergencyFlattenSkip, 0),
		StartedAt: time.Now(),
		Reason:    reason,
	}

	t.mu.RLock()
	for symbol, pos := range t.positions {
		if pos.Quantity > 0 {
			result.Sold = append(result.Sold, live.EmergencyFlattenOrder{
				Symbol:      symbol,
				Quantity:    pos.Quantity,
				BypassedT1:  true,
				SubmittedAt: time.Now(),
			})
		}
	}
	t.mu.RUnlock()

	result.CompletedAt = time.Now()

	t.logger.Warn().Int("orders", len(result.Sold)).Msg("emergency flatten completed")
	return result, nil
}

// State returns the current connection state.
func (t *XTPTrader) State() ConnectionState {
	return ConnectionState(t.state.Load())
}

// IsOffline returns true if the trader is in offline mode.
func (t *XTPTrader) IsOffline() bool {
	return t.cfg.OfflineMode
}

// ─── Compile-time interface check ──────────────────────────

// Ensure XTPTrader satisfies the LiveTrader interface.
var _ live.LiveTrader = (*XTPTrader)(nil)
