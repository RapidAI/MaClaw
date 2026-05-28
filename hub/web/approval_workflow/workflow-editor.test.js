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

console.log('\n=== Results: ' + passCount + '/' + testCount + ' passed ===\n');
if (passCount < testCount) process.exit(1);
