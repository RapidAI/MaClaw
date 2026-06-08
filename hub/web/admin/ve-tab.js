/*
 * Digital Employee admin module.
 * ASCII only - all Chinese text uses \uXXXX escape sequences.
 *
 * Tasks covered:
 * 7.1 - Add "Digital Employee" Tab to Hub Admin Panel
 * 7.2 - Pending/Active list views with name/skill_description/access_policy/online_status/registered_at
 * 7.3 - Approve/Reject/Disable action buttons calling Admin API
 * 7.4 - Group chat participant limit config (1-10, default 5), push ve:group_config event on change
 */
(function(global) {
  'use strict';

  // --- i18n ---
  var VE_I18N = {
    en: {
      tabTitle: 'Digital Employees',
      tabSubtitle: 'Manage digital employee registrations and group chat settings.',
      navTitle: 'Digital Employees',
      navDesc: 'Approval, status, and group config',
      panelTitle: 'Digital Employee Management',
      panelDesc: 'Review pending registrations, manage active digital employees, and configure group chat limits.',
      pendingTitle: 'Pending Approval',
      activeTitle: 'Active Employees',
      deletedTitle: 'Deleted Employees (History Retained)',
      inactiveTitle: 'Disabled Employees',
      emptyPending: 'No pending registration requests.',
      emptyActive: 'No active digital employees.',
      emptyDeleted: 'No deleted digital employees with retained history.',
      emptyInactive: 'No disabled or stale digital employees.',
      name: 'Name',
      employeeType: 'Employee Type',
      employeeTypeVirtual: 'Virtual Employee',
      employeeTypePhysical: 'Physical Employee',
      skillDesc: 'Skill Description',
      accessPolicy: 'Access Policy',
      visibleDepartments: 'Visible Departments',
      residentEmployee: 'Resident',
      setResident: 'Set Resident',
      clearResident: 'Clear Resident',
      residentSaved: 'Resident digital employee saved.',
      residentSaveFailed: 'Save resident digital employee failed: {error}',
      globalVisible: 'Global',
      editVisibility: 'Visibility',
      saveVisibility: 'Save Visibility',
      visibilitySaved: 'Visible departments saved.',
      visibilitySaveFailed: 'Save visible departments failed: {error}',
      noDepartments: 'No departments available.',
      departmentTreeHint: 'Select one or more departments from the organization tree. Leave empty for global visibility.',
      onlineStatus: 'Online Status',
      registeredAt: 'Registered At',
      actions: 'Actions',
      approve: 'Approve',
      reject: 'Reject',
      disable: 'Disable',
      purge: 'Clear',
      forcePurge: 'Force Delete',
      forcePurgePasswordPrompt: 'Enter administrator password to permanently delete this employee and related history:',
      online: 'Online',
      offline: 'Offline',
      policyPublic: 'Public',
      policyWhitelist: 'Whitelist',
      policyBlacklist: 'Blacklist',
      policyPerRequest: 'Per-Request',
      autoApproveTitle: 'Auto Approval',
      autoApproveDesc: 'When enabled, new digital employee applications are approved automatically while quota is available.',
      autoApproveLabel: 'Automatically approve applications',
      autoApproveEnabled: 'Enabled',
      autoApproveDisabled: 'Disabled',
      groupConfigTitle: 'Group Chat Configuration',
      groupConfigDesc: 'Maximum number of digital employee participants in a single group chat.',
      maxParticipants: 'Max Participants',
      saveConfig: 'Save',
      configSaved: 'Group chat configuration saved.',
      configSaveFailed: 'Save config failed: {error}',
      approveSuccess: 'Digital employee approved.',
      approveFailed: 'Approve failed: {error}',
      rejectSuccess: 'Digital employee rejected.',
      rejectFailed: 'Reject failed: {error}',
      disableSuccess: 'Digital employee disabled.',
      disableFailed: 'Disable failed: {error}',
      purgeSuccess: 'Digital employee record cleared from Hub.',
      purgeFailed: 'Clear failed: {error}',
      forcePurgeSuccess: 'Digital employee and related history deleted.',
      forcePurgeFailed: 'Force delete failed: {error}',
      loadFailed: 'Load digital employees failed: {error}',
      refresh: 'Refresh',
      quotaInfo: 'Active: {active} / Quota: {quota}',
      status: 'Status',
      ownerEmail: 'Owner Email',
      historyTitle: 'History Sessions',
      historyDesc: 'Search by digital employee name or owner email, then preview related group discussions.',
      historySearchPlaceholder: 'Name or owner email',
      searchHistory: 'Search Sessions',
      openHistory: 'History',
      preview: 'Preview',
      historyEmpty: 'No related history sessions.',
      historyLoadFailed: 'Load history failed: {error}',
      previewLoadFailed: 'Load preview failed: {error}',
      messages: 'Messages',
      participants: 'Participants',
      counterpartEmail: 'Counterpart Email',
      close: 'Close',
      loading: 'Loading...',
      searchNoMatch: 'No matching digital employees.',
      searchMultiple: '{count} digital employees matched. Histories are merged below.',
      selectedEmployee: 'Selected: {name}',
      attachments: 'Attachments',
      resultSummary: 'Result',
      createdAt: 'Created At',
      updatedAt: 'Updated At'
    },
    zh: {
      tabTitle: '\u6570\u5b57\u5458\u5de5',
      tabSubtitle: '\u7ba1\u7406\u6570\u5b57\u5458\u5de5\u6ce8\u518c\u548c\u7fa4\u804a\u8bbe\u7f6e\u3002',
      navTitle: '\u6570\u5b57\u5458\u5de5',
      navDesc: '\u5ba1\u6279\u3001\u72b6\u6001\u4e0e\u7fa4\u804a\u914d\u7f6e',
      panelTitle: '\u6570\u5b57\u5458\u5de5\u7ba1\u7406',
      panelDesc: '\u5ba1\u6838\u5f85\u5ba1\u6279\u6ce8\u518c\u3001\u7ba1\u7406\u5df2\u6fc0\u6d3b\u6570\u5b57\u5458\u5de5\u3001\u914d\u7f6e\u7fa4\u804a\u4e0a\u9650\u3002',
      pendingTitle: '\u5f85\u5ba1\u6279',
      activeTitle: '\u5df2\u6fc0\u6d3b',
      deletedTitle: '\u5df2\u5220\u9664\uff08\u4fdd\u7559\u5386\u53f2\uff09',
      inactiveTitle: '\u5df2\u505c\u7528',
      emptyPending: '\u6682\u65e0\u5f85\u5ba1\u6279\u7684\u6ce8\u518c\u8bf7\u6c42\u3002',
      emptyActive: '\u6682\u65e0\u5df2\u6fc0\u6d3b\u7684\u6570\u5b57\u5458\u5de5\u3002',
      emptyDeleted: '\u6682\u65e0\u5df2\u5220\u9664\u4e14\u4fdd\u7559\u5386\u53f2\u7684\u6570\u5b57\u5458\u5de5\u3002',
      emptyInactive: '\u6682\u65e0\u5df2\u505c\u7528\u6216\u6b8b\u7559\u7684\u6570\u5b57\u5458\u5de5\u3002',
      name: '\u540d\u79f0',
      employeeType: '\u6570\u5b57\u5458\u5de5\u7c7b\u578b',
      employeeTypeVirtual: '\u865a\u62df\u5458\u5de5',
      employeeTypePhysical: '\u7269\u7406\u5458\u5de5',
      skillDesc: '\u6280\u80fd\u63cf\u8ff0',
      accessPolicy: '\u8bbf\u95ee\u7b56\u7565',
      visibleDepartments: '\u53ef\u89c1\u90e8\u95e8',
      residentEmployee: '\u5e38\u9a7b',
      setResident: '\u8bbe\u7f6e\u5e38\u9a7b',
      clearResident: '\u53d6\u6d88\u5e38\u9a7b',
      residentSaved: '\u5e38\u9a7b\u6570\u5b57\u5458\u5de5\u5df2\u4fdd\u5b58\u3002',
      residentSaveFailed: '\u4fdd\u5b58\u5e38\u9a7b\u6570\u5b57\u5458\u5de5\u5931\u8d25\uff1a{error}',
      globalVisible: '\u5168\u5c40\u53ef\u89c1',
      editVisibility: '\u53ef\u89c1\u90e8\u95e8',
      saveVisibility: '\u4fdd\u5b58\u53ef\u89c1\u90e8\u95e8',
      visibilitySaved: '\u53ef\u89c1\u90e8\u95e8\u5df2\u4fdd\u5b58\u3002',
      visibilitySaveFailed: '\u4fdd\u5b58\u53ef\u89c1\u90e8\u95e8\u5931\u8d25\uff1a{error}',
      noDepartments: '\u6682\u65e0\u53ef\u7528\u90e8\u95e8\u3002',
      departmentTreeHint: '\u4ece\u7ec4\u7ec7\u673a\u6784\u6811\u4e2d\u9009\u62e9\u4e00\u4e2a\u6216\u591a\u4e2a\u90e8\u95e8\uff0c\u4e0d\u9009\u8868\u793a\u5168\u5c40\u53ef\u89c1\u3002',
      onlineStatus: '\u5728\u7ebf\u72b6\u6001',
      registeredAt: '\u6ce8\u518c\u65f6\u95f4',
      actions: '\u64cd\u4f5c',
      approve: '\u901a\u8fc7',
      reject: '\u62d2\u7edd',
      disable: '\u7981\u7528',
      purge: '\u6e05\u9664',
      forcePurge: '\u5f3a\u5236\u5220\u9664',
      forcePurgePasswordPrompt: '\u8f93\u5165\u7ba1\u7406\u5458\u5bc6\u7801\uff0c\u6c38\u4e45\u5220\u9664\u8be5\u6570\u5b57\u5458\u5de5\u53ca\u76f8\u5173\u5386\u53f2\uff1a',
      online: '\u5728\u7ebf',
      offline: '\u79bb\u7ebf',
      policyPublic: '\u516c\u5f00',
      policyWhitelist: '\u767d\u540d\u5355',
      policyBlacklist: '\u9ed1\u540d\u5355',
      policyPerRequest: '\u9010\u6b21\u6388\u6743',
      autoApproveTitle: '\u81ea\u52a8\u901a\u8fc7',
      autoApproveDesc: '\u6253\u5f00\u540e\uff0c\u5728\u914d\u989d\u5141\u8bb8\u65f6\u65b0\u7684\u6570\u5b57\u5458\u5de5\u7533\u8bf7\u5c06\u81ea\u52a8\u901a\u8fc7\u3002',
      autoApproveLabel: '\u81ea\u52a8\u901a\u8fc7\u6570\u5b57\u5458\u5de5\u7533\u8bf7',
      autoApproveEnabled: '\u5df2\u5f00\u542f',
      autoApproveDisabled: '\u5df2\u5173\u95ed',
      groupConfigTitle: '\u7fa4\u804a\u914d\u7f6e',
      groupConfigDesc: '\u5355\u4e2a\u7fa4\u804a\u4e2d\u6570\u5b57\u5458\u5de5\u53c2\u4e0e\u8005\u7684\u6700\u5927\u6570\u91cf\u3002',
      maxParticipants: '\u6700\u5927\u53c2\u4e0e\u8005',
      saveConfig: '\u4fdd\u5b58',
      configSaved: '\u7fa4\u804a\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',
      configSaveFailed: '\u4fdd\u5b58\u914d\u7f6e\u5931\u8d25\uff1a{error}',
      approveSuccess: '\u6570\u5b57\u5458\u5de5\u5df2\u901a\u8fc7\u5ba1\u6279\u3002',
      approveFailed: '\u5ba1\u6279\u5931\u8d25\uff1a{error}',
      rejectSuccess: '\u6570\u5b57\u5458\u5de5\u5df2\u62d2\u7edd\u3002',
      rejectFailed: '\u62d2\u7edd\u5931\u8d25\uff1a{error}',
      disableSuccess: '\u6570\u5b57\u5458\u5de5\u5df2\u7981\u7528\u3002',
      disableFailed: '\u7981\u7528\u5931\u8d25\uff1a{error}',
      purgeSuccess: '\u6570\u5b57\u5458\u5de5\u8bb0\u5f55\u5df2\u4ece Hub \u6e05\u9664\u3002',
      purgeFailed: '\u6e05\u9664\u5931\u8d25\uff1a{error}',
      forcePurgeSuccess: '\u6570\u5b57\u5458\u5de5\u53ca\u76f8\u5173\u5386\u53f2\u5df2\u5220\u9664\u3002',
      forcePurgeFailed: '\u5f3a\u5236\u5220\u9664\u5931\u8d25\uff1a{error}',
      loadFailed: '\u52a0\u8f7d\u6570\u5b57\u5458\u5de5\u5217\u8868\u5931\u8d25\uff1a{error}',
      refresh: '\u5237\u65b0',
      quotaInfo: '\u5df2\u6fc0\u6d3b\uff1a{active} / \u914d\u989d\uff1a{quota}',
      status: '\u72b6\u6001',
      ownerEmail: '\u6240\u5c5e\u90ae\u7bb1',
      historyTitle: '\u5386\u53f2\u4f1a\u8bdd',
      historyDesc: '\u6309\u6570\u5b57\u5458\u5de5\u540d\u79f0\u6216\u6240\u5c5e\u90ae\u7bb1\u641c\u7d22\uff0c\u5e76\u9884\u89c8\u76f8\u5173\u7fa4\u804a\u8ba8\u8bba\u3002',
      historySearchPlaceholder: '\u540d\u79f0\u6216\u90ae\u7bb1',
      searchHistory: '\u67e5\u8be2\u4f1a\u8bdd',
      openHistory: '\u5386\u53f2',
      preview: '\u9884\u89c8',
      historyEmpty: '\u6682\u65e0\u76f8\u5173\u5386\u53f2\u4f1a\u8bdd\u3002',
      historyLoadFailed: '\u52a0\u8f7d\u5386\u53f2\u5931\u8d25\uff1a{error}',
      previewLoadFailed: '\u52a0\u8f7d\u9884\u89c8\u5931\u8d25\uff1a{error}',
      messages: '\u6d88\u606f',
      participants: '\u53c2\u4e0e\u8005',
      counterpartEmail: '\u4ea4\u8c08\u5bf9\u65b9\u90ae\u7bb1',
      close: '\u5173\u95ed',
      loading: '\u52a0\u8f7d\u4e2d...',
      searchNoMatch: '\u672a\u627e\u5230\u5339\u914d\u7684\u6570\u5b57\u5458\u5de5\u3002',
      searchMultiple: '\u5df2\u5339\u914d {count} \u4e2a\u6570\u5b57\u5458\u5de5\uff0c\u4e0b\u65b9\u5408\u5e76\u663e\u793a\u4f1a\u8bdd\u3002',
      selectedEmployee: '\u5df2\u9009\u62e9\uff1a{name}',
      attachments: '\u9644\u4ef6',
      resultSummary: '\u7ed3\u679c',
      createdAt: '\u521b\u5efa\u65f6\u95f4',
      updatedAt: '\u66f4\u65b0\u65f6\u95f4'
    }
  };

  function vt(key) {
    var lang = global.currentLang || 'zh';
    var dict = VE_I18N[lang] || VE_I18N['zh'];
    return dict[key] || key;
  }

  // --- State ---
  var veListCache = [];
  var veGroupConfig = { max_group_participants: 5, auto_approve: false };
  var veGroupConfigLoaded = false;
  var veQuota = 0;
  var veActiveCount = 0;
  var veHistoryEmployeeID = '';
  var veHistoryEmployeeLabel = '';
  var veHistoryDiscussions = [];
  var veHistoryLoading = false;
  var veHistoryHint = '';
  var veSecurityGroupsLoaded = false;
  var veSecurityGroupsLoadPromise = null;
  var veSecurityGroupTree = null;
  var veSecurityGroupMap = Object.create(null);

  // --- Helpers ---
  function formatAccessPolicy(policy) {
    switch (policy) {
      case 'public': return vt('policyPublic');
      case 'whitelist': return vt('policyWhitelist');
      case 'blacklist': return vt('policyBlacklist');
      case 'per_request': return vt('policyPerRequest');
      default: return policy || '';
    }
  }

  function inferEmployeeType(ve) {
    if (ve && (String(ve.platform_id || '').trim() || String(ve.platform_employee_id || '').trim() || String(ve.runtime_provider_id || '').trim().toLowerCase() === 'maclawsrv')) return 'virtual';
    var explicit = String((ve && ve.employee_type) || '').trim().toLowerCase();
    if (explicit === 'virtual' || explicit === 'physical') return explicit;
    return 'physical';
  }

  function formatEmployeeType(ve) {
    var type = inferEmployeeType(ve);
    var label = type === 'virtual' ? vt('employeeTypeVirtual') : vt('employeeTypePhysical');
    var cls = type === 'virtual' ? 'badge info' : 'badge warn';
    return '<span class="' + cls + '" title="' + escapeHtml(vt('employeeType')) + '">' + escapeHtml(label) + '</span>';
  }

  function formatOnlineStatus(status) {
    if (status === 'online') return '<span class="badge ok">' + vt('online') + '</span>';
    return '<span class="badge warn">' + vt('offline') + '</span>';
  }

  function formatDate(dateStr) {
    if (!dateStr) return '-';
    try {
      var d = new Date(dateStr);
      if (isNaN(d.getTime())) return dateStr;
      return d.toLocaleString();
    } catch (e) {
      return dateStr;
    }
  }

  function truncate(str, maxLen) {
    if (!str) return '';
    if (str.length <= maxLen) return str;
    return str.substring(0, maxLen) + '...';
  }

  function jsString(value) {
    return JSON.stringify(String(value || '')).replace(/</g, '\\u003c');
  }

  function jsAttrString(value) {
    return escapeHtml(jsString(value));
  }
  function discussionTitle(discussion) {
    return discussion.topic || discussion.question || discussion.id || '';
  }

  function compactMessageText(value, maxLen) {
    return truncate(String(value || '').replace(/\s+/g, ' ').trim(), maxLen);
  }

  function historyCounterpartText(discussion) {
    var emails = Array.isArray(discussion && discussion.counterpart_emails) ? discussion.counterpart_emails : [];
    return emails.length ? emails.join(', ') : '-';
  }

  function veEmployeeLabel(ve) {
    if (!ve) return '';
    var owner = ve.owner_email || ve.owner_user_id || '';
    return (ve.name || ve.id || '') + (owner ? ' / ' + owner : '');
  }

  function veInitials(ve) {
    var name = String((ve && (ve.name || ve.id)) || '').trim();
    if (!name) return '?';
    return (Array.from(name)[0] || '?').toUpperCase();
  }

  function renderVEAvatar(ve) {
    var avatar = String((ve && ve.avatar_data_url) || '').trim();
    var fallback = '<span class="ve-avatar-fallback">' + escapeHtml(veInitials(ve)) + '</span>';
    if (!avatar) return '<span class="ve-avatar" aria-hidden="true">' + fallback + '</span>';
    return '<span class="ve-avatar" aria-hidden="true">' +
      '<img src="' + escapeHtml(avatar) + '" alt="" loading="lazy" decoding="async" onerror="this.hidden=true;this.nextElementSibling.hidden=false">' +
      '<span class="ve-avatar-fallback" hidden>' + escapeHtml(veInitials(ve)) + '</span>' +
      '</span>';
  }

  function veListGridTemplate() {
    return 'var(--ve-list-grid,minmax(150px,.95fr) minmax(92px,.65fr) minmax(150px,1fr) minmax(150px,.9fr) minmax(76px,.55fr) minmax(108px,.72fr) minmax(64px,.5fr) minmax(74px,.55fr) minmax(122px,.75fr) minmax(260px,1.45fr))';
  }

  function flattenSecurityGroupTree(node, depth, out, options) {
    out = out || [];
    options = options || {};
    if (!node) return out;
    var id = String(node.id || '').trim();
    var skipRoot = options.skipRoot && !depth;
    if (id && !skipRoot) out.push({ id: id, name: String(node.name || id), depth: depth || 0 });
    (node.children || []).forEach(function(child) {
      flattenSecurityGroupTree(child, skipRoot ? 0 : (depth || 0) + 1, out, options);
    });
    return out;
  }

  function updateSecurityGroupMap(tree) {
    veSecurityGroupMap = Object.create(null);
    flattenSecurityGroupTree(tree, 0, []).forEach(function(group) {
      veSecurityGroupMap[group.id] = group;
    });
  }

  async function ensureVESecurityGroupsLoaded(force) {
    if (veSecurityGroupsLoaded && !force) return;
    if (veSecurityGroupsLoadPromise) return veSecurityGroupsLoadPromise;
    veSecurityGroupsLoadPromise = (async function() {
      var data = await api('/api/admin/security/groups');
      veSecurityGroupTree = data && data.tree ? data.tree : null;
      updateSecurityGroupMap(veSecurityGroupTree);
      veSecurityGroupsLoaded = true;
    })();
    try {
      await veSecurityGroupsLoadPromise;
    } finally {
      veSecurityGroupsLoadPromise = null;
    }
  }

  function visibleGroupIDs(ve) {
    var seen = Object.create(null);
    var ids = [];
    ((ve && Array.isArray(ve.visible_group_ids)) ? ve.visible_group_ids : []).forEach(function(raw) {
      var id = String(raw || '').trim();
      if (!id || seen[id]) return;
      seen[id] = true;
      ids.push(id);
    });
    return ids;
  }

  function formatVisibleDepartments(ve) {
    var ids = visibleGroupIDs(ve);
    if (!ids.length) return vt('globalVisible');
    return ids.map(function(id) {
      var group = veSecurityGroupMap[id];
      return group ? group.name : id;
    }).join(', ');
  }

  // --- Rendering ---
  function renderVEListHeader() {
    return '<div class="row header" style="grid-template-columns:' + veListGridTemplate() + '">' +
      '<div>' + vt('name') + '</div>' +
      '<div>' + vt('employeeType') + '</div>' +
      '<div>' + vt('skillDesc') + '</div>' +
      '<div>' + vt('ownerEmail') + '</div>' +
      '<div>' + vt('accessPolicy') + '</div>' +
      '<div>' + vt('visibleDepartments') + '</div>' +
      '<div>' + vt('residentEmployee') + '</div>' +
      '<div>' + vt('onlineStatus') + '</div>' +
      '<div>' + vt('registeredAt') + '</div>' +
      '<div>' + vt('actions') + '</div>' +
      '</div>';
  }

  function renderVERow(ve, actionButtons) {
    var veIDExpr = JSON.stringify(ve.id || '');
    var historyBtn = actionBtn(vt('openHistory'), 'btn-ghost', 'veLoadHistory(' + veIDExpr + ')');
    var visibilityBtn = actionBtn(vt('editVisibility'), 'btn-secondary', 'veOpenVisibilityEditor(' + veIDExpr + ')');
    var ownerText = ve.owner_email || ve.owner_user_id || '';
    var visibleText = formatVisibleDepartments(ve);
    return '<div class="row" style="grid-template-columns:' + veListGridTemplate() + '">' +
      '<div class="ve-name-cell">' + renderVEAvatar(ve) + '<strong>' + escapeHtml(truncate(ve.name || '', 50)) + '</strong></div>' +
      '<div>' + formatEmployeeType(ve) + '</div>' +
      '<div class="item-meta">' + escapeHtml(truncate(ve.skill_description || '', 100)) + '</div>' +
      '<div class="item-meta" title="' + escapeHtml(ownerText) + '">' + escapeHtml(truncate(ownerText, 42)) + '</div>' +
      '<div><span class="badge info">' + escapeHtml(formatAccessPolicy(ve.access_policy)) + '</span></div>' +
      '<div class="item-meta" title="' + escapeHtml(visibleText) + '">' + escapeHtml(truncate(visibleText, 42)) + '</div>' +
      '<div>' + (ve.resident ? '<span class="badge ok">' + escapeHtml(vt('residentEmployee')) + '</span>' : '<span class="item-meta">-</span>') + '</div>' +
      '<div>' + formatOnlineStatus(ve.online_status) + '</div>' +
      '<div class="item-meta">' + escapeHtml(formatDate(ve.registered_at)) + '</div>' +
      '<div class="ve-row-actions">' + visibilityBtn + historyBtn + actionButtons + '</div>' +
      '</div>';
  }

  function actionBtn(label, cls, onclick) {
    return '<button class="' + escapeHtml(cls) + '" style="height:27px;font-size:11px;padding:0 8px;min-width:0;white-space:nowrap" onclick="' + escapeHtml(onclick) + '">' + escapeHtml(label) + '</button>';
  }

  function renderPendingList(employees) {
    var pending = employees.filter(function(e) { return e.status === 'pending'; });
    var container = document.getElementById('vePendingList');
    if (!container) return;

    var header = renderVEListHeader();
    if (!pending.length) {
      container.innerHTML = header + '<div class="hint">' + escapeHtml(vt('emptyPending')) + '</div>';
      return;
    }

    container.innerHTML = header + pending.map(function(ve) {
      var veIDExpr = JSON.stringify(ve.id || '');
      var approveBtn = actionBtn(vt('approve'), 'btn-primary', 'veApprove(' + veIDExpr + ')');
      var rejectBtn = actionBtn(vt('reject'), 'btn-danger', 'veReject(' + veIDExpr + ')');
      return renderVERow(ve, approveBtn + rejectBtn);
    }).join('');
  }

  function renderActiveList(employees) {
    var active = employees.filter(function(e) { return e.status === 'active'; });
    var container = document.getElementById('veActiveList');
    if (!container) return;

    var header = renderVEListHeader();
    if (!active.length) {
      container.innerHTML = header + '<div class="hint">' + escapeHtml(vt('emptyActive')) + '</div>';
      return;
    }

    container.innerHTML = header + active.map(function(ve) {
      var veIDExpr = JSON.stringify(ve.id || '');
      var residentBtn = actionBtn(ve.resident ? vt('clearResident') : vt('setResident'), ve.resident ? 'btn-secondary' : 'btn-primary', 'veSetResident(' + veIDExpr + ',' + (ve.resident ? 'false' : 'true') + ')');
      var disableBtn = actionBtn(vt('disable'), 'btn-danger', 'veDisable(' + veIDExpr + ')');
      return renderVERow(ve, residentBtn + disableBtn);
    }).join('');
  }

  function isDeletedVEWithHistory(ve) {
    return !!(ve && (ve.runtime_missing || ve.history_retained));
  }

  function renderDeletedList(employees) {
    var deleted = employees.filter(isDeletedVEWithHistory);
    var container = document.getElementById('veDeletedList');
    if (!container) return;

    var header = renderVEListHeader();
    if (!deleted.length) {
      container.innerHTML = header + '<div class="hint">' + escapeHtml(vt('emptyDeleted')) + '</div>';
      return;
    }

    container.innerHTML = header + deleted.map(function(ve) {
      var veIDExpr = JSON.stringify(ve.id || ve.platform_employee_id || ve.machine_id || '');
      var purgeBtn = actionBtn(vt('purge'), 'btn-danger', 'vePurge(' + veIDExpr + ')');
      var forcePurgeBtn = actionBtn(vt('forcePurge'), 'btn-danger', 'veForcePurge(' + veIDExpr + ')');
      return renderVERow(ve, purgeBtn + forcePurgeBtn);
    }).join('');
  }

  function renderInactiveList(employees) {
    var inactive = employees.filter(function(e) { return e.status !== 'active' && e.status !== 'pending' && !isDeletedVEWithHistory(e); });
    var container = document.getElementById('veInactiveList');
    if (!container) return;

    var header = renderVEListHeader();
    if (!inactive.length) {
      container.innerHTML = header + '<div class="hint">' + escapeHtml(vt('emptyInactive')) + '</div>';
      return;
    }

    container.innerHTML = header + inactive.map(function(ve) {
      var veIDExpr = JSON.stringify(ve.id || ve.platform_employee_id || ve.machine_id || '');
      var purgeBtn = actionBtn(vt('purge'), 'btn-danger', 'vePurge(' + veIDExpr + ')');
      var forcePurgeBtn = actionBtn(vt('forcePurge'), 'btn-danger', 'veForcePurge(' + veIDExpr + ')');
      return renderVERow(ve, purgeBtn + forcePurgeBtn);
    }).join('');
  }

  function renderGroupConfig() {
    var input = document.getElementById('veMaxParticipantsInput');
    if (input) {
      input.value = String(veGroupConfig.max_group_participants || 5);
    }
  }

  function renderAutoApproveConfig() {
    var checkbox = document.getElementById('veAutoApproveInput');
    if (checkbox) checkbox.checked = !!veGroupConfig.auto_approve;
    var badge = document.getElementById('veAutoApproveBadge');
    if (badge) {
      badge.className = 'badge ' + (veGroupConfig.auto_approve ? 'ok' : 'warn');
      badge.textContent = veGroupConfig.auto_approve ? vt('autoApproveEnabled') : vt('autoApproveDisabled');
    }
  }

  function renderQuotaInfo() {
    var el = document.getElementById('veQuotaInfo');
    if (!el) return;
    el.textContent = vt('quotaInfo')
      .replace('{active}', String(veActiveCount))
      .replace('{quota}', String(veQuota));
  }

  function buildVEHistoryListHTML() {
    if (veHistoryLoading) {
      return '<div class="hint">' + escapeHtml(vt('loading')) + '</div>';
    }
    if (!veHistoryEmployeeID && !veHistoryHint) {
      return '<div class="hint">' + escapeHtml(vt('historyDesc')) + '</div>';
    }
    if (veHistoryHint && !veHistoryDiscussions.length) {
      return '<div class="hint">' + escapeHtml(veHistoryHint) + '</div>';
    }
    var intro = '';
    if (veHistoryHint) intro = '<div class="hint" style="margin-bottom:8px">' + escapeHtml(veHistoryHint) + '</div>';
    else if (veHistoryEmployeeLabel) intro = '<div class="hint" style="margin-bottom:8px">' + escapeHtml(vt('selectedEmployee').replace('{name}', veHistoryEmployeeLabel)) + '</div>';
    if (!veHistoryDiscussions.length) {
      return intro + '<div class="hint">' + escapeHtml(vt('historyEmpty')) + '</div>';
    }
    return intro + veHistoryDiscussions.map(function(d) {
      var idExpr = jsAttrString(d.id || '');
      var employeeMeta = d._employee_label ? ' - ' + escapeHtml(d._employee_label) : '';
      var counterpart = historyCounterpartText(d);
      return '<div class="item" style="margin-bottom:8px;padding:10px 12px">' +
        '<div class="item-head"><div><div class="item-title">' + escapeHtml(truncate(discussionTitle(d), 90)) + '</div>' +
        '<div class="item-meta">' + escapeHtml(d.status || '-') + ' - ' + escapeHtml(formatDate(d.updated_at || d.created_at)) + employeeMeta + ' - ' + escapeHtml((d.participant_ids || []).join(', ')) + '</div></div>' +
        '<button class="btn-secondary" style="height:28px;font-size:11px;padding:0 8px" onclick="vePreviewHistory(' + idExpr + ')">' + escapeHtml(vt('preview')) + '</button></div>' +
        '<div class="item-meta" style="margin-top:6px">' + escapeHtml(vt('counterpartEmail')) + ': ' + escapeHtml(counterpart) + '</div>' +
        (d.result_summary ? '<div class="item-meta" style="margin-top:6px">' + escapeHtml(vt('resultSummary')) + ': ' + escapeHtml(truncate(d.result_summary, 160)) + '</div>' : '') +
        '</div>';
    }).join('');
  }

  function renderVEHistoryList() {
    var container = document.getElementById('veHistoryList');
    if (container) container.innerHTML = '';
    renderVEHistoryDialog();
  }

  function ensureVEHistoryDialog() {
    var overlay = document.getElementById('veHistoryDialogOverlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'veHistoryDialogOverlay';
      overlay.className = 'session-modal-overlay';
      overlay.onclick = function(event) { if (event.target === overlay) global.closeVEHistoryDialog(); };
      document.body.appendChild(overlay);
    }
    return overlay;
  }

  function renderVEHistoryDialog() {
    var overlay = document.getElementById('veHistoryDialogOverlay');
    if (!overlay || String(overlay.className || '').indexOf('show') < 0) return;
    overlay.innerHTML = '<div class="session-modal ve-history-dialog" role="dialog" aria-modal="true" aria-labelledby="veHistoryDialogTitle">' +
      '<button class="close-btn" onclick="closeVEHistoryDialog()" aria-label="' + escapeHtml(vt('close')) + '">&times;</button>' +
      '<h3 id="veHistoryDialogTitle">' + escapeHtml(vt('historyTitle')) + '</h3>' +
      '<div class="item-meta" style="margin-bottom:10px">' + escapeHtml(vt('historyDesc')) + '</div>' +
      '<div class="ve-history-dialog-list">' + buildVEHistoryListHTML() + '</div>' +
      '</div>';
  }

  function openVEHistoryDialog() {
    var overlay = ensureVEHistoryDialog();
    overlay.classList.add('show');
    if (String(overlay.className || '').indexOf('show') < 0) overlay.className = String(overlay.className || '') + ' show';
    renderVEHistoryDialog();
  }

  global.closeVEHistoryDialog = function closeVEHistoryDialog() {
    var overlay = document.getElementById('veHistoryDialogOverlay');
    if (overlay) {
      overlay.classList.remove('show');
      overlay.className = String(overlay.className || '').replace(/\bshow\b/g, '').replace(/\s+/g, ' ').trim();
    }
  };

  function renderVELists() {
    renderPendingList(veListCache);
    renderActiveList(veListCache);
    renderDeletedList(veListCache);
    renderInactiveList(veListCache);
  }

  async function ensureVEGroupConfigLoaded() {
    if (veGroupConfigLoaded) return;
    var cfg = await api('/api/ve/config');
    veGroupConfig = cfg || { max_group_participants: 5, auto_approve: false };
    veGroupConfigLoaded = true;
    renderGroupConfig();
    renderAutoApproveConfig();
  }

  // --- API Calls ---
  global.loadVEList = async function loadVEList() {
    try {
      var data = await api('/api/ve/list');
      veListCache = data.employees || [];
      veGroupConfig = data.group_config || { max_group_participants: 5, auto_approve: false };
      veGroupConfigLoaded = true;
      veQuota = data.quota || 0;
      veActiveCount = data.active_count || 0;
      try {
        await ensureVESecurityGroupsLoaded(true);
      } catch (groupErr) {
        // Keep the employee list usable; unknown group IDs are shown as-is.
      }
      renderVELists();
      renderGroupConfig();
      renderAutoApproveConfig();
      renderQuotaInfo();
      renderVEHistoryList();
    } catch (err) {
      var msg = vt('loadFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veSearchHistory = function veSearchHistory(loadMatches) {
    if (!loadMatches) return;
    global.veLoadHistorySearch(currentVEQuery());
  };

  function currentVEQuery() {
    var input = document.getElementById('veHistorySearchInput');
    return input ? String(input.value || '').trim().toLowerCase() : '';
  }

  function mergeVEHistoryDiscussionMatches(matches, limit) {
    var byID = Object.create(null);
    var merged = [];
    (matches || []).forEach(function(match) {
      var label = veEmployeeLabel(match.employee || {});
      (match.discussions || []).forEach(function(item) {
        var id = item && item.id ? String(item.id) : '';
        if (!id) {
          var copy = Object.assign({}, item || {});
          copy._employee_label = label;
          merged.push(copy);
          return;
        }
        var existing = byID[id];
        if (!existing) {
          existing = Object.assign({}, item);
          existing._employee_labels = [];
          byID[id] = existing;
          merged.push(existing);
        }
        if (label && existing._employee_labels.indexOf(label) === -1) existing._employee_labels.push(label);
        existing._employee_label = existing._employee_labels.join(', ');
      });
    });
    merged.sort(function(a, b) { return Date.parse(b.updated_at || b.created_at || '') - Date.parse(a.updated_at || a.created_at || ''); });
    limit = parseInt(limit, 10);
    if (limit > 0 && merged.length > limit) return merged.slice(0, limit);
    return merged;
  }
  global.veLoadHistorySearch = async function veLoadHistorySearch(query) {
    query = String(query || '').trim();
    if (!query) {
      veHistoryEmployeeID = '';
      veHistoryEmployeeLabel = '';
      veHistoryDiscussions = [];
      veHistoryHint = '';
      renderVEHistoryList();
      return;
    }
    openVEHistoryDialog();
    veHistoryEmployeeID = 'search';
    veHistoryEmployeeLabel = '';
    veHistoryDiscussions = [];
    veHistoryHint = '';
    veHistoryLoading = true;
    renderVEHistoryList();
    try {
      var data = await api('/api/ve/history/search?q=' + encodeURIComponent(query) + '&limit=20');
      var matches = data.matches || [];
      var merged = mergeVEHistoryDiscussionMatches(matches, 20);
      veHistoryDiscussions = merged;
      if (!matches.length) veHistoryHint = vt('searchNoMatch');
      else if (matches.length === 1) veHistoryEmployeeLabel = veEmployeeLabel(matches[0].employee || {});
      else veHistoryHint = vt('searchMultiple').replace('{count}', String(matches.length));
    } catch (err) {
      var msg = vt('historyLoadFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    } finally {
      veHistoryLoading = false;
      renderVEHistoryList();
    }
  };

  global.veLoadHistory = async function veLoadHistory(veID) {
    var employee = veListCache.find(function(ve) { return ve.id === veID; }) || null;
    return global.veLoadHistoryForEmployees(employee ? [employee] : [{ id: veID }]);
  };

  global.veLoadHistoryForEmployees = async function veLoadHistoryForEmployees(employees) {
    employees = employees || [];
    openVEHistoryDialog();
    veHistoryEmployeeID = employees.length === 1 ? (employees[0].id || '') : 'search';
    veHistoryEmployeeLabel = employees.length === 1 ? veEmployeeLabel(employees[0]) : '';
    veHistoryDiscussions = [];
    veHistoryHint = employees.length > 1 ? vt('searchMultiple').replace('{count}', String(employees.length)) : '';
    veHistoryLoading = true;
    renderVEHistoryList();
    try {
      var merged = [];
      for (var i = 0; i < employees.length; i++) {
        var ve = employees[i];
        if (!ve || !ve.id) continue;
        var data = await api('/api/ve/' + encodeURIComponent(ve.id) + '/history?limit=20');
        var label = veEmployeeLabel(data.employee || ve);
        (data.discussions || []).forEach(function(d) {
          d._employee_label = label;
          merged.push(d);
        });
      }
      merged.sort(function(a, b) { return Date.parse(b.updated_at || b.created_at || '') - Date.parse(a.updated_at || a.created_at || ''); });
      veHistoryDiscussions = merged.slice(0, 20);
    } catch (err) {
      var msg = vt('historyLoadFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    } finally {
      veHistoryLoading = false;
      renderVEHistoryList();
    }
  };

  global.vePreviewHistory = async function vePreviewHistory(discussionID) {
    try {
      var detail = await api('/api/ve/history/' + encodeURIComponent(discussionID) + '/detail');
      showVEHistoryPreview(detail);
    } catch (err) {
      var msg = vt('previewLoadFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.closeVEHistoryPreview = function closeVEHistoryPreview() {
    var overlay = document.getElementById('veHistoryPreviewOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  function historyAttachmentURL(fileURL, discussionID) {
    if (!fileURL || !discussionID) return '';
    try {
      var origin = (global.location && global.location.origin) || (window.location && window.location.origin) || '';
      var URLCtor = global.URL || window.URL || URL;
      var url = new URLCtor(fileURL, origin);
      if (url.origin !== origin || url.pathname.indexOf('/api/ve/files/') !== 0) return '';
      var parts = url.pathname.split('/').filter(Boolean);
      if (parts.length < 4 || parts[0] !== 'api' || parts[1] !== 've' || parts[2] !== 'files') return '';
      if (parts[3] === 'upload') return '';
      var fileID = '';
      if (parts[3] === 'download') {
        if (parts.length !== 5) return '';
        fileID = parts[4] || '';
      } else {
        if (parts.length !== 4) return '';
        fileID = parts[3] || '';
      }
      try {
        if (decodeURIComponent(fileID).indexOf('/') >= 0 || decodeURIComponent(fileID).indexOf('\\') >= 0) return '';
      } catch (decodeErr) {
        return '';
      }
      if (!fileID || fileID === 'download') return '';
      return '/api/ve/history/' + encodeURIComponent(discussionID) + '/attachments/' + encodeURIComponent(decodeURIComponent(fileID));
    } catch (err) {
      return '';
    }
  }

  global.veDownloadHistoryAttachment = async function veDownloadHistoryAttachment(url, filename) {
    try {
      var headers = {};
      if (typeof token === 'function' && token()) headers.Authorization = 'Bearer ' + token();
      var resp = await fetch(url, { headers: headers });
      if (!resp.ok) throw new Error(resp.statusText || ('HTTP ' + resp.status));
      var blob = await resp.blob();
      var objectURL = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = objectURL;
      a.download = filename || 'attachment';
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(function() { URL.revokeObjectURL(objectURL); }, 1000);
    } catch (err) {
      var msg = vt('previewLoadFailed').replace('{error}', err.message || String(err));
      showToast(msg, 'error');
    }
  };

  function inlineTextAttachmentURL(att) {
    if (!att || !att.content) return '';
    var content = String(att.content || '').replace(/\s+/g, '');
    if (!/^[A-Za-z0-9+/=_-]+$/.test(content)) return '';
    content = content.replace(/-/g, '+').replace(/_/g, '/');
    while (content.length % 4 !== 0) content += '=';
    var mime = String(att.mime_type || 'text/plain').toLowerCase();
    if (mime.indexOf('text/') !== 0 && mime !== 'application/json' && mime !== 'application/xml') mime = 'text/plain';
    return 'data:' + encodeURIComponent(mime) + ';base64,' + content;
  }

  function messageAttachmentItems(m, discussionID) {
    var items = [];
    (m.text_attachments || []).forEach(function(att) {
      var filename = att.filename || 'text.txt';
      items.push({ label: filename + (att.mime_type ? ' (' + att.mime_type + ')' : ''), filename: filename, url: inlineTextAttachmentURL(att) });
    });
    (m.image_attachments || []).forEach(function(att) {
      items.push({ label: (att.filename || 'image') + (att.mime_type ? ' (' + att.mime_type + ')' : ''), filename: att.filename || 'image', url: historyAttachmentURL(att.file_url, discussionID) });
    });
    (m.file_attachments || []).forEach(function(att) {
      items.push({ label: (att.filename || 'file') + (att.mime_type ? ' (' + att.mime_type + ')' : '') + (att.size_bytes ? ' - ' + att.size_bytes + ' bytes' : ''), filename: att.filename || 'file', url: historyAttachmentURL(att.file_url, discussionID) });
    });
    return items;
  }

  function renderHistoryAttachmentItems(items) {
    return (items || []).map(function(item) {
      var label = escapeHtml(item.label || 'file');
      if (!item.url) return label;
      return '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" onclick="veDownloadHistoryAttachment(' + jsAttrString(item.url) + ',' + jsAttrString(item.filename || item.label || 'attachment') + ')">' + label + '</button>';
    }).join(' ');
  }

  function showVEHistoryPreview(detail) {
    var overlay = document.getElementById('veHistoryPreviewOverlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'veHistoryPreviewOverlay';
      overlay.className = 'session-modal-overlay';
      overlay.onclick = function(event) { if (event.target === overlay) global.closeVEHistoryPreview(); };
      document.body.appendChild(overlay);
    }
    var discussion = detail.discussion || {};
    var messages = detail.messages || (detail.session && detail.session.messages) || [];
    var participants = discussion.participant_ids || (detail.session && (detail.session.participants || []).map(function(p) { return p.id || p.name || ''; })) || [];
    var headerMeta = [
      escapeHtml(vt('status')) + ': ' + escapeHtml(discussion.status || '-'),
      escapeHtml(vt('createdAt')) + ': ' + escapeHtml(formatDate(discussion.created_at)),
      escapeHtml(vt('updatedAt')) + ': ' + escapeHtml(formatDate(discussion.updated_at))
    ].join(' &nbsp; ');
    var counterpart = historyCounterpartText(discussion);
    var resultHtml = discussion.result_summary ? '<div class="session-item"><div class="session-label">' + escapeHtml(vt('resultSummary')) + '</div><div class="session-value">' + escapeHtml(compactMessageText(discussion.result_summary, 900)) + '</div></div>' : '';
    overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="veHistoryPreviewTitle" style="width:min(900px,calc(100% - 48px));max-height:86vh;overflow:auto">' +
      '<button class="close-btn" onclick="closeVEHistoryPreview()" aria-label="' + escapeHtml(vt('close')) + '">&times;</button>' +
      '<h3 id="veHistoryPreviewTitle">' + escapeHtml(discussionTitle(discussion) || vt('historyTitle')) + '</h3>' +
      '<div class="item-meta" style="margin-bottom:6px">' + headerMeta + '</div>' +
      '<div class="item-meta" style="margin-bottom:6px">' + escapeHtml(vt('counterpartEmail')) + ': ' + escapeHtml(counterpart) + '</div>' +
      '<div class="item-meta" style="margin-bottom:10px">' + escapeHtml(vt('participants')) + ': ' + escapeHtml(participants.join(', ') || '-') + '</div>' +
      resultHtml +
      '<div class="item-title" style="margin-bottom:8px">' + escapeHtml(vt('messages')) + '</div>' +
      messages.slice(-12).map(function(m) {
        var attachments = messageAttachmentItems(m, discussion.id);
        return '<div class="session-item"><div class="session-field"><span class="session-label">' + escapeHtml(m.from_id || '-') + '</span><span class="session-value">' + escapeHtml(formatDate(m.created_at)) + '</span></div>' +
          '<div class="session-value">' + escapeHtml(compactMessageText(m.content, 700)) + '</div>' +
          (attachments.length ? '<div class="item-meta" style="margin-top:6px">' + escapeHtml(vt('attachments')) + ': ' + renderHistoryAttachmentItems(attachments) + '</div>' : '') + '</div>';
      }).join('') + '</div>';
    overlay.classList.add('show');
  }

  function renderVEVisibilityOptions(ve) {
    var selected = Object.create(null);
    visibleGroupIDs(ve).forEach(function(id) { selected[id] = true; });
    var roots = veSecurityGroupTree && Array.isArray(veSecurityGroupTree.children) ? veSecurityGroupTree.children : [];
    if (!roots.length) return '<div class="hint">' + escapeHtml(vt('noDepartments')) + '</div>';
    return '<div class="hint" style="margin:8px 0 10px">' + escapeHtml(vt('departmentTreeHint')) + '</div>' +
      '<div class="ve-department-tree" role="tree" aria-label="' + escapeHtml(vt('visibleDepartments')) + '" style="max-height:46vh;overflow:auto;margin:10px 0;padding:8px;border:1px solid var(--border,#d8dee9);border-radius:10px;background:var(--panel-muted,#f8fafc)">' +
      roots.map(function(node) { return renderVEVisibilityTreeNode(node, selected, 0); }).join('') +
      '</div>';
  }

  function renderVEVisibilityTreeNode(node, selected, depth) {
    if (!node) return '';
    var id = String(node.id || '').trim();
    if (!id) return '';
    var name = String(node.name || id);
    var children = Array.isArray(node.children) ? node.children.filter(function(child) { return child && String(child.id || '').trim(); }) : [];
    var checked = selected[id] ? ' checked' : '';
    var pad = Math.min(depth || 0, 10) * 18;
    var branch = children.length ? '<span aria-hidden="true" style="width:14px;color:var(--text-muted,#64748b);font-size:13px;line-height:1">&rsaquo;</span>' : '<span aria-hidden="true" style="width:14px"></span>';
    return '<div role="treeitem" aria-checked="' + (selected[id] ? 'true' : 'false') + '"' + (children.length ? ' aria-expanded="true"' : '') + ' aria-level="' + ((depth || 0) + 1) + '" style="margin:2px 0">' +
      '<label style="display:flex;align-items:center;gap:8px;min-height:32px;padding:3px 8px 3px ' + (8 + pad) + 'px;border-radius:8px;cursor:pointer;color:var(--text,#1f2937)">' +
      branch +
      '<input type="checkbox" class="ve-visible-group-option" value="' + escapeHtml(id) + '"' + checked + ' style="width:16px;height:16px;flex:0 0 auto">' +
      '<span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + escapeHtml(name) + '</span>' +
      '</label>' +
      (children.length ? '<div role="group">' + children.map(function(child) { return renderVEVisibilityTreeNode(child, selected, (depth || 0) + 1); }).join('') + '</div>' : '') +
      '</div>';
  }

  function showVEVisibilityEditor(ve) {
    var overlay = document.getElementById('veVisibilityOverlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'veVisibilityOverlay';
      overlay.className = 'session-modal-overlay';
      overlay.onclick = function(event) { if (event.target === overlay) global.closeVEVisibilityEditor(); };
      document.body.appendChild(overlay);
    }
    var title = (ve && (ve.name || ve.id)) || vt('editVisibility');
    overlay.innerHTML = '<div class="session-modal" style="width:min(560px,calc(100% - 48px));max-height:86vh;overflow:auto">' +
      '<button class="close-btn" onclick="closeVEVisibilityEditor()" aria-label="' + escapeHtml(vt('close')) + '">&times;</button>' +
      '<h3>' + escapeHtml(vt('visibleDepartments')) + '</h3>' +
      '<div class="item-meta" style="margin-bottom:8px">' + escapeHtml(title) + '</div>' +
      renderVEVisibilityOptions(ve) +
      '<div style="display:flex;gap:8px;justify-content:flex-end;margin-top:12px">' +
      '<button class="btn-ghost" onclick="closeVEVisibilityEditor()">' + escapeHtml(vt('close')) + '</button>' +
      '<button class="btn-primary" onclick="veSaveVisibility(' + jsAttrString(ve && ve.id) + ')">' + escapeHtml(vt('saveVisibility')) + '</button>' +
      '</div></div>';
    bindVEVisibilityTreeState(overlay);
    overlay.classList.add('show');
  }

  function bindVEVisibilityTreeState(overlay) {
    if (!overlay || typeof overlay.querySelectorAll !== 'function') return;
    overlay.querySelectorAll('.ve-visible-group-option').forEach(function(input) {
      if (!input || typeof input.addEventListener !== 'function') return;
      input.addEventListener('change', function() {
        var item = typeof input.closest === 'function' ? input.closest('[role="treeitem"]') : null;
        if (item && typeof item.setAttribute === 'function') item.setAttribute('aria-checked', input.checked ? 'true' : 'false');
      });
    });
  }

  global.closeVEVisibilityEditor = function closeVEVisibilityEditor() {
    var overlay = document.getElementById('veVisibilityOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.veOpenVisibilityEditor = async function veOpenVisibilityEditor(veID) {
    var employee = veListCache.find(function(ve) { return ve.id === veID; }) || null;
    if (!employee) return;
    try {
      await ensureVESecurityGroupsLoaded(true);
      showVEVisibilityEditor(employee);
    } catch (err) {
      var msg = vt('visibilitySaveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veSaveVisibility = async function veSaveVisibility(veID) {
    var overlay = document.getElementById('veVisibilityOverlay');
    var ids = [];
    if (overlay) {
      overlay.querySelectorAll('.ve-visible-group-option:checked').forEach(function(input) {
        ids.push(input.value);
      });
    }
    try {
      await api('/api/ve/' + encodeURIComponent(veID) + '/visibility', {
        method: 'PUT',
        body: JSON.stringify({ visible_group_ids: ids })
      });
      showToast(vt('visibilitySaved'), 'success');
      global.closeVEVisibilityEditor();
      await global.loadVEList();
    } catch (err) {
      var msg = vt('visibilitySaveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veApprove = async function veApprove(veID) {
    if (!confirm(vt('approve') + '?')) return;
    try {
      await api('/api/ve/' + encodeURIComponent(veID) + '/approve', { method: 'POST' });
      showToast(vt('approveSuccess'), 'success');
      await global.loadVEList();
    } catch (err) {
      var msg = vt('approveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veReject = async function veReject(veID) {
    var reason = prompt(vt('reject') + ' - reason:');
    if (reason === null) return;
    try {
      await api('/api/ve/' + encodeURIComponent(veID) + '/reject', {
        method: 'POST',
        body: JSON.stringify({ reason: reason })
      });
      showToast(vt('rejectSuccess'), 'success');
      await global.loadVEList();
    } catch (err) {
      var msg = vt('rejectFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veDisable = async function veDisable(veID) {
    if (!confirm(vt('disable') + '?')) return;
    try {
      await api('/api/ve/' + encodeURIComponent(veID) + '/disable', { method: 'POST' });
      showToast(vt('disableSuccess'), 'success');
      await global.loadVEList();
    } catch (err) {
      var msg = vt('disableFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.vePurge = async function vePurge(veID) {
    if (!confirm(vt('purge') + '?')) return;
    try {
      await api('/api/ve/' + encodeURIComponent(veID), { method: 'DELETE' });
      showToast(vt('purgeSuccess'), 'success');
      await global.loadVEList();
    } catch (err) {
      var msg = vt('purgeFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  function promptVEAdminPassword(message) {
    if (!document || !document.createElement || !document.body) {
      return Promise.resolve(prompt(message));
    }
    var probe = document.createElement('button');
    if (!probe || typeof probe.addEventListener !== 'function') {
      return Promise.resolve(prompt(message));
    }
    return new Promise(function(resolve) {
      var overlay = document.createElement('div');
      overlay.className = 'session-modal-overlay show';
      overlay.style.cssText = 'z-index:9999;background:rgba(15,23,42,.42);padding:18px';
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="veForcePurgeDialogTitle" style="width:min(420px,100%);max-height:none;overflow:visible;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22)">' +
        '<div class="item-title" id="veForcePurgeDialogTitle" style="margin-bottom:8px">' + escapeHtml(vt('forcePurge')) + '</div>' +
        '<div class="item-meta" style="margin-bottom:12px">' + escapeHtml(message) + '</div>' +
        '<input id="veForcePurgePasswordInput" type="password" autocomplete="current-password" style="width:100%;height:36px;margin-bottom:12px">' +
        '<div class="actions" style="justify-content:flex-end;gap:8px">' +
        '<button type="button" class="btn-ghost" id="veForcePurgeCancelBtn">' + escapeHtml(vt('close')) + '</button>' +
        '<button type="button" class="btn-danger" id="veForcePurgeConfirmBtn">' + escapeHtml(vt('forcePurge')) + '</button>' +
        '</div></div>';
      var done = function(value) {
        if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay);
        resolve(value);
      };
      document.body.appendChild(overlay);
      if (global.AdminUI && typeof global.AdminUI.bindModalOverlayDismiss === 'function') {
        global.AdminUI.bindModalOverlayDismiss(overlay, function() { done(null); });
      } else {
        overlay.onclick = function(event) {
          if (event && event.target === overlay) done(null);
        };
      }
      var input = overlay.querySelector ? overlay.querySelector('#veForcePurgePasswordInput') : null;
      var cancel = overlay.querySelector ? overlay.querySelector('#veForcePurgeCancelBtn') : null;
      var ok = overlay.querySelector ? overlay.querySelector('#veForcePurgeConfirmBtn') : null;
      if (cancel) cancel.addEventListener('click', function() { done(null); });
      if (ok) ok.addEventListener('click', function() { done(input ? input.value : ''); });
      if (input) {
        input.addEventListener('keydown', function(event) {
          if (event.key === 'Enter') done(input.value);
          if (event.key === 'Escape') done(null);
        });
        input.focus();
      }
    });
  }

  global.veForcePurge = async function veForcePurge(veID) {
    var password = await promptVEAdminPassword(vt('forcePurgePasswordPrompt'));
    if (password === null) return;
    if (!password) {
      showToast(vt('forcePurgeFailed').replace('{error}', 'admin_password is required'), 'error');
      return;
    }
    try {
      await api('/api/ve/' + encodeURIComponent(veID) + '/force-delete', {
        method: 'POST',
        body: JSON.stringify({ admin_password: password })
      });
      showToast(vt('forcePurgeSuccess'), 'success');
      await global.loadVEList();
    } catch (err) {
      var msg = vt('forcePurgeFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veSetResident = async function veSetResident(veID, resident) {
    try {
      await api('/api/ve/' + encodeURIComponent(veID) + '/resident', {
        method: 'PUT',
        body: JSON.stringify({ resident: !!resident })
      });
      showToast(vt('residentSaved'), 'success');
      await global.loadVEList();
    } catch (err) {
      var msg = vt('residentSaveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veSaveGroupConfig = async function veSaveGroupConfig() {
    var input = document.getElementById('veMaxParticipantsInput');
    if (!input) return;
    var val = parseInt(input.value, 10);
    if (isNaN(val) || val < 1 || val > 10) {
      showToast('max_group_participants must be 1-10', 'error');
      return;
    }
    try {
      await ensureVEGroupConfigLoaded();
      await api('/api/ve/config', {
        method: 'PUT',
        body: JSON.stringify({ max_group_participants: val, auto_approve: !!veGroupConfig.auto_approve })
      });
      veGroupConfig.max_group_participants = val;
      renderGroupConfig();
      showToast(vt('configSaved'), 'success');
      if (veGroupConfig.auto_approve) await global.loadVEList();
      // Push ve:group_config event is handled server-side on config change
    } catch (err) {
      var msg = vt('configSaveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.veSaveAutoApproveConfig = async function veSaveAutoApproveConfig() {
    var checkbox = document.getElementById('veAutoApproveInput');
    var nextAutoApprove = !!(checkbox && checkbox.checked);
    var input = document.getElementById('veMaxParticipantsInput');
    var requestedParticipants = NaN;
    if (input && String(input.value || '').trim()) {
      requestedParticipants = parseInt(input.value, 10);
      if (isNaN(requestedParticipants) || requestedParticipants < 1 || requestedParticipants > 10) {
        showToast('max_group_participants must be 1-10', 'error');
        renderAutoApproveConfig();
        return;
      }
    }
    try {
      await ensureVEGroupConfigLoaded();
      var maxParticipants = !isNaN(requestedParticipants) ? requestedParticipants : parseInt((input && input.value) || veGroupConfig.max_group_participants || 5, 10);
      if (isNaN(maxParticipants) || maxParticipants < 1 || maxParticipants > 10) {
        showToast('max_group_participants must be 1-10', 'error');
        renderAutoApproveConfig();
        return;
      }
      await api('/api/ve/config', {
        method: 'PUT',
        body: JSON.stringify({ max_group_participants: maxParticipants, auto_approve: nextAutoApprove })
      });
      veGroupConfig.max_group_participants = maxParticipants;
      veGroupConfig.auto_approve = nextAutoApprove;
      renderAutoApproveConfig();
      showToast(vt('configSaved'), 'success');
      if (nextAutoApprove) await global.loadVEList();
    } catch (err) {
      var msg = vt('configSaveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
      renderAutoApproveConfig();
    }
  };

  // --- Tab Registration ---
  if (global.AdminTabRegistry) {
    global.AdminTabRegistry.registerTab({
      id: 'virtualemployees',
      title: function() { return vt('tabTitle'); },
      subtitle: function() { return vt('tabSubtitle'); },
      onOpen: function() { global.loadVEList(); }
    });
  }

  // --- Text Application (i18n) ---
  function setText(id, text) {
    var el = document.getElementById(id);
    if (el) el.textContent = text;
  }

  function applyVEText() {
    setText('navVirtualEmployees', vt('navTitle'));
    setText('navVirtualEmployeesDesc', vt('navDesc'));
    setText('vePanelTitle', vt('panelTitle'));
    setText('vePanelDesc', vt('panelDesc'));
    setText('veAutoApproveTitle', vt('autoApproveTitle'));
    setText('veAutoApproveDesc', vt('autoApproveDesc'));
    setText('veAutoApproveLabel', vt('autoApproveLabel'));
    setText('veGroupConfigTitle', vt('groupConfigTitle'));
    setText('veGroupConfigDesc', vt('groupConfigDesc'));
    setText('veMaxParticipantsLabel', vt('maxParticipants'));
    setText('veSaveConfigBtn', vt('saveConfig'));
    setText('vePendingTitle', vt('pendingTitle'));
    setText('veActiveTitle', vt('activeTitle'));
    setText('veDeletedTitle', vt('deletedTitle'));
    setText('veInactiveTitle', vt('inactiveTitle'));
    setText('veHistoryTitle', vt('historyTitle'));
    setText('veHistoryDesc', vt('historyDesc'));
    setText('veRefreshBtn', vt('refresh'));
    var search = document.getElementById('veHistorySearchInput');
    if (search) search.placeholder = vt('historySearchPlaceholder');
    setText('veSearchHistoryBtn', vt('searchHistory'));
    renderAutoApproveConfig();
  }

  // Apply text on load and on language change
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', applyVEText);
  } else {
    applyVEText();
  }
  if (global.AdminTabRegistry) {
    global.AdminTabRegistry.onLanguageChange(function() { applyVEText(); });
  }

})(window);
