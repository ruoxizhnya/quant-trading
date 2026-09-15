#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""跨源数据质量抽检（桥 B3 / 改造项 C-7 / 任务 EQD-P0-2）

把 EquityDeep 的 M1 数据质量抽检方法泛化为**跨源通用脚本**：任一侧读数作为被检源，
另一侧作为参照源，逐单元格比对，输出错误率与明细。

    判定: 错误率 = 不一致单元格 / 可比单元格 < 2% → PASS
         （阈值可用 --threshold 覆盖）

为什么用「读数转储」而不是直连数据源（ADR-022 的有意选择）:
    L0 数据面是取数的唯一入口，脚本不得绕过它去直连 tushare/akshare。
    两侧读数都以同一种转储格式出口（JSONL，每行一条 C1 行形状记录，
    见 contracts/fundamentals_detail.schema.sql），脚本只消费转储：
      - tushare 侧：L0 归档（ingest.raw）→ 白名单映射后的读数导出
      - akshare 侧：工作面 1 经 POST /api/ingest/raw 上报 → 同法导出
    待 L0 暴露 20 字段的读取端点后，只需新增一个 URL 转储适配器，比较核心不变。

三项检查（各自消费一份冻结契约）:
    1. 白名单校验  raw_field_name 必须 ∈ contracts/field_dictionary.yaml 的 raw_names
                   —— 白名单之外的映射不得猜测（RESEARCH §3.2）
    2. 单位换算    base_value = value × unit_scale[unit]，显式换算表，禁止 variants() 式启发式
                   —— RESEARCH §3.8 加固项 #2
    3. 双源比对    |target - ref| / |ref| ≤ tolerance（默认 1%）→ 一致
                   —— tolerance 与 citation 的默认容差一致（RESEARCH §3.8 #1）

退出码: 0 = PASS, 1 = FAIL, 3 = INCONCLUSIVE（可比单元格为 0）, 4 = ERROR（用法/契约/转储错误）

用法:
    python evals/data_quality/spot_check.py --source akshare
    python evals/data_quality/spot_check.py --source tushare --ref akshare --json out.json
    python evals/data_quality/spot_check.py --source akshare --tickers 600519,000858 --fields total_revenue,ocf_net
    python evals/data_quality/spot_check.py --self-test
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import tempfile
import urllib.request
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation, getcontext
from pathlib import Path
from typing import Iterable, Iterator

getcontext().prec = 28

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[1]
DEFAULT_CONTRACTS_DIR = REPO_ROOT / "contracts"
DEFAULT_DUMP_DIR = SCRIPT_DIR / "dumps"
SOURCES = ("akshare", "tushare")

DEFAULT_THRESHOLD = Decimal("0.02")
DEFAULT_TOLERANCE = Decimal("0.01")

EXIT_PASS = 0
EXIT_FAIL = 1
EXIT_INCONCLUSIVE = 3
EXIT_ERROR = 4

DUMP_REQUIRED_COLUMNS = (
    "ts_code",
    "end_date",
    "ann_date",
    "field_code",
    "raw_field_name",
    "value",
    "unit",
    "source",
)

DUMP_HINT = (
    "转储格式：JSONL，每行一条 C1 行形状记录（见 contracts/fundamentals_detail.schema.sql），例如：\n"
    '  {"ts_code":"600519.SH","end_date":"2025-09-30","ann_date":"2025-10-25",'
    '"field_code":"total_revenue","raw_field_name":"营业总收入",'
    '"value":127854000000,"unit":"CNY","source":"tushare","content_hash":"<64 hex>"}'
)

_TICKER_SUFFIX = re.compile(r"\.(SH|SZ|BJ)$", re.IGNORECASE)
_TICKER_CODE = re.compile(r"^[0-9]{6}$")


class UsageError(Exception):
    """用法、契约或转储层面的错误（对应退出码 4）。"""


# --------------------------------------------------------------------------- 契约


@dataclass(frozen=True)
class FieldSpec:
    field_code: str
    raw_names: tuple[str, ...]
    unit: str
    statement: str


