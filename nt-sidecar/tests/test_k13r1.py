"""K13-R1 regression legs for the NT sidecar.

Each test here is a *frozen minimal reproduction* of a self-review finding on
the K13 delivery — the ticket requires them to stay green:

* A · ``total_return`` must be the market-value net return (cash + open-position
  value), **same口径** as the Go kernel. The判別 experiment: a holding whose
  price goes 10 → 20 must show a positive return, never the cash-flow
  ``PnL% (total)`` (which is negative here and has the opposite sign).
* B · an early-exit path must not leave the request body unread on a keep-alive
  connection — the second request on the same connection must still work.
* C · ``initial_capital`` must fail-closed on ``inf`` / ``nan`` / explicit ``0``.
* 增项 · a successful run reports the server-side ``timing`` phases.

Run with::

    python -m unittest discover -s tests -t .      (from nt-sidecar/)
"""

from __future__ import annotations

import http.client
import json
import shutil
import tempfile
import threading
import unittest
from decimal import Decimal
from pathlib import Path

import pandas as pd
from nautilus_trader.model import Bar, BarType, Price, Quantity
from nautilus_trader.persistence import ParquetDataCatalog

from sidecar.catalog import bar_type_str, make_equity
from sidecar.engine import _map_result, run_backtest
from sidecar.errors import RunSpecError
from sidecar.runspec import DEFAULT_INITIAL_CAPITAL, RunSpec
from sidecar.server import build_server

SYMBOLS = ("600000.SH", "000001.SZ")
START = "2024-01-02"
END = "2024-01-10"


def _write_price_path_catalog(path: Path, prices: list[float], symbol: str = "600000.SH") -> None:
    """Materialise a one-symbol catalog whose close prices follow ``prices``."""
    path.mkdir(parents=True, exist_ok=True)
    catalog = ParquetDataCatalog(str(path))
    instrument = make_equity(symbol, "SIM")
    catalog.write_instruments([instrument])
    bar_type = BarType.from_str(bar_type_str(symbol, "SIM"))
    timestamps = pd.date_range(START, periods=len(prices), freq="D", tz="UTC")
    catalog.write_bars(
        [
            Bar(
                bar_type=bar_type,
                open=Price(px, precision=2),
                high=Price(px + 0.10, precision=2),
                low=Price(px - 0.10, precision=2),
                close=Price(px, precision=2),
                # 1,000,000 shares/day keeps the demo trade (10,000) well inside
                # any participation cap.
                volume=Quantity.from_int(1_000_000),
                ts_event=int(ts.value),
                ts_init=int(ts.value),
            )
            for px, ts in zip(prices, timestamps)
        ]
    )


def _runspec(**overrides) -> RunSpec:
    payload = {
        "strategy": "buy_and_hold",
        "stock_pool": ["600000.SH"],
        "start_date": START,
        "end_date": END,
    }
    payload.update(overrides)
    return RunSpec.from_json(payload)


class InitialCapitalValidationTest(unittest.TestCase):
    """K13-R1 C · fail-closed capital validation."""

    def test_missing_capital_uses_documented_default(self) -> None:
        self.assertEqual(_runspec().initial_capital, DEFAULT_INITIAL_CAPITAL)

    def test_rejects_non_finite_and_non_positive(self) -> None:
        cases = {
            "inf": float("inf"),
            "nan": float("nan"),
            "explicit zero": 0,
            "negative": -1,
            "not a number": "abc",
        }
        for name, value in cases.items():
            with self.subTest(case=name):
                with self.assertRaises(RunSpecError):
                    _runspec(initial_capital=value)

    def test_accepts_a_positive_capital(self) -> None:
        self.assertEqual(_runspec(initial_capital=250_000).initial_capital, 250_000)


class TotalReturnCaliberTest(unittest.TestCase):
    """K13-R1 A · market-value net return, not the cash-flow PnL%."""

    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = Path(tempfile.mkdtemp(prefix="nt-k13r1-a-"))
        # Price doubles: a buy-and-hold entry at 10 ends at 20.
        _write_price_path_catalog(cls._tmp, [10.0, 20.0, 20.0])

    @classmethod
    def tearDownClass(cls) -> None:
        shutil.rmtree(cls._tmp, ignore_errors=True)

    def test_price_doubling_yields_a_positive_net_return(self) -> None:
        result = run_backtest(
            _runspec(initial_capital=1_000_000),
            catalog_path=str(self._tmp),
        )

        total_return = result["total_return"]
        self.assertIsInstance(total_return, float)
        # 10,000 shares bought at 10 (=100k of the 1,000,000 capital) then marked
        # at 20 → end equity 1,100,000 → +0.10. The old cash-flow mapping would
        # have reported −0.10 (opposite sign).
        self.assertGreater(total_return, 0.0)
        self.assertAlmostEqual(total_return, 0.10, places=6)

        # The cash-flow PnL% is still carried, but only as raw provenance.
        balance = result["nt_stats"]["nt_balance_pnl_pct"]
        self.assertIsNotNone(balance)
        self.assertLess(balance, 0.0)
        self.assertEqual(
            (total_return > 0) != (balance > 0),
            True,
            "the two口径 must be distinguishable — the self-review found them sign-opposite",
        )

    def test_end_equity_is_market_value(self) -> None:
        result = run_backtest(
            _runspec(initial_capital=1_000_000),
            catalog_path=str(self._tmp),
        )
        self.assertAlmostEqual(result["nt_stats"]["nt_end_equity"], 1_100_000.0, places=2)


