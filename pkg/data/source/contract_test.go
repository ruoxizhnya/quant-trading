package source

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件是 connector 注册契约（D2）的验收测试：
//   - 负例表驱动：未声明 / 声明非法 / 与形态矛盾 三类拒收逐条钉死；
//   - 正例：全部存量 adapter 的声明完整且注册成功；
//   - 注册原子性：被拒的 adapter 不污染 Registry 状态。

// mockAdapter / testAdapter 是既有测试替身，这里补上它们的声明方法
// （接口新增方法后必须实现，否则测试包整体编译失败）。用保留锚点
// trade_time，使既有注册路径在 fail-closed 校验下仍然通过。
func (m *mockAdapter) Declarations() []ConnectorDeclaration {
	decls := make([]ConnectorDeclaration, 0, len(m.supported))
	for _, dt := range m.supported {
		decls = append(decls, ConnectorDeclaration{
			DataType:      dt,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		})
	}
	return decls
}

func (a *testAdapter) Declarations() []ConnectorDeclaration {
	decls := make([]ConnectorDeclaration, 0, len(a.supported))
	for _, dt := range a.supported {
		decls = append(decls, ConnectorDeclaration{
			DataType:      dt,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		})
	}
	return decls
}

// declAdapter 是声明可控的测试替身，用于负例。
type declAdapter struct {
	AdapterBase
	types  []string
	fields map[string][]SchemaField
	decls  []ConnectorDeclaration
}

func (a *declAdapter) Type() AdapterType                    { return AdapterTypeHTTP }
func (a *declAdapter) SupportedTypes() []string             { return a.types }
func (a *declAdapter) Declarations() []ConnectorDeclaration { return a.decls }
func (a *declAdapter) Schema(dt string) (DataSchema, error) {
	if f, ok := a.fields[dt]; ok {
		return DataSchema{DataType: dt, Fields: f}, nil
	}
	return DataSchema{}, fmt.Errorf("%w: declAdapter does not serve %s", ErrUnsupported, dt)
}
func (a *declAdapter) Fetch(context.Context, FetchRequest) (*FetchResponse, error) {
	return &FetchResponse{}, nil
}
func (a *declAdapter) HealthCheck(context.Context) error { return nil }
func (a *declAdapter) RateLimit() RateLimitConfig        { return RateLimitConfig{} }

// validDecl 是合法声明模板（after_close 锚点取自保留记号）。
func validDecl(dt string) ConnectorDeclaration {
	return ConnectorDeclaration{
		DataType:      dt,
		TimeSemantics: market.VisibleAfterClose,
		VisibleField:  VisibleFieldTradeTime,
		Frequency:     market.FreqSpecDaily,
	}
}

