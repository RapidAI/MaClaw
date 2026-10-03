// HubCenter Admin — LaTeX Template Library.
//
// The desktop app browses the public catalogue (GET /api/v1/latex-templates)
// and shares locally imported packages back to the Hub. This module owns the
// operator side of that loop: reviewing submissions, publishing or withdrawing
// a template, and curating the category list the library sections are built
// from (会议 / 期刊 / 毕业论文 / 其它 plus anything an operator adds).
//
// The nav button and panel are injected at runtime instead of being hard-coded
// into index.html so the LaTeX surface stays independently cache-busted and the
// operator console keeps working when this module is absent.

// --- i18n ---

Object.assign(I18N_EN, {
  navLatexTemplates: 'LaTeX Templates',
  navLatexTemplatesDesc: 'Paper template library and review',
  latexTemplatesTitle: 'LaTeX Templates',
  latexTemplatesDesc: 'Upload paper templates, review user submissions, and curate template categories.',
  latexTemplatesRefresh: 'Refresh',
  latexTemplatesSearch: 'Search',
  latexTemplatesSearchPlaceholder: 'Search name, author, or submitter',
  latexTemplatesStatus: 'Status',
  latexTemplatesStatusAll: 'All statuses',
  latexTemplatesStatusPending: 'Pending review',
  latexTemplatesStatusApproved: 'Published',
  latexTemplatesStatusRejected: 'Not published',
  latexTemplatesCategoryAll: 'All categories',
  latexTemplatesUpload: 'Upload template',
  latexTemplatesUploadHint: 'Zip package with at least one .tex file.',
  latexTemplatesUploadPickFile: 'Choose a .zip package to upload.',
  latexTemplatesCategoryRemoved: 'Removed category {name}.',
  latexTemplatesUploadTitle: 'Upload a template',
  latexTemplatesUploadName: 'Name',
  latexTemplatesUploadDescription: 'Description',
  latexTemplatesUploadCategory: 'Category',
  latexTemplatesUploadVersion: 'Version',
  latexTemplatesUploadAuthor: 'Author',
  latexTemplatesUploadNote: 'Review note',
  latexTemplatesUploadSubmit: 'Upload and publish',
  latexTemplatesEmpty: 'No template matches this filter.',
  latexTemplatesSubmitter: 'Submitter',
  latexTemplatesAuthor: 'Author',
  latexTemplatesVersion: 'Version',
  latexTemplatesFiles: 'Source files',
  latexTemplatesSize: 'Package size',
  latexTemplatesUpdated: 'Updated',
  latexTemplatesApprove: 'Publish',
  latexTemplatesReject: 'Reject',
  latexTemplatesUnlist: 'Unlist',
  latexTemplatesDelete: 'Delete',
  latexTemplatesReason: 'Note',
  latexTemplatesReasonHint: 'Required to publish, reject, or unlist',
  latexTemplatesReasonPlaceholder: 'Required for review',
  latexTemplatesCategoryUnchanged: 'Choose a different category first.',
  latexTemplatesMoveTo: 'Category',
  latexTemplatesApplyCategory: 'Move',
  latexTemplatesFileCount: '{n} files',
  latexTemplatesConfirm: 'Confirm',
  latexTemplatesCancel: 'Cancel',
  latexTemplatesDeleteConfirm: 'Delete this template? The package is removed permanently.',
  latexTemplatesCategoriesTitle: 'Categories',
  latexTemplatesCategoriesDesc: 'Built-in categories stay. Rename, reorder, add, or remove your own.',
  latexTemplatesCategoryName: 'Category name',
  latexTemplatesCategoryDescription: 'Description',
  latexTemplatesCategoryOrder: 'Order',
  latexTemplatesCategoryAdd: 'Add category',
  latexTemplatesCategoryNameRequired: 'Enter a category name first.',
  latexTemplatesCategorySave: 'Save',
  latexTemplatesCategoryDelete: 'Remove',
  latexTemplatesCategoryDeleteConfirm: 'Remove this category? Templates must be moved first.',
  latexTemplatesCategoryBuiltin: 'Built-in',
  latexTemplatesActive: 'Active',
  latexTemplatesDisabled: 'Disabled',
  latexTemplatesToggleActive: 'Activate',
  latexTemplatesToggleDisabled: 'Disable',
  latexTemplatesEmptyCategories: 'No category yet.',
  latexTemplatesListTitle: 'Template list',
  latexTemplatesListDesc: 'Filter submissions by status or category, then approve, unpublish, or re-file them.',
  latexTemplatesCount: '{n} templates',
  latexTemplatesCountFiltered: '{n} matching',
  latexTemplatesCategoryStatus: 'Status',
  latexTemplatesCategoryActions: 'Actions',
  latexTemplatesLoading: 'Loading…',
  latexTemplatesLoadFailed: 'Could not load templates: {error}',
  latexTemplatesActionFailed: 'Action failed: {error}',
  latexTemplatesSavedOk: 'Template saved.',
  latexTemplatesApprovedOk: 'Template approved and published.',
  latexTemplatesRejectedOk: 'Template rejected.',
  latexTemplatesUnlistedOk: 'Template unpublished.',
  latexTemplatesDeletedOk: 'Template deleted.',
  latexTemplatesUploadedOk: 'Template uploaded and published.',
  latexTemplatesCategorySavedOk: 'Category saved.',
  latexTemplatesCategoryDeletedOk: 'Category removed.',
  latexTemplatesReasonRequired: 'Enter a moderation note before continuing.',
});

