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
      notifDelete: 'Delete',
      notifDeleteConfirm: 'Permanently delete this notification?',
      notifDeleteSuccess: 'Notification deleted.',
      notifDeleteFailed: 'Delete failed: {error}',
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
      notifFormPinned: 'Pinned message (floating banner)',
      notifFormPinnedForever: 'Permanent pin (always leftmost)',
      notifFormLink: 'Link (optional)',
      notifFormLinkPlaceholder: 'https://... opened by the banner button',
      notifFormButtonText: 'Button label',
      notifFormButtonTextPlaceholder: 'View details',
      notifFormSourceLang: 'Written in',
      notifFormTranslateTargets: 'Also save in',
      notifFormTranslateHint: 'Checked languages are translated from the original. The link is not translated.',
      notifLangZhHans: 'Simplified Chinese',
      notifLangZhHant: 'Traditional Chinese',
      notifLangEn: 'English',
      notifLangMn: 'Cyrillic Mongolian',
      notifBadgePinned: 'Pinned',
      notifBadgeUrgent: 'Urgent',
      notifBadgeImportant: 'Important',
      notifFormPublishAt: 'Publish time',
      notifFormPublishNow: 'Publish now',
      notifFormPublishScheduled: 'Scheduled',
      notifFormExpireAt: 'Expire time (optional)',
      notifFormSaveDraft: 'Save draft',
      notifFormPublish: 'Publish now',
      notifCreateSuccess: 'Notification created.',
      notifScheduledSaved: 'Saved. It will be sent at the scheduled time.',
      notifCreateFailed: 'Create failed: {error}',
      notifDetailTitle: 'Notification detail',
      notifDetailBack: 'Back to list',
      notifDetailContent: 'Content',
      notifDetailStats: 'Delivery statistics',
      notifStatTotal: 'Total pushed',
      notifStatRead: 'Read',
      notifStatRate: 'Read rate',
      notifPreview: 'Preview',
      notifFormContentChars: '{count}/2000',
      notifErrTitleRequired: 'Title is required',
      notifErrTitleTooLong: 'Title exceeds 100 characters',
      notifErrContentRequired: 'Content is required',
      notifErrContentTooLong: 'Content exceeds 2000 characters',
      notifErrLink: 'Link must start with http:// or https://',
      notifErrPublishAt: 'Choose a publish time',
      notifErrPublishFuture: 'Publish time must be in the future',
      notifErrExpireAt: 'Expire time is not a valid date',
      notifErrExpireOrder: 'Expire time must be after the publish time'
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
      notifDelete: '\u5220\u9664',
      notifDeleteConfirm: '\u786e\u8ba4\u6c38\u4e45\u5220\u9664\u6b64\u901a\u77e5\uff1f',
      notifDeleteSuccess: '\u901a\u77e5\u5df2\u5220\u9664\u3002',
      notifDeleteFailed: '\u5220\u9664\u5931\u8d25\uff1a{error}',
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
      notifFormPinned: '\u7f6e\u9876\u6d88\u606f\uff08\u60ac\u6d6e\u6761\uff09',
      notifFormPinnedForever: '\u6c38\u4e45\u7f6e\u9876\uff08\u59cb\u7ec8\u5728\u6700\u5de6\uff09',
      notifFormLink: '\u94fe\u63a5\uff08\u53ef\u9009\uff09',
      notifFormLinkPlaceholder: 'https://... \u7531\u6a2a\u5e45\u6309\u94ae\u6253\u5f00',
      notifFormButtonText: '\u6309\u94ae\u6587\u5b57',
      notifFormButtonTextPlaceholder: '\u67e5\u770b\u8be6\u60c5',
      notifFormSourceLang: '\u539f\u6587\u8bed\u8a00',
      notifFormTranslateTargets: '\u540c\u65f6\u4fdd\u5b58\u4e3a',
      notifFormTranslateHint: '\u52fe\u9009\u7684\u8bed\u8a00\u4f1a\u4ece\u539f\u6587\u7ffb\u8bd1\u3002\u94fe\u63a5\u4e0d\u7ffb\u8bd1\u3002',
      notifLangZhHans: '\u7b80\u4f53\u4e2d\u6587',
      notifLangZhHant: '\u7e41\u4f53\u4e2d\u6587',
      notifLangEn: '\u82f1\u6587',
      notifLangMn: '\u897f\u91cc\u5c14\u8499\u53e4\u6587',
      notifBadgePinned: '\u7f6e\u9876',
      notifBadgeUrgent: '\u7d27\u6025',
      notifBadgeImportant: '\u91cd\u8981',
      notifFormPublishAt: '\u53d1\u5e03\u65f6\u95f4',
      notifFormPublishNow: '\u7acb\u5373\u53d1\u5e03',
      notifFormPublishScheduled: '\u5b9a\u65f6\u53d1\u5e03',
      notifFormExpireAt: '\u8fc7\u671f\u65f6\u95f4\uff08\u53ef\u9009\uff09',
      notifFormSaveDraft: '\u4fdd\u5b58\u8349\u7a3f',
      notifFormPublish: '\u7acb\u5373\u53d1\u5e03',
      notifCreateSuccess: '\u901a\u77e5\u521b\u5efa\u6210\u529f\u3002',
      notifScheduledSaved: '\u5df2\u4fdd\u5b58\uff0c\u5230\u53d1\u5e03\u65f6\u95f4\u540e\u53d1\u51fa\u3002',
      notifCreateFailed: '\u521b\u5efa\u5931\u8d25\uff1a{error}',
      notifDetailTitle: '\u901a\u77e5\u8be6\u60c5',
      notifDetailBack: '\u8fd4\u56de\u5217\u8868',
      notifDetailContent: '\u5185\u5bb9',
      notifDetailStats: '\u9001\u8fbe\u7edf\u8ba1',
      notifStatTotal: '\u603b\u63a8\u9001\u6570',
      notifStatRead: '\u5df2\u8bfb\u6570',
      notifStatRate: '\u5df2\u8bfb\u7387',
      notifPreview: '\u9884\u89c8',
      notifFormContentChars: '{count}/2000',
      notifErrTitleRequired: '\u8bf7\u586b\u5199\u6807\u9898',
      notifErrTitleTooLong: '\u6807\u9898\u4e0d\u80fd\u8d85\u8fc7 100 \u4e2a\u5b57\u7b26',
      notifErrContentRequired: '\u8bf7\u586b\u5199\u5185\u5bb9',
      notifErrContentTooLong: '\u5185\u5bb9\u4e0d\u80fd\u8d85\u8fc7 2000 \u4e2a\u5b57\u7b26',
      notifErrLink: '\u94fe\u63a5\u5fc5\u987b\u4ee5 http:// \u6216 https:// \u5f00\u5934',
      notifErrPublishAt: '\u8bf7\u9009\u62e9\u53d1\u5e03\u65f6\u95f4',
      notifErrPublishFuture: '\u53d1\u5e03\u65f6\u95f4\u987b\u665a\u4e8e\u73b0\u5728',
      notifErrExpireAt: '\u8fc7\u671f\u65f6\u95f4\u4e0d\u662f\u6709\u6548\u65e5\u671f',
      notifErrExpireOrder: '\u8fc7\u671f\u65f6\u95f4\u987b\u665a\u4e8e\u53d1\u5e03\u65f6\u95f4'
    });
    var notificationMn = (global.HUB_ADMIN_DICT_MN && global.HUB_ADMIN_DICT_MN.NOTIFICATION) || {};
    I18N.mn = Object.assign({}, I18N.mn || {}, notificationMn);
  }

  // --- State ---
  const notifState = {
    items: [],
    total: 0,
    statusFilter: '',
    categoryFilter: '',
    currentView: 'list', // 'list' | 'create' | 'detail'
    submitting: false
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

  function notifAudienceLine(data) {
    var type = data && data.audience_type || '';
    var keys = {
      all: 'notifAudienceAll',
      tenant: 'notifAudienceTenant',
      department: 'notifAudienceDepartment',
      user: 'notifAudienceUser'
    };
    if (!keys[type]) return '';
    var label = tr(keys[type]);
    var ids = (data.audience_ids || []).filter(Boolean);
    if (type !== 'all' && ids.length) label += ': ' + ids.join(', ');
    return label;
  }

  function notifTimeMeta(data) {
    var parts = [];
    if (data && data.publish_at) parts.push(tr('notifFormPublishAt') + ': ' + formatTime(data.publish_at));
    if (data && data.expire_at) parts.push(tr('notifFormExpireAt') + ': ' + formatTime(data.expire_at));
    var audience = notifAudienceLine(data);
    if (audience) parts.push(audience);
    return parts.join(' | ');
  }

  function deliveryRateText(stats) {
    if (!stats || !stats.total_pushed) return '-';
    const rate = stats.total_pushed > 0
      ? Math.round((stats.read_count / stats.total_pushed) * 100)
      : 0;
    return rate + '% (' + stats.read_count + '/' + stats.total_pushed + ')';
  }

  function notificationItemsFromResponse(data) {
    if (Array.isArray(data)) return data;
    if (data && Array.isArray(data.items)) return data.items;
    if (data && Array.isArray(data.notifications)) return data.notifications;
    return [];
  }

  function notificationDetailFromResponse(data) {
    if (!data) return null;
    if (data.notification) {
      data.notification.stats = data.stats || data.notification.stats || null;
      return data.notification;
    }
    return data;
  }

  function notifJsArg(value) {
    return escapeHtml(JSON.stringify(String(value || '')));
  }

  function canDeleteNotification(item) {
    return !!item && (item.status === 'revoked' || item.status === 'draft' || item.status === 'expired');
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
      notifState.items = notificationItemsFromResponse(data);
      notifState.total = Number(data && data.total || notifState.items.length);
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
      const canDelete = canDeleteNotification(item);
      const idArg = notifJsArg(item.id);
      const revokeBtn = canRevoke
        ? '<button class="btn-danger" style="height:30px;padding:0 10px;font-size:11px" onclick="notifRevokeItem(' + idArg + ')">' + escapeHtml(tr('notifRevoke')) + '</button>'
        : '';
      const deleteBtn = canDelete
        ? '<button class="btn-danger" style="height:30px;padding:0 10px;font-size:11px" onclick="notifDeleteItem(' + idArg + ')">' + escapeHtml(tr('notifDelete')) + '</button>'
        : '';
      return '<div class="item" style="padding:12px 14px">'
        + '<div style="display:flex;justify-content:space-between;align-items:flex-start;gap:10px">'
        + '<div style="min-width:0;flex:1">'
        + '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:4px">'
        + '<span class="badge ' + statusBadgeClass(item.status) + '">' + escapeHtml(statusLabel(item.status)) + '</span>'
        + '<span class="badge info">' + escapeHtml(categoryLabel(item.category)) + '</span>'
        + (item.pinned ? '<span class="badge warn">' + escapeHtml(tr('notifBadgePinned')) + '</span>' : '')
        + (item.priority === 'urgent' ? '<span class="badge danger">' + escapeHtml(tr('notifBadgeUrgent')) + '</span>' : '')
        + (item.priority === 'important' ? '<span class="badge warn">' + escapeHtml(tr('notifBadgeImportant')) + '</span>' : '')
        + '</div>'
        + '<div class="item-title" style="font-size:14px;margin-bottom:3px">' + escapeHtml(item.title || '-') + '</div>'
        + '<div class="item-meta">' + escapeHtml(tr('notifCreatedAt')) + ': ' + escapeHtml(formatTime(item.created_at))
        + (item.publish_at ? ' | ' + escapeHtml(tr('notifFormPublishAt')) + ': ' + escapeHtml(formatTime(item.publish_at)) : '')
        + ' &nbsp;|&nbsp; ' + escapeHtml(tr('notifDeliveryRate')) + ': ' + escapeHtml(rate) + '</div>'
        + '</div>'
        + '<div style="display:flex;gap:6px;flex-shrink:0">'
        + '<button class="btn-ghost" style="height:30px;padding:0 10px;font-size:11px" onclick="notifShowDetail(' + idArg + ')">' + escapeHtml(tr('notifView')) + '</button>'
        + revokeBtn
        + deleteBtn
        + '</div></div></div>';
    }).join('');
    root.innerHTML = cards;
  }

  function notifDefaultSourceLang() {
    var lang = (typeof currentLang === 'string') ? currentLang : 'en';
    if (lang === 'zh') return 'zh-Hans';
    if (lang === 'mn') return 'mn';
    return 'en';
  }

  // Empty pinned buttons are stored as the source language's own label.
  // The placeholder has to show that label, not the console language.
  function notifDefaultButtonText(lang) {
    switch (lang) {
      case 'zh-Hant': return '\u67e5\u770b\u8a73\u60c5';
      case 'en': return 'View details';
      case 'mn': return '\u0414\u044d\u043b\u0433\u044d\u0440\u044d\u043d\u0433\u04af\u0439 \u0445\u0430\u0440\u0430\u0445';
      default: return '\u67e5\u770b\u8be6\u60c5';
    }
  }

  function notifSetSubmitLocked(locked) {
    ['notifSubmitDraft', 'notifSubmitPublish'].forEach(function(id) {
      var el = document.getElementById(id);
      if (el) el.disabled = locked;
    });
  }

  global.notifSyncButtonPlaceholder = function() {
    var input = document.getElementById('notifInputButtonText');
    if (!input) return;
    var sel = document.getElementById('notifInputSourceLang');
    var lang = (sel && sel.value) || notifDefaultSourceLang();
    input.placeholder = notifDefaultButtonText(lang);
  };

  var notifLangChoices = [
    ['zh-Hans', 'notifLangZhHans'],
    ['zh-Hant', 'notifLangZhHant'],
    ['en', 'notifLangEn'],
    ['mn', 'notifLangMn']
  ];

  function notifSourceLangOptions() {
    var selected = notifDefaultSourceLang();
    return notifLangChoices.map(function(opt) {
      return '<option value="' + opt[0] + '"' + (opt[0] === selected ? ' selected' : '') + '>' + escapeHtml(tr(opt[1])) + '</option>';
    }).join('');
  }

  function notifTargetLangChecks() {
    return notifLangChoices.map(function(opt) {
      return '<label style="display:flex;align-items:center;gap:5px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px"><input type="checkbox" class="notif-target-lang" value="' + opt[0] + '" checked> ' + escapeHtml(tr(opt[1])) + '</label>';
    }).join('');
  }

  function notifParseLocalDate(value) {
    var raw = String(value || '').trim();
    if (!raw) return null;
    var parsed = new Date(raw);
    if (isNaN(parsed.getTime())) return null;
    return parsed;
  }

  function readNotifCreateDraft() {
    if (!document.getElementById('notifInputTitle')) return null;
    function val(id) {
      var el = document.getElementById(id);
      return el ? el.value : '';
    }
    function on(id) {
      var el = document.getElementById(id);
      return !!(el && el.checked);
    }
    var langs = [];
    document.querySelectorAll('.notif-target-lang').forEach(function(el) {
      langs.push({ value: el.value, checked: !!el.checked });
    });
    return {
      title: val('notifInputTitle'),
      content: val('notifInputContent'),
      sourceLang: val('notifInputSourceLang'),
      category: val('notifInputCategory'),
      audience: val('notifInputAudience'),
      audienceIds: val('notifInputAudienceIds'),
      priority: (document.querySelector('input[name="notifPriority"]:checked') || {}).value || 'normal',
      publish: (document.querySelector('input[name="notifPublishMode"]:checked') || {}).value || 'now',
      publishAt: val('notifInputPublishAt'),
      expireAt: val('notifInputExpireAt'),
      imPush: on('notifInputImPush'),
      pinned: on('notifInputPinned'),
      pinnedForever: on('notifInputPinnedForever'),
      link: val('notifInputLinkUrl'),
      button: val('notifInputButtonText'),
      langs: langs
    };
  }

  function applyNotifCreateDraft(draft) {
    if (!draft) return;
    function set(id, value) {
      var el = document.getElementById(id);
      if (el && value != null) el.value = value;
    }
    set('notifInputTitle', draft.title);
    set('notifInputContent', draft.content);
    set('notifInputSourceLang', draft.sourceLang);
    set('notifInputCategory', draft.category);
    set('notifInputAudience', draft.audience);
    set('notifInputAudienceIds', draft.audienceIds);
    set('notifInputPublishAt', draft.publishAt);
    set('notifInputExpireAt', draft.expireAt);
    set('notifInputLinkUrl', draft.link);
    set('notifInputButtonText', draft.button);
    document.querySelectorAll('input[name="notifPriority"]').forEach(function(el) {
      el.checked = el.value === draft.priority;
    });
    document.querySelectorAll('input[name="notifPublishMode"]').forEach(function(el) {
      el.checked = el.value === draft.publish;
    });
    var im = document.getElementById('notifInputImPush');
    if (im) im.checked = !!draft.imPush;
    var pinned = document.getElementById('notifInputPinned');
    if (pinned) pinned.checked = !!draft.pinned;
    var forever = document.getElementById('notifInputPinnedForever');
    if (forever) forever.checked = !!draft.pinnedForever;
    if (draft.langs) {
      var wanted = {};
      draft.langs.forEach(function(item) { wanted[item.value] = !!item.checked; });
      document.querySelectorAll('.notif-target-lang').forEach(function(el) {
        if (Object.prototype.hasOwnProperty.call(wanted, el.value)) el.checked = wanted[el.value];
      });
    }
    global.notifAudienceChanged();
    global.notifPriorityChanged();
    global.notifPinnedChanged();
    global.notifPublishModeChanged();
    global.notifSyncButtonPlaceholder();
    global.notifUpdatePreview();
  }

  function notifSelectedTargetLangs() {
    var langs = [];
    document.querySelectorAll('.notif-target-lang:checked').forEach(function(el) {
      langs.push(el.value);
    });
    return langs;
  }

  // --- Create View ---
  function renderCreateView(draft) {
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
      + '<div><label for="notifInputSourceLang">' + escapeHtml(tr('notifFormSourceLang')) + '</label>'
      + '<select id="notifInputSourceLang" onchange="notifSyncButtonPlaceholder()">' + notifSourceLangOptions() + '</select>'
      + '<div class="item-meta" style="margin-top:6px">' + escapeHtml(tr('notifFormTranslateTargets')) + '</div>'
      + '<div style="display:flex;flex-wrap:wrap;gap:12px">' + notifTargetLangChecks() + '</div>'
      + '<div class="item-meta">' + escapeHtml(tr('notifFormTranslateHint')) + '</div></div>'
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
      // Pinned message (floating banner)
      + '<div><label style="display:flex;align-items:center;gap:8px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px">'
      + '<input type="checkbox" id="notifInputPinned" onchange="notifPinnedChanged()"> ' + escapeHtml(tr('notifFormPinned')) + '</label></div>'
      + '<div id="notifPinnedExtras" class="hidden" style="display:grid;gap:12px">'
      + '<div><label style="display:flex;align-items:center;gap:8px;cursor:pointer;margin-bottom:0;text-transform:none;font-weight:normal;font-size:13px">'
      + '<input type="checkbox" id="notifInputPinnedForever"> ' + escapeHtml(tr('notifFormPinnedForever')) + '</label></div>'
      + '<div><label for="notifInputLinkUrl">' + escapeHtml(tr('notifFormLink')) + '</label>'
      + '<input id="notifInputLinkUrl" maxlength="500" placeholder="' + escapeHtml(tr('notifFormLinkPlaceholder')) + '"></div>'
      + '<div><label for="notifInputButtonText">' + escapeHtml(tr('notifFormButtonText')) + '</label>'
      + '<input id="notifInputButtonText" maxlength="20" placeholder="' + escapeHtml(notifDefaultButtonText(notifDefaultSourceLang())) + '"></div>'
      + '</div>'
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
      + '<button id="notifSubmitDraft" class="btn-secondary" type="button" onclick="notifSubmitCreate(\'draft\')">' + escapeHtml(tr('notifFormSaveDraft')) + '</button>'
      + '<button id="notifSubmitPublish" class="btn-primary" type="button" onclick="notifSubmitCreate(\'publish\')">' + escapeHtml(tr('notifFormPublish')) + '</button>'
      + '</div></div>';
    panel.innerHTML = html;
    // Attach priority change listener
    document.querySelectorAll('input[name="notifPriority"]').forEach(function(radio) {
      radio.addEventListener('change', global.notifPriorityChanged);
    });
    applyNotifCreateDraft(draft);
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
    const canDelete = canDeleteNotification(data);
    // Simple Markdown to HTML (basic: bold, italic, code, headings, line breaks)
    const contentHtml = simpleMarkdownRender(data.content || '');
    const scheduleMeta = notifTimeMeta(data);

    const html = '<div class="head"><div><h3>' + escapeHtml(tr('notifDetailTitle')) + '</h3></div>'
      + '<button class="btn-ghost" type="button" onclick="notifShowList()">' + escapeHtml(tr('notifDetailBack')) + '</button></div>'
      + '<div style="display:grid;gap:14px">'
      // Header info
      + '<div class="item" style="padding:12px 14px">'
      + '<div style="display:flex;gap:8px;align-items:center;margin-bottom:6px">'
      + '<span class="badge ' + statusBadgeClass(data.status) + '">' + escapeHtml(statusLabel(data.status)) + '</span>'
      + '<span class="badge info">' + escapeHtml(categoryLabel(data.category)) + '</span>'
      + (data.pinned ? '<span class="badge warn">' + escapeHtml(tr('notifBadgePinned')) + '</span>' : '')
      + (data.priority === 'urgent' ? '<span class="badge danger">' + escapeHtml(tr('notifBadgeUrgent')) + '</span>' : '')
      + (data.priority === 'important' ? '<span class="badge warn">' + escapeHtml(tr('notifBadgeImportant')) + '</span>' : '')
      + '</div>'
      + '<div class="item-title" style="font-size:16px">' + escapeHtml(data.title || '') + '</div>'
      + '<div class="item-meta">' + escapeHtml(tr('notifCreatedAt')) + ': ' + escapeHtml(formatTime(data.created_at)) + '</div>'
      + (scheduleMeta ? '<div class="item-meta">' + escapeHtml(scheduleMeta) + '</div>' : '')
      + '</div>'
      // Content
      + '<div class="item" style="padding:14px 16px">'
      + '<div class="item-title" style="font-size:13px;margin-bottom:8px">' + escapeHtml(tr('notifDetailContent')) + '</div>'
      + '<div style="font-size:13px;line-height:1.7">' + contentHtml + '</div>'
      + (data.link_url ? '<div class="item-meta" style="margin-top:10px">' + escapeHtml(tr('notifFormLink')) + ': <a href="' + escapeHtml(data.link_url) + '" target="_blank" rel="noopener noreferrer">' + escapeHtml(data.link_url) + '</a>'
        + (data.button_text ? ' &nbsp;|&nbsp; ' + escapeHtml(tr('notifFormButtonText')) + ': ' + escapeHtml(data.button_text) : '') + '</div>' : '')
      + '</div>'
      // Statistics
      + '<div class="item" style="padding:14px 16px">'
      + '<div class="item-title" style="font-size:13px;margin-bottom:10px">' + escapeHtml(tr('notifDetailStats')) + '</div>'
      + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:10px">'
      + '<div class="metric"><label>' + escapeHtml(tr('notifStatTotal')) + '</label><strong>' + totalPushed + '</strong></div>'
      + '<div class="metric"><label>' + escapeHtml(tr('notifStatRead')) + '</label><strong>' + readCount + '</strong></div>'
      + '<div class="metric"><label>' + escapeHtml(tr('notifStatRate')) + '</label><strong>' + readRate + '</strong></div>'
      + '</div></div>'
      // Lifecycle actions
      + (canRevoke || canDelete ? '<div class="actions">'
        + (canRevoke ? '<button class="btn-danger" type="button" onclick="notifRevokeItem(' + notifJsArg(data.id) + ')">' + escapeHtml(tr('notifRevoke')) + '</button>' : '')
        + (canDelete ? '<button class="btn-danger" type="button" onclick="notifDeleteItem(' + notifJsArg(data.id) + ')">' + escapeHtml(tr('notifDelete')) + '</button>' : '')
        + '</div>' : '')
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
      const detail = notificationDetailFromResponse(data);
      if (!detail || !detail.id) throw new Error('Notification not found');
      notifState.currentView = 'detail';
      renderDetailView(detail);
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

  global.notifDeleteItem = async function(id) {
    if (!confirm(tr('notifDeleteConfirm'))) return;
    try {
      await api('/api/v1/admin/notifications/' + encodeURIComponent(id), { method: 'DELETE' });
      showToast(tr('notifDeleteSuccess'), 'info');
      if (notifState.currentView === 'detail') {
        notifState.currentView = 'list';
        renderListView();
      } else {
        loadNotifications();
      }
    } catch (err) {
      showToast(tr('notifDeleteFailed', { error: err.message }), 'error');
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

  global.notifPinnedChanged = function() {
    const pinned = document.getElementById('notifInputPinned');
    const extras = document.getElementById('notifPinnedExtras');
    if (!pinned || !extras) return;
    if (pinned.checked) {
      extras.classList.remove('hidden');
    } else {
      extras.classList.add('hidden');
    }
  };

  global.notifPublishModeChanged = function() {
    const selected = document.querySelector('input[name="notifPublishMode"]:checked');
    const wrap = document.getElementById('notifPublishAtWrap');
    const scheduled = !!(selected && selected.value === 'scheduled');
    if (wrap) wrap.classList.toggle('hidden', !scheduled);
    const btn = document.getElementById('notifSubmitPublish');
    if (btn) btn.textContent = tr(scheduled ? 'notifFormPublishScheduled' : 'notifFormPublish');
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
    const title = ((document.getElementById('notifInputTitle') || {}).value || '').trim();
    const content = ((document.getElementById('notifInputContent') || {}).value || '').trim();
    const category = (document.getElementById('notifInputCategory') || {}).value || 'system_announcement';
    const audienceType = (document.getElementById('notifInputAudience') || {}).value || 'all';
    const audienceIdsRaw = (document.getElementById('notifInputAudienceIds') || {}).value || '';
    const priority = (document.querySelector('input[name="notifPriority"]:checked') || {}).value || 'normal';
    const imPush = !!(document.getElementById('notifInputImPush') || {}).checked;
    const pinned = !!(document.getElementById('notifInputPinned') || {}).checked;
    const pinnedForever = !!(document.getElementById('notifInputPinnedForever') || {}).checked;
    const linkUrl = ((document.getElementById('notifInputLinkUrl') || {}).value || '').trim();
    const buttonText = ((document.getElementById('notifInputButtonText') || {}).value || '').trim();
    const publishMode = (document.querySelector('input[name="notifPublishMode"]:checked') || {}).value || 'now';
    const publishAtRaw = (document.getElementById('notifInputPublishAt') || {}).value || '';
    const expireAtRaw = (document.getElementById('notifInputExpireAt') || {}).value || '';

    if (!title) { showToast(tr('notifErrTitleRequired'), 'error'); return; }
    if (title.length > 100) { showToast(tr('notifErrTitleTooLong'), 'error'); return; }
    if (!content.trim()) { showToast(tr('notifErrContentRequired'), 'error'); return; }
    if (content.length > 2000) { showToast(tr('notifErrContentTooLong'), 'error'); return; }
    if (linkUrl && !/^https?:\/\//i.test(linkUrl)) { showToast(tr('notifErrLink'), 'error'); return; }
    var publishDate = null;
    if (publishMode === 'scheduled' && publishAtRaw) {
      publishDate = notifParseLocalDate(publishAtRaw);
      if (!publishDate) { showToast(tr('notifErrPublishAt'), 'error'); return; }
    }
    if (mode === 'publish' && publishMode === 'scheduled' && !publishDate) {
      showToast(tr('notifErrPublishAt'), 'error');
      return;
    }
    if (mode === 'publish' && publishDate && publishDate.getTime() <= Date.now()) {
      showToast(tr('notifErrPublishFuture'), 'error');
      return;
    }
    var expireDate = null;
    if (String(expireAtRaw || '').trim()) {
      expireDate = notifParseLocalDate(expireAtRaw);
      if (!expireDate) { showToast(tr('notifErrExpireAt'), 'error'); return; }
      var earliest = publishDate ? publishDate.getTime() : Date.now();
      if (expireDate.getTime() <= earliest) { showToast(tr('notifErrExpireOrder'), 'error'); return; }
    }
    if (notifState.submitting) return;
    notifState.submitting = true;
    notifSetSubmitLocked(true);

    const audienceIds = audienceType === 'all' ? [] :
      audienceIdsRaw.split(',').map(function(s) { return s.trim(); }).filter(Boolean);
    const sourceLang = (document.getElementById('notifInputSourceLang') || {}).value || '';

    const body = {
      title: title,
      content: content,
      category: category,
      priority: priority,
      audience_type: audienceType,
      audience_ids: audienceIds,
      im_push: imPush && priority === 'urgent',
      pinned: pinned,
      pinned_forever: pinned && pinnedForever,
      link_url: linkUrl,
      button_text: buttonText,
      source_lang: sourceLang,
      target_langs: notifSelectedTargetLangs(),
      status: mode === 'publish' ? 'published' : 'draft'
    };

    if (mode === 'publish' && publishDate) body.publish_at = publishDate.toISOString();
    if (expireDate) body.expire_at = expireDate.toISOString();

    try {
      const data = await api('/api/v1/admin/notifications', {
        method: 'POST',
        body: JSON.stringify(body)
      });
      const saved = data && data.notification;
      const scheduled = !!(saved && saved.status === 'draft' && saved.publish_at);
      showToast(tr(scheduled ? 'notifScheduledSaved' : 'notifCreateSuccess'), 'info');
      notifState.currentView = 'list';
      notifState.statusFilter = '';
      notifState.categoryFilter = '';
      renderListView();
    } catch (err) {
      showToast(tr('notifCreateFailed', { error: err.message }), 'error');
    } finally {
      notifState.submitting = false;
      notifSetSubmitLocked(false);
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
      else if (notifState.currentView === 'create') renderCreateView(readNotifCreateDraft());
      else if (notifState.currentView === 'detail' && notifDetailData) renderDetailView(notifDetailData);
    });
  }
})(window);
