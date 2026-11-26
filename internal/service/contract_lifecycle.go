package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// TerminationInput is the payload of an explicit termination action. It is
// separate from ContractInput so the ledger can end a lease without
// resubmitting the whole contract form.
type TerminationInput struct {
	TerminatedOn string `json:"terminated_on"`
	Reason       string `json:"reason"`
	Note         string `json:"note"`
}

// TerminateContract ends an active contract before its natural lease expiry.
// The row stays in the ledger with status terminated and releases the asset;
// date and reason are mandatory. Expiry itself never calls this (te-l0hh0 D4).
func (s *Service) TerminateContract(ctx context.Context, id string, in TerminationInput) (model.Contract, error) {
	unlock := s.writeLock()
	defer unlock()

	contract, err := s.rd.GetContract(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return model.Contract{}, business("not_found", "合同不存在")
		}
		return model.Contract{}, err
	}
	switch contract.Status {
	case model.ContractActive:
	case model.ContractTerminated:
		return model.Contract{}, business("conflict", "合同已终止，无需重复操作")
	default:
		return model.Contract{}, business("conflict",
			"只有生效中的合同可以终止（当前状态：%s）", StatusLabel(contract.Status))
	}
	date, ok := normalizeDate(in.TerminatedOn)
	if !ok || date == "" {
		return model.Contract{}, business("invalid_termination", "终止日期不能为空，格式应为 YYYY-MM-DD")
	}
	reason, ok := normalizeTerminationReason(in.Reason)
	if !ok {
		return model.Contract{}, business("invalid_termination", "终止原因请选择 提前退租 / 作废 / 其他")
	}
	if contract.LeaseStart != "" && date < contract.LeaseStart {
		return model.Contract{}, business("invalid_termination", "终止日期不能早于租赁起租日")
	}

	before := contract
	contract.Status = model.ContractTerminated
	contract.TerminatedOn = date
	contract.TerminationReason = reason
	contract.TerminationNote = truncateRunes(strings.TrimSpace(in.Note), 500)
	if err := s.st.PutContract(ctx, &contract); err != nil {
		return model.Contract{}, err
	}
	summary := fmt.Sprintf("终止合同 %s「%s」（%s：%s）", contract.ContractNo,
		contract.Title, TerminationReasonLabel(reason), date)
	if contract.TerminationNote != "" {
		summary += "；备注：" + contract.TerminationNote
	}
	s.logChange(ctx, model.ChangeEntityContract, contract.ID, model.ChangeActionUpdate,
		summary, s.diffContract(ctx, &before, &contract))
	return contract, nil
}

// linkRenewalTermination applies the renewal rule: when an active renewal
// contract is saved, its still-active source is terminated with reason renewal
// and the asset it held is released. The termination date comes from
// renewalTerminationDate; confirm acknowledges an earlier end than the source
// lease end. Idempotent: a source that is already terminated or archived is
// left untouched, so retrying the save converges.
func (s *Service) linkRenewalTermination(ctx context.Context, contract *model.Contract, confirm bool) error {
	if contract == nil || contract.Status != model.ContractActive || contract.SourceContractID == "" {
		return nil
	}
	source, err := s.rd.GetContract(ctx, contract.SourceContractID)
	if err != nil {
		if isNotFound(err) {
			// The provenance link outlives the source row; nothing to end.
			return nil
		}
		return err
	}
	if source.ID == contract.ID || source.Status != model.ContractActive {
		return nil
	}
	date, needsConfirm, err := s.renewalTerminationDate(source, *contract, confirm)
	if err != nil {
		return err
	}
	if needsConfirm {
		// buildContract rejects this before persisting; reaching here means the
		// data changed underneath the save, so fail closed instead of ending the
		// source with an unconfirmed date.
		return business("renewal_termination_confirm",
			"续签合同起租日 %s 早于原合同 %s 的租赁到期日 %s，需要确认后才能终止原合同",
			contract.LeaseStart, source.ContractNo, source.LeaseEnd)
	}

	before := source
	source.Status = model.ContractTerminated
	source.TerminatedOn = date
	source.TerminationReason = model.TerminationReasonRenewal
	source.TerminationNote = fmt.Sprintf("续签联动：新合同 %s 于 %s 起租",
		contract.ContractNo, contract.LeaseStart)
	if err := s.st.PutContract(ctx, &source); err != nil {
		return fmt.Errorf("续签联动终止原合同 %s: %w", source.ContractNo, err)
	}
	s.logChange(ctx, model.ChangeEntityContract, source.ID, model.ChangeActionUpdate,
		fmt.Sprintf("续签联动：新合同 %s 生效，自动终止本合同（终止日期 %s）",
			contract.ContractNo, date),
		s.diffContract(ctx, &before, &source))
	return nil
}

// renewalTerminationDate computes when an active renewal source ends.
//
//   - default: the source lease end;
//   - source without a lease end: the day before the new lease start, else today;
//   - new lease starts strictly before the source lease end: the day before the
//     new start, returned with needsConfirm=true until the caller confirms.
func (s *Service) renewalTerminationDate(source, renewal model.Contract, confirm bool) (date string, needsConfirm bool, err error) {
	if renewal.LeaseStart != "" && source.LeaseEnd != "" && renewal.LeaseStart < source.LeaseEnd {
		suggested, err := shiftDate(renewal.LeaseStart, -1)
		if err != nil {
			return "", false, err
		}
		return suggested, !confirm, nil
	}
	if source.LeaseEnd != "" {
		return source.LeaseEnd, false, nil
	}
	if renewal.LeaseStart != "" {
		day, err := shiftDate(renewal.LeaseStart, -1)
		if err != nil {
			return "", false, err
		}
		return day, false, nil
	}
	return s.now().UTC().Format("2006-01-02"), false, nil
}

// shiftDate moves a YYYY-MM-DD date by whole days.
func shiftDate(date string, days int) (string, error) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", fmt.Errorf("日期格式应为 YYYY-MM-DD：%s", date)
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02"), nil
}
