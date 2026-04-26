const app = () => window.go?.main?.App;

const registryPaging = {
  skills: { limit: 10, before: '', nextBefore: '', hasMore: false },
  mcp: { limit: 10, before: '', nextBefore: '', hasMore: false },
  adminTenants: { limit: 10, before: '', nextBefore: '', hasMore: false },
  adminUsers: { limit: 10, before: '', nextBefore: '', hasMore: false },
  adminCredentials: { limit: 10, before: '', nextBefore: '', hasMore: false },
};

const els = {
  bridgeStatus: document.getElementById('bridgeStatus'),
  baseUrlView: document.getElementById('baseUrlView'),
  operationBanner: document.getElementById('operationBanner'),
  operationTitle: document.getElementById('operationTitle'),
  operationDetail: document.getElementById('operationDetail'),
  baseUrl: document.getElementById('baseUrl'),
  timeoutSec: document.getElementById('timeoutSec'),
  adminSecret: document.getElementById('adminSecret'),
  apiKey: document.getElementById('apiKey'),
  apiSecret: document.getElementById('apiSecret'),
  accessToken: document.getElementById('accessToken'),
  skipTLSVerify: document.getElementById('skipTLSVerify'),
  authOutput: document.getElementById('authOutput'),
  tenantName: document.getElementById('tenantName'),
  tenantId: document.getElementById('tenantId'),
  userName: document.getElementById('userName'),
  userEmail: document.getElementById('userEmail'),
  userId: document.getElementById('userId'),
  credentialName: document.getElementById('credentialName'),
  newApiKey: document.getElementById('newApiKey'),
  newApiSecret: document.getElementById('newApiSecret'),
  adminOutput: document.getElementById('adminOutput'),
  adminAlertTenantId: document.getElementById('adminAlertTenantId'),
  adminAlertUserId: document.getElementById('adminAlertUserId'),
  adminAlertKind: document.getElementById('adminAlertKind'),
  adminAlertSince: document.getElementById('adminAlertSince'),
  adminAlertLimit: document.getElementById('adminAlertLimit'),
  adminAuditAction: document.getElementById('adminAuditAction'),
  adminAuditResourceType: document.getElementById('adminAuditResourceType'),
  adminAuditBefore: document.getElementById('adminAuditBefore'),
  adminAuditLimit: document.getElementById('adminAuditLimit'),
  adminTenantGrid: document.getElementById('adminTenantGrid'),
  adminUserGrid: document.getElementById('adminUserGrid'),
  adminCredentialGrid: document.getElementById('adminCredentialGrid'),
  adminTenantLimit: document.getElementById('adminTenantLimit'),
  adminTenantBefore: document.getElementById('adminTenantBefore'),
  adminUserLimit: document.getElementById('adminUserLimit'),
  adminUserBefore: document.getElementById('adminUserBefore'),
  adminCredentialLimit: document.getElementById('adminCredentialLimit'),
  adminCredentialBefore: document.getElementById('adminCredentialBefore'),
  tenantMaxInstances: document.getElementById('tenantMaxInstances'),
  tenantMaxSessions: document.getElementById('tenantMaxSessions'),
  tenantMaxMessages: document.getElementById('tenantMaxMessages'),
  tenantMaxRuns: document.getElementById('tenantMaxRuns'),
  userMaxInstances: document.getElementById('userMaxInstances'),
  userMaxSessions: document.getElementById('userMaxSessions'),
  userMaxMessages: document.getElementById('userMaxMessages'),
  userMaxRuns: document.getElementById('userMaxRuns'),
  adminOverviewGrid: document.getElementById('adminOverviewGrid'),
  adminDashboardGrid: document.getElementById('adminDashboardGrid'),
  adminAlertsGrid: document.getElementById('adminAlertsGrid'),
  adminAuditGrid: document.getElementById('adminAuditGrid'),
  configEditor: document.getElementById('configEditor'),
  configOutput: document.getElementById('configOutput'),
  instanceName: document.getElementById('instanceName'),
  instanceDesc: document.getElementById('instanceDesc'),
  instanceMetadata: document.getElementById('instanceMetadata'),
  instanceId: document.getElementById('instanceId'),
  includeArchivedSessions: document.getElementById('includeArchivedSessions'),
  sessionId: document.getElementById('sessionId'),
  runId: document.getElementById('runId'),
  messageRoleFilter: document.getElementById('messageRoleFilter'),
  messageSinceFilter: document.getElementById('messageSinceFilter'),
  messageUntilFilter: document.getElementById('messageUntilFilter'),
  runStatusFilter: document.getElementById('runStatusFilter'),
  runResponseSourceFilter: document.getElementById('runResponseSourceFilter'),
  runWaitingFilter: document.getElementById('runWaitingFilter'),
  messageTitle: document.getElementById('messageTitle'),
  messageInput: document.getElementById('messageInput'),
  instanceOutput: document.getElementById('instanceOutput'),
  capabilitySummary: document.getElementById('capabilitySummary'),
  capabilityPills: document.getElementById('capabilityPills'),
  capabilityGrid: document.getElementById('capabilityGrid'),
  instanceListGrid: document.getElementById('instanceListGrid'),
  instanceDetailGrid: document.getElementById('instanceDetailGrid'),
  instanceSummaryGrid: document.getElementById('instanceSummaryGrid'),
  sessionListGrid: document.getElementById('sessionListGrid'),
  runListGrid: document.getElementById('runListGrid'),
  messageTimelineGrid: document.getElementById('messageTimelineGrid'),
  runtimeFilterPills: document.getElementById('runtimeFilterPills'),
  apiExamplesGrid: document.getElementById('apiExamplesGrid'),
  mcpServerId: document.getElementById('mcpServerId'),
  mcpKind: document.getElementById('mcpKind'),
  mcpName: document.getElementById('mcpName'),
  mcpAuthType: document.getElementById('mcpAuthType'),
  mcpEndpointUrl: document.getElementById('mcpEndpointUrl'),
  mcpAuthSecret: document.getElementById('mcpAuthSecret'),
  mcpCommand: document.getElementById('mcpCommand'),
  mcpArgs: document.getElementById('mcpArgs'),
  mcpHeaders: document.getElementById('mcpHeaders'),
  mcpEnv: document.getElementById('mcpEnv'),
  mcpDisabled: document.getElementById('mcpDisabled'),
  mcpAutoStart: document.getElementById('mcpAutoStart'),
  mcpOutput: document.getElementById('mcpOutput'),
  mcpListGrid: document.getElementById('mcpListGrid'),
  mcpToolsGrid: document.getElementById('mcpToolsGrid'),
  mcpGuideGrid: document.getElementById('mcpGuideGrid'),
  mcpExamplesGrid: document.getElementById('mcpExamplesGrid'),
  skillName: document.getElementById('skillName'),
  skillSearchQuery: document.getElementById('skillSearchQuery'),
  skillSources: document.getElementById('skillSources'),
  skillTopN: document.getElementById('skillTopN'),
  skillHubUrl: document.getElementById('skillHubUrl'),
  skillMarketUrl: document.getElementById('skillMarketUrl'),
  skillMarketEmail: document.getElementById('skillMarketEmail'),
  skillInstallSource: document.getElementById('skillInstallSource'),
  skillRepoFullName: document.getElementById('skillRepoFullName'),
  skillRepoUrl: document.getElementById('skillRepoUrl'),
  skillRawUrl: document.getElementById('skillRawUrl'),
  skillFilePath: document.getElementById('skillFilePath'),
  skillBranch: document.getElementById('skillBranch'),
  skillDefinitionType: document.getElementById('skillDefinitionType'),
  skillID: document.getElementById('skillID'),
  skillGitHubToken: document.getElementById('skillGitHubToken'),
  skillSubmissionId: document.getElementById('skillSubmissionId'),
  skillArchiveName: document.getElementById('skillArchiveName'),
  skillZipBase64: document.getElementById('skillZipBase64'),
  skillOverwrite: document.getElementById('skillOverwrite'),
  includeInstalledSkills: document.getElementById('includeInstalledSkills'),
  skillGuideGrid: document.getElementById('skillGuideGrid'),
  skillSearchGrid: document.getElementById('skillSearchGrid'),
  skillListGrid: document.getElementById('skillListGrid'),
  skillExamplesGrid: document.getElementById('skillExamplesGrid'),
  skillOutput: document.getElementById('skillOutput'),
  playbookGrid: document.getElementById('playbookGrid'),
  demoFlowGrid: document.getElementById('demoFlowGrid'),
  responseSamplesGrid: document.getElementById('responseSamplesGrid'),
  integrationPackEditor: document.getElementById('integrationPackEditor'),
  integrationChecklistGrid: document.getElementById('integrationChecklistGrid'),
  quickGuideGrid: document.getElementById('quickGuideGrid'),
};

function hasBridge() {
  return typeof app() !== 'undefined';
}

function renderOutput(target, value, isError = false) {
  target.textContent = value || '';
  target.classList.toggle('error', Boolean(isError));
}

function setOperationStatus(state, title, detail) {
  if (!els.operationBanner || !els.operationTitle || !els.operationDetail) {
    return;
  }
  els.operationBanner.classList.remove('idle', 'running', 'success', 'error');
  els.operationBanner.classList.add(state || 'idle');
  els.operationTitle.textContent = title || 'Idle';
  els.operationDetail.textContent = detail || '';
}

function normalizePageEnvelope(parsed) {
  const source = parsed && typeof parsed === 'object' ? parsed : {};
  return {
    items: Array.isArray(source.items) ? source.items : (Array.isArray(parsed) ? parsed : []),
    limit: Number.isFinite(source.limit) ? source.limit : 0,
    hasMore: Boolean(source.has_more),
    nextBefore: typeof source.next_before === 'string' ? source.next_before : '',
  };
}

function renderPaginationCard(kind, meta, emptyLabel) {
  const label = emptyLabel || 'items';
  const detail = [`limit=${meta?.limit || 0}`, `has_more=${meta?.hasMore ? 'true' : 'false'}`];
  if (meta?.nextBefore) {
    detail.push(`next_before=${meta.nextBefore}`);
  }
  return [
    `<article class="api-card summary-card" data-${kind}-pager="true">`,
    '  <div class="snippet-head">',
    `    <h4>${escapeHTML(label)} pagination</h4>`,
    `    <span class="policy-pill neutral">${escapeHTML(meta?.hasMore ? 'more available' : 'end reached')}</span>`,
    '  </div>',
    `  <p>${escapeHTML(detail.join(' | '))}</p>`,
    '  <div class="mcp-server-actions">',
    `    <button class="ghost" type="button" data-${kind}-page-action="reset">First Page</button>`,
    meta?.hasMore ? `    <button class="ghost" type="button" data-${kind}-page-action="more">Load Older</button>` : '    <button class="ghost" type="button" disabled>No More</button>',
    '  </div>',
    '</article>',
  ].join('\\n');
}

function encodeCopyText(value) {
  return encodeURIComponent(String(value || ''));
}

function renderSnippetCard(title, body, snippet, extraClass = '') {
  const cls = extraClass ? ` ${extraClass}` : '';
  return [
    `<article class="api-card${cls}">`,
    '  <div class="snippet-head">',
    `    <h4>${escapeHTML(title)}</h4>`,
    `    <button class="ghost snippet-copy-btn" type="button" data-copy-text="${encodeCopyText(snippet)}">Copy</button>`,
    '  </div>',
    `  <p>${escapeHTML(body)}</p>`,
    `  <pre class="api-snippet">${escapeHTML(snippet)}</pre>`,
    '</article>',
  ].join('\n');
}

function renderChecklistCard(item) {
  const tone = item.status === 'OK' ? 'good' : (item.status === 'Warn' ? 'warn' : 'bad');
  const cls = item.status === 'OK' ? ' checklist-good' : (item.status === 'Warn' ? ' checklist-warn' : ' checklist-bad');
  return [
    `<article class="api-card checklist-card${cls}">`,
    '  <div class="snippet-head">',
    `    <h4>${escapeHTML(item.title)}</h4>`,
    `    <span class="policy-pill ${tone}">${escapeHTML(item.status)}</span>`,
    '  </div>',
    `  <p>${escapeHTML(item.body)}</p>`,
    `  <div class="cap-foot">${escapeHTML(item.footer || '')}</div>`,
    '</article>',
  ].join('\n');
}

function renderMetricCard(title, value, body, footer = '') {
  return [
    '<article class="api-card summary-card">',
    '  <div class="snippet-head">',
    `    <h4>${escapeHTML(title)}</h4>`,
    `    <span class="policy-pill neutral">${escapeHTML(String(value ?? '-'))}</span>`,
    '  </div>',
    `  <p>${escapeHTML(body || '')}</p>`,
    `  <div class="cap-foot">${escapeHTML(footer || '')}</div>`,
    '</article>',
  ].join('\n');
}

function setAdminOverviewEmpty(message) {
  if (!els.adminOverviewGrid) {
    return;
  }
  els.adminOverviewGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAdminDashboardEmpty(message) {
  if (!els.adminDashboardGrid) {
    return;
  }
  els.adminDashboardGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAdminAlertsEmpty(message) {
  if (!els.adminAlertsGrid) {
    return;
  }
  els.adminAlertsGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAdminAuditEmpty(message) {
  if (!els.adminAuditGrid) {
    return;
  }
  els.adminAuditGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAdminTenantListEmpty(message) {
  if (!els.adminTenantGrid) {
    return;
  }
  els.adminTenantGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAdminUserListEmpty(message) {
  if (!els.adminUserGrid) {
    return;
  }
  els.adminUserGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAdminCredentialListEmpty(message) {
  if (!els.adminCredentialGrid) {
    return;
  }
  els.adminCredentialGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function formatDateValue(value) {
  const text = (value || '').trim();
  return text || '-';
}

function parseOptionalPositiveInt(value) {
  const parsed = Number.parseInt((value || '').trim(), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function formatQuotaUsageItem(item) {
  if (!item) {
    return '-';
  }
  const used = item.used ?? 0;
  if (item.unlimited) {
    return `${used} / unlimited`;
  }
  const limit = item.limit ?? 0;
  const remaining = item.remaining == null ? '-' : item.remaining;
  return `${used} / ${limit} (remaining ${remaining})`;
}

function syncAdminIdentityFilters() {
  if (els.adminAlertTenantId && !els.adminAlertTenantId.value.trim() && els.tenantId?.value?.trim()) {
    els.adminAlertTenantId.value = els.tenantId.value.trim();
  }
  if (els.adminAlertUserId && !els.adminAlertUserId.value.trim() && els.userId?.value?.trim()) {
    els.adminAlertUserId.value = els.userId.value.trim();
  }
}

function applyAdminIdentityFilters(tenantID = '', userID = '') {
  if (els.adminAlertTenantId) {
    els.adminAlertTenantId.value = (tenantID || '').trim();
  }
  if (els.adminAlertUserId) {
    els.adminAlertUserId.value = (userID || '').trim();
  }
}

function renderAdminOverview(body) {
  if (!els.adminOverviewGrid) {
    return;
  }
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setAdminOverviewEmpty('Admin overview response was not valid JSON.');
    return;
  }
  const cards = [
    renderMetricCard('Tenants', parsed.tenants ?? 0, 'Total tenants managed by this MaClawSrv deployment.', `active=${parsed.active_tenants ?? 0} disabled=${parsed.disabled_tenants ?? 0}`),
    renderMetricCard('Users', parsed.users ?? 0, 'Total tenant users across the deployment.', `active=${parsed.active_users ?? 0} disabled=${parsed.disabled_users ?? 0}`),
    renderMetricCard('Instances', parsed.instances ?? 0, 'Logical runtime instances currently persisted.', `ready=${parsed.ready_instances ?? 0} stopped=${parsed.stopped_instances ?? 0}`),
    renderMetricCard('Sessions', parsed.sessions ?? 0, 'Conversation sessions across all tenants and users.', `messages=${parsed.messages ?? 0}`),
    renderMetricCard('Runs', parsed.runs ?? 0, 'Execution runs across all instances.', `statuses=${JSON.stringify(parsed.runs_by_status || {})}`),
    renderMetricCard('Audit Events', parsed.audit_events ?? 0, 'Control-plane and runtime audit records.', `last_audit_at=${formatDateValue(parsed.last_audit_at || '')}`),
  ];
  els.adminOverviewGrid.innerHTML = cards.join('');
}

function renderAdminDashboard(body) {
  if (!els.adminDashboardGrid) {
    return;
  }
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setAdminDashboardEmpty('Admin dashboard response was not valid JSON.');
    return;
  }
  const recentAuditEvents = Array.isArray(parsed.recent_audit_events) ? parsed.recent_audit_events : [];
  const last24Hours = Array.isArray(parsed.last_24_hours) ? parsed.last_24_hours : [];
  const last7Days = Array.isArray(parsed.last_7_days) ? parsed.last_7_days : [];
  const active24 = last24Hours.filter((item) => (item.messages || 0) > 0 || (item.runs || 0) > 0 || (item.audit_events || 0) > 0).length;
  const active7 = last7Days.filter((item) => (item.messages || 0) > 0 || (item.runs || 0) > 0 || (item.audit_events || 0) > 0).length;
  const cards = [
    renderMetricCard('Recent Audits', recentAuditEvents.length, 'Recent control-plane events available for dashboards.', recentAuditEvents[0] ? `${recentAuditEvents[0].action || '-'} @ ${formatDateValue(recentAuditEvents[0].created_at || '')}` : 'no recent audit event'),
    renderMetricCard('24h Buckets', last24Hours.length, 'Hourly trend buckets covering the latest 24 hours.', `active_buckets=${active24}`),
    renderMetricCard('7d Buckets', last7Days.length, 'Daily trend buckets covering the latest 7 days.', `active_buckets=${active7}`),
    renderMetricCard('Generated At', formatDateValue(parsed.generated_at || ''), 'Dashboard snapshot generation time.', `overview_runs=${parsed.overview?.runs ?? 0}`),
  ];
  els.adminDashboardGrid.innerHTML = cards.join('');
}

function renderAdminAlerts(body) {
  if (!els.adminAlertsGrid) {
    return;
  }
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setAdminAlertsEmpty('Admin alerts response was not valid JSON.');
    return;
  }
  const items = Array.isArray(parsed.items) ? parsed.items : [];
  if (!items.length) {
    setAdminAlertsEmpty('No alert items matched the current filter.');
    return;
  }
  els.adminAlertsGrid.innerHTML = items.map((item) => {
    const severity = (item.severity || 'low').trim().toLowerCase();
    return [
      `<article class="api-card admin-alert-card ${escapeHTML(severity)}">`,
      '  <div class="snippet-head">',
      `    <h4>${escapeHTML(item.title || item.kind || 'alert')}</h4>`,
      `    <span class="policy-pill ${severity === 'high' ? 'bad' : (severity === 'medium' ? 'warn' : 'neutral')}">${escapeHTML(severity || 'info')}</span>`,
      '  </div>',
      `  <p>${escapeHTML(item.reason || item.kind || 'No reason provided.')}</p>`,
      `  <div class="cap-foot">kind=${escapeHTML(item.kind || '-')} tenant=${escapeHTML(item.tenant_id || '-')} user=${escapeHTML(item.user_id || '-')} instance=${escapeHTML(item.instance_id || '-')} run=${escapeHTML(item.run_id || '-')} occurred_at=${escapeHTML(formatDateValue(item.occurred_at || ''))}</div>`,
      item.suggested_action ? `  <div class="cap-foot">suggested_action=${escapeHTML(item.suggested_action)}</div>` : '',
      '</article>',
    ].join('\n');
  }).join('');
}

function renderAuditEvents(body) {
  if (!els.adminAuditGrid) {
    return;
  }
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setAdminAuditEmpty('Audit events response was not valid JSON.');
    return;
  }
  const items = Array.isArray(parsed.items) ? parsed.items : [];
  if (!items.length) {
    setAdminAuditEmpty('No audit events matched the current filter.');
    return;
  }
  els.adminAuditGrid.innerHTML = items.map((item) => {
    const actor = [item.actor_type || '-', item.actor_tenant_id || '', item.actor_user_id || ''].filter(Boolean).join(' / ');
    return [
      '<article class="api-card audit-event-card">',
      '  <div class="snippet-head">',
      `    <h4>${escapeHTML(item.action || 'audit event')}</h4>`,
      `    <span class="policy-pill neutral">${escapeHTML(item.resource_type || 'resource')}</span>`,
      '  </div>',
      `  <p>${escapeHTML(item.resource_id || '-')}</p>`,
      `  <div class="cap-foot">tenant=${escapeHTML(item.tenant_id || '-')} user=${escapeHTML(item.user_id || '-')} actor=${escapeHTML(actor || '-')}</div>`,
      `  <div class="cap-foot">created_at=${escapeHTML(formatDateValue(item.created_at || ''))}</div>`,
      item.metadata ? `  <div class="cap-foot">metadata=${escapeHTML(JSON.stringify(item.metadata))}</div>` : '',
      '</article>',
    ].join('\n');
  }).join('');
}

function renderTenantSummary(body) {
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setAdminOverviewEmpty('Tenant summary response was not valid JSON.');
    setAdminDashboardEmpty('Tenant summary response was not valid JSON.');
    return;
  }
  if (!parsed || !parsed.tenant_id) {
    setAdminOverviewEmpty('Tenant summary did not include tenant metadata.');
    setAdminDashboardEmpty('Tenant summary did not include tenant metadata.');
    return;
  }
  const overviewCards = [
    renderMetricCard('Tenant', parsed.name || parsed.tenant_id, 'Selected tenant summary and aggregate state.', `id=${parsed.tenant_id} status=${parsed.status || '-'}`),
    renderMetricCard('Users', parsed.users ?? 0, 'Users currently under this tenant.', `active=${parsed.active_users ?? 0} disabled=${parsed.disabled_users ?? 0}`),
    renderMetricCard('Instances', parsed.instances ?? 0, 'Instances aggregated from all users in this tenant.', `ready=${parsed.ready_instances ?? 0} stopped=${parsed.stopped_instances ?? 0}`),
    renderMetricCard('Sessions', parsed.sessions ?? 0, 'Conversation sessions across all tenant users.', `messages=${parsed.messages ?? 0}`),
    renderMetricCard('Runs', parsed.runs ?? 0, 'Execution runs across all tenant users.', `statuses=${JSON.stringify(parsed.runs_by_status || {})}`),
    renderMetricCard('Quota Headroom', formatQuotaUsageItem(parsed.quota_usage?.instances), 'Tenant instance quota usage.', `sessions=${formatQuotaUsageItem(parsed.quota_usage?.sessions)} | messages=${formatQuotaUsageItem(parsed.quota_usage?.messages)} | runs=${formatQuotaUsageItem(parsed.quota_usage?.runs)}`),
  ];
  if (els.adminOverviewGrid) {
    els.adminOverviewGrid.innerHTML = overviewCards.join('');
  }
  const users = Array.isArray(parsed.user_summaries) ? parsed.user_summaries : [];
  if (!els.adminDashboardGrid) {
    return;
  }
  if (!users.length) {
    setAdminDashboardEmpty('No user summaries were returned for this tenant.');
    return;
  }
  els.adminDashboardGrid.innerHTML = users.map((item) => [
    `<article class="api-card summary-card runtime-record-card" data-admin-user-card="true" data-tenant-id="${escapeHTML(parsed.tenant_id || '')}" data-user-id="${escapeHTML(item.user_id || '')}">`,
    '  <div class="snippet-head">',
    `    <h4>${escapeHTML(item.name || item.user_id || 'user')}</h4>`,
    `    <span class="policy-pill ${item.status === 'disabled' ? 'bad' : 'good'}">${escapeHTML(item.status || 'active')}</span>`,
    '  </div>',
    `  <p>${escapeHTML(item.email || item.user_id || '-')}</p>`,
    `  <div class="cap-foot">instances=${escapeHTML(String(item.instances ?? 0))} ready=${escapeHTML(String(item.ready_instances ?? 0))} sessions=${escapeHTML(String(item.sessions ?? 0))} runs=${escapeHTML(String(item.runs ?? 0))}</div>`,
    `  <div class="cap-foot">instance_quota=${escapeHTML(formatQuotaUsageItem(item.quota_usage?.instances))}</div>`,
    `  <div class="cap-foot">session_quota=${escapeHTML(formatQuotaUsageItem(item.quota_usage?.sessions))}</div>`,
    `  <div class="cap-foot">message_quota=${escapeHTML(formatQuotaUsageItem(item.quota_usage?.messages))}</div>`,
    `  <div class="cap-foot">run_quota=${escapeHTML(formatQuotaUsageItem(item.quota_usage?.runs))}</div>`,
    '  <div class="mcp-server-actions">',
    '    <button class="ghost" type="button" data-admin-user-action="select">Select</button>',
    '    <button class="ghost" type="button" data-admin-user-action="alerts">Alerts</button>',
    '    <button class="ghost" type="button" data-admin-user-action="audit">Audit</button>',
    '  </div>',
    '</article>',
  ].join('\n')).join('');
}

function updateSelectedAdminTenantCard() {
  if (!els.adminTenantGrid) {
    return;
  }
  const selectedID = (els.tenantId.value || '').trim();
  els.adminTenantGrid.querySelectorAll('[data-admin-tenant-id]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-admin-tenant-id') === selectedID);
  });
}

function updateSelectedAdminUserCard() {
  if (!els.adminUserGrid) {
    return;
  }
  const selectedID = (els.userId.value || '').trim();
  els.adminUserGrid.querySelectorAll('[data-admin-user-id]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-admin-user-id') === selectedID);
  });
}

