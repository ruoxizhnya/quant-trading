#!/usr/bin/env python3
"""把手写 gin.H{"error": ...} 批量换成 internal/httpserver 的统一出口（P1-5）。

只处理**单行、单键**的形态，其余一律跳过并统计 —— 宁可少转，也不要转错：

    c.JSON(STATUS, gin.H{"error": err.Error()})        -> httpserver.Error(c, STATUS, err)
    c.JSON(STATUS, gin.H{"error": "字面量"})            -> httpserver.Fail(c, STATUS, "字面量")
    c.JSON(STATUS, gin.H{"error": "前缀 " + err.Error()}) -> httpserver.Wrap(c, STATUS, err, "前缀 ")
    c.JSON(STATUS, gin.H{"error": fmt.Sprintf(...)})   -> httpserver.Failf(c, STATUS, ...)
    c.JSON(STATUS, gin.H{"error": 变量})                -> httpserver.Fail(c, STATUS, 变量)

跳过：多键（带 details 等）、跨行、测试文件（那些是夹具，不是链路）。
"""
import io
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
IMPORT = '"github.com/ruoxizhnya/quant-trading/internal/httpserver"'
SKIPPED = []


def matching_paren(s: str, open_idx: int) -> int:
    depth = 0
    for i in range(open_idx, len(s)):
        if s[i] == '(':
            depth += 1
        elif s[i] == ')':
            depth -= 1
            if depth == 0:
                return i
    return -1


def convert_line(line: str) -> str | None:
    m = re.search(r'c\.JSON\(', line)
    if not m:
        return None
    open_idx = m.end() - 1
    close_idx = matching_paren(line, open_idx)
    if close_idx < 0:
        return None
    inner = line[open_idx + 1:close_idx]

    # 只有 "(STATUS, gin.H{...})" 这一种形状才动。
    mm = re.fullmatch(r'\s*(http\.Status\w+)\s*,\s*gin\.H\{(.*)\}\s*', inner, re.S)
    if not mm:
        return None
    status, body = mm.group(1), mm.group(2).strip()

    # 单键：key 后面不能出现顶层逗号。
    #
    # 引号里的逗号不算 —— "use YYYYMMDD, e.g. 20240101" 这种文案里带逗号是
    # 常事，按字符数逗号会把所有带逗号的文案都误判成多键（踩过一次）。
    depth = 0
    quote = ''
    for i, ch in enumerate(body):
        if quote:
            if ch == '\\':
                continue
            if ch == quote:
                quote = ''
            continue
        if ch in '"`':
            quote = ch
        elif ch in '([{':
            depth += 1
        elif ch in ')]}':
            depth -= 1
        elif ch == ',' and depth == 0:
            SKIPPED.append(body[:60])
            return None
    km = re.fullmatch(r'"error"\s*:\s*(.*)', body, re.S)
    if not km:
        SKIPPED.append(body[:60])
        return None
    expr = km.group(1).strip()

    prefix = line[:m.start()]
    suffix = line[close_idx + 1:]

    err_call = re.fullmatch(r'([A-Za-z_][\w.]*)\.Error\(\)', expr)
    if expr == 'err.Error()':
        call = f'httpserver.Error(c, {status}, err)'
    elif err_call:
        call = f'httpserver.Error(c, {status}, {err_call.group(1)})'
    elif re.fullmatch(r'"[^"]*"\s*\+\s*[A-Za-z_][\w.]*\.Error\(\)', expr):
        # "前缀 " + foo.Error() —— 前缀给人看，foo 进日志。
        # 注意用 removesuffix 而不是数字符数切：手数长度必错（踩过一次）。
        lit, var = expr.split('+', 1)
        call = f'httpserver.Wrap(c, {status}, {var.strip().removesuffix(".Error()")}, {lit.strip()})'
    elif expr.startswith('fmt.Sprintf('):
        args = expr[len('fmt.Sprintf('):-1]
        call = f'httpserver.Failf(c, {status}, {args})'
    elif re.fullmatch(r'"[^"]*"', expr):
        call = f'httpserver.Fail(c, {status}, {expr})'
    elif re.fullmatch(r'[A-Za-z_][\w.]*', expr):
        call = f'httpserver.Fail(c, {status}, {expr})'
    else:
        SKIPPED.append(expr[:60])
        return None

    return f'{prefix}{call}{suffix}'


def add_import(text: str) -> str:
    # fmt.Sprintf 被换成 Failf 之后，fmt 可能就没处用了 —— 顺手摘掉，
    # 否则编译不过（Go 不允许未使用的 import）。
    if 'fmt.' not in text:
        text = re.sub(r'^\t"fmt"\n', '', text, flags=re.M)
    if IMPORT in text:
        return text
    m = re.search(r'^import \(\n', text, re.M)
    if not m:
        return text
    # 插到 import 块里第一个非空行之前，保持分组整洁。
    insert_at = m.end()
    return text[:insert_at] + '\t' + IMPORT + '\n' + text[insert_at:]


def main() -> int:
    changed = 0
    converted = 0
    for path in sorted((ROOT / 'cmd').rglob('*.go')):
        if path.name.endswith('_test.go'):
            continue
        text = io.open(path, encoding='utf-8').read()
        out_lines = []
        file_hits = 0
        for line in text.split('\n'):
            new = convert_line(line)
            if new is not None:
                out_lines.append(new)
                file_hits += 1
            else:
                out_lines.append(line)
        if not file_hits:
            continue
        new_text = add_import('\n'.join(out_lines))
        io.open(path, 'w', encoding='utf-8', newline='').write(new_text)
        changed += 1
        converted += file_hits
        print(f'{path.relative_to(ROOT)}: {file_hits}')
    print(f'\n文件 {changed} 个，替换 {converted} 处；跳过（多键/复杂表达式）{len(SKIPPED)} 处')
    for s in SKIPPED[:15]:
        print('  跳过：', s)
    return 0


if __name__ == '__main__':
    sys.exit(main())
