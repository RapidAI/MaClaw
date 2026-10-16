/*
 * Admin tab registry.
 * ASCII only.
 */
(function(global) {
  const tabs = Object.create(null);
  const languageListeners = [];
  function normalizeTabID(id) {
    return String(id || '').trim().toLowerCase();
  }
  function resolveText(value) {
    return typeof value === 'function' ? value() : (value || '');
  }
  function registerTab(definition) {
    if (!definition || !definition.id) return;
    const id = normalizeTabID(definition.id);
    if (!id) return;
    tabs[id] = Object.assign({}, tabs[id] || {}, definition, { id: id });
  }
  function getTab(id) {
    const key = normalizeTabID(id);
    return key ? (tabs[key] || null) : null;
  }
  function getTitle(id, fallback) {
    const tab = getTab(id);
    if (!tab || typeof tab.title === 'undefined') return fallback || '';
    return resolveText(tab.title) || fallback || '';
  }
  function getSubtitle(id, fallback) {
    const tab = getTab(id);
    if (!tab || typeof tab.subtitle === 'undefined') return fallback || '';
    return resolveText(tab.subtitle) || fallback || '';
  }
  function onOpen(id) {
    const tab = getTab(id);
    if (tab && typeof tab.onOpen === 'function') tab.onOpen(id);
  }
  function onLanguageChange(listener) {
    if (typeof listener === 'function') languageListeners.push(listener);
  }
  function notifyLanguageChange(lang) {
    languageListeners.forEach(function(listener) {
      try {
        listener(lang);
      } catch (err) {
        console.error('AdminTabRegistry language listener failed:', err);
      }
    });
  }
  global.AdminTabRegistry = {
    registerTab: registerTab,
    getTab: getTab,
    getTitle: getTitle,
    getSubtitle: getSubtitle,
    onOpen: onOpen,
    onLanguageChange: onLanguageChange,
    notifyLanguageChange: notifyLanguageChange
  };
})(window);