"""Export ``ohlcv_daily_qfq`` into a NautilusTrader Parquet catalog (K13 data feed).

**Offline ETL — this is not the sidecar service.** ADR-031 D5 requires the
sidecar process to have no database access; the sidecar reads only the Parquet
catalog this script materialises. Run it once (or after each data refresh)
before starting the sidecar.

Adjusted-price convention (design doc §8, item 3): ``ohlcv_daily_qfq`` is a
**forward-adjusted (qfq)** series, and it is consumed **as-is** — no further
adjustment is applied on the NT side. The double-run alignment experiment (K15)
needs both kernels to read the same price series, so any extra dividend/split
adjustment here would introduce differences that cannot be attributed to
execution mechanics.

Timestamps: an A-share daily bar has no intraday time, so ``trade_date`` is
mapped to **midnight UTC** in nanoseconds for both ``ts_event`` and ``ts_init``.

Kernel translation layer (D1 ③3): this tool is the **first consumer** of
``sidecar.mapping`` — the declarative canonical-layer → NT mapping. It holds
no per-field translation logic of its own: adding a data class means adding a
row to ``BAR_FIELD_MAP`` / ``BAR_SUFFIX_BY_FREQ`` in that module (pinned by
``tests/test_d1_mapping.py``), not editing this loop.

Usage::

    python tools/export_bars.py \
        --symbols 600000.SH,000001.SZ \
        --start 2024-01-02 --end 2024-12-31 \
        --out catalog

The ``--dsn`` default is the local development database; override it with
``--dsn`` or the ``NT_PG_DSN`` environment variable.
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

# The sidecar package sits next to this script; make it importable when the tool
# is invoked directly (e.g. `python tools/export_bars.py`).
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from sidecar.catalog import BASE_CURRENCY, make_equity  # noqa: E402 — import after sys.path tweak
from sidecar.mapping import (  # noqa: E402 — import after sys.path tweak
    FREQ_1D,
    bar_kwargs_for_row,
    bar_type_for,
)

DEFAULT_DSN = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
DEFAULT_VENUE = "SIM"

# The translation layer (sidecar.mapping.BAR_FIELD_MAP) addresses columns by
# **canonical field name** (SPEC.md drift:ohlcv json tags), and the canonical
# name for the bar's period start is `date` — not the physical column name
# `trade_date`. Alias it here so the row dict carries the canonical key;
# before this alias the exporter raised KeyError on every row (the D1 refactor
# renamed the canonical field but the SELECT kept the physical name).
QUERY = """
SELECT symbol, trade_date AS date, open, high, low, close, volume
FROM ohlcv_daily_qfq
WHERE symbol = ANY(%s)
  AND trade_date BETWEEN %s AND %s
ORDER BY symbol, trade_date
"""


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--symbols", required=True, help="comma-separated symbols, e.g. 600000.SH,000001.SZ")
    parser.add_argument("--start", required=True, help="inclusive start date (YYYY-MM-DD)")
    parser.add_argument("--end", required=True, help="inclusive end date (YYYY-MM-DD)")
    parser.add_argument("--out", required=True, help="catalog directory to write")
    parser.add_argument("--venue", default=DEFAULT_VENUE, help="simulated venue name (default: SIM)")
    parser.add_argument(
        "--dsn",
        default=os.environ.get("NT_PG_DSN", DEFAULT_DSN),
        help="Postgres DSN (default: local dev database, or $NT_PG_DSN)",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)

    import psycopg
    from psycopg.rows import dict_row
    from nautilus_trader.model import Bar, BarType
    from nautilus_trader.persistence import ParquetDataCatalog

    symbols = [s.strip() for s in args.symbols.split(",") if s.strip()]
    if not symbols:
        print("error: --symbols is empty", file=sys.stderr)
        return 2

    # dict_row: the translation layer addresses columns by canonical field
    # name (SPEC.md drift:ohlcv json tags); positional unpacking would
    # re-introduce an implicit column-order contract.
    with psycopg.connect(args.dsn) as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(QUERY, (symbols, args.start, args.end))
        rows = cur.fetchall()

    if not rows:
        print(
            f"error: no rows for {symbols} in {args.start}..{args.end} — check symbols/dates",
            file=sys.stderr,
        )
        return 1

    out_dir = Path(args.out).expanduser().resolve()
    out_dir.mkdir(parents=True, exist_ok=True)
    catalog = ParquetDataCatalog(str(out_dir))

    by_symbol: dict[str, list] = {symbol: [] for symbol in symbols}
    for row in rows:
        bar_type = BarType.from_str(bar_type_for(row["symbol"], args.venue, FREQ_1D))
        by_symbol.setdefault(row["symbol"], []).append(Bar(bar_type=bar_type, **bar_kwargs_for_row(row)))

    written: dict[str, int] = {}
    for symbol, bars in by_symbol.items():
        if not bars:
            print(f"warning: no bars for {symbol}; skipped", file=sys.stderr)
            continue
        # Instruments first: the catalog resolves bar writes against them.
        catalog.write_instruments([make_equity(symbol, args.venue)])
        catalog.write_bars(bars)
        written[symbol] = len(bars)

    if not written:
        print("error: nothing written", file=sys.stderr)
        return 1

    print(f"catalog: {out_dir}")
    for symbol, count in sorted(written.items()):
        print(f"  {symbol}: {count} daily bars ({BASE_CURRENCY} bar; qfq as-is)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
