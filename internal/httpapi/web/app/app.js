/* 腾手本地应用界面。纯原生 JS，无构建步骤；数据来自本机 /app/api/v1。 */
"use strict";

const state = {
  bootstrap: null,
  /* B8 首装引导：仅 boot 用来决定是否默认停在引导页；界面由 Vue 岛渲染。 */
  onboarding: null,
  fields: { contract: [], party: [] },
  templates: [],
  changelog: [],
  tab: "contracts",
  /* 实时同步游标：最近一条变更记录的 id（服务端用它补发之后的事件）。 */
  lastEventId: "",
};

/* PDF.js renders imported template PDFs. The worker is vendored locally so
   the desktop app stays fully offline. */
if (typeof pdfjsLib !== "undefined") {
  pdfjsLib.GlobalWorkerOptions.workerSrc = "/app/vendor/pdf.worker.min.js";
}

const ENTITY_LABELS = {
  contract: "合同", party: "相对方", field: "字段", seal: "公章", template: "模板",
  signature: "签名", backup: "备份", settings: "设置", import: "导入",
};
const ACTION_LABELS = {
  create: "新建", update: "修改", delete: "删除", archive: "归档", import: "导入",
  export: "导出", restore: "恢复", generate: "生成",
};
/* MCP-originated writes carry this actor (service.MCPActor). */
const MCP_ACTOR = "tengshou-mcp";


/* ---------- 基础工具 ---------- */

function esc(value) {
  return String(value == null ? "" : value).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function $(sel, root) { return (root || document).querySelector(sel); }
function $$(sel, root) { return Array.from((root || document).querySelectorAll(sel)); }

let toastTimer = null;
function toast(message, isError) {
  const el = document.getElementById("toast");
  el.textContent = message;
  el.classList.toggle("error", !!isError);
  el.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove("show"), isError ? 6000 : 3200);
}

async function api(path, options) {
  const opts = Object.assign({ headers: {} }, options || {});
  opts.headers = Object.assign({ "X-Tengshou-App": "1" }, opts.headers || {});
  if (opts.body && typeof opts.body !== "string") {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, opts);
  const ct = res.headers.get("content-type") || "";
  let data = null;
  if (ct.includes("application/json")) {
    data = await res.json();
  } else {
    data = await res.text();
  }
  if (!res.ok) {
    const err = new Error((data && data.error && data.error.message) || `请求失败（${res.status}）`);
    err.code = data && data.error && data.error.code;
    err.fields = (data && data.error && data.error.fields) || [];
    err.payload = data;
    throw err;
  }
  return data;
}

function fmtDate(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString("zh-CN", { hour12: false });
}

/* ---------- 弹出层 ---------- */

function closeModal() {
  stopSignPolling();
  closePDFOverlay();
  if (currentSignSessionId) {
    const id = currentSignSessionId;
    currentSignSessionId = null;
    api(`/app/api/v1/signature-sessions/${encodeURIComponent(id)}`, { method: "DELETE" }).catch(() => {});
  }
  const root = document.getElementById("modalRoot");
  root.innerHTML = "";
  document.removeEventListener("keydown", onModalKey);
}
function onModalKey(e) { if (e.key === "Escape") closeModal(); }

function openModal(title, opts) {
  closeModal();
  const options = opts || {};
  const root = document.getElementById("modalRoot");
  const backdrop = document.createElement("div");
  backdrop.className = "modal-backdrop";
  const modalClass = ["modal", options.wide ? "wide" : "", options.full ? "full" : ""].filter(Boolean).join(" ");
  backdrop.innerHTML = `
    <div class="${modalClass}" role="dialog" aria-modal="true">
      <div class="modal-head"><h2>${esc(title)}</h2>
        <button type="button" class="close" aria-label="关闭">×</button></div>
      <div class="modal-body"></div>
      <div class="modal-foot"></div>
    </div>`;
  root.appendChild(backdrop);
  document.addEventListener("keydown", onModalKey);
  $(".close", backdrop).addEventListener("click", closeModal);
  return {
    el: backdrop,
    body: $(".modal-body", backdrop),
    foot: $(".modal-foot", backdrop),
    title: $(".modal-head h2", backdrop),
    close: closeModal,
  };
}

function footerButton(modal, label, cls, onClick) {
  const btn = document.createElement("button");
  btn.type = "button";
  if (cls) btn.className = cls;
  btn.textContent = label;
  btn.addEventListener("click", onClick);
  modal.foot.appendChild(btn);
  return btn;
}

function fieldErrorsHTML(err) {
  if (err && err.fields && err.fields.length) {
    const items = err.fields.map((f) =>
      `<li>${esc(f.label || f.field || "")}：${esc(f.message || "")}</li>`).join("");
    return `<div class="form-errors">请修正以下问题：<ul>${items}</ul></div>`;
  }
  return `<div class="form-errors">${esc(err.message || "保存失败")}</div>`;
}

/* ---------- 数据加载 ---------- */

async function loadBootstrap() {
  const data = await api("/app/api/v1/bootstrap");
  state.bootstrap = data;
  if (data.onboarding) state.onboarding = data.onboarding;
  renderMeta();
}

async function loadFields() {
  const data = await api("/app/api/v1/fields?archived=1");
  state.fields = { contract: data.contract || [], party: data.party || [] };
}

// 模板页与标定编辑器已迁到 Vue（frontend/src/components/templates/），本函数
// 只为未迁移的生成面板/批量生成与引导步骤保留当前模板版本列表。
async function loadTemplates() {
  const data = await api("/app/api/v1/templates?limit=500&kind=pdf");
  state.templates = data.items || [];
}

async function loadChangelog() {
  const entity = $("#changeEntityFilter").value;
  const source = $("#changeActorFilter") ? $("#changeActorFilter").value : "";
  const params = new URLSearchParams({ limit: "100" });
  if (entity) params.set("entity", entity);
  if (source) params.set("source", source);
  const data = await api("/app/api/v1/changelog?" + params.toString());
  state.changelog = data.items || [];
  renderChangelog(data.total || 0);
  loadRecentActivity().catch(() => {});
}

async function loadRecentActivity() {
  const box = $("#recentActivity");
  if (!box) return;
  const data = await api("/app/api/v1/changelog?limit=8");
  renderRecentActivity(data.items || []);
}

/* ---------- 顶栏 ---------- */

function renderMeta() {
  const meta = $("#meta");
  if (!state.bootstrap) { meta.textContent = ""; return; }
  const stats = state.bootstrap.stats || {};
  meta.innerHTML = `
    <span>操作者：${esc(state.bootstrap.operator || "")}</span>
    <span>合同 ${stats.contracts || 0} 份</span>
    <span>模板 ${stats.templates || 0} 版</span>
    <span>相对方 ${stats.parties || 0} 个</span>
    <span>公章 ${stats.seals || 0} 枚</span>
    <span>字段 ${stats.fields || 0} 个</span>
    <span title="应用版本（数据格式 v${esc(String(state.bootstrap.schema_version || ""))}）">版本 ${esc(state.bootstrap.version || "未知")}</span>`;
}

/* ---------- 合同批量生成、整改重出与 PDF 预览（B4） ----------
   合同详情里的单份生成面板与生成历史已迁到 Vue
   （frontend/src/components/contracts/ContractGenerationPanel.vue，PDF
   预览 overlay 在 frontend/src/components/pdf/）；这里保留批量生成对话框与
   showPDFOverlay，批量样例预览仍在使用。 */

let pdfOverlayEl = null;
let pdfOverlayDoc = null;
let pdfOverlayKeyHandler = null;

function closePDFOverlay() {
  if (pdfOverlayEl && pdfOverlayEl.parentNode) {
    pdfOverlayEl.parentNode.removeChild(pdfOverlayEl);
  }
  pdfOverlayEl = null;
  try { if (pdfOverlayDoc) pdfOverlayDoc.destroy(); } catch (_) { /* ignore */ }
  pdfOverlayDoc = null;
  if (pdfOverlayKeyHandler) {
    document.removeEventListener("keydown", pdfOverlayKeyHandler, true);
    pdfOverlayKeyHandler = null;
  }
}

/* showPDFOverlay renders a PDF on top of the page. actions are optional
   buttons placed in the header (used by generation previews). */
async function showPDFOverlay(bytes, opts) {
  const options = opts || {};
  closePDFOverlay();
  const overlay = document.createElement("div");
  overlay.className = "pdf-overlay";
  const actionButtons = (options.actions || []).map((action, index) =>
    `<button type="button" data-overlay-action="${index}"${action.primary ? ' class="primary"' : ""}>${esc(action.label)}</button>`).join("");
  const warnings = options.warnings || [];
  overlay.innerHTML = `
    <div class="tpl-preview-head">
      <strong>${esc(options.title || "PDF 预览")}</strong>
      <span class="spacer"></span>
      ${actionButtons}
      <button type="button" id="pdfOverlayClose">关闭预览</button>
    </div>
    <div class="tpl-preview-pages"><div class="tpl-loading">正在渲染预览…</div></div>
    <div class="tpl-preview-warnings">${warnings.length
      ? warnings.map((w) => `<div class="tpl-warning">${esc(w.message || w.code)}</div>`).join("")
      : `<div class="tpl-status ok">未发现缺失字段或位置问题。</div>`}</div>`;
  document.body.appendChild(overlay);
  pdfOverlayEl = overlay;
  $("#pdfOverlayClose", overlay).addEventListener("click", closePDFOverlay);
  (options.actions || []).forEach((action, index) => {
    const button = overlay.querySelector(`button[data-overlay-action="${index}"]`);
    if (button && action.onClick) button.addEventListener("click", () => action.onClick(overlay));
  });
  pdfOverlayKeyHandler = (event) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closePDFOverlay();
    }
  };
  document.addEventListener("keydown", pdfOverlayKeyHandler, true);
  const pages = $(".tpl-preview-pages", overlay);
  try {
    pdfOverlayDoc = await renderPDFPagesInto(pages, bytes, 0.9);
  } catch (err) {
    pages.innerHTML = `<div class="tpl-loading">预览渲染失败：${esc(err.message)}</div>`;
  }
}

