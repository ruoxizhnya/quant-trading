// 注册表自洽 + 防漂移护栏（K3 切片 2 / OBS-06）。
//
//  1. 自洽：遍历注册表，每个算子都能被 parser 解析 + 过闸门 + 出现在
//     AvailableOperators()；
//  2. 反向：没有算子名常量写死在 registry.go 之外（扫本包**非 _test.go**
//     文件的字符串字面量，ts_/cs_ 前缀只允许出现在 registry.go）。
package expression

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// sampleExpr 给每个算子构造一条最小合法表达式。
var sampleExpr = map[string]string{
	// ts
	"ts_mean":       "ts_mean(close, 5)",
	"ts_std":        "ts_std(close, 5)",
	"ts_sum":        "ts_sum(close, 5)",
	"ts_max":        "ts_max(close, 5)",
	"ts_min":        "ts_min(close, 5)",
	"ts_delay":      "ts_delay(close, 1)",
	"ts_delta":      "ts_delta(close, 1)",
	"ts_pct_change": "ts_pct_change(close, 5)",
	"ts_corr":       "ts_corr(close, open, 5)",
	"ts_rank":       "ts_rank(close, 5)",
	"ts_rma":        "ts_rma(close, 14)",
	"ts_ewma":       "ts_ewma(close, 0.3)",
	"ts_kalman":     "ts_kalman(close, 0.1, 0.5)",
	// cs
	"cs_rank":       "cs_rank(close)",
	"cs_zscore":     "cs_zscore(close)",
	"cs_percentile": "cs_percentile(close)",
	"cs_neutralize": "cs_neutralize(close, sector)",
	// unary
	"neg":  "neg(close)",
	"abs":  "abs(close)",
	"log":  "log(close)",
	"sqrt": "sqrt(close)",
	"sign": "sign(close)",
	"exp":  "exp(close)",
	// binary
	"+":  "close + open",
	"-":  "close - open",
	"*":  "close * open",
	"/":  "close / open",
	"^":  "close ^ open",
	">":  "close > open",
	"<":  "close < open",
	"==": "close == open",
}

func TestRegistry_SelfConsistency(t *testing.T) {
	available := AvailableOperators()
	availSet := make(map[string]bool, len(available))
	for _, n := range available {
		availSet[n] = true
	}

	// 每个已登记算子：有样例 + 能解析 + 过闸门 + 在可用集合里。
	for name := range operatorRegistry {
		expr, ok := sampleExpr[name]
		if !ok {
			t.Errorf("算子 %q 缺测试样例（注册表新增算子时同步 sampleExpr）", name)
			continue
		}
		if !availSet[name] {
			t.Errorf("算子 %q 未出现在 AvailableOperators()", name)
		}
		parsed, err := NewParser().Parse(expr)
		if err != nil {
			t.Errorf("样例 %q（算子 %q）解析失败: %v", expr, name, err)
			continue
		}
		if err := parsed.Validate(); err != nil {
			t.Errorf("样例 %q（算子 %q）未过闸门: %v", expr, name, err)
		}
	}

	// 反向：样例表里没有注册表外的幽灵条目。
	for name := range sampleExpr {
		if _, ok := operatorRegistry[name]; !ok {
			t.Errorf("样例表含未登记算子 %q（幽灵条目）", name)
		}
	}
}

// TestNoOperatorNameLiteralsOutsideRegistry 是防漂移反向护栏：本包
// （registry.go 之外、非 _test.go）的字符串字面量里，**不得**出现以
// "ts_" / "cs_" 开头的算子名 —— 名字集合的单一事实源只能是 registry.go。
//
// 参照 internal/repoguard 的做法（go/ast 扫源码，而非手写清单）。
func TestNoOperatorNameLiteralsOutsideRegistry(t *testing.T) {
	fset := gotoken.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录失败: %v", err)
	}

	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		// 只护栏**生产代码**：测试文件里的表达式字符串是合法的（它们正是
		// 用来驱动 parser 的输入）。
		if strings.HasSuffix(name, "_test.go") || name == "registry.go" {
			continue
		}
		scanned++
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != gotoken.STRING {
				return true
			}
			v, uerr := strconv.Unquote(bl.Value)
			if uerr != nil {
				return true
			}
			if strings.HasPrefix(v, "ts_") || strings.HasPrefix(v, "cs_") {
				t.Errorf("%s:%d：算子名字面量 %q 出现在 registry.go 之外 —— 名字集合只能有一处（registry.go）",
					name, fset.Position(bl.Pos()).Line, v)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何生产代码文件，护栏形同虚设")
	}
}

// TestAvailableOperators_SortedAndComplete 钉住导出集合的基本性质。
func TestAvailableOperators_SortedAndComplete(t *testing.T) {
	got := AvailableOperators()
	if len(got) != len(operatorRegistry) {
		t.Fatalf("AvailableOperators() 数量 %d != 注册表 %d", len(got), len(operatorRegistry))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("AvailableOperators() 未升序：%q >= %q", got[i-1], got[i])
		}
	}
}