Object.assign(I18N_ZH, {
  navLatexTemplates: 'LaTeX 模板',
  navLatexTemplatesDesc: '论文模板库与审核',
  latexTemplatesTitle: 'LaTeX 模板',
  latexTemplatesDesc: '上传论文模板、审核用户投稿，并维护模板分类。',
  latexTemplatesRefresh: '刷新',
  latexTemplatesSearch: '搜索',
  latexTemplatesSearchPlaceholder: '搜索名称、作者或提交者',
  latexTemplatesStatus: '状态',
  latexTemplatesStatusAll: '全部状态',
  latexTemplatesStatusPending: '待审核',
  latexTemplatesStatusApproved: '已展示',
  latexTemplatesStatusRejected: '未展示',
  latexTemplatesCategoryAll: '全部分类',
  latexTemplatesUpload: '上传模板',
  latexTemplatesUploadHint: 'zip 包，至少包含一个 .tex 文件。',
  latexTemplatesUploadPickFile: '请先选择要上传的 .zip 模板包。',
  latexTemplatesCategoryRemoved: '删除分类 {name}。',
  latexTemplatesUploadTitle: '上传模板',
  latexTemplatesUploadName: '名称',
  latexTemplatesUploadDescription: '描述',
  latexTemplatesUploadCategory: '分类',
  latexTemplatesUploadVersion: '版本',
  latexTemplatesUploadAuthor: '作者',
  latexTemplatesUploadNote: '审核备注',
  latexTemplatesUploadSubmit: '上传并展示',
  latexTemplatesEmpty: '没有符合条件的模板。',
  latexTemplatesSubmitter: '提交者',
  latexTemplatesAuthor: '作者',
  latexTemplatesVersion: '版本',
  latexTemplatesFiles: '源文件数',
  latexTemplatesSize: '压缩包大小',
  latexTemplatesUpdated: '更新时间',
  latexTemplatesApprove: '通过',
  latexTemplatesReject: '驳回',
  latexTemplatesUnlist: '下架',
  latexTemplatesDelete: '删除',
  latexTemplatesReason: '备注',
  latexTemplatesReasonHint: '通过、驳回或下架时必填',
  latexTemplatesReasonPlaceholder: '审核时必填',
  latexTemplatesCategoryUnchanged: '请先选择另一个分类。',
  latexTemplatesMoveTo: '分类',
  latexTemplatesApplyCategory: '归类',
  latexTemplatesFileCount: '{n} 文件',
  latexTemplatesConfirm: '确认',
  latexTemplatesCancel: '取消',
  latexTemplatesDeleteConfirm: '确定删除该模板？压缩包将被永久移除。',
  latexTemplatesCategoriesTitle: '模板分类',
  latexTemplatesCategoriesDesc: '内置分类保留，可重命名、排序、新增或删除自定义分类。',
  latexTemplatesCategoryName: '分类名称',
  latexTemplatesCategoryDescription: '分类描述',
  latexTemplatesCategoryOrder: '排序',
  latexTemplatesCategoryAdd: '新增分类',
  latexTemplatesCategoryNameRequired: '请先填写分类名称。',
  latexTemplatesCategorySave: '保存',
  latexTemplatesCategoryDelete: '删除',
  latexTemplatesCategoryDeleteConfirm: '确定删除该分类？请先移走其中的模板。',
  latexTemplatesCategoryBuiltin: '内置',
  latexTemplatesActive: '启用',
  latexTemplatesDisabled: '停用',
  latexTemplatesToggleActive: '启用',
  latexTemplatesToggleDisabled: '停用',
  latexTemplatesEmptyCategories: '暂无分类。',
  latexTemplatesListTitle: '模板清单',
  latexTemplatesListDesc: '按状态与分类筛选投稿，在此完成审核、下架与分类调整。',
  latexTemplatesCount: '共 {n} 个模板',
  latexTemplatesCountFiltered: '筛选出 {n} 个',
  latexTemplatesCategoryStatus: '状态',
  latexTemplatesCategoryActions: '操作',
  latexTemplatesLoading: '正在加载…',
  latexTemplatesLoadFailed: '加载模板失败：{error}',
  latexTemplatesActionFailed: '操作失败：{error}',
  latexTemplatesSavedOk: '模板已保存。',
  latexTemplatesApprovedOk: '模板已审核通过并展示。',
  latexTemplatesRejectedOk: '模板已驳回。',
  latexTemplatesUnlistedOk: '模板已取消展示。',
  latexTemplatesDeletedOk: '模板已删除。',
  latexTemplatesUploadedOk: '模板已上传并展示。',
  latexTemplatesCategorySavedOk: '分类已保存。',
  latexTemplatesCategoryDeletedOk: '分类已删除。',
  latexTemplatesReasonRequired: '请先填写审核备注。',
});

tabMeta.latextemplates = ['latexTemplatesTitle', 'latexTemplatesDesc'];
TAB_ICONS.latextemplates = '<svg viewBox="0 0 24 24"><path d="M4 5.5A2.5 2.5 0 0 1 6.5 3H18v14.5H6.5A2.5 2.5 0 0 0 4 20z"></path><path d="M4 20a2.5 2.5 0 0 1 2.5-2.5H18V21H6.5"></path><path d="M8 7.5h6M8 11h4"></path></svg>';

// --- State ---

var latexTemplatesPageSize = 20;
var latexTemplatesState = {
  templates: [],
  categories: [],
  status: '',
  category: '',
  keyword: '',
  seq: 0,
  controller: null,
};

