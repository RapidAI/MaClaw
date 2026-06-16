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
      authStatusLoading: 'Checking Compute Auth',
      unlockHint: 'Ask HubCenter to grant compute access before adding external LLM providers.',
      storeContextMissing: 'Hub Center registration is missing. Register this Hub with HubCenter before purchasing compute.',
      activeAuthorizations: 'Active Compute',
      noActiveAuthorizations: 'No active compute authorization.',
      showInactiveAuthorizations: 'Show expired/invalid',
      hideInactiveAuthorizations: 'Hide expired/invalid',
      creditsRemaining: 'Remaining',
      creditsTotal: 'Total',
      expiresAt: 'Expires',
      serviceGroup: 'Service Group',
      officialComputeGrant: 'MaClaw Compute',
      externalProviderGrant: 'External Provider Permission',
      statusActive: 'Active',
      statusExpired: 'Expired',
      statusExhausted: 'Exhausted',
      statusInactive: 'Invalid',
    },
    zh: {
      purchaseCompute: '\u8d2d\u4e70\u7b97\u529b',
      officialProvider: 'MaClaw \u5b98\u65b9',
      officialGroup: 'MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4',
      lockedBadge: '\u5185\u7f6e',
      authStatusActive: '\u7b97\u529b\u5df2\u6388\u6743',
      authStatusInactive: '\u672a\u6388\u6743\u7b97\u529b\u6a21\u5757',
      authStatusLoading: '\u6b63\u5728\u68c0\u67e5\u7b97\u529b\u6388\u6743',
      unlockHint: '\u9700\u8981\u5148\u5728 HubCenter \u6388\u4e88\u79df\u6237\u7b97\u529b\uff0c\u624d\u80fd\u65b0\u5efa\u5916\u90e8 LLM \u670d\u52a1\u5546\u3002',
      storeContextMissing: '\u7f3a\u5c11 HubCenter \u6ce8\u518c\u4fe1\u606f\uff0c\u8bf7\u5148\u5c06\u6b64 Hub \u6ce8\u518c\u5230 HubCenter \u540e\u518d\u8d2d\u4e70\u7b97\u529b\u3002',
      activeAuthorizations: '\u5df2\u6fc0\u6d3b\u7b97\u529b',
      noActiveAuthorizations: '\u6682\u65e0\u6709\u6548\u7b97\u529b\u6388\u6743\u3002',
      showInactiveAuthorizations: '\u663e\u793a\u8fc7\u671f/\u5931\u6548',
      hideInactiveAuthorizations: '\u9690\u85cf\u8fc7\u671f/\u5931\u6548',
      creditsRemaining: '\u5269\u4f59',
      creditsTotal: '\u603b\u989d',
      expiresAt: '\u5230\u671f',
      serviceGroup: '\u670d\u52a1\u7ec4',
      officialComputeGrant: 'MaClaw \u7b97\u529b',
      externalProviderGrant: '\u5916\u90e8\u670d\u52a1\u5546\u6743\u9650',
      statusActive: '\u6709\u6548',
      statusExpired: '\u5df2\u8fc7\u671f',
      statusExhausted: '\u5df2\u7528\u5b8c',
      statusInactive: '\u5df2\u5931\u6548',
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

  async function loadComputeStatus() {
    var path = '/api/admin/llm/maclaw-compute-status';
    var tenantID = currentComputeTenantID();
    if (tenantID) {
      var params = new URLSearchParams();
      params.set('tenant_id', tenantID);
      path += '?' + params.toString();
    }
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

    // Fallback: auto-detect from legacy Hub admin context globals
    if (!hubID && window.hubConfigCache) hubID = window.hubConfigCache.hub_id || '';
    if ((!tenantID || tenantID === 'default') && profile && String(profile.scope || '').toLowerCase() === 'tenant' && profile.tenant_id) tenantID = profile.tenant_id;
    if (!tenantID && window.currentTenantID) tenantID = window.currentTenantID;
    if (!email && profile) email = profile.email || '';
    if (!hubCenterURL && window.hubConfigCache) hubCenterURL = window.hubConfigCache.center_base_url || '';
    if (!hubCenterURL) hubCenterURL = 'https://hubs.mypapers.top';
    hubCenterURL = String(hubCenterURL || '').replace(/\/+$/, '');

    if (!hubID) {
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
    if (email) params.set('email', email);
    var url = hubCenterURL + '/compute-store?' + params.toString();

    window.open(url, '_blank');
  };

  // ---------------------------------------------------------------------------
  // "添加服务商" gating
  // ---------------------------------------------------------------------------

  var _computeAuthStatus = null; // cached
  var _showInactiveComputeAuthorizations = false;

  window.checkComputeAuthorization = async function() {
    try {
      _computeAuthStatus = await loadComputeStatus();
    } catch (e) {
      _computeAuthStatus = { allow_external_providers: false };
    }
    updateComputeUI();
    return _computeAuthStatus;
  };

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

    // Update auth status badge if exists
    var badge = document.getElementById('maclawComputeAuthBadge');
    if (badge) {
      if (!_computeAuthStatus) {
        badge.className = 'badge info';
        badge.textContent = t('authStatusLoading');
      } else if (_computeAuthStatus.allow_external_providers) {
        badge.className = 'badge ok';
        badge.textContent = t('authStatusActive');
      } else {
        badge.className = 'badge warn';
        badge.textContent = t('authStatusInactive');
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

  function isAuthorizationActive(item) {
    if (!item) return false;
    if (typeof item.active === 'boolean') return item.active;
    var status = String(item.status || '').toLowerCase();
    if (status === 'expired' || status === 'exhausted' || status === 'inactive' || status === 'invalid') return false;
    var expiresAt = item.expires_at ? new Date(item.expires_at) : null;
    if (expiresAt && !Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) return false;
    return Number(item.credits_remaining || 0) > 0;
  }

  function formatComputeNumber(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n)) return '0';
    if (Math.abs(n) >= 1000) return Math.round(n).toLocaleString();
    if (Math.abs(n % 1) > 0.0001) return n.toFixed(2);
    return String(Math.round(n));
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

  function renderComputeAuthorizationList() {
    if (!_computeAuthStatus) return '';
    var all = computeAuthorizations();
    if (!all.length) {
      return '<div class="hint" style="margin-top:10px;padding:10px 12px;background:#fbfcfd;border-color:#e7eaf0">' + esc(t('noActiveAuthorizations')) + '</div>';
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
      var remaining = formatComputeNumber(typeof item.credits_remaining === 'number' ? item.credits_remaining : Math.max(0, Number(item.credits_total || 0) - Number(item.credits_used || 0)));
      return '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px;align-items:center;padding:9px 10px;border:1px solid #e7eaf0;border-radius:10px;background:#fff">'
        + '<div style="min-width:0"><div class="item-title" style="font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(authorizationTitle(item)) + '</div><div class="item-meta mono" style="font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(serviceGroupID || '-') + '</div></div>'
        + '<div style="min-width:0"><div class="item-meta" style="font-size:10px">' + esc(t('creditsRemaining')) + ' / ' + esc(t('creditsTotal')) + '</div><div class="mono" style="font-size:12px;font-weight:700;color:var(--text,var(--ink))">' + esc(remaining) + ' / ' + esc(total) + '</div></div>'
        + '<div style="min-width:0"><div class="item-meta" style="font-size:10px">' + esc(t('expiresAt')) + '</div><div class="item-meta" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(formatComputeDate(item.expires_at)) + '</div></div>'
        + '<span class="badge ' + badgeClass + '" style="justify-self:end">' + esc(authorizationStatusText(item)) + '</span>'
        + '</div>';
    }).join('');
    if (!rows) {
      rows = '<div class="hint" style="padding:10px 12px;background:#fbfcfd;border-color:#e7eaf0">' + esc(t('noActiveAuthorizations')) + '</div>';
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
      updateExternalProviderEntryVisibility();
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() { window.checkComputeAuthorization(); });
  } else {
    window.checkComputeAuthorization();
  }
  updateComputeUI();
})();
