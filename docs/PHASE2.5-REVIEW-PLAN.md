# 系统审查报告 & 下一步开发计划

**审查日期**: 2026-04-07
**审查范围**: 设计文档、代码实现、测试覆盖、代码质量
**审查人**: AI Assistant (龙少)

---

## 一、设计文档审查结果

### ✅ 设计优点

1. **市场无关核心架构**
   - VISION.md 明确定义了"Market-Agnostic Core"原则
   - 领域模型（domain/types.go）确实实现了市场无关性
   - 符合主流量化软件（如vnpy、Zipline）的架构模式

2. **T+1 和涨跌停处理**
   - 详细定义了A股特有的交易规则
   - Tracker 实现了 QuantityYesterday/QuantityToday 分桶
   - 符合 VISION.md Principle 1: "Accuracy Before Features"

3. **插件化策略架构**
   - Strategy 接口设计合理
   - init() 自动注册机制符合 Go 惯例
   - 支持热交换（Hot-Swap）

4. **微服务架构**
   - 清晰的服务边界（analysis, data, strategy, risk）
   - HTTP API 设计 RESTful
   - Docker Compose 编排完整

### ⚠️ 设计问题与建议

#### 问题 1: 策略接口三版本不一致 [严重]

**VISION.md 定义:**
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    GenerateSignals(ctx, bars []OHLCV, portfolio *Portfolio) ([]Signal, error)
    Cleanup()
}
```

**SPEC.md 定义:**
```go
type Strategy interface {
    Name() string
    Description() string
    Configure(config map[string]interface{}) error
    Signals(ctx, universe []Stock, data MarketData, date time.Time) ([]Signal, error)
    Weight(signal Signal) float64
    Cleanup()
}
```

**实际代码 (strategy.go):**
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]Signal, error)
}
```

**影响:**
- 缺少 `Configure()` 方法导致策略无法动态配置
- 缺少 `Weight()` 方法无法实现自定义仓位权重
- 缺少 `Cleanup()` 方法可能导致资源泄漏
- `GenerateSignals` 签名在不同文档中不一致（[]OHLCV vs map[string][]OHLCV）

**建议:** 统一为 SPEC.md 版本，并更新所有文档和代码

#### 问题 2: Signal 类型定义不完整 [中等]

**SPEC.md 定义了完整的 Signal:**
- Date, Direction, Strength, Factors, Metadata 字段

**实际代码只有:**
- Symbol, Action, Strength, Price

**缺失关键字段:**
- `Date` - 无法追踪信号时间
- `Direction` - 使用 Action 字符串代替枚举，类型不安全
- `Factors` - 因子归因分析无法进行
- `Metadata` - 扩展信息丢失

**建议:** 扩展 Signal 结构体，添加缺失字段

#### 问题 3: Position 结构缺少 T+1 分桶字段 [严重]

**VISION.md 设计:**
```go
type Position struct {
    Quantity         float64
    QuantityYesterday float64  // 可卖
    QuantityToday    float64   // 锁定
    BuyDate          map[int]float64  // T+1 追踪
}
```

**实际 domain/types.go:**
```go
type Position struct {
    Symbol          string
    Quantity        float64
    AvgCost         float64
    // ... 缺少 T+1 分桶字段
}
```

**Tracker 内部实现有分桶，但 domain 层没有暴露**

**建议:** 在 domain.Position 中添加 T+1 相关字段

#### 问题 4: 多因子策略权重配置不一致 [低]

**SPEC.md:**
- value_pe: 0.25, value_pb: 0.20, momentum: 0.30, quality: 0.25

**实际代码默认值:**
- value_weight: 0.4, quality_weight: 0.3, momentum_weight: 0.3

**差异原因:** 代码简化为3因子（合并 value_pe + value_pb），但文档未更新

**建议:** 更新 SPEC.md 或调整代码默认值以保持一致

#### 问题 5: ARCHITECTURE.md 前端路由不完整 [低]

**文档只列出:**
- /, /dashboard, /screen, /index.html

**实际存在但未记录:**
- /strategy-selector.html
- /copilot.html

**建议:** 更新 ARCHITECTURE.md 添加遗漏的路由

---

## 二、设计与代码一致性检查

### ❌ 不一致项清单

| # | 文档 | 代码 | 严重程度 | 状态 |
|---|------|------|----------|------|
| D1 | Strategy 接口含 Configure/Weight/Cleanup | 接口只有4个方法 | 🔴 高 | 待修复 |
| D2 | Signal 含 Direction 枚举 | 使用 Action 字符串 | 🟡 中 | 待修复 |
| D3 | Signal 含 Factors/Metadata 字段 | 缺失这些字段 | 🟡 中 | 待修复 |
| D4 | Position 含 T+1 分桶字段 | domain 层缺失 | 🔴 高 | 待修复 |
| D5 | ARCHITECTURE.md 列出4个前端页面 | 实际6个页面 | 🟢 低 | 已修复 |
| D6 | RiskManager.CalculatePosition 应返回 ATR-based StopLoss | 返回0（未实现） | 🟡 中 | 待修复 |
| D7 | SPEC.md 定义4因子（value_pe, value_pb, momentum, quality） | 代码实现3因子（value, quality, momentum） | 🟢 低 | 文档待更新 |
| D8 | VISION.md GenerateSignals 签名用 []OHLCV | 实际用 map[string][]OHLCV | 🟡 中 | 文档待更新 |

### ✅ 一致项

1. ✅ 微服务架构与代码一致
2. ✅ 数据库表结构与代码一致
3. ✅ API 端点路径与代码一致
4. ✅ 佣金计算规则正确实现
5. ✅ Redis 缓存设计已实现
6. ✅ Docker Compose 配置正确
7. ✅ T+1 Tracker 逻辑已实现（虽然 domain 层未暴露）
8. ✅ 涨跌停检测逻辑已实现

---

## 三、测试有效性评估

### 当前测试状况

**E2E 测试 (Playwright)**: 9 个测试文件
- api-health.spec.ts ✅
- api-backtest.spec.ts ✅
- api-strategy.spec.ts ✅
- dashboard.spec.ts ✅
- backtest-engine.spec.ts ✅
- strategy-selector.spec.ts ✅
- copilot.spec.ts ✅
- screener.spec.ts ✅
- cross-navigation.spec.ts ✅

**单元测试**: ❌ 未找到 *_test.go 文件

### ⚠️ 测试覆盖率问题

| 组件 | 期望覆盖率 | 实际覆盖率 | 差距 |
|------|-----------|-----------|------|
| pkg/backtest/ | >80% | ~0% (无单元测试) | 🔴 严重 |
| pkg/tracker/ | >90% | ~0% | 🔴 严重 |
| pkg/risk/ | >80% | ~0% | 🔴 严重 |
| pkg/strategy/plugins/ | >80% | ~0% | 🔴 严重 |
| pkg/data/ | >70% | ~0% | 🔴 严重 |
| Frontend E2E | 关键流程100% | ~60% | 🟡 中等 |

### 测试有效性分析

#### ✅ 有效测试

1. **API Health Check** - 验证服务可用性
2. **Dashboard 页面加载** - 验证 UI 渲染
3. **Backtest API 调用** - 验证核心功能可调用
4. **跨页面导航** - 验证路由连通性

#### ❌ 无效或不足测试

1. **缺少 T+1 边界测试**
   - 期望: 5个测试用例（buy→sell same day blocked 等）
   - 实际: 0

2. **缺少涨跌停测试**
   - 期望: 6个测试用例（limit-up buy blocked 等）
   - 实际: 0

3. **缺少佣金计算准确性测试**
   - 期望: 验证印花税、过户费、最低佣金
   - 实际: 0

4. **缺少回归测试**
   - 期望: 3个确定性 fixture
   - 实际: 0

5. **缺少策略逻辑测试**
   - 期望: 动量排名、多因子评分正确性
   - 实际: 0

6. **E2E 测试断言过松**
   - 问题: `expect([200, 201, 202, 400]).toContain(res.status())`
   - 影响: 400 错误也被视为通过，掩盖真实 bug

### 建议

**优先级 P0 (必须立即修复):**
1. 为 tracker 编写 T+1 单元测试（至少 5 个 case）
2. 为 tracker 编写涨跌停单元测试（至少 6 个 case）
3. 为 backtest engine 编写核心流程测试

**优先级 P1 (本周完成):**
4. 为 risk manager 编写仓位计算测试
5. 为策略插件编写信号生成测试
6. 收紧 E2E 断言（不接受 400 作为成功）

**优先级 P2 (Phase 2 完成):**
7. 添加回归测试 fixture
8. 达到 80% 代码覆盖率目标

---

## 四、代码质量审查

### 🔴 严重问题

#### 1. RiskManager.StopLoss 未实现

[manager.go:176-179](pkg/risk/manager.go#L176-L179)
```go
return domain.PositionSize{
    Size:       size,
    Weight:     weight,
    StopLoss:   0, // Would be calculated with ATR  ← TODO
    TakeProfit: 0, // Would be calculated with ATR  ← TODO
    RiskScore:  riskScore,
}, nil
```

**影响:** 止损止盈功能完全不可用
**建议:** 实现 ATR 计算器，集成到 position sizing 流程

#### 2. Strategy 接口缺少关键方法

[strategy.go:31-37](pkg/strategy/strategy.go#L31-L37)
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]Signal, error)
    // 缺少:
    // Configure(params map[string]any) error
    // Weight(signal Signal) float64
    // Cleanup()
}
```

**影响:**
- 策略无法动态配置参数（只能硬编码默认值）
- 无法实现自定义仓位权重逻辑
- 资源无法清理（缓存、HTTP 连接等）

**建议:** 扩展接口，并为现有策略添加实现

#### 3. 错误处理不一致

部分函数返回 error 但调用方忽略：
```go
// engine.go - 忽略错误
result, _ := s.getSignals(ctx, date, universe)
```

**建议:** 使用严格的错误处理策略，要么 log 要么 propagate

### 🟡 中等问题

#### 4. 硬编码配置散落各处

```go
// tracker.go
const stampTaxRate = 0.001
const minCommission = 5.0
const transferFeeRate = 0.00001

// engine.go
const priceLimitNormal = 0.10
const priceLimitST = 0.05
```

**建议:** 移至 config.yaml 或 config struct，支持不同市场配置

#### 5. 缺少输入验证

BacktestRequest 验证不足：
```go
// 应该验证:
// - start_date < end_date
// - stock_pool 不为空且 symbol 格式正确
// - initial_capital > 0
// - strategy name 存在于 registry
```

**建议:** 添加 validator 中间件或 request validation 函数

#### 6. 日志不规范

混用 zerolog 和 fmt.Printf：
```go
logger.Info().Msg("...")  // 好
fmt.Println("...")        // 不好，应移除
```

**建议:** 统一使用 zerolog，设置全局日志级别

#### 7. HTTP Client 未复用

每个策略都创建新的 http.Client：
```go
httpClient := &http.Client{Timeout: 10 * time.Second}
```

**建议:** 使用连接池或注入共享 client

### 🟢 轻微问题

#### 8. 命名不一致

- `GenerateSignals` vs `Signals` (SPEC.md 用后者)
- `Action` vs `Direction` (Signal 字段)
- `ohlcv_daily_qfq` vs `OHLCV` (数据库 vs 代码)

**建议:** 统一命名规范，编写 style guide

#### 9. 注释不足

关键业务逻辑缺少注释：
- T+1 分桶算法
- 复权价格计算
- 因子 Z-Score 标准化

**建议:** 添加算法说明注释，引用 VISION.md 对应章节

#### 10. Magic Numbers

```go
if weight < rm.config.MinPositionWeight {
    weight = rm.config.MinPositionWeight
}
if weight > rm.config.MaxPositionWeight {  // 0.05?
    weight = rm.config.MaxPositionWeight
}
```

**建议:** 提取为命名常量，添加单位注释

---

## 五、架构重构建议

### 当前架构评分: 7/10

**优点:**
- ✅ 微服务边界清晰
- ✅ 领域驱动设计
- ✅ 插件化策略
- ✅ 缓存层抽象

**改进空间:**

#### 1. 引入依赖注入框架 [推荐]

当前: 手动组装依赖
```go
engine, _ := backtest.NewEngine(v, logger)
store, _ := storage.NewPostgresStore(ctx, dbURL)
```

建议: 使用 wire 或 fx
```go
// wire.go
func InitializeApp(*viper.Viper) (*App, error) {
    wire.Build(
        NewEngine,
        NewTracker,
        NewRiskManager,
        NewDataCache,
        ...
    )
    return nil, nil
}
```

**好处:**
- 依赖关系可视化
- 易于 mock 测试
- 循环依赖检测

#### 2. 抽象数据访问层 [推荐]

当前: Engine 直接 HTTP 调用 data-service
```go
resp, err := http.Post(dataServiceURL + "/ohlcv/" + symbol, ...)
```

建议: 定义 MarketData 接口
```go
type MarketDataProvider interface {
    GetOHLCV(symbol string, start, end time.Time) ([]OHLCV, error)
    GetFundamental(symbol string, date time.Time) (*Fundamental, error)
}

