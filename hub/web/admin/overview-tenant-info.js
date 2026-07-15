/*
 * overview-tenant-info.js
 * Renders tenant information and authorization status on the Overview tab.
 * Visible to both global admins (shows current/default tenant) and tenant admins.
 * ASCII only - use \uXXXX for CJK characters.
 */
(function(global) {
  'use strict';

  var OTI18N = {
    en: {
      tenantInfoTitle: 'Tenant Information',
      authTitle: 'Authorization Status',
      tenantIDLabel: 'Tenant ID',
      tenantNameLabel: 'Tenant Name',
      tenantStatusLabel: 'Status',
      tenantDomainLabel: 'Email Domains',
      deAuthLabel: 'Digital Employee Authorization',
      computeAuthLabel: 'Compute Module Authorization',
      statusActive: 'Active',
      statusInactive: 'Inactive',
      statusDisabled: 'Disabled',
      statusExpired: 'Expired',
      statusNotSubscribed: 'Not Subscribed',
      deQuota: 'Authorized seats: {quota}',
      deExpires: 'Expires: {date}',
      deUsed: 'Used: {used} / {quota}',
      deReasonDisabled: 'Authorization disabled',
      deReasonQuotaZero: 'No seats allocated',
      deReasonExpired: 'Authorization expired',
      deReasonNotSubscribed: 'Not subscribed',
      computeCredits: 'Credits: {remaining} / {total}',
      computeExpires: 'Valid until {date}',
      computeCards: '{count} active authorization(s)',
      computeAllowExternal: 'External LLM providers allowed',
      computeNoExternal: 'External LLM providers restricted',
      notAvailable: 'Not available (verify Hub Center sync)',
      noDomains: 'None configured'
    },
    zh: {
      tenantInfoTitle: '\u79df\u6237\u4fe1\u606f',
      authTitle: '\u6388\u6743\u72b6\u6001',
      tenantIDLabel: '\u79df\u6237 ID',
      tenantNameLabel: '\u79df\u6237\u540d\u79f0',
      tenantStatusLabel: '\u72b6\u6001',
      tenantDomainLabel: '\u90ae\u7bb1\u57df\u540d',
      deAuthLabel: '\u6570\u5b57\u5458\u5de5\u6388\u6743',
      computeAuthLabel: '\u7b97\u529b\u6a21\u5757\u6388\u6743',
      statusActive: '\u5df2\u6fc0\u6d3b',
      statusInactive: '\u672a\u6fc0\u6d3b',
      statusDisabled: '\u5df2\u7981\u7528',
      statusExpired: '\u5df2\u8fc7\u671f',
      statusNotSubscribed: '\u672a\u8ba2\u9605',
      deQuota: '\u6388\u6743\u6570\uff1a{quota}',
      deExpires: '\u5230\u671f\u65f6\u95f4\uff1a{date}',
      deUsed: '\u5df2\u4f7f\u7528\uff1a{used} / {quota}',
      deReasonDisabled: '\u6388\u6743\u5df2\u7981\u7528',
      deReasonQuotaZero: '\u672a\u5206\u914d\u5e2d\u4f4d',
      deReasonExpired: '\u6388\u6743\u5df2\u8fc7\u671f',
      deReasonNotSubscribed: '\u672a\u8ba2\u9605',
      computeCredits: '\u7b97\u529b\u4f59\u989d\uff1a{remaining} / {total}',
      computeExpires: '\u6709\u6548\u671f\u81f3 {date}',
      computeCards: '{count} \u4e2a\u6709\u6548\u6388\u6743',
      computeAllowExternal: '\u5141\u8bb8\u6dfb\u52a0\u7b2c\u4e09\u65b9 LLM \u670d\u52a1',
      computeNoExternal: '\u26d4 \u4ec5\u9650 MaClaw \u5b98\u65b9\u7b97\u529b',
      notAvailable: '\u6682\u65e0\u6570\u636e\uff08\u8bf7\u786e\u8ba4\u8282\u70b9\u4e2d\u5fc3\u5df2\u540c\u6b65\u6388\u6743\uff09',
      noDomains: '\u672a\u914d\u7f6e'
    }
  };

  function ott(key, params) {
    var lang = global.currentLang === 'zh' ? 'zh' : 'en';
    var text = (OTI18N[lang] && OTI18N[lang][key]) || (OTI18N.en && OTI18N.en[key]) || key;
    if (params) {
      Object.keys(params).forEach(function(k) {
        text = text.replace(new RegExp('\\{' + k + '\\}', 'g'), String(params[k]));
      });
    }
    return text;
  }

  function byID(id) { return document.getElementById(id); }
  function esc(s) { return typeof global.escapeHtml === 'function' ? global.escapeHtml(String(s || '')) : String(s || ''); }

  function formatDate(isoStr) {
    if (!isoStr) return '-';
    var d = new Date(isoStr);
    if (isNaN(d.getTime())) return isoStr;
    return d.toLocaleDateString();
  }

  function formatNumber(n) {
    if (n === undefined || n === null) return '0';
    return String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  }
  function normalizeComputeCredits(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n)) return 0;
    return Math.round(n * 10000) / 10000;
  }
  function formatComputeCredits(value) {
    return normalizeComputeCredits(value).toLocaleString(undefined, { maximumFractionDigits: 4 });
  }
  function sumComputeCredits(items, key) {
    return normalizeComputeCredits((items || []).reduce(function(sum, item) {
      return sum + Number(item && item[key] || 0);
    }, 0));
  }

  function computeCardIsActive(item) {
    if (!item) return false;
    if (typeof item.active === 'boolean') return item.active;
    var status = String(item.status || '').toLowerCase();
    if (status === 'expired' || status === 'exhausted' || status === 'inactive' || status === 'invalid') return false;
    var expiresAt = item.expires_at ? new Date(item.expires_at) : null;
    if (expiresAt && !isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) return false;
    return Number(item.credits_remaining || 0) > 0;
  }

  function isTenantAdminScope() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  // Resolves which tenant ID to display on the overview.
  // Tenant admins: their own tenant.
  // Global admins: the currently selected tenant context, or from profile, or 'tenant_default'.
  function resolveOverviewTenantID() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    if (!profile) return '';
    if (isTenantAdminScope()) return profile.tenant_id || '';
    // Global admin: check if a tenant filter is active (from tenant-tab selection)
    if (typeof global.getActiveTenantFilter === 'function') {
      var filtered = global.getActiveTenantFilter();
      if (filtered) return filtered;
    }
    // Use profile's tenant_id if available, otherwise the canonical default
    return profile.tenant_id || 'tenant_default';
  }

  // Renders the tenant info panel on the overview tab.
  function renderOverviewTenantInfo(tenantData, centerData, computeData) {
    var panel = byID('overviewTenantInfoPanel');
    if (!panel) return;

    // Hide panel if no tenant data at all
    if (!tenantData && !centerData && !computeData) {
      panel.classList.add('hidden');
      return;
    }

    panel.classList.remove('hidden');
    var tenantID = resolveOverviewTenantID();

    // Labels (re-set on every render to support language switching)
    var titleEl = byID('overviewTenantInfoTitle');
    if (titleEl) titleEl.textContent = ott('tenantInfoTitle');
    var authTitleEl = byID('overviewAuthTitle');
    if (authTitleEl) authTitleEl.textContent = ott('authTitle');
    var idLabel = byID('overviewTenantIDLabel');
    if (idLabel) idLabel.textContent = ott('tenantIDLabel');
    var nameLabel = byID('overviewTenantNameLabel');
    if (nameLabel) nameLabel.textContent = ott('tenantNameLabel');
    var statusLabel = byID('overviewTenantStatusLabel');
    if (statusLabel) statusLabel.textContent = ott('tenantStatusLabel');
    var domainLabel = byID('overviewTenantDomainLabel');
    if (domainLabel) domainLabel.textContent = ott('tenantDomainLabel');
    var deLabel = byID('overviewDEAuthLabel');
    if (deLabel) deLabel.textContent = ott('deAuthLabel');
    var computeLabel = byID('overviewComputeAuthLabel');
    if (computeLabel) computeLabel.textContent = ott('computeAuthLabel');

    // Tenant basic info
    var tenant = tenantData || {};
    var idVal = byID('overviewTenantIDValue');
    if (idVal) idVal.textContent = tenant.id || '-';
    var nameVal = byID('overviewTenantNameValue');
    if (nameVal) nameVal.textContent = tenant.name || tenant.slug || '-';
    var statusVal = byID('overviewTenantStatusValue');
    if (statusVal) {
      var st = String(tenant.status || 'active').toLowerCase();
      var statusText = st === 'active' ? ott('statusActive') : st === 'disabled' ? ott('statusDisabled') : esc(tenant.status || '-');
      var color = st === 'active' ? '#27ae60' : '#e74c3c';
      statusVal.innerHTML = '<span style="color:' + color + ';font-weight:500">\u25cf ' + esc(statusText) + '</span>';
    }
    var domainVal = byID('overviewTenantDomainValue');
    if (domainVal) {
      var domains = tenant.domains || [];
      domainVal.textContent = domains.length > 0 ? domains.join(', ') : ott('noDomains');
    }

    // Digital Employee Authorization
    // Sources (priority order):
    // 1. centerData.digital_employee_authorization (single, filtered by backend for this tenant)
    // 2. centerData.digital_employee_authorizations[tenantID] (map, available for global admins)
    // 3. tenant.digital_employee_authorization (from tenant detail API with enrichment)
    var deStatus = byID('overviewDEAuthStatus');
    var deDetail = byID('overviewDEAuthDetail');
    var deAuth = null;
    if (centerData) {
      deAuth = centerData.digital_employee_authorization || null;
      if (!deAuth && centerData.digital_employee_authorizations) {
        // Try multiple key variants: the backend normalizes tenant IDs but
        // HubCenter might use different formats in the plural map.
        var mapKeys = [tenantID];
        if (tenantID === 'tenant_default') mapKeys.push('default');
        else if (tenantID === 'default') mapKeys.push('tenant_default');
        else mapKeys.push('tenant_' + tenantID, tenantID.replace(/^tenant_/, ''));
        for (var ki = 0; ki < mapKeys.length && !deAuth; ki++) {
          deAuth = centerData.digital_employee_authorizations[mapKeys[ki]] || null;
        }
      }
    }
    if (!deAuth && tenant.digital_employee_authorization) {
      deAuth = tenant.digital_employee_authorization;
    }
    if (deStatus && deDetail) {
      if (!deAuth) {
        deStatus.innerHTML = '<span style="color:var(--muted)">' + esc(ott('notAvailable')) + '</span>';
        deDetail.textContent = '';
      } else if (deAuth.active) {
        deStatus.innerHTML = '<span style="color:#27ae60;font-weight:500">\u25cf ' + esc(ott('statusActive')) + '</span>';
        var details = [];
        if (deAuth.quota) details.push(ott('deQuota', { quota: formatNumber(deAuth.quota) }));
        if (deAuth.used !== undefined && deAuth.quota) details.push(ott('deUsed', { used: formatNumber(deAuth.used || 0), quota: formatNumber(deAuth.quota) }));
        if (deAuth.expires_at) details.push(ott('deExpires', { date: formatDate(deAuth.expires_at) }));
        deDetail.textContent = details.join(' | ');
      } else {
        var reason = deAuth.reason || '';
        var reasonText = reason === 'disabled' ? ott('deReasonDisabled') : reason === 'quota_zero' ? ott('deReasonQuotaZero') : reason === 'expired' ? ott('deReasonExpired') : reason === 'not_subscribed' ? ott('deReasonNotSubscribed') : esc(reason || ott('statusInactive'));
        deStatus.innerHTML = '<span style="color:#e74c3c;font-weight:500">\u25cf ' + esc(ott('statusInactive')) + '</span>';
        deDetail.textContent = reasonText;
      }
    }

    // Compute Module Authorization
    var computeStatus = byID('overviewComputeAuthStatus');
    var computeDetail = byID('overviewComputeAuthDetail');
    var compute = computeData || (tenant.compute_authorization) || null;
    if (computeStatus && computeDetail) {
      if (!compute) {
        computeStatus.innerHTML = '<span style="color:var(--muted)">' + esc(ott('notAvailable')) + '</span>';
        computeDetail.textContent = '';
      } else if (compute.active) {
        computeStatus.innerHTML = '<span style="color:#27ae60;font-weight:500">\u25cf ' + esc(ott('statusActive')) + '</span>';
        var cDetails = [];
        if (compute.total_credits !== undefined) {
          cDetails.push(ott('computeCredits', { remaining: formatComputeCredits(compute.remaining_credits), total: formatComputeCredits(compute.total_credits) }));
        }
        if (compute.authorization_count) cDetails.push(ott('computeCards', { count: compute.authorization_count }));
        if (compute.expires_at) cDetails.push(ott('computeExpires', { date: formatDate(compute.expires_at) }));
        var extLine = compute.allow_external ? ott('computeAllowExternal') : ott('computeNoExternal');
        if (cDetails.length > 0) {
          computeDetail.textContent = cDetails.join(' | ') + '\n' + extLine;
        } else {
          computeDetail.textContent = extLine;
        }
      } else {
        // No active credits but the authorization status itself may still be valid
        // (e.g. allow_external is set even without credit purchases)
        var computeStatusText = ott('statusInactive');
        var computeStatusColor = '#e74c3c';
        if (compute.allow_external) {
          computeStatusText = ott('statusActive');
          computeStatusColor = '#27ae60';
        }
        computeStatus.innerHTML = '<span style="color:' + computeStatusColor + ';font-weight:500">\u25cf ' + esc(computeStatusText) + '</span>';
        var fallbackDetail = '';
        if (compute.allow_external !== undefined) {
          fallbackDetail = compute.allow_external ? ott('computeAllowExternal') : ott('computeNoExternal');
        }
        if (compute.error) {
          fallbackDetail = (fallbackDetail ? fallbackDetail + '\n' : '') + esc(compute.error);
        }
        computeDetail.textContent = fallbackDetail;
      }
    }
  }

  // Fetches tenant info + authorization data and renders the overview panel.
  // API calls run in parallel for faster rendering.
  async function loadOverviewTenantInfo() {
    var panel = byID('overviewTenantInfoPanel');
    if (!panel) return;

    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    if (!profile) {
      panel.classList.add('hidden');
      return;
    }

    var tenantID = resolveOverviewTenantID();
    if (!tenantID) {
      panel.classList.add('hidden');
      return;
    }

    var tenantAdmin = isTenantAdminScope();

    // Fetch tenant detail and compute status in parallel.
    // Use ?refresh=1 for compute status to trigger a direct QueryAuthorization
    // call to HubCenter, ensuring we get the latest AllowExternalProviders
    // value rather than relying on heartbeat-cached data which may be stale
    // or never populated (if HubCenter heartbeat response omits authorization fields).
    var isDefaultTenant = (tenantID === 'tenant_default' || tenantID === 'default');
    var computeURL = '/api/admin/llm/maclaw-compute-status?refresh=1';
    if (!tenantAdmin && !isDefaultTenant) {
      computeURL += '&tenant_id=' + encodeURIComponent(tenantID);
    }

    var results = await Promise.allSettled([
      global.api('/api/admin/tenants/' + encodeURIComponent(tenantID)),
      global.api('/api/admin/center/status'),
      global.api(computeURL)
    ]);

    var tenantData = results[0].status === 'fulfilled' && results[0].value ? (results[0].value.tenant || null) : null;
    var centerData = results[1].status === 'fulfilled' ? results[1].value : null;
    var computeRaw = results[2].status === 'fulfilled' ? results[2].value : null;

    // Normalize compute data into a summary
    var computeData = null;
    if (computeRaw) {
      var computeAuthorizations = Array.isArray(computeRaw.authorizations) ? computeRaw.authorizations : [];
      var computeCards = computeAuthorizations.filter(function(a) {
        return String(a && a.service_group_id || '').trim() !== '__external_compute_permission__';
      });
      var activeComputeCards = computeCards.filter(computeCardIsActive);
      computeData = {
        active: !!computeRaw.allow_external_providers,
        total_credits: sumComputeCredits(activeComputeCards, 'credits_total'),
        used_credits: sumComputeCredits(activeComputeCards, 'credits_used'),
        remaining_credits: sumComputeCredits(activeComputeCards, 'credits_remaining'),
        authorization_count: activeComputeCards.length,
        expires_at: activeComputeCards.reduce(function(l, a) { return a.expires_at > l ? a.expires_at : l; }, ''),
        allow_external: !!computeRaw.allow_external_providers,
        error: computeRaw.authorization_error || ''
      };
    }

    renderOverviewTenantInfo(tenantData, centerData, computeData);
    loadOverviewSystemFreeStatus();
  }

  function systemFreeI18n(key) {
    var zh = global.currentLang === 'zh';
    var map = zh ? {
      title: 'system-free \u7cfb\u7edf\u514d\u8d39 LLM',
      desc: 'Hub / MaClawSrv \u670d\u52a1\u7aef Agent \u9ed8\u8ba4\u4f7f\u7528\u6b64\u7ec4\uff08\u514d\u5145\u503c\uff0c\u4e0d\u53ef\u5220\uff09',
      ready: '\u5df2\u5c31\u7eea',
      notReady: '\u672a\u5c31\u7eea \u2014 \u8bf7\u914d\u7f6e\u670d\u52a1\u5546\u5e76\u6d4b\u8bd5',
      test: '\u6d4b\u8bd5 system-free',
      config: '\u524d\u5f80\u6a21\u578b\u670d\u52a1',
      providers: '\u670d\u52a1\u5546: '
    } : {
      title: 'system-free (server LLM)',
      desc: 'Default free group for Hub/MaClawSrv server-side agents (no recharge, not deletable)',
      ready: 'Ready',
      notReady: 'Not ready - configure providers and test',
      test: 'Test system-free',
      config: 'Open Model Services',
      providers: 'Providers: '
    };
    return map[key] || key;
  }

  async function loadOverviewSystemFreeStatus() {
    var panel = byID('overviewSystemFreePanel');
    if (!panel) return;
    // Only meaningful for tenant-scoped admins / after login with tenant context.
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    if (!profile) {
      panel.classList.add('hidden');
      return;
    }
    panel.classList.remove('hidden');
    var title = byID('overviewSystemFreeTitle');
    var desc = byID('overviewSystemFreeDesc');
    var badge = byID('overviewSystemFreeBadge');
    var detail = byID('overviewSystemFreeDetail');
    var testBtn = byID('overviewSystemFreeTestBtn');
    var configBtn = byID('overviewSystemFreeConfigBtn');
    if (title) title.textContent = systemFreeI18n('title');
    if (desc) desc.textContent = systemFreeI18n('desc');
    if (testBtn) testBtn.textContent = systemFreeI18n('test');
    if (configBtn) configBtn.textContent = systemFreeI18n('config');
    try {
      var st = await global.api('/api/admin/llm/system-free');
      global.tenantSystemFreeStatusCache = st || {};
      var ready = !!(st && st.ready);
      if (badge) {
        badge.textContent = ready ? systemFreeI18n('ready') : systemFreeI18n('notReady');
        badge.style.color = ready ? '#1f7a3f' : '#b42318';
      }
      var ids = (st && st.provider_ids || []).join(', ') || '-';
      var reasons = (st && st.reasons || []).join(', ');
      if (detail) {
        detail.textContent = systemFreeI18n('providers') + ids + (reasons ? ' | ' + reasons : '');
      }
      // Soft gate: keep panel visible and highlight when not ready.
      panel.style.borderColor = ready ? '' : 'rgba(180,35,24,.35)';
      panel.style.background = ready ? '' : 'rgba(180,35,24,.04)';
    } catch (err) {
      if (badge) {
        badge.textContent = systemFreeI18n('notReady');
        badge.style.color = '#b42318';
      }
      if (detail) detail.textContent = String(err && err.message || err || 'load failed');
    }
  }

  global.loadOverviewTenantInfo = loadOverviewTenantInfo;
  global.renderOverviewTenantInfo = renderOverviewTenantInfo;
  global.loadOverviewSystemFreeStatus = loadOverviewSystemFreeStatus;

})(window);
