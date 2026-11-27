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
  notifFormPublishNow: 'Publish now',
  notifFormSaveDraft: 'Save draft',
  notifFormCancel: 'Cancel',
  notifFormTitlePlaceholder: 'Notification title (max 100 characters)',
  notifFormContentPlaceholder: 'Notification content, Markdown (max 2000 characters)',
  notifFormSelectHubs: 'Select Hub instances...',
  notifFormSelectTenant: 'Select a Hub and tenant...',
  notifFormHub: 'Hub',
  notifFormTenant: 'Tenant',
  notifFormAddTenant: 'Add target',
  notifFormSelectedTenants: 'Selected targets',
  notifFormNoHubs: 'No hubs available',
  notifFormNoTenants: 'No tenants available',
  notifFormTenantAlreadyAdded: 'That Hub and tenant is already selected.',
  notifFormRemoveTenant: 'Remove',
  notifFormImmediate: 'Now',
  notifFormScheduled: 'Scheduled',
  notifFormPublishScheduled: 'Scheduled publish',
  notifRevoke: 'Revoke',
  notifRevokeConfirm: 'Revoke this notification? It will be removed from every client.',
  notifRevoked: 'Notification revoked.',
  notifRevokeFailed: 'Revoke failed: {error}',
  notifDelete: 'Delete',
  notifDeleteConfirm: 'Permanently delete this notification? A published notification must be revoked first.',
  notifDeleted: 'Notification deleted.',
  notifDeleteFailed: 'Delete failed: {error}',
  notifCreated: 'Notification created.',
  notifScheduledSaved: 'Saved. It will be sent at the scheduled time.',
  notifCreateFailed: 'Create failed: {error}',
  notifCascadeTitle: 'Cascade delivery',
  notifCascadeHub: 'Hub',
  notifCascadeTime: 'Pushed at',
  notifCascadeStatus: 'Status',
  notifCascadeSuccess: 'Succeeded',
  notifCascadeFailed: 'Failed',
  notifCascadePending: 'Pending',
  notifCascadeTimeout: 'Timed out',
  notifCascadeAuthFailed: 'Sign-in failed',
  notifDetailBack: 'Back to list',
  notifDetailStats: 'Delivery statistics',
  notifDetailTotal: 'Total pushed',
  notifDetailRead: 'Read',
  notifDetailReadRate: 'Read rate',
  notifValidationTitle: 'Title is required (max 100 characters)',
  notifValidationContent: 'Content is required (max 2000 characters)',
  notifValidationAudience: 'Choose an audience',
  notifValidationLink: 'Link must start with http:// or https://',
  notifValidationPublishAt: 'Choose a publish time',
  notifValidationPublishFuture: 'Publish time must be in the future',
  notifValidationExpireAt: 'Expire time is not a valid date',
  notifValidationExpireOrder: 'Expire time must be after the publish time',
  notifHubCount: '{count} Hubs',
  notifHubCountOne: '1 Hub'
});

