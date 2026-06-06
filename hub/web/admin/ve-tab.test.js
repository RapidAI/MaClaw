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

function countOccurrences(str, substr) {
  if (typeof str !== 'string' || !substr) return 0;
  return str.split(substr).length - 1;
}

// --- Setup mock DOM and globals ---
function createMockDOM() {
  var elements = {};
  function makeElement(id) {
    return {
      id: id || '',
      innerHTML: '',
      textContent: '',
      value: '',
      style: {},
      classList: { add: function() {}, remove: function() {} },
      querySelectorAll: function() { return []; }
    };
  }
  return {
    elements: elements,
    getElementById: function(id) {
      if (!elements[id]) {
        elements[id] = makeElement(id);
      }
      return elements[id];
    },
    createElement: function(tag) {
      return makeElement(tag);
    },
    body: {
      appendChild: function(el) {
        if (el && el.id) elements[el.id] = el;
      }
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
    location: { origin: 'https://hub.example' },
    Object: Object,
    JSON: JSON,
    Date: Date,
    URL: URL,
    String: String,
    parseInt: parseInt,
    isNaN: isNaN,
    encodeURIComponent: encodeURIComponent,
    api: async function(url, opts) {
      apiCalls.push({ url: url, opts: opts || {} });
      if (url === '/api/ve/list') {
        return {
          employees: [
            { id: 've-001', name: 'Test VE 1', employee_type: 'physical', avatar_data_url: 'data:image/png;base64,iVBORw0KGgo=', skill_description: 'Python expert', access_policy: 'public', online_status: 'online', status: 'pending', registered_at: '2024-01-15T10:30:00Z' },
            { id: 've-002', name: 'Test VE 2', employee_type: 'physical', skill_description: 'Go developer with extensive backend experience', access_policy: 'whitelist', visible_group_ids: ['dept-legal'], online_status: 'offline', status: 'active', registered_at: '2024-01-10T08:00:00Z' },
            { id: 've-003', name: 'Test VE 3', employee_type: 'physical', platform_id: 'maclawsrv', platform_employee_id: 'srv-user-1', skill_description: 'Data analyst', access_policy: 'per_request', online_status: 'online', status: 'active', resident: true, registered_at: '2024-01-12T14:00:00Z' },
            { id: 've-004', name: 'Test VE 4', runtime_provider_id: 'maclawsrv', skill_description: 'Runtime user', access_policy: 'public', online_status: 'online', status: 'active', registered_at: '2024-01-13T14:00:00Z' },
            { id: 've-005', name: 'Test VE 5', runtime_provider_id: 'other-runtime', skill_description: 'Other runtime user', access_policy: 'public', online_status: 'online', status: 'active', registered_at: '2024-01-14T14:00:00Z' }
          ],
          group_config: { max_group_participants: 5, auto_approve: true },
          quota: 10,
          active_count: 4
        };
      }
      if (url === '/api/admin/security/groups') {
        return { tree: { id: 'root', name: 'Root', children: [{ id: 'dept-legal', name: 'Legal', children: [{ id: 'dept-contract', name: 'Contracts', children: [] }] }] } };
      }
      return {};
    },
    escapeHtml: function(str) { return String(str || '').replace(/[&<>\"']/g, function(c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '\"': '&quot;', "'": '&#39;' }[c]; }); },
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
    assertIncludes(pendingList.innerHTML, 'class="ve-avatar"', 'Pending list should render avatar shell');
    assertIncludes(pendingList.innerHTML, 'data:image/png;base64,iVBORw0KGgo=', 'Pending list should render avatar image data URL');
    assertIncludes(pendingList.innerHTML, 'aria-hidden="true"', 'Avatar should be decorative because name text is adjacent');
    assertIncludes(pendingList.innerHTML, 'loading="lazy"', 'Avatar image should lazy-load');
    assertIncludes(pendingList.innerHTML, 'decoding="async"', 'Avatar image should decode asynchronously');
    assertIncludes(pendingList.innerHTML, 'onerror="this.hidden=true;this.nextElementSibling.hidden=false"', 'Avatar image should fall back without layout shift');
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
    assertIncludes(activeList.innerHTML, 've-avatar-fallback', 'Active list should render fallback avatar for VE without image');
    assertIncludes(activeList.innerHTML, 'Test VE 3', 'Active list should contain second active VE');
    assertIncludes(activeList.innerHTML, 'veDisable', 'Active list should have disable button');
    assertIncludes(activeList.innerHTML, 'veSetResident', 'Active list should have resident button');
    assertIncludes(activeList.innerHTML, '\u5e38\u9a7b', 'Active list should display resident badge');
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

  // Test 3a: List displays digital employee type
  {
    console.log('  Test: List displays physical and virtual digital employee type');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var pendingList = g.document.elements['vePendingList'];
    var activeList = g.document.elements['veActiveList'];
    assertIncludes(pendingList.innerHTML, '\u7269\u7406\u5458\u5de5', 'Pending list should display physical employee label');
    assertIncludes(activeList.innerHTML, '\u7269\u7406\u5458\u5de5', 'Active list should display physical employee label');
    assertIncludes(activeList.innerHTML, '\u865a\u62df\u5458\u5de5', 'Active list should prefer platform fields when inferring virtual employee label');
    assertIncludes(activeList.innerHTML, 'Test VE 4', 'Active list should contain runtime-provider digital employee');
    assertIncludes(activeList.innerHTML, 'Test VE 5', 'Active list should contain non-maclawsrv runtime employee');
    assertEqual(countOccurrences(activeList.innerHTML, '\u865a\u62df\u5458\u5de5'), 2, 'Active list should display platform and maclawsrv runtime employees as virtual');
    assertEqual(countOccurrences(activeList.innerHTML, '\u7269\u7406\u5458\u5de5'), 2, 'Active list should display GUI and non-maclawsrv runtime employees as physical');
    assertIncludes(activeList.innerHTML, 'var(--ve-list-grid', 'VE list should use responsive CSS grid variable');
  }

  // Test 3b: List displays visible departments
  {
    console.log('  Test: List displays visible departments and global visibility');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var activeList = g.document.elements['veActiveList'];
    assertIncludes(activeList.innerHTML, 'Legal', 'Active list should map visible group IDs to department names');
    assertIncludes(activeList.innerHTML, '\u5168\u5c40\u53ef\u89c1', 'Active list should show global visibility when no department is set');
    assertIncludes(activeList.innerHTML, 'veOpenVisibilityEditor', 'Active rows should include visibility edit action');
  }

  // Test 3c: Visibility save calls new endpoint
  {
    console.log('  Test: Visibility editor saves selected departments');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veVisibilityOverlay'] = {
      querySelectorAll: function() {
        return [{ value: 'dept-legal' }, { value: 'dept-contract' }];
      },
      classList: { add: function() {}, remove: function() {} }
    };
    g._apiCalls.length = 0;
    await ctx.veSaveVisibility('ve-002');
    var saveCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/ve-002/visibility'; });
    assert(saveCall !== undefined, 'Should call visibility API');
    assertEqual(saveCall.opts.method, 'PUT', 'Visibility save should use PUT');
    var body = JSON.parse(saveCall.opts.body);
    assertEqual(body.visible_group_ids.join(','), 'dept-legal,dept-contract', 'Visibility save should send selected department IDs');
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
    assertIncludes(quotaInfo.textContent, '4', 'Quota info should show active count');
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

  // Test 8a: Resident toggle calls correct API
  {
    console.log('  Test: Resident toggle calls PUT /api/ve/{id}/resident');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g._apiCalls.length = 0;
    await ctx.veSetResident('ve-002', true);
    var residentCall = g._apiCalls.find(function(c) { return c.url.includes('/resident'); });
    assert(residentCall !== undefined, 'Should call resident API');
    assertEqual(residentCall.url, '/api/ve/ve-002/resident', 'Should call correct resident URL');
    assertEqual(residentCall.opts.method, 'PUT', 'Should use PUT method');
    assertEqual(residentCall.opts.body, JSON.stringify({ resident: true }), 'Should include resident flag in body');
  }

  // Test 9: Approve refreshes list
  {
    console.log('  Test: Approve refreshes digital employee list after success');
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
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
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
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
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
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
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
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
    assert(configCall !== undefined, 'Should accept value 1');
    var body = JSON.parse(configCall.opts.body);
    assertEqual(body.max_group_participants, 1, 'Should send value 1');

    g.document.elements['veMaxParticipantsInput'] = { value: '10' };
    g._apiCalls.length = 0;
    await ctx.veSaveGroupConfig();
    configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
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

  // Test 17a: Auto approve state renders from config
  {
    console.log('  Test: Auto approve checkbox reflects loaded config');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    var checkbox = g.document.elements['veAutoApproveInput'];
    var badge = g.document.elements['veAutoApproveBadge'];
    assertEqual(checkbox.checked, true, 'Auto approve checkbox should be checked from group config');
    assertIncludes(badge.textContent, '\u5df2\u5f00\u542f', 'Auto approve badge should show enabled state');
  }

  // Test 17b: Auto approve toggle saves alongside group config
  {
    console.log('  Test: Auto approve toggle saves group config');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veAutoApproveInput'] = { checked: false };
    g.document.elements['veMaxParticipantsInput'] = { value: '6' };
    g._apiCalls.length = 0;
    await ctx.veSaveAutoApproveConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
    assert(configCall !== undefined, 'Should call config API when toggling auto approve');
    assertEqual(configCall.opts.method, 'PUT', 'Auto approve save should use PUT');
    var body = JSON.parse(configCall.opts.body);
    assertEqual(body.max_group_participants, 6, 'Auto approve save should preserve max participants');
    assertEqual(body.auto_approve, false, 'Auto approve save should send checkbox state');
  }

  // Test 17c: Group config save preserves server auto approve before list load
  {
    console.log('  Test: Group config save preserves server auto approve before list load');
    var g = createMockGlobal();
    g.api = async function(url, opts) {
      g._apiCalls.push({ url: url, opts: opts || {} });
      if (url === '/api/ve/config' && (!opts || !opts.method)) return { max_group_participants: 9, auto_approve: true };
      return {};
    };
    var ctx = loadVETab(g);
    g.document.elements['veMaxParticipantsInput'] = { value: '7' };
    await ctx.veSaveGroupConfig();
    var saveCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
    var body = JSON.parse(saveCall.opts.body);
    assertEqual(body.max_group_participants, 7, 'Should save requested max participants');
    assertEqual(body.auto_approve, true, 'Should preserve server auto approve state before list load');
  }

  // Test 17d: Enabling auto approve refreshes list after save
  {
    console.log('  Test: Enabling auto approve refreshes digital employee list');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    g.document.elements['veAutoApproveInput'] = { checked: true };
    g.document.elements['veMaxParticipantsInput'] = { value: '5' };
    g._apiCalls.length = 0;
    await ctx.veSaveAutoApproveConfig();
    var listCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/list'; });
    assert(listCall !== undefined, 'Should refresh list after enabling auto approve because pending rows may become active');
  }

  // Test 17e: Auto approve toggle rejects invalid group config
  {
    console.log('  Test: Auto approve toggle rejects invalid group config');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    g.document.elements['veAutoApproveInput'] = { checked: false };
    g.document.elements['veMaxParticipantsInput'] = { value: '11' };
    g._apiCalls.length = 0;
    g._toasts.length = 0;
    await ctx.veSaveAutoApproveConfig();
    var configCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/config' && c.opts.method === 'PUT'; });
    var errorToast = g._toasts.find(function(t) { return t.type === 'error'; });
    assertEqual(configCall, undefined, 'Should not save auto approve when max participants is invalid');
    assert(errorToast !== undefined, 'Should show error toast for invalid max participants');
  }

  // Test 17f: Saving group config refreshes list when auto approve is active
  {
    console.log('  Test: Saving group config refreshes list when auto approve is active');
    var g = createMockGlobal();
    var ctx = loadVETab(g);
    await ctx.loadVEList();
    g.document.elements['veMaxParticipantsInput'] = { value: '8' };
    g._apiCalls.length = 0;
    await ctx.veSaveGroupConfig();
    var listCall = g._apiCalls.find(function(c) { return c.url === '/api/ve/list'; });
    assert(listCall !== undefined, 'Should refresh list after group config save while auto approve is enabled');
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


  // ============================================================
  // TEST SUITE: History Search and Preview
  // ============================================================
  console.log('\n--- Test Suite: History Search and Preview ---');

  // Test 21: History search loads matching sessions
  {
    console.log('  Test: History search loads matching sessions by owner email');
    var g = createMockGlobal();
    g.api = async function(url) {
      g._apiCalls.push({ url: url, opts: {} });
      if (url === '/api/ve/list') return { employees: [], group_config: { max_group_participants: 5 }, quota: 10, active_count: 0 };
      if (url.indexOf('/api/ve/history/search') === 0) {
        return { matches: [{ employee: { id: 've-001', name: 'Legal Researcher', owner_email: 'owner@example.com' }, discussions: [{ id: 'disc-1', topic: 'Contract review', status: 'open', updated_at: '2026-01-01T00:00:00Z', participant_ids: ['human-a', 'machine-a'] }] }] };
      }
      return {};
    };
    var ctx = loadVETab(g);
    g.document.elements['veHistorySearchInput'] = { value: 'owner@example.com', classList: { add: function() {}, remove: function() {} } };
    await ctx.veLoadHistorySearch('owner@example.com');
    var list = g.document.elements['veHistoryDialogOverlay'];
    assertIncludes(list.innerHTML, 'Contract review', 'History dialog should contain discussion title');
    assertIncludes(list.innerHTML, 'role="dialog"', 'History dialog should expose dialog role');
    assertIncludes(list.innerHTML, 'aria-modal="true"', 'History dialog should be modal for assistive tech');
    assertIncludes(list.innerHTML, 'aria-labelledby="veHistoryDialogTitle"', 'History dialog should label itself from title');
    assertIncludes(list.innerHTML, 'Legal Researcher / owner@example.com', 'History dialog should show selected digital employee label');
    assertIncludes(list.innerHTML, 'vePreviewHistory(&quot;disc-1&quot;)', 'History dialog should include an attribute-safe preview action');
  }

  // Test 21a: History list escapes merged employee labels
  {
    console.log('  Test: History list escapes merged employee labels');
    var g = createMockGlobal();
    g.api = async function(url) {
      if (url.indexOf('/api/ve/history/search') === 0) {
        return { matches: [{ employee: { id: 've-x', name: '<img src=x onerror=alert(1)>', owner_email: 'owner@example.com' }, discussions: [{ id: 'disc-x', topic: 'Safe topic', status: 'open', updated_at: '2026-01-01T00:00:00Z' }] }] };
      }
      return {};
    };
    var ctx = loadVETab(g);
    await ctx.veLoadHistorySearch('owner@example.com');
    var list = g.document.elements['veHistoryDialogOverlay'];
    assertIncludes(list.innerHTML, '&lt;img src=x onerror=alert(1)&gt; / owner@example.com', 'History dialog should render escaped employee label text');
    assertNotIncludes(list.innerHTML, '<img src=x onerror=alert(1)>', 'History dialog should not inject employee label HTML');
  }

  // Test 21b: History search caps merged discussion list
  {
    console.log('  Test: History search caps merged discussion list to requested limit');
    var g = createMockGlobal();
    g.api = async function(url) {
      g._apiCalls.push({ url: url, opts: {} });
      if (url.indexOf('/api/ve/history/search') === 0) {
        var discussionsA = [];
        var discussionsB = [];
        for (var i = 1; i <= 15; i++) discussionsA.push({ id: 'a-' + i, topic: 'Alpha ' + String(i).padStart(2, '0'), status: 'open', updated_at: '2026-01-' + String(i).padStart(2, '0') + 'T00:00:00Z' });
        for (var j = 1; j <= 15; j++) discussionsB.push({ id: 'b-' + j, topic: 'Beta ' + String(j).padStart(2, '0'), status: 'open', updated_at: '2026-02-' + String(j).padStart(2, '0') + 'T00:00:00Z' });
        return { matches: [
          { employee: { id: 've-a', name: 'Alpha Analyst', owner_email: 'alpha@example.com' }, discussions: discussionsA },
          { employee: { id: 've-b', name: 'Beta Analyst', owner_email: 'beta@example.com' }, discussions: discussionsB }
        ] };
      }
      return {};
    };
    var ctx = loadVETab(g);
    await ctx.veLoadHistorySearch('example.com');
    var list = g.document.elements['veHistoryDialogOverlay'];
    var count = (list.innerHTML.match(/vePreviewHistory/g) || []).length;
    assertEqual(count, 20, 'History search should render at most 20 merged discussions');
    assertIncludes(list.innerHTML, 'Beta 15', 'Newest discussions should be kept after capping');
    assertNotIncludes(list.innerHTML, 'Alpha 01', 'Oldest overflow discussions should be omitted after capping');
  }
  // Test 22: Preview renders safe attachment download links
  {
    console.log('  Test: Preview renders only safe same-origin attachment download links');
    var g = createMockGlobal();
    g.location = { origin: 'https://hub.example' };
    g.api = async function(url) {
      g._apiCalls.push({ url: url, opts: {} });
      if (url.indexOf('/api/ve/history/disc-1/detail') === 0) {
        return {
          discussion: { id: 'disc-1', topic: 'Contract review', status: 'open', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-02T00:00:00Z', participant_ids: ['human-a', 'machine-a'] },
          messages: [{ from_id: 'machine-a', content: 'See attachments.', created_at: '2026-01-02T00:00:00Z', text_attachments: [
            { content: 'cmV2aWV3IG5vdGVz', filename: 'notes.txt', mime_type: 'text/plain' },
            { content: 'dXJsLXNhZmU', filename: 'urlsafe.txt', mime_type: 'text/plain' }
          ], file_attachments: [
            { file_url: 'https://hub.example/api/ve/files/doc-1', filename: 'safe.pdf', mime_type: 'application/pdf', size_bytes: 123 },
            { file_url: 'https://hub.example/api/ve/files/download/doc-4', filename: 'download.pdf', mime_type: 'application/pdf', size_bytes: 234 },
            { file_url: 'https://hub.example/api/ve/files/upload/doc-3', filename: 'upload.pdf', mime_type: 'application/pdf', size_bytes: 789 },
            { file_url: 'https://hub.example/api/ve/files/download/doc-5/extra', filename: 'nested.pdf', mime_type: 'application/pdf', size_bytes: 111 },
            { file_url: 'https://hub.example/api/ve/files/doc%2F6', filename: 'encoded-slash.pdf', mime_type: 'application/pdf', size_bytes: 222 },
            { file_url: 'https://evil.example/api/ve/files/doc-2', filename: 'evil.pdf', mime_type: 'application/pdf', size_bytes: 456 }
          ] }]
        };
      }
      return {};
    };
    var ctx = loadVETab(g);
    await ctx.vePreviewHistory('disc-1');
    var overlay = g.document.elements['veHistoryPreviewOverlay'];
    assertIncludes(overlay.innerHTML, 'notes.txt', 'Preview should show inline text attachment label');
    assertIncludes(overlay.innerHTML, 'role="dialog"', 'Preview should expose dialog role');
    assertIncludes(overlay.innerHTML, 'aria-labelledby="veHistoryPreviewTitle"', 'Preview should label itself from title');
    assertIncludes(overlay.innerHTML, 'data:text%2Fplain;base64,cmV2aWV3IG5vdGVz', 'Preview should expose inline text attachment as downloadable data URL');
    assertIncludes(overlay.innerHTML, 'data:text%2Fplain;base64,dXJsLXNhZmU=', 'Preview should normalize URL-safe inline text attachment data URL');
    assertIncludes(overlay.innerHTML, 'safe.pdf', 'Preview should show safe attachment label');
    assertIncludes(overlay.innerHTML, '/api/ve/history/disc-1/attachments/doc-1', 'Preview should use admin attachment download URL');
    assertIncludes(overlay.innerHTML, 'veDownloadHistoryAttachment(&quot;/api/ve/history/disc-1/attachments/doc-1&quot;', 'Preview attachment download action should be attribute-safe');
    assertIncludes(overlay.innerHTML, '/api/ve/history/disc-1/attachments/doc-4', 'Preview should use admin download URL for canonical download paths');
    assertIncludes(overlay.innerHTML, 'upload.pdf', 'Preview should show upload-path attachment label for audit context');
    assertNotIncludes(overlay.innerHTML, '/api/ve/history/disc-1/attachments/doc-3', 'Preview should not transform upload endpoints into admin download links');
    assertIncludes(overlay.innerHTML, 'nested.pdf', 'Preview should show nested-path attachment label for audit context');
    assertNotIncludes(overlay.innerHTML, '/api/ve/history/disc-1/attachments/extra', 'Preview should not collapse nested file paths into an attachment id');
    assertIncludes(overlay.innerHTML, 'encoded-slash.pdf', 'Preview should show encoded-slash attachment label for audit context');
    assertNotIncludes(overlay.innerHTML, '/api/ve/history/disc-1/attachments/doc%2F6', 'Preview should not link encoded slash file ids');
    assertIncludes(overlay.innerHTML, 'evil.pdf', 'Preview should show external attachment label for audit context');
    assertNotIncludes(overlay.innerHTML, 'https://evil.example/api/ve/files/doc-2', 'Preview should not link external attachment URLs');
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
