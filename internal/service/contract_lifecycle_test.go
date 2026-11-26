package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func dayOffset(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02")
}

// TestTerminateContractValidationAndAssetRelease covers the explicit
// termination flow: date and reason are mandatory, the row keeps its history,
// and the asset becomes vacant.
func TestTerminateContractValidationAndAssetRelease(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")
	asset := mustAsset(t, svc, "终止测试房产")
	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "终止测试合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "生效",
	})

	if _, err := svc.TerminateContract(ctx, contract.ID, service.TerminationInput{}); businessCode(t, err) != "invalid_termination" {
		t.Fatalf("missing date should fail: %v", err)
	}
	if _, err := svc.TerminateContract(ctx, contract.ID, service.TerminationInput{
		TerminatedOn: "2026-06-01",
	}); businessCode(t, err) != "invalid_termination" {
		t.Fatalf("missing reason should fail: %v", err)
	}
	if _, err := svc.TerminateContract(ctx, contract.ID, service.TerminationInput{
		TerminatedOn: "2025-12-31", Reason: "提前退租",
	}); businessCode(t, err) != "invalid_termination" {
		t.Fatalf("date before lease start should fail: %v", err)
	}

	terminated, err := svc.TerminateContract(ctx, contract.ID, service.TerminationInput{
		TerminatedOn: "2026-06-30", Reason: "提前退租", Note: "租户工作调动",
	})
	if err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if terminated.Status != model.ContractTerminated ||
		terminated.TerminatedOn != "2026-06-30" ||
		terminated.TerminationReason != model.TerminationReasonEarly ||
		terminated.TerminationNote != "租户工作调动" {
		t.Fatalf("terminated contract = %+v", terminated)
	}
	stored, err := rd.GetContract(ctx, contract.ID)
	if err != nil || stored.Status != model.ContractTerminated {
		t.Fatalf("stored contract = %+v err=%v", stored, err)
	}
	vacant, err := rd.GetAsset(ctx, asset.ID)
	if err != nil || vacant.LeaseStatus != model.LeaseStatusVacant {
		t.Fatalf("asset after termination = %q err=%v", vacant.LeaseStatus, err)
	}

	// Terminated contracts stay in the ledger and are filterable.
	list, err := svc.ListContracts(ctx, model.ListFilter{Status: model.ContractTerminated})
	if err != nil || list.Total != 1 || list.Items[0].ID != contract.ID {
		t.Fatalf("terminated filter = %+v err=%v", list, err)
	}
	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{EntityID: contract.ID})
	if err != nil {
		t.Fatalf("changelog: %v", err)
	}
	found := false
	for _, entry := range logs.Items {
		if strings.Contains(entry.Summary, "终止合同") && strings.Contains(entry.Summary, "提前退租") {
			found = true
		}
	}
	if !found {
		t.Fatalf("termination not audited: %+v", logs.Items)
	}

	// Repeated termination is a conflict; drafts cannot be terminated.
	if _, err := svc.TerminateContract(ctx, contract.ID, service.TerminationInput{
		TerminatedOn: "2026-06-30", Reason: "作废",
	}); businessCode(t, err) != "conflict" {
		t.Fatalf("repeat termination = %v", err)
	}
	draft := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "草稿合同", PartyBID: party.ID,
	})
	if _, err := svc.TerminateContract(ctx, draft.ID, service.TerminationInput{
		TerminatedOn: "2026-06-30", Reason: "作废",
	}); businessCode(t, err) != "conflict" {
		t.Fatalf("draft termination = %v", err)
	}
}

