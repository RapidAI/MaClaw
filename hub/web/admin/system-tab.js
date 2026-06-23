/*
 * System admin module.
 * ASCII only.
 */
const TLS_I18N = {
  en: {
    title: 'TLS / HTTPS',
    desc: 'Configure HTTPS and WebSocket encryption',
    reload: 'Reload',
    enable: 'Enable TLS',
    unknown: 'Unknown',
    certFile: 'Certificate File',
    keyFile: 'Key File',
    save: 'Save TLS',
    saving: 'Saving...',
    savedWaiting: 'Saved, waiting for restart...',
    savedRestart: 'Save and Restart',
    guideTitle: 'TLS Guide',
    guideContent: 'Hub listens on port 9399 for HTTPS/WSS when TLS is enabled.<br>Recommended: ECDSA P-256 certificate.<br>Use https:// and wss:// after enabling.<br>Alternatively, use nginx as a reverse proxy.',
    nginxTitle: 'Nginx Example',
    statusValidUntil: 'Valid until {date}',
    statusExpired: 'Expired',
    statusNotGenerated: 'Not generated',
    certExpiredHint: 'The certificate has expired and will be regenerated automatically after enabling TLS.',
    certGenerateHint: 'A self-signed certificate will be generated automatically the first time TLS is enabled.',
    loadFailed: 'Load TLS config failed: {error}',
    confirmEnable: 'Enable TLS? The process will restart automatically and the page may be unavailable for a short time.',
    confirmDisable: 'Disable TLS? The process will restart automatically and the page may be unavailable for a short time.',
    restartMessage: 'TLS {action}, the process is restarting. Please visit later: {url}',
    restartActionEnable: 'enabled',
    restartActionDisable: 'disabled',
    restartVisit: 'Visit the new address: {url}',
    saveFailed: 'Save TLS config failed: {error}',
    sans: 'SANs: {value}',
    statusOkBadge: '[OK]',
    statusExpiredBadge: '[EXPIRED]',
    statusPendingBadge: '[PENDING]'
  },
  zh: {
    title: 'TLS / HTTPS',
    desc: '\u914d\u7f6e HTTPS \u4e0e WebSocket \u52a0\u5bc6',
    reload: '\u5237\u65b0',
    enable: '\u542f\u7528 TLS',
    unknown: '\u672a\u77e5',
    certFile: '\u8bc1\u4e66\u6587\u4ef6',
    keyFile: '\u79c1\u94a5\u6587\u4ef6',
    save: '\u4fdd\u5b58 TLS',
    saving: '\u4fdd\u5b58\u4e2d...',
    savedWaiting: '\u5df2\u4fdd\u5b58\uff0c\u7b49\u5f85\u91cd\u542f...',
    savedRestart: '\u4fdd\u5b58\u5e76\u91cd\u542f',
    guideTitle: 'TLS \u6307\u5357',
    guideContent: '\u542f\u7528 TLS \u540e\uff0cHub \u4f1a\u5728 9399 \u7aef\u53e3\u76d1\u542c HTTPS/WSS\u3002<br>\u5efa\u8bae\uff1aECDSA P-256 \u8bc1\u4e66\u3002<br>\u542f\u7528\u540e\u8bf7\u4f7f\u7528 https:// \u548c wss://\u3002<br>\u4e5f\u53ef\u4f7f\u7528 nginx \u4f5c\u4e3a\u53cd\u5411\u4ee3\u7406\u3002',
    nginxTitle: 'Nginx \u793a\u4f8b',
    statusValidUntil: '\u6709\u6548\u81f3 {date}',
    statusExpired: '\u5df2\u8fc7\u671f',
    statusNotGenerated: '\u672a\u751f\u6210',
    certExpiredHint: '\u8bc1\u4e66\u5df2\u8fc7\u671f\uff0c\u542f\u7528\u540e\u5c06\u81ea\u52a8\u91cd\u65b0\u751f\u6210\u3002',
    certGenerateHint: '\u9996\u6b21\u542f\u7528\u65f6\u5c06\u81ea\u52a8\u751f\u6210\u81ea\u7b7e\u540d\u8bc1\u4e66\u3002',
    loadFailed: '\u52a0\u8f7d TLS \u914d\u7f6e\u5931\u8d25: {error}',
    confirmEnable: '\u786e\u5b9a\u542f\u7528 TLS\uff1f\u8fdb\u7a0b\u5c06\u81ea\u52a8\u91cd\u542f\uff0c\u9875\u9762\u4f1a\u77ed\u6682\u4e0d\u53ef\u7528\u3002',
    confirmDisable: '\u786e\u5b9a\u5173\u95ed TLS\uff1f\u8fdb\u7a0b\u5c06\u81ea\u52a8\u91cd\u542f\uff0c\u9875\u9762\u4f1a\u77ed\u6682\u4e0d\u53ef\u7528\u3002',
    restartMessage: 'TLS \u5df2{action}\uff0c\u8fdb\u7a0b\u6b63\u5728\u91cd\u542f\u3002\u8bf7\u7a0d\u540e\u8bbf\u95ee\uff1a{url}',
    restartActionEnable: '\u542f\u7528',
    restartActionDisable: '\u5173\u95ed',
    restartVisit: '\u70b9\u51fb\u8bbf\u95ee\u65b0\u5730\u5740: {url}',
    saveFailed: '\u4fdd\u5b58 TLS \u914d\u7f6e\u5931\u8d25: {error}',
    sans: 'SANs: {value}',
    statusOkBadge: '[\u6b63\u5e38]',
    statusExpiredBadge: '[\u5df2\u8fc7\u671f]',
    statusPendingBadge: '[\u5f85\u751f\u6210]'
  }
};
const tlsx = (key, vars = {}) => ((TLS_I18N[currentLang] || TLS_I18N.en)[key] || TLS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
function applyTLSI18n() {
  _s('tlsTitle', 'textContent', tlsx('title'));
  _s('tlsDesc', 'textContent', tlsx('desc'));
  _s('tlsReloadBtn', 'textContent', tlsx('reload'));
  _s('tlsEnabledLabel', 'textContent', tlsx('enable'));
  _s('tlsCertFileLabel', 'textContent', tlsx('certFile'));
  _s('tlsKeyFileLabel', 'textContent', tlsx('keyFile'));
  _s('tlsSaveBtn', 'textContent', tlsx('save'));
  _s('tlsGuideTitle', 'textContent', tlsx('guideTitle'));
  _s('tlsGuideContent', 'innerHTML', tlsx('guideContent'));
  _s('tlsNginxTitle', 'textContent', tlsx('nginxTitle'));
}
async function loadTlsConfig() {
  applyTLSI18n();
  try {
    const data = await api('/api/admin/tls_config');
    document.getElementById('tlsEnabled').checked = !!data.enabled;
    document.getElementById('tlsCertFile').textContent = data.cert_file || '-';
    document.getElementById('tlsKeyFile').textContent = data.key_file || '-';
    const badge = document.getElementById('tlsCertBadge');
    const info = document.getElementById('tlsCertInfo');
    if (data.cert_valid) {
      const expiry = new Date(data.cert_expiry).toLocaleDateString();
      badge.textContent = tlsx('statusOkBadge') + ' ' + tlsx('statusValidUntil', { date: expiry });
      badge.className = 'badge ok';
      info.innerHTML = '<div class="item-meta">' + escapeHtml(tlsx('sans', { value: data.cert_sans || '-' })) + '</div>';
    } else if (data.cert_expiry) {
      badge.textContent = tlsx('statusExpiredBadge') + ' ' + tlsx('statusExpired');
      badge.className = 'badge danger';
      info.innerHTML = '<div class="item-meta" style="color:var(--danger)">' + escapeHtml(tlsx('certExpiredHint')) + '</div>';
    } else {
      badge.textContent = tlsx('statusPendingBadge') + ' ' + tlsx('statusNotGenerated');
      badge.className = 'badge info';
      info.innerHTML = '<div class="item-meta">' + escapeHtml(tlsx('certGenerateHint')) + '</div>';
    }
  } catch (err) {
    const msg = tlsx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

const ROUTING_I18N = {
  en: {
    title: 'Work Mode',
    desc: 'Choose whether this hub routes enterprise email domains or accepts public signups.',
    reload: 'Reload',
    workMode: 'Work Mode',
    enterpriseMode: 'Enterprise Routing',
    publicMode: 'Public Signup',
    workModeHintEnterprise: 'Enterprise email domains can be configured only in Enterprise Routing mode.',
    workModeHintPublic: 'Public Signup mode accepts users outside enterprise domains; enterprise email domains are disabled.',
    primaryDomain: 'Primary Corporate Email Domain',
    primaryPlaceholder: 'rapidai.tech',
    domains: 'Corporate Email Domains',
    domainsPlaceholder: 'rapidai.tech, subsidiary.example',
    domainsHint: 'Comma or newline separated. The first domain is treated as the primary route when the primary field is empty.',
    save: 'Save Routing',
    saving: 'Saving...',
    savedButton: 'Saved',
    loadFailed: 'Load work mode failed: {error}',
    saveFailed: 'Save work mode failed: {error}',
    saved: 'Work mode saved.'
  },
  zh: {
    title: '\u5de5\u4f5c\u6a21\u5f0f',
    desc: '\u9009\u62e9\u6b64 Hub \u662f\u627f\u63a5\u4f01\u4e1a\u90ae\u7bb1\u8def\u7531\uff0c\u8fd8\u662f\u5141\u8bb8\u6563\u5ba2\u6ce8\u518c\u3002',
    reload: '\u5237\u65b0',
    workMode: '\u5de5\u4f5c\u6a21\u5f0f',
    enterpriseMode: '\u4f01\u4e1a\u8def\u7531',
    publicMode: '\u6563\u5ba2\u6ce8\u518c',
    workModeHintEnterprise: '\u4ec5\u5728\u4f01\u4e1a\u8def\u7531\u6a21\u5f0f\u4e0b\u53ef\u8bbe\u7f6e\u4f01\u4e1a\u90ae\u4ef6\u57df\u540d\u3002',
    workModeHintPublic: '\u6563\u5ba2\u6ce8\u518c\u6a21\u5f0f\u4f1a\u627f\u63a5\u975e\u4f01\u4e1a\u57df\u540d\u7528\u6237\uff0c\u4f01\u4e1a\u90ae\u4ef6\u57df\u540d\u4e0d\u53ef\u7f16\u8f91\u3002',
    primaryDomain: '\u4e3b\u4f01\u4e1a\u90ae\u7bb1\u57df\u540d',
    primaryPlaceholder: 'rapidai.tech',
    domains: '\u4f01\u4e1a\u90ae\u7bb1\u57df\u540d\u5217\u8868',
    domainsPlaceholder: 'rapidai.tech, subsidiary.example',
    domainsHint: '\u652f\u6301\u9017\u53f7\u6216\u6362\u884c\u5206\u9694\uff0c\u5f53\u4e3b\u57df\u540d\u4e3a\u7a7a\u65f6\u53d6\u5217\u8868\u7684\u7b2c\u4e00\u4e2a\u4f5c\u4e3a\u4e3b\u8def\u7531\u57df\u540d\u3002',
    save: '\u4fdd\u5b58\u8def\u7531\u914d\u7f6e',
    saving: '\u4fdd\u5b58\u4e2d...',
    savedButton: '\u5df2\u4fdd\u5b58',
    loadFailed: '\u52a0\u8f7d\u5de5\u4f5c\u6a21\u5f0f\u5931\u8d25: {error}',
    saveFailed: '\u4fdd\u5b58\u5de5\u4f5c\u6a21\u5f0f\u5931\u8d25: {error}',
    saved: '\u5de5\u4f5c\u6a21\u5f0f\u5df2\u4fdd\u5b58\u3002'
  }
};
const srx = (key, vars = {}) => ((ROUTING_I18N[currentLang] || ROUTING_I18N.en)[key] || ROUTING_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const TENANT_MAIL_SENDER_I18N = {
  en: {
    title: 'Mail Sender Name',
    desc: 'Tenant admins can set the sender display name only. SMTP server, sender email, and test mail remain global admin settings.',
    reload: 'Reload',
    label: 'Sender display name',
    hint: 'Used as this tenant display name when tenant-scoped mail is sent.',
    save: 'Save Sender Name',
    saved: 'Sender name saved.',
    loadFailed: 'Load sender name failed: {error}',
    saveFailed: 'Save sender name failed: {error}'
  },
  zh: {
    title: '\u90ae\u4ef6\u53d1\u4ef6\u4eba\u540d\u79f0',
    desc: '\u79df\u6237\u7ba1\u7406\u5458\u53ea\u80fd\u8bbe\u7f6e\u53d1\u4ef6\u4eba\u5c55\u793a\u540d\u79f0\u3002SMTP \u670d\u52a1\u3001\u53d1\u4ef6\u90ae\u7bb1\u548c\u6d4b\u8bd5\u90ae\u4ef6\u4ecd\u7531\u5168\u5c40\u7ba1\u7406\u5458\u914d\u7f6e\u3002',
    reload: '\u5237\u65b0',
    label: '\u53d1\u4ef6\u4eba\u5c55\u793a\u540d\u79f0',
    hint: '\u7528\u4e8e\u79df\u6237\u8303\u56f4\u90ae\u4ef6\u7684\u53d1\u4ef6\u4eba\u5c55\u793a\u540d\u79f0\u3002',
    save: '\u4fdd\u5b58\u53d1\u4ef6\u4eba\u540d\u79f0',
    saved: '\u53d1\u4ef6\u4eba\u540d\u79f0\u5df2\u4fdd\u5b58\u3002',
    loadFailed: '\u52a0\u8f7d\u53d1\u4ef6\u4eba\u540d\u79f0\u5931\u8d25: {error}',
    saveFailed: '\u4fdd\u5b58\u53d1\u4ef6\u4eba\u540d\u79f0\u5931\u8d25: {error}'
  }
};
const tmsx = (key, vars = {}) => ((TENANT_MAIL_SENDER_I18N[currentLang] || TENANT_MAIL_SENDER_I18N.en)[key] || TENANT_MAIL_SENDER_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const TENANT_MAIL_SENDER_MAX_RUNES = 80;
const TENANT_MIGRATION_SETTINGS_I18N = {
  en: {
    title: 'Migration Package Limit',
    desc: 'Limit the compressed user migration package stored temporarily on this tenant hub.',
    reload: 'Reload',
    label: 'Max compressed size (MB)',
    hint: 'Allowed range: 100 MB to 1024 MB. New maclaw exports that exceed this tenant limit are rejected.',
    save: 'Save Limit',
    saving: 'Saving...',
    saved: 'Migration package limit saved.',
    loadFailed: 'Load migration package limit failed: {error}',
    saveFailed: 'Save migration package limit failed: {error}',
    invalid: 'Please enter a value from 100 to 1024 MB.'
  },
  zh: {
    title: '\u8fc1\u79fb\u5305\u5927\u5c0f\u4e0a\u9650',
    desc: '\u9650\u5236\u6b64\u79df\u6237 Hub \u4e0a\u4e34\u65f6\u4fdd\u5b58\u7684\u7528\u6237\u8fc1\u79fb\u538b\u7f29\u5305\u5927\u5c0f\u3002',
    reload: '\u5237\u65b0',
    label: '\u538b\u7f29\u540e\u6700\u5927\u5927\u5c0f\uff08MB\uff09',
    hint: '\u53ef\u8bbe\u7f6e\u8303\u56f4\uff1a100 MB \u5230 1024 MB\u3002\u65b0\u7684 maclaw \u8fc1\u51fa\u5305\u8d85\u8fc7\u6b64\u79df\u6237\u4e0a\u9650\u65f6\u4f1a\u88ab\u62d2\u7edd\u3002',
    save: '\u4fdd\u5b58\u4e0a\u9650',
    saving: '\u4fdd\u5b58\u4e2d...',
    saved: '\u8fc1\u79fb\u5305\u5927\u5c0f\u4e0a\u9650\u5df2\u4fdd\u5b58\u3002',
    loadFailed: '\u52a0\u8f7d\u8fc1\u79fb\u5305\u4e0a\u9650\u5931\u8d25\uff1a{error}',
    saveFailed: '\u4fdd\u5b58\u8fc1\u79fb\u5305\u4e0a\u9650\u5931\u8d25\uff1a{error}',
    invalid: '\u8bf7\u8f93\u5165 100 \u5230 1024 MB \u4e4b\u95f4\u7684\u6574\u6570\u3002'
  }
};
const tmgx = (key, vars = {}) => ((TENANT_MIGRATION_SETTINGS_I18N[currentLang] || TENANT_MIGRATION_SETTINGS_I18N.en)[key] || TENANT_MIGRATION_SETTINGS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const TENANT_SYSTEM_LLM_DEFAULTS_I18N = {
  en: {
    title: 'System Default LLM Service Group',
    desc: 'Approval workflow draft generation and other Hub system features use this service group.',
    reload: 'Reload',
    label: 'Default service group',
    emptyOption: 'Select a service group',
    noGroups: 'No model service groups found. Create a model service group first.',
    noUsableGroups: 'Model service groups exist, but none have a usable LLM route. Add a configured provider route in Model Services first.',
    hint: 'Used by approval workflow natural-language draft generation. It is independent from new-user benefits.',
    invalidSelected: 'Current setting is unavailable: {id}. Select another usable service group.',
    invalidSelectedOption: 'Current setting unavailable ({id})',
    save: 'Save Default LLM',
    saving: 'Saving...',
    saved: 'System default LLM service group saved.',
    required: 'Select a system default LLM service group first.',
    loadFailed: 'Load system default LLM service group failed: {error}',
    saveFailed: 'Save system default LLM service group failed: {error}'
  },
  zh: {
    title: '\u7cfb\u7edf\u9ed8\u8ba4 LLM \u670d\u52a1\u7ec4',
    desc: '\u5ba1\u6279\u5de5\u4f5c\u6d41\u8349\u7a3f\u751f\u6210\u7b49 Hub \u7cfb\u7edf\u80fd\u529b\u4f7f\u7528\u8fd9\u4e2a\u670d\u52a1\u7ec4\u3002',
    reload: '\u5237\u65b0',
    label: '\u9ed8\u8ba4\u670d\u52a1\u7ec4',
    emptyOption: '\u9009\u62e9\u670d\u52a1\u7ec4',
    noGroups: '\u6682\u65e0\u6a21\u578b\u670d\u52a1\u7ec4\u3002\u8bf7\u5148\u521b\u5efa\u6a21\u578b\u670d\u52a1\u7ec4\u3002',
    noUsableGroups: '\u5df2\u6709\u6a21\u578b\u670d\u52a1\u7ec4\uff0c\u4f46\u6ca1\u6709\u53ef\u7528\u7684 LLM \u8def\u7531\u3002\u8bf7\u5148\u5728\u6a21\u578b\u670d\u52a1\u4e2d\u6dfb\u52a0\u5df2\u914d\u7f6e\u7684\u63d0\u4f9b\u5546\u8def\u7531\u3002',
    hint: '\u7528\u4e8e\u5ba1\u6279\u5de5\u4f5c\u6d41\u81ea\u7136\u8bed\u8a00\u8349\u7a3f\u751f\u6210\uff0c\u4e0e\u65b0\u7528\u6237\u798f\u5229\u4e92\u76f8\u72ec\u7acb\u3002',
    invalidSelected: '\u5f53\u524d\u914d\u7f6e\u4e0d\u53ef\u7528\uff1a{id}\u3002\u8bf7\u6539\u9009\u5176\u4ed6\u53ef\u7528\u670d\u52a1\u7ec4\u3002',
    invalidSelectedOption: '\u5f53\u524d\u914d\u7f6e\u4e0d\u53ef\u7528 ({id})',
    save: '\u4fdd\u5b58\u9ed8\u8ba4 LLM',
    saving: '\u4fdd\u5b58\u4e2d...',
    saved: '\u7cfb\u7edf\u9ed8\u8ba4 LLM \u670d\u52a1\u7ec4\u5df2\u4fdd\u5b58\u3002',
    required: '\u8bf7\u5148\u9009\u62e9\u7cfb\u7edf\u9ed8\u8ba4 LLM \u670d\u52a1\u7ec4\u3002',
    loadFailed: '\u52a0\u8f7d\u7cfb\u7edf\u9ed8\u8ba4 LLM \u670d\u52a1\u7ec4\u5931\u8d25: {error}',
    saveFailed: '\u4fdd\u5b58\u7cfb\u7edf\u9ed8\u8ba4 LLM \u670d\u52a1\u7ec4\u5931\u8d25: {error}'
  }
};
const tslx = (key, vars = {}) => ((TENANT_SYSTEM_LLM_DEFAULTS_I18N[currentLang] || TENANT_SYSTEM_LLM_DEFAULTS_I18N.en)[key] || TENANT_SYSTEM_LLM_DEFAULTS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const TENANT_MIGRATION_MIN_MB = 100;
const TENANT_MIGRATION_MAX_MB = 1024;
let tenantSystemLLMDefaultsCache = null;
let tenantSystemLLMProviderIDs = {};
function normalizeTenantMailSenderName(value) {
  return Array.from(String(value || '').trim()).slice(0, TENANT_MAIL_SENDER_MAX_RUNES).join('');
}
function applyTenantMailSenderI18n() {
  _s('tenantMailSenderTitle', 'textContent', tmsx('title'));
  _s('tenantMailSenderDesc', 'textContent', tmsx('desc'));
  _s('tenantMailSenderReloadBtn', 'textContent', tmsx('reload'));
  _s('tenantMailFromNameLabel', 'textContent', tmsx('label'));
  _s('tenantMailSenderHint', 'textContent', tmsx('hint'));
  _s('tenantMailSenderSaveBtn', 'textContent', tmsx('save'));
}
function applyTenantMigrationSettingsI18n() {
  _s('tenantMigrationSettingsTitle', 'textContent', tmgx('title'));
  _s('tenantMigrationSettingsDesc', 'textContent', tmgx('desc'));
  _s('tenantMigrationSettingsReloadBtn', 'textContent', tmgx('reload'));
  _s('tenantMigrationMaxMBLabel', 'textContent', tmgx('label'));
  _s('tenantMigrationSettingsHint', 'textContent', tmgx('hint'));
  _s('tenantMigrationSettingsSaveBtn', 'textContent', tmgx('save'));
}
function applyTenantSystemLLMDefaultsI18n() {
  _s('tenantSystemLLMDefaultsTitle', 'textContent', tslx('title'));
  _s('tenantSystemLLMDefaultsDesc', 'textContent', tslx('desc'));
  _s('tenantSystemLLMDefaultsReloadBtn', 'textContent', tslx('reload'));
  _s('tenantSystemDefaultLLMServiceGroupLabel', 'textContent', tslx('label'));
  _s('tenantSystemLLMDefaultsHint', 'textContent', tslx('hint'));
  _s('tenantSystemLLMDefaultsSaveBtn', 'textContent', tslx('save'));
  renderTenantSystemLLMDefaultOptions();
}
function tenantMigrationBytesToMB(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n) || n <= 0) return TENANT_MIGRATION_MIN_MB;
  return Math.round(n / (1024 * 1024));
}
function normalizeTenantMigrationMB(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return 0;
  return Math.round(n);
}
function normalizeSystemRoutingDomains(value) {
  return String(value || '')
    .split(/[\n,]/)
    .map(item => item.trim())
    .filter(Boolean);
}
function formatSystemRoutingDomains(value) {
  return normalizeSystemRoutingDomains(value).join('\n');
}
function applySystemRoutingI18n() {
  _s('systemRoutingTitle', 'textContent', srx('title'));
  _s('systemRoutingDesc', 'textContent', srx('desc'));
  _s('systemRoutingReloadBtn', 'textContent', srx('reload'));
  _s('systemWorkModeLabel', 'textContent', srx('workMode'));
  _s('systemWorkModeEnterprise', 'textContent', srx('enterpriseMode'));
  _s('systemWorkModePublic', 'textContent', srx('publicMode'));
  _s('systemCorporateEmailDomainLabel', 'textContent', srx('primaryDomain'));
  _s('systemCorporateEmailDomainsLabel', 'textContent', srx('domains'));
  _s('systemCorporateEmailDomainsHint', 'textContent', srx('domainsHint'));
  _s('systemRoutingSaveBtn', 'textContent', srx('save'));
  _s('systemCorporateEmailDomain', 'placeholder', srx('primaryPlaceholder'));
  _s('systemCorporateEmailDomains', 'placeholder', srx('domainsPlaceholder'));
  updateSystemRoutingModeState();
}
function systemRoutingIsEnterpriseMode() {
  const mode = document.getElementById('systemWorkMode');
  return !mode || mode.value !== 'public';
}
function updateSystemRoutingModeState() {
  const enterpriseMode = systemRoutingIsEnterpriseMode();
  const primary = document.getElementById('systemCorporateEmailDomain');
  const domains = document.getElementById('systemCorporateEmailDomains');
  if (primary) primary.disabled = !enterpriseMode;
  if (domains) domains.disabled = !enterpriseMode;
  _s('systemWorkModeHint', 'textContent', srx(enterpriseMode ? 'workModeHintEnterprise' : 'workModeHintPublic'));
}
async function loadSystemRoutingConfig() {
  applySystemRoutingI18n();
  try {
    const data = await api('/api/admin/center/status');
    const domains = Array.isArray(data.corporate_email_domains) ? data.corporate_email_domains.filter(Boolean) : [];
    document.getElementById('systemCorporateEmailDomain').value = data.corporate_email_domain || '';
    document.getElementById('systemCorporateEmailDomains').value = domains.length ? domains.join('\n') : formatSystemRoutingDomains(data.corporate_email_domain || '');
    document.getElementById('systemWorkMode').value = data.accept_public_signup ? 'public' : 'enterprise';
    updateSystemRoutingModeState();
    return data;
  } catch (err) {
    const msg = srx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    throw err;
  }
}
async function saveSystemRoutingConfig() {
  const btn = document.getElementById('systemRoutingSaveBtn');
  const previousLabel = btn ? btn.textContent : '';
  let savedOk = false;
  if (btn) { btn.disabled = true; btn.textContent = srx('saving'); }
  try {
    const current = await api('/api/admin/center/status');
    const enterpriseMode = systemRoutingIsEnterpriseMode();
    const payload = {
      base_url: current.base_url || '',
      public_base_url: current.public_base_url || '',
      visibility: current.visibility || 'private',
      enrollment_mode: current.enrollment_mode || 'open',
      accept_public_signup: !enterpriseMode
    };
    if (enterpriseMode) {
      let corporateDomains = normalizeSystemRoutingDomains(document.getElementById('systemCorporateEmailDomains').value);
      let primaryDomain = document.getElementById('systemCorporateEmailDomain').value.trim();
      if (!corporateDomains.length && primaryDomain) corporateDomains = [primaryDomain];
      if (!primaryDomain && corporateDomains.length) primaryDomain = corporateDomains[0];
      payload.corporate_email_domain = primaryDomain;
      payload.corporate_email_domains = corporateDomains;
    }
    await api('/api/admin/center/config', {
      method: 'POST',
      body: JSON.stringify(payload)
    });
    await loadSystemRoutingConfig();
    const msg = srx('saved');
    setOutput(msg);
    savedOk = true;
    if (btn) btn.textContent = srx('savedButton');
    showToast(msg, 'success');
  } catch (err) {
    const msg = srx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  } finally {
    if (btn) {
      const restore = function() {
        btn.disabled = false;
        btn.textContent = previousLabel || srx('save');
      };
      if (savedOk) setTimeout(restore, 900);
      else restore();
    }
  }
}

async function saveTlsConfig() {
  const enabled = document.getElementById('tlsEnabled').checked;
  const btn = document.getElementById('tlsSaveBtn');
  const action = enabled ? tlsx('restartActionEnable') : tlsx('restartActionDisable');
  if (!confirm(enabled ? tlsx('confirmEnable') : tlsx('confirmDisable'))) return;
  if (btn) { btn.disabled = true; btn.textContent = tlsx('saving'); }
  try {
    const data = await api('/api/admin/tls_config', { method: 'POST', body: JSON.stringify({ enabled }) });
    if (data.restarting) {
      const host = location.hostname;
      const port = location.port || (location.protocol === 'https:' ? '443' : '80');
      const newProto = enabled ? 'https:' : 'http:';
      const newUrl = newProto + '//' + host + ':' + port + '/admin/';
      const msg = tlsx('restartMessage', { action: action, url: newUrl });
      setOutput(msg);
      showToast(msg, 'info');
      if (btn) btn.textContent = tlsx('savedWaiting');
      setTimeout(function() {
        showToast(tlsx('restartVisit', { url: newUrl }), 'info');
        if (btn) {
          btn.disabled = false;
          btn.textContent = tlsx('savedRestart');
          btn.onclick = function() { location.href = newUrl; };
        }
      }, 4000);
    }
  } catch (err) {
    const msg = tlsx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    if (btn) { btn.disabled = false; btn.textContent = tlsx('savedRestart'); }
  }
}

if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(function() {
    applyTLSI18n();
    applySystemRoutingI18n();
    applyTenantMailSenderI18n();
    applyTenantMigrationSettingsI18n();
    applyTenantSystemLLMDefaultsI18n();
  });
}
applyTLSI18n();
applySystemRoutingI18n();
applyTenantMailSenderI18n();
applyTenantMigrationSettingsI18n();
applyTenantSystemLLMDefaultsI18n();
function findMailPreset(provider) { return MAIL_PRESETS[provider] || MAIL_PRESETS.custom; }
function detectMailProvider(cfg) { const host = String(cfg?.smtp_host || '').trim().toLowerCase(); const port = Number(cfg?.smtp_port || 0); const encryption = String(cfg?.smtp_encryption || '').trim().toLowerCase(); for (const [provider, preset] of Object.entries(MAIL_PRESETS)) { if (provider === 'custom') continue; if (host === preset.smtp_host && (!port || port === preset.smtp_port) && (!encryption || encryption === preset.smtp_encryption)) return provider; } return String(cfg?.provider || '').trim() || 'custom'; }
function renderMailConfig(cfg = {}) { const provider = detectMailProvider(cfg); document.getElementById('mailProvider').value = MAIL_PRESETS[provider] ? provider : 'custom'; document.getElementById('mailHost').value = cfg.smtp_host || ''; document.getElementById('mailPort').value = cfg.smtp_port ? String(cfg.smtp_port) : ''; document.getElementById('mailEncryption').value = cfg.smtp_encryption || 'auto'; document.getElementById('mailUsername').value = cfg.smtp_username || ''; document.getElementById('mailPassword').value = cfg.smtp_password || ''; document.getElementById('mailFromName').value = cfg.from_name || 'MaClaw Hub'; document.getElementById('mailFromEmail').value = cfg.from_email || cfg.smtp_username || ''; }
function applyMailPreset() { const provider = document.getElementById('mailProvider').value; const preset = findMailPreset(provider); if (provider !== 'custom') { document.getElementById('mailHost').value = preset.smtp_host || ''; document.getElementById('mailPort').value = preset.smtp_port ? String(preset.smtp_port) : ''; document.getElementById('mailEncryption').value = preset.smtp_encryption || 'auto'; if (!document.getElementById('mailFromEmail').value.trim()) document.getElementById('mailFromEmail').value = document.getElementById('mailUsername').value.trim(); } }
function collectMailConfig() { const host = document.getElementById('mailHost').value.trim(); const username = document.getElementById('mailUsername').value.trim(); const password = document.getElementById('mailPassword').value; const fromEmail = document.getElementById('mailFromEmail').value.trim(); const provider = document.getElementById('mailProvider').value || 'custom'; if (!host || !username || !password || !fromEmail) throw new Error(tr('mailRequiredFields')); const parsedPort = Number(document.getElementById('mailPort').value || 0); return { enabled: true, provider, smtp_host: host, smtp_port: parsedPort > 0 ? parsedPort : findMailPreset(provider).smtp_port || 587, smtp_encryption: document.getElementById('mailEncryption').value || 'auto', smtp_username: username, smtp_password: password, from_name: document.getElementById('mailFromName').value.trim() || 'MaClaw Hub', from_email: fromEmail }; }
async function loadMailConfig() { try { const data = await api('/api/admin/mail/config'); renderMailConfig(data || {}); } catch (err) { const msg = tr('mailConfigLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function saveMailConfig() { try { const payload = collectMailConfig(); const data = await api('/api/admin/mail/config', { method: 'POST', body: JSON.stringify(payload) }); renderMailConfig(data || payload); const msg = tr('mailConfigSaved'); setOutput(msg); showToast(msg, 'success'); return data || payload; } catch (err) { const msg = tr('mailConfigSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); throw err; } }
async function loadTenantMailSenderName() { applyTenantMailSenderI18n(); try { const data = await api('/api/admin/mail/sender-name'); const input = document.getElementById('tenantMailFromName'); if (input) input.value = (data && data.from_name) || ''; return data || {}; } catch (err) { const msg = tmsx('loadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function saveTenantMailSenderName() { try { const input = document.getElementById('tenantMailFromName'); const fromName = normalizeTenantMailSenderName(input ? input.value : ''); if (input) input.value = fromName; const data = await api('/api/admin/mail/sender-name', { method: 'POST', body: JSON.stringify({ from_name: fromName }) }); if (input) input.value = (data && data.from_name) || fromName; const msg = tmsx('saved'); setOutput(msg); showToast(msg, 'success'); return data || { from_name: fromName }; } catch (err) { const msg = tmsx('saveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); throw err; } }
async function loadTenantMigrationSettings() { applyTenantMigrationSettingsI18n(); try { const data = await api('/api/admin/migration/settings'); const input = document.getElementById('tenantMigrationMaxMB'); if (input) { input.min = String(tenantMigrationBytesToMB(data && data.min_bytes) || TENANT_MIGRATION_MIN_MB); input.max = String(tenantMigrationBytesToMB(data && data.max_bytes) || TENANT_MIGRATION_MAX_MB); input.value = String(tenantMigrationBytesToMB(data && data.max_compressed_bytes)); } return data || {}; } catch (err) { const msg = tmgx('loadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function saveTenantMigrationSettings() { const input = document.getElementById('tenantMigrationMaxMB'); const valueMB = normalizeTenantMigrationMB(input ? input.value : 0); if (valueMB < TENANT_MIGRATION_MIN_MB || valueMB > TENANT_MIGRATION_MAX_MB) { const msg = tmgx('invalid'); setOutput(msg); showToast(msg, 'error'); return; } const btn = document.getElementById('tenantMigrationSettingsSaveBtn'); const previousLabel = btn ? btn.textContent : ''; if (btn) { btn.disabled = true; btn.textContent = tmgx('saving'); } try { const data = await api('/api/admin/migration/settings', { method: 'PUT', body: JSON.stringify({ max_compressed_bytes: valueMB * 1024 * 1024 }) }); if (input) input.value = String(tenantMigrationBytesToMB(data && data.max_compressed_bytes)); const msg = tmgx('saved'); setOutput(msg); showToast(msg, 'success'); return data || {}; } catch (err) { const msg = tmgx('saveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); throw err; } finally { if (btn) { btn.disabled = false; btn.textContent = previousLabel || tmgx('save'); } } }
function tenantSystemLLMProviderIsConfigured(id) {
  const key = String(id || '').trim().toLowerCase();
  if (!key) return false;
  if (key === 'maclaw_official') return true;
  return !!tenantSystemLLMProviderIDs[key];
}
function tenantSystemLLMModelProviderIDs(model) {
  const ids = [];
  (model && model.provider_ids || []).forEach(function(id) {
    id = String(id || '').trim();
    if (id) ids.push(id);
  });
  (model && model.provider_configs || []).forEach(function(cfg) {
    const id = String(cfg && cfg.provider_id || '').trim();
    if (id) ids.push(id);
  });
  return ids;
}
function tenantSystemLLMUsableGroups(data) {
  return (data && data.model_service_groups || []).filter(function(group) {
    if (!group || !String(group.id || '').trim()) return false;
    return Array.isArray(group.models) && group.models.some(function(model) {
      return String(model && model.name || '').trim() && tenantSystemLLMModelProviderIDs(model).some(tenantSystemLLMProviderIsConfigured);
    });
  });
}
function renderTenantSystemLLMDefaultOptions() {
  const select = document.getElementById('tenantSystemDefaultLLMServiceGroup');
  if (!select) return;
  const data = tenantSystemLLMDefaultsCache || {};
  const selected = String(data.system_default_service_group_id || '').trim();
  const groups = tenantSystemLLMUsableGroups(data);
  const hasServiceGroups = (data.model_service_groups || []).some(function(group) { return group && String(group.id || '').trim(); });
  const selectedUsable = !!selected && groups.some(function(group) { return String(group && group.id || '').trim() === selected; });
  if (!groups.length) {
    const emptyMessage = hasServiceGroups ? tslx('noUsableGroups') : tslx('noGroups');
    select.innerHTML = '<option value="">' + escapeHtml(selected ? tslx('invalidSelectedOption', { id: selected }) : emptyMessage) + '</option>';
    select.disabled = true;
    _s('tenantSystemLLMDefaultsHint', 'textContent', selected ? tslx('invalidSelected', { id: selected }) : emptyMessage);
    return;
  }
  select.disabled = false;
  _s('tenantSystemLLMDefaultsHint', 'textContent', selected && !selectedUsable ? tslx('invalidSelected', { id: selected }) : tslx('hint'));
  select.innerHTML = '<option value="">' + escapeHtml(selected && !selectedUsable ? tslx('invalidSelectedOption', { id: selected }) : tslx('emptyOption')) + '</option>' + groups.map(function(group) {
    const id = String(group.id || '').trim();
    const name = String(group.name || id).trim();
    const label = name === id ? id : name + ' (' + id + ')';
    return '<option value="' + escapeHtml(id) + '"' + (id === selected ? ' selected' : '') + '>' + escapeHtml(label) + '</option>';
  }).join('');
}
async function loadTenantSystemLLMDefaults() {
  applyTenantSystemLLMDefaultsI18n();
  try {
    const results = await Promise.all([
      api('/api/admin/llm/services?include_cards=false'),
      api('/api/admin/llm/providers')
    ]);
    tenantSystemLLMDefaultsCache = results[0];
    tenantSystemLLMProviderIDs = {};
    (results[1] && results[1].providers || []).forEach(function(provider) {
      const id = String(provider && provider.id || '').trim().toLowerCase();
      if (id) tenantSystemLLMProviderIDs[id] = true;
    });
    renderTenantSystemLLMDefaultOptions();
    return tenantSystemLLMDefaultsCache || {};
  } catch (err) {
    const msg = tslx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    throw err;
  }
}
async function saveTenantSystemLLMDefaults() {
  const select = document.getElementById('tenantSystemDefaultLLMServiceGroup');
  const serviceGroupID = String(select && select.value || '').trim();
  if (!serviceGroupID) {
    const msg = tslx('required');
    setOutput(msg);
    showToast(msg, 'error');
    return;
  }
  if (!tenantSystemLLMDefaultsCache) {
    try {
      await loadTenantSystemLLMDefaults();
    } catch (err) {
      return;
    }
  }
  const btn = document.getElementById('tenantSystemLLMDefaultsSaveBtn');
  const previousLabel = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = tslx('saving'); }
  try {
    const payload = Object.assign({}, tenantSystemLLMDefaultsCache || {}, { system_default_service_group_id: serviceGroupID });
    tenantSystemLLMDefaultsCache = await api('/api/admin/llm/services?include_cards=false', { method: 'PUT', body: JSON.stringify(payload) });
    renderTenantSystemLLMDefaultOptions();
    const msg = tslx('saved');
    setOutput(msg);
    showToast(msg, 'success');
    return tenantSystemLLMDefaultsCache || {};
  } catch (err) {
    const msg = tslx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    throw err;
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = previousLabel || tslx('save'); }
  }
}
// Machines runtime moved to machines-tab.js
async function sendTestMail() { try { const email = document.getElementById('testMailEmail').value.trim(); if (!email) { const msg = tr('testRecipientRequired'); setOutput(msg); showToast(msg, 'error'); return; } await saveMailConfig(); const data = await api('/api/admin/mail/test', { method: 'POST', body: JSON.stringify({ email }) }); const msg = data.message || tr('mailSent'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = tr('mailFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function changeAdminPassword() { const currentPassword = document.getElementById('currentPasswordInput').value; const newPassword = document.getElementById('newPasswordInput').value; const confirmPassword = document.getElementById('confirmPasswordInput').value; if (!currentPassword || !newPassword) { const msg = tr('requestFailed'); setOutput(msg); showToast(msg, 'error'); return; } if (newPassword !== confirmPassword) { const msg = ptr('mismatch'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/password', { method: 'POST', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) }); if (data.access_token) localStorage.setItem(adminTokenKey, data.access_token); if (data.admin) setAdminProfile(data.admin); document.getElementById('currentPasswordInput').value = ''; document.getElementById('newPasswordInput').value = ''; document.getElementById('confirmPasswordInput').value = ''; refreshAdminHeader(); const msg = ptr('changed'); setOutput(msg); showToast(msg, 'success'); } catch (err) { setOutput(err.message); showToast(err.message, 'error'); } }
async function updateAdminProfile() { const email = document.getElementById('adminEmailInput').value.trim(); if (!email) { const msg = prf('required'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/profile', { method: 'POST', body: JSON.stringify({ email }) }); if (data.access_token) localStorage.setItem(adminTokenKey, data.access_token); if (data.admin) setAdminProfile(data.admin); refreshAdminHeader(); const msg = prf('saved'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = prf('failed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } }
