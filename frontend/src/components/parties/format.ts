// 相对方域的展示格式化：共享函数已集中到 ui/format，这里只保留本域专用的
// 台账单元格取值，并重导出原有名字以保持调用点不变。

import type { FieldDefinition, Party } from "../../api/types";
import { formatDynamic, kindLabel, money, statusLabel } from "../../ui/format";

export { formatDynamic, kindLabel, money, statusLabel };

/** 台账单元格值：内置列走一级字段，其余读 data（app.js partyFieldValue）。 */
export function partyFieldValue(party: Party, field: FieldDefinition): string {
  switch (field.key) {
    case "name":
      return party.name;
    case "kind":
      return kindLabel(party.kind);
    case "id_number":
      return party.id_number || "—";
    case "phone":
      return party.phone || "—";
    case "address":
      return party.address || "—";
    case "note":
      return party.note || "—";
  }
  const value = party.data ? party.data[field.key] : null;
  return formatDynamic(field, value);
}
