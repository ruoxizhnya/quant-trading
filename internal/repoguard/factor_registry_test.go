package repoguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── D3 ⑤2③：回到硬编码注册表（绕过库）→ 护栏红 ───
//
// D3 把因子注册表从「代码冻结」改为「库管理」（pkg/domain 种子定义 +
// pkg/data.Registry）。本护栏钉住这个迁移**不可逆**：两类「绕回库外」
// 的形态都拦。
//
// 与 canonical_layer_test.go（物理表直连护栏）同构：AST 扫非测试生产
// 文件 + 棘轮基线（只减不增，新开=红，清理=逼删条目）。

// factorKernelMethods 是 8 个计算内核的 Go 出口方法名。它们只允许活在
// pkg/data 的定义/绑定文件里；其他任何生产文件引用（调用或当值传）都
// 意味着「绕过 registry 直调内核」。
var factorKernelMethods = []string{
	"ComputeMomentumFactor",
	"ComputeValueFactor",
	"ComputeQualityFactor",
	"ComputeGrossMarginTrendFactor",
	"ComputeContractLiabilityRatioFactor",
	"ComputeOCFToNetProfitFactor",
	"ComputeROEDuPontLeverageFactor",
	"ComputeInventoryTurnoverDeltaFactor",
}

// factorKernelAllowedFiles 登记内核方法名可以出现的生产文件（定义本体
// 与 CodeRef 绑定表）。每条写清为什么允许——白名单不许打掩护。
var factorKernelAllowedFiles = map[string]string{
	"pkg/data/factor.go":            "横截面三因子内核定义 + ComputeAllFactors（registry 驱动入口）",
	"pkg/data/factor_equitydeep.go": "纵向五因子内核定义（ADR-022 计算面）",
	"pkg/data/factor_registry.go":   "CodeRef → 内核方法绑定表（库与 Go 出口的唯一接缝）",
}

// factorRosterConstants 是冻结名册的 11 个常量名（domain.FactorType）。
// registry 之外再出现「成规模的常量引用」（>=3 个/文件）= 硬编码清单
// 气味——清单只能有一处（domain/factor_def_seed.go 的库里种子）。
var factorRosterConstants = []string{
	"FactorMomentum", "FactorValue", "FactorQuality", "FactorSize",
	"FactorVolatility", "FactorGrowth", "FactorGrossMarginTrend",
	"FactorContractLiabilityRatio", "FactorOCFToNetProfit",
	"FactorROEDuPontLeverage", "FactorInventoryTurnoverDelta",
}

// factorRosterBaseline 登记当前仍引用名册常量的生产文件及理由。
var factorRosterBaseline = map[string]string{
	"pkg/domain/factor.go":               "P2-3 假设来源清单（记录「为什么有效」，非计算注册表）",
	"pkg/domain/factor_def_seed.go":      "库里种子定义本体——唯一权威清单（Registry 的数据源）",
	"pkg/backtest/cache/factor_cache.go": "回测因子缓存预热清单（存量：预热 3 个横截面因子。待收敛为从 Registry 读取——D6 多频率视野时一并处理，届时删本条目）",
}

// factorRosterThreshold 是「清单气味」的判定阈值：单文件引用名册常量
// 达到这个数即视为在库外重建了一份清单。
const factorRosterThreshold = 3

