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
  return { id: id, value: value || '', textContent: '', disabled: false };
}

async function main() {
  const dir = __dirname;
  const html = fs.readFileSync(path.join(dir, 'index.html'), 'utf8');
  const system = fs.readFileSync(path.join(dir, 'system-tab.js'), 'utf8');
  const start = system.indexOf('const MACLAWSRV_I18N');
  assert(start > 0, 'system tab owns the maclawsrv connection copy');
  const source = system.slice(start);
  assert(!/[\u0080-\uffff]/.test(source), 'maclawsrv connection script stays ASCII');
  const card = html.slice(html.indexOf('id="maclawsrvConnectionCard"'), html.indexOf('id="registrationAuthCard"'));
  assert(card && !/[\u0080-\uffff]/.test(card), 'maclawsrv card markup stays ASCII');
  assert(card.includes('id="maclawsrvConnectionTestBtn"'), 'system settings can test the connection');
  assert(!card.includes('maclawsrvAccessToken'), 'system settings does not ask for an access token');
  assert(source.includes('\\u4e91\\u7aef\\u4efb\\u52a1\\u5171\\u7528'), 'hint says cloud tasks share this record');
  assert(source.includes('127.0.0.1'), 'hint names the loopback address');
  assert(source.includes('\\u4fdd\\u5b58\\u540e\\u81ea\\u52a8\\u521b\\u5efa\\u8fde\\u63a5\\u8d26\\u53f7'), 'admin secret hint matches bot management');
  assert(source.includes('function canManageMaclawsrv()'), 'maclawsrv calls are tenant-scoped');
  const admin = fs.readFileSync(path.join(dir, 'admin.js'), 'utf8');
  const tenantBranch = admin.slice(admin.indexOf("normalized === 'system'"), admin.indexOf('function restoreTab'));
  const tenantLoads = tenantBranch.slice(0, tenantBranch.indexOf('} else {'));
  const globalLoads = tenantBranch.slice(tenantBranch.indexOf('} else {'));
  assert(tenantLoads.includes('loadMaclawsrvConnection'), 'tenant system settings load the connection');
  assert(!globalLoads.includes('loadMaclawsrvConnection'), 'global system settings do not call the tenant route');
  const tenantTab = fs.readFileSync(path.join(dir, 'tenant-tab.js'), 'utf8');
  assert(tenantTab.includes("maclawsrvConnectionCard.classList.toggle('hidden', !tenantAdmin)"), 'global admins do not see the tenant connection card');

  const elements = {
    maclawsrvConnectionTitle: element('maclawsrvConnectionTitle'),
    maclawsrvConnectionHint: element('maclawsrvConnectionHint'),
    maclawsrvBaseUrlLabel: element('maclawsrvBaseUrlLabel'),
    maclawsrvAdminSecretLabel: element('maclawsrvAdminSecretLabel'),
    maclawsrvAdminSecretHint: element('maclawsrvAdminSecretHint'),
    maclawsrvConnectionSaveBtn: element('maclawsrvConnectionSaveBtn'),
    maclawsrvConnectionTestBtn: element('maclawsrvConnectionTestBtn'),
    maclawsrvConnectionReloadBtn: element('maclawsrvConnectionReloadBtn'),
    maclawsrvConnectionStatus: element('maclawsrvConnectionStatus'),
    maclawsrvBaseUrl: element('maclawsrvBaseUrl', 'http://10.0.0.8:18080'),
    maclawsrvAdminSecret: element('maclawsrvAdminSecret', '')
  };
  const calls = [];
  const toasts = [];
  const state = { testError: '' };
  const context = {
    document: { getElementById: function (id) { return elements[id] || null; } },
    api: async function (url, opts) {
      calls.push({ url: url, opts: opts || null });
      if (opts && opts.method === 'POST' && url === '/api/admin/bots/connection/test') {
        if (state.testError) throw new Error(state.testError);
        return { ok: true, instance_count: 3 };
      }
      if (!opts) return { base_url: 'http://127.0.0.1:18080', admin_secret_set: true, token_set: false };
      return { ok: true };
    },
    showToast: function (message) { toasts.push(message); },
    getCurrentLanguage: function () { return 'zh'; },
    URL: URL,
    console: console
  };
  vm.createContext(context);
  vm.runInContext(source, context, { filename: 'system-tab-maclawsrv.js' });
  await context.loadMaclawsrvConnection();
  assert(elements.maclawsrvConnectionTitle.textContent === 'maclawsrv \u63a5\u5165', 'rendered title');
  assert(elements.maclawsrvBaseUrl.value === 'http://127.0.0.1:18080', 'load fills the saved URL');
  assert(elements.maclawsrvAdminSecret.value === '', 'load does not echo the secret');
  assert(elements.maclawsrvAdminSecretHint.textContent.indexOf('\u81ea\u52a8\u521b\u5efa\u8fde\u63a5\u8d26\u53f7') !== -1, 'explains the admin secret provisions the account');
  assert(elements.maclawsrvConnectionStatus.textContent === '\u7ba1\u7406\u5bc6\u94a5\u5df2\u4fdd\u5b58', 'status reports the saved admin secret');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('\u8bbf\u95ee\u4ee4\u724c') === -1, 'status does not mention an access token');

  elements.maclawsrvBaseUrl.value = 'http://10.0.0.8:18080';
  await context.saveMaclawsrvConnection();
  const puts = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(puts.length === 1, 'blank secret still saves the shared record');
  assert(puts[0].url === '/api/admin/bots/settings', 'save uses the bot settings record');
  const body = JSON.parse(puts[0].opts.body);
  assert(body.base_url === 'http://10.0.0.8:18080', 'non-loopback URL is saved for bots');
  assert(!Object.prototype.hasOwnProperty.call(body, 'admin_secret'), 'blank admin secret is omitted');
  assert(!Object.prototype.hasOwnProperty.call(body, 'access_token'), 'save never sends an access token');

  elements.maclawsrvAdminSecret.value = 'new-secret';
  await context.saveMaclawsrvConnection();
  const written = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(written.length === 2, 'a typed secret is saved');
  const second = JSON.parse(written[1].opts.body);
  assert(second.admin_secret === 'new-secret', 'typed admin secret is written');
  assert(!Object.prototype.hasOwnProperty.call(second, 'access_token'), 'a typed secret does not send an access token');

  elements.maclawsrvBaseUrl.value = 'http://127.0.0.1:18080';
  elements.maclawsrvAdminSecret.value = '   ';
  const spacedBefore = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; }).length;
  await context.saveMaclawsrvConnection();
  const spaced = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(spaced.length === spacedBefore + 1, 'whitespace still saves the address');
  const spacedBody = JSON.parse(spaced[spaced.length - 1].opts.body);
  assert(!Object.prototype.hasOwnProperty.call(spacedBody, 'admin_secret'), 'whitespace does not replace the saved admin secret');

  const before = calls.length;
  elements.maclawsrvBaseUrl.value = 'not a url';
  await context.saveMaclawsrvConnection();
  assert(calls.length === before, 'a bad URL does not write');
  assert(toasts.length > 0, 'a bad URL is reported');

  elements.maclawsrvBaseUrl.value = 'http://127.0.0.1:18080';
  elements.maclawsrvAdminSecret.value = 'draft';
  const testsBefore = calls.filter(function (call) { return call.opts && call.opts.method === 'POST'; }).length;
  await context.testMaclawsrvConnection();
  assert(calls.filter(function (call) { return call.opts && call.opts.method === 'POST'; }).length === testsBefore, 'an unsaved secret is not tested');
  assert(toasts.some(function (toast) { return String(toast).indexOf('\u8fd8\u6ca1\u4fdd\u5b58') !== -1; }), 'asks to save before testing');

  elements.maclawsrvAdminSecret.value = '';
  await context.testMaclawsrvConnection();
  assert(calls.some(function (call) { return call.url === '/api/admin/bots/connection/test'; }), 'tests the saved connection');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('3') !== -1, 'reports the instance count');

  state.testError = 'MaClawSrv rejected the instance request (HTTP 401; the access token was rejected, re-save it in bot settings): unauthorized';
  await context.testMaclawsrvConnection();
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('HTTP 401') !== -1, 'names the status code');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('\u51ed\u8bc1') !== -1, 'explains a rejected credential in Chinese');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('unauthorized') !== -1, 'keeps what MaClawSrv said');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('the access token was rejected') === -1, 'drops the English hint');

  state.testError = 'save the MaClawSrv URL and admin secret first';
  await context.testMaclawsrvConnection();
  assert(elements.maclawsrvConnectionStatus.textContent === '\u8bf7\u5148\u4fdd\u5b58 maclawsrv \u5730\u5740\u548c\u7ba1\u7406\u5bc6\u94a5', 'an unconfigured test says to save the address and secret');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('\u7cfb\u7edf\u8bbe\u7f6e') === -1, 'the system settings page does not send the admin to itself');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('save the MaClawSrv') === -1, 'does not leave the unconfigured error in English');

  state.testError = '\u8bf7\u5148\u5728\u7cfb\u7edf\u8bbe\u7f6e\u91cc\u4fdd\u5b58 MaClawSrv \u7ba1\u7406\u5bc6\u94a5';
  await context.testMaclawsrvConnection();
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('\u7cfb\u7edf\u8bbe\u7f6e') === -1, 'a missing-secret hint is rewritten for this page');
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('\u4fdd\u5b58 maclawsrv') !== -1, 'a missing-secret hint still says to save');

  state.testError = 'MaClawSrv rejected the instance request (HTTP 404; endpoint not found, check the service URL): not found';
  await context.testMaclawsrvConnection();
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('HTTP 404') !== -1, 'explains a wrong URL in Chinese');

  const callsBeforeScope = calls.length;
  context.adminProfile = function () { return { scope: 'global' }; };
  await context.loadMaclawsrvConnection();
  await context.saveMaclawsrvConnection();
  await context.testMaclawsrvConnection();
  assert(calls.length === callsBeforeScope, 'a global admin does not call the tenant maclawsrv route');

  console.log(passed + ' passed, ' + failed + ' failed');
  if (failed) process.exit(1);
}

main().catch(function (err) {
  console.error(err);
  process.exit(1);
});
