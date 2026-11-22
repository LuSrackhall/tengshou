/* 腾手本地应用界面。纯原生 JS，无构建步骤；数据来自本机 /app/api/v1。 */
"use strict";

const state = {
  bootstrap: null,
  fields: { contract: [], party: [] },
  contracts: [],
  contractTotal: 0,
  parties: [],
  partyTotal: 0,
  seals: [],
  sealTotal: 0,
  sealQ: "",
  templates: [],
  templateTotal: 0,
  templateQ: "",
  converters: [],
  years: [],
  year: "",
  contractQ: "",
  status: "",
  partyQ: "",
  fieldEntity: "contract",
  showArchivedFields: false,
  changelog: [],
  tab: "contracts",
  /* 实时同步游标：最近一条变更记录的 id（服务端用它补发之后的事件）。 */
  lastEventId: "",
  /* 当前打开详情弹窗的审计目标，用于外部写入时只刷新审计区块。 */
  auditTarget: null,
};

/* PDF.js renders imported template PDFs. The worker is vendored locally so
   the desktop app stays fully offline. */
if (typeof pdfjsLib !== "undefined") {
  pdfjsLib.GlobalWorkerOptions.workerSrc = "/app/vendor/pdf.worker.min.js";
}

const TYPE_LABELS = {
  text: "文本", longtext: "长文本", number: "数字", money: "金额", date: "日期",
  select: "单选", bool: "是/否", image: "图片路径", party: "相对方", status: "状态",
};

