// 腾手 App API 契约类型入口。
//
// 阶段 1 起按域拆分到 ./types/*.ts：单文件 ≤ 600 行的结构预算要求，
// 同时让每个域 bead 只改自己那份。调用方仍统一从 `api/types` 引入，
// 不要直接引用子模块路径。
//
// 与 Go 侧的对照清单见 frontend/src/api/README.md；契约变更流程见
// docs/frontend.md 的「API 客户端与类型」一节。
//
// 序列化约定：
//   - Go int / float64 → number；time.Time → RFC3339 字符串；
//   - omitempty 字段 → 可选属性；
//   - Go 的 nil slice 在 JSON 里是 null，类型按数组声明；消费方对
//     可能为空的列表（如 warnings/issues）应使用 `?? []` 兜底展示。
//
// 禁止用 any 兜底：动态字段值用 Record<string, unknown>；确需例外时
// 必须在调用点写明原因。

export type * from "./types/bootstrap";
export type * from "./types/common";
export type * from "./types/documents";
export type * from "./types/imports";
export type * from "./types/onboarding";
export type * from "./types/records";
export type * from "./types/requests";
export type * from "./types/settings";
export type * from "./types/signing";
export type * from "./types/templates";

// 运行时清单是值，barrel 的 `export type *` 不会导出它。
export { CHANGE_ENTITIES } from "./types/common";
