'use strict';

var CLOUD_AGENT_QUOTA_I18N = {
  en: {
    nav: 'Cloud agents',
    navDesc: 'Opening scope and limits',
    title: 'Cloud agent limits',
    hint: 'Global switch and default limits. A department or user can turn the feature on or off. A blank limit inherits the level above. Zero means that account cannot create, or cannot run. The task host is the maclawsrv connection in System settings. This page does not edit that connection.',
    enabled: 'Allow cloud agents',
    instances: 'Instance limit',
    turns: 'Concurrent turns',
    save: 'Save',
    reload: 'Reload',
    saved: 'Saved',
    bad: 'Limits must be zero or a positive integer',
    loadFailed: 'Load cloud agent limits failed: {error}',
    saveFailed: 'Save cloud agent limits failed: {error}',
    accessTitle: 'Opening scope',
    accessHint: 'Pick a department in the organization tree, or expand it and pick a user. Inherit follows the next level up. On opens this scope even when a higher level is off. Off closes it even when a higher level is on.',
    inherit: 'Inherit',
    on: 'On',
    off: 'Off',
    limitInherit: 'Inherit',
    filter: 'Search departments or users',
    empty: 'No departments yet. Add them in the organization first.',
    loading: 'Loading organization...',
    membersFailed: 'Members could not be loaded',
    membersLoading: 'Loading members...',
    usersFailed: 'Accounts could not be loaded, so people in a department cannot be saved yet',
    users: 'Matching users',
    accessSaved: 'Opening scope saved',
    accessFailed: 'Save opening scope failed: {error}',
    accessLoadFailed: 'Load opening scope failed: {error}'
  },
  zh: {
    nav: '\u4e91\u7aef Agent',
    navDesc: '\u5f00\u901a\u8303\u56f4\u4e0e\u4e0a\u9650',
    title: '\u4e91\u7aef Agent \u9650\u989d',
    hint: '\u5168\u5c40\u5f00\u5173\u548c\u9ed8\u8ba4\u4e0a\u9650\u3002\u90e8\u95e8\u548c\u7528\u6237\u53ef\u4ee5\u5355\u72ec\u5f00\u901a\u6216\u5173\u95ed\u3002\u7559\u7a7a\u7684\u4e0a\u9650\u7ee7\u627f\u4e0a\u4e00\u7ea7\u30020 \u8868\u793a\u4e0d\u80fd\u521b\u5efa\uff0c\u6216\u4e0d\u80fd\u8fd0\u884c\u3002\u4efb\u52a1\u5bbf\u4e3b\u4f7f\u7528\u7cfb\u7edf\u8bbe\u7f6e\u91cc\u7684 maclawsrv \u63a5\u5165\uff0c\u8fd9\u91cc\u4e0d\u518d\u914d\u7f6e\u3002',
    enabled: '\u5141\u8bb8\u4e91\u7aef Agent',
    instances: '\u5b9e\u4f8b\u4e0a\u9650',
    turns: '\u540c\u65f6\u8f6e\u6570',
    save: '\u4fdd\u5b58',
    reload: '\u91cd\u65b0\u52a0\u8f7d',
    saved: '\u5df2\u4fdd\u5b58',
    bad: '\u4e0a\u9650\u5fc5\u987b\u662f 0 \u6216\u6b63\u6574\u6570',
    loadFailed: '\u8bfb\u53d6\u4e91\u7aef Agent \u9650\u989d\u5931\u8d25\uff1a{error}',
    saveFailed: '\u4fdd\u5b58\u4e91\u7aef Agent \u9650\u989d\u5931\u8d25\uff1a{error}',
    accessTitle: '\u5f00\u901a\u8303\u56f4',
    accessHint: '\u5728\u7ec4\u7ec7\u673a\u6784\u6811\u91cc\u9009\u62e9\u90e8\u95e8\uff0c\u6216\u5c55\u5f00\u540e\u9009\u62e9\u7528\u6237\u3002\u7ee7\u627f\u6cbf\u7528\u4e0a\u4e00\u7ea7\u3002\u5f00\u901a\u53ef\u4ee5\u5728\u4e0a\u4e00\u7ea7\u5173\u95ed\u65f6\u5355\u72ec\u6253\u5f00\u3002\u5173\u95ed\u53ef\u4ee5\u5728\u4e0a\u4e00\u7ea7\u5f00\u901a\u65f6\u5355\u72ec\u5173\u6389\u3002',
    inherit: '\u7ee7\u627f',
    on: '\u5f00\u901a',
    off: '\u5173\u95ed',
    limitInherit: '\u7ee7\u627f',
    filter: '\u641c\u7d22\u90e8\u95e8\u6216\u7528\u6237',
    empty: '\u8fd8\u6ca1\u6709\u90e8\u95e8\u3002\u8bf7\u5148\u5728\u7ec4\u7ec7\u673a\u6784\u91cc\u6dfb\u52a0\u3002',
    loading: '\u6b63\u5728\u8bfb\u53d6\u7ec4\u7ec7\u673a\u6784...',
    membersFailed: '\u6210\u5458\u6ca1\u6709\u8bfb\u51fa\u6765',
    membersLoading: '\u6b63\u5728\u8bfb\u53d6\u6210\u5458...',
    usersFailed: '\u8d26\u53f7\u6ca1\u6709\u8bfb\u51fa\u6765\uff0c\u90e8\u95e8\u91cc\u7684\u4eba\u6682\u65f6\u4e0d\u80fd\u4fdd\u5b58',
    users: '\u5339\u914d\u7684\u7528\u6237',
    accessSaved: '\u5f00\u901a\u8303\u56f4\u5df2\u4fdd\u5b58',
    accessFailed: '\u4fdd\u5b58\u5f00\u901a\u8303\u56f4\u5931\u8d25\uff1a{error}',
    accessLoadFailed: '\u8bfb\u53d6\u5f00\u901a\u8303\u56f4\u5931\u8d25\uff1a{error}'
  }
};

