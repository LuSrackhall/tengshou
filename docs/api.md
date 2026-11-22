# 本地只读数据接口（v1）

腾手的结构化数据通过本机 HTTP 服务对外暴露，供 B6 MCP 连接器与用户侧 AI
agent 取数。接口**只读**：所有数据端点只接受 `GET`（`HEAD` 随 `GET` 自动支持），
写操作只能由桌面应用 / CLI 在进程内完成。

B6 MCP 连接器（`tengshou-cli mcp`）读取时直接使用本接口；它的写工具走仅限
回环的 `/app/api/v1` 应用写入面，不改变本接口的只读性质。注册方式、工具清单
与安全边界见 [mcp.md](mcp.md)。

## 启动

```bash
tengshou-cli serve --data-dir ./data
```

默认监听 `0.0.0.0:8765`（局域网可访问测试页）。若只需本机访问：

```bash
tengshou-cli serve --data-dir ./data --loopback
```

终端会打印本机与局域网地址，以及数据接口令牌。

## 认证

`config.json` 中的 `http.token` 在首次运行时自动生成（48 位十六进制）。
除 `/`、`/api/v1/health` 外，所有数据端点都需要：

```http
Authorization: Bearer <token>
```

或：

```http
X-Api-Token: <token>
```

令牌比较使用常量时间比较。令牌泄露等同于数据泄露，请按密码对待。

## 通用约定

- 响应：`application/json; charset=utf-8`，`Cache-Control: no-store`。
- 错误：`{"error":{"code":"...","message":"..."}}`。
- 分页：`?limit=50&offset=0`，`limit` 上限 500；响应为
  `{"items":[...],"total":n,"limit":50,"offset":0}`。
- 时间：RFC3339 UTC 字符串；日期字段为 `YYYY-MM-DD`。
- `data` 字段为对象，承载合同 / 相对方的扩展字段（B2 动态字段）。
- 接口契约版本：`tengshou.data.v1`（见 `/api/v1/schema`）。

## 端点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/` | 局域网连通性测试页（公开） |
| GET | `/api/v1` | 接口目录 |
| GET | `/api/v1/health` | 健康检查（公开） |
| GET | `/api/v1/schema` | 机器可读的数据契约与字段说明 |
| GET | `/api/v1/stats` | 各类记录数量 |
| GET | `/api/v1/fields?entity=contract\|party` | 字段定义（运行时新增字段立即可见） |
| GET | `/api/v1/parties?q=&limit=&offset=` | 相对方列表（按姓名 / 证件号 / 电话搜索） |
| GET | `/api/v1/parties/{id}` | 相对方详情 |
| GET | `/api/v1/contracts?q=&status=&year=&limit=&offset=` | 合同列表；`year` 省略即跨年查询 |
| GET | `/api/v1/contracts/{id}` | 合同详情（相对方、签名、公章关联、模板版本、PDF 记录） |
| GET | `/api/v1/templates?q=&limit=&offset=` | 模板列表（不含 HTML 正文） |
| GET | `/api/v1/templates/{id}` | 模板详情（含 HTML） |
| GET | `/api/v1/signature-assets?q=&contract_id=&limit=&offset=` | 签名图片元数据（含合同、内容版本关联；不含图片内容） |
| GET | `/api/v1/documents?contract_id=&limit=&offset=` | 已生成 PDF 记录 |

### 示例

```bash
TOKEN="$(python3 - <<'PY'
import json, os
print(json.load(open(os.path.expanduser('~/Library/Application Support/Tengshou/config.json')))['http']['token'])
PY
)"

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8765/api/v1/contracts?status=active'

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8765/api/v1/contracts/demo-contract-1'
```

合同详情响应：

```json
{
  "contract": {
    "id": "demo-contract-1",
    "contract_no": "DEMO-2026-001",
    "year": 2026,
    "title": "幸福村 3 组 12 号房屋租赁合同",
    "type": "房屋租赁",
    "amount": 1200,
    "status": "active",
    "party_a_id": "demo-party-a",
    "party_b_id": "demo-party-b",
    "template_id": "demo-template-basic",
    "signed_on": "2026-11-22",
    "data": {"rent": 1200, "address": "幸福村 3 组 12 号", "...": "..."},
    "created_at": "2026-11-22T11:00:00Z",
    "updated_at": "2026-11-22T11:00:00Z"
  },
  "party_a": {"id": "demo-party-a", "kind": "organization", "name": "幸福村股份经济合作社"},
  "party_b": {"id": "demo-party-b", "kind": "person", "name": "张三"},
  "template": {"id": "demo-template-basic", "name": "房屋租赁合同（示例）", "version": 1},
  "signatures": [{"id": "demo-signature-1", "party_id": "demo-party-b", "sha256": "...", "..." : "..."}],
  "documents": [{"id": "demo-document-1", "page_count": 2, "sha256": "...", "..." : "..."}]
}
```

## 应用写入接口（仅本机）

台账界面使用 `/app/api/v1/*`：字段、档案、合同 CRUD，变更记录，Excel 模板 /
台账下载与导入预览/提交，以及签名会话（B5）。该接口**只接受回环地址**，写请求
必须带 `X-Tengshou-App: 1`；跨站 Origin 会被拒绝。它不面向外部 agent，契约也不
承诺稳定。字段、合并与验收细节见 [b2-data.md](b2-data.md)；签名流程、安全边界
与端点见 [b5-signature.md](b5-signature.md)。

签名会话响应中的 `urls` 是手机可访问的局域网签名页地址，`qr_url` 是二维码
PNG。二维码中的一次性 token 只出现在 URL 里，不出现在 JSON 响应中。

## 稳定性

- 字段只增不改名；删除或改名会提升 `schema` 版本。
- `data` 内的键属于使用者模板，接口原样透传，不做平台字段对齐。
- `/api/v1/schema` 是字段说明的权威来源，包含类型与中文描述。

## 安全

- 只读：本接口无写接口；MCP 连接器的写工具复用仅限回环的
  `/app/api/v1` 应用写入面，并受 `config.json` 的 `mcp.write_enabled` 约束。
- 默认仅监听局域网，不对公网暴露；无外部服务器依赖。
- 测试页不展示合同数据；数据接口需要令牌。
- 手机签名页（`/sign/<token>`）是唯一面向局域网的写入口：一次性短时令牌、
  仅接受私网来源、提交只接受 PNG；它不携带任何合同数据接口权限。
- 签名图片目前只返回元数据（路径、摘要、尺寸），不提供图片下载。