Object.assign(I18N_ZH, {
  navNotifications: '\u901a\u77e5\u7ba1\u7406',
  navNotificationsDesc: '\u8de8 Hub \u63a8\u9001\u901a\u77e5',
  notifTitle: '\u901a\u77e5\u7ba1\u7406',
  notifDesc: '\u521b\u5efa\u3001\u76d1\u63a7\u5e76\u64a4\u56de\u9762\u5411\u7ec8\u7aef\u7528\u6237\u7684\u8de8 Hub \u63a8\u9001\u901a\u77e5\u3002',
  notifRefresh: '\u5237\u65b0',
  notifCreate: '\u521b\u5efa\u901a\u77e5',
  notifFilterAll: '\u5168\u90e8',
  notifFilterPublished: '\u5df2\u53d1\u5e03',
  notifFilterExpired: '\u5df2\u8fc7\u671f',
  notifFilterRevoked: '\u5df2\u64a4\u56de',
  notifFilterDraft: '\u8349\u7a3f',
  notifEmpty: '\u8fd8\u6ca1\u6709\u901a\u77e5\u3002',
  notifLoading: '\u6b63\u5728\u52a0\u8f7d\u901a\u77e5\u2026',
  notifLoadFailed: '\u52a0\u8f7d\u901a\u77e5\u5931\u8d25\uff1a{error}',
  notifStatusPublished: '\u5df2\u53d1\u5e03',
  notifStatusExpired: '\u5df2\u8fc7\u671f',
  notifStatusRevoked: '\u5df2\u64a4\u56de',
  notifStatusDraft: '\u8349\u7a3f',
  notifCategorySystem: '\u7cfb\u7edf',
  notifCategoryFeature: '\u529f\u80fd',
  notifCategorySecurity: '\u5b89\u5168',
  notifCategoryMaintenance: '\u7ef4\u62a4',
  notifCategoryCustom: '\u81ea\u5b9a\u4e49',
  notifPriorityNormal: '\u666e\u901a',
  notifPriorityImportant: '\u91cd\u8981',
  notifPriorityUrgent: '\u7d27\u6025',
  notifAudienceAllHubs: '\u5168\u90e8 Hub\uff08\u5e7f\u64ad\uff09',
  notifAudienceHubs: '\u6307\u5b9a Hub',
  notifAudienceTenant: 'Hub + \u79df\u6237',
  notifFormTitle: '\u6807\u9898',
  notifFormContent: '\u5185\u5bb9\uff08Markdown\uff09',
  notifFormCategory: '\u5206\u7c7b',
  notifFormAudience: '\u53d7\u4f17',
  notifFormPriority: '\u4f18\u5148\u7ea7',
  notifFormPublishAt: '\u53d1\u5e03\u65f6\u95f4',
  notifFormExpireAt: '\u8fc7\u671f\u65f6\u95f4',
  notifFormImPush: '\u540c\u65f6\u63a8\u9001\u5230 IM',
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
  notifFormPublishNow: '\u7acb\u5373\u53d1\u5e03',
  notifFormSaveDraft: '\u4fdd\u5b58\u8349\u7a3f',
  notifFormCancel: '\u53d6\u6d88',
  notifFormTitlePlaceholder: '\u901a\u77e5\u6807\u9898\uff08\u6700\u591a 100 \u5b57\u7b26\uff09',
  notifFormContentPlaceholder: '\u901a\u77e5\u5185\u5bb9\uff0cMarkdown \u683c\u5f0f\uff08\u6700\u591a 2000 \u5b57\u7b26\uff09',
  notifFormSelectHubs: '\u9009\u62e9 Hub \u5b9e\u4f8b...',
  notifFormSelectTenant: '\u9009\u62e9 Hub \u548c\u79df\u6237...',
  notifFormHub: 'Hub',
  notifFormTenant: '\u79df\u6237',
  notifFormAddTenant: '\u6dfb\u52a0\u76ee\u6807',
  notifFormSelectedTenants: '\u5df2\u9009\u76ee\u6807',
  notifFormNoHubs: '\u6682\u65e0\u53ef\u9009 Hub',
  notifFormNoTenants: '\u6682\u65e0\u53ef\u9009\u79df\u6237',
  notifFormTenantAlreadyAdded: '\u8be5 Hub \u548c\u79df\u6237\u5df2\u9009\u62e9\u3002',
  notifFormRemoveTenant: '\u79fb\u9664',
  notifFormImmediate: '\u7acb\u5373',
  notifFormScheduled: '\u5b9a\u65f6',
  notifFormPublishScheduled: '\u5b9a\u65f6\u53d1\u5e03',
  notifRevoke: '\u64a4\u56de',
  notifRevokeConfirm: '\u786e\u8ba4\u64a4\u56de\u6b64\u901a\u77e5\uff1f\u5b83\u4f1a\u4ece\u6240\u6709\u5ba2\u6237\u7aef\u79fb\u9664\u3002',
  notifRevoked: '\u901a\u77e5\u5df2\u64a4\u56de\u3002',
  notifRevokeFailed: '\u64a4\u56de\u5931\u8d25\uff1a{error}',
  notifDelete: '\u5220\u9664',
  notifDeleteConfirm: '\u786e\u8ba4\u6c38\u4e45\u5220\u9664\u6b64\u901a\u77e5\uff1f\u5df2\u53d1\u5e03\u7684\u901a\u77e5\u987b\u5148\u64a4\u56de\u3002',
  notifDeleted: '\u901a\u77e5\u5df2\u5220\u9664\u3002',
  notifDeleteFailed: '\u5220\u9664\u5931\u8d25\uff1a{error}',
  notifCreated: '\u901a\u77e5\u5df2\u521b\u5efa\u3002',
  notifScheduledSaved: '\u5df2\u4fdd\u5b58\uff0c\u5230\u53d1\u5e03\u65f6\u95f4\u540e\u53d1\u51fa\u3002',
  notifCreateFailed: '\u521b\u5efa\u901a\u77e5\u5931\u8d25\uff1a{error}',
  notifCascadeTitle: '\u7ea7\u8054\u63a8\u9001',
  notifCascadeHub: 'Hub',
  notifCascadeTime: '\u63a8\u9001\u65f6\u95f4',
  notifCascadeStatus: '\u72b6\u6001',
  notifCascadeSuccess: '\u6210\u529f',
  notifCascadeFailed: '\u5931\u8d25',
  notifCascadePending: '\u5f85\u63a8\u9001',
  notifCascadeTimeout: '\u8d85\u65f6',
  notifCascadeAuthFailed: '\u8ba4\u8bc1\u5931\u8d25',
  notifDetailBack: '\u8fd4\u56de\u5217\u8868',
  notifDetailStats: '\u9001\u8fbe\u7edf\u8ba1',
  notifDetailTotal: '\u603b\u63a8\u9001\u6570',
  notifDetailRead: '\u5df2\u8bfb',
  notifDetailReadRate: '\u5df2\u8bfb\u7387',
  notifValidationTitle: '\u6807\u9898\u4e0d\u80fd\u4e3a\u7a7a\uff08\u6700\u591a 100 \u5b57\u7b26\uff09',
  notifValidationContent: '\u5185\u5bb9\u4e0d\u80fd\u4e3a\u7a7a\uff08\u6700\u591a 2000 \u5b57\u7b26\uff09',
  notifValidationAudience: '\u8bf7\u9009\u62e9\u53d7\u4f17',
  notifValidationLink: '\u94fe\u63a5\u987b\u4ee5 http:// \u6216 https:// \u5f00\u5934',
  notifValidationPublishAt: '\u8bf7\u9009\u62e9\u53d1\u5e03\u65f6\u95f4',
  notifValidationPublishFuture: '\u53d1\u5e03\u65f6\u95f4\u987b\u665a\u4e8e\u73b0\u5728',
  notifValidationExpireAt: '\u8fc7\u671f\u65f6\u95f4\u4e0d\u662f\u6709\u6548\u65e5\u671f',
  notifValidationExpireOrder: '\u8fc7\u671f\u65f6\u95f4\u987b\u665a\u4e8e\u53d1\u5e03\u65f6\u95f4',
  notifHubCount: '{count} \u4e2a Hub',
  notifHubCountOne: '1 \u4e2a Hub'
});

