/**
 * MaClaw Compute Module — Hub admin frontend extension.
 * Adds:
 * 1. "MaClaw 官方" provider recognition (locked, not editable)
 * 2. "购买算力" button on the MaClaw Official provider card
 * 3. Authorization status badge
 * 4. "添加服务商" button visibility/gating based on compute authorization
 *
 * ASCII-only source. Chinese text via \uXXXX or string literals.
 */
(function() {
  'use strict';

  var MACLAW_PROVIDER_ID = 'maclaw_official';
  var MACLAW_GROUP_ID = 'maclaw_official_group';

  var I18N = {
    en: {
      purchaseCompute: 'Purchase Compute',
      officialProvider: 'MaClaw Official',
      officialGroup: 'MaClaw Official Service Group',
      lockedBadge: 'Built-in',
      authStatusActive: 'Compute Active',
      authStatusInactive: 'No Compute Auth',
      authStatusError: 'Compute Auth Sync Failed',
      authStatusLoading: 'Checking Compute Auth',
      openingComputeStore: 'Opening compute store...',
      unlockHint: 'Ask HubCenter to grant compute access before adding external LLM providers.',
      storeContextMissing: 'Hub Center registration is missing. Register this Hub with HubCenter before purchasing compute.',
      activeAuthorizations: 'Active Compute',
      noActiveAuthorizations: 'No active compute authorization.',
      noComputeCredits: 'Compute module authorized. No active compute credits yet.',
      noAvailableCompute: 'No available compute',
      noAvailableComputeAction: 'Click to purchase',
      showInactiveAuthorizations: 'Show expired/invalid',
      hideInactiveAuthorizations: 'Hide expired/invalid',
      creditsRemaining: 'Remaining',
      creditsTotal: 'Total',
      creditsUsed: 'Used',
      expiresAt: 'Expires',
      serviceGroup: 'Service Group',
      officialComputeGrant: 'MaClaw Compute',
      externalProviderGrant: 'External Provider Permission',
      statusActive: 'Active',
      statusExpired: 'Expired',
      statusExhausted: 'Exhausted',
      statusInactive: 'Invalid',
      billingTimeOfUse: 'Time-of-use rates',
      billingDefaultRate: 'Default',
      billingCurrentRate: 'Now',
      billingEveryday: 'Every day',
      billingWeekdays: 'Weekdays',
      weekdaySun: 'Sun', weekdayMon: 'Mon', weekdayTue: 'Tue', weekdayWed: 'Wed',
      weekdayThu: 'Thu', weekdayFri: 'Fri', weekdaySat: 'Sat'
    },
    zh: {
      purchaseCompute: '\u8d2d\u4e70\u7b97\u529b',
      officialProvider: 'MaClaw \u5b98\u65b9',
      officialGroup: 'MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4',
      lockedBadge: '\u5185\u7f6e',
      authStatusActive: '\u7b97\u529b\u5df2\u6388\u6743',
      authStatusInactive: '\u672a\u6388\u6743\u7b97\u529b\u6a21\u5757',
      authStatusError: '\u7b97\u529b\u6388\u6743\u540c\u6b65\u5931\u8d25',
      authStatusLoading: '\u6b63\u5728\u68c0\u67e5\u7b97\u529b\u6388\u6743',
      openingComputeStore: '\u6b63\u5728\u6253\u5f00\u7b97\u529b\u5546\u5e97...',
      unlockHint: '\u9700\u8981\u5148\u5728 HubCenter \u6388\u4e88\u79df\u6237\u7b97\u529b\uff0c\u624d\u80fd\u65b0\u5efa\u5916\u90e8 LLM \u670d\u52a1\u5546\u3002',
      storeContextMissing: '\u7f3a\u5c11 HubCenter \u6ce8\u518c\u4fe1\u606f\uff0c\u8bf7\u5148\u5c06\u6b64 Hub \u6ce8\u518c\u5230 HubCenter \u540e\u518d\u8d2d\u4e70\u7b97\u529b\u3002',
      activeAuthorizations: '\u5df2\u6fc0\u6d3b\u7b97\u529b',
      noActiveAuthorizations: '\u6682\u65e0\u6709\u6548\u7b97\u529b\u6388\u6743\u3002',
      noComputeCredits: '\u7b97\u529b\u6a21\u5757\u5df2\u6388\u6743\uff0c\u6682\u65e0\u53ef\u7528\u7b97\u529b\u989d\u5ea6\u3002',
      noAvailableCompute: '\u65e0\u53ef\u7528\u7b97\u529b',
      noAvailableComputeAction: '\u70b9\u51fb\u8d2d\u4e70',
      showInactiveAuthorizations: '\u663e\u793a\u8fc7\u671f/\u5931\u6548',
      hideInactiveAuthorizations: '\u9690\u85cf\u8fc7\u671f/\u5931\u6548',
      creditsRemaining: '\u5269\u4f59',
      creditsTotal: '\u603b\u989d',
      creditsUsed: '\u5df2\u6d88\u8017',
      expiresAt: '\u5230\u671f',
      serviceGroup: '\u670d\u52a1\u7ec4',
      officialComputeGrant: 'MaClaw \u7b97\u529b',
      externalProviderGrant: '\u5916\u90e8\u670d\u52a1\u5546\u6743\u9650',
      statusActive: '\u6709\u6548',
      statusExpired: '\u5df2\u8fc7\u671f',
      statusExhausted: '\u5df2\u7528\u5b8c',
      statusInactive: '\u5df2\u5931\u6548',
      billingTimeOfUse: '\u5206\u65f6\u500d\u7387',
      billingDefaultRate: '\u9ed8\u8ba4',
      billingCurrentRate: '\u5f53\u524d',
      billingEveryday: '\u6bcf\u5929',
      billingWeekdays: '\u5de5\u4f5c\u65e5',
      weekdaySun: '\u65e5', weekdayMon: '\u4e00', weekdayTue: '\u4e8c', weekdayWed: '\u4e09',
      weekdayThu: '\u56db', weekdayFri: '\u4e94', weekdaySat: '\u516d'
    }
  };

  function t(key) {
    var lang = (window.currentLang || document.documentElement.lang || 'en').toLowerCase();
    var dict = lang.startsWith('zh') ? I18N.zh : I18N.en;
    return dict[key] || I18N.en[key] || key;
  }

  function currentAdminProfile() {
    if (typeof window.adminProfile === 'function') return window.adminProfile() || {};
    return window.adminProfile || {};
  }

  function currentComputeTenantID() {
    var profile = currentAdminProfile();
    if (profile && String(profile.scope || '').toLowerCase() === 'tenant' && profile.tenant_id) {
      return String(profile.tenant_id || '').trim();
    }
    if (window._currentTenantID) return String(window._currentTenantID || '').trim();
    if (typeof window.currentTenantID === 'function') return String(window.currentTenantID() || '').trim();
    if (window.currentTenantID) return String(window.currentTenantID || '').trim();
    return '';
  }

  function currentComputeTenantName(profile) {
    profile = profile || currentAdminProfile();
    return String((profile && (profile.tenant_name || profile.tenant_label || profile.tenant_slug)) || '').trim();
  }

  async function loadComputeStatus() {
    var path = '/api/admin/llm/maclaw-compute-status';
    var params = new URLSearchParams();
    params.set('refresh', '1');
    var tenantID = currentComputeTenantID();
    if (tenantID) {
      params.set('tenant_id', tenantID);
    }
    path += '?' + params.toString();
    if (typeof window.api === 'function') {
      return await window.api(path);
    }

    var headers = { 'Accept': 'application/json' };
    if (typeof window.token === 'function' && window.token()) {
      headers.Authorization = 'Bearer ' + window.token();
    }
    var resp = await fetch(path, { headers: headers });
    if (!resp.ok) throw new Error(resp.statusText || 'request failed');
    return await resp.json();
  }

  // ---------------------------------------------------------------------------
  // Provider/Group identification
  // ---------------------------------------------------------------------------

  window.isMaClawOfficialProvider = function(id) {
    return String(id || '').trim().toLowerCase() === MACLAW_PROVIDER_ID;
  };

  window.isMaClawOfficialServiceGroup = function(id) {
    return String(id || '').trim().toLowerCase() === MACLAW_GROUP_ID;
  };

  // Extend existing isBuiltinLLMServiceGroup if present
  var _origIsBuiltin = window.isBuiltinLLMServiceGroup;
  window.isBuiltinLLMServiceGroup = function(id) {
    if (window.isMaClawOfficialServiceGroup(id)) return true;
    if (_origIsBuiltin) return _origIsBuiltin(id);
    return String(id || '').trim().toLowerCase() === 'default';
  };

  // ---------------------------------------------------------------------------
  // "购买算力" button
  // ---------------------------------------------------------------------------

  window.openComputeStore = async function() {
    // Open a placeholder while this click still has browser user activation.
    // Waiting for the authorization request first causes popup blockers to
    // reject the first purchase attempt on most browsers.
    var storeWindow = window.open('about:blank', '_blank');
    if (storeWindow) {
      try {
        // The blank document is same-origin. Give slow authorization refreshes
        // an intentional state and prevent the destination from retaining an
        // opener reference after navigation.
        storeWindow.opener = null;
        storeWindow.document.title = t('openingComputeStore');
        storeWindow.document.body.textContent = t('openingComputeStore');
      } catch (e) {
        // The placeholder is still usable when a browser restricts its document.
      }
    }
    try {
      if (typeof window.checkComputeAuthorization === 'function') {
        await window.checkComputeAuthorization();
      }
    } catch (e) {
      // Keep legacy fallbacks below available when status refresh fails.
    }
    // Read values from the cached compute-status API response (populated by checkComputeAuthorization).
    var cached = _computeAuthStatus || {};
    var hubID = cached.hub_id || window._hubInstanceID || '';
    var tenantID = cached.tenant_id || currentComputeTenantID() || 'default';
    var email = cached.admin_email || '';
    var hubCenterURL = cached.center_base_url || window._hubCenterBaseURL || '';
    var profile = currentAdminProfile();
    var tenantName = cached.tenant_name || currentComputeTenantName(profile);
    var hubName = cached.hub_name || '';

    // Fallback: auto-detect from legacy Hub admin context globals
    if (!hubID && window.hubConfigCache) hubID = window.hubConfigCache.hub_id || '';
    if (!hubName && window.hubConfigCache) hubName = window.hubConfigCache.hub_name || window.hubConfigCache.name || window.hubConfigCache.display_name || '';
    if ((!tenantID || tenantID === 'default') && profile && String(profile.scope || '').toLowerCase() === 'tenant' && profile.tenant_id) tenantID = profile.tenant_id;
    if (!tenantID && window.currentTenantID) tenantID = window.currentTenantID;
    if (!tenantName) tenantName = currentComputeTenantName(profile);
    if (!email && profile) email = profile.email || '';
    if (!hubCenterURL && window.hubConfigCache) hubCenterURL = window.hubConfigCache.center_base_url || '';
    if (!hubCenterURL) hubCenterURL = 'https://hubs.mypapers.top';
    hubCenterURL = String(hubCenterURL || '').replace(/\/+$/, '');

    if (!hubID) {
      try {
        if (storeWindow && !storeWindow.closed) {
          storeWindow.close();
        }
      } catch (e) {
        // The placeholder is only a convenience; retain the registration hint.
      }
      if (window.showToast) {
        window.showToast(t('storeContextMissing'), 'warn');
      } else {
        alert(t('storeContextMissing'));
      }
      return;
    }

    var params = new URLSearchParams();
    params.set('hub_id', hubID);
    params.set('tenant_id', tenantID || 'default');
    if (hubName) params.set('hub_name', hubName);
    if (tenantName) params.set('tenant_name', tenantName);
    if (email) params.set('email', email);
    var url = hubCenterURL + '/compute-store?' + params.toString();

    try {
      if (storeWindow && !storeWindow.closed) {
        storeWindow.location.replace(url);
        return;
      }
    } catch (e) {
      // Continue in the current tab when the placeholder cannot navigate.
    }
    // If a browser blocks the placeholder tab, or it is closed while the
    // authorization refresh runs, still take the tenant to the store instead
    // of silently requiring a second click.
    window.location.assign(url);
  };

  // ---------------------------------------------------------------------------
  // "添加服务商" gating
  // ---------------------------------------------------------------------------

  var _computeAuthStatus = null; // cached
  var _showInactiveComputeAuthorizations = false;
  var _computeAuthCheckedAt = 0;
  var _computeAuthRefreshInFlight = null;

  window.checkComputeAuthorization = async function() {
    if (_computeAuthRefreshInFlight) return _computeAuthRefreshInFlight.then(function() {
      return _computeAuthStatus;
    });
    _computeAuthRefreshInFlight = (async function() {
      try {
        _computeAuthStatus = await loadComputeStatus();
        _computeAuthCheckedAt = Date.now();
      } catch (e) {
        _computeAuthStatus = { allow_external_providers: false, authorization_error: e && e.message || 'request failed' };
        _computeAuthCheckedAt = Date.now();
      }
      return _computeAuthStatus;
    })();
    try {
      await _computeAuthRefreshInFlight;
    } finally {
      _computeAuthRefreshInFlight = null;
    }
    // Full banner re-render with the freshly fetched data.
    // refreshMaClawOfficialBanner internally calls updateComputeUI.
    refreshMaClawOfficialBanner();
    // Also update gating in case the banner DOM doesn't exist yet (e.g. tab not visible).
    updateComputeUI();
    return _computeAuthStatus;
  };

  function refreshComputeAuthorizationIfStale(maxAgeMS) {
    if (_computeAuthRefreshInFlight) {
      return _computeAuthRefreshInFlight.then(function() {
        return _computeAuthStatus;
      });
    }
    var age = _computeAuthCheckedAt ? Date.now() - _computeAuthCheckedAt : Infinity;
    if (age <= maxAgeMS) return Promise.resolve(_computeAuthStatus);
    return window.checkComputeAuthorization();
  }

  window.canAddExternalProvider = function() {
    if (!_computeAuthStatus) return false;
    return !!_computeAuthStatus.allow_external_providers;
  };

  /**
   * Called by "添加服务商" button. If no compute auth, shows unlock hint instead.
   */
  window.gatedAddProvider = function(originalAction) {
    if (canAddExternalProvider()) {
      if (typeof originalAction === 'function') originalAction();
      return;
    }
    if (window.showToast) {
      window.showToast(t('unlockHint'), 'info');
    } else {
      alert(t('unlockHint'));
    }
  };

  // ---------------------------------------------------------------------------
  // UI rendering helpers
  // ---------------------------------------------------------------------------

  function updateComputeUI() {
    updateExternalProviderEntryVisibility();
    updateGlobalComputeAlert();

    // Update auth status badge if exists
    var badge = document.getElementById('maclawComputeAuthBadge');
    if (badge) {
      if (!_computeAuthStatus) {
        badge.className = 'badge info';
        badge.textContent = t('authStatusLoading');
      } else if (hasComputeModuleAuthorization()) {
        badge.className = 'badge ok';
        badge.textContent = t('authStatusActive');
      } else if (_computeAuthStatus.authorization_error) {
        badge.className = 'badge warn';
        badge.textContent = t('authStatusError');
        badge.title = String(_computeAuthStatus.authorization_error || '');
      } else {
        badge.className = 'badge warn';
        badge.textContent = t('authStatusInactive');
        badge.title = '';
      }
    }
  }

  function esc(value) {
    if (typeof window.escapeHtml === 'function') return window.escapeHtml(value);
    var m = {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'};
    return String(value == null ? '' : value).replace(/[&<>"']/g, function(c) { return m[c]; });
  }

  function computeAuthorizations() {
    if (!_computeAuthStatus || !Array.isArray(_computeAuthStatus.authorizations)) return [];
    return _computeAuthStatus.authorizations.slice();
  }

  function hasComputeModuleAuthorization() {
    return !!(_computeAuthStatus && _computeAuthStatus.allow_external_providers);
  }

  function isTenantAdminComputeContext() {
    var profile = currentAdminProfile();
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  function computeCreditsRemaining(item) {
    if (item && typeof item.credits_remaining === 'number') return Number(item.credits_remaining);
    return Math.max(0, Number(item && item.credits_total || 0) - Number(item && item.credits_used || 0));
  }

  function hasAvailableOfficialComputeCredits() {
    return computeAuthorizations().some(function(item) {
      if (String(item && item.service_group_id || '').trim() === '__external_compute_permission__') return false;
      if (!item) return false;
      if (typeof item.active === 'boolean' && !item.active) return false;
      var status = String(item.status || '').toLowerCase();
      if (status === 'expired' || status === 'exhausted' || status === 'inactive' || status === 'invalid') return false;
      var expiresAt = item.expires_at ? new Date(item.expires_at) : null;
      if (expiresAt && !Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) return false;
      var remaining = computeCreditsRemaining(item);
      return Number.isFinite(remaining) && remaining > 0;
    });
  }

  function shouldShowGlobalComputeAlert() {
    if (!_computeAuthStatus) return false;
    if (!isTenantAdminComputeContext()) return false;
    if (_computeAuthStatus.authorization_error) return false;
    return !hasComputeModuleAuthorization() && !hasAvailableOfficialComputeCredits();
  }

  function updateGlobalComputeAlert() {
    var alert = document.getElementById('maclawComputeTopAlert');
    if (!alert) return;
    if (!shouldShowGlobalComputeAlert()) {
      alert.classList.add('hidden');
      alert.innerHTML = '';
      return;
    }
    alert.classList.remove('hidden');
    alert.innerHTML = '<span>' + esc(t('noAvailableCompute')) + '</span>'
      + '<button type="button" onclick="openComputeStore()">' + esc(t('noAvailableComputeAction')) + '</button>';
  }

  function isAuthorizationActive(item) {
    if (!item) return false;
    if (typeof item.active === 'boolean') return item.active;
    var status = String(item.status || '').toLowerCase();
    if (status === 'expired' || status === 'exhausted' || status === 'inactive' || status === 'invalid') return false;
    var expiresAt = item.expires_at ? new Date(item.expires_at) : null;
    if (expiresAt && !Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) return false;
    return computeCreditsRemaining(item) > 0;
  }

  function formatComputeNumber(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n)) return '0';
    n = Math.round(n * 10000) / 10000;
    return n.toLocaleString(undefined, { maximumFractionDigits: 4 });
  }

  function formatComputeDate(value) {
    if (!value) return '-';
    var d = new Date(value);
    if (Number.isNaN(d.getTime())) return String(value);
    return d.toLocaleString();
  }

  function authorizationStatusText(item) {
    if (isAuthorizationActive(item)) return t('statusActive');
    var status = String(item && item.status || '').toLowerCase();
    if (status === 'expired') return t('statusExpired');
    if (status === 'exhausted') return t('statusExhausted');
    return t('statusInactive');
  }

  function authorizationTitle(item) {
    var serviceGroupID = String(item && item.service_group_id || '').trim();
    if (serviceGroupID === '__external_compute_permission__') return t('externalProviderGrant');
    return t('officialComputeGrant');
  }

  var officialBillingWeekdayKeys = ['weekdaySun', 'weekdayMon', 'weekdayTue', 'weekdayWed', 'weekdayThu', 'weekdayFri', 'weekdaySat'];

  function uniqueOfficialBillingDays(days) {
    var seen = {};
    var out = [];
    (days || []).forEach(function(day) {
      var n = Number(day);
      if (!Number.isFinite(n) || n < 0 || n > 6 || Math.round(n) !== n || seen[n]) return;
      seen[n] = true;
      out.push(n);
    });
    return out;
  }

  function formatOfficialBillingMultiplier(value) {
    var n = Number(value);
    if (!Number.isFinite(n) || n <= 0) n = 1;
    n = Math.round(n * 10000) / 10000;
    return '\u00d7' + String(n);
  }

  function formatOfficialBillingDays(days) {
    var uniq = uniqueOfficialBillingDays(days).slice().sort();
    if (!uniq.length || uniq.length === 7) return t('billingEveryday');
    if (uniq.length === 5 && uniq.join(',') === '1,2,3,4,5') return t('billingWeekdays');
    var keys = officialBillingWeekdayKeys;
    var lang = (window.currentLang || document.documentElement.lang || 'en').toLowerCase();
    var sep = lang.startsWith('zh') ? '\u3001' : ', ';
    return uniq.map(function(day) { return t(keys[day]); }).join(sep);
  }

  function officialBillingPolicies() {
    if (!_computeAuthStatus || !Array.isArray(_computeAuthStatus.provider_billing)) return [];
    return _computeAuthStatus.provider_billing.filter(function(item) {
      if (!item || item.paused) return false;
      var windows = item.credit_multiplier_schedule;
      if (Array.isArray(windows) && windows.length) return true;
      var n = Number(item.credit_multiplier);
      return Number.isFinite(n) && n > 0 && n !== 1;
    });
  }

  function renderOfficialBillingWindows(item) {
    var windows = Array.isArray(item && item.credit_multiplier_schedule) ? item.credit_multiplier_schedule : [];
    if (!windows.length) return '';
    return windows.map(function(window) {
      var start = String(window && window.start || '').trim() || '--:--';
      var end = String(window && window.end || '').trim() || '--:--';
      return '<div class="item-meta" style="font-size:11px;margin-top:4px">'
        + esc(formatOfficialBillingDays(window && window.days)) + ' '
        + esc(start) + '\u2013' + esc(end) + ' '
        + esc(formatOfficialBillingMultiplier(window && window.multiplier))
        + '</div>';
    }).join('');
  }

  function renderOfficialProviderBilling() {
    var policies = officialBillingPolicies();
    if (!policies.length) return '';
    var rows = policies.map(function(item) {
      var title = String(item.provider_id || '').trim() || t('officialProvider');
      var timezone = String(item.timezone || '').trim();
      var meta = esc(t('billingDefaultRate') + ' ' + formatOfficialBillingMultiplier(item.credit_multiplier));
      if (timezone) meta += ' \u00b7 ' + esc(timezone);
      return '<div style="padding:9px 10px;border:1px solid #e7eaf0;border-radius:10px;background:#fff">'
        + '<div style="display:flex;justify-content:space-between;gap:8px;align-items:center">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(title) + '</div>'
        + '<div class="item-meta" style="font-size:10px">' + meta + '</div></div>'
        + '<span class="badge info" style="justify-self:end;white-space:nowrap">' + esc(t('billingCurrentRate') + ' ' + formatOfficialBillingMultiplier(item.current_multiplier)) + '</span>'
        + '</div>'
        + renderOfficialBillingWindows(item)
        + '</div>';
    }).join('');
    return '<div style="margin-top:10px;display:grid;gap:8px">'
      + '<div class="item-meta" style="font-weight:700;color:var(--text,var(--ink))">' + esc(t('billingTimeOfUse')) + '</div>'
      + rows
      + '</div>';
  }

  function renderComputeAuthorizationList() {
    if (!_computeAuthStatus) return '';
    var all = computeAuthorizations();
    // Filter out pure permission records — they are not real credit purchases
    // and should not appear as "充值记录" to the user.
    all = all.filter(function(item) {
      return String(item && item.service_group_id || '').trim() !== '__external_compute_permission__';
    });
    if (!all.length) {
      if (_computeAuthStatus.authorization_error) {
        return '<div class="hint" style="margin-top:10px;padding:10px 12px;background:#fff8f0;border-color:#f2d3a6;color:#8a5b13">' + esc(t('authStatusError')) + ': ' + esc(_computeAuthStatus.authorization_error) + '</div>';
      }
      return '<div class="hint" style="margin-top:10px;padding:10px 12px;background:#fbfcfd;border-color:#e7eaf0">' + esc(hasComputeModuleAuthorization() ? t('noComputeCredits') : t('noActiveAuthorizations')) + '</div>';
    }
    var visible = all.filter(function(item) {
      return _showInactiveComputeAuthorizations || isAuthorizationActive(item);
    });
    var hiddenCount = all.length - visible.length;
    var toggle = hiddenCount > 0 || _showInactiveComputeAuthorizations
      ? '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 9px;border-radius:8px" onclick="toggleInactiveComputeAuthorizations()">'
        + esc(_showInactiveComputeAuthorizations ? t('hideInactiveAuthorizations') : t('showInactiveAuthorizations') + ' (' + hiddenCount + ')') + '</button>'
      : '';
    var rows = visible.map(function(item) {
      var active = isAuthorizationActive(item);
      var badgeClass = active ? 'ok' : 'warn';
      var serviceGroupID = String(item.service_group_id || '').trim();
      var total = formatComputeNumber(item.credits_total);
      var used = formatComputeNumber(item.credits_used);
      var remaining = formatComputeNumber(computeCreditsRemaining(item));
      return '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px;align-items:center;padding:9px 10px;border:1px solid #e7eaf0;border-radius:10px;background:#fff">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(authorizationTitle(item)) + '</div><div class="item-meta mono" style="font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(serviceGroupID || '-') + '</div></div>'
        + '<div style="min-width:0"><div class="item-meta" style="font-size:10px">' + esc(t('creditsRemaining')) + ' / ' + esc(t('creditsTotal')) + '</div><div class="mono" style="font-size:12px;font-weight:700;color:var(--text,var(--ink))">' + esc(remaining) + ' / ' + esc(total) + '</div></div>'
        + '<div style="min-width:0"><div class="item-meta" style="font-size:10px">' + esc(t('creditsUsed')) + '</div><div class="mono" style="font-size:12px;font-weight:700;color:var(--text,var(--ink))">' + esc(used) + '</div></div>'
        + '<div style="min-width:0"><div class="item-meta" style="font-size:10px">' + esc(t('expiresAt')) + '</div><div class="item-meta" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(formatComputeDate(item.expires_at)) + '</div></div>'
        + '<span class="badge ' + badgeClass + '" style="justify-self:end">' + esc(authorizationStatusText(item)) + '</span>'
        + '</div>';
    }).join('');
    if (!rows) {
      rows = '<div class="hint" style="padding:10px 12px;background:#fbfcfd;border-color:#e7eaf0">' + esc(hasComputeModuleAuthorization() ? t('noComputeCredits') : t('noActiveAuthorizations')) + '</div>';
    }
    return '<div style="margin-top:10px;display:grid;gap:8px">'
      + '<div style="display:flex;align-items:center;justify-content:space-between;gap:8px;flex-wrap:wrap"><div class="item-meta" style="font-weight:700;color:var(--text,var(--ink))">' + esc(t('activeAuthorizations')) + '</div>' + toggle + '</div>'
      + rows
      + '</div>';
  }

  function updateExternalProviderEntryVisibility() {
    var allow = window.canAddExternalProvider();
    ['llmProviderCreateBtn', 'llmProviderCreateInlineBtn', 'llmProvidersImportBtn'].forEach(function(id) {
      var btn = document.getElementById(id);
      if (!btn) return;
      btn.hidden = !allow;
      btn.style.display = allow ? '' : 'none';
      btn.disabled = !allow;
      btn.setAttribute('aria-hidden', allow ? 'false' : 'true');
      btn.title = allow ? '' : t('unlockHint');
    });
  }

  /**
   * Renders a "购买算力" button for use in the MaClaw Official provider card.
   * Returns HTML string.
   */
  window.renderMaClawPurchaseButton = function() {
    return '<button class="btn-primary" style="height:28px;font-size:12px;padding:0 12px;margin-left:8px" '
      + 'onclick="openComputeStore()" title="' + t('purchaseCompute') + '">'
      + t('purchaseCompute') + '</button>';
  };

  window.toggleInactiveComputeAuthorizations = function() {
    _showInactiveComputeAuthorizations = !_showInactiveComputeAuthorizations;
    refreshMaClawOfficialBanner();
  };

  /**
   * Renders the MaClaw Official provider info banner (for provider list).
   * Returns HTML string.
   */
  window.renderMaClawOfficialBanner = function() {
    return '<div class="item" style="background:#f8fbff;color:var(--text,var(--ink));border:1px solid #d9e7f7;border-radius:8px;padding:12px 16px;margin-bottom:8px;box-shadow:none">'
      + '<div style="display:flex;justify-content:space-between;align-items:center">'
      + '<div><strong>' + t('officialProvider') + '</strong>'
      + ' <span class="badge info" style="font-size:10px">' + t('lockedBadge') + '</span></div>'
      + '<div style="display:flex;align-items:center;gap:8px">'
      + '<span id="maclawComputeAuthBadge" class="badge warn">' + t('authStatusInactive') + '</span>'
      + window.renderMaClawPurchaseButton()
      + '</div></div>'
      + renderOfficialProviderBilling()
      + renderComputeAuthorizationList()
      + '</div>';
  };

  // ---------------------------------------------------------------------------
  // Init on load
  // ---------------------------------------------------------------------------

  function refreshMaClawOfficialBanner() {
    var banner = document.getElementById('maclawOfficialBanner');
    if (!banner) return;
    banner.innerHTML = window.renderMaClawOfficialBanner();
    updateComputeUI();
    refreshComputeAuthorizationIfStale(3000);
  }

  // Inject MaClaw Official banner at the top of the provider list after render
  function injectMaClawBannerToProviderList() {
    var list = document.getElementById('llmProviderList');
    if (!list) return;
    if (document.getElementById('maclawOfficialBanner')) {
      refreshMaClawOfficialBanner();
      return;
    }
    var banner = document.createElement('div');
    banner.id = 'maclawOfficialBanner';
    banner.innerHTML = window.renderMaClawOfficialBanner();
    list.parentElement.insertBefore(banner, list);
    updateComputeUI();
    refreshComputeAuthorizationIfStale(3000);
  }

  function observeLLMProviderListForBanner() {
    if (typeof MutationObserver !== 'function' || !document.body) return;
    var pending = false;
    var observer = new MutationObserver(function() {
      if (pending) return;
      pending = true;
      setTimeout(function() {
        pending = false;
        if (document.getElementById('llmProviderList') && !document.getElementById('maclawOfficialBanner')) {
          injectMaClawBannerToProviderList();
        }
      }, 0);
    });
    observer.observe(document.body, { childList: true, subtree: true });
  }

  // Monkey-patch renderLLMProviders to inject the banner after each render
  var _origRenderLLMProviders = window.renderLLMProviders;
  if (typeof _origRenderLLMProviders === 'function') {
    window.renderLLMProviders = function() {
      _origRenderLLMProviders.apply(this, arguments);
      injectMaClawBannerToProviderList();
      updateExternalProviderEntryVisibility();
    };
  }

  var _origAddLLMProvider = window.addLLMProvider;
  if (typeof _origAddLLMProvider === 'function') {
    window.addLLMProvider = function() {
      return window.gatedAddProvider(function() {
        return _origAddLLMProvider.apply(this, arguments);
      }.bind(this));
    };
  }

  var _origTriggerLLMProvidersImport = window.triggerLLMProvidersImport;
  if (typeof _origTriggerLLMProvidersImport === 'function') {
    window.triggerLLMProvidersImport = function() {
      return window.gatedAddProvider(function() {
        return _origTriggerLLMProvidersImport.apply(this, arguments);
      }.bind(this));
    };
  }

  var _origImportLLMProvidersJSON = window.importLLMProvidersJSON;
  if (typeof _origImportLLMProvidersJSON === 'function') {
    window.importLLMProvidersJSON = function() {
      return window.gatedAddProvider(function() {
        return _origImportLLMProvidersJSON.apply(this, arguments);
      }.bind(this));
    };
  }

  // Also hook into "Add Provider" button to gate it
  var _origOpenLLMProviderDialog = window.openLLMProviderDialog;
  if (typeof _origOpenLLMProviderDialog === 'function') {
    window.openLLMProviderDialog = function(mode) {
      if (mode === 'create') {
        window.gatedAddProvider(function() {
          _origOpenLLMProviderDialog(mode);
        });
        return;
      }
      _origOpenLLMProviderDialog(mode);
    };
  }

  if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
    window.AdminTabRegistry.onLanguageChange(function() {
      refreshMaClawOfficialBanner();
      updateComputeUI();
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      window.checkComputeAuthorization();
      injectMaClawBannerToProviderList();
      observeLLMProviderListForBanner();
    });
  } else {
    window.checkComputeAuthorization();
    injectMaClawBannerToProviderList();
    observeLLMProviderListForBanner();
  }
  updateComputeUI();
})();
