/*
 * Machines admin module.
 * ASCII only.
 */
(function(global) {
  function formatPlatformLabel(platform) {
    if (!platform) return '';
    var p = String(platform).toLowerCase();
    if (p === 'windows' || p === 'win32' || p === 'win') return 'Windows';
    if (p === 'darwin' || p === 'mac' || p === 'macos') return 'macOS';
    if (p === 'linux') return 'Linux';
    return platform;
  }

  function ensureMachinePagingState() {
    if (!Array.isArray(global.machineItemsCache)) global.machineItemsCache = [];
    if (!Number.isFinite(global.machinePage) || global.machinePage < 1) global.machinePage = 1;
    if (!Number.isFinite(global.machinePageSize) || global.machinePageSize < 1) global.machinePageSize = 10;
  }

  function textHint(message) {
    return '<div class="hint">' + escapeHtml(message || '') + '</div>';
  }

  function errorHint(message) {
    return '<div class="hint" style="color:var(--danger)">' + escapeHtml(message || '') + '</div>';
  }

  function renderHeader() {
    return '<div class="row header" style="grid-template-columns:1.08fr .92fr 1.02fr .84fr .62fr .78fr .98fr"><div>' + tr('machine') + '</div><div>' + ml('boundEmail') + '</div><div>' + ml('runtime') + '</div><div>' + ml('heartbeat') + '</div><div>' + tr('status') + '</div><div>' + tr('lastSeen') + '</div><div>' + ml('actions') + '</div></div>';
  }

  function actionButton(label, className, onclick) {
    return '<button class="' + className + '" style="height:27px;font-size:11px;padding:0 8px" onclick="' + onclick + '">' + label + '</button>';
  }

  function machineNameExpr(value) {
    return JSON.stringify(String(value || ''));
  }

  global.renderMachineList = function renderMachineList(items) {
    ensureMachinePagingState();
    global.machineItemsCache = Array.isArray(items) ? items : [];
    const countHero = document.getElementById('machineCountHero');
    const root = document.getElementById('machines');
    const pager = document.getElementById('machinesPager');
    const pagerMeta = document.getElementById('machinesPagerMeta');
    const prevButton = document.getElementById('machinesPrevButton');
    const nextButton = document.getElementById('machinesNextButton');
    if (countHero) countHero.textContent = String(global.machineItemsCache.length);
    if (!root || !pager || !pagerMeta || !prevButton || !nextButton) return;
    const header = renderHeader();
    if (!global.machineItemsCache.length) {
      pager.classList.add('hidden');
      root.innerHTML = header + textHint(tr('noMachinesLoaded'));
      return;
    }
    const totalPages = Math.max(1, Math.ceil(global.machineItemsCache.length / global.machinePageSize));
    if (global.machinePage > totalPages) global.machinePage = totalPages;
    if (global.machinePage < 1) global.machinePage = 1;
    const startIndex = (global.machinePage - 1) * global.machinePageSize;
    const pageItems = global.machineItemsCache.slice(startIndex, startIndex + global.machinePageSize);
    root.innerHTML = header + pageItems.map(function(item) {
      const isOnline = !!item.online;
      const state = isOnline ? tr('online') : tr('offline');
      const email = item.user_email || tr('na');
      const runtime = [
        formatPlatformLabel(item.platform),
        item.hostname ? (ml('host') + ': ' + item.hostname) : '',
        item.arch ? (ml('arch') + ': ' + item.arch) : '',
        item.app_version ? (ml('version') + ': ' + item.app_version) : ''
      ].filter(Boolean);
      const heartbeat = [
        item.heartbeat_interval_sec ? (ml('interval') + ': ' + item.heartbeat_interval_sec + 's') : '',
        typeof item.active_sessions === 'number' ? (ml('activeSessions') + ': ' + item.active_sessions) : ''
      ].filter(Boolean);
      const displayName = escapeHtml(item.alias || item.name || item.machine_id || tr('machine'));
      const aliasLine = item.alias ? '<div class="item-meta">' + escapeHtml(item.name || '') + '</div>' : '';
      const machineID = machineNameExpr(item.machine_id || '');
      const aliasExpr = machineNameExpr(item.alias || '');
      const sessionNameExpr = machineNameExpr(item.alias || item.name || item.machine_id || tr('machine'));
      const renameBtn = actionButton(ml('renameMachine'), 'btn-secondary', 'renameMachine(' + machineID + ',' + aliasExpr + ')');
      const deleteBtn = actionButton(ml('deleteMachine'), 'btn-danger', 'deleteMachine(' + machineID + ')');
      const sessionsBtn = isOnline ? actionButton(ml('viewSessions'), 'btn-secondary', 'viewMachineSessions(' + machineID + ',' + machineNameExpr(item.user_id || '') + ',' + sessionNameExpr + ')') : '';
      return '<div class="row" style="grid-template-columns:1.08fr .92fr 1.02fr .84fr .62fr .78fr .98fr"><div style="min-width:0"><strong style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis;display:block">' + displayName + '</strong>' + aliasLine + '<div class="item-meta mono" style="margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.machine_id || '') + '</div></div><div style="min-width:0"><div style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(email) + '</div><div class="item-meta mono" style="margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.user_id || '') + '</div></div><div>' + runtime.map(function(line) { return '<div class="item-meta" style="margin-top:2px">' + escapeHtml(line) + '</div>'; }).join('') + '</div><div>' + (heartbeat.length ? heartbeat.map(function(line) { return '<div class="item-meta" style="margin-top:2px">' + escapeHtml(line) + '</div>'; }).join('') : ('<div class="item-meta">' + tr('na') + '</div>')) + '</div><div><span class="badge ' + (isOnline ? 'ok' : 'warn') + '">' + escapeHtml(state) + '</span></div><div class="item-meta">' + escapeHtml(item.last_seen_at || tr('na')) + '</div><div style="display:flex;gap:4px;flex-wrap:wrap;justify-content:flex-end">' + renameBtn + sessionsBtn + deleteBtn + '</div></div>';
    }).join('');
    const start = startIndex + 1;
    const end = startIndex + pageItems.length;
    pagerMeta.textContent = totalPages > 1
      ? ml('pageSummary').replace('{start}', String(start)).replace('{end}', String(end)).replace('{total}', String(global.machineItemsCache.length))
      : ml('pageSingle').replace('{total}', String(global.machineItemsCache.length));
    prevButton.disabled = global.machinePage <= 1;
    nextButton.disabled = global.machinePage >= totalPages;
    pager.classList.toggle('hidden', global.machineItemsCache.length <= global.machinePageSize);
  };

  global.changeMachinesPage = function changeMachinesPage(step) {
    ensureMachinePagingState();
    const totalPages = Math.max(1, Math.ceil(global.machineItemsCache.length / global.machinePageSize));
    global.machinePage = Math.min(totalPages, Math.max(1, global.machinePage + step));
    global.renderMachineList(global.machineItemsCache);
  };

  global.closeSessionModal = function closeSessionModal() {
    const overlay = document.getElementById('sessionModalOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.viewMachineSessions = async function viewMachineSessions(machineId, userId, machineName) {
    const overlay = document.getElementById('sessionModalOverlay');
    const title = document.getElementById('sessionModalTitle');
    const content = document.getElementById('sessionModalContent');
    if (!overlay || !title || !content) return;
    title.textContent = ml('viewSessionsTitle') + ' - ' + machineName;
    content.innerHTML = textHint(tr('checking'));
    overlay.classList.add('show');
    try {
      const data = await api('/api/admin/debug/sessions?machine_id=' + encodeURIComponent(machineId) + '&user_id=' + encodeURIComponent(userId));
      const allSessions = data.sessions || [];
      const terminalSet = new Set(['exited', 'stopped', 'finished', 'failed', 'killed', 'closed', 'done', 'error', 'completed', 'terminated']);
      const sessions = allSessions.filter(function(session) {
        const status = String((session.Summary || {}).status || '').toLowerCase();
        return !terminalSet.has(status);
      });
      if (!sessions.length) {
        content.innerHTML = textHint(ml('noSessions'));
        return;
      }
      content.innerHTML = sessions.map(function(session) {
        const sum = session.Summary || {};
        const updatedAt = sum.updated_at ? new Date(sum.updated_at * 1000).toLocaleString() : tr('na');
        return '<div class="session-item"><div class="session-field"><span class="session-label">' + ml('sessionTitle') + '</span><span class="session-value">' + escapeHtml(sum.title || tr('na')) + '</span></div><div class="session-field"><span class="session-label">' + ml('sessionTask') + '</span><span class="session-value">' + escapeHtml(sum.current_task || tr('na')) + '</span></div><div class="session-field"><span class="session-label">' + ml('sessionTool') + '</span><span class="session-value">' + escapeHtml(sum.tool || tr('na')) + '</span></div><div class="session-field"><span class="session-label">' + ml('sessionStatus') + '</span><span class="session-value"><span class="badge ' + (sum.status === 'running' ? 'ok' : 'info') + '">' + escapeHtml(sum.status || tr('unknown')) + '</span></span></div><div class="session-field"><span class="session-label">' + ml('sessionLastHeartbeat') + '</span><span class="session-value">' + escapeHtml(updatedAt) + '</span></div></div>';
      }).join('');
    } catch (err) {
      content.innerHTML = errorHint(ml('sessionsLoadFailed').replace('{error}', err.message));
    }
  };

  global.loadMachines = async function loadMachines() {
    try {
      ensureMachinePagingState();
      global.machinePage = 1;
      const emailScope = document.getElementById('machineEmailSearch').value.trim();
      const showAll = document.getElementById('machinesShowAll') && document.getElementById('machinesShowAll').checked;
      let path = '/api/admin/debug/machines';
      if (emailScope) {
        path = '/api/admin/debug/machines?email=' + encodeURIComponent(emailScope);
      } else if (showAll) {
        path = '/api/admin/debug/machines?all=1';
      }
      const data = await api(path);
      global.renderMachineList(data.machines || []);
    } catch (err) {
      setOutput(tr('machinesLoadFailed', { error: err.message }));
    }
  };

  global.searchMachinesByEmail = async function searchMachinesByEmail() {
    const email = document.getElementById('machineEmailSearch').value.trim();
    if (!email) {
      const msg = ml('emailRequired');
      setOutput(msg);
      showToast(msg, 'error');
      return;
    }
    await global.loadMachines();
  };

  global.deleteAllMachinesByEmail = async function deleteAllMachinesByEmail() {
    const email = document.getElementById('machineEmailSearch').value.trim();
    if (!email) {
      const msg = ml('emailRequired');
      setOutput(msg);
      showToast(msg, 'error');
      return;
    }
    if (!confirm(ml('emailDeleteAllConfirm'))) return;
    try {
      const data = await api('/api/admin/machines/by-email?email=' + encodeURIComponent(email), { method: 'DELETE' });
      const msg = ml('emailDeleteAllSuccess').replace('{count}', String(data.deleted || 0));
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadMachines();
    } catch (err) {
      const msg = ml('emailDeleteAllFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.forceDeleteAllMachinesByEmail = async function forceDeleteAllMachinesByEmail() {
    const email = document.getElementById('machineEmailSearch').value.trim();
    if (!email) {
      const msg = ml('emailRequired');
      setOutput(msg);
      showToast(msg, 'error');
      return;
    }
    if (!confirm(ml('emailForceDeleteConfirm'))) return;
    try {
      const data = await api('/api/admin/machines/force-by-email?email=' + encodeURIComponent(email), { method: 'DELETE' });
      const msg = ml('emailForceDeleteSuccess').replace('{count}', String(data.deleted || 0));
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadMachines();
    } catch (err) {
      const msg = ml('emailForceDeleteFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.deleteMachine = async function deleteMachine(machineID) {
    if (!confirm(ml('deleteConfirm'))) return;
    try {
      await api('/api/admin/machines?machine_id=' + encodeURIComponent(machineID), { method: 'DELETE' });
      const msg = ml('deleteSuccess');
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadMachines();
    } catch (err) {
      const msg = ml('deleteFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.renameMachine = async function renameMachine(machineID, currentAlias) {
    const alias = prompt(ml('renamePrompt'), currentAlias || '');
    if (alias === null) return;
    try {
      await api('/api/admin/machines/rename', { method: 'POST', body: JSON.stringify({ machine_id: machineID, alias: alias.trim() }) });
      const msg = ml('renameSuccess');
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadMachines();
    } catch (err) {
      const msg = ml('renameFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.clearOfflineMachines = async function clearOfflineMachines() {
    if (!confirm(ml('clearOfflineConfirm'))) return;
    try {
      const data = await api('/api/admin/machines/clear-offline', { method: 'POST' });
      const msg = ml('clearSuccess').replace('{count}', String(data.cleared || 0));
      setOutput(msg);
      showToast(msg, 'success');
      await global.loadMachines();
    } catch (err) {
      const msg = ml('clearFailed').replace('{error}', err.message);
      setOutput(msg);
      showToast(msg, 'error');
    }
  };
})(window);
