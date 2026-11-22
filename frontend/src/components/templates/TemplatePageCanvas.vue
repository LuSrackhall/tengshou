<script setup lang="ts">
// 标定编辑器的一页：canvas 渲染模板 PDF + 该页的标定框图层。
// 类名（tpl-page / tpl-boxes / tpl-page-no）与迁移前 renderTemplateEditorPages
// 的输出一致；缩放变化时先取消上一笔渲染再重绘。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type {
  FieldsByEntityResponse,
  Placement,
  Seal,
} from "../../api/types";
import PlacementBox from "./PlacementBox.vue";
import { bindingLabel } from "./labels";
import {
  renderPageOnCanvas,
  type PdfDocument,
  type PdfRenderTask,
  type PdfViewport,
} from "./pdfjs";

const props = defineProps<{
  doc: PdfDocument;
  pageNumber: number;
  pageCount: number;
  scale: number;
  /** 该页的 PDF 点尺寸（scale=1 时的视口）。 */
  pageSize: PdfViewport;
  placements: Placement[];
  selectedId: string | null;
  fields: FieldsByEntityResponse;
  seals: Seal[];
}>();

const emit = defineEmits<{
  select: [id: string];
  change: [id: string, patch: { x?: number; y?: number; w?: number; h?: number }];
  remove: [id: string];
}>();

const canvasEl = ref<HTMLCanvasElement | null>(null);
let task: PdfRenderTask | null = null;
let token = 0;

const viewport = computed(() => ({
  width: props.pageSize.width * props.scale,
  height: props.pageSize.height * props.scale,
}));

const pagePlacements = computed(() =>
  props.placements.filter((placement) => placement.page === props.pageNumber),
);

async function render(): Promise<void> {
  const canvas = canvasEl.value;
  if (!canvas) {
    return;
  }
  const seq = ++token;
  task?.cancel();
  task = null;
  try {
    const page = await props.doc.getPage(props.pageNumber);
    if (seq !== token) {
      return;
    }
    task = renderPageOnCanvas(page, canvas, props.scale);
    await task.promise;
  } catch {
    // 缩放切换会取消上一笔渲染；取消异常不影响新的渲染。
  }
}

watch(
  () => [props.doc, props.pageNumber, props.scale],
  () => {
    void render();
  },
);

// 首次渲染要等 canvas 真正挂载（setup 期的 ref 还是 null）。
onMounted(() => {
  void render();
});

onBeforeUnmount(() => {
  token += 1;
  task?.cancel();
  task = null;
});
</script>

<template>
  <div
    class="tpl-page"
    :data-page="pageNumber"
    :style="{ width: `${viewport.width}px`, height: `${viewport.height}px` }"
  >
    <canvas ref="canvasEl" />
    <div class="tpl-boxes">
      <PlacementBox
        v-for="placement in pagePlacements"
        :key="placement.id"
        :placement="placement"
        :scale="scale"
        :page-width="pageSize.width"
        :page-height="pageSize.height"
        :selected="selectedId === placement.id"
        :label="bindingLabel(placement, fields, seals)"
        @select="emit('select', placement.id)"
        @change="(patch) => emit('change', placement.id, patch)"
        @remove="emit('remove', placement.id)"
      />
    </div>
    <div class="tpl-page-no">第 {{ pageNumber }} / {{ pageCount }} 页</div>
  </div>
</template>
