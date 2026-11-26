// 房产表单的取值与提交：
//   - 控件值统一按字符串处理（input / textarea 的 value 语义）；
//   - 面积留空提交 null（清空选填面积），负数由后端校验拦截；
//   - asset_type 固定 house（v0.2 只有房产），启用状态不在表单里改。

import type { Asset, AssetInput } from "../../api/types";

export interface AssetFormValues {
  name: string;
  address: string;
  room_no: string;
  area: string;
  note: string;
}

/** 编辑/新建时的控件初始值。 */
export function assetControlValues(asset: Asset | null): AssetFormValues {
  return {
    name: asset?.name ?? "",
    address: asset?.address ?? "",
    room_no: asset?.room_no ?? "",
    area: asset?.area_sqm === undefined ? "" : String(asset.area_sqm),
    note: asset?.note ?? "",
  };
}

/** 表单值转 AssetInput。空面积用 undefined 表示「清空」。 */
export function collectAssetInput(values: AssetFormValues): AssetInput {
  // <input type="number"> 的 v-model 在 Vue 里会把输入转成 number，
  // 这里统一按字符串归一，避免对数字调用 trim()。
  const areaText = String(values.area ?? "").trim();
  const input: AssetInput = {
    asset_type: "house",
    name: values.name.trim(),
    address: values.address.trim(),
    room_no: values.room_no.trim(),
    note: values.note.trim(),
  };
  if (areaText !== "") {
    const area = Number(areaText);
    input.area_sqm = Number.isFinite(area) ? area : undefined;
  }
  return input;
}

/** 停用/启用只切换 enabled，其余字段按当前档案回填，避免清空已有信息。 */
export function toggleAssetInput(asset: Asset, enabled: boolean): AssetInput {
  return {
    asset_type: asset.asset_type,
    name: asset.name,
    address: asset.address ?? "",
    room_no: asset.room_no ?? "",
    area_sqm: asset.area_sqm,
    note: asset.note ?? "",
    enabled,
  };
}
