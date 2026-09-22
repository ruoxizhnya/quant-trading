package backtest

// AUD-37 (ODR-065) regression tests.
//
// Background: NewEngine read the trading rules from `v.Sub("backtest")`, i.e.
// the key `backtest.trading.*`. config/analysis-service.yaml never had that
// key — it puts the block at the TOP level (`trading:`) — so config.Trading
// was always the zero value and the old all-or-nothing guard
//
//	if config.Trading.StampTaxRate == 0 {
//		config.Trading = defaultTradingConfig()
//	}
//
// replaced the whole struct with defaults. Every key in the yaml block
// happened to equal its default, so editing the yaml had no observable
// effect and the gap survived.
//
// These tests pin the four things that were wrong or could go wrong again:
//
//  1. the engine reads the top-level `trading` key (values that DIFFER from
//     the defaults, so a silent fallback cannot make the test pass);
//  2. `backtest.trading` is not a second entry for the same knobs;
//  3. a partial block fills the unset fields per field instead of zeroing
//     them (the "附带地雷" in the registration — zero is not "unset" on the
//     fee path);
//  4. every key in the real yaml block is consumed by some Go code.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
)

// tradingRulesFromYAML is the set of NON-DEFAULT values used to prove the
// engine actually reads the yaml block. Every value differs from both
// contracts.Default* and from the other values, so:
//   - a silent fallback to defaults fails the test, and
//   - a field wired to the wrong key fails the test.
var tradingRulesFromYAML = map[string]any{
	"stamp_tax_rate":        0.0009,
	"stamp_tax_rate_before": 0.002,
	"min_commission":        7.5,
	"transfer_fee_rate":     0.00003,
	"price_limit.normal":    0.09,
	"price_limit.st":        0.11,
	"price_limit.st_before": 0.06,
	"price_limit.new":       0.25,
	"new_stock_days":        45,
}

// newTradingViper returns a viper that looks like the service config: a
// `backtest:` section (NewEngine requires one — see AUD-40) plus whatever
// the caller adds.
func newTradingViper() *viper.Viper {
	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	return v
}

func viperWithTradingRules(t *testing.T) *viper.Viper {
	t.Helper()
	v := newTradingViper()
	for k, val := range tradingRulesFromYAML {
		v.Set("trading."+k, val)
	}
	return v
}

// TestNewEngine_ReadsTradingRulesFromTheTopLevelKey is the AUD-37 wiring
// proof: the engine's TradingConfig really comes from the yaml block that
// cmd/analysis also reads.
func TestNewEngine_ReadsTradingRulesFromTheTopLevelKey(t *testing.T) {
	eng, err := NewEngine(viperWithTradingRules(t), nil, zerolog.Nop())
	require.NoError(t, err)

	got := eng.config.Trading
	assert.Equal(t, 0.0009, got.StampTaxRate, "trading.stamp_tax_rate")
	assert.Equal(t, 0.002, got.StampTaxRateBefore, "trading.stamp_tax_rate_before")
	assert.Equal(t, 7.5, got.MinCommission, "trading.min_commission")
	assert.Equal(t, 0.00003, got.TransferFeeRate, "trading.transfer_fee_rate")
	assert.Equal(t, 0.09, got.PriceLimit.Normal, "trading.price_limit.normal")
	assert.Equal(t, 0.11, got.PriceLimit.ST, "trading.price_limit.st")
	assert.Equal(t, 0.06, got.PriceLimit.STBefore, "trading.price_limit.st_before")
	assert.Equal(t, 0.25, got.PriceLimit.New, "trading.price_limit.new")
	assert.Equal(t, 45, got.NewStockDays, "trading.new_stock_days")
}

// TestNewEngine_PartialTradingBlockDoesNotZeroTheRest guards the "附带地雷"
// named in the AUD-37 registration.
//
// Under the old all-or-nothing guard, a block that set only
// `trading.stamp_tax_rate` would leave MinCommission and TransferFeeRate at
// zero — and zero is NOT "unset" for those: portfolio.ComputeFees does not
// call fees.AShareFees.ApplyDefaults, so a zero minimum commission means
// "no floor" and a zero transfer fee means "no transfer fee". Both make
// fills silently cheaper and inflate backtest returns.
func TestNewEngine_PartialTradingBlockDoesNotZeroTheRest(t *testing.T) {
	v := newTradingViper()
	v.Set("trading.stamp_tax_rate", 0.0009) // the ONLY key the operator set

	eng, err := NewEngine(v, nil, zerolog.Nop())
	require.NoError(t, err)

	got := eng.config.Trading
	assert.Equal(t, 0.0009, got.StampTaxRate, "the one key that was set must survive")

	// Everything else must be the DEFAULT, not zero.
	def := contracts.DefaultTradingConfig()
	assert.Equal(t, def.MinCommission, got.MinCommission,
		"an unset min_commission must default, not become 0 (= no commission floor)")
	assert.Equal(t, def.TransferFeeRate, got.TransferFeeRate,
		"an unset transfer_fee_rate must default, not become 0 (= no transfer fee)")
	assert.Equal(t, def.StampTaxRateBefore, got.StampTaxRateBefore)
	assert.Equal(t, def.PriceLimit, got.PriceLimit)
	assert.Equal(t, def.NewStockDays, got.NewStockDays)
}

