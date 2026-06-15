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

  /**
   * Renders the MaClaw Official provider info banner (for provider list).
   * Returns HTML string.
   */
  window.renderMaClawOfficialBanner = function() {
    return '<div class="item" style="background:#f8fbff;color:var(--ink);border:1px solid #d9e7f7;border-radius:8px;padding:12px 16px;margin-bottom:8px;box-shadow:none">'
      + '<div style="display:flex;justify-content:space-between;align-items:center">'
      + '<div><strong>' + t('officialProvider') + '</strong>'
      + ' <span class="badge info" style="font-size:10px">' + t('lockedBadge') + '</span></div>'
      + '<div style="display:flex;align-items:center;gap:8px">'
      + '<span id="maclawComputeAuthBadge" class="badge warn">' + t('authStatusInactive') + '</span>'
      + window.renderMaClawPurchaseButton()
      + '</div></div></div>';
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
