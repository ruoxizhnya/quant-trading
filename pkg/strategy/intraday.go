package strategy

import (
	"context"
	"time"
)

// ─── D6 · 多频率进回测策略视野（ADR-031 蓝图 §4.3 裁定 4）────────────────
//
// 目标口径：策略在回测中「日频决策 + 引用当日内分钟特征」（如开盘半小时
// 动量），分钟级防偷看闸门按分钟粒度生效（10:00 的 1 分钟线 10:01 才
// 可见），现有纯日线回测结果数值不变。
//
// 红线（蓝图 §4.3，与引擎侧 warmIntradayIfNeeded 的注释互为镜像）：
// 策略运行时**禁止**直接查 marketdata.Provider —— 日内数据与日线同制，
// 全部在回测启动前快照预加载，经本文件的契约注入。执法者是
// internal/repoguard 的 import 隔离护栏（pkg/strategy 不许 import
// pkg/marketdata）加本契约的接口边界，破坏验证腿②的对象。

// IntradayFeatureProvider 是日内分钟特征的供给契约（D6）。
//
// 范式同 FactorZScoreReader / FundamentalsProvider：引擎靠类型断言
// 识别 IntradayAware 策略就注入本供给，识别不了就跳过（纯日线策略
// 零行为变化）。与那两个老范式的一点关键差异写在 IntradayFeature
// 的注释里 —— **asOf 是分钟粒度的**。
type IntradayFeatureProvider interface {
	// Features 返回本供给支持的日内特征名清单（能力声明）。
	// 策略可据此自查「我要的特征供方有没有」；清单与 SPEC.md
	// 「日内特征目录」一一对应（防漂移由测试钉住）。
	Features() []string

	// IntradayFeature 返回 symbol 在 asOf 时刻**已可见**的日内特征值。
	//
	// asOf 是分钟粒度的回放时刻（不是日）：特征只许由 VisibleAt <= asOf
	// 的 bar 算出（闸门在 provider 实现里集中执行，pkg/domain/market
	// 的 OHLCV.VisibleBy）。蓝图 §4.3 原句：10:00 的 1 分钟线 10:01
	// 才可见 —— 依赖 10:00 bar 的特征在 10:00:30 不可得、10:01 起可得。
	//
	// 第二个返回值为 false 表示「此刻还没有这个特征」（窗口未闭合 /
	// 当日无数据 / 停牌半日），**不是 0** —— 缺失≠0 是规范层三要求
	// 之一，把「还没有」读成 0 会让策略对着凭空捏造的数值排序。
	//
	// ctx 只为取消/超时；实现不得经它触网或触库（数据全部来自预加载
	// 快照 —— 红线）。
	IntradayFeature(ctx context.Context, symbol, name string, asOf time.Time) (float64, bool)
}

// IntradayAware 是可选能力接口（D6），范式同 FactorAware /
// FundamentalAware。
//
// GenerateSignals 的签名里没有日内特征参数（改签名要动所有策略实现），
// 所以走「引擎识别得了就注入、识别不了就跳过」。策略没拿到供给却在
// 表达式/逻辑里要用日内特征时，应当**明确失败**而不是拿到一串 0 ——
// 与 pkg/strategy/expression 的 OHLCVDataProvider 对基本面字段的
// 口径一致（ADR-024：引擎不是「不支持」，是「不给假数字」）。
type IntradayAware interface {
	SetIntradayFeatures(provider IntradayFeatureProvider)
}
