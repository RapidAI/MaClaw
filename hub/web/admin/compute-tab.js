/*
 * Compute admin module.
 * ASCII only.
 */
(function(global) {
  const COMPUTE_I18N = {
    en: { providerSaveTodo: 'Provider save not yet implemented' },
    zh: { providerSaveTodo: '\u63d0\u4f9b\u5546\u4fdd\u5b58\u529f\u80fd\u6682\u672a\u5b9e\u73b0' }
  };
  function cpt(key) {
    const dict = COMPUTE_I18N[window.currentLang] || COMPUTE_I18N.en;
    return dict[key] || COMPUTE_I18N.en[key] || key;
  }
  global.openComputePane = function openComputePane(pane) {
    document.querySelectorAll('.compute-pane').forEach(function(panel) { panel.classList.remove('active'); });
    var el = document.getElementById('compute-' + pane);
    if (el) el.classList.add('active');
  };

  global.showAddProviderForm = function showAddProviderForm() {
    var form = document.getElementById('addProviderForm');
    if (form) form.classList.remove('hidden');
  };

  global.saveProvider = async function saveProvider() {
    showToast(cpt('providerSaveTodo'), 'info');
  };

  global.hideProviderForm = function hideProviderForm() {
    var form = document.getElementById('addProviderForm');
    if (form) form.classList.add('hidden');
  };

  global.loadCostStats = async function loadCostStats() {
    return null;
  };
})(window);
