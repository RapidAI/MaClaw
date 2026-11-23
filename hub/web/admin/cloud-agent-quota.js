'use strict';

var CLOUD_AGENT_QUOTA_I18N = {
  en: {
    title: 'Cloud agent limits',
    hint: 'Tenant default. A user override inherits a blank field and cannot turn the feature on.',
    enabled: 'Allow cloud agents',
    instances: 'Instance limit',
    turns: 'Concurrent turns',
    user: 'User id override',
    userInstances: 'User instance limit',
    userTurns: 'User concurrent turns',
    save: 'Save',
    reload: 'Reload',
    saved: 'Saved',
    bad: 'Limits must be zero or a positive integer',
    loadFailed: 'Load cloud agent limits failed: {error}',
    saveFailed: 'Save cloud agent limits failed: {error}'
  },
  zh: {
    title: '\u4e91\u7aef Agent \u9650\u989d',
    hint: '\u79df\u6237\u9ed8\u8ba4\u3002\u7528\u6237\u8986\u76d6\u7559\u7a7a\u5219\u7ee7\u627f\uff0c\u4e0d\u80fd\u5355\u72ec\u6253\u5f00\u529f\u80fd\u3002',
    enabled: '\u5141\u8bb8\u4e91\u7aef Agent',
    instances: '\u5b9e\u4f8b\u4e0a\u9650',
    turns: '\u540c\u65f6\u8f6e\u6570',
    user: '\u7528\u6237\u8986\u76d6',
    userInstances: '\u7528\u6237\u5b9e\u4f8b\u4e0a\u9650',
    userTurns: '\u7528\u6237\u540c\u65f6\u8f6e\u6570',
    save: '\u4fdd\u5b58',
    reload: '\u91cd\u65b0\u52a0\u8f7d',
    saved: '\u5df2\u4fdd\u5b58',
    bad: '\u4e0a\u9650\u5fc5\u987b\u662f 0 \u6216\u6b63\u6574\u6570',
    loadFailed: '\u8bfb\u53d6\u4e91\u7aef Agent \u9650\u989d\u5931\u8d25\uff1a{error}',
    saveFailed: '\u4fdd\u5b58\u4e91\u7aef Agent \u9650\u989d\u5931\u8d25\uff1a{error}'
  }
};

function cloudAgentQuotaText() {
  var lang = (typeof getCurrentLanguage === 'function' && getCurrentLanguage() === 'en') ? 'en' : 'zh';
  return CLOUD_AGENT_QUOTA_I18N[lang] || CLOUD_AGENT_QUOTA_I18N.zh;
}

function applyCloudAgentQuotaI18n() {
  var t = cloudAgentQuotaText();
  var set = function (id, value) {
    var el = document.getElementById(id);
    if (el) el.textContent = value;
  };
  set('cloudAgentQuotaTitle', t.title);
  set('cloudAgentQuotaHint', t.hint);
  set('cloudAgentQuotaEnabledLabel', t.enabled);
  set('cloudAgentQuotaInstancesLabel', t.instances);
  set('cloudAgentQuotaTurnsLabel', t.turns);
  set('cloudAgentQuotaUserLabel', t.user);
  set('cloudAgentQuotaUserInstancesLabel', t.userInstances);
  set('cloudAgentQuotaUserTurnsLabel', t.userTurns);
  set('cloudAgentQuotaSaveBtn', t.save);
  set('cloudAgentQuotaReloadBtn', t.reload);
}

function cloudAgentQuotaInt(id) {
  var el = document.getElementById(id);
  var raw = el ? String(el.value || '').trim() : '';
  if (raw === '') return null;
  if (!/^\d+$/.test(raw)) return undefined;
  return Number(raw);
}

async function loadCloudAgentQuota() {
  applyCloudAgentQuotaI18n();
  var status = document.getElementById('cloudAgentQuotaStatus');
  try {
    var data = await api('/api/admin/cloud-agents/settings');
    var enabled = document.getElementById('cloudAgentQuotaEnabled');
    var instances = document.getElementById('cloudAgentQuotaInstances');
    var turns = document.getElementById('cloudAgentQuotaTurns');
    if (enabled) enabled.checked = !!(data && data.cloud_agents_enabled);
    if (instances) instances.value = data && data.max_instances != null ? String(data.max_instances) : '3';
    if (turns) turns.value = data && data.max_concurrent_turns != null ? String(data.max_concurrent_turns) : '1';
    if (status) status.textContent = '';
    return data;
  } catch (err) {
    var msg = cloudAgentQuotaText().loadFailed.replace('{error}', err.message || String(err));
    if (status) status.textContent = msg;
    if (typeof showToast === 'function') showToast(msg, 'error');
    return null;
  }
}

async function saveCloudAgentQuota() {
  var t = cloudAgentQuotaText();
  var instances = cloudAgentQuotaInt('cloudAgentQuotaInstances');
  var turns = cloudAgentQuotaInt('cloudAgentQuotaTurns');
  if (instances == null || turns == null || instances === undefined || turns === undefined) {
    if (typeof showToast === 'function') showToast(t.bad, 'error');
    return;
  }
  var enabled = document.getElementById('cloudAgentQuotaEnabled');
  var body = {
    cloud_agents_enabled: !!(enabled && enabled.checked),
    max_instances: instances,
    max_concurrent_turns: turns
  };
  var button = document.getElementById('cloudAgentQuotaSaveBtn');
  if (button) button.disabled = true;
  try {
    await api('/api/admin/cloud-agents/settings', { method: 'PATCH', body: JSON.stringify(body) });
    var userEl = document.getElementById('cloudAgentQuotaUser');
    var user = userEl ? String(userEl.value || '').trim() : '';
    if (user) {
      var userInstances = cloudAgentQuotaInt('cloudAgentQuotaUserInstances');
      var userTurns = cloudAgentQuotaInt('cloudAgentQuotaUserTurns');
      if (userInstances === undefined || userTurns === undefined) {
        if (typeof showToast === 'function') showToast(t.bad, 'error');
        return;
      }
      await api('/api/admin/cloud-agents/users/{id}/limits'.replace('{id}', encodeURIComponent(user)), {
        method: 'PATCH',
        body: JSON.stringify({
          max_instances: userInstances,
          max_concurrent_turns: userTurns
        })
      });
    }
    await loadCloudAgentQuota();
    if (typeof showToast === 'function') showToast(t.saved, 'success');
  } catch (err) {
    var msg = t.saveFailed.replace('{error}', err.message || String(err));
    if (typeof showToast === 'function') showToast(msg, 'error');
  } finally {
    if (button) button.disabled = false;
  }
}
