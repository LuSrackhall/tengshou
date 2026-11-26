package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Tool names. Reads use the stable /api/v1 surface; creates use the loopback
// app surface so they pass through the same service validation as the UI.
const (
	toolStatus              = "tengshou_status"
	toolGetDataSchema       = "tengshou_get_data_schema"
	toolListFields          = "tengshou_list_fields"
	toolListParties         = "tengshou_list_parties"
	toolGetParty            = "tengshou_get_party"
	toolListContracts       = "tengshou_list_contracts"
	toolGetContract         = "tengshou_get_contract"
	toolListSignatureAssets = "tengshou_list_signature_assets"
	toolListTemplates       = "tengshou_list_templates"
	toolGetTemplate         = "tengshou_get_template"
	toolListDocuments       = "tengshou_list_documents"
	toolPreflightGeneration = "tengshou_preflight_generation"
	toolCreateField         = "tengshou_create_field"
	toolCreateParty         = "tengshou_create_party"
	toolCreateContractDraft = "tengshou_create_contract_draft"
	toolGenerateContract    = "tengshou_generate_contract"
)

type toolDefinition struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		readTool(toolStatus, "腾手服务状态",
			"检查腾手本地服务是否可用，返回版本、数据库 schema 版本、记录统计、年度计数、当前操作者与 MCP 写开关。所有工具都要求腾手桌面应用正在运行。",
			objectSchema(map[string]any{}, nil)),
		readTool(toolGetDataSchema, "读取数据契约",
			"读取只读数据接口的机器可读 schema 与字段说明（tengshou.data.v1），包括每个资源的类型与含义。该 schema 描述 /api/v1 只读面；写工具另走仅限本机的应用写入面。",
			objectSchema(map[string]any{}, nil)),
		readTool(toolListFields, "读取字段定义",
			"读取可配置字段定义（合同 / 档案）。合同与档案的 data 对象中的键由这些定义决定；返回类型、必填、选项、默认值、是否进台账 / 模板与排序。",
			objectSchema(map[string]any{
				"entity": enumProp("contract", "party", "实体；省略时同时返回合同与档案两组字段"),
			}, nil)),
		readTool(toolListParties, "查询相对方档案",
			"按姓名 / 证件号 / 电话搜索相对方档案，返回分页列表与动态字段 data。",
			objectSchema(map[string]any{
				"q":      stringProp("关键词（姓名 / 证件号 / 电话）"),
				"limit":  intProp("每页条数，1～500，默认 50"),
				"offset": intProp("偏移量，默认 0"),
			}, nil)),
		readTool(toolGetParty, "读取相对方档案",
			"读取单个相对方档案，包含类型、证件号、电话、地址、备注与动态字段 data。",
			objectSchema(map[string]any{
				"id": stringProp("相对方 id"),
			}, []string{"id"})),
		readTool(toolListContracts, "查询合同台账",
			"查询合同列表，可按年度、状态与关键词筛选并分页。合同编号 contract_no 稳定唯一。",
			objectSchema(map[string]any{
				"year":   intProp("合同年度，例如 2026；省略即跨年查询"),
				"status": enumProp("draft", "active", "terminated", "archived", "合同状态；省略为全部"),
				"q":      stringProp("关键词（合同编号 / 名称 / 相对方名称）"),
				"limit":  intProp("每页条数，1～500，默认 50"),
				"offset": intProp("偏移量，默认 0"),
			}, nil)),
		readTool(toolGetContract, "读取合同详情",
			"读取合同详情：合同字段与动态 data、双方档案、模板版本、签名资产（签名状态与 content_sha256）、公章关联、已生成 PDF 记录。",
			objectSchema(map[string]any{
				"id": stringProp("合同 id"),
			}, []string{"id"})),
		readTool(toolListSignatureAssets, "查询签名资产",
			"查询签名图片元数据（不返回图片内容）。传入 contract_id 可查看某份合同的签名状态；content_sha256 表示签名针对的合同内容版本。",
			objectSchema(map[string]any{
				"contract_id": stringProp("合同 id；省略返回全部签名资产"),
				"q":           stringProp("关键词（签名人 / 相对方 id）"),
				"limit":       intProp("每页条数，1～500，默认 50"),
				"offset":      intProp("偏移量，默认 0"),
			}, nil)),
		readTool(toolListTemplates, "查询模板版本",
			"查询合同模板列表及版本号（HTML / PDF 两类；列表不含 HTML 正文）。",
			objectSchema(map[string]any{
				"q":      stringProp("模板名称关键词"),
				"limit":  intProp("每页条数，1～500，默认 50"),
				"offset": intProp("偏移量，默认 0"),
			}, nil)),
		readTool(toolGetTemplate, "读取模板详情",
			"读取模板详情：kind、变量列表；HTML 模板含 HTML 正文，PDF 模板含 pdf_path、page_count 与 placements。",
			objectSchema(map[string]any{
				"id": stringProp("模板 id"),
			}, []string{"id"})),
		readTool(toolListDocuments, "查询已生成 PDF 文档",
			"查询 PDF 生成记录（成功与失败），可按合同与状态筛选并分页。每条记录含合同编号、模板名称与版本、生成类型、状态、时间与下载路径 /app/api/v1/documents/{id}/file；失败记录没有可下载文件。",
			objectSchema(map[string]any{
				"contract_id": stringProp("合同 id；省略返回全部合同的文档"),
				"status":      enumProp("success", "failed", "生成状态；省略为全部"),
				"limit":       intProp("每页条数，1～500，默认 50"),
				"offset":      intProp("偏移量，默认 0"),
			}, nil)),
		readTool(toolPreflightGeneration, "预检合同生成",
			"生成 PDF 前的只读检查：返回是否可生成（ready）、阻断项 issues、警告 warnings、预览摘要 preview_sha256、将使用的签名与已生成数量。阻断项需要用户在桌面应用内处理，本工具不会产生文件或记录。",
			objectSchema(map[string]any{
				"template_id": stringProp("PDF 模板 id"),
				"contract_id": stringProp("合同 id"),
			}, []string{"template_id", "contract_id"})),
		writeTool(toolCreateField, "新建动态字段",
			"新建合同 / 档案的动态字段（无需重启）。字段键创建后不可修改；新字段立即用于表单、Excel 与模板占位符。写操作以 actor=tengshou-mcp 记入变更记录。",
			objectSchema(map[string]any{
				"entity":        enumProp("contract", "party", "字段所属实体"),
				"key":           stringProp("字段键：小写字母开头，可含数字与下划线，例如 area_sqm"),
				"label":         stringProp("显示名称，例如 建筑面积"),
				"type":          enumProp("text", "longtext", "number", "money", "date", "select", "bool", "image", "字段类型"),
				"required":      boolProp("是否必填"),
				"default_value": stringProp("默认值"),
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "单选字段的选项（type=select 时至少一个）",
				},
				"in_ledger":   boolProp("是否出现在台账与 Excel（默认 true）"),
				"in_template": boolProp("是否用于模板占位符（默认 true）"),
				"group":       stringProp("合同表单分组，例如 租期 / 租金与押金；留空归入「其他约定」"),
				"note":        stringProp("说明"),
			}, []string{"entity", "key", "label", "type"})),
		writeTool(toolCreateParty, "新建相对方档案",
			"新建相对方档案（个人或单位），可同时写入动态字段 data。新建档案必须提供证件号（身份证号 / 统一社会信用代码）；会套用字段默认值；同名或同证件号档案只提醒不阻断。写操作以 actor=tengshou-mcp 记入变更记录。",
			objectSchema(map[string]any{
				"name":      stringProp("姓名或单位名称"),
				"kind":      enumProp("person", "organization", "个人或单位，默认 person"),
				"id_number": stringProp("身份证号 / 统一社会信用代码（必填）"),
				"phone":     stringProp("联系电话"),
				"address":   stringProp("联系地址"),
				"note":      stringProp("备注"),
				"data":      map[string]any{"type": "object", "additionalProperties": true, "description": "动态字段值，键必须已在字段定义中"},
			}, []string{"name", "id_number"})),
		writeTool(toolCreateContractDraft, "新建合同草稿",
			"新建合同草稿（状态固定为 draft，后续由用户在桌面应用内确认）。编号省略时自动分配 HT-YYYY-NNNN；相对方可用 party_b_id 指定档案，或用 party_b 按名称自动建档。写操作以 actor=tengshou-mcp 记入变更记录。",
			objectSchema(map[string]any{
				"title":       stringProp("合同名称"),
				"year":        intProp("合同年度，例如 2026；省略时按签订日期或当前年度"),
				"type":        stringProp("合同类型"),
				"party_b_id":  stringProp("相对方（乙方）档案 id"),
				"party_b":     stringProp("相对方（乙方）名称；档案不存在时自动建档"),
				"asset_id":    stringProp("租赁房产档案 id（可为空；历史合同兼容）"),
				"lease_start": dateProp("租赁起租日 YYYY-MM-DD"),
				"lease_end":   dateProp("租赁到期日 YYYY-MM-DD"),
				"amount":      numberProp("金额（元），非负"),
				"template_id": stringProp("模板 id"),
				"signed_on":   dateProp("签订日期 YYYY-MM-DD"),
				"valid_from":  dateProp("生效日期 YYYY-MM-DD"),
				"valid_to":    dateProp("到期日期 YYYY-MM-DD"),
				"contract_no": stringProp("自定义合同编号；省略时自动分配"),
				"data":        map[string]any{"type": "object", "additionalProperties": true, "description": "动态字段值，键必须已在字段定义中"},
			}, []string{"title"})),
		writeTool(toolGenerateContract, "生成合同 PDF",
			"为合同套用 PDF 模板生成合同文件。内部先执行预检：未通过时返回 isError 并逐条列出阻断项，不写记录、不产生文件；通过后用预检的预览摘要生成，返回 {item, warnings}。生成后合同模板版本可能更新为所选版本。不提供签名、激活、删除或修改工具；写开关关闭时返回 403 mcp_write_disabled。",
			objectSchema(map[string]any{
				"template_id":           stringProp("PDF 模板 id"),
				"contract_id":           stringProp("合同 id"),
				"allow_stale_signature": boolProp("确认复用合同内容修改前采集的旧签名（默认 false；仅在某份内容未实质变化、用户明确知情时使用）"),
				"kind":                  enumProp("initial", "regenerate", "生成类型；省略时按合同是否已有成功 PDF 自动判断"),
			}, []string{"template_id", "contract_id"})),
	}
}

