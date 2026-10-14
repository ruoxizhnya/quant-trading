"""symbol -> NautilusTrader instrument + bar-type mapping (K13).

A-share daily bars land in the catalog as ``Equity`` instruments on a single
simulated venue, with the instrument id ``{symbol}.{venue}`` (e.g.
``600000.SH.SIM``) and daily external bars
``{symbol}.{venue}-1-DAY-LAST-EXTERNAL``.

Only the *reading* side lives here; the offline export tool reuses
:func:`make_equity` so both ends agree on precision and currency by construction.
"""

from __future__ import annotations

from pathlib import Path

from nautilus_trader.model import Currency, Equity, InstrumentId, Price, Symbol
from nautilus_trader.persistence import ParquetDataCatalog

from .errors import SidecarError
# 单一真相在翻译层（sidecar.mapping）：bar 后缀表与 Price 构造精度都属于
# 「规范层 → NT」的翻译契约，catalog 反向 import 并沿用旧名（D1 ③3）。
from .mapping import BAR_SUFFIX_BY_FREQ, FREQ_1D, PRICE_PRECISION, PRICE_INCREMENT

#: Daily bars are externally aggregated by the export tool, hence ``EXTERNAL``.
#: 值来自 sidecar.mapping.BAR_SUFFIX_BY_FREQ（D1 起不再本地定义）。
DAILY_BAR_SUFFIX = BAR_SUFFIX_BY_FREQ[FREQ_1D]

#: A-share cash equity: CNY, tick size 0.01.
BASE_CURRENCY = "CNY"


def instrument_id_str(symbol: str, venue: str) -> str:
    """``("600000.SH", "SIM") -> "600000.SH.SIM"``."""
    return f"{symbol}.{venue}"


def bar_type_str(symbol: str, venue: str) -> str:
    """``("600000.SH", "SIM") -> "600000.SH.SIM-1-DAY-LAST-EXTERNAL"``."""
    return f"{instrument_id_str(symbol, venue)}-{DAILY_BAR_SUFFIX}"


def make_equity(
    symbol: str,
    venue: str,
    *,
    currency: str = BASE_CURRENCY,
    price_precision: int = PRICE_PRECISION,
    price_increment: str = PRICE_INCREMENT,
) -> Equity:
    """Build the canonical instrument for a symbol (used by the export tool).

    The three keyword arguments default to the A-share values, so every
    existing call site (K13 ``export_bars``, the K13/K14/K15 tests) keeps its
    exact behaviour. D7's foreign line passes the **market profile**'s values
    (e.g. USD for ``market.US``) so a foreign instrument is born with its own
    currency instead of being stamped ``CNY`` and re-interpreted later.
    """
    return Equity(
        instrument_id=InstrumentId.from_str(instrument_id_str(symbol, venue)),
        raw_symbol=Symbol(symbol),
        currency=Currency.from_str(currency),
        price_precision=price_precision,
        price_increment=Price.from_str(price_increment),
        ts_event=0,
        ts_init=0,
    )


def open_catalog(catalog_path: str | Path) -> ParquetDataCatalog:
    path = Path(catalog_path)
    if not path.is_dir():
        raise SidecarError(
            f"catalog path {str(path)!r} does not exist — materialise it first with "
            "`python tools/export_bars.py --symbols ... --out ...`"
        )
    return ParquetDataCatalog(str(path))


def resolve_instruments(
    catalog: ParquetDataCatalog,
    symbols: tuple[str, ...] | list[str],
    venue: str,
) -> dict[str, object]:
    """Return ``{symbol: instrument}`` for symbols present in the catalog.

    Missing symbols are reported together in one error so an operator can fix
    the export in a single pass.
    """
    wanted = [instrument_id_str(symbol, venue) for symbol in symbols]
    by_id = {inst.id.value: inst for inst in catalog.instruments(instrument_ids=wanted)}
    missing = [symbol for symbol in symbols if instrument_id_str(symbol, venue) not in by_id]
    if missing:
        raise SidecarError(
            f"catalog has no instrument for {missing} on venue {venue}; "
            "export those symbols with tools/export_bars.py"
        )
    return {symbol: by_id[instrument_id_str(symbol, venue)] for symbol in symbols}