Object.assign(I18N_MN, {
  notifFormPinned: '\u0411\u044d\u0445\u044d\u043b\u0441\u044d\u043d \u043c\u044d\u0434\u044d\u0433\u0434\u044d\u043b (\u0445\u04e9\u0432\u04e9\u0433\u0447 \u0442\u0443\u0443\u0437)',
  notifFormPinnedForever: '\u0411\u0430\u0439\u043d\u0433\u0430 \u0431\u044d\u0445\u043b\u044d\u0445 (\u04af\u0440\u0433\u044d\u043b\u0436 \u0437\u04af\u04af\u043d \u0442\u0430\u043b\u0434)',
  notifFormLink: '\u0425\u043e\u043b\u0431\u043e\u043e\u0441 (\u0437\u0430\u0430\u0432\u0430\u043b \u0431\u0438\u0448)',
  notifFormLinkPlaceholder: 'https://... \u0442\u0443\u0443\u0437\u043d\u044b \u0442\u043e\u0432\u0447\u043e\u043e\u0440 \u043d\u044d\u044d\u0433\u0434\u044d\u043d\u044d',
  notifFormButtonText: '\u0422\u043e\u0432\u0447\u043d\u044b \u043d\u044d\u0440',
  notifFormButtonTextPlaceholder: '\u0414\u044d\u043b\u0433\u044d\u0440\u044d\u043d\u0433\u04af\u0439 \u0445\u0430\u0440\u0430\u0445',
  notifFormSourceLang: '\u042d\u0445 \u0445\u044d\u043b',
  notifFormTranslateTargets: '\u041c\u04e9\u043d \u0445\u0430\u0434\u0433\u0430\u043b\u0430\u0445',
  notifFormTranslateHint: '\u0421\u043e\u043d\u0433\u043e\u0441\u043e\u043d \u0445\u044d\u043b\u0438\u0439\u0433 \u044d\u0445\u044d\u044d\u0441 \u043e\u0440\u0447\u0443\u0443\u043b\u043d\u0430. \u0425\u043e\u043b\u0431\u043e\u043e\u0441 \u043e\u0440\u0447\u0443\u0443\u043b\u0430\u0433\u0434\u0430\u0445\u0433\u04af\u0439.',
  notifLangZhHans: '\u0425\u044f\u043b\u0431\u0430\u0440\u0448\u0443\u0443\u043b\u0441\u0430\u043d \u0445\u044f\u0442\u0430\u0434',
  notifLangZhHant: '\u0423\u043b\u0430\u043c\u0436\u043b\u0430\u043b\u0442 \u0445\u044f\u0442\u0430\u0434',
  notifLangEn: '\u0410\u043d\u0433\u043b\u0438',
  notifLangMn: '\u041a\u0438\u0440\u0438\u043b\u043b \u043c\u043e\u043d\u0433\u043e\u043b',
  notifBadgePinned: '\u0411\u044d\u0445\u044d\u043b\u0441\u044d\u043d',
  notifHubCount: '{count} Hub',
  notifHubCountOne: '1 Hub',
  notifValidationLink: '\u0425\u043e\u043b\u0431\u043e\u043e\u0441 http:// \u044d\u0441\u0432\u044d\u043b https://-\u044d\u044d\u0440 \u044d\u0445\u043b\u044d\u0445 \u0451\u0441\u0442\u043e\u0439',
  notifValidationPublishAt: '\u041d\u0438\u0439\u0442\u043b\u044d\u0445 \u0446\u0430\u0433\u0430\u0430 \u0441\u043e\u043d\u0433\u043e\u043d\u043e \u0443\u0443',
  notifValidationPublishFuture: '\u041d\u0438\u0439\u0442\u043b\u044d\u0445 \u0446\u0430\u0433 \u043e\u0434\u043e\u043e\u0433\u043e\u043e\u0441 \u0445\u043e\u0439\u0448 \u0431\u0430\u0439\u0445 \u0451\u0441\u0442\u043e\u0439',
  notifValidationExpireAt: '\u0414\u0443\u0443\u0441\u0430\u0445 \u0446\u0430\u0433 \u0431\u0443\u0440\u0443\u0443 \u0431\u0430\u0439\u043d\u0430',
  notifValidationExpireOrder: '\u0414\u0443\u0443\u0441\u0430\u0445 \u0446\u0430\u0433 \u043d\u0438\u0439\u0442\u043b\u044d\u0445 \u0446\u0430\u0433\u0430\u0430\u0441 \u0445\u043e\u0439\u0448 \u0431\u0430\u0439\u0445 \u0451\u0441\u0442\u043e\u0439',
  notifScheduledSaved: '\u0425\u0430\u0434\u0433\u0430\u043b\u0441\u0430\u043d. \u0422\u043e\u0432\u043b\u043e\u0441\u043e\u043d \u0446\u0430\u0433\u0442 \u0438\u043b\u0433\u044d\u044d\u043d\u044d.',
  notifFormPublishScheduled: '\u0422\u043e\u0432\u043b\u043e\u0436 \u043d\u0438\u0439\u0442\u043b\u044d\u0445',
  notifCascadeTimeout: '\u0425\u0443\u0433\u0430\u0446\u0430\u0430 \u0445\u044d\u0442\u044d\u0440\u0441\u044d\u043d',
  notifCascadeAuthFailed: '\u041d\u044d\u0432\u0442\u0440\u044d\u043b\u0442 \u0430\u043c\u0436\u0438\u043b\u0442\u0433\u04af\u0439',
  notifDetailRead: '\u0423\u043d\u0448\u0441\u0430\u043d',
  notifDetailReadRate: '\u0423\u043d\u0448\u0441\u0430\u043d \u0445\u0443\u0432\u044c'
});

// --- Tab metadata ---
tabMeta.notifications = ['notifTitle', 'notifDesc'];
TAB_ICONS.notifications = '<svg viewBox="0 0 24 24"><path d="M12 2a7 7 0 0 0-7 7c0 3.53-.8 5.74-1.64 7.13A1 1 0 0 0 4.2 18h15.6a1 1 0 0 0 .84-1.87C19.8 14.74 19 12.53 19 9a7 7 0 0 0-7-7Z"></path><path d="M9.1 19a3 3 0 0 0 5.8 0"></path></svg>';

// --- State ---
var notifPage = 1;
var notifPageSize = 20;
var notifPaintLang = '';
var notifListEpoch = 0;
var notifDetailEpoch = 0;
var notifCreateEpoch = 0;
var notifLastList = null;
var notifLastDetail = null;
var notifDeferListReload = false;
var notifStatusFilter = '';
var notifViewMode = 'list'; // 'list' | 'create' | 'detail'
var notifSubmitting = false;
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
var notifHubListInflight = null;
function notifLoadHubList() {
  if (notifHubListCache) return Promise.resolve(notifHubListCache);
  if (notifHubListInflight) return notifHubListInflight;
  notifHubListInflight = notifApiGet('/api/admin/hubs').then(function(data) {
    notifHubListCache = Array.isArray(data) ? data : (data.hubs || []);
    return notifHubListCache;
  }, function() {
    return [];
  }).then(function(list) {
    notifHubListInflight = null;
    return list;
  });
  return notifHubListInflight;
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
  notifDeferListReload = true;
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
  var group = (newsBtn && newsBtn.closest('.nav-group')) || nav.querySelector('.nav-group[data-nav-group="community"]') || nav;
  if (newsBtn && newsBtn.parentNode === group) {
    group.insertBefore(btn, newsBtn.nextSibling);
  } else {
    var before = nav.querySelector('[data-tab="failurelogs"],[data-tab="system"]');
    group.insertBefore(btn, before || null);
  }
  if (typeof syncNavGroups === 'function') syncNavGroups();
}

