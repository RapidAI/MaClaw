/*
 * Tenant cloud-workspace settings (system tab card + org tree).
 * ASCII only in source; CJK via i18n \u escapes.
 * Department tree is an independent copy of digital-assets-tab.js
 * (do not extract a shared widget).
 */
(function(global) {
  var CWS_I18N = {
    en: {
      title: 'Cloud Workspace',
      desc: 'Enable cloud workspaces for all users, or for multiple selected departments (including descendants). Users who are not enabled never see cloud workspaces when creating a task.',
      reload: 'Reload',
      save: 'Save Settings',
      saving: 'Saving...',
      saved: 'Cloud workspace settings saved.',
      hint: 'Quota is 1-10 workspaces per user. Per-workspace capacity is 256-8192 MiB. Tenant total is 1-1024 GiB.',
      modeLabel: 'Availability',
      modeOff: 'Off',
      modeAllUsers: 'Open to all users',
      modeDepartments: 'Open by department (multi-select)',
      departmentsLabel: 'Departments',
      departmentsHint: 'Check one or more departments. Checking a parent includes all descendants. Only members of the selected departments (and their descendants) can use cloud workspaces.',
      selectedDepartments: '{n} department(s) selected',
      deptFilter: 'Filter departments...',
      deptUnknown: 'Unknown department (kept): {id}',
      departmentsEmpty: 'No security groups found. Create departments under Security first.',
      departmentsLoading: 'Loading departments...',
      emptyDepartmentsWarn: 'Select at least one department when opening by department.',
      groupsFailed: 'Failed to load departments: {error}',
      reloadGroups: 'Reload departments',
      clearDepartments: 'Clear selection',
      selectVisible: 'Select visible',
      selectedChipsAria: 'Selected departments',
      quotaLabel: 'Per-user quota (1-10)',
      maxMiBLabel: 'Per-workspace capacity (MiB)',
      tenantGiBLabel: 'Tenant total capacity (GiB)',
      preview: 'Currently covering {n} departments, about {m} users',
      overQuota: '{k} users already exceed the new quota ({sns}); they cannot create new workspaces; existing workspaces are kept.',
      loadFailed: 'Load cloud workspace settings failed: {error}',
      saveFailed: 'Save cloud workspace settings failed: {error}',
      invalidQuota: 'Please enter a quota from 1 to 10.',
      invalidMaxMiB: 'Please enter a per-workspace capacity from 256 to 8192 MiB.',
      invalidTenantGiB: 'Please enter a tenant total capacity from 1 to 1024 GiB.'
    },
    zh: {
      title: '\u4e91\u5de5\u4f5c\u533a',
      desc: '\u53ef\u4e3a\u5168\u5458\u5f00\u901a\uff0c\u4e5f\u53ef\u540c\u65f6\u52fe\u9009\u591a\u4e2a\u90e8\u95e8\u5f00\u901a\uff08\u542b\u5b50\u90e8\u95e8\uff09\u3002\u672a\u5f00\u901a\u7684\u7528\u6237\u5728\u65b0\u5efa\u4efb\u52a1\u65f6\u770b\u4e0d\u5230\u4e91\u7aef\u5de5\u4f5c\u533a\u3002',
      reload: '\u5237\u65b0',
      save: '\u4fdd\u5b58\u8bbe\u7f6e',
      saving: '\u4fdd\u5b58\u4e2d...',
      saved: '\u4e91\u5de5\u4f5c\u533a\u8bbe\u7f6e\u5df2\u4fdd\u5b58\u3002',
      hint: '\u4eba\u5747\u914d\u989d 1-10 \u4e2a\u5de5\u4f5c\u533a\u3002\u5355\u5de5\u4f5c\u533a\u5bb9\u91cf 256-8192 MiB\u3002\u79df\u6237\u603b\u5bb9\u91cf 1-1024 GiB\u3002',
      modeLabel: '\u5f00\u653e\u8303\u56f4',
      modeOff: '\u5173\u95ed',
      modeAllUsers: '\u5168\u5458\u5f00\u653e',
      modeDepartments: '\u6309\u90e8\u95e8\u5f00\u653e\uff08\u53ef\u591a\u9009\uff09',
      departmentsLabel: '\u90e8\u95e8',
      departmentsHint: '\u53ef\u540c\u65f6\u52fe\u9009\u591a\u4e2a\u90e8\u95e8\u3002\u52fe\u9009\u7236\u90e8\u95e8\u4f1a\u5305\u542b\u5176\u5168\u90e8\u5b50\u5b59\u3002\u53ea\u6709\u6240\u9009\u90e8\u95e8\u53ca\u5176\u5b50\u5b59\u7684\u6210\u5458\u53ef\u4ee5\u4f7f\u7528\u4e91\u7aef\u5de5\u4f5c\u533a\u3002',
      selectedDepartments: '\u5df2\u9009 {n} \u4e2a\u90e8\u95e8',
      deptFilter: '\u7b5b\u9009\u90e8\u95e8...',
      deptUnknown: '\u672a\u77e5\u90e8\u95e8\uff08\u5df2\u4fdd\u7559\uff09\uff1a{id}',
      departmentsEmpty: '\u672a\u627e\u5230\u5b89\u5168\u7ec4\u3002\u8bf7\u5148\u5728\u5b89\u5168\u7ba1\u7406\u4e2d\u521b\u5efa\u90e8\u95e8\u3002',
      departmentsLoading: '\u6b63\u5728\u52a0\u8f7d\u90e8\u95e8...',
      emptyDepartmentsWarn: '\u6309\u90e8\u95e8\u5f00\u653e\u65f6\u8bf7\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a\u90e8\u95e8\u3002',
      groupsFailed: '\u52a0\u8f7d\u90e8\u95e8\u5931\u8d25: {error}',
      reloadGroups: '\u91cd\u65b0\u52a0\u8f7d\u90e8\u95e8',
      clearDepartments: '\u6e05\u7a7a\u9009\u62e9',
      selectVisible: '\u5168\u9009\u5f53\u524d\u53ef\u89c1',
      selectedChipsAria: '\u5df2\u9009\u90e8\u95e8',
      quotaLabel: '\u4eba\u5747\u914d\u989d\uff081-10\uff09',
      maxMiBLabel: '\u5355\u5de5\u4f5c\u533a\u5bb9\u91cf\uff08MiB\uff09',
      tenantGiBLabel: '\u79df\u6237\u603b\u5bb9\u91cf\uff08GiB\uff09',
      preview: '\u5f53\u524d\u5c06\u8986\u76d6 {n} \u4e2a\u90e8\u95e8\u3001\u7ea6 {m} \u540d\u7528\u6237',
      overQuota: '{k} \u540d\u7528\u6237\u5df2\u8d85\u8fc7\u65b0\u914d\u989d\uff08{sns}\uff09\uff0c\u5c06\u65e0\u6cd5\u65b0\u5efa\uff0c\u73b0\u6709\u5de5\u4f5c\u533a\u4fdd\u7559',
      loadFailed: '\u52a0\u8f7d\u4e91\u5de5\u4f5c\u533a\u8bbe\u7f6e\u5931\u8d25: {error}',
      saveFailed: '\u4fdd\u5b58\u4e91\u5de5\u4f5c\u533a\u8bbe\u7f6e\u5931\u8d25: {error}',
      invalidQuota: '\u8bf7\u8f93\u5165 1 \u5230 10 \u7684\u914d\u989d\u3002',
      invalidMaxMiB: '\u8bf7\u8f93\u5165 256 \u5230 8192 MiB \u7684\u5355\u5de5\u4f5c\u533a\u5bb9\u91cf\u3002',
      invalidTenantGiB: '\u8bf7\u8f93\u5165 1 \u5230 1024 GiB \u7684\u79df\u6237\u603b\u5bb9\u91cf\u3002'
    }
  };

  var CWS_QUOTA_MIN = 1;
  var CWS_QUOTA_MAX = 10;
  var CWS_QUOTA_DEFAULT = 5;
  var CWS_MAX_MIB_MIN = 256;
  var CWS_MAX_MIB_MAX = 8192;
  var CWS_MAX_MIB_DEFAULT = 2048;
  var CWS_TENANT_GIB_MIN = 1;
  var CWS_TENANT_GIB_MAX = 1024;
  var CWS_TENANT_GIB_DEFAULT = 50;
  var MIB = 1024 * 1024;
  var GIB = 1024 * 1024 * 1024;
  var cwsMaxDepartmentTreeDepth = 64;

  var state = {
    securityGroupTree: [],
    securityGroups: [],
    securityGroupsLoading: false,
    securityGroupsLoaded: false,
    securityGroupsError: '',
    securityGroupsPromise: null,
    selectedDepts: [],
    deptFilter: '',
    preview: { department_count: 0, user_count: 0, over_quota_users: [] },
    saving: false
  };

  function cwsx(key, vars) {
    vars = vars || {};
    var lang = global.currentLang || 'en';
    var table = CWS_I18N[lang] || CWS_I18N.en;
    var text = table[key] || CWS_I18N.en[key] || key;
    return String(text).replace(/\{(\w+)\}/g, function(_, name) {
      return vars[name] != null ? String(vars[name]) : '';
    });
  }

  function byID(id) { return global.document.getElementById(id); }

  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function api(path, opts) {
    if (typeof global.api === 'function') return global.api(path, opts);
    return Promise.reject(new Error('api unavailable'));
  }

  function showToast(msg, kind) {
    if (typeof global.showToast === 'function') global.showToast(msg, kind);
  }

  function setOutput(msg) {
    if (typeof global.setOutput === 'function') global.setOutput(msg);
  }

  function setText(id, val) {
    var el = byID(id);
    if (el) el.textContent = val;
  }

  function cardEl() { return byID('tenantCloudWorkspaceSettingsCard'); }

  function cardQueryAll(sel) {
    var card = cardEl();
    return card ? Array.prototype.slice.call(card.querySelectorAll(sel)) : [];
  }

  function canManageTenantCloudWorkspace() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!profile;
  }

  function actionButton(id, label, kind) {
    var cls = 'btn-secondary';
    if (kind === 'danger') cls = 'btn-danger';
    else if (kind === 'primary') cls = 'btn-primary';
    else if (kind === 'ghost') cls = 'btn-ghost';
    return '<button type="button" class="' + cls + '" id="' + id + '" style="height:34px;padding:0 12px;font-size:12px">'
      + escapeHtml(label) + '</button>';
  }

  function flattenSecurityGroups(node, path, out) {
    if (!node) return;
    var id = String(node.id || '').trim();
    var name = String(node.name || '').trim();
    var label = path ? (path + ' / ' + (name || id)) : (name || id);
    if (id) out.push({ id: id, name: name, path: label });
    var children = Array.isArray(node.children) ? node.children : [];
    children.forEach(function(child) {
      flattenSecurityGroups(child, label, out);
    });
  }

  function normalizeSecurityGroupTree(nodes, ancestry, seenIDs, depth) {
    var safeNodes = Array.isArray(nodes) ? nodes : [];
    var path = ancestry || {};
    var seen = seenIDs || {};
    var currentDepth = Number(depth) || 0;
    if (currentDepth > cwsMaxDepartmentTreeDepth) return [];
    return safeNodes.reduce(function(out, node) {
      if (!node || typeof node !== 'object') return out;
      var id = String(node.id || '').trim();
      if (!id || path[id] || seen[id]) return out;
      var nextPath = Object.assign({}, path);
      nextPath[id] = true;
      seen[id] = true;
      out.push({
        id: id,
        name: String(node.name || '').trim(),
        children: normalizeSecurityGroupTree(node.children, nextPath, seen, currentDepth + 1)
      });
      return out;
    }, []);
  }

  async function loadSecurityGroups(opts) {
    opts = opts || {};
    if (state.securityGroupsPromise) {
      await state.securityGroupsPromise;
      if (state.securityGroupsLoaded && !opts.force) {
        if (opts.renderTree !== false) renderCwsAclPanel();
        return state.securityGroups;
      }
    }
    if (state.securityGroupsLoaded && !opts.force) {
      if (opts.renderTree !== false) renderCwsAclPanel();
      return state.securityGroups;
    }

    state.securityGroupsLoading = true;
    if (opts.renderTree !== false) renderCwsAclPanel();
    var pending = (async function() {
      try {
        var data = await api('/api/admin/security/groups');
        var out = [];
        // API may return a single root node or (rarely) an array of roots.
        var tree = data && data.tree;
        var roots = normalizeSecurityGroupTree(Array.isArray(tree) ? tree : (tree ? [tree] : []));
        roots.forEach(function(node) { flattenSecurityGroups(node, '', out); });
        state.securityGroupTree = roots;
        state.securityGroups = out;
        state.securityGroupsError = '';
        state.securityGroupsLoaded = true;
      } catch (err) {
        state.securityGroups = [];
        state.securityGroupTree = [];
        state.securityGroupsError = String(err && err.message || err || 'error');
        state.securityGroupsLoaded = true;
      } finally {
        state.securityGroupsLoading = false;
        if (state.securityGroupsPromise === pending) state.securityGroupsPromise = null;
      }
    })();
    state.securityGroupsPromise = pending;
    await pending;
    if (opts.renderTree !== false) renderCwsAclPanel();
    return state.securityGroups;
  }

  function uniqueStrings(arr) {
    var seen = {};
    var out = [];
    (arr || []).forEach(function(x) {
      var k = String(x || '');
      if (!k || seen[k]) return;
      seen[k] = true;
      out.push(k);
    });
    return out;
  }

  function isDeptSelected(depts, deptId) {
    var want = String(deptId || '').trim();
    if (!want) return false;
    for (var i = 0; i < depts.length; i++) {
      if (depts[i] === want) return true;
    }
    return false;
  }

  function knownDeptIdSet() {
    var known = {};
    (state.securityGroups || []).forEach(function(g) {
      if (g && g.id) known[String(g.id)] = true;
    });
    return known;
  }

  function unknownSelectedDepartments(depts) {
    var known = knownDeptIdSet();
    return (depts || []).filter(function(id) { return id && !known[id]; });
  }

  function collectSelectedFromDom() {
    var boxes = cardQueryAll('.cws-acl-tree-dept');
    // No inputs means the tree is not mounted yet (loading/empty/error), not an empty selection.
    if (!boxes.length) return state.selectedDepts;
    var depts = [];
    boxes.forEach(function(cb) {
      if (cb.checked) depts.push(String(cb.value || '').trim());
    });
    state.selectedDepts = uniqueStrings(depts.filter(Boolean));
    return state.selectedDepts;
  }

  function currentMode() {
    var depts = byID('tenantCloudWorkspaceModeDepartments');
    if (depts && depts.checked) return 'departments';
    var allUsers = byID('tenantCloudWorkspaceModeAllUsers');
    if (allUsers && allUsers.checked) return 'all_users';
    return 'off';
  }

  function setMode(mode) {
    var normalized = mode === 'all_users' || mode === 'departments' ? mode : 'off';
    var off = byID('tenantCloudWorkspaceModeOff');
    var allUsers = byID('tenantCloudWorkspaceModeAllUsers');
    var depts = byID('tenantCloudWorkspaceModeDepartments');
    if (off) off.checked = normalized === 'off';
    if (allUsers) allUsers.checked = normalized === 'all_users';
    if (depts) depts.checked = normalized === 'departments';
    updateDepartmentsVisibility();
  }

  function updateDepartmentsVisibility() {
    var restricted = currentMode() === 'departments';
    var box = byID('tenantCloudWorkspaceDepartmentsBox');
    if (box) {
      box.hidden = !restricted;
      box.setAttribute('aria-hidden', restricted ? 'false' : 'true');
    }
    updateEmptyWarn();
  }

  function deptLabelById() {
    var labels = {};
    (state.securityGroups || []).forEach(function(g) {
      if (!g || !g.id) return;
      labels[String(g.id)] = g.path || g.name || g.id;
    });
    return labels;
  }

  function selectedChipsHost() {
    var host = byID('tenantCloudWorkspaceSelectedChips');
    if (host) return host;
    var summary = byID('tenantCloudWorkspaceSelectedDepartments');
    if (!summary || !summary.parentNode) return null;
    host = global.document.createElement('div');
    host.id = 'tenantCloudWorkspaceSelectedChips';
    host.className = 'cws-acl-chips';
    host.setAttribute('aria-live', 'polite');
    summary.parentNode.insertBefore(host, summary.nextSibling);
    return host;
  }

  function renderSelectedChips() {
    var host = selectedChipsHost();
    if (!host) return;
    var ids = state.selectedDepts || [];
    if (!ids.length || currentMode() !== 'departments') {
      host.innerHTML = '';
      host.style.display = 'none';
      return;
    }
    var labels = deptLabelById();
    host.style.display = '';
    host.setAttribute('aria-label', cwsx('selectedChipsAria'));
    host.innerHTML = ids.map(function(id) {
      var label = labels[id] || id;
      return '<button type="button" class="cws-acl-chip" data-dept-id="' + escapeHtml(id) + '"'
        + ' title="' + escapeHtml(label) + '">'
        + '<span>' + escapeHtml(label) + '</span>'
        + '<span aria-hidden="true">\u00d7</span></button>';
    }).join('');
    host.querySelectorAll('.cws-acl-chip').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var id = String(btn.getAttribute('data-dept-id') || '');
        cardQueryAll('.cws-acl-tree-dept').forEach(function(cb) {
          if (String(cb.value || '') === id) cb.checked = false;
        });
        state.selectedDepts = (state.selectedDepts || []).filter(function(x) { return x !== id; });
        collectSelectedFromDom();
        updateEmptyWarn();
      });
    });
  }

  function updateSelectionSummary(count) {
    var summary = byID('tenantCloudWorkspaceSelectedDepartments');
    if (count == null) count = collectSelectedFromDom().length;
    if (summary) summary.textContent = cwsx('selectedDepartments', { n: String(count) });
    renderSelectedChips();
  }

  function updateEmptyWarn() {
    var warn = byID('tenantCloudWorkspaceEmptyWarn');
    var save = byID('tenantCloudWorkspaceSettingsSaveBtn');
    var restricted = currentMode() === 'departments';
    var selected = collectSelectedFromDom().length;
    updateSelectionSummary(selected);
    var empty = restricted && !selected;
    if (warn) {
      warn.textContent = cwsx('emptyDepartmentsWarn');
      warn.style.display = empty ? '' : 'none';
    }
    if (save) save.disabled = !!(state.saving || empty);
  }

  function applyDeptFilter() {
    var filterEl = byID('tenantCloudWorkspaceDeptFilter');
    var q = String(filterEl && filterEl.value || '').trim().toLowerCase();
    state.deptFilter = filterEl ? (filterEl.value || '') : '';
    var branches = cardQueryAll('.cws-acl-tree-branch');
    for (var i = branches.length - 1; i >= 0; i -= 1) {
      var branch = branches[i];
      var row = branch.firstElementChild;
      var ownMatch = !q || String(row && row.getAttribute('data-filter') || '').toLowerCase().indexOf(q) >= 0;
      var childMatch = Array.prototype.slice.call(branch.children || []).some(function(child) {
        return child.classList && child.classList.contains('cws-acl-tree-children')
          && Array.prototype.slice.call(child.children || []).some(function(grandchild) {
            return grandchild.style.display !== 'none';
          });
      });
      branch.style.display = ownMatch || childMatch ? '' : 'none';
    }
  }

  function renderDeptCheckbox(id, label, checked, extraCls, filterText, depth) {
    var accessibleLabel = String(filterText || label || id || '').trim();
    return '<label class="cws-acl-tree-dept-row' + (extraCls ? ' ' + extraCls : '') + '"'
      + ' data-filter="' + escapeHtml(accessibleLabel + ' ' + String(id || '')) + '"'
      + ' style="--cws-acl-tree-depth:' + String(depth || 0) + ';display:flex;align-items:center;gap:8px;padding:7px 8px;cursor:pointer;margin:0;font-size:12px">'
      + '<input type="checkbox" class="cws-acl-tree-dept" value="' + escapeHtml(id) + '"'
      + ' aria-label="' + escapeHtml(accessibleLabel) + '" title="' + escapeHtml(accessibleLabel) + '"'
      + (checked ? ' checked' : '') + '>'
      + '<span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + escapeHtml(label) + '</span></label>';
  }

  function renderDepartmentTree(nodes, selectedDepts, depth, parentPath) {
    var currentDepth = Number(depth) || 0;
    if (currentDepth > cwsMaxDepartmentTreeDepth) return '';
    return (Array.isArray(nodes) ? nodes : []).map(function(node) {
      var id = String(node && node.id || '').trim();
      var name = String(node && node.name || '').trim();
      var label = name || id;
      var path = parentPath ? (parentPath + ' / ' + label) : label;
      var children = Array.isArray(node && node.children) ? node.children : [];
      var childHtml = renderDepartmentTree(children, selectedDepts, currentDepth + 1, path);
      if (!id) return childHtml;
      return '<div class="cws-acl-tree-branch">'
        + renderDeptCheckbox(id, label, isDeptSelected(selectedDepts, id),
          children.length ? 'has-children' : '', path, currentDepth)
        + (childHtml ? '<div class="cws-acl-tree-children">' + childHtml + '</div>' : '')
        + '</div>';
    }).join('');
  }

  function renderCwsAclPanel() {
    var host = byID('tenantCloudWorkspaceDeptTree');
    if (!host) return;
    var selectedDepts = state.selectedDepts || [];
    var filterVal = state.deptFilter || '';
    var deptsHtml;
    if (state.securityGroupsLoading && !state.securityGroupsLoaded) {
      deptsHtml = '<div class="item-meta">' + escapeHtml(cwsx('departmentsLoading')) + '</div>';
    } else if (state.securityGroupsError) {
      deptsHtml = '<div class="item-meta" style="color:var(--danger,#b91c1c)">'
        + escapeHtml(cwsx('groupsFailed', { error: state.securityGroupsError }))
        + '</div>'
        + '<div class="actions" style="margin-top:6px">'
        + actionButton('tenantCloudWorkspaceReloadGroupsBtn', cwsx('reloadGroups'), 'ghost')
        + '</div>';
      var orphansWhileErr = uniqueStrings(selectedDepts);
      if (orphansWhileErr.length) {
        deptsHtml += '<div style="max-height:160px;overflow:auto;border:1px solid rgba(31,34,48,.08);border-radius:8px;padding:8px 10px;margin-top:8px">'
          + orphansWhileErr.map(function(id) {
            return renderDeptCheckbox(id, cwsx('deptUnknown', { id: id }), true, 'is-unknown');
          }).join('')
          + '</div>';
      }
    } else {
      var knownRows = renderDepartmentTree(state.securityGroupTree, selectedDepts, 0, '');
      var unknown = unknownSelectedDepartments(selectedDepts);
      var unknownRows = unknown.map(function(id) {
        return renderDeptCheckbox(id, cwsx('deptUnknown', { id: id }), true, 'is-unknown', id, 0);
      });
      if (!knownRows && !unknownRows.length) {
        deptsHtml = '<div class="item-meta">' + escapeHtml(cwsx('departmentsEmpty')) + '</div>'
          + '<div class="actions" style="margin-top:6px">'
          + actionButton('tenantCloudWorkspaceReloadGroupsBtn', cwsx('reloadGroups'), 'ghost')
          + '</div>';
      } else {
        deptsHtml = '<div class="cws-acl-tree-toolbar">'
          + '<input id="tenantCloudWorkspaceDeptFilter" type="search" value="' + escapeHtml(filterVal) + '"'
          + ' placeholder="' + escapeHtml(cwsx('deptFilter')) + '"'
          + ' aria-label="' + escapeHtml(cwsx('deptFilter')) + '"'
          + '>'
          + actionButton('tenantCloudWorkspaceSelectVisibleBtn', cwsx('selectVisible'), 'ghost')
          + actionButton('tenantCloudWorkspaceClearDepartmentsBtn', cwsx('clearDepartments'), 'ghost')
          + '</div>'
          + '<div class="cws-acl-tree" role="group" aria-label="' + escapeHtml(cwsx('departmentsLabel')) + '">'
          + unknownRows.join('')
          + knownRows
          + '</div>';
      }
    }
    host.innerHTML = deptsHtml;
    wireTreeHandlers();
    applyDeptFilter();
    updateEmptyWarn();
  }

  function wireTreeHandlers() {
    cardQueryAll('.cws-acl-tree-dept').forEach(function(cb) {
      cb.addEventListener('change', function() {
        collectSelectedFromDom();
        updateEmptyWarn();
      });
    });
    var deptFilter = byID('tenantCloudWorkspaceDeptFilter');
    if (deptFilter) {
      deptFilter.addEventListener('input', applyDeptFilter);
    }
    var selectVisible = byID('tenantCloudWorkspaceSelectVisibleBtn');
    if (selectVisible) {
      selectVisible.addEventListener('click', function() {
        cardQueryAll('.cws-acl-tree-branch').forEach(function(branch) {
          if (branch.style.display === 'none') return;
          var cb = branch.querySelector('.cws-acl-tree-dept');
          if (cb) cb.checked = true;
        });
        collectSelectedFromDom();
        updateEmptyWarn();
      });
    }
    var clearDepartments = byID('tenantCloudWorkspaceClearDepartmentsBtn');
    if (clearDepartments) {
      clearDepartments.addEventListener('click', function() {
        cardQueryAll('.cws-acl-tree-dept').forEach(function(cb) { cb.checked = false; });
        collectSelectedFromDom();
        updateEmptyWarn();
      });
    }
    var reloadGroups = byID('tenantCloudWorkspaceReloadGroupsBtn');
    if (reloadGroups) {
      reloadGroups.addEventListener('click', function() {
        collectSelectedFromDom();
        state.securityGroupsLoaded = false;
        loadSecurityGroups({ force: true, renderTree: true });
      });
    }
  }

  function clampInt(value, fallback, min, max) {
    var n = Number(value);
    if (!Number.isFinite(n)) return fallback;
    n = Math.round(n);
    if (n < min) return min;
    if (n > max) return max;
    return n;
  }

  function bytesToMiB(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n) || n <= 0) return CWS_MAX_MIB_DEFAULT;
    return Math.round(n / MIB);
  }

  function bytesToGiB(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n) || n <= 0) return CWS_TENANT_GIB_DEFAULT;
    return Math.round(n / GIB);
  }

  function readNumberInput(id, fallback) {
    var el = byID(id);
    var n = Number(el ? el.value : fallback);
    if (!Number.isFinite(n)) return NaN;
    return Math.round(n);
  }

  function applyPreview(preview) {
    var data = preview || {};
    state.preview = {
      department_count: Number(data.department_count || 0) || 0,
      user_count: Number(data.user_count || 0) || 0,
      over_quota_users: Array.isArray(data.over_quota_users) ? data.over_quota_users : []
    };
    var previewEl = byID('tenantCloudWorkspacePreview');
    if (previewEl) {
      previewEl.textContent = cwsx('preview', {
        n: String(state.preview.department_count),
        m: String(state.preview.user_count)
      });
    }
    var over = byID('tenantCloudWorkspaceOverQuota');
    if (over) {
      var users = state.preview.over_quota_users.map(function(item) {
        if (item && typeof item === 'object') return String(item.sn || '').trim();
        return String(item || '').trim();
      }).filter(Boolean);
      if (!users.length) {
        over.style.display = 'none';
        over.textContent = '';
      } else {
        over.style.display = '';
        over.textContent = cwsx('overQuota', {
          k: String(users.length),
          sns: users.join(', ')
        });
      }
    }
  }

  function applySettings(data) {
    var settings = data || {};
    setMode(String(settings.mode || 'off'));
    var quotaEl = byID('tenantCloudWorkspaceQuota');
    if (quotaEl) {
      quotaEl.min = String(CWS_QUOTA_MIN);
      quotaEl.max = String(CWS_QUOTA_MAX);
      quotaEl.value = String(clampInt(settings.quota, CWS_QUOTA_DEFAULT, CWS_QUOTA_MIN, CWS_QUOTA_MAX));
    }
    var maxEl = byID('tenantCloudWorkspaceMaxMiB');
    if (maxEl) {
      maxEl.min = String(CWS_MAX_MIB_MIN);
      maxEl.max = String(CWS_MAX_MIB_MAX);
      maxEl.value = String(clampInt(bytesToMiB(settings.max_workspace_bytes), CWS_MAX_MIB_DEFAULT, CWS_MAX_MIB_MIN, CWS_MAX_MIB_MAX));
    }
    var totalEl = byID('tenantCloudWorkspaceTenantGiB');
    if (totalEl) {
      totalEl.min = String(CWS_TENANT_GIB_MIN);
      totalEl.max = String(CWS_TENANT_GIB_MAX);
      totalEl.value = String(clampInt(bytesToGiB(settings.tenant_max_total_bytes), CWS_TENANT_GIB_DEFAULT, CWS_TENANT_GIB_MIN, CWS_TENANT_GIB_MAX));
    }
    var ids = Array.isArray(settings.department_ids) ? settings.department_ids : [];
    state.selectedDepts = uniqueStrings(ids.map(function(id) { return String(id || '').trim(); }).filter(Boolean));
    applyPreview(settings.preview);
  }

  function applyTenantCloudWorkspaceSettingsI18n() {
    setText('tenantCloudWorkspaceSettingsTitle', cwsx('title'));
    setText('tenantCloudWorkspaceSettingsDesc', cwsx('desc'));
    setText('tenantCloudWorkspaceSettingsReloadBtn', cwsx('reload'));
    setText('tenantCloudWorkspaceSettingsSaveBtn', state.saving ? cwsx('saving') : cwsx('save'));
    setText('tenantCloudWorkspaceModeLabel', cwsx('modeLabel'));
    setText('tenantCloudWorkspaceModeOffLabel', cwsx('modeOff'));
    setText('tenantCloudWorkspaceModeAllUsersLabel', cwsx('modeAllUsers'));
    setText('tenantCloudWorkspaceModeDepartmentsLabel', cwsx('modeDepartments'));
    setText('tenantCloudWorkspaceDepartmentsLabel', cwsx('departmentsLabel'));
    setText('tenantCloudWorkspaceDepartmentsHint', cwsx('departmentsHint'));
    setText('tenantCloudWorkspaceQuotaLabel', cwsx('quotaLabel'));
    setText('tenantCloudWorkspaceMaxMiBLabel', cwsx('maxMiBLabel'));
    setText('tenantCloudWorkspaceTenantGiBLabel', cwsx('tenantGiBLabel'));
    setText('tenantCloudWorkspaceSettingsHint', cwsx('hint'));
    setText('tenantCloudWorkspaceEmptyWarn', cwsx('emptyDepartmentsWarn'));
    applyPreview(state.preview);
    updateSelectionSummary(state.selectedDepts.length);
    var filterEl = byID('tenantCloudWorkspaceDeptFilter');
    if (filterEl) {
      filterEl.placeholder = cwsx('deptFilter');
      filterEl.setAttribute('aria-label', cwsx('deptFilter'));
    }
  }

  function tenantCloudWorkspaceModeChanged() {
    updateDepartmentsVisibility();
    if (currentMode() === 'departments' && !state.securityGroupsLoaded && !state.securityGroupsLoading) {
      loadSecurityGroups({ renderTree: true });
    }
  }

  async function loadTenantCloudWorkspaceSettings() {
    if (!canManageTenantCloudWorkspace()) return null;
    applyTenantCloudWorkspaceSettingsI18n();
    try {
      var data = await api('/api/admin/cloud-workspaces/settings');
      applySettings(data || {});
      applyTenantCloudWorkspaceSettingsI18n();
      if (currentMode() === 'departments' || (state.selectedDepts && state.selectedDepts.length)) {
        await loadSecurityGroups({ renderTree: true });
      } else {
        renderCwsAclPanel();
      }
      return data || {};
    } catch (err) {
      var msg = cwsx('loadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  }

  async function saveTenantCloudWorkspaceSettings() {
    if (!canManageTenantCloudWorkspace()) return null;
    var mode = currentMode();
    var quota = readNumberInput('tenantCloudWorkspaceQuota', CWS_QUOTA_DEFAULT);
    var maxMiB = readNumberInput('tenantCloudWorkspaceMaxMiB', CWS_MAX_MIB_DEFAULT);
    var tenantGiB = readNumberInput('tenantCloudWorkspaceTenantGiB', CWS_TENANT_GIB_DEFAULT);
    if (!Number.isFinite(quota) || quota < CWS_QUOTA_MIN || quota > CWS_QUOTA_MAX) {
      var qMsg = cwsx('invalidQuota');
      setOutput(qMsg);
      showToast(qMsg, 'error');
      return;
    }
    if (!Number.isFinite(maxMiB) || maxMiB < CWS_MAX_MIB_MIN || maxMiB > CWS_MAX_MIB_MAX) {
      var mMsg = cwsx('invalidMaxMiB');
      setOutput(mMsg);
      showToast(mMsg, 'error');
      return;
    }
    if (!Number.isFinite(tenantGiB) || tenantGiB < CWS_TENANT_GIB_MIN || tenantGiB > CWS_TENANT_GIB_MAX) {
      var tMsg = cwsx('invalidTenantGiB');
      setOutput(tMsg);
      showToast(tMsg, 'error');
      return;
    }
    var departmentIds = collectSelectedFromDom();
    if (mode === 'departments' && !departmentIds.length) {
      var emptyMsg = cwsx('emptyDepartmentsWarn');
      setOutput(emptyMsg);
      showToast(emptyMsg, 'error');
      updateEmptyWarn();
      return;
    }
    var btn = byID('tenantCloudWorkspaceSettingsSaveBtn');
    var previousLabel = btn ? btn.textContent : '';
    state.saving = true;
    if (btn) {
      btn.disabled = true;
      btn.textContent = cwsx('saving');
    }
    try {
      var data = await api('/api/admin/cloud-workspaces/settings', {
        method: 'PUT',
        body: JSON.stringify({
          mode: mode,
          quota: quota,
          department_ids: departmentIds,
          max_workspace_bytes: maxMiB * MIB,
          tenant_max_total_bytes: tenantGiB * GIB
        })
      });
      applySettings(data || {});
      applyTenantCloudWorkspaceSettingsI18n();
      renderCwsAclPanel();
      var msg = cwsx('saved');
      setOutput(msg);
      showToast(msg, 'success');
      return data || {};
    } catch (err) {
      var failMsg = cwsx('saveFailed', { error: err.message });
      setOutput(failMsg);
      showToast(failMsg, 'error');
      throw err;
    } finally {
      state.saving = false;
      if (btn) {
        btn.disabled = false;
        btn.textContent = previousLabel || cwsx('save');
      }
      updateEmptyWarn();
    }
  }

  function applyLanguage() {
    var filterEl = byID('tenantCloudWorkspaceDeptFilter');
    if (filterEl) state.deptFilter = filterEl.value || '';
    collectSelectedFromDom();
    applyTenantCloudWorkspaceSettingsI18n();
    if (byID('tenantCloudWorkspaceDeptTree')) renderCwsAclPanel();
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(applyLanguage);
  }

  applyTenantCloudWorkspaceSettingsI18n();

  global.loadTenantCloudWorkspaceSettings = loadTenantCloudWorkspaceSettings;
  global.saveTenantCloudWorkspaceSettings = saveTenantCloudWorkspaceSettings;
  global.tenantCloudWorkspaceModeChanged = tenantCloudWorkspaceModeChanged;
  global.applyTenantCloudWorkspaceSettingsI18n = applyTenantCloudWorkspaceSettingsI18n;
})(window);
