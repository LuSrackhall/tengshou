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
		DatabaseVersion: dbVersion,
		ReadOnly:        true,
		Auth:            "Authorization: Bearer <token> (token lives in config.json)",
		Notes: []string{
			"所有数据来自本机 SQLite，不经过任何外部服务器。",
			"接口只接受 GET；写操作由桌面应用完成。",
			"字段扩展数据放在 data 对象中，键名由模板 / 使用者决定。",
			"合同详情中的模板不包含 HTML 正文；模板正文由 /api/v1/templates/{id} 返回。",
		},
		Resources: []resourceDoc{
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
					{"title", "string", "合同名称"},
					{"template_id", "string", "使用的模板 id"},
					{"party_a_id", "string", "甲方 id"},
					{"party_b_id", "string", "乙方 id"},
					{"status", "string", "draft / active / archived"},
					{"signed_on", "string", "签署日期 YYYY-MM-DD"},
					{"valid_from", "string", "生效日期"},
					{"valid_to", "string", "到期日期"},
					{"data", "object", "合同字段数据（租金、地址等）"},
					{"created_at", "string", "RFC3339 创建时间"},
					{"updated_at", "string", "RFC3339 更新时间"},
				},
			},
			{
				Name:        "contract_detail",
				Path:        "/api/v1/contracts/{id}",
				Description: "合同及其相对方、签名、模板版本与输出记录。",
				Fields: []fieldDoc{
					{"contract", "object", "contract 资源"},
					{"party_a", "object", "party 资源（可为空）"},
					{"party_b", "object", "party 资源（可为空）"},
					{"template", "object", "模板元数据（不含 HTML）"},
					{"signatures", "array", "signature_asset 列表"},
					{"documents", "array", "output_document 列表"},
				},
			},
			{
				Name:        "template",
				Path:        "/api/v1/templates, /api/v1/templates/{id}",
				Description: "HTML 合同模板及版本。列表不含 HTML 正文。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"name", "string", "模板名称"},
					{"version", "integer", "版本号"},
					{"html", "string", "模板 HTML（仅详情返回）"},
					{"variables", "array", "模板引用的 data 键"},
					{"is_active", "boolean", "是否启用"},
					{"created_at", "string", "RFC3339 创建时间"},
					{"updated_at", "string", "RFC3339 更新时间"},
				},
			},
			{
				Name:        "signature_asset",
				Path:        "/api/v1/signature-assets",
				Description: "留存的手写签名图片元数据（不返回图片内容）。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"party_id", "string", "签名所属相对方"},
					{"signer_name", "string", "签名人姓名"},
					{"asset_path", "string", "相对于数据目录的图片路径"},
					{"sha256", "string", "图片摘要，用于校验留存"},
					{"width_px", "integer", "像素宽"},
					{"height_px", "integer", "像素高"},
					{"captured_at", "string", "采集时间"},
					{"created_at", "string", "RFC3339 创建时间"},
				},
			},
			{
				Name:        "output_document",
				Path:        "/api/v1/documents?contract_id={id}",
				Description: "已生成的 PDF 记录。",
				Fields: []fieldDoc{
					{"id", "string", "稳定标识"},
					{"contract_id", "string", "所属合同"},
					{"template_id", "string", "使用的模板"},
					{"template_version", "integer", "模板版本"},
					{"file_path", "string", "相对于数据目录的 PDF 路径"},
					{"sha256", "string", "PDF 摘要"},
					{"page_count", "integer", "页数"},
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
					{"documents", "integer", "输出 PDF 数量"},
				},
			},
		},
	})
}
