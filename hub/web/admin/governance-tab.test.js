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

function openGovernance(lang, items, state) {
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
  if (state) {
    Object.keys(state).forEach(function(key) { context[key] = state[key]; });
  }
  context.renderBoundUsers(items);
  context.root = root;
  return context;
}

function renderGovernance(lang, items, state) {
  return openGovernance(lang, items, state).root.innerHTML;
}

function localStamp(iso) {
  var date = new Date(iso);
  function pad(n) { return (n < 10 ? '0' : '') + n; }
  return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) + ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes());
}

function localNoonISO(offsetDays) {
  var date = new Date();
  date.setHours(12, 0, 0, 0);
  date.setDate(date.getDate() + offsetDays);
  return date.toISOString();
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
  assertIncludes(html, 'Last login: PC \u00b7 Windows 8.1.0.12464', 'English card shows the client platform and version');
  assertIncludes(html, 'Last login: PC \u00b7 Windows 8.1.0.12464 |', 'the login timestamp stays available in the tooltip');
}

function testChineseLastLogin() {
  var html = renderGovernance('zh', [boundUser({
    last_login_platform: 'mac',
    last_login_app_version: '8.1.0.12464'
  })]);
  assertIncludes(html, '\u6700\u540e\u767b\u5f55: \u7535\u8111\u7aef \u00b7 macOS 8.1.0.12464', 'Chinese card localizes the label and platform');
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
  assertIncludes(customPlatform, 'Last login: PC \u00b7 Linux', 'linux-derived platforms still group under PC');
  var versionOnly = renderGovernance('en', [boundUser({ last_login_app_version: '9.9.9' })]);
  assertIncludes(versionOnly, 'Last login: 9.9.9', 'a version without a platform is still shown');
  var unknownPlatform = renderGovernance('en', [boundUser({ last_login_platform: 'unknown', last_login_app_version: '1.0.0' })]);
  assertIncludes(unknownPlatform, 'Last login: Unknown platform 1.0.0', 'the server default platform is localized');
  var blank = renderGovernance('en', [boundUser({ last_login_platform: '   ', last_login_app_version: '2.0.0' })]);
  assertIncludes(blank, 'Last login: 2.0.0', 'whitespace-only platforms are ignored');
}

function testLoginClientsGrouping() {
  var html = renderGovernance('en', [boundUser({
    login_clients: [
      { platform: 'windows', app_version: '8.1.0.12464', last_login_at: '2026-10-28T10:00:00Z' },
      { platform: 'mac', app_version: '8.0.0.100' },
      { platform: 'android', app_version: '1.0.0' }
    ],
    last_login_platform: 'windows',
    last_login_app_version: '8.1.0.12464',
    last_login_at: '2026-10-28T10:00:00Z'
  })]);
  assertIncludes(html, 'Last login: PC \u00b7 Windows 8.1.0.12464 / macOS 8.0.0.100 | Mobile \u00b7 Android 1.0.0', 'per-platform clients split PC and mobile');
  var harmony = renderGovernance('en', [boundUser({
    login_clients: [{ platform: 'harmonyos', app_version: '2.0.0' }]
  })]);
  assertIncludes(harmony, 'Mobile \u00b7 HarmonyOS 2.0.0', 'HarmonyOS shows under the mobile group');
  var zh = renderGovernance('zh', [boundUser({
    login_clients: [{ platform: 'ios', app_version: '1.2.0' }]
  })]);
  assertIncludes(zh, '\u79fb\u52a8\u7aef \u00b7 iOS 1.2.0', 'mobile group label localizes');
  var genericMobile = renderGovernance('en', [boundUser({
    login_clients: [{ platform: 'mobile', app_version: '1.0.0' }]
  })]);
  assertIncludes(genericMobile, 'Mobile \u00b7 1.0.0', 'the generic mobile platform shows just its version');
}