function updateSelectedAdminCredentialCard() {
  if (!els.adminCredentialGrid) {
    return;
  }
  const selectedKey = (els.newApiKey.value || '').trim();
  els.adminCredentialGrid.querySelectorAll('[data-admin-credential-key]').forEach((node) => {
    node.classList.toggle('selected', Boolean(selectedKey) && node.getAttribute('data-admin-credential-key') === selectedKey);
  });
}

function updateSelectedAdminDashboardUserCard() {
  if (!els.adminDashboardGrid) {
    return;
  }
  const selectedTenantID = (els.tenantId.value || '').trim();
  const selectedUserID = (els.userId.value || '').trim();
  els.adminDashboardGrid.querySelectorAll('[data-admin-user-card="true"]').forEach((node) => {
    const sameTenant = node.getAttribute('data-tenant-id') === selectedTenantID;
    const sameUser = node.getAttribute('data-user-id') === selectedUserID;
    node.classList.toggle('selected', sameTenant && sameUser);
  });
}

function renderAdminTenantList(items, meta = {}) {
  if (!els.adminTenantGrid) {
    return;
  }
  const tenants = Array.isArray(items) ? items : [];
  if (!tenants.length) {
    const emptyMessage = registryPaging.adminTenants.before
      ? 'No more tenants were found before the current cursor.'
      : 'No tenants were returned by the admin API.';
    setAdminTenantListEmpty(emptyMessage);
    return;
  }
  const selectedID = (els.tenantId.value || '').trim();
  const cards = tenants.map((item) => {
    const tenantID = item.id || '';
    const quota = item.quota || {};
    const selected = tenantID === selectedID ? ' selected' : '';
    return [
      `<article class="api-card summary-card runtime-record-card${selected}" data-admin-tenant-id="${escapeHTML(tenantID)}" data-admin-tenant-name="${escapeHTML(item.name || '')}" data-admin-tenant-max-instances="${escapeHTML(String(quota.max_instances ?? ''))}" data-admin-tenant-max-sessions="${escapeHTML(String(quota.max_sessions ?? ''))}" data-admin-tenant-max-messages="${escapeHTML(String(quota.max_messages ?? ''))}" data-admin-tenant-max-runs="${escapeHTML(String(quota.max_runs ?? ''))}">`,
      '  <div class="snippet-head">',
      `    <h4>${escapeHTML(item.name || tenantID || 'tenant')}</h4>`,
      `    <span class="policy-pill ${item.status === 'disabled' ? 'bad' : 'good'}">${escapeHTML(item.status || 'active')}</span>`,
      '  </div>',
      `  <p>${escapeHTML(tenantID || '-')}</p>`,
      `  <div class="cap-foot">instances=${escapeHTML(String(quota.max_instances ?? '-'))} sessions=${escapeHTML(String(quota.max_sessions ?? '-'))} messages=${escapeHTML(String(quota.max_messages ?? '-'))} runs=${escapeHTML(String(quota.max_runs ?? '-'))}</div>`,
      `  <div class="cap-foot">created_at=${escapeHTML(formatDateValue(item.created_at || ''))}</div>`,
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-admin-tenant-action="select">Select</button>',
      '    <button class="ghost" type="button" data-admin-tenant-action="users">Users</button>',
      '    <button class="ghost" type="button" data-admin-tenant-action="summary">Summary</button>',
      item.status === 'disabled'
        ? '    <button class="ghost" type="button" data-admin-tenant-action="enable">Enable</button>'
        : '    <button class="ghost" type="button" data-admin-tenant-action="disable">Disable</button>',
      '  </div>',
      '</article>',
    ].join('\n');
  });
  cards.push(renderPaginationCard('admin-tenant', meta, 'tenant registry'));
  els.adminTenantGrid.innerHTML = cards.join('');
  updateSelectedAdminTenantCard();
}

function renderAdminUserList(items, meta = {}) {
  if (!els.adminUserGrid) {
    return;
  }
  const users = Array.isArray(items) ? items : [];
  if (!users.length) {
    const emptyMessage = registryPaging.adminUsers.before
      ? 'No more users were found before the current cursor.'
      : 'No users were returned for the selected tenant.';
    setAdminUserListEmpty(emptyMessage);
    return;
  }
  const selectedID = (els.userId.value || '').trim();
  const cards = users.map((item) => {
    const userID = item.id || '';
    const selected = userID === selectedID ? ' selected' : '';
    return [
      `<article class="api-card summary-card runtime-record-card${selected}" data-admin-user-id="${escapeHTML(userID)}" data-admin-user-name="${escapeHTML(item.name || '')}" data-admin-user-email="${escapeHTML(item.email || '')}" data-admin-user-max-instances="${escapeHTML(String((item.quota || {}).max_instances ?? ''))}" data-admin-user-max-sessions="${escapeHTML(String((item.quota || {}).max_sessions ?? ''))}" data-admin-user-max-messages="${escapeHTML(String((item.quota || {}).max_messages ?? ''))}" data-admin-user-max-runs="${escapeHTML(String((item.quota || {}).max_runs ?? ''))}">`,
      '  <div class="snippet-head">',
      `    <h4>${escapeHTML(item.name || userID || 'user')}</h4>`,
      `    <span class="policy-pill ${item.status === 'disabled' ? 'bad' : 'good'}">${escapeHTML(item.status || 'active')}</span>`,
      '  </div>',
      `  <p>${escapeHTML(item.email || userID || '-')}</p>`,
      `  <div class="cap-foot">tenant=${escapeHTML(item.tenant_id || els.tenantId.value || '-')} created_at=${escapeHTML(formatDateValue(item.created_at || ''))}</div>`,
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-admin-user-list-action="select">Select</button>',
      '    <button class="ghost" type="button" data-admin-user-list-action="credentials">Credentials</button>',
      '    <button class="ghost" type="button" data-admin-user-list-action="alerts">Alerts</button>',
      item.status === 'disabled'
        ? '    <button class="ghost" type="button" data-admin-user-list-action="enable">Enable</button>'
        : '    <button class="ghost" type="button" data-admin-user-list-action="disable">Disable</button>',
      '  </div>',
      '</article>',
    ].join('\n');
  });
  cards.push(renderPaginationCard('admin-user', meta, 'user registry'));
  els.adminUserGrid.innerHTML = cards.join('');
  updateSelectedAdminUserCard();
}

function renderAdminCredentialList(items, meta = {}) {
  if (!els.adminCredentialGrid) {
    return;
  }
  const credentials = Array.isArray(items) ? items : [];
  if (!credentials.length) {
    const emptyMessage = registryPaging.adminCredentials.before
      ? 'No more credentials were found before the current cursor.'
      : 'No credentials were returned for the selected tenant user.';
    setAdminCredentialListEmpty(emptyMessage);
    return;
  }
  const selectedKey = (els.newApiKey.value || '').trim();
  const cards = credentials.map((item) => {
    const apiKey = item.api_key || item.apiKey || '';
    const selected = selectedKey && apiKey === selectedKey ? ' selected' : '';
    return [
      `<article class="api-card summary-card runtime-record-card${selected}" data-admin-credential-id="${escapeHTML(item.id || '')}" data-admin-credential-key="${escapeHTML(apiKey)}">`,
      '  <div class="snippet-head">',
      `    <h4>${escapeHTML(item.name || item.id || 'credential')}</h4>`,
      `    <span class="policy-pill ${item.status === 'revoked' ? 'bad' : 'good'}">${escapeHTML(item.status || 'active')}</span>`,
      '  </div>',
      `  <p>${escapeHTML(apiKey || item.api_key_prefix || '-')}</p>`,
      `  <div class="cap-foot">credential_id=${escapeHTML(item.id || '-')} created_at=${escapeHTML(formatDateValue(item.created_at || ''))}</div>`,
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-admin-credential-action="select">Select</button>',
      '    <button class="ghost" type="button" data-admin-credential-action="copy-key">Use Key</button>',
      item.status === 'revoked'
        ? '    <button class="ghost" type="button" disabled>Revoked</button>'
        : '    <button class="ghost" type="button" data-admin-credential-action="revoke">Revoke</button>',
      '  </div>',
      '</article>',
    ].join('\n');
  });
  cards.push(renderPaginationCard('admin-credential', meta, 'credential registry'));
  els.adminCredentialGrid.innerHTML = cards.join('');
  updateSelectedAdminCredentialCard();
}

async function refreshAdminTenants(options = {}) {
  if (options.reset) {
    registryPaging.adminTenants.before = '';
  } else if (Object.prototype.hasOwnProperty.call(options, 'before')) {
    registryPaging.adminTenants.before = options.before || '';
  }
  registryPaging.adminTenants.limit = parseOptionalPositiveInt(els.adminTenantLimit?.value || '') || 10;
  if (!Object.prototype.hasOwnProperty.call(options, 'before') && !options.reset) {
    registryPaging.adminTenants.before = els.adminTenantBefore?.value?.trim() || '';
  }
  if (els.adminTenantBefore) {
    els.adminTenantBefore.value = registryPaging.adminTenants.before;
  }
  const result = await safeCall(() => app().ListTenantsPage({
    limit: registryPaging.adminTenants.limit,
    before: registryPaging.adminTenants.before,
  }), els.adminOutput, (value) => value.body || '');
  try {
    const page = normalizePageEnvelope(JSON.parse(result.body || '{}'));
    registryPaging.adminTenants.hasMore = page.hasMore;
    registryPaging.adminTenants.nextBefore = page.nextBefore;
    renderAdminTenantList(page.items, page);
  } catch {
    setAdminTenantListEmpty('Tenant list response was not valid JSON.');
  }
  return result;
}

async function refreshAdminUsers(options = {}) {
  if (options.reset) {
    registryPaging.adminUsers.before = '';
  } else if (Object.prototype.hasOwnProperty.call(options, 'before')) {
    registryPaging.adminUsers.before = options.before || '';
  }
  registryPaging.adminUsers.limit = parseOptionalPositiveInt(els.adminUserLimit?.value || '') || 10;
  if (!Object.prototype.hasOwnProperty.call(options, 'before') && !options.reset) {
    registryPaging.adminUsers.before = els.adminUserBefore?.value?.trim() || '';
  }
  if (els.adminUserBefore) {
    els.adminUserBefore.value = registryPaging.adminUsers.before;
  }
  const result = await safeCall(() => app().ListUsersPage(els.tenantId.value.trim(), {
    limit: registryPaging.adminUsers.limit,
    before: registryPaging.adminUsers.before,
  }), els.adminOutput, (value) => value.body || '');
  try {
    const page = normalizePageEnvelope(JSON.parse(result.body || '{}'));
    registryPaging.adminUsers.hasMore = page.hasMore;
    registryPaging.adminUsers.nextBefore = page.nextBefore;
    renderAdminUserList(page.items, page);
  } catch {
    setAdminUserListEmpty('User list response was not valid JSON.');
  }
  return result;
}

async function refreshAdminCredentials(options = {}) {
  if (options.reset) {
    registryPaging.adminCredentials.before = '';
  } else if (Object.prototype.hasOwnProperty.call(options, 'before')) {
    registryPaging.adminCredentials.before = options.before || '';
  }
  registryPaging.adminCredentials.limit = parseOptionalPositiveInt(els.adminCredentialLimit?.value || '') || 10;
  if (!Object.prototype.hasOwnProperty.call(options, 'before') && !options.reset) {
    registryPaging.adminCredentials.before = els.adminCredentialBefore?.value?.trim() || '';
  }
  if (els.adminCredentialBefore) {
    els.adminCredentialBefore.value = registryPaging.adminCredentials.before;
  }
  const result = await safeCall(() => app().ListCredentialsPage(els.tenantId.value.trim(), els.userId.value.trim(), {
    limit: registryPaging.adminCredentials.limit,
    before: registryPaging.adminCredentials.before,
  }), els.adminOutput, (value) => value.body || '');
  try {
    const page = normalizePageEnvelope(JSON.parse(result.body || '{}'));
    registryPaging.adminCredentials.hasMore = page.hasMore;
    registryPaging.adminCredentials.nextBefore = page.nextBefore;
    renderAdminCredentialList(page.items, page);
  } catch {
    setAdminCredentialListEmpty('Credential list response was not valid JSON.');
  }
  return result;
}

function renderIntegrationChecklist() {
  if (!els.integrationChecklistGrid) {
    return;
  }
  const hasBaseURL = Boolean((els.baseUrl.value || '').trim());
  const hasAPIKey = Boolean((els.apiKey.value || '').trim());
  const hasAPISecret = Boolean((els.apiSecret.value || '').trim());
  const hasToken = Boolean((els.accessToken.value || '').trim());
  const hasConfigDraft = Boolean((els.configEditor.value || '').trim());
  const hasInstance = Boolean((els.instanceId.value || '').trim());
  const hasSession = Boolean((els.sessionId.value || '').trim());
  const hasPrompt = Boolean((els.messageInput.value || '').trim());
  const waitingFilter = (els.runWaitingFilter?.value || '').trim();
  const items = [
    {
      title: 'Connection Target',
      status: hasBaseURL ? 'OK' : 'Warn',
      body: hasBaseURL ? 'Base URL is configured for MaClawSrv.' : 'Base URL is empty; calls will fail until a target is configured.',
      footer: hasBaseURL ? els.baseUrl.value.trim() : 'example: http://127.0.0.1:18080',
    },
    {
      title: 'Credential Pair',
      status: hasAPIKey && hasAPISecret ? 'OK' : 'Warn',
      body: hasAPIKey && hasAPISecret ? 'api_key and api_secret are both present.' : 'Token exchange requires both api_key and api_secret.',
      footer: hasAPIKey && hasAPISecret ? 'ready for POST /api/v1/auth/token' : 'create credential first or fill both values',
    },
    {
      title: 'Bearer Token',
      status: hasToken ? 'OK' : 'Warn',
      body: hasToken ? 'Authenticated user-scoped APIs are available.' : 'Most config, skill, MCP, and runtime APIs require a bearer token.',
      footer: hasToken ? 'Authorization: Bearer <token>' : 'login required',
    },
    {
      title: 'Config Readiness',
      status: hasConfigDraft ? 'OK' : 'Warn',
      body: hasConfigDraft ? 'A config draft exists and can be validated or saved.' : 'Missing LLM config is the most common reason instance creation fails.',
      footer: hasConfigDraft ? 'run validate -> save before create instance' : 'check maclaw_llm_url / key / model',
    },
    {
      title: 'Runtime Context',
      status: hasInstance ? 'OK' : 'Warn',
      body: hasInstance ? 'An instance is selected for summary, session, run, and message queries.' : 'Most runtime endpoints need an instance_id first.',
      footer: hasInstance ? `instance=${els.instanceId.value.trim()}` : 'POST /api/v1/instances',
    },
    {
      title: 'Conversation Context',
      status: hasSession ? 'OK' : (hasPrompt && hasInstance ? 'Warn' : 'Warn'),
      body: hasSession ? 'A session exists, so message and run history can be queried.' : 'Sessions are created by the first message or via explicit session APIs.',
      footer: hasSession ? `session=${els.sessionId.value.trim()}` : 'send first message to create session/run',
    },
    {
      title: 'Common Failure Modes',
      status: waitingFilter === 'true' || !hasConfigDraft || !hasToken ? 'Warn' : 'OK',
      body: waitingFilter === 'true'
        ? 'Current filters are focused on waiting-for-user runs; empty run lists may be expected.'
        : !hasConfigDraft
          ? 'Invalid config is likely if runtime creation or resume returns 400.'
          : !hasToken
            ? '401 unauthorized is likely until login completes.'
            : 'No obvious local blocker is present in the current form state.',
      footer: waitingFilter === 'true' ? 'clear waiting filter if normal runs are expected' : 'watch for 400 invalid config / 401 unauthorized / 409 archived session',
    },
  ];
  els.integrationChecklistGrid.innerHTML = items.map(renderChecklistCard).join('');
}
function renderQuickGuide() {
  if (!els.quickGuideGrid) {
    return;
  }
  const hasAdminSecret = Boolean((els.adminSecret.value || '').trim());
  const hasAPIKey = Boolean((els.apiKey.value || '').trim());
  const hasToken = Boolean((els.accessToken.value || '').trim());
  const hasConfigDraft = Boolean((els.configEditor.value || '').trim());
  const hasInstance = Boolean((els.instanceId.value || '').trim());
  const hasSession = Boolean((els.sessionId.value || '').trim());
  const cards = [
    {
      title: '1. Authenticate',
      body: hasToken
        ? 'Access token already exists, so you can continue with config and runtime flows.'
        : hasAdminSecret && hasAPIKey
          ? 'API credentials are present. Exchange api_key + api_secret for a bearer token next.'
          : 'Start by preparing admin access or an existing API credential before logging in.',
      status: hasToken ? 'Done' : (hasAdminSecret && hasAPIKey ? 'In Progress' : 'Pending'),
      footer: hasToken ? 'login completed' : 'bootstrap -> credential -> login',
    },
    {
      title: '2. Validate Config',
      body: hasConfigDraft
        ? 'A config draft is ready. You can validate, test, and then save it.'
        : 'Load schema or paste a config draft for the current user LLM/runtime settings.',
      status: hasConfigDraft ? 'In Progress' : 'Pending',
      footer: 'validate before test/save',
    },
    {
      title: '3. Create Instance',
      body: hasInstance
        ? 'An instance ID is already present. You can inspect capabilities or send messages now.'
        : 'Create a runtime instance after config validation succeeds.',
      status: hasInstance ? 'Done' : 'Pending',
      footer: hasInstance ? `instance=${els.instanceId.value.trim()}` : 'instance ID will be filled after creation',
    },
    {
      title: '4. Start Conversation',
      body: hasSession
        ? 'A session ID already exists. You can continue polling messages and runs.'
        : hasInstance
          ? 'The instance is ready. Send the first message to create session/run records.'
          : 'Get an instance ID first, then start posting messages.',
      status: hasSession ? 'Done' : (hasInstance ? 'In Progress' : 'Pending'),
      footer: hasSession ? `session=${els.sessionId.value.trim()}` : 'first message fills session/run IDs',
    },
  ];
  els.quickGuideGrid.innerHTML = cards.map((card) => {
    const tone = card.status === 'Done' ? 'guide-good' : (card.status === 'In Progress' ? 'guide-warn' : 'guide-idle');
    const pill = card.status === 'Done' ? 'good' : (card.status === 'In Progress' ? 'warn' : 'neutral');
    return [
      `<article class="api-card quick-guide-card ${tone}">`,
      '  <div class="snippet-head">',
      `    <h4>${escapeHTML(card.title)}</h4>`,
      `    <span class="policy-pill ${pill}">${escapeHTML(card.status)}</span>`,
      '  </div>',
      `  <p>${escapeHTML(card.body)}</p>`,
      `  <div class="cap-foot">${escapeHTML(card.footer)}</div>`,
      '</article>',
    ].join('\n');
  }).join('');
}

