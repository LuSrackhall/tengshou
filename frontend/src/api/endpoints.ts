// App API typed 调用面的入口。
//
// 按域拆到 ./endpoints/*.ts（单文件 ≤ 600 行）；调用方统一从
// `api/endpoints` 引入。错误类型与底层 helper 见 ../client.ts。

export * from "./endpoints/assets";
export * from "./endpoints/onboarding";
export * from "./endpoints/records";
export * from "./endpoints/seals";
export * from "./endpoints/imports";
export * from "./endpoints/jobs";
export * from "./endpoints/templates";
export * from "./endpoints/signing";
export * from "./endpoints/system";
