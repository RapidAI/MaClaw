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
    sans: 'SANs: {value}'
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
    sans: 'SANs: {value}'
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
      badge.textContent = '[OK] ' + tlsx('statusValidUntil', { date: expiry });
      badge.className = 'badge ok';
      info.innerHTML = '<div class="item-meta">' + escapeHtml(tlsx('sans', { value: data.cert_sans || '-' })) + '</div>';
    } else if (data.cert_expiry) {
      badge.textContent = '[EXPIRED] ' + tlsx('statusExpired');
      badge.className = 'badge danger';
      info.innerHTML = '<div class="item-meta" style="color:var(--danger)">' + escapeHtml(tlsx('certExpiredHint')) + '</div>';
    } else {
      badge.textContent = '[PENDING] ' + tlsx('statusNotGenerated');
      badge.className = 'badge info';
      info.innerHTML = '<div class="item-meta">' + escapeHtml(tlsx('certGenerateHint')) + '</div>';
    }
  } catch (err) {
    const msg = tlsx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
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
  });
}
applyTLSI18n();
function findMailPreset(provider) { return MAIL_PRESETS[provider] || MAIL_PRESETS.custom; }
function detectMailProvider(cfg) { const host = String(cfg?.smtp_host || '').trim().toLowerCase(); const port = Number(cfg?.smtp_port || 0); const encryption = String(cfg?.smtp_encryption || '').trim().toLowerCase(); for (const [provider, preset] of Object.entries(MAIL_PRESETS)) { if (provider === 'custom') continue; if (host === preset.smtp_host && (!port || port === preset.smtp_port) && (!encryption || encryption === preset.smtp_encryption)) return provider; } return String(cfg?.provider || '').trim() || 'custom'; }
function renderMailConfig(cfg = {}) { const provider = detectMailProvider(cfg); document.getElementById('mailProvider').value = MAIL_PRESETS[provider] ? provider : 'custom'; document.getElementById('mailHost').value = cfg.smtp_host || ''; document.getElementById('mailPort').value = cfg.smtp_port ? String(cfg.smtp_port) : ''; document.getElementById('mailEncryption').value = cfg.smtp_encryption || 'auto'; document.getElementById('mailUsername').value = cfg.smtp_username || ''; document.getElementById('mailPassword').value = cfg.smtp_password || ''; document.getElementById('mailFromName').value = cfg.from_name || 'MaClaw Hub'; document.getElementById('mailFromEmail').value = cfg.from_email || cfg.smtp_username || ''; }
function applyMailPreset() { const provider = document.getElementById('mailProvider').value; const preset = findMailPreset(provider); if (provider !== 'custom') { document.getElementById('mailHost').value = preset.smtp_host || ''; document.getElementById('mailPort').value = preset.smtp_port ? String(preset.smtp_port) : ''; document.getElementById('mailEncryption').value = preset.smtp_encryption || 'auto'; if (!document.getElementById('mailFromEmail').value.trim()) document.getElementById('mailFromEmail').value = document.getElementById('mailUsername').value.trim(); } }
function collectMailConfig() { const host = document.getElementById('mailHost').value.trim(); const username = document.getElementById('mailUsername').value.trim(); const password = document.getElementById('mailPassword').value; const fromEmail = document.getElementById('mailFromEmail').value.trim(); const provider = document.getElementById('mailProvider').value || 'custom'; if (!host || !username || !password || !fromEmail) throw new Error(tr('mailRequiredFields')); const parsedPort = Number(document.getElementById('mailPort').value || 0); return { enabled: true, provider, smtp_host: host, smtp_port: parsedPort > 0 ? parsedPort : findMailPreset(provider).smtp_port || 587, smtp_encryption: document.getElementById('mailEncryption').value || 'auto', smtp_username: username, smtp_password: password, from_name: document.getElementById('mailFromName').value.trim() || 'MaClaw Hub', from_email: fromEmail }; }
async function loadMailConfig() { try { const data = await api('/api/admin/mail/config'); renderMailConfig(data || {}); } catch (err) { const msg = tr('mailConfigLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function saveMailConfig() { try { const payload = collectMailConfig(); const data = await api('/api/admin/mail/config', { method: 'POST', body: JSON.stringify(payload) }); renderMailConfig(data || payload); const msg = tr('mailConfigSaved'); setOutput(msg); showToast(msg, 'success'); return data || payload; } catch (err) { const msg = tr('mailConfigSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); throw err; } }
// Machines runtime moved to machines-tab.js
async function sendTestMail() { try { const email = document.getElementById('testMailEmail').value.trim(); if (!email) { const msg = tr('testRecipientRequired'); setOutput(msg); showToast(msg, 'error'); return; } await saveMailConfig(); const data = await api('/api/admin/mail/test', { method: 'POST', body: JSON.stringify({ email }) }); const msg = data.message || tr('mailSent'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = tr('mailFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } }
async function changeAdminPassword() { const currentPassword = document.getElementById('currentPasswordInput').value; const newPassword = document.getElementById('newPasswordInput').value; const confirmPassword = document.getElementById('confirmPasswordInput').value; if (!currentPassword || !newPassword) { const msg = tr('requestFailed'); setOutput(msg); showToast(msg, 'error'); return; } if (newPassword !== confirmPassword) { const msg = ptr('mismatch'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/password', { method: 'POST', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) }); if (data.access_token) localStorage.setItem(adminTokenKey, data.access_token); if (data.admin) localStorage.setItem(adminProfileKey, JSON.stringify(data.admin)); document.getElementById('currentPasswordInput').value = ''; document.getElementById('newPasswordInput').value = ''; document.getElementById('confirmPasswordInput').value = ''; refreshAdminHeader(); const msg = ptr('changed'); setOutput(msg); showToast(msg, 'success'); } catch (err) { setOutput(err.message); showToast(err.message, 'error'); } }
async function updateAdminProfile() { const email = document.getElementById('adminEmailInput').value.trim(); if (!email) { const msg = prf('required'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/profile', { method: 'POST', body: JSON.stringify({ email }) }); if (data.access_token) localStorage.setItem(adminTokenKey, data.access_token); if (data.admin) localStorage.setItem(adminProfileKey, JSON.stringify(data.admin)); refreshAdminHeader(); const msg = prf('saved'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = prf('failed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } }
