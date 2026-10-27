(function () {
  const PROBLEM_REPORTS_PAGE_SIZE = 30;
  const ATTACHMENT_MANIFEST_CONCURRENCY = 3;
  let problemReportsPage = 1;
  let problemReportsLoadSeq = 0;
  let attachmentManifestActive = 0;
  const attachmentManifestQueue = [];
  function installProblemReportsTab() {
    const contentGroup = document.querySelector('[data-nav-group="ops"]');
    const main = document.querySelector('main.main');
    if (document.getElementById('tab-problemreports')) return true;
    if (!contentGroup || !main) return false;
    const button = document.createElement('button');
    button.dataset.tab = 'problemreports';
    const problemIcon = (typeof TAB_ICONS === 'object' && TAB_ICONS.problemreports) ? TAB_ICONS.problemreports : '<svg viewBox="0 0 24 24"><path d="M12 3 2 21h20L12 3z"></path><path d="M12 9v4"></path><path d="M12 17h.01"></path></svg>';
    button.innerHTML = '<span class="nav-icon" aria-hidden="true">' + problemIcon + '</span><span data-i18n="navProblemReports"></span><small data-i18n="navProblemReportsDesc"></small>';
    button.onclick = function () { window.openTab('problemreports'); };
    contentGroup.insertBefore(button, contentGroup.querySelector('[data-tab="failurelogs"]'));
    if (typeof applyI18n === 'function') applyI18n();
    const panel = document.createElement('section');
    panel.id = 'tab-problemreports'; panel.className = 'panel card';
    panel.innerHTML = '<div class="head"><div><h3>故障报告</h3><div class="desc">查看用户诊断包、联系信息及处理状态。</div></div><div class="actions"><select id="problemReportsStatus" onchange="changeProblemReportsStatus()"><option value="">全部状态</option><option value="pending">待处理</option><option value="fixed">已修复</option><option value="deferred">延期</option><option value="rejected">不予处理</option><option value="archived">已归档</option></select><button class="btn-ghost" onclick="loadProblemReports()">刷新</button></div></div><div id="problemReportsList" class="list"><div class="hint">暂无故障报告。</div></div><div id="problemReportsPager" class="pager pager-compact"><div id="problemReportsPageInfo" class="pager-meta" aria-live="polite"></div><div class="pager-actions"><button id="problemReportsPrev" class="btn-ghost" type="button" onclick="changeProblemReportsPage(-1)">Previous</button><button id="problemReportsNext" class="btn-ghost" type="button" onclick="changeProblemReportsPage(1)">Next</button></div></div>';
    main.appendChild(panel);
    const previousOpenTab = window.openTab;
    window.openTab = function (name) {
      if (name !== 'problemreports') return previousOpenTab(name);
      // Keep admin-core's active-tab key in sync (like the usermgmt wrap does),
      // otherwise a refresh can't restore this tab: admin-core's restoreTab()
      // runs before this module loads and never sees the panel.
      localStorage.setItem('maclawHubCenterActiveTab', name);
      localStorage.setItem('maclaw.admin.activeTab', name);
      document.querySelectorAll('.nav button[data-tab]').forEach(function (node) { node.classList.toggle('active', node.dataset.tab === name); });
      document.querySelectorAll('.panel').forEach(function (node) { node.classList.toggle('active', node.id === 'tab-' + name); });
      document.getElementById('pageTitle').textContent = tr('problemReportsTabTitle');
      document.getElementById('pageSubtitle').textContent = tr('problemReportsTabSubtitle');
      const pageIcon = document.getElementById('pageTabIcon');
      if (pageIcon) pageIcon.innerHTML = problemIcon;
      window.loadProblemReports();
    };
    return true;
  }
  window.installProblemReportsTab = installProblemReportsTab;
  function escapeValue(value) { return typeof escapeHtml === 'function' ? escapeHtml(String(value || '')) : String(value || ''); }
  function label(status) { return {pending:'Pending',fixed:'Fixed',deferred:'Deferred',rejected:'Not processed',archived:'Archived'}[status] || status; }
  function reporterContact(value) {
    const contact = String(value || '').trim();
    if (!contact) return 'Not available';
    return contact.toLowerCase().indexOf('phone:') === 0 ? contact.slice(6) : contact;
  }
  async function request(path, options) {
    if (typeof api === 'function') return api(path, options);
    const response = await fetch(path, Object.assign({headers:{'Content-Type':'application/json'}}, options || {}));
    if (!response.ok) throw new Error(await response.text());
    return response.status === 204 ? {} : response.json();
  }
  function problemReportErrorMessage(error) {
    const raw = String(error && error.message || error || '').trim();
    // Reverse proxies can return a full HTML 502 page.  It is neither useful
    // nor safe to dump that markup into an alert; retain the actionable status.
    if (/^<html[\s>]/i.test(raw) || /<title>\s*502\s+Bad Gateway/i.test(raw)) {
      return 'The original upload server is temporarily unavailable (502). The report and its attachments were not deleted; please try again shortly.';
    }
    return raw || 'Unable to delete the problem report.';
  }
  function renderProblemReportsPager(total, itemCount) {
    const pages = Math.max(1, Math.ceil(total / PROBLEM_REPORTS_PAGE_SIZE));
    const offset = (problemReportsPage - 1) * PROBLEM_REPORTS_PAGE_SIZE;
    const start = itemCount ? offset + 1 : 0;
    const end = offset + itemCount;
    const info = document.getElementById('problemReportsPageInfo');
    const prev = document.getElementById('problemReportsPrev');
    const next = document.getElementById('problemReportsNext');
    if (info) info.textContent = 'Showing ' + start + '-' + end + ' of ' + total;
    if (prev) prev.disabled = problemReportsPage <= 1;
    if (next) next.disabled = problemReportsPage >= pages;
  }
  window.changeProblemReportsStatus = function () {
    problemReportsPage = 1;
    window.loadProblemReports();
  };
  window.changeProblemReportsPage = function (delta) {
    problemReportsPage = Math.max(1, problemReportsPage + delta);
    window.loadProblemReports();
  };
  window.loadProblemReports = async function () {
    const root = document.getElementById('problemReportsList');
    if (!root) return;
    const seq = ++problemReportsLoadSeq;
    root.setAttribute('aria-busy', 'true');
    root.textContent = 'Loading...';
    const status = document.getElementById('problemReportsStatus')?.value || '';
    const params = new URLSearchParams({limit:String(PROBLEM_REPORTS_PAGE_SIZE), offset:String((problemReportsPage - 1) * PROBLEM_REPORTS_PAGE_SIZE)});
    if (status) params.set('status', status);
    try {
      const payload = await request('/api/v1/admin/problem-reports?' + params.toString());
      if (seq !== problemReportsLoadSeq) return;
      const items = payload.items || [];
      const total = Math.max(0, Number(payload.total) || 0);
      const pages = Math.max(1, Math.ceil(total / PROBLEM_REPORTS_PAGE_SIZE));
      if (!items.length && total > 0 && problemReportsPage > pages) {
        problemReportsPage = pages;
        window.loadProblemReports();
        return;
      }
      if (!items.length) {
        root.innerHTML = '<div class="hint">No problem reports.</div>';
        renderProblemReportsPager(total, 0);
        return;
      }
      root.innerHTML = items.map(function (item) {
        const attachments = ['diagnostics.zip'].concat(item.screenshot_paths || []).map(function (file) {
          return '<button type="button" class="btn-ghost compact-btn" onclick="downloadProblemReportAttachment(\'' + String(item.id).replace(/'/g, "\\'") + '\',\'' + String(file).replace(/'/g, "\\'") + '\')">' + escapeValue(file) + '</button>';
        }).join(' ');
        return '<article class="item"><div class="head"><div><div class="item-title">' + escapeValue(item.id) + ' <span class="badge ' + (item.status === 'fixed' ? 'ok' : item.status === 'pending' ? 'warn' : '') + '">' + escapeValue(label(item.status)) + '</span></div><div class="item-meta">' + escapeValue(item.created_at) + '</div></div></div>' +
          '<div class="grid2"><div><label>Reporter ID / contact (admin only)</label><div class="item-meta">' + escapeValue(item.reporter_user_id) + '<br>' + escapeValue(reporterContact(item.reporter_contact)) + '</div></div><div><label>运行环境</label><div class="item-meta">操作系统：' + escapeValue(item.os_version) + '<br>MaClaw GUI：' + escapeValue(item.gui_version || '未提供') + '</div></div></div>' +
          '<div class="stack-gap-sm"><label>Description</label><pre class="item-meta problem-report-description">' + escapeValue(item.description) + '</pre></div>' +
          '<details class="stack-gap-sm" ontoggle="if(this.open)loadProblemReportAttachmentManifest(\'' + String(item.id).replace(/'/g, "\\'") + '\')"><summary>Attachments</summary><div class="actions" id="problem-attachments-' + escapeValue(item.id) + '" data-report-id="' + escapeValue(item.id) + '" data-manifest-known="' + ((item.screenshot_paths || []).length ? '1' : '') + '">' + attachments + '</div></details>' +
          '<div class="grid2 stack-gap-sm"><div><label>Status</label><select id="problem-status-' + escapeValue(item.id) + '"><option value="pending">Pending</option><option value="fixed">Fixed</option><option value="deferred">Deferred</option><option value="rejected">Not processed</option><option value="archived">Archived</option></select></div><div><label>Administrator note</label><input id="problem-note-' + escapeValue(item.id) + '" value="' + escapeValue(item.admin_note) + '"></div></div>' +
          '<div class="actions section-gap"><button class="btn-primary" onclick="saveProblemReport(\'' + String(item.id).replace(/'/g, "\\'") + '\')">Save status</button><button class="btn-ghost" onclick="deleteProblemReport(\'' + String(item.id).replace(/'/g, "\\'") + '\')">Force delete</button></div></article>';
      }).join('');
      items.forEach(function (item) { const select = document.getElementById('problem-status-' + item.id); if (select) select.value = item.status; });
      renderProblemReportsPager(total, items.length);
    } catch (error) {
      if (seq === problemReportsLoadSeq) root.innerHTML = '<div class="hint">Failed to load reports: ' + escapeValue(error.message || error) + '</div>';
    } finally {
      if (seq === problemReportsLoadSeq) root.setAttribute('aria-busy', 'false');
    }
  };
  window.saveProblemReport = async function (id) {
    const status = document.getElementById('problem-status-' + id).value;
    const admin_note = document.getElementById('problem-note-' + id).value;
    try { await request('/api/v1/admin/problem-reports/' + encodeURIComponent(id), {method:'PUT',body:JSON.stringify({status:status,admin_note:admin_note})}); window.loadProblemReports(); }
    catch (error) { alert(error.message || error); }
  };
  window.downloadProblemReportAttachment = async function (id, file) {
    const path = '/api/v1/admin/problem-reports/' + encodeURIComponent(id) + '/attachments/' + encodeURIComponent(file);
    // Open during the click gesture. Opening only after the async link request is
    // commonly classified as a popup by browser privacy controls.
    const popup = window.open('', '_blank');
    if (popup) popup.opener = null;
    try {
      const link = await request(path + '/link');
      // HA-origin attachments use a short-lived signed URL.  Local attachments
      // deliberately remain behind RequireAdmin, so a normal browser navigation
      // cannot work: it drops the Authorization header and gets ADMIN_UNAUTHORIZED.
      if (link && link.url) {
        if (!popup) throw new Error('Your browser blocked the download window. Please allow popups for this admin console and try again.');
        popup.location.replace(link.url);
        return;
      }
      if (popup && !popup.closed) popup.close();
      const accessToken = typeof window.token === 'function' ? window.token() : '';
      const response = await fetch(path, {
        headers: accessToken ? { Authorization: 'Bearer ' + accessToken } : {}
      });
      if (!response.ok) {
        const detail = await response.text();
        throw new Error(detail || response.statusText || 'Unable to download attachment.');
      }
      const blobUrl = URL.createObjectURL(await response.blob());
      const anchor = document.createElement('a');
      anchor.href = blobUrl;
      anchor.download = String(file || 'attachment');
      anchor.style.display = 'none';
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(function () { URL.revokeObjectURL(blobUrl); }, 1000);
    } catch (error) {
      if (popup && !popup.closed) popup.close();
      alert(error.message || error);
    }
  };
  function problemReportAttachmentRoot(id) {
    const root = document.getElementById('problem-attachments-' + id);
    return root && root.dataset.reportId === String(id) ? root : null;
  }
  function pumpProblemReportAttachmentManifests() {
    while (attachmentManifestActive < ATTACHMENT_MANIFEST_CONCURRENCY && attachmentManifestQueue.length) {
      const job = attachmentManifestQueue.shift();
      const root = problemReportAttachmentRoot(job.id);
      if (root !== job.root || root.dataset.manifestToken !== job.token) continue;
      attachmentManifestActive++;
      root.dataset.manifestState = 'loading';
      request('/api/v1/admin/problem-reports/' + encodeURIComponent(job.id) + '/attachments').then(function (payload) {
        const current = problemReportAttachmentRoot(job.id);
        if (current !== job.root || current.dataset.manifestToken !== job.token) return;
        const files = Array.isArray(payload.items) ? payload.items : [];
        if (files.length) {
          current.replaceChildren();
          files.forEach(function (file) {
            const button = document.createElement('button');
            button.type = 'button'; button.className = 'btn-ghost compact-btn'; button.textContent = file;
            button.onclick = function () { window.downloadProblemReportAttachment(job.id, file); };
            current.appendChild(button);
          });
        }
        current.dataset.manifestState = 'loaded';
      }).catch(function () {
        const current = problemReportAttachmentRoot(job.id);
        if (current === job.root && current.dataset.manifestToken === job.token) current.dataset.manifestState = '';
      }).finally(function () {
        attachmentManifestActive--;
        pumpProblemReportAttachmentManifests();
      });
    }
  }
  window.loadProblemReportAttachmentManifest = function (id, knownScreenshots) {
    if (knownScreenshots && knownScreenshots.length) return;
    const root = problemReportAttachmentRoot(id);
    if (!root || root.dataset.manifestKnown === '1' || ['queued', 'loading', 'loaded'].includes(root.dataset.manifestState)) return;
    const token = String(Number(root.dataset.manifestToken || 0) + 1);
    root.dataset.manifestToken = token;
    root.dataset.manifestState = 'queued';
    attachmentManifestQueue.push({id:String(id), root:root, token:token});
    pumpProblemReportAttachmentManifests();
  };
  window.deleteProblemReport = async function (id) {
    if (!window.confirm('Force delete this report and all stored attachments? This cannot be undone.')) return;
    try { await request('/api/v1/admin/problem-reports/' + encodeURIComponent(id), {method:'DELETE'}); window.loadProblemReports(); }
    catch (error) { alert(problemReportErrorMessage(error)); }
  };
  function installWhenAdminShellReady(attempt) {
    if (installProblemReportsTab()) {
      // Restore the tab after the panel exists. admin-core's restoreTab() runs
      // before this module executes, so it could not have restored it itself.
      // Guard on token() to avoid firing an unauthenticated data load.
      var savedTab = null;
      try { savedTab = localStorage.getItem('maclawHubCenterActiveTab'); } catch (_) {}
      var signedIn = typeof window.token === 'function' && !!window.token();
      if (savedTab === 'problemreports' && signedIn && typeof window.openTab === 'function') window.openTab('problemreports');
      return;
    }
    if (attempt >= 20) return;
    // The admin shell may be replaced by another deferred module during startup.
    // Retry briefly instead of leaving this navigation entry silently absent.
    window.setTimeout(function () { installWhenAdminShellReady(attempt + 1); }, 50);
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { installWhenAdminShellReady(0); }, { once: true });
  } else {
    installWhenAdminShellReady(0);
  }
}());
