// 字段键自动生成：新建字段时用户只需要填「字段名称」，字段键由名称派生，
// 用户不需要理解机器标识（数据存储、模板绑定、Excel 快照、MCP 都用它）。
//
// 规则与后端 internal/service/service.go 的 fieldKeyPattern
// （^[a-z][a-z0-9_]{0,62}$）保持一致：
//   - ASCII 名称 → snake_case（小写；非字母数字转下划线；去首尾下划线）；
//   - 数字开头 → 补 field_ 前缀；
//   - 中文/无法派生 → field_ + 递增序号（同实体范围）；
//   - 与同实体既有键冲突 → 自动加 _2、_3…。
// 后端仍校验格式与唯一性，前端只负责默认值始终合法、不冲突。

/** 后端字段键格式（internal/service/service.go fieldKeyPattern）。 */
export const FIELD_KEY_PATTERN = /^[a-z][a-z0-9_]{0,62}$/;

/** 字段键最大长度：首字符 + 至多 62 个 [a-z0-9_]。 */
export const FIELD_KEY_MAX_LENGTH = 63;

/** 截断到最大长度并去掉结尾下划线（下划线结尾不美观也无意义）。 */
function clampKey(key: string): string {
  return key.slice(0, FIELD_KEY_MAX_LENGTH).replace(/_+$/, "");
}

/**
 * 把纯 ASCII 名称转成 snake_case 基础键。
 * 含非 ASCII（中文等）或无法派生时返回空串，由调用方走序号兜底。
 */
export function fieldKeyBase(label: string): string {
  const trimmed = label.trim();
  if (trimmed === "" || /[^\x20-\x7e]/.test(trimmed)) {
    return "";
  }
  let base = trimmed
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  if (base === "") {
    return "";
  }
  if (/^[0-9]/.test(base)) {
    base = `field_${base}`;
  }
  return clampKey(base);
}

/**
 * 按名称生成同实体唯一的字段键。
 * `existingKeys` 必须包含同实体的全部字段键（含已归档）。
 */
export function generateFieldKey(
  label: string,
  existingKeys: readonly string[],
): string {
  const taken = new Set(existingKeys);
  const base = fieldKeyBase(label);
  if (base === "") {
    for (let seq = 1; ; seq += 1) {
      const candidate = `field_${seq}`;
      if (!taken.has(candidate)) {
        return candidate;
      }
    }
  }
  if (!taken.has(base)) {
    return base;
  }
  for (let seq = 2; ; seq += 1) {
    const suffix = `_${seq}`;
    const prefix = (base.slice(0, FIELD_KEY_MAX_LENGTH - suffix.length) || base)
      .replace(/_+$/, "");
    const candidate = `${prefix}${suffix}`;
    if (!taken.has(candidate)) {
      return candidate;
    }
  }
}
