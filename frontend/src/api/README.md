# App API 客户端与契约类型

本目录是前端访问后端 `/app/api/v1` 的唯一入口（阶段 1 起）：

```
frontend/src/api/
  client.ts            fetch 封装：base、JSON、错误信封、query、超时/断连
  endpoints.ts         按域导出的 typed 调用面（barrel）
  endpoints/
    onboarding.ts      bootstrap / onboarding / events
    records.ts         fields / parties / contracts / changelog
    seals.ts           seals
    imports.ts         excel 导入导出
    templates.ts       templates / generation / batch / documents / converters
    signing.ts         signature-sessions / signature export
    system.ts          mcp / backup / integrity
  types.ts             契约类型 barrel（统一从 `api/types` 引入）
  types/
    bootstrap.ts       Bootstrap
    common.ts          List / 错误信封 / Stats / ChangeLog / 查询基类
    documents.ts       Signature / OutputDocument / Preflight / Batch
    imports.ts         Excel 导入预览与提交
    onboarding.ts      Onboarding / 自检
    records.ts         Party / Contract / FieldDefinition / Seal / Template
    requests.ts        写入请求体与列表查询参数
    settings.ts        MCP / Backup / Integrity
    signing.ts         SignatureSession
    templates.ts       TemplateCheck / 预览 / 生成响应 / Converter
```

## 使用方式

```ts
import { ApiError } from "../api/client";
import { getBootstrap, listParties } from "../api/endpoints";

async function load(): Promise<void> {
  try {
    const bootstrap = await getBootstrap();
    const parties = await listParties({ limit: 500, q: "村" });
    console.log(bootstrap.current_year, parties.items.length);
  } catch (error) {
    if (error instanceof ApiError) {
      // error.fields 是 422 校验错误里的逐字段问题
      console.warn(error.code, error.message, error.fields);
    }
  }
}
```

- 组件里**禁止裸 `fetch`**；下载用 `getFile` / `postFile`，上传用
  `postForm`，SSE 用 `eventsUrl(after)`。
- 路径参数用 `encodePathSegment`（端点函数内部已处理）。
- query 只用字符串/数字；`undefined`、`null` 与空串会被省略，布尔值不
  自动转换（`archived` 请传 `"1"`）。
- 写操作自动带 `X-Tengshou-App: 1`；multipart 上传不设置 Content-Type。
- 禁止 `any`：动态字段值用 `Record<string, unknown>`，响应边界在
  `client.ts` 内做一次 `unknown → T` 断言。

## 错误处理

所有失败抛 `ApiError`（`api/client.ts`）：

| 场景 | `code` | `status` | 说明 |
| --- | --- | --- | --- |
| 服务端错误信封 | 服务端 `error.code`（如 `validation`、`conflict`） | HTTP 状态 | `fields` 为逐字段错误 |
| 非 JSON 错误响应 | `http_error` | HTTP 状态 | 消息回落为 `请求失败（<status>）` |
| 无法连接 | `network` | 0 | 服务未启动 / 连接被拒绝 |
| 超过 `timeoutMs` | `timeout` | 0 | 默认不设超时；长任务不要设 |
| 成功响应无法解析 | `invalid_response` | HTTP 状态 | 本地服务返回了非 JSON |

调用方传入的 `AbortSignal` 触发时，原样抛出 `AbortError`，不包装成
`ApiError`，便于区分「用户取消」与「请求失败」。

`POST /excel/commit` 的 422 响应同时携带 `error` 与 `result`；用
`ApiError.payload` 取 `ImportCommitErrorPayload`，即可展示逐行错误。

## 契约变更流程

1. 先改 Go 侧结构（或 handler 响应）并确认字段名/JSON tag；
2. **先改** `frontend/src/api/types/` 里对应的类型（以及必要时
   `requests.ts` 的请求体），再改 `endpoints/` 里的函数签名；
3. 调用点最后改；`npm run typecheck` 必须通过；
4. 契约变更写进 PR 说明；禁止用 `any` 兜底。

序列化约定：Go `int`/`float64` → `number`；`time.Time` → RFC3339 字符串；
`omitempty` → 可选属性；Go 的 nil slice 在 JSON 里是 `null`，类型按数组
声明，展示端对可能为空的列表（`warnings`/`issues`/`items`）用 `?? []`。

## 类型 ↔ Go 源码对照清单

阶段 1 落地时逐字段核对了以下 Go 文件（截至 2026-11-23，main `e896825`）：

