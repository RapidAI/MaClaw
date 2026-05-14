/*
 * Digital Employee admin module tests.
 * Run with: node hub/web/admin/ve-tab.test.js
 *
 * Tests covered (Task 7.5):
 * - List rendering: pending and active lists render correct rows
 * - Action button callbacks: approve/reject/disable call correct API endpoints
 * - Config form validation: max_group_participants must be 1-10
 */
'use strict';

const vm = require('vm');
const fs = require('fs');
const path = require('path');

let passed = 0;
let failed = 0;

function assert(condition, message) {
  if (condition) {
    passed++;
  } else {
    failed++;
    console.error('  FAIL:', message);
  }
}

function assertEqual(actual, expected, message) {
  if (actual === expected) {
    passed++;
  } else {
    failed++;
    console.error('  FAIL:', message, '| expected:', JSON.stringify(expected), '| got:', JSON.stringify(actual));
  }
}

function assertIncludes(str, substr, message) {
  if (typeof str === 'string' && str.includes(substr)) {
    passed++;
  } else {
    failed++;
    console.error('  FAIL:', message, '| string does not include:', JSON.stringify(substr));
  }
}

function assertNotIncludes(str, substr, message) {
  if (typeof str === 'string' && !str.includes(substr)) {
    passed++;
  } else {
    failed++;
    console.error('  FAIL:', message, '| string should not include:', JSON.stringify(substr));
  }
}

// --- Setup mock DOM and globals ---
function createMockDOM() {
  var elements = {};
  return {
    elements: elements,
    getElementById: function(id) {
      if (!elements[id]) {
        elements[id] = { id: id, innerHTML: '', textContent: '', value: '', style: {} };
      }
      return elements[id];
    },
    addEventListener: function() {},
    readyState: 'complete'
  };
}

function createMockGlobal() {
  var apiCalls = [];
  var toasts = [];
  var outputs = [];

  var mockGlobal = {
    currentLang: 'zh',
    document: createMockDOM(),
    confirm: function() { return true; },
    prompt: function() { return 'test reason'; },
    console: console,
    Object: Object,
    JSON: JSON,
    Date: Date,
    String: String,
    parseInt: parseInt,
    isNaN: isNaN,
    encodeURIComponent: encodeURIComponent,
    api: async function(url, opts) {
      apiCalls.push({ url: url, opts: opts || {} });
      if (url === '/api/ve/list') {
        return {
          employees: [
            { id: 've-001', name: 'Test VE 1', skill_description: 'Python expert', access_policy: 'public', online_status: 'online', status: 'pending', registered_at: '2024-01-15T10:30:00Z' },
            { id: 've-002', name: 'Test VE 2', skill_description: 'Go developer with extensive backend experience', access_policy: 'whitelist', online_status: 'offline', status: 'active', registered_at: '2024-01-10T08:00:00Z' },
            { id: 've-003', name: 'Test VE 3', skill_description: 'Data analyst', access_policy: 'per_request', online_status: 'online', status: 'active', registered_at: '2024-01-12T14:00:00Z' }
          ],
          group_config: { max_group_participants: 5 },
          quota: 10,
          active_count: 2
        };
      }
      return {};
    },
    escapeHtml: function(str) { return String(str || ''); },
    setOutput: function(msg) { outputs.push(msg); },
    showToast: function(msg, type) { toasts.push({ msg: msg, type: type }); },
    AdminTabRegistry: {
      registerTab: function() {},
      onLanguageChange: function() {}
    },
    _apiCalls: apiCalls,
    _toasts: toasts,
    _outputs: outputs
  };
  mockGlobal.window = mockGlobal;
  return mockGlobal;
}

function loadVETab(mockGlobal) {
  var code = fs.readFileSync(path.join(__dirname, 've-tab.js'), 'utf8');
  var script = new vm.Script(code, { filename: 've-tab.js' });
  var context = vm.createContext(mockGlobal);
  script.runInContext(context);
  return context;
}

