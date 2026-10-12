// K17：execalgo.Dispatcher（下单主路径接线的运行时）单元测试。
//
// 覆盖验收判据：多父单路由、OnBar 到点取单、OnFill 进度与**超额
// fail-loud**、Σ 子单量 == 父单量、重复挂载 fail-loud、未知算法
// fail-loud、输出定序（确定性）、Unfinished 会话结束裁决输入、
// ResolveExecWindow 片数/窗口口径。
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

// dispatchParent 构造一个 7 片 TWAP 父单的 AttachSpec（窗口 7 个自然日）。
func dispatchParent(parentID, symbol string, qty float64, start time.Time) execalgo.AttachSpec {
	n, end := execalgo.ResolveExecWindow(start, 7, nil)
	return execalgo.AttachSpec{
		ParentID: parentID, Symbol: symbol, Side: domain.DirectionLong,
		Qty: qty, Style: "twap", Slices: n, OrderType: domain.OrderTypeMarket,
		StartAt: start, EndAt: end,
	}
}

func barOn(symbol string, d time.Time) domain.OHLCV {
	return domain.OHLCV{Symbol: symbol, Date: d, Close: 10.0, Volume: 1_000_000}
}

// TestDispatcher_AttachOnBarFiresAllSlices 是 K17 e2e 判据的派发器层正证据：
// TWAP 7 片大单按 bar 到点取单 → 7 笔子单、Σ=父单、时间片严格等差、
// 子单号 (父单号, 片序号) 幂等纯函数。
func TestDispatcher_AttachOnBarFiresAllSlices(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // 周一
	plan, err := d.Attach(context.Background(), dispatchParent("PO-1", "000001.SZ", 1000, start))
	require.NoError(t, err)
	require.Len(t, plan, 7)

	var got []execalgo.DueChild
	for i := 0; i < 7; i++ {
		due, err := d.OnBar(context.Background(), barOn("000001.SZ", start.AddDate(0, 0, i)))
		require.NoError(t, err)
		got = append(got, due...)
	}

	require.Len(t, got, 7, "7 个交易日各到点 1 片（窗口步长恰为 1 自然日）")
	var sum float64
	for i, dc := range got {
		sum += dc.Qty
		assert.Equal(t, plan[i].OrderID, dc.OrderID, "第 %d 片子单号与计划一致", i)
		assert.Equal(t, "PO-1", dc.ParentID)
		assert.Equal(t, domain.OrderTypeMarket, dc.OrderType, "子单继承父单下单类型")
		assert.Positive(t, dc.Qty, "每片为正手")
	}
	assert.Equal(t, 1000.0, sum, "Σ 子单 Qty == 父单量")

	// 时间片均匀：相邻 SubmitAt 间隔全等（splitWindow 等差）。
	step := plan[1].SubmitAt.Sub(plan[0].SubmitAt)
	assert.Equal(t, 24*time.Hour, step, "窗口 7 自然日 / 7 片 ⇒ 步长 1 天")
	for i := 1; i < len(plan); i++ {
		assert.Equal(t, step, plan[i].SubmitAt.Sub(plan[i-1].SubmitAt), "第 %d 片间隔应等于 step", i)
	}
	assert.Equal(t, start, plan[0].SubmitAt, "首片落在窗口起点（信号当日）")

	// 到点即取走：第 8 天再喂 bar 不应重复产出。
	due, err := d.OnBar(context.Background(), barOn("000001.SZ", start.AddDate(0, 0, 7)))
	require.NoError(t, err)
	assert.Empty(t, due, "子单只报送一次（取走即清空）")
}

// TestDispatcher_OnFillProgressAndOverfillFailLoud 覆盖进度累计、完成态、
// 超额 fail-loud（K17 要求 3）与管辖外回报忽略。
func TestDispatcher_OnFillProgressAndOverfillFailLoud(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	plan, err := d.Attach(context.Background(), dispatchParent("PO-2", "000001.SZ", 1000, start))
	require.NoError(t, err)

	filled, total, ok := d.Progress("PO-2")
	require.True(t, ok)
	assert.Equal(t, 0.0, filled)
	assert.Equal(t, 1000.0, total)

	// 前 6 片成交（143×6=858），未完。
	for i := 0; i < 6; i++ {
		require.NoError(t, d.OnFill(execalgo.Fill{OrderID: plan[i].OrderID, Qty: plan[i].Qty}))
	}
	filled, _, _ = d.Progress("PO-2")
	assert.InDelta(t, 858.0, filled, 1e-9)
	assert.Len(t, d.Unfinished(), 1, "未报完 → 进入会话结束裁决清单")

	// 末片成交 → 完成，Unfinished 空。
	require.NoError(t, d.OnFill(execalgo.Fill{OrderID: plan[6].OrderID, Qty: plan[6].Qty}))
	filled, _, _ = d.Progress("PO-2")
	assert.InDelta(t, 1000.0, filled, 1e-9)
	assert.Empty(t, d.Unfinished())

	// 超额 fill（含重复投递同一子单）→ fail-loud error。
	err = d.OnFill(execalgo.Fill{OrderID: plan[0].OrderID, Qty: 1})
	require.Error(t, err, "超过父单量必须 fail-loud（算法侧钳制不报错，闸门在本层）")
	assert.Contains(t, err.Error(), "over-filled")

	// 管辖外子单号 → 忽略不报错。
	require.NoError(t, d.OnFill(execalgo.Fill{OrderID: "OTHER-999", Qty: 5}))
	filled, _, _ = d.Progress("PO-2")
	assert.InDelta(t, 1000.0, filled, 1e-9, "管辖外回报不改进度")

	// 不存在的父单查询 → ok=false。
	_, _, ok = d.Progress("NOPE")
	assert.False(t, ok)
}

// TestDispatcher_DuplicateAttachFailLoud 断言同一父单号重复挂载报错
// （一个算法实例只服务一个父单；静默覆盖会残留一份永不上报的旧计划）。
func TestDispatcher_DuplicateAttachFailLoud(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	spec := dispatchParent("PO-3", "000001.SZ", 1000, start)
	_, err := d.Attach(context.Background(), spec)
	require.NoError(t, err)
	_, err = d.Attach(context.Background(), spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

// TestDispatcher_InvalidAttachFailLoud 断言非法挂载输入一律 error。
func TestDispatcher_InvalidAttachFailLoud(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	t.Run("unknown style", func(t *testing.T) {
		spec := dispatchParent("PO-4", "000001.SZ", 1000, start)
		spec.Style = "iceberg"
		_, err := d.Attach(context.Background(), spec)
		assert.Error(t, err)
	})
	t.Run("empty parent id", func(t *testing.T) {
		spec := dispatchParent("", "000001.SZ", 1000, start)
		_, err := d.Attach(context.Background(), spec)
		assert.Error(t, err)
	})
	t.Run("qty<=0 (Schedule rejects)", func(t *testing.T) {
		spec := dispatchParent("PO-5", "000001.SZ", 0, start)
		_, err := d.Attach(context.Background(), spec)
		assert.Error(t, err)
	})
	t.Run("end<=start (Schedule rejects)", func(t *testing.T) {
		spec := dispatchParent("PO-6", "000001.SZ", 1000, start)
		spec.EndAt = spec.StartAt
		_, err := d.Attach(context.Background(), spec)
		assert.Error(t, err)
	})
}

// TestDispatcher_MultiParentRoutingBySymbol 断言多父单并发时：
// bar 只推进同 symbol 的父单；OnFill 按子单号路由到各自父单。
func TestDispatcher_MultiParentRoutingBySymbol(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	_, err := d.Attach(context.Background(), dispatchParent("PO-A", "000001.SZ", 700, start))
	require.NoError(t, err)
	_, err = d.Attach(context.Background(), dispatchParent("PO-B", "600000.SH", 1400, start))
	require.NoError(t, err)

	ctx := context.Background()
	// 只喂 A 的 bar：只有 A 的父单推进。
	due, err := d.OnBar(ctx, barOn("000001.SZ", start))
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, "PO-A", due[0].ParentID)

	// B 的进度仍为零。
	filled, _, _ := d.Progress("PO-B")
	assert.Equal(t, 0.0, filled)

	// 喂 B 的 bar：B 推进；A 不再产出（取走即清空 + 未到点）。
	due, err = d.OnBar(ctx, barOn("600000.SH", start))
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, "PO-B", due[0].ParentID)

	// OnFill 路由：A 的子单回报只进 A 的账。
	require.NoError(t, d.OnFill(execalgo.Fill{OrderID: "PO-A-C0000", Qty: 100}))
	fa, _, _ := d.Progress("PO-A")
	fb, _, _ := d.Progress("PO-B")
	assert.InDelta(t, 100.0, fa, 1e-9)
	assert.Equal(t, 0.0, fb)

	assert.Equal(t, []string{"000001.SZ", "600000.SH"}, d.Symbols(), "Symbols 去重且字典序")
	assert.Equal(t, 2, d.Len())
}