function templateByID(id) { return state.templates.find((t) => t.id === id); }

function pdfTemplateOptionsHTML(selectedID) {
  return state.templates.map((tpl) =>
    `<option value="${esc(tpl.id)}"${tpl.id === selectedID ? " selected" : ""}>${esc(tpl.name)} v${tpl.version}${tpl.is_active ? "（当前版本）" : ""}</option>`).join("");
}

const BATCH_STATUS_LABELS = {
  ready: "可用",
  needs_signature: "缺签名",
  needs_resign: "需重新签名",
  missing_fields: "缺必填字段",
  missing_party: "缺相对方",
  missing_seal: "缺公章",
  blocked: "不可用",
  error: "预检失败",
};

function batchStatusLabel(status) { return BATCH_STATUS_LABELS[status] || "不可用"; }

/* A preflight item can be generated when it is ready, or when the only
   blocking issue is a content change and the user explicitly allowed reusing
   the old signature for this batch. */
function batchItemEligible(batch, item) {
  if (!batch.selected.has(item.contract_id)) return false;
  if (item.ready) return true;
  if (!batch.allowStale) return false;
  return (item.issues || []).every((issue) => issue.code === "content_changed");
}

function batchEligibleItems(batch) {
  if (!batch.preview) return [];
  return (batch.preview.items || []).filter((item) => batchItemEligible(batch, item));
}

function updateBatchGenerateState(modal, batch) {
  if (!batch.generateBtn) return;
  batch.generateBtn.disabled = batchEligibleItems(batch).length === 0;
  if (!batch.preview) return;
  const eligible = batchEligibleItems(batch).length;
  const needsResign = (batch.preview.items || []).some((item) => item.status === "needs_resign");
  const staleWrap = $("#batchStaleWrap", modal.body);
  if (staleWrap) staleWrap.style.display = needsResign ? "" : "none";
  $("#batchSummary", modal.body).textContent =
    `可生成 ${eligible} 份 · 需处理 ${(batch.preview.items || []).filter((item) => !batchItemEligible(batch, item) && batch.selected.has(item.contract_id)).length} 份` +
    `（共 ${batch.preview.summary.total} 份）` +
    (batch.allowStale ? " · 已确认保留旧签名" : "");
}

/* openBatchGenerateDialog runs the B4 batch flow: choose a template version,
   preflight each contract (required fields / party / signatures / seals), show
   a sample preview, then generate with per-contract results and retry. */
async function openBatchGenerateDialog(opts) {
  const options = opts || {};
  const contracts = (options.contracts || []).filter(Boolean);
  if (!contracts.length) { toast("请选择要生成 PDF 的合同", true); return null; }
  if (!state.templates.length) { toast("还没有 PDF 模板，请先在「模板」页导入", true); return null; }
  const modal = openModal(options.title || `批量生成 PDF · ${contracts.length} 份合同`, { wide: true });
  const fallback = state.templates.find((t) => t.is_active) || state.templates[0];
  const templateID = options.templateId && templateByID(options.templateId)
    ? options.templateId : fallback.id;
  const batch = {
    contracts,
    templateID,
    kind: options.kind || "",
    selected: new Set(contracts.map((c) => c.id)),
    preview: null,
    sampleBytes: null,
    results: null,
    allowStale: false,
    generateBtn: null,
  };
  modal.body.innerHTML = `
    <div id="batchErrors"></div>
    <div class="gen-row">
      <div class="field"><label for="batchTemplate">模板版本</label>
        <select id="batchTemplate">${pdfTemplateOptionsHTML(templateID)}</select></div>
      <div class="gen-actions">
        <button type="button" id="batchPreflight">预检 / 刷新状态</button>
        <button type="button" id="batchSample" disabled>查看样例预览</button>
      </div>
    </div>
    <div class="hint">勾选要生成的合同。预检会逐份检查必填字段、相对方档案、签名（含内容版本）与公章，再开始生成。</div>
    <div class="batch-contract-list" id="batchContractList">
      ${contracts.map((c) => `<label class="batch-contract-row">
        <input type="checkbox" data-batch-select="${esc(c.id)}" checked>
        <span class="mono">${esc(c.contract_no)}</span>
        <span class="grow" title="${esc(c.title)}">${esc(c.title)}</span>
        <span class="gen-chip" data-contract-status="${esc(c.id)}">未预检</span>
      </label>`).join("")}
    </div>
    <div id="batchSummary" class="tpl-status">已选 ${contracts.length} 份合同。点击「预检 / 刷新状态」查看每份合同能否生成。</div>
    <label class="check-row" id="batchStaleWrap" style="display:none">
      <input type="checkbox" id="batchAllowStale">
      保留内容变更前的旧签名重出（仅对签名未失效、内容已变化的合同生效，逐份写入变更记录）
    </label>
    <div id="batchIssues" class="tpl-warnings"></div>
    <div id="batchResults"></div>`;
  modal.foot.innerHTML = "";
  footerButton(modal, "关闭", "", closeModal);
  batch.generateBtn = footerButton(modal, "开始批量生成", "primary", () => {
    runBatchGeneration(modal, batch).catch((err) => {
      $("#batchErrors", modal.body).innerHTML = fieldErrorsHTML(err);
    });
  });
  batch.generateBtn.disabled = true;

  $("#batchTemplate", modal.body).addEventListener("change", (event) => {
    batch.templateID = event.target.value;
    batch.preview = null;
    batch.sampleBytes = null;
    batch.allowStale = false;
    $("#batchAllowStale", modal.body).checked = false;
    $("#batchStaleWrap", modal.body).style.display = "none";
    batch.generateBtn.disabled = true;
    $("#batchSample", modal.body).disabled = true;
    $("#batchSummary", modal.body).textContent = "模板版本已更换，请重新预检。";
    $("#batchIssues", modal.body).innerHTML = "";
    $$("#batchContractList [data-contract-status]", modal.body).forEach((chip) => {
      chip.textContent = "未预检";
      chip.className = "gen-chip";
    });
  });
  $("#batchPreflight", modal.body).addEventListener("click", () => {
    runBatchPreflight(modal, batch).catch((err) => {
      $("#batchErrors", modal.body).innerHTML = fieldErrorsHTML(err);
    });
  });
  $("#batchSample", modal.body).addEventListener("click", () => {
    if (!batch.sampleBytes) return;
    const tpl = templateByID(batch.templateID);
    showPDFOverlay(batch.sampleBytes, {
      title: `样例预览 · ${tpl ? `${tpl.name} v${tpl.version}` : "模板"}`,
      warnings: (batch.preview && batch.preview.warnings) || [],
    }).catch((err) => toast(err.message, true));
  });
  $("#batchAllowStale", modal.body).addEventListener("change", (event) => {
    if (event.target.checked) {
      const confirmed = window.confirm(
        "确认保留内容变更前的旧签名重出？\n\n" +
        "仅当签名仍然有效、只是合同内容有改动时使用。每份合同都会在变更记录中单独记载此确认。");
      if (!confirmed) {
        event.target.checked = false;
        return;
      }
    }
    batch.allowStale = event.target.checked;
    updateBatchGenerateState(modal, batch);
  });
  $$("#batchContractList input[data-batch-select]", modal.body).forEach((input) => {
    input.addEventListener("change", () => {
      if (input.checked) batch.selected.add(input.dataset.batchSelect);
      else batch.selected.delete(input.dataset.batchSelect);
      batch.preview = null;
      batch.allowStale = false;
      $("#batchAllowStale", modal.body).checked = false;
      $("#batchStaleWrap", modal.body).style.display = "none";
      batch.generateBtn.disabled = true;
      $("#batchSummary", modal.body).textContent =
        `已选 ${batch.selected.size} 份合同。勾选变化后请重新预检。`;
    });
  });
  return modal;
}

