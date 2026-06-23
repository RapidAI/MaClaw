/*
 * Security admin module tests.
 * Run with: node hub/web/admin/security-tab.test.js
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
  if (typeof str === 'string' && str.indexOf(substr) !== -1) {
    passed++;
  } else {
    failed++;
    console.error('  FAIL:', message, '| missing:', JSON.stringify(substr));
  }
}

function assertNotIncludes(str, substr, message) {
  if (typeof str === 'string' && str.indexOf(substr) === -1) {
    passed++;
  } else {
    failed++;
    console.error('  FAIL:', message, '| should not include:', JSON.stringify(substr));
  }
}

function createClassList() {
  var values = {};
  return {
    add: function(name) { values[name] = true; },
    remove: function(name) { delete values[name]; },
    toggle: function(name, on) { if (on) values[name] = true; else delete values[name]; },
    contains: function(name) { return !!values[name]; }
  };
}

function createMockDOM() {
  var elements = {};
  var approvalRoleRows = [];
  function decodeAttr(value) {
    return String(value || '')
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&amp;/g, '&');
  }
  function attrsFromTag(tag) {
    var attrs = {};
    var re = /([a-zA-Z0-9_-]+(?:-[a-zA-Z0-9_-]+)*)="([^"]*)"/g;
    var match;
    while ((match = re.exec(tag))) attrs[match[1]] = decodeAttr(match[2]);
    return attrs;
  }
  function makeElement(id) {
    var attrs = {};
    var el = {
      id: id || '',
      _innerHTML: '',
      textContent: '',
      value: '',
      disabled: false,
      checked: false,
      style: {},
      classList: createClassList(),
      parentNode: null,
      setAttribute: function(name, value) { attrs[name] = String(value); },
      getAttribute: function(name) { return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null; },
      addEventListener: function() {},
      appendChild: function(child) { if (child) child.parentNode = el; },
      removeChild: function(child) { if (child) child.parentNode = null; },
      focus: function() {},
      querySelector: function(selector) {
        if (selector && selector.charAt(0) === '#') return elements[selector.slice(1)] || null;
        if (selector === '[data-approval-assignees]') return el._approvalAssignees || null;
        if (selector === '[data-approval-assignees-json]') return el._approvalAssigneesJSON || null;
        if (selector === '[data-approval-mode]') return el._approvalMode || null;
        return null;
      },
      querySelectorAll: function() { return []; }
    };
    Object.defineProperty(el, 'innerHTML', {
      get: function() { return el._innerHTML; },
      set: function(value) {
        el._innerHTML = String(value || '');
        var idRe = /\sid="([^"]+)"/g;
        var match;
        while ((match = idRe.exec(el._innerHTML))) {
          if (!elements[match[1]]) elements[match[1]] = makeElement(match[1]);
          elements[match[1]].parentNode = el;
        }
        if (el.id === 'secApprovalRolesRoot') {
          approvalRoleRows = [];
          var rowRe = /<div class="approval-role-row[^"]*"([^>]*)>/g;
          var rowMatch;
          while ((rowMatch = rowRe.exec(el._innerHTML))) {
            var start = rowMatch.index;
            var next = el._innerHTML.indexOf('<div class="approval-role-row', start + 1);
            var chunk = next === -1 ? el._innerHTML.slice(start) : el._innerHTML.slice(start, next);
            var row = makeElement('approval-role-row-' + approvalRoleRows.length);
            var rowAttrs = attrsFromTag(rowMatch[0]);
            Object.keys(rowAttrs).forEach(function(name) { row.setAttribute(name, rowAttrs[name]); });
            var assigneeMatch = /<input data-approval-assignees value="([^"]*)"/.exec(chunk);
            var jsonMatch = /<input type="hidden" data-approval-assignees-json value="([^"]*)"/.exec(chunk);
            var modeMatch = /<option value="([^"]*)" selected>/.exec(chunk);
            row._approvalAssignees = makeElement('approval-assignees-' + approvalRoleRows.length);
            row._approvalAssignees.value = assigneeMatch ? decodeAttr(assigneeMatch[1]) : '';
            row._approvalAssigneesJSON = makeElement('approval-assignees-json-' + approvalRoleRows.length);
            row._approvalAssigneesJSON.value = jsonMatch ? decodeAttr(jsonMatch[1]) : '';
            row._approvalMode = makeElement('approval-mode-' + approvalRoleRows.length);
            row._approvalMode.value = modeMatch ? decodeAttr(modeMatch[1]) : 'manual';
            approvalRoleRows.push(row);
          }
        }
      }
    });
    return el;
  }
  return {
    elements: elements,
    getElementById: function(id) {
      if (!elements[id]) elements[id] = makeElement(id);
      return elements[id];
    },
    createElement: function(tag) {
      return makeElement(tag);
    },
    querySelectorAll: function(selector) {
      if (selector === '#secApprovalRolesRoot .approval-role-row[data-approval-role-id]') return approvalRoleRows.slice();
      return [];
    },
    body: {
      appendChild: function(el) { if (el && el.id) elements[el.id] = el; },
      removeChild: function(el) { if (el && el.id && elements[el.id] === el) delete elements[el.id]; }
    },
    addEventListener: function() {},
    readyState: 'complete'
  };
}

function createMockGlobal() {
  var apiCalls = [];
  var toasts = [];
  var registeredTabs = [];
  var storage = {};
  var mockGlobal = {
    currentLang: 'zh',
    document: createMockDOM(),
    console: console,
    JSON: JSON,
    Date: Date,
    Object: Object,
    String: String,
    Number: Number,
    Array: Array,
    Promise: Promise,
    encodeURIComponent: encodeURIComponent,
    decodeURIComponent: decodeURIComponent,
    localStorage: {
      getItem: function(key) { return Object.prototype.hasOwnProperty.call(storage, key) ? storage[key] : null; },
      setItem: function(key, value) { storage[key] = String(value); },
      removeItem: function(key) { delete storage[key]; }
    },
    AdminTabRegistry: {
      registerTab: function(definition) { registeredTabs.push(definition); },
      onLanguageChange: function() {}
    },
    showToast: function(msg, type) { toasts.push({ msg: msg, type: type }); },
    setOutput: function() {},
    _s: function(id, prop, value) {
      var el = mockGlobal.document.getElementById(id);
      if (el) el[prop] = value;
    },
    escapeHtml: function(value) {
      return String(value || '').replace(/[&<>"']/g, function(ch) {
        return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch];
      });
    },
    confirm: function() { return true; },
    prompt: function() { return 'Custom Role'; },
    api: async function(url, opts) {
      apiCalls.push({ url: url, opts: opts || {} });
      if (url === '/api/admin/security/approval-roles') {
        if (opts && opts.method === 'PUT') {
          return JSON.parse(opts.body || '{"roles":[]}');
        }
        return { roles: [] };
      }
      if (url === '/api/admin/users') {
        return { users: [
          { email: 'alice@example.com', sn: 'A001', status: 'active' },
          { email: 'bob@example.com', sn: 'B001', status: 'active' }
        ] };
      }
      if (url === '/api/ve/list') {
        return { employees: [
          { id: 'finance-bot', name: 'Finance Bot', status: 'active', visible_group_ids: ['dept-finance'] },
          { id: 'alice-twin', name: 'Alice Twin', status: 'active', owner_email: 'alice@example.com' }
        ] };
      }
      if (url === '/api/admin/security/groups/dept-finance/members') {
        return { members: ['alice@example.com'], children: [] };
      }
      return {};
    },
    _apiCalls: apiCalls,
    _toasts: toasts,
    _registeredTabs: registeredTabs
  };
  mockGlobal.window = mockGlobal;
  return mockGlobal;
}

function loadSecurityTab(mockGlobal) {
  var code = fs.readFileSync(path.join(__dirname, 'security-tab.js'), 'utf8');
  var script = new vm.Script(code, { filename: 'security-tab.js' });
  var context = vm.createContext(mockGlobal);
  script.runInContext(context);
  return context;
}

async function runTests() {
  console.log('\n--- Test Suite: Approval Roles ---');

  {
    console.log('  Test: approval roles registers as an independent admin tab');
    var g = createMockGlobal();
    loadSecurityTab(g);
    var securityTab = g._registeredTabs.find(function(tab) { return tab && tab.id === 'security'; });
    var approvalTab = g._registeredTabs.find(function(tab) { return tab && tab.id === 'approvalroles'; });
    assert(!!securityTab, 'enterprise management tab should still register');
    assert(!!approvalTab, 'approval roles should register as its own left nav tab');
    assertEqual(approvalTab.title(), '\u5ba1\u6279\u89d2\u8272', 'approval roles tab title should not reuse enterprise management');
    assertEqual(approvalTab.subtitle(), '\u6309\u7ec4\u7ec7\u6216\u804c\u80fd\u914d\u7f6e\u5ba1\u6279\u89d2\u8272\uff0c\u4f9b\u5ba1\u6279\u5de5\u4f5c\u6d41\u8bbe\u8ba1\u5668\u5f15\u7528\u3002', 'approval roles tab subtitle should explain workflow reuse');
  }

  {
    console.log('  Test: approval roles tab renders organization scopes and scoped subjects');
    var g = createMockGlobal();
    var ctx = loadSecurityTab(g);
    g.document.getElementById('secApprovalRolesRoot');
    g.__securityAdminState.groupTree = [{
      id: 'root',
      name: 'Company',
      children: [{ id: 'dept-finance', name: 'Finance', children: [] }]
    }];
    g.__securityAdminState.approvalRoleScope = 'department:dept-finance';
    g.__securityAdminState.approvalRolesLoaded = true;
    g.__securityAdminState.approvalRoles = [];
    await ctx.loadApprovalRolesTab();
    await new Promise(function(resolve) { setTimeout(resolve, 0); });
    var html = g.document.elements.secApprovalRolesRoot.innerHTML;
    assertIncludes(html, 'Finance', 'department scope should render');
    assertIncludes(html, 'Alice Twin', 'digital twin owned by department member should render');
    assertIncludes(html, 'Finance Bot', 'department digital employee should render');
    assertNotIncludes(html, 'bob@example.com', 'non-member physical employee should not render for selected department');
  }

  {
    console.log('  Test: function scopes include presets and support add/delete');
    var g = createMockGlobal();
    g.currentLang = 'en';
    var ctx = loadSecurityTab(g);
    g.document.getElementById('secApprovalRolesRoot');
    g.document.getElementById('secApprovalRolesSaveBtn');
    g.__securityAdminState.approvalRoleView = 'function';
    g.__securityAdminState.approvalRoleScope = 'function:hr';
    g.__securityAdminState.approvalRolesLoaded = true;
    g.__securityAdminState.approvalRoles = [];
    await ctx.loadApprovalRolesTab();
    var html = g.document.elements.secApprovalRolesRoot.innerHTML;
    assertIncludes(html, 'HR', 'HR function should be available as a preset');
    assertIncludes(html, 'Sales', 'sales function should be available as a preset');
    assertIncludes(html, 'Risk &amp; Compliance', 'risk and compliance function should be available as a preset');

    g.prompt = function() { return 'HR'; };
    ctx.addSecApprovalFunction();
    assertEqual(g.__securityAdminState.approvalRoleScope, 'function:hr', 'duplicate function name should select existing scope');
    assertEqual(g.__securityAdminState.approvalFunctionScopes.filter(function(item) { return item.scopeId === 'hr'; }).length, 1, 'duplicate function name should not create a second HR scope');

    g.__securityAdminState.approvalFunctionScopes = g.__securityAdminState.approvalFunctionScopes.concat([{ scopeId: 'hr_alt', scopeName: ' HR ', custom: true }]);
    await ctx.loadApprovalRolesTab();
    assertEqual(g.__securityAdminState.approvalFunctionScopes.filter(function(item) { return String(item.scopeName || '').trim().toLowerCase() === 'hr'; }).length, 1, 'same function name with another id should be deduped');

    g.prompt = function() { return 'People Ops'; };
    ctx.addSecApprovalFunction();
    html = g.document.elements.secApprovalRolesRoot.innerHTML;
    assertIncludes(html, 'People Ops', 'custom function should render after add');
    assertEqual(g.__securityAdminState.approvalRoleScope, 'function:people_ops', 'new custom function should become selected');

    await ctx.saveSecApprovalRoles();
    var putCall = g._apiCalls.find(function(call) {
      return call.url === '/api/admin/security/approval-roles' && call.opts.method === 'PUT';
    });
    var body = JSON.parse(putCall.opts.body);
    assert(body.functionScopes.some(function(item) { return item.scopeId === 'people_ops' && item.scopeName === 'People Ops'; }), 'save payload should include custom function scope');

    ctx.removeSecApprovalFunction('people_ops');
    html = g.document.elements.secApprovalRolesRoot.innerHTML;
    assertNotIncludes(html, 'People Ops', 'custom function should be removed from scope list');

    g.__securityAdminState.approvalFunctionScopes.slice().forEach(function(scope) {
      ctx.removeSecApprovalFunction(scope.scopeId);
    });
    html = g.document.elements.secApprovalRolesRoot.innerHTML;
    assertIncludes(html, 'No functions yet. Add a function before configuring approval roles.', 'empty function list should explain next step');
    assertNotIncludes(html, 'Add role', 'empty function list should not show add role action');
  }

  {
    console.log('  Test: save persists approval roles through the admin API');
    var g = createMockGlobal();
    var ctx = loadSecurityTab(g);
    var saveBtn = g.document.getElementById('secApprovalRolesSaveBtn');
    g.document.getElementById('secApprovalRolesRoot');
    g.__securityAdminState.approvalRolesLoaded = true;
    g.__securityAdminState.approvalRoles = [{
      id: 'role:function:finance:finance_approver',
      view: 'function',
      scopeType: 'function',
      scopeId: 'finance',
      scopeName: 'Finance',
      roleCode: 'finance_approver',
      roleName: 'Finance Approver',
      executionMode: 'digital_review',
      assignees: [{ subjectType: 'user', subjectId: 'alice@example.com', displayName: 'alice@example.com' }]
    }];
    await ctx.saveSecApprovalRoles();
    var putCall = g._apiCalls.find(function(call) {
      return call.url === '/api/admin/security/approval-roles' && call.opts.method === 'PUT';
    });
    assert(!!putCall, 'save should PUT approval roles');
    var body = JSON.parse(putCall.opts.body);
    assertEqual(body.roles.length, 1, 'save should include one role');
    assertEqual(body.roles[0].assignees[0].subjectId, 'alice@example.com', 'save should preserve assignee');
    assert(body.functionScopes.some(function(item) { return item.scopeId === 'hr'; }), 'save should include default HR function scope');
    assertEqual(saveBtn.disabled, false, 'save button should be re-enabled after save');
    assert(g._toasts.some(function(item) { return item.type === 'success'; }), 'save should show success toast');
  }

  {
    console.log('  Test: subject picker writes scoped organization assignees before save');
    var g = createMockGlobal();
    var ctx = loadSecurityTab(g);
    g.document.getElementById('secApprovalRolesRoot');
    g.document.getElementById('secApprovalRolesSaveBtn');
    g.__securityAdminState.groupTree = [{
      id: 'root',
      name: 'Company',
      children: [{ id: 'dept-finance', name: 'Finance', children: [] }]
    }];
    g.__securityAdminState.approvalRoleScope = 'department:dept-finance';
    g.__securityAdminState.approvalRolesLoaded = true;
    g.__securityAdminState.approvalRoles = [];
    await ctx.loadApprovalRolesTab();
    await ctx.openSecApprovalSubjectPicker('role:department:dept-finance:department_manager');
    var pickerHtml = g.document.elements.approvalSubjectList.innerHTML;
    assertIncludes(pickerHtml, 'alice@example.com', 'picker should list department physical employee');
    assertIncludes(pickerHtml, 'Alice Twin', 'picker should list department member digital twin');
    assertIncludes(pickerHtml, 'Finance Bot', 'picker should list department digital employee');
    assertNotIncludes(pickerHtml, 'bob@example.com', 'picker should not list non-member physical employee');

    ctx.toggleSecApprovalSubject('user:alice@example.com', true);
    ctx.toggleSecApprovalSubject('digital_twin:alice-twin', true);
    ctx.toggleSecApprovalSubject('digital_employee:finance-bot', true);
    ctx.confirmSecApprovalSubjectPicker();

    var role = g.__securityAdminState.approvalRoles.find(function(item) {
      return item.id === 'role:department:dept-finance:department_manager';
    });
    assert(!!role, 'confirm should create/update department manager role');
    assertEqual(role.assignees.length, 3, 'confirm should keep all selected assignees');
    assert(role.assignees.some(function(item) { return item.subjectType === 'user' && item.subjectId === 'alice@example.com'; }), 'role should include physical employee');
    assert(role.assignees.some(function(item) { return item.subjectType === 'digital_twin' && item.subjectId === 'alice-twin'; }), 'role should include digital twin');
    assert(role.assignees.some(function(item) { return item.subjectType === 'digital_employee' && item.subjectId === 'finance-bot'; }), 'role should include department digital employee');

    await ctx.saveSecApprovalRoles();
    var putCall = g._apiCalls.find(function(call) {
      return call.url === '/api/admin/security/approval-roles' && call.opts.method === 'PUT';
    });
    var body = JSON.parse(putCall.opts.body);
    var savedRole = body.roles.find(function(item) {
      return item.id === 'role:department:dept-finance:department_manager';
    });
    assert(!!savedRole, 'save payload should include selected department role');
    assertEqual(savedRole.assignees.length, 3, 'save payload should include selected assignees');
  }
}

runTests().then(function() {
  console.log('\n=== Results: ' + passed + '/' + (passed + failed) + ' passed ===');
  if (failed > 0) process.exit(1);
}).catch(function(err) {
  console.error(err);
  process.exit(1);
});
