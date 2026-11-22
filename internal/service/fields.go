package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// FieldInput is the editable payload for creating or updating a field
// definition. Pointer fields distinguish "not provided" from false/zero.
type FieldInput struct {
	Entity       string   `json:"entity"`
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	Required     *bool    `json:"required"`
	DefaultValue *string  `json:"default_value"`
	Options      []string `json:"options"`
	InLedger     *bool    `json:"in_ledger"`
	InTemplate   *bool    `json:"in_template"`
	SortOrder    *int     `json:"sort_order"`
	Note         *string  `json:"note"`
	Archived     *bool    `json:"archived"`
}

// builtInRequired keeps the core identity fields mandatory.
var builtInRequired = map[string]map[string]bool{
	model.FieldEntityContract: {
		model.FieldContractNo: true,
		model.FieldYear:       true,
		model.FieldTitle:      true,
		model.FieldPartyB:     true,
	},
	model.FieldEntityParty: {
		model.FieldPartyName: true,
	},
}

// ListFields returns field definitions for an entity in display order.
func (s *Service) ListFields(ctx context.Context, entity string, includeArchived bool) ([]model.FieldDefinition, error) {
	if entity != model.FieldEntityContract && entity != model.FieldEntityParty {
		return nil, business("invalid_entity", "未知的字段实体 %q", entity)
	}
	return s.rd.ListFieldDefinitions(ctx, entity, includeArchived)
}

// CreateField adds a runtime field definition. New fields are immediately
// usable by forms, Excel import/export and template placeholders.
func (s *Service) CreateField(ctx context.Context, in FieldInput) (model.FieldDefinition, error) {
	entity := strings.TrimSpace(in.Entity)
	if entity != model.FieldEntityContract && entity != model.FieldEntityParty {
		return model.FieldDefinition{}, business("invalid_entity", "未知的字段实体 %q", entity)
	}
	key := strings.TrimSpace(in.Key)
	if !fieldKeyPattern.MatchString(key) {
		return model.FieldDefinition{}, business("invalid_key",
			"字段键只能使用小写字母、数字和下划线，且以字母开头（如 area_sqm）")
	}
	if _, err := s.rd.GetFieldByKey(ctx, entity, key); err == nil {
		return model.FieldDefinition{}, business("conflict", "字段 %q 已存在", key)
	} else if !isNotFound(err) {
		return model.FieldDefinition{}, err
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return model.FieldDefinition{}, business("invalid", "字段名称不能为空")
	}
	label = truncateRunes(label, 40)
	fieldType := strings.TrimSpace(in.Type)
	if !userFieldTypes[fieldType] {
		return model.FieldDefinition{}, business("invalid_type", "不支持的字段类型 %q", in.Type)
	}
	options, err := cleanOptions(fieldType, in.Options)
	if err != nil {
		return model.FieldDefinition{}, err
	}
	defaultValue := ""
	if in.DefaultValue != nil {
		defaultValue = strings.TrimSpace(*in.DefaultValue)
	}
	field := model.FieldDefinition{
		ID:           "",
		Entity:       entity,
		Key:          key,
		Label:        label,
		Type:         fieldType,
		Required:     in.Required != nil && *in.Required,
		DefaultValue: defaultValue,
		Options:      options,
		InLedger:     in.InLedger == nil || *in.InLedger,
		InTemplate:   in.InTemplate == nil || *in.InTemplate,
		SortOrder:    s.defaultSortOrder(ctx, entity, in.SortOrder),
	}
	if in.Note != nil {
		field.Note = strings.TrimSpace(*in.Note)
	}
	if field.DefaultValue != "" {
		if _, err := coerceFieldValue(field, field.DefaultValue); err != nil {
			return model.FieldDefinition{}, business("invalid_default",
				"默认值不合法：%s", err.Error())
		}
	}
	if err := s.st.PutFieldDefinition(ctx, &field); err != nil {
		return model.FieldDefinition{}, err
	}
	s.logChange(ctx, model.ChangeEntityField, field.ID, model.ChangeActionCreate,
		fmt.Sprintf("新建字段「%s」（%s，%s）", field.Label, field.Key, fieldTypeLabel(field.Type)),
		[]model.FieldChange{{Field: field.Key, Label: field.Label, Old: "", New: fieldTypeLabel(field.Type)}})
	return field, nil
}