var cagOrg = {
  tree: [],
  users: [],
  usersByEmail: {},
  usersById: {},
  access: {},
  members: {},
  memberLoads: {},
  memberFailed: {},
  expanded: {},
  loaded: false,
  drafts: {},
  loadEpoch: 0,
  memberEpoch: 0,
  suspendDrafts: 0,
  saveChain: Promise.resolve()
};

function cloudAgentQuotaText() {
  var lang = (typeof getCurrentLanguage === 'function' && getCurrentLanguage() === 'en') ? 'en' : 'zh';
  return CLOUD_AGENT_QUOTA_I18N[lang] || CLOUD_AGENT_QUOTA_I18N.zh;
}

function applyCloudAgentQuotaI18n() {
  var t = cloudAgentQuotaText();
  var set = function (id, value) {
    var el = document.getElementById(id);
    if (el) el.textContent = value;
  };
  set('navCloudAgents', t.nav);
  set('navCloudAgentsDesc', t.navDesc);
  set('cloudAgentQuotaTitle', t.title);
  set('cloudAgentQuotaHint', t.hint);
  set('cloudAgentQuotaEnabledLabel', t.enabled);
  set('cloudAgentQuotaInstancesLabel', t.instances);
  set('cloudAgentQuotaTurnsLabel', t.turns);
  set('cloudAgentQuotaSaveBtn', t.save);
  set('cloudAgentQuotaReloadBtn', t.reload);
  set('cloudAgentAccessTitle', t.accessTitle);
  set('cloudAgentAccessHint', t.accessHint);
  set('cloudAgentAccessReloadBtn', t.reload);
  var filter = document.getElementById('cloudAgentAccessFilter');
  if (filter) filter.placeholder = t.filter;
  if (cagOrg.loaded) renderCloudAgentAccessTree();
}

function cloudAgentQuotaInt(id) {
  var el = document.getElementById(id);
  var raw = el ? String(el.value || '').trim() : '';
  if (raw === '') return null;
  if (!/^\d+$/.test(raw)) return undefined;
  return Number(raw);
}

function cagReadLimit(raw) {
  var text = String(raw == null ? '' : raw).trim();
  if (text === '') return null;
  if (!/^\d+$/.test(text)) return undefined;
  return Number(text);
}

function cagEscape(value) {
  return String(value == null ? '' : value).replace(/[&<>"']/g, function (ch) {
    return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[ch];
  });
}

function cagKey(scope, id) {
  return String(scope || '') + '\n' + String(id || '');
}

