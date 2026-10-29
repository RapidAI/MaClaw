(function () {
  const PROBLEM_REPORTS_PAGE_SIZE = 30;
  const PR_TEXT = {
    en: { heading: 'Fault reports', desc: 'Review diagnostic packages, contact details, and handling status.', all: 'All statuses', pending: 'Pending', fixed: 'Fixed', deferred: 'Deferred', rejected: 'Not processed', archived: 'Archived', refresh: 'Refresh', empty: 'No problem reports.', loading: 'Loading...', prev: 'Previous', next: 'Next', page: 'Showing {start}-{end} of {total}', reporter: 'Reporter ID / contact (admin only)', runtime: 'Runtime', os: 'Operating system', notProvided: 'Not provided', description: 'Description', attachments: 'Attachments', status: 'Status', note: 'Administrator note', save: 'Save status', forceDelete: 'Force delete', notAvailable: 'Not available', loadFailed: 'Failed to load reports: {error}', confirmDelete: 'Force delete this report and all stored attachments? This cannot be undone.', unavailable502: 'The original upload server is temporarily unavailable (502). The report and its attachments were not deleted; please try again shortly.', deleteFailed: 'Unable to delete the problem report.', downloadFailed: 'Unable to download attachment.', popupBlocked: 'Your browser blocked the download window. Please allow popups for this admin console and try again.' },
    zh: { heading: '故障报告', desc: '查看用户诊断包、联系信息及处理状态。', all: '全部状态', pending: '待处理', fixed: '已修复', deferred: '延期', rejected: '不予处理', archived: '已归档', refresh: '刷新', empty: '暂无故障报告。', loading: '加载中...', prev: '上一页', next: '下一页', page: '显示 {start}-{end} / {total}', reporter: '报告人 ID / 联系方式（仅管理员）', runtime: '运行环境', os: '操作系统', notProvided: '未提供', description: '描述', attachments: '附件', status: '状态', note: '管理员备注', save: '保存状态', forceDelete: '强制删除', notAvailable: '不可用', loadFailed: '加载故障报告失败：{error}', confirmDelete: '强制删除此报告及其全部附件？此操作不可撤销。', unavailable502: '原始上传服务器暂时不可用（502）。报告及其附件未被删除，请稍后重试。', deleteFailed: '无法删除故障报告。', downloadFailed: '无法下载附件。', popupBlocked: '浏览器拦截了下载窗口。请允许此管理台的弹出窗口后重试。' },
    mn: { heading: 'Алдааны тайлан', desc: 'Оношилгооны багц, холбоо барих мэдээлэл, шийдвэрлэлтийн төлөвийг хянана.', all: 'Бүх төлөв', pending: 'Хүлээгдэж буй', fixed: 'Зассан', deferred: 'Хойшлуулсан', rejected: 'Шийдвэрлэхгүй', archived: 'Архивласан', refresh: 'Шинэчлэх', empty: 'Алдааны тайлан алга.', loading: 'Ачаалж байна...', prev: 'Өмнөх', next: 'Дараах', page: '{total}-аас {start}-{end} харуулж байна', reporter: 'Мэдээлэгчийн ID / холбоо (зөвхөн админ)', runtime: 'Ажиллах орчин', os: 'Үйлдлийн систем', notProvided: 'Өгөөгүй', description: 'Тайлбар', attachments: 'Хавсралт', status: 'Төлөв', note: 'Админы тэмдэглэл', save: 'Төлөв хадгалах', forceDelete: 'Шууд устгах', notAvailable: 'Байхгүй', loadFailed: 'Тайлан ачаалах амжилтгүй: {error}', confirmDelete: 'Энэ тайлан ба хадгалсан бүх хавсралтыг шууд устгах уу? Үүнийг буцаах боломжгүй.', unavailable502: 'Анхны оруулалтын сервер түр боломжгүй (502). Тайлан ба хавсралт устсангүй. Түр хүлээгээд дахин оролдоно уу.', deleteFailed: 'Алдааны тайланг устгаж чадсангүй.', downloadFailed: 'Хавсралт татаж чадсангүй.', popupBlocked: 'Хөтөч татах цонхыг хаасан. Энэ админ консолд попап зөвшөөрөөд дахин оролдоно уу.' }
  };
  function pr(key, vars) {
    const lang = window.currentLang === 'zh' ? 'zh' : window.currentLang === 'mn' ? 'mn' : 'en';
    const table = PR_TEXT[lang] || PR_TEXT.en;
    const raw = (table[key] || PR_TEXT.en[key] || key);
    return String(raw).replace(/\{(\w+)\}/g, function (_, name) { return vars && vars[name] != null ? vars[name] : ''; });
  }
  function applyProblemReportsChrome() {
    const panel = document.getElementById('tab-problemreports');
    if (!panel) return;
    const heading = panel.querySelector('.head h3');
    const desc = panel.querySelector('.head .desc');
    if (heading) heading.textContent = pr('heading');
    if (desc) desc.textContent = pr('desc');
    const select = document.getElementById('problemReportsStatus');
    if (select) Array.prototype.forEach.call(select.options, function (option) { option.textContent = pr(option.value || 'all'); });
    const refresh = panel.querySelector('.head .actions button');
    if (refresh) refresh.textContent = pr('refresh');
    const prev = document.getElementById('problemReportsPrev');
    const next = document.getElementById('problemReportsNext');
    if (prev) prev.textContent = pr('prev');
    if (next) next.textContent = pr('next');
    const hint = document.querySelector('#problemReportsList > .hint');
    if (hint) hint.textContent = pr('empty');
  }
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
    panel.innerHTML = '<div class="head"><div><h3></h3><div class="desc"></div></div><div class="actions"><select id="problemReportsStatus" onchange="changeProblemReportsStatus()"><option value=""></option><option value="pending"></option><option value="fixed"></option><option value="deferred"></option><option value="rejected"></option><option value="archived"></option></select><button class="btn-ghost" onclick="loadProblemReports()"></button></div></div><div id="problemReportsList" class="list"><div class="hint"></div></div><div id="problemReportsPager" class="pager pager-compact"><div id="problemReportsPageInfo" class="pager-meta" aria-live="polite"></div><div class="pager-actions"><button id="problemReportsPrev" class="btn-ghost" type="button" onclick="changeProblemReportsPage(-1)"></button><button id="problemReportsNext" class="btn-ghost" type="button" onclick="changeProblemReportsPage(1)"></button></div></div>';
    applyProblemReportsChrome();
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
  function label(status) { return pr(status) || status; }
  function reporterContact(value) {
    const contact = String(value || '').trim();
    if (!contact) return pr('notAvailable');
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
      return pr('unavailable502');
    }
    return raw || pr('deleteFailed');
  }
  function renderProblemReportsPager(total, itemCount) {
    const pages = Math.max(1, Math.ceil(total / PROBLEM_REPORTS_PAGE_SIZE));
    const offset = (problemReportsPage - 1) * PROBLEM_REPORTS_PAGE_SIZE;
    const start = itemCount ? offset + 1 : 0;
    const end = offset + itemCount;
    const info = document.getElementById('problemReportsPageInfo');
    const prev = document.getElementById('problemReportsPrev');
    const next = document.getElementById('problemReportsNext');
    if (info) info.textContent = pr('page', { start: start, end: end, total: total });
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
    root.textContent = pr('loading');
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
        root.innerHTML = '<div class="hint">' + escapeValue(pr('empty')) + '</div>';
        renderProblemReportsPager(total, 0);
        return;
      }
      root.innerHTML = items.map(function (item) {
        const attachments = ['diagnostics.zip'].concat(item.screenshot_paths || []).map(function (file) {
          return '<button type="button" class="btn-ghost compact-btn" onclick="downloadProblemReportAttachment(\'' + String(item.id).replace(/'/g, "\\'") + '\',\'' + String(file).replace(/'/g, "\\'") + '\')">' + escapeValue(file) + '</button>';
        }).join(' ');
        return '<article class="item"><div class="head"><div><div class="item-title">' + escapeValue(item.id) + ' <span class="badge ' + (item.status === 'fixed' ? 'ok' : item.status === 'pending' ? 'warn' : '') + '">' + escapeValue(label(item.status)) + '</span></div><div class="item-meta">' + escapeValue(item.created_at) + '</div></div></div>' +
          '<div class="grid2"><div><label>' + escapeValue(pr('reporter')) + '</label><div class="item-meta">' + escapeValue(item.reporter_user_id) + '<br>' + escapeValue(reporterContact(item.reporter_contact)) + '</div></div><div><label>' + escapeValue(pr('runtime')) + '</label><div class="item-meta">' + escapeValue(pr('os')) + '：' + escapeValue(item.os_version) + '<br>MaClaw GUI：' + escapeValue(item.gui_version || pr('notProvided')) + '</div></div></div>' +
          '<div class="stack-gap-sm"><label>' + escapeValue(pr('description')) + '</label><pre class="item-meta problem-report-description">' + escapeValue(item.description) + '</pre></div>' +
          '<details class="stack-gap-sm" ontoggle="if(this.open)loadProblemReportAttachmentManifest(\'' + String(item.id).replace(/'/g, "\\'") + '\')"><summary>' + escapeValue(pr('attachments')) + '</summary><div class="actions" id="problem-attachments-' + escapeValue(item.id) + '" data-report-id="' + escapeValue(item.id) + '" data-manifest-known="' + ((item.screenshot_paths || []).length ? '1' : '') + '">' + attachments + '</div></details>' +
          '<div class="grid2 stack-gap-sm"><div><label>' + escapeValue(pr('status')) + '</label><select id="problem-status-' + escapeValue(item.id) + '"><option value="pending">' + escapeValue(pr('pending')) + '</option><option value="fixed">' + escapeValue(pr('fixed')) + '</option><option value="deferred">' + escapeValue(pr('deferred')) + '</option><option value="rejected">' + escapeValue(pr('rejected')) + '</option><option value="archived">' + escapeValue(pr('archived')) + '</option></select></div><div><label>' + escapeValue(pr('note')) + '</label><input id="problem-note-' + escapeValue(item.id) + '" value="' + escapeValue(item.admin_note) + '"></div></div>' +
          '<div class="actions section-gap"><button class="btn-primary" onclick="saveProblemReport(\'' + String(item.id).replace(/'/g, "\\'") + '\')">' + escapeValue(pr('save')) + '</button><button class="btn-ghost" onclick="deleteProblemReport(\'' + String(item.id).replace(/'/g, "\\'") + '\')">' + escapeValue(pr('forceDelete')) + '</button></div></article>';
      }).join('');
      items.forEach(function (item) { const select = document.getElementById('problem-status-' + item.id); if (select) select.value = item.status; });
      renderProblemReportsPager(total, items.length);
    } catch (error) {
      if (seq === problemReportsLoadSeq) root.innerHTML = '<div class="hint">' + escapeValue(pr('loadFailed', { error: error.message || error })) + '</div>';
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
        if (!popup) throw new Error(pr('popupBlocked'));
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
        throw new Error(detail || response.statusText || pr('downloadFailed'));
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
    if (!window.confirm(pr('confirmDelete'))) return;
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
  var originalSetLanguage = window.setLanguage;
  if (typeof originalSetLanguage === 'function' && !window._problemReportsOrigSetLanguage) {
    window._problemReportsOrigSetLanguage = originalSetLanguage;
    window.setLanguage = function () {
      var result = originalSetLanguage.apply(this, arguments);
      applyProblemReportsChrome();
      var panel = document.getElementById('tab-problemreports');
      var signedIn = typeof window.token === 'function' && !!window.token();
      if (panel && panel.classList.contains('active') && signedIn) window.loadProblemReports();
      return result;
    };
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { installWhenAdminShellReady(0); }, { once: true });
  } else {
    installWhenAdminShellReady(0);
  }
}());
