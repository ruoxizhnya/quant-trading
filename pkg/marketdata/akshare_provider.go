package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/rs/zerolog"
)

type akshareProvider struct {
	pythonPath string
	scriptDir  string
	logger     zerolog.Logger
}

func NewAkShareProvider(pythonPath, scriptDir string, logger zerolog.Logger) Provider {
	if pythonPath == "" {
		pythonPath = "python3"
	}
	return &akshareProvider{
		pythonPath: pythonPath,
		scriptDir:  scriptDir,
		logger:     logger.With().Str("component", "akshare_provider").Logger(),
	}
}

func (p *akshareProvider) Name() string {
	return "akshare"
}

func (p *akshareProvider) CheckConnectivity(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, p.pythonPath, "-c", "import akshare; print(akshare.__version__)")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("akshare not available: %w, output: %s", err, string(out))
	}
	p.logger.Debug().Str("version", strings.TrimSpace(string(out))).Msg("akshare connected")
	return nil
}

func (p *akshareProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	result, err := p.runScript(ctx, "ohlcv.py", map[string]string{
		"symbol": symbol,
		"start":  start.Format("2006-01-02"),
		"end":    end.Format("2006-01-02"),
	})
	if err != nil {
		return nil, err
	}
	var bars []domain.OHLCV
	if err := json.Unmarshal(result, &bars); err != nil {
		return nil, fmt.Errorf("akshare ohlcv decode failed: %w", err)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date.Before(bars[j].Date) })
	return bars, nil
}

func (p *akshareProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	result, err := p.runScript(ctx, "fundamental.py", map[string]string{
		"symbol": symbol,
		"date":   date.Format("2006-01-02"),
	})
	if err != nil {
		return nil, err
	}
	var funds []domain.Fundamental
	if err := json.Unmarshal(result, &funds); err != nil {
		return nil, fmt.Errorf("akshare fundamental decode failed: %w", err)
	}
	if len(funds) == 0 {
		return nil, apperrors.NotFound("fundamental", symbol)
	}
	return &funds[len(funds)-1], nil
}

func (p *akshareProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	params := map[string]string{}
	if exchange != "" {
		params["exchange"] = exchange
	}
	result, err := p.runScript(ctx, "stocks.py", params)
	if err != nil {
		return nil, err
	}
	var stocks []domain.Stock
	if err := json.Unmarshal(result, &stocks); err != nil {
		return nil, fmt.Errorf("akshare stocks decode failed: %w", err)
	}
	return stocks, nil
}

func (p *akshareProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -5)
	bars, err := p.GetOHLCV(ctx, symbol, start, end)
	if err != nil {
		return 0, err
	}
	if len(bars) == 0 {
		return 0, apperrors.NotFound("OHLCV", symbol)
	}
	return bars[len(bars)-1].Close, nil
}

func (p *akshareProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	result, err := p.runScript(ctx, "index_constituents.py", map[string]string{
		"index_code": indexCode,
	})
	if err != nil {
		return nil, err
	}
	var symbols []string
	if err := json.Unmarshal(result, &symbols); err != nil {
		return nil, fmt.Errorf("akshare index decode failed: %w", err)
	}
	return symbols, nil
}

func (p *akshareProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	result, err := p.runScript(ctx, "trading_days.py", map[string]string{
		"start": start.Format("2006-01-02"),
		"end":   end.Format("2006-01-02"),
	})
	if err != nil {
		return nil, err
	}
	var dayStrs []string
	if err := json.Unmarshal(result, &dayStrs); err != nil {
		return nil, fmt.Errorf("akshare trading days decode failed: %w", err)
	}
	days := make([]time.Time, 0, len(dayStrs))
	for _, ds := range dayStrs {
		t, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		days = append(days, t)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	return days, nil
}

func (p *akshareProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	stocks, err := p.GetStocks(ctx, "")
	if err != nil {
		return domain.Stock{Symbol: symbol}, nil
	}
	for _, s := range stocks {
		if s.Symbol == symbol {
			return s, nil
		}
	}
	return domain.Stock{Symbol: symbol}, nil
}

func (p *akshareProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	data := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		bars, err := p.GetOHLCV(ctx, sym, start, end)
		if err != nil {
			p.logger.Warn().Str("symbol", sym).Err(err).Msg("BulkLoadOHLCV skip symbol")
			continue
		}
		data[sym] = bars
	}
	return data, nil
}

func (p *akshareProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	days, err := p.GetTradingDays(ctx, start, end)
	if err != nil {
		return false, err
	}
	return len(days) > 0, nil
}

func (p *akshareProvider) runScript(ctx context.Context, scriptName string, params map[string]string) ([]byte, error) {
	args := []string{}
	if p.scriptDir != "" {
		scriptPath := p.scriptDir + "/" + scriptName
		args = append(args, scriptPath)
	} else {
		args = append(args, "-c", akshareBuiltinScript(scriptName, params))
	}
	for k, v := range params {
		args = append(args, "--"+k, v)
	}

	cmd := exec.CommandContext(ctx, p.pythonPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("akshare script %s failed: %w, output: %s", scriptName, err, string(out))
	}
	return out, nil
}

func akshareBuiltinScript(name string, params map[string]string) string {
	symbol := params["symbol"]
	start := params["start"]
	end := params["end"]

	switch name {
	case "ohlcv.py":
		return fmt.Sprintf(`
import akshare as ak
import json
df = ak.stock_zh_a_hist(symbol=%q, period="daily", start_date=%q, end_date=%q, adjust="qfq")
records = []
for _, r in df.iterrows():
    records.append({"symbol":%q, "date": str(r["日期"])[:10].replace("-",""), "open": float(r["开盘"]), "high": float(r["最高"]), "low": float(r["最低"]), "close": float(r["收盘"]), "volume": float(r["成交量"]), "turnover": float(r["成交额"])})
print(json.dumps(records))
`, symbol, start, end, symbol)
	case "trading_days.py":
		return fmt.Sprintf(`
import akshare as ak
df = ak.tool_trade_date_hist_sse()
days = [str(d)[:10].replace("-","") for d in df["trade_date"] if "%s" <= str(d)[:10] <= "%s"]
print(json.dumps(days))
`, start, end)
	default:
		return `print("[]")`
	}
}
