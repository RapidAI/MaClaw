/*
 * Invitation codes admin module.
 * ASCII only.
 */

function invExtra(key) {
  const en = {
    llmServiceGroup: "LLM service group",
    llmGrantDays: "LLM grant days",
    llmGrantCredits: "LLM credits",
    llmGrantInfo: "LLM grant",
    noGrant: "-",
    grantSummary: "{group} / {days}d / {credits} credits",
    incompleteGrant: "LLM grant requires service group, days, and credits."
  };
  const zh = {
    llmServiceGroup: "LLM \u670d\u52a1\u7ec4",
    llmGrantDays: "\u6388\u6743\u5929\u6570",
    llmGrantCredits: "\u6388\u6743\u989d\u5ea6",
    llmGrantInfo: "LLM \u6388\u6743",
    noGrant: "-",
    grantSummary: "{group} / {days} \u5929 / {credits} \u989d\u5ea6",
    incompleteGrant: "LLM \u6388\u6743\u9700\u540c\u65f6\u586b\u5199\u670d\u52a1\u7ec4\u3001\u5929\u6570\u548c\u989d\u5ea6\u3002"
  };
  const dict = (typeof currentLang !== "undefined" && currentLang === "zh") ? zh : en;
  return dict[key] || key;
}

function applyInvitationExtraI18n() {
  const serviceGroupLabel = document.getElementById("invCodeLLMServiceGroupLabel");
  const grantDaysLabel = document.getElementById("invCodeLLMGrantDaysLabel");
  const grantCreditsLabel = document.getElementById("invCodeLLMGrantCreditsLabel");
  if (serviceGroupLabel) serviceGroupLabel.textContent = invExtra("llmServiceGroup");
  if (grantDaysLabel) grantDaysLabel.textContent = invExtra("llmGrantDays");
  if (grantCreditsLabel) grantCreditsLabel.textContent = invExtra("llmGrantCredits");
}

async function populateInvCodeServiceGroupDropdown() {
  const select = document.getElementById("invCodeLLMServiceGroupID");
  if (!select || select.tagName !== "SELECT") return;
  let groups = [];
  if (typeof llmServiceAdminCache !== "undefined" && llmServiceAdminCache && llmServiceAdminCache.model_service_groups) {
    groups = llmServiceAdminCache.model_service_groups;
  } else {
    try {
      const data = await api("/api/admin/llm/services?include_cards=false");
      groups = (data && data.model_service_groups) || [];
    } catch (_) {
      return;
    }
  }
  const currentValue = select.value;
  const placeholder = (typeof currentLang !== "undefined" && currentLang === "zh")
    ? "-- \u8bf7\u9009\u62e9 --"
    : "-- select --";
  select.innerHTML = "";
  const emptyOpt = document.createElement("option");
  emptyOpt.value = "";
  emptyOpt.textContent = placeholder;
  select.appendChild(emptyOpt);
  groups.filter(function(group) {
    return String(group && group.id || "").trim();
  }).forEach(function(group) {
    const id = String(group.id).trim();
    const name = String(group.name || "").trim();
    const label = name && name !== id ? id + " - " + name : id;
    const option = document.createElement("option");
    option.value = id;
    option.textContent = label;
    if (id === currentValue) option.selected = true;
    select.appendChild(option);
  });
}

async function loadInvitationCodeStatus() {
  try {
    applyInvitationExtraI18n();
    populateInvCodeServiceGroupDropdown();
    const data = await api("/api/admin/invitation-codes/status");
    invitationCodeRequired = !!data.invitation_code_required;
    renderInvitationCodeToggle();
  } catch (err) {
    const msg = inv("statusLoadFailed", { error: err.message });
    setOutput(msg);
  }
}

async function toggleInvitationCodeRequired() {
  try {
    const newVal = !invitationCodeRequired;
    const data = await api("/api/admin/invitation-codes/toggle", { method: "POST", body: JSON.stringify({ required: newVal }) });
    invitationCodeRequired = !!data.invitation_code_required;
    renderInvitationCodeToggle();
    const msg = inv("toggleSuccess");
    setOutput(msg);
    showToast(msg, "success");
  } catch (err) {
    const msg = inv("toggleFailed", { error: err.message });
    setOutput(msg);
    showToast(msg, "error");
  }
}

function renderInvitationCodeToggle() {
  const badge = document.getElementById("invCodeToggleBadge");
  const btn = document.getElementById("invCodeToggleBtn");
  if (badge) {
    badge.textContent = invitationCodeRequired ? inv("toggleEnabled") : inv("toggleDisabled");
    badge.className = "badge " + (invitationCodeRequired ? "ok" : "warn");
  }
  if (btn) {
    btn.textContent = invitationCodeRequired ? inv("toggleDisableAction") : inv("toggleEnableAction");
    btn.className = invitationCodeRequired ? "btn-danger" : "btn-primary";
  }
}

async function generateInvitationCodes() {
  try {
    const count = parseInt(document.getElementById("invCodeCount")?.value, 10) || 5;
    let validity_days = 0;
    const permanent = document.getElementById("invCodePermanent");
    if (!permanent || !permanent.checked) {
      const valNum = parseInt(document.getElementById("invCodeValidityValue")?.value, 10) || 0;
      const unitMul = parseInt(document.getElementById("invCodeValidityUnit")?.value, 10) || 1;
      validity_days = valNum > 0 ? valNum * unitMul : 0;
    }
    const llm_service_group_id = (document.getElementById("invCodeLLMServiceGroupID")?.value || "").trim();
    const llm_grant_duration_days = parseInt(document.getElementById("invCodeLLMGrantDays")?.value, 10) || 0;
    const llmGrantCreditsField = document.getElementById("invCodeLLMGrantCredits");
    const llmGrantCreditsInput = parseFloat(llmGrantCreditsField?.value);
    const llm_grant_credits = Number.isFinite(llmGrantCreditsInput) ? llmGrantCreditsInput : 0;
    const hasLLMGrantInput = llm_service_group_id !== "" || llm_grant_duration_days > 0 || (llmGrantCreditsField?.value || "").trim() !== "";
    if (hasLLMGrantInput && (llm_service_group_id === "" || llm_grant_duration_days <= 0 || !Number.isFinite(llmGrantCreditsInput) || llm_grant_credits <= 0)) {
      const msg = invExtra("incompleteGrant");
      setOutput(msg);
      showToast(msg, "error");
      return;
    }
    const vip = !!document.getElementById("invCodeVIP")?.checked;
    const data = await api("/api/admin/invitation-codes/generate", {
      method: "POST",
      body: JSON.stringify({ count, validity_days, vip, llm_service_group_id, llm_grant_duration_days, llm_grant_credits })
    });
    const msg = inv("generateSuccess", { count: String((data.codes || []).length) });
    setOutput(msg);
    showToast(msg, "success");
    await loadInvitationCodes();
  } catch (err) {
    const msg = inv("generateFailed", { error: err.message });
    setOutput(msg);
    showToast(msg, "error");
  }
}

function toggleValidityInputs() {
  const cb = document.getElementById("invCodePermanent");
  const row = document.getElementById("invCodeValidityRow");
  if (row) row.style.display = (cb && cb.checked) ? "none" : "block";
}

async function loadInvitationCodes() {
  try {
    let path = "/api/admin/invitation-codes";
    const params = [];
    if (invitationStatusFilter) params.push("status=" + encodeURIComponent(invitationStatusFilter));
    if (invitationSearchTerm) params.push("search=" + encodeURIComponent(invitationSearchTerm));
    params.push("page=" + invitationPage);
    params.push("page_size=" + invitationPageSize);
    if (params.length) path += "?" + params.join("&");
    const data = await api(path);
    invitationCodesCache = data.codes || [];
    invitationCodesTotal = data.total || 0;
    renderInvitationCodeList();
  } catch (err) {
    const msg = inv("loadFailed", { error: err.message });
    setOutput(msg);
    showToast(msg, "error");
  }
}

