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

#: Daily bars are externally aggregated by the export tool, hence ``EXTERNAL``.
DAILY_BAR_SUFFIX = "1-DAY-LAST-EXTERNAL"

#: A-share cash equity: CNY, tick size 0.01.
BASE_CURRENCY = "CNY"
PRICE_PRECISION = 2
PRICE_INCREMENT = "0.01"


def instrument_id_str(symbol: str, venue: str) -> str:
    """``("600000.SH", "SIM") -> "600000.SH.SIM"``."""
    return f"{symbol}.{venue}"


def bar_type_str(symbol: str, venue: str) -> str:
    """``("600000.SH", "SIM") -> "600000.SH.SIM-1-DAY-LAST-EXTERNAL"``."""
    return f"{instrument_id_str(symbol, venue)}-{DAILY_BAR_SUFFIX}"


def make_equity(symbol: str, venue: str) -> Equity:
    """Build the canonical instrument for a symbol (used by the export tool)."""
    return Equity(
        instrument_id=InstrumentId.from_str(instrument_id_str(symbol, venue)),
        raw_symbol=Symbol(symbol),
        currency=Currency.from_str(BASE_CURRENCY),
        price_precision=PRICE_PRECISION,
        price_increment=Price.from_str(PRICE_INCREMENT),
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