function latexTemplatesAPI(path, options) {
  if (typeof window.api !== 'function') throw new Error('Admin API is not ready.');
  return window.api(path, options || {});
}

// api() always sets a JSON content type, so multipart uploads go through a
// dedicated fetch that only supplies the bearer token.
async function latexTemplatesUploadRequest(url, formData) {
  const res = await fetch(url, {
    method: 'POST',
    headers: (typeof window.token === 'function' && window.token()) ? { Authorization: 'Bearer ' + window.token() } : {},
    body: formData,
  });
  const raw = await res.text();
  let data = {};
  try { data = raw ? JSON.parse(raw) : {}; } catch (_) { data = {}; }
  if (!res.ok) {
    const message = data.message || (typeof data.error === 'string' ? data.error : '') || raw || res.statusText;
    const err = new Error(message || 'request failed');
    err.status = res.status;
    err.code = data.code || '';
    throw err;
  }
  return data;
}

function latexTemplatesStatus(kind, message) {
  const el = document.getElementById('latexTemplatesStatus');
  if (!el) return;
  // An empty message means "clear": an empty status box still takes a row
  // between the page head and the template list.
  const text = String(message || '').trim();
  el.className = 'sm-status' + (kind && text ? ' show ' + kind : '');
  el.textContent = text;
}