// TestContractOverlapHardBlock covers occupancy: an active overlapping lease
// is rejected with the conflicting contract number; drafts only warn; a
// terminated conflict no longer blocks; historical rows without lease dates
// never block.
func TestContractOverlapHardBlock(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "李四")
	asset := mustAsset(t, svc, "硬拦房产")
	first := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "首份在租合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "生效",
	})

	_, _, err := svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "重叠生效合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-06-01", LeaseEnd: "2027-05-31", Status: "生效",
	})
	if businessCode(t, err) != "lease_conflict" || !strings.Contains(err.Error(), first.ContractNo) {
		t.Fatalf("overlap should be blocked with contract number: %v", err)
	}
	// The failed save must not persist a phantom contract.
	list, _ := svc.ListContracts(ctx, model.ListFilter{})
	if list.Total != 1 {
		t.Fatalf("blocked save persisted a contract: total=%d", list.Total)
	}

	// A draft with the same overlapping period saves and only warns.
	draft := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "重叠草稿", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-06-01", LeaseEnd: "2027-05-31", Status: "草稿",
	})
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{
		ID: draft.ID, Year: 2026, Title: draft.Title, PartyBID: party.ID,
		AssetID: asset.ID, LeaseStart: draft.LeaseStart, LeaseEnd: draft.LeaseEnd,
		Status: "生效",
	}); businessCode(t, err) != "lease_conflict" {
		t.Fatalf("activating overlapping draft should be blocked: %v", err)
	}

	// Terminating the active contract releases the asset for the draft.
	if _, err := svc.TerminateContract(ctx, first.ID, service.TerminationInput{
		TerminatedOn: "2026-05-31", Reason: "提前退租",
	}); err != nil {
		t.Fatalf("terminate first: %v", err)
	}
	if _, _, err := svc.SaveContract(ctx, service.ContractInput{
		ID: draft.ID, Year: 2026, Title: draft.Title, PartyBID: party.ID,
		AssetID: asset.ID, LeaseStart: draft.LeaseStart, LeaseEnd: draft.LeaseEnd,
		Status: "生效",
	}); err != nil {
		t.Fatalf("activation after termination: %v", err)
	}
	rented, err := rd.GetAsset(ctx, asset.ID)
	if err != nil || rented.LeaseStatus != model.LeaseStatusRented {
		t.Fatalf("asset after re-rent = %q err=%v", rented.LeaseStatus, err)
	}

	// A legacy active contract without lease dates does not block.
	legacyAsset := mustAsset(t, svc, "历史房产")
	mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "无租期历史合同", PartyBID: party.ID,
		AssetID: legacyAsset.ID, Status: "生效",
	})
	_, warnings, err := svc.SaveContract(ctx, service.ContractInput{
		Year: 2026, Title: "补录租期合同", PartyBID: party.ID, AssetID: legacyAsset.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "生效",
	})
	if err != nil {
		t.Fatalf("legacy row must not block: %v", err)
	}
	joined := strings.Join(warnings, "；")
	if !strings.Contains(joined, "缺少完整租赁日期，未参与租期重叠校验") {
		t.Fatalf("legacy warning missing: %#v", warnings)
	}
}

