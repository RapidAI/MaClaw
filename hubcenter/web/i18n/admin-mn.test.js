'use strict';
var fs = require('fs');
var path = require('path');
var assert = require('assert');

var html = fs.readFileSync(path.join(__dirname, '..', 'admin', 'index.html'), 'utf8');
var core = fs.readFileSync(path.join(__dirname, '..', 'admin', 'assets', 'js', 'admin-core.js'), 'utf8');

assert.ok(html.indexOf("setLanguage('mn')") >= 0, 'index.html calls setLanguage(mn)');
assert.ok(html.indexOf('Монгол') >= 0, 'language button label is Монгол');
assert.ok(!/lang\s*===\s*'zh'\s*\?\s*'zh'\s*:\s*'en'/.test(core), 'setLanguage no longer collapses non-zh to en');
assert.ok(!/langs\.some\([^)]*startsWith\(\s*'zh'\s*\)/.test(core), 'currentLang detection does not scan every navigator language for zh');
assert.ok(/mn\s*:\s*I18N_MN/.test(core), 'I18N has an mn table');
assert.ok(/const I18N_MN\s*=/.test(core), 'I18N_MN exists');

var titleMatch = core.match(/I18N_MN\s*=\s*\{title:\s*'([^']*)'/);
assert.ok(titleMatch, 'mn title is declared on I18N_MN');
var mnTitle = titleMatch[1];
assert.ok(/[А-Яа-яӨөҮү]/.test(mnTitle), 'mn shell title contains Cyrillic');
assert.notStrictEqual(mnTitle, 'MaClaw 节点中心管理后台');
assert.ok(mnTitle.indexOf('节点中心管理后台') < 0, 'mn title is not the Simplified Chinese title');

console.log('admin-mn.test.js ok');
