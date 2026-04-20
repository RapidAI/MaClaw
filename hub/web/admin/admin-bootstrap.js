/*
 * Admin bootstrap runtime.
 * ASCII only.
 */
(function(global) {
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
