// AST 静态推导：warmup 与 lookback（ADR-028 §8）。
//
// 本切片只**导出**推导函数并用测试钉住，不把 warmup 接进策略 / 引擎
// （`BarHandler.Warmup()` 的接线属后续切片）。
//
// ─── 推导规则（ADR-028 §8） ──────────────────────────────────────────
//
//	warmup(AST) = 对表达式递归推导
//	            = 各**并行链** warmup 的最大值（不是相加）
//	            + 各**串行链** warmup 的累加
//
// 形式化：对函数节点 f(args…)，
//
//	warmup(f(args…)) = max_i warmup(args_i) + selfWarmup(f)
//
// 其中 selfWarmup(f) 是本算子**自身**的预热（如 ts_mean(x,N) = N−1、
// ts_rma(x,N) = N）。一元 / 二元 / 横截面算子的 selfWarmup = 0。
//
// lookback 同构（max 于并行 + 累加于串行），但多一条：**任一节点
// State=true ⇒ infinite=true**（有状态算子看无穷远的历史，K0 的 ∞ 约定）。
//
// ─── 与 ADR-028 附录 A 的对齐 ────────────────────────────────────────
//
//	ts_mean(close, 20)                      → warmup 19
//	ts_mean(close,20) + ts_rma(close,14)    → max(19,14) = 19（两条并行链）
//	ts_rma(ts_mean(close,5),14)             → 4 + 14 = 18（串行累加）
//	任一 State=true 节点                     → DeriveLookback infinite=true
package expression

// DeriveWarmup 递归推导表达式产出首个有效值所需的预热 bar 数。
//
// 并行取 max、串行累加（ADR-028 §8）。参数化算子的 selfWarmup 由注册表的
// selfWarmup 按实参推导（含 ts_ewma 的 ceil(ln(1e-6)/ln(1-α))）。
func DeriveWarmup(node Node) int {
	switch n := node.(type) {
	case nil:
		return 0
	case *LiteralNode, *IdentifierNode:
		return 0
	case *UnaryOpNode:
		return DeriveWarmup(n.Expr)
	case *BinaryOpNode:
		// 二元：两条并行链 → max。
		return max(DeriveWarmup(n.Left), DeriveWarmup(n.Right))
	case *FunctionNode:
		inner := 0
		for _, arg := range n.Args {
			if w := DeriveWarmup(arg); w > inner {
				inner = w
			}
		}
		return inner + functionSelfWarmup(n)
	case *CrossSectionalNode:
		w := DeriveWarmup(n.Expr)
		if n.Group != nil {
			if gw := DeriveWarmup(n.Group); gw > w {
				w = gw
			}
		}
		return w
	}
	return 0
}

// functionSelfWarmup 返回函数节点 f 自身的 warmup 贡献（不含实参）。未登记
// 算子 / 参数个数不符 → 0（不 panic；闸门负责合法性）。
func functionSelfWarmup(n *FunctionNode) int {
	def, ok := operatorRegistry[n.Name]
	if !ok || def.selfWarmup == nil || len(n.Args) != def.Arity {
		return 0
	}
	return def.selfWarmup(n.Args)
}

// DeriveLookback 递归推导表达式的最长回看窗口。
//
// 返回 (bars, infinite)：infinite=true 表示表达式含 State=true 的递推算子
// （如 ts_rma/ts_ewma/ts_kalman），理论上需要无穷历史（由递推状态承载）。
// bars 在 infinite=false 时是有限回看窗口。
func DeriveLookback(node Node) (bars int, infinite bool) {
	switch n := node.(type) {
	case nil:
		return 0, false
	case *LiteralNode, *IdentifierNode:
		return 0, false
	case *UnaryOpNode:
		return DeriveLookback(n.Expr)
	case *BinaryOpNode:
		lb, li := DeriveLookback(n.Left)
		rb, ri := DeriveLookback(n.Right)
		return max(lb, rb), li || ri
	case *FunctionNode:
		bars, infinite = 0, false
		for _, arg := range n.Args {
			b, i := DeriveLookback(arg)
			if b > bars {
				bars = b
			}
			infinite = infinite || i
		}
		if def, ok := operatorRegistry[n.Name]; ok {
			// 任一 State=true 节点 ⇒ ∞。
			if def.Spec.State {
				infinite = true
			}
			// 自身 lookback 串行累加（参数化算子按实参推导）。
			if def.selfLookback != nil && len(n.Args) == def.Arity {
				bars += def.selfLookback(n.Args)
			}
		}
		return bars, infinite
	case *CrossSectionalNode:
		bars, infinite = DeriveLookback(n.Expr)
		if n.Group != nil {
			gb, gi := DeriveLookback(n.Group)
			if gb > bars {
				bars = gb
			}
			infinite = infinite || gi
		}
		return bars, infinite
	}
	return 0, false
}