class FieldDictionary:
    """读取并校验 contracts/field_dictionary.yaml（契约 C1-a）。"""

    def __init__(self, path: Path, doc: dict) -> None:
        self.path = path
        self.version = doc.get("version")
        self.status = doc.get("status")
        self.base_unit = doc.get("base_unit") or "CNY"
        self.unit_scale = _load_unit_scale(doc, path)
        self.fields = _load_fields(doc, path)
        self.default_tickers = _load_default_tickers(doc, path)

    @classmethod
    def load(cls, path: Path) -> "FieldDictionary":
        if not path.exists():
            raise UsageError(f"字段字典不存在: {path}")
        try:
            import yaml
        except ImportError as exc:  # pragma: no cover - 环境相关
            raise UsageError(
                "缺少 PyYAML：本脚本用 PyYAML 读取契约 "
                f"{path.name}（脚本其余部分仅用标准库）。请先 `pip install pyyaml`。"
            ) from exc
        try:
            doc = yaml.safe_load(path.read_text(encoding="utf-8"))
        except Exception as exc:  # noqa: BLE001 - 需要把 YAML 错误原样报给使用者
            raise UsageError(f"解析字段字典失败 {path}: {exc}") from exc
        if not isinstance(doc, dict):
            raise UsageError(f"字段字典根节点必须是映射: {path}")
        return cls(path, doc)

    def to_base(self, value: Decimal, unit: str) -> Decimal:
        """按显式换算表折算到 base_unit；未知单位直接报错，不做猜测。"""
        if unit not in self.unit_scale:
            raise KeyError(unit)
        return value * self.unit_scale[unit]

    def accepts(self, field_code: str, raw_field_name: str) -> bool:
        spec = self.fields.get(field_code)
        return spec is not None and raw_field_name in spec.raw_names


def _load_unit_scale(doc: dict, path: Path) -> dict[str, Decimal]:
    raw = doc.get("unit_scale")
    if not isinstance(raw, dict) or not raw:
        raise UsageError(f"字段字典缺少非空的 unit_scale 段: {path}")
    scale: dict[str, Decimal] = {}
    for unit, factor in raw.items():
        try:
            scale[str(unit)] = Decimal(str(factor))
        except InvalidOperation as exc:
            raise UsageError(f"unit_scale[{unit}] 非数值: {factor!r}") from exc
    return scale


def _load_fields(doc: dict, path: Path) -> dict[str, FieldSpec]:
    raw = doc.get("fields")
    if not isinstance(raw, list) or not raw:
        raise UsageError(f"字段字典缺少非空的 fields 段: {path}")
    fields: dict[str, FieldSpec] = {}
    for idx, item in enumerate(raw):
        if not isinstance(item, dict):
            raise UsageError(f"fields[{idx}] 必须是映射: {path}")
        code = item.get("field_code")
        names = item.get("raw_names")
        if not isinstance(code, str) or not code:
            raise UsageError(f"fields[{idx}] 缺少 field_code: {path}")
        if code in fields:
            raise UsageError(f"fields 中 field_code 重复: {code}")
        if not isinstance(names, list) or not all(isinstance(n, str) and n for n in names):
            raise UsageError(f"fields[{code}] 的 raw_names 必须是非空字符串列表")
        fields[code] = FieldSpec(
            field_code=code,
            raw_names=tuple(names),
            unit=str(item.get("unit") or ""),
            statement=str(item.get("statement") or ""),
        )
    return fields


def _load_default_tickers(doc: dict, path: Path) -> tuple[str, ...]:
    raw = (doc.get("spot_check_defaults") or {}).get("tickers")
    if not isinstance(raw, list) or not raw:
        raise UsageError(f"字段字典缺少 spot_check_defaults.tickers: {path}")
    return tuple(normalize_ticker(str(t)) for t in raw)


# --------------------------------------------------------------------------- 读数


@dataclass(frozen=True)
class Reading:
    """一条读数 —— 与 C1 表行同构。"""

    ticker: str  # 归一化 6 位代码
    ts_code: str
    end_date: str
    ann_date: str
    field_code: str
    raw_field_name: str
    value: Decimal
    unit: str
    source: str
    content_hash: str | None

    @property
    def cell(self) -> tuple[str, str, str]:
        return (self.ticker, self.end_date, self.field_code)


def normalize_ticker(code: str) -> str:
    """600519.SH / 600519 → 600519（交易所在比对时无关）。"""
    return _TICKER_SUFFIX.sub("", code.strip().upper())