// TestDispatcher_DeterministicSequence 断言确定性（K17 验收 4）：
// 两个派发器吃同一输入，产出逐字节一致；且单个派发器内输出按父单号
// 定序（map 遍历序随机，不定序就会漂移）。
func TestDispatcher_DeterministicSequence(t *testing.T) {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	run := func() []string {
		d := execalgo.NewDispatcher()
		for _, sym := range []string{"600000.SH", "000001.SZ", "300750.SZ"} {
			_, err := d.Attach(context.Background(), dispatchParent("PO-"+sym, sym, 1000, start))
			require.NoError(t, err)
		}
		var ids []string
		for i := 0; i < 3; i++ {
			for _, sym := range []string{"300750.SZ", "000001.SZ", "600000.SH"} {
				due, err := d.OnBar(context.Background(), barOn(sym, start.AddDate(0, 0, i)))
				require.NoError(t, err)
				for _, dc := range due {
					ids = append(ids, dc.OrderID)
				}
			}
		}
		return ids
	}
	first := run()
	second := run()
	assert.Equal(t, first, second, "同一输入两次运行必须逐字节一致（禁时间戳派生 ID）")
	// 输出按喂 bar 的顺序（每次 OnBar 只返回该 symbol 的到点片；同一次
	// OnBar 内多父单同 symbol 时才按父单号定序——见下条用例的 Symbols）。
	assert.Equal(t, []string{
		"PO-300750.SZ-C0000", "PO-000001.SZ-C0000", "PO-600000.SH-C0000",
		"PO-300750.SZ-C0001", "PO-000001.SZ-C0001", "PO-600000.SH-C0001",
		"PO-300750.SZ-C0002", "PO-000001.SZ-C0002", "PO-600000.SH-C0002",
	}, first)
}

// TestDispatcher_VWAPProfileWeights 覆盖 VWAP 经派发器挂载：按 profile
// 加权分配，Σ 仍精确等于父单量。
func TestDispatcher_VWAPProfileWeights(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	n, end := execalgo.ResolveExecWindow(start, 0, []float64{1, 2, 3, 4})
	spec := execalgo.AttachSpec{
		ParentID: "PO-V", Symbol: "000001.SZ", Side: domain.DirectionLong,
		Qty: 1000, Style: "vwap", Slices: n, Profile: []float64{1, 2, 3, 4},
		OrderType: domain.OrderTypeMarket, StartAt: start, EndAt: end,
	}
	plan, err := d.Attach(context.Background(), spec)
	require.NoError(t, err)
	require.Len(t, plan, 4)

	var sum float64
	for _, c := range plan {
		sum += c.Qty
	}
	assert.Equal(t, 1000.0, sum)
	// 占比 0.1/0.2/0.3/0.4 → 100/200/300/400（余数并入末片）。
	assert.InDelta(t, 100.0, plan[0].Qty, 1e-9)
	assert.InDelta(t, 200.0, plan[1].Qty, 1e-9)
	assert.InDelta(t, 300.0, plan[2].Qty, 1e-9)
	assert.InDelta(t, 400.0, plan[3].Qty, 1e-9)
}

// TestDispatcher_SameSymbolParentsSorted 钉住防 map 漂移行为：同一 symbol
// 上多个父单（如 long + close 两个方向）在同一根 bar 到点时，输出按父单号
// 升序——map 遍历序随机，不过这层排序，同一输入两次运行序列会漂移。
func TestDispatcher_SameSymbolParentsSorted(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	// 挂载序故意与排序序相反（先挂 close 方向）。
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

	var ids []string
	for i := 0; i < 3; i++ { // 3 片 × 3 天，每根 bar 两个父单都到点 1 片
		due, err := d.OnBar(context.Background(), barOn("000001.SZ", start.AddDate(0, 0, i)))
		require.NoError(t, err)
		ids = ids[:0]
		for _, dc := range due {
			ids = append(ids, dc.ParentID)
		}
		assert.Equal(t, []string{"EA-20260105-000001.SZ-close", "EA-20260105-000001.SZ-long"}, ids,
			"同 symbol 多父单到点必须按父单号定序")
	}
}

// TestResolveExecWindow 断言片数/窗口口径：显式优先；否则 profile 长度；
// 再否则 1 片（等价整单仍走算法路径）。窗口恒为 n 个自然日（步长 1 日）。
func TestResolveExecWindow(t *testing.T) {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	n, end := execalgo.ResolveExecWindow(start, 7, nil)
	assert.Equal(t, 7, n)
	assert.Equal(t, start.AddDate(0, 0, 7), end)

	n, _ = execalgo.ResolveExecWindow(start, 0, []float64{1, 2, 3})
	assert.Equal(t, 3, n, "未指定片数 → 取 profile 长度")

	n, end = execalgo.ResolveExecWindow(start, 0, nil)
	assert.Equal(t, 1, n, "片数与 profile 均缺 → 1 片（等价整单）")
	assert.True(t, end.After(start), "窗口必须合法（EndAt > StartAt，Schedule 会校验）")
}
