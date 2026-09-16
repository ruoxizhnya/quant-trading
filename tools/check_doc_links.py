#!/usr/bin/env python3
"""校验文档内部链接是否指向真实存在的文件。

对应文档治理规则 R5（见 docs/README.md）：文档必须可被自动验证，否则必然腐烂。

用法：
    python tools/check_doc_links.py                 # 校验常青层 + 活跃层（默认跳过 archive/）
    python tools/check_doc_links.py --include-archive
    python tools/check_doc_links.py --root docs

退出码：
    0 = 无坏链；1 = 发现坏链（CI 应据此失败）
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

# 匹配 Markdown 行内链接，排除 http(s):// 与 mailto: 等外部协议。
# 例：[PRODUCT.md](PRODUCT.md) / [ADR](adr/adr-001.md#decision)
LINK_RE = re.compile(r"\]\((?!https?://|mailto:|#)([^)]+?\.md)(#[^)]*)?\)")


def find_markdown_files(root: Path, include_archive: bool) -> list[Path]:
    files = sorted(root.rglob("*.md"))
    if include_archive:
        return files
    return [f for f in files if "archive" not in f.parts]


def check(root: Path, include_archive: bool) -> list[tuple[Path, str]]:
    broken: list[tuple[Path, str]] = []
    for md in find_markdown_files(root, include_archive):
        try:
            text = md.read_text(encoding="utf-8")
        except OSError:
            continue
        for match in LINK_RE.finditer(text):
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

    files = find_markdown_files(root, args.include_archive)
    broken = check(root, args.include_archive)

    scope = "含归档层" if args.include_archive else "仅常青层与活跃层（跳过 archive/）"
    print(f"扫描 {len(files)} 个 Markdown 文件（{scope}）")

    if not broken:
        print("✓ 未发现坏链")
        return 0

    print(f"✗ 发现 {len(broken)} 条坏链：", file=sys.stderr)
    for md, target in broken:
        print(f"  {md} -> {target}", file=sys.stderr)
    print(
        "\n提示：改代码或改文档，二选一，不要两边都留着（见 docs/README.md R5）。",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