def iter_dump_lines(spec: str) -> Iterator[str]:
    if spec == "-":
        yield from sys.stdin
        return
    if spec.startswith(("http://", "https://")):
        try:
            with urllib.request.urlopen(spec, timeout=60) as resp:  # noqa: S310 - 由使用者显式指定
                yield from resp.read().decode("utf-8").splitlines()
        except Exception as exc:  # noqa: BLE001 - 网络错误需原样报出
            raise UsageError(f"拉取转储失败 {spec}: {exc}") from exc
        return
    path = Path(spec)
    if not path.exists():
        raise UsageError(f"转储文件不存在: {path}\n{DUMP_HINT}")
    with path.open("r", encoding="utf-8") as handle:
        yield from handle


def load_readings(spec: str, source: str) -> tuple[list[Reading], list[str]]:
    """读入转储；返回（读数, 结构性问题列表）。结构性问题不计入比对。"""
    readings: list[Reading] = []
    problems: list[str] = []
    for lineno, line in enumerate(iter_dump_lines(spec), start=1):
        text = line.strip()
        if not text:
            continue
        try:
            row = json.loads(text)
        except json.JSONDecodeError as exc:
            problems.append(f"{spec}:{lineno}: JSON 解析失败: {exc}")
            continue
        if not isinstance(row, dict):
            problems.append(f"{spec}:{lineno}: 每行必须是一个 JSON 对象")
            continue
        absent = [col for col in DUMP_REQUIRED_COLUMNS if col not in row]
        if absent:
            problems.append(f"{spec}:{lineno}: 缺少必填列 {', '.join(absent)}")
            continue
        if str(row["source"]) != source:
            problems.append(
                f"{spec}:{lineno}: source={row['source']!r} 与 --source {source} 不符"
            )
            continue
        if row["value"] is None:
            continue  # 源侧缺失：该单元格不参与比对
        try:
            value = Decimal(str(row["value"]))
        except InvalidOperation:
            problems.append(f"{spec}:{lineno}: value 非数值: {row['value']!r}")
            continue
        readings.append(
            Reading(
                ticker=normalize_ticker(str(row["ts_code"])),
                ts_code=str(row["ts_code"]),
                end_date=str(row["end_date"]),
                ann_date=str(row["ann_date"]),
                field_code=str(row["field_code"]),
                raw_field_name=str(row["raw_field_name"]),
                value=value,
                unit=str(row["unit"]),
                source=str(row["source"]),
                content_hash=row.get("content_hash"),
            )
        )
    return readings, problems


# --------------------------------------------------------------------------- 比对


@dataclass
class CellResult:
    ticker: str
    end_date: str
    field_code: str
    status: str  # ok | mismatch | missing | violation
    target: str = ""
    ref: str = ""
    note: str = ""

    def as_dict(self) -> dict:
        return {
            "ticker": self.ticker,
            "end_date": self.end_date,
            "field_code": self.field_code,
            "status": self.status,
            "target": self.target,
            "ref": self.ref,
            "note": self.note,
        }


@dataclass
class Report:
    source: str
    ref_source: str
    threshold: Decimal
    tolerance: Decimal
    tickers: tuple[str, ...]
    fields: tuple[str, ...]
    cells: list[CellResult]
    dump_problems: list[str]
    dictionary_note: str

    @property
    def total(self) -> int:
        return len(self.cells)

    def count(self, status: str) -> int:
        return sum(1 for cell in self.cells if cell.status == status)

    @property
    def comparable(self) -> int:
        return self.count("ok") + self.count("mismatch")

    @property
    def error_rate(self) -> Decimal | None:
        if self.comparable == 0:
            return None
        return Decimal(self.count("mismatch")) / Decimal(self.comparable)

    @property
    def verdict(self) -> str:
        rate = self.error_rate
        if rate is None:
            return "INCONCLUSIVE"
        return "PASS" if rate < self.threshold else "FAIL"

    @property
    def exit_code(self) -> int:
        verdict = self.verdict
        if verdict == "PASS":
            return EXIT_PASS
        if verdict == "FAIL":
            return EXIT_FAIL
        return EXIT_INCONCLUSIVE

    def as_dict(self) -> dict:
        rate = self.error_rate
        return {
            "source": self.source,
            "ref_source": self.ref_source,
            "threshold": str(self.threshold),
            "tolerance": str(self.tolerance),
            "tickers": list(self.tickers),
            "fields": list(self.fields),
            "verdict": self.verdict,
            "summary": {
                "cells": self.total,
                "comparable": self.comparable,
                "ok": self.count("ok"),
                "mismatch": self.count("mismatch"),
                "missing": self.count("missing"),
                "violation": self.count("violation"),
                "error_rate": None if rate is None else f"{rate:.6f}",
            },
            "mismatches": [c.as_dict() for c in self.cells if c.status == "mismatch"],
            "violations": [c.as_dict() for c in self.cells if c.status == "violation"],
            "missing": [c.as_dict() for c in self.cells if c.status == "missing"],
            "dump_problems": self.dump_problems,
            "dictionary": self.dictionary_note,
        }


