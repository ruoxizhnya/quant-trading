package main

// ohlc_units_guard_test.go —— DATA-05 的单位声明守卫。
//
// 守什么：**已启用**的 A 股行情源，其 volume / amount / turnover 字段必须声明单位。
//
// 为什么要这条规则（DATA-04 的根因形状）：mootdx 的 schema 曾写着
// `volume Unit: "share"`，而 fetchBars 把 transport 原值直接塞进 DataItem ——
// 同步边界一个换算点都没有。声明与实现分叉时不会有人报错，只会让下游全体偏
// 100 倍（volume）/ 1000 倍（turnover），而偏出来的数字看起来仍然像行情。
//
// 今天的实测状态（2026-10-14 DATA-05 核对，三处独立）：
//   · registry_init.go 以 nil transport 注册 mootdx ⇒ Enabled()=false ⇒ 规则够不着它，
//     它的 schema 因此把 Unit 如实留空（pkg/data/source/mootdx_adapter.go）；
//   · eastmoney 只服务 capital_flow，Schema(ohlcv_daily) 直接 ErrUnsupported；
//   · go.mod 里没有 go-mootdx SDK；ETLPipeline（Registry.Fetch 的唯一生产调用方）
//     零调用方 ⇒ 日线 fallback 链整条不可达（已登记 docs/TASKS.md）。
//
// 接线 mootdx 的那一刻它变成 Enabled，本规则立刻开始管它：要么先实测单位 +
// 在同步边界换算到规范层 + 补上声明，要么红。规则的牙由
// TestUnitRuleFlagsEnabledAdapterWithoutUnit 用替身当场证明，不靠「将来会有人踩」；
// global_ohlcv 不在此列（境外市场，另一套单位来历），其口径见 docs/SPEC.md。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/data/source"
	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// marketUnitFields 是必须回答「单位是什么」的行情字段。
var marketUnitFields = map[string]bool{"volume": true, "amount": true, "turnover": true}

// aShareMarketDataTypes 是本规则覆盖的 A 股 bar / 快照类型。
var aShareMarketDataTypes = map[string]bool{
	source.DataTypeOHLCDaily:  true,
	source.DataTypeOHLCMinute: true,
	source.DataTypeRealtime:   true,
}

// undeclaredMarketUnits 返回「已启用的行情源没能把单位声明齐全」的清单。元素一律
// 是 `源名/类型.字段` 形状：缺单位时字段就是 volume/amount/turnover，取不到 schema
// 时是 `schema(...)` —— 后者同样是无从判定单位（声称服务该类型却没给出字段表），
// 所以并列进来，但调用方的报错文案必须同时涵盖这两种。清单为空即合规。
func undeclaredMarketUnits(adapters []source.DataSourceAdapter) []string {
	var bad []string
	for _, a := range adapters {
		if !a.Enabled() {
			continue // 未接线的源不逼它声明 —— 声明必须是实测的结果，空口声明正是本单要防的
		}
		for _, dt := range a.SupportedTypes() {
			if !aShareMarketDataTypes[dt] {
				continue
			}
			schema, err := a.Schema(dt)
			if err != nil {
				bad = append(bad, a.Name()+"/"+dt+".schema("+err.Error()+")")
				continue
			}
			for _, f := range schema.Fields {
				if marketUnitFields[f.Name] && f.Unit == "" {
					bad = append(bad, a.Name()+"/"+dt+"."+f.Name)
				}
			}
		}
	}
	return bad
}

func TestProductionRegistryMarketUnitsDeclared(t *testing.T) {
	t.Parallel()
	reg := buildDataSourceRegistry(nil, zerolog.Nop())

	names := reg.ListAdapters()
	require.NotEmpty(t, names, "生产 registry 一个 adapter 都没有 —— 装配已坏，本守卫失去检查对象")

	adapters := make([]source.DataSourceAdapter, 0, len(names))
	for _, n := range names {
		a := reg.GetAdapter(n)
		require.NotNil(t, a, "registry 列出了 %s 却取不到实例", n)
		adapters = append(adapters, a)
	}

	if bad := undeclaredMarketUnits(adapters); len(bad) > 0 {
		t.Errorf("已启用的行情源没能把 volume/amount/turnover 的单位声明齐全（或拿不出字段表）：%v —— "+
			"DATA-05 的规矩是「先实测再声明」：取一次真实响应核对量级，在同步边界"+
			"换算到规范层（volume=股 / turnover=CNY，docs/SPEC.md），然后才把 Unit 填上", bad)
	}
}

