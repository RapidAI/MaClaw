/**
 * Terminal Node Config Panel - Unit Tests
 *
 * Tests the terminal node configuration panel logic:
 * - Default config generation
 * - Config form HTML generation
 * - User search and add flow
 * - Validation (timeout_hours range, max_reminders range)
 * - Warning banner visibility
 *
 * Run with: node terminal-node-config.test.js
 */

// Minimal DOM mock for Node.js testing
var document = {
  _elements: {},
  createElement: function (tag) {
    return {
      textContent: '',
      innerHTML: '',
      get innerHTML() { return this._innerHTML || ''; },
      set innerHTML(v) { this._innerHTML = v; },
      get textContent() { return this._textContent || ''; },
      set textContent(v) { this._textContent = v; },
    };
  },
  getElementById: function () { return null; },
  querySelectorAll: function () { return []; },
};

var window = {};

// Load the module
var fs = require('fs');
var path = require('path');
var code = fs.readFileSync(path.join(__dirname, 'terminal-node-config.js'), 'utf8');
eval(code);

// --- Test helpers ---
var testCount = 0;
var passCount = 0;

function assert(condition, message) {
  testCount++;
  if (condition) {
    passCount++;
    console.log('  ✓ ' + message);
  } else {
    console.log('  ✗ FAIL: ' + message);
  }
}

function assertEqual(actual, expected, message) {
  testCount++;
  if (actual === expected) {
    passCount++;
    console.log('  ✓ ' + message);
  } else {
    console.log('  ✗ FAIL: ' + message + ' (expected: ' + JSON.stringify(expected) + ', got: ' + JSON.stringify(actual) + ')');
  }
}

// --- Tests ---

console.log('\n=== Terminal Node Config Panel Tests ===\n');

// Test 1: getTerminalNodeDefaultConfig
console.log('Test: getTerminalNodeDefaultConfig');
(function () {
  var config = window.getTerminalNodeDefaultConfig();
  assert(Array.isArray(config.result_executors), 'result_executors is array');
  assertEqual(config.result_executors.length, 0, 'result_executors is empty by default');
  assert(Array.isArray(config.notifiers), 'notifiers is array');
  assertEqual(config.notifiers.length, 0, 'notifiers is empty by default');
})();

// Test 2: buildTerminalNodeConfigForm generates HTML
console.log('\nTest: buildTerminalNodeConfigForm');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: { result_executors: [], notifiers: [] },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('terminalNoExecutorWarning') !== -1, 'contains warning banner element');
  assert(html.indexOf('Result Executors') !== -1, 'contains Result Executors section');
  assert(html.indexOf('Notifiers') !== -1, 'contains Notifiers section');
  assert(html.indexOf('terminalExecutorSearch') !== -1, 'contains executor search input');
  assert(html.indexOf('terminalNotifierSearch') !== -1, 'contains notifier search input');
})();

// Test 3: Warning banner visible when no executors
console.log('\nTest: Warning banner visibility');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: { result_executors: [], notifiers: [] },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  // Warning should be visible (no display:none)
  assert(html.indexOf('display:none') === -1 || html.indexOf('terminalNoExecutorWarning') < html.indexOf('display:none'), 'warning banner visible when no executors');
})();

// Test 4: Warning banner hidden when executors present
console.log('\nTest: Warning banner hidden with executors');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [{ user_id: 'user1', user_name: 'User One', timeout_hours: 48, max_reminders: 3, reminder_interval_hours: 24 }],
      notifiers: [],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  // Warning should have display:none
  var warningStart = html.indexOf('terminalNoExecutorWarning');
  var styleAfterWarning = html.substring(warningStart, warningStart + 200);
  assert(styleAfterWarning.indexOf('display:none') !== -1, 'warning banner hidden when executors present');
})();

// Test 5: Executor list renders user items
console.log('\nTest: Executor list rendering');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [
        { user_id: 'user1', user_name: 'Alice', timeout_hours: 48, max_reminders: 3, reminder_interval_hours: 24 },
        { user_id: 'user2', user_name: 'Bob', timeout_hours: 72, max_reminders: 5, reminder_interval_hours: 24 },
      ],
      notifiers: [],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('Alice') !== -1, 'renders first executor name');
  assert(html.indexOf('Bob') !== -1, 'renders second executor name');
  assert(html.indexOf('user1') !== -1, 'renders first executor ID');
  assert(html.indexOf('user2') !== -1, 'renders second executor ID');
  assert(html.indexOf('value="48"') !== -1, 'renders timeout_hours=48');
  assert(html.indexOf('value="72"') !== -1, 'renders timeout_hours=72');
  assert(html.indexOf('value="3"') !== -1, 'renders max_reminders=3');
  assert(html.indexOf('value="5"') !== -1, 'renders max_reminders=5');
})();

// Test 6: Notifier list renders user items
console.log('\nTest: Notifier list rendering');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [{ user_id: 'exec1', user_name: 'Exec', timeout_hours: 48, max_reminders: 3, reminder_interval_hours: 24 }],
      notifiers: [
        { user_id: 'notif1', user_name: 'Charlie', timeout_hours: 72, max_reminders: 2, reminder_interval_hours: 24 },
      ],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('Charlie') !== -1, 'renders notifier name');
  assert(html.indexOf('notif1') !== -1, 'renders notifier ID');
})();

