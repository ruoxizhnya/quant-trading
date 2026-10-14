package backtest

import "strings"

// indexCodeAliases 把「人类写法」的指数标识映射到库里存的 Tushare 指数代码。
//
// DATA-01：探索链路（agent 仓）的 YAML 默认 universe 是 `csi300`，而
// index_constituents 里存的是 `000300.SH`。两边对不上时，GetIndexConstituentsByDate
// 查不到任何行 ⇒ 票池为空 ⇒ 回测 fail-loud invalid —— 看起来像「数据没灌」，
// 实际是**代码口径不一致**。
//
// 放在 core 侧归一化的理由：口径翻译属于「指数代码」这一个概念的知识，
// 放在消费方（core 引擎）一处生效，任何调用方（含 agent 与将来的 UI）传
// `csi300` 都能展开，不需要每个调用方各自记一张表。
//
// 只登记**确有数据源**的指数（三表灌数覆盖的），不认识的原样返回 ——
// 未知指数最终会展开为空并按既有语义 fail-loud，不静默猜一个。
var indexCodeAliases = map[string]string{
	"csi300": "000300.SH",
	"hs300":  "000300.SH",
	"沪深300":  "000300.SH",
	"csi500": "000905.SH",
	"zz500":  "000905.SH",
	"中证500":  "000905.SH",
	"csi800": "000906.SH",
	"zz800":  "000906.SH",
	"中证800":  "000906.SH",
	"sse50":  "000016.SH",
	"上证50":   "000016.SH",
}

// normalizeIndexCode 归一化指数代码：命中别名表则翻译，否则原样返回。
// 空串直接返回空串（调用方按「未指定指数」处理）。
func normalizeIndexCode(code string) string {
	if code == "" {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(code))
	if mapped, ok := indexCodeAliases[key]; ok {
		return mapped
	}
	return code
}
