// strategy-runtime 检查点 / 断点续跑（K2 切片 3）。
//
// 本文件在 StreamRunner 之上加一层「检查点」能力：周期保存策略状态到
// quant.strategy_state、异常退出尽力保存、续跑时从断点恢复并**精确跳过**
// 已处理的 bar 前缀。落库后端是 StateStore（见 state_store.go），本文件只
// 依赖接口，不绑死 PG（测试可注入 fake）。
//
// ─── 为什么保存点必须对齐「日期边界」（本切片核心裁决） ─────────────
//
// 续跑用「跳过前缀 Date <= 存储 ts」实现。若在**同一日期块的中间**保存，
// 存储 ts 就是该日期块的某根 bar 的时间，续跑会把同日期块里**尚未处理**的
// 其余 bar 一并跳过——多 symbol 平铺流（[d1sA, d1sB, ...]）下这就是
// **静默丢 bar**：d1sA 处理后保存，续跑跳过 Date<=d1 的全部 d1sA/d1sB，
// d1sB 永远不跑。
//
// 因此保存点只落在日期边界：处理完第 i 根后，仅当
//   i+1 == len(bars)（最后一根）或 bars[i+1].Date > bars[i].Date（下一根进入新日期）
// 才是边界。边界对齐让 `Date <= 存储 ts` 的跳过规则**精确成立**：跳过的
// 前缀恰好是「已处理且已落库的那些 bar 所在的完整日期块」，一根不多一根不少。
//
// ─── 异常退出保存的必要性 ──────────────────────────────────────────
// 周期性保存 + 终局保存覆盖了正常路径；进程/策略在跑到一半崩掉时，若只剩
// 上一次周期保存的断点，中间那段确定性的重算就要重来（结果一致，但白跑）。
// 异常退出时**尽力**把当前断点写下，能把这段重算窗口缩到一个日期块。
// 唯一红线：异常退出若**不在日期边界**，绝不保存（见上）。
package strategy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// bestEffortSaveTimeout 是异常退出时保存断点的超时上限。单独成常量：异常
// 路径的 ctx 用 context.WithoutCancel(ctx) 重建，必须自带一个上限，否则
// 库若挂了，这个「尽力」保存会把进程拖住——尽力不等于无限等。
const bestEffortSaveTimeout = 10 * time.Second

// Checkpoint 是 RunCheckpointed 的检查点配置。
//
// 零值 Checkpoint{} 完全等价于 Run（Store==nil → 无保存、无恢复），这是
// Run 委托 RunCheckpointed 的实现基础（见 Run 注释）。
type Checkpoint struct {
	// Store 是落库后端。nil → RunCheckpointed 退化为 Run（无保存、无恢复）。
	Store StateStore
	// StrategyID / RunID 是 quant.strategy_state 的复合主键（策略 × run）。
	StrategyID string
	RunID      string
	// Every：>0 时，每攒够 Every 根已处理 bar **且到达日期边界**才保存一次
	//（保存后计数清零）；<=0 时只在终局保存。
	Every int
	// Resume：true 时开跑前尝试恢复（Load + LoadState + 跳过已处理 bar 前缀）。
	Resume bool
}

