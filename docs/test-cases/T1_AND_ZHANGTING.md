# T+1 Settlement & 涨跌停 (Price Limit) Test Cases

**Spec version:** 1.0  
**Author:** QA Engineer  
**For:** Backend Developer implementing T+1 settlement and 涨跌停 enforcement  
**Engine:** `pkg/backtest/tracker.go` + `pkg/backtest/engine.go`

---

## Overview

This document specifies the **exact behaviour** that must be implemented for two A-share market rules:

1. **T+1 Settlement** — Shares bought today cannot be sold today; they become sellable the next trading day.
2. **涨跌停 (Price Limit)** — A-stock daily price movement is capped at ±10% (normal) or ±5% (ST/*ST).

These are **specifications**, not implementation notes. A developer should be able to implement against this document without asking questions.

---

## 1. T+1 Settlement Tests

### Background

- Every long position tracks two quantities: `QuantityToday` (bought today, not yet sellable) and `QuantityYesterday` (bought on or before yesterday, sellable today).
- `AdvanceDay()` is called at the end of every trading day. It shifts `QuantityToday → QuantityYesterday` and zeroes `QuantityToday`.
- On a sell (`DirectionClose`), only `QuantityYesterday` is sellable.
- Short positions have **no T+1 restriction**.

---

### Test Cases

| ID | Case Name | Setup | Action | Expected Result |
|----|-----------|-------|--------|-----------------|
| T1-01 | **T+1 basic violation** | Buy 1000 shares of `600000.SH` on Day 1 (2024-01-02). No prior position. | Sell 1000 shares on Day 1 (same day). | **Blocked.** Error message must contain `"T+1 settlement violation"` and indicate shares were bought on today's date. `QuantityYesterday` remains 0. No trade is recorded. |
| T1-02 | **T+1 success — sell next day** | Buy 1000 shares of `600000.SH` on Day 1 (2024-01-02). Call `AdvanceDay` to Day 2. | Sell 500 shares on Day 2. | **Succeeds.** 500 shares sold at execution price. `QuantityYesterday` reduces from 1000 → 500. Trade recorded with `direction=close`, `quantity=500`. |
| T1-03 | **T+1 partial — sell more than sellable** | Buy 500 shares on Day 1. Buy 300 shares on Day 2 (new buy, separate trade). Call `AdvanceDay` to Day 3. `QuantityYesterday` should be 500 (Day 1 buy rolled over). | Sell 600 shares on Day 3. | **Partial fill.** 300 shares blocked (bought Day 2, not yet sellable on Day 3). 300 shares sold from Day 1 position. Trade recorded with `quantity=300`. Log message indicates T+1 partial fill: `"attempted_sell=600, actual_sell=300, can_sell=300"`. |
| T1-04 | **T+1 multiple buys roll up** | Buy 200 shares Day 1. Buy 300 shares Day 2. Call `AdvanceDay` to Day 3 (now `QuantityYesterday = 200 + 300 = 500`). | Sell 400 shares on Day 3. | **Succeeds.** 200 from Day 1 + 300 from Day 2 = 500 sellable. 400 sold, `QuantityYesterday` reduces to 100. Trade recorded with `quantity=400`. |
| T1-05 | **T+1 same-day sell with no position** | No position in `600000.SH`. | Sell 200 shares on Day 1. | **Blocked.** Error: `"position not found"` or `"T+1 settlement violation: no shares available to sell"`. No trade recorded. |
| T1-06 | **T+1 sell all yesterday qty** | Buy 1000 shares Day 1. `AdvanceDay` to Day 2 (`QuantityYesterday=1000`). | Sell 1000 shares on Day 2. | **Full close.** Position fully closed. `QuantityYesterday=0`, `Quantity=0`. Position removed from tracker. Trade recorded with `quantity=1000`. |
| T1-07 | **T+1 buy does not reset yesterday qty** | Buy 1000 shares Day 1. `AdvanceDay` to Day 2 (`QuantityYesterday=1000`). Buy 200 more shares Day 2 (`QuantityToday=200`). | Sell 500 shares on Day 2. | **Blocked** if attempting to sell more than `QuantityYesterday` (1000). If selling 500 ≤ 1000, succeeds. `QuantityYesterday` remains 500 (sell drawn from yesterday qty, today's 200 untouched). `QuantityToday` stays 200. |
| T1-08 | **T+1 next-day advance transfers qty** | Buy 1000 shares Day 1. | Call `AdvanceDay` from Day 1 → Day 2. Check quantities. | `QuantityYesterday` becomes 1000. `QuantityToday` becomes 0. The 1000 shares are now sellable on Day 2. |
| T1-09 | **T+1 — multiple symbols independent** | Buy 1000 shares `600000.SH` Day 1. Buy 500 shares `600001.SH` Day 1. `AdvanceDay` to Day 2. | Sell 500 `600000.SH` on Day 2. Attempt to sell 600 `600001.SH` on Day 2. | `600000.SH`: 500 sold successfully. `600001.SH`: sell blocked (only 500 buyable, all rolled to yesterday, so 500 sellable — 600 > 500 sellable → partial fill of 500). Each symbol tracked independently. |
| T1-10 | **T+1 short has no restriction** | Short sell 1000 shares `600000.SH` Day 1. | Buy-to-cover 1000 shares Day 1. | **Succeeds.** Short positions can be closed on the same day without T+1 restriction. |

---

## 2. 涨跌停 (Price Limit / Limit-Up / Limit-Down) Tests

### Background

- **Normal A-share:** ±10% price limit per trading day.
- **ST / \*ST / SST / S\*ST:** ±5% price limit.
- **New stocks (< 60 trading days listed):** ±20% price limit.
- On a **limit-up day (涨停):** Buys are **blocked**; sells are **allowed**.
- On a **limit-down day (跌停):** Sells are **blocked**; buys are **allowed**.
- The `LimitUp` / `LimitDown` flags are set on the OHLCV bar by the engine after detecting the condition.
- `prevCloseCache` is updated so the next day's limit is computed from the limit price, not the uncapped theoretical close.

---

### Limit-Up / Limit-Down Flag Detection

| Condition | Flag Set |
|-----------|-----------|
| `today.close >= prev_close × 1.10` (normal) or `× 1.05` (ST) or `× 1.20` (new) | `LimitUp = true` |
| `today.close <= prev_close × 0.90` (normal) or `× 0.95` (ST) or `× 0.80` (new) | `LimitDown = true` |

---

### Test Cases

| ID | Case Name | Setup | Action | Expected Result |
|----|-----------|-------|--------|-----------------|
| ZT-01 | **Normal limit-up blocks buys** | OHLCV bar: `prev_close=10.0`, `close=11.0`, `high=11.0` (10% up). `LimitUp=true`. | Attempt to buy `600000.SH` on this day. | **Blocked.** Log: `"Trade blocked: stock hit limit-up (涨停), cannot buy"`. No trade recorded. Sell order proceeds normally. |
| ZT-02 | **Normal limit-up allows sells** | Same bar as ZT-01. Existing long position of 1000 shares. `QuantityYesterday ≥ 1000`. | Sell 1000 shares at limit-up price (11.0). | **Succeeds.** 1000 shares sold. Trade recorded at execution price (slippage-applied). |
| ZT-03 | **Normal limit-down blocks sells** | OHLCV bar: `prev_close=10.0`, `close=9.0`, `low=9.0` (10% down). `LimitDown=true`. | Attempt to sell `600000.SH` on this day. | **Blocked.** Log: `"Trade blocked: stock hit limit-down (跌停), cannot sell"`. No trade recorded. Buy order proceeds normally. |
| ZT-04 | **Normal limit-down allows buys** | Same bar as ZT-03. No existing position. | Buy 1000 shares at limit-down price (9.0). | **Succeeds.** 1000 shares bought. Position opened. Trade recorded at execution price (slippage-applied). |
| ZT-05 | **ST limit-up is ±5% not ±10%** | Stock name starts with `"ST"`. `prev_close=5.0`. Bar: `close=5.25`, `high=5.25`. | Detect limit-up. Attempt to buy. | `LimitUp=true` (5.25 ≥ 5.0×1.05=5.25). Buy **blocked**. Not 5.5 (which would be 10%). |
| ZT-06 | **ST limit-down is ±5%** | ST stock. `prev_close=5.0`. Bar: `close=4.75`, `low=4.75`. | Detect limit-down. Attempt to sell. | `LimitDown=true` (4.75 ≤ 5.0×0.95=4.75). Sell **blocked**. Not 4.5 (which would be 10%). |
| ZT-07 | **New stock limit-up is ±20%** | Stock listed < 60 trading days. `prev_close=10.0`. Bar: `close=12.0`, `high=12.0`. | Detect limit-up. Attempt to buy. | `LimitUp=true` (12.0 ≥ 10.0×1.20=12.0). Buy **blocked**. |
| ZT-08 | **New stock limit-down is ±20%** | Stock listed < 60 trading days. `prev_close=10.0`. Bar: `close=8.0`, `low=8.0`. | Detect limit-down. Attempt to sell. | `LimitDown=true` (8.0 ≤ 10.0×0.80=8.0). Sell **blocked**. |
| ZT-09 | **Next-day gap open at limit price — no artificial continuity** | Day 1: `prev_close=10.0`, `close=11.0` (limit-up). `prevCloseCache[sym] = 11.0` after Day 1 end. Day 2 bar: `open=11.0`, `close=11.5`. | Execute trades on Day 2 using open=11.0 as price. | Day 2 limit is computed from `prevCloseCache=11.0`. Upper limit = 11.0×1.10=12.1, lower = 9.9. Trades execute at `open=11.0`. No artificial smoothing or capping of Day 2's prices. |
| ZT-10 | **Limit-up flag persists correctly in market data cache** | Engine processes Day N for `600000.SH`. Bar has limit-up. | Check `marketDataCache["600000.SH"][last].LimitUp` on Day N. | `LimitUp == true`. Price used for execution on Day N is the limit price (prev_close × 1.10). |
| ZT-11 | **prevCloseCache updated after limit-up** | Day N: `600000.SH` hits limit-up. Engine calls `AdvanceDay(N)`. | After `AdvanceDay`, check `prevCloseCache["600000.SH"]`. | `prevCloseCache["600000.SH"] == today's close (limit price)`, not the pre-limit theoretical price. Used for Day N+1 limit calculation. |
| ZT-12 | **prevCloseCache updated after limit-down** | Day N: `600000.SH` hits limit-down. Engine calls `AdvanceDay(N)`. | After `AdvanceDay`, check `prevCloseCache["600000.SH"]`. | `prevCloseCache["600000.SH"] == today's close (limit price)`. |
| ZT-13 | **Continuous limit-up (连续涨停)** | Day 1: limit-up to 11.0. Day 2: `prev_close=11.0`, limit-up again to 12.1 (×1.10). Day 3: `prev_close=12.1`, limit-up to 13.31. | Execute on each day. | Each day: buys blocked, sells allowed. `prevCloseCache` carries forward the limit price. |
| ZT-14 | **Limit price used for trade execution** | `600000.SH` hits limit-up. `prev_close=10.0`, limit price = 11.0. Existing position to sell. | Sell 100 shares at limit-up price. | Execution price = 11.0 × (1 - slippage) = 10.9989 (for slippage 0.0001). Trade recorded at execution price. Not the theoretical uncapped price. |

---

## 3. Combined T+1 + 涨跌停 Edge Cases

| ID | Case Name | Setup | Action | Expected |
|----|-----------|-------|--------|----------|
| CZ-01 | **Limit-up buy blocked, then sold next day** | Day 1: limit-up on `600000.SH`. No position. Buy blocked. | Advance to Day 2. Buy 1000 shares on Day 2. Advance to Day 3. Sell 1000 shares Day 3. | Day 1 buy blocked. Day 2 buy succeeds (price = Day 2 market price). Day 3 sell succeeds (T+1 satisfied: bought Day 2, sold Day 3). |
| CZ-02 | **Limit-down sell blocked, then bought, then sold T+1** | Day 1: limit-down on `600000.SH`. No position. Sell blocked. | Buy 1000 Day 1 (allowed on limit-down). Advance to Day 2. Sell 1000 Day 2. | Day 1 buy succeeds. Day 2 sell succeeds (T+1 satisfied: bought Day 1, sold Day 2). |
| CZ-03 | **Buy on limit-up day → T+1 sell on next day** | Buy 1000 shares `600000.SH` on a non-limit-up day. Next day: limit-up. | Advance to limit-up day. Sell 1000 shares. | Sell blocked: stock is at limit-up (ZT-01). Must wait until stock no longer at limit-up to sell. |
| CZ-04 | **Partial T+1 sell after partial buy** | Buy 500 Day 1. `AdvanceDay` to Day 2 (500 sellable). Buy 300 more Day 2. | Sell 600 on Day 2. | Blocked: only 500 sellable from Day 1. The 300 bought Day 2 cannot be sold on Day 2 (T+1). Only 500 from Day 1 sold. |

---

## 4. Acceptance Criteria

For T+1:
- [ ] Buying and immediately selling the same symbol on the same day returns an error with `"T+1 settlement violation"` in the message.
- [ ] `AdvanceDay` correctly moves `QuantityToday` → `QuantityYesterday`.
- [ ] Selling exactly `QuantityYesterday` shares succeeds; attempting to sell more than `QuantityYesterday` results in a partial fill (up to sellable qty) with a log warning.
- [ ] Short positions are NOT subject to T+1 restrictions.

For 涨跌停:
- [ ] On a declared limit-up day, buy signals are logged and skipped; sell signals execute normally.
- [ ] On a declared limit-down day, sell signals are logged and skipped; buy signals execute normally.
- [ ] Limit rate is ±10% for normal stocks, ±5% for ST/*ST, ±20% for new listings (< 60 trade days).
- [ ] `prevCloseCache` is updated to the limit price after a limit-up/down day for correct next-day limit calculation.
- [ ] Execution price on a limit day is the limit price (not the theoretical uncapped price).

---

*End of specification.*