const ENTITY_LABELS = {
  contract: "合同", party: "相对方", field: "字段", seal: "公章", template: "模板",
  signature: "签名", backup: "备份", settings: "设置",
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

function debounce(fn, ms) {
  let timer = null;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

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

function money(value) {
  const n = Number(value || 0);
  return "¥" + n.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function statusLabel(s) { return s === "active" ? "生效" : s === "archived" ? "归档" : "草稿"; }
function kindLabel(k) { return k === "organization" ? "单位" : "个人"; }

function activeField(entity) { return state.fields[entity].filter((f) => !f.archived); }
function fieldByKey(entity, key) { return state.fields[entity].find((f) => f.key === key); }
function partyName(id) {
  const p = state.parties.find((x) => x.id === id);
  return p ? p.name : "";
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
  destroyTemplateEditor();
  state.auditTarget = null;
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
  state.years = data.years || [];
  const hasCurrent = state.years.some((y) => y.year === data.current_year);
  state.year = hasCurrent ? String(data.current_year) : (state.years.length ? String(state.years[0].year) : "");
  renderMeta();
  renderYearSelect();
}

async function loadFields() {
  const data = await api("/app/api/v1/fields?archived=1");
  state.fields = { contract: data.contract || [], party: data.party || [] };
}

async function loadParties() {
  const params = new URLSearchParams({ limit: "500" });
  if (state.partyQ) params.set("q", state.partyQ);
  const data = await api("/app/api/v1/parties?" + params.toString());
  state.parties = data.items || [];
  state.partyTotal = data.total || 0;
}

async function loadSeals() {
  const params = new URLSearchParams({ limit: "500" });
  if (state.sealQ) params.set("q", state.sealQ);
  const data = await api("/app/api/v1/seals?" + params.toString());
  state.seals = data.items || [];
  state.sealTotal = data.total || 0;
}

async function loadTemplates() {
  const params = new URLSearchParams({ limit: "500", kind: "pdf" });
  if (state.templateQ) params.set("q", state.templateQ);
  const data = await api("/app/api/v1/templates?" + params.toString());
  state.templates = data.items || [];
  state.templateTotal = data.total || 0;
}

async function loadConverters() {
  try {
    const data = await api("/app/api/v1/converters");
    state.converters = data.items || [];
  } catch (_) {
    state.converters = [];
  }
}

async function loadContracts() {
  const params = new URLSearchParams({ limit: "500" });
  if (state.year) params.set("year", state.year);
  if (state.status) params.set("status", state.status);
  if (state.contractQ) params.set("q", state.contractQ);
  const data = await api("/app/api/v1/contracts?" + params.toString());
  state.contracts = data.items || [];
  state.contractTotal = data.total || 0;
  applyExportLink();
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

function applyExportLink() {
  const button = $("#exportLedgerBtn");
  if (button) button.title = (state.year ? state.year + " 年度" : "全部年度") + "台账 Excel";
}

function exportLedgerPath() {
  const params = new URLSearchParams();
  if (state.year) params.set("year", state.year);
  if (state.status) params.set("status", state.status);
  if (state.contractQ) params.set("q", state.contractQ);
  return "/app/api/v1/excel/ledger" + (params.toString() ? "?" + params.toString() : "");
}

async function downloadWorkbook(kind) {
  const path = kind === "template" ? "/app/api/v1/excel/template" : exportLedgerPath();
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
    let filename = kind === "template" ? "tengshou-entry-template.xlsx" : "tengshou-ledger.xlsx";
    const disposition = res.headers.get("Content-Disposition") || "";
    const match = /filename="([^"]+)"/.exec(disposition);
    if (match) filename = match[1];
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  } catch (err) {
    toast(err.message, true);
  }
}

async function reloadAll() {
  await Promise.all([loadFields(), loadParties(), loadContracts(), loadSeals(), loadTemplates()]);
  renderMeta();
  renderContracts();
  renderParties();
  renderFields();
  renderSeals();
  renderTemplates();
}

/* ---------- 顶栏 / 年度 ---------- */

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
    <span>字段 ${stats.fields || 0} 个</span>`;
}

function renderYearSelect() {
  const select = $("#yearSelect");
  const options = [`<option value="">全部年度（跨年查询）</option>`];
  for (const yc of state.years) {
    options.push(`<option value="${yc.year}">${yc.year} 年（${yc.count} 份）</option>`);
  }
  if (!state.years.length && state.bootstrap) {
    options.push(`<option value="${state.bootstrap.current_year}">${state.bootstrap.current_year} 年</option>`);
  }
  select.innerHTML = options.join("");
  select.value = state.year;
}

/* ---------- 合同台账 ---------- */

function contractFieldValue(contract, field) {
  switch (field.key) {
    case "contract_no": return { text: contract.contract_no, mono: true };
    case "year": return { text: String(contract.year || "") };
    case "title": return { text: contract.title, wrap: true };
    case "type": return { text: contract.type || "—" };
    case "party_b": return { text: partyName(contract.party_b_id) || "—" };
    case "amount": return { text: money(contract.amount), money: true };
    case "signed_on": return { text: contract.signed_on || "—" };
    case "valid_from": return { text: contract.valid_from || "—" };
    case "valid_to": return { text: contract.valid_to || "—" };
    case "status": return { badge: contract.status, text: statusLabel(contract.status) };
  }
  const value = contract.data ? contract.data[field.key] : null;
  return { text: formatDynamic(field, value) };
}

function formatDynamic(field, value) {
  if (value == null || value === "") return "—";
  switch (field.type) {
    case "money": return money(value);
    case "number": return Number(value).toLocaleString("zh-CN", { maximumFractionDigits: 6 });
    case "bool": return value === true ? "是" : value === false ? "否" : String(value);
    default: return String(value);
  }
}

function renderContracts() {
  const table = $("#contractTable");
  const fields = activeField("contract").filter((f) => f.in_ledger);
  const yearLabel = state.year ? `${state.year} 年度` : "全部年度";
  $("#contractSummary").textContent =
    `${yearLabel} · 共 ${state.contractTotal} 份${state.status ? " · 状态：" + statusLabel(state.status) : ""}` +
    (state.contractQ ? ` · 搜索「${state.contractQ}」` : "");
  if (!fields.length) {
    table.innerHTML = `<tbody><tr><td class="empty">没有可显示的台账字段，请先在「字段」页新增字段。</td></tr></tbody>`;
    return;
  }
  const head = fields.map((f) => `<th>${esc(f.label)}</th>`).join("");
  const rows = state.contracts.map((contract) => {
    const cells = fields.map((f) => {
      const v = contractFieldValue(contract, f);
      if (v.badge) return `<td><span class="badge ${esc(v.badge)}">${esc(v.text)}</span></td>`;
      const cls = [v.mono ? "mono" : "", v.money ? "money" : "", v.wrap ? "wrap" : ""].filter(Boolean).join(" ");
      return `<td class="${cls}">${esc(v.text)}</td>`;
    }).join("");
    return `<tr data-id="${esc(contract.id)}">
      ${cells}
      <td><div class="actions">
        <button type="button" data-action="sign">扫码签名</button>
        <button type="button" data-action="edit">编辑</button>
        <button type="button" class="danger" data-action="delete">删除</button>
      </div></td>
    </tr>`;
  }).join("");
  table.innerHTML = `<thead><tr>${head}<th>操作</th></tr></thead>
    <tbody>${rows || `<tr><td class="empty" colspan="${fields.length + 1}">还没有合同。可以新建，或下载 Excel 模板填写后导入。</td></tr>`}</tbody>`;
  $$("#contractTable tbody tr[data-id]").forEach((tr) => {
    tr.addEventListener("click", (e) => {
      const button = e.target.closest("button[data-action]");
      const id = tr.dataset.id;
      const contract = state.contracts.find((c) => c.id === id);
      if (!contract) return;
      if (button && button.dataset.action === "delete") {
        e.stopPropagation();
        deleteContract(contract);
        return;
      }
      if (button && button.dataset.action === "sign") {
        e.stopPropagation();
        openSignatureDialog(contract);
        return;
      }
      openContractForm(contract);
    });
  });
}

/* ---------- 合同表单 ---------- */

function templateHint(f) {
  if (!f.in_template || f.entity !== "contract") return "";
  const core = {
    contract_no: ".Contract.ContractNo", year: ".Contract.Year", title: ".Contract.Title",
    type: ".Contract.Type", amount: ".Contract.Amount", signed_on: ".Contract.SignedOn",
    valid_from: ".Contract.ValidFrom", valid_to: ".Contract.ValidTo", status: ".Contract.Status",
  };
  if (core[f.key]) return `模板：<code>{{${core[f.key]}}}</code>`;
  if (f.key === "party_b") return "模板：<code>{{.PartyB.Name}}</code>";
  return `模板：<code>{{.Data.${f.key}}}</code>`;
}

function fieldControlHTML(entity, f, value, opts) {
  const id = `f_${entity}_${f.key}`;
  const required = f.required ? `<span class="req">*</span>` : "";
  const readonly = opts && opts.readonly ? " disabled" : "";
  let control = "";
  const type = f.type;
  if (type === "longtext") {
    control = `<textarea id="${id}"${readonly}>${esc(value == null ? "" : value)}</textarea>`;
  } else if (type === "select" || type === "status") {
    const options = [`<option value="">${f.required ? "请选择" : "（空）"}</option>`];
    for (const opt of f.options || []) {
      options.push(`<option value="${esc(opt)}"${String(value) === opt ? " selected" : ""}>${esc(opt)}</option>`);
    }
    control = `<select id="${id}"${readonly}>${options.join("")}</select>`;
  } else if (type === "bool") {
    const yes = value === true || value === "是";
    const no = value === false || value === "否";
    control = `<select id="${id}"${readonly}>
      <option value="">（空）</option>
      <option value="是"${yes ? " selected" : ""}>是</option>
      <option value="否"${no ? " selected" : ""}>否</option>
    </select>`;
  } else if (type === "party") {
    control = `<input id="${id}" type="text" value="${esc(value == null ? "" : value)}"
      list="partyNameList" placeholder="输入或选择相对方名称"${readonly}>`;
  } else {
    let inputType = "text";
    let step = "";
    if (type === "number") { inputType = "number"; step = ` step="any"`; }
    if (type === "money") { inputType = "number"; step = ` step="0.01" min="0"`; }
    if (type === "date") inputType = "date";
    control = `<input id="${id}" type="${inputType}"${step}
      value="${esc(value == null ? "" : value)}"${readonly}>`;
  }
  const help = [f.note ? esc(f.note) : "", templateHint(f), f.built_in ? "" : `键：${esc(f.key)}`].filter(Boolean).join(" · ");
  return `<div class="field ${type === "longtext" ? "full" : ""}" data-key="${esc(f.key)}">
    <label for="${id}">${esc(f.label)}${required}</label>
    ${control}
    ${help ? `<span class="help">${help}</span>` : ""}
  </div>`;
}

function contractControlValue(f, contract, partyBName) {
  if (!contract) {
    if (f.built_in) {
      if (f.key === "year") return state.bootstrap ? state.bootstrap.current_year : "";
      if (f.key === "status") return "草稿";
      if (f.key === "party_b") return "";
      return f.default_value || "";
    }
    return f.default_value || "";
  }
  switch (f.key) {
    case "contract_no": return contract.contract_no;
    case "year": return contract.year;
    case "title": return contract.title;
    case "type": return contract.type || "";
    case "party_b": return partyBName || "";
    case "amount": return contract.amount || "";
    case "signed_on": return contract.signed_on || "";
    case "valid_from": return contract.valid_from || "";
    case "valid_to": return contract.valid_to || "";
    case "status": return statusLabel(contract.status);
  }
  return contract.data ? contract.data[f.key] : "";
}

async function openContractForm(contract) {
  let detail = null;
  if (contract) {
    try {
      detail = await api(`/app/api/v1/contracts/${encodeURIComponent(contract.id)}`);
    } catch (err) {
      toast(err.message, true);
      return;
    }
  }
  const partyBName = detail && detail.party_b ? detail.party_b.name : partyName(contract && contract.party_b_id);
  const partyBID = detail && detail.party_b ? detail.party_b.id : (contract ? contract.party_b_id || "" : "");
  const fields = activeField("contract");
  const modal = openModal(contract ? `编辑合同 · ${contract.contract_no}` : "新建合同", { wide: true });
  const form = fields.map((f) => fieldControlHTML("contract", f,
    contractControlValue(f, contract, partyBName),
    { readonly: contract && f.key === "contract_no" })).join("");
  modal.body.innerHTML = `
    <div id="contractErrors"></div>
    ${contract ? `<div class="warning-list">合同编号创建后不可修改。修改会写入变更记录。</div>` : ""}
    ${detail && detail.signatures && detail.signatures.length ? signaturesSummaryHTML(detail) : ""}
    ${detail && detail.seals && detail.seals.length ? sealsSummaryHTML(detail) : ""}
    <datalist id="partyNameList">
      ${state.parties.map((p) => `<option value="${esc(p.name)}"></option>`).join("")}
    </datalist>
    <div class="form-grid">${form}</div>
    ${contract ? `<div id="contractHistory" class="audit-host"></div>` : ""}`;
  if (contract) {
    state.auditTarget = { entity: "contract", id: contract.id, selector: "#contractHistory" };
    loadAuditHistory("contract", contract.id, "#contractHistory");
  }
  footerButton(modal, "取消", "", closeModal);
  if (contract) {
    footerButton(modal, "删除", "danger", () => deleteContract(contract, modal));
  }
  const save = footerButton(modal, "保存", "primary", async () => {
    save.disabled = true;
    $("#contractErrors", modal.body).innerHTML = "";
    try {
      const input = collectContractInput(fields, contract, partyBID, partyBName);
      const res = await api(contract
        ? `/app/api/v1/contracts/${encodeURIComponent(contract.id)}`
        : "/app/api/v1/contracts",
        { method: contract ? "PUT" : "POST", body: input });
      closeModal();
      toast("合同已保存");
      if (res.warnings && res.warnings.length) {
        setTimeout(() => toast(res.warnings.join("；")), 350);
      }
      await refreshAfterWrite();
    } catch (err) {
      save.disabled = false;
      $("#contractErrors", modal.body).innerHTML = fieldErrorsHTML(err);
      modal.body.scrollTop = 0;
    }
  });
}

function collectContractInput(fields, contract, partyBID, partyBName) {
  const input = { data: {}, party_a_id: contract ? contract.party_a_id || "" : "", template_id: contract ? contract.template_id || "" : "" };
  for (const f of fields) {
    const el = document.getElementById(`f_contract_${f.key}`);
    const value = el ? el.value.trim() : "";
    if (!f.built_in) {
      input.data[f.key] = value;
      continue;
    }
    switch (f.key) {
      case "contract_no": input.contract_no = value; break;
      case "year": input.year = Number(value) || 0; break;
      case "title": input.title = value; break;
      case "type": input.type = value; break;
      case "party_b": input.party_b = value; break;
      case "amount": input.amount = Number(value) || 0; break;
      case "signed_on": input.signed_on = value; break;
      case "valid_from": input.valid_from = value; break;
      case "valid_to": input.valid_to = value; break;
      case "status": input.status = value; break;
    }
  }
  if (partyBID && input.party_b && input.party_b === partyBName) {
    input.party_b_id = partyBID;
  }
  return input;
}

async function deleteContract(contract, modal) {
  if (!window.confirm(`确定删除合同 ${contract.contract_no}「${contract.title}」吗？删除会记入变更记录。`)) return;
  try {
    await api(`/app/api/v1/contracts/${encodeURIComponent(contract.id)}`, { method: "DELETE" });
    if (modal) closeModal();
    toast("合同已删除");
    await refreshAfterWrite();
  } catch (err) {
    toast(err.message, true);
  }
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

/* ---------- 公章图片 ---------- */

function sealImageURL(seal) {
  return `/app/api/v1/seals/${encodeURIComponent(seal.id)}/image`;
}

function sealsSummaryHTML(detail) {
  const items = detail.seals.map((seal) =>
    `<li>${esc(seal.name)} · ${seal.width_px}×${seal.height_px}px · <code>${esc(seal.sha256.slice(0, 12))}</code></li>`).join("");
  return `<div class="signature-summary seal-summary"><strong>已关联公章 ${detail.seals.length} 枚</strong>
    <ul>${items}</ul></div>`;
}

function renderSeals() {
  const grid = $("#sealGrid");
  if (!grid) return;
  $("#sealSummary").textContent =
    `共 ${state.sealTotal} 枚公章图片${state.sealQ ? ` · 搜索「${state.sealQ}」` : ""}`;
  if (!state.seals.length) {
    grid.innerHTML = `<div class="empty">还没有公章图片。点击「导入公章图片」上传 PNG / JPG。</div>`;
    return;
  }
  grid.innerHTML = state.seals.map((seal) => {
    const owner = seal.party_id ? (partyName(seal.party_id) || "单位档案") : "未关联单位";
    return `<div class="seal-card" data-id="${esc(seal.id)}">
      <div class="seal-thumb"><img src="${sealImageURL(seal)}" alt="${esc(seal.name)}" loading="lazy"></div>
      <div class="seal-card-body">
        <div class="seal-name">${esc(seal.name)}</div>
        <div class="seal-meta">${esc(owner)} · ${seal.width_px}×${seal.height_px}px</div>
        <div class="seal-meta mono">sha256 ${esc(seal.sha256.slice(0, 12))}…</div>
        <div class="seal-meta">已关联 ${seal.contract_count || 0} 份合同</div>
        <div class="seal-actions">
          <button type="button" data-action="preview">预览</button>
          <button type="button" data-action="attach">关联合同</button>
          <button type="button" data-action="replace">替换</button>
          <button type="button" class="danger" data-action="delete">删除</button>
        </div>
      </div>
    </div>`;
  }).join("");
  $$("#sealGrid .seal-card").forEach((card) => {
    card.addEventListener("click", (e) => {
      const button = e.target.closest("button[data-action]");
      const seal = state.seals.find((s) => s.id === card.dataset.id);
      if (!seal) return;
      const action = button ? button.dataset.action : "preview";
      if (action === "attach") { openSealAttach(seal); return; }
      if (action === "replace") { replaceSealImage(seal); return; }
      if (action === "delete") { deleteSeal(seal); return; }
      openSealPreview(seal);
    });
  });
}

async function openSealImport(file) {
  if (file.size > 12 * 1024 * 1024) {
    toast("公章图片文件过大，请压缩到 12MB 以内", true);
    return;
  }
  let base64;
  try {
    base64 = await readFileBase64(file);
  } catch (err) {
    toast(err.message, true);
    return;
  }
  const defaultName = file.name.replace(/\.[^.]+$/, "");
  const parties = state.parties.map((p) =>
    `<option value="${esc(p.id)}">${esc(p.name)}（${kindLabel(p.kind)}）</option>`).join("");
  const modal = openModal(`导入公章图片 · ${file.name}`);
  modal.body.innerHTML = `
    <div id="sealImportErrors"></div>
    <div class="form-grid">
      <div class="field"><label for="sealName">公章名称<span class="req">*</span></label>
        <input id="sealName" type="text" value="${esc(defaultName)}" placeholder="例如：幸福村股份经济合作社公章"></div>
      <div class="field"><label for="sealParty">所属单位（可选）</label>
        <select id="sealParty"><option value="">（暂不关联）</option>${parties}</select>
        <span class="help">关联单位后，该单位的合同可按模板自动取用这枚公章。</span></div>
      <div class="field full"><label for="sealNote">说明</label>
        <input id="sealNote" type="text" placeholder="可选，例如印章类型或保管人"></div>
      <div class="field full"><label>图片预览</label>
        <div class="seal-import-preview"><img src="data:${esc(file.type || "image/png")};base64,${base64}" alt="公章预览"></div>
        <span class="help">支持 PNG / JPG。PNG 保留透明底；JPG 或白底图片会自动去除白底并裁剪空白边距。</span></div>
    </div>`;
  footerButton(modal, "取消", "", closeModal);
  const save = footerButton(modal, "导入", "primary", async () => {
    save.disabled = true;
    $("#sealImportErrors", modal.body).innerHTML = "";
    try {
      await api("/app/api/v1/seals", {
        method: "POST",
        body: {
          name: $("#sealName", modal.body).value.trim(),
          party_id: $("#sealParty", modal.body).value,
          note: $("#sealNote", modal.body).value.trim(),
          file_base64: base64,
        },
      });
      closeModal();
      toast("公章图片已导入");
      await refreshAfterWrite();
    } catch (err) {
      save.disabled = false;
      $("#sealImportErrors", modal.body).innerHTML = fieldErrorsHTML(err);
    }
  });
}

function sealPlacementSnippet(seal, widthMM, align) {
  const source = seal.party_id ? "sealPath .Seals .PartyA" : "contractSealPath .ContractSeals";
  const textAlign = align === "left" ? "left" : align === "right" ? "right" : "center";
  return `{{with ${source}}}<p style="text-align:${textAlign}"><img src="{{.}}" style="width:${widthMM}mm"></p>{{end}}`;
}

function openSealPreview(seal) {
  const owner = seal.party_id ? (partyName(seal.party_id) || "单位档案") : "未关联单位";
  const modal = openModal(`公章预览 · ${seal.name}`, { wide: true });
  modal.body.innerHTML = `
    <div class="seal-preview">
      <div class="seal-a4">
        <div class="seal-a4-page">
          <div class="seal-a4-row" id="sealA4Row"><img id="sealA4Img" src="${sealImageURL(seal)}" alt="${esc(seal.name)}" style="width:40mm"></div>
          <div class="seal-a4-label">A4 页面 · 盖章位置示意</div>
        </div>
        <p class="hint">这里只示意模板中的水平位置与图片大小，方便调整模板占位；实际换行与分页由模板排版决定。</p>
      </div>
      <div class="seal-preview-side">
        <div class="seal-meta">${esc(owner)} · ${seal.width_px}×${seal.height_px}px</div>
        <div class="seal-meta mono">sha256 ${esc(seal.sha256)}</div>
        <div class="seal-meta">已关联 ${seal.contract_count || 0} 份合同</div>
        ${seal.note ? `<div class="seal-meta">说明：${esc(seal.note)}</div>` : ""}
        <div class="field"><label for="sealWidth">盖章宽度</label>
          <select id="sealWidth">${[30, 35, 40, 45, 50].map((w) => `<option value="${w}"${w === 40 ? " selected" : ""}>${w} mm</option>`).join("")}</select></div>
        <div class="field"><label for="sealAlign">水平位置</label>
          <select id="sealAlign">
            <option value="left">靠左</option>
            <option value="center" selected>居中</option>
            <option value="right">靠右</option>
          </select></div>
        <div class="field"><label>模板占位</label>
          <pre id="sealSnippet" class="snippet"></pre>
          <span class="help">把这段复制到模板的盖章位置；公章属于乙方时把 <code>.PartyA</code> 换成 <code>.PartyB</code>。换模板时按新模板里的位置重新落位，图片本身可以继续复用。</span></div>
      </div>
    </div>`;
  const row = $("#sealA4Row", modal.body);
  const img = $("#sealA4Img", modal.body);
  const snippet = $("#sealSnippet", modal.body);
  const sync = () => {
    const width = Number($("#sealWidth", modal.body).value) || 40;
    const align = $("#sealAlign", modal.body).value;
    img.style.width = width + "mm";
    row.style.justifyContent = align === "left" ? "flex-start" : align === "right" ? "flex-end" : "center";
    snippet.textContent = sealPlacementSnippet(seal, width, align);
  };
  $("#sealWidth", modal.body).addEventListener("change", sync);
  $("#sealAlign", modal.body).addEventListener("change", sync);
  sync();
  footerButton(modal, "关闭", "", closeModal);
  footerButton(modal, "复制模板占位", "primary", async () => {
    const ok = await copyText(snippet.textContent);
    toast(ok ? "模板占位已复制" : "复制失败，请手动选择文本", !ok);
  });
}

async function openSealAttach(seal) {
  let linked = [];
  try {
    const res = await api(`/app/api/v1/seals/${encodeURIComponent(seal.id)}/contracts`);
    linked = res.contract_ids || [];
  } catch (err) {
    toast(err.message, true);
    return;
  }
  let contracts = [];
  let contractTotal = 0;
  try {
    const res = await api("/app/api/v1/contracts?limit=500");
    contracts = res.items || [];
    contractTotal = res.total || contracts.length;
  } catch (err) {
    toast(err.message, true);
    return;
  }
  const linkedSet = new Set(linked);
  const visible = new Set(contracts.map((contract) => contract.id));
  const rows = contracts.map((contract) => `
    <label class="seal-contract-row">
      <input type="checkbox" class="seal-contract-check" data-id="${esc(contract.id)}"${linkedSet.has(contract.id) ? " checked" : ""}>
      <span class="mono">${esc(contract.contract_no)}</span>
      <span>${esc(contract.title)}</span>
      <span class="seal-meta">${contract.year} 年</span>
    </label>`).join("");
  const truncated = contractTotal > contracts.length
    ? `<p class="hint">合同较多，这里只列出前 ${contracts.length} 份（共 ${contractTotal} 份）；未列出的合同会保持原有关联不变。</p>`
    : "";
  const modal = openModal(`关联合同 · ${seal.name}`, { wide: true });
  modal.body.innerHTML = `
    <p class="hint">勾选要使用这枚公章的合同，可以一次选择多份（批量落位）。取消勾选会解除对应合同与这枚公章的关联。</p>
    ${truncated}
    <div class="seal-contract-list">${rows || `<div class="empty">还没有合同。</div>`}</div>`;
  footerButton(modal, "取消", "", closeModal);
  const save = footerButton(modal, "保存关联", "primary", async () => {
    save.disabled = true;
    try {
      const selected = new Set($$(".seal-contract-check:checked", modal.body).map((el) => el.dataset.id));
      const add = [...selected].filter((id) => !linkedSet.has(id));
      const remove = [...linkedSet].filter((id) => visible.has(id) && !selected.has(id));
      if (add.length) {
        await api(`/app/api/v1/seals/${encodeURIComponent(seal.id)}/attach`, {
          method: "POST", body: { contract_ids: add },
        });
      }
      if (remove.length) {
        await api(`/app/api/v1/seals/${encodeURIComponent(seal.id)}/detach`, {
          method: "POST", body: { contract_ids: remove },
        });
      }
      closeModal();
      toast(`公章关联已保存：新增 ${add.length} 份，解除 ${remove.length} 份`);
      await refreshAfterWrite();
    } catch (err) {
      save.disabled = false;
      toast(err.message, true);
    }
  });
}

function pickImageFile(callback) {
  const input = document.createElement("input");
  input.type = "file";
  input.accept = "image/png,image/jpeg";
  input.addEventListener("change", () => {
    const file = input.files && input.files[0];
    if (file) callback(file);
  });
  input.click();
}

function replaceSealImage(seal) {
  if (!window.confirm(`确定替换公章图片「${seal.name}」吗？替换后生成的新 PDF 使用新图片，历史文件不受影响；替换会记入变更记录。`)) return;
  pickImageFile(async (file) => {
    let base64;
    try {
      base64 = await readFileBase64(file);
    } catch (err) {
      toast(err.message, true);
      return;
    }
    try {
      await api(`/app/api/v1/seals/${encodeURIComponent(seal.id)}/replace`, {
        method: "POST", body: { file_base64: base64 },
      });
      toast("公章图片已替换");
      await refreshAfterWrite();
    } catch (err) {
      toast(err.message, true);
    }
  });
}

async function deleteSeal(seal) {
  const linked = seal.contract_count || 0;
  const extra = linked ? `，并解除 ${linked} 份合同的关联` : "";
  if (!window.confirm(`确定删除公章图片「${seal.name}」吗${extra}？删除会记入变更记录。`)) return;
  try {
    await api(`/app/api/v1/seals/${encodeURIComponent(seal.id)}`, { method: "DELETE" });
    toast("公章图片已删除");
    await refreshAfterWrite();
  } catch (err) {
    toast(err.message, true);
  }
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch (_) {
    const area = document.createElement("textarea");
    area.value = text;
    document.body.appendChild(area);
    area.select();
    let ok = false;
    try { ok = document.execCommand("copy"); } catch (_) { /* ignore */ }
    area.remove();
    return ok;
  }
}

/* ---------- 模板管理（Word → PDF 套印，B3） ---------- */

let templateEditor = null;

function templateConverterLabel(converter) {
  if (converter === "word") return "Microsoft Word";
  if (converter === "libreoffice") return "LibreOffice";
  if (converter === "pdf") return "PDF 直接导入";
  return converter || "—";
}

function renderTemplates() {
  const table = $("#templateTable");
  if (!table) return;
  const familyNames = new Set(state.templates.map((t) => t.name));
  $("#templateSummary").textContent =
    `共 ${familyNames.size} 个模板 · ${state.templateTotal} 个版本` +
    (state.templateQ ? ` · 搜索「${state.templateQ}」` : "");
  const head = ["模板名称", "版本", "页数", "转换方式", "标定位置", "状态", "更新时间", "操作"]
    .map((h) => `<th>${h}</th>`).join("");
  const rows = state.templates.map((tpl) => {
    const positions = (tpl.placements || []).length;
    return `<tr data-id="${esc(tpl.id)}">
      <td class="wrap">${esc(tpl.name)}</td>
      <td><span class="badge ${tpl.is_active ? "active" : ""}">v${tpl.version}</span></td>
      <td class="row-num">${tpl.page_count || 0}</td>
      <td>${esc(templateConverterLabel(tpl.converter))}</td>
      <td class="row-num">${positions}</td>
      <td>${tpl.is_active ? "当前版本" : "历史版本"}</td>
      <td class="row-num">${esc(fmtDate(tpl.updated_at))}</td>
      <td><div class="actions">
        <button type="button" data-action="edit">标定与生成</button>
        <button type="button" data-action="source">原文件</button>
        <button type="button" class="danger" data-action="delete">删除</button>
      </div></td>
    </tr>`;
  }).join("");
  table.innerHTML = `<thead><tr>${head}</tr></thead>
    <tbody>${rows || `<tr><td class="empty" colspan="8">还没有 PDF 模板。点击「导入模板」选择您的 Word 合同模板（或另存好的 PDF）。</td></tr>`}</tbody>`;
  $$("#templateTable tbody tr[data-id]").forEach((tr) => {
    tr.addEventListener("click", (e) => {
      const button = e.target.closest("button[data-action]");
      const tpl = state.templates.find((t) => t.id === tr.dataset.id);
      if (!tpl) return;
      const action = button ? button.dataset.action : "edit";
      if (action === "delete") {
        e.stopPropagation();
        deleteTemplateVersion(tpl);
        return;
      }
      if (action === "source") {
        e.stopPropagation();
        const ext = (String(tpl.source_path || "").match(/\.[a-z0-9]+$/i) || [".docx"])[0];
        downloadAsset(
          `/app/api/v1/templates/${encodeURIComponent(tpl.id)}/source`,
          `${tpl.name}-v${tpl.version}${ext}`);
        return;
      }
      openTemplateEditor(tpl).catch((err) => toast(err.message, true));
    });
  });
}

async function deleteTemplateVersion(tpl) {
  if (!window.confirm(`确定删除模板「${tpl.name}」v${tpl.version} 吗？已被合同或 PDF 引用的版本会保留。`)) return;
  try {
    await api(`/app/api/v1/templates/${encodeURIComponent(tpl.id)}`, { method: "DELETE" });
    toast(`模板「${tpl.name}」v${tpl.version} 已删除`);
    await loadTemplates();
    renderTemplates();
    await loadBootstrapAndMeta();
  } catch (err) {
    toast(err.message, true);
  }
}

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
  state.years = bootstrap.years || [];
  renderMeta();
  renderYearSelect();
}

async function openTemplateImport(file) {
  if (file.size > 40 * 1024 * 1024) {
    toast("模板文件过大，请压缩到 40MB 以内", true);
    return;
  }
  let base64;
  try {
    base64 = await readFileBase64(file);
  } catch (err) {
    toast(err.message, true);
    return;
  }
  await loadConverters();
  const ext = (file.name.match(/\.[^.]+$/) || [""])[0].toLowerCase();
  const isPDF = ext === ".pdf";
  const converterRows = (state.converters || []).map((c) =>
    `<li>${esc(c.label)}：${c.available ? `可用（${esc(c.detail || "")}）` : `不可用（${esc(c.detail || "")}）`}</li>`).join("");
  const anyConverter = (state.converters || []).some((c) => c.available);
  const warning = (!isPDF && !anyConverter)
    ? `<div class="warning-list">本机未检测到 Microsoft Word 或 LibreOffice。Word 转换会失败；
        可以安装 LibreOffice（免费），或先用 Word / WPS 将文档另存为 PDF，再导入 PDF。</div>`
    : "";
  const defaultName = file.name.replace(/\.[^.]+$/, "");
  const modal = openModal(`导入模板 · ${file.name}`, { wide: true });
  modal.body.innerHTML = `
    <div id="templateImportErrors"></div>
    ${warning}
    <div class="form-grid">
      <div class="field"><label for="tplImportName">模板名称<span class="req">*</span></label>
        <input id="tplImportName" type="text" value="${esc(defaultName)}" placeholder="例如：房屋租赁合同（村委会版）">
        <span class="help">同名再导入会增加新版本；旧版本和已生成 PDF 都会保留。</span></div>
      <div class="field"><label>文件</label>
        <div class="mono">${esc(file.name)} · ${(file.size / 1024 / 1024).toFixed(2)} MB</div>
        <span class="help">${isPDF ? "直接作为模板 PDF 导入，不经过转换。" : "将转换为 PDF 后用于所见即所得的标定。"}</span></div>
      <div class="field full"><label>转换程序</label>
        <ul class="import-notes">${converterRows || "<li>未检测到转换程序</li>"}</ul>
        ${!isPDF ? `<span class="help">优先使用本机 Microsoft Word（Windows，保真度最高）；否则用 LibreOffice，可能出现细微排版差异。</span>` : ""}
      </div>
    </div>`;
  footerButton(modal, "取消", "", closeModal);
  const save = footerButton(modal, "导入", "primary", async () => {
    save.disabled = true;
    $("#templateImportErrors", modal.body).innerHTML = "";
    try {
      const res = await api("/app/api/v1/templates/import", {
        method: "POST",
        body: {
          name: $("#tplImportName", modal.body).value.trim(),
          file_name: file.name,
          file_base64: base64,
        },
      });
      closeModal();
      const warnings = res.warnings || [];
      toast(warnings.length ? warnings[0] : `模板已导入（v${res.item.version}）`);
      if (warnings.length > 1) setTimeout(() => toast(warnings.slice(1).join("；")), 400);
      await loadTemplates();
      renderTemplates();
      await loadBootstrapAndMeta();
      openTemplateEditor(res.item).catch((err) => toast(err.message, true));
    } catch (err) {
      save.disabled = false;
      $("#templateImportErrors", modal.body).innerHTML = fieldErrorsHTML(err);
    }
  });
}

function templateFieldOptionsHTML() {
  const groups = [];
  const push = (label, fields, prefix) => {
    const options = [];
    for (const f of fields) {
      if (f.archived) continue;
      if (!f.built_in && f.type === "image") continue;
      const key = f.built_in ? `${prefix}.${f.key}` : `${prefix}.data.${f.key}`;
      options.push(`<option value="${esc(key)}">${esc(label)} · ${esc(f.label)}</option>`);
    }
    if (options.length) {
      groups.push(`<optgroup label="${esc(label)}">${options.join("")}</optgroup>`);
    }
  };
  push("合同", state.fields.contract, "contract");
  push("甲方", state.fields.party, "party_a");
  push("乙方", state.fields.party, "party_b");
  return groups.join("");
}

function templateSealOptionsHTML() {
  if (!state.seals.length) {
    return `<option value="">（还没有公章图片）</option>`;
  }
  return state.seals.map((seal) =>
    `<option value="${esc(seal.id)}">${esc(seal.name)}${seal.party_id ? "（已关联单位）" : ""}</option>`).join("");
}

function templateBindingLabel(p) {
  if (p.type === "signature") {
    return `签名 · ${p.party === "party_a" ? "甲方" : "乙方"}`;
  }
  if (p.type === "seal") {
    if (p.seal_mode === "seal" && p.seal_id) {
      const seal = state.seals.find((s) => s.id === p.seal_id);
      return `公章 · ${seal ? seal.name : "指定图片"}`;
    }
    const party = p.party === "party_a" ? "（甲方）" : p.party === "party_b" ? "（乙方）" : "";
    return `公章 · 合同关联/单位公章${party}`;
  }
  const key = String(p.field || "");
  const prefix = key.split(".")[0];
  const role = prefix === "party_a" ? "甲方" : prefix === "party_b" ? "乙方" : "合同";
  const dataKey = key.includes(".data.") ? key.split(".data.")[1] : key.split(".")[1];
  const list = prefix === "contract" ? state.fields.contract : state.fields.party;
  const field = list.find((f) => f.key === dataKey);
  return `${role} · ${field ? field.label : key}`;
}

function newPlacementID() {
  return "p_" + Date.now().toString(36) + Math.random().toString(36).slice(2, 7);
}

async function openTemplateEditor(template) {
  const modal = openModal(`模板标定 · ${template.name} v${template.version}`, { full: true });
  modal.foot.style.display = "none";
  const placements = JSON.parse(JSON.stringify(template.placements || []));
  const editor = {
    modal,
    template,
    placements,
    scale: 1.25,
    selected: null,
    dirty: false,
    preview: null,
    pdfDoc: null,
    pdfBytes: null,
    pageObjs: [],
    pageSizes: [],
    renderToken: 0,
    previewDoc: null,
    previewOverlay: null,
    contracts: [],
  };
  templateEditor = editor;
  modal.body.innerHTML = `
    <div class="template-editor">
      <div class="tpl-toolbar">
        <span class="tpl-current" id="tplCurrent">v${template.version} · ${template.page_count || 0} 页 · ${placements.length} 个位置</span>
        <label class="inline">缩放
          <select id="tplZoom">
            ${[0.75, 1, 1.25, 1.5, 2].map((z) => `<option value="${z}"${z === 1.25 ? " selected" : ""}>${Math.round(z * 100)}%</option>`).join("")}
          </select>
        </label>
        <span class="spacer"></span>
        <button type="button" id="tplSaveBtn">保存为新版本</button>
        <button type="button" id="tplCloseBtn">关闭</button>
      </div>
      <div class="tpl-body">
        <div class="tpl-pages" id="tplPages"><div class="tpl-loading">正在加载模板 PDF…</div></div>
        <aside class="tpl-side">
          <h3>新增位置</h3>
          <div class="tpl-add">
            <div class="field"><label for="tplAddKind">类型</label>
              <select id="tplAddKind">
                <option value="field">字段文字</option>
                <option value="signature">手写签名</option>
                <option value="seal">公章图片</option>
              </select></div>
            <div class="field" id="tplAddFieldWrap"><label for="tplAddField">字段</label>
              <select id="tplAddField">${templateFieldOptionsHTML()}</select></div>
            <div class="field" id="tplAddPartyWrap" style="display:none"><label for="tplAddParty">签名方 / 公章所属方</label>
              <select id="tplAddParty">
                <option value="">（自动 / 不限）</option>
                <option value="party_b">乙方</option>
                <option value="party_a">甲方</option>
              </select></div>
            <div class="field" id="tplAddSealModeWrap" style="display:none"><label for="tplAddSealMode">公章来源</label>
              <select id="tplAddSealMode">
                <option value="contract">合同关联 / 单位公章（自动）</option>
                <option value="seal">指定公章图片</option>
              </select></div>
            <div class="field" id="tplAddSealWrap" style="display:none"><label for="tplAddSeal">公章图片</label>
              <select id="tplAddSeal">${templateSealOptionsHTML()}</select></div>
            <button type="button" id="tplAddPlace">添加位置</button>
            <span class="help">添加后拖动框到目标位置，右下角可缩放大小。每次保存都会生成新版本。</span>
          </div>
          <h3>位置列表</h3>
          <div id="tplPlacementList" class="tpl-list"></div>
          <div id="tplSelectedPanel"></div>
          <h3>预览与生成</h3>
          <div id="tplUsage" class="tpl-status">正在统计影响范围…</div>
          <div class="field"><label for="tplContract">合同</label>
            <select id="tplContract"><option value="">（正在加载合同…）</option></select></div>
          <div class="tpl-add" style="grid-template-columns:1fr 1fr">
            <button type="button" id="tplPreviewSample">样例预览</button>
            <button type="button" id="tplPreviewContract">合同预览</button>
          </div>
          <button type="button" id="tplGenerate" class="primary" disabled>生成 PDF</button>
          <div id="tplPreviewStatus" class="tpl-status"></div>
          <div id="tplWarnings" class="tpl-warnings"></div>
          <div id="tplResult"></div>
        </aside>
      </div>
    </div>`;
  bindTemplateEditorEvents(editor);
  renderPlacementList(editor);
  updateTemplatePreviewStatus(editor);
  loadTemplateUsage(editor).catch(() => {});
  loadEditorContracts(editor).catch(() => {});
  try {
    await loadTemplateEditorPDF(editor);
  } catch (err) {
    $("#tplPages", modal.body).innerHTML =
      `<div class="tpl-loading">模板 PDF 加载失败：${esc(err.message)}</div>`;
  }
}

function destroyTemplateEditor() {
  const editor = templateEditor;
  if (!editor) return;
  templateEditor = null;
  try { if (editor.pdfDoc) editor.pdfDoc.destroy(); } catch (_) { /* ignore */ }
  try { if (editor.previewDoc) editor.previewDoc.destroy(); } catch (_) { /* ignore */ }
}

async function loadTemplateEditorPDF(editor) {
  const res = await fetch(`/app/api/v1/templates/${encodeURIComponent(editor.template.id)}/pdf`);
  if (!res.ok) throw new Error(`请求失败（${res.status}）`);
  const bytes = new Uint8Array(await res.arrayBuffer());
  editor.pdfBytes = bytes;
  editor.pdfDoc = await pdfjsLib.getDocument({
    data: bytes.slice(0),
    isEvalSupported: false,
    useSystemFonts: true,
  }).promise;
  editor.pageObjs = [];
  editor.pageSizes = [];
  for (let i = 1; i <= editor.pdfDoc.numPages; i++) {
    const page = await editor.pdfDoc.getPage(i);
    const base = page.getViewport({ scale: 1 });
    editor.pageObjs[i] = page;
    editor.pageSizes[i - 1] = { width: base.width, height: base.height };
  }
  await renderTemplateEditorPages(editor);
}

async function renderTemplateEditorPages(editor) {
  const pagesEl = $("#tplPages", editor.modal.body);
  if (!pagesEl) return;
  const token = ++editor.renderToken;
  pagesEl.innerHTML = "";
  const count = editor.pdfDoc.numPages;
  const dpr = window.devicePixelRatio || 1;
  for (let i = 1; i <= count; i++) {
    if (token !== editor.renderToken) return;
    const page = editor.pageObjs[i] || await editor.pdfDoc.getPage(i);
    editor.pageObjs[i] = page;
    const viewport = page.getViewport({ scale: editor.scale });
    const wrap = document.createElement("div");
    wrap.className = "tpl-page";
    wrap.dataset.page = String(i);
    wrap.style.width = viewport.width + "px";
    wrap.style.height = viewport.height + "px";
    const canvas = document.createElement("canvas");
    canvas.width = Math.floor(viewport.width * dpr);
    canvas.height = Math.floor(viewport.height * dpr);
    canvas.style.width = viewport.width + "px";
    canvas.style.height = viewport.height + "px";
    const boxes = document.createElement("div");
    boxes.className = "tpl-boxes";
    const pageNo = document.createElement("div");
    pageNo.className = "tpl-page-no";
    pageNo.textContent = `第 ${i} / ${count} 页`;
    wrap.append(canvas, boxes, pageNo);
    pagesEl.appendChild(wrap);
    await page.render({
      canvasContext: canvas.getContext("2d"),
      viewport,
      transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : null,
    }).promise;
    if (token !== editor.renderToken) return;
    renderTemplateBoxes(editor, i, boxes);
  }
}

function renderTemplateBoxes(editor, pageNumber, boxesEl) {
  boxesEl.innerHTML = "";
  const scale = editor.scale;
  for (const p of editor.placements) {
    if (p.page !== pageNumber) continue;
    boxesEl.appendChild(makeTemplateBox(editor, p, scale));
  }
}

function makeTemplateBox(editor, p, scale) {
  const el = document.createElement("div");
  const typeClass = p.type === "signature" ? "sig" : p.type === "seal" ? "seal" : "";
  el.className = ["tpl-box", typeClass, editor.selected === p.id ? "selected" : ""].filter(Boolean).join(" ");
  el.dataset.id = p.id;
  positionTemplateBox(el, p, scale);
  el.innerHTML = `<span class="tpl-box-label">${esc(templateBindingLabel(p))}</span>
    <span class="tpl-handle"></span>
    <button type="button" class="tpl-box-del" title="删除位置">×</button>`;
  el.addEventListener("pointerdown", (e) => startTemplateBoxDrag(editor, p, el, e));
  const handle = el.querySelector(".tpl-handle");
  handle.addEventListener("pointerdown", (e) => startTemplateBoxResize(editor, p, el, e));
  el.querySelector(".tpl-box-del").addEventListener("click", (e) => {
    e.stopPropagation();
    removeTemplatePlacement(editor, p.id);
  });
  return el;
}

function positionTemplateBox(el, p, scale) {
  el.style.left = (p.x * scale) + "px";
  el.style.top = (p.y * scale) + "px";
  el.style.width = (p.w * scale) + "px";
  el.style.height = (p.h * scale) + "px";
}

function startTemplateBoxDrag(editor, p, el, e) {
  if (e.target.closest(".tpl-handle") || e.target.closest(".tpl-box-del")) return;
  e.preventDefault();
  selectTemplatePlacement(editor, p.id);
  const page = editor.pageSizes[p.page - 1];
  const startX = e.clientX;
  const startY = e.clientY;
  const origin = { x: p.x, y: p.y };
  const move = (ev) => {
    const dx = (ev.clientX - startX) / editor.scale;
    const dy = (ev.clientY - startY) / editor.scale;
    p.x = roundPt(clampNumber(origin.x + dx, 0, page.width - p.w));
    p.y = roundPt(clampNumber(origin.y + dy, 0, page.height - p.h));
    positionTemplateBox(el, p, editor.scale);
    markTemplateDirty(editor);
  };
  const up = () => {
    el.removeEventListener("pointermove", move);
    el.removeEventListener("pointerup", up);
    el.removeEventListener("pointercancel", up);
    renderPlacementList(editor);
    renderSelectedPanel(editor);
  };
  el.setPointerCapture && el.setPointerCapture(e.pointerId);
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", up);
  el.addEventListener("pointercancel", up);
}

function startTemplateBoxResize(editor, p, el, e) {
  e.preventDefault();
  e.stopPropagation();
  selectTemplatePlacement(editor, p.id);
  const page = editor.pageSizes[p.page - 1];
  const startX = e.clientX;
  const startY = e.clientY;
  const origin = { w: p.w, h: p.h };
  const move = (ev) => {
    const dx = (ev.clientX - startX) / editor.scale;
    const dy = (ev.clientY - startY) / editor.scale;
    p.w = roundPt(clampNumber(origin.w + dx, 6, page.width - p.x));
    p.h = roundPt(clampNumber(origin.h + dy, 6, page.height - p.y));
    positionTemplateBox(el, p, editor.scale);
    markTemplateDirty(editor);
  };
  const up = () => {
    el.removeEventListener("pointermove", move);
    el.removeEventListener("pointerup", up);
    el.removeEventListener("pointercancel", up);
    renderPlacementList(editor);
    renderSelectedPanel(editor);
  };
  el.setPointerCapture && el.setPointerCapture(e.pointerId);
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", up);
  el.addEventListener("pointercancel", up);
}

function selectTemplatePlacement(editor, id) {
  editor.selected = id;
  $$(".tpl-box", editor.modal.body).forEach((el) => {
    el.classList.toggle("selected", el.dataset.id === id);
  });
  renderPlacementList(editor);
  renderSelectedPanel(editor);
}

function renderPlacementList(editor) {
  const list = $("#tplPlacementList", editor.modal.body);
  if (!list) return;
  if (!editor.placements.length) {
    list.innerHTML = `<div class="tpl-list-empty">还没有标定位置。在上方选择字段/签名/公章后点击「添加位置」。</div>`;
    return;
  }
  list.innerHTML = editor.placements.map((p) => `
    <div class="tpl-list-item${editor.selected === p.id ? " selected" : ""}" data-id="${esc(p.id)}">
      <span class="grow">${esc(templateBindingLabel(p))}</span>
      <span class="tpl-kind">第 ${p.page} 页</span>
      <button type="button" data-action="remove" title="删除">×</button>
    </div>`).join("");
  $$(".tpl-list-item", list).forEach((item) => {
    item.addEventListener("click", (e) => {
      const remove = e.target.closest("button[data-action='remove']");
      if (remove) {
        e.stopPropagation();
        removeTemplatePlacement(editor, item.dataset.id);
        return;
      }
      selectTemplatePlacement(editor, item.dataset.id);
      const pageEl = $(`.tpl-page[data-page="${editor.placements.find((p) => p.id === item.dataset.id)?.page || 1}"]`, editor.modal.body);
      const boxEl = pageEl ? $(`.tpl-box[data-id="${item.dataset.id}"]`, pageEl) : null;
      if (boxEl) boxEl.scrollIntoView({ block: "center", behavior: "smooth" });
    });
  });
}

function renderSelectedPanel(editor) {
  const panel = $("#tplSelectedPanel", editor.modal.body);
  if (!panel) return;
  const p = editor.placements.find((item) => item.id === editor.selected);
  if (!p) {
    panel.innerHTML = `<div class="tpl-status">点击页面上的框或上方列表进行编辑。</div>`;
    return;
  }
  if (p.type !== "field") {
    panel.innerHTML = `<div class="tpl-add">
      <div class="tpl-status">${esc(templateBindingLabel(p))} · 第 ${p.page} 页 · ${Math.round(p.w)}×${Math.round(p.h)} pt</div>
      <button type="button" class="danger" id="tplRemoveSelected">删除这个位置</button>
    </div>`;
    $("#tplRemoveSelected", panel).addEventListener("click", () => removeTemplatePlacement(editor, p.id));
    return;
  }
  panel.innerHTML = `<div class="tpl-add">
    <div class="tpl-status">${esc(templateBindingLabel(p))} · 第 ${p.page} 页 · ${Math.round(p.w)}×${Math.round(p.h)} pt</div>
    <div class="field"><label for="tplFontSize">字号（pt，0 = 自动缩放）</label>
      <input id="tplFontSize" type="number" min="0" max="72" step="0.5" value="${p.font_size || 0}"></div>
    <div class="field"><label for="tplAlign">对齐</label>
      <select id="tplAlign">
        <option value="left"${(p.align || "left") === "left" ? " selected" : ""}>左对齐</option>
        <option value="center"${p.align === "center" ? " selected" : ""}>居中</option>
        <option value="right"${p.align === "right" ? " selected" : ""}>右对齐</option>
      </select></div>
    <button type="button" class="danger" id="tplRemoveSelected">删除这个位置</button>
  </div>`;
  $("#tplFontSize", panel).addEventListener("change", (e) => {
    const value = Number(e.target.value) || 0;
    p.font_size = value > 0 ? value : 0;
    markTemplateDirty(editor);
    toast(value > 0 ? `字号设为 ${value}pt（放不下时仍会自动缩小）` : "字号改为自动缩放");
  });
  $("#tplAlign", panel).addEventListener("change", (e) => {
    p.align = e.target.value;
    markTemplateDirty(editor);
  });
  $("#tplRemoveSelected", panel).addEventListener("click", () => removeTemplatePlacement(editor, p.id));
}

function removeTemplatePlacement(editor, id) {
  editor.placements = editor.placements.filter((p) => p.id !== id);
  if (editor.selected === id) editor.selected = null;
  markTemplateDirty(editor);
  rerenderTemplateBoxes(editor);
  renderPlacementList(editor);
  renderSelectedPanel(editor);
}

function rerenderTemplateBoxes(editor) {
  $$(".tpl-page", editor.modal.body).forEach((pageEl) => {
    const pageNumber = Number(pageEl.dataset.page);
    const boxes = $(".tpl-boxes", pageEl);
    if (boxes) renderTemplateBoxes(editor, pageNumber, boxes);
  });
}

function addTemplatePlacement(editor) {
  const kind = $("#tplAddKind", editor.modal.body).value;
  const page = 1;
  const pageSize = editor.pageSizes[page - 1];
  if (!pageSize) {
    toast("模板 PDF 尚未加载完成", true);
    return;
  }
  const placement = {
    id: newPlacementID(),
    page,
    x: 60,
    y: 100 + (editor.placements.filter((p) => p.page === page).length % 12) * 34,
    w: 180,
    h: 22,
    type: kind,
    field: "",
    party: "",
    seal_mode: "",
    seal_id: "",
    font_size: 0,
    align: "left",
  };
  if (kind === "field") {
    const key = $("#tplAddField", editor.modal.body).value;
    if (!key) {
      toast("请选择一个字段", true);
      return;
    }
    placement.field = key;
    placement.align = "left";
  } else if (kind === "signature") {
    placement.party = $("#tplAddParty", editor.modal.body).value;
    if (!placement.party) {
      toast("请选择签名方（甲方或乙方）", true);
      return;
    }
    placement.w = 130;
    placement.h = 50;
  } else {
    placement.seal_mode = $("#tplAddSealMode", editor.modal.body).value;
    placement.party = $("#tplAddParty", editor.modal.body).value;
    if (placement.seal_mode === "seal") {
      const sealID = $("#tplAddSeal", editor.modal.body).value;
      if (!sealID) {
        toast("请先选择或导入公章图片", true);
        return;
      }
      placement.seal_id = sealID;
    }
    placement.w = 110;
    placement.h = 110;
  }
  placement.x = roundPt(clampNumber(placement.x, 0, pageSize.width - placement.w));
  placement.y = roundPt(clampNumber(placement.y, 0, pageSize.height - placement.h));
  editor.placements.push(placement);
  markTemplateDirty(editor);
  rerenderTemplateBoxes(editor);
  selectTemplatePlacement(editor, placement.id);
  const pageEl = $(`.tpl-page[data-page="${page}"]`, editor.modal.body);
  const boxEl = pageEl ? $(`.tpl-box[data-id="${placement.id}"]`, pageEl) : null;
  if (boxEl) boxEl.scrollIntoView({ block: "center", behavior: "smooth" });
}

function markTemplateDirty(editor) {
  editor.dirty = true;
  editor.preview = null;
  updateTemplatePreviewStatus(editor);
}

function updateTemplatePreviewStatus(editor) {
  const status = $("#tplPreviewStatus", editor.modal.body);
  const generate = $("#tplGenerate", editor.modal.body);
  if (!status || !generate) return;
  let text = "请先预览（样例或合同）再生成 PDF。";
  let cls = "tpl-status";
  if (editor.dirty) {
    text = "有未保存的修改；预览或生成时会先自动保存为新版本。";
    cls = "tpl-status warn";
  } else if (editor.preview && !editor.preview.sample) {
    text = "已用目标合同预览，可以生成 PDF。";
    cls = "tpl-status ok";
  } else if (editor.preview && editor.preview.sample) {
    text = "已用样例数据预览；生成正式 PDF 前请用目标合同预览。";
    cls = "tpl-status warn";
  }
  status.textContent = text;
  status.className = cls;
  generate.disabled = !(editor.preview && !editor.preview.sample && !editor.dirty);
}

function updateTemplateEditorHeader(editor) {
  const item = editor.template;
  editor.modal.title.textContent = `模板标定 · ${item.name} v${item.version}`;
  const current = $("#tplCurrent", editor.modal.body);
  if (current) {
    current.textContent = `v${item.version} · ${item.page_count || 0} 页 · ${editor.placements.length} 个位置`;
  }
}

function bindTemplateEditorEvents(editor) {
  $("#tplZoom", editor.modal.body).addEventListener("change", (e) => {
    editor.scale = Number(e.target.value) || 1.25;
    renderTemplateEditorPages(editor).catch((err) => toast(err.message, true));
  });
  $("#tplAddKind", editor.modal.body).addEventListener("change", () => {
    const kind = $("#tplAddKind", editor.modal.body).value;
    $("#tplAddFieldWrap", editor.modal.body).style.display = kind === "field" ? "" : "none";
    $("#tplAddPartyWrap", editor.modal.body).style.display = kind === "signature" ? "" : "none";
    $("#tplAddSealModeWrap", editor.modal.body).style.display = kind === "seal" ? "" : "none";
    syncTemplateAddSealControls(editor);
  });
  $("#tplAddSealMode", editor.modal.body).addEventListener("change", () => syncTemplateAddSealControls(editor));
  $("#tplAddPlace", editor.modal.body).addEventListener("click", () => addTemplatePlacement(editor));
  $("#tplSaveBtn", editor.modal.body).addEventListener("click", () => {
    saveTemplateMapping(editor).catch((err) => toast(err.message, true));
  });
  $("#tplCloseBtn", editor.modal.body).addEventListener("click", () => {
    if (editor.dirty && !window.confirm("还有未保存的标定，确定关闭吗？")) return;
    closeModal();
  });
  $("#tplPreviewSample", editor.modal.body).addEventListener("click", () => {
    previewTemplate(editor, true).catch((err) => toast(err.message, true));
  });
  $("#tplPreviewContract", editor.modal.body).addEventListener("click", () => {
    previewTemplate(editor, false).catch((err) => toast(err.message, true));
  });
  $("#tplGenerate", editor.modal.body).addEventListener("click", () => {
    generateTemplatePDF(editor).catch((err) => toast(err.message, true));
  });
  syncTemplateAddSealControls(editor);
}

function syncTemplateAddSealControls(editor) {
  const kind = $("#tplAddKind", editor.modal.body).value;
  const mode = $("#tplAddSealMode", editor.modal.body).value;
  const wrap = $("#tplAddSealWrap", editor.modal.body);
  wrap.style.display = kind === "seal" && mode === "seal" ? "" : "none";
  $("#tplAddPartyWrap", editor.modal.body).style.display =
    (kind === "signature" || kind === "seal") ? "" : "none";
}

async function loadTemplateUsage(editor) {
  const el = $("#tplUsage", editor.modal.body);
  try {
    const u = await api(`/app/api/v1/templates/${encodeURIComponent(editor.template.id)}/usage`);
    if (!el) return;
    el.textContent = `影响范围：${u.family_contracts} 份合同使用该模板系列` +
      `（其中 ${u.other_version_contracts} 份仍用旧版本）· 已生成 ${u.documents} 份 PDF`;
  } catch (err) {
    if (el) el.textContent = "影响范围统计失败：" + err.message;
  }
}

async function loadEditorContracts(editor) {
  const select = $("#tplContract", editor.modal.body);
  try {
    const data = await api("/app/api/v1/contracts?limit=500");
    editor.contracts = data.items || [];
  } catch (_) {
    editor.contracts = [];
  }
  if (!editor.contracts.length) {
    select.innerHTML = `<option value="">（还没有合同，可先用样例预览）</option>`;
    return;
  }
  select.innerHTML = editor.contracts.map((c) =>
    `<option value="${esc(c.id)}">${esc(c.contract_no)} · ${esc(c.title)}</option>`).join("");
}

async function saveTemplateMapping(editor) {
  if (!editor.dirty) return editor.template;
  const payload = {
    placements: editor.placements.map((p) => ({
      id: p.id,
      page: p.page,
      x: p.x,
      y: p.y,
      w: p.w,
      h: p.h,
      type: p.type,
      field: p.field || "",
      party: p.party || "",
      seal_mode: p.seal_mode || "",
      seal_id: p.seal_id || "",
      font_size: p.font_size || 0,
      align: p.align || "left",
    })),
  };
  const res = await api(`/app/api/v1/templates/${encodeURIComponent(editor.template.id)}/mapping`, {
    method: "PUT",
    body: payload,
  });
  const item = res.item;
  editor.template = item;
  editor.placements = JSON.parse(JSON.stringify(item.placements || []));
  editor.selected = null;
  editor.dirty = false;
  editor.preview = null;
  updateTemplateEditorHeader(editor);
  updateTemplatePreviewStatus(editor);
  renderPlacementList(editor);
  renderSelectedPanel(editor);
  rerenderTemplateBoxes(editor);
  await loadTemplates();
  renderTemplates();
  await loadBootstrapAndMeta();
  loadTemplateUsage(editor).catch(() => {});
  const warnings = res.warnings || [];
  toast(`已保存为 v${item.version}`);
  if (warnings.length) setTimeout(() => toast(warnings.join("；")), 350);
  return item;
}

async function previewTemplate(editor, sample) {
  if (editor.dirty) {
    await saveTemplateMapping(editor);
  }
  const contractID = sample ? "" : $("#tplContract", editor.modal.body).value;
  if (!sample && !contractID) {
    toast("请先选择用于预览的合同", true);
    return;
  }
  const body = sample ? { sample: true } : { contract_id: contractID };
  const res = await api(`/app/api/v1/templates/${encodeURIComponent(editor.template.id)}/preview`, {
    method: "POST",
    body,
  });
  editor.preview = {
    sample: !!sample,
    contractID: sample ? "" : contractID,
    sha: res.preview_sha256,
  };
  const bytes = base64ToBytes(res.pdf_base64);
  const title = sample
    ? "样例数据预览"
    : `合同预览 · ${($("#tplContract", editor.modal.body).selectedOptions[0] || {}).textContent || ""}`;
  await showTemplatePreview(editor, bytes, res.warnings || [], title, null);
  renderTemplateWarnings(editor, res.warnings || []);
  updateTemplatePreviewStatus(editor);
}

function renderTemplateWarnings(editor, warnings) {
  const box = $("#tplWarnings", editor.modal.body);
  if (!box) return;
  if (!warnings.length) {
    box.innerHTML = `<div class="tpl-status ok">预览未发现缺失字段或越界问题。</div>`;
    return;
  }
  box.innerHTML = warnings.map((w) => `<div class="tpl-warning">${esc(w.message || w.code)}</div>`).join("");
}

async function showTemplatePreview(editor, bytes, warnings, title, download) {
  closeTemplatePreview(editor);
  const shell = $(".template-editor", editor.modal.body);
  if (!shell) return;
  const overlay = document.createElement("div");
  overlay.className = "tpl-preview-overlay";
  overlay.innerHTML = `
    <div class="tpl-preview-head">
      <strong>${esc(title)}</strong>
      <span class="spacer"></span>
      ${download ? `<button type="button" id="tplPreviewDownload">下载 PDF</button>` : ""}
      <button type="button" id="tplPreviewClose">关闭预览</button>
    </div>
    <div class="tpl-preview-pages"><div class="tpl-loading">正在渲染预览…</div></div>
    <div class="tpl-preview-warnings">${warnings.length
      ? warnings.map((w) => `<div class="tpl-warning">${esc(w.message || w.code)}</div>`).join("")
      : `<div class="tpl-status ok">未发现位置或数据问题。</div>`}</div>`;
  shell.appendChild(overlay);
  editor.previewOverlay = overlay;
  $("#tplPreviewClose", overlay).addEventListener("click", () => closeTemplatePreview(editor));
  if (download) {
    $("#tplPreviewDownload", overlay).addEventListener("click", () => {
      downloadAsset(download.path, download.filename);
    });
  }
  const pages = $(".tpl-preview-pages", overlay);
  try {
    editor.previewDoc = await renderPDFPagesInto(pages, bytes, 0.9);
  } catch (err) {
    pages.innerHTML = `<div class="tpl-loading">预览渲染失败：${esc(err.message)}</div>`;
  }
}

function closeTemplatePreview(editor) {
  if (editor.previewOverlay && editor.previewOverlay.parentNode) {
    editor.previewOverlay.parentNode.removeChild(editor.previewOverlay);
  }
  editor.previewOverlay = null;
  try { if (editor.previewDoc) editor.previewDoc.destroy(); } catch (_) { /* ignore */ }
  editor.previewDoc = null;
}

async function generateTemplatePDF(editor) {
  if (!editor.preview || editor.preview.sample) {
    toast("请先用目标合同预览，再生成 PDF", true);
    return;
  }
  if (editor.dirty) {
    toast("标定已修改，请重新预览后再生成", true);
    return;
  }
  const res = await api(`/app/api/v1/templates/${encodeURIComponent(editor.template.id)}/generate`, {
    method: "POST",
    body: {
      contract_id: editor.preview.contractID,
      preview_sha256: editor.preview.sha,
    },
  });
  const doc = res.item;
  const warnings = res.warnings || [];
  renderTemplateWarnings(editor, warnings);
  const result = $("#tplResult", editor.modal.body);
  const path = `/app/api/v1/documents/${encodeURIComponent(doc.id)}/file`;
  result.innerHTML = `<div class="tpl-add">
    <div class="tpl-status ok">已生成 PDF（${doc.page_count} 页，sha256 ${esc(String(doc.sha256 || "").slice(0, 12))}…）</div>
    <div class="doc-download">
      <button type="button" id="tplOpenGenerated">查看生成结果</button>
      <button type="button" id="tplDownloadGenerated">下载 PDF</button>
    </div>
  </div>`;
  $("#tplDownloadGenerated", result).addEventListener("click", () => downloadAsset(path, "contract.pdf"));
  $("#tplOpenGenerated", result).addEventListener("click", async () => {
    try {
      const resp = await fetch(path, { headers: { "X-Tengshou-App": "1" } });
      if (!resp.ok) throw new Error(`请求失败（${resp.status}）`);
      const bytes = new Uint8Array(await resp.arrayBuffer());
      await showTemplatePreview(editor, bytes, warnings, "生成结果", { path, filename: "contract.pdf" });
    } catch (err) {
      toast(err.message, true);
    }
  });
  result.scrollIntoView({ block: "nearest", behavior: "smooth" });
  toast(`已生成 PDF（v${doc.template_version}）`);
  await loadTemplates();
  renderTemplates();
  await loadBootstrapAndMeta();
  loadTemplateUsage(editor).catch(() => {});
}

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

function roundPt(v) { return Math.round(v * 10) / 10; }

function clampNumber(v, min, max) {
  if (max < min) max = min;
  if (v < min) return min;
  if (v > max) return max;
  return v;
}

/* ---------- 相对方档案 ---------- */

function partyFieldValue(party, field) {
  switch (field.key) {
    case "name": return party.name;
    case "kind": return kindLabel(party.kind);
    case "id_number": return party.id_number || "—";
    case "phone": return party.phone || "—";
    case "address": return party.address || "—";
    case "note": return party.note || "—";
  }
  const value = party.data ? party.data[field.key] : null;
  return formatDynamic(field, value);
}

function renderParties() {
  const table = $("#partyTable");
  const fields = activeField("party").filter((f) => f.in_ledger);
  $("#partySummary").textContent = `共 ${state.partyTotal} 个相对方${state.partyQ ? ` · 搜索「${state.partyQ}」` : ""}`;
  if (!fields.length) {
    table.innerHTML = `<tbody><tr><td class="empty">没有可显示的档案字段，请先在「字段」页新增。</td></tr></tbody>`;
    return;
  }
  const head = fields.map((f) => `<th>${esc(f.label)}</th>`).join("");
  const rows = state.parties.map((party) => {
    const cells = fields.map((f) =>
      `<td class="${f.key === "note" || f.key === "address" ? "wrap" : ""}">${esc(partyFieldValue(party, f))}</td>`).join("");
    return `<tr data-id="${esc(party.id)}">${cells}
      <td><div class="actions">
        <button type="button" data-action="edit">编辑</button>
        <button type="button" class="danger" data-action="delete">删除</button>
      </div></td></tr>`;
  }).join("");
  table.innerHTML = `<thead><tr>${head}<th>操作</th></tr></thead>
    <tbody>${rows || `<tr><td class="empty" colspan="${fields.length + 1}">还没有相对方档案。</td></tr>`}</tbody>`;
  $$("#partyTable tbody tr[data-id]").forEach((tr) => {
    tr.addEventListener("click", (e) => {
      const button = e.target.closest("button[data-action]");
      const party = state.parties.find((p) => p.id === tr.dataset.id);
      if (!party) return;
      if (button && button.dataset.action === "delete") {
        e.stopPropagation();
        deleteParty(party);
        return;
      }
      openPartyForm(party);
    });
  });
}

function partyControlValue(f, party) {
  if (!party) return f.built_in && f.key === "kind" ? "个人" : (f.default_value || "");
  switch (f.key) {
    case "name": return party.name;
    case "kind": return kindLabel(party.kind);
    case "id_number": return party.id_number || "";
    case "phone": return party.phone || "";
    case "address": return party.address || "";
    case "note": return party.note || "";
  }
  return party.data ? party.data[f.key] : "";
}

function openPartyForm(party) {
  const fields = activeField("party");
  const modal = openModal(party ? `编辑相对方 · ${party.name}` : "新建相对方");
  const form = fields.map((f) =>
    fieldControlHTML("party", f, partyControlValue(f, party), {})).join("");
  modal.body.innerHTML = `<div id="partyErrors"></div><div class="form-grid">${form}</div>
    ${party ? `<div id="partyHistory" class="audit-host"></div>` : ""}`;
  if (party) {
    state.auditTarget = { entity: "party", id: party.id, selector: "#partyHistory" };
    loadAuditHistory("party", party.id, "#partyHistory");
  }
  footerButton(modal, "取消", "", closeModal);
  if (party) footerButton(modal, "删除", "danger", () => deleteParty(party, modal));
  const save = footerButton(modal, "保存", "primary", async () => {
    save.disabled = true;
    $("#partyErrors", modal.body).innerHTML = "";
    try {
      const input = collectPartyInput(fields);
      const res = await api(party
        ? `/app/api/v1/parties/${encodeURIComponent(party.id)}`
        : "/app/api/v1/parties",
        { method: party ? "PUT" : "POST", body: input });
      closeModal();
      toast("相对方档案已保存");
      if (res.warnings && res.warnings.length) {
        setTimeout(() => toast(res.warnings.join("；")), 350);
      }
      await refreshAfterWrite();
    } catch (err) {
      save.disabled = false;
      $("#partyErrors", modal.body).innerHTML = fieldErrorsHTML(err);
      modal.body.scrollTop = 0;
    }
  });
}

function collectPartyInput(fields) {
  const input = { data: {} };
  for (const f of fields) {
    const el = document.getElementById(`f_party_${f.key}`);
    const value = el ? el.value.trim() : "";
    if (!f.built_in) {
      input.data[f.key] = value;
      continue;
    }
    switch (f.key) {
      case "name": input.name = value; break;
      case "kind": input.kind = value; break;
      case "id_number": input.id_number = value; break;
      case "phone": input.phone = value; break;
      case "address": input.address = value; break;
      case "note": input.note = value; break;
    }
  }
  return input;
}

async function deleteParty(party, modal) {
  if (!window.confirm(`确定删除相对方「${party.name}」吗？`)) return;
  try {
    await api(`/app/api/v1/parties/${encodeURIComponent(party.id)}`, { method: "DELETE" });
    if (modal) closeModal();
    toast("相对方已删除");
    await refreshAfterWrite();
  } catch (err) {
    toast(err.message, true);
  }
}

/* ---------- 字段管理 ---------- */

function renderFields() {
  const table = $("#fieldTable");
  const entity = state.fieldEntity;
  const fields = state.fields[entity].filter((f) => state.showArchivedFields || !f.archived);
  $("#fieldSummary").textContent =
    `${entity === "contract" ? "合同" : "相对方"}字段 ${fields.filter((f) => !f.archived).length} 个` +
    (fields.some((f) => f.archived) ? ` · 已归档 ${fields.filter((f) => f.archived).length} 个` : "");
  const head = ["名称", "键", "类型", "必填", "默认值", "台账", "模板", "排序", "说明", "操作"]
    .map((h) => `<th>${h}</th>`).join("");
  const rows = fields.map((f) => `
    <tr data-id="${esc(f.id)}" class="${f.archived ? "archived-row" : ""}">
      <td>${esc(f.label)}${f.built_in ? ` <span class="badge">内置</span>` : ""}${f.archived ? ` <span class="badge archived">已归档</span>` : ""}</td>
      <td class="mono">${esc(f.key)}</td>
      <td>${esc(TYPE_LABELS[f.type] || f.type)}</td>
      <td>${f.required ? "是" : "否"}</td>
      <td>${esc(f.default_value || "—")}</td>
      <td>${f.in_ledger ? "是" : "否"}</td>
      <td>${f.in_template ? "是" : "否"}</td>
      <td class="row-num">${f.sort_order}</td>
      <td class="wrap">${esc(f.note || "")}</td>
      <td><div class="actions">
        <button type="button" data-action="edit">编辑</button>
        ${!f.built_in && !f.archived ? `<button type="button" class="danger" data-action="archive">归档</button>` : ""}
        ${!f.built_in && f.archived ? `<button type="button" data-action="restore">恢复</button>` : ""}
      </div></td>
    </tr>`).join("");
  table.innerHTML = `<thead><tr>${head}</tr></thead>
    <tbody>${rows || `<tr><td class="empty" colspan="10">还没有字段。</td></tr>`}</tbody>`;
  $$("#fieldTable tbody tr[data-id]").forEach((tr) => {
    tr.addEventListener("click", (e) => {
      const button = e.target.closest("button[data-action]");
      const field = state.fields[entity].find((f) => f.id === tr.dataset.id);
      if (!field) return;
      if (button && button.dataset.action === "archive") {
        e.stopPropagation();
        archiveField(field);
        return;
      }
      if (button && button.dataset.action === "restore") {
        e.stopPropagation();
        restoreField(field);
        return;
      }
      openFieldForm(field);
    });
  });
}

function openFieldForm(field) {
  const isEdit = !!field;
  const entity = field ? field.entity : state.fieldEntity;
  const modal = openModal(isEdit ? `编辑字段 · ${field.label}` : "新建字段");
  const types = Object.keys(TYPE_LABELS).filter((t) => t !== "party" && t !== "status");
  const type = field ? field.type : "text";
  const optionsText = field && field.options ? field.options.join("\n") : "";
  modal.body.innerHTML = `
    <div id="fieldErrors"></div>
    <div class="form-grid">
      <div class="field"><label for="fieldLabel">字段名称<span class="req">*</span></label>
        <input id="fieldLabel" type="text" value="${esc(field ? field.label : "")}" placeholder="例如：平方数"></div>
      <div class="field"><label for="fieldKey">字段键<span class="req">*</span></label>
        <input id="fieldKey" type="text" class="mono" value="${esc(field ? field.key : "")}"
          placeholder="area_sqm" ${isEdit ? "disabled" : ""}>
        <span class="help">小写字母、数字、下划线；用于数据键与模板占位符，创建后不可修改。</span></div>
      <div class="field"><label for="fieldType">类型</label>
        <select id="fieldType" ${isEdit && field.built_in ? "disabled" : ""}>
          ${types.map((t) => `<option value="${t}"${t === type ? " selected" : ""}>${TYPE_LABELS[t]}</option>`).join("")}
          ${field && (type === "party" || type === "status") ? `<option value="${type}" selected>${TYPE_LABELS[type]}</option>` : ""}
        </select></div>
      <div class="field"><label for="fieldSort">排序</label>
        <input id="fieldSort" type="number" value="${field ? field.sort_order : ""}" placeholder="留空自动排到最后"></div>
      <div class="field"><label for="fieldDefault">默认值</label>
        <input id="fieldDefault" type="text" value="${esc(field ? field.default_value || "" : "")}"></div>
      <div class="field full" id="fieldOptionsWrap"><label for="fieldOptions">选项（单选字段，每行一个）</label>
        <textarea id="fieldOptions" placeholder="每行一个选项">${esc(optionsText)}</textarea></div>
      <div class="field full"><label for="fieldNote">说明</label>
        <input id="fieldNote" type="text" value="${esc(field ? field.note || "" : "")}"></div>
      <div class="field full">
        <div class="check-row"><input id="fieldRequired" type="checkbox" ${field && field.required ? "checked" : ""}> 必填</div>
        <div class="check-row"><input id="fieldLedger" type="checkbox" ${!field || field.in_ledger ? "checked" : ""}> 出现在台账与 Excel</div>
        <div class="check-row"><input id="fieldTemplate" type="checkbox" ${!field || field.in_template ? "checked" : ""}> 用于模板占位符</div>
      </div>
    </div>`;
  const typeSelect = $("#fieldType", modal.body);
  const optionsWrap = $("#fieldOptionsWrap", modal.body);
  const syncOptions = () => { optionsWrap.style.display = typeSelect.value === "select" ? "" : "none"; };
  typeSelect.addEventListener("change", syncOptions);
  syncOptions();
  footerButton(modal, "取消", "", closeModal);
  const save = footerButton(modal, "保存", "primary", async () => {
    save.disabled = true;
    $("#fieldErrors", modal.body).innerHTML = "";
    const input = {
      entity,
      key: $("#fieldKey", modal.body).value.trim(),
      label: $("#fieldLabel", modal.body).value.trim(),
      type: typeSelect.value,
      required: $("#fieldRequired", modal.body).checked,
      default_value: $("#fieldDefault", modal.body).value.trim(),
      options: $("#fieldOptions", modal.body).value.split(/\n+/).map((s) => s.trim()).filter(Boolean),
      in_ledger: $("#fieldLedger", modal.body).checked,
      in_template: $("#fieldTemplate", modal.body).checked,
      note: $("#fieldNote", modal.body).value.trim(),
    };
    const sortRaw = $("#fieldSort", modal.body).value.trim();
    if (sortRaw !== "") input.sort_order = Number(sortRaw);
    try {
      await api(isEdit
        ? `/app/api/v1/fields/${encodeURIComponent(field.id)}`
        : "/app/api/v1/fields",
        { method: isEdit ? "PUT" : "POST", body: input });
      closeModal();
      toast("字段已保存");
      await refreshAfterWrite();
    } catch (err) {
      save.disabled = false;
      $("#fieldErrors", modal.body).innerHTML = fieldErrorsHTML(err);
    }
  });
}

async function restoreField(field) {
  try {
    await api(`/app/api/v1/fields/${encodeURIComponent(field.id)}`, {
      method: "PUT",
      body: { archived: false },
    });
    toast("字段已恢复");
    await refreshAfterWrite();
  } catch (err) {
    toast(err.message, true);
  }
}

async function archiveField(field) {
  if (!window.confirm(`确定归档字段「${field.label}」吗？字段会从录入和 Excel 中隐藏，已录数据保留。`)) return;
  try {
    await api(`/app/api/v1/fields/${encodeURIComponent(field.id)}/archive`, { method: "POST", body: {} });
    toast("字段已归档");
    await refreshAfterWrite();
  } catch (err) {
    toast(err.message, true);
  }
}

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

/* ---------- 审计详情（合同 / 相对方弹窗内的变更记录） ---------- */

function auditHistoryHTML(items) {
  if (!items.length) {
    return `<div class="audit-block"><div class="audit-head">变更记录</div>
      <div class="empty">这条记录还没有变更历史。</div></div>`;
  }
  return `<div class="audit-block">
    <div class="audit-head">变更记录 <span class="muted">（最近 ${items.length} 条）</span></div>
    <ul class="audit-list">${items.map((entry) => `
      <li>
        <div class="audit-line">
          <span class="recent-time">${esc(fmtDate(entry.created_at))}</span>
          ${actorBadgeHTML(entry)}
          <span class="badge ${esc(entry.action)}">${esc(ACTION_LABELS[entry.action] || entry.action)}</span>
          <span class="audit-summary">${esc(entry.summary)}</span>
        </div>
        ${changeDiffsHTML(entry)}
      </li>`).join("")}
    </ul></div>`;
}

async function loadAuditHistory(entity, id, selector) {
  const host = $(selector);
  if (!host || !id) return;
  const params = new URLSearchParams({ entity, entity_id: id, limit: "20" });
  try {
    const data = await api("/app/api/v1/changelog?" + params.toString());
    if (!host.isConnected) return; // this modal was closed or replaced meanwhile
    host.innerHTML = auditHistoryHTML(data.items || []);
  } catch (err) {
    if (host.isConnected) {
      host.innerHTML = `<div class="audit-block error">变更记录加载失败：${esc(err.message)}</div>`;
    }
  }
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
    options: { mode: "upsert", skip_errors: false, create_parties: true },
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

function renderImportRows(preview) {
  const rows = preview.rows || [];
  const shown = rows.slice(0, 200);
  const body = shown.map((row) => {
    const issues = [];
    for (const e of row.errors || []) {
      issues.push(`<li>${esc(e.label || e.field || "")}：${esc(e.message)}</li>`);
    }
    const warnings = (row.warnings || []).map((w) => `<li>${esc(w)}</li>`).join("");
    return `<tr>
      <td class="row-num">${row.row}</td>
      <td><span class="badge ${esc(row.action)}">${esc({ create: "新增", update: "更新", unchanged: "不变", error: "错误" }[row.action] || row.action)}</span></td>
      <td class="mono">${esc(row.contract_no || "")}</td>
      <td class="wrap">${esc(row.title || "")}</td>
      <td>${esc(row.party_b || "")}</td>
      <td class="wrap">
        ${issues.length ? `<ul class="row-errors">${issues.join("")}</ul>` : ""}
        ${warnings ? `<ul class="row-warnings">${warnings}</ul>` : ""}
      </td>
    </tr>`;
  }).join("");
  return `<div class="import-summary">
      <span class="badge create">新增 ${preview.summary.create}</span>
      <span class="badge update">更新 ${preview.summary.update}</span>
      <span class="badge unchanged">不变 ${preview.summary.unchanged}</span>
      <span class="badge error">错误 ${preview.summary.error}</span>
      <span class="badge">共 ${preview.summary.total} 行</span>
    </div>
    <div class="table-wrap"><table class="data-table import-table">
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
  if (!modal || !modal.body.querySelector("#importMode")) {
    return importState ? Object.assign({}, importState.options) : { mode: "upsert", skip_errors: false, create_parties: true };
  }
  return {
    mode: $("#importMode", modal.body).value,
    skip_errors: $("#importSkipErrors", modal.body).checked,
    create_parties: $("#importCreateParties", modal.body).checked,
  };
}