function cagEnsureStyle() {
  if (!document || typeof document.getElementById !== 'function' || typeof document.createElement !== 'function') return;
  if (document.getElementById('cloud-agent-access-style')) return;
  if (!document.head || typeof document.head.appendChild !== 'function') return;
  var style = document.createElement('style');
  style.id = 'cloud-agent-access-style';
  style.textContent = ''
    + '#tab-cloud-agents .cag-org-tree{max-height:460px;overflow:auto;border:1px solid #e4e9f2;border-radius:10px;padding:6px;background:#fbfcfe}'
    + '#tab-cloud-agents .cag-org-row{display:flex;align-items:center;gap:8px;min-height:36px;padding:3px 8px;border-radius:8px}'
    + '#tab-cloud-agents .cag-org-row:hover{background:#eef5ff}'
    + '#tab-cloud-agents .cag-org-toggle{width:22px;min-width:22px;height:22px;min-height:22px;padding:0;border:0;background:transparent;box-shadow:none;color:#64748b;font-size:12px}'
    + '#tab-cloud-agents .cag-org-pad{width:22px;min-width:22px;flex:0 0 auto}'
    + '#tab-cloud-agents .cag-org-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px}'
    + '#tab-cloud-agents .cag-org-user .cag-org-name{color:#64748b;font-size:12px}'
    + '#tab-cloud-agents .cag-access-enabled{width:92px;height:30px;min-height:30px;flex:0 0 auto}'
    + '#tab-cloud-agents .cag-access-instances,#tab-cloud-agents .cag-access-turns{width:88px;height:30px;min-height:30px;flex:0 0 auto}'
    + '#tab-cloud-agents .cag-access-save{height:30px;min-height:30px;padding:0 10px;font-size:12px;flex:0 0 auto}'
    + '#tab-cloud-agents .cag-org-note{margin:4px 8px;color:#64748b;font-size:12px}'
    + '#tab-cloud-agents #cloudAgentAccessFilter{max-width:360px;margin:0 0 8px}';
  document.head.appendChild(style);
}

function cagNormalize(nodes, ancestry, depth) {
  var path = ancestry || {};
  var current = depth || 0;
  if (current > 24) return [];
  return (Array.isArray(nodes) ? nodes : []).reduce(function (out, node) {
    if (!node || typeof node !== 'object') return out;
    var id = String(node.id || '').trim();
    if (!id || path[id]) return out;
    var next = {};
    Object.keys(path).forEach(function (key) { next[key] = true; });
    next[id] = true;
    out.push({
      id: id,
      name: String(node.name || id).trim() || id,
      children: cagNormalize(node.children, next, current + 1)
    });
    return out;
  }, []);
}

function cagUserLabel(user) {
  if (!user) return '';
  return String(user.email || user.phone || user.id || '').trim();
}

function cagIndexUsers(users) {
  cagOrg.users = [];
  cagOrg.usersByEmail = {};
  cagOrg.usersById = {};
  (users || []).forEach(function (user) {
    if (!user) return;
    var id = String(user.id || '').trim();
    if (!id) return;
    cagOrg.users.push(user);
    cagOrg.usersById[id] = user;
    [user.email].concat(user.emails || [], user.phone ? [user.phone] : [], user.phones || []).forEach(function (email) {
      var key = String(email || '').trim().toLowerCase();
      if (key && !cagOrg.usersByEmail[key]) cagOrg.usersByEmail[key] = user;
    });
  });
}

function cagOption(value, label, current) {
  return '<option value="' + value + '"' + (value === current ? ' selected' : '') + '>' + cagEscape(label) + '</option>';
}

function cagControls(scope, target) {
  var t = cloudAgentQuotaText();
  var row = cagOrg.access[cagKey(scope, target)] || {};
  var draft = cagOrg.drafts[cagKey(scope, target)];
  var enabled = draft ? draft.enabled : (row.enabled || 'inherit');
  var instances = draft ? draft.instances : (row.max_instances == null ? '' : String(row.max_instances));
  var turns = draft ? draft.turns : (row.max_concurrent_turns == null ? '' : String(row.max_concurrent_turns));
  return '<select class="cag-access-enabled" aria-label="' + cagEscape(t.accessTitle) + '">'
    + cagOption('inherit', t.inherit, enabled)
    + cagOption('on', t.on, enabled)
    + cagOption('off', t.off, enabled)
    + '</select>'
    + '<input class="cag-access-instances" type="number" min="0" step="1" placeholder="' + cagEscape(t.limitInherit) + '" value="' + cagEscape(instances) + '" aria-label="' + cagEscape(t.instances) + '">'
    + '<input class="cag-access-turns" type="number" min="0" step="1" placeholder="' + cagEscape(t.limitInherit) + '" value="' + cagEscape(turns) + '" aria-label="' + cagEscape(t.turns) + '">'
    + '<button type="button" class="btn-secondary cag-access-save">' + cagEscape(t.save) + '</button>';
}

