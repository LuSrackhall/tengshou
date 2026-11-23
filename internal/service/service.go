// Package service is the writable application layer: field definitions,
// contract and party records, Excel import/export and the audit log.
//
// All writes from the desktop UI, the CLI demo and Excel import flow through
// this package so validation, contract numbering and change logging stay in
// one place. The external /api/v1 surface remains read-only.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LuSrackhall/tengshou/internal/backup"
	"github.com/LuSrackhall/tengshou/internal/doctopdf"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
	"github.com/tdewolff/canvas"
)

// Service owns the writable business rules. It is safe for concurrent use.
type Service struct {
	st      *store.Store
	rd      *store.Reader
	actor   string
	dataDir string
	now     func() time.Time
	mu      sync.Mutex // serializes writes that allocate contract numbers

	// converters turns imported Word documents into PDF (B3). Callers may
	// replace the chain, e.g. in tests.
	converters *doctopdf.Chain
	// backupMgr builds and stages full data backup packages (B9).
	backupMgr *backup.Manager
	// fontLoader lazily resolves the CJK font used for template overlays.
	// It is injected by the app layer so the font is shared with the HTML
	// generator.
	fontLoader func() (*canvas.FontFamily, error)
	fontOnce   sync.Once
	font       *canvas.FontFamily
	fontErr    error
}

// New constructs a Service. An empty actor falls back to the OS user name.
func New(st *store.Store, rd *store.Reader, actor string) *Service {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = OSUser()
	}
	return &Service{
		st:         st,
		rd:         rd,
		actor:      actor,
		now:        func() time.Time { return model.Now() },
		converters: doctopdf.DefaultChain(),
	}
}

// OSUser returns a best-effort local user name for the audit log.
func OSUser() string {
	if u, err := user.Current(); err == nil {
		if name := strings.TrimSpace(u.Username); name != "" {
			return name
		}
	}
	return "本机用户"
}

// Actor returns the name recorded in change-log entries.
func (s *Service) Actor() string { return s.actor }

// MCPActor is the change-log actor recorded for writes initiated through the
// B6 MCP connector. The HTTP layer sets it from the X-Tengshou-MCP header so
// agent writes are distinguishable from desktop-UI writes.
const MCPActor = "tengshou-mcp"

type actorContextKey struct{}