// UpdateField edits a field definition. Keys and built-in types are immutable;
// stored data is never rewritten, so old values stay untouched.
func (s *Service) UpdateField(ctx context.Context, id string, in FieldInput) (model.FieldDefinition, error) {
	field, err := s.rd.GetFieldDefinition(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return model.FieldDefinition{}, business("not_found", "字段不存在")
		}
		return model.FieldDefinition{}, err
	}
	before := field
	if key := strings.TrimSpace(in.Key); key != "" && key != field.Key {
		return model.FieldDefinition{}, business("invalid", "字段键创建后不可修改")
	}
	if label := strings.TrimSpace(in.Label); label != "" {
		field.Label = truncateRunes(label, 40)
	}
	if fieldType := strings.TrimSpace(in.Type); fieldType != "" && fieldType != field.Type {
		if field.BuiltIn {
			return model.FieldDefinition{}, business("invalid", "内置字段类型不可修改")
		}
		if !userFieldTypes[fieldType] {
			return model.FieldDefinition{}, business("invalid_type", "不支持的字段类型 %q", fieldType)
		}
		field.Type = fieldType
		if fieldType != model.FieldTypeSelect {
			field.Options = []string{}
		} else if in.Options == nil {
			return model.FieldDefinition{}, business("invalid_options", "修改为单选字段时需要提供选项")
		}
	}
	if in.Required != nil {
		if before.BuiltIn && builtInRequired[before.Entity][before.Key] && !*in.Required {
			return model.FieldDefinition{}, business("invalid", "内置字段「%s」必须保持必填", before.Label)
		}
		field.Required = *in.Required
	}
	if in.DefaultValue != nil {
		field.DefaultValue = strings.TrimSpace(*in.DefaultValue)
	}
	if in.Options != nil && !before.BuiltIn {
		options, err := cleanOptions(field.Type, in.Options)
		if err != nil {
			return model.FieldDefinition{}, err
		}
		field.Options = options
	}
	if in.InLedger != nil {
		field.InLedger = *in.InLedger
	}
	if in.InTemplate != nil {
		field.InTemplate = *in.InTemplate
	}
	if in.SortOrder != nil {
		field.SortOrder = *in.SortOrder
	}
	if in.Note != nil {
		field.Note = strings.TrimSpace(*in.Note)
	}
	action := model.ChangeActionUpdate
	if in.Archived != nil {
		if *in.Archived && before.BuiltIn {
			return model.FieldDefinition{}, business("invalid", "内置字段不能归档")
		}
		if *in.Archived != before.Archived {
			action = model.ChangeActionArchive
			if !*in.Archived {
				action = model.ChangeActionUpdate
			}
		}
		field.Archived = *in.Archived
	}
	if field.DefaultValue != "" {
		if _, err := coerceFieldValue(field, field.DefaultValue); err != nil {
			return model.FieldDefinition{}, business("invalid_default", "默认值不合法：%s", err.Error())
		}
	}
	if err := s.st.PutFieldDefinition(ctx, &field); err != nil {
		return model.FieldDefinition{}, err
	}
	changes := diffFieldDefinitions(before, field)
	summary := fmt.Sprintf("修改字段「%s」", field.Label)
	if action == model.ChangeActionArchive {
		summary = fmt.Sprintf("归档字段「%s」", field.Label)
	}
	s.logChange(ctx, model.ChangeEntityField, field.ID, action, summary, changes)
	return field, nil
}

// ArchiveField hides a user field from entry and Excel while keeping all
// previously stored values.
func (s *Service) ArchiveField(ctx context.Context, id string) (model.FieldDefinition, error) {
	archived := true
	return s.UpdateField(ctx, id, FieldInput{Archived: &archived})
}

func cleanOptions(fieldType string, raw []string) ([]string, error) {
	if fieldType != model.FieldTypeSelect {
		return []string{}, nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, opt := range raw {
		opt = strings.TrimSpace(opt)
		if opt == "" || seen[opt] {
			continue
		}
		seen[opt] = true
		out = append(out, opt)
		if len(out) > 100 {
			return nil, business("invalid_options", "选项不能超过 100 个")
		}
	}
	if len(out) == 0 {
		return nil, business("invalid_options", "单选字段至少需要一个选项")
	}
	return out, nil
}

func (s *Service) defaultSortOrder(ctx context.Context, entity string, requested *int) int {
	if requested != nil {
		return *requested
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, entity, true)
	if err != nil {
		return 1000
	}
	max := 0
	for _, f := range fields {
		if f.SortOrder > max {
			max = f.SortOrder
		}
	}
	return max + 10
}

func fieldTypeLabel(t string) string {
	switch t {
	case model.FieldTypeText:
		return "文本"
	case model.FieldTypeLongText:
		return "长文本"
	case model.FieldTypeNumber:
		return "数字"
	case model.FieldTypeMoney:
		return "金额"
	case model.FieldTypeDate:
		return "日期"
	case model.FieldTypeSelect:
		return "单选"
	case model.FieldTypeBool:
		return "是/否"
	case model.FieldTypeImage:
		return "图片路径"
	case model.FieldTypeParty:
		return "相对方"
	case model.FieldTypeStatus:
		return "状态"
	default:
		return t
	}
}

func diffFieldDefinitions(before, after model.FieldDefinition) []model.FieldChange {
	changes := []model.FieldChange{}
	add := func(key, label, old, new string) {
		if old == new {
			return
		}
		changes = append(changes, model.FieldChange{Field: key, Label: label, Old: old, New: new})
	}
	add("label", "字段名称", before.Label, after.Label)
	add("required", "是否必填", yesNoLabel(before.Required), yesNoLabel(after.Required))
	add("default_value", "默认值", before.DefaultValue, after.DefaultValue)
	add("in_ledger", "出现在台账", yesNoLabel(before.InLedger), yesNoLabel(after.InLedger))
	add("in_template", "出现在模板", yesNoLabel(before.InTemplate), yesNoLabel(after.InTemplate))
	add("sort_order", "排序", fmt.Sprint(before.SortOrder), fmt.Sprint(after.SortOrder))
	add("note", "说明", before.Note, after.Note)
	add("options", "选项", strings.Join(before.Options, " / "), strings.Join(after.Options, " / "))
	add("archived", "归档", yesNoLabel(before.Archived), yesNoLabel(after.Archived))
	return changes
}

func yesNoLabel(v bool) string {
	if v {
		return "是"
	}
	return "否"
}