// --- List view ---
function renderNotifList(opts) {
  notifPaintLang = window.currentLang || '';
  notifViewMode = 'list';
  var root = document.getElementById('notifContent');
  if (!root) return;
  var relabel = !!(opts && opts.relabel && notifLastList
    && notifLastList.filter === notifStatusFilter
    && notifLastList.page === notifPage);
  var filterHtml = '<div class="notif-filters">'
    + '<button type="button" class="btn-ghost' + (notifStatusFilter === '' ? ' active' : '') + '" onclick="notifSetFilter(\'\')">' + tr('notifFilterAll') + '</button>'
    + '<button type="button" class="btn-ghost' + (notifStatusFilter === 'published' ? ' active' : '') + '" onclick="notifSetFilter(\'published\')">' + tr('notifFilterPublished') + '</button>'
    + '<button type="button" class="btn-ghost' + (notifStatusFilter === 'expired' ? ' active' : '') + '" onclick="notifSetFilter(\'expired\')">' + tr('notifFilterExpired') + '</button>'
    + '<button type="button" class="btn-ghost' + (notifStatusFilter === 'revoked' ? ' active' : '') + '" onclick="notifSetFilter(\'revoked\')">' + tr('notifFilterRevoked') + '</button>'
    + '<button type="button" class="btn-ghost' + (notifStatusFilter === 'draft' ? ' active' : '') + '" onclick="notifSetFilter(\'draft\')">' + tr('notifFilterDraft') + '</button>'
    + '</div>';
  root.innerHTML = '<div class="head"><div><h3>' + tr('notifTitle') + '</h3><div class="desc">' + tr('notifDesc') + '</div></div>'
    + '<div class="actions"><button type="button" class="btn-primary" onclick="renderNotifCreate()">' + tr('notifCreate') + '</button>'
    + '<button type="button" class="btn-ghost" onclick="loadNotifList()">' + tr('notifRefresh') + '</button></div></div>'
    + filterHtml
    + '<div id="notifList" class="list" role="status" aria-live="polite">' + (relabel ? '' : '<div class="hint">' + tr('notifLoading') + '</div>') + '</div>';
  if (relabel) notifPaintListBody(notifLastList.items, notifLastList.total);
  else loadNotifList();
}

function notifSetFilter(s) { notifStatusFilter = s; notifPage = 1; renderNotifList(); }

function notifPaintListBody(items, total, listEl) {
  if (!listEl) listEl = document.getElementById('notifList');
  if (!listEl) return;
  var limit = notifPageSize;
  if (!items || !items.length) {
    listEl.innerHTML = '<div class="hint">' + tr('notifEmpty') + '</div>';
    notifRenderPager(0, 0, limit);
    return;
  }
  listEl.innerHTML = items.map(notifCardHtml).join('');
  notifRenderPager(total, items.length, limit);
}