// WithActor overrides the change-log actor for operations carried by ctx. An
// empty actor leaves the service default (config operator / OS user) intact.
// This is used for MCP-originated writes; it does not change authorization.
func WithActor(ctx context.Context, actor string) context.Context {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// actorFor resolves the audit actor for one operation.
func (s *Service) actorFor(ctx context.Context) string {
	if ctx != nil {
		if actor, ok := ctx.Value(actorContextKey{}).(string); ok && actor != "" {
			return actor
		}
	}
	return s.actor
}

// SetDataDir configures the data directory used for binary assets such as
// captured signature images. It is called once at startup, before serving.
func (s *Service) SetDataDir(dir string) { s.dataDir = dir }

// SetBackupManager wires the B9 backup package manager. It is called once at
// startup, before serving.
func (s *Service) SetBackupManager(m *backup.Manager) { s.backupMgr = m }

// DataDir returns the configured data directory ("" when unset).
func (s *Service) DataDir() string { return s.dataDir }

// Now returns the service clock.
func (s *Service) Now() time.Time { return s.now() }

// Error is a business error with a machine-readable code. The HTTP layer maps
// codes to statuses; anything else becomes a 500.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func business(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// FieldError is one validation failure attached to a field.
type FieldError struct {
	Field   string `json:"field"`
	Label   string `json:"label"`
	Message string `json:"message"`
}

// ValidationErrors groups field errors. It is returned as-is by the service so
// HTTP handlers and Excel previews can report every problem in one pass.
type ValidationErrors struct {
	Items []FieldError `json:"items"`
}

func (v *ValidationErrors) Error() string {
	if v == nil || len(v.Items) == 0 {
		return "validation failed"
	}
	parts := make([]string, 0, len(v.Items))
	for _, item := range v.Items {
		parts = append(parts, item.Label+"："+item.Message)
	}
	return strings.Join(parts, "；")
}

// Empty reports whether no validation errors were collected.
func (v *ValidationErrors) Empty() bool { return v == nil || len(v.Items) == 0 }

func (v *ValidationErrors) add(field, label, message string) {
	v.Items = append(v.Items, FieldError{Field: field, Label: label, Message: message})
}

func (v *ValidationErrors) addErr(field, label string, err error) {
	v.add(field, label, err.Error())
}

func (v *ValidationErrors) errOrNil() error {
	if v.Empty() {
		return nil
	}
	return v
}

// logChange appends an audit entry. Audit writes are best effort: the data
// change has already committed and failing the request would invite duplicate
// retries. A failed append is logged instead of being silently dropped.
func (s *Service) logChange(ctx context.Context, entity, entityID, action, summary string, changes []model.FieldChange) {
	entry := &model.ChangeLog{
		Entity:    entity,
		EntityID:  entityID,
		Action:    action,
		Actor:     s.actorFor(ctx),
		Summary:   summary,
		Changes:   changes,
		CreatedAt: s.now(),
	}
	if err := s.st.AppendChangeLog(ctx, entry); err != nil {
		slog.Warn("追加变更记录失败",
			"entity", entity, "entity_id", entityID, "action", action, "error", err)
	}
}

// RecordMCPWriteEnabled appends an audit entry for a change of the MCP write
// switch. Settings changes reuse the append-only change log so the audit view
// shows who opened or closed agent writes, and when.
func (s *Service) RecordMCPWriteEnabled(ctx context.Context, oldEnabled, newEnabled bool) {
	state := func(enabled bool) string {
		if enabled {
			return "开启"
		}
		return "关闭"
	}
	summary := "关闭 MCP 写工具：agent 只能读取，写工具会返回 mcp_write_disabled"
	if newEnabled {
		summary = "开启 MCP 写工具：agent 可新建字段、档案与合同草稿（写入记为 tengshou-mcp）"
	}
	s.logChange(ctx, model.ChangeEntitySettings, "mcp", model.ChangeActionUpdate, summary,
		[]model.FieldChange{{
			Field: "mcp.write_enabled",
			Label: "MCP 写工具",
			Old:   state(oldEnabled),
			New:   state(newEnabled),
		}})
}

var fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// userFieldTypes are the types a runtime-created field may use. party/status
// are reserved for built-in fields.
var userFieldTypes = map[string]bool{
	model.FieldTypeText:     true,
	model.FieldTypeLongText: true,
	model.FieldTypeNumber:   true,
	model.FieldTypeMoney:    true,
	model.FieldTypeDate:     true,
	model.FieldTypeSelect:   true,
	model.FieldTypeBool:     true,
	model.FieldTypeImage:    true,
}

var datePattern = regexp.MustCompile(`^(\d{4})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?$`)

// normalizeDate accepts common Chinese and Excel date spellings and returns
// YYYY-MM-DD. A bare Excel serial number (20000..80000) is also accepted.
func normalizeDate(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if m := datePattern.FindStringSubmatch(raw); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
		if t.Year() == y && int(t.Month()) == mo && t.Day() == d {
			return t.Format("2006-01-02"), true
		}
		return "", false
	}
	if n, err := strconv.Atoi(raw); err == nil && n >= 20000 && n <= 80000 {
		t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
		return t.Format("2006-01-02"), true
	}
	return "", false
}

// parseNumber strips currency and grouping noise users type in Excel.
func parseNumber(raw string) (float64, bool) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ',', '，', ' ', ' ', '¥', '￥', '元':
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	if cleaned == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func parseBoolValue(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "是", "true", "1", "y", "yes", "√", "对", "有":
		return true, true
	case "否", "false", "0", "n", "no", "×", "x", "错", "无":
		return false, true
	}
	return false, false
}

