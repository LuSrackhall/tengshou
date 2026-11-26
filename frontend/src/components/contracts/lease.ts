// 合同表单的日期与默认甲方约定（te-thrth）。
//
// 租期默认一年：起租日默认今天，到期日为一年后的前一天（含首尾）。
// 甲方默认带出「最近一次使用」的单位档案，没有记录时取档案列表中的第一个
// 单位；都是可切换的默认值，最终以用户保存的合同为准。

import type { Party } from "../../api/types";

const PARTY_A_KEY = "tengshou:contract-party-a";

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

export function isoDate(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function todayISO(): string {
  return isoDate(new Date());
}

/** 起租日 + 1 年 - 1 天；非法输入返回空串。 */
export function oneYearLeaseEnd(start: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(start.trim());
  if (!match) {
    return "";
  }
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const end = new Date(year + 1, month - 1, day);
  end.setDate(end.getDate() - 1);
  return isoDate(end);
}

function readStoredPartyA(): string {
  try {
    return window.localStorage.getItem(PARTY_A_KEY) ?? "";
  } catch {
    return "";
  }
}

export function rememberPartyA(id: string): void {
  try {
    if (id) {
      window.localStorage.setItem(PARTY_A_KEY, id);
    } else {
      window.localStorage.removeItem(PARTY_A_KEY);
    }
  } catch {
    /* 隐私模式禁用 localStorage 时仅本次生效 */
  }
}

/** 新建合同的默认甲方：最近使用 → 第一个单位档案 → 空。 */
export function defaultPartyAID(parties: Party[]): string {
  const stored = readStoredPartyA();
  if (stored && parties.some((party) => party.id === stored)) {
    return stored;
  }
  return parties.find((party) => party.kind === "organization")?.id ?? "";
}
