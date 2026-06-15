/*
 * Validate admin module structure.
 * Run with: node hub/web/admin/validate-admin-modules.js
 */
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const root = __dirname;
const indexPath = path.join(root, 'index.html');
const expectedScripts = [
  'admin.js',
  'admin-tabs.js',
  'tenant-tab.js',
  'admin-ui.js',
  'center-tab.js',
  'governance-tab.js',
  'marketplace-tab.js',
  'security-tab.js',
  'machines-tab.js',
  've-tab.js',
  'im-tab.js',
  'hub-llm-tab.js',
  'feishu-tab.js',
  'invitation-tab.js',
  'pwa-tab.js',
  'system-tab.js',
  'compute-tab.js',
  'llm-provider-tab.js',
  'llm-service-tabs.js',
  'card-store-tab.js',
  'usage-stats-tab.js',
  'failure-logs-tab.js',
  'admin-module-health.js',
  'admin-bootstrap.js'
];
const removedLegacyFiles = [
  'llmproviders.js',
  'usagestats.js',
  'admin-check.js',
  'hub-admin-check.js',
  '_extra.js'
];
const expectedExports = {
  'machines-tab.js': ['renderMachineList', 'loadMachines'],
  'security-tab.js': ['loadSecurityTab', 'selectSecGroup', 'confirmAssignUsers'],
  'llm-provider-tab.js': ['loadLlmProviders', 'openLlmProviderTab', 'saveLLMProviders'],
  'llm-service-tabs.js': ['loadLlmServiceGroups', 'openLlmServiceGroupTab', 'saveLLMServiceAdmin'],
  'usage-stats-tab.js': ['loadUsageStats']
};

function fail(message) {
  console.error('VALIDATION FAILED:', message);
  process.exitCode = 1;
}

function read(name) {
  return fs.readFileSync(path.join(root, name), 'utf8');
}

function assertAscii(name) {
  const content = read(name);
  for (let i = 0; i < content.length; i += 1) {
    if (content.charCodeAt(i) > 127) {
      fail(name + ' contains non-ASCII character at offset ' + i + '.');
      return;
    }
  }
}

function assertExists(name) {
  const full = path.join(root, name);
  if (!fs.existsSync(full)) {
    fail('Missing file: ' + name);
  }
}

function assertJavaScriptSyntax(name) {
  if (!name.endsWith('.js')) {
    return;
  }
  const content = read(name);
  try {
    new vm.Script(content, { filename: name });
  } catch (err) {
    fail(name + ' has invalid JavaScript syntax: ' + err.message);
  }
}

function assertMissing(name) {
  const full = path.join(root, name);
  if (fs.existsSync(full)) {
    fail('Legacy file should stay deleted: ' + name);
  }
}

function assertScriptOrder() {
  const html = fs.readFileSync(indexPath, 'utf8');
  if (html.includes('/admin/js/')) {
    fail('index.html must not reference legacy /admin/js/ assets.');
  }
  let lastIndex = -1;
  expectedScripts.forEach(function(name) {
    const needle = 'src="/admin/' + name + '"';
    const idx = html.indexOf(needle);
    if (idx === -1) {
      fail('index.html is missing script tag for ' + name);
      return;
    }
    if (idx < lastIndex) {
      fail('index.html script order is wrong around ' + name);
      return;
    }
    lastIndex = idx;
  });
}

function assertHealthHook() {
  const content = read('admin-module-health.js');
  if (!content.includes('runAdminModuleHealthCheck')) {
    fail('admin-module-health.js should export runAdminModuleHealthCheck.');
  }
}

function assertGlobalAdminRuntimeHooks() {
  const center = read('center-tab.js');
  ['centerGlobalScoped', '_stopCenterPoll'].forEach(function(marker) {
    if (!center.includes(marker)) {
      fail('center-tab.js is missing global-only center marker: ' + marker);
    }
  });
  const services = read('llm-service-tabs.js');
  ['llmProviderModelRuntimeCard', 'ensureLLMProviderRuntimeUI', 'model_download/status', 'llmServiceGlobalScoped', 'llmServiceGlobalScoped() ? loadLLMServiceModelRuntime'].forEach(function(marker) {
    if (!services.includes(marker)) {
      fail('llm-service-tabs.js is missing global runtime marker: ' + marker);
    }
  });
}