let invitationSearchTimer = null;

function onInvitationSearch() {
  clearTimeout(invitationSearchTimer);
  invitationSearchTimer = setTimeout(() => {
    invitationSearchTerm = (document.getElementById("invCodeSearch")?.value || "").trim();
    invitationPage = 1;
    loadInvitationCodes();
  }, 300);
}

function onInvitationFilterChange() {
  invitationStatusFilter = document.getElementById("invCodeFilter")?.value || "";
  invitationPage = 1;
  loadInvitationCodes();
}

function getValidityDisplay(c) {
  const vd = c.validity_days || 0;
  if (vd === 0) return { text: inv("validityPermanentLabel"), style: "" };
  if (c.status === "used" && c.bound_at) {
    const boundDate = new Date(c.bound_at);
    const now = new Date();
    const elapsed = Math.floor((now - boundDate) / (1000 * 60 * 60 * 24));
    const remaining = vd - elapsed;
    if (remaining <= 0) return { text: inv("validityExpired"), style: "color:#e53935;font-weight:600" };
    return { text: inv("validityRemaining", { days: String(remaining) }), style: "color:#43a047" };
  }
  return { text: inv("validityDaysLabel", { days: String(vd) }), style: "" };
}

function getLLMGrantDisplay(c) {
  const group = String(c.llm_service_group_id || "").trim();
  const days = Number(c.llm_grant_duration_days || 0);
  const credits = Number(c.llm_grant_credits || 0);
  if (!group || days <= 0 || credits <= 0) return invExtra("noGrant");
  return invExtra("grantSummary")
    .replace("{group}", group)
    .replace("{days}", String(days))
    .replace("{credits}", String(credits));
}

function renderInvitationCodeList() {
  applyInvitationExtraI18n();
  const root = document.getElementById("invitationCodeList");
  const pager = document.getElementById("invitationCodesPager");
  const pagerMeta = document.getElementById("invitationCodesPagerMeta");
  const prevBtn = document.getElementById("invitationCodesPrevButton");
  const nextBtn = document.getElementById("invitationCodesNextButton");
  if (!root) return;
  if (!invitationCodesCache.length) {
    root.innerHTML = '<div class="hint">' + inv("emptyList") + "</div>";
    if (pager) pager.classList.add("hidden");
    return;
  }
  const columns = "1.15fr .62fr .72fr 1.1fr .86fr .78fr .58fr";
  const header = '<div class="row header" style="grid-template-columns:' + columns + '"><div>' + inv("codeLabel") + "</div><div>Status</div><div>" + inv("validityInfo") + "</div><div>" + invExtra("llmGrantInfo") + "</div><div>" + inv("usedBy") + "</div><div>" + inv("createdAt") + "</div><div></div></div>";
  const rows = invitationCodesCache.map(c => {
    const isUsed = c.status === "used";
    const validity = getValidityDisplay(c);
    const isExpired = validity.text === inv("validityExpired");
    const statusClass = isExpired ? "danger" : (isUsed ? "warn" : "ok");
    const statusText = isExpired ? inv("validityExpired") : (isUsed ? inv("statusUsed") : inv("statusUnused"));
    const exportBadge = (!isUsed && c.exported) ? '<span class="badge info" style="margin-left:4px">' + inv("exportedLabel") + "</span>" : "";
    const vipBadge = c.vip ? '<span class="badge warn" style="margin-right:6px">VIP</span>' : "";
    const action = isUsed ? '<button class="btn-danger js-unbind-invitation" type="button" style="height:28px;font-size:11px;padding:0 8px" data-code-id="' + escapeHtml(c.id || "") + '" data-email="' + escapeHtml(c.used_by_email || "") + '">' + inv("unbind") + "</button>" : '<span class="item-meta">-</span>';
    const rowStyle = isExpired ? "border-color:rgba(214,93,87,.2);background:#fff9f9" : (c.vip ? "background:#fffdf5;border-color:rgba(245,166,35,.16)" : "");
    return '<div class="row" style="grid-template-columns:' + columns + ";" + rowStyle + '">' +
      '<div style="min-width:0"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + vipBadge + escapeHtml(c.code || "-") + "</div></div>" +
      '<div><span class="badge ' + statusClass + '">' + statusText + "</span>" + exportBadge + "</div>" +
      '<div class="item-meta" style="' + (validity.style || "") + '">' + validity.text + "</div>" +
      '<div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(getLLMGrantDisplay(c)) + "</div>" +
      '<div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(c.used_by_email || "-") + "</div>" +
      '<div class="item-meta">' + escapeHtml(c.created_at || "-") + "</div>" +
      '<div style="display:flex;justify-content:flex-end">' + action + "</div></div>";
  }).join("");
  root.innerHTML = '<div class="table" style="gap:6px">' + header + rows + "</div>";
  bindInvitationCodeActions(root);
  if (pager) {
    const totalPages = Math.max(1, Math.ceil(invitationCodesTotal / invitationPageSize));
    const start = (invitationPage - 1) * invitationPageSize + 1;
    const end = start + invitationCodesCache.length - 1;
    if (pagerMeta) pagerMeta.textContent = invitationCodesTotal > invitationPageSize ? inv("pageSummary", { start: String(start), end: String(end), total: String(invitationCodesTotal) }) : inv("pageSingle", { total: String(invitationCodesTotal) });
    if (prevBtn) prevBtn.disabled = invitationPage <= 1;
    if (nextBtn) nextBtn.disabled = invitationPage >= totalPages;
    pager.classList.toggle("hidden", invitationCodesTotal <= invitationPageSize);
  }
}

