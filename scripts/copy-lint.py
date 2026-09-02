#!/usr/bin/env python3
"""全局文案 lint：以 docs/COPY_GUIDE.md 为基准检查用户可见文案。

用法:
    python3 scripts/copy-lint.py            # 检查，有问题时退出码 1
    python3 scripts/copy-lint.py --stats    # 附带术语分布统计

只检查会被用户看到的文案（Go 字符串字面量、HTML、Markdown 文档、
.env.example 注释）。代码注释与测试断言不算违规。
"""
from __future__ import annotations

import argparse
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SKIP_DIRS = {".git", "node_modules", "vendor", "dist", "build", "backup"}

# 检查规则：(分类, 违规写法, 建议写法)
RULES: list[tuple[str, str, str]] = [
    # 品牌
    ("品牌", "YiMao 放映室", "云海求片"),
    ("品牌", "云海放映室", "云海求片"),
    ("品牌", "云海影视助手", "云海求片助手"),
    # 已下线功能（游戏娱乐 + AI 能力）
    ("已下线", "电影大冒险", "（功能已移除）"),
    ("已下线", "求片大冒险", "（功能已移除）"),
    ("已下线", "电影冒险", "（功能已移除）"),
    ("已下线", "趣味求片", "（功能已移除）"),
    ("已下线", "游戏中心", "（功能已移除）"),
    ("已下线", "命运轮盘", "（功能已移除）"),
    ("已下线", "电影情报站", "（功能已移除）"),
    ("已下线", "AI 解说", "（功能已移除）"),
    ("已下线", "AI 电影解说", "（功能已移除）"),
    # 功能名
    ("功能名", "普通求片", "搜索求片"),
    ("功能名", "我的进度", "求片进度"),
    ("功能名", "我的求片", "求片进度"),
    ("功能名", "我的请求", "求片进度"),
    ("功能名", "求片记录", "求片进度"),
    # 重试话术
    ("重试话术", "请稍后重试", "请稍后再试"),
    ("重试话术", "请重试", "请稍后再试"),
    ("重试话术", "再试一次", "请稍后再试"),
    ("重试话术", "这次操作没有成功", "操作失败，请返回后重试"),
    # 语气
    ("语气", "下次一定赢", "（移除施压话术）"),
    ("语气", "通关才给下载", "（移除，玩法已下线）"),
    ("语气", "只有通关才能求片", "（移除，玩法已下线）"),
]

# 允许豁免的文件（历史兼容 / 测试固定值 / 本文件自身）
EXEMPT_FILES = {
    "docs/COPY_GUIDE.md",
    "scripts/copy-lint.py",
    "CHANGELOG.md",
}

GO_STRING = re.compile(r'"((?:[^"\\\n]|\\.)*)"|`([^`]*)`')
HALFWIDTH_PUNCT = re.compile(r"[\u4e00-\u9fa5][,?!](?![\d)\]}])")
CURLY_QUOTE = re.compile(r"[\u201c\u201d]")


def rel(path: str) -> str:
    return os.path.relpath(path, ROOT).replace(os.sep, "/")


def walk(exts: set[str]):
    for dirpath, dirnames, filenames in os.walk(ROOT):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS and not d.startswith("backup-")]
        for name in filenames:
            path = os.path.join(dirpath, name)
            if os.path.splitext(name)[1] not in exts:
                continue
            if rel(path) in EXEMPT_FILES:
                continue
            yield path


def user_visible_segments(path: str, text: str):
    """产出 (行号, 文本片段)，只包含用户可见部分。"""
    if path.endswith(".go"):
        # 只看字符串字面量，跳过注释行
        for lineno, line in enumerate(text.splitlines(), 1):
            stripped = line.lstrip()
            if stripped.startswith("//"):
                continue
            for match in GO_STRING.finditer(line):
                value = match.group(1) if match.group(1) is not None else match.group(2)
                if value:
                    yield lineno, value
    else:
        for lineno, line in enumerate(text.splitlines(), 1):
            yield lineno, line


GUARD_FILE_MARKERS = ("no_legacy", "copy_guard", "copy_consistency")
GUARD_VAR_RE = re.compile(r"\b(forbidden|legacy|retired|banned|hidden|leak\w*|stale)\s*:?=")


def guard_lines(text: str) -> set[int]:
    """标出「守卫断言」所在行号。

    守卫断言是刻意列出禁用词、断言它们不出现的代码，例如：
        forbidden := []string{"下次一定赢", "通关才给下载"}
        for _, legacy := range []string{"普通求片", "趣味求片"} {
    这些行里的禁用词不算违规。识别方式：从声明了 forbidden/legacy/... 的行开始，
    直到该字面量闭合为止。
    """
    lines = text.splitlines()
    marked: set[int] = set()
    depth = 0
    for index, line in enumerate(lines, 1):
        if depth > 0:
            marked.add(index)
            depth += line.count("{") - line.count("}")
            depth += line.count("[") - line.count("]")
            if depth <= 0:
                depth = 0
            continue
        if GUARD_VAR_RE.search(line):
            marked.add(index)
            opened = line.count("{") - line.count("}")
            if opened > 0:
                depth = opened
    return marked


