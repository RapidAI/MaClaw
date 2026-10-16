/**
 * Workflow editor behavior tests for helper logic that must stay aligned with
 * backend workflow version semantics.
 *
 * Run with: node workflow-editor.test.js
 */

var fs = require('fs');
var path = require('path');
var source = fs.readFileSync(path.join(__dirname, 'workflow-editor.js'), 'utf8');

function extractFunction(name) {
  var start = source.indexOf('function ' + name + '(');
  if (start === -1) throw new Error('missing function ' + name);
  var brace = source.indexOf('{', start);
  var depth = 0;
  for (var i = brace; i < source.length; i++) {
    if (source[i] === '{') depth++;
    if (source[i] === '}') depth--;
    if (depth === 0) return source.slice(start, i + 1);
  }
  throw new Error('unterminated function ' + name);
}

var helperCode = [
  extractFunction('latestVersion'),
  extractFunction('compareWorkflowVersions'),
  extractFunction('parseVersionNumber'),
  extractFunction('clampContextMenuPosition'),
  extractFunction('isContextMenuKey'),
  extractFunction('cloneConfig'),
  extractFunction('workflowVersionBlocksDesignerDelete'),
  extractFunction('workflowHasPublishedHistory'),
  extractFunction('workflowVersionHistoryUnavailable'),
  extractFunction('workflowLibraryStatus')
].join('\n');

var helpers = new Function('state', helperCode + '\nreturn { latestVersion: latestVersion, parseVersionNumber: parseVersionNumber, clampContextMenuPosition: clampContextMenuPosition, isContextMenuKey: isContextMenuKey, cloneConfig: cloneConfig, workflowHasPublishedHistory: workflowHasPublishedHistory, workflowVersionHistoryUnavailable: workflowVersionHistoryUnavailable, workflowLibraryStatus: workflowLibraryStatus };');
var state = { workflowVersionsById: {} };
var workflowHelpers = helpers(state);

var approverHelpers = new Function('state', 'tr', [
  extractFunction('normalizeApproverIds'),
  extractFunction('formatApproverSelection'),
  extractFunction('pruneApproverPickerSelection'),
  extractFunction('normalizeEmail'),
  extractFunction('indexApproverDirectory'),
  extractFunction('approverRowsForGroup'),
  extractFunction('approverRoleCatalog'),
  extractFunction('approvalScopeHash'),
  extractFunction('approvalScopeCodeFromName'),
  extractFunction('normalizeApprovalFunctionScope'),
  extractFunction('functionScopeCatalog'),
  extractFunction('normalizeApprovalRole'),
  extractFunction('approvalRoleHasAssignees'),
  extractFunction('approvalRoleId'),
  extractFunction('approvalRoleRows'),
  extractFunction('assigneeSummary'),
  extractFunction('executionModeLabel'),
  extractFunction('approverKindLabel'),
  extractFunction('renderApproverRow')
].join('\n') + '\nreturn { normalizeApproverIds: normalizeApproverIds, formatApproverSelection: formatApproverSelection, pruneApproverPickerSelection: pruneApproverPickerSelection, indexApproverDirectory: indexApproverDirectory, approverRowsForGroup: approverRowsForGroup, approvalRoleRows: approvalRoleRows, normalizeApprovalRole: normalizeApprovalRole, normalizeApprovalFunctionScope: normalizeApprovalFunctionScope, functionScopeCatalog: functionScopeCatalog, renderApproverRow: renderApproverRow };')(
  state,
  function (key, params) {
    if (key === 'selectedApprovers') return String(params.count) + ' selected';
    if (key === 'virtualEmployee') return 'VE';
    if (key === 'userMachine') return 'Machine';
    if (key === 'approverRole') return 'Approval role';
    if (key === 'approvalRoleNotConfigured') return 'No approval role configured';
    if (key === 'approvalRolesEmptyTitle') return 'No approval roles from Hub';
    if (key === 'approvalRolesEmptyHint') return 'Configure roles in Admin';
    if (key === 'approvalRolesEmptyAction') return 'Open Hub Admin → Approval roles';
    if (key === 'approvalRolesNoAssigneesTitle') return 'Approval roles need assignees';
    if (key === 'approvalRolesNoAssigneesHint') return 'Bind people to roles';
    if (key === 'departmentDigitalEmployee') return 'Department digital employee';
    if (key === 'digitalTwin') return 'Digital twin';
    if (key === 'roleExecutionManual') return 'Manual';
    if (key === 'roleExecutionDigitalSuggest') return 'Digital suggest';
    if (key === 'roleExecutionDigitalReview') return 'Digital review';
    if (key === 'roleExecutionAuto') return 'Auto';
    if (key === 'approverPickerEmpty') return 'No approvers';
    return key;
  }
);

var pickerBindingHelpers = new Function('state', 'document', 'openApproverPicker', 'markDirty', 'tr', 'isReadOnlyPreview', [
  extractFunction('bindApproverPicker'),
  extractFunction('syncApproverPickerField'),
  extractFunction('normalizeApproverIds'),
  extractFunction('formatApproverSelection')
].join('\n') + '\nreturn { bindApproverPicker: bindApproverPicker, syncApproverPickerField: syncApproverPickerField };');

var draftHelpers = new Function('state', 'confirm', 'tr', [
  extractFunction('draftHasCanvasContent'),
  extractFunction('confirmDraftOverwriteIfNeeded'),
  extractFunction('draftDebugDetails'),
  extractFunction('draftGeneratedStatus'),
  extractFunction('draftExampleText')
].join('\n') + '\nreturn { draftHasCanvasContent: draftHasCanvasContent, confirmDraftOverwriteIfNeeded: confirmDraftOverwriteIfNeeded, draftDebugDetails: draftDebugDetails, draftGeneratedStatus: draftGeneratedStatus, draftExampleText: draftExampleText };')(
  state,
  function () { return draftHelpersConfirmResult; },
  function (key, vars) {
    if (key === 'draftGenerated') return 'Draft generated.';
    if (key === 'draftGeneratedFallback') return 'Basic draft generated because the LLM service was unavailable. In HUB System Settings, confirm the system default LLM service group, then review the workflow before saving.';
    if (key === 'draftGeneratedFallbackProvider') return 'Basic draft generated because the LLM provider request failed. Review the workflow before saving, then try again after the provider recovers.';
    if (key === 'draftGeneratedFallbackResponse') return 'Basic draft generated because the LLM response could not be applied as a workflow. Review the workflow before saving, then refine the description and try again.';
    if (key === 'draftDebugDetails') return 'Details: ' + vars.details;
    if (key === 'draftDebugServiceGroup') return 'service group';
    if (key === 'draftDebugProvider') return 'provider';
    if (key === 'draftDebugModel') return 'model';
    if (key === 'draftDebugProviderGroups') return 'provider groups';
    if (key === 'draftDebugStatus') return 'status';
    if (key === 'draftDebugError') return 'error';
    if (key === 'draftExampleFullControlsText') return 'full controls example text';
    return key;
  }
);
var draftHelpersConfirmResult = true;

