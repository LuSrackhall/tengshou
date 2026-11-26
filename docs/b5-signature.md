# B5 局域网扫码手写签名

签名是**数据**，不是流程步骤：合同和相对方档案都可以持有签名，模板决定签名
放在 PDF 的什么位置。本版实现「电脑生成一次性二维码 → 手机同局域网浏览器手写 →
签名回传电脑本机保存」的闭环。

## 用户流程

1. 电脑打开 `/app/` 合同台账，在合同行点「扫码签名」。
2. 选择签名人（甲方 / 乙方档案），点「生成签名二维码」。
3. 手机与电脑处于同一局域网时扫码，浏览器打开 `/sign/<token>`：
   - 手写画布、清除重写、提交；
   - 不需要安装 App，不经过任何公网服务器。
4. 手机提交后，电脑弹窗自动轮询到「签名已保存」，签名图片落到
   `<data>/assets/signatures/<sig_id>.png`，元数据写入 `signature_assets`。
5. 生成 PDF 时，模板用 `signaturePath` 取签名路径，例如：

   ```html
   <p>乙方（签字）：</p>
   {{with signaturePath .Signatures .PartyB}}
   <p><img src="{{.}}" style="width:45mm"></p>
   {{end}}
   ```

   内置示例模板 `contract_basic.html` 已采用该写法，签名为透明底黑墨 PNG，
   按 45mm 宽排版打印。

## 端点

| 方法 | 路径 | 访问范围 | 说明 |
| --- | --- | --- | --- |
| POST | `/app/api/v1/signature-sessions` | 仅回环 | 创建签名会话，返回二维码地址 |
| GET | `/app/api/v1/signature-sessions/{id}` | 仅回环 | 查询会话状态（pending / submitting / completed / expired） |
| DELETE | `/app/api/v1/signature-sessions/{id}` | 仅回环 | 取消会话，二维码立即失效 |
| GET | `/app/api/v1/signature-sessions/{id}/qr.png` | 仅回环 | 二维码 PNG；`?index=` 选择多网卡地址 |
| GET | `/sign/{token}` | 局域网 | 手机签名页（自包含 HTML/JS） |
| GET | `/sign/{token}/status` | 局域网 | 手机侧状态轮询 |
| POST | `/sign/{token}/submit` | 局域网 | 提交手写 PNG |

创建请求：

```json
{"contract_id": "ctr_xxx", "party_id": "pty_xxx", "signer_name": "张三"}
```

`party_id` 省略时默认合同相对方（乙方），也可以显式选择甲方档案；
签名人必须属于该合同甲乙双方。

### v0.2 档案级签名与直接复用

相对方档案可以保存一份长期签名：档案页「扫码采集」走同一个 `/sign/<token>`
手机页，提交后写入 `signature_assets`（`party_id` 有值、`contract_id` 为空），
并把 `parties.signature_asset_id` 指向该记录。

合同需要时，签名对话框会显示「使用租户档案签名」按钮（仅当当前签名人档案已有
档案签名），点击后直接套印，无需租户再次扫码或逐份确认：

| 方法 | 路径 | 访问范围 | 说明 |
| --- | --- | --- | --- |
| POST | `/app/api/v1/contracts/{id}/signature-reuse` | 仅回环 | 直接使用档案签名套印到合同，请求体 `{"party_id": "pty_xxx"}` |
| POST | `/app/api/v1/parties/{id}/signature-sessions` | 仅回环 | 为档案采集长期签名 |
| GET | `/app/api/v1/parties/{id}/signature` | 仅回环 | 读取档案签名 PNG（校验摘要） |

复用的语义（v0.2 D3=B）：不复制图片文件，只新增一条 `signature_assets` 记录，
指向同一不可变 PNG（同一 SHA-256），`source=archive`，并在变更记录里记一条
「复用档案签名（来源：档案复用，直接套印）」。档案复用**不绑定合同内容版本**：
`content_sha256` 留空，合同内容随后修改不会让该签名过期，也无需再次确认；同一
合同 / 档案 / 签名资产重复调用是幂等的。签名来源（档案复用 / 现场手写）写入
生成记录的签名快照与变更记录。

档案签名更换只影响之后的复用：已关联到合同的签名记录继续指向旧资产，已生成的
PDF 也不会被改写。签名只表示手写笔迹已保存在本机；签署事实、授权范围由出租方
用户自行确认，本工具不承诺法律效力。

扫码手写（下方端点与流程）保留为备选路径：需要现场签署时仍可生成一次性二维码
让签名人当场手写，该签名按合同内容版本校验。

## 安全边界

