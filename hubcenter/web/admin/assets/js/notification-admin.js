// HubCenter Admin — Notification Management Module
// Provides cross-Hub notification creation, listing, cascade status monitoring, and revocation.
// API endpoints:
//   POST   /api/v1/admin/notifications         — create cross-Hub notification
//   GET    /api/v1/admin/notifications         — list with cascade status
//   GET    /api/v1/admin/notifications/{id}    — detail with per-Hub delivery status
//   POST   /api/v1/admin/notifications/{id}/revoke — revoke (cascade)

// --- i18n ---
Object.assign(I18N_EN, {
  navNotifications: 'Notifications',
  navNotificationsDesc: 'Cross-Hub push notifications management',
  notifTitle: 'Notification Management',
  notifDesc: 'Create, monitor, and revoke cross-Hub push notifications to end users.',
  notifRefresh: 'Refresh',
  notifCreate: 'Create Notification',
  notifFilterAll: 'All',
  notifFilterPublished: 'Published',
  notifFilterExpired: 'Expired',
  notifFilterRevoked: 'Revoked',
  notifFilterDraft: 'Draft',
  notifEmpty: 'No notifications yet.',
  notifLoading: 'Loading notifications...',
  notifLoadFailed: 'Load notifications failed: {error}',
  notifStatusPublished: 'Published',
  notifStatusExpired: 'Expired',
  notifStatusRevoked: 'Revoked',
  notifStatusDraft: 'Draft',
  notifCategorySystem: 'System',
  notifCategoryFeature: 'Feature',
  notifCategorySecurity: 'Security',
  notifCategoryMaintenance: 'Maintenance',
  notifCategoryCustom: 'Custom',
  notifPriorityNormal: 'Normal',
  notifPriorityImportant: 'Important',
  notifPriorityUrgent: 'Urgent',
  notifAudienceAllHubs: 'All Hubs (Broadcast)',
  notifAudienceHubs: 'Selected Hubs',
  notifAudienceTenant: 'Hub + Tenant',
  notifFormTitle: 'Title',
  notifFormContent: 'Content (Markdown)',
  notifFormCategory: 'Category',
  notifFormAudience: 'Audience',
  notifFormPriority: 'Priority',
  notifFormPublishAt: 'Publish at',
  notifFormExpireAt: 'Expire at',
  notifFormImPush: 'Also push to IM channels',
  notifFormPublishNow: 'Publish Now',
  notifFormSaveDraft: 'Save Draft',
  notifFormCancel: 'Cancel',
  notifFormTitlePlaceholder: 'Notification title (max 100 chars)',
  notifFormContentPlaceholder: 'Notification content in Markdown (max 2000 chars)',
  notifFormSelectHubs: 'Select Hub instances...',
  notifFormSelectTenant: 'Select Hub and Tenant...',
  notifFormHub: 'Hub',
  notifFormTenant: 'Tenant',
  notifFormAddTenant: 'Add Target',
  notifFormSelectedTenants: 'Selected targets',
  notifFormNoHubs: 'No hubs available',
  notifFormNoTenants: 'No tenants available',
  notifFormTenantAlreadyAdded: 'This Hub + Tenant target is already selected.',
  notifFormRemoveTenant: 'Remove',
  notifFormImmediate: 'Immediate',
  notifFormScheduled: 'Scheduled',
  notifRevoke: 'Revoke',
  notifRevokeConfirm: 'Revoke this notification? It will be removed from all clients.',
  notifRevoked: 'Notification revoked successfully.',
  notifRevokeFailed: 'Revoke failed: {error}',
  notifDelete: 'Delete',
  notifDeleteConfirm: 'Permanently delete this notification? Published notifications must be revoked first.',
  notifDeleted: 'Notification deleted successfully.',
  notifDeleteFailed: 'Delete failed: {error}',
  notifCreated: 'Notification created successfully.',
  notifCreateFailed: 'Create notification failed: {error}',
  notifCascadeTitle: 'Cascade Push Status',
  notifCascadeHub: 'Hub',
  notifCascadeTime: 'Push Time',
  notifCascadeStatus: 'Status',
  notifCascadeSuccess: 'Success',
  notifCascadeFailed: 'Failed',
  notifCascadePending: 'Pending',
  notifDetailBack: '← Back to list',
  notifDetailStats: 'Delivery Statistics',
  notifDetailTotal: 'Total Pushed',
  notifDetailRead: 'Read',
  notifDetailReadRate: 'Read Rate',
  notifValidationTitle: 'Title is required (max 100 chars)',
  notifValidationContent: 'Content is required (max 2000 chars)',
  notifValidationAudience: 'Please select audience',
});
Object.assign(I18N_ZH, {
  navNotifications: '通知管理',
  navNotificationsDesc: '跨 Hub 推送通知管理',
  notifTitle: '通知管理',
  notifDesc: '创建、监控和撤回跨 Hub 推送通知。',
  notifRefresh: '刷新',
  notifCreate: '创建通知',
  notifFilterAll: '全部',
  notifFilterPublished: '已发布',
  notifFilterExpired: '已过期',
  notifFilterRevoked: '已撤回',
  notifFilterDraft: '草稿',
  notifEmpty: '暂无通知。',
  notifLoading: '正在加载通知...',
  notifLoadFailed: '加载通知失败：{error}',
  notifStatusPublished: '已发布',
  notifStatusExpired: '已过期',
  notifStatusRevoked: '已撤回',
  notifStatusDraft: '草稿',
  notifCategorySystem: '系统公告',
  notifCategoryFeature: '功能更新',
  notifCategorySecurity: '安全告警',
  notifCategoryMaintenance: '运维通知',
  notifCategoryCustom: '自定义',
  notifPriorityNormal: '普通',
  notifPriorityImportant: '重要',
  notifPriorityUrgent: '紧急',
  notifAudienceAllHubs: '全网广播（所有 Hub）',
  notifAudienceHubs: '指定 Hub',
  notifAudienceTenant: 'Hub + 租户',
  notifFormTitle: '标题',
  notifFormContent: '内容（Markdown）',
  notifFormCategory: '分类',
  notifFormAudience: '受众范围',
  notifFormPriority: '优先级',
  notifFormPublishAt: '发布时间',
  notifFormExpireAt: '过期时间',
  notifFormImPush: '同时推送到 IM 通道',
  notifFormPublishNow: '立即发布',
  notifFormSaveDraft: '保存草稿',
  notifFormCancel: '取消',
  notifFormTitlePlaceholder: '通知标题（最多 100 字符）',
  notifFormContentPlaceholder: '通知内容，Markdown 格式（最多 2000 字符）',
  notifFormSelectHubs: '选择 Hub 实例...',
  notifFormSelectTenant: '选择 Hub 和租户...',
  notifFormHub: 'Hub',
  notifFormTenant: '租户',
  notifFormAddTenant: '添加目标',
  notifFormSelectedTenants: '已选目标',
  notifFormNoHubs: '暂无可选 Hub',
  notifFormNoTenants: '暂无可选租户',
  notifFormTenantAlreadyAdded: '该 Hub + 租户已选择。',
  notifFormRemoveTenant: '移除',
  notifFormImmediate: '立即',
  notifFormScheduled: '定时',
  notifRevoke: '撤回',
  notifRevokeConfirm: '确认撤回此通知？撤回后将从所有客户端移除。',
  notifRevoked: '通知已成功撤回。',
  notifRevokeFailed: '撤回失败：{error}',
  notifDelete: '删除',
  notifDeleteConfirm: '确认永久删除此通知？已发布通知需要先撤回。',
  notifDeleted: '通知已删除。',
  notifDeleteFailed: '删除失败：{error}',
  notifCreated: '通知创建成功。',
  notifCreateFailed: '创建通知失败：{error}',
  notifCascadeTitle: '级联推送状态',
  notifCascadeHub: 'Hub 名称',
  notifCascadeTime: '推送时间',
  notifCascadeStatus: '状态',
  notifCascadeSuccess: '成功',
  notifCascadeFailed: '失败',
  notifCascadePending: '待推送',
  notifDetailBack: '← 返回列表',
  notifDetailStats: '送达统计',
  notifDetailTotal: '总推送数',
  notifDetailRead: '已读数',
  notifDetailReadRate: '已读率',
  notifValidationTitle: '标题不能为空（最多 100 字符）',
  notifValidationContent: '内容不能为空（最多 2000 字符）',
  notifValidationAudience: '请选择受众范围',
});

