<script setup lang="ts">
// 关联合同弹窗：勾选使用这枚公章的合同（可多选），保存时批量 attach/detach。
// 数据由 SealsView 打开前取好（与存量 app.js openSealAttach 一致：取数失败
// 只提示、不打开弹窗）；保存后的提示与刷新路径保持不变。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { attachSeal, detachSeal } from "../../api/endpoints";
import type { Contract, Seal } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";

const props = defineProps<{
  seal: Seal;
  /** 当前已关联的合同 id。 */
  linkedIds: string[];
  /** 列表接口返回的前 N 份合同。 */
  contracts: Contract[];
  /** 合同总数；大于列表长度时给出截断提示。 */
  contractTotal: number;
}>();

const emit = defineEmits<{
  close: [];
}>();

const selected = ref(new Set(props.linkedIds));
const saving = ref(false);

const linkedSet = new Set(props.linkedIds);
const visibleIds = new Set(props.contracts.map((contract) => contract.id));
const truncated = computed(() =>
  props.contractTotal > props.contracts.length
    ? `合同较多，这里只列出前 ${props.contracts.length} 份（共 ${props.contractTotal} 份）；未列出的合同会保持原有关联不变。`
    : "",
);

function toggle(id: string, event: Event): void {
  const checked = (event.target as HTMLInputElement).checked;
  if (checked) {
    selected.value.add(id);
  } else {
    selected.value.delete(id);
  }
}

async function save(): Promise<void> {
  if (saving.value) {
    return;
  }
  saving.value = true;
  try {
    const add = [...selected.value].filter((id) => !linkedSet.has(id));
    const remove = [...linkedSet].filter(
      (id) => visibleIds.has(id) && !selected.value.has(id),
    );
    if (add.length) {
      await attachSeal(props.seal.id, add);
    }
    if (remove.length) {
      await detachSeal(props.seal.id, remove);
    }
    emit("close");
    toast(`公章关联已保存：新增 ${add.length} 份，解除 ${remove.length} 份`);
    await refreshAfterWrite();
  } catch (error) {
    saving.value = false;
    toast(errorMessage(error), true);
  }
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
          <h2>关联合同 · {{ seal.name }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <p class="hint">勾选要使用这枚公章的合同，可以一次选择多份（批量落位）。取消勾选会解除对应合同与这枚公章的关联。</p>
          <p v-if="truncated" class="hint">{{ truncated }}</p>
          <div class="seal-contract-list">
            <label
              v-for="contract in contracts"
              :key="contract.id"
              class="seal-contract-row"
            >
              <input
                type="checkbox"
                class="seal-contract-check"
                :data-id="contract.id"
                :checked="selected.has(contract.id)"
                @change="toggle(contract.id, $event)"
              >
              <span class="mono">{{ contract.contract_no }}</span>
              <span>{{ contract.title }}</span>
              <span class="seal-meta">{{ contract.year }} 年</span>
            </label>
            <div v-if="!contracts.length" class="empty">还没有合同。</div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="save">保存关联</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