function assertTenantAdminUIHooks() {
  const html = fs.readFileSync(indexPath, 'utf8');
  [
    'id="loginTenant"',
    'data-tab="tenants"',
    'data-tab="hubllm"',
    'id="tab-tenants"',
    'id="tab-hubllm"',
    'id="tenantCreatePanel"',
    'id="currentTenantChip"',
    'id="currentTenantName"',
    'src="/admin/tenant-tab.js"'
  ].forEach(function(marker) {
    if (!html.includes(marker)) {
      fail('index.html is missing tenant admin UI marker: ' + marker);
    }
  });
  const chipIndex = html.indexOf('id="currentTenantChip"');
  const topbarIndex = html.indexOf('class="topbar"');
  const adminChipIndex = html.indexOf('class="admin-chip"');
  const brandIndex = html.indexOf('class="brand"');
  const navIndex = html.indexOf('<nav class="nav"');
  if (chipIndex < topbarIndex || chipIndex > adminChipIndex || (brandIndex >= 0 && navIndex > brandIndex && chipIndex > brandIndex && chipIndex < navIndex)) {
    fail('index.html must place the tenant context chip in the right topbar before the admin chip.');
  }
  const admin = read('admin.js');
  ['window.tr = tr', 'window.tabMeta = tabMeta', 'Object.assign(window', 'adminProfile', 'refreshTenantChip', 'updateCurrentTenantContext', 'setOutput', 'showToast', 'openTab'].forEach(function(marker) {
    if (!admin.includes(marker)) {
      fail('admin.js must expose shared admin runtime marker: ' + marker);
    }
  });
  if (!admin.includes("tenant: document.getElementById('loginTenant')")) {
    fail('admin.js login payload must include selected tenant scope.');
  }
  if (!admin.includes('previous.tenant_name') || !admin.includes('previous.tenant_slug')) {
    fail('admin.js must preserve tenant display context when refreshing admin profile tokens.');
  }
  const system = read('system-tab.js');
  if (!system.includes('setAdminProfile(data.admin)') || system.includes('localStorage.setItem(adminProfileKey, JSON.stringify(data.admin))')) {
    fail('system-tab.js must preserve tenant display context when password/profile updates return a fresh admin token.');
  }
  ['adminTabAllowed', 'window.adminGlobalOnlyTabs', 'window.adminTenantOnlyTabs', "return 'tenants'"].forEach(function(marker) {
    if (!admin.includes(marker)) {
      fail('admin.js must block programmatic cross-scope tab opens: ' + marker);
    }
  });
  if (!admin.includes("normalized === 'system'") || !admin.includes("String(profile.scope || '').toLowerCase() === 'tenant'") || !admin.includes('openDefaultImSub')) {
    fail('admin.js must avoid global-only system loads for tenant admins.');
  }
  ['id="mailConfigCard"', 'id="tenantMailSenderCard"', 'tenantMailFromName'].forEach(function(marker) {
    if (!html.includes(marker)) {
      fail('index.html is missing tenant-safe mail settings marker: ' + marker);
    }
  });
  ['loadTenantMailSenderName', 'saveTenantMailSenderName', 'TENANT_MAIL_SENDER_MAX_RUNES', 'normalizeTenantMailSenderName', '/api/admin/mail/sender-name'].forEach(function(marker) {
    if (!system.includes(marker)) {
      fail('system-tab.js is missing tenant sender-name marker: ' + marker);
    }
  });
  const bootstrap = read('admin-bootstrap.js');
  ['Promise.allSettled', 'loadTenants', 'loadCenterStatus', 'loadMailConfig', 'loadLlmProviders', 'loadLlmServiceGroups', 'loadUsageStats', 'loadFailureLogs'].forEach(function(marker) {
    if (!bootstrap.includes(marker)) {
      fail('admin-bootstrap.js is missing scoped refresh marker: ' + marker);
    }
  });
  const im = read('im-tab.js');
  ['applyImScopeUI', 'openDefaultImSub', 'IM_SUBS', 'contentaudit: true', 'tenantRuntimeReloadMessage', 'runtime_reload_error'].forEach(function(marker) {
    if (!im.includes(marker)) {
      fail('im-tab.js is missing scoped IM marker: ' + marker);
    }
  });
  if (/HUB_LLM_PANE_I18N|loadHubLlmConfig|loadHubLlmStatus|PromptCache/i.test(im)) {
    fail('im-tab.js must not own Hub LLM or prompt-cache logic. Keep it in hub-llm-tab.js.');
  }
  const imSection = html.slice(html.indexOf('id="tab-im"'), html.indexOf('id="tab-hubllm"'));
  ['imSubHubLlm', 'hubLlmPromptCache', 'hubLlmCacheConfig'].forEach(function(marker) {
    if (imSection.includes(marker)) {
      fail('index.html IM section still contains Hub LLM marker: ' + marker);
    }
  });
  const hubllm = read('hub-llm-tab.js');
  ['registerTab', "id: 'hubllm'", 'navHubLlm', 'loadHubLlmConfig', 'loadHubLlmStatus'].forEach(function(marker) {
    if (!hubllm.includes(marker)) {
      fail('hub-llm-tab.js is missing standalone Hub LLM marker: ' + marker);
    }
  });
  const tenant = read('tenant-tab.js');
  [
    'loadLoginTenants',
    'renderLoginTenantOptions',
    'applyAdminScopeUI',
    'createTenantAdmin',
    'updateTenantStatus',
    'deleteTenant',
    'isReservedTenantID',
    'tenant_default',
    'applyImScopeUI',
    'tenantMailSenderCard',
    "toggleNearest('machineCountHero'"
  ].forEach(function(marker) {
    if (!tenant.includes(marker)) {
      fail('tenant-tab.js is missing marker: ' + marker);
    }
  });
  const health = read('admin-module-health.js');
  ['TenantTab', 'loadTenants', 'createTenantAdmin', 'loadLoginTenants'].forEach(function(marker) {
    if (!health.includes(marker)) {
      fail('admin-module-health.js is missing tenant health marker: ' + marker);
    }
  });
}

