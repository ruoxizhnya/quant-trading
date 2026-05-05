# Live Trading Interface Specification

> **版本**: v1.0
> **最后更新**: 2026-05-05
> **状态**: Phase 3 已完成 (MockTrader 实现就绪，真实券商接口预留)

## 1. 概述

本文档定义 Quant Lab 实盘交易接口规范，包括：

- **LiveTrader**: 核心交易接口（下单、撤单、查询）
- **AdvancedTrader**: 扩展接口（批量操作、行情订阅、风控检查）
- **MockTrader**: 模拟交易实现（A股规则、T+1、印花税、涨跌停）
- **OrderStore**: 订单持久化存储接口

## 2. 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                     Strategy Engine                         │
│              (GenerateSignals → OrderRequest)               │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                    LiveTrader Interface                     │
│  SubmitOrder / CancelOrder / GetOrder / GetPositions /      │
│  GetAccount / HealthCheck                                   │
└─────────────────────┬───────────────────────────────────────┘
                      │
          ┌───────────┴───────────┐
          │                       │
          ▼                       ▼
┌─────────────────┐    ┌──────────────────────┐
│   MockTrader    │    │  Real Broker Adapter │
│  (Simulation)   │    │  (Interactive Brokers│
│                 │    │   / 中泰 / 恒生等)    │
└────────┬────────┘    └──────────┬───────────┘
         │                        │
         ▼                        ▼
┌─────────────────┐    ┌──────────────────────┐
│   OrderStore    │    │   Broker API         │
│  (Memory/PG/    │    │  (REST/WebSocket)    │
│   Redis)        │    │                      │
└─────────────────┘    └──────────────────────┘
```

## 3. 核心接口

### 3.1 LiveTrader

```go
type LiveTrader interface {
    // 提交订单
    SubmitOrder(ctx context.Context, symbol string, direction domain.Direction, 
                orderType domain.OrderType, quantity float64, price float64) 
                (*OrderResult, error)
    
    // 撤销订单
    CancelOrder(ctx context.Context, orderID string) error
    
    // 查询订单状态
    GetOrder(ctx context.Context, orderID string) (*OrderResult, error)
    
    // 获取持仓
    GetPositions(ctx context.Context) ([]PositionInfo, error)
    
    // 获取账户信息
    GetAccount(ctx context.Context) (*AccountInfo, error)
    
    // 获取实现名称
    Name() string
    
    // 健康检查
    HealthCheck(ctx context.Context) error
}
```

### 3.2 AdvancedTrader

```go
type AdvancedTrader interface {
    LiveTrader
    
    // 批量操作
    SubmitOrders(ctx context.Context, orders []OrderRequest) (*BatchOrderResult, error)
    CancelAllOrders(ctx context.Context, symbol string) (int, error)
    
    // 查询操作
    ListOrders(ctx context.Context, filter OrderFilter) ([]*OrderResult, int, error)
    GetTrades(ctx context.Context, orderID string) ([]TradeRecord, error)
    ListTodayTrades(ctx context.Context) ([]TradeRecord, error)
    
    // 持仓查询
    GetPosition(ctx context.Context, symbol string) (*PositionDetail, error)
    GetAvailableQuantity(ctx context.Context, symbol string) (float64, error)
    
    // 资金流水
    GetCashFlow(ctx context.Context, startTime, endTime *time.Time) ([]CashFlow, error)
    GetFrozenCash(ctx context.Context) (float64, error)
    
    // 行情订阅
    SubscribeQuotes(ctx context.Context, symbols []string) (<-chan MarketData, error)
    UnsubscribeQuotes(symbols []string) error
    GetQuote(ctx context.Context, symbol string) (*MarketData, error)
    
    // 连接管理
    Connect(ctx context.Context) error
    Disconnect() error
    GetConnectionStatus() ConnectionStatus
    
    // 风控检查
    CheckMargin(ctx context.Context, symbol string, direction domain.Direction, 
                quantity float64) (*MarginCheckResult, error)
}
```

## 4. 数据模型

### 4.1 OrderResult

| 字段 | 类型 | 说明 |
|------|------|------|
| `order_id` | string | 订单唯一标识 |
| `symbol` | string | 股票代码 |
| `direction` | string | 方向: "buy"/"sell" |
| `order_type` | string | 类型: "market"/"limit" |
| `quantity` | float64 | 委托数量 |
| `filled_qty` | float64 | 成交数量 |
| `price` | float64 | 委托价格 |
| `status` | string | 状态: pending/filled/partial/cancelled/rejected/expired |
| `submitted_at` | time.Time | 提交时间 |
| `message` | string | 附加信息 |

### 4.2 AccountInfo

| 字段 | 类型 | 说明 |
|------|------|------|
| `total_assets` | float64 | 总资产 |
| `cash` | float64 | 可用现金 |
| `market_value` | float64 | 持仓市值 |
| `unrealized_pnl` | float64 | 浮动盈亏 |
| `realized_pnl` | float64 | 已实现盈亏 |
| `updated_at` | time.Time | 更新时间 |

### 4.3 PositionInfo

| 字段 | 类型 | 说明 |
|------|------|------|
| `symbol` | string | 股票代码 |
| `quantity` | float64 | 总持仓 |
| `available_qty` | float64 | 可卖数量（T+1后） |
| `avg_cost` | float64 | 平均成本 |
| `current_price` | float64 | 当前价格 |
| `market_value` | float64 | 市值 |
| `unrealized_pnl` | float64 | 浮动盈亏 |
| `quantity_today` | float64 | 今日买入 |
| `quantity_yesterday` | float64 | 昨日持仓 |

## 5. A股交易规则

### 5.1 费用结构

| 费用类型 | 费率 | 说明 |
|---------|------|------|
| 佣金 | 0.025% (最低5元) | 买卖双向收取 |
| 印花税 | 0.1% | 卖出时收取 |
| 过户费 | 0.001% | 买卖双向收取 |

### 5.2 交易限制

- **T+1 制度**: 当日买入的股票，次日才能卖出
- **涨跌停限制**: 普通股票 ±10%，ST股票 ±5%
- **最小变动单位**: 0.01元
- **交易时间**: 9:30-11:30, 13:00-15:00

## 6. MockTrader 实现

### 6.1 配置

```go
type MockTraderConfig struct {
    InitialCash     float64       // 初始资金
    CommissionRate  float64       // 佣金费率
    StampTaxRate    float64       // 印花税率
    TransferFeeRate float64       // 过户费率
    Slippage        float64       // 滑点
    PriceSource     PriceSource   // 价格来源
}
```

### 6.2 使用示例

```go
// 创建模拟交易员
config := live.MockTraderConfig{
    InitialCash:     1_000_000,
    CommissionRate:  0.00025,
    StampTaxRate:    0.001,
    TransferFeeRate: 0.00001,
}
trader := live.NewMockTrader(config, logger)

