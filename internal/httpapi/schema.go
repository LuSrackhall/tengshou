package httpapi

import "net/http"

// dataSchema documents the stable read-only data contract. External agents
// (the B6 MCP connector) rely on these names; bump SchemaName only for
// breaking changes.
const SchemaName = "tengshou.data.v1"

type fieldDoc struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type resourceDoc struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Description string     `json:"description"`
	Fields      []fieldDoc `json:"fields"`
}

type schemaDoc struct {
	Schema          string        `json:"schema"`
	AppVersion      string        `json:"app_version"`
	DatabaseVersion int           `json:"database_version"`
	ReadOnly        bool          `json:"read_only"`
	Auth            string        `json:"auth"`
	Notes           []string      `json:"notes"`
	Resources       []resourceDoc `json:"resources"`
}

func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	dbVersion := 0
	if s.opts.Reader != nil {
		dbVersion, _ = s.opts.Reader.SchemaVersion(r.Context())
	}
	writeJSON(w, http.StatusOK, schemaDoc{
		Schema:          SchemaName,
		AppVersion:      s.version(),
		DatabaseVersion: dbVersion,
		ReadOnly:        true,
		Auth:            "Authorization: Bearer <token> (token lives in config.json)",
		Notes: []string{
			"所有数据来自本机 SQLite，不经过任何外部服务器。",
			"接口只接受 GET；写操作由桌面应用完成。",
			"字段扩展数据放在 data 对象中，键名由字段定义（/api/v1/fields）决定。",
			"合同编号 contract_no 稳定唯一，创建后不可修改。",
			"合同详情中的模板不包含 HTML 正文；模板正文由 /api/v1/templates/{id} 返回。",
			"PDF 模板（kind=pdf）由用户 Word 文档转换而来；placements 是从页面左上角起算的 PDF 点坐标（已应用页面旋转），生成 PDF 前必须先预览确认。",
			"签名资产通过 contract_id 与 content_sha256 关联合同和采集时的内容版本。",
			"output_documents 同时记录成功与失败尝试：谁、何时、用哪个模板版本、合同内容版本、用了哪些签名；失败记录保留错误信息以便重试。",
			"合同内容变化后不能静默复用旧签名重出 PDF；需重新签名或显式确认「保留旧签名重出」。",
			"局域网签名页使用一次性短时令牌，仅接受局域网来源；令牌不写入数据库。",
		},
		Resources: []resourceDoc{
			{
				Name:        "field_definition",
				Path:        "/api/v1/fields?entity=contract|party",
				Description: "可配置字段定义。用户新增字段后，data 中对应键立即可用。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"entity", "string", "contract 或 party"},
					{"key", "string", "data 中的键；创建后不可变"},
					{"label", "string", "显示名称"},
					{"type", "string", "text/longtext/number/money/date/select/bool/image；内置字段另有 party/status"},
					{"required", "boolean", "是否必填"},
					{"default_value", "string", "默认值"},
					{"options", "array", "单选字段的选项"},
					{"in_ledger", "boolean", "是否出现在应用内台账表格（Excel 列始终由当前字段定义生成）"},
					{"in_template", "boolean", "是否用于模板占位符"},
					{"built_in", "boolean", "是否内置字段"},
					{"archived", "boolean", "是否已归档（数据保留）"},
					{"sort_order", "integer", "显示顺序"},
					{"note", "string", "说明"},
				},
			},
			{
				Name:        "party",
				Path:        "/api/v1/parties, /api/v1/parties/{id}",
				Description: "相对方档案（个人或组织）。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"kind", "string", "person 或 organization"},
					{"name", "string", "姓名或单位名称"},
					{"id_number", "string", "身份证号 / 统一社会信用代码"},
					{"phone", "string", "联系电话"},
					{"address", "string", "联系地址"},
					{"note", "string", "备注"},
					{"data", "object", "扩展字段（B2 动态字段）"},
					{"created_at", "string", "RFC3339 创建时间"},
					{"updated_at", "string", "RFC3339 更新时间"},
				},
			},
			{
				Name:        "contract",
				Path:        "/api/v1/contracts, /api/v1/contracts/{id}",
				Description: "合同台账记录；{id} 返回合同详情。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"contract_no", "string", "稳定唯一的合同编号（创建后不可修改）"},
					{"year", "integer", "合同年度，用于年度视图与跨年查询"},
					{"title", "string", "合同名称"},
					{"type", "string", "合同类型"},
					{"template_id", "string", "使用的模板 id"},
					{"party_a_id", "string", "甲方 id"},
					{"party_b_id", "string", "乙方 id"},
					{"asset_id", "string", "租赁房产 id（历史合同可为空）"},
					{"status", "string", "draft / active / archived"},
					{"signed_on", "string", "签署日期 YYYY-MM-DD"},
					{"valid_from", "string", "生效日期"},
					{"valid_to", "string", "到期日期"},
					{"lease_start", "string", "租赁起租日 YYYY-MM-DD"},
					{"lease_end", "string", "租赁到期日 YYYY-MM-DD"},
					{"amount", "number", "金额（元）"},
					{"data", "object", "动态字段数据（由字段定义驱动）"},
					{"created_at", "string", "RFC3339 创建时间"},
					{"updated_at", "string", "RFC3339 更新时间"},
				},
			},
			{
				Name:        "contract_detail",
				Path:        "/api/v1/contracts/{id}",
				Description: "合同及其相对方、签名、公章关联、模板版本与输出记录。",
				Fields: []fieldDoc{
					{"contract", "object", "contract 资源"},
					{"party_a", "object", "party 资源（可为空）"},
					{"party_b", "object", "party 资源（可为空）"},
					{"asset", "object", "asset 资源（历史合同可为空）"},
					{"template", "object", "模板元数据（不含 HTML）"},
					{"signatures", "array", "signature_asset 列表"},
					{"seals", "array", "显式关联到合同的公章图片元数据（asset_path / sha256 / 尺寸）"},
					{"documents", "array", "output_document 列表"},
				},
			},
			{
				Name:        "template",
				Path:        "/api/v1/templates, /api/v1/templates/{id}",
				Description: "合同模板版本。kind=html 为内置排版模板；kind=pdf 为用户导入的 Word/PDF 模板（含字段、签名、公章位置映射）。列表不含 HTML 正文。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识（每个版本一行）"},
					{"name", "string", "模板名称；同名不同版本属于同一模板系列"},
					{"version", "integer", "版本号；保存标定或重新导入都会递增"},
					{"kind", "string", "html 或 pdf"},
					{"html", "string", "HTML 模板正文（仅详情返回；PDF 模板为空）"},
					{"variables", "array", "HTML 模板引用的 data 键"},
					{"is_active", "boolean", "是否为该模板系列的最新版本"},
					{"source_path", "string", "原始 .docx / .pdf 资产路径（相对数据目录）"},
					{"pdf_path", "string", "转换后的模板 PDF 资产路径"},
					{"page_count", "integer", "模板 PDF 页数"},
					{"converter", "string", "word / libreoffice / pdf（PDF 直接导入）"},
					{"source_sha256", "string", "原始文件摘要"},
					{"pdf_sha256", "string", "模板 PDF 摘要"},
					{"placements", "array", "PDF 模板标定位置：{id,page,x,y,w,h,type,field,party,seal_mode,seal_id,font_size,align}"},
					{"created_at", "string", "RFC3339 创建时间"},
					{"updated_at", "string", "RFC3339 更新时间"},
				},
			},
			{
				Name:        "signature_asset",
				Path:        "/api/v1/signature-assets?contract_id={id}",
				Description: "留存的手写签名图片元数据（不返回图片内容），关联合同与采集时的内容版本。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"party_id", "string", "签名所属相对方"},
					{"contract_id", "string", "采集时关联的合同（B5 起）"},
					{"signer_name", "string", "签名人姓名"},
					{"asset_path", "string", "相对于数据目录的图片路径"},
					{"sha256", "string", "图片摘要，用于校验留存"},
					{"content_sha256", "string", "采集时合同内容版本摘要"},
					{"source", "string", "采集渠道，例如 lan_qr"},
					{"session_id", "string", "签名会话标识"},
					{"width_px", "integer", "像素宽"},
					{"height_px", "integer", "像素高"},
					{"captured_at", "string", "采集时间"},
					{"created_at", "string", "RFC3339 创建时间"},
				},
			},
			{
				Name:        "output_document",
				Path:        "/api/v1/documents?contract_id=&template_id=&batch_id=&status=",
				Description: "PDF 生成记录（成功与失败尝试），可按合同、模板版本、批次、状态查询。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"contract_id", "string", "所属合同"},
					{"template_id", "string", "使用的模板"},
					{"template_version", "integer", "模板版本"},
					{"file_path", "string", "相对于数据目录的 PDF 路径；失败记录为空"},
					{"sha256", "string", "PDF 摘要；失败记录为空"},
					{"page_count", "integer", "页数"},
					{"content_sha256", "string", "生成时的合同内容版本摘要"},
					{"signatures", "array", "套印使用的签名快照（签名、签名人、图片摘要与内容版本）"},
					{"operator", "string", "操作人（本机用户或 tengshou-mcp）"},
					{"kind", "string", "initial（初次生成）或 regenerate（整改重出）"},
					{"status", "string", "success 或 failed"},
					{"error_message", "string", "失败原因"},
					{"batch_id", "string", "批量生成批次的标识"},
					{"source_document_id", "string", "整改重出时的来源文档"},
					{"created_at", "string", "生成时间"},
				},
			},
			{
				Name:        "stats",
				Path:        "/api/v1/stats",
				Description: "各类记录数量。",
				Fields: []fieldDoc{
					{"parties", "integer", "相对方数量"},
					{"contracts", "integer", "合同数量"},
					{"templates", "integer", "模板版本数量"},
					{"signatures", "integer", "签名资产数量"},
					{"seals", "integer", "公章图片数量"},
					{"documents", "integer", "输出 PDF 数量"},
				},
			},
		},
	})
}
