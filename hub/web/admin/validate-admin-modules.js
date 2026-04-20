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
  'security-tab.js',
  'machines-tab.js',
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

expectedScripts.concat(['MODULES.md', 'check-admin.ps1']).forEach(assertExists);
expectedScripts.forEach(assertJavaScriptSyntax);
expectedScripts.forEach(assertModuleExports);
expectedScripts.concat(['MODULES.md', 'validate-admin-modules.js', 'check-admin.ps1']).forEach(assertAscii);
removedLegacyFiles.forEach(assertMissing);
assertScriptOrder();
assertHealthHook();
assertLegacyMirrorRemoved();

if (!process.exitCode) {
  console.log('Admin module validation passed.');
}