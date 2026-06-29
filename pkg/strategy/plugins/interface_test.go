package plugins

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

func TestMomentumStrategy_Interface(t *testing.T) {
	s := &momentumStrategy{
		BaseStrategy: strategy.NewBaseStrategy("momentum", "test momentum strategy"),
		params: MomentumConfig{
			LookbackDays:       20,
			TopN:               5,
			RebalanceFrequency: "weekly",
		},
	}

	t.Run("Name", func(t *testing.T) {
		if s.Name() != "momentum" {
			t.Errorf("expected name 'momentum', got '%s'", s.Name())
		}
	})

	t.Run("Description", func(t *testing.T) {
		if len(s.Description()) == 0 {
			t.Error("description should not be empty")
		}
	})

	t.Run("Parameters", func(t *testing.T) {
		params := s.Parameters()
		if len(params) == 0 {
			t.Error("should have parameters")
		}
	})

	t.Run("Configure", func(t *testing.T) {
		err := s.Configure(map[string]interface{}{
			"lookback_days": 30,
			"top_n":         10,
		})
		if err != nil {
			t.Errorf("Configure should not return error, got: %v", err)
		}
		if s.params.LookbackDays != 30 {
			t.Errorf("expected LookbackDays=30, got %d", s.params.LookbackDays)
		}
		if s.params.TopN != 10 {
			t.Errorf("expected TopN=10, got %d", s.params.TopN)
		}
	})

	t.Run("Weight", func(t *testing.T) {
		sig := strategy.Signal{
			Symbol:   "600000.SH",
			Action:   "buy",
			Strength: 0.8,
			Price:    10.0,
		}
		weight := s.Weight(sig, 1000000.0)
		if weight <= 0 || weight > 0.05 {
			t.Errorf("weight should be in (0, 0.05], got %.4f", weight)
		}
	})

	t.Run("Weight_CappedAtMax", func(t *testing.T) {
		sig := strategy.Signal{
			Symbol:   "600000.SH",
			Action:   "buy",
			Strength: 2.0,
			Price:    10.0,
		}
		weight := s.Weight(sig, 1000000.0)
		if weight > 0.05 {
			t.Errorf("weight should be capped at 0.05, got %.4f", weight)
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		s.Cleanup()
		if s.params.LookbackDays != 0 {
			t.Error("Cleanup should reset params")
		}
	})
}

func TestMeanReversionStrategy_Interface(t *testing.T) {
	s := &meanReversionStrategy{
		BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test mean reversion strategy"),
		params: MeanReversionConfig{
			BollingerPeriod: 20,
			BollingerStdDev: 2.0,
			RSIPeriod:       14,
			RSIOversold:     30,
			RSIOverbought:   70,
		},
	}

	t.Run("Name", func(t *testing.T) {
		if s.Name() != "mean_reversion" {
			t.Errorf("expected name 'mean_reversion', got '%s'", s.Name())
		}
	})

	t.Run("Configure", func(t *testing.T) {
		err := s.Configure(map[string]interface{}{
			"bollinger_period": 50,
			"rsi_oversold":     25.0,
		})
		if err != nil {
			t.Errorf("Configure should not return error, got: %v", err)
		}
		if s.params.BollingerPeriod != 50 {
			t.Errorf("expected BollingerPeriod=50, got %d", s.params.BollingerPeriod)
		}
	})

	t.Run("Weight", func(t *testing.T) {
		sig := strategy.Signal{Strength: 0.5}
		weight := s.Weight(sig, 500000.0)
		if weight <= 0 || weight > 0.05 {
			t.Errorf("weight should be in (0, 0.05], got %.4f", weight)
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		s.Cleanup()
		if s.params.BollingerPeriod != 0 {
			t.Error("Cleanup should reset params")
		}
	})
}

func TestValueScreeningStrategy_Interface(t *testing.T) {
	s := &valueScreeningStrategy{
		BaseStrategy: strategy.NewBaseStrategy("value_screening", "test value screening strategy"),
		params: ValueScreeningConfig{
			PEMax:              30.0,
			PBMax:              3.0,
			ROEMin:             0.1,
			MomentumDays:       60,
			TopN:               10,
			RebalanceFrequency: "monthly",
		},
	}

	t.Run("Name", func(t *testing.T) {
		if s.Name() != "value_screening" {
			t.Errorf("expected name 'value_screening', got '%s'", s.Name())
		}
	})

	t.Run("Configure", func(t *testing.T) {
		err := s.Configure(map[string]interface{}{
			"pe_max":  25.0,
			"roe_min": 0.15,
		})
		if err != nil {
			t.Errorf("Configure should not return error, got: %v", err)
		}
		if s.params.PEMax != 25.0 {
			t.Errorf("expected PEMax=25.0, got %.2f", s.params.PEMax)
		}
	})

	t.Run("Weight", func(t *testing.T) {
		sig := strategy.Signal{Strength: 0.7}
		weight := s.Weight(sig, 1000000.0)
		if weight <= 0 || weight > 0.05 {
			t.Errorf("weight should be in (0, 0.05], got %.4f", weight)
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		s.Cleanup()
		if s.httpClient != nil {
			t.Error("Cleanup should set httpClient to nil")
		}
	})
}

func TestMultiFactorStrategy_Interface(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "test multi factor strategy"),
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       60,
			TopN:               10,
			RebalanceFrequency: "weekly",
		},
	}

	t.Run("Name", func(t *testing.T) {
		if s.Name() != "multi_factor" {
			t.Errorf("expected name 'multi_factor', got '%s'", s.Name())
		}
	})

	t.Run("Configure", func(t *testing.T) {
		err := s.Configure(map[string]interface{}{
			"value_weight":    0.5,
			"quality_weight":  0.25,
			"momentum_weight": 0.25,
		})
		if err != nil {
			t.Errorf("Configure should not return error, got: %v", err)
		}
		if s.params.ValueWeight != 0.5 {
			t.Errorf("expected ValueWeight=0.5, got %.2f", s.params.ValueWeight)
		}
	})

	t.Run("Weight", func(t *testing.T) {
		sig := strategy.Signal{Strength: 0.9}
		weight := s.Weight(sig, 2000000.0)
		if weight <= 0 || weight > 0.05 {
			t.Errorf("weight should be in (0, 0.05], got %.4f", weight)
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		s.Cleanup()
		if s.httpClient != nil {
			t.Error("Cleanup should set httpClient to nil")
		}
	})
}

func TestStrategyInterface_Compliance(t *testing.T) {
	strategies := []strategy.Strategy{
		&momentumStrategy{BaseStrategy: strategy.NewBaseStrategy("momentum", "test")},
		&meanReversionStrategy{BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test")},
		&valueScreeningStrategy{BaseStrategy: strategy.NewBaseStrategy("value_screen", "test")},
		&multiFactorStrategy{BaseStrategy: strategy.NewBaseStrategy("multi_factor", "test")},
	}

	for i, s := range strategies {
		t.Run(fmt.Sprintf("strategy_%d_interface_check", i), func(t *testing.T) {
			_ = s.Name()
			_ = s.Description()
			_ = s.Parameters()

			err := s.Configure(map[string]interface{}{})
			if err != nil {
				t.Errorf("Configure returned error: %v", err)
			}

			ctx := context.Background()
			bars := map[string][]domain.OHLCV{}
			portfolio := &domain.Portfolio{}

			signals, err := s.GenerateSignals(ctx, bars, portfolio)
			if err != nil {
				t.Errorf("GenerateSignals returned error: %v", err)
			}
			if signals == nil {
				t.Log("GenerateSignals returned nil signals (acceptable for empty input)")
			}

			sig := strategy.Signal{Symbol: "TEST"}
			weight := s.Weight(sig, 1000000.0)
			if weight < 0 {
				t.Errorf("Weight should be non-negative, got %.4f", weight)
			}

			s.Cleanup()
		})
	}
}

func TestSignal_Structure(t *testing.T) {
	t.Run("NewSignal_Fields", func(t *testing.T) {
		now := time.Now()
		sig := strategy.Signal{
			Symbol:    "600000.SH",
			Action:    "buy",
			Strength:  0.85,
			Price:     12.5,
			Date:      now,
			Direction: domain.DirectionLong,
			Factors: map[string]float64{
				"momentum": 0.6,
				"value":    0.4,
			},
			Metadata: map[string]interface{}{
				"source": "momentum_strategy",
				"rank":   3,
			},
		}

		if sig.Symbol != "600000.SH" {
			t.Error("symbol mismatch")
		}
		if sig.Direction != domain.DirectionLong {
			t.Error("direction should be DirectionLong")
		}
		if sig.Factors["momentum"] != 0.6 {
			t.Error("factor value mismatch")
		}
		if sig.Metadata["rank"] != 3 {
			t.Error("metadata value mismatch")
		}
	})

	t.Run("Signal_DefaultValues", func(t *testing.T) {
		sig := strategy.Signal{}

		if sig.Action != "" {
			t.Error("default action should be empty")
		}
		if sig.Factors != nil {
			t.Error("default factors should be nil")
		}
		if sig.Metadata != nil {
			t.Error("default metadata should be nil")
		}
	})
}
