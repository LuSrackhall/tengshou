// 房产资产档案端点：列表 / 详情 / 新建 / 更新 / 删除。
//
//   GET/POST       /assets
//   GET/PUT/DELETE /assets/{id}
//
// 停用/启用走 PUT（enabled 字段），与字段归档不同：资产停用保留档案数据。

import {
  del,
  encodePathSegment,
  get,
  post,
  put,
  type RequestOptions,
} from "../client";
import type {
  Asset,
  AssetInput,
  AssetsQuery,
  DeletedResponse,
  ItemWarningsResponse,
  List,
} from "../types";

const enc = encodePathSegment;

/** GET /assets：分页 + 关键词（名称/地址/房号）+ 启用状态过滤。 */
export function listAssets(
  query: AssetsQuery = {},
  options: RequestOptions = {},
): Promise<List<Asset>> {
  return get("/assets", { ...options, query });
}

/** GET /assets/{id}。 */
export function getAsset(
  id: string,
  options: RequestOptions = {},
): Promise<Asset> {
  return get(`/assets/${enc(id)}`, options);
}

/** POST /assets：新建房产档案；warnings 为同名等非阻断提示。 */
export function createAsset(
  input: AssetInput,
  options: RequestOptions = {},
): Promise<ItemWarningsResponse<Asset>> {
  return post("/assets", input, options);
}

/** PUT /assets/{id}。 */
export function updateAsset(
  id: string,
  input: AssetInput,
  options: RequestOptions = {},
): Promise<ItemWarningsResponse<Asset>> {
  return put(`/assets/${enc(id)}`, input, options);
}

/** DELETE /assets/{id}。界面只提供停用；删除保留给 API/后续珠子。 */
export function deleteAsset(
  id: string,
  options: RequestOptions = {},
): Promise<DeletedResponse> {
  return del(`/assets/${enc(id)}`, options);
}
