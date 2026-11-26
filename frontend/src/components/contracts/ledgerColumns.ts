// 合同台账列设置（te-iuns4）：本机显示偏好，存 localStorage，不入库、不进
// 审计。默认只勾关键列；动态字段默认不勾。未勾选的字段仍在编辑表单、模板
// 与 Excel 导出中，搜索/筛选/批量选择/到期徽标与其他台账操作均不受影响，
// 服务端 in_ledger 语义不变。

/** localStorage 键：勾选的台账列字段键（JSON 字符串数组）。 */
export const LEDGER_COLUMNS_KEY = "tengshou.contractLedgerColumns";

/**
 * 默认关键列：合同编号/年度/合同名称/合同类型/相对方/金额/签订日期/
 * 租赁起租日/租赁到期日/状态。按字段定义的排序展示，与这里的顺序无关。
 */
export const DEFAULT_LEDGER_COLUMN_KEYS: readonly string[] = [
  "contract_no",
  "year",
  "title",
  "type",
  "party_b",
  "amount",
  "signed_on",
  "lease_start",
  "lease_end",
  "status",
];

/** 读取本机偏好；未保存、内容损坏或存储不可用时回退默认关键列。 */
export function readLedgerColumnKeys(): string[] {
  try {
    const raw: unknown = JSON.parse(
      window.localStorage.getItem(LEDGER_COLUMNS_KEY) ?? "null",
    );
    if (Array.isArray(raw)) {
      return raw.filter((key): key is string => typeof key === "string");
    }
  } catch {
    /* 存储不可用或内容损坏：回退默认 */
  }
  return [...DEFAULT_LEDGER_COLUMN_KEYS];
}

/** 保存勾选；隐私模式禁用 localStorage 时仅本次会话生效。 */
export function saveLedgerColumnKeys(keys: string[]): void {
  try {
    window.localStorage.setItem(LEDGER_COLUMNS_KEY, JSON.stringify(keys));
  } catch {
    /* 隐私模式禁用 localStorage 时仅本次生效 */
  }
}

/** 「恢复默认」：清掉本机偏好，回到默认关键列。 */
export function resetLedgerColumnKeys(): void {
  try {
    window.localStorage.removeItem(LEDGER_COLUMNS_KEY);
  } catch {
    /* 同上 */
  }
}
