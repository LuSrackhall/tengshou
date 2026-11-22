package service

import (
	"context"
	"sort"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// RenewalSource identifies the contract a renewal draft was built from.
type RenewalSource struct {
	ID         string `json:"id"`
	ContractNo string `json:"contract_no"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
}

// RenewalDraft is the prefilled new-contract payload for B7 repeat signing.
// Input is an editable ContractInput; Reused lists the carried-over field keys
// so the UI can show what the archive contributed. Nothing is persisted by
// the draft itself: the user reviews and saves a normal draft contract, and
// the new contract must collect its own signatures before PDF generation.
type RenewalDraft struct {
	Source RenewalSource `json:"source"`
	Input  ContractInput `json:"input"`
	Reused []string      `json:"reused"`
}

// RenewalDraft returns a prefilled contract input based on an existing
// contract (B7 layer one and two: counterparty archive reuse plus generating
// a new contract draft from a previous one).
//
// Prefill rules:
//   - copied: title, type, party A/B, template, amount, and every dynamic
//     data value;
//   - cleared: contract number (auto-allocated on save), signing/validity
//     dates, and the status (back to draft);
//   - recorded: source_contract_id, so the saved contract keeps provenance.
//
// Signatures, seals and output documents are never copied. The third D3 layer
// (signing directly with a retained signature) is intentionally not offered.
func (s *Service) RenewalDraft(ctx context.Context, contractID string) (RenewalDraft, error) {
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return RenewalDraft{}, business("not_found", "合同不存在")
	}
	source, err := s.rd.GetContract(ctx, contractID)
	if err != nil {
		if isNotFound(err) {
			return RenewalDraft{}, business("not_found", "合同不存在")
		}
		return RenewalDraft{}, err
	}

	input := ContractInput{
		Year:             s.now().Year(),
		Title:            source.Title,
		Type:             source.Type,
		PartyAID:         source.PartyAID,
		PartyBID:         source.PartyBID,
		PartyB:           s.partyName(ctx, source.PartyBID),
		Status:           model.ContractDraft,
		Amount:           source.Amount,
		TemplateID:       source.TemplateID,
		SourceContractID: source.ID,
		Data:             copyData(source.Data),
	}

	reused := make([]string, 0, 6+len(input.Data))
	for _, item := range []struct {
		key   string
		value string
	}{
		{model.FieldTitle, input.Title},
		{model.FieldType, input.Type},
		{"party_a_id", input.PartyAID},
		{"party_b_id", input.PartyBID},
		{"template_id", input.TemplateID},
	} {
		if strings.TrimSpace(item.value) != "" {
			reused = append(reused, item.key)
		}
	}
	if input.Amount != 0 {
		reused = append(reused, model.FieldAmount)
	}
	keys := make([]string, 0, len(input.Data))
	for key := range input.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	reused = append(reused, keys...)

	return RenewalDraft{
		Source: RenewalSource{
			ID: source.ID, ContractNo: source.ContractNo,
			Title: source.Title, Year: source.Year,
		},
		Input:  input,
		Reused: reused,
	}, nil
}
