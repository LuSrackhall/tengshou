// 首屏引导接口契约：GET /app/api/v1/bootstrap。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/httpapi/appapi.go   handleAppBootstrap 的响应 map
//   internal/model/model.go      Stats / YearCount
//   internal/httpapi/onboarding.go  onboardingPayload

import type { ISODateTime, Stats, YearCount } from "./common";
import type { Onboarding } from "./onboarding";

export interface Bootstrap {
  version: string;
  schema_version: number;
  operator: string;
  current_year: number;
  years: YearCount[];
  stats: Stats;
  mcp_write_enabled: boolean;
  onboarding: Onboarding;
  now: ISODateTime;
}
