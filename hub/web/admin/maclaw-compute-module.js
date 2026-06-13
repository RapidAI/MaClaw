/**
 * MaClaw Compute Module — Hub admin frontend extension.
 * Adds:
 * 1. "MaClaw 官方" provider recognition (locked, not editable)
 * 2. "购买算力" button on the MaClaw Official provider card
 * 3. Authorization status badge
 * 4. "添加服务商" button gating (shows unlock hint when no compute authorization)
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
      unlockHint: 'Contact MaClaw to obtain compute module authorization to add custom LLM providers.',
      creditsRemaining: 'Credits remaining',
      expiresAt: 'Expires',
    },
    zh: {
      purchaseCompute: '\u8d2d\u4e70\u7b97\u529b',
      officialProvider: 'MaClaw \u5b98\u65b9',
      officialGroup: 'MaClaw \u5b98\u65b9\u670d\u52a1\u7ec4',
      lockedBadge: '\u5185\u7f6e',
      authStatusActive: '\u7b97\u529b\u5df2\u6388\u6743',
      authStatusInactive: '\u672a\u6388\u6743\u7b97\u529b\u6a21\u5757',
      unlockHint: '\u9700\u8981\u83b7\u5f97 MaClaw \u5b98\u65b9\u7b97\u529b\u6a21\u5757\u6388\u6743\u624d\u80fd\u6dfb\u52a0\u81ea\u5b9a\u4e49 LLM \u670d\u52a1\u3002\u8bf7\u8054\u7cfb MaClaw \u5b98\u65b9\u83b7\u53d6\u6388\u6743\u3002',
      creditsRemaining: '\u5269\u4f59\u989d\u5ea6',
      expiresAt: '\u6709\u6548\u671f\u81f3',
    }
  };

  function t(key) {
    var lang = (window.currentLang || document.documentElement.lang || 'en').toLowerCase();
    var dict = lang.startsWith('zh') ? I18N.zh : I18N.en;
    return dict[key] || I18N.en[key] || key;
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

  window.openComputeStore = function() {
    // Read values from the cached compute-status API response (populated by checkComputeAuthorization).
    var cached = _computeAuthStatus || {};
    var hubID = cached.hub_id || window._hubInstanceID || '';
    var tenantID = cached.tenant_id || window._currentTenantID || 'default';
    var email = cached.admin_email || '';
    var hubCenterURL = cached.center_base_url || window._hubCenterBaseURL || '';

    // Fallback: auto-detect from legacy Hub admin context globals
    if (!hubID && window.hubConfigCache) hubID = window.hubConfigCache.hub_id || '';
    if (!tenantID && window.currentTenantID) tenantID = window.currentTenantID;
    if (!email && window.adminProfile) email = window.adminProfile.email || '';
    if (!hubCenterURL && window.hubConfigCache) hubCenterURL = window.hubConfigCache.center_base_url || '';
    if (!hubCenterURL) hubCenterURL = 'https://hubs.mypapers.top';

    var url = hubCenterURL + '/compute-store?hub_id=' + encodeURIComponent(hubID)
      + '&tenant_id=' + encodeURIComponent(tenantID)
      + '&email=' + encodeURIComponent(email);

    window.open(url, '_blank');
  };

  // ---------------------------------------------------------------------------
  // "添加服务商" gating
  // ---------------------------------------------------------------------------

  var _computeAuthStatus = null; // cached

  window.checkComputeAuthorization = async function() {
    try {
      var resp = await fetch('/api/admin/llm/maclaw-compute-status');
      if (resp.ok) {
        _computeAuthStatus = await resp.json();
      } else {
        _computeAuthStatus = { allow_external_providers: false };
      }
    } catch (e) {
      _computeAuthStatus = { allow_external_providers: false };
    }
    updateComputeUI();
    return _computeAuthStatus;
  };

  window.canAddExternalProvider = function() {
    if (!_computeAuthStatus) return true; // not loaded yet, be permissive
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
    // Update auth status badge if exists
    var badge = document.getElementById('maclawComputeAuthBadge');
    if (badge) {
      if (_computeAuthStatus && _computeAuthStatus.allow_external_providers) {
        badge.className = 'badge ok';
        badge.textContent = t('authStatusActive');
      } else {
        badge.className = 'badge warn';
        badge.textContent = t('authStatusInactive');
      }
    }

    // Update credits display
    var creditsEl = document.getElementById('maclawComputeCredits');
    if (creditsEl && _computeAuthStatus) {
      var auths = _computeAuthStatus.authorizations || [];
      var totalRemaining = auths.reduce(function(sum, a) { return sum + (a.credits_remaining || 0); }, 0);
      creditsEl.textContent = t('creditsRemaining') + ': ' + Math.floor(totalRemaining).toLocaleString();
    }
  }

  /**
   * Renders a "购买算力" button for use in the MaClaw Official provider card.
   * Returns HTML string.
   */
  window.renderMaClawPurchaseButton = function() {
    return '<button class="btn-primary" style="height:28px;font-size:12px;padding:0 12px;margin-left:8px" '
      + 'onclick="openComputeStore()" title="' + t('purchaseCompute') + '">'
      + '\ud83d\ude80 ' + t('purchaseCompute') + '</button>';
  };

  /**
   * Renders the MaClaw Official provider info banner (for provider list).
   * Returns HTML string.
   */
  window.renderMaClawOfficialBanner = function() {
    return '<div class="item" style="background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);color:#fff;border-radius:8px;padding:12px 16px;margin-bottom:8px">'
      + '<div style="display:flex;justify-content:space-between;align-items:center">'
      + '<div><strong>' + t('officialProvider') + '</strong>'
      + ' <span class="badge" style="background:rgba(255,255,255,.2);color:#fff;font-size:10px">' + t('lockedBadge') + '</span>'
      + '<div style="font-size:12px;opacity:.8;margin-top:4px" id="maclawComputeCredits"></div></div>'
      + '<div style="display:flex;align-items:center;gap:8px">'
      + '<span id="maclawComputeAuthBadge" class="badge warn">' + t('authStatusInactive') + '</span>'
      + window.renderMaClawPurchaseButton()
      + '</div></div></div>';
  };

  // ---------------------------------------------------------------------------
  // Init on load
  // ---------------------------------------------------------------------------

  // Inject MaClaw Official banner at the top of the provider list after render
  function injectMaClawBannerToProviderList() {
    var list = document.getElementById('llmProviderList');
    if (!list) return;
    if (document.getElementById('maclawOfficialBanner')) return; // already present
    var banner = document.createElement('div');
    banner.id = 'maclawOfficialBanner';
    banner.innerHTML = window.renderMaClawOfficialBanner();
    list.parentElement.insertBefore(banner, list);
  }

  // Monkey-patch renderLLMProviders to inject the banner after each render
  var _origRenderLLMProviders = window.renderLLMProviders;
  if (typeof _origRenderLLMProviders === 'function') {
    window.renderLLMProviders = function() {
      _origRenderLLMProviders.apply(this, arguments);
      injectMaClawBannerToProviderList();
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

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() { window.checkComputeAuthorization(); });
  } else {
    window.checkComputeAuthorization();
  }
})();
