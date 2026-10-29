'use strict';

function normalizeHubLang(input) {
  var s = String(input || '').trim().toLowerCase().replace(/_/g, '-');
  if (!s) return 'en';
  if (s === 'zh' || s.indexOf('zh-') === 0) return 'zh';
  if (s === 'mn-mong' || s.indexOf('mn-mong-') === 0) return 'en';
  if (
    s === 'mn' ||
    s === 'mn-mn' ||
    s === 'mn-cyrl' ||
    s === 'mn-cyrl-mn' ||
    s.indexOf('mn-cyrl-') === 0 ||
    s.indexOf('mn-mn-') === 0
  ) {
    return 'mn';
  }
  return 'en';
}

function detectHubLang(languages, saved) {
  if (saved === 'en' || saved === 'zh' || saved === 'mn') return saved;
  var first = '';
  if (languages && languages.length) first = languages[0];
  return normalizeHubLang(first || 'en');
}

function pick(dict, lang) {
  var table = dict || {};
  var normalized = normalizeHubLang(lang);
  if (normalized === 'mn') return table.mn || table.en;
  return table[normalized] || table.en;
}

function htmlLang(lang) {
  var normalized = normalizeHubLang(lang);
  if (normalized === 'zh') return 'zh-CN';
  if (normalized === 'mn') return 'mn';
  return 'en';
}

module.exports = {
  normalizeHubLang: normalizeHubLang,
  detectHubLang: detectHubLang,
  pick: pick,
  htmlLang: htmlLang
};
