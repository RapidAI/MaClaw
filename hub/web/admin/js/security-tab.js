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
        defaultGroupPickedId: null
      };
    }
    return global.__securityAdminState;
  }

  function isZh() {
    return global.currentLang === 'zh';
  }

  function text(zh, en) {
    return isZh() ? zh : en;
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

  function renderTreeNodes(nodes, container, depth) {
    const sec = state();
    if (!container) return;
    if (depth === 0) container.innerHTML = '';
    if (!nodes || !nodes.length) {
      if (depth === 0) container.innerHTML = hint(text('\u65e0\u7ec4\u7ec7\u6570\u636e', 'No groups'));
      return;
    }
    nodes.forEach(function(node) {
      const row = document.createElement('div');
      row.style.paddingLeft = (depth * 18) + 'px';
      row.style.cursor = 'pointer';
      row.style.padding = '4px 8px 4px ' + (depth * 18 + 8) + 'px';
      row.style.borderRadius = '6px';
      row.style.transition = 'background .15s';
      if (node.id === sec.selectedGroupId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }
      const icon = node.children && node.children.length ? '\u25bc ' : '\u25cf ';
      const countBadge = typeof node.member_count === 'number'
        ? ' <span style="color:var(--muted);font-size:11px">(' + node.member_count + ')</span>'
        : '';
      row.innerHTML = icon + escapeHtml(node.name) + countBadge;
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
      if (node.children && node.children.length) renderTreeNodes(node.children, container, depth + 1);
    });
  }

  async function loadSettings() {
    const settings = await api('/api/admin/security/settings');
    document.getElementById('secCentralizedToggle').checked = !!settings.centralized_security_enabled;
    document.getElementById('secOrgToggle').checked = !!settings.org_structure_enabled;
    _s('secCentralizedHint', 'textContent', settings.centralized_security_enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    _s('secOrgHint', 'textContent', settings.org_structure_enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    const dgHint = settings.default_group_id || text('\u672a\u8bbe\u7f6e', 'Not set');
    _s('secDefaultGroupHint', 'textContent', text('\u9ed8\u8ba4\u7ec4: ', 'Default group: ') + dgHint);
  }

  async function loadGroups() {
    const sec = state();
    const treeData = await api('/api/admin/security/groups');
    sec.groupTree = treeData.tree || [];
    global._secGroupTree = sec.groupTree;
    global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    if (sec.groupTree.length > 0 && !sec.selectedGroupId) {
      global.selectSecGroup(sec.groupTree[0].id, sec.groupTree[0].name);
    }
  }

  global.loadSecurityTab = async function loadSecurityTab() {
    try {
      await loadSettings();
    } catch (err) {
      showToast(text('\u52a0\u8f7d\u5b89\u5168\u8bbe\u7f6e\u5931\u8d25: ', 'Load security settings failed: ') + err.message, 'error');
    }
    try {
      await loadGroups();
    } catch (err) {
      showToast(text('\u52a0\u8f7d\u7ec4\u7ec7\u6811\u5931\u8d25: ', 'Load group tree failed: ') + err.message, 'error');
    }
  };

  global.renderSecGroupTree = function renderSecGroupTree(nodes, container, depth) {
    renderTreeNodes(nodes, container, depth || 0);
  };

  global.showSecContextMenu = function showSecContextMenu(x, y) {
    const menu = document.getElementById('secContextMenu');
    if (!menu) return;
    menu.classList.remove('hidden');
    menu.style.left = x + 'px';
    menu.style.top = y + 'px';
    function hide() {
      menu.classList.add('hidden');
      document.removeEventListener('click', hide);
    }
    setTimeout(function() { document.addEventListener('click', hide); }, 0);
  };

  global.selectSecGroup = function selectSecGroup(id, name) {
    const sec = state();
    sec.selectedGroupId = id;
    sec.selectedGroupName = name;
    global._secSelectedGroupId = id;
    global._secSelectedGroupName = name;
    if (sec.groupTree) global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    _s('secPolicyTitle', 'textContent', text('\u7b56\u7565: ', 'Policy: ') + name);
    _s('secPolicySubtitle', 'textContent', text('\u7ec4 ID: ', 'Group ID: ') + id);
    document.getElementById('secPolicyActions').classList.remove('hidden');
    document.getElementById('secGroupMembers').classList.remove('hidden');
    global.loadSecGroupPolicy(id);
    global.loadSecGroupMembers();
  };

  global.loadSecGroupPolicy = async function loadSecGroupPolicy(groupId) {
    const panel = document.getElementById('secPolicyPanel');
    if (!panel) return;
    try {
      const view = await api('/api/admin/security/groups/' + encodeURIComponent(groupId) + '/policy');
      state().policyCache = view;
      global._secPolicyCache = view;
      const items = view.items || {};
      const policyKeys = [
        { key: 'file_outbound_enabled', label: text('\u6587\u4ef6\u5916\u53d1', 'File Outbound'), type: 'bool' },
        { key: 'image_outbound_enabled', label: text('\u56fe\u7247\u5916\u53d1', 'Image Outbound'), type: 'bool' },
        { key: 'gossip_enabled', label: text('Gossip \u529f\u80fd', 'Gossip'), type: 'bool' },
        { key: 'yolo_mode_allowed', label: text('YOLO \u6a21\u5f0f', 'YOLO Mode'), type: 'bool' },
        { key: 'smart_route_enabled', label: text('\u667a\u80fd\u8def\u7531', 'Smart Route'), type: 'bool' },
        { key: 'guardrail_mode', label: text('\u62a4\u680f\u6a21\u5f0f', 'Guardrail Mode'), type: 'select', options: ['none', 'standard', 'strict'] },
        { key: 'sandbox_mode', label: text('\u6c99\u7bb1\u6a21\u5f0f', 'Sandbox Mode'), type: 'select', options: ['none', 'basic', 'strict'] },
        { key: 'network_level', label: text('\u7f51\u7edc\u7ea7\u522b', 'Network Level'), type: 'select', options: ['none', 'limited', 'full'] }
      ];
      let html = '';
      policyKeys.forEach(function(pk) {
        const item = items[pk.key] || {};
        const value = item.value;
        const source = item.source || 'inherited';
        const sourceName = item.source_name || '';
        const sourceTag = source === 'self'
          ? '<span style="color:var(--accent);font-size:11px;margin-left:6px">' + text('\u81ea\u5b9a\u4e49', 'self') + '</span>'
          : '<span style="color:var(--muted);font-size:11px;margin-left:6px">' + text('\u7ee7\u627f\u81ea ', 'from ') + escapeHtml(sourceName) + '</span>';
        html += '<div style="display:flex;align-items:center;justify-content:space-between;padding:6px 0;border-bottom:1px solid var(--line)">';
        html += '<div>' + escapeHtml(pk.label) + sourceTag + '</div>';
        if (pk.type === 'bool') {
          html += '<label style="cursor:pointer"><input type="checkbox" data-policy-key="' + pk.key + '" data-policy-type="bool" ' + (value ? 'checked' : '') + '></label>';
        } else {
          html += '<select data-policy-key="' + pk.key + '" data-policy-type="select" style="font-size:13px;padding:2px 8px;border-radius:6px;border:1px solid var(--line)">';
          pk.options.forEach(function(option) {
            html += '<option value="' + option + '"' + (value === option ? ' selected' : '') + '>' + option + '</option>';
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
    const sec = state();
    if (!sec.selectedGroupId) {
      showToast(text('\u8bf7\u5148\u9009\u62e9\u4e00\u4e2a\u7ec4', 'Select a group first'), 'info');
      return;
    }
    const policy = {};
    document.querySelectorAll('#secPolicyPanel [data-policy-key]').forEach(function(el) {
      const key = el.dataset.policyKey;
      policy[key] = el.dataset.policyType === 'bool' ? el.checked : el.value;
    });
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/policy', { method: 'PUT', body: JSON.stringify({ policy: policy }) });
      showToast(text('\u7b56\u7565\u5df2\u4fdd\u5b58', 'Policy saved'), 'success');
      global.loadSecGroupPolicy(sec.selectedGroupId);
    } catch (err) {
      showToast(text('\u4fdd\u5b58\u7b56\u7565\u5931\u8d25: ', 'Save policy failed: ') + err.message, 'error');
    }
  };

  global.loadSecGroupMembers = async function loadSecGroupMembers() {
    const sec = state();
    if (!sec.selectedGroupId) return;
    const container = document.getElementById('secMembersList');
    if (!container) return;
    try {
      const data = await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members');
      const members = data.members || [];
      const children = data.children || [];
      let html = '';
      if (children.length) {
        html += '<div style="margin-bottom:8px;font-size:12px;color:var(--muted)">' + text('\u5b50\u7ec4:', 'Sub-groups:') + '</div>';
        children.forEach(function(child) {
          html += '<div class="item" style="min-height:auto;padding:8px 12px;margin-bottom:4px"><span style="font-weight:600">\ud83d\udcc1 ' + escapeHtml(child.name) + '</span><span style="color:var(--muted);font-size:11px;margin-left:8px">(' + (child.member_count || 0) + ')</span></div>';
        });
      }
      if (members.length) {
        html += '<div style="margin-bottom:8px;font-size:12px;color:var(--muted)">' + (isZh() ? ('\u6210\u5458 (' + members.length + '):') : ('Members (' + members.length + '):')) + '</div>';
        members.forEach(function(email) {
          html += '<div class="item" style="min-height:auto;padding:6px 12px;margin-bottom:2px;display:flex;align-items:center;justify-content:space-between">';
          html += '<span>' + escapeHtml(email) + '</span>';
          html += '<button class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px;color:var(--danger)" onclick="removeSecGroupMember(\'' + escapeHtml(email).replace(/'/g, "\\'") + '\')">' + text('\u79fb\u9664', 'Remove') + '</button>';
          html += '</div>';
        });
      }
      container.innerHTML = html || hint(text('\u65e0\u6210\u5458', 'No members'));
    } catch (err) {
      container.innerHTML = errorHint(err.message);
    }
  };

  global.removeSecGroupMember = async function removeSecGroupMember(email) {
    const sec = state();
    if (!sec.selectedGroupId) return;
    if (!confirm(isZh() ? ('\u786e\u5b9a\u79fb\u9664 ' + email + ' \u5417\uff1f') : ('Remove ' + email + '?'))) return;
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members/' + encodeURIComponent(email), { method: 'DELETE' });
      showToast(text('\u5df2\u79fb\u9664', 'Removed'), 'success');
      global.loadSecGroupMembers();
      global.loadSecurityTab();
    } catch (err) {
      showToast(text('\u79fb\u9664\u5931\u8d25: ', 'Remove failed: ') + err.message, 'error');
    }
  };

  global.toggleSecCentralized = async function toggleSecCentralized(enabled) {
    try {
      const settings = await api('/api/admin/security/settings');
      settings.centralized_security_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? text('\u96c6\u4e2d\u7b56\u7565\u5df2\u542f\u7528', 'Centralized policy enabled') : text('\u96c6\u4e2d\u7b56\u7565\u5df2\u7981\u7528', 'Centralized policy disabled'), 'success');
      _s('secCentralizedHint', 'textContent', enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    } catch (err) {
      showToast(text('\u66f4\u65b0\u5931\u8d25: ', 'Update failed: ') + err.message, 'error');
      document.getElementById('secCentralizedToggle').checked = !enabled;
    }
  };

  global.toggleSecOrg = async function toggleSecOrg(enabled) {
    try {
      const settings = await api('/api/admin/security/settings');
      settings.org_structure_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? text('\u7ec4\u7ec7\u67b6\u6784\u5df2\u542f\u7528', 'Org structure enabled') : text('\u7ec4\u7ec7\u67b6\u6784\u5df2\u7981\u7528', 'Org structure disabled'), 'success');
      _s('secOrgHint', 'textContent', enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    } catch (err) {
      showToast(text('\u66f4\u65b0\u5931\u8d25: ', 'Update failed: ') + err.message, 'error');
      document.getElementById('secOrgToggle').checked = !enabled;
    }
  };

  global.showSetDefaultGroup = function showSetDefaultGroup() {
    const sec = state();
    const picker = document.getElementById('defaultGroupTreePicker');
    if (picker && sec.groupTree) {
      sec.defaultGroupPickedId = null;
      global._secDefaultGroupPickedId = null;
      global.renderDefaultGroupPicker(sec.groupTree, picker, 0);
    }
    document.getElementById('defaultGroupModalOverlay').classList.add('show');
  };

  global.renderDefaultGroupPicker = function renderDefaultGroupPicker(nodes, container, depth) {
    const sec = state();
    if (!container) return;
    if (depth === 0) container.innerHTML = '';
    if (!nodes || !nodes.length) return;
    nodes.forEach(function(node) {
      const row = document.createElement('div');
      row.style.paddingLeft = (depth * 16 + 8) + 'px';
      row.style.padding = '4px 8px 4px ' + (depth * 16 + 8) + 'px';
      row.style.cursor = 'pointer';
      row.style.borderRadius = '6px';
      row.style.transition = 'background .15s';
      if (node.id === sec.defaultGroupPickedId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }
      row.textContent = (node.children && node.children.length ? '\u25bc ' : '\u25cf ') + node.name;
      row.addEventListener('click', function() {
        sec.defaultGroupPickedId = node.id;
        global._secDefaultGroupPickedId = node.id;
        global.renderDefaultGroupPicker(sec.groupTree, container, 0);
      });
      container.appendChild(row);
      if (node.children && node.children.length) global.renderDefaultGroupPicker(node.children, container, depth + 1);
    });
  };

  global.confirmDefaultGroup = async function confirmDefaultGroup() {
    const sec = state();
    if (!sec.defaultGroupPickedId) {
      showToast(text('\u8bf7\u9009\u62e9\u4e00\u4e2a\u7ec4', 'Please select a group'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/settings/default-group', { method: 'PUT', body: JSON.stringify({ group_id: sec.defaultGroupPickedId }) });
      showToast(text('\u9ed8\u8ba4\u7ec4\u5df2\u8bbe\u7f6e', 'Default group set'), 'success');
      closeDefaultGroupModal();
      global.loadSecurityTab();
    } catch (err) {
      showToast(text('\u8bbe\u7f6e\u9ed8\u8ba4\u7ec4\u5931\u8d25: ', 'Set default group failed: ') + err.message, 'error');
    }
  };

  global.secCtxAction = function secCtxAction(action) {
    const sec = state();
    const menu = document.getElementById('secContextMenu');
    if (menu) menu.classList.add('hidden');
    const groupID = sec.contextGroupId;
    const groupName = sec.contextGroupName;
    if (!groupID) return;
    if (action === 'create') {
      const name = prompt(text('\u8f93\u5165\u65b0\u5b50\u7ec4\u540d\u79f0:', 'Enter new sub-group name:'));
      if (!name) return;
      api('/api/admin/security/groups', { method: 'POST', body: JSON.stringify({ name: name, parent_id: groupID }) })
        .then(function() { showToast(text('\u5b50\u7ec4\u5df2\u521b\u5efa', 'Sub-group created'), 'success'); global.loadSecurityTab(); })
        .catch(function(err) { showToast(text('\u521b\u5efa\u5931\u8d25: ', 'Create failed: ') + err.message, 'error'); });
      return;
    }
    if (action === 'rename') {
      const newName = prompt(text('\u8f93\u5165\u65b0\u540d\u79f0:', 'Enter new name:'), groupName);
      if (!newName) return;
      api('/api/admin/security/groups/' + encodeURIComponent(groupID), { method: 'PUT', body: JSON.stringify({ name: newName }) })
        .then(function() {
          showToast(text('\u5df2\u91cd\u547d\u540d', 'Renamed'), 'success');
          if (state().selectedGroupId === groupID) state().selectedGroupName = newName;
          global.loadSecurityTab();
        })
        .catch(function(err) { showToast(text('\u91cd\u547d\u540d\u5931\u8d25: ', 'Rename failed: ') + err.message, 'error'); });
      return;
    }
    if (action === 'assign') {
      sec.assignGroupId = groupID;
      global._secAssignGroupId = groupID;
      _s('assignUsersModalTitle', 'textContent', text('\u5206\u914d\u7528\u6237\u5230: ', 'Assign users to: ') + groupName);
      document.getElementById('assignUsersSearch').value = '';
      document.getElementById('assignUsersTree').innerHTML = hint(text('\u8f93\u5165\u90ae\u7bb1\u5730\u5740\u5e76\u6309\u56de\u8f66\u6dfb\u52a0', 'Type email and press Enter to add'));
      _s('assignUsersCount', 'textContent', '');
      document.getElementById('assignUsersModalOverlay').classList.add('show');
      document.getElementById('assignUsersSearch').focus();
      return;
    }
    if (action === 'delete') {
      if (!confirm(isZh() ? ('\u786e\u5b9a\u5220\u9664\u7ec4 "' + groupName + '" \u5417\uff1f') : ('Delete group "' + groupName + '"?'))) return;
      api('/api/admin/security/groups/' + encodeURIComponent(groupID), { method: 'DELETE' })
        .then(function() {
          showToast(text('\u7ec4\u5df2\u5220\u9664', 'Group deleted'), 'success');
          if (state().selectedGroupId === groupID) {
            state().selectedGroupId = null;
            state().selectedGroupName = null;
            global._secSelectedGroupId = null;
            global._secSelectedGroupName = null;
          }
          global.loadSecurityTab();
        })
        .catch(function(err) { showToast(text('\u5220\u9664\u5931\u8d25: ', 'Delete failed: ') + err.message, 'error'); });
    }
  };

  global.filterAssignUsers = function filterAssignUsers() {};

  global.confirmAssignUsers = async function confirmAssignUsers() {
    const sec = state();
    const email = document.getElementById('assignUsersSearch').value.trim();
    if (!email || !sec.assignGroupId) {
      showToast(text('\u8bf7\u8f93\u5165\u90ae\u7bb1', 'Enter an email'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.assignGroupId) + '/members', { method: 'POST', body: JSON.stringify({ email: email }) });
      showToast(text('\u7528\u6237\u5df2\u5206\u914d', 'User assigned'), 'success');
      closeAssignUsersModal();
      global.loadSecurityTab();
      if (sec.selectedGroupId) global.loadSecGroupMembers();
    } catch (err) {
      showToast(text('\u5206\u914d\u5931\u8d25: ', 'Assign failed: ') + err.message, 'error');
    }
  };
})(window);
