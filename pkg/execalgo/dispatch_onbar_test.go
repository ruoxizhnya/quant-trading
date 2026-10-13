package execalgo

// K17-R2-R1：Dispatcher.OnBar 错误路径不得丢弃已取子单（TASKS.md
// K17-R2 ③ 要求 1）。
//
// 错误路径正常接线不可达（Attach 后 planState.onBar 不会报错），故用
// in-package 注入一个 OnBar 报错的算法 stub（Schedule 照常产计划）。
// 旧实现（遇错即 return out, err）在「错误父单排字典序在前」时会让
// 排在后面的正常父单从未被推进——已到点的子单凭空消失；本测试钉住
// 「已取子单照常交还调用方处置 + 错误聚合返回」两个判据。
import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onBarErrAlgo 是注入 OnBar 错误的测试替身：Schedule 正常、OnBar 恒错。
type onBarErrAlgo struct {
	plan []ChildOrder
	err  error
}

func (a *onBarErrAlgo) Name() string { return "onbar-err-stub" }
func (a *onBarErrAlgo) Schedule(_ context.Context, _ ParentOrder) ([]ChildOrder, error) {
	return a.plan, nil
}
func (a *onBarErrAlgo) OnBar(_ context.Context, _ domain.OHLCV) error { return a.err }
func (a *onBarErrAlgo) OnFill(_ context.Context, _ Fill) error        { return nil }
func (a *onBarErrAlgo) DueChildOrders() []ChildOrder                  { return nil }

// TestDispatcher_OnBarErrorReturnsTakenChildren 断言：遍历完全部父单后
// 才返回——错误父单（排序在前）不产出，其后正常父单的已取子单照常
// 交还，错误聚合 fail-loud。
func TestDispatcher_OnBarErrorReturnsTakenChildren(t *testing.T) {
	d := NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // 周一
	ctx := context.Background()

	// 父单 A（字典序在前）：算法运行态损坏，OnBar 报错。
	badPlan, err := NewTWAP(2).Schedule(ctx, ParentOrder{
		OrderID: "EA-20260105-A-long", Symbol: "000001.SZ", Side: domain.DirectionLong,
		Qty: 200, StartAt: start, EndAt: start.AddDate(0, 0, 2),
	})
	require.NoError(t, err)
	require.Len(t, badPlan, 2)
	boom := errors.New("boom: algo runtime corrupted")
	stub := &onBarErrAlgo{plan: badPlan, err: boom}
	d.parents["EA-20260105-A-long"] = &parentRun{
		spec: AttachSpec{
			ParentID: "EA-20260105-A-long", Symbol: "000001.SZ",
			Side: domain.DirectionLong, Qty: 200, OrderType: domain.OrderTypeMarket,
		},
		algo: stub,
		due:  stub,
		plan: badPlan,
	}

	// 父单 B（字典序在后）：真 TWAP，正常到点。
	_, err = d.Attach(ctx, AttachSpec{
		ParentID: "EA-20260105-B-long", Symbol: "000001.SZ", Side: domain.DirectionLong,
		Qty: 1000, Style: StyleTWAP, Slices: 2, OrderType: domain.OrderTypeMarket,
		StartAt: start, EndAt: start.AddDate(0, 0, 2),
	})
	require.NoError(t, err)

	// 第 1 根 bar：A 报错 + B 首片到点。
	due, oerr := d.OnBar(ctx, domain.OHLCV{Symbol: "000001.SZ", Date: start})
	require.Error(t, oerr, "错误必须聚合返回（fail-loud，不吞）")
	assert.ErrorIs(t, oerr, boom)
	assert.Contains(t, oerr.Error(), "EA-20260105-A-long", "错误点名出错父单")
	require.Len(t, due, 1, "破坏判据：B 的已取子单必须照常交还（旧实现遇错即返 ⇒ 空）")
	assert.Equal(t, "EA-20260105-B-long", due[0].ParentID)

	// 第 2 根 bar：A 仍报错但不中断，B 的第二片照常到点。
	due2, oerr2 := d.OnBar(ctx, domain.OHLCV{Symbol: "000001.SZ", Date: start.AddDate(0, 0, 1)})
	require.Error(t, oerr2)
	require.Len(t, due2, 1)
	assert.Equal(t, "EA-20260105-B-long-C0001", due2[0].OrderID, "B 的拆单节奏未被错误父单拖乱")

	// A 的账未被错误路径污染：filled=0、未虚增 submitted（无子单交出）。
	filled, total, ok := d.Progress("EA-20260105-A-long")
	require.True(t, ok)
	assert.Equal(t, 0.0, filled)
	assert.Equal(t, 200.0, total)
}
