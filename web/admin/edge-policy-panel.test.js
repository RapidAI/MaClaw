/*
 * Platform Bot-node policy panel.
 * Run with: node hub/web/admin/edge-policy-panel.test.js
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

function assertEqual(actual, expected, message) {
  if (actual === expected) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', message, '| got:', JSON.stringify(actual), 'want:', JSON.stringify(expected));
  }
}

function createHarness(opts) {
  opts = opts || {};
  const elements = {};
  const calls = [];
  const toasts = [];
  function createElement(id) {
    const el = {
      id: id,
      hidden: id === 'tenantEdgePolicy',
      value: '',
      checked: false,
      disabled: false,
      textContent: '',
      _html: '',
      listeners: {},
      classList: {
        names: {},
        toggle: function (name, on) { this.names[name] = !!on; },
        contains: function (name) { return !!this.names[name]; }
      },
      addEventListener: function (type, fn) {
        (this.listeners[type] || (this.listeners[type] = [])).push(fn);
      }
    };
    if (id === 'tenantEdgePolicy') el.classList.names.hidden = true;
    Object.defineProperty(el, 'innerHTML', {
      configurable: true,
      get: function () { return this._html; },
      set: function (html) { this._html = String(html || ''); }
    });
    elements[id] = el;
    return el;
  }
  ['tenantEdgePolicy', 'tenantEdgePolicyTitle', 'tenantEdgePolicyDesc', 'tenantEdgeNodeList',
    'tenantEdgeTenant', 'tenantEdgeTenantLabel', 'tenantEdgeAllow', 'tenantEdgeAllowLabel',
    'tenantEdgeMaxNodes', 'tenantEdgeMaxNodesLabel', 'tenantEdgeMaxStreams', 'tenantEdgeMaxStreamsLabel',
    'tenantEdgeMaxViews', 'tenantEdgeMaxViewsLabel', 'tenantEdgeSave'].forEach(createElement);

  let nodeLoads = 0;
  function api(url, request) {
    calls.push({ path: url, body: request && request.body, method: request && request.method });
    if (url === '/api/admin/platform/edge-nodes') {
      nodeLoads += 1;
      if (nodeLoads === 1 && opts.holdFirstNodes) {
        return opts.holdFirstNodes.then(function () {
          return { nodes: [{ id: 'n1', tenant_id: 'acme', name: 'lab', online: true, has_desktopd: true, version: 'v9' }] };
        });
      }
      if (opts.failLaterNodes && nodeLoads > 1) return Promise.reject(new Error(opts.failLaterMessage || 'edge nodes are disabled'));
    }
    if (opts.failNodes && url === '/api/admin/platform/edge-nodes') return Promise.reject(new Error('edge disabled'));
    if (url === '/api/admin/platform/edge-nodes') {
      return Promise.resolve({
        nodes: [{
          id: 'n1', tenant_id: 'acme', name: 'lab', online: true, has_desktopd: true, has_srv: false, version: 'v9',
          desktopd_key: 'SECRET-KEY', secret_hash: 'SECRET-HASH', hub_token_hash: 'SECRET-TOKEN', srv_admin_secret: 'SECRET-ADMIN'
        }]
      });
    }
    if (url === '/api/admin/tenants') {
      return Promise.resolve({ tenants: [{ id: 'acme', name: 'Acme' }, { id: 'beta', name: 'Beta' }] });
    }
    if (url.indexOf('/edge-policy') > 0 && request && request.method === 'PUT') {
      if (opts.holdPut) return opts.holdPut.then(function () { return {}; });
      return Promise.resolve({});
    }
    if (url.indexOf('/tenants/acme/edge-policy') > 0 && opts.holdAcme && !(request && request.method === 'PUT')) {
      return opts.holdAcme.then(function () {
        return { allow_self: true, max_nodes: 9, max_streams: 9, max_views: 9 };
      });
    }
    if (url.indexOf('/tenants/beta/edge-policy') > 0) {
      return Promise.resolve({ allow_self: false, max_nodes: 2, max_streams: 4, max_views: 1 });
    }
    if (url.indexOf('/edge-policy') > 0) {
      return Promise.resolve({ allow_self: true, max_nodes: 0, max_streams: 0, max_views: 0 });
    }
    return Promise.reject(new Error('unexpected ' + url));
  }

  const languageListeners = [];
  const document = { getElementById: function (id) { return elements[id] || null; } };
  const context = vm.createContext({
    console: console,
    Promise: Promise,
    document: document,
    currentLang: 'zh'
  });
  context.window = context;
  context.api = api;
  context.adminProfile = function () { return opts.profile || { scope: 'global' }; };
  context.showToast = function (msg, kind) { toasts.push({ msg: msg, kind: kind }); };
  context.AdminTabRegistry = { onLanguageChange: function (fn) { languageListeners.push(fn); } };
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'edge-policy-panel.js'), 'utf8'), context, { filename: 'edge-policy-panel.js' });

  function fire(el, type) {
    (el.listeners[type] || []).forEach(function (fn) { fn({}); });
  }

  function setLang(lang) {
    context.currentLang = lang;
    languageListeners.forEach(function (fn) { fn(); });
  }

  return { elements: elements, calls: calls, toasts: toasts, fire: fire, load: context.loadTenantEdgePolicy, setLang: setLang };
}

async function testHiddenWhenEdgeIsOff() {
  console.log('  Test: the platform block stays hidden when edge nodes are off');
  const harness = createHarness({ failNodes: true });
  await harness.load();
  assertEqual(harness.elements.tenantEdgePolicy.hidden, true, 'the block stays hidden');
  assert(harness.elements.tenantEdgePolicy.classList.contains('hidden'), 'the item class stays hidden');
}

async function testHiddenForATenantAdmin() {
  console.log('  Test: a tenant admin does not see platform node limits');
  const harness = createHarness({ profile: { scope: 'tenant', tenant_id: 'acme' } });
  await harness.load();
  assertEqual(harness.elements.tenantEdgePolicy.hidden, true, 'the block stays hidden');
  assertEqual(harness.calls.length, 0, 'the platform API is not called');
}

async function testListsMetadataAndEditsOneTenant() {
  console.log('  Test: a global admin sees node status and saves that tenant limit');
  const harness = createHarness();
  await harness.load();
  assertEqual(harness.elements.tenantEdgePolicy.hidden, false, 'the block is shown');
  const html = harness.elements.tenantEdgeNodeList.innerHTML;
  assert(html.indexOf('acme') >= 0 && html.indexOf('lab') >= 0, 'tenant and name are listed');
  assert(html.indexOf('\u5728\u7ebf') >= 0 && html.indexOf('desktopd') >= 0 && html.indexOf('v9') >= 0, 'status, capability, and version are listed');
  assert(html.indexOf('SECRET-KEY') < 0 && html.indexOf('SECRET-HASH') < 0 && html.indexOf('SECRET-TOKEN') < 0 && html.indexOf('SECRET-ADMIN') < 0, 'secrets stay off the page');
  assertEqual(harness.elements.tenantEdgeAllow.checked, true, 'a missing cap still allows nodes');
  assertEqual(harness.elements.tenantEdgeMaxNodes.value, '0', 'zero is the unlimited cap');

  harness.elements.tenantEdgeTenant.value = 'beta';
  harness.fire(harness.elements.tenantEdgeTenant, 'change');
  await Promise.resolve();
  await Promise.resolve();
  assertEqual(harness.elements.tenantEdgeAllow.checked, false, 'the selected tenant can be closed');
  assertEqual(harness.elements.tenantEdgeMaxNodes.value, '2', 'the node cap is shown');
  assertEqual(harness.elements.tenantEdgeMaxStreams.value, '4', 'the tunnel cap is shown');
  assertEqual(harness.elements.tenantEdgeMaxViews.value, '1', 'the view cap is shown');

  harness.elements.tenantEdgeMaxNodes.value = '-1';
  harness.fire(harness.elements.tenantEdgeSave, 'click');
  await Promise.resolve();
  assert(harness.calls.every(function (call) { return call.method !== 'PUT'; }), 'a negative cap is not saved');
  harness.elements.tenantEdgeMaxNodes.value = '10000000000';
  harness.fire(harness.elements.tenantEdgeSave, 'click');
  await Promise.resolve();
  assert(harness.calls.every(function (call) { return call.method !== 'PUT'; }), 'a cap that does not fit in an integer is not saved');

  harness.elements.tenantEdgeAllow.checked = true;
  harness.elements.tenantEdgeMaxNodes.value = '3';
  harness.elements.tenantEdgeMaxStreams.value = '0';
  harness.elements.tenantEdgeMaxViews.value = '1';
  harness.fire(harness.elements.tenantEdgeSave, 'click');
  await Promise.resolve();
  await Promise.resolve();
  const put = harness.calls.filter(function (call) { return call.method === 'PUT'; }).pop();
  assert(put && put.path.indexOf('/tenants/beta/edge-policy') > 0, 'save addresses the selected tenant');
  assertEqual(put && put.body, JSON.stringify({ allow_self: true, max_nodes: 3, max_streams: 0, max_views: 1 }), 'save sends the caps');
  assert(!harness.elements.tenantEdgePolicy.classList.contains('hidden'), 'a shown block is not forced hidden by the item layout');

  harness.setLang('en');
  assertEqual(harness.elements.tenantEdgePolicyTitle.textContent, 'Tenant node limits', 'the title follows the language');
  assert(harness.elements.tenantEdgeNodeList.innerHTML.indexOf('Online') >= 0, 'status follows the language');
}

async function testRefreshKeepsAnUnsavedCap() {
  console.log('  Test: reloading the tenant list keeps a cap that is not saved yet');
  const harness = createHarness();
  await harness.load();
  harness.elements.tenantEdgeMaxNodes.value = '7';
  const policies = function () {
    return harness.calls.filter(function (call) { return call.path.indexOf('/edge-policy') > 0 && call.method !== 'PUT'; }).length;
  };
  const before = policies();
  await harness.load();
  assertEqual(harness.elements.tenantEdgeMaxNodes.value, '7', 'the unsaved cap stays');
  assertEqual(policies(), before, 'the refresh does not replace it from the server');
}

async function flush() {
  for (let i = 0; i < 8; i += 1) await Promise.resolve();
}

async function testALateReplyDoesNotReplaceTheTenantOnScreen() {
  console.log('  Test: a slow reply does not overwrite the tenant now on screen');
  let releaseAcme;
  const holdAcme = new Promise(function (resolve) { releaseAcme = resolve; });
  const first = createHarness({ holdAcme: holdAcme });
  const opening = first.load();
  await flush();
  first.elements.tenantEdgeTenant.value = 'beta';
  first.fire(first.elements.tenantEdgeTenant, 'change');
  await flush();
  assertEqual(first.elements.tenantEdgeMaxNodes.value, '2', 'the newly selected tenant is shown');
  releaseAcme();
  await opening;
  assertEqual(first.elements.tenantEdgeMaxNodes.value, '2', 'the earlier tenant reply is ignored');
  assertEqual(first.elements.tenantEdgeAllow.checked, false, 'the closed switch stays closed');

  let releaseDuringSave;
  const duringSave = new Promise(function (resolve) { releaseDuringSave = resolve; });
  const editing = createHarness({ holdAcme: duringSave });
  const pending = editing.load();
  await flush();
  editing.elements.tenantEdgeAllow.checked = false;
  editing.elements.tenantEdgeMaxNodes.value = '3';
  editing.fire(editing.elements.tenantEdgeSave, 'click');
  assert(!editing.calls.some(function (call) { return call.method === 'PUT'; }), 'save waits until this tenant policy is on screen');
  releaseDuringSave();
  await pending;
  await flush();
  assertEqual(editing.elements.tenantEdgeMaxNodes.value, '9', 'the loaded cap replaces the typed value');
  assertEqual(editing.elements.tenantEdgeAllow.checked, true, 'the loaded switch replaces the unchecked default');

  let releaseNodes;
  const holdFirstNodes = new Promise(function (resolve) { releaseNodes = resolve; });
  const page = createHarness({ holdFirstNodes: holdFirstNodes, failLaterNodes: true });
  const slow = page.load();
  await flush();
  const failed = page.load();
  await failed;
  assertEqual(page.elements.tenantEdgePolicy.hidden, true, 'a later disabled reply hides the block');
  releaseNodes();
  await slow;
  assertEqual(page.elements.tenantEdgePolicy.hidden, true, 'the earlier list does not open the block again');
  assert(page.elements.tenantEdgePolicy.classList.contains('hidden'), 'the item class stays hidden');

  const open = createHarness({ failLaterNodes: true, failLaterMessage: 'network down' });
  await open.load();
  assertEqual(open.elements.tenantEdgePolicy.hidden, false, 'the block is open before the failed refresh');
  await open.load();
  assertEqual(open.elements.tenantEdgePolicy.hidden, false, 'a failed refresh leaves the block open');
  assert(!open.elements.tenantEdgePolicy.classList.contains('hidden'), 'the item class stays visible');
  assert(open.elements.tenantEdgeNodeList.innerHTML.indexOf('lab') >= 0, 'the node list stays');
  assert(open.toasts.some(function (item) { return item.msg === 'network down'; }), 'the refresh error is shown');
}

async function testSaveDoesNotTreatALaterEditAsStored() {
  console.log('  Test: an edit made while saving stays unsaved');
  let releasePut;
  const holdPut = new Promise(function (resolve) { releasePut = resolve; });
  const harness = createHarness({ holdPut: holdPut });
  await harness.load();
  harness.elements.tenantEdgeMaxNodes.value = '3';
  harness.fire(harness.elements.tenantEdgeSave, 'click');
  await flush();
  assertEqual(harness.elements.tenantEdgeMaxNodes.disabled, true, 'the cap cannot change while saving');
  assertEqual(harness.elements.tenantEdgeTenant.disabled, true, 'the tenant cannot change while saving');
  assertEqual(harness.elements.tenantEdgeSave.disabled, true, 'save stays disabled while the request is open');
  const policyReads = function () {
    return harness.calls.filter(function (call) { return call.path.indexOf('/edge-policy') > 0 && call.method !== 'PUT'; }).length;
  };
  const reads = policyReads();
  harness.elements.tenantEdgeTenant.value = 'beta';
  harness.fire(harness.elements.tenantEdgeTenant, 'change');
  assertEqual(policyReads(), reads, 'a locked tenant switch does not load another policy');
  harness.elements.tenantEdgeTenant.value = 'acme';
  harness.elements.tenantEdgeMaxNodes.value = '8';
  releasePut();
  await flush();
  assertEqual(harness.elements.tenantEdgeMaxNodes.disabled, false, 'the cap can be edited again');
  assertEqual(harness.elements.tenantEdgeMaxNodes.value, '8', 'the later cap stays on screen');
  assertEqual(harness.elements.tenantEdgeSave.disabled, false, 'save is available for the unsaved cap');
  assert(!harness.toasts.some(function (item) { return item.msg === '\u5df2\u4fdd\u5b58'; }), 'the later cap is not reported as saved');
  const put = harness.calls.filter(function (call) { return call.method === 'PUT'; }).pop();
  assertEqual(put && put.body, JSON.stringify({ allow_self: true, max_nodes: 3, max_streams: 0, max_views: 0 }), 'the request kept the cap from the click');
  await harness.load();
  assertEqual(harness.elements.tenantEdgeMaxNodes.value, '8', 'a refresh does not replace the unsaved cap');
}

Promise.resolve()
  .then(testHiddenWhenEdgeIsOff)
  .then(testHiddenForATenantAdmin)
  .then(testListsMetadataAndEditsOneTenant)
  .then(testRefreshKeepsAnUnsavedCap)
  .then(testALateReplyDoesNotReplaceTheTenantOnScreen)
  .then(testSaveDoesNotTreatALaterEditAsStored)
  .then(function () {
    console.log('edge-policy panel tests: ' + passed + ' passed, ' + failed + ' failed');
    if (failed) process.exit(1);
  })
  .catch(function (err) {
    console.error(err);
    process.exit(1);
  });
