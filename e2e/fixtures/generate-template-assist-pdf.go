// 生成模板制作辅助的 E2E 夹具 PDF：e2e/fixtures/template-assist.pdf。
//
// 为什么用 PDF 而不是 docx：占位符提示依赖 PDF 文本层，docx 还要经过本机
// Word/LibreOffice 转换；个别 macOS LibreOffice 无法枚举中文字体（中文
// PDF 空白），会让提示类断言受转换器环境影响。这里用产品同一套
// tdewolff/canvas + pdfdoc.LoadFont 直接生成 PDF，字体自动子集嵌入，
// 文本层在 Windows / macOS / Linux 的 pdf.js 中都能提取。
//
// 夹具内容固定（虚构数据）：第 1 页 3 个【…】括号占位符 + 2 段连续
// 下划线；第 2 页 2 段连续下划线。字段名标签在全文档唯一，便于 E2E 按
// 「标签：＿＿」定位标定框。
//
// 用法（仓库根目录）：
//
//	go run ./e2e/fixtures/generate-template-assist-pdf.go
package main

import (
	"bytes"
	"image/color"
	"os"
	"path/filepath"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
)

const (
	pageWidthMM  = 210.0
	pageHeightMM = 297.0
	leftMM       = 25.0
	fontSizePT   = 14.0
)

func main() {
	family, _, err := pdfdoc.LoadFont(pdfdoc.FontOptions{})
	if err != nil {
		panic(err)
	}
	face := family.Face(fontSizePT, color.RGBA{A: 255}, canvas.FontRegular)
	if face == nil {
		panic("字体加载失败")
	}

	var buf bytes.Buffer
	r := pdf.New(&buf, pageWidthMM, pageHeightMM, &pdf.Options{Compress: true, SubsetFonts: true})

	// 第 1 页：括号占位符 + 连续下划线。
	page1 := canvas.New(pageWidthMM, pageHeightMM)
	c1 := canvas.NewContext(page1)
	draw(c1, face, 270, "模板制作辅助测试夹具（虚构数据）")
	field(c1, face, 250, "甲方名称：", "【甲方名称】")
	field(c1, face, 238, "乙方姓名：", "【乙方姓名】")
	field(c1, face, 226, "标的名称：", "＿＿＿＿＿＿")
	field(c1, face, 214, "合同金额：", "＿＿＿＿")
	field(c1, face, 202, "签订日期：", "【签订日期】")
	page1.RenderTo(r)

	// 第 2 页：签字盖章区，仍保留连续下划线。
	r.NewPage(pageWidthMM, pageHeightMM)
	page2 := canvas.New(pageWidthMM, pageHeightMM)
	c2 := canvas.NewContext(page2)
	draw(c2, face, 270, "签字与盖章")
	field(c2, face, 250, "甲方签章：", "＿＿＿＿＿＿＿＿")
	field(c2, face, 230, "乙方签字：", "＿＿＿＿")
	page2.RenderTo(r)
	if err := r.Close(); err != nil {
		panic(err)
	}

	out := filepath.Join("e2e", "fixtures", "template-assist.pdf")
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
	os.Stdout.WriteString("已生成：" + out + "\n")
}

func draw(ctx *canvas.Context, face *canvas.FontFace, y float64, text string) {
	ctx.DrawText(leftMM, y, canvas.NewTextLine(face, text, canvas.Left))
}

// field 先画「标签：」再画独立的占位符文本项（x = 标签宽度之后），这样
// PDF 文本层里占位符是单独一项，E2E 的锚点定位可直接取到下划线起点。
func field(ctx *canvas.Context, face *canvas.FontFace, y float64, label, placeholder string) {
	ctx.DrawText(leftMM, y, canvas.NewTextLine(face, label, canvas.Left))
	ctx.DrawText(leftMM+face.TextWidth(label), y, canvas.NewTextLine(face, placeholder, canvas.Left))
}
