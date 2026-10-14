"""NT 原生模式的最小样例策略（D7）。

**它与 ``weight_replay`` 的区别是本质的**：``weight_replay`` 是 A 股线的
适配器——它把目标权重翻译成 A 股订单，并施加 T+1 / 涨跌停 / 整手三条
A 股规则（``sidecar.weight_replay`` + ``sidecar.ashare``）。本模块不做任何
这类魔改：订单用 NT 自带的订单类型，执行交给 NT 自己的模型，策略读到什么
就是什么。

样例策略本身刻意最小：**快/慢均线交叉**（金叉做多、死叉平掉），
买用 ``LIMIT`` 单、卖用 ``MARKET`` 单——两条腿各走一种 NT 原生订单类型，
这样「原生订单类型在跑」这件事在回执的成交流里是可查证的，而不是一句声明。

为什么策略是 Python 而不是 Go：蓝图 §3 双核职责表——国外线**想法和动作都在
NT**（全 feature）；国内线的策略仍留在 Go 侧。本模块是「国外线策略用 NT
原生写」的第一条实现（新增，不是重写）。
"""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from statistics import fmean
from typing import Any

import pandas as pd
from nautilus_trader.config import StrategyConfig
from nautilus_trader.model import Bar, BarType, InstrumentId, OrderSide
from nautilus_trader.trading import Strategy

from .errors import SidecarError

#: 默认每笔下单数量（股）。
DEFAULT_TRADE_SIZE = Decimal(1_000)

#: 均线窗默认值：快 5 / 慢 20（日线，约一周 vs 一个月）。
DEFAULT_FAST = 5
DEFAULT_SLOW = 20


@dataclass(frozen=True)
class NativeDecision:
    """一次 on_bar 的决策留痕（测试与溯源用）。"""

    day: date
    fast_ma: float
    slow_ma: float
    position: float
    action: str  # "limit_buy" | "market_sell" | "hold"
    reason: str


class NativeSmaCrossConfig(StrategyConfig):
    """Config for :class:`NativeSmaCross`.

    v2 ``StrategyConfig`` is a PyO3 type: declare custom fields as keyword-only
    args, forward ``**_kwargs`` untouched, and call ``super().__init__()`` with
    no arguments (same shape as the K13/K14 configs).
    """

    def __init__(
        self,
        *,
        instrument_id: InstrumentId,
        bar_type: BarType,
        trade_size: Decimal = DEFAULT_TRADE_SIZE,
        fast: int = DEFAULT_FAST,
        slow: int = DEFAULT_SLOW,
        **_kwargs: object,
    ) -> None:
        super().__init__()
        self.instrument_id = instrument_id
        self.bar_type = bar_type
        self.trade_size = trade_size
        self.fast = fast
        self.slow = slow