function currentMapping(modal) {
  const mapping = {};
  $$("select[data-map-index]", modal.body).forEach((select) => {
    if (select.value) mapping[select.dataset.mapIndex] = select.value;
  });
  return mapping;
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
      const body = {
        file_base64: importState.base64,
        options: Object.assign({}, importState.options, { mapping: currentMapping(modal) }),
      };
      const result = await api("/app/api/v1/excel/commit", { method: "POST", body });
      showImportResult(modal, result, false);
      toast(`导入完成：新增 ${result.created} 条，更新 ${result.updated} 条`);
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
}

function showImportResult(modal, result, blocked) {
  const target = $("#importResult", modal.body);
  if (!target) return;
  const rows = (result.rows || []).slice(0, 200).map((row) => {
    const errors = (row.errors || []).map((e) => `<li>${esc(e.label || "")}：${esc(e.message)}</li>`).join("");
    return `<tr>
      <td class="row-num">${row.row}</td>
      <td><span class="badge ${esc(row.action)}">${esc({ create: "新增", update: "更新", unchanged: "不变", error: "错误" }[row.action] || row.action)}</span></td>
      <td class="mono">${esc(row.contract_no || "")}</td>
      <td class="wrap">${esc(row.title || "")}</td>
      <td class="wrap">${errors ? `<ul class="row-errors">${errors}</ul>` : ""}</td>
    </tr>`;
  }).join("");
  target.innerHTML = `
    <div class="import-summary">
      <span class="badge create">新增 ${result.created || 0}</span>
      <span class="badge update">更新 ${result.updated || 0}</span>
      <span class="badge unchanged">不变 ${result.unchanged || 0}</span>
      <span class="badge error">错误 ${result.errors || 0}</span>
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
  const el = $("#liveStatus");
  if (!el) return;
  el.classList.remove("sse", "poll", "connecting", "offline");
  el.classList.add(mode);
  if (mode === "sse") {
    el.textContent = "实时同步";
    el.title = "已连接本地变更事件流，外部写入会立即显示";
  } else if (mode === "poll") {
    el.textContent = "轮询同步";
    el.title = "事件流不可用，已降级为 3 秒轮询；外部写入会在数秒内显示";
  } else if (mode === "offline") {
    el.textContent = "同步中断";
    el.title = "暂时无法接收变更事件，正在重试";
  } else {
    el.textContent = "同步中…";
    el.title = "正在连接本地变更事件流";
  }
  if (detail) el.title = detail;
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
  scheduleRealtimeRefresh(150);
}

function handleChangeEvent(item) {
  if (!item || !item.id || seenEventIds.has(item.id)) return;
  seenEventIds.add(item.id);
  if (seenEventIds.size > 500) {
    seenEventIds.delete(seenEventIds.values().next().value);
  }
  state.lastEventId = item.id;
  pendingEntities.add(item.entity || "");
  if (isAgentEntry(item)) pendingAgentActivity = true;
  if (state.auditTarget &&
      item.entity === state.auditTarget.entity &&
      item.entity_id === state.auditTarget.id) {
    loadAuditHistory(state.auditTarget.entity, state.auditTarget.id, state.auditTarget.selector);
    if (isAgentEntry(item)) {
      toast("Agent（MCP）修改了当前打开的记录，变更记录已刷新；请核对后再保存");
    }
  }
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
  if (entities.has("contract") || needFields) jobs.push(loadContracts());
  if (entities.has("party") || needFields) jobs.push(loadParties());
  if (needFields) jobs.push(loadFields());
  if (entities.has("seal")) jobs.push(loadSeals());
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
      state.years = bootstrap.years || [];
      renderYearSelect();
      renderMeta();
    }
  }
  if (entities.has("contract") || needFields) renderContracts();
  if (entities.has("party") || needFields) renderParties();
  if (needFields) renderFields();
  if (entities.has("seal")) renderSeals();
  if (entities.has("template")) renderTemplates();
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
  if (tab === "changelog") loadChangelog().catch((err) => toast(err.message, true));
  if (tab === "seals") loadSeals().then(renderSeals).catch((err) => toast(err.message, true));
  if (tab === "templates") loadTemplates().then(renderTemplates).catch((err) => toast(err.message, true));
  if (tab === "data") loadBackupStatus().catch((err) => toast(err.message, true));
  if (tab === "settings") loadMCPStatus().catch((err) => toast(err.message, true));
}

async function refreshAfterWrite() {
  await Promise.all([loadFields(), loadParties(), loadContracts(), loadSeals(), loadTemplates()]);
  renderMeta();
  const bootstrap = await api("/app/api/v1/bootstrap").catch(() => null);
  if (bootstrap) {
    state.bootstrap = bootstrap;
    state.years = bootstrap.years || [];
    if (state.year && !state.years.some((y) => String(y.year) === state.year)) {
      state.year = "";
      await loadContracts();
    }
    renderYearSelect();
    renderMeta();
  }
  renderContracts();
  renderParties();
  renderFields();
  renderSeals();
  renderTemplates();
  if (state.tab === "changelog") {
    loadChangelog().catch((err) => toast(err.message, true));
  }
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
  $("#yearSelect").addEventListener("change", async (e) => {
    state.year = e.target.value;
    try {
      await loadContracts();
      renderContracts();
    } catch (err) { toast(err.message, true); }
  });
  $("#statusFilter").addEventListener("change", async (e) => {
    state.status = e.target.value;
    try {
      await loadContracts();
      renderContracts();
    } catch (err) { toast(err.message, true); }
  });
  $("#contractSearch").addEventListener("input", debounce(async (e) => {
    state.contractQ = e.target.value.trim();
    try {
      await loadContracts();
      renderContracts();
    } catch (err) { toast(err.message, true); }
  }, 250));
  $("#partySearch").addEventListener("input", debounce(async (e) => {
    state.partyQ = e.target.value.trim();
    try {
      await loadParties();
      renderParties();
    } catch (err) { toast(err.message, true); }
  }, 250));
  $("#sealSearch").addEventListener("input", debounce(async (e) => {
    state.sealQ = e.target.value.trim();
    try {
      await loadSeals();
      renderSeals();
    } catch (err) { toast(err.message, true); }
  }, 250));
  $("#templateSearch").addEventListener("input", debounce(async (e) => {
    state.templateQ = e.target.value.trim();
    try {
      await loadTemplates();
      renderTemplates();
    } catch (err) { toast(err.message, true); }
  }, 250));
  $("#importTemplateBtn").addEventListener("click", () => {
    const input = $("#templateFileInput");
    input.value = "";
    input.click();
  });
  $("#templateFileInput").addEventListener("change", (e) => {
    const file = e.target.files && e.target.files[0];
    if (file) openTemplateImport(file);
  });
  $("#refreshTemplatesBtn").addEventListener("click", () => {
    loadTemplates().then(renderTemplates).catch((err) => toast(err.message, true));
  });
  $("#importSealBtn").addEventListener("click", () => {
    const input = $("#sealImageFile");
    input.value = "";
    input.click();
  });
  $("#sealImageFile").addEventListener("change", (e) => {
    const file = e.target.files && e.target.files[0];
    if (file) openSealImport(file);
  });
  $("#newContractBtn").addEventListener("click", () => openContractForm(null));
  $("#newPartyBtn").addEventListener("click", () => openPartyForm(null));
  $("#newFieldBtn").addEventListener("click", () => openFieldForm(null));
  $("#downloadTemplateBtn").addEventListener("click", () => downloadWorkbook("template"));
  $("#exportLedgerBtn").addEventListener("click", () => downloadWorkbook("ledger"));
  $("#importBtn").addEventListener("click", () => {
    const input = $("#importFile");
    input.value = "";
    input.click();
  });
  $("#importFile").addEventListener("change", (e) => {
    const file = e.target.files && e.target.files[0];
    if (file) openImport(file);
  });
  $$("#fieldEntityTabs button").forEach((button) => {
    button.addEventListener("click", () => {
      state.fieldEntity = button.dataset.entity;
      $$("#fieldEntityTabs button").forEach((b) => b.classList.toggle("active", b === button));
      renderFields();
    });
  });
  $("#showArchivedFields").addEventListener("change", (e) => {
    state.showArchivedFields = e.target.checked;
    renderFields();
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
    await Promise.all([loadBootstrap(), loadFields(), loadParties(), loadSeals(), loadTemplates(), loadConverters()]);
    await loadContracts();
    renderMeta();
    renderContracts();
    renderParties();
    renderFields();
    renderSeals();
    renderTemplates();
  } catch (err) {
    toast(err.message, true);
    const summary = $("#contractSummary");
    if (summary) summary.textContent = "加载失败：" + err.message;
  }
  // Start the change feed even when the initial load failed: it is what
  // surfaces data written by agents while the page is open.
  startRealtime();
}

document.addEventListener("DOMContentLoaded", boot);
