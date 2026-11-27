<script setup lang="ts">
// 可打印的自助录入海报（te-48hkl，Q1）：固定入口长期贴墙，任何租户扫码
// 自助建档；换码/停用让旧海报立即失效。打印走浏览器 window.print，A4
// 打印规则在本组件末尾的 @media print 块。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  disableEnrollEntry,
  enrollEntryQrUrl,
  getEnrollEntry,
  rotateEnrollEntry,
} from "../../api/endpoints";
import type { EnrollEntry } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";

const emit = defineEmits<{ close: [] }>();

const entry = ref<EnrollEntry | null>(null);
const busy = ref(false);
const errorText = ref("");
const qrIndex = ref(0);

const urls = computed(() => entry.value?.urls ?? []);
const currentUrl = computed(() => urls.value[qrIndex.value] ?? urls.value[0] ?? "");
/** 二维码地址带当前 token 版本：换码后 <img> 的 src 变化，浏览器重新取新码。 */
const qrSrc = computed(() => {
  const token = entry.value?.token ?? "";
  return token ? `${enrollEntryQrUrl(qrIndex.value)}&v=${token}` : "";
});
const enabled = computed(() => Boolean(entry.value?.enabled));

async function load(): Promise<void> {
  try {
    const result = await getEnrollEntry();
    errorText.value = "";
    if (!result.item.issued) {
      // 未签发时 token 为空：先签发拿到 token 再渲染海报，避免 <img> 以
      // 空 token 请求二维码接口得到 404（CI 控制台报错的来源）。
      await issue();
      return;
    }
    entry.value = result.item;
  } catch (error) {
    errorText.value = errorMessage(error);
  }
}

async function issue(): Promise<void> {
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    const result = await rotateEnrollEntry();
    entry.value = result.item;
    errorText.value = "";
    qrIndex.value = 0;
    toast("已生成固定录入入口；可直接打印海报");
    await refreshAfterWrite();
  } catch (error) {
    errorText.value = errorMessage(error);
  } finally {
    busy.value = false;
  }
}

async function rotate(): Promise<void> {
  if (!window.confirm("换码后，已经打印或张贴的旧海报会立即失效。确定换码吗？")) {
    return;
  }
  await issue();
}

async function disable(): Promise<void> {
  if (!window.confirm("停用后，已经张贴的海报会立即失效；需要时可以再生成新码。确定停用吗？")) {
    return;
  }
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    const result = await disableEnrollEntry();
    entry.value = result.item;
    toast("固定录入入口已停用");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    busy.value = false;
  }
}

function printPoster(): void {
  document.body.classList.add("printing-poster");
  const cleanup = (): void => {
    document.body.classList.remove("printing-poster");
    window.removeEventListener("afterprint", cleanup);
  };
  window.addEventListener("afterprint", cleanup);
  window.print();
  // 部分浏览器不触发 afterprint（打印对话框取消时），定时兜底。
  window.setTimeout(cleanup, 60_000);
}

onMounted(() => {
  void load();
});

onBeforeUnmount(() => {
  document.body.classList.remove("printing-poster");
});
</script>

