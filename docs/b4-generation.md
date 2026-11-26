# 合同 PDF 生成与整改重出（B4）

合同 PDF 是本系统的**核心主产物**：用户导入 Word 模板并在页面位置标定后，腾手把
合同记录、相对方档案、签名与公章按标定位置套印到模板 PDF 上。B4 把「生成」从一次
按钮动作升级为可追溯、可整改重出、可批量执行的交付流程。

## 1. 生成路径

1. **合同详情「生成 PDF」**：在合同台账行点「生成 PDF」（或打开合同后滚动到
   「合同 PDF 生成」区块），选择模板版本 →「预览」→ 确认版式与数据 →「生成
   PDF」→ 下载。
2. **批量生成**：合同台账勾选多份合同 →「批量生成 PDF」；或在「模板」页点
   「批量生成」把当前台账（年度/状态/搜索过滤后）的合同带入批量对话框。
3. **整改重出**：模板换版后，在「模板」页对新版本点「整改重出」，系统列出仍然
   引用该模板系列旧版本的合同，逐份预检后批量重出。

所有生成都要求先预览：服务端校验 `preview_sha256`（模板版本 + 标定位置 + 合同
内容摘要），预览后修改了合同内容或切换模板版本都必须重新预览。

## 2. 输出前检查（阻断 vs 警告）

生成前逐份检查，**阻断项**会拒绝生成并记录一条失败记录；**警告**只提示，不阻断：

| 检查 | 结果 |
| --- | --- |
| 合同必填字段（内置 `contract_no` / `year` / `title` / `party_b` 与用户新增的必填字段） | 阻断 `missing_field` |
| 相对方（乙方）档案及其必填字段；模板引用了甲方信息时检查甲方档案 | 阻断 `missing_party` |
| 模板版本与标定位置有效、PDF 资产存在 | 阻断 `invalid_template` / `missing_asset` |
| 每个签名位置都有对应方的签名记录，且签名图片文件存在 | 阻断 `missing_signature` |
| 签名的 `content_sha256` 与当前合同内容版本一致 | 阻断 `content_changed`（仅现场手写签名；档案复用不绑定内容版本） |
| 每个公章位置能解析到公章图片且文件存在 | 阻断 `missing_seal` |
| 文字超出标定框、可选字段无值等版式问题 | 警告（`text_overflow` / `missing_value`） |

旧签名（B5 之前没有 `content_sha256` 的记录）无法判断内容版本，按兼容策略允许
复用，不做内容版本提示。

## 3. 内容指纹与签名复用

- 合同内容指纹使用 B5 的 `service.ContentSHA256`：合同稳定字段 + 动态 `data` 的
  规范化 JSON，不含时间戳。模板换版不会改变内容指纹。
- 生成时把当前内容指纹写入输出记录，并把实际套印的签名（`signature_id`、签名人、
  图片 SHA-256、来源、采集时内容版本）写入 `signatures` 快照；`source` 区分
  现场手写（`lan_qr`）与档案复用（`archive`）。
- 若现场手写签名采集后合同**实质字段**发生变化：
  - 默认拒绝生成，返回 `content_changed`，提示重新扫码签名；
  - 用户可显式勾选「保留旧签名重出」（单份二次确认 / 批量勾选），服务端在审计中
    追加一条「确认保留旧签名重出 PDF」记录，并继续生成。
- 档案签名复用（v0.2 D3=B）直接套印、不绑定内容版本：合同内容修改不会触发
  `content_changed`，也无需逐份确认；签署事实由出租方用户自行确认。
- 仅在内容未变时自动复用现场手写签名，不存在「默默沿用旧签名」的路径。

## 4. 生成记录（`output_documents`）

每次进入生成流程的尝试都写一行（未通过 `preview_required` 校验的请求不落记录），
成功与失败都保留：

| 字段 | 说明 |
| --- | --- |
| `contract_id` / `template_id` / `template_version` | 哪份合同、哪个模板版本 |
| `content_sha256` | 生成时的合同内容版本 |
| `signatures` | 签名引用快照（JSON） |
| `operator` | 操作人（本机用户或 `tengshou-mcp`） |
| `kind` | `initial`（初次生成）/ `regenerate`（整改重出） |
| `status` | `success` / `failed` |
| `error_message` | 失败原因（失败记录无文件） |
| `batch_id` | 批量生成批次 |
| `source_document_id` | 整改重出时被替换的来源文档 |
| `file_path` / `sha256` / `page_count` | 输出文件位置与摘要 |
| `created_at` | 生成时间 |

查询接口：

```
GET /app/api/v1/documents?contract_id=&template_id=&batch_id=&status=&limit=&offset=
GET /api/v1/documents?contract_id=&template_id=&batch_id=&status=&limit=&offset=
```

合同详情（`/app/api/v1/contracts/{id}` 与应用接口）中的 `documents` 即该合同的完整
生成历史。界面上按时间倒序展示：时间、操作人、模板版本、内容短哈希、类型、状态、
文件/失败原因；失败记录可「重试」（重新预览后再生成）。

`stats.documents` 与模板影响统计只统计成功记录；失败记录不会让模板无法删除。

## 5. 整改重出（同内容换壳）

- 模板每次保存标定都会生成新版本，合同通过 `template_id` 引用某一具体版本。
- 换版后重出：选择新版本 → 重新预览（`preview_sha256` 覆盖新版本与标定）→ 生成。
  内容未变时签名自动保留；内容变更时按第 3 节阻断或显式确认。
- 输出记录标记 `kind=regenerate`，并写入 `source_document_id`（默认取该合同最近
  一份成功文档）。
- 生成成功会把合同钉到此次使用的模板版本；旧版本与其历史 PDF 不受影响。

## 6. 批量生成

1. 选择模板版本与多份合同（台账多选，或模板页带入当前过滤后的台账）。
2. 「预检 / 刷新状态」：`POST /app/api/v1/templates/{id}/batch/preview`
   - 逐份返回 `ready` / `needs_signature` / `needs_resign` / `missing_fields` /
     `missing_party` / `missing_seal`、问题清单、内容版本与预览摘要；
   - 返回一份样例数据 PDF（现有 Sample 机制），供查看新模板版式；
   - 不符合的合同列明原因，不参与生成。
3. 「开始批量生成」：`POST /app/api/v1/templates/{id}/batch/generate`
   - 逐份同步处理；单份失败不会中断其余合同；
   - 每份成功/失败分别写生成记录（同一 `batch_id`）；
   - 结果逐份展示，失败可单份「重试」。
4. 「保留内容变更前的旧签名重出」勾选后，仅对「签名未失效、内容已变化」的合同生效，
   每份都会写入确认审计。

单批上限 200 份（防止误操作触发大量渲染），超出时请分批处理。

## 7. 端点汇总（应用写入面，仅回环 + `X-Tengshou-App: 1`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/app/api/v1/templates/{id}/preflight` | 单份生成前检查，返回 `preview_sha256` |
| POST | `/app/api/v1/templates/{id}/preview` | 渲染预览 PDF（B3，不变） |
| POST | `/app/api/v1/templates/{id}/generate` | 生成单份；可选 `kind` / `source_document_id` / `allow_stale_signature` |
| POST | `/app/api/v1/templates/{id}/batch/preview` | 批量预检 + 样例 PDF |
| POST | `/app/api/v1/templates/{id}/batch/generate` | 批量生成（逐份结果） |
| GET | `/app/api/v1/documents` | 生成历史查询（合同/模板/批次/状态） |

业务错误码：`preview_required`、`content_changed` 返回 409；`missing_signature`、
`missing_field`、`missing_party`、`missing_seal`、`preflight_failed` 返回 422。

## 8. 兼容与审计

- 迁移 `0007_b4_generation.sql` 只新增字段并给出默认值（`kind=initial`、
  `status=success`），旧数据无需改写。
- 初次生成/整改重出/失败/保留旧签名确认都会写变更记录，经 B18 的 SSE 自动反映到
  界面；MCP 写入仍记为 `tengshou-mcp`。
- 不新增第三方依赖；失败记录不指向文件，下载失败记录返回 404。