function loadNotifList() {
  var epoch = ++notifListEpoch;
  var listEl = document.getElementById('notifList');
  if (!listEl) return;
  listEl.innerHTML = '<div class="hint">' + tr('notifLoading') + '</div>';
  var limit = notifPageSize;
  var page = notifPage;
  var filter = notifStatusFilter;
  var offset = Math.max(0, (page - 1) * limit);
  var url = '/api/v1/admin/notifications?limit=' + limit + '&offset=' + offset;
  if (filter) url += '&status=' + encodeURIComponent(filter);
  notifApiGet(url).then(function(data) {
    if (epoch !== notifListEpoch) return;
    var live = document.getElementById('notifList');
    if (!live || notifViewMode !== 'list') return;
    var items = notifUnwrapList(data);
    var total = Number(data && data.total || items.length || 0);
    if (items.length === 0 && total > 0 && page > 1 && notifPage === page) {
      notifPage -= 1;
      loadNotifList();
      return;
    }
    notifLastList = { items: items, total: total, filter: filter, page: page };
    notifPaintListBody(items, total, live);
  }).catch(function(err) {
    if (epoch !== notifListEpoch) return;
    var live = document.getElementById('notifList');
    if (!live || notifViewMode !== 'list') return;
    live.innerHTML = '<div class="hint danger">' + tr('notifLoadFailed', { error: escapeHtml(err.message) }) + '</div>';
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

function notifFormatTime(value) {
  if (!value) return '';
  var parsed = new Date(value);
  if (isNaN(parsed.getTime())) return String(value);
  return parsed.toLocaleString();
}

function notifAudienceText(n) {
  var type = n && n.audience_type || '';
  var key = type === 'hub' ? 'notifAudienceHubs' : type === 'hub_tenant' ? 'notifAudienceTenant' : (type === 'all' ? 'notifAudienceAllHubs' : '');
  if (!key) return '';
  var label = tr(key);
  var ids = ((n && n.audience_ids) || []).filter(Boolean);
  if (type !== 'all' && ids.length) label += ': ' + ids.join(', ');
  return label;
}

function notifScheduleMeta(n) {
  var parts = [];
  if (n && n.publish_at) parts.push(tr('notifFormPublishAt') + ': ' + notifFormatTime(n.publish_at));
  if (n && n.expire_at) parts.push(tr('notifFormExpireAt') + ': ' + notifFormatTime(n.expire_at));
  var audience = notifAudienceText(n);
  if (audience) parts.push(audience);
  return parts.join(' · ');
}

function notifCardHtml(n) {
  var statusBadge = notifStatusBadge(n.status);
  var catLabel = notifCategoryLabel(n.category);
  var prioLabel = n.priority === 'urgent' ? '<span class="badge danger">' + tr('notifPriorityUrgent') + '</span>' :
    n.priority === 'important' ? '<span class="badge warn">' + tr('notifPriorityImportant') + '</span>' : '';
  var pinnedLabel = n.pinned ? '<span class="badge warn">' + tr('notifBadgePinned') + '</span>' : '';
  var time = n.created_at ? notifFormatTime(n.created_at) : '';
  if (n.publish_at) time += (time ? ' · ' : '') + tr('notifFormPublishAt') + ' ' + notifFormatTime(n.publish_at);
  var cascadeCount = (n.cascade_results || []).length;
  var cascadeHint = cascadeCount > 0 ? ' <span class="item-meta">(' + notifHubCountText(cascadeCount) + ')</span>' : '';
  var deleteBtn = notifCanDelete(n)
    ? '<button class="btn-danger" type="button" onclick="event.stopPropagation();notifDelete(' + notifJsArg(n.id) + ')">' + escapeHtml(tr('notifDelete')) + '</button>'
    : '';
  return '<div class="item news-card notif-card" onclick="renderNotifDetail(' + notifJsArg(n.id) + ')">'
    + '<div class="notif-card-row">'
    + '<div class="notif-card-main"><div class="news-title">' + escapeHtml(n.title || '') + '</div>'
    + '<div class="news-meta">' + catLabel + ' ' + statusBadge + ' ' + pinnedLabel + ' ' + prioLabel + cascadeHint + '</div>'
    + '<div class="item-meta">' + escapeHtml(time) + '</div></div>'
    + (deleteBtn ? '<div class="actions notif-card-actions">' + deleteBtn + '</div>' : '')
    + '</div>'
    + '</div>';
}

function notifHubCountText(count) {
  var n = Number(count) || 0;
  if (n === 1) return tr('notifHubCountOne');
  return tr('notifHubCount', { count: String(n) });
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
function notifCascadeCounts(results) {
  var total = 0;
  var success = 0;
  var failed = 0;
  var pending = 0;
  (results || []).forEach(function(r) {
    if (!r) return;
    total += 1;
    var status = r.status || '';
    if (status === 'success') success += 1;
    else if (status === 'failed' || status === 'timeout' || status === 'auth_failed') failed += 1;
    else if (status === 'pending' || status === '') pending += 1;
  });
  return { total: total, success: success, failed: failed, pending: pending };
}

function notifKnownHubName(id) {
  var wanted = String(id || '');
  if (!wanted || !notifHubListCache) return '';
  var hub = notifHubListCache.find(function(item) { return notifHubID(item) === wanted; });
  return hub ? notifHubName(hub) : '';
}

function notifCascadeNeedsHubNames(results) {
  return (results || []).some(function(r) {
    return !!(r && r.hub_id && !r.hub_name && !notifKnownHubName(r.hub_id));
  });
}

// Names arrive with the hub list, which the detail request does not include.
// One shared load fills them in; a failed load leaves the hub id on screen.
function notifResolveCascadeHubNames() {
  if (notifHubListCache) return;
  var epoch = notifDetailEpoch;
  notifLoadHubList().then(function(list) {
    if (!list || !list.length) return;
    if (epoch !== notifDetailEpoch || notifViewMode !== 'detail') return;
    var live = document.getElementById('notifContent');
    if (!live || !notifLastDetail) return;
    notifPaintDetail(live, notifLastDetail.notification);
  });
}

function notifPaintDetail(root, n) {
  var statusBadge = notifStatusBadge(n.status);
  var catLabel = notifCategoryLabel(n.category);
  var canRevoke = n.status === 'published';
  var revokeBtn = canRevoke ? '<button type="button" class="btn-danger" onclick="notifRevoke(' + notifJsArg(n.id) + ')">' + tr('notifRevoke') + '</button>' : '';
  var deleteBtn = notifCanDelete(n) ? '<button type="button" class="btn-danger" onclick="notifDelete(' + notifJsArg(n.id) + ')">' + tr('notifDelete') + '</button>' : '';
  var counts = notifCascadeCounts(n.cascade_results);
  var pendingNote = counts.pending > 0
    ? '<div class="item-meta">' + escapeHtml(tr('notifCascadePending')) + ' ' + counts.pending + '</div>'
    : '';
  var statsHtml = '<div class="grid3 section-gap"><div class="item"><div class="item-title">' + tr('notifDetailTotal') + '</div><strong>' + counts.total + '</strong></div>'
    + '<div class="item"><div class="item-title">' + tr('notifCascadeSuccess') + '</div><strong>' + counts.success + '</strong></div>'
    + '<div class="item"><div class="item-title">' + tr('notifCascadeFailed') + '</div><strong>' + counts.failed + '</strong></div></div>'
    + pendingNote;
  var cascadeHtml = notifCascadeTableHtml(n.cascade_results || []);
  var scheduleMeta = notifScheduleMeta(n);
  var contentHtml = '<div class="item section-gap"><div class="item-title" data-icon="file">' + tr('notifFormContent') + '</div><div class="notif-content-preview">' + escapeHtml(n.content || '') + '</div>'
    + (n.link_url ? '<div class="item-meta section-gap-sm">' + escapeHtml(tr('notifFormLink')) + ': <a href="' + escapeHtml(n.link_url) + '" target="_blank" rel="noopener noreferrer">' + escapeHtml(n.link_url) + '</a>'
      + (n.button_text ? ' &nbsp;|&nbsp; ' + escapeHtml(tr('notifFormButtonText')) + ': ' + escapeHtml(n.button_text) : '') + '</div>' : '')
    + '</div>';
  root.innerHTML = '<div class="head"><div><h3>' + escapeHtml(n.title || '') + '</h3><div class="desc">' + catLabel + ' ' + statusBadge + (n.pinned ? ' <span class="badge warn">' + tr('notifBadgePinned') + '</span>' : '') + '</div></div>'
    + '<div class="actions"><button type="button" class="btn-ghost" onclick="renderNotifList()">' + tr('notifDetailBack') + '</button>' + revokeBtn + deleteBtn + '</div></div>'
    + (scheduleMeta ? '<div class="item-meta section-gap-sm">' + escapeHtml(scheduleMeta) + '</div>' : '')
    + contentHtml
    + '<div class="item section-gap"><div class="item-title" data-icon="chart">' + tr('notifDetailStats') + '</div>' + statsHtml + '</div>'
    + cascadeHtml;
  if (notifCascadeNeedsHubNames(n.cascade_results)) notifResolveCascadeHubNames();
}

function renderNotifDetail(id, opts) {
  var epoch = ++notifDetailEpoch;
  notifViewMode = 'detail';
  notifCurrentId = id;
  notifPaintLang = window.currentLang || '';
  var root = document.getElementById('notifContent');
  if (!root) return;
  if (opts && opts.relabel && notifLastDetail && String(notifLastDetail.id) === String(id)) {
    notifPaintDetail(root, notifLastDetail.notification);
    return;
  }
  root.innerHTML = '<div class="hint">' + tr('notifLoading') + '</div>';
  notifApiGet('/api/v1/admin/notifications/' + encodeURIComponent(id)).then(function(data) {
    if (epoch !== notifDetailEpoch) return;
    var live = document.getElementById('notifContent');
    if (!live || notifViewMode !== 'detail') return;
    var n = notifUnwrapDetail(data);
    notifLastDetail = { id: id, notification: n };
    notifPaintDetail(live, n);
  }).catch(function(err) {
    if (epoch !== notifDetailEpoch) return;
    var live = document.getElementById('notifContent');
    if (!live || notifViewMode !== 'detail') return;
    live.innerHTML = '<div class="hint danger">' + tr('notifLoadFailed', { error: escapeHtml(err.message) }) + '</div>'
      + '<button type="button" class="btn-ghost" onclick="renderNotifList()">' + tr('notifDetailBack') + '</button>';
  });
}

function notifCascadeStatusLabel(status) {
  var map = {
    success: 'notifCascadeSuccess',
    failed: 'notifCascadeFailed',
    pending: 'notifCascadePending',
    timeout: 'notifCascadeTimeout',
    auth_failed: 'notifCascadeAuthFailed'
  };
  if (map[status]) return tr(map[status]);
  return status ? String(status) : tr('notifCascadePending');
}

function notifCascadeTableHtml(results) {
  if (!results || results.length === 0) return '';
  var rows = results.map(function(r) {
    if (!r) return '';
    var status = r.status || '';
    var statusCls = status === 'success' ? 'ok' : (status === 'failed' || status === 'timeout' || status === 'auth_failed') ? 'danger' : 'warn';
    var time = r.pushed_at ? new Date(r.pushed_at).toLocaleString() : '-';
    var hubLabel = r.hub_name || notifKnownHubName(r.hub_id) || r.hub_id || '-';
    var errHtml = r.error_message ? '<div class="item-meta">' + escapeHtml(r.error_message) + '</div>' : '';
    return '<tr><td>' + escapeHtml(hubLabel) + '</td><td>' + escapeHtml(time) + '</td><td><span class="badge ' + statusCls + '">' + escapeHtml(notifCascadeStatusLabel(status)) + '</span>' + errHtml + '</td></tr>';
  }).join('');
  return '<div class="item section-gap"><div class="item-title" data-icon="tree">' + tr('notifCascadeTitle') + '</div>'
    + '<table class="notif-cascade-table"><thead><tr><th>' + tr('notifCascadeHub') + '</th><th>' + tr('notifCascadeTime') + '</th><th>' + tr('notifCascadeStatus') + '</th></tr></thead>'
    + '<tbody>' + rows + '</tbody></table></div>';
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
    case 'zh-Hant': return '查看詳情';
    case 'en': return 'View details';
    case 'mn': return 'Дэлгэрэнгүй харах';
    default: return '查看详情';
  }
}

function notifSyncButtonPlaceholder() {
  var input = document.getElementById('notifButtonTextInput');
  if (!input) return;
  var sel = document.getElementById('notifSourceLang');
  var lang = (sel && sel.value) || notifDefaultSourceLang();
  input.placeholder = notifDefaultButtonText(lang);
}

function notifSetSubmitLocked(locked) {
  ['notifSubmitDraft', 'notifSubmitPublish'].forEach(function(id) {
    var el = document.getElementById(id);
    if (el) el.disabled = locked;
  });
}

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
    return '<label class="inline-label"><input type="checkbox" class="notif-target-lang auto-check" value="' + opt[0] + '" checked> ' + escapeHtml(tr(opt[1])) + '</label>';
  }).join('');
}

