package repoguard

// D1 规范层棘轮护栏：**直连两张物理表（ohlcv_daily_qfq / ohlcv_minute）的
// 非测试 Go 文件，只允许两类**——
//
//  1. pkg/marketdata（规范层唯一读路径，Router.Bars 按频率声明透明路由）；
//  2. 本文件 directTableAccessBaseline 登记的遗留文件。
//
// 为什么存在。ADR-031 蓝图 §4 裁定「全系统数据取用只认一套规范层 schema，
// connector 之外无人感知底层表」。在按内核迁移（D2 接 connector 契约、
// 各消费方逐个切 Router）完成之前，仓库里必然还有一批直连物理表的遗留
// 路径。放任它们隐形扩散，迁移就永远做不完；一刀切全删又超出 D1 边界
// （④ 明确「不接 connector 契约、不迁移既有数据」）。棘轮是中间态的正确
// 形态：**只减不增**——
//
//   - 新文件出现物理表名字面量（SQL / DDL / 表名常量）→ 红，要么改走
//     Router，要么显式登记进基线并写理由；
//   - 基线文件被清理（不再含表名literal）→ 红，逼着删条目。
//     白名单一旦替已消失的违规打掩护，棘轮就锈死了（PITFALLS §41）。
//
// 检测口径：AST 遍历字符串字面量（go/parser 全量解析，模式 0 不含
// ParseComments），只有字面量命中才算数。注释里提表名是文档不是取数
// 路径——pkg/domain/market 的类型注释正在解释两张表的来源，误报它会
// 逼着人把文档删掉。已知边界（与 ai_boundary / package_wiring 两个护栏
// 的「保守」取向一致）：动态拼接的表名（"ohlcv_" + suffix）扫不到；
// 那是 code review 的阵地，不是结构性护栏的。
//
// 参照先例：ui 仓 A-02/A-03 的同型棘轮（基线只减不增 + 双向红）。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// physicalTables 是规范层之下、迁移完成前不允许新开直连路径的两张物理表。
// 15m 不物化（聚合产出），所以只有这两张。
var physicalTables = []string{"ohlcv_daily_qfq", "ohlcv_minute"}

// canonicalLayerDirs 是规范层读路径所在的目录（相对仓库根，slash 分隔）。
// 豁免理由是「这里就是唯一被许可感知表名的地方」——Router 的表名常量与
// 两条 SELECT 都在 pkg/marketdata/router.go。粒度到目录不到文件：更细的
// 管控属于 code review（包内新增绕过 Router 的直查），结构性护栏管到
// 「包外不许新开」这一层。
var canonicalLayerDirs = map[string]string{
	"pkg/marketdata": "D1 规范层唯一读路径：Router.Bars 按频率声明路由（1d→daily 表 / 1m→minute 表 / 15m→1m 聚合），频率与可见时间点在此显式声明",
}

// directTableAccessBaseline 登记「当前仍直连物理表」的遗留文件，每条一个
// 裁决：为什么留着、什么会接走它。文件被清理后对应条目必须删（测试会红）。
//
// 2026-10-14（D1）建账，来源：grep 两张表名 + AST 字面量复核（注释命中
// 不计：pkg/domain/market/types.go、frequency.go、cmd/dualrun/main.go 的
// 包文档提及属于文档；dualrun 另有一条诊断错误信息命中字面量，单列）。
var directTableAccessBaseline = map[string]string{
	"pkg/storage/postgres.go":    "D1 ④约束钦定的 DDL 唯一合法位置（migrationStatements 内联数组）：规范层 schema 的建表语句只加在这里，别处开新表 = 红",
	"pkg/storage/ohlcv.go":       "遗留日线存取（SaveOHLCV/GetOHLCV）；D1 回归红线对拍的另一半——router_integration_test.go 与它逐字段比对。消费方全部切 Router 后删本条",
	"pkg/storage/bulk_insert.go": "同步写入路径的表名注册表（DataType→物理表名映射）。属 connector 侧写入，D2 接 connector 契约时收编",
	"pkg/data/source/adapter.go": "DataTypeOHLCMinute 常量：同步链路的数据类型标识（写路径），非取数路径；随 D2 收编",
	"pkg/expression/registry.go": "TableOHLCV 常量：表达式层把「日线表」当数据源名引用；表达式层接规范层时收编",
	// cmd/data/handlers_stocks.go 的条目已于 2026-10-16 DATA-03/DATA-07 删除：
	// 它原来有 3 处就地 SQL（latest_date 的 MAX、每个指数两趟 close 查询），
	// 现收进 pkg/storage 的 LatestQuoteDate / LatestIndexBars，cmd 侧不再直连物理表。
	// 注意：登记里设想的路径是「切到 Router.Bars」，实际走的是「收进 storage 读方法」——
	// 护栏判据（不直连物理表）已满足，但那条**意图没有兑现**，读侧仍不经规范层 Router。
	"tools/warmcache_bench.go": "预热压测工具直查 symbol 列表；bench 工具随工具链迁移删本条",
	"cmd/dualrun/main.go":      "仅启动诊断错误信息提及 ohlcv_daily_qfq（无 SQL）。登记它不是认可取数，而是承认「这里也感知了表名」——消息文案随 dualrun 迁规范层时一并清理",
}

