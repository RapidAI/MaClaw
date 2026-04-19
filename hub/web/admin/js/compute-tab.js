/*
 * Compute admin module.
 * ASCII only.
 */
(function(global) {
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
    showToast('Provider save not yet implemented', 'info');
  };

  global.hideProviderForm = function hideProviderForm() {
    var form = document.getElementById('addProviderForm');
    if (form) form.classList.add('hidden');
  };

  global.loadCostStats = async function loadCostStats() {
    return null;
  };
})(window);