var edgeHelpers = new Function('state', [
  extractFunction('canCreateEdge'),
  extractFunction('edgeNodeBox'),
  extractFunction('edgeAnchorPoints'),
  extractFunction('edgePathD')
].join('\n') + '\nreturn { canCreateEdge: canCreateEdge, edgeAnchorPoints: edgeAnchorPoints, edgePathD: edgePathD };')(state);
var graphHelpers = new Function('state', [
  extractFunction('getWorkflowGraph')
].join('\n') + '\nreturn { getWorkflowGraph: getWorkflowGraph };');
var conditionOperatorHelpers = new Function([
  extractFunction('isConditionBranchOperator')
].join('\n') + '\nreturn { isConditionBranchOperator: isConditionBranchOperator };')();
var conditionBranchEditorHelpers = new Function([
  extractFunction('normalizeConditionBranchesForEditor'),
  extractFunction('conditionBranchOperatorNeedsValue'),
  extractFunction('conditionBranchValueToInput'),
  extractFunction('parseConditionBranchValue'),
  extractFunction('formatConditionBranchSummary'),
  extractFunction('conditionBranchExpressionHasRequiredValue'),
  extractFunction('updateConditionBranchField')
].join('\n') + '\nreturn { normalizeConditionBranchesForEditor: normalizeConditionBranchesForEditor, conditionBranchOperatorNeedsValue: conditionBranchOperatorNeedsValue, conditionBranchValueToInput: conditionBranchValueToInput, parseConditionBranchValue: parseConditionBranchValue, conditionBranchExpressionHasRequiredValue: conditionBranchExpressionHasRequiredValue, updateConditionBranchField: updateConditionBranchField };')();
var conditionBranchRenderHelpers = new Function('state', 'tr', 'escapeHtml', 'escapeAttr', [
  extractFunction('renderConditionBranchTargetOptions')
].join('\n') + '\nreturn { renderConditionBranchTargetOptions: renderConditionBranchTargetOptions };')(
  state,
  function (key, params) {
    if (key === 'conditionNoTarget') return 'No target';
    if (key === 'conditionMissingTarget') return 'Missing target: ' + params.target;
    return key;
  },
  function (value) { return String(value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); },
  function (value) { return String(value).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
);
var validationHelpers = new Function('state', 'tr', 'configPanelBody', 'configFieldLabel', [
  extractFunction('validateWorkflow'),
  extractFunction('validateConditionBranchRoutes'),
  extractFunction('reachableNodeIds'),
  extractFunction('getInvalidConfigErrors'),
  extractFunction('normalizeApproverIds'),
  extractFunction('isConditionBranchOperator'),
  extractFunction('conditionBranchOperatorNeedsValue'),
  extractFunction('conditionBranchExpressionHasRequiredValue')
].join('\n') + '\nreturn { validateWorkflow: validateWorkflow };')(
  state,
  function (key, params) {
    if (key === 'approvalApproverRequired') return 'approver required: ' + params.label;
    if (key === 'requireOneTrigger') return 'trigger required';
    if (key === 'onlyOneTrigger') return 'only one trigger';
    if (key === 'disconnectedNode') return 'disconnected: ' + params.label;
    if (key === 'triggerNoIncoming') return 'trigger incoming: ' + params.label;
    if (key === 'terminalNoOutgoing') return 'terminal outgoing: ' + params.label;
    if (key === 'conditionBranchNoRoute') return 'condition no route: ' + params.label;
    if (key === 'conditionBranchInvalidTarget') return 'condition bad target: ' + params.target;
    if (key === 'conditionBranchInvalidExpression') return 'condition bad expression: ' + params.label;
    if (key === 'invalidJsonField') return 'invalid json: ' + params.field;
    if (key === 'invalidConfigField') return 'invalid config: ' + params.field;
    return key;
  },
  { querySelectorAll: function () { return []; } },
  function () { return 'field'; }
);
var nodeLookupHelpers = new Function('canvasNodes', [
  extractFunction('findCanvasNodeElement')
].join('\n') + '\nreturn { findCanvasNodeElement: findCanvasNodeElement };')({
  querySelectorAll: function () {
    return [
      { getAttribute: function () { return 'node "quoted"/1'; }, marker: 'quoted' },
      { getAttribute: function () { return 'node_2'; }, marker: 'plain' }
    ];
  }
});

var testCount = 0;
var passCount = 0;

function assertEqual(actual, expected, message) {
  testCount++;
  if (actual === expected) {
    passCount++;
    console.log('  OK ' + message);
  } else {
    console.log('  ERR ' + message + ' (expected: ' + JSON.stringify(expected) + ', got: ' + JSON.stringify(actual) + ')');
  }
}

function assertTrue(value, message) {
  assertEqual(!!value, true, message);
}

console.log('\n=== Workflow Editor Helper Tests ===\n');

var latest = workflowHelpers.latestVersion([
  { id: 'older-time-newer-version', version_number: '1.10.0', created_at: '2024-01-01T00:00:00Z' },
  { id: 'newer-time-older-version', version_number: '1.2.9', created_at: '2026-01-01T00:00:00Z' }
]);
assertEqual(latest.id, 'older-time-newer-version', 'selects highest semantic version before timestamp');

latest = workflowHelpers.latestVersion([
  { id: 'patch-9', version_number: '2.0.9', created_at: '2026-01-01T00:00:00Z' },
  { id: 'patch-10', version_number: '2.0.10', created_at: '2025-01-01T00:00:00Z' }
]);
assertEqual(latest.id, 'patch-10', 'compares numeric patch values');

latest = workflowHelpers.latestVersion([
  { id: 'old-same-version', version_number: '3.1.0', created_at: '2024-01-01T00:00:00Z' },
  { id: 'new-same-version', version_number: '3.1.0', created_at: '2025-01-01T00:00:00Z' }
]);
assertEqual(latest.id, 'new-same-version', 'uses timestamp only as same-version tie breaker');

assertEqual(workflowHelpers.parseVersionNumber('bad.version').join('.'), '0.0.0', 'invalid version parses to safe floor');
var menuPos = workflowHelpers.clampContextMenuPosition(790, 590, 180, 120, 800, 600);
assertEqual(menuPos.x + ',' + menuPos.y, '612,472', 'keeps context menu inside viewport');
menuPos = workflowHelpers.clampContextMenuPosition(-20, -10, 180, 120, 800, 600);
assertEqual(menuPos.x + ',' + menuPos.y, '8,8', 'keeps context menu away from top-left viewport edge');
assertEqual(workflowHelpers.isContextMenuKey({ key: 'ContextMenu' }), true, 'recognizes keyboard context menu key');
assertEqual(workflowHelpers.isContextMenuKey({ key: 'F10', shiftKey: true }), true, 'recognizes Shift+F10 context menu shortcut');
assertEqual(workflowHelpers.isContextMenuKey({ key: 'F10', shiftKey: false }), false, 'does not treat plain F10 as context menu shortcut');
var clonedConfig = workflowHelpers.cloneConfig({ nested: { enabled: true }, list: [1, 2] });
clonedConfig.nested.enabled = false;
assertEqual(clonedConfig.list.join(','), '1,2', 'clones node config arrays');
assertEqual(workflowHelpers.cloneConfig({ nested: { enabled: true } }).nested.enabled, true, 'deep clones node config objects');
assertTrue(source.indexOf("CONTEXT_MENU_ADD_NODE_TYPES = ['trigger', 'form', 'approval', 'condition_branch', 'action', 'notification', 'sub_process', 'terminal']") !== -1, 'canvas context menu offers every palette node type');
assertTrue(extractFunction('buildContextMenuItems').indexOf("action: 'duplicate_node'") !== -1, 'node context menu offers duplicate action');
assertTrue(extractFunction('ensureContextMenu').indexOf("menu.setAttribute('aria-label', tr('contextMenuLabel'));") !== -1, 'refreshes context menu aria label when reused');
assertTrue(extractFunction('ensureContextMenu').indexOf('e.stopPropagation();') !== -1, 'context menu Escape does not bubble to global shortcuts');
assertTrue(extractFunction('ensureContextMenu').indexOf("e.key === 'Home'") !== -1 && extractFunction('ensureContextMenu').indexOf("e.key === 'End'") !== -1, 'context menu supports Home and End keyboard navigation');
assertTrue(extractFunction('showContextMenu').indexOf('state.contextMenuReturnFocus = document.activeElement') !== -1, 'stores return focus before opening context menu');
assertTrue(extractFunction('hideContextMenu').indexOf('opts.restoreFocus') !== -1, 'context menu can restore trigger focus on close');
assertTrue(extractFunction('runContextMenuAction').indexOf('hideContextMenu({ restoreFocus: false });') !== -1, 'context menu actions do not restore stale focus before mutating graph');
assertTrue(extractFunction('runContextMenuAction').indexOf("duplicateNode(targetId);") !== -1, 'duplicate action routes through duplicateNode');
assertTrue(extractFunction('duplicateNode').indexOf('config: cloneConfig(source.config)') !== -1, 'duplicate node copies config without sharing object references');
assertTrue(extractFunction('isContextMenuOpen').indexOf('!menu.hidden') !== -1, 'detects an open context menu');
assertTrue(source.indexOf("if (isContextMenuOpen()) {\n        e.preventDefault();\n        return;\n      }") !== -1, 'global Delete and Backspace ignore open context menu without browser defaults');
assertTrue(source.indexOf("if (e.key === 'Escape') {\n      if (isContextMenuOpen()) {\n        e.preventDefault();\n        hideContextMenu({ restoreFocus: true });\n        return;\n      }") !== -1, 'global Escape closes an open context menu before clearing selection');
assertEqual(approverHelpers.normalizeApproverIds([' m1 ', 'm1', '', ' ve1 ']).join(','), 'm1,ve1', 'normalizes approver ids without duplicates');
var zhFunctionScope = approverHelpers.normalizeApprovalFunctionScope({ scopeName: '\u4eba\u4e8b' });
assertTrue(!!zhFunctionScope && /^function_[0-9a-f]{8}$/.test(zhFunctionScope.scopeId), 'normalizes non-Latin function names to stable scope ids');
assertEqual(zhFunctionScope.scopeId, approverHelpers.normalizeApprovalFunctionScope({ scopeName: ' \u4eba\u4e8b ' }).scopeId, 'non-Latin function scope ids should be deterministic');


state.approverDirectory = null;
assertEqual(approverHelpers.formatApproverSelection(['machine-secret-1'], 'Choose approvers'), '1 selected', 'hides raw approver id before directory loads');

state.approverDirectory = { byId: { 'machine-secret-1': { name: 'Alice' }, 've-secret-1': { name: 'Runtime Worker' } } };
assertEqual(approverHelpers.formatApproverSelection(['machine-secret-1', 've-secret-1'], 'Choose approvers'), 'Alice, Runtime Worker', 'renders approver names from directory');
assertEqual(approverHelpers.formatApproverSelection(['machine-secret-1', 'stale-secret-1'], 'Choose approvers'), '2 selected', 'does not hide stale selected approver ids behind partial names');
var roleDirectory = {
  functionScopes: [
    { scopeId: 'finance', scopeName: 'Finance' },
    { scopeId: 'hr', scopeName: 'HR' },
    { scopeId: 'hr_alt', scopeName: ' HR ' }
  ],
  approvalRoles: [
    approverHelpers.normalizeApprovalRole({
      scopeType: 'function',
      scopeId: 'finance',
      scopeName: 'Finance',
      roleCode: 'finance_approver',
      roleName: 'Finance Approver',
      executionMode: 'digital_review',
      assignees: [{ subjectId: 'finance-bot', displayName: 'Finance Bot' }]
    }),
    approverHelpers.normalizeApprovalRole({
      scopeType: 'department',
      scopeId: 'dept-finance',
      scopeName: 'Finance Department',
      roleCode: 'department_manager',
      roleName: 'Department Manager',
      executionMode: 'manual',
      assignees: [
        { subjectType: 'user', subjectId: 'alice@example.com', displayName: 'alice@example.com' },
        { subjectType: 'digital_twin', subjectId: 'alice-twin', displayName: 'Alice Twin' }
      ]
    })
  ],
  veEntries: [
    { id: 'finance-bot', name: 'Finance Bot', kind: 'department_digital_employee' },
    { id: 'alice-twin', name: 'Alice Twin', kind: 'digital_twin', ownerEmail: 'alice@example.com' }
  ],
  machinesByEmail: {},
  usersByEmail: {},
  byId: {}
};
approverHelpers.indexApproverDirectory(roleDirectory);
assertEqual(roleDirectory.byId['role:function:finance:finance_approver'].name, 'Finance / Finance Approver', 'indexes Hub approval roles for display');
assertEqual(approverHelpers.formatApproverSelection(['role:function:finance:finance_approver'], 'Choose approvers'), '1 selected', 'does not show role id from unrelated directory');
state.approverDirectory = roleDirectory;
assertEqual(approverHelpers.formatApproverSelection(['role:function:finance:finance_approver'], 'Choose approvers'), 'Finance / Finance Approver', 'renders approval role names from directory');
var functionRoleRows = approverHelpers.approvalRoleRows('function', roleDirectory, 'finance');
assertEqual(functionRoleRows.length, 1, 'function role view lists Hub approval roles');
assertEqual(functionRoleRows[0].detail, 'Finance Bot', 'role row summarizes configured assignees');
var functionScopeRows = approverHelpers.approvalRoleRows('function', roleDirectory, 'hr');
assertEqual(functionScopeRows.length, 1, 'function role view lists Hub function scopes without roles');
assertEqual(functionScopeRows[0].disabled, true, 'function scope without role should render disabled');
assertEqual(functionScopeRows[0].meta, 'No approval role configured', 'function scope without role explains missing role');
var emptyRoleDirectory = {
  functionScopes: [{ scopeId: 'finance', scopeName: 'Finance' }],
  approvalRoles: [
    approverHelpers.normalizeApprovalRole({
      scopeType: 'function',
      scopeId: 'finance',
      scopeName: 'Finance',
      roleCode: 'finance_approver',
      roleName: 'Finance Approver',
      assignees: []
    })
  ],
  byId: {}
};
var emptyRoleRows = approverHelpers.approvalRoleRows('function', emptyRoleDirectory, 'finance');
assertEqual(emptyRoleRows.length, 1, 'function role view should keep empty Hub role scopes visible');
assertEqual(emptyRoleRows[0].disabled, true, 'empty Hub role should not be selectable');
var noCatalogRows = approverHelpers.approvalRoleRows('function', { functionScopes: [], approvalRoles: [], byId: {} }, '');
assertEqual(noCatalogRows.length, 1, 'empty Hub catalog shows admin guide row');
assertEqual(noCatalogRows[0].disabled, true, 'admin guide row is not selectable');
assertEqual(noCatalogRows[0].name, 'No approval roles from Hub', 'admin guide title');
var noAssigneeRows = approverHelpers.approvalRoleRows('organization', emptyRoleDirectory, '');
assertEqual(noAssigneeRows.length, 1, 'roles without assignees show bind-people guide');
assertEqual(noAssigneeRows[0].name, 'Approval roles need assignees', 'no-assignees guide title');
var organizationRoleRows = approverHelpers.approvalRoleRows('organization', roleDirectory, 'alice twin');
assertEqual(organizationRoleRows.length, 1, 'organization role view searches configured assignees');
assertEqual(organizationRoleRows[0].id, 'role:department:dept-finance:department_manager', 'organization role view lists department approval roles');
state.approverDirectory = { byId: { 'machine-secret-1': { name: 'Alice' }, 've-secret-1': { name: 'Runtime Worker' } } };
state.approverPicker = { selected: { 'machine-secret-1': true, 'stale-secret-1': true } };
approverHelpers.pruneApproverPickerSelection();
assertEqual(Object.keys(state.approverPicker.selected).join(','), 'machine-secret-1', 'drops stale approver ids once directory is loaded');
var approverRows = approverHelpers.approverRowsForGroup({
  root: { id: 'root' },
  membersByGroup: { dept1: ['alice@example.com'] },
  machinesByEmail: { 'alice@example.com': [{ id: 'machine-alice', name: 'Alice laptop' }] },
  veEntries: [
    { id: 'twin-alice', name: 'Alice twin', kind: 'digital_twin', ownerEmail: 'alice@example.com', visibleGroupIds: [], approvalCapabilityEnabled: true },
    { id: 'finance-bot', name: 'Finance bot', kind: 'department_digital_employee', ownerEmail: '', visibleGroupIds: ['dept1'], approvalCapabilityEnabled: true },
    { id: 'root-bot', name: 'Root bot', kind: 'department_digital_employee', ownerEmail: '', visibleGroupIds: [], approvalCapabilityEnabled: true }
  ]
}, { id: 'dept1' });
assertEqual(approverRows.map(function (row) { return row.id; }).join(','), 'machine-alice,twin-alice,finance-bot', 'places digital twins under owners and department employees under departments');
var loadApproverDirectorySource = extractFunction('loadApproverDirectory') + '\n' + extractFunction('fetchApproverDirectory') + '\n' + extractFunction('loadAndRenderApproverDirectory');
assertTrue(loadApproverDirectorySource.indexOf('.catch(') === -1, 'loads approver directory without catch chaining');

var pickerElements = {};
function pickerEl(id) {
  if (!pickerElements[id]) {
    pickerElements[id] = {
      id: id,
      value: '',
      textContent: '',
      listener: null,
      addEventListener: function (eventName, cb) {
        if (eventName === 'click') this.listener = cb;
      }
    };
  }
  return pickerElements[id];
}
var pickerState = {
  approverDirectory: {
    byId: {
      'role:function:finance:finance_approver': { name: 'Finance / Finance Approver' },
      'role:department:dept-finance:department_manager': { name: 'Finance Department / Department Manager' }
    }
  }
};
var pickerDirtyCount = 0;
var pickerDocument = { getElementById: pickerEl };
var pickerNode = { config: { approver_ids: [], fallback_approver: '' } };
var pickerBinding = pickerBindingHelpers(
  pickerState,
  pickerDocument,
  function (options) {
    options.onConfirm(['role:function:finance:finance_approver', 'role:function:finance:finance_approver', 'role:department:dept-finance:department_manager']);
  },
  function () { pickerDirtyCount += 1; },
  function (key, params) {
    if (key === 'chooseApprovers') return 'Choose approvers';
    if (key === 'chooseFallbackApprover') return 'Choose fallback approver';
    if (key === 'selectedApprovers') return String(params.count) + ' selected';
    return key;
  },
  function () { return false; }
);
pickerBinding.bindApproverPicker(pickerNode, 'cfgApproverIdsPicker', true, function (ids) { pickerNode.config.approver_ids = ids; });
pickerEl('cfgApproverIdsPicker').listener();
assertEqual(pickerNode.config.approver_ids.join(','), 'role:function:finance:finance_approver,role:department:dept-finance:department_manager', 'approver picker writes selected role ids to node config');
assertEqual(pickerEl('cfgApproverIds').value, 'role:function:finance:finance_approver, role:department:dept-finance:department_manager', 'approver picker syncs hidden approver ids field');
assertEqual(pickerEl('cfgApproverIdsPicker').textContent, 'Finance / Finance Approver, Finance Department / Department Manager', 'approver picker syncs readable role summary');

pickerBinding = pickerBindingHelpers(
  pickerState,
  pickerDocument,
  function (options) {
    options.onConfirm(['role:function:finance:finance_approver', 'role:department:dept-finance:department_manager']);
  },
  function () { pickerDirtyCount += 1; },
  function (key, params) {
    if (key === 'chooseApprovers') return 'Choose approvers';
    if (key === 'chooseFallbackApprover') return 'Choose fallback approver';
    if (key === 'selectedApprovers') return String(params.count) + ' selected';
    return key;
  },
  function () { return false; }
);
pickerBinding.bindApproverPicker(pickerNode, 'cfgFallbackPicker', false, function (ids) { pickerNode.config.fallback_approver = ids[0] || ''; });
pickerEl('cfgFallbackPicker').listener();
assertEqual(pickerNode.config.fallback_approver, 'role:function:finance:finance_approver', 'fallback picker writes only the first selected role id');
assertEqual(pickerEl('cfgFallback').value, 'role:function:finance:finance_approver', 'fallback picker syncs hidden fallback field');
assertTrue(pickerDirtyCount >= 2, 'approver picker confirmation marks workflow dirty');

global.escapeHtml = function (value) { return String(value || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); };
global.escapeAttr = global.escapeHtml;
state.approverPicker = { selected: { 've-secret-1': true } };
var veRowHtml = approverHelpers.renderApproverRow({ id: 've-secret-1', name: 'Runtime Worker', kind: 've' });
assertTrue(veRowHtml.indexOf('Runtime Worker') !== -1 && veRowHtml.indexOf('>ve-secret-1<') === -1, 'renders VE row name without visible id');
var machineRowHtml = approverHelpers.renderApproverRow({ id: 'machine-secret-1', name: 'Alice', kind: 'machine' });
assertTrue(machineRowHtml.indexOf('Alice') !== -1 && machineRowHtml.indexOf('>machine-secret-1<') === -1, 'renders machine row name without visible id');
delete global.escapeHtml;
delete global.escapeAttr;

state.workflowVersionsById = { wf_failed: null };
assertEqual(workflowHelpers.workflowVersionHistoryUnavailable('wf_failed'), true, 'marks missing version history unavailable');
assertEqual(workflowHelpers.workflowLibraryStatus('wf_failed'), 'unknown', 'shows unknown status when version history failed');

state.workflowVersionsById = { wf_clean: [{ status: 'draft', version_number: '1.0.0' }] };
assertEqual(workflowHelpers.workflowHasPublishedHistory('wf_clean'), false, 'allows delete when only draft history exists');

state.workflowVersionsById = { wf_published: [{ status: 'superseded', version_number: '1.0.0' }] };
assertEqual(workflowHelpers.workflowHasPublishedHistory('wf_published'), true, 'blocks delete when published history exists');

var saveWorkflowDraftSource = extractFunction('saveWorkflowDraft');
var savedRevisionIndex = saveWorkflowDraftSource.indexOf('var savedRevision = state.dirtyRevision;');
var firstAwaitIndex = saveWorkflowDraftSource.indexOf('await ');
assertTrue(savedRevisionIndex !== -1 && firstAwaitIndex !== -1 && savedRevisionIndex < firstAwaitIndex, 'captures dirty revision before async save work');
assertTrue(saveWorkflowDraftSource.indexOf('ensureWorkflowDefinition(name, description)') !== -1, 'saves stable workflow metadata snapshot');
assertTrue(saveWorkflowDraftSource.indexOf('return { version: ver, clean: clearDirty(savedRevision) };') !== -1, 'reports whether save cleared dirty state');
var graphState = {
  nodes: [{
    id: 'approval-1',
    type: 'approval',
    label: 'Approval',
    position: { x: 10, y: 20 },
    config: {
      mode: 'single',
      approver_ids: ['role:function:finance:finance_approver', 'role:department:dept-finance:department_manager'],
      fallback_approver: 'role:function:legal:contract_approver'
    }
  }],
  edges: []
};
var exportedGraph = graphHelpers(graphState).getWorkflowGraph();
assertEqual(exportedGraph.nodes[0].config.approver_ids.join(','), 'role:function:finance:finance_approver,role:department:dept-finance:department_manager', 'workflow graph export preserves approval role ids');
assertEqual(exportedGraph.nodes[0].config.fallback_approver, 'role:function:legal:contract_approver', 'workflow graph export preserves fallback approval role id');
var saveClickIndex = source.indexOf("btnSave.addEventListener('click'");
var saveValidateIndex = source.indexOf('var errors = validateWorkflow();', saveClickIndex);
var saveBusyIndex = source.indexOf("setBusy(true, 'saving');", saveClickIndex);
assertTrue(saveClickIndex !== -1 && saveValidateIndex !== -1 && saveValidateIndex < saveBusyIndex, 'validates workflow structure before saving');
var validateWorkflowSource = extractFunction('validateWorkflow');
assertTrue(validateWorkflowSource.indexOf("node.type === 'terminal'") !== -1 && validateWorkflowSource.indexOf("tr('terminalNoOutgoing'") !== -1, 'rejects terminal nodes with outgoing edges');
assertTrue(validateWorkflowSource.indexOf("node.type === 'approval'") !== -1 && validateWorkflowSource.indexOf("tr('approvalApproverRequired'") !== -1, 'requires approval nodes to choose approvers or approval roles');
assertTrue(validateWorkflowSource.indexOf('validateConditionBranchRoutes(errors);') !== -1, 'validates condition branch routing before save or submit');
state.invalidConfigFields = {};
state.nodes = [
  { id: 'trigger_approval_required', type: 'trigger', label: 'Trigger', position: { x: 0, y: 0 }, config: {} },
  { id: 'approval_required', type: 'approval', label: 'Finance approval', position: { x: 100, y: 0 }, config: { approver_ids: [] } },
  { id: 'terminal_approval_required', type: 'terminal', label: 'Done', position: { x: 200, y: 0 }, config: {} }
];
state.edges = [
  { source_id: 'trigger_approval_required', target_id: 'approval_required' },
  { source_id: 'approval_required', target_id: 'terminal_approval_required' }
];
assertTrue(validationHelpers.validateWorkflow().indexOf('approver required: Finance approval') !== -1, 'validation blocks approval nodes without an approver or role');
state.nodes[1].config.approver_ids = ['role:function:finance:finance_approver'];
assertEqual(validationHelpers.validateWorkflow().indexOf('approver required: Finance approval'), -1, 'validation accepts Hub approval role references as approvers');
var validateConditionBranchRoutesSource = extractFunction('validateConditionBranchRoutes');
assertTrue(validateConditionBranchRoutesSource.indexOf("tr('conditionBranchNoRoute'") !== -1, 'requires condition branches to have a configured route');
assertTrue(validateConditionBranchRoutesSource.indexOf("tr('conditionBranchInvalidTarget'") !== -1, 'rejects condition branch targets that do not exist');
assertTrue(validateConditionBranchRoutesSource.indexOf("tr('conditionBranchInvalidExpression'") !== -1 && validateConditionBranchRoutesSource.indexOf('isConditionBranchOperator(expr.operator)') !== -1, 'rejects condition branch routes without a supported expression operator');
assertTrue(validateConditionBranchRoutesSource.indexOf('conditionBranchExpressionHasRequiredValue(expr)') !== -1, 'rejects condition branch routes with missing required values');
assertEqual(conditionOperatorHelpers.isConditionBranchOperator('greater_than'), true, 'accepts supported condition branch operators');
assertEqual(conditionOperatorHelpers.isConditionBranchOperator('>'), false, 'rejects shorthand operators that runtime cannot evaluate');
var normalizedBranches = conditionBranchEditorHelpers.normalizeConditionBranchesForEditor([
  { label: 'High value', target_node_id: 'approval_1', expression: { field: 'amount', operator: 'greater_than', value: 10000 } }
]);
assertEqual(normalizedBranches[0].condition, 'amount greater_than 10000', 'condition branch editor derives readable condition summaries');
assertEqual(conditionBranchEditorHelpers.conditionBranchValueToInput(['HR', 'Finance']), 'HR, Finance', 'condition branch editor renders list values as comma-separated text');
assertEqual(JSON.stringify(conditionBranchEditorHelpers.parseConditionBranchValue('in_list', 'HR, Finance')), JSON.stringify(['HR', 'Finance']), 'condition branch editor parses list values');
assertEqual(conditionBranchEditorHelpers.conditionBranchExpressionHasRequiredValue({ field: 'amount', operator: 'greater_than', value: 0 }), true, 'condition branch editor treats numeric zero as a configured value');
assertEqual(conditionBranchEditorHelpers.conditionBranchExpressionHasRequiredValue({ field: 'flag', operator: 'equals', value: false }), true, 'condition branch editor treats boolean false as a configured value');
assertEqual(conditionBranchEditorHelpers.conditionBranchExpressionHasRequiredValue({ field: 'amount', operator: 'in_list', value: [0] }), true, 'condition branch editor treats numeric zero list entries as configured values');
assertEqual(conditionBranchEditorHelpers.conditionBranchExpressionHasRequiredValue({ field: 'amount', operator: 'in_list', value: [null, ''] }), false, 'condition branch editor rejects empty list values');
assertEqual(conditionBranchEditorHelpers.conditionBranchExpressionHasRequiredValue({ field: 'amount', operator: 'greater_than', value: '' }), false, 'condition branch editor rejects empty values for comparison operators');
assertEqual(conditionBranchEditorHelpers.conditionBranchExpressionHasRequiredValue({ field: 'comment', operator: 'is_empty' }), true, 'condition branch editor allows empty-check operators without values');
var editableConditionNode = { config: { branches: normalizedBranches } };
conditionBranchEditorHelpers.updateConditionBranchField(editableConditionNode, 0, 'operator', 'is_empty');
assertEqual(Object.prototype.hasOwnProperty.call(editableConditionNode.config.branches[0].expression, 'value'), false, 'condition branch editor drops values for empty checks');
conditionBranchEditorHelpers.updateConditionBranchField(editableConditionNode, 0, 'operator', 'less_than');
conditionBranchEditorHelpers.updateConditionBranchField(editableConditionNode, 0, 'value', '3');
assertEqual(editableConditionNode.config.branches[0].expression.value, 3, 'condition branch editor writes numeric values back to expression');
conditionBranchEditorHelpers.updateConditionBranchField(editableConditionNode, 0, 'target_node_id', 'hr_approval');
assertEqual(editableConditionNode.config.branches[0].target_node_id, 'hr_approval', 'condition branch editor writes selected target node ids');
state.nodes = [{ id: 'condition_1', type: 'condition_branch', label: 'Condition' }, { id: 'approval_1', type: 'approval', label: 'Approval' }];
var missingTargetOptions = conditionBranchRenderHelpers.renderConditionBranchTargetOptions('missing_node', 'condition_1');
assertTrue(missingTargetOptions.indexOf('value="missing_node" selected') !== -1 && missingTargetOptions.indexOf('Missing target: missing_node') !== -1, 'condition branch target selector preserves missing configured targets');
assertEqual(nodeLookupHelpers.findCanvasNodeElement('node "quoted"/1').marker, 'quoted', 'finds canvas nodes with selector-sensitive ids');
var addEdgeSource = extractFunction('addEdge');
assertTrue(addEdgeSource.indexOf('canCreateEdge(sourceId, targetId)') !== -1, 'validates edge structure before adding canvas edges');
state.nodes = [
  { id: 'trigger_1', type: 'trigger' },
  { id: 'approval_1', type: 'approval' },
  { id: 'terminal_1', type: 'terminal' }
];
assertEqual(edgeHelpers.canCreateEdge('trigger_1', 'approval_1'), true, 'allows a normal trigger to approval edge');
assertEqual(edgeHelpers.canCreateEdge('terminal_1', 'approval_1'), false, 'prevents creating outgoing edges from terminal nodes');
assertEqual(edgeHelpers.canCreateEdge('approval_1', 'trigger_1'), false, 'prevents creating incoming edges to trigger nodes');
assertEqual(edgeHelpers.canCreateEdge('approval_1', 'approval_1'), false, 'prevents self-loop edges');
assertEqual(edgeHelpers.canCreateEdge('missing', 'approval_1'), false, 'prevents edges with missing source nodes');

var generateWorkflowDraftSource = extractFunction('generateWorkflowDraftFromPrompt');
var setDraftAssistantStatusSource = extractFunction('setDraftAssistantStatus');
var overwriteConfirmIndex = generateWorkflowDraftSource.indexOf('if (!confirmDraftOverwriteIfNeeded()) {');
var cancelStatusIndex = generateWorkflowDraftSource.indexOf("setDraftAssistantStatus(tr('draftGenerationCancelled'));");
var draftApiIndex = generateWorkflowDraftSource.indexOf("workflowApi('/api/v1/workflow-drafts/generate'");
var applyGeneratedGraphIndex = generateWorkflowDraftSource.indexOf('applyWorkflowGraph(data.graph || { nodes: [], edges: [] });');
var generatedStatusIndex = generateWorkflowDraftSource.indexOf('setDraftAssistantStatus(draftGeneratedStatus(data),');
assertTrue(overwriteConfirmIndex !== -1, 'asks before overwriting an existing workflow draft');
assertTrue(draftApiIndex !== -1, 'generates workflow drafts through the LLM draft endpoint');
assertTrue(overwriteConfirmIndex < draftApiIndex, 'confirms overwrite before calling the LLM draft endpoint');
assertTrue(applyGeneratedGraphIndex > draftApiIndex, 'fills the canvas with the generated draft graph');
assertTrue(generatedStatusIndex > applyGeneratedGraphIndex, 'shows generated draft notes after applying the graph');
assertTrue(generateWorkflowDraftSource.indexOf("body: JSON.stringify({\n          description: description,") !== -1, 'sends the natural-language workflow description to draft generation');
assertTrue(generateWorkflowDraftSource.indexOf("if (workflowNameInput && data.name && !getWorkflowName()) workflowNameInput.value = data.name;") !== -1, 'keeps an existing workflow name when applying a generated draft');
assertTrue(cancelStatusIndex !== -1 && cancelStatusIndex < draftApiIndex, 'reports when draft generation is canceled before overwrite');
assertTrue(setDraftAssistantStatusSource.indexOf("setAttribute('title', text)") !== -1 && setDraftAssistantStatusSource.indexOf("removeAttribute('title')") !== -1, 'keeps full draft assistant status available when visually truncated');
state.nodes = [];
state.edges = [];
draftHelpersConfirmResult = false;
assertEqual(draftHelpers.draftHasCanvasContent(), false, 'draft canvas content check is false for an empty canvas');
assertEqual(draftHelpers.confirmDraftOverwriteIfNeeded(), true, 'empty canvas skips overwrite confirmation');
state.nodes = [{ id: 'node_1' }];
state.edges = [];
draftHelpersConfirmResult = false;
assertEqual(draftHelpers.draftHasCanvasContent(), true, 'draft canvas content check detects existing nodes');
assertEqual(draftHelpers.confirmDraftOverwriteIfNeeded(), false, 'existing canvas respects overwrite cancellation');
draftHelpersConfirmResult = true;
assertEqual(draftHelpers.confirmDraftOverwriteIfNeeded(), true, 'existing canvas proceeds after overwrite confirmation');
assertEqual(draftHelpers.draftGeneratedStatus({}), 'Draft generated.', 'generated draft status works without notes');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback' }), 'Basic draft generated because the LLM service was unavailable. In HUB System Settings, confirm the system default LLM service group, then review the workflow before saving.', 'generated draft status distinguishes fallback drafts');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', fallback_reason: 'llm_settings' }), 'Basic draft generated because the LLM service was unavailable. In HUB System Settings, confirm the system default LLM service group, then review the workflow before saving.', 'settings fallback status points to HUB System Settings');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', fallback_reason: 'llm_route' }), 'Basic draft generated because the LLM service was unavailable. In HUB System Settings, confirm the system default LLM service group, then review the workflow before saving.', 'route fallback status points to HUB System Settings');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', fallback_reason: 'llm_provider' }), 'Basic draft generated because the LLM provider request failed. Review the workflow before saving, then try again after the provider recovers.', 'provider fallback status does not blame settings');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', fallback_reason: 'llm_provider', debug: { service_group_id: 'system-free', provider_id: 'deepseek', model: 'auto', provider_service_group_ids: ['system-free'], status_code: 550, response: 'context canceled' } }), 'Basic draft generated because the LLM provider request failed. Review the workflow before saving, then try again after the provider recovers. Details: service group: system-free; provider: deepseek; model: auto; provider groups: system-free; status: 550; error: context canceled', 'provider fallback status includes backend debug details');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', fallback_reason: 'llm_response' }), 'Basic draft generated because the LLM response could not be applied as a workflow. Review the workflow before saving, then refine the description and try again.', 'response fallback status asks user to refine description');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', notes: ['LLM draft generation was unavailable, so a basic fallback draft was generated.'] }), 'Basic draft generated because the LLM service was unavailable. In HUB System Settings, confirm the system default LLM service group, then review the workflow before saving.', 'fallback draft status does not append backend debug notes');
assertEqual(draftHelpers.draftGeneratedStatus({ notes: ['Select real approvers before saving.'] }), 'Draft generated. Select real approvers before saving.', 'generated draft status includes first LLM note');
assertEqual(draftHelpers.draftGeneratedStatus({ notes: [Array(130).join('x')] }).length, 'Draft generated. '.length + 120, 'generated draft status truncates long LLM notes');
assertEqual(draftHelpers.draftExampleText('fullControls'), 'full controls example text', 'all-controls draft example fills the prompt through the example map');

var rightEdge = edgeHelpers.edgeAnchorPoints(
  { position: { x: 80, y: 100 } },
  { offsetWidth: 160, offsetHeight: 64 },
  { position: { x: 320, y: 108 } },
  { offsetWidth: 160, offsetHeight: 64 }
);
assertEqual(rightEdge.orientation, 'horizontal', 'uses horizontal anchors when generated draft nodes are laid out side by side');
assertEqual(rightEdge.sx + ',' + rightEdge.sy, '240,132', 'horizontal edge leaves the source node right side');
assertEqual(rightEdge.tx + ',' + rightEdge.ty, '320,140', 'horizontal edge enters the target node left side');
assertTrue(edgeHelpers.edgePathD(rightEdge).indexOf('C 280 132, 280 140, 320 140') !== -1, 'horizontal edge path keeps the arrow outside node bodies');

var leftEdge = edgeHelpers.edgeAnchorPoints(
  { position: { x: 360, y: 100 } },
  { offsetWidth: 160, offsetHeight: 64 },
  { position: { x: 80, y: 100 } },
  { offsetWidth: 160, offsetHeight: 64 }
);
assertEqual(leftEdge.sx + ',' + leftEdge.sy, '360,132', 'reverse horizontal edge leaves the source node left side');
assertEqual(leftEdge.tx + ',' + leftEdge.ty, '240,132', 'reverse horizontal edge enters the target node right side');

