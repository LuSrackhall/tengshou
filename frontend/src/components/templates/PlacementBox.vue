<script setup lang="ts">
// 单个标定框：拖动移动、右下角手柄缩放，类名/手柄/删除按钮与迁移前
// makeTemplateBox + startTemplateBoxDrag/Resize 一致。
// 坐标仍是 PDF 点（1 位小数），换算显示时乘以缩放比例。
import { computed, ref, type ComponentPublicInstance } from "vue";
import type { Placement } from "../../api/types";
import { clampNumber, roundPt } from "./format";

const props = defineProps<{
  placement: Placement;
  scale: number;
  /** 所在页的 PDF 点宽高（拖动/缩放的边界）。 */
  pageWidth: number;
  pageHeight: number;
  selected: boolean;
  label: string;
  /** 登记本框的真实 DOM 元素，供编辑器滚动定位；卸载时为 null。 */
  registerElement?: (element: HTMLElement | null) => void;
}>();

const emit = defineEmits<{
  select: [];
  change: [{ x?: number; y?: number; w?: number; h?: number }];
  remove: [];
}>();

const boxEl = ref<HTMLDivElement | null>(null);

/** 模板 ref 回调：既保存拖动/缩放用的元素，也登记给编辑器做滚动定位。 */
function setBoxEl(element: Element | ComponentPublicInstance | null): void {
  const boxElement = element instanceof HTMLDivElement ? element : null;
  boxEl.value = boxElement;
  props.registerElement?.(boxElement);
}

const typeClass = computed(() =>
  props.placement.type === "signature"
    ? "sig"
    : props.placement.type === "seal"
      ? "seal"
      : "",
);

const boxStyle = computed(() => ({
  left: `${props.placement.x * props.scale}px`,
  top: `${props.placement.y * props.scale}px`,
  width: `${props.placement.w * props.scale}px`,
  height: `${props.placement.h * props.scale}px`,
}));

function startGesture(event: PointerEvent, mode: "move" | "resize"): void {
  const el = boxEl.value;
  if (!el) {
    return;
  }
  event.preventDefault();
  emit("select");
  const origin = {
    x: props.placement.x,
    y: props.placement.y,
    w: props.placement.w,
    h: props.placement.h,
  };
  const startX = event.clientX;
  const startY = event.clientY;
  const move = (moveEvent: PointerEvent): void => {
    const dx = (moveEvent.clientX - startX) / props.scale;
    const dy = (moveEvent.clientY - startY) / props.scale;
    if (mode === "move") {
      emit("change", {
        x: roundPt(
          clampNumber(origin.x + dx, 0, props.pageWidth - props.placement.w),
        ),
        y: roundPt(
          clampNumber(origin.y + dy, 0, props.pageHeight - props.placement.h),
        ),
      });
      return;
    }
    emit("change", {
      w: roundPt(
        clampNumber(origin.w + dx, 6, props.pageWidth - props.placement.x),
      ),
      h: roundPt(
        clampNumber(origin.h + dy, 6, props.pageHeight - props.placement.y),
      ),
    });
  };
  const up = (): void => {
    el.removeEventListener("pointermove", move);
    el.removeEventListener("pointerup", up);
    el.removeEventListener("pointercancel", up);
  };
  el.setPointerCapture(event.pointerId);
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", up);
  el.addEventListener("pointercancel", up);
}

function onPointerDown(event: PointerEvent): void {
  startGesture(event, "move");
}

function onHandleDown(event: PointerEvent): void {
  event.stopPropagation();
  startGesture(event, "resize");
}
</script>

<template>
  <div
    :ref="setBoxEl"
    class="tpl-box"
    :class="[typeClass, { selected }]"
    :data-id="placement.id"
    :style="boxStyle"
    @pointerdown="onPointerDown"
  >
    <span class="tpl-box-label">{{ label }}</span>
    <span class="tpl-handle" @pointerdown="onHandleDown"></span>
    <button
      type="button"
      class="tpl-box-del"
      title="删除位置"
      @pointerdown.stop
      @click.stop="emit('remove')"
    >
      ×
    </button>
  </div>
</template>
