/*
 * Desktop tab VNC viewer tests.
 * Run with: node hub/web/admin/desktop-tab.test.js
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

function assertIncludes(haystack, needle, message) {
  if (String(haystack || '').indexOf(needle) !== -1) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', message, '| missing:', JSON.stringify(needle));
  }
}

async function flush() {
  for (let i = 0; i < 40; i += 1) {
    await new Promise(function (resolve) { setImmediate(resolve); });
  }
}

const HANDOFF = '/api/v1/desktop-handoff/abc123/vnc.html?autoconnect=1&path=api%2Fv1%2Fdesktop-handoff%2Fabc123%2Fwebsockify';

function createHarness(options) {
  const opts = options || {};
  const elements = Object.create(null);
  const created = [];
  const calls = [];
  const toasts = [];
  const keyHandlers = [];

  function classList(el) {
    const set = [];
    return {
      add: function (name) { if (set.indexOf(name) === -1) set.push(name); },
      remove: function (name) {
        const at = set.indexOf(name);
        if (at >= 0) set.splice(at, 1);
      },
      contains: function (name) { return set.indexOf(name) !== -1; },
      values: set
    };
  }

  function registerIds(html, parent) {
    const re = /id="([^"]+)"/g;
    let match;
    while ((match = re.exec(String(html || '')))) {
      if (!elements[match[1]]) {
        elements[match[1]] = createElement('div');
        elements[match[1]].parentElement = parent;
      }
    }
  }

  function createElement(tag, id) {
    const el = {
      tagName: String(tag || 'div').toUpperCase(),
      id: id || '',
      dataset: {},
      style: { cssText: '' },
      classList: classList(),
      className: '',
      value: '',
      disabled: false,
      hidden: false,
      href: '',
      src: '',
      textContent: '',
      parentElement: null,
      attrs: {},
      listeners: {},
      childNodes: [],
      options: [],
      selectedIndex: -1,
      setAttribute: function (name, value) { this.attrs[name] = String(value); },
      getAttribute: function (name) {
        return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null;
      },
      removeAttribute: function (name) { delete this.attrs[name]; },
      addEventListener: function (type, fn) {
        (this.listeners[type] || (this.listeners[type] = [])).push(fn);
      },
      appendChild: function (child) {
        this.childNodes.push(child);
        child.parentElement = this;
        if (child.id) elements[child.id] = child;
        return child;
      },
      querySelectorAll: function (selector) {
        this._queries = this._queries || {};
        if (this._queries[selector]) return this._queries[selector];
        const name = /^\[([A-Za-z0-9_-]+)\]$/.exec(String(selector || ''));
        if (!name) return [];
        const attr = name[1];
        const re = new RegExp(attr + '="([^"]*)"', 'g');
        const html = this._html || '';
        const out = [];
        let match;
        while ((match = re.exec(html))) {
          const button = createElement('button');
          button.setAttribute(attr, match[1]);
          out.push(button);
        }
        this._queries[selector] = out;
        return out;
      },
      focus: function () {}
    };
    Object.defineProperty(el, 'innerHTML', {
      configurable: true,
      get: function () { return this._html || ''; },
      set: function (html) {
        this._html = String(html || '');
        this._queries = {};
        this.childNodes = [];
        registerIds(this._html, this);
      }
    });
    created.push(el);
    return el;
  }

  elements['botDesktopHost'] = createElement('div', 'botDesktopHost');
  elements['botDesktopHost'].dataset = {};

  const document = {
    readyState: 'complete',
    activeElement: null,
    body: createElement('body', 'body'),
    createElement: function (tag) { return createElement(tag); },
    getElementById: function (id) { return elements[id] || null; },
    addEventListener: function (type, fn) {
      if (type === 'keydown') keyHandlers.push(fn);
    },
    removeEventListener: function (type, fn) {
      if (type !== 'keydown') return;
      const at = keyHandlers.indexOf(fn);
      if (at >= 0) keyHandlers.splice(at, 1);
    }
  };

  let releaseView = null;
  const viewGate = opts.holdView
    ? new Promise(function (resolve) { releaseView = resolve; })
    : Promise.resolve();

  function api(url, request) {
    calls.push({ path: url, body: request && request.body, method: request && request.method });
    if (url === '/api/admin/desktop-services') {
      return Promise.resolve({
        servers: opts.servers || [{ id: 's1', name: 'dockerd-a', base_url: 'http://docker-host:18081', token_set: true, image: 'maclaw-gui:1', memory: '2500m', cpus: '1.5', shm_size: '512m' }],
        assignments: [{ id: 'a1', scope: 'global', target_id: '', server_id: 's1' }],
        users: opts.viewUsers === undefined
          ? [{ id: 'u_alice', label: 'alice@example.com' }, { id: 'u_bob', label: 'bob@example.com' }]
          : opts.viewUsers
      });
    }
    if (url === '/api/admin/bots/nodes') {
      if (!opts.nodes) return Promise.reject(new Error('edge disabled'));
      return Promise.resolve({ nodes: opts.nodes });
    }
    if (url.indexOf('/api/admin/bots/nodes/') === 0 && request && request.method === 'DELETE') {
      return Promise.resolve({ deleted: true });
    }
    if (url === '/api/admin/desktop-services/desktops/view') {
      if (opts.viewError) return Promise.reject(new Error(opts.viewError));
      return viewGate.then(function () { return { novnc_url: opts.novncURL === undefined ? HANDOFF : opts.novncURL }; });
    }
    return Promise.reject(new Error('unexpected ' + url));
  }

  const context = vm.createContext({
    console: console,
    setTimeout: setTimeout,
    clearTimeout: clearTimeout,
    Promise: Promise,
    URL: URL,
    document: document
  });
  context.window = context;
  context.currentLang = 'zh';
  context.api = api;
  context.showToast = function (msg, kind) { toasts.push({ msg: msg, kind: kind }); };
  context.botOrgChoices = function () {
    return {
      ready: true,
      departments: [{ id: 'dept-finance', label: 'Finance' }],
      users: [{ id: 'u_alice', label: 'Alice' }, { id: 'u_stranger', label: 'Stranger' }]
    };
  };
  context.AdminTabRegistry = { onLanguageChange: function () {} };

  vm.runInContext(fs.readFileSync(path.join(__dirname, 'desktop-tab.js'), 'utf8'), context, { filename: 'desktop-tab.js' });

  function fire(el, type, event) {
    (el.listeners[type] || []).forEach(function (fn) { fn(event || {}); });
  }

  function pressKey(key) {
    keyHandlers.slice().forEach(function (fn) { fn({ key: key }); });
  }

  function frames() {
    return created.filter(function (el) { return el.tagName === 'IFRAME'; });
  }

  function overlay() { return elements.desktopVncOverlay || null; }

  function pickUser() {
    const select = elements.desktopUser;
    select.value = 'u_alice';
    select.selectedIndex = 1;
    select.options = [{ textContent: '请选择' }, { textContent: 'Alice' }];
  }

  return {
    context: context,
    elements: elements,
    calls: calls,
    toasts: toasts,
    created: created,
    mount: function () { context.mountBotDesktop(); },
    load: function () { return context.loadBotDesktop(); },
    fire: fire,
    pressKey: pressKey,
    frames: frames,
    overlay: overlay,
    pickUser: pickUser,
    releaseView: function (value) { releaseView(value); }
  };
}

async function testRelayHintShowsWhenTheDesktopIsOnAnotherNode() {
  console.log('  Test: assigning a desktop on another node warns about delay');
  const harness = createHarness({
    servers: [{ id: 's-node', name: 'lab', base_url: 'http://desktopd.n1.node.invalid', node_id: 'n1', token_set: true, image: 'maclaw-gui:1', memory: '3g', cpus: '1.5', shm_size: '1g' }],
    nodes: [{ id: 'n1', name: 'lab', has_desktopd: true, has_srv: false }]
  });
  harness.mount();
  await harness.load();
  assertIncludes(harness.elements.desktopRelayHint.textContent, '\u5ef6\u8fdf\u8f83\u9ad8', 'cross-node assignment warns');

  const local = createHarness({
    servers: [{ id: 's-node', name: 'lab', base_url: 'http://desktopd.n1.node.invalid', node_id: 'n1', token_set: true, image: 'maclaw-gui:1', memory: '3g', cpus: '1.5', shm_size: '1g' }],
    nodes: [{ id: 'n1', name: 'lab', has_desktopd: true, has_srv: true }]
  });
  local.mount();
  await local.load();
  assertIncludes(local.elements.desktopRelayHint.textContent, '\u5ef6\u8fdf\u8f83\u9ad8', 'everyone still crosses networks');
  local.elements.desktopScope.value = 'user';
  local.fire(local.elements.desktopScope, 'change');
  assertEqual(local.elements.desktopRelayHint.textContent, '', 'a user follows that node srv');
  local.elements.desktopScope.value = 'department';
  local.fire(local.elements.desktopScope, 'change');
  assertIncludes(local.elements.desktopRelayHint.textContent, '\u5ef6\u8fdf\u8f83\u9ad8', 'a department stays on the public connection');
}

async function testEditingANodeKeepsItsVirtualAddress() {
  console.log('  Test: editing a node service keeps its virtual address');
  const harness = createHarness({
    servers: [{ id: 's-node', name: 'lab', base_url: 'http://desktopd.n1.node.invalid', node_id: 'n1', token_set: true, image: 'maclaw-gui:1', memory: '3g', cpus: '1.5', shm_size: '1g' }]
  });
  harness.mount();
  await harness.load();
  const edit = harness.elements.desktopServerList.querySelectorAll('[data-desktop-edit]')[0];
  harness.fire(edit, 'click');
  harness.elements.desktopMemory.value = '4g';
  harness.fire(harness.elements.desktopAdd, 'click');
  await flush();
  const saved = harness.calls.filter(function (call) { return call.method === 'PATCH' && call.path === '/api/admin/desktop-services/s-node'; });
  assertEqual(saved.length, 1, 'a resource change is saved');
  const body = JSON.parse(saved[0].body);
  assertEqual(body.base_url, 'http://desktopd.n1.node.invalid', 'the node address stays');
  assertEqual(body.memory, '4g', 'the new memory is sent');
  const beforeUnit = harness.calls.length;
  harness.elements.desktopMemory.value = '4';
  harness.fire(harness.elements.desktopAdd, 'click');
  await flush();
  assertEqual(harness.calls.length, beforeUnit, 'memory without a unit is not saved');
  const unitToast = harness.toasts[harness.toasts.length - 1];
  assertEqual(unitToast && unitToast.kind, 'error', 'a bare memory value is rejected');
  assertIncludes(unitToast && unitToast.msg, '\u5185\u5b58', 'the operator is told to check the size');
  const beforeEmpty = harness.calls.length;
  harness.elements.desktopMemory.value = '';
  harness.fire(harness.elements.desktopAdd, 'click');
  await flush();
  assertEqual(harness.calls.length, beforeEmpty, 'empty memory is not saved');

  const before = harness.calls.length;
  harness.elements.desktopUrl.value = 'http://desktopd.other.node.invalid';
  harness.fire(harness.elements.desktopAdd, 'click');
  await flush();
  assertEqual(harness.calls.length, before, 'a different virtual host is not saved');
  harness.fire(harness.elements.desktopCancel, 'click');
  harness.elements.desktopName.value = 'public';
  harness.elements.desktopUrl.value = 'http://desktopd.abc.NODE.INVALID';
  harness.elements.desktopToken.value = 'secret';
  harness.fire(harness.elements.desktopAdd, 'click');
  await flush();
  assertEqual(harness.calls.length, before, 'a new service cannot use a virtual host');
}

async function testIntranetNodesStayHiddenUntilTheFeatureIsOn() {
  console.log('  Test: the intranet node block stays hidden when edge nodes are off');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  assertEqual(harness.elements.desktopNodeBox.hidden, true, 'intranet block is hidden');
}

async function testIntranetNodeListShowsStatusCapabilityAndVersion() {
  console.log('  Test: an intranet node shows status, capability, and version');
  const harness = createHarness({
    nodes: [{ id: 'n1', name: 'lab', online: true, has_desktopd: true, has_srv: false, version: 'v9' }]
  });
  harness.mount();
  await harness.load();
  assertEqual(harness.elements.desktopNodeBox.hidden, false, 'intranet block is shown');
  const html = harness.elements.desktopNodeList.innerHTML;
  assertIncludes(html, 'lab', 'the node name is listed');
  assertIncludes(html, '\u5728\u7ebf', 'online status is listed');
  assertIncludes(html, 'desktopd', 'desktopd capability is listed');
  assertIncludes(html, 'v9', 'version is listed');
  assertIncludes(html, 'data-node-delete="n1"', 'delete is offered');
  assertIncludes(html, 'data-node-rotate="n1"', 'a new secret can be issued');
}

async function testUserListCarriesOnlyAuthorizedUsers() {
  console.log('  Test: the check-desktop dropdown lists only the users the backend authorized');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  const html = harness.elements.desktopUser.innerHTML;
  assertIncludes(html, 'u_alice', 'an assigned user is listed');
  assertIncludes(html, 'u_bob', 'a department-covered user is listed');
  assert(html.indexOf('u_stranger') === -1, 'an org user without an assignment is not listed');
}

async function testUserListEmptyShowsTheAssignmentHint() {
  console.log('  Test: without authorized users the dropdown explains the assignment step');
  const harness = createHarness({ viewUsers: [] });
  harness.mount();
  await harness.load();
  const html = harness.elements.desktopUser.innerHTML;
  assertIncludes(html, '\u5148\u5728\u5f00\u901a\u8303\u56f4\u91cc\u6388\u6743', 'the empty list tells the admin to enable someone and assign a service');
  assert(html.indexOf('u_alice') === -1, 'no unauthorized user is listed');
}

async function testViewButtonFollowsStopButton() {
  console.log('  Test: the VNC button sits right after Stop desktop');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  const panel = harness.elements.botDesktopHost.innerHTML;
  const stop = panel.indexOf('id="desktopStop"');
  const view = panel.indexOf('id="desktopView"');
  assert(stop >= 0 && view > stop, 'VNC button is rendered after the stop button');
  assert(harness.elements.desktopView && harness.elements.desktopStop, 'both run buttons exist');
}

async function testViewOpensFramedDesktop() {
  console.log('  Test: viewing a desktop opens the Hub noVNC page in a frame');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();

  const call = harness.calls.filter(function (c) { return c.path === '/api/admin/desktop-services/desktops/view'; })[0];
  assert(!!call, 'the view request is sent');
  assertEqual(call.method, 'POST', 'the view request is a POST');
  assertEqual(call.body, JSON.stringify({ user_id: 'u_alice' }), 'the view request carries the chosen user');
  const overlay = harness.overlay();
  assert(!!overlay && overlay.classList.contains('show'), 'the viewer is shown');
  const frames = harness.frames();
  assertEqual(frames.length, 1, 'one noVNC frame is created');
  assertEqual(frames[0].src, HANDOFF, 'the frame points at the Hub handoff path');
  assertEqual(harness.elements.desktopRunStatus.textContent, 'VNC \u5df2\u6253\u5f00', 'the run status says the viewer opened');
  assertEqual(harness.elements.desktopVncTitle.textContent, 'VNC \u67e5\u770b \u00b7 Alice', 'the viewer names the user');
  assertIncludes(harness.elements.desktopVncNote.textContent, '30 \u5206\u949f', 'the viewer explains that the desktop stays up');
}

async function testCloseButtonDropsTheFrame() {
  console.log('  Test: closing the viewer removes the noVNC frame');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();
  assert(harness.frames().length === 1, 'the frame is open before closing');

  harness.fire(harness.elements.desktopVncClose, 'click');
  const overlay = harness.overlay();
  assert(!overlay.classList.contains('show'), 'the viewer is hidden again');
  assertEqual(harness.elements.desktopVncBody.innerHTML, '', 'the frame is dropped so the websocket ends');
}

async function testEscapeClosesTheViewer() {
  console.log('  Test: Escape closes the viewer');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();

  harness.pressKey('Escape');
  assert(!harness.overlay().classList.contains('show'), 'Escape hides the viewer');
  assertEqual(harness.elements.desktopVncBody.innerHTML, '', 'Escape drops the frame');
}

async function testBackdropClickClosesTheViewer() {
  console.log('  Test: clicking the backdrop closes the viewer');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();

  const overlay = harness.overlay();
  harness.fire(overlay, 'click', { target: overlay });
  assert(!overlay.classList.contains('show'), 'a backdrop click hides the viewer');
}

async function testViewRefusesAnUnsafeAddress() {
  console.log('  Test: a non-http noVNC address never reaches the frame');
  const harness = createHarness({ novncURL: 'javascript:alert(1)' });
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();

  assertEqual(harness.frames().length, 0, 'no frame is created for a javascript: address');
  assert(!harness.overlay().classList.contains('show'), 'the viewer stays closed');
  const errors = harness.toasts.filter(function (t) { return t.kind === 'error'; });
  assert(errors.length === 1, 'the admin is told the address was unusable');
}

async function testClosingWhileLoadingDoesNotReopen() {
  console.log('  Test: closing during the request stops the viewer from reopening');
  const harness = createHarness({ holdView: true });
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  harness.fire(harness.elements.desktopVncClose, 'click');

  harness.releaseView();
  await flush();
  assert(!harness.overlay().classList.contains('show'), 'the viewer stays closed');
  assertEqual(harness.frames().length, 0, 'a late reply does not open a frame');
}

async function testSecondClickKeepsTheSession() {
  console.log('  Test: viewing again while the frame is open does not reconnect');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();
  const frame = harness.frames()[0];
  let focused = 0;
  frame.contentWindow = { focus: function () { focused += 1; } };

  harness.fire(harness.elements.desktopView, 'click');
  await flush();
  assertEqual(harness.calls.filter(function (c) { return c.path === '/api/admin/desktop-services/desktops/view'; }).length, 1, 'no second request is sent');
  assertEqual(harness.frames().length, 1, 'the same frame stays open');
  assertEqual(focused, 1, 'the keyboard goes back to the running desktop');

  harness.fire(harness.elements.desktopVncClose, 'click');
  harness.fire(harness.elements.desktopView, 'click');
  await flush();
  assertEqual(harness.calls.filter(function (c) { return c.path === '/api/admin/desktop-services/desktops/view'; }).length, 2, 'after closing, viewing asks again');
}

async function testSwitchingUserOpensThatDesktop() {
  console.log('  Test: viewing another user replaces the frame');
  const harness = createHarness();
  harness.mount();
  await harness.load();
  harness.pickUser();
  harness.fire(harness.elements.desktopView, 'click');
  await flush();
  assert(harness.frames().length === 1, 'the first desktop is open');

  harness.elements.desktopUser.value = 'u_bob';
  harness.elements.desktopUser.selectedIndex = 2;
  harness.elements.desktopUser.options = [{ textContent: '\u8bf7\u9009\u62e9' }, { textContent: 'Alice' }, { textContent: 'Bob' }];
  harness.fire(harness.elements.desktopView, 'click');
  await flush();

  const calls = harness.calls.filter(function (c) { return c.path === '/api/admin/desktop-services/desktops/view'; });
  assertEqual(calls.length, 2, 'the new user is requested');
  assertEqual(calls[1].body, JSON.stringify({ user_id: 'u_bob' }), 'the request carries the new user');
  assertEqual(harness.elements.desktopVncTitle.textContent, 'VNC \u67e5\u770b \u00b7 Bob', 'the viewer names the new user');
}

Promise.resolve()
  .then(testRelayHintShowsWhenTheDesktopIsOnAnotherNode)
  .then(testEditingANodeKeepsItsVirtualAddress)
  .then(testIntranetNodesStayHiddenUntilTheFeatureIsOn)
  .then(testIntranetNodeListShowsStatusCapabilityAndVersion)
  .then(testUserListCarriesOnlyAuthorizedUsers)
  .then(testUserListEmptyShowsTheAssignmentHint)
  .then(testViewButtonFollowsStopButton)
  .then(testViewOpensFramedDesktop)
  .then(testCloseButtonDropsTheFrame)
  .then(testEscapeClosesTheViewer)
  .then(testBackdropClickClosesTheViewer)
  .then(testViewRefusesAnUnsafeAddress)
  .then(testClosingWhileLoadingDoesNotReopen)
  .then(testSecondClickKeepsTheSession)
  .then(testSwitchingUserOpensThatDesktop)
  .then(function () {
    console.log('desktop-tab tests: ' + passed + ' passed, ' + failed + ' failed');
    if (failed) process.exit(1);
  })
  .catch(function (err) {
    console.error(err);
    process.exit(1);
  });
