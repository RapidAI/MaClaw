// Profile, public URL, mail settings, and first-run setup checks.
(() => {
      const PROFILE_TEXT = {
        en: { title: 'Administrator Profile', desc: 'Update the administrator email used for hub registration confirmation and platform notices.', email: 'Administrator email', username: 'Administrator username', usernameValue: 'Current username', action: 'Save Email', saved: 'Administrator email updated.', failed: 'Update administrator email failed: {error}', required: 'Please enter an administrator email.' },
        zh: {}
      };
      const SERVER_TEXT = {
        en: { title: 'Public Domain', desc: 'Set the external Hub Center URL used when building hub registration confirmation links.', publicBaseURL: 'Hub Center public URL', save: 'Save Public URL', saved: 'Hub Center public URL saved.', failed: 'Save Hub Center public URL failed: {error}', loadFailed: 'Load Hub Center public URL failed: {error}', publicUrlRequired: 'Please enter a public URL.' },
        zh: {}
      };
      const MAIL_TEXT = {
        en: { title: 'Mail Delivery', desc: 'Configure SMTP delivery for hub registration confirmation and test common providers such as QQ, 163, 126, Gmail, and Outlook.', provider: 'Provider Preset', presetCustom: 'Custom SMTP', presetQQ: 'QQ Mail', preset163: '163 Mail', preset126: '126 Mail', presetGmail: 'Gmail', presetOutlook: 'Outlook / Office 365', host: 'SMTP Host', port: 'SMTP Port', encryption: 'Encryption', encryptionAuto: 'Auto', encryptionSSL: 'SSL / TLS', encryptionStartTLS: 'STARTTLS', encryptionPlain: 'Plain', username: 'SMTP Username', password: 'SMTP Password / App Password', fromName: 'From Name', fromEmail: 'From Email', testRecipient: 'Test recipient', save: 'Save Mail Settings', test: 'Send Test Mail', saved: 'Mail settings saved.', saveFailed: 'Save mail settings failed: {error}', loadFailed: 'Load mail settings failed: {error}', sent: 'Test email sent.', sendFailed: 'Test mail failed: {error}', required: 'Please complete the SMTP host, account, password, and sender email first.', recipientRequired: 'Please enter a test recipient email.' },
        zh: {}
      };
      const MAIL_PRESETS = { custom: { smtp_host: '', smtp_port: 587, smtp_encryption: 'auto' }, qq: { smtp_host: 'smtp.qq.com', smtp_port: 465, smtp_encryption: 'ssl' }, '163': { smtp_host: 'smtp.163.com', smtp_port: 465, smtp_encryption: 'ssl' }, '126': { smtp_host: 'smtp.126.com', smtp_port: 465, smtp_encryption: 'ssl' }, gmail: { smtp_host: 'smtp.gmail.com', smtp_port: 587, smtp_encryption: 'starttls' }, outlook: { smtp_host: 'smtp.office365.com', smtp_port: 587, smtp_encryption: 'starttls' } };
      const SETUP_GATE_TEXT = {
        en: { title: 'Setup Reminder', heading: 'Some settings still need attention', desc: 'The following items are not yet configured. You can dismiss this notice and configure them later.', openSystem: 'Open System Settings', refresh: 'Recheck', dismiss: 'Dismiss', adminEmail: 'Set an administrator email for hub registration confirmations and platform notices.', publicBaseURL: 'Set the Hub Center public URL used in confirmation emails.', mailConfig: 'Complete the SMTP mail configuration.', mailTest: 'Send a successful test email to verify mail delivery.' },
        zh: {}
      };
      Object.assign(PROFILE_TEXT.zh, {
        title: '\u7ba1\u7406\u5458\u8d44\u6599',
        desc: '\u4fee\u6539\u7528\u4e8e\u8282\u70b9\u6ce8\u518c\u786e\u8ba4\u548c\u5e73\u53f0\u901a\u77e5\u7684\u7ba1\u7406\u5458\u90ae\u7bb1\u3002',
        email: '\u7ba1\u7406\u5458\u90ae\u7bb1',
        username: '\u7ba1\u7406\u5458\u7528\u6237\u540d',
        usernameValue: '\u5f53\u524d\u7528\u6237\u540d',
        action: '\u4fdd\u5b58\u90ae\u7bb1',
        saved: '\u7ba1\u7406\u5458\u90ae\u7bb1\u5df2\u66f4\u65b0\u3002',
        failed: '\u66f4\u65b0\u7ba1\u7406\u5458\u90ae\u7bb1\u5931\u8d25\uff1a{error}',
        required: '\u8bf7\u8f93\u5165\u7ba1\u7406\u5458\u90ae\u7bb1\u3002'
      });
      Object.assign(SERVER_TEXT.zh, {
        title: '\u5bf9\u5916\u57df\u540d',
        desc: '\u8bbe\u7f6e\u8282\u70b9\u4e2d\u5fc3\u5bf9\u5916\u8bbf\u95ee\u5730\u5740\uff0c\u7528\u4e8e\u751f\u6210\u8282\u70b9\u6ce8\u518c\u786e\u8ba4\u94fe\u63a5\u3002',
        publicBaseURL: '\u8282\u70b9\u4e2d\u5fc3\u5bf9\u5916\u5730\u5740',
        save: '\u4fdd\u5b58\u5bf9\u5916\u5730\u5740',
        saved: '\u8282\u70b9\u4e2d\u5fc3\u5bf9\u5916\u5730\u5740\u5df2\u4fdd\u5b58\u3002',
        failed: '\u4fdd\u5b58\u8282\u70b9\u4e2d\u5fc3\u5bf9\u5916\u5730\u5740\u5931\u8d25\uff1a{error}',
        loadFailed: '\u52a0\u8f7d\u8282\u70b9\u4e2d\u5fc3\u5bf9\u5916\u5730\u5740\u5931\u8d25\uff1a{error}',
        publicUrlRequired: '\u8bf7\u8f93\u5165\u5bf9\u5916\u5730\u5740\u3002'
      });
      Object.assign(MAIL_TEXT.zh, {
        title: '\u90ae\u4ef6\u53d1\u9001',
        desc: '\u914d\u7f6e\u7528\u4e8e\u8282\u70b9\u6ce8\u518c\u786e\u8ba4\u7684 SMTP \u90ae\u4ef6\u53d1\u9001\uff0c\u5e76\u652f\u6301 QQ\u3001163\u3001126\u3001Gmail \u548c Outlook \u7b49\u5e38\u89c1\u670d\u52a1\u5546\u6d4b\u8bd5\u3002',
        provider: '\u670d\u52a1\u5546\u9884\u8bbe',
        presetCustom: '\u81ea\u5b9a\u4e49 SMTP',
        presetQQ: 'QQ \u90ae\u7bb1',
        preset163: '163 \u90ae\u7bb1',
        preset126: '126 \u90ae\u7bb1',
        presetGmail: 'Gmail',
        presetOutlook: 'Outlook / Office 365',
        host: 'SMTP \u4e3b\u673a',
        port: 'SMTP \u7aef\u53e3',
        encryption: '\u52a0\u5bc6\u65b9\u5f0f',
        encryptionAuto: '\u81ea\u52a8',
        encryptionSSL: 'SSL / TLS',
        encryptionStartTLS: 'STARTTLS',
        encryptionPlain: '\u660e\u6587',
        username: 'SMTP \u7528\u6237\u540d',
        password: 'SMTP \u5bc6\u7801 / \u6388\u6743\u7801',
        fromName: '\u53d1\u4ef6\u4eba\u540d\u79f0',
        fromEmail: '\u53d1\u4ef6\u90ae\u7bb1',
        testRecipient: '\u6d4b\u8bd5\u6536\u4ef6\u4eba',
        save: '\u4fdd\u5b58\u90ae\u4ef6\u8bbe\u7f6e',
        test: '\u53d1\u9001\u6d4b\u8bd5\u90ae\u4ef6',
        saved: '\u90ae\u4ef6\u8bbe\u7f6e\u5df2\u4fdd\u5b58\u3002',
        saveFailed: '\u4fdd\u5b58\u90ae\u4ef6\u8bbe\u7f6e\u5931\u8d25\uff1a{error}',
        loadFailed: '\u52a0\u8f7d\u90ae\u4ef6\u8bbe\u7f6e\u5931\u8d25\uff1a{error}',
        sent: '\u6d4b\u8bd5\u90ae\u4ef6\u5df2\u53d1\u9001\u3002',
        sendFailed: '\u6d4b\u8bd5\u90ae\u4ef6\u53d1\u9001\u5931\u8d25\uff1a{error}',
        required: '\u8bf7\u5148\u586b\u5199 SMTP \u4e3b\u673a\u3001\u8d26\u53f7\u3001\u5bc6\u7801\u4ee5\u53ca\u53d1\u4ef6\u90ae\u7bb1\u3002',
        recipientRequired: '\u8bf7\u8f93\u5165\u6d4b\u8bd5\u6536\u4ef6\u4eba\u90ae\u7bb1\u3002'
      });
      Object.assign(SETUP_GATE_TEXT.zh, {
        title: '\u8bbe\u7f6e\u63d0\u9192',
        heading: '\u4ee5\u4e0b\u8bbe\u7f6e\u9879\u5c1a\u672a\u5b8c\u6210',
        desc: '\u4ee5\u4e0b\u914d\u7f6e\u5c1a\u672a\u5b8c\u6210\uff0c\u60a8\u53ef\u4ee5\u5173\u95ed\u6b64\u63d0\u9192\uff0c\u7a0d\u540e\u518d\u914d\u7f6e\u3002',
        openSystem: '\u6253\u5f00\u7cfb\u7edf\u8bbe\u7f6e',
        refresh: '\u91cd\u65b0\u68c0\u67e5',
        dismiss: '\u5173\u95ed',
        adminEmail: '\u8bbe\u7f6e\u7ba1\u7406\u5458\u90ae\u7bb1\uff0c\u7528\u4e8e\u8282\u70b9\u6ce8\u518c\u786e\u8ba4\u548c\u5e73\u53f0\u901a\u77e5\u3002',
        publicBaseURL: '\u8bbe\u7f6e\u7528\u4e8e\u786e\u8ba4\u90ae\u4ef6\u7684\u8282\u70b9\u4e2d\u5fc3\u5bf9\u5916\u5730\u5740\u3002',
        mailConfig: '\u5b8c\u6210 SMTP \u90ae\u4ef6\u914d\u7f6e\u3002',
        mailTest: '\u53d1\u9001\u4e00\u6b21\u6d4b\u8bd5\u90ae\u4ef6\uff0c\u786e\u8ba4\u90ae\u4ef6\u80fd\u6b63\u5e38\u53d1\u9001\u3002'
      });
      const profileText = key => (PROFILE_TEXT[currentLang] || PROFILE_TEXT.en)[key] || PROFILE_TEXT.en[key] || key;
      const serverText = key => (SERVER_TEXT[currentLang] || SERVER_TEXT.en)[key] || SERVER_TEXT.en[key] || key;
      const mailText = key => (MAIL_TEXT[currentLang] || MAIL_TEXT.en)[key] || MAIL_TEXT.en[key] || key;
      const setupGateText = key => (SETUP_GATE_TEXT[currentLang] || SETUP_GATE_TEXT.en)[key] || SETUP_GATE_TEXT.en[key] || key;
      let serverConfigCache = null;
      let mailConfigCache = null;
      let setupGateItems = [];
      let setupGateSuppressed = false;
      function renderSetupGate() { document.querySelector('#setupGate .mini').textContent = setupGateText('title'); document.getElementById('setupGateHeading').textContent = setupGateText('heading'); document.getElementById('setupGateDesc').textContent = setupGateText('desc'); document.getElementById('setupGateAction').textContent = setupGateText('openSystem'); document.getElementById('setupGateRefresh').textContent = setupGateText('refresh'); var dismissBtn = document.querySelector('#setupGate button[onclick="dismissSetupGate()"]'); if (dismissBtn) dismissBtn.setAttribute('aria-label', setupGateText('dismiss')); document.getElementById('setupGateList').innerHTML = setupGateItems.map(key => `<div class="item"><div class="item-title">${setupGateText(key)}</div></div>`).join(''); document.getElementById('setupGate').classList.toggle('hidden', setupGateItems.length === 0 || !token() || setupGateSuppressed); }
      function evaluateSetupGate() { const profile = adminProfile() || {}; const serverCfg = serverConfigCache || {}; const mailCfg = mailConfigCache || {}; const items = []; if (!String(profile.email || '').trim()) items.push('adminEmail'); if (!String(serverCfg.public_base_url || '').trim()) items.push('publicBaseURL'); const mailComplete = !!(mailCfg.enabled && String(mailCfg.smtp_host || '').trim() && String(mailCfg.smtp_username || '').trim() && String(mailCfg.smtp_password || '').trim() && String(mailCfg.from_email || '').trim()); if (!mailComplete) items.push('mailConfig'); setupGateItems = items; if (!items.length) setupGateSuppressed = false; renderSetupGate(); }
      window.openSetupGateTarget = function() { setupGateSuppressed = true; document.getElementById('setupGate').classList.add('hidden'); openTab('system'); };
      window.recheckSetupGate = function() { setupGateSuppressed = false; evaluateSetupGate(); };
      window.dismissSetupGate = function() { setupGateSuppressed = true; document.getElementById('setupGate').classList.add('hidden'); };
      const baseApplyI18n = applyI18n;
      applyI18n = function() {
        baseApplyI18n();
        document.getElementById('serverCardTitle').textContent = serverText('title');
        document.getElementById('serverCardDesc').textContent = serverText('desc');
        document.getElementById('serverPublicBaseURLLabel').textContent = serverText('publicBaseURL');
        document.getElementById('saveServerConfigButton').textContent = serverText('save');
        document.getElementById('mailCardTitle').textContent = mailText('title');
        document.getElementById('mailCardDesc').textContent = mailText('desc');
        document.getElementById('mailProviderLabel').textContent = mailText('provider');
        document.getElementById('mailPresetCustom').textContent = mailText('presetCustom');
        document.getElementById('mailPresetQQ').textContent = mailText('presetQQ');
        document.getElementById('mailPreset163').textContent = mailText('preset163');
        document.getElementById('mailPreset126').textContent = mailText('preset126');
        document.getElementById('mailPresetGmail').textContent = mailText('presetGmail');
        document.getElementById('mailPresetOutlook').textContent = mailText('presetOutlook');
        document.getElementById('mailHostLabel').textContent = mailText('host');
        document.getElementById('mailPortLabel').textContent = mailText('port');
        document.getElementById('mailEncryptionLabel').textContent = mailText('encryption');
        document.getElementById('mailEncryptionAuto').textContent = mailText('encryptionAuto');
        document.getElementById('mailEncryptionSSL').textContent = mailText('encryptionSSL');
        document.getElementById('mailEncryptionStartTLS').textContent = mailText('encryptionStartTLS');
        document.getElementById('mailEncryptionPlain').textContent = mailText('encryptionPlain');
        document.getElementById('mailUsernameLabel').textContent = mailText('username');
        document.getElementById('mailPasswordLabel').textContent = mailText('password');
        document.getElementById('mailFromNameLabel').textContent = mailText('fromName');
        document.getElementById('mailFromEmailLabel').textContent = mailText('fromEmail');
        document.getElementById('mailTestRecipientLabel').textContent = mailText('testRecipient');
        document.getElementById('saveMailConfigButton').textContent = mailText('save');
        document.getElementById('sendTestMailButton').textContent = mailText('test');
        document.getElementById('profileCardTitle').textContent = profileText('title');
        document.getElementById('profileCardDesc').textContent = profileText('desc');
        document.getElementById('adminEmailLabel').textContent = profileText('email');
        document.getElementById('adminUsernameLabel').textContent = profileText('username');
        document.getElementById('adminUsernameInput').placeholder = profileText('usernameValue');
        document.getElementById('saveProfileButton').textContent = profileText('action');
        renderSetupGate();
      };
      const baseRefreshAdminHeader = refreshAdminHeader;
      refreshAdminHeader = function() {
        baseRefreshAdminHeader();
        if (!token()) setupGateSuppressed = false;
        const profile = adminProfile();
        document.getElementById('adminEmailInput').value = profile?.email || '';
        document.getElementById('adminUsernameInput').value = profile?.username || '';
        evaluateSetupGate();
      };
      window.loadServerConfig = async function() { try { const data = await api('/api/admin/server/config'); serverConfigCache = data || {}; document.getElementById('serverPublicBaseURL').value = data.public_base_url || ''; evaluateSetupGate(); } catch (err) { const msg = serverText('loadFailed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } };
      window.saveServerConfig = async function() { const publicBaseURL = document.getElementById('serverPublicBaseURL').value.trim(); if (!publicBaseURL) { const msg = serverText('failed').replace('{error}', serverText('publicUrlRequired')); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/server/config', { method: 'POST', body: JSON.stringify({ public_base_url: publicBaseURL }) }); serverConfigCache = data || { public_base_url: publicBaseURL }; document.getElementById('serverPublicBaseURL').value = data.public_base_url || publicBaseURL; evaluateSetupGate(); const msg = serverText('saved'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = serverText('failed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } };
      function findMailPreset(provider) { return MAIL_PRESETS[provider] || MAIL_PRESETS.custom; }
      function detectMailProvider(cfg) { const host = String(cfg?.smtp_host || '').trim().toLowerCase(); const port = Number(cfg?.smtp_port || 0); const encryption = String(cfg?.smtp_encryption || '').trim().toLowerCase(); for (const [provider, preset] of Object.entries(MAIL_PRESETS)) { if (provider === 'custom') continue; if (host === preset.smtp_host && (!port || port === preset.smtp_port) && (!encryption || encryption === preset.smtp_encryption)) return provider; } return String(cfg?.provider || '').trim() || 'custom'; }
      function renderMailConfig(cfg = {}) { const provider = detectMailProvider(cfg); document.getElementById('mailProvider').value = MAIL_PRESETS[provider] ? provider : 'custom'; document.getElementById('mailHost').value = cfg.smtp_host || ''; document.getElementById('mailPort').value = cfg.smtp_port ? String(cfg.smtp_port) : ''; document.getElementById('mailEncryption').value = cfg.smtp_encryption || 'auto'; document.getElementById('mailUsername').value = cfg.smtp_username || ''; document.getElementById('mailPassword').value = cfg.smtp_password || ''; document.getElementById('mailFromName').value = cfg.from_name || 'MaClaw Hub Center'; document.getElementById('mailFromEmail').value = cfg.from_email || cfg.smtp_username || ''; }
      window.applyMailPreset = function() { const provider = document.getElementById('mailProvider').value; const preset = findMailPreset(provider); if (provider !== 'custom') { document.getElementById('mailHost').value = preset.smtp_host || ''; document.getElementById('mailPort').value = preset.smtp_port ? String(preset.smtp_port) : ''; document.getElementById('mailEncryption').value = preset.smtp_encryption || 'auto'; if (!document.getElementById('mailFromEmail').value.trim()) document.getElementById('mailFromEmail').value = document.getElementById('mailUsername').value.trim(); } };
      function collectMailConfig() { const host = document.getElementById('mailHost').value.trim(); const username = document.getElementById('mailUsername').value.trim(); const password = document.getElementById('mailPassword').value; const fromEmail = document.getElementById('mailFromEmail').value.trim(); const provider = document.getElementById('mailProvider').value || 'custom'; if (!host || !username || !password || !fromEmail) throw new Error(mailText('required')); const parsedPort = Number(document.getElementById('mailPort').value || 0); return { enabled: true, provider, smtp_host: host, smtp_port: parsedPort > 0 ? parsedPort : findMailPreset(provider).smtp_port || 587, smtp_encryption: document.getElementById('mailEncryption').value || 'auto', smtp_username: username, smtp_password: password, from_name: document.getElementById('mailFromName').value.trim() || 'MaClaw Hub Center', from_email: fromEmail }; }
      window.loadMailConfig = async function() { try { const data = await api('/api/admin/mail/config'); mailConfigCache = data || {}; renderMailConfig(mailConfigCache); evaluateSetupGate(); } catch (err) { const msg = mailText('loadFailed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } };
      window.saveMailConfig = async function() { try { const payload = collectMailConfig(); const data = await api('/api/admin/mail/config', { method: 'POST', body: JSON.stringify(payload) }); mailConfigCache = data || payload; renderMailConfig(mailConfigCache); evaluateSetupGate(); const msg = mailText('saved'); setOutput(msg); showToast(msg, 'success'); return data || payload; } catch (err) { const msg = mailText('saveFailed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); throw err; } };
      window.sendTestMail = async function() { try { const email = document.getElementById('testMailEmail').value.trim(); if (!email) { const msg = mailText('recipientRequired'); setOutput(msg); showToast(msg, 'error'); return; } await saveMailConfig(); const data = await api('/api/admin/mail/test', { method: 'POST', body: JSON.stringify({ email }) }); mailConfigCache = { ...(mailConfigCache || {}), tested: true, tested_at: Date.now() / 1000 }; evaluateSetupGate(); const msg = data.message || mailText('sent'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = mailText('sendFailed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } };
      window.updateAdminProfile = async function() { const email = document.getElementById('adminEmailInput').value.trim(); if (!email) { const msg = profileText('required'); setOutput(msg); showToast(msg, 'error'); return; } try { const data = await api('/api/admin/profile', { method: 'POST', body: JSON.stringify({ email }) }); if (data.access_token) { sessionStorage.removeItem(adminTokenKey); localStorage.setItem(adminTokenKey, data.access_token); } if (data.admin) { sessionStorage.removeItem(adminProfileKey); localStorage.setItem(adminProfileKey, JSON.stringify(data.admin)); } refreshAdminHeader(); evaluateSetupGate(); const msg = profileText('saved'); setOutput(msg); showToast(msg, 'success'); } catch (err) { const msg = profileText('failed').replace('{error}', err.message); setOutput(msg); showToast(msg, 'error'); } };
      const baseRefreshAll = refreshAll;
      refreshAll = async function() { await Promise.all([baseRefreshAll(), loadMailConfig(), loadServerConfig()]); };
      applyI18n();
      refreshAdminHeader();
      if (token()) { loadMailConfig(); loadServerConfig(); }
    })();
