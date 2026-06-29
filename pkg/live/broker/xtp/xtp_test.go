package xtp

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func validConfig() Config {
	return Config{
		IP:          "tcp://210.14.63.51",
		Port:        6100,
		AccountID:   "test_account",
		Password:    "test_password",
		OfflineMode: true,
		ClientID:    1,
	}
}

// ─── Config Validation ─────────────────────────────────────

func TestConfig_Validate_Success(t *testing.T) {
	cfg := validConfig()
	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, 15, cfg.HeartbeatInterval)
	assert.Equal(t, 3*1e9, float64(cfg.ReconnectInterval)) // 3s in ns
	assert.Equal(t, 10, cfg.MaxReconnectAttempts)
}

func TestConfig_Validate_MissingIP(t *testing.T) {
	cfg := validConfig()
	cfg.IP = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	tests := []int{0, -1, 70000, 100000}
	for _, port := range tests {
		cfg := validConfig()
		cfg.Port = port
		err := cfg.Validate()
		require.Error(t, err, "port %d should fail", port)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	}
}

func TestConfig_Validate_MissingAccountID(t *testing.T) {
	cfg := validConfig()
	cfg.AccountID = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestConfig_Validate_MissingPassword(t *testing.T) {
	cfg := validConfig()
	cfg.Password = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestConfig_Validate_DefaultsApplied(t *testing.T) {
	cfg := validConfig()
	cfg.HeartbeatInterval = 0
	cfg.ReconnectInterval = 0
	cfg.MaxReconnectAttempts = 0
	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, 15, cfg.HeartbeatInterval)
	assert.Equal(t, 10, cfg.MaxReconnectAttempts)
}

// ─── Protocol String ───────────────────────────────────────

func TestProtocol_String(t *testing.T) {
	assert.Equal(t, "tcp", ProtocolTCP.String())
	assert.Equal(t, "udp", ProtocolUDP.String())
	assert.Equal(t, "unknown", Protocol(99).String())
}

// ─── Environment ───────────────────────────────────────────

func TestEnvironment_Values(t *testing.T) {
	assert.Equal(t, EnvSimulation, Environment(0))
	assert.Equal(t, EnvProduction, Environment(1))
}

// ─── ConnectionState String ─────────────────────────────────

func TestConnectionState_String(t *testing.T) {
	states := []ConnectionState{
		StateDisconnected, StateConnecting, StateConnected,
		StateLoggingIn, StateReady, StateReconnecting, StateError,
	}
	expected := []string{
		"disconnected", "connecting", "connected",
		"logging_in", "ready", "reconnecting", "error",
	}
	for i, s := range states {
		assert.Equal(t, expected[i], s.String())
	}
	assert.Equal(t, "unknown", ConnectionState(99).String())
}

// ─── NewXTPTrader ──────────────────────────────────────────

func TestNewXTPTrader_Success(t *testing.T) {
	cfg := validConfig()
	trader, err := NewXTPTrader(cfg, zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, trader)
	assert.Equal(t, "xtp_broker", trader.Name())
	assert.True(t, trader.IsOffline())
	assert.Equal(t, StateDisconnected, trader.State())
}

func TestNewXTPTrader_InvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.IP = ""
	_, err := NewXTPTrader(cfg, zerolog.Nop())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

// ─── Offline Mode ─────────────────────────────────────────

func TestOffline_HealthCheck(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	err := trader.HealthCheck(context.Background())
	assert.ErrorIs(t, err, ErrOffline)
}

func TestOffline_SubmitOrder(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	_, err := trader.SubmitOrder(context.Background(), "000001.SZ",
		domain.DirectionLong, domain.OrderTypeMarket, 100, 0)
	assert.ErrorIs(t, err, ErrOffline)
}

func TestOffline_CancelOrder(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	err := trader.CancelOrder(context.Background(), "order-1")
	assert.ErrorIs(t, err, ErrOffline)
}

func TestOffline_GetOrder(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	_, err := trader.GetOrder(context.Background(), "order-1")
	assert.ErrorIs(t, err, ErrOffline)
}

func TestOffline_GetPositions(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	_, err := trader.GetPositions(context.Background())
	assert.ErrorIs(t, err, ErrOffline)
}

func TestOffline_GetAccount(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	_, err := trader.GetAccount(context.Background())
	assert.ErrorIs(t, err, ErrOffline)
}

func TestOffline_EmergencyFlatten(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	_, err := trader.EmergencyFlatten(context.Background(), "test")
	assert.ErrorIs(t, err, ErrOffline)
}

// ─── Connect (offline mode) ───────────────────────────────

func TestConnect_OfflineMode(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	err := trader.Connect(context.Background())
	assert.ErrorIs(t, err, ErrOffline)
}

// ─── Not Connected State ──────────────────────────────────

func TestNotConnected_HealthCheck(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	err := trader.HealthCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestNotConnected_SubmitOrder(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	_, err := trader.SubmitOrder(context.Background(), "000001.SZ",
		domain.DirectionLong, domain.OrderTypeMarket, 100, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestNotConnected_CancelOrder(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	err := trader.CancelOrder(context.Background(), "order-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestNotConnected_EmergencyFlatten(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	_, err := trader.EmergencyFlatten(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// ─── Connect (SDK not linked) ─────────────────────────────

func TestConnect_SDKNotLinked(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	err := trader.Connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SDK not linked")
	assert.Equal(t, StateError, trader.State())
}

// ─── SubmitOrder Validation ────────────────────────────────

func TestSubmitOrder_EmptySymbol(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	// Force state to Ready for validation testing
	trader.state.Store(int32(StateReady))
	_, err := trader.SubmitOrder(context.Background(), "",
		domain.DirectionLong, domain.OrderTypeMarket, 100, 0)
	require.Error(t, err)
}

func TestSubmitOrder_NegativeQuantity(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	trader.state.Store(int32(StateReady))
	_, err := trader.SubmitOrder(context.Background(), "000001.SZ",
		domain.DirectionLong, domain.OrderTypeMarket, -100, 0)
	require.Error(t, err)
}

func TestSubmitOrder_LimitNoPrice(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	trader.state.Store(int32(StateReady))
	_, err := trader.SubmitOrder(context.Background(), "000001.SZ",
		domain.DirectionLong, domain.OrderTypeLimit, 100, 0)
	require.Error(t, err)
}

func TestSubmitOrder_QuantityNotMultipleOf100(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	trader.state.Store(int32(StateReady))
	_, err := trader.SubmitOrder(context.Background(), "000001.SZ",
		domain.DirectionLong, domain.OrderTypeMarket, 150, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple of 100")
}

func TestSubmitOrder_SDKNotLinked(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	trader.state.Store(int32(StateReady))
	_, err := trader.SubmitOrder(context.Background(), "000001.SZ",
		domain.DirectionLong, domain.OrderTypeMarket, 100, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SDK not linked")
}

// ─── CancelOrder ───────────────────────────────────────────

func TestCancelOrder_EmptyID(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	trader.state.Store(int32(StateReady))
	err := trader.CancelOrder(context.Background(), "")
	require.Error(t, err)
}

func TestCancelOrder_NotFound(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	trader.state.Store(int32(StateReady))
	err := trader.CancelOrder(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

// ─── GetOrder ──────────────────────────────────────────────

func TestGetOrder_NotFound(t *testing.T) {
	cfg := validConfig()
	cfg.OfflineMode = false
	trader, _ := NewXTPTrader(cfg, zerolog.Nop())
	_, err := trader.GetOrder(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

// ─── Disconnect ────────────────────────────────────────────

func TestDisconnect_AlreadyDisconnected(t *testing.T) {
	trader, _ := NewXTPTrader(validConfig(), zerolog.Nop())
	err := trader.Disconnect()
	require.NoError(t, err)
	assert.Equal(t, StateDisconnected, trader.State())
}

// ─── BusinessType ──────────────────────────────────────────

func TestBusinessType_Values(t *testing.T) {
	assert.Equal(t, BizCash, BusinessType(0))
	assert.Equal(t, BizMargin, BusinessType(1))
	assert.Equal(t, BizFuture, BusinessType(2))
	assert.Equal(t, BizOption, BusinessType(3))
	assert.Equal(t, BizHKStock, BusinessType(4))
}

// ─── Interface Compliance ──────────────────────────────────

func TestXTPTrader_ImplementsLiveTrader(t *testing.T) {
	// The compile-time check is at package level:
	// var _ live.LiveTrader = (*XTPTrader)(nil)
	// If the package compiles, the interface is satisfied.
	// This test verifies runtime behavior.
	trader, err := NewXTPTrader(validConfig(), zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, trader)
	assert.Equal(t, "xtp_broker", trader.Name())
}