def is_guard_assertion(name: str, segment: str, lineno: int, guarded: set[int]) -> bool:
    """判断测试文件里的禁用词是否属于「守卫断言」（刻意断言它不该出现）。

    三种情况放过：
      1. 专职守卫文件（no_legacy_* / *copy_guard* / *copy_consistency*）
      2. 位于 forbidden/legacy/... 列表内的行
      3. 用拼接写法避开本 lint 的断言（如 "电影" + "冒险"）

    其余情况属于真实文案样本（如按钮文字 + callback 的成对数据），
    必须跟着产品文案一起更新——之前无条件跳过 _test.go，
    导致 telegram_transport_guard_test.go 里的过期按钮文案逃过检查。
    """
    base = name.rsplit("/", 1)[-1]
    if any(marker in base for marker in GUARD_FILE_MARKERS):
        return True
    if lineno in guarded:
        return True
    return '" + "' in segment


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--stats", action="store_true", help="附带术语分布统计")
    args = parser.parse_args()

    violations: list[str] = []

    # 1. 禁用写法（Go 字面量 + 文档 + HTML）
    prod_exts = {".go", ".html", ".md", ".yml", ".yaml", ".example"}
    for path in walk(prod_exts):
        name = rel(path)
        is_test = name.endswith("_test.go")
        try:
            text = open(path, encoding="utf-8", errors="ignore").read()
        except OSError:
            continue
        is_doc = name.endswith(".md")
        guarded = guard_lines(text) if is_test else set()
        for lineno, segment in user_visible_segments(path, text):
            for kind, bad, good in RULES:
                if bad not in segment:
                    continue
                # 测试文件：只放过明确的「守卫断言」，不放过真实文案样本。
                # 之前无条件跳过 _test.go，导致 telegram_transport_guard_test.go
                # 里的 "📋 我的进度" 这类过期文案样本逃过检查。
                if is_test and is_guard_assertion(name, segment, lineno, guarded):
                    continue
                # 文档需要说明「某功能已下线」，这类描述不算违规；
                # 已下线词只在真正的用户界面（Go 字面量 / HTML）里禁止。
                if is_doc and kind == "已下线":
                    continue
                violations.append(f"{name}:{lineno} [{kind}] 用了「{bad}」→ 应为「{good}」")

    # 2. 中文里的半角标点（仅 Go 用户可见字符串 + HTML 文案）
    for path in walk({".go", ".html"}):
        name = rel(path)
        if name.endswith("_test.go"):
            continue
        try:
            text = open(path, encoding="utf-8", errors="ignore").read()
        except OSError:
            continue
        for lineno, segment in user_visible_segments(path, text):
            # 跳过日志/格式化串：含 % 动词或 [模块] 前缀的多为内部日志
            if re.match(r"^\[[^\]]+\]", segment) or "%v" in segment or "%w" in segment:
                continue
            hit = HALFWIDTH_PUNCT.search(segment)
            if hit:
                violations.append(
                    f"{name}:{lineno} [标点] 中文里用了半角「{hit.group(0)[-1]}」: {segment[:50]}"
                )

    # 3. 弯引号（应统一为直角引号）
    for path in walk({".go", ".html"}):
        name = rel(path)
        if name.endswith("_test.go"):
            continue
        try:
            text = open(path, encoding="utf-8", errors="ignore").read()
        except OSError:
            continue
        for lineno, segment in user_visible_segments(path, text):
            if CURLY_QUOTE.search(segment):
                violations.append(f"{name}:{lineno} [引号] 用了弯引号，应为「」: {segment[:50]}")

    if args.stats:
        print("== 术语分布 ==")
        terms = ["云海求片助手", "云海求片", "搜索求片", "求片进度", "许愿池", "观影画像", "洗版", "问题反馈"]
        counts = dict.fromkeys(terms, 0)
        for path in walk({".go", ".html", ".md", ".yml", ".example"}):
            try:
                text = open(path, encoding="utf-8", errors="ignore").read()
            except OSError:
                continue
            for t in terms:
                counts[t] += text.count(t)
        for t in terms:
            print(f"  {counts[t]:5d}  {t}")
        print()

    if violations:
        print(f"文案检查失败：{len(violations)} 处违规\n")
        for v in violations:
            print(f"  {v}")
        print("\n基准见 docs/COPY_GUIDE.md")
        return 1

    print("[ok] 文案检查通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