def relative_diff(target: Decimal, ref: Decimal) -> Decimal | None:
    """相对差；ref 为 0 时无法用相对差表达（返回 None，由调用方判为不一致）。"""
    if ref == 0:
        return Decimal(0) if target == 0 else None
    return abs(target - ref) / abs(ref)


def run_spot_check(
    target_readings: Iterable[Reading],
    ref_readings: Iterable[Reading],
    dictionary: FieldDictionary,
    tickers: tuple[str, ...],
    fields: tuple[str, ...],
    source: str,
    ref_source: str,
    threshold: Decimal,
    tolerance: Decimal,
    dump_problems: list[str],
) -> Report:
    ticker_set = set(tickers)
    field_set = set(fields)
    ref_index = {reading.cell: reading for reading in ref_readings}

    cells: list[CellResult] = []
    for reading in target_readings:
        if reading.ticker not in ticker_set or reading.field_code not in field_set:
            continue
        cell = CellResult(
            ticker=reading.ticker,
            end_date=reading.end_date,
            field_code=reading.field_code,
            status="ok",
        )
        # 检查 1：白名单（含 field_code 是否在字典内）
        if not dictionary.accepts(reading.field_code, reading.raw_field_name):
            cell.status = "violation"
            cell.note = (
                f"raw_field_name {reading.raw_field_name!r} 不在 field_code "
                f"{reading.field_code!r} 的白名单内（禁止启发式翻译）"
            )
            cells.append(cell)
            continue
        # 检查 2：单位换算用显式换算表
        try:
            target_value = dictionary.to_base(reading.value, reading.unit)
        except KeyError:
            cell.status = "violation"
            cell.note = f"unit {reading.unit!r} 不在 unit_scale 换算表中"
            cells.append(cell)
            continue
        counterpart = ref_index.get(reading.cell)
        if counterpart is None:
            cell.status = "missing"
            cell.target = _render(target_value, dictionary.base_unit)
            cell.note = "参照源无同 (标的, 报告期, 字段) 读数"
            cells.append(cell)
            continue
        try:
            ref_value = dictionary.to_base(counterpart.value, counterpart.unit)
        except KeyError:
            cell.status = "violation"
            cell.note = f"参照源 unit {counterpart.unit!r} 不在 unit_scale 换算表中"
            cells.append(cell)
            continue
        # 检查 3：双源比对
        cell.target = _render(target_value, dictionary.base_unit)
        cell.ref = _render(ref_value, dictionary.base_unit)
        diff = relative_diff(target_value, ref_value)
        if diff is None:
            cell.status = "mismatch"
            cell.note = "参照值为 0 而目标值非 0，相对差不可比"
        elif diff > tolerance:
            cell.status = "mismatch"
            cell.note = f"相对差 {diff:.4%} > 容差 {tolerance:.2%}"
        cells.append(cell)

    return Report(
        source=source,
        ref_source=ref_source,
        threshold=threshold,
        tolerance=tolerance,
        tickers=tickers,
        fields=fields,
        cells=cells,
        dump_problems=dump_problems,
        dictionary_note=(
            f"{dictionary.path.name} version={dictionary.version} "
            f"status={dictionary.status} fields={len(dictionary.fields)}"
        ),
    )


def _render(value: Decimal, unit: str) -> str:
    return f"{value.normalize():f} {unit}"


# --------------------------------------------------------------------------- 输出


