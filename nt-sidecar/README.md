# NT sidecar (K13)

A **separate Python process** that executes a backtest RunSpec on
[NautilusTrader](https://pypi.org/project/nautilus_trader/) and returns a result
JSON. The Go service reaches it over loopback through the frozen `EngineRunner`
contract; registering it under the kernel name `nautilus` is what makes the Go
kernel pluggable in practice.

Scope: this is the **K13 skeleton** — the RunSpec link, the data feed, and the Go
client. It ships a deliberately trivial **buy-and-hold demo** so the link can be
validated on real NautilusTrader. The weight-replay adapter (A-share T+1 / price
limits / fees) is K14; metric alignment with the Go kernel's canonical ResultSpec
is K15.

---

## 1. Prerequisites

* Python **3.12 – 3.14** (NautilusTrader `2.0.0rc6` declares `>=3.12,<3.15`)
* A Parquet data catalog (see §3) — or the bundled export tool plus a reachable
  Postgres with `ohlcv_daily_qfq`

NautilusTrader is pinned exactly in `requirements.txt` (`2.0.0rc6`, a
prerelease). Installing from that file works without `--pre` because the version
is exact.

## 2. One command to start

```bash
cd nt-sidecar
python -m venv .venv
.venv/bin/python -m pip install -r requirements.txt      # Windows: .venv\Scripts\python.exe
.venv/bin/python -m sidecar --port 9100 --catalog catalog
```

The service binds **127.0.0.1 only** and refuses any other address (ADR-031 D5);
starting it with `--host 0.0.0.0` exits with an error by design.

Verify it is up:

```bash
curl -s http://127.0.0.1:9100/healthz
# {"status": "ok", "kernel": "nautilus"}
```

Point the Go service at it with `NT_SIDECAR_URL` (default
`http://127.0.0.1:9100`).

Options: `--host` (loopback only) · `--port` (9100) · `--catalog` (default
`nt-sidecar/catalog`) · `--venue` (default `SIM`) · `--log-level`.

## 3. Data feed

`ohlcv_daily_qfq` → Parquet catalog, one offline ETL step:

```bash
.venv/bin/python tools/export_bars.py \
    --symbols 600000.SH,000001.SZ \
    --start 2024-01-02 --end 2024-12-31 \
    --out catalog
```

This tool reads Postgres; **the sidecar service never does**. ADR-031 D5 forbids
the sidecar from touching the database, so the service only ever reads the
Parquet catalog this step materialises. Re-run it whenever the underlying table
is refreshed.

**Adjusted-price convention** (design doc §8, item 3): `ohlcv_daily_qfq` is a
forward-adjusted (`qfq`) series and is consumed **as-is**, with no second
adjustment on the NautilusTrader side. Both kernels must read the same price
series for the K15 double-run alignment to attribute differences to execution
mechanics rather than to data.

**Timestamps**: an A-share daily bar has no intraday time, so `trade_date` maps
to **midnight UTC** in nanoseconds for both `ts_event` and `ts_init`; the bar type
is `{symbol}.{venue}-1-DAY-LAST-EXTERNAL`.

**Instruments**: each symbol becomes an `Equity` with id `{symbol}.{venue}`,
currency `CNY`, tick size `0.01`.

## 4. Wire contract

The sidecar speaks **HTTP/1.1 + JSON over loopback** (design doc §8, item 2). The
request mirrors the Go `contracts.BacktestRequest` field-for-field so the client
mapping is a straight copy.

```
POST /v1/backtest
Content-Type: application/json

{
  "strategy":        "buy_and_hold",
  "stock_pool":      ["600000.SH", "000001.SZ"],
  "start_date":      "2024-01-02",
  "end_date":        "2024-12-31",
  "initial_capital": 1000000,
  "kernel":          "nautilus",
  "engine_params":   {}
}
```

* `200` — the run completed. Body: the minimal result field set (`id`, `status`,
  `strategy`, dates, `initial_capital`, `total_return`, `sharpe_ratio`,
  `sortino_ratio`, `total_orders`, `total_positions`, `iterations`) plus
  `nt_stats`, the raw NautilusTrader statistics, for K15 to align against.
* `400` — malformed RunSpec (missing field, bad date, wrong kernel, unknown
  `engine_params`). Rejected, not ignored.
* `500` — the request was valid but the engine could not complete it (missing
  catalog data, NautilusTrader raised). Body carries the reason.

`GET /healthz` → `{"status": "ok", "kernel": "nautilus"}`.

Failures are never dressed up as success: the Go `NautilusRunner` turns every
non-2xx and every `status: "failed"` into a Go error, so a caller that asked for
`kernel: "nautilus"` never silently receives a Go-kernel result (FR-1.3).

## 5. Tests

```bash
.venv/bin/python -m unittest discover -s tests -t .
```

Runs the real NautilusTrader engine against a synthetic catalog (no Postgres, no
exported data, no network). The Go halves live in `pkg/backtest/nautilus/` and
`cmd/analysis/handlers_backtest_nautilus_test.go`; the latter includes an
end-to-end leg enabled by setting `NT_SIDECAR_URL`.

## 6. Operating notes

* **Runs are serialised.** One NautilusTrader run executes at a time
  (`threading.Lock`). The engine is CPU- and memory-heavy and deterministic
  replay is a property K15 depends on.
* **No authentication.** The service is loopback-only and is reached exclusively
  by the Go service on the same host; there is no network-facing surface to
  authenticate.
* **Licence.** NautilusTrader is LGPL-3.0. It is used **as a separate process**
  (no linking, no embedding), which is the deployment shape ADR-031 D5 chose to
  keep the licence surface at zero.

## 7. Layout

| Path | Purpose |
|---|---|
| `sidecar/runspec.py` | RunSpec DTO + fail-closed validation |
| `sidecar/catalog.py` | symbol → instrument / bar-type mapping |
| `sidecar/strategies.py` | demo strategies (buy-and-hold only, for now) |
| `sidecar/engine.py` | NautilusTrader `BacktestNode` driver + result mapping |
| `sidecar/server.py` | HTTP/JSON transport (loopback only) |
| `sidecar/__main__.py` | `python -m sidecar` entry point |
| `tools/export_bars.py` | offline `ohlcv_daily_qfq` → Parquet export |
| `tests/test_smoke.py` | engine + server smoke tests |
