package source

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// MootdxTransport is the interface that the mootdx adapter uses to talk
// to the underlying mootdx library. It is a thin shim so the adapter can
// be unit-tested without a live TCP connection.
//
// The transport is responsible for:
//   - Encoding requests into the mootdx binary protocol
//   - Decoding responses into the structs below
//   - Connection pooling / reconnection (handled in the SDK)
//
// In production, the transport wraps github.com/qmaru/go-mootdx or
// similar. The adapter itself does not import the SDK to keep the
// dependency footprint small and the test surface clean.
type MootdxTransport interface {
	// GetSecurityQuotes returns 5-tick snapshots for the given symbols.
	// The market parameter is 0 (SZ) or 1 (SH).
	GetSecurityQuotes(ctx context.Context, market int, symbols []string) ([]MootdxQuote, error)

	// GetSecurityBars returns K-line bars for a single symbol/category.
	// category: 4=daily, 7=1min, 8=5min, 9=15min, 10=30min, 11=60min.
	// count: number of bars to fetch.
	GetSecurityBars(ctx context.Context, market int, symbol string, category, count int) ([]MootdxBar, error)

	// GetSecurityTransaction returns tick-by-tick transactions.
	// date: YYYYMMDD format.
	GetSecurityTransaction(ctx context.Context, market int, symbol, date string) ([]MootdxTransaction, error)

	// GetFinanceSnapshot returns 37-field financial snapshot.
	GetFinanceSnapshot(ctx context.Context, market int, symbol string) (*MootdxFinanceSnapshot, error)

	// Ping verifies the TCP connection is alive.
	Ping(ctx context.Context) error
}

// MootdxQuote represents a single tick snapshot with 5-level depth.
type MootdxQuote struct {
	Symbol    string  // 6-digit code (e.g. "600519")
	Price     float64 // latest price
	Open      float64
	High      float64
	Low       float64
	LastClose float64
	Volume    int64
	Amount    float64
	// 5-level bid/ask
	Bid1 float64
	Bid1Vol int32
	Bid2 float64
	Bid2Vol int32
	Bid3 float64
	Bid3Vol int32
	Bid4 float64
	Bid4Vol int32
	Bid5 float64
	Bid5Vol int32
	Ask1 float64
	Ask1Vol int32
	Ask2 float64
	Ask2Vol int32
	Ask3 float64
	Ask3Vol int32
	Ask4 float64
	Ask4Vol int32
	Ask5 float64
	Ask5Vol int32
	ServerTime time.Time
}

// MootdxBar represents a single K-line bar.
type MootdxBar struct {
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
	Amount float64
	Date   time.Time
}

// MootdxTransaction represents a single tick transaction.
type MootdxTransaction struct {
	Time     time.Time
	Price    float64
	Volume   int64
	Num      int32   // batch count
	BuyOrSell int32  // 0=buy, 1=sell, 2=neutral
}

// MootdxFinanceSnapshot is the 37-field quarterly financial snapshot.
type MootdxFinanceSnapshot struct {
	Symbol      string
	ReportDate  time.Time
	EPS         float64 // 每股收益
	BVPS        float64 // 每股净资产
	ROE         float64 // 净资产收益率
	NetProfit   float64 // 净利润
	Income      float64 // 主营收入
	TotalShares float64 // 总股本
	CircShares  float64 // 流通股本
}

// MootdxAdapter implements DataSourceAdapter over the mootdx SDK.
//
// Supported data types:
//   - DataTypeRealtime: 5-level snapshot
//   - DataTypeOHLCMinute: 1/5/15/30/60-minute bars
//   - DataTypeOHLCDaily: daily bars (mootdx provides them as a side-effect
//     of the bars endpoint with category=4)
//
// Notes:
//   - mootdx does not provide PE/PB/news — those go through other
//     adapters. The Schema method reflects that.
type MootdxAdapter struct {
	AdapterBase
	transport MootdxTransport
}

// NewMootdxAdapter constructs a MootdxAdapter. The transport must be
// initialized; pass nil only in unit tests that don't exercise Fetch.
func NewMootdxAdapter(transport MootdxTransport) *MootdxAdapter {
	return &MootdxAdapter{
		AdapterBase: NewAdapterBase("mootdx", transport != nil),
		transport:   transport,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *MootdxAdapter) Type() AdapterType { return AdapterTypeSDK }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *MootdxAdapter) SupportedTypes() []string {
	return []string{
		DataTypeRealtime,
		DataTypeOHLCMinute,
		DataTypeOHLCDaily,
	}
}

