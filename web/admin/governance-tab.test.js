/*
 * Governance tab user card tests.
 * Run with: node hub/web/admin/governance-tab.test.js
 */
'use strict';

const fs = require('fs');
const path = require('path');
const vm = require('vm');

let passed = 0;
let failed = 0;

function assert(condition, message) {
  if (condition) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', message);
  }
}

function assertIncludes(haystack, needle, message) {
  assert(typeof haystack === 'string' && haystack.indexOf(needle) !== -1, message + ' | missing: ' + JSON.stringify(needle));
}

function assertNotIncludes(haystack, needle, message) {
  assert(typeof haystack === 'string' && haystack.indexOf(needle) === -1, message + ' | unexpected: ' + JSON.stringify(needle));
}

function createElement(id) {
  return {
    id: id || '',
    innerHTML: '',
    textContent: '',
    dataset: {},
    setAttribute: function() {},
    addEventListener: function() {},
    appendChild: function() {},
    removeChild: function() {}
  };
}

function escapeHtml(value) {
  if (value === null || value === undefined) return '';
  var map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
  return String(value).replace(/[&<>"']/g, function(c) { return map[c]; });
}

function tr(key) {
  var map = { na: 'n/a', emptyBoundUsers: 'No bound users yet.' };
  return map[key] || key;
}

function renderGovernance(lang, items) {
  var root = createElement('boundUsers');
  var elements = { boundUsers: root };
  var context = {
    console: console,
    currentLang: lang,
    tr: tr,
    escapeHtml: escapeHtml,
    token: function() { return 'token'; },
    api: async function() { return {}; },
    setOutput: function() {},
    showToast: function() {},
    document: {
      getElementById: function(id) { return elements[id] || null; },
      createElement: function() { return createElement(''); },
      body: { appendChild: function() {} }
    },
    AdminUI: {
      hint: function(message) { return '<div class="hint">' + message + '</div>'; }
    }
  };
  context.window = context;
  vm.createContext(context);
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'governance-tab.js'), 'utf8'), context);
  context.renderBoundUsers(items);
  return root.innerHTML;
}

function boundUser(overrides) {
  var item = {
    id: 'user-1',
    email: 'user@example.com',
    sn: 'SN-1',
    status: 'approved',
    enrollment_status: 'approved',
    smart_route: false,
    email_verified: false
  };
  Object.keys(overrides || {}).forEach(function(key) { item[key] = overrides[key]; });
  return item;
}

function testEnglishLastLogin() {
  var html = renderGovernance('en', [boundUser({
    last_login_platform: 'windows',
    last_login_app_version: '8.1.0.12464',
    last_login_at: '2026-10-28T10:00:00Z'
  })]);
  assertIncludes(html, 'Last login: Windows \u00b7 8.1.0.12464', 'English card shows the client platform and version');
  assertIncludes(html, 'Last login: Windows \u00b7 8.1.0.12464 |', 'the login timestamp stays available in the tooltip');
}

function testChineseLastLogin() {
  var html = renderGovernance('zh', [boundUser({
    last_login_platform: 'mac',
    last_login_app_version: '8.1.0.12464'
  })]);
  assertIncludes(html, '\u6700\u540e\u767b\u5f55: macOS \u00b7 8.1.0.12464', 'Chinese card localizes the label and platform');
}

function testMissingLastLogin() {
  var english = renderGovernance('en', [boundUser({})]);
  assertIncludes(english, 'Last login: No login record', 'English card explains the missing client');
  var chinese = renderGovernance('zh', [boundUser({})]);
  assertIncludes(chinese, '\u6700\u540e\u767b\u5f55: \u6682\u65e0\u767b\u5f55\u8bb0\u5f55', 'Chinese card explains the missing client');
  var timestampOnly = renderGovernance('en', [boundUser({ last_login_at: '2026-10-28T10:00:00Z' })]);
  assertIncludes(timestampOnly, 'Last login: Unknown platform', 'a timestamp without platform data is still a login record');
}

function testPlatformFallbacks() {
  var mobile = renderGovernance('en', [boundUser({ last_login_platform: 'mobile' })]);
  assertIncludes(mobile, 'Last login: Mobile', 'mobile clients show the platform without a version');
  var customPlatform = renderGovernance('en', [boundUser({ last_login_platform: 'linux-arm64' })]);
  assertIncludes(customPlatform, 'Last login: linux-arm64', 'custom platforms are shown verbatim');
  var versionOnly = renderGovernance('en', [boundUser({ last_login_app_version: '9.9.9' })]);
  assertIncludes(versionOnly, 'Last login: 9.9.9', 'a version without a platform is still shown');
  var unknownPlatform = renderGovernance('en', [boundUser({ last_login_platform: 'unknown', last_login_app_version: '1.0.0' })]);
  assertIncludes(unknownPlatform, 'Last login: Unknown platform \u00b7 1.0.0', 'the server default platform is localized');
  var blank = renderGovernance('en', [boundUser({ last_login_platform: '   ', last_login_app_version: '2.0.0' })]);
  assertIncludes(blank, 'Last login: 2.0.0', 'whitespace-only platforms are ignored');
}

function testEscaping() {
  var html = renderGovernance('en', [boundUser({
    last_login_platform: '<img src=x onerror=alert(1)>',
    last_login_app_version: '" onmouseover="alert(1)'
  })]);
  assertNotIncludes(html, '<img src=x', 'platform markup is escaped');
  assertNotIncludes(html, '" onmouseover="', 'version attribute break-out is escaped');
  assertIncludes(html, '&lt;img src=x', 'escaped platform is still visible as text');
}

Promise.resolve()
  .then(testEnglishLastLogin)
  .then(testChineseLastLogin)
  .then(testMissingLastLogin)
  .then(testPlatformFallbacks)
  .then(testEscaping)
  .then(function() {
    console.log('governance-tab tests: ' + passed + ' passed, ' + failed + ' failed');
    if (failed) process.exit(1);
  })
  .catch(function(err) {
    console.error(err);
    process.exit(1);
  });