// unitStubAdapter 是规则的替身：已启用、服务 ohlcv_daily（可由 supported 改），
// volume / amount 的单位由用例决定。除 Schema 外的方法都是最小实现（本测试不碰 Fetch）。
type unitStubAdapter struct {
	source.AdapterBase
	volumeUnit string
	amountUnit string
	supported  []string
}

func (s *unitStubAdapter) Type() source.AdapterType { return source.AdapterTypeSDK }
func (s *unitStubAdapter) SupportedTypes() []string {
	if len(s.supported) > 0 {
		return s.supported
	}
	return []string{source.DataTypeOHLCDaily}
}
func (s *unitStubAdapter) Declarations() []source.ConnectorDeclaration {
	return []source.ConnectorDeclaration{{
		DataType:      source.DataTypeOHLCDaily,
		TimeSemantics: market.VisibleAfterClose,
		VisibleField:  source.VisibleFieldTradeTime,
		Frequency:     market.FreqSpecDaily,
	}}
}
func (s *unitStubAdapter) Schema(dataType string) (source.DataSchema, error) {
	if dataType != source.DataTypeOHLCDaily {
		return source.DataSchema{}, source.ErrUnsupported
	}
	return source.DataSchema{
		DataType: dataType,
		Fields: []source.SchemaField{
			{Name: "close", Type: "float", Required: true, Unit: "yuan"},
			{Name: "volume", Type: "float", Required: true, Unit: s.volumeUnit},
			{Name: "amount", Type: "float", Required: false, Unit: s.amountUnit},
		},
	}, nil
}
func (s *unitStubAdapter) Fetch(context.Context, source.FetchRequest) (*source.FetchResponse, error) {
	return &source.FetchResponse{Source: s.Name(), FetchedAt: time.Now().UTC()}, nil
}
func (s *unitStubAdapter) HealthCheck(context.Context) error { return nil }
func (s *unitStubAdapter) RateLimit() source.RateLimitConfig { return source.RateLimitConfig{} }

// TestUnitRuleFlagsEnabledAdapterWithoutUnit 证明规则真的咬人，四条腿各管一条边界：
// 缺单位 ⇒ 被抓；单位齐全 ⇒ 放行；未启用 ⇒ 不在射程；拿不出字段表 ⇒ 也被抓。
// 少了这一组，TestProductionRegistryMarketUnitsDeclared 今天绿得可能只是因为没东西可查。
func TestUnitRuleFlagsEnabledAdapterWithoutUnit(t *testing.T) {
	t.Parallel()

	undeclared := &unitStubAdapter{
		AdapterBase: source.NewAdapterBase("stub-undeclared", true),
		volumeUnit:  "",
		amountUnit:  "",
	}
	got := undeclaredMarketUnits([]source.DataSourceAdapter{undeclared})
	require.Equal(t,
		[]string{"stub-undeclared/ohlcv_daily.volume", "stub-undeclared/ohlcv_daily.amount"}, got,
		"已启用却没声明单位的行情源必须被规则抓到")

	declared := &unitStubAdapter{
		AdapterBase: source.NewAdapterBase("stub-declared", true),
		volumeUnit:  "share",
		amountUnit:  "yuan",
	}
	require.Empty(t, undeclaredMarketUnits([]source.DataSourceAdapter{declared}),
		"单位齐全的同款替身必须放行 —— 否则规则不是「缺声明」的判据，而是别的什么")

	// 未接线的源不在射程内（今天的 mootdx 就是这个形状）。
	disabled := &unitStubAdapter{
		AdapterBase: source.NewAdapterBase("stub-disabled", false),
	}
	require.Empty(t, undeclaredMarketUnits([]source.DataSourceAdapter{disabled}),
		"Enabled()=false 的源不该被要求声明未实测的单位")

	// 反证腿：声称服务行情类型却拿不出字段表，同样是「单位无从判定」，
	// 必须并列抓到 —— 否则「Schema 取不到」这条路是空转的，规则只在字段表齐全时才生效。
	noSchema := &unitStubAdapter{
		AdapterBase: source.NewAdapterBase("stub-noschema", true),
		supported:   []string{source.DataTypeOHLCMinute},
	}
	gotNS := undeclaredMarketUnits([]source.DataSourceAdapter{noSchema})
	require.Len(t, gotNS, 1, "拿不出 ohlcv_minute 字段表的已启用源必须被记一条：%v", gotNS)
	require.True(t, strings.HasPrefix(gotNS[0], "stub-noschema/ohlcv_minute.schema("),
		"违规项要保持 `源名/类型.字段` 形状（调用方的报错文案按这个形状说话），got=%v", gotNS)
}
