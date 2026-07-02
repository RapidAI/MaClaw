/*
 * Notification management admin module.
 * ASCII only.
 */
(function(global) {
  if (typeof I18N !== 'undefined') {
    I18N.en = Object.assign({}, I18N.en, {
      navNotifications: 'Notifications',
      navNotificationsDesc: 'Manage and push notifications to users',
      notifTabTitle: 'Notification Management',
      notifTabSubtitle: 'Create, publish, and manage notifications for end users.',
      notifCreateNew: 'Create Notification',
      notifFilterAll: 'All',
      notifFilterPublished: 'Published',
      notifFilterExpired: 'Expired',
      notifFilterRevoked: 'Revoked',
      notifFilterDraft: 'Draft',
      notifCategoryAll: 'All Categories',
      notifCategorySystem: 'System Announcement',
      notifCategoryFeature: 'Feature Update',
      notifCategorySecurity: 'Security Alert',
      notifCategoryMaintenance: 'Maintenance',
      notifCategoryCustom: 'Custom',
      notifEmpty: 'No notifications match the current filters.',
      notifLoadFailed: 'Failed to load notifications: {error}',
      notifTitle: 'Title',
      notifStatus: 'Status',
      notifCategory: 'Category',
      notifCreatedAt: 'Created',
      notifDeliveryRate: 'Delivery Rate',
      notifActions: 'Actions',
      notifView: 'View',
      notifRevoke: 'Revoke',
      notifRevokeConfirm: 'Confirm revoke this notification?',
      notifRevokeSuccess: 'Notification revoked.',
      notifRevokeFailed: 'Revoke failed: {error}',
      notifCreateTitle: 'Create Notification',
      notifFormTitle: 'Title',
      notifFormTitlePlaceholder: 'Notification title (max 100 chars)',
      notifFormContent: 'Content (Markdown)',
      notifFormContentPlaceholder: 'Notification content, supports Markdown (max 2000 chars)',
      notifFormCategory: 'Category',
      notifFormAudience: 'Audience',
      notifAudienceAll: 'All Users',
      notifAudienceTenant: 'Specific Tenants',
      notifAudienceDepartment: 'Specific Departments',
      notifAudienceUser: 'Specific Users',
      notifFormAudienceIds: 'Target IDs (comma separated)',
      notifFormPriority: 'Priority',
      notifPriorityNormal: 'Normal',
      notifPriorityImportant: 'Important',
      notifPriorityUrgent: 'Urgent',
      notifFormImPush: 'Push to IM channel',
      notifFormPublishAt: 'Publish Time',
      notifFormPublishNow: 'Immediately',
      notifFormPublishScheduled: 'Scheduled',
      notifFormExpireAt: 'Expire Time (optional)',
      notifFormSaveDraft: 'Save Draft',
      notifFormPublish: 'Publish Now',
      notifCreateSuccess: 'Notification created.',
      notifCreateFailed: 'Create failed: {error}',
      notifDetailTitle: 'Notification Detail',
      notifDetailBack: 'Back to List',
      notifDetailContent: 'Content',
      notifDetailStats: 'Delivery Statistics',
      notifStatTotal: 'Total Pushed',
      notifStatRead: 'Read',
      notifStatRate: 'Read Rate',
      notifPreview: 'Preview',
      notifFormContentChars: '{count}/2000'
    });
    I18N.zh = Object.assign({}, I18N.zh, {
      navNotifications: '\u901a\u77e5\u7ba1\u7406',
      navNotificationsDesc: '\u7ba1\u7406\u5e76\u63a8\u9001\u901a\u77e5\u7ed9\u7528\u6237',
      notifTabTitle: '\u901a\u77e5\u7ba1\u7406',
      notifTabSubtitle: '\u521b\u5efa\u3001\u53d1\u5e03\u548c\u7ba1\u7406\u7ec8\u7aef\u7528\u6237\u901a\u77e5\u3002',
      notifCreateNew: '\u521b\u5efa\u65b0\u901a\u77e5',
      notifFilterAll: '\u5168\u90e8',
      notifFilterPublished: '\u5df2\u53d1\u5e03',
      notifFilterExpired: '\u5df2\u8fc7\u671f',
      notifFilterRevoked: '\u5df2\u64a4\u56de',
      notifFilterDraft: '\u8349\u7a3f',
      notifCategoryAll: '\u5168\u90e8\u5206\u7c7b',
      notifCategorySystem: '\u7cfb\u7edf\u516c\u544a',
      notifCategoryFeature: '\u529f\u80fd\u66f4\u65b0',
      notifCategorySecurity: '\u5b89\u5168\u544a\u8b66',
      notifCategoryMaintenance: '\u8fd0\u7ef4\u901a\u77e5',
      notifCategoryCustom: '\u81ea\u5b9a\u4e49',
      notifEmpty: '\u5f53\u524d\u7b5b\u9009\u6761\u4ef6\u4e0b\u6682\u65e0\u901a\u77e5\u3002',
      notifLoadFailed: '\u52a0\u8f7d\u901a\u77e5\u5931\u8d25\uff1a{error}',
      notifTitle: '\u6807\u9898',
      notifStatus: '\u72b6\u6001',
      notifCategory: '\u5206\u7c7b',
      notifCreatedAt: '\u521b\u5efa\u65f6\u95f4',
      notifDeliveryRate: '\u9001\u8fbe\u7387',
      notifActions: '\u64cd\u4f5c',
      notifView: '\u67e5\u770b',
      notifRevoke: '\u64a4\u56de',
      notifRevokeConfirm: '\u786e\u8ba4\u64a4\u56de\u6b64\u901a\u77e5\uff1f',
      notifRevokeSuccess: '\u901a\u77e5\u5df2\u64a4\u56de\u3002',
      notifRevokeFailed: '\u64a4\u56de\u5931\u8d25\uff1a{error}',
      notifCreateTitle: '\u521b\u5efa\u901a\u77e5',
      notifFormTitle: '\u6807\u9898',
      notifFormTitlePlaceholder: '\u901a\u77e5\u6807\u9898\uff08\u6700\u591a100\u5b57\u7b26\uff09',
      notifFormContent: '\u5185\u5bb9\uff08Markdown\uff09',
      notifFormContentPlaceholder: '\u901a\u77e5\u5185\u5bb9\uff0c\u652f\u6301 Markdown\uff08\u6700\u591a2000\u5b57\u7b26\uff09',
      notifFormCategory: '\u5206\u7c7b',
      notifFormAudience: '\u53d7\u4f17',
      notifAudienceAll: '\u6240\u6709\u7528\u6237',
      notifAudienceTenant: '\u6307\u5b9a\u79df\u6237',
      notifAudienceDepartment: '\u6307\u5b9a\u90e8\u95e8',
      notifAudienceUser: '\u6307\u5b9a\u7528\u6237',
      notifFormAudienceIds: '\u76ee\u6807 ID\uff08\u82f1\u6587\u9017\u53f7\u5206\u9694\uff09',
      notifFormPriority: '\u4f18\u5148\u7ea7',
      notifPriorityNormal: '\u666e\u901a',
      notifPriorityImportant: '\u91cd\u8981',
      notifPriorityUrgent: '\u7d27\u6025',
      notifFormImPush: '\u63a8\u9001\u5230 IM \u901a\u9053',
      notifFormPublishAt: '\u53d1\u5e03\u65f6\u95f4',
      notifFormPublishNow: '\u7acb\u5373\u53d1\u5e03',
      notifFormPublishScheduled: '\u5b9a\u65f6\u53d1\u5e03',
      notifFormExpireAt: '\u8fc7\u671f\u65f6\u95f4\uff08\u53ef\u9009\uff09',
      notifFormSaveDraft: '\u4fdd\u5b58\u8349\u7a3f',
      notifFormPublish: '\u7acb\u5373\u53d1\u5e03',
      notifCreateSuccess: '\u901a\u77e5\u521b\u5efa\u6210\u529f\u3002',
      notifCreateFailed: '\u521b\u5efa\u5931\u8d25\uff1a{error}',
      notifDetailTitle: '\u901a\u77e5\u8be6\u60c5',
      notifDetailBack: '\u8fd4\u56de\u5217\u8868',
      notifDetailContent: '\u5185\u5bb9',
      notifDetailStats: '\u9001\u8fbe\u7edf\u8ba1',
      notifStatTotal: '\u603b\u63a8\u9001\u6570',
      notifStatRead: '\u5df2\u8bfb\u6570',
      notifStatRate: '\u5df2\u8bfb\u7387',
      notifPreview: '\u9884\u89c8',
      notifFormContentChars: '{count}/2000'
    });
  }

  // --- State ---
  const notifState = {
    items: [],
    total: 0,
    statusFilter: '',
    categoryFilter: '',
    currentView: 'list' // 'list' | 'create' | 'detail'
  };
  let notifDetailData = null;

  // --- Helpers ---
  function statusBadgeClass(status) {
    switch (status) {
      case 'published': return 'ok';
      case 'expired': return 'warn';
      case 'revoked': return 'danger';
      case 'draft': return 'info';
      default: return 'info';
    }
  }

  function statusLabel(status) {
    switch (status) {
      case 'published': return tr('notifFilterPublished');
      case 'expired': return tr('notifFilterExpired');
      case 'revoked': return tr('notifFilterRevoked');
      case 'draft': return tr('notifFilterDraft');
      default: return status;
    }
  }

  function categoryLabel(cat) {
    switch (cat) {
      case 'system_announcement': return tr('notifCategorySystem');
      case 'feature_update': return tr('notifCategoryFeature');
      case 'security_alert': return tr('notifCategorySecurity');
      case 'maintenance': return tr('notifCategoryMaintenance');
      case 'custom': return tr('notifCategoryCustom');
      default: return cat;
    }
  }

  function formatTime(iso) {
    if (!iso) return '-';
    try {
      const d = new Date(iso);
      return d.toLocaleString();
    } catch (_) { return iso; }
  }

  function deliveryRateText(stats) {
    if (!stats || !stats.total_pushed) return '-';
    const rate = stats.total_pushed > 0
      ? Math.round((stats.read_count / stats.total_pushed) * 100)
      : 0;
    return rate + '% (' + stats.read_count + '/' + stats.total_pushed + ')';
  }

  // --- Tab Panel Container ---
  function getPanel() {
    return document.getElementById('tab-notifications');
  }

  // --- List View ---
  function renderListView() {
    const panel = getPanel();
    if (!panel) return;
    const html = '<div class="head"><div><h3>' + escapeHtml(tr('notifTabTitle')) + '</h3>'
      + '<div class="desc">' + escapeHtml(tr('notifTabSubtitle')) + '</div></div>'
      + '<button class="btn-primary" type="button" onclick="notifShowCreate()">' + escapeHtml(tr('notifCreateNew')) + '</button></div>'
      + '<div class="notif-filters" style="display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap">'
      + buildStatusFilter()
      + buildCategoryFilter()
      + '</div>'
      + '<div id="notifList" role="status" aria-live="polite">'
      + '<div class="hint">' + escapeHtml(tr('notifEmpty')) + '</div></div>';
    panel.innerHTML = html;
    loadNotifications();
  }

  function buildStatusFilter() {
    const options = [
      { value: '', label: tr('notifFilterAll') },
      { value: 'published', label: tr('notifFilterPublished') },
      { value: 'expired', label: tr('notifFilterExpired') },
      { value: 'revoked', label: tr('notifFilterRevoked') },
      { value: 'draft', label: tr('notifFilterDraft') }
    ];
    let html = '<select id="notifStatusFilter" onchange="notifApplyFilters()" style="width:auto;min-width:120px">';
    options.forEach(function(opt) {
      const sel = opt.value === notifState.statusFilter ? ' selected' : '';
      html += '<option value="' + opt.value + '"' + sel + '>' + escapeHtml(opt.label) + '</option>';
    });
    html += '</select>';
    return html;
  }

  function buildCategoryFilter() {
    const options = [
      { value: '', label: tr('notifCategoryAll') },
      { value: 'system_announcement', label: tr('notifCategorySystem') },
      { value: 'feature_update', label: tr('notifCategoryFeature') },
      { value: 'security_alert', label: tr('notifCategorySecurity') },
      { value: 'maintenance', label: tr('notifCategoryMaintenance') },
      { value: 'custom', label: tr('notifCategoryCustom') }
    ];
    let html = '<select id="notifCategoryFilter" onchange="notifApplyFilters()" style="width:auto;min-width:140px">';
    options.forEach(function(opt) {
      const sel = opt.value === notifState.categoryFilter ? ' selected' : '';
      html += '<option value="' + opt.value + '"' + sel + '>' + escapeHtml(opt.label) + '</option>';
    });
    html += '</select>';
    return html;
  }

  async function loadNotifications() {
    try {
      const params = new URLSearchParams();
      if (notifState.statusFilter) params.set('status', notifState.statusFilter);
      if (notifState.categoryFilter) params.set('category', notifState.categoryFilter);
      const qs = params.toString();
      const url = '/api/v1/admin/notifications' + (qs ? '?' + qs : '');
      const data = await api(url);
      notifState.items = Array.isArray(data.notifications) ? data.notifications : (Array.isArray(data) ? data : []);
      notifState.total = notifState.items.length;
      renderNotificationList();
    } catch (err) {
      const msg = tr('notifLoadFailed', { error: err.message });
      showToast(msg, 'error');
      const root = document.getElementById('notifList');
      if (root) root.innerHTML = '<div class="hint" style="color:var(--danger)">' + escapeHtml(msg) + '</div>';
    }
  }

  function renderNotificationList() {
    const root = document.getElementById('notifList');
    if (!root) return;
    if (!notifState.items.length) {
      root.innerHTML = '<div class="hint">' + escapeHtml(tr('notifEmpty')) + '</div>';
      return;
    }
    const cards = notifState.items.map(function(item) {
      const stats = item.stats || {};
      const rate = deliveryRateText(stats);
      const canRevoke = item.status === 'published';
      const revokeBtn = canRevoke
        ? '<button class="btn-danger" style="height:30px;padding:0 10px;font-size:11px" onclick="notifRevokeItem(\'' + escapeHtml(item.id) + '\')">' + escapeHtml(tr('notifRevoke')) + '</button>'
        : '';
      return '<div class="item" style="padding:12px 14px">'
        + '<div style="display:flex;justify-content:space-between;align-items:flex-start;gap:10px">'
        + '<div style="min-width:0;flex:1">'
        + '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:4px">'
        + '<span class="badge ' + statusBadgeClass(item.status) + '">' + escapeHtml(statusLabel(item.status)) + '</span>'
        + '<span class="badge info">' + escapeHtml(categoryLabel(item.category)) + '</span>'
        + (item.priority === 'urgent' ? '<span class="badge danger">URGENT</span>' : '')
        + (item.priority === 'important' ? '<span class="badge warn">IMPORTANT</span>' : '')
        + '</div>'
        + '<div class="item-title" style="font-size:14px;margin-bottom:3px">' + escapeHtml(item.title || '-') + '</div>'
        + '<div class="item-meta">' + escapeHtml(tr('notifCreatedAt')) + ': ' + escapeHtml(formatTime(item.created_at))
        + ' &nbsp;|&nbsp; ' + escapeHtml(tr('notifDeliveryRate')) + ': ' + escapeHtml(rate) + '</div>'
        + '</div>'
        + '<div style="display:flex;gap:6px;flex-shrink:0">'
        + '<button class="btn-ghost" style="height:30px;padding:0 10px;font-size:11px" onclick="notifShowDetail(\'' + escapeHtml(item.id) + '\')">' + escapeHtml(tr('notifView')) + '</button>'
        + revokeBtn
        + '</div></div></div>';
    }).join('');
    root.innerHTML = cards;
  }

  // --- Create View ---
  function renderCreateView() {
    const panel = getPanel();
    if (!panel) return;
    const html = '<div class="head"><div><h3>' + escapeHtml(tr('notifCreateTitle')) + '</h3></div>'
      + '<button class="btn-ghost" type="button" onclick="notifShowList()">' + escapeHtml(tr('notifDetailBack')) + '</button></div>'
      + '<div style="display:grid;gap:12px;max-width:720px">'
      // Title
      + '<div><label for="notifInputTitle">' + escapeHtml(tr('notifFormTitle')) + '</label>'
      + '<input id="notifInputTitle" maxlength="100" placeholder="' + escapeHtml(tr('notifFormTitlePlaceholder')) + '"></div>'
      // Content with preview
      + '<div><label for="notifInputContent">' + escapeHtml(tr('notifFormContent')) + '</label>'
      + '<div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">'
      + '<div><textarea id="notifInputContent" maxlength="2000" rows="8" placeholder="' + escapeHtml(tr('notifFormContentPlaceholder')) + '" oninput="notifUpdatePreview()"></textarea>'
      + '<div id="notifContentCharCount" class="item-meta" style="text-align:right;margin-top:4px">' + tr('notifFormContentChars', { count: '0' }) + '</div></div>'
      + '<div id="notifPreviewPane" style="border:1px solid var(--line);border-radius:12px;padding:10px 12px;min-height:180px;background:#fff;overflow:auto;font-size:13px;line-height:1.6">'
      + '<div class="item-meta">' + escapeHtml(tr('notifPreview')) + '</div></div></div></div>'
      // Category
      + '<div><label for="notifInputCategory">' + escapeHtml(tr('notifFormCategory')) + '</label>'
      + '<select id="notifInputCategory">'
      + '<option value="system_announcement">' + escapeHtml(tr('notifCategorySystem')) + '</option>'
      + '<option value="feature_update">' + escapeHtml(tr('notifCategoryFeature')) + '</option>'
      + '<option value="security_alert">' + escapeHtml(tr('notifCategorySecurity')) + '</option>'
      + '<option value="maintenance">' + escapeHtml(tr('notifCategoryMaintenance')) + '</option>'
      + '<option value="custom">' + escapeHtml(tr('notifCategoryCustom')) + '</option>'
      + '</select></div>'
      // Audience
      + '<div><label for="notifInputAudience">' + escapeHtml(tr('notifFormAudience')) + '</label>'
      + '<select id="notifInputAudience" onchange="notifAudienceChanged()">'
      + '<option value="all">' + escapeHtml(tr('notifAudienceAll')) + '</option>'
      + '<option value="tenant">' + escapeHtml(tr('notifAudienceTenant')) + '</option>'
      + '<option value="department">' + escapeHtml(tr('notifAudienceDepartment')) + '</option>'
      + '<option value="user">' + escapeHtml(tr('notifAudienceUser')) + '</option>'
      + '</select></div>'
      + '<div id="notifAudienceIdsWrap" class="hidden"><label for="notifInputAudienceIds">' + escapeHtml(tr('notifFormAudienceIds')) + '</label>'
      + '<input id="notifInputAudienceIds" placeholder="id1, id2, ..."></div>'
      // Priority
      + '<div><label>' + escapeHtml(tr('notifFormPriority')) + '</label>'
      + '<div style="display:flex;gap:14px">'
      + '<label style="display:flex;align-items:center;gap:5px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px"><input type="radio" name="notifPriority" value="normal" checked> ' + escapeHtml(tr('notifPriorityNormal')) + '</label>'
      + '<label style="display:flex;align-items:center;gap:5px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px"><input type="radio" name="notifPriority" value="important"> ' + escapeHtml(tr('notifPriorityImportant')) + '</label>'
      + '<label style="display:flex;align-items:center;gap:5px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px"><input type="radio" name="notifPriority" value="urgent"> ' + escapeHtml(tr('notifPriorityUrgent')) + '</label>'
      + '</div></div>'
      // IM Push (only visible for urgent)
      + '<div id="notifImPushWrap" class="hidden"><label style="display:flex;align-items:center;gap:8px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px">'
      + '<input type="checkbox" id="notifInputImPush"> ' + escapeHtml(tr('notifFormImPush')) + '</label></div>'
      // Publish time
      + '<div><label>' + escapeHtml(tr('notifFormPublishAt')) + '</label>'
      + '<div style="display:flex;gap:14px;align-items:center">'
      + '<label style="display:flex;align-items:center;gap:5px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px"><input type="radio" name="notifPublishMode" value="now" checked onchange="notifPublishModeChanged()"> ' + escapeHtml(tr('notifFormPublishNow')) + '</label>'
      + '<label style="display:flex;align-items:center;gap:5px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px"><input type="radio" name="notifPublishMode" value="scheduled" onchange="notifPublishModeChanged()"> ' + escapeHtml(tr('notifFormPublishScheduled')) + '</label>'
      + '</div>'
      + '<div id="notifPublishAtWrap" class="hidden" style="margin-top:8px"><input type="datetime-local" id="notifInputPublishAt"></div></div>'
      // Expire time
      + '<div><label for="notifInputExpireAt">' + escapeHtml(tr('notifFormExpireAt')) + '</label>'
      + '<input type="datetime-local" id="notifInputExpireAt"></div>'
      // Actions
      + '<div class="actions" style="margin-top:8px">'
      + '<button class="btn-secondary" type="button" onclick="notifSubmitCreate(\'draft\')">' + escapeHtml(tr('notifFormSaveDraft')) + '</button>'
      + '<button class="btn-primary" type="button" onclick="notifSubmitCreate(\'publish\')">' + escapeHtml(tr('notifFormPublish')) + '</button>'
      + '</div></div>';
    panel.innerHTML = html;
    // Attach priority change listener
    document.querySelectorAll('input[name="notifPriority"]').forEach(function(radio) {
      radio.addEventListener('change', notifPriorityChanged);
    });
  }

  // --- Detail View ---
  function renderDetailView(data) {
    const panel = getPanel();
    if (!panel || !data) return;
    notifDetailData = data;
    const stats = data.stats || {};
    const totalPushed = stats.total_pushed || 0;
    const readCount = stats.read_count || 0;
    const readRate = totalPushed > 0 ? Math.round((readCount / totalPushed) * 100) + '%' : '-';
    const canRevoke = data.status === 'published' && !isExpired(data.expire_at);
    // Simple Markdown to HTML (basic: bold, italic, code, headings, line breaks)
    const contentHtml = simpleMarkdownRender(data.content || '');

    const html = '<div class="head"><div><h3>' + escapeHtml(tr('notifDetailTitle')) + '</h3></div>'
      + '<button class="btn-ghost" type="button" onclick="notifShowList()">' + escapeHtml(tr('notifDetailBack')) + '</button></div>'
      + '<div style="display:grid;gap:14px">'
      // Header info
      + '<div class="item" style="padding:12px 14px">'
      + '<div style="display:flex;gap:8px;align-items:center;margin-bottom:6px">'
      + '<span class="badge ' + statusBadgeClass(data.status) + '">' + escapeHtml(statusLabel(data.status)) + '</span>'
      + '<span class="badge info">' + escapeHtml(categoryLabel(data.category)) + '</span>'
      + (data.priority === 'urgent' ? '<span class="badge danger">URGENT</span>' : '')
      + (data.priority === 'important' ? '<span class="badge warn">IMPORTANT</span>' : '')
      + '</div>'
      + '<div class="item-title" style="font-size:16px">' + escapeHtml(data.title || '') + '</div>'
      + '<div class="item-meta">' + escapeHtml(tr('notifCreatedAt')) + ': ' + escapeHtml(formatTime(data.created_at)) + '</div>'
      + '</div>'
      // Content
      + '<div class="item" style="padding:14px 16px">'
      + '<div class="item-title" style="font-size:13px;margin-bottom:8px">' + escapeHtml(tr('notifDetailContent')) + '</div>'
      + '<div style="font-size:13px;line-height:1.7">' + contentHtml + '</div>'
      + '</div>'
      // Statistics
      + '<div class="item" style="padding:14px 16px">'
      + '<div class="item-title" style="font-size:13px;margin-bottom:10px">' + escapeHtml(tr('notifDetailStats')) + '</div>'
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:10px">'
      + '<div class="metric"><label>' + escapeHtml(tr('notifStatTotal')) + '</label><strong>' + totalPushed + '</strong></div>'
      + '<div class="metric"><label>' + escapeHtml(tr('notifStatRead')) + '</label><strong>' + readCount + '</strong></div>'
      + '<div class="metric"><label>' + escapeHtml(tr('notifStatRate')) + '</label><strong>' + readRate + '</strong></div>'
      + '</div></div>'
      // Revoke action
      + (canRevoke ? '<div class="actions"><button class="btn-danger" type="button" onclick="notifRevokeItem(\'' + escapeHtml(data.id) + '\')">' + escapeHtml(tr('notifRevoke')) + '</button></div>' : '')
      + '</div>';
    panel.innerHTML = html;
  }

  function isExpired(expireAt) {
    if (!expireAt) return false;
    try { return new Date(expireAt) <= new Date(); } catch (_) { return false; }
  }

  function simpleMarkdownRender(md) {
    if (!md) return '';
    let html = escapeHtml(md);
    // Headings
    html = html.replace(/^### (.+)$/gm, '<h4 style="margin:8px 0 4px;font-size:14px">$1</h4>');
    html = html.replace(/^## (.+)$/gm, '<h3 style="margin:10px 0 4px;font-size:15px">$1</h3>');
    html = html.replace(/^# (.+)$/gm, '<h2 style="margin:12px 0 6px;font-size:16px">$1</h2>');
    // Bold
    html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    // Italic
    html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
    // Inline code
    html = html.replace(/`([^`]+)`/g, '<code style="background:#f4f0ef;padding:1px 5px;border-radius:4px;font-size:12px">$1</code>');
    // Line breaks
    html = html.replace(/\n/g, '<br>');
    return html;
  }

  // --- Global handlers ---
  global.notifShowList = function() {
    notifState.currentView = 'list';
    renderListView();
  };

  global.notifShowCreate = function() {
    notifState.currentView = 'create';
    renderCreateView();
  };

  global.notifShowDetail = async function(id) {
    try {
      const data = await api('/api/v1/admin/notifications/' + encodeURIComponent(id));
      notifState.currentView = 'detail';
      renderDetailView(data);
    } catch (err) {
      showToast(err.message, 'error');
    }
  };

  global.notifApplyFilters = function() {
    const statusEl = document.getElementById('notifStatusFilter');
    const categoryEl = document.getElementById('notifCategoryFilter');
    if (statusEl) notifState.statusFilter = statusEl.value;
    if (categoryEl) notifState.categoryFilter = categoryEl.value;
    loadNotifications();
  };

  global.notifRevokeItem = async function(id) {
    if (!confirm(tr('notifRevokeConfirm'))) return;
    try {
      await api('/api/v1/admin/notifications/' + encodeURIComponent(id) + '/revoke', { method: 'POST' });
      showToast(tr('notifRevokeSuccess'), 'info');
      if (notifState.currentView === 'detail') {
        global.notifShowDetail(id);
      } else {
        loadNotifications();
      }
    } catch (err) {
      showToast(tr('notifRevokeFailed', { error: err.message }), 'error');
    }
  };

  global.notifAudienceChanged = function() {
    const audienceEl = document.getElementById('notifInputAudience');
    const idsWrap = document.getElementById('notifAudienceIdsWrap');
    if (!audienceEl || !idsWrap) return;
    if (audienceEl.value === 'all') {
      idsWrap.classList.add('hidden');
    } else {
      idsWrap.classList.remove('hidden');
    }
  };

  global.notifPriorityChanged = function() {
    const selected = document.querySelector('input[name="notifPriority"]:checked');
    const imWrap = document.getElementById('notifImPushWrap');
    if (!imWrap) return;
    if (selected && selected.value === 'urgent') {
      imWrap.classList.remove('hidden');
    } else {
      imWrap.classList.add('hidden');
    }
  };

  global.notifPublishModeChanged = function() {
    const selected = document.querySelector('input[name="notifPublishMode"]:checked');
    const wrap = document.getElementById('notifPublishAtWrap');
    if (!wrap) return;
    if (selected && selected.value === 'scheduled') {
      wrap.classList.remove('hidden');
    } else {
      wrap.classList.add('hidden');
    }
  };

  global.notifUpdatePreview = function() {
    const textarea = document.getElementById('notifInputContent');
    const preview = document.getElementById('notifPreviewPane');
    const charCount = document.getElementById('notifContentCharCount');
    if (!textarea) return;
    const content = textarea.value || '';
    if (preview) {
      preview.innerHTML = '<div class="item-meta" style="margin-bottom:6px">' + escapeHtml(tr('notifPreview')) + '</div>' + simpleMarkdownRender(content);
    }
    if (charCount) {
      charCount.textContent = tr('notifFormContentChars', { count: String(content.length) });
    }
  };

  global.notifSubmitCreate = async function(mode) {
    const title = (document.getElementById('notifInputTitle') || {}).value || '';
    const content = (document.getElementById('notifInputContent') || {}).value || '';
    const category = (document.getElementById('notifInputCategory') || {}).value || 'system_announcement';
    const audienceType = (document.getElementById('notifInputAudience') || {}).value || 'all';
    const audienceIdsRaw = (document.getElementById('notifInputAudienceIds') || {}).value || '';
    const priority = (document.querySelector('input[name="notifPriority"]:checked') || {}).value || 'normal';
    const imPush = !!(document.getElementById('notifInputImPush') || {}).checked;
    const publishMode = (document.querySelector('input[name="notifPublishMode"]:checked') || {}).value || 'now';
    const publishAtRaw = (document.getElementById('notifInputPublishAt') || {}).value || '';
    const expireAtRaw = (document.getElementById('notifInputExpireAt') || {}).value || '';

    // Validate
    if (!title.trim()) { showToast('Title is required', 'error'); return; }
    if (title.length > 100) { showToast('Title exceeds 100 characters', 'error'); return; }
    if (!content.trim()) { showToast('Content is required', 'error'); return; }
    if (content.length > 2000) { showToast('Content exceeds 2000 characters', 'error'); return; }

    const audienceIds = audienceType === 'all' ? [] :
      audienceIdsRaw.split(',').map(function(s) { return s.trim(); }).filter(Boolean);

    const body = {
      title: title.trim(),
      content: content,
      category: category,
      priority: priority,
      audience_type: audienceType,
      audience_ids: audienceIds,
      im_push: imPush && priority === 'urgent',
      status: mode === 'publish' ? 'published' : 'draft'
    };

    if (publishMode === 'scheduled' && publishAtRaw) {
      body.publish_at = new Date(publishAtRaw).toISOString();
    }
    if (expireAtRaw) {
      body.expire_at = new Date(expireAtRaw).toISOString();
    }

    try {
      await api('/api/v1/admin/notifications', {
        method: 'POST',
        body: JSON.stringify(body)
      });
      showToast(tr('notifCreateSuccess'), 'info');
      notifState.currentView = 'list';
      renderListView();
    } catch (err) {
      showToast(tr('notifCreateFailed', { error: err.message }), 'error');
    }
  };

  // --- Tab Registration ---
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'notifications',
      title: function() { return tr('notifTabTitle'); },
      subtitle: function() { return tr('notifTabSubtitle'); },
      onOpen: function() {
        notifState.currentView = 'list';
        renderListView();
      }
    });
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() {
      if (notifState.currentView === 'list') renderListView();
      else if (notifState.currentView === 'create') renderCreateView();
      else if (notifState.currentView === 'detail' && notifDetailData) renderDetailView(notifDetailData);
    });
  }
})(window);