// Schema implements DataSourceAdapter.Schema.
func (a *MootdxAdapter) Schema(dataType string) (DataSchema, error) {
	switch dataType {
	case DataTypeRealtime:
		return DataSchema{
			DataType: DataTypeRealtime,
			Fields: []SchemaField{
				{Name: "price", Type: "float", Required: true, Unit: "yuan"},
				{Name: "open", Type: "float", Required: false, Unit: "yuan"},
				{Name: "high", Type: "float", Required: false, Unit: "yuan"},
				{Name: "low", Type: "float", Required: false, Unit: "yuan"},
				{Name: "last_close", Type: "float", Required: false, Unit: "yuan"},
				{Name: "volume", Type: "int", Required: false, Unit: "share"},
				{Name: "amount", Type: "float", Required: false, Unit: "yuan"},
				{Name: "bid1", Type: "float", Required: false, Unit: "yuan"},
				{Name: "ask1", Type: "float", Required: false, Unit: "yuan"},
				{Name: "bid1_vol", Type: "int", Required: false},
				{Name: "ask1_vol", Type: "int", Required: false},
			},
		}, nil
	case DataTypeOHLCMinute, DataTypeOHLCDaily:
		return DataSchema{
			DataType: dataType,
			Fields: []SchemaField{
				{Name: "open", Type: "float", Required: true, Unit: "yuan"},
				{Name: "high", Type: "float", Required: true, Unit: "yuan"},
				{Name: "low", Type: "float", Required: true, Unit: "yuan"},
				{Name: "close", Type: "float", Required: true, Unit: "yuan"},
				{Name: "volume", Type: "float", Required: true, Unit: "share"},
				{Name: "amount", Type: "float", Required: false, Unit: "yuan"},
			},
		}, nil
	default:
		return DataSchema{}, fmt.Errorf("%w: mootdx does not serve %s", ErrUnsupported, dataType)
	}
}

// Fetch implements DataSourceAdapter.Fetch.
//
// For DataTypeRealtime:
//   - Symbols required; each becomes a 5-tick snapshot.
//   - Time window ignored (we always fetch the current snapshot).
//
// For DataTypeOHLCMinute / DataTypeOHLCDaily:
//   - Single symbol per request (mootdx limitation).
//   - Period controls the bar size; default = 1min (7) or daily (4).
func (a *MootdxAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if a.transport == nil {
		return nil, fmt.Errorf("mootdx adapter: nil transport")
	}
	switch req.DataType {
	case DataTypeRealtime:
		return a.fetchRealtime(ctx, req)
	case DataTypeOHLCMinute:
		return a.fetchBars(ctx, req, 7) // default: 1-minute
	case DataTypeOHLCDaily:
		return a.fetchBars(ctx, req, 4) // 4 = daily
	default:
		return nil, fmt.Errorf("%w: mootdx: %s", ErrUnsupported, req.DataType)
	}
}