// TestValidateDeclarations_Rejection 覆盖 fail-closed 的三类拒收及其细分。
func TestValidateDeclarations_Rejection(t *testing.T) {
	t.Run("missing_declaration", func(t *testing.T) {
		a := &declAdapter{
			AdapterBase: NewAdapterBase("missing", true),
			types:       []string{"ohlcv_daily"},
			fields:      map[string][]SchemaField{},
			decls:       nil,
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationMissing)
		assert.Contains(t, err.Error(), "ohlcv_daily")
	})

	t.Run("duplicate_declaration", func(t *testing.T) {
		a := &declAdapter{
			AdapterBase: NewAdapterBase("dup", true),
			types:       []string{"ohlcv_daily"},
			fields:      map[string][]SchemaField{},
			decls:       []ConnectorDeclaration{validDecl("ohlcv_daily"), validDecl("ohlcv_daily")},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationInvalid)
	})

	t.Run("declaration_for_unsupported_type", func(t *testing.T) {
		a := &declAdapter{
			AdapterBase: NewAdapterBase("extra", true),
			types:       []string{"ohlcv_daily"},
			fields:      map[string][]SchemaField{},
			decls:       []ConnectorDeclaration{validDecl("ohlcv_daily"), validDecl("bogus_type")},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationInvalid)
		assert.Contains(t, err.Error(), "bogus_type")
	})

	t.Run("illegal_time_semantics", func(t *testing.T) {
		d := validDecl("ohlcv_daily")
		d.TimeSemantics = "intraday_visible" // 盘中即可见：词表外的非法值
		a := &declAdapter{
			AdapterBase: NewAdapterBase("badsem", true),
			types:       []string{"ohlcv_daily"},
			fields:      map[string][]SchemaField{},
			decls:       []ConnectorDeclaration{d},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationInvalid)
		assert.Contains(t, err.Error(), "intraday_visible")
	})

	t.Run("custom_without_expression", func(t *testing.T) {
		d := validDecl("realtime")
		d.TimeSemantics = market.VisibleCustom
		d.VisibleExpr = "" // 自定义时点必须给判定表达
		a := &declAdapter{
			AdapterBase: NewAdapterBase("noexpr", true),
			types:       []string{"realtime"},
			fields:      map[string][]SchemaField{},
			decls:       []ConnectorDeclaration{d},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationInvalid)
		assert.Contains(t, err.Error(), "VisibleExpr")
	})

	t.Run("illegal_frequency_spec", func(t *testing.T) {
		d := validDecl("ohlcv_daily")
		d.Frequency = "hourly" // 词表外
		a := &declAdapter{
			AdapterBase: NewAdapterBase("badfreq", true),
			types:       []string{"ohlcv_daily"},
			fields:      map[string][]SchemaField{},
			decls:       []ConnectorDeclaration{d},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationInvalid)
		assert.Contains(t, err.Error(), "hourly")
	})

	t.Run("no_visible_anchor", func(t *testing.T) {
		d := validDecl("announce")
		d.VisibleField = "" // 声明了语义却指不出可见时间锚点
		a := &declAdapter{
			AdapterBase: NewAdapterBase("noanchor", true),
			types:       []string{"announce"},
			fields:      map[string][]SchemaField{"announce": {{Name: "ann_time", Type: "datetime"}}},
			decls:       []ConnectorDeclaration{d},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationMismatch)
		assert.Contains(t, err.Error(), "VisibleField")
	})

	t.Run("anchor_not_in_schema", func(t *testing.T) {
		d := validDecl("announce")
		d.TimeSemantics = market.VisibleNextOpen
		d.VisibleField = "nonexistent_field" // 形态矛盾：schema 里没有这个字段
		a := &declAdapter{
			AdapterBase: NewAdapterBase("ghost", true),
			types:       []string{"announce"},
			fields:      map[string][]SchemaField{"announce": {{Name: "ann_time", Type: "datetime"}}},
			decls:       []ConnectorDeclaration{d},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationMismatch)
		assert.Contains(t, err.Error(), "nonexistent_field")
	})

	t.Run("anchor_wrong_type", func(t *testing.T) {
		d := validDecl("announce")
		d.VisibleField = "ann_title" // 锚点必须是 date/datetime，string 不能承载可见时间
		a := &declAdapter{
			AdapterBase: NewAdapterBase("wrongtype", true),
			types:       []string{"announce"},
			fields:      map[string][]SchemaField{"announce": {{Name: "ann_title", Type: "string"}}},
			decls:       []ConnectorDeclaration{d},
		}
		err := validateDeclarations(a)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDeclarationMismatch)
		assert.Contains(t, err.Error(), "ann_title")
	})

	t.Run("valid_passes", func(t *testing.T) {
		a := &declAdapter{
			AdapterBase: NewAdapterBase("ok", true),
			types:       []string{"announce"},
			fields:      map[string][]SchemaField{"announce": {{Name: "ann_time", Type: "datetime"}}},
			decls: []ConnectorDeclaration{{
				DataType:      "announce",
				TimeSemantics: market.VisibleOnAnnouncementDay,
				VisibleField:  "ann_time",
				Frequency:     market.FreqSpecEvent,
			}},
		}
		require.NoError(t, validateDeclarations(a))
	})

	// 财报类数据的披露日锚点在规范层（market.Fundamental.AnnDate），不在
	// connector wire schema 上——器 schema 里没有 ann_date 也必须放行，
	// 否则 tushare 财报 connector 会因「锚点不在 schema」被误拒。
	t.Run("reserved_ann_date_anchor_passes", func(t *testing.T) {
		a := &declAdapter{
			AdapterBase: NewAdapterBase("fund", true),
			types:       []string{"fundamentals"},
			fields: map[string][]SchemaField{
				"fundamentals": {{Name: "pe", Type: "float"}},
			},
			decls: []ConnectorDeclaration{{
				DataType:      "fundamentals",
				TimeSemantics: market.VisibleOnAnnouncementDay,
				VisibleField:  VisibleFieldAnnDate,
				Frequency:     market.FreqSpecEvent,
			}},
		}
		require.NoError(t, validateDeclarations(a))
	})
}

