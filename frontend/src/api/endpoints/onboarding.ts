// 引导与事件流端点。
//
//   GET  /bootstrap
//   GET  /onboarding
//   GET  /onboarding/check
//   POST /onboarding/state
//   GET  /events（SSE 地址）

import { apiUrl, get, post, type RequestOptions } from "../client";
import type {
  Bootstrap,
  Onboarding,
  OnboardingCheckResponse,
  OnboardingStateInput,
} from "../types";

/** GET /bootstrap：版本、年度、统计、引导状态与 MCP 开关。 */
export function getBootstrap(options: RequestOptions = {}): Promise<Bootstrap> {
  return get("/bootstrap", options);
}

/** GET /onboarding：五步引导的当前状态。 */
export function getOnboarding(
  options: RequestOptions = {},
): Promise<Onboarding> {
  return get("/onboarding", options);
}

/** GET /onboarding/check：只读自检，逐项报告状态。 */
export function getOnboardingCheck(
  options: RequestOptions = {},
): Promise<OnboardingCheckResponse> {
  return get("/onboarding/check", options);
}

/** POST /onboarding/state：记录完成/跳过/重开或样例步骤。 */
export function updateOnboardingState(
  input: OnboardingStateInput,
  options: RequestOptions = {},
): Promise<Onboarding> {
  return post("/onboarding/state", input, options);
}

/** GET /events 的 SSE 地址；after 为上次收到的事件 id（可空）。 */
export function eventsUrl(after?: string): string {
  return apiUrl("/events", { after });
}
