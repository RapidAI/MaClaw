/*
 * Digital assets admin module (enterprise knowledge libraries).
 * ASCII only in source; CJK via i18n.
 */
(function(global) {
  var digitalAssetsMaxAclDepartments = 512;
  // Nav/header keys also live in admin.js so the sidebar localizes before this
  // lazy module loads. Keep both copies in sync.
  if (typeof I18N !== 'undefined') {
    I18N.en = Object.assign({}, I18N.en, {
      digitalAssetsTabTitle: 'Digital Assets',
      digitalAssetsTabSubtitle: 'Tenant enterprise knowledge libraries, ACL, import and client sync.',
      digitalAssetsNavDesc: 'Enterprise knowledge libraries',
      digitalAssetsReload: 'Reload',
      digitalAssetsCreate: 'New library',
      digitalAssetsName: 'Name',
      digitalAssetsSearch: 'Search library',
      digitalAssetsImportSection: 'Import content',
      digitalAssetsUpload: 'Upload files',
      digitalAssetsArchive: 'Upload zip',
      digitalAssetsBrowserDir: 'Browser folder',
      digitalAssetsServerDir: 'Server directory',
      digitalAssetsServerDirPrompt: 'Absolute path on the Hub server (must be under local_dir_allowlist)',
      digitalAssetsServerDirHint: 'Imports from a path on the Hub host. Path must be allowlisted in tenant digital_assets settings.',
      digitalAssetsShareImport: 'Import share link',
      digitalAssetsManageSection: 'Library actions',
      digitalAssetsMerge: 'Merge libraries into this',
      digitalAssetsMergeTitle: 'Merge into "{name}"',
      digitalAssetsMergeHint: 'Select one or more source libraries. Their content is merged into the current library; sources are archived by default.',
      digitalAssetsMergeEmpty: 'No other libraries available to merge.',
      digitalAssetsMergeConfirm: 'Merge selected',
      digitalAssetsMergeCancel: 'Cancel',
      digitalAssetsMergeNeedSelect: 'Select at least one source library.',
      digitalAssetsMergeArchive: 'Archive source libraries after merge',
      digitalAssetsExport: 'Export backup',
      digitalAssetsImportBackup: 'Restore backup',
      digitalAssetsEmpty: 'No libraries yet. Create one to import documents.',
      digitalAssetsSelectHint: 'Select a library on the left',
      digitalAssetsLoadFailed: 'Load digital assets failed: {error}',
      digitalAssetsDisabledHint: 'Digital Assets is off for this tenant. Enable it under System Settings > Digital Assets.',
      digitalAssetsCreateDone: 'Library created.',
      digitalAssetsDelete: 'Delete',
      digitalAssetsImportDone: 'Import finished: {status}',
      digitalAssetsSharePrompt: 'Paste knowledge share link or knowledge ID',
      digitalAssetsNoHits: 'No hits',
      digitalAssetsViewContent: 'View contents',
      digitalAssetsContentTitle: 'Library contents: {name}',
      digitalAssetsContentClose: 'Close',
      digitalAssetsContentSources: 'Imported files',
      digitalAssetsContentJobs: 'Import history',
      digitalAssetsContentEmptySources: 'No imported files yet.',
      digitalAssetsContentEmptyJobs: 'No import jobs yet.',
      digitalAssetsContentLoadFailed: 'Load contents failed: {error}',
      digitalAssetsContentRefresh: 'Refresh',
      digitalAssetsContentSearch: 'Filter files...',
      digitalAssetsContentShowing: 'Showing {count} of {total}',
      digitalAssetsContentSelected: '{n} selected',
      digitalAssetsContentSelectAll: 'Select loaded',
      digitalAssetsContentClearSelect: 'Clear',
      digitalAssetsContentDeleteSelected: 'Delete selected',
      digitalAssetsContentDeleteOne: 'Delete',
      digitalAssetsContentDeleteConfirm: 'Delete {n} file(s) from this library? Clients will stop receiving them on next sync.',
      digitalAssetsContentDeleteDone: 'Deleted {n} file(s).',
      digitalAssetsContentDeleteFailed: 'Delete failed: {error}',
      digitalAssetsContentBusy: 'Working...',
      digitalAssetsContentTruncated: 'More files available; load more or filter to narrow.',
      digitalAssetsContentAutoFillCapped: 'Stopped auto-loading after many pages. Scroll or click Load more to continue.',
      digitalAssetsContentLoadMore: 'Load more',
      digitalAssetsContentLoadingMore: 'Loading more...',
      digitalAssetsProgressTitle: 'Import in progress',
      digitalAssetsProgressUploading: 'Uploading...',
      digitalAssetsProgressProcessing: 'Processing on server...',
      digitalAssetsProgressPhase: 'Phase: {phase}',
      digitalAssetsProgressFile: 'Current: {file}',
      digitalAssetsProgressCount: '{processed} / {total} files (imported {imported})',
      digitalAssetsProgressDone: 'Import completed',
      digitalAssetsProgressFailed: 'Import failed: {error}',
      digitalAssetsProgressTimeout: 'Import progress timed out; check import history.',
      digitalAssetsPhaseQueued: 'Queued',
      digitalAssetsPhaseUploading: 'Uploading',
      digitalAssetsPhaseExtracting: 'Extracting archive',
      digitalAssetsPhaseImporting: 'Importing files',
      digitalAssetsPhasePackaging: 'Building sync package',
      digitalAssetsPhaseMerging: 'Merging libraries',
      digitalAssetsPhaseRestoring: 'Restoring backup',
      digitalAssetsPhaseExporting: 'Exporting backup',
      digitalAssetsPhaseDone: 'Completed',
      digitalAssetsPhaseFailed: 'Failed',
      digitalAssetsJobKind: 'Kind',
      digitalAssetsJobStatus: 'Status',
      digitalAssetsJobFiles: 'Files',
      digitalAssetsJobTime: 'Time',
      digitalAssetsJobProgress: 'Progress',
      digitalAssetsAclSection: 'Access control',
      digitalAssetsAclHint: 'Choose whether everyone can access this library or only members of selected departments and their child departments.',
      digitalAssetsAclMode: 'Access mode',
      digitalAssetsAclAllMembers: 'All tenant members',
      digitalAssetsAclRestricted: 'Selected departments and child departments',
      digitalAssetsAclDepartments: 'Organization',
      digitalAssetsAclDepartmentsHint: 'Select one or more departments. Members of their child departments are included automatically.',
      digitalAssetsAclSelectedDepartments: '{n} department(s) selected',
      digitalAssetsAclDepartmentsLimit: 'You can select up to {n} departments.',
      digitalAssetsAclClearDepartments: 'Clear selection',
      digitalAssetsAclDepartmentsEmpty: 'No security groups found. Create departments under Security first.',
      digitalAssetsAclDepartmentsLoading: 'Loading departments...',
      digitalAssetsAclEmptyRestrictedWarn: 'Select at least one department. Otherwise no regular member can access this library.',
      digitalAssetsAclSync: 'Client sync enabled',
      digitalAssetsAclSyncHint: 'When off, authorized clients keep local cache but stop pulling updates.',
      digitalAssetsAclSave: 'Save access settings',
      digitalAssetsAclSaving: 'Saving...',
      digitalAssetsAclSaved: 'Access settings saved.',
      digitalAssetsAclSaveFailed: 'Save access settings failed: {error}',
      digitalAssetsAclSummaryAll: 'ACL: all members',
      digitalAssetsAclSummaryRestricted: 'ACL: {depts} department(s) and child departments',
      digitalAssetsAclSummarySyncOff: 'sync off',
      digitalAssetsAclDeptFilter: 'Filter departments...',
      digitalAssetsAclDeptUnknown: 'Unknown department (kept): {id}',
      digitalAssetsAclGroupsFailed: 'Failed to load departments: {error}',
      digitalAssetsAclReloadGroups: 'Reload departments',
      digitalAssetsDialogCancel: 'Cancel',
      digitalAssetsDialogOk: 'OK',
      digitalAssetsDialogConfirm: 'Confirm',
      digitalAssetsDialogRequired: 'This field is required.',
      digitalAssetsCreateTitle: 'New library',
      digitalAssetsCreateNamePrompt: 'Enter a name for the new library.',
      digitalAssetsCreateNameRequired: 'Library name is required.',
      digitalAssetsServerDirTitle: 'Server directory import',
      digitalAssetsShareTitle: 'Import knowledge share',
      digitalAssetsSearchTitle: 'Search library',
      digitalAssetsSearchPrompt: 'Enter search keywords.',
      digitalAssetsDeleteLibraryTitle: 'Delete library',
      digitalAssetsDeleteLibraryConfirm: 'Delete library "{name}"? This cannot be undone from the admin list (soft delete).',
      digitalAssetsDeleteSourcesTitle: 'Delete files',
      digitalAssetsAclConfirmTitle: 'Save restricted access',
      digitalAssetsCardMeta: 'rev {rev} | src {src} | sync {sync}',
      digitalAssetsRevLabel: 'rev {rev}',
      digitalAssetsSyncOn: 'on',
      digitalAssetsSyncOff: 'off',
      digitalAssetsKind: 'Library type',
      digitalAssetsKindBusiness: 'Business',
      digitalAssetsKindTechnical: 'Technical',
      digitalAssetsKindPrompt: 'Library type: business or technical',
      digitalAssetsAcceptsSubmissions: 'Accept member contributions',
      digitalAssetsAcceptsHint: 'When on, members can submit personal experience for admin review.',
      digitalAssetsQueueSection: 'Contribution queue',
      digitalAssetsQueueEmpty: 'No pending contributions for this library.',
      digitalAssetsQueueLoadFailed: 'Failed to load contributions: {error}',
      digitalAssetsQueueApprove: 'Approve',
      digitalAssetsQueueReject: 'Reject',
      digitalAssetsQueueRejectPrompt: 'Why is this contribution rejected?',
      digitalAssetsQueueRejected: 'Rejected.',
      digitalAssetsQueueApproved: 'Approved and imported.',
      digitalAssetsQueueMeta: '{kind} · {count} item(s) · {email}',
      digitalAssetsKindBadgeBusiness: 'business',
      digitalAssetsKindBadgeTechnical: 'technical'
    });
    I18N.zh = Object.assign({}, I18N.zh, {
      digitalAssetsTabTitle: '\u6570\u5b57\u8d44\u4ea7',
      digitalAssetsTabSubtitle: '\u79df\u6237\u4f01\u4e1a\u77e5\u8bc6\u5e93\u3001ACL\u3001\u5bfc\u5165\u4e0e\u5ba2\u6237\u7aef\u540c\u6b65\u3002',
      digitalAssetsNavDesc: '\u4f01\u4e1a\u77e5\u8bc6\u5e93',
      digitalAssetsReload: '\u5237\u65b0',
      digitalAssetsCreate: '\u65b0\u5efa\u5e93',
      digitalAssetsName: '\u540d\u79f0',
      digitalAssetsSearch: '\u68c0\u7d22\u5e93',
      digitalAssetsImportSection: '\u5bfc\u5165\u5185\u5bb9',
      digitalAssetsUpload: '\u4e0a\u4f20\u6587\u4ef6',
      digitalAssetsArchive: '\u4e0a\u4f20\u538b\u7f29\u5305',
      digitalAssetsBrowserDir: '\u6d4f\u89c8\u5668\u9009\u76ee\u5f55',
      digitalAssetsServerDir: '\u670d\u52a1\u5668\u76ee\u5f55',
      digitalAssetsServerDirPrompt: 'Hub \u670d\u52a1\u5668\u4e0a\u7684\u7edd\u5bf9\u8def\u5f84\uff08\u987b\u5728 local_dir_allowlist \u767d\u540d\u5355\u5185\uff09',
      digitalAssetsServerDirHint: '\u4ece Hub \u6240\u5728\u4e3b\u673a\u8def\u5f84\u5bfc\u5165\uff1b\u8def\u5f84\u987b\u5728\u79df\u6237 digital_assets \u8bbe\u7f6e\u7684\u767d\u540d\u5355\u4e2d\u3002',
      digitalAssetsShareImport: '\u4ece\u5206\u4eab\u94fe\u63a5\u5bfc\u5165',
      digitalAssetsManageSection: '\u5e93\u64cd\u4f5c',
      digitalAssetsMerge: '\u5408\u5e76\u5e93\u5230\u5f53\u524d\u5e93',
      digitalAssetsMergeTitle: '\u5408\u5e76\u5230\u300c{name}\u300d',
      digitalAssetsMergeHint: '\u52fe\u9009\u4e00\u4e2a\u6216\u591a\u4e2a\u6e90\u5e93\uff0c\u5185\u5bb9\u4f1a\u5408\u5e76\u5230\u5f53\u524d\u5e93\uff1b\u9ed8\u8ba4\u4f1a\u5f52\u6863\u6e90\u5e93\u3002',
      digitalAssetsMergeEmpty: '\u6ca1\u6709\u53ef\u5408\u5e76\u7684\u5176\u4ed6\u5e93\u3002',
      digitalAssetsMergeConfirm: '\u5408\u5e76\u6240\u9009',
      digitalAssetsMergeCancel: '\u53d6\u6d88',
      digitalAssetsMergeNeedSelect: '\u8bf7\u81f3\u5c11\u52fe\u9009\u4e00\u4e2a\u6e90\u5e93\u3002',
      digitalAssetsMergeArchive: '\u5408\u5e76\u540e\u5f52\u6863\u6e90\u5e93',
      digitalAssetsExport: '\u5bfc\u51fa\u5907\u4efd',
      digitalAssetsImportBackup: '\u6062\u590d\u5907\u4efd',
      digitalAssetsEmpty: '\u6682\u65e0\u5e93\uff0c\u8bf7\u5148\u521b\u5efa\u3002',
      digitalAssetsSelectHint: '\u8bf7\u5148\u5728\u5de6\u4fa7\u9009\u62e9\u4e00\u4e2a\u5e93',
      digitalAssetsLoadFailed: '\u52a0\u8f7d\u6570\u5b57\u8d44\u4ea7\u5931\u8d25\uff1a{error}',
      digitalAssetsDisabledHint: '\u5f53\u524d\u79df\u6237\u672a\u5f00\u542f\u6570\u5b57\u8d44\u4ea7\u3002\u8bf7\u5230\u300c\u7cfb\u7edf\u8bbe\u7f6e > \u6570\u5b57\u8d44\u4ea7\u300d\u5f00\u542f\u3002',
      digitalAssetsCreateDone: '\u5e93\u5df2\u521b\u5efa\u3002',
      digitalAssetsDelete: '\u5220\u9664',
      digitalAssetsImportDone: '\u5bfc\u5165\u5b8c\u6210\uff1a{status}',
      digitalAssetsSharePrompt: '\u7c98\u8d34\u77e5\u8bc6\u5206\u4eab\u94fe\u63a5\u6216\u77e5\u8bc6 ID',
      digitalAssetsNoHits: '\u65e0\u5339\u914d\u7ed3\u679c',
      digitalAssetsViewContent: '\u67e5\u770b\u5185\u5bb9',
      digitalAssetsContentTitle: '\u5e93\u5185\u5bb9\uff1a{name}',
      digitalAssetsContentClose: '\u5173\u95ed',
      digitalAssetsContentSources: '\u5df2\u5bfc\u5165\u6587\u4ef6',
      digitalAssetsContentJobs: '\u5bfc\u5165\u5386\u53f2',
      digitalAssetsContentEmptySources: '\u5c1a\u65e0\u5bfc\u5165\u6587\u4ef6\u3002',
      digitalAssetsContentEmptyJobs: '\u5c1a\u65e0\u5bfc\u5165\u8bb0\u5f55\u3002',
      digitalAssetsContentLoadFailed: '\u52a0\u8f7d\u5e93\u5185\u5bb9\u5931\u8d25\uff1a{error}',
      digitalAssetsContentRefresh: '\u5237\u65b0',
      digitalAssetsContentSearch: '\u7b5b\u9009\u6587\u4ef6...',
      digitalAssetsContentShowing: '\u663e\u793a {count} / {total}',
      digitalAssetsContentSelected: '\u5df2\u9009 {n}',
      digitalAssetsContentSelectAll: '\u5168\u9009\u5df2\u52a0\u8f7d',
      digitalAssetsContentClearSelect: '\u6e05\u7a7a',
      digitalAssetsContentDeleteSelected: '\u5220\u9664\u6240\u9009',
      digitalAssetsContentDeleteOne: '\u5220\u9664',
      digitalAssetsContentDeleteConfirm: '\u786e\u5b9a\u4ece\u672c\u5e93\u5220\u9664 {n} \u4e2a\u6587\u4ef6\uff1f\u5ba2\u6237\u7aef\u4e0b\u6b21\u540c\u6b65\u540e\u5c06\u4e0d\u518d\u6536\u5230\u8fd9\u4e9b\u5185\u5bb9\u3002',
      digitalAssetsContentDeleteDone: '\u5df2\u5220\u9664 {n} \u4e2a\u6587\u4ef6\u3002',
      digitalAssetsContentDeleteFailed: '\u5220\u9664\u5931\u8d25\uff1a{error}',
      digitalAssetsContentBusy: '\u5904\u7406\u4e2d...',
      digitalAssetsContentTruncated: '\u8fd8\u6709\u66f4\u591a\u6587\u4ef6\uff1b\u53ef\u52a0\u8f7d\u66f4\u591a\u6216\u7528\u7b5b\u9009\u7f29\u8303\u56f4\u3002',
      digitalAssetsContentAutoFillCapped: '\u5df2\u81ea\u52a8\u52a0\u8f7d\u591a\u9875\u540e\u505c\u6b62\uff1b\u8bf7\u6eda\u52a8\u6216\u70b9\u300c\u52a0\u8f7d\u66f4\u591a\u300d\u7ee7\u7eed\u3002',
      digitalAssetsContentLoadMore: '\u52a0\u8f7d\u66f4\u591a',
      digitalAssetsContentLoadingMore: '\u6b63\u5728\u52a0\u8f7d...',
      digitalAssetsProgressTitle: '\u5bfc\u5165\u8fdb\u884c\u4e2d',
      digitalAssetsProgressUploading: '\u4e0a\u4f20\u4e2d...',
      digitalAssetsProgressProcessing: '\u670d\u52a1\u7aef\u5904\u7406\u4e2d...',
      digitalAssetsProgressPhase: '\u9636\u6bb5\uff1a{phase}',
      digitalAssetsProgressFile: '\u5f53\u524d\uff1a{file}',
      digitalAssetsProgressCount: '{processed} / {total} \u4e2a\u6587\u4ef6\uff08\u5df2\u5bfc\u5165 {imported}\uff09',
      digitalAssetsProgressDone: '\u5bfc\u5165\u5b8c\u6210',
      digitalAssetsProgressFailed: '\u5bfc\u5165\u5931\u8d25\uff1a{error}',
      digitalAssetsProgressTimeout: '\u5bfc\u5165\u8fdb\u5ea6\u8d85\u65f6\uff0c\u8bf7\u5728\u5bfc\u5165\u5386\u53f2\u4e2d\u67e5\u770b\u3002',
      digitalAssetsPhaseQueued: '\u6392\u961f\u4e2d',
      digitalAssetsPhaseUploading: '\u4e0a\u4f20\u4e2d',
      digitalAssetsPhaseExtracting: '\u89e3\u538b\u4e2d',
      digitalAssetsPhaseImporting: '\u5bfc\u5165\u6587\u4ef6',
      digitalAssetsPhasePackaging: '\u6784\u5efa\u540c\u6b65\u5305',
      digitalAssetsPhaseMerging: '\u5408\u5e76\u5e93',
      digitalAssetsPhaseRestoring: '\u6062\u590d\u5907\u4efd',
      digitalAssetsPhaseExporting: '\u5bfc\u51fa\u5907\u4efd',
      digitalAssetsPhaseDone: '\u5df2\u5b8c\u6210',
      digitalAssetsPhaseFailed: '\u5df2\u5931\u8d25',
      digitalAssetsJobKind: '\u7c7b\u578b',
      digitalAssetsJobStatus: '\u72b6\u6001',
      digitalAssetsJobFiles: '\u6587\u4ef6',
      digitalAssetsJobTime: '\u65f6\u95f4',
      digitalAssetsJobProgress: '\u8fdb\u5ea6',
      digitalAssetsAclSection: '\u8bbf\u95ee\u6743\u9650',
      digitalAssetsAclHint: '\u9009\u62e9\u5168\u5458\u53ef\u8bbf\u95ee\uff0c\u6216\u4ec5\u5141\u8bb8\u6307\u5b9a\u90e8\u95e8\u53ca\u5176\u5b50\u90e8\u95e8\u6210\u5458\u8bbf\u95ee\u672c\u5e93\u3002',
      digitalAssetsAclMode: '\u8bbf\u95ee\u6a21\u5f0f',
      digitalAssetsAclAllMembers: '\u5168\u90e8\u79df\u6237\u6210\u5458',
      digitalAssetsAclRestricted: '\u6307\u5b9a\u90e8\u95e8\u53ca\u5b50\u90e8\u95e8',
      digitalAssetsAclDepartments: '\u7ec4\u7ec7\u673a\u6784',
      digitalAssetsAclDepartmentsHint: '\u53ef\u9009\u62e9\u4e00\u4e2a\u6216\u591a\u4e2a\u90e8\u95e8\uff1b\u6240\u9009\u90e8\u95e8\u7684\u5b50\u90e8\u95e8\u6210\u5458\u4f1a\u81ea\u52a8\u5305\u542b\u3002',
      digitalAssetsAclSelectedDepartments: '\u5df2\u9009 {n} \u4e2a\u90e8\u95e8',
      digitalAssetsAclDepartmentsLimit: '\u6700\u591a\u53ef\u9009 {n} \u4e2a\u90e8\u95e8\u3002',
      digitalAssetsAclClearDepartments: '\u6e05\u7a7a\u9009\u62e9',
      digitalAssetsAclDepartmentsEmpty: '\u6682\u65e0\u5b89\u5168\u7ec4\uff0c\u8bf7\u5148\u5728\u300c\u5b89\u5168\u300d\u4e2d\u521b\u5efa\u90e8\u95e8\u3002',
      digitalAssetsAclDepartmentsLoading: '\u6b63\u5728\u52a0\u8f7d\u90e8\u95e8...',
      digitalAssetsAclEmptyRestrictedWarn: '\u8bf7\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a\u90e8\u95e8\uff0c\u5426\u5219\u666e\u901a\u6210\u5458\u65e0\u6cd5\u8bbf\u95ee\u672c\u5e93\u3002',
      digitalAssetsAclSync: '\u5141\u8bb8\u5ba2\u6237\u7aef\u540c\u6b65',
      digitalAssetsAclSyncHint: '\u5173\u95ed\u540e\uff0c\u5df2\u6388\u6743\u5ba2\u6237\u7aef\u4fdd\u7559\u672c\u5730\u7f13\u5b58\u4f46\u4e0d\u518d\u62c9\u53d6\u66f4\u65b0\u3002',
      digitalAssetsAclSave: '\u4fdd\u5b58\u6743\u9650\u8bbe\u7f6e',
      digitalAssetsAclSaving: '\u4fdd\u5b58\u4e2d...',
      digitalAssetsAclSaved: '\u6743\u9650\u8bbe\u7f6e\u5df2\u4fdd\u5b58\u3002',
      digitalAssetsAclSaveFailed: '\u4fdd\u5b58\u6743\u9650\u5931\u8d25\uff1a{error}',
      digitalAssetsAclSummaryAll: 'ACL\uff1a\u5168\u4f53\u6210\u5458',
      digitalAssetsAclSummaryRestricted: 'ACL\uff1a{depts} \u4e2a\u90e8\u95e8\u53ca\u5b50\u90e8\u95e8',
      digitalAssetsAclSummarySyncOff: '\u540c\u6b65\u5173',
      digitalAssetsAclDeptFilter: '\u7b5b\u9009\u90e8\u95e8...',
      digitalAssetsAclDeptUnknown: '\u672a\u77e5\u90e8\u95e8\uff08\u4fdd\u7559\uff09\uff1a{id}',
      digitalAssetsAclGroupsFailed: '\u52a0\u8f7d\u90e8\u95e8\u5931\u8d25\uff1a{error}',
      digitalAssetsAclReloadGroups: '\u91cd\u65b0\u52a0\u8f7d\u90e8\u95e8',
      digitalAssetsDialogCancel: '\u53d6\u6d88',
      digitalAssetsDialogOk: '\u786e\u5b9a',
      digitalAssetsDialogConfirm: '\u786e\u8ba4',
      digitalAssetsDialogRequired: '\u6b64\u9879\u4e3a\u5fc5\u586b\u3002',
      digitalAssetsCreateTitle: '\u65b0\u5efa\u5e93',
      digitalAssetsCreateNamePrompt: '\u8bf7\u8f93\u5165\u65b0\u5e93\u540d\u79f0\u3002',
      digitalAssetsCreateNameRequired: '\u5e93\u540d\u79f0\u4e3a\u5fc5\u586b\u3002',
      digitalAssetsServerDirTitle: '\u670d\u52a1\u5668\u76ee\u5f55\u5bfc\u5165',
      digitalAssetsShareTitle: '\u4ece\u77e5\u8bc6\u5206\u4eab\u5bfc\u5165',
      digitalAssetsSearchTitle: '\u68c0\u7d22\u5e93',
      digitalAssetsSearchPrompt: '\u8bf7\u8f93\u5165\u68c0\u7d22\u5173\u952e\u8bcd\u3002',
      digitalAssetsDeleteLibraryTitle: '\u5220\u9664\u5e93',
      digitalAssetsDeleteLibraryConfirm: '\u786e\u5b9a\u5220\u9664\u5e93\u300c{name}\u300d\uff1f\u7ba1\u7406\u5217\u8868\u4e2d\u5c06\u8f6f\u5220\u9664\u3002',
      digitalAssetsDeleteSourcesTitle: '\u5220\u9664\u6587\u4ef6',
      digitalAssetsAclConfirmTitle: '\u4fdd\u5b58\u9650\u5b9a\u8bbf\u95ee',
      digitalAssetsCardMeta: '\u7248\u672c {rev} | \u6587\u4ef6 {src} | \u540c\u6b65 {sync}',
      digitalAssetsRevLabel: '\u7248\u672c {rev}',
      digitalAssetsSyncOn: '\u5f00',
      digitalAssetsSyncOff: '\u5173',
      digitalAssetsKind: '\u5e93\u7c7b\u578b',
      digitalAssetsKindBusiness: '\u4e1a\u52a1',
      digitalAssetsKindTechnical: '\u6280\u672f',
      digitalAssetsKindPrompt: '\u5e93\u7c7b\u578b\uff1abusiness \u6216 technical',
      digitalAssetsAcceptsSubmissions: '\u63a5\u53d7\u6210\u5458\u6295\u7a3f',
      digitalAssetsAcceptsHint: '\u5f00\u542f\u540e\u6210\u5458\u53ef\u628a\u4e2a\u4eba\u7ecf\u9a8c\u6295\u7a3f\u5230\u6b64\u5e93\uff0c\u7ba1\u7406\u5458\u5ba1\u6279\u540e\u5165\u5e93\u3002',
      digitalAssetsQueueSection: '\u6295\u7a3f\u961f\u5217',
      digitalAssetsQueueEmpty: '\u8be5\u5e93\u6682\u65e0\u5f85\u5ba1\u6295\u7a3f\u3002',
      digitalAssetsQueueLoadFailed: '\u52a0\u8f7d\u6295\u7a3f\u5931\u8d25\uff1a{error}',
      digitalAssetsQueueApprove: '\u6279\u51c6',
      digitalAssetsQueueReject: '\u62d2\u7edd',
      digitalAssetsQueueRejectPrompt: '\u8bf7\u8bf4\u660e\u62d2\u7edd\u539f\u56e0\u3002',
      digitalAssetsQueueRejected: '\u5df2\u62d2\u7edd\u3002',
      digitalAssetsQueueApproved: '\u5df2\u6279\u51c6\u5e76\u5165\u5e93\u3002',
      digitalAssetsQueueMeta: '{kind} \u00b7 {count} \u6761 \u00b7 {email}',
      digitalAssetsKindBadgeBusiness: '\u4e1a\u52a1',
      digitalAssetsKindBadgeTechnical: '\u6280\u672f'
    });
  }

  const state = {
    items: [],
    selectedId: '',
    hits: [],
    mergeOpen: false,
    progress: null,
    progressToken: 0,
    contentOpen: false,
    contentTab: 'sources',
    contentSources: [],
    contentJobs: [],
    contentSourcesTotal: 0,
    contentSourcesLimit: 100,
    contentSourcesOffset: 0,
    contentLoading: false,
    contentLoadingMore: false,
    contentBusy: false,
    contentQuery: '',
    contentSelected: {},
    contentLoadSeq: 0,
    contentLibraryId: '',
    contentSearchTimer: null,
    securityGroups: [],
    securityGroupTree: [],
    securityGroupsLoaded: false,
    securityGroupsLoading: false,
    securityGroupsPromise: null,
    securityGroupsError: '',
    aclSaving: false,
    aclSaveGuard: false,
    contentDeleteGuard: false,
    deleteLibraryBusy: false,
    aclDraft: null,
    aclDeptFilter: '',
    contentServerQuery: '',
    contentHasMore: false,
    contentScrollTop: 0,
    contentComposing: false,
    submissions: [],
    submissionsLoading: false,
    contentJobsPollTimer: null,
    contentJobsPollToken: 0,
    contentAutoFillRounds: 0,
    contentAutoFillCappedNotified: false,
    contentJobsPollDelayMs: 1500
  };

  function canManageDigitalAssets() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!profile && String(profile.scope || '').toLowerCase() === 'tenant';
  }

  function stopDigitalAssetsForUnauthorizedScope() {
    if (canManageDigitalAssets()) return;
    if (state.contentSearchTimer) {
      global.clearTimeout(state.contentSearchTimer);
      state.contentSearchTimer = null;
    }
    stopContentJobsPoll();
    state.contentJobsPollToken = (state.contentJobsPollToken || 0) + 1;
    state.progressToken = (state.progressToken || 0) + 1;
    state.progress = null;
    state.contentOpen = false;
    state.contentBusy = false;
    state.contentLoading = false;
    state.contentLoadingMore = false;
    state.contentLibraryId = '';
    state.contentSelected = {};
    state.items = [];
    state.selectedId = '';
    state.mergeOpen = false;
    clearAclDraft();
    var list = byID('digitalAssetsList');
    if (list) list.innerHTML = '';
    var detail = byID('digitalAssetsDetail');
    if (detail) detail.innerHTML = '';
    var overlayRoot = byID('digitalAssetsOverlayRoot');
    if (overlayRoot) overlayRoot.innerHTML = '';
  }

  var PHASE_I18N = {
    queued: 'digitalAssetsPhaseQueued',
    uploading: 'digitalAssetsPhaseUploading',
    extracting: 'digitalAssetsPhaseExtracting',
    importing: 'digitalAssetsPhaseImporting',
    packaging: 'digitalAssetsPhasePackaging',
    merging: 'digitalAssetsPhaseMerging',
    restoring: 'digitalAssetsPhaseRestoring',
    exporting: 'digitalAssetsPhaseExporting',
    done: 'digitalAssetsPhaseDone',
    failed: 'digitalAssetsPhaseFailed',
    succeeded: 'digitalAssetsPhaseDone'
  };

  function phaseLabel(phase) {
    var key = PHASE_I18N[String(phase || '').toLowerCase()];
    return key ? tr(key) : (phase || '');
  }

  function jobIdOf(job) {
    if (!job) return '';
    return job.id || job.job_id || '';
  }

  function isTerminalStatus(status) {
    var s = String(status || '').toLowerCase();
    return s === 'succeeded' || s === 'failed' || s === 'done' || s === 'canceled' || s === 'cancelled';
  }

  function tr(key, vars) {
    if (typeof global.tr === 'function') return global.tr(key, vars || {});
    const lang = (typeof global.getAdminLang === 'function' ? global.getAdminLang() : global.currentLang) || 'en';
    const normalized = String(lang).toLowerCase().startsWith('zh') ? 'zh' : 'en';
    const table = (I18N && I18N[normalized]) || (I18N && I18N.en) || {};
    let s = table[key] || (I18N && I18N.en && I18N.en[key]) || key;
    if (vars) Object.keys(vars).forEach(function(k) { s = s.replace('{' + k + '}', vars[k]); });
    return s;
  }
  function byID(id) { return global.document.getElementById(id); }
  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
  function api(path, opts) {
    if (typeof global.api === 'function') return global.api(path, opts);
    return Promise.reject(new Error('api unavailable'));
  }
  function showToast(msg, kind) {
    if (typeof global.showToast === 'function') global.showToast(msg, kind);
  }
  function sleep(ms) {
    return new Promise(function(resolve) { setTimeout(resolve, ms); });
  }

  // Custom dialogs only -- never window.alert / prompt / confirm.
  function isAdminDialogOpen() {
    return !!(global.AdminUI && typeof global.AdminUI.isDialogOpen === 'function'
      && global.AdminUI.isDialogOpen());
  }
  function showConfirm(message, options) {
    var opts = Object.assign({
      cancelText: tr('digitalAssetsDialogCancel'),
      confirmText: tr('digitalAssetsDialogConfirm')
    }, options || {});
    if (global.AdminUI && typeof global.AdminUI.confirmDialog === 'function') {
      return global.AdminUI.confirmDialog(message, opts);
    }
    // Fallback for tests without AdminUI: refuse rather than open a native dialog.
    return Promise.resolve(false);
  }
  function showPrompt(message, options) {
    var opts = Object.assign({
      cancelText: tr('digitalAssetsDialogCancel'),
      confirmText: tr('digitalAssetsDialogOk'),
      requiredText: tr('digitalAssetsDialogRequired')
    }, options || {});
    if (global.AdminUI && typeof global.AdminUI.promptDialog === 'function') {
      return global.AdminUI.promptDialog(message, opts);
    }
    return Promise.resolve(null);
  }

  function fileButton(id, label, attrs) {
    return '<label class="btn-secondary" style="height:34px;padding:0 12px;font-size:12px;display:inline-flex;align-items:center;cursor:pointer;margin:0">'
      + escapeHtml(label)
      + '<input id="' + id + '" type="file" ' + (attrs || '') + ' style="display:none"></label>';
  }
  function actionButton(id, label, kind) {
    var cls = 'btn-secondary';
    if (kind === 'danger') cls = 'btn-danger';
    else if (kind === 'primary') cls = 'btn-primary';
    else if (kind === 'ghost') cls = 'btn-ghost';
    return '<button type="button" class="' + cls + '" id="' + id + '" style="height:34px;padding:0 12px;font-size:12px">'
      + escapeHtml(label) + '</button>';
  }
  function sectionTitle(text) {
    return '<div class="item-meta" style="margin:14px 0 8px;font-weight:700;letter-spacing:.02em">' + escapeHtml(text) + '</div>';
  }

  function flattenSecurityGroups(node, path, out) {
    if (!node) return;
    var id = String(node.id || '').trim();
    var name = String(node.name || '').trim();
    var label = path ? (path + ' / ' + (name || id)) : (name || id);
    if (id) out.push({ id: id, name: name, path: label });
    var children = Array.isArray(node.children) ? node.children : [];
    children.forEach(function(child) {
      flattenSecurityGroups(child, label, out);
    });
  }

  var digitalAssetsMaxDepartmentTreeDepth = 64;

  function normalizeSecurityGroupTree(nodes, ancestry, seenIDs, depth) {
    var safeNodes = Array.isArray(nodes) ? nodes : [];
    var path = ancestry || {};
    var seen = seenIDs || {};
    var currentDepth = Number(depth) || 0;
    if (currentDepth > digitalAssetsMaxDepartmentTreeDepth) return [];
    return safeNodes.reduce(function(out, node) {
      if (!node || typeof node !== 'object') return out;
      var id = String(node.id || '').trim();
      // Do not render malformed, cyclic, or duplicate references received from the API.
      if (!id || path[id] || seen[id]) return out;
      var nextPath = Object.assign({}, path);
      nextPath[id] = true;
      seen[id] = true;
      out.push({
        id: id,
        name: String(node.name || '').trim(),
        children: normalizeSecurityGroupTree(node.children, nextPath, seen, currentDepth + 1)
      });
      return out;
    }, []);
  }

  async function loadSecurityGroups(opts) {
    opts = opts || {};
    if (state.securityGroupsPromise) {
      await state.securityGroupsPromise;
      if (state.securityGroupsLoaded && !opts.force) {
        if (opts.renderDetail !== false && state.selectedId) renderDetail();
        return state.securityGroups;
      }
    }
    if (state.securityGroupsLoaded && !opts.force) return state.securityGroups;

    state.securityGroupsLoading = true;
    var pending = (async function() {
      try {
        var data = await api('/api/admin/security/groups');
        var out = [];
        // API may return a single root node or (rarely) an array of roots.
        var tree = data && data.tree;
        var roots = normalizeSecurityGroupTree(Array.isArray(tree) ? tree : (tree ? [tree] : []));
        roots.forEach(function(node) { flattenSecurityGroups(node, '', out); });
        state.securityGroupTree = roots;
        state.securityGroups = out;
        state.securityGroupsError = '';
        state.securityGroupsLoaded = true;
      } catch (err) {
        state.securityGroups = [];
        state.securityGroupTree = [];
        state.securityGroupsError = String(err && err.message || err || 'error');
        state.securityGroupsLoaded = true;
      } finally {
        state.securityGroupsLoading = false;
        if (state.securityGroupsPromise === pending) state.securityGroupsPromise = null;
      }
    })();
    state.securityGroupsPromise = pending;
    await pending;
    if (opts.renderDetail !== false && state.selectedId) {
      // Preserve in-progress ACL edits while refreshing department list.
      captureAclDraftFromDom();
      renderDetail();
    }
    return state.securityGroups;
  }

  function aclModeOf(item) {
    var m = String(item && item.acl_mode || '').toLowerCase();
    return m === 'restricted' ? 'restricted' : 'all_members';
  }

  function listStringArray(v) {
    if (!v) return [];
    if (Array.isArray(v)) {
      return v.map(function(x) { return String(x || '').trim(); }).filter(Boolean);
    }
    return [];
  }

  function uniqueStrings(arr) {
    var seen = {};
    var out = [];
    (arr || []).forEach(function(x) {
      var k = String(x || '');
      if (!k || seen[k]) return;
      seen[k] = true;
      out.push(k);
    });
    return out;
  }

  function isDeptSelected(depts, deptId) {
    var want = String(deptId || '').trim();
    if (!want) return false;
    for (var i = 0; i < depts.length; i++) {
      if (depts[i] === want) return true;
    }
    return false;
  }

  function knownDeptIdSet() {
    var known = {};
    (state.securityGroups || []).forEach(function(g) {
      if (g && g.id) known[String(g.id)] = true;
    });
    return known;
  }

  function unknownSelectedDepartments(depts) {
    var known = knownDeptIdSet();
    return (depts || []).filter(function(id) { return id && !known[id]; });
  }

  function aclSummaryText(item) {
    if (!item) return '';
    var mode = aclModeOf(item);
    var text;
    if (mode === 'restricted') {
      text = tr('digitalAssetsAclSummaryRestricted', {
        depts: String(listStringArray(item.departments).length)
      });
    } else {
      text = tr('digitalAssetsAclSummaryAll');
    }
    if (!item.sync_enabled) {
      text += ' | ' + tr('digitalAssetsAclSummarySyncOff');
    }
    return text;
  }

  function clearAclDraft() {
    state.aclDraft = null;
    state.aclDeptFilter = '';
  }

  // Snapshot unsaved ACL form so merge toggle / group reload does not wipe edits.
  function captureAclDraftFromDom() {
    if (!state.selectedId) return null;
    if (!byID('digitalAssetsAclModeAll') && !byID('digitalAssetsAclModeRestricted')) return null;
    var form = collectAclFormRaw();
    var filterEl = byID('digitalAssetsAclDeptFilter');
    state.aclDraft = {
      libraryId: state.selectedId,
      mode: form.mode,
      departments: form.departments,
      sync_enabled: form.sync_enabled
    };
    if (filterEl) state.aclDeptFilter = filterEl.value || '';
    return state.aclDraft;
  }

  function draftForLibrary(libraryId) {
    if (!state.aclDraft || state.aclDraft.libraryId !== libraryId) return null;
    return state.aclDraft;
  }

  // Overlay saved library with in-progress draft for re-render.
  function itemWithAclDraft(item) {
    if (!item) return item;
    var draft = draftForLibrary(item.id);
    if (!draft) return item;
    return Object.assign({}, item, {
      acl_mode: draft.mode,
      departments: draft.departments,
      sync_enabled: draft.sync_enabled
    });
  }

  function updateAclRestrictedVisibility() {
    var modeEl = byID('digitalAssetsAclModeRestricted');
    var restricted = !!(modeEl && modeEl.checked);
    var box = byID('digitalAssetsAclRestrictedBox');
    if (box) {
      box.hidden = !restricted;
      box.setAttribute('aria-hidden', restricted ? 'false' : 'true');
    }
    updateAclEmptyWarn();
  }

  function aclRestrictionChanged(changedCheckbox) {
    var selected = collectAclFormRaw().departments.length;
    var restrictedEl = byID('digitalAssetsAclModeRestricted');
    var restricted = !!(restrictedEl && restrictedEl.checked);
    if (restricted && selected > digitalAssetsMaxAclDepartments && changedCheckbox && changedCheckbox.checked) {
      changedCheckbox.checked = false;
      selected -= 1;
      showToast(tr('digitalAssetsAclDepartmentsLimit', { n: String(digitalAssetsMaxAclDepartments) }), 'error');
    }
    updateAclEmptyWarn();
    captureAclDraftFromDom();
  }

  function updateAclEmptyWarn() {
    var warn = byID('digitalAssetsAclEmptyWarn');
    if (!warn) return;
    var modeEl = byID('digitalAssetsAclModeRestricted');
    var restricted = !!(modeEl && modeEl.checked);
    var form = collectAclFormRaw();
    updateAclSelectionSummary(form.departments.length);
    var overLimit = restricted && form.departments.length > digitalAssetsMaxAclDepartments;
    if (!restricted) {
      warn.style.display = 'none';
      var allMembersSave = byID('digitalAssetsAclSaveBtn');
      if (allMembersSave) allMembersSave.disabled = !!state.aclSaving;
      return;
    }
    var empty = !form.departments.length;
    warn.textContent = overLimit
      ? tr('digitalAssetsAclDepartmentsLimit', { n: String(digitalAssetsMaxAclDepartments) })
      : tr('digitalAssetsAclEmptyRestrictedWarn');
    warn.style.display = empty || overLimit ? '' : 'none';
    var save = byID('digitalAssetsAclSaveBtn');
    if (save) save.disabled = !!(state.aclSaving || empty || overLimit);
  }

  function updateAclSelectionSummary(count) {
    var summary = byID('digitalAssetsAclSelectedDepartments');
    if (!summary) return;
    if (count == null) count = collectAclFormRaw().departments.length;
    summary.textContent = tr('digitalAssetsAclSelectedDepartments', { n: String(count) });
  }

  function collectAclFormRaw() {
    var modeEl = byID('digitalAssetsAclModeRestricted');
    var mode = (modeEl && modeEl.checked) ? 'restricted' : 'all_members';
    var depts = [];
    // Include both visible and filter-hidden checked depts via data held in draft + DOM.
    detailQueryAll('.digital-assets-acl-dept').forEach(function(cb) {
      if (cb.checked) depts.push(String(cb.value || '').trim());
    });
    // Hidden-by-filter checkboxes remain in DOM and stay checked -- OK.
    depts = uniqueStrings(depts.filter(Boolean));
    var syncEl = byID('digitalAssetsAclSync');
    var syncEnabled = !syncEl || !!syncEl.checked;
    return {
      mode: mode,
      departments: depts,
      sync_enabled: syncEnabled
    };
  }

  function collectAclForm() {
    var raw = collectAclFormRaw();
    return {
      acl_mode: raw.mode,
      departments: raw.mode === 'restricted' ? raw.departments : [],
      sync_enabled: raw.sync_enabled,
      set_acl: true
    };
  }

  function applyDeptFilter() {
    var filterEl = byID('digitalAssetsAclDeptFilter');
    var q = String(filterEl && filterEl.value || '').trim().toLowerCase();
    state.aclDeptFilter = filterEl ? (filterEl.value || '') : '';
    var branches = detailQueryAll('.digital-assets-acl-tree-branch');
    for (var i = branches.length - 1; i >= 0; i -= 1) {
      var branch = branches[i];
      var row = branch.firstElementChild;
      var ownMatch = !q || String(row && row.getAttribute('data-filter') || '').toLowerCase().indexOf(q) >= 0;
      var childMatch = Array.prototype.slice.call(branch.children || []).some(function(child) {
        return child.classList && child.classList.contains('digital-assets-acl-tree-children')
          && Array.prototype.slice.call(child.children || []).some(function(grandchild) {
            return grandchild.style.display !== 'none';
          });
      });
      branch.style.display = ownMatch || childMatch ? '' : 'none';
    }
  }

  async function saveLibraryAcl(item) {
    if (!item || !item.id || state.aclSaving || state.aclSaveGuard) return;
    // Ignore re-entry while another AdminUI dialog is already open (double-click).
    if (isAdminDialogOpen()) return;
    // Sync re-entry lock covering confirm await + network (aclSaving alone is too late).
    state.aclSaveGuard = true;
    try {
      // Ensure filter-hidden checked depts are included (they stay in DOM).
      var payload = collectAclForm();
      if (payload.acl_mode === 'restricted' && !payload.departments.length) {
        showToast(tr('digitalAssetsAclEmptyRestrictedWarn'), 'error');
        updateAclEmptyWarn();
        return;
      }
      var acceptsEl = byID('digitalAssetsAcceptsSubmissions');
      var kindEl = byID('digitalAssetsLibraryKind');
      var body = {
        acl_mode: payload.acl_mode,
        departments: payload.departments,
        sync_enabled: payload.sync_enabled,
        set_acl: true,
        accepts_submissions: !acceptsEl || !!acceptsEl.checked
      };
      if (kindEl && kindEl.value) body.library_kind = kindEl.value;
      state.aclSaving = true;
      var btn = byID('digitalAssetsAclSaveBtn');
      if (btn) {
        btn.disabled = true;
        btn.textContent = tr('digitalAssetsAclSaving');
      }
      try {
        var updated = await api('/api/admin/digital-assets/libraries/' + encodeURIComponent(item.id), {
          method: 'PATCH',
          body: JSON.stringify(body)
        });
        // Merge response into local list so re-render shows saved ACL without full reload.
        if (updated && updated.id) {
          var idx = -1;
          for (var i = 0; i < state.items.length; i++) {
            if (state.items[i].id === updated.id) { idx = i; break; }
          }
          if (idx >= 0) state.items[idx] = Object.assign({}, state.items[idx], updated);
          else state.items.push(updated);
        }
        clearAclDraft();
        showToast(tr('digitalAssetsAclSaved'), 'success');
        state.aclSaving = false;
        renderList();
        // skipCapture: DOM still has pre-save form; do not re-snapshot it over the saved item.
        renderDetail({ skipCapture: true });
      } catch (err) {
        showToast(tr('digitalAssetsAclSaveFailed', { error: String(err.message || err) }), 'error');
        state.aclSaving = false;
        var btn2 = byID('digitalAssetsAclSaveBtn');
        if (btn2) {
          btn2.disabled = false;
          btn2.textContent = tr('digitalAssetsAclSave');
        }
      }
    } finally {
      state.aclSaveGuard = false;
    }
  }

  function renderDeptCheckbox(id, label, checked, extraCls, filterText, depth) {
    var accessibleLabel = String(filterText || label || id || '').trim();
    return '<label class="digital-assets-acl-dept-row' + (extraCls ? ' ' + extraCls : '') + '"'
      + ' data-filter="' + escapeHtml(accessibleLabel + ' ' + String(id || '')) + '"'
      + ' style="--digital-assets-tree-depth:' + String(depth || 0) + ';display:flex;align-items:center;gap:8px;padding:7px 8px;cursor:pointer;margin:0;font-size:12px">'
      + '<input type="checkbox" class="digital-assets-acl-dept" value="' + escapeHtml(id) + '"'
      + ' aria-label="' + escapeHtml(accessibleLabel) + '" title="' + escapeHtml(accessibleLabel) + '"'
      + (checked ? ' checked' : '') + '>'
      + '<span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + escapeHtml(label) + '</span></label>';
  }

  function renderDepartmentTree(nodes, selectedDepts, depth, parentPath) {
    var currentDepth = Number(depth) || 0;
    // The API tree is normalized on load; this guard also protects direct calls
    // against an unexpectedly deep response.
    if (currentDepth > digitalAssetsMaxDepartmentTreeDepth) return '';
    return (Array.isArray(nodes) ? nodes : []).map(function(node) {
      var id = String(node && node.id || '').trim();
      var name = String(node && node.name || '').trim();
      var label = name || id;
      var path = parentPath ? (parentPath + ' / ' + label) : label;
      var children = Array.isArray(node && node.children) ? node.children : [];
      var childHtml = renderDepartmentTree(children, selectedDepts, currentDepth + 1, path);
      if (!id) return childHtml;
      return '<div class="digital-assets-acl-tree-branch">'
        + renderDeptCheckbox(id, label, isDeptSelected(selectedDepts, id),
          children.length ? 'has-children' : '', path, currentDepth)
        + (childHtml ? '<div class="digital-assets-acl-tree-children">' + childHtml + '</div>' : '')
        + '</div>';
    }).join('');
  }

  function renderAclPanel(item) {
    var mode = aclModeOf(item);
    var selectedDepts = listStringArray(item.departments);
    var filterVal = state.aclDeptFilter || '';
    var deptsHtml;
    if (state.securityGroupsLoading && !state.securityGroupsLoaded) {
      deptsHtml = '<div class="item-meta">' + escapeHtml(tr('digitalAssetsAclDepartmentsLoading')) + '</div>';
    } else if (state.securityGroupsError) {
      deptsHtml = '<div class="item-meta" style="color:var(--danger,#b91c1c)">'
        + escapeHtml(tr('digitalAssetsAclGroupsFailed', { error: state.securityGroupsError }))
        + '</div>'
        + '<div class="actions" style="margin-top:6px">'
        + actionButton('digitalAssetsAclReloadGroupsBtn', tr('digitalAssetsAclReloadGroups'), 'ghost')
        + '</div>';
      // Still show orphan/selected depts so save does not wipe them while groups are down.
      var orphansWhileErr = uniqueStrings(selectedDepts);
      if (orphansWhileErr.length) {
        deptsHtml += '<div style="max-height:160px;overflow:auto;border:1px solid rgba(31,34,48,.08);border-radius:8px;padding:8px 10px;margin-top:8px">'
          + orphansWhileErr.map(function(id) {
            return renderDeptCheckbox(id, tr('digitalAssetsAclDeptUnknown', { id: id }), true, 'is-unknown');
          }).join('')
          + '</div>';
      }
    } else {
      var knownRows = renderDepartmentTree(state.securityGroupTree, selectedDepts, 0, '');
      var unknown = unknownSelectedDepartments(selectedDepts);
      var unknownRows = unknown.map(function(id) {
        return renderDeptCheckbox(id, tr('digitalAssetsAclDeptUnknown', { id: id }), true, 'is-unknown', id, 0);
      });
      if (!knownRows && !unknownRows.length) {
        deptsHtml = '<div class="item-meta">' + escapeHtml(tr('digitalAssetsAclDepartmentsEmpty')) + '</div>'
          + '<div class="actions" style="margin-top:6px">'
          + actionButton('digitalAssetsAclReloadGroupsBtn', tr('digitalAssetsAclReloadGroups'), 'ghost')
          + '</div>';
      } else {
        deptsHtml = '<div class="digital-assets-acl-tree-toolbar">'
          + '<input id="digitalAssetsAclDeptFilter" type="search" value="' + escapeHtml(filterVal) + '"'
          + ' placeholder="' + escapeHtml(tr('digitalAssetsAclDeptFilter')) + '"'
          + ' aria-label="' + escapeHtml(tr('digitalAssetsAclDeptFilter')) + '"'
          + '>'
          + actionButton('digitalAssetsAclClearDepartmentsBtn', tr('digitalAssetsAclClearDepartments'), 'ghost')
          + '</div>'
          + '<div class="digital-assets-acl-tree" role="group" aria-label="' + escapeHtml(tr('digitalAssetsAclDepartments')) + '">'
          + unknownRows.join('')
          + knownRows
          + '</div>';
      }
    }
    var emptyGrants = mode === 'restricted' && !selectedDepts.length;
    return ''
      + sectionTitle(tr('digitalAssetsAclSection'))
      + '<div class="item-meta" style="margin:0 0 10px">' + escapeHtml(tr('digitalAssetsAclHint')) + '</div>'
      + '<div class="item-meta" style="margin:0 0 6px;font-weight:600">' + escapeHtml(tr('digitalAssetsAclMode')) + '</div>'
      + '<label style="display:flex;align-items:center;gap:8px;margin:0 0 6px;cursor:pointer;font-size:13px">'
      + '<input type="radio" name="digitalAssetsAclMode" id="digitalAssetsAclModeAll" value="all_members"'
      + (mode === 'all_members' ? ' checked' : '') + '> '
      + escapeHtml(tr('digitalAssetsAclAllMembers')) + '</label>'
      + '<label style="display:flex;align-items:center;gap:8px;margin:0 0 10px;cursor:pointer;font-size:13px">'
      + '<input type="radio" name="digitalAssetsAclMode" id="digitalAssetsAclModeRestricted" value="restricted"'
      + (mode === 'restricted' ? ' checked' : '') + '> '
      + escapeHtml(tr('digitalAssetsAclRestricted')) + '</label>'
      + '<div id="digitalAssetsAclRestrictedBox"' + (mode === 'restricted' ? '' : ' hidden aria-hidden="true"') + '>'
      + '<div class="item-meta" style="margin:0 0 6px;font-weight:600">' + escapeHtml(tr('digitalAssetsAclDepartments')) + '</div>'
      + '<div class="item-meta" style="margin:0 0 8px">' + escapeHtml(tr('digitalAssetsAclDepartmentsHint')) + '</div>'
      + '<div id="digitalAssetsAclSelectedDepartments" class="item-meta" aria-live="polite" style="margin:0 0 8px;font-weight:600">'
      + escapeHtml(tr('digitalAssetsAclSelectedDepartments', { n: String(selectedDepts.length) })) + '</div>'
      + deptsHtml
      + '<div id="digitalAssetsAclEmptyWarn" class="item-meta" style="margin-top:8px;color:var(--warning,#b45309)'
      + (emptyGrants ? '' : ';display:none') + '">'
      + escapeHtml(tr('digitalAssetsAclEmptyRestrictedWarn')) + '</div>'
      + '</div>'
      + '<label style="display:flex;align-items:center;gap:8px;margin:14px 0 4px;cursor:pointer;font-size:13px;font-weight:600">'
      + '<input type="checkbox" id="digitalAssetsAclSync"' + (item.sync_enabled !== false ? ' checked' : '') + '> '
      + escapeHtml(tr('digitalAssetsAclSync')) + '</label>'
      + '<div class="item-meta" style="margin:0 0 10px">' + escapeHtml(tr('digitalAssetsAclSyncHint')) + '</div>'
      + '<div class="actions" style="display:flex;gap:8px;flex-wrap:wrap;margin:0">'
      + actionButton('digitalAssetsAclSaveBtn',
        state.aclSaving ? tr('digitalAssetsAclSaving') : tr('digitalAssetsAclSave'), 'primary')
      + '</div>';
  }

  function ensureOverlayRoot() {
    var root = byID('digitalAssetsOverlayRoot');
    if (root) return root;
    root = global.document.createElement('div');
    root.id = 'digitalAssetsOverlayRoot';
    global.document.body.appendChild(root);
    return root;
  }

  function setProgress(p, token) {
    // Ignore late updates from a superseded import tracker.
    if (token != null && token !== state.progressToken) return;
    state.progress = p;
    renderOverlays();
  }

  function beginProgress(initial) {
    state.progressToken += 1;
    var token = state.progressToken;
    setProgress(initial || { phase: 'queued', percent: 0 }, token);
    return token;
  }

  function clearProgress(token) {
    if (token != null && token !== state.progressToken) return;
    state.progress = null;
    renderOverlays();
    // Progress modal blocked jobs polling; resume if jobs tab still has running work.
    scheduleContentJobsPoll();
  }

  function captureContentScroll() {
    var scroller = byID('digitalAssetsContentScroll');
    if (scroller) state.contentScrollTop = scroller.scrollTop || 0;
  }

  function restoreContentScroll() {
    var scroller = byID('digitalAssetsContentScroll');
    if (scroller && state.contentScrollTop > 0) {
      scroller.scrollTop = state.contentScrollTop;
    }
  }

  function updateSelectionChrome() {
    // Lightweight update for selected-count / badges without rebuilding the whole dialog.
    var selectedCount = selectedSourceIds().length;
    var delSel = byID('digitalAssetsContentDeleteSelectedBtn');
    if (delSel) {
      delSel.disabled = !(selectedCount && !state.contentBusy);
      delSel.textContent = tr('digitalAssetsContentDeleteSelected')
        + (selectedCount ? ' (' + selectedCount + ')' : '');
    }
    var tabSources = byID('digitalAssetsContentTabSources');
    if (tabSources) {
      tabSources.textContent = tr('digitalAssetsContentSources')
        + (state.contentSourcesTotal ? ' (' + state.contentSourcesTotal + ')' : '');
    }
    var tabJobs = byID('digitalAssetsContentTabJobs');
    if (tabJobs) {
      tabJobs.textContent = tr('digitalAssetsContentJobs')
        + (state.contentJobs && state.contentJobs.length ? ' (' + state.contentJobs.length + ')' : '');
    }
    var meta = byID('digitalAssetsContentMeta');
    if (meta) {
      var shown = filteredSources(state.contentSources);
      var total = state.contentSourcesTotal || (state.contentSources || []).length;
      var text = tr('digitalAssetsContentShowing', {
        count: String(shown.length), total: String(total)
      });
      if (selectedCount) text += ' | ' + tr('digitalAssetsContentSelected', { n: String(selectedCount) });
      if (state.contentHasMore) {
        text += ' | ' + tr('digitalAssetsContentTruncated');
      }
      meta.textContent = text;
    }
    var loadMore = byID('digitalAssetsContentLoadMoreBtn');
    if (loadMore) {
      loadMore.style.display = state.contentHasMore ? '' : 'none';
      loadMore.disabled = !!(state.contentBusy || state.contentLoading || state.contentLoadingMore);
      loadMore.textContent = state.contentLoadingMore
        ? tr('digitalAssetsContentLoadingMore')
        : tr('digitalAssetsContentLoadMore');
    }
  }

  // Update only the sources list body (keeps search input / IME composition intact).
  // opts.stickBottom: after load-more, if already near bottom, pin to bottom so new rows show.
  function renderSourcesListBody(opts) {
    opts = opts || {};
    if (!state.contentOpen || state.contentTab !== 'sources') {
      renderOverlays();
      return;
    }
    var scroll = byID('digitalAssetsContentScroll');
    if (!scroll) {
      renderOverlays();
      return;
    }
    // Always capture first so a mid-list load-more does not jump to top after rebuild.
    var nearBottom = (scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight) < 140;
    captureContentScroll();
    scroll.innerHTML = renderSourcesList(filteredSources(state.contentSources));
    if (opts.stickBottom && nearBottom) {
      scroll.scrollTop = scroll.scrollHeight;
      state.contentScrollTop = scroll.scrollTop;
    } else {
      restoreContentScroll();
    }
    updateSelectionChrome();
    wireContentScrollLoadMore();
    // If the list still does not fill the viewport (or we are still near bottom), chain pages.
    if (opts.autoFill !== false) {
      maybeAutoFillSources();
    }
  }

  // Near-bottom auto load-more (sources tab only).
  function wireContentScrollLoadMore() {
    var scroll = byID('digitalAssetsContentScroll');
    if (!scroll || scroll._daScrollBound) return;
    scroll._daScrollBound = true;
    scroll.addEventListener('scroll', function() {
      if (!state.contentOpen || state.contentTab !== 'sources') return;
      if (!state.contentHasMore || state.contentLoadingMore || state.contentLoading || state.contentBusy) return;
      var remain = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight;
      if (remain < 120) {
        // User-driven scroll resets auto-fill budget.
        state.contentAutoFillRounds = 0;
        state.contentAutoFillCappedNotified = false;
        loadMoreContentSources();
      }
    });
  }

  // After first paint / load-more, keep fetching while content is shorter than the viewport
  // (scroll events alone never fire if there is nothing to scroll).
  function maybeAutoFillSources() {
    if (!state.contentOpen || state.contentTab !== 'sources') return;
    if (!state.contentHasMore || state.contentLoadingMore || state.contentLoading || state.contentBusy) return;
    // Hard cap chained auto-fills so a sticky has_more cannot hammer the API.
    var rounds = state.contentAutoFillRounds || 0;
    if (rounds >= 20) {
      if (!state.contentAutoFillCappedNotified) {
        state.contentAutoFillCappedNotified = true;
        showToast(tr('digitalAssetsContentAutoFillCapped'), 'error');
      }
      return;
    }
    var scroll = byID('digitalAssetsContentScroll');
    if (!scroll) return;
    var shortContent = scroll.scrollHeight <= scroll.clientHeight + 8;
    var nearBottom = (scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight) < 120;
    if (shortContent || nearBottom) {
      loadMoreContentSources({ fromAutoFill: true });
    }
  }

  function maybeRefreshOpenContent(libraryId) {
    if (!state.contentOpen || !state.contentLibraryId) return;
    if (libraryId && libraryId !== state.contentLibraryId) return;
    loadContentDialogData(state.contentLibraryId, {
      keepTab: true,
      keepSelection: true,
      keepQuery: true,
      softFail: true,
      quiet: true
    });
  }

  function renderOverlays(opts) {
    opts = opts || {};
    if (!opts.skipScrollCapture) captureContentScroll();
    var root = ensureOverlayRoot();
    var html = '';
    if (state.progress) {
      var p = state.progress;
      var percent = Math.max(0, Math.min(100, Math.round(Number(p.percent) || 0)));
      var phase = p.phase || '';
      var file = p.current_file || p.file || '';
      var phaseText = phaseLabel(phase);
      var line1 = p.message || (phase === 'uploading'
        ? tr('digitalAssetsProgressUploading')
        : tr('digitalAssetsProgressProcessing'));
      if (phaseText) {
        line1 = tr('digitalAssetsProgressPhase', { phase: phaseText });
      }
      var step = '';
      if (p.current_step) {
        step = p.current_step;
        if (p.current_step_num && p.total_steps) {
          step += ' (' + p.current_step_num + '/' + p.total_steps + ')';
        }
      }
      html += '<div style="position:fixed;inset:0;z-index:12000;background:rgba(20,24,32,.35);display:flex;align-items:center;justify-content:center;padding:20px">'
        + '<div class="card item" style="width:min(480px,100%);padding:18px 20px;box-shadow:0 18px 40px rgba(20,24,32,.18)">'
        + '<div class="item-title" style="font-size:15px">' + escapeHtml(tr('digitalAssetsProgressTitle')) + '</div>'
        + '<div class="item-meta" style="margin-top:8px">' + escapeHtml(line1) + '</div>'
        + (file ? '<div class="item-meta mono" style="margin-top:4px">' + escapeHtml(tr('digitalAssetsProgressFile', { file: file })) + '</div>' : '')
        + (step ? '<div class="item-meta" style="margin-top:4px">' + escapeHtml(step) + '</div>' : '')
        + ((p.total || p.total_files) ? '<div class="item-meta" style="margin-top:4px">' + escapeHtml(tr('digitalAssetsProgressCount', {
          processed: String(p.processed || 0),
          total: String(p.total || p.total_files || 0),
          imported: String(p.imported || 0)
        })) + '</div>' : '')
        + '<div style="margin-top:14px;height:10px;border-radius:999px;background:rgba(31,34,48,.08);overflow:hidden">'
        + '<div style="height:100%;width:' + percent + '%;background:linear-gradient(90deg,#3b82f6,#60a5fa);transition:width .25s ease"></div>'
        + '</div>'
        + '<div class="item-meta" style="margin-top:8px;text-align:right">' + percent + '%</div>'
        + '</div></div>';
    }
    if (state.contentOpen) {
      html += renderContentDialogHtml();
    }
    root.innerHTML = html;
    wireOverlayHandlers(opts);
    restoreContentScroll();
    if (state.contentOpen && state.contentTab === 'sources') {
      wireContentScrollLoadMore();
    }
  }

  function selectedSourceIds() {
    return Object.keys(state.contentSelected || {}).filter(function(id) { return !!state.contentSelected[id]; });
  }

  function filteredSources(items) {
    // Server already filtered when contentServerQuery is set; still apply local filter
    // for instant feedback while debounce is pending.
    var q = String(state.contentQuery || '').trim().toLowerCase();
    var serverQ = String(state.contentServerQuery || '').trim().toLowerCase();
    if (!q || q === serverQ) return items || [];
    return (items || []).filter(function(src) {
      var hay = [
        src.title, src.relative_path, src.uri, src.id, src.kind, src.batch_id, src.status
      ].map(function(x) { return String(x || '').toLowerCase(); }).join(' ');
      return hay.indexOf(q) >= 0;
    });
  }

  function renderContentDialogHtml() {
    var item = state.items.find(function(x) { return x.id === state.selectedId || x.id === state.contentLibraryId; }) || {};
    var tab = state.contentTab || 'sources';
    var body = '';
    var toolbar = '';
    var selectedCount = selectedSourceIds().length;
    var sourcesShown = filteredSources(state.contentSources);
    if (state.contentLoading && !(state.contentSources || []).length && !(state.contentJobs || []).length) {
      body = '<div class="item-meta" style="padding:16px 0">...</div>';
    } else if (tab === 'jobs') {
      body = renderJobsList(state.contentJobs);
    } else {
      toolbar = renderSourcesToolbar(sourcesShown, selectedCount);
      body = renderSourcesList(sourcesShown);
    }
    var sourcesLabel = tr('digitalAssetsContentSources')
      + (state.contentSourcesTotal ? ' (' + state.contentSourcesTotal + ')' : '');
    var jobsLabel = tr('digitalAssetsContentJobs')
      + (state.contentJobs && state.contentJobs.length ? ' (' + state.contentJobs.length + ')' : '');
    return '<div id="digitalAssetsContentBackdrop" style="position:fixed;inset:0;z-index:11000;background:rgba(20,24,32,.35);display:flex;align-items:center;justify-content:center;padding:20px">'
      + '<div id="digitalAssetsContentPanel" class="card item" style="width:min(780px,100%);max-height:min(84vh,760px);display:flex;flex-direction:column;padding:0;overflow:hidden;box-shadow:0 18px 40px rgba(20,24,32,.18)">'
      + '<div style="padding:14px 16px;border-bottom:1px solid rgba(31,34,48,.08);display:flex;justify-content:space-between;gap:12px;align-items:center">'
      + '<div><div class="item-title" style="font-size:15px">' + escapeHtml(tr('digitalAssetsContentTitle', { name: item.name || item.id || '' })) + '</div>'
      + '<div class="item-meta mono" style="margin-top:2px">' + escapeHtml(item.id || state.contentLibraryId || '') + '</div></div>'
      + '<div style="display:flex;gap:8px;align-items:center">'
      + actionButton('digitalAssetsContentRefreshBtn', tr('digitalAssetsContentRefresh'), 'ghost')
      + actionButton('digitalAssetsContentCloseBtn', tr('digitalAssetsContentClose'), 'ghost')
      + '</div></div>'
      + '<div style="padding:10px 16px 0;display:flex;gap:8px;flex-wrap:wrap;align-items:center">'
      + '<button type="button" class="' + (tab === 'sources' ? 'btn-primary' : 'btn-ghost') + '" id="digitalAssetsContentTabSources" style="height:32px;padding:0 12px;font-size:12px">'
      + escapeHtml(sourcesLabel) + '</button>'
      + '<button type="button" class="' + (tab === 'jobs' ? 'btn-primary' : 'btn-ghost') + '" id="digitalAssetsContentTabJobs" style="height:32px;padding:0 12px;font-size:12px">'
      + escapeHtml(jobsLabel) + '</button>'
      + (state.contentBusy || state.contentLoading ? '<span class="item-meta">' + escapeHtml(tr('digitalAssetsContentBusy')) + '</span>' : '')
      + '</div>'
      + (toolbar || '')
      + '<div id="digitalAssetsContentScroll" style="padding:12px 16px 16px;overflow:auto;flex:1;min-height:0">' + body + '</div>'
      + '</div></div>';
  }

  function renderSourcesToolbar(shown, selectedCount) {
    var total = state.contentSourcesTotal || (state.contentSources || []).length;
    var count = (shown || []).length;
    var truncated = !!state.contentHasMore;
    return '<div style="padding:10px 16px 0;display:flex;flex-direction:column;gap:8px">'
      + '<div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">'
      + '<input id="digitalAssetsContentSearch" type="search" value="' + escapeHtml(state.contentQuery || '') + '" placeholder="'
      + escapeHtml(tr('digitalAssetsContentSearch')) + '" style="flex:1;min-width:160px;height:34px;padding:0 10px;border:1px solid rgba(31,34,48,.12);border-radius:8px;font-size:12px"'
      + (state.contentBusy ? ' disabled' : '') + '>'
      + actionButton('digitalAssetsContentSelectAllBtn', tr('digitalAssetsContentSelectAll'), 'ghost')
      + actionButton('digitalAssetsContentClearSelectBtn', tr('digitalAssetsContentClearSelect'), 'ghost')
      + '<button type="button" class="btn-danger" id="digitalAssetsContentDeleteSelectedBtn" style="height:34px;padding:0 12px;font-size:12px"'
      + (selectedCount && !state.contentBusy ? '' : ' disabled') + '>'
      + escapeHtml(tr('digitalAssetsContentDeleteSelected'))
      + (selectedCount ? ' (' + selectedCount + ')' : '') + '</button>'
      + '</div>'
      + '<div class="item-meta" id="digitalAssetsContentMeta">' + escapeHtml(tr('digitalAssetsContentShowing', {
        count: String(count), total: String(total)
      }))
      + (selectedCount ? ' | ' + escapeHtml(tr('digitalAssetsContentSelected', { n: String(selectedCount) })) : '')
      + (truncated ? ' | ' + escapeHtml(tr('digitalAssetsContentTruncated')) : '')
      + '</div></div>';
  }

  function renderLoadMoreButtonHtml() {
    if (!state.contentHasMore) return '';
    return '<div style="display:flex;justify-content:center;padding:8px 0 4px">'
      + '<button type="button" class="btn-secondary" id="digitalAssetsContentLoadMoreBtn" style="height:34px;padding:0 16px;font-size:12px"'
      + (state.contentBusy || state.contentLoading || state.contentLoadingMore ? ' disabled' : '') + '>'
      + escapeHtml(state.contentLoadingMore ? tr('digitalAssetsContentLoadingMore') : tr('digitalAssetsContentLoadMore'))
      + '</button></div>';
  }

  function renderSourcesList(items) {
    if (!items || !items.length) {
      // Keep Load more visible when the current filter matches nothing in already-loaded
      // pages but the server still reports more rows (or a server search is pending).
      return '<div class="empty">' + escapeHtml(tr('digitalAssetsContentEmptySources')) + '</div>'
        + renderLoadMoreButtonHtml();
    }
    var rows = items.map(function(src) {
      var path = src.relative_path || src.uri || src.title || src.id;
      var checked = state.contentSelected && state.contentSelected[src.id] ? ' checked' : '';
      var statusBadge = src.status && src.status !== 'ready' && src.status !== 'active'
        ? ' | ' + escapeHtml(src.status) : '';
      var errLine = src.error_message
        ? '<div class="item-meta" style="color:var(--danger);margin-top:2px">' + escapeHtml(src.error_message) + '</div>'
        : '';
      return '<div class="item" data-source-id="' + escapeHtml(src.id) + '" style="margin-bottom:8px;padding:10px 12px;display:flex;gap:10px;align-items:flex-start">'
        + '<input type="checkbox" class="digitalAssetsSourceCheck" data-source-id="' + escapeHtml(src.id) + '"' + checked
        + (state.contentBusy ? ' disabled' : '') + ' style="margin-top:3px">'
        + '<div style="flex:1;min-width:0">'
        + '<div class="item-title" style="font-size:13px">' + escapeHtml(src.title || path) + '</div>'
        + '<div class="item-meta mono" style="word-break:break-all">' + escapeHtml(path) + '</div>'
        + '<div class="item-meta">' + escapeHtml(src.kind || '')
        + (src.batch_id ? ' | batch ' + escapeHtml(src.batch_id) : '')
        + ' | nodes ' + escapeHtml(String(src.node_count || 0))
        + ' | cards ' + escapeHtml(String(src.card_count || 0))
        + statusBadge
        + ' | ' + escapeHtml(src.created_at || '') + '</div>'
        + errLine
        + '</div>'
        + '<button type="button" class="btn-ghost digitalAssetsSourceDeleteBtn" data-source-id="' + escapeHtml(src.id) + '"'
        + ' style="height:30px;padding:0 10px;font-size:12px;flex-shrink:0"'
        + (state.contentBusy ? ' disabled' : '') + '>'
        + escapeHtml(tr('digitalAssetsContentDeleteOne')) + '</button>'
        + '</div>';
    }).join('');
    return rows + renderLoadMoreButtonHtml();
  }

  function renderJobsList(items) {
    if (!items || !items.length) {
      return '<div class="empty">' + escapeHtml(tr('digitalAssetsContentEmptyJobs')) + '</div>';
    }
    return items.map(function(job) {
      var p = job.progress || {};
      var names = p.file_names || [];
      if (!Array.isArray(names)) names = [];
      var namesText = names.length ? names.slice(0, 12).join(', ') + (names.length > 12 ? ' ...' : '') : (p.root_label || p.root_path || '-');
      var summary = '';
      if (p.imported != null || p.total_files != null) {
        summary = 'imported ' + (p.imported || 0) + (p.total_files != null ? ' / ' + p.total_files : '');
        if (p.failed) summary += ', failed ' + p.failed;
        if (p.skipped) summary += ', skipped ' + p.skipped;
      }
      var percent = Math.max(0, Math.min(100, Math.round(Number(p.percent) || 0)));
      var phase = phaseLabel(p.phase || job.status || '');
      var running = !isTerminalStatus(job.status);
      return '<div class="item" style="margin-bottom:8px;padding:10px 12px">'
        + '<div class="item-title" style="font-size:13px">' + escapeHtml(String(job.kind || '')) + ' - ' + escapeHtml(String(job.status || ''))
        + (phase ? ' | ' + escapeHtml(phase) : '') + '</div>'
        + '<div class="item-meta">' + escapeHtml(tr('digitalAssetsJobFiles')) + ': ' + escapeHtml(namesText) + '</div>'
        + (summary ? '<div class="item-meta">' + escapeHtml(summary) + '</div>' : '')
        + ((running || percent > 0) ? '<div class="item-meta" style="margin-top:6px">' + escapeHtml(tr('digitalAssetsJobProgress')) + ': ' + percent + '%'
          + '<div style="margin-top:4px;height:6px;border-radius:999px;background:rgba(31,34,48,.08);overflow:hidden">'
          + '<div style="height:100%;width:' + percent + '%;background:' + (job.status === 'failed' ? '#ef4444' : '#3b82f6') + '"></div>'
          + '</div></div>' : '')
        + (job.error ? '<div class="item-meta" style="color:var(--danger)">' + escapeHtml(job.error) + '</div>' : '')
        + '<div class="item-meta">' + escapeHtml(tr('digitalAssetsJobTime')) + ': ' + escapeHtml(job.created_at || job.updated_at || '') + '</div>'
        + '</div>';
    }).join('');
  }

  function stopContentJobsPoll() {
    if (state.contentJobsPollTimer) {
      global.clearTimeout(state.contentJobsPollTimer);
      state.contentJobsPollTimer = null;
    }
  }

  function hasRunningContentJobs() {
    var jobs = state.contentJobs || [];
    for (var i = 0; i < jobs.length; i++) {
      if (!isTerminalStatus(jobs[i] && jobs[i].status)) return true;
    }
    return false;
  }

  function scheduleContentJobsPoll() {
    stopContentJobsPoll();
    if (!state.contentOpen || !state.contentLibraryId) return;
    if (state.contentTab !== 'jobs') return;
    if (!hasRunningContentJobs()) return;
    var delay = state.contentJobsPollDelayMs || 1500;
    if (delay < 1500) delay = 1500;
    if (delay > 15000) delay = 15000;
    // Defer network while progress modal / delete / full reload is in flight,
    // but always keep a timer so polling resumes (do not early-return without one).
    if (state.progress || state.contentBusy || state.contentLoading) {
      delay = Math.max(delay, 2000);
    }
    state.contentJobsPollTimer = global.setTimeout(function() {
      state.contentJobsPollTimer = null;
      if (!state.contentOpen || state.contentTab !== 'jobs' || !state.contentLibraryId) return;
      if (state.progress || state.contentBusy || state.contentLoading) {
        scheduleContentJobsPoll();
        return;
      }
      if (!hasRunningContentJobs()) return;
      refreshContentJobsOnly();
    }, delay);
  }

  async function refreshContentJobsOnly() {
    if (!state.contentLibraryId || !state.contentOpen) return;
    var libId = state.contentLibraryId;
    // Independent of contentLoadSeq so source search/load-more does not drop jobs updates.
    var pollToken = (state.contentJobsPollToken = (state.contentJobsPollToken || 0) + 1);
    try {
      var data = await api('/api/admin/digital-assets/libraries/' + encodeURIComponent(libId) + '/import-jobs?limit=50');
      if (pollToken !== state.contentJobsPollToken) return;
      if (!state.contentOpen || state.contentLibraryId !== libId) return;
      var prevRunning = hasRunningContentJobs();
      var prevSig = jobsStatusSignature(state.contentJobs);
      state.contentJobs = (data && data.items) || [];
      var nowRunning = hasRunningContentJobs();
      var nowSig = jobsStatusSignature(state.contentJobs);
      if (state.contentTab === 'jobs') {
        var scroll = byID('digitalAssetsContentScroll');
        if (scroll) {
          captureContentScroll();
          scroll.innerHTML = renderJobsList(state.contentJobs);
          restoreContentScroll();
        }
        updateSelectionChrome();
      } else {
        updateSelectionChrome();
      }
      // Back off while status is unchanged; snap back when something moves.
      if (nowRunning && prevSig === nowSig) {
        state.contentJobsPollDelayMs = Math.min(15000, Math.round((state.contentJobsPollDelayMs || 1500) * 1.5));
      } else {
        state.contentJobsPollDelayMs = 1500;
      }
      // When jobs flip terminal, refresh sources count/list once (import may have finished).
      if (prevRunning && !nowRunning && state.contentTab === 'jobs') {
        loadContentDialogData(libId, {
          keepTab: true, keepSelection: true, keepQuery: true, softFail: true, quiet: true, sourcesOnly: true
        });
      }
    } catch (_) {
      // Transient errors: mild backoff, next open/refresh will surface hard failures.
      state.contentJobsPollDelayMs = Math.min(15000, Math.round((state.contentJobsPollDelayMs || 1500) * 1.25));
    } finally {
      if (pollToken === state.contentJobsPollToken) scheduleContentJobsPoll();
    }
  }

  function jobsStatusSignature(jobs) {
    // Prefer phase/percent over updated_at so pure timestamp churn does not defeat backoff.
    return (jobs || []).map(function(j) {
      var p = (j && j.progress) || {};
      return String((j && j.id) || '') + ':' + String((j && j.status) || '')
        + ':' + String(p.phase || '') + ':' + String(p.percent != null ? p.percent : '');
    }).join('|');
  }

  function closeContentDialog() {
    if (state.contentSearchTimer) {
      global.clearTimeout(state.contentSearchTimer);
      state.contentSearchTimer = null;
    }
    stopContentJobsPoll();
    state.contentJobsPollToken = (state.contentJobsPollToken || 0) + 1;
    state.contentOpen = false;
    state.contentBusy = false;
    state.contentLoading = false;
    state.contentComposing = false;
    state.contentSelected = {};
    state.contentQuery = '';
    state.contentServerQuery = '';
    state.contentLibraryId = '';
    state.contentScrollTop = 0;
    state.contentHasMore = false;
    state.contentSourcesOffset = 0;
    state.contentLoadingMore = false;
    state.contentAutoFillRounds = 0;
    state.contentAutoFillCappedNotified = false;
    state.contentJobsPollDelayMs = 1500;
    renderOverlays({ skipScrollCapture: true });
  }

  function scheduleServerSourceSearch() {
    if (state.contentSearchTimer) global.clearTimeout(state.contentSearchTimer);
    state.contentSearchTimer = global.setTimeout(function() {
      state.contentSearchTimer = null;
      if (!state.contentLibraryId || !state.contentOpen) return;
      if (state.contentComposing) {
        // compositionend will schedule the server search after IME commits.
        return;
      }
      var q = String(state.contentQuery || '').trim();
      if (q === String(state.contentServerQuery || '').trim()) {
        // Server already has this query; local filter already applied via list body.
        return;
      }
      loadContentDialogData(state.contentLibraryId, {
        keepTab: true,
        keepSelection: true,
        keepQuery: true,
        softFail: true,
        quiet: true,
        sourcesOnly: true
      });
    }, 280);
  }

  function wireOverlayHandlers(opts) {
    opts = opts || {};
    var closeBtn = byID('digitalAssetsContentCloseBtn');
    if (closeBtn) closeBtn.addEventListener('click', function() { closeContentDialog(); });
    var refreshBtn = byID('digitalAssetsContentRefreshBtn');
    if (refreshBtn) refreshBtn.addEventListener('click', function() {
      if (state.contentLibraryId) {
        loadContentDialogData(state.contentLibraryId, {
          keepTab: true, keepSelection: true, keepQuery: true, softFail: true
        });
      }
    });
    var backdrop = byID('digitalAssetsContentBackdrop');
    if (backdrop) backdrop.addEventListener('click', function(ev) {
      if (ev.target === backdrop && !state.contentBusy) closeContentDialog();
    });

    var tabSources = byID('digitalAssetsContentTabSources');
    if (tabSources) tabSources.addEventListener('click', function() {
      state.contentTab = 'sources';
      stopContentJobsPoll();
      renderOverlays();
    });
    var tabJobs = byID('digitalAssetsContentTabJobs');
    if (tabJobs) tabJobs.addEventListener('click', function() {
      state.contentTab = 'jobs';
      state.contentJobsPollDelayMs = 1500;
      renderOverlays();
      scheduleContentJobsPoll();
    });

    var search = byID('digitalAssetsContentSearch');
    if (search) {
      search.addEventListener('compositionstart', function() {
        state.contentComposing = true;
      });
      search.addEventListener('compositionend', function() {
        state.contentComposing = false;
        state.contentQuery = search.value || '';
        // Commit IME text: local list + server search without replacing the input node mid-composition.
        renderSourcesListBody();
        scheduleServerSourceSearch();
      });
      search.addEventListener('input', function() {
        state.contentQuery = search.value || '';
        // Never full-rebuild the dialog while typing - destroys CJK IME composition.
        if (!state.contentComposing) {
          renderSourcesListBody();
          scheduleServerSourceSearch();
        }
      });
      search.addEventListener('keydown', function(ev) {
        if (ev.key === 'Escape') {
          ev.stopPropagation();
          if (state.contentQuery) {
            state.contentQuery = '';
            state.contentServerQuery = '';
            state.contentComposing = false;
            if (state.contentSearchTimer) {
              global.clearTimeout(state.contentSearchTimer);
              state.contentSearchTimer = null;
            }
            loadContentDialogData(state.contentLibraryId, {
              keepTab: true, keepSelection: true, keepQuery: true, softFail: true, quiet: true, sourcesOnly: true
            });
          }
        } else if (ev.key === 'Enter') {
          ev.preventDefault();
          if (state.contentComposing) return;
          if (state.contentSearchTimer) {
            global.clearTimeout(state.contentSearchTimer);
            state.contentSearchTimer = null;
          }
          loadContentDialogData(state.contentLibraryId, {
            keepTab: true, keepSelection: true, keepQuery: true, softFail: true, quiet: true, sourcesOnly: true
          });
        }
      });
      if (opts.focusSearch) {
        try { search.focus(); } catch (_) {}
      }
    }

    var selectAll = byID('digitalAssetsContentSelectAllBtn');
    if (selectAll) selectAll.addEventListener('click', function() {
      filteredSources(state.contentSources).forEach(function(src) {
        if (src && src.id) state.contentSelected[src.id] = true;
      });
      // Sync checkboxes without full rebuild when possible.
      var root = byID('digitalAssetsOverlayRoot');
      if (root) {
        root.querySelectorAll('.digitalAssetsSourceCheck').forEach(function(el) {
          el.checked = true;
        });
      }
      updateSelectionChrome();
    });
    var clearSel = byID('digitalAssetsContentClearSelectBtn');
    if (clearSel) clearSel.addEventListener('click', function() {
      state.contentSelected = {};
      var root = byID('digitalAssetsOverlayRoot');
      if (root) {
        root.querySelectorAll('.digitalAssetsSourceCheck').forEach(function(el) {
          el.checked = false;
        });
      }
      updateSelectionChrome();
    });
    var delSel = byID('digitalAssetsContentDeleteSelectedBtn');
    if (delSel) delSel.addEventListener('click', function() {
      deleteContentSources(selectedSourceIds());
    });

    // Event delegation for per-row checkbox / delete / load-more (once per overlay root).
    var root = byID('digitalAssetsOverlayRoot');
    if (root && !root._daContentDelegated) {
      root._daContentDelegated = true;
      root.addEventListener('change', function(ev) {
        var t = ev.target;
        if (!t || !t.classList || !t.classList.contains('digitalAssetsSourceCheck')) return;
        var id = t.getAttribute('data-source-id') || '';
        if (!id) return;
        if (t.checked) state.contentSelected[id] = true;
        else delete state.contentSelected[id];
        updateSelectionChrome();
      });
      root.addEventListener('click', function(ev) {
        var t = ev.target;
        if (!t || !t.closest) return;
        var loadMoreBtn = t.closest('#digitalAssetsContentLoadMoreBtn');
        if (loadMoreBtn && !loadMoreBtn.disabled) {
          loadMoreContentSources();
          return;
        }
        var btn = t.closest('.digitalAssetsSourceDeleteBtn');
        if (!btn || btn.disabled) return;
        var id = btn.getAttribute('data-source-id') || '';
        if (id) deleteContentSources([id]);
      });
    }
  }

  // Escape closes content dialog when progress overlay is not active.
  // Prefer not closing content while an AdminUI dialog is open (z-index above us).
  if (typeof global.document !== 'undefined' && !global.document._daContentEscBound) {
    global.document._daContentEscBound = true;
    global.document.addEventListener('keydown', function(ev) {
      if (ev.key !== 'Escape') return;
      if (state.progress) return;
      if (!state.contentOpen) return;
      // AdminUI dialogs use capture+stopPropagation; also hard-check open session/DOM.
      if (global.AdminUI && typeof global.AdminUI.isDialogOpen === 'function'
          && global.AdminUI.isDialogOpen()) return;
      if (global.document.querySelector('.admin-ui-dialog-overlay')) return;
      // Let search box clear first when focused with text.
      var search = byID('digitalAssetsContentSearch');
      if (search && global.document.activeElement === search && state.contentQuery) return;
      if (state.contentBusy) return;
      closeContentDialog();
    });
  }

  async function loadContentDialogData(libraryId, opts) {
    opts = opts || {};
    libraryId = String(libraryId || '').trim();
    if (!libraryId) return;
    var seq = ++state.contentLoadSeq;
    state.contentLibraryId = libraryId;
    var append = !!opts.append;
    if (!opts.quiet && !append) state.contentLoading = true;
    if (append) {
      state.contentLoadingMore = true;
    } else {
      // A non-append reload supersedes any in-flight "load more".
      state.contentLoadingMore = false;
    }
    if (!opts.keepSelection && !append) state.contentSelected = {};
    if (!opts.keepQuery && !append) {
      state.contentQuery = '';
      state.contentServerQuery = '';
    }
    if (!append) {
      state.contentSourcesOffset = 0;
      state.contentAutoFillRounds = 0;
      state.contentAutoFillCappedNotified = false;
    }
    if (!opts.quiet) renderOverlays();
    else updateSelectionChrome();
    var loadOk = false;
    try {
      var limit = state.contentSourcesLimit || 100;
      var offset = append ? (state.contentSourcesOffset || 0) : 0;
      var base = '/api/admin/digital-assets/libraries/' + encodeURIComponent(libraryId);
      var q = (opts.keepQuery || append) ? String(state.contentQuery || '').trim() : '';
      var sourcesURL = base + '/sources?limit=' + encodeURIComponent(String(limit))
        + '&offset=' + encodeURIComponent(String(offset));
      if (q) sourcesURL += '&q=' + encodeURIComponent(q);
      var fetches = [api(sourcesURL)];
      if (!opts.sourcesOnly && !append) fetches.push(api(base + '/import-jobs?limit=50'));
      var results = await Promise.all(fetches);
      if (seq !== state.contentLoadSeq) return; // stale
      var pageItems = (results[0] && results[0].items) || [];
      var respOffset = (results[0] && results[0].offset != null) ? Number(results[0].offset) : offset;
      if (isNaN(respOffset) || respOffset < 0) respOffset = offset;
      // Advance cursor by the server page window (not local array length) so a
      // full-overlap / empty page cannot re-request the same offset forever.
      var nextOffset = respOffset + pageItems.length;
      var hasMore = !!(results[0] && results[0].has_more);
      if (pageItems.length === 0) hasMore = false;

      if (append) {
        var seen = {};
        (state.contentSources || []).forEach(function(s) { if (s && s.id) seen[s.id] = true; });
        pageItems.forEach(function(s) {
          if (s && s.id && !seen[s.id]) {
            state.contentSources.push(s);
            seen[s.id] = true;
          }
        });
      } else {
        state.contentSources = pageItems;
      }
      state.contentSourcesOffset = nextOffset;
      state.contentHasMore = hasMore;
      state.contentSourcesTotal = (results[0] && results[0].total != null)
        ? Number(results[0].total)
        : state.contentSources.length;
      if (isNaN(state.contentSourcesTotal) || state.contentSourcesTotal < state.contentSources.length) {
        state.contentSourcesTotal = state.contentSources.length;
      }
      state.contentServerQuery = q;
      if (!opts.sourcesOnly && !append) {
        state.contentJobs = (results[1] && results[1].items) || [];
      }
      // Drop selections that no longer exist after a full reload (not on append/filter keep).
      if (!q && !append) {
        var alive = {};
        state.contentSources.forEach(function(s) { if (s && s.id) alive[s.id] = true; });
        Object.keys(state.contentSelected).forEach(function(id) {
          if (!alive[id]) delete state.contentSelected[id];
        });
      }
      loadOk = true;
    } catch (err) {
      if (seq !== state.contentLoadSeq) return;
      showToast(tr('digitalAssetsContentLoadFailed', { error: err.message || err }), 'error');
      if (!opts.softFail && !append) state.contentOpen = false;
    } finally {
      if (seq === state.contentLoadSeq) {
        state.contentLoading = false;
        state.contentLoadingMore = false;
        // Auto-fill rounds: count every finished auto-fill attempt (ok or fail) so
        // persistent errors cannot tight-loop; only chain further after success.
        if (append && opts.fromAutoFill) {
          state.contentAutoFillRounds = (state.contentAutoFillRounds || 0) + 1;
        }
        if (loadOk) {
          if ((opts.quiet || append) && (opts.sourcesOnly || append) && state.contentTab === 'sources' && byID('digitalAssetsContentScroll')) {
            renderSourcesListBody({ stickBottom: !!append });
          } else {
            renderOverlays({ focusSearch: !!opts.keepQuery && !!String(state.contentQuery || '').trim() && !state.contentComposing });
            if (state.contentOpen && state.contentTab === 'sources') {
              wireContentScrollLoadMore();
              maybeAutoFillSources();
            }
          }
        } else {
          // Release Load more disabled state without re-entering auto-fill.
          updateSelectionChrome();
        }
        scheduleContentJobsPoll();
      }
    }
  }

  function loadMoreContentSources(opts) {
    opts = opts || {};
    if (!state.contentLibraryId || !state.contentHasMore || state.contentLoadingMore || state.contentLoading || state.contentBusy) return;
    // Synchronous gate so rapid scroll events / auto-fill cannot double-start.
    state.contentLoadingMore = true;
    updateSelectionChrome();
    loadContentDialogData(state.contentLibraryId, {
      append: true,
      keepTab: true,
      keepSelection: true,
      keepQuery: true,
      softFail: true,
      quiet: true,
      sourcesOnly: true,
      fromAutoFill: !!opts.fromAutoFill
    });
  }

  async function openContentDialog(libraryId) {
    state.contentOpen = true;
    state.contentTab = 'sources';
    state.contentQuery = '';
    state.contentServerQuery = '';
    state.contentSelected = {};
    state.contentSources = [];
    state.contentJobs = [];
    state.contentSourcesTotal = 0;
    state.contentSourcesOffset = 0;
    state.contentHasMore = false;
    state.contentLoadingMore = false;
    state.contentScrollTop = 0;
    state.contentAutoFillRounds = 0;
    state.contentAutoFillCappedNotified = false;
    state.contentJobsPollDelayMs = 1500;
    await loadContentDialogData(libraryId, {});
  }

  async function deleteContentSources(ids) {
    ids = (ids || []).filter(Boolean);
    if (!ids.length || !state.contentLibraryId || state.contentBusy || state.contentDeleteGuard) return;
    if (isAdminDialogOpen()) return;
    // Guard covers confirm await so double-click cannot open two confirms / fire two deletes.
    state.contentDeleteGuard = true;
    try {
      var okDel = await showConfirm(tr('digitalAssetsContentDeleteConfirm', { n: String(ids.length) }), {
        title: tr('digitalAssetsDeleteSourcesTitle'),
        danger: true,
        confirmText: tr('digitalAssetsContentDeleteSelected')
      });
      if (!okDel) return;
      state.contentBusy = true;
      renderOverlays();
      try {
        var base = '/api/admin/digital-assets/libraries/' + encodeURIComponent(state.contentLibraryId);
        var res;
        if (ids.length === 1) {
          res = await api(base + '/sources/' + encodeURIComponent(ids[0]), { method: 'DELETE' });
        } else {
          res = await api(base + '/sources/delete', {
            method: 'POST',
            body: JSON.stringify({ source_ids: ids })
          });
        }
        var n = (res && res.deleted != null) ? res.deleted : ids.length;
        var missingN = (res && res.missing && res.missing.length) ? res.missing.length : 0;
        var msg = tr('digitalAssetsContentDeleteDone', { n: String(n) });
        if (missingN) msg += ' (' + missingN + ' missing)';
        if (res && res.error) msg += ' | ' + String(res.error);
        showToast(msg, (n > 0 && !(res && res.error)) ? 'success' : 'error');
        ids.forEach(function(id) { delete state.contentSelected[id]; });
        // Refresh dialog + library list (source_count / content_rev).
        await Promise.all([
          loadContentDialogData(state.contentLibraryId, {
            keepTab: true, keepSelection: true, keepQuery: true, softFail: true, quiet: true
          }),
          global.loadDigitalAssetLibraries()
        ]);
      } catch (err) {
        showToast(tr('digitalAssetsContentDeleteFailed', { error: err.message || err }), 'error');
      } finally {
        state.contentBusy = false;
        renderOverlays();
        // Reload may have tried to schedule poll while contentBusy was true; resume now.
        scheduleContentJobsPoll();
      }
    } finally {
      state.contentDeleteGuard = false;
    }
  }

  async function trackJob(job, token) {
    var id = jobIdOf(job);
    if (!id) return job;
    if (token == null) token = state.progressToken;

    function applyProgress(view) {
      var p = (view && view.progress) || {};
      var status = (view && view.status) || '';
      var pct = p.percent;
      if (pct == null) {
        pct = isTerminalStatus(status) && status !== 'failed' ? 100 : 15;
      }
      setProgress({
        phase: p.phase || status || 'importing',
        percent: pct,
        current_file: p.current_file || p.last_item_path || '',
        current_step: p.current_step || '',
        current_step_num: p.current_step_num || 0,
        total_steps: p.total_steps || 0,
        processed: p.processed || 0,
        total: p.total_files || p.total || 0,
        imported: p.imported || 0,
        message: p.message || ''
      }, token);
    }

    function finishSuccess(view) {
      clearProgress(token);
      showToast(tr('digitalAssetsProgressDone'), 'success');
      // Keep open content dialog in sync (sources / jobs / source_count).
      var libId = (view && view.library_id) || state.selectedId || state.contentLibraryId || '';
      maybeRefreshOpenContent(libId);
      return view || job;
    }
    function finishFailed(errMsg) {
      clearProgress(token);
      showToast(tr('digitalAssetsProgressFailed', { error: errMsg || 'failed' }), 'error');
      return job;
    }

    applyProgress(job);
    if (job.status === 'succeeded' || job.status === 'done') {
      return finishSuccess(job);
    }
    if (job.status === 'failed') {
      return finishFailed(job.error);
    }

    // Adaptive poll: 400ms to 2s, cap wall-clock ~30 min for large imports.
    var tries = 0;
    var delay = 400;
    var maxTries = 1200;
    var consecutiveErrors = 0;
    while (tries < maxTries) {
      if (token !== state.progressToken) return job; // superseded
      tries += 1;
      await sleep(delay);
      if (delay < 2000) delay = Math.min(2000, delay + 80);
      var view;
      try {
        view = await api('/api/admin/digital-assets/import/jobs/' + encodeURIComponent(id));
        consecutiveErrors = 0;
      } catch (err) {
        consecutiveErrors += 1;
        if (consecutiveErrors > 8) {
          return finishFailed(err.message || err);
        }
        continue;
      }
      if (!view) continue;
      // Normalize job_id-only payloads.
      if (!view.id && view.job_id) view.id = view.job_id;
      applyProgress(view);
      if (view.status === 'succeeded' || view.status === 'done') {
        return finishSuccess(view);
      }
      if (view.status === 'failed') {
        return finishFailed(view.error);
      }
    }
    clearProgress(token);
    showToast(tr('digitalAssetsProgressTimeout'), 'error');
    return job;
  }

  async function runImport(path, body, isForm) {
    var token = beginProgress({
      phase: isForm ? 'uploading' : 'queued',
      percent: isForm ? 5 : 3,
      message: isForm ? tr('digitalAssetsProgressUploading') : tr('digitalAssetsProgressProcessing')
    });
    try {
      var res = await api(path, { method: 'POST', body: body });
      if (res && !res.id && res.job_id) res.id = res.job_id;
      var done = await trackJob(res, token);
      if (token === state.progressToken) {
        await global.loadDigitalAssetLibraries();
        // trackJob already refreshes open content; libraries list is enough here.
      }
      return done;
    } catch (err) {
      clearProgress(token);
      showToast(String(err.message || err), 'error');
      throw err;
    }
  }

  function renderList() {
    const box = byID('digitalAssetsList');
    if (!box) return;
    if (!state.items.length) {
      box.innerHTML = '<div class="empty">' + escapeHtml(tr('digitalAssetsEmpty')) + '</div>';
      return;
    }
    box.innerHTML = state.items.map(function(item) {
      const active = item.id === state.selectedId ? ' is-selected' : '';
      return '<button type="button" class="digital-assets-library-card' + active + '" data-id="' + escapeHtml(item.id) + '"'
        + (item.id === state.selectedId ? ' aria-current="true"' : '') + '>'
        + '<strong>' + escapeHtml(item.name || item.id) + '</strong>'
        + '<small>' + escapeHtml((item.library_kind === 'technical' ? tr('digitalAssetsKindBadgeTechnical') : tr('digitalAssetsKindBadgeBusiness')) + ' · ' + tr('digitalAssetsCardMeta', {
          rev: String(item.content_rev || 0),
          src: String(item.source_count || 0),
          sync: item.sync_enabled ? tr('digitalAssetsSyncOn') : tr('digitalAssetsSyncOff')
        })) + '</small>'
        + '<small>' + escapeHtml(aclSummaryText(item)) + '</small>'
        + '</button>';
    }).join('');
    box.querySelectorAll('.digital-assets-library-card').forEach(function(el) {
      el.addEventListener('click', function() {
        var id = el.getAttribute('data-id') || '';
        var reselect = id && id === state.selectedId;
        if (id !== state.selectedId) {
          // Switching library: discard unsaved ACL draft for the previous one.
          clearAclDraft();
          state.mergeOpen = false;
        }
        state.selectedId = id;
        renderList();
        loadLibrarySubmissions(id).then(function() { renderDetail(); });
        // Second click on the same library opens contents (first click only selects).
        if (reselect && id) openContentDialog(id);
      });
    });
  }

  function renderSubmissionQueue(item) {
    var rows = (state.submissions || []).filter(function(s) {
      return s && s.library_id === item.id && (s.status === 'submitted' || s.status === 'import_failed');
    });
    var body = '';
    if (state.submissionsLoading) {
      body = '<div class="item-meta">' + escapeHtml(tr('digitalAssetsAclDepartmentsLoading')) + '</div>';
    } else if (!rows.length) {
      body = '<div class="item-meta">' + escapeHtml(tr('digitalAssetsQueueEmpty')) + '</div>';
    } else {
      body = rows.map(function(s) {
        var titles = (s.preview_titles || []).slice(0, 4).join(' / ');
        return '<div class="item" style="margin-top:8px;padding:10px 12px">'
          + '<div class="item-title">' + escapeHtml(s.title || s.id) + '</div>'
          + '<div class="item-meta">' + escapeHtml(tr('digitalAssetsQueueMeta', {
            kind: s.kind || '',
            count: String(s.item_count || 0),
            email: s.submitter_email || ''
          })) + '</div>'
          + (s.summary ? '<div class="item-meta" style="margin-top:4px">' + escapeHtml(s.summary) + '</div>' : '')
          + (titles ? '<div class="item-meta">' + escapeHtml(titles) + '</div>' : '')
          + '<div class="actions" style="margin-top:8px">'
          + actionButton('digitalAssetsApprove-' + s.id, tr('digitalAssetsQueueApprove'), 'primary')
          + actionButton('digitalAssetsReject-' + s.id, tr('digitalAssetsQueueReject'), 'danger')
          + '</div></div>';
      }).join('');
    }
    return sectionTitle(tr('digitalAssetsQueueSection')) + body;
  }

  async function loadLibrarySubmissions(libraryId) {
    if (!libraryId) {
      state.submissions = [];
      return;
    }
    state.submissionsLoading = true;
    try {
      var data = await api('/api/admin/digital-assets/submissions?library_id=' + encodeURIComponent(libraryId) + '&limit=50');
      state.submissions = (data && data.items) || [];
    } catch (err) {
      state.submissions = [];
      showToast(tr('digitalAssetsQueueLoadFailed', { error: String(err && err.message || err) }), 'error');
    } finally {
      state.submissionsLoading = false;
    }
  }

  async function approveSubmission(id) {
    try {
      await api('/api/admin/digital-assets/submissions/' + encodeURIComponent(id) + '/approve', { method: 'POST', body: '{}' });
      showToast(tr('digitalAssetsQueueApproved'), 'success');
      await global.loadDigitalAssetLibraries();
    } catch (err) {
      showToast(String(err && err.message || err), 'error');
    }
  }

  async function rejectSubmission(id) {
    if (isAdminDialogOpen()) return;
    var note = await showPrompt(tr('digitalAssetsQueueRejectPrompt'), {
      title: tr('digitalAssetsQueueReject'),
      required: true
    });
    if (!note) return;
    try {
      await api('/api/admin/digital-assets/submissions/' + encodeURIComponent(id) + '/reject', {
        method: 'POST',
        body: JSON.stringify({ review_note: note })
      });
      showToast(tr('digitalAssetsQueueRejected'), 'success');
      await loadLibrarySubmissions(state.selectedId);
      renderDetail({ skipGroups: true, skipCapture: true });
    } catch (err) {
      showToast(String(err && err.message || err), 'error');
    }
  }

  function renderMergePanel(item) {
    const sources = state.items.filter(function(x) { return x && x.id && x.id !== item.id; });
    if (!sources.length) {
      return '<div class="item" style="margin-top:10px;padding:12px 14px">'
        + '<div class="item-meta">' + escapeHtml(tr('digitalAssetsMergeEmpty')) + '</div>'
        + '<div class="actions" style="margin-top:10px">'
        + actionButton('digitalAssetsMergeCancelBtn', tr('digitalAssetsMergeCancel'), 'ghost')
        + '</div></div>';
    }
    const rows = sources.map(function(src) {
      return '<label style="display:flex;align-items:flex-start;gap:10px;padding:8px 0;border-bottom:1px solid rgba(31,34,48,.06);cursor:pointer;margin:0">'
        + '<input type="checkbox" class="digital-assets-merge-src" value="' + escapeHtml(src.id) + '" style="margin-top:3px">'
        + '<span style="min-width:0"><span class="item-title" style="font-size:13px">' + escapeHtml(src.name || src.id) + '</span>'
        + '<div class="item-meta">' + escapeHtml(tr('digitalAssetsRevLabel', { rev: String(src.content_rev || 0) })) + '</div></span></label>';
    }).join('');
    return '<div class="item" style="margin-top:10px;padding:12px 14px">'
      + '<div class="item-title" style="font-size:14px">' + escapeHtml(tr('digitalAssetsMergeTitle', { name: item.name || item.id })) + '</div>'
      + '<div class="item-meta" style="margin:6px 0 10px">' + escapeHtml(tr('digitalAssetsMergeHint')) + '</div>'
      + '<div style="max-height:240px;overflow:auto">' + rows + '</div>'
      + '<label style="display:inline-flex;align-items:center;gap:8px;margin:12px 0 0;cursor:pointer;font-size:12px;font-weight:600">'
      + '<input type="checkbox" id="digitalAssetsMergeArchive" checked> '
      + escapeHtml(tr('digitalAssetsMergeArchive')) + '</label>'
      + '<div class="actions" style="margin-top:12px;display:flex;gap:8px;flex-wrap:wrap">'
      + actionButton('digitalAssetsMergeConfirmBtn', tr('digitalAssetsMergeConfirm'), 'primary')
      + actionButton('digitalAssetsMergeCancelBtn', tr('digitalAssetsMergeCancel'), 'ghost')
      + '</div></div>';
  }

  function renderDetail(opts) {
    opts = opts || {};
    const detail = byID('digitalAssetsDetail');
    if (!detail) return;
    // Capture unsaved ACL edits before wiping the panel (merge open, group reload, etc.).
    if (!opts.skipCapture) captureAclDraftFromDom();
    const item = state.items.find(function(x) { return x.id === state.selectedId; });
    if (!item) {
      detail.innerHTML = '<div class="empty">' + escapeHtml(tr('digitalAssetsSelectHint')) + '</div>';
      return;
    }
    // Paint ACL form from draft when present so re-renders keep in-progress edits.
    const viewItem = itemWithAclDraft(item);

    detail.innerHTML =
      '<div class="item" style="padding:14px 16px">'
      + '<div style="display:flex;justify-content:space-between;gap:12px;align-items:flex-start;flex-wrap:wrap">'
      + '<div><div class="item-title">' + escapeHtml(item.name) + '</div>'
      + '<div class="item-meta mono" style="margin-top:4px">' + escapeHtml(item.id) + '</div>'
      + '<div class="item-meta" style="margin-top:4px">' + escapeHtml(aclSummaryText(viewItem)) + '</div></div>'
      + actionButton('digitalAssetsViewContentBtn', tr('digitalAssetsViewContent'), 'primary')
      + '</div>'
      + (item.description ? '<p style="margin:10px 0 0">' + escapeHtml(item.description) + '</p>' : '')
      + '<div style="margin-top:12px;display:flex;gap:12px;flex-wrap:wrap;align-items:center">'
      + '<label>' + escapeHtml(tr('digitalAssetsKind')) + ' <select id="digitalAssetsLibraryKind" class="input">'
      + '<option value="business"' + (item.library_kind !== 'technical' ? ' selected' : '') + '>' + escapeHtml(tr('digitalAssetsKindBusiness')) + '</option>'
      + '<option value="technical"' + (item.library_kind === 'technical' ? ' selected' : '') + '>' + escapeHtml(tr('digitalAssetsKindTechnical')) + '</option>'
      + '</select></label>'
      + '<label><input type="checkbox" id="digitalAssetsAcceptsSubmissions"' + (item.accepts_submissions !== false ? ' checked' : '') + '> ' + escapeHtml(tr('digitalAssetsAcceptsSubmissions')) + '</label>'
      + '</div>'
      + '<div class="item-meta" style="margin-top:6px">' + escapeHtml(tr('digitalAssetsAcceptsHint')) + '</div>'
      + renderSubmissionQueue(item)
      + renderAclPanel(viewItem)
      + sectionTitle(tr('digitalAssetsImportSection'))
      + '<div class="actions" style="display:flex;gap:8px;flex-wrap:wrap;margin:0">'
      + fileButton('digitalAssetsFileInput', tr('digitalAssetsUpload'), 'multiple')
      + fileButton('digitalAssetsZipInput', tr('digitalAssetsArchive'), 'accept=".zip"')
      + fileButton('digitalAssetsFolderInput', tr('digitalAssetsBrowserDir'), 'multiple webkitdirectory directory')
      + actionButton('digitalAssetsServerDirBtn', tr('digitalAssetsServerDir'))
      + actionButton('digitalAssetsShareBtn', tr('digitalAssetsShareImport'))
      + '</div>'
      + '<div class="item-meta" style="margin-top:8px">' + escapeHtml(tr('digitalAssetsServerDirHint')) + '</div>'
      + sectionTitle(tr('digitalAssetsManageSection'))
      + '<div class="actions" style="display:flex;gap:8px;flex-wrap:wrap;margin:0">'
      + actionButton('digitalAssetsMergeBtn', tr('digitalAssetsMerge'))
      + actionButton('digitalAssetsExportBtn', tr('digitalAssetsExport'))
      + fileButton('digitalAssetsBackupInput', tr('digitalAssetsImportBackup'), 'accept=".zip"')
      + actionButton('digitalAssetsSearchBtn', tr('digitalAssetsSearch'))
      + actionButton('digitalAssetsDeleteBtn', tr('digitalAssetsDelete'), 'danger')
      + '</div>'
      + (state.mergeOpen ? renderMergePanel(item) : '')
      + '<div id="digitalAssetsHits" style="margin-top:12px"></div>'
      + '</div>';

    wireDetailHandlers(item);
    wireSubmissionHandlers();
    applyDeptFilter();
    // Lazy-load department tree for ACL multi-select (first open / refresh).
    if (!opts.skipGroups && !state.securityGroupsLoaded && !state.securityGroupsLoading) {
      loadSecurityGroups({ renderDetail: true });
    }
  }

  function wireDetailHandlers(item) {
    const viewBtn = byID('digitalAssetsViewContentBtn');
    if (viewBtn) viewBtn.addEventListener('click', function() { openContentDialog(item.id); });
    const aclModeAll = byID('digitalAssetsAclModeAll');
    const aclModeRestricted = byID('digitalAssetsAclModeRestricted');
    if (aclModeAll) aclModeAll.addEventListener('change', function() {
      updateAclRestrictedVisibility();
      captureAclDraftFromDom();
    });
    if (aclModeRestricted) aclModeRestricted.addEventListener('change', function() {
      updateAclRestrictedVisibility();
      captureAclDraftFromDom();
    });
    detailQueryAll('.digital-assets-acl-dept').forEach(function(cb) {
      cb.addEventListener('change', function() { aclRestrictionChanged(cb); });
    });
    const deptFilter = byID('digitalAssetsAclDeptFilter');
    if (deptFilter) {
      deptFilter.addEventListener('input', applyDeptFilter);
    }
    const clearDepartments = byID('digitalAssetsAclClearDepartmentsBtn');
    if (clearDepartments) clearDepartments.addEventListener('click', function() {
      detailQueryAll('.digital-assets-acl-dept').forEach(function(cb) { cb.checked = false; });
      aclRestrictionChanged();
    });
    const reloadGroups = byID('digitalAssetsAclReloadGroupsBtn');
    if (reloadGroups) {
      reloadGroups.addEventListener('click', function() {
        captureAclDraftFromDom();
        loadSecurityGroups({ force: true, renderDetail: true });
      });
    }
    const aclSave = byID('digitalAssetsAclSaveBtn');
    if (aclSave) {
      aclSave.addEventListener('click', function() { saveLibraryAcl(item); });
    }
    // Apply initial validation too: an empty restricted ACL must never look saveable.
    updateAclEmptyWarn();
    const fileInput = byID('digitalAssetsFileInput');
    if (fileInput) fileInput.addEventListener('change', function() {
      if (!fileInput.files || !fileInput.files.length) return;
      uploadFiles(item.id, fileInput.files).finally(function() { fileInput.value = ''; });
    });
    const zipInput = byID('digitalAssetsZipInput');
    if (zipInput) zipInput.addEventListener('change', function() {
      if (!zipInput.files || !zipInput.files.length) return;
      uploadZip(item.id, zipInput.files[0]).finally(function() { zipInput.value = ''; });
    });
    const folderInput = byID('digitalAssetsFolderInput');
    if (folderInput) folderInput.addEventListener('change', function() {
      if (!folderInput.files || !folderInput.files.length) return;
      uploadFolder(item.id, folderInput.files).finally(function() { folderInput.value = ''; });
    });
    const serverDirBtn = byID('digitalAssetsServerDirBtn');
    if (serverDirBtn) serverDirBtn.addEventListener('click', function() {
      if (isAdminDialogOpen()) return;
      showPrompt(tr('digitalAssetsServerDirPrompt'), {
        title: tr('digitalAssetsServerDirTitle'),
        required: true,
        placeholder: '/data/docs'
      }).then(function(path) {
        if (!path) return;
        importServerDir(item.id, path);
      });
    });
    const shareBtn = byID('digitalAssetsShareBtn');
    if (shareBtn) shareBtn.addEventListener('click', function() {
      if (isAdminDialogOpen()) return;
      showPrompt(tr('digitalAssetsSharePrompt'), {
        title: tr('digitalAssetsShareTitle'),
        required: true
      }).then(function(ref) {
        if (!ref) return;
        importShare(item.id, ref);
      });
    });
    const mergeBtn = byID('digitalAssetsMergeBtn');
    if (mergeBtn) mergeBtn.addEventListener('click', function() {
      captureAclDraftFromDom();
      state.mergeOpen = true;
      renderDetail();
    });
    const mergeCancel = byID('digitalAssetsMergeCancelBtn');
    if (mergeCancel) mergeCancel.addEventListener('click', function() {
      captureAclDraftFromDom();
      state.mergeOpen = false;
      renderDetail();
    });
    const mergeConfirm = byID('digitalAssetsMergeConfirmBtn');
    if (mergeConfirm) mergeConfirm.addEventListener('click', function() {
      const selected = [];
      detailQueryAll('.digital-assets-merge-src').forEach(function(cb) {
        if (cb.checked) selected.push(cb.value);
      });
      if (!selected.length) {
        showToast(tr('digitalAssetsMergeNeedSelect'), 'error');
        return;
      }
      const archiveEl = byID('digitalAssetsMergeArchive');
      const archive = !archiveEl || !!archiveEl.checked;
      mergeInto(item.id, selected, archive).then(function() {
        state.mergeOpen = false;
      });
    });
    const exportBtn = byID('digitalAssetsExportBtn');
    if (exportBtn) exportBtn.addEventListener('click', function() { exportBackup(item.id); });
    const bakInput = byID('digitalAssetsBackupInput');
    if (bakInput) bakInput.addEventListener('change', function() {
      if (!bakInput.files || !bakInput.files.length) return;
      importBackup(bakInput.files[0]).finally(function() { bakInput.value = ''; });
    });
    const searchBtn = byID('digitalAssetsSearchBtn');
    if (searchBtn) searchBtn.addEventListener('click', function() {
      if (isAdminDialogOpen()) return;
      showPrompt(tr('digitalAssetsSearchPrompt'), {
        title: tr('digitalAssetsSearchTitle'),
        required: true
      }).then(function(q) {
        if (!q) return;
        searchLibrary(item.id, q);
      });
    });
    const delBtn = byID('digitalAssetsDeleteBtn');
    if (delBtn) delBtn.addEventListener('click', function() {
      if (isAdminDialogOpen() || state.deleteLibraryBusy) return;
      showConfirm(tr('digitalAssetsDeleteLibraryConfirm', { name: item.name || item.id }), {
        title: tr('digitalAssetsDeleteLibraryTitle'),
        danger: true,
        confirmText: tr('digitalAssetsDelete')
      }).then(function(ok) {
        if (!ok || state.deleteLibraryBusy) return;
        deleteLibrary(item.id);
      });
    });
  }

  function wireSubmissionHandlers() {
    (state.submissions || []).forEach(function(s) {
      if (!s || !s.id) return;
      var approveBtn = byID('digitalAssetsApprove-' + s.id);
      if (approveBtn) approveBtn.addEventListener('click', function() { approveSubmission(s.id); });
      var rejectBtn = byID('digitalAssetsReject-' + s.id);
      if (rejectBtn) rejectBtn.addEventListener('click', function() { rejectSubmission(s.id); });
    });
  }

  function detailQueryAll(sel) {
    const detail = byID('digitalAssetsDetail');
    if (!detail) return [];
    return Array.prototype.slice.call(detail.querySelectorAll(sel));
  }

  async function uploadFiles(id, fileList) {
    const fd = new FormData();
    Array.prototype.forEach.call(fileList, function(f) { fd.append('files', f, f.name); });
    await runImport('/api/admin/digital-assets/libraries/' + encodeURIComponent(id) + '/import/upload', fd, true);
  }

  async function uploadZip(id, file) {
    const fd = new FormData();
    fd.append('file', file, file.name);
    await runImport('/api/admin/digital-assets/libraries/' + encodeURIComponent(id) + '/import/archive', fd, true);
  }

  async function uploadFolder(id, fileList) {
    const fd = new FormData();
    Array.prototype.forEach.call(fileList, function(f) {
      var rel = f.webkitRelativePath || f.name;
      fd.append('files', f, rel);
    });
    await runImport('/api/admin/digital-assets/libraries/' + encodeURIComponent(id) + '/import/browser-dir', fd, true);
  }

  async function importServerDir(id, path) {
    await runImport('/api/admin/digital-assets/libraries/' + encodeURIComponent(id) + '/import/local-dir',
      JSON.stringify({ path: path }), false);
  }

  async function importShare(id, ref) {
    var token = beginProgress({ phase: 'importing', percent: 20, message: tr('digitalAssetsProgressProcessing') });
    try {
      const res = await api('/api/admin/digital-assets/libraries/' + encodeURIComponent(id) + '/import/knowledge-share', {
        method: 'POST', body: JSON.stringify({ share_ref: ref, import_mode: 'merge_namespace' })
      });
      if (res && !res.id && res.job_id) res.id = res.job_id;
      // Knowledge-share import is synchronous today, but still normalize via trackJob.
      if (res && jobIdOf(res) && !isTerminalStatus(res.status)) {
        await trackJob(res, token);
      } else {
        clearProgress(token);
        if (res && res.status === 'failed') {
          showToast(tr('digitalAssetsProgressFailed', { error: res.error || 'failed' }), 'error');
        } else {
          showToast(tr('digitalAssetsImportDone', { status: (res && res.status) || 'ok' }), 'success');
          maybeRefreshOpenContent(id);
        }
      }
      await global.loadDigitalAssetLibraries();
    } catch (err) {
      clearProgress(token);
      showToast(String(err.message || err), 'error');
    }
  }

  async function mergeInto(targetId, sourceIds, archiveSources) {
    var token = beginProgress({ phase: 'merging', percent: 30, message: tr('digitalAssetsProgressProcessing') });
    try {
      const res = await api('/api/admin/digital-assets/libraries/merge', {
        method: 'POST',
        body: JSON.stringify({
          target_library_id: targetId,
          source_library_ids: sourceIds,
          archive_sources: archiveSources !== false
        })
      });
      if (res && !res.id && res.job_id) res.id = res.job_id;
      if (res && jobIdOf(res) && !isTerminalStatus(res.status)) {
        await trackJob(res, token);
      } else {
        clearProgress(token);
        if (res && res.status === 'failed') {
          showToast(tr('digitalAssetsProgressFailed', { error: res.error || 'failed' }), 'error');
        } else {
          showToast(tr('digitalAssetsImportDone', { status: (res && res.status) || 'ok' }), 'success');
          maybeRefreshOpenContent(targetId);
        }
      }
      await global.loadDigitalAssetLibraries();
    } catch (err) {
      clearProgress(token);
      showToast(String(err.message || err), 'error');
    }
  }

  async function exportBackup(libraryId) {
    try {
      const res = await api('/api/admin/digital-assets/export', {
        method: 'POST', body: JSON.stringify({ library_ids: [libraryId] })
      });
      if (res && res.download_path) {
        await downloadBackup(res.download_path);
      }
      showToast(tr('digitalAssetsImportDone', { status: (res && res.status) || 'ok' }), 'success');
    } catch (err) {
      showToast(String(err.message || err), 'error');
    }
  }

  async function downloadBackup(path) {
    var accessToken = typeof global.token === 'function' ? global.token() : '';
    var res = await fetch(path, { headers: { Authorization: 'Bearer ' + accessToken } });
    if (res.status === 401) {
      if (typeof global.token !== 'function' || global.token() === accessToken) {
        if (typeof global.logoutAdmin === 'function') global.logoutAdmin();
      }
      throw new Error(tr('sessionExpired'));
    }
    if (!res.ok) throw new Error('backup download failed');
    var blob = await res.blob();
    var url = global.URL.createObjectURL(blob);
    var link = global.document.createElement('a');
    try {
      link.href = url;
      link.download = backupFilename(res.headers.get('Content-Disposition'));
      global.document.body.appendChild(link);
      link.click();
    } finally {
      link.remove();
      global.URL.revokeObjectURL(url);
    }
  }

  function backupFilename(contentDisposition) {
    var match = /filename="?([^";]+)"?/i.exec(String(contentDisposition || ''));
    return match && match[1] ? match[1] : 'digital-assets-backup.zip';
  }

  async function importBackup(file) {
    const fd = new FormData();
    fd.append('file', file, file.name);
    fd.append('mode', 'new_libraries');
    fd.append('restore_acl', 'true');
    var token = beginProgress({ phase: 'restoring', percent: 25, message: tr('digitalAssetsProgressProcessing') });
    try {
      const res = await api('/api/admin/digital-assets/import/backup', { method: 'POST', body: fd });
      if (res && !res.id && res.job_id) res.id = res.job_id;
      if (res && jobIdOf(res) && !isTerminalStatus(res.status)) {
        await trackJob(res, token);
      } else {
        clearProgress(token);
        showToast(tr('digitalAssetsImportDone', { status: (res && res.status) || 'ok' }), 'success');
      }
      await global.loadDigitalAssetLibraries();
    } catch (err) {
      clearProgress(token);
      showToast(String(err.message || err), 'error');
    }
  }

  async function searchLibrary(id, q) {
    try {
      const data = await api('/api/admin/digital-assets/libraries/' + encodeURIComponent(id) + '/search?q=' + encodeURIComponent(q));
      const hits = (data && data.items) || [];
      const box = byID('digitalAssetsHits');
      if (!box) return;
      box.innerHTML = hits.length
        ? hits.map(function(h) {
          return '<div class="item"><div class="item-title">' + escapeHtml(h.title || h.source_id || '') + '</div>'
            + '<div class="item-meta">' + escapeHtml((h.snippet || h.text || '').slice(0, 200)) + '</div></div>';
        }).join('')
        : '<div class="empty">' + escapeHtml(tr('digitalAssetsNoHits')) + '</div>';
    } catch (err) {
      showToast(String(err.message || err), 'error');
    }
  }

  async function deleteLibrary(id) {
    if (!id || state.deleteLibraryBusy) return;
    state.deleteLibraryBusy = true;
    try {
      await api('/api/admin/digital-assets/libraries/' + encodeURIComponent(id), { method: 'DELETE' });
      state.selectedId = '';
      state.mergeOpen = false;
      state.contentOpen = false;
      clearAclDraft();
      renderOverlays();
      await global.loadDigitalAssetLibraries();
    } catch (err) {
      showToast(String(err.message || err), 'error');
    } finally {
      state.deleteLibraryBusy = false;
    }
  }

  global.loadDigitalAssetLibraries = async function loadDigitalAssetLibraries(opts) {
    if (!canManageDigitalAssets()) {
      stopDigitalAssetsForUnauthorizedScope();
      return null;
    }
    opts = opts || {};
    try {
      // Capture unsaved ACL form before list refresh (import complete / manual reload).
      captureAclDraftFromDom();
      // Force-refresh department tree on tab open / explicit Reload only (not every import).
      await loadSecurityGroups({ force: !!opts.refreshGroups, renderDetail: false });
      const data = await api('/api/admin/digital-assets/libraries');
      state.items = (data && data.items) || [];
      if (state.selectedId && !state.items.some(function(x) { return x.id === state.selectedId; })) {
        state.selectedId = '';
        state.mergeOpen = false;
        state.contentOpen = false;
        clearAclDraft();
      }
      if (!state.selectedId && state.items.length) state.selectedId = state.items[0].id;
      // Drop draft if its library disappeared.
      if (state.aclDraft && state.aclDraft.libraryId
          && !state.items.some(function(x) { return x.id === state.aclDraft.libraryId; })) {
        clearAclDraft();
      }
      await loadLibrarySubmissions(state.selectedId);
      renderList();
      renderDetail();
      renderOverlays();
    } catch (err) {
      const box = byID('digitalAssetsList');
      if (!box) return;
      const msg = String(err && err.message || err || '');
      const disabled = /feature is disabled|FEATURE_DISABLED|feature disabled/i.test(msg);
      box.innerHTML = '<div class="empty">' + escapeHtml(disabled
        ? tr('digitalAssetsDisabledHint')
        : tr('digitalAssetsLoadFailed', { error: msg })) + '</div>';
      const detail = byID('digitalAssetsDetail');
      if (detail) detail.innerHTML = '';
    }
  };

  var createLibraryBusy = false;
  global.createDigitalAssetLibrary = async function createDigitalAssetLibrary() {
    if (!canManageDigitalAssets()) {
      stopDigitalAssetsForUnauthorizedScope();
      return null;
    }
    if (createLibraryBusy || isAdminDialogOpen()) return;
    createLibraryBusy = true;
    try {
      const name = await showPrompt(tr('digitalAssetsCreateNamePrompt'), {
        title: tr('digitalAssetsCreateTitle'),
        required: true,
        requiredText: tr('digitalAssetsCreateNameRequired'),
        placeholder: tr('digitalAssetsName'),
        confirmText: tr('digitalAssetsCreate')
      });
      if (!name) return;
      const kindAnswer = await showPrompt(tr('digitalAssetsKindPrompt'), {
        title: tr('digitalAssetsKind'),
        placeholder: 'business'
      });
      var kind = String(kindAnswer || 'business').trim().toLowerCase();
      if (kind !== 'technical') kind = 'business';
      try {
        await api('/api/admin/digital-assets/libraries', {
          method: 'POST',
          body: JSON.stringify({ name: name, acl_mode: 'all_members', sync_enabled: true, library_kind: kind, accepts_submissions: true })
        });
        showToast(tr('digitalAssetsCreateDone'), 'success');
        await global.loadDigitalAssetLibraries();
      } catch (err) {
        showToast(String(err.message || err), 'error');
      }
    } finally {
      createLibraryBusy = false;
    }
  };

  function applyDigitalAssetsI18n() {
    var panel = byID('tab-digital-assets');
    if (panel && panel.classList.contains('active')) {
      renderList();
      renderDetail({ skipGroups: true });
    }
    if (state.contentOpen || state.progress) renderOverlays();
  }

  if (typeof global.tabMeta === 'object') global.tabMeta['digital-assets'] = ['digitalAssetsTabTitle', 'digitalAssetsTabSubtitle'];
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'digital-assets',
      title: function() { return tr('digitalAssetsTabTitle'); },
      subtitle: function() { return tr('digitalAssetsTabSubtitle'); },
      onOpen: function() { global.loadDigitalAssetLibraries({ refreshGroups: true }); }
    });
  }
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(applyDigitalAssetsI18n);
  }
  global.stopDigitalAssetsForUnauthorizedScope = stopDigitalAssetsForUnauthorizedScope;
})(window);
