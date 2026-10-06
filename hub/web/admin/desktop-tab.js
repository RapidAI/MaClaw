(function (global) {
  'use strict';

  var defaults = { image: 'maclaw-gui:1', memory: '2500m', cpus: '1.5', shm: '512m' };
  var editingId = '';

  var copy = {
    zh: {
      servers: 'Docker \u670d\u52a1',
      hint: 'Bot \u9700\u8981\u4e91\u684c\u9762\u65f6\uff0cHub \u6309\u7528\u6237\u3001\u90e8\u95e8\u6216\u5168\u5c40\u9009\u62e9\u8fd9\u91cc\u7684 Docker \u670d\u52a1\u3002',
      name: '\u540d\u79f0',
      url: '\u670d\u52a1\u5730\u5740',
      token: '\u8bbf\u95ee\u4ee4\u724c',
      tokenHint: '\u6dfb\u52a0\u670d\u52a1\u65f6\u5fc5\u586b\u3002',
      tokenKeep: '\u4fee\u6539\u65f6\u7559\u7a7a\u5219\u4e0d\u6539\u5df2\u4fdd\u5b58\u7684\u4ee4\u724c\u3002',
      tokenSet: '\u4ee4\u724c\u5df2\u4fdd\u5b58',
      tokenMissing: '\u5c1a\u672a\u4fdd\u5b58\u4ee4\u724c',
      image: '\u955c\u50cf',
      memory: '\u5185\u5b58',
      cpus: 'CPU',
      shm: '\u5171\u4eab\u5185\u5b58',
      add: '\u6dfb\u52a0\u670d\u52a1',
      saveEdit: '\u4fdd\u5b58\u4fee\u6539',
      cancel: '\u53d6\u6d88',
      edit: '\u4fee\u6539',
      assignments: '\u5206\u914d',
      assignHint: '\u7528\u6237\u4f18\u5148\uff0c\u5176\u6b21\u662f\u6240\u5728\u90e8\u95e8\uff08\u542b\u4e0a\u7ea7\uff09\uff0c\u6700\u540e\u662f\u5168\u5c40\u3002',
      scope: '\u8303\u56f4',
      global: '\u5168\u5c40\u7528\u6237',
      department: '\u90e8\u95e8',
      user: '\u7528\u6237',
      target: '\u90e8\u95e8\u6216\u7528\u6237',
      targetPick: '\u8bf7\u9009\u62e9',
      targetEmpty: '\u7ec4\u7ec7\u673a\u6784\u52a0\u8f7d\u540e\u53ef\u9009',
      server: 'Docker \u670d\u52a1',
      assign: '\u4fdd\u5b58\u5206\u914d',
      desktop: '\u68c0\u67e5\u684c\u9762',
      desktopHint: '\u6309\u8fd9\u4e2a\u7528\u6237\u7684\u5206\u914d\u62c9\u8d77\u6216\u505c\u6b62\u684c\u9762\u3002',
      userId: '\u7528\u6237',
      start: '\u521b\u5efa\u684c\u9762',
      stop: '\u505c\u6b62\u684c\u9762',
      remove: '\u5220\u9664',
      empty: '\u8fd8\u6ca1\u6709 Docker \u670d\u52a1\u3002',
      saved: '\u5df2\u4fdd\u5b58',
      started: '\u5df2\u8bf7\u6c42\u521b\u5efa\u684c\u9762',
      stopped: '\u5df2\u8bf7\u6c42\u505c\u6b62\u684c\u9762',
      working: '\u6b63\u5728\u8bf7\u6c42...',
      statusRunning: '\u8fd0\u884c\u4e2d',
      statusStopped: '\u5df2\u505c\u6b62',
      confirmDelete: '\u5220\u9664\u8fd9\u4e2a Docker \u670d\u52a1\uff1f',
      confirmUnassign: '\u53d6\u6d88\u8fd9\u4e2a\u5206\u914d\uff1f',
      needServer: '\u8bf7\u5148\u6dfb\u52a0 Docker \u670d\u52a1',
      needFields: '\u8bf7\u586b\u5199\u540d\u79f0\u548c\u670d\u52a1\u5730\u5740',
      badUrl: '\u670d\u52a1\u5730\u5740\u9700\u8981\u4ee5 http:// \u6216 https:// \u5f00\u5934',
      badResources: '\u8bf7\u68c0\u67e5\u955c\u50cf\u3001\u5185\u5b58\u3001CPU \u548c\u5171\u4eab\u5185\u5b58',
      assignedDelete: '\u5148\u53d6\u6d88\u5206\u914d\uff0c\u518d\u5220\u9664\u8fd9\u4e2a\u670d\u52a1',
      alreadyAssigned: '\u8fd9\u4e2a\u8303\u56f4\u5df2\u7ecf\u5206\u914d\u8fc7\u4e86',
      notAssigned: '\u8fd9\u4e2a\u7528\u6237\u8fd8\u6ca1\u6709\u5206\u914d\u5230 Docker \u670d\u52a1',
      failed: '\u64cd\u4f5c\u5931\u8d25'
    },
    en: {
      servers: 'Docker services',
      hint: 'When a bot needs a cloud desktop, Hub picks a Docker service here by user, department, or everyone.',
      name: 'Name',
      url: 'Service URL',
      token: 'Access token',
      tokenHint: 'Required when adding a service.',
      tokenKeep: 'Leave blank while editing to keep the saved token.',
      tokenSet: 'Token saved',
      tokenMissing: 'No token saved',
      image: 'Image',
      memory: 'Memory',
      cpus: 'CPU',
      shm: 'Shared memory',
      add: 'Add service',
      saveEdit: 'Save changes',
      cancel: 'Cancel',
      edit: 'Edit',
      assignments: 'Assignments',
      assignHint: 'A user match wins, then that user\'s department chain, then everyone.',
      scope: 'Scope',
      global: 'Everyone',
      department: 'Department',
      user: 'User',
      target: 'Department or user',
      targetPick: 'Choose',
      targetEmpty: 'Available after the organization loads',
      server: 'Docker service',
      assign: 'Save assignment',
      desktop: 'Check a desktop',
      desktopHint: 'Starts or stops a desktop using the service assigned to this user.',
      userId: 'User',
      start: 'Create desktop',
      stop: 'Stop desktop',
      remove: 'Delete',
      empty: 'No Docker services yet.',
      saved: 'Saved',
      started: 'Create request sent',
      stopped: 'Stop request sent',
      working: 'Requesting...',
      statusRunning: 'Running',
      statusStopped: 'Stopped',
      confirmDelete: 'Delete this Docker service?',
      confirmUnassign: 'Remove this assignment?',
      needServer: 'Add a Docker service first',
      needFields: 'Enter a name and service URL',
      badUrl: 'The service URL must start with http:// or https://',
      badResources: 'Check the image, memory, CPU, and shared memory',
      assignedDelete: 'Remove its assignments before deleting this service',
      alreadyAssigned: 'This scope already has an assignment',
      notAssigned: 'No Docker service is assigned to this user',
      failed: 'Request failed'
    }
  };

  function text() { return copy[global.currentLang === 'en' ? 'en' : 'zh']; }
  function byID(id) { return document.getElementById(id); }
  function host() { return byID('botDesktopHost'); }
  function api(path, opts) {
    if (typeof global.api === 'function') return global.api(path, opts);
    return Promise.reject(new Error('api unavailable'));
  }
  function showToast(msg, kind) {
    if (typeof global.showToast === 'function') global.showToast(msg, kind);
  }
  function escapeHtml(value) {
    return String(value == null ? '' : value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
  function messageOf(err) {
    var raw = err && err.message ? err.message : '';
    if (raw.indexOf('server is still assigned') !== -1) return text().assignedDelete;
    if (raw.indexOf('assignment already exists') !== -1) return text().alreadyAssigned;
    if (raw.indexOf('no docker service is assigned') !== -1) return text().notAssigned;
    if (raw.indexOf('memory is invalid') !== -1 || raw.indexOf('image is invalid') !== -1 || raw.indexOf('cpus is invalid') !== -1 || raw.indexOf('shm_size is invalid') !== -1) return text().badResources;
    if (raw.indexOf('base_url is invalid') !== -1) return text().badUrl;
    return raw || text().failed;
  }
  function setLabel(id, value) { var el = byID(id); if (el) el.textContent = value; }
  function orgChoices() {
    if (typeof global.botOrgChoices === 'function') return global.botOrgChoices();
    return { ready: false, departments: [], users: [] };
  }

  function applyDesktopI18n() {
    var panel = host();
    if (!panel || panel.dataset.ready !== '1') return;
    var t = text();
    setLabel('desktopServersTitle', t.servers);
    setLabel('desktopHint', t.hint);
    setLabel('desktopNameLabel', t.name);
    setLabel('desktopUrlLabel', t.url);
    setLabel('desktopTokenLabel', t.token);
    setLabel('desktopTokenHint', editingId ? t.tokenKeep : t.tokenHint);
    setLabel('desktopImageLabel', t.image);
    setLabel('desktopMemoryLabel', t.memory);
    setLabel('desktopCpusLabel', t.cpus);
    setLabel('desktopShmLabel', t.shm);
    setLabel('desktopAdd', editingId ? t.saveEdit : t.add);
    setLabel('desktopCancel', t.cancel);
    setLabel('desktopAssignTitle', t.assignments);
    setLabel('desktopAssignHint', t.assignHint);
    setLabel('desktopScopeLabel', t.scope);
    setLabel('desktopTargetLabel', t.target);
    setLabel('desktopServerLabel', t.server);
    setLabel('desktopAssign', t.assign);
    setLabel('desktopRunTitle', t.desktop);
    setLabel('desktopRunHint', t.desktopHint);
    setLabel('desktopUserLabel', t.userId);
    setLabel('desktopStart', t.start);
    setLabel('desktopStop', t.stop);
    var scope = byID('desktopScope');
    if (scope && scope.options && scope.options.length === 3) {
      scope.options[0].textContent = t.global;
      scope.options[1].textContent = t.department;
      scope.options[2].textContent = t.user;
    }
    fillTargets();
    render(panel._view || { servers: [], assignments: [] });
  }

  function mount() {
    var panel = host();
    if (!panel || panel.dataset.ready === '1') return;
    panel.dataset.ready = '1';
    panel.innerHTML = ''
      + '<section><h3 id="desktopServersTitle"></h3><p id="desktopHint"></p>'
      + '<label id="desktopNameLabel" for="desktopName"></label><input id="desktopName" maxlength="80" autocomplete="off">'
      + '<label id="desktopUrlLabel" for="desktopUrl"></label><input id="desktopUrl" placeholder="http://docker-host:18081" autocomplete="off" spellcheck="false">'
      + '<label id="desktopTokenLabel" for="desktopToken"></label><input id="desktopToken" type="password" autocomplete="new-password">'
      + '<p id="desktopTokenHint"></p>'
      + '<div class="bot-fields">'
      + '<div><label id="desktopImageLabel" for="desktopImage"></label><input id="desktopImage" autocomplete="off"></div>'
      + '<div><label id="desktopMemoryLabel" for="desktopMemory"></label><input id="desktopMemory" autocomplete="off"></div>'
      + '<div><label id="desktopCpusLabel" for="desktopCpus"></label><input id="desktopCpus" autocomplete="off"></div>'
      + '<div><label id="desktopShmLabel" for="desktopShm"></label><input id="desktopShm" autocomplete="off"></div>'
      + '</div>'
      + '<div class="bot-actions"><button type="button" class="btn-primary" id="desktopAdd"></button>'
      + '<button type="button" class="btn-secondary" id="desktopCancel" hidden></button></div>'
      + '<div id="desktopServerList"></div></section>'
      + '<section><h3 id="desktopAssignTitle"></h3><p id="desktopAssignHint"></p>'
      + '<label id="desktopScopeLabel" for="desktopScope"></label><select id="desktopScope"><option value="global"></option><option value="department"></option><option value="user"></option></select>'
      + '<label id="desktopTargetLabel" for="desktopTarget"></label><select id="desktopTarget"></select>'
      + '<label id="desktopServerLabel" for="desktopServer"></label><select id="desktopServer"></select>'
      + '<div class="bot-actions"><button type="button" class="btn-primary" id="desktopAssign"></button></div><div id="desktopAssignList"></div></section>'
      + '<section><h3 id="desktopRunTitle"></h3><p id="desktopRunHint"></p>'
      + '<label id="desktopUserLabel" for="desktopUser"></label><select id="desktopUser"></select>'
      + '<div class="bot-actions"><button type="button" class="btn-secondary" id="desktopStart"></button><button type="button" class="btn-secondary" id="desktopStop"></button></div><p id="desktopRunStatus"></p></section>';
    byID('desktopImage').value = defaults.image;
    byID('desktopMemory').value = defaults.memory;
    byID('desktopCpus').value = defaults.cpus;
    byID('desktopShm').value = defaults.shm;
    byID('desktopAdd').addEventListener('click', saveServer);
    byID('desktopCancel').addEventListener('click', function () { clearForm(); });
    byID('desktopAssign').addEventListener('click', addAssignment);
    byID('desktopScope').addEventListener('change', fillTargets);
    byID('desktopStart').addEventListener('click', function () { runDesktop(true); });
    byID('desktopStop').addEventListener('click', function () { runDesktop(false); });
    applyDesktopI18n();
  }

  function fillTargets() {
    var scope = byID('desktopScope');
    var target = byID('desktopTarget');
    var targetLabel = byID('desktopTargetLabel');
    if (!scope || !target) return;
    fillUserSelect();
    var everyone = scope.value === 'global';
    target.disabled = everyone;
    if (targetLabel) targetLabel.hidden = everyone;
    target.hidden = everyone;
    if (everyone) target.innerHTML = '';
    else {
      var previous = target.value;
      var list = scope.value === 'user' ? orgChoices().users : orgChoices().departments;
      var placeholder = list.length ? text().targetPick : text().targetEmpty;
      target.innerHTML = '<option value="">' + escapeHtml(placeholder) + '</option>' + list.map(function (item) {
        return '<option value="' + escapeHtml(item.id) + '">' + escapeHtml(item.label) + '</option>';
      }).join('');
      if (previous) target.value = previous;
    }
    var panel = host();
    if (panel && panel._view) renderAssignments(panel._view);
  }

  function validServiceUrl(value) {
    var raw = String(value || '').trim();
    var parsed;
    try { parsed = new URL(raw); } catch (err) { return false; }
    return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && !!parsed.host && !parsed.username && !parsed.password;
  }

  function fillUserSelect() {
    var select = byID('desktopUser');
    if (!select) return;
    var previous = select.value;
    var users = orgChoices().users;
    var placeholder = users.length ? text().targetPick : text().targetEmpty;
    select.innerHTML = '<option value="">' + escapeHtml(placeholder) + '</option>' + users.map(function (item) {
      return '<option value="' + escapeHtml(item.id) + '">' + escapeHtml(item.label) + '</option>';
    }).join('');
    if (previous) select.value = previous;
  }

  function clearForm() {
    editingId = '';
    var name = byID('desktopName');
    var url = byID('desktopUrl');
    var token = byID('desktopToken');
    if (name) name.value = '';
    if (url) url.value = '';
    if (token) token.value = '';
    if (byID('desktopImage')) byID('desktopImage').value = defaults.image;
    if (byID('desktopMemory')) byID('desktopMemory').value = defaults.memory;
    if (byID('desktopCpus')) byID('desktopCpus').value = defaults.cpus;
    if (byID('desktopShm')) byID('desktopShm').value = defaults.shm;
    setLabel('desktopAdd', text().add);
    setLabel('desktopTokenHint', text().tokenHint);
    var cancel = byID('desktopCancel');
    if (cancel) cancel.hidden = true;
  }

  function beginEdit(id) {
    var servers = ((host() && host()._view) || {}).servers || [];
    var server = null;
    servers.forEach(function (item) { if (item.id === id) server = item; });
    if (!server) return;
    editingId = id;
    byID('desktopName').value = server.name || '';
    byID('desktopUrl').value = server.base_url || '';
    byID('desktopToken').value = '';
    byID('desktopImage').value = server.image || defaults.image;
    byID('desktopMemory').value = server.memory || defaults.memory;
    byID('desktopCpus').value = server.cpus || defaults.cpus;
    byID('desktopShm').value = server.shm_size || defaults.shm;
    setLabel('desktopAdd', text().saveEdit);
    setLabel('desktopTokenHint', text().tokenKeep);
    byID('desktopCancel').hidden = false;
    var name = byID('desktopName');
    if (name) {
      name.focus();
      if (name.scrollIntoView) name.scrollIntoView({ block: 'center' });
    }
  }

  function load() {
    mount();
    return api('/api/admin/desktop-services').then(function (view) {
      render(view || { servers: [], assignments: [] });
    }).catch(function (err) { showToast(messageOf(err), 'error'); });
  }

  function scopeLabel(scope) {
    if (scope === 'department') return text().department;
    if (scope === 'user') return text().user;
    return text().global;
  }

  function targetLabel(scope, id) {
    if (!id) return '';
    var list = scope === 'user' ? orgChoices().users : orgChoices().departments;
    for (var i = 0; i < list.length; i += 1) {
      if (list[i].id === id) return list[i].label;
    }
    return id;
  }

  function render(view) {
    var panel = host();
    if (panel) panel._view = view;
    var servers = view.servers || [];
    var list = byID('desktopServerList');
    if (list) {
      var assignedIds = {};
      (view.assignments || []).forEach(function (item) { if (item && item.server_id) assignedIds[item.server_id] = true; });
      list.innerHTML = servers.length ? servers.map(function (server) {
        var token = server.token_set ? text().tokenSet : text().tokenMissing;
        var inUse = !!assignedIds[server.id];
        return '<div class="bot-row"><div><strong>' + escapeHtml(server.name) + '</strong><div>' + escapeHtml(server.base_url) + '</div>'
          + '<small>' + escapeHtml(server.image) + ' / ' + escapeHtml(server.memory) + ' / ' + escapeHtml(server.cpus) + ' CPU / shm ' + escapeHtml(server.shm_size) + ' / ' + escapeHtml(token) + '</small></div>'
          + '<div class="bot-actions"><button type="button" class="btn-secondary" data-desktop-edit="' + escapeHtml(server.id) + '">' + escapeHtml(text().edit) + '</button>'
          + '<button type="button" class="btn-danger" data-desktop-delete="' + escapeHtml(server.id) + '"'
          + (inUse ? ' disabled title="' + escapeHtml(text().assignedDelete) + '"' : '') + '>' + escapeHtml(text().remove) + '</button></div></div>';
      }).join('') : '<p>' + escapeHtml(text().empty) + '</p>';
      list.querySelectorAll('[data-desktop-edit]').forEach(function (button) {
        button.addEventListener('click', function () { beginEdit(button.getAttribute('data-desktop-edit')); });
      });
      list.querySelectorAll('[data-desktop-delete]').forEach(function (button) {
        button.addEventListener('click', function () { removeServer(button.getAttribute('data-desktop-delete')); });
      });
    }
    var select = byID('desktopServer');
    if (select) {
      var selected = select.value;
      select.innerHTML = servers.map(function (server) {
        return '<option value="' + escapeHtml(server.id) + '">' + escapeHtml(server.name) + '</option>';
      }).join('');
      if (selected) select.value = selected;
    }
    renderAssignments(view);
  }

  function renderAssignments(view) {
    var assignments = byID('desktopAssignList');
    if (!assignments || !view) return;
    var names = {};
    (view.servers || []).forEach(function (server) { names[server.id] = server.name; });
    assignments.innerHTML = (view.assignments || []).map(function (item) {
      var who = scopeLabel(item.scope);
      var named = targetLabel(item.scope, item.target_id);
      if (named) who += ' ' + named;
      return '<div class="bot-row"><span>' + escapeHtml(who) + ' \u2192 ' + escapeHtml(names[item.server_id] || item.server_id) + '</span>'
        + '<button type="button" class="btn-danger" data-assign-delete="' + escapeHtml(item.id) + '">' + escapeHtml(text().remove) + '</button></div>';
    }).join('');
    assignments.querySelectorAll('[data-assign-delete]').forEach(function (button) {
      button.addEventListener('click', function () { removeAssignment(button.getAttribute('data-assign-delete')); });
    });
  }

  function formBody() {
    return {
      name: byID('desktopName').value.trim(),
      base_url: byID('desktopUrl').value.trim(),
      image: byID('desktopImage').value.trim(),
      memory: byID('desktopMemory').value.trim(),
      cpus: byID('desktopCpus').value.trim(),
      shm_size: byID('desktopShm').value.trim()
    };
  }

  function saveServer() {
    var panel = host();
    if (panel && panel._saving) return;
    var body = formBody();
    if (!body.name || !body.base_url) {
      showToast(text().needFields, 'error');
      return;
    }
    if (!validServiceUrl(body.base_url)) {
      showToast(text().badUrl, 'error');
      return;
    }
    if (!validResources(body)) {
      showToast(text().badResources, 'error');
      return;
    }
    var token = byID('desktopToken').value;
    if (!editingId && !String(token).trim()) {
      showToast(text().tokenHint, 'error');
      return;
    }
    var path = '/api/admin/desktop-services';
    var method = 'POST';
    if (editingId) {
      path += '/' + encodeURIComponent(editingId);
      method = 'PATCH';
      if (token !== '') body.access_token = token;
    } else {
      body.access_token = token;
    }
    if (panel) panel._saving = true;
    var button = byID('desktopAdd');
    if (button) button.disabled = true;
    return api(path, { method: method, body: JSON.stringify(body) }).then(function () {
      clearForm();
      showToast(text().saved, 'success');
      return load().catch(function (err) { showToast(messageOf(err), 'error'); });
    }).catch(function (err) { showToast(messageOf(err), 'error'); }).then(function () {
      if (panel) panel._saving = false;
      if (button) button.disabled = false;
    });
  }

  function removeServer(id) {
    if (!global.confirm(text().confirmDelete)) return;
    if (editingId === id) clearForm();
    return api('/api/admin/desktop-services/' + encodeURIComponent(id), { method: 'DELETE' }).then(load).catch(function (err) { showToast(messageOf(err), 'error'); });
  }

  function assignmentExists(scope, target) {
    var view = (host() && host()._view) || {};
    var want = String(target || '');
    return (view.assignments || []).some(function (item) {
      return item && item.scope === scope && String(item.target_id || '') === want;
    });
  }

  function validResources(body) {
    if (body.image && !/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,200}$/.test(body.image)) return false;
    if (body.memory && !/^[1-9][0-9]{0,6}([kmg]i?b?)?$/i.test(body.memory)) return false;
    if (body.shm_size && !/^[1-9][0-9]{0,6}([kmg]i?b?)?$/i.test(body.shm_size)) return false;
    if (body.cpus) {
      var cpus = Number(body.cpus);
      if (!isFinite(cpus) || cpus <= 0 || cpus > 64) return false;
    }
    return true;
  }

  function addAssignment() {
    var panel = host();
    if (panel && panel._assigning) return;
    var serverID = byID('desktopServer').value;
    if (!serverID) {
      showToast(text().needServer, 'error');
      return;
    }
    var scope = byID('desktopScope').value;
    var target = scope === 'global' ? '' : byID('desktopTarget').value;
    if (scope !== 'global' && !target) {
      showToast(text().targetPick, 'error');
      return;
    }
    if (assignmentExists(scope, target)) {
      showToast(text().alreadyAssigned, 'error');
      return;
    }
    var body = { scope: scope, server_id: serverID, target_id: target };
    if (panel) panel._assigning = true;
    var button = byID('desktopAssign');
    if (button) button.disabled = true;
    return api('/api/admin/desktop-services/assignments', { method: 'POST', body: JSON.stringify(body) }).then(function () {
      fillTargets();
      showToast(text().saved, 'success');
      return load().catch(function (err) { showToast(messageOf(err), 'error'); });
    }).catch(function (err) { showToast(messageOf(err), 'error'); }).then(function () {
      if (panel) panel._assigning = false;
      if (button) button.disabled = false;
    });
  }

  function removeAssignment(id) {
    if (!global.confirm(text().confirmUnassign)) return;
    return api('/api/admin/desktop-services/assignments/' + encodeURIComponent(id), { method: 'DELETE' }).then(load).catch(function (err) { showToast(messageOf(err), 'error'); });
  }

  function setRunStatus(msg) {
    var status = byID('desktopRunStatus');
    if (status) status.textContent = msg || '';
  }

  function statusLabel(status) {
    if (status === 'running') return text().statusRunning;
    if (status === 'stopped') return text().statusStopped;
    return status || '';
  }

  function runDesktop(create) {
    var panel = host();
    if (panel && panel._running) return;
    var userID = (byID('desktopUser').value || '').trim();
    if (!userID) {
      showToast(text().targetPick, 'error');
      return;
    }
    if (panel) panel._running = true;
    var start = byID('desktopStart');
    var stop = byID('desktopStop');
    if (start) start.disabled = true;
    if (stop) stop.disabled = true;
    setRunStatus(text().working);
    var path = create ? '/api/admin/desktop-services/desktops' : '/api/admin/desktop-services/desktops/stop';
    return api(path, { method: 'POST', body: JSON.stringify({ user_id: userID }) }).then(function (result) {
      var service = result && (result.server_name || result.server_id) || '';
      var detail = [service, statusLabel(result && result.status)].filter(Boolean).join(' ');
      var msg = (create ? text().started : text().stopped) + (detail ? ' ' + detail : '');
      setRunStatus(msg);
      showToast(msg, 'success');
    }).catch(function (err) {
      var msg = messageOf(err);
      setRunStatus(msg);
      showToast(msg, 'error');
    }).then(function () {
      if (panel) panel._running = false;
      if (start) start.disabled = false;
      if (stop) stop.disabled = false;
    });
  }

  global.mountBotDesktop = mount;
  global.loadBotDesktop = load;
  global.refreshBotDesktopTargets = fillTargets;
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(applyDesktopI18n);
  }
})(window);
