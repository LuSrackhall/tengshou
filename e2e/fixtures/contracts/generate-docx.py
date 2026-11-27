#!/usr/bin/env python3
"""刷新/校验 E2E 真实业务场景使用的 Word 合同模板夹具（零第三方依赖）。

产物：`e2e/fixtures/contracts/房屋租赁合同.docx`

来源说明
--------
这份租赁合同模板在 v0.3 租赁定稿时由
`plans/v03-lease/房屋租赁合同-定稿.docx` 定稿（珠 te-ml9pp）：3 页、24 个
标定位置、12 个动态字段，页面上是字面下划线。`plans/` 不入库，因此
`e2e/fixtures/contracts/房屋租赁合同.docx` 这份**二进制夹具**就是仓库内的
来源，版面与下划线宽度都以定稿为准，不在脚本里用 OOXML 重新拼一份近似文档
（重新拼的版面无法保证和定稿一致，锚点会漂移）。

本脚本负责：

1. `--from <定稿.docx>`：从定稿复制并覆盖夹具（定稿更新时使用）；
2. 默认：校验夹具可打开、包含全部 24 个标定锚点、没有残留【】占位符。

锚点清单与 `plans/v03-lease/模板定稿-绑定清单.md` 一致。E2E 标定是按锚点
文字在 PDF 文本层里定位的（`e2e/tests/support/business-calibrate.ts`），
锚点缺失会直接让场景 C 失败，所以这条校验是夹具的回归闸门。

用法：
    python3 generate-docx.py                     # 校验现有夹具
    python3 generate-docx.py --from <定稿.docx>   # 用定稿刷新夹具后校验
"""

from __future__ import annotations

import html
import re
import shutil
import sys
import zipfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
FIXTURE = HERE / "房屋租赁合同.docx"

# 与模板定稿-绑定清单.md 的 24 个标定位置一一对应（含签章两处）。
ANCHORS = [
    "出租方（甲方）：",
    "证件号/统一社会信用代码：",
    "承租方（乙方）：",
    "身份证号：",
    "坐落于",
    "房号",
    "建筑面积",
    "租赁期自",
    "起至",
    "止。甲方应于",
    "签订日期：",
    "月租金为人民币",
    "（月租金大写：",
    "租金按",
    "押金人民币",
    "（押金大写：",
    "等费用由",
    "支付违约金人民币",
    "租赁用途：",
    "应提前",
    "拖欠租金超过",
    "其他约定：",
    "甲方（签字/盖章）：",
    "乙方（签字）：",
]


def document_text(path: Path) -> str:
    """读取 docx 的正文文字（去标签、反转义、去空白）。"""
    with zipfile.ZipFile(path) as archive:
        xml = archive.read("word/document.xml").decode("utf-8")
    runs = re.findall(r"<w:t[^>]*>([^<]*)</w:t>", xml)
    text = html.unescape("".join(runs))
    return re.sub(r"\s+", "", text)


def verify(path: Path) -> None:
    if not path.exists():
        raise SystemExit(f"错误：夹具不存在：{path}")
    text = document_text(path)
    missing = [anchor for anchor in ANCHORS if anchor not in text]
    if missing:
        raise SystemExit(
            "错误：夹具缺少标定锚点：" + "、".join(missing) + "\n"
            "请用 --from 从 plans/v03-lease/房屋租赁合同-定稿.docx 重新复制。"
        )
    leftover = re.findall(r"【[^】]{0,20}】?", text)
    if leftover:
        raise SystemExit("错误：夹具仍有【】占位符：" + "、".join(leftover[:5]))
    print(f"校验通过：{path}（{len(ANCHORS)} 个标定锚点齐全，无【】占位符）")


def main(argv: list[str]) -> None:
    if len(argv) == 3 and argv[1] == "--from":
        source = Path(argv[2]).expanduser().resolve()
        if not source.exists():
            raise SystemExit(f"错误：定稿 docx 不存在：{source}")
        shutil.copyfile(source, FIXTURE)
        print(f"已从定稿复制夹具：{source} -> {FIXTURE}")
    elif len(argv) != 1:
        raise SystemExit("用法：python3 generate-docx.py [--from <定稿.docx>]")
    verify(FIXTURE)


if __name__ == "__main__":
    main(sys.argv)