function cagRow(scope, target, name, depth, userRow, toggle) {
  return '<div class="cag-org-row' + (userRow ? ' cag-org-user' : '') + '" data-scope="' + cagEscape(scope) + '" data-target="' + cagEscape(target) + '" style="margin-left:' + (depth * 16) + 'px">'
    + (toggle || '<span class="cag-org-pad"></span>')
    + '<span class="cag-org-name">' + cagEscape(name) + '</span>'
    + cagControls(scope, target)
    + '</div>';
}

function cagQuery() {
  var el = document.getElementById('cloudAgentAccessFilter');
  return el ? String(el.value || '').trim().toLowerCase() : '';
}

function cagMembersReady(groupId) {
  return Object.prototype.hasOwnProperty.call(cagOrg.members, groupId);
}

function cagMemberView(email) {
  var raw = String(email || '').trim();
  var user = cagOrg.usersByEmail[raw.toLowerCase()];
  var userId = user ? String(user.id || '').trim() : '';
  return {
    email: raw,
    userId: userId,
    label: user ? (cagUserLabel(user) || raw) : raw
  };
}

function cagMemberViews(groupId) {
  return (cagOrg.members[groupId] || []).map(cagMemberView);
}

function cagMembers(groupId) {
  var seen = {};
  return cagMemberViews(groupId).filter(function (member) {
    if (!member.userId) return true;
    if (seen[member.userId]) return false;
    seen[member.userId] = true;
    return true;
  });
}

function cagUserHaystack(user) {
  if (!user) return '';
  return [cagUserLabel(user), user.id].concat(user.emails || [], user.phones || [], user.phone ? [user.phone] : []).join('\n').toLowerCase();
}

function cagQueryWantsMembers(query) {
  if (!query) return false;
  if (query.length >= 2) return true;
  return /[\u3400-\u9fff]/.test(query);
}

function cagDeptMatch(node, query) {
  if (!query) return true;
  if (String(node.name || '').toLowerCase().indexOf(query) !== -1) return true;
  var members = cagMemberViews(node.id);
  for (var i = 0; i < members.length; i += 1) {
    if (String(members[i].label || members[i].email || '').toLowerCase().indexOf(query) !== -1) return true;
    var account = members[i].userId ? cagOrg.usersById[members[i].userId] : null;
    if (cagUserHaystack(account).indexOf(query) !== -1) return true;
  }
  var children = node.children || [];
  for (var c = 0; c < children.length; c += 1) {
    if (cagDeptMatch(children[c], query)) return true;
  }
  return false;
}

function cagMemberVisible(member, query, deptNameHit) {
  if (!query || deptNameHit) return true;
  if (String(member.label || member.email || '').toLowerCase().indexOf(query) !== -1) return true;
  var user = member.userId ? cagOrg.usersById[member.userId] : null;
  return cagUserHaystack(user).indexOf(query) !== -1;
}

