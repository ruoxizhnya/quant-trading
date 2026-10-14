package source

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// 本文件是 connector 注册契约（任务 D2，ADR-031 蓝图 §4.2）的唯一落点。
//
// 每个数据源注册时必须显式声明两件事：
//   - 时间语义（何时可见）：market.TimeSemantics；
//   - 频率口径（什么节奏产出）：market.FrequencySpec。
//
// 未声明 / 声明非法 / 与数据实际时间戳形态矛盾者，Registry.Register 一律
// fail-closed 拒收——不静默放行、不降级、不猜默认值。理由见蓝图 §4.2 红线：
// 漏声明的时间语义 = 数据泄漏，且回测曲线照样漂亮，事后极难发现。

// VisibleFieldTradeTime 是保留锚点记号：声明可见时间锚点取自
// DataItem.TradeTime。OHLCV 类数据的时间戳以 bar 时间承载在 DataItem.TradeTime
// 而非 DataSchema.Fields，故用该记号而非某个 schema 字段名。
const VisibleFieldTradeTime = "trade_time"

// VisibleFieldAnnDate 是保留锚点记号：声明可见时间锚点取自规范层
// `market.Fundamental.AnnDate`（披露日）。docs/SPEC.md 规范层 Fundamental
// 明确「披露日即可见时间点，故本类型不设 VisibleAt」——所以财报类数据的
// 锚点是披露日，而不是 DataItem.TradeTime（对财报连接器而言那是**报告期
// 截止日**，用它当锚点会把前视洞放回来）。
//
// 该锚点不落在 connector 的 wire schema 上（DataSchema.Fields 无 ann_date），
// 由规范层字段承载，故同样列为保留记号。
const VisibleFieldAnnDate = "ann_date"

// ConnectorDeclaration 是单个数据类型的注册声明。
type ConnectorDeclaration struct {
	// DataType 必须是 adapter.SupportedTypes() 的成员。
	DataType string
	// TimeSemantics 时间语义（何时可见）。必填且必须是合法枚举。
	TimeSemantics market.TimeSemantics
	// VisibleExpr 自定义时点的判定表达；TimeSemantics == VisibleCustom 时必填。
	VisibleExpr string
	// VisibleField 记录中承载可见时间锚点的字段名：DataSchema.Fields 里的
	// date/datetime 字段名，或保留记号 VisibleFieldTradeTime。必填——
	// 「声明了次日开盘可见但记录里没有可见时间可推导」即此处的形态矛盾。
	VisibleField string
	// Frequency 频率口径。必填且必须是合法枚举。
	Frequency market.FrequencySpec
}

// fail-closed 拒收的三类错误哨兵。调用方（ETL 启动路径、注册自检测试）用
// errors.Is 区分：missing = 漏声明（工程纪律问题）；invalid = 声明本身非法
// （枚举错 / 自定义缺表达式 / 重复声明）；mismatch = 声明与数据形态矛盾
// （可见锚点无法从 schema 推导）。
var (
	ErrDeclarationMissing  = errors.New("source: connector declaration missing")
	ErrDeclarationInvalid  = errors.New("source: connector declaration invalid")
	ErrDeclarationMismatch = errors.New("source: connector declaration contradicts data schema")
)

// validateDeclarations 校验 adapter 的注册契约，通过返回 nil。
//
// 校验覆盖：SupportedTypes 每个成员都有且仅有一条声明、声明不含多余类型、
// 时间语义 / 频率口径枚举合法、自定义语义附判定表达、可见时间锚点可从
// schema 推导。任一项不过 = 拒收，且错误信息点名 adapter + 数据类型 +
// 缺什么。
func validateDeclarations(a DataSourceAdapter) error {
	decls := a.Declarations()
	supported := a.SupportedTypes()

	byType := make(map[string]ConnectorDeclaration, len(decls))
	for _, d := range decls {
		if _, dup := byType[d.DataType]; dup {
			return fmt.Errorf("%w: %s declares data type %q twice", ErrDeclarationInvalid, a.Name(), d.DataType)
		}
		byType[d.DataType] = d
	}

	for _, dt := range supported {
		d, ok := byType[dt]
		if !ok {
			return fmt.Errorf("%w: %s supports %q but has no declaration (missing time semantics + frequency spec)",
				ErrDeclarationMissing, a.Name(), dt)
		}
		if err := validateDeclaration(a, d); err != nil {
			return err
		}
	}

	for _, d := range decls {
		if !containsString(supported, d.DataType) {
			return fmt.Errorf("%w: %s declares data type %q which is not in SupportedTypes()",
				ErrDeclarationInvalid, a.Name(), d.DataType)
		}
	}
	return nil
}

// validateDeclaration 校验单条声明。
func validateDeclaration(a DataSourceAdapter, d ConnectorDeclaration) error {
	if !d.TimeSemantics.Valid() {
		return fmt.Errorf("%w: %s/%s time semantics %q is not a known value (known: %v)",
			ErrDeclarationInvalid, a.Name(), d.DataType, d.TimeSemantics, market.KnownTimeSemantics())
	}
	if d.TimeSemantics.RequiresExpression() && strings.TrimSpace(d.VisibleExpr) == "" {
		return fmt.Errorf("%w: %s/%s time semantics %q requires a decision expression (VisibleExpr)",
			ErrDeclarationInvalid, a.Name(), d.DataType, d.TimeSemantics)
	}
	if !d.Frequency.Valid() {
		return fmt.Errorf("%w: %s/%s frequency spec %q is not a known value (known: %v)",
			ErrDeclarationInvalid, a.Name(), d.DataType, d.Frequency, market.KnownFrequencySpecs())
	}
	return validateVisibleAnchor(a, d)
}

// validateVisibleAnchor 校验「声明与数据形态矛盾」：声明的可见时间锚点必须
// 能从该数据类型的 schema 推导出来，否则声明形同虚设（等于没声明何时可见）。
func validateVisibleAnchor(a DataSourceAdapter, d ConnectorDeclaration) error {
	field := strings.TrimSpace(d.VisibleField)
	if field == "" {
		return fmt.Errorf("%w: %s/%s declares time semantics %q but names no visible anchor field (VisibleField)",
			ErrDeclarationMismatch, a.Name(), d.DataType, d.TimeSemantics)
	}
	// 保留锚点：规范层已定义其承载者的名字，不需要出现在 connector wire schema。
	// trade_time → DataItem.TradeTime；ann_date → market.Fundamental.AnnDate。
	if field == VisibleFieldTradeTime || field == VisibleFieldAnnDate {
		return nil
	}

	schema, err := a.Schema(d.DataType)
	if err != nil {
		return fmt.Errorf("%w: %s/%s schema unavailable for anchor check: %v",
			ErrDeclarationMismatch, a.Name(), d.DataType, err)
	}
	for _, f := range schema.Fields {
		if f.Name != field {
			continue
		}
		if f.Type != "date" && f.Type != "datetime" {
			return fmt.Errorf("%w: %s/%s visible anchor %q has type %q (want date/datetime)",
				ErrDeclarationMismatch, a.Name(), d.DataType, field, f.Type)
		}
		return nil
	}
	return fmt.Errorf("%w: %s/%s visible anchor %q not found in schema (fields: %v)",
		ErrDeclarationMismatch, a.Name(), d.DataType, field, schemaFieldNames(schema))
}

// schemaFieldNames 返回 schema 字段名，用于错误信息点名。
func schemaFieldNames(s DataSchema) []string {
	out := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		out = append(out, f.Name)
	}
	return out
}

// containsString 报告 ss 是否含 s。
func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