function renderFlowCard(step) {
  const tone = step.status === 'Done' ? 'good' : (step.status === 'Ready' ? 'warn' : 'neutral');
  const cardClass = step.status === 'Done' ? ' flow-done' : (step.status === 'Ready' ? ' flow-ready' : ' flow-pending');
  const actionButton = step.action
    ? `<button class="ghost demo-flow-action" type="button" data-flow-action="${escapeHTML(step.action)}">${escapeHTML(step.actionLabel || 'Run')}</button>`
    : '';
  return [
    `<article class="api-card demo-flow-card${cardClass}">`,
    '  <div class="snippet-head">',
    `    <h4>${escapeHTML(step.title)}</h4>`,
    `    <span class="policy-pill ${tone}">${escapeHTML(step.status)}</span>`,
    '  </div>',
    `  <p>${escapeHTML(step.body)}</p>`,
    `  <div class="cap-foot">${escapeHTML(step.footer || '')}</div>`,
    actionButton ? `  <div class="demo-flow-actions">${actionButton}</div>` : '',
    '</article>',
  ].join('\n');
}

function renderDemoFlow() {
  if (!els.demoFlowGrid) {
    return;
  }
  const hasAPIKey = Boolean((els.apiKey.value || '').trim());
  const hasAPISecret = Boolean((els.apiSecret.value || '').trim());
  const hasToken = Boolean((els.accessToken.value || '').trim());
  const hasConfigDraft = Boolean((els.configEditor.value || '').trim());
  const hasInstance = Boolean((els.instanceId.value || '').trim());
  const hasSession = Boolean((els.sessionId.value || '').trim());
  const hasPrompt = Boolean((els.messageInput.value || '').trim());
  const cards = [
    {
      title: '1. Login',
      status: hasToken ? 'Done' : (hasAPIKey && hasAPISecret ? 'Ready' : 'Pending'),
      body: hasToken
        ? 'Bearer token already exists for the current API credential.'
        : hasAPIKey && hasAPISecret
          ? 'Credentials are present. Exchange them for a bearer token next.'
          : 'Prepare api_key and api_secret first.',
      footer: hasToken ? 'token ready for authenticated calls' : 'POST /api/v1/auth/token',
      action: hasToken ? '' : 'login',
      actionLabel: 'Run Login',
    },
    {
      title: '2. Load Config',
      status: hasConfigDraft ? 'Done' : (hasToken ? 'Ready' : 'Pending'),
      body: hasConfigDraft
        ? 'A user config draft is already loaded in the editor.'
        : hasToken
          ? 'Fetch current config or schema before validation.'
          : 'Login first so user-scoped config APIs are available.',
      footer: hasConfigDraft ? 'editor populated' : 'GET /api/v1/config',
      action: hasConfigDraft || !hasToken ? '' : 'load_config',
      actionLabel: 'Load Config',
    },
    {
      title: '3. Validate Config',
      status: hasConfigDraft ? 'Ready' : 'Pending',
      body: hasConfigDraft
        ? 'Validate the current config draft before saving or creating an instance.'
        : 'Load or paste a config draft first.',
      footer: 'POST /api/v1/config/validate',
      action: hasConfigDraft ? 'validate_config' : '',
      actionLabel: 'Validate',
    },
    {
      title: '4. Save Config',
      status: hasConfigDraft ? 'Ready' : 'Pending',
      body: hasConfigDraft
        ? 'Persist the current config once validation looks good.'
        : 'No config draft is available to save yet.',
      footer: 'PUT /api/v1/config',
      action: hasConfigDraft ? 'save_config' : '',
      actionLabel: 'Save Config',
    },
    {
      title: '5. Create Instance',
      status: hasInstance ? 'Done' : (hasToken ? 'Ready' : 'Pending'),
      body: hasInstance
        ? 'An instance is already selected and ready for runtime inspection.'
        : hasToken
          ? 'Create an instance for the current tenant/user after config is ready.'
          : 'Authenticate first before creating runtime instances.',
      footer: hasInstance ? `instance=${els.instanceId.value.trim()}` : 'POST /api/v1/instances',
      action: hasInstance || !hasToken ? '' : 'create_instance',
      actionLabel: 'Create Instance',
    },
    {
      title: '6. Send First Message',
      status: hasSession ? 'Done' : (hasInstance && hasPrompt ? 'Ready' : 'Pending'),
      body: hasSession
        ? 'A session already exists. Runtime panels can refresh runs, sessions, and messages.'
        : hasInstance && hasPrompt
          ? 'Send the first prompt to create session and run records.'
          : 'Create an instance and prepare a prompt before sending.',
      footer: hasSession ? `session=${els.sessionId.value.trim()}` : 'POST /api/v1/instances/{id}/messages',
      action: hasSession || !(hasInstance && hasPrompt) ? '' : 'send_message',
      actionLabel: 'Send Message',
    },
    {
      title: '7. Refresh Runtime',
      status: hasSession ? 'Ready' : 'Pending',
      body: hasSession
        ? 'Refresh instance, sessions, runs, and messages after each step to observe live state.'
        : 'A session is needed before runtime panels become meaningful.',
      footer: 'summary + sessions + runs + messages',
      action: hasSession ? 'refresh_runtime' : '',
      actionLabel: 'Refresh Panels',
    },
  ];
  els.demoFlowGrid.innerHTML = cards.map(renderFlowCard).join('');
}

