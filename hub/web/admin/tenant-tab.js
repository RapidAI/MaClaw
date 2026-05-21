/*
 * Tenant admin module.
 * ASCII only.
 */
(function(global) {
  var tenantCache = [];
  var loginTenantOptionsCache = [];
  var tenantCreateBusy = false;
  var tenantAdminCreateBusy = false;
  var TENANT_I18N = {
    nav: { zh: '\u79df\u6237\u7ba1\u7406', en: 'Tenants' },
    navDesc: { zh: '\u521b\u5efa\u79df\u6237\u548c\u79df\u6237\u7ba1\u7406\u5458', en: 'Create tenants and tenant admins' },
    navDescTenant: { zh: '\u7ba1\u7406\u672c\u79df\u6237\u4fe1\u606f\u548c\u7ba1\u7406\u5458', en: 'Manage this tenant and its admins' },
    title: { zh: '\u79df\u6237\u7ba1\u7406', en: 'Tenant Management' },
    desc: { zh: '\u5168\u5c40\u7ba1\u7406\u5458\u5728\u8fd9\u91cc\u521b\u5efa Hub \u79df\u6237\u3001\u6307\u5b9a\u57df\u540d\u5e76\u914d\u7f6e\u79df\u6237\u7ba1\u7406\u5458\u3002', en: 'Global admins create Hub tenants, assign domains, and provision tenant admins here.' },
    descTenant: { zh: '\u79df\u6237\u7ba1\u7406\u5458\u5728\u8fd9\u91cc\u67e5\u770b\u81ea\u5df1\u7684\u79df\u6237\u4fe1\u606f\uff0c\u5e76\u6dfb\u52a0\u672c\u79df\u6237\u7684\u7ba1\u7406\u8d26\u53f7\u3002', en: 'Tenant admins review their own tenant and add admin accounts within that tenant.' },
    createTitle: { zh: '\u521b\u5efa\u79df\u6237', en: 'Create Tenant' },
    createDesc: { zh: '\u79df\u6237 Slug\u3001\u540d\u79f0\u548c\u521d\u59cb\u7ba1\u7406\u5458\u4e3a\u5fc5\u586b\u9879\u3002Tenant ID \u7559\u7a7a\u65f6\u81ea\u52a8\u751f\u6210\u3002', en: 'Tenant slug, name, and initial admin credentials are required. Tenant ID is generated when blank.' },
    adminCreateTitle: { zh: '\u6dfb\u52a0\u79df\u6237\u7ba1\u7406\u5458', en: 'Add Tenant Admin' },
    adminCreateDesc: { zh: '\u4e3a\u5df2\u6709\u79df\u6237\u589e\u52a0\u79df\u6237\u8303\u56f4\u7684\u7ba1\u7406\u8d26\u53f7\u3002', en: 'Add a tenant-scoped admin account to an existing tenant.' },
    listTitle: { zh: '\u79df\u6237\u5217\u8868', en: 'Tenant List' },
    listDesc: { zh: '\u5f53\u524d Hub \u79df\u6237\u53ca\u5176\u72b6\u6001\u548c\u4e3b\u57df\u540d\u3002', en: 'Hub tenants, status, and primary domains.' },
    listDescTenant: { zh: '\u5f53\u524d\u767b\u5f55\u7ba1\u7406\u5458\u6240\u5c5e\u7684\u79df\u6237\u3002', en: 'The tenant owned by the signed-in tenant admin.' },
    reload: { zh: '\u91cd\u65b0\u52a0\u8f7d', en: 'Reload' },
    create: { zh: '\u521b\u5efa\u79df\u6237', en: 'Create Tenant' },
    addAdmin: { zh: '\u6dfb\u52a0\u7ba1\u7406\u5458', en: 'Add Admin' },
    slug: { zh: '\u79df\u6237 Slug', en: 'Tenant Slug' },
    name: { zh: '\u79df\u6237\u540d\u79f0', en: 'Tenant Name' },
    tenantID: { zh: 'Tenant ID', en: 'Tenant ID' },
    domain: { zh: '\u4e3b\u57df\u540d', en: 'Primary Domain' },
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
    systemNavTenant: { zh: '\u8d26\u53f7\u8bbe\u7f6e', en: 'Account Settings' },
    systemNavDescTenant: { zh: '\u7ba1\u7406\u672c\u79df\u6237\u7ba1\u7406\u5458\u7684\u90ae\u7bb1\u548c\u5bc6\u7801', en: 'Manage this tenant admin email and password' },
    systemTitleTenant: { zh: '\u8d26\u53f7\u8bbe\u7f6e', en: 'Account Settings' },
    systemDescTenant: { zh: '\u79df\u6237\u7ba1\u7406\u5458\u53ef\u5728\u8fd9\u91cc\u66f4\u65b0\u81ea\u5df1\u7684\u90ae\u7bb1\u548c\u767b\u5f55\u5bc6\u7801\u3002', en: 'Tenant admins can update their own email address and login password here.' },
    systemSubtitleTenant: { zh: '\u79df\u6237\u7ba1\u7406\u5458\u8d26\u53f7\u5b89\u5168\u8bbe\u7f6e\u3002', en: 'Tenant admin account security settings.' },
    role: { zh: '\u89d2\u8272', en: 'Role' },
    empty: { zh: '\u6682\u65e0\u79df\u6237\u3002', en: 'No tenants yet.' },
    loading: { zh: '\u52a0\u8f7d\u4e2d...', en: 'Loading...' },
    forbidden: { zh: '\u9700\u8981\u5168\u5c40\u7ba1\u7406\u5458\u6743\u9650\u624d\u80fd\u67e5\u770b\u548c\u521b\u5efa\u79df\u6237\u3002', en: 'Global admin authorization is required to view and create tenants.' },
    required: { zh: '\u8bf7\u586b\u5199\u79df\u6237 Slug\u3001\u540d\u79f0\u3001\u521d\u59cb\u7ba1\u7406\u5458\u8d26\u53f7\u3001\u5bc6\u7801\u548c\u90ae\u7bb1\u3002', en: 'Fill tenant slug, name, initial admin username, password, and email.' },
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
    deactivate: { zh: '\u505c\u7528', en: 'Deactivate' },
    reactivate: { zh: '\u542f\u7528', en: 'Reactivate' },
    deleteTenant: { zh: '\u5220\u9664', en: 'Delete' },
    deactivateConfirm: { zh: '\u786e\u8ba4\u505c\u7528\u79df\u6237 {tenant} \u5417\uff1f', en: 'Deactivate tenant {tenant}?' },
    reactivateConfirm: { zh: '\u786e\u8ba4\u91cd\u65b0\u542f\u7528\u79df\u6237 {tenant} \u5417\uff1f', en: 'Reactivate tenant {tenant}?' },
    deleteConfirm: { zh: '\u786e\u8ba4\u5220\u9664\u79df\u6237 {tenant} \u5417\uff1f\u6b64\u64cd\u4f5c\u4f1a\u7acb\u5373\u505c\u6b62\u8be5\u79df\u6237 IM \u8fd0\u884c\u65f6\u3002', en: 'Delete tenant {tenant}? This immediately stops tenant IM runtimes.' },
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
  function tenantLabel(item) { return String(item && (item.name || item.slug || item.id) || ''); }
  function tenantOptionLabel(item) {
    if (!item) return '';
    var label = tenantLabel(item);
    var suffix = item.primary_domain || item.slug || item.id || '';
    return suffix && suffix !== label ? label + ' (' + suffix + ')' : label;
  }
  function tenantStatus(item) {
    if (!item) return 'active';
    if (item.deleted_at) return 'deleted';
    return String(item.status || 'active').toLowerCase();
  }
  function isReservedTenantID(id) {
    id = String(id || '').trim();
    return id === '__global__' || id === 'tenant_default';
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
    return '<div style="display:flex;gap:6px;justify-content:flex-end;flex-wrap:wrap">'
      + '<button class="btn-ghost" style="height:30px;font-size:11px;padding:0 10px" type="button" onclick="updateTenantStatus(' + JSON.stringify(item.id) + ',' + JSON.stringify(nextStatus) + ',' + JSON.stringify(label) + ',' + JSON.stringify(confirmKey) + ')">' + esc(tt(statusKey)) + '</button>'
      + '<button class="btn-danger" style="height:30px;font-size:11px;padding:0 10px" type="button" onclick="deleteTenant(' + JSON.stringify(item.id) + ',' + JSON.stringify(label) + ')">' + esc(tt('deleteTenant')) + '</button>'
      + '</div>';
  }
  function applyTenantI18n() {
    setText('navTenants', 'nav'); setText('navTenantsDesc', tenantScoped() ? 'navDescTenant' : 'navDesc'); setText('loginTenantLabel', 'loginTenant'); setText('loginTenantHint', 'loginTenantHint'); setText('tenantsTitle', 'title'); setText('tenantsDesc', tenantScoped() ? 'descTenant' : 'desc');
    setText('tenantsReloadBtn', 'reload'); setText('tenantCreateTitle', 'createTitle'); setText('tenantCreateDesc', 'createDesc');
    setText('tenantAdminCreateTitle', 'adminCreateTitle'); setText('tenantAdminCreateDesc', 'adminCreateDesc'); setText('tenantListTitle', 'listTitle'); setText('tenantListDesc', tenantScoped() ? 'listDescTenant' : 'listDesc');
    setText('tenantCreateBtn', 'create'); setText('tenantAdminCreateBtn', 'addAdmin'); setText('tenantSlugLabel', 'slug'); setText('tenantNameLabel', 'name'); setText('tenantIDLabel', 'tenantID'); setText('tenantDomainLabel', 'domain');
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
    var list = Array.isArray(items) ? items.filter(function(item) { return item && !isReservedTenantID(item.id); }) : [];
    if (badge) badge.textContent = String(list.length);
    renderTenantSelect(list);
    if (!root) return;
    if (!list.length) {
      root.innerHTML = '<div class="hint" id="tenantListEmpty">' + esc(tt('empty')) + '</div>';
      return;
    }
    root.innerHTML = '<div style="display:grid;gap:8px">' + list.map(function(item) {
      var domain = item.primary_domain || '-';
      var statusTitle = tt('updated') + ': ' + fmtTime(item.updated_at);
      if (item.deleted_at) statusTitle += ' / ' + tt('deleted') + ': ' + fmtTime(item.deleted_at);
      return '<div class="item" style="padding:10px 12px;border-radius:12px;background:#fff;border:1px solid rgba(31,34,48,.06);box-shadow:none">'
        + '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:10px;align-items:center">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(tenantLabel(item)) + '</div><div class="item-meta mono" style="font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(item.id || '') + '</div></div>'
        + '<div style="min-width:0"><label style="margin:0 0 3px;font-size:9px">Slug</label><div class="mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(item.slug || '-') + '</div></div>'
        + '<div style="min-width:0"><label style="margin:0 0 3px;font-size:9px">' + esc(tt('domain')) + '</label><div class="mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(domain) + '</div></div>'
        + '<div style="display:flex;justify-content:flex-start"><span class="badge ' + esc(tenantBadgeClass(item)) + '" title="' + esc(statusTitle) + '">' + esc(tenantStatusText(item)) + '</span></div>'
        + tenantAdminActions(item)
        + '</div></div>';
    }).join('') + '</div>';
  }

  async function loadLoginTenants() {
    try {
      var res = await global.fetch('/api/admin/login/tenants', { headers: { 'Accept': 'application/json' } });
      var data = {};
      try { data = await res.json(); } catch (_) {}
      if (!res.ok) throw new Error(data.message || res.statusText || 'request failed');
      loginTenantOptionsCache = Array.isArray(data.tenants) ? data.tenants : [];
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
      } else {
        var data = await global.api('/api/admin/tenants');
        tenantCache = Array.isArray(data.tenants) ? data.tenants : [];
      }
      renderTenants(tenantCache);
      return tenantCache;
    } catch (err) {
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
  async function createTenant() {
    if (tenantCreateBusy) return;
    var payload = {
      id: val('tenantID'), slug: val('tenantSlug'), name: val('tenantName'), primary_domain: val('tenantDomain'),
      initial_admin_username: val('tenantAdminUsername'), initial_admin_password: val('tenantAdminPassword'), initial_admin_email: val('tenantAdminEmail'), initial_admin_name: val('tenantAdminName')
    };
    if (!payload.slug || !payload.name || !payload.initial_admin_username || !payload.initial_admin_password || !payload.initial_admin_email) {
      setTenantOutput(tt('required'), 'info');
      return;
    }
    if (!validEmail(payload.initial_admin_email)) {
      setTenantOutput(tt('invalidAdminEmail'), 'info');
      return;
    }
    var btn = byID('tenantCreateBtn');
    tenantCreateBusy = true;
    if (btn) btn.disabled = true;
    try {
      var data = await global.api('/api/admin/tenants', { method: 'POST', body: JSON.stringify(payload) });
      ['tenantID','tenantSlug','tenantName','tenantDomain','tenantAdminUsername','tenantAdminPassword','tenantAdminEmail','tenantAdminName'].forEach(function(id) { var el = byID(id); if (el) el.value = ''; });
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
    if (!global.confirm(tt(confirmKey || 'deactivateConfirm', { tenant: label || tenantID }))) return;
    try {
      await global.api('/api/admin/tenants/' + encodeURIComponent(tenantID) + '/status', { method: 'PATCH', body: JSON.stringify({ status: status }) });
      await loadTenants();
      await loadLoginTenants();
      setTenantOutput(tt('statusUpdated'), 'success');
    } catch (err) {
      setTenantOutput(tt('statusUpdateFailed', { error: err.message || err }), 'error');
    }
  }

  async function deleteTenant(tenantID, label) {
    if (!tenantID) return;
    if (!global.confirm(tt('deleteConfirm', { tenant: label || tenantID }))) return;
    try {
      await global.api('/api/admin/tenants/' + encodeURIComponent(tenantID), { method: 'DELETE' });
      await loadTenants();
      await loadLoginTenants();
      setTenantOutput(tt('deletedDone'), 'success');
    } catch (err) {
      setTenantOutput(tt('deleteFailed', { error: err.message || err }), 'error');
    }
  }

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
    var globalOnly = global.adminGlobalOnlyTabs || { center: true, console: true, hubllm: true };
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
  global.updateTenantStatus = updateTenantStatus;
  global.deleteTenant = deleteTenant;
  applyAdminScopeUI();
})(window);