// TestNoBypassOfFactorRegistry 拦两类绕过库的形态：
//
//	① 直调/引用计算内核方法（Compute*Factor）——绕开 registry 分派；
//	② 在库外重建因子名册清单（>=3 个名册常量聚集）——绕开种子定义。
func TestNoBypassOfFactorRegistry(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	kernelViolations := map[string][]string{} // file -> 引用的内核方法
	rosterViolations := map[string]int{}      // file -> 名册常量引用数
	staleKernel := []string{}
	staleRoster := []string{}

	kernelMethodSet := make(map[string]bool, len(factorKernelMethods))
	for _, m := range factorKernelMethods {
		kernelMethodSet[m] = true
	}
	rosterSet := make(map[string]bool, len(factorRosterConstants))
	for _, c := range factorRosterConstants {
		rosterSet[c] = true
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", ".workbuddy", "node_modules", "web", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil // 只扫生产代码
		}

		rel := fileRel(root, path)
		if rel == "" {
			return nil // 模块外
		}

		kernelHits, rosterCount := factorBypassLiterals(t, path, kernelMethodSet, rosterSet)

		if len(kernelHits) > 0 {
			if _, ok := factorKernelAllowedFiles[rel]; !ok {
				kernelViolations[rel] = kernelHits
			}
		}
		// 判据二只认「聚集」：同一 composite literal（slice/map/array
		// 字面量）内出现 >=threshold 个名册常量 = 在库外重建了一份清单。
		// 分散的多行消费（如 multi_factor.go 逐因子 reader 调用）不聚集，
		// 不是清单形态，不误伤。
		if rosterCount >= factorRosterThreshold {
			if _, ok := factorRosterBaseline[rel]; !ok {
				rosterViolations[rel] = rosterCount
			}
		}
		return nil
	})
	require.NoError(t, err)

	// 反向棘轮：基线文件已不命中 → 条目生锈，逼删。
	for rel := range factorKernelAllowedFiles {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, statErr := os.Stat(abs); statErr != nil {
			staleKernel = append(staleKernel, rel+"（文件已不存在）")
			continue
		}
		hits, _ := factorBypassLiterals(t, abs, kernelMethodSet, rosterSet)
		if len(hits) == 0 && !hasKernelMethodDef(t, abs, kernelMethodSet) {
			// 定义文件持有的是 FuncDecl 形态（func (f *FactorComputer)
			// ComputeXxxFactor(...)），不是 selector 引用——「定义或引用」
			// 有一即合法，两者皆无才生锈。
			staleKernel = append(staleKernel, rel+"（既无内核定义也无引用）")
		}
	}
	for rel := range factorRosterBaseline {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, statErr := os.Stat(abs); statErr != nil {
			staleRoster = append(staleRoster, rel+"（文件已不存在）")
			continue
		}
		_, count := factorBypassLiterals(t, abs, kernelMethodSet, rosterSet)
		if count < factorRosterThreshold {
			staleRoster = append(staleRoster, rel+"（已不再聚集名册常量）")
		}
	}
	sort.Strings(staleKernel)
	sort.Strings(staleRoster)

	var kernelLines []string
	for f, hits := range kernelViolations {
		kernelLines = append(kernelLines, fmt.Sprintf("%s -> %s", f, strings.Join(hits, ", ")))
	}
	sort.Strings(kernelLines)
	var rosterLines []string
	for f, n := range rosterViolations {
		rosterLines = append(rosterLines, fmt.Sprintf("%s -> %d 个名册常量", f, n))
	}
	sort.Strings(rosterLines)

	assert.Empty(t, kernelLines,
		"这些生产文件绕开 registry 直调/引用因子计算内核（Compute*Factor）。"+
			"计算分派只能走 FactorComputer.ComputeFactorByName / ComputeAllFactors"+
			"（registry 驱动）；确需接新的 Go 出口，在 pkg/data/factor_registry.go 的"+
			"factorCodeKernels 绑定表加一行，并登记进 factorKernelAllowedFiles：\n%s",
		strings.Join(kernelLines, "\n"))
	assert.Empty(t, rosterLines,
		"这些生产文件在库外重建了因子名册清单（>=%d 个名册常量聚集）。"+
			"因子清单的唯一权威是 pkg/domain/factor_def_seed.go 的种子定义"+
			"（Registry 数据源）；确有理由登记进 factorRosterBaseline 并写清用途：\n%s",
		factorRosterThreshold, strings.Join(rosterLines, "\n"))
	assert.Empty(t, staleKernel,
		"factorKernelAllowedFiles 里这些文件已不再引用内核方法——删掉对应条目，"+
			"白名单不许替已消失的违规打掩护：\n%s",
		strings.Join(staleKernel, "\n"))
	assert.Empty(t, staleRoster,
		"factorRosterBaseline 里这些文件已不再聚集名册常量——删掉对应条目：\n%s",
		strings.Join(staleRoster, "\n"))
}

// factorBypassLiterals 扫一个文件的 AST，返回（引用的内核方法名去重有序，
// 名册常量引用总数）。
//
// 两种形态都抓：
//   - fc.ComputeMomentumFactor(...)：SelectorExpr，Sel.Name 命中内核方法；
//   - domain.FactorMomentum / FactorMomentum：SelectorExpr（跨包）或
//     Ident（domain 包内）命中名册常量。
func factorBypassLiterals(t *testing.T, path string, kernelMethods, roster map[string]bool) ([]string, int) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var kernelHits []string
	kernelSeen := make(map[string]bool)
	rosterCount := 0

	// rosterRefsIn 数一个表达式子树里的名册常量引用（selector 与裸
	// ident 两种形态——domain 包内是裸标识符）。
	rosterRefsIn := func(n ast.Node) int {
		count := 0
		ast.Inspect(n, func(m ast.Node) bool {
			switch node := m.(type) {
			case *ast.SelectorExpr:
				if roster[node.Sel.Name] {
					count++
				}
			case *ast.Ident:
				if roster[node.Name] {
					count++
				}
			}
			return true
		})
		return count
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if kernelMethods[node.Sel.Name] && !kernelSeen[node.Sel.Name] {
				kernelSeen[node.Sel.Name] = true
				kernelHits = append(kernelHits, node.Sel.Name)
			}
		case *ast.CompositeLit:
			// 清单形态：一个字面量里聚集多个名册常量。
			if refs := rosterRefsIn(node); refs > rosterCount {
				rosterCount = refs
			}
		}
		return true
	})
	sort.Strings(kernelHits)
	return kernelHits, rosterCount
}

// hasKernelMethodDef 检查文件是否**定义**了任一内核方法（FuncDecl 形态）。
func hasKernelMethodDef(t *testing.T, path string, kernelMethods map[string]bool) bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Name != nil && kernelMethods[fn.Name.Name] {
			found = true
		}
		return true
	})
	return found
}