async function runDemoFlowAction(action) {
  if (!action) {
    return;
  }
  if (action === 'login') {
    await login();
    return;
  }
  if (action === 'load_config') {
    await loadConfig();
    return;
  }
  if (action === 'validate_config') {
    await safeCall(() => app().ValidateConfig(els.configEditor.value), els.configOutput, (value) => value.body || '');
    return;
  }
  if (action === 'save_config') {
    const result = await safeCall(() => app().UpdateConfig(els.configEditor.value), els.configOutput, (value) => value.body || '');
    if (result?.body) {
      els.configEditor.value = result.body;
    }
    return;
  }
  if (action === 'create_instance') {
    const result = await safeCall(() => app().CreateInstance({
      name: els.instanceName.value.trim(),
      description: els.instanceDesc.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    syncInstanceIdFromBody(result.body || '');
    renderInstanceDetail(result.body || '');
    await refreshInstanceRegistry();
    if (els.instanceId.value.trim()) {
      await loadCapabilities();
      await loadInstanceSummary();
    }
    return;
  }
  if (action === 'send_message') {
    const result = await safeCall(() => app().SendMessage({
      instance_id: els.instanceId.value.trim(),
      session_id: els.sessionId.value.trim(),
      title: els.messageTitle.value.trim(),
      content: els.messageInput.value,
    }), els.instanceOutput, (value) => value.body || '');
    syncMessageResult(result.body || '');
    if (els.instanceId.value.trim()) {
      await refreshRuntimePanels();
    }
    return;
  }
  if (action === 'refresh_runtime') {
    await refreshRuntimePanels();
  }
}

async function refreshRuntimePanels() {
  if (!els.instanceId.value.trim()) {
    return;
  }
  await loadInstanceDetail(els.instanceId.value.trim());
  await loadCapabilities();
  await loadInstanceSummary();
  const sessions = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
  renderSessionList(sessions?.body || '');
  const runs = await safeCall(() => app().ListRuns({
    instance_id: els.instanceId.value.trim(),
    status: els.runStatusFilter.value.trim(),
    session_id: els.sessionId.value.trim(),
    response_source: els.runResponseSourceFilter.value.trim(),
    waiting_for_user: els.runWaitingFilter.value.trim(),
  }), els.instanceOutput, (value) => value.body || '');
  renderRunList(runs?.body || '');
  if (els.sessionId.value.trim()) {
    const messages = await safeCall(() => app().ListMessages({
      instance_id: els.instanceId.value.trim(),
      session_id: els.sessionId.value.trim(),
      role: els.messageRoleFilter.value.trim(),
      since: els.messageSinceFilter.value.trim(),
      until: els.messageUntilFilter.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    renderMessageTimeline(messages?.body || '');
  }
}
function prettyJSON(value) {
  return JSON.stringify(value, null, 2);
}

function renderResponseSamples() {
  if (!els.responseSamplesGrid) {
    return;
  }
  const hasToken = Boolean((els.accessToken.value || '').trim());
  const instanceId = (els.instanceId.value || '').trim() || 'inst_xxx';
  const sessionId = (els.sessionId.value || '').trim() || 'sess_xxx';
  const runId = (els.runId.value || '').trim() || 'run_xxx';
  const samples = [
    {
      title: 'Login Success',
      body: 'Typical bearer token exchange response from `/api/v1/auth/token`.',
      snippet: prettyJSON({
        access_token: '<jwt-token>',
        expires_at: '2026-04-25T10:30:00Z',
        principal: { tenant_id: 'tenant_xxx', user_id: 'user_xxx' },
        me: { tenant_id: 'tenant_xxx', user_id: 'user_xxx', credential_id: 'cred_xxx' },
      }),
      cls: 'response-sample-card good-sample',
    },
    {
      title: 'Instance Success',
      body: 'Representative instance payload returned by `POST /api/v1/instances` or `GET /api/v1/instances/{id}`.',
      snippet: prettyJSON({
        id: instanceId,
        tenant_id: 'tenant_xxx',
        user_id: 'user_xxx',
        name: 'demo-instance',
        status: 'ready',
        ready: hasToken,
        ready_reason: hasToken ? 'runtime initialized' : 'missing auth or config',
        workspace_dir: `~/.maclaw_srv/tenants/tenant_xxx/users/user_xxx/instances/${instanceId}/workspace`,
        metadata: { tier: 'demo' },
        created_at: '2026-04-25T10:00:00Z',
        updated_at: '2026-04-25T10:00:08Z',
      }),
      cls: 'response-sample-card good-sample',
    },
    {
      title: 'Message Success',
      body: 'Typical send-message response containing session, run, and message records.',
      snippet: prettyJSON({
        session: {
          id: sessionId,
          instance_id: instanceId,
          title: 'Demo session',
          waiting_for_user: false,
          archived: false,
        },
        run: {
          id: runId,
          instance_id: instanceId,
          session_id: sessionId,
          status: 'succeeded',
          response_source: 'assistant',
          waiting_for_user: false,
        },
        message: {
          id: 'msg_xxx',
          session_id: sessionId,
          role: 'assistant',
          content: 'Hello from MaClawSrv.',
        },
      }),
      cls: 'response-sample-card good-sample',
    },
    {
      title: 'Config Validation Error',
      body: 'Typical validation failure when required LLM settings are missing before instance creation.',
      snippet: prettyJSON({
        error: 'invalid config',
        config_validation: {
          valid: false,
          issues: [
            { field: 'maclaw_llm_url', message: 'is required' },
            { field: 'maclaw_llm_key', message: 'is required' },
            { field: 'maclaw_llm_model', message: 'is required' },
          ],
        },
      }),
      cls: 'response-sample-card bad-sample',
    },
    {
      title: 'Auth Error',
      body: 'Common unauthorized response when token is missing, expired, or malformed.',
      snippet: prettyJSON({
        error: 'unauthorized',
        message: 'missing or invalid bearer token',
        status_code: 401,
      }),
      cls: 'response-sample-card bad-sample',
    },
    {
      title: 'Session Conflict',
      body: 'Archived sessions reject new messages with a conflict response.',
      snippet: prettyJSON({
        error: 'session is archived',
        session_id: sessionId,
        status_code: 409,
      }),
      cls: 'response-sample-card bad-sample',
    },
  ];
  els.responseSamplesGrid.innerHTML = samples.map((item) => renderSnippetCard(item.title, item.body, item.snippet, item.cls)).join('');
}
function buildIntegrationPackMarkdown() {
  const baseURL = (els.baseUrl.value || '').trim() || 'http://127.0.0.1:18080';
  const token = (els.accessToken.value || '').trim() || '<access-token>';
  const apiKey = (els.apiKey.value || '').trim() || '<api-key>';
  const apiSecret = (els.apiSecret.value || '').trim() || '<api-secret>';
  const instanceId = (els.instanceId.value || '').trim() || 'inst_xxx';
  const sessionId = (els.sessionId.value || '').trim() || 'sess_xxx';
  const runId = (els.runId.value || '').trim() || 'run_xxx';
  const configDraft = (els.configEditor.value || '').trim() || '{"maclaw_llm_url":"https://api.openai.com/v1","maclaw_llm_key":"sk-xxx","maclaw_llm_model":"gpt-5.4"}';
  const messageTitle = (els.messageTitle.value || '').trim() || 'Demo session';
  const messageContent = (els.messageInput.value || '').trim() || 'Please introduce your capabilities and available tools.';
  const pack = [
    '# MaClawSrv Integration Pack',
    '',
    '## Target',
    `- Base URL: ${baseURL}`,
    `- Auth: Bearer token from /api/v1/auth/token`,
    `- Current instance: ${instanceId}`,
    `- Current session: ${sessionId}`,
    `- Current run: ${runId}`,
    '',
    '## Recommended Order',
    '1. Exchange api_key + api_secret for bearer token.',
    '2. Load, validate, and save user config.',
    '3. Create or select an instance.',
    '4. Send the first user message to create session/run records.',
    '5. Poll summary, sessions, runs, and messages.',
    '',
    '## Auth Request',
    '```bash',
    `curl -s ${JSON.stringify(baseURL + '/api/v1/auth/token')} \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -d ${JSON.stringify(JSON.stringify({ api_key: apiKey, api_secret: apiSecret }))}`,
    '```',
    '',
    '## Config Save',
    '```bash',
    `curl -s ${JSON.stringify(baseURL + '/api/v1/config')} \\`,
    `  -X PUT \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\`,
    `  -d ${JSON.stringify(configDraft)}`,
    '```',
    '',
    '## Create Instance',
    '```bash',
    `curl -s ${JSON.stringify(baseURL + '/api/v1/instances')} \\`,
    `  -X POST \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\`,
    `  -d ${JSON.stringify(JSON.stringify({ name: (els.instanceName.value || '').trim() || 'demo-instance', description: (els.instanceDesc.value || '').trim() || 'demo description' }))}`,
    '```',
    '',
    '## Send First Message',
    '```bash',
    `curl -s ${JSON.stringify(baseURL + `/api/v1/instances/${instanceId}/messages`)} \\`,
    `  -X POST \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\`,
    `  -d ${JSON.stringify(JSON.stringify({ session_id: sessionId, title: messageTitle, content: messageContent }))}`,
    '```',
    '',
    '## Read Models',
    '- `GET /api/v1/instances/{instanceId}/summary`',
    '- `GET /api/v1/instances/{instanceId}/sessions`',
    '- `GET /api/v1/instances/{instanceId}/runs`',
    '- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`',
    '',
    '## Typical Success Shapes',
    '```json',
    prettyJSON({
      access_token: '<jwt-token>',
      expires_at: '2026-04-25T10:30:00Z',
      principal: { tenant_id: 'tenant_xxx', user_id: 'user_xxx' },
    }),
    '```',
    '',
    '```json',
    prettyJSON({
      session: { id: sessionId, instance_id: instanceId, title: messageTitle },
      run: { id: runId, instance_id: instanceId, session_id: sessionId, status: 'succeeded' },
      message: { id: 'msg_xxx', session_id: sessionId, role: 'assistant', content: 'Hello from MaClawSrv.' },
    }),
    '```',
    '',
    '## Common Failures',
    '- `401 unauthorized`: bearer token missing, malformed, or expired.',
    '- `400 invalid config`: required LLM config is missing or invalid.',
    '- `409 session is archived`: restore the session before sending new messages.',
    '- Empty run/message lists may be caused by active filters such as `waiting_for_user=true`.',
  ];
  return pack.join('\n');
}

function renderIntegrationPack() {
  if (!els.integrationPackEditor) {
    return;
  }
  els.integrationPackEditor.value = buildIntegrationPackMarkdown();
}
function renderPlaybooks() {
  if (!els.playbookGrid) {
    return;
  }
  const baseURL = (els.baseUrl.value || '').trim() || 'http://127.0.0.1:18080';
  const token = (els.accessToken.value || '').trim() || '<access-token>';
  const adminSecret = (els.adminSecret.value || '').trim() ? '<configured-admin-secret>' : '<admin-secret>';
  const instanceId = (els.instanceId.value || '').trim() || 'inst_xxx';
  const sessionId = (els.sessionId.value || '').trim() || 'sess_xxx';
  const playbooks = [
    {
      title: 'Bootstrap tenant and user',
      body: 'Create a tenant, a user, and a credential so the demo can log in end to end.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/admin/tenants')} \
  -H ${JSON.stringify(`X-MaClaw-Admin-Secret: ${adminSecret}`)} \
  -H 'Content-Type: application/json' \
  -d '{"name":"Demo Tenant"}'

` +
        `curl -s ${JSON.stringify(baseURL + '/api/v1/admin/tenants/tenant_xxx/users')} \
  -H ${JSON.stringify(`X-MaClaw-Admin-Secret: ${adminSecret}`)} \
  -H 'Content-Type: application/json' \
  -d '{"name":"Demo User","email":"demo@example.com"}'`,
    },
    {
      title: 'Validate and save config',
      body: 'Validate first, then save the current user runtime and LLM settings.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/config/validate')} \
  -X POST \
  -H 'Content-Type: application/json' \
  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \
  -d '{"maclaw_llm_url":"https://api.openai.com/v1","maclaw_llm_key":"sk-xxx","maclaw_llm_model":"gpt-5.4"}'

` +
        `curl -s ${JSON.stringify(baseURL + '/api/v1/config')} \
  -X PUT \
  -H 'Content-Type: application/json' \
  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \
  -d '{"maclaw_llm_url":"https://api.openai.com/v1","maclaw_llm_key":"sk-xxx","maclaw_llm_model":"gpt-5.4"}'`,
    },
    {
      title: 'Create instance and send message',
      body: 'Create an instance and immediately post the first user message.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/instances')} \
  -X POST \
  -H 'Content-Type: application/json' \
  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \
  -d '{"name":"primary-agent","description":"demo"}'

` +
        `curl -s ${JSON.stringify(baseURL + `/api/v1/instances/${instanceId}/messages`)} \
  -X POST \
  -H 'Content-Type: application/json' \
  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \
  -d ${JSON.stringify(JSON.stringify({ session_id: sessionId, title: 'Demo session', content: 'Please introduce your capabilities.' }))}`,
    },
  ];
  els.playbookGrid.innerHTML = playbooks.map((item) => renderSnippetCard(item.title, item.body, item.snippet, 'playbook-card')).join('');
}

function currentSettingsPayload() {
  return {
    base_url: els.baseUrl.value.trim(),
    admin_secret: els.adminSecret.value.trim(),
    api_key: els.apiKey.value.trim(),
    api_secret: els.apiSecret.value.trim(),
    access_token: els.accessToken.value.trim(),
    skip_tls_verify: els.skipTLSVerify.checked,
    timeout_sec: Number(els.timeoutSec.value || '60'),
  };
}

function syncSettingsView(settings) {
  els.baseUrl.value = settings.base_url || '';
  els.timeoutSec.value = settings.timeout_sec || 60;
  els.adminSecret.value = settings.admin_secret || '';
  els.apiKey.value = settings.api_key || '';
  els.apiSecret.value = settings.api_secret || '';
  els.accessToken.value = settings.access_token || '';
  els.skipTLSVerify.checked = Boolean(settings.skip_tls_verify);
  els.baseUrlView.textContent = settings.base_url || '-';
}

function syncCredentialFieldsFromResult(text) {
  try {
    const parsed = JSON.parse(text);
    if (parsed.api_key) {
      els.apiKey.value = parsed.api_key;
      els.newApiKey.value = parsed.api_key;
    }
  } catch {
  }
}

function syncMessageResult(text) {
  try {
    const parsed = JSON.parse(text);
    if (parsed.session?.id) {
      els.sessionId.value = parsed.session.id;
    }
    if (parsed.run?.id) {
      els.runId.value = parsed.run.id;
    }
  } catch {
  }
  renderQuickGuide();
  renderIntegrationChecklist();
  renderDemoFlow();
  renderResponseSamples();
  renderIntegrationPack();
}

function syncInstanceIdFromBody(body) {
  try {
    const parsed = JSON.parse(body || '{}');
    if (parsed.id) {
      els.instanceId.value = parsed.id;
    }
  } catch {
  }
  renderQuickGuide();
  renderIntegrationChecklist();
  renderDemoFlow();
  renderResponseSamples();
  renderIntegrationPack();
}

function escapeHTML(value) {
  return String(value || '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function setCapabilityEmpty(message) {
  els.capabilitySummary.classList.add('empty');
  els.capabilityPills.innerHTML = '';
  els.capabilityGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setAPIExamplesEmpty(message) {
  if (!els.apiExamplesGrid) {
    return;
  }
  els.apiExamplesGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setInstanceListEmpty(message) {
  if (!els.instanceListGrid) {
    return;
  }
  els.instanceListGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setInstanceDetailEmpty(message) {
  if (!els.instanceDetailGrid) {
    return;
  }
  els.instanceDetailGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function updateSelectedInstanceCard() {
  if (!els.instanceListGrid) {
    return;
  }
  const selectedID = (els.instanceId.value || '').trim();
  els.instanceListGrid.querySelectorAll('[data-instance-id]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-instance-id') === selectedID);
  });
}

function renderInstanceRegistry(body) {
  let items;
  try {
    items = parseResponseItems(body);
  } catch {
    setInstanceListEmpty('Instance list response was not valid JSON.');
    return;
  }
  if (!items.length) {
    setInstanceListEmpty('No instances found for the current user.');
    return;
  }
  const selectedID = (els.instanceId.value || '').trim();
  els.instanceListGrid.innerHTML = items.map((item) => {
    const id = item.id || item.ID || '';
    const ready = Boolean(item.ready || item.readiness?.ready);
    const status = String(item.status || 'unknown');
    const selected = id && id === selectedID ? ' selected' : '';
    const toneClass = !ready ? ' state-waiting' : (status === 'ready' ? ' state-good' : '');
    const badges = [
      `<span class="policy-pill ${ready ? 'good' : 'warn'}">${escapeHTML(ready ? 'ready' : 'not ready')}</span>`,
      `<span class="policy-pill neutral">${escapeHTML(status)}</span>`,
    ];
    if (item.user_id) {
      badges.push(`<span class="policy-pill neutral">user ${escapeHTML(item.user_id)}</span>`);
    }
    return [
      `<article class="api-card runtime-record-card instance-list-card${toneClass}${selected}" data-instance-id="${escapeHTML(id)}">`,
      '  <div class="runtime-record-head">',
      `    <h4>${escapeHTML(item.name || id || 'instance')}</h4>`,
      `    <span class="runtime-record-id">${escapeHTML(id || 'instance')}</span>`,
      '  </div>',
      `  <div class="mcp-server-badges">${badges.join('')}</div>`,
      `  <p>${escapeHTML(item.description || item.ready_reason || 'No description provided.')}</p>`,
      '  <div class="runtime-meta-grid">',
      `    <span><strong>Created</strong>${escapeHTML(formatDateTime(item.created_at))}</span>`,
      `    <span><strong>Updated</strong>${escapeHTML(formatDateTime(item.updated_at))}</span>`,
      `    <span><strong>Workspace</strong>${escapeHTML(item.workspace_dir || item.workspace || '-')}</span>`,
      '  </div>',
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-instance-action="select">Select</button>',
      '    <button class="ghost" type="button" data-instance-action="summary">Summary</button>',
      '    <button class="ghost" type="button" data-instance-action="capabilities">Capabilities</button>',
      '  </div>',
      '</article>',
    ].join('\n');
  }).join('');
  updateSelectedInstanceCard();
}

function renderInstanceDetail(body) {
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setInstanceDetailEmpty('Instance detail response was not valid JSON.');
    return;
  }
  if (!parsed || !parsed.id) {
    setInstanceDetailEmpty('Select an instance to inspect readiness, workspace, and metadata.');
    return;
  }
  const cards = [
    { title: 'Identity', body: `${parsed.name || parsed.id}`, footer: `id=${parsed.id}` },
    { title: 'Status', body: `${parsed.status || '-'} / ready=${String(Boolean(parsed.ready))}`, footer: parsed.ready_reason || 'no readiness reason' },
    { title: 'Workspace', body: parsed.workspace_dir || parsed.workspace || '-', footer: parsed.runtime_dir || parsed.data_dir || 'runtime path unavailable' },
    { title: 'Readiness', body: parsed.readiness?.reason || parsed.ready_reason || '-', footer: `config_valid=${String(Boolean(parsed.readiness?.config_valid))} llm=${String(Boolean(parsed.readiness?.has_llm_config))}` },
  ];
  if (parsed.description) {
    cards.push({ title: 'Description', body: parsed.description, footer: parsed.user_id ? `user=${parsed.user_id}` : 'instance description' });
  }
  const metadata = parsed.metadata || {};
  const metadataKeys = Object.keys(metadata);
  cards.push({ title: 'Metadata', body: metadataKeys.length ? metadataKeys.join(', ') : 'none', footer: metadataKeys.length ? JSON.stringify(metadata) : 'no metadata' });
  els.instanceDetailGrid.innerHTML = cards.map((card) => [
    '<article class="api-card summary-card">',
    `  <h4>${escapeHTML(card.title)}</h4>`,
    `  <p>${escapeHTML(card.body)}</p>`,
    `  <div class="cap-foot">${escapeHTML(card.footer)}</div>`,
    '</article>',
  ].join('\n')).join('');
}

async function refreshInstanceRegistry() {
  const result = await safeCall(() => app().ListInstances(), els.instanceOutput, (value) => value.body || '');
  renderInstanceRegistry(result?.body || '');
  return result;
}

async function loadInstanceDetail(instanceID) {
  const id = (instanceID || els.instanceId.value || '').trim();
  if (!id) {
    setInstanceDetailEmpty('Select an instance to inspect readiness, workspace, and metadata.');
    return null;
  }
  const result = await safeCall(() => app().GetInstance(id), els.instanceOutput, (value) => value.body || '');
  syncInstanceIdFromBody(result?.body || '');
  renderInstanceDetail(result?.body || '');
  updateSelectedInstanceCard();
  return result;
}
function setInstanceSummaryEmpty(message) {
  if (!els.instanceSummaryGrid) {
    return;
  }
  els.instanceSummaryGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setSessionListEmpty(message) {
  if (!els.sessionListGrid) {
    return;
  }
  els.sessionListGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setRunListEmpty(message) {
  if (!els.runListGrid) {
    return;
  }
  els.runListGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function parseResponseItems(body) {
  const parsed = JSON.parse(body || '[]');
  if (Array.isArray(parsed)) {
    return parsed;
  }
  if (Array.isArray(parsed.items)) {
    return parsed.items;
  }
  if (Array.isArray(parsed.sessions)) {
    return parsed.sessions;
  }
  if (Array.isArray(parsed.runs)) {
    return parsed.runs;
  }
  return [];
}

function formatDateTime(value) {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return String(value);
  }
  return date.toLocaleString();
}

function updateSelectedSessionCard() {
  if (!els.sessionListGrid) {
    return;
  }
  const selectedID = (els.sessionId.value || '').trim();
  els.sessionListGrid.querySelectorAll('[data-session-id]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-session-id') === selectedID);
  });
}

function updateSelectedRunCard() {
  if (!els.runListGrid) {
    return;
  }
  const selectedID = (els.runId.value || '').trim();
  els.runListGrid.querySelectorAll('[data-run-id]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-run-id') === selectedID);
  });
}

function renderSessionList(body) {
  let items;
  try {
    items = parseResponseItems(body);
  } catch {
    setSessionListEmpty('Session list response was not valid JSON.');
    return;
  }
  if (!items.length) {
    setSessionListEmpty('No sessions found for the current instance and filters.');
    return;
  }
  const selectedID = (els.sessionId.value || '').trim();
  els.sessionListGrid.innerHTML = items.map((item) => {
    const id = item.id || item.ID || item.session_id || '';
    const title = item.title || item.name || 'Untitled session';
    const archived = Boolean(item.archived);
    const waiting = Boolean(item.waiting_for_user);
    const toneClass = archived ? ' state-archived' : (waiting ? ' state-waiting' : '');
    const selected = id && id === selectedID ? ' selected' : '';
    const badges = [];
    badges.push(`<span class="policy-pill ${archived ? 'bad' : 'neutral'}">${escapeHTML(archived ? 'Archived' : 'Active')}</span>`);
    if (waiting) {
      badges.push('<span class="policy-pill warn">Waiting User</span>');
    }
    if (item.agent_id) {
      badges.push(`<span class="policy-pill neutral">agent ${escapeHTML(item.agent_id)}</span>`);
    }
    return [
      `<article class="api-card runtime-record-card${toneClass}${selected}" data-session-id="${escapeHTML(id)}">`,
      '  <div class="runtime-record-head">',
      `    <h4>${escapeHTML(title)}</h4>`,
      `    <span class="runtime-record-id">${escapeHTML(id || 'session')}</span>`,
      '  </div>',
      badges.length ? `  <div class="mcp-server-badges">${badges.join('')}</div>` : '',
      `  <p>${escapeHTML(waiting ? 'This session is waiting for user input.' : 'Conversation history is available for inspection and continuation.')}</p>`,
      '  <div class="runtime-meta-grid">',
      `    <span><strong>Last Message</strong>${escapeHTML(formatDateTime(item.last_message_at || item.updated_at || item.created_at))}</span>`,
      `    <span><strong>Created</strong>${escapeHTML(formatDateTime(item.created_at))}</span>`,
      `    <span><strong>Metadata</strong>${escapeHTML(item.metadata ? 'present' : 'none')}</span>`,
      '  </div>',
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-session-action="select">Select</button>',
      `    <button class="ghost" type="button" data-session-action="${archived ? 'restore' : 'archive'}">${archived ? 'Restore' : 'Archive'}</button>`,
      '  </div>',
      '</article>',
    ].join('\n');
  }).join('');
  updateSelectedSessionCard();
}

function runStatusTone(status, waiting) {
  if (waiting) {
    return 'warn';
  }
  if (status === 'succeeded' || status === 'completed') {
    return 'good';
  }
  if (status === 'failed' || status === 'cancelled' || status === 'canceled') {
    return 'bad';
  }
  return 'neutral';
}

function renderRunList(body) {
  let items;
  try {
    items = parseResponseItems(body);
  } catch {
    setRunListEmpty('Run list response was not valid JSON.');
    return;
  }
  if (!items.length) {
    setRunListEmpty('No runs found for the current instance and filters.');
    return;
  }
  const selectedID = (els.runId.value || '').trim();
  els.runListGrid.innerHTML = items.map((item) => {
    const id = item.id || item.ID || item.run_id || '';
    const status = String(item.status || 'unknown');
    const waiting = Boolean(item.waiting_for_user);
    const responseSource = item.response_source || '-';
    const selected = id && id === selectedID ? ' selected' : '';
    const tone = runStatusTone(status, waiting);
    const toneClass = waiting ? ' state-waiting' : (tone === 'bad' ? ' state-error' : (tone === 'good' ? ' state-good' : ''));
    const badges = [
      `<span class="policy-pill ${tone}">${escapeHTML(status)}</span>`,
      `<span class="policy-pill neutral">${escapeHTML(responseSource)}</span>`,
    ];
    if (waiting) {
      badges.push('<span class="policy-pill warn">Waiting User</span>');
    }
    if (item.session_id) {
      badges.push(`<span class="policy-pill neutral">session ${escapeHTML(item.session_id)}</span>`);
    }
    return [
      `<article class="api-card runtime-record-card${toneClass}${selected}" data-run-id="${escapeHTML(id)}" data-session-id="${escapeHTML(item.session_id || '')}">`,
      '  <div class="runtime-record-head">',
      `    <h4>${escapeHTML(waiting ? 'Waiting for user response' : 'Run execution')}</h4>`,
      `    <span class="runtime-record-id">${escapeHTML(id || 'run')}</span>`,
      '  </div>',
      `  <div class="mcp-server-badges">${badges.join('')}</div>`,
      `  <p>${escapeHTML(waiting ? 'The agent paused and expects the next user action.' : `Response source: ${responseSource}`)}</p>`,
      '  <div class="runtime-meta-grid">',
      `    <span><strong>Started</strong>${escapeHTML(formatDateTime(item.started_at || item.created_at))}</span>`,
      `    <span><strong>Completed</strong>${escapeHTML(formatDateTime(item.completed_at || item.updated_at))}</span>`,
      `    <span><strong>Session</strong>${escapeHTML(item.session_id || '-')}</span>`,
      '  </div>',
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-run-action="select">Select</button>',
      '    <button class="ghost" type="button" data-run-action="get">Details</button>',
      '  </div>',
      '</article>',
    ].join('\n');
  }).join('');
  updateSelectedRunCard();
}
function setMessageTimelineEmpty(message) {
  if (!els.messageTimelineGrid) {
    return;
  }
  els.messageTimelineGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function summarizeMessageContent(item) {
  const raw = item.content || item.text || item.body || item.message || item.output_text || item.input_text || '';
  if (typeof raw === 'string') {
    return raw;
  }
  try {
    return JSON.stringify(raw, null, 2);
  } catch {
    return String(raw);
  }
}

function currentRuntimeFilterPills() {
  const pills = [];
  const role = (els.messageRoleFilter?.value || '').trim();
  const since = (els.messageSinceFilter?.value || '').trim();
  const until = (els.messageUntilFilter?.value || '').trim();
  const status = (els.runStatusFilter?.value || '').trim();
  const source = (els.runResponseSourceFilter?.value || '').trim();
  const waiting = (els.runWaitingFilter?.value || '').trim();
  if (role) pills.push({ tone: 'neutral', label: `role=${role}` });
  if (since) pills.push({ tone: 'neutral', label: `since=${since}` });
  if (until) pills.push({ tone: 'neutral', label: `until=${until}` });
  if (status) pills.push({ tone: 'neutral', label: `run=${status}` });
  if (source) pills.push({ tone: 'neutral', label: `source=${source}` });
  if (waiting) pills.push({ tone: waiting === 'true' ? 'warn' : 'neutral', label: `waiting=${waiting}` });
  if (els.includeArchivedSessions?.checked) pills.push({ tone: 'warn', label: 'archived sessions included' });
  return pills;
}

function renderRuntimeFilterPills() {
  if (!els.runtimeFilterPills) {
    return;
  }
  const pills = currentRuntimeFilterPills();
  if (!pills.length) {
    els.runtimeFilterPills.innerHTML = '<span class="policy-pill neutral">no active filters</span>';
    return;
  }
  els.runtimeFilterPills.innerHTML = pills.map((pill) => `<span class="policy-pill ${pill.tone}">${escapeHTML(pill.label)}</span>`).join('');
}

function renderMessageTimeline(body) {
  let items;
  try {
    items = parseResponseItems(body);
  } catch {
    setMessageTimelineEmpty('Message list response was not valid JSON.');
    return;
  }
  if (!items.length) {
    setMessageTimelineEmpty('No messages found for the current session and filters.');
    return;
  }
  els.messageTimelineGrid.innerHTML = items.map((item) => {
    const role = String(item.role || item.type || 'message').toLowerCase();
    const tone = role === 'assistant' ? 'good' : (role === 'user' ? 'neutral' : (role === 'system' ? 'warn' : 'bad'));
    const content = summarizeMessageContent(item);
    const preview = content.length > 800 ? `${content.slice(0, 800)}...` : content;
    const createdAt = formatDateTime(item.created_at || item.timestamp || item.updated_at);
    const messageID = item.id || item.ID || item.message_id || '-';
    const sessionID = item.session_id || item.sessionId || '';
    return [
      `<article class="message-card message-${escapeHTML(role)}" data-message-id="${escapeHTML(messageID)}" data-session-id="${escapeHTML(sessionID)}">`,
      '  <div class="runtime-record-head">',
      `    <h4>${escapeHTML(role)}</h4>`,
      `    <span class="runtime-record-id">${escapeHTML(messageID)}</span>`,
      '  </div>',
      `  <div class="mcp-server-badges"><span class="policy-pill ${tone}">${escapeHTML(role)}</span>${sessionID ? `<span class="policy-pill neutral">session ${escapeHTML(sessionID)}</span>` : ''}</div>`,
      `  <pre class="message-body">${escapeHTML(preview || '(empty message)')}</pre>`,
      '  <div class="runtime-meta-grid">',
      `    <span><strong>Created</strong>${escapeHTML(createdAt)}</span>`,
      `    <span><strong>Length</strong>${escapeHTML(String(content.length))} chars</span>`,
      `    <span><strong>Metadata</strong>${escapeHTML(item.metadata ? 'present' : 'none')}</span>`,
      '  </div>',
      '</article>',
    ].join('\n');
  }).join('');
}
function renderInstanceSummary(body) {
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setInstanceSummaryEmpty('Summary response was not valid JSON.');
    return;
  }
  const cards = [
    { title: 'Lifecycle', body: `status=${parsed.status || '-'} ready=${String(Boolean(parsed.ready))}`, footer: parsed.ready_reason || 'no readiness detail' },
    { title: 'Sessions', body: `${parsed.sessions || 0} total / ${parsed.archived_sessions || 0} archived`, footer: `${parsed.waiting_sessions || 0} waiting sessions` },
    { title: 'Messages', body: `${parsed.messages || 0} total`, footer: `${parsed.user_messages || 0} user / ${parsed.assistant_messages || 0} assistant` },
    { title: 'Runs', body: `${parsed.runs || 0} total`, footer: `${parsed.waiting_runs || 0} waiting runs` },
  ];
  const runsByStatus = parsed.runs_by_status || {};
  Object.keys(runsByStatus).sort().forEach((key) => {
    cards.push({ title: `Run ${key}`, body: `${runsByStatus[key]} runs`, footer: `status bucket: ${key}` });
  });
  if (parsed.last_activity_at) {
    cards.push({ title: 'Last Activity', body: parsed.last_activity_at, footer: parsed.instance_id ? `instance=${parsed.instance_id}` : 'summary timestamp' });
  }
  els.instanceSummaryGrid.innerHTML = cards.map((card) => [
    '<article class="api-card summary-card">',
    `  <h4>${escapeHTML(card.title)}</h4>`,
    `  <p>${escapeHTML(card.body)}</p>`,
    `  <div class="cap-foot">${escapeHTML(card.footer)}</div>`,
    '</article>',
  ].join('\\n')).join('');
}

async function loadInstanceSummary() {
  const result = await safeCall(() => app().GetInstanceSummary(els.instanceId.value.trim()), els.instanceOutput, (value) => value.body || '');
  renderInstanceSummary(result?.body || '');
  return result;
}

function boolWord(value) {
  return value ? 'enabled' : 'disabled';
}

function summarizeSSHPolicy(metadata = {}) {
  const direct = metadata.ssh_direct_connect_enabled === 'true';
  const transfer = metadata.ssh_file_transfer_enabled === 'true';
  if (!direct && !transfer) {
    return 'SSH is limited to configured labels, and local file transfer is blocked.';
  }
  if (!direct && transfer) {
    return 'SSH requires configured labels, but workspace-scoped file transfer is enabled.';
  }
  if (direct && !transfer) {
    return 'Direct SSH credentials are enabled, but local file transfer stays blocked.';
  }
  return 'Direct SSH credentials and workspace-scoped file transfer are both enabled.';
}
function summarizeBashPolicy(parsed = {}, metadata = {}) {
  const scopedTenant = metadata.bash_scope_tenant_id || '-';
  const scopedUser = metadata.bash_scope_user_id || '-';
  if (parsed.supports_local_bash) {
    return {
      body: 'Local bash is active for the current authenticated principal.',
      footer: `scope: tenant=${scopedTenant} user=${scopedUser}`,
    };
  }
  if (scopedTenant !== '-' && scopedUser !== '-') {
    return {
      body: 'Local bash is configured, but this instance principal is outside the allowed tenant/user scope.',
      footer: `scope: tenant=${scopedTenant} user=${scopedUser}`,
    };
  }
  return {
    body: 'Local bash is not available for this deployment.',
    footer: 'scope not configured',
  };
}


function renderCapabilitiesSummary(body) {
  let parsed;
  try {
    parsed = JSON.parse(body || '{}');
  } catch {
    setCapabilityEmpty('Capability response was not valid JSON.');
    return;
  }

  const tools = Array.isArray(parsed.tools) ? parsed.tools : [];
  const metadata = parsed.metadata || {};
  const pills = [
    { label: `Executor ${parsed.executor || '-'}`, tone: 'neutral' },
    { label: `SSH ${boolWord(Boolean(parsed.supports_ssh))}`, tone: parsed.supports_ssh ? 'good' : 'bad' },
    { label: `Ask user ${boolWord(Boolean(parsed.supports_ask_user))}`, tone: parsed.supports_ask_user ? 'good' : 'bad' },
    { label: `Local bash ${boolWord(Boolean(parsed.supports_local_bash))}`, tone: parsed.supports_local_bash ? 'warn' : 'good' },
  ];

  els.capabilitySummary.classList.remove('empty');
  els.capabilityPills.innerHTML = pills
    .map((pill) => `<span class="policy-pill ${pill.tone}">${escapeHTML(pill.label)}</span>`)
    .join('');

  const bashPolicy = summarizeBashPolicy(parsed, metadata);
  const introCards = [
    {
      title: 'SSH policy',
      body: summarizeSSHPolicy(metadata),
      footer: metadata.workspace_dir ? `workspace: ${metadata.workspace_dir}` : 'workspace not reported',
    },
    {
      title: 'Local bash scope',
      body: bashPolicy.body,
      footer: bashPolicy.footer,
      restricted: !parsed.supports_local_bash,
    },
    {
      title: 'Tool exposure',
      body: `${tools.filter((tool) => tool.enabled).length} enabled, ${tools.filter((tool) => !tool.enabled).length} restricted`,
      footer: parsed.supports_sessions ? 'session-based runtime enabled' : 'session support unavailable',
    },
  ];

  const toolCards = tools.map((tool) => {
    const properties = Object.keys(tool.parameters?.properties || {});
    const summary = properties.length > 0 ? `${properties.length} params: ${properties.slice(0, 6).join(', ')}` : 'no structured parameters';
    const footer = tool.enabled ? summary : (tool.disabled_reason || 'restricted by server policy');
    return {
      title: tool.name || 'tool',
      body: tool.description || 'No description provided.',
      footer,
      restricted: !tool.enabled,
    };
  });

  const cards = introCards.concat(toolCards)
    .map((card) => {
      const toneClass = card.restricted ? ' restricted' : '';
      return [
        `<article class="cap-card${toneClass}">`,
        `  <h4>${escapeHTML(card.title)}</h4>`,
        `  <p>${escapeHTML(card.body)}</p>`,
        `  <div class="cap-foot">${escapeHTML(card.footer)}</div>`,
        `</article>`,
      ].join('\n');
    })
    .join('');

  els.capabilityGrid.innerHTML = cards || '<p class="capability-empty">No capabilities returned.</p>';
  renderAPIExamples(parsed);
}
function renderAPIExamples(capabilities) {
  if (!els.apiExamplesGrid) {
    return;
  }
  const instanceId = (els.instanceId.value || '').trim();
  const sessionId = (els.sessionId.value || '').trim();
  const runId = (els.runId.value || '').trim();
  const baseURL = (els.baseUrl.value || '').trim() || 'http://127.0.0.1:18080';
  const token = (els.accessToken.value || '').trim() || '<access-token>';
  if (!instanceId) {
    setAPIExamplesEmpty('Create or select an instance first to generate REST examples here.');
    return;
  }

  const examples = [
    {
      title: 'Capabilities',
      body: 'Inspect current runtime policy, enabled tools, SSH exposure, and local bash scope.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/instances/${instanceId}/capabilities`)} \\\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    },
    {
      title: 'Access Token',
      body: 'Create or continue a conversation by posting a user message to the current instance.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/instances/${instanceId}/messages`)} \\\n  -H 'Content-Type: application/json' \\\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\\n  -d ${JSON.stringify(JSON.stringify({ session_id: sessionId || undefined, title: (els.messageTitle.value || '').trim() || 'Demo session', content: (els.messageInput.value || '').trim() || 'Hello from srvdemo' }))}`,
    },
    {
      title: 'Access Token',
      body: 'Browse per-user installed skills under the current tenant/user scope.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skills`)} \\\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    },
    {
      title: 'Search skills',
      body: 'Search GitHub, SkillMarket, and optionally SkillHub through the new REST facade.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skills/search`)} \\\n  -H 'Content-Type: application/json' \\\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\\n  -d ${JSON.stringify(JSON.stringify({ query: 'ssh deploy', sources: ['github', 'skillmarket'], include_installed: true }))}`,
    },
    {
      title: 'Validate skill',
      body: 'Run portability checks before upload or multi-platform reuse.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skills/demo-skill/validate`)} \\\n  -X POST \\\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    },
  ];

  if (capabilities?.supports_ssh) {
    examples.push({
      title: 'Runs',
      body: 'Poll run status after the agent responds or requests more user input.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/instances/${instanceId}/runs${runId ? `/${runId}` : ''}`)} \\\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    });
  }

  els.apiExamplesGrid.innerHTML = examples.map((item) => [
    '<article class="api-card">',
    `  <h4>${escapeHTML(item.title)}</h4>`,
    `  <p>${escapeHTML(item.body)}</p>`,
    `  <pre class="api-snippet">${escapeHTML(item.snippet)}</pre>`,
    '</article>',
  ].join('\n')).join('');
}

async function safeCall(fn, target, formatter) {
  setOperationStatus('running', 'Request Running', 'Calling MaClawSrv...');
  try {
    const result = await fn();
    const formatted = formatter ? formatter(result) : (result?.body || JSON.stringify(result, null, 2));
    renderOutput(target, formatted, false);
    const statusCode = result?.status_code || result?.StatusCode;
    setOperationStatus('success', 'Access Token', statusCode ? `Completed with HTTP ${statusCode}.` : 'Access Token');
    return result;
  } catch (error) {
    const detail = error?.message || String(error);
    const body = error?.body || error?.Body;
    renderOutput(target, body ? `${detail}\n\n${body}` : detail, true);
    setOperationStatus('error', 'Request Failed', detail);
    throw error;
  }
}

async function loadSettings() {
  if (!hasBridge()) {
    els.bridgeStatus.textContent = 'Unavailable';
    renderOutput(els.authOutput, 'Wails bridge is unavailable. Start with `wails dev` or a packaged build.', true);
    setCapabilityEmpty('Wails bridge is unavailable.');
    setOperationStatus('error', 'Bridge Unavailable', 'No Wails bridge was detected in the current environment.');
    return;
  }
  els.bridgeStatus.textContent = 'Connected';
  setOperationStatus('idle', 'Idle', 'Authenticate first, then continue with Skills, MCP, and agent flows.');
  setInstanceSummaryEmpty('Load instance summary to see session, run, and waiting-state counters.');
  setSessionListEmpty('Load sessions to inspect archived and waiting conversations.');
  setRunListEmpty('Load runs to inspect status, source, and waiting-for-user state.');
  setMessageTimelineEmpty('Load messages to inspect role, content, and timeline order.');
  const settings = await app().LoadSettings();
  syncSettingsView(settings);
  setCapabilityEmpty('Create an instance to inspect SSH policy, tool exposure, and bash availability.');
  setInstanceListEmpty('Load instances to inspect the current user runtime fleet.');
  setInstanceDetailEmpty('Select an instance to inspect readiness, workspace, and metadata.');
  setAPIExamplesEmpty('Create or select an instance first to generate REST examples here.');
  setMCPToolsEmpty('Select an MCP server to inspect its discovered tools.');
  syncMCPKindUI();
  renderMCPGuide();
  renderMCPExamples();
  renderSkillGuide();
  renderSkillExamples();
  renderPlaybooks();
  renderQuickGuide();
  renderIntegrationChecklist();
  renderDemoFlow();
  renderResponseSamples();
  renderIntegrationPack();
  renderRuntimeFilterPills();
  setSkillSearchEmpty('Search results from GitHub, SkillHub, or SkillMarket will appear here.');
  setSkillListEmpty('Installed skills for the current user will appear here.');
}

async function saveSettings() {
  const result = await safeCall(() => app().SaveSettings(currentSettingsPayload()), els.authOutput, (value) => `Settings saved\n\n${JSON.stringify(value, null, 2)}`);
  syncSettingsView(result);
  renderQuickGuide();
  renderIntegrationChecklist();
  renderDemoFlow();
  renderResponseSamples();
  renderIntegrationPack();
}

async function login() {
  const payload = {
    base_url: els.baseUrl.value.trim(),
    api_key: els.apiKey.value.trim(),
    api_secret: els.apiSecret.value.trim(),
    skip_tls_verify: els.skipTLSVerify.checked,
    timeout_sec: Number(els.timeoutSec.value || '60'),
  };
  await safeCall(() => app().Login(payload), els.authOutput, (value) => {
    els.accessToken.value = value.access_token || '';
    return [
      'Access Token',
      `expires_at: ${value.expires_at || ''}`,
      '',
      'Access Token',
      value.principal || '',
      '',
      'Access Token',
      value.me || '',
    ].join('\n');
  });
  els.baseUrlView.textContent = payload.base_url;
  renderQuickGuide();
  renderIntegrationChecklist();
  renderDemoFlow();
  renderResponseSamples();
  renderIntegrationPack();
}

async function loadConfig() {
  const result = await safeCall(() => app().GetConfig(), els.configOutput, (value) => value.body || '');
  if (result?.body) {
    els.configEditor.value = result.body;
  }
  renderQuickGuide();
  renderIntegrationChecklist();
  renderDemoFlow();
  renderResponseSamples();
  renderIntegrationPack();
}

function currentSkillSearchPayload() {
  return {
    query: els.skillSearchQuery.value.trim(),
    sources: (els.skillSources.value || '').split(',').map((item) => item.trim()).filter(Boolean),
    top_n: Number(els.skillTopN.value || '10'),
    skill_hub_url: els.skillHubUrl.value.trim(),
    skill_market_url: els.skillMarketUrl.value.trim(),
    github_token: els.skillGitHubToken.value.trim(),
    include_installed: els.includeInstalledSkills.checked,
  };
}

function currentSkillInstallPayload() {
  return {
    source: els.skillInstallSource.value.trim(),
    repo_url: els.skillRepoUrl.value.trim(),
    raw_url: els.skillRawUrl.value.trim(),
    repo_full_name: els.skillRepoFullName.value.trim(),
    file_path: els.skillFilePath.value.trim(),
    branch: els.skillBranch.value.trim(),
    definition_type: els.skillDefinitionType.value.trim(),
    zip_base64: els.skillZipBase64.value.trim(),
    skill_hub_url: els.skillHubUrl.value.trim(),
    skill_id: els.skillID.value.trim(),
    overwrite: els.skillOverwrite.checked,
    github_token: els.skillGitHubToken.value.trim(),
  };
}

function currentSkillImportPayload() {
  return {
    zip_base64: els.skillZipBase64.value.trim(),
    overwrite: els.skillOverwrite.checked,
    archive_name: els.skillArchiveName.value.trim(),
  };
}

function currentSkillActionName() {
  const source = (els.skillInstallSource.value || '').trim().toLowerCase();
  if (source === 'zip') {
    return 'zip install';
  }
  if (source === 'skillhub' || source === 'skillmarket') {
    return `${source} install`;
  }
  if (source === 'github') {
    return 'github install';
  }
  return source || 'skill install';
}

function applySkillSourceTemplate(source) {
  const normalized = (source || '').trim().toLowerCase();
  els.skillInstallSource.value = normalized || 'github';
  els.skillOverwrite.checked = false;
  if (normalized === 'github') {
    els.skillRepoFullName.value = 'owner/repo';
    els.skillRepoUrl.value = 'https://github.com/owner/repo';
    els.skillRawUrl.value = '';
    els.skillFilePath.value = 'skills/demo/SKILL.md';
    els.skillBranch.value = 'main';
    els.skillDefinitionType.value = 'skill';
    els.skillID.value = '';
    if (!els.skillSearchQuery.value.trim()) {
      els.skillSearchQuery.value = 'ssh deploy';
    }
    if (!els.skillSources.value.trim()) {
      els.skillSources.value = 'github,skillmarket';
    }
  } else if (normalized === 'zip') {
    els.skillRepoFullName.value = '';
    els.skillRepoUrl.value = '';
    els.skillRawUrl.value = '';
    els.skillFilePath.value = '';
    els.skillBranch.value = '';
    els.skillDefinitionType.value = '';
    els.skillID.value = '';
    els.skillArchiveName.value = 'demo-skill.zip';
    if (!els.skillZipBase64.value.trim()) {
      els.skillZipBase64.value = '';
    }
  } else if (normalized === 'skillhub') {
    els.skillRepoFullName.value = '';
    els.skillRepoUrl.value = '';
    els.skillRawUrl.value = '';
    els.skillFilePath.value = '';
    els.skillBranch.value = '';
    els.skillDefinitionType.value = '';
    els.skillID.value = 'skill_xxx';
    if (!els.skillHubUrl.value.trim()) {
      els.skillHubUrl.value = 'https://hub.example.com';
    }
    if (!els.skillSources.value.trim()) {
      els.skillSources.value = 'skillhub,github';
    }
  } else if (normalized === 'skillmarket') {
    els.skillRepoFullName.value = '';
    els.skillRepoUrl.value = '';
    els.skillRawUrl.value = '';
    els.skillFilePath.value = '';
    els.skillBranch.value = '';
    els.skillDefinitionType.value = '';
    els.skillID.value = 'skill_xxx';
    if (!els.skillMarketUrl.value.trim()) {
      els.skillMarketUrl.value = 'https://market.example.com';
    }
    if (!els.skillMarketEmail.value.trim()) {
      els.skillMarketEmail.value = 'demo@example.com';
    }
    if (!els.skillSources.value.trim()) {
      els.skillSources.value = 'skillmarket,github';
    }
  }
  renderSkillGuide();
  renderSkillExamples();
}

function setSkillGuideEmpty(message) {
  if (!els.skillGuideGrid) {
    return;
  }
  els.skillGuideGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setSkillSearchEmpty(message) {
  if (!els.skillSearchGrid) {
    return;
  }
  els.skillSearchGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function applySkillSearchResult(item) {
  const source = String(item.source || 'github').toLowerCase();
  applySkillSourceTemplate(source);
  if (item.name) els.skillName.value = item.name;
  if (item.repo_full_name) els.skillRepoFullName.value = item.repo_full_name;
  if (item.repo_url) els.skillRepoUrl.value = item.repo_url;
  if (item.raw_url) els.skillRawUrl.value = item.raw_url;
  if (item.file_path) els.skillFilePath.value = item.file_path;
  if (item.branch) els.skillBranch.value = item.branch;
  if (item.definition_type) els.skillDefinitionType.value = item.definition_type;
  if (item.id && source !== 'github') els.skillID.value = item.id;
  renderSkillGuide();
  renderSkillExamples();
  updateSelectedSkillCard();
}

function renderSkillSearchResults(items) {
  if (!els.skillSearchGrid) {
    return;
  }
  const results = Array.isArray(items) ? items : [];
  if (!results.length) {
    setSkillSearchEmpty('No matching skills were found.');
    return;
  }
  els.skillSearchGrid.dataset.items = JSON.stringify(results);
  els.skillSearchGrid.innerHTML = results.map((item, index) => {
    const source = item.source || 'unknown';
    const name = item.name || item.id || `result-${index + 1}`;
    const description = item.description || 'No description provided.';
    const badges = [source];
    if (item.version) badges.push(`v${item.version}`);
    if (item.author) badges.push(item.author);
    if (item.installed) badges.push('installed');
    const meta = item.repo_full_name || item.file_path || item.raw_url || item.id || '';
    return [
      `<article class="api-card skill-search-card" data-search-index="${index}">`,
      '  <div class="skill-card-head">',
      `    <h4>${escapeHTML(name)}</h4>`,
      `    <span class="skill-card-meta">${escapeHTML(source)}</span>`,
      '  </div>',
      `  <p>${escapeHTML(description)}</p>`,
      `  <div class="mcp-server-badges">${badges.map((badge) => `<span class="policy-pill neutral">${escapeHTML(badge)}</span>`).join('')}</div>`,
      meta ? `  <div class="cap-foot">${escapeHTML(meta)}</div>` : '',
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-search-action="fill">Fill form</button>',
      '    <button class="ghost" type="button" data-search-action="install">Install</button>',
      '  </div>',
      '</article>',
    ].filter(Boolean).join('\n');
  }).join('');
}

function setSkillListEmpty(message) {
  if (!els.skillListGrid) {
    return;
  }
  els.skillListGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function updateSelectedSkillCard() {
  if (!els.skillListGrid) {
    return;
  }
  const selectedName = (els.skillName.value || '').trim();
  els.skillListGrid.querySelectorAll('[data-skill-name]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-skill-name') === selectedName);
  });
}

function renderSkillList(items, meta = {}) {
  if (!els.skillListGrid) {
    return;
  }
  const skills = Array.isArray(items) ? items : [];
  if (!skills.length) {
    const emptyMessage = registryPaging.skills.before
      ? 'No more installed skills were found before the current cursor.'
      : 'No installed skills found for the current user.';
    setSkillListEmpty(emptyMessage);
    return;
  }
  const selectedName = (els.skillName.value || '').trim();
  const cards = skills.map((item) => {
    const name = item.name || item.Name || item.id || item.ID || 'skill';
    const description = item.description || item.Description || item.summary || item.Summary || 'No description provided.';
    const version = item.version || item.Version || '';
    const triggers = Array.isArray(item.triggers || item.Triggers) ? (item.triggers || item.Triggers) : [];
    const selected = name === selectedName ? ' selected' : '';
    const badges = [];
    if (version) badges.push(`v${version}`);
    if (triggers.length) badges.push(`${triggers.length} triggers`);
    return [
      `<article class="api-card skill-card${selected}" data-skill-name="${escapeHTML(name)}">`,
      '  <div class="skill-card-head">',
      `    <h4>${escapeHTML(name)}</h4>`,
      version ? `    <span class="skill-card-meta">${escapeHTML(version)}</span>` : '    <span class="skill-card-meta">installed</span>',
      '  </div>',
      `  <p>${escapeHTML(description)}</p>`,
      badges.length ? `  <div class="mcp-server-badges">${badges.map((badge) => `<span class="policy-pill neutral">${escapeHTML(badge)}</span>`).join('')}</div>` : '',
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-skill-action="get">Get</button>',
      '    <button class="ghost" type="button" data-skill-action="validate">Validate</button>',
      '    <button class="ghost" type="button" data-skill-action="export">Export</button>',
      '    <button class="ghost" type="button" data-skill-action="delete">Delete</button>',
      '  </div>',
      '</article>',
    ].filter(Boolean).join('\n');
  });
  cards.push(renderPaginationCard('skill', meta, 'skill registry'));
  els.skillListGrid.innerHTML = cards.join('');
}

async function refreshSkillRegistry(options = {}) {
  if (options.reset) {
    registryPaging.skills.before = '';
  } else if (Object.prototype.hasOwnProperty.call(options, 'before')) {
    registryPaging.skills.before = options.before || '';
  }
  const result = await safeCall(() => app().ListSkillsPage({
    limit: registryPaging.skills.limit,
    before: registryPaging.skills.before,
  }), els.skillOutput, (value) => value.body || '');
  try {
    const parsed = JSON.parse(result.body || '{}');
    const page = normalizePageEnvelope(parsed);
    registryPaging.skills.hasMore = page.hasMore;
    registryPaging.skills.nextBefore = page.nextBefore;
    renderSkillList(page.items, page);
  } catch {
    setSkillListEmpty('Skill list response was not valid JSON.');
  }
  updateSelectedSkillCard();
  return result;
}

function renderSkillGuide() {
  if (!els.skillGuideGrid) {
    return;
  }
  const source = (els.skillInstallSource.value || '').trim().toLowerCase() || 'github';
  const installPayload = currentSkillInstallPayload();
  const importPayload = currentSkillImportPayload();
  const cards = [];

  cards.push({
    title: 'Install mode',
    body: source === 'github'
      ? 'Use GitHub mode when the skill definition already lives in a repo or raw URL.'
      : source === 'zip'
        ? 'Use zip mode when another tool already exported a base64 skill archive.'
        : 'Use source-specific mode when your skill comes from a remote marketplace or hub.',
    snippet: `source=${escapeHTML(source)}\noverwrite=${escapeHTML(String(els.skillOverwrite.checked))}`,
    raw: true,
  });

  if (source === 'github') {
    cards.push({
      title: 'GitHub checklist',
      body: 'Prefer repo_full_name + file_path + branch. Raw URL is useful for one-off direct installs.',
      snippet: `repo_full_name=${escapeHTML(els.skillRepoFullName.value.trim() || 'owner/repo')}\nfile_path=${escapeHTML(els.skillFilePath.value.trim() || 'skills/demo/SKILL.md')}\nbranch=${escapeHTML(els.skillBranch.value.trim() || 'main')}`,
      raw: true,
    });
  } else if (source === 'zip') {
    cards.push({
      title: 'Zip checklist',
      body: 'Paste the exported base64 archive into Zip base64. Import is useful when you do not need source metadata.',
      snippet: `archive_name=${escapeHTML(els.skillArchiveName.value.trim() || 'demo-skill.zip')}\nzip_base64=${escapeHTML((els.skillZipBase64.value || '').trim() ? '<provided>' : '<missing>')}`,
      raw: true,
    });
  } else {
    cards.push({
      title: 'Remote source checklist',
      body: 'Set skill_id and service URL when installing from a hub or market source.',
      snippet: `skill_id=${escapeHTML(els.skillID.value.trim() || 'skill_xxx')}\nservice_url=${escapeHTML((els.skillHubUrl.value.trim() || els.skillMarketUrl.value.trim() || 'https://service.example.com'))}`,
      raw: true,
    });
  }

  cards.push({
    title: 'Install payload draft',
    body: `This is the JSON body that the ${currentSkillActionName()} request will send.`,
    snippet: JSON.stringify(installPayload, null, 2),
    raw: true,
  });

  cards.push({
    title: 'Import payload draft',
    body: 'This is the JSON body that the direct import request will send.',
    snippet: JSON.stringify(importPayload, null, 2),
    raw: true,
  });

  els.skillGuideGrid.innerHTML = cards.map((card) => [
    '<article class="api-card">',
    `  <h4>${escapeHTML(card.title)}</h4>`,
    `  <p>${escapeHTML(card.body)}</p>`,
    `  <pre class="api-snippet">${escapeHTML(card.snippet)}</pre>`,
    '</article>',
  ].join('\n')).join('');
}

function renderSkillExamples() {
  if (!els.skillExamplesGrid) {
    return;
  }
  const baseURL = (els.baseUrl.value || '').trim() || 'http://127.0.0.1:18080';
  const token = (els.accessToken.value || '').trim() || '<access-token>';
  const name = (els.skillName.value || '').trim() || 'demo-skill';
  const submissionId = (els.skillSubmissionId.value || '').trim();
  const marketURL = (els.skillMarketUrl.value || '').trim();
  const marketEmail = (els.skillMarketEmail.value || '').trim() || 'demo@example.com';
  const searchPayload = currentSkillSearchPayload();
  const installPayload = currentSkillInstallPayload();
  const importPayload = currentSkillImportPayload();
  const examples = [
    {
      title: 'Access Token',
      body: 'List skills already installed for the current tenant/user scope.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/skills')} \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    },
    {
      title: 'Search skills',
      body: 'Search GitHub, SkillMarket, or other sources from the current form state.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/skills/search')} \\n  -X POST \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify(searchPayload))}`,
    },
    {
      title: 'Access Token',
      body: `Install using the current ${currentSkillActionName()} draft.`,
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/skills/install')} \\n  -X POST \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify(installPayload))}`,
    },
    {
      title: 'Import archive',
      body: 'Import a base64 skill archive directly without source metadata.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/skills/import')} \\n  -X POST \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify(importPayload))}`,
    },
    {
      title: 'Validate and improve',
      body: 'Run portability validation first, then optionally apply auto-fix.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skills/${name}/validate`)} \\n  -X POST \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}\n\n` +
        `curl -s ${JSON.stringify(baseURL + `/api/v1/skills/${name}/improve`)} \\n  -X POST \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify({ auto_fix: true }))}`,
    },
    {
      title: 'Export and upload',
      body: 'Export the skill archive or upload it to the configured market endpoint.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skills/${name}/export`)} \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}\n\n` +
        `curl -s ${JSON.stringify(baseURL + `/api/v1/skills/${name}/upload`)} \\n  -X POST \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify({ skill_market_url: marketURL, email: marketEmail }))}`,
    },
  ];

  if (submissionId) {
    examples.push({
      title: 'Upload status',
      body: 'Poll the async market submission status.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skill-uploads/${submissionId}${marketURL ? `?base_url=${encodeURIComponent(marketURL)}` : ''}`)} \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    });
  }

  if (marketEmail) {
    const query = new URLSearchParams({ email: marketEmail });
    if (marketURL) {
      query.set('base_url', marketURL);
    }
    examples.push({
      title: 'Market account',
      body: 'Read current market account credits and status for the provided email.',
      snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/skill-market/account?${query.toString()}`)} \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
    });
  }

  els.skillExamplesGrid.innerHTML = examples.map((item) => [
    '<article class="api-card">',
    `  <h4>${escapeHTML(item.title)}</h4>`,
    `  <p>${escapeHTML(item.body)}</p>`,
    `  <pre class="api-snippet">${escapeHTML(item.snippet)}</pre>`,
    '</article>',
  ].join('\n')).join('');
}

function parseOptionalJSONObject(raw, fieldName) {
  const text = (raw || '').trim();
  if (!text) {
    return undefined;
  }
  try {
    const parsed = JSON.parse(text);
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
      throw new Error(`${fieldName} must be a JSON object`);
    }
    return parsed;
  } catch (error) {
    throw new Error(`${fieldName} must be valid JSON: ${error.message || error}`);
  }
}

function splitCommaList(raw) {
  return (raw || '').split(',').map((item) => item.trim()).filter(Boolean);
}

function parseOptionalJSONObjectLoose(raw) {
  const text = (raw || '').trim();
  if (!text) {
    return undefined;
  }
  try {
    const parsed = JSON.parse(text);
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
      return text;
    }
    return parsed;
  } catch {
    return text;
  }
}

function currentMCPDraftPayload() {
  const kind = els.mcpKind.value;
  const payload = {
    kind,
    name: els.mcpName.value.trim() || (kind === 'local' ? 'Local MCP' : 'Remote MCP'),
  };
  if (kind === 'remote') {
    payload.endpoint_url = els.mcpEndpointUrl.value.trim() || 'https://mcp.example.com';
    payload.auth_type = els.mcpAuthType.value || 'none';
    if ((els.mcpAuthSecret.value || '').trim()) {
      payload.auth_secret = '<hidden>';
    }
    const headers = parseOptionalJSONObjectLoose(els.mcpHeaders.value);
    if (headers !== undefined) {
      payload.headers = headers;
    }
  } else {
    payload.command = els.mcpCommand.value.trim() || 'npx';
    const args = splitCommaList(els.mcpArgs.value);
    if (args.length) {
      payload.args = args;
    }
    const env = parseOptionalJSONObjectLoose(els.mcpEnv.value);
    if (env !== undefined) {
      payload.env = env;
    }
  }
  if (els.mcpDisabled.checked) {
    payload.disabled = true;
  }
  if (els.mcpAutoStart.checked) {
    payload.auto_start = true;
  }
  return payload;
}

function setMCPExamplesEmpty(message) {
  if (!els.mcpExamplesGrid) {
    return;
  }
  els.mcpExamplesGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function renderMCPGuide() {
  if (!els.mcpGuideGrid) {
    return;
  }
  const kind = els.mcpKind.value;
  const authType = els.mcpAuthType.value;
  const payload = currentMCPDraftPayload();
  const cards = [];

  if (kind === 'local') {
    cards.push({
      title: 'Local stdio mode',
      body: 'Use this when MaClawSrv should spawn an MCP process for the current user scope.',
      snippet: 'Typical command: npx or uvx. Keep workspace paths inside the current tenant/user data area.',
    });
    cards.push({
      title: 'Command checklist',
      body: 'Provide a startup command and comma-separated args. Auto start is useful for always-on local servers.',
      snippet: `command=${escapeHTML(els.mcpCommand.value.trim() || 'npx')}\nargs=${escapeHTML(els.mcpArgs.value.trim() || '-y,@modelcontextprotocol/server-filesystem,D:\\workprj\\aicoder')}`,
      raw: true,
    });
  } else {
    cards.push({
      title: 'Remote HTTP mode',
      body: 'Use this when an external MCP endpoint already exists and MaClawSrv only needs to connect to it.',
      snippet: 'Set endpoint URL, choose auth type, and add custom headers only when the remote service requires them.',
    });
    cards.push({
      title: 'Remote auth hint',
      body: authType === 'bearer'
        ? 'Bearer mode is selected. Put the token in Auth Secret, or use Headers JSON for custom Authorization wiring.'
        : authType === 'api_key'
          ? 'API key mode is selected. Put the key in Auth Secret and add any vendor-specific headers in Headers JSON.'
          : 'No built-in auth selected. Use Headers JSON if the remote gateway still needs custom metadata.',
      snippet: `auth_type=${escapeHTML(authType || 'none')}\nendpoint=${escapeHTML(els.mcpEndpointUrl.value.trim() || 'https://mcp.example.com')}`,
      raw: true,
    });
  }

  cards.push({
    title: 'Payload draft',
    body: 'This is the JSON body that the Create MCP action is about to send.',
    snippet: JSON.stringify(payload, null, 2),
    raw: true,
  });

  els.mcpGuideGrid.innerHTML = cards.map((card) => [
    '<article class="api-card">',
    `  <h4>${escapeHTML(card.title)}</h4>`,
    `  <p>${escapeHTML(card.body)}</p>`,
    `  <pre class="api-snippet">${card.raw ? escapeHTML(card.snippet) : escapeHTML(card.snippet)}</pre>`,
    '</article>',
  ].join('\n')).join('');
}

function renderMCPExamples() {
  if (!els.mcpExamplesGrid) {
    return;
  }
  const baseURL = (els.baseUrl.value || '').trim() || 'http://127.0.0.1:18080';
  const token = (els.accessToken.value || '').trim() || '<access-token>';
  const serverID = (els.mcpServerId.value || '').trim();
  const payload = currentMCPDraftPayload();
  const examples = [
    {
      title: 'Create server',
      body: 'Create an MCP server from the current form draft.',
      snippet: `curl -s ${JSON.stringify(baseURL + '/api/v1/mcp/servers')} \\n  -X POST \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify(payload))}`,
    },
  ];
  if (serverID) {
    examples.push(
      {
        title: 'Get server',
        body: 'Inspect the current persisted MCP server configuration.',
        snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/mcp/servers/${serverID}`)} \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
      },
      {
        title: 'Access Token',
        body: 'Run a connectivity or process health check on the selected server.',
        snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/mcp/servers/${serverID}/health-check`)} \\n  -X POST \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
      },
      {
        title: 'List tools',
        body: 'Fetch the discovered tools exposed by the selected MCP server.',
        snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/mcp/servers/${serverID}/tools`)} \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
      },
      {
        title: 'Update flags',
        body: 'Patch the selected server state without recreating it.',
        snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/mcp/servers/${serverID}`)} \\n  -X PATCH \\n  -H 'Content-Type: application/json' \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)} \\n  -d ${JSON.stringify(JSON.stringify({ disabled: els.mcpDisabled.checked, auto_start: els.mcpAutoStart.checked }))}`,
      },
    );
    if (els.mcpKind.value === 'local') {
      examples.push({
        title: 'Start local server',
        body: 'Start the selected local stdio MCP process under the current user scope.',
        snippet: `curl -s ${JSON.stringify(baseURL + `/api/v1/mcp/servers/${serverID}/start`)} \\n  -X POST \\n  -H ${JSON.stringify(`Authorization: Bearer ${token}`)}`,
      });
    }
  } else {
    examples.push({
      title: 'Next step',
      body: 'Create a server first, then select it from the registry to unlock get, health, tools, start, stop, and patch examples.',
      snippet: 'The MCP registry cards below become selectable after a successful create or list operation.',
    });
  }

  els.mcpExamplesGrid.innerHTML = examples.map((item) => [
    '<article class="api-card">',
    `  <h4>${escapeHTML(item.title)}</h4>`,
    `  <p>${escapeHTML(item.body)}</p>`,
    `  <pre class="api-snippet">${escapeHTML(item.snippet)}</pre>`,
    '</article>',
  ].join('\n')).join('');
}

function currentMCPCreatePayload() {
  return {
    kind: els.mcpKind.value,
    name: els.mcpName.value.trim(),
    endpoint_url: els.mcpEndpointUrl.value.trim(),
    auth_type: els.mcpAuthType.value,
    auth_secret: els.mcpAuthSecret.value,
    headers: parseOptionalJSONObject(els.mcpHeaders.value, 'headers'),
    command: els.mcpCommand.value.trim(),
    args: splitCommaList(els.mcpArgs.value),
    env: parseOptionalJSONObject(els.mcpEnv.value, 'env'),
    disabled: els.mcpDisabled.checked,
    auto_start: els.mcpAutoStart.checked,
  };
}

function currentMCPUpdatePayload() {
  const payload = {};
  const name = els.mcpName.value.trim();
  const endpoint = els.mcpEndpointUrl.value.trim();
  const authSecret = els.mcpAuthSecret.value;
  const command = els.mcpCommand.value.trim();
  if (name) payload.name = name;
  if (endpoint) payload.endpoint_url = endpoint;
  if (els.mcpAuthType.value) payload.auth_type = els.mcpAuthType.value;
  if (authSecret) payload.auth_secret = authSecret;
  if (els.mcpHeaders.value.trim()) payload.headers = parseOptionalJSONObject(els.mcpHeaders.value, 'headers');
  if (command) payload.command = command;
  if (els.mcpArgs.value.trim()) payload.args = splitCommaList(els.mcpArgs.value);
  if (els.mcpEnv.value.trim()) payload.env = parseOptionalJSONObject(els.mcpEnv.value, 'env');
  payload.disabled = els.mcpDisabled.checked;
  payload.auto_start = els.mcpAutoStart.checked;
  return payload;
}

