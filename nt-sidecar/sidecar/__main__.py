"""Entry point: ``python -m sidecar`` (K13).

Starts the loopback RunSpec execution service. See ``README.md`` for the
one-command start, the wire contract, and the data-feed workflow.
"""

from __future__ import annotations

import argparse
import logging
import sys
from pathlib import Path

from .engine import DEFAULT_VENUE
from .errors import SidecarError
from .markets import DEFAULT_MARKET, known_markets
from .server import build_server, serve_forever

DEFAULT_PORT = 9100
DEFAULT_CATALOG = Path(__file__).resolve().parent.parent / "catalog"


def _parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="sidecar",
        description="NautilusTrader RunSpec sidecar (loopback-only, K13).",
    )
    parser.add_argument(
        "--host",
        default="127.0.0.1",
        help="bind address; must be a loopback host (default: 127.0.0.1)",
    )
    parser.add_argument("--port", type=int, default=DEFAULT_PORT, help="bind port (default: 9100)")
    parser.add_argument(
        "--catalog",
        default=str(DEFAULT_CATALOG),
        help="Parquet data catalog produced by tools/export_bars.py (default: nt-sidecar/catalog)",
    )
    parser.add_argument("--venue", default=DEFAULT_VENUE, help="simulated venue name (default: SIM)")
    parser.add_argument(
        "--market",
        default=DEFAULT_MARKET,
        help="market profile this instance serves: "
        f"{', '.join(known_markets())} (default: {DEFAULT_MARKET})",
    )
    parser.add_argument(
        "--log-level",
        default="INFO",
        choices=["DEBUG", "INFO", "WARNING", "ERROR"],
        help="log verbosity (default: INFO)",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(argv)
    logging.basicConfig(
        level=getattr(logging, args.log_level),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        stream=sys.stderr,
    )
    try:
        server = build_server(
            host=args.host,
            port=args.port,
            catalog_path=args.catalog,
            venue=args.venue,
            market=args.market,
        )
    except SidecarError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    try:
        serve_forever(server)
    except KeyboardInterrupt:
        print("shutting down", file=sys.stderr)
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
