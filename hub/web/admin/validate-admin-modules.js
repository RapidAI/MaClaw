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

expectedScripts.concat(['MODULES.md', 'check-admin.ps1']).forEach(assertExists);
expectedScripts.forEach(assertJavaScriptSyntax);
expectedScripts.forEach(assertModuleExports);
expectedScripts.concat(['MODULES.md', 'validate-admin-modules.js', 'check-admin.ps1']).forEach(assertAscii);
removedLegacyFiles.forEach(assertMissing);
assertScriptOrder();
assertHealthHook();
assertLegacyMirrorRemoved();
assertLLMProviderPricingHooks();
assertSecurityCapabilityComplianceExportHooks();

if (!process.exitCode) {
  console.log('Admin module validation passed.');
}
