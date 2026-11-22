<script setup lang="ts">
// 字段管理（阶段 1 迁移）：实体切换、归档过滤、字段台账表格与归档/恢复。
// 结构与 app.js renderFields 的输出一致（toolbar / segmented / summary /
// data-table / actions 类名不变），数据走 frontend/src/api 的 typed 端点。
//
// 跨域联动：app.js 仍持有 loadFields 与 state.fields，供未迁移的合同表单、
// 模板标定读取；本域写操作后调用 refreshAfterWrite() 让 app.js 重新取字段并
// 派发 tengshou:data-changed，相对方档案等已迁移域据此立即刷新。
// 外部写入（MCP/curl）经 SSE/轮询到达 app.js 后同样派发该事件，本组件重取。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  archiveField as archiveFieldRequest,
  listFields,
  updateField,
} from "../../api/endpoints";
import type { FieldDefinition, FieldEntity, FieldsByEntityResponse } from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  subscribeDataChanged,
  toast,
} from "../../bridge/app";
import FieldFormModal from "./FieldFormModal.vue";
import { FIELD_TYPE_LABELS, entityLabel } from "./labels";

const fieldsByEntity = ref<FieldsByEntityResponse>({ contract: [], party: [] });
const entity = ref<FieldEntity>("contract");
const showArchived = ref(false);
const loaded = ref(false);
const formOpen = ref(false);
const editingField = ref<FieldDefinition | null>(null);

let loadSeq = 0;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let unsubscribe: (() => void) | null = null;

const visibleFields = computed(() =>
  fieldsByEntity.value[entity.value].filter(
    (field) => showArchived.value || !field.archived,
  ),
);

// 与 app.js renderFields 的 fieldSummary 文案一致（已归档计数只按过滤后的
// 列表统计：未勾选「显示已归档」时不追加已归档后缀）。
const summary = computed(() => {
  const visible = visibleFields.value;
  const active = visible.filter((field) => !field.archived).length;
  const archived = visible.filter((field) => field.archived).length;
  return (
    `${entityLabel(entity.value)}字段 ${active} 个` +
    (archived ? ` · 已归档 ${archived} 个` : "")
  );
});

async function fetchData(): Promise<void> {
  const seq = ++loadSeq;
  try {
    const data = await listFields({ archived: "1" });
    if (seq !== loadSeq) {
      return;
    }
    fieldsByEntity.value = {
      contract: data.contract ?? [],
      party: data.party ?? [],
    };
    loaded.value = true;
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
    loaded.value = true;
    toast(errorMessage(error), true);
  }
}

function scheduleFetch(delay: number): void {
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
  }
  reloadTimer = setTimeout(() => {
    reloadTimer = undefined;
    void fetchData();
  }, delay);
}

function openCreate(): void {
  editingField.value = null;
  formOpen.value = true;
}

function openEdit(field: FieldDefinition): void {
  editingField.value = field;
  formOpen.value = true;
}

function closeForm(): void {
  formOpen.value = false;
}

async function restore(field: FieldDefinition): Promise<void> {
  try {
    await updateField(field.id, { archived: false });
    toast("字段已恢复");
    await refreshAfterWrite();
    scheduleFetch(0);
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function archive(field: FieldDefinition): Promise<void> {
  if (
    !window.confirm(
      `确定归档字段「${field.label}」吗？字段会从录入和 Excel 中隐藏，已录数据保留。`,
    )
  ) {
    return;
  }
  try {
    await archiveFieldRequest(field.id);
    toast("字段已归档");
    await refreshAfterWrite();
    scheduleFetch(0);
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

onMounted(() => {
  void fetchData();
  unsubscribe = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (entities.length === 0 || entities.includes("field")) {
      scheduleFetch(150);
    }
  });
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
    reloadTimer = undefined;
  }
});
</script>

<template>
  <div class="toolbar">
    <div id="fieldEntityTabs" class="segmented">
      <button
        type="button"
        data-entity="contract"
        :class="{ active: entity === 'contract' }"
        @click="entity = 'contract'"
      >合同字段</button>
      <button
        type="button"
        data-entity="party"
        :class="{ active: entity === 'party' }"
        @click="entity = 'party'"
      >相对方字段</button>
    </div>
    <label class="inline">
      <input id="showArchivedFields" v-model="showArchived" type="checkbox"> 显示已归档
    </label>
    <span class="spacer"></span>
    <button type="button" id="newFieldBtn" class="primary" @click="openCreate">新建字段</button>
  </div>
  <p class="hint">字段是您自己的：新增后立刻可用于录入表单、Excel 导入导出与模板占位符，无需重启或更新软件。旧数据始终保留。</p>
  <div id="fieldSummary" class="summary">{{ loaded ? summary : "" }}</div>
  <div class="table-wrap">
    <table id="fieldTable" class="data-table">
      <template v-if="loaded">
        <thead>
          <tr>
            <th>名称</th>
            <th>键</th>
            <th>类型</th>
            <th>必填</th>
            <th>默认值</th>
            <th>台账</th>
            <th>模板</th>
            <th>排序</th>
            <th>说明</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="field in visibleFields"
            :key="field.id"
            :data-id="field.id"
            :class="{ 'archived-row': field.archived }"
            @click="openEdit(field)"
          >
            <td>
              {{ field.label }}{{ field.built_in ? " " : ""
              }}<span v-if="field.built_in" class="badge">内置</span>{{
                field.archived ? " " : ""
              }}<span v-if="field.archived" class="badge archived">已归档</span>
            </td>
            <td class="mono">{{ field.key }}</td>
            <td>{{ FIELD_TYPE_LABELS[field.type] ?? field.type }}</td>
            <td>{{ field.required ? "是" : "否" }}</td>
            <td>{{ field.default_value || "—" }}</td>
            <td>{{ field.in_ledger ? "是" : "否" }}</td>
            <td>{{ field.in_template ? "是" : "否" }}</td>
            <td class="row-num">{{ field.sort_order }}</td>
            <td class="wrap">{{ field.note || "" }}</td>
            <td>
              <div class="actions">
                <button type="button" @click.stop="openEdit(field)">编辑</button>
                <button
                  v-if="!field.built_in && !field.archived"
                  type="button"
                  class="danger"
                  @click.stop="archive(field)"
                >归档</button>
                <button
                  v-if="!field.built_in && field.archived"
                  type="button"
                  @click.stop="restore(field)"
                >恢复</button>
              </div>
            </td>
          </tr>
          <tr v-if="!visibleFields.length">
            <td class="empty" colspan="10">还没有字段。</td>
          </tr>
        </tbody>
      </template>
    </table>
  </div>
  <FieldFormModal
    v-if="formOpen"
    :key="editingField?.id ?? 'new'"
    :field="editingField"
    :entity="entity"
    @close="closeForm"
  />
</template>
