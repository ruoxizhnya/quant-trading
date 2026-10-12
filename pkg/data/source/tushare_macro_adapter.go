package source

// TushareMacroAdapter serves the domestic macro indicator series
// (DataTypeMacro, P2-1) from the Tushare macro APIs: cn_cpi / cn_ppi /
// cn_pmi / cn_m / sf_month / shibor_lpr / fx_daily.
//
// Why a separate adapter name ("tushare_macro") instead of extending the
// market-data TushareAdapter's SupportedTypes:
//   - the macro door gets its own kill switch (DATA_DISABLE_TUSHARE_MACRO)
//     — operators can stop macro imports without touching the OHLCV chain;
//   - Registry.Register replaces same-named adapters, so a distinct name
//     keeps both doors registered side by side.
//
// It reuses the same *data.TushareClient (and therefore the same
// TUSHARE_TOKEN — 0 new keys). A nil client (token not configured)
// registers the adapter as disabled, matching the degraded-mode posture
// of buildDataSourceRegistry.

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
)

// TushareMacroAdapter wraps the TushareClient's macro import face
// (pkg/data/macro.go) as a DataSourceAdapter.
type TushareMacroAdapter struct {
	AdapterBase
	client *data.TushareClient
}

// NewTushareMacroAdapter constructs a TushareMacroAdapter. A nil client
// (TUSHARE_TOKEN not configured) yields a disabled adapter: the Registry
// skips it at Fetch time instead of failing at startup.
func NewTushareMacroAdapter(client *data.TushareClient) *TushareMacroAdapter {
	return &TushareMacroAdapter{
		AdapterBase: NewAdapterBase("tushare_macro", client != nil),
		client:      client,
	}
}

// Type implements DataSourceAdapter.Type.
func (a *TushareMacroAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (a *TushareMacroAdapter) SupportedTypes() []string {
	return []string{DataTypeMacro}
}

// Schema implements DataSourceAdapter.Schema.
func (a *TushareMacroAdapter) Schema(dataType string) (DataSchema, error) {
	if dataType != DataTypeMacro {
		return DataSchema{}, fmt.Errorf("%w: tushare_macro does not serve %s", ErrUnsupported, dataType)
	}
	return DataSchema{
		DataType: DataTypeMacro,
		Fields: []SchemaField{
			{Name: "indicator", Type: "string", Required: true},
			{Name: "freq", Type: "string", Required: true},
			{Name: "obs_date", Type: "date", Required: true},
			{Name: "value", Type: "float", Required: true},
			{Name: "unit", Type: "string", Required: false},
			{Name: "source", Type: "string", Required: true},
			{Name: "as_of", Type: "datetime", Required: true},
		},
	}, nil
}

// Fetch implements DataSourceAdapter.Fetch.
//
// Symbols carries the indicator codes (data.MacroIndicators() for the
// catalog); an empty list means "all catalog indicators". Indicators are
// fetched (and persisted) via data.TushareClient.FetchMacroIndicators,
// which groups indicators that share a Tushare API into one request —
// shibor_lpr is rate limited to 1 call/hour (measured 40203), so LPR_1Y
// and LPR_5Y simply cannot be fetched separately. The response items
// mirror the persisted rows so callers see exactly what landed in
// quant.macro_series. StartDate/EndDate are not honored in the first
// version — imports are full-history by design (idempotent upserts make
// re-imports cheap).
func (a *TushareMacroAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if req.DataType != DataTypeMacro {
		return nil, fmt.Errorf("%w: tushare_macro: %s", ErrUnsupported, req.DataType)
	}
	if a.client == nil {
		return nil, fmt.Errorf("tushare_macro adapter: nil client")
	}

	indicators := req.Symbols
	if len(indicators) == 0 {
		indicators = data.MacroIndicators()
	}
	rows, err := a.client.FetchMacroIndicators(ctx, indicators...)
	if err != nil {
		return nil, fmt.Errorf("tushare_macro fetch %v: %w", indicators, err)
	}
	items := make([]DataItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, DataItem{
			Symbol:    r.Indicator,
			TradeTime: r.ObsDate,
			Data: map[string]interface{}{
				"indicator": r.Indicator,
				"freq":      r.Freq,
				"obs_date":  r.ObsDate,
				"value":     r.Value,
				"unit":      r.Unit,
				"source":    r.Source,
				"as_of":     r.AsOf,
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
//
// CPI_YOY is the cheapest catalog probe (single monthly call, no save —
// ProbeMacro never writes). A failed probe returns ErrUpstreamUnavailable
// so the Registry can fall back.
func (a *TushareMacroAdapter) HealthCheck(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("tushare_macro adapter: nil client")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.client.ProbeMacro(probeCtx, "CPI_YOY"); err != nil {
		return fmt.Errorf("%w: tushare_macro: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (a *TushareMacroAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 200, // same ceiling as the tushare market adapter
		Burst:             10,
	}
}

// String renders a debug-friendly summary.
func (a *TushareMacroAdapter) String() string {
	return "TushareMacroAdapter{name=" + a.Name() + ", enabled=" + strconv.FormatBool(a.Enabled()) + "}"
}