// --- Tab metadata ---
tabMeta.notifications = ['notifTitle', 'notifDesc'];
TAB_ICONS.notifications = '<svg viewBox="0 0 24 24"><path d="M12 2a7 7 0 0 0-7 7c0 3.53-.8 5.74-1.64 7.13A1 1 0 0 0 4.2 18h15.6a1 1 0 0 0 .84-1.87C19.8 14.74 19 12.53 19 9a7 7 0 0 0-7-7Z"></path><path d="M9.1 19a3 3 0 0 0 5.8 0"></path></svg>';

// --- State ---
var notifPage = 1;
var notifStatusFilter = '';
var notifViewMode = 'list'; // 'list' | 'create' | 'detail'
var notifCurrentId = null;
var notifHubListCache = null;
var notifTenantTargets = [];

// --- API helpers ---
function notifApiGet(path) {
  return api(path);
}
function notifApiPost(path, body) {
  return api(path, { method: 'POST', body: JSON.stringify(body || {}) });
}
function notifApiDelete(path) {
  return api(path, { method: 'DELETE' });
}

function notifToast(message, type) {
  if (typeof showToast === 'function') showToast(message, type === 'danger' ? 'error' : type);
  else alert(message);
}

function notifJsArg(value) {
  return escapeHtml(JSON.stringify(String(value || '')));
}

function notifCanDelete(n) {
  return !!n && (n.status === 'revoked' || n.status === 'draft' || n.status === 'expired');
}

