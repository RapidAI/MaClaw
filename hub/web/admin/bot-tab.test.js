/*
 * Bot access picker tests.
 * Run with: node hub/web/admin/bot-tab.test.js
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
  if (String(haystack || '').indexOf(needle) !== -1) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', message, '| missing:', JSON.stringify(needle));
  }
}

function assertNotIncludes(haystack, needle, message) {
  if (String(haystack || '').indexOf(needle) === -1) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', message, '| should not include:', JSON.stringify(needle));
  }
}

function attrValues(html, name) {
  const re = new RegExp(name + '="([^"]*)"', 'g');
  const out = [];
  let match;
  while ((match = re.exec(String(html || '')))) out.push(match[1]);
  return out;
}

async function flush() {
  for (let i = 0; i < 40; i += 1) {
    await new Promise(function (resolve) { setImmediate(resolve); });
  }
}

function createHarness(options) {
  const opts = options || {};
  const elements = Object.create(null);
  const calls = [];
  const toasts = [];
  const settings = {
    base_url: 'http://maclaw.example',
    token_set: true,
    admin_secret_set: false,
    bots: [],
    grants: []
  };
  const groups = opts.groups || {
    tree: {
      id: 'root',
      name: 'Company',
      children: [
        { id: 'dept-finance', name: 'Finance', children: [] },
        {
          id: 'dept-research',
          name: 'Research',
          children: [
            { id: 'dept-platform', name: 'Platform', children: [] }
          ]
        }
      ]
    }
  };
  const users = opts.users || {
    users: [
      { id: 'u_alice', email: 'alice@example.com' },
      { id: 'u_carol', email: 'carol@example.com', emails: ['carol.alt@example.com'] }
    ]
  };
  const members = opts.members || {
    root: [],
    'dept-finance': ['alice@example.com', 'ghost@example.com', 'alice@example.com'],
    'dept-research': [],
    'dept-platform': ['carol@example.com']
  };
  let failGroups = !!opts.failGroups;
  let financeFails = opts.failFinanceOnce ? 1 : 0;
  let releaseUsers = function () {};
  const usersGate = opts.delayUsers ? new Promise(function (resolve) { releaseUsers = resolve; }) : Promise.resolve();
  let releaseSettings = function () {};
  const settingsGate = opts.delaySettings ? new Promise(function (resolve) { releaseSettings = resolve; }) : Promise.resolve();

  function registerIds(html, parent) {
    const re = /id="([^"]+)"/g;
    let match;
    while ((match = re.exec(html))) {
      if (!elements[match[1]]) {
        elements[match[1]] = createElement(match[1]);
        elements[match[1]].parentElement = parent;
      }
    }
  }

  function createElement(id) {
    const el = {
      id: id || '',
      dataset: {},
      style: {},
      className: '',
      value: '',
      placeholder: '',
      checked: false,
      disabled: false,
      textContent: '',
      parentElement: null,
      attrs: {},
      listeners: {},
      _html: '',
      setAttribute: function (name, value) { this.attrs[name] = String(value); },
      getAttribute: function (name) {
        return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null;
      },
      addEventListener: function (type, fn) {
        (this.listeners[type] || (this.listeners[type] = [])).push(fn);
      },
      querySelectorAll: function () { return []; },
      focus: function () {}
    };
    Object.defineProperty(el, 'innerHTML', {
      configurable: true,
      get: function () { return el._html; },
      set: function (html) {
        el._html = String(html || '');
        registerIds(el._html, el);
      }
    });
    return el;
  }

  elements['tab-bots'] = createElement('tab-bots');
  const document = {
    readyState: 'complete',
    activeElement: null,
    getElementById: function (id) { return elements[id] || null; },
    addEventListener: function () {}
  };

  function api(url, request) {
    calls.push({ path: url, body: request && request.body, method: request && request.method });
    if (url === '/api/admin/bots/settings') {
      if (request && request.method === 'PUT') return Promise.resolve(settings);
      return settingsGate.then(function () { return settings; });
    }
    if (url === '/api/admin/security/groups') {
      if (failGroups) return Promise.reject(new Error('tree down'));
      return Promise.resolve(groups);
    }
    if (url === '/api/admin/users') return usersGate.then(function () { return users; });
    const memberPath = String(url || '').match(/^\/api\/admin\/security\/groups\/([^/]+)\/members$/);
    if (memberPath) {
      const groupId = decodeURIComponent(memberPath[1]);
      if (groupId === 'dept-finance' && financeFails) {
        financeFails -= 1;
        return Promise.reject(new Error('members down'));
      }
      return Promise.resolve({ members: members[groupId] || [] });
    }
    if (url === '/api/admin/bots/connection/test') return Promise.resolve({ ok: true, instance_count: 3 });
    if (url === '/api/admin/desktop-services/desktops' || url === '/api/admin/desktop-services/desktops/stop') {
      if (opts.failDesktop) return Promise.reject(new Error('no docker service is assigned to this user'));
      return Promise.resolve({ server_name: 'dockerd-a', status: 'running', user_id: 'u_alice' });
    }
    if (url === '/api/admin/desktop-services') {
      return Promise.resolve({
        servers: [{ id: 's1', name: 'dockerd-a', base_url: 'http://docker-host:18081', token_set: true, image: 'maclaw-gui:1', memory: '2500m', cpus: '1.5', shm_size: '512m' }],
        assignments: [{ id: 'a1', scope: 'department', target_id: 'dept-finance', server_id: 's1' }]
      });
    }
    if (url === '/api/admin/bots/grants' && request && request.method === 'POST') {
      const body = JSON.parse(request.body);
      settings.grants.push({ id: 'g' + settings.grants.length, scope: body.scope, target_id: body.target_id });
      return Promise.resolve({ id: 'g' + (settings.grants.length - 1) });
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

  vm.runInContext(fs.readFileSync(path.join(__dirname, 'admin-tabs.js'), 'utf8'), context, { filename: 'admin-tabs.js' });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'bot-tab.js'), 'utf8'), context, { filename: 'bot-tab.js' });
  if (opts.loadDesktop) vm.runInContext(fs.readFileSync(path.join(__dirname, 'desktop-tab.js'), 'utf8'), context, { filename: 'desktop-tab.js' });

  function click(attr, value) {
    const tree = elements.botOrgTree;
    const target = {
      id: attr ? '' : 'botOrgReload',
      parentElement: tree,
      getAttribute: function (name) { return name === attr ? value : null; }
    };
    (tree.listeners.click || []).forEach(function (fn) { fn({ target: target }); });
  }

  return {
    elements: elements,
    calls: calls,
    toasts: toasts,
    settings: settings,
    context: context,
    click: click,
    succeedGroups: function () { failGroups = false; },
    releaseUsers: function () { releaseUsers(); },
    releaseSettings: function () { releaseSettings(); },
    open: function () { return context.AdminTabRegistry.getTab('bots').onOpen(); }
  };
}

async function testPickerSelectsDepartmentAndUser() {
  console.log('  Test: organization picker adds department and user grants');
  const harness = createHarness();
  await harness.open();
  await flush();
  const panel = harness.elements['tab-bots']._html;
  const tree = harness.elements.botOrgTree._html;
  assertNotIncludes(panel, 'botDepartmentId', 'removes department id input');
  assertNotIncludes(panel, 'botUserId', 'removes user id input');
  assertIncludes(tree, 'Company', 'shows organization root');
  assertIncludes(tree, 'Finance', 'shows child department');
  assertIncludes(tree, 'data-org-add-dept="dept-finance"', 'department action uses the group id');
  assertNotIncludes(tree, 'alice@example.com', 'does not list people before the department is opened');

  harness.click('data-org-toggle', 'dept-finance');
  await flush();
  const opened = harness.elements.botOrgTree._html;
  assert(attrValues(opened, 'data-org-add-user').join(',') === 'u_alice', 'user action uses the Hub user id');
  assertIncludes(opened, 'alice@example.com', 'shows the member email');
  assertIncludes(opened, '\u672a\u7ed1\u5b9a\u8d26\u53f7', 'marks a member who has no Hub account');
  assertNotIncludes(opened, 'data-org-add-user=""', 'does not grant an unbound member');

  harness.click('data-org-add-user', 'u_alice');
  await flush();
  const userPosts = harness.calls.filter(function (call) {
    return call.path === '/api/admin/bots/grants' && call.body && call.body.indexOf('"scope":"user"') !== -1;
  });
  assert(userPosts.length === 1, 'posts one user grant');
  assertIncludes(userPosts[0].body, '"target_id":"u_alice"', 'posts the user id rather than the email');
  assertIncludes(harness.elements.botOrgTree._html, 'data-org-add-user="u_alice"', 'keeps the user action');
  assertIncludes(harness.elements.botOrgTree._html, 'disabled', 'marks the selected user as enabled');
  assertIncludes(harness.elements.botGrants._html, 'alice@example.com', 'lists the enabled user by email');

  harness.click('data-org-add-user', 'u_alice');
  await flush();
  assert(harness.calls.filter(function (call) {
    return call.path === '/api/admin/bots/grants' && call.body && call.body.indexOf('"target_id":"u_alice"') !== -1;
  }).length === 1, 'does not add the same user twice');

  harness.click('data-org-add-dept', 'dept-finance');
  await flush();
  const deptPosts = harness.calls.filter(function (call) {
    return call.path === '/api/admin/bots/grants' && call.body && call.body.indexOf('"scope":"department"') !== -1;
  });
  assert(deptPosts.length === 1, 'posts one department grant');
  assertIncludes(deptPosts[0].body, '"target_id":"dept-finance"', 'posts the department id from the tree');
  assertIncludes(harness.elements.botGrants._html, 'Company / Finance', 'lists the department by its organization path');

  const memberCallsBefore = harness.calls.filter(function (call) { return call.path.indexOf('/members') !== -1; }).length;
  harness.elements.botOrgFilter.value = 'carol.alt@example.com';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertIncludes(harness.elements.botOrgTree._html, 'data-org-add-user="u_carol"', 'search finds an account from the user directory');
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com / carol.alt@example.com', 'shows the alias that was typed next to the account');
  assertIncludes(harness.elements.botOrgTree._html, '\u5339\u914d\u7684\u8d26\u53f7', 'labels accounts that match outside the open departments');
  assertNotIncludes(harness.elements.botOrgTree._html, '\u6ca1\u6709\u5339\u914d', 'does not flash a no-match message');
  assertNotIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'search hides people who do not match');
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  const memberCallsAfter = harness.calls.filter(function (call) { return call.path.indexOf('/members') !== -1; }).length;
  assert(memberCallsAfter === memberCallsBefore, 'an account search does not load every department');

  const name = { id: '', parentElement: null, getAttribute: function () { return null; } };
  const row = {
    id: '',
    parentElement: harness.elements.botOrgTree,
    getAttribute: function (attr) { return attr === 'data-org-toggle' ? 'dept-finance' : null; }
  };
  name.parentElement = row;
  harness.elements.botOrgFilter.value = '';
  harness.elements.botOrgFilter.listeners.input[0]();
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'clearing search restores the departments that were already open');
  assertNotIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'clearing search closes departments the search had opened');
  harness.elements.botOrgTree.listeners.click[0]({ target: name });
  await flush();
  assertNotIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'clicking the department name collapses it');
}

async function testAccountSearchIgnoresSharedDomain() {
  console.log('  Test: an account search matches the mailbox name, not the shared domain or internal id');
  const users = [];
  for (let i = 0; i < 51; i += 1) {
    const n = String(i).padStart(2, '0');
    users.push({ id: 'u_aa' + n, email: 'aa' + n + '@example.com' });
  }
  users.push({ id: 'u_alice', email: 'alice@example.com' });
  const harness = createHarness({ users: { users: users } });
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = 'example';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertNotIncludes(harness.elements.botOrgTree._html, 'data-org-add-user=', 'does not list every account that shares a domain');
  assertIncludes(harness.elements.botOrgTree._html, '\u6ca1\u6709\u5339\u914d', 'says nothing matched');
  harness.elements.botOrgFilter.value = 'u_';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertNotIncludes(harness.elements.botOrgTree._html, 'data-org-add-user=', 'does not list accounts from a partial internal id');
  harness.elements.botOrgFilter.value = 'aa';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertIncludes(harness.elements.botOrgTree._html, 'data-org-add-user="u_aa00"', 'shows an account whose mailbox starts with the query');
  assertNotIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'hides a mailbox that does not start with the query');
  assertIncludes(harness.elements.botOrgTree._html, '\u53ea\u663e\u793a\u4e86\u524d 50 \u4e2a\u8d26\u53f7', 'says only the first accounts are shown');
  const buttons = attrValues(harness.elements.botOrgTree._html, 'data-org-add-user');
  assert(buttons.length === 50, 'caps the account list at 50');
}

async function testPhoneSearchFindsFormattedNumber() {
  console.log('  Test: a phone search finds a formatted number and ignores a short country code');
  const harness = createHarness({
    users: { users: [
      { id: 'u_ping', email: 'ping@example.com', phone: '+86 13800138000' },
      { id: 'u_room', email: 'room1380@example.com' }
    ] }
  });
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = '13800138000';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertIncludes(harness.elements.botOrgTree._html, 'data-org-add-user="u_ping"', 'finds the account from the phone digits');
  assertIncludes(harness.elements.botOrgTree._html, 'ping@example.com / +86 13800138000', 'shows the phone next to the account');
  harness.elements.botOrgFilter.value = '86';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertNotIncludes(harness.elements.botOrgTree._html, 'u_ping', 'does not match every number from the country code');
  harness.elements.botOrgFilter.value = '+86';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertNotIncludes(harness.elements.botOrgTree._html, 'u_ping', 'does not match every number from a written country code');
  harness.elements.botOrgFilter.value = 'room1380';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertIncludes(harness.elements.botOrgTree._html, 'data-org-add-user="u_room"', 'finds the mailbox that starts with those letters');
  assertNotIncludes(harness.elements.botOrgTree._html, 'u_ping', 'does not treat digits inside a mailbox as a phone search');
}

async function testDepartmentSearchSkipsOtherMembers() {
  console.log('  Test: a department search does not load every other department');
  const harness = createHarness();
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = 'Finance';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  const researchCalls = harness.calls.filter(function (call) {
    return call.path.indexOf('/dept-research/members') !== -1 || call.path.indexOf('/dept-platform/members') !== -1;
  });
  assert(researchCalls.length === 0, 'does not fetch members outside the matched department');
  assertIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'shows people in the matched department');
  assertNotIncludes(harness.elements.botOrgTree._html, 'Research', 'hides departments that do not match');
}

async function testParentSearchShowsNestedPeople() {
  console.log('  Test: a department search includes people in child departments');
  const harness = createHarness();
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = 'Research';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  const financeCalls = harness.calls.filter(function (call) {
    return call.path.indexOf('/dept-finance/members') !== -1;
  });
  assert(financeCalls.length === 0, 'does not scan unrelated departments');
  assert(harness.calls.some(function (call) {
    return call.path.indexOf('/dept-platform/members') !== -1;
  }), 'loads the child department under the matched name');
  assertIncludes(harness.elements.botOrgTree._html, 'data-org-add-user="u_carol"', 'shows a person who belongs to a child department');
  assertNotIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'hides people outside the matched department');

  harness.click('data-org-toggle', 'dept-research');
  await flush();
  assertNotIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'collapsing during search stays collapsed');
  harness.click('data-org-toggle', 'dept-research');
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'opening the department during search shows its people again');
}

async function testShortQueryDoesNotScanMembers() {
  console.log('  Test: one letter filters names without loading the organization');
  const harness = createHarness();
  await harness.open();
  await flush();
  const before = harness.calls.filter(function (call) { return call.path.indexOf('/members') !== -1; }).length;
  harness.elements.botOrgFilter.value = 'a';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  const after = harness.calls.filter(function (call) { return call.path.indexOf('/members') !== -1; }).length;
  assert(after === before, 'does not fetch members for a single letter');
  assertIncludes(harness.elements.botOrgTree._html, 'Finance', 'shows a department whose name contains the letter');
  assertNotIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'does not list people before a real search');
}

async function testSearchSurvivesRefresh() {
  console.log('  Test: opening the tab again keeps the current search');
  const harness = createHarness();
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = 'Research';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'search finds the nested person');
  const financeBefore = harness.calls.filter(function (call) {
    return call.path.indexOf('/dept-finance/members') !== -1;
  }).length;
  await harness.open();
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'the same person is still shown after refresh');
  const financeAfter = harness.calls.filter(function (call) {
    return call.path.indexOf('/dept-finance/members') !== -1;
  }).length;
  assert(financeAfter === financeBefore, 'refresh does not scan departments outside the search');
}

async function testDepartmentSearchShowsLoading() {
  console.log('  Test: a department search says it is loading people before they arrive');
  const harness = createHarness();
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = 'Research';
  harness.elements.botOrgFilter.listeners.input[0]();
  assertIncludes(harness.elements.botOrgTree._html, '\u6b63\u5728\u52a0\u8f7d\u6210\u5458', 'shows a loading state under the matched department');
  assertNotIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'does not show people before the search runs');
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'shows the nested person after loading');
  assertNotIncludes(harness.elements.botOrgTree._html, '\u6b63\u5728\u52a0\u8f7d\u6210\u5458', 'removes the loading state once people arrive');
}

async function testCollapsedDepartmentIsNotRefetched() {
  console.log('  Test: a closed department is not fetched again when the tab reopens');
  const harness = createHarness();
  await harness.open();
  await flush();
  harness.click('data-org-toggle', 'dept-finance');
  await flush();
  harness.click('data-org-toggle', 'dept-finance');
  await flush();
  const before = harness.calls.filter(function (call) {
    return call.path.indexOf('/dept-finance/members') !== -1;
  }).length;
  assert(before === 1, 'loads the department once when it is opened');
  await harness.open();
  await flush();
  const after = harness.calls.filter(function (call) {
    return call.path.indexOf('/dept-finance/members') !== -1;
  }).length;
  assert(after === before, 'does not load a department that was closed');
}

async function testCompositionDoesNotSearchEarly() {
  console.log('  Test: an IME composition does not search until the word is chosen');
  const harness = createHarness();
  await harness.open();
  await flush();
  const before = harness.calls.filter(function (call) { return call.path.indexOf('/members') !== -1; }).length;
  harness.elements['tab-bots']._org.composing = true;
  harness.elements.botOrgFilter.value = 'ca';
  harness.elements.botOrgFilter.listeners.input[0]({ isComposing: true });
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  const during = harness.calls.filter(function (call) { return call.path.indexOf('/members') !== -1; }).length;
  assert(during === before, 'does not fetch members while a word is being composed');
  assertIncludes(harness.elements.botOrgTree._html, 'Finance', 'keeps the full tree during composition');
  harness.elements['tab-bots']._org.composing = false;
  harness.elements.botOrgFilter.value = 'Research';
  harness.elements.botOrgFilter.listeners.compositionend[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'searches after the composed word is committed');
}

async function testWideDepartmentSearchStaysBounded() {
  console.log('  Test: a wide department search leaves departments past the limit closed');
  const children = [];
  const memberMap = { root: [], labs: [] };
  const userList = [];
  for (let i = 0; i < 85; i += 1) {
    const id = 'squad-' + String(i).padStart(2, '0');
    children.push({ id: id, name: 'Squad ' + String(i).padStart(2, '0'), children: [] });
    const email = 'person' + String(i).padStart(2, '0') + '@example.com';
    memberMap[id] = [email];
    userList.push({ id: 'u_' + id, email: email });
  }
  const harness = createHarness({
    groups: {
      tree: {
        id: 'root',
        name: 'Company',
        children: [{ id: 'labs', name: 'Labs', children: children }]
      }
    },
    users: { users: userList },
    members: memberMap
  });
  await harness.open();
  await flush();
  harness.elements.botOrgFilter.value = 'Labs';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  for (let n = 0; n < 40; n += 1) await flush();
  const html = harness.elements.botOrgTree._html;
  assertIncludes(html, 'person00@example.com', 'shows a person from a child department inside the limit');
  assertIncludes(html, 'data-org-toggle="squad-84" aria-expanded="false"', 'keeps a department past the limit closed');
  assertNotIncludes(html, 'person84@example.com', 'does not show people from a department that was not loaded');
  assertIncludes(html, '\u53ea\u641c\u7d22\u4e86\u524d\u9762\u4e00\u90e8\u5206\u90e8\u95e8', 'says only part of the organization was searched');
  assert(!harness.calls.some(function (call) {
    return call.path.indexOf('/squad-84/members') !== -1;
  }), 'does not fetch a department past the limit');
  harness.click('data-org-toggle', 'squad-84');
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'person84@example.com', 'opening that department loads its people');

  const callsAfterOpen = harness.calls.filter(function (call) {
    return call.path.indexOf('/members') !== -1;
  }).length;
  harness.elements.botOrgFilter.value = 'Lab';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  for (let n = 0; n < 10; n += 1) await flush();
  const callsAfterRetype = harness.calls.filter(function (call) {
    return call.path.indexOf('/members') !== -1;
  }).length;
  assert(callsAfterRetype === callsAfterOpen, 'typing a shorter prefix does not load the next batch of departments');
  assert(harness.calls.filter(function (call) {
    return call.path.indexOf('/squad-83/members') !== -1;
  }).length === 0, 'departments past the first window stay unloaded');
}

async function testMemberLoadRetries() {
  console.log('  Test: a failed member load can be retried');
  const harness = createHarness({ failFinanceOnce: true });
  await harness.open();
  await flush();
  harness.click('data-org-toggle', 'dept-finance');
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, '\u6210\u5458\u52a0\u8f7d\u5931\u8d25', 'shows the member load failure');
  assertNotIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'does not treat a failure as an empty department');
  harness.click('data-org-toggle', 'dept-finance');
  harness.click('data-org-toggle', 'dept-finance');
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'opening the department again loads its people');
}

async function testReopenRefreshesDirectory() {
  console.log('  Test: opening the tab again refreshes the organization');
  const harness = createHarness();
  await harness.open();
  await flush();
  const first = harness.calls.filter(function (call) { return call.path === '/api/admin/security/groups'; }).length;
  await harness.open();
  await flush();
  const second = harness.calls.filter(function (call) { return call.path === '/api/admin/security/groups'; }).length;
  assert(first === 1, 'loads the organization once on the first visit');
  assert(second === 2, 'loads the organization again when the tab is opened again');
}

async function testSearchRestoresTheTree() {
  console.log('  Test: clearing a search restores the departments that were open');
  const harness = createHarness();
  await harness.open();
  await flush();
  harness.click('data-org-toggle', 'dept-finance');
  await flush();
  harness.elements.botOrgFilter.value = 'Research';
  harness.elements.botOrgFilter.listeners.input[0]();
  await new Promise(function (resolve) { setTimeout(resolve, 280); });
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'search opens the matching department');
  harness.elements.botOrgFilter.value = '';
  harness.elements.botOrgFilter.listeners.input[0]();
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'alice@example.com', 'keeps a department that was open before the search');
  assertNotIncludes(harness.elements.botOrgTree._html, 'carol@example.com', 'closes departments that the search opened');
}

async function testAssignmentLabelFollowsDirectory() {
  console.log('  Test: a Docker assignment shows the department path after the directory loads');
  const harness = createHarness({ delayUsers: true, loadDesktop: true });
  const pending = harness.open();
  await flush();
  assertIncludes(harness.elements.desktopAssignList._html, 'dept-finance', 'shows the department id until the directory is ready');
  harness.releaseUsers();
  await pending;
  await flush();
  assertIncludes(harness.elements.desktopAssignList._html, 'Company / Finance', 'replaces the department id with its path');
  assertNotIncludes(harness.elements.desktopAssignList._html, 'dept-finance', 'does not keep the raw department id');
}

async function testConnectionAndDockerUrls() {
  console.log('  Test: connection and Docker forms reject an empty or non-http address');
  const harness = createHarness({ loadDesktop: true });
  await harness.open();
  await flush();
  function puts() {
    return harness.calls.filter(function (call) {
      return call.method === 'PUT' || (call.method === 'POST' && call.path === '/api/admin/desktop-services');
    });
  }
  harness.elements.botBaseUrl.value = '';
  harness.elements.botSave.listeners.click[0]();
  harness.elements.botBaseUrl.value = 'ftp://maclaw.example';
  harness.elements.botSave.listeners.click[0]();
  harness.elements.desktopName.value = '';
  harness.elements.desktopAdd.listeners.click[0]();
  harness.elements.desktopName.value = 'dockerd';
  harness.elements.desktopUrl.value = 'docker-host:18081';
  harness.elements.desktopAdd.listeners.click[0]();
  assert(puts().length === 0, 'does not save an empty or non-http address');
  assert(harness.toasts.filter(function (toast) { return toast.kind === 'error'; }).length >= 3, 'explains why the save did not run');
  harness.elements.botBaseUrl.value = 'http://maclaw.example';
  harness.elements.botSave.listeners.click[0]();
  await flush();
  assert(harness.calls.some(function (call) {
    return call.path === '/api/admin/bots/settings' && call.method === 'PUT';
  }), 'saves an http MaClawSrv address');
}

async function testDockerServiceGuards() {
  console.log('  Test: assigned services and duplicate scopes are blocked before the request');
  const harness = createHarness({ loadDesktop: true });
  await harness.open();
  await flush();
  assertIncludes(harness.elements.desktopServerList._html, 'disabled', 'does not offer to delete a service that is still assigned');
  assertIncludes(harness.elements.desktopServerList._html, '\u5148\u53d6\u6d88\u5206\u914d', 'explains that assignments must be removed first');
  harness.elements.desktopServer.value = 's1';
  harness.elements.desktopScope.value = 'department';
  harness.elements.desktopTarget.value = 'dept-finance';
  harness.elements.desktopAssign.listeners.click[0]();
  assert(!harness.calls.some(function (call) {
    return call.path === '/api/admin/desktop-services/assignments';
  }), 'does not save a duplicate assignment');
  assert(harness.toasts.some(function (toast) { return toast.msg.indexOf('\u5df2\u7ecf\u5206\u914d') !== -1; }), 'explains the duplicate assignment');
  harness.elements.desktopName.value = 'dockerd';
  harness.elements.desktopUrl.value = 'http://docker-host:18081';
  harness.elements.desktopToken.value = 'secret';
  harness.elements.desktopMemory.value = 'lots';
  harness.elements.desktopAdd.listeners.click[0]();
  assert(!harness.calls.some(function (call) {
    return call.method === 'POST' && call.path === '/api/admin/desktop-services';
  }), 'does not save a service with an invalid memory value');
  assert(harness.toasts.some(function (toast) { return toast.msg.indexOf('\u5185\u5b58') !== -1; }), 'explains the resource format');
}

async function testTypedUrlSurvivesFirstLoad() {
  console.log('  Test: the first settings load keeps an address already typed');
  const harness = createHarness({ delaySettings: true });
  const pending = harness.open();
  await flush();
  harness.elements.botBaseUrl.value = 'http://typed.example';
  harness.releaseSettings();
  await pending;
  await flush();
  assert(harness.elements.botBaseUrl.value === 'http://typed.example', 'does not replace an address typed before settings return');
}

async function testUnsavedUrlSurvivesReload() {
  console.log('  Test: reloading settings keeps an unsaved MaClawSrv address');
  const harness = createHarness();
  await harness.open();
  await flush();
  assert(harness.elements.botBaseUrl.value === 'http://maclaw.example', 'fills the saved address');
  harness.elements.botBaseUrl.value = 'http://typed.example';
  harness.settings.base_url = 'http://updated.example';
  await harness.open();
  await flush();
  assert(harness.elements.botBaseUrl.value === 'http://typed.example', 'does not replace an address the admin is still editing');
  harness.elements.botBaseUrl.value = 'http://maclaw.example';
  harness.elements.botBaseUrl._serverValue = 'http://maclaw.example';
  await harness.open();
  await flush();
  assert(harness.elements.botBaseUrl.value === 'http://updated.example', 'updates the address when the field still matches the last saved value');
}

async function testConnectionUsesSavedSettings() {
  console.log('  Test: connection test waits until the form is saved');
  const harness = createHarness();
  await harness.open();
  await flush();
  function tests() {
    return harness.calls.filter(function (call) { return call.path === '/api/admin/bots/connection/test'; });
  }
  harness.elements.botBaseUrl.value = 'http://other.example';
  harness.elements.botTest.listeners.click[0]();
  harness.elements.botBaseUrl.value = 'http://maclaw.example';
  harness.elements.botAccessToken.value = 'new-token';
  harness.elements.botTest.listeners.click[0]();
  assert(tests().length === 0, 'does not test an address or token that is not saved');
  assert(harness.toasts.filter(function (toast) { return toast.msg.indexOf('\u8fd8\u6ca1\u4fdd\u5b58') !== -1; }).length === 2, 'asks to save before testing');
  harness.elements.botAccessToken.value = '';
  harness.elements.botTest.listeners.click[0]();
  await flush();
  assert(tests().length === 1, 'tests the saved connection');
  assertIncludes(harness.elements.botConnStatus.textContent, '3', 'reports the instance count');
}

async function testDesktopCheckNamesTheService() {
  console.log('  Test: checking a desktop names the assigned Docker service');
  const harness = createHarness({ loadDesktop: true });
  await harness.open();
  await flush();
  harness.elements.desktopUser.value = 'u_alice';
  harness.elements.desktopStart.listeners.click[0]();
  await flush();
  assertIncludes(harness.elements.desktopRunStatus.textContent, 'dockerd-a', 'names the Docker service that was used');
  assertIncludes(harness.elements.desktopRunStatus.textContent, '\u8fd0\u884c\u4e2d', 'shows the running status in Chinese');
  assertNotIncludes(harness.elements.desktopRunStatus.textContent, 'running', 'does not leave the status in English');
  assert(harness.calls.some(function (call) {
    return call.path === '/api/admin/desktop-services/desktops' && call.body && call.body.indexOf('u_alice') !== -1;
  }), 'creates the desktop for the selected user');
}

async function testDesktopCheckKeepsTheError() {
  console.log('  Test: a failed desktop check stays on the status line');
  const harness = createHarness({ loadDesktop: true, failDesktop: true });
  await harness.open();
  await flush();
  harness.elements.desktopUser.value = 'u_alice';
  harness.elements.desktopStart.listeners.click[0]();
  await flush();
  assertIncludes(harness.elements.desktopRunStatus.textContent, '\u8fd8\u6ca1\u6709\u5206\u914d', 'keeps the failure on the status line');
  assert(harness.toasts.some(function (toast) { return toast.kind === 'error' && toast.msg.indexOf('\u8fd8\u6ca1\u6709\u5206\u914d') !== -1; }), 'also toasts the failure');
}

async function testBotOwnerUsesDirectoryName() {
  console.log('  Test: a bot lists its owner by email after the directory arrives');
  const harness = createHarness({ delayUsers: true });
  harness.settings.bots = [{
    name: 'daily',
    description: 'morning',
    owner_user_id: 'u_alice',
    instance_id: 'inst-1'
  }];
  const pending = harness.open();
  await flush();
  assertIncludes(harness.elements.botList._html, 'u_alice', 'shows the user id until the directory is ready');
  assertNotIncludes(harness.elements.botList._html, 'alice@example.com', 'does not invent an email before the directory loads');
  harness.releaseUsers();
  await pending;
  await flush();
  assertIncludes(harness.elements.botList._html, 'alice@example.com', 'replaces the user id with the directory email');
  assertNotIncludes(harness.elements.botList._html, 'u_alice', 'does not keep the raw user id once the email is known');
  assertIncludes(harness.elements.botList._html, 'inst-1', 'keeps the instance id');
  assertNotIncludes(harness.elements['tab-bots']._html, 'botName', 'does not offer a create form');
}

async function testReloadAfterDirectoryFailure() {
  console.log('  Test: organization reload recovers from a failed load');
  const harness = createHarness({ failGroups: true });
  await harness.open();
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'tree down', 'shows the directory error');
  assertIncludes(harness.elements.botOrgTree._html, 'id="botOrgReload"', 'offers a reload action');
  harness.succeedGroups();
  harness.click('', '');
  await harness.elements['tab-bots']._org.promise;
  await flush();
  assertIncludes(harness.elements.botOrgTree._html, 'Company', 'reload renders the organization');
  assert(harness.toasts.length === 0, 'reload does not toast after success');
}

Promise.resolve()
  .then(testPickerSelectsDepartmentAndUser)
  .then(testAccountSearchIgnoresSharedDomain)
  .then(testPhoneSearchFindsFormattedNumber)
  .then(testDepartmentSearchSkipsOtherMembers)
  .then(testParentSearchShowsNestedPeople)
  .then(testShortQueryDoesNotScanMembers)
  .then(testSearchSurvivesRefresh)
  .then(testDepartmentSearchShowsLoading)
  .then(testCollapsedDepartmentIsNotRefetched)
  .then(testCompositionDoesNotSearchEarly)
  .then(testWideDepartmentSearchStaysBounded)
  .then(testMemberLoadRetries)
  .then(testReopenRefreshesDirectory)
  .then(testSearchRestoresTheTree)
  .then(testAssignmentLabelFollowsDirectory)
  .then(testConnectionAndDockerUrls)
  .then(testDockerServiceGuards)
  .then(testTypedUrlSurvivesFirstLoad)
  .then(testUnsavedUrlSurvivesReload)
  .then(testConnectionUsesSavedSettings)
  .then(testDesktopCheckNamesTheService)
  .then(testDesktopCheckKeepsTheError)
  .then(testBotOwnerUsesDirectoryName)
  .then(testReloadAfterDirectoryFailure)
  .then(function () {
    console.log('bot-tab tests: ' + passed + ' passed, ' + failed + ' failed');
    if (failed) process.exit(1);
  })
  .catch(function (err) {
    console.error(err);
    process.exit(1);
  });
