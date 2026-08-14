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
    if (loads[filename]) return loads[filename].promise;
    var entry = { loaded: false, promise: null };
    loads[filename] = entry;
    entry.promise = new Promise(function(resolve, reject) {
      var script = document.createElement('script');
      script.src = '/admin/' + filename;
      script.async = false;
      script.onload = function() {
        // Modules may register translations after the initial i18n pass. Apply
        // the active locale as part of loading so both navigation and callers
        // of loadAdminLazyModule receive a fully localized module.
        if (typeof global.applyI18n === 'function') global.applyI18n();
        if (global.AdminTabRegistry && typeof global.AdminTabRegistry.notifyLanguageChange === 'function') {
          global.AdminTabRegistry.notifyLanguageChange(global.currentLang);
        }
        entry.loaded = true;
        resolve();
      };
      script.onerror = function() {
        if (loads[filename] === entry) delete loads[filename];
        reject(new Error('Unable to load admin module: ' + filename));
      };
      document.head.appendChild(script);
    });
    return entry.promise;
  }

  global.loadAdminLazyModule = loadModule;
  global.isAdminLazyModuleLoaded = function(filename) {
    return !!(loads[filename] && loads[filename].loaded);
  };
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
