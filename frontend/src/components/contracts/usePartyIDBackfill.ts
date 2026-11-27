// 相对方证件号补录提示（te-48hkl 拆分自 ContractFormModal）：v0.2 起新建
// 档案必须填证件号；选中（或打开）缺少证件号的存量档案时提示一次补录，
// 补录后永久有效。同一档案在同一浏览器只提示一次，不打断合同录入。
import { watch } from "vue";
import type { Party } from "../../api/types";
import { toast } from "../../bridge/app";

export function usePartyIDBackfill(
  partyID: () => string,
  parties: () => Party[],
): void {
  watch(
    [partyID, parties],
    () => {
      const match = parties().find((item) => item.id === partyID());
      if (!match || (match.id_number ?? "").trim() !== "") {
        return;
      }
      const key = `tengshou:party-id-backfill:${match.id}`;
      try {
        if (window.localStorage.getItem(key) === "1") {
          return;
        }
        window.localStorage.setItem(key, "1");
      } catch {
        /* 隐私模式禁用 localStorage 时仍提示，但不落标记 */
      }
      toast(
        `相对方「${match.name}」还没有证件号，请在「相对方档案」页补录一次，补录后长期有效`,
        true,
      );
    },
    { immediate: true },
  );
}
