/*
 * Security admin module.
 * ASCII only.
 */
(function(global) {
  function state() {
    if (!global.__securityAdminState) {
      global.__securityAdminState = {
        groupTree: [],
        selectedGroupId: null,
        selectedGroupName: null,
        contextGroupId: null,
        contextGroupName: null,
        policyCache: null,
        assignGroupId: null,
        defaultGroupPickedId: null,
        expandedGroupIds: {},
        loadedChildrenGroupIds: {},
        loadingChildrenGroupIds: {},
        defaultGroupTree: [],
        assignUsers: [],
        selectedAssignEmail: '',
        selectedAssignEmails: {},
        contextMenuHideHandler: null,
        membersPage: 1,
        membersPageSize: 60,
        membersCache: [],
        membersChildrenCache: [],
        membersSearch: '',
        membersModalPage: 1,
        selectedObjectType: 'global',
        selectedUserEmail: '',
        userDirectoryCache: null,
        llmServiceCache: null,
        capabilityCache: null,
        lastCapabilityCompliance: null,
        currentPolicyExport: null,
        currentModelServiceExport: null,
        currentCapabilityExport: null,
        currentMemberSnapshot: null,
        currentOverviewSnapshot: null,
        capabilityComplianceStatusFilter: '',
        capabilityIncludeUnmanaged: true,
        capabilityStaleAfterHours: 168,
        auditLogs: [],
        auditLimit: 20,
        auditQuery: '',
        auditAction: '',
        auditFrom: '',
        auditTo: '',
        subTab: 'management'
      };
    }
    return global.__securityAdminState;
  }

  function getCurrentLang() {
    if (typeof currentLang !== 'undefined' && (currentLang === 'zh' || currentLang === 'en')) return currentLang;
    if (global.currentLang === 'zh' || global.currentLang === 'en') return global.currentLang;
    try {
      var saved = global.localStorage && global.localStorage.getItem('maclaw_admin_lang');
      if (saved === 'zh' || saved === 'en') return saved;
    } catch (_) {}
    return (global.document && global.document.documentElement && global.document.documentElement.lang === 'zh-CN') ? 'zh' : 'en';
  }

  function isZh() {
    return getCurrentLang() === 'zh';
  }

  function text(zh, en) {
    return isZh() ? zh : en;
  }

  function secTr(key, zh, en) {
    if (typeof global.tr === 'function') {
      var translated = global.tr(key);
      if (translated && translated !== key) return translated;
    }
    return text(zh, en);
  }

  var SECURITY_I18N = {
    subgroupLabel: { zh: '\u5b50\u7ec4:', en: 'Sub-groups:' },
    membersLabel: { zh: '\u6210\u5458', en: 'Members' },
    userIndex: { zh: '\u7b2c {index} \u4e2a\u7528\u6237', en: 'User #{index}' },
    remove: { zh: '\u79fb\u9664', en: 'Remove' },
    pagerSummary: { zh: '\u7b2c {page} / {totalPages} \u9875\uff0c\u663e\u793a {start}-{end} / {total}', en: 'Page {page} / {totalPages}, showing {start}-{end} / {total}' },
    previous: { zh: '\u4e0a\u4e00\u9875', en: 'Previous' },
    next: { zh: '\u4e0b\u4e00\u9875', en: 'Next' },
    noMembers: { zh: '\u65e0\u6210\u5458', en: 'No members' },
    noGroups: { zh: '\u65e0\u7ec4\u7ec7\u6570\u636e', en: 'No groups' },
    loading: { zh: '\u6b63\u5728\u52a0\u8f7d...', en: 'Loading...' },
    enabled: { zh: '\u5df2\u542f\u7528', en: 'Enabled' },
    disabled: { zh: '\u5df2\u7981\u7528', en: 'Disabled' },
    notSet: { zh: '\u672a\u8bbe\u7f6e', en: 'Not set' },
    noUsersMatchSearch: { zh: '\u65e0\u5339\u914d\u7528\u6237', en: 'No users match the search' },
    noUsersAvailable: { zh: '\u6682\u65e0\u53ef\u5206\u914d\u7528\u6237', en: 'No users available' },
    status: { zh: '\u72b6\u6001', en: 'Status' },
    unknown: { zh: '\u672a\u77e5', en: 'Unknown' },
    statusActive: { zh: '\u5df2\u542f\u7528', en: 'Active' },
    statusInactive: { zh: '\u672a\u542f\u7528', en: 'Inactive' },
    statusPending: { zh: '\u5f85\u5904\u7406', en: 'Pending' },
    statusBlocked: { zh: '\u5df2\u5c4f\u853d', en: 'Blocked' },
    statusDisabled: { zh: '\u5df2\u7981\u7528', en: 'Disabled' },
    statusApproved: { zh: '\u5df2\u6279\u51c6', en: 'Approved' },
    move: { zh: '\u79fb\u5165', en: 'Move' },
    showingUsers: { zh: '\u663e\u793a {visible} / {total} \u4e2a\u7528\u6237', en: 'Showing {visible} / {total} users' },
    selectedUsers: { zh: '\u5df2\u9009 {count} \u4e2a\u7528\u6237', en: 'Selected {count} users' },
    selectVisibleUsers: { zh: '\u5168\u9009\u5217\u8868\u7528\u6237', en: 'Select listed users' },
    clearVisibleUsers: { zh: '\u53d6\u6d88\u5217\u8868\u5168\u9009', en: 'Clear listed users' },
    loadingMembers: { zh: '\u6b63\u5728\u52a0\u8f7d\u6210\u5458...', en: 'Loading members...' },
    removed: { zh: '\u5df2\u79fb\u9664', en: 'Removed' },
    centralizedEnabled: { zh: '\u96c6\u4e2d\u7b56\u7565\u5df2\u542f\u7528', en: 'Centralized policy enabled' },
    centralizedDisabled: { zh: '\u96c6\u4e2d\u7b56\u7565\u5df2\u7981\u7528', en: 'Centralized policy disabled' },
    orgEnabled: { zh: '\u7ec4\u7ec7\u67b6\u6784\u5df2\u542f\u7528', en: 'Org structure enabled' },
    orgDisabled: { zh: '\u7ec4\u7ec7\u67b6\u6784\u5df2\u7981\u7528', en: 'Org structure disabled' },
    defaultGroupSet: { zh: '\u9ed8\u8ba4\u7ec4\u5df2\u8bbe\u7f6e', en: 'Default group set' },
    assignTitleWithGroup: { zh: '\u79fb\u5165\u7528\u6237\u5230\u90e8\u95e8: {name}', en: 'Move users to department: {name}' },
    loadingUsers: { zh: '\u6b63\u5728\u52a0\u8f7d\u7528\u6237\u5217\u8868...', en: 'Loading users...' },
    moveUsersHere: { zh: '\u79fb\u5165\u7528\u6237', en: 'Move Users Here' },
    moveUsers: { zh: '\u79fb\u5165\u7528\u6237', en: 'Move Users' },
    moveUsersDesc: { zh: '\u53ef\u4ee5\u641c\u7d22\u5e76\u6279\u91cf\u5c06\u7528\u6237\u79fb\u5165\u5f53\u524d\u90e8\u95e8\u3002', en: 'Search and move users into the selected department.' },
    searchEmailOrSn: { zh: '\u641c\u7d22\u90ae\u7bb1\u6216 SN', en: 'Search email or SN' },
    departmentActions: { zh: '\u90e8\u95e8\u64cd\u4f5c\u83dc\u5355', en: 'Department actions' },
    userMoved: { zh: '\u7528\u6237\u5df2\u79fb\u5165', en: 'User moved' },
    reload: { zh: '\u5237\u65b0', en: 'Reload' },
    centralizedPolicy: { zh: '\u96c6\u4e2d\u7b56\u7565', en: 'Centralized Policy' },
    orgStructure: { zh: '\u7ec4\u7ec7\u67b6\u6784', en: 'Org Structure' },
    defaultGroup: { zh: '\u9ed8\u8ba4\u7ec4', en: 'Default Group' },
    set: { zh: '\u8bbe\u7f6e', en: 'Set' },
    groupTree: { zh: '\u7ec4\u7ec7\u6811', en: 'Group Tree' },
    createSubDepartment: { zh: '\u521b\u5efa\u5b50\u90e8\u95e8', en: 'Create Sub-department' },
    renameDepartment: { zh: '\u91cd\u547d\u540d\u90e8\u95e8', en: 'Rename Department' },
    deleteDepartment: { zh: '\u5220\u9664\u90e8\u95e8', en: 'Delete Department' },
    save: { zh: '\u4fdd\u5b58', en: 'Save' },
    cancel: { zh: '\u53d6\u6d88', en: 'Cancel' },
    confirm: { zh: '\u786e\u8ba4', en: 'Confirm' },
    chooseDefaultGroupDesc: { zh: '\u4e3a\u65b0\u7528\u6237\u9009\u62e9\u9ed8\u8ba4\u6240\u5c5e\u7ec4\u3002', en: 'Choose the default group for new users.' },
    defaultGroupPrefix: { zh: '\u9ed8\u8ba4\u7ec4: ', en: 'Default group: ' },
    loadSecuritySettingsFailed: { zh: '\u52a0\u8f7d\u5b89\u5168\u8bbe\u7f6e\u5931\u8d25: ', en: 'Load security settings failed: ' },
    loadGroupTreeFailed: { zh: '\u52a0\u8f7d\u7ec4\u7ec7\u6811\u5931\u8d25: ', en: 'Load group tree failed: ' },
    loadChildGroupsFailed: { zh: '\u52a0\u8f7d\u5b50\u90e8\u95e8\u5931\u8d25: ', en: 'Load child groups failed: ' },
    policyPrefix: { zh: '\u7b56\u7565: ', en: 'Policy: ' },
    groupIdPrefix: { zh: '\u7ec4 ID: ', en: 'Group ID: ' },
    fileOutbound: { zh: '\u6587\u4ef6\u5916\u53d1', en: 'File Outbound' },
    imageOutbound: { zh: '\u56fe\u7247\u5916\u53d1', en: 'Image Outbound' },
    gossip: { zh: 'Gossip \u529f\u80fd', en: 'Gossip' },
    yoloMode: { zh: 'YOLO \u6a21\u5f0f', en: 'YOLO Mode' },
    smartRoute: { zh: '\u667a\u80fd\u8def\u7531', en: 'Smart Route' },
    guardrailMode: { zh: '\u62a4\u680f\u6a21\u5f0f', en: 'Guardrail Mode' },
    sandboxMode: { zh: '\u6c99\u7bb1\u6a21\u5f0f', en: 'Sandbox Mode' },
    networkLevel: { zh: '\u7f51\u7edc\u7ea7\u522b', en: 'Network Level' },
    custom: { zh: '\u81ea\u5b9a\u4e49', en: 'Custom' },
    inheritedFrom: { zh: '\u7ee7\u627f\u81ea ', en: 'Inherited from ' },
    inheritPolicy: { zh: '\u7ee7\u627f', en: 'Inherit' },
    overridePolicy: { zh: '\u8986\u76d6', en: 'Override' },
    selectGroupFirst: { zh: '\u8bf7\u5148\u9009\u62e9\u4e00\u4e2a\u7ec4', en: 'Select a group first' },
    policySaved: { zh: '\u7b56\u7565\u5df2\u4fdd\u5b58', en: 'Policy saved' },
    savePolicyFailed: { zh: '\u4fdd\u5b58\u7b56\u7565\u5931\u8d25: ', en: 'Save policy failed: ' },
    removeFailed: { zh: '\u79fb\u9664\u5931\u8d25: ', en: 'Remove failed: ' },
    updateFailed: { zh: '\u66f4\u65b0\u5931\u8d25: ', en: 'Update failed: ' },
    pleaseSelectGroup: { zh: '\u8bf7\u9009\u62e9\u4e00\u4e2a\u7ec4', en: 'Please select a group' },
    setDefaultGroupFailed: { zh: '\u8bbe\u7f6e\u9ed8\u8ba4\u7ec4\u5931\u8d25: ', en: 'Set default group failed: ' },
    promptNewSubGroup: { zh: '\u8f93\u5165\u65b0\u5b50\u7ec4\u540d\u79f0:', en: 'Enter new sub-group name:' },
    subgroupCreated: { zh: '\u5b50\u7ec4\u5df2\u521b\u5efa', en: 'Sub-group created' },
    createFailed: { zh: '\u521b\u5efa\u5931\u8d25: ', en: 'Create failed: ' },
    promptNewName: { zh: '\u8f93\u5165\u65b0\u540d\u79f0:', en: 'Enter new name:' },
    renamed: { zh: '\u5df2\u91cd\u547d\u540d', en: 'Renamed' },
    renameFailed: { zh: '\u91cd\u547d\u540d\u5931\u8d25: ', en: 'Rename failed: ' },
    loadUsersFailed: { zh: '\u52a0\u8f7d\u7528\u6237\u5931\u8d25: ', en: 'Load users failed: ' },
    groupDeleted: { zh: '\u7ec4\u5df2\u5220\u9664', en: 'Group deleted' },
    deleteFailed: { zh: '\u5220\u9664\u5931\u8d25: ', en: 'Delete failed: ' },
    selectOrEnterEmail: { zh: '\u8bf7\u9009\u62e9\u6216\u8f93\u5165\u90ae\u7bb1', en: 'Select or enter an email' },
    selectOrEnterUsers: { zh: '\u8bf7\u9009\u62e9\u7528\u6237\u6216\u8f93\u5165\u90ae\u7bb1', en: 'Select users or enter an email' },
    assignFailed: { zh: '\u5206\u914d\u5931\u8d25: ', en: 'Assign failed: ' },
    members: { zh: '\u6210\u5458', en: 'Members' },
    confirmRemoveUser: { zh: '\u786e\u5b9a\u79fb\u9664 {email} \u5417\uff1f', en: 'Remove {email}?' },
    confirmDeleteGroup: { zh: '\u786e\u5b9a\u5220\u9664\u7ec4 "{name}" \u5417\uff1f', en: 'Delete group "{name}"?' },
    enterpriseTitle: { zh: '\u4f01\u4e1a\u7ba1\u7406', en: 'Enterprise Management' },
    enterpriseSubtitle: { zh: '\u4ee5\u7ec4\u7ec7\u67b6\u6784\u4e3a\u4e2d\u5fc3\uff0c\u67e5\u770b\u90e8\u95e8\u3001\u7528\u6237\u7684\u751f\u6548\u7b56\u7565\u548c\u80fd\u529b\u5305\u3002', en: 'Manage effective policies and capability packages from the organization tree.' },
    objectOverview: { zh: '\u5bf9\u8c61\u6982\u89c8', en: 'Object Overview' },
    exportObjectSnapshotJson: { zh: '\u5bfc\u51fa\u5bf9\u8c61\u603b\u5feb\u7167 JSON', en: 'Export object snapshot JSON' },
    objectSnapshotExported: { zh: '\u5bf9\u8c61\u603b\u5feb\u7167\u5df2\u5bfc\u51fa', en: 'Object snapshot exported' },
    objectSnapshotFailed: { zh: '\u5bfc\u51fa\u5bf9\u8c61\u603b\u5feb\u7167\u5931\u8d25: ', en: 'Export object snapshot failed: ' },
    effectivePolicy: { zh: '\u751f\u6548\u7b56\u7565', en: 'Effective Policy' },
    policyOverrides: { zh: '\u8986\u76d6\u9879', en: 'Overrides' },
    policyInherited: { zh: '\u7ee7\u627f\u9879', en: 'Inherited' },
    exportPolicyJson: { zh: '\u5bfc\u51fa\u7b56\u7565 JSON', en: 'Export policy JSON' },
    policyExported: { zh: '\u7b56\u7565\u5feb\u7167\u5df2\u5bfc\u51fa', en: 'Policy snapshot exported' },
    policyExportEmpty: { zh: '\u6682\u65e0\u53ef\u5bfc\u51fa\u7684\u7b56\u7565', en: 'No policy snapshot to export' },
    policyExportFailed: { zh: '\u5bfc\u51fa\u7b56\u7565\u5931\u8d25: ', en: 'Export policy failed: ' },
    globalSecurityPolicy: { zh: '\u5168\u5c40\u5b89\u5168\u7b56\u7565', en: 'Global Security Policy' },
    modelServiceGroups: { zh: '\u6a21\u578b\u670d\u52a1\u7ec4', en: 'Model Service Groups' },
    exportModelServiceJson: { zh: '\u5bfc\u51fa\u6a21\u578b\u670d\u52a1 JSON', en: 'Export model service JSON' },
    modelServiceExported: { zh: '\u6a21\u578b\u670d\u52a1\u5feb\u7167\u5df2\u5bfc\u51fa', en: 'Model service snapshot exported' },
    modelServiceExportEmpty: { zh: '\u6682\u65e0\u53ef\u5bfc\u51fa\u7684\u6a21\u578b\u670d\u52a1\u5feb\u7167', en: 'No model service snapshot to export' },
    modelServiceExportFailed: { zh: '\u5bfc\u51fa\u6a21\u578b\u670d\u52a1\u5931\u8d25: ', en: 'Export model service failed: ' },
    capabilityPackages: { zh: 'Skill/MCP \u80fd\u529b\u5305', en: 'Skill/MCP Packages' },
    exportCapabilityJson: { zh: '\u5bfc\u51fa\u80fd\u529b\u5305 JSON', en: 'Export package JSON' },
    capabilitySnapshotExported: { zh: '\u80fd\u529b\u5305\u5feb\u7167\u5df2\u5bfc\u51fa', en: 'Package snapshot exported' },
    capabilitySnapshotEmpty: { zh: '\u6682\u65e0\u53ef\u5bfc\u51fa\u7684\u80fd\u529b\u5305\u5feb\u7167', en: 'No package snapshot to export' },
    capabilitySnapshotFailed: { zh: '\u5bfc\u51fa\u80fd\u529b\u5305\u5931\u8d25: ', en: 'Export package snapshot failed: ' },
    membersDialog: { zh: '\u67e5\u770b\u6210\u5458', en: 'View Members' },
    memberCount: { zh: '\u6210\u5458', en: 'Members' },
    childGroupCount: { zh: '\u5b50\u90e8\u95e8', en: 'Sub-groups' },
    orgPath: { zh: '\u7ec4\u7ec7\u8def\u5f84', en: 'Org Path' },
    selectedDepartment: { zh: '\u5f53\u524d\u90e8\u95e8', en: 'Selected Department' },
    selectedUser: { zh: '\u5f53\u524d\u7528\u6237', en: 'Selected User' },
    userSN: { zh: '\u7528\u6237 SN', en: 'User SN' },
    enrollmentStatus: { zh: '\u51c6\u5165\u72b6\u6001', en: 'Enrollment' },
    deviceSummary: { zh: '\u8bbe\u5907\u4e0e\u4f1a\u8bdd', en: 'Devices and Sessions' },
    deviceOnlineCount: { zh: '\u5728\u7ebf\u8bbe\u5907', en: 'Online Devices' },
    deviceTotalCount: { zh: '\u7ed1\u5b9a\u8bbe\u5907', en: 'Bound Devices' },
    deviceLastSeen: { zh: '\u6700\u8fd1\u5fc3\u8df3', en: 'Last Seen' },
    deviceRuntime: { zh: '\u8fd0\u884c\u73af\u5883', en: 'Runtime' },
    deviceNoMachines: { zh: '\u6682\u65e0\u8bbe\u5907\u7ed1\u5b9a\u6216\u5fc3\u8df3\u8bb0\u5f55', en: 'No bound devices or heartbeat records yet' },
    deviceActiveSessions: { zh: '\u6d3b\u8dc3\u4f1a\u8bdd', en: 'Active Sessions' },
    serviceAccess: { zh: '\u6a21\u578b\u670d\u52a1', en: 'Model Service' },
    serviceAccessOn: { zh: '\u5df2\u5f00\u901a', en: 'Active' },
    serviceAccessOff: { zh: '\u672a\u5f00\u901a', en: 'Inactive' },
    sourceGlobalFallback: { zh: '\u672a\u6307\u5b9a\uff0c\u56de\u9000\u5230\u65b0\u7528\u6237\u9ed8\u8ba4\u6a21\u578b\u670d\u52a1\u7ec4', en: 'Not specified; falls back to new-user default service group' },
    sourceGlobalBinding: { zh: '\u4f01\u4e1a\u5168\u5c40\u9ed8\u8ba4\u6a21\u578b\u670d\u52a1\u7ec4', en: 'Enterprise global default service group' },
    sourceCurrentGroupBinding: { zh: '\u6765\u81ea\u5f53\u524d\u90e8\u95e8\u76f4\u63a5\u7ed1\u5b9a', en: 'From current department binding' },
    sourceInheritedGroupBinding: { zh: '\u7ee7\u627f\u4e0a\u7ea7\u90e8\u95e8\u7ed1\u5b9a', en: 'Inherited from parent department binding' },
    sourceGroupBinding: { zh: '\u6765\u81ea\u90e8\u95e8/\u7ec4\u7ed1\u5b9a', en: 'From department/group binding' },
    sourceUserBinding: { zh: '\u6765\u81ea\u7528\u6237\u76f4\u63a5\u7ed1\u5b9a', en: 'From direct user binding' },
    modelServiceEffective: { zh: '\u751f\u6548\u6a21\u578b\u670d\u52a1\u7ec4', en: 'Effective Model Service Groups' },
    modelServiceDirectOverride: { zh: '\u76f4\u63a5\u8986\u76d6', en: 'Direct Override' },
    modelServiceInheritHint: { zh: '\u7559\u7a7a\u8868\u793a\u7ee7\u627f\u90e8\u95e8\u3001\u5168\u5c40\u6216\u65b0\u7528\u6237\u9ed8\u8ba4\u7ec4', en: 'Leave empty to inherit department, global, or new-user defaults' },
    resolvedSecurityGroups: { zh: '\u547d\u4e2d\u90e8\u95e8', en: 'Matched Departments' },
    matchedBindings: { zh: '\u547d\u4e2d\u7ed1\u5b9a', en: 'Matched Bindings' },
    globalObject: { zh: '\u5168\u5c40', en: 'Global' },
    globalObjectDesc: { zh: '\u4f01\u4e1a\u9ed8\u8ba4\u7b56\u7565\u3001\u65b0\u7528\u6237\u56de\u9000\u548c\u5168\u5c40\u80fd\u529b\u5305\u3002', en: 'Enterprise defaults, new-user fallback, and global capability packages.' },
    capabilityRequired: { zh: '\u5fc5\u88c5', en: 'Required' },
    capabilityRecommended: { zh: '\u63a8\u8350', en: 'Recommended' },
    capabilityBlocked: { zh: '\u7981\u6b62', en: 'Blocked' },
    capabilitySourceGlobal: { zh: '\u5168\u5c40\u4e0b\u53d1', en: 'Global rollout' },
    capabilitySourceGroup: { zh: '\u90e8\u95e8/\u7ec4\u4e0b\u53d1', en: 'Department/group rollout' },
    capabilitySourceUser: { zh: '\u7528\u6237\u4e0b\u53d1', en: 'User rollout' },
    capabilityCompliant: { zh: '\u5df2\u5408\u89c4', en: 'Compliant' },
    capabilityMissing: { zh: '\u7f3a\u5931', en: 'Missing' },
    capabilityVersionMismatch: { zh: '\u7248\u672c\u4e0d\u7b26', en: 'Version mismatch' },
    capabilityBlockedInstalled: { zh: '\u7981\u6b62\u4f46\u5df2\u5b89\u88c5', en: 'Blocked but installed' },
    capabilityPendingReport: { zh: '\u5f85\u5ba2\u6237\u7aef\u4e0a\u62a5', en: 'Pending client report' },
    capabilityReportStale: { zh: '\u4e0a\u62a5\u8fc7\u671f', en: 'Report stale' },
    capabilityCompliance: { zh: '\u5408\u89c4\u72b6\u6001', en: 'Compliance' },
    capabilityRequiredCount: { zh: '\u5e94\u88c5', en: 'Required' },
    capabilityRecommendedCount: { zh: '\u63a8\u8350', en: 'Recommended' },
    capabilityBlockedCount: { zh: '\u7981\u6b62', en: 'Blocked' },
    capabilityTelemetryHint: { zh: '\u7528\u6237\u7aef\u5b89\u88c5\u6e05\u5355\u4e0a\u62a5\u63a5\u5165\u540e\uff0c\u6b64\u5904\u5c06\u663e\u793a\u5df2\u88c5\u3001\u7f3a\u5931\u3001\u7248\u672c\u4e0d\u7b26\u548c\u7981\u6b62\u4f46\u5df2\u5b89\u88c5\u3002', en: 'When client install inventory reporting is wired, this view will show installed, missing, version mismatch, and blocked-but-installed states.' },
    capabilityInstalledVersion: { zh: '\u5df2\u88c5\u7248\u672c', en: 'Installed version' },
    capabilityExpectedVersion: { zh: '\u671f\u671b\u7248\u672c', en: 'Expected version' },
    capabilityLastSeen: { zh: '\u6700\u540e\u4e0a\u62a5', en: 'Last seen' },
    capabilityInstallStatus: { zh: '\u5ba2\u6237\u7aef\u72b6\u6001', en: 'Client status' },
    capabilityUnmanagedInstalled: { zh: '\u989d\u5916\u5b89\u88c5', en: 'Unmanaged installed' },
    capabilityUnmanagedHint: { zh: '\u5ba2\u6237\u7aef\u5df2\u5b89\u88c5\uff0c\u4f46\u5f53\u524d\u5168\u5c40/\u90e8\u95e8/\u7528\u6237\u7b56\u7565\u6ca1\u6709\u8986\u76d6\u7684\u80fd\u529b\u5305\u3002', en: 'Installed on the client but not covered by current global, department, or user policy.' },
    capabilityPolicySource: { zh: '\u7b56\u7565\u6765\u6e90', en: 'Policy source' },
    capabilityPolicyId: { zh: '\u7b56\u7565 ID', en: 'Policy ID' },
    capabilitySpecificity: { zh: '\u4f18\u5148\u7ea7', en: 'Priority' },
    capabilityTotal: { zh: '\u603b\u6570', en: 'Total' },
    capabilityGeneratedAt: { zh: '\u8ba1\u7b97\u65f6\u95f4', en: 'Generated at' },
    capabilityStaleAfterHours: { zh: '\u8fc7\u671f\u9608\u503c', en: 'Stale after' },
    capabilityStatusFilter: { zh: '\u72b6\u6001\u7b5b\u9009', en: 'Status filter' },
    capabilityAllStatuses: { zh: '\u5168\u90e8\u72b6\u6001', en: 'All statuses' },
    capabilityShowUnmanaged: { zh: '\u663e\u793a\u989d\u5916\u5b89\u88c5', en: 'Show unmanaged installed' },
    capabilityExportJson: { zh: '\u5bfc\u51fa JSON', en: 'Export JSON' },
    capabilityExported: { zh: '\u5408\u89c4\u7ed3\u679c\u5df2\u5bfc\u51fa', en: 'Compliance result exported' },
    capabilityExportEmpty: { zh: '\u6682\u65e0\u53ef\u5bfc\u51fa\u7684\u5408\u89c4\u7ed3\u679c', en: 'No compliance result to export yet' },
    capabilityExportFailed: { zh: '\u5bfc\u51fa\u5408\u89c4\u7ed3\u679c\u5931\u8d25: ', en: 'Export compliance result failed: ' },
    noCapabilityPackages: { zh: '\u5f53\u524d\u5bf9\u8c61\u6ca1\u6709\u5339\u914d\u7684 Skill/MCP \u4e0b\u53d1\u7b56\u7565\u3002', en: 'No Skill/MCP rollout policies match this object yet.' },
    capabilitySelect: { zh: '\u9009\u62e9\u80fd\u529b\u5305', en: 'Choose package' },
    addRequiredCapability: { zh: '\u8bbe\u4e3a\u5fc5\u88c5', en: 'Set Required' },
    addRecommendedCapability: { zh: '\u8bbe\u4e3a\u63a8\u8350', en: 'Recommend' },
    addBlockedCapability: { zh: '\u8bbe\u4e3a\u7981\u6b62', en: 'Block' },
    removeCapabilityPolicy: { zh: '\u53d6\u6d88\u4e0b\u53d1', en: 'Remove Rollout' },
    confirmRemoveCapabilityPolicy: { zh: '\u786e\u5b9a\u53d6\u6d88\u8fd9\u6761\u80fd\u529b\u5305\u4e0b\u53d1\u7b56\u7565\u5417\uff1f', en: 'Remove this capability rollout policy?' },
    noCapabilitiesAvailable: { zh: '\u80fd\u529b\u5e02\u573a\u6682\u65e0\u53ef\u7528\u80fd\u529b\u5305\u3002', en: 'No marketplace capabilities are available yet.' },
    capabilitySaved: { zh: '\u80fd\u529b\u5305\u4e0b\u53d1\u7b56\u7565\u5df2\u521b\u5efa', en: 'Capability rollout policy created' },
    capabilityRemoved: { zh: '\u80fd\u529b\u5305\u4e0b\u53d1\u7b56\u7565\u5df2\u53d6\u6d88', en: 'Capability rollout policy removed' },
    capabilitySaveFailed: { zh: '\u521b\u5efa\u80fd\u529b\u5305\u7b56\u7565\u5931\u8d25: ', en: 'Create capability policy failed: ' },
    version: { zh: '\u7248\u672c', en: 'Version' },
    viewUserDetail: { zh: '\u8be6\u60c5', en: 'Details' },
    membersModalDesc: { zh: '\u6210\u5458\u5217\u8868\u5df2\u79fb\u5165\u5f39\u7a97\uff0c\u652f\u6301\u641c\u7d22\u3001\u5206\u9875\u548c\u67e5\u770b\u7528\u6237\u751f\u6548\u7b56\u7565\u3002', en: 'Members are shown in a dialog with search, paging, and user effective policy details.' },
    exportMembersCsv: { zh: '\u5bfc\u51fa CSV', en: 'Export CSV' },
    membersExported: { zh: '\u6210\u5458\u5217\u8868\u5df2\u5bfc\u51fa', en: 'Members exported' },
    membersExportEmpty: { zh: '\u6682\u65e0\u53ef\u5bfc\u51fa\u6210\u5458', en: 'No members to export' },
    membersExportFailed: { zh: '\u5bfc\u51fa\u6210\u5458\u5931\u8d25: ', en: 'Export members failed: ' },
    searchMembers: { zh: '\u641c\u7d22\u6210\u5458\u90ae\u7bb1', en: 'Search member email' },
    modelRoutes: { zh: '\u53ef\u7528\u6a21\u578b', en: 'Available Models' },
    defaultModel: { zh: '\u9ed8\u8ba4\u6a21\u578b', en: 'Default Model' },
    inactiveReasons: { zh: '\u672a\u6fc0\u6d3b\u539f\u56e0', en: 'Inactive Reasons' },
    credits: { zh: '\u79ef\u5206', en: 'Credits' },
    creditsRemaining: { zh: '\u5269\u4f59\u79ef\u5206', en: 'Remaining Credits' },
    creditsUsed: { zh: '\u5df2\u7528\u79ef\u5206', en: 'Used Credits' },
    creditsTotal: { zh: '\u603b\u79ef\u5206', en: 'Total Credits' },
    effectiveExpiresAt: { zh: '\u6709\u6548\u671f', en: 'Effective Expires' },
    activeGrants: { zh: '\u751f\u6548\u6388\u6743', en: 'Active Grants' },
    recentChanges: { zh: '\u6700\u8fd1\u53d8\u66f4', en: 'Recent Changes' },
    recentChangesDesc: { zh: '\u5c55\u793a\u4f01\u4e1a\u7b56\u7565\u3001\u90e8\u95e8\u548c\u80fd\u529b\u5305\u7684\u6700\u8fd1\u7ba1\u7406\u64cd\u4f5c\u3002', en: 'Recent enterprise policy, department, and capability package admin actions.' },
    enterpriseManagementTab: { zh: '\u4f01\u4e1a\u7ba1\u7406', en: 'Enterprise Management' },
    recentChangesTab: { zh: '\u6700\u8fd1\u53d8\u66f4', en: 'Recent Changes' },
    auditSearch: { zh: '\u641c\u7d22\u64cd\u4f5c\u6216\u5185\u5bb9', en: 'Search action or payload' },
    auditActionFilter: { zh: '\u64cd\u4f5c\u7c7b\u578b', en: 'Action Type' },
    auditAllActions: { zh: '\u5168\u90e8\u64cd\u4f5c', en: 'All Actions' },
    auditLimitLabel: { zh: '\u663e\u793a\u6761\u6570', en: 'Limit' },
    auditFrom: { zh: '\u5f00\u59cb\u65e5\u671f', en: 'From' },
    auditTo: { zh: '\u7ed3\u675f\u65e5\u671f', en: 'To' },
    auditClearFilters: { zh: '\u6e05\u7a7a\u7b5b\u9009', en: 'Clear Filters' },
    auditCurrentObject: { zh: '\u5f53\u524d\u5bf9\u8c61', en: 'Current Object' },
    auditCurrentObjectEmpty: { zh: '\u8bf7\u5148\u9009\u62e9\u90e8\u95e8\u6216\u7528\u6237', en: 'Select a department or user first' },
    auditExportJson: { zh: '\u5bfc\u51fa JSON', en: 'Export JSON' },
    auditExportCsv: { zh: '\u5bfc\u51fa CSV', en: 'Export CSV' },
    auditExported: { zh: '\u53d8\u66f4\u8bb0\u5f55\u5df2\u5bfc\u51fa', en: 'Recent changes exported' },
    auditExportEmpty: { zh: '\u6682\u65e0\u53ef\u5bfc\u51fa\u7684\u53d8\u66f4\u8bb0\u5f55', en: 'No recent changes to export yet' },
    auditExportFailed: { zh: '\u5bfc\u51fa\u53d8\u66f4\u8bb0\u5f55\u5931\u8d25: ', en: 'Export recent changes failed: ' },
    auditEmpty: { zh: '\u6682\u65e0\u53d8\u66f4\u8bb0\u5f55', en: 'No recent changes yet' },
    auditLoadFailed: { zh: '\u52a0\u8f7d\u53d8\u66f4\u8bb0\u5f55\u5931\u8d25: ', en: 'Load recent changes failed: ' },
    auditActor: { zh: '\u64cd\u4f5c\u4eba', en: 'Actor' },
    auditTime: { zh: '\u65f6\u95f4', en: 'Time' },
    auditScope: { zh: '\u8303\u56f4', en: 'Scope' },
    auditGroupId: { zh: '\u90e8\u95e8 ID', en: 'Department ID' },
    auditEmail: { zh: '\u7528\u6237', en: 'User' },
    auditName: { zh: '\u540d\u79f0', en: 'Name' },
    auditParentId: { zh: '\u4e0a\u7ea7\u90e8\u95e8', en: 'Parent Department' },
    auditOldValue: { zh: '\u539f\u503c', en: 'Old' },
    auditNewValue: { zh: '\u65b0\u503c', en: 'New' },
    auditPolicyItems: { zh: '\u7b56\u7565\u9879', en: 'Policy Items' },
    auditActionCapabilityDeployCreate: { zh: '\u521b\u5efa\u5fc5\u88c5/\u7981\u6b62\u80fd\u529b\u5305\u4e0b\u53d1', en: 'Created managed package rollout' },
    auditActionCapabilityDeployDelete: { zh: '\u53d6\u6d88\u5fc5\u88c5/\u7981\u6b62\u80fd\u529b\u5305\u4e0b\u53d1', en: 'Removed managed package rollout' },
    auditActionCapabilityRecommendCreate: { zh: '\u521b\u5efa\u63a8\u8350\u80fd\u529b\u5305', en: 'Created recommended package' },
    auditActionCapabilityRecommendDelete: { zh: '\u53d6\u6d88\u63a8\u8350\u80fd\u529b\u5305', en: 'Removed recommended package' },
    auditActionGroupCreate: { zh: '\u521b\u5efa\u90e8\u95e8', en: 'Created department' },
    auditActionGroupRename: { zh: '\u91cd\u547d\u540d\u90e8\u95e8', en: 'Renamed department' },
    auditActionGroupDelete: { zh: '\u5220\u9664\u90e8\u95e8', en: 'Deleted department' },
    auditActionMemberAdd: { zh: '\u79fb\u5165\u6210\u5458', en: 'Moved member into department' },
    auditActionMemberRemove: { zh: '\u79fb\u9664\u6210\u5458', en: 'Removed member from department' },
    auditActionGroupPolicy: { zh: '\u66f4\u65b0\u90e8\u95e8\u7b56\u7565', en: 'Updated department policy' },
    auditActionDefaultGroup: { zh: '\u66f4\u65b0\u9ed8\u8ba4\u90e8\u95e8', en: 'Updated default department' },
    auditActionModelServiceBindings: { zh: '\u66f4\u65b0\u6a21\u578b\u670d\u52a1\u7ec4\u7ed1\u5b9a', en: 'Updated model service group bindings' },
    auditActionCentralizedOn: { zh: '\u542f\u7528\u96c6\u4e2d\u7b56\u7565', en: 'Enabled centralized policy' },
    auditActionCentralizedOff: { zh: '\u7981\u7528\u96c6\u4e2d\u7b56\u7565', en: 'Disabled centralized policy' },
    auditActionOrgOn: { zh: '\u542f\u7528\u7ec4\u7ec7\u67b6\u6784', en: 'Enabled organization structure' },
    auditActionOrgOff: { zh: '\u7981\u7528\u7ec4\u7ec7\u67b6\u6784', en: 'Disabled organization structure' },
    snapshotIdShort: { zh: '\u5feb\u7167 ID: {id}', en: 'Snapshot ID: {id}' }
  };

  function st(key, vars) {
    var entry = SECURITY_I18N[key];
    var value = entry ? (isZh() ? entry.zh : entry.en) : key;
    if (!vars) return value;
    return value.replace(/\{(\w+)\}/g, function(match, name) {
      return Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match;
    });
  }

  function policyOptionLabel(policyKey, option) {
    var labels = {
      guardrail_mode: { none: text('\u65e0', 'None'), standard: text('\u6807\u51c6', 'Standard'), strict: text('\u4e25\u683c', 'Strict') },
      sandbox_mode: { none: text('\u65e0', 'None'), basic: text('\u57fa\u7840', 'Basic'), strict: text('\u4e25\u683c', 'Strict') },
      network_level: { none: text('\u65e0', 'None'), limited: text('\u53d7\u9650', 'Limited'), full: text('\u5b8c\u5168\u5f00\u653e', 'Full') }
    };
    return labels[policyKey] && labels[policyKey][option] ? labels[policyKey][option] : option;
  }

  function ui() {
    return global.AdminUI || null;
  }

  function hint(message) {
    const helper = ui();
    return helper && typeof helper.hint === 'function'
      ? helper.hint(message)
      : '<div class="hint">' + escapeHtml(message || '') + '</div>';
  }

  function errorHint(message) {
    return '<div class="hint" style="color:var(--danger)">' + escapeHtml(message || '') + '</div>';
  }

  function escapeJsString(value) {
    return String(value || '')
      .replace(/\\/g, '\\\\')
      .replace(/'/g, "\\'")
      .replace(/\r/g, '\\r')
      .replace(/\n/g, '\\n')
      .replace(/</g, '\\x3c')
      .replace(/>/g, '\\x3e')
      .replace(/&/g, '\\x26');
  }

  function normalizeEmailKey(email) {
    return String(email || '').trim().toLowerCase();
  }

  function normalizeGroupKey(groupID) {
    return String(groupID || '').trim().toLowerCase();
  }

  function dedupeEmails(emails) {
    var seen = {};
    var items = [];
    (emails || []).forEach(function(email) {
      var key = normalizeEmailKey(email);
      if (!key || seen[key]) return;
      seen[key] = true;
      items.push(String(email || '').trim());
    });
    return items;
  }

  function dedupeStrings(values) {
    var seen = {};
    var items = [];
    (values || []).forEach(function(value) {
      var key = String(value || '').trim();
      if (!key || seen[key]) return;
      seen[key] = true;
      items.push(key);
    });
    return items;
  }

  function localizeUserStatus(status) {
    var value = String(status || '').trim().toLowerCase();
    if (!value) return st('unknown');
    var map = {
      active: 'statusActive',
      inactive: 'statusInactive',
      pending: 'statusPending',
      blocked: 'statusBlocked',
      disabled: 'statusDisabled',
      approved: 'statusApproved'
    };
    return map[value] ? st(map[value]) : String(status || '');
  }

  function dedupeUsersByEmail(users) {
    var seen = {};
    var items = [];
    (users || []).forEach(function(user) {
      if (!user) return;
      var key = normalizeEmailKey(user.email);
      if (!key || seen[key]) return;
      seen[key] = true;
      items.push(user);
    });
    return items;
  }

  function selectedAssignEmailList() {
    var selected = state().selectedAssignEmails || {};
    return Object.keys(selected).filter(function(key) { return !!selected[key]; }).map(function(key) { return selected[key]; });
  }

  function filteredAssignUsers() {
    var sec = state();
    var input = document.getElementById('assignUsersSearch');
    var query = String(input && input.value || '').trim().toLowerCase();
    return (sec.assignUsers || []).filter(function(user) {
      if (!query) return true;
      var email = String(user.email || '').toLowerCase();
      var sn = String(user.sn || '').toLowerCase();
      return email.indexOf(query) >= 0 || sn.indexOf(query) >= 0;
    });
  }

  function setAssignUserSelected(email, checked) {
    var sec = state();
    var clean = String(email || '').trim();
    var key = normalizeEmailKey(clean);
    if (!key) return;
    if (!sec.selectedAssignEmails) sec.selectedAssignEmails = {};
    if (checked) {
      sec.selectedAssignEmails[key] = clean;
    } else {
      delete sec.selectedAssignEmails[key];
    }
    sec.selectedAssignEmail = selectedAssignEmailList()[0] || '';
  }

  function renderMembersSection(children, members) {
    var sec = state();
    var pageSize = Number(sec.membersPageSize || 60);
    var totalMembers = members.length;
    var totalPages = Math.max(1, Math.ceil(totalMembers / pageSize));
    if (sec.membersPage > totalPages) sec.membersPage = totalPages;
    if (sec.membersPage < 1) sec.membersPage = 1;
    var start = (sec.membersPage - 1) * pageSize;
    var pageMembers = members.slice(start, start + pageSize);
    var html = '';
    if (children.length) {
      html += '<div style="margin-bottom:6px;font-size:11px;color:var(--muted)">' + st('subgroupLabel') + '</div>';
      html += '<div style="display:grid;gap:4px">';
      children.forEach(function(child) {
        html += '<div class="item" style="min-height:auto;padding:8px 10px;border-radius:10px;box-shadow:none">'
          + '<div style="display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;align-items:center">'
          + '<div style="font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(child.name) + '</div>'
          + '<div class="item-meta">' + String(Number(child.member_count || 0)) + '</div>'
          + '</div></div>';
      });
      html += '</div>';
    }
    if (totalMembers) {
      html += '<div style="margin:10px 0 6px;font-size:11px;color:var(--muted)">' + (st('membersLabel') + ' (' + totalMembers + ')') + '</div>';
      html += '<div style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:4px" id="secMembersGrid">';
      pageMembers.forEach(function(email, idx) {
        var absoluteIndex = start + idx + 1;
        html += '<div class="item" style="min-height:auto;padding:8px 10px;border-radius:10px;box-shadow:none">';
        html += '<div style="display:grid;grid-template-columns:minmax(0,1.5fr) auto auto;gap:8px;align-items:center">';
        html += '<div style="min-width:0"><div style="font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(email) + '</div></div>';
        html += '<div class="item-meta" style="font-size:11px">#' + absoluteIndex + '</div>';
        html += '<button class="btn-ghost" style="height:26px;font-size:11px;padding:0 10px;color:var(--danger)" data-email="' + escapeHtml(email) + '" onclick="removeSecGroupMember(this.dataset.email)">' + st('remove') + '</button>';
        html += '</div></div>';
      });
      html += '</div>';
      if (totalPages > 1) {
        var startIdx = start + 1;
        var endIdx = Math.min(start + pageMembers.length, totalMembers);
        html += '<div class="pager" style="margin-top:8px"><div class="pager-meta">' + st('pagerSummary', { page: sec.membersPage, totalPages: totalPages, start: startIdx, end: endIdx, total: totalMembers }) + '</div><div class="pager-actions"><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="changeSecMembersPage(-1)"' + (sec.membersPage <= 1 ? ' disabled' : '') + '>' + st('previous') + '</button><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="changeSecMembersPage(1)"' + (sec.membersPage >= totalPages ? ' disabled' : '') + '>' + st('next') + '</button></div></div>';
      }
    }
    return html || hint(st('noMembers'));
  }

  function applySecurityI18n() {
    _s('navSecurity', 'textContent', st('enterpriseTitle'));
    _s('navSecurityDesc', 'textContent', st('enterpriseSubtitle'));
    _s('secTitle', 'textContent', st('enterpriseTitle'));
    _s('secDesc', 'textContent', st('enterpriseSubtitle'));
    _s('secManagementTabBtn', 'textContent', st('enterpriseManagementTab'));
    _s('secAuditTabBtn', 'textContent', st('recentChangesTab'));
    _s('secReloadBtn', 'textContent', st('reload'));
    _s('secCentralizedTitle', 'textContent', st('centralizedPolicy'));
    _s('secOrgTitle', 'textContent', st('orgStructure'));
    _s('secDefaultGroupTitle', 'textContent', st('defaultGroup'));
    _s('secDefaultGroupSetBtn', 'textContent', st('set'));
    _s('secGroupTreeTitle', 'textContent', st('groupTree'));
    _s('secGlobalBtn', 'textContent', st('globalObject'));
    _s('secCtxCreate', 'textContent', st('createSubDepartment'));
    _s('secCtxRename', 'textContent', st('renameDepartment'));
    _s('secCtxAssign', 'textContent', st('moveUsersHere'));
    _s('secCtxDelete', 'textContent', st('deleteDepartment'));
    _s('secPolicySaveBtn', 'textContent', st('save'));
    _s('secMembersTitle', 'textContent', st('members'));
    _s('secMembersReloadBtn', 'textContent', st('reload'));
    _s('defaultGroupModalTitle', 'textContent', st('defaultGroup'));
    _s('defaultGroupModalDesc', 'textContent', st('chooseDefaultGroupDesc'));
    _s('defaultGroupCancelBtn', 'textContent', st('cancel'));
    _s('defaultGroupConfirmBtn', 'textContent', st('confirm'));
    _s('assignUsersModalTitle', 'textContent', st('moveUsers'));
    _s('assignUsersModalDesc', 'textContent', st('moveUsersDesc'));
    _s('assignUsersCancelBtn', 'textContent', st('cancel'));
    _s('assignUsersConfirmBtn', 'textContent', st('confirm'));
    _s('assignUsersSelectAllBtn', 'textContent', st('selectVisibleUsers'));
    _s('assignUsersSearch', 'placeholder', st('searchEmailOrSn'));
    _s('secMembersModalReloadBtn', 'textContent', st('reload'));
    _s('secMembersModalExportBtn', 'textContent', st('exportMembersCsv'));
    _s('secMembersSearch', 'placeholder', st('searchMembers'));
    _s('secMembersModalPrevBtn', 'textContent', st('previous'));
    _s('secMembersModalNextBtn', 'textContent', st('next'));
    _s('secAuditTitle', 'textContent', st('recentChanges'));
    _s('secAuditDesc', 'textContent', st('recentChangesDesc'));
    _s('secAuditReloadBtn', 'textContent', st('reload'));
    renderAuditControls();
    applySecSubTab();
    _s('secContextMenu', 'title', st('departmentActions'));
  }

  function applySecSubTab() {
    var sec = state();
    var active = sec.subTab === 'audit' ? 'audit' : 'management';
    sec.subTab = active;
    var managementPane = document.getElementById('secManagementPane');
    var auditPanel = document.getElementById('secAuditPanel');
    if (managementPane) managementPane.classList.toggle('hidden', active !== 'management');
    if (auditPanel) auditPanel.classList.toggle('hidden', active !== 'audit');
    var managementBtn = document.getElementById('secManagementTabBtn');
    var auditBtn = document.getElementById('secAuditTabBtn');
    if (managementBtn) managementBtn.className = active === 'management' ? 'btn-secondary' : 'btn-ghost';
    if (auditBtn) auditBtn.className = active === 'audit' ? 'btn-secondary' : 'btn-ghost';
  }

  function normalizeNode(raw) {
    if (!raw) return null;
    return {
      id: raw.id || '',
      name: raw.name || '',
      parent_id: raw.parent_id || '',
      member_count: Number(raw.member_count || 0),
      has_children: !!raw.has_children || !!(raw.children && raw.children.length),
      children: (raw.children || []).map(normalizeNode).filter(Boolean)
    };
  }

  function findGroupNode(nodes, id) {
    if (!nodes || !nodes.length || !id) return null;
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].id === id) return nodes[i];
      var found = findGroupNode(nodes[i].children || [], id);
      if (found) return found;
    }
    return null;
  }

  function findGroupPath(nodes, id, path) {
    path = path || [];
    if (!nodes || !nodes.length || !id) return [];
    for (var i = 0; i < nodes.length; i++) {
      var node = nodes[i];
      var nextPath = path.concat([node.id]);
      if (node.id === id) return nextPath;
      var found = findGroupPath(node.children || [], id, nextPath);
      if (found.length) return found;
    }
    return [];
  }

  function selectedGroupChainIds(groupId) {
    var sec = state();
    var id = groupId || sec.selectedGroupId;
    var path = findGroupPath(sec.groupTree || [], id, []);
    if (path.length) return path;
    return id ? [id] : [];
  }

  function rootGroupId() {
    var tree = state().groupTree || [];
    return tree[0] && tree[0].id ? tree[0].id : '';
  }

  function groupPathLabel(groupId) {
    var sec = state();
    var chain = selectedGroupChainIds(groupId);
    if (!chain.length) return '-';
    return chain.map(function(id) {
      var node = findGroupNode(sec.groupTree || [], id);
      return node && node.name ? node.name : id;
    }).join(' / ');
  }

  function groupDisplayName(groupId) {
    var node = findGroupNode(state().groupTree || [], groupId);
    if (!node) return String(groupId || '');
    return (node.name || node.id || '') + (node.id ? ' (' + node.id + ')' : '');
  }

  function policyGroupPathLabel(pathItems, fallbackGroupId) {
    var items = Array.isArray(pathItems) ? pathItems : [];
    if (!items.length) return groupPathLabel(fallbackGroupId);
    return items.map(function(item) { return item && (item.name || item.id) || ''; }).filter(Boolean).join(' / ') || groupPathLabel(fallbackGroupId);
  }

  function policyGroupPathIds(pathItems, fallbackGroupId) {
    var items = Array.isArray(pathItems) ? pathItems : [];
    if (!items.length) return selectedGroupChainIds(fallbackGroupId).join(' / ') || fallbackGroupId || '-';
    return items.map(function(item) { return item && item.id || ''; }).filter(Boolean).join(' / ') || fallbackGroupId || '-';
  }

  function replaceGroupChildren(nodes, parentID, children) {
    var parent = findGroupNode(nodes, parentID);
    if (!parent) return false;
    var sec = state();
    var prevById = {};
    (parent.children || []).forEach(function(child) {
      prevById[child.id] = child;
    });
    parent.children = (children || []).map(function(child) {
      var normalized = normalizeNode(child);
      var prev = prevById[normalized.id];
      if (prev && prev.children && prev.children.length) {
        normalized.children = prev.children;
      }
      if (prev && sec.loadedChildrenGroupIds[normalized.id]) {
        sec.loadedChildrenGroupIds[normalized.id] = true;
      }
      if (prev && sec.expandedGroupIds[normalized.id]) {
        sec.expandedGroupIds[normalized.id] = true;
      }
      return normalized;
    });
    parent.has_children = parent.children.length > 0 || !!parent.has_children;
    return true;
  }

  function renderTreeNodes(nodes, container, depth) {
    var sec = state();
    if (!container) return;
    var globalBtn = document.getElementById('secGlobalBtn');
    if (globalBtn) {
      globalBtn.classList.toggle('active', sec.selectedObjectType === 'global');
      globalBtn.textContent = st('globalObject');
    }
    if (depth === 0) container.innerHTML = '';
    if (!nodes || !nodes.length) {
      if (depth === 0) container.innerHTML = hint(st('noGroups'));
      return;
    }
    nodes.forEach(function(node) {
      var row = document.createElement('div');
      row.style.padding = '3px 8px 3px ' + (depth * 16 + 8) + 'px';
      row.style.borderRadius = '8px';
      row.style.transition = 'background .15s';
      row.style.display = 'flex';
      row.style.alignItems = 'center';
      row.style.gap = '6px';
      row.style.cursor = 'pointer';
      if (node.id === sec.selectedGroupId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }

      var toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.style.width = '18px';
      toggle.style.minWidth = '18px';
      toggle.style.height = '18px';
      toggle.style.padding = '0';
      toggle.style.border = 'none';
      toggle.style.borderRadius = '6px';
      toggle.style.background = 'transparent';
      toggle.style.color = 'var(--muted)';
      toggle.style.boxShadow = 'none';
      toggle.style.transform = 'none';
      toggle.style.cursor = node.has_children ? 'pointer' : 'default';
      if (!node.has_children) {
        toggle.textContent = '\u25cf';
        toggle.disabled = true;
      } else if (sec.loadingChildrenGroupIds[node.id]) {
        toggle.textContent = '...';
      } else {
        toggle.textContent = sec.expandedGroupIds[node.id] ? '\u25bc' : '\u25b6';
      }
      toggle.addEventListener('click', function(event) {
        event.preventDefault();
        event.stopPropagation();
        if (node.has_children) global.toggleSecGroup(node.id);
      });

      var label = document.createElement('div');
      label.style.flex = '1';
      label.innerHTML = '<span style="font-size:12px;font-weight:600">' + escapeHtml(node.name) + '</span><span style="color:var(--muted);font-size:10px;margin-left:6px">(' + String(Number(node.member_count || 0)) + ')</span>'; 
      row.appendChild(toggle);
      row.appendChild(label);
      row.addEventListener('click', function(event) {
        event.stopPropagation();
        global.selectSecGroup(node.id, node.name);
      });
      row.addEventListener('contextmenu', function(event) {
        event.preventDefault();
        event.stopPropagation();
        sec.contextGroupId = node.id;
        sec.contextGroupName = node.name;
        global.showSecContextMenu(event.clientX, event.clientY);
      });
      container.appendChild(row);

      if (sec.expandedGroupIds[node.id]) {
        if (node.children && node.children.length) {
          renderTreeNodes(node.children, container, depth + 1);
        } else if (sec.loadingChildrenGroupIds[node.id]) {
          var loading = document.createElement('div');
          loading.style.padding = '2px 8px 6px ' + ((depth + 1) * 18 + 8) + 'px';
          loading.style.color = 'var(--muted)';
          loading.style.fontSize = '12px';
          loading.textContent = st('loading');
          container.appendChild(loading);
        }
      }
    });
  }

  async function loadSettings() {
    var settings = await api('/api/admin/security/settings');
    var centralizedToggle = document.getElementById('secCentralizedToggle');
    var orgToggle = document.getElementById('secOrgToggle');
    if (centralizedToggle) centralizedToggle.checked = !!settings.centralized_security_enabled;
    if (orgToggle) orgToggle.checked = !!settings.org_structure_enabled;
    _s('secCentralizedHint', 'textContent', settings.centralized_security_enabled ? st('enabled') : st('disabled'));
    _s('secOrgHint', 'textContent', settings.org_structure_enabled ? st('enabled') : st('disabled'));
    var dgHint = settings.default_group_id || st('notSet');
    _s('secDefaultGroupHint', 'textContent', st('defaultGroupPrefix') + dgHint);
  }

  async function loadGroups() {
    var sec = state();
    var data = await api('/api/admin/security/groups/root');
    var root = normalizeNode(data.root);
    sec.groupTree = root ? [root] : [];
    sec.expandedGroupIds = {};
    sec.loadedChildrenGroupIds = {};
    sec.loadingChildrenGroupIds = {};
    if (root) {
      sec.expandedGroupIds[root.id] = true;
      await global.loadSecGroupChildren(root.id, true);
      if (!sec.selectedGroupId && sec.selectedObjectType !== 'group' && sec.selectedObjectType !== 'user') {
        global.selectSecGlobal();
        return;
      }
    }
    global._secGroupTree = sec.groupTree;
    global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
  }

  function renderAssignUsers() {
    var sec = state();
    var root = document.getElementById('assignUsersTree');
    var input = document.getElementById('assignUsersSearch');
    if (!root || !input) return;
    var query = String(input.value || '').trim().toLowerCase();
    var rows = filteredAssignUsers();
    if (!rows.length) {
      root.innerHTML = hint(query ? st('noUsersMatchSearch') : st('noUsersAvailable'));
    } else {
      root.innerHTML = rows.map(function(user) {
        var email = user.email || '';
        var key = normalizeEmailKey(email);
        var selected = !!(sec.selectedAssignEmails && sec.selectedAssignEmails[key]);
        var jsEmail = escapeJsString(email);
        return '<div class="item" style="min-height:auto;padding:8px 10px;margin-bottom:6px;border:' + (selected ? '1px solid rgba(47,128,237,.38)' : '1px solid var(--line)') + ';background:' + (selected ? 'rgba(47,128,237,.06)' : 'linear-gradient(180deg,rgba(255,255,255,.98) 0%,rgba(247,251,255,.98) 100%)') + ';cursor:pointer" onclick="selectAssignUser(\'' + jsEmail + '\')"><div style="display:flex;align-items:center;justify-content:space-between;gap:8px"><label style="display:flex;align-items:center;gap:8px;margin:0;min-width:0;cursor:pointer;flex:1" onclick="event.stopPropagation()"><input type="checkbox" style="width:16px;height:16px;flex:0 0 auto" ' + (selected ? 'checked' : '') + ' onchange="toggleAssignUser(\'' + jsEmail + '\', this.checked)"><span style="min-width:0"><span style="display:block;font-weight:600;word-break:break-all">' + escapeHtml(email) + '</span><span class="item-meta">' + escapeHtml(text('SN', 'SN')) + ': ' + escapeHtml(user.sn || '-') + ' | ' + escapeHtml(st('status')) + ': ' + escapeHtml(localizeUserStatus(user.status)) + '</span></span></label><button class="btn-ghost" type="button" style="height:26px;font-size:11px;padding:0 10px;flex:0 0 auto" onclick="event.stopPropagation();selectAssignUser(\'' + jsEmail + '\')">' + escapeHtml(selected ? st('remove') : st('move')) + '</button></div></div>';
      }).join('');
    }
    var selectedCount = selectedAssignEmailList().length;
    var visibleSelectedCount = rows.filter(function(user) { return !!(sec.selectedAssignEmails && sec.selectedAssignEmails[normalizeEmailKey(user.email)]); }).length;
    var countText = st('showingUsers', { visible: rows.length, total: sec.assignUsers.length });
    if (selectedCount) countText += ' | ' + st('selectedUsers', { count: selectedCount });
    _s('assignUsersCount', 'textContent', countText);
    var selectAllBtn = document.getElementById('assignUsersSelectAllBtn');
    if (selectAllBtn) {
      selectAllBtn.textContent = rows.length > 0 && visibleSelectedCount === rows.length ? st('clearVisibleUsers') : st('selectVisibleUsers');
      selectAllBtn.disabled = rows.length === 0;
    }
  }

  async function loadAssignableUsers() {
    var sec = state();
    var userReq = api('/api/admin/users');
    var memberReq = sec.assignGroupId
      ? api('/api/admin/security/groups/' + encodeURIComponent(sec.assignGroupId) + '/members')
      : Promise.resolve({ members: [] });
    var results = await Promise.all([userReq, memberReq]);
    var data = results[0] || {};
    var memberData = results[1] || {};
    sec.userDirectoryCache = dedupeUsersByEmail(data.users || []);
    var currentMemberKeys = {};
    dedupeEmails(memberData.members || []).forEach(function(email) {
      currentMemberKeys[normalizeEmailKey(email)] = true;
    });
    sec.assignUsers = dedupeUsersByEmail((sec.userDirectoryCache || []).filter(function(user) {
      return !!(user && user.email) && !currentMemberKeys[normalizeEmailKey(user.email)];
    }));
    renderAssignUsers();
  }


  function secPolicyKeys() {
    return [
      { key: 'file_outbound_enabled', label: st('fileOutbound'), type: 'bool' },
      { key: 'image_outbound_enabled', label: st('imageOutbound'), type: 'bool' },
      { key: 'gossip_enabled', label: st('gossip'), type: 'bool' },
      { key: 'yolo_mode_allowed', label: st('yoloMode'), type: 'bool' },
      { key: 'smart_route_enabled', label: st('smartRoute'), type: 'bool' },
      { key: 'guardrail_mode', label: st('guardrailMode'), type: 'select', options: ['none', 'standard', 'strict'] },
      { key: 'sandbox_mode', label: st('sandboxMode'), type: 'select', options: ['none', 'basic', 'strict'] },
      { key: 'network_level', label: st('networkLevel'), type: 'select', options: ['none', 'limited', 'full'] }
    ];
  }

  function setCurrentPolicyExport(payload) {
    state().currentPolicyExport = payload || null;
  }

  function safeExportName(value, fallback) {
    return String(value || fallback || 'policy').replace(/[^a-zA-Z0-9._-]+/g, '-').replace(/^-+|-+$/g, '') || String(fallback || 'policy');
  }

  function snapshotObjectKey(payload) {
    return safeExportName(payload && (payload.user_email || payload.group_id || payload.object_id || payload.object_name || payload.object_type), 'snapshot');
  }

  function currentAdminSnapshot() {
    try {
      var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
      if (!profile) return null;
      return { username: profile.username || '', email: profile.email || '' };
    } catch (_) {
      return null;
    }
  }

  function stableSnapshotStringify(value) {
    if (value === null || typeof value !== 'object') return JSON.stringify(value);
    if (Array.isArray(value)) return '[' + value.map(stableSnapshotStringify).join(',') + ']';
    return '{' + Object.keys(value).sort().filter(function(key) {
      return key !== 'snapshot_checksum';
    }).map(function(key) {
      return JSON.stringify(key) + ':' + stableSnapshotStringify(value[key]);
    }).join(',') + '}';
  }

  function snapshotChecksum(payload) {
    var input = stableSnapshotStringify(payload || {});
    var hash = 2166136261;
    for (var i = 0; i < input.length; i += 1) {
      hash ^= input.charCodeAt(i);
      hash = Math.imul(hash, 16777619) >>> 0;
    }
    return ('00000000' + hash.toString(16)).slice(-8);
  }

  function normalizeSnapshotPayload(payload, type) {
    payload = payload || {};
    payload.snapshot_schema_version = payload.snapshot_schema_version || 1;
    payload.snapshot_type = payload.snapshot_type || type || 'enterprise_snapshot';
    payload.exported_from = payload.exported_from || 'maclaw_hub_enterprise_management';
    payload.exported_by = payload.exported_by || currentAdminSnapshot();
    payload.exported_at = payload.exported_at || new Date().toISOString();
    payload.snapshot_id = payload.snapshot_id || [payload.snapshot_type, safeExportName(payload.object_type, 'object'), snapshotObjectKey(payload), payload.exported_at.replace(/[:.]/g, '-').slice(0, 19)].join(':');
    payload.snapshot_checksum_algorithm = payload.snapshot_checksum_algorithm || 'fnv1a32-stable-json';
    payload.snapshot_checksum = snapshotChecksum(payload);
    return payload;
  }

  function snapshotExportStamp(payload) {
    return String(payload && payload.exported_at || new Date().toISOString()).replace(/[:.]/g, '-').slice(0, 19);
  }

  function showSnapshotExportToast(labelKey, payload) {
    var id = payload && payload.snapshot_id ? String(payload.snapshot_id) : '';
    showToast(st(labelKey) + (id ? ' - ' + st('snapshotIdShort', { id: id }) : ''), 'success');
  }

  function renderPolicyExportButton() {
    return '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="exportSecCurrentPolicy()">' + escapeHtml(st('exportPolicyJson')) + '</button>';
  }

  function setCurrentModelServiceExport(payload) {
    state().currentModelServiceExport = payload || null;
  }

  function renderModelServiceExportButton() {
    return '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="exportSecCurrentModelService()">' + escapeHtml(st('exportModelServiceJson')) + '</button>';
  }

  function renderPolicySourceSummary(items) {
    var total = 0;
    var overrides = 0;
    var inherited = 0;
    secPolicyKeys().forEach(function(pk) {
      total += 1;
      var item = items && items[pk.key] || {};
      if ((item.source || 'inherited') === 'self') overrides += 1;
      else inherited += 1;
    });
    return '<div class="grid3" style="margin:10px 0"><div class="metric"><label>' + escapeHtml(st('policyOverrides')) + '</label><strong>' + String(overrides) + '</strong><span>' + escapeHtml(st('custom')) + '</span></div><div class="metric"><label>' + escapeHtml(st('policyInherited')) + '</label><strong>' + String(inherited) + '</strong><span>' + escapeHtml(st('inheritPolicy')) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityTotal')) + '</label><strong>' + String(total) + '</strong><span>' + escapeHtml(st('effectivePolicy')) + '</span></div></div>';
  }

  function renderPolicyRows(items, editable) {
    items = items || {};
    var html = '';
    secPolicyKeys().forEach(function(pk) {
      var item = items[pk.key] || {};
      var value = item.value;
      var source = item.source || 'inherited';
      var inherited = source !== 'self';
      var sourceName = item.source_name || item.source_group || '';
      var sourceTag = source === 'self'
        ? '<span style="color:var(--accent);font-size:11px;margin-left:6px">' + st('custom') + '</span>'
        : '<span style="color:var(--muted);font-size:11px;margin-left:6px">' + st('inheritedFrom') + escapeHtml(sourceName) + '</span>';
      html += '<div style="display:grid;grid-template-columns:minmax(160px,1.2fr) auto;gap:8px;align-items:center;padding:7px 0;border-bottom:1px solid var(--line)">';
      html += '<div style="font-size:12px;font-weight:600">' + escapeHtml(pk.label) + sourceTag + '</div>';
      if (!editable) {
        html += '<div class="item-meta" style="justify-self:end">' + escapeHtml(pk.type === 'bool' ? (value ? st('enabled') : st('disabled')) : policyOptionLabel(pk.key, value)) + '</div>';
      } else {
        html += '<div style="display:flex;align-items:center;gap:10px;justify-self:end">';
        html += '<label class="item-meta" style="display:flex;align-items:center;gap:4px;cursor:pointer;white-space:nowrap"><input type="checkbox" data-policy-inherit-key="' + pk.key + '" onchange="toggleSecPolicyInherit(this)" ' + (inherited ? 'checked' : '') + '> ' + escapeHtml(st('inheritPolicy')) + '</label>';
        if (pk.type === 'bool') {
          html += '<label style="cursor:pointer"><input type="checkbox" data-policy-key="' + pk.key + '" data-policy-type="bool" ' + (value ? 'checked' : '') + ' ' + (inherited ? 'disabled' : '') + '></label>';
        } else {
          html += '<select data-policy-key="' + pk.key + '" data-policy-type="select" style="font-size:11px;padding:2px 8px;border-radius:6px;border:1px solid var(--line)" ' + (inherited ? 'disabled' : '') + '>';
          pk.options.forEach(function(option) {
            html += '<option value="' + option + '"' + (value === option ? ' selected' : '') + '>' + escapeHtml(policyOptionLabel(pk.key, option)) + '</option>';
          });
          html += '</select>';
        }
        html += '</div>';
      }
      html += '</div>';
    });
    return html;
  }

  function userPolicyItemsFromGroupView(policy, groupView, email) {
    var items = {};
    var sourceGroupName = policyGroupPathLabel(groupView && groupView.group_path || [], state().selectedGroupId) || state().selectedGroupName || groupDisplayName(state().selectedGroupId) || email;
    secPolicyKeys().forEach(function(pk) {
      var fromGroup = groupView && groupView.items && groupView.items[pk.key];
      if (fromGroup) {
        items[pk.key] = {
          value: fromGroup.value,
          source: 'inherited',
          source_group: fromGroup.source_group || state().selectedGroupId || '',
          source_name: fromGroup.source === 'self' ? sourceGroupName : (fromGroup.source_name || fromGroup.source_group || sourceGroupName)
        };
      } else {
        items[pk.key] = { value: policy && policy[pk.key], source: 'inherited', source_name: sourceGroupName };
      }
    });
    return items;
  }

  function serviceGroupKey(id) {
    return String(id || '').trim().toLowerCase();
  }

  function serviceGroupMap(cache) {
    var map = {};
    (cache && cache.model_service_groups || []).forEach(function(group) {
      if (group && group.id) map[serviceGroupKey(group.id)] = group;
    });
    return map;
  }

  function knownServiceGroupIds(ids, cache) {
    var map = serviceGroupMap(cache);
    var seen = {};
    var known = [];
    dedupeStrings(ids || []).forEach(function(id) {
      var key = serviceGroupKey(id);
      var group = map[key];
      if (!group || seen[key]) return;
      seen[key] = true;
      known.push(String(group.id || id));
    });
    return known;
  }

  function serviceGroupChips(ids, cache) {
    ids = dedupeStrings(ids || []);
    var map = serviceGroupMap(cache);
    if (!ids.length) return '<span class="item-meta">-</span>';
    return ids.map(function(id) {
      var group = map[serviceGroupKey(id)] || null;
      var label = group ? (group.name || group.id) : id;
      return '<span class="badge info" style="margin:2px 4px 2px 0;text-transform:none;letter-spacing:0">' + escapeHtml(label) + '</span>';
    }).join('');
  }

  function serviceGroupDetails(ids, cache) {
    var map = serviceGroupMap(cache);
    return knownServiceGroupIds(ids || [], cache).map(function(id) {
      var group = map[serviceGroupKey(id)] || {};
      return {
        id: String(group.id || id),
        name: group.name || group.id || id,
        access_policy: group.access_policy || '',
        models: Array.isArray(group.models) ? group.models : []
      };
    });
  }

  async function loadLLMServiceCache() {
    var sec = state();
    if (sec.llmServiceCache) return sec.llmServiceCache;
    sec.llmServiceCache = await api('/api/admin/llm/services?include_cards=false');
    return sec.llmServiceCache;
  }

  async function loadUserDirectoryCache() {
    var sec = state();
    if (sec.userDirectoryCache) return sec.userDirectoryCache;
    var data = await api('/api/admin/users');
    sec.userDirectoryCache = dedupeUsersByEmail(data && data.users || []);
    return sec.userDirectoryCache;
  }

  function findUserDirectoryEntry(email, users) {
    var key = normalizeEmailKey(email);
    var list = users || state().userDirectoryCache || [];
    for (var i = 0; i < list.length; i += 1) {
      if (normalizeEmailKey(list[i] && list[i].email) === key) return list[i];
    }
    return null;
  }

  function userDirectoryBoolLabel(value) {
    return value ? st('enabled') : st('disabled');
  }

  async function loadCapabilityCache() {
    var sec = state();
    if (sec.capabilityCache) return sec.capabilityCache;
    var cache = { capabilities: [], deployments: [], recommendations: [], inventory: [] };
    try {
      var results = await Promise.all([
        api('/api/capabilities').catch(function() { return { items: [] }; }),
        api('/api/capabilities/managed-deployments').catch(function() { return { items: [] }; }),
        api('/api/capabilities/recommended').catch(function() { return { items: [] }; })
      ]);
      cache.capabilities = Array.isArray(results[0].items) ? results[0].items : [];
      cache.deployments = Array.isArray(results[1].items) ? results[1].items : [];
      cache.recommendations = Array.isArray(results[2].items) ? results[2].items : [];
    } catch (_) {}
    sec.capabilityCache = cache;
    return cache;
  }

  async function loadUserCapabilityInventory(email) {
    try {
      var data = await api('/api/admin/capability-market/users/' + encodeURIComponent(email) + '/inventory');
      return Array.isArray(data.items) ? data.items : [];
    } catch (_) {
      return [];
    }
  }

  async function loadUserEffectiveCapabilityPolicies(email) {
    try {
      var data = await api('/api/admin/capability-market/users/' + encodeURIComponent(email) + '/effective-policies');
      return data || { items: [] };
    } catch (_) {
      return null;
    }
  }

  async function loadUserCapabilityCompliance(email) {
    try {
      var sec = state();
      var params = [];
      if (sec.capabilityComplianceStatusFilter) params.push('status=' + encodeURIComponent(sec.capabilityComplianceStatusFilter));
      if (sec.capabilityIncludeUnmanaged === false) params.push('include_unmanaged=false');
      if (Number(sec.capabilityStaleAfterHours || 0) > 0) params.push('stale_after_hours=' + encodeURIComponent(String(Number(sec.capabilityStaleAfterHours || 168))));
      var data = await api('/api/admin/capability-market/users/' + encodeURIComponent(email) + '/compliance' + (params.length ? '?' + params.join('&') : ''));
      return data || { items: [], summary: {} };
    } catch (_) {
      return null;
    }
  }

  async function loadGroupEffectiveCapabilityPolicies(groupId) {
    try {
      var data = await api('/api/admin/capability-market/groups/' + encodeURIComponent(groupId) + '/effective-policies');
      return data || { items: [] };
    } catch (_) {
      return null;
    }
  }

  function groupServiceBinding(groupID, cache) {
    var bindings = cache && cache.group_bindings || [];
    var ids = [];
    var foundGroupID = '';
    for (var i = 0; i < bindings.length; i += 1) {
      if (normalizeGroupKey(bindings[i].group_id) !== normalizeGroupKey(groupID)) continue;
      foundGroupID = foundGroupID || bindings[i].group_id;
      ids = ids.concat(bindings[i].service_group_ids || []);
    }
    ids = knownServiceGroupIds(ids, cache);
    return ids.length ? { group_id: foundGroupID || groupID, service_group_ids: ids } : null;
  }

  function effectiveGroupServiceBinding(groupID, cache) {
    var chain = selectedGroupChainIds(groupID).slice().reverse();
    for (var i = 0; i < chain.length; i += 1) {
      var binding = groupServiceBinding(chain[i], cache);
      if (binding && binding.service_group_ids && binding.service_group_ids.length) return binding;
    }
    return null;
  }

  function userServiceBinding(email, cache) {
    var key = normalizeEmailKey(email);
    var bindings = cache && cache.user_bindings || [];
    var ids = [];
    var foundEmail = '';
    for (var i = 0; i < bindings.length; i += 1) {
      if (normalizeEmailKey(bindings[i].email) !== key) continue;
      foundEmail = foundEmail || bindings[i].email;
      ids = ids.concat(bindings[i].service_group_ids || []);
    }
    ids = knownServiceGroupIds(ids, cache);
    return ids.length ? { email: foundEmail || email, service_group_ids: ids } : null;
  }



  function serviceGroupMultiSelectOptions(selected, cache) {
    var selectedMap = {};
    dedupeStrings(selected || []).forEach(function(id) { selectedMap[serviceGroupKey(id)] = true; });
    return (cache && cache.model_service_groups || []).map(function(group) {
      if (!group || !group.id) return '';
      var id = String(group.id || '');
      return '<option value="' + escapeHtml(id) + '"' + (selectedMap[serviceGroupKey(id)] ? ' selected' : '') + '>' + escapeHtml((group.name || id) + ' (' + id + ')') + '</option>';
    }).join('');
  }

  function selectedOptions(select) {
    if (!select) return [];
    return Array.prototype.slice.call(select.options || []).filter(function(option) { return option.selected && option.value; }).map(function(option) { return option.value; });
  }

  function llmServiceSavePayload(cache) {
    var payload = Object.assign({}, cache || {});
    delete payload.cards;
    delete payload.grants;
    delete payload.user_diagnostic;
    delete payload.provider_link_issues;
    delete payload.available_models;
    delete payload.expose_api_base_url;
    delete payload.expose_base_url;
    delete payload.expose_models_url;
    return payload;
  }

  async function saveLLMServiceCache(cache) {
    await api('/api/admin/llm/services', { method: 'PUT', body: JSON.stringify(llmServiceSavePayload(cache)) });
    state().llmServiceCache = null;
    return loadLLMServiceCache();
  }

  global.saveSecGroupModelService = async function saveSecGroupModelService() {
    var sec = state();
    if (!sec.selectedGroupId) return;
    var select = document.getElementById('secGroupModelServiceSelect');
    var selected = selectedOptions(select);
    var cache = await loadLLMServiceCache();
    cache.group_bindings = (cache.group_bindings || []).filter(function(binding) { return normalizeGroupKey(binding.group_id) !== normalizeGroupKey(sec.selectedGroupId); });
    if (selected.length) cache.group_bindings.push({ group_id: sec.selectedGroupId, service_group_ids: selected });
    try {
      await saveLLMServiceCache(cache);
      showToast(st('policySaved'), 'success');
      global.reloadSecAuditLogs();
      global.loadSecGroupPolicy(sec.selectedGroupId);
    } catch (err) {
      showToast(st('savePolicyFailed') + err.message, 'error');
    }
  };

  global.saveSecGlobalModelService = async function saveSecGlobalModelService() {
    var select = document.getElementById('secGlobalModelServiceSelect');
    var selected = selectedOptions(select);
    var cache = await loadLLMServiceCache();
    cache.global_service_group_ids = selected;
    try {
      await saveLLMServiceCache(cache);
      showToast(st('policySaved'), 'success');
      global.reloadSecAuditLogs();
      global.selectSecGlobal();
    } catch (err) {
      showToast(st('savePolicyFailed') + err.message, 'error');
    }
  };

  global.saveSecUserModelService = async function saveSecUserModelService() {
    var sec = state();
    var email = sec.selectedUserEmail;
    if (!email) return;
    var select = document.getElementById('secUserModelServiceSelect');
    var selected = selectedOptions(select);
    var cache = await loadLLMServiceCache();
    cache.user_bindings = (cache.user_bindings || []).filter(function(binding) { return normalizeEmailKey(binding.email) !== normalizeEmailKey(email); });
    if (selected.length) cache.user_bindings.push({ email: email, service_group_ids: selected });
    try {
      await saveLLMServiceCache(cache);
      showToast(st('policySaved'), 'success');
      global.reloadSecAuditLogs();
      global.selectSecUser(email);
    } catch (err) {
      showToast(st('savePolicyFailed') + err.message, 'error');
    }
  };
  function renderModelServiceForGroup(groupID, cache) {
    var directBinding = groupServiceBinding(groupID, cache);
    var effectiveBinding = effectiveGroupServiceBinding(groupID, cache);
    var selected = directBinding && directBinding.service_group_ids && directBinding.service_group_ids.length ? dedupeStrings(directBinding.service_group_ids) : [];
    var inherited = effectiveBinding && normalizeGroupKey(effectiveBinding.group_id) !== normalizeGroupKey(groupID) ? dedupeStrings(effectiveBinding.service_group_ids || []) : [];
    var globalIds = knownServiceGroupIds(cache.global_service_group_ids || [], cache);
    var defaultIds = knownServiceGroupIds(cache.default_new_user_service_groups || [], cache);
    var ids = selected.length ? selected : (inherited.length ? inherited : (globalIds.length ? globalIds : defaultIds));
    var source = selected.length ? st('sourceCurrentGroupBinding') : (inherited.length ? st('sourceInheritedGroupBinding') : (globalIds.length ? st('sourceGlobalBinding') : st('sourceGlobalFallback')));
    var inheritedHint = inherited.length ? ('<div class="item-meta" style="margin-top:6px">' + escapeHtml(st('inheritedFrom') + groupDisplayName(effectiveBinding.group_id || '')) + '</div>') : '';
    setCurrentModelServiceExport({ object_type: 'group', group_id: groupID, group_name: groupDisplayName(groupID), source: source, effective_service_group_ids: ids, effective_service_groups: serviceGroupDetails(ids, cache), direct_service_group_ids: selected, inherited_group_id: inherited.length && effectiveBinding ? effectiveBinding.group_id : '', inherited_service_group_ids: inherited, global_service_group_ids: globalIds, default_new_user_service_group_ids: defaultIds, exported_at: new Date().toISOString() });
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('modelServiceGroups')) + '</div><div class="item-meta">' + escapeHtml(source) + '</div></div><div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end">' + renderModelServiceExportButton() + '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="saveSecGroupModelService()">' + escapeHtml(st('save')) + '</button></div></div><div style="margin-top:8px">' + serviceGroupChips(ids, cache) + '</div>' + inheritedHint + '<div style="margin-top:10px"><label>' + escapeHtml(st('modelServiceDirectOverride')) + '</label><select id="secGroupModelServiceSelect" multiple size="4" style="min-height:92px">' + serviceGroupMultiSelectOptions(selected, cache) + '</select><div class="item-meta" style="margin-top:6px">' + escapeHtml(st('modelServiceInheritHint')) + '</div></div></div>';
  }

  function renderModelServiceForGlobal(cache) {
    var selected = knownServiceGroupIds(cache.global_service_group_ids || [], cache);
    var fallback = knownServiceGroupIds(cache.default_new_user_service_groups || [], cache);
    var shown = selected.length ? selected : fallback;
    var source = selected.length ? st('sourceGlobalBinding') : st('sourceGlobalFallback');
    setCurrentModelServiceExport({ object_type: 'global', source: source, effective_service_group_ids: shown, effective_service_groups: serviceGroupDetails(shown, cache), global_service_group_ids: selected, default_new_user_service_group_ids: fallback, exported_at: new Date().toISOString() });
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('modelServiceGroups')) + '</div><div class="item-meta">' + escapeHtml(source) + '</div></div><div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end">' + renderModelServiceExportButton() + '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="saveSecGlobalModelService()">' + escapeHtml(st('save')) + '</button></div></div><div style="margin-top:8px">' + serviceGroupChips(shown, cache) + '</div><div style="margin-top:10px"><label>' + escapeHtml(st('modelServiceGroups')) + '</label><select id="secGlobalModelServiceSelect" multiple size="4" style="min-height:92px">' + serviceGroupMultiSelectOptions(selected, cache) + '</select><div class="item-meta" style="margin-top:6px">' + escapeHtml(st('sourceGlobalFallback') + ': ' + ((fallback || []).join(', ') || '-')) + '</div></div></div>';
  }

  function formatSecNumber(value) {
    var n = Number(value || 0);
    if (!isFinite(n)) n = 0;
    return String(Math.round(n * 100) / 100);
  }

  function compactDateTime(value) {
    var textValue = String(value || '').trim();
    if (!textValue) return '-';
    return textValue.replace('T', ' ').replace('Z', ' UTC');
  }

  function renderUserModelServiceUsage(status) {
    status = status || {};
    var available = formatSecNumber(status.credits_available || status.credits_remaining || 0);
    var remaining = formatSecNumber(status.credits_remaining || 0);
    var used = formatSecNumber(status.credits_used || 0);
    var total = formatSecNumber(status.credits_total || 0);
    var grants = Array.isArray(status.credit_grants) ? status.credit_grants : (Array.isArray(status.active_grants) ? status.active_grants : []);
    var expires = status.effective_expires_at || status.nearest_expires_at || '';
    return '<div class="grid4" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('credits')) + '</label><strong>' + escapeHtml(available) + '</strong><span>' + escapeHtml(st('creditsRemaining') + ': ' + remaining) + '</span></div><div class="metric"><label>' + escapeHtml(st('creditsUsed')) + '</label><strong>' + escapeHtml(used) + '</strong><span>' + escapeHtml(st('creditsTotal') + ': ' + total) + '</span></div><div class="metric"><label>' + escapeHtml(st('effectiveExpiresAt')) + '</label><strong style="font-size:13px">' + escapeHtml(compactDateTime(expires)) + '</strong><span>' + escapeHtml(st('modelServiceGroups')) + ': ' + String((status.service_group_ids || []).length) + '</span></div><div class="metric"><label>' + escapeHtml(st('activeGrants')) + '</label><strong>' + String(grants.length || 0) + '</strong><span>' + escapeHtml(st('serviceAccess') + ': ' + (status.active ? st('serviceAccessOn') : st('serviceAccessOff'))) + '</span></div></div>';
  }

  function renderUserModelServiceEvidence(diag, cache) {
    diag = diag || {};
    var groupNames = (diag.resolved_security_group_ids || []).map(function(id) { return groupDisplayName(id); }).join(', ') || '-';
    var matched = (diag.matched_group_bindings || []).map(function(binding) {
      return groupDisplayName(binding.group_id) + ': ' + dedupeStrings(binding.service_group_ids || []).join(', ');
    }).join(' | ') || '-';
    var direct = (diag.direct_user_bindings || []).map(function(binding) {
      return serviceGroupChips(binding.service_group_ids || [], cache);
    }).join(' ') || '<span class="item-meta">-</span>';
    return '<div class="grid3" style="margin-top:10px"><div><label>' + escapeHtml(st('resolvedSecurityGroups')) + '</label><div class="item-meta">' + escapeHtml(groupNames) + '</div></div><div><label>' + escapeHtml(st('matchedBindings')) + '</label><div class="item-meta">' + escapeHtml(matched) + '</div></div><div><label>' + escapeHtml(st('modelServiceDirectOverride')) + '</label><div class="item-meta">' + direct + '</div></div></div>';
  }

  function renderModelServiceForUser(email, cache, diag) {
    var binding = userServiceBinding(email, cache);
    var selected = binding && binding.service_group_ids && binding.service_group_ids.length ? dedupeStrings(binding.service_group_ids) : [];
    var status = diag && diag.service_status || {};
    var globalIds = knownServiceGroupIds(cache.global_service_group_ids || [], cache);
    var defaultIds = knownServiceGroupIds(cache.default_new_user_service_groups || [], cache);
    var ids = (status.service_group_ids && status.service_group_ids.length) ? knownServiceGroupIds(status.service_group_ids, cache) : (selected.length ? selected : (globalIds.length ? globalIds : defaultIds));
    var source = selected.length ? st('sourceUserBinding') : ((diag && diag.matched_group_bindings && diag.matched_group_bindings.length) ? st('sourceGroupBinding') : (globalIds.length ? st('sourceGlobalBinding') : st('sourceGlobalFallback')));
    var models = (status.available_models || []).join(', ') || '-';
    var inactive = (status.inactive_reasons || []).join(', ');
    setCurrentModelServiceExport({ object_type: 'user', user_email: email, source: source, effective_service_group_ids: ids, effective_service_groups: serviceGroupDetails(ids, cache), direct_service_group_ids: selected, global_service_group_ids: globalIds, default_new_user_service_group_ids: defaultIds, diagnostic: diag || null, service_status: status, exported_at: new Date().toISOString() });
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('modelServiceGroups')) + '</div><div class="item-meta">' + escapeHtml(source) + '</div></div><div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end">' + renderModelServiceExportButton() + '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="saveSecUserModelService()">' + escapeHtml(st('save')) + '</button></div></div><div style="margin-top:8px"><label>' + escapeHtml(st('modelServiceEffective')) + '</label><div style="margin-top:4px">' + serviceGroupChips(ids, cache) + '</div></div><div style="margin-top:10px"><label>' + escapeHtml(st('modelServiceDirectOverride')) + '</label><select id="secUserModelServiceSelect" multiple size="4" style="min-height:92px">' + serviceGroupMultiSelectOptions(selected, cache) + '</select><div class="item-meta" style="margin-top:6px">' + escapeHtml(st('modelServiceInheritHint')) + '</div></div><div class="grid2" style="margin-top:10px"><div><label>' + escapeHtml(st('modelRoutes')) + '</label><div class="item-meta">' + escapeHtml(models) + '</div></div><div><label>' + escapeHtml(st('defaultModel')) + '</label><div class="item-meta mono">' + escapeHtml(status.default_model || 'auto') + '</div></div></div>' + renderUserModelServiceUsage(status) + renderUserModelServiceEvidence(diag, cache) + (inactive ? '<div class="hint" style="margin-top:8px">' + escapeHtml(st('inactiveReasons') + ': ' + inactive) + '</div>' : '') + '</div>';
  }

  function machineDisplayName(item) {
    return item && (item.alias || item.name || item.machine_id) || '-';
  }

  function machineRuntimeLabel(item) {
    item = item || {};
    return [item.platform || '', item.hostname || '', item.app_version || ''].filter(Boolean).join(' / ') || '-';
  }

  function renderUserDeviceSummary(machines) {
    machines = Array.isArray(machines) ? machines : [];
    var online = machines.filter(function(item) { return !!(item && item.online); }).length;
    var lastSeen = machines.map(function(item) { return item && item.last_seen_at || ''; }).filter(Boolean).sort().pop() || '-';
    var activeSessions = machines.reduce(function(sum, item) { return sum + Number(item && item.active_sessions || 0); }, 0);
    var rows = machines.slice(0, 6).map(function(item) {
      var status = item && item.online ? st('statusActive') : st('statusInactive');
      return '<div class="row" style="grid-template-columns:minmax(150px,1.1fr) minmax(140px,1fr) .52fr minmax(120px,.8fr)"><div style="min-width:0"><strong style="display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(machineDisplayName(item)) + '</strong><div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item && item.machine_id || '-') + '</div></div><div class="item-meta">' + escapeHtml(machineRuntimeLabel(item)) + '</div><div><span class="badge ' + (item && item.online ? 'ok' : 'warn') + '">' + escapeHtml(status) + '</span></div><div class="item-meta">' + escapeHtml(item && item.last_seen_at || '-') + '</div></div>';
    }).join('');
    var more = machines.length > 6 ? '<div class="item-meta" style="margin-top:8px">+' + String(machines.length - 6) + '</div>' : '';
    var table = rows ? ('<div class="table" style="gap:6px;margin-top:10px"><div class="row header" style="grid-template-columns:minmax(150px,1.1fr) minmax(140px,1fr) .52fr minmax(120px,.8fr)"><div>' + escapeHtml(st('deviceTotalCount')) + '</div><div>' + escapeHtml(st('deviceRuntime')) + '</div><div>' + escapeHtml(st('status')) + '</div><div>' + escapeHtml(st('deviceLastSeen')) + '</div></div>' + rows + '</div>' + more) : '<div class="hint" style="margin-top:10px">' + escapeHtml(st('deviceNoMachines')) + '</div>';
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-title">' + escapeHtml(st('deviceSummary')) + '</div><div class="grid3" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('deviceTotalCount')) + '</label><strong>' + String(machines.length) + '</strong><span>' + escapeHtml(st('deviceSummary')) + '</span></div><div class="metric"><label>' + escapeHtml(st('deviceOnlineCount')) + '</label><strong>' + String(online) + '</strong><span>' + escapeHtml(st('statusActive')) + '</span></div><div class="metric"><label>' + escapeHtml(st('deviceActiveSessions')) + '</label><strong>' + String(activeSessions) + '</strong><span>' + escapeHtml(st('deviceLastSeen') + ': ' + lastSeen) + '</span></div></div>' + table + '</div>';
  }

  function renderCapabilityPackages() {
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-title">' + escapeHtml(st('capabilityPackages')) + '</div><div class="hint" style="margin-top:8px">' + escapeHtml(st('noCapabilityPackages')) + '</div></div>';
  }

  function capabilityMap(cache) {
    var map = {};
    (cache && cache.capabilities || []).forEach(function(item) {
      if (!item) return;
      var id = String(item.id || item.capability_ref || '').trim();
      if (id) map[id] = item;
    });
    return map;
  }

  function parseScope(scopeJSON) {
    if (!scopeJSON) return {};
    if (typeof scopeJSON === 'object') return scopeJSON;
    try { return JSON.parse(String(scopeJSON || '{}')); } catch (_) { return {}; }
  }

  function scopeMatches(scope, objectType, objectId) {
    return scopeSpecificity(scope, objectType, objectId) >= 0;
  }

  function scopeSpecificity(scope, objectType, objectId) {
    scope = scope || {};
    var type = String(scope.type || scope.scope || '').trim().toLowerCase();
    var ids = [];
    if (Array.isArray(scope.group_ids)) ids = ids.concat(scope.group_ids);
    if (Array.isArray(scope.user_emails)) ids = ids.concat(scope.user_emails);
    if (scope.group_id) ids.push(scope.group_id);
    if (scope.user_email) ids.push(scope.user_email);
    if (type === 'global' || (!type && !ids.length)) return (objectType === 'global' || objectType === 'group' || objectType === 'user') ? 0 : -1;
    if (objectType === 'global') return -1;
    if (objectType === 'group') {
      if (type !== 'group' && type !== 'department') return -1;
      var groupChain = selectedGroupChainIds(objectId).map(String);
      var bestGroupIndex = -1;
      ids.map(String).forEach(function(id) { bestGroupIndex = Math.max(bestGroupIndex, groupChain.indexOf(id)); });
      return bestGroupIndex >= 0 ? 100 + bestGroupIndex : -1;
    }
    if (objectType === 'user') {
      if (type === 'user') return ids.map(function(v) { return normalizeEmailKey(v); }).indexOf(normalizeEmailKey(objectId)) >= 0 ? 1000 : -1;
      if (type === 'group' || type === 'department') {
        var userGroupChain = selectedGroupChainIds(state().selectedGroupId).map(String);
        var bestUserGroupIndex = -1;
        ids.map(String).forEach(function(id) { bestUserGroupIndex = Math.max(bestUserGroupIndex, userGroupChain.indexOf(id)); });
        return bestUserGroupIndex >= 0 ? 100 + bestUserGroupIndex : -1;
      }
    }
    return -1;
  }

  function capabilityPolicyWeight(policy) {
    policy = String(policy || '').toLowerCase();
    if (policy === 'blocked') return 3;
    if (policy === 'required') return 2;
    return 1;
  }

  function effectiveCapabilityRows(rows) {
    var byCapability = {};
    (rows || []).forEach(function(row) {
      var key = String(row.item && row.item.capability_ref || '');
      if (!key) return;
      var existing = byCapability[key];
      if (!existing || Number(row.specificity || 0) > Number(existing.specificity || 0) || (Number(row.specificity || 0) === Number(existing.specificity || 0) && capabilityPolicyWeight(row.policy) > capabilityPolicyWeight(existing.policy))) {
        byCapability[key] = row;
      }
    });
    return Object.keys(byCapability).map(function(key) { return byCapability[key]; }).sort(function(a, b) {
      return Number(b.specificity || 0) - Number(a.specificity || 0) || capabilityPolicyWeight(b.policy) - capabilityPolicyWeight(a.policy) || String(a.item.capability_ref || '').localeCompare(String(b.item.capability_ref || ''));
    });
  }

  function scopeLabel(scope, objectType) {
    scope = scope || {};
    var type = String(scope.type || scope.scope || '').trim().toLowerCase();
    if (type === 'user') return st('capabilitySourceUser');
    if (type === 'group' || type === 'department') return st('capabilitySourceGroup');
    if (objectType === 'global') return st('capabilitySourceGlobal');
    return st('capabilitySourceGlobal');
  }

  function capabilityOptions(cache) {
    var caps = cache && cache.capabilities || [];
    if (!caps.length) return '<option value="">' + escapeHtml(st('noCapabilitiesAvailable')) + '</option>';
    return '<option value="">' + escapeHtml(st('capabilitySelect')) + '</option>' + caps.map(function(item) {
      var id = String(item.id || item.capability_ref || '').trim();
      if (!id) return '';
      var label = item.display_name || item.capability_id || id;
      return '<option value="' + escapeHtml(id) + '" data-version="' + escapeHtml(item.current_version_key || '') + '">' + escapeHtml(label + ' (' + (item.capability_type || '-') + ')') + '</option>';
    }).join('');
  }

  function setCurrentCapabilityExport(payload) {
    state().currentCapabilityExport = payload || null;
  }

  function renderCapabilityExportButton() {
    return '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="exportSecCurrentCapabilityPackages()">' + escapeHtml(st('exportCapabilityJson')) + '</button>';
  }

  function capabilityExportRows(rows) {
    return (rows || []).map(function(row) {
      var cap = row.capability || {};
      return {
        policy_id: row.item && row.item.id || '',
        capability_ref: row.item && row.item.capability_ref || '',
        capability_version_key: row.item && row.item.capability_version_key || '',
        kind: row.kind || '',
        policy: row.policy || '',
        source: row.source || '',
        specificity: Number(row.specificity || 0),
        capability: cap && Object.keys(cap).length ? cap : null,
        compliance: row.compliance || null
      };
    });
  }

  function renderCapabilityPackagesFor(objectType, objectId, cache) {
    cache = cache || { capabilities: [], deployments: [], recommendations: [] };
    if (objectType === 'user' && cache.compliance && Array.isArray(cache.compliance.items)) {
      return renderCapabilityPackageRows('user', cache, cache.compliance.items.map(function(item) {
        return {
          item: { id: item.policy_id || '', capability_ref: item.capability_ref || '', capability_version_key: item.capability_version_key || '' },
          kind: item.policy === 'recommended' ? 'recommendation' : 'deployment',
          policy: item.policy || 'required',
          source: item.source === 'user' ? st('capabilitySourceUser') : (item.source === 'group' ? st('capabilitySourceGroup') : st('capabilitySourceGlobal')),
          capability: item.capability || null,
          specificity: item.specificity || 0,
          compliance: item
        };
      }));
    }
    if ((objectType === 'user' || objectType === 'group') && Array.isArray(cache.effectivePolicies)) {
      return renderCapabilityPackageRows(objectType, cache, cache.effectivePolicies.map(function(policy) {
        return {
          item: { id: policy.policy_id || '', capability_ref: policy.capability_ref || '', capability_version_key: policy.capability_version_key || '' },
          kind: policy.kind || 'deployment',
          policy: policy.policy || 'required',
          source: policy.source === 'user' ? st('capabilitySourceUser') : (policy.source === 'group' ? st('capabilitySourceGroup') : st('capabilitySourceGlobal')),
          capability: policy.capability || null,
          specificity: policy.specificity || 0
        };
      }));
    }
    var capMap = capabilityMap(cache);
    var rows = [];
    (cache.deployments || []).forEach(function(item) {
      var scope = parseScope(item.scope_json);
      var specificity = scopeSpecificity(scope, objectType, objectId);
      if (specificity < 0) return;
      rows.push({ item: item, kind: 'deployment', policy: item.deployment_policy || 'required', source: scopeLabel(scope, objectType), capability: capMap[item.capability_ref] || null, specificity: specificity });
    });
    (cache.recommendations || []).forEach(function(item) {
      var scope = parseScope(item.scope_json);
      var specificity = scopeSpecificity(scope, objectType, objectId);
      if (specificity < 0) return;
      rows.push({ item: item, kind: 'recommendation', policy: 'recommended', source: scopeLabel(scope, objectType), capability: capMap[item.capability_ref] || null, specificity: specificity });
    });
    return renderCapabilityPackageRows(objectType, cache, effectiveCapabilityRows(rows));
  }

  function renderCapabilityPolicySummary(rows) {
    var counts = { required: 0, recommended: 0, blocked: 0 };
    (rows || []).forEach(function(row) {
      var policy = String(row && row.policy || 'recommended').toLowerCase();
      if (policy === 'blocked') counts.blocked += 1;
      else if (policy === 'required') counts.required += 1;
      else counts.recommended += 1;
    });
    return '<div class="grid4" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('capabilityRequiredCount')) + '</label><strong>' + String(counts.required) + '</strong><span>' + escapeHtml(st('capabilityRequired')) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityRecommendedCount')) + '</label><strong>' + String(counts.recommended) + '</strong><span>' + escapeHtml(st('capabilityRecommended')) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityBlockedCount')) + '</label><strong>' + String(counts.blocked) + '</strong><span>' + escapeHtml(st('capabilityBlocked')) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityTotal')) + '</label><strong>' + String((rows || []).length) + '</strong><span>' + escapeHtml(st('capabilityPackages')) + '</span></div></div>';
  }

  function renderCapabilityPackageRows(objectType, cache, rows) {
    var addControls = '<div class="grid3" style="margin-top:10px;align-items:end"><div><label>' + escapeHtml(st('capabilitySelect')) + '</label><select id="secCapabilitySelect" style="height:36px">' + capabilityOptions(cache) + '</select></div><div><label>' + escapeHtml(st('version')) + '</label><input id="secCapabilityVersion" style="height:36px" placeholder="auto"></div><div class="actions" style="gap:6px;padding:0;flex-wrap:wrap"><button class="btn-secondary" style="height:32px;font-size:12px;padding:0 10px" onclick="saveSecCapabilityPolicy(\'required\')">' + escapeHtml(st('addRequiredCapability')) + '</button><button class="btn-ghost" style="height:32px;font-size:12px;padding:0 10px" onclick="saveSecCapabilityPolicy(\'recommended\')">' + escapeHtml(st('addRecommendedCapability')) + '</button><button class="btn-ghost" style="height:32px;font-size:12px;padding:0 10px;color:var(--danger)" onclick="saveSecCapabilityPolicy(\'blocked\')">' + escapeHtml(st('addBlockedCapability')) + '</button></div></div>';
    var body = rows.length ? rows.map(function(row) {
      var cap = row.capability || {};
      var title = cap.display_name || cap.capability_id || row.item.capability_ref || '-';
      var type = cap.capability_type || '-';
      var version = row.item.capability_version_key || cap.current_version_key || 'auto';
      var badge = row.policy === 'required' ? st('capabilityRequired') : (row.policy === 'blocked' ? st('capabilityBlocked') : st('capabilityRecommended'));
      var meta = [
        row.item.capability_ref || '-',
        type,
        st('version') + ': ' + version,
        st('capabilityPolicySource') + ': ' + row.source,
        st('capabilitySpecificity') + ': ' + String(row.specificity || 0),
        st('capabilityPolicyId') + ': ' + (row.item.id || '-')
      ].join(' | ');
      return '<div class="row" style="grid-template-columns:minmax(0,1fr) auto auto;gap:10px"><div style="min-width:0"><div style="font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(title) + '</div><div class="item-meta mono">' + escapeHtml(meta) + '</div></div><span class="badge info">' + escapeHtml(badge) + '</span><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px;color:var(--danger)" onclick="removeSecCapabilityPolicy(\'' + escapeJsString(row.kind) + '\',\'' + escapeJsString(row.item.id || '') + '\')">' + escapeHtml(st('removeCapabilityPolicy')) + '</button></div>';
    }).join('') : '<div class="hint" style="margin-top:8px">' + escapeHtml(st('noCapabilityPackages')) + '</div>';
    var summary = renderCapabilityPolicySummary(rows);
    var compliance = objectType === 'user' ? renderCapabilityCompliance(rows, cache.inventory || []) : '';
    var sec = state();
    setCurrentCapabilityExport({ object_type: objectType, group_id: objectType === 'group' ? sec.selectedGroupId || '' : '', group_name: objectType === 'group' ? sec.selectedGroupName || groupDisplayName(sec.selectedGroupId) || '' : '', user_email: objectType === 'user' ? sec.selectedUserEmail || '' : '', policy_rows: capabilityExportRows(rows), compliance: objectType === 'user' ? sec.lastCapabilityCompliance || null : null, inventory: objectType === 'user' ? cache.inventory || [] : [], exported_at: new Date().toISOString() });
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('capabilityPackages')) + '</div><div class="item-meta">' + escapeHtml(st('capabilityTotal') + ': ' + String((rows || []).length)) + '</div></div>' + renderCapabilityExportButton() + '</div>' + summary + compliance + body + addControls + '</div>';
  }

  function inventoryMap(items) {
    var map = {};
    (items || []).forEach(function(item) {
      if (item && item.capability_ref) map[String(item.capability_ref)] = item;
    });
    return map;
  }

  function capabilityComplianceStatus(row, inventory) {
    if (row && row.compliance && row.compliance.status) return row.compliance.status;
    var inv = inventory && inventory[String(row.item.capability_ref || '')] || null;
    var requiredVersion = row.item.capability_version_key || (row.capability && row.capability.current_version_key) || '';
    if (inv && inv.last_seen_at) {
      var ts = Date.parse(inv.last_seen_at);
      if (ts && Date.now() - ts > 7 * 24 * 60 * 60 * 1000) return 'stale';
    }
    if (row.policy === 'blocked') return inv && inv.installed ? 'blocked_installed' : 'compliant';
    if (!inv || !inv.installed) return 'missing';
    if (requiredVersion && inv.capability_version_key && requiredVersion !== inv.capability_version_key) return 'version_mismatch';
    return 'compliant';
  }

  function complianceBadge(status) {
    if (status === 'compliant') return '<span class="badge ok">' + escapeHtml(st('capabilityCompliant')) + '</span>';
    if (status === 'missing') return '<span class="badge warn">' + escapeHtml(st('capabilityMissing')) + '</span>';
    if (status === 'version_mismatch') return '<span class="badge warn">' + escapeHtml(st('capabilityVersionMismatch')) + '</span>';
    if (status === 'blocked_installed') return '<span class="badge danger">' + escapeHtml(st('capabilityBlockedInstalled')) + '</span>';
    if (status === 'stale') return '<span class="badge warn">' + escapeHtml(st('capabilityReportStale')) + '</span>';
    return '<span class="badge warn">' + escapeHtml(st('capabilityPendingReport')) + '</span>';
  }

  function renderCapabilityCompliance(rows, inventoryItems) {
    if (state().lastCapabilityCompliance && state().lastCapabilityCompliance.summary) {
      return renderServerCapabilityCompliance(rows, inventoryItems, state().lastCapabilityCompliance);
    }
    var invMap = inventoryMap(inventoryItems || []);
    var required = 0;
    var recommended = 0;
    var blocked = 0;
    var compliant = 0;
    var missing = 0;
    var mismatch = 0;
    var blockedInstalled = 0;
    var stale = 0;
    (rows || []).forEach(function(row) {
      if (row.policy === 'required') required += 1;
      else if (row.policy === 'blocked') blocked += 1;
      else recommended += 1;
      var status = capabilityComplianceStatus(row, invMap);
      if (status === 'compliant') compliant += 1;
      else if (status === 'missing') missing += 1;
      else if (status === 'version_mismatch') mismatch += 1;
      else if (status === 'blocked_installed') blockedInstalled += 1;
      else if (status === 'stale') stale += 1;
    });
    var metrics = '<div class="grid3" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('capabilityRequiredCount')) + '</label><strong>' + String(required) + '</strong><span>' + escapeHtml(st('capabilityRecommendedCount') + ': ' + recommended) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityCompliant')) + '</label><strong>' + String(compliant) + '</strong><span>' + escapeHtml(st('capabilityMissing') + ': ' + missing + ' | ' + st('capabilityReportStale') + ': ' + stale) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityBlockedCount')) + '</label><strong>' + String(blocked) + '</strong><span>' + escapeHtml(st('capabilityVersionMismatch') + ': ' + mismatch + ' | ' + st('capabilityBlockedInstalled') + ': ' + blockedInstalled) + '</span></div></div>';
    var list = (rows || []).length ? '<div style="display:grid;gap:6px;margin-top:8px">' + rows.map(function(row) {
      var cap = row.capability || {};
      var title = cap.display_name || cap.capability_id || row.item.capability_ref || '-';
      var inv = row.compliance || invMap[String(row.item.capability_ref || '')] || null;
      var status = capabilityComplianceStatus(row, invMap);
      var installedVersion = inv && (inv.capability_version_key || inv.installed_version) ? (inv.capability_version_key || inv.installed_version) : '-';
      var expectedVersion = row.item.capability_version_key || (row.capability && row.capability.current_version_key) || 'auto';
      var clientStatus = inv && inv.install_status ? inv.install_status : '-';
      var lastSeen = inv && inv.last_seen_at ? inv.last_seen_at : '-';
      var detail = [
        st('capabilityExpectedVersion') + ': ' + expectedVersion,
        st('capabilityInstalledVersion') + ': ' + installedVersion,
        st('capabilityInstallStatus') + ': ' + clientStatus,
        st('capabilityLastSeen') + ': ' + lastSeen
      ].join(' | ');
      return '<div class="row" style="grid-template-columns:minmax(0,1fr) auto;gap:10px"><div style="min-width:0"><div style="font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(title) + '</div><div class="item-meta mono">' + escapeHtml(row.item.capability_ref || '-') + '</div><div class="item-meta">' + escapeHtml(detail) + '</div></div>' + complianceBadge(status) + '</div>';
    }).join('') + '</div>' : '';
    var hintHtml = (inventoryItems || []).length ? '' : '<div class="hint" style="margin-top:8px">' + escapeHtml(st('capabilityTelemetryHint')) + '</div>';
    var unmanaged = renderUnmanagedInventory(rows, inventoryItems || []);
    return '<div style="margin-top:10px"><div class="item-meta" style="font-weight:650">' + escapeHtml(st('capabilityCompliance')) + '</div>' + metrics + hintHtml + list + unmanaged + '</div>';
  }

  function renderServerCapabilityCompliance(rows, inventoryItems, compliance) {
    var sec = state();
    var summary = compliance.summary || {};
    var metrics = '<div class="grid3" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('capabilityTotal')) + '</label><strong>' + String(summary.total || 0) + '</strong><span>' + escapeHtml(st('capabilityUnmanagedInstalled') + ': ' + (summary.unmanaged_installed || 0)) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityCompliant')) + '</label><strong>' + String(summary.compliant || 0) + '</strong><span>' + escapeHtml(st('capabilityMissing') + ': ' + (summary.missing || 0) + ' | ' + st('capabilityReportStale') + ': ' + (summary.stale || 0)) + '</span></div><div class="metric"><label>' + escapeHtml(st('capabilityBlockedCount')) + '</label><strong>' + String(summary.blocked_installed || 0) + '</strong><span>' + escapeHtml(st('capabilityVersionMismatch') + ': ' + (summary.version_mismatch || 0)) + '</span></div></div>';
    var meta = '<div class="item-meta" style="margin-top:6px">' + escapeHtml(st('capabilityGeneratedAt') + ': ' + (compliance.generated_at || '-') + ' | ' + st('capabilityStaleAfterHours') + ': ' + (compliance.stale_after_hours || 168) + 'h') + '</div>';
    var filters = '<div class="grid3" style="margin-top:10px;align-items:end"><div><label>' + escapeHtml(st('capabilityStatusFilter')) + '</label><select id="secCapabilityComplianceStatus" onchange="changeSecCapabilityComplianceFilter()" style="height:34px"><option value="">' + escapeHtml(st('capabilityAllStatuses')) + '</option><option value="compliant"' + (sec.capabilityComplianceStatusFilter === 'compliant' ? ' selected' : '') + '>' + escapeHtml(st('capabilityCompliant')) + '</option><option value="missing"' + (sec.capabilityComplianceStatusFilter === 'missing' ? ' selected' : '') + '>' + escapeHtml(st('capabilityMissing')) + '</option><option value="version_mismatch"' + (sec.capabilityComplianceStatusFilter === 'version_mismatch' ? ' selected' : '') + '>' + escapeHtml(st('capabilityVersionMismatch')) + '</option><option value="blocked_installed"' + (sec.capabilityComplianceStatusFilter === 'blocked_installed' ? ' selected' : '') + '>' + escapeHtml(st('capabilityBlockedInstalled')) + '</option><option value="stale"' + (sec.capabilityComplianceStatusFilter === 'stale' ? ' selected' : '') + '>' + escapeHtml(st('capabilityReportStale')) + '</option></select></div><div><label>' + escapeHtml(st('capabilityStaleAfterHours')) + '</label><input id="secCapabilityStaleAfterHours" type="number" min="1" max="8760" step="1" value="' + escapeHtml(String(sec.capabilityStaleAfterHours || 168)) + '" onchange="changeSecCapabilityComplianceFilter()" style="height:34px"></div><div style="display:flex;align-items:center;gap:10px;justify-content:space-between"><label style="display:flex;align-items:center;gap:8px;margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="secCapabilityIncludeUnmanaged" onchange="changeSecCapabilityComplianceFilter()"' + (sec.capabilityIncludeUnmanaged === false ? '' : ' checked') + '> ' + escapeHtml(st('capabilityShowUnmanaged')) + '</label><button class="btn-ghost" type="button" style="height:32px;font-size:12px;padding:0 10px" onclick="exportSecCapabilityCompliance()">' + escapeHtml(st('capabilityExportJson')) + '</button></div></div>';
    var list = (rows || []).length ? '<div style="display:grid;gap:6px;margin-top:8px">' + rows.map(function(row) {
      var cap = row.capability || {};
      var title = cap.display_name || cap.capability_id || row.item.capability_ref || '-';
      var cmp = row.compliance || {};
      var expectedVersion = row.item.capability_version_key || (row.capability && row.capability.current_version_key) || 'auto';
      var detail = [
        st('capabilityExpectedVersion') + ': ' + expectedVersion,
        st('capabilityInstalledVersion') + ': ' + (cmp.installed_version || '-'),
        st('capabilityInstallStatus') + ': ' + (cmp.install_status || '-'),
        st('capabilityLastSeen') + ': ' + (cmp.last_seen_at || '-')
      ].join(' | ');
      return '<div class="row" style="grid-template-columns:minmax(0,1fr) auto;gap:10px"><div style="min-width:0"><div style="font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(title) + '</div><div class="item-meta mono">' + escapeHtml(row.item.capability_ref || '-') + '</div><div class="item-meta">' + escapeHtml(detail) + '</div></div>' + complianceBadge(cmp.status || 'missing') + '</div>';
    }).join('') + '</div>' : '';
    var unmanaged = renderUnmanagedInventory(rows, inventoryItems || []);
    return '<div style="margin-top:10px"><div class="item-meta" style="font-weight:650">' + escapeHtml(st('capabilityCompliance')) + '</div>' + metrics + meta + filters + list + unmanaged + '</div>';
  }

  function renderUnmanagedInventory(rows, inventoryItems) {
    if (state().selectedObjectType === 'user' && state().lastCapabilityCompliance && Array.isArray(state().lastCapabilityCompliance.unmanaged_items)) {
      inventoryItems = state().lastCapabilityCompliance.unmanaged_items;
    }
    var managed = {};
    (rows || []).forEach(function(row) { if (row.item && row.item.capability_ref) managed[String(row.item.capability_ref)] = true; });
    var extras = (inventoryItems || []).filter(function(item) { return item && item.installed && item.capability_ref && !managed[String(item.capability_ref)]; });
    if (!extras.length) return '';
    return '<div style="margin-top:10px"><div class="item-meta" style="font-weight:650">' + escapeHtml(st('capabilityUnmanagedInstalled')) + '</div><div class="hint" style="margin-top:6px">' + escapeHtml(st('capabilityUnmanagedHint')) + '</div><div style="display:grid;gap:6px;margin-top:8px">' + extras.map(function(item) {
      var detail = [
        st('capabilityInstalledVersion') + ': ' + (item.capability_version_key || '-'),
        st('capabilityInstallStatus') + ': ' + (item.install_status || '-'),
        st('capabilityLastSeen') + ': ' + (item.last_seen_at || '-')
      ].join(' | ');
      return '<div class="row" style="grid-template-columns:minmax(0,1fr) auto;gap:10px"><div style="min-width:0"><div style="font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.capability_ref || '-') + '</div><div class="item-meta">' + escapeHtml(detail) + '</div></div><span class="badge warn">' + escapeHtml(st('capabilityUnmanagedInstalled')) + '</span></div>';
    }).join('') + '</div></div>';
  }

  function renderGroupMembersSummary(children, members) {
    var childCount = (children || []).length;
    var memberCount = (members || []).length;
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('members')) + '</div><div class="item-meta">' + escapeHtml(st('membersModalDesc')) + '</div></div><button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="openSecMembersModal()">' + escapeHtml(st('membersDialog')) + '</button></div><div class="grid2" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('memberCount')) + '</label><strong>' + String(memberCount) + '</strong><span>' + escapeHtml(st('selectedDepartment')) + '</span></div><div class="metric"><label>' + escapeHtml(st('childGroupCount')) + '</label><strong>' + String(childCount) + '</strong><span>' + escapeHtml(st('groupTree')) + '</span></div></div></div>';
  }

  function auditPayloadSummary(payload) {
    payload = payload || {};
    var parts = [];
    [
      ['capability_ref', 'Capability'],
      ['capability_version_key', st('version')],
      ['deployment_policy', st('capabilityPolicySource')],
      ['recommendation_reason', 'Reason'],
      ['group_id', st('auditGroupId')],
      ['email', st('auditEmail')],
      ['name', st('auditName')],
      ['parent_id', st('auditParentId')],
      ['id', 'ID']
    ].forEach(function(entry) {
      var key = entry[0];
      if (payload[key] !== undefined && payload[key] !== null && String(payload[key]) !== '') parts.push(entry[1] + ': ' + String(payload[key]));
    });
    if (payload.old_value !== undefined || payload.new_value !== undefined) parts.push(st('auditOldValue') + ': ' + String(payload.old_value) + ' -> ' + st('auditNewValue') + ': ' + String(payload.new_value));
    if (payload.policy && typeof payload.policy === 'object') parts.push(st('auditPolicyItems') + ': ' + Object.keys(payload.policy).sort().join(', '));
    if (payload.old && payload.new) parts.push(st('modelServiceGroups') + ': ' + auditBindingSnapshotSize(payload.old) + ' -> ' + auditBindingSnapshotSize(payload.new));
    if (payload.scope) {
      try {
        var scope = typeof payload.scope === 'string' ? JSON.parse(payload.scope) : payload.scope;
        var scopeBits = [];
        if (scope.type) scopeBits.push(scope.type);
        if (scope.group_id) scopeBits.push(scope.group_id);
        if (scope.user_email) scopeBits.push(scope.user_email);
        if (scopeBits.length) parts.push(st('auditScope') + ': ' + scopeBits.join('/'));
      } catch (_) {}
    }
    return parts.join(' | ');
  }

  function auditBindingSnapshotSize(snapshot) {
    snapshot = snapshot || {};
    var globalIds = snapshot.global_service_group_ids || [];
    var defaults = snapshot.default_new_user_service_groups || [];
    var groups = snapshot.group_bindings || [];
    var users = snapshot.user_bindings || [];
    return 'global ' + globalIds.length + ', fallback ' + defaults.length + ', group ' + groups.length + ', user ' + users.length;
  }
  function auditActionLabel(action) {
    var map = {
      'capability.managed_deployment.create': 'auditActionCapabilityDeployCreate',
      'capability.managed_deployment.delete': 'auditActionCapabilityDeployDelete',
      'capability.recommendation.create': 'auditActionCapabilityRecommendCreate',
      'capability.recommendation.delete': 'auditActionCapabilityRecommendDelete',
      'security.group.create': 'auditActionGroupCreate',
      'security.group.rename': 'auditActionGroupRename',
      'security.group.delete': 'auditActionGroupDelete',
      'security.group.member.add': 'auditActionMemberAdd',
      'security.group.member.remove': 'auditActionMemberRemove',
      'security.group.policy.update': 'auditActionGroupPolicy',
      'security.default_group.update': 'auditActionDefaultGroup',
      'llm.service_bindings.update': 'auditActionModelServiceBindings',
      'centralized_security_enabled': 'auditActionCentralizedOn',
      'centralized_security_disabled': 'auditActionCentralizedOff',
      'org_structure_enabled': 'auditActionOrgOn',
      'org_structure_disabled': 'auditActionOrgOff'
    };
    return map[action] ? st(map[action]) : action;
  }

  function auditActionOptions(selected) {
    var actions = [
      'security.group.create',
      'security.group.rename',
      'security.group.delete',
      'security.group.member.add',
      'security.group.member.remove',
      'security.group.policy.update',
      'security.default_group.update',
      'llm.service_bindings.update',
      'centralized_security_enabled',
      'centralized_security_disabled',
      'org_structure_enabled',
      'org_structure_disabled',
      'capability.managed_deployment.create',
      'capability.managed_deployment.delete',
      'capability.recommendation.create',
      'capability.recommendation.delete'
    ];
    return '<option value="">' + escapeHtml(st('auditAllActions')) + '</option>' + actions.map(function(action) {
      return '<option value="' + escapeHtml(action) + '"' + (selected === action ? ' selected' : '') + '>' + escapeHtml(auditActionLabel(action)) + '</option>';
    }).join('');
  }

  function auditControlField(labelKey, body) {
    return '<div><label>' + escapeHtml(st(labelKey)) + '</label>' + body + '</div>';
  }

  function auditLimitOptions(selected) {
    return [20, 50, 100, 200].map(function(value) {
      return '<option value="' + value + '"' + (Number(selected || 20) === value ? ' selected' : '') + '>' + value + '</option>';
    }).join('');
  }

  function auditButton(className, onclick, labelKey, id) {
    return '<button class="' + className + '" type="button" style="height:34px;font-size:12px;padding:0 12px" onclick="' + onclick + '" id="' + id + '">' + escapeHtml(st(labelKey)) + '</button>';
  }

  function renderAuditControls() {
    var sec = state();
    var root = document.getElementById('secAuditControls');
    if (!root) return;
    var controls = [
      auditControlField('auditSearch', '<input id="secAuditSearch" style="height:34px" value="' + escapeHtml(sec.auditQuery || '') + '" onkeydown="if(event.key===\'Enter\')reloadSecAuditLogs()">'),
      auditControlField('auditActionFilter', '<select id="secAuditAction" style="height:34px" onchange="reloadSecAuditLogs()">' + auditActionOptions(sec.auditAction || '') + '</select>'),
      auditControlField('auditFrom', '<input id="secAuditFrom" type="date" style="height:34px" value="' + escapeHtml(sec.auditFrom || '') + '" onchange="reloadSecAuditLogs()">'),
      auditControlField('auditTo', '<input id="secAuditTo" type="date" style="height:34px" value="' + escapeHtml(sec.auditTo || '') + '" onchange="reloadSecAuditLogs()">'),
      auditControlField('auditLimitLabel', '<select id="secAuditLimit" style="height:34px" onchange="reloadSecAuditLogs()">' + auditLimitOptions(sec.auditLimit) + '</select>')
    ];
    var actions = [
      auditButton('btn-ghost', 'reloadSecAuditLogs()', 'reload', 'secAuditSearchBtn'),
      auditButton('btn-ghost', 'clearSecAuditFilters()', 'auditClearFilters', 'secAuditClearBtn'),
      auditButton('btn-secondary', 'exportSecAuditLogs(\'json\')', 'auditExportJson', 'secAuditExportBtn'),
      auditButton('btn-secondary', 'exportSecAuditLogs(\'csv\')', 'auditExportCsv', 'secAuditExportCsvBtn')
    ];
    controls.push('<div style="display:flex;align-items:end;gap:6px;flex-wrap:wrap">' + actions.join('') + '</div>');
    root.innerHTML = controls.join('');
  }
  function renderAuditLogs() {
    var sec = state();
    var root = document.getElementById('secAuditList');
    if (!root) return;
    var items = sec.auditLogs || [];
    if (!items.length) {
      root.innerHTML = hint(st('auditEmpty'));
      return;
    }
    root.innerHTML = items.map(function(item) {
      var payload = item.payload || {};
      var summary = auditPayloadSummary(payload) || item.payload_json || '';
      var created = item.created_at || '';
      if (created && typeof created === 'string') created = created.replace('T', ' ').replace('Z', ' UTC');
      var action = item.action || '-';
      return '<div class="row" style="grid-template-columns:minmax(0,1fr) auto;gap:10px"><div style="min-width:0"><div style="font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(auditActionLabel(action)) + '</div><div class="item-meta mono">' + escapeHtml(action) + '</div><div class="item-meta">' + escapeHtml(summary || '-') + '</div></div><div class="item-meta" style="text-align:right;white-space:nowrap"><div>' + escapeHtml(st('auditActor') + ': ' + (item.admin_user_id || '-')) + '</div><div>' + escapeHtml(created || '-') + '</div></div></div>';
    }).join('');
  }

  function setCurrentOverviewSnapshot(payload) {
    state().currentOverviewSnapshot = payload || null;
  }

  function machineSummarySnapshot(machines) {
    machines = Array.isArray(machines) ? machines : [];
    var lastSeen = machines.map(function(item) { return item && item.last_seen_at || ''; }).filter(Boolean).sort().pop() || '';
    return {
      total: machines.length,
      online: machines.filter(function(item) { return !!(item && item.online); }).length,
      active_sessions: machines.reduce(function(sum, item) { return sum + Number(item && item.active_sessions || 0); }, 0),
      last_seen_at: lastSeen
    };
  }

  function setCurrentMemberSnapshot(payload) {
    state().currentMemberSnapshot = payload || null;
  }

  function buildMemberSnapshot(groupId, groupName, children, members) {
    members = dedupeEmails(members || []);
    children = Array.isArray(children) ? children : [];
    return {
      object_type: 'group',
      group_id: groupId || '',
      group_name: groupName || groupDisplayName(groupId) || groupId || '',
      group_path: groupPathLabel(groupId),
      group_path_ids: selectedGroupChainIds(groupId),
      member_count: members.length,
      child_group_count: children.length,
      members: members,
      children: children,
      exported_at: new Date().toISOString()
    };
  }

  function selectedObjectSnapshotMeta() {
    var sec = state();
    if (sec.selectedObjectType === 'user') {
      return { object_type: 'user', object_id: sec.selectedUserEmail || '', object_name: sec.selectedUserEmail || '' };
    }
    if (sec.selectedObjectType === 'group') {
      return { object_type: 'group', object_id: sec.selectedGroupId || '', object_name: sec.selectedGroupName || groupDisplayName(sec.selectedGroupId) || '' };
    }
    return { object_type: 'global', object_id: rootGroupId() || 'global', object_name: st('globalObject') };
  }

  function objectAuditQuery() {
    var sec = state();
    if (sec.selectedObjectType === 'user' && sec.selectedUserEmail) return sec.selectedUserEmail;
    if (sec.selectedObjectType === 'group' && sec.selectedGroupId) return sec.selectedGroupId;
    if (sec.selectedObjectType === 'global') return rootGroupId() || 'global';
    return '';
  }

  function renderObjectAuditSection() {
    var query = objectAuditQuery();
    var title = st('auditCurrentObject');
    var filterButton = query ? '<button class="btn-ghost" type="button" style="height:30px;font-size:12px;padding:0 10px" onclick="filterSecAuditByCurrentObject()">' + escapeHtml(query) + '</button>' : '';
    var empty = query ? '' : '<span class="item-meta">' + escapeHtml(st('auditCurrentObjectEmpty')) + '</span>';
    var actions = '<div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end">' + empty + filterButton + '<button class="btn-secondary" type="button" style="height:30px;font-size:12px;padding:0 10px" onclick="exportSecObjectSnapshot()">' + escapeHtml(st('exportObjectSnapshotJson')) + '</button></div>';
    return '<div class="item" style="padding:12px;margin-top:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(title) + '</div><div class="item-meta">' + escapeHtml(st('recentChangesDesc')) + '</div></div>' + actions + '</div></div>';
  }

  async function loadSecAuditLogs() {
    var sec = state();
    renderAuditControls();
    var query = encodeURIComponent(sec.auditQuery || '');
    var action = encodeURIComponent(sec.auditAction || '');
    var from = encodeURIComponent(sec.auditFrom || '');
    var to = encodeURIComponent(sec.auditTo || '');
    var limit = Math.max(1, Math.min(200, Number(sec.auditLimit || 20) || 20));
    var data = await api('/api/admin/audit-logs?limit=' + limit + (query ? '&q=' + query : '') + (action ? '&action=' + action : '') + (from ? '&from=' + from : '') + (to ? '&to=' + to : ''));
    sec.auditLogs = data.items || [];
    renderAuditLogs();
  }

  function csvEscape(value) {
    value = value === undefined || value === null ? '' : String(value);
    return '"' + value.replace(/"/g, '""') + '"';
  }

  function auditLogsCsv(items) {
    var header = ['created_at', 'actor', 'action_label', 'action', 'summary', 'payload_json'];
    var rows = (items || []).map(function(item) {
      var payload = item && item.payload || {};
      var action = item && item.action || '';
      return [
        item && item.created_at || '',
        item && item.admin_user_id || '',
        auditActionLabel(action),
        action,
        auditPayloadSummary(payload) || item && item.payload_json || '',
        item && item.payload_json || JSON.stringify(payload || {})
      ];
    });
    return [header].concat(rows).map(function(row) { return row.map(csvEscape).join(','); }).join('\r\n');
  }

  function membersModalFilteredRows() {
    var sec = state();
    var query = String(sec.membersSearch || '').trim().toLowerCase();
    return (sec.membersCache || []).filter(function(email) {
      var entry = findUserDirectoryEntry(email);
      var serviceStatus = entry && entry.service_status || {};
      var textValue = [email, entry && entry.sn, entry && entry.status, entry && entry.enrollment_status, serviceStatus.default_model, (serviceStatus.service_group_ids || []).join(' ')].join(' ').toLowerCase();
      return !query || textValue.indexOf(query) >= 0;
    });
  }

  function renderMembersModalSummary(rows) {
    var active = 0;
    var enrolled = 0;
    var service = 0;
    (rows || []).forEach(function(email) {
      var entry = findUserDirectoryEntry(email);
      var status = String(entry && entry.status || '').toLowerCase();
      var enrollment = String(entry && entry.enrollment_status || '').toLowerCase();
      if (status === 'active' || status === 'approved') active += 1;
      if (enrollment === 'approved' || enrollment === 'bound' || enrollment === 'enrolled') enrolled += 1;
      if ((entry && entry.has_service_access) || (entry && entry.service_status && entry.service_status.active)) service += 1;
    });
    return '<div class="grid4" style="margin-bottom:10px"><div class="metric"><label>' + escapeHtml(st('memberCount')) + '</label><strong>' + String((rows || []).length) + '</strong><span>' + escapeHtml(st('members')) + '</span></div><div class="metric"><label>' + escapeHtml(st('statusActive')) + '</label><strong>' + String(active) + '</strong><span>' + escapeHtml(st('status')) + '</span></div><div class="metric"><label>' + escapeHtml(st('enrollmentStatus')) + '</label><strong>' + String(enrolled) + '</strong><span>' + escapeHtml(st('statusApproved')) + '</span></div><div class="metric"><label>' + escapeHtml(st('serviceAccess')) + '</label><strong>' + String(service) + '</strong><span>' + escapeHtml(st('serviceAccessOn')) + '</span></div></div>';
  }

  function renderMembersModalPage() {
    var sec = state();
    var root = document.getElementById('secMembersModalList');
    if (!root) return;
    var rows = membersModalFilteredRows();
    var pageSize = 30;
    var totalPages = Math.max(1, Math.ceil(rows.length / pageSize));
    sec.membersModalPage = Math.max(1, Math.min(totalPages, Number(sec.membersModalPage || 1)));
    var start = (sec.membersModalPage - 1) * pageSize;
    var pageRows = rows.slice(start, start + pageSize);
    var summaryHtml = renderMembersModalSummary(rows);
    if (!pageRows.length) {
      root.innerHTML = summaryHtml + hint(st('noMembers'));
    } else {
      root.innerHTML = summaryHtml + pageRows.map(function(email, idx) {
        var absoluteIndex = start + idx + 1;
        var jsEmail = escapeJsString(email);
        var entry = findUserDirectoryEntry(email);
        var meta = '#' + absoluteIndex + ' | ' + st('status') + ': ' + localizeUserStatus(entry && entry.status) + ' | ' + st('userSN') + ': ' + (entry && entry.sn || '-');
        return '<div class="row" style="grid-template-columns:minmax(0,1fr) auto auto;gap:10px"><div style="min-width:0"><div style="font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(email) + '</div><div class="item-meta">' + escapeHtml(meta) + '</div></div><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="selectSecUser(\'' + jsEmail + '\')">' + escapeHtml(st('viewUserDetail')) + '</button><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px;color:var(--danger)" onclick="removeSecGroupMember(\'' + jsEmail + '\')">' + escapeHtml(st('remove')) + '</button></div>';
      }).join('');
    }
    _s('secMembersModalPagerMeta', 'textContent', st('pagerSummary', { page: sec.membersModalPage, totalPages: totalPages, start: rows.length ? start + 1 : 0, end: Math.min(start + pageRows.length, rows.length), total: rows.length }));
    var prev = document.getElementById('secMembersModalPrevBtn');
    var next = document.getElementById('secMembersModalNextBtn');
    if (prev) { prev.textContent = st('previous'); prev.disabled = sec.membersModalPage <= 1; }
    if (next) { next.textContent = st('next'); next.disabled = sec.membersModalPage >= totalPages; }
  }
  global.loadSecurityTab = async function loadSecurityTab() {
    applySecurityI18n();
    try {
      await loadSettings();
    } catch (err) {
      showToast(st('loadSecuritySettingsFailed') + err.message, 'error');
    }
    try {
      await loadGroups();
    } catch (err) {
      showToast(st('loadGroupTreeFailed') + err.message, 'error');
    }
    if (state().subTab === 'audit') {
      try {
        await loadSecAuditLogs();
      } catch (err) {
        showToast(st('auditLoadFailed') + err.message, 'error');
      }
    }
  };

  global.selectSecSubTab = function selectSecSubTab(tab) {
    var sec = state();
    sec.subTab = tab === 'audit' ? 'audit' : 'management';
    applySecSubTab();
    if (sec.subTab === 'audit') {
      global.reloadSecAuditLogs();
    }
  };

  global.saveSecCapabilityPolicy = async function saveSecCapabilityPolicy(policy) {
    var sec = state();
    var select = document.getElementById('secCapabilitySelect');
    var capabilityRef = String(select && select.value || '').trim();
    if (!capabilityRef) return;
    var versionInput = document.getElementById('secCapabilityVersion');
    var version = String(versionInput && versionInput.value || '').trim();
    var scope = { type: sec.selectedObjectType || 'global' };
    if (scope.type === 'group') scope.group_id = sec.selectedGroupId;
    if (scope.type === 'user') scope.user_email = sec.selectedUserEmail;
    try {
      if (policy === 'recommended') {
        await api('/api/admin/capability-market/recommendations', { method: 'POST', body: JSON.stringify({ capability_ref: capabilityRef, capability_version_key: version, scope: scope, recommendation_reason: 'enterprise_management', allow_user_dismiss: true, enabled: true }) });
      } else {
        await api('/api/admin/capability-market/managed-deployments', { method: 'POST', body: JSON.stringify({ capability_ref: capabilityRef, capability_version_key: version, scope: scope, deployment_policy: policy || 'required', reinstall_if_removed: true, retry_interval_minutes: 60, enabled: true }) });
      }
      sec.capabilityCache = null;
      showToast(st('capabilitySaved'), 'success');
      global.reloadSecAuditLogs();
      if (sec.selectedObjectType === 'user') global.selectSecUser(sec.selectedUserEmail);
      else if (sec.selectedObjectType === 'group') global.loadSecGroupPolicy(sec.selectedGroupId);
      else global.selectSecGlobal();
    } catch (err) {
      showToast(st('capabilitySaveFailed') + err.message, 'error');
    }
  };

  global.removeSecCapabilityPolicy = async function removeSecCapabilityPolicy(kind, id) {
    var sec = state();
    id = String(id || '').trim();
    if (!id || !confirm(st('confirmRemoveCapabilityPolicy'))) return;
    var url = kind === 'recommendation'
      ? '/api/admin/capability-market/recommendations/' + encodeURIComponent(id)
      : '/api/admin/capability-market/managed-deployments/' + encodeURIComponent(id);
    try {
      await api(url, { method: 'DELETE' });
      sec.capabilityCache = null;
      showToast(st('capabilityRemoved'), 'success');
      global.reloadSecAuditLogs();
      if (sec.selectedObjectType === 'user') global.selectSecUser(sec.selectedUserEmail);
      else if (sec.selectedObjectType === 'group') global.loadSecGroupPolicy(sec.selectedGroupId);
      else global.selectSecGlobal();
    } catch (err) {
      showToast(st('capabilitySaveFailed') + err.message, 'error');
    }
  };

  global.changeSecCapabilityComplianceFilter = function changeSecCapabilityComplianceFilter() {
    var sec = state();
    var status = document.getElementById('secCapabilityComplianceStatus');
    var unmanaged = document.getElementById('secCapabilityIncludeUnmanaged');
    var stale = document.getElementById('secCapabilityStaleAfterHours');
    sec.capabilityComplianceStatusFilter = String(status && status.value || '').trim();
    sec.capabilityIncludeUnmanaged = unmanaged ? !!unmanaged.checked : true;
    sec.capabilityStaleAfterHours = Math.max(1, Math.min(8760, Number(stale && stale.value || 168) || 168));
    if (sec.selectedUserEmail) global.selectSecUser(sec.selectedUserEmail);
  };

  global.exportSecObjectSnapshot = function exportSecObjectSnapshot() {
    try {
      var sec = state();
      var meta = selectedObjectSnapshotMeta();
      var query = objectAuditQuery();
      var payload = {
        snapshot_schema_version: 1,
        snapshot_type: 'enterprise_object',
        object_type: meta.object_type,
        object_id: meta.object_id,
        object_name: meta.object_name,
        exported_at: new Date().toISOString(),
        overview_snapshot: sec.currentOverviewSnapshot || null,
        policy_snapshot: sec.currentPolicyExport || null,
        model_service_snapshot: sec.currentModelServiceExport || null,
        capability_snapshot: sec.currentCapabilityExport || null,
        member_snapshot: sec.currentMemberSnapshot || null,
        snapshot_sections: {
          overview: !!sec.currentOverviewSnapshot,
          policy: !!sec.currentPolicyExport,
          model_service: !!sec.currentModelServiceExport,
          capability: !!sec.currentCapabilityExport,
          members: !!sec.currentMemberSnapshot,
          audit: true
        },
        audit_context: {
          query: query,
          current_filters: {
            q: sec.auditQuery || '',
            action: sec.auditAction || '',
            from: sec.auditFrom || '',
            to: sec.auditTo || '',
            limit: sec.auditLimit || 20
          },
          loaded_logs: sec.auditLogs || []
        }
      };
      payload = normalizeSnapshotPayload(payload, 'enterprise_object');
      var blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = 'enterprise-object-' + safeExportName(meta.object_type, 'object') + '-' + safeExportName(meta.object_id || meta.object_name, 'snapshot') + '-' + snapshotExportStamp(payload) + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      showSnapshotExportToast('objectSnapshotExported', payload);
    } catch (err) {
      showToast(st('objectSnapshotFailed') + (err && err.message || 'export failed'), 'error');
    }
  };

  global.exportSecCurrentCapabilityPackages = function exportSecCurrentCapabilityPackages() {
    var snapshot = state().currentCapabilityExport;
    if (!snapshot) {
      showToast(st('capabilitySnapshotEmpty'), 'info');
      return;
    }
    try {
      var payload = JSON.parse(JSON.stringify(snapshot));
      payload = normalizeSnapshotPayload(payload, 'capability_packages');
      var objectName = payload.user_email || payload.group_name || payload.group_id || payload.object_type || 'capability';
      var blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = 'enterprise-capability-' + safeExportName(payload.object_type, 'object') + '-' + safeExportName(objectName, 'capability') + '-' + snapshotExportStamp(payload) + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      showSnapshotExportToast('capabilitySnapshotExported', payload);
    } catch (err) {
      showToast(st('capabilitySnapshotFailed') + (err && err.message || 'export failed'), 'error');
    }
  };

  global.exportSecCapabilityCompliance = function exportSecCapabilityCompliance() {
    var sec = state();
    var compliance = sec.lastCapabilityCompliance;
    if (!compliance || !Array.isArray(compliance.items)) {
      showToast(st('capabilityExportEmpty'), 'info');
      return;
    }
    try {
      var payload = {
        snapshot_schema_version: 1,
        snapshot_type: 'capability_compliance',
        object_type: sec.selectedObjectType || 'user',
        user_email: sec.selectedUserEmail || '',
        group_id: sec.selectedGroupId || '',
        group_name: sec.selectedGroupName || '',
        filters: {
          status: sec.capabilityComplianceStatusFilter || '',
          include_unmanaged: sec.capabilityIncludeUnmanaged !== false,
          stale_after_hours: Math.max(1, Math.min(8760, Number(sec.capabilityStaleAfterHours || 168) || 168))
        },
        exported_at: new Date().toISOString(),
        compliance: compliance
      };
      payload = normalizeSnapshotPayload(payload, 'capability_compliance');
      var blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var safeUser = String(sec.selectedUserEmail || 'user').replace(/[^a-zA-Z0-9._-]+/g, '-').replace(/^-+|-+$/g, '') || 'user';
      var a = document.createElement('a');
      a.href = url;
      a.download = 'capability-compliance-' + safeUser + '-' + snapshotExportStamp(payload) + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      showSnapshotExportToast('capabilityExported', payload);
    } catch (err) {
      showToast(st('capabilityExportFailed') + (err && err.message || 'export failed'), 'error');
    }
  };

  global.exportSecCurrentModelService = function exportSecCurrentModelService() {
    var snapshot = state().currentModelServiceExport;
    if (!snapshot) {
      showToast(st('modelServiceExportEmpty'), 'info');
      return;
    }
    try {
      var payload = JSON.parse(JSON.stringify(snapshot));
      payload = normalizeSnapshotPayload(payload, 'model_service');
      var objectName = payload.user_email || payload.group_name || payload.group_id || payload.object_type || 'model-service';
      var blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = 'enterprise-model-service-' + safeExportName(payload.object_type, 'object') + '-' + safeExportName(objectName, 'model-service') + '-' + snapshotExportStamp(payload) + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      showSnapshotExportToast('modelServiceExported', payload);
    } catch (err) {
      showToast(st('modelServiceExportFailed') + err.message, 'error');
    }
  };

  global.exportSecCurrentPolicy = function exportSecCurrentPolicy() {
    var snapshot = state().currentPolicyExport;
    if (!snapshot) {
      showToast(st('policyExportEmpty'), 'info');
      return;
    }
    try {
      var payload = JSON.parse(JSON.stringify(snapshot));
      payload = normalizeSnapshotPayload(payload, 'security_policy');
      var objectName = payload.user_email || payload.group_name || payload.group_id || payload.object_type || 'policy';
      var blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = 'enterprise-policy-' + safeExportName(payload.object_type, 'object') + '-' + safeExportName(objectName, 'policy') + '-' + snapshotExportStamp(payload) + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      showSnapshotExportToast('policyExported', payload);
    } catch (err) {
      showToast(st('policyExportFailed') + err.message, 'error');
    }
  };

  global.reloadSecAuditLogs = async function reloadSecAuditLogs() {
    var sec = state();
    var input = document.getElementById('secAuditSearch');
    var action = document.getElementById('secAuditAction');
    var limit = document.getElementById('secAuditLimit');
    var from = document.getElementById('secAuditFrom');
    var to = document.getElementById('secAuditTo');
    sec.auditQuery = String(input && input.value || '').trim();
    sec.auditAction = String(action && action.value || '').trim();
    sec.auditLimit = Math.max(1, Math.min(200, Number(limit && limit.value || sec.auditLimit || 20) || 20));
    sec.auditFrom = String(from && from.value || '').trim();
    sec.auditTo = String(to && to.value || '').trim();
    try {
      await loadSecAuditLogs();
    } catch (err) {
      showToast(st('auditLoadFailed') + err.message, 'error');
    }
  };

  global.filterSecAuditByCurrentObject = async function filterSecAuditByCurrentObject() {
    var query = objectAuditQuery();
    if (!query) {
      showToast(st('auditCurrentObjectEmpty'), 'info');
      return;
    }
    var sec = state();
    sec.auditQuery = query;
    sec.auditAction = '';
    try {
      await loadSecAuditLogs();
      var input = document.getElementById('secAuditSearch');
      if (input) input.value = query;
      var panel = document.getElementById('secAuditPanel');
      if (panel && typeof panel.scrollIntoView === 'function') panel.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } catch (err) {
      showToast(st('auditLoadFailed') + err.message, 'error');
    }
  };

  global.clearSecAuditFilters = async function clearSecAuditFilters() {
    var sec = state();
    sec.auditQuery = '';
    sec.auditAction = '';
    sec.auditFrom = '';
    sec.auditTo = '';
    try {
      await loadSecAuditLogs();
    } catch (err) {
      showToast(st('auditLoadFailed') + err.message, 'error');
    }
  };

  global.exportSecAuditLogs = function exportSecAuditLogs(format) {
    var sec = state();
    var items = sec.auditLogs || [];
    if (!items.length) {
      showToast(st('auditExportEmpty'), 'info');
      return;
    }
    try {
      format = format === 'csv' ? 'csv' : 'json';
      var payload = {
        snapshot_schema_version: 1,
        snapshot_type: 'audit_logs',
        object_type: 'audit',
        object_id: 'recent-changes',
        filters: {
          q: sec.auditQuery || '',
          action: sec.auditAction || '',
          from: sec.auditFrom || '',
          to: sec.auditTo || '',
          limit: Math.max(1, Math.min(200, Number(sec.auditLimit || 20) || 20))
        },
        item_count: items.length,
        items: items
      };
      payload = normalizeSnapshotPayload(payload, 'audit_logs');
      var content = format === 'csv' ? auditLogsCsv(items) : JSON.stringify(payload, null, 2);
      var blob = new Blob([content], { type: format === 'csv' ? 'text/csv;charset=utf-8' : 'application/json;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = 'enterprise-audit-' + snapshotExportStamp(payload) + '.' + format;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      if (format === 'json') showSnapshotExportToast('auditExported', payload);
      else showToast(st('auditExported'), 'success');
    } catch (err) {
      showToast(st('auditExportFailed') + (err && err.message || 'export failed'), 'error');
    }
  };

  global.renderSecGroupTree = function renderSecGroupTree(nodes, container, depth) {
    renderTreeNodes(nodes, container, depth || 0);
  };

  global.loadSecGroupChildren = async function loadSecGroupChildren(groupID, silent) {
    var sec = state();
    if (!groupID) return [];
    sec.loadingChildrenGroupIds[groupID] = true;
    global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    try {
      var data = await api('/api/admin/security/groups/' + encodeURIComponent(groupID) + '/members');
      replaceGroupChildren(sec.groupTree, groupID, data.children || []);
      sec.loadedChildrenGroupIds[groupID] = true;
      global._secGroupTree = sec.groupTree;
      if (!silent) global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
      return data.children || [];
    } finally {
      delete sec.loadingChildrenGroupIds[groupID];
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    }
  };

  global.toggleSecGroup = async function toggleSecGroup(groupID) {
    var sec = state();
    if (!groupID) return;
    sec.expandedGroupIds[groupID] = !sec.expandedGroupIds[groupID];
    if (sec.expandedGroupIds[groupID] && !sec.loadedChildrenGroupIds[groupID]) {
      try {
        await global.loadSecGroupChildren(groupID);
      } catch (err) {
        showToast(st('loadChildGroupsFailed') + err.message, 'error');
      }
    } else {
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    }
  };

  global.showSecContextMenu = function showSecContextMenu(x, y) {
    var sec = state();
    var menu = document.getElementById('secContextMenu');
    if (!menu) return;
    if (sec.contextMenuHideHandler) {
      document.removeEventListener('click', sec.contextMenuHideHandler);
      document.removeEventListener('contextmenu', sec.contextMenuHideHandler);
      sec.contextMenuHideHandler = null;
    }
    if (menu.parentElement !== document.body) document.body.appendChild(menu);
    menu.classList.remove('hidden');
    menu.style.left = '0px';
    menu.style.top = '0px';
    var margin = 8;
    var rect = menu.getBoundingClientRect();
    var maxLeft = Math.max(margin, global.innerWidth - rect.width - margin);
    var maxTop = Math.max(margin, global.innerHeight - rect.height - margin);
    var left = Math.min(Math.max(margin, Number(x || 0) + 2), maxLeft);
    var top = Math.min(Math.max(margin, Number(y || 0) + 2), maxTop);
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
    function hide() {
      menu.classList.add('hidden');
      document.removeEventListener('click', hide);
      document.removeEventListener('contextmenu', hide);
      if (state().contextMenuHideHandler === hide) state().contextMenuHideHandler = null;
    }
    sec.contextMenuHideHandler = hide;
    setTimeout(function() {
      document.addEventListener('click', hide);
      document.addEventListener('contextmenu', hide);
    }, 0);
  };

  global.selectSecGroup = function selectSecGroup(id, name) {
    var sec = state();
    sec.selectedGroupId = id;
    sec.selectedGroupName = name;
    sec.selectedObjectType = 'group';
    sec.selectedUserEmail = '';
    sec.currentOverviewSnapshot = null;
    sec.currentMemberSnapshot = null;
    sec.membersCache = [];
    sec.membersChildrenCache = [];
    global._secSelectedGroupId = id;
    global._secSelectedGroupName = name;
    if (sec.groupTree) global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    var globalBtn = document.getElementById('secGlobalBtn');
    if (globalBtn) globalBtn.classList.remove('active');
    _s('secPolicyTitle', 'textContent', st('policyPrefix') + name);
    _s('secPolicySubtitle', 'textContent', st('groupIdPrefix') + id);
    var policyActions = document.getElementById('secPolicyActions');
    var groupMembers = document.getElementById('secGroupMembers');
    if (policyActions) policyActions.classList.remove('hidden');
    if (groupMembers) groupMembers.classList.remove('hidden');
    global.loadSecGroupPolicy(id);
    global.loadSecGroupMembers();
  };

  global.selectSecGlobal = async function selectSecGlobal() {
    var sec = state();
    sec.selectedObjectType = 'global';
    sec.selectedUserEmail = '';
    sec.currentOverviewSnapshot = null;
    sec.currentMemberSnapshot = null;
    sec.selectedGroupId = null;
    sec.selectedGroupName = '';
    global._secSelectedGroupId = null;
    global._secSelectedGroupName = '';
    if (sec.groupTree) global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
    var policyActions = document.getElementById('secPolicyActions');
    var groupMembers = document.getElementById('secGroupMembers');
    if (policyActions) policyActions.classList.add('hidden');
    if (groupMembers) groupMembers.classList.add('hidden');
    _s('secPolicyTitle', 'textContent', st('globalObject'));
    _s('secPolicySubtitle', 'textContent', st('globalObjectDesc'));
    var panel = document.getElementById('secPolicyPanel');
    if (panel) panel.innerHTML = hint(st('loading'));
    try {
      var rootId = rootGroupId();
      var results = await Promise.all([
        loadLLMServiceCache(),
        loadCapabilityCache(),
        rootId ? api('/api/admin/security/groups/' + encodeURIComponent(rootId) + '/policy').catch(function() { return null; }) : Promise.resolve(null)
      ]);
      setCurrentOverviewSnapshot({ object_type: 'global', object_id: rootId || 'global', object_name: st('enterpriseTitle'), default_group: document.getElementById('secDefaultGroupHint') ? document.getElementById('secDefaultGroupHint').textContent.replace(st('defaultGroupPrefix'), '') : '', exported_at: new Date().toISOString() });
      var overview = '<div class="item" style="padding:12px;margin-bottom:10px"><div class="item-title">' + escapeHtml(st('objectOverview')) + '</div><div class="grid2" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('globalObject')) + '</label><strong style="font-size:18px">' + escapeHtml(st('enterpriseTitle')) + '</strong><span>' + escapeHtml(st('globalObjectDesc')) + '</span></div><div class="metric"><label>' + escapeHtml(st('defaultGroup')) + '</label><strong style="font-size:18px">' + escapeHtml(document.getElementById('secDefaultGroupHint') ? document.getElementById('secDefaultGroupHint').textContent.replace(st('defaultGroupPrefix'), '') : '-') + '</strong><span>' + escapeHtml(st('defaultGroup')) + '</span></div></div></div>';
      if (results[2]) setCurrentPolicyExport({ object_type: 'global', group_id: rootId, items: results[2].items || {}, exported_at: new Date().toISOString() });
      var globalPolicy = results[2] ? '<div class="item" style="padding:12px;margin-bottom:10px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('globalSecurityPolicy')) + '</div><div class="item-meta">' + escapeHtml(st('groupIdPrefix') + rootId) + '</div></div><div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end">' + renderPolicyExportButton() + '<button class="btn-secondary" style="height:32px;font-size:12px;padding:0 12px" onclick="saveSecGlobalPolicy(\'' + escapeJsString(rootId) + '\')">' + escapeHtml(st('save')) + '</button></div></div>' + renderPolicySourceSummary(results[2].items || {}) + '<div style="margin-top:8px">' + renderPolicyRows(results[2].items || {}, true) + '</div></div>' : '';
      if (panel) panel.innerHTML = overview + globalPolicy + renderModelServiceForGlobal(results[0]) + renderCapabilityPackagesFor('global', '', results[1]) + renderObjectAuditSection();
    } catch (err) {
      if (panel) panel.innerHTML = errorHint(err.message);
    }
  };

  global.loadSecGroupPolicy = async function loadSecGroupPolicy(groupId) {
    var panel = document.getElementById('secPolicyPanel');
    if (!panel) return;
    try {
      var view = await api('/api/admin/security/groups/' + encodeURIComponent(groupId) + '/policy');
      var results = await Promise.all([loadLLMServiceCache(), loadCapabilityCache(), loadGroupEffectiveCapabilityPolicies(groupId)]);
      var serviceCache = results[0];
      var capabilityCache = results[1];
      if (capabilityCache && results[2] && Array.isArray(results[2].items)) capabilityCache.effectivePolicies = results[2].items;
      state().policyCache = view;
      global._secPolicyCache = view;
      var group = findGroupNode(state().groupTree, groupId) || { id: groupId, name: state().selectedGroupName || groupId, children: [] };
      var groupPath = groupPathLabel(groupId);
      setCurrentOverviewSnapshot({ object_type: 'group', group_id: groupId, group_name: group.name || groupId, group_path: groupPath, group_path_ids: selectedGroupChainIds(groupId), member_count: Number((state().membersCache || []).length || group.member_count || 0), child_group_count: (group.children || []).length, exported_at: new Date().toISOString() });
      var overview = '<div class="item" style="padding:12px;margin-bottom:10px"><div class="item-title">' + escapeHtml(st('objectOverview')) + '</div><div class="grid3" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('selectedDepartment')) + '</label><strong style="font-size:18px">' + escapeHtml(group.name || '-') + '</strong><span class="mono">' + escapeHtml(group.id || '-') + '</span></div><div class="metric"><label>' + escapeHtml(st('memberCount')) + '</label><strong>' + String(Number((state().membersCache || []).length || group.member_count || 0)) + '</strong><span>' + escapeHtml(st('members')) + '</span></div><div class="metric"><label>' + escapeHtml(st('childGroupCount')) + '</label><strong>' + String((group.children || []).length) + '</strong><span>' + escapeHtml(st('groupTree')) + '</span></div></div><div class="metric" style="margin-top:10px"><label>' + escapeHtml(st('orgPath')) + '</label><strong style="font-size:14px;word-break:break-word">' + escapeHtml(groupPath) + '</strong><span class="mono">' + escapeHtml(selectedGroupChainIds(groupId).join(' / ') || '-') + '</span></div></div>';
      setCurrentPolicyExport({ object_type: 'group', group_id: groupId, group_name: group.name || groupId, group_path: groupPath, group_path_ids: selectedGroupChainIds(groupId), items: view.items || {}, exported_at: new Date().toISOString() });
      var policy = '<div class="item" style="padding:12px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('effectivePolicy')) + '</div><div class="item-meta">' + escapeHtml(st('policyPrefix') + (group.name || groupId)) + '</div></div>' + renderPolicyExportButton() + '</div>' + renderPolicySourceSummary(view.items || {}) + '<div style="margin-top:8px">' + renderPolicyRows(view.items || {}, true) + '</div></div>';
      panel.innerHTML = overview + policy + renderModelServiceForGroup(groupId, serviceCache) + renderCapabilityPackagesFor('group', groupId, capabilityCache) + renderObjectAuditSection();
    } catch (err) {
      panel.innerHTML = errorHint(err.message);
    }
  };

  global.toggleSecPolicyInherit = function toggleSecPolicyInherit(el) {
    var key = el && el.getAttribute('data-policy-inherit-key');
    if (!key) return;
    var control = document.querySelector('#secPolicyPanel [data-policy-key="' + key + '"]');
    if (control) control.disabled = !!el.checked;
  };

  function collectSecPolicyPayload() {
    var policy = {};
    document.querySelectorAll('#secPolicyPanel [data-policy-key]').forEach(function(el) {
      var key = el.dataset.policyKey;
      var inherit = document.querySelector('#secPolicyPanel [data-policy-inherit-key="' + key + '"]');
      if (inherit && inherit.checked) return;
      policy[key] = el.dataset.policyType === 'bool' ? el.checked : el.value;
    });
    return policy;
  }

  async function saveSecPolicyForGroup(groupId, afterSave) {
    if (!groupId) {
      showToast(st('selectGroupFirst'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(groupId) + '/policy', { method: 'PUT', body: JSON.stringify({ policy: collectSecPolicyPayload() }) });
      showToast(st('policySaved'), 'success');
      global.reloadSecAuditLogs();
      if (afterSave) afterSave();
    } catch (err) {
      showToast(st('savePolicyFailed') + err.message, 'error');
    }
  }

  global.saveSecGlobalPolicy = function saveSecGlobalPolicy(groupId) {
    saveSecPolicyForGroup(groupId || rootGroupId(), function() { global.selectSecGlobal(); });
  };

  global.saveSecPolicy = function saveSecPolicy() {
    var sec = state();
    saveSecPolicyForGroup(sec.selectedGroupId, function() { global.loadSecGroupPolicy(sec.selectedGroupId); });
  };

  global.loadSecGroupMembers = async function loadSecGroupMembers() {
    var sec = state();
    if (!sec.selectedGroupId) return;
    var container = document.getElementById('secMembersList');
    if (!container) return;
    container.innerHTML = hint(st('loadingMembers'));
    try {
      var data = await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members');
      replaceGroupChildren(sec.groupTree, sec.selectedGroupId, data.children || []);
      sec.loadedChildrenGroupIds[sec.selectedGroupId] = true;
      global.renderSecGroupTree(sec.groupTree, document.getElementById('secGroupTree'), 0);
      var members = dedupeEmails(data.members || []);
      var children = data.children || [];
      setCurrentMemberSnapshot(buildMemberSnapshot(sec.selectedGroupId, sec.selectedGroupName, children, members));
      sec.membersCache = members.slice();
      sec.membersChildrenCache = children.slice ? children.slice() : children;
      sec.membersPage = 1;
      sec.membersModalPage = 1;
      container.innerHTML = renderGroupMembersSummary(children, members);
      if (sec.selectedGroupId) global.loadSecGroupPolicy(sec.selectedGroupId);
    } catch (err) {
      container.innerHTML = errorHint(err.message);
    }
  };

  global.changeSecMembersPage = function changeSecMembersPage(step) {
    var sec = state();
    if (!sec.selectedGroupId) return;
    var total = (sec.membersCache || []).length;
    var pageSize = Number(sec.membersPageSize || 60);
    var totalPages = Math.max(1, Math.ceil(total / pageSize));
    sec.membersPage = Math.max(1, Math.min(totalPages, Number(sec.membersPage || 1) + Number(step || 0)));
    var container = document.getElementById('secMembersList');
    if (!container) return;
    var group = findGroupNode(sec.groupTree, sec.selectedGroupId);
    var children = group && group.children ? group.children : [];
    container.innerHTML = renderMembersSection(children, sec.membersCache || []);
  };


  global.openSecMembersModal = async function openSecMembersModal() {
    var sec = state();
    if (!sec.selectedGroupId) return;
    if (!sec.membersCache || !sec.membersCache.length) {
      try {
        var data = await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members');
        sec.membersCache = dedupeEmails(data.members || []);
        sec.membersChildrenCache = data.children || [];
        setCurrentMemberSnapshot(buildMemberSnapshot(sec.selectedGroupId, sec.selectedGroupName, sec.membersChildrenCache, sec.membersCache));
      } catch (err) {
        showToast(st('loadUsersFailed') + err.message, 'error');
      }
    }
    _s('secMembersModalTitle', 'textContent', st('members') + ': ' + (sec.selectedGroupName || sec.selectedGroupId));
    _s('secMembersModalDesc', 'textContent', st('membersModalDesc'));
    _s('secMembersSearch', 'placeholder', st('searchMembers'));
    _s('secMembersModalReloadBtn', 'textContent', st('reload'));
    _s('secMembersModalExportBtn', 'textContent', st('exportMembersCsv'));
    _s('secMembersSearch', 'value', sec.membersSearch || '');
    try { await loadUserDirectoryCache(); } catch (_) {}
    var overlay = document.getElementById('secMembersModalOverlay');
    if (overlay) overlay.classList.add('show');
    renderMembersModalPage();
  };

  global.closeSecMembersModal = function closeSecMembersModal() {
    var overlay = document.getElementById('secMembersModalOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.filterSecMembersModal = function filterSecMembersModal() {
    var input = document.getElementById('secMembersSearch');
    state().membersSearch = String(input && input.value || '').trim();
    state().membersModalPage = 1;
    renderMembersModalPage();
  };

  global.changeSecMembersModalPage = function changeSecMembersModalPage(step) {
    state().membersModalPage = Number(state().membersModalPage || 1) + Number(step || 0);
    renderMembersModalPage();
  };

  global.exportSecMembersModalCsv = function exportSecMembersModalCsv() {
    var rows = membersModalFilteredRows();
    if (!rows.length) {
      showToast(st('membersExportEmpty'), 'info');
      return;
    }
    try {
      var header = ['email', 'sn', 'status', 'enrollment_status', 'smart_route', 'has_service_access', 'service_active', 'service_group_ids', 'default_model', 'credits_available', 'credits_remaining', 'effective_expires_at'];
      var lines = [header].concat(rows.map(function(email) {
        var entry = findUserDirectoryEntry(email) || {};
        var serviceStatus = entry.service_status || {};
        return [
          email,
          entry.sn || '',
          entry.status || '',
          entry.enrollment_status || '',
          entry.smart_route ? 'true' : 'false',
          entry.has_service_access ? 'true' : 'false',
          serviceStatus.active ? 'true' : 'false',
          (serviceStatus.service_group_ids || []).join('|'),
          serviceStatus.default_model || '',
          formatSecNumber(serviceStatus.credits_available || 0),
          formatSecNumber(serviceStatus.credits_remaining || 0),
          serviceStatus.effective_expires_at || serviceStatus.nearest_expires_at || ''
        ];
      })).map(function(row) { return row.map(csvEscape).join(','); }).join('\r\n');
      var blob = new Blob([lines], { type: 'text/csv;charset=utf-8' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      var group = String(state().selectedGroupName || state().selectedGroupId || 'members').replace(/[^a-zA-Z0-9._-]+/g, '-').replace(/^-+|-+$/g, '') || 'members';
      a.href = url;
      a.download = 'enterprise-members-' + group + '-' + new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19) + '.csv';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      showToast(st('membersExported'), 'success');
    } catch (err) {
      showToast(st('membersExportFailed') + (err && err.message || 'export failed'), 'error');
    }
  };

  global.selectSecUser = async function selectSecUser(email) {
    var sec = state();
    email = String(email || '').trim();
    if (!email) return;
    sec.selectedObjectType = 'user';
    sec.selectedUserEmail = email;
    sec.currentOverviewSnapshot = null;
    sec.currentMemberSnapshot = null;
    global.closeSecMembersModal();
    _s('secPolicyTitle', 'textContent', st('selectedUser') + ': ' + email);
    _s('secPolicySubtitle', 'textContent', st('selectedDepartment') + ': ' + (sec.selectedGroupName || '-'));
    var panel = document.getElementById('secPolicyPanel');
    if (panel) panel.innerHTML = hint(st('loading'));
    try {
      var results = await Promise.all([
        api('/api/admin/security/users/' + encodeURIComponent(email) + '/effective-policy'),
        loadLLMServiceCache(),
        api('/api/admin/llm/services/diagnose?email=' + encodeURIComponent(email)).catch(function() { return null; }),
        loadCapabilityCache(),
        loadUserDirectoryCache().catch(function() { return []; }),
        loadUserCapabilityInventory(email),
        loadUserEffectiveCapabilityPolicies(email),
        loadUserCapabilityCompliance(email),
        api('/api/admin/debug/machines?email=' + encodeURIComponent(email)).catch(function() { return { machines: [] }; })
      ]);
      var policyResponse = results[0] || {};
      var policy = policyResponse.policy || {};
      if (policyResponse.group_policy && policyResponse.group_path) policyResponse.group_policy.group_path = policyResponse.group_path;
      var cache = results[1] || {};
      var diag = results[2] || null;
      var capabilityCache = results[3] || null;
      var userEntry = findUserDirectoryEntry(email, results[4] || []);
      var userMachines = results[8] && Array.isArray(results[8].machines) ? results[8].machines : [];
      if (capabilityCache) capabilityCache.inventory = results[5] || [];
      if (capabilityCache && results[6] && Array.isArray(results[6].items)) capabilityCache.effectivePolicies = results[6].items;
      if (capabilityCache && results[7] && Array.isArray(results[7].items)) {
        capabilityCache.compliance = results[7];
        sec.lastCapabilityCompliance = results[7];
      } else {
        sec.lastCapabilityCompliance = null;
      }
      var items = userPolicyItemsFromGroupView(policy, policyResponse.group_policy || null, email);
      var statusText = userEntry ? localizeUserStatus(userEntry.status) : st('unknown');
      var enrollText = userEntry ? localizeUserStatus(userEntry.enrollment_status) : st('unknown');
      var serviceActive = !!(diag && diag.service_status && diag.service_status.active) || !!(userEntry && userEntry.has_service_access);
      var serviceText = serviceActive ? st('serviceAccessOn') : st('serviceAccessOff');
      var modelCount = diag && diag.service_status && Array.isArray(diag.service_status.available_models) ? diag.service_status.available_models.length : 0;
      var defaultModel = diag && diag.service_status && diag.service_status.default_model || 'auto';
      var policyGroupId = policyResponse.group_id || sec.selectedGroupId;
      var userOrgPath = policyGroupPathLabel(policyResponse.group_path || [], policyGroupId);
      var userOrgIds = policyGroupPathIds(policyResponse.group_path || [], policyGroupId);
      var userGroupName = (policyResponse.group_path || []).length ? (policyResponse.group_path[policyResponse.group_path.length - 1].name || policyGroupId || '-') : (findGroupNode(sec.groupTree || [], policyGroupId) || {}).name || sec.selectedGroupName || policyGroupId || '-';
      _s('secPolicySubtitle', 'textContent', st('selectedDepartment') + ': ' + userGroupName);
      setCurrentOverviewSnapshot({ object_type: 'user', user_email: email, status: userEntry && userEntry.status || '', sn: userEntry && userEntry.sn || '', enrollment_status: userEntry && userEntry.enrollment_status || '', smart_route: !!(userEntry && userEntry.smart_route), service_active: serviceActive, group_id: policyGroupId || '', group_name: userGroupName, group_path: userOrgPath, group_path_ids: userOrgIds, default_model: defaultModel, model_count: modelCount, machines: machineSummarySnapshot(userMachines), exported_at: new Date().toISOString() });
      var overview = '<div class="item" style="padding:12px;margin-bottom:10px"><div class="item-title">' + escapeHtml(st('objectOverview')) + '</div><div class="grid3" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('selectedUser')) + '</label><strong style="font-size:18px">' + escapeHtml(email) + '</strong><span>' + escapeHtml(st('status')) + ': ' + escapeHtml(statusText) + '</span></div><div class="metric"><label>' + escapeHtml(st('userSN')) + '</label><strong style="font-size:18px">' + escapeHtml(userEntry && userEntry.sn || '-') + '</strong><span>' + escapeHtml(st('enrollmentStatus')) + ': ' + escapeHtml(enrollText) + '</span></div><div class="metric"><label>' + escapeHtml(st('serviceAccess')) + '</label><strong style="font-size:18px">' + escapeHtml(serviceText) + '</strong><span>' + escapeHtml(st('smartRoute')) + ': ' + escapeHtml(userDirectoryBoolLabel(userEntry && userEntry.smart_route)) + '</span></div></div><div class="grid2" style="margin-top:10px"><div class="metric"><label>' + escapeHtml(st('selectedDepartment')) + '</label><strong style="font-size:18px">' + escapeHtml(userGroupName) + '</strong><span class="mono">' + escapeHtml(userOrgPath) + ' | ' + escapeHtml(userOrgIds) + '</span></div><div class="metric"><label>' + escapeHtml(st('defaultModel')) + '</label><strong style="font-size:18px">' + escapeHtml(defaultModel) + '</strong><span>' + escapeHtml(st('modelRoutes')) + ': ' + String(modelCount) + '</span></div></div></div>';
      setCurrentPolicyExport({ object_type: 'user', user_email: email, group_id: policyGroupId, group_path: policyResponse.group_path || [], policy: policy, items: items, machines: userMachines, exported_at: new Date().toISOString() });
      var policyHtml = '<div class="item" style="padding:12px"><div class="item-head"><div><div class="item-title">' + escapeHtml(st('effectivePolicy')) + '</div><div class="item-meta">' + escapeHtml(st('policyPrefix') + email) + '</div></div>' + renderPolicyExportButton() + '</div>' + renderPolicySourceSummary(items) + '<div style="margin-top:8px">' + renderPolicyRows(items, false) + '</div></div>';
      if (panel) panel.innerHTML = overview + policyHtml + renderModelServiceForUser(email, cache, diag) + renderCapabilityPackagesFor('user', email, capabilityCache) + renderUserDeviceSummary(userMachines) + renderObjectAuditSection();
    } catch (err) {
      if (panel) panel.innerHTML = errorHint(err.message);
    }
  };
  global.removeSecGroupMember = async function removeSecGroupMember(email) {
    var sec = state();
    if (!sec.selectedGroupId) return;
    if (!confirm(st('confirmRemoveUser', { email: email }))) return;
    try {
      await api('/api/admin/security/groups/' + encodeURIComponent(sec.selectedGroupId) + '/members/' + encodeURIComponent(email), { method: 'DELETE' });
      showToast(st('removed'), 'success');
      global.reloadSecAuditLogs();
      global.loadSecGroupMembers();
      global.loadSecurityTab();
    } catch (err) {
      showToast(st('removeFailed') + err.message, 'error');
    }
  };

  global.toggleSecCentralized = async function toggleSecCentralized(enabled) {
    try {
      var settings = await api('/api/admin/security/settings');
      settings.centralized_security_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? st('centralizedEnabled') : st('centralizedDisabled'), 'success');
      _s('secCentralizedHint', 'textContent', enabled ? st('enabled') : st('disabled'));
      global.reloadSecAuditLogs();
    } catch (err) {
      showToast(st('updateFailed') + err.message, 'error');
      var toggle = document.getElementById('secCentralizedToggle');
      if (toggle) toggle.checked = !enabled;
    }
  };

  global.toggleSecOrg = async function toggleSecOrg(enabled) {
    try {
      var settings = await api('/api/admin/security/settings');
      settings.org_structure_enabled = enabled;
      await api('/api/admin/security/settings', { method: 'PUT', body: JSON.stringify(settings) });
      showToast(enabled ? st('orgEnabled') : st('orgDisabled'), 'success');
      _s('secOrgHint', 'textContent', enabled ? st('enabled') : st('disabled'));
      global.reloadSecAuditLogs();
    } catch (err) {
      showToast(st('updateFailed') + err.message, 'error');
      var toggle = document.getElementById('secOrgToggle');
      if (toggle) toggle.checked = !enabled;
    }
  };

  global.closeDefaultGroupModal = function closeDefaultGroupModal() {
    var overlay = document.getElementById('defaultGroupModalOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.showSetDefaultGroup = async function showSetDefaultGroup() {
    var sec = state();
    try {
      var data = await api('/api/admin/security/groups');
      var root = normalizeNode(data.tree);
      sec.defaultGroupTree = root ? [root] : [];
      sec.defaultGroupPickedId = null;
      global._secDefaultGroupPickedId = null;
      var picker = document.getElementById('defaultGroupTreePicker');
      var overlay = document.getElementById('defaultGroupModalOverlay');
      global.renderDefaultGroupPicker(sec.defaultGroupTree, picker, 0);
      if (overlay) overlay.classList.add('show');
    } catch (err) {
      showToast(st('loadGroupTreeFailed') + err.message, 'error');
    }
  };

  global.renderDefaultGroupPicker = function renderDefaultGroupPicker(nodes, container, depth) {
    var sec = state();
    if (!container) return;
    if (depth === 0) container.innerHTML = '';
    if (!nodes || !nodes.length) return;
    nodes.forEach(function(node) {
      var row = document.createElement('div');
      row.style.paddingLeft = (depth * 16 + 8) + 'px';
      row.style.padding = '4px 8px 4px ' + (depth * 16 + 8) + 'px';
      row.style.cursor = 'pointer';
      row.style.borderRadius = '6px';
      row.style.transition = 'background .15s';
      if (node.id === sec.defaultGroupPickedId) {
        row.style.background = 'var(--accent-bg, #e8f0fe)';
        row.style.fontWeight = '600';
      }
      row.textContent = ((node.children && node.children.length) ? '\u25bc ' : '\u25cf ') + node.name;
      row.addEventListener('click', function() {
        sec.defaultGroupPickedId = node.id;
        global._secDefaultGroupPickedId = node.id;
        global.renderDefaultGroupPicker(sec.defaultGroupTree, container, 0);
      });
      container.appendChild(row);
      if (node.children && node.children.length) global.renderDefaultGroupPicker(node.children, container, depth + 1);
    });
  };

  global.confirmDefaultGroup = async function confirmDefaultGroup() {
    var sec = state();
    if (!sec.defaultGroupPickedId) {
      showToast(st('pleaseSelectGroup'), 'info');
      return;
    }
    try {
      await api('/api/admin/security/settings/default-group', { method: 'PUT', body: JSON.stringify({ group_id: sec.defaultGroupPickedId }) });
      showToast(st('defaultGroupSet'), 'success');
      global.closeDefaultGroupModal();
      global.reloadSecAuditLogs();
      global.loadSecurityTab();
    } catch (err) {
      showToast(st('setDefaultGroupFailed') + err.message, 'error');
    }
  };

  global.closeAssignUsersModal = function closeAssignUsersModal() {
    var sec = state();
    sec.selectedAssignEmail = '';
    sec.selectedAssignEmails = {};
    sec.assignGroupId = null;
    sec.contextGroupName = null;
    var overlay = document.getElementById('assignUsersModalOverlay');
    if (overlay) overlay.classList.remove('show');
  };

  global.selectAssignUser = function selectAssignUser(email) {
    var key = normalizeEmailKey(email);
    var selected = !!(state().selectedAssignEmails && state().selectedAssignEmails[key]);
    setAssignUserSelected(email, !selected);
    renderAssignUsers();
  };

  global.toggleAssignUser = function toggleAssignUser(email, checked) {
    setAssignUserSelected(email, checked);
    renderAssignUsers();
  };

  global.toggleAssignVisibleUsers = function toggleAssignVisibleUsers() {
    var rows = filteredAssignUsers();
    if (!rows.length) return;
    var sec = state();
    var allSelected = rows.every(function(user) {
      return !!(sec.selectedAssignEmails && sec.selectedAssignEmails[normalizeEmailKey(user.email)]);
    });
    rows.forEach(function(user) {
      setAssignUserSelected(user.email, !allSelected);
    });
    renderAssignUsers();
  };

  global.secCtxAction = function secCtxAction(action) {
    var sec = state();
    var menu = document.getElementById('secContextMenu');
    if (menu) menu.classList.add('hidden');
    var groupID = sec.contextGroupId;
    var groupName = sec.contextGroupName;
    if (!groupID) return;
    if (action === 'create') {
      var name = prompt(st('promptNewSubGroup'));
      if (!name) return;
      api('/api/admin/security/groups', { method: 'POST', body: JSON.stringify({ name: name, parent_id: groupID }) })
        .then(function() { showToast(st('subgroupCreated'), 'success'); global.reloadSecAuditLogs(); global.loadSecurityTab(); })
        .catch(function(err) { showToast(st('createFailed') + err.message, 'error'); });
      return;
    }
    if (action === 'rename') {
      var newName = prompt(st('promptNewName'), groupName);
      if (!newName) return;
      api('/api/admin/security/groups/' + encodeURIComponent(groupID), { method: 'PUT', body: JSON.stringify({ name: newName }) })
        .then(function() {
          showToast(st('renamed'), 'success');
          if (state().selectedGroupId === groupID) state().selectedGroupName = newName;
          global.reloadSecAuditLogs();
          global.loadSecurityTab();
        })
        .catch(function(err) { showToast(st('renameFailed') + err.message, 'error'); });
      return;
    }
    if (action === 'assign') {
      sec.assignGroupId = groupID;
      sec.contextGroupName = groupName;
      sec.selectedAssignEmail = '';
      sec.selectedAssignEmails = {};
      global._secAssignGroupId = groupID;
      _s('assignUsersModalTitle', 'textContent', st('assignTitleWithGroup', { name: groupName }));
      _s('assignUsersSearch', 'value', '');
      _s('assignUsersCount', 'textContent', st('loading'));
      var assignTree = document.getElementById('assignUsersTree');
      var assignOverlay = document.getElementById('assignUsersModalOverlay');
      var assignSearch = document.getElementById('assignUsersSearch');
      if (assignTree) assignTree.innerHTML = hint(st('loadingUsers'));
      if (assignOverlay) assignOverlay.classList.add('show');
      if (assignSearch && typeof assignSearch.focus === 'function') assignSearch.focus();
      loadAssignableUsers().catch(function(err) {
        var tree = document.getElementById('assignUsersTree');
        if (tree) tree.innerHTML = errorHint(err.message);
        showToast(st('loadUsersFailed') + err.message, 'error');
      });
      return;
    }
    if (action === 'delete') {
      if (!confirm(st('confirmDeleteGroup', { name: groupName }))) return;
      api('/api/admin/security/groups/' + encodeURIComponent(groupID), { method: 'DELETE' })
        .then(function() {
          showToast(st('groupDeleted'), 'success');
          if (state().selectedGroupId === groupID) {
            state().selectedGroupId = null;
            state().selectedGroupName = null;
            global._secSelectedGroupId = null;
            global._secSelectedGroupName = null;
          }
          global.reloadSecAuditLogs();
          global.loadSecurityTab();
        })
        .catch(function(err) { showToast(st('deleteFailed') + err.message, 'error'); });
    }
  };

  global.filterAssignUsers = function filterAssignUsers() {
    var input = document.getElementById('assignUsersSearch');
    state().selectedAssignEmail = String(input && input.value || '').trim();
    renderAssignUsers();
  };

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'security',
      title: function() { return st('enterpriseTitle'); },
      subtitle: function() { return st('enterpriseSubtitle'); }
    });
  }

  applySecurityI18n();
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() {
      var sec = state();
      applySecurityI18n();
      if (sec.assignGroupId && sec.contextGroupName) {
        _s('assignUsersModalTitle', 'textContent', st('assignTitleWithGroup', { name: sec.contextGroupName }));
      }
      renderAssignUsers();
      var membersPanel = document.getElementById('secGroupMembers');
      if (sec.selectedGroupId && membersPanel && !membersPanel.classList.contains('hidden')) {
        var container = document.getElementById('secMembersList');
        if (container) {
          var group = findGroupNode(sec.groupTree, sec.selectedGroupId);
          var children = group && group.children ? group.children : [];
          container.innerHTML = renderMembersSection(children, sec.membersCache || []);
        }
      }
    });
  }

  global.confirmAssignUsers = async function confirmAssignUsers() {
    var sec = state();
    var input = document.getElementById('assignUsersSearch');
    var typedEmail = String(input && input.value || '').trim();
    var emails = selectedAssignEmailList();
    if (!emails.length && typedEmail) emails = [typedEmail];
    emails = dedupeEmails(emails);
    if (!emails.length || !sec.assignGroupId) {
      showToast(st('selectOrEnterUsers'), 'info');
      return;
    }
    try {
      for (var i = 0; i < emails.length; i += 1) {
        await api('/api/admin/security/groups/' + encodeURIComponent(sec.assignGroupId) + '/members', { method: 'POST', body: JSON.stringify({ email: emails[i] }) });
      }
      var targetGroupId = sec.assignGroupId;
      var targetGroupName = sec.contextGroupName;
      showToast(st('userMoved'), 'success');
      global.closeAssignUsersModal();
      global.reloadSecAuditLogs();
      sec.selectedGroupId = targetGroupId;
      sec.selectedGroupName = targetGroupName;
      global._secSelectedGroupId = targetGroupId;
      global._secSelectedGroupName = targetGroupName;
      await global.loadSecurityTab();
      if (targetGroupId) global.selectSecGroup(targetGroupId, targetGroupName || targetGroupId);
    } catch (err) {
      showToast(st('assignFailed') + err.message, 'error');
    }
  };
})(window);
