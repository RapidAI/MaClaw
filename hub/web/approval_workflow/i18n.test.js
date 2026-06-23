/**
 * Approval workflow i18n unit tests.
 *
 * Run with: node i18n.test.js
 */

var storage = {};
var listeners = {};

function makeElement(tag, attrs) {
  attrs = attrs || {};
  return {
    tagName: tag.toUpperCase(),
    textContent: attrs.textContent || '',
    innerHTML: attrs.innerHTML || '',
    classList: {
      values: {},
      toggle: function (name, active) { this.values[name] = !!active; },
    },
    attrs: attrs,
    setAttribute: function (name, value) { this.attrs[name] = String(value); },
    getAttribute: function (name) { return this.attrs[name] || ''; },
    addEventListener: function (name, cb) { this.listener = { name: name, cb: cb }; },
  };
}

var h1 = makeElement('h1', { 'data-i18n': 'appTitle', textContent: 'Approval Workflow Designer' });
var buttonZh = makeElement('button', { 'data-set-lang': 'zh' });
var buttonEn = makeElement('button', { 'data-set-lang': 'en' });
var empty = makeElement('p', { 'data-i18n': 'emptyHint', 'data-i18n-multiline': 'true' });
var input = makeElement('input', { 'data-i18n-placeholder': 'searchUsers' });
var close = makeElement('button', { 'data-i18n-aria': 'closeNodeConfiguration' });

var document = {
  title: '',
  documentElement: { lang: '' },
  addEventListener: function (name, cb) { listeners[name] = cb; },
  querySelectorAll: function (selector) {
    if (selector === '[data-i18n]') return [h1, empty];
    if (selector === '[data-i18n-placeholder]') return [input];
    if (selector === '[data-i18n-aria]') return [close];
    if (selector === '[data-set-lang]') return [buttonZh, buttonEn];
    return [];
  },
};

var localStorage = {
  getItem: function (key) { return storage[key] || null; },
  setItem: function (key, value) { storage[key] = String(value); },
};
var navigator = { language: 'en-US' };
var window = {
  dispatchEvent: function (event) { this.lastEvent = event; },
};
function CustomEvent(name, init) { return { type: name, detail: init && init.detail }; }

var fs = require('fs');
var path = require('path');
var code = fs.readFileSync(path.join(__dirname, 'i18n.js'), 'utf8');
eval(code);

var testCount = 0;
var passCount = 0;

function assertEqual(actual, expected, message) {
  testCount++;
  if (actual === expected) {
    passCount++;
    console.log('  \u2713 ' + message);
  } else {
    console.log('  \u2717 ' + message + ' (expected: ' + JSON.stringify(expected) + ', got: ' + JSON.stringify(actual) + ')');
  }
}

console.log('\n=== Approval Workflow i18n Tests ===\n');

