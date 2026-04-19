/*
 * Voiceprint admin module.
 * ASCII only.
 */
async function loadVpConfig() {
  try {
    var cfg = await api('/api/admin/voiceprint/config');
    document.getElementById('vpEnabled').checked = !!cfg.enabled;
    document.getElementById('vpServerUrl').value = cfg.server_url || '';
    document.getElementById('vpThreshold').value = cfg.threshold || 0.6;
  } catch (e) {
    showToast((currentLang === 'zh' ? '\u52a0\u8f7d\u58f0\u7eb9\u914d\u7f6e\u5931\u8d25: ' : 'Load voiceprint config failed: ') + e.message, 'error');
  }
}
async function saveVpConfig() {
  var cfg = {
    enabled: document.getElementById('vpEnabled').checked,
    server_url: document.getElementById('vpServerUrl').value.trim(),
    threshold: parseFloat(document.getElementById('vpThreshold').value) || 0.6
  };
  try {
    await api('/api/admin/voiceprint/config', { method: 'PUT', body: JSON.stringify(cfg) });
    showToast(currentLang === 'zh' ? '\u58f0\u7eb9\u914d\u7f6e\u5df2\u4fdd\u5b58' : 'Voiceprint config saved', 'success');
  } catch (e) {
    showToast((currentLang === 'zh' ? '\u4fdd\u5b58\u58f0\u7eb9\u914d\u7f6e\u5931\u8d25: ' : 'Save voiceprint config failed: ') + e.message, 'error');
  }
}
async function vpEnroll() {
  var email = document.getElementById('vpEnrollEmail').value.trim();
  var label = document.getElementById('vpEnrollLabel').value.trim() || 'default';
  var fileInput = document.getElementById('vpEnrollFile');
  if (!email) { showToast(currentLang === 'zh' ? '\u8bf7\u8f93\u5165\u90ae\u7bb1' : 'Email is required', 'info'); return; }
  if (!fileInput.files || !fileInput.files.length) { showToast(currentLang === 'zh' ? '\u8bf7\u9009\u62e9 WAV \u6587\u4ef6' : 'Select a WAV file', 'info'); return; }
  var fd = new FormData();
  fd.append('email', email);
  fd.append('label', label);
  fd.append('file', fileInput.files[0]);
  try {
    var res = await fetch('/api/admin/voiceprint/enroll', { method: 'POST', headers: { 'Authorization': 'Bearer ' + token() }, body: fd });
    var data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Enroll failed');
    showToast(currentLang === 'zh' ? '\u58f0\u7eb9\u6ce8\u518c\u6210\u529f (dim=' + (data.dim || '?') + ')' : 'Enrolled (dim=' + (data.dim || '?') + ')', 'success');
    setOutput(JSON.stringify(data, null, 2));
    fileInput.value = '';
    loadVoiceprints();
  } catch (e) {
    showToast((currentLang === 'zh' ? '\u58f0\u7eb9\u6ce8\u518c\u5931\u8d25: ' : 'Enroll failed: ') + e.message, 'error');
  }
}
async function vpIdentify() {
  var fileInput = document.getElementById('vpIdentifyFile');
  if (!fileInput.files || !fileInput.files.length) { showToast(currentLang === 'zh' ? '\u8bf7\u9009\u62e9 WAV \u6587\u4ef6' : 'Select a WAV file', 'info'); return; }
  var fd = new FormData();
  fd.append('file', fileInput.files[0]);
  var resultDiv = document.getElementById('vpIdentifyResult');
  try {
    var res = await fetch('/api/admin/voiceprint/identify', { method: 'POST', headers: { 'Authorization': 'Bearer ' + token() }, body: fd });
    var data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Identify failed');
    var matches = data.matches || [];
    if (!matches.length) {
      resultDiv.innerHTML = '<div class="hint">' + (currentLang === 'zh' ? '\u672a\u8bc6\u522b\u5230\u8bf4\u8bdd\u4eba' : 'No speaker identified') + '</div>';
    } else {
      var html = '<table style="width:100%;font-size:13px;border-collapse:collapse"><thead><tr style="border-bottom:2px solid var(--line)"><th style="text-align:left;padding:6px">' + (currentLang === 'zh' ? '\u90ae\u7bb1' : 'Email') + '</th><th style="text-align:left;padding:6px">' + (currentLang === 'zh' ? '\u6807\u7b7e' : 'Label') + '</th><th style="text-align:right;padding:6px">' + (currentLang === 'zh' ? '\u76f8\u4f3c\u5ea6' : 'Similarity') + '</th></tr></thead><tbody>';
      matches.forEach(function(m) {
        html += '<tr style="border-bottom:1px solid var(--line)"><td style="padding:6px">' + escapeHtml(m.email) + '</td><td style="padding:6px">' + escapeHtml(m.label) + '</td><td style="text-align:right;padding:6px">' + (m.similarity != null ? m.similarity.toFixed(4) : '-') + '</td></tr>';
      });
      html += '</tbody></table>';
      resultDiv.innerHTML = html;
    }
    setOutput(JSON.stringify(data, null, 2));
    fileInput.value = '';
  } catch (e) {
    resultDiv.innerHTML = '<div class="hint" style="color:var(--danger)">' + escapeHtml(e.message) + '</div>';
    showToast((currentLang === 'zh' ? '\u8bc6\u522b\u5931\u8d25: ' : 'Identify failed: ') + e.message, 'error');
  }
}
async function loadVoiceprints() {
  var container = document.getElementById('vpList');
  if (!container) return;
  try {
    var data = await api('/api/admin/voiceprints');
    var items = data.voiceprints || [];
    if (!items.length) {
      container.innerHTML = '<div class="hint">' + (currentLang === 'zh' ? '\u65e0\u5df2\u6ce8\u518c\u58f0\u7eb9' : 'No voiceprints enrolled') + '</div>';
      return;
    }
    var html = '<table style="width:100%;font-size:13px;border-collapse:collapse"><thead><tr style="border-bottom:2px solid var(--line)"><th style="text-align:left;padding:6px">' + (currentLang === 'zh' ? '\u90ae\u7bb1' : 'Email') + '</th><th style="text-align:left;padding:6px">' + (currentLang === 'zh' ? '\u6807\u7b7e' : 'Label') + '</th><th style="text-align:left;padding:6px">' + (currentLang === 'zh' ? '\u7ef4\u5ea6' : 'Dim') + '</th><th style="text-align:left;padding:6px">' + (currentLang === 'zh' ? '\u521b\u5efa\u65f6\u95f4' : 'Created') + '</th><th style="text-align:right;padding:6px">' + (currentLang === 'zh' ? '\u64cd\u4f5c' : 'Actions') + '</th></tr></thead><tbody>';
    items.forEach(function(vp) {
      html += '<tr style="border-bottom:1px solid var(--line)"><td style="padding:6px">' + escapeHtml(vp.email) + '</td><td style="padding:6px">' + escapeHtml(vp.label) + '</td><td style="padding:6px">' + (vp.dim || '-') + '</td><td style="padding:6px">' + escapeHtml(vp.created_at) + '</td><td style="text-align:right;padding:6px"><button class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px;color:var(--danger)" onclick="deleteVoiceprint(\'' + escapeHtml(vp.id).replace(/'/g, "\\'") + '\')">' + (currentLang === 'zh' ? '\u5220\u9664' : 'Delete') + '</button></td></tr>';
    });
    html += '</tbody></table>';
    container.innerHTML = html;
  } catch (e) {
    container.innerHTML = '<div class="hint" style="color:var(--danger)">' + escapeHtml(e.message) + '</div>';
  }
}
async function deleteVoiceprint(id) {
  if (!confirm(currentLang === 'zh' ? '\u786e\u5b9a\u5220\u9664\u6b64\u58f0\u7eb9\u5417\uff1f' : 'Delete this voiceprint?')) return;
  try {
    await api('/api/admin/voiceprints?id=' + encodeURIComponent(id), { method: 'DELETE' });
    showToast(currentLang === 'zh' ? '\u58f0\u7eb9\u5df2\u5220\u9664' : 'Voiceprint deleted', 'success');
    loadVoiceprints();
  } catch (e) {
    showToast((currentLang === 'zh' ? '\u5220\u9664\u58f0\u7eb9\u5931\u8d25: ' : 'Delete voiceprint failed: ') + e.message, 'error');
  }
}
