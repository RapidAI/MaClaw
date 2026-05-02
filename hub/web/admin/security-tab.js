/*
 * Security admin module.
 * ASCII only.
 */
(function(global) {
  function state() {
    if (!global.__securityAdminState) {
      global.__securityAdminState = {
        groupTree: [],
        selectedGroupId: null,
        selectedGroupName: null,
        contextGroupId: null,
        contextGroupName: null,
        policyCache: null,
        assignGroupId: null,
        defaultGroupPickedId: null,
        expandedGroupIds: {},
        loadedChildrenGroupIds: {},
        loadingChildrenGroupIds: {},
        defaultGroupTree: [],
        assignUsers: [],
        selectedAssignEmail: '',
        selectedAssignEmails: {},
        contextMenuHideHandler: null,
        membersPage: 1,
        membersPageSize: 50,
        membersCache: []
      };
    }
    return global.__securityAdminState;
  }

  function getCurrentLang() {
    if (typeof currentLang !== 'undefined' && (currentLang === 'zh' || currentLang === 'en')) return currentLang;
    if (global.currentLang === 'zh' || global.currentLang === 'en') return global.currentLang;
    try {
      var saved = global.localStorage && global.localStorage.getItem('maclaw_admin_lang');
      if (saved === 'zh' || saved === 'en') return saved;
    } catch (_) {}
    return (global.document && global.document.documentElement && global.document.documentElement.lang === 'zh-CN') ? 'zh' : 'en';
  }

  function isZh() {
    return getCurrentLang() === 'zh';
  }

  function text(zh, en) {
    return isZh() ? zh : en;
  }

  function secTr(key, zh, en) {
    if (typeof global.tr === 'function') {
      var translated = global.tr(key);
      if (translated && translated !== key) return translated;
    }
    return text(zh, en);
  }

  var SECURITY_I18N = {
    subgroupLabel: { zh: '\u5b50\u7ec4:', en: 'Sub-groups:' },
    membersLabel: { zh: '\u6210\u5458', en: 'Members' },
    userIndex: { zh: '\u7b2c {index} \u4e2a\u7528\u6237', en: 'User #{index}' },
    remove: { zh: '\u79fb\u9664', en: 'Remove' },
    pagerSummary: { zh: '\u7b2c {page} / {totalPages} \u9875\uff0c\u663e\u793a {start}-{end} / {total}', en: 'Page {page} / {totalPages}, showing {start}-{end} / {total}' },
    previous: { zh: '\u4e0a\u4e00\u9875', en: 'Previous' },
    next: { zh: '\u4e0b\u4e00\u9875', en: 'Next' },
    noMembers: { zh: '\u65e0\u6210\u5458', en: 'No members' },
    noGroups: { zh: '\u65e0\u7ec4\u7ec7\u6570\u636e', en: 'No groups' },
    loading: { zh: '\u6b63\u5728\u52a0\u8f7d...', en: 'Loading...' },
    enabled: { zh: '\u5df2\u542f\u7528', en: 'Enabled' },
    disabled: { zh: '\u5df2\u7981\u7528', en: 'Disabled' },
    notSet: { zh: '\u672a\u8bbe\u7f6e', en: 'Not set' },
    noUsersMatchSearch: { zh: '\u65e0\u5339\u914d\u7528\u6237', en: 'No users match the search' },
    noUsersAvailable: { zh: '\u6682\u65e0\u53ef\u5206\u914d\u7528\u6237', en: 'No users available' },
    status: { zh: '\u72b6\u6001', en: 'Status' },
    unknown: { zh: '\u672a\u77e5', en: 'Unknown' },
    statusActive: { zh: '\u5df2\u542f\u7528', en: 'Active' },
    statusInactive: { zh: '\u672a\u542f\u7528', en: 'Inactive' },
    statusPending: { zh: '\u5f85\u5904\u7406', en: 'Pending' },
    statusBlocked: { zh: '\u5df2\u5c4f\u853d', en: 'Blocked' },
    statusDisabled: { zh: '\u5df2\u7981\u7528', en: 'Disabled' },
    statusApproved: { zh: '\u5df2\u6279\u51c6', en: 'Approved' },
    move: { zh: '\u79fb\u5165', en: 'Move' },
    showingUsers: { zh: '\u663e\u793a {visible} / {total} \u4e2a\u7528\u6237', en: 'Showing {visible} / {total} users' },
    selectedUsers: { zh: '\u5df2\u9009 {count} \u4e2a\u7528\u6237', en: 'Selected {count} users' },
    selectVisibleUsers: { zh: '\u5168\u9009\u5217\u8868\u7528\u6237', en: 'Select listed users' },
    clearVisibleUsers: { zh: '\u53d6\u6d88\u5217\u8868\u5168\u9009', en: 'Clear listed users' },
    loadingMembers: { zh: '\u6b63\u5728\u52a0\u8f7d\u6210\u5458...', en: 'Loading members...' },
    removed: { zh: '\u5df2\u79fb\u9664', en: 'Removed' },
    centralizedEnabled: { zh: '\u96c6\u4e2d\u7b56\u7565\u5df2\u542f\u7528', en: 'Centralized policy enabled' },
    centralizedDisabled: { zh: '\u96c6\u4e2d\u7b56\u7565\u5df2\u7981\u7528', en: 'Centralized policy disabled' },
    orgEnabled: { zh: '\u7ec4\u7ec7\u67b6\u6784\u5df2\u542f\u7528', en: 'Org structure enabled' },
    orgDisabled: { zh: '\u7ec4\u7ec7\u67b6\u6784\u5df2\u7981\u7528', en: 'Org structure disabled' },
    defaultGroupSet: { zh: '\u9ed8\u8ba4\u7ec4\u5df2\u8bbe\u7f6e', en: 'Default group set' },
    assignTitleWithGroup: { zh: '\u79fb\u5165\u7528\u6237\u5230\u90e8\u95e8: {name}', en: 'Move users to department: {name}' },
    loadingUsers: { zh: '\u6b63\u5728\u52a0\u8f7d\u7528\u6237\u5217\u8868...', en: 'Loading users...' },
    moveUsersHere: { zh: '\u79fb\u5165\u7528\u6237', en: 'Move Users Here' },
    moveUsers: { zh: '\u79fb\u5165\u7528\u6237', en: 'Move Users' },
    moveUsersDesc: { zh: '\u53ef\u4ee5\u641c\u7d22\u5e76\u6279\u91cf\u5c06\u7528\u6237\u79fb\u5165\u5f53\u524d\u90e8\u95e8\u3002', en: 'Search and move users into the selected department.' },
    searchEmailOrSn: { zh: '\u641c\u7d22\u90ae\u7bb1\u6216 SN', en: 'Search email or SN' },
    departmentActions: { zh: '\u90e8\u95e8\u64cd\u4f5c\u83dc\u5355', en: 'Department actions' },
    userMoved: { zh: '\u7528\u6237\u5df2\u79fb\u5165', en: 'User moved' },
    reload: { zh: '\u5237\u65b0', en: 'Reload' },
    centralizedPolicy: { zh: '\u96c6\u4e2d\u7b56\u7565', en: 'Centralized Policy' },
    orgStructure: { zh: '\u7ec4\u7ec7\u67b6\u6784', en: 'Org Structure' },
    defaultGroup: { zh: '\u9ed8\u8ba4\u7ec4', en: 'Default Group' },
    set: { zh: '\u8bbe\u7f6e', en: 'Set' },
    groupTree: { zh: '\u7ec4\u7ec7\u6811', en: 'Group Tree' },
    createSubDepartment: { zh: '\u521b\u5efa\u5b50\u90e8\u95e8', en: 'Create Sub-department' },
    renameDepartment: { zh: '\u91cd\u547d\u540d\u90e8\u95e8', en: 'Rename Department' },
    deleteDepartment: { zh: '\u5220\u9664\u90e8\u95e8', en: 'Delete Department' },
    save: { zh: '\u4fdd\u5b58', en: 'Save' },
    cancel: { zh: '\u53d6\u6d88', en: 'Cancel' },
    confirm: { zh: '\u786e\u8ba4', en: 'Confirm' },
    chooseDefaultGroupDesc: { zh: '\u4e3a\u65b0\u7528\u6237\u9009\u62e9\u9ed8\u8ba4\u6240\u5c5e\u7ec4\u3002', en: 'Choose the default group for new users.' },
    defaultGroupPrefix: { zh: '\u9ed8\u8ba4\u7ec4: ', en: 'Default group: ' },
    loadSecuritySettingsFailed: { zh: '\u52a0\u8f7d\u5b89\u5168\u8bbe\u7f6e\u5931\u8d25: ', en: 'Load security settings failed: ' },
    loadGroupTreeFailed: { zh: '\u52a0\u8f7d\u7ec4\u7ec7\u6811\u5931\u8d25: ', en: 'Load group tree failed: ' },
    loadChildGroupsFailed: { zh: '\u52a0\u8f7d\u5b50\u90e8\u95e8\u5931\u8d25: ', en: 'Load child groups failed: ' },
    policyPrefix: { zh: '\u7b56\u7565: ', en: 'Policy: ' },
    groupIdPrefix: { zh: '\u7ec4 ID: ', en: 'Group ID: ' },
    fileOutbound: { zh: '\u6587\u4ef6\u5916\u53d1', en: 'File Outbound' },
    imageOutbound: { zh: '\u56fe\u7247\u5916\u53d1', en: 'Image Outbound' },
    gossip: { zh: 'Gossip \u529f\u80fd', en: 'Gossip' },
    yoloMode: { zh: 'YOLO \u6a21\u5f0f', en: 'YOLO Mode' },
    smartRoute: { zh: '\u667a\u80fd\u8def\u7531', en: 'Smart Route' },
    guardrailMode: { zh: '\u62a4\u680f\u6a21\u5f0f', en: 'Guardrail Mode' },
    sandboxMode: { zh: '\u6c99\u7bb1\u6a21\u5f0f', en: 'Sandbox Mode' },
    networkLevel: { zh: '\u7f51\u7edc\u7ea7\u522b', en: 'Network Level' },
    custom: { zh: '\u81ea\u5b9a\u4e49', en: 'Custom' },
    inheritedFrom: { zh: '\u7ee7\u627f\u81ea ', en: 'Inherited from ' },
    selectGroupFirst: { zh: '\u8bf7\u5148\u9009\u62e9\u4e00\u4e2a\u7ec4', en: 'Select a group first' },
    policySaved: { zh: '\u7b56\u7565\u5df2\u4fdd\u5b58', en: 'Policy saved' },
    savePolicyFailed: { zh: '\u4fdd\u5b58\u7b56\u7565\u5931\u8d25: ', en: 'Save policy failed: ' },
    removeFailed: { zh: '\u79fb\u9664\u5931\u8d25: ', en: 'Remove failed: ' },
    updateFailed: { zh: '\u66f4\u65b0\u5931\u8d25: ', en: 'Update failed: ' },
    pleaseSelectGroup: { zh: '\u8bf7\u9009\u62e9\u4e00\u4e2a\u7ec4', en: 'Please select a group' },
    setDefaultGroupFailed: { zh: '\u8bbe\u7f6e\u9ed8\u8ba4\u7ec4\u5931\u8d25: ', en: 'Set default group failed: ' },
    promptNewSubGroup: { zh: '\u8f93\u5165\u65b0\u5b50\u7ec4\u540d\u79f0:', en: 'Enter new sub-group name:' },
    subgroupCreated: { zh: '\u5b50\u7ec4\u5df2\u521b\u5efa', en: 'Sub-group created' },
    createFailed: { zh: '\u521b\u5efa\u5931\u8d25: ', en: 'Create failed: ' },
    promptNewName: { zh: '\u8f93\u5165\u65b0\u540d\u79f0:', en: 'Enter new name:' },
    renamed: { zh: '\u5df2\u91cd\u547d\u540d', en: 'Renamed' },
    renameFailed: { zh: '\u91cd\u547d\u540d\u5931\u8d25: ', en: 'Rename failed: ' },
    loadUsersFailed: { zh: '\u52a0\u8f7d\u7528\u6237\u5931\u8d25: ', en: 'Load users failed: ' },
    groupDeleted: { zh: '\u7ec4\u5df2\u5220\u9664', en: 'Group deleted' },
    deleteFailed: { zh: '\u5220\u9664\u5931\u8d25: ', en: 'Delete failed: ' },
    selectOrEnterEmail: { zh: '\u8bf7\u9009\u62e9\u6216\u8f93\u5165\u90ae\u7bb1', en: 'Select or enter an email' },
    selectOrEnterUsers: { zh: '\u8bf7\u9009\u62e9\u7528\u6237\u6216\u8f93\u5165\u90ae\u7bb1', en: 'Select users or enter an email' },
    assignFailed: { zh: '\u5206\u914d\u5931\u8d25: ', en: 'Assign failed: ' },
    members: { zh: '\u6210\u5458', en: 'Members' },
    confirmRemoveUser: { zh: '\u786e\u5b9a\u79fb\u9664 {email} \u5417\uff1f', en: 'Remove {email}?' },
    confirmDeleteGroup: { zh: '\u786e\u5b9a\u5220\u9664\u7ec4 "{name}" \u5417\uff1f', en: 'Delete group "{name}"?' }
  };

  function st(key, vars) {
    var entry = SECURITY_I18N[key];
    var value = entry ? (isZh() ? entry.zh : entry.en) : key;
    if (!vars) return value;
    return value.replace(/\{(\w+)\}/g, function(match, name) {
      return Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match;
    });
  }

  function policyOptionLabel(policyKey, option) {
    var labels = {
      guardrail_mode: { none: text('\u65e0', 'None'), standard: text('\u6807\u51c6', 'Standard'), strict: text('\u4e25\u683c', 'Strict') },
      sandbox_mode: { none: text('\u65e0', 'None'), basic: text('\u57fa\u7840', 'Basic'), strict: text('\u4e25\u683c', 'Strict') },
      network_level: { none: text('\u65e0', 'None'), limited: text('\u53d7\u9650', 'Limited'), full: text('\u5b8c\u5168\u5f00\u653e', 'Full') }
    };
    return labels[policyKey] && labels[policyKey][option] ? labels[policyKey][option] : option;
  }

  function ui() {
    return global.AdminUI || null;
  }

  function hint(message) {
    const helper = ui();
    return helper && typeof helper.hint === 'function'
      ? helper.hint(message)
      : '<div class="hint">' + escapeHtml(message || '') + '</div>';
  }

  function errorHint(message) {
    return '<div class="hint" style="color:var(--danger)">' + escapeHtml(message || '') + '</div>';
  }

  function escapeJsString(value) {
    return String(value || '')
      .replace(/\\/g, '\\\\')
      .replace(/'/g, "\\'")
      .replace(/\r/g, '\\r')
      .replace(/\n/g, '\\n')
      .replace(/</g, '\\x3c')
      .replace(/>/g, '\\x3e')
      .replace(/&/g, '\\x26');
  }

  function normalizeEmailKey(email) {
    return String(email || '').trim().toLowerCase();
  }

  function dedupeEmails(emails) {
    var seen = {};
    var items = [];
    (emails || []).forEach(function(email) {
      var key = normalizeEmailKey(email);
      if (!key || seen[key]) return;
      seen[key] = true;
      items.push(String(email || '').trim());
    });
    return items;
  }

  function localizeUserStatus(status) {
    var value = String(status || '').trim().toLowerCase();
    if (!value) return st('unknown');
    var map = {
      active: 'statusActive',
      inactive: 'statusInactive',
      pending: 'statusPending',
      blocked: 'statusBlocked',
      disabled: 'statusDisabled',
      approved: 'statusApproved'
    };
    return map[value] ? st(map[value]) : String(status || '');
  }

  function dedupeUsersByEmail(users) {
    var seen = {};
    var items = [];
    (users || []).forEach(function(user) {
      if (!user) return;
      var key = normalizeEmailKey(user.email);
      if (!key || seen[key]) return;
      seen[key] = true;
      items.push(user);
    });
    return items;
  }

  function selectedAssignEmailList() {
    var selected = state().selectedAssignEmails || {};
    return Object.keys(selected).filter(function(key) { return !!selected[key]; }).map(function(key) { return selected[key]; });
  }

  function filteredAssignUsers() {
    var sec = state();
    var input = document.getElementById('assignUsersSearch');
    var query = String(input && input.value || '').trim().toLowerCase();
    return (sec.assignUsers || []).filter(function(user) {
      if (!query) return true;
      var email = String(user.email || '').toLowerCase();
      var sn = String(user.sn || '').toLowerCase();
      return email.indexOf(query) >= 0 || sn.indexOf(query) >= 0;
    });
  }

  function setAssignUserSelected(email, checked) {
    var sec = state();
    var clean = String(email || '').trim();
    var key = normalizeEmailKey(clean);
    if (!key) return;
    if (!sec.selectedAssignEmails) sec.selectedAssignEmails = {};
    if (checked) {
      sec.selectedAssignEmails[key] = clean;
    } else {
      delete sec.selectedAssignEmails[key];
    }
    sec.selectedAssignEmail = selectedAssignEmailList()[0] || '';
  }

  function renderMembersSection(children, members) {
    var sec = state();
    var pageSize = Number(sec.membersPageSize || 50);
    var totalMembers = members.length;
    var totalPages = Math.max(1, Math.ceil(totalMembers / pageSize));
    if (sec.membersPage > totalPages) sec.membersPage = totalPages;
    if (sec.membersPage < 1) sec.membersPage = 1;
    var start = (sec.membersPage - 1) * pageSize;
    var pageMembers = members.slice(start, start + pageSize);
    var html = '';
    if (children.length) {
      html += '<div style="margin-bottom:6px;font-size:11px;color:var(--muted)">' + st('subgroupLabel') + '</div>';
      html += '<div style="display:grid;gap:4px">';
      children.forEach(function(child) {
        html += '<div class="item" style="min-height:auto;padding:8px 10px;border-radius:10px;box-shadow:none">'
          + '<div style="display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;align-items:center">'
          + '<div style="font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(child.name) + '</div>'
          + '<div class="item-meta">' + String(Number(child.member_count || 0)) + '</div>'
          + '</div></div>';
      });
      html += '</div>';
    }
    if (totalMembers) {
      html += '<div style="margin:10px 0 6px;font-size:11px;color:var(--muted)">' + (st('membersLabel') + ' (' + totalMembers + ')') + '</div>';
      html += '<div style="display:grid;gap:4px" id="secMembersGrid">';
      pageMembers.forEach(function(email, idx) {
        var absoluteIndex = start + idx + 1;
        html += '<div class="item" style="min-height:auto;padding:8px 10px;border-radius:10px;box-shadow:none">';
        html += '<div style="display:grid;grid-template-columns:minmax(0,1.5fr) auto auto;gap:8px;align-items:center">';
        html += '<div style="min-width:0"><div style="font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(email) + '</div></div>';
        html += '<div class="item-meta" style="font-size:11px">#' + absoluteIndex + '</div>';
        html += '<button class="btn-ghost" style="height:26px;font-size:11px;padding:0 10px;color:var(--danger)" data-email="' + escapeHtml(email) + '" onclick="removeSecGroupMember(this.dataset.email)">' + st('remove') + '</button>';
        html += '</div></div>';
      });
      html += '</div>';
      if (totalPages > 1) {
        var startIdx = start + 1;
        var endIdx = Math.min(start + pageMembers.length, totalMembers);
        html += '<div class="pager" style="margin-top:8px"><div class="pager-meta">' + st('pagerSummary', { page: sec.membersPage, totalPages: totalPages, start: startIdx, end: endIdx, total: totalMembers }) + '</div><div class="pager-actions"><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="changeSecMembersPage(-1)"' + (sec.membersPage <= 1 ? ' disabled' : '') + '>' + st('previous') + '</button><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="changeSecMembersPage(1)"' + (sec.membersPage >= totalPages ? ' disabled' : '') + '>' + st('next') + '</button></div></div>';
      }
    }
    return html || hint(st('noMembers'));
  }

  function applySecurityI18n() {
    _s('navSecurity', 'textContent', secTr('navSecurity', '\u5b89\u5168\u7ba1\u7406', 'Security Management'));
    _s('navSecurityDesc', 'textContent', secTr('navSecurityDesc', '\u7528\u6237\u7ec4\u3001\u7b56\u7565\u4e0e\u7ec4\u7ec7\u67b6\u6784', 'Groups, policies, and organization structure'));
    _s('secTitle', 'textContent', secTr('securityTabTitle', '\u5b89\u5168\u7ba1\u7406', 'Security Management'));
    _s('secDesc', 'textContent', secTr('securityTabSubtitle', '\u7ba1\u7406\u7528\u6237\u7ec4\u3001\u7b56\u7565\u548c\u7ec4\u7ec7\u67b6\u6784\u3002', 'Manage groups, policies, and the organization structure.'));
    _s('secReloadBtn', 'textContent', st('reload'));
    _s('secCentralizedTitle', 'textContent', st('centralizedPolicy'));
    _s('secOrgTitle', 'textContent', st('orgStructure'));
    _s('secDefaultGroupTitle', 'textContent', st('defaultGroup'));
    _s('secDefaultGroupSetBtn', 'textContent', st('set'));
    _s('secGroupTreeTitle', 'textContent', st('groupTree'));
    _s('secCtxCreate', 'textContent', st('createSubDepartment'));
    _s('secCtxRename', 'textContent', st('renameDepartment'));
    _s('secCtxAssign', 'textContent', st('moveUsersHere'));
    _s('secCtxDelete', 'textContent', st('deleteDepartment'));
    _s('secPolicySaveBtn', 'textContent', st('save'));
    _s('secMembersTitle', 'textContent', st('members'));
    _s('secMembersReloadBtn', 'textContent', st('reload'));
    _s('defaultGroupModalTitle', 'textContent', st('defaultGroup'));
    _s('defaultGroupModalDesc', 'textContent', st('chooseDefaultGroupDesc'));
    _s('defaultGroupCancelBtn', 'textContent', st('cancel'));
    _s('defaultGroupConfirmBtn', 'textContent', st('confirm'));
    _s('assignUsersModalTitle', 'textContent', st('moveUsers'));
    _s('assignUsersModalDesc', 'textContent', st('moveUsersDesc'));
    _s('assignUsersCancelBtn', 'textContent', st('cancel'));
    _s('assignUsersConfirmBtn', 'textContent', st('confirm'));
    _s('assignUsersSelectAllBtn', 'textContent', st('selectVisibleUsers'));
    _s('assignUsersSearch', 'placeholder', st('searchEmailOrSn'));
    _s('secContextMenu', 'title', st('departmentActions'));
  }

  function normalizeNode(raw) {
    if (!raw) return null;
    return {
      id: raw.id || '',
      name: raw.name || '',
      parent_id: raw.parent_id || '',
      member_count: Number(raw.member_count || 0),
      has_children: !!raw.has_children || !!(raw.children && raw.children.length),
      children: (raw.children || []).map(normalizeNode).filter(Boolean)
    };
  }

  function findGroupNode(nodes, id) {
    if (!nodes || !nodes.length || !id) return null;
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].id === id) return nodes[i];
      var found = findGroupNode(nodes[i].children || [], id);
      if (found) return found;
    }
    return null;
  }

  function replaceGroupChildren(nodes, parentID, children) {
    var parent = findGroupNode(nodes, parentID);
    if (!parent) return false;
    var sec = state();
    var prevById = {};
    (parent.children || []).forEach(function(child) {
      prevById[child.id] = child;
    });
    parent.children = (children || []).map(function(child) {
      var normalized = normalizeNode(child);
      var prev = prevById[normalized.id];
      if (prev && prev.children && prev.children.length) {
        normalized.children = prev.children;
      }
      if (prev && sec.loadedChildrenGroupIds[normalized.id]) {
        sec.loadedChildrenGroupIds[normalized.id] = true;
      }
      if (prev && sec.expandedGroupIds[normalized.id]) {
        sec.expandedGroupIds[normalized.id] = true;
      }
      return normalized;
    });
    parent.has_children = parent.children.length > 0 || !!parent.has_children;
    return true;
  }

  function renderTreeNodes(nodes, container, depth) {
    var sec = state();
    if (!container) return;
    if (depth === 0) container.innerHTML = '';
    if (!nodes || !nodes.length) {
      if (depth === 0) container.innerHTML = hint(st('noGroups'));
      return;
    }
    nodes.forEach(function(node) {
      var row = document.createElement('div');
      row.style.padding = '3px 8px 3px ' + (depth * 16 + 8) + 'px';
      row.style.borderRadius = '8px';
      row.style.transition = 'background .15s';
      row.style.display = 'flex';
      row.style.alignItems = 'center';
      row.style.gap = '6px';
      row.style.cursor = 'pointer';
      if (node.id === sec.selectedGroupId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }

      var toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.style.width = '18px';
      toggle.style.minWidth = '18px';
      toggle.style.height = '18px';
      toggle.style.padding = '0';
      toggle.style.border = 'none';
      toggle.style.borderRadius = '6px';
      toggle.style.background = 'transparent';
      toggle.style.color = 'var(--muted)';
      toggle.style.boxShadow = 'none';
      toggle.style.transform = 'none';
      toggle.style.cursor = node.has_children ? 'pointer' : 'default';
      if (!node.has_children) {
        toggle.textContent = '\u25cf';
        toggle.disabled = true;
      } else if (sec.loadingChildrenGroupIds[node.id]) {
        toggle.textContent = '...';
      } else {
        toggle.textContent = sec.expandedGroupIds[node.id] ? '\u25bc' : '\u25b6';
      }
      toggle.addEventListener('click', function(event) {
        event.preventDefault();
        event.stopPropagation();
        if (node.has_children) global.toggleSecGroup(node.id);
      });

      var label = document.createElement('div');
      label.style.flex = '1';
      label.innerHTML = '<span style="font-size:12px;font-weight:600">' + escapeHtml(node.name) + '</span><span style="color:var(--muted);font-size:10px;margin-left:6px">(' + String(Number(node.member_count || 0)) + ')</span>'; 
      row.appendChild(toggle);
      row.appendChild(label);
      row.addEventListener('click', function(event) {
        event.stopPropagation();
        global.selectSecGroup(node.id, node.name);
      });
      row.addEventListener('contextmenu', function(event) {
        event.preventDefault();
        event.stopPropagation();
        sec.contextGroupId = node.id;
        sec.contextGroupName = node.name;
        global.showSecContextMenu(event.clientX, event.clientY);
      });
      container.appendChild(row);

      if (sec.expandedGroupIds[node.id]) {
        if (node.children && node.children.length) {
          renderTreeNodes(node.children, container, depth + 1);
        } else if (sec.loadingChildrenGroupIds[node.id]) {
          var loading = document.createElement('div');
          loading.style.padding = '2px 8px 6px ' + ((depth + 1) * 18 + 8) + 'px';
          loading.style.color = 'var(--muted)';
          loading.style.fontSize = '12px';
          loading.textContent = st('loading');
          container.appendChild(loading);
        }
      }
    });
  }

  async function loadSettings() {
    var settings = await api('/api/admin/security/settings');
    var centralizedToggle = document.getElementById('secCentralizedToggle');
    var orgToggle = document.getElementById('secOrgToggle');
    if (centralizedToggle) centralizedToggle.checked = !!settings.centralized_security_enabled;
    if (orgToggle) orgToggle.checked = !!settings.org_structure_enabled;
    _s('secCentralizedHint', 'textContent', settings.centralized_security_enabled ? st('enabled') : st('disabled'));
    _s('secOrgHint', 'textContent', settings.org_structure_enabled ? st('enabled') : st('disabled'));
    var dgHint = settings.default_group_id || st('notSet');
    _s('secDefaultGroupHint', 'textContent', st('defaultGroupPrefix') + dgHint);
  }

  async function loadGroups() {
    var sec = state();
    var data = await api('/api/admin/security/groups/root');
    var root = normalizeNode(data.root);
    sec.groupTree = root ? [root] : [];
    sec.expandedGroupIds = {};
    sec.loadedChildrenGroupIds = {};
    sec.loadingChildrenGroupIds = {};
    if (root) {
      sec.expandedGroupIds[root.id] = true;
      await global.loadSecGroupChildren(root.id, true);
      if (!sec.selectedGroupId) {
        global.selectSecGroup(root.id, root.name);
        return;
      }
    }
    global._secGroupTree = sec.groupTree;
    global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
  }

  function renderAssignUsers() {
    var sec = state();
    var root = document.getElementById('assignUsersTree');
    var input = document.getElementById('assignUsersSearch');
    if (!root || !input) return;
    var query = String(input.value || '').trim().toLowerCase();
    var rows = filteredAssignUsers();
    if (!rows.length) {
      root.innerHTML = hint(query ? st('noUsersMatchSearch') : st('noUsersAvailable'));
    } else {
      root.innerHTML = rows.map(function(user) {
        var email = user.email || '';
        var key = normalizeEmailKey(email);
        var selected = !!(sec.selectedAssignEmails && sec.selectedAssignEmails[key]);
        var jsEmail = escapeJsString(email);
        return '<div class="item" style="min-height:auto;padding:8px 10px;margin-bottom:6px;border:' + (selected ? '1px solid rgba(47,128,237,.38)' : '1px solid var(--line)') + ';background:' + (selected ? 'rgba(47,128,237,.06)' : 'linear-gradient(180deg,rgba(255,255,255,.98) 0%,rgba(247,251,255,.98) 100%)') + ';cursor:pointer" onclick="selectAssignUser(\'' + jsEmail + '\')"><div style="display:flex;align-items:center;justify-content:space-between;gap:8px"><label style="display:flex;align-items:center;gap:8px;margin:0;min-width:0;cursor:pointer;flex:1" onclick="event.stopPropagation()"><input type="checkbox" style="width:16px;height:16px;flex:0 0 auto" ' + (selected ? 'checked' : '') + ' onchange="toggleAssignUser(\'' + jsEmail + '\', this.checked)"><span style="min-width:0"><span style="display:block;font-weight:600;word-break:break-all">' + escapeHtml(email) + '</span><span class="item-meta">' + escapeHtml(text('SN', 'SN')) + ': ' + escapeHtml(user.sn || '-') + ' | ' + escapeHtml(st('status')) + ': ' + escapeHtml(localizeUserStatus(user.status)) + '</span></span></label><button class="btn-ghost" type="button" style="height:26px;font-size:11px;padding:0 10px;flex:0 0 auto" onclick="event.stopPropagation();selectAssignUser(\'' + jsEmail + '\')">' + escapeHtml(selected ? st('remove') : st('move')) + '</button></div></div>';
      }).join('');
    }
    var selectedCount = selectedAssignEmailList().length;
    var visibleSelectedCount = rows.filter(function(user) { return !!(sec.selectedAssignEmails && sec.selectedAssignEmails[normalizeEmailKey(user.email)]); }).length;
    var countText = st('showingUsers', { visible: rows.length, total: sec.assignUsers.length });
    if (selectedCount) countText += ' | ' + st('selectedUsers', { count: selectedCount });
    _s('assignUsersCount', 'textContent', countText);
    var selectAllBtn = document.getElementById('assignUsersSelectAllBtn');
    if (selectAllBtn) {
      selectAllBtn.textContent = rows.length > 0 && visibleSelectedCount === rows.length ? st('clearVisibleUsers') : st('selectVisibleUsers');
      selectAllBtn.disabled = rows.length === 0;
    }
  }

  async function loadAssignableUsers() {
    var sec = state();
    var userReq = api('/api/admin/users');
    var memberReq = sec.assignGroupId
      ? api('/api/admin/security/groups/' + encodeURIComponent(sec.assignGroupId) + '/members')
      : Promise.resolve({ members: [] });
    var results = await Promise.all([userReq, memberReq]);
    var data = results[0] || {};
    var memberData = results[1] || {};
    var currentMemberKeys = {};
    dedupeEmails(memberData.members || []).forEach(function(email) {
      currentMemberKeys[normalizeEmailKey(email)] = true;
    });
    sec.assignUsers = dedupeUsersByEmail((data.users || []).filter(function(user) {
      return !!(user && user.email) && !currentMemberKeys[normalizeEmailKey(user.email)];
    }));
    renderAssignUsers();
  }

  global.loadSecurityTab = async function loadSecurityTab() {
    applySecurityI18n();
    try {
      await loadSettings();
    } catch (err) {
      showToast(st('loadSecuritySettingsFailed') + err.message, 'error');
    }
    try {
      await loadGroups();
    } catch (err) {
      showToast(st('loadGroupTreeFailed') + err.message, 'error');
    }
  };

  global.renderSecGroupTree = function renderSecGroupTree(nodes, container, depth) {
    renderTreeNodes(nodes, container, depth || 0);
  };

  global.loadSecGroupChildren = async function loadSecGroupChildren(groupID, silent) {
    var sec = state();
    if (!groupID) return [];
    sec.loadingChildrenGroupIds[groupID] = true;
    global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    try {
      var data = await api('/api/admin/security/groups/' + encodeURIComponent(groupID) + '/members');
      replaceGroupChildren(sec.groupTree, groupID, data.children || []);
      sec.loadedChildrenGroupIds[groupID] = true;
      global._secGroupTree = sec.groupTree;
      if (!silent) global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
      return data.children || [];
    } finally {
      delete sec.loadingChildrenGroupIds[groupID];
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    }
  };

  global.toggleSecGroup = async function toggleSecGroup(groupID) {
    var sec = state();
    if (!groupID) return;
    sec.expandedGroupIds[groupID] = !sec.expandedGroupIds[groupID];
    if (sec.expandedGroupIds[groupID] && !sec.loadedChildrenGroupIds[groupID]) {
      try {
        await global.loadSecGroupChildren(groupID);
      } catch (err) {
        showToast(st('loadChildGroupsFailed') + err.message, 'error');
      }
    } else {
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    }
  };

  global.showSecContextMenu = function showSecContextMenu(x, y) {
    var sec = state();
    var menu = document.getElementById('secContextMenu');
    if (!menu) return;
    if (sec.contextMenuHideHandler) {
      document.removeEventListener('click', sec.contextMenuHideHandler);
      document.removeEventListener('contextmenu', sec.contextMenuHideHandler);
      sec.contextMenuHideHandler = null;
    }
    if (menu.parentElement !== document.body) document.body.appendChild(menu);
    menu.classList.remove('hidden');
    menu.style.left = '0px';
    menu.style.top = '0px';
    var margin = 8;
    var rect = menu.getBoundingClientRect();
    var maxLeft = Math.max(margin, global.innerWidth - rect.width - margin);
    var maxTop = Math.max(margin, global.innerHeight - rect.height - margin);
    var left = Math.min(Math.max(margin, Number(x || 0) + 2), maxLeft);
    var top = Math.min(Math.max(margin, Number(y || 0) + 2), maxTop);
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
    function hide() {
      menu.classList.add('hidden');
      document.removeEventListener('click', hide);
      document.removeEventListener('contextmenu', hide);
      if (state().contextMenuHideHandler === hide) state().contextMenuHideHandler = null;
    }
    sec.contextMenuHideHandler = hide;
    setTimeout(function() {
      document.addEventListener('click', hide);
      document.addEventListener('contextmenu', hide);
    }, 0);
  };

  global.selectSecGroup = function selectSecGroup(id, name) {
    var sec = state();
    sec.selectedGroupId = id;
    sec.selectedGroupName = name;
    global._secSelectedGroupId = id;
    global._secSelectedGroupName = name;
    if (sec.groupTree) global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    _s('secPolicyTitle', 'textContent', st('policyPrefix') + name);
    _s('secPolicySubtitle', 'textContent', st('groupIdPrefix') + id);
    var policyActions = document.getElementById('secPolicyActions');
    var groupMembers = document.getElementById('secGroupMembers');
    if (policyActions) policyActions.classList.remove('hidden');
    if (groupMembers) groupMembers.classList.remove('hidden');
    global.loadSecGroupPolicy(id);
    global.loadSecGroupMembers();
  };

  global.loadSecGroupPolicy = async function loadSecGroupPolicy(groupId) {
    var panel = document.getElementById('secPolicyPanel');
    if (!panel) return;
    try {
      var view = await api('/api/admin/security/groups/' + encodeURIComponent(groupId) + '/policy');
      state().policyCache = view;
      global._secPolicyCache = view;
      var items = view.items || {};
      var policyKeys = [
        { key: 'file_outbound_enabled', label: st('fileOutbound'), type: 'bool' },
        { key: 'image_outbound_enabled', label: st('imageOutbound'), type: 'bool' },
        { key: 'gossip_enabled', label: st('gossip'), type: 'bool' },
        { key: 'yolo_mode_allowed', label: st('yoloMode'), type: 'bool' },
        { key: 'smart_route_enabled', label: st('smartRoute'), type: 'bool' },
        { key: 'guardrail_mode', label: st('guardrailMode'), type: 'select', options: ['none', 'standard', 'strict'] },
        { key: 'sandbox_mode', label: st('sandboxMode'), type: 'select', options: ['none', 'basic', 'strict'] },
        { key: 'network_level', label: st('networkLevel'), type: 'select', options: ['none', 'limited', 'full'] }
      ];
      var html = '';
      policyKeys.forEach(function(pk) {
        var item = items[pk.key] || {};
        var value = item.value;
        var source = item.source || 'inherited';
        var sourceName = item.source_name || '';
        var sourceTag = source === 'self'
          ? '<span style="color:var(--accent);font-size:11px;margin-left:6px">' + st('custom') + '</span>'
          : '<span style="color:var(--muted);font-size:11px;margin-left:6px">' + st('inheritedFrom') + escapeHtml(sourceName) + '</span>';
        html += '<div style="display:grid;grid-template-columns:minmax(160px,1.2fr) auto;gap:8px;align-items:center;padding:7px 0;border-bottom:1px solid var(--line)">';
        html += '<div style="font-size:12px;font-weight:600">' + escapeHtml(pk.label) + sourceTag + '</div>'; 
        if (pk.type === 'bool') {
          html += '<label style="cursor:pointer;justify-self:end"><input type="checkbox" data-policy-key="' + pk.key + '" data-policy-type="bool" ' + (value ? 'checked' : '') + '></label>'; 
        } else {
          html += '<select data-policy-key="' + pk.key + '" data-policy-type="select" style="font-size:11px;padding:2px 8px;border-radius:6px;border:1px solid var(--line)">';
          pk.options.forEach(function(option) {
            html += '<option value="' + option + '"' + (value === option ? ' selected' : '') + '>' + escapeHtml(policyOptionLabel(pk.key, option)) + '</option>';
          });
          html += '</select>';
        }
        html += '</div>';
      });
      panel.innerHTML = html;
    } catch (err) {
      panel.innerHTML = errorHint(err.message);
    }
  };

  global.saveSecPolicy = async function saveSecPolicy() {
    var sec = state();
    if (!sec.selectedGroupId) {
      showToast(st('selectGroupFirst'), 'info');
      return;
    }
    var policy = {};
    document.querySelectorAll('#secPolicyPanel [data-policy-key]').forEach(function(el) {
      var key = el.dataset.policyKey;
      policy[key] = el.dataset.policyType === 'bool' ? el.checked : el.value;
    });
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/policy', { method: 'PUT', body: JSON.stringify({ policy: policy }) });
      showToast(st('policySaved'), 'success');
      global.loadSecGroupPolicy(sec.selectedGroupId);
    } catch (err) {
      showToast(st('savePolicyFailed') + err.message, 'error');
    }
  };

  global.loadSecGroupMembers = async function loadSecGroupMembers() {
    var sec = state();
    if (!sec.selectedGroupId) return;
    var container = document.getElementById('secMembersList');
    if (!container) return;
    container.innerHTML = hint(st('loadingMembers'));
    try {
      var data = await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members');
      replaceGroupChildren(sec.groupTree, sec.selectedGroupId, data.children || []);
      sec.loadedChildrenGroupIds[sec.selectedGroupId] = true;
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
      var members = dedupeEmails(data.members || []);
      var children = data.children || [];
      sec.membersCache = members.slice();
      sec.membersPage = 1;
      container.innerHTML = renderMembersSection(children, members);
    } catch (err) {
      container.innerHTML = errorHint(err.message);
    }
  };

  global.changeSecMembersPage = function changeSecMembersPage(step) {
    var sec = state();
    if (!sec.selectedGroupId) return;
    var total = (sec.membersCache || []).length;
    var pageSize = Number(sec.membersPageSize || 50);
    var totalPages = Math.max(1, Math.ceil(total / pageSize));
    sec.membersPage = Math.max(1, Math.min(totalPages, Number(sec.membersPage || 1) + Number(step || 0)));
    var container = document.getElementById('secMembersList');
    if (!container) return;
    var group = findGroupNode(sec.groupTree, sec.selectedGroupId);
    var children = group && group.children ? group.children : [];
    container.innerHTML = renderMembersSection(children, sec.membersCache || []);
  };

  global.removeSecGroupMember = async function removeSecGroupMember(email) {
    var sec = state();
    if (!sec.selectedGroupId) return;
    if (!confirm(st('confirmRemoveUser', { email: email }))) return;
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members/' + encodeURIComponent(email), { method: 'DELETE' });
      showToast(st('removed'), 'success');
      global.loadSecGroupMembers();
      global.loadSecurityTab();
    } catch (err) {
      showToast(st('removeFailed') + err.message, 'error');
    }
  };

  global.toggleSecCentralized = async function toggleSecCentralized(enabled) {
    try {
      var settings = await api('/api/admin/security/settings');
      settings.centralized_security_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? st('centralizedEnabled') : st('centralizedDisabled'), 'success');
      _s('secCentralizedHint', 'textContent', enabled ? st('enabled') : st('disabled'));
    } catch (err) {
      showToast(st('updateFailed') + err.message, 'error');
      var toggle = document.getElementById('secCentralizedToggle');
      if (toggle) toggle.checked = !enabled;
    }
  };

  global.toggleSecOrg = async function toggleSecOrg(enabled) {
    try {
      var settings = await api('/api/admin/security/settings');
      settings.org_structure_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? st('orgEnabled') : st('orgDisabled'), 'success');
      _s('secOrgHint', 'textContent', enabled ? st('enabled') : st('disabled'));
    } catch (err) {
      showToast(st('updateFailed') + err.message, 'error');
      var toggle = document.getElementById('secOrgToggle');
      if (toggle) toggle.checked = !enabled;
    }
  };

  global.closeDefaultGroupModal = function closeDefaultGroupModal() {
    var overlay = document.getElementById('defaultGroupModalOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.showSetDefaultGroup = async function showSetDefaultGroup() {
    var sec = state();
    try {
      var data = await api('/api/admin/security/groups');
      var root = normalizeNode(data.tree);
      sec.defaultGroupTree = root ? [root] : [];
      sec.defaultGroupPickedId = null;
      global._secDefaultGroupPickedId = null;
      var picker = document.getElementById('defaultGroupTreePicker');
      var overlay = document.getElementById('defaultGroupModalOverlay');
      global.renderDefaultGroupPicker(sec.defaultGroupTree, picker, 0);
      if (overlay) overlay.classList.add('show');
    } catch (err) {
      showToast(st('loadGroupTreeFailed') + err.message, 'error');
    }
  };

  global.renderDefaultGroupPicker = function renderDefaultGroupPicker(nodes, container, depth) {
    var sec = state();
    if (!container) return;
    if (depth === 0) container.innerHTML = '';
    if (!nodes || !nodes.length) return;
    nodes.forEach(function(node) {
      var row = document.createElement('div');
      row.style.paddingLeft = (depth * 16 + 8) + 'px';
      row.style.padding = '4px 8px 4px ' + (depth * 16 + 8) + 'px';
      row.style.cursor = 'pointer';
      row.style.borderRadius = '6px';
      row.style.transition = 'background .15s';
      if (node.id === sec.defaultGroupPickedId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }
      row.textContent = ((node.children && node.children.length) ? '\u25bc ' : '\u25cf ') + node.name;
      row.addEventListener('click', function() {
        sec.defaultGroupPickedId = node.id;
        global._secDefaultGroupPickedId = node.id;
        global.renderDefaultGroupPicker(sec.defaultGroupTree, container, 0);
      });
      container.appendChild(row);
      if (node.children && node.children.length) global.renderDefaultGroupPicker(node.children, container, depth + 1);
    });
  };

  global.confirmDefaultGroup = async function confirmDefaultGroup() {
    var sec = state();
    if (!sec.defaultGroupPickedId) {
      showToast(st('pleaseSelectGroup'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/settings/default-group', { method: 'PUT', body: JSON.stringify({ group_id: sec.defaultGroupPickedId }) });
      showToast(st('defaultGroupSet'), 'success');
      global.closeDefaultGroupModal();
      global.loadSecurityTab();
    } catch (err) {
      showToast(st('setDefaultGroupFailed') + err.message, 'error');
    }
  };

  global.closeAssignUsersModal = function closeAssignUsersModal() {
    var sec = state();
    sec.selectedAssignEmail = '';
    sec.selectedAssignEmails = {};
    sec.assignGroupId = null;
    sec.contextGroupName = null;
    var overlay = document.getElementById('assignUsersModalOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.selectAssignUser = function selectAssignUser(email) {
    var key = normalizeEmailKey(email);
    var selected = !!(state().selectedAssignEmails && state().selectedAssignEmails[key]);
    setAssignUserSelected(email, !selected);
    renderAssignUsers();
  };

  global.toggleAssignUser = function toggleAssignUser(email, checked) {
    setAssignUserSelected(email, checked);
    renderAssignUsers();
  };

  global.toggleAssignVisibleUsers = function toggleAssignVisibleUsers() {
    var rows = filteredAssignUsers();
    if (!rows.length) return;
    var sec = state();
    var allSelected = rows.every(function(user) {
      return !!(sec.selectedAssignEmails && sec.selectedAssignEmails[normalizeEmailKey(user.email)]);
    });
    rows.forEach(function(user) {
      setAssignUserSelected(user.email, !allSelected);
    });
    renderAssignUsers();
  };

  global.secCtxAction = function secCtxAction(action) {
    var sec = state();
    var menu = document.getElementById('secContextMenu');
    if (menu) menu.classList.add('hidden');
    var groupID = sec.contextGroupId;
    var groupName = sec.contextGroupName;
    if (!groupID) return;
    if (action === 'create') {
      var name = prompt(st('promptNewSubGroup'));
      if (!name) return;
      api('/api/admin/security/groups', { method: 'POST', body: JSON.stringify({ name: name, parent_id: groupID }) })
        .then(function() { showToast(st('subgroupCreated'), 'success'); global.loadSecurityTab(); })
        .catch(function(err) { showToast(st('createFailed') + err.message, 'error'); });
      return;
    }
    if (action === 'rename') {
      var newName = prompt(st('promptNewName'), groupName);
      if (!newName) return;
      api('/api/admin/security/groups/' + encodeURIComponent(groupID), { method: 'PUT', body: JSON.stringify({ name: newName }) })
        .then(function() {
          showToast(st('renamed'), 'success');
          if (state().selectedGroupId === groupID) state().selectedGroupName = newName;
          global.loadSecurityTab();
        })
        .catch(function(err) { showToast(st('renameFailed') + err.message, 'error'); });
      return;
    }
    if (action === 'assign') {
      sec.assignGroupId = groupID;
      sec.contextGroupName = groupName;
      sec.selectedAssignEmail = '';
      sec.selectedAssignEmails = {};
      global._secAssignGroupId = groupID;
      _s('assignUsersModalTitle', 'textContent', st('assignTitleWithGroup', { name: groupName }));
      _s('assignUsersSearch', 'value', '');
      _s('assignUsersCount', 'textContent', st('loading'));
      var assignTree = document.getElementById('assignUsersTree');
      var assignOverlay = document.getElementById('assignUsersModalOverlay');
      var assignSearch = document.getElementById('assignUsersSearch');
      if (assignTree) assignTree.innerHTML = hint(st('loadingUsers'));
      if (assignOverlay) assignOverlay.classList.add('show');
      if (assignSearch && typeof assignSearch.focus === 'function') assignSearch.focus();
      loadAssignableUsers().catch(function(err) {
        var tree = document.getElementById('assignUsersTree');
        if (tree) tree.innerHTML = errorHint(err.message);
        showToast(st('loadUsersFailed') + err.message, 'error');
      });
      return;
    }
    if (action === 'delete') {
      if (!confirm(st('confirmDeleteGroup', { name: groupName }))) return;
      api('/api/admin/security/groups/' + encodeURIComponent(groupID), { method: 'DELETE' })
        .then(function() {
          showToast(st('groupDeleted'), 'success');
          if (state().selectedGroupId === groupID) {
            state().selectedGroupId = null;
            state().selectedGroupName = null;
            global._secSelectedGroupId = null;
            global._secSelectedGroupName = null;
          }
          global.loadSecurityTab();
        })
        .catch(function(err) { showToast(st('deleteFailed') + err.message, 'error'); });
    }
  };

  global.filterAssignUsers = function filterAssignUsers() {
    var input = document.getElementById('assignUsersSearch');
    state().selectedAssignEmail = String(input && input.value || '').trim();
    renderAssignUsers();
  };

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'security',
      title: function() { return secTr('securityTabTitle', '\u5b89\u5168\u7ba1\u7406', 'Security Management'); },
      subtitle: function() { return secTr('securityTabSubtitle', '\u7ba1\u7406\u7528\u6237\u7ec4\u3001\u7b56\u7565\u548c\u7ec4\u7ec7\u67b6\u6784\u3002', 'Manage groups, policies, and the organization structure.'); }
    });
  }

  applySecurityI18n();
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() {
      var sec = state();
      applySecurityI18n();
      if (sec.assignGroupId && sec.contextGroupName) {
        _s('assignUsersModalTitle', 'textContent', st('assignTitleWithGroup', { name: sec.contextGroupName }));
      }
      renderAssignUsers();
      var membersPanel = document.getElementById('secGroupMembers');
      if (sec.selectedGroupId && membersPanel && !membersPanel.classList.contains('hidden')) {
        var container = document.getElementById('secMembersList');
        if (container) {
          var group = findGroupNode(sec.groupTree, sec.selectedGroupId);
          var children = group && group.children ? group.children : [];
          container.innerHTML = renderMembersSection(children, sec.membersCache || []);
        }
      }
    });
  }

  global.confirmAssignUsers = async function confirmAssignUsers() {
    var sec = state();
    var input = document.getElementById('assignUsersSearch');
    var typedEmail = String(input && input.value || '').trim();
    var emails = selectedAssignEmailList();
    if (!emails.length && typedEmail) emails = [typedEmail];
    emails = dedupeEmails(emails);
    if (!emails.length || !sec.assignGroupId) {
      showToast(st('selectOrEnterUsers'), 'info');
      return;
    }
    try {
      for (var i = 0; i < emails.length; i += 1) {
        await api('/api/admin/security/groups/' + encodeURIComponent(sec.assignGroupId) + '/members', { method: 'POST', body: JSON.stringify({ email: emails[i] }) });
      }
      var targetGroupId = sec.assignGroupId;
      var targetGroupName = sec.contextGroupName;
      showToast(st('userMoved'), 'success');
      global.closeAssignUsersModal();
      sec.selectedGroupId = targetGroupId;
      sec.selectedGroupName = targetGroupName;
      global._secSelectedGroupId = targetGroupId;
      global._secSelectedGroupName = targetGroupName;
      await global.loadSecurityTab();
      if (targetGroupId) global.selectSecGroup(targetGroupId, targetGroupName || targetGroupId);
    } catch (err) {
      showToast(st('assignFailed') + err.message, 'error');
    }
  };
})(window);