function notifSelectedTargetLangs() {
  var langs = [];
  document.querySelectorAll('.notif-target-lang:checked').forEach(function(el) { langs.push(el.value); });
  return langs;
}

function notifReadCreateDraft() {
  if (!document.getElementById('notifTitleInput')) return null;
  function val(id) {
    var el = document.getElementById(id);
    return el ? el.value : '';
  }
  function on(id) {
    var el = document.getElementById(id);
    return !!(el && el.checked);
  }
  var hubs = [];
  document.querySelectorAll('#notifHubCheckboxes input[type="checkbox"]:checked').forEach(function(box) {
    hubs.push(box.value);
  });
  var langs = [];
  document.querySelectorAll('.notif-target-lang').forEach(function(el) {
    langs.push({ value: el.value, checked: !!el.checked });
  });
  return {
    title: val('notifTitleInput'),
    content: val('notifContentInput'),
    sourceLang: val('notifSourceLang'),
    category: val('notifCategorySelect'),
    priority: val('notifPrioritySelect'),
    publish: val('notifPublishSelect'),
    publishAt: val('notifPublishAtInput'),
    expireAt: val('notifExpireAtInput'),
    audience: val('notifAudienceSelect'),
    tenantHub: val('notifTenantHubSelect'),
    tenant: val('notifTenantSelect'),
    imPush: on('notifImPush'),
    pinned: on('notifPinned'),
    pinnedForever: on('notifPinnedForever'),
    link: val('notifLinkUrlInput'),
    button: val('notifButtonTextInput'),
    hubs: hubs,
    langs: langs,
    targets: notifTenantTargets.slice()
  };
}

function notifSetField(id, value) {
  var el = document.getElementById(id);
  if (el && value != null) el.value = value;
}