// TestRenewalLinkageTerminatesSource covers the renewal rule: an active
// renewal ends its source with reason renewal; an earlier new start requires
// confirmation and then terminates the day before the new start; a draft
// renewal changes nothing.
func TestRenewalLinkageTerminatesSource(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "王五")
	asset := mustAsset(t, svc, "续签房产")
	source := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "原合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "生效",
	})

	// Draft renewal saves without touching the source.
	mustContract(t, svc, service.ContractInput{
		Year: 2027, Title: "续签草稿", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2027-01-01", LeaseEnd: "2027-12-31", Status: "草稿",
		SourceContractID: source.ID,
	})
	unchanged, _ := rd.GetContract(ctx, source.ID)
	if unchanged.Status != model.ContractActive {
		t.Fatalf("draft renewal terminated the source: %+v", unchanged)
	}

	// Active renewal starting after the source lease end: source ends on its
	// lease end date with reason renewal.
	renewal := mustContract(t, svc, service.ContractInput{
		Year: 2027, Title: "续签合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2027-01-01", LeaseEnd: "2027-12-31", Status: "生效",
		SourceContractID: source.ID,
	})
	closed, err := rd.GetContract(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != model.ContractTerminated ||
		closed.TerminationReason != model.TerminationReasonRenewal ||
		closed.TerminatedOn != "2026-12-31" {
		t.Fatalf("source after renewal = %+v", closed)
	}
	logs, err := rd.ListChangeLogs(ctx, store.ChangeFilter{EntityID: source.ID})
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, entry := range logs.Items {
		if strings.Contains(entry.Summary, "续签联动") &&
			strings.Contains(entry.Summary, renewal.ContractNo) {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("renewal linkage not audited: %+v", logs.Items)
	}

	// A renewal starting before the source lease end needs confirmation.
	source2 := mustContract(t, svc, service.ContractInput{
		Year: 2028, Title: "提前续签原合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2028-01-01", LeaseEnd: "2028-12-31", Status: "生效",
	})
	early := service.ContractInput{
		Year: 2028, Title: "提前续签合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: "2028-06-01", LeaseEnd: "2029-05-31", Status: "生效",
		SourceContractID: source2.ID,
	}
	_, _, err = svc.SaveContract(ctx, early)
	if businessCode(t, err) != "renewal_termination_confirm" ||
		!strings.Contains(err.Error(), "2028-05-31") {
		t.Fatalf("early renewal should ask for confirmation: %v", err)
	}
	// Nothing was persisted by the rejected save.
	if still, _ := rd.GetContract(ctx, source2.ID); still.Status != model.ContractActive {
		t.Fatalf("rejected renewal changed source: %+v", still)
	}
	early.RenewalTerminationConfirm = true
	if _, _, err := svc.SaveContract(ctx, early); err != nil {
		t.Fatalf("confirmed early renewal: %v", err)
	}
	closedEarly, _ := rd.GetContract(ctx, source2.ID)
	if closedEarly.TerminatedOn != "2028-05-31" {
		t.Fatalf("confirmed termination date = %q, want 2028-05-31", closedEarly.TerminatedOn)
	}
}

// TestContractExpiryRemindersAndStats pins the read-time reminder labels and
// the reminder counters: they never change status.
func TestContractExpiryRemindersAndStats(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "赵六")
	asset := mustAsset(t, svc, "提醒房产")

	expiring := mustContract(t, svc, service.ContractInput{
		Year: time.Now().Year(), Title: "即将到期合同", PartyBID: party.ID, AssetID: asset.ID,
		LeaseStart: dayOffset(-300), LeaseEnd: dayOffset(10), Status: "生效",
	})
	expired := mustContract(t, svc, service.ContractInput{
		Year: time.Now().Year(), Title: "已到期合同", PartyBID: party.ID,
		LeaseStart: dayOffset(-400), LeaseEnd: dayOffset(-1), Status: "生效",
	})
	far := mustContract(t, svc, service.ContractInput{
		Year: time.Now().Year(), Title: "远期合同", PartyBID: party.ID,
		LeaseStart: dayOffset(-1), LeaseEnd: dayOffset(100), Status: "生效",
	})

	check := func(id, want string) {
		t.Helper()
		got, err := rd.GetContract(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ExpiryState != want {
			t.Fatalf("expiry_state(%s) = %q, want %q", got.Title, got.ExpiryState, want)
		}
		if want != "" && got.Status != model.ContractActive {
			t.Fatalf("reminder changed status: %+v", got)
		}
	}
	check(expiring.ID, model.ExpiryStateExpiring)
	check(expired.ID, model.ExpiryStateExpired)
	check(far.ID, "")

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ExpiringContracts != 1 || stats.ExpiredContracts != 1 {
		t.Fatalf("stats reminders = %d/%d", stats.ExpiringContracts, stats.ExpiredContracts)
	}

	// Terminating removes the reminder (status is no longer active).
	if _, err := svc.TerminateContract(ctx, expiring.ID, service.TerminationInput{
		TerminatedOn: dayOffset(0), Reason: "其他",
	}); err != nil {
		t.Fatal(err)
	}
	check(expiring.ID, "")
	stats, _ = svc.Stats(ctx)
	if stats.ExpiringContracts != 0 || stats.ExpiredContracts != 1 {
		t.Fatalf("stats after termination = %d/%d", stats.ExpiringContracts, stats.ExpiredContracts)
	}
}

// TestContractTerminationViaEditForm covers the form path: status 已终止 needs
// the date and reason, and leaving the terminated state clears them.
func TestContractTerminationViaEditForm(t *testing.T) {
	svc, rd, _ := openService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "孙七")
	contract := mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "表单终止合同", PartyBID: party.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "生效",
	})

	_, _, err := svc.SaveContract(ctx, service.ContractInput{
		ID: contract.ID, Year: 2026, Title: contract.Title, PartyBID: party.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "已终止",
	})
	var validation *service.ValidationErrors
	if !errors.As(err, &validation) {
		t.Fatalf("edit termination without date/reason = %v", err)
	}
	fields := map[string]bool{}
	for _, item := range validation.Items {
		fields[item.Field] = true
	}
	if !fields["terminated_on"] || !fields["termination_reason"] {
		t.Fatalf("validation fields = %+v", validation.Items)
	}

	terminated := mustContract(t, svc, service.ContractInput{
		ID: contract.ID, Year: 2026, Title: contract.Title, PartyBID: party.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "已终止",
		TerminatedOn: "2026-03-31", TerminationReason: "作废", TerminationNote: "录入错误",
	})
	if terminated.Status != model.ContractTerminated ||
		terminated.TerminationReason != model.TerminationReasonVoid {
		t.Fatalf("form termination = %+v", terminated)
	}

	// Reverting to active clears the lifecycle fields.
	reopened := mustContract(t, svc, service.ContractInput{
		ID: contract.ID, Year: 2026, Title: contract.Title, PartyBID: party.ID,
		LeaseStart: "2026-01-01", LeaseEnd: "2026-12-31", Status: "生效",
	})
	if reopened.Status != model.ContractActive || reopened.TerminatedOn != "" ||
		reopened.TerminationReason != "" || reopened.TerminationNote != "" {
		t.Fatalf("reopened contract keeps lifecycle fields: %+v", reopened)
	}
	stored, _ := rd.GetContract(ctx, contract.ID)
	if stored.TerminatedOn != "" || stored.TerminationReason != "" {
		t.Fatalf("stored reopened contract = %+v", stored)
	}
}
