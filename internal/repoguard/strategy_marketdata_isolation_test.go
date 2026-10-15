package repoguard

// D6 红线护栏：**策略包（pkg/strategy）不许 import 规范层取数包
// （pkg/marketdata）**。
//
// 红线原文（ADR-031 蓝图 §4.3，D6 ②）：策略运行时禁止直接查
// marketdata.Provider；所有数据（日线 / 日内分钟）回测启动前快照预加载，
// 经契约注入（FactorZScoreReader / FundamentalsProvider /
// IntradayFeatureProvider）。为什么必须结构性执法而不是写在文档里：
// 泄漏**极难事后发现**（回测曲线照样漂亮，D2 契约注释同款教训）——
// 一个能摸到 Provider 的策略实现，就等于能在回放循环里现查未来数据。
//
// 执法口径（AST，同 canonical_layer_test.go 的扫描模式）：
//   - 扫描范围：pkg/strategy 全部子包的**生产** Go 文件（测试豁免：
//     测试替身本来就要实现 marketdata.Provider 之类的接口）；
//   - 判据：import 路径含本仓 module 的 pkg/marketdata；
//   - 命中即红，报错点名文件与 import 行，并说明唯一合法通道
//     （IntradayFeatureProvider 契约注入）。
//
// 与 canonical_layer 护栏的分工：那个管「不许直连物理表」（数据从哪来），
// 本护栏管「策略运行时不许摸取数包」（数据怎么到策略手里）。一个守
// 下游、一个守出口，中间的快照预加载是唯一允许的桥。

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strategyDir 是被隔离的策略包根目录（相对仓库根，slash 分隔）。
const strategyDir = "pkg/strategy"

// TestStrategyPackage_CannotImportMarketdata 执行上面那条不变量。
func TestStrategyPackage_CannotImportMarketdata(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	strategyRoot := filepath.Join(root, filepath.FromSlash(strategyDir))

	violations := []string{} // "file -> import path"

	err := filepath.WalkDir(strategyRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		// 只扫生产代码（测试豁免：替身实现 Provider 接口是测试惯例）。
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel := fileRel(root, path)
		if rel == "" {
			return nil // 模块外
		}
		for _, imp := range marketdataImports(t, path) {
			violations = append(violations, rel+" -> "+imp)
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(violations)
	assert.Empty(t, violations,
		"D6 红线：pkg/strategy 的生产文件不许 import pkg/marketdata —— "+
			"策略运行时禁止直接查 marketdata.Provider（蓝图 §4.3）。"+
			"日内/日线数据一律走启动前快照 + 契约注入（strategy.IntradayFeatureProvider "+
			"等）。若确有新合法通道需求，先改本护栏并写清理由，而不是就地绕过：\n%s",
		strings.Join(violations, "\n"))
}

// marketdataImports 返回文件中 import 了 pkg/marketdata 的路径列表。
// 解析失败返回 nil（由 go build / go vet 负责报错，护栏不重复）。
func marketdataImports(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	var hits []string
	for _, spec := range file.Imports {
		val := strings.Trim(spec.Path.Value, `"`)
		// 本仓 module 下的规范层取数包。判定用路径后缀而非全等：
		// vendored / replace 场景下 module 前缀可能不同，包身份由
		// "pkg/marketdata" 这一段承载。
		if val == "pkg/marketdata" || strings.HasSuffix(val, "/pkg/marketdata") {
			hits = append(hits, val)
		}
	}
	return hits
}
