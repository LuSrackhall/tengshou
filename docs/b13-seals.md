# B13 公章图片与 PDF 落位

公章在腾手里是**图片资产**：导入后保存为数据目录下的独立文件，数据库只记录
路径、SHA-256 摘要与尺寸；模板决定它在 PDF 中的位置和大小。本版只做图片
落位，不承担其它用途，也不对文件效力作任何判断。

## 用户流程

1. 打开 `/app/` →「公章」页 →「导入公章图片」。
2. 选择 PNG / JPG，填写公章名称，可关联到单位档案（例如甲方合作社）。
   - PNG 透明底保持不变；
   - JPG 或白底 PNG 会自动去除白底、裁剪空白边距，输出透明底 PNG；
   - 预览区显示实际保存的图片。
3. 在公章卡片点「关联合同」，可勾选**一份或多份**合同（批量落位）；同一枚
   公章可以被多份合同复用。
4. 在模板中放置盖章占位（见下节）。生成 PDF 时，图片落在模板指定的位置。
5. 换模板重新生成时，公章继续复用；新模板里的占位决定新位置，无需重新导入。

## 资产存储

| 项目 | 说明 |
| --- | --- |
| 文件 | `<数据目录>/assets/seals/<seal_id>-<sha256 前 12 位>.png` |
| 数据库 | `seal_assets`：名称、所属单位、相对路径、SHA-256、MIME、像素尺寸、说明 |
| 合同关联 | `contract_seals`：合同与公章多对多，记录落位时间 |
| 摘要 | 保存的是归一化后 PNG 的 SHA-256；替换图片会产生新的文件名与摘要 |

图片不写入数据库单元格，也不使用 `data:` URL 或绝对路径；渲染器只接受数据
目录内的安全相对路径。

## 模板占位

模板数据新增：

| 字段 | 说明 |
| --- | --- |
| `.Seals` | 当前合同可用的公章：已关联公章在前，其次是甲乙双方单位名下的公章 |
| `.ContractSeals` | 只包含显式关联到当前合同的公章 |

辅助函数：

| 函数 | 说明 |
| --- | --- |
| `sealPath .Seals .PartyA` | 取甲方单位名下最新的公章路径；传 `.PartyB` 或单位 id 同理 |
| `contractSealPath .ContractSeals` | 取当前合同显式关联的最新公章路径 |

两者在没有可用公章时返回空字符串，配合 `{{with}}` 可安全省略：

```html
<p style="margin-top:10mm">甲方（盖章）：{{.PartyA.Name}}</p>
{{with sealPath .Seals .PartyA}}
<p style="text-align:right"><img src="{{.}}" style="width:40mm"></p>
{{end}}
```

合同级关联的写法：

```html
{{with contractSealPath .ContractSeals}}
<p style="text-align:center"><img src="{{.}}" style="width:42mm"></p>
{{end}}
```

位置由 `<p style="text-align:...">` 的水平和 `<img style="width:...">` 的
尺寸控制；应用内「预览」对话框可以调整宽度与水平位置，并复制对应的模板
片段。换模板时把这段放进新模板的盖章位置即可。

## 应用接口

以下端点仅在回环地址可用；写操作需 `X-Tengshou-App: 1` 与 JSON 请求体。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/app/api/v1/seals` | 公章列表；`q=` 搜名称/说明，`party_id=` 按单位筛选 |
| POST | `/app/api/v1/seals` | 导入图片：`{name, party_id?, note?, file_base64}` |
| GET | `/app/api/v1/seals/{id}` | 公章元数据 |
| GET | `/app/api/v1/seals/{id}/image` | 归一化后的 PNG（预览） |
| GET | `/app/api/v1/seals/{id}/contracts` | 已关联的合同 id 列表 |
| PUT | `/app/api/v1/seals/{id}` | 修改名称 / 所属单位 / 说明 |
| POST | `/app/api/v1/seals/{id}/replace` | 替换图片：`{file_base64}` |
| DELETE | `/app/api/v1/seals/{id}` | 删除公章并解除全部合同关联 |
| POST | `/app/api/v1/seals/{id}/attach` | 批量关联：`{contract_ids: [...]}` |
| POST | `/app/api/v1/seals/{id}/detach` | 批量解除：`{contract_ids: [...]}` |

合同详情 `GET /api/v1/contracts/{id}` 的 `seals` 字段返回显式关联的公章；
`GET /api/v1/stats` 新增 `seals` 计数。

## 删除、替换与审计

- **替换**：确认后写入新文件、更新路径与摘要；之后生成的 PDF 使用新图片，
  历史 PDF 文件不受影响。变更记录保留旧摘要 → 新摘要。
- **删除**：确认后删除记录、图片文件，并解除所有合同关联；变更记录写明
  删除了哪枚公章、解除了多少份合同的关联。
- **关联 / 解除**：每份受影响合同都会写一条变更记录，便于按合同追溯。
- 所有写操作仍由应用接口完成，外部只读数据接口保持不变。

## 与措辞检查的联动

界面、文档与代码中的用户可见文案统一使用「公章图片」「图片落位」描述本
功能，不出现超出图片落位范围的说法。`internal/httpapi/seal_wording_test.go`
会扫描应用静态资源与文档，防止后续改动引入这类表述。

## 验证

```bash
go vet ./...
go test -count=1 ./...
```

覆盖点：

- `internal/seal`：PNG 透明底保留、JPG 白底转透明、空白边距裁剪、过小 /
  过大 / 非图片拒绝。
- `internal/service`：导入落盘与摘要、单位关联、替换摘要变更、删除解除关联、
  批量关联、合同与单位公章合并、变更记录。
- `internal/pdfdoc`：`sealPath` / `contractSealPath` 取值；含公章 PDF 生成。
- `internal/httpapi`：导入 → 预览图片 → 批量关联 → 合同详情包含公章 →
  替换 → 删除；守卫与参数校验。
- `internal/demo`：示例公章与含公章 PDF。

手工验证：

```bash
go run ./cmd/tengshou-cli serve --data-dir ./data
# 打开 http://127.0.0.1:8765/app/ →「公章」→ 导入 PNG/JPG
# 关联一份或多份合同；在「预览」里调整宽度与位置并复制模板占位
go run ./cmd/tengshou-cli render --data-dir ./data --contract <合同 id>
# PDF 中公章落在模板指定位置；换模板后再 render，公章按新位置落位
```
