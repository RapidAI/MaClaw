/*
 * Lazy-load admin modules that are only needed after their navigation tab is
 * opened. Keep this file ASCII-only because it is part of the admin bundle.
 */
(function(global) {
  var modulesByTab = {
    security: 'security-tab.js',
    approvalroles: 'security-tab.js',
    'digital-assets': 'digital-assets-tab.js',
    llmproviders: 'llm-provider-tab.js',
    modelservices: 'llm-service-tabs.js',
    servicecards: 'llm-service-tabs.js'
  };
  var loads = Object.create(null);
  var navigationRequest = 0;
  var eagerOpenTab = global.openTab;

  function normalizeTabID(tab) {
    return String(tab || '').trim().toLowerCase();
  }

  function loadModule(filename) {
    if (!filename) return Promise.resolve();
    if (loads[filename]) return loads[filename];
    loads[filename] = new Promise(function(resolve, reject) {
      var script = document.createElement('script');
      script.src = '/admin/' + filename;
      script.async = false;
      script.onload = resolve;
      script.onerror = function() {
        delete loads[filename];
        reject(new Error('Unable to load admin module: ' + filename));
      };
      document.head.appendChild(script);
    });
    return loads[filename];
  }

  global.loadAdminLazyModule = loadModule;
  global.isAdminLazyModuleLoaded = function(filename) { return !!loads[filename]; };
  global.openTab = function(name) {
    var request = ++navigationRequest;
    var filename = modulesByTab[normalizeTabID(name)];
    if (!filename || typeof eagerOpenTab !== 'function') return eagerOpenTab(name);
    return loadModule(filename).then(function() {
      if (request === navigationRequest) eagerOpenTab(name);
    }).catch(function(err) {
      console.error(err);
      if (typeof global.showToast === 'function') global.showToast(err.message || String(err), 'error');
    });
  };
})(window);
