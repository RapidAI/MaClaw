/*
 * Admin module health checks.
 * ASCII only.
 */
(function(global) {
  const checks = [
    { name: 'AdminTabRegistry', ok: function() { return !!global.AdminTabRegistry; } },
    { name: 'AdminUI', ok: function() { return !!global.AdminUI; } },
    { name: 'CenterTab', ok: function() { return typeof global.loadCenterStatus === 'function'; } },
    { name: 'GovernanceTab', ok: function() { return typeof global.loadBlockedEmails === 'function' && typeof global.loadBoundUsers === 'function'; } },
    { name: 'SecurityTab', ok: function() { return typeof global.loadSecurityTab === 'function'; } },
    { name: 'MachinesTab', ok: function() { return typeof global.loadMachines === 'function' && typeof global.renderMachineList === 'function'; } },
    { name: 'ImTab', ok: function() { return typeof global.openImSub === 'function'; } },
    { name: 'HubLlmTab', ok: function() { return typeof global.loadHubLlmConfig === 'function' && typeof global.loadHubLlmStatus === 'function'; } },
    { name: 'FeishuTab', ok: function() { return typeof global.loadFeishuConfig === 'function'; } },
    { name: 'InvitationTab', ok: function() { return typeof global.loadInvitationCodes === 'function'; } },
    { name: 'PwaTab', ok: function() { return typeof global.loadPwaEnrollments === 'function'; } },
    { name: 'SystemTab', ok: function() { return typeof global.loadMailConfig === 'function' && typeof global.loadTlsConfig === 'function'; } },
    { name: 'ComputeTab', ok: function() { return typeof global.openComputePane === 'function'; } },
    { name: 'LlmProviderTab', ok: function() { return typeof global.openLlmProviderTab === 'function' || typeof global.renderLlmProviderList === 'function' || typeof global.loadLlmProviders === 'function'; } },
    { name: 'LlmServiceTabs', ok: function() { return typeof global.openLlmServiceGroupTab === 'function' || typeof global.loadLlmServiceGroups === 'function'; } },
    { name: 'UsageStatsTab', ok: function() { return typeof global.loadUsageStats === 'function' || typeof global.renderUsageStatsCharts === 'function'; } }
  ];

  function runChecks() {
    const failed = checks.filter(function(item) {
      try {
        return !item.ok();
      } catch (_) {
        return true;
      }
    }).map(function(item) { return item.name; });
    global.__adminModuleHealth = {
      ok: failed.length === 0,
      failed: failed,
      checkedAt: new Date().toISOString()
    };
    if (failed.length) {
      console.error('Admin module health check failed:', failed.join(', '));
    }
  }

  runChecks();
})(window);
