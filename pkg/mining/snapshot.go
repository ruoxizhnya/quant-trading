package mining

import (
	"fmt"
	"math"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// Snapshot 是挖掘与治理求值唯一的数据来源：**启动前预加载**好的规范化层
// 数据切片（「考前发资料」，蓝图 §7.2）。
//
// 防偷看（D8 ④ 命根子之二）在这里是结构性的，不是约定：本类型只有读方法，
// 流水线不持有 marketdata.Provider / DB 连接，「运行时查数据」没有入口。
// 于是「同快照 ⇒ 同结果」成立，复现性与可比性都由它兜底。
type Snapshot struct {
	// Fields: field → symbol → 升序日频序列（末点 = as-of 当日）。
	Fields map[string]map[string][]float64
}

// NewSnapshot 由「field → symbol → series」构造快照。
func NewSnapshot(fields map[string]map[string][]float64) *Snapshot {
	if fields == nil {
		fields = map[string]map[string][]float64{}
	}
	return &Snapshot{Fields: fields}
}

// symbols 返回快照内的全部股票代码（升序，确定性）。
func (s *Snapshot) symbols() []string {
	seen := make(map[string]bool)
	for _, bySymbol := range s.Fields {
		for sym := range bySymbol {
			seen[sym] = true
		}
	}
	out := make([]string, 0, len(seen))
	for sym := range seen {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

// provider 把快照适配成表达引擎的 DataProvider，只暴露候选声明用到的字段。
func (s *Snapshot) provider(fields []string) expression.DataProvider {
	fs := make([]string, len(fields))
	copy(fs, fields)
	sort.Strings(fs)
	return &snapshotProvider{snap: s, fields: fs, symbols: s.symbols()}
}

// snapshotProvider 是快照的只读 DataProvider 适配器。
type snapshotProvider struct {
	snap    *Snapshot
	fields  []string
	symbols []string
}

func (p *snapshotProvider) GetSymbols() []string { return p.symbols }

func (p *snapshotProvider) Fields() []string { return p.fields }

func (p *snapshotProvider) GetField(symbol, field string, _ int) ([]float64, error) {
	bySymbol, ok := p.snap.Fields[field]
	if !ok {
		// 快照没装这个字段：对该 symbol 一律 NaN。这不是 error——混源
		// universe 下「有行情没财报」是常态，缺失≠0（产出侧即缺失）。
		return []float64{math.NaN()}, nil
	}
	series, ok := bySymbol[symbol]
	if !ok {
		return []float64{math.NaN()}, nil
	}
	return series, nil
}

// describe 渲染快照规模（用于审计留痕 / 报错文案，不参与判定）。
func (s *Snapshot) describe() string {
	fields := make([]string, 0, len(s.Fields))
	for f := range s.Fields {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	return fmt.Sprintf("%d fields %v / %d symbols", len(fields), fields, len(s.symbols()))
}