async function runBatchPreflight(modal, batch) {
  const ids = batch.contracts.filter((c) => batch.selected.has(c.id)).map((c) => c.id);
  if (!ids.length) { toast("请至少勾选一份合同", true); return; }
  if (ids.length > 200) { toast("一次最多预检 200 份合同，请减少勾选", true); return; }
  const button = $("#batchPreflight", modal.body);
  button.disabled = true;
  $("#batchErrors", modal.body).innerHTML = "";
  $("#batchSummary", modal.body).textContent = "正在逐份预检…";
  try {
    const res = await api(`/app/api/v1/templates/${encodeURIComponent(batch.templateID)}/batch/preview`, {
      method: "POST",
      body: { contract_ids: ids, sample: true },
    });
    batch.preview = res;
    batch.sampleBytes = res.pdf_base64 ? base64ToBytes(res.pdf_base64) : null;
    $("#batchSample", modal.body).disabled = !batch.sampleBytes;
    const items = res.items || [];
    const byID = new Map(items.map((item) => [item.contract_id, item]));
    $$("#batchContractList input[data-batch-select]", modal.body).forEach((input) => {
      const chip = $(`[data-contract-status="${input.dataset.batchSelect}"]`, modal.body);
      if (!chip) return;
      const item = byID.get(input.dataset.batchSelect);
      if (!item) {
        chip.textContent = "未预检";
        chip.className = "gen-chip";
        return;
      }
      chip.textContent = batchStatusLabel(item.status);
      chip.className = "gen-chip " + (item.ready ? "ok" : "warn");
    });
    const blocked = items.filter((item) => !item.ready);
    $("#batchIssues", modal.body).innerHTML = blocked.map((item) =>
      `<div class="tpl-warning">${esc(item.contract_no)}：${esc(item.issues.map((issue) => issue.message).join("；"))}</div>`).join("");
    updateBatchGenerateState(modal, batch);
    if (res.summary.blocked > 0) {
      toast(`${res.summary.blocked} 份合同预检未通过，原因已列出`, true);
    } else {
      toast("预检通过；可查看样例预览后开始批量生成");
    }
  } finally {
    button.disabled = false;
  }
}

async function runBatchGeneration(modal, batch) {
  if (!batch.preview) { toast("请先预检，再开始批量生成", true); return; }
  const ready = batchEligibleItems(batch);
  if (!ready.length) { toast("没有预检通过的合同", true); return; }
  const tpl = templateByID(batch.templateID);
  const label = tpl ? `${tpl.name} v${tpl.version}` : "所选模板";
  const staleNote = batch.allowStale ? "\n\n已勾选「保留内容变更前的旧签名重出」，该确认会逐份写入变更记录。" : "";
  if (!window.confirm(`将用「${label}」生成 ${ready.length} 份合同 PDF，继续吗？${staleNote}`)) return;
  batch.generateBtn.disabled = true;
  $("#batchErrors", modal.body).innerHTML = "";
  $("#batchSummary", modal.body).textContent = `正在生成 ${ready.length} 份 PDF…`;
  try {
    const res = await api(`/app/api/v1/templates/${encodeURIComponent(batch.templateID)}/batch/generate`, {
      method: "POST",
      body: {
        items: ready.map((item) => ({
          contract_id: item.contract_id, preview_sha256: item.preview_sha256,
        })),
        kind: batch.kind,
        allow_stale_signature: batch.allowStale,
      },
    });
    batch.results = res;
    batch.preview = null;
    renderBatchResults(modal, batch);
    $("#batchSummary", modal.body).textContent =
      `生成完成：成功 ${res.summary.success} 份 · 失败 ${res.summary.failed} 份`;
    await Promise.all([loadTemplates(), loadBootstrapAndMeta()]);
    notifyDataChanged(["contract", "template"]);
  } finally {
    batch.generateBtn.disabled = true;
  }
}

function renderBatchResults(modal, batch) {
  const host = $("#batchResults", modal.body);
  if (!host) return;
  const res = batch.results;
  if (!res) { host.innerHTML = ""; return; }
  const rows = (res.items || []).map((item) => {
    const resultCell = item.success && item.document
      ? `<button type="button" data-batch-download="${esc(item.document.id)}">下载 PDF</button>`
      : `<span class="row-errors">${esc(item.error_message || "生成失败")}</span>`;
    return `<tr>
      <td class="mono">${esc(item.contract_no || "")}</td>
      <td class="wrap">${esc(item.title || "")}</td>
      <td><span class="gen-badge ${item.success ? "success" : "failed"}">${item.success ? "成功" : "失败"}</span></td>
      <td class="wrap">${resultCell}</td>
      <td><div class="row-actions">${item.success ? "" : `<button type="button" data-batch-retry="${esc(item.contract_id)}">重试</button>`}</div></td>
    </tr>`;
  }).join("");
  host.innerHTML = `<div class="table-wrap"><table class="gen-history-table">
    <thead><tr><th>合同编号</th><th>名称</th><th>状态</th><th>结果</th><th>操作</th></tr></thead>
    <tbody>${rows}</tbody></table></div>
    <div class="tpl-status">批次号 <code>${esc(res.batch_id)}</code>，可在合同详情的生成历史中按批次追溯。</div>`;
  $$("button[data-batch-download]", host).forEach((button) => {
    button.addEventListener("click", () => downloadAsset(
      `/app/api/v1/documents/${encodeURIComponent(button.dataset.batchDownload)}/file`, "contract.pdf"));
  });
  $$("button[data-batch-retry]", host).forEach((button) => {
    button.addEventListener("click", () => {
      retryBatchItem(modal, batch, button.dataset.batchRetry).catch((err) => toast(err.message, true));
    });
  });
}

async function retryBatchItem(modal, batch, contractID) {
  const preview = await api(`/app/api/v1/templates/${encodeURIComponent(batch.templateID)}/batch/preview`, {
    method: "POST",
    body: { contract_ids: [contractID], sample: false },
  });
  const item = (preview.items || [])[0];
  const eligible = item && (item.ready ||
    (batch.allowStale && (item.issues || []).every((issue) => issue.code === "content_changed")));
  if (!eligible) {
    const reason = item ? item.issues.map((issue) => issue.message).join("；") : "预检失败";
    toast(`仍不能生成：${reason}`, true);
    return;
  }
  const res = await api(`/app/api/v1/templates/${encodeURIComponent(batch.templateID)}/batch/generate`, {
    method: "POST",
    body: {
      items: [{ contract_id: contractID, preview_sha256: item.preview_sha256 }],
      kind: batch.kind,
      allow_stale_signature: batch.allowStale,
    },
  });
  const outcome = (res.items || [])[0];
  if (!outcome) throw new Error("批次没有返回结果");
  batch.results.items = (batch.results.items || []).map((entry) =>
    entry.contract_id === contractID ? outcome : entry);
  batch.results.summary = {
    total: batch.results.items.length,
    success: batch.results.items.filter((entry) => entry.success).length,
    failed: batch.results.items.filter((entry) => !entry.success).length,
  };
  renderBatchResults(modal, batch);
  $("#batchSummary", modal.body).textContent =
    `生成完成：成功 ${batch.results.summary.success} 份 · 失败 ${batch.results.summary.failed} 份`;
  toast(outcome.success ? "已重试成功" : `重试失败：${outcome.error_message || ""}`, !outcome.success);
  await Promise.all([loadTemplates(), loadBootstrapAndMeta()]);
  notifyDataChanged(["contract", "template"]);
}

/* ---------- 手写签名（局域网扫码） ---------- */

let signPollTimer = null;
let currentSignSessionId = null;

function stopSignPolling() {
  if (signPollTimer) {
    clearInterval(signPollTimer);
    signPollTimer = null;
  }
}

function signaturesSummaryHTML(detail) {
  const roleOf = (partyID) => {
    if (detail.party_a && detail.party_a.id === partyID) return "甲方";
    if (detail.party_b && detail.party_b.id === partyID) return "乙方";
    return "签名";
  };
  const items = detail.signatures.map((sig) => {
    const version = sig.content_sha256 ? ` · 内容版本 <code>${esc(sig.content_sha256.slice(0, 12))}</code>` : "";
    return `<li><span class="badge active">${esc(roleOf(sig.party_id))}</span> ${esc(sig.signer_name)} · ${esc(fmtDate(sig.captured_at))}${version}
      <button type="button" data-export-signature="${esc(sig.id)}">导出</button></li>`;
  }).join("");
  return `<div class="signature-summary"><strong>已采集签名 ${detail.signatures.length} 个</strong>
    <ul>${items}</ul></div>`;
}

async function openSignatureDialog(contract) {
  let detail = null;
  try {
    detail = await api(`/app/api/v1/contracts/${encodeURIComponent(contract.id)}`);
  } catch (err) {
    toast(err.message, true);
    return;
  }
  const signers = [];
  if (detail.party_b) signers.push({ id: detail.party_b.id, role: "乙方", name: detail.party_b.name });
  if (detail.party_a) signers.push({ id: detail.party_a.id, role: "甲方", name: detail.party_a.name });
  if (!signers.length) {
    toast("合同还没有关联相对方，无法发起签名", true);
    return;
  }
  currentSignSessionId = null;
  const modal = openModal(`扫码签名 · ${contract.contract_no}`);
  modal.body.innerHTML = `
    <div class="signature-panel">
      <div class="field">
        <label for="signParty">签名人</label>
        <select id="signParty">
          ${signers.map((p) => `<option value="${esc(p.id)}">${esc(p.role)} · ${esc(p.name)}</option>`).join("")}
        </select>
        <span class="help">手机与电脑需连接同一局域网；二维码一次性有效，过期或提交后失效。手机打不开时，请确认连接的是同一 Wi-Fi（非访客网络），也可直接输入下方地址。</span>
      </div>
      ${detail.signatures.length ? signaturesSummaryHTML(detail) : ""}
      <div id="signQrArea" class="sign-qr-empty">点击下方「生成签名二维码」，让签名人用手机扫码手写。</div>
    </div>`;
  footerButton(modal, "关闭", "", closeModal);
  const startBtn = footerButton(modal, "生成签名二维码", "primary", () => {
    startSignatureSession(modal, contract, $("#signParty", modal.body).value, startBtn);
  });
}

