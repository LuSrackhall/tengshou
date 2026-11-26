// 合同表单分组（te-bzqhx）：把内置控件与动态字段归入租赁业务分区。
//
// 规则：
//   - 内置字段固定归组，不读取字段定义里的 group：
//     编号/年度/名称/类型/状态/签订日期 → 基本信息；
//     租赁起止/生效/到期 → 租期；金额 → 租金与押金；
//   - 甲方/乙方/房产三个档案选择控件固定放在基本信息开头，沿用迁移前顺序；
//   - 动态字段按 field.group 归位；未分组（空/缺省）落「其他约定」；
//   - 空组不渲染；组内顺序沿用字段列表现有顺序（sort_order）。
import type { FieldDefinition } from "../../api/types";

export const CONTRACT_GROUP_BASIC = "基本信息";
export const CONTRACT_GROUP_LEASE = "租期";
export const CONTRACT_GROUP_MONEY = "租金与押金";
export const CONTRACT_GROUP_FEES = "费用与违约";
export const CONTRACT_GROUP_OTHER = "其他约定";

/** 字段管理页的分组预设；用户也可自定义分组名。 */
export const CONTRACT_GROUP_PRESETS = [
  CONTRACT_GROUP_BASIC,
  CONTRACT_GROUP_LEASE,
  CONTRACT_GROUP_MONEY,
  CONTRACT_GROUP_FEES,
  CONTRACT_GROUP_OTHER,
] as const;

/** 内置字段的固定分组（内置字段在字段页不可改分组）。 */
const BUILT_IN_GROUPS: Record<string, string> = {
  contract_no: CONTRACT_GROUP_BASIC,
  year: CONTRACT_GROUP_BASIC,
  title: CONTRACT_GROUP_BASIC,
  type: CONTRACT_GROUP_BASIC,
  status: CONTRACT_GROUP_BASIC,
  signed_on: CONTRACT_GROUP_BASIC,
  party_b: CONTRACT_GROUP_BASIC,
  lease_start: CONTRACT_GROUP_LEASE,
  lease_end: CONTRACT_GROUP_LEASE,
  valid_from: CONTRACT_GROUP_LEASE,
  valid_to: CONTRACT_GROUP_LEASE,
  amount: CONTRACT_GROUP_MONEY,
};

/** 表单条目：甲方/乙方/房产是档案选择控件，不是字段定义。 */
export type ContractFormEntry =
  | { kind: "party_a" }
  | { kind: "party_b" }
  | { kind: "asset" }
  | { kind: "field"; field: FieldDefinition };

export interface ContractFormGroup {
  name: string;
  entries: ContractFormEntry[];
}

/** 内置字段固定分组；动态字段读取 group，空值落「其他约定」。 */
export function contractFieldGroup(field: FieldDefinition): string {
  if (field.built_in) {
    return BUILT_IN_GROUPS[field.key] ?? CONTRACT_GROUP_OTHER;
  }
  const group = (field.group ?? "").trim();
  return group === "" ? CONTRACT_GROUP_OTHER : group;
}

/**
 * 按分组组织合同表单条目。乙方内置字段由 PartyBPicker 渲染，跳过其定义；
 * 空组不会出现在结果里。
 */
export function groupContractFields(fields: FieldDefinition[]): ContractFormGroup[] {
  const groups = new Map<string, ContractFormEntry[]>();
  const ensure = (name: string): ContractFormEntry[] => {
    let entries = groups.get(name);
    if (!entries) {
      entries = [];
      groups.set(name, entries);
    }
    return entries;
  };

  ensure(CONTRACT_GROUP_BASIC).push(
    { kind: "party_a" },
    { kind: "party_b" },
    { kind: "asset" },
  );
  for (const field of fields) {
    if (field.key === "party_b") {
      continue;
    }
    ensure(contractFieldGroup(field)).push({ kind: "field", field });
  }

  const presetIndex = new Map<string, number>(
    CONTRACT_GROUP_PRESETS.map((name, index) => [name, index]),
  );
  const customIndex = new Map<string, number>();
  for (const name of groups.keys()) {
    if (!presetIndex.has(name)) {
      customIndex.set(name, customIndex.size);
    }
  }
  // 预设顺序 → 自定义分组（按首次出现的字段顺序）→「其他约定」垫底。
  const rank = (name: string): [number, number] => {
    if (name === CONTRACT_GROUP_OTHER) {
      return [2, 0];
    }
    const preset = presetIndex.get(name);
    if (preset !== undefined) {
      return [0, preset];
    }
    return [1, customIndex.get(name) ?? 0];
  };
  return [...groups.entries()]
    .sort(([a], [b]) => {
      const [rankA, indexA] = rank(a);
      const [rankB, indexB] = rank(b);
      return rankA - rankB || indexA - indexB;
    })
    .map(([name, entries]) => ({ name, entries }));
}

/** 条目渲染键：字段用 key，内置控件用 kind。 */
export function contractFormEntryKey(entry: ContractFormEntry): string {
  return entry.kind === "field" ? `field:${entry.field.key}` : entry.kind;
}