- **仅局域网**：`/sign/*` 在路由守卫中检查来源地址，只接受回环、RFC1918 /
  IPv6 ULA、链路本地地址；公网来源一律 `403 lan_only`，即使误将端口映射到
  公网也无法打开签名页。
- **默认绑定**：服务默认监听 `0.0.0.0:8765`，供同一局域网手机访问；
  `--loopback` / `allow_lan=false` 时签名会话直接拒绝创建，并提示改回局域网模式。
- **一次性令牌**：256 位随机 token 只出现在二维码 URL 中，不写入数据库、
  不出现在应用接口响应里；提交时原子占用，成功后立即作废。
- **短时有效**：默认 10 分钟（`config.json` 的 `signature.ttl_minutes`，
  允许 1–120 分钟）；过期后页面显示「链接已失效」。
- **不扩大 B2 写接口**：`/app/api/v1/*` 仍只允许回环 + `X-Tengshou-App` +
  Origin 校验；手机侧只有独立的 `/sign/*` 令牌端点。
- **跨站防护**：提交必须 `Content-Type: application/json` 且带
  `X-Tengshou-Sign: 1` 请求头，强制浏览器预检；服务不返回 CORS 头，
  其它网页无法代提交。签名页 CSP 禁止外部脚本 / 资源与表单提交。
- **图片校验**：只接受 PNG；限制尺寸与像素数；服务端把图片归一化为
  透明底黑墨、裁剪空白边距；笔画太淡 / 太少时拒绝并允许重写。

## 签名数据与内容版本

`signature_assets` 从 B5 起增加：

| 列 | 说明 |
| --- | --- |
| `contract_id` | 采集时关联的合同 |
| `content_sha256` | 采集时合同内容版本（稳定字段 + 动态数据的 SHA-256）；档案复用记录留空 |
| `source` | 采集渠道：现场扫码手写 `lan_qr`，档案复用 `archive` |
| `session_id` | 签名会话标识，便于审计 |

内容版本摘要不包含时间戳，合同内容不变时保持稳定；修改任何字段都会改变摘要，
因此可以判断现场手写签名对应的是哪一版合同内容。档案复用记录不写内容版本，
按「直接套印」处理（v0.2 D3=B）。接口：

- `GET /api/v1/signature-assets?contract_id=<合同 id>`：按合同查签名（需令牌）；
- `GET /api/v1/contracts/{id}`：合同详情中的 `signatures` 同时包含
  该合同的签名和旧版（未关联合同）的相对方签名。

## 失败处理

| 场景 | 表现 |
| --- | --- |
| 手机打不开链接 | 二维码弹窗同时显示 URL，可手动输入；多网卡时可切换地址 |
| 链接过期 | 手机显示「链接已失效」；电脑轮询显示「二维码已过期，请重新生成」 |
| 重复提交 | 第二次提交返回 409「这份签名已经提交过了」，不会产生第二条记录 |
| 断网 / 响应丢失 | 手机提示「网络中断，签名没有提交成功」并可重试；若服务端已保存，重试得到「已提交」，不会重复落库 |
| 签名过淡 / 太小 | 422 提示「签名太淡 / 太小」，令牌不消耗，可以清除后重写再交 |
| 磁盘或数据库写入失败 | 服务端回滚临时文件并释放令牌，手机可重试；不会留下半条记录 |
| 关闭电脑弹窗 | 未完成的会话立即取消，二维码随即失效 |

## 验证

自动测试：

```bash
go vet ./...
go test -count=1 ./...
```

覆盖点：

- `internal/sign`：会话一次性 / 过期 / 取消 / 并发占用；PNG 透明底黑墨归一化、
  裁剪、白底转换、过淡 / 过小 / 非 PNG 拒绝。
- `internal/service`：签名落盘、合同与内容版本关联、合同详情与按合同查询、
  变更记录、合同不存在时拒绝。
- `internal/httpapi`：完整「创建会话 → 局域网打开签名页 → 提交 → 电脑收到」、
  公网来源 403、令牌复用 409、过期 410、太淡 422 后可重试、回环模式拒绝创建、
  QR PNG、取消会话。
- `internal/pdfdoc` / `internal/demo`：`signaturePath` 模板占位与含签名 PDF。

手工验证：

```bash
go run ./cmd/tengshou-cli serve --data-dir ./data
# 电脑打开 http://127.0.0.1:8765/app/ → 合同行「扫码签名」
# 手机连同一 Wi-Fi 扫码 → 手写 → 提交
# 电脑弹窗显示「签名已保存」；数据目录 assets/signatures/ 出现 PNG
go run ./cmd/tengshou-cli render --data-dir ./data --contract <合同 id>
# PDF 中签名位于模板指定位置，可直接打印
```