function assertEmptyTextNodesAreOwned() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const scripts = expectedScripts.map(function(name) { return read(name); }).join('\n');
  const allowedDynamic = {
    setupGateList: true,
    hubLlmTestResult: true
  };
  const emptyNode = /<([a-z0-9]+)\b([^>]*\bid="([^"]+)"[^>]*)>\s*<\/\1>/gi;
  let match;
  while ((match = emptyNode.exec(html))) {
    const attrs = match[2] || '';
    const id = match[3] || '';
    if (!id || allowedDynamic[id]) continue;
    if (attrs.includes('data-i18n=')) continue;
    if (scripts.includes(id)) continue;
    fail('index.html has empty text node without i18n or JS owner: #' + id);
  }
  if (html.includes('title=""')) {
    fail('index.html contains an empty title attribute; visible controls need readable labels/tooltips.');
  }
  if (/<span>\s*<\/span>/i.test(html)) {
    fail('index.html contains a blank span; visible controls need readable text or a JS-owned id.');
  }
}

function assertBlankPlaceholdersAreOwned() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const scripts = expectedScripts.map(function(name) { return read(name); }).join('\n');
  const allowedDynamic = {
    llmEndpointAccessLogFilterProvider: true,
    llmEndpointAccessLogFilterUpstreamHost: true,
    llmEndpointAccessLogFilterClientIP: true,
    llmEndpointAccessLogFilterEmail: true,
    llmEndpointAccessLogFilterKeyword: true,
    llmServiceGroupModels: true
  };
  const blankPlaceholder = /<([a-z0-9]+)\b([^>]*\bid="([^"]+)"[^>]*)\bplaceholder=""/gi;
  let match;
  while ((match = blankPlaceholder.exec(html))) {
    const id = match[3] || '';
    if (!id || allowedDynamic[id]) continue;
    const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const ownerPatterns = [
      new RegExp("_s\\('" + escaped + "',\\s*'placeholder'"),
      new RegExp('_s\\("' + escaped + '",\\s*"placeholder"'),
      new RegExp('getElementById\\([\'\"]' + escaped + '[\'\"]\\)[^;\\n]*\\.placeholder'),
      new RegExp(escaped + '[\'\"][^\n]{0,160}placeholder')
    ];
    if (!ownerPatterns.some(function(pattern) { return pattern.test(scripts); })) {
      fail('index.html has blank placeholder without JS owner: #' + id);
    }
  }
}