function notifUnwrapItem(item) {
  if (!item) return null;
  if (item.notification) {
    var n = item.notification;
    n.cascade_results = item.cascade_results || n.cascade_results || [];
    return n;
  }
  return item;
}

function notifUnwrapList(data) {
  var raw = Array.isArray(data) ? data : (data && (data.items || data.notifications) || []);
  return raw.map(notifUnwrapItem).filter(Boolean);
}

function notifUnwrapDetail(data) {
  var n = notifUnwrapItem(data);
  if (data && data.notification) {
    n = data.notification;
    n.cascade_results = data.cascade_results || n.cascade_results || [];
  }
  return n || {};
}

// --- Load hub list for audience selector ---
function notifLoadHubList() {
  if (notifHubListCache) return Promise.resolve(notifHubListCache);
  return notifApiGet('/api/admin/hubs').then(function(data) {
    notifHubListCache = Array.isArray(data) ? data : (data.hubs || []);
    return notifHubListCache;
  }).catch(function() { return []; });
}

function notifHubID(hub) {
  return String(hub && (hub.id || hub.hub_id) || '');
}

function notifHubName(hub) {
  var id = notifHubID(hub);
  return String(hub && (hub.name || hub.hub_name) || id || 'Hub');
}

function notifTenantID(tenant) {
  return String(tenant && tenant.tenant_id || '');
}

function notifTenantName(tenant) {
  var id = notifTenantID(tenant);
  if (id === 'tenant_default') return String(tenant && tenant.tenant_name || tr('defaultTenant'));
  return String(tenant && tenant.tenant_name || id || tr('notifFormTenant'));
}

function notifTenantsForHub(hub) {
  var items = Array.isArray(hub && hub.tenants) ? hub.tenants.slice() : [];
  var hasDefault = items.some(function(t) { return notifTenantID(t) === 'tenant_default'; });
  if (!hasDefault && hub) {
    items.unshift({ tenant_id: 'tenant_default', tenant_name: tr('defaultTenant') });
  }
  return items;
}

// --- Render tab section (injected into DOM on load) ---
function initNotificationTab() {
  var main = document.querySelector('.main');
  if (!main || document.getElementById('tab-notifications')) return;
  var section = document.createElement('section');
  section.id = 'tab-notifications';
  section.className = 'panel card';
  section.innerHTML = '<div id="notifContent"></div>';
  var before = document.getElementById('tab-news') || document.getElementById('tab-system');
  main.insertBefore(section, before || null);
  renderNotifList();
}

// --- Navigation button injection ---
function initNotificationNav() {
  var nav = document.querySelector('.nav');
  if (!nav || nav.querySelector('[data-tab="notifications"]')) return;
  var btn = document.createElement('button');
  btn.type = 'button';
  btn.setAttribute('data-tab', 'notifications');
  btn.onclick = function() { openTab('notifications'); };
  btn.innerHTML = '<span class="nav-icon" aria-hidden="true">' + TAB_ICONS.notifications + '</span>'
    + '<span data-i18n="navNotifications">' + tr('navNotifications') + '</span>'
    + '<small data-i18n="navNotificationsDesc">' + tr('navNotificationsDesc') + '</small>';
  // Insert into content group after news (or before failurelogs/system fallback)
  var newsBtn = nav.querySelector('[data-tab="news"]');
  var group = (newsBtn && newsBtn.closest('.nav-group')) || nav.querySelector('.nav-group[data-nav-group="content"]') || nav;
  if (newsBtn && newsBtn.parentNode === group) {
    group.insertBefore(btn, newsBtn.nextSibling);
  } else {
    var before = nav.querySelector('[data-tab="failurelogs"],[data-tab="system"]');
    group.insertBefore(btn, before || null);
  }
  if (typeof syncNavGroups === 'function') syncNavGroups();
}

// --- List view ---
function renderNotifList() {
  notifViewMode = 'list';
  var root = document.getElementById('notifContent');
  if (!root) return;
  var filterHtml = '<div class="notif-filters">'
    + '<button class="btn-ghost' + (notifStatusFilter === '' ? ' active' : '') + '" onclick="notifSetFilter(\'\')">' + tr('notifFilterAll') + '</button>'
    + '<button class="btn-ghost' + (notifStatusFilter === 'published' ? ' active' : '') + '" onclick="notifSetFilter(\'published\')">' + tr('notifFilterPublished') + '</button>'
    + '<button class="btn-ghost' + (notifStatusFilter === 'expired' ? ' active' : '') + '" onclick="notifSetFilter(\'expired\')">' + tr('notifFilterExpired') + '</button>'
    + '<button class="btn-ghost' + (notifStatusFilter === 'revoked' ? ' active' : '') + '" onclick="notifSetFilter(\'revoked\')">' + tr('notifFilterRevoked') + '</button>'
    + '<button class="btn-ghost' + (notifStatusFilter === 'draft' ? ' active' : '') + '" onclick="notifSetFilter(\'draft\')">' + tr('notifFilterDraft') + '</button>'
    + '</div>';
  root.innerHTML = '<div class="head"><div><h3>' + tr('notifTitle') + '</h3><div class="desc">' + tr('notifDesc') + '</div></div>'
    + '<div class="actions"><button class="btn-primary" onclick="renderNotifCreate()">' + tr('notifCreate') + '</button>'
    + '<button class="btn-ghost" onclick="loadNotifList()">' + tr('notifRefresh') + '</button></div></div>'
    + filterHtml
    + '<div id="notifList" class="list" role="status" aria-live="polite"><div class="hint">' + tr('notifLoading') + '</div></div>';
  loadNotifList();
}

