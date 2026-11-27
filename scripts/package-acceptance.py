#!/usr/bin/env python3
"""汇总三端业务验收工件为发布用验收包。

输入：从 main 上同一 commit 的 CI run 下载的三端工件目录
（<evidence-dir>/<platform>/）。每个平台包只保留四件套：
业务验收报告.html、验收过程录像.webm、房屋租赁合同-已签字盖章.pdf 与
SHA256SUMS；工件里重复的「腾手使用教程.html」不进入验收包（每端约省 12MB）。
验收包名：tengshou-acceptance-<tag>-<platform>.zip。

用 zipfile 而不是 zip(1)：中文文件名会写入 UTF-8 标志位，Windows 解压不会
乱码（与教程包打包一致）。

本地模拟（三端工件目录用占位文件即可）：

    python3 -I scripts/package-acceptance.py --tag v0.1.0-alpha.28 \
      --evidence-dir /tmp/evidence --out-dir /tmp/acceptance
"""

import argparse
import hashlib
import os
import sys
import zipfile

PLATFORMS = ("linux-chromium", "macos-webkit", "windows-chromium")
KEEP_FILES = ("业务验收报告.html", "验收过程录像.webm", "房屋租赁合同-已签字盖章.pdf")
SUMS_NAME = "SHA256SUMS"


def file_digest(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def build_package(tag, platform, evidence_dir, out_dir):
    src_dir = os.path.join(evidence_dir, platform)
    if not os.path.isdir(src_dir):
        sys.exit(f"错误：缺少 {platform} 工件目录：{src_dir}")
    for name in KEEP_FILES:
        if not os.path.isfile(os.path.join(src_dir, name)):
            sys.exit(f"错误：{platform} 工件缺少 {name}")

    sums = "".join(
        f"{file_digest(os.path.join(src_dir, name))}  {name}\n" for name in KEEP_FILES
    )
    zip_path = os.path.join(out_dir, f"tengshou-acceptance-{tag}-{platform}.zip")
    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as archive:
        for name in KEEP_FILES:
            archive.write(os.path.join(src_dir, name), arcname=name)
        archive.writestr(SUMS_NAME, sums)
    verify_package(zip_path, sums)
    return zip_path


def verify_package(zip_path, expected_sums):
    with zipfile.ZipFile(zip_path) as archive:
        bad = archive.testzip()
        if bad is not None:
            sys.exit(f"错误：{zip_path} 内 {bad} CRC 校验失败")
        names = sorted(archive.namelist())
        expected = sorted(list(KEEP_FILES) + [SUMS_NAME])
        if names != expected:
            sys.exit(f"错误：验收包内容不符：{names} != {expected}")
        if archive.read(SUMS_NAME).decode("utf-8") != expected_sums:
            sys.exit(f"错误：{zip_path} 内 SHA256SUMS 与包内文件不一致")


def main():
    parser = argparse.ArgumentParser(description="汇总三端业务验收工件为发布用验收包")
    parser.add_argument("--tag", required=True, help="版本 tag（与 Release 版本一致）")
    parser.add_argument("--evidence-dir", required=True, help="含三端平台子目录的工件根目录")
    parser.add_argument("--out-dir", required=True, help="验收包输出目录")
    args = parser.parse_args()

    os.makedirs(args.out_dir, exist_ok=True)
    for platform in PLATFORMS:
        path = build_package(args.tag, platform, args.evidence_dir, args.out_dir)
        print(f"已生成 {path}")


if __name__ == "__main__":
    main()
