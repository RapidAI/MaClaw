/*
 * Admin bootstrap runtime.
 * ASCII only.
 */
(function(global) {
  function registerCoreTabs() {
    if (!global.AdminTabRegistry || typeof global.AdminTabRegistry.registerTab !== 'function') return;
    if (typeof global.tr !== 'function') return;

    [
      ['overview', 'overviewTabTitle', 'overviewTabSubtitle'],
      ['system', 'systemTabTitle', 'systemTabSubtitle'],
      ['im', 'imTabTitle', 'imTabSubtitle'],
      ['machines', 'machinesTabTitle', 'machinesTabSubtitle'],
      ['invitationcodes', 'invitationCodesTabTitle', 'invitationCodesTabSubtitle'],
      ['pwarequests', 'pwaRequestsTabTitle', 'pwaRequestsTabSubtitle'],
      ['console', 'consoleTabTitle', 'consoleTabSubtitle']
    ].forEach(function(def) {
      global.AdminTabRegistry.registerTab({
        id: def[0],
        title: function() { return global.tr(def[1]); },
        subtitle: function() { return global.tr(def[2]); }
      });
    });
  }

  function hasCoreRuntime() {
    return typeof global.applyI18n === 'function'
      && typeof global.token === 'function'
      && typeof global.setAuthState === 'function'
      && typeof global.restoreTab === 'function'
      && typeof global.refreshAuthStage === 'function';
  }

  if (!hasCoreRuntime()) {
    console.error('Admin bootstrap aborted: core admin runtime is not ready.');
    return;
  }

  function isTenantAdminProfile(profile) {
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  function callIfAvailable(name) {
    var fn = global[name];
    if (typeof fn !== 'function') return Promise.resolve({ skipped: name });
    return Promise.resolve().then(fn);
  }

  function reportRefreshFailures(results) {
    var failed = (results || []).filter(function(result) { return result && result.status === 'rejected' && !(result.reason && result.reason.staleAuth); });
    if (!failed.length || typeof global.setOutput !== 'function') return;
    var names = failed.map(function(result) { return result.reason && result.reason.message || String(result.reason || 'refresh failed'); }).join('; ');
    global.setOutput((typeof global.tr === 'function' ? global.tr('requestFailed') : 'Refresh failed') + ': ' + names);
  }

  global.refreshAll = async function refreshAll() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    var tenantAdmin = isTenantAdminProfile(profile);
    var tasks = tenantAdmin
      ? ['loadOverviewTenantInfo', 'loadTenants', 'loadBlockedEmails', 'loadBoundUsers', 'loadInvites', 'loadMachines', 'loadPwaEnrollments', 'loadMarketplace', 'loadTenantMailSenderName', 'loadTenantMigrationSettings', 'loadTenantSystemLLMDefaults', 'checkComputeAuthorization', 'loadLlmProviders', 'loadLlmServiceGroups', 'loadUsageStats', 'loadFailureLogs']
      : ['loadOverviewTenantInfo', 'loadCenterStatus', 'loadMailConfig', 'loadTenants'];
    var results = await Promise.allSettled(tasks.map(callIfAvailable));
    reportRefreshFailures(results);
    // After login/refresh: soft-block when system-free is not ready.
    if (typeof global.maybeShowSystemFreeGate === 'function') {
      try { await global.maybeShowSystemFreeGate(false); } catch (_) {}
    }
    if (typeof global.applyConfigAgentI18n === 'function') {
      try { global.applyConfigAgentI18n(); } catch (_) {}
    }
  };

  try {
    applyI18n();
    registerCoreTabs();
  } catch (err) {
    console.error('applyI18n failed:', err);
  }

  if (token()) {
    setAuthState(true);
    restoreTab();
    global.refreshAll();
  } else {
    setAuthState(false);
    refreshAuthStage();
  }
})(window);
