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
        for lineno, segment in user_visible_segments(path, text):
            for kind, bad, good in RULES:
                if bad not in segment:
                    continue
                # 测试文件里出现禁用词通常是「守卫断言」，只在非测试文件报错
                if is_test:
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
