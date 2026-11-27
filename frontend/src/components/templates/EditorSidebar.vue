<script setup lang="ts">
// 标定编辑器右侧面板：新增位置、位置列表、选中位置编辑、影响范围/体检、
// 合同选择、预览与生成。DOM 结构与类名逐条对应迁移前 openTemplateEditor
// 里的 aside.tpl-side；业务动作交给 TemplateEditor。
import { computed, ref, watch } from "vue";
import type {
  Contract,
  FieldsByEntityResponse,
  JobSnapshot,
  OverlayWarning,
  Placement,
  PlacementOverlap,
  Seal,
} from "../../api/types";
import ProgressPanel from "../common/ProgressPanel.vue";
import { bindingLabel, fieldOptionGroups, sealOptionLabel } from "./labels";
import type {
  AddPlacementPayload,
  CheckView,
  GeneratedResult,
} from "./types";
import type { PlaceholderScanView } from "./usePlaceholderScan";

const props = defineProps<{
  placements: Placement[];
  selectedId: string | null;
  /** 模板总页数（PDF 已加载时以页数为准），用于「新增位置」的页码选择。 */
  pageCount: number;
  fields: FieldsByEntityResponse;
  seals: Seal[];
  contracts: Contract[];
  contractsLoaded: boolean;
  contractId: string;
  usageText: string;
  check: CheckView;
  /** 模板文字中的【…】/下划线占位符提示（只提示，不阻断）。 */
  placeholderScan: PlaceholderScanView;
  /** 字段值白底遮盖开关（随模板版本保存）。 */
  maskFieldValues: boolean;
  /** 最近一次保存返回的公章/签名框与字段框重叠提醒。 */
  overlaps: PlacementOverlap[];
  previewStatusText: string;
  previewStatusClass: string;
  canGenerate: boolean;
  /** 正在运行的合同预览/生成任务快照（阶段 + 已用秒数）；空闲时 null。 */
  activeJob: JobSnapshot<unknown> | null;
  /** null 表示尚未预览过（警告区为空）。 */
  warnings: OverlayWarning[] | null;
  result: GeneratedResult | null;
}>();

const emit = defineEmits<{
  add: [payload: AddPlacementPayload];
  select: [id: string];
  remove: [id: string];
  "update-font-size": [id: string, value: number];
  "update-align": [id: string, value: string];
  "update:contractId": [id: string];
  "update:maskFieldValues": [value: boolean];
  "preview-sample": [];
  "preview-contract": [];
  generate: [];
  "open-result": [];
  "download-result": [];
}>();

const kind = ref<Placement["type"]>("field");
/** 新框所在页码（1 起）；页数变化后若超出范围则回落到第 1 页。 */
const page = ref(1);
const fieldKey = ref("");
const party = ref("");
const sealMode = ref("contract");
const sealId = ref("");

const fieldGroups = computed(() => fieldOptionGroups(props.fields));

const selected = computed<Placement | null>(
  () => props.placements.find((item) => item.id === props.selectedId) ?? null,
);

/** 有重叠提醒的位置 id（公章/签名与字段两端都标出来）。 */
const overlapIds = computed<Set<string>>(() => {
  const ids = new Set<string>();
  for (const overlap of props.overlaps) {
    ids.add(overlap.a);
    ids.add(overlap.b);
  }
  return ids;
});

const checkClass = computed(() => {
  switch (props.check.kind) {
    case "ok":
      return "tpl-status ok";
    case "issues":
      return "tpl-warnings";
    case "error":
      return "tpl-status warn";
    default:
      return "tpl-status";
  }
});

watch(
  fieldGroups,
  (groups) => {
    const values = groups.flatMap((group) => group.options.map((option) => option.value));
    if (values.includes(fieldKey.value)) {
      return;
    }
    fieldKey.value = values[0] ?? "";
  },
  { immediate: true },
);

watch(
  () => props.pageCount,
  (count) => {
    if (count > 0 && page.value > count) {
      page.value = 1;
    }
  },
);

watch(
  () => props.seals,
  (seals) => {
    if (seals.length && !seals.some((seal) => seal.id === sealId.value)) {
      sealId.value = seals[0]?.id ?? "";
    }
  },
  { immediate: true },
);

function labelOf(placement: Placement): string {
  return bindingLabel(placement, props.fields, props.seals);
}

function submitAdd(): void {
  emit("add", {
    kind: kind.value,
    page: page.value,
    field: kind.value === "field" ? fieldKey.value : "",
    party: party.value,
    sealMode: kind.value === "seal" ? sealMode.value : "",
    sealId: kind.value === "seal" && sealMode.value === "seal" ? sealId.value : "",
  });
}

function onFontSize(event: Event): void {
  if (!selected.value) {
    return;
  }
  const input = event.target as HTMLInputElement;
  emit("update-font-size", selected.value.id, Number(input.value) || 0);
}

function onAlign(event: Event): void {
  if (!selected.value) {
    return;
  }
  emit("update-align", selected.value.id, (event.target as HTMLSelectElement).value);
}

