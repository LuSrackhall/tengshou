// 标定框的编辑状态与动作：增删改、字号/对齐、选中与滚动定位。
// 与拆分前 useTemplateEditor 的同名逻辑等价；滚动定位不再用
// querySelector 查 .tpl-page/.tpl-box，改由 PlacementBox 通过
// registerPlacementElement 登记自身元素，去掉组合式函数对视图结构的耦合。
import { nextTick, ref } from "vue";
import type { Placement } from "../../api/types";
import { toast } from "../../bridge/app";
import { clonePlacements } from "./format";
import { createPlacement } from "./placements";
import type { AddPlacementPayload } from "./types";

export interface PlacementEditingOptions {
  /** 初始标定位置（props.template.placements）。 */
  initialPlacements?: Placement[];
  /** 新建标定框所在页的尺寸；该页尚未加载时为 undefined。 */
  pageSize: (page: number) => { width: number; height: number } | undefined;
  /** 任何修改（含新建）后置脏并让当前预览失效。 */
  onChanged: () => void;
}

export function usePlacementEditing(options: PlacementEditingOptions) {
  const placements = ref<Placement[]>(clonePlacements(options.initialPlacements));
  const selectedId = ref<string | null>(null);
  /** PlacementBox 登记的真实 DOM 元素（按标定位置 id），用于滚动定位。 */
  const boxElements = new Map<string, HTMLElement>();

  /** PlacementBox 的模板 ref 回调：挂载时登记，卸载时（element=null）移除。 */
  function registerPlacementElement(
    id: string,
    element: HTMLElement | null,
  ): void {
    if (element) {
      boxElements.set(id, element);
      return;
    }
    boxElements.delete(id);
  }

  /** 选中标定框；scroll=true 时滚动到框所在位置（位置列表点击用）。 */
  function selectPlacement(id: string, scroll = false): void {
    selectedId.value = id;
    if (!scroll) {
      return;
    }
    boxElements
      .get(id)
      ?.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  function patchPlacement(
    id: string,
    patch: { x?: number; y?: number; w?: number; h?: number },
  ): void {
    const placement = placements.value.find((item) => item.id === id);
    if (!placement) {
      return;
    }
    Object.assign(placement, patch);
    options.onChanged();
  }

  function removePlacement(id: string): void {
    placements.value = placements.value.filter((item) => item.id !== id);
    if (selectedId.value === id) {
      selectedId.value = null;
    }
    options.onChanged();
  }

  function addPlacement(payload: AddPlacementPayload): void {
    const pageSize = options.pageSize(payload.page);
    if (!pageSize) {
      toast(`模板 PDF 第 ${payload.page} 页尚未加载完成，无法在该页新增位置`, true);
      return;
    }
    const created = createPlacement(payload, pageSize, placements.value);
    if (!created.placement) {
      if (created.error) {
        toast(created.error, true);
      }
      return;
    }
    const placementId = created.placement.id;
    placements.value.push(created.placement);
    options.onChanged();
    selectPlacement(placementId);
    // 等新框渲染到页面上再滚动定位（迁移前是同步建 DOM）。
    void nextTick(() => selectPlacement(placementId, true));
  }

  function updateFontSize(id: string, value: number): void {
    const placement = placements.value.find((item) => item.id === id);
    if (!placement) {
      return;
    }
    const size = value > 0 ? value : 0;
    placement.font_size = size;
    options.onChanged();
    toast(
      size > 0
        ? `字号设为 ${size}pt（放不下时仍会自动缩小）`
        : "字号改为自动缩放",
    );
  }

  function updateAlign(id: string, value: string): void {
    const placement = placements.value.find((item) => item.id === id);
    if (!placement) {
      return;
    }
    placement.align = value;
    options.onChanged();
  }

  /** 保存成功后重置为服务端返回的位置列表并清空选中。 */
  function resetPlacements(items?: Placement[]): void {
    placements.value = clonePlacements(items);
    selectedId.value = null;
  }

  return {
    placements,
    selectedId,
    registerPlacementElement,
    selectPlacement,
    patchPlacement,
    removePlacement,
    addPlacement,
    updateFontSize,
    updateAlign,
    resetPlacements,
  };
}