// 提交市价买单
result, err := trader.SubmitOrder(ctx, "000001.SZ", domain.DirectionLong, 
                                  domain.OrderTypeMarket, 100, 0)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Order %s status: %s\n", result.OrderID, result.Status)

// 查询持仓
positions, err := trader.GetPositions(ctx)
if err != nil {
    log.Fatal(err)
}
for _, pos := range positions {
    fmt.Printf("%s: %f shares @ %f\n", pos.Symbol, pos.Quantity, pos.AvgCost)
}
```

## 7. OrderStore 接口

```go
type OrderStore interface {
    SaveOrder(ctx context.Context, order *OrderResult) error
    GetOrder(ctx context.Context, orderID string) (*OrderResult, error)
    UpdateOrder(ctx context.Context, order *OrderResult) error
    ListOrders(ctx context.Context, filter OrderFilter) ([]*OrderResult, error)
    SaveTrade(ctx context.Context, trade *TradeRecord) error
    GetTrades(ctx context.Context, orderID string) ([]TradeRecord, error)
}
```

### 7.1 实现

- **MemoryOrderStore**: 内存存储（测试用）
- **PostgresOrderStore**: PostgreSQL 持久化
- **RedisOrderStore**: Redis 缓存

## 8. 真实券商接入规范

### 8.1 接入流程

1. **申请 API 权限**: 向券商申请程序化交易接口
2. **实现 BrokerAdapter**: 实现 `LiveTrader` 或 `AdvancedTrader` 接口
3. **配置连接参数**: 服务器地址、账户信息、API Key
4. **测试环境验证**: 使用券商模拟环境测试
5. **生产环境上线**: 切换至真实交易环境

### 8.2 适配器示例

```go
type BrokerAdapter struct {
    client     *BrokerAPIClient
    accountID  string
    orderStore OrderStore
}

func (a *BrokerAdapter) SubmitOrder(ctx context.Context, symbol string, 
    direction domain.Direction, orderType domain.OrderType, 
    quantity float64, price float64) (*OrderResult, error) {
    
    // 转换为本地方向到券商方向
    brokerDirection := convertDirection(direction)
    
    // 调用券商 API
    resp, err := a.client.PlaceOrder(ctx, &broker.PlaceOrderRequest{
        AccountID: a.accountID,
        Symbol:    symbol,
        Direction: brokerDirection,
        Type:      convertOrderType(orderType),
        Quantity:  quantity,
        Price:     price,
    })
    if err != nil {
        return nil, err
    }
    
    // 转换为标准 OrderResult
    return &OrderResult{
        OrderID:   resp.OrderID,
        Symbol:    symbol,
        Direction: direction,
        OrderType: orderType,
        Quantity:  quantity,
        Price:     price,
        Status:    convertStatus(resp.Status),
    }, nil
}
```

## 9. API 端点

### 9.1 订单管理

```
POST /api/orders              - 提交订单
DELETE /api/orders/:id        - 撤销订单
GET /api/orders/:id           - 查询订单
GET /api/orders               - 列出订单（支持过滤）
```

### 9.2 持仓与账户

```
GET /api/positions            - 获取持仓
GET /api/account              - 获取账户信息
GET /api/cash-flow            - 获取资金流水
```

### 9.3 行情数据

```
GET /api/quotes/:symbol       - 获取实时行情
POST /api/quotes/subscribe    - 订阅行情
POST /api/quotes/unsubscribe  - 取消订阅
```

## 10. 测试

### 10.1 单元测试

```bash
go test ./pkg/live/... -v
```

### 10.2 模拟交易测试

```bash
# 运行 MockTrader 回测验证
go test ./pkg/live/... -run TestMockTrader -v
```

## 11. 注意事项

1. **并发安全**: 所有实现必须保证并发安全
2. **超时控制**: 所有操作必须设置合理的超时
3. **错误处理**: 网络错误应重试，业务错误应返回明确错误信息
4. **日志记录**: 所有订单操作必须记录完整日志
5. **风控检查**: 实盘交易前必须进行资金、持仓、价格限制检查

---

_本文档随代码更新而更新，最后更新: 2026-05-05_