function notifSetFilter(s) { notifStatusFilter = s; notifPage = 1; renderNotifList(); }

function loadNotifList() {
  var listEl = document.getElementById('notifList');
  if (!listEl) return;
  listEl.innerHTML = '<div class="hint">' + tr('notifLoading') + '</div>';
  var limit = 20;
  var offset = Math.max(0, (notifPage - 1) * limit);
  var url = '/api/v1/admin/notifications?limit=' + limit + '&offset=' + offset;
  if (notifStatusFilter) url += '&status=' + encodeURIComponent(notifStatusFilter);
  notifApiGet(url).then(function(data) {
    var items = notifUnwrapList(data);
    var total = Number(data && data.total || items.length || 0);
    if (items.length === 0 && total > 0 && notifPage > 1) {
      notifPage -= 1;
      loadNotifList();
      return;
    }
    if (items.length === 0) {
      listEl.innerHTML = '<div class="hint">' + tr('notifEmpty') + '</div>';
      notifRenderPager(0, 0, limit);
      return;
    }
    listEl.innerHTML = items.map(notifCardHtml).join('');
    notifRenderPager(total, items.length, limit);
  }).catch(function(err) {
    listEl.innerHTML = '<div class="hint danger">' + tr('notifLoadFailed', { error: escapeHtml(err.message) }) + '</div>';
    notifRenderPager(0, 0, limit);
  });
}

function notifRenderPager(total, count, limit) {
  var listEl = document.getElementById('notifList');
  if (!listEl) return;
  var old = document.getElementById('notifPager');
  if (old) old.remove();
  if (total <= limit) return;
  var totalPages = Math.max(1, Math.ceil(total / limit));
  notifPage = Math.min(Math.max(1, notifPage), totalPages);
  var start = (notifPage - 1) * limit + 1;
  var end = Math.min(start + count - 1, total);
  var pager = document.createElement('div');
  pager.id = 'notifPager';
  pager.className = 'pager pager-compact';
  pager.innerHTML = '<div class="pager-meta">' + escapeHtml(start + '-' + end + ' / ' + total) + '</div>'
    + '<div class="pager-actions"><button class="btn-ghost" type="button" onclick="notifChangePage(-1)" ' + (notifPage <= 1 ? 'disabled' : '') + '>&larr;</button>'
    + '<button class="btn-ghost" type="button" onclick="notifChangePage(1)" ' + (notifPage >= totalPages ? 'disabled' : '') + '>&rarr;</button></div>';
  listEl.insertAdjacentElement('afterend', pager);
}

function notifChangePage(delta) {
  notifPage = Math.max(1, notifPage + (Number(delta) || 0));
  loadNotifList();
}

function notifCardHtml(n) {
  var statusBadge = notifStatusBadge(n.status);
  var catLabel = notifCategoryLabel(n.category);
  var prioLabel = n.priority === 'urgent' ? '<span class="badge danger">' + tr('notifPriorityUrgent') + '</span>' :
    n.priority === 'important' ? '<span class="badge warn">' + tr('notifPriorityImportant') + '</span>' : '';
  var time = n.created_at ? new Date(n.created_at).toLocaleString() : '';
  var cascadeCount = (n.cascade_results || []).length;
  var cascadeHint = cascadeCount > 0 ? ' <span class="item-meta">(' + cascadeCount + ' Hubs)</span>' : '';
  var deleteBtn = notifCanDelete(n)
    ? '<button class="btn-danger" type="button" onclick="event.stopPropagation();notifDelete(' + notifJsArg(n.id) + ')">' + escapeHtml(tr('notifDelete')) + '</button>'
    : '';
  return '<div class="item news-card notif-card" onclick="renderNotifDetail(' + notifJsArg(n.id) + ')">'
    + '<div class="notif-card-row">'
    + '<div class="notif-card-main"><div class="news-title">' + escapeHtml(n.title || '') + '</div>'
    + '<div class="news-meta">' + catLabel + ' ' + statusBadge + ' ' + prioLabel + cascadeHint + '</div>'
    + '<div class="item-meta">' + escapeHtml(time) + '</div></div>'
    + (deleteBtn ? '<div class="actions notif-card-actions">' + deleteBtn + '</div>' : '')
    + '</div>'
    + '</div>';
}

