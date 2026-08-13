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
    regularUsers: { zh: '\u666e\u901a\u7528\u6237', en: 'Regular Users' },
    virtualEmployees: { zh: '\u865a\u62df\u5458\u5de5', en: 'Virtual Employees' },
    virtualEmployeeHint: { zh: 'VE Platform \u6ce8\u518c', en: 'Registered from VE Platform' },
    virtualUserDeleteLabel: { zh: '\u5f3a\u5236\u5220\u9664', en: 'Force Delete' },
    virtualUserDeleteWarning: { zh: '\u8be5\u865a\u62df\u7528\u6237\u6765\u81ea VE Platform\uff0c\u901a\u5e38\u4e0d\u5e94\u7531 Hub \u7ba1\u7406\u5458\u76f4\u63a5\u5220\u9664\u3002\u53ea\u6709\u5728\u786e\u8ba4\u9700\u8981\u5f3a\u5236\u6e05\u7406\u65f6\u624d\u7ee7\u7eed\u3002', en: 'This virtual user was registered from VE Platform. Hub administrators normally should not delete it directly. Continue only when force cleanup is truly necessary.' },
    virtualUserDeletePasswordPrompt: { zh: '\u8f93\u5165\u7ba1\u7406\u5458\u5bc6\u7801\uff0c\u5f3a\u5236\u5220\u9664\u8be5 VE Platform \u865a\u62df\u7528\u6237\uff1a', en: 'Enter administrator password to force delete this VE Platform virtual user:' },
    virtualUserDeleteSuccess: { zh: '\u865a\u62df\u7528\u6237\u5df2\u5f3a\u5236\u5220\u9664', en: 'Virtual user force deleted' },
    virtualUserDeleteFailed: { zh: '\u5f3a\u5236\u5220\u9664\u865a\u62df\u7528\u6237\u5931\u8d25: {error}', en: 'Force delete virtual user failed: {error}' },
    serviceGroupsPrefix: { zh: '\u670d\u52a1\u7ec4: ', en: 'Service Groups: ' },
    modelsPrefix: { zh: '\u53ef\u7528\u6a21\u578b: ', en: 'Models: ' },
    nearestExpiryPrefix: { zh: '\u6700\u8fd1\u5230\u671f: ', en: 'Nearest Expiry: ' },
    creditsPrefix: { zh: '\u79ef\u5206: ', en: 'Credits: ' },
    creditsTotal: { zh: '\u62e5\u6709 Credits', en: 'Total Credits' },
    creditsUsed: { zh: '\u5df2\u6d88\u8d39', en: 'Used' },
    creditsRemaining: { zh: '\u5269\u4f59', en: 'Remaining' },
    grantStatusActive: { zh: '\u751f\u6548\u4e2d', en: 'Active' },
    grantStatusPeriodLimited: { zh: '\u5468\u671f\u9650\u6d41', en: 'Period limit' },
    grantStatusQueued: { zh: '\u5f85\u751f\u6548', en: 'Pending start' },
    grantStatusExhausted: { zh: '\u989d\u5ea6\u5df2\u7528\u5c3d', en: 'Credits exhausted' },
    grantStatusExpired: { zh: '\u5df2\u8fc7\u671f', en: 'Expired' },
    grantStatusInactive: { zh: '\u672a\u751f\u6548', en: 'Inactive' },
    grantRetryAfterAt: { zh: '\u6062\u590d\u65f6\u95f4 {time}', en: 'Restores at {time}' },
    smartRouteLabel: { zh: '\u667a\u80fd\u63a7\u5236', en: 'Smart Route' },
    smartRouteAllLabel: { zh: '\u5168\u5458\u667a\u80fd\u8def\u7531', en: 'Smart Route for all' },
    emailVerifiedTooltip: { zh: '\u90ae\u7bb1\u5df2\u9a8c\u8bc1', en: 'Email verified' },
    referredUser: { zh: '\ud83c\udf81 \u53d7\u9080\u6ce8\u518c', en: '\ud83c\udf81 Referred signup' },
    referredUserTooltip: { zh: '\u901a\u8fc7\u7528\u6237\u9080\u8bf7\u5b8c\u6210\u6ce8\u518c', en: 'Registered through a user referral' },
    referredOnly: { zh: '\u4ec5\u770b\u53d7\u9080\u6ce8\u518c', en: 'Referred signups only' },
    contactEmailLabel: { zh: '\u90ae\u7bb1', en: 'Email' },
    contactPhoneLabel: { zh: '\u624b\u673a', en: 'Phone' },
    boundUsersSearchPlaceholder: { zh: '\u641c\u7d22\u90ae\u7bb1 / \u624b\u673a / SN...', en: 'Search email / phone / SN...' },
    syncPhoneRoutes: { zh: '\u540c\u6b65\u624b\u673a\u8def\u7531', en: 'Sync Phone Routes' },
    syncPhoneRoutesBusy: { zh: '\u540c\u6b65\u4e2d...', en: 'Syncing...' },
    syncPhoneRoutesRunning: { zh: '\u6b63\u5728\u540c\u6b65\u5df2\u9a8c\u8bc1\u624b\u673a\u8def\u7531...', en: 'Syncing verified phone routes...' },
    syncPhoneRoutesDone: { zh: '\u5df2\u540c\u6b65 {count} \u6761\u624b\u673a\u8def\u7531', en: 'Synced {count} phone route(s)' },
    syncPhoneRoutesFailed: { zh: '\u540c\u6b65\u624b\u673a\u8def\u7531\u5931\u8d25: {error}', en: 'Sync phone routes failed: {error}' },
    noMatches: { zh: '\u65e0\u5339\u914d\u7ed3\u679c', en: 'No matches' },
    loadContentAuditConfigFailed: { zh: '\u52a0\u8f7d\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5931\u8d25: ', en: 'Load content audit config failed: ' },
    contentAuditConfigSaved: { zh: '\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5df2\u4fdd\u5b58', en: 'Content audit config saved' },
    saveContentAuditConfigFailed: { zh: '\u4fdd\u5b58\u5185\u5bb9\u5ba1\u6838\u914d\u7f6e\u5931\u8d25: ', en: 'Save content audit config failed: ' },
    smartRouteAllEnabled: { zh: '\u5df2\u5f00\u542f\u5168\u5458\u667a\u80fd\u8def\u7531', en: 'Smart Route enabled for all users' },
    unbindUser: { zh: '\u5220\u9664\u7ed1\u5b9a', en: 'Remove Binding' },
    unbindConfirm: { zh: '\u786e\u8ba4\u5220\u9664 {email} \u7684 Hub \u7ed1\u5b9a\u5417\uff1f\u8fd9\u4f1a\u540c\u65f6\u6e05\u7406\u8be5\u7528\u6237\u7684\u673a\u5668\u548c IM \u7ed1\u5b9a\u3002', en: 'Remove Hub binding for {email}? This also clears the user machines and IM bindings.' },
    unbindSuccess: { zh: '\u5df2\u5220\u9664 {email} \u7684 Hub \u7ed1\u5b9a', en: 'Removed Hub binding for {email}' },
    unbindFailed: { zh: '\u5220\u9664\u7ed1\u5b9a\u5931\u8d25: {error}', en: 'Remove binding failed: {error}' },
    smartRouteAllDisabled: { zh: '\u5df2\u5173\u95ed\u5168\u5458\u667a\u80fd\u8def\u7531', en: 'Smart Route disabled for all users' }
  };

  function gt(key) {
    var entry = GOV_I18N[key];
    return entry ? (currentLang === 'zh' ? entry.zh : entry.en) : key;
  }

  function serviceAccessLabel() {
    return gt('serviceAccess');
  }

  function governanceUserType(item) {
    return item && item.is_virtual_employee ? 'virtual' : 'regular';
  }

  function governanceUserTypeLabel(type) {
    return gt(type === 'virtual' ? 'virtualEmployees' : 'regularUsers');
  }

  function uniqueStrings(values) {
    var seen = {};
    var out = [];
    (values || []).forEach(function(value) {
      value = String(value || '').trim();
      if (!value) return;
      var key = value.toLowerCase();
      if (seen[key]) return;
      seen[key] = true;
      out.push(value);
    });
    return out;
  }

  function normalizePhoneDigits(value) {
    return String(value || '').replace(/\D/g, '');
  }

  function boundUserEmails(item) {
    var values = Array.isArray(item && item.emails) ? item.emails.slice() : [];
    var email = String(item && item.email || '').trim();
    if (email && email.toLowerCase().indexOf('phone:') !== 0 && email.indexOf('@') !== -1) values.unshift(email);
    return uniqueStrings(values);
  }

  function boundUserPhones(item) {
    var values = Array.isArray(item && item.phones) ? item.phones.slice() : [];
    if (item && item.phone) values.unshift(item.phone);
    var email = String(item && item.email || '').trim();
    if (email.toLowerCase().indexOf('phone:') === 0) values.unshift(email.slice(6));
    return uniqueStrings(values);
  }

  function boundUserSearchText(item) {
    var parts = [item && item.email, item && item.sn, item && item.id].concat(boundUserEmails(item), boundUserPhones(item));
    var normalizedPhones = boundUserPhones(item).map(normalizePhoneDigits);
    return parts.concat(normalizedPhones).map(function(value) { return String(value || '').toLowerCase(); }).join(' ');
  }

  function boundUserMatchesSearch(item, query) {
    query = String(query || '').trim().toLowerCase();
    if (!query) return true;
    var text = boundUserSearchText(item);
    if (text.indexOf(query) !== -1) return true;
    var digits = normalizePhoneDigits(query);
    return digits.length > 0 && text.indexOf(digits) !== -1;
  }

  function renderBoundUserContacts(item) {
    var rows = [];
    var emails = boundUserEmails(item);
    var phones = boundUserPhones(item);
    if (emails.length) {
      rows.push('<div class="item-meta" style="font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted)">' + escapeHtml(gt('contactEmailLabel')) + ': ' + escapeHtml(emails.join(', ')) + '</div>');
    }
    if (phones.length) {
      rows.push('<div class="item-meta mono" style="font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted)">' + escapeHtml(gt('contactPhoneLabel')) + ': ' + escapeHtml(phones.join(', ')) + '</div>');
    }
    return rows.join('');
  }

  function boundUserDisplayName(item) {
    var emails = boundUserEmails(item);
    if (emails.length) return emails[0];
    var phones = boundUserPhones(item);
    if (phones.length) return phones[0];
    return item && item.email || '';
  }

  function groupBoundUsers(items) {
    var groups = { regular: [], virtual: [] };
    (items || []).forEach(function(item) {
      groups[governanceUserType(item)].push(item);
    });
    return groups;
  }

  function promptGovernanceAdminPassword(title, message, confirmLabel) {
    if (!document || !document.createElement || !document.body) {
      return Promise.resolve(prompt(message));
    }
    return new Promise(function(resolve) {
      var overlay = document.createElement('div');
      overlay.className = 'session-modal-overlay show';
      overlay.style.cssText = 'z-index:9999;background:rgba(15,23,42,.42);padding:18px';
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="governanceForceDeleteTitle" style="width:min(420px,100%);max-height:none;overflow:visible;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22)">' +
        '<div class="item-title" id="governanceForceDeleteTitle" style="margin-bottom:8px">' + escapeHtml(title) + '</div>' +
        '<div class="item-meta" style="margin-bottom:12px;white-space:pre-wrap">' + escapeHtml(message) + '</div>' +
        '<input id="governanceForceDeletePasswordInput" type="password" autocomplete="current-password" style="width:100%;height:36px;margin-bottom:12px">' +
        '<div class="actions" style="justify-content:flex-end;gap:8px">' +
        '<button type="button" class="btn-ghost" id="governanceForceDeleteCancelBtn">' + escapeHtml(tr('closeDialog')) + '</button>' +
        '<button type="button" class="btn-danger" id="governanceForceDeleteConfirmBtn">' + escapeHtml(confirmLabel) + '</button>' +
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
      var input = overlay.querySelector('#governanceForceDeletePasswordInput');
      var cancel = overlay.querySelector('#governanceForceDeleteCancelBtn');
      var ok = overlay.querySelector('#governanceForceDeleteConfirmBtn');
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

  function formatCreditsValue(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n)) n = 0;
    n = Math.round(n * 10000) / 10000;
    return n.toLocaleString(undefined, { maximumFractionDigits: 4 });
  }

  function referralRegistrationTime(value) {
    var date = new Date(value || '');
    return isNaN(date.getTime()) ? '' : date.toLocaleString();
  }

  function serviceCreditGrants(status) {
    if (!status) return [];
    if (Array.isArray(status.credit_grants) && status.credit_grants.length) return status.credit_grants;
    return Array.isArray(status.active_grants) ? status.active_grants : [];
  }

  function grantStatusKey(grant) {
    return String(grant && grant.status || '').trim().toLowerCase() || 'active';
  }

  function grantStatusRank(key, serviceActive) {
    var ranks = serviceActive
      ? { active: 0, period_limited: 1, queued: 2, exhausted: 3, expired: 4 }
      : { period_limited: 0, queued: 1, exhausted: 2, expired: 3, active: 4 };
    return Object.prototype.hasOwnProperty.call(ranks, key) ? ranks[key] : 5;
  }

  function grantStatusLabel(key) {
    return gt({
      active: 'grantStatusActive',
      period_limited: 'grantStatusPeriodLimited',
      queued: 'grantStatusQueued',
      exhausted: 'grantStatusExhausted',
      expired: 'grantStatusExpired'
    }[key] || 'grantStatusInactive');
  }

  function userGrantStatusSummary(item) {
    var status = item && item.service_status;
    var grants = serviceCreditGrants(status);
    if (!grants.length) return '';
    if (status && status.active) {
      var hasActiveGrant = grants.some(function(grant) { return grantStatusKey(grant) === 'active'; });
      if (hasActiveGrant) return '';
    }
    var selected = grants.slice().sort(function(a, b) {
      return grantStatusRank(grantStatusKey(a), !!(status && status.active)) - grantStatusRank(grantStatusKey(b), !!(status && status.active));
    })[0];
    var key = grantStatusKey(selected);
    if (key === 'active') return '';
    var detail = String(selected && selected.status_reason || '').trim();
    var retryAfterAt = String(selected && selected.retry_after_at || '').trim();
    if (retryAfterAt) detail = detail ? (detail + ' | ' + gt('grantRetryAfterAt').replace('{time}', retryAfterAt)) : gt('grantRetryAfterAt').replace('{time}', retryAfterAt);
    return grantStatusLabel(key) + (detail ? (' | ' + detail) : '');
  }

  function userCreditsSummary(item) {
    var status = item && item.service_status;
    var grants = serviceCreditGrants(status);
    var total = 0;
    var used = 0;
    grants.forEach(function(grant) {
      var isEffective = typeof grant.effective === 'boolean' ? grant.effective : (function() { var s = String(grant.status || '').toLowerCase(); return s !== 'queued' && s !== 'expired'; })();
      if (!isEffective) return;
      total += Number(grant.credits_total || 0);
      used += Number(grant.credits_used || 0);
    });
    var remaining = status && Number.isFinite(Number(status.credits_available)) ? Number(status.credits_available) : Math.max(0, total - used);
    return { total: total, used: used, remaining: remaining };
  }

  function renderCreditsSummary(item) {
    var credits = userCreditsSummary(item);
    var statusSummary = userGrantStatusSummary(item);
    return '<div style="padding:7px 8px;border-radius:8px;background:#f8fafc;border:1px solid rgba(31,34,48,.06)">'
      + '<div style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:6px">'
      + '<div style="min-width:0"><div class="item-meta" style="font-size:9px;line-height:1.1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(gt('creditsTotal')) + '</div><div class="mono" style="font-size:12px;font-weight:800;color:var(--ink);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(formatCreditsValue(credits.total)) + '</div></div>'
      + '<div style="min-width:0"><div class="item-meta" style="font-size:9px;line-height:1.1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(gt('creditsUsed')) + '</div><div class="mono" style="font-size:12px;font-weight:800;color:#ef5b70;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(formatCreditsValue(credits.used)) + '</div></div>'
      + '<div style="min-width:0"><div class="item-meta" style="font-size:9px;line-height:1.1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(gt('creditsRemaining')) + '</div><div class="mono" style="font-size:12px;font-weight:800;color:var(--ok);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(formatCreditsValue(credits.remaining)) + '</div></div>'
      + '</div>'
      + (statusSummary ? ('<div class="item-meta" style="margin-top:5px;color:#c05621;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="' + escapeHtml(statusSummary) + '">' + escapeHtml(statusSummary) + '</div>') : '')
      + '</div>';
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
    const allItems = Array.isArray(items) ? items : [];
    global._boundUsersAll = allItems;
    global._boundUsersPage = global._boundUsersPage || 1;
    global._boundUsersSearch = global._boundUsersSearch || '';
    global._boundUsersReferredOnly = !!global._boundUsersReferredOnly;
    if (!allItems.length) {
      root.innerHTML = hint(tr('emptyBoundUsers'));
      return;
    }
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
    const referredOnly = !!global._boundUsersReferredOnly;
    const filtered = (query || referredOnly) ? items.filter(function(item) {
      if (referredOnly && !item.referral) return false;
      if (!query) return true;
      return boundUserMatchesSearch(item, query);
    }) : items;
    const pageSize = 36;
    const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
    if (global._boundUsersPage > totalPages) global._boundUsersPage = totalPages;
    if (global._boundUsersPage < 1) global._boundUsersPage = 1;
    const start = (global._boundUsersPage - 1) * pageSize;
    const pageItems = filtered.slice(start, start + pageSize);
    const searchHtml = '<div style="margin-bottom:8px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">'
      + '<input id="boundUsersSearchInput" placeholder="' + gt('boundUsersSearchPlaceholder') + '" value="' + escapeHtml(global._boundUsersSearch || '') + '" style="max-width:260px;height:34px" oninput="window._boundUsersSearch=this.value;window._boundUsersPage=1;clearTimeout(window._busDeb);window._busDeb=setTimeout(_renderBoundUsersPage,200)">'
      + '<label class="toggle-label" style="font-size:11px;white-space:nowrap"><input type="checkbox" ' + (referredOnly ? 'checked' : '') + ' onchange="window._boundUsersReferredOnly=this.checked;window._boundUsersPage=1;_renderBoundUsersPage()"><span>' + escapeHtml(gt('referredOnly')) + '</span></label>'
      + '<button type="button" class="btn-secondary" id="syncVerifiedPhoneRoutesBtn" style="height:34px;font-size:12px;padding:0 12px" onclick="syncVerifiedPhoneRoutes(this)">' + escapeHtml(gt('syncPhoneRoutes')) + '</button>'
      + '</div>';
    var grouped = groupBoundUsers(pageItems);
    var gridStyle = 'display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px';
    var sections = ['regular', 'virtual'].map(function(type) {
      var list = grouped[type] || [];
      if (!list.length) return '';
      var cards = list.map(function(item) {
        var smartRoute = item.smart_route;
        var toggleId = 'sr_' + item.id;
        var serviceBadge = item.has_service_access ? '<span class="badge ok" title="' + escapeHtml(serviceAccessTooltip(item)) + '" style="padding:4px 8px;font-size:10px">' + escapeHtml(serviceAccessLabel()) + '</span>' : '<span class="badge info" style="padding:4px 8px;font-size:10px">-</span>';
        var typeBadge = item.is_virtual_employee
          ? '<span class="badge warn" style="padding:4px 8px;font-size:10px" title="' + escapeHtml(gt('virtualEmployeeHint')) + '">' + escapeHtml(gt('virtualEmployees')) + '</span>'
          : '<span class="badge info" style="padding:4px 8px;font-size:10px">' + escapeHtml(gt('regularUsers')) + '</span>';
        var actionLabel = item.is_virtual_employee ? gt('virtualUserDeleteLabel') : gt('unbindUser');
        var primaryPhone = boundUserPhones(item)[0] || '';
        var unbindBtn = '<button class="btn-danger" style="height:24px;font-size:10px;padding:0 8px" data-email="' + escapeHtml(String(item.email || '')) + '" data-tenant-id="' + escapeHtml(String(item.tenant_id || '')) + '" data-user-id="' + escapeHtml(String(item.id || '')) + '" data-phone="' + escapeHtml(String(primaryPhone || '')) + '" data-is-virtual="' + (item.is_virtual_employee ? 'true' : 'false') + '" onclick="unbindBoundUser(this.dataset.email, this.dataset.tenantId, this.dataset.userId, this.dataset.phone)">' + escapeHtml(actionLabel) + '</button>';
        var verifiedStar = item.email_verified ? '<span title="' + escapeHtml(gt('emailVerifiedTooltip')) + '" style="color:#f59e0b;font-size:13px;margin-left:4px;cursor:default">&#9733;</span>' : '';
        var referralTitle = item.referral ? gt('referredUserTooltip') + (item.referral.inviter_display_name ? ' | ' + item.referral.inviter_display_name + ' | ' + referralRegistrationTime(item.referral.registered_at) : '') : '';
        var referralBadge = item.referral ? '<span class="badge info" tabindex="0" title="' + escapeHtml(referralTitle) + '" style="padding:4px 8px;font-size:10px;background:#fdf2f8;color:#b23c68;border-color:#f5c7d8">' + escapeHtml(gt('referredUser')) + '</span>' : '';
        var displayName = boundUserDisplayName(item);
        return '<div class="item" style="padding:10px 12px;border-radius:12px;background:#fff;border:1px solid rgba(31,34,48,.06);box-shadow:none">'
          + '<div style="display:flex;flex-direction:column;gap:8px;min-width:0">'
          + '<div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0">' + escapeHtml(displayName) + verifiedStar + '</div>'
          + renderBoundUserContacts(item)
          + '<div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap;min-width:0">'
          + '<span class="badge info" style="padding:4px 8px;font-size:10px">' + escapeHtml(formatStatus(item.enrollment_status || item.status || 'active')) + '</span>'
          + typeBadge
          + referralBadge
          + serviceBadge
          + '<label class="toggle-label" title="' + gt('smartRouteLabel') + '" style="justify-content:flex-start;font-size:11px"><input type="checkbox" id="' + toggleId + '" ' + (smartRoute ? 'checked' : '') + ' data-user-id="' + escapeHtml(String(item.id || '')) + '" onchange="toggleSmartRoute(this.dataset.userId, this.checked)"><span>AI</span></label>'
          + unbindBtn
          + '</div>'
          + renderCreditsSummary(item)
          + '<div class="item-meta mono" style="font-size:10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted)">' + escapeHtml(item.sn || tr('na')) + '</div>'
          + '</div></div>';
      }).join('');
      return '<div style="grid-column:1 / -1;margin-top:4px"><div class="item-title" style="font-size:12px;margin-bottom:8px">' + escapeHtml(governanceUserTypeLabel(type)) + ' (' + list.length + ')</div><div class="user-grid-wrap" style="' + gridStyle + '">' + cards + '</div></div>';
    }).join('');
    var pagerHtml = '';
    var showCount = filtered.length > 0 ? (start + 1) + '-' + (start + pageItems.length) + ' / ' + filtered.length : '0 / 0';
    if (totalPages > 1 || query) {
      pagerHtml = '<div class="pager" style="margin-top:8px;display:flex;align-items:center;justify-content:center;gap:6px"><button class="btn-secondary" style="height:28px;font-size:11px;padding:0 10px" onclick="window._boundUsersPage=Math.max(1,window._boundUsersPage-1);_renderBoundUsersPage()" ' + (global._boundUsersPage <= 1 ? 'disabled' : '') + '>Prev</button><span style="font-size:11px">' + showCount + '</span><button class="btn-secondary" style="height:28px;font-size:11px;padding:0 10px" onclick="window._boundUsersPage=Math.min(' + totalPages + ',window._boundUsersPage+1);_renderBoundUsersPage()" ' + (global._boundUsersPage >= totalPages ? 'disabled' : '') + '>Next</button></div>';
    }
    root.innerHTML = searchHtml + (pageItems.length ? sections : '<div class="hint">' + gt('noMatches') + '</div>') + pagerHtml;
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

  global.unbindBoundUser = async function unbindBoundUser(email, tenantID, userID, phone) {
    email = String(email || '').trim();
    tenantID = String(tenantID || '').trim();
    userID = String(userID || '').trim();
    phone = String(phone || '').trim();
    if (!email && !userID && !phone) return;
    var matched = (global._boundUsersAll || []).find(function(item) {
      if (tenantID && String(item && item.tenant_id || '').trim() !== tenantID) return false;
      if (userID && String(item && item.id || '').trim() === userID) return true;
      if (email && String(item && item.email || '').trim().toLowerCase() === email.toLowerCase()) return true;
      if (phone) {
        var phoneDigits = normalizePhoneDigits(phone);
        return boundUserPhones(item).some(function(value) {
          return String(value || '').trim() === phone || (phoneDigits && normalizePhoneDigits(value) === phoneDigits);
        });
      }
      return false;
    }) || null;
    var isVirtualEmployee = !!(matched && matched.is_virtual_employee);
    var displayIdentity = boundUserDisplayName(matched || { email: email, phone: phone, phones: phone ? [phone] : [] }) || email || phone || userID;
    if (isVirtualEmployee) {
      var warning = gt('virtualUserDeleteWarning') + '\n\n' + displayIdentity;
      if (!confirm(warning)) return;
      var password = await promptGovernanceAdminPassword(gt('virtualUserDeleteLabel'), gt('virtualUserDeletePasswordPrompt') + '\n' + displayIdentity, gt('virtualUserDeleteLabel'));
      if (password === null) return;
      if (!password) {
        const emptyMsg = gt('virtualUserDeleteFailed').replace('{error}', 'admin_password is required');
        setOutput(emptyMsg);
        showToast(emptyMsg, 'error');
        return;
      }
      try {
        const data = await api('/api/admin/users/force-delete-virtual', {
          method: 'POST',
          body: JSON.stringify({ email: email, tenant_id: tenantID, admin_password: password })
        });
        const removedEmail = data.email || email;
        let msg = gt('virtualUserDeleteSuccess') + ': ' + removedEmail;
        if (data.route_delete_warning) msg += ' Route sync warning: ' + data.route_delete_warning;
        setOutput(msg);
        showToast(msg, 'success');
        await Promise.all([global.loadBoundUsers(), global.loadMachines()]);
      } catch (err) {
        const msg = gt('virtualUserDeleteFailed').replace('{error}', err.message);
        setOutput(msg);
        showToast(msg, 'error');
      }
      return;
    }
    if (!confirm(gt('unbindConfirm').replace('{email}', displayIdentity))) return;
    try {
      const params = new URLSearchParams();
      if (email) params.set('email', email);
      if (tenantID) params.set('tenant_id', tenantID);
      if (userID) params.set('user_id', userID);
      if (phone) params.set('phone', phone);
      const query = '?' + params.toString();
      const data = await api('/api/admin/users' + query, { method: 'DELETE' });
      const removedEmail = data.email || displayIdentity;
      let msg = gt('unbindSuccess').replace('{email}', removedEmail);
      if (data.route_delete_warning) msg += ' Route sync warning: ' + data.route_delete_warning;
      setOutput(msg);
      showToast(msg, 'success');
      await Promise.all([global.loadBoundUsers(), global.loadMachines()]);
    } catch (err) {
      const msg = gt('unbindFailed').replace('{error}', err.message);
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

  global.syncVerifiedPhoneRoutes = async function syncVerifiedPhoneRoutes(button) {
    const original = button ? button.textContent : '';
    try {
      if (button) {
        button.disabled = true;
        button.textContent = gt('syncPhoneRoutesBusy');
      }
      showToast(gt('syncPhoneRoutesRunning'), 'info');
      const data = await api('/api/admin/routing/sync-verified-phone-routes', { method: 'POST' });
      const msg = gt('syncPhoneRoutesDone').replace('{count}', String(data.synced_count || 0));
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = gt('syncPhoneRoutesFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    } finally {
      if (button) {
        button.disabled = false;
        button.textContent = original || gt('syncPhoneRoutes');
      }
    }
  };

  function applyGovernanceI18n() {
    var smartAllLabel = document.getElementById('smartRouteAllLabel');
    var smartAllLabelText = document.getElementById('smartRouteAllLabelText');
    if (smartAllLabel) smartAllLabel.title = gt('smartRouteAllLabel');
    if (smartAllLabelText) smartAllLabelText.textContent = gt('smartRouteAllLabel');
  }

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
      applyGovernanceI18n();
      if (global._boundUsersAll && typeof global._renderBoundUsersPage === 'function') global._renderBoundUsersPage();
      if (global._invitesAll && typeof global.renderInvites === 'function') global.renderInvites(global._invitesAll);
    });
  }

  applyGovernanceI18n();
})(window);
