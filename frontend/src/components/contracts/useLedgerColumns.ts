// 台账列设置的组件状态（te-iuns4）：把「全部 in_ledger 字段 → 本机勾选 →
// 可见列」与弹层开关收在一处，ContractsView 只负责接线；偏好读写仍在
// ledgerColumns.ts（localStorage，不入库、不进审计）。
import { computed, ref, type Ref } from "vue";
import type { FieldDefinition } from "../../api/types";
import {
  DEFAULT_LEDGER_COLUMN_KEYS,
  readLedgerColumnKeys,
  resetLedgerColumnKeys,
  saveLedgerColumnKeys,
} from "./ledgerColumns";

export function useLedgerColumns(fields: Ref<FieldDefinition[]>) {
  /** 全部可作为台账列的字段（in_ledger），列设置弹层也列这些。 */
  const ledgerFields = computed(() => fields.value.filter((field) => field.in_ledger));
  const selectedKeys = ref<string[]>(readLedgerColumnKeys());
  /** 台账实际渲染的列：字段顺序不变，只按本机勾选过滤。 */
  const tableFields = computed(() => {
    const selected = new Set(selectedKeys.value);
    return ledgerFields.value.filter((field) => selected.has(field.key));
  });
  const dialogOpen = ref(false);

  function openDialog(): void {
    dialogOpen.value = true;
  }

  function closeDialog(): void {
    dialogOpen.value = false;
  }

  function updateSelected(keys: string[]): void {
    selectedKeys.value = keys;
    saveLedgerColumnKeys(keys);
  }

  function restoreDefaults(): void {
    resetLedgerColumnKeys();
    selectedKeys.value = [...DEFAULT_LEDGER_COLUMN_KEYS];
  }

  return {
    ledgerFields,
    selectedKeys,
    tableFields,
    dialogOpen,
    openDialog,
    closeDialog,
    updateSelected,
    restoreDefaults,
  };
}
