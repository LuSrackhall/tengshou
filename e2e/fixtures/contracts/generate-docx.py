#!/usr/bin/env python3
"""生成 E2E 真实业务场景使用的 Word 合同模板（零第三方依赖）。

产物：e2e/fixtures/contracts/农产品购销合同.docx

设计要点
--------
- 只使用 Python 标准库 zipfile 直接写 OOXML，任何环境都能重建，不需要
  Word / python-docx / LibreOffice。
- 合同全部为虚构演示数据（幸福村股份经济合作社、农户王建国、证件号与电话
  均为编造），只用于界面端到端验收。
- 每个待套印字段单独一行，格式为「字段名：＿＿＿＿」，便于测试根据 PDF
  文字位置把标定框拖到对应位置；签字盖章栏单独放在第 2 页，给手写签名与
  公章留出空间。
- 版面固定为 A4、上下左右 2cm 页边距、正文 10.5pt，LibreOffice headless
  与 Word 都能正常转换。

用法：python3 generate-docx.py
"""

from __future__ import annotations

import zipfile
from pathlib import Path
from xml.sax.saxutils import escape

HERE = Path(__file__).resolve().parent
OUTPUT = HERE / "农产品购销合同.docx"

FONT_EAST_ASIA = "Noto Sans CJK SC"
FONT_LATIN = "Times New Roman"
BODY_SIZE = 21  # 半磅：10.5pt
SMALL_SIZE = 18  # 9pt


def run(
    text: str,
    *,
    size: int = BODY_SIZE,
    bold: bool = False,
) -> str:
    """一个文字 run；字体同时覆盖西文与中文，避免 Linux 缺字。"""
    props = [
        f'<w:rFonts w:ascii="{FONT_LATIN}" w:hAnsi="{FONT_LATIN}" '
        f'w:eastAsia="{FONT_EAST_ASIA}"/>',
        f'<w:sz w:val="{size}"/>',
        f'<w:szCs w:val="{size}"/>',
    ]
    if bold:
        props.append("<w:b/>")
    return (
        "<w:r><w:rPr>" + "".join(props) + "</w:rPr>"
        f'<w:t xml:space="preserve">{escape(text)}</w:t></w:r>'
    )


def paragraph(
    text: str = "",
    *,
    size: int = BODY_SIZE,
    bold: bool = False,
    align: str | None = None,
    space_after: int = 60,
    page_break: bool = False,
) -> str:
    """一个段落；align 取 center / right，page_break 在该段前分页。"""
    props = [f'<w:spacing w:after="{space_after}" w:line="260" w:lineRule="auto"/>']
    if align:
        props.append(f'<w:jc w:val="{align}"/>')
    content = run(text, size=size, bold=bold) if text else ""
    if page_break:
        content = '<w:r><w:br w:type="page"/></w:r>' + content
    return "<w:p><w:pPr>" + "".join(props) + "</w:pPr>" + content + "</w:p>"


def field_line(label: str, blanks: int = 18) -> str:
    """「字段名：＿＿＿＿」样式的一行；测试以「字段名」定位套印位置。"""
    return paragraph(f"{label}：{'＿' * blanks}")


def heading(text: str) -> str:
    return paragraph(text, size=22, bold=True, space_after=60)


def document_xml() -> str:
    parts = [
        paragraph("农产品购销合同", size=32, bold=True, align="center", space_after=120),
        paragraph(
            "合同模板（虚构演示数据，仅用于腾手界面验收测试）",
            size=SMALL_SIZE,
            align="center",
            space_after=160,
        ),
        field_line("合同编号", 12),
        field_line("合同名称", 14),
        field_line("签订日期", 12),
        heading("甲方（采购方）"),
        field_line("甲方名称", 16),
        field_line("甲方证件号", 18),
        field_line("甲方地址", 16),
        field_line("甲方电话", 14),
        field_line("甲方联系人", 10),
        heading("乙方（供货方）"),
        field_line("乙方姓名", 12),
        field_line("乙方证件号", 18),
        field_line("乙方地址", 16),
        field_line("乙方电话", 14),
        heading("标的与价款"),
        field_line("标的名称", 16),
        field_line("规格", 20),
        field_line("数量", 12),
        paragraph(f"单价：{'＿' * 12} 元/公斤", space_after=60),
        paragraph(f"小写金额：{'＿' * 12} 元", space_after=60),
        field_line("大写金额", 18),
        heading("交付与结算"),
        field_line("交货时间", 16),
        field_line("交货地点", 16),
        field_line("验收标准", 18),
        field_line("付款方式", 18),
        field_line("违约责任", 18),
        field_line("争议解决", 18),
        paragraph(
            "本合同经双方签字盖章后生效，双方各执一份，具有同等法律效力。",
            space_after=60,
        ),
        paragraph(
            "乙方应保证所供农产品符合国家食品安全标准；甲方应按约定及时结清货款。",
            space_after=60,
        ),
        paragraph(
            "未尽事宜由双方另行协商，补充协议与本合同具有同等法律效力。",
            space_after=60,
        ),
        paragraph(page_break=True, space_after=0),
        paragraph("签字与盖章", size=24, bold=True, align="center", space_after=160),
        field_line("甲方（盖章）", 10),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        field_line("乙方（签字）", 10),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("", space_after=0),
        paragraph("签署日期：＿＿＿＿年＿＿月＿＿日", space_after=60),
    ]
    return (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">'
        "<w:body>"
        + "".join(parts)
        + '<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>'
        '<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" '
        'w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>'
        "</w:body></w:document>"
    )


CONTENT_TYPES = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>
</Types>
"""

ROOT_RELS = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>
"""

DOCUMENT_RELS = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>
"""

STYLES = f"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults>
    <w:rPrDefault><w:rPr>
      <w:rFonts w:ascii="{FONT_LATIN}" w:hAnsi="{FONT_LATIN}" w:eastAsia="{FONT_EAST_ASIA}"/>
      <w:sz w:val="{BODY_SIZE}"/><w:szCs w:val="{BODY_SIZE}"/>
    </w:rPr></w:rPrDefault>
    <w:pPrDefault><w:pPr><w:spacing w:after="60" w:line="260" w:lineRule="auto"/></w:pPr></w:pPrDefault>
  </w:docDefaults>
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
  </w:style>
</w:styles>
"""

CORE = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
  xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>农产品购销合同</dc:title>
  <dc:creator>腾手 E2E 测试夹具</dc:creator>
  <dc:description>虚构演示数据，仅用于界面端到端验收</dc:description>
  <dcterms:created xsi:type="dcterms:W3CDTF">2026-11-24T00:00:00Z</dcterms:created>
  <dcterms:modified xsi:type="dcterms:W3CDTF">2026-11-24T00:00:00Z</dcterms:modified>
</cp:coreProperties>
"""

APP = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">
  <Application>tengshou-e2e</Application>
</Properties>
"""


def main() -> None:
    with zipfile.ZipFile(OUTPUT, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("[Content_Types].xml", CONTENT_TYPES)
        zf.writestr("_rels/.rels", ROOT_RELS)
        zf.writestr("word/document.xml", document_xml())
        zf.writestr("word/_rels/document.xml.rels", DOCUMENT_RELS)
        zf.writestr("word/styles.xml", STYLES)
        zf.writestr("docProps/core.xml", CORE)
        zf.writestr("docProps/app.xml", APP)
    print(f"已生成：{OUTPUT}")


if __name__ == "__main__":
    main()
