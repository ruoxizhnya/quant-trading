package domain

import "time"

// Sector 是板块字典行（物理表 sectors）。
//
// DATA-01：本表原按「东财板块快照」设计（DDL 带 change_pct / leading_symbol），
// 但东财 push2 出口在 2026-10-14 实测返回 502，取不到数。本轮改由
// stocks.industry（Tushare stock_basic 同步而来，5908 只全覆盖、111 个细分行业）
// 派生：sector_code 用行业名，category 固定 'industry'，快照类字段（涨跌幅、
// 龙头股）本源不提供 ⇒ 留 NULL 而不是编造。
//
// 诚实标注：trade_date 是**派生快照日**（同步当天），不是行情日；
// source 标 'stocks_industry' 以便与将来的东财/同花顺板块区分。
type Sector struct {
	SectorCode    string    `json:"sector_code"`
	SectorName    string    `json:"sector_name"`
	Category      string    `json:"category"`
	TradeDate     time.Time `json:"trade_date"`
	ChangePct     *float64  `json:"change_pct,omitempty"`
	LeadingSymbol string    `json:"leading_symbol,omitempty"`
	Source        string    `json:"source"`
}

// StockSector 是个股 ↔ 板块映射行（物理表 stock_sector_map）。
type StockSector struct {
	Symbol     string `json:"symbol"`
	SectorCode string `json:"sector_code"`
	SectorName string `json:"sector_name"`
	Source     string `json:"source"`
}

// StockIndustry 是派生用的最小读取单元（symbol + 行业名）。
type StockIndustry struct {
	Symbol   string
	Industry string
}