function testRegisteredDateOnCard() {
  var iso = '2026-11-02T04:00:00Z';
  var stamp = localStamp(iso);
  var english = renderGovernance('en', [boundUser({ created_at: iso })]);
  assertIncludes(english, 'Registered: ' + stamp, 'English card shows the registration date and time');
  assertIncludes(english, 'id="boundUsersRegisteredFrom"', 'registration filter sits above the list');
  assertIncludes(english, 'id="boundUsersRegisteredTo"', 'registration filter has an end date');
  assertIncludes(english, 'width:148px', 'date fields keep a fixed width against the global full-width input rule');
  assertIncludes(english, 'padding:0 4px', 'date fields drop the padding that clips the calendar value');
  assertIncludes(english, 'font-size:13px', 'date fields set their own size so the cluster caption cannot shrink them');
  assert(/id="boundUsersSearchInput"[^>]*font-size:13px/.test(english), 'the search field uses 13px so the English placeholder fits');
  assertIncludes(english, 'min-height:24px', 'card actions keep the height written on them');
  assertIncludes(english, 'text-transform:none', 'the range controls do not inherit the uppercase label style');
  assert(!/inline-flex[^"]*font-size:/.test(english), 'the date cluster does not set a font size the date fields would inherit');
  assertIncludes(english, 'min-height:32px', 'toolbar chips override the global button min-height');
  assertIncludes(english, 'data-registered-preset="1"', 'today is a one-click registration range');
  assertIncludes(english, 'data-registered-preset="7"', 'the last 7 days is a one-click registration range');
  assertIncludes(english, 'data-registered-preset="30"', 'the last 30 days is a one-click registration range');
  assert(english.indexOf('boundUsersRegisteredFrom') < english.indexOf('user@example.com'), 'the date filter renders before the user cards');
  var chinese = renderGovernance('zh', [boundUser({ created_at: iso })]);
  assertIncludes(chinese, '\u6ce8\u518c\u65e5\u671f: ' + stamp, 'Chinese card shows the registration date and time');
  assertIncludes(chinese, '\u6ce8\u518c\u65f6\u95f4', 'Chinese filter is labeled by registration time');
  assertIncludes(chinese, '>\u4eca\u5929<', 'Chinese preset says today');
  var missing = renderGovernance('zh', [boundUser({})]);
  assertIncludes(missing, '\u6ce8\u518c\u65e5\u671f: \u6682\u65e0\u6ce8\u518c\u65e5\u671f', 'a card without a registration time says so');
}

function testRegisteredRangeFilter() {
  var early = new Date(2026, 10, 1, 9, 0, 0).toISOString();
  var mid = new Date(2026, 10, 2, 15, 0, 0).toISOString();
  var late = new Date(2026, 10, 3, 8, 0, 0).toISOString();
  var items = [
    boundUser({ id: 'a', email: 'early@example.com', created_at: early }),
    boundUser({ id: 'b', email: 'mid@example.com', created_at: mid }),
    boundUser({ id: 'c', email: 'late@example.com', created_at: late }),
    boundUser({ id: 'd', email: 'none@example.com' })
  ];
  var html = renderGovernance('en', items, {
    _boundUsersRegisteredFrom: '2026-11-02',
    _boundUsersRegisteredTo: '2026-11-02'
  });
  assertIncludes(html, 'mid@example.com', 'the chosen registration day stays visible');
  assertNotIncludes(html, 'early@example.com', 'the day before the range is hidden');
  assertNotIncludes(html, 'late@example.com', 'the day after the range is hidden');
  assertNotIncludes(html, 'none@example.com', 'a user without a registration date is hidden while a range is set');
  assertIncludes(html, 'value="2026-11-02"', 'the chosen range stays in the date inputs');
  assertIncludes(html, '>Clear<', 'an active range offers a clear control');
  assertNotIncludes(html, '>Prev<', 'a registration range that fits on one page does not add a pager');
  var side = html.slice(html.indexOf('data-bound-users-side'), html.indexOf('id="syncVerifiedPhoneRoutesBtn"'));
  assert(side.indexOf('type="checkbox"') !== -1, 'the referred filter stays with the sync button');
  var openEnded = renderGovernance('en', items, { _boundUsersRegisteredFrom: '2026-11-03' });
  assertIncludes(openEnded, 'late@example.com', 'an open start date keeps later registrations');
  assertNotIncludes(openEnded, 'mid@example.com', 'an open start date hides earlier registrations');
  var ordered = renderGovernance('en', items, {
    _boundUsersRegisteredFrom: '2026-11-01',
    _boundUsersRegisteredTo: '2026-11-03'
  });
  assert(ordered.indexOf('late@example.com') < ordered.indexOf('mid@example.com'), 'a registration range lists the newest registration first');
  assert(ordered.indexOf('mid@example.com') < ordered.indexOf('early@example.com'), 'older registrations follow the newest one');
  var reversed = renderGovernance('en', items, {
    _boundUsersRegisteredFrom: '2026-11-03',
    _boundUsersRegisteredTo: '2026-11-01'
  });
  assertIncludes(reversed, 'early@example.com', 'a reversed range still includes the earlier day');
  assertIncludes(reversed, 'late@example.com', 'a reversed range still includes the later day');
  assert(reversed.indexOf('value="2026-11-01"') !== -1 && reversed.indexOf('value="2026-11-01"') < reversed.indexOf('value="2026-11-03"'), 'a reversed range is shown from the earlier day');
  var withSearch = renderGovernance('en', items, {
    _boundUsersSearch: 'late',
    _boundUsersRegisteredFrom: '2026-11-01',
    _boundUsersRegisteredTo: '2026-11-03'
  });
  assertIncludes(withSearch, 'late@example.com', 'search and registration range apply together');
  assertNotIncludes(withSearch, 'early@example.com', 'search still hides users outside the query');
}

function testRegisteredPresets() {
  var items = [
    boundUser({ id: 'today', email: 'today@example.com', created_at: localNoonISO(0) }),
    boundUser({ id: 'week', email: 'week@example.com', created_at: localNoonISO(-3) }),
    boundUser({ id: 'old', email: 'old@example.com', created_at: localNoonISO(-10) })
  ];
  var app = openGovernance('zh', items);
  app._applyBoundUsersRegisteredPreset(7);
  var html = app.root.innerHTML;
  assertIncludes(html, 'today@example.com', 'the 7-day preset keeps today');
  assertIncludes(html, 'week@example.com', 'the 7-day preset keeps a registration inside the window');
  assertNotIncludes(html, 'old@example.com', 'the 7-day preset hides an older registration');
  assert(html.indexOf('today@example.com') < html.indexOf('week@example.com'), 'preset results are newest first');
  assertIncludes(html, 'data-registered-preset="7" data-active="1"', 'the 7-day preset shows as selected');
  app._applyBoundUsersRegisteredPreset(1);
  html = app.root.innerHTML;
  assertIncludes(html, 'today@example.com', 'the today preset keeps today');
  assertNotIncludes(html, 'week@example.com', 'the today preset hides earlier days');
  assertIncludes(html, 'data-registered-preset="1" data-active="1"', 'the today preset shows as selected');
  app._applyBoundUsersRegisteredPreset(1);
  html = app.root.innerHTML;
  assertIncludes(html, 'old@example.com', 'clicking the selected today preset clears the range');
  assertNotIncludes(html, 'data-active="1"', 'toggling the selected preset deselects it');
  app._applyBoundUsersRegisteredPreset(7);
  app._setBoundUsersRegisteredRange('', '');
  html = app.root.innerHTML;
  assertIncludes(html, 'old@example.com', 'clearing the range shows every user again');
  assertNotIncludes(html, 'data-active="1"', 'clearing the range deselects the presets');
}

function testRegisteredRangeRejectsUnusableDays() {
  var items = [boundUser({ id: 'a', email: 'kept@example.com', created_at: '2026-11-02T04:00:00Z' })];
  var junk = renderGovernance('en', items, { _boundUsersRegisteredFrom: 'soon' });
  assertIncludes(junk, 'kept@example.com', 'a non-date range does not hide users');
  assertNotIncludes(junk, 'value="soon"', 'a non-date range is not kept in the input');
  assertNotIncludes(junk, '>Clear<', 'a rejected range does not stay active');
  var impossible = renderGovernance('en', items, {
    _boundUsersRegisteredFrom: '2026-02-31',
    _boundUsersRegisteredTo: '2026-02-31'
  });
  assertIncludes(impossible, 'kept@example.com', 'an impossible calendar day does not hide users');
  assertNotIncludes(impossible, 'value="2026-02-31"', 'an impossible calendar day is not kept');
}

function testRegisteredRangePageCount() {
  var items = [];
  var i;
  for (i = 0; i < 40; i++) {
    items.push(boundUser({
      id: 'u' + i,
      email: 'user' + (i < 10 ? '0' : '') + i + '@example.com',
      created_at: '2026-11-02T04:00:00Z'
    }));
  }
  items.push(boundUser({ id: 'old', email: 'old@example.com', created_at: '2020-01-01T00:00:00Z' }));
  var html = renderGovernance('en', items, {
    _boundUsersRegisteredFrom: '2026-11-02',
    _boundUsersRegisteredTo: '2026-11-02'
  });
  assertIncludes(html, 'Regular Users (36/40)', 'a filtered group that continues past this page shows shown/matched');
  assertIncludes(html, '1-36 / 40', 'the pager counts every user in the registration range');
  assertIncludes(html, 'min-height:28px', 'pager buttons keep the height written on them');
  assertNotIncludes(html, 'old@example.com', 'a registration outside the range stays off the page');
}

function testGovernanceCheckboxSize() {
  var html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  var broad = html.indexOf('#tab-governance input,#tab-governance select{height:34px}');
  var checkbox = html.indexOf('#tab-governance input[type="checkbox"]{width:15px;height:15px;min-height:15px;padding:0}');
  assert(broad !== -1 && checkbox > broad, 'governance checkboxes keep the 15px box instead of the 34px field height');
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
  .then(testLoginClientsGrouping)
  .then(testRegisteredDateOnCard)
  .then(testRegisteredRangeFilter)
  .then(testRegisteredPresets)
  .then(testRegisteredRangeRejectsUnusableDays)
  .then(testRegisteredRangePageCount)
  .then(testGovernanceCheckboxSize)
  .then(testEscaping)
  .then(function() {
    console.log('governance-tab tests: ' + passed + ' passed, ' + failed + ' failed');
    if (failed) process.exit(1);
  })
  .catch(function(err) {
    console.error(err);
    process.exit(1);
  });