function cagRenderNodes(nodes, depth, query) {
  var html = '';
  (nodes || []).forEach(function (node) {
    if (!cagDeptMatch(node, query)) return;
    var nameHit = !query || String(node.name || '').toLowerCase().indexOf(query) !== -1;
    var expanded = !!cagOrg.expanded[node.id];
    var ready = cagMembersReady(node.id);
    var failed = !!cagOrg.memberFailed[node.id] && !ready;
    var showMembers = expanded || !!query;
    var waiting = showMembers && (expanded || cagQueryWantsMembers(query)) && !ready && !failed;
    var childHtml = (expanded || !!query) ? cagRenderNodes(node.children, depth + 1, query) : '';
    var memberHtml = '';
    var noteStyle = ' style="margin-left:' + ((depth + 1) * 16) + 'px"';
    if (showMembers && failed) memberHtml += '<div class="cag-org-note"' + noteStyle + '>' + cagEscape(cloudAgentQuotaText().membersFailed) + '</div>';
    else if (waiting) memberHtml += '<div class="cag-org-note"' + noteStyle + '>' + cagEscape(cloudAgentQuotaText().membersLoading) + '</div>';
    if (showMembers && ready) {
      cagMembers(node.id).forEach(function (member) {
        if (!cagMemberVisible(member, query, nameHit)) return;
        if (!member.userId) {
          memberHtml += '<div class="cag-org-row cag-org-user" style="margin-left:' + ((depth + 1) * 16) + 'px"><span class="cag-org-pad"></span><span class="cag-org-name">' + cagEscape(member.label || member.email) + '</span></div>';
          return;
        }
        memberHtml += cagRow('user', member.userId, member.label || member.email, depth + 1, true, '');
      });
    }
    var open = !!(expanded || childHtml || memberHtml);
    var toggle = '<button type="button" class="cag-org-toggle" data-dept="' + cagEscape(node.id) + '" aria-expanded="' + (open ? 'true' : 'false') + '">' + (open ? 'v' : '>') + '</button>';
    html += cagRow('department', node.id, node.name, depth, false, toggle);
    html += childHtml + memberHtml;
  });
  return html;
}

function cagMatchingUsers(query) {
  if (!query) return [];
  var out = [];
  (cagOrg.users || []).forEach(function (user) {
    if (cagUserHaystack(user).indexOf(query) !== -1) out.push(user);
  });
  return out.slice(0, 40);
}

function cagVisibleUserIds(query) {
  var ids = {};
  function walk(nodes) {
    (nodes || []).forEach(function (node) {
      if (!cagDeptMatch(node, query)) return;
      var nameHit = !query || String(node.name || '').toLowerCase().indexOf(query) !== -1;
      var open = !!cagOrg.expanded[node.id] || !!query;
      if (!open) return;
      walk(node.children);
      cagMembers(node.id).forEach(function (member) {
        if (!member.userId || !cagMemberVisible(member, query, nameHit)) return;
        ids[member.userId] = true;
      });
    });
  }
  walk(cagOrg.tree);
  return ids;
}

function cagCaptureDrafts(root) {
  var drafts = {};
  if (!root || typeof root.querySelectorAll !== 'function') return drafts;
  var rows = root.querySelectorAll('.cag-org-row') || [];
  for (var i = 0; i < rows.length; i += 1) {
    var row = rows[i];
    if (!row || typeof row.getAttribute !== 'function' || typeof row.querySelector !== 'function') continue;
    var scope = String(row.getAttribute('data-scope') || '');
    var target = String(row.getAttribute('data-target') || '');
    if (!scope || !target) continue;
    var enabledEl = row.querySelector('.cag-access-enabled');
    if (!enabledEl) continue;
    var instEl = row.querySelector('.cag-access-instances');
    var turnEl = row.querySelector('.cag-access-turns');
    var saved = cagOrg.access[cagKey(scope, target)] || {};
    var enabled = String(enabledEl.value || 'inherit');
    var instances = instEl ? String(instEl.value == null ? '' : instEl.value) : '';
    var turns = turnEl ? String(turnEl.value == null ? '' : turnEl.value) : '';
    var savedEnabled = saved.enabled || 'inherit';
    var savedInstances = saved.max_instances == null ? '' : String(saved.max_instances);
    var savedTurns = saved.max_concurrent_turns == null ? '' : String(saved.max_concurrent_turns);
    if (enabled === savedEnabled && instances === savedInstances && turns === savedTurns) continue;
    drafts[cagKey(scope, target)] = { enabled: enabled, instances: instances, turns: turns };
  }
  return drafts;
}

function cagFocusMark(root) {
  if (!document || !document.activeElement || !root || typeof root.contains !== 'function') return null;
  var active = document.activeElement;
  if (!root.contains(active) || typeof active.closest !== 'function') return null;
  var row = active.closest('.cag-org-row');
  if (!row || typeof row.getAttribute !== 'function') return null;
  var field = '';
  var classes = active.classList;
  if (classes && classes.contains('cag-access-enabled')) field = 'enabled';
  else if (classes && classes.contains('cag-access-instances')) field = 'instances';
  else if (classes && classes.contains('cag-access-turns')) field = 'turns';
  if (!field) return null;
  return { scope: row.getAttribute('data-scope'), target: row.getAttribute('data-target'), field: field };
}

