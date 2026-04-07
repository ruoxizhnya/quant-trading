package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestStrategy_InterfaceCompliance(t *testing.T) {
	s := &mockStrategy{
		name:        "test_strategy",
		description: "Test strategy for unit testing",
		params: []Parameter{
			{Name: "period", Type: "int", Default: 20, Description: "Lookback period", Min: 5, Max: 100},
			{Name: "threshold", Type: "float", Default: 0.02, Description: "Signal threshold", Min: 0.01, Max: 0.10},
		},
	}

	assert.Equal(t, "test_strategy", s.Name())
	assert.NotEmpty(t, s.Description())
	assert.NotNil(t, s.Parameters())
	assert.Len(t, s.Parameters(), 2)
}

func TestStrategy_Configure_NoError(t *testing.T) {
	s := &mockStrategy{name: "config_test"}

	err := s.Configure(map[string]interface{}{
		"period":    30,
		"threshold": 0.05,
	})
	require.NoError(t, err)
}

func TestStrategy_GenerateSignals_EmptyBars(t *testing.T) {
	s := &mockStrategy{
		name:   "empty_test",
		signals: []Signal{},
	}

	ctx := context.Background()
	bars := map[string][]domain.OHLCV{}
	portfolio := &domain.Portfolio{TotalValue: 1000000}

	signals, err := s.GenerateSignals(ctx, bars, portfolio)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestStrategy_GenerateSignals_WithSignals(t *testing.T) {
	expectedSignals := []Signal{
		{Symbol: "600000.SH", Action: "buy", Strength: 0.8, Direction: domain.DirectionLong},
		{Symbol: "000001.SZ", Action: "sell", Strength: 0.9, Direction: domain.DirectionClose},
	}
	s := &mockStrategy{
		name:    "signal_test",
		signals: expectedSignals,
	}

	ctx := context.Background()
	bars := map[string][]domain.OHLCV{}
	portfolio := &domain.Portfolio{}

	signals, err := s.GenerateSignals(ctx, bars, portfolio)
	require.NoError(t, err)
	assert.Len(t, signals, 2)
	assert.Equal(t, "600000.SH", signals[0].Symbol)
	assert.Equal(t, domain.DirectionLong, signals[0].Direction)
}

func TestStrategy_GenerateSignals_Error(t *testing.T) {
	s := &mockStrategy{
		name:       "error_test",
		signalsErr: assert.AnError,
	}

	ctx := context.Background()
	bars := map[string][]domain.OHLCV{}
	portfolio := &domain.Portfolio{}

	_, err := s.GenerateSignals(ctx, bars, portfolio)
	assert.Error(t, err)
}

func TestStrategy_Weight_ReturnsFixed(t *testing.T) {
	s := &mockStrategy{name: "weight_test"}

	signal := Signal{Strength: 0.8}
	weight := s.Weight(signal, 1000000)
	assert.Equal(t, 0.1, weight)
}

func TestSignal_DirectionMapping(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		expected domain.Direction
	}{
		{"buy action", "buy", domain.DirectionLong},
		{"sell action", "sell", domain.DirectionClose},
		{"hold action", "hold", domain.DirectionHold},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signal := Signal{Action: tc.action, Direction: tc.expected}
			assert.Equal(t, tc.expected, signal.Direction)
		})
	}
}

func TestSignal_FactorsMap(t *testing.T) {
	signal := Signal{
		Symbol:   "600000.SH",
		Action:   "buy",
		Strength: 0.8,
		Factors: map[string]float64{
			"momentum": 0.6,
			"value":    0.3,
			"volume":   0.1,
		},
	}

	assert.Equal(t, "600000.SH", signal.Symbol)
	assert.Equal(t, 0.8, signal.Strength)
	assert.Len(t, signal.Factors, 3)
	assert.Equal(t, 0.6, signal.Factors["momentum"])
	assert.Equal(t, 0.3, signal.Factors["value"])
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	s := &mockStrategy{name: "registry_test"}
	err := reg.Register(s)
	require.NoError(t, err)

	retrieved, err := reg.Get("registry_test")
	require.NoError(t, err)
	assert.Equal(t, "registry_test", retrieved.Name())
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	reg := NewRegistry()

	s1 := &mockStrategy{name: "dup_test"}
	s2 := &mockStrategy{name: "dup_test"}

	err := reg.Register(s1)
	require.NoError(t, err)

	err = reg.Register(s2)
	assert.Error(t, err, "should error on duplicate registration")
}

