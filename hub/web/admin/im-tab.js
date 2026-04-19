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
      setOutput('\u9489\u9489\u914d\u7f6e\u52a0\u8f7d\u5931\u8d25: ' + err.message);
      showToast('\u9489\u9489\u914d\u7f6e\u52a0\u8f7d\u5931\u8d25', 'error');
    }
  };

  global.saveDingTalkConfig = async function saveDingTalkConfig() {
    try {
      const payload = { enabled: document.getElementById('dingtalkEnabled').checked, client_id: document.getElementById('dingtalkClientId').value.trim(), client_secret: document.getElementById('dingtalkClientSecret').value };
      const data = await api('/api/admin/settings/dingtalk', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('dingtalkEnabled').checked = !!data.enabled;
      document.getElementById('dingtalkClientId').value = data.client_id || '';
      document.getElementById('dingtalkClientSecret').value = data.client_secret || '';
      setOutput('\u9489\u9489\u914d\u7f6e\u5df2\u4fdd\u5b58');
      showToast('\u9489\u9489\u914d\u7f6e\u5df2\u4fdd\u5b58', 'success');
    } catch (err) {
      setOutput('\u9489\u9489\u914d\u7f6e\u4fdd\u5b58\u5931\u8d25: ' + err.message);
      showToast('\u9489\u9489\u914d\u7f6e\u4fdd\u5b58\u5931\u8d25', 'error');
    }
  };

  global.loadDingTalkBindings = async function loadDingTalkBindings() {
    try {
      const data = await api('/api/admin/dingtalk/bindings');
      const bindings = data.bindings || [];
      const root = document.getElementById('dingtalkBindingsList');
      if (!root) return;
      if (!bindings.length) {
        root.innerHTML = '<div class="hint">\u6682\u65e0\u7ed1\u5b9a\u8bb0\u5f55\u3002\u7528\u6237\u5728\u9489\u9489\u4e2d\u53d1\u9001\u90ae\u7bb1\u5730\u5740\u5373\u53ef\u7ed1\u5b9a\u3002</div>';
        return;
      }
      root.innerHTML = bindings.map(function(b) { return bindingCard(b.email, b.staff_id, '\u89e3\u7ed1', 'unbindDingTalk(' + JSON.stringify(String(b.staff_id || '')) + ')'); }).join('');
    } catch (err) {
      setOutput('\u9489\u9489\u7ed1\u5b9a\u52a0\u8f7d\u5931\u8d25: ' + err.message);
    }
  };

  global.unbindDingTalk = async function unbindDingTalk(staffId) {
    if (!confirm('\u786e\u8ba4\u89e3\u7ed1 ' + staffId + '\uff1f')) return;
    try {
      await api('/api/admin/dingtalk/bindings?staff_id=' + encodeURIComponent(staffId), { method: 'DELETE' });
      setOutput('\u89e3\u7ed1\u6210\u529f');
      showToast('\u89e3\u7ed1\u6210\u529f', 'success');
      global.loadDingTalkBindings();
    } catch (err) {
      setOutput('\u89e3\u7ed1\u5931\u8d25: ' + err.message);
      showToast('\u89e3\u7ed1\u5931\u8d25', 'error');
    }
  };
})(window);