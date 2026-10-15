package source

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件是 REV-05 的验收测试。Registry.Fetch 对「adapter 返回 (nil, nil)」
// 这一契约违背必须显式处理：
//  1. 不 panic；
//  2. 链继续尝试后续 adapter（健康的源仍能胜出）；
//  3. 若链上所有 adapter 都返回 (nil, nil)，最终错误可读：含 "(nil, nil)"
//     且不含损坏诊断 "%!w"。
//
// 破坏验证：删掉 registry.go 的 (nil, nil) 防御分支 → 本文件两条测试转红。

// nilResponseAdapter 是故意违背 Fetch 契约的测试替身：Fetch 恒返回 (nil, nil)。
// 它必须能像真实 adapter 一样通过 Register 的 fail-closed 契约校验，
// 否则测不到目标路径。
type nilResponseAdapter struct {
	AdapterBase
	types      []string
	fetchCalls int
}

func newNilResponseAdapter(name string, types []string) *nilResponseAdapter {
	return &nilResponseAdapter{AdapterBase: NewAdapterBase(name, true), types: types}
}

func (a *nilResponseAdapter) Type() AdapterType { return AdapterTypeHTTP }

func (a *nilResponseAdapter) SupportedTypes() []string { return a.types }

func (a *nilResponseAdapter) Declarations() []ConnectorDeclaration {
	decls := make([]ConnectorDeclaration, 0, len(a.types))
	for _, dt := range a.types {
		decls = append(decls, ConnectorDeclaration{
			DataType:      dt,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		})
	}
	return decls
}

func (a *nilResponseAdapter) Schema(string) (DataSchema, error) { return DataSchema{}, nil }

// Fetch 是刻意的契约违背：err == nil 却返回 nil 响应。
func (a *nilResponseAdapter) Fetch(context.Context, FetchRequest) (*FetchResponse, error) {
	a.fetchCalls++
	return nil, nil
}

func (a *nilResponseAdapter) HealthCheck(context.Context) error { return nil }
func (a *nilResponseAdapter) RateLimit() RateLimitConfig        { return RateLimitConfig{} }

func realtimeFetchRequest() FetchRequest {
	now := time.Now()
	return FetchRequest{DataType: DataTypeRealtime, StartDate: now, EndDate: now}
}

// 链上有 (nil, nil) 源 + 健康源：(nil, nil) 记为该源失败，链继续，健康源胜出。
func TestRegistry_Fetch_NilNilResponse_FallsThroughToNextAdapter(t *testing.T) {
	reg := NewRegistry()
	badNil := newNilResponseAdapter("badnil", []string{DataTypeRealtime})
	good := &testAdapter{
		name:      "good",
		supported: []string{DataTypeRealtime},
		items:     []DataItem{{Symbol: "600519.SH"}},
	}

	require.NoError(t, reg.Register(badNil))
	require.NoError(t, reg.Register(good))
	reg.SetChain(DataTypeRealtime, []string{"badnil", "good"})

	resp, err := reg.Fetch(context.Background(), realtimeFetchRequest())
	require.NoError(t, err, "a healthy downstream adapter must still satisfy the chain")
	require.NotNil(t, resp, "Fetch must never return a nil response with a nil error")
	assert.Equal(t, "good", resp.Source)
	assert.Equal(t, 1, badNil.fetchCalls, "the (nil, nil) adapter must have been attempted first")
}

// 链上所有 adapter 都返回 (nil, nil)：报错可读、含 "(nil, nil)"、无 "%!w"。
func TestRegistry_Fetch_AllNilNilResponses_ReadableError(t *testing.T) {
	reg := NewRegistry()
	a1 := newNilResponseAdapter("nil1", []string{DataTypeRealtime})
	a2 := newNilResponseAdapter("nil2", []string{DataTypeRealtime})
	require.NoError(t, reg.Register(a1))
	require.NoError(t, reg.Register(a2))
	reg.SetChain(DataTypeRealtime, []string{"nil1", "nil2"})

	resp, err := reg.Fetch(context.Background(), realtimeFetchRequest())
	require.Error(t, err, "an all-(nil,nil) chain must fail, not silently succeed")
	assert.Nil(t, resp, "no adapter served data, so the response must be nil")
	assert.Contains(t, err.Error(), "(nil, nil)", "error must name the contract violation")
	assert.NotContains(t, err.Error(), "%!w", "error must not corrupt the %w verb with a nil error")
	assert.False(t, strings.Contains(err.Error(), "%!"), "no fmt verb corruption in the diagnostic")
	// 两个违反者都必须被点名，诊断可定位。
	assert.Contains(t, err.Error(), "nil1")
	assert.Contains(t, err.Error(), "nil2")
}