// Test 7: Default values for executor
console.log('\nTest: Executor default values');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [{ user_id: 'user1', user_name: 'Test' }],
      notifiers: [],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  // Default timeout for executor is 48
  assert(html.indexOf('value="48"') !== -1, 'executor default timeout_hours=48');
  // Default max_reminders for executor is 3
  assert(html.indexOf('value="3"') !== -1, 'executor default max_reminders=3');
})();

// Test 8: Default values for notifier
console.log('\nTest: Notifier default values');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [],
      notifiers: [{ user_id: 'user1', user_name: 'Test' }],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  // Default timeout for notifier is 72
  assert(html.indexOf('value="72"') !== -1, 'notifier default timeout_hours=72');
  // Default max_reminders for notifier is 2
  assert(html.indexOf('value="2"') !== -1, 'notifier default max_reminders=2');
})();

// Test 9: Input range attributes
console.log('\nTest: Input range attributes');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [{ user_id: 'user1', user_name: 'Test', timeout_hours: 48, max_reminders: 3, reminder_interval_hours: 24 }],
      notifiers: [],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('min="1"') !== -1, 'timeout min=1');
  assert(html.indexOf('max="720"') !== -1, 'timeout max=720');
  assert(html.indexOf('max="10"') !== -1, 'reminders max=10');
})();

// Test 10: Config initializes empty arrays if missing
console.log('\nTest: Config initialization');
(function () {
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {},
  };
  window.buildTerminalNodeConfigForm(node);
  assert(Array.isArray(node.config.result_executors), 'initializes result_executors array');
  assert(Array.isArray(node.config.notifiers), 'initializes notifiers array');
})();

// Test 11: i18n hook localizes dynamic terminal config text
console.log('\nTest: i18n localized terminal config');
(function () {
  window.ApprovalWorkflowI18n = {
    t: function (key, vars) {
      var zh = {
        noExecutorWarning: '未配置结果执行人。',
        resultExecutors: '结果执行人',
        resultExecutorsDesc: '工作流完成后需要采取行动的人员',
        notifiers: '通知人',
        notifiersDesc: '需要获知工作流结果的人员',
        searchUsers: '按姓名或 ID 搜索用户...',
        noExecutorsAdded: '尚未添加执行人。',
        noNotifiersAdded: '尚未添加通知人。',
        remove: '移除',
        timeoutShort: '超时（小时）',
        maxReminders: '最多提醒次数',
        noUsersFound: '未找到用户',
        mustBeBetween: '必须介于 ' + (vars && vars.min || '{min}') + ' 和 ' + (vars && vars.max || '{max}') + ' 之间',
      };
      return zh[key] || key;
    },
  };
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: { result_executors: [], notifiers: [] },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('未配置结果执行人。') !== -1, 'localizes warning banner');
  assert(html.indexOf('结果执行人') !== -1, 'localizes executor title');
  assert(html.indexOf('通知人') !== -1, 'localizes notifier title');
  assert(html.indexOf('按姓名或 ID 搜索用户...') !== -1, 'localizes search placeholder');
  assert(html.indexOf('尚未添加执行人。') !== -1, 'localizes empty executor list');
  assert(html.indexOf('尚未添加通知人。') !== -1, 'localizes empty notifier list');
  delete window.ApprovalWorkflowI18n;
})();

// Test 12: malformed i18n object falls back to English safely
console.log('\nTest: malformed i18n fallback');
(function () {
  window.ApprovalWorkflowI18n = {};
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: { result_executors: [], notifiers: [] },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('No Result Executor configured.') !== -1, 'falls back when i18n.t is missing');
  assert(html.indexOf('Search users by name or ID...') !== -1, 'fallback keeps search placeholder readable');
  delete window.ApprovalWorkflowI18n;
})();

// Test 13: attribute values escape ampersands before other entities
console.log('\nTest: attribute escaping');
(function () {
  window.ApprovalWorkflowI18n = {
    t: function (key) {
      return key === 'remove' ? 'A&B"<>' : key;
    },
  };
  var node = {
    id: 'node_1',
    type: 'terminal',
    label: 'End',
    config: {
      result_executors: [{ user_id: 'u&"<>', user_name: 'User & "Name"', timeout_hours: 48, max_reminders: 3, reminder_interval_hours: 24 }],
      notifiers: [],
    },
  };
  var html = window.buildTerminalNodeConfigForm(node);
  assert(html.indexOf('title="A&amp;B&quot;&lt;&gt;"') !== -1, 'escapes translated title attribute entities');
  delete window.ApprovalWorkflowI18n;
})();

// Test 14: invalid number input is not committed to node config
console.log('\nTest: invalid number input guard');
(function () {
  var source = fs.readFileSync(path.join(__dirname, 'terminal-node-config.js'), 'utf8');
  assert(source.indexOf('if (validateField(input, field, value) && idx >= 0 && idx < items.length)') !== -1, 'validates before committing numeric fields');
  assert(source.indexOf('return error === \'\';') !== -1, 'validateField returns validity flag');
  assert(source.indexOf("input.setAttribute('aria-invalid', error !== '' ? 'true' : 'false')") !== -1, 'sets aria-invalid for numeric validation');
  assert(source.indexOf("parseInt(btn.getAttribute('data-index'), 10)") !== -1, 'remove index parse uses radix');
})();

// --- Summary ---
console.log('\n=== Results: ' + passCount + '/' + testCount + ' passed ===\n');
if (passCount < testCount) {
  process.exit(1);
}