function cagRestoreFocus(root, mark) {
  if (!mark || !root || typeof root.querySelectorAll !== 'function') return;
  var rows = root.querySelectorAll('.cag-org-row') || [];
  for (var i = 0; i < rows.length; i += 1) {
    var row = rows[i];
    if (!row || typeof row.getAttribute !== 'function' || typeof row.querySelector !== 'function') continue;
    if (row.getAttribute('data-scope') !== mark.scope || row.getAttribute('data-target') !== mark.target) continue;
    var field = row.querySelector('.cag-access-' + mark.field);
    if (field && typeof field.focus === 'function') field.focus();
    return;
  }
}

function cagToggleDepartment(id) {
  if (!id) return;
  if (cagQuery()) {
    if (!cagMembersReady(id)) {
      cagOrg.expanded[id] = true;
      cagLoadMembers(id).then(function () { renderCloudAgentAccessTree(); });
    }
    return;
  }
  cagOrg.expanded[id] = !cagOrg.expanded[id];
  renderCloudAgentAccessTree();
  if (cagOrg.expanded[id]) cagLoadMembers(id).then(function () { renderCloudAgentAccessTree(); });
}

function cagBindTree(root) {
  if (!root || typeof root.addEventListener !== 'function') return;
  if (root.dataset && root.dataset.bound === '1') return;
  if (root.dataset) root.dataset.bound = '1';
  root.addEventListener('click', function (event) {
    var target = event.target;
    if (!target || typeof target.closest !== 'function') return;
    var toggle = target.closest('.cag-org-toggle');
    if (toggle) {
      cagToggleDepartment(toggle.getAttribute('data-dept'));
      return;
    }
    var save = target.closest('.cag-access-save');
    if (!save) return;
    var row = save.closest('.cag-org-row');
    if (row) saveCloudAgentAccessFromRow(row);
  });
}

function renderCloudAgentAccessTree() {
  var root = document.getElementById('cloudAgentAccessTree');
  if (!root) return;
  cagEnsureStyle();
  cagBindTree(root);
  var t = cloudAgentQuotaText();
  if (!cagOrg.loaded) {
    var loading = '<div class="cag-org-note">' + cagEscape(t.loading) + '</div>';
    root.innerHTML = loading;
    root._cagDrawn = loading;
    return;
  }
  var query = cagQuery();
  cagOrg.drafts = cagOrg.suspendDrafts ? {} : cagCaptureDrafts(root);
  var html = cagRenderNodes(cagOrg.tree, 0, query);
  var shown = cagVisibleUserIds(query);
  var hits = cagMatchingUsers(query).filter(function (user) {
    return !shown[String(user.id || '').trim()];
  });
  if (hits.length) {
    html += '<div class="cag-org-note">' + cagEscape(t.users) + '</div>';
    hits.forEach(function (user) {
      html += cagRow('user', user.id, cagUserLabel(user), 0, true, '');
    });
  }
  if (!html) html = '<div class="cag-org-note">' + cagEscape(t.empty) + '</div>';
  cagOrg.drafts = {};
  if (root._cagDrawn !== html) {
    var mark = cagFocusMark(root);
    var top = root.scrollTop || 0;
    root.innerHTML = html;
    root._cagDrawn = html;
    if (top) root.scrollTop = top;
    cagRestoreFocus(root, mark);
  }
  cagEnsureOpenMembers(query);
}

function cagEnsureOpenMembers(query) {
  var ids = [];
  var scan = cagQueryWantsMembers(query);
  function walk(nodes) {
    (nodes || []).forEach(function (node) {
      if (!node || !cagDeptMatch(node, query)) return;
      var expanded = !!cagOrg.expanded[node.id];
      if (!expanded && !query) return;
      if ((expanded || scan) && !cagMembersReady(node.id) && !cagOrg.memberFailed[node.id] && !cagOrg.memberLoads[node.id]) ids.push(node.id);
      walk(node.children);
    });
  }
  walk(cagOrg.tree);
  if (!ids.length) return;
  var epoch = cagOrg.memberEpoch;
  Promise.all(ids.slice(0, 30).map(cagLoadMembers)).then(function () {
    if (epoch !== cagOrg.memberEpoch) return;
    renderCloudAgentAccessTree();
  });
}

function cagInvalidateMembers() {
  cagOrg.memberEpoch = (cagOrg.memberEpoch || 0) + 1;
  cagOrg.members = {};
  cagOrg.memberFailed = {};
  cagOrg.memberLoads = {};
}

