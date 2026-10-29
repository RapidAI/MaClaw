'use strict';

var fs = require('fs');
var path = require('path');
var failed = 0;
var passed = 0;

function assert(cond, message) {
  if (cond) {
    passed += 1;
    console.log('  OK ' + message);
    return;
  }
  failed += 1;
  console.log('  ERR ' + message);
}

var pages = [
  'skillmarket/index.html',
  'skillmarket/user/index.html',
  'skillhub/index.html',
  'gossip/index.html',
  'compute-store/index.html',
  'developer/index.html',
  'developer/docs/index.html'
];

var collapse = /lang\s*===\s*'zh'\s*\?\s*'zh'\s*:\s*'en'|lang\s*===\s*'en'\s*\?\s*'en'\s*:\s*'zh'/;
var cyrillic = /[А-Яа-яӨөҮү]/;
var setsMn = /htmlLang\(\s*'mn'\s*\)|lang\s*=\s*['"]mn['"]/;

console.log('\n=== HubCenter public Mongolian pages ===\n');

pages.forEach(function (rel) {
  var file = path.join(__dirname, '..', rel);
  var src = fs.readFileSync(file, 'utf8');
  console.log('\n--- ' + rel + ' ---');
  assert(src.indexOf('Монгол') !== -1, rel + ' contains Монгол');
  assert(src.indexOf('HubCenterI18n') !== -1 || src.indexOf('normalizeHubLang') !== -1, rel + ' references HubCenterI18n or normalizeHubLang');
  assert(cyrillic.test(src), rel + ' contains Khalkha Cyrillic UI copy');
  assert(!collapse.test(src), rel + ' does not collapse non-zh to en');
  assert(setsMn.test(src), rel + ' sets htmlLang(\'mn\') or lang = \'mn\'');
  if (rel === 'skillhub/index.html') {
    assert(src.indexOf("n >= 1000) return (n / 1000)") !== -1 && src.indexOf("мянга") !== -1, rel + ' counts мянга by thousands');
    assert(src.indexOf("сая") !== -1, rel + ' counts сая by millions');
    assert(!/\/\s*10000\)[\s\S]{0,80}мянга/.test(src), rel + ' does not label a 10000-scale figure as мянга');
  }
  var langTag = '<script src="/i18n/lang.js"></script>';
  var langJs = src.indexOf(langTag);
  var inline = src.indexOf('<script>', langJs === -1 ? 0 : langJs + langTag.length);
  assert(langJs !== -1, rel + ' loads /i18n/lang.js without defer');
  assert(langJs !== -1 && inline > langJs, rel + ' loads /i18n/lang.js before the inline script');
});

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed) process.exit(1);
