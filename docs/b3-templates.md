# PDF 模板：Word 导入 → 页面标定 → 套印生成（B3）

B3 的用户路径只有一条：**用户导入自己已经用惯的 Word 合同模板 → 程序把 Word
转成 PDF → 在 PDF 页面上拖框标定字段 / 签名 / 公章 → 预览确认 → 套印生成最终
PDF**。用户始终只编辑 Word，不学习 HTML，也不经过向导。

## 1. 用户流程

1. 「模板」页 →「导入模板」选择 `.docx`：
   - Windows 且本机安装了 Microsoft Word：用 Word 自身导出 PDF，效果与 Word 中
     所见一致；
   - 没有 Word 时使用 LibreOffice headless；
   - 两者都不可用、或用户已经自行导出：可以直接导入 `.pdf`。
2. 导入后立即进入标定编辑器：PDF.js 把转换后的 PDF 页面渲染成画布，用户从右侧
   选择「字段文字 / 手写签名 / 公章图片」并添加位置，然后拖动、缩放框；字段可设
   字号与对齐。
3. 点「保存为新版本」：当前标定保存成一个新的模板版本。导入本身创建 v1，第一次
   保存标定创建 v2，之后每次保存递增。
4. 「样例预览」用内置示例值检查位置；「合同预览」用选中的真实合同数据（含已采集
   签名与公章图片）预览。
5. 只有用目标合同预览过之后，「生成 PDF」才可用（服务端同样校验预览摘要）。
   生成的 PDF 落盘到 `<data>/outputs/`，记录到 `output_documents`，并可在界面里
   查看或下载。

## 2. 转换方案

| 环境 | 方案 | 说明 |
| --- | --- | --- |
| Windows | Microsoft Word COM | 通过 `powershell.exe -Command` 调用 `Word.Application`，以 `ExportAsFixedFormat(..., 17)` 导出 PDF。保真度最高，所见即所得。无需 cgo。 |
| macOS / Windows / Linux | LibreOffice headless | `soffice --headless --norestore --convert-to pdf:writer_pdf_Export`，使用临时用户配置目录，可与用户已打开的 LibreOffice 并存。个别字体/行距可能有细微差异，导入时会提示。 |
| 任意平台 | 直接导入 PDF | 用户先用 Word / WPS「另存为 PDF」；不经过转换，保真度取决于用户导出的文件。 |

- 覆盖路径：`TENGHSHOU_SOFFICE`（soffice 可执行文件）、`TENGHSHOU_POWERSHELL`。
- 两者都不可用时返回明确提示：安装 Word（Windows）或 LibreOffice，或先导出
  PDF 再导入。
- 原始 `.docx` 作为资产保留（`templates/pdf/<id>/source.docx`），转换后的 PDF
  为 `template.pdf`，均只记录相对路径与 SHA-256。

## 3. 套印技术选型

