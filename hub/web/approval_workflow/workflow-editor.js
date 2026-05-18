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
    nextNodeId: 1,
    nextEdgeId: 1,
    draggingNodeType: null,
    connectingFrom: null, // node ID when drawing an edge
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
  const btnValidate = document.getElementById('btnValidate');
  const btnSave = document.getElementById('btnSave');
  const btnSubmit = document.getElementById('btnSubmit');

  // --- Node type metadata ---
  const NODE_TYPES = {
    trigger: { label: 'Trigger', icon: '\u25B6', color: '#e8f5e9', textColor: '#2e7d32' },
    form: { label: 'Form', icon: '\u270E', color: '#e3f2fd', textColor: '#1565c0' },
    approval: { label: 'Approval', icon: '\u2713', color: '#fff3e0', textColor: '#e65100' },
    condition_branch: { label: 'Condition Branch', icon: '\u2666', color: '#f3e5f5', textColor: '#6a1b9a' },
    action: { label: 'Action', icon: '\u2699', color: '#e0f2f1', textColor: '#00695c' },
    notification: { label: 'Notification', icon: '\u2709', color: '#fce4ec', textColor: '#c62828' },
    sub_process: { label: 'Sub-Process', icon: '\u21C4', color: '#ede7f6', textColor: '#4527a0' },
    terminal: { label: 'Terminal', icon: '\u25A0', color: '#efebe9', textColor: '#4e342e' },
  };

  // --- Utility ---
  function generateNodeId() {
    return 'node_' + (state.nextNodeId++);
  }

  function generateEdgeId() {
    return 'edge_' + (state.nextEdgeId++);
  }

  function updateEmptyState() {
    canvasEmpty.style.display = state.nodes.length === 0 ? 'block' : 'none';
  }

  // --- Drag and Drop from Palette ---
  const paletteNodes = document.querySelectorAll('.palette-node');
  paletteNodes.forEach(function (el) {
    el.addEventListener('dragstart', function (e) {
      state.draggingNodeType = el.getAttribute('data-node-type');
      e.dataTransfer.setData('text/plain', state.draggingNodeType);
      e.dataTransfer.effectAllowed = 'copy';
    });
    el.addEventListener('dragend', function () {
      state.draggingNodeType = null;
      dropIndicator.classList.remove('visible');
    });
  });

  canvasContainer.addEventListener('dragover', function (e) {
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
    e.preventDefault();
    dropIndicator.classList.remove('visible');
    const nodeType = e.dataTransfer.getData('text/plain');
    if (!nodeType || !NODE_TYPES[nodeType]) return;

    const rect = canvasArea.getBoundingClientRect();
    const x = e.clientX - rect.left - 80;
    const y = e.clientY - rect.top - 30;

    const node = {
      id: generateNodeId(),
      type: nodeType,
      label: NODE_TYPES[nodeType].label,
      position: { x: Math.max(0, x), y: Math.max(0, y) },
      config: getDefaultConfig(nodeType),
    };

    state.nodes.push(node);
    renderNode(node);
    updateEmptyState();

    // Select the newly placed node and show config panel within 500ms
    selectNode(node.id);
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
    el.style.left = node.position.x + 'px';
    el.style.top = node.position.y + 'px';
    el.innerHTML =
      '<div class="canvas-node-header">' +
        '<div class="palette-node-icon" style="width:24px;height:24px;border-radius:6px;font-size:12px;background:' + meta.color + ';color:' + meta.textColor + ';display:flex;align-items:center;justify-content:center;">' + meta.icon + '</div>' +
        '<span class="canvas-node-label">' + escapeHtml(node.label) + '</span>' +
      '</div>' +
      '<div class="canvas-node-type">' + escapeHtml(node.type.replace('_', ' ')) + '</div>';

    // Node click to select
    el.addEventListener('mousedown', function (e) {
      if (e.button !== 0) return;
      e.stopPropagation();
      selectNode(node.id);
      startDragNode(e, node, el);
    });

    // Double-click to start edge connection
    el.addEventListener('dblclick', function (e) {
      e.stopPropagation();
      if (state.connectingFrom === null) {
        state.connectingFrom = node.id;
        el.style.borderColor = '#e65100';
      } else if (state.connectingFrom !== node.id) {
        addEdge(state.connectingFrom, node.id);
        // Reset connecting state
        const fromEl = canvasNodes.querySelector('[data-node-id="' + state.connectingFrom + '"]');
        if (fromEl) fromEl.style.borderColor = '';
        state.connectingFrom = null;
      } else {
        // Cancel connection
        el.style.borderColor = '';
        state.connectingFrom = null;
      }
    });

    canvasNodes.appendChild(el);
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // --- Node dragging on canvas ---
  function startDragNode(e, node, el) {
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
    }

    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }

  // --- Node selection ---
  function selectNode(nodeId) {
    state.selectedNodeId = nodeId;
    // Update visual selection
    canvasNodes.querySelectorAll('.canvas-node').forEach(function (el) {
      el.classList.toggle('selected', el.getAttribute('data-node-id') === nodeId);
    });
    // Show config panel (requirement: within 500ms of node placement)
    showConfigPanel(nodeId);
  }

  function deselectNode() {
    state.selectedNodeId = null;
    canvasNodes.querySelectorAll('.canvas-node').forEach(function (el) {
      el.classList.remove('selected');
    });
    configPanel.classList.remove('visible');
  }

  // Click on canvas background to deselect
  canvasArea.addEventListener('mousedown', function (e) {
    if (e.target === canvasArea || e.target.classList.contains('canvas-grid') || e.target === canvasNodes) {
      deselectNode();
    }
  });

  // --- Config Panel ---
  function showConfigPanel(nodeId) {
    const node = state.nodes.find(function (n) { return n.id === nodeId; });
    if (!node) return;

    const meta = NODE_TYPES[node.type];
    configPanelTitle.textContent = meta.label + ' Configuration';
    configPanelBody.innerHTML = buildConfigForm(node);
    configPanel.classList.add('visible');

    // Attach change listeners
    attachConfigListeners(node);
  }

  function buildConfigForm(node) {
    var html = '';
    // Common: Label
    html += '<div class="config-field">';
    html += '<label>Label</label>';
    html += '<input type="text" id="cfgLabel" value="' + escapeAttr(node.label) + '">';
    html += '</div>';

    switch (node.type) {
      case 'trigger':
        html += '<div class="config-field">';
        html += '<label>Trigger Type</label>';
        html += '<select id="cfgTriggerType">';
        html += '<option value="manual"' + (node.config.trigger_type === 'manual' ? ' selected' : '') + '>Manual</option>';
        html += '<option value="api"' + (node.config.trigger_type === 'api' ? ' selected' : '') + '>API Call</option>';
        html += '<option value="schedule"' + (node.config.trigger_type === 'schedule' ? ' selected' : '') + '>Schedule</option>';
        html += '<option value="event"' + (node.config.trigger_type === 'event' ? ' selected' : '') + '>Event</option>';
        html += '</select>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Description</label>';
        html += '<textarea id="cfgDescription">' + escapeHtml(node.config.description || '') + '</textarea>';
        html += '</div>';
        break;

      case 'form':
        html += '<div class="config-field">';
        html += '<label>Description</label>';
        html += '<textarea id="cfgDescription">' + escapeHtml(node.config.description || '') + '</textarea>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Form Fields (JSON)</label>';
        html += '<textarea id="cfgFields">' + escapeHtml(JSON.stringify(node.config.fields || [], null, 2)) + '</textarea>';
        html += '</div>';
        break;

      case 'approval':
        html += '<div class="config-field">';
        html += '<label>Approval Mode</label>';
        html += '<select id="cfgMode">';
        html += '<option value="single"' + (node.config.mode === 'single' ? ' selected' : '') + '>Single Approver</option>';
        html += '<option value="countersign"' + (node.config.mode === 'countersign' ? ' selected' : '') + '>Countersign (All must approve)</option>';
        html += '<option value="any_n_of_m"' + (node.config.mode === 'any_n_of_m' ? ' selected' : '') + '>Any N of M</option>';
        html += '<option value="sequential"' + (node.config.mode === 'sequential' ? ' selected' : '') + '>Sequential</option>';
        html += '</select>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Approver IDs (comma-separated)</label>';
        html += '<input type="text" id="cfgApproverIds" value="' + escapeAttr((node.config.approver_ids || []).join(', ')) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Min Approvals (for Any N of M)</label>';
        html += '<input type="number" id="cfgMinApprovals" min="1" value="' + (node.config.min_approvals || 1) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Timeout (hours, 1-720)</label>';
        html += '<input type="number" id="cfgTimeout" min="1" max="720" value="' + (node.config.timeout_hours || 24) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Fallback Approver ID</label>';
        html += '<input type="text" id="cfgFallback" value="' + escapeAttr(node.config.fallback_approver || '') + '">';
        html += '</div>';
        break;

      case 'condition_branch':
        html += '<div class="config-field">';
        html += '<label>Branches (JSON)</label>';
        html += '<textarea id="cfgBranches">' + escapeHtml(JSON.stringify(node.config.branches || [], null, 2)) + '</textarea>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Default Branch (target node ID)</label>';
        html += '<input type="text" id="cfgDefaultBranch" value="' + escapeAttr(node.config.default_branch || '') + '">';
        html += '</div>';
        break;

      case 'action':
        html += '<div class="config-field">';
        html += '<label>Action Type</label>';
        html += '<select id="cfgActionType">';
        html += '<option value=""' + (!node.config.action_type ? ' selected' : '') + '>Select...</option>';
        html += '<option value="api_call"' + (node.config.action_type === 'api_call' ? ' selected' : '') + '>API Call</option>';
        html += '<option value="update_status"' + (node.config.action_type === 'update_status' ? ' selected' : '') + '>Update Status</option>';
        html += '<option value="webhook"' + (node.config.action_type === 'webhook' ? ' selected' : '') + '>Webhook</option>';
        html += '</select>';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Parameters (JSON)</label>';
        html += '<textarea id="cfgParameters">' + escapeHtml(JSON.stringify(node.config.parameters || {}, null, 2)) + '</textarea>';
        html += '</div>';
        break;

      case 'notification':
        html += '<div class="config-field">';
        html += '<label>Recipients (comma-separated)</label>';
        html += '<input type="text" id="cfgRecipients" value="' + escapeAttr((node.config.recipients || []).join(', ')) + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Message Template</label>';
        html += '<textarea id="cfgMessageTemplate">' + escapeHtml(node.config.message_template || '') + '</textarea>';
        html += '</div>';
        break;

      case 'sub_process':
        html += '<div class="config-field">';
        html += '<label>Workflow ID</label>';
        html += '<input type="text" id="cfgWorkflowId" value="' + escapeAttr(node.config.workflow_id || '') + '">';
        html += '</div>';
        html += '<div class="config-field">';
        html += '<label>Input Mapping (JSON)</label>';
        html += '<textarea id="cfgInputMapping">' + escapeHtml(JSON.stringify(node.config.input_mapping || {}, null, 2)) + '</textarea>';
        html += '</div>';
        break;

      case 'terminal':
        if (window.buildTerminalNodeConfigForm) {
          html += window.buildTerminalNodeConfigForm(node);
        } else {
          html += '<div class="config-field"><label>Result Executors (JSON)</label>';
          html += '<textarea id="cfgResultExecutors">' + escapeHtml(JSON.stringify(node.config.result_executors || [], null, 2)) + '</textarea></div>';
          html += '<div class="config-field"><label>Notifiers (JSON)</label>';
          html += '<textarea id="cfgNotifiers">' + escapeHtml(JSON.stringify(node.config.notifiers || [], null, 2)) + '</textarea></div>';
        }
        break;
    }

    // Delete button
    html += '<div style="margin-top:20px;padding-top:14px;border-top:1px solid var(--line);">';
    html += '<button id="cfgDeleteNode" style="width:100%;padding:9px;border-radius:8px;border:1px solid rgba(193,62,53,0.2);background:rgba(193,62,53,0.06);color:#c5221f;font-weight:700;font-size:13px;cursor:pointer;">Delete Node</button>';
    html += '</div>';

    return html;
  }

  function escapeAttr(str) {
    return String(str).replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function attachConfigListeners(node) {
    var labelInput = document.getElementById('cfgLabel');
    if (labelInput) {
      labelInput.addEventListener('input', function () {
        node.label = labelInput.value;
        var nodeEl = canvasNodes.querySelector('[data-node-id="' + node.id + '"] .canvas-node-label');
        if (nodeEl) nodeEl.textContent = node.label;
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
        bindTextarea('cfgFields', function (v) { try { node.config.fields = JSON.parse(v); } catch (e) {} });
        break;
      case 'approval':
        bindSelect('cfgMode', function (v) { node.config.mode = v; });
        bindInput('cfgApproverIds', function (v) { node.config.approver_ids = v.split(',').map(function (s) { return s.trim(); }).filter(Boolean); });
        bindInput('cfgMinApprovals', function (v) { node.config.min_approvals = parseInt(v) || 1; });
        bindInput('cfgTimeout', function (v) { node.config.timeout_hours = Math.min(720, Math.max(1, parseInt(v) || 24)); });
        bindInput('cfgFallback', function (v) { node.config.fallback_approver = v.trim(); });
        break;
      case 'condition_branch':
        bindTextarea('cfgBranches', function (v) { try { node.config.branches = JSON.parse(v); } catch (e) {} });
        bindInput('cfgDefaultBranch', function (v) { node.config.default_branch = v.trim(); });
        break;
      case 'action':
        bindSelect('cfgActionType', function (v) { node.config.action_type = v; });
        bindTextarea('cfgParameters', function (v) { try { node.config.parameters = JSON.parse(v); } catch (e) {} });
        break;
      case 'notification':
        bindInput('cfgRecipients', function (v) { node.config.recipients = v.split(',').map(function (s) { return s.trim(); }).filter(Boolean); });
        bindTextarea('cfgMessageTemplate', function (v) { node.config.message_template = v; });
        break;
      case 'sub_process':
        bindInput('cfgWorkflowId', function (v) { node.config.workflow_id = v.trim(); });
        bindTextarea('cfgInputMapping', function (v) { try { node.config.input_mapping = JSON.parse(v); } catch (e) {} });
        break;
      case 'terminal':
        if (window.attachTerminalNodeConfigListeners) {
          window.attachTerminalNodeConfigListeners(node);
        } else {
          bindTextarea('cfgResultExecutors', function (v) { try { node.config.result_executors = JSON.parse(v); } catch (e) {} });
          bindTextarea('cfgNotifiers', function (v) { try { node.config.notifiers = JSON.parse(v); } catch (e) {} });
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

  function bindInput(id, cb) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('input', function () { cb(el.value); });
  }
  function bindSelect(id, cb) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('change', function () { cb(el.value); });
  }
  function bindTextarea(id, cb) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('input', function () { cb(el.value); });
  }

  // --- Delete node ---
  function deleteNode(nodeId) {
    state.nodes = state.nodes.filter(function (n) { return n.id !== nodeId; });
    state.edges = state.edges.filter(function (e) { return e.source_id !== nodeId && e.target_id !== nodeId; });
    var el = canvasNodes.querySelector('[data-node-id="' + nodeId + '"]');
    if (el) el.remove();
    deselectNode();
    renderEdges();
    updateEmptyState();
  }

  // --- Edges ---
  function addEdge(sourceId, targetId) {
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
    renderEdges();
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

      // Arrow marker
      path.setAttribute('marker-end', 'url(#arrowhead)');

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
      if (state.selectedNodeId && document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'TEXTAREA' && document.activeElement.tagName !== 'SELECT') {
        deleteNode(state.selectedNodeId);
      }
    }
    if (e.key === 'Escape') {
      deselectNode();
      if (state.connectingFrom) {
        var fromEl = canvasNodes.querySelector('[data-node-id="' + state.connectingFrom + '"]');
        if (fromEl) fromEl.style.borderColor = '';
        state.connectingFrom = null;
      }
    }
  });

  // --- Validation ---
  btnValidate.addEventListener('click', function () {
    var errors = validateWorkflow();
    if (errors.length === 0) {
      alert('Workflow is valid!');
    } else {
      alert('Validation errors:\n\n' + errors.join('\n'));
    }
  });

  function validateWorkflow() {
    var errors = [];

    // Check for at least one trigger node
    var triggerNodes = state.nodes.filter(function (n) { return n.type === 'trigger'; });
    if (triggerNodes.length === 0) {
      errors.push('Exactly one Trigger node is required as the workflow entry point.');
    } else if (triggerNodes.length > 1) {
      errors.push('Only one Trigger node is allowed. Found ' + triggerNodes.length + ' Trigger nodes.');
    }

    // Check for disconnected nodes
    state.nodes.forEach(function (node) {
      if (node.type === 'trigger') return; // Trigger has no incoming
      var hasIncoming = state.edges.some(function (e) { return e.target_id === node.id; });
      var hasOutgoing = state.edges.some(function (e) { return e.source_id === node.id; });
      if (!hasIncoming && !hasOutgoing) {
        errors.push('Node "' + node.label + '" at (' + Math.round(node.position.x) + ', ' + Math.round(node.position.y) + ') is disconnected.');
      }
    });

    // Check trigger has no incoming edges
    triggerNodes.forEach(function (tn) {
      var hasIncoming = state.edges.some(function (e) { return e.target_id === tn.id; });
      if (hasIncoming) {
        errors.push('Trigger node "' + tn.label + '" must not have incoming edges.');
      }
    });

    return errors;
  }

  // --- Save ---
  btnSave.addEventListener('click', function () {
    var graph = getWorkflowGraph();
    console.log('Saving workflow graph:', JSON.stringify(graph, null, 2));
    // TODO: POST to /api/v1/workflows/:id/versions
    alert('Workflow saved (see console for graph data).');
  });

  // --- Submit for Review ---
  btnSubmit.addEventListener('click', function () {
    var errors = validateWorkflow();
    if (errors.length > 0) {
      alert('Cannot submit: please fix validation errors first.\n\n' + errors.join('\n'));
      return;
    }
    console.log('Submitting workflow for review:', JSON.stringify(getWorkflowGraph(), null, 2));
    // TODO: POST to /api/v1/workflows/:id/versions/:vid/submit
    alert('Workflow submitted for review.');
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
  updateEmptyState();

})();
