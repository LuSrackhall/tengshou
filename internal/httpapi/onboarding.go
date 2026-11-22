package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/service"
)

// onboardingStep is one line of the first-run guide. Data-backed steps are
// recomputed from the database on every read; sample-only steps carry the
// timestamp recorded in config.json.
type onboardingStep struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Done       bool   `json:"done"`
	DoneAt     string `json:"done_at,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Action     string `json:"action"`
	DataBacked bool   `json:"data_backed"`
}

// onboardingPayload is the shared bootstrap / onboarding response. First-run
// means the data directory has no template and no contract; existing field
// definitions do not change that (B8).
type onboardingPayload struct {
	FirstRun    bool                    `json:"first_run"`
	Completed   bool                    `json:"completed"`
	Skipped     bool                    `json:"skipped"`
	CompletedAt string                  `json:"completed_at,omitempty"`
	SkippedAt   string                  `json:"skipped_at,omitempty"`
	ShowGuide   bool                    `json:"show_guide"`
	Steps       []onboardingStep        `json:"steps"`
	Facts       service.OnboardingFacts `json:"facts"`
}

func (s *Server) onboardingState() config.Onboarding {
	if s.opts.Settings != nil {
		return s.opts.Settings.Onboarding()
	}
	return s.opts.Config.Onboarding
}

// onboardingPayload builds the guide state. Facts read failures degrade to
// zeros so a broken read never blocks the welcome page itself.
func (s *Server) onboardingPayload(ctx context.Context, svc *service.Service) onboardingPayload {
	state := s.onboardingState()
	facts := service.OnboardingFacts{}
	if svc != nil {
		got, err := svc.OnboardingFacts(ctx)
		if err != nil {
			if s.opts.Logger != nil {
				s.opts.Logger.Warn("read onboarding facts", "error", err)
			}
		} else {
			facts = got
		}
	}
	stepDoneAt := func(id string) string {
		if state.Steps == nil {
			return ""
		}
		return strings.TrimSpace(state.Steps[id])
	}
	calibrateDetail := "在模板 PDF 上拖框，标出哪里填字段、哪里落签名与公章。"
	if facts.Templates == 0 {
		calibrateDetail = "先完成第 1 步：导入一份上级或对方给过的模板。"
	} else if facts.ActiveTemplateName != "" {
		calibrateDetail = fmt.Sprintf("当前模板「%s」v%d；在模板 PDF 上拖框，标出字段、签名与公章位置。",
			facts.ActiveTemplateName, facts.ActiveTemplateVersion)
	}
	steps := []onboardingStep{
		{
			ID:         service.OnboardingStepTemplateImport,
			Label:      "导入上级模板",
			Done:       facts.Templates > 0,
			Detail:     "从上级或对方给过的 Word 模板（.docx）或现成 PDF 开始，不必从空白功能开始。",
			Action:     service.OnboardingStepTemplateImport,
			DataBacked: true,
		},
		{
			ID:         service.OnboardingStepTemplateCalibrate,
			Label:      "标定字段、签名与公章位置",
			Done:       facts.CalibratedTemplates > 0,
			Detail:     calibrateDetail,
			Action:     service.OnboardingStepTemplateCalibrate,
			DataBacked: true,
		},
		{
			ID:         service.OnboardingStepSamplePreview,
			Label:      "用样例数据预览",
			Done:       stepDoneAt(service.OnboardingStepSamplePreview) != "",
			DoneAt:     stepDoneAt(service.OnboardingStepSamplePreview),
			Detail:     "样例数据不写入台账，也不写入档案或生成记录，可放心试。",
			Action:     service.OnboardingStepSamplePreview,
			DataBacked: false,
		},
		{
			ID:         service.OnboardingStepTrialPDF,
			Label:      "试生成 PDF",
			Done:       stepDoneAt(service.OnboardingStepTrialPDF) != "",
			DoneAt:     stepDoneAt(service.OnboardingStepTrialPDF),
			Detail:     "试生成只产生临时 PDF，同样使用样例数据，不写入生成记录。",
			Action:     service.OnboardingStepTrialPDF,
			DataBacked: false,
		},
		{
			ID:         service.OnboardingStepContractCreate,
			Label:      "录入第一份合同",
			Done:       facts.Contracts > 0,
			Detail:     "录入第一份真实合同，台账、导出与出件从这份开始。",
			Action:     service.OnboardingStepContractCreate,
			DataBacked: true,
		},
	}
	firstRun := facts.Templates == 0 && facts.Contracts == 0
	return onboardingPayload{
		FirstRun:    firstRun,
		Completed:   state.Completed,
		Skipped:     state.Skipped,
		CompletedAt: state.CompletedAt,
		SkippedAt:   state.SkippedAt,
		ShowGuide:   firstRun && !state.Completed && !state.Skipped,
		Steps:       steps,
		Facts:       facts,
	}
}

func (s *Server) handleAppOnboarding(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.onboardingPayload(r.Context(), svc))
}

// handleAppOnboardingCheck is the read-only self-check page endpoint. Each
// item reports {id, label, status, detail, action}; failures never abort the
// other probe results.
func (s *Server) handleAppOnboardingCheck(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	items := svc.OnboardingCheck(r.Context(), service.OnboardingCheckInput{
		AllowLAN: s.opts.Config.HTTP.AllowLAN,
		LANHosts: s.lanHosts(),
	})
	summary := map[string]int{
		service.CheckStatusOK:    0,
		service.CheckStatusWarn:  0,
		service.CheckStatusError: 0,
	}
	for _, item := range items {
		switch item.Status {
		case service.CheckStatusOK:
			summary[service.CheckStatusOK]++
		case service.CheckStatusError:
			summary[service.CheckStatusError]++
		default:
			summary[service.CheckStatusWarn]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"checked_at": time.Now().UTC().Format(time.RFC3339),
		"items":      items,
		"summary":    summary,
	})
}

type onboardingStateInput struct {
	// Action is complete, skip, reopen or step.
	Action string `json:"action"`
	// Step is required for action=step: sample_preview or trial_pdf.
	Step string `json:"step"`
}

// handleAppOnboardingState records guide bookkeeping in config.json. It never
// writes contracts, parties, templates or documents; the two sample-only steps
// are recorded here because the sample render path intentionally leaves no
// business data behind.
func (s *Server) handleAppOnboardingState(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable",
			"运行时设置尚未初始化")
		return
	}
	var in onboardingStateInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	step := strings.TrimSpace(in.Step)
	now := time.Now().UTC().Format(time.RFC3339)
	next := s.opts.Settings.Onboarding()
	switch action {
	case "complete":
		next.Completed = true
		next.CompletedAt = now
		next.Skipped = false
		next.SkippedAt = ""
	case "skip":
		next.Skipped = true
		next.SkippedAt = now
		next.Completed = false
		next.CompletedAt = ""
	case "reopen":
		next.Completed = false
		next.CompletedAt = ""
		next.Skipped = false
		next.SkippedAt = ""
	case "step":
		switch step {
		case service.OnboardingStepSamplePreview, service.OnboardingStepTrialPDF:
		default:
			writeError(w, http.StatusBadRequest, "invalid_step",
				"只能记录 sample_preview 或 trial_pdf 步骤；其它步骤由真实数据自动判定")
			return
		}
		if next.Steps == nil {
			next.Steps = map[string]string{}
		}
		next.Steps[step] = now
	default:
		writeError(w, http.StatusBadRequest, "invalid_action",
			"action 只能是 complete / skip / reopen / step")
		return
	}
	if err := s.opts.Settings.SetOnboarding(next); err != nil {
		s.internalError(w, "save onboarding state", err)
		return
	}
	writeJSON(w, http.StatusOK, s.onboardingPayload(r.Context(), svc))
}
