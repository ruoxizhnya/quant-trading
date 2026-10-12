"""Demo strategies for the K13 link validation.

K13 validates the wire, not a strategy. NautilusTrader ships no built-in
buy-and-hold (its example strategies live behind an ``examples`` feature), so
this module provides the minimal demo the task calls for: **buy a fixed size on
the first bar of the window and hold it**. There is deliberately no signal,
indicator, or risk logic — real strategy behaviour arrives with the K14
weight-replay adapter.
"""

from __future__ import annotations

from decimal import Decimal

from nautilus_trader.config import StrategyConfig
from nautilus_trader.model import Bar, BarType, InstrumentId, OrderSide
from nautilus_trader.trading import Strategy

from .errors import SidecarError

#: Default notional-per-run trade size for the demo (shares).
DEFAULT_TRADE_SIZE = Decimal(10_000)


class BuyAndHoldConfig(StrategyConfig):
    """Config for :class:`BuyAndHold`.

    v2 ``StrategyConfig`` is a PyO3 type: custom fields are declared as
    keyword-only arguments, the surplus ``**_kwargs`` is forwarded untouched so
    the base class can read its own fields, and ``super().__init__()`` is called
    with no arguments.
    """

    def __init__(
        self,
        *,
        instrument_id: InstrumentId,
        bar_type: BarType,
        trade_size: Decimal,
        **_kwargs: object,
    ) -> None:
        super().__init__()
        self.instrument_id = instrument_id
        self.bar_type = bar_type
        self.trade_size = trade_size


class BuyAndHold(Strategy):
    """Enter once on the first bar, then do nothing for the rest of the window."""

    def __init__(self, config: BuyAndHoldConfig) -> None:
        super().__init__(config)
        self._entered = False

    def on_start(self) -> None:
        self.subscribe_bars(self.config.bar_type)

    def on_bar(self, bar: Bar) -> None:
        if self._entered:
            return
        self._entered = True
        instrument = self.cache.instrument(self.config.bar_type.instrument_id)
        if instrument is None:
            self.log.error(
                f"instrument {self.config.bar_type.instrument_id} missing from cache; "
                "no order submitted"
            )
            return
        order = self.order_factory.market(
            self.config.bar_type.instrument_id,
            OrderSide.BUY,
            instrument.make_qty(self.config.trade_size),
        )
        self.submit_order(order)


#: Registry of demo strategies the sidecar will serve. K14 adds the real one.
DEMO_STRATEGIES: dict[str, tuple[type[Strategy], type[StrategyConfig]]] = {
    "buy_and_hold": (BuyAndHold, BuyAndHoldConfig),
}


def build_demo_strategy(
    name: str,
    *,
    instrument_id: InstrumentId,
    bar_type: BarType,
    trade_size: Decimal = DEFAULT_TRADE_SIZE,
) -> Strategy:
    try:
        strategy_cls, config_cls = DEMO_STRATEGIES[name]
    except KeyError:
        raise SidecarError(
            f"unknown strategy {name!r}; K13 serves demo strategies only: "
            f"{sorted(DEMO_STRATEGIES)}"
        ) from None
    config = config_cls(
        instrument_id=instrument_id,
        bar_type=bar_type,
        trade_size=trade_size,
    )
    return strategy_cls(config)