var i18nKeys = [
  'pageTitle', 'appTitle', 'draft', 'newWorkflow', 'newWorkflowConfirm', 'openWorkflowConfirm', 'workflowLibrary', 'refreshWorkflows',
  'workflowSearchPlaceholder', 'workflowStatusFilter', 'workflowStatusAll', 'statusDraftShort', 'statusPendingReviewShort',
  'statusPublishedShort', 'statusRejectedShort', 'statusUnpublishedShort', 'statusSupersededShort', 'statusUnknownShort', 'workflowListEmpty', 'workflowListNoMatches',
  'workflowListFailed', 'openWorkflow', 'continueWorkflow', 'reviseWorkflow', 'deleteWorkflow', 'deleteWorkflowConfirm',
  'deleteWorkflowBlocked', 'deleteWorkflowUnavailable', 'workflowDeleted', 'workflowVersionMeta', 'workflowVersionUnknown', 'reviewPreviewMode', 'reviewPreviewStatus', 'adminAuthRequired', 'validate', 'save', 'submitReview', 'nodeTypes',
  'nodeTypesHelp', 'coreNodes', 'advancedNodes',
  'workflowMetadata', 'workflowNamePlaceholder', 'workflowDescriptionPlaceholder',
  'statusDraft', 'statusLoading', 'statusLoadFailed', 'statusLoaded', 'statusSaving', 'statusSaved', 'statusUnsaved', 'statusSubmitting', 'statusPendingReview',
  'statusPublished', 'statusRejected', 'statusSuperseded', 'statusUnpublished',
  'workflowNameRequired', 'authRequired', 'requestFailed', 'invalidJsonField', 'invalidConfigField', 'configErrorSummary',
  'designWorkflow', 'emptyHint', 'draftAssistant', 'draftAssistantTitle', 'draftAssistantHint', 'draftExamples',
  'draftExampleLeave', 'draftExamplePurchase', 'draftExampleContract', 'draftExampleFullControls', 'draftPromptPlaceholder', 'generateDraft',
  'draftGenerating', 'draftGenerated', 'draftNeedDescription', 'draftGenerationCancelled', 'draftOverwriteConfirm',
  'draftGeneratedFallback', 'draftGeneratedFallbackProvider', 'draftGeneratedFallbackResponse',
  'draftExampleLeaveText', 'draftExamplePurchaseText', 'draftExampleContractText', 'draftExampleFullControlsText',
  'nodeConfiguration', 'closeNodeConfiguration',
  'canvasTools', 'selectTool', 'connectTool', 'deleteEdgeTool', 'connectHint', 'deleteEdgeHint', 'edgeConnectorLabel',
  'contextMenuLabel', 'editNode', 'duplicateNode', 'copySuffix', 'startConnection', 'deleteIncomingEdges', 'deleteOutgoingEdges',
  'deleteConnectedEdges', 'deleteEdge', 'addNodeType', 'canvasContextSelect', 'canvasContextConnect',
  'contextMenuReadOnly', 'configurationSuffix', 'deleteNode', 'label', 'description', 'triggerType', 'manual',
  'apiCall', 'schedule', 'event', 'formFieldsJson', 'approvalMode', 'singleApprover',
  'countersign', 'anyNOfM', 'sequential', 'approverIds', 'chooseApprovers',
  'chooseFallbackApprover', 'approverPickerTitle', 'fallbackApproverPickerTitle',
	'approverPickerSearch', 'approverPickerLoading', 'approverPickerEmpty',
	'approverPickerLoadFailed', 'selectedApprovers', 'clearSelection', 'cancel',
	'confirm', 'noApproverIdentity', 'virtualEmployee', 'userMachine',
	'approverViewOrganization', 'approverViewFunction', 'approverViewDirect',
	'approverRole', 'approvalRoleNotConfigured', 'departmentDigitalEmployee', 'digitalTwin',
	'applicantDepartmentScope', 'fixedDepartmentScope', 'departmentManager', 'directManager',
	'functionScopeFinance', 'functionScopeProcurement', 'functionScopeLegal', 'functionScopeIT',
	'functionScopeHR', 'functionScopeAdministration', 'functionScopeSales', 'functionScopeOperations',
	'functionScopeCustomerSuccess', 'functionScopeSecurity', 'functionScopeRiskCompliance', 'functionScopeData',
	'financeApprover', 'procurementApprover', 'contractApprover', 'itApprover', 'hrApprover',
	'roleExecutionManual', 'roleExecutionDigitalSuggest', 'roleExecutionDigitalReview', 'roleExecutionAuto',
	'minApprovals', 'timeoutHours', 'fallbackApprover', 'branchesJson', 'conditionBranches', 'addConditionBranch',
  'removeConditionBranch', 'conditionBranchLabel', 'conditionField', 'conditionOperator', 'conditionValue',
  'conditionTarget', 'conditionDefaultTarget', 'conditionNoTarget', 'conditionMissingTarget', 'conditionAdvancedJson',
  'conditionOperatorEquals', 'conditionOperatorNotEquals', 'conditionOperatorGreaterThan', 'conditionOperatorLessThan',
  'conditionOperatorContains', 'conditionOperatorInList', 'conditionOperatorNotInList', 'conditionOperatorIsEmpty',
  'conditionOperatorIsNotEmpty', 'defaultBranch', 'actionType', 'selectPlaceholder',
  'updateStatus', 'webhook', 'parametersJson', 'recipients', 'messageTemplate', 'workflowId',
  'inputMappingJson', 'resultExecutorsJson', 'notifiersJson', 'validWorkflow', 'validationErrors',
  'saveDone', 'saveChangedDuringSave', 'submitBlocked', 'submitChangedDuringSave', 'submitDone', 'requireOneTrigger', 'onlyOneTrigger',
  'disconnectedNode', 'triggerNoIncoming', 'terminalNoOutgoing',
  'approvalApproverRequired',
  'conditionBranchNoRoute', 'conditionBranchInvalidTarget', 'conditionBranchInvalidExpression',
  'language', 'trigger', 'triggerDesc', 'form',
  'formDesc', 'approval', 'approvalDesc', 'conditionBranch', 'conditionBranchDesc', 'action',
  'actionDesc', 'notification', 'notificationDesc', 'subProcess', 'subProcessDesc', 'terminal',
  'terminalDesc', 'noExecutorWarning', 'resultExecutors', 'resultExecutorsDesc', 'notifiers',
  'notifiersDesc', 'searchUsers', 'noExecutorsAdded', 'noNotifiersAdded', 'remove',
  'timeoutShort', 'maxReminders', 'noUsersFound', 'mustBeBetween'
];

