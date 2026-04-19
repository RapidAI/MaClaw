/*
 * Admin bootstrap runtime.
 * ASCII only.
 */
(function(global) {
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