// RunCheckpointed 是 Run 的检查点版本：逐 bar 驱动逻辑与 Run **完全一致**
//（clk.Advance 非递减、失败立即停），外加周期/终局/异常退出保存与续跑恢复。
//
// ─── 语义（已裁决） ────────────────────────────────────────────────
//
//  1. 恢复：Resume=true 且库中有状态 → h.LoadState(state)（**失败立即返回
//     error，绝不静默从头跑**）；然后跳过 bars 前缀中所有 Date <= 存储 ts 的
//     bar，从第一根 Date > ts 开始处理。Resume=true 但库中无状态
//     (ErrStateNotFound) → 从头正常跑（首次运行是正常情况，不报错）；其它
//     Load error → 返回。
//  2. Resume=true 但 Store==nil → error（配置矛盾，fail-loud）。
//  3. 保存时机：① 周期保存（Every>0 且到日期边界且攒够 Every 根）；
//     ② 正常跑完最后一根后必保存；③ 异常退出（OnBar 报错 / 时间倒退）尽力
//     保存——但若最后处理的 bar 不在日期边界则不保存（见文件头「宁可重做」）。
//  4. 无 bar 可跑（恢复后前缀覆盖全部 bars，或空输入）→ 直接返回 nil，不保存
//     （没有任何状态推进，写一份不会有新信息；空流跑完不算错误）。
func (r *StreamRunner) RunCheckpointed(ctx context.Context, h BarHandler, bars []domain.OHLCV, clk clock.Clock, ckpt Checkpoint) error {
	if h == nil {
		return errors.New("strategy: StreamRunner 收到 nil BarHandler")
	}
	if clk == nil {
		return errors.New("strategy: StreamRunner 收到 nil Clock")
	}
	if ckpt.Resume && ckpt.Store == nil {
		return errors.New("strategy: RunCheckpointed Resume=true 但 Store 为 nil——无法恢复，配置矛盾")
	}

	start, err := r.resumeStart(ctx, h, bars, ckpt)
	if err != nil {
		return err
	}
	if start >= len(bars) {
		// 无 bar 可跑：恢复后断点已覆盖全部输入（或本就空流）。直接返回，
		// 不保存（没有状态推进，也没有可写的「新」断点）。
		return nil
	}

	processedSinceSave := 0
	for i := start; i < len(bars); i++ {
		bar := bars[i]
		if err := clk.Advance(bar.Date); err != nil {
			origErr := fmt.Errorf(
				"strategy: StreamRunner 在第 %d 根 bar (symbol=%s date=%s) 推进时钟失败，已停止喂后续 bar: %w",
				i, bar.Symbol, bar.Date.UTC().Format(time.RFC3339Nano), err)
			return r.exitWithBestEffortSave(ctx, h, bars, ckpt, i, start, origErr)
		}
		if err := h.OnBar(ctx, bar); err != nil {
			origErr := fmt.Errorf(
				"strategy: StreamRunner 第 %d 根 bar (symbol=%s date=%s) OnBar 失败: %w",
				i, bar.Symbol, bar.Date.UTC().Format(time.RFC3339Nano), err)
			return r.exitWithBestEffortSave(ctx, h, bars, ckpt, i, start, origErr)
		}

		processedSinceSave++
		if !isDateBoundary(bars, i) {
			continue
		}
		// 到达日期边界：终局必保存；否则周期保存（Every>0 且攒够 Every 根）。
		terminal := i+1 == len(bars)
		if terminal || (ckpt.Every > 0 && processedSinceSave >= ckpt.Every) {
			if err := saveCheckpoint(ctx, h, ckpt, bar.Date); err != nil {
				return err
			}
			processedSinceSave = 0
		}
	}
	return nil
}

// resumeStart 计算首批要处理的 bar 下标 start。
//
//   - Resume=false → 0（从头跑）。
//   - Resume=true：Load 库中状态；ErrStateNotFound → 0（首次运行，正常）；
//     其它 error → 返回 error；有状态 → h.LoadState（失败即返回 error，绝不
//     静默从头跑），然后 start = skipPrefix(bars, 存储 ts)。
func (r *StreamRunner) resumeStart(ctx context.Context, h BarHandler, bars []domain.OHLCV, ckpt Checkpoint) (int, error) {
	if !ckpt.Resume {
		return 0, nil
	}
	state, ts, err := ckpt.Store.Load(ctx, ckpt.StrategyID, ckpt.RunID)
	if err != nil {
		if errors.Is(err, ErrStateNotFound) {
			// 首次运行：库中无检查点，从头跑是正常情况，不报错。
			return 0, nil
		}
		return 0, fmt.Errorf("strategy: RunCheckpointed 恢复失败 (strategy=%s run=%s): %w", ckpt.StrategyID, ckpt.RunID, err)
	}
	// fail-loud：LoadState 失败必须终止——静默从头跑会让「续跑」变成「悄悄
	// 重来」，结果错了还查不出来（BarHandler.LoadState 契约明确要求）。
	if err := h.LoadState(state); err != nil {
		return 0, fmt.Errorf("strategy: RunCheckpointed LoadState 失败 (strategy=%s run=%s): %w", ckpt.StrategyID, ckpt.RunID, err)
	}
	return skipPrefix(bars, ts), nil
}

