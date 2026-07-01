# S7-P3-1: Expression Engine Expansion (Signal / Position / Risk Layers + ExpressionStrategy Adapter)

> **Task ID**: S7-P3-1 (ODR-043 Sprint 7, P3)
> **Branch**: `fix/s7-p3-1-cs-neutralize-2arg` (Phase 1) → extend for Phases 2-7
> **Workflow**: AGENTS.md §8.3 — test-first (red/green), code review, atomic commit per phase
> **Plan Status**: Ready for execution

---

## Summary

Expand the factor-only expression DSL (`pkg/ai/expression/`) into a complete strategy-authoring stack by:

1. **Fixing the `cs_neutralize` 3-layer bug** (declared in AST, parser rejects 2-arg form, evaluator has no impl) — this unblocks LLM-emitted neutralization formulas.
2. Adding a **signal layer** that maps a DSL comparison expression → buy/sell signals.
3. Adding a **position-sizing layer** (config-driven, not DSL) for equal-weight / strength-proportional / risk-parity.
4. Adding a **risk-control layer** (config-driven) for max-position / max-drawdown guards.
5. Adding an **OHLCVDataProvider** adapter so the existing evaluator can consume `map[string][]domain.OHLCV` (the strategy `GenerateSignals` input shape).
6. Adding an **ExpressionStrategy** adapter that plugs the expression stack into the `strategy.Strategy` composite interface and self-registers via `init()`.
7. Syncing docs (SPEC.md, ARCHITECTURE.md, TASKS.md, AGENTS.md).

**Key design decisions (approved):**
- `CrossSectionalNode` gains an optional `Group Node` field (not a new node type) — backward compatible.
- `applyCrossSectionalOp` signature changes to `(op string, values, group []float64) []float64`; group is `nil` for 1-arg cs ops.
- Signal layer reuses existing `> < ==` DSL operators (no new parser features needed).
- Position sizing / risk are **config-driven** for MVP (not DSL formulas) — keeps parser surface stable.
- New package `pkg/strategy/expression/` (not in `pkg/ai/`) — strategy orchestration belongs with strategies.
- Inline ~30 LOC of parse helpers (copied from `plugins/utils.go`) into the new package to avoid an import cycle (`plugins/utils.go` is unexported and lives in a leaf package).

---

## Current State Analysis

### The `cs_neutralize` bug (3 layers broken)

| Layer | File | Current State | Bug |
|-------|------|---------------|-----|
| **AST** | `pkg/ai/expression/ast.go:166-174` | `IsCrossSectionalOp` lists `cs_neutralize` | Declared but `CrossSectionalNode` (line 89-92) has no `Group` field — only `Op` + `Expr` |
| **Parser** | `pkg/ai/expression/parser.go:317-323` | `parseFunctionCall` enforces `len(args) != 1` for ALL cs ops | Rejects the 2-arg form the LLM emits → parse error |
| **Evaluator** | `pkg/ai/expression/evaluator.go:322-333` | `applyCrossSectionalOp` switch has no `cs_neutralize` case | Falls through to `default: return values` (no-op, silently wrong) |
| **LLM prompt** | `pkg/ai/prompts/factor_research.txt:36` | Advertises `cs_neutralize(x, group)` | Contract mismatch — any LLM emission fails at parse time |

### Existing strategy plugin pattern (reference for ExpressionStrategy)

From `pkg/strategy/plugins/momentum.go`:
- Embed `*strategy.BaseStrategy` → gets Name/Description/Configure(no-op)/Cleanup(no-op)/Parameters(empty) for free
- Override `Configure()` with typed validation using `parseIntParam` / `validateIntRange` etc. (unexported in `plugins/utils.go`)
- Override `GenerateSignals()` + `Weight()` + `Cleanup()` + `Parameters()`
- Self-register: `func init() { strategy.GlobalRegister(s) }`

### Compliance test registration

`pkg/strategy/interfaces_compliance_test.go:44-45` blank-imports `examples` + `plugins` to fire their `init()`. After Phase 6, must add:
```go
_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/expression"
```

---

## Proposed Changes (7 Phases)

### Phase 1: Fix `cs_neutralize` 2-arg bug

**Test file (TDD red first)**: `pkg/ai/expression/cs_neutralize_test.go` (new)