// TestRegistryRejectsInvalidDeclarations 断言被拒的 adapter 不污染 Registry：
// 注册失败后 Registry 里不能残留该 adapter 或其链路。
func TestRegistryRejectsInvalidDeclarations(t *testing.T) {
	r := NewRegistry()
	a := &declAdapter{
		AdapterBase: NewAdapterBase("bad", true),
		types:       []string{"ohlcv_daily"},
		fields:      map[string][]SchemaField{},
		decls:       nil, // 未声明 → 拒收
	}
	err := r.Register(a)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeclarationMissing)
	assert.Empty(t, r.ListAdapters(), "被拒的 adapter 不得进入 Registry")
}

// allConcreteAdapters 返回全部存量 adapter（与 TestInterfaceCompliance 对齐，
// 另加 TushareMacroAdapter）。
func allConcreteAdapters() []DataSourceAdapter {
	return []DataSourceAdapter{
		NewTushareAdapter(nil),
		NewMootdxAdapter(nil),
		NewEastmoneyAdapter(NewEastmoneyClient()),
		NewEastmoneySectorsAdapter(NewEastmoneyClient()),
		NewEastmoneyTopListAdapter(NewEastmoneyClient()),
		NewJuchaoAdapter(),
		NewXueqiuAdapter(),
		NewAlphaVantageAdapter(""),
		NewYahooFinanceAdapter(),
		NewTushareMacroAdapter(nil),
	}
}

// TestAllExistingConnectorsDeclareEverySupportedType 是「全部存量 connector
// 带声明注册成功」的验收：每个 adapter 的 SupportedTypes 都有且仅有一条声明，
// 且整批注册进同一个 Registry 无错。
//
// 破坏验证（人工腿）：删掉任一 adapter 的某条声明 → 本测试红；
// 把某条「收盘后可见」改成词表外的「盘中即可见」→ 本测试红。
func TestAllExistingConnectorsDeclareEverySupportedType(t *testing.T) {
	adapters := allConcreteAdapters()
	require.Len(t, adapters, 10)

	r := NewRegistry()
	names := make([]string, 0, len(adapters))
	for _, a := range adapters {
		require.NoErrorf(t, validateDeclarations(a),
			"adapter %s 的注册声明未通过 fail-closed 校验", a.Name())

		declared := make(map[string]int, len(a.Declarations()))
		for _, d := range a.Declarations() {
			declared[d.DataType]++
		}
		for _, dt := range a.SupportedTypes() {
			assert.Equalf(t, 1, declared[dt],
				"adapter %s 的 %q 声明数应为 1（0 = 漏声明，>1 = 重复）", a.Name(), dt)
		}

		require.NoErrorf(t, r.Register(a), "adapter %s 注册失败", a.Name())
		names = append(names, a.Name())
	}
	for _, n := range names {
		assert.NotNilf(t, r.GetAdapter(n), "adapter %s 未落到 Registry", n)
	}
}

// TestDeclarationsErrorIsWrapped 钉住：错误信息点名 adapter + 数据类型 +
// 缺什么（可运维），并与哨兵可 errors.Is 判定。
func TestDeclarationsErrorIsWrapped(t *testing.T) {
	a := &declAdapter{
		AdapterBase: NewAdapterBase("named", true),
		types:       []string{"fundamentals"},
		fields:      map[string][]SchemaField{},
		decls:       nil,
	}
	err := validateDeclarations(a)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDeclarationMissing))
	assert.Contains(t, err.Error(), "named")
	assert.Contains(t, err.Error(), "fundamentals")
}