function notifStatusBadge(status) {
  status = status || 'draft';
  var cls = status === 'published' ? 'ok' : status === 'revoked' ? 'danger' : status === 'expired' ? 'warn' : 'info';
  var key = 'notifStatus' + status.charAt(0).toUpperCase() + status.slice(1);
  return '<span class="badge ' + cls + '">' + tr(key) + '</span>';
}

function notifCategoryLabel(cat) {
  var map = { system_announcement: 'notifCategorySystem', feature_update: 'notifCategoryFeature',
    security_alert: 'notifCategorySecurity', maintenance: 'notifCategoryMaintenance', custom: 'notifCategoryCustom' };
  return '<span class="badge info">' + tr(map[cat] || 'notifCategoryCustom') + '</span>';
}

// --- Detail view ---
function renderNotifDetail(id) {
  notifViewMode = 'detail';
  notifCurrentId = id;
  var root = document.getElementById('notifContent');
  if (!root) return;
  root.innerHTML = '<div class="hint">' + tr('notifLoading') + '</div>';
  notifApiGet('/api/v1/admin/notifications/' + encodeURIComponent(id)).then(function(data) {
    var n = notifUnwrapDetail(data);
    var statusBadge = notifStatusBadge(n.status);
    var catLabel = notifCategoryLabel(n.category);
    var canRevoke = n.status === 'published';
    var revokeBtn = canRevoke ? '<button class="btn-danger" onclick="notifRevoke(' + notifJsArg(n.id) + ')">' + tr('notifRevoke') + '</button>' : '';
    var deleteBtn = notifCanDelete(n) ? '<button class="btn-danger" onclick="notifDelete(' + notifJsArg(n.id) + ')">' + tr('notifDelete') + '</button>' : '';
    var statsHtml = '<div class="grid3 section-gap"><div class="item"><div class="item-title">' + tr('notifDetailTotal') + '</div><strong>' + (n.total_pushed || 0) + '</strong></div>'
      + '<div class="item"><div class="item-title">' + tr('notifDetailRead') + '</div><strong>' + (n.read_count || 0) + '</strong></div>'
      + '<div class="item"><div class="item-title">' + tr('notifDetailReadRate') + '</div><strong>' + (n.read_rate || '0%') + '</strong></div></div>';
    var cascadeHtml = notifCascadeTableHtml(n.cascade_results || []);
    var contentHtml = '<div class="item section-gap"><div class="item-title" data-icon="file">' + tr('notifFormContent') + '</div><div class="notif-content-preview">' + escapeHtml(n.content || '') + '</div></div>';
    root.innerHTML = '<div class="head"><div><h3>' + escapeHtml(n.title || '') + '</h3><div class="desc">' + catLabel + ' ' + statusBadge + '</div></div>'
      + '<div class="actions"><button class="btn-ghost" onclick="renderNotifList()">' + tr('notifDetailBack') + '</button>' + revokeBtn + deleteBtn + '</div></div>'
      + contentHtml
      + '<div class="item section-gap"><div class="item-title" data-icon="chart">' + tr('notifDetailStats') + '</div>' + statsHtml + '</div>'
      + cascadeHtml;
  }).catch(function(err) {
    root.innerHTML = '<div class="hint danger">' + tr('notifLoadFailed', { error: escapeHtml(err.message) }) + '</div>'
      + '<button class="btn-ghost" onclick="renderNotifList()">' + tr('notifDetailBack') + '</button>';
  });
}

function notifCascadeTableHtml(results) {
  if (!results || results.length === 0) return '';
  var rows = results.map(function(r) {
    var statusCls = r.status === 'success' ? 'ok' : r.status === 'failed' ? 'danger' : 'warn';
    var statusKey = 'notifCascade' + r.status.charAt(0).toUpperCase() + r.status.slice(1);
    var time = r.pushed_at ? new Date(r.pushed_at).toLocaleString() : '-';
    return '<tr><td>' + escapeHtml(r.hub_name || r.hub_id || '-') + '</td><td>' + escapeHtml(time) + '</td><td><span class="badge ' + statusCls + '">' + tr(statusKey) + '</span></td></tr>';
  }).join('');
  return '<div class="item section-gap"><div class="item-title" data-icon="tree">' + tr('notifCascadeTitle') + '</div>'
    + '<table class="notif-cascade-table"><thead><tr><th>' + tr('notifCascadeHub') + '</th><th>' + tr('notifCascadeTime') + '</th><th>' + tr('notifCascadeStatus') + '</th></tr></thead>'
    + '<tbody>' + rows + '</tbody></table></div>';
}