async function startSignatureSession(modal, contract, partyID, startBtn) {
  stopSignPolling();
  if (currentSignSessionId) {
    api(`/app/api/v1/signature-sessions/${encodeURIComponent(currentSignSessionId)}`, { method: "DELETE" })
      .catch(() => {});
    currentSignSessionId = null;
  }
  const area = $("#signQrArea", modal.body);
  if (!area) return;
  area.innerHTML = `<div class="sign-qr-empty">正在生成二维码…</div>`;
  if (startBtn) startBtn.disabled = true;
  try {
    const res = await api("/app/api/v1/signature-sessions", {
      method: "POST",
      body: { contract_id: contract.id, party_id: partyID },
    });
    const item = res.item;
    currentSignSessionId = item.id;
    if (startBtn) startBtn.textContent = "重新生成二维码";
    const urls = item.urls && item.urls.length ? item.urls : [item.url || ""];
    const urlPicker = urls.length > 1
      ? `<label class="inline sign-url-pick">电脑有多张网卡时请选择手机能访问的地址：
          <select id="signUrlPick">${urls.map((u, i) => `<option value="${i}">${esc(u)}</option>`).join("")}</select>
        </label>`
      : "";
    area.innerHTML = `
      <div class="sign-qr">
        <img id="signQrImg" src="${esc(item.qr_url)}" alt="签名二维码" width="300" height="300">
        <div class="sign-qr-meta">
          ${urlPicker}
          <div class="sign-url mono" id="signUrlText">${esc(urls[0])}</div>
          <div id="signState" class="sign-state">等待手机扫码并提交签名…</div>
          <div id="signExpiry" class="sign-expiry"></div>
        </div>
      </div>`;
    const pick = document.getElementById("signUrlPick");
    if (pick) {
      pick.addEventListener("change", () => {
        const index = Number(pick.value) || 0;
        const img = document.getElementById("signQrImg");
        const text = document.getElementById("signUrlText");
        if (img) img.src = item.qr_url + "?index=" + index;
        if (text) text.textContent = urls[index] || urls[0];
      });
    }
    renderSignExpiry(item.expires_at);
    pollSignatureSession(item.id);
  } catch (err) {
    currentSignSessionId = null;
    area.innerHTML = `<div class="sign-qr-empty error">${esc(err.message)}</div>`;
    toast(err.message, true);
  } finally {
    if (startBtn) startBtn.disabled = false;
  }
}

function renderSignExpiry(expiresAt) {
  const el = document.getElementById("signExpiry");
  if (!el || !expiresAt) return;
  const when = new Date(expiresAt);
  if (isNaN(when.getTime())) return;
  el.textContent = `有效期至 ${when.toLocaleString("zh-CN", { hour12: false })}`;
}

function pollSignatureSession(id) {
  stopSignPolling();
  const tick = async () => {
    if (currentSignSessionId !== id) {
      stopSignPolling();
      return;
    }
    let res;
    try {
      res = await api(`/app/api/v1/signature-sessions/${encodeURIComponent(id)}`);
    } catch (err) {
      if (err && err.code === "not_found") {
        stopSignPolling();
        const stateEl = document.getElementById("signState");
        if (stateEl) {
          stateEl.textContent = "签名会话已失效（可能服务已重启），请重新生成二维码。";
          stateEl.className = "sign-state error";
        }
      }
      return; // other errors are transient: keep polling
    }
    const item = res.item;
    const stateEl = document.getElementById("signState");
    if (!stateEl || currentSignSessionId !== id) return;
    if (item.state === "completed") {
      stopSignPolling();
      const version = item.content_sha256 ? `（内容版本 ${item.content_sha256.slice(0, 12)}）` : "";
      const changed = item.content_changed
        ? "。注意：合同内容在生成二维码后已修改，这份签名对应旧内容版本" : "";
      stateEl.textContent = `签名已保存：${item.signer_name} · ${fmtDate(item.completed_at)}${version}${changed}`;
      stateEl.className = item.content_changed ? "sign-state warn" : "sign-state ok";
      const expiry = document.getElementById("signExpiry");
      if (expiry) expiry.textContent = "";
      toast(item.content_changed ? "签名已保存，但合同内容已变更，请核对" : "签名已保存到本机", item.content_changed);
      refreshAfterWrite().catch((err) => toast(err.message, true));
    } else if (item.state === "expired") {
      stopSignPolling();
      stateEl.textContent = "二维码已过期，请重新生成。";
      stateEl.className = "sign-state error";
    }
  };
  tick();
  signPollTimer = setInterval(tick, 2000);
}

/* ---------- 模板管理（Word → PDF 套印，B3） ---------- */

async function downloadAsset(path, filename) {
  try {
    const res = await fetch(path, { headers: { "X-Tengshou-App": "1" } });
    if (!res.ok) {
      let message = `下载失败（${res.status}）`;
      try {
        const data = await res.json();
        if (data && data.error && data.error.message) message = data.error.message;
      } catch (_) { /* ignore */ }
      throw new Error(message);
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename || "download";
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  } catch (err) {
    toast(err.message, true);
  }
}

async function loadBootstrapAndMeta() {
  const bootstrap = await api("/app/api/v1/bootstrap").catch(() => null);
  if (!bootstrap) return;
  state.bootstrap = bootstrap;
  renderMeta();
}

/* ---------- 首装引导与模板向导（B8） ----------
   引导界面（五步列表、完成/跳过/重新打开、本机自检与模板体检）已迁到
   frontend/src/components/onboarding/。app.js 只保留 boot 用的
   state.onboarding.show_guide 判断；深链经 bridge 切 tab 并派发
   tengshou:open-* 事件。 */

async function renderPDFPagesInto(container, bytes, scale) {
  container.innerHTML = "";
  const doc = await pdfjsLib.getDocument({
    data: bytes.slice(0),
    isEvalSupported: false,
    useSystemFonts: true,
  }).promise;
  const dpr = window.devicePixelRatio || 1;
  for (let i = 1; i <= doc.numPages; i++) {
    const page = await doc.getPage(i);
    const viewport = page.getViewport({ scale });
    const wrap = document.createElement("div");
    wrap.className = "pdf-page";
    wrap.style.width = viewport.width + "px";
    wrap.style.height = viewport.height + "px";
    const canvas = document.createElement("canvas");
    canvas.width = Math.floor(viewport.width * dpr);
    canvas.height = Math.floor(viewport.height * dpr);
    canvas.style.width = viewport.width + "px";
    canvas.style.height = viewport.height + "px";
    wrap.appendChild(canvas);
    container.appendChild(wrap);
    await page.render({
      canvasContext: canvas.getContext("2d"),
      viewport,
      transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : null,
    }).promise;
  }
  return doc;
}

function base64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/* ---------- 字段管理 ----------
   字段页已迁到 Vue（frontend/src/components/fields/）：渲染、新建/编辑弹窗、
   归档与恢复都由 Vue 负责。app.js 只保留 loadFields() 与 state.fields，供
   未迁移的合同生成面板读取；字段变更仍经 refreshAfterWrite() 派发
   tengshou:data-changed。 */

/* ---------- 变更记录 ---------- */

function isAgentEntry(entry) { return entry.actor === MCP_ACTOR; }
function actorLabel(entry) { return isAgentEntry(entry) ? "Agent（MCP）" : "人工"; }

function actorBadgeHTML(entry) {
  const agent = isAgentEntry(entry);
  return `<span class="badge source ${agent ? "agent" : "human"}" title="actor：${esc(entry.actor || "")}">${actorLabel(entry)}</span>`;
}

function changeDiffsHTML(entry) {
  const diffs = (entry.changes || []).map((c) => {
    const oldText = c.old === "" ? "（空）" : c.old;
    const newText = c.new === "" ? "（空）" : c.new;
    return `<li>${esc(c.label || c.field)}：<span class="old">${esc(oldText)}</span>
      <span class="arrow">→</span> <code>${esc(newText)}</code></li>`;
  }).join("");
  return diffs ? `<ul class="change-diffs">${diffs}</ul>` : "";
}

function renderChangelog(total) {
  $("#changelogSummary").textContent = `共 ${total} 条记录（显示最近 ${state.changelog.length} 条）`;
  const list = $("#changelogList");
  if (!state.changelog.length) {
    list.innerHTML = `<div class="empty">还没有变更记录。</div>`;
    return;
  }
  list.innerHTML = state.changelog.map((entry) => `
    <div class="change-card">
      <div class="change-head">
        ${actorBadgeHTML(entry)}
        <span class="badge ${esc(entry.action)}">${esc(ACTION_LABELS[entry.action] || entry.action)}</span>
        <span class="summary">${esc(entry.summary)}</span>
        <span class="who">${esc(ENTITY_LABELS[entry.entity] || entry.entity)} · ${esc(entry.actor || "")} · ${esc(fmtDate(entry.created_at))}</span>
      </div>
      ${changeDiffsHTML(entry)}
    </div>`).join("");
}

function renderRecentActivity(items) {
  const box = $("#recentActivity");
  if (!box) return;
  if (!items.length) {
    box.innerHTML = "";
    return;
  }
  box.innerHTML = `
    <div class="recent-head">最近操作</div>
    <ul class="recent-list">${items.map((entry) => `
      <li>
        <span class="recent-time">${esc(fmtDate(entry.created_at))}</span>
        ${actorBadgeHTML(entry)}
        <span class="badge ${esc(entry.action)}">${esc(ACTION_LABELS[entry.action] || entry.action)}</span>
        <span class="recent-summary">${esc(entry.summary)}</span>
      </li>`).join("")}
    </ul>`;
}

/* ---------- Excel 导入 ---------- */

let importState = null;

function readFileBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      const idx = result.indexOf(",");
      resolve(idx >= 0 ? result.slice(idx + 1) : result);
    };
    reader.onerror = () => reject(new Error("读取文件失败"));
    reader.readAsDataURL(file);
  });
}