<template>
  <Teleport to="body">
    <div id="enrollPosterHost" class="modal-backdrop poster-backdrop">
      <div class="modal poster-modal" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>自助录入海报（可打印）</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div v-if="errorText" class="form-errors">{{ errorText }}</div>
          <template v-else-if="entry">
            <p class="help">
              把海报打印出来贴在墙上：租户用手机扫码即可填写姓名、证件号、证件照与签名（或签名章），企业租户还可上传公章。提交后档案自动出现在「相对方档案」，建合同时可直接选择。
            </p>
            <div class="poster-tools">
              <button type="button" id="posterPrintBtn" class="primary" :disabled="!enabled" @click="printPoster">打印海报</button>
              <button type="button" id="posterRotateBtn" :disabled="busy" @click="rotate">换码（旧海报失效）</button>
              <button v-if="enabled" type="button" id="posterDisableBtn" class="danger" :disabled="busy" @click="disable">停用</button>
              <span class="poster-state" :class="enabled ? 'ok' : 'off'">
                {{ enabled ? "入口已启用" : "入口已停用" }}
              </span>
            </div>
            <div class="poster-preview">
              <div id="enrollPosterSheet" class="poster-sheet">
                <div class="poster-mark">腾手</div>
                <h1>租户自助建档</h1>
                <p class="poster-lead">想租房？用手机扫码，先把资料填好，到店签合同更快。</p>
                <ol class="poster-steps">
                  <li>用手机相机或微信扫下面的二维码。</li>
                  <li>填写姓名、证件号，拍摄身份证正反面。</li>
                  <li>手写签名，或上传签名章（私章）图片；企业租户可加传公章。</li>
                  <li>点「提交建档」，资料会保存到出租方的电脑。</li>
                </ol>
                <div class="poster-qr">
                  <img v-if="entry.token && enabled" :src="qrSrc" alt="自助录入二维码" width="320" height="320">
                  <div v-else class="poster-qr-empty">
                    {{ enabled ? "二维码生成中…" : "入口已停用，二维码已失效" }}
                  </div>
                </div>
                <p class="poster-url">手机无法扫码时，请在浏览器打开：<code>{{ currentUrl }}</code></p>
                <p class="poster-foot">仅限与出租方电脑同一局域网的手机访问；资料只保存在出租方电脑本机，不上传互联网。</p>
              </div>
            </div>
            <label v-if="urls.length > 1" class="inline poster-index">
              电脑有多张网卡时请选择手机能访问的地址：
              <select v-model="qrIndex">
                <option v-for="(url, index) in urls" :key="index" :value="index">{{ url }}</option>
              </select>
            </label>
            <p class="help">
              换码会让已打印的旧海报立即失效；停用可暂时关闭入口，重新生成会得到新码。档案按「自助提交」来源记录，提交即入档、不审核。
            </p>
          </template>
          <div v-else class="poster-empty">正在读取固定入口…</div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">关闭</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.poster-tools {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin: 10px 0 14px;
}
.poster-state {
  font-size: 13px;
  color: #5f6670;
}
.poster-state.ok {
  color: var(--accent, #1f6f4a);
}
.poster-state.off {
  color: var(--danger, #b4342c);
}
.poster-preview {
  display: flex;
  justify-content: center;
  padding: 10px 0 4px;
}
.poster-sheet {
  width: 210mm;
  max-width: 100%;
  min-height: 297mm;
  background: #fff;
  color: #1f2328;
  border: 1px solid #d8ddda;
  border-radius: 6px;
  padding: 18mm 18mm 14mm;
  box-shadow: 0 12px 34px rgba(22, 34, 28, .12);
  text-align: center;
}
.poster-mark {
  font-size: 26px;
  font-weight: 700;
  letter-spacing: .12em;
  color: #1f6f4a;
}
.poster-sheet h1 {
  margin: 10px 0 6px;
  font-size: 30px;
}
.poster-lead {
  margin: 0 0 14px;
  font-size: 15px;
  color: #5f6670;
}
.poster-steps {
  text-align: left;
  margin: 0 auto 16px;
  max-width: 150mm;
  padding-left: 22px;
  line-height: 1.9;
  font-size: 14px;
}
.poster-qr img {
  width: 78mm;
  height: 78mm;
  max-width: 100%;
}
.poster-qr-empty {
  width: 78mm;
  height: 78mm;
  max-width: 100%;
  margin: 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: #5f6670;
  border: 1px dashed #d8ddda;
  border-radius: 6px;
}
.poster-url {
  font-size: 11px;
  color: #5f6670;
  word-break: break-all;
  margin: 10px 0;
}
.poster-foot {
  font-size: 11px;
  color: #7d857f;
  margin: 12px 0 0;
}
.poster-index {
  display: block;
  margin-top: 10px;
}
.poster-empty {
  padding: 30px;
  text-align: center;
  color: #5f6670;
}
</style>

<style>
/* 打印时只保留海报本身（Teleport 到 body 的宿主）。 */
@media print {
  body.printing-poster #app,
  body.printing-poster .toast-host,
  body.printing-poster > .modal-backdrop:not(#enrollPosterHost) {
    display: none !important;
  }
  body.printing-poster #enrollPosterHost {
    position: static !important;
    display: block !important;
    padding: 0 !important;
    background: #fff !important;
    overflow: visible !important;
  }
  body.printing-poster #enrollPosterHost .poster-modal {
    width: auto !important;
    max-height: none !important;
    border: 0 !important;
    box-shadow: none !important;
    background: #fff !important;
  }
  body.printing-poster #enrollPosterHost .modal-head,
  body.printing-poster #enrollPosterHost .modal-foot,
  body.printing-poster #enrollPosterHost .poster-tools,
  body.printing-poster #enrollPosterHost .help,
  body.printing-poster #enrollPosterHost .poster-index {
    display: none !important;
  }
  body.printing-poster #enrollPosterHost .modal-body {
    padding: 0 !important;
    overflow: visible !important;
  }
  body.printing-poster #enrollPosterHost .poster-preview {
    padding: 0 !important;
  }
  body.printing-poster #enrollPosterHost .poster-sheet {
    width: auto !important;
    min-height: auto !important;
    border: 0 !important;
    border-radius: 0 !important;
    box-shadow: none !important;
    padding: 0 !important;
  }
  @page {
    size: A4;
    margin: 12mm;
  }
}
</style>