function setMCPToolsEmpty(message) {
  if (!els.mcpToolsGrid) {
    return;
  }
  els.mcpToolsGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function setMCPListEmpty(message) {
  if (!els.mcpListGrid) {
    return;
  }
  els.mcpListGrid.innerHTML = `<p class="capability-empty">${escapeHTML(message)}</p>`;
}

function updateSelectedMCPCard() {
  if (!els.mcpListGrid) {
    return;
  }
  const selectedID = (els.mcpServerId.value || '').trim();
  els.mcpListGrid.querySelectorAll('[data-server-id]').forEach((node) => {
    node.classList.toggle('selected', node.getAttribute('data-server-id') === selectedID);
  });
}

function renderMCPServerList(items, meta = {}) {
  if (!els.mcpListGrid) {
    return;
  }
  const servers = Array.isArray(items) ? items : [];
  if (!servers.length) {
    const emptyMessage = registryPaging.mcp.before
      ? 'No more MCP servers were found before the current cursor.'
      : 'No MCP servers configured for the current user.';
    setMCPListEmpty(emptyMessage);
    return;
  }
  const selectedID = (els.mcpServerId.value || '').trim();
  const cards = servers.map((item) => {
    const summary = item.kind === 'local'
      ? (item.command || 'local command not set')
      : (item.endpoint_url || 'remote endpoint not set');
    const badges = [item.kind || '-', item.health_status || 'unknown', item.running ? 'running' : 'idle'];
    const selected = item.id === selectedID ? ' selected' : '';
    return [
      `<article class="api-card mcp-server-card${selected}" data-server-id="${escapeHTML(item.id || '')}">`,
      '  <div class="mcp-server-head">',
      `    <h4>${escapeHTML(item.name || item.id || 'MCP server')}</h4>`,
      `    <span class="mcp-server-id">${escapeHTML(item.id || '')}</span>`,
      '  </div>',
      `  <p>${escapeHTML(summary)}</p>`,
      `  <div class="mcp-server-badges">${badges.map((badge) => `<span class="policy-pill neutral">${escapeHTML(badge)}</span>`).join('')}</div>`,
      '  <div class="mcp-server-actions">',
      '    <button class="ghost" type="button" data-mcp-action="get">Get</button>',
      '    <button class="ghost" type="button" data-mcp-action="health">Health</button>',
      '    <button class="ghost" type="button" data-mcp-action="tools">Tools</button>',
      '    <button class="ghost" type="button" data-mcp-action="start">Start</button>',
      '    <button class="ghost" type="button" data-mcp-action="stop">Stop</button>',
      '  </div>',
      '</article>',
    ].join('\n');
  });
  cards.push(renderPaginationCard('mcp', meta, 'mcp registry'));
  els.mcpListGrid.innerHTML = cards.join('');
}

function renderMCPTools(items) {
  if (!els.mcpToolsGrid) {
    return;
  }
  const tools = Array.isArray(items) ? items : [];
  if (!tools.length) {
    setMCPToolsEmpty('No tools discovered for this MCP server yet.');
    return;
  }
  els.mcpToolsGrid.innerHTML = tools.map((tool) => {
    const keys = Object.keys(tool.input_schema?.properties || {});
    const summary = keys.length ? `${keys.length} params: ${keys.slice(0, 8).join(', ')}` : 'no structured parameters';
    return [
      '<article class="api-card">',
      `  <h4>${escapeHTML(tool.name || 'tool')}</h4>`,
      `  <p>${escapeHTML(tool.description || 'No description provided.')}</p>`,
      `  <pre class="api-snippet">${escapeHTML(summary)}</pre>`,
      '</article>',
    ].join('\n');
  }).join('');
}

function syncMCPKindUI() {
  const kind = els.mcpKind.value;
  document.querySelectorAll('.mcp-remote-field').forEach((node) => node.classList.toggle('mcp-hidden', kind !== 'remote'));
  document.querySelectorAll('.mcp-local-field').forEach((node) => node.classList.toggle('mcp-hidden', kind !== 'local'));
  renderMCPGuide();
  renderMCPExamples();
}

function applyLocalTemplate(name, packageName, args = []) {
  els.mcpKind.value = 'local';
  els.mcpName.value = name;
  els.mcpCommand.value = 'npx';
  els.mcpArgs.value = ['-y', packageName].concat(args).join(',');
  els.mcpEnv.value = '';
  els.mcpHeaders.value = '';
  els.mcpEndpointUrl.value = '';
  els.mcpAuthSecret.value = '';
  els.mcpAuthType.value = 'none';
  els.mcpDisabled.checked = false;
  els.mcpAutoStart.checked = true;
  syncMCPKindUI();
}

function fillFilesystemTemplate() {
  applyLocalTemplate('Filesystem MCP', '@modelcontextprotocol/server-filesystem', ['D:\\workprj\\aicoder']);
}

function fillGitTemplate() {
  applyLocalTemplate('Git MCP', '@modelcontextprotocol/server-git', ['D:\\workprj\\aicoder']);
}

function fillSQLiteTemplate() {
  applyLocalTemplate('SQLite MCP', '@modelcontextprotocol/server-sqlite', ['D:\\workprj\\aicoder\\srvdemo.db']);
}

function fillRemoteTemplate() {
  els.mcpKind.value = 'remote';
  els.mcpName.value = 'Remote MCP';
  els.mcpEndpointUrl.value = 'https://mcp.example.com';
  els.mcpAuthType.value = 'bearer';
  els.mcpAuthSecret.value = '';
  els.mcpCommand.value = '';
  els.mcpArgs.value = '';
  els.mcpEnv.value = '';
  els.mcpHeaders.value = JSON.stringify({ Authorization: 'Bearer <token>' }, null, 2);
  els.mcpDisabled.checked = false;
  els.mcpAutoStart.checked = false;
  syncMCPKindUI();
}

function syncMCPFormFromBody(body) {
  let parsed;
  try {
    parsed = typeof body === 'string' ? JSON.parse(body || '{}') : body;
  } catch {
    return;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return;
  }
  if (parsed.id) els.mcpServerId.value = parsed.id;
  if (parsed.kind) els.mcpKind.value = parsed.kind;
  if (parsed.name) els.mcpName.value = parsed.name;
  els.mcpEndpointUrl.value = parsed.endpoint_url || '';
  if (parsed.auth_type) {
    els.mcpAuthType.value = parsed.auth_type;
  } else if (parsed.kind === 'local') {
    els.mcpAuthType.value = 'none';
  }
  els.mcpArgs.value = Array.isArray(parsed.args) ? parsed.args.join(',') : '';
  els.mcpCommand.value = parsed.command || '';
  if (parsed.header_names === undefined) {
    els.mcpHeaders.value = '';
  }
  if (Array.isArray(parsed.header_names) && parsed.header_names.length) {
    const headers = {};
    parsed.header_names.forEach((name) => { headers[name] = '<configured>'; });
    els.mcpHeaders.value = JSON.stringify(headers, null, 2);
  }
  if (parsed.env_keys === undefined) {
    els.mcpEnv.value = '';
  }
  if (Array.isArray(parsed.env_keys) && parsed.env_keys.length) {
    const env = {};
    parsed.env_keys.forEach((name) => { env[name] = '<configured>'; });
    els.mcpEnv.value = JSON.stringify(env, null, 2);
  }
  if (typeof parsed.disabled === 'boolean') els.mcpDisabled.checked = parsed.disabled;
  if (typeof parsed.auto_start === 'boolean') els.mcpAutoStart.checked = parsed.auto_start;
  syncMCPKindUI();
  updateSelectedMCPCard();
  renderMCPTools(parsed.tools || []);
  renderMCPExamples();
}

async function refreshMCPRegistry(options = {}) {
  if (options.reset) {
    registryPaging.mcp.before = '';
  } else if (Object.prototype.hasOwnProperty.call(options, 'before')) {
    registryPaging.mcp.before = options.before || '';
  }
  const result = await safeCall(() => app().ListMCPServersPage({
    limit: registryPaging.mcp.limit,
    before: registryPaging.mcp.before,
  }), els.mcpOutput, (value) => value.body || '');
  try {
    const parsed = JSON.parse(result.body || '{}');
    const page = normalizePageEnvelope(parsed);
    registryPaging.mcp.hasMore = page.hasMore;
    registryPaging.mcp.nextBefore = page.nextBefore;
    renderMCPServerList(page.items, page);
  } catch {
    setMCPListEmpty('Registry response was not valid JSON.');
  }
  return result;
}

async function loadCapabilities() {
  const result = await safeCall(() => app().GetInstanceCapabilities(els.instanceId.value.trim()), els.instanceOutput, (value) => value.body || '');
  renderCapabilitiesSummary(result?.body || '');
  return result;
}

function bind() {
  document.body.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-copy-text]');
    if (!button) {
      return;
    }
    const text = decodeURIComponent(button.getAttribute('data-copy-text') || '');
    try {
      await navigator.clipboard.writeText(text);
      setOperationStatus('success', 'Demo Provisioned', 'Tenant, user, credential, and form fields were populated successfully.');
    } catch (error) {
      setOperationStatus('error', 'Profile', error?.message || String(error));
    }
  });
  document.getElementById('saveSettingsBtn').addEventListener('click', saveSettings);
  document.getElementById('healthBtn').addEventListener('click', () => safeCall(() => app().HealthCheck(), els.authOutput, (value) => value.body || ''));
  document.getElementById('loginBtn').addEventListener('click', login);
  document.getElementById('clearTokenBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().ClearToken(), els.authOutput, (value) => `Token cleared\n\n${JSON.stringify(value, null, 2)}`);
    syncSettingsView(result);
  });

  document.getElementById('provisionDemoBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().ProvisionDemo({
      tenant_name: els.tenantName.value.trim(),
      user_name: els.userName.value.trim(),
      user_email: els.userEmail.value.trim(),
      credential_name: els.credentialName.value.trim(),
      api_key: els.newApiKey.value.trim(),
      api_secret: els.newApiSecret.value.trim(),
    }), els.adminOutput, (value) => JSON.stringify(value, null, 2));
    els.tenantId.value = result.tenant_id || '';
    els.userId.value = result.user_id || '';
    applyAdminIdentityFilters(result.tenant_id || '', result.user_id || '');
    els.apiKey.value = result.api_key || '';
    els.apiSecret.value = result.api_secret || '';
    els.newApiKey.value = result.api_key || '';
    els.newApiSecret.value = result.api_secret || '';
  });
  document.getElementById('listTenantsBtn').addEventListener('click', () => refreshAdminTenants());
  document.getElementById('createTenantBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().CreateTenant({ name: els.tenantName.value.trim() }), els.adminOutput, (value) => value.body || '');
    try {
      const parsed = JSON.parse(result.body || '{}');
      if (parsed.id) {
        els.tenantId.value = parsed.id;
        applyAdminIdentityFilters(parsed.id, els.userId.value.trim());
        updateSelectedAdminTenantCard();
      }
    } catch {
    }
  });
  document.getElementById('updateTenantBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().UpdateTenant(els.tenantId.value.trim(), buildTenantUpdatePayload()), els.adminOutput, (value) => value.body || '');
    try {
      const parsed = JSON.parse(result.body || '{}');
      if (parsed.id) {
        els.tenantName.value = parsed.name || els.tenantName.value;
        assignQuotaFields('tenant', parsed.quota || {});
      }
    } catch {
    }
    await refreshAdminTenants();
  });
  document.getElementById('listUsersBtn').addEventListener('click', () => refreshAdminUsers());
  document.getElementById('createUserBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().CreateUser({
      tenant_id: els.tenantId.value.trim(),
      name: els.userName.value.trim(),
      email: els.userEmail.value.trim(),
    }), els.adminOutput, (value) => value.body || '');
    try {
      const parsed = JSON.parse(result.body || '{}');
      if (parsed.id) {
        els.userId.value = parsed.id;
        applyAdminIdentityFilters(els.tenantId.value.trim(), parsed.id);
        updateSelectedAdminUserCard();
      }
    } catch {
    }
  });
  document.getElementById('updateUserBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().UpdateUser(els.tenantId.value.trim(), els.userId.value.trim(), buildUserUpdatePayload()), els.adminOutput, (value) => value.body || '');
    try {
      const parsed = JSON.parse(result.body || '{}');
      if (parsed.id) {
        els.userName.value = parsed.name || els.userName.value;
        els.userEmail.value = parsed.email || els.userEmail.value;
        assignQuotaFields('user', parsed.quota || {});
      }
    } catch {
    }
    await refreshAdminUsers();
  });
  document.getElementById('copyIntegrationPackBtn').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(els.integrationPackEditor?.value || '');
      setOperationStatus('success', 'Integration Pack Copied', 'Markdown integration pack copied to clipboard.');
    } catch (error) {
      setOperationStatus('error', 'Copy Failed', error?.message || String(error));
    }
  });
  document.getElementById('listCredentialsBtn').addEventListener('click', () => refreshAdminCredentials());
  document.getElementById('createCredentialBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().CreateCredential({
      tenant_id: els.tenantId.value.trim(),
      user_id: els.userId.value.trim(),
      name: els.credentialName.value.trim(),
      api_key: els.newApiKey.value.trim(),
      api_secret: els.newApiSecret.value.trim(),
    }), els.adminOutput, (value) => value.body || '');
    syncCredentialFieldsFromResult(result.body || '');
    updateSelectedAdminCredentialCard();
  });

  document.getElementById('adminOverviewBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().GetAdminOverview(), els.adminOutput, (value) => value.body || '');
    renderAdminOverview(result?.body || '{}');
  });
  document.getElementById('adminDashboardBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().GetAdminDashboard(), els.adminOutput, (value) => value.body || '');
    renderAdminDashboard(result?.body || '{}');
  });
  document.getElementById('adminAlertsBtn').addEventListener('click', async () => {
    const limit = Number.parseInt((els.adminAlertLimit?.value || '').trim(), 10);
    const result = await safeCall(() => app().GetAdminAlerts({
      tenant_id: els.adminAlertTenantId?.value?.trim() || '',
      user_id: els.adminAlertUserId?.value?.trim() || '',
      kind: els.adminAlertKind?.value?.trim() || '',
      since: els.adminAlertSince?.value?.trim() || '',
      limit: Number.isFinite(limit) && limit > 0 ? limit : 0,
    }), els.adminOutput, (value) => value.body || '');
    renderAdminAlerts(result?.body || '{}');
  });
  document.getElementById('adminAuditBtn').addEventListener('click', async () => {
    const limit = Number.parseInt((els.adminAuditLimit?.value || '').trim(), 10);
    const result = await safeCall(() => app().ListAuditEvents({
      tenant_id: els.adminAlertTenantId?.value?.trim() || '',
      user_id: els.adminAlertUserId?.value?.trim() || '',
      action: els.adminAuditAction?.value?.trim() || '',
      resource_type: els.adminAuditResourceType?.value?.trim() || '',
      limit: Number.isFinite(limit) && limit > 0 ? limit : 0,
      before: els.adminAuditBefore?.value?.trim() || '',
    }), els.adminOutput, (value) => value.body || '');
    renderAuditEvents(result?.body || '{}');
  });
  document.getElementById('tenantSummaryBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().GetTenantSummary(els.tenantId.value.trim()), els.adminOutput, (value) => value.body || '');
    renderTenantSummary(result?.body || '{}');
  });

  ['tenantId', 'userId'].forEach((id) => {
    document.getElementById(id).addEventListener('input', () => {
      syncAdminIdentityFilters();
    });
  });

  if (els.adminTenantGrid) {
    els.adminTenantGrid.addEventListener('click', async (event) => {
      const pageAction = event.target.closest('[data-admin-tenant-page-action]')?.getAttribute('data-admin-tenant-page-action') || '';
      if (pageAction === 'reset') {
        await refreshAdminTenants({ reset: true });
        return;
      }
      if (pageAction === 'more' && registryPaging.adminTenants.nextBefore) {
        await refreshAdminTenants({ before: registryPaging.adminTenants.nextBefore });
        return;
      }
      const card = event.target.closest('[data-admin-tenant-id]');
      if (!card) {
        return;
      }
      const tenantID = card.getAttribute('data-admin-tenant-id') || '';
      if (!tenantID) {
        return;
      }
      els.tenantId.value = tenantID;
      els.tenantName.value = card.getAttribute('data-admin-tenant-name') || els.tenantName.value;
      if (els.tenantMaxInstances) els.tenantMaxInstances.value = card.getAttribute('data-admin-tenant-max-instances') || '';
      if (els.tenantMaxSessions) els.tenantMaxSessions.value = card.getAttribute('data-admin-tenant-max-sessions') || '';
      if (els.tenantMaxMessages) els.tenantMaxMessages.value = card.getAttribute('data-admin-tenant-max-messages') || '';
      if (els.tenantMaxRuns) els.tenantMaxRuns.value = card.getAttribute('data-admin-tenant-max-runs') || '';
      applyAdminIdentityFilters(tenantID, els.userId.value.trim());
      updateSelectedAdminTenantCard();
      const action = event.target.closest('[data-admin-tenant-action]')?.getAttribute('data-admin-tenant-action') || 'select';
      if (action === 'select') {
        const detail = await safeCall(() => app().GetTenant(tenantID), els.adminOutput, (value) => value.body || '');
        hydrateTenantFormFromBody(detail?.body || '{}');
        const summary = await safeCall(() => app().GetTenantSummary(tenantID), els.adminOutput, (value) => value.body || '');
        renderTenantSummary(summary?.body || '{}');
        updateSelectedAdminDashboardUserCard();
        return;
      }
      if (action === 'users') {
        registryPaging.adminUsers.before = '';
        await refreshAdminUsers({ reset: true });
        return;
      }
      if (action === 'summary') {
        const result = await safeCall(() => app().GetTenantSummary(tenantID), els.adminOutput, (value) => value.body || '');
        renderTenantSummary(result?.body || '{}');
        return;
      }
      if (action === 'enable' || action === 'disable') {
        const nextStatus = action === 'enable' ? 'active' : 'disabled';
        const result = await safeCall(() => app().UpdateTenant(tenantID, { status: nextStatus }), els.adminOutput, (value) => value.body || '');
        try {
          const parsed = JSON.parse(result.body || '{}');
          if (parsed.id) {
            els.tenantName.value = parsed.name || els.tenantName.value;
            assignQuotaFields('tenant', parsed.quota || {});
          }
        } catch {
        }
        await refreshAdminTenants();
        return;
      }
    });
  }

  if (els.adminUserGrid) {
    els.adminUserGrid.addEventListener('click', async (event) => {
      const pageAction = event.target.closest('[data-admin-user-page-action]')?.getAttribute('data-admin-user-page-action') || '';
      if (pageAction === 'reset') {
        await refreshAdminUsers({ reset: true });
        return;
      }
      if (pageAction === 'more' && registryPaging.adminUsers.nextBefore) {
        await refreshAdminUsers({ before: registryPaging.adminUsers.nextBefore });
        return;
      }
      const card = event.target.closest('[data-admin-user-id]');
      if (!card) {
        return;
      }
      const userID = card.getAttribute('data-admin-user-id') || '';
      if (!userID) {
        return;
      }
      els.userId.value = userID;
      els.userName.value = card.getAttribute('data-admin-user-name') || els.userName.value;
      els.userEmail.value = card.getAttribute('data-admin-user-email') || els.userEmail.value;
      if (els.userMaxInstances) els.userMaxInstances.value = card.getAttribute('data-admin-user-max-instances') || '';
      if (els.userMaxSessions) els.userMaxSessions.value = card.getAttribute('data-admin-user-max-sessions') || '';
      if (els.userMaxMessages) els.userMaxMessages.value = card.getAttribute('data-admin-user-max-messages') || '';
      if (els.userMaxRuns) els.userMaxRuns.value = card.getAttribute('data-admin-user-max-runs') || '';
      applyAdminIdentityFilters(els.tenantId.value.trim(), userID);
      updateSelectedAdminUserCard();
      const action = event.target.closest('[data-admin-user-list-action]')?.getAttribute('data-admin-user-list-action') || 'select';
      if (action === 'select') {
        const detail = await safeCall(() => app().GetUser(els.tenantId.value.trim(), userID), els.adminOutput, (value) => value.body || '');
        hydrateUserFormFromBody(detail?.body || '{}');
        const summary = await safeCall(() => app().GetTenantSummary(els.tenantId.value.trim()), els.adminOutput, (value) => value.body || '');
        renderTenantSummary(summary?.body || '{}');
        updateSelectedAdminDashboardUserCard();
        return;
      }
      if (action === 'credentials') {
        registryPaging.adminCredentials.before = '';
        await refreshAdminCredentials({ reset: true });
        return;
      }
      if (action === 'alerts') {
        const limit = parseOptionalPositiveInt(els.adminAlertLimit?.value || '');
        const result = await safeCall(() => app().GetAdminAlerts({
          tenant_id: els.tenantId.value.trim(),
          user_id: userID,
          kind: els.adminAlertKind?.value?.trim() || '',
          since: els.adminAlertSince?.value?.trim() || '',
          limit,
        }), els.adminOutput, (value) => value.body || '');
        renderAdminAlerts(result?.body || '{}');
        return;
      }
      if (action === 'enable' || action === 'disable') {
        const nextStatus = action === 'enable' ? 'active' : 'disabled';
        const result = await safeCall(() => app().UpdateUser(els.tenantId.value.trim(), userID, { status: nextStatus }), els.adminOutput, (value) => value.body || '');
        try {
          const parsed = JSON.parse(result.body || '{}');
          if (parsed.id) {
            els.userName.value = parsed.name || els.userName.value;
            els.userEmail.value = parsed.email || els.userEmail.value;
            assignQuotaFields('user', parsed.quota || {});
          }
        } catch {
        }
        await refreshAdminUsers();
        return;
      }
    });
  }

  if (els.adminCredentialGrid) {
    els.adminCredentialGrid.addEventListener('click', async (event) => {
      const pageAction = event.target.closest('[data-admin-credential-page-action]')?.getAttribute('data-admin-credential-page-action') || '';
      if (pageAction === 'reset') {
        await refreshAdminCredentials({ reset: true });
        return;
      }
      if (pageAction === 'more' && registryPaging.adminCredentials.nextBefore) {
        await refreshAdminCredentials({ before: registryPaging.adminCredentials.nextBefore });
        return;
      }
      const card = event.target.closest('[data-admin-credential-id]');
      if (!card) {
        return;
      }
      const credentialID = card.getAttribute('data-admin-credential-id') || '';
      const apiKey = card.getAttribute('data-admin-credential-key') || '';
      const action = event.target.closest('[data-admin-credential-action]')?.getAttribute('data-admin-credential-action') || 'select';
      if (action === 'revoke') {
        if (!credentialID) {
          return;
        }
        if (!window.confirm('Revoke credential ' + credentialID + '?')) {
          return;
        }
        await safeCall(() => app().RevokeCredential(els.tenantId.value.trim(), els.userId.value.trim(), credentialID), els.adminOutput, (value) => value.body || '');
        await refreshAdminCredentials();
        return;
      }
      if (apiKey) {
        els.newApiKey.value = apiKey;
        els.apiKey.value = apiKey;
      }
      updateSelectedAdminCredentialCard();
    });
  }

  if (els.adminDashboardGrid) {
    els.adminDashboardGrid.addEventListener('click', async (event) => {
      const card = event.target.closest('[data-admin-user-card="true"]');
      if (!card) {
        return;
      }
      const tenantID = card.getAttribute('data-tenant-id') || '';
      const userID = card.getAttribute('data-user-id') || '';
      const action = event.target.closest('[data-admin-user-action]')?.getAttribute('data-admin-user-action') || 'select';
      applyAdminIdentityFilters(tenantID, userID);
      els.tenantId.value = tenantID || els.tenantId.value;
      els.userId.value = userID || els.userId.value;
      if (action === 'alerts') {
        const limit = Number.parseInt((els.adminAlertLimit?.value || '').trim(), 10);
        const result = await safeCall(() => app().GetAdminAlerts({
          tenant_id: tenantID,
          user_id: userID,
          kind: els.adminAlertKind?.value?.trim() || '',
          since: els.adminAlertSince?.value?.trim() || '',
          limit: Number.isFinite(limit) && limit > 0 ? limit : 0,
        }), els.adminOutput, (value) => value.body || '');
        renderAdminAlerts(result?.body || '{}');
        return;
      }
      if (action === 'audit') {
        const limit = Number.parseInt((els.adminAuditLimit?.value || '').trim(), 10);
        const result = await safeCall(() => app().ListAuditEvents({
          tenant_id: tenantID,
          user_id: userID,
          action: els.adminAuditAction?.value?.trim() || '',
          resource_type: els.adminAuditResourceType?.value?.trim() || '',
          limit: Number.isFinite(limit) && limit > 0 ? limit : 0,
          before: els.adminAuditBefore?.value?.trim() || '',
        }), els.adminOutput, (value) => value.body || '');
        renderAuditEvents(result?.body || '{}');
      }
    });
  }

  document.getElementById('mcpKind').addEventListener('change', syncMCPKindUI);
  ['mcpAuthType', 'mcpEndpointUrl', 'mcpCommand', 'mcpArgs', 'mcpHeaders', 'mcpEnv', 'mcpName', 'mcpServerId', 'baseUrl', 'accessToken'].forEach((id) => {
    const eventName = id === 'mcpAuthType' ? 'change' : 'input';
    document.getElementById(id).addEventListener(eventName, () => {
      renderMCPGuide();
      renderMCPExamples();
      if (id === 'mcpServerId') {
        updateSelectedMCPCard();
      }
    });
  });
  ['mcpDisabled', 'mcpAutoStart'].forEach((id) => {
    document.getElementById(id).addEventListener('change', () => {
      renderMCPGuide();
      renderMCPExamples();
    });
  });
  ['skillName', 'skillSearchQuery', 'skillSources', 'skillTopN', 'skillHubUrl', 'skillMarketUrl', 'skillMarketEmail', 'skillInstallSource', 'skillRepoFullName', 'skillRepoUrl', 'skillRawUrl', 'skillFilePath', 'skillBranch', 'skillDefinitionType', 'skillID', 'skillGitHubToken', 'skillSubmissionId', 'skillArchiveName', 'skillZipBase64', 'baseUrl', 'accessToken', 'adminSecret', 'instanceId', 'sessionId'].forEach((id) => {
    document.getElementById(id).addEventListener('input', () => {
      renderSkillGuide();
      renderSkillExamples();
      renderPlaybooks();
      renderQuickGuide();
      renderIntegrationChecklist();
      renderDemoFlow();
      if (id === 'skillName') {
        updateSelectedSkillCard();
      }
    });
  });
  ['includeInstalledSkills', 'skillOverwrite'].forEach((id) => {
    document.getElementById(id).addEventListener('change', () => {
      renderSkillGuide();
      renderSkillExamples();
      renderPlaybooks();
      renderQuickGuide();
      renderIntegrationChecklist();
      renderDemoFlow();
    });
  });
  ['instanceId', 'sessionId', 'messageRoleFilter', 'messageSinceFilter', 'messageUntilFilter', 'runStatusFilter', 'runResponseSourceFilter', 'runWaitingFilter'].forEach((id) => {
    document.getElementById(id).addEventListener('input', () => {
      renderRuntimeFilterPills();
      if (id === 'sessionId') {
        updateSelectedSessionCard();
      }
      if (id === 'instanceId') {
        updateSelectedRunCard();
      }
    });
  });
  document.getElementById('includeArchivedSessions').addEventListener('change', () => {
    renderRuntimeFilterPills();
    renderIntegrationChecklist();
    renderDemoFlow();
    renderResponseSamples();
    renderIntegrationPack();
  });
  ['baseUrl', 'apiKey', 'apiSecret', 'accessToken', 'configEditor', 'messageInput'].forEach((id) => {
    document.getElementById(id).addEventListener('input', () => {
      renderQuickGuide();
      renderPlaybooks();
      renderIntegrationChecklist();
      renderDemoFlow();
      renderResponseSamples();
      renderIntegrationPack();
    });
  });
  if (els.demoFlowGrid) {
    els.demoFlowGrid.addEventListener('click', async (event) => {
      const action = event.target.closest('[data-flow-action]')?.getAttribute('data-flow-action') || '';
      if (!action) {
        return;
      }
      await runDemoFlowAction(action);
      renderIntegrationChecklist();
      renderDemoFlow();
      renderResponseSamples();
      renderIntegrationPack();
    });
  }
  document.getElementById('skillGitHubTemplateBtn').addEventListener('click', () => applySkillSourceTemplate('github'));
  document.getElementById('skillZipTemplateBtn').addEventListener('click', () => applySkillSourceTemplate('zip'));
  document.getElementById('skillHubTemplateBtn').addEventListener('click', () => applySkillSourceTemplate('skillhub'));
  document.getElementById('skillMarketTemplateBtn').addEventListener('click', () => applySkillSourceTemplate('skillmarket'));
  if (els.skillListGrid) {
    els.skillListGrid.addEventListener('click', async (event) => {
      const pageAction = event.target.closest('[data-skill-page-action]')?.getAttribute('data-skill-page-action') || '';
      if (pageAction === 'reset') {
        await refreshSkillRegistry({ reset: true });
        return;
      }
      if (pageAction === 'more' && registryPaging.skills.nextBefore) {
        await refreshSkillRegistry({ before: registryPaging.skills.nextBefore });
        return;
      }
      const card = event.target.closest('[data-skill-name]');
      if (!card) {
        return;
      }
      const name = card.getAttribute('data-skill-name') || '';
      if (!name) {
        return;
      }
      els.skillName.value = name;
      updateSelectedSkillCard();
      renderSkillGuide();
      renderSkillExamples();
      const action = event.target.closest('[data-skill-action]')?.getAttribute('data-skill-action') || 'get';
      if (action === 'validate') {
        await safeCall(() => app().ValidateSkill(name), els.skillOutput, (value) => value.body || '');
        return;
      }
      if (action === 'export') {
        await safeCall(() => app().ExportSkill(name), els.skillOutput, (value) => value.body || '');
        return;
      }
      if (action === 'delete') {
        if (!window.confirm(`Delete skill ${name}?`)) {
          return;
        }
        await safeCall(() => app().DeleteSkill(name), els.skillOutput, (value) => value.body || '');
        els.skillName.value = '';
        await refreshSkillRegistry();
        renderSkillGuide();
        renderSkillExamples();
        return;
      }
      await safeCall(() => app().GetSkill(name), els.skillOutput, (value) => value.body || '');
    });
  }
  if (els.skillSearchGrid) {
    els.skillSearchGrid.addEventListener('click', async (event) => {
      const card = event.target.closest('[data-search-index]');
      if (!card) {
        return;
      }
      const items = JSON.parse(els.skillSearchGrid.dataset.items || '[]');
      const index = Number(card.getAttribute('data-search-index') || '-1');
      const item = items[index];
      if (!item) {
        return;
      }
      const action = event.target.closest('[data-search-action]')?.getAttribute('data-search-action') || 'fill';
      applySkillSearchResult(item);
      if (action === 'install') {
        await safeCall(() => app().InstallSkill(currentSkillInstallPayload()), els.skillOutput, (value) => value.body || '');
        await refreshSkillRegistry();
      }
    });
  }
  document.getElementById('mcpFilesystemTemplateBtn').addEventListener('click', () => { fillFilesystemTemplate(); updateSelectedMCPCard(); });
  document.getElementById('mcpGitTemplateBtn').addEventListener('click', () => { fillGitTemplate(); updateSelectedMCPCard(); });
  document.getElementById('mcpSQLiteTemplateBtn').addEventListener('click', () => { fillSQLiteTemplate(); updateSelectedMCPCard(); });
  document.getElementById('mcpRemoteTemplateBtn').addEventListener('click', () => { fillRemoteTemplate(); updateSelectedMCPCard(); });
  if (els.mcpListGrid) {
    els.mcpListGrid.addEventListener('click', async (event) => {
      const pageAction = event.target.closest('[data-mcp-page-action]')?.getAttribute('data-mcp-page-action') || '';
      if (pageAction === 'reset') {
        await refreshMCPRegistry({ reset: true });
        return;
      }
      if (pageAction === 'more' && registryPaging.mcp.nextBefore) {
        await refreshMCPRegistry({ before: registryPaging.mcp.nextBefore });
        return;
      }
      const card = event.target.closest('[data-server-id]');
      if (!card) {
        return;
      }
      const serverID = card.getAttribute('data-server-id') || '';
      if (!serverID) {
        return;
      }
      els.mcpServerId.value = serverID;
      updateSelectedMCPCard();
      const action = event.target.closest('[data-mcp-action]')?.getAttribute('data-mcp-action') || 'get';
      if (action === 'tools') {
        const result = await safeCall(() => app().GetMCPServerTools(serverID), els.mcpOutput, (value) => value.body || '');
        try {
          const parsed = JSON.parse(result.body || '{}');
          renderMCPTools(parsed.items || []);
        } catch {
          setMCPToolsEmpty('Tool response was not valid JSON.');
        }
        return;
      }
      if (action === 'health') {
        const result = await safeCall(() => app().CheckMCPServer(serverID), els.mcpOutput, (value) => value.body || '');
        syncMCPFormFromBody(result.body || '{}');
        return;
      }
      if (action === 'start') {
        const result = await safeCall(() => app().StartMCPServer(serverID), els.mcpOutput, (value) => value.body || '');
        syncMCPFormFromBody(result.body || '{}');
        return;
      }
      if (action === 'stop') {
        const result = await safeCall(() => app().StopMCPServer(serverID), els.mcpOutput, (value) => value.body || '');
        syncMCPFormFromBody(result.body || '{}');
        return;
      }
      const result = await safeCall(() => app().GetMCPServer(serverID), els.mcpOutput, (value) => value.body || '');
      syncMCPFormFromBody(result.body || '{}');
    });
  }
  document.getElementById('listMcpBtn').addEventListener('click', () => refreshMCPRegistry({ reset: true }));
  document.getElementById('createMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().CreateMCPServer(currentMCPCreatePayload()), els.mcpOutput, (value) => value.body || '');
    syncMCPFormFromBody(result.body || '{}');
    await refreshMCPRegistry();
  });
  document.getElementById('getMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().GetMCPServer(els.mcpServerId.value.trim()), els.mcpOutput, (value) => value.body || '');
    syncMCPFormFromBody(result.body || '{}');
  });
  document.getElementById('updateMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().UpdateMCPServer(els.mcpServerId.value.trim(), currentMCPUpdatePayload()), els.mcpOutput, (value) => value.body || '');
    syncMCPFormFromBody(result.body || '{}');
    await refreshMCPRegistry();
  });
  document.getElementById('startMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().StartMCPServer(els.mcpServerId.value.trim()), els.mcpOutput, (value) => value.body || '');
    syncMCPFormFromBody(result.body || '{}');
    await refreshMCPRegistry();
  });
  document.getElementById('stopMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().StopMCPServer(els.mcpServerId.value.trim()), els.mcpOutput, (value) => value.body || '');
    syncMCPFormFromBody(result.body || '{}');
    await refreshMCPRegistry();
  });
  document.getElementById('checkMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().CheckMCPServer(els.mcpServerId.value.trim()), els.mcpOutput, (value) => value.body || '');
    syncMCPFormFromBody(result.body || '{}');
    await refreshMCPRegistry();
  });
  document.getElementById('toolsMcpBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().GetMCPServerTools(els.mcpServerId.value.trim()), els.mcpOutput, (value) => value.body || '');
    try {
      const parsed = JSON.parse(result.body || '{}');
      renderMCPTools(parsed.items || []);
    } catch {
      setMCPToolsEmpty('Tool response was not valid JSON.');
    }
  });
  document.getElementById('deleteMcpBtn').addEventListener('click', async () => {
    const serverID = els.mcpServerId.value.trim();
    if (!serverID) {
      renderOutput(els.mcpOutput, 'server_id is required', true);
      return;
    }
    if (!window.confirm(`Delete MCP server ${serverID}?`)) {
      return;
    }
    await safeCall(() => app().DeleteMCPServer(serverID), els.mcpOutput, (value) => value.body || '');
    els.mcpServerId.value = '';
    updateSelectedMCPCard();
    setMCPToolsEmpty('MCP server deleted.');
    await refreshMCPRegistry();
  });

  document.getElementById('listSkillsBtn').addEventListener('click', () => refreshSkillRegistry({ reset: true }));
  document.getElementById('getSkillBtn').addEventListener('click', () => safeCall(() => app().GetSkill(els.skillName.value.trim()), els.skillOutput, (value) => value.body || ''));
  document.getElementById('searchSkillsBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().SearchSkills(currentSkillSearchPayload()), els.skillOutput, (value) => value.body || '');
    try {
      const parsed = JSON.parse(result.body || '{}');
      renderSkillSearchResults(parsed.items || parsed);
    } catch {
      setSkillSearchEmpty('Skill search response was not valid JSON.');
    }
  });
  document.getElementById('installSkillBtn').addEventListener('click', async () => {
    await safeCall(() => app().InstallSkill(currentSkillInstallPayload()), els.skillOutput, (value) => value.body || '');
    await refreshSkillRegistry();
  });
  document.getElementById('importSkillBtn').addEventListener('click', async () => {
    await safeCall(() => app().ImportSkill(currentSkillImportPayload()), els.skillOutput, (value) => value.body || '');
    await refreshSkillRegistry();
  });
  document.getElementById('exportSkillBtn').addEventListener('click', () => safeCall(() => app().ExportSkill(els.skillName.value.trim()), els.skillOutput, (value) => value.body || ''));
  document.getElementById('validateSkillBtn').addEventListener('click', () => safeCall(() => app().ValidateSkill(els.skillName.value.trim()), els.skillOutput, (value) => value.body || ''));
  document.getElementById('improveSkillBtn').addEventListener('click', () => safeCall(() => app().ImproveSkill(els.skillName.value.trim(), true), els.skillOutput, (value) => value.body || ''));
  document.getElementById('deleteSkillBtn').addEventListener('click', async () => {
    const name = els.skillName.value.trim();
    if (!name) {
      renderOutput(els.skillOutput, 'skill_name is required', true);
      return;
    }
    if (!window.confirm(`Delete skill ${name}?`)) {
      return;
    }
    await safeCall(() => app().DeleteSkill(name), els.skillOutput, (value) => value.body || '');
  });
  document.getElementById('getSkillUploadStatusBtn').addEventListener('click', () => safeCall(() => app().GetSkillUploadStatus(els.skillSubmissionId.value.trim(), els.skillMarketUrl.value.trim()), els.skillOutput, (value) => value.body || ''));
  document.getElementById('getSkillAccountBtn').addEventListener('click', () => safeCall(() => app().GetSkillMarketAccount(els.skillMarketEmail.value.trim(), els.skillMarketUrl.value.trim()), els.skillOutput, (value) => value.body || ''));
  document.getElementById('uploadSkillBtn').addEventListener('click', () => safeCall(() => app().UploadSkill(els.skillName.value.trim(), {
    skill_market_url: els.skillMarketUrl.value.trim(),
    email: els.skillMarketEmail.value.trim(),
  }), els.skillOutput, (value) => value.body || ''));

  document.getElementById('schemaBtn').addEventListener('click', () => safeCall(() => app().GetConfigSchema(), els.configOutput, (value) => value.body || ''));
  document.getElementById('loadConfigBtn').addEventListener('click', loadConfig);
  document.getElementById('validateConfigBtn').addEventListener('click', () => safeCall(() => app().ValidateConfig(els.configEditor.value), els.configOutput, (value) => value.body || ''));
  document.getElementById('testConfigBtn').addEventListener('click', () => safeCall(() => app().TestConfig(els.configEditor.value), els.configOutput, (value) => value.body || ''));
  document.getElementById('saveConfigBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().UpdateConfig(els.configEditor.value), els.configOutput, (value) => value.body || '');
    if (result?.body) {
      els.configEditor.value = result.body;
    }
  });

  document.getElementById('quickStartBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().QuickStartDemo({
      config_json: els.configEditor.value,
      instance_name: els.instanceName.value.trim(),
      instance_description: els.instanceDesc.value.trim(),
      message_title: els.messageTitle.value.trim(),
      message_content: els.messageInput.value,
    }), els.instanceOutput, (value) => JSON.stringify(value, null, 2));
    if (result.instance_id) {
      els.instanceId.value = result.instance_id;
    }
    if (result.session_id) {
      els.sessionId.value = result.session_id;
    }
    if (result.run_id) {
      els.runId.value = result.run_id;
    }
    if (result.instance_id) {
      await refreshInstanceRegistry();
      await loadInstanceDetail(result.instance_id);
      await loadCapabilities();
      await loadInstanceSummary();
    }
  });
  document.getElementById('refreshConversationBtn').addEventListener('click', () => safeCall(() => app().RefreshConversation({
    instance_id: els.instanceId.value.trim(),
    session_id: els.sessionId.value.trim(),
    run_id: els.runId.value.trim(),
  }), els.instanceOutput, (value) => JSON.stringify(value, null, 2)));
  document.getElementById('usageBtn').addEventListener('click', () => safeCall(() => app().GetUsageSummary(), els.instanceOutput, (value) => value.body || ''));
  document.getElementById('listInstancesBtn').addEventListener('click', refreshInstanceRegistry);
  document.getElementById('createInstanceBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().CreateInstance({
      name: els.instanceName.value.trim(),
      description: els.instanceDesc.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    syncInstanceIdFromBody(result.body || '');
    renderInstanceDetail(result.body || '');
    await refreshInstanceRegistry();
    if (els.instanceId.value.trim()) {
      await loadCapabilities();
      await loadInstanceSummary();
    }
  });
  document.getElementById('getInstanceBtn').addEventListener('click', async () => {
    await loadInstanceDetail(els.instanceId.value.trim());
    await loadCapabilities();
    await loadInstanceSummary();
  });
  document.getElementById('updateInstanceBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().UpdateInstance({
      instance_id: els.instanceId.value.trim(),
      name: els.instanceName.value.trim(),
      description: els.instanceDesc.value.trim(),
      metadata_json: els.instanceMetadata.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    syncInstanceIdFromBody(result.body || '');
    renderInstanceDetail(result.body || '');
    await refreshInstanceRegistry();
    await loadInstanceSummary();
  });
  document.getElementById('getSummaryBtn').addEventListener('click', loadInstanceSummary);
  document.getElementById('getCapabilitiesBtn').addEventListener('click', loadCapabilities);
  document.getElementById('sessionsBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
    renderSessionList(result?.body || '');
  });
  document.getElementById('getSessionBtn').addEventListener('click', () => safeCall(() => app().GetSession(els.instanceId.value.trim(), els.sessionId.value.trim()), els.instanceOutput, (value) => value.body || ''));
  document.getElementById('archiveSessionBtn').addEventListener('click', async () => {
    await safeCall(() => app().ArchiveSession({
      instance_id: els.instanceId.value.trim(),
      session_id: els.sessionId.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    if (els.instanceId.value.trim()) {
      const result = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
      renderSessionList(result?.body || '');
    }
  });
  document.getElementById('restoreSessionBtn').addEventListener('click', async () => {
    await safeCall(() => app().RestoreSession({
      instance_id: els.instanceId.value.trim(),
      session_id: els.sessionId.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    if (els.instanceId.value.trim()) {
      const result = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
      renderSessionList(result?.body || '');
    }
  });
  document.getElementById('messagesBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().ListMessages({
      instance_id: els.instanceId.value.trim(),
      session_id: els.sessionId.value.trim(),
      role: els.messageRoleFilter.value.trim(),
      since: els.messageSinceFilter.value.trim(),
      until: els.messageUntilFilter.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    renderMessageTimeline(result?.body || '');
  });
  document.getElementById('runsBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().ListRuns({
      instance_id: els.instanceId.value.trim(),
      status: els.runStatusFilter.value.trim(),
      session_id: els.sessionId.value.trim(),
      response_source: els.runResponseSourceFilter.value.trim(),
      waiting_for_user: els.runWaitingFilter.value.trim(),
    }), els.instanceOutput, (value) => value.body || '');
    renderRunList(result?.body || '');
  });
  document.getElementById('getRunBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().GetRun(els.instanceId.value.trim(), els.runId.value.trim()), els.instanceOutput, (value) => value.body || '');
    try {
      renderRunList(JSON.stringify([JSON.parse(result?.body || '{}')]));
    } catch {
      setRunListEmpty('Run detail response was not valid JSON.');
    }
  });
  document.getElementById('sendBtn').addEventListener('click', async () => {
    const result = await safeCall(() => app().SendMessage({
      instance_id: els.instanceId.value.trim(),
      session_id: els.sessionId.value.trim(),
      title: els.messageTitle.value.trim(),
      content: els.messageInput.value,
    }), els.instanceOutput, (value) => value.body || '');
    syncMessageResult(result.body || '');
    if (els.instanceId.value.trim()) {
      const sessions = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
      renderSessionList(sessions?.body || '');
      const runs = await safeCall(() => app().ListRuns({
        instance_id: els.instanceId.value.trim(),
        status: els.runStatusFilter.value.trim(),
        session_id: els.sessionId.value.trim(),
        response_source: els.runResponseSourceFilter.value.trim(),
        waiting_for_user: els.runWaitingFilter.value.trim(),
      }), els.instanceOutput, (value) => value.body || '');
      renderRunList(runs?.body || '');
      const messages = await safeCall(() => app().ListMessages({
        instance_id: els.instanceId.value.trim(),
        session_id: els.sessionId.value.trim(),
        role: els.messageRoleFilter.value.trim(),
        since: els.messageSinceFilter.value.trim(),
        until: els.messageUntilFilter.value.trim(),
      }), els.instanceOutput, (value) => value.body || '');
      renderMessageTimeline(messages?.body || '');
    }
  });

  if (els.instanceListGrid) {
    els.instanceListGrid.addEventListener('click', async (event) => {
      const card = event.target.closest('[data-instance-id]');
      if (!card) {
        return;
      }
      const instanceID = card.getAttribute('data-instance-id') || '';
      if (!instanceID) {
        return;
      }
      els.instanceId.value = instanceID;
      updateSelectedInstanceCard();
      renderRuntimeFilterPills();
      const action = event.target.closest('[data-instance-action]')?.getAttribute('data-instance-action') || 'select';
      await loadInstanceDetail(instanceID);
      if (action === 'summary') {
        await loadInstanceSummary();
        return;
      }
      if (action === 'capabilities') {
        await loadCapabilities();
        return;
      }
      await loadCapabilities();
      await loadInstanceSummary();
    });
  }

  if (els.sessionListGrid) {
    els.sessionListGrid.addEventListener('click', async (event) => {
      const card = event.target.closest('[data-session-id]');
      if (!card) {
        return;
      }
      const sessionID = card.getAttribute('data-session-id') || '';
      if (!sessionID) {
        return;
      }
      els.sessionId.value = sessionID;
      updateSelectedSessionCard();
      const action = event.target.closest('[data-session-action]')?.getAttribute('data-session-action') || 'select';
      if (action === 'archive') {
        await safeCall(() => app().ArchiveSession({
          instance_id: els.instanceId.value.trim(),
          session_id: sessionID,
        }), els.instanceOutput, (value) => value.body || '');
        const result = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
        renderSessionList(result?.body || '');
        return;
      }
      if (action === 'restore') {
        await safeCall(() => app().RestoreSession({
          instance_id: els.instanceId.value.trim(),
          session_id: sessionID,
        }), els.instanceOutput, (value) => value.body || '');
        const result = await safeCall(() => app().ListSessions(els.instanceId.value.trim(), Boolean(els.includeArchivedSessions?.checked)), els.instanceOutput, (value) => value.body || '');
        renderSessionList(result?.body || '');
        return;
      }
      await safeCall(() => app().GetSession(els.instanceId.value.trim(), sessionID), els.instanceOutput, (value) => value.body || '');
    });
  }

  if (els.runListGrid) {
    els.runListGrid.addEventListener('click', async (event) => {
      const card = event.target.closest('[data-run-id]');
      if (!card) {
        return;
      }
      const runID = card.getAttribute('data-run-id') || '';
      const sessionID = card.getAttribute('data-session-id') || '';
      if (!runID) {
        return;
      }
      els.runId.value = runID;
      if (sessionID) {
        els.sessionId.value = sessionID;
        updateSelectedSessionCard();
      }
      updateSelectedRunCard();
      await safeCall(() => app().GetRun(els.instanceId.value.trim(), runID), els.instanceOutput, (value) => value.body || '');
    });
  }

  setAdminOverviewEmpty('Load admin overview to inspect total tenants, users, instances, runs, and audit events.');
  setAdminDashboardEmpty('Load dashboard to inspect recent audit events and 24h/7d activity buckets.');
  setAdminAlertsEmpty('Load alerts to inspect unready instances, waiting runs, and failed runs.');
  setAdminAuditEmpty('Load audit events to inspect tenant, user, credential, config, instance, and run operations.');
  setAdminTenantListEmpty('Load tenants to browse the admin tenant registry.');
  setAdminUserListEmpty('Select a tenant, then load users to inspect tenant membership.');
  setAdminCredentialListEmpty('Select a tenant and user, then load credentials to inspect access keys.');
}

window.addEventListener('DOMContentLoaded', async () => {
  bind();
  await loadSettings();
});


















































