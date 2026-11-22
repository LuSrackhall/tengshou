# HTML 合同模板说明

模板是普通 HTML 文件，先用 Go `html/template` 渲染数据，再经腾手的排版器
生成 A4 PDF。模板中的文字会自动进行 HTML 转义，不会注入执行。

## 数据模型

模板中可使用：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `.Contract` | 对象 | 合同记录（`Title`、`SignedOn`、`ID`、`Status`…） |
| `.PartyA` | 对象 / 空 | 甲方档案（`Name`、`IDNumber`、`Phone`、`Address`、`Data`） |
| `.PartyB` | 对象 / 空 | 乙方档案 |
| `.Signatures` | 数组 | 签名资产元数据（`AssetPath`、`SignerName`、`SHA256`…） |
| `.Data` | 对象 | 合同扩展字段（租金、地址、条款正文等） |
| `.Now` | 时间 | 生成时间 |

辅助函数：

| 函数 | 示例 | 输出 |
| --- | --- | --- |
| `cnDate` / `date` | `{{cnDate .Data.signed_on}}` | `2026年11月22日` |
| `money` | `{{money .Data.rent}}` | `1,200.00` |
| `default` | `{{default "—" .PartyA.IDNumber}}` | 空值回退 |

示例：

```html
<h1 style="text-align:center">房屋租赁合同</h1>
<p>甲方：{{.PartyA.Name}}</p>
<p>月租金：{{money .Data.rent}} 元</p>
<p>签署日期：{{cnDate .Data.signed_on}}</p>
```

模板采用严格模式：引用不存在的 `.Data` 键会报错，避免生成空白合同。

## 支持的 HTML

块级元素：

- `h1`–`h6`：标题（自动加粗、段前段后间距；`h1` 默认居中）
- `p`：段落
- `div` / `section` / `article` / `main` / `header` / `footer`：容器
- `ul` / `ol` / `li`：列表
- `table` / `thead` / `tbody` / `tfoot` / `tr` / `th` / `td`：表格（等宽列，表头
  灰底加粗，跨页重复表头）
- `hr`：分隔线
- `img`：图片（需位于独立段落或块级；支持 PNG / JPEG / GIF）
- `br`：段内换行
- `div class="page-break"`：强制分页

行内元素：`strong`/`b`、`em`/`i`、`span`、`a`、`u`、`small`、`big`。

## 支持的内联样式

写在元素的 `style` 属性中：

| 属性 | 取值 | 说明 |
| --- | --- | --- |
| `font-size` | `12pt`、`16px`、`4mm`、`1.2em` | 字号 |
| `color` | `#333`、`#333333`、`rgb(0,0,0)`、`black` 等 | 文字颜色 |
| `text-align` | `left` / `center` / `right` | 对齐 |
| `line-height` | `1.6`、`160%`、`20pt` | 行高 |
| `margin-top` / `margin-bottom` | `6mm`、`12pt` | 段前 / 段后间距 |
| `text-indent` | `8mm` | 首行缩进 |
| `font-weight` | `bold` / `normal` | 行内加粗 |
| `font-style` | `italic` / `normal` | 行内斜体 |
| `page-break-before` | `always` | 段前分页 |
| `width` / `height`（`img`） | `45mm`、`200px` | 图片尺寸 |

`class="center"` 等价于居中；`<center>` 也可用但不推荐。

## 图片

图片路径必须是**相对于数据目录**的安全相对路径：

```html
<p><img src="assets/signatures/demo-signature.png" style="width:45mm"></p>
```

- 禁止绝对路径、`..`、`http(s)://` 与 `data:` URL。
- 只指定宽度时按原比例缩放；都不指定时按 96 DPI 计算。
- 图片放不下时自动换页；超高图片按页高缩放。
- 图片必须单独成段（段内不能混排文字），否则排版器会报错提示。

## 字体

PDF 使用一份覆盖中文的字体（见 README「中文字体」）。标题 / 表头用合成加粗，
无需单独的粗体字体文件。若模板包含字体不支持的字符，生成结果会给出
`Missing` 字形提示（CLI 会打印，测试会失败）。

## 当前不支持

- 外部 CSS 文件、`<style>` 选择器与 `class` 样式表（仅支持内联样式与少量 class）。
- 浮动、多栏、绝对定位、自定义页眉页脚。
- 段内混排图片、SVG、背景图。
- JavaScript。

这些限制是为了保证「纯 Go、离线、可测试」的确定性排版；后续模板管理
（B3）可在此基础上增加受控能力。