function cagLoadMembers(groupId) {
  var id = String(groupId || '').trim();
  if (!id) return Promise.resolve();
  if (cagMembersReady(id)) return Promise.resolve(cagOrg.members[id]);
  if (cagOrg.memberLoads[id]) return cagOrg.memberLoads[id];
  var epoch = cagOrg.memberEpoch || 0;
  delete cagOrg.memberFailed[id];
  var pending = api('/api/admin/security/groups/' + encodeURIComponent(id) + '/members').then(function (data) {
    if (epoch !== cagOrg.memberEpoch) return;
    var seen = {};
    cagOrg.members[id] = ((data && data.members) || []).map(function (email) {
      return String(email || '').trim();
    }).filter(function (email) {
      var key = email.toLowerCase();
      if (!key || seen[key]) return false;
      seen[key] = true;
      return true;
    });
    delete cagOrg.memberFailed[id];
  }).catch(function () {
    if (epoch !== cagOrg.memberEpoch) return;
    cagOrg.memberFailed[id] = true;
    delete cagOrg.members[id];
  }).then(function () {
    if (epoch !== cagOrg.memberEpoch) return;
    if (cagOrg.memberLoads[id] === pending) delete cagOrg.memberLoads[id];
  });
  cagOrg.memberLoads[id] = pending;
  return pending;
}

async function loadCloudAgentAccess(discardDrafts) {
  applyCloudAgentQuotaI18n();
  var status = document.getElementById('cloudAgentAccessStatus');
  var tree = document.getElementById('cloudAgentAccessTree');
  if (!tree) return null;
  var loadEpoch = (cagOrg.loadEpoch || 0) + 1;
  cagOrg.loadEpoch = loadEpoch;
  var dropDrafts = discardDrafts === true;
  if (dropDrafts) cagOrg.suspendDrafts = loadEpoch;
  if (!cagOrg.loaded && status) status.textContent = cloudAgentQuotaText().loading;
  try {
    var usersFailed = false;
    var results = await Promise.all([
      api('/api/admin/cloud-agents/access'),
      api('/api/admin/security/groups'),
      api('/api/admin/users').then(function (data) { return data || { users: [] }; }, function () {
        usersFailed = true;
        return { users: [] };
      })
    ]);
    if (loadEpoch !== cagOrg.loadEpoch) return null;
    if (!dropDrafts) cagOrg.suspendDrafts = 0;
    cagInvalidateMembers();
    var items = (results[0] && results[0].items) || [];
    cagOrg.access = {};
    items.forEach(function (row) {
      if (!row || !row.scope) return;
      cagOrg.access[cagKey(row.scope, row.target_id)] = row;
    });
    var rawTree = results[1] && results[1].tree;
    cagOrg.tree = cagNormalize(Array.isArray(rawTree) ? rawTree : (rawTree ? [rawTree] : []));
    cagIndexUsers((results[2] && results[2].users) || []);
    cagOrg.loaded = true;
    if (!Object.keys(cagOrg.expanded).length && cagOrg.tree[0]) cagOrg.expanded[cagOrg.tree[0].id] = true;
    renderCloudAgentAccessTree();
    var open = Object.keys(cagOrg.expanded).filter(function (id) { return cagOrg.expanded[id]; });
    await Promise.all(open.slice(0, 30).map(cagLoadMembers));
    if (loadEpoch !== cagOrg.loadEpoch) return null;
    renderCloudAgentAccessTree();
    if (status) status.textContent = usersFailed ? cloudAgentQuotaText().usersFailed : '';
    return results[0] || { items: [] };
  } catch (err) {
    if (loadEpoch !== cagOrg.loadEpoch) return null;
    var msg = cloudAgentQuotaText().accessLoadFailed.replace('{error}', err && err.message ? err.message : String(err));
    if (status) status.textContent = msg;
    if (typeof showToast === 'function') showToast(msg, 'error');
    return null;
  } finally {
    if (cagOrg.suspendDrafts === loadEpoch) cagOrg.suspendDrafts = 0;
  }
}

