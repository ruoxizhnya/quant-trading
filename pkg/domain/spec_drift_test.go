package domain

// 防漂移测试（D1 验收⑤1 的机械判据）：docs/SPEC.md「规范层 Schema」节的
// 三类字段表与频率词表，和 Go 类型反射比对——**双向**：SPEC 写了 Go 没有
// 的字段 = 红（文档在描述不存在的东西）；Go 有 SPEC 没写的字段 = 红
// （代码悄悄长了，文档没跟上）。表格用 `<!-- drift:... -->` 锚点标记，
// 改锚点名会比对失败并提示——那是故意的：锚点是契约的一部分。
//
// 为什么值得一个测试：规范层的全部意义在于「一套 schema 所有人认」。
// 字段表靠手抄 = 必然漂移（PITFALLS §41 同款逻辑：手写清单会在你加包的
// 那天生锈）。反射比对把「SPEC 与代码一致」从纪律变成机器判据。

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specPath 相对本测试文件（pkg/domain/）的 SPEC 位置。
const specPath = "../../docs/SPEC.md"

// specTableRows 提取 `<!-- drift:name -->` 与 `<!-- /drift:name -->` 之间
// 的 markdown 表格行，返回按单元格拆分的二维数组（已去空单元格、跳过
// 表头与分隔行）。锚点不存在 = 测试失败（契约被改名/删除，必须显式处理）。
func specTableRows(t *testing.T, anchor string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	require.NoError(t, err, "读取 %s 失败——测试必须在仓库内运行", specPath)

	openMarker := "<!-- drift:" + anchor + " -->"
	closeMarker := "<!-- /drift:" + anchor + " -->"
	body := string(raw)
	start := strings.Index(body, openMarker)
	require.GreaterOrEqual(t, start, 0,
		"SPEC.md 找不到锚点 %s ——「规范层 Schema」节的表格锚点是防漂移契约，"+
			"改名/删除必须同步改本测试，不许静默跳过", openMarker)
	rest := body[start+len(openMarker):]
	end := strings.Index(rest, closeMarker)
	require.GreaterOrEqual(t, end, 0, "锚点 %s 没有闭合标记 %s", openMarker, closeMarker)
	rest = rest[:end]

	var rows [][]string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 || cells[0] == "" {
			continue
		}
		// 分隔行（|---|）与表头行（| 字段 | ... |）都不是数据。
		if strings.Trim(cells[0], "-: ") == "" || cells[0] == "字段" || cells[0] == "频率" {
			continue
		}
		rows = append(rows, cells)
	}
	require.NotEmpty(t, rows, "锚点 %s 之间没有解析到表格数据行", anchor)
	return rows
}

// fieldSpec 是 SPEC 表格里一行字段契约。
type fieldSpec struct {
	name    string // Go 字段名
	jsonTag string // json tag 名（不含 ,omitempty 等选项）
}

// parseFieldSpecs 解析字段表（第 0 列 = Go 字段名，第 2 列 = json tag）。
func parseFieldSpecs(t *testing.T, anchor string) []fieldSpec {
	t.Helper()
	rows := specTableRows(t, anchor)
	out := make([]fieldSpec, 0, len(rows))
	for _, r := range rows {
		require.GreaterOrEqual(t, len(r), 3, "锚点 %s 的行 %v 列数不足 3", anchor, r)
		out = append(out, fieldSpec{name: r[0], jsonTag: strings.Split(r[2], ",")[0]})
	}
	return out
}

// goFieldSpecs 反射提取结构体的 (字段名, json tag 名) 集合。
func goFieldSpecs(v any) []fieldSpec {
	typ := reflect.TypeOf(v)
	out := make([]fieldSpec, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			name = f.Name // 无 tag / 显式跳过序列化的字段：以字段名参与比对
		}
		out = append(out, fieldSpec{name: f.Name, jsonTag: name})
	}
	return out
}

