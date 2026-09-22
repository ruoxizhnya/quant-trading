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
  2. **反引号里的部署配置路径**（`` `config/*.yaml` `` / `` `deploy/**` ``）存在。
     检查 2 是 AUD-41 的教训：`docs/SPEC.md` 的 `## Configuration` 段整段描述了一个
     并不存在的 `config/global.yaml`，而那种引用是**行内代码、不是 Markdown 链接** ——
     检查 1 根本看不见它，所以那段草稿烂了很久没人发现。

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
CONFIG_PATH_RE = re.compile(
    r"`((?:config|deploy)/[A-Za-z0-9_./-]+\.(?:yaml|yml|json))`"
)

# 否定词豁免：**整行**出现这些词就跳过。历史行（「已删除 X」「X 从未被读取」）
# 会合法地引用已删文件 —— TASKS.md 的「已完成」行几乎全是这个形状。
#
# ⚠️ 边界（这是启发式，不是语义分析）：一行里既说「A 不存在」又引用了真缺失的
# B 会**漏报**。选整行而不是「引用之前」是因为「删 `cmd/ai/` + `config/ai-service.yaml`」
# 这类句子里否定词在引用**之后**；首版用「引用之前」实测漏放 3 条历史行。
_NEGATION_MARKERS = (
    "没有", "不存在", "并不存在", "已删除", "从未", "不许",
    "不再", "移除", "删", "废弃", "退役",
)


def check_config_path_refs(
    root: Path, repo_root: Path, include_archive: bool
) -> list[tuple[Path, int, str]]:
    """行内代码里引用的部署配置路径必须真实存在（AUD-41）。"""
    missing: list[tuple[Path, int, str]] = []
    for md in find_markdown_files(root, include_archive):
        try:
            text = md.read_text(encoding="utf-8")
        except OSError:
            continue
        for lineno, line in enumerate(text.splitlines(), 1):
            if any(marker in line for marker in _NEGATION_MARKERS):
                continue
            for match in CONFIG_PATH_RE.finditer(line):
                target = match.group(1)
                if not (repo_root / target).exists():
                    missing.append((md, lineno, target))
    return missing


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

    scope = "含归档层" if args.include_archive else "仅常青层与活跃层（跳过 archive/）"
    print(f"扫描 {len(files)} 个 Markdown 文件（{scope}）")

    if not broken:
        print("✓ 未发现坏链")
    else:
        print(f"✗ 发现 {len(broken)} 条坏链：", file=sys.stderr)
        for md, target in broken:
            print(f"  {md} -> {target}", file=sys.stderr)

    if not missing_paths:
        print("✓ 行内代码引用的 config/ · deploy/ 路径都存在")
    else:
        print(f"✗ 发现 {len(missing_paths)} 条不存在的配置路径引用：", file=sys.stderr)
        for md, lineno, target in missing_paths:
            print(f"  {md}:{lineno} -> {target}", file=sys.stderr)

    if broken or missing_paths:
        print(
            "\n提示：改代码或改文档，二选一，不要两边都留着（见 docs/README.md R5）。",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