function latexTemplatesBytes(bytes) {
  const value = Number(bytes || 0);
  if (!Number.isFinite(value) || value <= 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB'];
  let index = 0;
  let scaled = value;
  while (scaled >= 1024 && index < units.length - 1) { scaled /= 1024; index += 1; }
  return (index === 0 ? scaled : scaled.toFixed(1)) + ' ' + units[index];
}

function latexTemplatesDate(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return raw;
  const pad = part => String(part).padStart(2, '0');
  const monthDay = pad(parsed.getMonth() + 1) + '-' + pad(parsed.getDate());
  const clock = pad(parsed.getHours()) + ':' + pad(parsed.getMinutes());
  if (parsed.getFullYear() === new Date().getFullYear()) return monthDay + ' ' + clock;
  return parsed.getFullYear() + '-' + monthDay + ' ' + clock;
}

function latexTemplatesChip(label, value) {
  const text = String(value || '').trim();
  if (!text || text === '—') return '';
  return '<span title="' + escapeHtml(label) + '">' + escapeHtml(text) + '</span>';
}

function latexTemplatesCategoryOptions(selected) {
  return latexTemplatesState.categories
    .map(item => '<option value="' + escapeHtml(item.id) + '"' + (item.id === selected ? ' selected' : '') + '>' + escapeHtml(item.name) + '</option>')
    .join('');
}

function latexTemplatesStatusBadge(status) {
  const value = String(status || '').toLowerCase();
  const label = value === 'approved'
    ? tr('latexTemplatesStatusApproved')
    : value === 'rejected'
      ? tr('latexTemplatesStatusRejected')
      : tr('latexTemplatesStatusPending');
  // Reuse the expert-market status palette: "pending_review" is the amber
  // variant, and the other two names already carry their own colours.
  const variant = value === 'pending' ? 'pending_review' : value;
  return '<span class="expert-market-status expert-market-status-' + escapeHtml(variant) + '">' + escapeHtml(label) + '</span>';
}

function latexTemplatesActionSpecs(status) {
  const value = String(status || 'pending').toLowerCase();
  const specs = [];
  if (value !== 'approved') specs.push(['approve', 'btn-primary', 'latexTemplatesApprove']);
  if (value === 'pending') specs.push(['reject', 'btn-ghost', 'latexTemplatesReject']);
  if (value === 'approved') specs.push(['unlist', 'btn-ghost', 'latexTemplatesUnlist']);
  specs.push(['edit', 'btn-ghost', 'latexTemplatesApplyCategory']);
  specs.push(['delete', 'btn-danger', 'latexTemplatesDelete']);
  return specs;
}

function latexTemplatesRenderCard(item) {
  const id = escapeHtml(item.id);
  const status = String(item.status || 'pending').toLowerCase();
  const categoryName = (latexTemplatesState.categories.find(entry => entry.id === item.category_id) || {}).name || item.category_id || '';
  const description = String(item.description || '').trim();
  const reviewNote = String(item.review_note || '').trim();
  const meta = [
    latexTemplatesChip(tr('latexTemplatesUploadCategory'), categoryName),
    latexTemplatesChip(tr('latexTemplatesVersion'), item.version),
    latexTemplatesChip(tr('latexTemplatesSize'), latexTemplatesBytes(item.size_bytes)),
    latexTemplatesChip(tr('latexTemplatesFiles'), tr('latexTemplatesFileCount', { n: String(item.file_count || 0) })),
    latexTemplatesChip(tr('latexTemplatesUpdated'), latexTemplatesDate(item.updated_at)),
    latexTemplatesChip(tr('latexTemplatesAuthor'), item.author),
    latexTemplatesChip(tr('latexTemplatesSubmitter'), item.owner_email),
  ].join('');
  const actions = latexTemplatesActionSpecs(status)
    .map(entry => '<button type="button" class="' + entry[1] + '" data-latex-action="' + entry[0] + '">' + escapeHtml(tr(entry[2])) + '</button>').join('');
  // One scan line plus a control line. Labels live on title/aria so the row
  // stays short enough to review a queue without scrolling past empty fields.
  return '<article class="latex-row" data-latex-id="' + id + '" data-latex-status="' + escapeHtml(status) + '">'
    + '<div class="latex-row-top">'
    + latexTemplatesStatusBadge(item.status)
    + '<strong title="' + escapeHtml(item.name) + '">' + escapeHtml(item.name) + '</strong>'
    + (meta ? '<div class="latex-row-meta">' + meta + '</div>' : '')
    + '</div>'
    + (description ? '<p class="latex-row-desc" title="' + escapeHtml(description) + '">' + escapeHtml(description) + '</p>' : '')
    + (reviewNote ? '<p class="latex-row-desc has-note" title="' + escapeHtml(reviewNote) + '">' + escapeHtml(tr('latexTemplatesReason') + ': ' + reviewNote) + '</p>' : '')
    + '<div class="latex-row-controls">'
    + '<input type="text" maxlength="2048" data-latex-reason aria-label="' + escapeHtml(tr('latexTemplatesReason')) + '" title="' + escapeHtml(tr('latexTemplatesReasonHint')) + '" placeholder="' + escapeHtml(tr('latexTemplatesReasonPlaceholder')) + '">'
    + '<select data-latex-category aria-label="' + escapeHtml(tr('latexTemplatesMoveTo')) + '">' + latexTemplatesCategoryOptions(item.category_id) + '</select>'
    + '<div class="latex-row-actions">' + actions + '</div>'
    + '</div>'
    + '</article>';
}

function latexTemplatesRenderCategories() {
  const root = document.getElementById('latexTemplatesCategoryList');
  if (!root) return;
  if (!latexTemplatesState.categories.length) {
    root.innerHTML = '<div class="hint">' + escapeHtml(tr('latexTemplatesEmptyCategories')) + '</div>';
    return;
  }
  // One editing row per category keeps the whole list scannable; the previous
  // card-per-category layout pushed the template list far below the fold.
  const header = '<div class="latex-category-head" aria-hidden="true">'
    + '<span>' + escapeHtml(tr('latexTemplatesCategoryName')) + '</span>'
    + '<span>' + escapeHtml(tr('latexTemplatesCategoryDescription')) + '</span>'
    + '<span>' + escapeHtml(tr('latexTemplatesCategoryOrder')) + '</span>'
    + '<span>' + escapeHtml(tr('latexTemplatesCategoryStatus')) + '</span>'
    + '<span>' + escapeHtml(tr('latexTemplatesCategoryActions')) + '</span>'
    + '</div>';
  const rows = latexTemplatesState.categories.map(item => {
    const id = escapeHtml(item.id);
    const active = String(item.status || 'active') === 'active';
    const nameLabel = escapeHtml(tr('latexTemplatesCategoryName'));
    const descLabel = escapeHtml(tr('latexTemplatesCategoryDescription'));
    const orderLabel = escapeHtml(tr('latexTemplatesCategoryOrder'));
    const actions = '<button type="button" class="btn-secondary" data-latex-category-action="save">' + escapeHtml(tr('latexTemplatesCategorySave')) + '</button>'
      + (item.builtin ? '' : '<button type="button" class="btn-ghost" data-latex-category-action="toggle">' + escapeHtml(tr(active ? 'latexTemplatesToggleDisabled' : 'latexTemplatesToggleActive')) + '</button>'
        + '<button type="button" class="btn-ghost" data-latex-category-action="delete">' + escapeHtml(tr('latexTemplatesCategoryDelete')) + '</button>');
    return '<div class="latex-category-row" data-latex-category-id="' + id + '">'
      + '<input type="text" maxlength="40" data-latex-category-name value="' + escapeAttr(item.name) + '" aria-label="' + nameLabel + '">'
      + '<input type="text" maxlength="200" data-latex-category-description value="' + escapeAttr(item.description || '') + '" aria-label="' + descLabel + '" placeholder="' + descLabel + '">'
      + '<input type="number" data-latex-category-order value="' + escapeAttr(String(item.sort_order || 0)) + '" aria-label="' + orderLabel + '">'
      + '<span class="latex-category-state">'
      + (item.builtin ? '<span class="latex-category-tag">' + escapeHtml(tr('latexTemplatesCategoryBuiltin')) + '</span>' : '')
      + '<span class="latex-category-tag ' + (active ? 'is-on' : 'is-off') + '">' + escapeHtml(tr(active ? 'latexTemplatesActive' : 'latexTemplatesDisabled')) + '</span>'
      + '</span>'
      + '<div class="latex-category-actions">' + actions + '</div>'
      + '</div>';
  }).join('');
  root.innerHTML = header + rows;
}

function latexTemplatesRenderUploadCategories() {
  const select = document.getElementById('latexTemplatesUploadCategory');
  if (!select) return;
  const current = select.value;
  select.innerHTML = latexTemplatesCategoryOptions(current);
}

function latexTemplatesRenderCount() {
  const el = document.getElementById('latexTemplatesCount');
  if (!el) return;
  const total = latexTemplatesState.templates.length;
  const filtered = !!(latexTemplatesState.status || latexTemplatesState.category || latexTemplatesState.keyword);
  el.textContent = total ? tr(filtered ? 'latexTemplatesCountFiltered' : 'latexTemplatesCount', { n: total }) : '';
  el.classList.toggle('hidden', !total);
}

async function loadLatexTemplatesAdmin(options) {
  const grid = document.getElementById('latexTemplatesList');
  if (!grid) return;
  const opts = options || {};
  const statusEl = document.getElementById('latexTemplatesStatusFilter');
  const categoryEl = document.getElementById('latexTemplatesCategoryFilter');
  const keywordEl = document.getElementById('latexTemplatesKeyword');
  latexTemplatesState.status = String((statusEl && statusEl.value) || latexTemplatesState.status || '');
  latexTemplatesState.category = String((categoryEl && categoryEl.value) || latexTemplatesState.category || '');
  latexTemplatesState.keyword = String((keywordEl && keywordEl.value) || latexTemplatesState.keyword || '').trim();
  const query = new URLSearchParams();
  if (latexTemplatesState.status) query.set('status', latexTemplatesState.status);
  if (latexTemplatesState.category) query.set('category', latexTemplatesState.category);
  if (latexTemplatesState.keyword) query.set('keyword', latexTemplatesState.keyword);
  const sequence = ++latexTemplatesState.seq;
  if (latexTemplatesState.controller) latexTemplatesState.controller.abort();
  const controller = new AbortController();
  latexTemplatesState.controller = controller;
  grid.setAttribute('aria-busy', 'true');
  grid.innerHTML = '<div class="hint">' + escapeHtml(tr('latexTemplatesLoading')) + '</div>';
  latexTemplatesStatus('loading', tr('latexTemplatesLoading'));
  try {
    const data = await latexTemplatesAPI('/api/admin/latex-templates?' + query.toString(), { signal: controller.signal });
    if (sequence !== latexTemplatesState.seq) return;
    latexTemplatesState.templates = Array.isArray(data && data.templates) ? data.templates : [];
    latexTemplatesState.categories = Array.isArray(data && data.categories) ? data.categories : [];
    const filterCategory = document.getElementById('latexTemplatesCategoryFilter');
    if (filterCategory) {
      const current = filterCategory.value;
      filterCategory.innerHTML = '<option value="">' + escapeHtml(tr('latexTemplatesCategoryAll')) + '</option>' + latexTemplatesCategoryOptions(current);
    }
    latexTemplatesRenderCategories();
    latexTemplatesRenderUploadCategories();
    latexTemplatesRenderCount();
    grid.innerHTML = latexTemplatesState.templates.length
      ? latexTemplatesState.templates.map(latexTemplatesRenderCard).join('')
      : '<div class="hint">' + escapeHtml(tr('latexTemplatesEmpty')) + '</div>';
    grid.setAttribute('aria-busy', 'false');
    latexTemplatesStatus('ok', '');
  } catch (err) {
    if (err && err.name === 'AbortError') return;
    if (sequence !== latexTemplatesState.seq) return;
    const message = tr('latexTemplatesLoadFailed', { error: (err && err.message) || String(err) });
    grid.innerHTML = '<div class="hint">' + escapeHtml(message) + '</div>';
    grid.setAttribute('aria-busy', 'false');
    latexTemplatesRenderCount();
    latexTemplatesStatus('error', message);
  } finally {
    if (latexTemplatesState.controller === controller) latexTemplatesState.controller = null;
  }
}

async function latexTemplatesRun(button, work) {
  if (!button || button.disabled) return;
  button.disabled = true;
  try {
    await work();
  } catch (err) {
    latexTemplatesStatus('error', tr('latexTemplatesActionFailed', { error: (err && err.message) || String(err) }));
  } finally {
    button.disabled = false;
  }
}

async function latexTemplatesAdminAction(card, button) {
  const action = button && button.dataset.latexAction;
  const id = card && card.dataset.latexId;
  if (!action || !id) return;
  const reasonField = card.querySelector('[data-latex-reason]');
  const categoryField = card.querySelector('[data-latex-category]');
  const reason = String((reasonField && reasonField.value) || '').trim();
  const categoryId = String((categoryField && categoryField.value) || '').trim();
  await latexTemplatesRun(button, async () => {
    if (action === 'delete') {
      if (!window.confirm(tr('latexTemplatesDeleteConfirm'))) return;
      await latexTemplatesAPI('/api/admin/latex-templates/' + encodeURIComponent(id), { method: 'DELETE' });
      showToast(tr('latexTemplatesDeletedOk'), 'ok');
      await loadLatexTemplatesAdmin();
      return;
    }
    if (action === 'edit') {
      const current = String((latexTemplatesState.templates.find(entry => entry.id === id) || {}).category_id || '');
      // The select always shows the current category, so a click with no
      // change used to report success and wipe the note the operator had typed.
      if (!categoryId || categoryId === current) {
        latexTemplatesStatus('error', tr('latexTemplatesCategoryUnchanged'));
        if (categoryField) categoryField.focus();
        return;
      }
      const body = { category_id: categoryId };
      if (reason) body.reason = reason;
      await latexTemplatesAPI('/api/admin/latex-templates/' + encodeURIComponent(id), { method: 'PATCH', body: JSON.stringify(body) });
      showToast(tr('latexTemplatesSavedOk'), 'ok');
      await loadLatexTemplatesAdmin();
      return;
    }
    if (!reason) {
      latexTemplatesStatus('error', tr('latexTemplatesReasonRequired'));
      if (reasonField) reasonField.focus();
      return;
    }
    const body = { action: action, reason: reason };
    if (categoryId) body.category_id = categoryId;
    await latexTemplatesAPI('/api/admin/latex-templates/' + encodeURIComponent(id) + '/review', { method: 'POST', body: JSON.stringify(body) });
    showToast(action === 'approve' ? tr('latexTemplatesApprovedOk') : action === 'unlist' ? tr('latexTemplatesUnlistedOk') : tr('latexTemplatesRejectedOk'), 'ok');
    await loadLatexTemplatesAdmin();
  });
}

async function submitLatexTemplateUpload(button) {
  const fileInput = document.getElementById('latexTemplatesUploadZip');
  const file = fileInput && fileInput.files && fileInput.files[0];
  if (!file) {
    latexTemplatesStatus('error', tr('latexTemplatesUploadPickFile'));
    return;
  }
  const form = new FormData();
  form.append('zip', file, file.name);
  form.append('name', String((document.getElementById('latexTemplatesUploadName') || {}).value || '').trim());
  form.append('description', String((document.getElementById('latexTemplatesUploadDescription') || {}).value || '').trim());
  form.append('category_id', String((document.getElementById('latexTemplatesUploadCategory') || {}).value || '').trim());
  form.append('version', String((document.getElementById('latexTemplatesUploadVersion') || {}).value || '').trim());
  form.append('author', String((document.getElementById('latexTemplatesUploadAuthor') || {}).value || '').trim());
  form.append('review_note', String((document.getElementById('latexTemplatesUploadNote') || {}).value || '').trim());
  await latexTemplatesRun(button, async () => {
    await latexTemplatesUploadRequest('/api/admin/latex-templates', form);
    showToast(tr('latexTemplatesUploadedOk'), 'ok');
    // Clear the per-template fields but keep category/version/author so an
    // operator uploading a batch of packages does not retype them each time.
    ['latexTemplatesUploadZip', 'latexTemplatesUploadName', 'latexTemplatesUploadDescription', 'latexTemplatesUploadNote'].forEach(fieldId => {
      const field = document.getElementById(fieldId);
      if (field) field.value = '';
    });
    await loadLatexTemplatesAdmin();
  });
}

async function addLatexTemplateCategory(button) {
  const name = String((document.getElementById('latexTemplatesNewCategoryName') || {}).value || '').trim();
  if (!name) {
    latexTemplatesStatus('error', tr('latexTemplatesCategoryNameRequired'));
    return;
  }
  await latexTemplatesRun(button, async () => {
    await latexTemplatesAPI('/api/admin/latex-template-categories', {
      method: 'POST',
      body: JSON.stringify({
        name: name,
        description: String((document.getElementById('latexTemplatesNewCategoryDescription') || {}).value || '').trim(),
      }),
    });
    const nameField = document.getElementById('latexTemplatesNewCategoryName');
    const descriptionField = document.getElementById('latexTemplatesNewCategoryDescription');
    if (nameField) nameField.value = '';
    if (descriptionField) descriptionField.value = '';
    showToast(tr('latexTemplatesCategorySavedOk'), 'ok');
    await loadLatexTemplatesAdmin();
  });
}

async function latexTemplatesCategoryAction(row, button) {
  const action = button && button.dataset.latexCategoryAction;
  const id = row && row.dataset.latexCategoryId;
  if (!action || !id) return;
  const name = String((row.querySelector('[data-latex-category-name]') || {}).value || '').trim();
  const description = String((row.querySelector('[data-latex-category-description]') || {}).value || '').trim();
  const order = Number((row.querySelector('[data-latex-category-order]') || {}).value || 0);
  await latexTemplatesRun(button, async () => {
    if (action === 'delete') {
      if (!window.confirm(tr('latexTemplatesCategoryDeleteConfirm'))) return;
      // The reason lands in the audit trail, so it describes the operation
      // rather than echoing the label back.
      const reason = tr('latexTemplatesCategoryRemoved', { name: name || id });
      await latexTemplatesAPI('/api/admin/latex-template-categories/' + encodeURIComponent(id) + '?reason=' + encodeURIComponent(reason), { method: 'DELETE' });
      showToast(tr('latexTemplatesCategoryDeletedOk'), 'ok');
      await loadLatexTemplatesAdmin();
      return;
    }
    if (action === 'toggle') {
      const current = latexTemplatesState.categories.find(entry => entry.id === id);
      const next = current && current.status === 'active' ? 'disabled' : 'active';
      await latexTemplatesAPI('/api/admin/latex-template-categories/' + encodeURIComponent(id), { method: 'PATCH', body: JSON.stringify({ status: next }) });
      await loadLatexTemplatesAdmin();
      return;
    }
    if (!name) {
      latexTemplatesStatus('error', tr('latexTemplatesCategoryNameRequired'));
      return;
    }
    await latexTemplatesAPI('/api/admin/latex-template-categories/' + encodeURIComponent(id), {
      method: 'PATCH',
      body: JSON.stringify({ name: name, description: description, sort_order: Number.isFinite(order) ? order : 0 }),
    });
    showToast(tr('latexTemplatesCategorySavedOk'), 'ok');
    await loadLatexTemplatesAdmin();
  });
}

function applyLatexTemplateAdminI18n() {
  document.querySelectorAll('[data-latex-i18n]').forEach(el => {
    const key = el.getAttribute('data-latex-i18n');
    if (key) el.textContent = tr(key);
  });
  document.querySelectorAll('[data-latex-placeholder]').forEach(el => {
    const key = el.getAttribute('data-latex-placeholder');
    if (key) el.setAttribute('placeholder', tr(key));
  });
  document.querySelectorAll('[data-latex-aria]').forEach(el => {
    const key = el.getAttribute('data-latex-aria');
    if (key) el.setAttribute('aria-label', tr(key));
  });
  const title = document.getElementById('latexTemplatesPanelTitle');
  if (title) title.textContent = tr('latexTemplatesTitle');
  const desc = document.getElementById('latexTemplatesPanelDesc');
  if (desc) desc.textContent = tr('latexTemplatesDesc');
}

function ensureLatexTemplateAdminPanel() {
  if (document.getElementById('tab-latextemplates')) return;
  const panel = document.createElement('section');
  panel.id = 'tab-latextemplates';
  panel.className = 'panel card latex-admin';
  panel.innerHTML =
    '<div class="head">'
    + '<div><h3 id="latexTemplatesPanelTitle"></h3><div class="desc" id="latexTemplatesPanelDesc"></div></div>'
    + '<div class="actions">'
    + '<button type="button" class="btn-ghost" onclick="loadLatexTemplatesAdmin()" data-latex-i18n="latexTemplatesRefresh"></button>'
    + '</div></div>'
    + '<div id="latexTemplatesStatus" class="sm-status" aria-live="polite"></div>'
    + '<div class="latex-list-bar">'
    + '<div class="latex-list-title"><span data-latex-i18n="latexTemplatesListTitle"></span><span id="latexTemplatesCount" class="latex-count hidden"></span></div>'
    + '<p class="sr-only" data-latex-i18n="latexTemplatesListDesc"></p>'
    + '<div class="latex-toolbar">'
    + '<label class="sr-only" for="latexTemplatesKeyword" data-latex-i18n="latexTemplatesSearchPlaceholder"></label>'
    + '<input id="latexTemplatesKeyword" type="search" maxlength="100" placeholder="" data-latex-placeholder="latexTemplatesSearchPlaceholder">'
    + '<label class="sr-only" for="latexTemplatesStatusFilter" data-latex-i18n="latexTemplatesStatus"></label>'
    + '<select id="latexTemplatesStatusFilter">'
    + '<option value="" data-latex-i18n="latexTemplatesStatusAll"></option>'
    + '<option value="pending" data-latex-i18n="latexTemplatesStatusPending"></option>'
    + '<option value="approved" data-latex-i18n="latexTemplatesStatusApproved"></option>'
    + '<option value="rejected" data-latex-i18n="latexTemplatesStatusRejected"></option>'
    + '</select>'
    + '<label class="sr-only" for="latexTemplatesCategoryFilter" data-latex-i18n="latexTemplatesCategoryAll"></label>'
    + '<select id="latexTemplatesCategoryFilter"><option value="" data-latex-i18n="latexTemplatesCategoryAll"></option></select>'
    + '<button type="button" class="btn-secondary" onclick="loadLatexTemplatesAdmin()" data-latex-i18n="latexTemplatesSearch"></button>'
    + '</div></div>'
    + '<div id="latexTemplatesList" class="latex-list" aria-live="polite"></div>'
    + '<details class="latex-fold">'
    + '<summary><span data-latex-i18n="latexTemplatesUploadTitle"></span><span class="latex-fold-hint" data-latex-i18n="latexTemplatesUploadHint"></span></summary>'
    + '<div class="latex-upload-card"><div class="latex-form-grid">'
    + '<div class="latex-field latex-field-span2"><label for="latexTemplatesUploadZip" data-latex-i18n="latexTemplatesUpload"></label><input id="latexTemplatesUploadZip" type="file" accept=".zip,application/zip"></div>'
    + '<div class="latex-field"><label for="latexTemplatesUploadCategory" data-latex-i18n="latexTemplatesUploadCategory"></label><select id="latexTemplatesUploadCategory"></select></div>'
    + '<div class="latex-field"><label for="latexTemplatesUploadVersion" data-latex-i18n="latexTemplatesUploadVersion"></label><input id="latexTemplatesUploadVersion" type="text" maxlength="32"></div>'
    + '<div class="latex-field"><label for="latexTemplatesUploadName" data-latex-i18n="latexTemplatesUploadName"></label><input id="latexTemplatesUploadName" type="text" maxlength="80"></div>'
    + '<div class="latex-field"><label for="latexTemplatesUploadAuthor" data-latex-i18n="latexTemplatesUploadAuthor"></label><input id="latexTemplatesUploadAuthor" type="text" maxlength="120"></div>'
    + '<div class="latex-field latex-field-span2"><label for="latexTemplatesUploadNote" data-latex-i18n="latexTemplatesUploadNote"></label><input id="latexTemplatesUploadNote" type="text" maxlength="2048"></div>'
    + '<div class="latex-field latex-field-span4"><label for="latexTemplatesUploadDescription" data-latex-i18n="latexTemplatesUploadDescription"></label><textarea id="latexTemplatesUploadDescription" rows="3" maxlength="2000"></textarea></div>'
    + '<div class="latex-field latex-field-end"><button type="button" class="btn-primary" onclick="submitLatexTemplateUpload(this)" data-latex-i18n="latexTemplatesUploadSubmit"></button></div>'
    + '</div></div>'
    + '</details>'
    + '<details class="latex-fold">'
    + '<summary><span data-latex-i18n="latexTemplatesCategoriesTitle"></span><span class="latex-fold-hint" data-latex-i18n="latexTemplatesCategoriesDesc"></span></summary>'
    + '<div class="latex-fold-body">'
    + '<div class="latex-category-add">'
    + '<input id="latexTemplatesNewCategoryName" type="text" maxlength="40" placeholder="" data-latex-placeholder="latexTemplatesCategoryName" aria-label="" data-latex-aria="latexTemplatesCategoryName">'
    + '<input id="latexTemplatesNewCategoryDescription" type="text" maxlength="200" placeholder="" data-latex-placeholder="latexTemplatesCategoryDescription" aria-label="" data-latex-aria="latexTemplatesCategoryDescription">'
    + '<button type="button" class="btn-primary" onclick="addLatexTemplateCategory(this)" data-latex-i18n="latexTemplatesCategoryAdd"></button>'
    + '</div>'
    + '<div id="latexTemplatesCategoryList" class="latex-category-list"></div>'
    + '</div></details>';
  const main = document.querySelector('main.main') || document.querySelector('.main');
  if (main) main.appendChild(panel);

  const grid = document.getElementById('latexTemplatesList');
  if (grid && grid.dataset.latexBound !== 'true') {
    grid.dataset.latexBound = 'true';
    grid.addEventListener('click', event => {
      const button = event.target.closest('[data-latex-action]');
      const card = button && button.closest('[data-latex-id]');
      if (button && card) void latexTemplatesAdminAction(card, button);
    });
  }
  const categoryList = document.getElementById('latexTemplatesCategoryList');
  if (categoryList && categoryList.dataset.latexBound !== 'true') {
    categoryList.dataset.latexBound = 'true';
    categoryList.addEventListener('click', event => {
      const button = event.target.closest('[data-latex-category-action]');
      const row = button && button.closest('[data-latex-category-id]');
      if (button && row) void latexTemplatesCategoryAction(row, button);
    });
  }
  const statusFilter = document.getElementById('latexTemplatesStatusFilter');
  if (statusFilter) statusFilter.addEventListener('change', () => void loadLatexTemplatesAdmin());
  const categoryFilter = document.getElementById('latexTemplatesCategoryFilter');
  if (categoryFilter) categoryFilter.addEventListener('change', () => void loadLatexTemplatesAdmin());
  const keyword = document.getElementById('latexTemplatesKeyword');
  if (keyword) {
    keyword.addEventListener('keydown', event => {
      if (event.key === 'Enter') void loadLatexTemplatesAdmin();
    });
  }
  applyLatexTemplateAdminI18n();
}

function ensureLatexTemplateAdminNav() {
  if (document.querySelector('.nav button[data-tab="latextemplates"]')) return;
  // The product groups the LaTeX library under 能力, which is the admin
  // console's "market" nav group (能力目录 / 能力市场 live there too).
  const group = document.querySelector('.nav .nav-group[data-nav-group="market"]')
    || document.querySelector('.nav .nav-group')
    || document.querySelector('.nav');
  if (!group) return;
  const button = document.createElement('button');
  button.type = 'button';
  button.dataset.tab = 'latextemplates';
  button.innerHTML = '<span class="nav-icon" aria-hidden="true">'
    + (typeof TAB_ICONS === 'object' && TAB_ICONS.latextemplates ? TAB_ICONS.latextemplates : '')
    + '</span><span data-latex-i18n="navLatexTemplates"></span>'
    + '<small data-latex-i18n="navLatexTemplatesDesc"></small>';
  button.addEventListener('click', () => {
    if (typeof window.openTab === 'function') window.openTab('latextemplates');
  });
  group.appendChild(button);
  applyLatexTemplateAdminI18n();
  if (typeof window.syncNavGroups === 'function') window.syncNavGroups();
}

(function initLatexTemplateAdmin() {
  var originalOpenTab = window.openTab;
  if (typeof originalOpenTab === 'function' && !window._latexOrigOpenTab) {
    window._latexOrigOpenTab = originalOpenTab;
    window.openTab = function (name) {
      // Shadow key: admin-core's restoreTab() runs during its own script
      // evaluation, before this module loads, and its openTab() silently
      // rewrites the active-tab key to 'overview' when the panel is still
      // missing. Record the requested tab where nothing overwrites it.
      try { localStorage.setItem('maclawHubCenterLatexLastTab', String(name)); } catch (_) {}
      if (name === 'latextemplates') ensureLatexTemplateAdminPanel();
      var result = originalOpenTab.apply(this, arguments);
      if (name === 'latextemplates') void loadLatexTemplatesAdmin();
      return result;
    };
  }
  var originalSetLanguage = window.setLanguage;
  if (typeof originalSetLanguage === 'function' && !window._latexOrigSetLanguage) {
    window._latexOrigSetLanguage = originalSetLanguage;
    window.setLanguage = function () {
      var result = originalSetLanguage.apply(this, arguments);
      applyLatexTemplateAdminI18n();
      return result;
    };
  }
  function build() {
    ensureLatexTemplateAdminPanel();
    ensureLatexTemplateAdminNav();
    // Restore the tab from the shadow key. Only when the user is signed in —
    // otherwise the hidden panel would fire an unauthenticated data load.
    var last = null;
    try { last = localStorage.getItem('maclawHubCenterLatexLastTab'); } catch (_) {}
    var signedIn = typeof window.token === 'function' && !!window.token();
    if (last === 'latextemplates' && signedIn && typeof window.openTab === 'function') {
      window.openTab('latextemplates');
    }
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', build);
  } else {
    build();
  }
})();

window.loadLatexTemplatesAdmin = loadLatexTemplatesAdmin;
window.submitLatexTemplateUpload = submitLatexTemplateUpload;
window.addLatexTemplateCategory = addLatexTemplateCategory;
window.applyLatexTemplateAdminI18n = applyLatexTemplateAdminI18n;
