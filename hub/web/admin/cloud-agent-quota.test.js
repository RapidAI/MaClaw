'use strict';

const fs = require('fs');
const path = require('path');
const vm = require('vm');

let passed = 0;
let failed = 0;

function assert(condition, message) {
  if (condition) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', message);
  }
}

function element(id, value) {
  return { id: id, value: value || '', checked: false, textContent: '', disabled: false };
}

async function main() {
  const dir = __dirname;
  const html = fs.readFileSync(path.join(dir, 'index.html'), 'utf8');
  const source = fs.readFileSync(path.join(dir, 'cloud-agent-quota.js'), 'utf8');
  const admin = fs.readFileSync(path.join(dir, 'admin.js'), 'utf8');
  assert(html.includes('id="cloudAgentQuotaCard"'), 'card is in the system tab');
  assert(html.includes('src="/admin/cloud-agent-quota.js"'), 'quota script is loaded');
  assert(admin.split('loadCloudAgentQuota').length >= 3, 'system tab loads the quota card for both scopes');
  assert(!/[\u0080-\uffff]/.test(source), 'quota script stays ASCII');
  const card = html.slice(html.indexOf('id="cloudAgentQuotaCard"'), html.indexOf('id="registrationAuthCard"'));
  assert(card && !/[\u0080-\uffff]/.test(card), 'quota card markup stays ASCII');
  assert(source.includes('\\u4e91\\u7aef Agent \\u9650\\u989d'), 'card title is 云端 Agent 限额');

  const elements = {
    cloudAgentQuotaTitle: element('cloudAgentQuotaTitle'),
    cloudAgentQuotaHint: element('cloudAgentQuotaHint'),
    cloudAgentQuotaEnabledLabel: element('cloudAgentQuotaEnabledLabel'),
    cloudAgentQuotaInstancesLabel: element('cloudAgentQuotaInstancesLabel'),
    cloudAgentQuotaTurnsLabel: element('cloudAgentQuotaTurnsLabel'),
    cloudAgentQuotaUserLabel: element('cloudAgentQuotaUserLabel'),
    cloudAgentQuotaUserInstancesLabel: element('cloudAgentQuotaUserInstancesLabel'),
    cloudAgentQuotaUserTurnsLabel: element('cloudAgentQuotaUserTurnsLabel'),
    cloudAgentQuotaSaveBtn: element('cloudAgentQuotaSaveBtn'),
    cloudAgentQuotaReloadBtn: element('cloudAgentQuotaReloadBtn'),
    cloudAgentQuotaStatus: element('cloudAgentQuotaStatus'),
    cloudAgentQuotaEnabled: element('cloudAgentQuotaEnabled'),
    cloudAgentQuotaInstances: element('cloudAgentQuotaInstances', '3'),
    cloudAgentQuotaTurns: element('cloudAgentQuotaTurns', '1'),
    cloudAgentQuotaUser: element('cloudAgentQuotaUser', 'user-7'),
    cloudAgentQuotaUserInstances: element('cloudAgentQuotaUserInstances', ''),
    cloudAgentQuotaUserTurns: element('cloudAgentQuotaUserTurns', '2')
  };
  const calls = [];
  const context = {
    document: { getElementById: function (id) { return elements[id] || null; } },
    api: async function (url, opts) {
      calls.push({ url: url, opts: opts || null });
      if (!opts) return { cloud_agents_enabled: false, max_instances: 3, max_concurrent_turns: 1 };
      return { ok: true };
    },
    showToast: function () {},
    getCurrentLanguage: function () { return 'zh'; },
    console: console
  };
  vm.createContext(context);
  vm.runInContext(source, context, { filename: 'cloud-agent-quota.js' });
  await context.loadCloudAgentQuota();
  assert(elements.cloudAgentQuotaTitle.textContent === '\u4e91\u7aef Agent \u9650\u989d', 'rendered title');
  elements.cloudAgentQuotaEnabled.checked = true;
  elements.cloudAgentQuotaInstances.value = '4';
  elements.cloudAgentQuotaTurns.value = '2';
  await context.saveCloudAgentQuota();
  const patch = calls.filter(function (call) { return call.opts && call.opts.method === 'PATCH'; });
  assert(patch.length === 2, 'tenant settings and user override both patch');
  const settings = patch[0];
  assert(settings.url === '/api/admin/cloud-agents/settings', 'settings path');
  const body = JSON.parse(settings.opts.body);
  assert(body.cloud_agents_enabled === true, 'switch is written');
  assert(body.max_instances === 4, 'instance limit is written');
  assert(body.max_concurrent_turns === 2, 'turn limit is written');
  console.log('PATCH ' + settings.url + ' ' + settings.opts.body);
  const user = patch[1];
  assert(user.url === '/api/admin/cloud-agents/users/user-7/limits', 'user override path');
  const userBody = JSON.parse(user.opts.body);
  assert(userBody.max_instances === null, 'blank user instance limit inherits');
  assert(userBody.max_concurrent_turns === 2, 'user turn limit is written');
  console.log(passed + ' passed, ' + failed + ' failed');
  if (failed) process.exit(1);
}

main().catch(function (err) {
  console.error(err);
  process.exit(1);
});
