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
  extractFunction('workflowVersionBlocksDesignerDelete'),
  extractFunction('workflowHasPublishedHistory'),
  extractFunction('workflowVersionHistoryUnavailable'),
  extractFunction('workflowLibraryStatus')
].join('\n');

var helpers = new Function('state', helperCode + '\nreturn { latestVersion: latestVersion, parseVersionNumber: parseVersionNumber, workflowHasPublishedHistory: workflowHasPublishedHistory, workflowVersionHistoryUnavailable: workflowVersionHistoryUnavailable, workflowLibraryStatus: workflowLibraryStatus };');
var state = { workflowVersionsById: {} };
var workflowHelpers = helpers(state);

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