func TestRegistry_GetNonExistent(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Get("non_existent")
	assert.Error(t, err)
}

func TestRegistry_ListStrategies(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(&mockStrategy{name: "strat_a"})
	_ = reg.Register(&mockStrategy{name: "strat_b"})
	_ = reg.Register(&mockStrategy{name: "strat_c"})

	list := reg.List()
	assert.Len(t, list, 3)
}

func TestCopilotService_NewService(t *testing.T) {
	svc := NewCopilotService()
	require.NotNil(t, svc)

	generated, buildable, backtested := svc.Stats()
	assert.Equal(t, int64(0), generated)
	assert.Equal(t, int64(0), buildable)
	assert.Equal(t, int64(0), backtested)
}

func TestCopilotService_IsConfigured(t *testing.T) {
	svc := NewCopilotService()
	assert.False(t, svc.IsConfigured())
}

func TestParameter_DefaultValues(t *testing.T) {
	params := []Parameter{
		{Name: "period", Type: "int", Default: 20, Description: "Lookback period"},
		{Name: "threshold", Type: "float", Default: 0.02, Description: "Threshold"},
		{Name: "enabled", Type: "bool", Default: true, Description: "Enable flag"},
		{Name: "mode", Type: "string", Default: "normal", Description: "Mode"},
	}

	assert.Equal(t, 20, params[0].Default)
	assert.Equal(t, 0.02, params[1].Default)
	assert.Equal(t, true, params[2].Default)
	assert.Equal(t, "normal", params[3].Default)
}

func TestParameter_RangeValidation(t *testing.T) {
	param := Parameter{
		Name:        "threshold",
		Type:        "float",
		Default:     0.02,
		Description: "Signal threshold",
		Min:         0.01,
		Max:         0.10,
	}

	assert.Equal(t, 0.01, param.Min)
	assert.Equal(t, 0.10, param.Max)
	assert.Equal(t, "float", param.Type)
}

func TestStrategy_SignalWithMetadata(t *testing.T) {
	now := time.Now()
	signal := Signal{
		Symbol:    "000001.SZ",
		Action:    "sell",
		Strength:  0.9,
		Price:     15.50,
		Date:      now,
		Direction: domain.DirectionClose,
		Metadata: map[string]interface{}{
			"source":     "test",
			"confidence": "high",
			"reasoning":  "RSI overbought > 80",
		},
	}

	assert.Equal(t, "000001.SZ", signal.Symbol)
	assert.Equal(t, "sell", signal.Action)
	assert.Equal(t, 15.50, signal.Price)
	assert.Equal(t, now, signal.Date)
	assert.Contains(t, signal.Metadata, "source")
	assert.Contains(t, signal.Metadata, "confidence")
	assert.Contains(t, signal.Metadata, "reasoning")
}

func TestStrategy_MultipleSignalsGeneration(t *testing.T) {
	s := &mockStrategy{
		name: "multi_signal",
		signals: []Signal{
			{Symbol: "600000.SH", Action: "buy", Strength: 0.8, Direction: domain.DirectionLong},
			{Symbol: "600001.SH", Action: "hold", Strength: 0.3, Direction: domain.DirectionHold},
			{Symbol: "000001.SZ", Action: "sell", Strength: 0.9, Direction: domain.DirectionClose},
		},
	}

	ctx := context.Background()
	bars := map[string][]domain.OHLCV{}
	portfolio := &domain.Portfolio{}

	signals, err := s.GenerateSignals(ctx, bars, portfolio)
	require.NoError(t, err)
	assert.Len(t, signals, 3)

	assert.Equal(t, "600000.SH", signals[0].Symbol)
	assert.Equal(t, domain.DirectionLong, signals[0].Direction)
	assert.Equal(t, "600001.SH", signals[1].Symbol)
	assert.Equal(t, domain.DirectionHold, signals[1].Direction)
	assert.Equal(t, "000001.SZ", signals[2].Symbol)
	assert.Equal(t, domain.DirectionClose, signals[2].Direction)
}

func TestStrategy_Cleanup_NoPanic(t *testing.T) {
	s := &mockStrategy{name: "cleanup_test"}

	assert.NotPanics(t, func() {
		s.Cleanup()
	})
}