def render_report(report: Report) -> str:
    lines: list[str] = []
    lines.append("跨源数据质量抽检（桥 B3 / C-7）")
    lines.append(f"  被检源   : {report.source}")
    lines.append(f"  参照源   : {report.ref_source}")
    lines.append(f"  契约     : {report.dictionary_note}")
    lines.append(f"  抽检集   : {len(report.tickers)} 票 × {len(report.fields)} 字段")
    lines.append(
        f"  判定阈值 : 错误率 < {report.threshold:.2%}（容差 {report.tolerance:.2%}）"
    )
    lines.append("")
    lines.append(
        f"  单元格 {report.total} | 可比 {report.comparable} | "
        f"一致 {report.count('ok')} | 不一致 {report.count('mismatch')} | "
        f"缺失 {report.count('missing')} | 违约 {report.count('violation')}"
    )
    rate = report.error_rate
    lines.append(f"  错误率   : {'n/a' if rate is None else f'{rate:.4%}'}")
    lines.append(f"  判定     : {report.verdict}")
    lines.append("")

    _append_detail(lines, "不一致明细", report, "mismatch")
    _append_detail(lines, "契约违约明细（已排除出比对）", report, "violation")

    missing = [cell for cell in report.cells if cell.status == "missing"]
    if missing:
        lines.append(f"缺失明细（{len(missing)} 条）：")
        for cell in missing[:20]:
            lines.append(f"  - {cell.ticker} {cell.end_date} {cell.field_code}: {cell.note}")
        if len(missing) > 20:
            lines.append(f"  ... 其余 {len(missing) - 20} 条见 --json 报告")

    if report.dump_problems:
        lines.append("")
        lines.append(f"转储结构性问题（{len(report.dump_problems)} 条，未计入比对）：")
        for problem in report.dump_problems[:20]:
            lines.append(f"  - {problem}")
        if len(report.dump_problems) > 20:
            lines.append(f"  ... 其余 {len(report.dump_problems) - 20} 条见 --json 报告")

    if report.comparable == 0:
        lines.append("")
        lines.append(
            "INCONCLUSIVE：可比单元格为 0 —— 两侧转储没有共同覆盖的 (标的, 报告期, 字段)。"
        )
    return "\n".join(lines)


def _append_detail(lines: list[str], title: str, report: Report, status: str) -> None:
    hits = [cell for cell in report.cells if cell.status == status]
    if not hits:
        return
    lines.append(f"{title}（{len(hits)} 条）：")
    for cell in hits:
        detail = f"  - {cell.ticker} {cell.end_date} {cell.field_code}: "
        if status == "mismatch":
            detail += f"{cell.target} vs {cell.ref}（{cell.note}）"
        else:
            detail += cell.note
        lines.append(detail)
    lines.append("")


# --------------------------------------------------------------------------- 自检


def _write_fixture(
    path: Path,
    dictionary: FieldDictionary,
    tickers: tuple[str, ...],
    fields: tuple[str, ...],
    source: str,
    mismatch_at: frozenset[tuple[str, str]],
    omit_at: frozenset[tuple[str, str]],
    unit_for: dict[str, str],
    bad_name_at: frozenset[tuple[str, str]],
) -> None:
    rows: list[str] = []
    for ticker in tickers:
        for field_code in fields:
            if (ticker, field_code) in omit_at:
                continue
            base = Decimal(1_000_000) * (1 + tickers.index(ticker)) * (1 + fields.index(field_code))
            unit = unit_for.get(field_code, "CNY")
            value = base
            if (ticker, field_code) in mismatch_at:
                value = base * 2  # 相对差 100%，确保超过任意容差
            if unit == "CNY_10k":
                value = value / Decimal(10_000)
            raw_name = dictionary.fields[field_code].raw_names[0]
            if (ticker, field_code) in bad_name_at:
                raw_name = "营业总收入(未经白名单确认)"
            rows.append(
                json.dumps(
                    {
                        "ts_code": f"{ticker}.SH",
                        "end_date": "2025-09-30",
                        "ann_date": "2025-10-25",
                        "field_code": field_code,
                        "raw_field_name": raw_name,
                        "value": str(value),
                        "unit": unit,
                        "source": source,
                        "content_hash": "0" * 64,
                    },
                    ensure_ascii=False,
                )
            )
    path.write_text("\n".join(rows) + "\n", encoding="utf-8")