func readTool(name, title, description string, schema map[string]any) toolDefinition {
	return toolDefinition{
		Name:        name,
		Title:       title,
		Description: description,
		InputSchema: schema,
		Annotations: map[string]any{
			"readOnlyHint":  true,
			"openWorldHint": false,
		},
	}
}

func writeTool(name, title, description string, schema map[string]any) toolDefinition {
	return toolDefinition{
		Name:        name,
		Title:       title,
		Description: description,
		InputSchema: schema,
		Annotations: map[string]any{
			"readOnlyHint":    false,
			"destructiveHint": false,
			"idempotentHint":  false,
			"openWorldHint":   false,
		},
	}
}

func objectSchema(props map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func dateProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description, "format": "date"}
}

func intProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func numberProp(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func enumProp(values ...string) map[string]any {
	description := values[len(values)-1]
	return map[string]any{
		"type":        "string",
		"enum":        values[:len(values)-1],
		"description": description,
	}
}

func knownTool(name string) bool {
	for _, tool := range toolDefinitions() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// isWriteTool reports whether name is a write tool. Write tools are exactly the
// definitions annotated readOnlyHint=false.
func isWriteTool(name string) bool {
	for _, tool := range toolDefinitions() {
		if tool.Name != name {
			continue
		}
		readOnly, _ := tool.Annotations["readOnlyHint"].(bool)
		return !readOnly
	}
	return false
}

func (s *Server) executeTool(ctx context.Context, name string, args json.RawMessage) toolResult {
	// Fail fast when the desktop app has closed the write switch. The HTTP
	// guard also enforces this (and stays authoritative); checking here gives
	// the agent a clear error before any network round-trip.
	if isWriteTool(name) && s.writeEnabled != nil && !s.writeEnabled() {
		return errorResult(fmt.Errorf(
			"mcp_write_disabled：MCP 写工具已关闭（mcp.write_enabled=false）；可在腾手桌面应用「设置」页开启"))
	}
	var (
		raw json.RawMessage
		err error
	)
	switch name {
	case toolStatus:
		raw, err = s.toolStatus(ctx)
	case toolGetDataSchema:
		raw, err = s.api.Get(ctx, "/api/v1/schema", nil)
	case toolListFields:
		raw, err = s.toolListFields(ctx, args)
	case toolListParties:
		raw, err = s.toolListParties(ctx, args)
	case toolGetParty:
		raw, err = s.toolGetByID(ctx, args, "/api/v1/parties/")
	case toolListContracts:
		raw, err = s.toolListContracts(ctx, args)
	case toolGetContract:
		raw, err = s.toolGetByID(ctx, args, "/api/v1/contracts/")
	case toolListSignatureAssets:
		raw, err = s.toolListSignatureAssets(ctx, args)
	case toolListTemplates:
		raw, err = s.toolListTemplates(ctx, args)
	case toolGetTemplate:
		raw, err = s.toolGetByID(ctx, args, "/api/v1/templates/")
	case toolListDocuments:
		raw, err = s.toolListDocuments(ctx, args)
	case toolPreflightGeneration:
		raw, err = s.toolPreflightGeneration(ctx, args)
	case toolCreateField:
		raw, err = s.toolCreateField(ctx, args)
	case toolCreateParty:
		raw, err = s.toolCreateParty(ctx, args)
	case toolCreateContractDraft:
		raw, err = s.toolCreateContractDraft(ctx, args)
	case toolGenerateContract:
		raw, err = s.toolGenerateContract(ctx, args)
	default:
		err = fmt.Errorf("未知工具 %q", name)
	}
	if err != nil {
		return errorResult(err)
	}
	return jsonResult(raw)
}

func (s *Server) toolStatus(ctx context.Context) (json.RawMessage, error) {
	raw, err := s.api.Get(ctx, "/app/api/v1/bootstrap", nil)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析服务状态失败：%w", err)
	}
	base, err := s.api.BaseURL(ctx)
	if err != nil {
		return nil, err
	}
	payload["api_base_url"] = base
	payload["mcp_actor"] = "tengshou-mcp"
	return json.Marshal(payload)
}

type listFieldsArgs struct {
	Entity string `json:"entity"`
}

func (s *Server) toolListFields(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in listFieldsArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	q := url.Values{}
	if entity := strings.TrimSpace(in.Entity); entity != "" {
		if entity != "contract" && entity != "party" {
			return nil, fmt.Errorf("entity 只能是 contract 或 party")
		}
		q.Set("entity", entity)
	}
	return s.api.Get(ctx, "/api/v1/fields", q)
}

type listPartiesArgs struct {
	Query  string `json:"q"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (s *Server) toolListParties(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in listPartiesArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	q, err := listQuery(in.Query, in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	return s.api.Get(ctx, "/api/v1/parties", q)
}

type listContractsArgs struct {
	Year   int    `json:"year"`
	Status string `json:"status"`
	Query  string `json:"q"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (s *Server) toolListContracts(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in listContractsArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	q, err := listQuery(in.Query, in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	if in.Year < 0 {
		return nil, fmt.Errorf("year 不能为负数")
	}
	if in.Year > 0 {
		q.Set("year", strconv.Itoa(in.Year))
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		switch status {
		case "draft", "active", "terminated", "archived":
			q.Set("status", status)
		default:
			return nil, fmt.Errorf("status 只能是 draft / active / terminated / archived")
		}
	}
	return s.api.Get(ctx, "/api/v1/contracts", q)
}

type listSignatureAssetsArgs struct {
	ContractID string `json:"contract_id"`
	Query      string `json:"q"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

func (s *Server) toolListSignatureAssets(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in listSignatureAssetsArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	q, err := listQuery(in.Query, in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	if id := strings.TrimSpace(in.ContractID); id != "" {
		q.Set("contract_id", id)
	}
	return s.api.Get(ctx, "/api/v1/signature-assets", q)
}

type listTemplatesArgs struct {
	Query  string `json:"q"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (s *Server) toolListTemplates(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in listTemplatesArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	q, err := listQuery(in.Query, in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	return s.api.Get(ctx, "/api/v1/templates", q)
}

type idArgs struct {
	ID string `json:"id"`
}

func (s *Server) toolGetByID(ctx context.Context, args json.RawMessage, prefix string) (json.RawMessage, error) {
	var in idArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, fmt.Errorf("id 不能为空")
	}
	return s.api.Get(ctx, prefix+url.PathEscape(id), nil)
}

type createFieldArgs struct {
	Entity       string   `json:"entity"`
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	Required     *bool    `json:"required,omitempty"`
	DefaultValue *string  `json:"default_value,omitempty"`
	Options      []string `json:"options,omitempty"`
	InLedger     *bool    `json:"in_ledger,omitempty"`
	InTemplate   *bool    `json:"in_template,omitempty"`
	Group        *string  `json:"group,omitempty"`
	Note         *string  `json:"note,omitempty"`
}

func (s *Server) toolCreateField(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in createFieldArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	return s.api.Post(ctx, "/app/api/v1/fields", in)
}

type createPartyArgs struct {
	Kind     string         `json:"kind,omitempty"`
	Name     string         `json:"name"`
	IDNumber string         `json:"id_number,omitempty"`
	Phone    string         `json:"phone,omitempty"`
	Address  string         `json:"address,omitempty"`
	Note     string         `json:"note,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

func (s *Server) toolCreateParty(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in createPartyArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	return s.api.Post(ctx, "/app/api/v1/parties", in)
}

type createContractDraftArgs struct {
	Title      string         `json:"title"`
	Year       int            `json:"year,omitempty"`
	Type       string         `json:"type,omitempty"`
	PartyBID   string         `json:"party_b_id,omitempty"`
	PartyB     string         `json:"party_b,omitempty"`
	AssetID    string         `json:"asset_id,omitempty"`
	LeaseStart string         `json:"lease_start,omitempty"`
	LeaseEnd   string         `json:"lease_end,omitempty"`
	Amount     float64        `json:"amount,omitempty"`
	TemplateID string         `json:"template_id,omitempty"`
	SignedOn   string         `json:"signed_on,omitempty"`
	ValidFrom  string         `json:"valid_from,omitempty"`
	ValidTo    string         `json:"valid_to,omitempty"`
	ContractNo string         `json:"contract_no,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

func (s *Server) toolCreateContractDraft(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in createContractDraftArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"title":       in.Title,
		"year":        in.Year,
		"type":        in.Type,
		"party_b_id":  in.PartyBID,
		"party_b":     in.PartyB,
		"asset_id":    in.AssetID,
		"lease_start": in.LeaseStart,
		"lease_end":   in.LeaseEnd,
		"amount":      in.Amount,
		"template_id": in.TemplateID,
		"signed_on":   in.SignedOn,
		"valid_from":  in.ValidFrom,
		"valid_to":    in.ValidTo,
		"contract_no": in.ContractNo,
		"data":        in.Data,
		// Drafts only: activation stays a human decision in the desktop app.
		"status": "draft",
	}
	if strings.TrimSpace(in.PartyB) != "" && strings.TrimSpace(in.PartyBID) == "" {
		payload["create_party"] = true
	}
	return s.api.Post(ctx, "/app/api/v1/contracts", payload)
}

func listQuery(query string, limit, offset int) (url.Values, error) {
	if limit < 0 || limit > 500 {
		return nil, fmt.Errorf("limit 应为 0（默认）或 1～500")
	}
	if offset < 0 {
		return nil, fmt.Errorf("offset 不能为负数")
	}
	q := url.Values{}
	if query = strings.TrimSpace(query); query != "" {
		q.Set("q", query)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return q, nil
}

// decodeArgs rejects unknown keys so a misspelled write argument fails loudly
// instead of being silently ignored.
func decodeArgs(raw json.RawMessage, dst any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		trimmed = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("参数错误：%w", err)
	}
	return nil
}

func jsonResult(raw json.RawMessage) toolResult {
	text := strings.TrimSpace(string(raw))
	if pretty, err := prettyJSON(raw); err == nil {
		text = pretty
	}
	if text == "" {
		text = "{}"
	}
	return toolResult{Content: []toolContent{{Type: "text", Text: text}}}
}

func errorResult(err error) toolResult {
	text := err.Error()
	var apiErr *APIError
	if errors.As(err, &apiErr) && len(apiErr.Detail) > 0 {
		if pretty, perr := prettyJSON(apiErr.Detail); perr == nil {
			text = apiErr.Error() + "\n" + pretty
		}
	}
	return toolResult{
		Content: []toolContent{{Type: "text", Text: text}},
		IsError: true,
	}
}

func prettyJSON(raw []byte) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}