~13 tests covering:
- 2-arg parse success: `cs_neutralize(close, sector)` → `CrossSectionalNode{Op, Expr, Group}` with Group non-nil
- 1-arg parse error: `cs_neutralize(close)` → error "requires exactly 2 arguments"
- 3-arg parse error: `cs_neutralize(close, sector, market)` → error
- 1-arg `cs_rank` regression: `cs_rank(close)` still parses (Group == nil)
- Evaluator: group-mean subtraction (`[1,2,3,4]` with group `[A,A,B,B]` → `[-0.5, 0.5, -0.5, 0.5]`)
- Evaluator: single-group (all same group) = global demean
- Evaluator: NaN exclusion (NaN values don't pollute group mean)
- Evaluator: empty input → empty output
- Evaluator: length-mismatch fallback (group shorter than values → treat as nil group = global demean)
- `String()` includes group: `cs_neutralize(close, sector)`
- `Children()` returns `[Expr, Group]` when Group non-nil, `[Expr]` when nil
- `ExtractInputs` recurses into Group (sector field collected)
- String round-trip via re-parse (optional, if time permits)

**Modify `pkg/ai/expression/ast.go`**:
- `CrossSectionalNode` struct (line 89-92): add `Group Node` field (optional, nil for 1-arg cs ops)
- `String()` (line 95-97): if `n.Group != nil`, format as `cs_neutralize(expr, group)`; else current `cs_op(expr)`
- `Children()` (line 98): if `n.Group != nil`, return `[]Node{n.Expr, n.Group}`; else `[]Node{n.Expr}`
- `extractInputsRecursive` (line 149-150): add `extractInputsRecursive(n.Group, inputs)` after the Expr recursion

**Modify `pkg/ai/expression/parser.go`**:
- `parseFunctionCall` (line 317-323): special-case `cs_neutralize` — require exactly 2 args, set `Group: args[1]`. Keep the 1-arg enforcement for `cs_rank`/`cs_zscore`/`cs_percentile`.

```go
if IsCrossSectionalOp(name) {
    if name == "cs_neutralize" {
        if len(args) != 2 {
            return nil, fmt.Errorf("%s requires exactly 2 arguments (expr, group), got %d", name, len(args))
        }
        return &CrossSectionalNode{Op: name, Expr: args[0], Group: args[1]}, nil
    }
    if len(args) != 1 {
        return nil, fmt.Errorf("%s requires exactly 1 argument, got %d", name, len(args))
    }
    return &CrossSectionalNode{Op: name, Expr: args[0]}, nil
}
```

**Modify `pkg/ai/expression/evaluator.go`**:
- `evaluateCrossSectional` (line 155-180): if `n.Group != nil`, evaluate group expr, build `groupValues []float64` aligned with `latestValues`, pass to `applyCrossSectionalOp`.
- `applyCrossSectionalOp` (line 322-333): change signature to `(op string, values, group []float64) []float64`; add `case "cs_neutralize": return csNeutralize(values, group)`.

**Modify `pkg/ai/expression/operators.go`**:
- Add `csNeutralize(values, group []float64) []float64` helper: for each group, compute mean of non-NaN values, subtract from each member. If `group == nil` or `len(group) != len(values)`, fall back to global demean (delegates to a mean-subtraction over all values).

**Verification**:
```bash
go test ./pkg/ai/expression/... -count=1 -race -v
go vet ./pkg/ai/expression/...
gofmt -l pkg/ai/expression/
```

**Atomic commit**: `fix(ai/expression): support 2-arg cs_neutralize(x, group)`

---

### Phase 2: Signal Layer

**New file**: `pkg/strategy/expression/signal.go`
**New test**: `pkg/strategy/expression/signal_test.go`

**Purpose**: Convert a DSL expression (typically `factor > threshold` or `factor < threshold`) into `[]strategy.Signal` for the symbols passing the filter.

**API**:
```go
package expression

type SignalConfig struct {
    Expression string  // e.g. "cs_rank(ts_delta(close, 5)) > 0.8"
    Action     string  // "buy" | "sell"
    Direction  domain.Direction  // DirectionLong | DirectionShort
    MinStrength float64 // threshold for Strength filtering (default 0)
}

type SignalGenerator struct {
    cfg     SignalConfig
    ast     *expression.Expression  // parsed once at Configure time
}

func NewSignalGenerator(cfg SignalConfig) (*SignalGenerator, error)  // parses + validates
func (g *SignalGenerator) Generate(bars map[string][]domain.OHLCV, evaluator *expression.Evaluator) ([]strategy.Signal, error)
```

**Behavior**:
- `Generate` evaluates the AST via the evaluator → `map[string][]float64`
- Takes the latest value per symbol
- For each symbol where the comparison result is `1` (truthy), emit a `Signal{Symbol, Action, Strength: latestValue, Price: latestClose}`
- Strength is the raw factor value (not the comparison result) — lets `Weight()` do proportional sizing

**Tests** (~8):
- Parse + generate happy path (3 symbols pass `> 0.8`)
- No symbols pass → empty signals
- Empty bars → empty signals, no error
- Sell action / short direction
- MinStrength filtering
- Invalid expression at construction → error
- Nil evaluator → error
- Multi-symbol with NaN values (NaN comparison = 0 = filtered out)

**Verification**: `go test ./pkg/strategy/expression/... -count=1 -race -v`

**Atomic commit**: `feat(strategy/expression): add signal layer (DSL → Signal)`

---

### Phase 3: Position Sizing Layer

**New file**: `pkg/strategy/expression/sizing.go`
**New test**: `pkg/strategy/expression/sizing_test.go`

**Purpose**: Given a set of signals + portfolio value, compute target weight per signal. Config-driven (no DSL).

**API**:
```go
type SizingMethod string

const (
    SizingEqual         SizingMethod = "equal"          // 1/N
    SizingStrengthProp  SizingMethod = "strength_prop"  // weight ∝ strength, normalized
    SizingFixed         SizingMethod = "fixed"          // fixed fraction per signal
)

type SizingConfig struct {
    Method       SizingMethod
    FixedWeight  float64  // for "fixed" (default 0.05)
    MaxPerStock  float64  // cap per position (default 0.10)
    MaxTotal     float64  // cap total exposure (default 1.0)
}

type PositionSizer struct {
    cfg SizingConfig
}

func NewPositionSizer(cfg SizingConfig) (*PositionSizer, error)
func (p *PositionSizer) Size(signals []strategy.Signal, portfolioValue float64) (map[string]float64, error)
```

**Behavior**:
- `equal`: weight = `min(MaxPerStock, MaxTotal / N)` for each of N signals
- `strength_prop`: weight = `min(MaxPerStock, MaxTotal * strength_i / sum(strengths))`
- `fixed`: weight = `min(MaxPerStock, FixedWeight)`
- All methods clamp to `[0, MaxPerStock]` and normalize to `MaxTotal` if sum exceeds it

**Tests** (~7):
- Equal weight with 5 signals, MaxTotal=1.0 → 0.20 each
- Equal weight capped by MaxPerStock=0.15
- Strength proportional normalization
- Fixed weight
- Empty signals → empty map
- Zero total strength (all strengths 0) → fall back to equal weight
- MaxTotal enforcement (sum > MaxTotal → scale down)

**Verification**: `go test ./pkg/strategy/expression/... -count=1 -race -v`

**Atomic commit**: `feat(strategy/expression): add position sizing layer`

---

### Phase 4: Risk Control Layer

**New file**: `pkg/strategy/expression/risk.go`
**New test**: `pkg/strategy/expression/risk_test.go`

**Purpose**: Pre-trade risk checks on proposed weights. Config-driven.

**API**:
```go
type RiskConfig struct {
    MaxPositionPct   float64  // max weight per single stock (default 0.10)
    MaxDrawdown      float64  // accepted but NOT enforced in v1 (engine owns equity curve)
    MaxOpenPositions int      // max concurrent positions (default 20)
    MinCashBuffer    float64  // keep at least this fraction in cash (default 0.05)
}

type RiskController struct {
    cfg RiskConfig
}

func NewRiskController(cfg RiskConfig) (*RiskController, error)
func (r *RiskController) Check(weights map[string]float64, portfolio *domain.Portfolio) (map[string]float64, error)
```

**Behavior**:
- Clamp each weight to `MaxPositionPct`
- If `len(weights) > MaxOpenPositions`, keep top-N by weight, zero the rest
- Ensure `sum(weights) <= 1 - MinCashBuffer` (scale down if exceeded)
- `MaxDrawdown` is stored but **not enforced** in v1 — document this clearly. The backtest engine owns the equity curve and is the right place for drawdown-based halt logic. v2 will wire it when the engine exposes a `CurrentDrawdown()` hook.

**Tests** (~6):
- Clamp single position above MaxPositionPct
- Truncate to MaxOpenPositions (keep top-N)
- Scale down when sum > (1 - MinCashBuffer)
- Nil portfolio → still clamps weights
- Empty weights → empty result
- MaxDrawdown field accepted without error (documented no-op)

**Verification**: `go test ./pkg/strategy/expression/... -count=1 -race -v`

**Atomic commit**: `feat(strategy/expression): add risk control layer`

---

### Phase 5: OHLCVDataProvider Adapter

**New file**: `pkg/strategy/expression/data_provider.go`
**New test**: `pkg/strategy/expression/data_provider_test.go`

**Purpose**: Bridge `map[string][]domain.OHLCV` (the `GenerateSignals` input) to the expression engine's `expression.DataProvider` interface (`GetField(symbol, field, lookback) ([]float64, error)` + `GetSymbols() []string`).

**API**:
```go
type OHLCVDataProvider struct {
    bars    map[string][]domain.OHLCV
    symbols []string  // pre-computed, sorted for determinism
}

func NewOHLCVDataProvider(bars map[string][]domain.OHLCV) *OHLCVDataProvider
func (p *OHLCVDataProvider) GetField(symbol, field string, lookback int) ([]float64, error)
func (p *OHLCVDataProvider) GetSymbols() []string
```

**Behavior**:
- `GetSymbols`: returns pre-sorted symbol list (sorted for deterministic cross-sectional ordering)
- `GetField`: dispatches on `field`:
  - `open/high/low/close/volume` → extract from OHLCV slice
  - `turnover/market_cap/pe/pb/roe/...` → return error "fundamental field %s not available from OHLCV bars" (these require a fundamentals provider, out of scope for v1)
  - Unknown field → error
- `lookback`: if `len(bars) > lookback`, return only the last `lookback` elements (most recent). If `lookback <= 0`, return all.
- Empty bars for a symbol → return empty slice (evaluator handles NaN propagation)

**Tests** (~7):
- GetField close for a symbol with 10 bars
- GetField with lookback=5 returns last 5
- GetField with lookback=0 returns all
- GetSymbols returns sorted list
- GetField for unknown symbol → empty slice
- GetField for fundamental field (pe) → error
- Empty bars map → empty symbols, GetField returns empty

**Verification**: `go test ./pkg/strategy/expression/... -count=1 -race -v`

**Atomic commit**: `feat(strategy/expression): add OHLCVDataProvider adapter`

---

### Phase 6: ExpressionStrategy Adapter

**New file**: `pkg/strategy/expression/strategy.go`
**New test**: `pkg/strategy/expression/strategy_test.go`
**Modify**: `pkg/strategy/interfaces_compliance_test.go` (add blank import)

**Purpose**: Compose signal + sizing + risk into a `strategy.Strategy` implementation that self-registers.

**API**:
```go
type ExpressionStrategyConfig struct {
    Name        string
    Description string
    Signal      SignalConfig
    Sizing      SizingConfig
    Risk        RiskConfig
    Lookback    int  // evaluator lookback window (default 60)
}

type ExpressionStrategy struct {
    *strategy.BaseStrategy
    cfg      ExpressionStrategyConfig
    signal   *SignalGenerator
    sizing   *PositionSizer
    risk     *RiskController
}

func NewExpressionStrategy(cfg ExpressionStrategyConfig) (*ExpressionStrategy, error)
// Configure / Parameters / GenerateSignals / Weight / Cleanup
func init()  // self-register a default instance
```

**Behavior**:
- `Configure()`: parse `cfg.Signal.Expression` into AST, construct `SignalGenerator`, `PositionSizer`, `RiskController`. Validate all config fields. Store typed cfg.
- `Parameters()`: expose `signal_expression`, `signal_action`, `sizing_method`, `max_per_stock`, `lookback` as the tunable schema.
- `GenerateSignals(ctx, bars, portfolio)`:
  1. Build `OHLCVDataProvider` from `bars`
  2. Build `Evaluator` from provider
  3. `signal.Generate(bars, evaluator)` → raw signals
  4. `sizing.Size(signals, portfolioValue)` → weights map
  5. `risk.Check(weights, portfolio)` → filtered weights
  6. Convert weights back to `[]Signal` with `Strength = weight`
  7. Return signals
- `Weight(signal, portfolioValue)`: return `signal.Strength` (already sized)
- `Cleanup()`: nil out cached AST/generators
- `init()`: register a default "expression_template" strategy so it shows up in the registry / compliance test

**Inline parse helpers**: copy `parseIntParam`/`parseFloatParam`/`parseStringParam`/`validateIntRange`/`validateFloatRange`/`validateStringChoice` (~30 LOC) from `plugins/utils.go` into `pkg/strategy/expression/helpers.go` (unexported). Document why: avoid import cycle (`plugins` is a leaf; can't be imported by a sibling orchestration package without promoting the helpers to `pkg/strategy` itself, which is a larger refactor deferred to a later task).

**Compliance test update** (`pkg/strategy/interfaces_compliance_test.go`):
```go
_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/expression"  // S7-P3-1 Phase 6
```

**Tests** (~9):
- NewExpressionStrategy happy path (valid cfg → no error)
- NewExpressionStrategy invalid expression → error
- Configure with valid params → updates cfg
- Configure with invalid sizing_method → error
- GenerateSignals end-to-end: 5 symbols, 2 pass filter → 2 signals with sized weights
- GenerateSignals empty bars → empty signals, no error
- Weight returns signal.Strength
- Cleanup is idempotent
- Self-registration: `strategy.DefaultRegistry.Get("expression_template")` returns non-nil

**Verification**:
```bash
go test ./pkg/strategy/expression/... -count=1 -race -v
go test ./pkg/strategy/... -count=1 -race -v   # compliance test picks up new blank import
go build ./...
go vet ./...
gofmt -l pkg/strategy/expression/ pkg/strategy/interfaces_compliance_test.go
```

**Atomic commit**: `feat(strategy/expression): add ExpressionStrategy adapter + self-registration`

---

### Phase 7: Documentation Sync

**Modify `docs/SPEC.md`**:
- Expression DSL section: document `cs_neutralize(x, group)` 2-arg form
- New section: "Expression Strategy Stack" — signal/sizing/risk layers + ExpressionStrategy adapter
- Update Strategy interface section to mention ExpressionStrategy as a built-in

**Modify `docs/ARCHITECTURE.md`**:
- Directory tree: add `pkg/strategy/expression/` sub-package
- Module dependencies: note `pkg/strategy/expression` depends on `pkg/ai/expression` + `pkg/strategy`

**Modify `docs/TASKS.md`**:
- Mark S7-P3-1 as ✅ with completion notes (phases, files, test count)

**Modify `AGENTS.md`**:
- §3 Directory structure: add `pkg/strategy/expression/` line
- §11 Document Index: no new docs (plan file is in `.trae/documents/`, not `docs/`)

**Verification**: read-through for cross-reference consistency.

**Atomic commit**: `docs: sync SPEC/ARCHITECTURE/TASKS/AGENTS for S7-P3-1`

---

## Assumptions & Decisions

1. **`MaxDrawdown` not enforced in v1** — engine owns equity curve; will wire in v2 when engine exposes `CurrentDrawdown()`. Documented in `risk.go` doc comment.
2. **Inline parse helpers** rather than promoting to `pkg/strategy` — avoids a larger refactor; ~30 LOC duplication is acceptable and documented.
3. **Signal Strength = raw factor value** (not comparison result) — preserves ordering info for proportional sizing.
4. **No new parser features for signal/sizing/risk** — these layers are config-driven. DSL expansion is limited to the `cs_neutralize` fix. This keeps the parser surface stable and the LLM prompt honest.
5. **`expression_template` default strategy** registered in `init()` — gives the compliance test something to verify and gives users a starting point. Real strategies will be constructed via `NewExpressionStrategy` with custom configs (S7-P3-2 YAML loader).
6. **Cross-sectional evaluation uses latest value only** — matches existing `evaluateCrossSectional` behavior (line 166-172). Full time-series cross-sectional is a future enhancement.
7. **Group field can be any expression**, not just an identifier — e.g. `cs_neutralize(close, sector)` where `sector` is an identifier, but also `cs_neutralize(close, market_cap > 1e10)` for binary grouping. The evaluator evaluates `n.Group` to get group values, then uses them as categorical labels (float64 values are compared by equality).

---

## Verification Steps (per phase + final)

**Per-phase**: `go test <package> -count=1 -race -v` + `go vet` + `gofmt -l`

**Final full-suite gate** (per AGENTS.md §8.3 + memory lesson):
```bash
go build ./...
go vet ./...
gofmt -l .
go test ./... -race -count=2 -skip '^e2e'   # -count>1 exposes global-state leaks
```

**Frontend**: N/A (this task is backend-only).

**Expected test count**: ~50 new tests across 7 phases (13 + 8 + 7 + 6 + 7 + 9 = 50).

---

## Execution Order

1. Phase 1 (cs_neutralize fix) — independent, unblocks LLM emissions
2. Phase 2 (signal) — depends on Phase 1 (uses fixed evaluator)
3. Phase 3 (sizing) — independent of Phase 2, but same package
4. Phase 4 (risk) — independent of Phase 3
5. Phase 5 (data provider) — independent, but used by Phase 6
6. Phase 6 (ExpressionStrategy) — depends on Phases 2-5
7. Phase 7 (docs) — depends on all prior phases

Each phase = one atomic commit on the feature branch. After Phase 7, merge to `main` via PR (or fast-forward merge per project workflow).
