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
      deQuota: 'Quota: {quota} seats',
      deExpires: 'Expires: {date}',
      deUsed: 'Used: {used} / {quota}',
      deReasonDisabled: 'Authorization disabled',
      deReasonQuotaZero: 'No seats allocated',
      deReasonExpired: 'Authorization expired',
      deReasonNotSubscribed: 'Not subscribed',
      computeCredits: 'Credits: {remaining} / {total} remaining',
      computeExpires: 'Expires: {date}',
      computeCards: '{count} active authorization(s)',
      computeAllowExternal: 'External LLM providers allowed',
      computeNoExternal: 'External LLM providers restricted',
      notAvailable: 'Not available',
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
      deQuota: '\u914d\u989d\uff1a{quota} \u5e2d',
      deExpires: '\u6709\u6548\u671f\u81f3\uff1a{date}',
      deUsed: '\u5df2\u4f7f\u7528\uff1a{used} / {quota}',
      deReasonDisabled: '\u6388\u6743\u5df2\u7981\u7528',
      deReasonQuotaZero: '\u672a\u5206\u914d\u5e2d\u4f4d',
      deReasonExpired: '\u6388\u6743\u5df2\u8fc7\u671f',
      deReasonNotSubscribed: '\u672a\u8ba2\u9605',
      computeCredits: '\u7b97\u529b\uff1a\u5269\u4f59 {remaining} / \u603b\u8ba1 {total}',
      computeExpires: '\u6709\u6548\u671f\u81f3\uff1a{date}',
      computeCards: '{count} \u4e2a\u6709\u6548\u6388\u6743',
      computeAllowExternal: '\u5141\u8bb8\u6dfb\u52a0\u7b2c\u4e09\u65b9 LLM \u670d\u52a1',
      computeNoExternal: '\u4ec5\u9650 MaClaw \u5b98\u65b9\u7b97\u529b',
      notAvailable: '\u6682\u65e0\u6570\u636e',
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

  function isTenantAdminScope() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  // Resolves which tenant ID to display on the overview.
  // Tenant admins: their own tenant.
  // Global admins: the currently selected tenant context (from tenant chip), or 'default'.
  function resolveOverviewTenantID() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    if (!profile) return '';
    if (isTenantAdminScope()) return profile.tenant_id || '';
    // Global admin: check if a tenant filter is active (from tenant-tab selection)
    if (typeof global.getActiveTenantFilter === 'function') {
      var filtered = global.getActiveTenantFilter();
      if (filtered) return filtered;
    }
    return 'default';
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
    var deStatus = byID('overviewDEAuthStatus');
    var deDetail = byID('overviewDEAuthDetail');
    var deAuth = (centerData && centerData.digital_employee_authorization) || (tenant.digital_employee_authorization) || null;
    if (deStatus && deDetail) {
      if (!deAuth) {
        deStatus.innerHTML = '<span style="color:var(--muted)">' + esc(ott('notAvailable')) + '</span>';
        deDetail.textContent = '';
      } else if (deAuth.active) {
        deStatus.innerHTML = '<span style="color:#27ae60;font-weight:500">\u25cf ' + esc(ott('statusActive')) + '</span>';
        var details = [];
        if (deAuth.quota) details.push(ott('deQuota', { quota: deAuth.quota }));
        if (deAuth.used !== undefined && deAuth.quota) details.push(ott('deUsed', { used: deAuth.used || 0, quota: deAuth.quota }));
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
          cDetails.push(ott('computeCredits', { remaining: Math.round(compute.remaining_credits || 0), total: Math.round(compute.total_credits || 0) }));
        }
        if (compute.authorization_count) cDetails.push(ott('computeCards', { count: compute.authorization_count }));
        if (compute.expires_at) cDetails.push(ott('computeExpires', { date: formatDate(compute.expires_at) }));
        if (compute.allow_external) cDetails.push(ott('computeAllowExternal'));
        else cDetails.push(ott('computeNoExternal'));
        computeDetail.textContent = cDetails.join(' | ');
      } else {
        computeStatus.innerHTML = '<span style="color:#e74c3c;font-weight:500">\u25cf ' + esc(ott('statusInactive')) + '</span>';
        var fallbackDetail = '';
        if (compute.allow_external !== undefined) {
          fallbackDetail = compute.allow_external ? ott('computeAllowExternal') : ott('computeNoExternal');
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

    // Fetch all three sources in parallel
    var computeURL = '/api/admin/llm/maclaw-compute-status';
    if (!tenantAdmin && tenantID !== 'default') {
      computeURL += '?tenant_id=' + encodeURIComponent(tenantID);
    }
    var results = await Promise.allSettled([
      global.api('/api/admin/tenants/' + encodeURIComponent(tenantID)),
      global.api('/api/admin/center/status' + (!tenantAdmin ? '?tenant_id=' + encodeURIComponent(tenantID) : '')),
      global.api(computeURL)
    ]);

    var tenantData = results[0].status === 'fulfilled' && results[0].value ? (results[0].value.tenant || null) : null;
    var centerData = results[1].status === 'fulfilled' ? results[1].value : null;
    var computeRaw = results[2].status === 'fulfilled' ? results[2].value : null;

    // Normalize compute data into a summary
    var computeData = null;
    if (computeRaw && computeRaw.authorizations && computeRaw.authorizations.length > 0) {
      computeData = {
        active: computeRaw.authorizations.some(function(a) { return a.active; }),
        total_credits: computeRaw.authorizations.reduce(function(s, a) { return s + (a.credits_total || 0); }, 0),
        used_credits: computeRaw.authorizations.reduce(function(s, a) { return s + (a.credits_used || 0); }, 0),
        remaining_credits: computeRaw.authorizations.reduce(function(s, a) { return s + (a.credits_remaining || 0); }, 0),
        authorization_count: computeRaw.authorizations.length,
        expires_at: computeRaw.authorizations.reduce(function(l, a) { return a.expires_at > l ? a.expires_at : l; }, ''),
        allow_external: !!computeRaw.allow_external_providers
      };
    } else if (computeRaw) {
      computeData = { active: false, allow_external: !!computeRaw.allow_external_providers };
    }

    renderOverviewTenantInfo(tenantData, centerData, computeData);
  }

  global.loadOverviewTenantInfo = loadOverviewTenantInfo;
  global.renderOverviewTenantInfo = renderOverviewTenantInfo;

})(window);