function assertBlankControlsAreOwned() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const scripts = expectedScripts.map(function(name) { return read(name); }).join('\n');
  const blankControl = /<(button|label)\b([^>]*\bid="([^"]+)"[^>]*)>\s*<\/\1>/gi;
  let match;
  while ((match = blankControl.exec(html))) {
    const attrs = match[2] || '';
    const id = match[3] || '';
    if (!id || attrs.includes('data-i18n=')) continue;
    if (scripts.includes(id)) continue;
    const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const ownerPatterns = [
      new RegExp("_s\\('" + escaped + "',\\s*'textContent'"),
      new RegExp('_s\\("' + escaped + '",\\s*"textContent"'),
      new RegExp('getElementById\\([\'\"]' + escaped + '[\'\"]\\)[^;\\n]*\\.textContent'),
      new RegExp('setText\\([\'\"]' + escaped + '[\'\"]')
    ];
    if (!ownerPatterns.some(function(pattern) { return pattern.test(scripts); })) {
      fail('index.html has blank control without text owner: #' + id);
    }
  }
}

function assertDataI18nKeysHaveTranslations() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const scripts = expectedScripts.map(function(name) { return read(name); }).join('\n');
  const keyPattern = /\bdata-i18n="([^"]+)"/g;
  let match;
  while ((match = keyPattern.exec(html))) {
    const key = match[1];
    const objectKey = new RegExp('(?:^|[\\s,{])' + key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*:');
    const assignedKey = new RegExp('\\.' + key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*=');
    if (!objectKey.test(scripts) && !assignedKey.test(scripts)) {
      fail('index.html data-i18n key has no visible translation owner: ' + key);
    }
  }
}

function assertScopedRefreshHooks() {
  [['llm-provider-tab.js', 'llmProviderTenantScopedRefresh'], ['usage-stats-tab.js', 'usageStatsTenantScoped']].forEach(function(entry) {
    const content = read(entry[0]);
    if (!content.includes(entry[1])) {
      fail(entry[0] + ' is missing scoped refresh marker: ' + entry[1]);
    }
  });
  [
    ['governance-tab.js', 'loadInvites'],
    ['marketplace-tab.js', 'loadMarketplace'],
    ['failure-logs-tab.js', 'loadFailureLogs'],
    ['llm-provider-tab.js', 'loadLlmProviders']
  ].forEach(function(entry) {
    const content = read(entry[0]);
    if (/refreshAll\s*=\s*async/.test(content)) {
      fail(entry[0] + ' must not wrap refreshAll; admin-bootstrap owns scoped refresh for ' + entry[1] + '.');
    }
  });
  const llmService = read('llm-service-tabs.js');
  ['llmServiceTenantScoped', 'if (llmServiceTenantScoped()) { applyLLMServiceSystemI18n(); ensureLLMServiceSystemSettingsLoaded(); }'].forEach(function(marker) {
    if (!llmService.includes(marker)) {
      fail('llm-service-tabs.js is missing tenant-scoped system settings marker: ' + marker);
    }
  });
  const html = fs.readFileSync(indexPath, 'utf8');
  const providerConfigIndex = html.indexOf('id="llmProviderConfigSection"');
  const modelServicesIndex = html.indexOf('id="tab-modelservices"');
  const serviceCardsIndex = html.indexOf('id="tab-servicecards"');
  if (providerConfigIndex < 0 || modelServicesIndex < 0 || providerConfigIndex < modelServicesIndex || (serviceCardsIndex > 0 && providerConfigIndex > serviceCardsIndex)) {
    fail('index.html must place LLM provider configuration inside the Model Services tab.');
  }
  const llmProvidersSection = html.slice(html.indexOf('id="tab-llmproviders"'), modelServicesIndex);
  if (llmProvidersSection.includes('id="llmProviderList"')) {
    fail('index.html must keep provider list out of the LLM EndPoint tab.');
  }
  if (!llmService.includes("id: 'modelservices'") || !llmService.includes('window.loadLlmProviders')) {
    fail('llm-service-tabs.js must load provider configuration when Model Services opens.');
  }
  ['window.refreshLlmServiceProviderOptions', 'window.getLlmProviderOptions', 'await refreshLLMServiceProviderOptions()'].forEach(function(marker) {
    if (!llmService.includes(marker) && !read('llm-provider-tab.js').includes(marker)) {
      fail('LLM service/provider tabs must keep provider options in sync: ' + marker);
    }
  });
  const pwa = read('pwa-tab.js');
  const legacyPendingLoginsPath = '/api/' + 'admin/' + 'enrollments/' + 'pending-logins';
  if (!pwa.includes('/api/admin/pending-logins') || pwa.includes(legacyPendingLoginsPath)) {
    fail('pwa-tab.js must use the registered pending-logins admin endpoint.');
  }
}

function assertAdminApiRoutesRegistered() {
  const routerPath = path.join(root, '..', '..', 'internal', 'httpapi', 'router.go');
  const router = fs.readFileSync(routerPath, 'utf8');
  const routes = [];
  const routePattern = /mux\.HandleFunc\("(GET|POST|PUT|PATCH|DELETE) (\/api\/admin\/[^" ]+)/g;
  let routeMatch;
  while ((routeMatch = routePattern.exec(router))) {
    routes.push(routeMatch[2]);
  }
  const routeSet = new Set(routes);
  const allowedDynamicPrefixes = [
    '/api/admin/capability-market/groups/',
    '/api/admin/capability-market/users/',
    '/api/admin/security/users/',
    '/api/admin/card-store/orders/'
  ];
  function routeCouldMatch(url) {
    if (routeSet.has(url)) return true;
    if (allowedDynamicPrefixes.some(function(prefix) { return url === prefix; })) return true;
    const parts = url.split('/');
    return routes.some(function(route) {
      const routeParts = route.split('/');
      if (routeParts.length !== parts.length) return false;
      return routeParts.every(function(part, index) {
        return (part.startsWith('{') && part.endsWith('}')) || part === parts[index];
      });
    });
  }
  expectedScripts.filter(function(name) { return name !== 'validate-admin-modules.js'; }).forEach(function(name) {
    const content = read(name);
    const apiPattern = /['"](\/api\/admin\/[^'"?+)]*)/g;
    let match;
    while ((match = apiPattern.exec(content))) {
      const url = match[1];
      if (!routeCouldMatch(url)) {
        fail(name + ' references unregistered admin API route: ' + url);
      }
    }
  });
}

function assertModuleExports(name) {
  const markers = expectedExports[name];
  if (!markers || !markers.length) {
    return;
  }
  const content = read(name);
  markers.forEach(function(marker) {
    if (!content.includes(marker)) {
      fail(name + ' is missing expected export or marker: ' + marker);
    }
  });
}

function assertLegacyMirrorRemoved() {
  const legacyDir = path.join(root, 'js');
  if (fs.existsSync(legacyDir)) {
    fail('Legacy mirror directory should stay deleted: js/');
  }
}

function assertLLMProviderPricingHooks() {
  const content = read('llm-provider-tab.js');
  [
    'input_price_per_m_tokens_rmb',
    'output_price_per_m_tokens_rmb',
    'total_cost_rmb',
    'llmProviderInputPricePerM',
    'llmProviderOutputPricePerM',
    'llm-provider-price-chip',
    'pricePerMShort',
    'input_cost_rmb',
    'output_cost_rmb'
  ].forEach(function(marker) {
    if (!content.includes(marker)) {
      fail('llm-provider-tab.js is missing pricing marker: ' + marker);
    }
  });
}

function assertMaClawComputeProviderGate() {
  const content = read('maclaw-compute-module.js');
  [
    'window.canAddExternalProvider',
    'return false;',
    'updateExternalProviderEntryVisibility',
    'llmProviderCreateInlineBtn',
    'llmProvidersImportBtn',
    'window.addLLMProvider',
    'window.triggerLLMProvidersImport',
    'window.importLLMProvidersJSON',
    'refreshMaClawOfficialBanner',
    'AdminTabRegistry.onLanguageChange',
    'window.gatedAddProvider'
  ].forEach(function(marker) {
    if (!content.includes(marker)) {
      fail('maclaw-compute-module.js is missing compute gate marker: ' + marker);
    }
  });
  if (content.includes('if (!_computeAuthStatus) return true')) {
    fail('maclaw-compute-module.js must not allow provider creation before compute auth status loads');
  }
  if (content.includes('\\ud83d\\ude80') || content.includes('linear-gradient(135deg,#667eea')) {
    fail('maclaw-compute-module.js MaClaw compute banner should stay restrained and icon-free');
  }
  const handler = fs.readFileSync(path.join(root, '..', '..', 'internal', 'httpapi', 'llm_provider_handlers.go'), 'utf8');
  if (!handler.includes('LLM_EXTERNAL_PROVIDER_NOT_GRANTED') || !handler.includes('llmProviderRegistryAddsProviders')) {
    fail('llm_provider_handlers.go must enforce compute grants when adding providers');
  }
  if (!handler.includes('filterLLMProviderRegistryForRequest') || !handler.includes('GetLLMProvidersHandler(system store.SystemSettingsRepository, accessCtrl *llmservice.TenantLLMAccessControl)')) {
    fail('llm_provider_handlers.go must hide configured providers for tenants without compute grants');
  }
  const providerTab = read('llm-provider-tab.js');
  if (!providerTab.includes('window.addLLMProvider = addLLMProvider')) {
    fail('llm-provider-tab.js must export addLLMProvider so compute gating can wrap inline add actions');
  }
}

function assertHubLlmStatusTextIsIconFree() {
  const content = read('hub-llm-tab.js');
  ['\\u2705', '\\u274c', '\\u26aa', '\\ud83d\\udfe2', '\\ud83d\\udfe1', '\\ud83d\\udd34'].forEach(function(marker) {
    if (content.includes(marker)) {
      fail('hub-llm-tab.js status/test text must stay icon-free: ' + marker);
    }
  });
}

function assertSecurityCapabilityComplianceExportHooks() {
  const content = read('security-tab.js');
  [
    'capabilityComplianceExportSummary',
    'capabilityComplianceExportSummary(compliance)',
    'capabilityComplianceHasFilteredSummary',
    'normalizeCapabilityComplianceStatusFilter',
    'normalizeCapabilityStaleAfterHours',
    'risks: true',
    'snapshotRegistryFirstDefined',
    'hasFilteredSummary ? compliance.filtered_summary',
    'capabilityComplianceCsvRows',
    'capability_compliance_csv',
    'export_summary: exportSummary',
    'warning_severity_counts: exportSummary.warning_severity_counts',
    'snapshot_summary: capabilityComplianceExportSummary(compliance)',
    'total: totalCount',
    'qualityDenominator = totalCount + snapshotRegistryNonNegativeNumber(summary.unmanaged_installed)',
    'normalizeCapabilityDeploymentPolicy',
    "String(policy || 'required').trim().toLowerCase()",
    'itemPolicy = normalizeCapabilityDeploymentPolicy',
    'itemSource = String(item.source ||',
    'effectivePolicy = normalizeCapabilityDeploymentPolicy',
    'effectiveKind = String(policy.kind ||',
    'effectiveSource = String(policy.source ||',
    'kind = String(kind ||',
    'policy = normalizeCapabilityDeploymentPolicy(policy)',
    'policy: normalizeCapabilityDeploymentPolicy(row.policy)',
    "policy !== 'recommended' && policy !== 'blocked'",
    'unmanaged_installed',
    'value="unmanaged_installed"',
    'capabilityRiskStatuses',
    "qualityText = st('snapshotQuality'",
    'snapshotRegistrySeveritySummary(severity)',
    'filtered_summary',
    "'summary_scope'",
    "'filtered_total'",
    "'full_total'",
    'summary_scope: summaryScope',
    'filtered_total: filteredTotal',
    'full_total: fullTotal',
    'filteredMeta',
    'capabilityFilteredMeta',
    'filteredSummary.unmanaged_installed',
    'item.summary_scope',
    "'summary_scope', 'filtered_total', 'full_total'",
    "capabilityComplianceStatusFilter === 'issues'",
    "quality: blockedInstalledCount ? 'incomplete'",
    "'quality_score'",
    "'warn_count'",
    "'error_count'",
    'blocked-installed',
    'Search ID/object/path/checksum/risk',
    'snapshotRegistryFilterErrors',
    'snapshotRegistryFilterWarnings',
    'snapshotRegistryFilterFiltered',
    'snapshotRegistryScopes',
    'snapshotRegistryScopeSummary',
    'scope_counts',
    'registry_scope_counts',
    'registry_filtered_count',
    'registry_all_scope_count',
    'scope_asc',
    'snapshotRegistrySortScopeAsc',
    'visible.map(function(item)',
    'filtered: true',
    'errors: true, warnings: true'
  ].forEach(function(marker) {
    if (!content.includes(marker)) {
      fail('security-tab.js is missing enterprise compliance export marker: ' + marker);
    }
  });
}

function assertSecurityDefaultGroupUsesName() {
  const content = read('security-tab.js');
  [
    'defaultGroupName',
    'settings.default_group_name',
    'defaultGroupLabel()',
    'renderDefaultGroupHint()',
    'default_group_id: state().defaultGroupId'
  ].forEach(function(marker) {
    if (!content.includes(marker)) {
      fail('security-tab.js is missing default group display-name marker: ' + marker);
    }
  });
  if (content.includes("settings.default_group_id || st('notSet')")) {
    fail('security-tab.js must not render default_group_id directly as the default group label');
  }
}

function assertSecurityTenantSchemaGuards() {
  const securityStore = fs.readFileSync(path.join(root, '..', '..', 'internal', 'security', 'store.go'), 'utf8');
  const sqliteMigrations = fs.readFileSync(path.join(root, '..', '..', 'internal', 'store', 'sqlite', 'migrations.go'), 'utf8');
  [securityStore, sqliteMigrations].forEach(function(content, index) {
    const name = index === 0 ? 'security/store.go' : 'store/sqlite/migrations.go';
    [
      'PRIMARY KEY (tenant_id, email)',
      'PRIMARY KEY (tenant_id, group_id)'
    ].forEach(function(marker) {
      if (!content.includes(marker)) {
        fail(name + ' is missing tenant-scoped security schema marker: ' + marker);
      }
    });
    if (content.includes('email TEXT PRIMARY KEY') || content.includes('group_id TEXT PRIMARY KEY')) {
      fail(name + ' must not use single-column primary keys for tenant-scoped security tables');
    }
  });
}

expectedScripts.concat(['MODULES.md', 'check-admin.ps1']).forEach(assertExists);
expectedScripts.forEach(assertJavaScriptSyntax);
expectedScripts.forEach(assertModuleExports);
expectedScripts.concat(['MODULES.md', 'validate-admin-modules.js', 'check-admin.ps1']).forEach(assertAscii);
removedLegacyFiles.forEach(assertMissing);
assertScriptOrder();
assertHealthHook();
assertTenantAdminUIHooks();
assertEmptyTextNodesAreOwned();
assertBlankPlaceholdersAreOwned();
assertBlankControlsAreOwned();
assertDataI18nKeysHaveTranslations();
assertAdminApiRoutesRegistered();
assertGlobalAdminRuntimeHooks();
assertScopedRefreshHooks();
assertLegacyMirrorRemoved();
assertHubLlmStatusTextIsIconFree();
assertLLMProviderPricingHooks();
assertMaClawComputeProviderGate();
assertSecurityDefaultGroupUsesName();
assertSecurityTenantSchemaGuards();
assertSecurityCapabilityComplianceExportHooks();

if (!process.exitCode) {
  console.log('Admin module validation passed.');
}