async function runTests() {

  // ============================================================
  // TEST SUITE: List Rendering
  // ============================================================
  console.log('\n--- Test Suite: List Rendering ---');

  // Test 1: Pending list renders pending digital employees
  {
    console.log('  Test: Pending list renders pending digital employees with approve/reject buttons');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var pendingList = g.document.elements['vePendingList'];
    assert(pendingList !== undefined, 'vePendingList element should exist');
    assertIncludes(pendingList.innerHTML, 'Test VE 1', 'Pending list should contain pending VE name');
    assertIncludes(pendingList.innerHTML, 'Python expert', 'Pending list should contain skill description');
    assertIncludes(pendingList.innerHTML, 'veApprove', 'Pending list should have approve button');
    assertIncludes(pendingList.innerHTML, 'veReject', 'Pending list should have reject button');
    assertNotIncludes(pendingList.innerHTML, 'Test VE 2', 'Pending list should not contain active digital employees');
    assertNotIncludes(pendingList.innerHTML, 'Test VE 3', 'Pending list should not contain other active digital employees');
  }

  // Test 2: Active list renders active digital employees
  {
    console.log('  Test: Active list renders active digital employees with disable button');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var activeList = g.document.elements['veActiveList'];
    assert(activeList !== undefined, 'veActiveList element should exist');
    assertIncludes(activeList.innerHTML, 'Test VE 2', 'Active list should contain active VE');
    assertIncludes(activeList.innerHTML, 'Test VE 3', 'Active list should contain second active VE');
    assertIncludes(activeList.innerHTML, 'veDisable', 'Active list should have disable button');
    assertNotIncludes(activeList.innerHTML, 'veApprove', 'Active list should not have approve button');
    assertNotIncludes(activeList.innerHTML, 'Test VE 1', 'Active list should not contain pending digital employees');
  }

  // Test 3: List displays all required fields
  {
    console.log('  Test: List displays name, skill_description, access_policy, online_status, registered_at');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var activeList = g.document.elements['veActiveList'];
    assertIncludes(activeList.innerHTML, '\u767d\u540d\u5355', 'Should display whitelist policy in Chinese');
    assertIncludes(activeList.innerHTML, 'badge ok', 'Should have online badge for online VE');
    assertIncludes(activeList.innerHTML, 'badge warn', 'Should have offline badge for offline VE');
  }

  // Test 4: Empty pending list shows hint
  {
    console.log('  Test: Empty pending list shows hint message');
    var g = createMockGlobal();
    g.api = async function(url) {
      if (url === '/api/ve/list') {
        return { employees: [{ id: 've-1', name: 'Active', skill_description: 'x', access_policy: 'public', online_status: 'online', status: 'active', registered_at: '2024-01-01T00:00:00Z' }], group_config: { max_group_participants: 5 }, quota: 10, active_count: 1 };
      }
      return {};
    };
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var pendingList = g.document.elements['vePendingList'];
    assertIncludes(pendingList.innerHTML, 'hint', 'Empty pending list should show hint class');
  }

  // Test 5: Quota info display
  {
    console.log('  Test: Quota info displays active count and quota');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var quotaInfo = g.document.elements['veQuotaInfo'];
    assertIncludes(quotaInfo.textContent, '2', 'Quota info should show active count');
    assertIncludes(quotaInfo.textContent, '10', 'Quota info should show quota');
  }

  // ============================================================
  // TEST SUITE: Action Button Callbacks
  // ============================================================
  console.log('\n--- Test Suite: Action Button Callbacks ---');

  // Test 6: Approve calls correct API
  {
    console.log('  Test: Approve calls POST /api/ve/{id}/approve');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g._apiCalls.length = 0;
    await ctx.veApprove('ve-001');
    var approveCall = g._apiCalls.find(function(c) { return c.url.includes('/approve'); });
    assert(approveCall !== undefined, 'Should call approve API');
    assertEqual(approveCall.url, '/api/ve/ve-001/approve', 'Should call correct approve URL');
    assertEqual(approveCall.opts.method, 'POST', 'Should use POST method');
    var successToast = g._toasts.find(function(t) { return t.type === 'success'; });
    assert(successToast !== undefined, 'Should show success toast after approve');
  }

  // Test 7: Reject calls correct API with reason
  {
    console.log('  Test: Reject calls POST /api/ve/{id}/reject with reason');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g._apiCalls.length = 0;
    await ctx.veReject('ve-001');
    var rejectCall = g._apiCalls.find(function(c) { return c.url.includes('/reject'); });
    assert(rejectCall !== undefined, 'Should call reject API');
    assertEqual(rejectCall.url, '/api/ve/ve-001/reject', 'Should call correct reject URL');
    assertEqual(rejectCall.opts.method, 'POST', 'Should use POST method');
    var body = JSON.parse(rejectCall.opts.body);
    assertEqual(body.reason, 'test reason', 'Should include rejection reason in body');
  }

  // Test 8: Disable calls correct API
  {
    console.log('  Test: Disable calls POST /api/ve/{id}/disable');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g._apiCalls.length = 0;
    await ctx.veDisable('ve-002');
    var disableCall = g._apiCalls.find(function(c) { return c.url.includes('/disable'); });
    assert(disableCall !== undefined, 'Should call disable API');
    assertEqual(disableCall.url, '/api/ve/ve-002/disable', 'Should call correct disable URL');
    assertEqual(disableCall.opts.method, 'POST', 'Should use POST method');
  }

  // Test 9: Approve refreshes list
  {
    console.log('  Test: Approve refreshes VE list after success');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g._apiCalls.length = 0;
    await ctx.veApprove('ve-001');
    var listCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/list'; });
    assert(listCall !== undefined, 'Should refresh list after approve');
  }

  // Test 10: Reject cancelled by user
  {
    console.log('  Test: Reject cancelled when user dismisses prompt');
    var g = createMockGlobal();
    g.prompt = function() { return null; };
    var ctx = loadVETab(g);
    g._apiCalls.length = 0;
    await ctx.veReject('ve-001');
    var rejectCall = g._apiCalls.find(function(c) { return c.url.includes('/reject'); });
    assertEqual(rejectCall, undefined, 'Should not call reject API when user cancels');
  }

  // Test 11: API error shows toast
  {
    console.log('  Test: API error shows error toast');
    var g = createMockGlobal();
    g.api = async function(url) {
      if (url.includes('/approve')) throw new Error('Network error');
      if (url === '/api/ve/list') return { employees: [], group_config: { max_group_participants: 5 }, quota: 10, active_count: 0 };
      return {};
    };
    var ctx = loadVETab(g);
    g._toasts.length = 0;
    await ctx.veApprove('ve-001');
    var errorToast = g._toasts.find(function(t) { return t.type === 'error'; });
    assert(errorToast !== undefined, 'Should show error toast on API failure');
    assertIncludes(errorToast.msg, 'Network error', 'Error toast should contain error message');
  }

  // ============================================================
  // TEST SUITE: Config Form Validation
  // ============================================================
  console.log('\n--- Test Suite: Config Form Validation ---');

  // Test 12: Valid config saves
  {
    console.log('  Test: Valid config (1-10) saves successfully');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veMaxParticipantsInput'] = { value: '7' };
    g._apiCalls.length = 0;
    g._toasts.length = 0;
    await ctx.veSaveGroupConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config'; });
    assert(configCall !== undefined, 'Should call config API');
    assertEqual(configCall.opts.method, 'PUT', 'Should use PUT method');
    var body = JSON.parse(configCall.opts.body);
    assertEqual(body.max_group_participants, 7, 'Should send correct value');
    var successToast = g._toasts.find(function(t) { return t.type === 'success'; });
    assert(successToast !== undefined, 'Should show success toast');
  }

  // Test 13: Rejects 0
  {
    console.log('  Test: Config rejects value 0 (below minimum)');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veMaxParticipantsInput'] = { value: '0' };
    g._apiCalls.length = 0;
    g._toasts.length = 0;
    await ctx.veSaveGroupConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config'; });
    assertEqual(configCall, undefined, 'Should not call API for invalid value 0');
    var errorToast = g._toasts.find(function(t) { return t.type === 'error'; });
    assert(errorToast !== undefined, 'Should show error toast for invalid value');
  }

  // Test 14: Rejects 11
  {
    console.log('  Test: Config rejects value 11 (above maximum)');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veMaxParticipantsInput'] = { value: '11' };
    g._apiCalls.length = 0;
    g._toasts.length = 0;
    await ctx.veSaveGroupConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config'; });
    assertEqual(configCall, undefined, 'Should not call API for invalid value 11');
    var errorToast = g._toasts.find(function(t) { return t.type === 'error'; });
    assert(errorToast !== undefined, 'Should show error toast for value > 10');
  }

  // Test 15: Rejects NaN
  {
    console.log('  Test: Config rejects non-numeric input');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veMaxParticipantsInput'] = { value: 'abc' };
    g._apiCalls.length = 0;
    g._toasts.length = 0;
    await ctx.veSaveGroupConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config'; });
    assertEqual(configCall, undefined, 'Should not call API for NaN input');
    var errorToast = g._toasts.find(function(t) { return t.type === 'error'; });
    assert(errorToast !== undefined, 'Should show error toast for NaN');
  }

  // Test 16: Boundary values 1 and 10
  {
    console.log('  Test: Config accepts boundary values 1 and 10');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veMaxParticipantsInput'] = { value: '1' };
    g._apiCalls.length = 0;
    await ctx.veSaveGroupConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config'; });
    assert(configCall !== undefined, 'Should accept value 1');
    var body = JSON.parse(configCall.opts.body);
    assertEqual(body.max_group_participants, 1, 'Should send value 1');

    g.document.elements['veMaxParticipantsInput'] = { value: '10' };
    g._apiCalls.length = 0;
    await ctx.veSaveGroupConfig();
    configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config'; });
    assert(configCall !== undefined, 'Should accept value 10');
    body = JSON.parse(configCall.opts.body);
    assertEqual(body.max_group_participants, 10, 'Should send value 10');
  }

  // Test 17: Default value after load
  {
    console.log('  Test: Config input shows default value 5 after load');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var input = g.document.elements['veMaxParticipantsInput'];
    assertEqual(input.value, '5', 'Config input should show default value 5');
  }

  // ============================================================
  // TEST SUITE: Tab Registration
  // ============================================================
  console.log('\n--- Test Suite: Tab Registration ---');

  // Test 18: Tab registration
  {
    console.log('  Test: VE tab is registered with AdminTabRegistry');
    var registeredTab = null;
    var g = createMockGlobal();
    g.AdminTabRegistry = {
      registerTab: function(def) { registeredTab = def; },
      onLanguageChange: function() {}
    };
    loadVETab(g);
    assert(registeredTab !== null, 'Should register tab');
    assertEqual(registeredTab.id, 'virtualemployees', 'Tab ID should be virtualemployees');
    assert(typeof registeredTab.title === 'function', 'Tab title should be a function');
    assert(typeof registeredTab.onOpen === 'function', 'Tab onOpen should be a function');
  }

  // ============================================================
  // TEST SUITE: i18n
  // ============================================================
  console.log('\n--- Test Suite: i18n ---');

  // Test 19: Chinese translation
  {
    console.log('  Test: Chinese translations are available for all keys');
    var registeredTab = null;
    var g = createMockGlobal();
    g.currentLang = 'zh';
    g.AdminTabRegistry = {
      registerTab: function(def) { registeredTab = def; },
      onLanguageChange: function() {}
    };
    loadVETab(g);
    var title = registeredTab.title();
    assertEqual(title, '\u6570\u5b57\u5458\u5de5', 'Chinese tab title should be correct');
  }

  // Test 20: English translation
  {
    console.log('  Test: English translations are available');
    var registeredTab = null;
    var g = createMockGlobal();
    g.currentLang = 'en';
    g.AdminTabRegistry = {
      registerTab: function(def) { registeredTab = def; },
      onLanguageChange: function() {}
    };
    loadVETab(g);
    var title = registeredTab.title();
    assertEqual(title, 'Digital Employees', 'English tab title should be correct');
  }

  // --- Summary ---
  console.log('\n=== Results ===');
  console.log('Passed:', passed);
  console.log('Failed:', failed);
  if (failed > 0) {
    process.exitCode = 1;
  }
}

runTests().catch(function(err) {
  console.error('Test runner error:', err);
  process.exitCode = 1;
});