async function openImport(file) {
  let base64;
  try {
    base64 = await readFileBase64(file);
  } catch (err) {
    toast(err.message, true);
    return;
  }
  importState = {
    base64,
    fileName: file.name,
    preview: null,
    mapping: null,
    options: {
      mode: "upsert", skip_errors: false, create_parties: true,
      apply_deletes: false, file_name: file.name,
    },
    dirty: false,
    loading: false,
  };
  const modal = openModal(`导入 Excel · ${file.name}`, { wide: true });
  modal.body.innerHTML = `<p>正在解析 <code>${esc(file.name)}</code>…</p>`;
  try {
    await runImportPreview(modal, null);
  } catch (err) {
    modal.body.innerHTML = `<div class="form-errors">${esc(err.message)}</div>`;
    footerButton(modal, "关闭", "", closeModal);
  }
}

const IMPORT_ACTION_LABELS = {
  create: "新增", update: "更新", unchanged: "不变", error: "错误",
  delete: "删除", conflict: "冲突",
};

function importActionLabel(action) {
  return IMPORT_ACTION_LABELS[action] || action;
}

function importOptionsHTML(options) {
  return `<div class="import-options">
    <label class="inline">模式
      <select id="importMode">
        <option value="upsert"${options.mode === "upsert" ? " selected" : ""}>合并更新（编号已存在则更新）</option>
        <option value="create"${options.mode === "create" ? " selected" : ""}>仅新增（编号已存在则报错）</option>
      </select>
    </label>
    <label class="check-row"><input type="checkbox" id="importSkipErrors"${options.skip_errors ? " checked" : ""}> 跳过错误行</label>
    <label class="check-row"><input type="checkbox" id="importCreateParties"${options.create_parties ? " checked" : ""}> 新相对方自动建档</label>
  </div>`;
}

function renderImportMapping(preview, mapping) {
  return `<div class="import-mapping">${preview.columns.map((col) => {
    const current = mapping[String(col.index)] || "";
    const opts = [`<option value="">（未映射）</option>`];
    for (const f of preview.fields) {
      opts.push(`<option value="${esc(f.key)}"${current === f.key ? " selected" : ""}>${esc(f.label)}（${esc(f.key)}）</option>`);
    }
    return `<div class="map-row">
      <span>第 ${col.index + 1} 列：${esc(col.header || "（空表头）")}</span>
      <select data-map-index="${col.index}" ${col.header ? "" : "disabled"}>${opts.join("")}</select>
    </div>`;
  }).join("")}</div>`;
}

function renderImportSummary(preview) {
  const summary = preview.summary || {};
  const badges = [
    ["create", "新增", summary.create],
    ["update", "更新", summary.update],
    ["unchanged", "不变", summary.unchanged],
    ["delete", "删除", summary.deleted],
    ["conflict", "冲突", summary.conflicts],
    ["error", "错误", summary.error],
  ];
  return `<div class="import-summary">
      ${badges.map(([cls, label, count]) =>
        `<span class="badge ${cls}">${label} ${count || 0}</span>`).join("")}
      <span class="badge">共 ${summary.total || 0} 行</span>
    </div>`;
}

function fieldLabelByKey(preview, key) {
  const field = (preview.fields || []).find((f) => f.key === key);
  return field ? field.label : key;
}

function conflictValuesHTML(preview, row) {
  const system = row.system_values || {};
  const excel = row.excel_values || {};
  const keys = Array.from(new Set([...Object.keys(system), ...Object.keys(excel)]));
  if (!keys.length) return "";
  const rows = keys.map((key) => `<tr>
      <td>${esc(fieldLabelByKey(preview, key))}</td>
      <td class="wrap">${esc(system[key] == null ? "" : system[key])}</td>
      <td class="wrap">${esc(excel[key] == null ? "" : excel[key])}</td>
    </tr>`).join("");
  return `<div class="table-wrap"><table class="data-table conflict-table">
      <thead><tr><th>字段</th><th>系统当前</th><th>Excel</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>`;
}

function conflictChoiceHTML(row, keepLabel, useLabel) {
  const name = "resolve-" + String(row.key).replace(/[^a-zA-Z0-9_-]/g, "_");
  return `<div class="conflict-choice" data-resolve-key="${esc(row.key)}">
      <label class="inline"><input type="radio" name="${name}" value="keep_system" checked> ${esc(keepLabel)}</label>
      <label class="inline"><input type="radio" name="${name}" value="use_excel"> ${esc(useLabel)}</label>
    </div>`;
}

function renderImportConflicts(preview) {
  const conflicts = (preview.rows || []).filter((row) => row.row > 0 && row.action === "conflict");
  if (!conflicts.length) return "";
  return `<h3 class="import-section-title">冲突（${conflicts.length} 行，需逐行选择）</h3>
    ${conflicts.map((row) => `<div class="import-conflict">
      <div class="import-conflict-head">
        <strong>第 ${row.row} 行 · ${esc(row.contract_no || "")} ${esc(row.title || "")}</strong>
        <span class="badge conflict">冲突</span>
      </div>
      <p class="hint">${esc(row.conflict_reason || "导出后两边都有改动，需要选择保留哪一边。")}</p>
      ${conflictValuesHTML(preview, row)}
      ${conflictChoiceHTML(row, "保留系统", "采用 Excel")}
    </div>`).join("")}`;
}

function renderImportDeletes(preview) {
  const deletes = (preview.rows || []).filter((row) => row.row === 0);
  if (!deletes.length) return "";
  const rows = deletes.map((row) => {
    const conflict = row.action === "conflict";
    return `<tr>
      <td class="mono">${esc(row.contract_no || "")}</td>
      <td class="wrap">${esc(row.title || "")}</td>
      <td>${esc(row.party_b || "")}</td>
      <td class="wrap">${conflict
        ? `<span class="badge conflict">系统有改动</span>
           <div class="hint">${esc(row.conflict_reason || "系统在导出后修改了该合同；默认保留系统。")}</div>
           ${conflictChoiceHTML(row, "保留系统（不删除）", "采用 Excel（删除）")}`
        : `<span class="badge delete">将删除</span>`}</td>
    </tr>`;
  }).join("");
  return `<div class="import-deletes">
    <h3 class="import-section-title">删除候选（${deletes.length} 行）</h3>
    <p class="hint">这些合同在导出快照中存在，但 Excel 里已经没有对应行。默认不删除；确认后才能勾选应用删除。</p>
    <label class="check-row"><input type="checkbox" id="importApplyDeletes"${importState.options.apply_deletes ? " checked" : ""}> 应用删除（未勾选时这些行会被跳过）</label>
    <div class="table-wrap"><table class="data-table import-table">
      <thead><tr><th>合同编号</th><th>合同名称</th><th>相对方</th><th>处理</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
  </div>`;
}

function renderImportRows(preview) {
  const rows = (preview.rows || []).filter((row) => row.row > 0 && row.action !== "conflict");
  const shown = rows.slice(0, 200);
  const body = shown.map((row) => {
    const issues = [];
    for (const e of row.errors || []) {
      issues.push(`<li>${esc(e.label || e.field || "")}：${esc(e.message)}</li>`);
    }
    const warnings = (row.warnings || []).map((w) => `<li>${esc(w)}</li>`).join("");
    return `<tr>
      <td class="row-num">${row.row}</td>
      <td><span class="badge ${esc(row.action)}">${esc(importActionLabel(row.action))}</span></td>
      <td class="mono">${esc(row.contract_no || "")}</td>
      <td class="wrap">${esc(row.title || "")}</td>
      <td>${esc(row.party_b || "")}</td>
      <td class="wrap">
        ${issues.length ? `<ul class="row-errors">${issues.join("")}</ul>` : ""}
        ${warnings ? `<ul class="row-warnings">${warnings}</ul>` : ""}
      </td>
    </tr>`;
  }).join("");
  return `<div class="table-wrap"><table class="data-table import-table">
      <thead><tr><th>行</th><th>动作</th><th>合同编号</th><th>合同名称</th><th>相对方</th><th>问题</th></tr></thead>
      <tbody>${body || `<tr><td class="empty" colspan="6">没有数据行</td></tr>`}</tbody>
    </table></div>
    ${rows.length > shown.length ? `<p class="hint">仅显示前 ${shown.length} 行，共 ${rows.length} 行。</p>` : ""}`;
}

