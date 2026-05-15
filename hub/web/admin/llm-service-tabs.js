const LLM_SERVICE_I18N = {
  en: {
    adminTitle: 'Model Service Groups',
    adminDesc: 'Manage model service groups, routing, and entitlement bindings. Public API and exposed model names are shown above.',
    linkageIssuesTitle: 'Linkage Health',
    linkageIssuesEmpty: 'Provider and service-group linkage looks healthy.',
    groups: 'Service Groups',
    bindings: 'Security Group Bindings',
    bindingsDesc: 'Reuse the user groups already created in Enterprise Management.',
    users: 'User Bindings',
    cards: 'Service Exchange Cards',
    grants: 'Active Grants',
    navLabel: 'Model Services',
    navDesc: 'Model service groups and bindings',
    serviceCardsNavLabel: 'Service Exchange Cards',
    serviceCardsNavDesc: 'Issue and review service exchange cards',
    tabTitle: 'Model Services',
    tabSubtitle: 'Model service groups, bindings, and diagnostics',
    serviceCardsTabTitle: 'Service Exchange Cards',
    serviceCardsTabSubtitle: 'Issue service exchange cards and review redemption grants',
    reload: 'Reload',
    runtimeTitle: 'Model Runtime',
    runtimeDesc: 'Check local model files, download progress, and the stable public download URL.',
    runtimeRefresh: 'Refresh Runtime',
    runtimeTrigger: 'Start Background Download',
    runtimeStatus: 'Runtime Status',
    runtimeDir: 'Model Directory',
    runtimePublicUrl: 'Public Download URL',
    runtimeLogPath: 'Download Log',
    runtimeExpected: 'Expected Files',
    runtimeFlags: 'Flags',
    runtimeFiles: 'Files',
    runtimeLog: 'Recent Log',
    runtimeStatusReady: 'Ready',
    runtimeStatusDownloading: 'Downloading',
    runtimeStatusPartial: 'Partial',
    runtimeStatusMissing: 'Missing',
    runtimeFlagInitialized: 'Initialized',
    runtimeFlagDownloading: 'Downloading',
    runtimeFlagReady: 'Ready',
    runtimeFlagTrigger: 'Trigger Ready',
    runtimeYes: 'Yes',
    runtimeNo: 'No',
    runtimeLoadFailed: 'Load model runtime failed: {error}',
    runtimeTriggerDone: 'Background model download started.',
    runtimeTriggerFailed: 'Start model download failed: {error}',
    emptyValue: '-',
    addGroup: 'Add / Update Group',
    saveAll: 'Save Service Config',
    issueCard: 'Issue Service Exchange Card',
    issueCardPlansHint: 'Plan defaults: day 300 credits / 5h 150; week 1200 / 5h 300 / daily 600; month 5000 / 5h 600 / daily 1200 / weekly 2400; quarter 17000 / 5h 1200 / daily 2400 / weekly 4800 / monthly 10000; year 70000 / 5h 2400 / daily 4800 / weekly 9600 / monthly 40000.',
    systemDefaults: 'New User Benefits',
    systemDesc: 'Grant credits and bind a model-service group automatically for new users.',
    systemHint: 'New users receive 30% of the configured credits after registration, and the remaining 70% after email confirmation. The built-in default group is Default (No Model Access) and is used as the fallback when no group is selected.',
    newUserGroups: 'Default LLM Service Group',
    newUserDays: 'Validity (Days)',
    newUserCredits: 'Benefit Credits',
    saveDefaults: 'Save Defaults',
    credits: 'Credits',
    fiveHourCredits: '5h Credits',
    dailyCredits: 'Daily Credits',
    weeklyCredits: 'Weekly Credits',
    monthlyCredits: 'Monthly Credits',
    periodLimits: 'Limits',
    count: 'Quantity',
    cardDuration: 'Validity',
    cardDurationDay: 'Day (1 day)',
    cardDurationWeek: 'Week (7 days)',
    cardDurationMonth: 'Month (30 days)',
    cardDurationQuarter: 'Quarter (91 days)',
    cardDurationYear: 'Year (365 days)',
    diagnoseTitle: 'Entitlement Diagnostic',
    diagnoseDesc: 'Explain why a user can or cannot access model services.',
    diagnoseEmail: 'Email',
    diagnoseBtn: 'Diagnose',
    diagnoseEmpty: 'Enter an email to inspect effective entitlements.',
    diagnoseLoadFailed: 'Load entitlement diagnostic failed: {error}',
    loadFailed: 'Load model services failed: {error}',
    saveDone: 'Model service configuration saved.',
    saveFailed: 'Save model services failed: {error}',
    issueDone: 'Created {count} service exchange card(s).',
    issueCodesTitle: 'Issued Codes',
    copyCodes: 'Copy',
    downloadTxt: 'Download TXT',
    downloadCsv: 'Download CSV',
    exportCurrentTxt: 'Current TXT',
    exportCurrentCsv: 'Current CSV',
    exportAllCsv: 'All CSV',
    selectPage: 'Select Page',
    selectFiltered: 'Select Filtered',
    selectedCount: 'Selected {count}',
    clearSelected: 'Clear Selected',
    deleteSelected: 'Delete Selected',
    exportSelectedTxt: 'Selected TXT',
    exportSelectedCsv: 'Selected CSV',
    noCardSelected: 'Select at least one card first.',
    deleteSelectedConfirm: 'Delete selected service cards?',
    filterAll: 'All',
    filterUnused: 'Unused',
    filterRedeemed: 'Redeemed',
    deleteCard: 'Delete',
    deleteUnusedBatch: 'Delete Filtered Unused',
    deleteCardDone: 'Service card deleted.',
    deleteBatchDone: 'Filtered unused service cards deleted: {count}',
    deleteCardFailed: 'Delete service card failed: {error}',
    deleteBatchFailed: 'Batch delete failed: {error}',
    deleteCardConfirm: 'Delete this service card?',
    deleteBatchConfirm: 'Delete all unused service cards in the current filtered result?',
    cardSearch: 'Search',
    cardSearchPlaceholder: 'Search by label, card ID, or redeemed email',
    grantSearch: 'Search Grants',
    grantSearchPlaceholder: 'Search by email, card ID, source, or service group',
    pagePrev: 'Prev',
    pageNext: 'Next',
    pageSingle: '{total} cards',
    pageSummary: '{start}-{end} / {total} cards',
    grantPageSingle: '{total} grants',
    grantPageSummary: '{start}-{end} / {total} grants',
    grantStartsAt: 'Starts',
    grantExpiresAt: 'Expires',
    creditsUsed: 'used {used}',
    copyCodesDone: 'Issued codes copied.',
    copyCodesFailed: 'Copy failed: {error}',
    issueFailed: 'Create service exchange card failed: {error}',
    apiBaseUrl: 'API Base URL',
    chatCompletionsUrl: 'Chat Completions URL',
    modelsUrl: 'Models URL',
    availableModels: 'Available Entry Aliases',
    id: 'ID',
    name: 'Name',
    description: 'Description',
    modelsLabel: 'Routes',
    modelsHint: 'One line per public route. Format: route=provider1,provider2; features=document,reasoning,tools; priority=50; resolution=1; multiplier=1.2. Provider order is failover priority. Auto scheduling prefers the highest matched capability score, then the lowest candidate tier, then the lowest multiplier.',
    removeGroup: 'Remove Group',
    securityGroupId: 'Security Group ID',
    serviceGroups: 'Service Groups',
    addGroupBinding: 'Add Group Binding',
    email: 'Email',
    addUserBinding: 'Add User Binding',
    label: 'Label',
    days: 'Days',
    groupIdPlaceholder: 'coding-basic',
    groupNamePlaceholder: 'Coding Basic',
    groupDescPlaceholder: 'Exposed models for basic coding',
    bindingGroupPlaceholder: 'engineering',
    bindingServiceGroupsPlaceholder: 'coding-basic,coding-pro',
    userEmailPlaceholder: 'user@example.com',
    userServiceGroupsPlaceholder: 'coding-pro',
    cardLabelPlaceholder: 'April campaign',
    cardGroupsPlaceholder: 'coding-basic',
    diagnoseEmailPlaceholder: 'user@example.com',
    builtInDefaultNoAccess: 'Built-in Default (No Model Access)',
    builtInDefault: 'Built-in Default',
    noServiceGroups: 'No service groups yet.',
    noSecurityGroupBindings: 'No security-group bindings yet.',
    noDirectUserBindings: 'No direct user bindings yet.',
    noRedeemCards: 'No service exchange cards issued yet.',
    noActiveGrants: 'No grants yet.',
    remove: 'Remove',
    modelsCount: '{count} routes',
    daysCount: '{count} days',
    creditsCount: '{count} credits',
    creditsRemaining: 'credits {remaining}/{total}',
    card: 'card',
    redeemed: 'redeemed',
    unused: 'unused',
    active: 'active',
    inactive: 'inactive',
    defaultModel: 'default model',
    securityGroups: 'Security Groups',
    effectiveServiceGroups: 'Effective Service Groups',
    directUserBindings: 'Direct User Bindings',
    matchedGroupBindings: 'Matched Group Bindings',
    activeGrants: 'Active Grants',
    grantDetails: 'Grant Details',
    inactiveReasons: 'Inactive Reasons',
    modelRouting: 'Route Mapping',
    providerRoute: 'Providers',
    serviceGroupRoute: 'Service Groups',
    multiplierLabel: 'Multiplier',
    noAuthorizedModelDetails: 'No route details yet.',
    groupIdNameRequired: 'Service group id and name are required.',
    duplicateGroupId: 'Service group id already exists: {id}',
    groupIdImmutable: 'Service group id cannot be changed after creation. Create a new group when you need a new id.',
    builtInDefaultReadOnly: 'The built-in Default group is read-only.',
    builtInDefaultCannotRemove: 'The built-in Default group cannot be removed.',
    addNewGroup: 'Add New Group',
    modelsPlaceholder: 'auto=provider-a,provider-b; features=reasoning,tools; priority=50; resolution=1; multiplier=1\\ndoc=provider-c; features=document; priority=80; resolution=1; multiplier=1.2',
    systemGroupsPlaceholder: 'Select a service group',
    edit: 'Edit',
    chainOverviewTitle: 'Service Chain Overview',
    chainOverviewDesc: 'Tracks whether providers, groups, entitlements, and cards are connected end-to-end.',
    chainReady: 'Ready to Serve',
    chainNotReady: 'Not Ready',
    chainHealthy: 'The chain is connected. Authorized users can reach live model routes end-to-end.',
    chainProviders: 'Providers',
    chainLiveRoutes: 'Live Routes',
    chainUserReach: 'User Reach',
    chainCards: 'Cards',
    chainActiveGrants: 'Active Grants',
    chainFreeGroups: 'Free Groups',
    chainGrantGroups: 'Grant-Required Groups',
    chainAccessSummary: 'Free groups use bindings directly; grant-required groups still need redeemed cards and active grants.',
    chainStepAccessBindings: 'Binding path',
    chainStepAccessGrant: 'Grant path',
    chainStepAccessFreeReady: 'Free groups are reachable through bindings.',
    chainStepAccessFreeMissing: 'Free groups exist, but no binding path is configured yet.',
    chainStepAccessGrantReady: 'Grant-required groups already have active grants.',
    chainStepAccessGrantMissing: 'Grant-required groups still need redeemed cards and active grants.',
    chainIssueGrantGroupsNeedCards: 'Some service groups are marked as grant-required, but no card issuance or active grant path is connected yet.',
    cardUnlocks: 'Unlocks',
    cardUnlocksFree: 'Free groups only',
    cardUnlocksGrant: 'Grant-required groups',
    cardUnlocksMixed: 'Mixed free/grant groups',
    chainStepProviders: '1. Providers',
    chainStepRoutes: '2. Group Routes',
    chainStepAccess: '3. User Access',
    chainStepCards: '4. Cards',
    chainStepGrants: '5. Grants',
    chainStepReady: 'Ready',
    chainStepMissing: 'Missing',
    chainIssueNoProviders: 'No LLM providers have been created yet, so service groups still cannot route to live providers.',
    chainIssueProviderLinkage: 'Provider linkage issues were detected. Some service-group routes still reference missing or unavailable providers.',
    chainIssueNoRoutes: 'Providers exist, but no service-group routes have been configured for exposed models yet.',
    chainIssueNoLiveRoutes: 'Service-group routes exist, but none of them currently point to a live provider.',
    chainIssueNoBindings: 'Service groups exist, but no user binding, security-group binding, or new-user default grant path is configured yet.',
    chainIssueNoCards: 'No service exchange cards have been issued yet, so the card redemption path is not connected yet.',
    chainIssueNoGrants: 'Cards exist, but there are no active grants yet. Check whether cards have been redeemed or grants have already expired.',
    chainIssueNoAuthorizedModels: 'Active grants exist, but their service groups still produce no authorized models under the current provider state.',
    cardHealthReady: 'Redeemable',
    cardHealthPartial: 'Partially Ready',
    cardHealthBroken: 'Blocked',
    cardHealthRoutes: 'Live Routes',
    cardHealthGroups: 'Service Groups',
    cardHealthActiveGrants: 'Active Redemptions',
    cardCodeLabel: 'Code',
    cardIssueMissingGroups: 'Some referenced service groups no longer exist.',
    cardIssueNoLiveRoutes: 'Referenced groups exist, but none of them can currently route to a live provider.',
    cardIssueRedeemedNoActiveGrant: 'The card has been redeemed, but no active grant is currently attached.',
    grantRouteModels: 'Authorized Routes',
    grantGrantSource: 'Grant Source',
    grantSourceCard: 'Card',
    grantAccessType: 'Access Type',
    deleteGrant: 'Delete',
    deleteGrantConfirm: 'Delete this active grant?',
    deleteGrantDone: 'Active grant deleted.',
    deleteGrantFailed: 'Delete active grant failed: {error}',
    grantRouteMissingGroup: 'The referenced service group no longer exists.',
    grantRouteNoLiveModels: 'This grant currently has no live model routes.',
    billingRoutes: 'Route Access Gate',
    billingRouteModel: 'Entry Alias',
    billingRouteProvider: 'Provider',
    billingRouteAccess: 'Unlock Path',
    billingRouteStatus: 'Route Result',
    billingRouteBinding: 'Binding unlock',
    billingRouteGrant: 'Grant unlock',
    billingRouteEligible: 'Routable',
    billingRouteBlocked: 'Blocked',
    billingRouteCredits: 'Grant credits {credits}',
    grantRouteHealthActive: 'Active',
    grantRouteHealthExpired: 'Expired or inactive',
    grantStatusActive: 'Active',
    grantStatusPeriodLimited: 'Period limit exhausted',
    grantStatusQueued: 'Not active yet',
    grantStatusExhausted: 'Credits exhausted',
    grantStatusExpired: 'Expired',
    grantStatusInactive: 'Inactive',
    grantRetryAfterAt: 'Restores at {time}',
    unknownServiceGroupRefs: 'Unknown service groups: {refs}',
    serviceGroupRequired: 'Select at least one service group first.',
    unknownSecurityGroupRefs: 'Unknown security groups: {refs}',
    missingSecurityGroup: 'Missing security group',
    serviceGroupQuickPick: 'Choose service groups',
    noSelectableServiceGroups: 'No service groups to choose yet.',
    securityGroupQuickPick: 'Choose security group',
    noSelectableSecurityGroups: 'No security groups to choose yet.',
    providerReference: 'Created providers: {providers}. You can use either provider ID or an exact provider name in the model definition.',
    providerReferenceEmpty: 'Created providers: -',
    statusLabel: 'Status',
    extraFeaturesPlaceholder: 'vision,audio',
    exposedModelPlaceholder: 'auto',
    routeAutoHint: 'This virtual model name is what callers send as the model. Keep auto to let Hub choose the best provider by capability tags.'
  },
  zh: {
    adminTitle: '\u6a21\u578b\u670d\u52a1\u7ec4',
    adminDesc: '\u7ba1\u7406\u6a21\u578b\u670d\u52a1\u7ec4\u3001\u8def\u7531\u4e0e\u6388\u6743\u7ed1\u5b9a\u3002\u9876\u90e8\u5c55\u793a\u5bf9\u5916 API \u4e0e\u53ef\u7528\u6a21\u578b\u5217\u8868\u3002',
    linkageIssuesTitle: '\u94fe\u8def\u5065\u5eb7\u68c0\u67e5',
    linkageIssuesEmpty: '\u670d\u52a1\u5546\u4e0e\u670d\u52a1\u7ec4\u7684\u5173\u8054\u6b63\u5e38\u3002',
    groups: '\u670d\u52a1\u7ec4',
    bindings: '\u5b89\u5168\u7ec4\u7ed1\u5b9a',
    bindingsDesc: '\u590d\u7528\u5728\u4f01\u4e1a\u7ba1\u7406\u4e2d\u5df2\u521b\u5efa\u7684\u7528\u6237\u7ec4\u3002',
    users: '\u7528\u6237\u7ed1\u5b9a',
    cards: '\u670d\u52a1\u5151\u6362\u5361',
    grants: '\u751f\u6548\u6388\u6743',
    navLabel: '\u6a21\u578b\u670d\u52a1',
    navDesc: '\u6a21\u578b\u670d\u52a1\u7ec4\u4e0e\u7ed1\u5b9a',
    serviceCardsNavLabel: '\u670d\u52a1\u5151\u6362\u5361\u7ba1\u7406',
    serviceCardsNavDesc: '\u53d1\u884c\u4e0e\u5151\u6362\u72b6\u6001',
    tabTitle: '\u6a21\u578b\u670d\u52a1',
    tabSubtitle: '\u6a21\u578b\u670d\u52a1\u7ec4\u3001\u7ed1\u5b9a\u4e0e\u8bca\u65ad',
    serviceCardsTabTitle: '\u670d\u52a1\u5151\u6362\u5361\u7ba1\u7406',
    serviceCardsTabSubtitle: '\u53d1\u884c\u670d\u52a1\u5151\u6362\u5361\u5e76\u67e5\u770b\u5151\u6362\u6388\u6743\u72b6\u6001',
    reload: '\u91cd\u65b0\u52a0\u8f7d',
    runtimeTitle: '\u6a21\u578b\u8fd0\u884c\u72b6\u6001',
    runtimeDesc: '\u67e5\u770b\u672c\u5730\u6a21\u578b\u6587\u4ef6\u3001\u4e0b\u8f7d\u8fdb\u5ea6\u548c\u4fdd\u6301\u4e0d\u53d8\u7684\u5bf9\u5916\u4e0b\u8f7d URL\u3002',
    runtimeRefresh: '\u5237\u65b0\u8fd0\u884c\u72b6\u6001',
    runtimeTrigger: '\u540e\u53f0\u542f\u52a8\u4e0b\u8f7d',
    runtimeStatus: '\u8fd0\u884c\u72b6\u6001',
    runtimeDir: '\u6a21\u578b\u76ee\u5f55',
    runtimePublicUrl: '\u5bf9\u5916\u4e0b\u8f7d URL',
    runtimeLogPath: '\u4e0b\u8f7d\u65e5\u5fd7',
    runtimeExpected: '\u9884\u671f\u6587\u4ef6',
    runtimeFlags: '\u6807\u8bb0',
    runtimeFiles: '\u5df2\u53d1\u73b0\u6587\u4ef6',
    runtimeLog: '\u6700\u8fd1\u65e5\u5fd7',
    runtimeStatusReady: '\u5c31\u7eea',
    runtimeStatusDownloading: '\u4e0b\u8f7d\u4e2d',
    runtimeStatusPartial: '\u90e8\u5206\u5b8c\u6210',
    runtimeStatusMissing: '\u7f3a\u5931',
    runtimeFlagInitialized: '\u5df2\u521d\u59cb\u5316',
    runtimeFlagDownloading: '\u4e0b\u8f7d\u4e2d',
    runtimeFlagReady: '\u5df2\u5c31\u7eea',
    runtimeFlagTrigger: '\u53ef\u89e6\u53d1\u4e0b\u8f7d',
    runtimeYes: '\u662f',
    runtimeNo: '\u5426',
    runtimeLoadFailed: '\u52a0\u8f7d\u6a21\u578b\u8fd0\u884c\u72b6\u6001\u5931\u8d25: {error}',
    runtimeTriggerDone: '\u5df2\u5728\u540e\u53f0\u542f\u52a8\u6a21\u578b\u4e0b\u8f7d\u3002',
    runtimeTriggerFailed: '\u542f\u52a8\u6a21\u578b\u4e0b\u8f7d\u5931\u8d25: {error}',
    emptyValue: '-',
    addGroup: '\u65b0\u5efa / \u66f4\u65b0\u670d\u52a1\u7ec4',
    saveAll: '\u4fdd\u5b58\u670d\u52a1\u914d\u7f6e',
    issueCard: '\u53d1\u884c\u670d\u52a1\u5151\u6362\u5361',
    issueCardPlansHint: '\u9ed8\u8ba4\u89c4\u5219\uff1a\u5929\u5361 300 / 5\u5c0f\u65f6 150\uff1b\u5468\u5361 1200 / 5\u5c0f\u65f6 300 / \u6bcf\u65e5 600\uff1b\u6708\u5361 5000 / 5\u5c0f\u65f6 600 / \u6bcf\u65e5 1200 / \u6bcf\u5468 2400\uff1b\u5b63\u5361 17000 / 5\u5c0f\u65f6 1200 / \u6bcf\u65e5 2400 / \u6bcf\u5468 4800 / \u6bcf\u6708 10000\uff1b\u5e74\u5361 70000 / 5\u5c0f\u65f6 2400 / \u6bcf\u65e5 4800 / \u6bcf\u5468 9600 / \u6bcf\u6708 40000\u3002',
    systemDefaults: '\u65b0\u7528\u6237\u798f\u5229',
    systemDesc: '\u4e3a\u65b0\u7528\u6237\u81ea\u52a8\u53d1\u653e credits \u5e76\u7ed1\u5b9a\u6a21\u578b\u670d\u52a1\u7ec4\u3002',
    systemHint: '\u65b0\u7528\u6237\u6ce8\u518c\u540e\u5148\u53d1\u653e\u914d\u7f6e credits \u7684 30%\uff0c\u90ae\u4ef6\u786e\u8ba4\u540e\u518d\u53d1\u653e\u5269\u4f59 70%\u3002\u5982\u679c\u672a\u9009\u62e9\u670d\u52a1\u7ec4\uff0c\u5c06\u56de\u9000\u5230\u5185\u7f6e Default\uff08\u65e0\u6a21\u578b\u6743\u9650\uff09\u3002',
    newUserGroups: '\u9ed8\u8ba4 LLM \u670d\u52a1\u7ec4',
    newUserDays: '\u6709\u6548\u671f\uff08\u5929\uff09',
    newUserCredits: '\u798f\u5229 Credits',
    saveDefaults: '\u4fdd\u5b58\u9ed8\u8ba4\u503c',
    credits: '\u79ef\u5206',
    fiveHourCredits: '5 \u5c0f\u65f6\u989d\u5ea6',
    dailyCredits: '\u6bcf\u65e5\u989d\u5ea6',
    weeklyCredits: '\u6bcf\u5468\u989d\u5ea6',
    monthlyCredits: '\u6bcf\u6708\u989d\u5ea6',
    periodLimits: '\u9650\u989d',
    count: '\u751f\u6210\u6570\u91cf',
    cardDuration: '\u6709\u6548\u671f',
    cardDurationDay: '\u5929\uff081 \u5929\uff09',
    cardDurationWeek: '\u5468\uff087 \u5929\uff09',
    cardDurationMonth: '\u6708\uff0830 \u5929\uff09',
    cardDurationQuarter: '\u5b63\uff0891 \u5929\uff09',
    cardDurationYear: '\u5e74\uff08365 \u5929\uff09',
    diagnoseTitle: '\u6388\u6743\u8bca\u65ad',
    diagnoseDesc: '\u8bf4\u660e\u67d0\u4e2a\u7528\u6237\u4e3a\u4ec0\u4e48\u80fd\u6216\u4e0d\u80fd\u8bbf\u95ee\u6a21\u578b\u670d\u52a1\u3002',
    diagnoseEmail: '\u90ae\u7bb1',
    diagnoseBtn: '\u5f00\u59cb\u8bca\u65ad',
    diagnoseEmpty: '\u8bf7\u8f93\u5165\u90ae\u7bb1\u4ee5\u67e5\u770b\u5b9e\u9645\u751f\u6548\u7684\u6743\u9650\u3002',
    diagnoseLoadFailed: '\u52a0\u8f7d\u6388\u6743\u8bca\u65ad\u5931\u8d25: {error}',
    loadFailed: '\u52a0\u8f7d\u6a21\u578b\u670d\u52a1\u5931\u8d25: {error}',
    saveDone: '\u6a21\u578b\u670d\u52a1\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',
    saveFailed: '\u4fdd\u5b58\u6a21\u578b\u670d\u52a1\u5931\u8d25: {error}',
    issueDone: '\u5df2\u751f\u6210 {count} \u5f20\u670d\u52a1\u5151\u6362\u5361\u3002',
    issueCodesTitle: '\u672c\u6b21\u751f\u6210\u5361\u53f7',
    copyCodes: '\u590d\u5236',
    downloadTxt: '\u4e0b\u8f7d TXT',
    downloadCsv: '\u4e0b\u8f7d CSV',
    exportCurrentTxt: '\u5bfc\u51fa\u5f53\u524d TXT',
    exportCurrentCsv: '\u5bfc\u51fa\u5f53\u524d CSV',
    exportAllCsv: '\u5bfc\u51fa\u5168\u90e8 CSV',
    selectPage: '\u5168\u9009\u672c\u9875',
    selectFiltered: '\u5168\u9009\u5f53\u524d\u7b5b\u9009',
    selectedCount: '\u5df2\u9009 {count} \u5f20',
    clearSelected: '\u6e05\u7a7a\u5df2\u9009',
    deleteSelected: '\u5220\u9664\u5df2\u9009',
    exportSelectedTxt: '\u5bfc\u51fa\u5df2\u9009 TXT',
    exportSelectedCsv: '\u5bfc\u51fa\u5df2\u9009 CSV',
    noCardSelected: '\u8bf7\u5148\u9009\u62e9\u81f3\u5c11\u4e00\u5f20\u5361\u3002',
    deleteSelectedConfirm: '\u786e\u8ba4\u5220\u9664\u5df2\u9009\u670d\u52a1\u5361\uff1f',
    filterAll: '\u5168\u90e8',
    filterUnused: '\u672a\u5151\u6362',
    filterRedeemed: '\u5df2\u5151\u6362',
    deleteCard: '\u5220\u9664',
    deleteUnusedBatch: '\u5220\u9664\u7b5b\u9009\u672a\u5151\u6362',
    deleteCardDone: '\u670d\u52a1\u5361\u5df2\u5220\u9664\u3002',
    deleteBatchDone: '\u5df2\u5220\u9664 {count} \u5f20\u7b5b\u9009\u672a\u5151\u6362\u670d\u52a1\u5361\u3002',
    deleteCardFailed: '\u5220\u9664\u670d\u52a1\u5361\u5931\u8d25: {error}',
    deleteBatchFailed: '\u6279\u91cf\u5220\u9664\u5931\u8d25: {error}',
    deleteCardConfirm: '\u786e\u8ba4\u5220\u9664\u8fd9\u5f20\u670d\u52a1\u5361\uff1f',
    deleteBatchConfirm: '\u786e\u8ba4\u5220\u9664\u5f53\u524d\u7b5b\u9009\u7ed3\u679c\u4e2d\u6240\u6709\u672a\u5151\u6362\u670d\u52a1\u5361\uff1f',
    cardSearch: '\u641c\u7d22',
    cardSearchPlaceholder: '\u6309\u6807\u7b7e\u3001\u5361 ID \u6216\u5151\u6362\u90ae\u7bb1\u641c\u7d22',
    grantSearch: '\u641c\u7d22\u6388\u6743',
    grantSearchPlaceholder: '\u6309\u90ae\u7bb1\u3001\u5361 ID\u3001\u6765\u6e90\u6216\u670d\u52a1\u7ec4\u641c\u7d22',
    pagePrev: '\u4e0a\u4e00\u9875',
    pageNext: '\u4e0b\u4e00\u9875',
    pageSingle: '\u5171 {total} \u5f20\u5361',
    pageSummary: '\u7b2c {start}-{end} \u6761 / \u5171 {total} \u5f20\u5361',
    grantPageSingle: '\u5171 {total} \u6761\u6388\u6743',
    grantPageSummary: '\u7b2c {start}-{end} \u6761 / \u5171 {total} \u6761\u6388\u6743',
    grantStartsAt: '\u5f00\u59cb\u65f6\u95f4',
    grantExpiresAt: '\u5230\u671f\u65f6\u95f4',
    creditsUsed: '\u5df2\u7528 {used}',
    copyCodesDone: '\u5df2\u590d\u5236\u672c\u6b21\u751f\u6210\u7684\u5361\u53f7\u3002',
    copyCodesFailed: '\u590d\u5236\u5931\u8d25: {error}',
    issueFailed: '\u521b\u5efa\u670d\u52a1\u5151\u6362\u5361\u5931\u8d25: {error}',
    apiBaseUrl: 'API \u57fa\u5730\u5740',
    chatCompletionsUrl: 'Chat Completions \u5730\u5740',
    modelsUrl: 'Models \u5730\u5740',
    availableModels: '\u53ef\u7528\u5165\u53e3',
    id: 'ID',
    name: '\u540d\u79f0',
    description: '\u63cf\u8ff0',
    modelsLabel: '\u8def\u7531',
    modelsHint: '\u6bcf\u884c\u5b9a\u4e49\u4e00\u4e2a\u5bf9\u5916\u8def\u7531\u5165\u53e3\u3002\u683c\u5f0f\uff1aroute=provider1,provider2; features=document,reasoning,tools; priority=50; resolution=1; multiplier=1.2\u3002provider \u987a\u5e8f\u4ee3\u8868\u5931\u8d25\u5207\u6362\u4f18\u5148\u7ea7\uff0c\u81ea\u52a8\u8c03\u5ea6\u4f1a\u4f18\u5148\u9009\u62e9\u80fd\u529b\u5339\u914d\u5ea6\u6700\u9ad8\u3001\u5019\u9009\u5c42\u7ea7\u6700\u4f4e\u3001\u500d\u7387\u6700\u4f4e\u7684 provider\u3002',
    removeGroup: '\u5220\u9664\u670d\u52a1\u7ec4',
    securityGroupId: '\u5b89\u5168\u7ec4 ID',
    serviceGroups: '\u670d\u52a1\u7ec4',
    addGroupBinding: '\u65b0\u589e\u7ec4\u7ed1\u5b9a',
    email: '\u90ae\u7bb1',
    addUserBinding: '\u65b0\u589e\u7528\u6237\u7ed1\u5b9a',
    label: '\u6807\u7b7e',
    days: '\u5929\u6570',
    groupIdPlaceholder: 'coding-basic',
    groupNamePlaceholder: '\u57fa\u7840\u7f16\u7801\u670d\u52a1',
    groupDescPlaceholder: '\u57fa\u7840\u7f16\u7801\u573a\u666f\u7684\u5bf9\u5916\u6a21\u578b',
    bindingGroupPlaceholder: 'engineering',
    bindingServiceGroupsPlaceholder: 'coding-basic,coding-pro',
    userEmailPlaceholder: 'user@example.com',
    userServiceGroupsPlaceholder: 'coding-pro',
    cardLabelPlaceholder: '\u56db\u6708\u6d3b\u52a8',
    cardGroupsPlaceholder: 'coding-basic',
    diagnoseEmailPlaceholder: 'user@example.com',
    builtInDefaultNoAccess: '\u5185\u7f6e Default\uff08\u65e0\u6a21\u578b\u6743\u9650\uff09',
    builtInDefault: '\u5185\u7f6e Default',
    noServiceGroups: '\u6682\u65e0\u670d\u52a1\u7ec4\u3002',
    noSecurityGroupBindings: '\u6682\u65e0\u5b89\u5168\u7ec4\u7ed1\u5b9a\u3002',
    noDirectUserBindings: '\u6682\u65e0\u76f4\u63a5\u7528\u6237\u7ed1\u5b9a\u3002',
    noRedeemCards: '\u6682\u65e0\u5df2\u53d1\u884c\u7684\u670d\u52a1\u5151\u6362\u5361\u3002',
    noActiveGrants: '\u6682\u65e0\u6388\u6743\u3002',
    remove: '\u79fb\u9664',
    modelsCount: '{count} \u6761\u8def\u7531',
    daysCount: '{count} \u5929',
    creditsCount: '{count} \u79ef\u5206',
    creditsRemaining: '\u79ef\u5206 {remaining}/{total}',
    card: '\u5361',
    redeemed: '\u5df2\u5151\u6362',
    unused: '\u672a\u4f7f\u7528',
    active: '\u751f\u6548\u4e2d',
    inactive: '\u672a\u751f\u6548',
    defaultModel: '\u9ed8\u8ba4\u6a21\u578b',
    securityGroups: '\u5b89\u5168\u7ec4',
    effectiveServiceGroups: '\u751f\u6548\u670d\u52a1\u7ec4',
    directUserBindings: '\u76f4\u63a5\u7528\u6237\u7ed1\u5b9a',
    matchedGroupBindings: '\u5339\u914d\u5230\u7684\u7ec4\u7ed1\u5b9a',
    activeGrants: '\u751f\u6548\u6388\u6743',
    grantDetails: '\u6388\u6743\u660e\u7ec6',
    inactiveReasons: '\u4e0d\u53ef\u7528\u539f\u56e0',
    modelRouting: '\u8def\u7531\u6620\u5c04',
    providerRoute: '\u670d\u52a1\u5546',
    serviceGroupRoute: '\u670d\u52a1\u7ec4',
    multiplierLabel: '\u500d\u7387',
    noAuthorizedModelDetails: '\u6682\u65e0\u8def\u7531\u660e\u7ec6\u3002',
    groupIdNameRequired: '\u670d\u52a1\u7ec4 ID \u548c\u540d\u79f0\u4e3a\u5fc5\u586b\u9879\u3002',
    duplicateGroupId: '\u670d\u52a1\u7ec4 ID \u5df2\u5b58\u5728: {id}',
    groupIdImmutable: '\u670d\u52a1\u7ec4 ID \u521b\u5efa\u540e\u4e0d\u80fd\u4fee\u6539\uff0c\u9700\u8981\u65b0 ID \u65f6\u8bf7\u65b0\u5efa\u670d\u52a1\u7ec4\u3002',
    builtInDefaultReadOnly: '\u5185\u7f6e Default \u7ec4\u4e3a\u53ea\u8bfb\u3002',
    builtInDefaultCannotRemove: '\u5185\u7f6e Default \u7ec4\u4e0d\u80fd\u5220\u9664\u3002',
    addNewGroup: '\u65b0\u5efa\u670d\u52a1\u7ec4',
    modelsPlaceholder: 'auto=provider-a,provider-b; features=reasoning,tools; priority=50; resolution=1; multiplier=1\\ndoc=provider-c; features=document; priority=80; resolution=1; multiplier=1.2',
    systemGroupsPlaceholder: 'Select a service group',
    edit: '\u7f16\u8f91',
    chainOverviewTitle: '\u670d\u52a1\u94fe\u8def\u603b\u89c8',
    chainOverviewDesc: '\u4ece LLM \u670d\u52a1\u5546\u3001\u670d\u52a1\u7ec4\u3001\u6388\u6743\u5230\u5151\u6362\u5361\u7684\u53ef\u670d\u52a1\u72b6\u6001\u3002',
    chainReady: '\u53ef\u5bf9\u5916\u670d\u52a1',
    chainNotReady: '\u8fd8\u672a\u5c31\u7eea',
    chainHealthy: '\u94fe\u8def\u5df2\u6253\u901a\uff1a\u5df2\u53ef\u4ee5\u4ece\u6388\u6743\u5165\u53e3\u8bbf\u95ee\u5230\u6709\u6548\u7684\u6a21\u578b\u8def\u7531\u3002',
    chainProviders: '\u670d\u52a1\u5546',
    chainLiveRoutes: '\u6709\u6548\u8def\u7531',
    chainUserReach: '\u7528\u6237\u89e6\u8fbe',
    chainCards: '\u5151\u6362\u5361',
    chainActiveGrants: '\u6d3b\u8dc3\u6388\u6743',
    chainStepProviders: '1. \u670d\u52a1\u5546',
    chainStepRoutes: '2. \u670d\u52a1\u7ec4\u8def\u7531',
    chainStepAccess: '3. \u7528\u6237\u89e6\u8fbe',
    chainStepCards: '4. \u5151\u6362\u5361',
    chainStepGrants: '5. \u751f\u6548\u6388\u6743',
    chainStepReady: '\u5df2\u5c31\u7eea',
    chainStepMissing: '\u5f85\u8865\u9f50',
    chainIssueNoProviders: '\u8fd8\u6ca1\u6709\u521b\u5efa LLM \u670d\u52a1\u5546\uff0c\u670d\u52a1\u7ec4\u8fd8\u65e0\u6cd5\u9009\u62e9\u53ef\u8def\u7531\u7684 provider\u3002',
    chainIssueProviderLinkage: '\u5b58\u5728\u670d\u52a1\u5546\u5173\u8054\u95ee\u9898\uff0c\u6709\u670d\u52a1\u7ec4\u5f15\u7528\u4e86\u7f3a\u5931\u6216\u4e0d\u53ef\u7528\u7684 provider\u3002',
    chainIssueNoRoutes: '\u5df2\u6709\u670d\u52a1\u5546\uff0c\u4f46\u8fd8\u6ca1\u6709\u914d\u7f6e\u53ef\u5bf9\u5916\u670d\u52a1\u7684\u670d\u52a1\u7ec4\u8def\u7531\u3002',
    chainIssueNoLiveRoutes: '\u670d\u52a1\u7ec4\u5df2\u914d\u7f6e\u8def\u7531\uff0c\u4f46\u6ca1\u6709\u4efb\u4f55\u8def\u7531\u80fd\u8fde\u5230\u5f53\u524d\u6709\u6548\u7684 LLM \u670d\u52a1\u5546\u3002',
    chainIssueNoBindings: '\u5df2\u6709\u670d\u52a1\u7ec4\uff0c\u4f46\u8fd8\u6ca1\u6709\u914d\u7f6e\u76f4\u63a5\u7528\u6237\u7ed1\u5b9a\u3001\u5b89\u5168\u7ec4\u7ed1\u5b9a\u6216\u65b0\u7528\u6237\u9ed8\u8ba4\u6388\u6743\u3002',
    chainIssueNoCards: '\u8fd8\u6ca1\u6709\u53d1\u884c\u53ef\u5151\u6362\u7684\u670d\u52a1\u5361\uff0c\u5361\u5238\u6388\u6743\u94fe\u8def\u8fd8\u6ca1\u6709\u6253\u901a\u3002',
    chainIssueNoGrants: '\u5df2\u6709\u5151\u6362\u5361\uff0c\u4f46\u8fd8\u6ca1\u6709\u4efb\u4f55\u751f\u6548\u6388\u6743\uff0c\u8bf7\u786e\u8ba4\u662f\u5426\u5df2\u88ab\u5151\u6362\u6216\u6388\u6743\u662f\u5426\u8fc7\u671f\u3002',
    chainIssueNoAuthorizedModels: '\u5df2\u6709\u751f\u6548\u6388\u6743\uff0c\u4f46\u5bf9\u5e94\u670d\u52a1\u7ec4\u5728\u5f53\u524d provider \u72b6\u6001\u4e0b\u4ecd\u7136\u65e0\u6cd5\u751f\u6210\u53ef\u6388\u6743\u6a21\u578b\u3002',
    cardHealthReady: '\u53ef\u5151\u6362',
    cardHealthPartial: '\u90e8\u5206\u5c31\u7eea',
    cardHealthBroken: '\u5df2\u963b\u585e',
    cardHealthRoutes: '\u6709\u6548\u8def\u7531',
    cardHealthGroups: '\u670d\u52a1\u7ec4',
    cardHealthActiveGrants: '\u6d3b\u8dc3\u5151\u6362',
    cardCodeLabel: '\u5361\u53f7',
    cardIssueMissingGroups: '\u5b58\u5728\u5f15\u7528\u7684\u670d\u52a1\u7ec4\u5df2\u4e0d\u5b58\u5728\u3002',
    cardIssueNoLiveRoutes: '\u5f15\u7528\u7684\u670d\u52a1\u7ec4\u867d\u7136\u5b58\u5728\uff0c\u4f46\u5f53\u524d\u6ca1\u6709\u4efb\u4f55\u53ef\u8def\u7531\u5230 live provider \u7684\u6a21\u578b\u3002',
    cardIssueRedeemedNoActiveGrant: '\u8be5\u5361\u5df2\u88ab\u5151\u6362\uff0c\u4f46\u5f53\u524d\u6ca1\u6709\u5173\u8054\u7684\u751f\u6548 grant\u3002',
    grantRouteModels: '\u6388\u6743\u8def\u7531',
    grantGrantSource: '\u6388\u6743\u6765\u6e90',
    grantSourceCard: '\u5361\u7247',
    grantAccessType: '\u6388\u6743\u65b9\u5f0f',
    deleteGrant: '\u5220\u9664',
    deleteGrantConfirm: '\u786e\u8ba4\u5220\u9664\u8be5\u751f\u6548\u6388\u6743\uff1f',
    deleteGrantDone: '\u751f\u6548\u6388\u6743\u5df2\u5220\u9664\u3002',
    deleteGrantFailed: '\u5220\u9664\u751f\u6548\u6388\u6743\u5931\u8d25: {error}',
    grantRouteMissingGroup: '\u5f15\u7528\u7684\u670d\u52a1\u7ec4\u5df2\u4e0d\u5b58\u5728\u3002',
    grantRouteNoLiveModels: '\u8be5 grant \u5f53\u524d\u6ca1\u6709\u53ef\u7528\u7684 live model route\u3002',
    billingRoutes: '\u8def\u7531\u51c6\u5165\u95e8',
    billingRouteModel: '\u5165\u53e3\u540d',
    billingRouteProvider: '\u670d\u52a1\u5546',
    billingRouteAccess: '\u89e3\u9501\u8def\u5f84',
    billingRouteStatus: '\u8def\u7531\u7ed3\u679c',
    billingRouteBinding: '\u7ed1\u5b9a\u5373\u53ef\u89e3\u9501',
    billingRouteGrant: '\u9700\u5151\u6362\u5361\u4e0e\u989d\u5ea6',
    billingRouteEligible: '\u53ef\u8def\u7531',
    billingRouteBlocked: '\u88ab\u62e6\u622a',
    billingRouteCredits: '\u53ef\u7528 grant \u989d\u5ea6 {credits}',
    grantRouteHealthActive: '\u751f\u6548\u4e2d',
    grantRouteHealthExpired: '\u5df2\u8fc7\u671f\u6216\u672a\u751f\u6548',
    grantStatusActive: '\u751f\u6548\u4e2d',
    grantStatusPeriodLimited: '\u5468\u671f\u9650\u6d41',
    grantStatusQueued: '\u5f85\u751f\u6548',
    grantStatusExhausted: '\u989d\u5ea6\u5df2\u7528\u5c3d',
    grantStatusExpired: '\u5df2\u8fc7\u671f',
    grantStatusInactive: '\u672a\u751f\u6548',
    grantRetryAfterAt: '\u6062\u590d\u65f6\u95f4 {time}',
    unknownServiceGroupRefs: '\u672a\u77e5\u670d\u52a1\u7ec4: {refs}',
    serviceGroupRequired: '\u8bf7\u5148\u9009\u62e9\u81f3\u5c11\u4e00\u4e2a\u670d\u52a1\u7ec4\u3002',
    unknownSecurityGroupRefs: '\u672a\u77e5\u5b89\u5168\u7ec4: {refs}',
    missingSecurityGroup: '\u5b89\u5168\u7ec4\u4e0d\u5b58\u5728',
    serviceGroupQuickPick: '\u9009\u62e9\u670d\u52a1\u7ec4',
    noSelectableServiceGroups: '\u6682\u65e0\u53ef\u9009\u670d\u52a1\u7ec4\u3002',
    securityGroupQuickPick: '\u9009\u62e9\u5b89\u5168\u7ec4',
    noSelectableSecurityGroups: '\u6682\u65e0\u53ef\u9009\u5b89\u5168\u7ec4\u3002',
    statusLabel: '\u72b6\u6001',
    extraFeaturesPlaceholder: 'vision,audio',
    exposedModelPlaceholder: 'auto',
    routeAutoHint: '\u8fd9\u4e2a\u865a\u62df\u6a21\u578b\u540d\u5c31\u662f\u8c03\u7528\u65f6 model \u8981\u4f20\u7684\u540d\u5b57\u3002\u4fdd\u7559 auto \u5373\u53ef\u8ba9 Hub \u5728\u8fd9\u4e2a\u865a\u62df\u6a21\u578b\u4e0b\u6309\u80fd\u529b\u6807\u7b7e\u81ea\u52a8\u6311\u9009\u7ec4\u5185\u670d\u52a1\u3002'
  }
};
const lsx = (key, vars = {}) => ((LLM_SERVICE_I18N[currentLang] || LLM_SERVICE_I18N.en)[key] || LLM_SERVICE_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
let llmServiceAdminCache = null;
let llmServiceModelRuntimeCache = null;
let llmServiceSelectedGroupID = '';
let llmServiceDraftDirty = false;
let llmServiceRenderedGroupID = null;
let llmServiceProviderOptions = [];
let llmServiceSecurityGroupOptions = [];
let llmServiceGroupDialogMode = 'create';
let llmServiceGroupDraft = null;
let llmServiceProviderDialogState = null;
let llmServiceLastIssuedCodes = [];
let llmServiceCardFilter = 'all';
let llmServiceSelectedCardIDs = [];
let llmServiceSelectedCardMap = {};
let llmServiceCardSearch = '';
let llmServiceCardPage = 1;
const llmServiceCardPageSize = 20;
let llmServiceCardsPageData = { items: [], total: 0, page: 1, page_size: llmServiceCardPageSize };
let llmServiceGrantSearch = '';
let llmServiceGrantPage = 1;
const llmServiceGrantPageSize = 20;
let llmServiceSystemSettingsLoading = false;
const llmServiceCapabilityOptions = ['document', 'reasoning', 'tools'];
const llmServicePriorityOptions = [0, 10, 30, 50, 80, 100];
const llmServiceResolutionOptions = [0, 1, 2, 3];
const llmServiceMultiplierOptions = [1, 1.2, 1.5, 2, 3];
const llmServiceCardDefaultPlansByDuration = {
  1: { credits: 300, fiveHour: 150, daily: 0, weekly: 0, monthly: 0 },
  7: { credits: 1200, fiveHour: 300, daily: 600, weekly: 0, monthly: 0 },
  30: { credits: 5000, fiveHour: 600, daily: 1200, weekly: 2400, monthly: 0 },
  91: { credits: 17000, fiveHour: 1200, daily: 2400, weekly: 4800, monthly: 10000 },
  365: { credits: 70000, fiveHour: 2400, daily: 4800, weekly: 9600, monthly: 40000 }
};
const BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID = 'default';
function isBuiltinLLMServiceGroup(id) { return String(id || '').trim().toLowerCase() === BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID; }
function parseCSV(value) { return String(value || '').split(/[\n\r,\uff0c;\uff1b]+/).map(function(v) { return v.trim(); }).filter(Boolean); }
function normalizeLLMServiceProviderRef(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  const exactID = llmServiceProviderOptions.find(function(provider) { return String(provider.id || '').trim() === raw; });
  if (exactID) return String(exactID.id || '').trim();
  const key = raw.toLowerCase();
  const exactNameMatches = llmServiceProviderOptions.filter(function(provider) { return String(provider.name || '').trim().toLowerCase() === key; });
  if (exactNameMatches.length === 1) return String(exactNameMatches[0].id || '').trim();
  return raw;
}
function parseModelDefs(value) { return String(value || '').split(/\r?\n/).map(function(line) { return line.trim(); }).filter(Boolean).map(function(line) { const segments = line.split(';').map(function(part) { return part.trim(); }).filter(Boolean); const main = segments.shift() || ''; const parts = main.split('='); const name = (parts.shift() || '').trim(); const providers = parts.join('=').split(',').map(function(v) { return normalizeLLMServiceProviderRef(v); }).filter(Boolean); const item = { name: name, provider_ids: providers, capability_tags: [], priority: 0, resolution_tier: 0, credit_multiplier: 1 }; segments.forEach(function(segment) { const kv = segment.split('='); const key = (kv.shift() || '').trim().toLowerCase(); const raw = kv.join('=').trim(); if (!key || !raw) return; if (key === 'features' || key === 'capabilities') item.capability_tags = raw.split(',').map(function(v) { return v.trim(); }).filter(Boolean); else if (key === 'priority') item.priority = Number(raw) || 0; else if (key === 'resolution' || key === 'resolution_tier') item.resolution_tier = Number(raw) || 0; else if (key === 'multiplier' || key === 'credit_multiplier') item.credit_multiplier = Number(raw) || 1; }); return item; }).filter(function(item) { return item.name && item.provider_ids.length; }); }
function modelDefsText(models) { return (models || []).map(function(m) { const parts = [(m.name || '') + '=' + ((m.provider_ids || []).join(','))]; if (m.capability_tags && m.capability_tags.length) parts.push('features=' + m.capability_tags.join(',')); if (m.priority) parts.push('priority=' + String(m.priority)); if (m.resolution_tier) parts.push('resolution=' + String(m.resolution_tier)); if (m.credit_multiplier && Number(m.credit_multiplier) !== 1) parts.push('multiplier=' + String(m.credit_multiplier)); return parts.join('; '); }).join('\n'); }
function llmServiceModelSummary(model) {
  const providers = (model && model.provider_ids || []).map(function(id) { return llmServiceProviderDisplay({ id: normalizeLLMServiceProviderRef(id), name: '' }); }).filter(Boolean).join(' -> ') || lsx('emptyValue');
  const features = (model && model.capability_tags || []).join(', ') || lsx('emptyValue');
  const priority = Number(model && model.priority || 0) || 0;
  const resolution = Number(model && model.resolution_tier || 0) || 0;
  const multiplier = Number(model && model.credit_multiplier || 1) || 1;
  return String(model && model.name || lsx('emptyValue')) + ' = ' + providers + ' | features: ' + features + ' | priority: ' + String(priority) + ' | candidate_tier: ' + String(resolution) + ' | multiplier: ' + String(multiplier);
}
function llmServiceProviderDisplay(provider) {
  const id = String(provider && provider.id || '').trim();
  const name = String(provider && provider.name || '').trim();
  if (!id && !name) return '';
  if (!name || name === id) return id;
  return name + ' (' + id + ')';
}
async function loadLLMServiceProviderOptions() {
  try {
    const data = await api('/api/admin/llm/providers');
    llmServiceProviderOptions = (data && data.providers || []).map(function(provider) {
      return { id: String(provider.id || '').trim(), name: String(provider.name || '').trim() };
    }).filter(function(provider) { return provider.id; });
  } catch (_) {
    llmServiceProviderOptions = [];
  }
}
function llmServiceFlattenSecurityGroups(node, path, out) {
  if (!node) return;
  var id = String(node.id || '').trim();
  var name = String(node.name || '').trim();
  var label = path ? (path + ' / ' + (name || id)) : (name || id);
  if (id) out.push({ id: id, name: name, path: label });
  (node.children || []).forEach(function(child) { llmServiceFlattenSecurityGroups(child, label, out); });
}
async function loadLLMServiceSecurityGroupOptions() {
  try {
    const data = await api('/api/admin/security/groups');
    const out = [];
    llmServiceFlattenSecurityGroups(data && data.tree, '', out);
    llmServiceSecurityGroupOptions = out;
  } catch (_) {
    llmServiceSecurityGroupOptions = [];
  }
}
function llmServiceAppendSecurityGroup(inputID, groupID) {
  var input = document.getElementById(inputID);
  if (!input) return;
  input.value = String(groupID || '').trim();
  if (typeof input.focus === 'function') input.focus();
}
function refreshLLMServiceSecurityGroupSelectors() {
  var datalist = document.getElementById('llmSecurityGroupOptions');
  if (datalist) {
    datalist.innerHTML = (llmServiceSecurityGroupOptions || []).map(function(group) {
      return '<option value="' + llsEsc(group.id) + '" label="' + llsEsc(group.path || group.name || group.id) + '"></option>';
    }).join('');
  }
  var root = document.getElementById('llmServiceBindingGroupIDPicker');
  if (!root) return;
  if (!llmServiceSecurityGroupOptions.length) {
    root.innerHTML = '<div class="hint">' + escapeHtml(lsx('noSelectableSecurityGroups')) + '</div>';
    return;
  }
  root.innerHTML = '<div class="item-meta" style="margin-bottom:6px">' + escapeHtml(lsx('securityGroupQuickPick')) + '</div>' + llmServiceSecurityGroupOptions.map(function(group) {
    var label = group.path || group.name || group.id;
    return '<button type="button" class="btn-ghost mono" style="height:24px;font-size:11px;padding:0 8px;margin:2px" onclick="event.stopPropagation();llmServiceAppendSecurityGroup(\'llmServiceBindingGroupID\',\'' + llmServiceJSArg(group.id) + '\')">' + escapeHtml(label) + '</button>';
  }).join(' ');
}
function llmServiceUnknownSecurityGroups(ids) {
  if (!llmServiceSecurityGroupOptions.length) return [];
  var known = {};
  llmServiceSecurityGroupOptions.forEach(function(group) {
    var id = String(group && group.id || '').trim();
    if (id) known[id.toLowerCase()] = true;
  });
  var unknown = [];
  (ids || []).forEach(function(id) {
    var clean = String(id || '').trim();
    if (!clean) return;
    if (!known[clean.toLowerCase()] && unknown.indexOf(clean) < 0) unknown.push(clean);
  });
  return unknown;
}
function llmServiceWarnUnknownSecurityGroups(ids) {
  var unknown = llmServiceUnknownSecurityGroups(ids);
  if (!unknown.length) return false;
  showToast(lsx('unknownSecurityGroupRefs', { refs: unknown.join(', ') }), 'error');
  return true;
}
function llmServiceCollectReferencedSecurityGroups(cache) {
  var refs = [];
  (cache && cache.group_bindings || []).forEach(function(binding) { refs.push(binding.group_id); });
  return refs;
}
function llmServiceCardGridColumns() {
  if (window.innerWidth <= 720) return 1;
  if (window.innerWidth <= 1200) return 2;
  return 4;
}
function bindLLMServiceCardGridResize() {
  if (bindLLMServiceCardGridResize.done) return;
  var timer = null;
  window.addEventListener('resize', function() {
    if (!document.getElementById('llmServiceCardsList')) return;
    if (timer) window.clearTimeout(timer);
    timer = window.setTimeout(function() {
      if (llmServiceAdminCache) renderLLMServiceAdmin();
    }, 120);
  });
  bindLLMServiceCardGridResize.done = true;
}
bindLLMServiceCardGridResize.done = false;
function llmServiceSecurityGroupMap() {
  var map = {};
  (llmServiceSecurityGroupOptions || []).forEach(function(group) {
    var id = String(group && group.id || '').trim();
    if (id) map[id.toLowerCase()] = group;
  });
  return map;
}
function llmServiceDescribeSecurityGroup(id) {
  var clean = String(id || '').trim();
  if (!clean) return { id: '', label: lsx('emptyValue'), missing: false };
  var group = llmServiceSecurityGroupMap()[clean.toLowerCase()] || null;
  return {
    id: clean,
    label: group ? String(group.path || group.name || group.id || clean) : clean,
    missing: !!(llmServiceSecurityGroupOptions.length && !group)
  };
}
function aui() { return window.AdminUI; }
function bindLLMServiceDraftInputs() {
  if (bindLLMServiceDraftInputs.done) return;
  ['llmServiceGroupID', 'llmServiceGroupName', 'llmServiceGroupDesc', 'llmServiceGroupModels'].forEach(function(id) {
    const el = document.getElementById(id);
    if (!el) return;
    el.addEventListener('input', function() { llmServiceDraftDirty = true; });
  });
  bindLLMServiceDraftInputs.done = true;
}
bindLLMServiceDraftInputs.done = false;
function writeLLMServiceGroupDraft(group) {
  _s('llmServiceGroupID', 'value', group && group.id || '');
  _s('llmServiceGroupName', 'value', group && group.name || '');
  _s('llmServiceGroupDesc', 'value', group && group.description || '');
  _s('llmServiceGroupModels', 'value', modelDefsText(group && group.models || []));
  llmServiceDraftDirty = false;
  llmServiceRenderedGroupID = group && group.id || '';
}
function ensureLLMServiceAdminUI() {
  if (document.getElementById('llmServiceAdminRoot')) return;
  const tab = document.getElementById('tab-modelservices');
  if (!tab) return;
  const host = document.createElement('div');
  host.id = 'llmServiceAdminRoot';
  host.className = 'grid2';
  host.style.marginTop = '16px';
  host.innerHTML = '' +
    '<div class="item" style="grid-column:1 / -1" id="llmServiceModelRuntimeCard"></div>' +
    '<div class="item"><div class="item-head"><div><div class="item-title" id="llmServiceAdminTitle"></div><div class="item-meta" id="llmServiceAdminDesc"></div></div><div class="actions"><button class="btn-secondary" onclick="saveLLMServiceAdmin()" id="llmServiceSaveBtn"></button></div></div><div id="llmServiceLinkageIssues" style="margin-top:10px"></div>' +
    '<div class="grid2" style="margin-top:10px">' +
    '<div><label id="llmServiceExposeApiBaseLabel"></label><div id="llmServiceExposeApiBase" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label id="llmServiceExposeChatUrlLabel"></label><div id="llmServiceExposeChatUrl" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label id="llmServiceExposeModelsUrlLabel"></label><div id="llmServiceExposeModelsUrl" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label id="llmServiceExposeModelsLabel"></label><div id="llmServiceExposeModels" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '</div><div class="grid2">' +
    '<div><label id="llmServiceGroupIDLabel"></label><input id="llmServiceGroupID"></div>' +
    '<div><label id="llmServiceGroupNameLabel"></label><input id="llmServiceGroupName"></div>' +
    '<div style="grid-column:1 / -1"><label id="llmServiceGroupDescLabel"></label><input id="llmServiceGroupDesc"></div>' +
    '<div style="grid-column:1 / -1"><label id="llmServiceGroupModelsLabel"></label><textarea id="llmServiceGroupModels" style="width:100%;min-height:100px;padding:10px;border-radius:12px;border:1px solid var(--line);font:inherit;resize:vertical" placeholder=""></textarea><div class="hint" id="llmServiceGroupModelsHint"></div><div id="llmServiceProviderReference" class="hint" style="margin-top:8px"></div></div>' +
    '</div><div id="llmServiceChainOverview"></div><div class="actions" style="margin-top:10px"><button class="btn-primary" onclick="upsertLLMServiceGroup()" id="llmServiceAddGroupBtn"></button><button class="btn-danger" onclick="removeSelectedLLMServiceGroup()" id="llmServiceRemoveGroupBtn"></button></div><div id="llmServiceGroupsList" style="margin-top:10px"></div></div>' +
    '<div class="item"><div class="item-head"><div><div class="item-title" id="llmServiceBindingsTitle"></div><div class="item-meta" id="llmServiceBindingsDesc"></div></div></div>' +
    '<div class="grid2"><div><label id="llmServiceBindingGroupIDLabel"></label><input id="llmServiceBindingGroupID" list="llmSecurityGroupOptions"><div id="llmServiceBindingGroupIDPicker" class="hint" style="margin-top:8px"></div></div><div><label id="llmServiceBindingServiceGroupsLabel"></label><input id="llmServiceBindingServiceGroups" list="llmServiceGroupOptions"><div id="llmServiceBindingServiceGroupsPicker" class="hint" style="margin-top:8px"></div></div></div><div class="actions"><button class="btn-secondary" onclick="addLLMServiceGroupBinding()" id="llmServiceAddGroupBindingBtn"></button></div><div id="llmServiceGroupBindingsList"></div>' +
    '<div style="margin-top:10px" class="item-title" id="llmServiceUsersTitle"></div><div class="grid2"><div><label id="llmServiceUserEmailLabel"></label><input id="llmServiceUserEmail"></div><div><label id="llmServiceUserServiceGroupsLabel"></label><input id="llmServiceUserServiceGroups" list="llmServiceGroupOptions"><div id="llmServiceUserServiceGroupsPicker" class="hint" style="margin-top:8px"></div></div></div><div class="actions"><button class="btn-secondary" onclick="addLLMServiceUserBinding()" id="llmServiceAddUserBindingBtn"></button></div><div id="llmServiceUserBindingsList"></div>' +
    '<div style="margin-top:10px" class="item-title" id="llmServiceDiagnoseTitle"></div><div class="item-meta" id="llmServiceDiagnoseDesc" style="margin-bottom:10px"></div><div class="grid2"><div><label id="llmServiceDiagnoseEmailLabel"></label><input id="llmServiceDiagnoseEmail"></div><div style="display:flex;align-items:flex-end"><button class="btn-secondary" onclick="diagnoseLLMServiceUser()" id="llmServiceDiagnoseBtn"></button></div></div><div id="llmServiceDiagnoseResult" style="margin-top:10px"></div></div>';
  tab.appendChild(host);
  if (!document.getElementById('llmServiceGroupOptions')) { var dl = document.createElement('datalist'); dl.id = 'llmServiceGroupOptions'; document.body.appendChild(dl); }
  if (!document.getElementById('llmSecurityGroupOptions')) { var sdl = document.createElement('datalist'); sdl.id = 'llmSecurityGroupOptions'; document.body.appendChild(sdl); }
  bindLLMServiceDraftInputs();
  applyLLMServiceI18n();
}
function llmServiceRuntimeBadge(status) {
  var clean = String(status || '').trim().toLowerCase();
  var cls = 'warn';
  var labelKey = 'runtimeStatusPartial';
  if (clean === 'ready') { cls = 'ok'; labelKey = 'runtimeStatusReady'; }
  else if (clean === 'downloading') { cls = 'info'; labelKey = 'runtimeStatusDownloading'; }
  else if (clean === 'missing') { cls = 'danger'; labelKey = 'runtimeStatusMissing'; }
  return '<span class="badge ' + cls + '">' + escapeHtml(lsx(labelKey)) + '</span>';
}
function llmServiceRuntimeBool(value) {
  return value ? lsx('runtimeYes') : lsx('runtimeNo');
}
function renderLLMServiceModelRuntime() {
  var root = document.getElementById('llmServiceModelRuntimeCard');
  if (!root) return;
  var data = llmServiceModelRuntimeCache;
  if (!data) {
    root.innerHTML = '<div class="item-head"><div><div class="item-title">' + escapeHtml(lsx('runtimeTitle')) + '</div><div class="item-meta">' + escapeHtml(lsx('runtimeDesc')) + '</div></div><div class="actions"><button class="btn-ghost" type="button" onclick="loadLLMServiceModelRuntime()" id="llmServiceModelRuntimeRefreshBtn">' + escapeHtml(lsx('runtimeRefresh')) + '</button><button class="btn-secondary" type="button" onclick="triggerLLMServiceModelDownload()" id="llmServiceModelRuntimeTriggerBtn">' + escapeHtml(lsx('runtimeTrigger')) + '</button></div></div><div class="hint" style="margin-top:10px">' + escapeHtml(lsx('loading')) + '</div>';
    return;
  }
  var expected = (data.expected_files || []).length ? (data.expected_files || []).map(escapeHtml).join('<br>') : escapeHtml(lsx('emptyValue'));
  var files = (data.files || []).length ? (data.files || []).map(function(file) {
    var meta = [];
    if (file.available) meta.push((file.size_bytes || 0) + ' B');
    if (file.modified_at) meta.push(file.modified_at);
    return '<div style="padding:10px 12px;border:1px solid var(--line);border-radius:14px;background:rgba(255,255,255,.7)"><div style="display:flex;align-items:center;justify-content:space-between;gap:6px;flex-wrap:wrap"><strong>' + escapeHtml(file.name || '-') + '</strong>' + (file.available ? '<span class="badge ok">OK</span>' : '<span class="badge danger">MISS</span>') + '</div><div class="item-meta" style="margin-top:6px">' + escapeHtml(meta.join(' | ') || lsx('emptyValue')) + '</div></div>';
  }).join('') : '<div class="hint">' + escapeHtml(lsx('emptyValue')) + '</div>';
  var logTail = (data.log_tail || []).length ? escapeHtml((data.log_tail || []).join('\n')) : escapeHtml(lsx('emptyValue'));
  var triggerDisabled = data.trigger_supported ? '' : ' disabled';
  root.innerHTML = '' +
    '<div class="item-head"><div><div class="item-title">' + escapeHtml(lsx('runtimeTitle')) + '</div><div class="item-meta">' + escapeHtml(lsx('runtimeDesc')) + '</div></div><div class="actions"><button class="btn-ghost" type="button" onclick="loadLLMServiceModelRuntime()" id="llmServiceModelRuntimeRefreshBtn">' + escapeHtml(lsx('runtimeRefresh')) + '</button><button class="btn-secondary" type="button" onclick="triggerLLMServiceModelDownload()" id="llmServiceModelRuntimeTriggerBtn"' + triggerDisabled + '>' + escapeHtml(lsx('runtimeTrigger')) + '</button></div></div>' +
    '<div class="grid3" style="margin-top:10px">' +
    '<div><label>' + escapeHtml(lsx('runtimeStatus')) + '</label><div>' + llmServiceRuntimeBadge(data.status) + '</div></div>' +
    '<div><label>' + escapeHtml(lsx('runtimeFlags')) + '</label><div class="item-meta">' + escapeHtml(lsx('runtimeFlagInitialized')) + ': ' + escapeHtml(llmServiceRuntimeBool(data.initialized)) + '<br>' + escapeHtml(lsx('runtimeFlagDownloading')) + ': ' + escapeHtml(llmServiceRuntimeBool(data.downloading)) + '<br>' + escapeHtml(lsx('runtimeFlagReady')) + ': ' + escapeHtml(llmServiceRuntimeBool(data.ready)) + '<br>' + escapeHtml(lsx('runtimeFlagTrigger')) + ': ' + escapeHtml(llmServiceRuntimeBool(data.trigger_supported)) + '</div></div>' +
    '<div><label>' + escapeHtml(lsx('runtimeExpected')) + '</label><div class="mono">' + expected + '</div></div>' +
    '</div>' +
    '<div class="grid3" style="margin-top:10px">' +
    '<div><label>' + escapeHtml(lsx('runtimeDir')) + '</label><div class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">' + escapeHtml(data.model_dir || lsx('emptyValue')) + '</div></div>' +
    '<div><label>' + escapeHtml(lsx('runtimePublicUrl')) + '</label><div class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">' + escapeHtml(data.public_models_url || lsx('emptyValue')) + '</div></div>' +
    '<div><label>' + escapeHtml(lsx('runtimeLogPath')) + '</label><div class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">' + escapeHtml(data.log_path || lsx('emptyValue')) + '</div></div>' +
    '</div>' +
    '<div class="grid2" style="margin-top:10px">' +
    '<div><label>' + escapeHtml(lsx('runtimeFiles')) + '</label><div style="display:grid;gap:6px">' + files + '</div></div>' +
    '<div><label>' + escapeHtml(lsx('runtimeLog')) + '</label><div class="console" style="min-height:200px;max-height:240px">' + logTail + '</div></div>' +
    '</div>' +
    ((data.last_download_error || '') ? ('<div class="hint" style="margin-top:10px;color:#b55246">' + escapeHtml(data.last_download_error) + '</div>') : '');
}
async function loadLLMServiceModelRuntime(options) {
  ensureLLMServiceAdminUI();
  var silent = !!(options && options.silent);
  try {
    llmServiceModelRuntimeCache = await api('/api/admin/model_download/status');
    renderLLMServiceModelRuntime();
  } catch (err) {
    renderLLMServiceModelRuntime();
    if (!silent) {
      var msg = lsx('runtimeLoadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  }
}
async function triggerLLMServiceModelDownload() {
  try {
    await api('/api/admin/model_download/trigger', { method: 'POST' });
    showToast(lsx('runtimeTriggerDone'), 'success');
    await loadLLMServiceModelRuntime({ silent: true });
  } catch (err) {
    var msg = lsx('runtimeTriggerFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
function llmServiceCardsPanelMarkup() {
  return '' +
    '<div class="item" style="padding:14px 16px"><div class="item-title" id="llmServiceCardsTitle"></div><div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(104px,1fr));gap:10px;align-items:end;margin-top:10px"><div style="min-width:0;grid-column:span 2"><label id="llmServiceCardLabelLabel"></label><input id="llmServiceCardLabel"></div><div style="min-width:0;grid-column:span 2"><label id="llmServiceCardGroupsLabel"></label><input id="llmServiceCardGroups" list="llmServiceGroupOptions"></div><div style="min-width:0"><label id="llmServiceCardDaysLabel"></label><select id="llmServiceCardDays" style="height:36px" onchange="llmServiceApplyCardDurationDefaults()"><option value="1" id="llmServiceCardDurationDayOption"></option><option value="7" id="llmServiceCardDurationWeekOption"></option><option value="30" id="llmServiceCardDurationMonthOption" selected></option><option value="91" id="llmServiceCardDurationQuarterOption"></option><option value="365" id="llmServiceCardDurationYearOption"></option></select></div><div style="min-width:0"><label id="llmServiceCardCreditsLabel"></label><input id="llmServiceCardCredits" type="number" min="0" step="1" value="5000"></div><div style="min-width:0"><label id="llmServiceCardFiveHourCreditsLabel"></label><input id="llmServiceCardFiveHourCredits" type="number" min="0" step="1" value="600"></div><div style="min-width:0"><label id="llmServiceCardDailyCreditsLabel"></label><input id="llmServiceCardDailyCredits" type="number" min="0" step="1" value="1200"></div><div style="min-width:0"><label id="llmServiceCardWeeklyCreditsLabel"></label><input id="llmServiceCardWeeklyCredits" type="number" min="0" step="1" value="2400"></div><div style="min-width:0"><label id="llmServiceCardMonthlyCreditsLabel"></label><input id="llmServiceCardMonthlyCredits" type="number" min="0" step="1" value="0"></div><div style="min-width:0"><label id="llmServiceCardCountLabel"></label><input id="llmServiceCardCount" type="number" min="1" max="1000" value="1"></div><div style="display:flex;align-items:flex-end;min-width:0;grid-column:span 2"><button class="btn-primary" onclick="issueLLMServiceCard()" id="llmServiceIssueBtn" style="width:100%;height:36px;padding:0 14px;white-space:nowrap"></button></div></div><div id="llmServiceIssuePlansHint" class="hint" style="margin-top:8px"></div><div id="llmServiceIssuedCodes" class="hint" style="margin-top:8px;padding:8px 10px;min-height:0"></div></div>' +
    '<div class="item"><div class="item-title" id="llmServiceCardsListTitle"></div><div class="grid2" style="margin-top:10px"><div><label id="llmServiceCardSearchLabel"></label><input id="llmServiceCardSearch" oninput="llmServiceSetCardSearch(this.value)"></div><div></div></div><div class="actions" style="margin-top:10px"><button class="btn-ghost" type="button" onclick="llmServiceSetCardFilter(\'all\')" id="llmServiceFilterAllBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceSetCardFilter(\'unused\')" id="llmServiceFilterUnusedBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceSetCardFilter(\'redeemed\')" id="llmServiceFilterRedeemedBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceSelectCurrentPage()" id="llmServiceSelectPageBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceSelectFilteredCards()" id="llmServiceSelectFilteredBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceClearSelectedCards()" id="llmServiceClearSelectedBtn"></button><button class="btn-danger" type="button" onclick="llmServiceDeleteSelectedCards()" id="llmServiceDeleteSelectedBtn"></button><button class="btn-danger" type="button" onclick="llmServiceDeleteFilteredUnusedCards()" id="llmServiceDeleteFilteredBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceDownloadSelectedCards(\'txt\')" id="llmServiceExportSelectedTxtBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceDownloadSelectedCards(\'csv\')" id="llmServiceExportSelectedCsvBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceExportCurrentCards(\'txt\')" id="llmServiceExportCurrentTxtBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceExportCurrentCards(\'csv\')" id="llmServiceExportCurrentCsvBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceExportAllCards(\'all\',\'csv\')" id="llmServiceExportAllCsvBtn"></button></div><div id="llmServiceCardsList"></div><div id="llmServiceCardsPager" class="actions hidden" style="margin-top:10px"><div id="llmServiceCardsPagerMeta" class="hint" style="margin-right:auto"></div><button class="btn-ghost" type="button" onclick="llmServiceChangeCardPage(-1)" id="llmServiceCardsPrevBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceChangeCardPage(1)" id="llmServiceCardsNextBtn"></button></div><div style="margin-top:12px" class="item-title" id="llmServiceGrantsTitle"></div><div class="grid2" style="margin-top:8px"><div><label id="llmServiceGrantSearchLabel"></label><input id="llmServiceGrantSearch" oninput="llmServiceSetGrantSearch(this.value)"></div><div></div></div><div id="llmServiceGrantsList"></div><div id="llmServiceGrantsPager" class="actions hidden" style="margin-top:10px"><div id="llmServiceGrantsPagerMeta" class="hint" style="margin-right:auto"></div><button class="btn-ghost" type="button" onclick="llmServiceChangeGrantPage(-1)" id="llmServiceGrantsPrevBtn"></button><button class="btn-ghost" type="button" onclick="llmServiceChangeGrantPage(1)" id="llmServiceGrantsNextBtn"></button></div></div>';
}
function llmServiceApplyCardDurationDefaults() {
  const daysEl = document.getElementById('llmServiceCardDays');
  const durationDays = Number(daysEl && daysEl.value || 30) || 30;
  const plan = llmServiceCardDefaultPlansByDuration[durationDays];
  if (!plan) return;
  const applyValue = function(id, value, enabled) {
    const el = document.getElementById(id);
    if (!el) return;
    el.value = String(value || 0);
    el.disabled = enabled === false;
  };
  applyValue('llmServiceCardCredits', plan.credits, true);
  applyValue('llmServiceCardFiveHourCredits', plan.fiveHour, true);
  applyValue('llmServiceCardDailyCredits', plan.daily, durationDays >= 7);
  applyValue('llmServiceCardWeeklyCredits', plan.weekly, durationDays >= 30);
  applyValue('llmServiceCardMonthlyCredits', plan.monthly, durationDays >= 91);
}
function applyLLMServiceI18n() {
  _s('llmServiceAdminTitle', 'textContent', lsx('adminTitle'));
  _s('llmServiceAdminDesc', 'textContent', lsx('adminDesc'));
  _s('llmServiceModelRuntimeRefreshBtn', 'textContent', lsx('runtimeRefresh'));
  _s('llmServiceModelRuntimeTriggerBtn', 'textContent', lsx('runtimeTrigger'));
  _s('llmServiceExposeApiBaseLabel', 'textContent', lsx('apiBaseUrl'));
  _s('llmServiceExposeChatUrlLabel', 'textContent', lsx('chatCompletionsUrl'));
  _s('llmServiceExposeModelsUrlLabel', 'textContent', lsx('modelsUrl'));
  _s('llmServiceExposeModelsLabel', 'textContent', lsx('availableModels'));
  _s('llmServiceGroupIDLabel', 'textContent', lsx('id'));
  _s('llmServiceGroupNameLabel', 'textContent', lsx('name'));
  _s('llmServiceGroupDescLabel', 'textContent', lsx('description'));
  _s('llmServiceGroupModelsLabel', 'textContent', lsx('modelsLabel'));
  _s('llmServiceGroupModelsHint', 'textContent', lsx('modelsHint'));
  _s('llmServiceBindingsTitle', 'textContent', lsx('bindings'));
  _s('llmServiceBindingsDesc', 'textContent', lsx('bindingsDesc'));
  _s('llmServiceBindingGroupIDLabel', 'textContent', lsx('securityGroupId'));
  _s('llmServiceBindingServiceGroupsLabel', 'textContent', lsx('serviceGroups'));
  _s('llmServiceAddGroupBindingBtn', 'textContent', lsx('addGroupBinding'));
  _s('llmServiceUsersTitle', 'textContent', lsx('users'));
  _s('llmServiceUserEmailLabel', 'textContent', lsx('email'));
  _s('llmServiceUserServiceGroupsLabel', 'textContent', lsx('serviceGroups'));
  _s('llmServiceAddUserBindingBtn', 'textContent', lsx('addUserBinding'));
  _s('llmServiceCardsTitle', 'textContent', lsx('cards'));
  _s('llmServiceCardLabelLabel', 'textContent', lsx('label'));
  _s('llmServiceCardGroupsLabel', 'textContent', lsx('serviceGroups'));
  _s('llmServiceCardDaysLabel', 'textContent', lsx('cardDuration'));
  _s('llmServiceCardDurationDayOption', 'textContent', lsx('cardDurationDay'));
  _s('llmServiceCardDurationWeekOption', 'textContent', lsx('cardDurationWeek'));
  _s('llmServiceCardDurationMonthOption', 'textContent', lsx('cardDurationMonth'));
  _s('llmServiceCardDurationQuarterOption', 'textContent', lsx('cardDurationQuarter'));
  _s('llmServiceCardDurationYearOption', 'textContent', lsx('cardDurationYear'));
  _s('llmServiceCardCountLabel', 'textContent', lsx('count'));
  _s('llmServiceCardFiveHourCreditsLabel', 'textContent', lsx('fiveHourCredits'));
  _s('llmServiceCardDailyCreditsLabel', 'textContent', lsx('dailyCredits'));
  _s('llmServiceCardWeeklyCreditsLabel', 'textContent', lsx('weeklyCredits'));
  _s('llmServiceCardMonthlyCreditsLabel', 'textContent', lsx('monthlyCredits'));
  _s('llmServiceCardSearchLabel', 'textContent', lsx('cardSearch'));
  _s('llmServiceGrantsTitle', 'textContent', lsx('grants'));
  _s('llmServiceGrantSearchLabel', 'textContent', lsx('grantSearch'));
  _s('llmServiceAddGroupBtn', 'textContent', lsx('addGroup'));
  _s('llmServiceSaveBtn', 'textContent', lsx('saveAll'));
  _s('llmServiceIssueBtn', 'textContent', lsx('issueCard'));
  _s('llmServiceIssuePlansHint', 'textContent', lsx('issueCardPlansHint'));
  _s('llmServiceFilterAllBtn', 'textContent', lsx('filterAll'));
  _s('llmServiceFilterUnusedBtn', 'textContent', lsx('filterUnused'));
  _s('llmServiceFilterRedeemedBtn', 'textContent', lsx('filterRedeemed'));
  _s('llmServiceSelectPageBtn', 'textContent', lsx('selectPage'));
  _s('llmServiceSelectFilteredBtn', 'textContent', lsx('selectFiltered'));
  _s('llmServiceClearSelectedBtn', 'textContent', lsx('clearSelected'));
  _s('llmServiceDeleteSelectedBtn', 'textContent', lsx('deleteSelected'));
  _s('llmServiceDeleteFilteredBtn', 'textContent', lsx('deleteUnusedBatch'));
  _s('llmServiceExportSelectedTxtBtn', 'textContent', lsx('exportSelectedTxt'));
  _s('llmServiceExportSelectedCsvBtn', 'textContent', lsx('exportSelectedCsv'));
  _s('llmServiceExportCurrentTxtBtn', 'textContent', lsx('exportCurrentTxt'));
  _s('llmServiceExportCurrentCsvBtn', 'textContent', lsx('exportCurrentCsv'));
  _s('llmServiceExportAllCsvBtn', 'textContent', lsx('exportAllCsv'));
  _s('llmServiceCardCreditsLabel', 'textContent', lsx('credits'));
  _s('llmServiceDiagnoseTitle', 'textContent', lsx('diagnoseTitle'));
  _s('llmServiceDiagnoseDesc', 'textContent', lsx('diagnoseDesc'));
  _s('llmServiceDiagnoseEmailLabel', 'textContent', lsx('diagnoseEmail'));
  _s('llmServiceDiagnoseBtn', 'textContent', lsx('diagnoseBtn'));
  _s('llmServiceGroupID', 'placeholder', lsx('groupIdPlaceholder'));
  _s('llmServiceGroupName', 'placeholder', lsx('groupNamePlaceholder'));
  _s('llmServiceGroupDesc', 'placeholder', lsx('groupDescPlaceholder'));
  _s('llmServiceBindingGroupID', 'placeholder', lsx('bindingGroupPlaceholder'));
  _s('llmServiceBindingServiceGroups', 'placeholder', lsx('bindingServiceGroupsPlaceholder'));
  _s('llmServiceUserEmail', 'placeholder', lsx('userEmailPlaceholder'));
  _s('llmServiceUserServiceGroups', 'placeholder', lsx('userServiceGroupsPlaceholder'));
  _s('llmServiceCardLabel', 'placeholder', lsx('cardLabelPlaceholder'));
  _s('llmServiceCardGroups', 'placeholder', lsx('cardGroupsPlaceholder'));
  _s('llmServiceCardSearch', 'placeholder', lsx('cardSearchPlaceholder'));
  _s('llmServiceGrantSearch', 'placeholder', lsx('grantSearchPlaceholder'));
  _s('llmServiceCardsPrevBtn', 'textContent', lsx('pagePrev'));
  _s('llmServiceCardsNextBtn', 'textContent', lsx('pageNext'));
  _s('llmServiceGrantsPrevBtn', 'textContent', lsx('pagePrev'));
  _s('llmServiceGrantsNextBtn', 'textContent', lsx('pageNext'));
  _s('llmServiceDiagnoseEmail', 'placeholder', lsx('diagnoseEmailPlaceholder'));
  _s('llmServiceGroupModels', 'placeholder', lsx('modelsPlaceholder'));
}
function llmServiceParseTime(value) {
  if (!value) return 0;
  var t = Date.parse(value);
  return Number.isFinite(t) ? t : 0;
}
function llmServiceGrantIsActive(grant, now) {
  var startsAt = llmServiceParseTime(grant && grant.starts_at);
  var expiresAt = llmServiceParseTime(grant && grant.expires_at);
  if (startsAt && startsAt > now) return false;
  if (expiresAt && expiresAt < now) return false;
  return true;
}
function llmServiceModelHasLiveProvider(model, providerMap) {
  return (model && model.provider_ids || []).some(function(id) { return !!providerMap[normalizeLLMServiceProviderRef(id)]; });
}
function llmServiceBuildProviderMap() {
  var providerMap = {};
  (llmServiceProviderOptions || []).forEach(function(provider) {
    var id = String(provider && provider.id || '').trim();
    if (id) providerMap[id] = true;
  });
  return providerMap;
}
function llmServiceBuildServiceGroupMap(cache) {
  var map = {};
  (cache && cache.model_service_groups || []).forEach(function(group) {
    var id = String(group && group.id || '').trim();
    if (id) map[id] = group;
  });
  return map;
}
function llmServiceJSArg(value) {
  return String(value || '').replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}
function llmServiceFormatLimitValue(value) {
  const n = Number(value || 0);
  if (!(n > 0)) return '';
  return n.toFixed(3).replace(/\.000$/, '').replace(/(\.\d*?)0+$/, '$1');
}
function llmServiceFormatCreditValue(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n)) return '0';
  return n.toFixed(3).replace(/\.000$/, '').replace(/(\.\d*?)0+$/, '$1');
}
function llmServicePeriodLimitsText(limits) {
  limits = limits || {};
  const parts = [];
  const fiveHour = llmServiceFormatLimitValue(limits.five_hour);
  const daily = llmServiceFormatLimitValue(limits.daily);
  const weekly = llmServiceFormatLimitValue(limits.weekly);
  const monthly = llmServiceFormatLimitValue(limits.monthly);
  if (fiveHour) parts.push(lsx('fiveHourCredits') + ': ' + fiveHour);
  if (daily) parts.push(lsx('dailyCredits') + ': ' + daily);
  if (weekly) parts.push(lsx('weeklyCredits') + ': ' + weekly);
  if (monthly) parts.push(lsx('monthlyCredits') + ': ' + monthly);
  return parts.join(' | ');
}
function llmServiceIssuedCodesText() {
  return (llmServiceLastIssuedCodes || []).join('\n');
}
function llmServiceDownloadIssuedCodes(kind) {
  var codes = llmServiceLastIssuedCodes || [];
  if (!codes.length) return;
  var content = kind === 'csv' ? ('code\n' + codes.join('\n')) : llmServiceIssuedCodesText();
  var blob = new Blob([content], { type: kind === 'csv' ? 'text/csv;charset=utf-8' : 'text/plain;charset=utf-8' });
  var url = URL.createObjectURL(blob);
  var a = document.createElement('a');
  a.href = url;
  a.download = 'llm-service-cards-' + new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19) + '.' + (kind === 'csv' ? 'csv' : 'txt');
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
function llmServiceFilteredCards() {
  return llmServiceCardsPageData && llmServiceCardsPageData.items || [];
}
async function llmServiceLoadCardsPage(page) {
  var nextPage = Math.max(1, Number(page || llmServiceCardPage || 1) || 1);
  var url = '/api/admin/llm/service-cards?status=' + encodeURIComponent(llmServiceCardFilter || 'all') + '&search=' + encodeURIComponent(String(llmServiceCardSearch || '').trim()) + '&page=' + encodeURIComponent(nextPage) + '&page_size=' + encodeURIComponent(llmServiceCardPageSize);
  var data = await api(url);
  llmServiceCardsPageData = data || { items: [], total: 0, page: nextPage, page_size: llmServiceCardPageSize };
  llmServiceCardPage = Number(llmServiceCardsPageData.page || nextPage) || 1;
}
async function llmServiceFetchFilteredCards() {
  var page = 1;
  var items = [];
  while (true) {
    var url = '/api/admin/llm/service-cards?status=' + encodeURIComponent(llmServiceCardFilter || 'all') + '&search=' + encodeURIComponent(String(llmServiceCardSearch || '').trim()) + '&page=' + encodeURIComponent(page) + '&page_size=200';
    var data = await api(url);
    var batch = data && data.items || [];
    items = items.concat(batch);
    if (!batch.length || items.length >= Number(data && data.total || 0)) break;
    page += 1;
  }
  return items;
}
async function llmServiceSetCardFilter(filter) {
  llmServiceCardFilter = filter || 'all';
  llmServiceCardPage = 1;
  await llmServiceLoadCardsPage(1);
  renderLLMServiceAdmin();
}
async function llmServiceSetCardSearch(value) {
  llmServiceCardSearch = String(value || '').trim();
  llmServiceCardPage = 1;
  await llmServiceLoadCardsPage(1);
  renderLLMServiceAdmin();
}
async function llmServiceChangeCardPage(step) {
  var totalPages = Math.max(1, Math.ceil(Number(llmServiceCardsPageData && llmServiceCardsPageData.total || 0) / llmServiceCardPageSize));
  llmServiceCardPage = Math.min(totalPages, Math.max(1, llmServiceCardPage + step));
  await llmServiceLoadCardsPage(llmServiceCardPage);
  renderLLMServiceAdmin();
}
function llmServiceSetGrantSearch(value) {
  llmServiceGrantSearch = String(value || '').trim();
  llmServiceGrantPage = 1;
  renderLLMServiceAdmin();
}
function llmServiceChangeGrantPage(step) {
  var filtered = llmServiceFilterGrants(llmServiceAdminCache && llmServiceAdminCache.grants || [], llmServiceGrantSearch || '', llmServiceAdminCache || {});
  var totalPages = Math.max(1, Math.ceil(filtered.length / llmServiceGrantPageSize));
  llmServiceGrantPage = Math.min(totalPages, Math.max(1, llmServiceGrantPage + Number(step || 0)));
  renderLLMServiceAdmin();
}
function llmServiceExportAllCards(status, format, search) {
  var url = '/api/admin/llm/service-cards/export?status=' + encodeURIComponent(status || 'all') + '&format=' + encodeURIComponent(format || 'txt');
  var query = String(search || '').trim();
  if (query) url += '&search=' + encodeURIComponent(query);
  window.open(url, '_blank');
}
function llmServiceExportCurrentCards(format) {
  llmServiceExportAllCards(llmServiceCardFilter || 'all', format || 'txt', llmServiceCardSearch || '');
}
function llmServiceSelectedCardSet() {
  return new Set((llmServiceSelectedCardIDs || []).map(function(id) { return String(id || '').trim(); }).filter(Boolean));
}
function llmServicePruneSelectedCards() {
  var valid = new Set((llmServiceSelectedCardIDs || []).map(function(id) { return String(id || '').trim(); }).filter(Boolean));
  Object.keys(llmServiceSelectedCardMap || {}).forEach(function(id) { if (!valid.has(String(id || '').trim())) delete llmServiceSelectedCardMap[id]; });
  llmServiceSelectedCardIDs = Array.from(valid);
}
function llmServiceToggleCardSelection(id, checked) {
  id = String(id || '').trim();
  if (!id) return;
  var set = llmServiceSelectedCardSet();
  if (checked) {
    set.add(id);
    var card = ((llmServiceCardsPageData && llmServiceCardsPageData.items) || []).find(function(item) { return String(item && item.id || '').trim() === id; });
    if (card) llmServiceSelectedCardMap[id] = card;
  } else {
    set.delete(id);
    delete llmServiceSelectedCardMap[id];
  }
  llmServiceSelectedCardIDs = Array.from(set);
  renderLLMServiceAdmin();
}
function llmServiceSelectCurrentPage() {
  llmServicePruneSelectedCards();
  var set = llmServiceSelectedCardSet();
  ((llmServiceCardsPageData && llmServiceCardsPageData.items) || []).forEach(function(card) { var id = String(card && card.id || '').trim(); if (id) { set.add(id); llmServiceSelectedCardMap[id] = card; } });
  llmServiceSelectedCardIDs = Array.from(set);
  renderLLMServiceAdmin();
}
async function llmServiceSelectFilteredCards() {
  llmServicePruneSelectedCards();
  var set = llmServiceSelectedCardSet();
  var cards = await llmServiceFetchFilteredCards();
  cards.forEach(function(card) {
    var id = String(card && card.id || '').trim();
    if (id) { set.add(id); llmServiceSelectedCardMap[id] = card; }
  });
  llmServiceSelectedCardIDs = Array.from(set);
  renderLLMServiceAdmin();
}
function llmServiceClearSelectedCards() {
  llmServiceSelectedCardIDs = [];
  llmServiceSelectedCardMap = {};
  renderLLMServiceAdmin();
}
function llmServiceSelectedCards() {
  var set = llmServiceSelectedCardSet();
  var pageMap = {};
  ((llmServiceCardsPageData && llmServiceCardsPageData.items) || []).forEach(function(card) {
    var id = String(card && card.id || '').trim();
    if (id) pageMap[id] = card;
  });
  return Array.from(set).map(function(id) {
    return llmServiceSelectedCardMap[id] || pageMap[id] || { id: id };
  }).filter(Boolean);
}
async function llmServiceDownloadSelectedCards(kind) {
  var ids = Array.from(llmServiceSelectedCardSet());
  if (!ids.length) { showToast(lsx('noCardSelected'), 'info'); return; }
  try {
    var headers = { 'Content-Type': 'application/json' };
    if (token()) headers.Authorization = 'Bearer ' + token();
    var res = await fetch('/api/admin/llm/service-cards/export-selected', { method: 'POST', headers: headers, body: JSON.stringify({ ids: ids, format: kind === 'csv' ? 'csv' : 'txt' }) });
    if (!res.ok) {
      var data = {};
      try { data = await res.json(); } catch (_) {}
      throw new Error(data.message || res.statusText || 'export failed');
    }
    var blob = await res.blob();
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'llm-service-cards-selected-' + new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19) + '.' + (kind === 'csv' ? 'csv' : 'txt');
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    showToast(lsx('selectedCount', { count: String(ids.length) }), 'success');
  } catch (err) {
    var msg = err && err.message || 'export failed';
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function llmServiceDeleteSelectedCards() {
  var ids = llmServiceSelectedCards().map(function(card) { return String(card && card.id || '').trim(); }).filter(Boolean);
  if (!ids.length) {
    ids = Array.from(llmServiceSelectedCardSet()).filter(Boolean);
  }
  if (!ids.length) { showToast(lsx('noCardSelected'), 'info'); return; }
  if (!confirm(lsx('deleteSelectedConfirm'))) return;
  try {
    var result = await api('/api/admin/llm/service-cards/delete-batch', { method: 'POST', body: JSON.stringify({ ids: ids }) });
    var deleted = result && result.deleted_ids || [];
    var deletedSet = new Set(deleted.map(function(id) { return String(id || '').trim(); }).filter(Boolean));
    llmServiceSelectedCardIDs = (llmServiceSelectedCardIDs || []).filter(function(id) { return !deletedSet.has(String(id || '').trim()); });
    var msg = lsx('deleteBatchDone', { count: String(deleted.length) });
    setOutput(msg);
    showToast(msg, 'success');
    await loadLLMServiceAdmin();
  } catch (err) {
    var msg = lsx('deleteBatchFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function llmServiceDeleteCard(id) {
  if (!id) return;
  if (!confirm(lsx('deleteCardConfirm'))) return;
  try {
    await api('/api/admin/llm/service-cards/' + encodeURIComponent(id), { method: 'DELETE' });
    setOutput(lsx('deleteCardDone'));
    showToast(lsx('deleteCardDone'), 'success');
    await loadLLMServiceAdmin();
  } catch (err) {
    var msg = lsx('deleteCardFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function llmServiceDeleteGrant(id) {
  id = String(id || '').trim();
  if (!id) return;
  if (!confirm(lsx('deleteGrantConfirm'))) return;
  try {
    await api('/api/admin/llm/service-grants/' + encodeURIComponent(id), { method: 'DELETE' });
    setOutput(lsx('deleteGrantDone'));
    showToast(lsx('deleteGrantDone'), 'success');
    await loadLLMServiceAdmin();
  } catch (err) {
    var msg = lsx('deleteGrantFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function llmServiceDeleteFilteredUnusedCards() {
  var ids = (await llmServiceFetchFilteredCards()).filter(function(card) { return !(card && card.redeemed_at); }).map(function(card) { return String(card && card.id || '').trim(); }).filter(Boolean);
  if (!ids.length) return;
  if (!confirm(lsx('deleteBatchConfirm'))) return;
  try {
    var result = await api('/api/admin/llm/service-cards/delete-batch', { method: 'POST', body: JSON.stringify({ ids: ids }) });
    var count = (result && result.deleted_ids || []).length;
    var msg = lsx('deleteBatchDone', { count: String(count) });
    setOutput(msg);
    showToast(msg, 'success');
    await loadLLMServiceAdmin();
  } catch (err) {
    var msg = lsx('deleteBatchFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function llmServiceCopyIssuedCodes() {
  try {
    var text = llmServiceIssuedCodesText();
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      var area = document.createElement('textarea');
      area.value = text;
      document.body.appendChild(area);
      area.select();
      document.execCommand('copy');
      document.body.removeChild(area);
    }
    showToast(lsx('copyCodesDone'), 'success');
  } catch (err) {
    showToast(lsx('copyCodesFailed', { error: err && err.message || lsx('unknownError') }), 'error');
  }
}
async function llmServiceCopyCardCode(code) {
  try {
    if (!code) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(code);
    } else {
      var area = document.createElement('textarea');
      area.value = code;
      document.body.appendChild(area);
      area.select();
      document.execCommand('copy');
      document.body.removeChild(area);
    }
    showToast(lsx('copyCodesDone'), 'success');
  } catch (err) {
    showToast(lsx('copyCodesFailed', { error: err && err.message || lsx('unknownError') }), 'error');
  }
}
function renderLLMServiceIssuedCodes() {
  var root = document.getElementById('llmServiceIssuedCodes');
  var codes = llmServiceLastIssuedCodes || [];
  if (!root) return;
  if (!codes.length) {
    root.innerHTML = '';
    return;
  }
  root.innerHTML = '<div style="display:flex;gap:6px;align-items:center;justify-content:space-between;flex-wrap:wrap;margin-bottom:6px"><div style="font-weight:600">' + escapeHtml(lsx('issueCodesTitle')) + '</div><div style="display:flex;gap:6px;flex-wrap:wrap"><button type="button" class="btn-ghost" onclick="llmServiceCopyIssuedCodes()">' + escapeHtml(lsx('copyCodes')) + '</button><button type="button" class="btn-ghost" onclick="llmServiceDownloadIssuedCodes(\'txt\')">' + escapeHtml(lsx('downloadTxt')) + '</button><button type="button" class="btn-ghost" onclick="llmServiceDownloadIssuedCodes(\'csv\')">' + escapeHtml(lsx('downloadCsv')) + '</button></div></div><div class="mono" style="white-space:pre-wrap;word-break:break-all">' + escapeHtml(codes.join('\n')) + '</div>';
}
function llmServiceGroupLink(id) {
  var clean = String(id || '').trim();
  if (!clean) return escapeHtml(lsx('emptyValue'));
  var group = llmServiceBuildServiceGroupMap(llmServiceAdminCache)[clean];
  if (!group || isBuiltinLLMServiceGroup(clean)) return '<span class="mono">' + escapeHtml(clean) + '</span>';
  return '<button type="button" class="btn-ghost mono" style="height:24px;font-size:11px;padding:0 8px;margin:2px" onclick="event.stopPropagation();openLLMServiceGroupDialog(\'edit\',\'' + llmServiceJSArg(clean) + '\')">' + escapeHtml(clean) + '</button>';
}
function llmServiceGroupLinks(ids) {
  var list = (ids || []).map(function(id) { return String(id || '').trim(); }).filter(Boolean);
  if (!list.length) return '<span class="mono">' + escapeHtml(lsx('emptyValue')) + '</span>';
  return list.map(llmServiceGroupLink).join(' ');
}
function llmServiceUnknownServiceGroups(ids) {
  var groupMap = llmServiceBuildServiceGroupMap(llmServiceAdminCache);
  groupMap[BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID] = groupMap[BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID] || { id: BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID };
  var unknown = [];
  (ids || []).forEach(function(id) {
    var clean = String(id || '').trim();
    if (!clean) return;
    var exists = Object.keys(groupMap).some(function(groupID) { return groupID.toLowerCase() === clean.toLowerCase(); });
    if (!exists && unknown.indexOf(clean) < 0) unknown.push(clean);
  });
  return unknown;
}
function llmServiceWarnUnknownServiceGroups(ids) {
  var unknown = llmServiceUnknownServiceGroups(ids);
  if (!unknown.length) return false;
  showToast(lsx('unknownServiceGroupRefs', { refs: unknown.join(', ') }), 'error');
  return true;
}
function llmServiceCollectReferencedServiceGroups(cache) {
  var refs = [];
  (cache && cache.global_service_group_ids || []).forEach(function(id) { refs.push(id); });
  (cache && cache.default_new_user_service_groups || []).forEach(function(id) { refs.push(id); });
  (cache && cache.group_bindings || []).forEach(function(binding) { (binding.service_group_ids || []).forEach(function(id) { refs.push(id); }); });
  (cache && cache.user_bindings || []).forEach(function(binding) { (binding.service_group_ids || []).forEach(function(id) { refs.push(id); }); });
  (cache && cache.cards || []).forEach(function(card) { (card.service_group_ids || []).forEach(function(id) { refs.push(id); }); });
  (cache && cache.grants || []).forEach(function(grant) { refs.push(grant.service_group_id); });
  return refs;
}
function llmServiceAppendServiceGroup(inputID, groupID) {
  var input = document.getElementById(inputID);
  if (!input) return;
  var current = parseCSV(input.value || '');
  var clean = String(groupID || '').trim();
  if (!clean) return;
  if (!current.some(function(id) { return id.toLowerCase() === clean.toLowerCase(); })) current.push(clean);
  input.value = current.join(', ');
  if (typeof input.focus === 'function') input.focus();
}
function llmServiceRenderServiceGroupPicker(inputID) {
  var groups = (llmServiceAdminCache && llmServiceAdminCache.model_service_groups || []).filter(function(group) { return String(group && group.id || '').trim(); });
  if (!groups.length) return '<div class="hint">' + escapeHtml(lsx('noSelectableServiceGroups')) + '</div>';
  return '<div class="item-meta" style="margin-bottom:6px">' + escapeHtml(lsx('serviceGroupQuickPick')) + '</div>' + groups.map(function(group) {
    var id = String(group.id || '').trim();
    var label = group.name && group.name !== id ? (id + ' - ' + group.name) : id;
    return '<button type="button" class="btn-ghost mono" style="height:24px;font-size:11px;padding:0 8px;margin:2px" onclick="event.stopPropagation();llmServiceAppendServiceGroup(\'' + llmServiceJSArg(inputID) + '\',\'' + llmServiceJSArg(id) + '\')">' + escapeHtml(label) + '</button>';
  }).join(' ');
}
function renderLLMServiceSystemGroupOptions() {
  var select = document.getElementById('llmServiceSystemGroups');
  if (!select) return;
  var groups = (llmServiceAdminCache && llmServiceAdminCache.model_service_groups || []).filter(function(group) { return String(group && group.id || '').trim(); });
  var options = [];
  var hasBuiltin = false;
  groups.forEach(function(group) {
    var id = String(group.id || '').trim();
    if (!id) return;
    if (id.toLowerCase() === BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID) hasBuiltin = true;
    var label = group.name && group.name !== id ? (id + ' - ' + group.name) : id;
    options.push('<option value="' + llsEsc(id) + '">' + llsEsc(label) + '</option>');
  });
  if (!hasBuiltin) options.unshift('<option value="' + llsEsc(BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID) + '">' + llsEsc(BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID + ' - Default (No Model Access)') + '</option>');
  select.innerHTML = options.join('');
}
function refreshLLMServiceGroupSelectors() {
  var datalist = document.getElementById('llmServiceGroupOptions');
  var groups = (llmServiceAdminCache && llmServiceAdminCache.model_service_groups || []).filter(function(group) { return String(group && group.id || '').trim(); });
  if (datalist) {
    datalist.innerHTML = groups.map(function(group) {
      var id = String(group.id || '').trim();
      var label = group.name && group.name !== id ? String(group.name || '') : '';
      return '<option value="' + llsEsc(id) + '"' + (label ? (' label="' + llsEsc(label) + '"') : '') + '></option>';
    }).join('');
  }
  [
    ['llmServiceBindingServiceGroupsPicker', 'llmServiceBindingServiceGroups'],
    ['llmServiceUserServiceGroupsPicker', 'llmServiceUserServiceGroups'],
    ['llmServiceCardGroupsPicker', 'llmServiceCardGroups'],
    ['llmServiceSystemGroupsPicker', 'llmServiceSystemGroups']
  ].forEach(function(pair) {
    var root = document.getElementById(pair[0]);
    if (root) root.innerHTML = llmServiceRenderServiceGroupPicker(pair[1]);
  });
}
function llmServiceCountLiveRoutesForGroup(group, providerMap) {
  return (group && group.models || []).filter(function(model) { return llmServiceModelHasLiveProvider(model, providerMap); }).length;
}
function llmServiceAnalyzeCard(card, cache) {
  var providerMap = llmServiceBuildProviderMap();
  var groupMap = llmServiceBuildServiceGroupMap(cache);
  var groupIDs = card && card.service_group_ids || [];
  var groups = [];
  var missing = [];
  var liveRouteCount = 0;
  var freeGroupCount = 0;
  var grantGroupCount = 0;
  groupIDs.forEach(function(id) {
    var key = String(id || '').trim();
    var group = groupMap[key];
    if (!group) {
      missing.push(key);
      return;
    }
    groups.push(group);
    if (llsNormalizeAccessPolicy(group && group.access_policy || '') === 'grant_required') grantGroupCount += 1; else freeGroupCount += 1;
    liveRouteCount += llmServiceCountLiveRoutesForGroup(group, providerMap);
  });
  var grants = (cache && cache.grants || []).filter(function(grant) {
    return String(grant && grant.card_id || '').trim() === String(card && card.id || '').trim();
  });
  var activeGrants = grants.filter(function(grant) { return llmServiceGrantIsActive(grant, Date.now()); });
  var issues = [];
  if (missing.length) issues.push(lsx('cardIssueMissingGroups'));
  if (!missing.length && groups.length && liveRouteCount === 0) issues.push(lsx('cardIssueNoLiveRoutes'));
  if (card && card.redeemed_at && activeGrants.length === 0) issues.push(lsx('cardIssueRedeemedNoActiveGrant'));
  var health = issues.length ? (liveRouteCount > 0 ? 'partial' : 'broken') : 'ready';
  return { health: health, groups: groups, missing: missing, liveRouteCount: liveRouteCount, activeGrants: activeGrants, issues: issues, freeGroupCount: freeGroupCount, grantGroupCount: grantGroupCount };
}
function llmServiceFindCardByID(cache, cardID) {
  var needle = String(cardID || '').trim();
  if (!needle) return null;
  var cards = cache && cache.cards || [];
  for (var i = 0; i < cards.length; i++) {
    if (String(cards[i] && cards[i].id || '').trim() === needle) return cards[i];
  }
  return null;
}
function llmServiceGrantSourceMeta(grant, cache) {
  var groupMap = llmServiceBuildServiceGroupMap(cache);
  var group = groupMap[String(grant && grant.service_group_id || '').trim()];
  var accessPolicy = llsNormalizeAccessPolicy(group && group.access_policy || '');
  var grantCardID = String(grant && grant.card_id || '').trim();
  var card = llmServiceFindCardByID(cache, grantCardID);
  return {
    accessPolicy: accessPolicy,
    accessLabel: llsAccessPolicyLabel(accessPolicy),
    cardLabel: card ? (card.label || card.id || grantCardID || '-') : (grantCardID || '-'),
    cardID: card ? String(card.id || grantCardID || '') : grantCardID
  };
}
function llmServiceGrantStatusKey(grant, now) {
  var raw = String(grant && grant.status || '').trim().toLowerCase();
  if (raw) return raw;
  if (!grant) return 'inactive';
  var startsAt = llmServiceParseTime(grant.starts_at);
  var expiresAt = llmServiceParseTime(grant.expires_at);
  if (startsAt && startsAt > now) return 'queued';
  if (expiresAt && expiresAt < now) return 'expired';
  var total = Number(grant.credits_total || 0);
  var used = Number(grant.credits_used || 0);
  if (total > 0 && Math.max(0, total - used) <= 0) return 'exhausted';
  return 'active';
}
function llmServiceGrantStatusMeta(grant, now) {
  var key = llmServiceGrantStatusKey(grant, now || Date.now());
  var labelKey = {
    active: 'grantStatusActive',
    period_limited: 'grantStatusPeriodLimited',
    queued: 'grantStatusQueued',
    exhausted: 'grantStatusExhausted',
    expired: 'grantStatusExpired'
  }[key] || 'grantStatusInactive';
  var badgeClass = key === 'active' ? 'ok' : (key === 'period_limited' || key === 'queued' ? 'warn' : 'danger');
  var details = [];
  var reason = String(grant && grant.status_reason || '').trim();
  if (reason) details.push(reason);
  var retryAfterAt = String(grant && grant.retry_after_at || '').trim();
  if (retryAfterAt) details.push(lsx('grantRetryAfterAt', { time: retryAfterAt }));
  return { key: key, label: lsx(labelKey), badgeClass: badgeClass, detail: details.join(' | ') };
}
function llmServiceGrantSearchText(grant, cache) {
  var sourceMeta = llmServiceGrantSourceMeta(grant, cache);
  var statusMeta = llmServiceGrantStatusMeta(grant, Date.now());
  return [
    grant && grant.id,
    grant && grant.email,
    grant && grant.card_id,
    grant && grant.service_group_id,
    grant && grant.source,
    statusMeta.label,
    statusMeta.detail,
    sourceMeta.cardID,
    sourceMeta.cardLabel,
    sourceMeta.accessLabel
  ].map(function(value) { return String(value || '').trim(); }).filter(Boolean).join(' ').toLowerCase();
}
function llmServiceFilterGrants(grants, search, cache) {
  var tokens = String(search || '').trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!tokens.length) return grants || [];
  return (grants || []).filter(function(grant) {
    var text = llmServiceGrantSearchText(grant, cache || {});
    return tokens.every(function(token) { return text.indexOf(token) >= 0; });
  });
}
function llmServiceBillingRouteAccessLabel(route) {
  return llsNormalizeAccessPolicy(route && route.access_policy || '') === 'grant_required' ? lsx('billingRouteGrant') : lsx('billingRouteBinding');
}
function llmServiceBillingRouteStatus(route, ui) {
  if (route && route.eligible) {
    var extra = Number(route.credits_available || 0) > 0 ? (' | ' + lsx('billingRouteCredits', { credits: String(Number(route.credits_available || 0)) })) : '';
    return ui.badge(lsx('billingRouteEligible'), 'ok') + '<div class="item-meta mono" style="margin-top:4px">' + escapeHtml((route.reason_message || '') || (llmServiceBillingRouteAccessLabel(route) + extra)) + '</div>';
  }
  var reason = String(route && (route.reason_message || route.reason_code) || '').trim() || lsx('billingRouteBlocked');
  return ui.badge(lsx('billingRouteBlocked'), 'warn') + '<div class="item-meta mono" style="margin-top:4px">' + escapeHtml(reason) + '</div>';
}
function llmServiceGrantRouteEntries(grant, cache) {
  var groupMap = llmServiceBuildServiceGroupMap(cache);
  var providerMap = llmServiceBuildProviderMap();
  var group = groupMap[String(grant && grant.service_group_id || '').trim()];
  if (!group) return { missingGroup: true, routes: [] };
  var routes = (group.models || []).map(function(model) {
    var providers = (model.provider_ids || []).filter(function(id) { return !!providerMap[normalizeLLMServiceProviderRef(id)]; });
    return {
      name: String(model && model.name || '').trim(),
      providers: providers,
      multiplier: Number(model && model.credit_multiplier || 1) || 1
    };
  }).filter(function(item) { return item.name && item.providers.length; });
  return { missingGroup: false, routes: routes };
}
function llmServiceRenderGrantRoutes(grant, cache, ui) {
  var analyzed = llmServiceGrantRouteEntries(grant, cache);
  if (analyzed.missingGroup) return '<div class="item-meta" style="margin-top:6px;color:#c05621">' + escapeHtml(lsx('grantRouteMissingGroup')) + '</div>';
  if (!(analyzed.routes || []).length) return '<div class="item-meta" style="margin-top:6px;color:#c05621">' + escapeHtml(lsx('grantRouteNoLiveModels')) + '</div>';
  return analyzed.routes.map(function(route) {
    return '<div class="item-meta" style="margin-top:6px"><span class="mono">' + escapeHtml(route.name || '-') + '</span> | ' + escapeHtml(lsx('providerRoute')) + ': <span class="mono">' + escapeHtml((route.providers || []).join(', ')) + '</span> | ' + escapeHtml(lsx('multiplierLabel')) + ': <span class="mono">' + escapeHtml(String(route.multiplier || 1)) + '</span></div>';
  }).join('');
}

function renderLLMServiceChainOverview(cache) {
  var providerMap = {};
  (llmServiceProviderOptions || []).forEach(function(provider) {
    var id = String(provider && provider.id || '').trim();
    if (id) providerMap[id] = true;
  });
  var groups = (cache && cache.model_service_groups || []).filter(function(group) { return !isBuiltinLLMServiceGroup(group && group.id); });
  var cards = cache && cache.cards || [];
  var grants = cache && cache.grants || [];
  var groupBindings = cache && cache.group_bindings || [];
  var userBindings = cache && cache.user_bindings || [];
  var globalGroups = (cache && cache.global_service_group_ids || []).filter(function(id) { return !isBuiltinLLMServiceGroup(id); });
  var defaultGroups = (cache && cache.default_new_user_service_groups || []).filter(function(id) { return !isBuiltinLLMServiceGroup(id); });
  var providerIssues = cache && cache.provider_link_issues || [];
  var now = Date.now();
  var routeCount = 0;
  var liveRouteCount = 0;
  var freeGroupCount = 0;
  var grantGroupCount = 0;
  var activeGrantModelNames = {};
  var groupMap = {};
  groups.forEach(function(group) {
    groupMap[String(group.id || '').trim()] = group;
    if (llsNormalizeAccessPolicy(group && group.access_policy || '') === 'grant_required') grantGroupCount += 1; else freeGroupCount += 1;
    (group.models || []).forEach(function(model) {
      routeCount += 1;
      if (llmServiceModelHasLiveProvider(model, providerMap)) liveRouteCount += 1;
    });
  });
  var directBindingCount = userBindings.filter(function(binding) { return (binding && binding.service_group_ids || []).length; }).length;
  var securityBindingCount = groupBindings.filter(function(binding) { return (binding && binding.service_group_ids || []).length; }).length;
  var globalCoverageCount = globalGroups.length;
  var defaultCoverageCount = defaultGroups.length;
  var bindingPathCount = directBindingCount + securityBindingCount + globalCoverageCount + defaultCoverageCount;
  var activeGrants = grants.filter(function(grant) { return llmServiceGrantIsActive(grant, now); });
  activeGrants.forEach(function(grant) {
    var group = groupMap[String(grant && grant.service_group_id || '').trim()];
    if (!group) return;
    (group.models || []).forEach(function(model) {
      if (!llmServiceModelHasLiveProvider(model, providerMap)) return;
      var name = String(model && model.name || '').trim();
      if (name) activeGrantModelNames[name.toLowerCase()] = name;
    });
  });
  var cardsWithRoutes = cards.filter(function(card) {
    return (card && card.service_group_ids || []).some(function(id) {
      var group = groupMap[String(id || '').trim()];
      return !!(group && (group.models || []).some(function(model) { return llmServiceModelHasLiveProvider(model, providerMap); }));
    });
  });
  var freeAccessReady = freeGroupCount === 0 || bindingPathCount > 0;
  var grantAccessReady = grantGroupCount === 0 || activeGrants.length > 0;
  var accessPathReady = freeAccessReady && grantAccessReady;
  var providerCount = Object.keys(providerMap).length;
  var ready = providerCount > 0 && liveRouteCount > 0 && accessPathReady && providerIssues.length === 0;
  var issues = [];
  if (!providerCount) issues.push(lsx('chainIssueNoProviders'));
  if (providerIssues.length) issues.push(lsx('chainIssueProviderLinkage'));
  if (providerCount > 0 && routeCount === 0) issues.push(lsx('chainIssueNoRoutes'));
  if (routeCount > 0 && liveRouteCount === 0) issues.push(lsx('chainIssueNoLiveRoutes'));
  if (freeGroupCount > 0 && bindingPathCount === 0) issues.push(lsx('chainIssueNoBindings'));
  if (grantGroupCount > 0 && cards.length === 0 && activeGrants.length === 0) {
    issues.push(lsx('chainIssueNoCards'));
    issues.push(lsx('chainIssueGrantGroupsNeedCards'));
  }
  if (grantGroupCount > 0 && cards.length > 0 && activeGrants.length === 0) issues.push(lsx('chainIssueNoGrants'));
  if (activeGrants.length > 0 && Object.keys(activeGrantModelNames).length === 0) issues.push(lsx('chainIssueNoAuthorizedModels'));
  var metrics = [
    { label: lsx('chainProviders'), value: String(providerCount), tone: providerCount ? 'rgba(47,128,237,.12)' : 'rgba(235,87,87,.10)' },
    { label: lsx('chainLiveRoutes'), value: String(liveRouteCount) + ' / ' + String(routeCount), tone: liveRouteCount ? 'rgba(39,174,96,.10)' : 'rgba(235,87,87,.10)' },
    { label: lsx('chainFreeGroups'), value: String(freeGroupCount), tone: freeGroupCount ? 'rgba(39,174,96,.10)' : 'rgba(31,34,48,.05)' },
    { label: lsx('chainGrantGroups'), value: String(grantGroupCount), tone: grantGroupCount ? 'rgba(242,153,74,.14)' : 'rgba(31,34,48,.05)' },
    { label: lsx('chainCards'), value: String(cardsWithRoutes.length) + ' / ' + String(cards.length), tone: cardsWithRoutes.length ? 'rgba(47,128,237,.12)' : 'rgba(242,153,74,.12)' },
    { label: lsx('chainActiveGrants'), value: String(activeGrants.length), tone: activeGrants.length ? 'rgba(39,174,96,.10)' : 'rgba(242,153,74,.12)' }
  ];
  var steps = [
    { label: lsx('chainStepProviders'), ready: providerCount > 0 },
    { label: lsx('chainStepRoutes'), ready: liveRouteCount > 0 },
    { label: lsx('chainStepAccess'), ready: accessPathReady },
    { label: lsx('chainStepCards'), ready: grantGroupCount === 0 || cards.length > 0 },
    { label: lsx('chainStepGrants'), ready: grantGroupCount === 0 || activeGrants.length > 0 }
  ];
  var accessLines = [
    '<div class="item-meta" style="margin-top:6px"><strong>' + escapeHtml(lsx('chainStepAccessBindings')) + ':</strong> ' + escapeHtml(freeAccessReady ? lsx('chainStepAccessFreeReady') : lsx('chainStepAccessFreeMissing')) + '</div>',
    '<div class="item-meta" style="margin-top:6px"><strong>' + escapeHtml(lsx('chainStepAccessGrant')) + ':</strong> ' + escapeHtml(grantAccessReady ? lsx('chainStepAccessGrantReady') : lsx('chainStepAccessGrantMissing')) + '</div>',
    '<div class="item-meta" style="margin-top:6px">' + escapeHtml(lsx('chainAccessSummary')) + '</div>'
  ].join('');
  return '<div class="item" style="margin-bottom:14px;border:' + (ready ? '1px solid rgba(39,174,96,.28)' : '1px solid rgba(242,153,74,.30)') + ';background:' + (ready ? 'rgba(39,174,96,.06)' : 'rgba(242,153,74,.06)') + '">' +
    '<div class="item-head"><div><div class="item-title">' + escapeHtml(lsx('chainOverviewTitle')) + '</div><div class="item-meta">' + escapeHtml(lsx('chainOverviewDesc')) + '</div></div><span class="badge ' + (ready ? 'ok' : 'warn') + '">' + escapeHtml(ready ? lsx('chainReady') : lsx('chainNotReady')) + '</span></div>' +
    '<div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:10px">' + steps.map(function(step) {
      return '<span class="badge ' + (step.ready ? 'ok' : 'warn') + '">' + escapeHtml(step.label + ' - ' + (step.ready ? lsx('chainStepReady') : lsx('chainStepMissing'))) + '</span>';
    }).join('') + '</div>' +
    '<div class="grid2" style="margin-top:10px">' + metrics.map(function(metric) {
      return '<div style="padding:12px;border-radius:12px;background:' + metric.tone + '"><div class="item-meta">' + escapeHtml(metric.label) + '</div><div class="item-title" style="margin-top:4px">' + escapeHtml(metric.value) + '</div></div>';
    }).join('') + '</div>' +
    '<div style="margin-top:10px">' + accessLines + '</div>' +
    '<div style="margin-top:10px">' + (issues.length ? issues.map(function(issue) {
      return '<div class="item-meta" style="margin-top:6px;color:#c05621">' + escapeHtml(issue) + '</div>';
    }).join('') : '<div class="item-meta" style="color:#2f855a">' + escapeHtml(lsx('chainHealthy')) + '</div>') + '</div>' +
  '</div>';
}
async function loadLLMServiceAdmin() {
  ensureLLMServiceAdminUI();
  ensureLLMServiceSystemUI();
  renderLLMServiceModelRuntime();
  try {
    await Promise.all([loadLLMServiceProviderOptions(), loadLLMServiceSecurityGroupOptions()]);
    const results = await Promise.all([
      api('/api/admin/llm/services?include_cards=false'),
      llmServiceLoadCardsPage(llmServiceCardPage || 1),
      loadLLMServiceModelRuntime({ silent: true })
    ]);
    const data = results[0];
    llmServiceAdminCache = data || { model_service_groups: [], global_service_group_ids: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
    if (llmServiceSelectedGroupID && !(llmServiceAdminCache.model_service_groups || []).some(function(g) { return g.id === llmServiceSelectedGroupID; })) llmServiceSelectedGroupID = '';
    renderLLMServiceAdmin();
    renderLLMServiceSystemSettings();
  } catch (err) {
    const msg = lsx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
function renderLLMServiceAdmin() {
  if (!llmServiceAdminCache) return;
  ensureLLMServiceAdminUI();
  const ui = aui();
  if (!ui || typeof ui.renderList !== 'function' || typeof ui.simpleCard !== 'function' || typeof ui.hint !== 'function' || typeof ui.actionButton !== 'function' || typeof ui.badge !== 'function' || typeof ui.meta !== 'function') return;
  applyLLMServiceI18n();
  renderLLMServiceIssuedCodes();
  refreshLLMServiceGroupSelectors();
  refreshLLMServiceSecurityGroupSelectors();
  const groups = llmServiceAdminCache.model_service_groups || [];
  const linkageRoot = document.getElementById('llmServiceLinkageIssues');
  if (linkageRoot) {
    const issues = llmServiceAdminCache.provider_link_issues || [];
    linkageRoot.innerHTML = issues.length ? ('<div class="item" style="border:1px solid rgba(242,153,74,.45);background:rgba(242,153,74,.08)"><div class="item-title">' + escapeHtml(lsx('linkageIssuesTitle')) + '</div><div style="margin-top:8px">' + issues.map(function(issue) { return '<div class="item-meta" style="color:#c05621;margin-top:6px">' + escapeHtml(issue) + '</div>'; }).join('') + '</div></div>') : ('<div class="hint">' + escapeHtml(lsx('linkageIssuesEmpty')) + '</div>');
  }
  _s('llmServiceExposeApiBase', 'textContent', llmServiceAdminCache.expose_api_base_url || lsx('emptyValue'));
  _s('llmServiceExposeChatUrl', 'textContent', llmServiceAdminCache.expose_base_url || lsx('emptyValue'));
  _s('llmServiceExposeModelsUrl', 'textContent', llmServiceAdminCache.expose_models_url || lsx('emptyValue'));
  _s('llmServiceExposeModels', 'textContent', (llmServiceAdminCache.available_models || []).length ? llmServiceAdminCache.available_models.join(', ') : lsx('emptyValue'));
  const chainOverview = document.getElementById('llmServiceChainOverview');
  if (chainOverview) chainOverview.innerHTML = renderLLMServiceChainOverview(llmServiceAdminCache);
  const providerReference = document.getElementById('llmServiceProviderReference');
  if (providerReference) {
    providerReference.textContent = llmServiceProviderOptions.length
      ? lsx('providerReference', { providers: llmServiceProviderOptions.map(llmServiceProviderDisplay).join(', ') })
      : lsx('providerReferenceEmpty');
  }
  const selected = groups.find(function(g) { return g.id === llmServiceSelectedGroupID; }) || null;
  const shouldRefreshDraft = !llmServiceDraftDirty || llmServiceRenderedGroupID !== (selected && selected.id || '');
  if (selected) {
    const builtin = isBuiltinLLMServiceGroup(selected.id);
    llmServiceSelectedGroupID = selected.id;
    if (shouldRefreshDraft) writeLLMServiceGroupDraft(selected);
    const idEl = document.getElementById('llmServiceGroupID');
    const nameEl = document.getElementById('llmServiceGroupName');
    const descEl = document.getElementById('llmServiceGroupDesc');
    const modelsEl = document.getElementById('llmServiceGroupModels');
    const addBtn = document.getElementById('llmServiceAddGroupBtn');
    const removeBtn = document.getElementById('llmServiceRemoveGroupBtn');
    if (idEl) idEl.disabled = builtin;
    if (nameEl) nameEl.disabled = builtin;
    if (descEl) descEl.disabled = builtin;
    if (modelsEl) modelsEl.disabled = builtin;
    if (addBtn) addBtn.textContent = builtin ? lsx('builtInDefaultNoAccess') : lsx('addGroup');
    if (removeBtn) { removeBtn.disabled = builtin; removeBtn.textContent = builtin ? lsx('builtInDefault') : lsx('removeGroup'); }
  } else {
    if (shouldRefreshDraft) writeLLMServiceGroupDraft(null);
    const idEl = document.getElementById('llmServiceGroupID');
    const nameEl = document.getElementById('llmServiceGroupName');
    const descEl = document.getElementById('llmServiceGroupDesc');
    const modelsEl = document.getElementById('llmServiceGroupModels');
    const addBtn = document.getElementById('llmServiceAddGroupBtn');
    const removeBtn = document.getElementById('llmServiceRemoveGroupBtn');
    if (idEl) idEl.disabled = false;
    if (nameEl) nameEl.disabled = false;
    if (descEl) descEl.disabled = false;
    if (modelsEl) modelsEl.disabled = false;
    if (addBtn) addBtn.textContent = lsx('addGroup');
    if (removeBtn) { removeBtn.disabled = true; removeBtn.textContent = lsx('removeGroup'); }
  }
  const groupsRoot = document.getElementById('llmServiceGroupsList');
  if (groupsRoot) {
    if (!groups.length) {
      groupsRoot.innerHTML = ui.hint(lsx('noServiceGroups'));
    } else {
      const groupHeader = '<div class="row header" style="grid-template-columns:1.1fr .9fr 1.6fr auto"><div>' + escapeHtml(lsx('name')) + '</div><div>' + escapeHtml(lsx('id')) + '</div><div>' + escapeHtml(lsx('modelsLabel')) + '</div><div></div></div>';
      const groupRows = groups.map(function(g) {
        const active = g.id === llmServiceSelectedGroupID;
        const escapedID = String(g.id || '').replace(/'/g, "\'");
        const modelList = (g.models || []).length ? (g.models || []).map(function(model) { return llmServiceModelSummary(model); }).join(' | ') : lsx('noAuthorizedModelDetails');
        const desc = g.description ? ('<div class="item-meta" style="margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(g.description) + '</div>') : '';
        const accessPolicy = llsNormalizeAccessPolicy(g && g.access_policy || '');
        const accessBadgeTone = accessPolicy === 'grant_required' ? 'warn' : 'ok';
        return '<div class="item" style="margin-bottom:6px;padding:0;overflow:hidden;border:' + (active ? '1px solid rgba(47,128,237,.36)' : '1px solid var(--line)') + ';box-shadow:' + (active ? '0 0 0 1px rgba(47,128,237,.08)' : 'none') + ';cursor:pointer" onclick="selectLLMServiceGroup(\'' + escapedID + '\')">'
          + '<div class="row" style="grid-template-columns:1.1fr .9fr 1.6fr auto;gap:10px;padding:10px 12px;border:none;background:' + (active ? '#f8fbff' : '#fff') + '">'
          + '<div style="min-width:0"><div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(g.name || g.id) + '</div>' + ui.badge(lsx('modelsCount', { count: String((g.models || []).length) }), 'info') + ui.badge(llsAccessPolicyLabel(accessPolicy), accessBadgeTone) + '</div>' + desc + '</div>'
          + '<div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(g.id || '') + '</div>'
          + '<div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(modelList) + '</div>'
          + '<div style="display:flex;justify-content:flex-end;gap:6px;flex-wrap:wrap"><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="event.stopPropagation();openLLMServiceGroupDialog(\'edit\',\'' + escapedID + '\')">' + escapeHtml(lsx('edit')) + '</button>' + (isBuiltinLLMServiceGroup(g.id) ? '' : '<button class="btn-danger" style="height:28px;font-size:11px;padding:0 10px" onclick="event.stopPropagation();llsRemoveGroupById(\'' + escapedID + '\')">' + escapeHtml(lsx('remove')) + '</button>') + '</div>'
          + '</div>'
          + '</div>';
      }).join('');
      groupsRoot.innerHTML = '<div class="table" style="gap:6px">' + groupHeader + groupRows + '</div>';
    }
  }
  const gbRoot = document.getElementById('llmServiceGroupBindingsList');
  if (gbRoot) {
    const bindings = llmServiceAdminCache.group_bindings || [];
    if (!bindings.length) {
      gbRoot.innerHTML = ui.hint(lsx('noSecurityGroupBindings'));
    } else {
      const bindingHeader = '<div class="row header" style="grid-template-columns:1fr 1.5fr .72fr auto"><div>' + escapeHtml(lsx('securityGroupId')) + '</div><div>' + escapeHtml(lsx('serviceGroups')) + '</div><div>' + escapeHtml(lsx('statusLabel')) + '</div><div></div></div>';
      const bindingRows = bindings.map(function(b, idx) {
        const securityGroup = llmServiceDescribeSecurityGroup(b.group_id);
        const statusClass = securityGroup.missing ? 'warn' : 'ok';
        const statusText = securityGroup.missing ? lsx('chainStepMissing') : lsx('chainStepReady');
        const issueLine = securityGroup.missing ? '<div class="item-meta" style="margin-top:4px;color:#c05621">' + escapeHtml(lsx('missingSecurityGroup')) + '</div>' : '';
        return '<div class="row" style="grid-template-columns:1fr 1.5fr .72fr auto;gap:10px' + (securityGroup.missing ? ';background:#fffaf4;border-color:rgba(242,153,74,.16)' : '') + '">'
          + '<div style="min-width:0"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(securityGroup.label) + '</div><div class="item-meta mono" style="margin-top:4px">' + escapeHtml(b.group_id || '') + '</div>' + issueLine + '</div>'
          + '<div class="item-meta" style="min-width:0">' + llmServiceGroupLinks(b.service_group_ids || []) + '</div>'
          + '<div><span class="badge ' + statusClass + '">' + escapeHtml(statusText) + '</span></div>'
          + '<div style="display:flex;justify-content:flex-end"><button class="btn-danger" style="height:28px;font-size:11px;padding:0 10px" onclick="removeLLMServiceGroupBinding(' + idx + ')">' + escapeHtml(lsx('remove')) + '</button></div>'
          + '</div>';
      }).join('');
      gbRoot.innerHTML = '<div class="table" style="gap:6px">' + bindingHeader + bindingRows + '</div>';
    }
  }
  const ubRoot = document.getElementById('llmServiceUserBindingsList');
  if (ubRoot) {
    const bindings = llmServiceAdminCache.user_bindings || [];
    if (!bindings.length) {
      ubRoot.innerHTML = ui.hint(lsx('noDirectUserBindings'));
    } else {
      const userHeader = '<div class="row header" style="grid-template-columns:1fr 1.6fr auto"><div>' + escapeHtml(lsx('email')) + '</div><div>' + escapeHtml(lsx('serviceGroups')) + '</div><div></div></div>';
      const userRows = bindings.map(function(b, idx) {
        return '<div class="row" style="grid-template-columns:1fr 1.6fr auto;gap:10px"><div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(b.email || '') + '</div><div class="item-meta">' + llmServiceGroupLinks(b.service_group_ids || []) + '</div><div style="display:flex;justify-content:flex-end"><button class="btn-danger" style="height:28px;font-size:11px;padding:0 10px" onclick="removeLLMServiceUserBinding(' + idx + ')">' + escapeHtml(lsx('remove')) + '</button></div></div>';
      }).join('');
      ubRoot.innerHTML = '<div class="table" style="gap:6px">' + userHeader + userRows + '</div>';
    }
  }
  var filterButtons = {
    all: document.getElementById('llmServiceFilterAllBtn'),
    unused: document.getElementById('llmServiceFilterUnusedBtn'),
    redeemed: document.getElementById('llmServiceFilterRedeemedBtn')
  };
  Object.keys(filterButtons).forEach(function(key) {
    var btn = filterButtons[key];
    if (!btn) return;
    btn.className = key === llmServiceCardFilter ? 'btn-secondary' : 'btn-ghost';
  });
  var cardSearchInput = document.getElementById('llmServiceCardSearch');
  if (cardSearchInput && cardSearchInput.value !== llmServiceCardSearch) cardSearchInput.value = llmServiceCardSearch;
  var filteredCards = llmServiceFilteredCards();
  var totalCards = Number(llmServiceCardsPageData && llmServiceCardsPageData.total || 0);
  var totalPages = Math.max(1, Math.ceil(totalCards / llmServiceCardPageSize));
  if (llmServiceCardPage > totalPages) llmServiceCardPage = totalPages;
  if (llmServiceCardPage < 1) llmServiceCardPage = 1;
  var startIndex = (llmServiceCardPage - 1) * llmServiceCardPageSize;
  var pageItems = filteredCards;
  const cardsRoot = document.getElementById('llmServiceCardsList');
  const selectedCardSet = llmServiceSelectedCardSet();
  if (cardsRoot) {
    if (!pageItems.length) {
      cardsRoot.innerHTML = ui.hint(lsx('noRedeemCards'));
    } else {
      var cardRows = pageItems.map(function(c) {
        const cardHealth = llmServiceAnalyzeCard(c, llmServiceAdminCache);
        const healthTone = cardHealth.health === 'ready' ? 'ok' : (cardHealth.health === 'partial' ? 'info' : 'warn');
        const healthLabel = cardHealth.health === 'ready' ? lsx('cardHealthReady') : (cardHealth.health === 'partial' ? lsx('cardHealthPartial') : lsx('cardHealthBroken'));
        const redeemedTone = c.redeemed_at ? 'warn' : 'ok';
        const redeemedLabel = c.redeemed_at ? lsx('redeemed') : lsx('unused');
        const redemptionMeta = (c.redeemed_by_email || '') + (c.redeemed_at ? (' | ' + String(c.redeemed_at)) : '');
        const issueLines = (cardHealth.issues || []).map(function(issue) { return '<span style="color:#c05621">' + escapeHtml(issue) + '</span>'; }).join('<span style="color:rgba(31,34,48,.16)"> | </span>');
        const unlockLabel = cardHealth.grantGroupCount > 0 && cardHealth.freeGroupCount > 0 ? lsx('cardUnlocksMixed') : (cardHealth.grantGroupCount > 0 ? lsx('cardUnlocksGrant') : lsx('cardUnlocksFree'));
        const periodLimits = llmServicePeriodLimitsText(c.period_limits);
        const selected = selectedCardSet.has(String(c && c.id || '').trim());
        const cardCode = String(c.code || '').trim();
        const cardCodeLine = cardCode
          ? '<div style="margin-top:6px;display:flex;align-items:center;gap:6px;flex-wrap:wrap"><span class="item-meta">' + escapeHtml(lsx('cardCodeLabel')) + ':</span><span class="mono" style="font-size:11px;letter-spacing:1px;user-select:all">' + escapeHtml(cardCode) + '</span><button type="button" class="btn-ghost" style="height:22px;font-size:10px;padding:0 6px" onclick="event.stopPropagation();llmServiceCopyCardCode(\'' + llmServiceJSArg(cardCode) + '\')">' + escapeHtml(lsx('copyCodes')) + '</button></div>'
          : '';
        return '<div class="item" style="margin-bottom:0;padding:0;overflow:hidden;border:1px solid var(--line);border-radius:8px;background:#fff;min-width:0">'
          + '<div style="display:grid;grid-template-columns:28px minmax(0,1.55fr) minmax(0,.95fr);gap:8px;padding:9px 10px 8px;background:#fff;align-items:start">'
          + '<div style="display:flex;align-items:flex-start;justify-content:center;padding-top:2px"><input type="checkbox" ' + (selected ? 'checked' : '') + ' onchange="event.stopPropagation();llmServiceToggleCardSelection(\'' + llmServiceJSArg(c.id || '') + '\', this.checked)"></div>'
          + '<div style="min-width:0">'
          + '<div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(c.label || c.id || lsx('card')) + '</div>'
          + '<div class="item-meta mono" style="margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(String(c.id || '')) + '</div>'
          + '<div class="item-meta" style="margin-top:5px;line-height:1.35;word-break:break-word">' + escapeHtml(redemptionMeta || '-') + '</div>'
          + '<div class="item-meta" style="margin-top:6px;min-width:0">' + llmServiceGroupLinks(c.service_group_ids || []) + '</div>'
          + '</div>'
          + '<div style="display:grid;gap:6px;justify-items:end;min-width:0">'
          + '<div style="display:flex;gap:4px;flex-wrap:wrap;justify-content:flex-end">' + ui.badge(healthLabel, healthTone) + ui.badge(redeemedLabel, redeemedTone) + '</div>'
          + '<div class="item-meta mono" style="text-align:right">' + escapeHtml(lsx('days')) + ': ' + escapeHtml(String(c.duration_days || 0)) + '</div>'
          + '<div class="item-meta mono" style="text-align:right">' + escapeHtml(lsx('credits')) + ': ' + escapeHtml(String(c.credits || 0)) + '</div>'
          + (periodLimits ? '<div class="item-meta mono" style="text-align:right;line-height:1.35">' + escapeHtml(lsx('periodLimits')) + ': ' + escapeHtml(periodLimits) + '</div>' : '')
          + '<div style="display:flex;justify-content:flex-end;gap:6px;flex-wrap:wrap;width:100%">' + (c.redeemed_at ? '' : '<button class="btn-danger" style="height:28px;font-size:11px;padding:0 10px" onclick="llmServiceDeleteCard(\'' + llmServiceJSArg(c.id || '') + '\')">' + escapeHtml(lsx('deleteCard')) + '</button>') + '</div>'
          + '</div>'
          + '</div>'
          + '<div style="padding:7px 10px 9px;border-top:1px solid rgba(31,34,48,.06);background:#fff">'
          + '<div class="item-meta" style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;line-height:1.35">'
          + '<span>' + escapeHtml(lsx('cardHealthGroups')) + ': ' + escapeHtml(String((cardHealth.groups || []).length)) + ' / ' + escapeHtml(String((c.service_group_ids || []).length)) + '</span>'
          + '<span>' + escapeHtml(lsx('cardHealthRoutes')) + ': ' + escapeHtml(String(cardHealth.liveRouteCount || 0)) + '</span>'
          + '<span>' + escapeHtml(lsx('cardHealthActiveGrants')) + ': ' + escapeHtml(String((cardHealth.activeGrants || []).length)) + '</span>'
          + '<span>' + escapeHtml(lsx('cardUnlocks')) + ': ' + escapeHtml(unlockLabel) + '</span>'
          + (issueLines ? ('<span>' + issueLines + '</span>') : '')
          + '</div>'
          + cardCodeLine
          + '</div>'
          + '</div>';
      }).join('');
      cardsRoot.innerHTML = '<div style="display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;align-items:start">' + cardRows + '</div>';
    }
  }
  var cardsPager = document.getElementById('llmServiceCardsPager');
  var cardsPagerMeta = document.getElementById('llmServiceCardsPagerMeta');
  var cardsPrevBtn = document.getElementById('llmServiceCardsPrevBtn');
  var cardsNextBtn = document.getElementById('llmServiceCardsNextBtn');
  if (cardsPager && cardsPagerMeta && cardsPrevBtn && cardsNextBtn) {
    var start = filteredCards.length ? (startIndex + 1) : 0;
    var end = startIndex + pageItems.length;
    var selectedCount = llmServiceSelectedCardIDs.length;
    var pageText = totalPages > 1
      ? lsx('pageSummary', { start: String(start), end: String(end), total: String(totalCards) })
      : lsx('pageSingle', { total: String(totalCards) });
    cardsPagerMeta.textContent = pageText + ' | ' + lsx('selectedCount', { count: String(selectedCount) });
    cardsPrevBtn.disabled = llmServiceCardPage <= 1;
    cardsNextBtn.disabled = llmServiceCardPage >= totalPages;
    cardsPager.classList.toggle('hidden', totalCards <= llmServiceCardPageSize);
  }
  var totalGrants = 0;
  var grantTotalPages = 1;
  var grantStartIndex = 0;
  var grantPageItems = [];
  const grantsRoot = document.getElementById('llmServiceGrantsList');
  if (grantsRoot) {
    const grantSearchEl = document.getElementById('llmServiceGrantSearch');
    if (grantSearchEl && grantSearchEl.value !== llmServiceGrantSearch) grantSearchEl.value = llmServiceGrantSearch || '';
    const grants = llmServiceFilterGrants(llmServiceAdminCache.grants || [], llmServiceGrantSearch || '', llmServiceAdminCache);
    totalGrants = grants.length;
    grantTotalPages = Math.max(1, Math.ceil(totalGrants / llmServiceGrantPageSize));
    if (llmServiceGrantPage > grantTotalPages) llmServiceGrantPage = grantTotalPages;
    grantStartIndex = (llmServiceGrantPage - 1) * llmServiceGrantPageSize;
    grantPageItems = grants.slice(grantStartIndex, grantStartIndex + llmServiceGrantPageSize);
    if (!grantPageItems.length) {
      grantsRoot.innerHTML = ui.hint(lsx('noActiveGrants'));
    } else {
      const grantRows = grantPageItems.map(function(g) {
        const total = Number(g.credits_total || 0);
        const used = Number(g.credits_used || 0);
        const remaining = total > 0 ? Math.max(0, total - used) : 0;
        const creditsText = total > 0 ? lsx('creditsRemaining', { remaining: llmServiceFormatCreditValue(remaining), total: llmServiceFormatCreditValue(total) }) : lsx('emptyValue');
        const usedText = total > 0 ? lsx('creditsUsed', { used: llmServiceFormatCreditValue(used) }) : '';
        const statusMeta = llmServiceGrantStatusMeta(g, Date.now());
        const sourceMeta = llmServiceGrantSourceMeta(g, llmServiceAdminCache);
        const startsAt = String(g.starts_at || '').trim() || lsx('emptyValue');
        const expiresAt = String(g.expires_at || '').trim() || lsx('emptyValue');
        const cardLine = sourceMeta.cardID
          ? (escapeHtml(lsx('grantSourceCard')) + ': <span class="mono">' + escapeHtml(sourceMeta.cardLabel) + '</span> <span class="item-meta mono">(' + escapeHtml(sourceMeta.cardID) + ')</span>')
          : (escapeHtml(lsx('grantSourceCard')) + ': ' + escapeHtml(sourceMeta.cardLabel));
        return '<div class="item" style="padding:0;overflow:hidden;border:1px solid var(--line);min-width:0">'
          + '<div style="padding:10px 12px;border-bottom:1px solid rgba(31,34,48,.06);display:flex;gap:8px;align-items:flex-start;background:#fff">'
          + '<div style="min-width:0;flex:1"><div class="mono" style="font-size:12px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(g.email || '') + '</div><div class="item-meta mono" style="margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(String(g.id || '')) + '</div></div>'
          + '<div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;justify-content:flex-end">' + ui.badge(statusMeta.label, statusMeta.badgeClass) + '<button class="btn-danger" style="height:28px;font-size:11px;padding:0 10px" onclick="llmServiceDeleteGrant(\'' + llmServiceJSArg(g.id || '') + '\')">' + escapeHtml(lsx('deleteGrant')) + '</button></div>'
          + '</div>'
          + '<div style="padding:10px 12px;background:#fff;display:grid;grid-template-columns:1fr 1fr;gap:8px 12px">'
          + '<div style="min-width:0"><div class="item-meta">' + escapeHtml(lsx('serviceGroups')) + '</div><div style="margin-top:4px">' + llmServiceGroupLinks([g.service_group_id]) + '</div></div>'
          + '<div style="min-width:0"><div class="item-meta">' + escapeHtml(lsx('grantGrantSource')) + '</div><div class="item-meta" style="margin-top:4px">' + cardLine + '</div><div style="margin-top:4px">' + escapeHtml(lsx('grantAccessType')) + ': ' + ui.badge(sourceMeta.accessLabel, sourceMeta.accessPolicy === 'grant_required' ? 'warn' : 'ok') + '</div></div>'
          + '<div style="min-width:0"><div class="item-meta">' + escapeHtml(lsx('credits')) + '</div><div class="mono" style="margin-top:4px;font-size:12px;font-weight:700">' + escapeHtml(creditsText) + '</div>' + (usedText ? ('<div class="item-meta mono" style="margin-top:3px">' + escapeHtml(usedText) + '</div>') : '') + '</div>'
          + '<div style="min-width:0"><div class="item-meta">' + escapeHtml(lsx('grantStartsAt')) + ' / ' + escapeHtml(lsx('grantExpiresAt')) + '</div><div class="item-meta mono" style="margin-top:4px">' + escapeHtml(startsAt) + '</div><div class="item-meta mono" style="margin-top:3px">' + escapeHtml(expiresAt) + '</div>' + (statusMeta.detail ? ('<div class="item-meta" style="margin-top:3px;color:#c05621">' + escapeHtml(statusMeta.detail) + '</div>') : '') + '</div>'
          + '<div style="min-width:0;grid-column:1 / -1"><div class="item-meta">' + escapeHtml(lsx('grantRouteModels')) + '</div><div style="margin-top:2px">' + llmServiceRenderGrantRoutes(g, llmServiceAdminCache, ui) + '</div></div>'
          + '</div>'
          + '</div>';
      }).join('');
      grantsRoot.innerHTML = '<div style="display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;align-items:start;margin-top:8px">' + grantRows + '</div>';
    }
  }
  var grantsPager = document.getElementById('llmServiceGrantsPager');
  var grantsPagerMeta = document.getElementById('llmServiceGrantsPagerMeta');
  var grantsPrevBtn = document.getElementById('llmServiceGrantsPrevBtn');
  var grantsNextBtn = document.getElementById('llmServiceGrantsNextBtn');
  if (grantsPager && grantsPagerMeta && grantsPrevBtn && grantsNextBtn) {
    var grantStart = totalGrants ? (grantStartIndex + 1) : 0;
    var grantEnd = grantStartIndex + grantPageItems.length;
    grantsPagerMeta.textContent = grantTotalPages > 1
      ? lsx('grantPageSummary', { start: String(grantStart), end: String(grantEnd), total: String(totalGrants) })
      : lsx('grantPageSingle', { total: String(totalGrants) });
    grantsPrevBtn.disabled = llmServiceGrantPage <= 1;
    grantsNextBtn.disabled = llmServiceGrantPage >= grantTotalPages;
    grantsPager.classList.toggle('hidden', totalGrants <= llmServiceGrantPageSize);
  }
  const diagnoseRoot = document.getElementById('llmServiceDiagnoseResult');
  if (diagnoseRoot) {
    const diag = llmServiceAdminCache.user_diagnostic || null;
    if (!diag || !diag.email) {
      diagnoseRoot.innerHTML = ui.hint(lsx('diagnoseEmpty'));
    } else {
      const status = diag.service_status || {};
      const securityGroups = (diag.resolved_security_group_ids || []).join(', ') || lsx('emptyValue');
      const effectiveGroups = (status.service_group_ids || []).join(', ') || lsx('emptyValue');
      const effectiveGroupLinks = llmServiceGroupLinks(status.service_group_ids || []);
      const models = (status.available_models || []).join(', ') || lsx('emptyValue');
      const inactiveReasons = (status.inactive_reasons || []).length ? (status.inactive_reasons || []).map(function(reason) { return '<div class="item-meta" style="margin-top:4px;color:#c05621">' + escapeHtml(reason) + '</div>'; }).join('') : ui.hint(lsx('emptyValue'));
      const userBindings = (diag.direct_user_bindings || []).length ? ('<div class="table" style="gap:6px;margin-top:6px">' + (diag.direct_user_bindings || []).map(function(b) { return '<div class="row" style="grid-template-columns:1fr"><div class="item-meta">' + llmServiceGroupLinks(b.service_group_ids || []) + '</div></div>'; }).join('') + '</div>') : ui.hint(lsx('emptyValue'));
      const groupBindings = (diag.matched_group_bindings || []).length ? ('<div class="table" style="gap:6px;margin-top:6px">' + (diag.matched_group_bindings || []).map(function(b) { var securityGroup = llmServiceDescribeSecurityGroup(b.group_id); return '<div class="row" style="grid-template-columns:.9fr 1.1fr"><div class="item-meta">' + escapeHtml(securityGroup.label) + (securityGroup.missing ? (' <span style="color:#c05621">(' + escapeHtml(lsx('missingSecurityGroup')) + ')</span>') : '') + '</div><div class="item-meta">' + llmServiceGroupLinks(b.service_group_ids || []) + '</div></div>'; }).join('') + '</div>') : ui.hint(lsx('emptyValue'));
      const diagnosticGrants = ((status.credit_grants || []).length ? status.credit_grants : (diag.active_grants || []));
      const grants = diagnosticGrants.length ? ('<div class="table" style="gap:6px;margin-top:6px">' + '<div class="row header" style="grid-template-columns:.9fr .9fr .8fr 1.2fr"><div>' + escapeHtml(lsx('serviceGroups')) + '</div><div>' + escapeHtml(lsx('grantGrantSource')) + '</div><div>' + escapeHtml(lsx('grantAccessType')) + '</div><div>' + escapeHtml(lsx('grantRouteModels')) + '</div></div>' + diagnosticGrants.map(function(g) { var sourceMeta = llmServiceGrantSourceMeta(g, llmServiceAdminCache); var statusMeta = llmServiceGrantStatusMeta(g, Date.now()); var cardText = sourceMeta.cardID ? ((sourceMeta.cardLabel || '-') + ' (' + sourceMeta.cardID + ')') : (sourceMeta.cardLabel || '-'); return '<div class="row" style="grid-template-columns:.9fr .9fr .8fr 1.2fr"><div class="item-meta">' + llmServiceGroupLinks([g.service_group_id]) + '</div><div><div class="item-meta mono">' + escapeHtml(cardText + ' | ' + (g.source || '') + ' | ' + String(g.expires_at || '')) + '</div><div style="margin-top:4px">' + ui.badge(statusMeta.label, statusMeta.badgeClass) + (statusMeta.detail ? ('<span class="item-meta" style="margin-left:6px">' + escapeHtml(statusMeta.detail) + '</span>') : '') + '</div></div><div>' + ui.badge(sourceMeta.accessLabel, sourceMeta.accessPolicy === 'grant_required' ? 'warn' : 'ok') + '</div><div>' + llmServiceRenderGrantRoutes(g, llmServiceAdminCache, ui) + '</div></div>'; }).join('') + '</div>') : ui.hint(lsx('emptyValue'));
      const billingRoutes = (diag.billing_routes || []).length ? ('<div class="table" style="gap:6px;margin-top:6px">' + '<div class="row header" style="grid-template-columns:.7fr .8fr .9fr .8fr 1.2fr"><div>' + escapeHtml(lsx('billingRouteModel')) + '</div><div>' + escapeHtml(lsx('billingRouteProvider')) + '</div><div>' + escapeHtml(lsx('serviceGroups')) + '</div><div>' + escapeHtml(lsx('billingRouteAccess')) + '</div><div>' + escapeHtml(lsx('billingRouteStatus')) + '</div></div>' + (diag.billing_routes || []).map(function(route) { return '<div class="row" style="grid-template-columns:.7fr .8fr .9fr .8fr 1.2fr"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(route.model_name || '-') + '</div><div class="item-meta mono">' + escapeHtml(route.provider_id || '-') + '</div><div class="item-meta">' + llmServiceGroupLinks(route.service_group_ids || []) + '</div><div>' + ui.badge(llmServiceBillingRouteAccessLabel(route), llsNormalizeAccessPolicy(route.access_policy || '') === 'grant_required' ? 'warn' : 'ok') + '</div><div>' + llmServiceBillingRouteStatus(route, ui) + '</div></div>'; }).join('') + '</div>') : ui.hint(lsx('emptyValue'));
      const authorizedModels = (status.authorized_models || []).length ? ('<div class="table" style="gap:6px;margin-top:6px">' + '<div class="row header" style="grid-template-columns:.9fr 1.3fr .8fr"><div>' + escapeHtml(lsx('modelsLabel')) + '</div><div>' + escapeHtml(lsx('providerRoute')) + '</div><div>' + escapeHtml(lsx('multiplierLabel')) + '</div></div>' + (status.authorized_models || []).map(function(model) {
        const providerGroups = model.provider_service_groups || {};
        const providerMultipliers = model.provider_credit_multipliers || {};
        const routeText = (model.provider_ids || []).map(function(providerID) {
          const groupList = providerGroups[String(providerID || '').toLowerCase()] || model.service_group_ids || [];
          return String(providerID || '-') + ' -> ' + (groupList || []).join(', ');
        }).join(' | ') || '-';
        const multiplierText = (model.provider_ids || []).map(function(providerID) {
          const multiplier = providerMultipliers[String(providerID || '').toLowerCase()];
          return String(multiplier || model.credit_multiplier || 1);
        }).join(' / ') || String(model.credit_multiplier || 1);
        return '<div class="row" style="grid-template-columns:.9fr 1.3fr .8fr"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(model.name || '-') + '</div><div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(routeText) + '</div><div class="item-meta mono">' + escapeHtml(multiplierText) + '</div></div>';
      }).join('') + '</div>') : ui.hint(lsx('noAuthorizedModelDetails'));
      var summaryCards = '<div class="row" style="grid-template-columns:repeat(6,minmax(0,1fr));gap:8px;border:none;padding:0;background:transparent">'
        + '<div class="metric"><label>' + escapeHtml(lsx('securityGroups')) + '</label><strong>' + escapeHtml(String((diag.resolved_security_group_ids || []).length || 0)) + '</strong><span class="mono">' + escapeHtml(securityGroups) + '</span></div>'
        + '<div class="metric"><label>' + escapeHtml(lsx('effectiveServiceGroups')) + '</label><strong>' + escapeHtml(String((status.service_group_ids || []).length || 0)) + '</strong><span class="mono">' + escapeHtml(effectiveGroups) + '</span></div>'
        + '<div class="metric"><label>' + escapeHtml(lsx('bindingHits')) + '</label><strong>' + escapeHtml(String((diag.direct_user_bindings || []).length + (diag.matched_group_bindings || []).length)) + '</strong><span class="mono">' + escapeHtml(String((diag.direct_user_bindings || []).length)) + ' + ' + escapeHtml(String((diag.matched_group_bindings || []).length)) + '</span></div>'
        + '<div class="metric"><label>' + escapeHtml(lsx('grantHits')) + '</label><strong>' + escapeHtml(String(diagnosticGrants.length || 0)) + '</strong><span class="mono">' + escapeHtml(String(diagnosticGrants.length || 0)) + '</span></div>'
        + '<div class="metric"><label>' + escapeHtml(lsx('availableModels')) + '</label><strong>' + escapeHtml(String((status.available_models || []).length || 0)) + '</strong><span class="mono">' + escapeHtml(models) + '</span></div>'
        + '<div class="metric"><label>' + escapeHtml(lsx('credits')) + '</label><strong>' + escapeHtml(String(status.credits_available || 0)) + '</strong><span class="mono">' + escapeHtml(status.default_model || 'auto') + '</span></div>'
        + '</div>';
      var section = function(title, body) { return '<div class="item" style="margin-top:8px;padding:12px 14px"><div class="item-title" style="margin-bottom:6px">' + escapeHtml(title) + '</div>' + body + '</div>'; };
      diagnoseRoot.innerHTML = '<div class="item"><div class="item-head"><div><div class="item-title">' + escapeHtml(diag.email || '') + '</div><div class="item-meta">' + (status.active ? lsx('active') : lsx('inactive')) + ' | ' + lsx('defaultModel') + ': <span class="mono">' + escapeHtml(status.default_model || 'auto') + '</span></div></div></div>' + summaryCards + '<div style="margin-top:8px">' + section(lsx('effectiveServiceGroups'), '<div class="item-meta">' + effectiveGroupLinks + '</div>') + section(lsx('inactiveReasons'), inactiveReasons) + section(lsx('modelRouting'), authorizedModels) + section(lsx('billingRoutes'), billingRoutes) + section(lsx('directUserBindings'), userBindings) + section(lsx('matchedGroupBindings'), groupBindings) + section(lsx('grantDetails'), grants) + '</div></div>';

    }
  }
}
function selectLLMServiceGroup(id) { llmServiceSelectedGroupID = id; openLLMServiceGroupDialog('edit', id); }
function readLLMServiceGroupDraft() {
  const idEl = document.getElementById('llmServiceGroupID');
  const nameEl = document.getElementById('llmServiceGroupName');
  const descEl = document.getElementById('llmServiceGroupDesc');
  const modelsEl = document.getElementById('llmServiceGroupModels');
  return {
    id: ((idEl && idEl.value) || '').trim(),
    name: ((nameEl && nameEl.value) || '').trim(),
    description: ((descEl && descEl.value) || '').trim(),
    models: parseModelDefs((modelsEl && modelsEl.value) || '')
  };
}
function upsertLLMServiceGroup() {
  if (!llmServiceAdminCache) llmServiceAdminCache = { model_service_groups: [], global_service_group_ids: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
  const next = readLLMServiceGroupDraft();
  if (!next.id || !next.name) { showToast(lsx('groupIdNameRequired'), 'error'); return false; }
  if (isBuiltinLLMServiceGroup(next.id) || isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast(lsx('builtInDefaultReadOnly'), 'info'); return false; }
  const groups = llmServiceAdminCache.model_service_groups || [];
  const selectedIdx = groups.findIndex(function(g) { return llmServiceGroupIDKey(g && g.id) === llmServiceGroupIDKey(llmServiceSelectedGroupID); });
  const oldID = selectedIdx >= 0 ? groups[selectedIdx].id : '';
  const duplicateIdx = groups.findIndex(function(g, idx) { return llmServiceGroupIDKey(g && g.id) === llmServiceGroupIDKey(next.id) && idx !== selectedIdx; });
  if (duplicateIdx >= 0) { showToast(lsx('duplicateGroupId', { id: next.id }), 'error'); return false; }
  if (selectedIdx >= 0 && llmServiceGroupIDKey(oldID) !== llmServiceGroupIDKey(next.id)) { showToast(lsx('groupIdImmutable'), 'error'); return false; }
  if (selectedIdx >= 0) groups[selectedIdx] = next; else groups.push(next);
  llmServiceAdminCache.model_service_groups = groups;
  llmServiceSelectedGroupID = next.id;
  llmServiceDraftDirty = false;
  renderLLMServiceAdmin();
  return true;
}
function llmServiceGroupIDKey(id) { return String(id || '').trim().toLowerCase(); }
function pruneLLMServiceGroupReferences(id) {
  var key = llmServiceGroupIDKey(id);
  if (!llmServiceAdminCache || !key) return;
  var keep = function(x) { return llmServiceGroupIDKey(x) !== key; };
  llmServiceAdminCache.model_service_groups = (llmServiceAdminCache.model_service_groups || []).filter(function(g) { return keep(g && g.id); });
  llmServiceAdminCache.global_service_group_ids = (llmServiceAdminCache.global_service_group_ids || []).filter(keep);
  llmServiceAdminCache.default_new_user_service_groups = (llmServiceAdminCache.default_new_user_service_groups || []).filter(keep);
  if (!(llmServiceAdminCache.default_new_user_service_groups || []).length) llmServiceAdminCache.default_new_user_service_groups = [BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID];
  llmServiceAdminCache.group_bindings = (llmServiceAdminCache.group_bindings || []).map(function(b) { b.service_group_ids = (b.service_group_ids || []).filter(keep); return b; }).filter(function(b) { return (b.service_group_ids || []).length; });
  llmServiceAdminCache.user_bindings = (llmServiceAdminCache.user_bindings || []).map(function(b) { b.service_group_ids = (b.service_group_ids || []).filter(keep); return b; }).filter(function(b) { return (b.service_group_ids || []).length; });
}
function removeSelectedLLMServiceGroup() {
  if (!llmServiceAdminCache || !llmServiceSelectedGroupID) return;
  if (isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast(lsx('builtInDefaultCannotRemove'), 'info'); return; }
  pruneLLMServiceGroupReferences(llmServiceSelectedGroupID);
  llmServiceSelectedGroupID = llmServiceAdminCache.model_service_groups[0] && llmServiceAdminCache.model_service_groups[0].id || '';
  llmServiceDraftDirty = false;
  llmServiceRenderedGroupID = llmServiceSelectedGroupID || '';
  renderLLMServiceAdmin();
}
function addLLMServiceGroupBinding() {
  if (!llmServiceAdminCache) return;
  const groupEl = document.getElementById('llmServiceBindingGroupID');
  const serviceEl = document.getElementById('llmServiceBindingServiceGroups');
  const groupID = ((groupEl && groupEl.value) || '').trim();
  const serviceGroupIDs = parseCSV((serviceEl && serviceEl.value) || '');
  if (!groupID || !serviceGroupIDs.length) return;
  if (llmServiceWarnUnknownSecurityGroups([groupID])) return;
  if (llmServiceWarnUnknownServiceGroups(serviceGroupIDs)) return;
  llmServiceAdminCache.group_bindings = llmServiceAdminCache.group_bindings || [];
  const existingIdx = llmServiceAdminCache.group_bindings.findIndex(function(item) { return String(item.group_id || '').trim().toLowerCase() === String(groupID || '').trim().toLowerCase(); });
  const nextBinding = { group_id: groupID, service_group_ids: serviceGroupIDs };
  if (existingIdx >= 0) llmServiceAdminCache.group_bindings[existingIdx] = nextBinding; else llmServiceAdminCache.group_bindings.push(nextBinding);
  if (groupEl) groupEl.value = '';
  if (serviceEl) serviceEl.value = '';
  renderLLMServiceAdmin();
}
function removeLLMServiceGroupBinding(idx) { if (!llmServiceAdminCache) return; llmServiceAdminCache.group_bindings.splice(idx, 1); renderLLMServiceAdmin(); }
async function addLLMServiceUserBinding() {
  if (!llmServiceAdminCache) return;
  const emailEl = document.getElementById('llmServiceUserEmail');
  const serviceEl = document.getElementById('llmServiceUserServiceGroups');
  const email = ((emailEl && emailEl.value) || '').trim();
  const serviceGroupIDs = parseCSV((serviceEl && serviceEl.value) || '');
  if (!email || !serviceGroupIDs.length) return;
  if (llmServiceWarnUnknownServiceGroups(serviceGroupIDs)) return;
  llmServiceAdminCache.user_bindings = llmServiceAdminCache.user_bindings || [];
  const normalizedEmail = email.toLowerCase();
  const existingIdx = llmServiceAdminCache.user_bindings.findIndex(function(item) { return String(item.email || '').trim().toLowerCase() === normalizedEmail; });
  const nextBinding = { email: email, service_group_ids: serviceGroupIDs };
  if (existingIdx >= 0) llmServiceAdminCache.user_bindings[existingIdx] = nextBinding; else llmServiceAdminCache.user_bindings.push(nextBinding);
  if (emailEl) emailEl.value = '';
  if (serviceEl) serviceEl.value = '';
  renderLLMServiceAdmin();
  await saveLLMServiceAdmin();
}
async function removeLLMServiceUserBinding(idx) { if (!llmServiceAdminCache) return; llmServiceAdminCache.user_bindings.splice(idx, 1); renderLLMServiceAdmin(); await saveLLMServiceAdmin(); }
function llmServiceAdminSavePayload() {
  var payload = Object.assign({}, llmServiceAdminCache || {});
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
function llmServiceLegacyEditorVisible() {
  var idEl = document.getElementById('llmServiceGroupID');
  return !!(idEl && idEl.offsetParent);
}
async function saveLLMServiceAdmin() {
  if (!llmServiceAdminCache) return;
  if (llmServiceLegacyEditorVisible()) {
    const draft = readLLMServiceGroupDraft();
    if (draft.id || draft.name) {
      if (!upsertLLMServiceGroup()) return;
      if (!llmServiceAdminCache) return;
    }
  }
  if (llmServiceWarnUnknownSecurityGroups(llmServiceCollectReferencedSecurityGroups(llmServiceAdminCache))) return;
  if (llmServiceWarnUnknownServiceGroups(llmServiceCollectReferencedServiceGroups(llmServiceAdminCache))) return;
  try {
    const data = await api('/api/admin/llm/services?include_cards=false', { method: 'PUT', body: JSON.stringify(llmServiceAdminSavePayload()) });
    llmServiceAdminCache = data || llmServiceAdminCache;
    llmServiceDraftDirty = false;
    renderLLMServiceAdmin();
    setOutput(lsx('saveDone'));
    showToast(lsx('saveDone'), 'success');
  } catch (err) {
    const msg = lsx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function diagnoseLLMServiceUser() {
  const emailEl = document.getElementById('llmServiceDiagnoseEmail');
  const email = (emailEl && emailEl.value || '').trim();
  if (!email) {
    showToast(lsx('diagnoseEmpty'), 'info');
    return;
  }
  try {
    const data = await api('/api/admin/llm/services/diagnose?email=' + encodeURIComponent(email));
    if (!llmServiceAdminCache) llmServiceAdminCache = { model_service_groups: [], global_service_group_ids: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
    llmServiceAdminCache.user_diagnostic = data || null;
    renderLLMServiceAdmin();
  } catch (err) {
    const msg = lsx('diagnoseLoadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function issueLLMServiceCard() {
  try {
    const labelEl = document.getElementById('llmServiceCardLabel');
    const groupsEl = document.getElementById('llmServiceCardGroups');
    const daysEl = document.getElementById('llmServiceCardDays');
    const creditsEl = document.getElementById('llmServiceCardCredits');
    const fiveHourCreditsEl = document.getElementById('llmServiceCardFiveHourCredits');
    const dailyCreditsEl = document.getElementById('llmServiceCardDailyCredits');
    const weeklyCreditsEl = document.getElementById('llmServiceCardWeeklyCredits');
    const monthlyCreditsEl = document.getElementById('llmServiceCardMonthlyCredits');
    const countEl = document.getElementById('llmServiceCardCount');
    const issuedCodesEl = document.getElementById('llmServiceIssuedCodes');
    if (!labelEl || !groupsEl || !daysEl || !creditsEl || !fiveHourCreditsEl || !dailyCreditsEl || !weeklyCreditsEl || !monthlyCreditsEl || !countEl) return;
    const serviceGroupIDs = parseCSV(groupsEl.value || '');
    if (!serviceGroupIDs.length) {
      const msg = lsx('serviceGroupRequired');
      setOutput(msg);
      showToast(msg, 'info');
      if (typeof groupsEl.focus === 'function') groupsEl.focus();
      return;
    }
    if (llmServiceWarnUnknownServiceGroups(serviceGroupIDs)) return;
    const count = Math.max(1, Math.min(1000, Number(countEl.value || 1) || 1));
    const durationDays = Math.max(1, Math.min(365, Number(daysEl.value || 30) || 30));
    const credits = Math.max(0, Number(creditsEl.value || 0) || 0);
    const data = await api('/api/admin/llm/service-cards', { method: 'POST', body: JSON.stringify({
      label: (labelEl.value || '').trim(),
      service_group_ids: serviceGroupIDs,
      duration_days: durationDays,
      credits: credits,
      five_hour_credits: Math.max(0, Number(fiveHourCreditsEl.value || 0) || 0),
      daily_credits: Math.max(0, Number(dailyCreditsEl.value || 0) || 0),
      weekly_credits: Math.max(0, Number(weeklyCreditsEl.value || 0) || 0),
      monthly_credits: Math.max(0, Number(monthlyCreditsEl.value || 0) || 0),
      count: count
    }) });
    const codes = (data.cards || []).map(function(card) { return String(card && card.code || '').trim(); }).filter(Boolean);
    llmServiceLastIssuedCodes = codes;
    if (issuedCodesEl) renderLLMServiceIssuedCodes();
    const msg = lsx('issueDone', { count: String(codes.length || count) });
    setOutput(msg);
    showToast(msg, 'success');
    await loadLLMServiceAdmin();
  } catch (err) {
    const msg = lsx('issueFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
ensureLLMServiceAdminUI();

function ensureLLMServiceSystemUI() {
  if (document.getElementById('llmServiceSystemRoot')) return;
  const tab = document.getElementById('tab-system');
  if (!tab) return;
  const host = document.createElement('div');
  host.id = 'llmServiceSystemRoot';
  host.className = 'item';
  host.style.marginTop = '18px';
  host.innerHTML = '' +
    '<div class="head" style="margin-bottom:8px"><div><div class="item-title" id="llmServiceSystemTitle"></div><div class="item-meta" id="llmServiceSystemDesc"></div></div><div class="actions"><button class="btn-primary" onclick="saveLLMServiceSystemSettings()" id="llmServiceSystemSaveBtn"></button></div></div>' +
    '<div class="grid2">' +
    '<div><label id="llmServiceSystemGroupsLabel"></label><select id="llmServiceSystemGroups" multiple size="4" style="width:100%;padding:10px 12px;border-radius:12px;border:1px solid var(--line);background:var(--panel, #fff);font:inherit;min-height:92px"></select></div>' +
    '<div><label id="llmServiceSystemDaysLabel"></label><input id="llmServiceSystemDays" type="number" min="1" value="30"></div>' +
    '</div><div class="grid2" style="margin-top:10px"><div><label id="llmServiceSystemCreditsLabel"></label><input id="llmServiceSystemCredits" type="number" min="1" step="1" value="1000"></div></div><div class="hint" id="llmServiceSystemHint" style="margin-top:10px"></div>';
  tab.appendChild(host);
  applyLLMServiceSystemI18n();
}
function applyLLMServiceSystemI18n() {
  _s('llmServiceSystemTitle', 'textContent', lsx('systemDefaults'));
  _s('llmServiceSystemDesc', 'textContent', lsx('systemDesc'));
  _s('llmServiceSystemGroupsLabel', 'textContent', lsx('newUserGroups'));
  _s('llmServiceSystemDaysLabel', 'textContent', lsx('newUserDays'));
  _s('llmServiceSystemCreditsLabel', 'textContent', lsx('newUserCredits'));
  _s('llmServiceSystemSaveBtn', 'textContent', lsx('saveDefaults'));
  _s('llmServiceSystemHint', 'textContent', lsx('systemHint'));
}
function renderLLMServiceSystemSettings() {
  applyLLMServiceSystemI18n();
  if (!llmServiceAdminCache) {
    ensureLLMServiceSystemSettingsLoaded();
    return;
  }
  renderLLMServiceSystemGroupOptions();
  var systemSelect = document.getElementById('llmServiceSystemGroups');
  var selectedSystemGroups = {};
  (llmServiceAdminCache.default_new_user_service_groups || [BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID]).forEach(function(id) { selectedSystemGroups[String(id || '').trim()] = true; });
  Array.prototype.slice.call(systemSelect && systemSelect.options || []).forEach(function(option) { option.selected = !!selectedSystemGroups[option.value]; });
  _s('llmServiceSystemDays', 'value', String(llmServiceAdminCache.default_new_user_duration_days || 30));
  _s('llmServiceSystemCredits', 'value', String(llmServiceAdminCache.default_new_user_credits || 1000));
  refreshLLMServiceGroupSelectors();
}
function ensureLLMServiceSystemSettingsLoaded() {
  ensureLLMServiceSystemUI();
  if (llmServiceAdminCache || llmServiceSystemSettingsLoading) return;
  if (typeof token === 'function' && !token()) return;
  llmServiceSystemSettingsLoading = true;
  loadLLMServiceAdmin().finally(function() {
    llmServiceSystemSettingsLoading = false;
  });
}
async function saveLLMServiceSystemSettings() {
  ensureLLMServiceSystemUI();
  if (!llmServiceAdminCache) await loadLLMServiceAdmin();
  var selectedSystemGroups = Array.prototype.slice.call(document.getElementById('llmServiceSystemGroups').options || []).filter(function(option) { return option.selected && option.value; }).map(function(option) { return option.value; });
  llmServiceAdminCache.default_new_user_service_groups = selectedSystemGroups.length ? selectedSystemGroups : [BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID];
  llmServiceAdminCache.default_new_user_duration_days = Number(document.getElementById('llmServiceSystemDays').value || 30);
  if (!(llmServiceAdminCache.default_new_user_duration_days > 0)) llmServiceAdminCache.default_new_user_duration_days = 30;
  llmServiceAdminCache.default_new_user_credits = Number(document.getElementById('llmServiceSystemCredits').value || 1000);
  if (!(llmServiceAdminCache.default_new_user_credits > 0)) llmServiceAdminCache.default_new_user_credits = 1000;
  if (!(llmServiceAdminCache.tokens_per_credit > 0)) llmServiceAdminCache.tokens_per_credit = 10000;
  if (llmServiceWarnUnknownServiceGroups(llmServiceAdminCache.default_new_user_service_groups || [])) return;
  try {
    const data = await api('/api/admin/llm/services?include_cards=false', { method: 'PUT', body: JSON.stringify(llmServiceAdminSavePayload()) });
    llmServiceAdminCache = data || llmServiceAdminCache;
    llmServiceDraftDirty = false;
    renderLLMServiceAdmin();
    renderLLMServiceSystemSettings();
    setOutput(lsx('saveDone'));
    showToast(lsx('saveDone'), 'success');
  } catch (err) {
    const msg = lsx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
ensureLLMServiceSystemUI();







function ensureLLMServiceCardsTab() {
  const tab = document.getElementById('tab-servicecards');
  if (!tab || document.getElementById('llmServiceCardsStandalone')) return;
  const host = document.createElement('div');
  host.id = 'llmServiceCardsStandalone';
  host.className = '';
  host.style.marginTop = '16px';
  host.style.display = 'grid';
  host.style.gridTemplateColumns = 'minmax(0, 1fr)';
  host.style.gap = '16px';
  host.innerHTML = llmServiceCardsPanelMarkup();
  tab.appendChild(host);
  applyLLMServiceI18n();
  llmServiceApplyCardDurationDefaults();
  bindLLMServiceCardGridResize();
}
function applyLLMServiceTabI18n() {
  if (typeof tabMeta === 'object') {
    tabMeta.modelservices = ['modelServicesTabTitle', 'modelServicesTabSubtitle'];
    tabMeta.servicecards = ['serviceCardsTabTitle', 'serviceCardsTabSubtitle'];
  }
  _s('navModelServices', 'textContent', lsx('navLabel'));
  _s('navModelServicesDesc', 'textContent', lsx('navDesc'));
  _s('navServiceCards', 'textContent', lsx('serviceCardsNavLabel'));
  _s('navServiceCardsDesc', 'textContent', lsx('serviceCardsNavDesc'));
  _s('modelServicesTabTitle', 'textContent', lsx('tabTitle'));
  _s('modelServicesTabSubtitle', 'textContent', lsx('tabSubtitle'));
  _s('modelServicesReloadBtn', 'textContent', lsx('reload'));
  _s('serviceCardsTabTitle', 'textContent', lsx('serviceCardsTabTitle'));
  _s('serviceCardsTabSubtitle', 'textContent', lsx('serviceCardsTabSubtitle'));
  _s('serviceCardsReloadBtn', 'textContent', lsx('reload'));
  _s('llmServiceCardsListTitle', 'textContent', lsx('cards'));
}
function registerLLMServiceTabs() {
  if (!window.AdminTabRegistry || typeof window.AdminTabRegistry.registerTab !== 'function') return;
  window.AdminTabRegistry.registerTab({
    id: 'modelservices',
    title: function() { return lsx('tabTitle'); },
    subtitle: function() { return lsx('tabSubtitle'); },
    onOpen: function() { ensureLLMServiceAdminUI(); applyLLMServiceTabI18n(); loadLLMServiceAdmin(); }
  });
  window.AdminTabRegistry.registerTab({
    id: 'servicecards',
    title: function() { return lsx('serviceCardsTabTitle'); },
    subtitle: function() { return lsx('serviceCardsTabSubtitle'); },
    onOpen: function() { ensureLLMServiceCardsTab(); applyLLMServiceTabI18n(); loadLLMServiceAdmin(); }
  });
  window.AdminTabRegistry.registerTab({
    id: 'system',
    onOpen: function() { ensureLLMServiceSystemUI(); applyLLMServiceSystemI18n(); ensureLLMServiceSystemSettingsLoaded(); }
  });
}
if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(function() {
    ensureLLMServiceAdminUI();
    applyLLMServiceTabI18n();
    applyLLMServiceI18n();
    applyLLMServiceSystemI18n();
    ensureLLMServiceCardsTab();
    ensureLLMServiceNewGroupButton();
    if (llmServiceAdminCache) {
      renderLLMServiceAdmin();
      renderLLMServiceSystemSettings();
    }
    if (llmServiceGroupDraft) renderLLMServiceGroupDialog();
  });
}
window.loadLlmServiceGroups = loadLLMServiceAdmin;
window.loadLLMServiceModelRuntime = loadLLMServiceModelRuntime;
window.triggerLLMServiceModelDownload = triggerLLMServiceModelDownload;
window.openLlmServiceGroupTab = function() { if (typeof openTab === 'function') openTab('modelservices'); };
registerLLMServiceTabs();
ensureLLMServiceAdminUI();
ensureLLMServiceCardsTab();
applyLLMServiceTabI18n();
function ensureLLMServiceNewGroupButton() {
  const addBtn = document.getElementById('llmServiceAddGroupBtn');
  if (!addBtn) return;
  const existing = document.getElementById('llmServiceNewGroupBtnInline');
  if (existing) {
    existing.textContent = lsx('addNewGroup');
    return;
  }
  const btn = document.createElement('button');
  btn.id = 'llmServiceNewGroupBtnInline';
  btn.className = 'btn-ghost';
  btn.textContent = lsx('addNewGroup');
  btn.onclick = function() { openLLMServiceGroupDialog('create'); };
  addBtn.parentNode.insertBefore(btn, addBtn);
}
ensureLLMServiceNewGroupButton();
(function() {
  const active = typeof localStorage !== 'undefined' ? localStorage.getItem(activeTabKey) : '';
  if (typeof token === 'function' && token() && (active === 'modelservices' || active === 'servicecards')) {
    openTab(active);
  }
})();









var LLS_DIALOG_I18N = {
  noProvidersYet: { zh: '\u8fd8\u6ca1\u6709\u53ef\u9009\u670d\u52a1\u5546', en: 'No providers yet' },
  chooseProvider: { zh: '\u9009\u62e9\u5df2\u6709\u670d\u52a1\u5546', en: 'Choose provider' },
  close: { zh: '\u5173\u95ed', en: 'Close' },
  up: { zh: '\u4e0a\u79fb', en: 'Up' },
  down: { zh: '\u4e0b\u79fb', en: 'Down' },
  routeTitle: { zh: '\u8def\u7531 #{index}', en: 'Route #{index}' },
  routeDesc: { zh: '\u4e3a\u540c\u4e00\u4e2a\u865a\u62df\u6a21\u578b\u540d\u914d\u7f6e\u591a\u4e2a LLM \u670d\u52a1\u5546\uff0cHub \u4f1a\u6309\u80fd\u529b\u6807\u7b7e\u81ea\u52a8\u9009\u62e9', en: 'Attach multiple providers to one virtual model name and let Hub route by capability tags automatically' },
  removeRoute: { zh: '\u5220\u9664\u8def\u7531', en: 'Remove' },
  exposedModel: { zh: '\u865a\u62df\u6a21\u578b\u540d', en: 'Virtual Model Name' },
  llmProviders: { zh: 'LLM \u670d\u52a1\u5546', en: 'LLM Providers' },
  add: { zh: '\u6dfb\u52a0\u670d\u52a1\u5546', en: 'Add' },
  addProviderConfig: { zh: '\u6dfb\u52a0\u670d\u52a1\u5546\u914d\u7f6e', en: 'Add Provider Config' },
  editProviderConfig: { zh: '\u7f16\u8f91\u670d\u52a1\u5546\u914d\u7f6e', en: 'Edit Provider Config' },
  providerName: { zh: '\u670d\u52a1\u5546', en: 'Provider' },
  providerList: { zh: '\u5df2\u6dfb\u52a0\u670d\u52a1\u5546\u5217\u8868', en: 'Configured Providers' },
  providerListHint: { zh: '\u5148\u5f39\u7a97\u914d\u7f6e\u53c2\u6570\uff0c\u4fdd\u5b58\u540e\u518d\u52a0\u5165\u5230\u8fd9\u6761\u8def\u7531\u3002', en: 'Configure provider settings in a popup, then save it into this route.' },
  noProvidersInRoute: { zh: '\u8fd8\u6ca1\u6709\u6dfb\u52a0\u670d\u52a1\u5546', en: 'No providers added yet' },
  saveProvider: { zh: '\u4fdd\u5b58\u670d\u52a1\u5546', en: 'Save Provider' },
  providerAlreadyAdded: { zh: '\u8fd9\u4e2a\u670d\u52a1\u5546\u5df2\u7ecf\u5728\u8be5\u8def\u7531\u4e2d', en: 'This provider is already added to the route' },
  featureSummary: { zh: '\u80fd\u529b', en: 'Features' },
  extraFeatureSummary: { zh: '\u5176\u4ed6\u6807\u7b7e', en: 'Extra Tags' },
  failoverHint: { zh: '\u5148\u6309 document/reasoning/tools \u7b49\u80fd\u529b\u6807\u7b7e\u5339\u914d\uff0c\u518d\u6309\u4e0a\u4e0b\u987a\u5e8f failover', en: 'Capability tags match first, then up/down order decides failover priority' },
  chooseAtLeastOneProvider: { zh: '\u8bf7\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a\u670d\u52a1\u5546', en: 'Choose at least one provider' },
  featureTags: { zh: '\u529f\u80fd\u6807\u7b7e', en: 'Features' },
  extraFeatures: { zh: '\u5176\u4ed6\u6807\u7b7e', en: 'Extra Features' },
  priority: { zh: '\u4f18\u5148\u7ea7', en: 'Priority' },
  resolutionTier: { zh: '\u5019\u9009\u987a\u5e8f', en: 'Fallback Order' },
  resolutionTierHint: { zh: '\u7528\u6765\u5b9a\u4e49\u5019\u9009\u987a\u5e8f\uff1a1 \u6700\u5148\u5c1d\u8bd5\uff0c2 \u6b21\u4e4b\uff0c3 \u518d\u540e\u30020 \u8868\u793a\u4e0d\u989d\u5916\u6307\u5b9a\uff0c\u4e3b\u8981\u770b\u80fd\u529b\u6807\u7b7e\u548c\u4e0a\u4e0b\u6392\u5e8f\u3002', en: 'Controls fallback order: tier 1 is tried first, then 2, then 3. Use 0 to leave it unspecified and rely on capability tags plus list order.' },
  accessPolicy: { zh: '\u8bbf\u95ee\u65b9\u5f0f', en: 'Access Policy' },
  accessPolicyHint: { zh: '\u51b3\u5b9a\u7528\u6237\u7ed1\u4e0a\u8fd9\u4e2a\u670d\u52a1\u7ec4\u540e\uff0c\u662f\u5426\u8fd8\u9700\u8981\u5151\u6362\u5361\u6388\u6743\u624d\u80fd\u771f\u6b63\u8c03\u7528\u6a21\u578b\u3002', en: 'Decides whether users only need this service-group binding, or also need a redeemed grant before calls can reach a live model.' },
  accessPolicyFree: { zh: '\u514d\u8d39\u901a\u884c\uff08\u53ea\u770b\u7ed1\u5b9a\uff09', en: 'Free via binding' },
  accessPolicyGrantRequired: { zh: '\u9700\u5151\u6362\u5361\uff08\u9700 grant \u548c\u989d\u5ea6\uff09', en: 'Grant required' },
  accessPolicyFreeHint: { zh: '\u53ea\u8981\u7528\u6237\u88ab\u7ed1\u5230\u8fd9\u4e2a\u670d\u52a1\u7ec4\uff0c\u5c31\u80fd\u76f4\u63a5\u4f7f\u7528\u3002\u4e0d\u5f3a\u4f9d\u8d56\u5151\u6362\u5361\u6216 grant\u3002', en: 'Anyone bound to this service group can use it directly. A redeemed card or active grant is not required.' },
  accessPolicyGrantRequiredHint: { zh: '\u7528\u6237\u5373\u4f7f\u5df2\u7ed1\u5230\u8fd9\u4e2a\u670d\u52a1\u7ec4\uff0c\u4ecd\u5fc5\u987b\u6301\u6709\u5bf9\u5e94\u7684\u6709\u6548 grant \u4e14\u6709\u5269\u4f59\u989d\u5ea6\uff0c\u624d\u4f1a\u88ab\u8def\u7531\u5230 provider\u3002', en: 'Even bound users must still have an active grant with remaining credits before traffic can be routed to a live provider.' },
  creditMultiplier: { zh: 'Credit \u500d\u7387', en: 'Credit Multiplier' },
  createdProvidersPrefix: { zh: '\u5df2\u521b\u5efa\u670d\u52a1\u5546: ', en: 'Created providers: ' },
  editServiceGroup: { zh: '\u7f16\u8f91\u670d\u52a1\u7ec4', en: 'Edit Service Group' },
  newServiceGroup: { zh: '\u65b0\u5efa\u670d\u52a1\u7ec4', en: 'New Service Group' },
  serviceGroupDesc: { zh: '\u53ef\u4ee5\u7ed9\u4e00\u4e2a\u865a\u62df\u6a21\u578b\u540d\uff08\u9ed8\u8ba4 auto\uff09\u6302\u591a\u4e2a LLM \u670d\u52a1\u5546\uff0c\u8c03\u7528\u65f6\u76f4\u63a5\u4f7f\u7528\u8fd9\u4e2a\u540d\u5b57\uff0cHub \u4f1a\u6309 document/reasoning/tools \u7b49\u80fd\u529b\u81ea\u52a8\u9009\u62e9', en: 'You can attach multiple providers to one virtual model name, default auto. Callers use this name directly, and Hub chooses by document/reasoning/tools capabilities' },
  providerRoutes: { zh: '\u670d\u52a1\u5546\u8def\u7531', en: 'Provider Routes' },
  providerRoutesDesc: { zh: '\u540c\u540d\u865a\u62df\u6a21\u578b\u4f1a\u81ea\u52a8\u5408\u5e76\uff0c\u9002\u5408\u56f4\u7ed5 auto \u8fd9\u79cd\u5355\u4e2a\u865a\u62df\u6a21\u578b\u505a\u80fd\u529b\u5206\u6d41', en: 'Rows with the same virtual model name merge automatically, which works well for a single auto model with capability-based routing' },
  providerParams: { zh: '\u670d\u52a1\u5546\u53c2\u6570', en: 'Provider Params' },
  addRoute: { zh: '\u65b0\u589e\u8def\u7531', en: 'Add Route' },
  noRoutesYet: { zh: '\u6682\u65e0\u8def\u7531\uff0c\u8bf7\u65b0\u589e\u4e00\u6761', en: 'No routes yet' },
  cancel: { zh: '\u53d6\u6d88', en: 'Cancel' },
  saveGroup: { zh: '\u4fdd\u5b58\u670d\u52a1\u7ec4', en: 'Save Group' },
  chooseProviderFirst: { zh: '\u8bf7\u5148\u521b\u5efa\u6216\u9009\u62e9\u670d\u52a1\u5546', en: 'Choose a provider first' },
  routeNeedsModel: { zh: '\u6bcf\u6761\u8def\u7531\u90fd\u9700\u8981\u4e00\u4e2a\u865a\u62df\u6a21\u578b\u540d\uff0c\u7559\u7a7a\u65f6\u9ed8\u8ba4\u4e3a auto', en: 'Each route needs a virtual model name; empty values default to auto' },
  routeNeedsProvider: { zh: '\u6bcf\u6761\u8def\u7531\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a\u670d\u52a1\u5546', en: 'Each route needs at least one provider' },
  duplicateRouteAlias: { zh: '\u5b58\u5728\u91cd\u590d\u7684\u865a\u62df\u6a21\u578b\u540d\uff1a{name}\u3002\u540c\u540d\u865a\u62df\u6a21\u578b\u4fdd\u5b58\u540e\u4f1a\u81ea\u52a8\u5408\u5e76\uff0c\u8bf7\u6539\u6210\u4e0d\u540c\u7684\u865a\u62df\u6a21\u578b\u540d\uff0c\u6216\u653e\u5728\u540c\u4e00\u6761\u8def\u7531\u91cc\u914d\u7f6e\u591a\u4e2a\u670d\u52a1\u5546\u3002', en: 'Duplicate virtual model name: {name}. Rows with the same name merge after save. Use different virtual model names, or attach multiple providers to one route.' },
  unknownError: { zh: '\u672a\u77e5\u9519\u8bef', en: 'unknown error' }
};
function llsX(key, vars) {
  var entry = LLS_DIALOG_I18N[key];
  var value = entry ? (currentLang === 'zh' ? entry.zh : entry.en) : key;
  if (!vars) return value;
  return value.replace(/\{(\w+)\}/g, function(match, name) {
    return Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match;
  });
}
function llsNormalizeProviderConfig(config, providerID, legacy) {
  var tags = Array.from(new Set((((config && config.capability_tags) || (legacy && legacy.capability_tags) || []).map(function(v) { return String(v || '').trim(); }).filter(Boolean))));
  var priority = Number(config && config.priority);
  if (!priority) priority = Number(legacy && legacy.priority || 0) || 0;
  var resolution = Number(config && config.resolution_tier);
  if (!resolution) resolution = Number(legacy && legacy.resolution_tier || 0) || 0;
  var multiplier = Number(config && config.credit_multiplier);
  if (!(multiplier > 0)) multiplier = Number(legacy && legacy.credit_multiplier || 1) || 1;
  return {
    provider_id: normalizeLLMServiceProviderRef(providerID || config && config.provider_id || ''),
    capability_tags: tags,
    priority: priority,
    resolution_tier: resolution,
    credit_multiplier: multiplier
  };
}
function llsAggregateProviderConfigs(configs) {
  var aggregate = { capability_tags: [], priority: 0, resolution_tier: 0, credit_multiplier: 1 };
  (configs || []).forEach(function(cfg) {
    aggregate.capability_tags = Array.from(new Set(aggregate.capability_tags.concat(cfg.capability_tags || [])));
    if (Number(cfg.priority || 0) > aggregate.priority) aggregate.priority = Number(cfg.priority || 0) || 0;
    var resolution = Number(cfg.resolution_tier || 0) || 0;
    if (!aggregate.resolution_tier || (resolution > 0 && resolution < aggregate.resolution_tier)) aggregate.resolution_tier = resolution;
    var multiplier = Number(cfg.credit_multiplier || 1) || 1;
    if (!(aggregate.credit_multiplier > 0) || multiplier < aggregate.credit_multiplier) aggregate.credit_multiplier = multiplier;
  });
  if (!(aggregate.credit_multiplier > 0)) aggregate.credit_multiplier = 1;
  return aggregate;
}
function llsCloneModel(m) {
  var legacy = {
    capability_tags: (m && m.capability_tags || []),
    priority: Number(m && m.priority || 0) || 0,
    resolution_tier: Number(m && m.resolution_tier || 0) || 0,
    credit_multiplier: Number(m && m.credit_multiplier || 1) || 1
  };
  var providerIDs = Array.from(new Set([].concat(m && m.provider_ids || [], (m && m.provider_configs || []).map(function(cfg) { return cfg && cfg.provider_id; })).map(normalizeLLMServiceProviderRef).filter(Boolean)));
  var configMap = {};
  (m && m.provider_configs || []).forEach(function(cfg) {
    var id = normalizeLLMServiceProviderRef(cfg && cfg.provider_id || '');
    if (!id) return;
    configMap[id] = llsNormalizeProviderConfig(cfg, id, legacy);
  });
  var providerConfigs = providerIDs.map(function(id) { return configMap[id] || llsNormalizeProviderConfig(null, id, legacy); });
  var aggregate = llsAggregateProviderConfigs(providerConfigs);
  return {
    name: String(m && m.name || '').trim(),
    provider_ids: providerIDs,
    provider_configs: providerConfigs,
    capability_tags: aggregate.capability_tags,
    priority: aggregate.priority,
    resolution_tier: aggregate.resolution_tier,
    credit_multiplier: aggregate.credit_multiplier
  };
}
function llsNormalizeAccessPolicy(value) {
  return String(value || '').trim().toLowerCase() === 'grant_required' ? 'grant_required' : 'free';
}
function llsAccessPolicyLabel(value) {
  return llsNormalizeAccessPolicy(value) === 'grant_required' ? llsX('accessPolicyGrantRequired') : llsX('accessPolicyFree');
}
function llsAccessPolicyHint(value) {
  return llsNormalizeAccessPolicy(value) === 'grant_required' ? llsX('accessPolicyGrantRequiredHint') : llsX('accessPolicyFreeHint');
}
function llsCloneGroup(g) {
  return {
    id: String(g && g.id || '').trim(),
    name: String(g && g.name || '').trim(),
    description: String(g && g.description || '').trim(),
    access_policy: llsNormalizeAccessPolicy(g && g.access_policy || ''),
    models: (g && g.models || []).map(llsCloneModel)
  };
}
function llsEmptyGroup() {
  return { id: '', name: '', description: '', access_policy: 'free', models: [{ name: 'auto', provider_ids: [], provider_configs: [], capability_tags: [], priority: 50, resolution_tier: 0, credit_multiplier: 1 }] };
}
function llsEsc(v) {
  return String(v || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
function llsProviderName(id) {
  var n = normalizeLLMServiceProviderRef(id);
  var p = llmServiceProviderOptions.find(function(x) { return x.id === n; });
  return p ? llmServiceProviderDisplay(p) : n;
}
function llsProviderChipLabel(index, id) {
  var list = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[index] && llmServiceGroupDraft.models[index].provider_ids || [];
  var pos = list.indexOf(id);
  var rank = pos >= 0 ? (' #' + String(pos + 1)) : '';
  return llsProviderName(id) + rank;
}
function ensureLLMServiceGroupModalUI() {
  if (document.getElementById('llmServiceGroupModalOverlay')) return;
  var o = document.createElement('div');
  o.id = 'llmServiceGroupModalOverlay';
  o.className = 'session-modal-overlay';
  o.onclick = function(e) { if (e.target === o) closeLLMServiceGroupDialog(); };
  o.innerHTML = '<div class="session-modal" style="width:min(920px,calc(100% - 48px))"><button class="close-btn" onclick="closeLLMServiceGroupDialog()" aria-label="' + llsEsc(llsX('close')) + '">&times;</button><div id="llmServiceGroupModalBody"></div></div>';
  document.body.appendChild(o);
}
function ensureLLMServiceProviderModalUI() {
  if (document.getElementById('llmServiceProviderModalOverlay')) return;
  var o = document.createElement('div');
  o.id = 'llmServiceProviderModalOverlay';
  o.className = 'session-modal-overlay';
  o.style.zIndex = '10020';
  o.onclick = function(e) { if (e.target === o) closeLLSProviderDialog(); };
  o.innerHTML = '<div class="session-modal" style="width:min(560px,calc(100% - 48px))"><button class="close-btn" onclick="closeLLSProviderDialog()" aria-label="' + llsEsc(llsX('close')) + '">&times;</button><div id="llmServiceProviderModalBody"></div></div>';
  document.body.appendChild(o);
}
function syncLLMServiceLegacyEditor() {
  ['llmServiceGroupID', 'llmServiceGroupName', 'llmServiceGroupDesc', 'llmServiceGroupModels'].forEach(function(id) {
    var el = document.getElementById(id);
    if (el && el.parentElement) el.parentElement.style.display = 'none';
  });
  var add = document.getElementById('llmServiceAddGroupBtn');
  if (add) add.style.display = 'none';
  var remove = document.getElementById('llmServiceRemoveGroupBtn');
  if (remove) remove.style.display = 'none';
}
function llsProviderOptions() {
  if (!llmServiceProviderOptions.length) return '<option value="">' + escapeHtml(llsX('noProvidersYet')) + '</option>';
  return '<option value="">' + escapeHtml(llsX('chooseProvider')) + '</option>' + llmServiceProviderOptions.map(function(p) {
    return '<option value="' + llsEsc(p.id) + '">' + escapeHtml(llmServiceProviderDisplay(p)) + '</option>';
  }).join('');
}
function openLLMServiceGroupDialog(mode, id) {
  ensureLLMServiceGroupModalUI();
  var g = mode === 'edit' ? ((llmServiceAdminCache && llmServiceAdminCache.model_service_groups || []).find(function(x) { return x.id === id; }) || null) : null;
  if (mode === 'edit' && (!g || isBuiltinLLMServiceGroup(g.id))) {
    showToast(lsx('builtInDefaultReadOnly'), 'info');
    return;
  }
  llmServiceGroupDialogMode = mode === 'edit' ? 'edit' : 'create';
  llmServiceSelectedGroupID = g && g.id || '';
  llmServiceGroupDraft = g ? llsCloneGroup(g) : llsEmptyGroup();
  renderLLMServiceGroupDialog();
  var o = document.getElementById('llmServiceGroupModalOverlay');
  if (o) o.classList.add('show');
  setTimeout(function() { var first = document.getElementById('llsGroupId'); if (first && typeof first.focus === 'function') first.focus(); }, 0);
}
function closeLLMServiceGroupDialog() {
  closeLLSProviderDialog();
  var o = document.getElementById('llmServiceGroupModalOverlay');
  if (o) o.classList.remove('show');
  llmServiceGroupDraft = null;
}
function llsProviderConfig(model, providerID) {
  if (!model) return null;
  model.provider_configs = model.provider_configs || [];
  var normalizedID = normalizeLLMServiceProviderRef(providerID);
  if (!normalizedID) return null;
  for (var i = 0; i < model.provider_configs.length; i++) {
    if (normalizeLLMServiceProviderRef(model.provider_configs[i] && model.provider_configs[i].provider_id || '') === normalizedID) return model.provider_configs[i];
  }
  var cfg = llsNormalizeProviderConfig(null, normalizedID, model);
  model.provider_configs.push(cfg);
  return cfg;
}
function llsSyncModelProviderState(model) {
  if (!model) return;
  model.provider_ids = Array.from(new Set((model.provider_ids || []).map(normalizeLLMServiceProviderRef).filter(Boolean)));
  model.provider_configs = (model.provider_ids || []).map(function(id) { return llsProviderConfig(model, id); }).filter(Boolean);
  var aggregate = llsAggregateProviderConfigs(model.provider_configs || []);
  model.capability_tags = aggregate.capability_tags;
  model.priority = aggregate.priority;
  model.resolution_tier = aggregate.resolution_tier;
  model.credit_multiplier = aggregate.credit_multiplier;
}
function llsProviderSummaryLine(label, value) {
  return '<div class="item-meta" style="margin-top:6px"><strong>' + escapeHtml(label) + ':</strong> <span class="mono">' + escapeHtml(value || lsx('emptyValue')) + '</span></div>';
}
function llsProviderExtraTags(cfg) {
  return (cfg && cfg.capability_tags || []).filter(function(v) { return llmServiceCapabilityOptions.indexOf(v) < 0; }).join(', ');
}
function llsRenderProviderCard(rowIndex, providerID, providerIndex, total) {
  var cfg = llsProviderConfig(llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[rowIndex], providerID);
  var escapedID = llmServiceJSArg(providerID);
  var upDisabled = providerIndex <= 0 ? ' disabled' : '';
  var downDisabled = providerIndex >= total - 1 ? ' disabled' : '';
  var features = (cfg && cfg.capability_tags || []).filter(function(v) { return llmServiceCapabilityOptions.indexOf(v) >= 0; }).join(', ');
  var extra = llsProviderExtraTags(cfg);
  return '<div class="item" style="margin-top:10px;border:1px solid rgba(47,128,237,.12);background:rgba(47,128,237,.04)">'
    + '<div class="item-head"><div><div class="item-title">' + escapeHtml(llsProviderChipLabel(rowIndex, providerID)) + '</div><div class="item-meta">' + escapeHtml(llsX('providerParams')) + '</div></div>'
    + '<div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">'
    + '<button class="btn-ghost" type="button" style="height:28px;padding:0 10px" onclick="openLLSProviderDialog(' + rowIndex + ',\'' + escapedID + '\')">' + escapeHtml(lsx('edit')) + '</button>'
    + '<button class="btn-ghost" type="button" style="height:28px;padding:0 10px" onclick="llsMoveProvider(' + rowIndex + ',\'' + escapedID + '\',-1)"' + upDisabled + '>' + escapeHtml(llsX('up')) + '</button>'
    + '<button class="btn-ghost" type="button" style="height:28px;padding:0 10px" onclick="llsMoveProvider(' + rowIndex + ',\'' + escapedID + '\',1)"' + downDisabled + '>' + escapeHtml(llsX('down')) + '</button>'
    + '<button class="btn-danger" type="button" style="height:28px;padding:0 10px" onclick="llsRemoveProvider(' + rowIndex + ',\'' + escapedID + '\')">' + escapeHtml(lsx('remove')) + '</button>'
    + '</div></div>'
    + llsProviderSummaryLine(llsX('featureSummary'), features)
    + llsProviderSummaryLine(llsX('extraFeatureSummary'), extra)
    + llsProviderSummaryLine(llsX('priority'), String(Number(cfg && cfg.priority || 0) || 0))
    + llsProviderSummaryLine(llsX('resolutionTier'), String(Number(cfg && cfg.resolution_tier || 0) || 0))
    + llsProviderSummaryLine(llsX('creditMultiplier'), String(Number(cfg && cfg.credit_multiplier || 1) || 1))
    + '</div>';
}
function llsProviderDialogDraft(rowIndex, providerID) {
  var model = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[rowIndex];
  var cfg = model && providerID ? llsProviderConfig(model, providerID) : null;
  var normalizedProviderID = normalizeLLMServiceProviderRef(cfg && cfg.provider_id || providerID || '');
  if (!normalizedProviderID && llmServiceProviderOptions.length) normalizedProviderID = normalizeLLMServiceProviderRef(llmServiceProviderOptions[0] && llmServiceProviderOptions[0].id || '');
  return {
    provider_id: normalizedProviderID,
    capability_tags: Array.from(new Set((cfg && cfg.capability_tags || []).map(function(v) { return String(v || '').trim(); }).filter(Boolean))),
    priority: Number(cfg && cfg.priority || 0) || 0,
    resolution_tier: Number(cfg && cfg.resolution_tier || 0) || 0,
    credit_multiplier: Number(cfg && cfg.credit_multiplier || 1) || 1
  };
}
function openLLSProviderDialog(rowIndex, providerID) {
  ensureLLMServiceProviderModalUI();
  llmServiceProviderDialogState = {
    rowIndex: rowIndex,
    originalProviderID: normalizeLLMServiceProviderRef(providerID || ''),
    draft: llsProviderDialogDraft(rowIndex, providerID)
  };
  renderLLSProviderDialog();
  var o = document.getElementById('llmServiceProviderModalOverlay');
  if (o) o.classList.add('show');
}
function closeLLSProviderDialog() {
  var o = document.getElementById('llmServiceProviderModalOverlay');
  if (o) o.classList.remove('show');
  llmServiceProviderDialogState = null;
}
function llsSetProviderDialogField(key, value) {
  if (!llmServiceProviderDialogState || !llmServiceProviderDialogState.draft) return;
  llmServiceProviderDialogState.draft[key] = value;
}
function llsToggleProviderDialogFeature(feature, enabled) {
  if (!llmServiceProviderDialogState || !llmServiceProviderDialogState.draft) return;
  var set = new Set(llmServiceProviderDialogState.draft.capability_tags || []);
  if (enabled) set.add(feature); else set.delete(feature);
  llmServiceProviderDialogState.draft.capability_tags = Array.from(set);
}
function llsSetProviderDialogExtra(value) {
  if (!llmServiceProviderDialogState || !llmServiceProviderDialogState.draft) return;
  var keep = (llmServiceProviderDialogState.draft.capability_tags || []).filter(function(x) { return llmServiceCapabilityOptions.indexOf(x) >= 0; });
  llmServiceProviderDialogState.draft.capability_tags = Array.from(new Set(keep.concat(parseCSV(value).filter(function(x) { return llmServiceCapabilityOptions.indexOf(x) < 0; }))));
}
function renderLLSProviderDialog() {
  ensureLLMServiceProviderModalUI();
  var root = document.getElementById('llmServiceProviderModalBody');
  var state = llmServiceProviderDialogState;
  if (!root || !state || !state.draft) return;
  var draft = state.draft;
  var originalProviderID = normalizeLLMServiceProviderRef(state.originalProviderID || '');
  var title = originalProviderID ? llsX('editProviderConfig') : llsX('addProviderConfig');
  var featureChecks = llmServiceCapabilityOptions.map(function(feature) {
    var checked = (draft.capability_tags || []).indexOf(feature) >= 0 ? ' checked' : '';
    return '<label style="display:inline-flex;align-items:center;gap:6px;margin-right:12px;margin-top:8px"><input type="checkbox"' + checked + ' onchange="llsToggleProviderDialogFeature(\'' + feature + '\',this.checked)">' + escapeHtml(feature) + '</label>';
  }).join('');
  var extra = llsProviderExtraTags(draft);
  var providerOptions = !llmServiceProviderOptions.length
    ? ('<option value="">' + escapeHtml(llsX('noProvidersYet')) + '</option>')
    : ('<option value="">' + escapeHtml(llsX('chooseProvider')) + '</option>' + llmServiceProviderOptions.map(function(p) {
        var id = normalizeLLMServiceProviderRef(p && p.id || '');
        return '<option value="' + llsEsc(id) + '"' + (id === normalizeLLMServiceProviderRef(draft.provider_id || '') ? ' selected' : '') + '>' + escapeHtml(llmServiceProviderDisplay(p)) + '</option>';
      }).join(''));
  root.innerHTML = '<div class="item" style="border:none;box-shadow:none;padding:0;background:transparent">'
    + '<div class="item-title">' + escapeHtml(title) + '</div>'
    + '<div class="grid2" style="margin-top:10px">'
    + '<div style="grid-column:1 / -1"><label>' + escapeHtml(llsX('providerName')) + '</label><select ' + (originalProviderID ? 'disabled' : '') + ' onchange="llsSetProviderDialogField(\'provider_id\', normalizeLLMServiceProviderRef(this.value || \'\'))" style="width:100%">' + providerOptions + '</select></div>'
    + '<div style="grid-column:1 / -1"><label>' + escapeHtml(llsX('featureTags')) + '</label><div>' + featureChecks + '</div></div>'
    + '<div style="grid-column:1 / -1"><label>' + escapeHtml(llsX('extraFeatures')) + '</label><input value="' + llsEsc(extra) + '" placeholder="' + llsEsc(llsX('extraFeaturesPlaceholder')) + '" oninput="llsSetProviderDialogExtra(this.value)"></div>'
    + '<div><label>' + escapeHtml(llsX('priority')) + '</label><select onchange="llsSetProviderDialogField(\'priority\', Number(this.value || 0) || 0)">' + llmServicePriorityOptions.map(function(v) { return '<option value="' + v + '"' + (Number(draft.priority || 0) === v ? ' selected' : '') + '>' + v + '</option>'; }).join('') + '</select></div>'
    + '<div><label>' + escapeHtml(llsX('resolutionTier')) + '</label><select onchange="llsSetProviderDialogField(\'resolution_tier\', Number(this.value || 0) || 0)">' + llmServiceResolutionOptions.map(function(v) { return '<option value="' + v + '"' + (Number(draft.resolution_tier || 0) === v ? ' selected' : '') + '>' + v + '</option>'; }).join('') + '</select><div class="hint" style="margin-top:6px">' + escapeHtml(llsX('resolutionTierHint')) + '</div></div>'
    + '<div><label>' + escapeHtml(llsX('creditMultiplier')) + '</label><select onchange="llsSetProviderDialogField(\'credit_multiplier\', Number(this.value || 1) || 1)">' + llmServiceMultiplierOptions.map(function(v) { return '<option value="' + v + '"' + (Number(draft.credit_multiplier || 1) === v ? ' selected' : '') + '>' + v + '</option>'; }).join('') + '</select></div>'
    + '</div>'
    + '<div class="actions" style="margin-top:10px"><button class="btn-ghost" type="button" onclick="closeLLSProviderDialog()">' + escapeHtml(llsX('cancel')) + '</button><button class="btn-primary" type="button" onclick="saveLLSProviderDialog()">' + escapeHtml(llsX('saveProvider')) + '</button></div>'
    + '</div>';
}
function saveLLSProviderDialog() {
  var state = llmServiceProviderDialogState;
  var model = llmServiceGroupDraft && llmServiceGroupDraft.models && state && llmServiceGroupDraft.models[state.rowIndex];
  if (!state || !state.draft || !model) return;
  var providerSelect = document.querySelector('#llmServiceProviderModalBody select');
  var providerID = normalizeLLMServiceProviderRef(state.draft.provider_id || (providerSelect && providerSelect.value) || '');
  if (!providerID) { showToast(llsX('chooseProviderFirst'), 'info'); return; }
  state.draft.provider_id = providerID;
  var originalProviderID = normalizeLLMServiceProviderRef(state.originalProviderID || '');
  if ((!originalProviderID || providerID !== originalProviderID) && (model.provider_ids || []).indexOf(providerID) >= 0) { showToast(llsX('providerAlreadyAdded'), 'info'); return; }
  if (originalProviderID && providerID !== originalProviderID) {
    model.provider_ids = (model.provider_ids || []).filter(function(v) { return normalizeLLMServiceProviderRef(v) !== originalProviderID; });
    model.provider_configs = (model.provider_configs || []).filter(function(cfg) { return normalizeLLMServiceProviderRef(cfg && cfg.provider_id || '') !== originalProviderID; });
  }
  if ((model.provider_ids || []).indexOf(providerID) < 0) model.provider_ids.push(providerID);
  var cfg = llsProviderConfig(model, providerID);
  cfg.capability_tags = Array.from(new Set((state.draft.capability_tags || []).map(function(v) { return String(v || '').trim(); }).filter(Boolean)));
  cfg.priority = Number(state.draft.priority || 0) || 0;
  cfg.resolution_tier = Number(state.draft.resolution_tier || 0) || 0;
  cfg.credit_multiplier = Number(state.draft.credit_multiplier || 1) || 1;
  llsSyncModelProviderState(model);
  closeLLSProviderDialog();
  renderLLMServiceGroupDialog();
}
function llsRenderRouteRow(m, i) {
  llsSyncModelProviderState(m);
  var providerCards = (m.provider_ids || []).map(function(id, providerIndex) {
    return llsRenderProviderCard(i, id, providerIndex, (m.provider_ids || []).length);
  }).join('');
  return `
    <div class="item" style="margin-top:10px;border:1px solid rgba(47,128,237,.14)">
      <div class="item-head">
        <div>
          <div class="item-title">${escapeHtml(llsX('routeTitle', { index: i + 1 }))}</div>
          <div class="item-meta">${escapeHtml(llsX('routeDesc'))}</div>
        </div>
        <button class="btn-danger" type="button" style="height:30px;padding:0 12px" onclick="llsRemoveRow(${i})">${escapeHtml(llsX('removeRoute'))}</button>
      </div>
      <div class="grid2" style="margin-top:10px;align-items:end">
        <div>
          <label>${escapeHtml(llsX('exposedModel'))}</label>
          <input value="${llsEsc(m.name || 'auto')}" placeholder="auto" oninput="llsSetRow(${i},'name',this.value)">
        </div>
        <div>
          <div class="actions" style="justify-content:flex-end">
            <button class="btn-ghost" type="button" onclick="openLLSProviderDialog(${i})">${escapeHtml(llsX('addProviderConfig'))}</button>
          </div>
        </div>
      </div>
      <div style="margin-top:10px">${providerCards || ('<div class="item" style="border:1px dashed rgba(47,128,237,.24);background:rgba(47,128,237,.04)"><div class="item-meta">' + escapeHtml(llsX('noProvidersInRoute')) + '</div><div class="actions" style="margin-top:10px"><button class="btn-ghost" type="button" onclick="openLLSProviderDialog(' + i + ')">' + escapeHtml(llsX('addProviderConfig')) + '</button></div></div>')}</div>
    </div>`;
}
function renderLLMServiceGroupDialog() {
  ensureLLMServiceGroupModalUI();
  var overlay = document.getElementById('llmServiceGroupModalOverlay');
  if (overlay) {
    var closeBtn = overlay.querySelector('.close-btn');
    if (closeBtn) closeBtn.setAttribute('aria-label', llsX('close'));
  }
  var r = document.getElementById('llmServiceGroupModalBody');
  if (!r) return;
  var d = llmServiceGroupDraft || llsEmptyGroup();
  var rows = (d.models || []).map(function(m, i) { return llsRenderRouteRow(m, i); }).join('');
  var providerRef = llmServiceProviderOptions.length
    ? escapeHtml(llsX('createdProvidersPrefix')) + escapeHtml(llmServiceProviderOptions.map(llmServiceProviderDisplay).join(', '))
    : '<span class="hint">' + escapeHtml(llsX('noProvidersYet')) + '</span>';
  r.innerHTML = `
    <div class="item" style="border:none;box-shadow:none;padding:0;background:transparent">
      <div class="item-title">${escapeHtml(llmServiceGroupDialogMode === 'edit' ? llsX('editServiceGroup') : llsX('newServiceGroup'))}</div>
      <div class="item-meta" style="margin-top:6px">${escapeHtml(llsX('serviceGroupDesc'))}</div>
      <div class="hint" style="margin-top:10px">${providerRef}</div>
      <div class="grid2" style="margin-top:10px">
        <div><label>${escapeHtml(lsx('id'))}</label><input id="llsGroupId" value="${llsEsc(d.id)}" placeholder="${llsEsc(lsx('groupIdPlaceholder'))}" oninput="llsSetGroup('id',this.value)"></div>
        <div><label>${escapeHtml(lsx('name'))}</label><input value="${llsEsc(d.name)}" placeholder="${llsEsc(lsx('groupNamePlaceholder'))}" oninput="llsSetGroup('name',this.value)"></div>
        <div style="grid-column:1 / -1"><label>${escapeHtml(lsx('description'))}</label><input value="${llsEsc(d.description)}" placeholder="${llsEsc(lsx('groupDescPlaceholder'))}" oninput="llsSetGroup('description',this.value)"></div>
        <div><label>${escapeHtml(llsX('accessPolicy'))}</label><select onchange="llsSetGroup('access_policy', this.value)"><option value="free"${llsNormalizeAccessPolicy(d.access_policy) === 'free' ? ' selected' : ''}>${escapeHtml(llsX('accessPolicyFree'))}</option><option value="grant_required"${llsNormalizeAccessPolicy(d.access_policy) === 'grant_required' ? ' selected' : ''}>${escapeHtml(llsX('accessPolicyGrantRequired'))}</option></select></div>
        <div><label>${escapeHtml(llsX('accessPolicyHint'))}</label><div class="hint" style="margin-top:8px">${escapeHtml(llsAccessPolicyHint(d.access_policy))}</div></div>
      </div>
      <div style="margin-top:10px">
        <div class="item-head">
          <div>
            <div class="item-title">${escapeHtml(llsX('providerRoutes'))}</div>
            <div class="item-meta">${escapeHtml(llsX('providerRoutesDesc'))}</div>
          </div>
          <button class="btn-ghost" type="button" onclick="llsAddRow()">${escapeHtml(llsX('addRoute'))}</button>
        </div>
        ${rows || ('<div class="hint" style="margin-top:10px">' + escapeHtml(llsX('noRoutesYet')) + '</div>')}
      </div>
      <div class="actions" style="margin-top:10px">
        <button class="btn-ghost" type="button" onclick="closeLLMServiceGroupDialog()">${escapeHtml(llsX('cancel'))}</button>
        <button class="btn-primary" type="button" onclick="saveLLMServiceGroupDialog()">${escapeHtml(llsX('saveGroup'))}</button>
      </div>
    </div>`;
}
function llsSetGroup(k, v) { if (!llmServiceGroupDraft) llmServiceGroupDraft = llsEmptyGroup(); llmServiceGroupDraft[k] = k === 'access_policy' ? llsNormalizeAccessPolicy(v) : String(v || '').trim(); if (k === 'access_policy') renderLLMServiceGroupDialog(); }
function llsAddRow() { if (!llmServiceGroupDraft) llmServiceGroupDraft = llsEmptyGroup(); llmServiceGroupDraft.models.push({ name: 'auto', provider_ids: [], provider_configs: [], capability_tags: [], priority: 50, resolution_tier: 0, credit_multiplier: 1 }); renderLLMServiceGroupDialog(); }
function llsRemoveRow(i) { if (llmServiceGroupDraft && llmServiceGroupDraft.models) { llmServiceGroupDraft.models.splice(i, 1); renderLLMServiceGroupDialog(); } }
function llsSetRow(i, k, v) { if (llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]) llmServiceGroupDraft.models[i][k] = String(v || '').trim(); }
function llsSetProviderNum(i, providerID, k, v) { var m = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]; if (!m) return; var cfg = llsProviderConfig(m, providerID); if (!cfg) return; cfg[k] = k === 'credit_multiplier' ? (Number(v || 1) || 1) : (Number(v || 0) || 0); llsSyncModelProviderState(m); }
function llsAddProvider(i) { var s = document.getElementById('llsProviderSel' + i); var id = normalizeLLMServiceProviderRef(s && s.value || ''); if (!id) { showToast(llsX('chooseProviderFirst'), 'info'); return; } var m = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]; if (!m) return; if ((m.provider_ids || []).indexOf(id) < 0) m.provider_ids.push(id); llsSyncModelProviderState(m); renderLLMServiceGroupDialog(); }
function llsMoveProvider(i, id, delta) { var m = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]; if (!m) return; var list = m.provider_ids || []; var from = list.indexOf(id); if (from < 0) return; var to = from + Number(delta || 0); if (to < 0 || to >= list.length) return; var item = list.splice(from, 1)[0]; list.splice(to, 0, item); m.provider_ids = list; llsSyncModelProviderState(m); renderLLMServiceGroupDialog(); }
function llsRemoveProvider(i, id) { var m = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]; if (!m) return; m.provider_ids = (m.provider_ids || []).filter(function(v) { return v !== id; }); m.provider_configs = (m.provider_configs || []).filter(function(cfg) { return normalizeLLMServiceProviderRef(cfg && cfg.provider_id || '') !== normalizeLLMServiceProviderRef(id); }); llsSyncModelProviderState(m); renderLLMServiceGroupDialog(); }
function llsRemoveGroupById(id) { if (!llmServiceAdminCache || !id) return; if (isBuiltinLLMServiceGroup(id)) { showToast(lsx('builtInDefaultCannotRemove'), 'info'); return; } pruneLLMServiceGroupReferences(id); if (llmServiceGroupIDKey(llmServiceSelectedGroupID) === llmServiceGroupIDKey(id)) llmServiceSelectedGroupID = ''; renderLLMServiceAdmin(); }
function llsToggleProviderFeature(i, providerID, f, on) { var m = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]; if (!m) return; var cfg = llsProviderConfig(m, providerID); if (!cfg) return; var s = new Set(cfg.capability_tags || []); if (on) s.add(f); else s.delete(f); cfg.capability_tags = Array.from(s); llsSyncModelProviderState(m); }
function llsSetProviderExtra(i, providerID, v) { var m = llmServiceGroupDraft && llmServiceGroupDraft.models && llmServiceGroupDraft.models[i]; if (!m) return; var cfg = llsProviderConfig(m, providerID); if (!cfg) return; var p = (cfg.capability_tags || []).filter(function(x) { return llmServiceCapabilityOptions.indexOf(x) >= 0; }); cfg.capability_tags = Array.from(new Set(p.concat(parseCSV(v).filter(function(x) { return llmServiceCapabilityOptions.indexOf(x) < 0; })))); llsSyncModelProviderState(m); }
async function saveLLMServiceGroupDialog() { if (!llmServiceAdminCache) llmServiceAdminCache = { model_service_groups: [], global_service_group_ids: [], group_bindings: [], user_bindings: [], cards: [], grants: [] }; var n = llsCloneGroup(llmServiceGroupDraft || llsEmptyGroup()); if (!n.id || !n.name) { showToast(lsx('groupIdNameRequired'), 'error'); return; } n.access_policy = llsNormalizeAccessPolicy(n.access_policy); if (isBuiltinLLMServiceGroup(n.id) || isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast(lsx('builtInDefaultReadOnly'), 'info'); return; } var routeNameSeen = {}; for (var i = 0; i < (n.models || []).length; i++) { n.models[i].name = String(n.models[i].name || '').trim() || 'auto'; if (!(n.models[i].provider_ids || []).length) { showToast(llsX('routeNeedsProvider'), 'error'); return; } var routeNameKey = String(n.models[i].name || '').trim().toLowerCase(); if (routeNameSeen[routeNameKey]) { showToast(llsX('duplicateRouteAlias', { name: n.models[i].name }), 'error'); return; } routeNameSeen[routeNameKey] = true; } var gs = llmServiceAdminCache.model_service_groups || [], si = gs.findIndex(function(g) { return llmServiceGroupIDKey(g && g.id) === llmServiceGroupIDKey(llmServiceSelectedGroupID); }), oldID = si >= 0 ? gs[si].id : '', di = gs.findIndex(function(g, idx) { return llmServiceGroupIDKey(g && g.id) === llmServiceGroupIDKey(n.id) && idx !== si; }); if (di >= 0) { showToast(lsx('duplicateGroupId', { id: n.id }), 'error'); return; } if (si >= 0 && llmServiceGroupIDKey(oldID) !== llmServiceGroupIDKey(n.id)) { showToast(lsx('groupIdImmutable'), 'error'); return; } if (si >= 0) gs[si] = n; else gs.push(n); llmServiceAdminCache.model_service_groups = gs; llmServiceSelectedGroupID = n.id; closeLLMServiceGroupDialog(); renderLLMServiceAdmin(); await saveLLMServiceAdmin(); }

var _renderLLMServiceAdmin=renderLLMServiceAdmin;renderLLMServiceAdmin=function(){ensureLLMServiceGroupModalUI();ensureLLMServiceProviderModalUI();_renderLLMServiceAdmin();syncLLMServiceLegacyEditor()};
