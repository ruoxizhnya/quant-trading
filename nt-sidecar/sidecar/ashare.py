"""A-share minimal rule set + fee model for the weight-replay adapter (K14).

The 口径 here is **byte-for-byte the Go kernel's**, so K15's double-run can
attribute any difference to *execution mechanism* rather than to a second,
drifting copy of the rules:

* **Fees** — mirrors ``pkg/portfolio/portfolio.go`` ``ComputeFees``::

      commission  = max(tradeValue * CommissionRate, MinCommission)
      transferFee = tradeValue * TransferFeeRate
      stampTax    = tradeValue * StampTaxRate          (sell only)

  with the rate constants from ``pkg/fees/ashare.go`` (2024-Q1 上交所/深交所).

* **Price limits** — mirrors ``pkg/backtest/pricelimit.go`` ``LimitPrices``::

      upper = round(prevClose * (1 + rate), 2)
      lower = round(prevClose * (1 - rate), 2)

  using half-away-from-zero rounding to the 0.01 tick, exactly like Go's
  ``math.Round`` (Python's built-in ``round`` is banker's rounding and would
  disagree on .005 boundaries).

* **Lot** — A-share buys are whole lots of 100 shares.
* **T+1** — shares bought today are not sellable until the next day.
* **Participation** — per-day traded shares ≤ ``D_max`` × bar volume (an
  exogenous hard cap; see the design doc §8, item 5).

Scope decision (design doc §8 item 5): the minimal set models the **main board
±10%** limit only. ST-family names, ChiNext/STAR (±20%), BSE (±30%), new-stock
first-days and call-auction mechanics are deliberately **out** — including them
would add uncontrolled variables to the K15 alignment run. Recorded, not
forgotten.
"""

from __future__ import annotations

import math
from dataclasses import dataclass

# ── lot ──────────────────────────────────────────────────────────────────────

#: A-share board lot — buys are placed in whole lots.
LOT_SIZE = 100

# ── price limits ─────────────────────────────────────────────────────────────

#: Main-board daily limit fraction (±10%). The minimal set uses this single tier.
MAIN_BOARD_LIMIT_RATE = 0.10

#: A-share equity tick size (CNY).
TICK_SIZE = 0.01

# ── fee schedule (mirrors pkg/fees/ashare.go, 2024-Q1) ───────────────────────

#: Broker commission, both sides (0.03%).
COMMISSION_RATE = 0.0003

#: Stamp tax, **sell side only** (0.05%, post-2023-08-28).
STAMP_TAX_RATE = 0.0005

#: Clearing-house transfer fee, both sides (0.001%).
TRANSFER_FEE_RATE = 0.00001

#: Minimum commission per transaction (CNY ¥5). Commission line only.
MIN_COMMISSION = 5.0


def round_to_cent(price: float) -> float:
    """Round a price to the nearest 0.01 CNY, half away from zero.

    Matches Go's ``math.Round(price*100)/100`` (``pkg/backtest/pricelimit.go``
    ``roundToCent``). Prices here are always positive.
    """
    scaled = price * 100.0
    if scaled >= 0:
        return math.floor(scaled + 0.5) / 100.0
    return math.ceil(scaled - 0.5) / 100.0


def limit_prices(prev_close: float, rate: float = MAIN_BOARD_LIMIT_RATE) -> tuple[float, float]:
    """Return ``(upper, lower)`` daily limit prices for a previous close."""
    return round_to_cent(prev_close * (1.0 + rate)), round_to_cent(prev_close * (1.0 - rate))


def round_down_to_lot(quantity: float, lot: int = LOT_SIZE) -> float:
    """Round a *buy* quantity down to a whole lot (整手). Never negative."""
    if lot <= 0:
        raise ValueError(f"lot must be positive, got {lot}")
    if quantity <= 0:
        return 0.0
    return math.floor(quantity / lot) * lot


def sellable_quantity(yesterday_close_quantity: float) -> float:
    """T+1: only yesterday's closing position is sellable today.

    Shares acquired today (``今天买的``) are not sellable until tomorrow; the
    adapter tracks the start-of-day position, so this is the settleable view.
    """
    return max(0.0, float(yesterday_close_quantity))


def participation_cap(daily_volume: float, d_max: float) -> float:
    """Max shares tradable in one day under the ``D_max`` participation cap.

    ``floor(D_max * daily_volume)`` — a **hard** constraint, not a warning. A
    non-positive ``D_max`` is a caller bug: silently returning 0 would forbid all
    trading (fail-closed would be to raise), and silently ignoring the cap would
    defeat its purpose.
    """
    if d_max <= 0:
        raise ValueError(f"D_max must be > 0, got {d_max}")
    if daily_volume <= 0:
        return 0.0
    return math.floor(d_max * daily_volume)


@dataclass(frozen=True)
class FeeBreakdown:
    """Per-trade A-share fee components (CNY), mirroring Go's FeeBreakdown."""

    commission: float
    transfer_fee: float
    stamp_tax: float

    @property
    def total(self) -> float:
        return self.commission + self.transfer_fee + self.stamp_tax


def compute_fees(
    trade_value: float,
    is_sell: bool,
    *,
    commission_rate: float = COMMISSION_RATE,
    stamp_tax_rate: float = STAMP_TAX_RATE,
    transfer_fee_rate: float = TRANSFER_FEE_RATE,
    min_commission: float = MIN_COMMISSION,
) -> FeeBreakdown:
    """A-share fee breakdown for a trade — parity with ``portfolio.ComputeFees``.

    A non-positive trade value yields zero fees (defensive, matching Go).
    """
    if trade_value <= 0:
        return FeeBreakdown(0.0, 0.0, 0.0)
    commission = max(trade_value * commission_rate, min_commission)
    transfer_fee = trade_value * transfer_fee_rate
    stamp_tax = trade_value * stamp_tax_rate if is_sell else 0.0
    return FeeBreakdown(commission, transfer_fee, stamp_tax)


class AShareFeeModel:  # noqa: D101 — see the factory below for the docstring
    """Placeholder replaced by :func:`make_fee_model` to keep NT import optional."""


def make_fee_model():  # pragma: no cover - exercised only with NT installed
    """Build the NT ``FeeModel`` implementing the A-share schedule.

    Imported lazily so this pure-rules module stays importable without
    NautilusTrader (the rule tests do not need the engine). The model returns the
    **total** A-share fee (floored commission + transfer fee + sell-side stamp
    tax) as a ``Money``, i.e. ``compute_fees(...).total`` — the same number
    ``pkg/portfolio.ComputeFees(...).Total()`` produces.
    """
    from nautilus_trader.execution import FeeModel
    from nautilus_trader.model import Money, OrderSide

    class _AShareFeeModel(FeeModel):
        def __init__(self) -> None:
            super().__init__()

        def get_commission(self, order, fill_quantity, fill_px, instrument):  # noqa: D102
            trade_value = float(fill_px.as_double()) * float(fill_quantity.as_double())
            is_sell = order.side == OrderSide.SELL
            fees = compute_fees(trade_value, is_sell)
            return Money(fees.total, instrument.quote_currency)

    return _AShareFeeModel()
