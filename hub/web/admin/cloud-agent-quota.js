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
    accessLoadFailed: 'Load opening scope failed: {error}',
    grantsTitle: 'Granted access',
    grantsHint: 'Saved department and user overrides. Revoke deletes the override so the level above applies again.',
    grantsEmpty: 'No department or user overrides yet. Pick one in the organization tree below.',
    scopeDept: 'Department',
    scopeUser: 'User',
    revoke: 'Revoke',
    revokeTitle: 'Revoke access',
    revokeConfirm: 'Remove the saved override for {name}?',
    revoked: 'Override removed',
    revokeFailed: 'Revoke failed: {error}',
    locate: 'Show in tree',
    colTarget: 'Department / member',
    colAccess: 'Access',
    colActions: 'Actions',
    custom: 'Custom',
    srvTitle: 'maclawsrv bindings',
    srvHint: 'Users bound to a private maclawsrv run their cloud tasks on it. Everyone else uses the system default.',
    srvEmpty: 'No private bindings. All users run on the system default maclawsrv.',
    srvPrivate: 'Private',
    srvOnline: 'online',
    srvOffline: 'offline',
    srvLoadFailed: 'Load bindings failed: {error}',
    srvLabel: 'Default maclawsrv',
    srvCol: 'maclawsrv',
    srvDefault: 'System default',
    srvInherit: 'Inherit'
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
    accessLoadFailed: '\u8bfb\u53d6\u5f00\u901a\u8303\u56f4\u5931\u8d25\uff1a{error}',
    grantsTitle: '\u5df2\u5f00\u901a\u8bb0\u5f55',
    grantsHint: '\u5df2\u4fdd\u5b58\u7684\u90e8\u95e8\u548c\u7528\u6237\u8bbe\u7f6e\u3002\u64a4\u9500\u4f1a\u5220\u9664\u8fd9\u6761\u8bbe\u7f6e\uff0c\u91cd\u65b0\u7ee7\u627f\u4e0a\u4e00\u7ea7\u3002',
    grantsEmpty: '\u8fd8\u6ca1\u6709\u90e8\u95e8\u6216\u7528\u6237\u7684\u5f00\u901a\u8bb0\u5f55\u3002\u5728\u4e0b\u65b9\u7ec4\u7ec7\u673a\u6784\u91cc\u9009\u62e9\u3002',
    scopeDept: '\u90e8\u95e8',
    scopeUser: '\u7528\u6237',
    revoke: '\u64a4\u9500',
    revokeTitle: '\u64a4\u9500\u5f00\u901a',
    revokeConfirm: '\u786e\u5b9a\u64a4\u9500 {name} \u7684\u5f00\u901a\u8bbe\u7f6e\uff1f',
    revoked: '\u5df2\u64a4\u9500',
    revokeFailed: '\u64a4\u9500\u5931\u8d25\uff1a{error}',
    locate: '\u5728\u6811\u4e2d\u5b9a\u4f4d',
    colTarget: '\u90e8\u95e8 / \u6210\u5458',
    colAccess: '\u72b6\u6001',
    colActions: '\u64cd\u4f5c',
    custom: '\u5df2\u81ea\u5b9a\u4e49',
    srvTitle: 'maclawsrv \u7ed1\u5b9a',
    srvHint: '\u7ed1\u5b9a\u7528\u6237\u7684\u4e91\u7aef\u4efb\u52a1\u8d70\u81ea\u5df1\u7684\u79c1\u6709 maclawsrv\uff0c\u5176\u4f59\u7528\u6237\u4f7f\u7528\u7cfb\u7edf\u7f3a\u7701\u3002',
    srvEmpty: '\u6682\u65e0\u79c1\u6709\u7ed1\u5b9a\uff0c\u5168\u90e8\u7528\u6237\u4f7f\u7528\u7cfb\u7edf\u7f3a\u7701 maclawsrv\u3002',
    srvPrivate: '\u79c1\u6709',
    srvOnline: '\u5728\u7ebf',
    srvOffline: '\u79bb\u7ebf',
    srvLoadFailed: '\u8bfb\u53d6\u7ed1\u5b9a\u5931\u8d25\uff1a{error}',
    srvLabel: '\u9ed8\u8ba4 maclawsrv',
    srvCol: 'maclawsrv',
    srvDefault: '\u7cfb\u7edf\u9ed8\u8ba4',
    srvInherit: '\u7ee7\u627f'
  }
};

