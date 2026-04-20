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
        membersPage: 1,
        membersPageSize: 50,
        membersCache: []
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
      html += '<div style="margin-bottom:8px;font-size:12px;color:var(--muted)">' + text('\u5b50\u7ec4:', 'Sub-groups:') + '</div>';
      children.forEach(function(child) {
        html += '<div class="item" style="min-height:auto;padding:8px 12px;margin-bottom:4px"><span style="font-weight:600">\ud83d\udcc1 ' + escapeHtml(child.name) + '</span><span style="color:var(--muted);font-size:11px;margin-left:8px">(' + String(Number(child.member_count || 0)) + ')</span></div>';
      });
    }
    if (totalMembers) {
      html += '<div style="margin:12px 0 10px;font-size:12px;color:var(--muted)">' + (isZh() ? ('\u6210\u5458 (' + totalMembers + '):') : ('Members (' + totalMembers + '):')) + '</div>';
      html += '<div style="display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px" id="secMembersGrid">';
      pageMembers.forEach(function(email, idx) {
        var absoluteIndex = start + idx + 1;
        html += '<div class="item" style="min-height:auto;padding:12px 12px 10px;gap:8px">';
        html += '<div style="font-weight:600;word-break:break-all;line-height:1.45">' + escapeHtml(email) + '</div>';
        html += '<div class="item-meta">' + (isZh() ? ('\u7b2c ' + absoluteIndex + ' \u4e2a\u7528\u6237') : ('User #' + absoluteIndex)) + '</div>';
        html += '<div class="actions" style="margin-top:auto"><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px;color:var(--danger);width:100%" onclick="removeSecGroupMember(\'' + escapeHtml(email).replace(/'/g, "\\'") + '\')">' + text('\u79fb\u9664', 'Remove') + '</button></div>';
        html += '</div>';
      });
      html += '</div>';
      if (totalPages > 1) {
        var startIdx = start + 1;
        var endIdx = Math.min(start + pageMembers.length, totalMembers);
        html += '<div class="pager" style="margin-top:14px"><div class="pager-meta">' + (isZh() ? ('\u7b2c ' + sec.membersPage + ' / ' + totalPages + ' \u9875\uff0c\u663e\u793a ' + startIdx + '-' + endIdx + ' / ' + totalMembers) : ('Page ' + sec.membersPage + ' / ' + totalPages + ', showing ' + startIdx + '-' + endIdx + ' / ' + totalMembers)) + '</div><div class="pager-actions"><button class="btn-ghost" style="height:32px" onclick="changeSecMembersPage(-1)"' + (sec.membersPage <= 1 ? ' disabled' : '') + '>' + text('\u4e0a\u4e00\u9875', 'Previous') + '</button><button class="btn-ghost" style="height:32px" onclick="changeSecMembersPage(1)"' + (sec.membersPage >= totalPages ? ' disabled' : '') + '>' + text('\u4e0b\u4e00\u9875', 'Next') + '</button></div></div>';
      }
    }
    return html || hint(text('\u65e0\u6210\u5458', 'No members'));
  }

  function applySecurityI18n() {
    _s('navSecurity', 'textContent', text('\u5b89\u5168\u7ba1\u7406', 'Security'));
    _s('navSecurityDesc', 'textContent', text('\u7528\u6237\u7ec4\u3001\u7b56\u7565\u4e0e\u7ec4\u7ec7\u67b6\u6784', 'Security Management'));
    _s('secTitle', 'textContent', text('\u5b89\u5168\u7ba1\u7406', 'Security'));
    _s('secDesc', 'textContent', text('\u7ba1\u7406\u7528\u6237\u7ec4\u3001\u7b56\u7565\u548c\u7ec4\u7ec7\u67b6\u6784\u3002', 'Manage groups, policies, and organization structure.'));
    _s('secReloadBtn', 'textContent', text('\u5237\u65b0', 'Reload'));
    _s('secCentralizedTitle', 'textContent', text('\u96c6\u4e2d\u7b56\u7565', 'Centralized Policy'));
    _s('secOrgTitle', 'textContent', text('\u7ec4\u7ec7\u67b6\u6784', 'Org Structure'));
    _s('secDefaultGroupTitle', 'textContent', text('\u9ed8\u8ba4\u7ec4', 'Default Group'));
    _s('secDefaultGroupSetBtn', 'textContent', text('\u8bbe\u7f6e', 'Set'));
    _s('secGroupTreeTitle', 'textContent', text('\u7ec4\u7ec7\u6811', 'Group Tree'));
    _s('secCtxCreate', 'textContent', text('\u521b\u5efa\u5b50\u90e8\u95e8', 'Create Sub-department'));
    _s('secCtxRename', 'textContent', text('\u91cd\u547d\u540d\u90e8\u95e8', 'Rename Department'));
    _s('secCtxAssign', 'textContent', text('\u79fb\u5165\u7528\u6237', 'Move Users Here'));
    _s('secCtxDelete', 'textContent', text('\u5220\u9664\u90e8\u95e8', 'Delete Department'));
    _s('secPolicySaveBtn', 'textContent', text('\u4fdd\u5b58', 'Save'));
    _s('secMembersTitle', 'textContent', text('\u6210\u5458', 'Members'));
    _s('secMembersReloadBtn', 'textContent', text('\u5237\u65b0', 'Reload'));
    _s('defaultGroupModalTitle', 'textContent', text('\u9ed8\u8ba4\u7ec4', 'Default Group'));
    _s('defaultGroupModalDesc', 'textContent', text('\u4e3a\u65b0\u7528\u6237\u9009\u62e9\u9ed8\u8ba4\u6240\u5c5e\u7ec4\u3002', 'Choose the default group for new users.'));
    _s('defaultGroupCancelBtn', 'textContent', text('\u53d6\u6d88', 'Cancel'));
    _s('defaultGroupConfirmBtn', 'textContent', text('\u786e\u8ba4', 'Confirm'));
    _s('assignUsersModalTitle', 'textContent', text('\u79fb\u5165\u7528\u6237', 'Move Users'));
    _s('assignUsersModalDesc', 'textContent', text('\u53ef\u4ee5\u641c\u7d22\u5e76\u5c06\u7528\u6237\u79fb\u5165\u5f53\u524d\u90e8\u95e8\u3002', 'Search and move a user into the selected department.'));
    _s('assignUsersCancelBtn', 'textContent', text('\u53d6\u6d88', 'Cancel'));
    _s('assignUsersConfirmBtn', 'textContent', text('\u786e\u8ba4', 'Confirm'));
    _s('assignUsersSearch', 'placeholder', text('\u641c\u7d22\u90ae\u7bb1\u6216 SN', 'Search email or SN'));
    _s('secContextMenu', 'title', text('\u90e8\u95e8\u64cd\u4f5c\u83dc\u5355', 'Department actions'));
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
      if (depth === 0) container.innerHTML = hint(text('\u65e0\u7ec4\u7ec7\u6570\u636e', 'No groups'));
      return;
    }
    nodes.forEach(function(node) {
      var row = document.createElement('div');
      row.style.padding = '4px 8px 4px ' + (depth * 18 + 8) + 'px';
      row.style.borderRadius = '6px';
      row.style.transition = 'background .15s';
      row.style.display = 'flex';
      row.style.alignItems = 'center';
      row.style.gap = '8px';
      row.style.cursor = 'pointer';
      if (node.id === sec.selectedGroupId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }

      var toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.style.width = '20px';
      toggle.style.minWidth = '20px';
      toggle.style.height = '20px';
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
      label.innerHTML = '<span>' + escapeHtml(node.name) + '</span><span style="color:var(--muted);font-size:11px;margin-left:8px">(' + String(Number(node.member_count || 0)) + ')</span>';
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
          loading.textContent = text('\u6b63\u5728\u52a0\u8f7d...', 'Loading...');
          container.appendChild(loading);
        }
      }
    });
  }

  async function loadSettings() {
    var settings = await api('/api/admin/security/settings');
    document.getElementById('secCentralizedToggle').checked = !!settings.centralized_security_enabled;
    document.getElementById('secOrgToggle').checked = !!settings.org_structure_enabled;
    _s('secCentralizedHint', 'textContent', settings.centralized_security_enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    _s('secOrgHint', 'textContent', settings.org_structure_enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    var dgHint = settings.default_group_id || text('\u672a\u8bbe\u7f6e', 'Not set');
    _s('secDefaultGroupHint', 'textContent', text('\u9ed8\u8ba4\u7ec4: ', 'Default group: ') + dgHint);
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
    var rows = (sec.assignUsers || []).filter(function(user) {
      if (!query) return true;
      var email = String(user.email || '').toLowerCase();
      var sn = String(user.sn || '').toLowerCase();
      return email.indexOf(query) >= 0 || sn.indexOf(query) >= 0;
    });
    if (!rows.length) {
      root.innerHTML = hint(query ? text('\u65e0\u5339\u914d\u7528\u6237', 'No users match the search') : text('\u6682\u65e0\u53ef\u5206\u914d\u7528\u6237', 'No users available'));
    } else {
      root.innerHTML = rows.map(function(user) {
        var email = user.email || '';
        var selected = sec.selectedAssignEmail === email;
        return '<div class="item" style="min-height:auto;padding:8px 10px;margin-bottom:6px;border:' + (selected ? '1px solid rgba(47,128,237,.38)' : '1px solid var(--line)') + ';background:' + (selected ? 'rgba(47,128,237,.06)' : 'linear-gradient(180deg,rgba(255,255,255,.98) 0%,rgba(247,251,255,.98) 100%)') + ';cursor:pointer" onclick="selectAssignUser(\'' + escapeHtml(email).replace(/'/g, "\\'") + '\')"><div style="display:flex;align-items:center;justify-content:space-between;gap:8px"><div><div style="font-weight:600">' + escapeHtml(email) + '</div><div class="item-meta">' + escapeHtml(text('SN', 'SN')) + ': ' + escapeHtml(user.sn || '-') + ' | ' + escapeHtml(text('\u72b6\u6001', 'Status')) + ': ' + escapeHtml(user.status || text('\u672a\u77e5', 'Unknown')) + '</div></div><button class="btn-ghost" style="height:26px;font-size:11px;padding:0 10px">' + escapeHtml(text('\u79fb\u5165', 'Move')) + '</button></div></div>';
      }).join('');
    }
    _s('assignUsersCount', 'textContent', (isZh() ? ('\u663e\u793a ' + rows.length + ' / ' + sec.assignUsers.length + ' \u4e2a\u7528\u6237') : ('Showing ' + rows.length + ' / ' + sec.assignUsers.length + ' users')));
  }

  async function loadAssignableUsers() {
    var sec = state();
    var data = await api('/api/admin/users');
    sec.assignUsers = dedupeUsersByEmail((data.users || []).filter(function(user) { return !!(user && user.email); }));
    renderAssignUsers();
  }

  global.loadSecurityTab = async function loadSecurityTab() {
    applySecurityI18n();
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
        showToast(text('\u52a0\u8f7d\u5b50\u90e8\u95e8\u5931\u8d25: ', 'Load child groups failed: ') + err.message, 'error');
      }
    } else {
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    }
  };

  global.showSecContextMenu = function showSecContextMenu(x, y) {
    var menu = document.getElementById('secContextMenu');
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
    var sec = state();
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
    var panel = document.getElementById('secPolicyPanel');
    if (!panel) return;
    try {
      var view = await api('/api/admin/security/groups/' + encodeURIComponent(groupId) + '/policy');
      state().policyCache = view;
      global._secPolicyCache = view;
      var items = view.items || {};
      var policyKeys = [
        { key: 'file_outbound_enabled', label: text('\u6587\u4ef6\u5916\u53d1', 'File Outbound'), type: 'bool' },
        { key: 'image_outbound_enabled', label: text('\u56fe\u7247\u5916\u53d1', 'Image Outbound'), type: 'bool' },
        { key: 'gossip_enabled', label: text('Gossip \u529f\u80fd', 'Gossip'), type: 'bool' },
        { key: 'yolo_mode_allowed', label: text('YOLO \u6a21\u5f0f', 'YOLO Mode'), type: 'bool' },
        { key: 'smart_route_enabled', label: text('\u667a\u80fd\u8def\u7531', 'Smart Route'), type: 'bool' },
        { key: 'guardrail_mode', label: text('\u62a4\u680f\u6a21\u5f0f', 'Guardrail Mode'), type: 'select', options: ['none', 'standard', 'strict'] },
        { key: 'sandbox_mode', label: text('\u6c99\u7bb1\u6a21\u5f0f', 'Sandbox Mode'), type: 'select', options: ['none', 'basic', 'strict'] },
        { key: 'network_level', label: text('\u7f51\u7edc\u7ea7\u522b', 'Network Level'), type: 'select', options: ['none', 'limited', 'full'] }
      ];
      var html = '';
      policyKeys.forEach(function(pk) {
        var item = items[pk.key] || {};
        var value = item.value;
        var source = item.source || 'inherited';
        var sourceName = item.source_name || '';
        var sourceTag = source === 'self'
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
    var sec = state();
    if (!sec.selectedGroupId) {
      showToast(text('\u8bf7\u5148\u9009\u62e9\u4e00\u4e2a\u7ec4', 'Select a group first'), 'info');
      return;
    }
    var policy = {};
    document.querySelectorAll('#secPolicyPanel [data-policy-key]').forEach(function(el) {
      var key = el.dataset.policyKey;
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
    var sec = state();
    if (!sec.selectedGroupId) return;
    var container = document.getElementById('secMembersList');
    if (!container) return;
    container.innerHTML = hint(text('\u6b63\u5728\u52a0\u8f7d\u6210\u5458...', 'Loading members...'));
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
      var settings = await api('/api/admin/security/settings');
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
      var settings = await api('/api/admin/security/settings');
      settings.org_structure_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? text('\u7ec4\u7ec7\u67b6\u6784\u5df2\u542f\u7528', 'Org structure enabled') : text('\u7ec4\u7ec7\u67b6\u6784\u5df2\u7981\u7528', 'Org structure disabled'), 'success');
      _s('secOrgHint', 'textContent', enabled ? text('\u5df2\u542f\u7528', 'Enabled') : text('\u5df2\u7981\u7528', 'Disabled'));
    } catch (err) {
      showToast(text('\u66f4\u65b0\u5931\u8d25: ', 'Update failed: ') + err.message, 'error');
      document.getElementById('secOrgToggle').checked = !enabled;
    }
  };

  global.closeDefaultGroupModal = function closeDefaultGroupModal() {
    document.getElementById('defaultGroupModalOverlay').classList.remove('show');
  };

  global.showSetDefaultGroup = async function showSetDefaultGroup() {
    var sec = state();
    try {
      var data = await api('/api/admin/security/groups');
      var root = normalizeNode(data.tree);
      sec.defaultGroupTree = root ? [root] : [];
      sec.defaultGroupPickedId = null;
      global._secDefaultGroupPickedId = null;
      global.renderDefaultGroupPicker(sec.defaultGroupTree, document.getElementById('defaultGroupTreePicker'), 0);
      document.getElementById('defaultGroupModalOverlay').classList.add('show');
    } catch (err) {
      showToast(text('\u52a0\u8f7d\u7ec4\u7ec7\u6811\u5931\u8d25: ', 'Load group tree failed: ') + err.message, 'error');
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
      showToast(text('\u8bf7\u9009\u62e9\u4e00\u4e2a\u7ec4', 'Please select a group'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/settings/default-group', { method: 'PUT', body: JSON.stringify({ group_id: sec.defaultGroupPickedId }) });
      showToast(text('\u9ed8\u8ba4\u7ec4\u5df2\u8bbe\u7f6e', 'Default group set'), 'success');
      global.closeDefaultGroupModal();
      global.loadSecurityTab();
    } catch (err) {
      showToast(text('\u8bbe\u7f6e\u9ed8\u8ba4\u7ec4\u5931\u8d25: ', 'Set default group failed: ') + err.message, 'error');
    }
  };

  global.closeAssignUsersModal = function closeAssignUsersModal() {
    var sec = state();
    sec.selectedAssignEmail = '';
    sec.assignGroupId = null;
    sec.contextGroupName = null;
    document.getElementById('assignUsersModalOverlay').classList.remove('show');
  };

  global.selectAssignUser = function selectAssignUser(email) {
    var sec = state();
    sec.selectedAssignEmail = email || '';
    _s('assignUsersSearch', 'value', sec.selectedAssignEmail);
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
      var name = prompt(text('\u8f93\u5165\u65b0\u5b50\u7ec4\u540d\u79f0:', 'Enter new sub-group name:'));
      if (!name) return;
      api('/api/admin/security/groups', { method: 'POST', body: JSON.stringify({ name: name, parent_id: groupID }) })
        .then(function() { showToast(text('\u5b50\u7ec4\u5df2\u521b\u5efa', 'Sub-group created'), 'success'); global.loadSecurityTab(); })
        .catch(function(err) { showToast(text('\u521b\u5efa\u5931\u8d25: ', 'Create failed: ') + err.message, 'error'); });
      return;
    }
    if (action === 'rename') {
      var newName = prompt(text('\u8f93\u5165\u65b0\u540d\u79f0:', 'Enter new name:'), groupName);
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
      sec.contextGroupName = groupName;
      sec.selectedAssignEmail = '';
      global._secAssignGroupId = groupID;
      _s('assignUsersModalTitle', 'textContent', text('\u79fb\u5165\u7528\u6237\u5230\u90e8\u95e8: ', 'Move users to department: ') + groupName);
      _s('assignUsersSearch', 'value', '');
      _s('assignUsersCount', 'textContent', text('\u6b63\u5728\u52a0\u8f7d...', 'Loading...'));
      document.getElementById('assignUsersTree').innerHTML = hint(text('\u6b63\u5728\u52a0\u8f7d\u7528\u6237\u5217\u8868...', 'Loading users...'));
      document.getElementById('assignUsersModalOverlay').classList.add('show');
      document.getElementById('assignUsersSearch').focus();
      loadAssignableUsers().catch(function(err) {
        document.getElementById('assignUsersTree').innerHTML = errorHint(err.message);
        showToast(text('\u52a0\u8f7d\u7528\u6237\u5931\u8d25: ', 'Load users failed: ') + err.message, 'error');
      });
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

  global.filterAssignUsers = function filterAssignUsers() {
    state().selectedAssignEmail = String(document.getElementById('assignUsersSearch').value || '').trim();
    renderAssignUsers();
  };

  applySecurityI18n();
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() {
      var sec = state();
      applySecurityI18n();
      if (sec.assignGroupId && sec.contextGroupName) {
        _s('assignUsersModalTitle', 'textContent', text('\u79fb\u5165\u7528\u6237\u5230: ', 'Move users to: ') + sec.contextGroupName);
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
    var email = String(document.getElementById('assignUsersSearch').value || '').trim() || sec.selectedAssignEmail;
    if (!email || !sec.assignGroupId) {
      showToast(text('\u8bf7\u9009\u62e9\u6216\u8f93\u5165\u90ae\u7bb1', 'Select or enter an email'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.assignGroupId) + '/members', { method: 'POST', body: JSON.stringify({ email: email }) });
      showToast(text('\u7528\u6237\u5df2\u79fb\u5165', 'User moved'), 'success');
      global.closeAssignUsersModal();
      global.loadSecurityTab();
      if (sec.selectedGroupId) global.loadSecGroupMembers();
    } catch (err) {
      showToast(text('\u5206\u914d\u5931\u8d25: ', 'Assign failed: ') + err.message, 'error');
    }
  };
})(window);