function onContractChange(event: Event): void {
  emit("update:contractId", (event.target as HTMLSelectElement).value);
}

function onMaskChange(event: Event): void {
  emit(
    "update:maskFieldValues",
    (event.target as HTMLInputElement).checked,
  );
}
</script>

<template>
  <aside class="tpl-side">
    <h3>新增位置</h3>
    <div class="tpl-add">
      <div class="field">
        <label for="tplAddPage">页码</label>
        <select id="tplAddPage" v-model.number="page">
          <option v-for="n in Math.max(pageCount, 1)" :key="n" :value="n">
            第 {{ n }} 页
          </option>
        </select>
      </div>
      <div class="field">
        <label for="tplAddKind">类型</label>
        <select id="tplAddKind" v-model="kind">
          <option value="field">字段文字</option>
          <option value="signature">手写签名</option>
          <option value="seal">公章图片</option>
        </select>
      </div>
      <div v-show="kind === 'field'" class="field">
        <label for="tplAddField">字段</label>
        <select id="tplAddField" v-model="fieldKey">
          <optgroup
            v-for="group in fieldGroups"
            :key="group.label"
            :label="group.label"
          >
            <option v-for="option in group.options" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </optgroup>
        </select>
      </div>
      <div v-show="kind === 'signature' || kind === 'seal'" class="field">
        <label for="tplAddParty">签名方 / 公章所属方</label>
        <select id="tplAddParty" v-model="party">
          <option value="">（自动 / 不限）</option>
          <option value="party_b">乙方</option>
          <option value="party_a">甲方</option>
        </select>
      </div>
      <div v-show="kind === 'seal'" class="field">
        <label for="tplAddSealMode">公章来源</label>
        <select id="tplAddSealMode" v-model="sealMode">
          <option value="contract">合同关联 / 单位公章（自动）</option>
          <option value="seal">指定公章图片</option>
        </select>
      </div>
      <div v-show="kind === 'seal' && sealMode === 'seal'" class="field">
        <label for="tplAddSeal">公章图片</label>
        <select id="tplAddSeal" v-model="sealId">
          <option v-if="!seals.length" value="">（还没有公章图片）</option>
          <option v-for="seal in seals" :key="seal.id" :value="seal.id">
            {{ sealOptionLabel(seal) }}
          </option>
        </select>
      </div>
      <button type="button" id="tplAddPlace" @click="submitAdd">添加位置</button>
      <span class="help">添加后拖动框到目标位置，右下角可缩放大小。每次保存都会生成新版本。</span>
    </div>

    <template v-if="placeholderScan.kind === 'ready' && placeholderScan.scan.total > 0">
      <h3>占位符提示</h3>
      <div class="tpl-hint">
        <p>
          模板文字里发现 {{ placeholderScan.scan.total }} 个疑似占位符（【…】{{ placeholderScan.scan.bracketCount }} 个、连续下划线 {{ placeholderScan.scan.underscoreCount }} 个）。
          套印文字会直接盖在模板上：请为每个占位符预留足够空白，或把字段框标在占位符上并开启下方「字段值白底遮盖」。
        </p>
        <ul>
          <li v-for="example in placeholderScan.scan.examples" :key="example">{{ example }}</li>
        </ul>
        <p class="tpl-hint-note">仅提示，不影响保存、预览与生成。</p>
      </div>
    </template>
    <p v-else-if="placeholderScan.kind === 'error'" class="tpl-status">
      模板文字扫描失败：{{ placeholderScan.text }}（不影响标定与生成）
    </p>

    <h3>位置列表</h3>
    <div id="tplPlacementList" class="tpl-list">
      <div v-if="!placements.length" class="tpl-list-empty">
        还没有标定位置。在上方选择字段/签名/公章后点击「添加位置」。
      </div>
      <div
        v-for="placement in placements"
        :key="placement.id"
        class="tpl-list-item"
        :class="{
          selected: placement.id === selectedId,
          overlap: overlapIds.has(placement.id),
        }"
        :data-id="placement.id"
        @click="emit('select', placement.id)"
      >
        <span class="grow">{{ labelOf(placement) }}</span>
        <span
          v-if="overlapIds.has(placement.id)"
          class="tpl-overlap-badge"
          title="与字段框或公章/签名框重叠"
        >⚠</span>
        <span class="tpl-kind">第 {{ placement.page }} 页</span>
        <button
          type="button"
          data-action="remove"
          title="删除"
          @click.stop="emit('remove', placement.id)"
        >
          ×
        </button>
      </div>
    </div>

    <template v-if="overlaps.length">
      <h3>重叠提醒</h3>
      <div class="tpl-warnings">
        <div
          v-for="overlap in overlaps"
          :key="`${overlap.a}-${overlap.b}`"
          class="tpl-warning tpl-overlap"
          role="button"
          tabindex="0"
          :title="`第 ${overlap.page} 页：点击定位到公章/签名框`"
          @click="emit('select', overlap.a)"
          @keydown.enter="emit('select', overlap.a)"
        >
          {{ overlap.message }}（点击定位）
        </div>
      </div>
      <p class="tpl-hint-note">重叠只提示，不阻止保存；可移动框后再次保存。</p>
    </template>

    <div id="tplSelectedPanel">
      <div v-if="!selected" class="tpl-status">
        点击页面上的框或上方列表进行编辑。
      </div>
      <div v-else-if="selected.type !== 'field'" class="tpl-add">
        <div class="tpl-status">
          {{ labelOf(selected) }} · 第 {{ selected.page }} 页 ·
          {{ Math.round(selected.w) }}×{{ Math.round(selected.h) }} pt
        </div>
        <button
          type="button"
          class="danger"
          id="tplRemoveSelected"
          @click="emit('remove', selected.id)"
        >
          删除这个位置
        </button>
      </div>
      <div v-else class="tpl-add">
        <div class="tpl-status">
          {{ labelOf(selected) }} · 第 {{ selected.page }} 页 ·
          {{ Math.round(selected.w) }}×{{ Math.round(selected.h) }} pt
        </div>
        <div class="field">
          <label for="tplFontSize">字号（pt，0 = 自动缩放）</label>
          <input
            id="tplFontSize"
            :key="`font-${selected.id}`"
            type="number"
            min="0"
            max="72"
            step="0.5"
            :value="selected.font_size || 0"
            @change="onFontSize"
          >
        </div>
        <div class="field">
          <label for="tplAlign">对齐</label>
          <select id="tplAlign" :key="`align-${selected.id}`" :value="selected.align || 'left'" @change="onAlign">
            <option value="left">左对齐</option>
            <option value="center">居中</option>
            <option value="right">右对齐</option>
          </select>
        </div>
        <button
          type="button"
          class="danger"
          id="tplRemoveSelected"
          @click="emit('remove', selected.id)"
        >
          删除这个位置
        </button>
      </div>
    </div>

    <h3>预览与生成</h3>
    <div id="tplUsage" class="tpl-status">{{ usageText }}</div>
    <div id="tplCheck" :class="checkClass">
      <template v-if="check.kind === 'ok'">
        模板体检通过：占位与字段定义一致，签名 / 公章位置齐全。
      </template>
      <template v-else-if="check.kind === 'issues'">
        <div v-for="(issue, index) in check.issues" :key="index" class="tpl-warning">
          {{ issue.message }}<template v-if="issue.action">（{{ issue.action }}）</template>
        </div>
      </template>
      <template v-else-if="check.kind === 'error'">{{ check.text }}</template>
      <template v-else>正在检查字段绑定…</template>
    </div>
    <label class="tpl-mask-toggle">
      <input
        type="checkbox"
        id="tplMaskFieldValues"
        :checked="maskFieldValues"
        @change="onMaskChange"
      >
      字段值白底遮盖（遮住模板里的【】占位符）
    </label>
    <p class="tpl-hint-note">开启后每个字段值先垫白色矩形再写字；改动需保存为新版本后生效。</p>
    <div class="field">
      <label for="tplContract">合同</label>
      <select id="tplContract" :value="contractId" @change="onContractChange">
        <option v-if="!contractsLoaded" value="">（正在加载合同…）</option>
        <option v-else-if="!contracts.length" value="">（还没有合同，可先用样例预览）</option>
        <option v-for="contract in contracts" :key="contract.id" :value="contract.id">
          {{ contract.contract_no }} · {{ contract.title }}
        </option>
      </select>
    </div>
    <div class="tpl-add" style="grid-template-columns: 1fr 1fr">
      <button type="button" id="tplPreviewSample" @click="emit('preview-sample')">样例预览</button>
      <button type="button" id="tplPreviewContract" @click="emit('preview-contract')">合同预览</button>
    </div>
    <button
      type="button"
      id="tplGenerate"
      class="primary"
      :disabled="!canGenerate"
      @click="emit('generate')"
    >
      生成 PDF
    </button>
    <div id="tplPreviewStatus" :class="previewStatusClass">{{ previewStatusText }}</div>
    <ProgressPanel v-if="activeJob" :job="activeJob" />
    <div id="tplWarnings" class="tpl-warnings">
      <template v-if="warnings">
        <div v-if="!warnings.length" class="tpl-status ok">预览未发现缺失字段或越界问题。</div>
        <div v-for="(warning, index) in warnings" :key="index" class="tpl-warning">
          {{ warning.message || warning.code }}
        </div>
      </template>
    </div>
    <div id="tplResult">
      <div v-if="result" class="tpl-add">
        <div class="tpl-status ok">
          已生成 PDF（{{ result.document.page_count }} 页，sha256
          {{ String(result.document.sha256 || "").slice(0, 12) }}…）
        </div>
        <div class="doc-download">
          <button type="button" id="tplOpenGenerated" @click="emit('open-result')">查看生成结果</button>
          <button type="button" id="tplDownloadGenerated" @click="emit('download-result')">下载 PDF</button>
        </div>
      </div>
    </div>
  </aside>
</template>
