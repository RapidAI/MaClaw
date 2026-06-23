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
  extractFunction('approverRowsForGroup'),
  extractFunction('approverKindLabel'),
  extractFunction('renderApproverRow')
].join('\n') + '\nreturn { normalizeApproverIds: normalizeApproverIds, formatApproverSelection: formatApproverSelection, pruneApproverPickerSelection: pruneApproverPickerSelection, approverRowsForGroup: approverRowsForGroup, renderApproverRow: renderApproverRow };')(
  state,
  function (key, params) {
    if (key === 'selectedApprovers') return String(params.count) + ' selected';
    if (key === 'virtualEmployee') return 'VE';
    if (key === 'userMachine') return 'Machine';
    if (key === 'approverRole') return 'Approval role';
    if (key === 'departmentDigitalEmployee') return 'Department digital employee';
    if (key === 'digitalTwin') return 'Digital twin';
    return key;
  }
);

var draftHelpers = new Function('state', 'confirm', 'tr', [
  extractFunction('draftHasCanvasContent'),
  extractFunction('confirmDraftOverwriteIfNeeded'),
  extractFunction('draftGeneratedStatus')
].join('\n') + '\nreturn { draftHasCanvasContent: draftHasCanvasContent, confirmDraftOverwriteIfNeeded: confirmDraftOverwriteIfNeeded, draftGeneratedStatus: draftGeneratedStatus };')(
  state,
  function () { return draftHelpersConfirmResult; },
  function (key) {
    if (key === 'draftGenerated') return 'Draft generated.';
    if (key === 'draftGeneratedFallback') return 'Basic draft generated because the LLM service was unavailable. Review and adjust the workflow before saving.';
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
var conditionOperatorHelpers = new Function([
  extractFunction('isConditionBranchOperator')
].join('\n') + '\nreturn { isConditionBranchOperator: isConditionBranchOperator };')();
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
    console.log('  ✓ ' + message);
  } else {
    console.log('  ✗ ' + message + ' (expected: ' + JSON.stringify(expected) + ', got: ' + JSON.stringify(actual) + ')');
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

state.approverDirectory = null;
assertEqual(approverHelpers.formatApproverSelection(['machine-secret-1'], 'Choose approvers'), '1 selected', 'hides raw approver id before directory loads');

state.approverDirectory = { byId: { 'machine-secret-1': { name: 'Alice' }, 've-secret-1': { name: 'Runtime Worker' } } };
assertEqual(approverHelpers.formatApproverSelection(['machine-secret-1', 've-secret-1'], 'Choose approvers'), 'Alice, Runtime Worker', 'renders approver names from directory');
assertEqual(approverHelpers.formatApproverSelection(['machine-secret-1', 'stale-secret-1'], 'Choose approvers'), '2 selected', 'does not hide stale selected approver ids behind partial names');
state.approverPicker = { selected: { 'machine-secret-1': true, 'stale-secret-1': true } };
approverHelpers.pruneApproverPickerSelection();
assertEqual(Object.keys(state.approverPicker.selected).join(','), 'machine-secret-1', 'drops stale approver ids once directory is loaded');
var approverRows = approverHelpers.approverRowsForGroup({
  root: { id: 'root' },
  membersByGroup: { dept1: ['alice@example.com'] },
  machinesByEmail: { 'alice@example.com': [{ id: 'machine-alice', name: 'Alice laptop' }] },
  veEntries: [
    { id: 'twin-alice', name: 'Alice twin', kind: 'digital_twin', ownerEmail: 'alice@example.com', visibleGroupIds: [] },
    { id: 'finance-bot', name: 'Finance bot', kind: 'department_digital_employee', ownerEmail: '', visibleGroupIds: ['dept1'] },
    { id: 'root-bot', name: 'Root bot', kind: 'department_digital_employee', ownerEmail: '', visibleGroupIds: [] }
  ]
}, { id: 'dept1' });
assertEqual(approverRows.map(function (row) { return row.id; }).join(','), 'machine-alice,twin-alice,finance-bot', 'places digital twins under owners and department employees under departments');
var loadApproverDirectorySource = extractFunction('loadApproverDirectory') + '\n' + extractFunction('fetchApproverDirectory') + '\n' + extractFunction('loadAndRenderApproverDirectory');
assertTrue(loadApproverDirectorySource.indexOf('.catch(') === -1, 'loads approver directory without catch chaining');

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
var saveClickIndex = source.indexOf("btnSave.addEventListener('click'");
var saveValidateIndex = source.indexOf('var errors = validateWorkflow();', saveClickIndex);
var saveBusyIndex = source.indexOf("setBusy(true, 'saving');", saveClickIndex);
assertTrue(saveClickIndex !== -1 && saveValidateIndex !== -1 && saveValidateIndex < saveBusyIndex, 'validates workflow structure before saving');
var validateWorkflowSource = extractFunction('validateWorkflow');
assertTrue(validateWorkflowSource.indexOf("node.type === 'terminal'") !== -1 && validateWorkflowSource.indexOf("tr('terminalNoOutgoing'") !== -1, 'rejects terminal nodes with outgoing edges');
assertTrue(validateWorkflowSource.indexOf('validateConditionBranchRoutes(errors);') !== -1, 'validates condition branch routing before save or submit');
var validateConditionBranchRoutesSource = extractFunction('validateConditionBranchRoutes');
assertTrue(validateConditionBranchRoutesSource.indexOf("tr('conditionBranchNoRoute'") !== -1, 'requires condition branches to have a configured route');
assertTrue(validateConditionBranchRoutesSource.indexOf("tr('conditionBranchInvalidTarget'") !== -1, 'rejects condition branch targets that do not exist');
assertTrue(validateConditionBranchRoutesSource.indexOf("tr('conditionBranchInvalidExpression'") !== -1 && validateConditionBranchRoutesSource.indexOf('isConditionBranchOperator(expr.operator)') !== -1, 'rejects condition branch routes without a supported expression operator');
assertEqual(conditionOperatorHelpers.isConditionBranchOperator('greater_than'), true, 'accepts supported condition branch operators');
assertEqual(conditionOperatorHelpers.isConditionBranchOperator('>'), false, 'rejects shorthand operators that runtime cannot evaluate');
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
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback' }), 'Basic draft generated because the LLM service was unavailable. Review and adjust the workflow before saving.', 'generated draft status distinguishes fallback drafts');
assertEqual(draftHelpers.draftGeneratedStatus({ generated_by: 'fallback', notes: ['LLM draft generation was unavailable, so a basic fallback draft was generated.'] }), 'Basic draft generated because the LLM service was unavailable. Review and adjust the workflow before saving.', 'fallback draft status does not append backend debug notes');
assertEqual(draftHelpers.draftGeneratedStatus({ notes: ['Select real approvers before saving.'] }), 'Draft generated. Select real approvers before saving.', 'generated draft status includes first LLM note');
assertEqual(draftHelpers.draftGeneratedStatus({ notes: [Array(130).join('x')] }).length, 'Draft generated. '.length + 120, 'generated draft status truncates long LLM notes');

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
