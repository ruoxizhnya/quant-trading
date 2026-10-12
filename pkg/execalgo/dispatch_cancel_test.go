// K18：execalgo.Dispatcher 窗口中途取消（外部仓位变动）单元测试。
//
// 覆盖：取消后剩余未报送子单不再产出（OnBar 静默）、已报送已成交部分
// 保留（Progress 不回滚）、被取消父单不进 Unfinished（不触发会话结束
// CANCELLED 告警）、Len/Symbols 不再计入、按 symbol 批量取消、幂等、
// 保守（不存在/已取消/已全部成交的父单不产生取消）、输出定序。
package execalgo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatcher_CancelDropsRemainingKeepsFilled 是 K18 的派发器层核心判据：
// 取消后剩余未报送子单不再产出，已成交进度保留不回滚，且不再进 Unfinished。
func TestDispatcher_CancelDropsRemainingKeepsFilled(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // 周一
	plan, err := d.Attach(context.Background(), dispatchParent("PO-C1", "000001.SZ", 1000, start))
	require.NoError(t, err)
	require.Len(t, plan, 7)

	// 前 3 天正常报送并成交（3 片）。
	filled := 0.0
	for i := 0; i < 3; i++ {
		due, err := d.OnBar(context.Background(), barOn("000001.SZ", start.AddDate(0, 0, i)))
		require.NoError(t, err)
		require.Len(t, due, 1)
		require.NoError(t, d.OnFill(execalgo.Fill{OrderID: due[0].OrderID, Qty: due[0].Qty}))
		filled += due[0].Qty
	}
	before, _, ok := d.Progress("PO-C1")
	require.True(t, ok)
	assert.InDelta(t, filled, before, 1e-9)

	// 外部仓位变动 → 取消。
	p, cancelled := d.Cancel("PO-C1", "stop_loss")
	require.True(t, cancelled, "活跃且有剩余片的父单应被取消")
	assert.Equal(t, "PO-C1", p.ParentID)
	assert.Equal(t, "stop_loss", p.Reason)
	assert.InDelta(t, filled, p.Filled, 1e-9, "取消快照保留已成交量（不回滚）")
	assert.InDelta(t, 1000.0, p.Total, 1e-9)

	// 后续交易日：不再产出任何子单。
	for i := 3; i < 7; i++ {
		due, err := d.OnBar(context.Background(), barOn("000001.SZ", start.AddDate(0, 0, i)))
		require.NoError(t, err)
		assert.Empty(t, due, "取消后第 %d 天不应再产出子单", i)
	}

	// 进度保留、但不再进会话结束裁决清单；Len/Symbols 归零。
	after, _, _ := d.Progress("PO-C1")
	assert.InDelta(t, filled, after, 1e-9, "已报送已成交部分保留（progress 不回滚）")
	assert.Empty(t, d.Unfinished(), "被取消父单不进 Unfinished（不触发会话结束 CANCELLED 告警）")
	assert.Equal(t, 0, d.Len(), "已取消父单不计入活跃数")
	assert.Empty(t, d.Symbols(), "已取消父单的 symbol 不再需要 bar")

	// 幂等：重复取消无副作用，返回 false。
	_, again := d.Cancel("PO-C1", "stop_loss")
	assert.False(t, again, "重复取消应返回 false（幂等）")

	// 不存在 / 已全部成交的父单：保守不取消。
	_, ok2 := d.Cancel("NOPE", "stop_loss")
	assert.False(t, ok2, "不存在父单不产生取消")

	// 全新父单跑满 → 取消应返回 false（无可丢弃剩余片）。
	start2 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	plan2, err := d.Attach(context.Background(), dispatchParent("PO-C2", "600000.SH", 700, start2))
	require.NoError(t, err)
	for i, c := range plan2 {
		_ = c
		due, err := d.OnBar(context.Background(), barOn("600000.SH", start2.AddDate(0, 0, i)))
		require.NoError(t, err)
		for _, dc := range due {
			require.NoError(t, d.OnFill(execalgo.Fill{OrderID: dc.OrderID, Qty: dc.Qty}))
		}
	}
	assert.Empty(t, d.Unfinished(), "全部成交 → 不在裁决清单")
	_, okFull := d.Cancel("PO-C2", "stop_loss")
	assert.False(t, okFull, "已全部成交的父单无可丢弃剩余片，不产生取消")
}