// --- Create view ---
function renderNotifCreate() {
  notifViewMode = 'create';
  var root = document.getElementById('notifContent');
  if (!root) return;
  root.innerHTML = '<div class="head"><div><h3>' + tr('notifCreate') + '</h3></div>'
    + '<div class="actions"><button class="btn-ghost" onclick="renderNotifList()">' + tr('notifFormCancel') + '</button></div></div>'
    + '<div class="notif-form">'
    + '<div><label for="notifTitleInput">' + tr('notifFormTitle') + '</label>'
    + '<input id="notifTitleInput" maxlength="100" placeholder="' + tr('notifFormTitlePlaceholder') + '"></div>'
    + '<div><label for="notifContentInput">' + tr('notifFormContent') + '</label>'
    + '<textarea id="notifContentInput" maxlength="2000" rows="6" placeholder="' + tr('notifFormContentPlaceholder') + '"></textarea></div>'
    + '<div class="grid3">'
    + '<div><label for="notifCategorySelect">' + tr('notifFormCategory') + '</label>'
    + '<select id="notifCategorySelect">'
    + '<option value="system_announcement">' + tr('notifCategorySystem') + '</option>'
    + '<option value="feature_update">' + tr('notifCategoryFeature') + '</option>'
    + '<option value="security_alert">' + tr('notifCategorySecurity') + '</option>'
    + '<option value="maintenance">' + tr('notifCategoryMaintenance') + '</option>'
    + '<option value="custom">' + tr('notifCategoryCustom') + '</option>'
    + '</select></div>'
    + '<div><label for="notifPrioritySelect">' + tr('notifFormPriority') + '</label>'
    + '<select id="notifPrioritySelect">'
    + '<option value="normal">' + tr('notifPriorityNormal') + '</option>'
    + '<option value="important">' + tr('notifPriorityImportant') + '</option>'
    + '<option value="urgent">' + tr('notifPriorityUrgent') + '</option>'
    + '</select></div>'
    + '<div><label for="notifPublishSelect">' + tr('notifFormPublishAt') + '</label>'
    + '<select id="notifPublishSelect" onchange="notifToggleSchedule()">'
    + '<option value="now">' + tr('notifFormImmediate') + '</option>'
    + '<option value="scheduled">' + tr('notifFormScheduled') + '</option>'
    + '</select></div>'
    + '</div>'
    + '<div id="notifScheduleRow" class="hidden"><label for="notifPublishAtInput">' + tr('notifFormPublishAt') + '</label>'
    + '<input id="notifPublishAtInput" type="datetime-local"></div>'
    + '<div><label for="notifExpireAtInput">' + tr('notifFormExpireAt') + '</label>'
    + '<input id="notifExpireAtInput" type="datetime-local"></div>'
    + '<div><label for="notifAudienceSelect">' + tr('notifFormAudience') + '</label>'
    + '<select id="notifAudienceSelect" onchange="notifToggleAudience()">'
    + '<option value="all">' + tr('notifAudienceAllHubs') + '</option>'
    + '<option value="hub">' + tr('notifAudienceHubs') + '</option>'
    + '<option value="hub_tenant">' + tr('notifAudienceTenant') + '</option>'
    + '</select></div>'
    + '<div id="notifAudienceHubsRow" class="hidden"><label>' + tr('notifFormSelectHubs') + '</label>'
    + '<div id="notifHubCheckboxes" class="notif-hub-checkboxes"></div></div>'
    + '<div id="notifAudienceTenantRow" class="hidden"><label>' + tr('notifFormSelectTenant') + '</label>'
    + '<div class="notif-tenant-picker">'
    + '<div><label for="notifTenantHubSelect">' + tr('notifFormHub') + '</label><select id="notifTenantHubSelect" onchange="notifSyncTenantSelect()"></select></div>'
    + '<div><label for="notifTenantSelect">' + tr('notifFormTenant') + '</label><select id="notifTenantSelect"></select></div>'
    + '<div class="notif-tenant-add"><button class="btn-secondary" type="button" onclick="notifAddTenantTarget()">' + tr('notifFormAddTenant') + '</button></div>'
    + '</div><div class="item-meta section-gap-sm">' + tr('notifFormSelectedTenants') + '</div><div id="notifTenantTargets" class="notif-tenant-targets"></div></div>'
    + '<div id="notifImPushRow" class="hidden inline-check"><label for="notifImPush" class="inline-label">' + tr('notifFormImPush') + '</label>'
    + '<input type="checkbox" id="notifImPush" class="auto-check"></div>'
    + '<div class="actions section-gap">'
    + '<button class="btn-primary" onclick="notifSubmit(\'published\')">' + tr('notifFormPublishNow') + '</button>'
    + '<button class="btn-secondary" onclick="notifSubmit(\'draft\')">' + tr('notifFormSaveDraft') + '</button>'
    + '</div></div>';
  notifTenantTargets = [];
  notifLoadHubCheckboxes();
  notifLoadTenantSelector();
  notifTogglePriorityIM();
  document.getElementById('notifPrioritySelect').addEventListener('change', notifTogglePriorityIM);
}

function notifToggleSchedule() {
  var sel = document.getElementById('notifPublishSelect');
  var row = document.getElementById('notifScheduleRow');
  if (sel && row) row.classList.toggle('hidden', sel.value !== 'scheduled');
}

