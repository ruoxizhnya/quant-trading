// D7 · 市场 → 内核 路由表（配置驱动 + fail-closed）。
//
// 设计依据：蓝图（`quant-trading-docs` 仓 docs/design/data-factor-unification.md）
// §2.1「热转换语义」第 2、3 条——「切换粒度 = 按市场路由（配置驱动，零代码
// 改动）：市场维度 → 内核的映射写在配置（如 market.US → nautilus、
// market.CN → go），新市场接入 = 加一条配置 + 对应数据 connector，不改路由
// 代码」「fail-closed 语义继承：未注册的内核名/市场映射 → 显式拒绝
// （400/error），不静默回退到 Go 内核」。
//
// # 为什么放在 contracts 包
//
// 与 runspec.go 的 ValidateRunSpec 同一个理由：叶子包，API 层（cmd/analysis）
// 与将来的编排层都能引用，不引入 import 环，路由口径只有一处实现。
//
// # 与「四契约签名」的关系（D7 ④ 约束）
//
// 本类型**不改任何契约类型/签名**。市场名**不进** RunSpec —— RunSpec 是四
// 契约之一，字段集是冻结面（D7 明文：只用既有 `kernel` 字段）。市场在
// **API 边界**解析成内核名后写进既有的 `kernel` 字段，路由侧照旧只认 kernel。
//
// # 为什么「拒绝而非回退」在这是致命的
//
// 回退的代价不是报错，而是**一份用错内核算出来的、看起来完全正常的回执**
// （FR-1.3 的原话）。市场维度还要更糟一层：回退 go 内核意味着「美股请求被
// A 股规则撮合」，数字自洽、无人会察觉。
package contracts

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrMarketNotRegistered 是 ResolveKernel 对未登记市场返回的哨兵错误。
// 调用方用 errors.Is 判定，禁止字符串匹配（与 ErrKernelNotRegistered 同款）。
var ErrMarketNotRegistered = errors.New("market not registered")

// MarketRouter 把市场名映射到内核名。零值不可用（markets 为 nil 时一律
// 拒绝）——请用 NewMarketRouter 构造。
//
// 并发模型：构造只在装配期（单线程）发生；ResolveKernel / ResolveRunTarget
// 可被 HTTP handler 并发调用。markets 构造后不再写，故读侧无需加锁。
type MarketRouter struct {
	// markets：规范化（大写、去空白）市场名 → 内核名。
	markets map[string]string
}

// NewMarketRouter 构造路由表。
//
// `markets` 来自配置（`kernel_routing.markets`），键是市场名、值是内核名。
// 键值都会去掉首尾空白；键统一**大写**——viper 读配置会把键小写，而请求里
// 写的是 `US`，两侧不一致会让一条**已经配好**的映射表现为「未注册」，这是
// 最难查的一类假故障。大小写归一在这里一次做掉。
//
// **空表是合法的**，且必须合法：存量部署没有 `kernel_routing` 配置，此时
// 「不带市场」的请求（即全部存量请求）行为必须逐字节不变。带市场的请求会
// 在 ResolveKernel 处 fail-closed —— 删掉配置的效果是「带市场的请求被拒」，
// 而不是「服务起不来」，这正是破坏验证要观察到的红。
func NewMarketRouter(markets map[string]string) (*MarketRouter, error) {
	normalized := make(map[string]string, len(markets))
	conflicts := make([]string, 0)
	keys := make([]string, 0, len(markets))
	for k := range markets {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 稳定报错顺序（map 遍历随机）

	for _, rawMarket := range keys {
		kernel := strings.TrimSpace(markets[rawMarket])
		market := normalizeMarket(rawMarket)

		if market == "" {
			return nil, fmt.Errorf(
				"kernel_routing.markets: a market name is blank — every entry needs a market " +
					"(it is the key the request declares), refusing to register an unnameable route")
		}
		if kernel == "" {
			return nil, fmt.Errorf(
				"kernel_routing.markets: market %q maps to a blank kernel name — refusing "+
					"(a blank kernel would silently mean %q at routing time)",
				rawMarket, DefaultKernel)
		}
		if existing, dup := normalized[market]; dup && existing != kernel {
			// "US" → nautilus 与 "us" → go 同时出现：归一后是同一条路由的两个
			// 不同答案。挑一个就是静默回退的同义词，必须拒绝并点名。
			conflicts = append(conflicts, fmt.Sprintf("%s→%s vs %s", market, existing, kernel))
			continue
		}
		normalized[market] = kernel
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf(
			"kernel_routing.markets: conflicting routes after case normalisation: [%s] — "+
				"refusing to pick one (that would be a silent fallback)",
			strings.Join(conflicts, "; "))
	}
	return &MarketRouter{markets: normalized}, nil
}

// NewMarketRouterFromConfig 是 NewMarketRouter 的配置读取包装：nil viper 值
// （缺配置节）等价于空表。
func NewMarketRouterFromConfig(markets map[string]string) (*MarketRouter, error) {
	return NewMarketRouter(markets)
}

// normalizeMarket 把市场名归一成路由键（去空白 + 大写）。
func normalizeMarket(market string) string {
	return strings.ToUpper(strings.TrimSpace(market))
}

// Markets 返回已登记市场名的排序副本（诊断 / 测试 / 错误消息用）。
// 修改返回值不影响路由表。
func (r *MarketRouter) Markets() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.markets))
	for name := range r.markets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveKernel 按市场名解析内核名。未登记（含空名、含 nil 路由表）→ 错误，
