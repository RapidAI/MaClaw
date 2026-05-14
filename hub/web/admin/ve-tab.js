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
      emptyPending: 'No pending registration requests.',
      emptyActive: 'No active digital employees.',
      name: 'Name',
      skillDesc: 'Skill Description',
      accessPolicy: 'Access Policy',
      onlineStatus: 'Online Status',
      registeredAt: 'Registered At',
      actions: 'Actions',
      approve: 'Approve',
      reject: 'Reject',
      disable: 'Disable',
      online: 'Online',
      offline: 'Offline',
      policyPublic: 'Public',
      policyWhitelist: 'Whitelist',
      policyBlacklist: 'Blacklist',
      policyPerRequest: 'Per-Request',
      groupConfigTitle: 'Group Chat Configuration',
      groupConfigDesc: 'Maximum number of digital employee participants in a single group chat.',
      maxParticipants: 'Max Participants',
      saveConfig: 'Save',
      configSaved: 'Group chat configuration saved.',
      configSaveFailed: 'Save config failed: {error}',
      approveSuccess: 'Virtual employee approved.',
      approveFailed: 'Approve failed: {error}',
      rejectSuccess: 'Virtual employee rejected.',
      rejectFailed: 'Reject failed: {error}',
      disableSuccess: 'Virtual employee disabled.',
      disableFailed: 'Disable failed: {error}',
      loadFailed: 'Load digital employees failed: {error}',
      refresh: 'Refresh',
      quotaInfo: 'Active: {active} / Quota: {quota}',
      status: 'Status'
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
      emptyPending: '\u6682\u65e0\u5f85\u5ba1\u6279\u7684\u6ce8\u518c\u8bf7\u6c42\u3002',
      emptyActive: '\u6682\u65e0\u5df2\u6fc0\u6d3b\u7684\u6570\u5b57\u5458\u5de5\u3002',
      name: '\u540d\u79f0',
      skillDesc: '\u6280\u80fd\u63cf\u8ff0',
      accessPolicy: '\u8bbf\u95ee\u7b56\u7565',
      onlineStatus: '\u5728\u7ebf\u72b6\u6001',
      registeredAt: '\u6ce8\u518c\u65f6\u95f4',
      actions: '\u64cd\u4f5c',
      approve: '\u901a\u8fc7',
      reject: '\u62d2\u7edd',
      disable: '\u7981\u7528',
      online: '\u5728\u7ebf',
      offline: '\u79bb\u7ebf',
      policyPublic: '\u516c\u5f00',
      policyWhitelist: '\u767d\u540d\u5355',
      policyBlacklist: '\u9ed1\u540d\u5355',
      policyPerRequest: '\u9010\u6b21\u6388\u6743',
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
      loadFailed: '\u52a0\u8f7d\u6570\u5b57\u5458\u5de5\u5217\u8868\u5931\u8d25\uff1a{error}',
      refresh: '\u5237\u65b0',
      quotaInfo: '\u5df2\u6fc0\u6d3b\uff1a{active} / \u914d\u989d\uff1a{quota}',
      status: '\u72b6\u6001'
    }
  };

  function vt(key) {
    var lang = global.currentLang || 'zh';
    var dict = VE_I18N[lang] || VE_I18N['zh'];
    return dict[key] || key;
  }

  // --- State ---
  var veListCache = [];
  var veGroupConfig = { max_group_participants: 5 };
  var veQuota = 0;
  var veActiveCount = 0;

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

  // --- Rendering ---
  function renderVEListHeader() {
    return '<div class="row header" style="grid-template-columns:1fr 1.5fr .8fr .7fr .9fr .8fr">' +
      '<div>' + vt('name') + '</div>' +
      '<div>' + vt('skillDesc') + '</div>' +
      '<div>' + vt('accessPolicy') + '</div>' +
      '<div>' + vt('onlineStatus') + '</div>' +
      '<div>' + vt('registeredAt') + '</div>' +
      '<div>' + vt('actions') + '</div>' +
      '</div>';
  }

  function renderVERow(ve, actionButtons) {
    return '<div class="row" style="grid-template-columns:1fr 1.5fr .8fr .7fr .9fr .8fr">' +
      '<div><strong>' + escapeHtml(truncate(ve.name || '', 50)) + '</strong></div>' +
      '<div class="item-meta">' + escapeHtml(truncate(ve.skill_description || '', 100)) + '</div>' +
      '<div><span class="badge info">' + escapeHtml(formatAccessPolicy(ve.access_policy)) + '</span></div>' +
      '<div>' + formatOnlineStatus(ve.online_status) + '</div>' +
      '<div class="item-meta">' + escapeHtml(formatDate(ve.registered_at)) + '</div>' +
      '<div style="display:flex;gap:4px;flex-wrap:wrap">' + actionButtons + '</div>' +
      '</div>';
  }

  function actionBtn(label, cls, onclick) {
    return '<button class="' + escapeHtml(cls) + '" style="height:27px;font-size:11px;padding:0 8px" onclick="' + escapeHtml(onclick) + '">' + escapeHtml(label) + '</button>';
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
      var disableBtn = actionBtn(vt('disable'), 'btn-danger', 'veDisable(' + veIDExpr + ')');
      return renderVERow(ve, disableBtn);
    }).join('');
  }

  function renderGroupConfig() {
    var container = document.getElementById('veGroupConfigForm');
    if (!container) return;
    var input = document.getElementById('veMaxParticipantsInput');
    if (input) {
      input.value = String(veGroupConfig.max_group_participants || 5);
    }
  }

  function renderQuotaInfo() {
    var el = document.getElementById('veQuotaInfo');
    if (!el) return;
    el.textContent = vt('quotaInfo')
      .replace('{active}', String(veActiveCount))
      .replace('{quota}', String(veQuota));
  }

  // --- API Calls ---
  global.loadVEList = async function loadVEList() {
    try {
      var data = await api('/api/ve/list');
      veListCache = data.employees || [];
      veGroupConfig = data.group_config || { max_group_participants: 5 };
      veQuota = data.quota || 0;
      veActiveCount = data.active_count || 0;
      renderPendingList(veListCache);
      renderActiveList(veListCache);
      renderGroupConfig();
      renderQuotaInfo();
    } catch (err) {
      var msg = vt('loadFailed').replace('{error}', err.message);
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

  global.veSaveGroupConfig = async function veSaveGroupConfig() {
    var input = document.getElementById('veMaxParticipantsInput');
    if (!input) return;
    var val = parseInt(input.value, 10);
    if (isNaN(val) || val < 1 || val > 10) {
      showToast('max_group_participants must be 1-10', 'error');
      return;
    }
    try {
      await api('/api/ve/config', {
        method: 'PUT',
        body: JSON.stringify({ max_group_participants: val })
      });
      veGroupConfig.max_group_participants = val;
      showToast(vt('configSaved'), 'success');
      // Push ve:group_config event is handled server-side on config change
    } catch (err) {
      var msg = vt('configSaveFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
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
    setText('veGroupConfigTitle', vt('groupConfigTitle'));
    setText('veGroupConfigDesc', vt('groupConfigDesc'));
    setText('veMaxParticipantsLabel', vt('maxParticipants'));
    setText('veSaveConfigBtn', vt('saveConfig'));
    setText('vePendingTitle', vt('pendingTitle'));
    setText('veActiveTitle', vt('activeTitle'));
    setText('veRefreshBtn', vt('refresh'));
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
