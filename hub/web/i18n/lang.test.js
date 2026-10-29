'use strict';

var i18n = require('./lang');
var failed = 0;
var passed = 0;

function assertEqual(actual, expected, message) {
  if (actual === expected) {
    passed += 1;
    console.log('  OK ' + message);
    return;
  }
  failed += 1;
  console.log('  ERR ' + message + ' (expected ' + JSON.stringify(expected) + ', got ' + JSON.stringify(actual) + ')');
}

console.log('\n=== Hub lang helper ===\n');

assertEqual(i18n.normalizeHubLang(''), 'en', 'empty -> en');
assertEqual(i18n.normalizeHubLang('ja'), 'en', 'ja -> en');
assertEqual(i18n.normalizeHubLang('mn-Mong'), 'en', 'mn-Mong -> en');
assertEqual(i18n.normalizeHubLang('mn-Mong-CN'), 'en', 'mn-Mong-CN -> en');
assertEqual(i18n.normalizeHubLang('ko'), 'en', 'ko -> en');
assertEqual(i18n.normalizeHubLang('fr'), 'en', 'fr -> en');
assertEqual(i18n.normalizeHubLang('zh-TW'), 'zh', 'zh-TW -> zh');
assertEqual(i18n.normalizeHubLang('zh-CN'), 'zh', 'zh-CN -> zh');
assertEqual(i18n.normalizeHubLang('zh-Hans'), 'zh', 'zh-Hans -> zh');
assertEqual(i18n.normalizeHubLang('zh-Hant'), 'zh', 'zh-Hant -> zh');
assertEqual(i18n.normalizeHubLang('zh-HK'), 'zh', 'zh-HK -> zh');
assertEqual(i18n.normalizeHubLang('mn-MN'), 'mn', 'mn-MN -> mn');
assertEqual(i18n.normalizeHubLang('mn-Cyrl-MN'), 'mn', 'mn-Cyrl-MN -> mn');
assertEqual(i18n.normalizeHubLang('mn'), 'mn', 'mn -> mn');
assertEqual(i18n.normalizeHubLang('mn-Cyrl'), 'mn', 'mn-Cyrl -> mn');
assertEqual(i18n.detectHubLang(['en-US', 'zh-CN']), 'en', "detectHubLang(['en-US','zh-CN']) -> en");
assertEqual(i18n.detectHubLang(['zh-CN'], null), 'zh', "detectHubLang(['zh-CN'], null) -> zh");
assertEqual(i18n.detectHubLang(['zh-CN'], 'mn'), 'mn', "saved 'mn' wins over ['zh-CN']");
assertEqual(i18n.pick({ en: 'E', zh: 'Z' }, 'mn'), 'E', "pick({en:'E', zh:'Z'}, 'mn') -> E");
assertEqual(i18n.pick({ en: 'E', mn: 'M' }, 'mn'), 'M', "pick({en:'E', mn:'M'}, 'mn') -> M");
assertEqual(i18n.htmlLang('zh'), 'zh-CN', "htmlLang('zh') -> zh-CN");
assertEqual(i18n.htmlLang('mn'), 'mn', "htmlLang('mn') -> mn");
assertEqual(i18n.htmlLang('ja'), 'en', "htmlLang('ja') -> en");

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed) process.exit(1);
