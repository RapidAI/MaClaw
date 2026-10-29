/* Shared Hub admin language pick. ASCII only.
   Cyrillic Mongolian lives in mn-pack-*.js as window.HUB_ADMIN_DICT_MN. */
(function (global) {
  'use strict';

  global.HUB_ADMIN_DICT_MN = global.HUB_ADMIN_DICT_MN || {};

  function uiLang() {
    var lang = global.currentLang;
    if (lang !== 'zh' && lang !== 'en' && lang !== 'mn') {
      try {
        var saved = global.localStorage && global.localStorage.getItem('maclaw_admin_lang');
        if (saved === 'zh' || saved === 'en' || saved === 'mn') lang = saved;
      } catch (_) {}
    }
    if (lang === 'zh' || lang === 'mn') return lang;
    return 'en';
  }

  function fill(text, vars) {
    var out = String(text == null ? '' : text);
    if (!vars) return out;
    Object.keys(vars).forEach(function (name) {
      out = out.split('{' + name + '}').join(vars[name] == null ? '' : String(vars[name]));
    });
    return out;
  }

  /* tables: { en: {key: text}, zh: {key: text} }. mn overlay is keyed the same way. */
  global.hubDict = function (pack, tables, key, vars) {
    var lang = uiLang();
    var mn = global.HUB_ADMIN_DICT_MN[pack];
    var text = (lang === 'mn' && mn && mn[key]) ||
      (tables && tables[lang] && tables[lang][key]) ||
      (tables && tables.en && tables.en[key]) ||
      key;
    return fill(text, vars);
  };

  /* table: { key: { en, zh } }. mn overlay is { key: text }. */
  global.hubEntry = function (pack, table, key, vars) {
    var lang = uiLang();
    var entry = (table && table[key]) || {};
    var mn = global.HUB_ADMIN_DICT_MN[pack];
    var text = (lang === 'mn' && mn && mn[key]) || entry[lang] || entry.en || key;
    return fill(text, vars);
  };

  /* Inline pair. mn overlay is keyed by the English sentence. */
  global.hubLine = function (pack, zh, en) {
    var lang = uiLang();
    if (lang === 'zh') return zh;
    if (lang === 'mn') {
      var mn = global.HUB_ADMIN_DICT_MN[pack];
      if (mn && mn[en]) return mn[en];
    }
    return en;
  };

  global.hubAdminLang = uiLang;
})(window);
