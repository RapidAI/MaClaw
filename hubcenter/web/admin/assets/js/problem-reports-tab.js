(function () {
  const PROBLEM_REPORTS_PAGE_SIZE = 30;
  const PR_TEXT = {
    en: { heading: 'Fault reports', desc: 'Review diagnostic packages, contact details, and handling status.', all: 'All statuses', pending: 'Pending', fixed: 'Fixed', deferred: 'Deferred', rejected: 'Not processed', archived: 'Archived', refresh: 'Refresh', empty: 'No problem reports.', loading: 'Loading...', prev: 'Previous', next: 'Next', page: 'Showing {start}-{end} of {total}', reporter: 'Reporter ID / contact (admin only)', runtime: 'Runtime', os: 'Operating system', notProvided: 'Not provided', description: 'Description', attachments: 'Attachments', status: 'Status', note: 'Administrator note', save: 'Save status', forceDelete: 'Force delete', notAvailable: 'Not available', loadFailed: 'Failed to load reports: {error}', confirmDelete: 'Force delete this report and all stored attachments? This cannot be undone.', unavailable502: 'The original upload server is temporarily unavailable (502). The report and its attachments were not deleted; please try again shortly.', deleteFailed: 'Unable to delete the problem report.', downloadFailed: 'Unable to download attachment.', popupBlocked: 'Your browser blocked the download window. Please allow popups for this admin console and try again.', zipLink: 'Diagnostics package', zipLinkHint: 'Click to copy. The download does not require sign-in.', zipCopied: 'Copied to the clipboard', zipCopyFailed: 'Unable to copy the link.', listURL: 'Unrepaired list (Markdown, no sign-in)', repairURL: 'Repair API (POST)', apiKey: 'API key', saveKey: 'Save', regenKey: 'New key', revokeKey: 'Revoke', copy: 'Copy', openList: 'Open', agentHint: 'Give this address to the repair agent. The page needs no sign-in and is rebuilt on every request, so a new submission shows up the next time it is opened. The list and the API key replicate through HA, so every node serves the same pending reports. The agent downloads each diagnostics package, then POSTs the repair API. It can change only a pending report, to Pending, Fixed, Deferred, Not processed, or Archived, and must include an administrator note. The last four leave the list.', keySaved: 'API key saved', keyRequired: 'Enter an API key of at least 16 characters, or generate one.', confirmRegen: 'Create a new API key? The current key stops working immediately. After HA sync, other nodes accept only the new key.', confirmRevoke: 'Revoke the current API key? The repair API rejects it until a new key is created. Other nodes drop it after HA sync.', keyFailed: 'Unable to save the API key: {error}', agentLoadFailed: 'Unable to load the agent access settings: {error}' },
    zh: { heading: '故障报告', desc: '查看用户诊断包、联系信息及处理状态。', all: '全部状态', pending: '待处理', fixed: '已修复', deferred: '延期', rejected: '不予处理', archived: '已归档', refresh: '刷新', empty: '暂无故障报告。', loading: '加载中...', prev: '上一页', next: '下一页', page: '显示 {start}-{end} / {total}', reporter: '报告人 ID / 联系方式（仅管理员）', runtime: '运行环境', os: '操作系统', notProvided: '未提供', description: '描述', attachments: '附件', status: '状态', note: '管理员备注', save: '保存状态', forceDelete: '强制删除', notAvailable: '不可用', loadFailed: '加载故障报告失败：{error}', confirmDelete: '强制删除此报告及其全部附件？此操作不可撤销。', unavailable502: '原始上传服务器暂时不可用（502）。报告及其附件未被删除，请稍后重试。', deleteFailed: '无法删除故障报告。', downloadFailed: '无法下载附件。', popupBlocked: '浏览器拦截了下载窗口。请允许此管理台的弹出窗口后重试。', zipLink: '诊断包', zipLinkHint: '点击复制。下载不需要登录。', zipCopied: '已复制到剪贴板', zipCopyFailed: '无法复制链接。', listURL: '未修复清单（Markdown，无需登录）', repairURL: '修复接口（POST）', apiKey: 'API Key', saveKey: '保存', regenKey: '新建', revokeKey: '作废', copy: '复制', openList: '打开', agentHint: '把清单地址交给修复用的 AI。页面无需登录，每次访问都按当前待处理故障现算，有新的提交会自动出现。清单和 API Key 会随高可用同步，其它节点看到的是同一份。AI 下载诊断包后调用修复接口，只能改待处理的报告，状态为待处理、已修复、延期、不予处理、已归档，并必须填写管理员备注。改为后四种后，该条会从清单消失。', keySaved: 'API Key 已保存', keyRequired: '请填写至少 16 位的 API Key，或点生成。', confirmRegen: '新建一把 API Key？当前 Key 会立即作废。同步后，其它节点也只认新 Key。', confirmRevoke: '作废当前 API Key？修复接口会立刻拒绝它，直到新建一把。同步后其它节点同样失效。', keyFailed: '无法保存 API Key：{error}', agentLoadFailed: '无法加载 AI 修复入口：{error}' },
    mn: { heading: 'Алдааны тайлан', desc: 'Оношилгооны багц, холбоо барих мэдээлэл, шийдвэрлэлтийн төлөвийг хянана.', all: 'Бүх төлөв', pending: 'Хүлээгдэж буй', fixed: 'Зассан', deferred: 'Хойшлуулсан', rejected: 'Шийдвэрлэхгүй', archived: 'Архивласан', refresh: 'Шинэчлэх', empty: 'Алдааны тайлан алга.', loading: 'Ачаалж байна...', prev: 'Өмнөх', next: 'Дараах', page: '{total}-аас {start}-{end} харуулж байна', reporter: 'Мэдээлэгчийн ID / холбоо (зөвхөн админ)', runtime: 'Ажиллах орчин', os: 'Үйлдлийн систем', notProvided: 'Өгөөгүй', description: 'Тайлбар', attachments: 'Хавсралт', status: 'Төлөв', note: 'Админы тэмдэглэл', save: 'Төлөв хадгалах', forceDelete: 'Шууд устгах', notAvailable: 'Байхгүй', loadFailed: 'Тайлан ачаалах амжилтгүй: {error}', confirmDelete: 'Энэ тайлан ба хадгалсан бүх хавсралтыг шууд устгах уу? Үүнийг буцаах боломжгүй.', unavailable502: 'Анхны оруулалтын сервер түр боломжгүй (502). Тайлан ба хавсралт устсангүй. Түр хүлээгээд дахин оролдоно уу.', deleteFailed: 'Алдааны тайланг устгаж чадсангүй.', downloadFailed: 'Хавсралт татаж чадсангүй.', popupBlocked: 'Хөтөч татах цонхыг хаасан. Энэ админ консолд попап зөвшөөрөөд дахин оролдоно уу.', zipLink: 'Оношилгооны багц', zipLinkHint: 'Хуулж авахын тулд дарна уу. Татахад нэвтрэх шаардлагагүй.', zipCopied: 'Самбар луу хууллаа', zipCopyFailed: 'Холбоосыг хуулж чадсангүй.', listURL: 'Засаагүй жагсаалт (Markdown, нэвтрэхгүй)', repairURL: 'Засварын API (POST)', apiKey: 'API түлхүүр', saveKey: 'Хадгалах', regenKey: 'Шинэ түлхүүр', revokeKey: 'Хүчингүй болгох', copy: 'Хуулах', openList: 'Нээх', agentHint: 'Энэ хаягийг засварын AI-д өгнө. Нэвтрэх шаардлагагүй бөгөөд хандах бүрд шинэчлэгдэнэ. Жигсаалт ба API түлхүүр HA-аар синк хийгдэж, бүх зангилаа нэг жагсаалтыг харуулна. AI зөвхөн хүлээгдэж буй тайланг Зассан, Хойшлуулсан, Шийдвэрлэхгүй, Архивласан эсвэл хүлээгдэж буй болгож, админы тэмдэглэл заавал бичнэ.', keySaved: 'API түлхүүр хадгалагдлаа', keyRequired: 'Дор хаяж 16 тэмдэгттэй түлхүүр оруулах эсвэл үүсгэнэ үү.', confirmRegen: 'Шинэ API түлхүүр үүсгэх үү? Одоогийн түлхүүр даруй хүчингүй болно. HA синкийн дараа бусад зангилаа зөвхөн шинэ түлхүүрийг хүлээн авна.', confirmRevoke: 'Одоогийн API түлхүүрийг хүчингүй болгох уу? Засварын API шинэ түлхүүр үүсгэх хүртэл татгалзана. HA синкийн дараа бусад зангилаа ч мөн хүчингүй болгоно.', keyFailed: 'API түлхүүр хадгалж чадсангүй: {error}', agentLoadFailed: 'AI засварын тохиргоо ачаалж чадсангүй: {error}' }
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
    const refresh = document.getElementById('problemReportsRefresh');
    if (refresh) refresh.textContent = pr('refresh');
    const listLabel = document.getElementById('problemReportListLabel');
    const repairLabel = document.getElementById('problemReportRepairLabel');
    const keyLabel = document.getElementById('problemReportKeyLabel');
    const agentHint = document.getElementById('problemReportAgentHint');
    if (listLabel) listLabel.textContent = pr('listURL');
    if (repairLabel) repairLabel.textContent = pr('repairURL');
    if (keyLabel) keyLabel.textContent = pr('apiKey');
    if (agentHint) agentHint.textContent = pr('agentHint');
    ['problemReportCopyList', 'problemReportCopyRepair', 'problemReportCopyKey'].forEach(function (id) {
      const node = document.getElementById(id);
      if (node) node.textContent = pr('copy');
    });
    const openList = document.getElementById('problemReportOpenList');
    const saveKey = document.getElementById('problemReportSaveKey');
    const regenKey = document.getElementById('problemReportRegenKey');
    if (openList) openList.textContent = pr('openList');
    if (saveKey) saveKey.textContent = pr('saveKey');
    if (regenKey) regenKey.textContent = pr('regenKey');
    const revokeKey = document.getElementById('problemReportRevokeKey');
    if (revokeKey) revokeKey.textContent = pr('revokeKey');
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
  let problemReportsSignature = '';
  let problemReportKeyEpoch = 0;
  let problemReportsPoll = 0;
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
    panel.innerHTML = '<div class="head problem-report-head"><div class="problem-report-intro"><h3></h3><div class="desc"></div><div class="hint" id="problemReportAgentHint"></div></div><div class="problem-report-agent" id="problemReportAgent"><div class="problem-report-agent-field"><label id="problemReportListLabel" for="problemReportListURL"></label><div class="problem-report-agent-row"><input id="problemReportListURL" readonly><button type="button" class="btn-ghost" id="problemReportCopyList" data-copy-input="problemReportListURL"></button><a id="problemReportOpenList" class="btn-ghost" target="_blank" rel="noopener"></a></div></div><div class="problem-report-agent-field"><label id="problemReportRepairLabel" for="problemReportRepairURL"></label><div class="problem-report-agent-row"><input id="problemReportRepairURL" readonly><button type="button" class="btn-ghost" id="problemReportCopyRepair" data-copy-input="problemReportRepairURL"></button></div></div><div class="problem-report-agent-field"><label id="problemReportKeyLabel" for="problemReportAPIKey"></label><div class="problem-report-agent-row"><input id="problemReportAPIKey" autocomplete="off" spellcheck="false"><button type="button" class="btn-ghost" id="problemReportCopyKey" data-copy-input="problemReportAPIKey"></button><button type="button" class="btn-primary" id="problemReportSaveKey"></button><button type="button" class="btn-ghost" id="problemReportRegenKey"></button><button type="button" class="btn-ghost" id="problemReportRevokeKey"></button></div></div></div></div><div class="problem-report-toolbar"><select id="problemReportsStatus" onchange="changeProblemReportsStatus()"><option value=""></option><option value="pending"></option><option value="fixed"></option><option value="deferred"></option><option value="rejected"></option><option value="archived"></option></select><button id="problemReportsRefresh" class="btn-ghost" type="button" onclick="loadProblemReports()"></button></div><div id="problemReportsList" class="list"><div class="hint"></div></div><div id="problemReportsPager" class="pager pager-compact"><div id="problemReportsPageInfo" class="pager-meta" aria-live="polite"></div><div class="pager-actions"><button id="problemReportsPrev" class="btn-ghost" type="button" onclick="changeProblemReportsPage(-1)"></button><button id="problemReportsNext" class="btn-ghost" type="button" onclick="changeProblemReportsPage(1)"></button></div></div>';
    // The panel is not in the document yet, so getElementById cannot see it.
    const reportList = panel.querySelector('#problemReportsList');
    if (reportList && !reportList.dataset.zipCopyBound) {
      reportList.dataset.zipCopyBound = '1';
      reportList.addEventListener('click', function (event) {
        const button = event.target && event.target.closest ? event.target.closest('.problem-report-zip-link') : null;
        if (!button || !reportList.contains(button)) return;
        const url = button.getAttribute('data-zip-url') || '';
        if (!url) return;
        copyProblemReportLink(url).then(function () {
          const previous = button.textContent;
          button.textContent = pr('zipCopied');
          window.setTimeout(function () { if (button.isConnected) button.textContent = previous; }, 1600);
        }).catch(function () { window.alert(pr('zipCopyFailed')); });
      });
    }
    const agentBox = panel.querySelector('#problemReportAgent');
    if (agentBox) {
      agentBox.addEventListener('click', function (event) {
        const button = event.target && event.target.closest ? event.target.closest('[data-copy-input]') : null;
        if (!button || !agentBox.contains(button)) return;
        const input = document.getElementById(button.getAttribute('data-copy-input') || '');
        const value = input ? String(input.value || '') : '';
        if (!value) return;
        copyProblemReportLink(value).then(function () {
          const previous = button.textContent;
          button.textContent = pr('zipCopied');
          window.setTimeout(function () { if (button.isConnected) button.textContent = previous; }, 1600);
        }).catch(function () { window.alert(pr('zipCopyFailed')); });
      });
    }
    const saveKey = panel.querySelector('#problemReportSaveKey');
    const regenKey = panel.querySelector('#problemReportRegenKey');
    if (saveKey) saveKey.onclick = function () { window.saveProblemReportAgentKey(false); };
    if (regenKey) regenKey.onclick = function () { window.saveProblemReportAgentKey(true); };
    const revokeKey = panel.querySelector('#problemReportRevokeKey');
    if (revokeKey) revokeKey.onclick = function () { window.revokeProblemReportAgentKey(); };
    main.appendChild(panel);
    applyProblemReportsChrome();
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
      window.loadProblemReportAgentAccess();
    };
    startProblemReportsPoll();
    return true;
  }
  window.installProblemReportsTab = installProblemReportsTab;
  function escapeValue(value) { return typeof escapeHtml === 'function' ? escapeHtml(String(value || '')) : String(value || ''); }
  function copyProblemReportLink(value) {
    if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(value);
    return new Promise(function (resolve, reject) {
      const input = document.createElement('textarea');
      input.value = value;
      input.setAttribute('readonly', 'readonly');
      document.body.appendChild(input);
      input.select();
      try {
        if (document.execCommand('copy')) resolve();
        else reject(new Error('copy'));
      } catch (err) {
        reject(err);
      } finally {
        input.remove();
      }
    });
  }
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
  function problemReportListEditing() {
    const root = document.getElementById('problemReportsList');
    const active = document.activeElement;
    return !!(root && active && root.contains(active));
  }
  // Quiet refresh must keep an unsaved status or note after the field blurs.
  function problemReportListDirty() {
    const root = document.getElementById('problemReportsList');
    if (!root) return false;
    const fields = root.querySelectorAll('[data-saved]');
    for (let i = 0; i < fields.length; i++) {
      if (fields[i].value !== fields[i].getAttribute('data-saved')) return true;
    }
    return false;
  }
  function problemReportsViewSignature(items, total, status) {
    return status + '|' + problemReportsPage + '|' + total + '|' + items.map(function (item) {
      return [item.id, item.status, item.updated_at, item.admin_note, item.diagnostics_url, item.description].join('\u001f');
    }).join('\n');
  }
  function startProblemReportsPoll() {
    if (problemReportsPoll) return;
    problemReportsPoll = window.setInterval(function () {
      const panel = document.getElementById('tab-problemreports');
      const signedIn = typeof window.token === 'function' && !!window.token();
      if (document.hidden || !signedIn || !panel || !panel.classList.contains('active')) return;
      window.loadProblemReports({quiet: true});
      window.loadProblemReportAgentAccess({quiet: true});
    }, 15000);
  }
  function absoluteAgentURL(value, path) {
    const raw = String(value || '').trim();
    if (/^https?:\/\//i.test(raw)) return raw;
    return window.location.origin + (raw.charAt(0) === '/' ? raw : path);
  }
  function paintProblemReportAgentAccess(payload, force) {
    const data = payload || {};
    const listURL = absoluteAgentURL(data.list_url, '/api/v1/problem-reports/open.md');
    const repairURL = absoluteAgentURL(data.repair_url, '/api/v1/problem-reports/repair');
    const list = document.getElementById('problemReportListURL');
    const repair = document.getElementById('problemReportRepairURL');
    const openList = document.getElementById('problemReportOpenList');
    const key = document.getElementById('problemReportAPIKey');
    if (list) list.value = listURL;
    if (repair) repair.value = repairURL;
    if (openList) openList.href = listURL;
    if (key && typeof data.api_key === 'string' && (force || document.activeElement !== key)) {
      const saved = key.getAttribute('data-saved');
      const untouched = !!force || (saved === null ? key.value === '' : key.value === saved);
      if (untouched) {
        key.value = data.api_key;
        key.setAttribute('data-saved', data.api_key);
      }
    }
  }
  function setProblemReportKeyBusy(busy) {
    ['problemReportSaveKey', 'problemReportRegenKey', 'problemReportRevokeKey'].forEach(function (id) {
      const node = document.getElementById(id);
      if (node) node.disabled = busy;
    });
  }
  // A list refresh that started before 作废/新建 must not paint the old key
  // back over the result. Only the newest key operation may update the field.
  async function commitProblemReportAgentKey(body) {
    const epoch = ++problemReportKeyEpoch;
    setProblemReportKeyBusy(true);
    try {
      const payload = await request('/api/v1/admin/problem-reports/agent-access', {
        method: 'PUT',
        body: JSON.stringify(body)
      });
      if (epoch !== problemReportKeyEpoch) return;
      paintProblemReportAgentAccess(payload, true);
    } catch (error) {
      if (epoch !== problemReportKeyEpoch) return;
      window.alert(pr('keyFailed', {error: error && error.message || error}));
    } finally {
      if (epoch === problemReportKeyEpoch) setProblemReportKeyBusy(false);
    }
  }
  window.loadProblemReportAgentAccess = async function (opts) {
    const epoch = problemReportKeyEpoch;
    try {
      const payload = await request('/api/v1/admin/problem-reports/agent-access');
      if (epoch !== problemReportKeyEpoch) return;
      paintProblemReportAgentAccess(payload);
    } catch (error) {
      if (opts && opts.quiet || epoch !== problemReportKeyEpoch) return;
      const list = document.getElementById('problemReportListURL');
      if (!list || !String(list.value || '').trim()) paintProblemReportAgentAccess({});
    }
  };
  window.revokeProblemReportAgentKey = async function () {
    const input = document.getElementById('problemReportAPIKey');
    const saved = input ? String(input.getAttribute('data-saved') || '').trim() : '';
    if (!saved) return;
    if (!window.confirm(pr('confirmRevoke'))) return;
    await commitProblemReportAgentKey({revoke: true});
  };
  window.saveProblemReportAgentKey = async function (regenerate) {
    const input = document.getElementById('problemReportAPIKey');
    const typed = input ? String(input.value || '').trim() : '';
    if (regenerate && !window.confirm(pr('confirmRegen'))) return;
    if (!regenerate && typed.length < 16) {
      window.alert(pr('keyRequired'));
      return;
    }
    await commitProblemReportAgentKey(regenerate ? {regenerate: true} : {api_key: typed});
  };
  window.loadProblemReports = async function (opts) {
    const quiet = !!(opts && opts.quiet);
    const root = document.getElementById('problemReportsList');
    if (!root) return;
    if (quiet && (document.hidden || problemReportListEditing() || problemReportListDirty())) return;
    const seq = ++problemReportsLoadSeq;
    if (!quiet) {
      root.setAttribute('aria-busy', 'true');
      root.textContent = pr('loading');
    }
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
        window.loadProblemReports(quiet ? {quiet: true} : undefined);
        return;
      }
      const signature = problemReportsViewSignature(items, total, status);
      if (quiet && signature === problemReportsSignature && root.querySelector('article, .hint')) {
        return;
      }
      if (quiet && (problemReportListEditing() || problemReportListDirty())) return;
      problemReportsSignature = signature;
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
          (item.diagnostics_url ? '<div class="stack-gap-sm"><label>' + escapeValue(pr('zipLink')) + '</label><button type="button" class="btn-ghost problem-report-zip-link" data-zip-url="' + escapeValue(item.diagnostics_url) + '">' + escapeValue(item.diagnostics_url) + '</button><div class="hint">' + escapeValue(pr('zipLinkHint')) + '</div></div>' : '') +
          '<details class="stack-gap-sm" ontoggle="if(this.open)loadProblemReportAttachmentManifest(\'' + String(item.id).replace(/'/g, "\\'") + '\')"><summary>' + escapeValue(pr('attachments')) + '</summary><div class="actions" id="problem-attachments-' + escapeValue(item.id) + '" data-report-id="' + escapeValue(item.id) + '" data-manifest-known="' + ((item.screenshot_paths || []).length ? '1' : '') + '">' + attachments + '</div></details>' +
          '<div class="grid2 stack-gap-sm"><div><label>' + escapeValue(pr('status')) + '</label><select id="problem-status-' + escapeValue(item.id) + '"><option value="pending">' + escapeValue(pr('pending')) + '</option><option value="fixed">' + escapeValue(pr('fixed')) + '</option><option value="deferred">' + escapeValue(pr('deferred')) + '</option><option value="rejected">' + escapeValue(pr('rejected')) + '</option><option value="archived">' + escapeValue(pr('archived')) + '</option></select></div><div><label>' + escapeValue(pr('note')) + '</label><input id="problem-note-' + escapeValue(item.id) + '" value="' + escapeValue(item.admin_note) + '"></div></div>' +
          '<div class="actions section-gap"><button class="btn-primary" onclick="saveProblemReport(\'' + String(item.id).replace(/'/g, "\\'") + '\')">' + escapeValue(pr('save')) + '</button><button class="btn-ghost" onclick="deleteProblemReport(\'' + String(item.id).replace(/'/g, "\\'") + '\')">' + escapeValue(pr('forceDelete')) + '</button></div></article>';
      }).join('');
      items.forEach(function (item) {
        const select = document.getElementById('problem-status-' + item.id);
        if (select) {
          select.value = item.status || 'pending';
          select.setAttribute('data-saved', select.value);
        }
        const note = document.getElementById('problem-note-' + item.id);
        if (note) note.setAttribute('data-saved', note.value);
      });
      renderProblemReportsPager(total, items.length);
    } catch (error) {
      if (seq !== problemReportsLoadSeq) return;
      if (quiet && root.querySelector('article, .hint')) return;
      root.innerHTML = '<div class="hint">' + escapeValue(pr('loadFailed', { error: error.message || error })) + '</div>';
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
