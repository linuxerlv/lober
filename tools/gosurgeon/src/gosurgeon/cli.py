"""命令行入口。

设计要点只有一条：**不让转义序列经过两层解析**。内容文件里写 @NL@，本模块
换成两字符的反斜杠-n 写进 Go 源码。这样内容文件是纯文本，bash heredoc 与
Python 字符串都不会动它。
"""

from __future__ import annotations

import io
import sys
from pathlib import Path

# Go 源码里的两字符转义序列。用 chr 拼，避免本文件自身的字符串里出现反斜杠
# 被某一层再解析一次——这个工具存在的理由正是那类问题。
GO_NEWLINE = chr(92) + "n"
NL_PLACEHOLDER = "@NL@"


def read_lines(path: Path) -> list[str]:
    return path.read_text(encoding="utf-8").split("\n")


def write_lines(path: Path, lines: list[str]) -> None:
    # newline="" 加显式 LF：仓库的 Go 文件一律 LF，不能让 Windows 的
    # 默认 CRLF 混进去（gofmt 不会报错，但 git diff 会整文件标记为改动）。
    with io.open(path, "w", encoding="utf-8", newline="") as f:
        f.write("\n".join(lines))


def load_content(path: Path) -> list[str]:
    raw = path.read_text(encoding="utf-8")
    if raw.endswith("\n"):
        raw = raw[:-1]
    return [line.replace(NL_PLACEHOLDER, GO_NEWLINE) for line in raw.split("\n")]


def find_unique(lines: list[str], anchor: str, what: str) -> int:
    hits = [i for i, line in enumerate(lines) if anchor in line]
    if not hits:
        raise SystemExit(f"gosurgeon: {what}锚点未命中: {anchor!r}")
    if len(hits) > 1:
        detail = ", ".join(f"第 {i + 1} 行" for i in hits)
        raise SystemExit(
            f"gosurgeon: {what}锚点命中 {len(hits)} 次（{detail}），"
            f"必须唯一——请换一个更长的锚点: {anchor!r}"
        )
    return hits[0]


def cmd_replace_range(argv: list[str]) -> int:
    if len(argv) != 4:
        raise SystemExit(
            "用法: gosurgeon replace-range <文件> <起始锚点> <结束锚点> <内容文件>"
        )
    target, start_anchor, end_anchor, content_path = argv
    path = Path(target)
    lines = read_lines(path)

    i = find_unique(lines, start_anchor, "起始")
    tail = lines[i:]
    hits = [k for k, line in enumerate(tail) if end_anchor in line]
    if not hits:
        raise SystemExit(
            f"gosurgeon: 起始锚点之后找不到结束锚点: {end_anchor!r}"
        )
    j = i + hits[0]

    new = load_content(Path(content_path))
    lines[i : j + 1] = new
    write_lines(path, lines)
    print(f"gosurgeon: 替换 {target} 第 {i + 1}-{j + 1} 行，共 {len(new)} 行新内容")
    return 0


def cmd_insert_after(argv: list[str]) -> int:
    if len(argv) != 3:
        raise SystemExit("用法: gosurgeon insert-after <文件> <锚点> <内容文件>")
    target, anchor, content_path = argv
    path = Path(target)
    lines = read_lines(path)
    i = find_unique(lines, anchor, "")
    new = load_content(Path(content_path))
    lines[i + 1 : i + 1] = new
    write_lines(path, lines)
    print(f"gosurgeon: 在 {target} 第 {i + 1} 行后插入 {len(new)} 行")
    return 0


def cmd_verify(argv: list[str]) -> int:
    """扫描未闭合的字符串字面量。

    判据是逐行统计不在注释内的双引号与反引号数量：Go 的解释型字符串
    （双引号）不能跨行，所以一行里未转义的双引号必须成对。反引号的原始
    字符串可以跨行，因此只做奇偶提示、不算错。
    """
    if len(argv) != 1:
        raise SystemExit("用法: gosurgeon verify <文件>")
    path = Path(argv[0])
    problems: list[str] = []
    in_block_comment = False

    for lineno, line in enumerate(path.read_text(encoding="utf-8").split("\n"), 1):
        stripped = line.strip()
        if in_block_comment:
            if "*/" in stripped:
                in_block_comment = False
            continue
        if stripped.startswith("/*"):
            if "*/" not in stripped:
                in_block_comment = True
            continue
        if stripped.startswith("//"):
            continue

        count = 0
        k = 0
        escaped = False
        in_raw = False  # 反引号原始字符串内部：里面的双引号不算数
        while k < len(line):
            ch = line[k]
            if in_raw:
                if ch == "`":
                    in_raw = False
            elif escaped:
                escaped = False
            elif ch == chr(92):
                escaped = True
            elif ch == "`":
                in_raw = True
            elif ch == '"':
                count += 1
            elif ch == "/" and k + 1 < len(line) and line[k + 1] == "/" and count % 2 == 0:
                break  # 行尾注释（且不在字符串内），之后的引号不算
            k += 1
        if count % 2 == 1:
            problems.append(f"  第 {lineno} 行有奇数个双引号: {line.rstrip()}")

    if problems:
        print(f"gosurgeon: {path} 可能有未闭合的字符串字面量：", file=sys.stderr)
        print("\n".join(problems), file=sys.stderr)
        return 1
    print(f"gosurgeon: {path} 字符串字面量配对检查通过")
    return 0


COMMANDS = {
    "replace-range": cmd_replace_range,
    "insert-after": cmd_insert_after,
    "verify": cmd_verify,
}


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if not args or args[0] not in COMMANDS:
        print("用法: gosurgeon <replace-range|insert-after|verify> ...", file=sys.stderr)
        return 2
    return COMMANDS[args[0]](args[1:])
