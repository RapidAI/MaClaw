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
const REGISTRATION_AUTH_I18N = {
  en: {
    title: 'Registration Verification',
    desc: 'Choose the verification method used when users register.',
    reload: 'Reload',
    method: 'Verification Method',
    email: 'Email Registration',
    phone: 'Phone Registration',
    mixed: 'Email or Phone',
    emailVerification: 'Require email verification (invitation code required when off)',
    emailVerificationHint: 'Turning this off skips the email code only for registrations with a valid invitation code.',
    emailHint: 'Default mode. Registration continues to use email verification.',
    phoneHint: 'Phone registration uses Aliyun Dypnsapi SMS verification.',
    mixedHint: 'Users can register and sign in with either an email code or an SMS code.',
    smsSettingsTitle: 'SMS verification settings',
    smsSettingsDesc: 'Required for Phone Registration and Email or Phone. Email verification continues to use the mail settings below.',
    accessKeyID: 'Aliyun AccessKey ID',
    accessKeySecret: 'Aliyun AccessKey Secret',
    signName: 'Aliyun SMS SignName',
    ttlMinutes: 'Code TTL (minutes)',
    codeLength: 'Code Length',
    dailySMSLimit: 'Daily SMS limit per phone',
    buyPackage: 'Buy SMS Verification Package',
    save: 'Save Verification Settings',
    saving: 'Saving...',
    saved: 'Registration verification settings saved.',
    loadFailed: 'Load registration verification settings failed: {error}',
    saveFailed: 'Save registration verification settings failed: {error}',
    required: 'Aliyun AccessKey ID, AccessKey Secret, SignName, valid TTL, code length, and daily SMS limit are required whenever phone registration is enabled.'
  },
  zh: {
    title: '\u6ce8\u518c\u9a8c\u8bc1\u65b9\u5f0f',
    desc: '\u9009\u62e9\u7528\u6237\u6ce8\u518c\u65f6\u4f7f\u7528\u7684\u9a8c\u8bc1\u65b9\u5f0f\u3002',
    reload: '\u5237\u65b0',
    method: '\u9a8c\u8bc1\u65b9\u5f0f',
    email: '\u90ae\u7bb1\u6ce8\u518c',
    phone: '\u624b\u673a\u53f7\u6ce8\u518c',
    mixed: '\u90ae\u7bb1\u6216\u624b\u673a\u53f7',
    emailVerification: '\u8981\u6c42\u90ae\u7bb1\u9a8c\u8bc1\uff08\u5173\u95ed\u540e\u6ce8\u518c\u5fc5\u987b\u4f7f\u7528\u9080\u8bf7\u7801\uff09',
    emailVerificationHint: '\u5173\u95ed\u540e\uff0c\u4ec5\u6301\u6709\u6709\u6548\u9080\u8bf7\u7801\u7684\u7528\u6237\u53ef\u8df3\u8fc7\u90ae\u7bb1\u9a8c\u8bc1\u7801\u6ce8\u518c\u3002',
    emailHint: '\u9ed8\u8ba4\u6a21\u5f0f\uff0c\u6ce8\u518c\u7ee7\u7eed\u4f7f\u7528\u90ae\u7bb1\u9a8c\u8bc1\u3002',
    phoneHint: '\u624b\u673a\u53f7\u6ce8\u518c\u4f7f\u7528\u963f\u91cc\u4e91 Dypnsapi \u77ed\u4fe1\u9a8c\u8bc1\u3002',
    mixedHint: '\u7528\u6237\u53ef\u4f7f\u7528\u90ae\u7bb1\u9a8c\u8bc1\u7801\u6216\u77ed\u4fe1\u9a8c\u8bc1\u7801\u6ce8\u518c\u3001\u767b\u5f55\u3002',
    smsSettingsTitle: '\u77ed\u4fe1\u9a8c\u8bc1\u8bbe\u7f6e',
    smsSettingsDesc: '\u9009\u62e9\u300c\u624b\u673a\u53f7\u6ce8\u518c\u300d\u6216\u300c\u90ae\u7bb1\u6216\u624b\u673a\u53f7\u300d\u65f6\u5fc5\u987b\u914d\u7f6e\u3002\u90ae\u7bb1\u9a8c\u8bc1\u7ee7\u7eed\u4f7f\u7528\u4e0b\u65b9\u90ae\u4ef6\u8bbe\u7f6e\u3002',
    accessKeyID: '\u963f\u91cc\u4e91 AccessKey ID',
    accessKeySecret: '\u963f\u91cc\u4e91 AccessKey Secret',
    signName: '\u963f\u91cc\u4e91\u77ed\u4fe1\u7b7e\u540d',
    ttlMinutes: '\u9a8c\u8bc1\u7801\u6709\u6548\u671f\uff08\u5206\u949f\uff09',
    codeLength: '\u9a8c\u8bc1\u7801\u4f4d\u6570',
    dailySMSLimit: '\u540c\u4e00\u624b\u673a\u53f7\u6bcf\u65e5\u6700\u591a\u53d1\u9001\u6b21\u6570',
    buyPackage: '\u77ed\u4fe1\u8ba4\u8bc1\u5305\u8d2d\u4e70',
    save: '\u4fdd\u5b58\u9a8c\u8bc1\u8bbe\u7f6e',
    saving: '\u4fdd\u5b58\u4e2d...',
    saved: '\u6ce8\u518c\u9a8c\u8bc1\u8bbe\u7f6e\u5df2\u4fdd\u5b58\u3002',
    loadFailed: '\u52a0\u8f7d\u6ce8\u518c\u9a8c\u8bc1\u8bbe\u7f6e\u5931\u8d25: {error}',
    saveFailed: '\u4fdd\u5b58\u6ce8\u518c\u9a8c\u8bc1\u8bbe\u7f6e\u5931\u8d25: {error}',
    required: '\u53ea\u8981\u542f\u7528\u624b\u673a\u53f7\u6ce8\u518c\uff0c\u5c31\u9700\u8981\u586b\u5199\u963f\u91cc\u4e91 AccessKey ID\u3001AccessKey Secret\u3001\u77ed\u4fe1\u7b7e\u540d\u3001\u6709\u6548\u5206\u949f\u6570\u3001\u9a8c\u8bc1\u7801\u4f4d\u6570\u548c\u6bcf\u65e5\u53d1\u9001\u4e0a\u9650\u3002'
  }
};
const rax = (key, vars = {}) => ((REGISTRATION_AUTH_I18N[currentLang] || REGISTRATION_AUTH_I18N.en)[key] || REGISTRATION_AUTH_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const USER_REFERRAL_SYSTEM_I18N = {
  en: {
    title: 'User Invitations', desc: 'Control whether tenant users can share referral links and set OEM installer destinations.', reload: 'Reload',
    enabled: 'Enable user invitations', enabledOn: 'Enabled - eligible users can create and share their own invitation link.', enabledOff: 'Disabled - the invitation entry and all new referral registrations are unavailable.', toggleOn: 'Enabled', toggleOff: 'Disabled',
    downloadsTitle: 'Client download links', downloadsDesc: 'Provide HTTPS installer links for OEM distribution. Leave a value unchanged to keep its current destination.',
    windowsAMD64: 'Windows x64', windowsARM64: 'Windows ARM64', macosAMD64: 'macOS Intel', macosARM64: 'macOS Apple Silicon', linuxAMD64: 'Linux x64', linuxARM64: 'Linux ARM64',
    save: 'Save invitation settings', saving: 'Saving...', saved: 'Invitation system settings saved.', defaults: 'Restore official links', restored: 'Official MaClaw installer links restored. Save to apply them.', loadFailed: 'Load invitation system settings failed: {error}', saveFailed: 'Save invitation system settings failed: {error}', conflict: 'Invitation settings changed by another administrator. The latest values have been reloaded.'
  },
  zh: {
    title: '\u7528\u6237\u9080\u8bf7', desc: '\u63a7\u5236\u5f53\u524d\u79df\u6237\u662f\u5426\u53ef\u4ee5\u5206\u4eab\u9080\u8bf7\u94fe\u63a5\uff0c\u5e76\u914d\u7f6e OEM \u5ba2\u6237\u7aef\u4e0b\u8f7d\u5730\u5740\u3002', reload: '\u5237\u65b0',
    enabled: '\u5f00\u542f\u7528\u6237\u9080\u8bf7', enabledOn: '\u5df2\u5f00\u542f \u2014 \u7b26\u5408\u6761\u4ef6\u7684\u7528\u6237\u53ef\u521b\u5efa\u5e76\u5206\u4eab\u4e2a\u4eba\u9080\u8bf7\u94fe\u63a5\u3002', enabledOff: '\u5df2\u5173\u95ed \u2014 \u4e0d\u663e\u793a\u9080\u8bf7\u5165\u53e3\uff0c\u4e5f\u4e0d\u63a5\u53d7\u65b0\u7684\u9080\u8bf7\u6ce8\u518c\u3002', toggleOn: '\u5df2\u5f00\u542f', toggleOff: '\u5df2\u5173\u95ed',
    downloadsTitle: '\u5ba2\u6237\u7aef\u4e0b\u8f7d\u94fe\u63a5', downloadsDesc: '\u4e3a OEM \u5206\u53d1\u914d\u7f6e HTTPS \u5b89\u88c5\u5668\u5730\u5740\u3002\u672a\u4fee\u6539\u7684\u5730\u5740\u4f1a\u4fdd\u7559\u5f53\u524d\u503c\u3002',
    windowsAMD64: 'Windows x64', windowsARM64: 'Windows ARM64', macosAMD64: 'macOS Intel', macosARM64: 'macOS Apple Silicon', linuxAMD64: 'Linux x64', linuxARM64: 'Linux ARM64',
    save: '\u4fdd\u5b58\u9080\u8bf7\u7cfb\u7edf\u8bbe\u7f6e', saving: '\u4fdd\u5b58\u4e2d...', saved: '\u9080\u8bf7\u7cfb\u7edf\u8bbe\u7f6e\u5df2\u4fdd\u5b58\u3002', defaults: '\u6062\u590d\u5b98\u65b9\u94fe\u63a5', restored: '\u5df2\u6062\u590d\u5b98\u65b9 MaClaw \u5b89\u88c5\u5668\u94fe\u63a5\uff0c\u8bf7\u4fdd\u5b58\u4ee5\u751f\u6548\u3002', loadFailed: '\u52a0\u8f7d\u9080\u8bf7\u7cfb\u7edf\u8bbe\u7f6e\u5931\u8d25\uff1a{error}', saveFailed: '\u4fdd\u5b58\u9080\u8bf7\u7cfb\u7edf\u8bbe\u7f6e\u5931\u8d25\uff1a{error}', conflict: '\u5176\u4ed6\u7ba1\u7406\u5458\u5df2\u66f4\u65b0\u9080\u8bf7\u8bbe\u7f6e\uff0c\u5df2\u91cd\u65b0\u52a0\u8f7d\u6700\u65b0\u503c\u3002'
  }
};
const urx = (key, vars = {}) => ((USER_REFERRAL_SYSTEM_I18N[currentLang] || USER_REFERRAL_SYSTEM_I18N.en)[key] || USER_REFERRAL_SYSTEM_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const USER_REFERRAL_OFFICIAL_DOWNLOADS = {
  windows_amd64: 'https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-windows-amd64.exe', windows_arm64: 'https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-windows-arm64.exe',
  macos_amd64: 'https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-darwin-amd64', macos_arm64: 'https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-darwin-arm64',
  linux_amd64: 'https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-linux-amd64', linux_arm64: 'https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-linux-arm64'
};
let userReferralSystemConfig = null;
let userReferralSystemConfigRequest = 0;
let userReferralSystemSaveBusy = false;
function applyUserReferralSystemI18n() {
  _s('userReferralSystemTitle', 'textContent', urx('title')); _s('userReferralSystemDesc', 'textContent', urx('desc')); _s('userReferralSystemReloadBtn', 'textContent', urx('reload'));
  _s('userReferralSystemEnabledLabel', 'textContent', urx('enabled')); _s('userReferralDownloadsTitle', 'textContent', urx('downloadsTitle')); _s('userReferralDownloadsDesc', 'textContent', urx('downloadsDesc'));
  _s('userReferralDownloadWindowsAMD64Label', 'textContent', urx('windowsAMD64')); _s('userReferralDownloadWindowsARM64Label', 'textContent', urx('windowsARM64'));
  _s('userReferralDownloadMacOSAMD64Label', 'textContent', urx('macosAMD64')); _s('userReferralDownloadMacOSARM64Label', 'textContent', urx('macosARM64'));
  _s('userReferralDownloadLinuxAMD64Label', 'textContent', urx('linuxAMD64')); _s('userReferralDownloadLinuxARM64Label', 'textContent', urx('linuxARM64'));
  _s('userReferralSystemSaveBtn', 'textContent', urx('save')); _s('userReferralSystemDefaultsBtn', 'textContent', urx('defaults'));
  updateUserReferralSystemEnabledState();
}
function updateUserReferralSystemEnabledState() {
  const enabled = !!document.getElementById('userReferralSystemEnabled')?.checked;
  _s('userReferralSystemEnabledHint', 'textContent', urx(enabled ? 'enabledOn' : 'enabledOff'));
  _s('userReferralSystemToggleText', 'textContent', urx(enabled ? 'toggleOn' : 'toggleOff'));
}
function userReferralDownloadValue(key) { return (document.getElementById('userReferralDownload' + key) || {}).value || ''; }
function renderUserReferralSystemConfig(cfg = {}) {
  userReferralSystemConfig = cfg || {};
  const downloads = cfg.downloads || {};
  const mapping = { WindowsAMD64: 'windows_amd64', WindowsARM64: 'windows_arm64', MacOSAMD64: 'macos_amd64', MacOSARM64: 'macos_arm64', LinuxAMD64: 'linux_amd64', LinuxARM64: 'linux_arm64' };
  Object.entries(mapping).forEach(([id, key]) => _s('userReferralDownload' + id, 'value', downloads[key] || USER_REFERRAL_OFFICIAL_DOWNLOADS[key]));
  const enabled = document.getElementById('userReferralSystemEnabled'); if (enabled) enabled.checked = !!cfg.enabled;
  updateUserReferralSystemEnabledState();
}
async function loadUserReferralSystemConfig() {
  if (!canManageRegistrationAuth()) return null;
  const request = ++userReferralSystemConfigRequest;
  applyUserReferralSystemI18n();
  try { const data = await api('/api/admin/user-referrals/config'); if (request !== userReferralSystemConfigRequest || userReferralSystemSaveBusy) return null; renderUserReferralSystemConfig(data || {}); return data || {}; }
  catch (err) { if (request !== userReferralSystemConfigRequest) return null; const msg = urx('loadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); return null; }
}
function toggleUserReferralSystemEnabled() { updateUserReferralSystemEnabledState(); }
function resetUserReferralDownloadDefaults() {
  const mapping = { WindowsAMD64: 'windows_amd64', WindowsARM64: 'windows_arm64', MacOSAMD64: 'macos_amd64', MacOSARM64: 'macos_arm64', LinuxAMD64: 'linux_amd64', LinuxARM64: 'linux_arm64' };
  Object.entries(mapping).forEach(([id, key]) => _s('userReferralDownload' + id, 'value', USER_REFERRAL_OFFICIAL_DOWNLOADS[key]));
  showToast(urx('restored'), 'info');
}
async function saveUserReferralSystemConfig() {
  if (!canManageRegistrationAuth()) return null;
  if (userReferralSystemSaveBusy) return null;
  const btn = document.getElementById('userReferralSystemSaveBtn'); const previousLabel = btn ? btn.textContent : '';
  const mapping = { WindowsAMD64: 'windows_amd64', WindowsARM64: 'windows_arm64', MacOSAMD64: 'macos_amd64', MacOSARM64: 'macos_arm64', LinuxAMD64: 'linux_amd64', LinuxARM64: 'linux_arm64' };
  const downloads = {}; Object.entries(mapping).forEach(([id, key]) => { downloads[key] = userReferralDownloadValue(id).trim(); });
  const payload = Object.assign({}, userReferralSystemConfig || {}, { enabled: !!document.getElementById('userReferralSystemEnabled')?.checked, downloads }); const headers = {}; if (userReferralSystemConfig && userReferralSystemConfig.version) headers['If-Match'] = userReferralSystemConfig.version;
  userReferralSystemSaveBusy = true; ++userReferralSystemConfigRequest;
  if (btn) { btn.disabled = true; btn.setAttribute('aria-busy', 'true'); btn.textContent = urx('saving'); }
  let reloadLatest = false;
  try { const data = await api('/api/admin/user-referrals/config', { method: 'PUT', headers, body: JSON.stringify(payload) }); renderUserReferralSystemConfig(data || payload); const msg = urx('saved'); setOutput(msg); showToast(msg, 'success'); return data || payload; }
  catch (err) { if (/CONFIG_CONFLICT|changed by another administrator/i.test(String(err && err.message || err || ''))) { const msg = urx('conflict'); setOutput(msg); showToast(msg, 'error'); reloadLatest = true; } else { const msg = urx('saveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } return null; }
  finally { userReferralSystemSaveBusy = false; if (btn) { btn.disabled = false; btn.removeAttribute('aria-busy'); btn.textContent = previousLabel || urx('save'); } if (reloadLatest) void loadUserReferralSystemConfig(); }
}
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
const TENANT_DIGITAL_ASSETS_SETTINGS_I18N = {
  en: {
    title: 'Digital Assets',
    desc: 'Enable enterprise knowledge libraries for this tenant. Default is off; turn on to use the Digital Assets admin tab and client one-way sync.',
    reload: 'Reload',
    enabledLabel: 'Enable Digital Assets',
    enabledHintOn: 'Enabled - manage libraries under Digital Assets.',
    enabledHintOff: 'Disabled - library APIs return feature disabled.',
    syncLabel: 'Client sync',
    syncHintOn: 'Clients may pull libraries they are allowed to access.',
    syncHintOff: 'Sync paused - local caches stay readable; no new pulls.',
    loadFailed: 'Load digital assets settings failed: {error}',
    saveFailed: 'Save digital assets settings failed: {error}',
    enabledSaved: 'Digital Assets enabled.',
    disabledSaved: 'Digital Assets disabled.',
    syncOnSaved: 'Client sync enabled.',
    syncOffSaved: 'Client sync disabled.'
  },
  zh: {
    title: '\u6570\u5b57\u8d44\u4ea7',
    desc: '\u4e3a\u5f53\u524d\u79df\u6237\u5f00\u542f\u4f01\u4e1a\u77e5\u8bc6\u5e93\u3002\u9ed8\u8ba4\u5173\u95ed\uff1b\u5f00\u542f\u540e\u53ef\u5728\u300c\u6570\u5b57\u8d44\u4ea7\u300d\u7ba1\u7406\u5e93\u5e76\u5411\u5ba2\u6237\u7aef\u5355\u5411\u540c\u6b65\u3002',
    reload: '\u5237\u65b0',
    enabledLabel: '\u542f\u7528\u6570\u5b57\u8d44\u4ea7',
    enabledHintOn: '\u5df2\u542f\u7528 \u2014 \u53ef\u5728\u300c\u6570\u5b57\u8d44\u4ea7\u300d\u7ba1\u7406\u5e93\u3002',
    enabledHintOff: '\u5df2\u5173\u95ed \u2014 \u5e93\u76f8\u5173 API \u4f1a\u8fd4\u56de\u529f\u80fd\u672a\u5f00\u542f\u3002',
    syncLabel: '\u5ba2\u6237\u7aef\u540c\u6b65',
    syncHintOn: '\u5ba2\u6237\u7aef\u53ef\u62c9\u53d6\u5176\u6709\u6743\u8bbf\u95ee\u7684\u5e93\u3002',
    syncHintOff: '\u540c\u6b65\u5df2\u6682\u505c \u2014 \u672c\u5730\u7f13\u5b58\u4ecd\u53ef\u53ea\u8bfb\u6d4f\u89c8\uff0c\u4e0d\u518d\u62c9\u53d6\u66f4\u65b0\u3002',
    loadFailed: '\u52a0\u8f7d\u6570\u5b57\u8d44\u4ea7\u8bbe\u7f6e\u5931\u8d25: {error}',
    saveFailed: '\u4fdd\u5b58\u6570\u5b57\u8d44\u4ea7\u8bbe\u7f6e\u5931\u8d25: {error}',
    enabledSaved: '\u5df2\u542f\u7528\u6570\u5b57\u8d44\u4ea7\u3002',
    disabledSaved: '\u5df2\u5173\u95ed\u6570\u5b57\u8d44\u4ea7\u3002',
    syncOnSaved: '\u5df2\u542f\u7528\u5ba2\u6237\u7aef\u540c\u6b65\u3002',
    syncOffSaved: '\u5df2\u5173\u95ed\u5ba2\u6237\u7aef\u540c\u6b65\u3002'
  }
};
const tdax = (key, vars = {}) => ((TENANT_DIGITAL_ASSETS_SETTINGS_I18N[currentLang] || TENANT_DIGITAL_ASSETS_SETTINGS_I18N.en)[key] || TENANT_DIGITAL_ASSETS_SETTINGS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const TENANT_SYSTEM_LLM_DEFAULTS_I18N = {
  en: {
    title: 'System Free LLM (system-free)',
    desc: 'Reserved free service group for all server-side Hub/MaClawSrv agents. Cannot be deleted; no recharge required. Change providers in Model Services.',
    reload: 'Reload',
    noUsableGroups: 'system-free exists but has no usable provider route. Open Model Services to attach MaClaw Official or a local provider.',
    hint: 'Pinned as the system default. Used by workflow draft, IM system LLM, config agents, and MaClawSrv agents.',
    notReadyDetail: 'system-free is not ready: {id}',
    config: 'Configure system-free',
    configFailed: 'Open system-free config failed: {error}',
    loadFailed: 'Load system-free status failed: {error}',
    test: 'Test system-free',
    testing: 'Testing...',
    testOk: 'system-free is available ({ms} ms, provider={provider})',
    testFail: 'system-free test failed: {error}',
    ready: 'Ready',
    notReady: 'Not ready',
    providers: 'Providers: {ids}'
  },
  zh: {
    title: '\u7cfb\u7edf\u514d\u8d39 LLM\uff08system-free\uff09',
    desc: '\u4f9b Hub / MaClawSrv \u6240\u6709\u670d\u52a1\u7aef Agent \u4f7f\u7528\u7684\u4fdd\u7559\u514d\u8d39\u670d\u52a1\u7ec4\u3002\u4e0d\u53ef\u5220\u9664\u3001\u4e0d\u9700\u5145\u503c\uff1b\u53ef\u5728\u300c\u6a21\u578b\u670d\u52a1\u300d\u4e2d\u4fee\u6539\u670d\u52a1\u5546\u3002',
    reload: '\u5237\u65b0',
    noUsableGroups: 'system-free \u5df2\u5b58\u5728\u4f46\u6ca1\u6709\u53ef\u7528\u670d\u52a1\u5546\u8def\u7531\u3002\u8bf7\u5728\u6a21\u578b\u670d\u52a1\u4e2d\u7ed1\u5b9a MaClaw \u5b98\u65b9\u6216\u672c\u5730\u670d\u52a1\u5546\u3002',
    hint: '\u56fa\u5b9a\u4e3a\u7cfb\u7edf\u9ed8\u8ba4\u3002\u7528\u4e8e\u5de5\u4f5c\u6d41\u8349\u7a3f\u3001IM \u7cfb\u7edf LLM\u3001\u914d\u7f6e\u52a9\u624b\u4e0e MaClawSrv Agent\u3002',
    notReadyDetail: 'system-free \u672a\u5c31\u7eea\uff1a{id}',
    config: '\u914d\u7f6e system-free',
    configFailed: '\u6253\u5f00 system-free \u914d\u7f6e\u5931\u8d25: {error}',
    loadFailed: '\u52a0\u8f7d system-free \u72b6\u6001\u5931\u8d25: {error}',
    test: '\u6d4b\u8bd5 system-free',
    testing: '\u6d4b\u8bd5\u4e2d...',
    testOk: 'system-free \u53ef\u7528\uff08{ms} ms\uff0c\u670d\u52a1\u5546={provider}\uff09',
    testFail: 'system-free \u6d4b\u8bd5\u5931\u8d25: {error}',
    ready: '\u5df2\u5c31\u7eea',
    notReady: '\u672a\u5c31\u7eea',
    providers: '\u670d\u52a1\u5546: {ids}'
  }
};
const tslx = (key, vars = {}) => ((TENANT_SYSTEM_LLM_DEFAULTS_I18N[currentLang] || TENANT_SYSTEM_LLM_DEFAULTS_I18N.en)[key] || TENANT_SYSTEM_LLM_DEFAULTS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
const TENANT_MIGRATION_MIN_MB = 100;
const TENANT_MIGRATION_MAX_MB = 1024;
// Declared early: applyTenantSystemLLMDefaultsI18n() runs at script load and renders status from this cache.
let tenantSystemFreeStatusCache = null;
let tenantSystemFreeStatusInflight = null;
let tenantSystemFreeTestInflight = false;
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
function applyTenantDigitalAssetsSettingsI18n() {
  _s('tenantDigitalAssetsSettingsTitle', 'textContent', tdax('title'));
  _s('tenantDigitalAssetsSettingsDesc', 'textContent', tdax('desc'));
  _s('tenantDigitalAssetsSettingsReloadBtn', 'textContent', tdax('reload'));
  _s('tenantDigitalAssetsEnabledLabel', 'textContent', tdax('enabledLabel'));
  _s('tenantDigitalAssetsSyncLabel', 'textContent', tdax('syncLabel'));
  const enabledToggle = document.getElementById('tenantDigitalAssetsEnabledToggle');
  const syncToggle = document.getElementById('tenantDigitalAssetsSyncToggle');
  _s('tenantDigitalAssetsEnabledHint', 'textContent', enabledToggle && enabledToggle.checked ? tdax('enabledHintOn') : tdax('enabledHintOff'));
  _s('tenantDigitalAssetsSyncHint', 'textContent', syncToggle && syncToggle.checked ? tdax('syncHintOn') : tdax('syncHintOff'));
}
function applyTenantSystemLLMDefaultsI18n() {
  _s('tenantSystemLLMDefaultsTitle', 'textContent', tslx('title'));
  _s('tenantSystemLLMDefaultsDesc', 'textContent', tslx('desc'));
  _s('tenantSystemLLMDefaultsReloadBtn', 'textContent', tslx('reload'));
  _s('tenantSystemLLMDefaultsSaveBtn', 'textContent', tslx('config'));
  _s('tenantSystemFreeTestBtn', 'textContent', tslx('test'));
  renderTenantSystemFreeStatus();
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
function applyRegistrationAuthI18n() {
  _s('registrationAuthTitle', 'textContent', rax('title'));
  _s('registrationAuthDesc', 'textContent', rax('desc'));
  _s('registrationAuthReloadBtn', 'textContent', rax('reload'));
  _s('registrationAuthMethodLabel', 'textContent', rax('method'));
  _s('registrationAuthMethodEmail', 'textContent', rax('email'));
  _s('registrationAuthMethodPhone', 'textContent', rax('phone'));
  _s('registrationAuthMethodMixed', 'textContent', rax('mixed'));
  _s('registrationAuthEmailVerificationLabel', 'textContent', rax('emailVerification'));
  _s('registrationAuthEmailVerificationHint', 'textContent', rax('emailVerificationHint'));
  _s('registrationAuthSMSSettingsTitle', 'textContent', rax('smsSettingsTitle'));
  _s('registrationAuthSMSSettingsDesc', 'textContent', rax('smsSettingsDesc'));
  _s('registrationAuthAliyunAccessKeyIDLabel', 'textContent', rax('accessKeyID'));
  _s('registrationAuthAliyunAccessKeySecretLabel', 'textContent', rax('accessKeySecret'));
  _s('registrationAuthAliyunSignNameLabel', 'textContent', rax('signName'));
  _s('registrationAuthCodeTTLMinutesLabel', 'textContent', rax('ttlMinutes'));
  _s('registrationAuthCodeLengthLabel', 'textContent', rax('codeLength'));
  _s('registrationAuthDailySMSLimitLabel', 'textContent', rax('dailySMSLimit'));
  _s('registrationAuthBuyLink', 'textContent', rax('buyPackage'));
  _s('registrationAuthSaveBtn', 'textContent', rax('save'));
  updateRegistrationAuthModeState();
}
function registrationAuthUsesPhone() {
  const el = document.getElementById('registrationAuthMethod');
  return el && (el.value === 'phone' || el.value === 'mixed');
}
function canManageRegistrationAuth() {
  const profile = typeof window !== 'undefined' && typeof window.adminProfile === 'function' ? window.adminProfile() : null;
  return !!profile && String(profile.scope || '').toLowerCase() === 'tenant';
}
function clearTenantSystemFreeState() {
  tenantSystemFreeStatusCache = null;
  if (typeof window !== 'undefined') delete window.tenantSystemFreeStatusCache;
}
function canManageTenantDigitalAssets() {
  const profile = typeof window !== 'undefined' && typeof window.adminProfile === 'function' ? window.adminProfile() : null;
  return !!profile && String(profile.scope || '').toLowerCase() === 'tenant';
}
function updateRegistrationAuthModeState() {
  const phoneEnabled = registrationAuthUsesPhone();
  const method = document.getElementById('registrationAuthMethod')?.value;
  const section = document.getElementById('registrationAuthAliyunSection');
  const buy = document.getElementById('registrationAuthBuyWrap');
  const emailVerification = document.getElementById('registrationAuthEmailVerificationWrap');
  if (section) section.classList.toggle('hidden', !phoneEnabled);
  if (buy) buy.classList.toggle('hidden', !phoneEnabled);
  if (emailVerification) emailVerification.classList.toggle('hidden', method === 'phone');
  _s('registrationAuthHint', 'textContent', rax(method === 'mixed' ? 'mixedHint' : phoneEnabled ? 'phoneHint' : 'emailHint'));
}
function renderRegistrationAuthConfig(cfg = {}) {
  const savedMethod = String(cfg.method || 'email').toLowerCase();
  const method = savedMethod === 'phone' || savedMethod === 'mixed' ? savedMethod : 'email';
  const methodEl = document.getElementById('registrationAuthMethod');
  if (methodEl) methodEl.value = method;
  _s('registrationAuthEmailVerificationDisabled', 'checked', !cfg.email_verification_disabled);
  _s('registrationAuthAliyunAccessKeyID', 'value', cfg.aliyun_access_key_id || '');
  _s('registrationAuthAliyunAccessKeySecret', 'value', cfg.aliyun_access_key_secret || '');
  _s('registrationAuthAliyunSignName', 'value', cfg.aliyun_sign_name || '\u901f\u901a\u4e92\u8054\u9a8c\u8bc1\u5e73\u53f0');
  _s('registrationAuthCodeTTLMinutes', 'value', String(cfg.code_ttl_minutes || 5));
  _s('registrationAuthCodeLength', 'value', String(cfg.code_length || 6));
  _s('registrationAuthDailySMSLimit', 'value', String(cfg.daily_sms_limit || 3));
  const link = document.getElementById('registrationAuthBuyLink');
  if (link) link.href = cfg.aliyun_sms_buy_url || 'https://common-buy.aliyun.com/?commodityCode=dypns_smsverify_public_cn#buy';
  updateRegistrationAuthModeState();
}
async function loadRegistrationAuthConfig() {
  // The card is tenant-only, but keep this guard for inline handlers and
  // console calls made after a scope switch.
  if (!canManageRegistrationAuth()) return null;
  applyRegistrationAuthI18n();
  try {
    const data = await api('/api/admin/settings/registration-auth');
    renderRegistrationAuthConfig(data || {});
    return data || {};
  } catch (err) {
    const msg = rax('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function saveRegistrationAuthConfig() {
  // The server enforces this too; avoid an unnecessary forbidden request from
  // stale UI or a direct console invocation.
  if (!canManageRegistrationAuth()) return null;
  const method = String(document.getElementById('registrationAuthMethod')?.value || 'email').toLowerCase() === 'mixed' ? 'mixed' : registrationAuthUsesPhone() ? 'phone' : 'email';
  const payload = {
    method,
    email_verification_disabled: !document.getElementById('registrationAuthEmailVerificationDisabled')?.checked,
    aliyun_access_key_id: (document.getElementById('registrationAuthAliyunAccessKeyID') && document.getElementById('registrationAuthAliyunAccessKeyID').value || '').trim(),
    aliyun_access_key_secret: (document.getElementById('registrationAuthAliyunAccessKeySecret') && document.getElementById('registrationAuthAliyunAccessKeySecret').value || '').trim(),
    aliyun_sign_name: (document.getElementById('registrationAuthAliyunSignName') && document.getElementById('registrationAuthAliyunSignName').value || '').trim(),
    aliyun_template_code: '100001',
    code_ttl_minutes: Number((document.getElementById('registrationAuthCodeTTLMinutes') && document.getElementById('registrationAuthCodeTTLMinutes').value || '5').trim()),
    code_length: Number((document.getElementById('registrationAuthCodeLength') && document.getElementById('registrationAuthCodeLength').value || '6').trim()),
    daily_sms_limit: Number((document.getElementById('registrationAuthDailySMSLimit') && document.getElementById('registrationAuthDailySMSLimit').value || '3').trim())
  };
  if ((method === 'phone' || method === 'mixed') && (!payload.aliyun_access_key_id || !payload.aliyun_access_key_secret || !payload.aliyun_sign_name || !Number.isFinite(payload.code_ttl_minutes) || payload.code_ttl_minutes < 1 || payload.code_ttl_minutes > 30 || !Number.isFinite(payload.code_length) || payload.code_length < 4 || payload.code_length > 8 || !Number.isFinite(payload.daily_sms_limit) || payload.daily_sms_limit < 1 || payload.daily_sms_limit > 50)) {
    const msg = rax('required');
    setOutput(msg);
    showToast(msg, 'error');
    return;
  }
  const btn = document.getElementById('registrationAuthSaveBtn');
  const previousLabel = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = rax('saving'); }
  try {
    const data = await api('/api/admin/settings/registration-auth', { method: 'PUT', body: JSON.stringify(payload) });
    renderRegistrationAuthConfig(data || payload);
    const msg = rax('saved');
    setOutput(msg);
    showToast(msg, 'success');
    return data || payload;
  } catch (err) {
    const msg = rax('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    throw err;
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = previousLabel || rax('save'); }
  }
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
    applyRegistrationAuthI18n();
    applyUserReferralSystemI18n();
    applyTenantMailSenderI18n();
    applyTenantMigrationSettingsI18n();
    applyTenantDigitalAssetsSettingsI18n();
    applyTenantSystemLLMDefaultsI18n();
  });
}
applyTLSI18n();
applySystemRoutingI18n();
applyRegistrationAuthI18n();
applyUserReferralSystemI18n();
applyTenantMailSenderI18n();
applyTenantMigrationSettingsI18n();
applyTenantDigitalAssetsSettingsI18n();
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
async function loadTenantDigitalAssetsSettings() {
  if (!canManageTenantDigitalAssets()) return null;
  applyTenantDigitalAssetsSettingsI18n();
  try {
    const data = await api('/api/admin/digital-assets/settings');
    const enabledToggle = document.getElementById('tenantDigitalAssetsEnabledToggle');
    const syncToggle = document.getElementById('tenantDigitalAssetsSyncToggle');
    if (enabledToggle) enabledToggle.checked = !!(data && data.enabled);
    if (syncToggle) {
      syncToggle.checked = data && data.sync_enabled !== false;
      syncToggle.disabled = !(data && data.enabled);
    }
    applyTenantDigitalAssetsSettingsI18n();
    return data || {};
  } catch (err) {
    const msg = tdax('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function toggleTenantDigitalAssetsEnabled(enabled) {
  if (!canManageTenantDigitalAssets()) return null;
  const enabledToggle = document.getElementById('tenantDigitalAssetsEnabledToggle');
  const syncToggle = document.getElementById('tenantDigitalAssetsSyncToggle');
  try {
    const data = await api('/api/admin/digital-assets/settings', {
      method: 'PUT',
      body: JSON.stringify({ enabled: !!enabled })
    });
    if (enabledToggle) enabledToggle.checked = !!(data && data.enabled);
    if (syncToggle) {
      syncToggle.checked = data && data.sync_enabled !== false;
      syncToggle.disabled = !(data && data.enabled);
    }
    applyTenantDigitalAssetsSettingsI18n();
    const msg = data && data.enabled ? tdax('enabledSaved') : tdax('disabledSaved');
    setOutput(msg);
    showToast(msg, 'success');
    return data || {};
  } catch (err) {
    if (enabledToggle) enabledToggle.checked = !enabled;
    const msg = tdax('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function toggleTenantDigitalAssetsSync(syncEnabled) {
  if (!canManageTenantDigitalAssets()) return null;
  const syncToggle = document.getElementById('tenantDigitalAssetsSyncToggle');
  try {
    const data = await api('/api/admin/digital-assets/settings', {
      method: 'PUT',
      body: JSON.stringify({ sync_enabled: !!syncEnabled })
    });
    if (syncToggle) syncToggle.checked = data && data.sync_enabled !== false;
    applyTenantDigitalAssetsSettingsI18n();
    const msg = data && data.sync_enabled !== false ? tdax('syncOnSaved') : tdax('syncOffSaved');
    setOutput(msg);
    showToast(msg, 'success');
    return data || {};
  } catch (err) {
    if (syncToggle) syncToggle.checked = !syncEnabled;
    const msg = tdax('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function saveTenantMigrationSettings() { const input = document.getElementById('tenantMigrationMaxMB'); const valueMB = normalizeTenantMigrationMB(input ? input.value : 0); if (valueMB < TENANT_MIGRATION_MIN_MB || valueMB > TENANT_MIGRATION_MAX_MB) { const msg = tmgx('invalid'); setOutput(msg); showToast(msg, 'error'); return; } const btn = document.getElementById('tenantMigrationSettingsSaveBtn'); const previousLabel = btn ? btn.textContent : ''; if (btn) { btn.disabled = true; btn.textContent = tmgx('saving'); } try { const data = await api('/api/admin/migration/settings', { method: 'PUT', body: JSON.stringify({ max_compressed_bytes: valueMB * 1024 * 1024 }) }); if (input) input.value = String(tenantMigrationBytesToMB(data && data.max_compressed_bytes)); const msg = tmgx('saved'); setOutput(msg); showToast(msg, 'success'); return data || {}; } catch (err) { const msg = tmgx('saveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); throw err; } finally { if (btn) { btn.disabled = false; btn.textContent = previousLabel || tmgx('save'); } } }
function getTenantSystemFreeCache() {
  if (!canManageRegistrationAuth()) {
    clearTenantSystemFreeState();
    return null;
  }
  if (tenantSystemFreeStatusCache) return tenantSystemFreeStatusCache;
  if (typeof window !== 'undefined' && window.tenantSystemFreeStatusCache) {
    tenantSystemFreeStatusCache = window.tenantSystemFreeStatusCache;
    return tenantSystemFreeStatusCache;
  }
  return null;
}
function setTenantSystemFreeCache(st) {
  if (!canManageRegistrationAuth()) {
    clearTenantSystemFreeState();
    return null;
  }
  tenantSystemFreeStatusCache = st || {};
  if (typeof window !== 'undefined') {
    window.tenantSystemFreeStatusCache = tenantSystemFreeStatusCache;
  }
  return tenantSystemFreeStatusCache;
}
// Dedupe parallel GETs (bootstrap runs overview + system tab refresh together).
async function fetchTenantSystemFreeStatus() {
  if (!canManageRegistrationAuth()) return null;
  if (tenantSystemFreeStatusInflight) return tenantSystemFreeStatusInflight;
  tenantSystemFreeStatusInflight = Promise.resolve()
    .then(function() { return api('/api/admin/llm/system-free'); })
    .then(function(st) { return canManageRegistrationAuth() ? setTenantSystemFreeCache(st) : null; })
    .finally(function() { tenantSystemFreeStatusInflight = null; });
  return tenantSystemFreeStatusInflight;
}
function formatTenantSystemFreeDetail(st) {
  const status = st || {};
  const providers = (status.provider_ids || []).join(', ') || '-';
  const reasons = (status.reasons || []).join(', ');
  const parts = [tslx('hint'), tslx('providers', { ids: providers })];
  if (!status.ready) {
    parts.push(reasons ? tslx('notReadyDetail', { id: reasons }) : tslx('noUsableGroups'));
  }
  return parts.join(' | ');
}
function renderTenantSystemFreeStatus() {
  const card = document.getElementById('tenantSystemLLMDefaultsCard');
  const statusEl = document.getElementById('tenantSystemFreeStatusBadge');
  const st = getTenantSystemFreeCache();
  if (!st) {
    if (card) {
      card.style.borderColor = '';
      card.style.background = '';
    }
    _s('tenantSystemLLMDefaultsHint', 'textContent', tslx('hint'));
    if (statusEl) {
      statusEl.textContent = '';
      statusEl.style.color = '';
    }
    return;
  }
  const ready = !!st.ready;
  if (card) {
    card.style.borderColor = ready ? '' : 'rgba(180,35,24,.35)';
    card.style.background = ready ? '' : 'rgba(180,35,24,.04)';
  }
  _s('tenantSystemLLMDefaultsHint', 'textContent', formatTenantSystemFreeDetail(st));
  if (statusEl) {
    statusEl.textContent = ready ? tslx('ready') : tslx('notReady');
    statusEl.style.color = ready ? '#1f7a3f' : '#b42318';
  }
}
function tenantSystemFreeTestButtons() {
  if (!canManageRegistrationAuth()) return [];
  return [
    document.getElementById('tenantSystemFreeTestBtn'),
    document.getElementById('overviewSystemFreeTestBtn'),
    document.getElementById('systemFreeGateTestBtn')
  ].filter(Boolean);
}
function applyTenantSystemFreeStatusUI(st) {
  if (!canManageRegistrationAuth()) return;
  if (st) setTenantSystemFreeCache(st);
  renderTenantSystemFreeStatus();
  // Paint overview from cache; skipPeer avoids re-entering this path.
  if (typeof window !== 'undefined' && typeof window.applyOverviewSystemFreeStatus === 'function') {
    try { window.applyOverviewSystemFreeStatus(getTenantSystemFreeCache(), { skipPeer: true }); } catch (_) { /* non-fatal */ }
  }
}
async function loadTenantSystemLLMDefaults() {
  if (!canManageRegistrationAuth()) return null;
  applyTenantSystemLLMDefaultsI18n();
  try {
    const st = await fetchTenantSystemFreeStatus();
    if (!canManageRegistrationAuth()) return null;
    applyTenantSystemFreeStatusUI(st);
    return getTenantSystemFreeCache() || {};
  } catch (err) {
    if (!canManageRegistrationAuth()) return null;
    const msg = tslx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    throw err;
  }
}
async function saveTenantSystemLLMDefaults() {
  if (!canManageRegistrationAuth()) return null;
  // system-free is fixed; open Model Services focused on that group.
  try {
    if (typeof window !== 'undefined' && typeof window.openSystemFreeServiceGroup === 'function') {
      await window.openSystemFreeServiceGroup();
      return;
    }
    if (typeof openTab === 'function') openTab('modelservices');
    else if (typeof window !== 'undefined' && typeof window.openTab === 'function') window.openTab('modelservices');
    else throw new Error('openTab unavailable');
  } catch (err) {
    // openSystemFreeServiceGroup already toasts; only toast for bare openTab fallback.
    if (err && err.systemFreeConfigToasted) return;
    const msg = tslx('configFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function testTenantSystemFreeLLM() {
  if (!canManageRegistrationAuth()) return null;
  if (tenantSystemFreeTestInflight) return null;
  const buttons = tenantSystemFreeTestButtons();
  if (!buttons.length) return null;
  tenantSystemFreeTestInflight = true;
  const previousLabels = buttons.map(function(btn) { return btn.textContent; });
  buttons.forEach(function(btn) {
    btn.disabled = true;
    btn.textContent = tslx('testing');
  });
  try {
    const data = await api('/api/admin/llm/system-free/test', { method: 'POST', body: '{}' });
    if (!canManageRegistrationAuth()) return null;
    if (data && data.status) {
      applyTenantSystemFreeStatusUI(data.status);
    } else if (data && (data.ok || data.success)) {
      // Quiet refresh: avoid loadTenantSystemLLMDefaults toasts stacking on test result.
      try {
        const st = await fetchTenantSystemFreeStatus();
        applyTenantSystemFreeStatusUI(st);
      } catch (_) { /* keep prior status */ }
    }
    if (data && (data.ok || data.success)) {
      const msg = tslx('testOk', {
        ms: String(data.latency_ms || 0),
        provider: String(data.provider_id || '-')
      });
      setOutput(msg);
      showToast(msg, 'success');
      return data;
    }
    const errMsg = tslx('testFail', { error: (data && data.error) || 'unknown' });
    setOutput(errMsg);
    showToast(errMsg, 'error');
    return data;
  } catch (err) {
    if (!canManageRegistrationAuth()) return null;
    const msg = tslx('testFail', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
    throw err;
  } finally {
    tenantSystemFreeTestInflight = false;
    if (!canManageRegistrationAuth()) return;
    buttons.forEach(function(btn, i) {
      btn.disabled = false;
      btn.textContent = previousLabels[i] || tslx('test');
    });
  }
}
if (typeof window !== 'undefined') {
  window.getTenantSystemFreeCache = getTenantSystemFreeCache;
  window.clearTenantSystemFreeState = clearTenantSystemFreeState;
  window.setTenantSystemFreeCache = setTenantSystemFreeCache;
  window.fetchTenantSystemFreeStatus = fetchTenantSystemFreeStatus;
  window.testTenantSystemFreeLLM = testTenantSystemFreeLLM;
  window.loadTenantSystemLLMDefaults = loadTenantSystemLLMDefaults;
  window.saveTenantSystemLLMDefaults = saveTenantSystemLLMDefaults;
  window.renderTenantSystemFreeStatus = renderTenantSystemFreeStatus;
  window.applyTenantSystemFreeStatusUI = applyTenantSystemFreeStatusUI;
  window.loadTenantDigitalAssetsSettings = loadTenantDigitalAssetsSettings;
  window.toggleTenantDigitalAssetsEnabled = toggleTenantDigitalAssetsEnabled;
  window.toggleTenantDigitalAssetsSync = toggleTenantDigitalAssetsSync;
}
// Machines runtime moved to machines-tab.js
async function sendTestMail() { try { const email = document.getElementById('testMailEmail').value.trim(); if (!email) { const msg = tr('testRecipientRequired'); setOutput(msg); showToast(msg, 'error'); return; } await saveMailConfig(); const data = await api('/api/admin/mail/test', { method: 'POST', body: JSON.stringify({ email }) }); const msg = data.message || tr('mailSent'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = tr('mailFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function changeAdminPassword() { const currentPassword = document.getElementById('currentPasswordInput').value; const newPassword = document.getElementById('newPasswordInput').value; const confirmPassword = document.getElementById('confirmPasswordInput').value; if (!currentPassword || !newPassword) { const msg = tr('requestFailed'); setOutput(msg); showToast(msg, 'error'); return; } if (newPassword !== confirmPassword) { const msg = ptr('mismatch'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/password', { method: 'POST', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) }); if (data.access_token) localStorage.setItem(adminTokenKey, data.access_token); if (data.admin) setAdminProfile(data.admin); document.getElementById('currentPasswordInput').value = ''; document.getElementById('newPasswordInput').value = ''; document.getElementById('confirmPasswordInput').value = ''; refreshAdminHeader(); const msg = ptr('changed'); setOutput(msg); showToast(msg, 'success'); } catch (err) { setOutput(err.message); showToast(err.message, 'error'); } }
async function updateAdminProfile() { const email = document.getElementById('adminEmailInput').value.trim(); if (!email) { const msg = prf('required'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/profile', { method: 'POST', body: JSON.stringify({ email }) }); if (data.access_token) localStorage.setItem(adminTokenKey, data.access_token); if (data.admin) setAdminProfile(data.admin); refreshAdminHeader(); const msg = prf('saved'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = prf('failed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } }
