package market

import "time"

// OHLCV represents daily candlestick data.
type OHLCV struct {
	Symbol    string    `json:"symbol"`
	Date      time.Time `json:"date"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	Turnover  float64   `json:"turnover"`
	TradeDays int       `json:"trade_days"`
	// LimitUp indicates the stock hit the upper price limit today (涨停)
	LimitUp bool `json:"limit_up"`
	// LimitDown indicates the stock hit the lower price limit today (跌停)
	LimitDown bool `json:"limit_down"`
}

// Stock represents a tradable security.
type Stock struct {
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Exchange  string    `json:"exchange"`
	Industry  string    `json:"industry"`
	MarketCap float64   `json:"market_cap"`
	ListDate  time.Time `json:"list_date"`
	Status    string    `json:"status"` // active, suspended, delisted

	// DelistDate 是摘牌日，**nil = 仍在市**（P2-4）。
	//
	// 用指针不是偷懒：零值 time.Time 是 0001-01-01，跟"退市日期未知"
	// 是两回事，混在一起会让「今天还在市」被算成「一万年前就退市了」。
	//
	// 没有这个字段就不可能修幸存者偏差 —— 回测 2020 年时，2021 年退市的
	// 票必须**在池子里**（否则收益被系统性高估），但又不能在 2021 年之后
	// 继续参与交易（那时候它已经不存在了）。两条都要靠这个日期。
	DelistDate *time.Time `json:"delist_date,omitempty"`
}

// IndexConstituent represents a constituent stock of an index.
type IndexConstituent struct {
	ID        int64     `json:"id"`
	IndexCode string    `json:"index_code"` // e.g. "000300.SH" (CSI 300), "000500.SH" (CSI 500), "000852.SH" (CSI 800)
	Symbol    string    `json:"symbol"`     // stock ts_code, e.g. "000001.SZ"
	InDate    time.Time `json:"in_date"`    // date when stock entered index
	OutDate   time.Time `json:"out_date"`   // date when stock exited index (zero if still in)
	Weight    float64   `json:"weight"`     // weight in index (if available from index_weight API)
}

// Split represents a stock split or rights issue event.
// Used for forward-price adjustment verification.
type Split struct {
	ID           int64     `json:"id"`
	Symbol       string    `json:"symbol"`
	TradeDate    time.Time `json:"trade_date"`     // ex-date of the split
	AnnDate      time.Time `json:"ann_date"`       // announcement date
	StkDivRatio  float64   `json:"stk_div_ratio"`  // stock dividend / split ratio (e.g., 0.1 = 10% stock dividend)
	CashDivRatio float64   `json:"cash_div_ratio"` // cash dividend ratio
	Currency     string    `json:"currency"`       // usually CNY
}

// Dividend represents a dividend event for a stock.
type Dividend struct {
	ID        int64     `json:"id"`
	Symbol    string    `json:"symbol"`
	AnnDate   time.Time `json:"ann_date"`   // announcement date
	RecDate   time.Time `json:"rec_date"`   // record date (shareholders as of this date receive dividend)
	PayDate   time.Time `json:"pay_date"`   // payment date / ex-dividend date
	DivAmt    float64   `json:"div_amt"`    // cash dividend amount per share
	StkDiv    float64   `json:"stk_div"`    // stock dividend per share (bonus shares)
	StkRatio  float64   `json:"stk_ratio"`  // stock split ratio
	CashRatio float64   `json:"cash_ratio"` // cash dividend ratio
}

// Fundamental represents fundamental financial data.
//
// 数值字段一律是 *float64：财报里缺一项是常态（当季没披露、口径不同、
// 采集漏了），而 **"缺失"和"等于 0"是两件完全不同的事** ——
// PE=0 会被读成"白送的股票"，是那种会让人真的买错东西的错。
//
// 改用指针之前，存储层用 COALESCE(col, 0) 兜底（见 TASKS P2-10）：
// 库里的 NULL 和真实 0 都被折成 0.0，下游无从分辨。现在 nil 就是"不知道"，
// 用之前必须显式判空 —— 这层啰嗦正是这个类型存在的理由。
type Fundamental struct {
	Symbol string `json:"symbol"`
	// Date 是这条记录的**可用日**（P1-4）：读出来时等于
	// COALESCE(ann_date, trade_date)，即"从哪天起这条数据才存在"。
	//
	// 写进去时历史上曾被当成报告期截止日（路径 A 把 end_date 塞进来），
	// 那是 P0-1 想修又没修干净的前视偏差 —— 见下方 AnnDate。
	Date time.Time `json:"date"`
	// AnnDate 是真实披露日（P1-4）。nil = 不知道什么时候披露的。
	//
	// 为什么必须留着它：路径 A（FetchFundamentals）此前把 API 返回的
	// ann_date 直接丢弃，只存报告期截止日 end_date。于是读取侧
	// COALESCE(ann_date, trade_date) 对它退化成 end_date —— 三季报
	// 9/30 截止、10/25 披露，却能在 9/30 就被回测看见。P0-1 的 COALESCE
	// 只是把洞盖住了，没补上；补的办法是让写入侧真的把 ann_date 存下来。
	//
	// 指针语义同 DelistDate：零值 time.Time 会被读成"从未披露"。
	AnnDate      *time.Time `json:"ann_date,omitempty"`
	PE           *float64   `json:"pe"`
	PB           *float64   `json:"pb"`
	PS           *float64   `json:"ps"`
	ROE          *float64   `json:"roe"`
	ROA          *float64   `json:"roa"`
	DebtToEquity *float64   `json:"debt_to_equity"`
	GrossMargin  *float64   `json:"gross_margin"`
	NetMargin    *float64   `json:"net_margin"`
	Revenue      *float64   `json:"revenue"`
	NetProfit    *float64   `json:"net_profit"`
	TotalAssets  *float64   `json:"total_assets"`
	TotalLiab    *float64   `json:"total_liab"`
}

// FundamentalData represents financial data from Tushare financial_data API.
// Used for factor-based screening (PE, PB, PS, ROE, ROA, etc.).
//
// Unlike Fundamental (pure domain), FundamentalData carries persistence
// metadata (ID, CreatedAt) and uses *float64 pointers to distinguish
// "missing" (nil) from "zero" (0.0) — important for sparse financial
// reports. This is the "view" half of the soft-layering decision
// (ODR-043 D3): FundamentalData is a persistence view over Fundamental.
type FundamentalData struct {
	ID           int       `json:"id"`
	TsCode       string    `json:"ts_code"`
	TradeDate    time.Time `json:"trade_date"`
	AnnDate      time.Time `json:"ann_date"` // announcement date
	EndDate      time.Time `json:"end_date"` // reporting period
	PE           *float64  `json:"pe"`
	PB           *float64  `json:"pb"`
	PS           *float64  `json:"ps"`
	ROE          *float64  `json:"roe"`
	ROA          *float64  `json:"roa"`
	DebtToEquity *float64  `json:"debt_to_equity"`
	GrossMargin  *float64  `json:"gross_margin"`
	NetMargin    *float64  `json:"net_margin"`
	Revenue      *float64  `json:"revenue"`
	NetProfit    *float64  `json:"net_profit"`
	TotalAssets  *float64  `json:"total_assets"`
	TotalLiab    *float64  `json:"total_liab"`
	CreatedAt    time.Time `json:"created_at"`
}
