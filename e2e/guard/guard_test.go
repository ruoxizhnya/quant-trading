// Package guard contains regression guards for the e2e test suite.
// It lives in a separate package so it is NOT skipped by the
// TestMain skip-guard in e2e/tests/integration_test.go.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// target 是被守的文件（相对本包的目录）。
const target = "../tests/integration_test.go"

// parseTarget 解析目标文件。
//
// ⚠️ 为什么用 AST 而不是 strings.Contains 扫原文：
//  1. 这个文件里到处是**注释**在解释「`:8084` 是已退役的端口」「`/api/risk/
//     health` 从来不存在」—— 扫原文会把这些说明文字当成违规命中，于是要么
//     误报、要么被迫把注释写得藏头露尾。
//  2. 更要紧的是**范围**：仅凭「某个字符串在文件里出现过」证明不了「门真的
//     探了它」—— `/api/strategies` 在用例里也会出现，删掉门里的探针仍然满足
//     全文扫描。所以下面有一个 scanInside(FuncName)，把断言限制在**函数体**内。
//     护栏守的是契约，不是文件里有没有那串字。
func parseTarget(t *testing.T) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.FromSlash(target), nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", target, err)
	}
	return f
}

// collect 把一个 AST 子树里的标识符名与字符串字面量值收出来。
func collect(n ast.Node) (idents, strs map[string]bool) {
	idents = map[string]bool{}
	strs = map[string]bool{}
	ast.Inspect(n, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.Ident:
			idents[v.Name] = true
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					strs[s] = true
				}
			}
		}
		return true
	})
	return idents, strs
}

// scanInside 只收集名为 name 的函数的**函数体**。
// 第二个返回值 false 表示文件里没有这个函数。
func scanInside(t *testing.T, name string) (map[string]bool, map[string]bool, bool) {
	t.Helper()
	f := parseTarget(t)
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name {
			continue
		}
		idents, strs := collect(fd)
		return idents, strs, true
	}
	return nil, nil, false
}

// TestIntegrationTestHasSkipGuard is a regression guard for S7-P0-8
// (ODR-043-6): e2e/tests/integration_test.go must define a TestMain
// that skips when Docker services are unreachable. Without it,
// `go test ./...` fails whenever Docker Compose isn't running.
func TestIntegrationTestHasSkipGuard(t *testing.T) {
	f := parseTarget(t)
	idents, _ := collect(f)

	if !idents["TestMain"] {
		t.Error("e2e/tests/integration_test.go must define TestMain for skip-guard (S7-P0-8)")
	}
	if !idents["servicesReachable"] {
		t.Error("e2e/tests/integration_test.go must probe service reachability in TestMain (S7-P0-8)")
	}
}

// TestIntegrationTestGateIsSameSourceWithAssertions 是 AUD-52 的回归护栏。
//
// 被守的契约：**套件门必须判它下面那些用例真正需要的东西**。AUD-52 的原状
// 是门只探 /health（「服务在」），而用例要的是「服务能用」—— 于是两条用例
// 长期红得没有信息量：一条拨已退役的 execution 端口，一条打 /api/strategies
// 拿到 401。
//
// 两个正向钉（都**限定在函数体内**，否则全文里别处的同名串会让它形同虚设）
// 加一个负向钉：
//  1. probeIntegrationEnv 必须真的探那两个端点，且必须区分「端点不存在」
//     （真回归 → FAIL）与「要鉴权」（形态不匹配 → skip）。
//  2. TestMain 必须真的用上这两档判据 —— 判出来却不用，等于没判。
//  3. 退役服务的端口字面量不许回来（这是 AUD-52 的**原症状**，负向钉比正向钉
//     更能防止它悄悄复活）。
func TestIntegrationTestGateIsSameSourceWithAssertions(t *testing.T) {
	// 正向 1) 门与自己下面的断言同源
	gateIdents, gateStrs, ok := scanInside(t, "probeIntegrationEnv")
	if !ok {
		t.Fatal("找不到 probeIntegrationEnv —— 门必须独立成函数才守得住「与断言同源」（AUD-52）")
	}
	for _, s := range []string{"/api/execution/account", "/api/strategies"} {
		if !gateStrs[s] {
			t.Errorf("probeIntegrationEnv 内没有探测 %q —— 门必须判用例真正会打的端点；"+
				"只探 /health 得到的是「服务在」，不是「服务能用」（AUD-52）", s)
		}
	}
	for _, id := range []string{"executionMissing", "authRequired"} {
		if !gateIdents[id] {
			t.Errorf("probeIntegrationEnv 内缺少判据 %q —— 门必须区分「端点缺失（真回归，FAIL）」"+
				"与「需要鉴权（形态不匹配，skip）」（AUD-52）", id)
		}
	}

	// 正向 2) TestMain 真的用上了这两档判据
	mainIdents, _, ok := scanInside(t, "TestMain")
	if !ok {
		t.Fatal("找不到 TestMain（S7-P0-8）")
	}
	for _, id := range []string{"executionMissing", "authRequired", "probeIntegrationEnv"} {
		if !mainIdents[id] {
			t.Errorf("TestMain 没有用上 %q —— 判出来却不用，等于没判（AUD-52）", id)
		}
	}

	// 正向 3) 被测的执行端点必须是 analysis 上的真实路由（ODR-021）
	_, fileStrs := collect(parseTarget(t))
	if !fileStrs["/api/execution/orders"] {
		t.Error("订单持久化用例必须打 /api/execution/orders —— ODR-021 把 execution " +
			"并进 analysis 后的真实路由（AUD-52）")
	}

	// 负向：已退役服务的端口不许再出现在**任何字符串字面量**里
	var retired []string
	for s := range fileStrs {
		if strings.Contains(s, "8083") || strings.Contains(s, "8084") {
			retired = append(retired, s)
		}
	}
	if len(retired) > 0 {
		sort.Strings(retired)
		t.Errorf("出现已退役服务的端口字面量 %v —— risk / execution 已由 ODR-021 并入 "+
			"analysis(:8085)，拨它们必然 connection refused（AUD-52）", retired)
	}
	for s := range fileStrs {
		if strings.Contains(s, "/risk/health") {
			t.Errorf("字符串 %q 把 risk 当成独立服务 —— 它是 analysis 的 in-process "+
				"组件，端点在 /api/risk/*，且从来没有过 /risk/health 这个路径（ODR-021）（AUD-52）", s)
		}
	}
}
