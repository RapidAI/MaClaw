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
  'feishu-tab.js',
  'invitation-tab.js',
	'user-referrals-tab.js',
  'pwa-tab.js',
  'system-tab.js',
  'compute-tab.js',
  'llm-provider-tab.js',
  'llm-service-tabs.js',
  'card-store-tab.js',
  'usage-stats-tab.js',
  'failure-logs-tab.js',
  'knowledge-management-tab.js',
  'digital-assets-tab.js',
  'notification-tab.js',
  'admin-module-health.js',
  'overview-tenant-info.js',
  'overview-config-agent.js',
  'admin-bootstrap.js'
];
const removedLegacyFiles = [
  'llmproviders.js',
  'usagestats.js',
  'admin-check.js',
  'hub-admin-check.js',
  '_extra.js',
  'hub-llm-tab.js'
];
const expectedExports = {
  'machines-tab.js': ['renderMachineList', 'loadMachines', 'loadAdaptivePromptFleet'],
  'security-tab.js': ['loadSecurityTab', 'loadApprovalRolesTab', 'saveSecApprovalRoles', 'selectSecGroup', 'confirmAssignUsers'],
  'llm-provider-tab.js': ['loadLlmProviders', 'openLlmProviderTab', 'saveLLMProviders'],
  'llm-service-tabs.js': ['loadLlmServiceGroups', 'openLlmServiceGroupTab', 'saveLLMServiceAdmin'],
  'usage-stats-tab.js': ['loadUsageStats'],
  'knowledge-management-tab.js': ['loadKnowledgeShares', 'forceDeleteKnowledgeShare'],
  'admin-ui.js': ['confirmDialog', 'promptDialog', 'dismissActiveDialog', 'isDialogOpen', 'admin-ui-dialog-overlay', 'mountDialogSession', 'DIALOG_Z_INDEX', '20000', 'bindModalOverlayDismiss', 'isImeComposing', 'skipDismiss'],
  'digital-assets-tab.js': ['loadDigitalAssetLibraries', 'createDigitalAssetLibrary', 'stopDigitalAssetsForUnauthorizedScope', 'digital-assets-merge-src', 'import/local-dir', 'import/browser-dir', 'digitalAssetsBrowserDir', 'digitalAssetsServerDir', 'trackJob', 'openContentDialog', 'import-jobs', '/sources', 'beginProgress', 'phaseLabel', 'jobIdOf', 'digitalAssetsProgressTimeout', 'digitalAssetsPhaseImporting', 'deleteContentSources', 'sources/delete', 'digitalAssetsContentDeleteSelected', 'digitalAssetsContentSearch', 'loadMoreContentSources', 'digitalAssetsContentLoadMore', 'offset=', 'scheduleContentJobsPoll', 'refreshContentJobsOnly', 'wireContentScrollLoadMore', 'maybeAutoFillSources', 'jobsStatusSignature', 'contentAutoFillRounds', 'renderAclPanel', 'renderDepartmentTree', 'saveLibraryAcl', 'loadSecurityGroups', 'set_acl', 'digitalAssetsAclSave', 'digital-assets-acl-dept', 'acl_mode', '/api/admin/security/groups', 'captureAclDraftFromDom', 'itemWithAclDraft', 'digitalAssetsAclClearDepartmentsBtn', 'digitalAssetsAclDeptFilter', 'digitalAssetsAclEmptyRestrictedWarn', 'unknownSelectedDepartments', 'showConfirm', 'showPrompt', 'confirmDialog', 'promptDialog', 'digitalAssetsDeleteLibraryConfirm', 'digitalAssetsCreateNamePrompt', 'admin-ui-dialog-overlay', 'isDialogOpen', 'createLibraryBusy', 'isAdminDialogOpen', 'aclSaveGuard', 'contentDeleteGuard', 'deleteLibraryBusy', 'downloadBackup', 'backupFilename', 'global.URL.createObjectURL', 'Authorization', 'res.status === 401', 'global.logoutAdmin']
};

function fail(message) {
  console.error('VALIDATION FAILED:', message);
  process.exitCode = 1;
}

function read(name) {
  return fs.readFileSync(path.join(root, name), 'utf8');
}

function extractNamedFunction(source, name) {
  const marker = 'function ' + name + '(';
  const start = source.indexOf(marker);
  if (start < 0) {
    throw new Error('missing function ' + name);
  }
  let depth = 0;
  for (let i = start; i < source.length; i += 1) {
    const ch = source[i];
    if (ch === '{') {
      depth += 1;
    } else if (ch === '}') {
      depth -= 1;
      if (depth === 0) {
        return source.slice(start, i + 1);
      }
    }
  }
  throw new Error('unterminated function ' + name);
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
    const scriptPattern = new RegExp('src="/admin/' + name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '(?:\\?[^"]*)?"');
    const match = scriptPattern.exec(html);
    const idx = match ? match.index : -1;
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
    'id="tab-tenants"',
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
  const tenant = read('tenant-tab.js');
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
  // Registration verification and SMS credentials are tenant policy. Keep the
  // visual boundary and the loading boundary aligned with the server route.
  if (!html.includes('id="registrationAuthCard"')) {
    fail('index.html is missing the tenant registration verification card.');
  }
  if (!tenant.includes("registrationAuthCard.classList.toggle('hidden', !tenantAdmin)")) {
    fail('tenant-tab.js must show registration verification only to tenant admins.');
  }
  const openTabSource = extractNamedFunction(admin, 'openTab');
  const tenantSystemLoads = "if (profile && String(profile.scope || '').toLowerCase() === 'tenant') { if (typeof loadRegistrationAuthConfig === 'function') loadRegistrationAuthConfig();";
  if (!openTabSource.includes(tenantSystemLoads)) {
    fail('admin.js must load registration verification only in the tenant system scope.');
  }
  const globalSystemLoads = 'else { loadTlsConfig(); loadSystemRoutingConfig();';
  if (!openTabSource.includes(globalSystemLoads)) {
    fail('admin.js must keep Hub routing and TLS loading in the global system scope.');
  }
  if (!system.includes('function canManageRegistrationAuth()') || !system.includes("String(profile.scope || '').toLowerCase() === 'tenant'")) {
    fail('system-tab.js must guard registration verification handlers to tenant scope.');
  }
  ['loadRegistrationAuthConfig', 'saveRegistrationAuthConfig'].forEach(function(name) {
    const handler = extractNamedFunction(system, name);
    if (!handler.includes('if (!canManageRegistrationAuth()) return null;')) {
      fail('system-tab.js must guard direct registration verification calls.');
    }
  });
  ['id="mailConfigCard"', 'id="tenantMailSenderCard"', 'tenantMailFromName', 'id="tenantMigrationSettingsCard"', 'tenantMigrationMaxMB', 'id="tenantSystemLLMDefaultsCard"', 'tenantSystemFreeStatusBadge', 'tenantSystemFreeTestBtn', 'tenantSystemLLMDefaultsSaveBtn', 'id="tenantDigitalAssetsSettingsCard"', 'tenantDigitalAssetsEnabledToggle', 'tenantDigitalAssetsSyncToggle'].forEach(function(marker) {
    if (!html.includes(marker)) {
      fail('index.html is missing tenant-safe mail settings marker: ' + marker);
    }
  });
  if (html.includes('tenantSystemDefaultLLMServiceGroup')) {
    fail('index.html must not expose a system-free service-group select; system-free is fixed.');
  }
  ['loadTenantMailSenderName', 'saveTenantMailSenderName', 'TENANT_MAIL_SENDER_MAX_RUNES', 'normalizeTenantMailSenderName', '/api/admin/mail/sender-name'].forEach(function(marker) {
    if (!system.includes(marker)) {
      fail('system-tab.js is missing tenant sender-name marker: ' + marker);
    }
  });
  ['loadTenantMigrationSettings', 'saveTenantMigrationSettings', 'TENANT_MIGRATION_MIN_MB', 'TENANT_MIGRATION_MAX_MB', '/api/admin/migration/settings'].forEach(function(marker) {
    if (!system.includes(marker)) {
      fail('system-tab.js is missing tenant migration settings marker: ' + marker);
    }
  });
  ['loadTenantDigitalAssetsSettings', 'toggleTenantDigitalAssetsEnabled', 'toggleTenantDigitalAssetsSync', '/api/admin/digital-assets/settings', 'TENANT_DIGITAL_ASSETS_SETTINGS_I18N'].forEach(function(marker) {
    if (!system.includes(marker)) {
      fail('system-tab.js is missing tenant digital assets settings marker: ' + marker);
    }
  });
  if (!system.includes('function canManageTenantDigitalAssets()')) {
    fail('system-tab.js must define the tenant digital assets scope guard.');
  }
  ['loadTenantDigitalAssetsSettings', 'toggleTenantDigitalAssetsEnabled', 'toggleTenantDigitalAssetsSync'].forEach(function(name) {
    const handler = extractNamedFunction(system, name);
    if (!handler.includes('if (!canManageTenantDigitalAssets()) return null;')) {
      fail('system-tab.js must guard ' + name + ' to tenant admins.');
    }
  });
  ['loadTenantSystemLLMDefaults', 'saveTenantSystemLLMDefaults', 'getTenantSystemFreeCache', 'setTenantSystemFreeCache', 'fetchTenantSystemFreeStatus', 'formatTenantSystemFreeDetail', 'renderTenantSystemFreeStatus', 'applyTenantSystemFreeStatusUI', '/api/admin/llm/system-free', '/api/admin/llm/system-free/test', 'testTenantSystemFreeLLM', 'openSystemFreeServiceGroup', 'skipPeer', 'systemFreeConfigToasted', 'tenantSystemFreeTestInflight'].forEach(function(marker) {
    if (!system.includes(marker)) {
      fail('system-tab.js is missing tenant system-free LLM marker: ' + marker);
    }
  });
  const llmServices = read('llm-service-tabs.js');
  ['openSystemFreeServiceGroup', 'SYSTEM_FREE_LLM_SERVICE_GROUP_ID', "openLLMServiceGroupDialog('edit', SYSTEM_FREE_LLM_SERVICE_GROUP_ID)", 'editingSystemFree', 'fetchTenantSystemFreeStatus'].forEach(function(marker) {
    if (!llmServices.includes(marker)) {
      fail('llm-service-tabs.js is missing system-free deep-link marker: ' + marker);
    }
  });
  const overviewTenant = read('overview-tenant-info.js');
  ['applyOverviewSystemFreeStatus', 'setTenantSystemFreeCache', 'loadOverviewSystemFreeStatus'].forEach(function(marker) {
    if (!overviewTenant.includes(marker)) {
      fail('overview-tenant-info.js is missing system-free status marker: ' + marker);
    }
  });
  if (system.includes("api('/api/admin/llm/services?include_cards=false')") && /async function loadTenantSystemLLMDefaults[\s\S]*?api\('\/api\/admin\/llm\/services\?include_cards=false'\)/.test(system)) {
    fail('loadTenantSystemLLMDefaults must only fetch system-free status, not model service groups.');
  }
  if (system.includes('escapeAttr(')) {
    fail('system-tab.js must not call escapeAttr; it is local to other admin modules. Use escapeHtml for option attributes.');
  }
  ['tenantSystemFreeStatusCache', 'system-free', 'noUsableGroups', 'testTenantSystemFreeLLM', 'renderTenantSystemFreeStatus'].forEach(function(marker) {
    if (!system.includes(marker)) {
      fail('system-tab.js is missing robust tenant system-free LLM marker: ' + marker);
    }
  });
  const bootstrap = read('admin-bootstrap.js');
  ['Promise.allSettled', 'loadTenants', 'loadCenterStatus', 'loadMailConfig', 'loadTenantMigrationSettings', 'loadTenantDigitalAssetsSettings', 'loadTenantSystemLLMDefaults', 'checkComputeAuthorization', 'loadLlmProviders', 'loadLlmServiceGroups', 'loadUsageStats', 'loadFailureLogs'].forEach(function(marker) {
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
  if (/HUB_LLM_PANE_I18N|loadHubLlmPromptCacheConfig|loadHubLlmStatus|PromptCache/i.test(im)) {
    fail('im-tab.js must not own removed LLM prompt-cache admin UI.');
  }
  const imSectionEnd = html.indexOf('id="tab-machines"');
  const imSection = html.slice(html.indexOf('id="tab-im"'), imSectionEnd >= 0 ? imSectionEnd : undefined);
  ['imSubHubLlm', 'hubLlmPromptCache', 'hubLlmCacheConfig'].forEach(function(marker) {
    if (imSection.includes(marker)) {
      fail('index.html IM section still contains LLM prompt-cache marker: ' + marker);
    }
  });
  const adminSources = html + '\n' + expectedScripts.map(function(name) { return read(name); }).join('\n');
  [
    'data-tab="hubllm"',
    'hubllm',
    'id="tab-hubllm"',
    'src="/admin/hub-llm-tab.js"',
    'LLM Prompt Cache',
    'hub_llm_config',
    'hub_llm_test',
    'hub_llm_prompt_cache_config',
    'hub_llm_prompt_cache_clear',
    'hub_llm_prompt_cache_entries',
    'hub_llm_prompt_cache_entry',
    'loadHubLlmConfig',
    'saveHubLlmConfig',
    'testHubLlm',
    'loadHubLlmPromptCacheConfig',
    'saveHubLlmPromptCacheConfig',
    'refreshHubLlmPromptCache',
    'clearHubLlmPromptCache',
    'loadHubLlmStatus',
    'navHubLlm',
    'hubLlmApiUrl',
    'hubLlmApiKey',
    'hubLlmModel',
    'hubLlmProtocol',
    'hubLlmEnabled',
    'hubLlmTestBtn',
    'hubLlmSaveBtn',
    'hubLlmPromptCache',
    'hubLlmCache'
  ].forEach(function(marker) {
    if (adminSources.includes(marker)) {
      fail('Hub LLM / prompt-cache admin UI must stay removed: ' + marker);
    }
  });
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
  const tenantSystemNavDescMatch = /systemNavDescTenant:\s*\{[^}]+\}/.exec(tenant);
  const tenantSystemNavDesc = tenantSystemNavDescMatch ? tenantSystemNavDescMatch[0] : '';
  const tenantSystemDescMentionsCapabilities = tenantSystemNavDesc.includes('Manage tenant mail, LLM, and system capabilities') && tenantSystemNavDesc.includes('\\u7cfb\\u7edf\\u80fd\\u529b');
  const tenantSystemDescMentionsAccounts = tenantSystemNavDesc.includes('admin account settings') || tenantSystemNavDesc.includes('\\u7ba1\\u7406\\u5458\\u8d26\\u53f7');
  if (!tenantSystemDescMentionsCapabilities || tenantSystemDescMentionsAccounts) {
    fail('tenant-tab.js tenant system settings navigation must describe system capabilities, not account settings.');
  }
  const health = read('admin-module-health.js');
  ['TenantTab', 'loadTenants', 'createTenantAdmin', 'loadLoginTenants'].forEach(function(marker) {
    if (!health.includes(marker)) {
      fail('admin-module-health.js is missing tenant health marker: ' + marker);
    }
  });
}

function assertTenantSystemLLMDefaultBehavior() {
  const system = read('system-tab.js');
  // system-free is a fixed reserved group: no service-group picker, status-only card.
  if (system.includes('tenantSystemDefaultLLMServiceGroup') || system.includes('tenantSystemLLMUsableGroups')) {
    fail('system-tab.js must not keep a service-group picker for system-free.');
  }
  const tslxFn = 'function tslx(key, vars) { vars = vars || {}; var map = {'
    + 'hint:"HINT", providers:"Providers: {ids}", notReadyDetail:"not ready: {id}", noUsableGroups:"NO_ROUTE",'
    + 'ready:"Ready", notReady:"Not ready"'
    + '}; var s = map[key] || key; return s.replace(/\\{(\\w+)\\}/g, function(_, n) { return vars[n] == null ? "" : vars[n]; }); }';
  const code = [
    tslxFn,
    'var tenantSystemFreeStatusCache = null;',
    'var window = {};',
    'function canManageRegistrationAuth() { return true; }',
    extractNamedFunction(system, 'clearTenantSystemFreeState'),
    extractNamedFunction(system, 'getTenantSystemFreeCache'),
    extractNamedFunction(system, 'setTenantSystemFreeCache'),
    extractNamedFunction(system, 'formatTenantSystemFreeDetail'),
    'var st = setTenantSystemFreeCache({ ready: true, provider_ids: ["maclaw_official"] });',
    'if (!st.ready || window.tenantSystemFreeStatusCache !== st) throw new Error("cache sync failed");',
    'if (getTenantSystemFreeCache() !== st) throw new Error("get cache mismatch");',
    'tenantSystemFreeStatusCache = null;',
    'window.tenantSystemFreeStatusCache = { ready: false, provider_ids: ["local"], reasons: ["x"] };',
    'var fromWindow = getTenantSystemFreeCache();',
    'if (!fromWindow || fromWindow.reasons[0] !== "x") throw new Error("window fallback failed");',
    'var readyDetail = formatTenantSystemFreeDetail(st);',
    'if (readyDetail.indexOf("maclaw_official") < 0 || readyDetail.indexOf("NO_ROUTE") >= 0) throw new Error("ready detail: " + readyDetail);',
    'var notReady = formatTenantSystemFreeDetail({ ready: false, provider_ids: [], reasons: ["no_provider"] });',
    'if (notReady.indexOf("no_provider") < 0) throw new Error("not-ready detail: " + notReady);',
    'var empty = formatTenantSystemFreeDetail({ ready: false });',
    'if (empty.indexOf("NO_ROUTE") < 0) throw new Error("empty not-ready detail: " + empty);'
  ].join('\n');
  try {
    new vm.Script(code, { filename: 'system-tab-tenant-system-free-behavior.js' }).runInNewContext({});
  } catch (err) {
    fail('system-tab.js tenant system-free behavior regression: ' + err.message);
  }
}

function assertEmptyTextNodesAreOwned() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const scripts = expectedScripts.map(function(name) { return read(name); }).join('\n');
  const allowedDynamic = {
    setupGateList: true,
    maclawComputeTopAlert: true,
    userReferralInviters: true,
	userReferralMetricGrid: true,
	userReferralMetricsHint: true,
    userReferralPagerMeta: true,
    userReferralReviewQueue: true,
    userReferralReviewPagerMeta: true,
    userReferralDetailBody: true,
    userReferralDetailMeta: true
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

function assertMaclawAppEvidenceReviewMarkers() {
  const marketplace = read('marketplace-tab.js');
  [
    'maclawAppEvidenceSummary',
    'approval_instance',
    'progress_instances',
    'datasrv_registration',
    'workflow_contract',
    "['Approval', approvalText]",
    "['DataSrv', dataSrvText]",
    "['Workflow', workflowText]",
    "['Coverage', coverageText]",
    "['Outputs', outputText]",
    'review_evidence',
    'result_contract_primary',
    'test_protocol_fingerprint',
    'result_coverage_primary',
    'result_coverage_covered_count'
  ].forEach(function(marker) {
    if (!marketplace.includes(marker)) {
      fail('marketplace-tab.js is missing MaClaw App evidence review marker: ' + marker);
    }
  });
  const hubcenterSkillmarketPath = path.join(root, '..', '..', '..', 'hubcenter', 'web', 'admin', 'assets', 'js', 'skillmarket-admin.js');
  const hubcenterSkillmarket = fs.existsSync(hubcenterSkillmarketPath) ? fs.readFileSync(hubcenterSkillmarketPath, 'utf8') : '';
  try {
    new vm.Script(hubcenterSkillmarket, { filename: 'hubcenter/web/admin/assets/js/skillmarket-admin.js' });
  } catch (err) {
    fail('hubcenter skillmarket-admin.js has invalid JavaScript syntax: ' + err.message);
  }
  [
    'smRenderEnterpriseReviewEvidence',
    'sm-review-evidence-strip',
    'approval_instance',
    'progress_instances',
    'datasrv_registration',
    'workflow_contract',
    'resultContract',
    'testProtocol',
    'resultCoverage',
    'coverageCovered',
    'result_coverage_covered_count',
    'output_count',
    'artifact_count'
  ].forEach(function(marker) {
    if (!hubcenterSkillmarket.includes(marker)) {
      fail('hubcenter skillmarket-admin.js is missing MaClaw App review evidence marker: ' + marker);
    }
  });
}
function assertUsageRankingEmailFilter() {
  const fnSource = extractNamedFunction(read('usage-stats-tab.js'), 'isRankingEmail');
  const sandbox = {};
  vm.runInNewContext(fnSource + '\nthis.isRankingEmail = isRankingEmail;', sandbox, { filename: 'usage-stats-tab.js:isRankingEmail' });
  [
    ['user@example.com', true],
    [' User@Example.com ', true],
    ['u_1774182684297100200', false],
    ['foo@', false],
    ['@example.com', false],
    ['foo @example.com', false],
    ['foo@@example.com', false],
    ['', false]
  ].forEach(function(entry) {
    const got = sandbox.isRankingEmail(entry[0]);
    if (got !== entry[1]) {
      fail('usage-stats-tab.js isRankingEmail(' + JSON.stringify(entry[0]) + ') = ' + got + ', want ' + entry[1]);
    }
  });
}

function assertUsageStatsSubtabState() {
  const content = read('usage-stats-tab.js');
  [
    'usage-stats-subtab is-active',
    "setAttribute('aria-selected'",
    "setAttribute('aria-hidden'",
    'onUsageStatsSubtabKeydown(event)',
    'role="tablist"',
    'role="tab"'
  ].forEach(function(marker) {
    if (!content.includes(marker)) {
      fail('usage-stats-tab.js is missing active subtab state marker: ' + marker);
    }
  });
}

function assertDigitalAssetDepartmentTreeRender() {
  const source = read('digital-assets-tab.js');
  if (!source.includes('function canManageDigitalAssets()')) {
    fail('digital-assets-tab.js must define a tenant-admin scope guard.');
  }
  ['loadDigitalAssetLibraries', 'createDigitalAssetLibrary'].forEach(function(name) {
    const handler = extractNamedFunction(source, name);
    if (!handler.includes('if (!canManageDigitalAssets())')
        || !handler.includes('stopDigitalAssetsForUnauthorizedScope();')
        || !handler.includes('return null;')) {
      fail('digital-assets-tab.js must guard ' + name + ' to tenant admins.');
    }
  });
  const clearUnauthorized = extractNamedFunction(source, 'stopDigitalAssetsForUnauthorizedScope');
  ['stopContentJobsPoll()', 'state.contentJobsPollToken', 'state.progressToken', 'state.contentOpen = false', 'state.items = []', "global.stopDigitalAssetsForUnauthorizedScope = stopDigitalAssetsForUnauthorizedScope"].forEach(function(marker) {
    if (!clearUnauthorized.includes(marker) && !source.includes(marker)) {
      fail('digital-assets-tab.js must clear tenant data and polling after scope changes: ' + marker);
    }
  });
  const normalizeTree = extractNamedFunction(source, 'normalizeSecurityGroupTree');
  const renderTree = extractNamedFunction(source, 'renderDepartmentTree');
  const panel = extractNamedFunction(source, 'renderAclPanel');
  if (!renderTree.includes("}).join('');")) {
    fail('digital-assets-tab.js renderDepartmentTree must return rendered HTML text.');
  }
  if (/\bknownRows\.join\(/.test(panel)) {
    fail('digital-assets-tab.js must not call join() on knownRows; it is already rendered HTML text.');
  }
  if (!panel.includes("+ knownRows")) {
    fail('digital-assets-tab.js must append rendered department tree HTML directly.');
  }
  ['normalizeSecurityGroupTree', 'seenIDs', 'digitalAssetsMaxDepartmentTreeDepth', "Array.isArray(node.children) ? node.children : []"].forEach(function(marker) {
    if (!source.includes(marker)) {
      fail('digital-assets-tab.js is missing department tree resilience marker: ' + marker);
    }
  });
  const sandbox = {};
  vm.runInNewContext('var digitalAssetsMaxDepartmentTreeDepth = 64;\n' + normalizeTree + '\nthis.normalizeSecurityGroupTree = normalizeSecurityGroupTree;', sandbox, { filename: 'digital-assets-tab.js:normalizeSecurityGroupTree' });
  const normalized = sandbox.normalizeSecurityGroupTree([
    { id: 'root', name: 'Root', children: [{ id: 'child', name: 'Child', children: [{ id: 'root', name: 'Cycle' }] }] },
    { id: 'child', name: 'Duplicate' },
    { id: '  ', name: 'Malformed' },
    { id: 'other', name: 'Other', children: 'not-an-array' }
  ]);
  if (normalized.length !== 2 || normalized[0].children.length !== 1 || normalized[0].children[0].children.length !== 0 || normalized[1].id !== 'other') {
    fail('digital-assets-tab.js must remove malformed, cyclic, and duplicate department tree nodes.');
  }
  let deepTree = { id: 'node-0' };
  let cursor = deepTree;
  for (let i = 1; i <= 70; i += 1) {
    cursor.children = [{ id: 'node-' + i }];
    cursor = cursor.children[0];
  }
  let normalizedDepth = 0;
  let deepCursor = sandbox.normalizeSecurityGroupTree([deepTree])[0];
  while (deepCursor) {
    normalizedDepth += 1;
    deepCursor = deepCursor.children[0];
  }
  if (normalizedDepth !== 65) {
    fail('digital-assets-tab.js must cap an oversized department tree before rendering.');
  }
  const selectionHandler = extractNamedFunction(source, 'aclRestrictionChanged');
  ['restricted && selected > digitalAssetsMaxAclDepartments', 'changedCheckbox.checked = false', 'digitalAssetsMaxAclDepartments'].forEach(function(marker) {
    if (!selectionHandler.includes(marker)) {
      fail('digital-assets-tab.js must prevent selecting departments over the ACL limit: ' + marker);
    }
  });
}

function assertDigitalAssetRoutesTenantScoped() {
  const routerPath = path.join(root, '..', '..', 'internal', 'httpapi', 'router.go');
  const router = fs.readFileSync(routerPath, 'utf8');
  const routes = router.split('\n').filter(function(line) {
    return line.includes('/api/admin/digital-assets/');
  });
  if (!routes.length) {
    fail('router.go must register digital asset admin routes.');
    return;
  }
  routes.forEach(function(line) {
    if (!line.includes('requireTenantAdmin(') || line.includes('requireAdmin(')) {
      fail('digital asset admin route must require a tenant admin: ' + line.trim());
    }
  });
}

function assertAdminAssetsNoStore() {
  const staticPath = path.join(root, '..', '..', 'internal', 'httpapi', 'static.go');
  const source = fs.readFileSync(staticPath, 'utf8');
  [
    'for _, method := range []string{http.MethodGet, http.MethodHead}',
    'if r.Method == http.MethodHead',
    'w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")'
  ].forEach(function(marker) {
    if (!source.includes(marker)) {
      fail('admin static assets must be no-store for GET and HEAD: ' + marker);
    }
  });
}

function assertUserReferralPanelIsolation() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const css = read('professional.css');
  const admin = read('admin.js');
  if (!html.includes('id="tab-userreferrals" class="panel card"')) {
    fail('index.html must keep User referrals inside an independently switchable panel.');
  }
  if (!css.includes('#tab-userreferrals.active{display:grid!important;gap:14px}')) {
    fail('professional.css must enable the User referrals grid only while its panel is active.');
  }
  if (css.includes('#tab-userreferrals{display:grid;gap:14px}')) {
    fail('professional.css must not force User referrals visible while another panel is active.');
  }
  if (!admin.includes("document.querySelectorAll('.panel').forEach(v => v.classList.remove('active'))")) {
    fail('admin.js must deactivate every panel before activating the selected one.');
  }
}

function assertAdminApiRoutesRegistered() {
  const routerPath = path.join(root, '..', '..', 'internal', 'httpapi', 'router.go');
  const router = fs.readFileSync(routerPath, 'utf8');
  const migrationHandlerPath = path.join(root, '..', '..', 'internal', 'httpapi', 'migration_handlers.go');
  const migrationHandler = fs.existsSync(migrationHandlerPath) ? fs.readFileSync(migrationHandlerPath, 'utf8') : '';
  const routes = [];
  const routePattern = /mux\.HandleFunc\("(GET|POST|PUT|PATCH|DELETE) (\/api\/admin\/[^" ]+)/g;
  let routeMatch;
  [router, migrationHandler].forEach(function(content) {
    routePattern.lastIndex = 0;
    while ((routeMatch = routePattern.exec(content))) {
      routes.push(routeMatch[2]);
    }
  });
  const routeSet = new Set(routes);
  const allowedDynamicPrefixes = [
    '/api/admin/capabilities/maclaw-apps/',
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

function assertRemovedLegacyFilesDocumented() {
  const docs = read('MODULES.md');
  removedLegacyFiles.forEach(function(name) {
    if (!docs.includes('- ' + name)) {
      fail('MODULES.md must document removed legacy file: ' + name);
    }
  });
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
    'maclawComputeTopAlert',
    'hasAvailableOfficialComputeCredits',
    'shouldShowGlobalComputeAlert',
    'updateGlobalComputeAlert',
    'noAvailableCompute',
    'noAvailableComputeAction',
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

function assertApprovalRolesHooks() {
  const html = fs.readFileSync(indexPath, 'utf8');
  const admin = read('admin.js');
  const security = read('security-tab.js');
  const css = read('professional.css');
  [
    'data-tab="approvalroles"',
    'id="tab-approvalroles"',
    'id="secApprovalRolesRoot"',
    'onclick="saveSecApprovalRoles()"'
  ].forEach(function(marker) {
    if (!html.includes(marker)) {
      fail('index.html is missing approval roles marker: ' + marker);
    }
  });
  [
    'approvalroles',
    "normalized === 'approvalroles'",
    'window.loadApprovalRolesTab'
  ].forEach(function(marker) {
    if (!admin.includes(marker)) {
      fail('admin.js is missing approval roles tab marker: ' + marker);
    }
  });
  [
    '/api/admin/security/approval-roles',
    'APPROVAL_ROLES_STORAGE_KEY',
    'approvalGroupMemberCache',
    'approvalSubjectRowsForScope',
    'openSecApprovalSubjectPicker',
    'approvalRoleTemplatesForScope',
    'syncVisibleApprovalRoleRows',
    'approvalSubjectTypeLabel'
  ].forEach(function(marker) {
    if (!security.includes(marker)) {
      fail('security-tab.js is missing approval roles marker: ' + marker);
    }
  });
  [
    '#tab-approvalroles .approval-role-layout',
    '#tab-approvalroles .approval-role-subjects',
    '.approval-subject-dialog'
  ].forEach(function(marker) {
    if (!css.includes(marker)) {
      fail('professional.css is missing approval roles layout marker: ' + marker);
    }
  });
}

expectedScripts.concat(['MODULES.md', 'check-admin.ps1']).forEach(assertExists);
expectedScripts.forEach(assertJavaScriptSyntax);
expectedScripts.forEach(assertModuleExports);
// Legacy modules may still contain pre-existing localized source. Keep the
// invitation module in the same ASCII-only contract as the other modern admin
// modules (Chinese copy must be expressed with \u escapes).
['user-referrals-tab.js', 'MODULES.md', 'validate-admin-modules.js', 'check-admin.ps1'].forEach(assertAscii);
removedLegacyFiles.forEach(assertMissing);
assertScriptOrder();
assertHealthHook();
assertTenantAdminUIHooks();
assertTenantSystemLLMDefaultBehavior();
assertEmptyTextNodesAreOwned();
assertBlankPlaceholdersAreOwned();
assertBlankControlsAreOwned();
assertDataI18nKeysHaveTranslations();
assertAdminApiRoutesRegistered();
assertGlobalAdminRuntimeHooks();
assertScopedRefreshHooks();
assertMaclawAppEvidenceReviewMarkers();
assertUsageRankingEmailFilter();
assertUsageStatsSubtabState();
assertDigitalAssetDepartmentTreeRender();
assertDigitalAssetRoutesTenantScoped();
assertAdminAssetsNoStore();
assertUserReferralPanelIsolation();
assertLegacyMirrorRemoved();
assertRemovedLegacyFilesDocumented();
assertLLMProviderPricingHooks();
assertMaClawComputeProviderGate();
assertSecurityDefaultGroupUsesName();
assertSecurityTenantSchemaGuards();
assertSecurityCapabilityComplianceExportHooks();
assertApprovalRolesHooks();
assertConfigAgentHooks();

function assertConfigAgentHooks() {
  const html = read('index.html');
  const ca = read('overview-config-agent.js');
  [
    'id="overviewConfigAgent"',
    'id="configAgentChatLog"',
    'id="configAgentInput"',
    'id="configAgentSendBtn"',
    'id="configAgentExamples"',
    'id="configAgentHistory"',
    'src="/admin/overview-config-agent.js"'
  ].forEach(function(marker) {
    if (!html.includes(marker)) {
      fail('index.html is missing config agent marker: ' + marker);
    }
  });
  [
    'initConfigAgent',
    'submitConfigAgent',
    'formatSupportPackJSON',
    'downloadSupportPackJson',
    'formatSupportHandoffText',
    'renderDiagnoseWizardStrip',
    'renderMissingFieldsPanel',
    'setSessionFromPlanResponse',
    'clearConfigAgentSession',
    'showToolCatalog',
    'filterCatalogTools',
    'buildCatalogSectionsHtml',
    'showShortcutsHelp',
    'toggleFavoriteCommand',
    'rememberRecentCommand',
    'exportCommandPrefs',
    'importCommandPrefsFromObject',
    'historyGroupBySession',
    'groupHistoryBySession',
    '/api/admin/config-agent/plan',
    '/api/admin/config-agent/execute',
    '/api/admin/config-agent/history',
    '/api/admin/config-agent/catalog',
    'session_turns',
    'session_expires_at',
    'data-ca-catalog-fav',
    'config-agent-diagnose-support-pack',
    'config-agent-command-prefs'
  ].forEach(function(marker) {
    if (!ca.includes(marker)) {
      fail('overview-config-agent.js is missing marker: ' + marker);
    }
  });
  // ASCII-only source (CJK via \\u escapes). File header requires this.
  for (var i = 0; i < ca.length; i++) {
    if (ca.charCodeAt(i) > 127) {
      fail('overview-config-agent.js must stay ASCII-only (use \\u escapes); non-ASCII at offset ' + i);
      break;
    }
  }
  // Ensure critical globals are exported for bootstrap.
  ['global.initConfigAgent', 'global.submitConfigAgent', 'global.maybeShowSystemFreeGate'].forEach(function(marker) {
    if (!ca.includes(marker)) {
      fail('overview-config-agent.js must export runtime hook: ' + marker);
    }
  });
  // Guard against accidental double-binding of catalog search without debounce flag.
  if (!ca.includes('catalogSearchTimer') || !ca.includes('lastCatalogExampleByName')) {
    fail('overview-config-agent.js is missing catalog filter performance helpers');
  }
}

function assertAdminUiDialogTests() {
  const testFile = path.join(root, 'admin-ui-dialog.test.js');
  if (!fs.existsSync(testFile)) {
    fail('admin-ui-dialog.test.js is missing');
    return;
  }
  const { spawnSync } = require('child_process');
  // Test file resolves admin-ui.js via __dirname; cwd only affects relative paths in the harness.
  const result = spawnSync(process.execPath, [testFile], {
    encoding: 'utf8',
    cwd: root
  });
  if (result.status !== 0) {
    fail('admin-ui-dialog.test.js failed:\n' + String(result.stdout || '') + String(result.stderr || ''));
  }
}

assertAdminUiDialogTests();

if (!process.exitCode) {
  console.log('Admin module validation passed.');
}
