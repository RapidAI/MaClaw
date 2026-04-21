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

  function serviceAccessLabel() {
    return currentLang === 'zh' ? '\u5df2\u6709\u670d\u52a1\u6743\u9650' : 'Service Access';
  }

  function serviceAccessTooltip(item) {
    const status = item && item.service_status;
    if (!status || !status.active) return '';
    const lines = [];
    const groupNames = (status.service_group_names || []).filter(Boolean);
    const models = (status.available_models || []).filter(Boolean);
    if (groupNames.length) lines.push((currentLang === 'zh' ? '\u670d\u52a1\u7ec4: ' : 'Service Groups: ') + groupNames.join(', '));
    if (models.length) lines.push((currentLang === 'zh' ? '\u53ef\u7528\u6a21\u578b: ' : 'Models: ') + models.join(', '));
    if (status.nearest_expires_at) lines.push((currentLang === 'zh' ? '\u6700\u8fd1\u5230\u671f: ' : 'Nearest Expiry: ') + status.nearest_expires_at);
    if (Number(status.credits_available || 0) > 0) lines.push('Credits: ' + String(status.credits_available));
    return lines.join('&#10;');
  }

  global.renderBlockedEmails = function renderBlockedEmails(items) {
    const helper = ui();
    document.getElementById('blockedCountHero').textContent = String(items.length);
    const root = document.getElementById('blockedEmails');
    if (!items.length) {
      root.innerHTML = hint(tr('emptyBlocked'));
      return;
    }
    if (helper && typeof helper.simpleCard === 'function') {
      root.innerHTML = items.map(function(item) {
        return helper.simpleCard({
          title: item.email,
          headRight: helper.badge(tr('addBlock'), 'danger'),
          body: helper.meta(item.reason || tr('noReason'))
        });
      }).join('');
      return;
    }
    root.innerHTML = items.map(function(item) {
      return '<div class="item"><div class="item-head"><div class="item-title">' + escapeHtml(item.email) + '</div><span class="badge danger">' + escapeHtml(tr('addBlock')) + '</span></div><div class="item-meta">' + escapeHtml(item.reason || tr('noReason')) + '</div></div>';
    }).join('');
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
    const pageSize = 40;
    const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
    if (global._boundUsersPage > totalPages) global._boundUsersPage = totalPages;
    if (global._boundUsersPage < 1) global._boundUsersPage = 1;
    const start = (global._boundUsersPage - 1) * pageSize;
    const pageItems = filtered.slice(start, start + pageSize);
    const lang = typeof currentLang !== 'undefined' ? currentLang : 'en';
    const searchHtml = '<div style="margin-bottom:10px"><input id="boundUsersSearchInput" placeholder="' + (lang === 'zh' ? '\u641c\u7d22\u90ae\u7bb1 / SN...' : 'Search email / SN...') + '" value="' + escapeHtml(global._boundUsersSearch || '') + '" style="max-width:320px" oninput="window._boundUsersSearch=this.value;window._boundUsersPage=1;clearTimeout(window._busDeb);window._busDeb=setTimeout(_renderBoundUsersPage,200)"></div>';
    const cards = pageItems.map(function(item) {
      var smartRoute = item.smart_route;
      var toggleId = 'sr_' + item.id;
      var userIdExpr = JSON.stringify(String(item.id || ''));
      var serviceBadge = item.has_service_access ? '<span class="badge ok" title="' + escapeHtml(serviceAccessTooltip(item)) + '" style="font-size:10px;padding:4px 8px">' + escapeHtml(serviceAccessLabel()) + '</span>' : '';
      return '<div class="user-card" style="flex-direction:column;gap:6px;cursor:default"><div style="min-width:0"><div class="item-title" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px">' + escapeHtml(item.email) + '</div><div class="item-meta mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px">' + escapeHtml(item.sn || tr('na')) + '</div></div><div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap"><span class="badge info" style="font-size:10px;padding:4px 8px">' + escapeHtml(item.enrollment_status || item.status || tr('active')) + '</span>' + serviceBadge + '<label class="toggle-label" title="' + (lang === 'zh' ? '\u667a\u80fd\u63a7\u5236' : 'Smart Route') + '"><input type="checkbox" id="' + toggleId + '" ' + (smartRoute ? 'checked' : '') + ' onchange="toggleSmartRoute(' + userIdExpr + ', this.checked)"><span>AI</span></label></div></div>';
    }).join('');
    var pagerHtml = '';
    var showCount = filtered.length > 0 ? (start + 1) + '-' + (start + pageItems.length) + ' / ' + filtered.length : '0 / 0';
    if (totalPages > 1 || query) {
      pagerHtml = '<div class="pager" style="margin-top:10px;display:flex;align-items:center;justify-content:center;gap:8px"><button class="btn-secondary" style="height:28px;font-size:12px;padding:0 10px" onclick="window._boundUsersPage=Math.max(1,window._boundUsersPage-1);_renderBoundUsersPage()" ' + (global._boundUsersPage <= 1 ? 'disabled' : '') + '>\u2039</button><span style="font-size:13px">' + showCount + '</span><button class="btn-secondary" style="height:28px;font-size:12px;padding:0 10px" onclick="window._boundUsersPage=Math.min(' + totalPages + ',window._boundUsersPage+1);_renderBoundUsersPage()" ' + (global._boundUsersPage >= totalPages ? 'disabled' : '') + '>\u203a</button></div>';
    }
    root.innerHTML = searchHtml + '<div class="user-grid-wrap" style="display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px">' + (pageItems.length ? cards : '<div class="hint" style="grid-column:1/-1">' + (lang === 'zh' ? '\u65e0\u5339\u914d\u7ed3\u679c' : 'No matches') + '</div>') + '</div>' + pagerHtml;
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
    const helper = ui();
    document.getElementById('inviteCountHero').textContent = String(items.length);
    const root = document.getElementById('invites');
    if (!items.length) {
      root.innerHTML = hint(tr('emptyInvites'));
      return;
    }
    root.innerHTML = items.map(function(item) {
      const status = item.status || 'active';
      const inviteExpr = JSON.stringify(String(item.id || ''));
      if (helper && typeof helper.simpleCard === 'function') {
        return helper.simpleCard({
          title: item.email,
          body: helper.meta(tr('role') + ': ' + (item.role || tr('roleViewer'))),
          headRight: '<div style="display:flex;align-items:center;gap:8px">' + helper.badge(formatStatus(status), statusBadgeClass(status)) + helper.actionButton(tr('deleteInviteLabel'), 'btn-danger', 'deleteInvite(' + inviteExpr + ')', { style: 'height:32px;font-size:12px;padding:0 10px' }) + '</div>'
        });
      }
      return '<div class="item"><div class="item-head"><div><div class="item-title">' + escapeHtml(item.email) + '</div><div class="item-meta">' + escapeHtml(tr('role') + ': ' + (item.role || tr('roleViewer'))) + '</div></div><div style="display:flex;align-items:center;gap:8px"><span class="badge ' + escapeHtml(statusBadgeClass(status)) + '">' + escapeHtml(formatStatus(status)) + '</span></div></div></div>';
    }).join('');
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
      showToast((currentLang === 'zh' ? '\u52a0\u8f7d\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5931\u8d25: ' : 'Load content audit config failed: ') + err.message, 'error');
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
      showToast(currentLang === 'zh' ? '\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5df2\u4fdd\u5b58' : 'Content audit config saved', 'success');
    } catch (err) {
      showToast((currentLang === 'zh' ? '\u4fdd\u5b58\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5931\u8d25: ' : 'Save content audit config failed: ') + err.message, 'error');
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
      showToast(enabled ? '\u5df2\u5f00\u542f\u5168\u5458\u667a\u80fd\u8def\u7531' : '\u5df2\u5173\u95ed\u5168\u5458\u667a\u80fd\u8def\u7531', 'success');
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