// diffFieldSpecs 双向比对：返回「SPEC 有 Go 无（含 tag 不一致）」与
// 「Go 有 SPEC 无」的清单。
func diffFieldSpecs(spec, goSpecs []fieldSpec) (specOnly, goOnly []string) {
	goSet := map[string]string{}
	for _, g := range goSpecs {
		goSet[g.name] = g.jsonTag
	}
	specSet := map[string]string{}
	for _, s := range spec {
		specSet[s.name] = s.jsonTag
	}
	for _, s := range spec {
		if tag, ok := goSet[s.name]; !ok {
			specOnly = append(specOnly, s.name)
		} else if tag != s.jsonTag {
			specOnly = append(specOnly, s.name+"（json tag：SPEC="+s.jsonTag+" Go="+tag+"）")
		}
	}
	for _, g := range goSpecs {
		if _, ok := specSet[g.name]; !ok {
			goOnly = append(goOnly, g.name)
		}
	}
	return specOnly, goOnly
}

// assertNoDrift 执行双向比对并输出可执行的失败信息。
func assertNoDrift(t *testing.T, typeName, anchor string, goType any) {
	t.Helper()
	spec := parseFieldSpecs(t, anchor)
	specOnly, goOnly := diffFieldSpecs(spec, goFieldSpecs(goType))
	assert.Empty(t, specOnly,
		"%s：SPEC.md 锚点 %s 里的这些字段在 Go 类型中不存在（或 json tag 不一致）——"+
			"文档在描述不存在的东西，改代码或改 SPEC，然后同步另一侧：%v", typeName, anchor, specOnly)
	assert.Empty(t, goOnly,
		"%s：Go 类型有这些字段，但 SPEC.md 锚点 %s 没登记——代码悄悄长了，"+
			"把字段补进 SPEC 表（字段名 + json tag 必须逐字一致）：%v", typeName, anchor, goOnly)
}

// parseFactorDefSpecs 解析因子定义骨架表（D3）。列序与三张字段表不同：
// 定义契约的关键列是 json tag，在第 1 列（第 2 列是含义散文）。
func parseFactorDefSpecs(t *testing.T, anchor string) []fieldSpec {
	t.Helper()
	rows := specTableRows(t, anchor)
	out := make([]fieldSpec, 0, len(rows))
	for _, r := range rows {
		require.GreaterOrEqual(t, len(r), 2, "锚点 %s 的行 %v 列数不足 2", anchor, r)
		out = append(out, fieldSpec{name: r[0], jsonTag: strings.Split(r[1], ",")[0]})
	}
	return out
}

// TestSpecDriftFactorDefinition 定义骨架双向比对（D3）：SPEC.md 锚点
// factor_definition 与 domain.FactorDefinition 的 (字段名, json tag)
// 必须逐字一致——定义 schema 是库里公式的契约，漂移即数值回归破裂。
func TestSpecDriftFactorDefinition(t *testing.T) {
	spec := parseFactorDefSpecs(t, "factor_definition")
	specOnly, goOnly := diffFieldSpecs(spec, goFieldSpecs(FactorDefinition{}))
	assert.Empty(t, specOnly,
		"domain.FactorDefinition：SPEC.md 锚点 factor_definition 里的这些字段在 Go 类型中"+
			"不存在（或 json tag 不一致）——文档在描述不存在的定义字段：%v", specOnly)
	assert.Empty(t, goOnly,
		"domain.FactorDefinition：Go 类型有这些字段，但 SPEC.md 锚点 factor_definition "+
			"没登记——定义 schema 悄悄长了，把字段补进 SPEC 表：%v", goOnly)
}

// TestSpecDriftFactorRegistration 登记记录骨架双向比对（D4）：SPEC.md 锚点
// factor_registration 与 domain.FactorRegistration 的 (字段名, json tag)
// 必须逐字一致——登记 schema 是治理账本的契约（谁/何时/改了什么/为什么
// 靠这八列留痕），漂移即治理留痕与复现链断裂。
func TestSpecDriftFactorRegistration(t *testing.T) {
	spec := parseFactorDefSpecs(t, "factor_registration")
	specOnly, goOnly := diffFieldSpecs(spec, goFieldSpecs(FactorRegistration{}))
	assert.Empty(t, specOnly,
		"domain.FactorRegistration：SPEC.md 锚点 factor_registration 里的这些字段在 Go 类型中"+
			"不存在（或 json tag 不一致）——文档在描述不存在的登记字段：%v", specOnly)
	assert.Empty(t, goOnly,
		"domain.FactorRegistration：Go 类型有这些字段，但 SPEC.md 锚点 factor_registration "+
			"没登记——登记 schema 悄悄长了，把字段补进 SPEC 表：%v", goOnly)
}

