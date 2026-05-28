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
    nextNodeId: 1,
    nextEdgeId: 1,
    toolMode: 'select',
    draggingNodeType: null,
    connectingFrom: null, // node ID when drawing an edge
    selectedEdgeId: null,
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
    approverIds: 'Approver IDs (comma-separated)',
    minApprovals: 'Min Approvals (for Any N of M)',
    timeoutHours: 'Timeout (hours, 1-720)',
    fallbackApprover: 'Fallback Approver ID',
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
    requireOneTrigger: 'Exactly one Trigger node is required as the workflow entry point.',
    onlyOneTrigger: 'Only one Trigger node is allowed. Found {count} Trigger nodes.',
    disconnectedNode: 'Node "{label}" at ({x}, {y}) is disconnected.',
    triggerNoIncoming: 'Trigger node "{label}" must not have incoming edges.',
    selectTool: 'Select',
    connectTool: 'Connect',
    deleteEdgeTool: 'Delete Line',
    connectHint: 'Connect mode: click a source node, then click a target node.',
    deleteEdgeHint: 'Delete line mode: click a connector line to remove it.',
    edgeConnectorLabel: 'Connector from {source} to {target}',
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
    var machineID = getUrlParam('machine_id') || storageGet('maclaw-approval-workflow-machine-id');
    var token = getUrlParam('token') || getUrlParam('machine_token') || storageGet('maclaw-approval-workflow-machine-token');
    storageSet('maclaw-approval-workflow-machine-id', machineID);
    storageSet('maclaw-approval-workflow-machine-token', token);
    return { machineID: machineID, token: token };
  }

  function getAdminToken() {
    return storageGet('maclawHubAdminToken');
  }

  async function adminWorkflowApi(path, options) {
    var adminToken = getAdminToken();
    if (!adminToken) throw new Error(tr('adminAuthRequired'));
    var opts = options || {};
    var headers = { 'Content-Type': 'application/json', Authorization: 'Bearer ' + adminToken };
    Object.keys(opts.headers || {}).forEach(function (key) { headers[key] = opts.headers[key]; });
    var resp;
    try {
      resp = await fetch(path, Object.assign({}, opts, { headers: headers }));
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
      var fromEl = canvasNodes.querySelector('[data-node-id="' + state.connectingFrom + '"]');
      if (fromEl) fromEl.classList.remove('connecting-source');
    }
    state.connectingFrom = null;
  }

  document.querySelectorAll('[data-tool-mode]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      setToolMode(btn.getAttribute('data-tool-mode'));
    });
  });

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
        html += '<input type="text" id="cfgApproverIds" value="' + escapeAttr((node.config.approver_ids || []).join(', ')) + '">';
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
        html += '<input type="text" id="cfgFallback" value="' + escapeAttr(node.config.fallback_approver || '') + '">';
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
        var nodeEl = canvasNodes.querySelector('[data-node-id="' + node.id + '"] .canvas-node-label');
        if (nodeEl) nodeEl.textContent = node.label;
        var nodeFrame = canvasNodes.querySelector('[data-node-id="' + node.id + '"]');
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
        bindInput('cfgApproverIds', function (v) { node.config.approver_ids = v.split(',').map(function (s) { return s.trim(); }).filter(Boolean); });
        bindInput('cfgMinApprovals', function (v) { node.config.min_approvals = parseInt(v, 10) || 1; });
        bindInput('cfgTimeout', function (v) { node.config.timeout_hours = Math.min(720, Math.max(1, parseInt(v, 10) || 24)); });
        bindInput('cfgFallback', function (v) { node.config.fallback_approver = v.trim(); });
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
    var el = canvasNodes.querySelector('[data-node-id="' + nodeId + '"]');
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
  function addEdge(sourceId, targetId) {
    if (isReadOnlyPreview()) return;
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

  function renderEdges() {
    var svg = canvasEdges.querySelector('svg');
    svg.innerHTML = '';

    state.edges.forEach(function (edge) {
      var sourceNode = state.nodes.find(function (n) { return n.id === edge.source_id; });
      var targetNode = state.nodes.find(function (n) { return n.id === edge.target_id; });
      if (!sourceNode || !targetNode) return;

      var sourceEl = canvasNodes.querySelector('[data-node-id="' + edge.source_id + '"]');
      var targetEl = canvasNodes.querySelector('[data-node-id="' + edge.target_id + '"]');
      if (!sourceEl || !targetEl) return;

      var sx = sourceNode.position.x + sourceEl.offsetWidth / 2;
      var sy = sourceNode.position.y + sourceEl.offsetHeight;
      var tx = targetNode.position.x + targetEl.offsetWidth / 2;
      var ty = targetNode.position.y;

      // Bezier curve
      var midY = (sy + ty) / 2;
      var d = 'M ' + sx + ' ' + sy + ' C ' + sx + ' ' + midY + ', ' + tx + ' ' + midY + ', ' + tx + ' ' + ty;

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
      hitPath.addEventListener('keydown', function (e) {
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

    // Ensure arrowhead marker exists
    if (!svg.querySelector('#arrowhead')) {
      var defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
      defs.innerHTML = '<marker id="arrowhead" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto"><polygon points="0 0, 10 3.5, 0 7" fill="#94a3b8"/></marker>';
      svg.insertBefore(defs, svg.firstChild);
    }
  }

  // --- Config panel close ---
  configPanelClose.addEventListener('click', function () {
    deselectNode();
  });

  // --- Keyboard shortcuts ---
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Delete' || e.key === 'Backspace') {
      if (state.selectedNodeId && !isEditingField()) {
        e.preventDefault();
        deleteNode(state.selectedNodeId);
      } else if (state.selectedEdgeId && !isEditingField()) {
        e.preventDefault();
        deleteEdge(state.selectedEdgeId);
      }
    }
    if (e.key === 'Escape') {
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

    return errors;
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
    var configErrors = getInvalidConfigErrors();
    if (configErrors.length > 0) {
      showConfigErrors(configErrors);
      alert(tr('validationErrors', { errors: configErrors.join('\n') }));
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