function notifToggleAudience() {
  var sel = document.getElementById('notifAudienceSelect');
  var hubsRow = document.getElementById('notifAudienceHubsRow');
  var tenantRow = document.getElementById('notifAudienceTenantRow');
  if (hubsRow) hubsRow.classList.toggle('hidden', sel.value !== 'hub');
  if (tenantRow) tenantRow.classList.toggle('hidden', sel.value !== 'hub_tenant');
}

function notifTogglePriorityIM() {
  var sel = document.getElementById('notifPrioritySelect');
  var row = document.getElementById('notifImPushRow');
  if (row) row.classList.toggle('hidden', !sel || sel.value !== 'urgent');
}

function notifLoadHubCheckboxes() {
  var container = document.getElementById('notifHubCheckboxes');
  if (!container) return;
  notifLoadHubList().then(function(hubs) {
    if (hubs.length === 0) { container.innerHTML = '<div class="hint">' + tr('notifFormNoHubs') + '</div>'; return; }
    container.innerHTML = hubs.map(function(h) {
      var name = h.name || h.hub_name || h.id || h.hub_id || 'Hub';
      var id = h.id || h.hub_id || '';
      return '<label class="notif-hub-check"><input type="checkbox" value="' + escapeHtml(id) + '"> ' + escapeHtml(name) + '</label>';
    }).join('');
  });
}

function notifLoadTenantSelector() {
  notifLoadHubList().then(function(hubs) {
    var hubSelect = document.getElementById('notifTenantHubSelect');
    if (!hubSelect) return;
    if (!hubs.length) {
      hubSelect.innerHTML = '<option value="">' + escapeHtml(tr('notifFormNoHubs')) + '</option>';
      hubSelect.disabled = true;
      notifSyncTenantSelect();
      notifRenderTenantTargets();
      return;
    }
    hubSelect.disabled = false;
    hubSelect.innerHTML = hubs.map(function(h) {
      var id = notifHubID(h);
      return '<option value="' + escapeHtml(id) + '">' + escapeHtml(notifHubName(h) + ' (' + id + ')') + '</option>';
    }).join('');
    notifSyncTenantSelect();
    notifRenderTenantTargets();
  });
}

function notifSyncTenantSelect() {
  var hubSelect = document.getElementById('notifTenantHubSelect');
  var tenantSelect = document.getElementById('notifTenantSelect');
  if (!tenantSelect) return;
  var hubID = String(hubSelect && hubSelect.value || '');
  var hub = (notifHubListCache || []).find(function(h) { return notifHubID(h) === hubID; });
  var tenants = notifTenantsForHub(hub);
  if (!hubID || !tenants.length) {
    tenantSelect.innerHTML = '<option value="">' + escapeHtml(tr('notifFormNoTenants')) + '</option>';
    tenantSelect.disabled = true;
    return;
  }
  tenantSelect.disabled = false;
  tenantSelect.innerHTML = tenants.map(function(t) {
    var id = notifTenantID(t);
    return '<option value="' + escapeHtml(id) + '">' + escapeHtml(notifTenantName(t) + ' (' + id + ')') + '</option>';
  }).join('');
}

function notifAddTenantTarget() {
  var hubID = String((document.getElementById('notifTenantHubSelect') || {}).value || '');
  var tenantID = String((document.getElementById('notifTenantSelect') || {}).value || '');
  if (!hubID || !tenantID) { notifToast(tr('notifValidationAudience'), 'warn'); return; }
  var value = hubID + ':' + tenantID;
  if (notifTenantTargets.some(function(t) { return t.value === value; })) {
    notifToast(tr('notifFormTenantAlreadyAdded'), 'warn');
    return;
  }
  var hub = (notifHubListCache || []).find(function(h) { return notifHubID(h) === hubID; });
  var tenant = notifTenantsForHub(hub).find(function(t) { return notifTenantID(t) === tenantID; });
  notifTenantTargets.push({ value: value, hub_id: hubID, tenant_id: tenantID, label: notifHubName(hub) + ' / ' + notifTenantName(tenant) });
  notifRenderTenantTargets();
}

function notifRemoveTenantTarget(value) {
  notifTenantTargets = notifTenantTargets.filter(function(t) { return t.value !== value; });
  notifRenderTenantTargets();
}

function notifRenderTenantTargets() {
  var root = document.getElementById('notifTenantTargets');
  if (!root) return;
  if (!notifTenantTargets.length) {
    root.innerHTML = '<div class="hint">' + escapeHtml(tr('notifValidationAudience')) + '</div>';
    return;
  }
  root.innerHTML = notifTenantTargets.map(function(t) {
    return '<span class="notif-tenant-chip"><span>' + escapeHtml(t.label) + '</span><small class="mono">' + escapeHtml(t.value) + '</small><button type="button" class="btn-ghost tiny-btn" onclick="notifRemoveTenantTarget(' + notifJsArg(t.value) + ')">' + escapeHtml(tr('notifFormRemoveTenant')) + '</button></span>';
  }).join('');
}