// TestSpecDriftFactorMiningRecord 挖掘留痕骨架双向比对（D8）：SPEC.md 锚点
// factor_mining_record 与 domain.FactorMiningRecord 的 (字段名, json tag)
// 必须逐字一致——留痕 schema 是治理审计的契约（来源/哈希/验证记录靠这些列
// 可追溯），漂移即「凭什么进库」这条链断裂。
func TestSpecDriftFactorMiningRecord(t *testing.T) {
	spec := parseFactorDefSpecs(t, "factor_mining_record")
	specOnly, goOnly := diffFieldSpecs(spec, goFieldSpecs(FactorMiningRecord{}))
	assert.Empty(t, specOnly,
		"domain.FactorMiningRecord：SPEC.md 锚点 factor_mining_record 里的这些字段在 Go 类型中"+
			"不存在（或 json tag 不一致）——文档在描述不存在的留痕字段：%v", specOnly)
	assert.Empty(t, goOnly,
		"domain.FactorMiningRecord：Go 类型有这些字段，但 SPEC.md 锚点 factor_mining_record "+
			"没登记——留痕 schema 悄悄长了，把字段补进 SPEC 表：%v", goOnly)
}

func TestSpecDriftOHLCV(t *testing.T) {
	assertNoDrift(t, "market.OHLCV", "ohlcv", market.OHLCV{})
}

func TestSpecDriftFundamental(t *testing.T) {
	assertNoDrift(t, "market.Fundamental", "fundamental", market.Fundamental{})
}

func TestSpecDriftFactorOutput(t *testing.T) {
	assertNoDrift(t, "domain.FactorOutput", "factor_output", FactorOutput{})
}

// TestSpecDriftMarketActivity 是 D5 新数据类的防漂移锚点：SPEC「规范层
// Schema」节的市场活跃度字段表 ↔ domain.MarketActivity 反射双向比对。
func TestSpecDriftMarketActivity(t *testing.T) {
	assertNoDrift(t, "domain.MarketActivity", "activity", MarketActivity{})
}

// TestSpecDriftFrequencyVocabulary 频率词表双向比对：SPEC 表格第 0 列是
// 字面量（"1m"/"15m"/"1d"），第 1 列是 Go 常量名。常量名一并核对——
// 光对字面量抓不住「Go 常量改名」。
func TestSpecDriftFrequencyVocabulary(t *testing.T) {
	rows := specTableRows(t, "frequency")

	goByLiteral := map[string]market.Frequency{}
	for _, f := range market.KnownFrequencies() {
		goByLiteral[string(f)] = f
	}
	goConstantNames := map[string]string{
		"market.Freq1m":  string(market.Freq1m),
		"market.Freq15m": string(market.Freq15m),
		"market.Freq1d":  string(market.Freq1d),
	}

	var specOnly []string
	for _, r := range rows {
		require.GreaterOrEqual(t, len(r), 2, "频率表行 %v 列数不足 2", r)
		literal, constName := r[0], r[1]
		if _, ok := goByLiteral[literal]; !ok {
			specOnly = append(specOnly, literal+"（Go 词表没有这个频率）")
			continue
		}
		if want, ok := goConstantNames[constName]; !ok {
			specOnly = append(specOnly, literal+"（常量名 "+constName+" 在 Go 侧不存在——改名不同步）")
		} else if want != literal {
			specOnly = append(specOnly, literal+"（"+constName+" 的字面量是 "+want+"，与表不一致）")
		}
	}
	var goOnly []string
	specLiterals := map[string]bool{}
	for _, r := range rows {
		specLiterals[r[0]] = true
	}
	for _, f := range market.KnownFrequencies() {
		if !specLiterals[string(f)] {
			goOnly = append(goOnly, string(f))
		}
	}

	assert.Empty(t, specOnly,
		"SPEC.md 频率词表与 Go 不一致——扩展频率必须「常量 + 路由分支 + 聚合口径 + SPEC」"+
			"四件套同步：%v", specOnly)
	assert.Empty(t, goOnly,
		"market.KnownFrequencies() 有这些频率，SPEC.md 频率词表没登记：%v", goOnly)
}
