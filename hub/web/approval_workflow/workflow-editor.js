/**
 * Workflow Editor - Canvas-based drag-and-drop workflow designer.
 *
 * Implements:
 * - Drag-and-drop node placement from palette to canvas
 * - Node selection and configuration panel display (within 500ms)
 * - Edge connections between nodes
 * - Workflow graph model matching Go backend's WorkflowGraph struct
 *
 * Graph model:
 *   { nodes: [{id, type, label, position: {x, y}, config: {}}], edges: [{id, source_id, target_id}] }
 */
(function () {
  'use strict';

  // --- State ---
  const state = {
    nodes: [],       // WorkflowNode[]
    edges: [],       // WorkflowEdge[]
    selectedNodeId: null,
    workflowId: null,
    versionId: null,
    versionNumber: '',
    versionStatus: 'draft',
    workflowSummaries: [],
    workflowVersionsById: {},
    reviewVersionId: null,
    isReadOnlyPreview: false,
    workflowSearch: '',
    workflowStatusFilter: '',
    isDirty: false,
    dirtyRevision: 0,
    isBusy: false,
    isLibraryLoading: false,
    libraryRequestId: 0,
    invalidConfigFields: {},
    approverPicker: null,
    approverDirectory: null,
    approverDirectoryLoading: false,
    nextNodeId: 1,
    nextEdgeId: 1,
    toolMode: 'select',
    draggingNodeType: null,
    connectingFrom: null, // node ID when drawing an edge
    selectedEdgeId: null,
    contextMenu: null,
    contextMenuReturnFocus: null,
    isGeneratingDraft: false,
  };

  // --- DOM refs ---
  const canvasContainer = document.getElementById('canvasContainer');
  const canvasArea = document.getElementById('canvasArea');
  const canvasNodes = document.getElementById('canvasNodes');
  const canvasEdges = document.getElementById('canvasEdges');
  const dropIndicator = document.getElementById('dropIndicator');
  const canvasEmpty = document.getElementById('canvasEmpty');
  const configPanel = document.getElementById('configPanel');
  const configPanelTitle = document.getElementById('configPanelTitle');
  const configPanelBody = document.getElementById('configPanelBody');
  const configPanelClose = document.getElementById('configPanelClose');
  const btnNew = document.getElementById('btnNew');
  const btnRefreshWorkflows = document.getElementById('btnRefreshWorkflows');
  const btnValidate = document.getElementById('btnValidate');
  const btnSave = document.getElementById('btnSave');
  const btnSubmit = document.getElementById('btnSubmit');
  const canvasToolHint = document.getElementById('canvasToolHint');
  const workflowNameInput = document.getElementById('workflowName');
  const workflowDescriptionInput = document.getElementById('workflowDescription');
  const workflowStatus = document.getElementById('workflowStatus');
  const versionBadge = document.getElementById('versionBadge');
  const workflowList = document.getElementById('workflowList');
  const workflowSearchInput = document.getElementById('workflowSearch');
  const workflowStatusFilter = document.getElementById('workflowStatusFilter');
  const reviewPreviewBanner = document.getElementById('reviewPreviewBanner');
  const draftPrompt = document.getElementById('draftPrompt');
  const btnGenerateDraft = document.getElementById('btnGenerateDraft');
  const draftAssistantStatus = document.getElementById('draftAssistantStatus');

  const FALLBACK_TEXT = {
    pageTitle: 'Approval Workflow Designer',
    configurationSuffix: 'Configuration',
    newWorkflow: 'New',
    newWorkflowConfirm: 'Start a new workflow? Unsaved changes will be lost.',
    openWorkflowConfirm: 'Open this workflow design? Unsaved changes will be lost.',
    workflowLibrary: 'My Workflows',
    refreshWorkflows: 'Refresh',
    workflowSearchPlaceholder: 'Search workflows',
    workflowStatusFilter: 'Workflow status',
    workflowStatusAll: 'All statuses',
    statusDraftShort: 'Draft',
    statusPendingReviewShort: 'Pending',
    statusPublishedShort: 'Published',
    statusRejectedShort: 'Rejected',
    statusUnpublishedShort: 'Unpublished',
    statusSupersededShort: 'Superseded',
    statusUnknownShort: 'Unknown',
    workflowListEmpty: 'No saved workflow designs yet.',
    workflowListNoMatches: 'No workflow designs match these filters.',
    workflowListFailed: 'Workflow list failed: {error}',
    openWorkflow: 'Open',
    continueWorkflow: 'Continue',
    reviseWorkflow: 'Revise',
    deleteWorkflow: 'Delete',
    deleteWorkflowConfirm: 'Delete workflow design "{name}"? Published capability market skills are not removed here.',
    deleteWorkflowBlocked: 'Published or previously published workflows cannot be deleted from the designer.',
    deleteWorkflowUnavailable: 'Cannot verify publish history. Refresh before deleting.',
    workflowDeleted: 'Workflow design deleted.',
    workflowVersionMeta: '{status} {version}',
    workflowVersionUnknown: 'Version history unavailable',
    reviewPreviewMode: 'Review preview mode. This workflow is read-only here.',
    reviewPreviewStatus: 'Review preview {version}',
    adminAuthRequired: 'Admin authorization required. Sign in to the Hub admin console first.',
    deleteNode: 'Delete Node',
    label: 'Label',
    description: 'Description',
    triggerType: 'Trigger Type',
    manual: 'Manual',
    apiCall: 'API Call',
    schedule: 'Schedule',
    event: 'Event',
    formFieldsJson: 'Form Fields (JSON)',
    approvalMode: 'Approval Mode',
    singleApprover: 'Single Approver',
    countersign: 'Countersign (All must approve)',
    anyNOfM: 'Any N of M',
    sequential: 'Sequential',
    approverIds: 'Approvers',
    chooseApprovers: 'Choose approvers',
    chooseFallbackApprover: 'Choose fallback approver',
    approverPickerTitle: 'Choose approvers from organization',
    fallbackApproverPickerTitle: 'Choose fallback approver from organization',
    approverPickerSearch: 'Search department or user',
    approverPickerLoading: 'Loading organization...',
    approverPickerEmpty: 'No selectable approvers in this department.',
    approverPickerLoadFailed: 'Load organization failed: {error}',
    selectedApprovers: '{count} selected',
    approverViewOrganization: 'By organization',
    approverViewFunction: 'By function',
    approverViewDirect: 'Direct members',
    approverRole: 'Approval role',
    departmentDigitalEmployee: 'Department digital employee',
    digitalTwin: 'Digital twin',
    functionScopeFinance: 'Finance',
    functionScopeProcurement: 'Procurement',
    functionScopeLegal: 'Legal',
    functionScopeIT: 'IT',
    departmentManager: 'Department manager',
    directManager: 'Direct manager',
    financeApprover: 'Finance approver',
    procurementApprover: 'Procurement approver',
    contractApprover: 'Contract approver',
    itApprover: 'IT approver',
    applicantDepartmentScope: 'Applicant department',
    fixedDepartmentScope: 'Fixed department',
    roleExecutionManual: 'Manual approval',
    roleExecutionDigitalSuggest: 'Digital suggestion + human confirmation',
    roleExecutionDigitalReview: 'Digital pre-review + human confirmation',
    roleExecutionAuto: 'Automatic approval',
    clearSelection: 'Clear',
    cancel: 'Cancel',
    confirm: 'Confirm',
    noApproverIdentity: 'No online approver identity found',
    virtualEmployee: 'VE',
    userMachine: 'Machine',
    minApprovals: 'Min Approvals (for Any N of M)',
    timeoutHours: 'Timeout (hours, 1-720)',
    fallbackApprover: 'Fallback Approver',
    branchesJson: 'Branches (JSON)',
    defaultBranch: 'Default Branch (target node ID)',
    actionType: 'Action Type',
    selectPlaceholder: 'Select...',
    updateStatus: 'Update Status',
    webhook: 'Webhook',
    parametersJson: 'Parameters (JSON)',
    recipients: 'Recipients (comma-separated)',
    messageTemplate: 'Message Template',
    workflowId: 'Workflow ID',
    inputMappingJson: 'Input Mapping (JSON)',
    resultExecutorsJson: 'Result Executors (JSON)',
    notifiersJson: 'Notifiers (JSON)',
    validWorkflow: 'Workflow is valid!',
    validationErrors: 'Validation errors:\n\n{errors}',
    saveDone: 'Workflow saved as {version}.',
    saveChangedDuringSave: 'Workflow saved, but newer unsaved changes remain.',
    submitBlocked: 'Cannot submit: please fix validation errors first.\n\n{errors}',
    submitChangedDuringSave: 'Workflow changed while saving. Review changes and submit again.',
    submitDone: 'Workflow submitted for review.',
    statusDraft: 'Draft not saved',
    statusLoading: 'Loading workflow...',
    statusLoadFailed: 'Workflow load failed: {error}',
    statusLoaded: 'Loaded {version}',
    statusSaving: 'Saving...',
    statusSaved: 'Saved {version}',
    statusUnsaved: 'Unsaved changes',
    statusSubmitting: 'Submitting...',
    statusPendingReview: 'Pending review {version}',
    statusPublished: 'Published {version}',
    statusRejected: 'Rejected {version}',
    statusSuperseded: 'Superseded {version}',
    statusUnpublished: 'Unpublished {version}',
    workflowNameRequired: 'Workflow name is required before saving.',
    authRequired: 'Machine authorization required. Open with machine_id and token query parameters, or save them in localStorage.',
    requestFailed: 'Request failed: {error}',
    invalidJsonField: 'Invalid JSON in {field}.',
    invalidConfigField: 'Invalid value in {field}.',
    configErrorSummary: 'Fix these issues before continuing:',
    draftGenerating: 'Generating workflow draft...',
    draftGenerated: 'Draft generated. Review the nodes, approvers, and terminal handlers before saving.',
    draftGeneratedFallback: 'Basic draft generated because the LLM service was unavailable. Review and adjust the workflow before saving.',
    draftNeedDescription: 'Describe the workflow before generating.',
    draftGenerationCancelled: 'Generation canceled. The current canvas was not changed.',
    draftOverwriteConfirm: 'The canvas already has a draft. Save it first if you want to keep it. Generate anyway and overwrite the current canvas?',
    draftExampleLeaveText: 'Employee submits a leave request with dates and reason. Direct manager approves first. If leave is longer than 3 days, HR also approves. Notify employee after final decision.',
    draftExamplePurchaseText: 'Employee submits a purchase request. If amount is above 10,000, department manager and finance both approve; otherwise only department manager approves. Notify requester after approval.',
    draftExampleContractText: 'Sales submits a contract review request. Legal reviews the contract, finance reviews payment terms when amount is above 50,000, then notify sales and archive the result.',
    requireOneTrigger: 'Exactly one Trigger node is required as the workflow entry point.',
    onlyOneTrigger: 'Only one Trigger node is allowed. Found {count} Trigger nodes.',
    disconnectedNode: 'Node "{label}" at ({x}, {y}) is disconnected.',
    triggerNoIncoming: 'Trigger node "{label}" must not have incoming edges.',
    terminalNoOutgoing: 'Terminal node "{label}" must not have outgoing edges.',
    conditionBranchNoRoute: 'Condition node "{label}" must route to at least one branch or default target.',
    conditionBranchInvalidTarget: 'Condition node "{label}" routes to missing target "{target}".',
    conditionBranchInvalidExpression: 'Condition node "{label}" has a branch without a valid expression field and operator.',
    selectTool: 'Select',
    connectTool: 'Connect',
    deleteEdgeTool: 'Delete Line',
    connectHint: 'Connect mode: click a source node, then click a target node.',
    deleteEdgeHint: 'Delete line mode: click a connector line to remove it.',
    edgeConnectorLabel: 'Connector from {source} to {target}',
    contextMenuLabel: 'Canvas context menu',
    editNode: 'Edit Node',
    duplicateNode: 'Duplicate Node',
    copySuffix: '{label} Copy',
    startConnection: 'Start Connection',
    deleteIncomingEdges: 'Delete Incoming Lines',
    deleteOutgoingEdges: 'Delete Outgoing Lines',
    deleteConnectedEdges: 'Delete Connected Lines',
    deleteEdge: 'Delete Line',
    addNodeType: 'Add {type}',
    canvasContextSelect: 'Switch to Select',
    canvasContextConnect: 'Switch to Connect',
    contextMenuReadOnly: 'Review preview is read-only',
    trigger: 'Trigger',
    form: 'Form',
    approval: 'Approval',
    conditionBranch: 'Condition Branch',
    action: 'Action',
    notification: 'Notification',
    subProcess: 'Sub-Process',
    terminal: 'Terminal'
  };

  function tr(key, vars) {
    if (window.ApprovalWorkflowI18n && typeof window.ApprovalWorkflowI18n.t === 'function') return window.ApprovalWorkflowI18n.t(key, vars);
    return String(FALLBACK_TEXT[key] || key).replace(/\{([^}]+)\}/g, function (_, name) {
      return vars && vars[name] !== undefined ? String(vars[name]) : '';
    });
  }

  // --- Node type metadata ---
  const NODE_TYPES = {
    trigger: { labelKey: 'trigger', icon: '\u25B6', color: '#e8f5e9', textColor: '#2e7d32' },
    form: { labelKey: 'form', icon: '\u270E', color: '#e3f2fd', textColor: '#1565c0' },
    approval: { labelKey: 'approval', icon: '\u2713', color: '#fff3e0', textColor: '#e65100' },
    condition_branch: { labelKey: 'conditionBranch', icon: '\u2666', color: '#f3e5f5', textColor: '#6a1b9a' },
    action: { labelKey: 'action', icon: '\u2699', color: '#e0f2f1', textColor: '#00695c' },
    notification: { labelKey: 'notification', icon: '\u2709', color: '#fce4ec', textColor: '#c62828' },
    sub_process: { labelKey: 'subProcess', icon: '\u21C4', color: '#ede7f6', textColor: '#4527a0' },
    terminal: { labelKey: 'terminal', icon: '\u25A0', color: '#efebe9', textColor: '#4e342e' },
  };
  const CONTEXT_MENU_ADD_NODE_TYPES = ['trigger', 'form', 'approval', 'condition_branch', 'action', 'notification', 'sub_process', 'terminal'];

  function nodeTypeLabel(type) {
    return NODE_TYPES[type] ? tr(NODE_TYPES[type].labelKey) : type;
  }

  // --- Utility ---
  function generateNodeId() {
    return 'node_' + (state.nextNodeId++);
  }

  function generateEdgeId() {
    return 'edge_' + (state.nextEdgeId++);
  }

  function clampContextMenuPosition(x, y, menuWidth, menuHeight, viewportWidth, viewportHeight) {
    var margin = 8;
    var maxX = Math.max(margin, viewportWidth - menuWidth - margin);
    var maxY = Math.max(margin, viewportHeight - menuHeight - margin);
    return {
      x: Math.min(Math.max(margin, x), maxX),
      y: Math.min(Math.max(margin, y), maxY)
    };
  }

  function isContextMenuKey(e) {
    return e && (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey));
  }

  function cloneConfig(config) {
    try {
      return JSON.parse(JSON.stringify(config || {}));
    } catch (_) {
      return {};
    }
  }

  function elementCenterPoint(el) {
    var rect = el.getBoundingClientRect();
    return {
      x: rect.left + rect.width / 2,
      y: rect.top + rect.height / 2
    };
  }

  function canvasDropPositionFromClient(clientX, clientY) {
    var rect = canvasArea.getBoundingClientRect();
    return {
      x: Math.max(0, clientX - rect.left - 80),
      y: Math.max(0, clientY - rect.top - 30)
    };
  }

  function getWorkflowName() {
    return workflowNameInput ? String(workflowNameInput.value || '').trim() : '';
  }

  function getWorkflowDescription() {
    return workflowDescriptionInput ? String(workflowDescriptionInput.value || '').trim() : '';
  }

  function getUrlParam(name) {
    try {
      var queryValue = new URLSearchParams(window.location.search || '').get(name);
      if (queryValue) return queryValue;
      return new URLSearchParams(String(window.location.hash || '').replace(/^#/, '')).get(name) || '';
    } catch (_) {
      return '';
    }
  }

  function storageGet(key) {
    try { return window.localStorage ? window.localStorage.getItem(key) || '' : ''; } catch (_) { return ''; }
  }

  function storageSet(key, value) {
    try { if (window.localStorage && value) window.localStorage.setItem(key, value); } catch (_) {}
  }

  function storageRemove(key) {
    try { if (window.localStorage) window.localStorage.removeItem(key); } catch (_) {}
  }

  function scrubWorkflowAuthFromLocation() {
    try {
      if (!window.history || typeof window.history.replaceState !== 'function') return;
      var url = new URL(window.location.href);
      var changed = false;
      ['machine_id', 'token', 'machine_token'].forEach(function (key) {
        if (url.searchParams.has(key)) {
          url.searchParams.delete(key);
          changed = true;
        }
      });
      var rawHash = String(url.hash || '').replace(/^#/, '');
      if (rawHash) {
        var hashParams = new URLSearchParams(rawHash);
        ['machine_id', 'token', 'machine_token'].forEach(function (key) {
          if (hashParams.has(key)) {
            hashParams.delete(key);
            changed = true;
          }
        });
        var nextHash = hashParams.toString();
        url.hash = nextHash ? '#' + nextHash : '';
      }
      if (changed) window.history.replaceState(window.history.state, document.title, url.pathname + url.search + url.hash);
    } catch (_) {}
  }

  function isReadOnlyPreview() {
    return !!state.isReadOnlyPreview;
  }

  function markDirty() {
    if (isReadOnlyPreview()) return;
    state.dirtyRevision++;
    state.isDirty = true;
    updateDocumentTitle();
    updateVersionStatus('dirty');
  }

  function clearDirty(savedRevision) {
    if (savedRevision !== undefined && savedRevision !== state.dirtyRevision) {
      updateDocumentTitle();
      updateVersionStatus('dirty');
      return false;
    }
    state.isDirty = false;
    if (savedRevision === undefined) state.dirtyRevision = 0;
    updateDocumentTitle();
    updateVersionStatus(state.versionStatus || 'draft');
    return true;
  }

  function hasUnsavedChanges() {
    return state.isDirty;
  }

  function updateDocumentTitle() {
    document.title = (state.isDirty ? '* ' : '') + tr('pageTitle');
  }

  function syncWorkflowRouteState() {
    state.reviewVersionId = getUrlParam('review_version_id') || null;
    state.isReadOnlyPreview = !!state.reviewVersionId;
    state.workflowId = getUrlParam('workflow_id') || storageGet('maclaw-approval-workflow-id') || null;
    var name = getUrlParam('name');
    var description = getUrlParam('description');
    if (workflowNameInput && name) workflowNameInput.value = name;
    if (workflowDescriptionInput && description) workflowDescriptionInput.value = description;
  }

  function getWorkflowAuth() {
    var rawMachineID = getUrlParam('machine_id') || storageGet('maclaw-approval-workflow-machine-id');
    var rawToken = getUrlParam('token') || getUrlParam('machine_token') || storageGet('maclaw-approval-workflow-machine-token');
    var machineID = String(rawMachineID || '').trim();
    var token = String(rawToken || '').trim();
    if (machineID) storageSet('maclaw-approval-workflow-machine-id', machineID);
    else storageRemove('maclaw-approval-workflow-machine-id');
    if (token) storageSet('maclaw-approval-workflow-machine-token', token);
    else storageRemove('maclaw-approval-workflow-machine-token');
    if (rawMachineID || rawToken) scrubWorkflowAuthFromLocation();
    return { machineID: machineID, token: token };
  }

  function getAdminToken() {
    return storageGet('maclawHubAdminToken');
  }

  function adminScopedPath(path) {
    try {
      var profile = JSON.parse(storageGet('maclawHubAdminProfile') || 'null');
      var tenantID = profile && String(profile.scope || '').toLowerCase() === 'tenant' ? String(profile.tenant_id || '').trim() : '';
      if (!tenantID) return path;
      var sep = path.indexOf('?') === -1 ? '?' : '&';
      return path + sep + 'tenant_id=' + encodeURIComponent(tenantID);
    } catch (_) {
      return path;
    }
  }

  async function adminWorkflowApi(path, options) {
    var adminToken = getAdminToken();
    if (!adminToken) throw new Error(tr('adminAuthRequired'));
    var opts = options || {};
    var headers = { 'Content-Type': 'application/json', Authorization: 'Bearer ' + adminToken };
    Object.keys(opts.headers || {}).forEach(function (key) { headers[key] = opts.headers[key]; });
    var resp;
    try {
      resp = await fetch(adminScopedPath(path), Object.assign({}, opts, { headers: headers }));
    } catch (err) {
      throw new Error(tr('requestFailed', { error: err && err.message || 'network' }));
    }
    var data = {};
    try { data = await resp.json(); } catch (_) {}
    if (!resp.ok) throw new Error(data.message || data.error || resp.statusText || String(resp.status));
    return data;
  }

  function setReadOnlyPreviewMode(enabled) {
    state.isReadOnlyPreview = !!enabled;
    if (reviewPreviewBanner) {
      reviewPreviewBanner.hidden = !state.isReadOnlyPreview;
      reviewPreviewBanner.textContent = tr('reviewPreviewMode');
    }
    [btnNew, btnSave, btnSubmit].forEach(function (btn) { setControlDisabled(btn, state.isReadOnlyPreview); });
    if (workflowNameInput) workflowNameInput.readOnly = state.isReadOnlyPreview;
    if (workflowDescriptionInput) workflowDescriptionInput.readOnly = state.isReadOnlyPreview;
    setGeneratingDraft(false);
    paletteNodes.forEach(function (el) {
      el.setAttribute('aria-disabled', state.isReadOnlyPreview ? 'true' : 'false');
      el.setAttribute('draggable', state.isReadOnlyPreview ? 'false' : 'true');
      el.tabIndex = state.isReadOnlyPreview ? -1 : 0;
    });
    updateWorkflowLibraryControls();
    updateToolModeUI();
    if (state.selectedNodeId) applyConfigPanelReadOnly();
  }

  function setBusy(isBusy, statusKey) {
    state.isBusy = !!isBusy;
    [btnNew, btnSave, btnSubmit].forEach(function (btn) { setControlDisabled(btn, !!isBusy || isReadOnlyPreview()); });
    setControlDisabled(btnValidate, !!isBusy);
    setGeneratingDraft(state.isGeneratingDraft);
    updateWorkflowLibraryControls();
    if (statusKey) updateVersionStatus(statusKey);
    renderWorkflowLibrary();
  }

  function setControlDisabled(control, disabled) {
    if (!control) return;
    control.disabled = !!disabled;
    control.setAttribute('aria-disabled', disabled ? 'true' : 'false');
  }

  function workflowLibraryControlsDisabled() {
    return !!(state.isBusy || state.isLibraryLoading || isReadOnlyPreview());
  }

  function updateWorkflowLibraryControls() {
    var disabled = workflowLibraryControlsDisabled();
    setControlDisabled(btnRefreshWorkflows, disabled);
    [workflowSearchInput, workflowStatusFilter].forEach(function (field) { setControlDisabled(field, disabled); });
  }

  function formatVersion(ver) {
    return ver ? 'v' + ver : '';
  }

  function updateVersionStatus(statusKey) {
    var status = statusKey || state.versionStatus || 'draft';
    var version = formatVersion(state.versionNumber);
    if (isReadOnlyPreview() && status !== 'loading') {
      if (workflowStatus) workflowStatus.textContent = tr('reviewPreviewStatus', { version: version });
      if (versionBadge) {
        versionBadge.className = 'version-badge pending_review';
        versionBadge.textContent = tr('reviewPreviewStatus', { version: version });
      }
      return;
    }
    var keyByStatus = {
      draft: state.versionId ? 'statusSaved' : 'statusDraft',
      saving: 'statusSaving',
      dirty: 'statusUnsaved',
      loading: 'statusLoading',
      loaded: 'statusLoaded',
      submitting: 'statusSubmitting',
      pending_review: 'statusPendingReview',
      published: 'statusPublished',
      rejected: 'statusRejected',
      superseded: 'statusSuperseded',
      unpublished: 'statusUnpublished'
    };
    var key = keyByStatus[status] || 'statusDraft';
    if (workflowStatus) workflowStatus.textContent = tr(key, { version: version });
    if (versionBadge) {
      versionBadge.className = 'version-badge ' + (status === 'saving' || status === 'submitting' || status === 'loading' || status === 'loaded' || status === 'dirty' ? 'draft' : status);
      versionBadge.textContent = status === 'draft' && !state.versionId ? tr('draft') : tr(key, { version: version });
    }
  }

  function applyWorkflowVersion(ver) {
    if (!ver) return;
    state.versionId = ver.id || state.versionId;
    state.versionNumber = ver.version_number || state.versionNumber;
    state.versionStatus = ver.status || state.versionStatus || 'draft';
    updateVersionStatus(state.versionStatus);
  }

  async function workflowApi(path, options) {
    var auth = getWorkflowAuth();
    if (!auth.machineID || !auth.token) throw new Error(tr('authRequired'));
    var opts = options || {};
    var headers = { 'Content-Type': 'application/json', 'X-Machine-ID': auth.machineID };
    Object.keys(opts.headers || {}).forEach(function (key) { headers[key] = opts.headers[key]; });
    headers.Authorization = 'Bearer ' + auth.token;
    var resp;
    try {
      resp = await fetch(path, Object.assign({}, opts, { headers: headers }));
    } catch (err) {
      throw new Error(tr('requestFailed', { error: err && err.message || 'network' }));
    }
    var data = {};
    try { data = await resp.json(); } catch (_) {}
    if (!resp.ok) {
      var apiErr = new Error(data.message || data.error || resp.statusText || String(resp.status));
      apiErr.code = data.code || '';
      throw apiErr;
    }
    return data;
  }

  function setDraftAssistantStatus(message, tone) {
    if (!draftAssistantStatus) return;
    var text = message || '';
    draftAssistantStatus.textContent = text;
    if (text) {
      draftAssistantStatus.setAttribute('title', text);
    } else {
      draftAssistantStatus.removeAttribute('title');
    }
    draftAssistantStatus.classList.toggle('error', tone === 'error');
    draftAssistantStatus.classList.toggle('success', tone === 'success');
  }

  function setGeneratingDraft(generating) {
    state.isGeneratingDraft = !!generating;
    var disabled = state.isGeneratingDraft || state.isBusy || isReadOnlyPreview();
    setControlDisabled(btnGenerateDraft, disabled);
    if (draftPrompt) {
      draftPrompt.disabled = disabled;
      draftPrompt.setAttribute('aria-disabled', draftPrompt.disabled ? 'true' : 'false');
    }
    document.querySelectorAll('[data-draft-example]').forEach(function (btn) {
      setControlDisabled(btn, disabled);
    });
  }

  function draftHasCanvasContent() {
    return state.nodes.length > 0 || state.edges.length > 0;
  }

  function confirmDraftOverwriteIfNeeded() {
    return !draftHasCanvasContent() || confirm(tr('draftOverwriteConfirm'));
  }

  function draftExampleText(key) {
    var map = {
      leave: 'draftExampleLeaveText',
      purchase: 'draftExamplePurchaseText',
      contract: 'draftExampleContractText'
    };
    return tr(map[key] || 'draftExamplePurchaseText');
  }

  function draftGeneratedStatus(data) {
    var message = data && data.generated_by === 'fallback' ? tr('draftGeneratedFallback') : tr('draftGenerated');
    var notes = data && Array.isArray(data.notes) ? data.notes : [];
    var note = notes.length > 0 ? String(notes[0] || '').trim() : '';
    if (!note) return message;
    if (note.length > 120) note = note.slice(0, 117) + '...';
    return message + ' ' + note;
  }

  async function generateWorkflowDraftFromPrompt() {
    if (isReadOnlyPreview() || state.isGeneratingDraft) return;
    var description = String(draftPrompt && draftPrompt.value || '').trim();
    if (!description) {
      setDraftAssistantStatus(tr('draftNeedDescription'), 'error');
      if (draftPrompt && typeof draftPrompt.focus === 'function') draftPrompt.focus();
      return;
    }
    if (!confirmDraftOverwriteIfNeeded()) {
      setDraftAssistantStatus(tr('draftGenerationCancelled'));
      return;
    }
    setGeneratingDraft(true);
    setDraftAssistantStatus(tr('draftGenerating'));
    try {
      var data = await workflowApi('/api/v1/workflow-drafts/generate', {
        method: 'POST',
        body: JSON.stringify({
          description: description,
          language: window.ApprovalWorkflowI18n && window.ApprovalWorkflowI18n.currentLang ? window.ApprovalWorkflowI18n.currentLang() : ''
        })
      });
      if (workflowNameInput && data.name && !getWorkflowName()) workflowNameInput.value = data.name;
      if (workflowDescriptionInput && data.description && !getWorkflowDescription()) workflowDescriptionInput.value = data.description;
      state.versionId = null;
      state.versionNumber = '';
      state.versionStatus = 'draft';
      applyWorkflowGraph(data.graph || { nodes: [], edges: [] });
      markDirty();
      setDraftAssistantStatus(draftGeneratedStatus(data), 'success');
    } catch (err) {
      setDraftAssistantStatus(err && err.message ? err.message : tr('requestFailed', { error: String(err) }), 'error');
    } finally {
      setGeneratingDraft(false);
      updateToolModeUI();
    }
  }

  async function ensureWorkflowDefinition() {
    if (state.workflowId) return state.workflowId;
    var name = arguments.length > 0 ? arguments[0] : undefined;
    var description = arguments.length > 1 ? arguments[1] : undefined;
    name = name != null ? String(name) : getWorkflowName();
    description = description != null ? String(description) : getWorkflowDescription();
    if (!name) throw new Error(tr('workflowNameRequired'));
    var def = await workflowApi('/api/v1/workflows', {
      method: 'POST',
      body: JSON.stringify({ name: name, description: description })
    });
    state.workflowId = def.id;
    storageSet('maclaw-approval-workflow-id', state.workflowId);
    return state.workflowId;
  }

  async function syncWorkflowDefinition() {
    if (!state.workflowId) return;
    var name = arguments.length > 0 ? arguments[0] : undefined;
    var description = arguments.length > 1 ? arguments[1] : undefined;
    name = name != null ? String(name) : getWorkflowName();
    description = description != null ? String(description) : getWorkflowDescription();
    if (!name) throw new Error(tr('workflowNameRequired'));
    await workflowApi('/api/v1/workflows/' + encodeURIComponent(state.workflowId), {
      method: 'PUT',
      body: JSON.stringify({ name: name, description: description })
    });
  }

  async function saveWorkflowDraft() {
    var savedRevision = state.dirtyRevision;
    var name = getWorkflowName();
    var description = getWorkflowDescription();
    var graph = getWorkflowGraph();
    var workflowID = await ensureWorkflowDefinition(name, description);
    await syncWorkflowDefinition(name, description);
    var ver = await workflowApi('/api/v1/workflows/' + encodeURIComponent(workflowID) + '/versions', {
      method: 'POST',
      body: JSON.stringify({ graph: graph })
    });
    applyWorkflowVersion(ver);
    // clearDirty(savedRevision); keeps the static accessibility contract anchored to the save path.
    return { version: ver, clean: clearDirty(savedRevision) };
  }

  function latestVersion(versions) {
    return (versions || []).slice().sort(compareWorkflowVersions)[0] || null;
  }

  function compareWorkflowVersions(a, b) {
    var av = parseVersionNumber(a && a.version_number);
    var bv = parseVersionNumber(b && b.version_number);
    for (var i = 0; i < 3; i++) {
      if (bv[i] !== av[i]) return bv[i] - av[i];
    }
    return String(b && (b.created_at || b.updated_at) || '').localeCompare(String(a && (a.created_at || a.updated_at) || ''));
  }

  function parseVersionNumber(version) {
    var parts = String(version || '').split('.');
    if (parts.length !== 3) return [0, 0, 0];
    return parts.map(function (part) {
      var value = parseInt(part, 10);
      return Number.isFinite(value) && value >= 0 ? value : 0;
    });
  }

  function workflowHasPublishedHistory(workflowId) {
    var versions = state.workflowVersionsById[workflowId];
    if (!Array.isArray(versions)) return true;
    return versions.some(function (ver) { return workflowVersionBlocksDesignerDelete(ver.status); });
  }

  function workflowVersionHistoryUnavailable(workflowId) {
    return !Array.isArray(state.workflowVersionsById[workflowId]);
  }

  function workflowVersionBlocksDesignerDelete(status) {
    return status === 'published' || status === 'superseded' || status === 'unpublished';
  }

  function workflowLibraryStatus(workflowId) {
    if (workflowVersionHistoryUnavailable(workflowId)) return 'unknown';
    var latest = latestVersion(state.workflowVersionsById[workflowId] || []);
    return latest && latest.status ? latest.status : 'draft';
  }

  function workflowStatusLabel(status) {
    var keyByStatus = {
      draft: 'statusDraftShort',
      pending_review: 'statusPendingReviewShort',
      published: 'statusPublishedShort',
      rejected: 'statusRejectedShort',
      unpublished: 'statusUnpublishedShort',
      superseded: 'statusSupersededShort',
      unknown: 'statusUnknownShort'
    };
    return tr(keyByStatus[status] || 'statusDraftShort');
  }

  function workflowPrimaryActionLabel(status) {
    if (status === 'published' || status === 'superseded' || status === 'unpublished') return tr('reviseWorkflow');
    return tr('continueWorkflow');
  }

  function filteredWorkflowSummaries() {
    var query = String(state.workflowSearch || '').toLowerCase().trim();
    var status = state.workflowStatusFilter || '';
    return state.workflowSummaries.filter(function (item) {
      var currentStatus = workflowLibraryStatus(item.id);
      if (status && currentStatus !== status) return false;
      if (!query) return true;
      return [item.name, item.description, item.id].some(function (value) {
        return String(value || '').toLowerCase().indexOf(query) !== -1;
      });
    });
  }

  function renderWorkflowLibrary(message, isError) {
    if (!workflowList) return;
    if (message) {
      workflowList.innerHTML = '<div role="listitem" class="' + (isError ? 'workflow-library-error' : 'workflow-library-empty') + '">' + escapeHtml(message) + '</div>';
      return;
    }
    if (!state.workflowSummaries.length) {
      workflowList.innerHTML = '<div role="listitem" class="workflow-library-empty">' + escapeHtml(tr('workflowListEmpty')) + '</div>';
      return;
    }
    var items = filteredWorkflowSummaries();
    if (!items.length) {
      workflowList.innerHTML = '<div role="listitem" class="workflow-library-empty">' + escapeHtml(tr('workflowListNoMatches')) + '</div>';
      return;
    }
    workflowList.innerHTML = items.map(function (item) {
      var versions = state.workflowVersionsById[item.id] || [];
      var latest = latestVersion(versions) || {};
      var hasPublished = workflowHasPublishedHistory(item.id);
      var historyUnavailable = workflowVersionHistoryUnavailable(item.id);
      var active = state.workflowId === item.id ? ' active' : '';
      var name = item.name || item.id;
      var currentStatus = historyUnavailable ? 'unknown' : (latest.status || 'draft');
      var versionLabel = historyUnavailable ? tr('workflowVersionUnknown') : (latest.version_number ? tr('workflowVersionMeta', { status: workflowStatusLabel(latest.status || 'draft'), version: formatVersion(latest.version_number) }) : tr('statusDraft'));
      var description = item.description ? ' | ' + item.description : '';
      var actionLabel = workflowPrimaryActionLabel(currentStatus);
      var openIsDisabled = workflowLibraryControlsDisabled();
      var deleteIsDisabled = workflowLibraryControlsDisabled() || hasPublished || historyUnavailable;
      var openDisabled = openIsDisabled ? ' disabled aria-disabled="true"' : ' aria-disabled="false"';
      var deleteDisabled = deleteIsDisabled ? ' disabled aria-disabled="true"' : ' aria-disabled="false"';
      var deleteTitle = historyUnavailable ? tr('deleteWorkflowUnavailable') : (hasPublished ? tr('deleteWorkflowBlocked') : '');
      return '<div class="workflow-library-item' + active + '" role="listitem" data-workflow-id="' + escapeAttr(item.id) + '">'
        + '<div class="workflow-library-row"><div class="workflow-library-name" title="' + escapeAttr(name) + '">' + escapeHtml(name) + '</div>'
        + '<span class="workflow-library-status ' + escapeAttr(currentStatus) + '">' + escapeHtml(workflowStatusLabel(currentStatus)) + '</span></div>'
        + '<div class="workflow-library-meta" title="' + escapeAttr(versionLabel + description) + '">' + escapeHtml(versionLabel + description) + '</div>'
        + '<div class="workflow-library-actions">'
        + '<button type="button" data-workflow-action="open" data-workflow-id="' + escapeAttr(item.id) + '"' + openDisabled + '>' + escapeHtml(actionLabel) + '</button>'
        + '<button type="button" class="danger" data-workflow-action="delete" data-workflow-id="' + escapeAttr(item.id) + '"' + deleteDisabled + (deleteTitle ? ' title="' + escapeAttr(deleteTitle) + '"' : '') + '>' + escapeHtml(tr('deleteWorkflow')) + '</button>'
        + '</div></div>';
    }).join('');
  }

  async function loadWorkflowLibrary() {
    if (!workflowList) return;
    var auth = getWorkflowAuth();
    if (!auth.machineID || !auth.token) {
      renderWorkflowLibrary(tr('authRequired'), true);
      return;
    }
    var requestId = ++state.libraryRequestId;
    var message = '';
    var isError = false;
    state.isLibraryLoading = true;
    updateWorkflowLibraryControls();
    renderWorkflowLibrary(tr('statusLoading'));
    try {
      var data = await workflowApi('/api/v1/workflows');
      if (requestId !== state.libraryRequestId) return;
      var workflows = Array.isArray(data.workflows) ? data.workflows : [];
      var versionsById = {};
      await Promise.all(workflows.map(async function (wf) {
        if (!wf || !wf.id) return;
        try {
          var versions = await workflowApi('/api/v1/workflows/' + encodeURIComponent(wf.id) + '/versions');
          versionsById[wf.id] = Array.isArray(versions.versions) ? versions.versions : [];
        } catch (_) {
          versionsById[wf.id] = null;
        }
      }));
      if (requestId !== state.libraryRequestId) return;
      state.workflowSummaries = workflows;
      state.workflowVersionsById = versionsById;
    } catch (err) {
      if (requestId !== state.libraryRequestId) return;
      message = tr('workflowListFailed', { error: err && err.message || String(err) });
      isError = true;
    } finally {
      if (requestId === state.libraryRequestId) {
        state.isLibraryLoading = false;
        updateWorkflowLibraryControls();
        renderWorkflowLibrary(message, isError);
      }
    }
  }

  async function openWorkflowDesign(workflowId) {
    if (workflowLibraryControlsDisabled()) return;
    if (!workflowId || workflowId === state.workflowId) return;
    if (hasUnsavedChanges() && !confirm(tr('openWorkflowConfirm'))) return;
    await loadWorkflowFromApi(workflowId);
    renderWorkflowLibrary();
  }

  async function deleteWorkflowDesign(workflowId) {
    if (workflowLibraryControlsDisabled()) return;
    if (!workflowId) return;
    if (workflowVersionHistoryUnavailable(workflowId)) {
      alert(tr('deleteWorkflowUnavailable'));
      return;
    }
    if (workflowHasPublishedHistory(workflowId)) {
      alert(tr('deleteWorkflowBlocked'));
      return;
    }
    var item = state.workflowSummaries.find(function (wf) { return wf.id === workflowId; }) || {};
    var name = item.name || workflowId;
    if (!confirm(tr('deleteWorkflowConfirm', { name: name }))) return;
    setBusy(true);
    try {
      await workflowApi('/api/v1/workflows/' + encodeURIComponent(workflowId), { method: 'DELETE' });
      if (state.workflowId === workflowId) resetWorkflowDesigner();
      await loadWorkflowLibrary();
      alert(tr('workflowDeleted'));
    } catch (err) {
      alert(tr('requestFailed', { error: err && err.message || String(err) }));
    } finally {
      setBusy(false);
      renderWorkflowLibrary();
      updateToolModeUI();
    }
  }

  function nextNumberFromIds(items, prefix) {
    return (items || []).reduce(function (max, item) {
      var id = String(item && item.id || '');
      var match = id.match(new RegExp('^' + prefix + '_(\\d+)$'));
      return match ? Math.max(max, parseInt(match[1], 10) + 1) : max;
    }, 1);
  }

  function normalizeLoadedNode(node) {
    return {
      id: String(node.id || generateNodeId()),
      type: NODE_TYPES[node.type] ? node.type : 'action',
      label: String(node.label || nodeTypeLabel(node.type)),
      position: node.position && typeof node.position === 'object' ? { x: Number(node.position.x || 0), y: Number(node.position.y || 0) } : { x: 0, y: 0 },
      config: node.config && typeof node.config === 'object' ? node.config : getDefaultConfig(node.type)
    };
  }

  function normalizeLoadedEdge(edge) {
    return {
      id: String(edge.id || generateEdgeId()),
      source_id: String(edge.source_id || ''),
      target_id: String(edge.target_id || ''),
      label: edge.label || '',
      priority: edge.priority || 0
    };
  }

  function applyWorkflowGraph(graph) {
    graph = graph || {};
    state.nodes = (Array.isArray(graph.nodes) ? graph.nodes : []).map(normalizeLoadedNode);
    state.edges = (Array.isArray(graph.edges) ? graph.edges : []).map(normalizeLoadedEdge).filter(function (edge) {
      return state.nodes.some(function (n) { return n.id === edge.source_id; }) && state.nodes.some(function (n) { return n.id === edge.target_id; });
    });
    state.nextNodeId = nextNumberFromIds(state.nodes, 'node');
    state.nextEdgeId = nextNumberFromIds(state.edges, 'edge');
    state.invalidConfigFields = {};
    state.selectedNodeId = null;
    state.selectedEdgeId = null;
    clearConnectingState();
    canvasNodes.innerHTML = '';
    state.nodes.forEach(renderNode);
    configPanel.classList.remove('visible');
    updateEmptyState();
    renderEdges();
    clearDirty();
  }

  async function loadWorkflowFromApi(workflowId) {
    var targetWorkflowId = workflowId || state.workflowId;
    if (!targetWorkflowId) return false;
    var auth = getWorkflowAuth();
    if (!auth.machineID || !auth.token) return false;
    setBusy(true, 'loading');
    try {
      var def = await workflowApi('/api/v1/workflows/' + encodeURIComponent(targetWorkflowId));
      var data = await workflowApi('/api/v1/workflows/' + encodeURIComponent(targetWorkflowId) + '/versions');
      state.workflowId = targetWorkflowId;
      storageSet('maclaw-approval-workflow-id', targetWorkflowId);
      state.versionId = null;
      state.versionNumber = '';
      state.versionStatus = 'draft';
      if (workflowNameInput) workflowNameInput.value = def.name || workflowNameInput.value || '';
      if (workflowDescriptionInput) workflowDescriptionInput.value = def.description || '';
      var ver = latestVersion(Array.isArray(data.versions) ? data.versions : []);
      if (ver) {
        applyWorkflowVersion(ver);
        applyWorkflowGraph(ver.graph);
      } else {
        applyWorkflowGraph({ nodes: [], edges: [] });
        updateVersionStatus('draft');
      }
      clearDirty();
      return true;
    } catch (err) {
      if (workflowStatus) workflowStatus.textContent = tr('statusLoadFailed', { error: err && err.message || String(err) });
      return false;
    } finally {
      setBusy(false);
      updateToolModeUI();
    }
  }

  async function loadWorkflowReviewPreview() {
    if (!state.reviewVersionId) return false;
    setReadOnlyPreviewMode(true);
    setBusy(true, 'loading');
    try {
      var detail = await adminWorkflowApi('/api/v1/admin/reviews/' + encodeURIComponent(state.reviewVersionId));
      var ver = detail.version || {};
      state.workflowId = ver.workflow_id || state.workflowId;
      if (workflowNameInput) workflowNameInput.value = detail.workflow_name || state.workflowId || '';
      if (workflowDescriptionInput) workflowDescriptionInput.value = detail.workflow_description || '';
      applyWorkflowVersion(ver);
      applyWorkflowGraph(detail.graph || ver.graph || { nodes: [], edges: [] });
      setReadOnlyPreviewMode(true);
      updateVersionStatus(ver.status || 'pending_review');
      return true;
    } catch (err) {
      if (workflowStatus) workflowStatus.textContent = tr('statusLoadFailed', { error: err && err.message || String(err) });
      return false;
    } finally {
      setBusy(false);
      setReadOnlyPreviewMode(true);
      updateToolModeUI();
    }
  }

  function resetWorkflowDesigner() {
    state.nodes = [];
    state.edges = [];
    state.selectedNodeId = null;
    state.selectedEdgeId = null;
    state.workflowId = null;
    state.versionId = null;
    state.versionNumber = '';
    state.versionStatus = 'draft';
    state.nextNodeId = 1;
    state.nextEdgeId = 1;
    state.invalidConfigFields = {};
    state.toolMode = 'select';
    clearDirty();
    clearConnectingState();
    storageRemove('maclaw-approval-workflow-id');
    if (workflowNameInput) workflowNameInput.value = '';
    if (workflowDescriptionInput) workflowDescriptionInput.value = '';
    canvasNodes.innerHTML = '';
    configPanel.classList.remove('visible');
    updateEmptyState();
    renderEdges();
    updateVersionStatus('draft');
    renderWorkflowLibrary();
  }

  function updateEmptyState() {
    canvasEmpty.style.display = state.nodes.length === 0 ? 'block' : 'none';
    var mode = state.toolMode;
    if (isToolModeDisabled(mode)) mode = 'select';
    if (mode !== state.toolMode) {
      state.toolMode = mode;
      state.selectedEdgeId = null;
      clearConnectingState();
    }
    updateToolModeUI();
  }

  function setToolMode(mode) {
    if (isToolModeDisabled(mode)) mode = 'select';
    state.toolMode = mode;
    state.selectedEdgeId = null;
    if (mode !== 'select') clearSelectedNode();
    clearConnectingState();
    updateToolModeUI();
    renderEdges();
  }

  function isToolModeDisabled(mode) {
    if (mode === 'connect') return state.nodes.length < 2;
    if (mode === 'delete_edge') return state.edges.length === 0;
    return false;
  }

  function updateToolModeUI() {
    if (isReadOnlyPreview() && state.toolMode !== 'select') {
      state.toolMode = 'select';
      state.selectedEdgeId = null;
      clearConnectingState();
    }
    if (isToolModeDisabled(state.toolMode)) {
      state.toolMode = 'select';
      state.selectedEdgeId = null;
      clearConnectingState();
    }
    document.querySelectorAll('[data-tool-mode]').forEach(function (btn) {
      var mode = btn.getAttribute('data-tool-mode');
      var active = mode === state.toolMode;
      var disabled = isToolModeDisabled(mode) || (isReadOnlyPreview() && mode !== 'select');
      btn.classList.toggle('active', active);
      btn.setAttribute('aria-pressed', active ? 'true' : 'false');
      btn.disabled = disabled;
      btn.setAttribute('aria-disabled', disabled ? 'true' : 'false');
    });
    if (!canvasToolHint) return;
    if (state.toolMode === 'connect') {
      canvasToolHint.textContent = tr('connectHint');
      canvasToolHint.classList.add('visible');
    } else if (state.toolMode === 'delete_edge') {
      canvasToolHint.textContent = tr('deleteEdgeHint');
      canvasToolHint.classList.add('visible');
    } else {
      canvasToolHint.textContent = '';
      canvasToolHint.classList.remove('visible');
    }
  }

  function clearConnectingState() {
    if (state.connectingFrom) {
      var fromEl = findCanvasNodeElement(state.connectingFrom);
      if (fromEl) fromEl.classList.remove('connecting-source');
    }
    state.connectingFrom = null;
  }

  function findCanvasNodeElement(nodeId) {
    var id = String(nodeId || '');
    var nodes = canvasNodes.querySelectorAll('[data-node-id]');
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].getAttribute('data-node-id') === id) return nodes[i];
    }
    return null;
  }

  document.querySelectorAll('[data-tool-mode]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      setToolMode(btn.getAttribute('data-tool-mode'));
    });
  });

  function ensureContextMenu() {
    var menu = document.getElementById('workflowContextMenu');
    if (!menu) {
      menu = document.createElement('div');
      menu.id = 'workflowContextMenu';
      menu.className = 'workflow-context-menu';
      menu.setAttribute('role', 'menu');
      menu.hidden = true;
      document.body.appendChild(menu);
      menu.addEventListener('click', function (e) {
        var item = e.target.closest('[data-context-action]');
        if (!item || item.getAttribute('aria-disabled') === 'true') return;
        e.preventDefault();
        runContextMenuAction(item.getAttribute('data-context-action'));
      });
      menu.addEventListener('keydown', function (e) {
        var items = Array.prototype.slice.call(menu.querySelectorAll('[data-context-action]:not([aria-disabled="true"])'));
        var index = items.indexOf(document.activeElement);
        if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          hideContextMenu({ restoreFocus: true });
        } else if (e.key === 'ArrowDown') {
          e.preventDefault();
          (items[index + 1] || items[0] || menu).focus();
        } else if (e.key === 'ArrowUp') {
          e.preventDefault();
          (items[index - 1] || items[items.length - 1] || menu).focus();
        } else if (e.key === 'Home') {
          e.preventDefault();
          (items[0] || menu).focus();
        } else if (e.key === 'End') {
          e.preventDefault();
          (items[items.length - 1] || menu).focus();
        } else if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          if (document.activeElement && document.activeElement.hasAttribute('data-context-action')) {
            runContextMenuAction(document.activeElement.getAttribute('data-context-action'));
          }
        }
      });
    }
    menu.setAttribute('aria-label', tr('contextMenuLabel'));
    return menu;
  }

  function showContextMenu(targetType, targetId, clientX, clientY, canvasPosition) {
    hideContextMenu();
    var menu = ensureContextMenu();
    var items = buildContextMenuItems(targetType, targetId);
    if (!items.length) return;
    state.contextMenu = { targetType: targetType, targetId: targetId, canvasPosition: canvasPosition || null };
    state.contextMenuReturnFocus = document.activeElement && typeof document.activeElement.focus === 'function' ? document.activeElement : null;
    menu.innerHTML = items.map(renderContextMenuItem).join('');
    menu.hidden = false;
    var rect = menu.getBoundingClientRect();
    var pos = clampContextMenuPosition(clientX, clientY, rect.width || 200, rect.height || 40, window.innerWidth || 1024, window.innerHeight || 768);
    menu.style.left = pos.x + 'px';
    menu.style.top = pos.y + 'px';
    var firstEnabled = menu.querySelector('[data-context-action]:not([aria-disabled="true"])');
    if (firstEnabled) firstEnabled.focus({ preventScroll: true });
  }

  function hideContextMenu(options) {
    var opts = options || {};
    var menu = document.getElementById('workflowContextMenu');
    state.contextMenu = null;
    if (!menu) return;
    menu.hidden = true;
    menu.innerHTML = '';
    if (opts.restoreFocus && state.contextMenuReturnFocus && document.contains(state.contextMenuReturnFocus)) {
      state.contextMenuReturnFocus.focus({ preventScroll: true });
    }
    state.contextMenuReturnFocus = null;
  }

  function isContextMenuOpen() {
    var menu = document.getElementById('workflowContextMenu');
    return !!(menu && !menu.hidden);
  }

  function renderContextMenuItem(item) {
    if (item.separator) return '<div class="workflow-context-menu-separator" role="separator"></div>';
    var disabled = item.disabled ? ' aria-disabled="true" tabindex="-1"' : ' aria-disabled="false" tabindex="0"';
    var danger = item.danger ? ' danger' : '';
    var icon = item.icon ? '<span class="workflow-context-menu-icon" aria-hidden="true">' + escapeHtml(item.icon) + '</span>' : '';
    return '<button type="button" role="menuitem" class="workflow-context-menu-item' + danger + '" data-context-action="' + escapeAttr(item.action) + '"' + disabled + '>' + icon + '<span>' + escapeHtml(item.label) + '</span></button>';
  }

  function buildContextMenuItems(targetType, targetId) {
    var readOnly = isReadOnlyPreview();
    if (targetType === 'node') {
      var node = state.nodes.find(function (n) { return n.id === targetId; });
      if (!node) return [];
      var incoming = state.edges.some(function (e) { return e.target_id === targetId; });
      var outgoing = state.edges.some(function (e) { return e.source_id === targetId; });
      return [
        { action: 'edit_node', label: tr('editNode'), icon: '\u2699' },
        { action: 'duplicate_node', label: tr('duplicateNode'), icon: '\u29c9', disabled: readOnly },
        { action: 'start_connection', label: tr('startConnection'), icon: '\u2192', disabled: readOnly || state.nodes.length < 2 },
        { separator: true },
        { action: 'delete_incoming_edges', label: tr('deleteIncomingEdges'), icon: '\u2193', disabled: readOnly || !incoming },
        { action: 'delete_outgoing_edges', label: tr('deleteOutgoingEdges'), icon: '\u2191', disabled: readOnly || !outgoing },
        { action: 'delete_connected_edges', label: tr('deleteConnectedEdges'), icon: '\u2573', disabled: readOnly || (!incoming && !outgoing) },
        { separator: true },
        { action: 'delete_node', label: tr('deleteNode'), icon: '\u232b', danger: true, disabled: readOnly }
      ];
    }
    if (targetType === 'edge') {
      var edge = state.edges.find(function (e) { return e.id === targetId; });
      if (!edge) return [];
      return [
        { action: 'select_edge', label: edgeAriaLabel(edge), icon: '\u2501' },
        { separator: true },
        { action: 'delete_edge', label: tr('deleteEdge'), icon: '\u232b', danger: true, disabled: readOnly }
      ];
    }
    if (targetType === 'canvas') {
      var addItems = CONTEXT_MENU_ADD_NODE_TYPES.map(function (type) {
        return { action: 'add_node:' + type, label: tr('addNodeType', { type: nodeTypeLabel(type) }), icon: NODE_TYPES[type].icon, disabled: readOnly };
      });
      return addItems.concat([
        { separator: true },
        { action: 'set_select_mode', label: tr('canvasContextSelect'), icon: '\u25a1' },
        { action: 'set_connect_mode', label: tr('canvasContextConnect'), icon: '\u2192', disabled: readOnly || state.nodes.length < 2 }
      ]);
    }
    return readOnly ? [{ action: 'noop', label: tr('contextMenuReadOnly'), disabled: true }] : [];
  }

  function runContextMenuAction(action) {
    var menuState = state.contextMenu;
    if (!menuState) return;
    var targetId = menuState.targetId;
    var canvasPosition = menuState.canvasPosition;
    hideContextMenu({ restoreFocus: false });
    if (action === 'noop') return;
    if (action === 'edit_node') {
      selectNode(targetId);
    } else if (action === 'duplicate_node') {
      duplicateNode(targetId);
    } else if (action === 'start_connection') {
      setToolMode('connect');
      var el = findCanvasNodeElement(targetId);
      if (el) handleConnectNodeClick(targetId, el);
    } else if (action === 'delete_incoming_edges') {
      deleteEdgesForNode(targetId, 'incoming');
    } else if (action === 'delete_outgoing_edges') {
      deleteEdgesForNode(targetId, 'outgoing');
    } else if (action === 'delete_connected_edges') {
      deleteEdgesForNode(targetId, 'connected');
    } else if (action === 'delete_node') {
      deleteNode(targetId);
    } else if (action === 'select_edge') {
      selectEdge(targetId);
    } else if (action === 'delete_edge') {
      deleteEdge(targetId);
    } else if (action.indexOf('add_node:') === 0) {
      addNodeToCanvas(action.slice('add_node:'.length), canvasPosition || { x: 120, y: 90 });
    } else if (action === 'set_select_mode') {
      setToolMode('select');
    } else if (action === 'set_connect_mode') {
      setToolMode('connect');
    }
  }

  function addNodeToCanvas(nodeType, position) {
    if (isReadOnlyPreview()) return;
    if (!nodeType || !NODE_TYPES[nodeType]) return;
    const node = {
      id: generateNodeId(),
      type: nodeType,
      label: nodeTypeLabel(nodeType),
      default_label_key: NODE_TYPES[nodeType].labelKey,
      label_is_default: true,
      position: position,
      config: getDefaultConfig(nodeType),
    };
    state.nodes.push(node);
    markDirty();
    renderNode(node);
    updateEmptyState();
    if (state.toolMode !== 'select') setToolMode('select');
    selectNode(node.id);
  }

  function duplicateNode(nodeId) {
    if (isReadOnlyPreview()) return;
    var source = state.nodes.find(function (n) { return n.id === nodeId; });
    if (!source) return;
    var node = {
      id: generateNodeId(),
      type: source.type,
      label: tr('copySuffix', { label: source.label || nodeTypeLabel(source.type) }),
      default_label_key: source.default_label_key,
      label_is_default: false,
      position: {
        x: Math.max(0, Number(source.position && source.position.x || 0) + 32),
        y: Math.max(0, Number(source.position && source.position.y || 0) + 32)
      },
      config: cloneConfig(source.config),
    };
    state.nodes.push(node);
    markDirty();
    renderNode(node);
    updateEmptyState();
    if (state.toolMode !== 'select') setToolMode('select');
    selectNode(node.id);
  }

  // --- Drag and Drop from Palette ---
  const paletteNodes = document.querySelectorAll('.palette-node');
  paletteNodes.forEach(function (el) {
    el.addEventListener('dragstart', function (e) {
      if (isReadOnlyPreview()) {
        e.preventDefault();
        return;
      }
      state.draggingNodeType = el.getAttribute('data-node-type');
      e.dataTransfer.setData('text/plain', state.draggingNodeType);
      e.dataTransfer.effectAllowed = 'copy';
    });
    el.addEventListener('dragend', function () {
      state.draggingNodeType = null;
      dropIndicator.classList.remove('visible');
    });
    el.addEventListener('keydown', function (e) {
      if (isReadOnlyPreview()) return;
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      const offset = state.nodes.length * 24;
      addNodeToCanvas(el.getAttribute('data-node-type'), { x: 120 + offset, y: 90 + offset });
    });
  });

  canvasContainer.addEventListener('dragover', function (e) {
    if (isReadOnlyPreview()) return;
    if (!state.draggingNodeType) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    const rect = canvasArea.getBoundingClientRect();
    const x = e.clientX - rect.left - 80;
    const y = e.clientY - rect.top - 30;
    dropIndicator.style.left = x + 'px';
    dropIndicator.style.top = y + 'px';
    dropIndicator.classList.add('visible');
  });

  canvasContainer.addEventListener('dragleave', function () {
    dropIndicator.classList.remove('visible');
  });

  canvasContainer.addEventListener('drop', function (e) {
    if (isReadOnlyPreview()) return;
    e.preventDefault();
    dropIndicator.classList.remove('visible');
    const nodeType = e.dataTransfer.getData('text/plain');
    if (!nodeType || !NODE_TYPES[nodeType]) return;

    const rect = canvasArea.getBoundingClientRect();
    const x = e.clientX - rect.left - 80;
    const y = e.clientY - rect.top - 30;

    addNodeToCanvas(nodeType, { x: Math.max(0, x), y: Math.max(0, y) });
  });

  // --- Default config per node type ---
  function getDefaultConfig(type) {
    switch (type) {
      case 'trigger':
        return { trigger_type: 'manual', description: '' };
      case 'form':
        return { fields: [], description: '' };
      case 'approval':
        return {
          approver_ids: [],
          mode: 'single',
          min_approvals: 1,
          approver_order: [],
          timeout_hours: 24,
          fallback_approver: '',
        };
      case 'condition_branch':
        return { branches: [], default_branch: '' };
      case 'action':
        return { action_type: '', parameters: {} };
      case 'notification':
        return { recipients: [], message_template: '' };
      case 'sub_process':
        return { workflow_id: '', input_mapping: {} };
      case 'terminal':
        return window.getTerminalNodeDefaultConfig ? window.getTerminalNodeDefaultConfig() : { result_executors: [], notifiers: [] };
      default:
        return {};
    }
  }

  // --- Render a node on canvas ---
  function renderNode(node) {
    const meta = NODE_TYPES[node.type];
    const el = document.createElement('div');
    el.className = 'canvas-node';
    el.setAttribute('data-node-id', node.id);
    el.setAttribute('role', 'button');
    el.tabIndex = 0;
    el.setAttribute('aria-pressed', 'false');
    el.setAttribute('aria-label', node.label + ' ' + nodeTypeLabel(node.type));
    el.style.left = node.position.x + 'px';
    el.style.top = node.position.y + 'px';
    el.innerHTML =
      '<div class="canvas-node-header">' +
        '<div class="palette-node-icon" style="width:24px;height:24px;border-radius:6px;font-size:12px;background:' + meta.color + ';color:' + meta.textColor + ';display:flex;align-items:center;justify-content:center;">' + meta.icon + '</div>' +
        '<span class="canvas-node-label">' + escapeHtml(node.label) + '</span>' +
      '</div>' +
      '<div class="canvas-node-type">' + escapeHtml(nodeTypeLabel(node.type)) + '</div>';

    // Node click to select
    el.addEventListener('mousedown', function (e) {
      if (e.button !== 0) return;
      e.stopPropagation();
      if (isReadOnlyPreview()) {
        selectNode(node.id);
        return;
      }
      if (state.toolMode === 'connect') {
        handleConnectNodeClick(node.id, el);
        return;
      }
      if (state.toolMode === 'delete_edge') return;
      selectNode(node.id);
      startDragNode(e, node, el);
    });

    el.addEventListener('keydown', function (e) {
      if (isContextMenuKey(e)) {
        e.preventDefault();
        selectNode(node.id);
        var point = elementCenterPoint(el);
        showContextMenu('node', node.id, point.x, point.y);
        return;
      }
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      if (isReadOnlyPreview()) {
        selectNode(node.id);
        return;
      }
      if (state.toolMode === 'connect') {
        handleConnectNodeClick(node.id, el);
        return;
      }
      if (state.toolMode === 'delete_edge') return;
      selectNode(node.id);
    });

    // Double-click remains a quick connection shortcut.
    el.addEventListener('dblclick', function (e) {
      e.stopPropagation();
      if (isReadOnlyPreview()) return;
      if (state.toolMode === 'delete_edge') return;
      handleConnectNodeClick(node.id, el);
    });

    el.addEventListener('contextmenu', function (e) {
      e.preventDefault();
      e.stopPropagation();
      selectNode(node.id);
      showContextMenu('node', node.id, e.clientX, e.clientY);
    });

    canvasNodes.appendChild(el);
  }

  function handleConnectNodeClick(nodeId, el) {
    if (isReadOnlyPreview()) return;
    if (state.nodes.length < 2) return;
    state.selectedEdgeId = null;
    if (state.connectingFrom === null) {
      state.connectingFrom = nodeId;
      el.classList.add('connecting-source');
      return;
    }
    if (state.connectingFrom !== nodeId) {
      addEdge(state.connectingFrom, nodeId);
      clearConnectingState();
      return;
    }
    clearConnectingState();
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // --- Node dragging on canvas ---
  function startDragNode(e, node, el) {
    if (isReadOnlyPreview()) return;
    const startX = e.clientX;
    const startY = e.clientY;
    const origX = node.position.x;
    const origY = node.position.y;

    function onMove(ev) {
      const dx = ev.clientX - startX;
      const dy = ev.clientY - startY;
      node.position.x = Math.max(0, origX + dx);
      node.position.y = Math.max(0, origY + dy);
      el.style.left = node.position.x + 'px';
      el.style.top = node.position.y + 'px';
      renderEdges();
    }

    function onUp() {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      if (node.position.x !== origX || node.position.y !== origY) markDirty();
    }

    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }

  // --- Node selection ---
  function selectNode(nodeId) {
    if (state.selectedNodeId && state.selectedNodeId !== nodeId) clearInvalidConfigFieldsForNode(state.selectedNodeId);
    state.selectedNodeId = nodeId;
    state.selectedEdgeId = null;
    // Update visual selection
    updateCanvasNodeSelection();
    renderEdges();
    // Show config panel (requirement: within 500ms of node placement)
    showConfigPanel(nodeId);
  }

  function deselectNode() {
    if (state.selectedNodeId) clearInvalidConfigFieldsForNode(state.selectedNodeId);
    state.selectedNodeId = null;
    state.selectedEdgeId = null;
    clearConnectingState();
    clearSelectedNode();
    renderEdges();
  }

  function clearSelectedNode() {
    state.selectedNodeId = null;
    updateCanvasNodeSelection();
    configPanel.classList.remove('visible');
  }

  function updateCanvasNodeSelection() {
    canvasNodes.querySelectorAll('.canvas-node').forEach(function (el) {
      var selected = el.getAttribute('data-node-id') === state.selectedNodeId;
      el.classList.toggle('selected', selected);
      el.setAttribute('aria-pressed', selected ? 'true' : 'false');
    });
  }

  // Click on canvas background to deselect
  canvasArea.addEventListener('mousedown', function (e) {
    if (e.target === canvasArea || e.target === canvasEdges || e.target.tagName === 'svg' || e.target.classList.contains('canvas-grid') || e.target === canvasNodes) {
      deselectNode();
    }
  });

  // --- Config Panel ---
  function showConfigPanel(nodeId) {
    const node = state.nodes.find(function (n) { return n.id === nodeId; });
    if (!node) return;

    configPanelTitle.textContent = nodeTypeLabel(node.type) + ' ' + tr('configurationSuffix');
    configPanelBody.innerHTML = buildConfigForm(node);
    configPanel.classList.add('visible');
    updateConfigErrorSummary([]);

    // Attach change listeners
    attachConfigListeners(node);
    applyConfigPanelReadOnly();
  }

  function buildConfigForm(node) {
    var html = '<div id="configErrorSummary" class="config-error-summary" role="alert" tabindex="-1" hidden></div>';
    // Common: Label
    html += '<div class="config-field">';
    html += '<label>' + escapeHtml(tr('label')) + '</label>';
    html += '<input type="text" id="cfgLabel" value="' + escapeAttr(node.label) + '">';
    html += '</div>';

    switch (node.type) {
      case 'trigger':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('triggerType')) + '</label>';
        html += '<select id="cfgTriggerType">';
        html += '<option value="manual"' + (node.config.trigger_type === 'manual' ? ' selected' : '') + '>' + escapeHtml(tr('manual')) + '</option>';
        html += '<option value="api"' + (node.config.trigger_type === 'api' ? ' selected' : '') + '>' + escapeHtml(tr('apiCall')) + '</option>';
        html += '<option value="schedule"' + (node.config.trigger_type === 'schedule' ? ' selected' : '') + '>' + escapeHtml(tr('schedule')) + '</option>';
        html += '<option value="event"' + (node.config.trigger_type === 'event' ? ' selected' : '') + '>' + escapeHtml(tr('event')) + '</option>';
        html += '</select>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('description')) + '</label>';
        html += '<textarea id="cfgDescription">' + escapeHtml(node.config.description || '') + '</textarea>';
        html += '</div>';
        break;

      case 'form':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('description')) + '</label>';
        html += '<textarea id="cfgDescription">' + escapeHtml(node.config.description || '') + '</textarea>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('formFieldsJson')) + '</label>';
        html += '<textarea id="cfgFields">' + escapeHtml(JSON.stringify(node.config.fields || [], null, 2)) + '</textarea>';
        html += '</div>';
        break;

      case 'approval':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('approvalMode')) + '</label>';
        html += '<select id="cfgMode">';
        html += '<option value="single"' + (node.config.mode === 'single' ? ' selected' : '') + '>' + escapeHtml(tr('singleApprover')) + '</option>';
        html += '<option value="countersign"' + (node.config.mode === 'countersign' ? ' selected' : '') + '>' + escapeHtml(tr('countersign')) + '</option>';
        html += '<option value="any_n_of_m"' + (node.config.mode === 'any_n_of_m' ? ' selected' : '') + '>' + escapeHtml(tr('anyNOfM')) + '</option>';
        html += '<option value="sequential"' + (node.config.mode === 'sequential' ? ' selected' : '') + '>' + escapeHtml(tr('sequential')) + '</option>';
        html += '</select>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('approverIds')) + '</label>';
        html += '<button type="button" class="approver-select-control" id="cfgApproverIdsPicker">' + escapeHtml(formatApproverSelection(node.config.approver_ids || [], tr('chooseApprovers'))) + '</button>';
        html += '<input type="hidden" id="cfgApproverIds" value="' + escapeAttr((node.config.approver_ids || []).join(', ')) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('minApprovals')) + '</label>';
        html += '<input type="number" id="cfgMinApprovals" min="1" value="' + (node.config.min_approvals || 1) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('timeoutHours')) + '</label>';
        html += '<input type="number" id="cfgTimeout" min="1" max="720" value="' + (node.config.timeout_hours || 24) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('fallbackApprover')) + '</label>';
        html += '<button type="button" class="approver-select-control" id="cfgFallbackPicker">' + escapeHtml(formatApproverSelection(node.config.fallback_approver ? [node.config.fallback_approver] : [], tr('chooseFallbackApprover'))) + '</button>';
        html += '<input type="hidden" id="cfgFallback" value="' + escapeAttr(node.config.fallback_approver || '') + '">';
        html += '</div>';
        break;

      case 'condition_branch':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('branchesJson')) + '</label>';
        html += '<textarea id="cfgBranches">' + escapeHtml(JSON.stringify(node.config.branches || [], null, 2)) + '</textarea>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('defaultBranch')) + '</label>';
        html += '<input type="text" id="cfgDefaultBranch" value="' + escapeAttr(node.config.default_branch || '') + '">';
        html += '</div>';
        break;

      case 'action':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('actionType')) + '</label>';
        html += '<select id="cfgActionType">';
        html += '<option value=""' + (!node.config.action_type ? ' selected' : '') + '>' + escapeHtml(tr('selectPlaceholder')) + '</option>';
        html += '<option value="api_call"' + (node.config.action_type === 'api_call' ? ' selected' : '') + '>' + escapeHtml(tr('apiCall')) + '</option>';
        html += '<option value="update_status"' + (node.config.action_type === 'update_status' ? ' selected' : '') + '>' + escapeHtml(tr('updateStatus')) + '</option>';
        html += '<option value="webhook"' + (node.config.action_type === 'webhook' ? ' selected' : '') + '>' + escapeHtml(tr('webhook')) + '</option>';
        html += '</select>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('parametersJson')) + '</label>';
        html += '<textarea id="cfgParameters">' + escapeHtml(JSON.stringify(node.config.parameters || {}, null, 2)) + '</textarea>';
        html += '</div>';
        break;

      case 'notification':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('recipients')) + '</label>';
        html += '<input type="text" id="cfgRecipients" value="' + escapeAttr((node.config.recipients || []).join(', ')) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('messageTemplate')) + '</label>';
        html += '<textarea id="cfgMessageTemplate">' + escapeHtml(node.config.message_template || '') + '</textarea>';
        html += '</div>';
        break;

      case 'sub_process':
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('workflowId')) + '</label>';
        html += '<input type="text" id="cfgWorkflowId" value="' + escapeAttr(node.config.workflow_id || '') + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>' + escapeHtml(tr('inputMappingJson')) + '</label>';
        html += '<textarea id="cfgInputMapping">' + escapeHtml(JSON.stringify(node.config.input_mapping || {}, null, 2)) + '</textarea>';
        html += '</div>';
        break;

      case 'terminal':
        if (window.buildTerminalNodeConfigForm) {
          html += window.buildTerminalNodeConfigForm(node);
        } else {
          html += '<div class="config-field"><label>' + escapeHtml(tr('resultExecutorsJson')) + '</label>';
          html += '<textarea id="cfgResultExecutors">' + escapeHtml(JSON.stringify(node.config.result_executors || [], null, 2)) + '</textarea></div>';
          html += '<div class="config-field"><label>' + escapeHtml(tr('notifiersJson')) + '</label>';
          html += '<textarea id="cfgNotifiers">' + escapeHtml(JSON.stringify(node.config.notifiers || [], null, 2)) + '</textarea></div>';
        }
        break;
    }

    // Delete button
    html += '<div style="margin-top:20px;padding-top:14px;border-top:1px solid var(--line);">';
    html += '<button id="cfgDeleteNode" style="width:100%;padding:9px;border-radius:8px;border:1px solid rgba(193,62,53,0.2);background:rgba(193,62,53,0.06);color:#c5221f;font-weight:700;font-size:13px;cursor:pointer;">' + escapeHtml(tr('deleteNode')) + '</button>';
    html += '</div>';

    return html;
  }

  function escapeAttr(str) {
    return String(str).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function attachConfigListeners(node) {
    var labelInput = document.getElementById('cfgLabel');
    if (labelInput) {
      labelInput.addEventListener('input', function () {
        node.label = labelInput.value;
        node.label_is_default = false;
        markDirty();
        var nodeFrame = findCanvasNodeElement(node.id);
        var nodeEl = nodeFrame ? nodeFrame.querySelector('.canvas-node-label') : null;
        if (nodeEl) nodeEl.textContent = node.label;
        if (nodeFrame) nodeFrame.setAttribute('aria-label', node.label + ' ' + nodeTypeLabel(node.type));
      });
    }

    // Type-specific listeners
    switch (node.type) {
      case 'trigger':
        bindSelect('cfgTriggerType', function (v) { node.config.trigger_type = v; });
        bindTextarea('cfgDescription', function (v) { node.config.description = v; });
        break;
      case 'form':
        bindTextarea('cfgDescription', function (v) { node.config.description = v; });
        bindJsonTextarea('cfgFields', function (v) { node.config.fields = v; });
        break;
      case 'approval':
        bindSelect('cfgMode', function (v) { node.config.mode = v; });
        bindApproverPicker(node, 'cfgApproverIdsPicker', true, function (ids) { node.config.approver_ids = ids; });
        bindInput('cfgMinApprovals', function (v) { node.config.min_approvals = parseInt(v, 10) || 1; });
        bindInput('cfgTimeout', function (v) { node.config.timeout_hours = Math.min(720, Math.max(1, parseInt(v, 10) || 24)); });
        bindApproverPicker(node, 'cfgFallbackPicker', false, function (ids) { node.config.fallback_approver = ids[0] || ''; });
        break;
      case 'condition_branch':
        bindJsonTextarea('cfgBranches', function (v) { node.config.branches = v; });
        bindInput('cfgDefaultBranch', function (v) { node.config.default_branch = v.trim(); });
        break;
      case 'action':
        bindSelect('cfgActionType', function (v) { node.config.action_type = v; });
        bindJsonTextarea('cfgParameters', function (v) { node.config.parameters = v; });
        break;
      case 'notification':
        bindInput('cfgRecipients', function (v) { node.config.recipients = v.split(',').map(function (s) { return s.trim(); }).filter(Boolean); });
        bindTextarea('cfgMessageTemplate', function (v) { node.config.message_template = v; });
        break;
      case 'sub_process':
        bindInput('cfgWorkflowId', function (v) { node.config.workflow_id = v.trim(); });
        bindJsonTextarea('cfgInputMapping', function (v) { node.config.input_mapping = v; });
        break;
      case 'terminal':
        if (window.attachTerminalNodeConfigListeners) {
          window.attachTerminalNodeConfigListeners(node, null, markDirty);
        } else {
          bindJsonTextarea('cfgResultExecutors', function (v) { node.config.result_executors = v; });
          bindJsonTextarea('cfgNotifiers', function (v) { node.config.notifiers = v; });
        }
        break;
    }

    // Delete node
    var deleteBtn = document.getElementById('cfgDeleteNode');
    if (deleteBtn) {
      deleteBtn.addEventListener('click', function () {
        deleteNode(node.id);
      });
    }
  }

  function applyConfigPanelReadOnly() {
    if (!isReadOnlyPreview()) return;
    configPanelBody.querySelectorAll('input, textarea, select, button').forEach(function (el) {
      el.disabled = true;
      el.setAttribute('aria-disabled', 'true');
    });
  }

  function refreshLocalizedUI() {
    canvasNodes.querySelectorAll('.canvas-node').forEach(function (el) {
      var nodeId = el.getAttribute('data-node-id');
      var node = state.nodes.find(function (n) { return n.id === nodeId; });
      if (!node) return;
      var typeEl = el.querySelector('.canvas-node-type');
      if (typeEl) typeEl.textContent = nodeTypeLabel(node.type);
      var labelEl = el.querySelector('.canvas-node-label');
      if (labelEl && node.label_is_default) {
        node.label = nodeTypeLabel(node.type);
        labelEl.textContent = node.label;
      }
      el.setAttribute('aria-label', node.label + ' ' + nodeTypeLabel(node.type));
    });
    updateToolModeUI();
    renderWorkflowLibrary();
    updateDocumentTitle();
    hideContextMenu();
    if (state.selectedNodeId) showConfigPanel(state.selectedNodeId);
  }

  window.addEventListener('approval-workflow-language-change', refreshLocalizedUI);

  function bindInput(id, cb) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('input', function () { cb(el.value); markDirty(); });
  }
  function bindSelect(id, cb) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('change', function () { cb(el.value); markDirty(); });
  }
  function bindTextarea(id, cb) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('input', function () { cb(el.value); markDirty(); });
  }

  function bindJsonTextarea(id, cb) {
    var el = document.getElementById(id);
    if (!el) return;
    el.addEventListener('input', function () {
      var fieldKey = (state.selectedNodeId || '') + ':' + id;
      markDirty();
      try {
        cb(JSON.parse(el.value));
        delete state.invalidConfigFields[fieldKey];
        el.classList.remove('config-field-invalid');
        el.setAttribute('aria-invalid', 'false');
        updateConfigErrorSummary(getInvalidConfigErrors());
      } catch (e) {
        state.invalidConfigFields[fieldKey] = el.previousElementSibling && el.previousElementSibling.textContent || id;
        el.classList.add('config-field-invalid');
        el.setAttribute('aria-invalid', 'true');
        updateConfigErrorSummary(getInvalidConfigErrors());
      }
    });
  }

  function bindApproverPicker(node, buttonId, multiple, cb) {
    var button = document.getElementById(buttonId);
    if (!button) return;
    button.addEventListener('click', function () {
      if (isReadOnlyPreview()) return;
      var current = multiple ? (node.config.approver_ids || []) : (node.config.fallback_approver ? [node.config.fallback_approver] : []);
      openApproverPicker({
        multiple: multiple,
        selectedIds: current,
        title: multiple ? tr('approverPickerTitle') : tr('fallbackApproverPickerTitle'),
        onConfirm: function (ids) {
          cb(ids);
          syncApproverPickerField(buttonId, ids, multiple);
          markDirty();
        }
      });
    });
  }

  function syncApproverPickerField(buttonId, ids, multiple) {
    ids = normalizeApproverIds(ids);
    if (buttonId === 'cfgApproverIdsPicker') {
      var approverInput = document.getElementById('cfgApproverIds');
      var approverButton = document.getElementById('cfgApproverIdsPicker');
      if (approverInput) approverInput.value = ids.join(', ');
      if (approverButton) approverButton.textContent = formatApproverSelection(ids, tr('chooseApprovers'));
      return;
    }
    var fallbackInput = document.getElementById('cfgFallback');
    var fallbackButton = document.getElementById('cfgFallbackPicker');
    var value = multiple ? ids : ids.slice(0, 1);
    if (fallbackInput) fallbackInput.value = value[0] || '';
    if (fallbackButton) fallbackButton.textContent = formatApproverSelection(value, tr('chooseFallbackApprover'));
  }

  function normalizeApproverIds(ids) {
    var seen = {};
    var out = [];
    (ids || []).forEach(function (id) {
      id = String(id || '').trim();
      if (!id || seen[id]) return;
      seen[id] = true;
      out.push(id);
    });
    return out;
  }

  function formatApproverSelection(ids, emptyText) {
    ids = normalizeApproverIds(ids);
    if (!ids.length) return emptyText;
    var directory = state.approverDirectory;
    if (!directory || !directory.byId) return tr('selectedApprovers', { count: ids.length });
    var names = ids.map(function (id) {
      var entry = directory && directory.byId && directory.byId[id];
      return entry && entry.name ? entry.name : '';
    }).filter(Boolean);
    return names.length === ids.length ? names.join(', ') : tr('selectedApprovers', { count: ids.length });
  }

  function ensureApproverPickerShell() {
    var overlay = document.getElementById('approverPickerOverlay');
    if (overlay) return overlay;
    overlay = document.createElement('div');
    overlay.id = 'approverPickerOverlay';
    overlay.className = 'approver-picker-overlay';
    overlay.hidden = true;
    overlay.innerHTML =
      '<div class="approver-picker-dialog" role="dialog" aria-modal="true" aria-labelledby="approverPickerTitle">' +
        '<div class="approver-picker-head">' +
          '<div><h2 id="approverPickerTitle"></h2><p id="approverPickerCount"></p></div>' +
          '<button type="button" class="approver-picker-close" id="approverPickerClose" aria-label="Close">&times;</button>' +
        '</div>' +
        '<div class="approver-picker-tabs" id="approverPickerTabs">' +
          '<button type="button" data-approver-view="organization"></button>' +
          '<button type="button" data-approver-view="function"></button>' +
          '<button type="button" data-approver-view="direct"></button>' +
        '</div>' +
        '<input class="approver-picker-search" id="approverPickerSearch" autocomplete="off">' +
        '<div class="approver-picker-body" id="approverPickerBody"></div>' +
        '<div class="approver-picker-actions">' +
          '<button type="button" class="approver-picker-secondary" id="approverPickerClear"></button>' +
          '<span></span>' +
          '<button type="button" class="approver-picker-secondary" id="approverPickerCancel"></button>' +
          '<button type="button" class="approver-picker-primary" id="approverPickerConfirm"></button>' +
        '</div>' +
      '</div>';
    document.body.appendChild(overlay);
    overlay.addEventListener('mousedown', function (event) {
      if (event.target === overlay) closeApproverPicker();
    });
    document.getElementById('approverPickerClose').addEventListener('click', closeApproverPicker);
    document.getElementById('approverPickerCancel').addEventListener('click', closeApproverPicker);
    document.getElementById('approverPickerClear').addEventListener('click', function () {
      if (!state.approverPicker) return;
      state.approverPicker.selected = {};
      renderApproverPicker();
    });
    overlay.querySelectorAll('[data-approver-view]').forEach(function (button) {
      button.addEventListener('click', function () {
        if (!state.approverPicker) return;
        state.approverPicker.view = button.getAttribute('data-approver-view') || 'organization';
        renderApproverPicker();
      });
    });
    document.getElementById('approverPickerConfirm').addEventListener('click', function () {
      if (!state.approverPicker) return;
      var ids = Object.keys(state.approverPicker.selected || {}).filter(function (id) { return state.approverPicker.selected[id]; });
      state.approverPicker.onConfirm(ids);
      closeApproverPicker();
    });
    document.getElementById('approverPickerSearch').addEventListener('input', renderApproverPicker);
    return overlay;
  }

  function openApproverPicker(options) {
    var overlay = ensureApproverPickerShell();
    var selected = {};
    normalizeApproverIds(options.selectedIds).forEach(function (id) { selected[id] = true; });
    state.approverPicker = {
      multiple: !!options.multiple,
      selected: selected,
      onConfirm: typeof options.onConfirm === 'function' ? options.onConfirm : function () {},
      title: options.title || tr('approverPickerTitle'),
      view: options.view || 'organization'
    };
    document.getElementById('approverPickerTitle').textContent = state.approverPicker.title;
    var tabLabels = {
      organization: tr('approverViewOrganization'),
      function: tr('approverViewFunction'),
      direct: tr('approverViewDirect')
    };
    overlay.querySelectorAll('[data-approver-view]').forEach(function (button) {
      var view = button.getAttribute('data-approver-view') || '';
      button.textContent = tabLabels[view] || view;
    });
    document.getElementById('approverPickerSearch').placeholder = tr('approverPickerSearch');
    document.getElementById('approverPickerSearch').value = '';
    document.getElementById('approverPickerClear').textContent = tr('clearSelection');
    document.getElementById('approverPickerCancel').textContent = tr('cancel');
    document.getElementById('approverPickerConfirm').textContent = tr('confirm');
    overlay.hidden = false;
    loadAndRenderApproverDirectory();
    renderApproverPicker();
    setTimeout(function () {
      var search = document.getElementById('approverPickerSearch');
      if (search) search.focus();
    }, 0);
  }

  function closeApproverPicker() {
    var overlay = document.getElementById('approverPickerOverlay');
    if (overlay) overlay.hidden = true;
    state.approverPicker = null;
  }

  async function loadAndRenderApproverDirectory() {
    try {
      await loadApproverDirectory();
      pruneApproverPickerSelection();
      refreshApproverControls();
      renderApproverPicker();
    } catch (err) {
      var body = document.getElementById('approverPickerBody');
      if (body) body.innerHTML = '<div class="approver-picker-empty">' + escapeHtml(tr('approverPickerLoadFailed', { error: err && err.message || String(err) })) + '</div>';
    }
  }

  function refreshApproverControls() {
    var node = state.nodes.find(function (n) { return n.id === state.selectedNodeId; });
    if (!node || node.type !== 'approval') return;
    syncApproverPickerField('cfgApproverIdsPicker', node.config.approver_ids || [], true);
    syncApproverPickerField('cfgFallbackPicker', node.config.fallback_approver ? [node.config.fallback_approver] : [], false);
  }

  function pruneApproverPickerSelection() {
    if (!state.approverPicker || !state.approverDirectory || !state.approverDirectory.byId) return;
    Object.keys(state.approverPicker.selected || {}).forEach(function (id) {
      if (!state.approverDirectory.byId[id]) delete state.approverPicker.selected[id];
    });
  }

  async function loadApproverDirectory() {
    if (state.approverDirectory) return state.approverDirectory;
    if (state.approverDirectoryLoading) return state.approverDirectoryLoading;
    state.approverDirectoryLoading = fetchApproverDirectory();
    return state.approverDirectoryLoading;
  }

  async function fetchApproverDirectory() {
    try {
      var data = await workflowApi('/api/v1/workflow-directory/approvers');
      var groupRoot = normalizeGroupNode(data && data.tree);
      var membersByGroup = data && data.members_by_group || {};
      var usersByEmail = {};
      (data && data.users || []).forEach(function (user) {
        var email = normalizeEmail(user && user.email);
        if (email) usersByEmail[email] = user;
      });
      var machinesByEmail = {};
      (data && data.machines || []).forEach(function (machine) {
        var email = normalizeEmail(machine && (machine.user_email || machine.UserEmail));
        var machineId = String(machine && (machine.machine_id || machine.id || machine.ID || machine.MachineID) || '').trim();
        if (!email || !machineId) return;
        if (!machinesByEmail[email]) machinesByEmail[email] = [];
        machinesByEmail[email].push({ id: machineId, name: machine.alias || machine.name || machine.hostname || '' });
      });
      var veEntries = (data && data.employees || []).filter(function (entry) {
        return String(entry && entry.status || '').toLowerCase() === 'active';
      }).map(function (entry) {
        var ownerEmail = normalizeEmail(entry && entry.owner_email);
        var visibleGroupIds = Array.isArray(entry && entry.visible_group_ids) ? entry.visible_group_ids.map(function (id) { return String(id || '').trim(); }).filter(Boolean) : [];
        return {
          id: String(entry.machine_id || entry.id || '').trim(),
          name: String(entry.name || entry.owner_email || tr('virtualEmployee')).trim(),
          kind: ownerEmail ? 'digital_twin' : 'department_digital_employee',
          ownerEmail: ownerEmail,
          visibleGroupIds: visibleGroupIds,
          employeeType: String(entry.employee_type || '').trim()
        };
      }).filter(function (entry) { return entry.id; });
      var approvalRoles = (data && data.approval_roles || []).map(normalizeApprovalRole).filter(Boolean);
      var directory = { root: groupRoot, membersByGroup: membersByGroup, usersByEmail: usersByEmail, machinesByEmail: machinesByEmail, veEntries: veEntries, approvalRoles: approvalRoles, byId: {} };
      indexApproverDirectory(directory);
      state.approverDirectory = directory;
      return directory;
    } finally {
      state.approverDirectoryLoading = false;
    }
  }

  function normalizeGroupNode(node) {
    if (!node) return null;
    return {
      id: String(node.id || node.ID || '').trim(),
      name: String(node.name || node.Name || '').trim(),
      children: (node.children || node.Children || []).map(normalizeGroupNode).filter(Boolean)
    };
  }

  function normalizeEmail(email) {
    return String(email || '').trim().toLowerCase();
  }

  function indexApproverDirectory(directory) {
    approverRoleCatalog(directory).forEach(function (role) {
      directory.byId[role.id] = { id: role.id, name: role.scopeName + ' / ' + role.roleName, kind: 'role' };
    });
    (directory.veEntries || []).forEach(function (entry) {
      directory.byId[entry.id] = entry;
    });
    Object.keys(directory.machinesByEmail || {}).forEach(function (email) {
      var user = directory.usersByEmail[email] || {};
      (directory.machinesByEmail[email] || []).forEach(function (machine) {
        directory.byId[machine.id] = { id: machine.id, name: user.email || email, kind: 'machine', email: email };
      });
    });
  }

  function approverRowsForGroup(directory, group) {
    var rows = [];
    (directory.membersByGroup[group.id] || []).forEach(function (email) {
      email = normalizeEmail(email);
      var machines = directory.machinesByEmail[email] || [];
      if (!machines.length) {
        rows.push({ disabled: true, name: email, meta: tr('noApproverIdentity') });
        return;
      }
      machines.forEach(function (machine) {
        rows.push({ id: machine.id, name: email, kind: 'machine' });
      });
      (directory.veEntries || []).filter(function (entry) {
        return entry.ownerEmail === email;
      }).forEach(function (entry) {
        rows.push({
          id: entry.id,
          name: entry.name,
          kind: 'digital_twin',
          meta: tr('digitalTwin'),
          detail: email
        });
      });
    });
    (directory.veEntries || []).filter(function (entry) {
      if (entry.ownerEmail) return false;
      if (Array.isArray(entry.visibleGroupIds) && entry.visibleGroupIds.length) {
        return entry.visibleGroupIds.indexOf(group.id) !== -1;
      }
      return group === directory.root;
    }).forEach(function (entry) {
      rows.push({
        id: entry.id,
        name: entry.name,
        kind: 'department_digital_employee',
        meta: tr('departmentDigitalEmployee')
      });
    });
    return rows;
  }

  function approverRoleCatalog(directory) {
    if (directory && Array.isArray(directory.approvalRoles) && directory.approvalRoles.length) return directory.approvalRoles;
    var fromHub = loadStoredApprovalRoles();
    if (fromHub.length) return fromHub;
    return defaultApprovalRoles();
  }

  function loadStoredApprovalRoles() {
    try {
      if (!window.localStorage) return [];
      var raw = window.localStorage.getItem('maclaw_approval_roles_v1');
      if (!raw) return [];
      var parsed = JSON.parse(raw);
      var roles = Array.isArray(parsed) ? parsed : parsed.roles;
      if (!Array.isArray(roles)) return [];
      return roles.map(normalizeApprovalRole).filter(Boolean);
    } catch (_) {
      return [];
    }
  }

  function normalizeApprovalRole(role) {
    if (!role) return null;
    var scopeType = String(role.scopeType || role.scope_type || '').trim() || 'global';
    var scopeId = String(role.scopeId || role.scope_id || '').trim() || 'global';
    var roleCode = String(role.roleCode || role.role_code || role.code || '').trim();
    var roleName = String(role.roleName || role.role_name || role.name || roleCode).trim();
    if (!roleCode || !roleName) return null;
    return {
      id: approvalRoleId(scopeType, scopeId, roleCode),
      roleCode: roleCode,
      roleName: roleName,
      scopeType: scopeType,
      scopeId: scopeId,
      scopeName: String(role.scopeName || role.scope_name || scopeId).trim() || scopeId,
      view: String(role.view || (scopeType === 'function' ? 'function' : 'organization')).trim(),
      executionMode: String(role.executionMode || role.execution_mode || 'manual').trim(),
      assignees: Array.isArray(role.assignees) ? role.assignees : []
    };
  }

  function approvalRoleId(scopeType, scopeId, roleCode) {
    return ['role', scopeType || 'global', scopeId || 'global', roleCode || ''].map(encodeURIComponent).join(':');
  }

  function defaultApprovalRoles() {
    return [
      { scopeType: 'dynamic', scopeId: 'applicant_department', scopeName: tr('applicantDepartmentScope'), roleCode: 'department_manager', roleName: tr('departmentManager') || 'Department Manager', view: 'organization', executionMode: 'manual', assignees: [] },
      { scopeType: 'dynamic', scopeId: 'applicant_department', scopeName: tr('applicantDepartmentScope'), roleCode: 'direct_manager', roleName: tr('directManager') || 'Direct Manager', view: 'organization', executionMode: 'manual', assignees: [] },
      { scopeType: 'function', scopeId: 'finance', scopeName: tr('functionScopeFinance'), roleCode: 'finance_approver', roleName: tr('financeApprover') || 'Finance Approver', view: 'function', executionMode: 'digital_review', assignees: [] },
      { scopeType: 'function', scopeId: 'procurement', scopeName: tr('functionScopeProcurement'), roleCode: 'procurement_approver', roleName: tr('procurementApprover') || 'Procurement Approver', view: 'function', executionMode: 'digital_suggest', assignees: [] },
      { scopeType: 'function', scopeId: 'legal', scopeName: tr('functionScopeLegal'), roleCode: 'contract_approver', roleName: tr('contractApprover') || 'Contract Approver', view: 'function', executionMode: 'digital_review', assignees: [] },
      { scopeType: 'function', scopeId: 'it', scopeName: tr('functionScopeIT'), roleCode: 'it_approver', roleName: tr('itApprover') || 'IT Approver', view: 'function', executionMode: 'manual', assignees: [] }
    ].map(normalizeApprovalRole).filter(Boolean);
  }

  function approvalRoleRows(view, directory, query) {
    var roles = approverRoleCatalog(directory).filter(function (role) {
      if (view === 'function') return role.view === 'function' || role.scopeType === 'function';
      if (view === 'organization') return role.view !== 'function' && role.scopeType !== 'function';
      return false;
    });
    query = String(query || '').trim().toLowerCase();
    if (query) {
      roles = roles.filter(function (role) {
        return [role.roleName, role.roleCode, role.scopeName, role.scopeId, executionModeLabel(role.executionMode), assigneeSummary(role, directory)].join(' ').toLowerCase().indexOf(query) !== -1;
      });
    }
    return roles.map(function (role) {
      return {
        id: role.id,
        name: role.scopeName + ' / ' + role.roleName,
        kind: 'role',
        meta: executionModeLabel(role.executionMode),
        detail: assigneeSummary(role, directory) || tr('approverPickerEmpty')
      };
    });
  }

  function assigneeSummary(role, directory) {
    var names = (role.assignees || []).map(function (assignee) {
      var id = String(assignee && (assignee.subjectId || assignee.subject_id || assignee.id) || '').trim();
      var entry = id && directory && directory.byId && directory.byId[id];
      return String(assignee && (assignee.displayName || assignee.display_name || assignee.name) || entry && entry.name || id || '').trim();
    }).filter(Boolean);
    return names.slice(0, 3).join(', ') + (names.length > 3 ? ' +' + (names.length - 3) : '');
  }

  function executionModeLabel(mode) {
    mode = String(mode || 'manual');
    if (mode === 'digital_suggest') return tr('roleExecutionDigitalSuggest');
    if (mode === 'digital_review') return tr('roleExecutionDigitalReview');
    if (mode === 'auto') return tr('roleExecutionAuto');
    return tr('roleExecutionManual');
  }

  function renderApproverPicker() {
    var picker = state.approverPicker;
    var body = document.getElementById('approverPickerBody');
    var count = document.getElementById('approverPickerCount');
    if (!picker || !body) return;
    var selectedIds = Object.keys(picker.selected || {}).filter(function (id) { return picker.selected[id]; });
    if (count) count.textContent = tr('selectedApprovers', { count: selectedIds.length });
    var directory = state.approverDirectory;
    if (!directory) {
      body.innerHTML = '<div class="approver-picker-empty">' + escapeHtml(tr('approverPickerLoading')) + '</div>';
      return;
    }
    var queryEl = document.getElementById('approverPickerSearch');
    var query = String(queryEl && queryEl.value || '').trim().toLowerCase();
    var view = picker.view || 'organization';
    var tabs = document.getElementById('approverPickerTabs');
    if (tabs) {
      tabs.querySelectorAll('[data-approver-view]').forEach(function (button) {
        var active = button.getAttribute('data-approver-view') === view;
        button.classList.toggle('active', active);
        button.setAttribute('aria-pressed', active ? 'true' : 'false');
      });
    }
    var html = '';
    if (view === 'function') {
      html = renderApproverRowsSection(tr('approverViewFunction'), approvalRoleRows('function', directory, query));
    } else if (view === 'direct') {
      html = renderApproverGroup(directory, directory.root, query, { includeRoles: false });
    } else {
      html = renderApproverRowsSection(tr('approverRole'), approvalRoleRows('organization', directory, query)) + renderApproverGroup(directory, directory.root, query, { includeRoles: false });
    }
    body.innerHTML = html || '<div class="approver-picker-empty">' + escapeHtml(tr('approverPickerEmpty')) + '</div>';
    body.querySelectorAll('[data-approver-id]').forEach(function (row) {
      row.addEventListener('click', function () { toggleApproverSelection(row.getAttribute('data-approver-id')); });
      row.addEventListener('keydown', function (event) {
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
        toggleApproverSelection(row.getAttribute('data-approver-id'));
      });
    });
  }

  function renderApproverRowsSection(title, rows) {
    rows = rows || [];
    if (!rows.length) return '';
    return '<section class="approver-group approver-role-group"><h3>' + escapeHtml(title) + '</h3>' + rows.map(renderApproverRow).join('') + '</section>';
  }

  function renderApproverGroup(directory, group, query, options) {
    if (!group) return '<div class="approver-picker-empty">' + escapeHtml(tr('approverPickerEmpty')) + '</div>';
    var rows = approverRowsForGroup(directory, group).filter(function (row) {
      if (!query) return true;
      return [row.name, row.meta, row.detail, approverKindLabel(row.kind), group.name].join(' ').toLowerCase().indexOf(query) !== -1;
    });
    var childHtml = (group.children || []).map(function (child) { return renderApproverGroup(directory, child, query, options); }).join('');
    var rowsHtml = rows.map(renderApproverRow).join('');
    if (query && !rowsHtml && !childHtml) return '';
    return '<section class="approver-group"><h3>' + escapeHtml(group.name || 'Root') + '</h3>' + (rowsHtml || '<div class="approver-picker-subempty">' + escapeHtml(tr('approverPickerEmpty')) + '</div>') + childHtml + '</section>';
  }

  function renderApproverRow(row) {
    if (row.disabled) {
      return '<div class="approver-row disabled"><span>' + escapeHtml(row.name) + '</span><small>' + escapeHtml(row.meta || '') + '</small></div>';
    }
    var selected = !!(state.approverPicker && state.approverPicker.selected && state.approverPicker.selected[row.id]);
    var kind = approverKindLabel(row.kind);
    var detail = row.detail ? '<em>' + escapeHtml(row.detail) + '</em>' : '';
    return '<div class="approver-row' + (selected ? ' selected' : '') + '" role="button" tabindex="0" data-approver-id="' + escapeAttr(row.id) + '"><span>' + escapeHtml(row.name || kind) + detail + '</span><small>' + escapeHtml(row.meta || kind) + '</small></div>';
  }

  function approverKindLabel(kind) {
    if (kind === 'role') return tr('approverRole');
    if (kind === 've') return tr('virtualEmployee');
    if (kind === 'department_digital_employee') return tr('departmentDigitalEmployee');
    if (kind === 'digital_twin') return tr('digitalTwin');
    return tr('userMachine');
  }

  function toggleApproverSelection(id) {
    if (!state.approverPicker || !id) return;
    if (!state.approverPicker.multiple) state.approverPicker.selected = {};
    state.approverPicker.selected[id] = !state.approverPicker.selected[id];
    renderApproverPicker();
  }

  function clearInvalidConfigFieldsForNode(nodeId) {
    var prefix = nodeId + ':';
    Object.keys(state.invalidConfigFields).forEach(function (key) {
      if (key.indexOf(prefix) === 0) delete state.invalidConfigFields[key];
    });
  }

  function isEditingField() {
    return document.activeElement && ['INPUT', 'TEXTAREA', 'SELECT'].indexOf(document.activeElement.tagName) !== -1;
  }

  // --- Delete node ---
  function deleteNode(nodeId) {
    if (isReadOnlyPreview()) return;
    var existed = state.nodes.some(function (n) { return n.id === nodeId; });
    if (!existed) return;
    if (state.connectingFrom === nodeId) clearConnectingState();
    clearInvalidConfigFieldsForNode(nodeId);
    state.nodes = state.nodes.filter(function (n) { return n.id !== nodeId; });
    state.edges = state.edges.filter(function (e) { return e.source_id !== nodeId && e.target_id !== nodeId; });
    var el = findCanvasNodeElement(nodeId);
    if (el) el.remove();
    deselectNode();
    renderEdges();
    updateEmptyState();
    markDirty();
  }

  function deleteEdge(edgeId) {
    if (isReadOnlyPreview()) return;
    var before = state.edges.length;
    state.edges = state.edges.filter(function (e) { return e.id !== edgeId; });
    if (state.edges.length === before) return;
    if (state.selectedEdgeId === edgeId) state.selectedEdgeId = null;
    renderEdges();
    updateToolModeUI();
    markDirty();
  }

  function deleteEdgesForNode(nodeId, direction) {
    if (isReadOnlyPreview()) return;
    var before = state.edges.length;
    state.edges = state.edges.filter(function (edge) {
      if (direction === 'incoming') return edge.target_id !== nodeId;
      if (direction === 'outgoing') return edge.source_id !== nodeId;
      return edge.source_id !== nodeId && edge.target_id !== nodeId;
    });
    if (state.edges.length === before) return;
    if (state.selectedEdgeId && !state.edges.some(function (edge) { return edge.id === state.selectedEdgeId; })) state.selectedEdgeId = null;
    renderEdges();
    updateToolModeUI();
    markDirty();
  }

  function selectEdge(edgeId) {
    state.selectedEdgeId = edgeId;
    state.selectedNodeId = null;
    clearConnectingState();
    updateCanvasNodeSelection();
    configPanel.classList.remove('visible');
    renderEdges();
  }

  function edgeAriaLabel(edge) {
    var sourceNode = state.nodes.find(function (n) { return n.id === edge.source_id; });
    var targetNode = state.nodes.find(function (n) { return n.id === edge.target_id; });
    return tr('edgeConnectorLabel', {
      source: sourceNode ? sourceNode.label : edge.source_id,
      target: targetNode ? targetNode.label : edge.target_id
    });
  }

  function activateEdge(edgeId) {
    if (isReadOnlyPreview()) {
      selectEdge(edgeId);
      return;
    }
    if (state.toolMode === 'connect') return;
    if (state.toolMode === 'delete_edge') {
      deleteEdge(edgeId);
      return;
    }
    selectEdge(edgeId);
  }

  // --- Edges ---
  function canCreateEdge(sourceId, targetId) {
    if (!sourceId || !targetId || sourceId === targetId) return false;
    var sourceNode = state.nodes.find(function (n) { return n.id === sourceId; });
    var targetNode = state.nodes.find(function (n) { return n.id === targetId; });
    if (!sourceNode || !targetNode) return false;
    if (sourceNode.type === 'terminal') return false;
    if (targetNode.type === 'trigger') return false;
    return true;
  }

  function addEdge(sourceId, targetId) {
    if (isReadOnlyPreview()) return;
    if (!canCreateEdge(sourceId, targetId)) return;
    // Prevent duplicate edges
    var exists = state.edges.some(function (e) {
      return e.source_id === sourceId && e.target_id === targetId;
    });
    if (exists) return;

    var edge = {
      id: generateEdgeId(),
      source_id: sourceId,
      target_id: targetId,
      label: '',
      priority: 0,
    };
    state.edges.push(edge);
    markDirty();
    renderEdges();
    updateToolModeUI();
  }

  function edgeNodeBox(node, el) {
    var width = el && el.offsetWidth ? el.offsetWidth : 160;
    var height = el && el.offsetHeight ? el.offsetHeight : 60;
    var left = node.position.x;
    var top = node.position.y;
    return {
      left: left,
      top: top,
      right: left + width,
      bottom: top + height,
      cx: left + width / 2,
      cy: top + height / 2,
      width: width,
      height: height
    };
  }

  function edgeAnchorPoints(sourceNode, sourceEl, targetNode, targetEl) {
    var source = edgeNodeBox(sourceNode, sourceEl);
    var target = edgeNodeBox(targetNode, targetEl);
    var dx = target.cx - source.cx;
    var dy = target.cy - source.cy;
    if (Math.abs(dx) >= Math.abs(dy)) {
      var sourceX = dx >= 0 ? source.right : source.left;
      var targetX = dx >= 0 ? target.left : target.right;
      return {
        orientation: 'horizontal',
        sx: sourceX,
        sy: source.cy,
        tx: targetX,
        ty: target.cy,
        direction: dx >= 0 ? 1 : -1
      };
    }
    var sourceY = dy >= 0 ? source.bottom : source.top;
    var targetY = dy >= 0 ? target.top : target.bottom;
    return {
      orientation: 'vertical',
      sx: source.cx,
      sy: sourceY,
      tx: target.cx,
      ty: targetY,
      direction: dy >= 0 ? 1 : -1
    };
  }

  function edgePathD(points) {
    if (points.orientation === 'horizontal') {
      var xOffset = Math.max(40, Math.min(180, Math.abs(points.tx - points.sx) * 0.5));
      var xControl = xOffset * points.direction;
      return 'M ' + points.sx + ' ' + points.sy + ' C ' + (points.sx + xControl) + ' ' + points.sy + ', ' + (points.tx - xControl) + ' ' + points.ty + ', ' + points.tx + ' ' + points.ty;
    }
    var yOffset = Math.max(40, Math.min(160, Math.abs(points.ty - points.sy) * 0.5));
    var yControl = yOffset * points.direction;
    return 'M ' + points.sx + ' ' + points.sy + ' C ' + points.sx + ' ' + (points.sy + yControl) + ', ' + points.tx + ' ' + (points.ty - yControl) + ', ' + points.tx + ' ' + points.ty;
  }

  function ensureEdgeArrowhead(svg) {
    if (svg.querySelector('#arrowhead')) return;
    var defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
    defs.innerHTML = '<marker id="arrowhead" markerWidth="12" markerHeight="8" refX="10.5" refY="4" orient="auto" markerUnits="strokeWidth" overflow="visible"><path d="M 0 0 L 11 4 L 0 8 z" fill="#94a3b8"/></marker>';
    svg.insertBefore(defs, svg.firstChild);
  }

  function renderEdges() {
    var svg = canvasEdges.querySelector('svg');
    svg.innerHTML = '';
    ensureEdgeArrowhead(svg);

    state.edges.forEach(function (edge) {
      var sourceNode = state.nodes.find(function (n) { return n.id === edge.source_id; });
      var targetNode = state.nodes.find(function (n) { return n.id === edge.target_id; });
      if (!sourceNode || !targetNode) return;

      var sourceEl = findCanvasNodeElement(edge.source_id);
      var targetEl = findCanvasNodeElement(edge.target_id);
      if (!sourceEl || !targetEl) return;

      var d = edgePathD(edgeAnchorPoints(sourceNode, sourceEl, targetNode, targetEl));

      var path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      path.setAttribute('d', d);
      path.setAttribute('class', 'edge-path');
      path.setAttribute('data-edge-id', edge.id);
      if (state.selectedEdgeId === edge.id) path.classList.add('selected');

      var hitPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      hitPath.setAttribute('d', d);
      hitPath.setAttribute('class', 'edge-hit-path');
      hitPath.setAttribute('data-edge-id', edge.id);
      hitPath.setAttribute('role', 'button');
      hitPath.setAttribute('tabindex', state.toolMode === 'connect' ? '-1' : '0');
      hitPath.setAttribute('aria-disabled', state.toolMode === 'connect' ? 'true' : 'false');
      hitPath.setAttribute('aria-label', edgeAriaLabel(edge));
      hitPath.setAttribute('aria-pressed', state.selectedEdgeId === edge.id ? 'true' : 'false');
      hitPath.addEventListener('click', function (e) {
        e.stopPropagation();
        activateEdge(edge.id);
      });
      hitPath.addEventListener('contextmenu', function (e) {
        e.preventDefault();
        e.stopPropagation();
        selectEdge(edge.id);
        showContextMenu('edge', edge.id, e.clientX, e.clientY);
      });
      hitPath.addEventListener('keydown', function (e) {
        if (isContextMenuKey(e)) {
          e.preventDefault();
          selectEdge(edge.id);
          var point = elementCenterPoint(hitPath);
          showContextMenu('edge', edge.id, point.x, point.y);
          return;
        }
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          activateEdge(edge.id);
        }
        if ((e.key === 'Delete' || e.key === 'Backspace') && !isEditingField()) {
          e.preventDefault();
          deleteEdge(edge.id);
        }
      });

      // Arrow marker
      path.setAttribute('marker-end', 'url(#arrowhead)');

      svg.appendChild(hitPath);
      svg.appendChild(path);
    });
  }

  // --- Config panel close ---
  configPanelClose.addEventListener('click', function () {
    deselectNode();
  });

  canvasArea.addEventListener('contextmenu', function (e) {
    if (!(e.target === canvasArea || e.target === canvasEdges || e.target.tagName === 'svg' || e.target.classList.contains('canvas-grid') || e.target === canvasNodes)) return;
    e.preventDefault();
    deselectNode();
    showContextMenu('canvas', null, e.clientX, e.clientY, canvasDropPositionFromClient(e.clientX, e.clientY));
  });

  document.addEventListener('mousedown', function (e) {
    var menu = document.getElementById('workflowContextMenu');
    if (menu && !menu.hidden && !menu.contains(e.target)) hideContextMenu();
  });

  window.addEventListener('resize', hideContextMenu);
  window.addEventListener('blur', hideContextMenu);

  // --- Keyboard shortcuts ---
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Delete' || e.key === 'Backspace') {
      if (isContextMenuOpen()) {
        e.preventDefault();
        return;
      }
      if (state.selectedNodeId && !isEditingField()) {
        e.preventDefault();
        deleteNode(state.selectedNodeId);
      } else if (state.selectedEdgeId && !isEditingField()) {
        e.preventDefault();
        deleteEdge(state.selectedEdgeId);
      }
    }
    if (e.key === 'Escape') {
      if (isContextMenuOpen()) {
        e.preventDefault();
        hideContextMenu({ restoreFocus: true });
        return;
      }
      hideContextMenu();
      deselectNode();
      clearConnectingState();
      if (state.toolMode !== 'select') setToolMode('select');
    }
  });

  // --- Validation ---
  btnNew.addEventListener('click', function () {
    if (hasUnsavedChanges() && !confirm(tr('newWorkflowConfirm'))) return;
    resetWorkflowDesigner();
  });

  if (workflowNameInput) workflowNameInput.addEventListener('input', markDirty);
  if (workflowDescriptionInput) workflowDescriptionInput.addEventListener('input', markDirty);

  window.addEventListener('beforeunload', function (e) {
    if (!hasUnsavedChanges()) return;
    e.preventDefault();
    e.returnValue = tr('newWorkflowConfirm');
    return e.returnValue;
  });

  if (btnRefreshWorkflows) {
    btnRefreshWorkflows.addEventListener('click', function () { loadWorkflowLibrary(); });
  }

  if (workflowList) {
    workflowList.addEventListener('click', function (e) {
      var btn = e.target.closest('button[data-workflow-action]');
      if (!btn) return;
      var workflowId = btn.getAttribute('data-workflow-id') || '';
      var action = btn.getAttribute('data-workflow-action') || '';
      if (action === 'open') openWorkflowDesign(workflowId);
      if (action === 'delete') deleteWorkflowDesign(workflowId);
    });
  }

  if (workflowSearchInput) {
    workflowSearchInput.addEventListener('input', function () {
      state.workflowSearch = workflowSearchInput.value || '';
      renderWorkflowLibrary();
    });
  }

  if (workflowStatusFilter) {
    workflowStatusFilter.addEventListener('change', function () {
      state.workflowStatusFilter = workflowStatusFilter.value || '';
      renderWorkflowLibrary();
    });
  }

  document.querySelectorAll('[data-draft-example]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      if (!draftPrompt || state.isGeneratingDraft || isReadOnlyPreview()) return;
      draftPrompt.value = draftExampleText(btn.getAttribute('data-draft-example'));
      setDraftAssistantStatus('');
      draftPrompt.focus();
    });
  });

  if (btnGenerateDraft) {
    btnGenerateDraft.addEventListener('click', generateWorkflowDraftFromPrompt);
  }

  btnValidate.addEventListener('click', function () {
    var errors = validateWorkflow();
    if (errors.length === 0) {
      updateConfigErrorSummary([]);
      alert(tr('validWorkflow'));
    } else {
      showConfigErrors(errors);
      alert(tr('validationErrors', { errors: errors.join('\n') }));
    }
  });

  function validateWorkflow() {
    var errors = getInvalidConfigErrors();

    // Check for at least one trigger node
    var triggerNodes = state.nodes.filter(function (n) { return n.type === 'trigger'; });
    if (triggerNodes.length === 0) {
      errors.push(tr('requireOneTrigger'));
    } else if (triggerNodes.length > 1) {
      errors.push(tr('onlyOneTrigger', { count: triggerNodes.length }));
    }

    if (triggerNodes.length === 1) {
      var reachable = reachableNodeIds(triggerNodes[0].id);
      state.nodes.forEach(function (node) {
        if (!reachable[node.id]) {
          errors.push(tr('disconnectedNode', { label: node.label, x: Math.round(node.position.x), y: Math.round(node.position.y) }));
        }
      });
    }

    // Check trigger has no incoming edges
    triggerNodes.forEach(function (tn) {
      var hasIncoming = state.edges.some(function (e) { return e.target_id === tn.id; });
      if (hasIncoming) {
        errors.push(tr('triggerNoIncoming', { label: tn.label }));
      }
    });

    state.nodes.filter(function (node) { return node.type === 'terminal'; }).forEach(function (node) {
      var hasOutgoing = state.edges.some(function (edge) { return edge.source_id === node.id; });
      if (hasOutgoing) {
        errors.push(tr('terminalNoOutgoing', { label: node.label }));
      }
    });

    validateConditionBranchRoutes(errors);

    return errors;
  }

  function validateConditionBranchRoutes(errors) {
    var nodesById = {};
    state.nodes.forEach(function (node) { nodesById[node.id] = node; });
    state.nodes.filter(function (node) { return node.type === 'condition_branch'; }).forEach(function (node) {
      var config = node.config || {};
      var branches = Array.isArray(config.branches) ? config.branches : [];
      var hasRoute = false;
      branches.forEach(function (branch) {
        var targetId = String(branch && branch.target_node_id || '').trim();
        if (!targetId) return;
        hasRoute = true;
        if (!nodesById[targetId] || targetId === node.id || nodesById[targetId].type === 'trigger') {
          errors.push(tr('conditionBranchInvalidTarget', { label: node.label, target: targetId }));
        }
        var expr = branch && branch.expression || {};
        if (!String(expr.field || '').trim() || !isConditionBranchOperator(expr.operator)) {
          errors.push(tr('conditionBranchInvalidExpression', { label: node.label }));
        }
      });
      var defaultBranch = String(config.default_branch || '').trim();
      if (defaultBranch) {
        hasRoute = true;
        if (!nodesById[defaultBranch] || defaultBranch === node.id || nodesById[defaultBranch].type === 'trigger') {
          errors.push(tr('conditionBranchInvalidTarget', { label: node.label, target: defaultBranch }));
        }
      }
      if (!hasRoute) {
        errors.push(tr('conditionBranchNoRoute', { label: node.label }));
      }
    });
  }

  function isConditionBranchOperator(operator) {
    switch (String(operator || '').trim()) {
      case 'equals':
      case 'not_equals':
      case 'greater_than':
      case 'less_than':
      case 'contains':
      case 'in_list':
      case 'not_in_list':
      case 'is_empty':
      case 'is_not_empty':
        return true;
      default:
        return false;
    }
  }

  function reachableNodeIds(triggerId) {
    var reachable = {};
    var queue = [triggerId];
    reachable[triggerId] = true;
    while (queue.length > 0) {
      var current = queue.shift();
      state.edges.forEach(function (edge) {
        if (edge.source_id !== current || reachable[edge.target_id]) return;
        reachable[edge.target_id] = true;
        queue.push(edge.target_id);
      });
    }
    return reachable;
  }

  // --- Save ---
  btnSave.addEventListener('click', async function () {
    var errors = validateWorkflow();
    if (errors.length > 0) {
      showConfigErrors(errors);
      alert(tr('validationErrors', { errors: errors.join('\n') }));
      return;
    }
    setBusy(true, 'saving');
    try {
      var saved = await saveWorkflowDraft();
      await loadWorkflowLibrary();
      if (saved.clean) {
        alert(tr('saveDone', { version: formatVersion(saved.version.version_number) }));
      } else {
        alert(tr('saveChangedDuringSave'));
      }
    } catch (err) {
      updateVersionStatus(state.versionStatus);
      if (err && err.code === 'VALIDATION_FAILED') {
        alert(tr('submitBlocked', { errors: err.message || String(err) }));
      } else {
        alert(tr('requestFailed', { error: err && err.message || String(err) }));
      }
    } finally {
      setBusy(false);
      updateToolModeUI();
    }
  });

  function getInvalidConfigErrors() {
    var errors = Object.keys(state.invalidConfigFields).map(function (key) {
      return tr('invalidJsonField', { field: state.invalidConfigFields[key] || key });
    });
    configPanelBody.querySelectorAll('[aria-invalid="true"], .terminal-field-invalid, .config-field-invalid').forEach(function (el) {
      var label = configFieldLabel(el);
      var message = tr(el.classList.contains('config-field-invalid') ? 'invalidJsonField' : 'invalidConfigField', { field: label });
      if (errors.indexOf(message) === -1) errors.push(message);
    });
    return errors;
  }

  function showConfigErrors(errors) {
    updateConfigErrorSummary(errors);
    focusFirstInvalidConfigField();
  }

  function updateConfigErrorSummary(errors) {
    var summary = document.getElementById('configErrorSummary');
    if (!summary) return;
    if (!errors || errors.length === 0) {
      summary.hidden = true;
      summary.innerHTML = '';
      return;
    }
    summary.hidden = false;
    summary.innerHTML = '<strong>' + escapeHtml(tr('configErrorSummary')) + '</strong><ul>' + errors.map(function (error) {
      return '<li>' + escapeHtml(error) + '</li>';
    }).join('') + '</ul>';
  }

  function focusFirstInvalidConfigField() {
    if (!configPanel.classList.contains('visible')) return;
    var field = configPanelBody.querySelector('[aria-invalid="true"], .terminal-field-invalid, .config-field-invalid');
    if (field && typeof field.focus === 'function') {
      field.focus();
      field.scrollIntoView({ block: 'center', behavior: 'smooth' });
      return;
    }
    var summary = document.getElementById('configErrorSummary');
    if (summary && !summary.hidden && typeof summary.focus === 'function') summary.focus();
  }

  function configFieldLabel(el) {
    if (!el) return '';
    var wrap = el.closest('.config-field,.terminal-inline-field');
    var label = wrap ? wrap.querySelector('label') : null;
    return label && label.textContent ? label.textContent : (el.id || el.name || 'configuration');
  }

  // --- Submit for Review ---
  btnSubmit.addEventListener('click', async function () {
    var errors = validateWorkflow();
    if (errors.length > 0) {
      showConfigErrors(errors);
      alert(tr('submitBlocked', { errors: errors.join('\n') }));
      return;
    }
    setBusy(true, 'submitting');
    try {
      var saved = await saveWorkflowDraft();
      if (!saved.clean || hasUnsavedChanges()) {
        alert(tr('submitChangedDuringSave'));
        return;
      }
      var ver = saved.version;
      var data = await workflowApi('/api/v1/workflows/' + encodeURIComponent(state.workflowId) + '/versions/' + encodeURIComponent(ver.id) + '/submit', {
        method: 'POST',
        body: JSON.stringify({})
      });
      applyWorkflowVersion(data.version || ver);
      await loadWorkflowLibrary();
      alert(tr('submitDone'));
    } catch (err) {
      updateVersionStatus(state.versionStatus);
      alert(tr('requestFailed', { error: err && err.message || String(err) }));
    } finally {
      setBusy(false);
      updateToolModeUI();
    }
  });

  // --- Export graph model ---
  function getWorkflowGraph() {
    return {
      nodes: state.nodes.map(function (n) {
        return {
          id: n.id,
          type: n.type,
          label: n.label,
          position: { x: n.position.x, y: n.position.y },
          config: n.config,
        };
      }),
      edges: state.edges.map(function (e) {
        return {
          id: e.id,
          source_id: e.source_id,
          target_id: e.target_id,
          label: e.label || '',
          priority: e.priority || 0,
        };
      }),
    };
  }

  // --- Initialize ---
  syncWorkflowRouteState();
  setReadOnlyPreviewMode(state.isReadOnlyPreview);
  updateDocumentTitle();
  updateEmptyState();
  updateToolModeUI();
  updateVersionStatus('draft');
  if (state.isReadOnlyPreview) {
    loadWorkflowReviewPreview();
  } else {
    loadWorkflowLibrary();
    loadWorkflowFromApi();
  }

})();