function uniqueMatches(text, regex) {
  var out = [];
  var seen = {};
  var match;
  while ((match = regex.exec(text)) !== null) {
    if (!seen[match[1]]) {
      seen[match[1]] = true;
      out.push(match[1]);
    }
  }
  return out;
}

function readAsset(name) {
  return fs.readFileSync(path.join(__dirname, name), 'utf8');
}

function assertKeysCovered(keys, owner) {
  keys.forEach(function (key) {
    assertEqual(i18nKeys.indexOf(key) !== -1, true, owner + ' key is listed: ' + key);
  });
}

function assertNoRawPhrases(text, phrases, owner) {
  phrases.forEach(function (phrase) {
    assertEqual(text.indexOf(phrase) === -1, true, owner + ' has no raw UI phrase: ' + phrase);
  });
}

function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
}

var indexHTML = readAsset('index.html');
assertKeysCovered(uniqueMatches(indexHTML, /data-i18n="([^"]+)"/g), 'index.html data-i18n');
assertKeysCovered(uniqueMatches(indexHTML, /data-i18n-placeholder="([^"]+)"/g), 'index.html placeholder i18n');
assertKeysCovered(uniqueMatches(indexHTML, /data-i18n-aria="([^"]+)"/g), 'index.html aria i18n');
var workflowEditorJS = readAsset('workflow-editor.js');
var terminalConfigJS = readAsset('terminal-node-config.js');
assertKeysCovered(uniqueMatches(workflowEditorJS, /tr\('([^']+)'/g), 'workflow-editor.js tr()');
assertKeysCovered(uniqueMatches(terminalConfigJS, /tr\('([^']+)'/g), 'terminal-node-config.js tr()');
assertNoRawPhrases(stripComments(workflowEditorJS.replace(/const FALLBACK_TEXT = \{[\s\S]*?\n  \};/, '')), [
  'Workflow is valid!',
  'Validation errors:',
  'Workflow saved as ',
  'Cannot submit: please fix validation errors first.',
  'Workflow submitted for review.',
  'Exactly one Trigger node is required as the workflow entry point.',
  'Only one Trigger node is allowed.',
  'must not have incoming edges.',
  'Delete Node',
  'Approval Mode',
  'Trigger Type',
  'Form Fields (JSON)',
  'Message Template'
], 'workflow-editor.js');
assertNoRawPhrases(stripComments(terminalConfigJS.replace(/var FALLBACK_TEXT = \{[\s\S]*?\n  \};/, '')), [
  'No Result Executor configured.',
  'Result Executors',
  'People who need to take action after workflow completion',
  'Search users by name or ID...',
  'No users found',
  'Must be between '
], 'terminal-node-config.js outside fallback');

listeners.DOMContentLoaded();
assertEqual(document.title, 'Approval Workflow Designer', 'defaults to English title');
assertEqual(h1.textContent, 'Approval Workflow Designer', 'applies English static text');
assertEqual(input.attrs.placeholder, 'Search users by name or ID...', 'applies English placeholder');

window.ApprovalWorkflowI18n.setLanguage('zh');
i18nKeys.forEach(function (key) {
  assertEqual(window.ApprovalWorkflowI18n.hasTranslation('zh', key), true, 'Chinese translation exists for ' + key);
});
assertEqual(document.documentElement.lang, 'zh-CN', 'sets zh-CN html lang');
assertEqual(document.title, '\u5ba1\u6279\u5de5\u4f5c\u6d41\u8bbe\u8ba1\u5668', 'applies Chinese title');
assertEqual(h1.textContent, '\u5ba1\u6279\u5de5\u4f5c\u6d41\u8bbe\u8ba1\u5668', 'applies Chinese static text');
assertEqual(input.attrs.placeholder, '\u6309\u59d3\u540d\u6216 ID \u641c\u7d22\u7528\u6237...', 'applies Chinese placeholder');
assertEqual(close.attrs['aria-label'], '\u5173\u95ed\u8282\u70b9\u914d\u7f6e', 'applies Chinese aria label');
assertEqual(empty.innerHTML, '从左侧面板拖拽节点到画布<br>开始搭建审批工作流。', 'renders multiline text');
assertEqual(storage['maclaw-approval-workflow-lang'], 'zh', 'persists selected language');
assertEqual(window.lastEvent.type, 'approval-workflow-language-change', 'dispatches language change event');
assertEqual(buttonZh.classList.values.active, true, 'marks Chinese language button active');
assertEqual(buttonZh.attrs['aria-pressed'], 'true', 'sets Chinese language button pressed');
assertEqual(buttonEn.classList.values.active, false, 'marks English language button inactive');
assertEqual(buttonEn.attrs['aria-pressed'], 'false', 'sets English language button unpressed');

assertEqual(window.ApprovalWorkflowI18n.t('onlyOneTrigger', { count: 2 }), '\u53ea\u5141\u8bb8\u4e00\u4e2a\u89e6\u53d1\u8282\u70b9\u3002\u5f53\u524d\u53d1\u73b0 2 \u4e2a\u89e6\u53d1\u8282\u70b9\u3002', 'formats interpolated Chinese message');

window.localStorage = {
  getItem: function () { throw new Error('blocked'); },
  setItem: function () { throw new Error('blocked'); },
};
CustomEvent = undefined;
window.ApprovalWorkflowI18n.setLanguage('zh');
assertEqual(document.documentElement.lang, 'zh-CN', 'continues when localStorage and CustomEvent are unavailable');
assertEqual(window.lastEvent.type, 'approval-workflow-language-change', 'dispatches fallback language change event');
delete window.localStorage;

window.ApprovalWorkflowI18n.setLanguage('en');
i18nKeys.forEach(function (key) {
  assertEqual(window.ApprovalWorkflowI18n.hasTranslation('en', key), true, 'English translation exists for ' + key);
});
assertEqual(document.documentElement.lang, 'en', 'sets English html lang');
assertEqual(window.ApprovalWorkflowI18n.t('mustBeBetween', { min: 1, max: 10 }), 'Must be between 1 and 10', 'formats interpolated English message');
assertEqual(buttonEn.classList.values.active, true, 'marks English language button active');
assertEqual(buttonZh.classList.values.active, false, 'marks Chinese language button inactive');

console.log('\n=== Results: ' + passCount + '/' + testCount + ' passed ===\n');
if (passCount < testCount) process.exit(1);
