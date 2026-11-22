// 标定位置的创建与保存载荷：逐条对应迁移前 addTemplatePlacement 与
// saveTemplateMapping 的默认值与坐标约定（PDF 点、1 起页码、字号 0=10.5pt）。
import type { Placement } from "../../api/types";
import { clampNumber, newPlacementId, roundPt } from "./format";
import type { AddPlacementPayload } from "./types";

export interface CreatePlacementResult {
  placement?: Placement;
  /** 与迁移前一致的校验提示；无错误时为空。 */
  error?: string;
}

function isParty(value: string): value is "party_a" | "party_b" {
  return value === "party_a" || value === "party_b";
}

/** 新建标定框：字段/签名/公章的默认尺寸与校验顺序照旧。 */
export function createPlacement(
  payload: AddPlacementPayload,
  pageSize: { width: number; height: number },
  existing: Placement[],
): CreatePlacementResult {
  const page = 1;
  const placement: Placement = {
    id: newPlacementId(),
    page,
    x: 60,
    y: 100 + (existing.filter((item) => item.page === page).length % 12) * 34,
    w: 180,
    h: 22,
    type: payload.kind,
    font_size: 0,
    align: "left",
  };
  if (payload.kind === "field") {
    if (!payload.field) {
      return { error: "请选择一个字段" };
    }
    placement.field = payload.field;
    placement.align = "left";
  } else if (payload.kind === "signature") {
    if (!payload.party) {
      return { error: "请选择签名方（甲方或乙方）" };
    }
    if (isParty(payload.party)) {
      placement.party = payload.party;
    }
    placement.w = 130;
    placement.h = 50;
  } else {
    placement.seal_mode = payload.sealMode === "seal" ? "seal" : "contract";
    if (isParty(payload.party)) {
      placement.party = payload.party;
    }
    if (placement.seal_mode === "seal") {
      if (!payload.sealId) {
        return { error: "请先选择或导入公章图片" };
      }
      placement.seal_id = payload.sealId;
    }
    placement.w = 110;
    placement.h = 110;
  }
  placement.x = roundPt(clampNumber(placement.x, 0, pageSize.width - placement.w));
  placement.y = roundPt(clampNumber(placement.y, 0, pageSize.height - placement.h));
  return { placement };
}

/** 保存载荷：保留全部坐标与绑定字段，空字符串 / 缺省值的序列化与迁移前一致。 */
export function mappingPlacements(placements: Placement[]): Placement[] {
  return placements.map((placement) => ({
    id: placement.id,
    page: placement.page,
    x: placement.x,
    y: placement.y,
    w: placement.w,
    h: placement.h,
    type: placement.type,
    field: placement.field ?? "",
    ...(placement.party ? { party: placement.party } : {}),
    ...(placement.seal_mode ? { seal_mode: placement.seal_mode } : {}),
    seal_id: placement.seal_id ?? "",
    font_size: placement.font_size ?? 0,
    align: placement.align ?? "left",
  }));
}
