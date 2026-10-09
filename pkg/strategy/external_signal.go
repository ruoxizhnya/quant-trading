// 外部模型信号契约（K7 切片 1）—— L3b 信号注入的「信号侧」。
//
// 蓝图 §6.4：外部模型（进程外，任意 Python/ML 框架）不直接下单，而是产出
// 信号数据注入内核、由策略消费（对应 nautilus 的 Actor.on_signal / 本项目
// 的 GenePool 回注链路）。模型在进程外，内核只认信号契约——把「自由度」
// 挡在内核之外，内核内仍然确定、可审阅。
//
// 本文件只冻结契约（类型 + content_hash 坐标 + 校验 + 存储接口），不含
// 消费逻辑——消费契约在 signal_strategy.go（SignalStrategy，Actor 侧）。
//
// 落位理由：信号表的 DB 归属是 strategy-runtime 模块（蓝图 §5 矩阵 L3 行：
// quant.strategy_state + 信号表），故与 StateStore 同住 pkg/strategy，
// 不走 pkg/storage 应用层 store（state_store.go 同一先例）。
package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ExternalSignal 是外部模型注入内核的一条信号（任务契约：符号/方向/强度/as_of）。
//
// 字段语义：
//   - ModelID  —— 外部模型标识（source）。哪个进程外模型产出的这条信号，
//     是 content_hash 坐标里的「source」位（对齐 AGENTS.md 的
//     citation = {source, dataset, key, as_of, content_hash}）。
//   - Symbol   —— 标的（key 位）。
//   - Direction—— long / short / close。hold 无意义：模型对某标的无观点
//     「不发信号」，而非「发一条 hold」（空操作不是信号）。
//   - Strength —— 强度。不做范围约束（不同模型的强度尺度不同），只拒非有限值。
//   - AsOf     —— 信号生效的交易日/时刻，对齐内核回放的 bar 时间轴。这是
//     「按 as_of 拉取」防前视的锚点：as_of 晚于当前回放日的信号物理不可见。
//   - Factors  —— 诊断因子快照（模型自报的解释变量），**不参与 content_hash**
//     （非身份，只供审计/复现，见 SignalContentHash 注释）。
type ExternalSignal struct {
	ModelID   string             `json:"model_id"`
	Symbol    string             `json:"symbol"`
	Direction domain.Direction   `json:"direction"`
	Strength  float64            `json:"strength"`
	AsOf      time.Time          `json:"as_of"`
	Factors   map[string]float64 `json:"factors,omitempty"`
}

// signalHashPayload 是 content_hash 的规范形态。encoding/json 对 struct 按
// **字段声明序**序列化（键序稳定），故同一信号两次哈希字节逐位一致。
// AsOf 用 string（UTC RFC3339Nano）而非 time.Time：显式归一 UTC + 精确到纳秒，
// 避免 time.Time 的 JSON 默认格式依赖时区/精度导致同一瞬时哈希不同。
type signalHashPayload struct {
	ModelID   string  `json:"model_id"`
	Symbol    string  `json:"symbol"`
	Direction string  `json:"direction"`
	Strength  float64 `json:"strength"`
	AsOf      string  `json:"as_of"`
}