async function runImportPreview(modal, mappingOverride) {
  if (importState.loading) return;
  importState.loading = true;
  const options = readImportOptions(modal);
  importState.options = options;
  modal.body.innerHTML = `<p>正在解析并校验…</p>`;
  modal.foot.innerHTML = "";
  try {
    const body = { file_base64: importState.base64, options: Object.assign({}, options) };
    if (mappingOverride) body.options.mapping = mappingOverride;
    const preview = await api("/app/api/v1/excel/preview", { method: "POST", body });
    importState.preview = preview;
    importState.mapping = Object.assign({}, preview.mapping || {});
    importState.dirty = false;
    renderImport(modal);
  } finally {
    importState.loading = false;
  }
}

function readImportOptions(modal) {
  const base = importState
    ? Object.assign({}, importState.options)
    : { mode: "upsert", skip_errors: false, create_parties: true, apply_deletes: false };
  if (!modal || !modal.body.querySelector("#importMode")) return base;
  const applyDeletes = $("#importApplyDeletes", modal.body);
  return Object.assign(base, {
    mode: $("#importMode", modal.body).value,
    skip_errors: $("#importSkipErrors", modal.body).checked,
    create_parties: $("#importCreateParties", modal.body).checked,
    apply_deletes: applyDeletes ? applyDeletes.checked : !!base.apply_deletes,
  });
}

function currentMapping(modal) {
  const mapping = {};
  $$("select[data-map-index]", modal.body).forEach((select) => {
    if (select.value) mapping[select.dataset.mapIndex] = select.value;
  });
  return mapping;
}

function collectResolutions(modal) {
  const resolutions = {};
  $$("[data-resolve-key]", modal.body).forEach((box) => {
    const checked = $('input[type="radio"]:checked', box);
    if (checked) resolutions[box.dataset.resolveKey] = checked.value;
  });
  return resolutions;
}

function importResultToast(result) {
  const parts = [`新增 ${result.created || 0} 条`, `更新 ${result.updated || 0} 条`];
  if (result.deleted) parts.push(`删除 ${result.deleted} 条`);
  if (result.skipped) parts.push(`跳过 ${result.skipped} 条`);
  return `导入完成：${parts.join("，")}`;
}

function renderImport(modal) {
  const preview = importState.preview;
  const notes = (preview.notes || []).map((n) => `<li>${esc(n)}</li>`).join("");
  modal.body.innerHTML = `
    ${importOptionsHTML(importState.options)}
    <p class="hint">文件：${esc(importState.fileName)} · 工作表：${esc(preview.sheet)}。导入前先预览，确认列名映射与每一行的动作。</p>
    ${renderImportMapping(preview, importState.mapping)}
    ${notes ? `<ul class="import-notes">${notes}</ul>` : ""}
    <p id="importDirtyHint" class="hint" style="display:none">列映射已修改，建议先点「重新预览」确认结果。</p>
    <div id="importResult"></div>
    ${renderImportSummary(preview)}
    ${renderImportConflicts(preview)}
    ${renderImportDeletes(preview)}
    ${renderImportRows(preview)}`;
  modal.foot.innerHTML = "";
  footerButton(modal, "取消", "", closeModal);
  footerButton(modal, "重新预览", "", async () => {
    try {
      await runImportPreview(modal, currentMapping(modal));
    } catch (err) {
      toast(err.message, true);
    }
  });
  const commit = footerButton(modal, "确认导入", "primary", async () => {
    commit.disabled = true;
    try {
      const applyDeletes = $("#importApplyDeletes", modal.body);
      const options = Object.assign({}, importState.options, {
        mapping: currentMapping(modal),
        apply_deletes: applyDeletes ? applyDeletes.checked : false,
        resolutions: collectResolutions(modal),
      });
      const body = { file_base64: importState.base64, options };
      const result = await api("/app/api/v1/excel/commit", { method: "POST", body });
      showImportResult(modal, result, false);
      toast(importResultToast(result));
      await refreshAfterWrite();
    } catch (err) {
      commit.disabled = false;
      if (err.payload && err.payload.result) {
        showImportResult(modal, err.payload.result, true);
      }
      toast(err.message, true);
    }
  });
  $$("select[data-map-index]", modal.body).forEach((select) => {
    select.addEventListener("change", () => {
      importState.dirty = true;
      const hint = $("#importDirtyHint", modal.body);
      if (hint) hint.style.display = "";
    });
  });
  $$("#importMode, #importSkipErrors, #importCreateParties", modal.body).forEach((el) => {
    el.addEventListener("change", () => { importState.options = readImportOptions(modal); });
  });
  const applyDeletes = $("#importApplyDeletes", modal.body);
  if (applyDeletes) {
    applyDeletes.addEventListener("change", () => {
      importState.options.apply_deletes = applyDeletes.checked;
    });
  }
}

function showImportResult(modal, result, blocked) {
  const target = $("#importResult", modal.body);
  if (!target) return;
  const rows = (result.rows || []).slice(0, 200).map((row) => {
    const errors = (row.errors || []).map((e) => `<li>${esc(e.label || "")}：${esc(e.message)}</li>`).join("");
    return `<tr>
      <td class="row-num">${row.row || "—"}</td>
      <td><span class="badge ${esc(row.action)}">${esc(importActionLabel(row.action))}</span></td>
      <td class="mono">${esc(row.contract_no || "")}</td>
      <td class="wrap">${esc(row.title || "")}</td>
      <td class="wrap">${errors ? `<ul class="row-errors">${errors}</ul>` : ""}</td>
    </tr>`;
  }).join("");
  const badges = [
    ["create", "新增", result.created],
    ["update", "更新", result.updated],
    ["unchanged", "不变", result.unchanged],
    ["delete", "删除", result.deleted],
    ["conflict", "冲突", result.conflicts],
    ["error", "错误", result.errors],
  ];
  target.innerHTML = `
    <div class="import-summary">
      ${badges.map(([cls, label, count]) => `<span class="badge ${cls}">${label} ${count || 0}</span>`).join("")}
      ${result.skipped ? `<span class="badge">跳过 ${result.skipped}</span>` : ""}
    </div>
    ${blocked ? `<div class="form-errors">存在错误行，本次未写入任何数据。请修正 Excel 后重新导入，或勾选「跳过错误行」。</div>` : ""}
    <div class="table-wrap"><table class="data-table import-table">
      <thead><tr><th>行</th><th>动作</th><th>合同编号</th><th>合同名称</th><th>问题</th></tr></thead>
      <tbody>${rows || `<tr><td class="empty" colspan="5">没有记录</td></tr>`}</tbody>
    </table></div>`;
  target.scrollIntoView({ block: "nearest" });
}

/* ---------- 数据安全（B9 备份 / 恢复 / 一致性） ---------- */

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
}

async function loadBackupStatus() {
  const status = await api("/app/api/v1/backup/status");
  renderBackupStatus(status);
}

function renderBackupStatus(status) {
  const el = $("#backupStatus");
  if (!el) return;
  if (!status) { el.innerHTML = ""; return; }
  const pending = status.pending;
  const pendingHTML = pending
    ? `<span class="badge update">待重启生效</span> 备份包 ${esc(pending.id)}（${esc(fmtDate(pending.staged_at))} 校验通过，${pending.file_count} 个文件）`
    : `<span class="badge active">已就绪</span> 恢复包会在校验通过后暂存，重启腾手才生效`;
  el.innerHTML = `
    <span>数据目录：<code>${esc(status.data_dir || "")}</code></span>
    <span>${pendingHTML}</span>`;
}

async function exportBackup() {
  const btn = $("#exportBackupBtn");
  const original = btn.textContent;
  btn.disabled = true;
  btn.textContent = "正在打包…";
  try {
    const res = await fetch("/app/api/v1/backup/export", { headers: { "X-Tengshou-App": "1" } });
    if (!res.ok) {
      let message = `导出失败（${res.status}）`;
      try {
        const data = await res.json();
        if (data && data.error && data.error.message) message = data.error.message;
      } catch (_) { /* ignore */ }
      throw new Error(message);
    }
    const blob = await res.blob();
    let filename = "tengshou-backup.zip";
    const disposition = res.headers.get("Content-Disposition") || "";
    const match = /filename="([^"]+)"/.exec(disposition);
    if (match) filename = match[1];
    downloadBlob(blob, filename);
    toast("完整备份包已开始下载；导出记录已写入变更记录");
    await loadBackupStatus().catch(() => {});
  } catch (err) {
    toast(err.message, true);
  } finally {
    btn.disabled = false;
    btn.textContent = original;
  }
}