var downEdge = edgeHelpers.edgeAnchorPoints(
  { position: { x: 120, y: 80 } },
  { offsetWidth: 160, offsetHeight: 64 },
  { position: { x: 130, y: 260 } },
  { offsetWidth: 160, offsetHeight: 64 }
);
assertEqual(downEdge.orientation, 'vertical', 'uses vertical anchors when target is mostly below source');
assertEqual(downEdge.sx + ',' + downEdge.sy, '200,144', 'vertical edge leaves the source node bottom side');
assertEqual(downEdge.tx + ',' + downEdge.ty, '210,260', 'vertical edge enters the target node top side');
assertTrue(extractFunction('renderEdges').indexOf('ensureEdgeArrowhead(svg);') !== -1, 'defines the arrow marker before rendering edge paths');
assertTrue(extractFunction('ensureEdgeArrowhead').indexOf('overflow="visible"') !== -1, 'keeps arrowheads visible at node boundaries');

var getWorkflowAuthSource = extractFunction('getWorkflowAuth');
assertTrue(getWorkflowAuthSource.indexOf('scrubWorkflowAuthFromLocation()') !== -1, 'scrubs machine credentials from URL after capture');

var storedAuth = {};
var scrubbedAuth = false;
var getWorkflowAuth = new Function('getUrlParam', 'storageGet', 'storageSet', 'storageRemove', 'scrubWorkflowAuthFromLocation', getWorkflowAuthSource + '\nreturn getWorkflowAuth;')(
  function (name) { return name === 'machine_id' ? ' machine-from-url ' : name === 'token' ? ' token-from-url ' : ''; },
  function (key) { return storedAuth[key] || ''; },
  function (key, value) { storedAuth[key] = value; },
  function (key) { delete storedAuth[key]; },
  function () { scrubbedAuth = true; }
);
var auth = getWorkflowAuth();
assertEqual(auth.machineID, 'machine-from-url', 'reads and trims workflow machine id from URL before storage');
assertEqual(auth.token, 'token-from-url', 'reads and trims workflow token from URL before storage');
assertEqual(storedAuth['maclaw-approval-workflow-machine-id'], 'machine-from-url', 'persists workflow machine id for reload');
assertEqual(storedAuth['maclaw-approval-workflow-machine-token'], 'token-from-url', 'persists workflow machine token for reload');
assertEqual(scrubbedAuth, true, 'scrubs URL after capturing workflow auth');

storedAuth = {
  'maclaw-approval-workflow-machine-id': 'stale-machine',
  'maclaw-approval-workflow-machine-token': 'stale-token'
};
scrubbedAuth = false;
getWorkflowAuth = new Function('getUrlParam', 'storageGet', 'storageSet', 'storageRemove', 'scrubWorkflowAuthFromLocation', getWorkflowAuthSource + '\nreturn getWorkflowAuth;')(
  function (name) { return name === 'machine_id' ? '   ' : name === 'token' ? '   ' : ''; },
  function (key) { return storedAuth[key] || ''; },
  function (key, value) { storedAuth[key] = value; },
  function (key) { delete storedAuth[key]; },
  function () { scrubbedAuth = true; }
);
auth = getWorkflowAuth();
assertEqual(auth.machineID, '', 'blank workflow machine id is normalized to empty');
assertEqual(auth.token, '', 'blank workflow token is normalized to empty');
assertEqual(storedAuth['maclaw-approval-workflow-machine-id'], undefined, 'blank workflow machine id clears stale storage');
assertEqual(storedAuth['maclaw-approval-workflow-machine-token'], undefined, 'blank workflow token clears stale storage');
assertEqual(scrubbedAuth, true, 'blank URL credentials still trigger URL scrub');

storedAuth = {
  'maclaw-approval-workflow-machine-id': ' stored-machine ',
  'maclaw-approval-workflow-machine-token': ' stored-token '
};
scrubbedAuth = false;
getWorkflowAuth = new Function('getUrlParam', 'storageGet', 'storageSet', 'storageRemove', 'scrubWorkflowAuthFromLocation', getWorkflowAuthSource + '\nreturn getWorkflowAuth;')(
  function () { return ''; },
  function (key) { return storedAuth[key] || ''; },
  function (key, value) { storedAuth[key] = value; },
  function (key) { delete storedAuth[key]; },
  function () { scrubbedAuth = true; }
);
auth = getWorkflowAuth();
assertEqual(auth.machineID, 'stored-machine', 'falls back to trimmed stored workflow machine id');
assertEqual(auth.token, 'stored-token', 'falls back to trimmed stored workflow token');
assertEqual(storedAuth['maclaw-approval-workflow-machine-id'], 'stored-machine', 'rewrites stored machine id in normalized form');
assertEqual(storedAuth['maclaw-approval-workflow-machine-token'], 'stored-token', 'rewrites stored token in normalized form');

var scrubWorkflowAuthFromLocation = new Function(extractFunction('scrubWorkflowAuthFromLocation') + '\nreturn scrubWorkflowAuthFromLocation;')();
var replacedURL = null;
global.document = { title: 'Approval Workflow Designer' };
global.window = {
  location: { href: 'https://hub.example/approval_workflow/?workflow_id=wf1&token=secret#machine_id=m1&machine_token=secret2&review_version_id=v1' },
  history: {
    state: { page: 'designer' },
    replaceState: function (_state, _title, url) { replacedURL = url; }
  }
};
scrubWorkflowAuthFromLocation();
assertEqual(replacedURL, '/approval_workflow/?workflow_id=wf1#review_version_id=v1', 'removes machine credentials from query and hash but keeps route params');

replacedURL = null;
global.window.location.href = 'https://hub.example/approval_workflow/?workflow_id=wf1#review_version_id=v1';
scrubWorkflowAuthFromLocation();
assertEqual(replacedURL, null, 'does not rewrite URL when no machine credentials are present');
delete global.window;
delete global.document;

console.log('\n=== Results: ' + passCount + '/' + testCount + ' passed ===\n');
if (passCount < testCount) process.exit(1);
