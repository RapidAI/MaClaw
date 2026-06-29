/*
 * Tenant admin module.
 * ASCII only.
 */
(function(global) {
  var tenantCache = [];
  var tenantAuthorizationLoaded = false;
  var loginTenantOptionsCache = [];
  var tenantCreateBusy = false;
  var tenantAdminCreateBusy = false;
  var tenantMergeBusy = {};
  var tenantDomainSaveBusy = {};
  var tenantListPage = 1;
  var tenantListPageSize = 10;
  var TENANT_I18N = {
    nav: { zh: '\u79df\u6237\u7ba1\u7406', en: 'Tenants' },
    navDesc: { zh: '\u521b\u5efa\u79df\u6237\u548c\u79df\u6237\u7ba1\u7406\u5458', en: 'Create tenants and tenant admins' },
    navDescTenant: { zh: '\u7ba1\u7406\u672c\u79df\u6237\u4fe1\u606f\u548c\u7ba1\u7406\u5458', en: 'Manage this tenant and its admins' },
    title: { zh: '\u79df\u6237\u7ba1\u7406', en: 'Tenant Management' },
    desc: { zh: '\u5168\u5c40\u7ba1\u7406\u5458\u5728\u8fd9\u91cc\u521b\u5efa Hub \u79df\u6237\u3002\u57df\u540d\u548c\u521d\u59cb\u79df\u6237\u7ba1\u7406\u5458\u90fd\u53ef\u9009\uff0c\u53ef\u7528\u4e8e\u63a5\u6536\u96f6\u6563\u7528\u6237\u3002', en: 'Global admins create Hub tenants here. Domain and initial tenant admin are optional, so a tenant can receive loose non-enterprise users.' },
    descTenant: { zh: '\u79df\u6237\u7ba1\u7406\u5458\u5728\u8fd9\u91cc\u67e5\u770b\u81ea\u5df1\u7684\u79df\u6237\u4fe1\u606f\uff0c\u5e76\u6dfb\u52a0\u672c\u79df\u6237\u7684\u7ba1\u7406\u8d26\u53f7\u3002', en: 'Tenant admins review their own tenant and add admin accounts within that tenant.' },
    createTitle: { zh: '\u521b\u5efa\u79df\u6237', en: 'Create Tenant' },
    createDesc: { zh: '\u53ea\u9700\u586b\u79df\u6237\u540d\u79f0\u5373\u53ef\u521b\u5efa\u3002Tenant ID \u53ef\u7559\u7a7a\u81ea\u52a8\u751f\u6210\uff0c\u90ae\u7bb1\u57df\u540d\u53ef\u9009\u4e14\u652f\u6301\u591a\u4e2a\u3002\u521d\u59cb\u7ba1\u7406\u5458\u53ef\u7559\u7a7a\uff0c\u4e4b\u540e\u518d\u6dfb\u52a0\u3002', en: 'Only tenant name is required. Tenant ID can be generated automatically, email domains are optional and can contain multiple entries, and initial admin can be added later.' },
    adminCreateTitle: { zh: '\u6dfb\u52a0\u79df\u6237\u7ba1\u7406\u5458', en: 'Add Tenant Admin' },
    adminCreateDesc: { zh: '\u4e3a\u5df2\u6709\u79df\u6237\u589e\u52a0\u79df\u6237\u8303\u56f4\u7684\u7ba1\u7406\u8d26\u53f7\u3002', en: 'Add a tenant-scoped admin account to an existing tenant.' },
    listTitle: { zh: '\u79df\u6237\u5217\u8868', en: 'Tenant List' },
    listDesc: { zh: '\u5f53\u524d Hub \u79df\u6237\u53ca\u5176\u72b6\u6001\u548c\u90ae\u7bb1\u57df\u540d\u8def\u7531\u3002', en: 'Hub tenants, status, and email domain routing.' },
    listDescTenant: { zh: '\u5f53\u524d\u767b\u5f55\u7ba1\u7406\u5458\u6240\u5c5e\u7684\u79df\u6237\u3002', en: 'The tenant owned by the signed-in tenant admin.' },
    reload: { zh: '\u91cd\u65b0\u52a0\u8f7d', en: 'Reload' },
    create: { zh: '\u521b\u5efa\u79df\u6237', en: 'Create Tenant' },
    addAdmin: { zh: '\u6dfb\u52a0\u7ba1\u7406\u5458', en: 'Add Admin' },
    slug: { zh: '\u79df\u6237\u7f16\u7801', en: 'Tenant Code' },
    name: { zh: '\u79df\u6237\u540d\u79f0', en: 'Tenant Name' },
    tenantID: { zh: 'Tenant ID', en: 'Tenant ID' },
    domain: { zh: '\u90ae\u7bb1\u57df\u540d\uff08\u53ef\u9009\uff0c\u591a\u4e2a\uff09', en: 'Email Domains (optional, multiple)' },
    saveSettings: { zh: '\u4fdd\u5b58\u8bbe\u7f6e', en: 'Save Settings' },
    domainsSaved: { zh: '\u79df\u6237\u8bbe\u7f6e\u5df2\u66f4\u65b0\u3002', en: 'Tenant settings updated.' },
    domainsSaveFailed: { zh: '\u66f4\u65b0\u79df\u6237\u8bbe\u7f6e\u5931\u8d25: {error}', en: 'Update tenant settings failed: {error}' },
    registration: { zh: '\u65b0\u7528\u6237\u6ce8\u518c', en: 'New User Registration' },
    registrationOpen: { zh: '\u5141\u8bb8', en: 'Open' },
    registrationClosed: { zh: '\u5173\u95ed', en: 'Closed' },
    acceptRegistration: { zh: '\u63a5\u53d7\u65b0\u7528\u6237\u6ce8\u518c', en: 'Accept new user registration' },
    adminUser: { zh: '\u7ba1\u7406\u5458\u7528\u6237\u540d', en: 'Admin Username' },
    adminEmail: { zh: '\u7ba1\u7406\u5458\u90ae\u7bb1', en: 'Admin Email' },
    adminName: { zh: '\u663e\u793a\u540d\u79f0', en: 'Display Name' },
    adminPassword: { zh: '\u521d\u59cb\u5bc6\u7801', en: 'Initial Password' },
    tenant: { zh: '\u79df\u6237', en: 'Tenant' },
    loginTenant: { zh: '\u767b\u5f55\u8303\u56f4', en: 'Login Scope' },
    loginTenantHint: { zh: '\u5168\u5c40\u7ba1\u7406\u5458\u9009\u62e9\u201c\u5168\u5c40\u7ba1\u7406\u5458\u201d\uff1b\u79df\u6237\u7ba1\u7406\u5458\u9009\u62e9\u81ea\u5df1\u7684\u79df\u6237\u3002', en: 'Global admins choose Global admin; tenant admins choose their tenant.' },
    loginTenantChoose: { zh: '\u9009\u62e9\u767b\u5f55\u8303\u56f4', en: 'Select login scope' },
    loginTenantGlobal: { zh: '\u5168\u5c40\u7ba1\u7406\u5458\uff08\u4e0d\u9009\u79df\u6237\uff09', en: 'Global admin (no tenant)' },
    loginTenantLoadFailed: { zh: '\u79df\u6237\u9009\u9879\u52a0\u8f7d\u5931\u8d25\uff0c\u5168\u5c40\u7ba1\u7406\u5458\u4ecd\u53ef\u76f4\u63a5\u767b\u5f55\u3002', en: 'Tenant options failed to load. Global admins can still sign in.' },
    systemNavTenant: { zh: '\u7cfb\u7edf\u8bbe\u7f6e', en: 'System Settings' },
    systemNavDescTenant: { zh: '\u7ba1\u7406\u672c\u79df\u6237\u7684\u90ae\u4ef6\u3001LLM \u548c\u7cfb\u7edf\u80fd\u529b', en: 'Manage tenant mail, LLM, and system capabilities' },
    systemTitleTenant: { zh: '\u7cfb\u7edf\u8bbe\u7f6e', en: 'System Settings' },
    systemDescTenant: { zh: '\u79df\u6237\u7ba1\u7406\u5458\u53ef\u5728\u8fd9\u91cc\u914d\u7f6e\u79df\u6237\u7ea7\u7cfb\u7edf\u80fd\u529b\u548c\u81ea\u5df1\u7684\u767b\u5f55\u4fe1\u606f\u3002', en: 'Tenant admins configure tenant-level system capabilities and their own sign-in information here.' },
    systemSubtitleTenant: { zh: '\u79df\u6237\u7ea7\u90ae\u4ef6\u3001LLM \u548c\u8d26\u53f7\u5b89\u5168\u8bbe\u7f6e\u3002', en: 'Tenant mail, LLM, and account security settings.' },
    role: { zh: '\u89d2\u8272', en: 'Role' },
    empty: { zh: '\u6682\u65e0\u79df\u6237\u3002', en: 'No tenants yet.' },
    loading: { zh: '\u52a0\u8f7d\u4e2d...', en: 'Loading...' },
    forbidden: { zh: '\u9700\u8981\u5168\u5c40\u7ba1\u7406\u5458\u6743\u9650\u624d\u80fd\u67e5\u770b\u548c\u521b\u5efa\u79df\u6237\u3002', en: 'Global admin authorization is required to view and create tenants.' },
    required: { zh: '\u8bf7\u586b\u5199\u79df\u6237\u540d\u79f0\u3002', en: 'Fill tenant name.' },
    partialAdminRequired: { zh: '\u521d\u59cb\u7ba1\u7406\u5458\u5982\u679c\u586b\u5199\uff0c\u9700\u540c\u65f6\u586b\u5199\u7528\u6237\u540d\u3001\u5bc6\u7801\u548c\u90ae\u7bb1\u3002', en: 'If creating an initial admin, fill username, password, and email together.' },
    invalidDomain: { zh: '\u90ae\u7bb1\u57df\u540d\u65e0\u6548: {domain}', en: 'Invalid email domain: {domain}' },
    domainConflict: { zh: '\u90ae\u7bb1\u57df\u540d {domain} \u5df2\u88ab\u79df\u6237 {tenant} \u4f7f\u7528\u3002', en: 'Email domain {domain} is already used by tenant {tenant}.' },
    adminRequired: { zh: '\u8bf7\u9009\u62e9\u79df\u6237\u5e76\u586b\u5199\u7ba1\u7406\u5458\u7528\u6237\u540d\u3001\u5bc6\u7801\u548c\u90ae\u7bb1\u3002', en: 'Choose a tenant and fill admin username, password, and email.' },
    invalidAdminEmail: { zh: '\u8bf7\u8f93\u5165\u6709\u6548\u7684\u7ba1\u7406\u5458\u90ae\u7bb1\u3002', en: 'Enter a valid admin email address.' },
    loadFailed: { zh: '\u52a0\u8f7d\u79df\u6237\u5931\u8d25: {error}', en: 'Load tenants failed: {error}' },
    createDone: { zh: '\u79df\u6237\u5df2\u521b\u5efa: {tenant}', en: 'Tenant created: {tenant}' },
    createFailed: { zh: '\u521b\u5efa\u79df\u6237\u5931\u8d25: {error}', en: 'Create tenant failed: {error}' },
    adminDone: { zh: '\u79df\u6237\u7ba1\u7406\u5458\u5df2\u521b\u5efa: {admin}', en: 'Tenant admin created: {admin}' },
    adminFailed: { zh: '\u521b\u5efa\u79df\u6237\u7ba1\u7406\u5458\u5931\u8d25: {error}', en: 'Create tenant admin failed: {error}' },
    updated: { zh: '\u66f4\u65b0\u65f6\u95f4', en: 'Updated' },
    active: { zh: '\u5df2\u542f\u7528', en: 'Active' },
    inactive: { zh: '\u5df2\u505c\u7528', en: 'Inactive' },
    deleted: { zh: '\u5df2\u5220\u9664', en: 'Deleted' },
    noActiveTenants: { zh: '\u6682\u65e0\u53ef\u7528\u79df\u6237', en: 'No active tenants' },
    pagePrev: { zh: '\u4e0a\u4e00\u9875', en: 'Prev' },
    pageNext: { zh: '\u4e0b\u4e00\u9875', en: 'Next' },
    pageInfo: { zh: '\u7b2c {page}/{pages} \u9875\uff0c\u5171 {total} \u4e2a\u79df\u6237', en: 'Page {page}/{pages}, {total} tenants' },
    mergeTenant: { zh: '\u5408\u5e76', en: 'Merge' },
    mergeDryRun: { zh: '\u9884\u68c0', en: 'Preview' },
    mergeChooseTarget: { zh: '\u8bf7\u9009\u62e9\u8981\u5408\u5e76\u5230\u7684\u76ee\u6807\u79df\u6237\u3002', en: 'Choose target tenant to merge into.' },
    mergeConfirm: { zh: '\u786e\u8ba4\u5c06\u79df\u6237 {source} \u7684\u6570\u636e\u5408\u5e76\u5230 {target} \u5417\uff1f\u5408\u5e76\u540e\u6e90\u79df\u6237\u4f1a\u88ab\u6807\u8bb0\u5220\u9664\u3002', en: 'Merge tenant {source} into {target}? The source tenant will be marked deleted after merge.' },
    mergeConfirmDefault: { zh: '\u786e\u8ba4\u5c06\u9ed8\u8ba4\u79df\u6237 {source} \u7684\u6570\u636e\u5408\u5e76\u5230 {target} \u5417\uff1f\u9ed8\u8ba4\u79df\u6237\u4e0d\u4f1a\u88ab\u5220\u9664\uff0c\u4f46\u6570\u636e\u4f1a\u8fc1\u79fb\u5230\u76ee\u6807\u79df\u6237\u3002', en: 'Merge default tenant {source} into {target}? Default tenant will not be deleted, but its data will move to the target tenant.' },
    mergeDone: { zh: '\u79df\u6237\u5408\u5e76\u5b8c\u6210\uff1a\u79fb\u52a8 {moved} \u6761\uff0c\u7cfb\u7edf\u8bbe\u7f6e\u5408\u5e76 {merged} \u9879\u3002', en: 'Tenant merge done: moved {moved} rows and merged {merged} system settings.' },
    mergePreviewDone: { zh: '\u9884\u68c0\u5b8c\u6210\uff1a\u5c06\u79fb\u52a8 {moved} \u6761\uff0c\u7cfb\u7edf\u8bbe\u7f6e\u5408\u5e76 {merged} \u9879\u3002', en: 'Preview done: would move {moved} rows and merge {merged} system settings.' },
    mergeFailed: { zh: '\u79df\u6237\u5408\u5e76\u5931\u8d25: {error}', en: 'Tenant merge failed: {error}' },
    deactivate: { zh: '\u505c\u7528', en: 'Deactivate' },
    reactivate: { zh: '\u542f\u7528', en: 'Reactivate' },
    deleteTenant: { zh: '\u5220\u9664', en: 'Delete' },
    deactivateConfirm: { zh: '\u786e\u8ba4\u505c\u7528\u79df\u6237 {tenant} \u5417\uff1f', en: 'Deactivate tenant {tenant}?' },
    reactivateConfirm: { zh: '\u786e\u8ba4\u91cd\u65b0\u542f\u7528\u79df\u6237 {tenant} \u5417\uff1f', en: 'Reactivate tenant {tenant}?' },
    deleteConfirm: { zh: '\u2757 \u786e\u8ba4\u5220\u9664\u79df\u6237 {tenant} \u5417\uff1f\u6b64\u64cd\u4f5c\u5c06\u6c38\u4e45\u5220\u9664\u8be5\u79df\u6237\u7684\u6240\u6709\u6570\u636e\uff0c\u4e0d\u53ef\u6062\u590d\uff01', en: '\u2757 Permanently delete tenant {tenant}? This will remove ALL tenant data and cannot be undone!' },
    statusUpdated: { zh: '\u79df\u6237\u72b6\u6001\u5df2\u66f4\u65b0\u3002', en: 'Tenant status updated.' },
    statusUpdateFailed: { zh: '\u66f4\u65b0\u79df\u6237\u72b6\u6001\u5931\u8d25: {error}', en: 'Update tenant status failed: {error}' },
    deletedDone: { zh: '\u79df\u6237\u5df2\u5220\u9664\u3002', en: 'Tenant deleted.' },
    deleteFailed: { zh: '\u5220\u9664\u79df\u6237\u5931\u8d25: {error}', en: 'Delete tenant failed: {error}' }
  };

  function lang() { return global.currentLang === 'zh' ? 'zh' : 'en'; }
  function tt(key, vars) {
    var entry = TENANT_I18N[key];
    var text = entry ? entry[lang()] : key;
    Object.keys(vars || {}).forEach(function(name) { text = text.replace('{' + name + '}', vars[name]); });
    return text;
  }
  function byID(id) { return global.document.getElementById(id); }
  function setText(id, key) { var el = byID(id); if (el) el.textContent = tt(key); }
  function tenantScoped() { var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null; return isTenantAdminProfile(profile); }
  function setTenantOutput(message, type) {
    if (typeof global.setOutput === 'function') global.setOutput(message);
    if (typeof global.showToast === 'function') global.showToast(message, type || 'info');
  }
  function val(id) { var el = byID(id); return el ? String(el.value || '').trim() : ''; }
  function validEmail(value) { return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(value || '').trim()); }
  function esc(value) {
    if (typeof global.escapeHtml === 'function') return global.escapeHtml(value || '');
    return String(value || '').replace(/[&<>"]/g, function(ch) { return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[ch] || ch; });
  }
  function fmtTime(value) {
    if (!value) return '-';
    var d = new Date(value);
    if (isNaN(d.getTime())) return value;
    return d.toLocaleString();
  }
  function computeCardIsActive(item) {
    if (!item) return false;
    if (typeof item.active === 'boolean') return item.active;
    var status = String(item.status || '').toLowerCase();
    if (status === 'expired' || status === 'exhausted' || status === 'inactive' || status === 'invalid') return false;
    var expiresAt = item.expires_at ? new Date(item.expires_at) : null;
    if (expiresAt && !isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) return false;
    return Number(item.credits_remaining || 0) > 0;
  }
  function normalizeComputeCredits(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n)) return 0;
    return Math.round(n * 10000) / 10000;
  }
  function formatComputeCredits(value) {
    return normalizeComputeCredits(value).toLocaleString(undefined, { maximumFractionDigits: 4 });
  }
  function sumComputeCredits(items, key) {
    return normalizeComputeCredits((items || []).reduce(function(sum, item) {
      return sum + Number(item && item[key] || 0);
    }, 0));
  }
  function tenantLabel(item) { return String(item && (item.name || item.slug || item.id) || ''); }
  function tenantOptionLabel(item) {
    if (!item) return '';
    var label = tenantLabel(item);
    var domains = tenantDomains(item);
    var suffix = domains.length ? domains[0] : (item.slug || item.id || '');
    return suffix && suffix !== label ? label + ' (' + suffix + ')' : label;
  }
  function splitDomains(value) {
    var seen = {};
    var out = [];
    String(value || '').split(/[\s,;]+/).forEach(function(part) {
      var domain = String(part || '').trim().toLowerCase().replace(/^\.+|\.+$/g, '');
      if (!domain || seen[domain]) return;
      seen[domain] = true;
      out.push(domain);
    });
    return out;
  }
  function invalidDomain(domains) {
    var re = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/;
    for (var i = 0; i < domains.length; i++) {
      if (!re.test(domains[i])) return domains[i];
    }
    return '';
  }
  function tenantDomains(item) {
    if (!item) return [];
    var domains = Array.isArray(item.domains) ? item.domains.slice() : [];
    if (item.primary_domain) domains.unshift(item.primary_domain);
    return splitDomains(domains.join('\n'));
  }
  function tenantSettings(item) {
    if (!item || !item.settings_json) return {};
    try {
      var parsed = JSON.parse(String(item.settings_json || '{}'));
      return parsed && typeof parsed === 'object' ? parsed : {};
    } catch (_) {
      return {};
    }
  }
  function tenantAllowsRegistration(item) {
    if (!item) return true;
    if (typeof item.allow_user_registration === 'boolean') return item.allow_user_registration;
    var settings = tenantSettings(item);
    if (settings.allow_user_registration === false || settings.registration_enabled === false) return false;
    return true;
  }
  function tenantDomainConflict(domains, currentTenantID) {
    var wanted = {};
    (domains || []).forEach(function(domain) { if (domain) wanted[domain] = true; });
    if (!Object.keys(wanted).length) return null;
    for (var i = 0; i < tenantCache.length; i++) {
      var item = tenantCache[i];
      if (!item || !item.id || item.id === currentTenantID || tenantStatus(item) === 'deleted') continue;
      var existing = tenantDomains(item);
      for (var j = 0; j < existing.length; j++) {
        if (wanted[existing[j]]) return { domain: existing[j], tenant: tenantLabel(item) || item.id };
      }
    }
    return null;
  }
  function tenantStatus(item) {
    if (!item) return 'active';
    if (item.deleted_at) return 'deleted';
    return String(item.status || 'active').toLowerCase();
  }
  function isReservedTenantID(id) {
    id = String(id || '').trim();
    return id === '__global__';
  }
  function isDefaultTenantID(id) {
    return String(id || '').trim() === 'tenant_default';
  }
  function isAssignableTenant(item) {
    return !!(item && item.id && !isReservedTenantID(item.id) && tenantIsActive(item));
  }
  function tenantIsActive(item) { return tenantStatus(item) === 'active'; }
  function tenantStatusText(item) {
    var status = tenantStatus(item);
    if (status === 'active' || status === 'inactive' || status === 'deleted') return tt(status);
    return status || tt('active');
  }
  function tenantBadgeClass(item) {
    var status = tenantStatus(item);
    if (status === 'active') return 'ok';
    if (status === 'deleted') return 'danger';
    return 'warn';
  }
  function tenantAdminActions(item) {
    if (tenantScoped() || !item || !item.id || isReservedTenantID(item.id) || tenantStatus(item) === 'deleted') return '';
    var label = tenantLabel(item) || item.id;
    var nextStatus = tenantStatus(item) === 'active' ? 'inactive' : 'active';
    var statusKey = nextStatus === 'active' ? 'reactivate' : 'deactivate';
    var confirmKey = nextStatus === 'active' ? 'reactivateConfirm' : 'deactivateConfirm';
    var escAttr = function(s) { return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); };
    var actions = '<button class="btn-secondary" type="button" onclick="' + escAttr('previewTenantMerge(' + JSON.stringify(item.id) + ',' + JSON.stringify(label) + ')') + '">' + esc(tt('mergeDryRun')) + '</button>'
      + '<button class="btn-secondary" type="button" onclick="' + escAttr('mergeTenant(' + JSON.stringify(item.id) + ',' + JSON.stringify(label) + ')') + '">' + esc(tt('mergeTenant')) + '</button>';
    if (!isDefaultTenantID(item.id)) {
      actions += '<button class="btn-ghost" type="button" onclick="' + escAttr('updateTenantStatus(' + JSON.stringify(item.id) + ',' + JSON.stringify(nextStatus) + ',' + JSON.stringify(label) + ',' + JSON.stringify(confirmKey) + ')') + '">' + esc(tt(statusKey)) + '</button>'
        + '<button class="btn-danger" type="button" onclick="' + escAttr('deleteTenant(' + JSON.stringify(item.id) + ',' + JSON.stringify(label) + ')') + '">' + esc(tt('deleteTenant')) + '</button>';
    }
    return '<div class="tenant-actions">'
      + actions
      + '</div>';
  }

  function tenantMergeTargetOptions(sourceID) {
    return (tenantCache || []).filter(function(item) {
      return item && item.id && item.id !== sourceID && !isReservedTenantID(item.id) && tenantIsActive(item);
    });
  }

  async function chooseTenantMergeTarget(sourceID) {
    var options = tenantMergeTargetOptions(sourceID);
    if (!options.length) return '';
    var title = tt('mergeChooseTarget');
    var selectOptions = options.map(function(item) { return { label: tenantOptionLabel(item), value: item.id }; });
    var index = await showTenantSelectDialog(title, '', selectOptions);
    if (index < 0 || index >= options.length) return '';
    return options[index].id;
  }

  function tenantMergeTotals(result) {
    var moved = 0;
    var merged = 0;
    var tables = result && result.tables || {};
    Object.keys(tables).forEach(function(name) {
      moved += Number(tables[name] && tables[name].moved_rows || 0);
      merged += Number(tables[name] && tables[name].merged_rows || 0);
    });
    moved += Number(result && result.system_settings && result.system_settings.moved_keys || 0);
    merged += Number(result && result.system_settings && result.system_settings.merged_keys || 0);
    return { moved: moved, merged: merged };
  }

  function setTenantListPage(page) {
    tenantListPage = Math.max(1, Number(page) || 1);
    renderTenants(tenantCache);
  }

  function renderTenantPager(page, pages, total) {
    if (pages <= 1) return '';
    return '<div class="tenant-pager"><button class="btn-ghost" type="button" ' + (page <= 1 ? 'disabled ' : '') + 'onclick="setTenantListPage(' + (page - 1) + ')">' + esc(tt('pagePrev')) + '</button><span class="item-meta">' + esc(tt('pageInfo', { page: page, pages: pages, total: total })) + '</span><button class="btn-ghost" type="button" ' + (page >= pages ? 'disabled ' : '') + 'onclick="setTenantListPage(' + (page + 1) + ')">' + esc(tt('pageNext')) + '</button></div>';
  }

  function renderTenantAuthorizationBadges(item) {
    if (!item) return '';
    var parts = [];
    // Digital employee authorization
    var de = item.digital_employee_authorization;
    if (de) {
      var deActive = de.active;
      var deBadge = deActive ? 'ok' : 'warn';
      var deQuota = Number(de.quota) || 0;
      var deUsed = Number(de.used) || 0;
      var deExpiry = de.expires_at ? fmtTime(de.expires_at) : '';
      var deLabel = lang() === 'zh' ? '\u6570\u5b57\u5458\u5de5' : 'Digital Employees';
      var deStatus = deActive
        ? (lang() === 'zh' ? '\u5df2\u6388\u6743 ' + deQuota + ' \u4e2a\u5e2d\u4f4d' + (deUsed > 0 ? '\uff08\u5df2\u7528 ' + deUsed + '\uff09' : '') : deQuota + ' seats' + (deUsed > 0 ? ' (' + deUsed + ' used)' : ''))
        : (lang() === 'zh' ? '\u672a\u6388\u6743' : 'Not authorized');
      var deTitle = deActive
        ? (deExpiry ? (lang() === 'zh' ? '\u5230\u671f: ' + deExpiry : 'Expires: ' + deExpiry) : '')
        : (de.reason || '');
      parts.push('<div class="tenant-authz-badge" style="display:flex;align-items:center;gap:6px;padding:4px 10px;border-radius:6px;background:#fff;border:1px solid #e2e8f0;font-size:12px"><span class="authz-icon" style="font-size:14px;line-height:1">\ud83e\udd16</span><span class="authz-label" style="font-weight:700;color:#475569;white-space:nowrap">' + esc(deLabel) + '</span><span class="badge ' + deBadge + '" title="' + esc(deTitle) + '">' + esc(deStatus) + '</span></div>');
    }
    // Compute module authorization
    var compute = item.compute_authorization;
    if (compute) {
      var cActive = compute.active;
      var cBadge = cActive ? 'ok' : 'warn';
      var cRemaining = formatComputeCredits(compute.remaining_credits);
      var cTotal = formatComputeCredits(compute.total_credits);
      var cExpiry = compute.expires_at ? fmtTime(compute.expires_at) : '-';
      var cLabel = lang() === 'zh' ? '\u7b97\u529b\u6a21\u5757' : 'Compute Module';
      var cStatus = cActive
        ? (lang() === 'zh' ? '\u5269\u4f59 ' + cRemaining + '/' + cTotal + ' \u7b97\u529b' : cRemaining + '/' + cTotal + ' credits')
        : (lang() === 'zh' ? '\u672a\u6388\u6743' : 'Not authorized');
      var cTitle = cActive
        ? (lang() === 'zh' ? '\u5230\u671f: ' + cExpiry + (compute.allow_external ? ' | \u5141\u8bb8\u5916\u90e8\u63d0\u4f9b\u5546' : '') : 'Expires: ' + cExpiry + (compute.allow_external ? ' | External providers allowed' : ''))
        : '';
      parts.push('<div class="tenant-authz-badge" style="display:flex;align-items:center;gap:6px;padding:4px 10px;border-radius:6px;background:#fff;border:1px solid #e2e8f0;font-size:12px"><span class="authz-icon" style="font-size:14px;line-height:1">\u26a1</span><span class="authz-label" style="font-weight:700;color:#475569;white-space:nowrap">' + esc(cLabel) + '</span><span class="badge ' + cBadge + '" title="' + esc(cTitle) + '">' + esc(cStatus) + '</span></div>');
    }
    // If neither authorization is present and data was loaded, show a hint
    if (!de && !compute && tenantAuthorizationLoaded) {
      var noAuthLabel = lang() === 'zh' ? '\u672a\u914d\u7f6e\u6388\u6743' : 'No authorization';
      parts.push('<div class="tenant-authz-badge" style="display:flex;align-items:center;gap:6px;padding:4px 10px;border-radius:6px;background:#fff;border:1px solid #e2e8f0;font-size:12px"><span class="authz-icon" style="font-size:14px">\u26a0\ufe0f</span><span class="authz-label" style="font-weight:700;color:#475569">' + esc(noAuthLabel) + '</span></div>');
    }
    return parts.join('');
  }

  function applyTenantI18n() {
    setText('navTenants', 'nav'); setText('navTenantsDesc', tenantScoped() ? 'navDescTenant' : 'navDesc'); setText('loginTenantLabel', 'loginTenant'); setText('loginTenantHint', 'loginTenantHint'); setText('tenantsTitle', 'title'); setText('tenantsDesc', tenantScoped() ? 'descTenant' : 'desc');
    setText('tenantsReloadBtn', 'reload'); setText('tenantCreateTitle', 'createTitle'); setText('tenantCreateDesc', 'createDesc');
    setText('tenantAdminCreateTitle', 'adminCreateTitle'); setText('tenantAdminCreateDesc', 'adminCreateDesc'); setText('tenantListTitle', 'listTitle'); setText('tenantListDesc', tenantScoped() ? 'listDescTenant' : 'listDesc');
    setText('tenantCreateBtn', 'create'); setText('tenantAdminCreateBtn', 'addAdmin'); setText('tenantNameLabel', 'name'); setText('tenantIDLabel', 'tenantID'); setText('tenantDomainLabel', 'domain');
    setText('tenantCreateAcceptRegistrationLabel', 'acceptRegistration');
    setText('tenantAdminUsernameLabel', 'adminUser'); setText('tenantAdminEmailLabel', 'adminEmail'); setText('tenantAdminNameLabel', 'adminName'); setText('tenantAdminPasswordLabel', 'adminPassword');
    setText('tenantAdminTenantLabel', 'tenant'); setText('tenantAdminRoleLabel', 'role'); setText('tenantExtraAdminUsernameLabel', 'adminUser'); setText('tenantExtraAdminEmailLabel', 'adminEmail'); setText('tenantExtraAdminNameLabel', 'adminName'); setText('tenantExtraAdminPasswordLabel', 'adminPassword');
    var empty = byID('tenantListEmpty'); if (empty) empty.textContent = tt('empty');
    renderLoginTenantOptions(loginTenantOptionsCache);
    renderTenants(tenantCache);
  }

  function renderTenantSelect(items) {
    var select = byID('tenantAdminTenantID');
    if (!select) return;
    var button = byID('tenantAdminCreateBtn');
    var current = select.value;
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    var tenantAdmin = isTenantAdminProfile(profile);
    var list = (items || []).filter(isAssignableTenant);
    if (!list.length) {
      select.innerHTML = '<option value="">' + esc(tt('noActiveTenants')) + '</option>';
    } else {
      select.innerHTML = list.map(function(item) {
        return '<option value="' + esc(item.id) + '">' + esc(tenantOptionLabel(item)) + '</option>';
      }).join('');
    }
    if (current && list.some(function(item) { return item && item.id === current; })) select.value = current;
    if (tenantAdmin && profile.tenant_id && list.some(function(item) { return item && item.id === profile.tenant_id; })) {
      select.value = profile.tenant_id;
      select.disabled = true;
    } else {
      select.disabled = !list.length;
    }
    if (button) button.disabled = !list.length;
  }
  function currentTenantItem() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    if (!isTenantAdminProfile(profile) || !profile.tenant_id || isReservedTenantID(profile.tenant_id)) return null;
    return { id: profile.tenant_id, slug: profile.tenant_id, name: profile.tenant_id, status: 'active' };
  }
  function renderLoginTenantOptions(items) {
    var select = byID('loginTenant');
    if (!select) return;
    var current = select.value;
    var options = ['<option value="">' + esc(tt('loginTenantChoose')) + '</option>', '<option value="__global__">' + esc(tt('loginTenantGlobal')) + '</option>'];
    var available = (items || []).filter(function(item) { return item && item.id && !isReservedTenantID(item.id); });
    available.forEach(function(item) {
      options.push('<option value="' + esc(item.id) + '">' + esc(tenantOptionLabel(item)) + '</option>');
    });
    select.innerHTML = options.join('');
    if (!current) current = '__global__';
    if (current === '__global__' || (current && available.some(function(item) { return item && item.id === current; }))) select.value = current;
  }

  function renderTenants(items) {
    var root = byID('tenantList');
    var badge = byID('tenantCountBadge');
    var list = Array.isArray(items) ? items.map(function(item, index) { return { item: item, index: index }; }).filter(function(entry) { return entry.item && !isReservedTenantID(entry.item.id); }) : [];
    var total = list.length;
    var pages = Math.max(1, Math.ceil(total / tenantListPageSize));
    tenantListPage = Math.min(Math.max(tenantListPage, 1), pages);
    var start = (tenantListPage - 1) * tenantListPageSize;
    var pageEntries = list.slice(start, start + tenantListPageSize);
    if (badge) badge.textContent = String(total);
    renderTenantSelect(list.map(function(entry) { return entry.item; }));
    if (!root) return;
    if (!total) {
      root.innerHTML = '<div class="hint" id="tenantListEmpty">' + esc(tt('empty')) + '</div>';
      return;
    }
    root.innerHTML = '<div class="tenant-list-shell">' + pageEntries.map(function(entry) {
      var item = entry.item;
      var index = entry.index;
      var domains = tenantDomains(item);
      var domain = domains.length ? domains.join(', ') : '-';
      var registrationOpen = tenantAllowsRegistration(item);
      var registrationText = registrationOpen ? tt('registrationOpen') : tt('registrationClosed');
      var registrationBadge = registrationOpen ? 'ok' : 'warn';
      var statusTitle = tt('updated') + ': ' + fmtTime(item.updated_at);
      if (item.deleted_at) statusTitle += ' / ' + tt('deleted') + ': ' + fmtTime(item.deleted_at);
      var canEditDomains = item && item.id && !isReservedTenantID(item.id) && tenantStatus(item) !== 'deleted';
      var authzHTML = renderTenantAuthorizationBadges(item);
      return '<div class="tenant-card ' + (isDefaultTenantID(item.id) ? 'tenant-default' : '') + '">'
        + '<div class="tenant-summary">'
        + '<div class="tenant-identity"><span class="tenant-dot" aria-hidden="true"></span><div><div class="tenant-name" title="' + esc(tenantLabel(item)) + '">' + esc(tenantLabel(item)) + '</div><div class="tenant-id mono" title="' + esc(item.id || '') + '">' + esc(item.id || '') + '</div></div></div>'
        + '<div class="tenant-cell"><label>' + esc(tt('domain')) + '</label><div class="tenant-value mono" title="' + esc(domain) + '">' + esc(domain) + '</div></div>'
        + '<div class="tenant-cell"><label>' + esc(tt('registration')) + '</label><div class="tenant-status-row"><span class="badge ' + esc(registrationBadge) + '">' + esc(registrationText) + '</span></div></div>'
        + '<div class="tenant-cell"><label>' + esc(tt('updated')) + '</label><div class="tenant-status-row"><span class="badge ' + esc(tenantBadgeClass(item)) + '" title="' + esc(statusTitle) + '">' + esc(tenantStatusText(item)) + '</span></div></div>'
        + tenantAdminActions(item)
        + '</div>'
        + (authzHTML ? '<div class="tenant-authorization-row" style="display:flex;gap:12px;flex-wrap:wrap;padding:10px 16px;border-top:1px solid #e2e8f0;background:#f8fafc">' + authzHTML + '</div>' : '')
        + (canEditDomains ? '<div class="tenant-settings"><div><label>' + esc(tt('name')) + '</label><input id="tenantNameEdit_' + index + '" value="' + esc(item.name || '') + '"></div><div><label>' + esc(tt('domain')) + '</label><textarea id="tenantDomainsEdit_' + index + '" placeholder="acme.example.com\nsubsidiary.example.com">' + esc(domains.join('\n')) + '</textarea></div><label class="tenant-check"><input id="tenantRegistrationEdit_' + index + '" type="checkbox" ' + (registrationOpen ? 'checked ' : '') + '>' + esc(tt('acceptRegistration')) + '</label><button class="btn-secondary tenant-save" id="tenantDomainsSave_' + index + '" type="button" onclick="saveTenantDomains(' + index + ')">' + esc(tt('saveSettings')) + '</button></div>' : '')
        + '</div>';
    }).join('') + '</div>' + renderTenantPager(tenantListPage, pages, total);
  }

  async function loadLoginTenants() {
    try {
      var res = await global.fetch('/api/admin/login/tenants', { headers: { 'Accept': 'application/json' } });
      var data = {};
      try { data = await res.json(); } catch (_) {}
      if (!res.ok) throw new Error(data.message || res.statusText || 'request failed');
      loginTenantOptionsCache = Array.isArray(data.tenants) ? data.tenants : [];
      var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
      if (profile && String(profile.scope || '').toLowerCase() === 'tenant' && profile.tenant_id && typeof global.updateCurrentTenantContext === 'function') {
        loginTenantOptionsCache.some(function(item) { if (item && item.id === profile.tenant_id) { global.updateCurrentTenantContext(item); return true; } return false; });
      }
      renderLoginTenantOptions(loginTenantOptionsCache);
      var hint = byID('loginTenantHint');
      if (hint) hint.textContent = tt('loginTenantHint');
      return loginTenantOptionsCache;
    } catch (err) {
      loginTenantOptionsCache = [];
      renderLoginTenantOptions([]);
      var hint = byID('loginTenantHint');
      if (hint) hint.textContent = tt('loginTenantLoadFailed');
      return [];
    }
  }

  async function loadTenants() {
    var root = byID('tenantList');
    if (root) root.innerHTML = '<div class="hint">' + esc(tt('loading')) + '</div>';
    try {
      var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
      if (isTenantAdminProfile(profile)) {
        if (!profile.tenant_id) throw new Error('tenant id is missing');
        var detail = await global.api('/api/admin/tenants/' + encodeURIComponent(profile.tenant_id));
        tenantCache = detail && detail.tenant ? [detail.tenant] : [currentTenantItem()];
        if (detail && detail.tenant && typeof global.updateCurrentTenantContext === 'function') global.updateCurrentTenantContext(detail.tenant);
      } else {
        var data = await global.api('/api/admin/tenants');
        tenantCache = Array.isArray(data.tenants) ? data.tenants : [];
        tenantAuthorizationLoaded = !!(data && data.authorization_loaded);
      }
      // Render immediately so user sees the tenant list without delay.
      renderTenants(tenantCache);
      // Then fetch authorization data in background and re-render.
      if (!isTenantAdminProfile(profile)) {
        enrichTenantCacheWithAuthorization().then(function() {
          renderTenants(tenantCache);
        });
      }
      return tenantCache;
    } catch (err) {
      if (err && err.staleAuth) return tenantCache;
      var fallback = currentTenantItem();
      tenantCache = fallback ? [fallback] : [];
      if (fallback) renderTenants(tenantCache); else renderTenantSelect([]);
      var raw = String(err && err.message || err || '');
      var msg = /global admin/i.test(raw) ? tt('forbidden') : tt('loadFailed', { error: raw });
      if (root && !fallback) root.innerHTML = '<div class="hint">' + esc(msg) + '</div>';
      if (!fallback && !/global admin/i.test(raw)) setTenantOutput(msg, 'error');
      return tenantCache;
    }
  }

  async function enrichTenantCacheWithAuthorization() {
    if (!tenantCache || !tenantCache.length) return;
    try {
      // Digital employee authorization from center status API.
      // Global admin sees full per-tenant map in digital_employee_authorizations.
      var centerData = await global.api('/api/admin/center/status');
      if (centerData) {
        var deAuthMap = centerData.digital_employee_authorizations;
        var deAuthDefault = centerData.digital_employee_authorization;
        if (deAuthMap || deAuthDefault) {
          tenantAuthorizationLoaded = true;
          for (var i = 0; i < tenantCache.length; i++) {
            var item = tenantCache[i];
            if (!item || !item.id) continue;
            if (item.digital_employee_authorization) continue;
            var auth = (deAuthMap && deAuthMap[item.id]) || null;
            if (!auth && isDefaultTenantID(item.id)) auth = deAuthDefault;
            if (auth) item.digital_employee_authorization = auth;
          }
        }
      }
    } catch (_) {}
    try {
      // Compute module authorization from MaClaw compute status API.
      var computeData = await global.api('/api/admin/llm/maclaw-compute-status');
      if (computeData) {
        tenantAuthorizationLoaded = true;
        var computeAuthorizations = Array.isArray(computeData.authorizations) ? computeData.authorizations : [];
        var computeCards = computeAuthorizations.filter(function(a) {
          return String(a && a.service_group_id || '').trim() !== '__external_compute_permission__';
        });
        var activeComputeCards = computeCards.filter(computeCardIsActive);
        var summary = {
          active: !!computeData.allow_external_providers,
          total_credits: sumComputeCredits(activeComputeCards, 'credits_total'),
          used_credits: sumComputeCredits(activeComputeCards, 'credits_used'),
          remaining_credits: sumComputeCredits(activeComputeCards, 'credits_remaining'),
          authorization_count: activeComputeCards.length,
          expires_at: activeComputeCards.reduce(function(l, a) { return a.expires_at > l ? a.expires_at : l; }, ''),
          allow_external: !!computeData.allow_external_providers
        };
        for (var j = 0; j < tenantCache.length; j++) {
          var t = tenantCache[j];
          if (t && t.id && !t.compute_authorization) t.compute_authorization = summary;
        }
      }
    } catch (_) {}
  }

  async function createTenant() {
    if (tenantCreateBusy) return;
    var createDomains = splitDomains(val('tenantDomain'));
    var payload = {
      id: val('tenantID'), name: val('tenantName'), primary_domain: createDomains[0] || '', domains: createDomains, allow_user_registration: (byID('tenantCreateAcceptRegistration') ? !!byID('tenantCreateAcceptRegistration').checked : true),
      initial_admin_username: val('tenantAdminUsername'), initial_admin_password: val('tenantAdminPassword'), initial_admin_email: val('tenantAdminEmail'), initial_admin_name: val('tenantAdminName')
    };
    if (!payload.name) {
      setTenantOutput(tt('required'), 'info');
      return;
    }
    var badDomain = invalidDomain(createDomains);
    if (badDomain) {
      setTenantOutput(tt('invalidDomain', { domain: badDomain }), 'info');
      return;
    }
    var domainConflict = tenantDomainConflict(createDomains, '');
    if (domainConflict) {
      setTenantOutput(tt('domainConflict', domainConflict), 'info');
      return;
    }
    var adminRequested = !!(payload.initial_admin_username || payload.initial_admin_password || payload.initial_admin_email || payload.initial_admin_name);
    if (adminRequested && (!payload.initial_admin_username || !payload.initial_admin_password || !payload.initial_admin_email)) {
      setTenantOutput(tt('partialAdminRequired'), 'info');
      return;
    }
    if (adminRequested && !validEmail(payload.initial_admin_email)) {
      setTenantOutput(tt('invalidAdminEmail'), 'info');
      return;
    }
    var btn = byID('tenantCreateBtn');
    tenantCreateBusy = true;
    if (btn) btn.disabled = true;
    try {
      var data = await global.api('/api/admin/tenants', { method: 'POST', body: JSON.stringify(payload) });
      ['tenantID','tenantName','tenantDomain','tenantAdminUsername','tenantAdminPassword','tenantAdminEmail','tenantAdminName'].forEach(function(id) { var el = byID(id); if (el) el.value = ''; });
      var acceptEl = byID('tenantCreateAcceptRegistration');
      if (acceptEl) acceptEl.checked = true;
      await loadTenants();
      await loadLoginTenants();
      setTenantOutput(tt('createDone', { tenant: tenantLabel(data.tenant || payload) }), 'success');
    } catch (err) {
      setTenantOutput(tt('createFailed', { error: err.message || err }), 'error');
    } finally {
      tenantCreateBusy = false;
      if (btn) btn.disabled = false;
    }
  }

  async function saveTenantDomains(index) {
    var item = tenantCache[index];
    if (!item || !item.id) return;
    if (tenantDomainSaveBusy[item.id]) return;
    var domains = splitDomains(val('tenantDomainsEdit_' + index));
    var badDomain = invalidDomain(domains);
    if (badDomain) {
      setTenantOutput(tt('invalidDomain', { domain: badDomain }), 'info');
      return;
    }
    var domainConflict = tenantDomainConflict(domains, item.id);
    if (domainConflict) {
      setTenantOutput(tt('domainConflict', domainConflict), 'info');
      return;
    }
    var btn = byID('tenantDomainsSave_' + index);
    tenantDomainSaveBusy[item.id] = true;
    if (btn) btn.disabled = true;
    try {
      var regEl = byID('tenantRegistrationEdit_' + index);
      var data = await global.api('/api/admin/tenants/' + encodeURIComponent(item.id) + '/domains', { method: 'PATCH', body: JSON.stringify({ name: val('tenantNameEdit_' + index) || item.name || item.id, primary_domain: domains[0] || '', domains: domains, allow_user_registration: regEl ? !!regEl.checked : tenantAllowsRegistration(item) }) });
      if (data && data.tenant) tenantCache[index] = data.tenant;
      await loadTenants();
      await loadLoginTenants();
      setTenantOutput(tt('domainsSaved'), 'success');
    } catch (err) {
      setTenantOutput(tt('domainsSaveFailed', { error: err.message || err }), 'error');
    } finally {
      delete tenantDomainSaveBusy[item.id];
      if (btn) btn.disabled = false;
    }
  }

  async function createTenantAdmin() {
    if (tenantAdminCreateBusy) return;
    var tenantID = val('tenantAdminTenantID');
    var payload = { username: val('tenantExtraAdminUsername'), password: val('tenantExtraAdminPassword'), email: val('tenantExtraAdminEmail'), display_name: val('tenantExtraAdminName'), role: val('tenantAdminRole') || 'tenant_admin' };
    if (!tenantID || isReservedTenantID(tenantID) || !payload.username || !payload.password || !payload.email) {
      setTenantOutput(tt('adminRequired'), 'info');
      return;
    }
    if (!validEmail(payload.email)) {
      setTenantOutput(tt('invalidAdminEmail'), 'info');
      return;
    }
    var btn = byID('tenantAdminCreateBtn');
    tenantAdminCreateBusy = true;
    if (btn) btn.disabled = true;
    try {
      var data = await global.api('/api/admin/tenants/' + encodeURIComponent(tenantID) + '/admins', { method: 'POST', body: JSON.stringify(payload) });
      ['tenantExtraAdminUsername','tenantExtraAdminPassword','tenantExtraAdminEmail','tenantExtraAdminName'].forEach(function(id) { var el = byID(id); if (el) el.value = ''; });
      setTenantOutput(tt('adminDone', { admin: (data.admin && (data.admin.username || data.admin.email)) || payload.username }), 'success');
    } catch (err) {
      setTenantOutput(tt('adminFailed', { error: err.message || err }), 'error');
    } finally {
      tenantAdminCreateBusy = false;
      renderTenantSelect(tenantCache);
    }
  }

  async function updateTenantStatus(tenantID, status, label, confirmKey) {
    if (!tenantID) return;
    if (!(await showTenantConfirmDialog(tt(confirmKey || 'deactivateConfirm', { tenant: label || tenantID }), { title: lang() === 'zh' ? '\u66f4\u6539\u72b6\u6001' : 'Change Status', danger: status !== 'active' }))) return;
    try {
      await global.api('/api/admin/tenants/' + encodeURIComponent(tenantID) + '/status', { method: 'PATCH', body: JSON.stringify({ status: status }) });
      await loadTenants();
      await loadLoginTenants();
      setTenantOutput(tt('statusUpdated'), 'success');
    } catch (err) {
      setTenantOutput(tt('statusUpdateFailed', { error: err.message || err }), 'error');
    }
  }

  function showDeleteTenantDialog(tenantID, displayName) {
    return new Promise(function(resolve) {
      var overlayId = 'deleteTenantDialogOverlay';
      var existing = global.document.getElementById(overlayId);
      if (existing) existing.parentNode.removeChild(existing);
      var overlay = global.document.createElement('div');
      overlay.id = overlayId;
      overlay.className = 'session-modal-overlay show';
      overlay.style.cssText = 'z-index:9999;background:rgba(15,23,42,.42);padding:18px';
      var titleText = lang() === 'zh' ? '\u5220\u9664\u79df\u6237' : 'Delete Tenant';
      var msgHtml = lang() === 'zh'
        ? '\u2757 \u6b64\u64cd\u4f5c\u5c06\u6c38\u4e45\u5220\u9664\u79df\u6237 <strong>' + esc(displayName) + '</strong> \u53ca\u5176\u6240\u6709\u6570\u636e\uff08\u7528\u6237\u3001\u8bbe\u5907\u3001\u914d\u7f6e\u3001\u8bb0\u5f55\u7b49\uff09\uff0c\u4e0d\u53ef\u6062\u590d\uff01'
        : '\u2757 This will <strong>PERMANENTLY</strong> delete tenant "' + esc(displayName) + '" and ALL its data (users, devices, settings, records, etc.). This cannot be undone!';
      var passwordLabel = lang() === 'zh' ? '\u8bf7\u8f93\u5165\u60a8\u7684\u7ba1\u7406\u5458\u767b\u5f55\u5bc6\u7801\u4ee5\u786e\u8ba4\uff1a' : 'Enter your admin login password to confirm:';
      var cancelText = lang() === 'zh' ? '\u53d6\u6d88' : 'Cancel';
      var confirmText = lang() === 'zh' ? '\u786e\u8ba4\u5220\u9664' : 'Confirm Delete';
      var emptyHint = lang() === 'zh' ? '\u8bf7\u8f93\u5165\u5bc6\u7801' : 'Password is required';
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="deleteTenantDialogTitle" style="width:min(420px,100%);max-height:none;overflow:visible;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22)">'
        + '<div class="item-title" id="deleteTenantDialogTitle" style="margin-bottom:8px;color:var(--danger,#e53935)">' + esc(titleText) + '</div>'
        + '<div class="item-meta" style="margin-bottom:12px">' + msgHtml + '</div>'
        + '<div class="item-meta" style="margin-bottom:8px">' + esc(passwordLabel) + '</div>'
        + '<input id="deleteTenantPasswordInput" type="password" autocomplete="current-password" style="width:100%;height:36px;margin-bottom:4px">'
        + '<div id="deleteTenantPasswordError" style="color:var(--danger,#e53935);font-size:12px;min-height:18px;margin-bottom:8px"></div>'
        + '<div class="actions" style="justify-content:flex-end;gap:8px">'
        + '<button type="button" class="btn-ghost" id="deleteTenantCancelBtn">' + esc(cancelText) + '</button>'
        + '<button type="button" class="btn-danger" id="deleteTenantConfirmBtn">' + esc(confirmText) + '</button>'
        + '</div></div>';
      var done = function(value) {
        if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay);
        resolve(value);
      };
      global.document.body.appendChild(overlay);
      if (global.AdminUI && typeof global.AdminUI.bindModalOverlayDismiss === 'function') {
        global.AdminUI.bindModalOverlayDismiss(overlay, function() { done(null); });
      } else {
        overlay.onclick = function(event) { if (event && event.target === overlay) done(null); };
      }
      var input = overlay.querySelector('#deleteTenantPasswordInput');
      var errorEl = overlay.querySelector('#deleteTenantPasswordError');
      var cancel = overlay.querySelector('#deleteTenantCancelBtn');
      var ok = overlay.querySelector('#deleteTenantConfirmBtn');
      if (cancel) cancel.addEventListener('click', function() { done(null); });
      if (ok) ok.addEventListener('click', function() {
        var pw = (input ? input.value : '').trim();
        if (!pw) { if (errorEl) errorEl.textContent = emptyHint; if (input) input.focus(); return; }
        done(pw);
      });
      if (input) {
        input.addEventListener('keydown', function(event) {
          if (event.key === 'Enter') { event.preventDefault(); if (ok) ok.click(); }
          if (event.key === 'Escape') { event.preventDefault(); done(null); }
        });
        input.focus();
      }
    });
  }

  function showTenantConfirmDialog(message, options) {
    options = options || {};
    var danger = options.danger !== false;
    return new Promise(function(resolve) {
      var overlayId = 'tenantConfirmDialogOverlay';
      var existing = global.document.getElementById(overlayId);
      if (existing && existing.parentNode) existing.parentNode.removeChild(existing);
      var overlay = global.document.createElement('div');
      overlay.id = overlayId;
      overlay.className = 'session-modal-overlay show';
      overlay.style.cssText = 'z-index:9999;background:rgba(15,23,42,.42);padding:18px';
      var titleText = options.title || (lang() === 'zh' ? '\u786e\u8ba4\u64cd\u4f5c' : 'Confirm');
      var confirmText = options.confirmText || (lang() === 'zh' ? '\u786e\u8ba4' : 'Confirm');
      var cancelText = lang() === 'zh' ? '\u53d6\u6d88' : 'Cancel';
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="tenantConfirmDialogTitle" style="width:min(420px,100%);max-height:none;overflow:visible;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22)">'
        + '<div class="item-title" id="tenantConfirmDialogTitle" style="margin-bottom:8px' + (danger ? ';color:var(--danger,#e53935)' : '') + '">' + esc(titleText) + '</div>'
        + '<div class="item-meta" style="margin-bottom:16px;white-space:pre-wrap">' + esc(message) + '</div>'
        + '<div class="actions" style="justify-content:flex-end;gap:8px">'
        + '<button type="button" class="btn-ghost" id="tenantConfirmCancelBtn">' + esc(cancelText) + '</button>'
        + '<button type="button" class="' + (danger ? 'btn-danger' : 'btn-primary') + '" id="tenantConfirmOkBtn">' + esc(confirmText) + '</button>'
        + '</div></div>';
      var done = function(value) { if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay); resolve(value); };
      global.document.body.appendChild(overlay);
      if (global.AdminUI && typeof global.AdminUI.bindModalOverlayDismiss === 'function') {
        global.AdminUI.bindModalOverlayDismiss(overlay, function() { done(false); });
      } else {
        overlay.onclick = function(event) { if (event && event.target === overlay) done(false); };
      }
      var cancel = overlay.querySelector('#tenantConfirmCancelBtn');
      var ok = overlay.querySelector('#tenantConfirmOkBtn');
      if (cancel) cancel.addEventListener('click', function() { done(false); });
      if (ok) { ok.addEventListener('click', function() { done(true); }); ok.focus(); }
      overlay.addEventListener('keydown', function(event) {
        if (event.key === 'Escape') { event.preventDefault(); done(false); }
      });
    });
  }

  function showTenantSelectDialog(title, message, options) {
    return new Promise(function(resolve) {
      var overlayId = 'tenantSelectDialogOverlay';
      var existing = global.document.getElementById(overlayId);
      if (existing && existing.parentNode) existing.parentNode.removeChild(existing);
      var overlay = global.document.createElement('div');
      overlay.id = overlayId;
      overlay.className = 'session-modal-overlay show';
      overlay.style.cssText = 'z-index:9999;background:rgba(15,23,42,.42);padding:18px';
      var cancelText = lang() === 'zh' ? '\u53d6\u6d88' : 'Cancel';
      var confirmText = lang() === 'zh' ? '\u786e\u5b9a' : 'OK';
      var optionsHtml = options.map(function(item, index) {
        return '<label style="display:flex;align-items:center;gap:8px;padding:6px 0;cursor:pointer"><input type="radio" name="tenantSelectOption" value="' + index + '"' + (index === 0 ? ' checked' : '') + '><span>' + esc(item.label) + '</span></label>';
      }).join('');
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="tenantSelectDialogTitle" style="width:min(420px,100%);max-height:80vh;overflow:visible;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22)">'
        + '<div class="item-title" id="tenantSelectDialogTitle" style="margin-bottom:8px">' + esc(title) + '</div>'
        + (message ? '<div class="item-meta" style="margin-bottom:12px">' + esc(message) + '</div>' : '')
        + '<div style="max-height:240px;overflow-y:auto;margin-bottom:12px;padding:4px 0">' + optionsHtml + '</div>'
        + '<div class="actions" style="justify-content:flex-end;gap:8px">'
        + '<button type="button" class="btn-ghost" id="tenantSelectCancelBtn">' + esc(cancelText) + '</button>'
        + '<button type="button" class="btn-primary" id="tenantSelectOkBtn">' + esc(confirmText) + '</button>'
        + '</div></div>';
      var done = function(value) { if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay); resolve(value); };
      global.document.body.appendChild(overlay);
      if (global.AdminUI && typeof global.AdminUI.bindModalOverlayDismiss === 'function') {
        global.AdminUI.bindModalOverlayDismiss(overlay, function() { done(-1); });
      } else {
        overlay.onclick = function(event) { if (event && event.target === overlay) done(-1); };
      }
      var cancel = overlay.querySelector('#tenantSelectCancelBtn');
      var ok = overlay.querySelector('#tenantSelectOkBtn');
      if (cancel) cancel.addEventListener('click', function() { done(-1); });
      if (ok) ok.addEventListener('click', function() {
        var checked = overlay.querySelector('input[name="tenantSelectOption"]:checked');
        done(checked ? Number(checked.value) : -1);
      });
      overlay.addEventListener('keydown', function(event) {
        if (event.key === 'Escape') { event.preventDefault(); done(-1); }
        if (event.key === 'Enter') { event.preventDefault(); if (ok) ok.click(); }
      });
    });
  }

  async function deleteTenant(tenantID, label) {
    if (!tenantID) return;
    var displayName = label || tenantID;
    var password = await showDeleteTenantDialog(tenantID, displayName);
    if (!password) return;
    try {
      await global.api('/api/admin/tenants/' + encodeURIComponent(tenantID), { method: 'DELETE', body: JSON.stringify({ password: password }) });
      await loadTenants();
      await loadLoginTenants();
      setTenantOutput(tt('deletedDone'), 'success');
    } catch (err) {
      var errMsg = err.message || String(err);
      if (errMsg.indexOf('PASSWORD_INCORRECT') >= 0 || errMsg.indexOf('password') >= 0) {
        setTenantOutput(lang() === 'zh' ? '\u5220\u9664\u5931\u8d25\uff1a\u5bc6\u7801\u9519\u8bef\uff0c\u8bf7\u91cd\u8bd5\u3002' : 'Delete failed: incorrect password, please try again.', 'error');
      } else {
        setTenantOutput(tt('deleteFailed', { error: errMsg }), 'error');
      }
    }
  }

  async function runTenantMerge(tenantID, label, dryRun) {
    if (!tenantID || tenantMergeBusy[tenantID]) return;
    var targetID = await chooseTenantMergeTarget(tenantID);
    if (!targetID) return;
    var target = (tenantCache || []).filter(function(item) { return item && item.id === targetID; })[0] || { id: targetID };
    var confirmKey = isDefaultTenantID(tenantID) ? 'mergeConfirmDefault' : 'mergeConfirm';
    if (!dryRun && !(await showTenantConfirmDialog(tt(confirmKey, { source: label || tenantID, target: tenantLabel(target) || targetID }), { title: lang() === 'zh' ? '\u5408\u5e76\u79df\u6237' : 'Merge Tenant', danger: true }))) return;
    tenantMergeBusy[tenantID] = true;
    try {
      var data = await global.api('/api/admin/tenants/' + encodeURIComponent(tenantID) + '/merge', { method: 'POST', body: JSON.stringify({ target_tenant_id: targetID, dry_run: !!dryRun, delete_source: true }) });
      var totals = tenantMergeTotals(data && data.result || {});
      if (dryRun) {
        setTenantOutput(tt('mergePreviewDone', totals), 'info');
      } else {
        await loadTenants();
        await loadLoginTenants();
        setTenantOutput(tt('mergeDone', totals), 'success');
      }
    } catch (err) {
      setTenantOutput(tt('mergeFailed', { error: err.message || err }), 'error');
    } finally {
      delete tenantMergeBusy[tenantID];
    }
  }

  function previewTenantMerge(tenantID, label) { runTenantMerge(tenantID, label, true); }
  function mergeTenant(tenantID, label) { runTenantMerge(tenantID, label, false); }

  function isTenantAdminProfile(profile) {
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  function updateTenantAdminRoleOptions(profile) {
    var select = byID('tenantAdminRole');
    if (!select) return;
    var tenantAdmin = isTenantAdminProfile(profile);
    var current = select.value || 'tenant_admin';
    var roles = tenantAdmin ? ['tenant_admin'] : ['tenant_admin', 'tenant_owner'];
    select.innerHTML = roles.map(function(role) { return '<option value="' + esc(role) + '">' + esc(role) + '</option>'; }).join('');
    select.value = roles.indexOf(current) >= 0 ? current : 'tenant_admin';
  }

  function systemTabTitle() { return tenantScoped() ? tt('systemTitleTenant') : (typeof global.tr === 'function' ? global.tr('systemTabTitle') : 'System Settings'); }
  function systemTabSubtitle() { return tenantScoped() ? tt('systemSubtitleTenant') : (typeof global.tr === 'function' ? global.tr('systemTabSubtitle') : ''); }

  function toggleNearest(id, selector, hidden) {
    var el = byID(id);
    var node = el && typeof el.closest === 'function' ? el.closest(selector) : null;
    if (node) node.classList.toggle('hidden', hidden);
  }

  function applyOverviewScopeUI(tenantAdmin, hasProfile) {
    var globalAdmin = !!(hasProfile && !tenantAdmin);
    toggleNearest('centerStatusHero', '.metric', tenantAdmin);
    toggleNearest('digitalEmployeeHero', '.metric', tenantAdmin);
    toggleNearest('centerStatusDetail', '.hint', tenantAdmin);
    toggleNearest('centerAdvertisedURL', '.item', tenantAdmin);
    toggleNearest('machineCountHero', '.metric', globalAdmin);
    toggleNearest('blockedCountHero', '.metric', globalAdmin);
    toggleNearest('inviteCountHero', '.metric', globalAdmin);
  }

  function applySystemScopeCopy(tenantAdmin) {
    var navTitle = global.document.querySelector('button[data-tab="system"] [data-i18n="navSystem"]');
    var navDesc = global.document.querySelector('button[data-tab="system"] [data-i18n="navSystemDesc"]');
    var panelTitle = global.document.querySelector('#tab-system [data-i18n="systemTitle"]');
    var panelDesc = global.document.querySelector('#tab-system [data-i18n="systemDesc"]');
    var title = tenantAdmin ? tt('systemTitleTenant') : (typeof global.tr === 'function' ? global.tr('systemTitle') : 'System Settings');
    var desc = tenantAdmin ? tt('systemDescTenant') : (typeof global.tr === 'function' ? global.tr('systemDesc') : '');
    if (navTitle) navTitle.textContent = tenantAdmin ? tt('systemNavTenant') : (typeof global.tr === 'function' ? global.tr('navSystem') : 'System Settings');
    if (navDesc) navDesc.textContent = tenantAdmin ? tt('systemNavDescTenant') : (typeof global.tr === 'function' ? global.tr('navSystemDesc') : '');
    if (panelTitle) panelTitle.textContent = title;
    if (panelDesc) panelDesc.textContent = desc;
    var active = global.document.querySelector('.main');
    if (active && active.dataset.activeTab === 'system') {
      var pageTitle = byID('pageTitle');
      var pageSubtitle = byID('pageSubtitle');
      if (pageTitle) pageTitle.textContent = title;
      if (pageSubtitle) pageSubtitle.textContent = tenantAdmin ? tt('systemSubtitleTenant') : (typeof global.tr === 'function' ? global.tr('systemTabSubtitle') : '');
    }
  }

  function applyAdminScopeUI() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    var tenantAdmin = isTenantAdminProfile(profile);
    var hasProfile = !!profile;
    updateTenantAdminRoleOptions(profile);
    var globalOnly = global.adminGlobalOnlyTabs || { center: true, console: true };
    var tenantOnly = global.adminTenantOnlyTabs || { governance: true, marketplace: true, im: true, machines: true, virtualemployees: true, invitationcodes: true, pwarequests: true, security: true, llmproviders: true, usagestats: true, modelservices: true, servicecards: true, failurelogs: true };
    global.document.querySelectorAll('.nav button[data-tab]').forEach(function(button) {
      var tab = button.dataset.tab || '';
      var hidden = false;
      if (hasProfile && tenantAdmin) hidden = !!globalOnly[tab];
      if (hasProfile && !tenantAdmin) hidden = !!tenantOnly[tab];
      button.classList.toggle('hidden', hidden);
    });
    var active = global.document.querySelector('.nav button.active');
    var activeHidden = !!(active && active.classList.contains('hidden'));
    if (activeHidden && typeof global.openTab === 'function') global.openTab('tenants');
    try {
      var stored = global.localStorage && global.localStorage.getItem('maclawHubActiveTab') || '';
      if (stored && ((tenantAdmin && globalOnly[stored]) || (!tenantAdmin && tenantOnly[stored]))) {
        global.localStorage.setItem('maclawHubActiveTab', 'tenants');
      }
    } catch (_) {}
    var createPanel = byID('tenantCreatePanel');
    if (createPanel) createPanel.classList.toggle('hidden', !!(hasProfile && tenantAdmin));
    ['systemRoutingCard','mailConfigCard','tlsConfigCard'].forEach(function(id) {
      var card = byID(id);
      if (card) card.classList.toggle('hidden', !!(hasProfile && tenantAdmin));
    });
    var tenantSenderCard = byID('tenantMailSenderCard');
    if (tenantSenderCard) tenantSenderCard.classList.toggle('hidden', !(hasProfile && tenantAdmin));
    var tenantMigrationCard = byID('tenantMigrationSettingsCard');
    if (tenantMigrationCard) tenantMigrationCard.classList.toggle('hidden', !(hasProfile && tenantAdmin));
    var tenantLLMDefaultsCard = byID('tenantSystemLLMDefaultsCard');
    if (tenantLLMDefaultsCard) tenantLLMDefaultsCard.classList.toggle('hidden', !(hasProfile && tenantAdmin));
    if (typeof global.applyImScopeUI === 'function') global.applyImScopeUI();
    applySystemScopeCopy(!!(hasProfile && tenantAdmin));
    applyOverviewScopeUI(!!(hasProfile && tenantAdmin), hasProfile);
  }
  function registerTenantTab() {
    if (!global.AdminTabRegistry || typeof global.AdminTabRegistry.registerTab !== 'function') return;
    global.AdminTabRegistry.registerTab({ id: 'tenants', title: function() { return tt('title'); }, subtitle: function() { return tt(tenantScoped() ? 'descTenant' : 'desc'); }, onOpen: function() { applyTenantI18n(); loadTenants(); } });
  }

  if (typeof global.tr === 'function') {
    if (typeof global.tabMeta === 'object') global.tabMeta.tenants = ['tenantsTitle', 'tenantsDesc'];
    registerTenantTab();
    applyTenantI18n();
    loadLoginTenants();
  }
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') global.AdminTabRegistry.onLanguageChange(applyTenantI18n);

  global.adminSystemTabTitle = systemTabTitle;
  global.adminSystemTabSubtitle = systemTabSubtitle;
  global.applyAdminScopeUI = applyAdminScopeUI;
  global.loadLoginTenants = loadLoginTenants;
  global.loadTenants = loadTenants;
  global.createTenant = createTenant;
  global.createTenantAdmin = createTenantAdmin;
  global.saveTenantDomains = saveTenantDomains;
  global.setTenantListPage = setTenantListPage;
  global.previewTenantMerge = previewTenantMerge;
  global.mergeTenant = mergeTenant;
  global.updateTenantStatus = updateTenantStatus;
  global.deleteTenant = deleteTenant;
  applyAdminScopeUI();
})(window);