| 类型模块 | Go 来源 |
| --- | --- |
| `types/bootstrap.ts` | `internal/httpapi/appapi.go`（handleAppBootstrap）、`internal/model/model.go`（Stats/YearCount）、`internal/httpapi/onboarding.go` |
| `types/common.ts` | `internal/model/model.go`（List/Stats/YearCount/ChangeLog/FieldChange）、`internal/httpapi/handlers.go`（writeError/filter）、`internal/httpapi/appapi.go`（appEvent）、`internal/service/service.go`（FieldError/ValidationErrors） |
| `types/records.ts` | `internal/model/model.go`（Party/Contract/FieldDefinition/Seal/Placement/Template/TemplateUsage/ContractDetail） |
| `types/documents.ts` | `internal/model/model.go`（Signature/OutputDocument/DocumentSignatureSnapshot）、`internal/overlay/overlay.go`（Warning）、`internal/service/documents.go`（Preflight/Batch） |
| `types/imports.ts` | `internal/service/excel.go`、`internal/httpapi/appapi.go`（importRequest） |
| `types/onboarding.ts` | `internal/httpapi/onboarding.go`、`internal/service/onboarding.go` |
| `types/settings.ts` | `internal/httpapi/appapi.go`（MCP）、`internal/service/backup.go`、`internal/backup/backup.go`、`internal/httpapi/backupapi.go` |
| `types/signing.ts` | `internal/sign/session.go`、`internal/httpapi/signapi.go` |
| `types/templates.ts` | `internal/service/onboarding.go`（TemplateCheck）、`internal/service/templates.go`、`internal/doctopdf/convert.go`、`internal/httpapi/templateapi.go` |
| `types/requests.ts` | `internal/service/parties.go`、`contracts.go`、`fields.go`、`seals.go`、`renewal.go`、`internal/httpapi/templateapi.go`、`appapi.go`（filter） |

## 端点覆盖清单

`internal/httpapi/server.go` 的 `routes()` 注册了 66 条 `/app/api/v1`
路由（`PUT` 与 `POST /settings/mcp` 共用同一个 handler）。客户端覆盖其中
阶段 1 各域需要的 65 条：

| 域 | 端点函数 | HTTP 路由 |
| --- | --- | --- |
| bootstrap | `getBootstrap` | GET `/bootstrap` |
| onboarding | `getOnboarding`、`getOnboardingCheck`、`updateOnboardingState` | GET `/onboarding`、`/onboarding/check`；POST `/onboarding/state` |
| 事件流 | `eventsUrl` | GET `/events`（SSE） |
| 字段 | `listFields`、`listFieldsForEntity`、`createField`、`updateField`、`archiveField` | GET/POST `/fields`；PUT `/fields/{id}`；POST `/fields/{id}/archive` |
| 相对方 | `listParties`、`getParty`、`createParty`、`updateParty`、`deleteParty` | GET/POST `/parties`；GET/PUT/DELETE `/parties/{id}` |
| 合同 | `listContracts`、`getContract`、`createContract`、`updateContract`、`deleteContract`、`getContractYears`、`getRenewalDraft` | GET/POST `/contracts`；GET/PUT/DELETE `/contracts/{id}`；GET `/contracts/years`；GET `/contracts/{id}/renewal-draft` |
| 变更记录 | `listChangelog` | GET `/changelog` |
| 公章 | `listSeals`、`getSeal`、`importSeal`、`updateSeal`、`replaceSealImage`、`deleteSeal`、`getSealContracts`、`attachSeal`、`detachSeal`、`sealImageUrl` | GET/POST `/seals`；GET/PUT/DELETE `/seals/{id}`；GET `/seals/{id}/image`、`/contracts`；POST `/seals/{id}/replace`、`/attach`、`/detach` |
| Excel | `excelTemplateUrl`、`excelLedgerUrl`、`downloadExcelTemplate`、`downloadExcelLedger`、`previewExcelImport`、`commitExcelImport` | GET `/excel/template`、`/excel/ledger`；POST `/excel/preview`、`/excel/commit` |
| 模板 | `listTemplates`、`getTemplate`、`importTemplate`、`deleteTemplate`、`saveTemplateMapping`、`getTemplateUsage`、`checkTemplate`、`templatePdfUrl`、`templateSourceUrl`、`previewTemplate`、`preflightGeneration`、`generateContract`、`batchPreviewGeneration`、`batchGenerateContracts` | GET/POST `/templates`、`/templates/import`；GET/DELETE `/templates/{id}`；PUT `/templates/{id}/mapping`；GET `/templates/{id}/usage`、`/check`、`/pdf`、`/source`；POST `/templates/{id}/preview`、`/preflight`、`/generate`、`/batch/preview`、`/batch/generate` |
| 生成记录 | `listDocuments`、`documentFileUrl` | GET `/documents`、`/documents/{id}/file` |
| 转换程序 | `listConverters` | GET `/converters` |
| 扫码签名 | `createSignatureSession`、`getSignatureSession`、`cancelSignatureSession`、`signatureSessionQrUrl`、`exportSignature` | POST/GET/DELETE `/signature-sessions`、`/signature-sessions/{id}`；GET `/signature-sessions/{id}/qr.png`；POST `/signatures/{id}/export` |
| MCP | `getMCPStatus`、`updateMCPSettings` | GET `/mcp/status`；PUT/POST `/settings/mcp`（客户端用 POST） |
| 备份/一致性 | `backupExportUrl`、`downloadBackup`、`getBackupStatus`、`restoreBackup`、`getIntegrityReport` | GET `/backup/export`、`/backup/status`；POST `/backup/restore`；GET `/integrity` |

未封装的两类路由：

- `GET /app/api/v1`：接口自描述文档，界面不需要；
- `/api/v1/...`（无 app 前缀）与 `/sign/{token}`：分别面向 MCP/agent 与
  手机签名页，不属于 Vue 前端。