// exitWithBestEffortSave 在异常退出路径上尽力保存断点，然后返回原始 error。
//
//   - 最后**成功处理**的 bar 是 failIdx-1（failIdx 那根 Advance 或 OnBar 失败，
//     未成功处理）。若它 < start（本次一段一根都没成功处理）→ 无可保存。
//   - 若最后处理的 bar **不在日期边界**（同日期块还有未处理的 bar）→ 不保存：
//     保存会让续跑误跳该日期块里尚未处理的 bar。宁可让续跑从上一个已保存的
//     边界重做该日期块——重做的是同一段确定性计算，结果一致。
//   - 保存失败**不掩盖原始 error**：原始 error 在前（errors.Is 仍可命中根因），
//     保存失败经 errors.Join 附加在后，两者都可见、都不被丢弃（见下）。
//
// 保存用的 ctx：原 ctx 很可能已取消（这正是异常退出的常见原因，例如上层
// 超时/关停）。断点必须在故障中**仍能写下**，故用 context.WithoutCancel(ctx)
// 摘掉取消信号，再套 bestEffortSaveTimeout 超时——不取消 ≠ 无上限，库挂了
// 不能让「尽力保存」把进程拖死。
func (r *StreamRunner) exitWithBestEffortSave(ctx context.Context, h BarHandler, bars []domain.OHLCV, ckpt Checkpoint, failIdx, start int, origErr error) error {
	saveErr := r.bestEffortSave(ctx, h, bars, ckpt, failIdx, start)
	if saveErr == nil {
		return origErr
	}
	// errors.Join 保留两侧：errors.Is(joined, 原始根因) 仍成立（原始 error 在
	// 前，语义上「优先」），同时保存失败不会被静默吞掉——审计能同时看到
	// 「策略为什么停」与「断点为什么没写下」。
	return errors.Join(origErr, saveErr)
}

// bestEffortSave 执行异常退出的「尽力保存」，返回保存本身的 error（未保存在
// 边界等情况返回 nil，不是错误）。
func (r *StreamRunner) bestEffortSave(ctx context.Context, h BarHandler, bars []domain.OHLCV, ckpt Checkpoint, failIdx, start int) error {
	if ckpt.Store == nil {
		return nil
	}
	lastProcessed := failIdx - 1
	if lastProcessed < start {
		// 本段尚无一概成功处理，没有可保存的推进。
		return nil
	}
	if !isDateBoundary(bars, lastProcessed) {
		// 最后处理的 bar 在日期块中间：保存会让续跑误跳同日期块的未处理 bar。
		return nil
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bestEffortSaveTimeout)
	defer cancel()
	return saveCheckpoint(sctx, h, ckpt, bars[lastProcessed].Date)
}

// saveCheckpoint 序列化 h 当前状态并落库，ts 为状态对应时刻。Store==nil 时
// 无 DB 行为（直接 nil）——这是 Run 委托 RunCheckpointed 时「零 DB 依赖」的
// 保证。
//
// 保存失败**向上返回 error**（周期/终局保存路径）：一份没落下的检查点意味着
// 「续跑会从更早的边界重做」，这本身安全，但静默吞掉会让「以为存了」的调用方
// 误判。宁可 fail-loud。
func saveCheckpoint(ctx context.Context, h BarHandler, ckpt Checkpoint, ts time.Time) error {
	if ckpt.Store == nil {
		return nil
	}
	state, err := h.SaveState()
	if err != nil {
		return fmt.Errorf("strategy: 检查点 SaveState 失败 (strategy=%s run=%s): %w", ckpt.StrategyID, ckpt.RunID, err)
	}
	if err := ckpt.Store.Save(ctx, ckpt.StrategyID, ckpt.RunID, ts, state); err != nil {
		return fmt.Errorf("strategy: 检查点落库失败 (strategy=%s run=%s ts=%s): %w",
			ckpt.StrategyID, ckpt.RunID, ts.UTC().Format(time.RFC3339Nano), err)
	}
	return nil
}

// skipPrefix 返回 bars 前缀中所有 Date <= ts 的 bar 根数，即续跑应当跳过的
// bar 数（从该下标开始处理）。抽成纯函数以便单测边界。
//
// 用 !Date.After(ts) 而非 Date.Before(ts)：「等于 ts」也必须跳过——存储 ts 就是
// 最后已处理 bar 的时刻，等于它的那根已经处理过，再喂一次就是重复处理。
//（破坏验证 a：把此处改成 Date.Before(ts)（严格小于）→ 续跑等价性测试红，
// 因为边界日期的那根 bar 会被重复处理。）
func skipPrefix(bars []domain.OHLCV, ts time.Time) int {
	n := 0
	for n < len(bars) && !bars[n].Date.After(ts) {
		n++
	}
	return n
}

// isDateBoundary 判定第 i 根 bar 是否是日期边界：它是最后一根，或下一根进入
// 新的日期。只有边界处才是合法保存点（见文件头裁决）。
func isDateBoundary(bars []domain.OHLCV, i int) bool {
	return i+1 == len(bars) || bars[i+1].Date.After(bars[i].Date)
}
