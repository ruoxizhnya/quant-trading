package data

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SectorSyncStore 是板块派生同步的存储面（DATA-01）。
//
// 命名成独立接口而非塞进 TushareStore，是因为这一条链路**不走 Tushare API** ——
// 它读的是已同步落库的 stocks.industry。沿用 SupplyChainStore / MacroStore
// 的惯例：独立命名契约，钉住 Save 签名。
type SectorSyncStore interface {
	ListStockIndustries(ctx context.Context) ([]domain.StockIndustry, error)
	SaveSectorBatch(ctx context.Context, sectors []domain.Sector) error
	SaveStockSectorMapBatch(ctx context.Context, rows []domain.StockSector) error
}

// SectorSyncResult 是同步结果（供 handler 回报与测试断言）。
type SectorSyncResult struct {
	Sectors     int      `json:"sectors"`
	Mappings    int      `json:"mappings"`
	Skipped     int      `json:"skipped"`      // industry 为空被跳过的个股数
	TradeDate   string   `json:"trade_date"`   // 派生快照日
	Source      string   `json:"source"`       // 数据来源标签
	SectorCodes []string `json:"sector_codes"` // 排序后的板块代码（前若干个由调用方截断）
}

// SectorSourceStocksIndustry 是本派生链路的来源标签。
//
// 与 DDL 默认值 'eastmoney' 区分开：本轮数据来自 stocks.industry（Tushare
// stock_basic 同步），不是东财板块快照。混标会让「这行哪来的」无法回答。
const SectorSourceStocksIndustry = "stocks_industry"

// SyncSectorsFromStocks 由 stocks.industry 派生 sectors 与 stock_sector_map。
//
// 为什么这样灌（DATA-01 的环境事实，写在这里以免后来者重蹈）：
//  1. 板块表的既有数据源是 eastmoney_sectors adapter（push2.eastmoney.com），
//     2026-10-14 实测该出口返回 **502 Bad Gateway**，取不到数；
//  2. Tushare 侧没有「个股 → 行业」接口：`stock_industry` 接口名不存在（40101），
//     申万 `index_member` 的 is_new 全为 'N'（SW2014 历史成分，无当前成分）；
//  3. stocks.industry 是 stock_basic 同步来的真实行业（2026-10-14 实测
//     5908/5908 只非空、111 个细分行业），零新增外网依赖、覆盖全市场。
//
// 因此本函数不新拉外网数据，只把库里已有的行业字段**物化**成板块表。
// 快照类字段（change_pct / leading_symbol）本源不提供 ⇒ 留 NULL，不编造。
func SyncSectorsFromStocks(ctx context.Context, store SectorSyncStore, tradeDate time.Time) (*SectorSyncResult, error) {
	if store == nil {
		return nil, fmt.Errorf("sector sync: store is nil")
	}
	if tradeDate.IsZero() {
		tradeDate = time.Now()
	}

	industries, err := store.ListStockIndustries(ctx)
	if err != nil {
		return nil, err
	}

	// 行业去重（有序，保证幂等重放产出一致）
	seen := make(map[string]string, 64) // industry -> sector_code
	mappings := make([]domain.StockSector, 0, len(industries))
	skipped := 0

	for _, si := range industries {
		if si.Industry == "" || si.Symbol == "" {
			skipped++
			continue
		}
		seen[si.Industry] = si.Industry // sector_code 即用行业名本身
		mappings = append(mappings, domain.StockSector{
			Symbol:     si.Symbol,
			SectorCode: si.Industry,
			SectorName: si.Industry,
			Source:     SectorSourceStocksIndustry,
		})
	}

	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	sectors := make([]domain.Sector, 0, len(codes))
	for _, code := range codes {
		sectors = append(sectors, domain.Sector{
			SectorCode: code,
			SectorName: code,
			Category:   "industry",
			TradeDate:  tradeDate,
			Source:     SectorSourceStocksIndustry,
		})
	}

	if err := store.SaveSectorBatch(ctx, sectors); err != nil {
		return nil, err
	}
	if err := store.SaveStockSectorMapBatch(ctx, mappings); err != nil {
		return nil, err
	}

	return &SectorSyncResult{
		Sectors:     len(sectors),
		Mappings:    len(mappings),
		Skipped:     skipped,
		TradeDate:   tradeDate.Format("2006-01-02"),
		Source:      SectorSourceStocksIndustry,
		SectorCodes: codes,
	}, nil
}