// 实现1: HttpMarketDataProvider (生产环境)
// 实现2: InMemoryProvider (测试环境)
// 实现3: CachedProvider (装饰器模式)
```

**好处:**
- 回测引擎与数据源解耦
- 单元测试无需启动 HTTP 服务
- 支持多种数据源切换

#### 3. 事件驱动架构 [Phase 3 考虑]

当前: 同步调用链
```
Engine → getSignals → Strategy → HTTP → data-service
```

建议: 引入事件总线
```
Engine → emit(MarketDataReady) → Strategy → emit(SignalGenerated) → RiskManager → emit(PositionCalculated) → Tracker
```

**好处:**
- 解耦组件
- 支持异步处理
- 易于添加监控、日志中间件

#### 4. 策略版本管理 [Phase 2]

当前: 策略无版本概念

建议: 添加版本元数据
```go
type StrategyVersion struct {
    Name      string
    Version   string  // semver
    CreatedAt time.Time
    ConfigHash string  // 参数指纹
    BacktestResults []BacktestResultID
}
```

**好处:**
- 策略可追溯
- A/B 测试支持
- 回滚能力

---

## 六、下一步开发计划

### Phase 2.5: 技术债务清理 & 基础加固

**目标:** 修复审查发现的所有问题，为 Phase 3 打下坚实基础
**预计工期:** 2 周 (10个工作日)

#### Sprint 2.5A: 接口统一 & 核心修复 (5天)

| # | 任务 | 优先级 | 工作量 | 验收标准 |
|---|------|--------|--------|----------|
| 2.5A.1 | **统一 Strategy 接口** | P0 | 1天 | 添加 Configure/Weight/Cleanup；所有策略实现新方法；向后兼容旧接口 |
| 2.5A.2 | **扩展 Signal 结构体** | P0 | 0.5天 | 添加 Date/Direction/Factors/Metadata；迁移现有代码；更新序列化 |
| 2.5A.3 | **完善 Position T+1 字段** | P0 | 0.5天 | domain.Position 添加分桶字段；Tracker 映射到 domain 层；API 返回包含 T+1 信息 |
| 2.5A.4 | **实现 ATR StopLoss** | P1 | 1天 | 实现 ATR 计算器；集成到 RiskManager.CalculatePosition；添加单元测试 |
| 2.5A.5 | **统一错误处理** | P1 | 1天 | 移除所有 ignored errors；添加 error wrapping；定义标准错误码 |
| 2.5A.6 | **更新设计文档** | P1 | 1天 | VISION/SPEC/ARCHITECTURE 三文档同步；标注最终版接口定义 |

**Sprint 退出标准:**
- [ ] 所有策略实现统一的 Strategy 接口
- [ ] Signal/Position 类型完整且文档化
- [ ] StopLoss 功能可用（非 0）
- [ ] 代码无 ignored errors
- [ ] 三份设计文档完全一致

#### Sprint 2.5B: 测试补全 & 质量提升 (5天)

| # | 任务 | 优先级 | 工作量 | 验收标准 |
|---|------|--------|--------|----------|
| 2.5B.1 | **Tracker T+1 单元测试** | P0 | 1天 | ≥5 个 test case；覆盖 buy→sell same day、partial sell、YD/TD depletion |
| 2.5B.2 | **Tracker 涨跌停测试** | P0 | 1天 | ≥6 个 test case；覆盖 ST stock ±5%、gap model、limit price accuracy |
| 2.5B.3 | **Backtest Engine 核心测试** | P0 | 1.5天 | 测试日循环、信号处理、交易执行、NAV 计算；mock data provider |
| 2.5B.4 | **Risk Manager 测试** | P1 | 1天 | 测试波动率目标、regime 调整、position sizing 边界条件 |
| 2.5B.5 | **策略插件测试** | P1 | 1天 | 测试动量排名、多因子评分、参数配置；使用 fixture 数据 |
| 2.5B.6 | **收紧 E2E 断言** | P1 | 0.5天 | 移除 accept-400 的宽松断言；区分 expected error vs unexpected failure |

**Sprint 退出标准:**
- [ ] go test ./pkg/backtest/... coverage > 80%
- [ ] go test ./pkg/tracker/... coverage > 90%
- [ ] go test ./pkg/risk/... coverage > 75%
- [ ] go test ./pkg/strategy/plugins/... coverage > 70%
- [ ] E2E 测试全部严格通过（no false positives）

#### Sprint 2.5C: 构构优化 (可选, 3天)

| # | 任务 | 优先级 | 工作量 | 说明 |
|---|------|--------|--------|------|
| 2.5C.1 | **引入 DI 容器** | P2 | 1天 | 使用 google/wire；重构 main.go；添加 integration test |
| 2.5C.2 | **抽象 MarketDataProvider** | P2 | 1天 | 定义接口；Http/InMemory/Cached 三个实现；重构 Engine |
| 2.5C.3 | **配置外部化** | P2 | 1天 | 移除硬编码常量；config.yaml 包含所有市场参数；支持多配置文件 |

**Sprint 退出标准:**
- [ ] 依赖关系通过 wire 自动生成
- [ ] Engine 不直接依赖 HTTP client
- [ ] 零 hard-coded magic numbers

---

## 七、风险与缓解措施

### 高风险项

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 接口变更破坏现有 E2E 测试 | 中 | 高 | 向后兼容旧接口；渐进式迁移；增加 adapter layer |
| T+1 测试发现引擎 bug | 高 | 高 | 先写测试再修 bug；保留 regression fixture |
| StopLoss 实现复杂度高 | 中 | 中 | 先实现简单版（固定百分比）；ATTR 后续迭代 |

### 中风险项

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 文档同步滞后 | 高 | 低 | 文档即代码；CI check 文档一致性 |
| 测试覆盖率达标困难 | 中 | 中 | 先覆盖 critical path；逐步提升 |

---

## 八、成功指标

### Phase 2.5 完成后应达到:

✅ **设计质量**
- 三份设计文档 100% 一致
- 接口定义清晰，无歧义
- 代码与文档完全对齐

✅ **代码质量**
- 零 critical/severity issues (SonarQube)
- 零 ignored errors
- 零 hard-coded constants
- 代码覆盖率: core > 85%, overall > 75%

✅ **测试质量**
- T+1: 5+ unit tests, all pass
- 涨跌停: 6+ unit tests, all pass
- 回测引擎: 10+ unit tests, all pass
- E2E: 全部 strict pass, no false positives

✅ **架构健康度**
- 依赖注入可行
- 数据访问层抽象
- 可测试性良好（mock 友好）

---

## 九、长期路线图建议 (Phase 3+)

### Phase 3: 生产就绪 (4周)

1. **Walk-Forward Validation**
   - Train/Test split 框架
   - Out-of-sample 性能度量
   - Overfitting 检测

2. **Strategy Copilot 完善**
   - LLM 集成优化
   - 代码生成→编译→回测 pipeline
   - 策略库管理

3. **Performance Optimization**
   - 目标: 500股/5年 ≤ 5秒
   - Goroutine 并行化
   - Batch DB 操作
   - 内存预加载

4. **Monitoring & Observability**
   - Prometheus metrics
   - Grafana dashboards
   - Structured logging (JSON)

### Phase 4: 高级特性 (6周)

1. **Machine Learning Factors**
   - Feature engineering pipeline
   - Model training framework
   - Prediction API

2. **Live Trading Simulation**
   - Paper trading mode
   - Order management
   - Real-time data feed

3. **Multi-Market Support**
   - US equities adapter
   - Crypto adapter
   - Config-driven market rules

---

## 十、总结与行动建议

### 当前状态评估

**成熟度:** Phase 1.5 (基础功能完成，技术债务积累中)
**代码质量:** B- (可运行，但需重构)
**测试覆盖:** D (仅有 E2E，缺单元测试)
**文档一致性:** C (多版本共存，需统一)

### 立即行动 (本周)

1. ✅ **创建分支** `feature/phase-2.5-refactor`
2. ✅ **从 Sprint 2.5A.1 开始** - 统一 Strategy 接口
3. ✅ **每日站会** - 跟踪进度，及时阻塞升级
4. ✅ **Code Review** - 每个 PR 必须通过 review

### 本月目标

完成 Phase 2.5 的 Sprint 2.5A 和 2.5B，使系统达到:
- 接口统一且稳定
- 核心逻辑有充分测试保护
- 技术债务清零
- 为 Phase 3 打下坚实基础

### 关键决策点

**是否现在做架构重构 (Sprint 2.5C)?**
- ✅ **推荐做** - 现在重构成本最低（用户量小）
- ⏰ **最晚 deadline** - Phase 3 开始前必须完成
- 💰 **投入产出比** - 高（减少未来 50% 维护成本）

---

**文档版本:** v1.0
**下次审查日期:** Phase 2.5 完成后
**负责人:** 龙少 (AI Assistant)
**批准状态:** 待 PM 审批
