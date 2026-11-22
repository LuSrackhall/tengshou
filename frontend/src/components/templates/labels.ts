// 标定位置的绑定文案与字段下拉分组：逐条对应存量 app.js 的
// templateFieldOptionsHTML / templateSealOptionsHTML / templateBindingLabel。
import type {
  FieldDefinition,
  FieldsByEntityResponse,
  Placement,
  Seal,
} from "../../api/types";

export interface FieldOption {
  value: string;
  label: string;
}

export interface FieldOptionGroup {
  label: string;
  options: FieldOption[];
}

function pushGroup(
  groups: FieldOptionGroup[],
  label: string,
  fields: FieldDefinition[],
  prefix: string,
): void {
  const options: FieldOption[] = [];
  for (const field of fields) {
    if (field.archived) {
      continue;
    }
    // 非内置的图片字段不进模板（迁移前的过滤规则）。
    if (!field.built_in && field.type === "image") {
      continue;
    }
    const key = field.built_in
      ? `${prefix}.${field.key}`
      : `${prefix}.data.${field.key}`;
    options.push({ value: key, label: `${label} · ${field.label}` });
  }
  if (options.length) {
    groups.push({ label, options });
  }
}

/** 字段下拉分组：合同 / 甲方 / 乙方；顺序与 optgroup 输出一致。 */
export function fieldOptionGroups(
  fields: FieldsByEntityResponse,
): FieldOptionGroup[] {
  const groups: FieldOptionGroup[] = [];
  pushGroup(groups, "合同", fields.contract, "contract");
  pushGroup(groups, "甲方", fields.party, "party_a");
  pushGroup(groups, "乙方", fields.party, "party_b");
  return groups;
}

/** 公章下拉文案；已关联单位的追加后缀与迁移前一致。 */
export function sealOptionLabel(seal: Seal): string {
  return `${seal.name}${seal.party_id ? "（已关联单位）" : ""}`;
}

function roleLabel(prefix: string): string {
  if (prefix === "party_a") {
    return "甲方";
  }
  if (prefix === "party_b") {
    return "乙方";
  }
  return "合同";
}

/** 标定框与列表里显示的绑定文案。 */
export function bindingLabel(
  placement: Placement,
  fields: FieldsByEntityResponse,
  seals: Seal[],
): string {
  if (placement.type === "signature") {
    return `签名 · ${placement.party === "party_a" ? "甲方" : "乙方"}`;
  }
  if (placement.type === "seal") {
    if (placement.seal_mode === "seal" && placement.seal_id) {
      const seal = seals.find((item) => item.id === placement.seal_id);
      return `公章 · ${seal ? seal.name : "指定图片"}`;
    }
    const party =
      placement.party === "party_a"
        ? "（甲方）"
        : placement.party === "party_b"
          ? "（乙方）"
          : "";
    return `公章 · 合同关联/单位公章${party}`;
  }
  const key = placement.field ?? "";
  const prefix = key.split(".")[0] ?? "";
  const dataKey = key.includes(".data.")
    ? (key.split(".data.")[1] ?? "")
    : (key.split(".")[1] ?? "");
  const list = prefix === "contract" ? fields.contract : fields.party;
  const field = list.find((item) => item.key === dataKey);
  return `${roleLabel(prefix)} · ${field ? field.label : key}`;
}