// TestNewEngine_WithoutATradingBlockUsesDefaults pins the boot path for a
// config file that has no `trading:` block at all.
func TestNewEngine_WithoutATradingBlockUsesDefaults(t *testing.T) {
	eng, err := NewEngine(newTradingViper(), nil, zerolog.Nop())
	require.NoError(t, err)

	assert.Equal(t, contracts.DefaultTradingConfig(), eng.config.Trading)
}

// TestNewEngine_BacktestTradingKeyIsNotASecondEntry is the "它不许回来" pin.
//
// `backtest.trading.*` is the path the engine used to read. Leaving it live
// alongside the top-level block would create two entries for the same knobs,
// with the top-level one always winning (it is always present in the yaml) —
// i.e. exactly the "看着有效，实际不生效" defect this task is about. If
// someone re-adds the mapstructure tag, this fails.
func TestNewEngine_BacktestTradingKeyIsNotASecondEntry(t *testing.T) {
	v := newTradingViper()
	v.Set("backtest.trading.min_commission", 99.0) // must be ignored
	v.Set("backtest.trading.stamp_tax_rate", 0.5)  // must be ignored

	eng, err := NewEngine(v, nil, zerolog.Nop())
	require.NoError(t, err)

	assert.Equal(t, contracts.DefaultMinCommission, eng.config.Trading.MinCommission,
		"backtest.trading 是已退役的路径（AUD-37）；它不许成为第二个入口")
	assert.Equal(t, contracts.DefaultStampTaxRate, eng.config.Trading.StampTaxRate)
}

// TestTradingBlockInServiceConfigHasNoDeadKeys is the structural guard.
//
// It parses the REAL config/analysis-service.yaml and checks every key under
// `trading:` against the mapstructure tags of contracts.TradingConfig. A key
// that no struct field claims is a dead key: it looks like a knob but is
// read by nobody, which is precisely how AUD-35 / AUD-36 / AUD-37 all
// survived. Nested keys are resolved through the struct type, so
// `price_limit.<typo>` is caught too, not just top-level typos.
//
// Parsing the YAML (rather than grepping the file) means the check follows
// the actual document structure — a commented-out key is not a key, and
// indentation decides nesting. See the AUD-13 / AUD-29 lesson about text
// guards.
func TestTradingBlockInServiceConfigHasNoDeadKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "analysis-service.yaml"))
	require.NoError(t, err, "the guard must read the real service config, not a fixture")

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	block, ok := doc["trading"].(map[string]any)
	require.True(t, ok, "config/analysis-service.yaml must have a top-level `trading:` mapping")

	// Keys that legitimately share the block but are NOT trading rules: they
	// are read by cmd/analysis, not by the engine. Keeping the list explicit
	// means adding a third one is a deliberate act, not an oversight.
	livePathOnly := map[string]bool{
		"emergency_token":      true, // cmd/analysis/main.go — kill-switch token
		"default_user_profile": true, // cmd/analysis/main.go — suitability profile
	}

	var walk func(prefix string, m map[string]any, rt reflect.Type)
	walk = func(prefix string, m map[string]any, rt reflect.Type) {
		for k, val := range m {
			field, found := fieldByMapstructureTag(rt, k)
			if !found {
				if prefix == "" && livePathOnly[k] {
					continue
				}
				t.Errorf("trading.%s%s is not consumed by any Go code — "+
					"either add it to contracts.TradingConfig or delete it from the yaml",
					prefix, k)
				continue
			}
			if child, isMap := val.(map[string]any); isMap {
				walk(prefix+k+".", child, structTypeOf(field.Type))
			}
		}
	}
	walk("", block, reflect.TypeOf(contracts.TradingConfig{}))
}

// fieldByMapstructureTag finds the struct field whose mapstructure tag
// matches key (first comma-separated element).
func fieldByMapstructureTag(rt reflect.Type, key string) (reflect.StructField, bool) {
	if rt.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
		if tag == key {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// structTypeOf dereferences pointers/slices so a nested mapping can be
// resolved against the element struct type.
func structTypeOf(rt reflect.Type) reflect.Type {
	for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		rt = rt.Elem()
	}
	return rt
}
