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
  assert(source.includes('\\u4e91\\u7aef\\u4efb\\u52a1\\u5171\\u7528'), 'hint says cloud tasks share this record');
  assert(source.includes('127.0.0.1'), 'hint names the loopback address');

  const elements = {
    maclawsrvConnectionTitle: element('maclawsrvConnectionTitle'),
    maclawsrvConnectionHint: element('maclawsrvConnectionHint'),
    maclawsrvBaseUrlLabel: element('maclawsrvBaseUrlLabel'),
    maclawsrvAdminSecretLabel: element('maclawsrvAdminSecretLabel'),
    maclawsrvAccessTokenLabel: element('maclawsrvAccessTokenLabel'),
    maclawsrvConnectionSaveBtn: element('maclawsrvConnectionSaveBtn'),
    maclawsrvConnectionTestBtn: element('maclawsrvConnectionTestBtn'),
    maclawsrvConnectionReloadBtn: element('maclawsrvConnectionReloadBtn'),
    maclawsrvConnectionStatus: element('maclawsrvConnectionStatus'),
    maclawsrvBaseUrl: element('maclawsrvBaseUrl', 'http://10.0.0.8:18080'),
    maclawsrvAdminSecret: element('maclawsrvAdminSecret', ''),
    maclawsrvAccessToken: element('maclawsrvAccessToken', '')
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

  elements.maclawsrvBaseUrl.value = 'http://10.0.0.8:18080';
  await context.saveMaclawsrvConnection();
  const puts = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(puts.length === 1, 'blank secret still saves the shared record');
  assert(puts[0].url === '/api/admin/bots/settings', 'save uses the bot settings record');
  const body = JSON.parse(puts[0].opts.body);
  assert(body.base_url === 'http://10.0.0.8:18080', 'non-loopback URL is saved for bots');
  assert(!Object.prototype.hasOwnProperty.call(body, 'admin_secret'), 'blank admin secret is omitted');
  assert(!Object.prototype.hasOwnProperty.call(body, 'access_token'), 'blank access token is omitted');

  elements.maclawsrvAdminSecret.value = 'new-secret';
  elements.maclawsrvAccessToken.value = 'new-token';
  await context.saveMaclawsrvConnection();
  const written = calls.filter(function (call) { return call.opts && call.opts.method === 'PUT'; });
  assert(written.length === 2, 'a typed secret is saved');
  const second = JSON.parse(written[1].opts.body);
  assert(second.admin_secret === 'new-secret', 'typed admin secret is written');
  assert(second.access_token === 'new-token', 'typed access token is written');

  const before = calls.length;
  elements.maclawsrvBaseUrl.value = 'not a url';
  await context.saveMaclawsrvConnection();
  assert(calls.length === before, 'a bad URL does not write');
  assert(toasts.length > 0, 'a bad URL is reported');

  elements.maclawsrvBaseUrl.value = 'http://127.0.0.1:18080';
  elements.maclawsrvAdminSecret.value = 'draft';
  elements.maclawsrvAccessToken.value = '';
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

  state.testError = 'MaClawSrv rejected the instance request (HTTP 404; endpoint not found, check the service URL): not found';
  await context.testMaclawsrvConnection();
  assert(elements.maclawsrvConnectionStatus.textContent.indexOf('HTTP 404') !== -1, 'explains a wrong URL in Chinese');

  console.log(passed + ' passed, ' + failed + ' failed');
  if (failed) process.exit(1);
}

main().catch(function (err) {
  console.error(err);
  process.exit(1);
});