// --- Submit notification ---
function notifSubmit(status) {
  var title = (document.getElementById('notifTitleInput') || {}).value || '';
  var content = (document.getElementById('notifContentInput') || {}).value || '';
  var category = (document.getElementById('notifCategorySelect') || {}).value || 'system_announcement';
  var priority = (document.getElementById('notifPrioritySelect') || {}).value || 'normal';
  var audienceType = (document.getElementById('notifAudienceSelect') || {}).value || 'all';
  var publishSelect = (document.getElementById('notifPublishSelect') || {}).value || 'now';
  var publishAt = publishSelect === 'scheduled' ? (document.getElementById('notifPublishAtInput') || {}).value || '' : '';
  var expireAt = (document.getElementById('notifExpireAtInput') || {}).value || '';
  var imPush = priority === 'urgent' && (document.getElementById('notifImPush') || {}).checked;

  // Validation
  if (!title || title.length > 100) { notifToast(tr('notifValidationTitle'), 'warn'); return; }
  if (!content || content.length > 2000) { notifToast(tr('notifValidationContent'), 'warn'); return; }

  // Build audience_ids based on audience type
  var audienceIds = [];
  if (audienceType === 'hub') {
    var checks = document.querySelectorAll('#notifHubCheckboxes input[type="checkbox"]:checked');
    checks.forEach(function(c) { audienceIds.push(c.value); });
    if (audienceIds.length === 0) { notifToast(tr('notifValidationAudience'), 'warn'); return; }
  } else if (audienceType === 'hub_tenant') {
    audienceIds = notifTenantTargets.map(function(t) { return t.value; }).filter(Boolean);
    if (audienceIds.length === 0) { notifToast(tr('notifValidationAudience'), 'warn'); return; }
  }

  var body = {
    title: title,
    content: content,
    category: category,
    priority: priority,
    audience_type: audienceType,
    audience_ids: audienceIds,
    status: status,
    im_push: imPush
  };
  if (publishAt) body.publish_at = new Date(publishAt).toISOString();
  if (expireAt) body.expire_at = new Date(expireAt).toISOString();

  notifApiPost('/api/v1/admin/notifications', body).then(function() {
    notifToast(tr('notifCreated'), 'ok');
    renderNotifList();
  }).catch(function(err) {
    notifToast(tr('notifCreateFailed', { error: err.message }), 'danger');
  });
}

// --- Revoke notification ---
function notifRevoke(id) {
  if (!confirm(tr('notifRevokeConfirm'))) return;
  notifApiPost('/api/v1/admin/notifications/' + encodeURIComponent(id) + '/revoke', {}).then(function() {
    notifToast(tr('notifRevoked'), 'ok');
    if (notifViewMode === 'detail') renderNotifDetail(id);
    else loadNotifList();
  }).catch(function(err) {
    notifToast(tr('notifRevokeFailed', { error: err.message }), 'danger');
  });
}

function notifDelete(id) {
  if (!confirm(tr('notifDeleteConfirm'))) return;
  notifApiDelete('/api/v1/admin/notifications/' + encodeURIComponent(id)).then(function() {
    notifToast(tr('notifDeleted'), 'ok');
    renderNotifList();
  }).catch(function(err) {
    notifToast(tr('notifDeleteFailed', { error: err.message }), 'danger');
  });
}

Object.assign(window, {
  renderNotifList: renderNotifList,
  notifSetFilter: notifSetFilter,
  loadNotifList: loadNotifList,
  renderNotifDetail: renderNotifDetail,
  renderNotifCreate: renderNotifCreate,
  notifToggleSchedule: notifToggleSchedule,
  notifToggleAudience: notifToggleAudience,
  notifSyncTenantSelect: notifSyncTenantSelect,
  notifAddTenantTarget: notifAddTenantTarget,
  notifRemoveTenantTarget: notifRemoveTenantTarget,
  notifChangePage: notifChangePage,
  notifSubmit: notifSubmit,
  notifRevoke: notifRevoke,
  notifDelete: notifDelete
});

// --- Initialize on app ready ---
(function() {
  var origOpenTab = window._notifOrigOpenTab || window.openTab;
  if (!window._notifOrigOpenTab) window._notifOrigOpenTab = origOpenTab;

  function activateSavedNotificationTab() {
    if (localStorage.getItem('maclawHubCenterActiveTab') === 'notifications' && typeof window.openTab === 'function') {
      window.openTab('notifications');
    }
  }

  // Patch openTab to lazy-init notification content
  var patchedOpenTab = function(tab) {
    if (typeof origOpenTab === 'function') origOpenTab(tab);
    if (tab === 'notifications') {
      if (!document.getElementById('tab-notifications')) initNotificationTab();
      if (notifViewMode === 'list') loadNotifList();
    }
  };
  window.openTab = patchedOpenTab;

  // Wait for DOM and inject nav + tab section
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      initNotificationNav();
      initNotificationTab();
      activateSavedNotificationTab();
    });
  } else {
    setTimeout(function() {
      initNotificationNav();
      initNotificationTab();
      activateSavedNotificationTab();
    }, 0);
  }
})();