async function restoreBackupFromFile(event) {
  const input = event.target;
  const file = input.files && input.files[0];
  input.value = "";
  if (!file) return;
  if (!/\.zip$/i.test(file.name)) {
    toast("请选择 .zip 备份包", true);
    return;
  }
  const ok = window.confirm(
    `将使用「${file.name}」整体替换当前数据：数据库、签名/公章图片、模板、已生成 PDF 和配置。\n\n` +
    "恢复包会先做完整性与摘要校验；校验通过后暂存，重启腾手才生效。\n" +
    "当前数据会先自动留一份“恢复前副本”；校验失败时原数据完全不动。\n\n确认恢复？");
  if (!ok) return;
  const btn = $("#restoreBackupBtn");
  const original = btn.textContent;
  btn.disabled = true;
  btn.textContent = "正在校验…";
  try {
    const form = new FormData();
    form.append("file", file);
    const res = await fetch("/app/api/v1/backup/restore", {
      method: "POST",
      headers: { "X-Tengshou-App": "1" },
      body: form,
    });
    let data = null;
    try { data = await res.json(); } catch (_) { /* ignore */ }
    if (!res.ok) {
      const message = (data && data.error && data.error.message) || `恢复失败（${res.status}）`;
      throw new Error(message);
    }
    toast("备份包校验通过并已暂存。请重启腾手，恢复才会生效。");
    await loadBackupStatus().catch(() => {});
  } catch (err) {
    toast(err.message, true);
  } finally {
    btn.disabled = false;
    btn.textContent = original;
  }
}

async function checkIntegrity() {
  const box = $("#integrityReport");
  box.innerHTML = `<p class="hint">正在检查数据库与资产文件…</p>`;
  try {
    const report = await api("/app/api/v1/integrity");
    const missing = report.missing_files || [];
    const orphans = report.orphan_files || [];
    const db = report.database_ok
      ? `<span class="badge active">数据库完整性检查通过</span>`
      : `<span class="badge error">数据库完整性检查失败</span> ${esc(report.database_error || "")}`;
    const list = (items) => items.length
      ? `<ul>${items.map((item) =>
          `<li><code>${esc(typeof item === "string" ? item : item.path)}</code></li>`).join("")}</ul>`
      : `<p class="hint">无</p>`;
    box.innerHTML = `
      <div class="change-card">
        <div class="change-head">${db}
          <span class="who">${esc(fmtDate(report.checked_at))} · 已核对 ${report.checked_files || 0} 个文件</span></div>
        <h3>数据库有记录但文件缺失（${missing.length}）</h3>${list(missing)}
        <h3>文件存在但数据库没有记录（${orphans.length}）</h3>${list(orphans)}
        <p class="hint">此检查只报告，不自动删除任何文件。</p>
      </div>`;
  } catch (err) {
    box.innerHTML = `<div class="form-errors">${esc(err.message)}</div>`;
  }
}

async function exportSignature(id) {
  if (!window.confirm("签名图片是敏感资产：导出后可能被复制、传播，离开本机数据目录。\n\n确认导出这份签名？")) {
    return;
  }
  try {
    const res = await fetch(`/app/api/v1/signatures/${encodeURIComponent(id)}/export`, {
      method: "POST",
      headers: { "X-Tengshou-App": "1", "Content-Type": "application/json" },
      body: JSON.stringify({ confirm: true }),
    });
    if (!res.ok) {
      let data = null;
      try { data = await res.json(); } catch (_) { /* ignore */ }
      const message = (data && data.error && data.error.message) || `导出失败（${res.status}）`;
      throw new Error(message);
    }
    const blob = await res.blob();
    downloadBlob(blob, `${id}.png`);
    toast("签名图片已导出；导出操作已写入变更记录");
  } catch (err) {
    toast(err.message, true);
  }
}

/* ---------- 设置：MCP 状态与读写开关 ---------- */

async function loadMCPStatus() {
  const panel = $("#mcpPanel");
  if (!panel) return null;
  try {
    const status = await api("/app/api/v1/mcp/status");
    renderMCPPanel(status);
    return status;
  } catch (err) {
    panel.innerHTML = `<div class="form-errors">MCP 状态读取失败：${esc(err.message)}</div>`;
    const summary = $("#mcpStatusSummary");
    if (summary) summary.textContent = "MCP 状态不可用";
    return null;
  }
}

function renderMCPPanel(status) {
  const panel = $("#mcpPanel");
  if (!panel) return;
  const enabled = !!status.write_enabled;
  const total = status.agent_actions_total || 0;
  const recent = status.agent_actions_recent || 0;
  const last = status.last_agent_action;
  const lastHTML = last
    ? `${esc(last.summary)} <span class="muted">（${esc(fmtDate(last.created_at))}）</span>`
    : "还没有 agent 写入记录";
  const summary = $("#mcpStatusSummary");
  if (summary) {
    summary.innerHTML = `<span class="badge active">服务运行中</span>
      <span>读工具 ${status.read_tools || 0} 个</span>
      <span>写工具 ${status.write_tools || 0} 个</span>`;
  }
  panel.innerHTML = `
    <div class="mcp-grid">
      <div class="mcp-card">
        <h3>连接器状态</h3>
        <dl>
          <dt>服务</dt><dd><span class="badge active">运行中</span></dd>
          <dt>连接器版本</dt><dd><code>${esc(status.version || "未知")}</code></dd>
          <dt>传输</dt><dd>stdio · 仅本机回环 · 不打开数据库</dd>
          <dt>读工具</dt><dd>${status.read_tools || 0} 个 <span class="muted">始终可用</span></dd>
          <dt>写工具</dt><dd>${status.write_tools || 0} 个 <span class="muted">新建字段 / 档案 / 合同草稿，全部记入变更记录</span></dd>
          <dt>Agent 操作</dt><dd>累计 ${total} 次，近 24 小时 ${recent} 次</dd>
          <dt>最近一次</dt><dd>${lastHTML}</dd>
          <dt>数据目录</dt><dd><code>${esc(status.data_dir || "")}</code></dd>
        </dl>
      </div>
      <div class="mcp-card">
        <h3>读写开关</h3>
        <div class="switch-row">
          <label class="switch"><input type="checkbox" checked disabled>
            <span>读工具（默认开启）</span></label>
        </div>
        <div class="switch-row">
          <label class="switch"><input type="checkbox" id="mcpWriteToggle" ${enabled ? "checked" : ""}>
            <span>写工具（${enabled ? "已开启" : "已关闭"}）</span></label>
        </div>
        <p class="hint">关闭后：agent 仍可读取；写工具返回 <code>mcp_write_disabled</code>，桌面界面的写入不受影响。改动即时生效，并写入变更记录。</p>
        <p class="hint">配置文件：<code>${esc(status.config_file || "config.json")}</code>（键 <code>${esc(status.settings_key || "mcp.write_enabled")}</code>）。</p>
      </div>
      <div class="mcp-card full">
        <h3>连接说明</h3>
        <p>${esc(status.connection_help || "")}</p>
        <p class="hint">注册命令（填到 Claude Code / Claude Desktop 的 MCP 配置中）：</p>
        <pre class="mono mcp-command">${esc(status.command || "tengshou-cli mcp")}</pre>
        <p class="hint">本面板只展示状态与开关，不启动或停止 MCP 进程。</p>
      </div>
    </div>`;
  const toggle = $("#mcpWriteToggle", panel);
  if (toggle) toggle.addEventListener("change", () => setMCPWriteEnabled(toggle.checked, toggle));
}

async function setMCPWriteEnabled(enabled, toggle) {
  if (toggle) toggle.disabled = true;
  if (!enabled && !window.confirm("关闭 MCP 写工具后，agent 只能读取数据（写工具会报错）。确认关闭？")) {
    if (toggle) { toggle.checked = true; toggle.disabled = false; }
    return;
  }
  try {
    await api("/app/api/v1/settings/mcp", { method: "POST", body: { write_enabled: enabled } });
    toast(enabled ? "已开启 MCP 写工具（即时生效）" : "已关闭 MCP 写工具（即时生效）");
    await loadMCPStatus();
  } catch (err) {
    if (toggle) { toggle.checked = !enabled; toggle.disabled = false; }
    toast(err.message, true);
  }
}

/* ---------- 实时同步（SSE + 轮询降级） ----------
   MCP / agent 从外部写数据时，界面无需重启或整页刷新：单例 EventSource
   接收 change_log 事件，按实体增量刷新当前视图；桌面壳或代理阻断 SSE 时
   自动降级为 3 秒轮询（页面隐藏时暂停）。 */

let eventSource = null;
let eventPollTimer = null;
let eventRefreshTimer = null;
let eventRetryTimer = null;
let realtimeMode = "connecting";
const pendingEntities = new Set();
const seenEventIds = new Set();
let pendingAgentActivity = false;

function setLiveStatus(mode, detail) {
  realtimeMode = mode;
  // 顶栏徽标已迁到 Vue 岛（frontend/src/components/LiveStatusBadge.vue）：
  // 这里只把状态写到 window 并派发事件，DOM 渲染不再落在 app.js。
  const live = { mode, detail: detail || "" };
  window.__tengshouLiveStatus = live;
  window.dispatchEvent(new CustomEvent("tengshou:live-status", { detail: live }));
}

// 迁移期数据变更广播：Vue 域（相对方档案、字段管理）监听后重取自己的数据。
// 与 tengshou:change（单条变更）配对使用，退役时随 app.js 一起删除。
function notifyDataChanged(entities) {
  window.dispatchEvent(new CustomEvent("tengshou:data-changed", { detail: { entities } }));
}

