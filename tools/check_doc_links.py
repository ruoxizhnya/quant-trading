#!/usr/bin/env python3
"""校验文档内部链接是否指向真实存在的文件。

对应文档治理规则 R5（见 docs/README.md）：文档必须可被自动验证，否则必然腐烂。

用法：
    python tools/check_doc_links.py                 # 校验常青层 + 活跃层（默认跳过 archive/）
    python tools/check_doc_links.py --include-archive
    python tools/check_doc_links.py --root docs

退出码：
    0 = 无坏链；1 = 发现坏链（CI 应据此失败）

两项检查：
  1. **Markdown 链接**（`[x](y.md)`）指向的文件存在。
  2. **文档里引用的部署配置路径**（`config/**` / `deploy/**` 的 yaml/yml/json）存在。
     检查 2 是 AUD-41 的教训：`docs/SPEC.md` 的 `## Configuration` 段整段描述了一个
     并不存在的 `config/global.yaml`，而那种引用**不是 Markdown 链接** ——
     检查 1 根本看不见它，所以那段草稿烂了很久没人发现。
  3. **入口层文档必须带 R1 frontmatter**（`status` / `last-verified` / `verified-by`）。
     检查 3 是 AUD-44 的教训：`docs/SPEC.md` / `docs/ADR.md` / `docs/TEST.md` 三份
     文档用的是自创的 `> **Status**:` 引用块，R1 的三个字段一个都没有 ——
     而 R1 恰恰是「文档不腐烂」的入口（没有 `last-verified` 就无法判断它是否可信）。
     范围从 `docs/README.md` 的链接**推导**，不是手写清单（PITFALLS §41）：
     README 是文档的唯一入口，被它链接的就是「新来的人会读的那几份」。

⚠️ 归档层（archive/）为什么也要查（AUD-14）：
ODR 与审计报告几乎全在 `docs/archive/`，而那恰恰是**相对层级最深、改名最频繁**
的地方。只查活跃层等于「元检查的盲区正好落在最需要它的区域」—— 事实上
ODR-065 报告归档时相对层级没同步，2 条死链就这么躺在里面没人发现。
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

# 匹配 Markdown 行内链接，排除 http(s):// 与 mailto: 等外部协议。
# 例：[PRODUCT.md](PRODUCT.md) / [ADR](adr/adr-001.md#decision)
LINK_RE = re.compile(r"\]\((?!https?://|mailto:|#)([^)]+?\.md)(#[^)]*)?\)")

# 代码块与行内代码：里面的 `[x](y.md)` 是**引文**（例如审计报告在表格里引用
# 一条坏链作为证据），不是导航链接。渲染出来也不是链接，不该被当成坏链告警 ——
# 否则修好它反而等于抹掉证据。
_FENCE_RE = re.compile(r"^```.*?^```", re.MULTILINE | re.DOTALL)
_INLINE_CODE_RE = re.compile(r"`[^`\n]*`")


def strip_code(text: str) -> str:
    """把围栏代码块与行内代码替换成等长空白，保持行号/偏移不变。"""

    def blank(m: re.Match[str]) -> str:
        return re.sub(r"[^\n]", " ", m.group(0))

    return _INLINE_CODE_RE.sub(blank, _FENCE_RE.sub(blank, text))


def find_markdown_files(root: Path, include_archive: bool) -> list[Path]:
    files = sorted(root.rglob("*.md"))
    if include_archive:
        return files
    return [f for f in files if "archive" not in f.parts]


# 反引号里的**仓库相对路径**引用。这些路径是相对仓库根解析的，不是相对文档。
#
# ⚠️ 反引号**不是必需的**：AUD-41 的原始形态就是 `### Global Config (config/global.yaml)`
# —— 一个**没有反引号**的标题。只认反引号的第一版护栏实测对 `381e60a` 的 SPEC
# **零命中**，即抓不住它要防的那个 bug。所以这里匹配裸路径。
#
# 前后 lookaround 排除「更长路径的一部分」（`docs/config/x.yaml` 不该被算成
# `config/x.yaml`）。
CONFIG_PATH_RE = re.compile(
    r"(?<![A-Za-z0-9_./-])((?:config|deploy)/[A-Za-z0-9_./-]+\.(?:yaml|yml|json))"
    r"(?![A-Za-z0-9_./-])"
)

# 否定词豁免窗口 = **当前行 + 上一行**。历史行（「已删除 X」「X 从未被读取」）
# 会合法地引用已删文件 —— TASKS.md 的「已完成」行几乎全是这个形状。纳入上一行
# 是因为散文会换行：否定词常常落在路径的**上一行**（AUD-41 的 changelog 就是）。
#
# ⚠️ 边界（启发式，不是语义分析）：
#   - 否定词落在**下一行**时不豁免 —— 写文档时把「不存在」放在路径同一行或上一行。
#   - 窗口内既说「A 不存在」又引用了真缺失的 B 会**漏报**。
_NEGATION_MARKERS = (
    "没有", "不存在", "并不存在", "已删除", "从未", "不许",
    "不再", "移除", "删", "废弃", "退役",
)


def _config_path_exists(target: str, md: Path, repo_root: Path) -> bool:
    """按**仓库根**或**文档自身目录**解析。

    两个基址都需要：`docs/hermes/system-design.md` 里的 `config/hermes.yaml`
    指的是它自己的 `docs/hermes/config/hermes.yaml`，不是仓库根的 `config/`。
    只按仓库根解析会把这类引用误报成缺失（实测 2 条）。
    """
    return (repo_root / target).exists() or (md.parent / target).exists()


def check_config_path_refs(
    root: Path, repo_root: Path, include_archive: bool
) -> list[tuple[Path, int, str]]:
    """文档里引用的部署配置路径必须真实存在（AUD-41）。"""
    missing: list[tuple[Path, int, str]] = []
    for md in find_markdown_files(root, include_archive):
        try:
            text = md.read_text(encoding="utf-8")
        except OSError:
            continue
        lines = text.splitlines()
        for idx, line in enumerate(lines):
            window = line + (lines[idx - 1] if idx >= 1 else "")
            for match in CONFIG_PATH_RE.finditer(line):
                target = match.group(1)
                if _config_path_exists(target, md, repo_root):
                    continue
                if any(marker in window for marker in _NEGATION_MARKERS):
                    continue
                missing.append((md, idx + 1, target))
    return missing


# R1（见 docs/README.md）：每份文档顶部必须写 status / last-verified / verified-by。
# 只认文件**开头**的 `---` frontmatter 块 —— 散在正文里的 `status:` 不算数
# （那正是 SPEC.md / TEST.md 过去的形状：自创的 `> **Status**:` 引用块）。
_FRONTMATTER_RE = re.compile(r"\A---\r?\n(.*?)\r?\n---\r?\n", re.DOTALL)
_R1_FIELDS = ("status", "last-verified", "verified-by")


def check_entry_frontmatter(root: Path) -> list[tuple[Path, list[str]]]:
    """入口层文档必须带 R1 frontmatter（AUD-44）。

    范围从 `docs/README.md` 的链接**推导**，不是手写清单：README 是文档的唯一入口，
    凡是被它链接的文档就是「新来的人会读的那几份」，腐烂的代价最高。这样加一份新
    入口文档时，护栏会自动要求它也带上 frontmatter。
    """
    readme = root / "README.md"
    if not readme.is_file():
        return []
    try:
        text = readme.read_text(encoding="utf-8")
    except OSError:
        return []

    bad: list[tuple[Path, list[str]]] = []
    for target in sorted({m.group(1) for m in LINK_RE.finditer(text)}):
        md = (readme.parent / target).resolve()
        if not md.is_file():
            continue  # 文件本身不存在由检查 1 报，这里不重复
        try:
            body = md.read_text(encoding="utf-8")
        except OSError:
            continue
        match = _FRONTMATTER_RE.match(body)
        if not match:
            bad.append((md, ["（没有 frontmatter 块）"]))
            continue
        block = match.group(1)
        missing = [
            f for f in _R1_FIELDS if not re.search(rf"^{re.escape(f)}:", block, re.M)
        ]
        if missing:
            bad.append((md, missing))
    return bad


def check(root: Path, include_archive: bool) -> list[tuple[Path, str]]:
    broken: list[tuple[Path, str]] = []
    for md in find_markdown_files(root, include_archive):
        try:
            text = md.read_text(encoding="utf-8")
        except OSError:
            continue
        for match in LINK_RE.finditer(strip_code(text)):
            target = match.group(1)
            resolved = (md.parent / target).resolve()
            if not resolved.exists():
                broken.append((md, target))
    return broken


def main() -> int:
    parser = argparse.ArgumentParser(description="校验文档内部链接")
    parser.add_argument("--root", default="docs", help="文档根目录（默认 docs）")
    parser.add_argument(
        "--include-archive",
        action="store_true",
        help="连同 archive/ 一起校验（归档层默认跳过）",
    )
    args = parser.parse_args()

    root = Path(args.root)
    if not root.is_dir():
        print(f"错误：文档根目录不存在：{root}", file=sys.stderr)
        return 1

    # 仓库根：config/ 与 deploy/ 这些路径是相对仓库根写的，不是相对 docs/。
    repo_root = Path(__file__).resolve().parent.parent

    files = find_markdown_files(root, args.include_archive)
    broken = check(root, args.include_archive)
    missing_paths = check_config_path_refs(root, repo_root, args.include_archive)
    bad_frontmatter = check_entry_frontmatter(root)

    scope = "含归档层" if args.include_archive else "仅常青层与活跃层（跳过 archive/）"
    print(f"扫描 {len(files)} 个 Markdown 文件（{scope}）")

    if not broken:
        print("✓ 未发现坏链")
    else:
        print(f"✗ 发现 {len(broken)} 条坏链：", file=sys.stderr)
        for md, target in broken:
            print(f"  {md} -> {target}", file=sys.stderr)

    if not missing_paths:
        print("✓ 文档里引用的 config/ · deploy/ 路径都存在")
    else:
        print(f"✗ 发现 {len(missing_paths)} 条不存在的配置路径引用：", file=sys.stderr)
        for md, lineno, target in missing_paths:
            print(f"  {md}:{lineno} -> {target}", file=sys.stderr)

    if not bad_frontmatter:
        print("✓ 入口层文档都带 R1 frontmatter（status / last-verified / verified-by）")
    else:
        print(
            f"✗ 发现 {len(bad_frontmatter)} 份入口层文档缺 R1 frontmatter：",
            file=sys.stderr,
        )
        for md, missing in bad_frontmatter:
            print(f"  {md} -> 缺 {', '.join(missing)}", file=sys.stderr)

    if broken or missing_paths or bad_frontmatter:
        print(
            "\n提示：改代码或改文档，二选一，不要两边都留着（见 docs/README.md R5）。",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