var cagOrg = {
  tree: [],
  deptNames: {},
  users: [],
  usersByEmail: {},
  usersById: {},
  access: {},
  members: {},
  memberLoads: {},
  memberFailed: {},
  expanded: {},
  srvs: [],
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
  set('cloudAgentQuotaSrvLabel', t.srvLabel);
  cagRenderQuotaSrv();
  set('cloudAgentQuotaSaveBtn', t.save);
  set('cloudAgentQuotaReloadBtn', t.reload);
  set('cloudAgentAccessTitle', t.accessTitle);
  set('cloudAgentAccessHint', t.accessHint);
  set('cloudAgentAccessReloadBtn', t.reload);
  set('cloudAgentGrantsTitle', t.grantsTitle);
  set('cloudAgentGrantsHint', t.grantsHint);
  set('cloudAgentSrvTitle', t.srvTitle);
  set('cloudAgentSrvHint', t.srvHint);
  set('cloudAgentSrvReloadBtn', t.reload);
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
    + '#tab-cloud-agents.active{display:grid;gap:14px;align-content:start}'
    + '#tab-cloud-agents .cag-card{padding:16px 18px}'
    + '#tab-cloud-agents .cag-card-head{margin-bottom:14px}'
    + '#tab-cloud-agents .cag-card-head .item-meta{max-width:640px}'
    + '#tab-cloud-agents .cag-status{margin-top:8px;font-size:12px;color:var(--muted,#667386);min-height:16px}'
    + '#tab-cloud-agents .cag-quota-body{display:flex;flex-wrap:wrap;gap:16px 28px;align-items:flex-end}'
    + '#tab-cloud-agents .cag-quota-fields{display:flex;gap:14px;flex-wrap:wrap}'
    + '#tab-cloud-agents .cag-field label{display:block;margin:0 0 5px;font-size:10px;font-weight:800;letter-spacing:.05em;text-transform:uppercase;color:var(--muted,#667386)}'
    + '#tab-cloud-agents .cag-field input{width:120px;height:38px}'
    + '#tab-cloud-agents .cag-field select{width:190px;height:38px}'
    + '#tab-cloud-agents .cag-quota-side{margin-left:auto;display:flex;align-items:center;gap:12px}'
    + '#tab-cloud-agents .cag-quota-side .cag-status{margin:0}'
    + '#tab-cloud-agents .cag-switch{display:inline-flex;align-items:center;gap:10px;padding-bottom:8px;cursor:pointer;font-size:13px;font-weight:650;color:var(--text,#202733)}'
    + '#tab-cloud-agents .cag-switch input{position:absolute;width:1px;height:1px;opacity:0}'
    + '#tab-cloud-agents .cag-switch-track{position:relative;width:38px;height:22px;flex:0 0 auto;border-radius:999px;background:#cbd5e1;transition:background .15s ease}'
    + '#tab-cloud-agents .cag-switch-track::after{content:"";position:absolute;left:3px;top:3px;width:16px;height:16px;border-radius:50%;background:#fff;box-shadow:0 1px 3px rgba(15,23,42,.3);transition:left .15s ease}'
    + '#tab-cloud-agents .cag-switch input:checked+.cag-switch-track{background:var(--primary,#2563eb)}'
    + '#tab-cloud-agents .cag-switch input:checked+.cag-switch-track::after{left:19px}'
    + '#tab-cloud-agents .cag-switch input:focus-visible+.cag-switch-track{outline:2px solid var(--primary,#2563eb);outline-offset:2px}'
    + '#tab-cloud-agents .cag-grant-count{min-width:22px;height:22px;padding:0 7px;border-radius:999px;background:var(--primary-soft,rgba(37,99,235,.1));color:var(--primary,#2563eb);font-size:11px;font-weight:700;display:inline-flex;align-items:center;justify-content:center;align-self:center}'
    + '#tab-cloud-agents .cag-grant-list{display:grid;gap:8px}'
    + '#tab-cloud-agents .cag-grant{display:grid;grid-template-columns:minmax(0,1fr) auto auto auto;gap:10px;align-items:center;padding:9px 12px;border:1px solid var(--line,rgba(28,39,55,.11));border-radius:10px;background:var(--bg-2,#fbfcfe)}'
    + '#tab-cloud-agents .cag-grant-main{display:flex;align-items:center;gap:10px;min-width:0;cursor:pointer}'
    + '#tab-cloud-agents .cag-grant-ic{width:30px;height:30px;flex:0 0 auto;border-radius:8px;border:1px solid #dbe7ff;background:#eef5ff center/16px no-repeat}'
    + '#tab-cloud-agents .cag-grant-ic-dept{background-image:var(--icon-building)}'
    + '#tab-cloud-agents .cag-grant-ic-user{background-image:var(--icon-user)}'
    + '#tab-cloud-agents .cag-grant-id{min-width:0}'
    + '#tab-cloud-agents .cag-grant-name{font-size:13px;font-weight:650;color:var(--text,#202733);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
    + '#tab-cloud-agents .cag-grant-meta{margin-top:2px;font-size:11px;color:var(--muted,#667386);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
    + '#tab-cloud-agents .cag-state{padding:3px 10px;border-radius:999px;font-size:11px;font-weight:700;white-space:nowrap}'
    + '#tab-cloud-agents .cag-state-on{background:rgba(20,125,88,.1);color:var(--ok,#147d58)}'
    + '#tab-cloud-agents .cag-state-off{background:rgba(194,65,58,.09);color:var(--danger,#c2413a)}'
    + '#tab-cloud-agents .cag-state-inherit{background:var(--soft-2,#eef3f8);color:var(--muted,#667386)}'
    + '#tab-cloud-agents .cag-grant-locate,#tab-cloud-agents .cag-grant-revoke{height:30px;min-height:30px;padding:0 10px;font-size:12px;white-space:nowrap}'
    + '#tab-cloud-agents .cag-grant-revoke{color:var(--danger,#c2413a)}'
    + '#tab-cloud-agents .cag-grant-revoke:hover{background:rgba(194,65,58,.07);border-color:rgba(194,65,58,.3)}'
    + '#tab-cloud-agents .cag-grants-empty{padding:16px;border:1px dashed rgba(28,39,55,.18);border-radius:10px;background:var(--bg-2,#fbfcfe);color:var(--muted,#667386);font-size:12px;text-align:center}'
    + '#tab-cloud-agents .cag-filter{position:relative;max-width:400px;margin-bottom:10px}'
    + '#tab-cloud-agents .cag-filter input{width:100%;height:36px;padding-left:32px;border-radius:9px}'
    + '#tab-cloud-agents .cag-filter::before{content:"";position:absolute;left:10px;top:50%;transform:translateY(-50%);width:14px;height:14px;background:var(--icon-search) center/contain no-repeat;opacity:.4;pointer-events:none}'
    + '#tab-cloud-agents .cag-org-tree{max-height:480px;overflow:auto;border:1px solid var(--line,rgba(28,39,55,.11));border-radius:10px;padding:4px;background:var(--bg-2,#fbfcfe)}'
    + '#tab-cloud-agents .cag-org-head,#tab-cloud-agents .cag-org-row{display:grid;grid-template-columns:minmax(0,1fr) 126px 148px 92px 92px 72px;gap:8px;align-items:center;min-width:800px}'
    + '#tab-cloud-agents .cag-org-head{position:sticky;top:0;z-index:2;margin:0 2px;padding:7px 10px;border-bottom:1px solid var(--line,rgba(28,39,55,.11));background:var(--bg-2,#fbfcfe);font-size:10px;font-weight:800;letter-spacing:.05em;text-transform:uppercase;color:var(--muted,#667386)}'
    + '#tab-cloud-agents .cag-org-row{min-height:38px;padding:2px 8px;border-radius:8px}'
    + '#tab-cloud-agents .cag-org-row:hover{background:#eef5ff}'
    + '#tab-cloud-agents .cag-org-row.is-dirty{background:#f5f9ff}'
    + '#tab-cloud-agents .cag-org-namecell{display:flex;align-items:center;gap:7px;min-width:0}'
    + '#tab-cloud-agents .cag-org-namecell:only-child{grid-column:1/-1}'
    + '#tab-cloud-agents .cag-org-toggle{width:22px;min-width:22px;height:22px;min-height:22px;padding:0;border:0;background:transparent;box-shadow:none;color:#64748b;font-size:11px;flex:0 0 auto}'
    + '#tab-cloud-agents .cag-org-pad{width:22px;min-width:22px;flex:0 0 auto}'
    + '#tab-cloud-agents .cag-org-ic{width:15px;height:15px;flex:0 0 auto;background:center/contain no-repeat;opacity:.5}'
    + '#tab-cloud-agents .cag-org-ic-dept{background-image:var(--icon-building)}'
    + '#tab-cloud-agents .cag-org-ic-user{background-image:var(--icon-user)}'
    + '#tab-cloud-agents .cag-org-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px;color:var(--text,#202733)}'
    + '#tab-cloud-agents .cag-org-user .cag-org-name{font-size:12px;font-weight:400;color:var(--muted,#667386)}'
    + '#tab-cloud-agents .cag-org-dot{width:6px;height:6px;flex:0 0 auto;border-radius:50%;background:var(--primary,#2563eb)}'
    + '#tab-cloud-agents .cag-access-enabled{width:100%;height:30px;min-height:30px;font-size:12px;border-radius:8px}'
    + '#tab-cloud-agents .cag-access-srv{width:100%;height:30px;min-height:30px;font-size:12px;border-radius:8px}'
    + '#tab-cloud-agents .cag-access-enabled.cag-state-on{border-color:rgba(20,125,88,.45);background:rgba(20,125,88,.07);color:var(--ok,#147d58);font-weight:650}'
    + '#tab-cloud-agents .cag-access-enabled.cag-state-off{border-color:rgba(194,65,58,.4);background:rgba(194,65,58,.06);color:var(--danger,#c2413a);font-weight:650}'
    + '#tab-cloud-agents .cag-access-instances,#tab-cloud-agents .cag-access-turns{width:100%;height:30px;min-height:30px;font-size:12px}'
    + '#tab-cloud-agents .cag-access-save{height:30px;min-height:30px;padding:0 10px;font-size:12px;visibility:hidden}'
    + '#tab-cloud-agents .cag-org-row.is-dirty .cag-access-save{visibility:visible}'
    + '#tab-cloud-agents .cag-org-note{margin:5px 10px;color:var(--muted,#667386);font-size:12px}';
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

/* The name a srv pin shows. An empty id is the public system default;
   an id no longer registered still renders as itself so a stale pin is
   visible instead of silently reading as the default. */
function cagSrvName(id) {
  var t = cloudAgentQuotaText();
  if (!id) return t.srvDefault;
  var found = null;
  (cagOrg.srvs || []).forEach(function (item) {
    if (item && String(item.id || '') === String(id)) found = item;
  });
  if (!found) return String(id);
  return String(found.name || found.id || '');
}

/* Picker options for one scope row: inherit, the system default, then
   every registered srv and node srv. */
function cagSrvOptions(current) {
  var t = cloudAgentQuotaText();
  var html = cagOption('inherit', t.srvInherit, current) + cagOption('', t.srvDefault, current);
  (cagOrg.srvs || []).forEach(function (item) {
    if (!item || !item.id) return;
    var label = String(item.name || item.id);
    if (item.kind === 'node' && item.online === false) label += ' (' + t.srvOffline + ')';
    html += cagOption(String(item.id), label, current);
  });
  if (current !== 'inherit' && current !== '' && current != null) {
    var seen = false;
    (cagOrg.srvs || []).forEach(function (item) {
      if (item && String(item.id || '') === String(current)) seen = true;
    });
    if (!seen) html += cagOption(String(current), String(current), current);
  }
  return html;
}

/* The global picker has no inherit level: default is the effective
   system default - the only usable srv when there is one, else the
   public connection. A pin naming a deleted srv stays visible so it
   does not silently read as the default. */
function cagSrvOptionsGlobal(current) {
  var t = cloudAgentQuotaText();
  var html = cagOption('', t.srvDefault, current);
  var seen = current === '' || current == null;
  (cagOrg.srvs || []).forEach(function (item) {
    if (!item || !item.id) return;
    var label = String(item.name || item.id);
    if (item.kind === 'node' && item.online === false) label += ' (' + t.srvOffline + ')';
    if (String(item.id) === String(current)) seen = true;
    html += cagOption(String(item.id), label, current);
  });
  if (!seen) html += cagOption(String(current), String(current), current);
  return html;
}

function cagRenderQuotaSrv() {
  var el = document.getElementById('cloudAgentQuotaSrv');
  if (!el) return;
  var current = el._cagSrv != null ? el._cagSrv : '';
  el.innerHTML = cagSrvOptionsGlobal(current);
  el.value = current;
}

function cagControls(scope, target) {
  var t = cloudAgentQuotaText();
  var row = cagOrg.access[cagKey(scope, target)] || {};
  var draft = cagOrg.drafts[cagKey(scope, target)];
  var enabled = draft ? draft.enabled : (row.enabled || 'inherit');
  var instances = draft ? draft.instances : (row.max_instances == null ? '' : String(row.max_instances));
  var turns = draft ? draft.turns : (row.max_concurrent_turns == null ? '' : String(row.max_concurrent_turns));
  var srv = draft ? draft.srv : (row.srv_id == null ? 'inherit' : String(row.srv_id));
  return '<select class="cag-access-enabled cag-state-' + cagEscape(enabled) + '" aria-label="' + cagEscape(t.accessTitle) + '">'
    + cagOption('inherit', t.inherit, enabled)
    + cagOption('on', t.on, enabled)
    + cagOption('off', t.off, enabled)
    + '</select>'
    + '<select class="cag-access-srv" aria-label="' + cagEscape(t.srvCol) + '">'
    + cagSrvOptions(srv)
    + '</select>'
    + '<input class="cag-access-instances" type="number" min="0" step="1" placeholder="' + cagEscape(t.limitInherit) + '" value="' + cagEscape(instances) + '" aria-label="' + cagEscape(t.instances) + '">'
    + '<input class="cag-access-turns" type="number" min="0" step="1" placeholder="' + cagEscape(t.limitInherit) + '" value="' + cagEscape(turns) + '" aria-label="' + cagEscape(t.turns) + '">'
    + '<button type="button" class="btn-secondary cag-access-save">' + cagEscape(t.save) + '</button>';
}

function cagRowHasOverride(scope, target) {
  var saved = cagOrg.access[cagKey(scope, target)] || {};
  return !!(saved.enabled && saved.enabled !== 'inherit') || saved.max_instances != null || saved.max_concurrent_turns != null || saved.srv_id != null;
}

function cagRow(scope, target, name, depth, userRow, toggle) {
  var dirty = !!cagOrg.drafts[cagKey(scope, target)];
  var dot = cagRowHasOverride(scope, target) ? '<span class="cag-org-dot" title="' + cagEscape(cloudAgentQuotaText().custom) + '"></span>' : '';
  return '<div class="cag-org-row' + (userRow ? ' cag-org-user' : '') + (dirty ? ' is-dirty' : '') + '" data-scope="' + cagEscape(scope) + '" data-target="' + cagEscape(target) + '" style="margin-left:' + (depth * 16) + 'px">'
    + '<div class="cag-org-namecell">'
    + (toggle || '<span class="cag-org-pad"></span>')
    + '<span class="cag-org-ic ' + (userRow ? 'cag-org-ic-user' : 'cag-org-ic-dept') + '" aria-hidden="true"></span>'
    + '<span class="cag-org-name">' + cagEscape(name) + '</span>'
    + dot
    + '</div>'
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
          memberHtml += '<div class="cag-org-row cag-org-user" style="margin-left:' + ((depth + 1) * 16) + 'px"><div class="cag-org-namecell"><span class="cag-org-pad"></span><span class="cag-org-ic cag-org-ic-user" aria-hidden="true"></span><span class="cag-org-name">' + cagEscape(member.label || member.email) + '</span></div></div>';
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

function cagRowDraft(row) {
  if (!row || typeof row.getAttribute !== 'function' || typeof row.querySelector !== 'function') return null;
  var scope = String(row.getAttribute('data-scope') || '');
  var target = String(row.getAttribute('data-target') || '');
  if (!scope || !target) return null;
  var enabledEl = row.querySelector('.cag-access-enabled');
  if (!enabledEl) return null;
  var instEl = row.querySelector('.cag-access-instances');
  var turnEl = row.querySelector('.cag-access-turns');
  var srvEl = row.querySelector('.cag-access-srv');
  var saved = cagOrg.access[cagKey(scope, target)] || {};
  var enabled = String(enabledEl.value || 'inherit');
  var instances = instEl ? String(instEl.value == null ? '' : instEl.value) : '';
  var turns = turnEl ? String(turnEl.value == null ? '' : turnEl.value) : '';
  var srv = srvEl ? String(srvEl.value || 'inherit') : 'inherit';
  var savedEnabled = saved.enabled || 'inherit';
  var savedInstances = saved.max_instances == null ? '' : String(saved.max_instances);
  var savedTurns = saved.max_concurrent_turns == null ? '' : String(saved.max_concurrent_turns);
  var savedSrv = saved.srv_id == null ? 'inherit' : String(saved.srv_id);
  if (enabled === savedEnabled && instances === savedInstances && turns === savedTurns && srv === savedSrv) return null;
  return { scope: scope, target: target, enabled: enabled, instances: instances, turns: turns, srv: srv };
}

function cagCaptureDrafts(root) {
  var drafts = {};
  if (!root || typeof root.querySelectorAll !== 'function') return drafts;
  var rows = root.querySelectorAll('.cag-org-row') || [];
  for (var i = 0; i < rows.length; i += 1) {
    var draft = cagRowDraft(rows[i]);
    if (draft) drafts[cagKey(draft.scope, draft.target)] = draft;
  }
  return drafts;
}

function cagSyncSelectState(select) {
  if (!select || !select.classList) return;
  select.classList.remove('cag-state-inherit');
  select.classList.remove('cag-state-on');
  select.classList.remove('cag-state-off');
  select.classList.add('cag-state-' + (select.value || 'inherit'));
}

function cagRowEdited(event) {
  var target = event && event.target;
  if (!target || typeof target.closest !== 'function') return;
  var row = target.closest('.cag-org-row');
  if (!row) return;
  if (row.classList && typeof row.classList.toggle === 'function') row.classList.toggle('is-dirty', !!cagRowDraft(row));
  var select = typeof row.querySelector === 'function' ? row.querySelector('.cag-access-enabled') : null;
  if (select) cagSyncSelectState(select);
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
  else if (classes && classes.contains('cag-access-srv')) field = 'srv';
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
  root.addEventListener('input', cagRowEdited);
  root.addEventListener('change', cagRowEdited);
}

function renderCloudAgentAccessTree() {
  var root = document.getElementById('cloudAgentAccessTree');
  if (!root) return;
  cagEnsureStyle();
  cagBindTree(root);
  cagRenderGrants();
  var t = cloudAgentQuotaText();
  if (!cagOrg.loaded) {
    var loading = '<div class="cag-org-note">' + cagEscape(t.loading) + '</div>';
    root.innerHTML = loading;
    root._cagDrawn = loading;
    return;
  }
  var query = cagQuery();
  cagOrg.drafts = cagOrg.suspendDrafts ? {} : cagCaptureDrafts(root);
  var rows = cagRenderNodes(cagOrg.tree, 0, query);
  var shown = cagVisibleUserIds(query);
  var hits = cagMatchingUsers(query).filter(function (user) {
    return !shown[String(user.id || '').trim()];
  });
  var extras = '';
  if (hits.length) {
    extras += '<div class="cag-org-note">' + cagEscape(t.users) + '</div>';
    hits.forEach(function (user) {
      extras += cagRow('user', user.id, cagUserLabel(user), 0, true, '');
    });
  }
  var html = '';
  if (rows || extras) {
    html = '<div class="cag-org-head"><span>' + cagEscape(t.colTarget) + '</span><span>' + cagEscape(t.colAccess) + '</span><span>' + cagEscape(t.srvCol) + '</span><span>' + cagEscape(t.instances) + '</span><span>' + cagEscape(t.turns) + '</span><span>' + cagEscape(t.colActions) + '</span></div>' + rows + extras;
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

function cagBuildDeptNames(nodes) {
  var map = {};
  (function walk(list) {
    (list || []).forEach(function (node) {
      if (!node || !node.id) return;
      map[node.id] = node.name;
      walk(node.children);
    });
  })(nodes);
  return map;
}

function cagGrantName(scope, target) {
  if (scope === 'department') return (cagOrg.deptNames || {})[target] || '';
  if (scope === 'user') return cagUserLabel(cagOrg.usersById[target]);
  return '';
}

function cagGrantRows() {
  var out = [];
  Object.keys(cagOrg.access || {}).forEach(function (key) {
    var row = cagOrg.access[key];
    if (!row || (row.scope !== 'department' && row.scope !== 'user')) return;
    out.push(row);
  });
  out.sort(function (a, b) {
    if (a.scope !== b.scope) return a.scope === 'department' ? -1 : 1;
    var an = cagGrantName(a.scope, a.target_id) || String(a.target_id || '');
    var bn = cagGrantName(b.scope, b.target_id) || String(b.target_id || '');
    return an.localeCompare(bn);
  });
  return out;
}

function cagGrantMeta(row, t) {
  var value = function (v) { return v == null ? t.limitInherit : String(v); };
  var meta = t.instances + ' ' + value(row.max_instances) + ' \u00b7 ' + t.turns + ' ' + value(row.max_concurrent_turns);
  if (row.srv_id != null) meta += ' \u00b7 ' + cagSrvName(row.srv_id);
  return meta;
}

function cagBindGrants(root) {
  if (!root || typeof root.addEventListener !== 'function') return;
  if (root.dataset && root.dataset.bound === '1') return;
  if (root.dataset) root.dataset.bound = '1';
  root.addEventListener('click', function (event) {
    var target = event.target;
    if (!target || typeof target.closest !== 'function') return;
    var row = target.closest('.cag-grant');
    if (!row || typeof row.getAttribute !== 'function') return;
    var scope = row.getAttribute('data-scope');
    var targetId = row.getAttribute('data-target');
    if (target.closest('.cag-grant-revoke')) {
      cagRevokeGrant(scope, targetId);
      return;
    }
    if (target.closest('.cag-grant-locate') || target.closest('.cag-grant-main')) cagLocateGrant(scope, targetId);
  });
  root.addEventListener('keydown', function (event) {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    var target = event.target;
    if (!target || typeof target.closest !== 'function' || !target.closest('.cag-grant-main')) return;
    var row = target.closest('.cag-grant');
    if (!row || typeof row.getAttribute !== 'function') return;
    event.preventDefault();
    cagLocateGrant(row.getAttribute('data-scope'), row.getAttribute('data-target'));
  });
}

function cagRenderGrants() {
  var list = document.getElementById('cloudAgentGrantList');
  if (!list) return;
  cagBindGrants(list);
  var t = cloudAgentQuotaText();
  var count = document.getElementById('cloudAgentGrantsCount');
  if (!cagOrg.loaded) {
    if (count) { count.textContent = '0'; count.hidden = true; }
    list.innerHTML = '<div class="cag-grants-empty">' + cagEscape(t.loading) + '</div>';
    return;
  }
  var rows = cagGrantRows();
  if (count) {
    count.textContent = String(rows.length);
    count.hidden = !rows.length;
  }
  if (!rows.length) {
    list.innerHTML = '<div class="cag-grants-empty">' + cagEscape(t.grantsEmpty) + '</div>';
    return;
  }
  var html = '';
  rows.forEach(function (row) {
    var scope = String(row.scope || '');
    var target = String(row.target_id || '');
    var enabled = String(row.enabled || 'inherit');
    var stateText = enabled === 'on' ? t.on : enabled === 'off' ? t.off : t.inherit;
    html += '<div class="cag-grant" data-scope="' + cagEscape(scope) + '" data-target="' + cagEscape(target) + '">'
      + '<div class="cag-grant-main" role="button" tabindex="0">'
      + '<span class="cag-grant-ic ' + (scope === 'user' ? 'cag-grant-ic-user' : 'cag-grant-ic-dept') + '" aria-hidden="true"></span>'
      + '<div class="cag-grant-id"><div class="cag-grant-name">' + cagEscape(cagGrantName(scope, target) || target) + '</div>'
      + '<div class="cag-grant-meta">' + cagEscape((scope === 'department' ? t.scopeDept : t.scopeUser) + ' \u00b7 ' + cagGrantMeta(row, t)) + '</div></div>'
      + '</div>'
      + '<span class="cag-state cag-state-' + cagEscape(enabled) + '">' + cagEscape(stateText) + '</span>'
      + '<button type="button" class="btn-ghost cag-grant-locate">' + cagEscape(t.locate) + '</button>'
      + '<button type="button" class="btn-ghost cag-grant-revoke">' + cagEscape(t.revoke) + '</button>'
      + '</div>';
  });
  list.innerHTML = html;
}

function cagLocateGrant(scope, target) {
  var filter = document.getElementById('cloudAgentAccessFilter');
  if (filter) filter.value = cagGrantName(scope, target) || String(target || '');
  renderCloudAgentAccessTree();
  var card = document.getElementById('cloudAgentAccessCard');
  if (card && typeof card.scrollIntoView === 'function') card.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  if (filter && typeof filter.focus === 'function') filter.focus();
}

function cagRevokeGrant(scope, target) {
  var t = cloudAgentQuotaText();
  var name = cagGrantName(scope, target) || String(target || '');
  var run = function () {
    return saveCloudAgentAccessRow(scope, target, 'inherit', null, null, null, { ok: t.revoked, fail: t.revokeFailed });
  };
  if (typeof AdminUI !== 'undefined' && AdminUI && typeof AdminUI.confirmDialog === 'function') {
    return AdminUI.confirmDialog(t.revokeConfirm.replace('{name}', name), { title: t.revokeTitle, confirmText: t.revoke, danger: true }).then(function (ok) {
      return ok ? run() : null;
    });
  }
  return run();
}

function cagRenderSrvBindings(items) {
  var list = document.getElementById('cloudAgentSrvList');
  if (!list) return;
  cagEnsureStyle();
  var t = cloudAgentQuotaText();
  if (!items || !items.length) {
    list.innerHTML = '<div class="cag-grants-empty">' + cagEscape(t.srvEmpty) + '</div>';
    return;
  }
  var html = '';
  items.forEach(function (item) {
    var label = cagUserLabel(cagOrg.usersById[String(item.user_id || '')]) || String(item.user_id || '');
    var nodeName = String(item.name || '') || String(item.node_id || '');
    var address = String(item.address || '');
    var meta = nodeName + (address && address !== nodeName ? ' \u00b7 ' + address : '');
    html += '<div class="cag-grant">'
      + '<div class="cag-grant-main"><span class="cag-grant-ic cag-grant-ic-user" aria-hidden="true"></span>'
      + '<div class="cag-grant-id"><div class="cag-grant-name">' + cagEscape(label) + '</div>'
      + '<div class="cag-grant-meta">' + cagEscape(t.srvPrivate + ' \u00b7 ' + meta) + '</div></div></div>'
      + '<span class="cag-state ' + (item.online ? 'cag-state-on' : 'cag-state-off') + '">' + cagEscape(item.online ? t.srvOnline : t.srvOffline) + '</span>'
      + '</div>';
  });
  list.innerHTML = html;
}

async function loadCloudAgentSrvBindings() {
  var list = document.getElementById('cloudAgentSrvList');
  if (!list) return;
  var t = cloudAgentQuotaText();
  try {
    var data = await api('/api/admin/cloud-agents/srv-bindings');
    cagRenderSrvBindings(data && data.items);
  } catch (err) {
    list.innerHTML = '<div class="cag-grants-empty">' + cagEscape(t.srvLoadFailed.replace('{error}', err.message || String(err))) + '</div>';
  }
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
      }),
      api('/api/admin/cloud-agents/srvs').then(function (data) { return (data && data.items) || []; }, function () {
        return [];
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
    cagOrg.deptNames = cagBuildDeptNames(cagOrg.tree);
    cagIndexUsers((results[2] && results[2].users) || []);
    cagOrg.srvs = Array.isArray(results[3]) ? results[3] : [];
    cagRenderQuotaSrv();
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

function cagWriteAccess(scope, target, enabled, instances, turns, srv, msgs) {
  var t = cloudAgentQuotaText();
  var okText = msgs && msgs.ok ? msgs.ok : t.accessSaved;
  var failText = msgs && msgs.fail ? msgs.fail : t.accessFailed;
  if (instances === undefined || turns === undefined) {
    if (typeof showToast === 'function') showToast(t.bad, 'error');
    return Promise.resolve();
  }
  // 'inherit' maps to a null srv_id; '' pins the system default.
  var srvId = srv === 'inherit' || srv === undefined ? null : srv;
  return api('/api/admin/cloud-agents/access', {
    method: 'PUT',
    body: JSON.stringify({
      scope: scope,
      target_id: target,
      enabled: enabled,
      max_instances: instances,
      max_concurrent_turns: turns,
      srv_id: srvId
    })
  }).then(function (saved) {
    // A successful PUT means the row is written; applying the echo locally
    // avoids a full access+groups+users+members reload after every save.
    // The server deletes a non-global row that inherits with blank limits.
    var key = cagKey(scope, target);
    var cleared = scope !== 'global' && (enabled === 'inherit' || enabled === '') && instances == null && turns == null && srvId == null;
    if (cleared || (saved && saved.deleted)) {
      delete cagOrg.access[key];
    } else {
      cagOrg.access[key] = {
        scope: scope,
        target_id: target,
        enabled: enabled || 'inherit',
        max_instances: instances == null ? null : instances,
        max_concurrent_turns: turns == null ? null : turns,
        srv_id: srvId
      };
    }
    renderCloudAgentAccessTree();
    cagRenderGrants();
    if (typeof showToast === 'function') showToast(okText, 'success');
  }, function (err) {
    var msg = failText.replace('{error}', err && err.message ? err.message : String(err));
    if (typeof showToast === 'function') showToast(msg, 'error');
  });
}

function saveCloudAgentAccessRow(scope, target, enabled, instances, turns, srv, msgs) {
  var next = cagOrg.saveChain.then(function () {
    return cagWriteAccess(scope, target, enabled, instances, turns, srv, msgs);
  });
  cagOrg.saveChain = next.then(function () {}, function () {});
  return next;
}

function saveCloudAgentAccessFromRow(row) {
  if (!row || typeof row.getAttribute !== 'function' || typeof row.querySelector !== 'function') return;
  var enabledEl = row.querySelector('.cag-access-enabled');
  var instEl = row.querySelector('.cag-access-instances');
  var turnEl = row.querySelector('.cag-access-turns');
  var srvEl = row.querySelector('.cag-access-srv');
  return saveCloudAgentAccessRow(
    row.getAttribute('data-scope'),
    row.getAttribute('data-target'),
    enabledEl ? enabledEl.value : 'inherit',
    cagReadLimit(instEl ? instEl.value : ''),
    cagReadLimit(turnEl ? turnEl.value : ''),
    srvEl ? srvEl.value : 'inherit'
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
    var srv = document.getElementById('cloudAgentQuotaSrv');
    if (enabled) enabled.checked = !!(data && data.cloud_agents_enabled);
    if (instances) instances.value = data && data.max_instances != null ? String(data.max_instances) : '3';
    if (turns) turns.value = data && data.max_concurrent_turns != null ? String(data.max_concurrent_turns) : '1';
    if (srv) {
      srv._cagSrv = data && data.srv_id != null ? String(data.srv_id) : '';
      cagRenderQuotaSrv();
    }
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
  if (instances == null || turns == null) {
    if (typeof showToast === 'function') showToast(t.bad, 'error');
    return;
  }
  var enabled = document.getElementById('cloudAgentQuotaEnabled');
  var srv = document.getElementById('cloudAgentQuotaSrv');
  var body = {
    cloud_agents_enabled: !!(enabled && enabled.checked),
    max_instances: instances,
    max_concurrent_turns: turns,
    srv_id: srv ? String(srv.value || '') : ''
  };
  var button = document.getElementById('cloudAgentQuotaSaveBtn');
  if (button) button.disabled = true;
  try {
    await api('/api/admin/cloud-agents/settings', { method: 'PATCH', body: JSON.stringify(body) });
    // Reflect the saved global row locally. A full loadCloudAgentAccess
    // reload refetches access+groups+users and would also drop unsaved
    // limit edits on other rows.
    if (cagOrg.loaded) {
      cagOrg.access[cagKey('global', '')] = {
        scope: 'global', target_id: '',
        enabled: body.cloud_agents_enabled ? 'on' : 'off',
        max_instances: body.max_instances,
        max_concurrent_turns: body.max_concurrent_turns,
        srv_id: body.srv_id
      };
      renderCloudAgentAccessTree();
      cagRenderGrants();
    }
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
    onOpen: function () { loadCloudAgentQuota().then(function () { return loadCloudAgentSrvBindings(); }); }
  });
}
if (typeof window !== 'undefined' && window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(applyCloudAgentQuotaI18n);
}
