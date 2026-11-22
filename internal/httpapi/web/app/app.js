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
  years: [],
  year: "",
  contractQ: "",
  status: "",
  partyQ: "",
  fieldEntity: "contract",
  showArchivedFields: false,
  changelog: [],
  tab: "contracts",
};

const TYPE_LABELS = {
  text: "文本", longtext: "长文本", number: "数字", money: "金额", date: "日期",
  select: "单选", bool: "是/否", image: "图片路径", party: "相对方", status: "状态",
};

const ENTITY_LABELS = { contract: "合同", party: "相对方", field: "字段", seal: "公章" };
const ACTION_LABELS = { create: "新建", update: "修改", delete: "删除", archive: "归档", import: "导入" };

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
  backdrop.innerHTML = `
    <div class="modal ${options.wide ? "wide" : ""}" role="dialog" aria-modal="true">
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
  const params = new URLSearchParams({ limit: "100" });
  if (entity) params.set("entity", entity);
  const data = await api("/app/api/v1/changelog?" + params.toString());
  state.changelog = data.items || [];
  renderChangelog(data.total || 0);
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
  await Promise.all([loadFields(), loadParties(), loadContracts(), loadSeals()]);
  renderMeta();
  renderContracts();
  renderParties();
  renderFields();
  renderSeals();
}

/* ---------- 顶栏 / 年度 ---------- */

function renderMeta() {
  const meta = $("#meta");
  if (!state.bootstrap) { meta.textContent = ""; return; }
  const stats = state.bootstrap.stats || {};
  meta.innerHTML = `
    <span>操作者：${esc(state.bootstrap.operator || "")}</span>
    <span>合同 ${stats.contracts || 0} 份</span>
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
    <div class="form-grid">${form}</div>`;
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
    return `<li><span class="badge active">${esc(roleOf(sig.party_id))}</span> ${esc(sig.signer_name)} · ${esc(fmtDate(sig.captured_at))}${version}</li>`;
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
  modal.body.innerHTML = `<div id="partyErrors"></div><div class="form-grid">${form}</div>`;
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

function renderChangelog(total) {
  $("#changelogSummary").textContent = `共 ${total} 条记录（显示最近 ${state.changelog.length} 条）`;
  const list = $("#changelogList");
  if (!state.changelog.length) {
    list.innerHTML = `<div class="empty">还没有变更记录。</div>`;
    return;
  }
  list.innerHTML = state.changelog.map((entry) => {
    const diffs = (entry.changes || []).map((c) => {
      const oldText = c.old === "" ? "（空）" : c.old;
      const newText = c.new === "" ? "（空）" : c.new;
      return `<li>${esc(c.label || c.field)}：<span class="old">${esc(oldText)}</span>
        <span class="arrow">→</span> <code>${esc(newText)}</code></li>`;
    }).join("");
    return `<div class="change-card">
      <div class="change-head">
        <span class="badge ${esc(entry.action)}">${esc(ACTION_LABELS[entry.action] || entry.action)}</span>
        <span class="summary">${esc(entry.summary)}</span>
        <span class="who">${esc(ENTITY_LABELS[entry.entity] || entry.entity)} · ${esc(entry.actor || "")} · ${esc(fmtDate(entry.created_at))}</span>
      </div>
      ${diffs ? `<ul class="change-diffs">${diffs}</ul>` : ""}
    </div>`;
  }).join("");
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

/* ---------- 页面切换 ---------- */

function switchTab(tab) {
  state.tab = tab;
  $$("#tabs button").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
  $$(".view").forEach((v) => v.classList.toggle("active", v.id === `view-${tab}`));
  if (tab === "changelog") loadChangelog().catch((err) => toast(err.message, true));
  if (tab === "seals") loadSeals().then(renderSeals).catch((err) => toast(err.message, true));
}

async function refreshAfterWrite() {
  await Promise.all([loadFields(), loadParties(), loadContracts(), loadSeals()]);
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
  if (state.tab === "changelog") {
    loadChangelog().catch((err) => toast(err.message, true));
  }
}

/* ---------- 事件绑定与启动 ---------- */

function bindEvents() {
  $$("#tabs button").forEach((button) => {
    button.addEventListener("click", () => switchTab(button.dataset.tab));
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
  $("#refreshChangelogBtn").addEventListener("click", () => {
    loadChangelog().catch((err) => toast(err.message, true));
  });
}

async function boot() {
  bindEvents();
  try {
    await Promise.all([loadBootstrap(), loadFields(), loadParties(), loadSeals()]);
    await loadContracts();
    renderMeta();
    renderContracts();
    renderParties();
    renderFields();
    renderSeals();
  } catch (err) {
    toast(err.message, true);
    const summary = $("#contractSummary");
    if (summary) summary.textContent = "加载失败：" + err.message;
  }
}

document.addEventListener("DOMContentLoaded", boot);