def run_self_test(dictionary: FieldDictionary) -> int:
    failures: list[str] = []
    tickers = tuple(dictionary.default_tickers)
    fields = tuple(dictionary.fields)
    violations = frozenset({(tickers[0], fields[0])})  # 白名单之外的原始字段名
    missing = frozenset({(tickers[1], fields[1])})  # 参照源缺该单元格
    reserved = violations | missing

    def mismatch_cells(count: int) -> frozenset[tuple[str, str]]:
        """构造互异、且不与违约/缺失单元格重叠的不一致集。"""
        picked: set[tuple[str, str]] = set()
        i = 0
        while len(picked) < count and i < len(tickers) * len(fields):
            cell = (tickers[i % len(tickers)], fields[(i * 3) % len(fields)])
            if cell not in reserved:
                picked.add(cell)
            i += 1
        return frozenset(picked)

    with tempfile.TemporaryDirectory() as tmpdir:
        tmp = Path(tmpdir)

        for mismatch_count, expected_verdict in ((3, "PASS"), (8, "FAIL")):
            mismatch = mismatch_cells(mismatch_count)
            if len(mismatch) != mismatch_count:
                failures.append(
                    f"自检无法构造 {mismatch_count} 个互异的不一致单元格"
                )
                continue
            target_path = tmp / f"target_{mismatch_count}.jsonl"
            ref_path = tmp / f"ref_{mismatch_count}.jsonl"
            # 目标源：全部字段以 CNY_10k 上报，借以验证显式换算表；其中 1 条白名单违约
            _write_fixture(
                target_path,
                dictionary,
                tickers,
                fields,
                "akshare",
                mismatch_at=mismatch,
                omit_at=frozenset(),
                unit_for={code: "CNY_10k" for code in fields},
                bad_name_at=violations,
            )
            _write_fixture(
                ref_path,
                dictionary,
                tickers,
                fields,
                "tushare",
                mismatch_at=frozenset(),
                omit_at=missing,
                unit_for={},
                bad_name_at=frozenset(),
            )
            target_readings, problems_a = load_readings(str(target_path), "akshare")
            ref_readings, problems_b = load_readings(str(ref_path), "tushare")
            if problems_a or problems_b:
                failures.append(f"自检转储结构性问题: {problems_a + problems_b}")
                continue
            report = run_spot_check(
                target_readings,
                ref_readings,
                dictionary,
                tickers,
                fields,
                source="akshare",
                ref_source="tushare",
                threshold=DEFAULT_THRESHOLD,
                tolerance=DEFAULT_TOLERANCE,
                dump_problems=[],
            )
            expected_cells = len(tickers) * len(fields)
            if report.total != expected_cells:
                failures.append(
                    f"mismatch={mismatch_count}: 单元格数 {report.total} != 期望 {expected_cells}"
                )
            if report.count("violation") != len(violations):
                failures.append(
                    f"mismatch={mismatch_count}: 白名单违约数 {report.count('violation')} != {len(violations)}"
                )
            if report.count("missing") != len(missing):
                failures.append(
                    f"mismatch={mismatch_count}: 缺失数 {report.count('missing')} != {len(missing)}"
                )
            if report.count("mismatch") != mismatch_count:
                failures.append(
                    f"mismatch={mismatch_count}: 不一致数 {report.count('mismatch')} != {mismatch_count}"
                )
            expected_comparable = expected_cells - len(violations) - len(missing)
            if report.comparable != expected_comparable:
                failures.append(
                    f"mismatch={mismatch_count}: 可比数 {report.comparable} != {expected_comparable}"
                )
            if report.verdict != expected_verdict:
                failures.append(
                    f"mismatch={mismatch_count}: 判定 {report.verdict} != {expected_verdict}"
                )

    if failures:
        print("SELF-TEST FAILED:")
        for failure in failures:
            print(f"  - {failure}")
        return EXIT_ERROR
    print(
        "SELF-TEST OK: 白名单校验 / 显式单位换算 / 双源比对 / 缺失与违约隔离 / PASS 与 FAIL 判定均符合预期"
    )
    return EXIT_PASS


# --------------------------------------------------------------------------- 入口


