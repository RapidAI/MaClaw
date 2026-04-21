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

  global.refreshAll = async function refreshAll() {
    await Promise.all([
      loadCenterStatus(),
      loadBlockedEmails(),
      loadBoundUsers(),
      loadMailConfig(),
      loadFeishuConfig(),
      loadMachines(),
      loadPwaEnrollments()
    ]);
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
