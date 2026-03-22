# 量化交易项目进度 — 2026-03-22

## 当前状态

**E2E 回测：部分通，0 笔交易（待修复）**

---

## 服务状态

| 服务 | 端口 | 状态 |
|------|------|------|
| Data Service | 8081 | ✅ 运行中，OHLCV 同步正常 |
| Strategy Service | 8082 | ✅ 运行中，momentum 策略已注册 |
| Risk Service | 8083 | ✅ 运行中，regime detection 已修复 |
| Analysis Service | 8085 | ✅ 运行中，回测引擎可调用 |

**Docker**: TimescaleDB + Redis 正常运行

---

## 已解决的问题（经验教训）

1. **tushare 日期格式**：`YYYYMMDD`，不是 `YYYY-MM-DD`
2. **服务间 API 路径**：每个服务的路由需要手动核对，不是自动一致的
3. **JSON tag 缺失**：domain types 没有 JSON tag，导致跨服务序列化失败
4. **lookback_days 不传递**：回测引擎没有把 lookback 参数传给策略服务
5. **Portfolio UpdatedAt**：时间字段序列化导致 JSON 无效，risk service 500
6. **regime detection 数据不足**：需要 200 日数据但只有 71-80，改为 fallback 到默认值
7. **subagent 超时问题**：实际写完了代码但没来得及报告，需要检查文件系统

---

## 当前待解问题

### 1. 回测 0 笔交易（优先级：高）

**症状**：回测引擎运行完成，但 0 笔交易

**已尝试的修复**：
- ✅ momentum 策略 threshold 改为 0（任何正动量 = long）
- ✅ risk service JSON tag 修复（Portfolio.UpdatedAt）
- ✅ regime detection fallback（数据不足时用默认值）
- ✅ getSignals 请求体现在正确传递 market_data

**疑似遗留问题**：
- `signal.Date.Before(date)` 判断导致大量信号被跳过（信号日期可能是 UTC 00:00，而 trading day 是北京时间）
- 或者 risk service 的 calculate_position 仍然返回 size=0

**下一步**：
- 找 coding agent 来 trace 并修复这个 bug

### 2. 财务数据未同步（优先级：中）

**状态**：value_momentum 策略无法工作，因为没有 PE/PB/ROE 数据

**需要**：实现 tushare 财务数据同步（fina_indicator 接口，按 ann_date 而非交易日）

---

## 代码位置

- 项目：`/Users/ruoxi/longshaosWorld/quant-trading`
- GitHub：https://github.com/ruoxizhnya/quant-trading
- 二进制：`/Users/ruoxi/longshaosWorld/quant-trading/bin/`

---

## 已验证的功能

- ✅ tushare API 连通（token 有效）
- ✅ 5只股票 OHLCV 数据同步（2024全年）：600036.SH, 600519.SH, 601398.SH, 600000.SH, 000001.SZ
- ✅ 策略服务接收 market_data 并生成信号
- ✅ Risk service calculate_position 返回有效 position size
- ✅ 回测引擎完整执行（但无交易）
- ✅ Docker Compose 环境完整

---

## 启动命令

```bash
# 启动数据库和缓存
cd /Users/ruoxi/longshaosWorld/quant-trading
docker-compose up -d postgres redis

# 启动各服务
TUSHARE_TOKEN=704d112dd1d5f203b88d86228eb9ea43c4b5a03862218c845ad89f20 \
  ./bin/data-service > /tmp/data-service.log 2>&1 &
./bin/strategy-service > /tmp/strategy-service.log 2>&1 &
./bin/risk-service > /tmp/risk-service.log 2>&1 &
./bin/analysis-service > /tmp/analysis-service.log 2>&1 &

# 跑回测
curl -X POST http://localhost:8085/backtest \
  -H "Content-Type: application/json" \
  -d '{
    "strategy": "momentum",
    "stock_pool": ["600036.SH"],
    "start_date": "2024-03-01",
    "end_date": "2024-06-30",
    "initial_capital": 1000000,
    "risk_free_rate": 0.03,
    "lookback_days": 5
  }'
```

---

## 下一步计划

1. **优先**：修复回测 0 交易问题（找 coding agent 来 debug）
2. **次优先**：同步财务数据，支持 value_momentum 策略
3. **后续**：扩展股票池，跑更完整的回测
4. **长期**：V1.5 实时信号、V2.0 模拟实盘