function eventStreamURL() {
  const params = new URLSearchParams();
  if (state.lastEventId) params.set("after", state.lastEventId);
  const qs = params.toString();
  return "/app/api/v1/events" + (qs ? "?" + qs : "");
}

function startRealtime() {
  connectEventStream();
}

function connectEventStream() {
  if (eventRetryTimer) {
    clearTimeout(eventRetryTimer);
    eventRetryTimer = null;
  }
  if (typeof EventSource === "undefined") {
    startEventPolling();
    return;
  }
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  let source;
  try {
    source = new EventSource(eventStreamURL());
  } catch (_) {
    startEventPolling();
    return;
  }
  eventSource = source;
  source.onopen = () => {
    if (eventSource !== source) return;
    stopEventPolling();
    setLiveStatus("sse");
    // Catch up on anything written between the last page load and the stream
    // starting (or while the stream was disconnected).
    requestCatchUpRefresh();
  };
  source.onmessage = (event) => {
    if (eventSource !== source || !event.data) return;
    let payload = null;
    try {
      payload = JSON.parse(event.data);
    } catch (_) {
      return;
    }
    if (payload && !payload.id && event.lastEventId) payload.id = event.lastEventId;
    handleChangeEvent(payload);
  };
  source.onerror = () => {
    if (eventSource !== source) return;
    // EventSource retries on its own while CONNECTING. Polling runs alongside
    // so updates keep flowing when SSE is blocked by the desktop WebView.
    if (realtimeMode !== "poll") setLiveStatus("poll");
    startEventPolling();
    if (source.readyState === 2 /* CLOSED */) {
      eventSource = null;
      eventRetryTimer = setTimeout(connectEventStream, 10000);
    }
  };
}

async function pollEventsOnce() {
  if (document.hidden) return;
  try {
    const params = new URLSearchParams({ limit: "200" });
    if (state.lastEventId) params.set("after", state.lastEventId);
    const data = await api("/app/api/v1/changelog?" + params.toString());
    for (const item of data.items || []) handleChangeEvent(item);
  } catch (_) {
    /* transient: the next tick retries */
  }
}

function startEventPolling() {
  if (eventPollTimer) return;
  pollEventsOnce();
  eventPollTimer = setInterval(pollEventsOnce, 3000);
}

function stopEventPolling() {
  if (eventPollTimer) {
    clearInterval(eventPollTimer);
    eventPollTimer = null;
  }
}

function scheduleRealtimeRefresh(delay) {
  if (eventRefreshTimer) clearTimeout(eventRefreshTimer);
  eventRefreshTimer = setTimeout(() => {
    eventRefreshTimer = null;
    flushRealtimeRefresh();
  }, delay == null ? 350 : delay);
}

function requestCatchUpRefresh() {
  for (const entity of ["field", "contract", "party", "seal", "template"]) {
    pendingEntities.add(entity);
  }
  if (state.tab === "settings") pendingEntities.add("settings");
  notifyDataChanged(Array.from(pendingEntities));
  scheduleRealtimeRefresh(150);
}

function handleChangeEvent(item) {
  if (!item || !item.id || seenEventIds.has(item.id)) return;
  seenEventIds.add(item.id);
  if (seenEventIds.size > 500) {
    seenEventIds.delete(seenEventIds.values().next().value);
  }
  state.lastEventId = item.id;
  window.dispatchEvent(new CustomEvent("tengshou:change", { detail: item }));
  notifyDataChanged([item.entity || ""]);
  pendingEntities.add(item.entity || "");
  if (isAgentEntry(item)) pendingAgentActivity = true;
  scheduleRealtimeRefresh(350);
}

async function flushRealtimeRefresh() {
  const entities = new Set(pendingEntities);
  pendingEntities.clear();
  const agentActivity = pendingAgentActivity;
  pendingAgentActivity = false;
  if (!entities.size) return;
  const needFields = entities.has("field");
  const needBootstrap = entities.has("contract") || needFields;
  const jobs = [];
  if (needFields) jobs.push(loadFields());
  if (entities.has("template")) jobs.push(loadTemplates());
  try {
    await Promise.all(jobs);
  } catch (_) {
    /* transient: leave the previous data on screen */
  }
  if (needBootstrap) {
    const bootstrap = await api("/app/api/v1/bootstrap").catch(() => null);
    if (bootstrap) {
      state.bootstrap = bootstrap;
      renderMeta();
    }
  }
  if (state.tab === "changelog") {
    loadChangelog().catch(() => {});
  } else {
    loadRecentActivity().catch(() => {});
  }
  if (state.tab === "settings" && (entities.has("settings") || agentActivity)) {
    loadMCPStatus().catch(() => {});
  }
}

document.addEventListener("visibilitychange", () => {
  if (!document.hidden) pollEventsOnce();
});

/* ---------- 页面切换 ---------- */

function switchTab(tab) {
  state.tab = tab;
  $$("#tabs button").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
  $$(".view").forEach((v) => v.classList.toggle("active", v.id === `view-${tab}`));
  // 已迁移的 Vue 域据此在重新显示时刷新（等价于迁移前 switchTab 内联的
  // loadXxx 调用；例如新手引导会重跑自检与模板体检）。
  window.dispatchEvent(new CustomEvent("tengshou:tab-change", { detail: { tab } }));
  if (tab === "changelog") loadChangelog().catch((err) => toast(err.message, true));
  if (tab === "data") loadBackupStatus().catch((err) => toast(err.message, true));
  if (tab === "settings") loadMCPStatus().catch((err) => toast(err.message, true));
}

async function refreshAfterWrite() {
  await Promise.all([loadFields(), loadTemplates()]);
  renderMeta();
  const bootstrap = await api("/app/api/v1/bootstrap").catch(() => null);
  if (bootstrap) {
    state.bootstrap = bootstrap;
    renderMeta();
  }
  // 已迁移的 Vue 域（新手引导、合同台账、相对方档案、字段管理、公章、模板）
  // 不复用这里的 DOM，但需要知道数据已经变了：它们据此重取各自的列表。
  notifyDataChanged(["field", "party", "contract", "seal", "template"]);
  if (state.tab === "changelog") {
    loadChangelog().catch((err) => toast(err.message, true));
  }
}

function openExcelImport() {
  const input = $("#importFile");
  if (!input) return;
  input.value = "";
  input.click();
}

/* ---------- 事件绑定与启动 ---------- */

function bindEvents() {
  $$("#tabs button").forEach((button) => {
    button.addEventListener("click", () => switchTab(button.dataset.tab));
  });
  $("#exportBackupBtn").addEventListener("click", exportBackup);
  $("#restoreBackupBtn").addEventListener("click", () => $("#backupFileInput").click());
  $("#backupFileInput").addEventListener("change", restoreBackupFromFile);
  $("#checkIntegrityBtn").addEventListener("click", checkIntegrity);
  document.addEventListener("click", (event) => {
    const button = event.target.closest("[data-export-signature]");
    if (button) exportSignature(button.dataset.exportSignature);
  });
  $("#importFile").addEventListener("change", (e) => {
    const file = e.target.files && e.target.files[0];
    if (file) openImport(file);
  });
  $("#changeEntityFilter").addEventListener("change", () => {
    loadChangelog().catch((err) => toast(err.message, true));
  });
  $("#changeActorFilter").addEventListener("change", () => {
    loadChangelog().catch((err) => toast(err.message, true));
  });
  $("#refreshChangelogBtn").addEventListener("click", () => {
    loadChangelog().catch((err) => toast(err.message, true));
  });
  $("#refreshMCPStatusBtn").addEventListener("click", () => {
    loadMCPStatus().catch((err) => toast(err.message, true));
  });
}

async function boot() {
  bindEvents();
  try {
    await Promise.all([loadBootstrap(), loadFields(), loadTemplates()]);
    renderMeta();
  } catch (err) {
    toast(err.message, true);
  }
  // B8: a fresh vault starts on the guide, not on an empty ledger. The guide
  // is a normal tab, so every other page stays one click away.
  if (state.onboarding && state.onboarding.show_guide) {
    switchTab("onboarding");
  }
  // Start the change feed even when the initial load failed: it is what
  // surfaces data written by agents while the page is open.
  startRealtime();
}

/* 合同详情里的签名摘要仍由 app.js 渲染（扫码签名域尚未迁移）：Vue 合同弹窗
   保留宿主 #contractSignatures，挂载后把根节点与详情交过来。生成面板已迁到
   Vue（frontend/src/components/contracts/ContractGenerationPanel.vue）。 */
function mountContractSignatures(root, detail) {
  if (!root || !detail) return;
  const signatureHost = $("#contractSignatures", root);
  if (signatureHost && detail.signatures && detail.signatures.length) {
    signatureHost.innerHTML = signaturesSummaryHTML(detail);
  }
}

// 迁移期跨域桥：Vue 域借用 app.js 的 toast/整体刷新/切 tab/签名/批量/Excel
// 导入与合同详情签名摘要；随对应域迁完移除。
window.__tengshouApp = {
  toast,
  refreshAfterWrite,
  switchTab,
  openSignatureDialog,
  openBatchGenerateDialog,
  openExcelImport,
  mountContractSignatures,
};

document.addEventListener("DOMContentLoaded", boot);
