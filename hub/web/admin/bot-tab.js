(function (global) {
  'use strict';

  var copy = {
    zh: {
      title: 'Bot \u7ba1\u7406',
      subtitle: '\u5bf9\u63a5 MaClawSrv\uff0c\u5e76\u8bbe\u7f6e Bot \u4f7f\u7528\u7684 Docker \u670d\u52a1\u3002Bot \u7531\u7528\u6237\u5728 MaClaw \u5ba2\u6237\u7aef\u521b\u5efa\u3002',
      nav: 'Bot \u7ba1\u7406',
      navDesc: '\u5bf9\u63a5 MaClawSrv \u4e0e Docker \u670d\u52a1',
      connection: '\u5bf9\u63a5 MaClawSrv',
      url: 'MaClawSrv \u5730\u5740',
      token: '\u8bbf\u95ee\u4ee4\u724c',
      tokenHint: '\u7559\u7a7a\u5219\u4e0d\u4fee\u6539\u5df2\u4fdd\u5b58\u7684\u4ee4\u724c',
      tokenSet: '\u4ee4\u724c\u5df2\u4fdd\u5b58',
      tokenMissing: '\u5c1a\u672a\u4fdd\u5b58\u4ee4\u724c',
      adminSecret: '\u7ba1\u7406\u5bc6\u94a5',
      adminHint: '\u7528\u4e8e\u6309\u7528\u6237\u521b\u5efa MaClawSrv \u8d26\u53f7\u3002\u7559\u7a7a\u5219\u4e0d\u4fee\u6539\u3002',
      adminSet: '\u7ba1\u7406\u5bc6\u94a5\u5df2\u4fdd\u5b58',
      adminMissing: '\u5c1a\u672a\u4fdd\u5b58\u7ba1\u7406\u5bc6\u94a5',
      save: '\u4fdd\u5b58\u5bf9\u63a5\u8bbe\u7f6e',
      test: '\u6d4b\u8bd5\u8fde\u63a5',
      saved: '\u5df2\u4fdd\u5b58',
      connected: '\u8fde\u63a5\u6210\u529f\uff0c\u5f53\u524d\u5b9e\u4f8b {count} \u4e2a',
      testing: '\u6b63\u5728\u6d4b\u8bd5...',
      needConfig: '\u8bf7\u5148\u4fdd\u5b58 MaClawSrv \u5730\u5740\u548c\u8bbf\u95ee\u4ee4\u724c',
      saveBeforeTest: '\u5730\u5740\u6216\u4ee4\u724c\u8fd8\u6ca1\u4fdd\u5b58\uff0c\u8bf7\u5148\u4fdd\u5b58\u518d\u6d4b\u8bd5',
      bots: '\u7528\u6237\u521b\u5efa\u7684 Bot',
      botsHint: '\u5728 MaClaw \u5ba2\u6237\u7aef\u7684 Bot \u7ba1\u7406\u91cc\u521b\u5efa\uff0c\u8fd9\u91cc\u53ea\u67e5\u770b\u3002',
      empty: '\u8fd8\u6ca1\u6709 Bot\u3002\u7528\u6237\u5728 MaClaw \u5ba2\u6237\u7aef\u7684 Bot \u7ba1\u7406\u91cc\u521b\u5efa\u3002',
      needUrl: '\u8bf7\u586b\u5199 MaClawSrv \u5730\u5740',
      badUrl: 'MaClawSrv \u5730\u5740\u9700\u8981\u4ee5 http:// \u6216 https:// \u5f00\u5934',
      owner: '\u7528\u6237',
      access: '\u5f00\u901a\u8303\u56f4',
      accessHint: '\u9ed8\u8ba4\u5173\u95ed\u3002\u53ea\u6709\u8fd9\u91cc\u5f00\u901a\u7684\u7528\u6237\uff0c\u684c\u9762\u5de6\u4e0b\u89d2\u624d\u4f1a\u51fa\u73b0 Bot\u3002',
      global: '\u5168\u5c40\u5f00\u901a',
      department: '\u90e8\u95e8',
      user: '\u7528\u6237',
      orgTitle: '\u7ec4\u7ec7\u673a\u6784',
      orgHint: '\u5c55\u5f00\u90e8\u95e8\u540e\uff0c\u53ef\u4ee5\u5f00\u901a\u8fd9\u4e2a\u90e8\u95e8\uff08\u542b\u4e0b\u7ea7\u90e8\u95e8\uff09\uff0c\u6216\u53ea\u5f00\u901a\u5176\u4e2d\u67d0\u4e2a\u7528\u6237\u3002',
      orgSearch: '\u641c\u7d22\u90e8\u95e8\u6216\u7528\u6237',
      orgLoading: '\u6b63\u5728\u52a0\u8f7d\u7ec4\u7ec7\u673a\u6784...',
      orgEmpty: '\u8fd8\u6ca1\u6709\u7ec4\u7ec7\u673a\u6784\u3002',
      orgFailed: '\u7ec4\u7ec7\u673a\u6784\u52a0\u8f7d\u5931\u8d25',
      orgUsersFailed: '\u7528\u6237\u76ee\u5f55\u52a0\u8f7d\u5931\u8d25\uff0c\u6682\u65f6\u4e0d\u80fd\u6309\u7528\u6237\u5f00\u901a\u3002',
      orgReload: '\u91cd\u65b0\u52a0\u8f7d',
      orgAddDepartment: '\u5f00\u901a\u90e8\u95e8',
      orgAddUser: '\u5f00\u901a\u7528\u6237',
      orgAdded: '\u5df2\u5f00\u901a',
      orgNoAccount: '\u672a\u7ed1\u5b9a\u8d26\u53f7',
      orgMembersLoading: '\u6b63\u5728\u52a0\u8f7d\u6210\u5458...',
      orgMembersFailed: '\u6210\u5458\u52a0\u8f7d\u5931\u8d25\uff0c\u518d\u5c55\u5f00\u4e00\u6b21\u53ef\u4ee5\u91cd\u8bd5\u3002',
      orgNoMembers: '\u6ca1\u6709\u6210\u5458',
      orgSearchEmpty: '\u6ca1\u6709\u5339\u914d\u7684\u90e8\u95e8\u6216\u7528\u6237',
      orgSearchLimited: '\u53ea\u641c\u7d22\u4e86\u524d\u9762\u4e00\u90e8\u5206\u90e8\u95e8\u3002\u6362\u4e2a\u66f4\u5177\u4f53\u7684\u5173\u952e\u8bcd\u3002',
      orgSearching: '\u6b63\u5728\u641c\u7d22...',
      orgMatchUsers: '\u5339\u914d\u7684\u8d26\u53f7',
      orgMatchLimited: '\u53ea\u663e\u793a\u4e86\u524d 50 \u4e2a\u8d26\u53f7\u3002\u518d\u591a\u8f93\u5165\u51e0\u4e2a\u5b57\u3002',
      grantEmpty: '\u5f53\u524d\u6ca1\u6709\u5f00\u901a\u3002',
      instance: '\u5b9e\u4f8b',
      remove: '\u5220\u9664',
      failed: '\u64cd\u4f5c\u5931\u8d25'
    },
    en: {
      title: 'Bot management',
      subtitle: 'Connect MaClawSrv and set the Docker service bots use. Users create bots in the MaClaw app.',
      nav: 'Bot management',
      navDesc: 'MaClawSrv connection and Docker service',
      connection: 'MaClawSrv connection',
      url: 'MaClawSrv URL',
      token: 'Access token',
      tokenHint: 'Leave blank to keep the saved token',
      tokenSet: 'Token saved',
      tokenMissing: 'No token saved',
      adminSecret: 'Admin secret',
      adminHint: 'Creates one MaClawSrv user per Hub user. Leave blank to keep the saved secret.',
      adminSet: 'Admin secret saved',
      adminMissing: 'No admin secret saved',
      save: 'Save connection',
      test: 'Test connection',
      saved: 'Saved',
      connected: 'Connected, {count} instances',
      testing: 'Testing...',
      needConfig: 'Save the MaClawSrv URL and access token first',
      saveBeforeTest: 'Save the URL and token before testing',
      bots: 'Bots created by users',
      botsHint: 'Created in the MaClaw app. This page only lists them.',
      empty: 'No bots yet. Users create them in the MaClaw app.',
      needUrl: 'Enter the MaClawSrv URL',
      badUrl: 'The MaClawSrv URL must start with http:// or https://',
      owner: 'User',
      access: 'Who can use bots',
      accessHint: 'Off by default. The desktop Bot entry appears only for users switched on here.',
      global: 'Everyone',
      department: 'Department',
      user: 'User',
      orgTitle: 'Organization',
      orgHint: 'Open a department to enable it, including child departments, or enable one person.',
      orgSearch: 'Search departments or users',
      orgLoading: 'Loading organization...',
      orgEmpty: 'No organization yet.',
      orgFailed: 'Could not load the organization',
      orgUsersFailed: 'The user directory failed to load, so people cannot be enabled yet.',
      orgReload: 'Reload',
      orgAddDepartment: 'Enable department',
      orgAddUser: 'Enable user',
      orgAdded: 'Enabled',
      orgNoAccount: 'No account',
      orgMembersLoading: 'Loading people...',
      orgMembersFailed: 'Could not load people. Collapse and open this department to retry.',
      orgNoMembers: 'No people',
      orgSearchEmpty: 'No matching department or user',
      orgSearchLimited: 'Only part of the organization was searched. Try a more specific word.',
      orgSearching: 'Searching...',
      orgMatchUsers: 'Matching accounts',
      orgMatchLimited: 'Showing the first 50 accounts. Type a few more letters.',
      grantEmpty: 'Nobody is enabled.',
      instance: 'Instance',
      remove: 'Delete',
      failed: 'Request failed'
    }
  };

  function text() {
    return copy[global.currentLang === 'en' ? 'en' : 'zh'];
  }

  function byID(id) {
    return document.getElementById(id);
  }

  function api(path, opts) {
    if (typeof global.api === 'function') return global.api(path, opts);
    return Promise.reject(new Error('api unavailable'));
  }

  function showToast(msg, kind) {
    if (typeof global.showToast === 'function') global.showToast(msg, kind);
  }

  function escapeHtml(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function messageOf(err) {
    var raw = err && err.message ? err.message : '';
    if (raw.indexOf('save the MaClawSrv URL') !== -1) return text().needConfig;
    if (raw.indexOf('base_url') !== -1) return text().badUrl;
    return raw || text().failed;
  }

  function applyBotI18n() {
    var t = text();
    var nav = byID('navBots');
    var desc = byID('navBotsDesc');
    if (nav) nav.textContent = t.nav;
    if (desc) desc.textContent = t.navDesc;
    var panel = byID('tab-bots');
    if (!panel || panel.dataset.ready !== '1') return;
    setLabel('botConnectionTitle', t.connection);
    setLabel('botUrlLabel', t.url);
    setLabel('botTokenLabel', t.token);
    setLabel('botTokenHint', t.tokenHint);
    setLabel('botAdminLabel', t.adminSecret);
    setLabel('botAdminHint', t.adminHint);
    setLabel('botSave', t.save);
    setLabel('botTest', t.test);
    setLabel('botAccessTitle', t.access);
    setLabel('botAccessHint', t.accessHint);
    setLabel('botGlobalLabel', t.global);
    setLabel('botOrgTitle', t.orgTitle);
    setLabel('botOrgHint', t.orgHint);
    var orgFilter = byID('botOrgFilter');
    if (orgFilter) {
      orgFilter.placeholder = t.orgSearch;
      if (orgFilter.setAttribute) orgFilter.setAttribute('aria-label', t.orgSearch);
    }
    setLabel('botListTitle', t.bots);
    setLabel('botListHint', t.botsHint);
    if (panel._botView) render(panel._botView);
  }

  function setLabel(id, value) {
    var el = byID(id);
    if (el) el.textContent = value;
  }

  function mount() {
    var panel = byID('tab-bots');
    if (!panel || panel.dataset.ready === '1') return;
    panel.dataset.ready = '1';
    panel.innerHTML = ''
      + '<style>'
      + '#tab-bots [hidden]{display:none !important}'
      + '#tab-bots .bot-grid{display:grid;gap:22px}'
      + '#tab-bots input:not([type=checkbox]),#tab-bots select{display:block;width:min(560px,100%);margin:6px 0 12px}'
      + '#tab-bots input[type=checkbox]{width:15px;height:15px;display:inline-block;margin:0 8px 0 0;vertical-align:middle}'
      + '#tab-bots label:has(input[type=checkbox]){display:flex;align-items:center;text-transform:none;letter-spacing:0;font-size:14px;font-weight:650;color:#303541}'
      + '#tab-bots .bot-actions{display:flex;gap:8px;align-items:center}'
      + '#tab-bots .bot-actions button{height:34px;min-height:34px}'
      + '#tab-bots .bot-row{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:10px 0;border-top:1px solid #e8ebf1}'
      + '#tab-bots .bot-row > div{min-width:0}'
      + '#tab-bots .bot-row small{display:block;color:#707684}'
      + '#tab-bots #botDesktopHost{display:grid;gap:22px;padding-top:18px;border-top:1px solid #e8ebf1}'
      + '#tab-bots .bot-fields{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;max-width:560px;margin:0 0 12px}'
      + '#tab-bots .bot-fields input{width:100%;margin:6px 0 0}'
      + '@media (max-width:760px){#tab-bots .bot-fields{grid-template-columns:1fr 1fr}}'
      + '#tab-bots .bot-org-head{margin:16px 0 4px;font-size:14px;font-weight:800}'
      + '#tab-bots #botOrgFilter{margin-bottom:8px}'
      + '#tab-bots .bot-org-tree{max-height:360px;overflow:auto;border:1px solid #e8ebf1;border-radius:12px;padding:6px;margin:0 0 12px;background:#fbfcfe}'
      + '#tab-bots .bot-org-row{display:flex;align-items:center;gap:8px;min-height:34px;padding:4px 8px;border-radius:8px;cursor:pointer}'
      + '#tab-bots .bot-org-row:hover{background:#f6f8fb}'
      + '#tab-bots .bot-org-toggle{width:22px;min-width:22px;height:22px;min-height:22px;padding:0;border:0;background:transparent;box-shadow:none;color:#707684;font-size:10px}'
      + '#tab-bots .bot-org-toggle:hover{transform:none;background:#eef2f7}'
      + '#tab-bots .bot-org-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px}'
      + '#tab-bots .bot-org-meta,#tab-bots .bot-org-status{color:#707684;font-size:12px}'
      + '#tab-bots .bot-org-add{height:28px;min-height:28px;padding:0 10px;font-size:12px;box-shadow:none}'
      + '#tab-bots .bot-org-add:disabled,#tab-bots button:disabled,#tab-bots button:disabled:hover{opacity:.55;cursor:default;transform:none}'
      + '</style>'
      + '<div class="bot-grid">'
      + '<section><h3 id="botConnectionTitle"></h3>'
      + '<label id="botUrlLabel" for="botBaseUrl"></label>'
      + '<input id="botBaseUrl" autocomplete="off" spellcheck="false">'
      + '<label id="botTokenLabel" for="botAccessToken"></label>'
      + '<input id="botAccessToken" type="password" autocomplete="new-password">'
      + '<p id="botTokenHint"></p><p id="botTokenState"></p>'
      + '<label id="botAdminLabel" for="botAdminSecret"></label>'
      + '<input id="botAdminSecret" type="password" autocomplete="new-password">'
      + '<p id="botAdminHint"></p><p id="botAdminState"></p>'
      + '<div class="bot-actions"><button type="button" class="btn-primary" id="botSave"></button><button type="button" class="btn-secondary" id="botTest"></button></div>'
      + '<p id="botConnStatus"></p></section>'
      + '<section><h3 id="botAccessTitle"></h3><p id="botAccessHint"></p>'
      + '<label><input id="botGlobal" type="checkbox"> <span id="botGlobalLabel"></span></label>'
      + '<div class="bot-org-head" id="botOrgTitle"></div><p id="botOrgHint"></p>'
      + '<input id="botOrgFilter" type="search" autocomplete="off">'
      + '<div id="botOrgTree" class="bot-org-tree" role="tree"></div>'
      + '<div id="botGrants"></div></section>'
      + '<div id="botDesktopHost"></div>'
      + '<section><h3 id="botListTitle"></h3><p id="botListHint"></p><div id="botList"></div></section></div>';
    byID('botSave').addEventListener('click', saveConnection);
    byID('botTest').addEventListener('click', testConnection);
    byID('botGlobal').addEventListener('change', toggleGlobal);
    byID('botOrgFilter').addEventListener('compositionstart', function () { orgState().composing = true; });
    byID('botOrgFilter').addEventListener('compositionend', function () {
      orgState().composing = false;
      onOrgFilter();
    });
    byID('botOrgFilter').addEventListener('input', function (event) {
      if (orgState().composing || (event && event.isComposing)) return;
      onOrgFilter();
    });
    byID('botOrgTree').addEventListener('click', onOrgClick);
    if (typeof global.mountBotDesktop === 'function') global.mountBotDesktop();
    applyBotI18n();
  }

  function load() {
    mount();
    if (typeof global.loadBotDesktop === 'function') global.loadBotDesktop();
    var directory = orgState().loaded ? Promise.resolve() : loadDirectory('');
    var settings = api('/api/admin/bots/settings').then(function (view) {
      render(view || {});
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    });
    return Promise.all([settings, directory]);
  }

  function openBotTab() {
    var refresh = orgState().loaded;
    var pending = load();
    if (refresh) return Promise.all([pending, loadDirectory('refresh')]);
    return pending;
  }

  function showServerUrl(url, next) {
    if (!url) return;
    var incoming = next || '';
    var dirty = url._serverValue != null ? url.value !== url._serverValue : url.value !== '' && url.value !== incoming;
    if (dirty) {
      if (url._serverValue == null) url._serverValue = incoming;
      return;
    }
    url.value = incoming;
    url._serverValue = url.value;
  }

  function render(view) {
    var panel = byID('tab-bots');
    if (panel) panel._botView = view;
    showServerUrl(byID('botBaseUrl'), view.base_url || '');
    var state = byID('botTokenState');
    if (state) state.textContent = view.token_set ? text().tokenSet : text().tokenMissing;
    var adminState = byID('botAdminState');
    if (adminState) adminState.textContent = view.admin_secret_set ? text().adminSet : text().adminMissing;
    renderBots(view);
    renderGrants(view);
    renderOrgTree();
  }

  function renderGrants(view) {
    var list = byID('botGrants');
    var grants = view && Array.isArray(view.grants) ? view.grants : [];
    var globalBox = byID('botGlobal');
    if (globalBox) {
      globalBox.checked = grants.some(function (item) { return item.scope === 'global'; });
    }
    if (!list) return;
    var specific = grants.filter(function (item) { return item.scope !== 'global'; });
    if (!specific.length) {
      list.innerHTML = grants.length ? '' : '<p>' + escapeHtml(text().grantEmpty) + '</p>';
      return;
    }
    list.innerHTML = specific.map(function (item) {
      var label = grantLabel(item);
      return '<div class="bot-row"><span>' + escapeHtml(label) + '</span>'
        + '<button type="button" class="btn-danger" data-grant-delete="' + escapeHtml(item.id) + '"' + (grantLocked() ? ' disabled' : '') + '>'
        + escapeHtml(text().remove) + '</button></div>';
    }).join('');
    list.querySelectorAll('[data-grant-delete]').forEach(function (button) {
      button.addEventListener('click', function () { deleteGrant(button.getAttribute('data-grant-delete')); });
    });
  }

  function grantLabel(item) {
    if (!item) return '';
    if (item.scope === 'global') return text().global;
    if (item.scope === 'department') return text().department + ' ' + (deptPath(item.target_id) || item.target_id || '');
    if (item.scope === 'user') return text().user + ' ' + (userLabel(item.target_id) || item.target_id || '');
    return (item.scope || '') + ' ' + (item.target_id || '');
  }

  function setGrantBusy(busy) {
    var panel = byID('tab-bots');
    if (panel) panel._botGrantBusy = !!busy;
    var globalBox = byID('botGlobal');
    if (globalBox) globalBox.disabled = !!busy;
    renderOrgTree();
    var panelView = panel && panel._botView;
    if (panelView) renderGrants(panelView);
  }

  function grantLocked() {
    var panel = byID('tab-bots');
    return !!(panel && panel._botGrantBusy);
  }

  function toggleGlobal() {
    var box = byID('botGlobal');
    if (!box || grantLocked()) return;
    var panel = byID('tab-bots');
    var view = panel && panel._botView;
    var grants = view && Array.isArray(view.grants) ? view.grants : [];
    var current = null;
    grants.forEach(function (item) { if (item.scope === 'global') current = item; });
    if (box.checked && !current) {
      setGrantBusy(true);
      return api('/api/admin/bots/grants', { method: 'POST', body: JSON.stringify({ scope: 'global' }) }).then(load).catch(function (err) {
        box.checked = false;
        showToast(messageOf(err), 'error');
      }).then(function () { setGrantBusy(false); });
    }
    if (!box.checked && current) {
      setGrantBusy(true);
      return api('/api/admin/bots/grants/' + encodeURIComponent(current.id), { method: 'DELETE' }).then(load).catch(function (err) {
        box.checked = true;
        showToast(messageOf(err), 'error');
      }).then(function () { setGrantBusy(false); });
    }
  }

  function orgState() {
    var panel = byID('tab-bots');
    if (!panel._org) {
      panel._org = {
        seq: 0,
        tree: null,
        usersByEmail: {},
        usersById: {},
        members: {},
        memberLoads: {},
        loadingMembers: {},
        expanded: {},
        filter: '',
        error: '',
        loading: false,
        loaded: false,
        searching: false,
        searchLimited: false,
        searchToken: 0,
        searchTimer: 0,
        renderQueued: false,
        promise: null,
        memberFailed: {},
        collapsed: {}
      };
    }
    return panel._org;
  }

  function normalizeOrgTree(nodes, ancestry, depth) {
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
        children: normalizeOrgTree(node.children, next, current + 1)
      });
      return out;
    }, []);
  }

  function indexOrgUsers(users) {
    var org = orgState();
      org.usersByEmail = {};
      org.usersById = {};
      org.hitQuery = '';
      org.hitCache = null;
    (users || []).forEach(function (user) {
      if (!user) return;
      var id = String(user.id || '').trim();
      if (!id) return;
      org.usersById[id] = user;
      [user.email].concat(user.emails || [], user.phone ? [user.phone] : [], user.phones || []).forEach(function (email) {
        var key = String(email || '').trim().toLowerCase();
        if (key && !org.usersByEmail[key]) org.usersByEmail[key] = user;
      });
    });
  }

  function userLabel(userId) {
    var user = orgState().usersById[String(userId || '')];
    if (!user) return '';
    return String(user.email || user.phone || user.id || '').trim();
  }

  function findDept(nodes, id, prefix) {
    var want = String(id || '');
    for (var i = 0; i < (nodes || []).length; i += 1) {
      var name = nodes[i].name || nodes[i].id;
      var path = prefix ? prefix + ' / ' + name : name;
      if (nodes[i].id === want) return path;
      var child = findDept(nodes[i].children, want, path);
      if (child) return child;
    }
    return '';
  }

  function deptPath(id) {
    return findDept(orgState().tree || [], id, '');
  }

  function flattenDepts(nodes, prefix, out) {
    (nodes || []).forEach(function (node) {
      var name = node.name || node.id;
      var path = prefix ? prefix + ' / ' + name : name;
      out.push({ id: node.id, label: path });
      flattenDepts(node.children, path, out);
    });
  }

  global.botOrgChoices = function () {
    var org = orgState();
    var departments = [];
    flattenDepts(org.tree || [], '', departments);
    var users = Object.keys(org.usersById || {}).map(function (id) {
      var user = org.usersById[id];
      return { id: id, label: String((user && (user.email || user.phone)) || id) };
    });
    users.sort(function (a, b) { return a.label.localeCompare(b.label); });
    return { ready: !!org.loaded, departments: departments, users: users };
  };

  function grantFor(scope, targetId) {
    var panel = byID('tab-bots');
    var grants = panel && panel._botView && Array.isArray(panel._botView.grants) ? panel._botView.grants : [];
    var want = String(targetId || '');
    for (var i = 0; i < grants.length; i += 1) {
      if (grants[i].scope === scope && String(grants[i].target_id || '') === want) return grants[i];
    }
    return null;
  }

  function loadDirectory(mode) {
    var org = orgState();
    var refresh = mode === 'refresh';
    var reload = mode === 'reload';
    if (!refresh && !reload && org.promise) return org.promise;
    org.seq += 1;
    var seq = org.seq;
    org.loading = true;
    if (reload || refresh) {
      org.members = {};
      org.memberLoads = {};
      org.loadingMembers = {};
      org.memberFailed = {};
    }
    if (reload) {
      org.loaded = false;
      org.tree = null;
      org.error = '';
    }
    if (!org.loaded) renderOrgTree();
    var usersFailed = false;
    org.promise = Promise.all([
      api('/api/admin/security/groups'),
      api('/api/admin/users').then(function (data) { return data || { users: [] }; }, function () {
        usersFailed = true;
        return { users: [] };
      })
    ]).then(function (results) {
      if (org.seq !== seq) return;
      var treeData = results[0] || {};
      var userData = results[1] || {};
      var tree = treeData.tree;
      org.tree = normalizeOrgTree(Array.isArray(tree) ? tree : (tree ? [tree] : []));
      indexOrgUsers(userData.users || []);
      org.loaded = true;
      org.error = '';
      if (usersFailed) showToast(text().orgUsersFailed, 'error');
      var open = Object.keys(org.expanded).filter(function (id) {
        return org.expanded[id] && findDept(org.tree, id, '');
      });
      if (!open.length && org.tree[0]) {
        org.expanded[org.tree[0].id] = true;
        open = [org.tree[0].id];
      }
      return Promise.all(open.slice(0, 30).map(function (id) { return loadMembers(id); }));
    }).catch(function (err) {
      if (org.seq !== seq) return;
      if (org.tree && org.tree.length) showToast(messageOf(err), 'error');
      else {
        org.tree = [];
        org.loaded = true;
        org.error = messageOf(err);
      }
    }).then(function () {
      if (org.seq !== seq) return;
      org.loading = false;
      renderOrgTree();
      notifyDirectory();
      var panel = byID('tab-bots');
      if (panel && panel._botView) {
        renderGrants(panel._botView);
        renderBots(panel._botView);
      }
      var query = String(org.filter || '').trim().toLowerCase();
      if (query) return searchMembers(query);
    });
    return org.promise;
  }

  function resolveMember(email) {
    var raw = String(email || '').trim();
    var key = raw.toLowerCase();
    if (!key) return null;
    var user = orgState().usersByEmail[key];
    return {
      email: raw,
      userId: user ? String(user.id || '').trim() : '',
      label: user ? (userLabel(user.id) || raw) : raw
    };
  }

  function loadMembers(groupId) {
    var org = orgState();
    var id = String(groupId || '').trim();
    if (!id) return Promise.resolve();
    if (org.members[id]) return Promise.resolve(org.members[id]);
    if (org.memberLoads[id]) return org.memberLoads[id];
    var seq = org.seq;
    org.loadingMembers[id] = true;
    delete org.memberFailed[id];
    var pending = api('/api/admin/security/groups/' + encodeURIComponent(id) + '/members').then(function (data) {
      if (org.seq !== seq) return;
      var seen = {};
      org.members[id] = (data && data.members || []).map(resolveMember).filter(function (member) {
        if (!member) return false;
        var key = member.email.toLowerCase();
        if (seen[key]) return false;
        seen[key] = true;
        return true;
      });
    }).catch(function () {
      if (org.seq !== seq) return;
      org.memberFailed[id] = true;
    }).then(function () {
      if (org.seq !== seq) return;
      delete org.loadingMembers[id];
      delete org.memberLoads[id];
      scheduleOrgRender();
    });
    org.memberLoads[id] = pending;
    return pending;
  }

  function namePrefix(node, query) {
    return String(node && (node.name || node.id) || '').toLowerCase().indexOf(query) === 0;
  }

  function shouldScanMembers(query) {
    if (!query) return false;
    if (query.length >= 2) return true;
    return /[\u3400-\u9fff]/.test(query);
  }

  function scanPlan(query) {
    if (!shouldScanMembers(query)) return [];
    var prefixHit = false;
    function mark(nodes) {
      (nodes || []).forEach(function (node) {
        if (!node || prefixHit) return;
        if (namePrefix(node, query)) prefixHit = true;
        else mark(node.children);
      });
    }
    mark(orgState().tree);
    var first = [];
    var rest = [];
    function walk(nodes, underPrefix, underName, depth) {
      (nodes || []).forEach(function (node) {
        if (!node || !node.id) return;
        var label = String(node.name || node.id || '').toLowerCase();
        var named = label.indexOf(query) === 0;
        var contains = label.indexOf(query) >= 0;
        if (prefixHit && (underPrefix || named)) (contains || underName ? first : rest).push(node.id);
        walk(node.children, underPrefix || (named && depth > 0), underName || contains, depth + 1);
      });
    }
    walk(orgState().tree, false, false, 0);
    return first.concat(rest);
  }

  function idsToScan(query) {
    var org = orgState();
    var windowIds = org.scanWindow;
    return scanPlan(query).filter(function (id) {
      if (org.members[id]) return false;
      return !windowIds || !!windowIds[id];
    });
  }

  function rememberScan(query) {
    var org = orgState();
    var plan = scanPlan(query);
    var windowIds = {};
    plan.slice(0, 80).forEach(function (id) { windowIds[id] = true; });
    org.scanWindow = windowIds;
    org.searchLimited = plan.length > 80;
  }

  function onOrgFilter() {
    var org = orgState();
    var input = byID('botOrgFilter');
    var next = input && input.value || '';
    var query = String(next).trim().toLowerCase();
    var previous = String(org.filter || '').trim().toLowerCase();
    org.filter = next;
    org.searchToken += 1;
    if (!query) {
      if (previous && org.expandedBeforeSearch) org.expanded = org.expandedBeforeSearch;
      org.expandedBeforeSearch = null;
      org.collapsed = {};
      org.scanWindow = null;
      org.searchLimited = false;
    } else if (!previous) {
      org.expandedBeforeSearch = Object.assign({}, org.expanded);
      org.collapsed = {};
    }
    if (query) rememberScan(query);
    org.searching = !!(query && idsToScan(query).length);
    renderOrgTree();
    if (!query) {
      if (org.searchTimer) clearTimeout(org.searchTimer);
      org.searchTimer = 0;
      return;
    }
    scheduleMemberSearch();
  }

  function scheduleMemberSearch() {
    var org = orgState();
    if (org.searchTimer) clearTimeout(org.searchTimer);
    org.searchTimer = setTimeout(function () {
      org.searchTimer = 0;
      searchMembers(String(org.filter || '').trim().toLowerCase());
    }, 200);
  }

  function searchMembers(query) {
    var org = orgState();
    org.searchToken += 1;
    var token = org.searchToken;
    if (!query) {
      org.searching = false;
      renderOrgTree();
      return Promise.resolve();
    }
    if (!org.scanWindow) rememberScan(query);
    org.searchLimited = scanPlan(query).length > 80;
    var queue = idsToScan(query);
    if (!queue.length) {
      org.searching = false;
      renderOrgTree();
      return Promise.resolve();
    }
    org.searching = true;
    var cursor = 0;
    var limit = 80;
    function pump() {
      if (token !== org.searchToken) return Promise.resolve();
      if (cursor >= queue.length) return Promise.resolve();
      if (cursor >= limit) {
        org.searchLimited = true;
        return Promise.resolve();
      }
      var id = queue[cursor];
      cursor += 1;
      return loadMembers(id).then(pump);
    }
    var jobs = [];
    for (var n = 0; n < 4; n += 1) jobs.push(pump());
    return Promise.all(jobs).then(function () {
      if (token !== org.searchToken) return;
      org.searching = false;
      renderOrgTree();
    });
  }

  function scheduleOrgRender() {
    var org = orgState();
    if (org.renderQueued) return;
    org.renderQueued = true;
    Promise.resolve().then(function () {
      org.renderQueued = false;
      renderOrgTree();
    });
  }

  function accountMatches(user, member, query) {
    if (!query) return true;
    var emails = [];
    var phones = [];
    var id = '';
    if (user) {
      if (user.email) emails.push(user.email);
      if (user.emails) emails = emails.concat(user.emails);
      if (user.phone) phones.push(user.phone);
      if (user.phones) phones = phones.concat(user.phones);
      id = String(user.id || '');
    }
    if (member) {
      if (member.email) emails.push(member.email);
      if (!id && member.userId) id = String(member.userId);
    }
    if (id && id.toLowerCase() === query) return true;
    var phoneQuery = query.replace(/[\s+\-().]/g, '');
    var queryDigits = /^\d{4,}$/.test(phoneQuery) ? phoneQuery : '';
    var i;
    for (i = 0; i < phones.length; i += 1) {
      var phone = String(phones[i] || '').trim().toLowerCase();
      if (!phone || !queryDigits) continue;
      if (phone.replace(/\D/g, '').indexOf(queryDigits) >= 0) return true;
    }
    for (i = 0; i < emails.length; i += 1) {
      var email = String(emails[i] || '').trim().toLowerCase();
      if (!email) continue;
      if (email.indexOf(query) === 0) return true;
      var at = email.indexOf('@');
      var local = at >= 0 ? email.slice(0, at) : email;
      if (local && local.indexOf(query) === 0) return true;
    }
    return false;
  }

  function memberMatches(member, query) {
    if (!query) return true;
    var user = member && member.userId ? orgState().usersById[member.userId] : null;
    return accountMatches(user, member, query);
  }

  function matchedContact(user, query) {
    var emails = [user && user.email].concat(user && user.emails || []);
    var phones = [user && user.phone].concat(user && user.phones || []);
    var i;
    for (i = 0; i < emails.length; i += 1) {
      var email = String(emails[i] || '').trim();
      if (email && accountMatches({ email: email }, null, query)) return email;
    }
    for (i = 0; i < phones.length; i += 1) {
      var phone = String(phones[i] || '').trim();
      if (phone && accountMatches({ phone: phone }, null, query)) return phone;
    }
    return '';
  }

  function directoryHits(query) {
    var org = orgState();
    if (!shouldScanMembers(query)) return [];
    if (org.hitQuery === query && org.hitCache) return org.hitCache;
    var hits = Object.keys(org.usersById).filter(function (id) {
      return accountMatches(org.usersById[id], null, query);
    }).map(function (id) {
      var user = org.usersById[id];
      var primary = userLabel(id) || id;
      var matched = matchedContact(user, query);
      var label = matched && matched.toLowerCase() !== primary.toLowerCase() ? primary + ' / ' + matched : primary;
      return {
        email: String((user && (user.email || user.phone)) || ''),
        userId: id,
        label: label
      };
    }).sort(function (a, b) { return a.label.localeCompare(b.label); });
    org.hitQuery = query;
    org.hitCache = hits;
    return hits;
  }

  function renderDirectoryHits(query, treeHtml) {
    var hits = directoryHits(query).filter(function (member) {
      return treeHtml.indexOf('data-org-add-user="' + member.userId + '"') === -1;
    });
    if (!hits.length) return '';
    var more = hits.length > 50;
    return '<div class="bot-org-status">' + escapeHtml(text().orgMatchUsers) + '</div>'
      + hits.slice(0, 50).map(function (member) { return renderUserRow(member, 0); }).join('')
      + (more ? '<div class="bot-org-status">' + escapeHtml(text().orgMatchLimited) + '</div>' : '');
  }

  function nodeMatches(node, query) {
    if (!query) return true;
    if (String(node.name || node.id || '').toLowerCase().indexOf(query) >= 0) return true;
    var members = orgState().members[node.id] || [];
    if (members.some(function (member) { return memberMatches(member, query); })) return true;
    return (node.children || []).some(function (child) { return nodeMatches(child, query); });
  }

  function orgAddButton(attr, id, label, granted, title) {
    var caption = granted ? text().orgAdded : label;
    var aria = title ? caption + ' ' + title : caption;
    return '<button type="button" class="btn-secondary bot-org-add" ' + attr + '="' + escapeHtml(id) + '" aria-label="' + escapeHtml(aria) + '"'
      + ((granted || grantLocked()) ? ' disabled' : '') + '>' + escapeHtml(caption) + '</button>';
  }

  function renderUserRow(member, pad) {
    var granted = !!(member.userId && grantFor('user', member.userId));
    var action = member.userId
      ? orgAddButton('data-org-add-user', member.userId, text().orgAddUser, granted, member.label)
      : '<span class="bot-org-meta">' + escapeHtml(text().orgNoAccount) + '</span>';
    return '<div class="bot-org-row bot-org-user" role="treeitem" style="padding-left:' + String(30 + pad) + 'px">'
      + '<span class="bot-org-name">' + escapeHtml(member.label) + '</span>' + action + '</div>';
  }

  function revealForFilter() {
    var org = orgState();
    var query = String(org.filter || '').trim().toLowerCase();
    if (!query) return;
    var scan = shouldScanMembers(query);
    var queued = org.scanWindow || {};
    function walk(nodes, underName, depth) {
      (nodes || []).forEach(function (node) {
        if (!node || !node.id) return;
        var named = namePrefix(node, query);
        var show = named || underName || (scan && nodeMatches(node, query));
        var known = !!org.members[node.id] || !!queued[node.id] || !!org.loadingMembers[node.id];
        if (!org.collapsed[node.id] && show && (known || named)) org.expanded[node.id] = true;
        walk(node.children, underName || (named && depth > 0), depth + 1);
      });
    }
    walk(org.tree, false, 0);
  }

  function renderOrgNodes(nodes, depth, query, ancestorNamed) {
    return (nodes || []).map(function (node) {
      return renderOrgNode(node, depth, query, ancestorNamed);
    }).join('');
  }

  function renderOrgNode(node, depth, query, ancestorNamed) {
    if (!node) return '';
    var selfNamed = !query || String(node.name || node.id || '').toLowerCase().indexOf(query) >= 0;
    var named = !!ancestorNamed || selfNamed;
    if (query && !named && !nodeMatches(node, query)) return '';
    var org = orgState();
    var expanded = !!org.expanded[node.id];
    var pad = depth * 16;
    var nameMatches = named;
    var html = '<div class="bot-org-row" role="treeitem" data-org-toggle="' + escapeHtml(node.id) + '" aria-expanded="' + (expanded ? 'true' : 'false') + '" style="padding-left:' + String(8 + pad) + 'px">'
      + '<button type="button" class="bot-org-toggle" data-org-toggle="' + escapeHtml(node.id) + '" aria-label="' + escapeHtml(node.name || node.id) + '">'
      + (expanded ? '\u25bc' : '\u25b6') + '</button>'
      + '<span class="bot-org-name">' + escapeHtml(node.name || node.id) + '</span>'
      + orgAddButton('data-org-add-dept', node.id, text().orgAddDepartment, !!grantFor('department', node.id), node.name || node.id)
      + '</div>';
    if (!expanded) return html;
    if (org.loadingMembers[node.id] && !org.members[node.id]) {
      html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgMembersLoading) + '</div>';
    } else if (org.memberFailed[node.id] && !org.members[node.id]) {
      html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgMembersFailed) + '</div>';
    } else if (org.searching && !org.members[node.id]) {
      html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgMembersLoading) + '</div>';
    } else {
      var members = (org.members[node.id] || []).filter(function (member) {
        return nameMatches || memberMatches(member, query);
      });
      html += members.map(function (member) { return renderUserRow(member, pad + 16); }).join('');
      if (org.members[node.id] && !members.length && !(node.children && node.children.length) && !query) {
        html += '<div class="bot-org-status" style="padding-left:' + String(36 + pad) + 'px">' + escapeHtml(text().orgNoMembers) + '</div>';
      }
    }
    html += renderOrgNodes(node.children || [], depth + 1, nameMatches ? '' : query, nameMatches);
    return html;
  }

  function notifyDirectory() {
    if (typeof global.refreshBotDesktopTargets === 'function') global.refreshBotDesktopTargets();
  }

  function drawOrg(host, html) {
    if (!host || host._drawn === html) return;
    var top = host.scrollTop || 0;
    host.innerHTML = html;
    host._drawn = html;
    if (top) host.scrollTop = top;
  }

  function renderOrgTree() {
    var host = byID('botOrgTree');
    if (!host) return;
    var org = orgState();
    var t = text();
    if (!org.loaded && org.loading) {
      drawOrg(host, '<p class="bot-org-status">' + escapeHtml(t.orgLoading) + '</p>');
      return;
    }
    if (org.error && !(org.tree && org.tree.length)) {
      drawOrg(host, '<p class="bot-org-status">' + escapeHtml(org.error || t.orgFailed) + '</p>'
        + '<button type="button" class="btn-secondary bot-org-add" id="botOrgReload">' + escapeHtml(t.orgReload) + '</button>');
      return;
    }
    if (!org.tree || !org.tree.length) {
      drawOrg(host, '<p class="bot-org-status">' + escapeHtml(t.orgEmpty) + '</p>'
        + '<button type="button" class="btn-secondary bot-org-add" id="botOrgReload">' + escapeHtml(t.orgReload) + '</button>');
      return;
    }
    revealForFilter();
    var query = String(org.filter || '').trim().toLowerCase();
    var html = renderOrgNodes(org.tree, 0, query, false);
    html = renderDirectoryHits(query, html) + html;
    if (!html) {
      var empty = org.searching ? t.orgSearching : (org.searchLimited ? t.orgSearchLimited : t.orgSearchEmpty);
      html = '<p class="bot-org-status">' + escapeHtml(empty) + '</p>';
    } else if (org.searchLimited) {
      html += '<p class="bot-org-status">' + escapeHtml(t.orgSearchLimited) + '</p>';
    }
    drawOrg(host, html);
  }

  function onOrgClick(event) {
    var node = event.target;
    var tree = byID('botOrgTree');
    var action = null;
    var kind = '';
    while (node && node !== tree) {
      if (node.getAttribute) {
        if (node.getAttribute('data-org-add-dept')) { action = node; kind = 'dept'; break; }
        if (node.getAttribute('data-org-add-user')) { action = node; kind = 'user'; break; }
        if (node.getAttribute('data-org-toggle')) { action = node; kind = 'toggle'; break; }
      }
      if (node.id === 'botOrgReload') { action = node; kind = 'reload'; break; }
      node = node.parentElement;
    }
    if (!action) return;
    if (kind === 'toggle') toggleOrg(action.getAttribute('data-org-toggle'));
    else if (kind === 'dept') addOrgGrant('department', action.getAttribute('data-org-add-dept'));
    else if (kind === 'user') addOrgGrant('user', action.getAttribute('data-org-add-user'));
    else if (kind === 'reload') loadDirectory('reload');
  }

  function toggleOrg(id) {
    var org = orgState();
    var query = String(org.filter || '').trim();
    if (org.expanded[id]) {
      delete org.expanded[id];
      if (query) org.collapsed[id] = true;
    } else {
      org.expanded[id] = true;
      if (query) delete org.collapsed[id];
      loadMembers(id);
    }
    renderOrgTree();
  }

  function addOrgGrant(scope, targetId) {
    if (grantLocked()) return;
    var target = String(targetId || '').trim();
    if (!target || grantFor(scope, target)) return;
    setGrantBusy(true);
    return api('/api/admin/bots/grants', { method: 'POST', body: JSON.stringify({ scope: scope, target_id: target }) }).then(function () {
      return load();
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () { setGrantBusy(false); });
  }

  function deleteGrant(id) {
    if (grantLocked()) return;
    setGrantBusy(true);
    return api('/api/admin/bots/grants/' + encodeURIComponent(id), { method: 'DELETE' }).then(load).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () { setGrantBusy(false); });
  }

  function renderBots(view) {
    var list = byID('botList');
    if (!list) return;
    var bots = view && Array.isArray(view.bots) ? view.bots : [];
    if (!bots.length) {
      list.innerHTML = '<p>' + escapeHtml(text().empty) + '</p>';
      return;
    }
    list.innerHTML = bots.map(function (bot) {
      return '<div class="bot-row"><div><strong>' + escapeHtml(bot.name) + '</strong>'
        + '<div>' + escapeHtml(bot.description || '') + '</div>'
        + '<small>' + escapeHtml(text().owner) + ' ' + escapeHtml(userLabel(bot.owner_user_id) || bot.owner_user_id || '')
        + ' / ' + escapeHtml(text().instance) + ' ' + escapeHtml(bot.instance_id || '') + '</small></div></div>';
    }).join('');
  }

  function validServiceUrl(value) {
    var raw = String(value || '').trim();
    var parsed;
    try { parsed = new URL(raw); } catch (err) { return false; }
    return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && !!parsed.host && !parsed.username && !parsed.password;
  }

  function saveConnection() {
    var panel = byID('tab-bots');
    if (panel && panel._connSaving) return;
    var base = (byID('botBaseUrl').value || '').trim();
    if (!base) {
      showToast(text().needUrl, 'error');
      return;
    }
    if (!validServiceUrl(base)) {
      showToast(text().badUrl, 'error');
      return;
    }
    var body = { base_url: base };
    var token = byID('botAccessToken').value || '';
    if (token !== '') body.access_token = token;
    var adminSecret = byID('botAdminSecret').value || '';
    if (adminSecret !== '') body.admin_secret = adminSecret;
    if (panel) panel._connSaving = true;
    var button = byID('botSave');
    if (button) button.disabled = true;
    return api('/api/admin/bots/settings', { method: 'PUT', body: JSON.stringify(body) }).then(function (view) {
      byID('botAccessToken').value = '';
      byID('botAdminSecret').value = '';
      var urlInput = byID('botBaseUrl');
      if (urlInput) urlInput._serverValue = urlInput.value;
      render(view || {});
      showToast(text().saved, 'success');
    }).catch(function (err) {
      showToast(messageOf(err), 'error');
    }).then(function () {
      if (panel) panel._connSaving = false;
      if (button) button.disabled = false;
    });
  }

  function testConnection() {
    var panel = byID('tab-bots');
    if (panel && panel._connTesting) return;
    var view = panel && panel._botView || {};
    var typedUrl = (byID('botBaseUrl').value || '').trim().replace(/\/+$/, '');
    var savedUrl = String(view.base_url || '').trim().replace(/\/+$/, '');
    if (typedUrl !== savedUrl || (byID('botAccessToken').value || '') !== '') {
      showToast(text().saveBeforeTest, 'error');
      return;
    }
    if (panel) panel._connTesting = true;
    var button = byID('botTest');
    if (button) button.disabled = true;
    var status = byID('botConnStatus');
    if (status) status.textContent = text().testing;
    return api('/api/admin/bots/connection/test', { method: 'POST', body: '{}' }).then(function (result) {
      var count = result && result.instance_count != null ? result.instance_count : 0;
      var msg = text().connected.replace('{count}', String(count));
      byID('botConnStatus').textContent = msg;
      showToast(msg, 'success');
    }).catch(function (err) {
      byID('botConnStatus').textContent = messageOf(err);
      showToast(messageOf(err), 'error');
    }).then(function () {
      if (panel) panel._connTesting = false;
      if (button) button.disabled = false;
    });
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'bots',
      title: function () { return text().title; },
      subtitle: function () { return text().subtitle; },
      onOpen: openBotTab
    });
  }
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(applyBotI18n);
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', applyBotI18n);
  } else {
    applyBotI18n();
  }
})(window);
