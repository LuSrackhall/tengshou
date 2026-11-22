<script setup lang="ts">
// 公章预览弹窗：A4 页面示意、盖章宽度/水平位置调整与模板占位复制。
// 结构与交互逐条对应存量 app.js openSealPreview。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { sealImageUrl } from "../../api/endpoints";
import type { Party, Seal } from "../../api/types";
import { toast } from "../../bridge/app";
import { copyText, sealOwner, sealPlacementSnippet } from "./format";

const props = defineProps<{
  seal: Seal;
  parties: Party[];
}>();

const emit = defineEmits<{
  close: [];
}>();

const WIDTHS = [30, 35, 40, 45, 50] as const;

const width = ref<number>(40);
const align = ref<string>("center");

const owner = computed(() => sealOwner(props.seal, props.parties));
const imageURL = computed(() => sealImageUrl(props.seal.id));
const rowStyle = computed(() => ({
  justifyContent:
    align.value === "left" ? "flex-start" : align.value === "right" ? "flex-end" : "center",
}));
const snippet = computed(() =>
  sealPlacementSnippet(props.seal, width.value, align.value),
);

async function copySnippet(): Promise<void> {
  const ok = await copyText(snippet.value);
  toast(ok ? "模板占位已复制" : "复制失败，请手动选择文本", !ok);
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

onMounted(() => {
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop">
      <div class="modal wide" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>公章预览 · {{ seal.name }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div class="seal-preview">
            <div class="seal-a4">
              <div class="seal-a4-page">
                <div id="sealA4Row" class="seal-a4-row" :style="rowStyle">
                  <img
                    id="sealA4Img"
                    :src="imageURL"
                    :alt="seal.name"
                    :style="{ width: `${width}mm` }"
                  >
                </div>
                <div class="seal-a4-label">A4 页面 · 盖章位置示意</div>
              </div>
              <p class="hint">这里只示意模板中的水平位置与图片大小，方便调整模板占位；实际换行与分页由模板排版决定。</p>
            </div>
            <div class="seal-preview-side">
              <div class="seal-meta">{{ owner }} · {{ seal.width_px }}×{{ seal.height_px }}px</div>
              <div class="seal-meta mono">sha256 {{ seal.sha256 }}</div>
              <div class="seal-meta">已关联 {{ seal.contract_count || 0 }} 份合同</div>
              <div v-if="seal.note" class="seal-meta">说明：{{ seal.note }}</div>
              <div class="field">
                <label for="sealWidth">盖章宽度</label>
                <select id="sealWidth" v-model="width">
                  <option v-for="w in WIDTHS" :key="w" :value="w">{{ w }} mm</option>
                </select>
              </div>
              <div class="field">
                <label for="sealAlign">水平位置</label>
                <select id="sealAlign" v-model="align">
                  <option value="left">靠左</option>
                  <option value="center">居中</option>
                  <option value="right">靠右</option>
                </select>
              </div>
              <div class="field">
                <label>模板占位</label>
                <pre id="sealSnippet" class="snippet">{{ snippet }}</pre>
                <span class="help">把这段复制到模板的盖章位置；公章属于乙方时把 <code>.PartyA</code> 换成 <code>.PartyB</code>。换模板时按新模板里的位置重新落位，图片本身可以继续复用。</span>
              </div>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">关闭</button>
          <button type="button" class="primary" @click="copySnippet">复制模板占位</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
