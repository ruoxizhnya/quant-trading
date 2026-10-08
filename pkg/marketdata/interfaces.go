// data-engine 契约（K0 切片 2）—— 本文件只冻结接口，不含实现逻辑。
//
// 模块职责（蓝图 §5 data-engine 行）：数据入口唯一闸口。
//   - 回测：Snapshot(symbols, start, end) 一次性把整个回测窗口物化到
//     内存（底层即 Provider.BulkLoadOHLCV，1 次 / run），此后 VirtualClock
//     只在内存中推进，不再触碰数据源；
//   - 实盘：Subscribe(symbols) 建立推送订阅，feed → bar 聚合 → 发布
//     msgbus.TopicDataBar。
//
// ⚠️ 回测侧禁止 per-bar / per-day 取数（ADR-030 OBS-11 已登记为反模式：
// 在日循环内部发 HTTP、body 带整个股票池的完整 K 线——per-day 而非
// per-run，ADR-027 已判「应当删除，而不是优化」）。
// **Snapshot 是回测取数的唯一入口**：物化边界在 run 开始处，绝不在
// bar 循环内。这条是回测确定性与性能的共同前提。
//
// Provider（pkg/marketdata/provider.go，冻结、逐字不改）是数据源契约
// 入口：回测 = PG 快照源，实盘 = 推送 feed。
//
// K0 切片 2：stub 方法体固定 panic("contract stub: not implemented")，
// K1 实现直接替换 stub。
package marketdata

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// DataEngine 是数据入口的唯一闸口。策略 / 回测引擎 / 实盘引擎一律经本
// 接口取数，禁止直接持有 Provider 按 bar 查询。
//
// 方法语义（冻结）：
//   - Snapshot：回测专用。一次性返回 [start, end] 窗口内 symbols 的全量
//     OHLCV（按 symbol 索引）。返回前必须已物化完毕；调用方此后不得再
//     就同一窗口取数。窗口内缺数据的 symbol 直接缺席 map，不补零值
//     （fail-loud 优于静默填充）。
//   - Subscribe：实盘专用。建立 symbols 的推送订阅；返回后 bar 经
//     msgbus.TopicDataBar 逐根投递。在回测模式（VirtualClock）下调用
//     一律返回 error——回测没有推送语义。
//   - Provider：返回本引擎背后的数据源契约入口（回测=PG 快照源，
//     实盘=推送 feed），供需要原始 provider 能力的调用方（如基本面、
//     交易日历）显式取用。
type DataEngine interface {
	Snapshot(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error)
	Subscribe(ctx context.Context, symbols []string) error
	Provider() Provider
}

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// SnapshotDataEngine 是回测侧数据引擎的契约 stub：Provider 为 PG 快照源，
// Snapshot 一次性物化窗口，Subscribe 不可用（回测无推送语义）。
type SnapshotDataEngine struct{}

// Snapshot 一次性物化 [start, end] 全窗口数据。
func (e *SnapshotDataEngine) Snapshot(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	panic("contract stub: not implemented")
}

// Subscribe 在回测模式下无意义，恒返回 error。
func (e *SnapshotDataEngine) Subscribe(ctx context.Context, symbols []string) error {
	panic("contract stub: not implemented")
}

// Provider 返回回测侧数据源（PG 快照源）。
func (e *SnapshotDataEngine) Provider() Provider { panic("contract stub: not implemented") }

// RealtimeDataEngine 是实盘侧数据引擎的契约 stub：Provider 为推送 feed，
// Subscribe 建立订阅后逐 bar 投递 msgbus.TopicDataBar。
type RealtimeDataEngine struct{}

// Snapshot 在实盘模式下不提供整窗口物化，恒返回 error。
func (e *RealtimeDataEngine) Snapshot(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	panic("contract stub: not implemented")
}

// Subscribe 建立 symbols 的推送订阅。
func (e *RealtimeDataEngine) Subscribe(ctx context.Context, symbols []string) error {
	panic("contract stub: not implemented")
}

// Provider 返回实盘侧数据源（推送 feed）。
func (e *RealtimeDataEngine) Provider() Provider { panic("contract stub: not implemented") }

// PGProvider 是 Provider（pkg/marketdata/provider.go，冻结）的契约 stub：
// 回测侧的 PG 快照源。它把 Provider 的 11 个方法钉进编译期检查——
// 从 Provider 增删任一方法，本包 go build 失败。
type PGProvider struct{}

// Name 返回数据源名称。
func (p *PGProvider) Name() string { panic("contract stub: not implemented") }

// CheckConnectivity 校验数据源连通性。
func (p *PGProvider) CheckConnectivity(ctx context.Context) error {
	panic("contract stub: not implemented")
}

// GetOHLCV 取单 symbol 的 K 线。
func (p *PGProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	panic("contract stub: not implemented")
}

// GetFundamental 取基本面数据。
func (p *PGProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	panic("contract stub: not implemented")
}

// GetStocks 取交易所股票列表。
func (p *PGProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	panic("contract stub: not implemented")
}

// GetLatestPrice 取最新价。
func (p *PGProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	panic("contract stub: not implemented")
}

// GetIndexConstituents 取指数成分。
func (p *PGProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	panic("contract stub: not implemented")
}

// GetTradingDays 取交易日历。
func (p *PGProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	panic("contract stub: not implemented")
}

// GetStock 取单只股票元数据。
func (p *PGProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	panic("contract stub: not implemented")
}

// BulkLoadOHLCV 批量物化多 symbol 的 K 线（Snapshot 的底层实现）。
func (p *PGProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	panic("contract stub: not implemented")
}

// CheckCalendarExists 校验交易日历是否已入库。
func (p *PGProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	panic("contract stub: not implemented")
}

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 前三行是既有样板（切片 1 模式）：stub 漂移出接口时 go build 失败。
//
// 方法表达式守卫（其后各行）防止「从接口删方法后 build 仍绿」：
// 从 DataEngine 删除任一方法，DataEngine.<Method> 即未定义，本包
// go build 直接编译失败——这是 K0 验收破坏验证的护栏。
var (
	_ DataEngine = (*SnapshotDataEngine)(nil)
	_ DataEngine = (*RealtimeDataEngine)(nil)
	_ Provider   = (*PGProvider)(nil)

	_ func(DataEngine, context.Context, []string, time.Time, time.Time) (map[string][]domain.OHLCV, error) = DataEngine.Snapshot
	_ func(DataEngine, context.Context, []string) error                                                    = DataEngine.Subscribe
	_ func(DataEngine) Provider                                                                            = DataEngine.Provider
)
