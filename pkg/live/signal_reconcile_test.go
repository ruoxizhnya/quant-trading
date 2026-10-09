package live

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkSignal(symbol string, day int, dir domain.Direction, strength float64) domain.Signal {
	return domain.Signal{
		Symbol:         symbol,
		Date:           time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC),
		Direction:      dir,
		Strength:       strength,
		CompositeScore: strength,
		OrderType:      domain.OrderTypeMarket,
	}
}

// TestReconcileSignals_Identical 是「信号一致」的正面证据：两侧序列一致时
// 零差异。
func TestReconcileSignals_Identical(t *testing.T) {
	paper := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
		mkSignal("600000.SH", 6, domain.DirectionClose, 0.5),
	}
	backtest := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
		mkSignal("600000.SH", 6, domain.DirectionClose, 0.5),
	}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	assert.Empty(t, diff, "两侧信号一致必须零差异")
}

// TestReconcileSignals_MissingBacktest 是「paper 多产出」的差异：缺一根
// 信号必须被点名。
func TestReconcileSignals_MissingBacktest(t *testing.T) {
	paper := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
		mkSignal("600000.SH", 5, domain.DirectionLong, 1.0),
	}
	backtest := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
	}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	require.Len(t, diff, 1)
	assert.Equal(t, KindSignalMissingBacktest, diff[0].Kind)
	assert.Equal(t, "600000.SH", diff[0].Symbol)
}

// TestReconcileSignals_MissingPaper 是「回测有 paper 无」的差异（纸面漏单）。
func TestReconcileSignals_MissingPaper(t *testing.T) {
	paper := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
	}
	backtest := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
		mkSignal("600000.SH", 5, domain.DirectionClose, 0.5),
	}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	require.Len(t, diff, 1)
	assert.Equal(t, KindSignalMissingPaper, diff[0].Kind)
	assert.Equal(t, "600000.SH", diff[0].Symbol)
}

// TestReconcileSignals_Direction 方向不一致必须被点名。
func TestReconcileSignals_Direction(t *testing.T) {
	paper := []domain.Signal{mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0)}
	backtest := []domain.Signal{mkSignal("000001.SZ", 5, domain.DirectionClose, 1.0)}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	require.Len(t, diff, 1)
	assert.Equal(t, KindSignalDirection, diff[0].Kind)
}

// TestReconcileSignals_Strength 强度超容差必须被点名。
func TestReconcileSignals_Strength(t *testing.T) {
	paper := []domain.Signal{mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0)}
	backtest := []domain.Signal{mkSignal("000001.SZ", 5, domain.DirectionLong, 2.0)}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	require.Len(t, diff, 1)
	assert.Equal(t, KindSignalStrength, diff[0].Kind)
}

// TestReconcileSignals_StrengthWithinTol 强度在容差内不算差异（浮点噪声）。
func TestReconcileSignals_StrengthWithinTol(t *testing.T) {
	paper := []domain.Signal{mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0)}
	backtest := []domain.Signal{mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0+1e-12)}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	assert.Empty(t, diff, "浮点噪声不该被读成差异")
}

// TestReconcileSignals_OrderIndependent 两侧内部顺序不同不该误报——同键内
// 排序对齐后应零差异。
func TestReconcileSignals_OrderIndependent(t *testing.T) {
	paper := []domain.Signal{
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
		mkSignal("600000.SH", 5, domain.DirectionLong, 0.5),
	}
	backtest := []domain.Signal{
		mkSignal("600000.SH", 5, domain.DirectionLong, 0.5),
		mkSignal("000001.SZ", 5, domain.DirectionLong, 1.0),
	}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	assert.Empty(t, diff, "两侧内部顺序不同不该误报")
}

// TestReconcileSignals_DeterministicOutput 输出顺序必须确定（按 date, symbol）。
func TestReconcileSignals_DeterministicOutput(t *testing.T) {
	paper := []domain.Signal{mkSignal("600000.SH", 5, domain.DirectionLong, 1.0)}
	backtest := []domain.Signal{
		mkSignal("000001.SZ", 3, domain.DirectionLong, 1.0),
		mkSignal("600000.SH", 5, domain.DirectionLong, 1.0),
	}
	d1 := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	d2 := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	require.Len(t, d1, 1)
	assert.Equal(t, d1, d2, "输出必须确定")
	assert.Equal(t, "000001.SZ", d1[0].Symbol, "缺失差异按 (date, symbol) 升序，最早的先出")
}

// TestReconcileSignals_TimeNormalization 证明「同一时刻、仅 time.Location 表达
// 不同」的信号必须对齐到同一键——不能因为 time.Time 的 loc 指针不同而误报
// missing。这是对 signalKey 用 time.Time 当 map 键的加固验证：time.FixedZone
// 每次返回**新指针**，与 time.UTC 指针不同，`==` 会失配。
func TestReconcileSignals_TimeNormalization(t *testing.T) {
	// 回测侧用 time.UTC；paper 侧用 time.FixedZone("UTC", 0) —— 同一时刻、
	// 同一 UTC 偏移，但 loc 是**不同的指针**。
	paper := []domain.Signal{{
		Symbol:    "000001.SZ",
		Date:      time.Date(2026, 1, 5, 0, 0, 0, 0, time.FixedZone("UTC", 0)),
		Direction: domain.DirectionLong,
		Strength:  1.0,
		OrderType: domain.OrderTypeMarket,
	}}
	backtest := []domain.Signal{{
		Symbol:    "000001.SZ",
		Date:      time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Direction: domain.DirectionLong,
		Strength:  1.0,
		OrderType: domain.OrderTypeMarket,
	}}
	diff := ReconcileSignals(paper, backtest, DefaultSignalReconcileConfig())
	assert.Empty(t, diff, "同一时刻（仅 loc 指针不同）必须对齐，不得误报 missing")
}
