package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// PartyInput is the editable payload for a counterparty record. Core fields
// are typed; Data carries the entity's dynamic field values by field key.
type PartyInput struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	IDNumber string         `json:"id_number"`
	Phone    string         `json:"phone"`
	Address  string         `json:"address"`
	Note     string         `json:"note"`
	Data     map[string]any `json:"data"`
}

// SaveParty creates or updates a counterparty. It returns non-blocking
// warnings such as a duplicated archive name.
func (s *Service) SaveParty(ctx context.Context, in PartyInput) (model.Party, []string, error) {
	var existing *model.Party
	if id := strings.TrimSpace(in.ID); id != "" {
		p, err := s.rd.GetParty(ctx, id)
		if err != nil {
			if isNotFound(err) {
				return model.Party{}, nil, business("not_found", "相对方不存在")
			}
			return model.Party{}, nil, err
		}
		existing = &p
	}
	party, warnings, err := s.buildParty(ctx, in, existing)
	if err != nil {
		return model.Party{}, nil, err
	}
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, nil, err
	}
	action := model.ChangeActionCreate
	summary := fmt.Sprintf("新建相对方档案「%s」", party.Name)
	var changes []model.FieldChange
	if existing != nil {
		action = model.ChangeActionUpdate
		summary = fmt.Sprintf("修改相对方档案「%s」", party.Name)
		changes = s.diffParty(ctx, existing, &party)
	} else {
		changes = s.partyChanges(ctx, &party)
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, action, summary, changes)
	return party, warnings, nil
}

func (s *Service) buildParty(ctx context.Context, in PartyInput, existing *model.Party) (model.Party, []string, error) {
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityParty, false)
	if err != nil {
		return model.Party{}, nil, err
	}
	validation := &ValidationErrors{}
	party := model.Party{}
	if existing != nil {
		party = *existing
		party.Data = copyData(existing.Data)
	} else {
		party.Kind = model.PartyPerson
		party.Data = map[string]any{}
	}
	party.Name = strings.TrimSpace(in.Name)
	if party.Name == "" {
		validation.add(model.FieldPartyName, "名称", "不能为空")
	} else {
		party.Name = truncateRunes(party.Name, 120)
	}
	if kind, ok := normalizeKind(in.Kind); ok {
		party.Kind = kind
	} else {
		validation.add(model.FieldPartyKind, "类型", "只能是 个人 / 单位")
	}
	party.IDNumber = truncateRunes(strings.TrimSpace(in.IDNumber), 120)
	party.Phone = truncateRunes(strings.TrimSpace(in.Phone), 120)
	party.Address = truncateRunes(strings.TrimSpace(in.Address), 300)
	party.Note = strings.TrimSpace(in.Note)

	applyDynamicFieldInput(fields, party.Data, in.Data, existing == nil, validation)
	if err := validation.errOrNil(); err != nil {
		return model.Party{}, nil, err
	}
	warnings := []string{}
	if party.Name != "" {
		matches, err := s.rd.FindPartiesByName(ctx, party.Name)
		if err != nil {
			return model.Party{}, nil, err
		}
		for _, m := range matches {
			if existing != nil && m.ID == existing.ID {
				continue
			}
			warnings = append(warnings,
				fmt.Sprintf("档案中已存在同名相对方「%s」（%s）；如非同一对象，请在名称中区分",
					party.Name, m.ID))
		}
	}
	return party, warnings, nil
}

// DeleteParty removes an unreferenced counterparty record.
func (s *Service) DeleteParty(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	party, err := s.rd.GetParty(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return business("not_found", "相对方不存在")
		}
		return err
	}
	count, err := s.rd.CountContractsForParty(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return business("in_use", "该相对方被 %d 份合同引用，不能删除；如需停用请改名标注", count)
	}
	if err := s.st.DeleteParty(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, model.ChangeEntityParty, id, model.ChangeActionDelete,
		fmt.Sprintf("删除相对方档案「%s」", party.Name), nil)
	return nil
}

func (s *Service) diffParty(ctx context.Context, before, after *model.Party) []model.FieldChange {
	oldValues := s.partyValues(ctx, before)
	newValues := s.partyValues(ctx, after)
	return diffValueMaps(oldValues, newValues, s.fieldLabels(ctx, model.FieldEntityParty))
}

func (s *Service) partyChanges(ctx context.Context, party *model.Party) []model.FieldChange {
	values := s.partyValues(ctx, party)
	labels := s.fieldLabels(ctx, model.FieldEntityParty)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changes := []model.FieldChange{}
	for _, key := range keys {
		if values[key] == "" {
			continue
		}
		changes = append(changes, model.FieldChange{Field: key, Label: labels[key], Old: "", New: values[key]})
	}
	return changes
}

func (s *Service) partyValues(ctx context.Context, party *model.Party) map[string]string {
	values := map[string]string{
		model.FieldPartyName:  party.Name,
		model.FieldPartyKind:  KindLabel(party.Kind),
		model.FieldPartyIDNo:  party.IDNumber,
		model.FieldPartyPhone: party.Phone,
		model.FieldPartyAddr:  party.Address,
		model.FieldPartyNote:  party.Note,
	}
	fields, err := s.rd.ListFieldDefinitions(ctx, model.FieldEntityParty, true)
	if err != nil {
		return values
	}
	for _, field := range fields {
		if field.BuiltIn {
			continue
		}
		values[field.Key] = displayValue(party.Data[field.Key])
	}
	return values
}

func copyData(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// diffValueMaps returns changed fields in display order.
func diffValueMaps(oldValues, newValues map[string]string, labels map[string]string) []model.FieldChange {
	seen := map[string]bool{}
	keys := make([]string, 0, len(oldValues)+len(newValues))
	for k := range oldValues {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range newValues {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	changes := []model.FieldChange{}
	for _, key := range keys {
		if oldValues[key] == newValues[key] {
			continue
		}
		label := labels[key]
		if label == "" {
			label = key
		}
		changes = append(changes, model.FieldChange{
			Field: key, Label: label, Old: oldValues[key], New: newValues[key],
		})
	}
	return changes
}

func (s *Service) fieldLabels(ctx context.Context, entity string) map[string]string {
	labels := map[string]string{}
	fields, err := s.rd.ListFieldDefinitions(ctx, entity, true)
	if err != nil {
		return labels
	}
	for _, f := range fields {
		labels[f.Key] = f.Label
	}
	return labels
}
