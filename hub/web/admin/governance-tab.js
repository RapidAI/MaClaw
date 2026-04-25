/*
 * Governance admin module.
 * ASCII only.
 */
(function(global) {
  function ui() {
    return global.AdminUI || null;
  }

  function hint(message) {
    const helper = ui();
    return helper && typeof helper.hint === 'function'
      ? helper.hint(message)
      : '<div class="hint">' + escapeHtml(message || '') + '</div>';
  }

  function formatStatus(raw) {
    const key = String(raw || '').trim().toLowerCase();
    const map = { approved: 'statusApproved', pending: 'statusPending', enrolled: 'statusEnrolled', active: 'active' };
    return map[key] ? tr(map[key]) : (raw || tr('active'));
  }

  function statusBadgeClass(raw) {
    const key = String(raw || '').trim().toLowerCase();
    if (key === 'approved' || key === 'enrolled') return 'ok';
    if (key === 'pending') return 'warn';
    return 'info';
  }

  var GOV_I18N = {
    serviceAccess: { zh: '\u5df2\u6709\u670d\u52a1\u6743\u9650', en: 'Service Access' },
    serviceGroupsPrefix: { zh: '\u670d\u52a1\u7ec4: ', en: 'Service Groups: ' },
    modelsPrefix: { zh: '\u53ef\u7528\u6a21\u578b: ', en: 'Models: ' },
    nearestExpiryPrefix: { zh: '\u6700\u8fd1\u5230\u671f: ', en: 'Nearest Expiry: ' },
    creditsPrefix: { zh: '\u79ef\u5206: ', en: 'Credits: ' },
    smartRouteLabel: { zh: '\u667a\u80fd\u63a7\u5236', en: 'Smart Route' },
    boundUsersSearchPlaceholder: { zh: '\u641c\u7d22\u90ae\u7bb1 / SN...', en: 'Search email / SN...' },
    noMatches: { zh: '\u65e0\u5339\u914d\u7ed3\u679c', en: 'No matches' },
    loadContentAuditConfigFailed: { zh: '\u52a0\u8f7d\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5931\u8d25: ', en: 'Load content audit config failed: ' },
    contentAuditConfigSaved: { zh: '\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5df2\u4fdd\u5b58', en: 'Content audit config saved' },
    saveContentAuditConfigFailed: { zh: '\u4fdd\u5b58\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5931\u8d25: ', en: 'Save content audit config failed: ' },
    smartRouteAllEnabled: { zh: '\u5df2\u5f00\u542f\u5168\u5458\u667a\u80fd\u8def\u7531', en: 'Smart Route enabled for all users' },
    smartRouteAllDisabled: { zh: '\u5df2\u5173\u95ed\u5168\u5458\u667a\u80fd\u8def\u7531', en: 'Smart Route disabled for all users' }
  };

  function gt(key) {
    var entry = GOV_I18N[key];
    return entry ? (currentLang === 'zh' ? entry.zh : entry.en) : key;
  }

  function serviceAccessLabel() {
    return gt('serviceAccess');
  }

  function serviceAccessTooltip(item) {
    const status = item && item.service_status;
    if (!status) return '';
    const lines = [];
    if (!status.active) {
      const reasons = (status.inactive_reasons || []).filter(Boolean);
      if (reasons.length) return reasons.join('&#10;');
      return '';
    }
    const groupNames = (status.service_group_names || []).filter(Boolean);
    const models = (status.available_models || []).filter(Boolean);
    if (groupNames.length) lines.push(gt('serviceGroupsPrefix') + groupNames.join(', '));
    if (models.length) lines.push(gt('modelsPrefix') + models.join(', '));
    if (status.nearest_expires_at) lines.push(gt('nearestExpiryPrefix') + status.nearest_expires_at);
    if (Number(status.credits_available || 0) > 0) lines.push(gt('creditsPrefix') + String(status.credits_available));
    return lines.join('&#10;');
  }

  global.renderBlockedEmails = function renderBlockedEmails(items) {
    document.getElementById('blockedCountHero').textContent = String(items.length);
    const root = document.getElementById('blockedEmails');
    if (!items.length) {
      root.innerHTML = hint(tr('emptyBlocked'));
      return;
    }
    root.innerHTML = '<div style="display:grid;gap:6px">' + items.map(function(item) {
      return '<div class="item" style="padding:10px 12px;border-radius:12px;background:#fff;border:1px solid rgba(31,34,48,.06);box-shadow:none">'
        + '<div style="display:grid;grid-template-columns:minmax(180px,1.2fr) minmax(0,2fr) auto;gap:10px;align-items:center">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.email) + '</div></div>'
        + '<div class="item-meta" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.reason || tr('noReason')) + '</div>'
        + '<span class="badge danger" style="padding:4px 8px;font-size:10px">' + escapeHtml(tr('addBlock')) + '</span>'
        + '</div></div>';
    }).join('') + '</div>';
  };

  global.renderBoundUsers = function renderBoundUsers(items) {
    const root = document.getElementById('boundUsers');
    if (!items.length) {
      root.innerHTML = hint(tr('emptyBoundUsers'));
      return;
    }
    global._boundUsersAll = items;
    global._boundUsersPage = global._boundUsersPage || 1;
    global._boundUsersSearch = global._boundUsersSearch || '';
    const smartAllEl = document.getElementById('smartRouteAllToggle');
    if (smartAllEl && !smartAllEl.dataset.loaded) {
      smartAllEl.dataset.loaded = '1';
      loadSmartRouteAll().then(function(enabled) { smartAllEl.checked = enabled; });
    }
    global._renderBoundUsersPage();
  };

  global._renderBoundUsersPage = function _renderBoundUsersPage() {
    const root = document.getElementById('boundUsers');
    const items = global._boundUsersAll || [];
    const query = (global._boundUsersSearch || '').trim().toLowerCase();
    const filtered = query ? items.filter(function(item) {
      return (item.email || '').toLowerCase().indexOf(query) !== -1 || (item.sn || '').toLowerCase().indexOf(query) !== -1;
    }) : items;
    const pageSize = 36;
    const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
    if (global._boundUsersPage > totalPages) global._boundUsersPage = totalPages;
    if (global._boundUsersPage < 1) global._boundUsersPage = 1;
    const start = (global._boundUsersPage - 1) * pageSize;
    const pageItems = filtered.slice(start, start + pageSize);
    const searchHtml = '<div style="margin-bottom:8px"><input id="boundUsersSearchInput" placeholder="' + gt('boundUsersSearchPlaceholder') + '" value="' + escapeHtml(global._boundUsersSearch || '') + '" style="max-width:260px;height:34px" oninput="window._boundUsersSearch=this.value;window._boundUsersPage=1;clearTimeout(window._busDeb);window._busDeb=setTimeout(_renderBoundUsersPage,200)"></div>';
    const rows = pageItems.map(function(item) {
      var smartRoute = item.smart_route;
      var toggleId = 'sr_' + item.id;
      var userIdValue = String(item.id || '').replace(/\\/g, '\\\\').replace(/'/g, "\\'");
      var serviceBadge = item.has_service_access ? '<span class="badge ok" title="' + escapeHtml(serviceAccessTooltip(item)) + '" style="padding:4px 8px;font-size:10px">' + escapeHtml(serviceAccessLabel()) + '</span>' : '<span class="badge info" style="padding:4px 8px;font-size:10px">-</span>';
      return '<div class="item" style="padding:10px 12px;border-radius:12px;background:#fff;border:1px solid rgba(31,34,48,.06);box-shadow:none">'
        + '<div style="display:grid;grid-template-columns:minmax(180px,1.45fr) minmax(110px,.95fr) auto auto auto;gap:10px;align-items:center">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.email) + '</div></div>'
        + '<div class="item-meta mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.sn || tr('na')) + '</div>'
        + '<span class="badge info" style="padding:4px 8px;font-size:10px">' + escapeHtml(formatStatus(item.enrollment_status || item.status || 'active')) + '</span>'
        + serviceBadge
        + '<label class="toggle-label" title="' + gt('smartRouteLabel') + '" style="justify-content:flex-end;font-size:11px"><input type="checkbox" id="' + toggleId + '" ' + (smartRoute ? 'checked' : '') + ' data-user-id="' + escapeHtml(String(item.id || '')) + '" onchange="toggleSmartRoute(this.dataset.userId, this.checked)"><span>AI</span></label>'
        + '</div></div>';
    }).join('');
    var pagerHtml = '';
    var showCount = filtered.length > 0 ? (start + 1) + '-' + (start + pageItems.length) + ' / ' + filtered.length : '0 / 0';
    if (totalPages > 1 || query) {
      pagerHtml = '<div class="pager" style="margin-top:8px;display:flex;align-items:center;justify-content:center;gap:6px"><button class="btn-secondary" style="height:28px;font-size:11px;padding:0 10px" onclick="window._boundUsersPage=Math.max(1,window._boundUsersPage-1);_renderBoundUsersPage()" ' + (global._boundUsersPage <= 1 ? 'disabled' : '') + '>Prev</button><span style="font-size:11px">' + showCount + '</span><button class="btn-secondary" style="height:28px;font-size:11px;padding:0 10px" onclick="window._boundUsersPage=Math.min(' + totalPages + ',window._boundUsersPage+1);_renderBoundUsersPage()" ' + (global._boundUsersPage >= totalPages ? 'disabled' : '') + '>Next</button></div>';
    }
    root.innerHTML = searchHtml + '<div class="user-grid-wrap" style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px">' + (pageItems.length ? rows : '<div class="hint" style="grid-column:1 / -1">' + gt('noMatches') + '</div>') + '</div>' + pagerHtml;
    var searchInput = document.getElementById('boundUsersSearchInput');
    if (searchInput && query) {
      searchInput.focus();
      searchInput.setSelectionRange(searchInput.value.length, searchInput.value.length);
    }
  };

  global.lookupScopedUser = async function lookupScopedUser(email) {
    if (!email) return null;
    const data = await api('/api/admin/users/lookup?email=' + encodeURIComponent(email));
    return data.user || null;
  };

  global.manualBind = async function manualBind() {
    try {
      const email = document.getElementById('bindEmail').value.trim();
      const data = await api('/api/admin/users/manual-bind', { method: 'POST', body: JSON.stringify({ email: email }) });
      const msg = tr('bindDone', { email: data.user && data.user.email ? data.user.email : email, sn: data.user && data.user.sn ? data.user.sn : tr('na') });
      setOutput(msg);
      showToast(msg, 'success');
      await Promise.all([global.loadBoundUsers(), global.loadMachines()]);
    } catch (err) {
      const msg = tr('bindFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadBoundUsers = async function loadBoundUsers() {
    try {
      const data = await api('/api/admin/users');
      global.renderBoundUsers(data.users || []);
    } catch (err) {
      setOutput(tr('bindFailed', { error: err.message }));
    }
  };

  global.addBlockedEmail = async function addBlockedEmail() {
    try {
      await api('/api/admin/blocklist', { method: 'POST', body: JSON.stringify({ email: document.getElementById('blockedEmail').value.trim(), reason: document.getElementById('blockedReason').value.trim() }) });
      const msg = tr('blockedAdded');
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadBlockedEmails();
    } catch (err) {
      const msg = tr('blockedAddFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadBlockedEmails = async function loadBlockedEmails() {
    try {
      const data = await api('/api/admin/blocklist');
      global.renderBlockedEmails(data.blocked_emails || []);
    } catch (err) {
      setOutput(tr('blockedLoadFailed', { error: err.message }));
    }
  };

  global.renderInvites = function renderInvites(items) {
    global._invitesAll = items || [];
    document.getElementById('inviteCountHero').textContent = String(items.length);
    const root = document.getElementById('invites');
    if (!items.length) {
      root.innerHTML = hint(tr('emptyInvites'));
      return;
    }
    root.innerHTML = '<div style="display:grid;gap:6px">' + items.map(function(item) {
      const status = item.status || 'active';
      const inviteValue = String(item.id || '').replace(/\\/g, '\\\\').replace(/'/g, "\\'");
      return '<div class="item" style="padding:10px 12px;border-radius:12px;background:#fff;border:1px solid rgba(31,34,48,.06);box-shadow:none">'
        + '<div style="display:grid;grid-template-columns:minmax(180px,1.3fr) minmax(90px,.8fr) auto auto;gap:10px;align-items:center">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.email) + '</div></div>'
        + '<div class="item-meta" style="font-size:11px">' + escapeHtml(item.role || tr('roleViewer')) + '</div>'
        + '<span class="badge ' + escapeHtml(statusBadgeClass(status)) + '" style="padding:4px 8px;font-size:10px">' + escapeHtml(formatStatus(status)) + '</span>'
        + '<button class="btn-danger" style="height:28px;font-size:11px;padding:0 9px" data-invite-id="' + escapeHtml(String(item.id || '')) + '" onclick="deleteInvite(this.dataset.inviteId)">' + escapeHtml(tr('deleteInviteLabel')) + '</button>'
        + '</div></div>';
    }).join('') + '</div>';
  };

  global.addInvite = async function addInvite() {
    try {
      await api('/api/admin/invites', { method: 'POST', body: JSON.stringify({ email: document.getElementById('inviteEmail').value.trim(), role: document.getElementById('inviteRole').value }) });
      const msg = tr('inviteCreated');
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadInvites();
    } catch (err) {
      const msg = tr('inviteAddFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadInvites = async function loadInvites() {
    try {
      const data = await api('/api/admin/invites');
      global.renderInvites(data.invites || []);
    } catch (err) {
      setOutput(tr('inviteLoadFailed', { error: err.message }));
    }
  };

  global.deleteInvite = async function deleteInvite(id) {
    if (!confirm(tr('deleteInviteLabel') + '?')) return;
    try {
      await api('/api/admin/invites/' + encodeURIComponent(id), { method: 'DELETE' });
      const msg = tr('inviteDeleted');
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadInvites();
    } catch (err) {
      const msg = tr('inviteDeleteFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadContentAuditConfig = async function loadContentAuditConfig() {
    try {
      const data = await api('/api/admin/content_audit/config');
      document.getElementById('caProgPath').value = data.program_path || '';
      document.getElementById('caTimeoutSec').value = data.timeout_seconds || 30;
      document.getElementById('caTimeoutPolicy').value = data.timeout_policy || 'block';
      document.getElementById('caKeywords').value = (data.keywords || []).join('\n');
    } catch (err) {
      showToast(gt('loadContentAuditConfigFailed') + err.message, 'error');
    }
  };

  global.saveContentAuditConfig = async function saveContentAuditConfig() {
    try {
      const keywords = document.getElementById('caKeywords').value.split('\n').map(function(item) { return item.trim(); }).filter(Boolean);
      const payload = {
        program_path: document.getElementById('caProgPath').value.trim(),
        timeout_seconds: parseInt(document.getElementById('caTimeoutSec').value, 10) || 30,
        timeout_policy: document.getElementById('caTimeoutPolicy').value,
        keywords: keywords
      };
      await api('/api/admin/content_audit/config', { method: 'PUT', body: JSON.stringify(payload) });
      showToast(gt('contentAuditConfigSaved'), 'success');
    } catch (err) {
      showToast(gt('saveContentAuditConfigFailed') + err.message, 'error');
    }
  };

  global.toggleSmartRoute = async function toggleSmartRoute(userID, enabled) {
    try {
      await api('/api/admin/users/smart_route', { method: 'POST', body: JSON.stringify({ user_id: userID, enabled: enabled }) });
      var items = global._boundUsersAll || [];
      for (var i = 0; i < items.length; i += 1) {
        if (items[i].id === userID) {
          items[i].smart_route = enabled;
          break;
        }
      }
      global._renderBoundUsersPage();
    } catch (err) {
      showToast(err.message, 'error');
      var checkbox = document.getElementById('sr_' + userID);
      if (checkbox) checkbox.checked = !enabled;
    }
  };

  global.loadSmartRouteAll = async function loadSmartRouteAll() {
    try {
      const data = await api('/api/admin/smart_route_all');
      return data.enabled || false;
    } catch (_) {
      return false;
    }
  };

  global.toggleSmartRouteAll = async function toggleSmartRouteAll(enabled) {
    try {
      await api('/api/admin/smart_route_all', { method: 'PUT', body: JSON.stringify({ enabled: enabled }) });
      showToast(enabled ? gt('smartRouteAllEnabled') : gt('smartRouteAllDisabled'), 'success');
    } catch (err) {
      showToast(err.message, 'error');
      var toggle = document.getElementById('smartRouteAllToggle');
      if (toggle) toggle.checked = !enabled;
    }
  };

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'governance',
      onOpen: function() {
        if (!token()) return;
        global.loadBlockedEmails();
        global.loadBoundUsers();
        global.loadInvites();
      }
    });
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() {
      if (global._boundUsersAll && typeof global._renderBoundUsersPage === 'function') global._renderBoundUsersPage();
      if (global._invitesAll && typeof global.renderInvites === 'function') global.renderInvites(global._invitesAll);
    });
  }

  const previousRefreshAll = global.refreshAll;
  global.refreshAll = async function refreshAll() {
    if (typeof previousRefreshAll === 'function') {
      await previousRefreshAll();
    }
    if (token()) {
      await global.loadInvites();
    }
  };
})(window);
