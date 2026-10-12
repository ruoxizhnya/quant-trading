"""Smoke tests for the NT sidecar (K13).

Run with::

    python -m unittest discover -s tests -t .      (from nt-sidecar/)
    # or
    python -m unittest tests.test_smoke

The suite exercises the real NautilusTrader engine against a synthetic Parquet
catalog, so it needs no Postgres, no exported data, and no network — but it does
need the pinned NT install (``pip install -r requirements.txt``).
"""

from __future__ import annotations

import json
import shutil
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from decimal import Decimal
from pathlib import Path

import pandas as pd
from nautilus_trader.model import Bar, BarType, Price, Quantity
from nautilus_trader.persistence import ParquetDataCatalog

from sidecar.catalog import bar_type_str, make_equity
from sidecar.engine import run_backtest
from sidecar.errors import RunSpecError, SidecarError
from sidecar.runspec import RunSpec
from sidecar.server import build_server

SYMBOLS = ("600000.SH", "000001.SZ")
START = "2024-01-02"
END = "2024-06-28"
BARS = 120


def _write_synthetic_catalog(path: Path, symbols=SYMBOLS, bars=BARS) -> None:
    """Materialise a minimal catalog: one Equity + daily bars per symbol."""
    path.mkdir(parents=True, exist_ok=True)
    catalog = ParquetDataCatalog(str(path))
    # Deterministic, strictly increasing series — no RNG so the run is reproducible.
    prices = [10.0 + 0.05 * i for i in range(bars)]
    timestamps = pd.date_range(START, periods=bars, freq="D", tz="UTC")
    for symbol in symbols:
        instrument = make_equity(symbol, "SIM")
        catalog.write_instruments([instrument])
        bar_type = BarType.from_str(bar_type_str(symbol, "SIM"))
        catalog.write_bars(
            [
                Bar(
                    bar_type=bar_type,
                    open=Price(px, precision=2),
                    high=Price(px + 0.10, precision=2),
                    low=Price(px - 0.10, precision=2),
                    close=Price(px, precision=2),
                    volume=Quantity.from_int(1_000_000),
                    ts_event=int(ts.value),
                    ts_init=int(ts.value),
                )
                for px, ts in zip(prices, timestamps)
            ]
        )


def _valid_runspec(**overrides) -> RunSpec:
    payload = {
        "strategy": "buy_and_hold",
        "stock_pool": list(SYMBOLS),
        "start_date": START,
        "end_date": END,
    }
    payload.update(overrides)
    return RunSpec.from_json(payload)


class RunSpecValidationTest(unittest.TestCase):
    def test_accepts_a_minimal_valid_spec(self) -> None:
        spec = _valid_runspec()
        self.assertEqual(spec.stock_pool, SYMBOLS)
        self.assertEqual(spec.kernel, "nautilus")
        self.assertGreater(spec.initial_capital, 0)

    def test_rejects_malformed_specs(self) -> None:
        cases = {
            "not an object": ["nope"],
            "missing strategy": {"stock_pool": ["600000.SH"], "start_date": START, "end_date": END},
            "empty stock pool": {"strategy": "buy_and_hold", "stock_pool": [], "start_date": START, "end_date": END},
            "bad date": {"strategy": "buy_and_hold", "stock_pool": ["600000.SH"], "start_date": "02/01/2024", "end_date": END},
            "start after end": {"strategy": "buy_and_hold", "stock_pool": ["600000.SH"], "start_date": END, "end_date": START},
            "wrong kernel": {"strategy": "buy_and_hold", "stock_pool": ["600000.SH"], "start_date": START, "end_date": END, "kernel": "go"},
            "engine params": {"strategy": "buy_and_hold", "stock_pool": ["600000.SH"], "start_date": START, "end_date": END, "engine_params": {"slippage": 1}},
            "non-positive capital": {"strategy": "buy_and_hold", "stock_pool": ["600000.SH"], "start_date": START, "end_date": END, "initial_capital": -5},
        }
        for name, payload in cases.items():
            with self.subTest(case=name):
                with self.assertRaises(RunSpecError):
                    RunSpec.from_json(payload)


class EngineLinkTest(unittest.TestCase):
    """Proves RunSpec -> real NautilusTrader -> mapped result."""

    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = Path(tempfile.mkdtemp(prefix="nt-sidecar-catalog-"))
        _write_synthetic_catalog(cls._tmp)

    @classmethod
    def tearDownClass(cls) -> None:
        shutil.rmtree(cls._tmp, ignore_errors=True)

    def test_buy_and_hold_demo_returns_a_completed_result(self) -> None:
        result = run_backtest(_valid_runspec(), catalog_path=str(self._tmp))

        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["strategy"], "buy_and_hold")
        self.assertTrue(result["id"])
        # One entry order per symbol; no round trips (positions stay open).
        self.assertEqual(result["total_orders"], len(SYMBOLS))
        self.assertGreaterEqual(result["iterations"], BARS)
        self.assertIsNotNone(result["total_return"])
        self.assertIsInstance(result["total_return"], float)
        # NT's own stats ride along for K15; the payload must stay strict JSON.
        self.assertIn("nt_stats", result)
        json.dumps(result, allow_nan=False)

    def test_missing_catalog_symbol_fails_loudly(self) -> None:
        spec = _valid_runspec(stock_pool=["600000.SH", "999999.SZ"])
        with self.assertRaises(SidecarError) as ctx:
            run_backtest(spec, catalog_path=str(self._tmp))
        self.assertIn("999999.SZ", str(ctx.exception))


class ServerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = Path(tempfile.mkdtemp(prefix="nt-sidecar-server-"))
        _write_synthetic_catalog(cls._tmp)
        cls._server = build_server(host="127.0.0.1", port=0, catalog_path=str(cls._tmp))
        cls._thread = threading.Thread(target=cls._server.serve_forever, daemon=True)
        cls._thread.start()
        cls._base = f"http://127.0.0.1:{cls._server.server_address[1]}"

    @classmethod
    def tearDownClass(cls) -> None:
        cls._server.shutdown()
        cls._server.server_close()
        shutil.rmtree(cls._tmp, ignore_errors=True)

    def test_healthz(self) -> None:
        with urllib.request.urlopen(f"{self._base}/healthz", timeout=10) as resp:
            self.assertEqual(resp.status, 200)
            self.assertEqual(json.loads(resp.read())["status"], "ok")

    def test_run_endpoint_returns_completed(self) -> None:
        body = json.dumps(
            {
                "strategy": "buy_and_hold",
                "stock_pool": list(SYMBOLS),
                "start_date": START,
                "end_date": END,
                "kernel": "nautilus",
            }
        ).encode()
        req = urllib.request.Request(
            f"{self._base}/v1/backtest",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=120) as resp:
            self.assertEqual(resp.status, 200)
            payload = json.loads(resp.read())
        self.assertEqual(payload["status"], "completed")
        self.assertEqual(payload["total_orders"], len(SYMBOLS))

    def test_bad_runspec_is_400(self) -> None:
        req = urllib.request.Request(
            f"{self._base}/v1/backtest",
            data=json.dumps({"strategy": ""}).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=10)
        self.assertEqual(ctx.exception.code, 400)


class BindGuardTest(unittest.TestCase):
    def test_refuses_non_loopback_bind(self) -> None:
        with self.assertRaises(SidecarError):
            build_server(host="0.0.0.0", port=0, catalog_path=".")


if __name__ == "__main__":
    unittest.main()