function bindInvitationCodeActions(root) {
  if (!root || root.dataset.invitationActionsBound === "1") return;
  root.dataset.invitationActionsBound = "1";
  root.addEventListener("click", event => {
    const target = event.target.closest(".js-unbind-invitation");
    if (!target) return;
    unbindInvitationCode(target.dataset.codeId || "", target.dataset.email || "");
  });
}

async function unbindInvitationCode(id, email) {
  if (!confirm(inv("unbindConfirm", { email: email }))) return;
  try {
    await api("/api/admin/invitation-codes/unbind", { method: "POST", body: JSON.stringify({ id: id }) });
    const msg = inv("unbindSuccess");
    setOutput(msg);
    showToast(msg, "success");
    await loadInvitationCodes();
  } catch (err) {
    const msg = inv("unbindFailed", { error: err.message });
    setOutput(msg);
    showToast(msg, "error");
  }
}

function changeInvitationPage(step) {
  const totalPages = Math.max(1, Math.ceil(invitationCodesTotal / invitationPageSize));
  invitationPage = Math.min(totalPages, Math.max(1, invitationPage + step));
  loadInvitationCodes();
}

function exportInvitationCodes() {
  const exportedFilter = document.getElementById("invCodeExportFilter")?.value || "unexported";
  const vipOnly = document.getElementById("invCodeExportVIP")?.checked ? "true" : "";
  let url = "/api/admin/invitation-codes/export?exported=" + encodeURIComponent(exportedFilter);
  if (vipOnly) url += "&vip=true";
  const headers = {};
  if (token()) headers.Authorization = "Bearer " + token();
  fetch(url, { headers }).then(r => {
    const count = r.headers.get("X-Export-Count") || "?";
    return r.blob().then(blob => ({ blob, count }));
  }).then(({ blob, count }) => {
    if (blob.size <= 1) {
      const msg = inv("exportEmpty");
      setOutput(msg);
      showToast(msg, "info");
      return;
    }
    const blobUrl = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = blobUrl;
    a.download = "invitation-codes.txt";
    a.click();
    URL.revokeObjectURL(blobUrl);
    const msg = inv("exportSuccess", { count });
    setOutput(msg);
    showToast(msg, "success");
    loadInvitationCodes();
  }).catch(err => {
    const msg = inv("loadFailed", { error: err.message || "export failed" });
    setOutput(msg);
    showToast(msg, "error");
  });
}