function cagWriteAccess(scope, target, enabled, instances, turns) {
  var t = cloudAgentQuotaText();
  if (instances === undefined || turns === undefined) {
    if (typeof showToast === 'function') showToast(t.bad, 'error');
    return Promise.resolve();
  }
  return api('/api/admin/cloud-agents/access', {
    method: 'PUT',
    body: JSON.stringify({
      scope: scope,
      target_id: target,
      enabled: enabled,
      max_instances: instances,
      max_concurrent_turns: turns
    })
  }).then(function () {
    return loadCloudAgentAccess();
  }).then(function (reloaded) {
    if (!reloaded) return;
    if (typeof showToast === 'function') showToast(t.accessSaved, 'success');
  }, function (err) {
    var msg = t.accessFailed.replace('{error}', err && err.message ? err.message : String(err));
    if (typeof showToast === 'function') showToast(msg, 'error');
  });
}

function saveCloudAgentAccessRow(scope, target, enabled, instances, turns) {
  var next = cagOrg.saveChain.then(function () {
    return cagWriteAccess(scope, target, enabled, instances, turns);
  });
  cagOrg.saveChain = next.then(function () {}, function () {});
  return next;
}

function saveCloudAgentAccessFromRow(row) {
  if (!row || typeof row.getAttribute !== 'function' || typeof row.querySelector !== 'function') return;
  var enabledEl = row.querySelector('.cag-access-enabled');
  var instEl = row.querySelector('.cag-access-instances');
  var turnEl = row.querySelector('.cag-access-turns');
  return saveCloudAgentAccessRow(
    row.getAttribute('data-scope'),
    row.getAttribute('data-target'),
    enabledEl ? enabledEl.value : 'inherit',
    cagReadLimit(instEl ? instEl.value : ''),
    cagReadLimit(turnEl ? turnEl.value : '')
  );
}

async function loadCloudAgentQuota() {
  applyCloudAgentQuotaI18n();
  var status = document.getElementById('cloudAgentQuotaStatus');
  try {
    var data = await api('/api/admin/cloud-agents/settings');
    var enabled = document.getElementById('cloudAgentQuotaEnabled');
    var instances = document.getElementById('cloudAgentQuotaInstances');
    var turns = document.getElementById('cloudAgentQuotaTurns');
    if (enabled) enabled.checked = !!(data && data.cloud_agents_enabled);
    if (instances) instances.value = data && data.max_instances != null ? String(data.max_instances) : '3';
    if (turns) turns.value = data && data.max_concurrent_turns != null ? String(data.max_concurrent_turns) : '1';
    if (status) status.textContent = '';
    await loadCloudAgentAccess(true);
    return data;
  } catch (err) {
    var msg = cloudAgentQuotaText().loadFailed.replace('{error}', err.message || String(err));
    if (status) status.textContent = msg;
    if (typeof showToast === 'function') showToast(msg, 'error');
    return null;
  }
}

async function saveCloudAgentQuota() {
  var t = cloudAgentQuotaText();
  var instances = cloudAgentQuotaInt('cloudAgentQuotaInstances');
  var turns = cloudAgentQuotaInt('cloudAgentQuotaTurns');
  if (instances == null || turns == null || instances === undefined || turns === undefined) {
    if (typeof showToast === 'function') showToast(t.bad, 'error');
    return;
  }
  var enabled = document.getElementById('cloudAgentQuotaEnabled');
  var body = {
    cloud_agents_enabled: !!(enabled && enabled.checked),
    max_instances: instances,
    max_concurrent_turns: turns
  };
  var button = document.getElementById('cloudAgentQuotaSaveBtn');
  if (button) button.disabled = true;
  try {
    await api('/api/admin/cloud-agents/settings', { method: 'PATCH', body: JSON.stringify(body) });
    await loadCloudAgentQuota();
    if (typeof showToast === 'function') showToast(t.saved, 'success');
  } catch (err) {
    var msg = t.saveFailed.replace('{error}', err.message || String(err));
    if (typeof showToast === 'function') showToast(msg, 'error');
  } finally {
    if (button) button.disabled = false;
  }
}

if (typeof window !== 'undefined' && window.AdminTabRegistry && typeof window.AdminTabRegistry.registerTab === 'function') {
  window.AdminTabRegistry.registerTab({
    id: 'cloud-agents',
    title: function () { return cloudAgentQuotaText().nav; },
    subtitle: function () { return cloudAgentQuotaText().navDesc; },
    onOpen: function () { loadCloudAgentQuota(); }
  });
}
if (typeof window !== 'undefined' && window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(applyCloudAgentQuotaI18n);
}