// 且**绝不回退**到 DefaultKernel。
//
// 错误信息点名三件事：被拒的市场、当前已登记市场集合、以及「拒绝而非回退」
// 的语义 —— 与 EngineRegistry.Resolve 的错误形状刻意对齐，运维在两处看到
// 同一种句式。
func (r *MarketRouter) ResolveKernel(market string) (string, error) {
	if r == nil {
		return "", fmt.Errorf(
			"%w: market %q: no market routing table is assembled (kernel_routing.markets "+
				"is absent?) — run refused, no silent fallback to the %q kernel",
			ErrMarketNotRegistered, market, DefaultKernel)
	}
	key := normalizeMarket(market)
	if key == "" {
		return "", fmt.Errorf(
			"%w: market name is blank — refusing to guess (registered markets: [%s]); "+
				"no silent fallback to the %q kernel",
			ErrMarketNotRegistered, strings.Join(r.Markets(), ", "), DefaultKernel)
	}
	kernel, ok := r.markets[key]
	if !ok {
		registered := strings.Join(r.Markets(), ", ")
		if registered == "" {
			registered = "(none)"
		}
		return "", fmt.Errorf(
			"%w: market %q is not registered (registered markets: [%s]); run refused — "+
				"no silent fallback to the %q kernel",
			ErrMarketNotRegistered, key, registered, DefaultKernel)
	}
	return kernel, nil
}

// ResolveRunTarget 决定一次请求实际使用的内核名（市场 + 显式内核 → 内核名）。
//
// 四种组合，各自的行为都是刻意的：
//
//	market=="" && kernel==""  → ""（既有的「走默认内核」语义；默认值由
//	                              EffectiveKernel 在路由时解析，不在请求层回填）
//	market=="" && kernel!=""  → kernel（存量请求逐字节不变）
//	market!="" && kernel==""  → 路由表解析出的内核名
//	market!="" && kernel!=""  → 两者必须一致；不一致 → 错误（拒绝而非替调用方
//	                              挑一个）。调用方同时点名市场和内核却给出两套
//	                              意图，任何自动选择都是在猜。
//
// 本函数**不**校验解析出的内核是否已注册 —— 那是 EngineRegistry.Resolve 的
// 职责（单一实现），调用方拿到内核名后照常走那条 fail-closed 路由。
func ResolveRunTarget(router *MarketRouter, market, kernel string) (string, error) {
	market = strings.TrimSpace(market)
	kernel = strings.TrimSpace(kernel)
	if market == "" {
		return kernel, nil
	}
	resolved, err := router.ResolveKernel(market)
	if err != nil {
		return "", err
	}
	if kernel != "" && kernel != resolved {
		return "", fmt.Errorf(
			"run spec declares kernel %q and market %q (which routes to kernel %q) — "+
				"refusing to pick one; drop one of the two fields",
			kernel, market, resolved)
	}
	return resolved, nil
}