func (a *MootdxAdapter) fetchRealtime(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if len(req.Symbols) == 0 {
		return nil, fmt.Errorf("mootdx realtime: symbols required")
	}
	// CR-17 (ODR-012): the previous implementation called
	// `transport.GetSecurityQuotes` once per symbol (N serial round-trips).
	// For a 500-symbol real-time snapshot that meant 500 TCP handshakes to
	// the mootdx server — typically 5-10s. Group symbols by market and issue
	// a single GetSecurityQuotes call per market. The transport signature
	// already accepts `[]string{codes}`; this change just stops the per-symbol
	// loop from happening.
	byMarket := make(map[int][]string, 2)
	codeToSymbol := make(map[string]string, len(req.Symbols))
	for _, sym := range req.Symbols {
		market, code, err := splitMarketSymbol(sym)
		if err != nil {
			return nil, fmt.Errorf("mootdx: %w", err)
		}
		byMarket[market] = append(byMarket[market], code)
		codeToSymbol[code] = sym
	}

	items := make([]DataItem, 0, len(req.Symbols))
	for market, codes := range byMarket {
		quotes, err := a.transport.GetSecurityQuotes(ctx, market, codes)
		if err != nil {
			return nil, fmt.Errorf("mootdx quotes: %w", err)
		}
		for _, q := range quotes {
			// Map the quote back to its full symbol via the code. The
			// transport returns quotes for the codes we sent, so a missing
			// code means mootdx dropped it (delisted/suspended).
			sym, ok := codeToSymbol[q.Symbol]
			if !ok {
				continue
			}
			items = append(items, DataItem{
				Symbol:    sym,
				TradeTime: q.ServerTime,
				Data:      quoteToMap(q),
			})
		}
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (a *MootdxAdapter) fetchBars(ctx context.Context, req FetchRequest, defaultCategory int) (*FetchResponse, error) {
	if len(req.Symbols) != 1 {
		return nil, fmt.Errorf("mootdx bars: exactly 1 symbol required, got %d", len(req.Symbols))
	}
	sym := req.Symbols[0]
	market, code, err := splitMarketSymbol(sym)
	if err != nil {
		return nil, fmt.Errorf("mootdx: %w", err)
	}
	category := defaultCategory
	if req.Period != "" {
		if c, ok := parseMootdxCategory(req.Period); ok {
			category = c
		}
	}
	count := barCountFromWindow(req.StartDate, req.EndDate, defaultCategory)
	bars, err := a.transport.GetSecurityBars(ctx, market, code, category, count)
	if err != nil {
		return nil, fmt.Errorf("mootdx bars %s: %w", sym, err)
	}
	items := make([]DataItem, 0, len(bars))
	for _, b := range bars {
		items = append(items, DataItem{
			Symbol:    sym,
			TradeTime: b.Date,
			Data: map[string]interface{}{
				"open":   b.Open,
				"high":   b.High,
				"low":    b.Low,
				"close":  b.Close,
				"volume": b.Volume,
				"amount": b.Amount,
			},
		})
	}
	return &FetchResponse{
		Source:    a.Name(),
		Items:     items,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
// mootdx is a TCP service; the transport exposes a Ping.
func (a *MootdxAdapter) HealthCheck(ctx context.Context) error {
	if a.transport == nil {
		return fmt.Errorf("mootdx adapter: nil transport")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := a.transport.Ping(probeCtx); err != nil {
		return fmt.Errorf("%w: mootdx: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
// mootdx has no documented rate limit; we still cap at the SDK's
// comfortable ceiling to avoid tripping server-side counters.
func (a *MootdxAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 600,
		Burst:             50,
	}
}

// String renders a debug-friendly summary.
func (a *MootdxAdapter) String() string {
	return "MootdxAdapter{name=" + a.Name() + ", enabled=" + strconv.FormatBool(a.Enabled()) + "}"
}

// splitMarketSymbol parses a project-canonical symbol (e.g. "600519.SH")
// into mootdx market+code (1, "600519"). Returns an error for unknown
// conventions.
func splitMarketSymbol(sym string) (int, string, error) {
	if len(sym) < 3 {
		return 0, "", fmt.Errorf("invalid symbol %q", sym)
	}
	switch sym[len(sym)-2:] {
	case "SH":
		return 1, sym[:len(sym)-3], nil
	case "SZ":
		return 0, sym[:len(sym)-3], nil
	default:
		// Heuristic: SH starts with 6 or 9 (Shanghai), others are SZ.
		switch sym[0] {
		case '6', '9':
			return 1, sym, nil
		default:
			return 0, sym, nil
		}
	}
}

// parseMootdxCategory maps the Period string to a mootdx category code.
// Supports: "1m"/"5m"/"15m"/"30m"/"60m"/"1d"/"daily".
func parseMootdxCategory(p string) (int, bool) {
	switch p {
	case "1m", "1min", "1":
		return 7, true
	case "5m", "5min", "5":
		return 8, true
	case "15m", "15min", "15":
		return 9, true
	case "30m", "30min", "30":
		return 10, true
	case "60m", "60min", "60":
		return 11, true
	case "1d", "daily", "d":
		return 4, true
	}
	return 0, false
}

// barCountFromWindow estimates the bar count from the time window. We
// return 800 (mootdx max) for daily and 240 (trading-day minutes) for
// intraday; finer estimation isn't worth the complexity at this stage.
func barCountFromWindow(start, end time.Time, category int) int {
	if category == 4 {
		// Daily: at most a few years of history
		return 800
	}
	// Intraday: 240 minutes per day
	return 240 * 5
}

// quoteToMap flattens a MootdxQuote into the Data map shape used by
// storage.BulkInsert. Field names match the realtime_quote table.
func quoteToMap(q MootdxQuote) map[string]interface{} {
	return map[string]interface{}{
		"price":      q.Price,
		"open":       q.Open,
		"high":       q.High,
		"low":        q.Low,
		"last_close": q.LastClose,
		"volume":     q.Volume,
		"amount":     q.Amount,
		"bid1":       q.Bid1,
		"bid1_vol":   q.Bid1Vol,
		"ask1":       q.Ask1,
		"ask1_vol":   q.Ask1Vol,
		"bid2":       q.Bid2,
		"bid2_vol":   q.Bid2Vol,
		"ask2":       q.Ask2,
		"ask2_vol":   q.Ask2Vol,
		"bid3":       q.Bid3,
		"bid3_vol":   q.Bid3Vol,
		"ask3":       q.Ask3,
		"ask3_vol":   q.Ask3Vol,
		"bid4":       q.Bid4,
		"bid4_vol":   q.Bid4Vol,
		"ask4":       q.Ask4,
		"ask4_vol":   q.Ask4Vol,
		"bid5":       q.Bid5,
		"bid5_vol":   q.Bid5Vol,
		"ask5":       q.Ask5,
		"ask5_vol":   q.Ask5Vol,
	}
}