class NativeSmaCross(Strategy):
    """快/慢均线交叉，LIMIT 买 / MARKET 卖——全部走 NT 原生订单类型。

    不做的事（刻意的，不是遗漏）：不整手取整、不设涨跌停拒单、不做 T+1
    可卖量追踪、不设参与率上限。那些是 A 股线的规则（``sidecar.ashare``）；
    国外线用 NT 自己的模型。
    """

    def __init__(self, config: NativeSmaCrossConfig) -> None:
        super().__init__(config)
        self._closes: deque[float] = deque(maxlen=max(config.slow, config.fast))
        self._prev_close: float | None = None
        self._submitted: set[str] = set()
        #: 每条 bar 的决策，按时间序——测试读它，而不是解析日志。
        self.decisions: list[NativeDecision] = []

    def on_start(self) -> None:
        self.subscribe_bars(self.config.bar_type)

    def on_bar(self, bar: Bar) -> None:
        close = float(bar.close.as_double())
        self._closes.append(close)
        self._prev_close = close

        day = pd.Timestamp(bar.ts_event, unit="ns", tz="UTC").date()

        if len(self._closes) < self.config.slow:
            self._record(day, 0.0, 0.0, "hold", "warming up the slow window")
            return

        closes = list(self._closes)
        fast_ma = fmean(closes[-self.config.fast :])
        slow_ma = fmean(closes[-self.config.slow :])
        position = self._net_position()

        if fast_ma > slow_ma and position <= 0:
            # 原生订单类型之一：LIMIT。挂在当根 close 上，bar 的低点一定
            # 不高于 close ⇒ 同根撮合（NT 的 bar 撮合语义）。
            self._submit_limit_buy(day, fast_ma, slow_ma, bar)
            return

        if fast_ma <= slow_ma and position > 0:
            # 原生订单类型之二：MARKET。
            self._submit_market_sell(day, fast_ma, slow_ma, position)
            return

        self._record(day, fast_ma, slow_ma, "hold", "already at target")

    # ── order paths ──────────────────────────────────────────────────────────

    def _submit_limit_buy(self, day: date, fast_ma: float, slow_ma: float, bar: Bar) -> None:
        instrument = self.cache.instrument(self.config.instrument_id)
        if instrument is None:
            raise SidecarError(
                f"instrument {self.config.instrument_id} missing from cache; no order submitted"
            )
        order = self.order_factory.limit(
            self.config.instrument_id,
            OrderSide.BUY,
            instrument.make_qty(self.config.trade_size),
            bar.close,
        )
        self._submit_order(order, day, fast_ma, slow_ma, "limit_buy", "golden cross")

    def _submit_market_sell(
        self, day: date, fast_ma: float, slow_ma: float, position: float
    ) -> None:
        instrument = self.cache.instrument(self.config.instrument_id)
        if instrument is None:
            raise SidecarError(
                f"instrument {self.config.instrument_id} missing from cache; no order submitted"
            )
        order = self.order_factory.market(
            self.config.instrument_id,
            OrderSide.SELL,
            instrument.make_qty(position),
        )
        self._submit_order(order, day, fast_ma, slow_ma, "market_sell", "death cross")

    def _submit_order(
        self,
        order: Any,
        day: date,
        fast_ma: float,
        slow_ma: float,
        action: str,
        reason: str,
    ) -> None:
        self.submit_order(order)
        self._submitted.add(str(order.client_order_id))
        self._record(day, fast_ma, slow_ma, action, reason)

    # ── helpers ──────────────────────────────────────────────────────────────

    def _net_position(self) -> float:
        return float(self.portfolio.net_position(self.config.instrument_id))

    def _record(
        self, day: date, fast_ma: float, slow_ma: float, action: str, reason: str
    ) -> None:
        self.decisions.append(
            NativeDecision(
                day=day,
                fast_ma=fast_ma,
                slow_ma=slow_ma,
                position=self._net_position(),
                action=action,
                reason=reason,
            )
        )


#: 原生策略注册表——「加一个原生策略 = 加一行」（与 DEMO_STRATEGIES 同款机制）。
NATIVE_STRATEGIES: dict[str, tuple[type[Strategy], type[StrategyConfig]]] = {
    "native_sma_cross": (NativeSmaCross, NativeSmaCrossConfig),
}


def native_strategy_names() -> list[str]:
    return sorted(NATIVE_STRATEGIES)


def build_native_strategy(
    name: str,
    *,
    instrument_id: InstrumentId,
    bar_type: BarType,
    trade_size: Decimal = DEFAULT_TRADE_SIZE,
    fast: int = DEFAULT_FAST,
    slow: int = DEFAULT_SLOW,
) -> Strategy:
    """按名字构造原生策略；未登记名字 → :class:`SidecarError`（拒绝而非回退）。"""
    try:
        strategy_cls, config_cls = NATIVE_STRATEGIES[name]
    except KeyError:
        raise SidecarError(
            f"unknown native strategy {name!r}; known strategies: {native_strategy_names()}"
        ) from None
    config = config_cls(
        instrument_id=instrument_id,
        bar_type=bar_type,
        trade_size=trade_size,
        fast=fast,
        slow=slow,
    )
    return strategy_cls(config)