def parse_args(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="spot_check.py",
        description="跨源数据质量抽检（错误率 < 2% → PASS）",
    )
    parser.add_argument("--source", choices=SOURCES, help="被检源")
    parser.add_argument("--ref", choices=SOURCES, help="参照源（默认取另一源）")
    parser.add_argument("--tickers", help="逗号分隔的标的代码（默认取契约的 10 票）")
    parser.add_argument("--fields", help="逗号分隔的 field_code（默认取契约的全部 20 字段）")
    parser.add_argument(
        "--source-dump",
        help=f"被检源读数转储（路径 / http(s) URL / - 表示 stdin；默认 {DEFAULT_DUMP_DIR}/<source>.jsonl）",
    )
    parser.add_argument("--ref-dump", help="参照源读数转储（同上）")
    parser.add_argument(
        "--contracts",
        default=str(DEFAULT_CONTRACTS_DIR),
        help="契约目录（默认仓库根的 contracts/）",
    )
    parser.add_argument(
        "--threshold",
        default=str(DEFAULT_THRESHOLD),
        help="错误率判定阈值（默认 0.02）",
    )
    parser.add_argument(
        "--tolerance",
        default=str(DEFAULT_TOLERANCE),
        help="单元格一致的相对容差（默认 0.01）",
    )
    parser.add_argument("--json", dest="json_path", help="输出机读报告到该文件")
    parser.add_argument("--self-test", action="store_true", help="用合成转储验证比对逻辑")
    args = parser.parse_args(argv)
    if not args.self_test and not args.source:
        parser.error("--source 必填（除非使用 --self-test）")
    return args


def _resolve_tickers(args: argparse.Namespace, dictionary: FieldDictionary) -> tuple[str, ...]:
    if not args.tickers:
        return tuple(dictionary.default_tickers)
    tickers = tuple(normalize_ticker(part) for part in args.tickers.split(",") if part.strip())
    if not tickers:
        raise UsageError("--tickers 为空")
    malformed = [code for code in tickers if not _TICKER_CODE.match(code)]
    if malformed:
        raise UsageError(
            f"--tickers 中的代码不是 6 位数字: {', '.join(malformed)}"
            "（逗号分隔的列表请加引号，避免被 shell 解析为数组后截断前导零）"
        )
    return tickers


def _resolve_fields(args: argparse.Namespace, dictionary: FieldDictionary) -> tuple[str, ...]:
    if not args.fields:
        return tuple(dictionary.fields)
    fields = tuple(part.strip() for part in args.fields.split(",") if part.strip())
    if not fields:
        raise UsageError("--fields 为空")
    unknown = [code for code in fields if code not in dictionary.fields]
    if unknown:
        raise UsageError(
            f"--fields 中的字段不在契约字典内: {', '.join(unknown)}"
            f"（可选项: {', '.join(dictionary.fields)}）"
        )
    return fields


def main(argv: list[str] | None = None) -> int:
    if hasattr(sys.stdout, "reconfigure"):
        try:
            sys.stdout.reconfigure(errors="replace")
        except (ValueError, OSError):  # pragma: no cover - 平台相关
            pass
    args = parse_args(argv)
    dictionary = FieldDictionary.load(Path(args.contracts) / "field_dictionary.yaml")

    if args.self_test:
        return run_self_test(dictionary)

    try:
        threshold = Decimal(args.threshold)
        tolerance = Decimal(args.tolerance)
        tickers = _resolve_tickers(args, dictionary)
        fields = _resolve_fields(args, dictionary)
    except InvalidOperation as exc:
        raise UsageError(f"--threshold/--tolerance 必须是数值: {exc}") from exc

    ref_source = args.ref or next(src for src in SOURCES if src != args.source)
    source_dump = args.source_dump or str(DEFAULT_DUMP_DIR / f"{args.source}.jsonl")
    ref_dump = args.ref_dump or str(DEFAULT_DUMP_DIR / f"{ref_source}.jsonl")

    target_readings, problems = load_readings(source_dump, args.source)
    ref_readings, ref_problems = load_readings(ref_dump, ref_source)
    problems.extend(ref_problems)

    report = run_spot_check(
        target_readings,
        ref_readings,
        dictionary,
        tickers,
        fields,
        source=args.source,
        ref_source=ref_source,
        threshold=threshold,
        tolerance=tolerance,
        dump_problems=problems,
    )

    print(render_report(report))
    if args.json_path:
        Path(args.json_path).write_text(
            json.dumps(report.as_dict(), ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
        print(f"\n机读报告已写入 {args.json_path}")
    return report.exit_code


if __name__ == "__main__":
    try:
        sys.exit(main())
    except UsageError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        sys.exit(EXIT_ERROR)