| 关注点 | 选型 | 原因 |
| --- | --- | --- |
| 在已有 PDF 上叠加内容 | [pdfcpu](https://github.com/pdfcpu/pdfcpu) v0.11.0（Apache-2.0，纯 Go） | 用「多页 PDF 水印」把套印层逐页贴到模板页上；单次读写、保留原 PDF 的字体/图片/分页；支持 Windows/macOS/Linux，无外部进程依赖。 |
| 套印层绘制 | [tdewolff/canvas](https://github.com/tdewolff/canvas)（仓库已有依赖） | 生成透明背景的 PDF，中文嵌入字体、字宽测量、图片与 PNG 组合都已有基础；与 B1 的 HTML→PDF 共用同一套字体。 |
| 页面预览/标定 | [PDF.js](https://mozilla.github.io/pdf.js/) 3.11.174（Apache-2.0，本地随包） | 浏览器/WebView 内渲染 PDF 页面，离线可用；CSP 只允许同源脚本与 worker。 |

pdfcpu 使用 `api.PDFMultiWatermarkForReadSeeker` + `api.AddWatermarks`：套印 PDF
与模板 PDF 页数、页面尺寸（含旋转后的可视尺寸）一一对应，`Scale=1`、
`Pos=BottomLeft`、`Dx/Dy=0`、关闭默认 45° 旋转，因此套印层与模板页 1:1 对齐。
最终 PDF 只增加透明内容层，不栅格化模板。

未选择的方案：UniPDF（AGPL/商业许可）、Ghostscript/poppler 外部进程（安装
依赖与 Windows 可信性差）、把模板页转成图片再叠加（会损失可打印的矢量清晰度）。

## 4. 坐标与绑定

- 坐标单位是 **PDF 点（pt，1/72 英寸）**，原点在**页面左上角**，`x,y` 是框的
  左上角，`w,h` 是宽高；页面号从 1 开始。页面尺寸使用「已应用 `/Rotate` 后的
  可视尺寸」，与编辑器和浏览器中看到的页面一致。
- 字段绑定键：
  - `contract.<内置键>`：`contract_no`、`year`、`title`、`type`、`party_b`、
    `amount`、`signed_on`、`valid_from`、`valid_to`、`status`；
  - `contract.amount.words` / `contract.data.<金额字段键>.words`：金额中文大写
    （如「人民币壹仟伍佰元整」）。表单里每个金额字段都可保存一个手填覆盖
    （`contract.data.__amount_words__` / `__words__<金额字段键>`），覆盖优先
    于自动换算；这些保留键不进 Excel 台账，也不新增字段定义；
  - `contract.data.<动态字段键>`：B2 动态字段；
  - `party_a.<内置键>` / `party_b.<内置键>`：`name`、`kind`、`id_number`、
    `phone`、`address`、`note`；
  - `party_a.data.<动态字段键>` / `party_b.data.<动态字段键>`。
- 图片类型字段不能作为文字绑定（会提示改用签名/公章位置）。
- 签名位置绑定 `party_a` / `party_b`，取该方最新的一份签名；没有签名时不跨方
  借用。预览里显示占位框；最终生成时缺少签名会**阻断**并提示先扫码采集（B4，
  见 [b4-generation.md](b4-generation.md)）。
- 公章位置有两种来源：
  - `contract`（默认）：优先取合同显式关联的公章，其次取甲/乙双方名下的公章；
  - `seal`：固定某一枚公章图片。位置绑定存在但没有可用公章图片时，生成会被阻断。
- 文字默认 10.5pt、自动换行；超出框高时逐步缩小字号（最低 4pt），仍放不下时
  返回 `text_overflow` 警告。日期自动显示为「2026年1月1日」，金额显示为
  「1,200.00」。

## 5. 版本模型

- 每个版本是 `templates` 表中的一行：`name` 相同、`version` 递增、`kind=pdf`，
  并保存原始文件路径、转换 PDF 路径、页数、转换方式、文件摘要与位置映射
  （`mapping_json`）。
- 只修改标定位置时，新版本与旧版本共享同一份 PDF/源文件资产；重新导入 Word 时
  生成新的资产目录和新版本，需要重新标定。
- 合同通过 `contracts.template_id` 引用**具体版本**；`output_documents` 同时记录
  模板版本。因此模板更新不会改变已生成 PDF，旧合同重出时仍按旧版本渲染，新合同
  默认使用最新版本。B4 起生成记录还包含内容指纹、签名快照、操作人、初次/整改
  与失败原因，详见 [b4-generation.md](b4-generation.md)。
- 删除版本前检查引用：被合同或 PDF 记录引用的版本不允许删除；未被引用的版本
  删除时同时清理独占的资产文件。
- 模板变更影响范围在保存/导入响应中返回（`usage`）：合同数、历史 PDF 数、仍使用
  旧版本的合同数。

## 6. 应用接口（仅回环）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/app/api/v1/templates?kind=pdf` | 模板版本列表（元数据） |
| GET | `/app/api/v1/templates/{id}` | 模板详情（含 placements） |
| POST | `/app/api/v1/templates/import` | `{name, file_name, file_base64}` 导入 `.docx`/`.pdf`，同名递增版本 |
| PUT | `/app/api/v1/templates/{id}/mapping` | `{placements}` 保存标定，生成新版本 |
| GET | `/app/api/v1/templates/{id}/usage` | 影响范围统计 |
| GET | `/app/api/v1/templates/{id}/pdf` | 模板 PDF（PDF.js 使用） |
| GET | `/app/api/v1/templates/{id}/source` | 原始 `.docx` / `.pdf` 下载 |
| POST | `/app/api/v1/templates/{id}/preview` | `{contract_id}` 或 `{sample:true}`，返回 `pdf_base64`、`preview_sha256`、`warnings` |
| POST | `/app/api/v1/templates/{id}/generate` | `{contract_id, preview_sha256}`，生成并记录 PDF |
| GET | `/app/api/v1/documents/{id}/file` | 打开/下载已生成 PDF |
| GET | `/app/api/v1/converters` | Word / LibreOffice 可用性 |
| DELETE | `/app/api/v1/templates/{id}` | 删除未被引用的版本 |

写入请求仍要求 `X-Tengshou-App: 1` 且只允许回环访问。

**预览先行**：`preview` 返回的 `preview_sha256` 覆盖模板版本、位置映射与合同
内容版本（或固定的样例标记）。`generate` 必须回传完全相同的摘要，否则返回
`409 preview_required`。这保证批量 / 单份生成前一定经过预览，且预览后修改合同
内容不会被悄悄跳过。

## 7. 警告代码

| 代码 | 含义 |
| --- | --- |
| `missing_value` | 绑定的字段没有值（预览/生成跳过该位置） |
| `text_overflow` | 文字缩到最小字号仍超出框，需要加大框或缩短内容 |
| `missing_image` | 尚未采集签名 / 未导入对应公章（预览显示占位框） |
| `image_load_failed` | 资产文件缺失或图片无法解码 |
| `stale_signature` | 签名采集于合同内容修改之前，需核对 |
| `invalid_box` / `unknown_binding` | 旧数据或异常映射，服务端保存时已拦截 |

## 8. 边界与安全

- 模板导入与生成都走 `/app/api/v1`：只回环、写请求需要 `X-Tengshou-App`；PDF.js
  与 worker 从同一个本地服务加载，CSP 不允许外部脚本。
- 公章仍然只保存路径 + SHA-256，图片不进入数据库；签名与公章的落位只是图片
  叠加，不宣称具备数字签名或认证效力。
- 模板 PDF、原始 Word、输出 PDF 都在数据目录内；路径经过数据目录越界校验。
- v0.1 不支持动态循环表格；这类需求后续另行设计。

## 9. 验证

- `go vet ./...`、`go test -count=1 ./...` 全通过。
- `internal/overlay`：用 canvas 构造模板 PDF，构建套印层并 pdfcpu 合成，校验页数、
  页面尺寸、套印层文字（中文/编号）、图片位置与输出结构；覆盖文字溢出与缺图警告。
- `internal/service`：导入（直接 PDF）、保存标定（版本递增）、样例/真实合同预览、
  预览摘要校验、生成并记录 PDF、合同版本固定、旧版本重出、影响统计、删除保护、
  非法绑定（未知字段/图片字段/越界/无签名方/未选公章）。
- `internal/doctopdf`：转换器顺序、错误信息、LibreOffice 路径在有 soffice 的机器上
  真实转换（没有则跳过）；Word 路径仅 Windows 可用（本机 macOS 无法实测）。
- `internal/httpapi`：导入/列表/PDF 下载/非法映射 422/预览/生成（缺少摘要 409）/
  输出文件下载/转换器列表/PDF.js 静态资源与 CSP。
- 桌面界面：用无头 Chrome 驱动完整流程（打开模板页 → 打开编辑器 → 拖动框
  x 从 120→160 → 新增位置 → 保存 v3 → 真实合同预览 → 生成 PDF），无控制台错误。
