/*
 * IM admin module.
 * ASCII only.
 */
(function(global) {
  function imState() {
    if (!global.__imAdminState) global.__imAdminState = { bridgeChannelsCache: [] };
    return global.__imAdminState;
  }

  function bindingCard(title, meta, actionText, onclickExpr) {
    return '<div class="item" style="margin-bottom:8px"><div class="item-head"><div><div class="item-title">' + escapeHtml(title || '') + '</div><div class="item-meta mono">' + escapeHtml(meta || '') + '</div></div><button class="btn-danger" style="height:32px;font-size:12px;padding:0 10px" onclick="' + onclickExpr + '">' + escapeHtml(actionText || '') + '</button></div></div>';
  }

  const DINGTALK_I18N = {
    en: {
      navLabel: 'DingTalk', navDesc: 'DingTalk bot integration', title: 'DingTalk Bot', desc: 'Configure DingTalk bot integration', reload: 'Reload', enabled: 'Enable', clientId: 'Client ID (AppKey)', clientSecret: 'Client Secret (AppSecret)', save: 'Save', guideTitle: 'DingTalk Setup Guide', guideContent: 'Visit <a href="https://open-dev.dingtalk.com" target="_blank">DingTalk Open Platform</a>.<br>Fill in AppKey (Client ID) and AppSecret (Client Secret).<br>Webhook: <code>{hub_url}/api/dingtalk/webhook</code><br>Enable Stream mode.<br><br><b>Note:</b><br>After Hub restarts, the bot reconnects within 6 seconds automatically.', bindingsTitle: 'Bindings', bindingsDesc: 'DingTalk user bindings', noBindings: 'No bindings loaded yet.', loadFailed: 'Load DingTalk config failed: {error}', saved: 'DingTalk config saved.', saveFailed: 'Save DingTalk config failed: {error}', bindingsLoadFailed: 'Load DingTalk bindings failed: {error}', unbind: 'Unbind', unbindConfirm: 'Remove DingTalk binding for {id}?', unbindSuccess: 'Unbind succeeded.', unbindFailed: 'Unbind failed: {error}'
    },
    zh: {
      navLabel: '\u9489\u9489', navDesc: '\u9489\u9489\u673a\u5668\u4eba\u96c6\u6210', title: '\u9489\u9489\u673a\u5668\u4eba', desc: '\u914d\u7f6e\u9489\u9489\u673a\u5668\u4eba\u96c6\u6210', reload: '\u5237\u65b0', enabled: '\u542f\u7528', clientId: 'Client ID (AppKey)', clientSecret: 'Client Secret (AppSecret)', save: '\u4fdd\u5b58', guideTitle: '\u9489\u9489\u63a5\u5165\u6307\u5357', guideContent: '\u8bbf\u95ee <a href="https://open-dev.dingtalk.com" target="_blank">\u9489\u9489\u5f00\u653e\u5e73\u53f0</a>\u3002<br>\u586b\u5199 AppKey (Client ID) \u548c AppSecret (Client Secret)\u3002<br>Webhook: <code>{hub_url}/api/dingtalk/webhook</code><br>\u542f\u7528 Stream \u6a21\u5f0f\u3002<br><br><b>\u6ce8\u610f\uff1a</b><br>Hub \u91cd\u542f\u540e\uff0c\u673a\u5668\u4eba\u4f1a\u5728 6 \u79d2\u5185\u81ea\u52a8\u91cd\u8fde\u3002', bindingsTitle: '\u7ed1\u5b9a', bindingsDesc: '\u9489\u9489\u7528\u6237\u7ed1\u5b9a', noBindings: '\u6682\u65e0\u7ed1\u5b9a\u8bb0\u5f55\u3002\u7528\u6237\u5728\u9489\u9489\u4e2d\u53d1\u9001\u90ae\u7bb1\u5730\u5740\u5373\u53ef\u7ed1\u5b9a\u3002', loadFailed: '\u52a0\u8f7d\u9489\u9489\u914d\u7f6e\u5931\u8d25: {error}', saved: '\u9489\u9489\u914d\u7f6e\u5df2\u4fdd\u5b58', saveFailed: '\u4fdd\u5b58\u9489\u9489\u914d\u7f6e\u5931\u8d25: {error}', bindingsLoadFailed: '\u52a0\u8f7d\u9489\u9489\u7ed1\u5b9a\u5931\u8d25: {error}', unbind: '\u89e3\u7ed1', unbindConfirm: '\u786e\u8ba4\u89e3\u7ed1 {id} \u5417\uff1f', unbindSuccess: '\u89e3\u7ed1\u6210\u529f', unbindFailed: '\u89e3\u7ed1\u5931\u8d25: {error}'
    }
  };
  const CONTENT_AUDIT_I18N = {
    en: { navLabel: 'Content Audit', navDesc: 'Content Audit', title: 'Content Audit', desc: 'Configure IM content audit', reload: 'Reload', programPath: 'Program Path', programPathPlaceholder: './audit_program', timeout: 'Timeout (seconds)', timeoutPolicy: 'Timeout Policy', timeoutBlock: 'block', timeoutPass: 'pass', keywords: 'Keywords', keywordsPlaceholder: 'one keyword per line', save: 'Save', guideTitle: 'Content Audit Guide', guideContent: 'External audit program integration.<br>See hub/cmd/audit_program/ for reference.<br>Keyword-based filtering.<br>Stdin JSON input, stdout JSON output.' },
    zh: { navLabel: '\u5185\u5bb9\u5ba1\u6838', navDesc: '\u5185\u5bb9\u5ba1\u6838', title: '\u5185\u5bb9\u5ba1\u6838', desc: '\u914d\u7f6e IM \u5185\u5bb9\u5ba1\u6838', reload: '\u5237\u65b0', programPath: '\u7a0b\u5e8f\u8def\u5f84', programPathPlaceholder: './audit_program', timeout: '\u8d85\u65f6\uff08\u79d2\uff09', timeoutPolicy: '\u8d85\u65f6\u7b56\u7565', timeoutBlock: 'block', timeoutPass: 'pass', keywords: '\u5173\u952e\u8bcd', keywordsPlaceholder: '\u6bcf\u884c\u4e00\u4e2a\u5173\u952e\u8bcd', save: '\u4fdd\u5b58', guideTitle: '\u5185\u5bb9\u5ba1\u6838\u6307\u5357', guideContent: '\u5916\u90e8\u5ba1\u6838\u7a0b\u5e8f\u96c6\u6210\u3002<br>\u53ef\u53c2\u8003 hub/cmd/audit_program/\u3002<br>\u652f\u6301\u57fa\u4e8e\u5173\u952e\u8bcd\u7684\u8fc7\u6ee4\u3002<br>Stdin JSON \u8f93\u5165\uff0cstdout JSON \u8f93\u51fa\u3002' }
  };
  const HUB_LLM_PANE_I18N = {
    en: { navLabel: 'Hub LLM', navDesc: 'Hub LLM', title: 'Hub LLM Configuration', desc: 'Configure Hub-level LLM provider', enabled: 'Enable Hub LLM', smartRoute: 'Smart route single-device LLM', apiUrl: 'API Base URL', apiKey: 'API Key', model: 'Model', protocol: 'Protocol', apiUrlPlaceholder: 'https://api.deepseek.com/v1', apiKeyPlaceholder: 'sk-...', modelPlaceholder: 'deepseek-chat', save: 'Save', guideTitle: 'Hub LLM Guide', guideContent: 'Supports DeepSeek V3, OpenAI compatible APIs.<br>User-Agent: OpenClaw/1.0<br>Used for intent classification and smart routing.<br>Fallback to keyword matching when disabled.<br>Smart route single-device LLM routes to local LLM when only one device is online.', protocolOpenAI: 'OpenAI', protocolAnthropic: 'Anthropic' },
    zh: { navLabel: 'Hub LLM', navDesc: 'Hub LLM', title: 'Hub LLM \u914d\u7f6e', desc: '\u914d\u7f6e Hub \u7ea7 LLM \u670d\u52a1\u5546', enabled: '\u542f\u7528 Hub LLM', smartRoute: '\u5355\u8bbe\u5907\u667a\u80fd\u8def\u7531 LLM', apiUrl: 'API \u57fa\u5730\u5740', apiKey: 'API Key', model: '\u6a21\u578b', protocol: '\u534f\u8bae', apiUrlPlaceholder: 'https://api.deepseek.com/v1', apiKeyPlaceholder: 'sk-...', modelPlaceholder: 'deepseek-chat', save: '\u4fdd\u5b58', guideTitle: 'Hub LLM \u6307\u5357', guideContent: '\u652f\u6301 DeepSeek V3 \u7b49 OpenAI \u517c\u5bb9 API\u3002<br>User-Agent: OpenClaw/1.0<br>\u7528\u4e8e\u610f\u56fe\u5206\u7c7b\u548c\u667a\u80fd\u8def\u7531\u3002<br>\u5173\u95ed\u65f6\u56de\u9000\u4e3a\u5173\u952e\u8bcd\u5339\u914d\u3002<br>\u5355\u8bbe\u5907\u667a\u80fd\u8def\u7531 LLM \u4f1a\u5728\u53ea\u6709\u4e00\u53f0\u8bbe\u5907\u5728\u7ebf\u65f6\u8def\u7531\u5230\u672c\u5730 LLM\u3002', protocolOpenAI: 'OpenAI', protocolAnthropic: 'Anthropic' }
  };
  const dtk = (key, vars = {}) => ((DINGTALK_I18N[currentLang] || DINGTALK_I18N.en)[key] || DINGTALK_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
  const cai = (key, vars = {}) => ((CONTENT_AUDIT_I18N[currentLang] || CONTENT_AUDIT_I18N.en)[key] || CONTENT_AUDIT_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
  const hpi = (key, vars = {}) => ((HUB_LLM_PANE_I18N[currentLang] || HUB_LLM_PANE_I18N.en)[key] || HUB_LLM_PANE_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
  function applyLegacyImI18n() {
    _s('wecomConfigTitle', 'textContent', wcm('title'));
    _s('wecomConfigDesc', 'textContent', wcm('desc'));
    _s('wecomReloadBtn', 'textContent', tr('reload'));
    _s('wecomEnabledLabel', 'textContent', currentLang === 'zh' ? '\u542f\u7528' : 'Enable');
    _s('wecomBotIdLabel', 'textContent', 'Bot ID');
    _s('wecomSecretLabel', 'textContent', wcm('appSecret'));
    _s('wecomSaveBtn', 'textContent', wcm('save'));
    _s('wecomGuideTitle', 'textContent', wcm('guideTitle'));
    _s('wecomGuideContent', 'innerHTML', wcm('guideContent'));
    _s('wecomBindingsTitle', 'textContent', wcm('bindingsTitle'));
    _s('wecomBindingsDesc', 'textContent', wcm('bindingsDesc'));
    _s('wecomBindingsReloadBtn', 'textContent', tr('reload'));
    _s('dingtalkConfigTitle', 'textContent', dtk('title'));
    _s('dingtalkConfigDesc', 'textContent', dtk('desc'));
    _s('dingtalkReloadBtn', 'textContent', dtk('reload'));
    _s('dingtalkEnabledLabel', 'textContent', dtk('enabled'));
    _s('dingtalkClientIdLabel', 'textContent', dtk('clientId'));
    _s('dingtalkClientSecretLabel', 'textContent', dtk('clientSecret'));
    _s('dingtalkSaveBtn', 'textContent', dtk('save'));
    _s('dingtalkGuideTitle', 'textContent', dtk('guideTitle'));
    _s('dingtalkGuideContent', 'innerHTML', dtk('guideContent'));
    _s('dingtalkBindingsTitle', 'textContent', dtk('bindingsTitle'));
    _s('dingtalkBindingsDesc', 'textContent', dtk('bindingsDesc'));
    _s('dingtalkBindingsReloadBtn', 'textContent', dtk('reload'));
    _s('imSubHubLlmNav', 'textContent', hpi('navLabel'));
    _s('imSubHubLlmNavDesc', 'textContent', hpi('navDesc'));
    _s('hubLlmPaneTitle', 'textContent', hpi('title'));
    _s('hubLlmPaneDesc', 'textContent', hpi('desc'));
    _s('hubLlmEnabledLabel', 'textContent', hpi('enabled'));
    _s('hubLlmSmartRouteSingleLabel', 'textContent', hpi('smartRoute'));
    _s('hubLlmApiUrlLabel', 'textContent', hpi('apiUrl'));
    _s('hubLlmApiKeyLabel', 'textContent', hpi('apiKey'));
    _s('hubLlmModelLabel', 'textContent', hpi('model'));
    _s('hubLlmProtocolLabel', 'textContent', hpi('protocol'));
    _s('hubLlmProtocolOpenAI', 'textContent', hpi('protocolOpenAI'));
    _s('hubLlmProtocolAnthropic', 'textContent', hpi('protocolAnthropic'));
    _s('hubLlmApiUrl', 'placeholder', hpi('apiUrlPlaceholder'));
    _s('hubLlmApiKey', 'placeholder', hpi('apiKeyPlaceholder'));
    _s('hubLlmModel', 'placeholder', hpi('modelPlaceholder'));
    _s('hubLlmSaveBtn', 'textContent', hpi('save'));
    _s('hubLlmGuideTitle', 'textContent', hpi('guideTitle'));
    _s('hubLlmGuideContent', 'innerHTML', hpi('guideContent'));
    _s('imSubContentAuditNav', 'textContent', cai('navLabel'));
    _s('imSubContentAuditNavDesc', 'textContent', cai('navDesc'));
    _s('contentAuditTitle', 'textContent', cai('title'));
    _s('contentAuditDesc', 'textContent', cai('desc'));
    _s('contentAuditReloadBtn', 'textContent', cai('reload'));
    _s('caProgPathLabel', 'textContent', cai('programPath'));
    _s('caProgPath', 'placeholder', cai('programPathPlaceholder'));
    _s('caTimeoutSecLabel', 'textContent', cai('timeout'));
    _s('caTimeoutPolicyLabel', 'textContent', cai('timeoutPolicy'));
    _s('caTimeoutPolicyBlock', 'textContent', cai('timeoutBlock'));
    _s('caTimeoutPolicyPass', 'textContent', cai('timeoutPass'));
    _s('caKeywordsLabel', 'textContent', cai('keywords'));
    _s('caKeywords', 'placeholder', cai('keywordsPlaceholder'));
    _s('contentAuditSaveBtn', 'textContent', cai('save'));
    _s('contentAuditGuideTitle', 'textContent', cai('guideTitle'));
    _s('contentAuditGuideContent', 'innerHTML', cai('guideContent'));
  }

  global.openImSub = function openImSub(sub) {
    document.querySelectorAll('.im-sidebar button').forEach(function(btn) { btn.classList.remove('active'); });
    document.querySelectorAll('.im-pane').forEach(function(pane) { pane.classList.remove('active'); });
    document.querySelectorAll('.im-sidebar button').forEach(function(btn) {
      if (btn.getAttribute('data-imsub') === sub) btn.classList.add('active');
    });
    const pane = document.getElementById('im-pane-' + sub);
    if (pane) pane.classList.add('active');
    if (sub === 'feishu') loadFeishuConfig();
    if (sub === 'openclaw') { global.loadOpenclawImConfig(); global.loadBridgeChannels(); }
    if (sub === 'qqbot') global.loadQQBotConfig();
    if (sub === 'wecom') global.loadWeComConfig();
    if (sub === 'dingtalk') global.loadDingTalkConfig();
    if (sub === 'hubllm') { loadHubLlmConfig(); loadHubLlmStatus(); }
    if (sub === 'contentaudit' && typeof loadContentAuditConfig === 'function') loadContentAuditConfig();
  };

  global.loadOpenclawImConfig = async function loadOpenclawImConfig() {
    try {
      const data = await api('/api/admin/settings/openclaw_im');
      document.getElementById('openclawImEnabled').checked = !!data.enabled;
      document.getElementById('openclawImWebhookUrl').value = data.webhook_url || 'http://127.0.0.1:3210/outbound';
      document.getElementById('openclawImSecret').value = data.secret || '';
    } catch (err) {
      const msg = ocim('loadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.saveOpenclawImConfig = async function saveOpenclawImConfig() {
    try {
      const payload = {
        enabled: document.getElementById('openclawImEnabled').checked,
        webhook_url: document.getElementById('openclawImWebhookUrl').value.trim(),
        secret: document.getElementById('openclawImSecret').value
      };
      const data = await api('/api/admin/settings/openclaw_im', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('openclawImEnabled').checked = !!data.enabled;
      document.getElementById('openclawImWebhookUrl').value = data.webhook_url || '';
      document.getElementById('openclawImSecret').value = data.secret || '';
      const msg = ocim('saved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = ocim('saveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.testOpenclawImWebhook = async function testOpenclawImWebhook() {
    const btn = document.getElementById('openclawImTestBtn');
    if (btn) {
      btn.disabled = true;
      btn.textContent = ocim('testing');
    }
    try {
      await global.saveOpenclawImConfig();
      const data = await api('/api/admin/settings/openclaw_im/test', { method: 'POST' });
      const msg = data.ok ? ocim('testSuccess', { status: String(data.status) }) : ocim('testFailed', { message: data.message || 'Unknown error' });
      setOutput(msg);
      showToast(msg, data.ok ? 'success' : 'error');
    } catch (err) {
      const msg = ocim('testFailed', { message: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    } finally {
      if (btn) {
        btn.disabled = false;
        btn.textContent = ocim('testWebhook');
      }
    }
  };

  global.loadBridgeChannels = async function loadBridgeChannels() {
    try {
      const data = await Promise.all([api('/api/admin/bridge/channels'), api('/api/admin/bridge/status')]);
      const channels = data[0];
      const status = data[1];
      imState().bridgeChannelsCache = channels.channels || [];
      const badge = document.getElementById('bridgeStatusBadge');
      if (badge) {
        const running = !!status.running;
        badge.textContent = running ? ocim('bridgeRunning') : ocim('bridgeStopped');
        badge.className = 'badge ' + (running ? 'ok' : 'warn');
      }
      global.renderBridgeChannels();
    } catch (err) {
      const msg = ocim('channelsLoadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.renderBridgeChannels = function renderBridgeChannels() {
    const root = document.getElementById('bridgeChannelsList');
    if (!root) return;
    const channels = imState().bridgeChannelsCache;
    const lang = global.currentLang;
    if (!channels.length) {
      root.innerHTML = '<div class="hint">No channels available.</div>';
      return;
    }
    root.innerHTML = channels.map(function(ch) {
      const name = escapeHtml(lang === 'zh' ? (ch.name_zh || ch.name) : ch.name);
      const desc = escapeHtml(lang === 'zh' ? (ch.desc_zh || ch.description) : ch.description);
      const installedBadge = ch.installed ? '<span class="badge ok" style="font-size:11px">' + ocim('channelInstalled') + '</span>' : '<span class="badge warn" style="font-size:11px">' + ocim('channelNotInstalled') + '</span>';
      const enabledChecked = ch.enabled ? 'checked' : '';
      const fields = (ch.fields || []).map(function(f) {
        const label = escapeHtml(lang === 'zh' ? (f.label_zh || f.label) : f.label);
        const val = escapeHtml((ch.config && ch.config[f.key]) || '');
        const inputType = f.type === 'password' ? 'password' : 'text';
        return '<div><label style="font-size:12px;font-weight:600;color:var(--muted)">' + label + '</label><input id="bridge_' + escapeHtml(ch.id) + '_' + escapeHtml(f.key) + '" type="' + inputType + '" value="' + val + '" placeholder="' + escapeHtml(f.placeholder || '') + '" style="font-size:13px"></div>';
      }).join('');
      return '<div style="border:1px solid var(--border);border-radius:10px;padding:14px;margin-bottom:12px"><div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px"><div style="display:flex;align-items:center;gap:8px"><label style="display:inline-flex;align-items:center;gap:6px;margin:0;cursor:pointer;font-size:13px;font-weight:600"><input type="checkbox" id="bridge_' + escapeHtml(ch.id) + '_enabled" ' + enabledChecked + '> ' + name + '</label>' + installedBadge + '</div><button class="btn-primary" style="height:32px;font-size:12px;padding:0 14px" onclick="saveBridgeChannel(' + JSON.stringify(String(ch.id || '')) + ')">' + ocim('channelSave') + '</button></div><div class="item-meta" style="margin-bottom:8px">' + desc + '</div><div class="grid2" style="gap:10px">' + fields + '</div></div>';
    }).join('');
  };

  global.saveBridgeChannel = async function saveBridgeChannel(channelId) {
    const ch = imState().bridgeChannelsCache.find(function(item) { return item.id === channelId; });
    if (!ch) return;
    const enabledEl = document.getElementById('bridge_' + channelId + '_enabled');
    const enabled = enabledEl ? !!enabledEl.checked : false;
    const fields = {};
    (ch.fields || []).forEach(function(f) {
      const el = document.getElementById('bridge_' + channelId + '_' + f.key);
      if (el) fields[f.key] = el.value.trim();
    });
    try {
      const data = await api('/api/admin/bridge/channels', { method: 'POST', body: JSON.stringify({ id: channelId, enabled: enabled, fields: fields }) });
      const name = currentLang === 'zh' ? (ch.name_zh || ch.name) : ch.name;
      let msg = ocim('channelSaved', { name: name });
      if (data.install_msg) msg += ' ' + data.install_msg;
      if (data.config_err) msg += ' config.json: ' + data.config_err;
      setOutput(msg);
      showToast(msg, data.config_err ? 'error' : 'success');
      await global.loadBridgeChannels();
    } catch (err) {
      const msg = ocim('channelSaveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadQQBotConfig = async function loadQQBotConfig() {
    try {
      const data = await api('/api/admin/settings/qqbot');
      document.getElementById('qqbotEnabled').checked = !!data.enabled;
      document.getElementById('qqbotAppId').value = data.app_id || '';
      document.getElementById('qqbotAppSecret').value = data.app_secret || '';
      global.loadQQBotBindings();
    } catch (err) {
      const msg = qqb('loadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.saveQQBotConfig = async function saveQQBotConfig() {
    try {
      const payload = { enabled: document.getElementById('qqbotEnabled').checked, app_id: document.getElementById('qqbotAppId').value.trim(), app_secret: document.getElementById('qqbotAppSecret').value };
      const data = await api('/api/admin/settings/qqbot', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('qqbotEnabled').checked = !!data.enabled;
      document.getElementById('qqbotAppId').value = data.app_id || '';
      document.getElementById('qqbotAppSecret').value = data.app_secret || '';
      const msg = qqb('saved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = qqb('saveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadQQBotBindings = async function loadQQBotBindings() {
    try {
      const data = await api('/api/admin/qqbot/bindings');
      const bindings = data.bindings || [];
      const root = document.getElementById('qqbotBindingsList');
      if (!root) return;
      if (!bindings.length) {
        root.innerHTML = '<div class="hint">' + qqb('noBindings') + '</div>';
        return;
      }
      root.innerHTML = bindings.map(function(b) { return bindingCard(b.email, b.open_id, qqb('unbind'), 'unbindQQBot(' + JSON.stringify(String(b.open_id || '')) + ')'); }).join('');
    } catch (err) {
      const msg = qqb('bindingsLoadFailed', { error: err.message });
      setOutput(msg);
    }
  };

  global.unbindQQBot = async function unbindQQBot(openId) {
    if (!confirm(qqb('unbindConfirm', { email: openId }))) return;
    try {
      await api('/api/admin/qqbot/bindings?open_id=' + encodeURIComponent(openId), { method: 'DELETE' });
      const msg = qqb('unbindSuccess');
      setOutput(msg);
      showToast(msg, 'success');
      global.loadQQBotBindings();
    } catch (err) {
      const msg = qqb('unbindFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadWeComConfig = async function loadWeComConfig() {
    try {
      const data = await api('/api/admin/settings/wecom');
      document.getElementById('wecomEnabled').checked = !!data.enabled;
      document.getElementById('wecomBotId').value = data.bot_id || '';
      document.getElementById('wecomSecret').value = data.secret || '';
      global.loadWeComBindings();
    } catch (err) {
      const msg = wcm('loadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.saveWeComConfig = async function saveWeComConfig() {
    try {
      const payload = { enabled: document.getElementById('wecomEnabled').checked, bot_id: document.getElementById('wecomBotId').value.trim(), secret: document.getElementById('wecomSecret').value };
      const data = await api('/api/admin/settings/wecom', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('wecomEnabled').checked = !!data.enabled;
      document.getElementById('wecomBotId').value = data.bot_id || '';
      document.getElementById('wecomSecret').value = data.secret || '';
      const msg = wcm('saved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = wcm('saveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadWeComBindings = async function loadWeComBindings() {
    try {
      const data = await api('/api/admin/wecom/bindings');
      const bindings = data.bindings || [];
      const root = document.getElementById('wecomBindingsList');
      if (!root) return;
      if (!bindings.length) {
        root.innerHTML = '<div class="hint">' + wcm('noBindings') + '</div>';
        return;
      }
      root.innerHTML = bindings.map(function(b) { return bindingCard(b.email, b.userid, wcm('unbind'), 'unbindWeCom(' + JSON.stringify(String(b.userid || '')) + ')'); }).join('');
    } catch (err) {
      const msg = wcm('bindingsLoadFailed', { error: err.message });
      setOutput(msg);
    }
  };

  global.unbindWeCom = async function unbindWeCom(userid) {
    if (!confirm(wcm('unbindConfirm', { email: userid }))) return;
    try {
      await api('/api/admin/wecom/bindings?userid=' + encodeURIComponent(userid), { method: 'DELETE' });
      const msg = wcm('unbindSuccess');
      setOutput(msg);
      showToast(msg, 'success');
      global.loadWeComBindings();
    } catch (err) {
      const msg = wcm('unbindFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadDingTalkConfig = async function loadDingTalkConfig() {
    try {
      const data = await api('/api/admin/settings/dingtalk');
      document.getElementById('dingtalkEnabled').checked = !!data.enabled;
      document.getElementById('dingtalkClientId').value = data.client_id || '';
      document.getElementById('dingtalkClientSecret').value = data.client_secret || '';
      global.loadDingTalkBindings();
    } catch (err) {
      const msg = dtk('loadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.saveDingTalkConfig = async function saveDingTalkConfig() {
    try {
      const payload = { enabled: document.getElementById('dingtalkEnabled').checked, client_id: document.getElementById('dingtalkClientId').value.trim(), client_secret: document.getElementById('dingtalkClientSecret').value };
      const data = await api('/api/admin/settings/dingtalk', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('dingtalkEnabled').checked = !!data.enabled;
      document.getElementById('dingtalkClientId').value = data.client_id || '';
      document.getElementById('dingtalkClientSecret').value = data.client_secret || '';
      const msg = dtk('saved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = dtk('saveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadDingTalkBindings = async function loadDingTalkBindings() {
    try {
      const data = await api('/api/admin/dingtalk/bindings');
      const bindings = data.bindings || [];
      const root = document.getElementById('dingtalkBindingsList');
      if (!root) return;
      if (!bindings.length) {
        root.innerHTML = '<div class="hint">' + dtk('noBindings') + '</div>';
        return;
      }
      root.innerHTML = bindings.map(function(b) { return bindingCard(b.email, b.staff_id, dtk('unbind'), 'unbindDingTalk(' + JSON.stringify(String(b.staff_id || '')) + ')'); }).join('');
    } catch (err) {
      setOutput(dtk('bindingsLoadFailed', { error: err.message }));
    }
  };

  global.unbindDingTalk = async function unbindDingTalk(staffId) {
    if (!confirm(dtk('unbindConfirm', { id: staffId }))) return;
    try {
      await api('/api/admin/dingtalk/bindings?staff_id=' + encodeURIComponent(staffId), { method: 'DELETE' });
      const msg = dtk('unbindSuccess');
      setOutput(msg);
      showToast(msg, 'success');
      global.loadDingTalkBindings();
    } catch (err) {
      const msg = dtk('unbindFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };
  applyLegacyImI18n();
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() {
      applyLegacyImI18n();
      global.loadWeComBindings();
      global.loadDingTalkBindings();
    });
  }
})(window);