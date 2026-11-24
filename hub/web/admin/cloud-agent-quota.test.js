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
  return { id: id, value: value || '', checked: false, textContent: '', disabled: false, placeholder: '', innerHTML: '' };
}

async function main() {
  const dir = __dirname;
  const html = fs.readFileSync(path.join(dir, 'index.html'), 'utf8');
  const source = fs.readFileSync(path.join(dir, 'cloud-agent-quota.js'), 'utf8');
  const admin = fs.readFileSync(path.join(dir, 'admin.js'), 'utf8');
  const botsAt = html.indexOf('data-tab="bots"');
  const cloudAt = html.indexOf('data-tab="cloud-agents"');
  const marketAt = html.indexOf('data-tab="marketplace"');
  assert(botsAt > 0 && cloudAt > botsAt && marketAt > cloudAt, 'cloud agents sit after Bot management');
  assert(html.includes('src="/admin/cloud-agent-quota.js"'), 'quota script is loaded');
  assert(!admin.includes('loadCloudAgentQuota'), 'system tab no longer loads the quota card');
  assert(source.includes("id: 'cloud-agents'") && source.includes('onOpen'), 'opening the tab loads the page');
  assert(!/[\u0080-\uffff]/.test(source), 'quota script stays ASCII');
  const panel = html.slice(html.indexOf('id="tab-cloud-agents"'), html.indexOf('id="tab-notifications"'));
  assert(panel.includes('id="cloudAgentQuotaCard"'), 'global card is on the cloud agent page');
  assert(panel.includes('id="cloudAgentAccessTree"'), 'organization tree is on the cloud agent page');
  assert(!panel.includes('maclawsrvBaseUrl'), 'the cloud agent page does not edit the maclawsrv connection');
  assert(!/[\u0080-\uffff]/.test(panel), 'cloud agent page markup stays ASCII');
  const systemCard = html.slice(html.indexOf('id="maclawsrvConnectionCard"'), html.indexOf('id="registrationAuthCard"'));
  assert(systemCard && !systemCard.includes('cloudAgentQuotaCard'), 'system settings no longer hosts the quota card');
  assert(source.includes('\\u4e91\\u7aef Agent \\u9650\\u989d'), 'card title is kept');
  assert(!source.includes('cannot turn the feature on'), 'a department or user can open the feature');
  assert(source.includes('/api/admin/security/groups'), 'departments come from the organization tree');

  const elements = {
    navCloudAgents: element('navCloudAgents'),
    navCloudAgentsDesc: element('navCloudAgentsDesc'),
    cloudAgentQuotaTitle: element('cloudAgentQuotaTitle'),
    cloudAgentQuotaHint: element('cloudAgentQuotaHint'),
    cloudAgentQuotaEnabledLabel: element('cloudAgentQuotaEnabledLabel'),
    cloudAgentQuotaInstancesLabel: element('cloudAgentQuotaInstancesLabel'),
    cloudAgentQuotaTurnsLabel: element('cloudAgentQuotaTurnsLabel'),
    cloudAgentQuotaSaveBtn: element('cloudAgentQuotaSaveBtn'),
    cloudAgentQuotaReloadBtn: element('cloudAgentQuotaReloadBtn'),
    cloudAgentQuotaStatus: element('cloudAgentQuotaStatus'),
    cloudAgentQuotaEnabled: element('cloudAgentQuotaEnabled'),
    cloudAgentQuotaInstances: element('cloudAgentQuotaInstances', '3'),
    cloudAgentQuotaTurns: element('cloudAgentQuotaTurns', '1'),
    cloudAgentAccessTitle: element('cloudAgentAccessTitle'),
    cloudAgentAccessHint: element('cloudAgentAccessHint'),
    cloudAgentAccessReloadBtn: element('cloudAgentAccessReloadBtn'),
    cloudAgentAccessStatus: element('cloudAgentAccessStatus'),
    cloudAgentAccessFilter: element('cloudAgentAccessFilter'),
    cloudAgentAccessTree: element('cloudAgentAccessTree'),
    cloudAgentGrantsTitle: element('cloudAgentGrantsTitle'),
    cloudAgentGrantsHint: element('cloudAgentGrantsHint'),
    cloudAgentGrantsCount: element('cloudAgentGrantsCount'),
    cloudAgentGrantList: element('cloudAgentGrantList'),
    cloudAgentSrvList: element('cloudAgentSrvList'),
    cloudAgentSrvReloadBtn: element('cloudAgentSrvReloadBtn')
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
  assert(elements.cloudAgentQuotaHint.textContent.indexOf('\u7cfb\u7edf\u8bbe\u7f6e') !== -1, 'the page points at the system maclawsrv connection');
  assert(elements.navCloudAgents.textContent === '\u4e91\u7aef Agent', 'nav label is rendered');
  elements.cloudAgentQuotaEnabled.checked = true;
  elements.cloudAgentQuotaInstances.value = '4';
  elements.cloudAgentQuotaTurns.value = '2';
  await context.saveCloudAgentQuota();
  const patch = calls.filter(function (call) { return call.opts && call.opts.method === 'PATCH'; });
  assert(patch.length === 1, 'global settings are the only quota patch');
  assert(patch[0].url === '/api/admin/cloud-agents/settings', 'settings path');
  const body = JSON.parse(patch[0].opts.body);
  assert(body.cloud_agents_enabled === true, 'switch is written');
  assert(body.max_instances === 4, 'instance limit is written');
  assert(body.max_concurrent_turns === 2, 'turn limit is written');

  context.cagOrg.loaded = false;
  context.cagOrg.expanded = {};
  context.cagOrg.members = {};
  context.cagOrg.memberLoads = {};
  context.cagOrg.memberFailed = {};
  context.cagOrg.access = {};
  calls.length = 0;
  var accessItems = [{ scope: 'department', target_id: 'dept-1', enabled: 'on', max_instances: 2, max_concurrent_turns: null }];
  context.api = async function (url, opts) {
    calls.push({ url: url, opts: opts || null });
    if (opts && opts.method === 'PUT') {
      var saved = JSON.parse(opts.body);
      accessItems = accessItems.filter(function (item) { return !(item.scope === saved.scope && item.target_id === saved.target_id); });
      if (saved.enabled !== 'inherit' || saved.max_instances !== null || saved.max_concurrent_turns !== null) accessItems.push(saved);
      return { ok: true };
    }
    if (url === '/api/admin/cloud-agents/access') {
      return { items: accessItems };
    }
    if (url === '/api/admin/security/groups') {
      return { tree: [{ id: 'dept-1', name: 'Engineering', children: [{ id: 'dept-2', name: 'Platform', children: [] }] }] };
    }
    if (url === '/api/admin/users') return { users: [{ id: 'user-7', email: 'ada@example.com', emails: ['ada.alias@example.com'] }] };
    if (url.indexOf('/members') !== -1) return { members: ['ada@example.com', 'ada.alias@example.com'] };
    return { cloud_agents_enabled: false, max_instances: 3, max_concurrent_turns: 1 };
  };
  await context.loadCloudAgentAccess();
  const tree = elements.cloudAgentAccessTree.innerHTML;
  assert(tree.includes('Engineering'), 'department is shown in the tree');
  assert(tree.includes('data-scope="department"') && tree.includes('data-target="dept-1"'), 'department row keeps its id');
  assert(tree.includes('value="on" selected'), 'saved department is marked open');
  assert(tree.includes('value="inherit"') && tree.includes('value="off"'), 'inherit and off stay available');
  assert(tree.includes('ada@example.com') && tree.includes('data-target="user-7"'), 'a department member can be opened as a user');
  assert(tree.split('data-target="user-7"').length - 1 === 1, 'two emails of one account are one save row');
  await context.saveCloudAgentAccessRow('department', 'dept-1', 'inherit', null, null);
  const put = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(put.length === 1 && put[0].url === '/api/admin/cloud-agents/access', 'scope save uses the access route');
  const accessBody = JSON.parse(put[0].opts.body);
  assert(accessBody.scope === 'department' && accessBody.target_id === 'dept-1', 'department target is saved');
  assert(accessBody.enabled === 'inherit' && accessBody.max_instances === null && accessBody.max_concurrent_turns === null, 'a blank inherit row is cleared');
  assert(!elements.cloudAgentGrantList.innerHTML.includes('data-target='), 'a cleared row leaves the granted card');
  await context.saveCloudAgentAccessRow('department', 'dept-1', 'on', 2, null);

  const grants = elements.cloudAgentGrantList.innerHTML;
  assert(grants.includes('data-scope="department"') && grants.includes('data-target="dept-1"'), 'a granted department is listed in the granted card');
  assert(grants.includes('Engineering') && grants.includes('cag-state-on'), 'the grant shows its resolved name and on state');
  assert(grants.includes('cag-grant-revoke') && grants.includes('cag-grant-locate'), 'a grant can be revoked or located');
  assert(elements.cloudAgentGrantsTitle.textContent === '\u5df2\u5f00\u901a\u8bb0\u5f55', 'granted card title is rendered');
  calls.length = 0;
  await context.cagRevokeGrant('department', 'dept-1');
  const revoke = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(revoke.length === 1 && revoke[0].url === '/api/admin/cloud-agents/access', 'revoking a grant writes one access update');
  const revokeBody = JSON.parse(revoke[0].opts.body);
  assert(revokeBody.scope === 'department' && revokeBody.target_id === 'dept-1', 'revoke targets the granted scope');
  assert(revokeBody.enabled === 'inherit' && revokeBody.max_instances === null && revokeBody.max_concurrent_turns === null, 'revoke clears the saved override');
  assert(!elements.cloudAgentGrantList.innerHTML.includes('data-target='), 'an empty granted card shows the empty state');
  context.cagLocateGrant('department', 'dept-1');
  assert(elements.cloudAgentAccessFilter.value === 'Engineering', 'locating a grant fills the search');
  assert(elements.cloudAgentAccessTree.innerHTML.includes('data-target="dept-1"'), 'the located row is rendered');
  elements.cloudAgentAccessFilter.value = '';

  elements.cloudAgentAccessFilter.value = 'ada';
  context.renderCloudAgentAccessTree();
  var searched = elements.cloudAgentAccessTree.innerHTML;
  assert(searched.split('data-target="user-7"').length - 1 === 1, 'a user already shown in the tree is not listed again');
  assert(searched.indexOf('\u5339\u914d\u7684\u7528\u6237') === -1, 'the matching-users heading stays hidden when every hit is already in the tree');
  elements.cloudAgentAccessFilter.value = 'ada.alias';
  context.renderCloudAgentAccessTree();
  assert(elements.cloudAgentAccessTree.innerHTML.split('data-target="user-7"').length - 1 === 1, 'an alias finds that one account');
  context.cagOrg.users.push({ id: 'user-8', email: 'bea@example.com' });
  context.cagOrg.usersByEmail['bea@example.com'] = context.cagOrg.users[context.cagOrg.users.length - 1];
  elements.cloudAgentAccessFilter.value = 'bea';
  context.renderCloudAgentAccessTree();
  var extra = elements.cloudAgentAccessTree.innerHTML;
  assert(extra.indexOf('data-target="user-8"') !== -1, 'a person outside the open departments still appears in search');
  assert(extra.indexOf('\u5339\u914d\u7684\u7528\u6237') !== -1, 'search names the extra user list');
  elements.cloudAgentAccessFilter.value = '';

  elements.cloudAgentAccessFilter.value = 'eng';
  var expandedBefore = !!context.cagOrg.expanded['dept-1'];
  context.cagToggleDepartment('dept-1');
  assert(expandedBefore && context.cagOrg.expanded['dept-1'] === true, 'searching does not collapse a department that is already open');
  elements.cloudAgentAccessFilter.value = '';
  context.cagToggleDepartment('dept-1');
  assert(context.cagOrg.expanded['dept-1'] !== true, 'the toggle collapses a department when search is empty');
  context.cagOrg.expanded['dept-1'] = true;

  var savedMembers = context.cagOrg.members;
  var savedExpanded = context.cagOrg.expanded;
  context.cagOrg.members = {};
  context.cagOrg.expanded = {};
  elements.cloudAgentAccessFilter.value = 'n';
  context.renderCloudAgentAccessTree();
  var shortSearch = elements.cloudAgentAccessTree.innerHTML;
  assert(shortSearch.indexOf('Engineering') !== -1, 'a one-letter search still lists the department');
  assert(shortSearch.indexOf('data-target="user-7"') === -1, 'a one-letter search does not pretend the member list was loaded');
  assert(shortSearch.indexOf('\u6b63\u5728\u8bfb\u53d6\u6210\u5458') === -1, 'a one-letter search does not start a member load');
  context.cagOrg.members = savedMembers;
  context.cagOrg.expanded = savedExpanded;
  elements.cloudAgentAccessFilter.value = '';
  context.renderCloudAgentAccessTree();
  await context.saveCloudAgentAccessRow('department', 'dept-1', 'on', 2, null);

  elements.cloudAgentAccessTree.querySelectorAll = function () {
    return [{
      getAttribute: function (name) {
        if (name === 'data-scope') return 'department';
        if (name === 'data-target') return 'dept-1';
        return '';
      },
      querySelector: function (sel) {
        if (sel === '.cag-access-enabled') return { value: 'off' };
        if (sel === '.cag-access-instances') return { value: '9' };
        if (sel === '.cag-access-turns') return { value: '' };
        return null;
      }
    }];
  };
  context.renderCloudAgentAccessTree();
  var kept = elements.cloudAgentAccessTree.innerHTML;
  assert(kept.indexOf('value="off" selected') !== -1, 'a redraw keeps an unsaved scope choice');
  assert(kept.indexOf('value="9"') !== -1, 'a redraw keeps an unsaved instance limit');
  await context.loadCloudAgentAccess(true);
  var refreshed = elements.cloudAgentAccessTree.innerHTML;
  assert(refreshed.indexOf('value="on" selected') !== -1, 'reload shows the saved scope');
  assert(refreshed.indexOf('value="9"') === -1, 'reload drops an unsaved instance limit');
  assert(context.cagOrg.suspendDrafts === 0, 'reload finishes without keeping drafts suppressed');
  delete elements.cloudAgentAccessTree.querySelectorAll;

  function resetOrg() {
    context.cagOrg.loaded = false;
    context.cagOrg.expanded = {};
    context.cagOrg.members = {};
    context.cagOrg.memberLoads = {};
    context.cagOrg.memberFailed = {};
    context.cagOrg.access = {};
    context.cagOrg.tree = [];
    context.cagOrg.users = [];
    context.cagOrg.usersByEmail = {};
    context.cagOrg.usersById = {};
    elements.cloudAgentAccessTree.innerHTML = '';
    elements.cloudAgentAccessTree._cagDrawn = '';
    elements.cloudAgentAccessStatus.textContent = '';
  }

  resetOrg();
  var memberCalls = 0;
  var usersFail = true;
  context.api = async function (url) {
    if (url === '/api/admin/cloud-agents/access') return { items: [] };
    if (url === '/api/admin/security/groups') return { tree: [{ id: 'dept-1', name: 'Engineering', children: [] }] };
    if (url === '/api/admin/users') {
      if (usersFail) throw new Error('users down');
      return { users: [{ id: 'user-7', email: 'ada@example.com' }] };
    }
    if (url.indexOf('/members') !== -1) {
      memberCalls += 1;
      return { members: ['ada@example.com'] };
    }
    return {};
  };
  await context.loadCloudAgentAccess();
  assert(elements.cloudAgentAccessTree.innerHTML.indexOf('ada@example.com') !== -1, 'department membership still renders when accounts fail');
  assert(elements.cloudAgentAccessTree.innerHTML.indexOf('data-target="user-7"') === -1, 'an unresolved person has no save row');
  assert(elements.cloudAgentAccessStatus.textContent.indexOf('\u8d26\u53f7\u6ca1\u6709\u8bfb\u51fa\u6765') !== -1, 'the status says the account list failed');
  usersFail = false;
  await context.loadCloudAgentAccess();
  assert(memberCalls === 2, 'a later load reads the department membership again');
  assert(elements.cloudAgentAccessTree.innerHTML.indexOf('data-target="user-7"') !== -1, 'the same membership can be saved once accounts arrive');
  assert(elements.cloudAgentAccessStatus.textContent === '', 'a recovered account list clears the status');

  resetOrg();
  memberCalls = 0;
  context.api = async function (url) {
    if (url === '/api/admin/cloud-agents/access') return { items: [] };
    if (url === '/api/admin/security/groups') return { tree: [{ id: 'dept-1', name: 'Engineering', children: [] }] };
    if (url === '/api/admin/users') return { users: [{ id: 'user-7', email: 'ada@example.com' }] };
    if (url.indexOf('/members') !== -1) {
      memberCalls += 1;
      if (memberCalls === 1) throw new Error('members down');
      return { members: ['ada@example.com'] };
    }
    return {};
  };
  await context.loadCloudAgentAccess();
  assert(memberCalls === 1, 'a failed member load is attempted once');
  assert(elements.cloudAgentAccessTree.innerHTML.indexOf('\u6210\u5458\u6ca1\u6709\u8bfb\u51fa\u6765') !== -1, 'a failed member load is visible');
  assert(elements.cloudAgentAccessTree.innerHTML.indexOf('data-target="user-7"') === -1, 'a failed member load does not look like an empty department');
  await context.loadCloudAgentAccess();
  assert(memberCalls === 2, 'the next load retries a failed member fetch');
  assert(elements.cloudAgentAccessTree.innerHTML.indexOf('data-target="user-7"') !== -1, 'the retried member fetch can be saved');

  resetOrg();
  var toasts = [];
  var previousToast = context.showToast;
  context.showToast = function (msg, kind) { toasts.push(kind); };
  context.api = async function (url, opts) {
    if (opts && opts.method === 'PUT') return { ok: true };
    if (url === '/api/admin/cloud-agents/access') throw new Error('reload down');
    if (url === '/api/admin/security/groups') return { tree: [] };
    if (url === '/api/admin/users') return { users: [] };
    return {};
  };
  await context.saveCloudAgentAccessRow('department', 'dept-1', 'on', 1, 1);
  assert(toasts.indexOf('error') !== -1, 'a failed reload reports the error');
  assert(toasts.indexOf('success') === -1, 'a failed reload does not claim the scope was saved');
  context.showToast = previousToast;

  resetOrg();
  var writes = [];
  var openWrites = 0;
  var maxWrites = 0;
  context.api = async function (url, opts) {
    if (opts && opts.method === 'PUT') {
      openWrites += 1;
      if (openWrites > maxWrites) maxWrites = openWrites;
      writes.push(JSON.parse(opts.body).enabled);
      await Promise.resolve();
      openWrites -= 1;
      return { ok: true };
    }
    if (url === '/api/admin/cloud-agents/access') return { items: [] };
    if (url === '/api/admin/security/groups') return { tree: [] };
    if (url === '/api/admin/users') return { users: [] };
    return {};
  };
  var firstSave = context.saveCloudAgentAccessRow('user', 'user-7', 'on', 1, 1);
  var secondSave = context.saveCloudAgentAccessRow('user', 'user-7', 'off', 1, 1);
  await firstSave;
  await secondSave;
  assert(maxWrites === 1, 'overlapping scope saves do not run together');
  assert(writes.length === 2 && writes[0] === 'on' && writes[1] === 'off', 'the later scope save is the one that lands last');

  resetOrg();
  var releaseOldMembers;
  var memberWave = 0;
  context.api = async function (url) {
    if (url === '/api/admin/cloud-agents/access') {
      return { items: [{ scope: 'department', target_id: 'dept-1', enabled: memberWave === 0 ? 'off' : 'on', max_instances: null, max_concurrent_turns: null }] };
    }
    if (url === '/api/admin/security/groups') return { tree: [{ id: 'dept-1', name: 'Engineering', children: [] }] };
    if (url === '/api/admin/users') return { users: [{ id: 'user-7', email: 'ada@example.com' }] };
    if (url.indexOf('/members') !== -1) {
      memberWave += 1;
      if (memberWave === 1) {
        await new Promise(function (resolve) { releaseOldMembers = resolve; });
        return { members: ['old@example.com'] };
      }
      return { members: ['ada@example.com'] };
    }
    return {};
  };
  var olderLoad = context.loadCloudAgentAccess();
  for (var wait = 0; wait < 20 && !releaseOldMembers; wait += 1) await Promise.resolve();
  assert(typeof releaseOldMembers === 'function', 'the first membership read is still in flight');
  await context.loadCloudAgentAccess();
  releaseOldMembers();
  await olderLoad;
  var settled = elements.cloudAgentAccessTree.innerHTML;
  assert(settled.indexOf('ada@example.com') !== -1, 'the newer membership stays on screen');
  assert(settled.indexOf('old@example.com') === -1, 'a late membership response does not replace it');
  assert(settled.indexOf('value="on" selected') !== -1, 'a late scope response does not replace the newer one');

  context.cagOrg.usersById = { 'user-7': { id: 'user-7', email: 'ada@example.com' } };
  context.api = async function (url) {
    if (url === '/api/admin/cloud-agents/srv-bindings') {
      return { items: [
        { user_id: 'user-7', node_id: 'node-7', scope: 'private', name: 'ada-srv', address: '10.0.0.8:18080', online: true },
        { user_id: 'ghost', node_id: 'node-gone', scope: 'private' }
      ] };
    }
    return {};
  };
  await context.loadCloudAgentSrvBindings();
  var srvList = elements.cloudAgentSrvList.innerHTML;
  assert(srvList.indexOf('ada@example.com') !== -1, 'a bound user resolves to the account email');
  assert(srvList.indexOf('ada-srv') !== -1 && srvList.indexOf('10.0.0.8:18080') !== -1, 'node name and address are shown');
  assert(srvList.indexOf('在线') !== -1 && srvList.indexOf('离线') !== -1, 'online and offline states both render');
  assert(srvList.indexOf('node-gone') !== -1, 'a missing node still lists with its id');
  context.api = async function () { return { items: [] }; };
  await context.loadCloudAgentSrvBindings();
  assert(elements.cloudAgentSrvList.innerHTML.indexOf('全部使用系统缺省') !== -1 || elements.cloudAgentSrvList.innerHTML.indexOf('cag-grants-empty') !== -1, 'empty bindings explain the public default');
  console.log(passed + ' passed, ' + failed + ' failed');
  if (failed) process.exit(1);
}

main().catch(function (err) {
  console.error(err);
  process.exit(1);
});