// TestDispatcher_CancelSymbolOnlyAffectsMatchingSymbol 断言按 symbol 批量
// 取消：只命中同 symbol 的活跃父单，其他 symbol 父单不受影响；输出按
// 父单号定序（确定性）。
func TestDispatcher_CancelSymbolOnlyAffectsMatchingSymbol(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	// 同 symbol 两个父单（long + close，模拟拆单 + 反向平仓单并发）。
	_, err := d.Attach(context.Background(), execalgo.AttachSpec{
		ParentID: "EA-20260105-000001.SZ-close", Symbol: "000001.SZ", Side: domain.DirectionClose,
		Qty: 500, Style: "twap", Slices: 3, OrderType: domain.OrderTypeMarket,
		StartAt: start, EndAt: start.AddDate(0, 0, 3),
	})
	require.NoError(t, err)
	_, err = d.Attach(context.Background(), execalgo.AttachSpec{
		ParentID: "EA-20260105-000001.SZ-long", Symbol: "000001.SZ", Side: domain.DirectionLong,
		Qty: 1000, Style: "twap", Slices: 3, OrderType: domain.OrderTypeMarket,
		StartAt: start, EndAt: start.AddDate(0, 0, 3),
	})
	require.NoError(t, err)
	_, err = d.Attach(context.Background(), dispatchParent("PO-OTHER", "600000.SH", 900, start))
	require.NoError(t, err)

	got := d.CancelSymbol("000001.SZ", "delisted")
	require.Len(t, got, 2, "同 symbol 的两个活跃父单都被取消")
	assert.Equal(t, []string{"EA-20260105-000001.SZ-close", "EA-20260105-000001.SZ-long"},
		[]string{got[0].ParentID, got[1].ParentID}, "输出按父单号定序")
	assert.Equal(t, "delisted", got[0].Reason)

	// 其他 symbol 父单不受影响。
	assert.Equal(t, []string{"600000.SH"}, d.Symbols())
	assert.Equal(t, 1, d.Len())
	_, _, ok := d.Progress("PO-OTHER")
	assert.True(t, ok)

	// 重复对同 symbol 取消 → 空切片（无活跃父单）。
	assert.Empty(t, d.CancelSymbol("000001.SZ", "delisted"))

	// 不存在的 symbol → 空切片。
	assert.Empty(t, d.CancelSymbol("300750.SZ", "stop_loss"))
}

// TestDispatcher_ActiveParentsSnapshot 断言 ActiveParents 只返回未取消父单，
// 且带 Side（paper 侧对账据此判敞口方向）。
func TestDispatcher_ActiveParentsSnapshot(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	_, err := d.Attach(context.Background(), dispatchParent("PO-A1", "000001.SZ", 1000, start))
	require.NoError(t, err)
	_, err = d.Attach(context.Background(), execalgo.AttachSpec{
		ParentID: "PO-A2", Symbol: "600000.SH", Side: domain.DirectionShort,
		Qty: 500, Style: "twap", Slices: 2, OrderType: domain.OrderTypeMarket,
		StartAt: start, EndAt: start.AddDate(0, 0, 2),
	})
	require.NoError(t, err)

	all := d.ActiveParents()
	require.Len(t, all, 2)
	assert.Equal(t, "PO-A1", all[0].ParentID)
	assert.Equal(t, domain.DirectionLong, all[0].Side)
	assert.Equal(t, "PO-A2", all[1].ParentID)
	assert.Equal(t, domain.DirectionShort, all[1].Side)
	assert.Empty(t, all[0].Reason, "未取消父单 Reason 为空")

	_, _ = d.Cancel("PO-A1", "stop_loss")
	left := d.ActiveParents()
	require.Len(t, left, 1)
	assert.Equal(t, "PO-A2", left[0].ParentID, "已取消父单不出现在 ActiveParents")
}
