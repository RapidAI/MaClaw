/*
 * Platform controls for Bot nodes. Global admins only.
 * The block stays hidden while HUB_EDGE_NODES is off.
 */
(function (global) {
  var copy = {
    zh: {
      title: '\u79df\u6237\u8282\u70b9\u9650\u5236',
      desc: '\u8bbe\u7f6e\u6bcf\u4e2a\u79df\u6237\u80fd\u4e0d\u80fd\u81ea\u5efa\u8282\u70b9\uff0c\u4ee5\u53ca\u8282\u70b9\u6570\u3001\u96a7\u9053\u8fde\u63a5\u548c\u753b\u9762\u8def\u6570\u4e0a\u9650\u30020 \u8868\u793a\u4e0d\u9650\u3002\u5217\u8868\u53ea\u663e\u793a\u72b6\u6001\uff0c\u4e0d\u663e\u793a\u5bc6\u94a5\u3002',
      tenant: '\u79df\u6237',
      allow: '\u5141\u8bb8\u81ea\u5efa\u8282\u70b9',
      maxNodes: '\u8282\u70b9\u6570\u4e0a\u9650',
      maxStreams: '\u96a7\u9053\u8fde\u63a5\u4e0a\u9650',
      maxViews: '\u753b\u9762\u8def\u6570\u4e0a\u9650',
      save: '\u4fdd\u5b58',
      saved: '\u5df2\u4fdd\u5b58',
      empty: '\u8fd8\u6ca1\u6709\u8282\u70b9',
      online: '\u5728\u7ebf',
      offline: '\u79bb\u7ebf',
      badLimit: '\u4e0a\u9650\u5fc5\u987b\u662f 0 \u6216\u6b63\u6574\u6570'
    },
    en: {
      title: 'Tenant node limits',
      desc: 'Choose whether each tenant may register its own nodes, and cap the node count, tunnel connections, and live views. 0 means no cap. The list shows status only.',
      tenant: 'Tenant',
      allow: 'Allow self-registered nodes',
      maxNodes: 'Node cap',
      maxStreams: 'Tunnel connection cap',
      maxViews: 'Live view cap',
      save: 'Save',
      saved: 'Saved',
      empty: 'No nodes yet',
      online: 'Online',
      offline: 'Offline',
      badLimit: 'A cap must be 0 or a positive integer'
    }
  };

  function text() {
    var lang = global.hubAdminLang ? global.hubAdminLang() : (global.currentLang === 'zh' ? 'zh' : 'en');
    return copy[lang] || copy.en;
  }

  function byID(id) {
    return document.getElementById(id);
  }

  function esc(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function showToast(msg, kind) {
    if (typeof global.showToast === 'function') global.showToast(msg, kind);
  }

  function tenantAdmin() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  function show(on) {
    var box = byID('tenantEdgePolicy');
    if (!box) return;
    box.hidden = !on;
    if (box.classList) box.classList.toggle('hidden', !on);
  }

  function applyLabels() {
    var t = text();
    var title = byID('tenantEdgePolicyTitle');
    var desc = byID('tenantEdgePolicyDesc');
    if (title) title.textContent = t.title;
    if (desc) desc.textContent = t.desc;
    var labels = {
      tenantEdgeTenantLabel: t.tenant,
      tenantEdgeAllowLabel: t.allow,
      tenantEdgeMaxNodesLabel: t.maxNodes,
      tenantEdgeMaxStreamsLabel: t.maxStreams,
      tenantEdgeMaxViewsLabel: t.maxViews
    };
    Object.keys(labels).forEach(function (id) {
      var el = byID(id);
      if (el) el.textContent = labels[id];
    });
    var button = byID('tenantEdgeSave');
    if (button) button.textContent = t.save;
  }

  function limitOf(raw) {
    var value = String(raw == null ? '' : raw).trim();
    if (value === '') return 0;
    if (!/^(0|[1-9][0-9]{0,8})$/.test(value)) return null;
    return Number(value);
  }

  function setLimit(id, value) {
    var input = byID(id);
    if (!input) return;
    var n = Number(value);
    input.value = Number.isFinite(n) && n > 0 ? String(Math.floor(n)) : '0';
  }

  function renderNodes(nodes) {
    var list = byID('tenantEdgeNodeList');
    if (!list) return;
    nodes = nodes || [];
    if (!nodes.length) {
      list.innerHTML = '';
      list.textContent = text().empty;
      return;
    }
    list.innerHTML = nodes.map(function (node) {
      var caps = [];
      if (node && node.has_desktopd) caps.push('desktopd');
      if (node && node.has_srv) caps.push('srv');
      var status = node && node.online ? text().online : text().offline;
      return '<div><strong>' + esc(node && node.tenant_id) + '</strong> ' + esc((node && (node.name || node.id)) || '')
        + ' <span>' + esc(status) + '</span>'
        + ' <span>' + esc(caps.join(' / ') || '-') + '</span>'
        + ' <span>' + esc((node && node.version) || '-') + '</span></div>';
    }).join('');
  }

  function renderTenants(tenants, nodes) {
    var select = byID('tenantEdgeTenant');
    if (!select) return;
    var previous = select.value;
    var seen = {};
    var options = [];
    function add(id, label) {
      id = String(id || '').trim();
      if (!id || seen[id]) return;
      seen[id] = true;
      options.push({ id: id, label: label || id });
    }
    (tenants || []).forEach(function (item) {
      if (!item) return;
      add(item.id, item.name ? item.name + ' (' + item.id + ')' : item.id);
    });
    (nodes || []).forEach(function (node) {
      if (node) add(node.tenant_id, node.tenant_id);
    });
    select.innerHTML = options.map(function (item) {
      return '<option value="' + esc(item.id) + '">' + esc(item.label) + '</option>';
    }).join('');
    if (previous && seen[previous]) select.value = previous;
    else if (options[0]) select.value = options[0].id;
  }

  var policySeq = 0;
  var saveToken = 0;
  var savingId = '';
  var applied = null;

  function fieldValue(id) {
    var el = byID(id);
    return el ? String(el.value) : '0';
  }

  function selectedID() {
    var select = byID('tenantEdgeTenant');
    return select ? String(select.value || '').trim() : '';
  }

  function rememberApplied(id) {
    var allow = byID('tenantEdgeAllow');
    applied = {
      id: id,
      allow: !!(allow && allow.checked),
      nodes: fieldValue('tenantEdgeMaxNodes'),
      streams: fieldValue('tenantEdgeMaxStreams'),
      views: fieldValue('tenantEdgeMaxViews')
    };
  }

  function rememberSent(id, body) {
    applied = {
      id: id,
      allow: !!body.allow_self,
      nodes: String(body.max_nodes),
      streams: String(body.max_streams),
      views: String(body.max_views)
    };
  }

  function setFieldsLocked(locked) {
    ['tenantEdgeTenant', 'tenantEdgeAllow', 'tenantEdgeMaxNodes', 'tenantEdgeMaxStreams', 'tenantEdgeMaxViews'].forEach(function (id) {
      var el = byID(id);
      if (el) el.disabled = locked;
    });
  }

  function syncSaveButton() {
    var button = byID('tenantEdgeSave');
    if (!button) return;
    button.disabled = !!savingId || !applied || applied.id !== selectedID();
  }

  function formDirty() {
    if (!applied) return false;
    var select = byID('tenantEdgeTenant');
    var id = select && String(select.value || '').trim();
    if (id !== applied.id) return false;
    var allow = byID('tenantEdgeAllow');
    return (!!(allow && allow.checked)) !== applied.allow
      || fieldValue('tenantEdgeMaxNodes') !== applied.nodes
      || fieldValue('tenantEdgeMaxStreams') !== applied.streams
      || fieldValue('tenantEdgeMaxViews') !== applied.views;
  }

  function loadPolicy() {
    var select = byID('tenantEdgeTenant');
    var id = select && String(select.value || '').trim();
    var seq = ++policySeq;
    if (!id) return Promise.resolve();
    return global.api('/api/admin/platform/tenants/' + encodeURIComponent(id) + '/edge-policy').then(function (policy) {
      var current = byID('tenantEdgeTenant');
      if (seq !== policySeq || savingId === id || !current || String(current.value || '').trim() !== id) return;
      var allow = byID('tenantEdgeAllow');
      if (allow) allow.checked = !policy || policy.allow_self !== false;
      setLimit('tenantEdgeMaxNodes', policy && policy.max_nodes);
      setLimit('tenantEdgeMaxStreams', policy && policy.max_streams);
      setLimit('tenantEdgeMaxViews', policy && policy.max_views);
      rememberApplied(id);
      syncSaveButton();
    }).catch(function (err) {
      if (seq !== policySeq) return;
      showToast(err && err.message ? err.message : String(err), 'error');
    });
  }

  function save() {
    var select = byID('tenantEdgeTenant');
    var id = select && String(select.value || '').trim();
    if (!id || !applied || applied.id !== id) return;
    var nodes = limitOf(byID('tenantEdgeMaxNodes') && byID('tenantEdgeMaxNodes').value);
    var streams = limitOf(byID('tenantEdgeMaxStreams') && byID('tenantEdgeMaxStreams').value);
    var views = limitOf(byID('tenantEdgeMaxViews') && byID('tenantEdgeMaxViews').value);
    if (nodes == null || streams == null || views == null) {
      showToast(text().badLimit, 'error');
      return;
    }
    var token = ++saveToken;
    savingId = id;
    policySeq += 1;
    var body = { allow_self: !!(byID('tenantEdgeAllow') && byID('tenantEdgeAllow').checked), max_nodes: nodes, max_streams: streams, max_views: views };
    setFieldsLocked(true);
    syncSaveButton();
    return global.api('/api/admin/platform/tenants/' + encodeURIComponent(id) + '/edge-policy', {
      method: 'PUT',
      body: JSON.stringify(body)
    }).then(function () {
      if (token !== saveToken) return;
      if (selectedID() === id) {
        rememberSent(id, body);
        if (!formDirty()) showToast(text().saved, 'success');
      }
    }).catch(function (err) {
      if (token !== saveToken) return;
      showToast(err && err.message ? err.message : String(err), 'error');
    }).then(function () {
      if (token !== saveToken) return;
      savingId = '';
      setFieldsLocked(false);
      if (selectedID() === id) syncSaveButton();
      else loadPolicy();
    });
  }

  var viewSeq = 0;
  var shownNodes = null;
  var bound = false;
  function bind() {
    if (bound) return;
    bound = true;
    var select = byID('tenantEdgeTenant');
    if (select) select.addEventListener('change', function () {
      if (select.disabled) return;
      syncSaveButton();
      loadPolicy().catch(function (err) { showToast(err && err.message ? err.message : String(err), 'error'); });
    });
    var button = byID('tenantEdgeSave');
    if (button) button.addEventListener('click', save);
  }

  function load() {
    var seq = ++viewSeq;
    bind();
    applyLabels();
    if (tenantAdmin() || typeof global.api !== 'function') {
      show(false);
      return Promise.resolve();
    }
    return global.api('/api/admin/platform/edge-nodes').then(function (view) {
      if (seq !== viewSeq) return null;
      var nodes = view && Array.isArray(view.nodes) ? view.nodes : [];
      return global.api('/api/admin/tenants').then(function (data) {
        return { nodes: nodes, tenants: data && Array.isArray(data.tenants) ? data.tenants : [] };
      }, function () {
        return { nodes: nodes, tenants: [] };
      });
    }, function (err) {
      if (seq !== viewSeq) return null;
      var msg = err && err.message ? String(err.message) : String(err || '');
      if (shownNodes && msg !== 'edge nodes are disabled') {
        showToast(msg, 'error');
        return null;
      }
      show(false);
      return null;
    }).then(function (loaded) {
      if (!loaded || seq !== viewSeq) return;
      show(true);
      shownNodes = loaded.nodes;
      renderNodes(shownNodes);
      renderTenants(loaded.tenants, loaded.nodes);
      var selected = byID('tenantEdgeTenant');
      var selectedID = selected && String(selected.value || '').trim();
      if (savingId && selectedID === savingId) return;
      if (formDirty()) return;
      return loadPolicy();
    });
  }

  global.loadTenantEdgePolicy = load;
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function () {
      applyLabels();
      if (shownNodes) renderNodes(shownNodes);
    });
  }
})(window);