// normalizeStatus maps labels and machine values to draft/active/archived.
func normalizeStatus(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "draft", "草稿", "未生效", "未签":
		return model.ContractDraft, true
	case "active", "生效", "已生效", "履行中", "执行中":
		return model.ContractActive, true
	case "archived", "归档", "已归档", "作废":
		return model.ContractArchived, true
	}
	return "", false
}

// StatusLabel returns the Chinese display label for a stored status.
func StatusLabel(status string) string {
	switch status {
	case model.ContractActive:
		return "生效"
	case model.ContractArchived:
		return "归档"
	default:
		return "草稿"
	}
}

// normalizeKind maps labels and machine values to person/organization.
func normalizeKind(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "person", "个人", "自然人":
		return model.PartyPerson, true
	case "organization", "org", "单位", "组织", "法人", "机构":
		return model.PartyOrganization, true
	}
	return "", false
}

// KindLabel returns the Chinese display label for a stored party kind.
func KindLabel(kind string) string {
	if kind == model.PartyOrganization {
		return "单位"
	}
	return "个人"
}

func isBlank(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// rawString flattens a JSON scalar to the string form used by Excel and forms.
func rawString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "是"
		}
		return "否"
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// displayValue renders a stored value for change logs and previews.
func displayValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "是"
		}
		return "否"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// coerceFieldValue validates and converts one value according to its field
// definition. Dynamic user fields use this path; built-in party/status values
// are normalized by their callers.
func coerceFieldValue(field model.FieldDefinition, raw any) (any, error) {
	if isBlank(raw) {
		return nil, nil
	}
	switch field.Type {
	case model.FieldTypeText, model.FieldTypeLongText:
		return rawString(raw), nil
	case model.FieldTypeNumber:
		v, ok := parseNumber(rawString(raw))
		if !ok {
			return nil, fmt.Errorf("必须是数字")
		}
		return v, nil
	case model.FieldTypeMoney:
		v, ok := parseNumber(rawString(raw))
		if !ok {
			return nil, fmt.Errorf("必须是金额数字")
		}
		if v < 0 {
			return nil, fmt.Errorf("不能为负数")
		}
		return math.Round(v*100) / 100, nil
	case model.FieldTypeDate:
		v, ok := normalizeDate(rawString(raw))
		if !ok {
			return nil, fmt.Errorf("日期格式应为 YYYY-MM-DD")
		}
		if v == "" {
			return nil, nil
		}
		return v, nil
	case model.FieldTypeSelect:
		v := rawString(raw)
		for _, opt := range field.Options {
			if v == opt {
				return v, nil
			}
		}
		return nil, fmt.Errorf("取值必须是：%s", strings.Join(field.Options, " / "))
	case model.FieldTypeBool:
		v, ok := parseBoolValue(rawString(raw))
		if !ok {
			return nil, fmt.Errorf("必须是 是 / 否")
		}
		return v, nil
	case model.FieldTypeImage:
		v := rawString(raw)
		if err := validateAssetPath(v); err != nil {
			return nil, err
		}
		return v, nil
	default:
		return rawString(raw), nil
	}
}

func validateAssetPath(p string) error {
	if p == "" {
		return nil
	}
	if strings.Contains(p, "://") || strings.HasPrefix(strings.ToLower(p), "data:") {
		return fmt.Errorf("图片必须是数据目录内的相对路径")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("图片必须是数据目录内的相对路径")
	}
	clean := path.Clean(filepath.ToSlash(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("图片路径不能包含 ..")
	}
	return nil
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

// writeLock serializes contract-number allocation and record writes.
func (s *Service) writeLock() func() {
	s.mu.Lock()
	return s.mu.Unlock
}

// PageAll loops through a paginated reader surface. It is used by exports.
func pageAll[T any](fetch func(offset int) (model.List[T], error)) ([]T, error) {
	const pageSize = 500
	out := []T{}
	for offset := 0; ; offset += pageSize {
		list, err := fetch(offset)
		if err != nil {
			return nil, err
		}
		out = append(out, list.Items...)
		if len(out) >= list.Total || len(list.Items) == 0 {
			return out, nil
		}
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
