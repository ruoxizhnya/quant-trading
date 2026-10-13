"""Weight-replay adapter — the minimal NT strategy for K14.

Its single job: translate a target-weight signal stream into A-share orders,
applying the minimal rule set in :mod:`sidecar.ashare`. It deliberately adds
**no** execution enhancement the Go kernel lacks (no iceberg / partial-fill
defaults), so a K15 double-run difference can be attributed to the execution
mechanism rather than to this adapter.

Order sizing (per rebalance day):

    target_qty = round_down_to_lot(weight * capital_base / bar_close)
    delta      = target_qty - current_net_position

with three hard guards, each matching a Go-side rule:

    T+1          a sell is truncated to the start-of-day sellable quantity
    price limit  a buy is skipped at/above the upper limit, a sell at/below the
                 lower limit (触及涨停不追买 / 触及跌停不追卖)
    participation per-day traded shares are capped at D_max * bar volume

The adapter reads target weights only via ``signals.as_of(bar_date)`` (never a
future bar), so it cannot look ahead.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date
from typing import Any

import pandas as pd
from nautilus_trader.config import StrategyConfig
from nautilus_trader.model import Bar, BarType, InstrumentId, OrderSide
from nautilus_trader.trading import Strategy

from . import ashare
from .errors import SidecarError
from .signals import SignalStream

#: Default participation cap: at most 10% of a bar's volume per day.
DEFAULT_D_MAX = 0.10


def _utc_date_from_ns(ts_event_ns: int) -> date:
    """UTC date of a nanosecond timestamp (used for day-rollover bookkeeping)."""
    return pd.Timestamp(ts_event_ns, unit="ns", tz="UTC").date()


@dataclass(frozen=True)
class ReplayDecision:
    """What the adapter decided for one bar — kept for tests / provenance."""

    day: date
    target_qty: float
    current_qty: float
    delta: float
    submitted: float
    side: str  # "buy" | "sell" | "none"
    reason: str


class WeightReplayConfig(StrategyConfig):
    """Config for :class:`WeightReplay`.

    v2 ``StrategyConfig`` is a PyO3 type: declare custom fields as keyword-only
    args, forward ``**_kwargs`` untouched, and call ``super().__init__()`` with no
    arguments (same shape as ``strategies.BuyAndHoldConfig``).
    """

    def __init__(
        self,
        *,
        instrument_id: InstrumentId,
        bar_type: BarType,
        signals: SignalStream,
        capital_base: float,
        d_max: float = DEFAULT_D_MAX,
        limit_rate: float = ashare.MAIN_BOARD_LIMIT_RATE,
        lot_size: int = ashare.LOT_SIZE,
        **_kwargs: object,
    ) -> None:
        super().__init__()
        self.instrument_id = instrument_id
        self.bar_type = bar_type
        self.signals = signals
        self.capital_base = capital_base
        self.d_max = d_max
        self.limit_rate = limit_rate
        self.lot_size = lot_size


class WeightReplay(Strategy):
    """Replay target weights into orders, under the A-share minimal rule set."""

    def __init__(self, config: WeightReplayConfig) -> None:
        super().__init__(config)
        self._day: date | None = None
        self._prev_close: float | None = None
        self._sellable: float = 0.0
        self._traded_today: float = 0.0
        #: K14-R1 B · orders submitted but not yet resolved (filled / rejected).
        #: Keyed by client order id, valued by the submitted quantity.
        self._pending: dict[str, float] = {}
        #: K14-R1 B · how many orders NT rejected. An **observable** record, not
        #: just a log line — a rejected leg must never pass silently.
        self.rejections: int = 0
        #: K14-R1 B · a rejected order halts further dispatch: the target weight is
        #: unreachable (e.g. cash), and blindly retrying every bar produced the
        #: observed "resubmit 5×" loop. Recorded once, then stop.
        self._halted: bool = False
        #: Every decision this run made, in order — the consistency gate reads it.
        self.decisions: list[ReplayDecision] = []

    def on_start(self) -> None:
        self.subscribe_bars(self.config.bar_type)

    # ── order lifecycle (K14-R1 B) ───────────────────────────────────────────

    def on_order_filled(self, event: Any) -> None:
        """Backfill the real fill into the participation budget and T+1 view.

        K14-R1 B: the previous code debited ``_traded_today`` / ``_sellable`` at
        *submit* time, so a rejected order still consumed budget and corrupted the
        T+1 sellable view. Accounting now happens on the actual fill.
        """
        oid = str(event.client_order_id)
        qty = float(event.last_qty.as_double())
        self._pending.pop(oid, None)
        self._traded_today += qty
        order = self.cache.order(event.client_order_id)
        if order is not None and order.side == OrderSide.SELL:
            self._sellable = max(0.0, self._sellable - qty)

    def on_order_denied(self, event: Any) -> None:
        """A pre-trade rejection by the risk engine (e.g. insufficient cash).

        K14-R1 B: NT reports a denied order here (``OrderDenied``), not via
        ``on_order_rejected`` (which carries *venue* rejections). Both must be
        accounted for, or a rejected leg keeps resubmitting every bar.
        """
        self._resolve_unfilled(str(event.client_order_id), getattr(event, "reason", ""))

    def on_order_rejected(self, event: Any) -> None:
        """A venue rejection — record it observably and halt further dispatch."""
        self._resolve_unfilled(str(event.client_order_id), getattr(event, "reason", ""))

    def _resolve_unfilled(self, oid: str, reason: str) -> None:
        qty = self._pending.pop(oid, 0.0)
        self.rejections += 1
        # A rejected/denied order halts dispatch: the target weight is unreachable
        # (e.g. cash), and retrying every bar only repeats the rejection.
        self._halted = True
        # NT's Logger takes a single pre-formatted message (no %-style args).
        self.log.warning(f"order not accepted (id={oid} qty={qty:g}): {reason}")

    def on_bar(self, bar: Bar) -> None:
        moment = pd.Timestamp(bar.ts_event, unit="ns", tz="UTC")
        day = moment.date()
        if day != self._day:
            # New day: yesterday's closing position becomes today's sellable
            # quantity (T+1), and the daily participation budget resets.
            self._day = day
            self._sellable = max(0.0, self._net_position())
            self._traded_today = 0.0

        symbol = self.config.bar_type.instrument_id.symbol.value
        # Read as-of the *bar's own timestamp* — never a future bar (no look-ahead).
        weight = self.config.signals.target_weight(symbol, moment.to_pydatetime())
        price = float(bar.close.as_double())
        prev_close = self._prev_close
        self._prev_close = price

        if weight is None:
            self._record(day, 0.0, "none", "no signal as-of this bar")
            return

        target_qty = ashare.round_down_to_lot(
            weight * self.config.capital_base / price, self.config.lot_size
        )
        current_qty = self._net_position()
        delta = target_qty - current_qty

        if delta > 0:
            self._buy(day, target_qty, current_qty, delta, price, prev_close, bar)
        elif delta < 0:
            self._sell(day, target_qty, current_qty, delta, price, prev_close, bar)
        else:
            self._record(day, target_qty, "none", "already at target")

    # ── order paths ──────────────────────────────────────────────────────────

    def _buy(
        self,
        day: date,
        target_qty: float,
        current_qty: float,
        delta: float,
        price: float,
        prev_close: float | None,
        bar: Bar,
    ) -> None:
        if prev_close is not None:
            upper, _ = ashare.limit_prices(prev_close, self.config.limit_rate)
            if price >= upper:
                self._record(day, target_qty, "none", f"no chase buy at limit-up {upper:.2f}")
                return
        qty = min(delta, self._participation_remaining(bar))
        # K14-R1 C · the participation cap can be any share count, so the buy must
        # be re-rounded to a whole lot after the min() — otherwise a cap of 105
        # submits 105 shares, violating the buy-整手 rule. Sells keep odd lots
        # (A-share allows selling 零股).
        qty = ashare.round_down_to_lot(qty, self.config.lot_size)
        if qty <= 0:
            self._record(day, target_qty, "none", "participation cap reached")
            return
        self._submit(day, target_qty, OrderSide.BUY, qty, "buy")

    def _sell(
        self,
        day: date,
        target_qty: float,
        current_qty: float,
        delta: float,
        price: float,
        prev_close: float | None,
        bar: Bar,
    ) -> None:
        if prev_close is not None:
            _, lower = ashare.limit_prices(prev_close, self.config.limit_rate)
            if price <= lower:
                self._record(day, target_qty, "none", f"no chase sell at limit-down {lower:.2f}")
                return
        # T+1: cannot sell more than the start-of-day sellable quantity.
        want = min(-delta, self._sellable)
        qty = min(want, self._participation_remaining(bar))
        if qty <= 0:
            reason = "T+1: nothing sellable (today's buys locked)" if want <= 0 else "participation cap reached"
            self._record(day, target_qty, "none", reason)
            return
        self._submit(day, target_qty, OrderSide.SELL, qty, "sell")

    # ── helpers ──────────────────────────────────────────────────────────────

    def _submit(self, day: date, target_qty: float, side: OrderSide, qty: float, side_name: str) -> None:
        # K14-R1 B · a halt set by a prior rejection stops further dispatch: the
        # target is unreachable, so retrying every bar only repeats the rejection.
        if self._halted:
            self._record(day, target_qty, "none", "halted after a rejected order")
            return
        instrument = self.cache.instrument(self.config.instrument_id)
        if instrument is None:
            raise SidecarError(
                f"instrument {self.config.instrument_id} missing from cache; no order submitted"
            )
        order = self.order_factory.market(
            self.config.instrument_id, side, instrument.make_qty(qty)
        )
        self.submit_order(order)
        # K14-R1 B · do NOT debit budget/T+1 here — wait for on_order_filled (or
        # on_order_rejected) so a rejected order is not silently treated as done.
        self._pending[str(order.client_order_id)] = qty
        # K14-R1 D · (structured ``submitted`` field instead of a hardcoded 0;
        # tests read the field, not a parsed reason string.)
        self._record(day, target_qty, side_name, f"submitted {qty:g}", submitted=qty)

    def _participation_remaining(self, bar: Bar) -> float:
        """今日参与率预算余量。

        K14-R1-R1 R2 口径记档：只减**已成交**（``_traded_today``），不预留在途
        （``_pending``）子单。前提是 NT 的 bar 撮合在同一根 bar 内把 order 结清
        （submit → fill 相邻，实测）；若换撮合模型或引入跨 bar 未结清的在途单，
        该前提不成立、会出现静默超额——届时需重估（把 ``sum(_pending.values())``
        也计入预留）。
        """
        cap = ashare.participation_cap(
            float(bar.volume.as_double()), self.config.d_max
        )
        return max(0.0, cap - self._traded_today)

    def _net_position(self) -> float:
        # NT's portfolio net position for the instrument (signed: long > 0).
        return float(self.portfolio.net_position(self.config.instrument_id))

    def _record(
        self, day: date, target_qty: float, side: str, reason: str, submitted: float = 0.0
    ) -> None:
        net = self._net_position()
        self.decisions.append(
            ReplayDecision(
                day=day,
                target_qty=target_qty,
                current_qty=net,
                delta=target_qty - net,
                submitted=submitted,
                side=side,
                reason=reason,
            )
        )