// dayOf 归一 UTC 自然日（丢弃时分秒与时区，锚定 00:00 UTC）。
//
// ─── 裁决：信号对齐以「日」为粒度 ──────────────────────────────────
// 内核是日线驱动的（K2 的 BarHandler 逐 bar = 逐交易日），外部信号的 as_of
// 的时分秒没有语义——它只回答「这条信号属于哪个交易日」。若保留时分秒，
// 「同一天不同时刻」会让 as_of 与 bar.Date 的比较产生歧义（例如信号 as_of
// 在 15:00、bar.Date 在 00:00，`as_of <= bar.Date` 恒假，当日信号被误排除）。
// 这与 K5 的 signalKey 归一化是同一类病根（time.Time 含单调钟/时区，比较
// 脆弱）。故统一归一到 00:00 UTC，让「日」成为唯一比较单位。
func dayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// SignalContentHash 计算一条外部信号的 content_hash 坐标（64 位小写 hex sha256，
// 约定与 pkg/storage.ContentHashOf 一致）。
//
// ─── 裁决：只哈希「可执行字段」，不哈希 Factors ──────────────────────
// 可执行字段（model_id / symbol / direction / strength / as_of）决定这条信号
// **会让内核做什么**；Factors 是解释性诊断。同一模型对同一标的同一 as_of
// 报出「相同的方向与强度、但附带不同的因子快照」时，内核的下单动作没有变
// —— 把它们哈希成两条信号只会让幂等去重失效、让「注入即下单」多出噪声。
// 故 content_hash 覆盖可执行字段，Factors 落库但不入哈希。
//
// json.Marshal 对纯 string/float64 的 struct 不可能失败（无 chan/func/complex），
// 错误分支为防御性占位，不期望触发。
func SignalContentHash(sig ExternalSignal) string {
	p := signalHashPayload{
		ModelID:   sig.ModelID,
		Symbol:    sig.Symbol,
		Direction: string(sig.Direction),
		Strength:  sig.Strength,
		AsOf:      dayOf(sig.AsOf).UTC().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(p)
	if err != nil {
		// 不可达：字段全为标量。保留分支以免将来加字段引入不可序列化类型时
		// 静默产出空哈希——宁可 panic 也不在可追溯坐标上造假。
		panic(fmt.Sprintf("strategy: SignalContentHash 序列化失败（不应发生）: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ValidateExternalSignal 校验一条外部信号是否可落库（fail-loud 拒绝）。
//
// 拒绝的口径是「写下去就是脏行/会让下游静默出错」：
//   - ModelID 空     —— content_hash 坐标的 source 位缺失，信号无法归因到模型；
//   - Symbol 空      —— 没有标的，下游无法路由；
//   - Direction 非法 —— 只认 long/short/close；hold/空/任意字符串都会让
//     convertStrategySignals 的 resolveDirection 静默跳过或误判方向；
//   - Strength 非有限 —— NaN/±Inf 是垃圾值（OBS-01 的 garbage 判据同源）；
//   - AsOf 零值       —— 没有时间锚点，「按 as_of 拉取」无从对齐，未来日期
//     判定也失去意义。
//
// 落位为包级导出函数：PGSignalStore.Save 调用它，任何未来的 SignalStore
// 实现与测试也可复用同一口径（校验只有一份，不随实现漂移）。
func ValidateExternalSignal(sig ExternalSignal) error {
	switch {
	case sig.ModelID == "":
		return errors.New("strategy: 外部信号 ModelID 为空（content_hash 的 source 位缺失）")
	case sig.Symbol == "":
		return errors.New("strategy: 外部信号 Symbol 为空")
	case sig.Direction != domain.DirectionLong &&
		sig.Direction != domain.DirectionShort &&
		sig.Direction != domain.DirectionClose:
		return fmt.Errorf("strategy: 外部信号 Direction 非法 %q（只认 long/short/close）", sig.Direction)
	case math.IsNaN(sig.Strength) || math.IsInf(sig.Strength, 0):
		return fmt.Errorf("strategy: 外部信号 Strength 非有限值 %v（NaN/±Inf 是垃圾）", sig.Strength)
	case sig.AsOf.IsZero():
		return errors.New("strategy: 外部信号 AsOf 为零值（无时间锚点）")
	}
	return nil
}

// SignalStore 持久化外部模型信号（quant.external_signals）。
//
// 落位理由同 StateStore：strategy-runtime 模块的 DB 归属，不走 pkg/storage
// 应用层 store——与 pkg/eventstore、PGStateStore 同为「内核模块自带 store」。
type SignalStore interface {
	// Save 落库一条外部信号。幂等：content_hash 相同 → 不重复落库（第一份
	// 保留）。非法信号（见 ValidateExternalSignal）返回 error，不落库。
	Save(ctx context.Context, sig ExternalSignal) error

	// ListFor 读出 (modelID, symbol) 下 as_of <= upTo 的全部信号，按 as_of
	// 升序。**未来信号（as_of > upTo）物理不可见**——这是「按 as_of 拉取」
	// 防前视的主边界：内核回放到时刻 upTo 时，只能看到 <= upTo 的信号。
	ListFor(ctx context.Context, modelID, symbol string, upTo time.Time) ([]ExternalSignal, error)
}