// TestNoNewDirectPhysicalTableAccess 是上面两条不变量的执行体。
func TestNoNewDirectPhysicalTableAccess(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	violations := map[string][]string{} // file -> 命中的表名
	stale := []string{}                 // 基线里已不再命中的文件

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .workbuddy* 是会话工作区；web/ 下有 vendored 的第三方 Go 包
			//（如 web/node_modules/flatted/golang）——都不是本仓代码。
			switch d.Name() {
			case ".git", ".workbuddy-ai", ".workbuddy", "node_modules", "web", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		// 只扫生产代码：测试按惯例豁免（集成测试自灌自证本来就要写表名）。
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		rel := fileRel(root, path)
		if rel == "" {
			return nil // 模块外
		}
		if dir, _ := filepath.Split(rel); isCanonicalLayerDir(dir) {
			return nil
		}

		hits := physicalTableLiterals(t, path)
		if len(hits) == 0 {
			return nil
		}
		if _, ok := directTableAccessBaseline[rel]; !ok {
			violations[rel] = hits
		}
		return nil
	})
	require.NoError(t, err)

	// 反向：基线文件已被清理（不存在或不再含表名 literal）→ 条目生锈，逼删。
	for rel := range directTableAccessBaseline {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, statErr := os.Stat(abs); statErr != nil {
			stale = append(stale, rel+"（文件已不存在）")
			continue
		}
		if len(physicalTableLiterals(t, abs)) == 0 {
			stale = append(stale, rel+"（已不再直连物理表）")
		}
	}
	sort.Strings(stale)

	var lines []string
	for f, hits := range violations {
		lines = append(lines, fmt.Sprintf("%s -> %s", f, strings.Join(hits, ", ")))
	}
	sort.Strings(lines)

	assert.Empty(t, lines,
		"这些生产文件新开了直连物理表的路径，绕过规范层（pkg/marketdata Router）。"+
			"要么改走 Router.Bars(ctx, freq, ...)，要么确有理由（DDL / 写入路径）就登记进 "+
			"directTableAccessBaseline 并写清「什么会接走它」——棘轮只减不增：\n%s",
		strings.Join(lines, "\n"))
	assert.Empty(t, stale,
		"directTableAccessBaseline 里这些文件已经不再直连物理表了——删掉对应条目，"+
			"白名单不许替已消失的违规打掩护：\n%s",
		strings.Join(stale, "\n"))
}

// physicalTableLiterals 返回文件中命中物理表名的字符串字面量列表。
// 注释不计：parser 模式 0 不挂注释节点，ast.Inspect 只会走到真实 literal。
func physicalTableLiterals(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	file, perr := parser.ParseFile(token.NewFileSet(), path, raw, 0)
	if perr != nil {
		return nil // 解析失败的文件由 go build / go vet 负责报错，护栏不重复
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val := lit.Value
		if unq, uerr := strconv.Unquote(val); uerr == nil {
			val = unq
		}
		for _, tbl := range physicalTables {
			if strings.Contains(val, tbl) {
				hits = append(hits, tbl)
			}
		}
		return true
	})
	return hits
}

// isCanonicalLayerDir 报告 slash 形式的目录是否属于规范层豁免范围。
func isCanonicalLayerDir(dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	for d := range canonicalLayerDirs {
		if dir == d || strings.HasPrefix(dir, d+"/") {
			return true
		}
	}
	return false
}

// fileRel 返回 path 相对 root 的 slash 形式相对路径；不在 root 之下返回 ""。
func fileRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}
