// 新手引导与只读自检契约。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/httpapi/onboarding.go   onboardingStep / onboardingPayload /
//                                    onboardingStateInput / 自检响应
//   internal/service/onboarding.go   OnboardingFacts / OnboardingCheckItem /
//                                    步骤常量
//   internal/config                  Onboarding 记录的 completed/skipped/steps

import type { CheckStatus, ISODateTime } from "./common";

export type OnboardingStepID =
  | "template_import"
  | "template_calibrate"
  | "sample_preview"
  | "trial_pdf"
  | "contract_create";

/** 五步引导的一行；数据驱动的步骤每次读取时重算。 */
export interface OnboardingStep {
  id: OnboardingStepID;
  label: string;
  done: boolean;
  done_at?: string;
  detail?: string;
  action: OnboardingStepID;
  /** true 表示完成状态来自数据库，false 表示来自 config.json 记录。 */
  data_backed: boolean;
}

export interface OnboardingFacts {
  templates: number;
  calibrated_templates: number;
  contracts: number;
  active_template_name?: string;
  active_template_version?: number;
  active_template_placements: number;
}

export interface Onboarding {
  /** 数据目录里既无模板也无合同时为 true。 */
  first_run: boolean;
  completed: boolean;
  skipped: boolean;
  completed_at?: string;
  skipped_at?: string;
  show_guide: boolean;
  steps: OnboardingStep[];
  facts: OnboardingFacts;
}

// ---------- 引导状态写入 ----------

export type OnboardingAction = "complete" | "skip" | "reopen" | "step";

/** 只能手动记录的两个样例步骤；其余步骤由真实数据判定。 */
export type OnboardingSampleStep = "sample_preview" | "trial_pdf";

export type OnboardingStateInput =
  | { action: "complete" | "skip" | "reopen" }
  | { action: "step"; step: OnboardingSampleStep };

// ---------- 只读自检 ----------

/** 自检一行（service.OnboardingCheckItem）。 */
export interface OnboardingCheckItem {
  id: string;
  label: string;
  status: CheckStatus;
  detail?: string;
  action?: string;
  /** 修复入口所在的应用 tab；空表示需在应用外处理。 */
  target?: string;
}

export interface OnboardingCheckSummary {
  ok: number;
  warn: number;
  error: number;
}

/** GET /onboarding/check 的响应。 */
export interface OnboardingCheckResponse {
  checked_at: ISODateTime;
  items: OnboardingCheckItem[];
  summary: OnboardingCheckSummary;
}
