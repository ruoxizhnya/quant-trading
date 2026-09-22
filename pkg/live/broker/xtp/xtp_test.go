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

// TestSubmitOrder_QuantityPerBoardRules is the AUD-21 guardrail at the
// broker boundary.
//
// The old check was a blanket `int(quantity)%100 != 0` — the MAIN-BOARD /
// ChiNext rule stated as if it were THE A-share rule. It rejected legal
// STAR (科创板, >=200 shares with 1-share increments) and BSE (北交所,
// >=100 with 1-share increments) orders, including ones
// risk.NormalizeOrderQuantity had just produced upstream. It also went
// through int(), so 100.9 shares passed while 250.0 was refused.
//
// "Accepted" is asserted by the order clearing the quantity gate and
// failing on the NEXT thing (the SDK not being linked) — never by
// expecting success, because SubmitOrder cannot succeed until the CGo
// binding lands. The `assert.NotContains` on the rejection path is what
// keeps the two directions distinguishable: a case that is supposed to be
// refused must be refused by the QUANTITY rule, not merely "somewhere".
func TestSubmitOrder_QuantityPerBoardRules(t *testing.T) {
	const clearedGate = "SDK not linked"

	cases := []struct {
		name    string
		symbol  string
		dir     domain.Direction
		qty     float64
		wantMsg string // substring of the rejection; "" = must clear the gate
	}{
		{"main board lot 100", "000001.SZ", domain.DirectionLong, 100, ""},
		{"main board odd 137", "000001.SZ", domain.DirectionLong, 137, "multiple of 100"},
		{"main board 100.9 rejected, not truncated to 100", "000001.SZ", domain.DirectionLong, 100.9, "whole number"},
		{"STAR floor 200", "688981.SH", domain.DirectionLong, 200, ""},
		{"STAR 250 (1-share increment)", "688981.SH", domain.DirectionLong, 250, ""},
		{"STAR 617 (the registry's case)", "688981.SH", domain.DirectionLong, 617, ""},
		{"STAR 150 (below the 200 floor)", "688981.SH", domain.DirectionLong, 150, "at least 200"},
		{"BSE 150 (1-share increment)", "830799.BJ", domain.DirectionLong, 150, ""},
		{"BSE 50 (below the 100 floor)", "830799.BJ", domain.DirectionLong, 50, "at least 100"},
		{"odd-lot SELL clears the gate", "000001.SZ", domain.DirectionClose, 137, ""},
		{"zero", "000001.SZ", domain.DirectionLong, 0, "must be positive"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.OfflineMode = false
			trader, err := NewXTPTrader(cfg, zerolog.Nop())
			require.NoError(t, err)
			trader.state.Store(int32(StateReady))

			_, err = trader.SubmitOrder(context.Background(), tc.symbol,
				tc.dir, domain.OrderTypeMarket, tc.qty, 0)
			require.Error(t, err, "an offline-mode submit must always error")

			if tc.wantMsg == "" {
				assert.Contains(t, err.Error(), clearedGate,
					"quantity %v on %s must clear the quantity gate and fail on the SDK; got %q",
					tc.qty, tc.symbol, err.Error())
				return
			}
			assert.Contains(t, err.Error(), tc.wantMsg)
			assert.NotContains(t, err.Error(), clearedGate,
				"this quantity must be refused by the quantity rule, before the SDK check")
		})
	}
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
