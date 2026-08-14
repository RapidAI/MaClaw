/*
 * Lazy admin module loader tests.
 * Run with: node hub/web/admin/admin-lazy-module-loader.test.js
 */
'use strict';

const fs = require('fs');
const path = require('path');
const vm = require('vm');

let passed = 0;
let failed = 0;

function assertEqual(actual, expected, message) {
  if (actual === expected) {
    passed += 1;
  } else {
    failed += 1;
    console.error('  FAIL:', message, '| expected:', expected, '| got:', actual);
  }
}

function createRuntime() {
  const scripts = [];
  const opened = [];
  let deferScriptLoad = false;
  let translationsRegistered = false;
  let i18nCalls = 0;
  let languageNotifications = 0;
  const global = {
    Promise: Promise,
    Error: Error,
    String: String,
    Object: Object,
    console: console,
    currentLang: 'zh',
    openTab: function(name) {
      opened.push({ name: name, i18nCalls: i18nCalls });
    },
    applyI18n: function() {
      assertEqual(translationsRegistered, true, 'translations must register before i18n runs');
      i18nCalls += 1;
    },
    AdminTabRegistry: {
      notifyLanguageChange: function(lang) {
        assertEqual(lang, 'zh', 'language notification should use the active language');
        languageNotifications += 1;
      }
    },
    document: {
      createElement: function() {
        return { async: true, src: '', onload: null, onerror: null };
      },
      head: {
        appendChild: function(script) {
          scripts.push(script);
          translationsRegistered = true;
          if (!deferScriptLoad) script.onload();
        }
      }
    }
  };
  global.window = global;
  return {
    global: global,
    scripts: scripts,
    opened: opened,
    deferScriptLoad: function() { deferScriptLoad = true; },
    finishScriptLoad: function(index) { scripts[index].onload(); },
    getI18nCalls: function() { return i18nCalls; },
    getLanguageNotifications: function() { return languageNotifications; }
  };
}

async function runTests() {
  {
    const runtime = createRuntime();
    const source = fs.readFileSync(path.join(__dirname, 'admin-lazy-module-loader.js'), 'utf8');
    vm.runInNewContext(source, runtime.global, { filename: 'admin-lazy-module-loader.js' });

    await runtime.global.loadAdminLazyModule('digital-assets-tab.js');
    assertEqual(runtime.scripts.length, 1, 'direct lazy-module load should insert one script');
    assertEqual(runtime.getI18nCalls(), 1, 'direct lazy-module load should re-apply i18n once');
    assertEqual(runtime.getLanguageNotifications(), 1, 'direct lazy-module load should notify module language listeners');
    assertEqual(runtime.opened.length, 0, 'direct lazy-module load should not navigate');
  }

  const runtime = createRuntime();
  const source = fs.readFileSync(path.join(__dirname, 'admin-lazy-module-loader.js'), 'utf8');
  vm.runInNewContext(source, runtime.global, { filename: 'admin-lazy-module-loader.js' });

  await runtime.global.openTab('digital-assets');
  assertEqual(runtime.scripts.length, 1, 'first lazy tab open should insert one script');
  assertEqual(runtime.scripts[0].src, '/admin/digital-assets-tab.js', 'digital assets should load its module');
  assertEqual(runtime.getI18nCalls(), 1, 'first lazy module load should re-apply i18n once');
  assertEqual(runtime.getLanguageNotifications(), 1, 'first lazy module load should notify module language listeners');
  assertEqual(runtime.opened.length, 1, 'first lazy tab open should reach the original handler');
  assertEqual(runtime.opened[0].i18nCalls, 1, 'i18n should finish before opening the lazy tab');

  await runtime.global.openTab('digital-assets');
  assertEqual(runtime.scripts.length, 1, 'cached lazy module should not insert another script');
  assertEqual(runtime.getI18nCalls(), 1, 'cached lazy module should not repeat full-page i18n');
  assertEqual(runtime.getLanguageNotifications(), 1, 'cached lazy module should not repeat language notifications');
  assertEqual(runtime.opened.length, 2, 'cached lazy tab should still open normally');

  {
    const pendingRuntime = createRuntime();
    pendingRuntime.deferScriptLoad();
    vm.runInNewContext(source, pendingRuntime.global, { filename: 'admin-lazy-module-loader.js' });
    const pendingLoad = pendingRuntime.global.loadAdminLazyModule('digital-assets-tab.js');
    assertEqual(pendingRuntime.global.isAdminLazyModuleLoaded('digital-assets-tab.js'), false, 'in-flight module should not report as loaded');
    pendingRuntime.finishScriptLoad(0);
    await pendingLoad;
    assertEqual(pendingRuntime.global.isAdminLazyModuleLoaded('digital-assets-tab.js'), true, 'loaded module should report as loaded after its script finishes');
  }
}

runTests().then(function() {
  console.log('\n=== Results: ' + passed + '/' + (passed + failed) + ' passed ===');
  if (failed > 0) process.exit(1);
}).catch(function(err) {
  console.error(err);
  process.exit(1);
});