class TimingReportTest(unittest.TestCase):
    """增项 · the sidecar reports the server-side queue/exec phases."""

    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = Path(tempfile.mkdtemp(prefix="nt-k13r1-t-"))
        _write_price_path_catalog(cls._tmp, [10.0 + 0.05 * i for i in range(20)])

    @classmethod
    def tearDownClass(cls) -> None:
        shutil.rmtree(cls._tmp, ignore_errors=True)

    def test_run_reports_timing(self) -> None:
        server = build_server(host="127.0.0.1", port=0, catalog_path=str(self._tmp))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            conn = http.client.HTTPConnection("127.0.0.1", server.server_address[1], timeout=120)
            conn.request(
                "POST",
                "/v1/backtest",
                body=json.dumps(
                    {
                        "strategy": "buy_and_hold",
                        "stock_pool": ["600000.SH"],
                        "start_date": START,
                        "end_date": "2024-01-20",
                    }
                ),
                headers={"Content-Type": "application/json"},
            )
            resp = conn.getresponse()
            payload = json.loads(resp.read())
            conn.close()
        finally:
            server.shutdown()
            server.server_close()

        self.assertEqual(resp.status, 200)
        self.assertIn("timing", payload)
        timing = payload["timing"]
        for key in ("queue_ms", "exec_ms", "total_ms"):
            self.assertIn(key, timing)
            self.assertIsInstance(timing[key], (int, float))
        self.assertGreaterEqual(timing["total_ms"], 0.0)


class _StubResult:
    """Minimal stand-in for an NT run result — only the fields ``_map_result`` reads."""

    run_config_id = "nt-run-stub"
    stats_returns: dict = {}
    stats_pnls: dict = {}
    stats_general: dict = {}
    total_orders = 1
    total_positions = 1
    iterations = 3


class MissingEndEquityIsFailedTest(unittest.TestCase):
    """K13-R1-R1 A (b) · 读不到期末市值净值 ⇒ status=failed，绝不折成 0。"""

    def test_unreadable_equity_yields_failed_not_a_zeroed_success(self) -> None:
        # portfolio=None models "the engine portfolio could not be read" — the
        # exact case the pre-fix code folded into completed + total_return=0.
        payload = _map_result(_runspec(), "nt-run-stub", _StubResult(), portfolio=None)

        self.assertEqual("failed", payload["status"])
        self.assertIsNone(payload["total_return"], "a missing metric must stay absent, not 0")
        self.assertIn("total_return", payload["error"])


class KeepAliveFramingTest(unittest.TestCase):
    """K13-R1 B · an early-exit 404 must not desync a keep-alive connection."""

    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = Path(tempfile.mkdtemp(prefix="nt-k13r1-b-"))
        _write_price_path_catalog(cls._tmp, [10.0, 11.0, 12.0])
        cls._server = build_server(host="127.0.0.1", port=0, catalog_path=str(cls._tmp))
        cls._thread = threading.Thread(target=cls._server.serve_forever, daemon=True)
        cls._thread.start()

    @classmethod
    def tearDownClass(cls) -> None:
        cls._server.shutdown()
        cls._server.server_close()
        shutil.rmtree(cls._tmp, ignore_errors=True)

    def test_second_request_on_same_connection_is_not_desynced(self) -> None:
        conn = http.client.HTTPConnection(
            "127.0.0.1", self._server.server_address[1], timeout=30
        )
        try:
            # 1 · POST to a *wrong* path with a body → 404 (the early-exit path).
            body = json.dumps(
                {"strategy": "buy_and_hold", "stock_pool": ["600000.SH"],
                 "start_date": START, "end_date": END}
            )
            conn.request(
                "POST",
                "/nt/v1/backtest",  # note the bogus /nt prefix
                body=body,
                headers={"Content-Type": "application/json"},
            )
            first = conn.getresponse()
            first.read()
            self.assertEqual(first.status, 404)

            # 2 · the very next request on the *same* connection must be clean.
            conn.request("GET", "/healthz")
            second = conn.getresponse()
            payload = json.loads(second.read())
            self.assertEqual(
                second.status,
                200,
                "a kept-alive connection was desynced by the unread 404 body",
            )
            self.assertEqual(payload["status"], "ok")
        finally:
            conn.close()

    def test_get_with_body_does_not_desync_a_kept_alive_connection(self) -> None:
        """K13-R1-R1 B · a GET carrying a body must also drain before its 404."""
        conn = http.client.HTTPConnection(
            "127.0.0.1", self._server.server_address[1], timeout=30
        )
        try:
            conn.request(
                "GET",
                "/no-such-route",
                body="ignored-by-the-server",
                headers={"Content-Type": "text/plain"},
            )
            first = conn.getresponse()
            first.read()
            self.assertEqual(first.status, 404)

            conn.request("GET", "/healthz")
            second = conn.getresponse()
            payload = json.loads(second.read())
            self.assertEqual(
                second.status,
                200,
                "a kept-alive connection was desynced by the unread GET 404 body",
            )
            self.assertEqual(payload["status"], "ok")
        finally:
            conn.close()


if __name__ == "__main__":
    unittest.main()