function notifApplyCreateDraft(draft) {
  if (!draft) return;
  notifSetField('notifTitleInput', draft.title);
  notifSetField('notifContentInput', draft.content);
  notifSetField('notifSourceLang', draft.sourceLang);
  notifSetField('notifCategorySelect', draft.category);
  notifSetField('notifPrioritySelect', draft.priority);
  notifSetField('notifPublishSelect', draft.publish);
  notifSetField('notifPublishAtInput', draft.publishAt);
  notifSetField('notifExpireAtInput', draft.expireAt);
  notifSetField('notifAudienceSelect', draft.audience);
  notifSetField('notifLinkUrlInput', draft.link);
  notifSetField('notifButtonTextInput', draft.button);
  var im = document.getElementById('notifImPush');
  if (im) im.checked = !!draft.imPush;
  var pinned = document.getElementById('notifPinned');
  if (pinned) pinned.checked = !!draft.pinned;
  var forever = document.getElementById('notifPinnedForever');
  if (forever) forever.checked = !!draft.pinnedForever;
  if (draft.langs) {
    var wanted = {};
    draft.langs.forEach(function(item) { wanted[item.value] = !!item.checked; });
    document.querySelectorAll('.notif-target-lang').forEach(function(el) {
      if (Object.prototype.hasOwnProperty.call(wanted, el.value)) el.checked = wanted[el.value];
    });
  }
  notifToggleSchedule();
  notifToggleAudience();
  notifTogglePinned();
  notifTogglePriorityIM();
  notifSyncButtonPlaceholder();
}

// --- Create view ---
function renderNotifCreate(draft) {
  var epoch = ++notifCreateEpoch;
  notifViewMode = 'create';
  notifPaintLang = window.currentLang || '';
  var root = document.getElementById('notifContent');
  if (!root) return;
  root.innerHTML = '<div class="head"><div><h3>' + tr('notifCreate') + '</h3></div>'
    + '<div class="actions"><button type="button" class="btn-ghost" onclick="renderNotifList()">' + tr('notifFormCancel') + '</button></div></div>'
    + '<div class="notif-form">'
    + '<div><label for="notifTitleInput">' + tr('notifFormTitle') + '</label>'
    + '<input id="notifTitleInput" maxlength="100" placeholder="' + escapeHtml(tr('notifFormTitlePlaceholder')) + '"></div>'
    + '<div><label for="notifContentInput">' + tr('notifFormContent') + '</label>'
    + '<textarea id="notifContentInput" maxlength="2000" rows="6" placeholder="' + escapeHtml(tr('notifFormContentPlaceholder')) + '"></textarea></div>'
    + '<div><label for="notifSourceLang">' + tr('notifFormSourceLang') + '</label>'
    + '<select id="notifSourceLang" onchange="notifSyncButtonPlaceholder()">' + notifSourceLangOptions() + '</select>'
    + '<div class="item-meta section-gap-sm">' + tr('notifFormTranslateTargets') + '</div>'
    + '<div class="notif-lang-checks">' + notifTargetLangChecks() + '</div>'
    + '<div class="hint">' + tr('notifFormTranslateHint') + '</div></div>'
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
    + '<div><label class="inline-label"><input type="checkbox" id="notifPinned" class="auto-check" onchange="notifTogglePinned()"> ' + tr('notifFormPinned') + '</label></div>'
    + '<div id="notifPinnedExtras" class="hidden notif-form">'
    + '<div><label class="inline-label"><input type="checkbox" id="notifPinnedForever" class="auto-check"> ' + tr('notifFormPinnedForever') + '</label></div>'
    + '<div><label for="notifLinkUrlInput">' + tr('notifFormLink') + '</label>'
    + '<input id="notifLinkUrlInput" maxlength="500" placeholder="' + escapeHtml(tr('notifFormLinkPlaceholder')) + '"></div>'
    + '<div><label for="notifButtonTextInput">' + tr('notifFormButtonText') + '</label>'
    + '<input id="notifButtonTextInput" maxlength="20" placeholder="' + escapeHtml(notifDefaultButtonText(notifDefaultSourceLang())) + '"></div>'
    + '</div>'
    + '<div class="actions section-gap">'
    + '<button id="notifSubmitPublish" class="btn-primary" type="button" onclick="notifSubmit(\'published\')">' + tr('notifFormPublishNow') + '</button>'
    + '<button id="notifSubmitDraft" class="btn-secondary" type="button" onclick="notifSubmit(\'draft\')">' + tr('notifFormSaveDraft') + '</button>'
    + '</div></div>';
  notifTenantTargets = draft && draft.targets ? draft.targets.slice() : [];
  notifApplyCreateDraft(draft);
  notifLoadHubCheckboxes(draft && draft.hubs, epoch);
  notifLoadTenantSelector(draft, epoch);
  var prioritySelect = document.getElementById('notifPrioritySelect');
  if (prioritySelect) prioritySelect.addEventListener('change', notifTogglePriorityIM);
}

