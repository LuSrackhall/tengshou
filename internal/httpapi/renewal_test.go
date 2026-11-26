package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type renewalDraftPayload struct {
	Source struct {
		ID         string `json:"id"`
		ContractNo string `json:"contract_no"`
		Title      string `json:"title"`
		Year       int    `json:"year"`
		Status     string `json:"status"`
	} `json:"source"`
	Input struct {
		ContractNo       string         `json:"contract_no"`
		Year             int            `json:"year"`
		Title            string         `json:"title"`
		Type             string         `json:"type"`
		PartyAID         string         `json:"party_a_id"`
		PartyBID         string         `json:"party_b_id"`
		Status           string         `json:"status"`
		SignedOn         string         `json:"signed_on"`
		ValidFrom        string         `json:"valid_from"`
		ValidTo          string         `json:"valid_to"`
		Amount           float64        `json:"amount"`
		TemplateID       string         `json:"template_id"`
		SourceContractID string         `json:"source_contract_id"`
		Data             map[string]any `json:"data"`
	} `json:"input"`
	Reused []string `json:"reused"`
}

func hasReusedField(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// TestAppRenewalDraftAndSourceTrace covers the B7 HTTP surface: the read-only
// draft, creation with source_contract_id, the audited summary and the party
// history filter.
func TestAppRenewalDraftAndSourceTrace(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/app/api/v1/contracts/"+f.contractID+"/renewal-draft", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("renewal draft: %d %s", resp.StatusCode, body)
	}
	var draft renewalDraftPayload
	if err := json.Unmarshal(body, &draft); err != nil {
		t.Fatalf("decode draft: %v body=%s", err, body)
	}
	if draft.Source.ID != f.contractID || draft.Source.ContractNo == "" ||
		draft.Source.Title != "幸福村房屋租赁合同" {
		t.Fatalf("source = %+v", draft.Source)
	}
	if draft.Source.Status != "active" {
		t.Fatalf("source status = %q, want active", draft.Source.Status)
	}
	if draft.Input.ContractNo != "" || draft.Input.Status != "draft" ||
		draft.Input.SignedOn != "" || draft.Input.ValidFrom != "" || draft.Input.ValidTo != "" {
		t.Fatalf("draft should clear identity/dates: %+v", draft.Input)
	}
	if draft.Input.SourceContractID != f.contractID ||
		draft.Input.PartyBID != f.partyBID ||
		draft.Input.PartyAID != f.partyID ||
		draft.Input.Amount != 0 || draft.Input.Title != draft.Source.Title {
		t.Fatalf("draft input = %+v", draft.Input)
	}
	if draft.Input.Data["rent"] != 1200.0 {
		t.Fatalf("draft data = %#v", draft.Input.Data)
	}
	for _, key := range []string{"title", "party_a_id", "party_b_id", "rent"} {
		if !hasReusedField(draft.Reused, key) {
			t.Fatalf("reused = %v, missing %q", draft.Reused, key)
		}
	}

	resp, body = f.get(t, "/app/api/v1/contracts/ctr_missing/renewal-draft", false)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "not_found") {
		t.Fatalf("missing draft: %d %s", resp.StatusCode, body)
	}

	// Creating with a source records provenance and an auditable summary.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
		"title": "续签后的合同", "party_b_id": f.partyBID, "amount": 1500,
		"year": 2027, "source_contract_id": f.contractID, "data": map[string]any{},
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create renewal: %d %s", resp.StatusCode, body)
	}
	renewed := decodeAppItem(t, body)
	if renewed["source_contract_id"] != f.contractID {
		t.Fatalf("renewal source = %v", renewed["source_contract_id"])
	}
	newID, _ := renewed["id"].(string)
	resp, body = f.get(t, "/app/api/v1/changelog?entity=contract&entity_id="+newID, false)
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), "续签（来源 "+draft.Source.ContractNo+"）") {
		t.Fatalf("renewal changelog: %d %s", resp.StatusCode, body)
	}

	// An update cannot rewrite the source link.
	_, otherBody := f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
		"title": "无关合同", "party_b_id": f.partyBID, "year": 2027, "data": map[string]any{},
	}, nil)
	otherID, _ := decodeAppItem(t, otherBody)["id"].(string)
	resp, body = f.appRequest(t, http.MethodPut, "/app/api/v1/contracts/"+newID, map[string]any{
		"title": "续签后的合同（改）", "party_b_id": f.partyBID, "amount": 1500,
		"year": 2027, "source_contract_id": otherID, "data": map[string]any{},
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update renewal: %d %s", resp.StatusCode, body)
	}
	updated := decodeAppItem(t, body)
	if updated["source_contract_id"] != f.contractID {
		t.Fatalf("update changed source to %v", updated["source_contract_id"])
	}

	// party_id filters contracts on either side of the link.
	resp, body = f.get(t, "/app/api/v1/contracts?party_id="+f.partyBID, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("party filter: %d %s", resp.StatusCode, body)
	}
	var byParty struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &byParty); err != nil {
		t.Fatal(err)
	}
	if byParty.Total != 3 {
		t.Fatalf("party filter total = %d, want 3", byParty.Total)
	}
	resp, body = f.get(t, "/app/api/v1/contracts?party_id="+f.partyID, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("party A filter: %d %s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &byParty); err != nil {
		t.Fatal(err)
	}
	if byParty.Total != 1 {
		t.Fatalf("party A filter total = %d, want 1", byParty.Total)
	}
}

func TestAppCreateWithMissingRenewalSource(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/contracts", map[string]any{
		"title": "坏来源合同", "party_b_id": f.partyBID,
		"source_contract_id": "ctr_missing", "data": map[string]any{},
	}, nil)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "not_found") {
		t.Fatalf("missing source create: %d %s", resp.StatusCode, body)
	}
}
