// 房产资产档案契约。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/model/model.go     Asset / AssetTypeHouse / LeaseStatus*
//   internal/service/assets.go  AssetInput
//
// v0.2 只做房产（asset_type=house）；在租/空置（lease_status）由后端按
// 合同派生，前端只读展示，不提供手工修改。

import type { ISODateTime } from "./common";

export type AssetType = "house";

/** 租赁状态：存在生效中且未终止的合同=在租，否则空置。 */
export type LeaseStatus = "rented" | "vacant";

export interface Asset {
  id: string;
  asset_type: AssetType;
  name: string;
  address?: string;
  room_no?: string;
  /** 选填；未填时后端返回缺省（不出现该字段）。 */
  area_sqm?: number;
  note?: string;
  enabled: boolean;
  lease_status: LeaseStatus;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}