function notifToggleSchedule() {
  var sel = document.getElementById('notifPublishSelect');
  var row = document.getElementById('notifScheduleRow');
  var scheduled = !!(sel && sel.value === 'scheduled');
  if (row) row.classList.toggle('hidden', !scheduled);
  var btn = document.getElementById('notifSubmitPublish');
  if (btn) btn.textContent = tr(scheduled ? 'notifFormPublishScheduled' : 'notifFormPublishNow');
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

function notifTogglePinned() {
  var pinned = document.getElementById('notifPinned');
  var extras = document.getElementById('notifPinnedExtras');
  if (pinned && extras) extras.classList.toggle('hidden', !pinned.checked);
}

function notifLoadHubCheckboxes(selected, epoch) {
  notifLoadHubList().then(function(hubs) {
    if (epoch != null && epoch !== notifCreateEpoch) return;
    var container = document.getElementById('notifHubCheckboxes');
    if (!container) return;
    if (hubs.length === 0) { container.innerHTML = '<div class="hint">' + tr('notifFormNoHubs') + '</div>'; return; }
    container.innerHTML = hubs.map(function(h) {
      var name = h.name || h.hub_name || h.id || h.hub_id || 'Hub';
      var id = h.id || h.hub_id || '';
      return '<label class="notif-hub-check"><input type="checkbox" value="' + escapeHtml(id) + '"> ' + escapeHtml(name) + '</label>';
    }).join('');
    if (selected && selected.length) {
      container.querySelectorAll('input[type="checkbox"]').forEach(function(box) {
        box.checked = selected.indexOf(box.value) >= 0;
      });
    }
  });
}

function notifLoadTenantSelector(draft, epoch) {
  notifLoadHubList().then(function(hubs) {
    if (epoch != null && epoch !== notifCreateEpoch) return;
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
    if (draft && draft.tenantHub) hubSelect.value = draft.tenantHub;
    notifSyncTenantSelect();
    if (draft && draft.tenant) notifSetField('notifTenantSelect', draft.tenant);
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

function notifParseLocalDate(value) {
  var raw = String(value || '').trim();
  if (!raw) return null;
  var parsed = new Date(raw);
  if (isNaN(parsed.getTime())) return null;
  return parsed;
}

// --- Submit notification ---
function notifSubmit(status) {
  var title = ((document.getElementById('notifTitleInput') || {}).value || '').trim();
  var content = ((document.getElementById('notifContentInput') || {}).value || '').trim();
  var category = (document.getElementById('notifCategorySelect') || {}).value || 'system_announcement';
  var priority = (document.getElementById('notifPrioritySelect') || {}).value || 'normal';
  var audienceType = (document.getElementById('notifAudienceSelect') || {}).value || 'all';
  var publishSelect = (document.getElementById('notifPublishSelect') || {}).value || 'now';
  var publishAt = publishSelect === 'scheduled' ? (document.getElementById('notifPublishAtInput') || {}).value || '' : '';
  var expireAt = (document.getElementById('notifExpireAtInput') || {}).value || '';
  var imPush = priority === 'urgent' && (document.getElementById('notifImPush') || {}).checked;
  var pinned = !!(document.getElementById('notifPinned') || {}).checked;
  var pinnedForever = pinned && !!(document.getElementById('notifPinnedForever') || {}).checked;
  var linkUrl = ((document.getElementById('notifLinkUrlInput') || {}).value || '').trim();
  var buttonText = ((document.getElementById('notifButtonTextInput') || {}).value || '').trim();

  // Validation runs before the submit lock. A bad date used to throw inside
  // toISOString after the lock was taken, and the form stayed disabled.
  if (!title || title.length > 100) { notifToast(tr('notifValidationTitle'), 'warn'); return; }
  if (!content || content.length > 2000) { notifToast(tr('notifValidationContent'), 'warn'); return; }
  if (linkUrl && !/^https?:\/\//i.test(linkUrl)) {
    notifToast(tr('notifValidationLink'), 'warn');
    return;
  }
  var publishDate = null;
  if (publishSelect === 'scheduled' && publishAt) {
    publishDate = notifParseLocalDate(publishAt);
    if (!publishDate) { notifToast(tr('notifValidationPublishAt'), 'warn'); return; }
  }
  if (status === 'published' && publishSelect === 'scheduled' && !publishDate) {
    notifToast(tr('notifValidationPublishAt'), 'warn');
    return;
  }
  if (status === 'published' && publishDate && publishDate.getTime() <= Date.now()) {
    notifToast(tr('notifValidationPublishFuture'), 'warn');
    return;
  }
  var expireDate = null;
  if (String(expireAt || '').trim()) {
    expireDate = notifParseLocalDate(expireAt);
    if (!expireDate) { notifToast(tr('notifValidationExpireAt'), 'warn'); return; }
    var earliest = publishDate ? publishDate.getTime() : Date.now();
    if (expireDate.getTime() <= earliest) { notifToast(tr('notifValidationExpireOrder'), 'warn'); return; }
  }

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
  if (notifSubmitting) return;
  notifSubmitting = true;
  notifSetSubmitLocked(true);

  var body = {
    title: title,
    content: content,
    category: category,
    priority: priority,
    audience_type: audienceType,
    audience_ids: audienceIds,
    status: status,
    im_push: imPush,
    pinned: pinned,
    pinned_forever: pinnedForever,
    link_url: linkUrl,
    button_text: buttonText,
    source_lang: (document.getElementById('notifSourceLang') || {}).value || notifDefaultSourceLang(),
    target_langs: notifSelectedTargetLangs()
  };
  if (status === 'published' && publishDate) body.publish_at = publishDate.toISOString();
  if (expireDate) body.expire_at = expireDate.toISOString();

  notifApiPost('/api/v1/admin/notifications', body).then(function(data) {
    var saved = data && data.notification;
    var scheduled = !!(saved && saved.status === 'draft' && saved.publish_at);
    notifToast(tr(scheduled ? 'notifScheduledSaved' : 'notifCreated'), 'ok');
    notifPage = 1;
    notifStatusFilter = '';
    renderNotifList();
  }).catch(function(err) {
    notifToast(tr('notifCreateFailed', { error: err.message }), 'danger');
  }).then(function() {
    notifSubmitting = false;
    notifSetSubmitLocked(false);
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
  notifTogglePinned: notifTogglePinned,
  notifSyncTenantSelect: notifSyncTenantSelect,
  notifAddTenantTarget: notifAddTenantTarget,
  notifRemoveTenantTarget: notifRemoveTenantTarget,
  notifChangePage: notifChangePage,
  notifSubmit: notifSubmit,
  notifPaintDetail: notifPaintDetail,
  notifCardHtml: notifCardHtml,
  notifSyncButtonPlaceholder: notifSyncButtonPlaceholder,
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

  function bootNotificationTab() {
    initNotificationNav();
    initNotificationTab();
    activateSavedNotificationTab();
    // The defer flag only skips the reload that boot itself just triggered.
    // A later visit to the tab still refreshes.
    notifDeferListReload = false;
  }

  // Patch openTab to lazy-init notification content
  var patchedOpenTab = function(tab) {
    if (typeof origOpenTab === 'function') origOpenTab(tab);
    if (tab === 'notifications') {
      if (!document.getElementById('tab-notifications')) initNotificationTab();
      else if (notifPaintLang !== (window.currentLang || '')) {
        var draft = notifViewMode === 'create' ? notifReadCreateDraft() : null;
        notifPaintLang = window.currentLang || '';
        if (notifViewMode === 'create') renderNotifCreate(draft);
        else if (notifViewMode === 'detail' && notifCurrentId) renderNotifDetail(notifCurrentId, { relabel: true });
        else renderNotifList({ relabel: true });
      } else if (notifViewMode === 'list') {
        if (notifDeferListReload) notifDeferListReload = false;
        else loadNotifList();
      }
    }
  };
  window.openTab = patchedOpenTab;

  // Wait for DOM and inject nav + tab section
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', bootNotificationTab);
  } else {
    setTimeout(bootNotificationTab, 0);
  }
})();
