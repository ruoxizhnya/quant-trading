package live

import (
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execution"
)

// PaperCost 是一笔 paper 市价单在共享执行成本核下的输出。
//
// 只含两个量：
//   - FillPrice  ：施加滑点/冲击后的成交价（与回测 ExecuteOrder 的
//     trade.Price 逐位可比）；
//   - Commission ：执行佣金 = max(FillPrice*Qty*CommissionRate, MinCommission)
//     （与回测 ExecuteOrder 的 trade.Commission 逐位可比）。
//
// 注意 **边界**：Commission 只是「执行佣金」，不含 A 股印花税与过户费。
// 回测撮合服务只建模佣金；paper 侧 MockTrader 会在佣金之外再补印花税
// （卖出）+ 过户费（双边）——那两项是**结算成本**，两侧共同已知的口径
// 边界，不是成本公式漂移。成本同构要证的是「滑点/冲击 + 佣金」这一执行
// 内核两边同一段代码，故本类型只承载这两项。
type PaperCost struct {
	FillPrice  float64
	Commission float64
}

// ComputePaperCost 是 paper 侧执行成本的**唯一入口**。
//
// 它不自己写任何公式，直接委托 pkg/execution.NewCostModel —— 与回测
// pkg/backtest/execution.BacktestExecutionService 调用的**是同一个
// CostModel**。因此「paper 与回测对同一 (order, refPrice, adv, config)
// 算出的成交价与佣金」在数值上必然相等（容差 0）。
//
// 参数语义：
//   - execCfg  ：执行配置（SlippageModel / CommissionRate / MinCommission /
//     ImpactSigma / ImpactLiquidityFactor）。用 domain.ExecutionConfig 保证
//     paper 与回测**同一份配置类型**，不会有「这边的模型名那边不认」的错配。
//   - order    ：市价单（OrderTypeMarket）。quantity<=0 返回 error。
//   - refPrice ：参考价（回测/paper 均为当根 bar 的 Close）。
//   - high/low ：当根 bar 的高低价（"variable" 模型用）。
//   - adv      ：平均日成交量代理（"impact" 模型用；本切片 = 当根 bar 的
//     Volume，与回测 K4 的 ADV 代理口径一致）。
func ComputePaperCost(
	execCfg domain.ExecutionConfig,
	order domain.Order,
	refPrice, high, low, adv float64,
) (PaperCost, error) {
	fillPrice, commission, err := execution.NewCostModel(execCfg).ExecuteMarket(order, refPrice, high, low, adv)
	if err != nil {
		return PaperCost{}, err
	}
	return PaperCost{FillPrice: fillPrice, Commission: commission}, nil
}
