/**
 * HubCenter Admin: LLM Service Tab
 * - Providers (CRUD, sequence, traffic, pause)
 * - Compute agents
 * - Service groups (dynamic official bands, default, traffic dialog)
 * - Classification head + embedding runtime
 * ASCII only. Chinese via \uXXXX or data-i18n.
 */

if (typeof I18N_EN !== 'undefined') {
  Object.assign(I18N_EN, {sgRouteHint:'Exposed model alias with provider failover',providerProbeModels:'Probe',providerProbing:'Probing models...',providerProbeEmpty:'No models returned.',providerProbeFailed:'Probe failed',providerCapabilityPreset:'Preset capabilities',fieldCacheReadCredits:'Cache Read Credits / 10k',fieldCacheWriteCredits:'Cache Write Credits / 10k',fieldCacheReadRMB:'Cache Read RMB / 10k',fieldCacheWriteRMB:'Cache Write RMB / 10k',sgPricingOverride:'Override provider base price for this service group'});
}
if (typeof I18N_ZH !== 'undefined') {
  Object.assign(I18N_ZH, {sgRouteHint:'\u66b4\u9732\u6a21\u578b\u522b\u540d\uff0c\u6309\u670d\u52a1\u5546\u4f18\u5148\u7ea7\u5b9e\u73b0\u6545\u969c\u8f6c\u79fb',providerProbeModels:'\u63a2\u6d4b',providerProbing:'\u6b63\u5728\u63a2\u6d4b\u6a21\u578b...',providerProbeEmpty:'\u672a\u8fd4\u56de\u6a21\u578b\u5217\u8868\u3002',providerProbeFailed:'\u63a2\u6d4b\u5931\u8d25',providerCapabilityPreset:'\u9884\u7f6e\u80fd\u529b',fieldCacheReadCredits:'\u7f13\u5b58\u8bfb\u53d6 Credits / \u4e07',fieldCacheWriteCredits:'\u7f13\u5b58\u5199\u5165 Credits / \u4e07',fieldCacheReadRMB:'\u7f13\u5b58\u8bfb\u53d6 RMB / \u4e07',fieldCacheWriteRMB:'\u7f13\u5b58\u5199\u5165 RMB / \u4e07',sgPricingOverride:'\u8986\u76d6\u8be5\u670d\u52a1\u7ec4\u7684\u670d\u52a1\u5546\u57fa\u51c6\u4ef7\u683c'});
}

(function() {
  'use strict';

  var I18N = {
    en: {
      llmTabTitle: 'LLM Service', llmTabDesc: 'Manage LLM providers, compute agents, and model service groups.',
      providersTitle: 'LLM Providers', providersDesc: 'Backend LLM API endpoints for model routing.',
      addProvider: 'Add Provider', editProvider: 'Edit', deleteProvider: 'Delete', noProviders: 'No providers configured.',
      deleteProviderBound: 'This provider is bound to the following service groups:', deleteProviderPruneHint: 'Deleting it will automatically remove it from these service groups.',
      deleteProviderInUse: 'Delete blocked: the provider is still bound to service groups:',
      providerDialogTitleNew: 'New Provider', providerDialogTitleEdit: 'Edit Provider',
      fieldID: 'Provider ID', fieldName: 'Name', fieldURL: 'API URL', fieldKey: 'API Key',
      fieldAuth: 'Authentication', authAPIKey: 'API Key',
      authWorkBuddyChina: 'WorkBuddy China', authWorkBuddyGlobal: 'WorkBuddy International',
      workBuddyLogin: 'Sign in and list models', workBuddyWaiting: 'Waiting for the browser sign-in to finish...',
      workBuddyReady: 'Select the models to connect.', workBuddyNeedLogin: 'Sign in to WorkBuddy before saving.',
      workBuddyNeedModel: 'Select at least one model.', workBuddyFailed: 'WorkBuddy sign-in failed',
      workBuddyHint: 'After sign-in, choose models and save. This account becomes a provider. Expose the models from a service group.',
      workBuddyOpen: 'If the login page did not open, use this link.',
      workBuddyCatalogWarn: 'The live catalog was unavailable. Showing the built-in model list.',
      fieldProtocol: 'Protocol', fieldModels: 'Models (comma-separated)', fieldCapabilities: 'Capabilities',
      fieldPriority: 'Priority', fieldConcurrency: 'Max Concurrency', fieldTimeout: 'Timeout (sec)',
      fieldSequence: 'Sequence', sequenceHint: 'Lower numbers are tried first. 0 means unset.',
      accessScope: 'Access scope', accessScopeAll: 'All nodes', accessScopeSelected: 'Selected nodes',
      accessScopeHint: 'Default: every HubCenter node may call this provider. Restricting the list makes upstream calls leave only from allowed nodes, to avoid regional model limits.',
      accessScopeAvailable: 'Available nodes: ', accessScopeAvailableEmpty: 'No cluster nodes are listed. You can still type node names.',
      accessScopeNodesInput: 'Nodes', accessScopeNodesPlaceholder: 'hc-1, hc-2, hc-3',
      accessScopeNeedNode: 'Select at least one HubCenter node.', accessScopeOffline: 'offline', accessScopeSelf: 'this node', accessScopeUnreachable: 'unreachable',
      lbGroup: 'LB group', pauseProvider: 'Pause', resumeProvider: 'Resume',
      providerArray: 'Provider array', providerArrayOwn: 'Independent array', providerArrayJoin: 'Array',
      providerArrayName: 'Array name', providerArrayHint: 'One array is one logical provider. Members share the multiplier and token price, rotate on each request, and a 429 or 5xx tries the next member.',
      providerArrayUseShared: 'This provider will use the array multiplier and token price.',
      providerArrayAdd: 'Add provider', providerArrayDelete: 'Delete array', providerArrayShared: 'Shared rate',
      providerArrayRename: 'Rename', providerArrayRenameTitle: 'Rename array',
      providerArrayEdit: 'Edit', providerArrayEditTitle: 'Edit array', providerArrayCreateTitle: 'New array',
      providerArrayID: 'Array ID', providerArrayNeedID: 'Enter an array ID.',
      providerArrayEmpty: 'No providers in this array yet.',
      providerArrayDragHint: 'Drag onto another array',
      providerArrayMoved: 'Moved into that array. It now uses the array rate.',
      providerArrayBusy: 'That array is still updating. Try the drag again in a moment.',
      providerArrayTrafficSum: 'Total', providerArraysTrafficSum: 'All arrays',
      providerArrayBillingOnEdit: 'Token pricing and the vendor multiplier are set on the array.',
      providerArrayRenameNeedName: 'Enter an array name.',
      providerArrayRenameTooLong: 'Array name must be 80 characters or fewer.',
      providerArrayRemove: 'Remove this provider from the array? The array stays on its service groups.',
      providerArrayRemoveProtected: 'Remove this provider? The platform array stays. A model that no remaining provider in the array serves is taken off service groups.',
      providerArrayExpand: 'Expand', providerArrayCollapse: 'Collapse',
      providerArrayDeleteConfirm: 'Delete this provider array and every provider in it?',
      providerArrayDeleteInUse: 'This provider array is used by the following service groups and cannot be deleted:',
      providerArrayProtected: 'This array is maintained by the platform and cannot be deleted.',
      providerArrayRenameProtected: 'This array is maintained by the platform and cannot be renamed.',
      providerCanaryUntil: 'Canary until {time}',
      sgRouteModelUnmatched: 'These routes name an upstream model that no member of the array offers:',
      sgRouteModelUnmatchedAsk: 'Save anyway?',
      sgMemberModelReferenced: 'These routes still name this member\'s model:',
      adminAPIKeyTitle: 'Automation API key',
      adminAPIKeyDesc: 'Programs use this key to list arrays, batch-add or dry-run, disable or delete members, read health, test a saved member and read its current status, and list node names. Upstream keys are returned only as configured-or-not or the last 4 characters. Saved keys stay in this list, hidden until shown. Copy one, or delete it to revoke access.',
      adminAPIKeyName: 'Key name', adminAPIKeyCreate: 'Create key', adminAPIKeyDelete: 'Delete',
      adminAPIKeyEmpty: 'No automation keys yet.', adminAPIKeyNeedName: 'Enter a key name.',
      adminAPIKeyCreated: 'Saved. It stays hidden until you show it. Copy and delete work from the list.',
      adminAPIKeyCopy: 'Copy', adminAPIKeyCopied: 'Copied',
      adminAPIKeyShow: 'Show', adminAPIKeyHide: 'Hide',
      adminAPIKeyLegacy: 'The full secret was not saved. Delete this key to revoke it.',
      adminAPIKeyAllScopes: 'All permissions', adminAPIKeyExpired: 'Expired',
      adminAPIKeyLastUsed: 'Last used',
      adminAPIKeyDeleteConfirm: 'Delete this automation key? Programs using it can no longer manage provider arrays.',
      adminAPIKeyDoc: 'API doc for AI tools', adminAPIKeyOpenAPI: 'OpenAPI JSON',
      adminAPIKeyCreatedAt: 'Created',
      trafficDay: 'Day', trafficWeek: 'Week', trafficMonth: 'Month', trafficLoading: 'Loading',
      trafficIn: 'In', trafficOut: 'Out', trafficTotal: 'Total',
      providerProbeModels: 'Probe', providerProbing: 'Probing models...', providerProbeEmpty: 'No models returned.',
      providerProbeFailed: 'Probe failed', providerCapabilityPreset: 'Preset capabilities',
      agentsTitle: 'Compute Agents', agentsDesc: 'Upstream compute resellers for settlement and customer-facing attribution.',
      addAgent: 'Add Agent', editAgent: 'Edit', deleteAgent: 'Delete', noAgents: 'No agents configured.',
      agentDialogTitleNew: 'New Compute Agent', agentDialogTitleEdit: 'Edit Compute Agent',
      fieldAgentID: 'Agent ID', fieldAgentName: 'Agent Name', fieldAgentContact: 'Contact', fieldAgentSettlement: 'Settlement',
      fieldAgentDesc: 'Description', fieldGroupAgent: 'Compute Agent', sgAgentRequired: 'Please select a compute agent.',
      groupsTitle: 'Model Service Groups', groupsDesc: 'Route models to providers with dispatch policies.',
      addGroup: 'Add Service Group', editGroup: 'Edit', deleteGroup: 'Delete', noGroups: 'No service groups.',
      groupDialogTitleNew: 'New Service Group', groupDialogTitleEdit: 'Edit Service Group',
      fieldGroupID: 'Group ID', fieldGroupName: 'Group Name', fieldGroupDesc: 'Description',
      fieldGroupModels: 'Models (JSON)', modelNamePlaceholder: 'e.g. gpt-4, claude-3.5',
      fieldGroupKind: 'Kind', sgKindDynamic: 'Dynamic', sgKindStatic: 'Static',
      sgRouteHint: 'Exposed model alias with provider failover',
      sgArrayDragHint: 'Drag an array to change its place in this group',
      sgArrayOrderHint: 'List order applies when capability, resolution tier, multiplier, and priority match.',
      sgMoveEarlier: 'Move earlier', sgMoveLater: 'Move later',
      sgRemoveRoute: 'Remove', sgExposedModel: 'Exposed Model', sgNoProviders: 'No providers assigned. Add a provider above.',
      sgAccessPolicy: 'Access Policy', sgPolicyFreeHint: 'no grant needed', sgPolicyGrantHint: 'needs card/grant',
      sgRoutes: 'Provider Routes', sgAddRoute: '+ Add Route',
      sgProviderAlreadyAdded: 'Provider already added to this route.',
      sgProviderConfigTitle: 'Provider Config', sgCapabilityTags: 'Capability Tags',
      sgCapabilityHint: 'Capabilities of this upstream model. Tags steer routing when the request asks for tools, vision, or similar.',
      sgExtraTags: 'Extra Tags (custom)', sgPriority: 'Priority',
      sgResolutionTier: 'Resolution Tier', sgCreditMultiplier: 'Credit Multiplier', sgFeeMultiplier: 'Fee multiplier',
      sgBillingMode: 'Billing Mode', sgBillingModeHint: 'paid = charge Credits, free = no user charge, empty = legacy',
      sgBillingModePaid: 'Paid', sgBillingModeFree: 'Free', sgBillingModeLegacy: 'Legacy (empty)',
      tokenPricingTitle: 'Token Pricing (per 10k tokens)', tokenPricingHint: 'Credits fields are billed; RMB fields are display-only and never affect Credits.',
      fieldInputCredits: 'Input Credits / 10k', fieldOutputCredits: 'Output Credits / 10k',
      fieldCacheReadCredits: 'Cache Read Credits / 10k', fieldCacheWriteCredits: 'Cache Write Credits / 10k',
      fieldInputRMB: 'Input RMB / 10k (ref)', fieldOutputRMB: 'Output RMB / 10k (ref)',
      fieldCacheReadRMB: 'Cache Read RMB / 10k', fieldCacheWriteRMB: 'Cache Write RMB / 10k',
      fieldMinimumCredits: 'Minimum Credits / request', fieldPricingTimezone: 'Pricing Timezone', fieldPricingVersion: 'Pricing Version',
      providerPriceSummary: 'Base price', providerSetPrice: 'Set price', pricingSchedule: 'Time-of-use prices', pricingAddWindow: 'Add price window', pricingRemoveWindow: 'Remove',
      pricingWindowHint: 'A matching window overrides only the prices entered below; blank values keep the base price. The first matching window wins.',
      pricingDroppedWindows: 'Fix incomplete price windows before saving.',
      billingInvalid: 'Fix invalid pricing numbers before saving.',
      billingPaidNeedsCredits: 'Paid billing requires at least one positive Credits price (input, output, or minimum).',
      sgIDNameRequired: 'ID and Name are required.', sgRouteNeedsProvider: 'Each route needs at least one provider.',
      chooseProvider: 'Choose provider array',
      fieldHubID: 'Hub ID', fieldTenantID: 'Tenant ID',
      fieldHubRequired: 'Select a Hub', fieldTenantRequired: 'Select a tenant', noHubs: 'No registered Hubs.', defaultTenant: 'Default tenant',
      save: 'Save', cancel: 'Cancel', confirm: 'Confirm', delete: 'Delete',
      saved: 'Saved successfully.', deleted: 'Deleted.', error: 'Error', sgFailed: 'Failed',
      testProvider: 'Test Status', providerTesting: 'Testing...', providerTestOK: 'Available', providerTestFailed: 'Unavailable',
      providerTestLatency: 'Latency', providerTestModels: 'Models',
      monitorSaved: 'Monitor settings saved.', monitorInvalidInterval: 'Enter a whole-number interval of at least 1 hour.',
      monitorLoadRetry: 'Load failed. Use the refresh button above to retry.',
      status: 'Status', credits: 'Credits', expires: 'Expires', active: 'Active',
      sgClassTraffic: 'Downstream traffic',
      sgClassTrafficHint: 'Successful requests billed to this group, by task class. Training lives on the Classification head tab.',
      sgClassTrafficOpen: 'Traffic',
      sgTryRules: 'Try rules', sgTryPlaceholder: 'write a business plan',
      sgTryWorkflow: 'Workflow type', sgTryPhase: 'Phase kind', sgTryTask: 'Task type',
      sgTryRun: 'Try', sgTryClass: 'Class', sgTrySource: 'Source', sgTryModel: 'Model', sgTryQuality: 'Quality',
      sgTryRaw: 'Raw JSON', sgClassCol: 'Class', sgClassReq: 'Req', sgClassIn: 'In', sgClassOut: 'Out', sgClassTok: 'Tokens',
      sgClassTotal: 'Total', sgClassEmpty: 'No billed traffic in this window.', sgSourceMix: 'Source mix',
      sgNoHintSamples: 'Recent previews', sgNoHintSamplesEmpty: 'No recent previews.',
      sgTierHigh: 'Official high (official-high)', sgTierMid: 'Official mid (official-mid)', sgTierLow: 'Official low (official-low)',
      sgPlanDesignNoLow: 'Plan and design cannot use the low band.',
      sgProtectedModel: 'auto and official quality bands cannot be renamed or removed while they are in use.',
      sgCatalogTitle: 'Catalog & floor', sgCatalogHint: 'Empty catalog lists auto, low, mid, and high. Clients that pin a band skip L1. Fee multipliers default to auto 1, low 0.5, mid 1, high 2.',
      sgWorkloadTitle: 'Workload routes', sgWorkloadHint: 'Each class picks an official band. Plan and design stay on high.',
      sgQualityFloor: 'Quality floor', sgQualityFloorNone: 'None',
      sgDefaultBadge: 'Default', sgSetDefault: 'Set default',
      sgDefaultSaved: 'Default group updated.',
      sgDeleteDefaultBlocked: 'Set another default group before deleting this one.',
      sgOfficialNoDelete: 'The MaClaw official group is system-generated and cannot be deleted.',
      sgClose: 'Close',
      sgSystemBadge: 'System', sgKindBadge: 'Kind',
      sgPipe_off: 'Rules', sgPipe_shadow: 'Shadow', sgPipe_canary: 'Canary', sgPipe_on: 'Live',
      sgGate_review_coverage: 'Review coverage', sgGate_accuracy: 'Accuracy', sgGate_recall: 'Recall',
      sgGate_two_windows: 'Two windows', sgGate_artifact: 'Artifact',
      sgHeadSt_unused: 'Live: rules only', sgHeadSt_unused_trained: 'Live: rules only (head ready)',
      sgHeadSt_training: 'Training', sgHeadSt_shadow: 'Shadow', sgHeadSt_canary: 'Canary',
      sgHeadSt_promoted: 'Live: head on', sgHeadSt_gates_failed: 'Gates failed', sgHeadSt_distributing: 'Distributing',
      sgHeadSt_rolled_back: 'Rolled back',
      sgHeadStHint_unused: 'Requests still follow rules. A trained head is not live until you promote it.',
      sgHeadPipeline: 'Live path',
      sgHeadUnused: 'The head is not live. Requests still follow rules. Train after gold samples exist.',
      sgHeadUnusedOfficial: 'The head is not live. Requests still follow rules. Train after gold samples exist.',
      sgHeadHasSamples: 'Samples are ready. Mark gold or train. Training does not put the head live.',
      sgHeadAdoptReady: 'A serving artifact is ready. Shadow it before canary or live.',
      sgHeadNeedShadow: 'Shadow before canary or live.',
      sgHeadNeedServing: 'Adopt a serving head before shadow.',
      sgHeadNeedDistribute: 'Finish serving distribute before changing the live path.',
      sgHeadDistributing: 'Distributing to peers.',
      sgTrainNeedData: 'Need samples or a previous head before training.',
      sgTrainNeedDataOfficial: 'Need L1 billed traffic or gold samples before training.',
      sgGoldClear: 'Clear gold', sgGoldPick: 'Mark gold', sgGoldInvalid: 'Unknown class.',
      sgSampleDelete: 'Delete', sgSampleDeleteConfirm: 'Delete this sample? This cannot be undone.',
      sgHeadSamplesEmpty: 'No samples on this page.',
      sgSampleRule: 'Rule', sgSampleGold: 'Gold', sgSampleHead: 'Head',
      sgHeadVersions: 'Head versions', sgHeadVersion: 'Version', sgHeadPrevious: 'Previous',
      sgHeadRole: 'Role', sgHeadTrainedAt: 'Trained', sgHeadSource: 'Source', sgHeadTau: 'Tau', sgHeadRetired: 'Retired',
      sgHeadTest: 'Score a prompt', sgHeadTestHint: 'Score against the serving head without changing live traffic.',
      sgHeadTestSlot: 'Slot', sgHeadTestRun: 'Score', sgHeadTestCompare: 'Compare',
      sgHeadEmbedderOff: 'Sync it above before scoring',
      sgHeadNeedText: 'Enter text to score.', sgHeadScoreGroup: 'Score group', sgHeadScoreGroupAuto: 'Auto (this head)',
      sgHeadTestGroup: 'Group', sgHeadIfLive: 'If live', sgHeadWouldRewrite: 'Would rewrite',
      sgHeadNoRewrite: 'No rewrite', sgHeadProtected: 'Protected',
      sgHeadAccuracy: 'Accuracy', sgHeadPlanRecall: 'Plan recall', sgHeadRuleAgreement: 'Rule agreement',
      sgHeadReviews: 'Reviews', sgHeadHuman: 'human', sgHeadGates: 'Gates', sgHeadAck: 'Peer ACK',
      sgHeadSamples: 'Review samples', sgHeadNoData: 'No head data yet.',
      sgHeadLocal: 'This node', sgDistributeStatus: 'Distribute',
      sgTrainerLocalTag: 'local', sgTrainerHint: 'The trainer node owns offline train. Others pull the serving artifact.',
      sgHeadTrainer: 'Trainer node', sgApplyTrainer: 'Apply', sgTrainerEmpty: 'This node',
      sgTrainThis: 'Train', sgTraining: 'Training...', sgRefreshHead: 'Refresh',
      sgDistribute: 'Distribute', sgRollBack: 'Roll back', sgPullOfficial: 'Pull official',
      sgConfirmLive: 'Switch the live path to the head? Rules stay first; the head may rewrite weak classes.',
      sgPromoteGo: 'Promote anyway', sgPromoteNeed: 'Type PROMOTE',
      sgPromptReason: 'Reason', sgPromptOverride: 'Override token',
      sgAck_acked: 'acked', sgAck_pending: 'pending',
      sgSrc_hint: 'Hint', sgSrc_workflow: 'Workflow', sgSrc_task_type: 'Task type',
      sgSrc_heuristic: 'Heuristic', sgSrc_fallback: 'Fallback', sgSrc_head: 'Head',
      sgClass_plan: 'Plan', sgClass_design: 'Design', sgClass_review: 'Review',
      sgClass_doc_write: 'Docs', sgClass_code: 'Code', sgClass_ops: 'Ops',
      sgClass_chat: 'Chat', sgClass_classify: 'Classify', sgClass_balanced: 'Balanced',
      sgFeat_reasoning: 'Reasoning', sgFeat_tools: 'Tools', sgFeat_document: 'Document',
      sgFeat_vision: 'Vision', sgFeat_audio: 'Audio', sgFeat_code: 'Code', sgFeat_search: 'Search',
      runtimeTitle: 'Embedding model',
      runtimeDesc: 'Classification head needs Gemma locally. HubCenter syncs the GGUF on start and copies it to ~/.maclaw/models.',
      runtimeRefresh: 'Refresh', runtimeTrigger: 'Sync',
      runtimeReady: 'Ready', runtimeDownloading: 'Downloading', runtimePartial: 'Partial',
      runtimeMissing: 'Missing', runtimeWarming: 'Warming',
      runtimeAlreadyRunning: 'A sync is already running.',
      runtimeDir: 'Cache', runtimeServing: 'Serving path',
      billingTitle: 'Vendor billing', billingHint: 'Optional time-of-use multipliers published to Hub.',
      billingTimezone: 'Timezone', billingMultiplier: 'Base multiplier',
      billingSchedule: 'Time windows', billingAddWindow: 'Add window', billingRemoveWindow: 'Remove',
      billingEveryday: 'Every day', billingWeekdays: 'Weekdays',
      billingStart: 'Start', billingEnd: 'End', billingWindowMultiplier: 'Multiplier',
      billingEmpty: 'No windows. The base multiplier applies all day.',
      billingCurrent: 'Now', billingDroppedWindows: 'Fix windows with empty or identical start/end times before saving.',
      billingOvernight: 'If start is later than end, the window wraps past midnight. The first matching window wins.',
      weekdaySun: 'Sun', weekdayMon: 'Mon', weekdayTue: 'Tue', weekdayWed: 'Wed', weekdayThu: 'Thu', weekdayFri: 'Fri', weekdaySat: 'Sat',
      serveTitle: 'Serve window', serveHint: 'Restrict the weekdays and hours when this provider may answer (evaluated in Beijing time). Requests outside every window skip to other supply; useful for time-limited free tiers.',
      serveAddWindow: 'Add window', serveRemoveWindow: 'Remove',
      serveEveryday: 'Every day', serveWeekdays: 'Weekdays', serveAllDay: 'All day',
      serveDroppedWindows: 'Fix windows with missing or identical start/end times, or invalid weekdays, before saving.',
      serveEmpty: 'No windows. The provider answers around the clock.',
      serveWindowBadge: 'Serve window', serveClosedNow: 'closed',
      weekdayMonFull: 'Mon', weekdayTueFull: 'Tue', weekdayWedFull: 'Wed', weekdayThuFull: 'Thu', weekdayFriFull: 'Fri', weekdaySatFull: 'Sat', weekdaySunFull: 'Sun'
    },
    zh: {
      llmTabTitle: 'LLM \u670d\u52a1', llmTabDesc: '\u7ba1\u7406 LLM \u670d\u52a1\u5546\u3001\u7b97\u529b\u4ee3\u7406\u5546\u548c\u6a21\u578b\u670d\u52a1\u7ec4\u3002',
      providersTitle: 'LLM \u670d\u52a1\u5546', providersDesc: '\u540e\u7aef LLM API \u7aef\u70b9\u914d\u7f6e\u3002',
      addProvider: '\u6dfb\u52a0\u670d\u52a1\u5546', editProvider: '\u7f16\u8f91', deleteProvider: '\u5220\u9664', noProviders: '\u672a\u914d\u7f6e\u670d\u52a1\u5546\u3002',
      deleteProviderBound: '\u8be5\u670d\u52a1\u5546\u5df2\u88ab\u4ee5\u4e0b\u670d\u52a1\u7ec4\u7ed1\u5b9a\uff1a', deleteProviderPruneHint: '\u5220\u9664\u540e\u5c06\u81ea\u52a8\u4ece\u8fd9\u4e9b\u670d\u52a1\u7ec4\u4e2d\u79fb\u9664\u8be5\u670d\u52a1\u5546\u3002',
      deleteProviderInUse: '\u5220\u9664\u88ab\u963b\u6b62\uff1a\u670d\u52a1\u5546\u4ecd\u7ed1\u5b9a\u4ee5\u4e0b\u670d\u52a1\u7ec4\uff1a',
      providerDialogTitleNew: '\u65b0\u5efa\u670d\u52a1\u5546', providerDialogTitleEdit: '\u7f16\u8f91\u670d\u52a1\u5546',
      fieldID: '\u670d\u52a1\u5546 ID', fieldName: '\u540d\u79f0', fieldURL: 'API \u5730\u5740', fieldKey: 'API \u5bc6\u94a5',
      fieldAuth: '\u8ba4\u8bc1\u65b9\u5f0f', authAPIKey: 'API \u5bc6\u94a5',
      authWorkBuddyChina: 'WorkBuddy \u56fd\u5185\u7248', authWorkBuddyGlobal: 'WorkBuddy \u56fd\u9645\u7248',
      workBuddyLogin: '\u767b\u5f55\u5e76\u5217\u51fa\u6a21\u578b', workBuddyWaiting: '\u6b63\u5728\u7b49\u5f85\u6d4f\u89c8\u5668\u5b8c\u6210\u767b\u5f55...',
      workBuddyReady: '\u8bf7\u52fe\u9009\u8981\u63a5\u5165\u7684\u6a21\u578b\u3002', workBuddyNeedLogin: '\u4fdd\u5b58\u524d\u8bf7\u5148\u5b8c\u6210 WorkBuddy \u767b\u5f55\u3002',
      workBuddyNeedModel: '\u8bf7\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a\u6a21\u578b\u3002', workBuddyFailed: 'WorkBuddy \u767b\u5f55\u5931\u8d25',
      workBuddyHint: '\u767b\u5f55\u540e\u52fe\u9009\u6a21\u578b\u5e76\u4fdd\u5b58\uff0c\u5373\u4ee5\u6b64\u8d26\u53f7\u63a5\u5165\u670d\u52a1\u5546\u3002\u4e4b\u540e\u5728\u6a21\u578b\u670d\u52a1\u7ec4\u4e2d\u66b4\u9732\u8fd9\u4e9b\u6a21\u578b\u3002',
      workBuddyOpen: '\u5982\u679c\u767b\u5f55\u9875\u6ca1\u6709\u6253\u5f00\uff0c\u8bf7\u4f7f\u7528\u6b64\u94fe\u63a5\u3002',
      workBuddyCatalogWarn: '\u5b9e\u65f6\u6a21\u578b\u76ee\u5f55\u6682\u4e0d\u53ef\u7528\uff0c\u5df2\u663e\u793a\u5185\u7f6e\u6a21\u578b\u5217\u8868\u3002',
      fieldProtocol: '\u534f\u8bae', fieldModels: '\u6a21\u578b\uff08\u9017\u53f7\u5206\u9694\uff09', fieldCapabilities: '\u80fd\u529b\u6807\u7b7e',
      fieldPriority: '\u4f18\u5148\u7ea7', fieldConcurrency: '\u6700\u5927\u5e76\u53d1', fieldTimeout: '\u8d85\u65f6\uff08\u79d2\uff09',
      fieldSequence: '\u5e8f\u5217', sequenceHint: '\u6570\u5b57\u8d8a\u5c0f\u8d8a\u5148\u8bd5\u30020 \u8868\u793a\u672a\u8bbe\u3002',
      accessScope: '\u63a5\u5165\u8303\u56f4', accessScopeAll: '\u5168\u90e8\u8282\u70b9', accessScopeSelected: '\u6307\u5b9a\u8282\u70b9',
      accessScopeHint: '\u9ed8\u8ba4\u5168\u90e8 HubCenter \u8282\u70b9\u53ef\u8c03\u7528\u8be5\u670d\u52a1\u5546\u3002\u6307\u5b9a\u8282\u70b9\u540e\uff0c\u4e0a\u6e38\u8bf7\u6c42\u4ece\u5141\u8bb8\u7684\u8282\u70b9\u53d1\u51fa\uff0c\u4ee5\u907f\u5f00\u6a21\u578b\u533a\u57df\u9650\u5236\u3002',
      accessScopeAvailable: '\u53ef\u7528\u8282\u70b9\uff1a', accessScopeAvailableEmpty: '\u5f53\u524d\u6ca1\u6709\u5217\u51fa\u96c6\u7fa4\u8282\u70b9\uff0c\u4ecd\u53ef\u624b\u5199\u8282\u70b9\u540d\u3002',
      accessScopeNodesInput: '\u6307\u5b9a\u8282\u70b9', accessScopeNodesPlaceholder: 'hc-1, hc-2, hc-3',
      accessScopeNeedNode: '\u8bf7\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a\u8282\u70b9\u3002', accessScopeOffline: '\u5df2\u4e0b\u7ebf', accessScopeSelf: '\u672c\u8282\u70b9', accessScopeUnreachable: '\u4e0d\u53ef\u8fbe',
      lbGroup: 'LB \u7ec4', pauseProvider: '\u6682\u505c', resumeProvider: '\u6062\u590d',
      providerArray: '\u670d\u52a1\u5546\u9635\u5217', providerArrayOwn: '\u72ec\u7acb\u9635\u5217', providerArrayJoin: '\u6240\u5c5e\u9635\u5217',
      providerArrayName: '\u9635\u5217\u540d\u79f0', providerArrayHint: '\u4e00\u4e2a\u9635\u5217\u5c31\u662f\u4e00\u4e2a\u903b\u8f91\u670d\u52a1\u5546\u3002\u6210\u5458\u5171\u4eab\u500d\u7387\u4e0e\u8d39\u7528\uff0c\u8bf7\u6c42\u8f6e\u8be2\u8bbf\u95ee\uff1b\u67d0\u4e2a\u6210\u5458\u8fd4\u56de 429 \u6216 5xx \u65f6\u81ea\u52a8\u8bbf\u95ee\u4e0b\u4e00\u4e2a\u3002',
      providerArrayUseShared: '\u5c06\u4f7f\u7528\u8be5\u9635\u5217\u5df2\u6709\u7684\u500d\u7387\u4e0e\u8d39\u7528\u3002',
      providerArrayAdd: '\u6dfb\u52a0\u670d\u52a1\u5546', providerArrayDelete: '\u5220\u9664\u9635\u5217', providerArrayShared: '\u5171\u4eab\u500d\u7387\u4e0e\u8d39\u7528',
      providerArrayRename: '\u91cd\u547d\u540d', providerArrayRenameTitle: '\u91cd\u547d\u540d\u9635\u5217',
      providerArrayEdit: '\u7f16\u8f91', providerArrayEditTitle: '\u7f16\u8f91\u9635\u5217', providerArrayCreateTitle: '\u65b0\u5efa\u9635\u5217',
      providerArrayID: '\u9635\u5217 ID', providerArrayNeedID: '\u8bf7\u586b\u5199\u9635\u5217 ID\u3002',
      providerArrayEmpty: '\u9635\u5217\u91cc\u8fd8\u6ca1\u6709\u670d\u52a1\u5546\u3002',
      providerArrayDragHint: '\u62d6\u5230\u5176\u5b83\u9635\u5217',
      providerArrayMoved: '\u5df2\u79fb\u5165\u8be5\u9635\u5217\uff0c\u5e76\u6539\u7528\u8be5\u9635\u5217\u7684\u8ba1\u8d39\u3002',
      providerArrayBusy: '\u8be5\u9635\u5217\u6b63\u5728\u66f4\u65b0\uff0c\u8bf7\u7a0d\u540e\u518d\u62d6\u3002',
      providerArrayTrafficSum: '\u5408\u8ba1', providerArraysTrafficSum: '\u5168\u90e8\u9635\u5217',
      providerArrayBillingOnEdit: 'Token \u8ba1\u8d39\u4e0e\u5382\u5546\u500d\u7387\u5728\u9635\u5217\u4e0a\u8bbe\u7f6e\u3002',
      providerArrayRenameNeedName: '\u8bf7\u586b\u5199\u9635\u5217\u540d\u79f0\u3002',
      providerArrayRenameTooLong: '\u9635\u5217\u540d\u79f0\u4e0d\u80fd\u8d85\u8fc7 80 \u4e2a\u5b57\u3002',
      providerArrayRemove: '\u4ece\u9635\u5217\u4e2d\u79fb\u9664\u8be5\u670d\u52a1\u5546\uff1f\u670d\u52a1\u7ec4\u4ecd\u4f1a\u4f7f\u7528\u8fd9\u4e2a\u9635\u5217\u3002',
      providerArrayRemoveProtected: '\u79fb\u9664\u8be5\u670d\u52a1\u5546\uff1f\u5e73\u53f0\u9635\u5217\u4f1a\u4fdd\u7559\u3002\u8be5\u9635\u5217\u91cc\u6ca1\u6709\u5269\u4f59\u670d\u52a1\u5546\u80fd\u63a5\u7684\u6a21\u578b\u4f1a\u4ece\u670d\u52a1\u7ec4\u8def\u7531\u91cc\u62ff\u6389\u3002',
      providerArrayExpand: '\u5c55\u5f00', providerArrayCollapse: '\u6298\u53e0',
      providerArrayDeleteConfirm: '\u5220\u9664\u8be5\u670d\u52a1\u5546\u9635\u5217\u53ca\u5176\u4e2d\u7684\u5168\u90e8\u670d\u52a1\u5546\uff1f',
      providerArrayDeleteInUse: '\u8be5\u670d\u52a1\u5546\u9635\u5217\u5df2\u88ab\u4ee5\u4e0b\u670d\u52a1\u7ec4\u4f7f\u7528\uff0c\u4e0d\u80fd\u5220\u9664\uff1a',
      providerArrayProtected: '\u8be5\u9635\u5217\u7531\u5e73\u53f0\u7ef4\u62a4\uff0c\u4e0d\u80fd\u5220\u9664\u3002',
      providerArrayRenameProtected: '\u8be5\u9635\u5217\u7531\u5e73\u53f0\u7ef4\u62a4\uff0c\u4e0d\u80fd\u6539\u540d\u3002',
      providerCanaryUntil: '\u91d1\u4e1d\u96c0\u65f6\u95f4\u81f3 {time}',
      sgRouteModelUnmatched: '\u4ee5\u4e0b\u8def\u7531\u5199\u6b7b\u7684\u4e0a\u6e38\u6a21\u578b\uff0c\u9635\u5217\u91cc\u6ca1\u6709\u6210\u5458\u63d0\u4f9b\uff1a',
      sgRouteModelUnmatchedAsk: '\u4ecd\u8981\u4fdd\u5b58\uff1f',
      sgMemberModelReferenced: '\u4ee5\u4e0b\u8def\u7531\u4ecd\u5728\u5f15\u7528\u8be5\u6210\u5458\u7684\u6a21\u578b\u540d\uff1a',
      adminAPIKeyTitle: '\u81ea\u52a8\u5316 API \u5bc6\u94a5',
      adminAPIKeyDesc: '\u7a0b\u5e8f\u7528\u8fd9\u628a\u5bc6\u94a5\u7ba1\u7406\u670d\u52a1\u5546\u9635\u5217\uff1a\u5217\u51fa\u6210\u5458\u3001\u6279\u91cf\u6dfb\u52a0\u6216\u8bd5\u8dd1\u3001\u505c\u7528\u6216\u5220\u9664\u3001\u67e5\u770b\u5065\u5eb7\u7528\u91cf\u3001\u6309\u5df2\u4fdd\u5b58\u914d\u7f6e\u6d4b\u8bd5\u5355\u4e2a\u6210\u5458\u5e76\u8fd4\u56de\u5f53\u524d\u72b6\u6001\uff0c\u4ee5\u53ca\u5217\u51fa\u53ef\u7528\u8282\u70b9\u540d\u3002\u4e0a\u6e38\u5bc6\u94a5\u53ea\u8fd4\u56de\u662f\u5426\u5df2\u914d\u7f6e\u6216\u672b 4 \u4f4d\u3002\u5df2\u4fdd\u5b58\u7684\u5bc6\u94a5\u4f1a\u5217\u5728\u8fd9\u91cc\uff0c\u9ed8\u8ba4\u9690\u85cf\uff0c\u53ef\u4ee5\u663e\u793a\u3001\u590d\u5236\uff0c\u5220\u9664\u5373\u64a4\u9500\u6388\u6743\u3002',
      adminAPIKeyName: '\u5bc6\u94a5\u540d\u79f0', adminAPIKeyCreate: '\u521b\u5efa\u5bc6\u94a5', adminAPIKeyDelete: '\u5220\u9664',
      adminAPIKeyEmpty: '\u8fd8\u6ca1\u6709\u81ea\u52a8\u5316\u5bc6\u94a5\u3002', adminAPIKeyNeedName: '\u8bf7\u586b\u5199\u5bc6\u94a5\u540d\u79f0\u3002',
      adminAPIKeyCreated: '\u5df2\u4fdd\u5b58\u3002\u9ed8\u8ba4\u9690\u85cf\uff0c\u53ef\u5728\u5217\u8868\u4e2d\u590d\u5236\u6216\u5220\u9664\uff0c\u9700\u8981\u65f6\u518d\u663e\u793a\u3002',
      adminAPIKeyCopy: '\u590d\u5236', adminAPIKeyCopied: '\u5df2\u590d\u5236',
      adminAPIKeyShow: '\u663e\u793a', adminAPIKeyHide: '\u9690\u85cf',
      adminAPIKeyLegacy: '\u5168\u6587\u672a\u4fdd\u5b58\u3002\u5220\u9664\u5373\u53ef\u64a4\u9500\u6388\u6743\u3002',
      adminAPIKeyAllScopes: '\u5168\u90e8\u6743\u9650', adminAPIKeyExpired: '\u5df2\u8fc7\u671f',
      adminAPIKeyLastUsed: '\u4e0a\u6b21\u4f7f\u7528',
      adminAPIKeyDeleteConfirm: '\u5220\u9664\u8fd9\u628a\u81ea\u52a8\u5316\u5bc6\u94a5\uff1f\u6b63\u5728\u4f7f\u7528\u5b83\u7684\u7a0b\u5e8f\u5c06\u4e0d\u80fd\u518d\u7ba1\u7406\u670d\u52a1\u5546\u9635\u5217\u3002',
      adminAPIKeyDoc: '\u7ed9 AI \u5de5\u5177\u7684\u63a5\u53e3\u8bf4\u660e', adminAPIKeyOpenAPI: 'OpenAPI JSON',
      adminAPIKeyCreatedAt: '\u521b\u5efa\u4e8e',
      trafficDay: '\u4eca\u65e5', trafficWeek: '\u672c\u5468', trafficMonth: '\u672c\u6708', trafficLoading: '\u52a0\u8f7d\u4e2d',
      trafficIn: '\u5165', trafficOut: '\u51fa', trafficTotal: '\u603b',
      providerProbeModels: '\u63a2\u6d4b', providerProbing: '\u6b63\u5728\u63a2\u6d4b\u6a21\u578b...', providerProbeEmpty: '\u672a\u8fd4\u56de\u6a21\u578b\u5217\u8868\u3002',
      providerProbeFailed: '\u63a2\u6d4b\u5931\u8d25', providerCapabilityPreset: '\u9884\u7f6e\u80fd\u529b',
      agentsTitle: '\u7b97\u529b\u4ee3\u7406\u5546', agentsDesc: '\u7528\u4e8e\u7ed3\u7b97\u548c\u5ba2\u6237\u7aef\u5c55\u793a\u7684\u4e0a\u6e38\u7b97\u529b\u4ee3\u7406\u3002',
      addAgent: '\u6dfb\u52a0\u4ee3\u7406\u5546', editAgent: '\u7f16\u8f91', deleteAgent: '\u5220\u9664', noAgents: '\u672a\u914d\u7f6e\u4ee3\u7406\u5546\u3002',
      agentDialogTitleNew: '\u65b0\u5efa\u7b97\u529b\u4ee3\u7406\u5546', agentDialogTitleEdit: '\u7f16\u8f91\u7b97\u529b\u4ee3\u7406\u5546',
      fieldAgentID: '\u4ee3\u7406\u5546 ID', fieldAgentName: '\u4ee3\u7406\u5546\u540d\u79f0', fieldAgentContact: '\u8054\u7cfb\u65b9\u5f0f', fieldAgentSettlement: '\u7ed3\u7b97\u5907\u6ce8',
      fieldAgentDesc: '\u63cf\u8ff0', fieldGroupAgent: '\u7b97\u529b\u4ee3\u7406\u5546', sgAgentRequired: '\u8bf7\u9009\u62e9\u7b97\u529b\u4ee3\u7406\u5546\u3002',
      groupsTitle: '\u6a21\u578b\u670d\u52a1\u7ec4', groupsDesc: '\u5c06\u6a21\u578b\u8def\u7531\u5230\u670d\u52a1\u5546\u3002',
      addGroup: '\u6dfb\u52a0\u670d\u52a1\u7ec4', editGroup: '\u7f16\u8f91', deleteGroup: '\u5220\u9664', noGroups: '\u672a\u914d\u7f6e\u670d\u52a1\u7ec4\u3002',
      groupDialogTitleNew: '\u65b0\u5efa\u670d\u52a1\u7ec4', groupDialogTitleEdit: '\u7f16\u8f91\u670d\u52a1\u7ec4',
      fieldGroupID: '\u7ec4 ID', fieldGroupName: '\u7ec4\u540d\u79f0', fieldGroupDesc: '\u63cf\u8ff0',
      fieldGroupModels: '\u6a21\u578b\u914d\u7f6e (JSON)', modelNamePlaceholder: '\u5982 gpt-4, claude-3.5',
      fieldGroupKind: '\u7c7b\u578b', sgKindDynamic: '\u52a8\u6001', sgKindStatic: '\u9759\u6001',
      sgRouteHint: '\u66b4\u9732\u6a21\u578b\u522b\u540d\uff0c\u6309\u670d\u52a1\u5546\u4f18\u5148\u7ea7\u5b9e\u73b0\u6545\u969c\u8f6c\u79fb',
      sgArrayDragHint: '\u62d6\u52a8\u9635\u5217\uff0c\u8c03\u6574\u5b83\u5728\u672c\u7ec4\u5185\u7684\u4f4d\u7f6e',
      sgArrayOrderHint: '\u80fd\u529b\u3001\u5206\u8fa8\u7387\u6863\u3001\u500d\u7387\u548c\u4f18\u5148\u7ea7\u76f8\u540c\u65f6\uff0c\u6309\u5217\u8868\u987a\u5e8f\u3002',
      sgMoveEarlier: '\u5411\u524d\u79fb\u4e00\u4f4d', sgMoveLater: '\u5411\u540e\u79fb\u4e00\u4f4d',
      sgRemoveRoute: '\u5220\u9664', sgExposedModel: '\u66b4\u9732\u6a21\u578b\u540d', sgNoProviders: '\u672a\u5206\u914d\u670d\u52a1\u5546\u3002\u8bf7\u5728\u4e0a\u65b9\u6dfb\u52a0\u3002',
      sgAccessPolicy: '\u8bbf\u95ee\u7b56\u7565', sgPolicyFreeHint: '\u65e0\u9700\u6388\u6743', sgPolicyGrantHint: '\u9700\u8981\u5361/\u6388\u6743',
      sgRoutes: '\u670d\u52a1\u5546\u8def\u7531', sgAddRoute: '+ \u6dfb\u52a0\u8def\u7531',
      sgProviderAlreadyAdded: '\u8be5\u670d\u52a1\u5546\u5df2\u6dfb\u52a0\u3002',
      sgProviderConfigTitle: '\u670d\u52a1\u5546\u914d\u7f6e', sgCapabilityTags: '\u80fd\u529b\u6807\u7b7e',
      sgCapabilityHint: '\u8fd9\u6761\u4e0a\u6e38\u6a21\u578b\u7684\u80fd\u529b\u3002\u8bf7\u6c42\u8981 tools / vision \u7b49\u65f6\u4f1a\u6309\u6807\u7b7e\u8def\u7531\u3002',
      sgExtraTags: '\u989d\u5916\u6807\u7b7e\uff08\u81ea\u5b9a\u4e49\uff09', sgPriority: '\u4f18\u5148\u7ea7',
      sgResolutionTier: '\u89e3\u6790\u5c42\u7ea7', sgCreditMultiplier: '\u989d\u5ea6\u500d\u7387', sgFeeMultiplier: '\u8d39\u7528\u500d\u7387',
      sgBillingMode: '\u8ba1\u8d39\u6a21\u5f0f', sgBillingModeHint: 'paid \u6263\u8d39\uff0cfree \u514d\u8d39\uff0c\u7a7a\u4e3a\u517c\u5bb9',
      sgBillingModePaid: '\u6536\u8d39', sgBillingModeFree: '\u514d\u8d39', sgBillingModeLegacy: '\u517c\u5bb9\uff08\u7a7a\uff09',
      tokenPricingTitle: 'Token \u8ba1\u8d39\uff08\u6bcf\u4e07 Token\uff09', tokenPricingHint: 'Credits \u5b57\u6bb5\u53c2\u4e0e\u6263\u8d39\uff1bRMB \u4ec5\u5c55\u793a\u3002',
      fieldInputCredits: '\u8f93\u5165 Credits / \u4e07', fieldOutputCredits: '\u8f93\u51fa Credits / \u4e07',
      fieldCacheReadCredits: '\u7f13\u5b58\u8bfb\u53d6 Credits / \u4e07', fieldCacheWriteCredits: '\u7f13\u5b58\u5199\u5165 Credits / \u4e07',
      fieldInputRMB: '\u8f93\u5165 RMB / \u4e07\uff08\u53c2\u8003\uff09', fieldOutputRMB: '\u8f93\u51fa RMB / \u4e07\uff08\u53c2\u8003\uff09',
      fieldCacheReadRMB: '\u7f13\u5b58\u8bfb\u53d6 RMB / \u4e07', fieldCacheWriteRMB: '\u7f13\u5b58\u5199\u5165 RMB / \u4e07',
      fieldMinimumCredits: '\u5355\u6b21\u6700\u4f4e\u6d88\u8d39 Credits', fieldPricingTimezone: '\u8ba1\u8d39\u65f6\u533a', fieldPricingVersion: '\u8ba1\u8d39\u7248\u672c',
      providerPriceSummary: '\u57fa\u7840\u5355\u4ef7', providerSetPrice: '\u8bbe\u7f6e\u5355\u4ef7', pricingSchedule: '\u5206\u65f6\u5355\u4ef7', pricingAddWindow: '\u6dfb\u52a0\u5355\u4ef7\u65f6\u6bb5', pricingRemoveWindow: '\u5220\u9664',
      pricingWindowHint: '\u547d\u4e2d\u65f6\u6bb5\u4ec5\u8986\u76d6\u5df2\u586b\u5355\u4ef7\uff0c\u7559\u7a7a\u7684\u65b9\u5411\u7ee7\u7eed\u4f7f\u7528\u57fa\u7840\u5355\u4ef7\uff1b\u9996\u4e2a\u547d\u4e2d\u7684\u65f6\u6bb5\u751f\u6548\u3002',
      pricingDroppedWindows: '\u8bf7\u5148\u4fee\u6b63\u4e0d\u5b8c\u6574\u7684\u5206\u65f6\u5355\u4ef7\u540e\u518d\u4fdd\u5b58\u3002',
      billingInvalid: '\u8bf7\u4fee\u6b63\u65e0\u6548\u7684\u8ba1\u8d39\u6570\u503c\u540e\u518d\u4fdd\u5b58\u3002',
      billingPaidNeedsCredits: '\u6536\u8d39\u6a21\u5f0f\u9700\u81f3\u5c11\u4e00\u9879 Credits \u6b63\u6570\uff08\u8f93\u5165/\u8f93\u51fa/\u6700\u4f4e\u6d88\u8d39\uff09\u3002',
      sgIDNameRequired: 'ID \u548c\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a\u3002', sgRouteNeedsProvider: '\u6bcf\u4e2a\u8def\u7531\u81f3\u5c11\u9700\u8981\u4e00\u4e2a\u670d\u52a1\u5546\u3002',
      chooseProvider: '\u9009\u62e9\u670d\u52a1\u5546\u9635\u5217',
      fieldHubID: 'Hub ID', fieldTenantID: '\u79df\u6237 ID',
      fieldHubRequired: '\u8bf7\u9009\u62e9 Hub', fieldTenantRequired: '\u8bf7\u9009\u62e9\u79df\u6237', noHubs: '\u6682\u65e0\u5df2\u6ce8\u518c Hub\u3002', defaultTenant: '\u9ed8\u8ba4\u79df\u6237',
      save: '\u4fdd\u5b58', cancel: '\u53d6\u6d88', confirm: '\u786e\u8ba4', delete: '\u5220\u9664',
      saved: '\u4fdd\u5b58\u6210\u529f\u3002', deleted: '\u5df2\u5220\u9664\u3002', error: '\u9519\u8bef', sgFailed: '\u5931\u8d25',
      testProvider: '\u6d4b\u8bd5\u72b6\u6001', providerTesting: '\u6d4b\u8bd5\u4e2d...', providerTestOK: '\u53ef\u7528', providerTestFailed: '\u5f02\u5e38',
      providerTestLatency: '\u8017\u65f6', providerTestModels: '\u6a21\u578b',
      monitorSaved: '\u76d1\u63a7\u8bbe\u7f6e\u5df2\u4fdd\u5b58\u3002', monitorInvalidInterval: '\u8bf7\u8f93\u5165\u4e0d\u5c0f\u4e8e 1 \u5c0f\u65f6\u7684\u6574\u6570\u95f4\u9694\u3002',
      monitorLoadRetry: '\u52a0\u8f7d\u5931\u8d25\uff0c\u8bf7\u4f7f\u7528\u4e0a\u65b9\u5237\u65b0\u6309\u94ae\u91cd\u8bd5\u3002',
      status: '\u72b6\u6001', credits: '\u989d\u5ea6', expires: '\u6709\u6548\u671f', active: '\u6d3b\u8dc3',
      sgClassTraffic: '\u4e0b\u6e38\u6d41\u91cf',
      sgClassTrafficHint: '\u8fd9\u7ec4\u6263\u8d39\u6210\u529f\u7684\u8bf7\u6c42\uff0c\u6309\u4efb\u52a1\u5206\u7c7b\u6c47\u603b\u3002\u8bad\u7ec3\u5728\u300c\u5206\u7c7b\u5934\u300d\u9875\u3002',
      sgClassTrafficOpen: '\u6d41\u91cf',
      sgTryRules: '\u8bd5\u8dd1\u89c4\u5219', sgTryPlaceholder: '\u5199\u4e00\u4efd\u5546\u4e1a\u8ba1\u5212',
      sgTryWorkflow: '\u5de5\u4f5c\u6d41\u7c7b\u578b', sgTryPhase: '\u9636\u6bb5\u7c7b\u578b', sgTryTask: '\u4efb\u52a1\u7c7b\u578b',
      sgTryRun: '\u8bd5\u8dd1', sgTryClass: '\u5206\u7c7b', sgTrySource: '\u6765\u6e90', sgTryModel: '\u6a21\u578b', sgTryQuality: '\u8d28\u91cf',
      sgTryRaw: '\u539f\u59cb JSON', sgClassCol: '\u5206\u7c7b', sgClassReq: '\u8bf7\u6c42', sgClassIn: '\u8f93\u5165', sgClassOut: '\u8f93\u51fa', sgClassTok: 'Tokens',
      sgClassTotal: '\u5408\u8ba1', sgClassEmpty: '\u8fd9\u4e2a\u7a97\u53e3\u6ca1\u6709\u6263\u8d39\u6d41\u91cf\u3002', sgSourceMix: '\u6765\u6e90\u6df7\u5408',
      sgNoHintSamples: '\u8fd1\u671f\u9884\u89c8', sgNoHintSamplesEmpty: '\u6682\u65e0\u9884\u89c8\u3002',
      sgTierHigh: '\u5b98\u65b9\u9ad8\u6863\uff08official-high\uff09', sgTierMid: '\u5b98\u65b9\u4e2d\u6863\uff08official-mid\uff09', sgTierLow: '\u5b98\u65b9\u4f4e\u6863\uff08official-low\uff09',
      sgPlanDesignNoLow: '\u89c4\u5212\u548c\u8bbe\u8ba1\u4e0d\u80fd\u8d70\u4f4e\u6863\u3002',
      sgProtectedModel: '\u52a8\u6001\u7ec4\u91cc\uff0cauto \u548c\u6b63\u5728\u4f7f\u7528\u7684\u5b98\u65b9\u6863\u4e0d\u80fd\u6539\u540d\u6216\u5220\u6389\u3002',
      sgCatalogTitle: '\u76ee\u5f55\u4e0e\u8d28\u91cf\u4e0b\u9650', sgCatalogHint: '\u76ee\u5f55\u4e3a\u7a7a\u65f6\u5217\u51fa auto\u3001low\u3001mid\u3001high\u3002\u5ba2\u6237\u7aef\u76f4\u63a5\u6307\u5b9a\u67d0\u4e00\u6863\u4f1a\u8df3\u8fc7 L1\u3002\u8d39\u7528\u500d\u7387\u9ed8\u8ba4 auto 1\u3001low 0.5\u3001mid 1\u3001high 2\u3002',
      sgWorkloadTitle: '\u5de5\u4f5c\u8d1f\u8377\u8def\u7531', sgWorkloadHint: '\u6bcf\u4e2a\u5206\u7c7b\u9009\u4e00\u4e2a\u5b98\u65b9\u6863\u3002\u89c4\u5212\u548c\u8bbe\u8ba1\u8d70\u9ad8\u6863\u3002',
      sgQualityFloor: '\u8d28\u91cf\u4e0b\u9650', sgQualityFloorNone: '\u65e0',
      sgDefaultBadge: '\u9ed8\u8ba4\u7ec4', sgSetDefault: '\u8bbe\u4e3a\u9ed8\u8ba4',
      sgDefaultSaved: '\u5df2\u66f4\u65b0\u9ed8\u8ba4\u7ec4\u3002',
      sgDeleteDefaultBlocked: '\u5148\u628a\u9ed8\u8ba4\u6539\u5230\u522b\u7684\u7ec4\uff0c\u518d\u5220\u8fd9\u4e2a\u7ec4\u3002',
      sgOfficialNoDelete: 'MaClaw \u5b98\u65b9\u7ec4\u7531\u7cfb\u7edf\u751f\u6210\uff0c\u53ea\u80fd\u7f16\u8f91\uff0c\u4e0d\u80fd\u5220\u9664\u3002',
      sgClose: '\u5173\u95ed',
      sgSystemBadge: '\u7cfb\u7edf', sgKindBadge: '\u7c7b\u578b',
      sgPipe_off: '\u89c4\u5219', sgPipe_shadow: '\u5f71\u5b50', sgPipe_canary: '\u91d1\u4e1d\u96c0', sgPipe_on: '\u7ebf\u4e0a',
      sgGate_review_coverage: '\u590d\u6838\u8986\u76d6', sgGate_accuracy: '\u51c6\u786e\u7387', sgGate_recall: '\u53ec\u56de',
      sgGate_two_windows: '\u53cc\u7a97', sgGate_artifact: '\u4ea7\u7269',
      sgHeadSt_unused: '\u7ebf\u4e0a\uff1a\u4ec5\u89c4\u5219', sgHeadSt_unused_trained: '\u7ebf\u4e0a\uff1a\u4ec5\u89c4\u5219\uff08\u5934\u5df2\u5c31\u7eea\uff09',
      sgHeadSt_training: '\u8bad\u7ec3\u4e2d', sgHeadSt_shadow: '\u5f71\u5b50', sgHeadSt_canary: '\u91d1\u4e1d\u96c0',
      sgHeadSt_promoted: '\u7ebf\u4e0a\uff1a\u5934\u5df2\u5f00', sgHeadSt_gates_failed: '\u95e8\u7981\u672a\u8fc7', sgHeadSt_distributing: '\u5206\u53d1\u4e2d',
      sgHeadSt_rolled_back: '\u5df2\u56de\u6eda',
      sgHeadStHint_unused: '\u8bf7\u6c42\u4ecd\u8d70\u89c4\u5219\u3002\u8bad\u597d\u7684\u5934\u8981\u8f6c\u6b63\u540e\u624d\u4f1a\u4e0a\u7ebf\u3002',
      sgHeadPipeline: '\u7ebf\u4e0a\u5206\u6d41',
      sgHeadUnused: '\u5934\u8fd8\u6ca1\u4e0a\u7ebf\uff0c\u8bf7\u6c42\u4ecd\u8d70\u89c4\u5219\u3002\u6709\u91d1\u6807\u540e\u518d\u8bad\u3002',
      sgHeadUnusedOfficial: '\u5934\u8fd8\u6ca1\u4e0a\u7ebf\uff0c\u8bf7\u6c42\u4ecd\u8d70\u89c4\u5219\u3002\u6709\u91d1\u6807\u540e\u518d\u8bad\u3002',
      sgHeadHasSamples: '\u5df2\u6709\u6837\u672c\u3002\u5148\u6807\u91d1\u6216\u8bad\u7ec3\uff0c\u8bad\u7ec3\u4e0d\u4f1a\u628a\u5934\u63a8\u4e0a\u7ebf\u3002',
      sgHeadAdoptReady: '\u670d\u52a1\u4ea7\u7269\u5df2\u5c31\u7eea\u3002\u5148\u5f71\u5b50\u518d\u91d1\u4e1d\u96c0/\u4e0a\u7ebf\u3002',
      sgHeadNeedShadow: '\u5148\u5f71\u5b50\uff0c\u518d\u91d1\u4e1d\u96c0\u6216\u4e0a\u7ebf\u3002',
      sgHeadNeedServing: '\u5148\u91c7\u7528\u670d\u52a1\u5934\uff0c\u518d\u5f71\u5b50\u3002',
      sgHeadNeedDistribute: '\u5148\u5b8c\u6210\u5206\u53d1\uff0c\u518d\u6539\u7ebf\u4e0a\u5206\u6d41\u3002',
      sgHeadDistributing: '\u6b63\u5728\u5206\u53d1\u5230\u540c\u8282\u70b9\u3002',
      sgTrainNeedData: '\u8fd8\u6ca1\u6837\u672c\u6216\u65e7\u5934\uff0c\u4e0d\u80fd\u8bad\u3002',
      sgTrainNeedDataOfficial: '\u5148\u6709 L1 \u6210\u4ea4\u6d41\u91cf\u6216\u91d1\u6807\uff0c\u518d\u8bad\u3002',
      sgGoldClear: '\u6e05\u9664\u91d1\u6807', sgGoldPick: '\u6807\u91d1', sgGoldInvalid: '\u4e0d\u8bc6\u522b\u7684\u5206\u7c7b\u3002',
      sgSampleDelete: '\u5220\u9664', sgSampleDeleteConfirm: '\u5220\u9664\u8fd9\u6761\u6837\u672c\uff1f\u5220\u9664\u540e\u65e0\u6cd5\u6062\u590d\u3002',
      sgHeadSamplesEmpty: '\u8fd9\u4e00\u9875\u6ca1\u6709\u6837\u672c\u3002',
      sgSampleRule: '\u89c4\u5219', sgSampleGold: '\u91d1\u6807', sgSampleHead: '\u5934',
      sgHeadVersions: '\u5934\u7248\u672c', sgHeadVersion: '\u7248\u672c', sgHeadPrevious: '\u4e0a\u4e00\u4e2a',
      sgHeadRole: '\u89d2\u8272', sgHeadTrainedAt: '\u8bad\u7ec3\u65f6\u95f4', sgHeadSource: '\u6765\u6e90', sgHeadTau: 'Tau', sgHeadRetired: '\u4e0b\u7ebf',
      sgHeadTest: '\u6253\u5206', sgHeadTestHint: '\u5bf9\u670d\u52a1\u5934\u6253\u5206\uff0c\u4e0d\u6539\u7ebf\u4e0a\u6d41\u91cf\u3002',
      sgHeadTestSlot: '\u69fd\u4f4d', sgHeadTestRun: '\u6253\u5206', sgHeadTestCompare: '\u5bf9\u6bd4',
      sgHeadEmbedderOff: '\u5148\u5728\u4e0a\u65b9\u540c\u6b65\uff0c\u518d\u6253\u5206',
      sgHeadNeedText: '\u5148\u8f93\u5165\u8981\u6253\u5206\u7684\u6587\u672c\u3002', sgHeadScoreGroup: '\u6253\u5206\u7ec4', sgHeadScoreGroupAuto: '\u81ea\u52a8\uff08\u672c\u5934\uff09',
      sgHeadTestGroup: '\u670d\u52a1\u7ec4', sgHeadIfLive: '\u82e5\u4e0a\u7ebf', sgHeadWouldRewrite: '\u4f1a\u6539\u5199',
      sgHeadNoRewrite: '\u4e0d\u6539\u5199', sgHeadProtected: '\u53d7\u4fdd\u62a4',
      sgHeadAccuracy: '\u51c6\u786e\u7387', sgHeadPlanRecall: '\u89c4\u5212\u53ec\u56de', sgHeadRuleAgreement: '\u4e0e\u89c4\u5219\u4e00\u81f4',
      sgHeadReviews: '\u590d\u6838', sgHeadHuman: '\u4eba\u5de5', sgHeadGates: '\u95e8\u7981', sgHeadAck: '\u8282\u70b9 ACK',
      sgHeadSamples: '\u5f85\u590d\u6838\u6837\u672c', sgHeadNoData: '\u8fd8\u6ca1\u6709\u5934\u6570\u636e\u3002',
      sgHeadLocal: '\u672c\u673a', sgDistributeStatus: '\u5206\u53d1',
      sgTrainerLocalTag: '\u672c\u673a', sgTrainerHint: '\u8bad\u7ec3\u8282\u70b9\u8d1f\u8d23\u79bb\u7ebf\u8bad\u7ec3\uff0c\u5176\u4ed6\u8282\u70b9\u62c9\u670d\u52a1\u4ea7\u7269\u3002',
      sgHeadTrainer: '\u8bad\u7ec3\u8282\u70b9', sgApplyTrainer: '\u5e94\u7528', sgTrainerEmpty: '\u672c\u673a',
      sgTrainThis: '\u8bad\u7ec3', sgTraining: '\u8bad\u7ec3\u4e2d...', sgRefreshHead: '\u5237\u65b0',
      sgDistribute: '\u5206\u53d1', sgRollBack: '\u56de\u6eda', sgPullOfficial: '\u62c9\u5b98\u65b9',
      sgConfirmLive: '\u628a\u7ebf\u4e0a\u5206\u6d41\u5207\u5230\u5206\u7c7b\u5934\uff1f\u89c4\u5219\u4ecd\u5148\u8dd1\uff0c\u5f31\u7c7b\u624d\u53ef\u80fd\u88ab\u6539\u5199\u3002',
      sgPromoteGo: '\u4ecd\u8981\u8f6c\u6b63', sgPromoteNeed: '\u8bf7\u8f93\u5165 PROMOTE',
      sgPromptReason: '\u539f\u56e0', sgPromptOverride: '\u8986\u76d6\u53e3\u4ee4',
      sgAck_acked: '\u5df2\u786e\u8ba4', sgAck_pending: '\u5f85\u786e\u8ba4',
      sgSrc_hint: '\u63d0\u793a', sgSrc_workflow: '\u5de5\u4f5c\u6d41', sgSrc_task_type: '\u4efb\u52a1',
      sgSrc_heuristic: '\u542f\u53d1', sgSrc_fallback: '\u56de\u9000', sgSrc_head: '\u5934',
      sgClass_plan: '\u89c4\u5212', sgClass_design: '\u8bbe\u8ba1', sgClass_review: '\u590d\u76d8',
      sgClass_doc_write: '\u6587\u6863', sgClass_code: '\u4ee3\u7801', sgClass_ops: '\u8fd0\u7ef4',
      sgClass_chat: '\u95f2\u804a', sgClass_classify: '\u5206\u7c7b', sgClass_balanced: '\u5747\u8861',
      sgFeat_reasoning: '\u63a8\u7406', sgFeat_tools: '\u5de5\u5177', sgFeat_document: '\u6587\u6863',
      sgFeat_vision: '\u89c6\u89c9', sgFeat_audio: '\u97f3\u9891', sgFeat_code: '\u4ee3\u7801', sgFeat_search: '\u641c\u7d22',
      runtimeTitle: 'Embedding \u6a21\u578b',
      runtimeDesc: '\u5206\u7c7b\u5934\u9700\u8981\u672c\u5730 Gemma\u3002HubCenter \u542f\u52a8\u65f6\u4f1a\u540c\u6b65 GGUF \u5e76\u590d\u5236\u5230 ~/.maclaw/models\u3002',
      runtimeRefresh: '\u5237\u65b0', runtimeTrigger: '\u540c\u6b65',
      runtimeReady: '\u5c31\u7eea', runtimeDownloading: '\u4e0b\u8f7d\u4e2d', runtimePartial: '\u90e8\u5206',
      runtimeMissing: '\u7f3a\u5931', runtimeWarming: '\u52a0\u70ed\u4e2d',
      runtimeAlreadyRunning: '\u540c\u6b65\u5df2\u5728\u8fdb\u884c\u3002',
      runtimeDir: '\u7f13\u5b58', runtimeServing: '\u670d\u52a1\u8def\u5f84',
      billingTitle: '\u5382\u5546\u8ba1\u8d39', billingHint: '\u53ef\u9009\u7684\u5206\u65f6\u6bb5\u500d\u7387\uff0c\u4f1a\u53d1\u5e03\u7ed9 Hub\u3002',
      billingTimezone: '\u65f6\u533a', billingMultiplier: '\u57fa\u7840\u500d\u7387',
      billingSchedule: '\u5206\u65f6\u65f6\u6bb5', billingAddWindow: '\u6dfb\u52a0\u65f6\u6bb5', billingRemoveWindow: '\u5220\u9664',
      billingEveryday: '\u6bcf\u5929', billingWeekdays: '\u5de5\u4f5c\u65e5',
      billingStart: '\u5f00\u59cb', billingEnd: '\u7ed3\u675f', billingWindowMultiplier: '\u500d\u7387',
      billingEmpty: '\u6682\u65e0\u5206\u65f6\u65f6\u6bb5\uff0c\u5168\u5929\u4f7f\u7528\u57fa\u7840\u500d\u7387\u3002',
      billingCurrent: '\u5f53\u524d', billingDroppedWindows: '\u5148\u4fee\u597d\u5f00\u59cb\u548c\u7ed3\u675f\u76f8\u540c\u6216\u4e3a\u7a7a\u7684\u65f6\u6bb5\uff0c\u518d\u4fdd\u5b58\u3002',
      billingOvernight: '\u5f00\u59cb\u665a\u4e8e\u7ed3\u675f\u65f6\uff0c\u65f6\u6bb5\u4f1a\u8de8\u8fc7\u5348\u591c\u3002\u5148\u5339\u914d\u5230\u7684\u65f6\u6bb5\u751f\u6548\u3002',
      weekdaySun: '\u65e5', weekdayMon: '\u4e00', weekdayTue: '\u4e8c', weekdayWed: '\u4e09', weekdayThu: '\u56db', weekdayFri: '\u4e94', weekdaySat: '\u516d',
      serveTitle: '\u670d\u52a1\u65f6\u6bb5', serveHint: '\u9650\u5236\u8be5\u670d\u52a1\u5546\u53ef\u4ee5\u63a5\u5355\u7684\u661f\u671f\u4e0e\u65f6\u6bb5\uff08\u6309\u5317\u4eac\u65f6\u95f4\u5224\u5b9a\uff09\u3002\u65f6\u6bb5\u5916\u7684\u8bf7\u6c42\u81ea\u52a8\u6539\u7528\u5176\u5b83\u4f9b\u7ed9\uff0c\u9002\u7528\u4e8e\u9650\u65f6\u514d\u8d39\u7b49\u573a\u666f\u3002',
      serveAddWindow: '\u6dfb\u52a0\u65f6\u6bb5', serveRemoveWindow: '\u5220\u9664',
      serveEveryday: '\u6bcf\u5929', serveWeekdays: '\u5de5\u4f5c\u65e5', serveAllDay: '\u5168\u5929',
      serveDroppedWindows: '\u5148\u4fee\u597d\u5f00\u59cb\u7ed3\u675f\u4e3a\u7a7a\u3001\u76f8\u540c\u6216\u661f\u671f\u65e0\u6548\u7684\u65f6\u6bb5\uff0c\u518d\u4fdd\u5b58\u3002',
      serveEmpty: '\u6682\u65e0\u9650\u5236\uff0c\u5168\u5929\u53ef\u63a5\u5355\u3002',
      serveWindowBadge: '\u670d\u52a1\u65f6\u6bb5', serveClosedNow: '\u4e0d\u53ef\u63a5\u5355',
      weekdayMonFull: '\u5468\u4e00', weekdayTueFull: '\u5468\u4e8c', weekdayWedFull: '\u5468\u4e09', weekdayThuFull: '\u5468\u56db', weekdayFriFull: '\u5468\u4e94', weekdaySatFull: '\u5468\u516d', weekdaySunFull: '\u5468\u65e5'
    }
  };
  function t(k) { var l = (window.currentLang || 'en').startsWith('zh') ? 'zh' : 'en'; return (I18N[l]||I18N.en)[k] || I18N.en[k] || k; }
  function isZh() { return (window.currentLang || 'en').startsWith('zh'); }
  function esc(s) { return String(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;'); }
  function jsArg(s) { return esc(JSON.stringify(String(s || ''))); }

  function adminToken() { return (typeof window.token === 'function' ? window.token() : '') || localStorage.getItem('maclawHubCenterAdminToken') || sessionStorage.getItem('maclawHubCenterAdminToken') || ''; }
  function apiErrorMessage(e, fallback) {
    if (e && typeof e.error === 'object' && e.error && e.error.message) return e.error.message;
    if (e && typeof e.error === 'string') return e.error;
    return (e && e.message) || fallback;
  }
  async function api(path, opts) {
    if (typeof window.api === 'function') return window.api(path, opts || {});
    var token = adminToken();
    var headers = { 'Content-Type': 'application/json' };
    if (token) headers.Authorization = 'Bearer ' + token;
    var resp = await fetch(path, Object.assign({ headers: headers }, opts));
    if (!resp.ok) { var e = await resp.json().catch(function(){return{};}); throw new Error(apiErrorMessage(e, resp.statusText)); }
    return resp.json();
  }

  var providers = [], providerArrays = [], providerArraysByID = null, providerArrayByMember = null, agents = [], serviceGroups = [];
  var defaultServiceGroupId = '';
  var providerTestStates = {};
  var providerDialogID = '';
  var providerDialogSeq = 0;
  var workBuddySessionID = '';
  var workBuddyModels = [];
  var workBuddyPollTimer = 0;
  var workBuddyPollGen = 0;
  var workBuddyEditions = {
    china: { id: 'workbuddy-cn', name: 'WorkBuddy \u56fd\u5185\u7248', url: 'https://copilot.tencent.com/v2' },
    global: { id: 'workbuddy-global', name: 'WorkBuddy \u56fd\u9645\u7248', url: 'https://www.workbuddy.ai/v2' }
  };
  var providerAccessMode = 'all';
  var providerAccessSelected = {};
  var providerAccessNodes = [];
  var providerBillingSchedule = [];
  var providerTokenPricingSchedule = [];
  var providerServeWindowSchedule = [];
  var providerServeWeekdayOrder = [1, 2, 3, 4, 5, 6, 0];
  var providerServeWeekdayKeys = ['weekdayMonFull','weekdayTueFull','weekdayWedFull','weekdayThuFull','weekdayFriFull','weekdaySatFull','weekdaySunFull'];
  var providerBillingNowTimer = 0;
  var providerBillingTimezoneOptions = ['Asia/Shanghai','Asia/Hong_Kong','Asia/Tokyo','UTC','America/New_York','Europe/London'];
  var providerBillingWeekdayKeys = ['weekdaySun','weekdayMon','weekdayTue','weekdayWed','weekdayThu','weekdayFri','weekdaySat'];
  var providerCapabilityOptions = ['chat','streaming','json','tools','reasoning','vision','document','code','search','audio','embedding','rerank'];
  var llmInitInFlight = null;
  var providersLoadSeq = 0;
  var providerSequenceInFlight = {};
  var providerTrafficReady = false;
  var providerTrafficLoadSeq = 0;
  var providerTrafficInFlight = 0;
  var providerTrafficById = {};
  var providerHealthById = {};
  var providerHealthHistoryState = '';
  var providerHealthEpoch = 0;
  var providerHealthHistoryToastSeq = 0;
  var providerTrafficPeriod = 'day';
  var serviceGroupsLoadSeq = 0;
  var serviceGroupTrafficReady = false;
  var serviceGroupTrafficLoadSeq = 0;
  var serviceGroupTrafficInFlight = 0;
  var serviceGroupTrafficById = {};
  var serviceGroupTrafficPeriod = 'day';
  var serviceGroupTrafficError = '';
  var llmEmbeddingModelRuntimeCache = null;
  var llmEmbeddingLoadSeq = 0;
  var llmEmbeddingRuntimePollTimer = 0;
  var sgDraft = null, sgMode = 'create', sgProviderDraft = null, sgOpenKind = '';
  var sgSaveBusy = false, sgTrainBusy = false, sgHeadBusy = false;
  var sgGroupReturn = null;
  var sgPendingGroupScroll = '';
  var sgGroupScrollMemory = {};
  var sgCapabilityOptions = ['reasoning','tools','document','vision','audio','code','search'];
  var sgPriorityOptions = [0,10,20,30,40,50,60,70,80,90,100];
  var sgResolutionOptions = [0,1,2,3,4,5];
  var sgMultiplierOptions = [0.25,0.5,0.75,1,1.5,2,3,5,10];
  var sgTrafficSeq = 0, sgTrySeq = 0, sgHeadSeq = 0, sgHeadPollTimer = 0;
  var _sgTrafficDataWin = 'day';
  var _testingGroupId = '';

  var llmAdminAPIKeysCache = null;
  var llmAdminAPIKeyVisible = {};
  var llmAdminAPIKeyRevoking = false;
  var llmAdminAPIKeyLoadSeq = 0;
  function llmAdminAPIURL(path) {
    var origin = window.location && window.location.origin && window.location.origin !== 'null' ? window.location.origin : '';
    return origin + path;
  }
  function paintLLMAdminAPIKeyChrome() {
    var title = document.getElementById('llmAdminAPIKeyTitle');
    var desc = document.getElementById('llmAdminAPIKeyDesc');
    var nameLabel = document.getElementById('llmAdminAPIKeyNameLabel');
    var nameInput = document.getElementById('llmAdminAPIKeyName');
    var createBtn = document.getElementById('llmAdminAPIKeyCreate');
    var docLabel = document.getElementById('llmAdminAPIDocLabel');
    var doc = document.getElementById('llmAdminAPIDocLink');
    var spec = document.getElementById('llmAdminAPIOpenAPILink');
    if (title) title.textContent = t('adminAPIKeyTitle');
    if (desc) desc.textContent = t('adminAPIKeyDesc');
    if (nameLabel) nameLabel.textContent = t('adminAPIKeyName');
    if (nameInput) nameInput.placeholder = t('adminAPIKeyName');
    if (createBtn) createBtn.textContent = t('adminAPIKeyCreate');
    if (docLabel) docLabel.textContent = t('adminAPIKeyDoc') + ' · ' + t('adminAPIKeyOpenAPI');
    if (doc) { var docURL = llmAdminAPIURL('/api/llm/admin-api.md'); doc.href = docURL; doc.textContent = docURL; }
    if (spec) { var specURL = llmAdminAPIURL('/api/llm/admin-api.json'); spec.href = specURL; spec.textContent = specURL; }
    if (llmAdminAPIKeysCache) renderLLMAdminAPIKeys(llmAdminAPIKeysCache);
  }
  function renderLLMAdminAPIKeys(keys) {
    var root = document.getElementById('llmAdminAPIKeyList');
    if (!root) return;
    keys = keys || [];
    if (!keys.length) {
      root.innerHTML = '<div class="hint">' + esc(t('adminAPIKeyEmpty')) + '</div>';
      return;
    }
    root.innerHTML = keys.map(function(key) {
      var when = key.created_at ? String(key.created_at).replace('T', ' ').slice(0, 16) : '';
      var secret = key.api_key || '';
      var shown = !!(secret && llmAdminAPIKeyVisible[key.id]);
      var meta = esc(key.prefix || '');
      if (when) meta += ' · ' + esc(t('adminAPIKeyCreatedAt')) + ' ' + esc(when);
      meta += ' · ' + esc(key.scopes && key.scopes.length ? key.scopes.join(', ') : t('adminAPIKeyAllScopes'));
      if (key.expires_at && String(key.expires_at).slice(0, 4) !== '0001') {
        meta += ' · ' + esc(String(key.expires_at).slice(0, 10));
        var expiryAt = new Date(key.expires_at);
        if (!isNaN(expiryAt.getTime()) && expiryAt.getTime() <= Date.now()) meta += ' · ' + esc(t('adminAPIKeyExpired'));
      }
      if (key.last_used_at && String(key.last_used_at).slice(0, 4) !== '0001') meta += ' · ' + esc(t('adminAPIKeyLastUsed')) + ' ' + esc(String(key.last_used_at).replace('T', ' ').slice(0, 16));
      var field = secret
        ? (shown
          ? '<input class="mono llm-admin-key-secret" type="text" readonly autocomplete="off" spellcheck="false" value="' + esc(secret) + '">'
          : '<div class="mono llm-admin-key-secret llm-admin-key-mask">' + esc((key.prefix || 'hck_') + '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022') + '</div>')
        : '<span class="data-row-meta">' + esc(t('adminAPIKeyLegacy')) + '</span>';
      var toggle = secret
        ? '<button type="button" class="btn-ghost" onclick="toggleLLMAdminAPIKey(' + jsArg(key.id) + ')">' + esc(shown ? t('adminAPIKeyHide') : t('adminAPIKeyShow')) + '</button><button type="button" class="btn-ghost" onclick="copyLLMAdminAPIKey(' + jsArg(key.id) + ')">' + esc(t('adminAPIKeyCopy')) + '</button>'
        : '';
      return '<div class="data-row llm-admin-key-row"><div class="data-row-main"><strong>' + esc(key.name || key.id) + '</strong><span class="data-row-meta mono">' + meta + '</span>' + field + '</div><div class="data-row-actions">' + toggle + '<button type="button" class="btn-danger-ghost" onclick="revokeLLMAdminAPIKey(' + jsArg(key.id) + ')">' + esc(t('adminAPIKeyDelete')) + '</button></div></div>';
    }).join('');
  }
  function rememberLLMAdminAPIKey(row) {
    if (!row || !row.id) return;
    llmAdminAPIKeyLoadSeq++;
    var keys = (llmAdminAPIKeysCache || []).filter(function(item) { return item && item.id !== row.id; });
    keys.unshift(row);
    llmAdminAPIKeysCache = keys;
    renderLLMAdminAPIKeys(keys);
  }
  function forgetLLMAdminAPIKey(id) {
    llmAdminAPIKeyLoadSeq++;
    delete llmAdminAPIKeyVisible[id];
    llmAdminAPIKeysCache = (llmAdminAPIKeysCache || []).filter(function(item) { return item && item.id !== id; });
    renderLLMAdminAPIKeys(llmAdminAPIKeysCache);
  }
  async function loadLLMAdminAPIKeys() {
    var seq = ++llmAdminAPIKeyLoadSeq;
    paintLLMAdminAPIKeyChrome();
    try {
      var data = await api('/api/admin/llm/admin-keys');
      if (seq !== llmAdminAPIKeyLoadSeq) return;
      llmAdminAPIKeysCache = (data && data.keys) || [];
      var alive = {};
      llmAdminAPIKeysCache.forEach(function(key) { if (key && key.id) alive[key.id] = true; });
      Object.keys(llmAdminAPIKeyVisible).forEach(function(id) { if (!alive[id]) delete llmAdminAPIKeyVisible[id]; });
      renderLLMAdminAPIKeys(llmAdminAPIKeysCache);
    } catch (e) {
      if (seq !== llmAdminAPIKeyLoadSeq) return;
      if (llmAdminAPIKeysCache) {
        renderLLMAdminAPIKeys(llmAdminAPIKeysCache);
        toast(e.message || String(e), 'error');
        return;
      }
      var root = document.getElementById('llmAdminAPIKeyList');
      if (root) root.innerHTML = '<div class="hint">' + esc(e.message || String(e)) + '</div>';
    }
  }
  window.createLLMAdminAPIKey = async function() {
    var nameEl = document.getElementById('llmAdminAPIKeyName');
    var name = nameEl ? nameEl.value.trim() : '';
    if (!name) { toast(t('adminAPIKeyNeedName'), 'error'); return; }
    var createBtn = document.getElementById('llmAdminAPIKeyCreate');
    if (createBtn) createBtn.disabled = true;
    try {
      var created = await api('/api/admin/llm/admin-keys', { method: 'POST', body: JSON.stringify({ name: name }) });
      if (nameEl) nameEl.value = '';
      rememberLLMAdminAPIKey({
        id: created && created.id,
        name: (created && created.name) || name,
        prefix: created && created.prefix,
        api_key: created && created.api_key,
        scopes: created && created.scopes,
        expires_at: created && created.expires_at,
        created_at: (created && created.created_at) || new Date().toISOString()
      });
      toast(t('adminAPIKeyCreated'), 'success');
      await loadLLMAdminAPIKeys();
    } catch (e) { toast(e.message || String(e), 'error'); }
    finally { if (createBtn) createBtn.disabled = false; }
  };
  window.toggleLLMAdminAPIKey = function(id) {
    if (!id) return;
    if (llmAdminAPIKeyVisible[id]) delete llmAdminAPIKeyVisible[id];
    else llmAdminAPIKeyVisible[id] = true;
    renderLLMAdminAPIKeys(llmAdminAPIKeysCache || []);
  };
  window.copyLLMAdminAPIKey = async function(id) {
    var key = (llmAdminAPIKeysCache || []).find(function(item) { return item && item.id === id; });
    var value = key && key.api_key ? key.api_key : '';
    if (!value) { toast(t('adminAPIKeyLegacy'), 'error'); return; }
    try {
      await writeLLMAdminAPIKeyText(value);
      toast(t('adminAPIKeyCopied'), 'success');
    } catch (e) { toast(e.message || String(e), 'error'); }
  };
  async function writeLLMAdminAPIKeyText(value) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      try {
        await navigator.clipboard.writeText(value);
        return;
      } catch (e) { /* use the selection fallback when clipboard is blocked */ }
    }
    var field = document.createElement('textarea');
    field.value = value;
    field.setAttribute('readonly', '');
    field.style.position = 'fixed';
    field.style.left = '-9999px';
    document.body.appendChild(field);
    try {
      field.select();
      if (!document.execCommand('copy')) throw new Error('copy failed');
    } finally {
      field.remove();
    }
  }
  window.revokeLLMAdminAPIKey = async function(id) {
    if (!id || llmAdminAPIKeyRevoking || !sgConfirm(t('adminAPIKeyDeleteConfirm'))) return;
    llmAdminAPIKeyRevoking = true;
    try {
      await api('/api/admin/llm/admin-keys/' + encodeURIComponent(id), { method: 'DELETE' });
      forgetLLMAdminAPIKey(id);
      await loadLLMAdminAPIKeys();
    } catch (e) { toast(e.message || String(e), 'error'); }
    finally { llmAdminAPIKeyRevoking = false; }
  };
  window.paintLLMAdminAPIKeyChrome = paintLLMAdminAPIKeyChrome;

  window.initLLMServiceTab = async function() {
    if (llmInitInFlight) return llmInitInFlight;
    llmInitInFlight = (async function() {
      await Promise.all([loadProviders(), loadAgents(), loadServiceGroups(), loadLLMProviderMonitorConfig(), loadLLMAdminAPIKeys()]);
      if (llmClassHeadViewVisible() && typeof window.sgReloadClassHeadPage === 'function') window.sgReloadClassHeadPage();
    })();
    try { return await llmInitInFlight; }
    finally { llmInitInFlight = null; }
  };

  function sortedProviders() {
    return (providers || []).slice().sort(function(a, b) {
      var as = Number(a && a.sequence || 0), bs = Number(b && b.sequence || 0);
      if (as <= 0 && bs > 0) return 1;
      if (bs <= 0 && as > 0) return -1;
      if (as !== bs) return as - bs;
      return String(a && (a.name || a.id) || '').localeCompare(String(b && (b.name || b.id) || ''));
    });
  }
  function applyProviderSequenceTargets() {
    if (!Object.keys(providerSequenceInFlight).length) return;
    providers.forEach(function(p) {
      if (providerSequenceInFlight[p.id] != null) p.sequence = providerSequenceInFlight[p.id];
    });
  }
  async function loadProviders(opts) {
    var seq = ++providersLoadSeq;
    var withTraffic = !(opts && opts.traffic === false);
    try {
      var data = await api('/api/admin/llm/providers');
      if (seq !== providersLoadSeq) return;
      providers = data.providers || [];
      providerArrays = data.provider_arrays || [];
      providerArraysByID = null;
      providerArrayByMember = null;
      applyProviderSequenceTargets();
    } catch (e) {
      if (seq !== providersLoadSeq) return;
      toast(e.message || t('sgFailed'), 'error');
      if (!providers.length) renderProviders();
      return;
    }
    renderProviders();
    if (withTraffic) loadProviderTraffic();
  }
  function formatTrafficTokens(value) {
    var n = Number(value || 0);
    if (!isFinite(n) || n <= 0) return '0';
    if (n < 1000) return String(Math.round(n));
    if (n < 1000000) return (n / 1000).toFixed(n < 10000 ? 1 : 0) + 'k';
    // One decimal through 100M. A single array usually stays under 10M and
    // already shows one decimal; the all-array total crosses 10M and has to
    // stay on that same scale.
    return (n / 1000000).toFixed(n < 100000000 ? 1 : 0) + 'M';
  }
  function formatTrafficExact(value) { return String(Math.round(Number(value || 0))); }
  function patchProviderTraffic() {
    var root = document.getElementById('llmProvidersList');
    if (root) {
      root.querySelectorAll('#llmProvidersList .provider-traffic[data-provider-id], #llmProvidersList .provider-traffic[data-traffic-array]').forEach(function(node) {
        applyProviderTrafficNode(node, node.getAttribute('data-provider-id'));
      });
    }
    renderAllArrayTraffic();
  }
  function renderProviderTraffic(id) {
    var root = document.getElementById('llmProvidersList');
    if (!root) return;
    applyProviderTrafficNode(providerTrafficNode(root, id), id);
  }
  function providerTrafficNode(root, id) {
    if (!root) return null;
    var want = String(id || '');
    var nodes = root.querySelectorAll('.provider-traffic[data-provider-id]');
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].getAttribute('data-provider-id') === want) return nodes[i];
    }
    return null;
  }
  function providerTrafficRow(id) {
    return providerPeriodRow(providerTrafficById, id);
  }
  function providerHealthRow(id) {
    return providerPeriodRow(providerHealthById, id);
  }
  function providerPeriodRow(rows, id) {
    var want = String(id || '');
    if (!rows) return null;
    if (rows[want]) return rows[want];
    var lower = want.toLowerCase();
    if (rows[lower]) return rows[lower];
    var keys = Object.keys(rows);
    for (var i = 0; i < keys.length; i++) {
      if (String(keys[i]).toLowerCase() === lower) return rows[keys[i]];
    }
    return null;
  }
  function emptyTrafficPeriods() {
    return {
      day: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
      week: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
      month: { input_tokens: 0, output_tokens: 0, total_tokens: 0 }
    };
  }
  function richerTrafficPeriods(left, right) {
    var out = emptyTrafficPeriods();
    ['day', 'week', 'month'].forEach(function(period) {
      var a = providerTrafficNumbers(left && left[period]);
      var b = providerTrafficNumbers(right && right[period]);
      out[period] = a.total_tokens >= b.total_tokens ? a : b;
    });
    return out;
  }
  function providerArrayForMember(id) {
    if (!providerArrayByMember) providerArrayRecords();
    var want = String(id || '').trim().toLowerCase();
    if (!want || !providerArrayByMember) return null;
    return providerArrayByMember[want] || null;
  }
  function sumTrafficPeriods(left, right) {
    var out = emptyTrafficPeriods();
    accumulateProviderTraffic(out, left);
    accumulateProviderTraffic(out, right);
    return out;
  }
  function memberTrafficRow(id) {
    var usage = providerTrafficRow(id);
    var health = providerHealthRow(id);
    var array = providerArrayForMember(id);
    // Usage booked on the logical array id is the whole array. A member that
    // shares that id must not display it again, or the header adds the other
    // members on top. A one-member array has nowhere else to show it.
    if (array && array.members.length > 1 && sameProviderArrayID(id, array.id)) return health;
    if (array && array.members.length === 1 && !sameProviderArrayID(id, array.id)) {
      var logical = providerTrafficRow(array.id);
      if (usage || logical) usage = sumTrafficPeriods(usage, logical);
    }
    if (usage && health) return richerTrafficPeriods(usage, health);
    return usage || health;
  }
  function providerTrafficWindow(row) {
    if (!row) return null;
    if (providerTrafficPeriod === 'week') return row.week;
    if (providerTrafficPeriod === 'month') return row.month;
    return row.day;
  }
  function providerTrafficNumbers(win) {
    var input = Number(win && win.input_tokens || 0);
    var output = Number(win && win.output_tokens || 0);
    var total = Number(win && win.total_tokens || 0);
    if (!(total > 0)) total = input + output;
    return { input_tokens: input, output_tokens: output, total_tokens: total };
  }
  function accumulateProviderTraffic(out, row) {
    if (!row) return false;
    ['day', 'week', 'month'].forEach(function(period) {
      var nums = providerTrafficNumbers(row[period]);
      out[period].input_tokens += nums.input_tokens;
      out[period].output_tokens += nums.output_tokens;
      out[period].total_tokens += nums.total_tokens;
    });
    return true;
  }
  function foldTrafficMap(raw) {
    var source = {};
    if (Array.isArray(raw)) {
      raw.forEach(function(item) { if (item && item.provider_id) source[item.provider_id] = item; });
    } else if (raw && typeof raw === 'object') source = raw;
    var rows = {};
    Object.keys(source).forEach(function(key) {
      var id = String(key || '').trim().toLowerCase();
      var item = source[key];
      if (!id || !item || typeof item !== 'object') return;
      var row = rows[id] || emptyTrafficPeriods();
      accumulateProviderTraffic(row, item);
      rows[id] = row;
    });
    return rows;
  }
  function arrayTrafficRow(arrayID) {
    var array = providerArrayByID(arrayID);
    var members = array && array.members || [];
    var usage = emptyTrafficPeriods();
    var shown = emptyTrafficPeriods();
    var any = false;
    var seen = {};
    var shownSeen = {};
    function takeUsage(id) {
      var key = String(id || '').trim().toLowerCase();
      if (!key || seen[key]) return;
      seen[key] = true;
      if (accumulateProviderTraffic(usage, providerTrafficRow(id))) any = true;
    }
    members.forEach(function(member) {
      var id = member && member.id;
      var key = String(id || '').trim().toLowerCase();
      takeUsage(id);
      if (!key || shownSeen[key]) return;
      shownSeen[key] = true;
      if (accumulateProviderTraffic(shown, memberTrafficRow(id))) any = true;
    });
    // Usage for older requests sits on the logical array id. Member rows
    // already show the split, so the header keeps whichever picture is larger
    // instead of adding the ledger and the rows together.
    takeUsage((array && array.id) || arrayID);
    if (!any) return null;
    return richerTrafficPeriods(shown, usage);
  }
  // One figure per array. arrayTrafficRow is the multi-member 合计, and for a
  // single member it is the same picture that row shows: the larger of the
  // member rows and the array ledger. The page total adds those figures and
  // does not add the member cells again.
  function displayedArrayTraffic(array) {
    if (!array || !array.id) return null;
    return arrayTrafficRow(array.id);
  }
  function allArrayTrafficRow(arrays) {
    var total = emptyTrafficPeriods();
    var any = false;
    (arrays || providerArrayRecords()).forEach(function(array) {
      if (accumulateProviderTraffic(total, displayedArrayTraffic(array))) any = true;
    });
    return any ? total : null;
  }
  function providerTrafficCells(win, pending, caption) {
    function line(label, value, totalCls) {
      var tip = (caption ? caption + ' \u00b7 ' : '') + providerTrafficPeriodLabel(providerTrafficPeriod) + ' \u00b7 ' + label + ' \u00b7 ' + formatTrafficExact(value);
      return '<div class="provider-traffic-line"><span class="k">' + esc(label) + '</span><span class="v' + (totalCls ? ' total' : '') + '" title="' + esc(tip) + '">' + esc(pending ? t('trafficLoading') : formatTrafficTokens(value)) + '</span></div>';
    }
    return (caption ? '<span class="provider-traffic-sum">' + esc(caption) + '</span>' : '')
      + '<div class="provider-traffic-col">'
      + line(t('trafficIn'), win.input_tokens)
      + line(t('trafficOut'), win.output_tokens)
      + line(t('trafficTotal'), win.total_tokens, true)
      + '</div>';
  }
  function renderAllArrayTraffic() {
    var node = document.getElementById('llmProviderTrafficTotal');
    if (!node) return;
    var arrays = providerArrayRecords();
    if (!arrays.length) {
      if (node.hidden && !node._trafficHTML) return;
      node.hidden = true;
      node._trafficHTML = '';
      node.innerHTML = '';
      return;
    }
    node.hidden = false;
    var row = allArrayTrafficRow(arrays);
    var hasData = !!(row && (row.day || row.week || row.month));
    var pending = !hasData && (!providerTrafficReady || providerTrafficInFlight);
    var win = providerTrafficNumbers(providerTrafficWindow(row));
    var caption = t('providerArraysTrafficSum');
    var html = '<div class="provider-traffic' + (pending ? ' is-pending' : '') + '" role="group" aria-label="' + esc(caption) + '">'
      + providerTrafficCells(win, pending, caption)
      + '</div>';
    if (node._trafficHTML === html) return;
    node._trafficHTML = html;
    node.innerHTML = html;
  }
  function applyProviderTrafficNode(node, id) {
    if (!node) return;
    var arrayID = node.getAttribute('data-traffic-array') || '';
    var row = arrayID ? arrayTrafficRow(arrayID) : memberTrafficRow(id);
    var hasData = !!(row && (row.day || row.week || row.month));
    var pending = !hasData && (!providerTrafficReady || providerTrafficInFlight);
    node.className = 'provider-traffic' + (pending ? ' is-pending' : '');
    var win = providerTrafficNumbers(providerTrafficWindow(row));
    var html = providerTrafficCells(win, pending, arrayID ? t('providerArrayTrafficSum') : '');
    if (node._trafficHTML === html) return;
    node._trafficHTML = html;
    node.innerHTML = html;
  }
  function setProviderTrafficPeriod(period) {
    var next = period === 'week' || period === 'month' ? period : 'day';
    var changed = next !== providerTrafficPeriod;
    providerTrafficPeriod = next;
    syncProviderTrafficSwitch();
    if (!changed) return;
    // A traffic reload may still be deciding whether member_health arrived in
    // one response. Starting the per-day fallback now would add those days on
    // top of that total, or throw the requests away.
    if (next !== 'day' && !(providerTrafficInFlight > 0)) ensureProviderHealthHistory();
    patchProviderTraffic();
  }
  function providerTrafficPeriodLabel(period) {
    if (period === 'week') return t('trafficWeek');
    if (period === 'month') return t('trafficMonth');
    return t('trafficDay');
  }
  function serviceGroupTrafficRow(id) {
    var want = String(id || '').trim();
    if (serviceGroupTrafficById[want]) return serviceGroupTrafficById[want];
    var keys = Object.keys(serviceGroupTrafficById || {});
    var lower = want.toLowerCase();
    for (var i = 0; i < keys.length; i++) {
      if (String(keys[i]).trim().toLowerCase() === lower) return serviceGroupTrafficById[keys[i]];
    }
    return null;
  }
  function serviceGroupTrafficWindow(row) {
    if (!row) return null;
    return row[serviceGroupTrafficPeriod] || null;
  }
  function serviceGroupTrafficTotals(data) {
    if (data && !Array.isArray(data.rows)) {
      var input = Number(data.input_tokens || 0);
      var output = Number(data.output_tokens || 0);
      return {
        input_tokens: input,
        output_tokens: output,
        total_tokens: Number(data.total_tokens || 0) || (input + output)
      };
    }
    var rows = data && Array.isArray(data.rows) ? data.rows : [];
    var total = rows.find(function(row) { return row && row.class === 'total'; });
    if (total) return total;
    return rows.reduce(function(sum, row) {
      if (!row || row.class === 'total') return sum;
      sum.input_tokens += Number(row.input_tokens || 0);
      sum.output_tokens += Number(row.output_tokens || 0);
      sum.total_tokens += Number(row.total_tokens || 0);
      return sum;
    }, { input_tokens: 0, output_tokens: 0, total_tokens: 0 });
  }
  function patchServiceGroupTraffic() {
    var root = document.getElementById('llmServiceGroupsList');
    if (!root) return;
    root.querySelectorAll('.service-group-traffic[data-service-group-id]').forEach(function(node) {
      applyServiceGroupTrafficNode(node, node.getAttribute('data-service-group-id'));
    });
  }
  function applyServiceGroupTrafficNode(node, id) {
    if (!node) return;
    var win = serviceGroupTrafficWindow(serviceGroupTrafficRow(id));
    var pending = !win && (!serviceGroupTrafficReady || serviceGroupTrafficInFlight > 0);
    var totals = serviceGroupTrafficTotals(win);
    var input = Number(totals.input_tokens || 0);
    var output = Number(totals.output_tokens || 0);
    var total = Number(totals.total_tokens || 0) || (input + output);
    node.className = 'service-group-traffic' + (pending ? ' is-pending' : '');
    function line(label, value, totalCls) {
      var tip = providerTrafficPeriodLabel(serviceGroupTrafficPeriod) + ' \u00b7 ' + label + ' \u00b7 ' + formatTrafficExact(value);
      return '<div class="service-group-traffic-line"><span class="k">' + esc(label) + '</span><span class="v' + (totalCls ? ' total' : '') + '" title="' + esc(tip) + '">' + esc(pending ? t('trafficLoading') : formatTrafficTokens(value)) + '</span></div>';
    }
    node.innerHTML = '<div class="service-group-traffic-col">'
      + line(t('trafficIn'), input)
      + line(t('trafficOut'), output)
      + line(t('trafficTotal'), total, true)
      + '</div>';
    node.title = serviceGroupTrafficError || '';
  }
  function setServiceGroupTrafficPeriod(period) {
    var next = period === 'week' || period === 'month' ? period : 'day';
    if (next === serviceGroupTrafficPeriod) return;
    serviceGroupTrafficPeriod = next;
    syncServiceGroupTrafficSwitch();
    patchServiceGroupTraffic();
  }
  function syncServiceGroupTrafficSwitch() {
    var el = document.getElementById('llmServiceGroupTrafficSwitch');
    if (!el) return;
    el.hidden = !serviceGroups.length;
    el.setAttribute('role', 'group');
    el.setAttribute('aria-label', t('trafficDay') + ' / ' + t('trafficWeek') + ' / ' + t('trafficMonth'));
    var buttons = el.querySelectorAll('button[data-period]');
    if (buttons.length !== 3) {
      el.innerHTML = ['day', 'week', 'month'].map(function(period) {
        var on = period === serviceGroupTrafficPeriod;
        return '<button type="button" class="' + (on ? 'is-active' : '') + '" data-period="' + period + '" aria-pressed="' + on + '" onclick="setServiceGroupTrafficPeriod(\'' + period + '\')">' + esc(providerTrafficPeriodLabel(period)) + '</button>';
      }).join('');
      el.onkeydown = onServiceGroupTrafficSwitchKeydown;
      return;
    }
    for (var i = 0; i < buttons.length; i++) {
      var period = buttons[i].getAttribute('data-period');
      var on = period === serviceGroupTrafficPeriod;
      buttons[i].classList.toggle('is-active', on);
      buttons[i].setAttribute('aria-pressed', String(on));
      buttons[i].textContent = providerTrafficPeriodLabel(period);
    }
  }
  function onServiceGroupTrafficSwitchKeydown(event) {
    if (!event || ['ArrowLeft', 'ArrowRight', 'Home', 'End'].indexOf(event.key) < 0) return;
    event.preventDefault();
    var order = ['day', 'week', 'month'];
    var index = order.indexOf(serviceGroupTrafficPeriod);
    if (index < 0) index = 0;
    if (event.key === 'Home') index = 0;
    else if (event.key === 'End') index = order.length - 1;
    else index = event.key === 'ArrowRight' ? Math.min(order.length - 1, index + 1) : Math.max(0, index - 1);
    setServiceGroupTrafficPeriod(order[index]);
    var next = document.querySelector('#llmServiceGroupTrafficSwitch button[data-period="' + order[index] + '"]');
    if (next) next.focus();
  }
  async function loadServiceGroupTraffic() {
    var seq = ++serviceGroupTrafficLoadSeq;
    serviceGroupTrafficInFlight += 1;
    serviceGroupTrafficError = '';
    patchServiceGroupTraffic();
    try {
      var data = await api('/api/admin/llm/service-groups/traffic');
      if (seq !== serviceGroupTrafficLoadSeq) return;
      var traffic = data && data.traffic;
      if (Array.isArray(traffic)) {
        serviceGroupTrafficById = traffic.reduce(function(rows, item) {
          var id = String(item && (item.service_group_id || item.group_id || item.id) || '').trim();
          if (id) rows[id] = item;
          return rows;
        }, {});
      } else {
        serviceGroupTrafficById = traffic && typeof traffic === 'object' ? Object.keys(traffic).reduce(function(rows, id) {
          var normalizedID = String(id || '').trim();
          if (normalizedID) rows[normalizedID] = traffic[id];
          return rows;
        }, {}) : {};
      }
      serviceGroupTrafficReady = true;
    } catch (e) {
      if (seq !== serviceGroupTrafficLoadSeq) return;
      serviceGroupTrafficReady = true;
      serviceGroupTrafficError = e.message || t('sgFailed');
      if (!Object.keys(serviceGroupTrafficById).length) toast(serviceGroupTrafficError, 'error');
    } finally {
      serviceGroupTrafficInFlight = Math.max(0, serviceGroupTrafficInFlight - 1);
      if (seq === serviceGroupTrafficLoadSeq) patchServiceGroupTraffic();
    }
  }
  function syncProviderTrafficSwitch() {
    var el = document.getElementById('llmProviderTrafficSwitch');
    if (!el) return;
    el.hidden = !providers.length;
    el.setAttribute('role', 'group');
    el.setAttribute('aria-label', t('trafficDay') + ' / ' + t('trafficWeek') + ' / ' + t('trafficMonth'));
    var buttons = el.querySelectorAll('button[data-period]');
    if (buttons.length === 3) {
      for (var i = 0; i < buttons.length; i++) {
        var period = buttons[i].getAttribute('data-period');
        var on = period === providerTrafficPeriod;
        buttons[i].classList.toggle('is-active', on);
        buttons[i].setAttribute('aria-pressed', String(on));
        buttons[i].textContent = providerTrafficPeriodLabel(period);
      }
      return;
    }
    el.innerHTML = ['day','week','month'].map(function(p) {
      var on = providerTrafficPeriod === p;
      return '<button type="button" class="' + (on ? 'is-active' : '') + '" data-period="' + p + '" aria-pressed="' + on + '" onclick="setProviderTrafficPeriod(\'' + p + '\')">' + esc(providerTrafficPeriodLabel(p)) + '</button>';
    }).join('');
    el.onkeydown = onProviderTrafficSwitchKeydown;
  }
  function onProviderTrafficSwitchKeydown(event) {
    if (!event || ['ArrowLeft', 'ArrowRight', 'Home', 'End'].indexOf(event.key) < 0) return;
    event.preventDefault();
    var order = ['day','week','month'];
    var i = order.indexOf(providerTrafficPeriod);
    if (i < 0) i = 0;
    if (event.key === 'Home') i = 0;
    else if (event.key === 'End') i = order.length - 1;
    else i = event.key === 'ArrowRight' ? Math.min(order.length - 1, i + 1) : Math.max(0, i - 1);
    setProviderTrafficPeriod(order[i]);
    var next = document.querySelector('#llmProviderTrafficSwitch button[data-period="' + order[i] + '"]');
    if (next) next.focus();
  }
  function normalizeTrafficMap(raw) {
    return foldTrafficMap(raw);
  }
  function shanghaiDateText(date) {
    var parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(date);
    var year = '', month = '', day = '';
    parts.forEach(function(part) {
      if (part.type === 'year') year = part.value;
      else if (part.type === 'month') month = part.value;
      else if (part.type === 'day') day = part.value;
    });
    return year + '-' + month + '-' + day;
  }
  function shanghaiDayOffset(daysAgo) {
    // Noon keeps the Shanghai calendar date on the same UTC day. Midnight +08
    // is the previous UTC evening, so its weekday is one day behind.
    var noon = new Date(shanghaiDateText(new Date()) + 'T12:00:00+08:00');
    noon.setUTCDate(noon.getUTCDate() - daysAgo);
    return shanghaiDateText(noon);
  }
  function shanghaiTrafficBounds() {
    var day = shanghaiDayOffset(0);
    var wd = new Date(day + 'T12:00:00+08:00').getUTCDay();
    var delta = wd === 0 ? 6 : wd - 1;
    return { day: day, week: shanghaiDayOffset(delta), month: day.slice(0, 8) + '01' };
  }
  function addHealthPeriod(rows, id, day, bounds, input, output) {
    if (day > bounds.day) return;
    var row = rows[id];
    if (!row) row = rows[id] = emptyTrafficPeriods();
    function add(bucket) {
      bucket.input_tokens += input;
      bucket.output_tokens += output;
      bucket.total_tokens += input + output;
    }
    if (day >= bounds.month) add(row.month);
    if (day >= bounds.week) add(row.week);
    if (day === bounds.day) add(row.day);
  }
  function mergeHealthReport(rows, day, data, bounds) {
    var nodes = data && data.nodes || [];
    nodes.forEach(function(node) {
      (node.members || []).forEach(function(member) {
        var id = String(member && member.id || '').trim().toLowerCase();
        var input = Number(member && member.input_tokens || 0);
        var output = Number(member && member.output_tokens || 0);
        if (!id || (!(input > 0) && !(output > 0))) return;
        addHealthPeriod(rows, id, day, bounds, input, output);
      });
    });
  }
  function fetchMemberHealthDay(day) {
    return api('/api/admin/llm/member-health?day=' + encodeURIComponent(day)).then(function(data) {
      return data || null;
    }, function() { return null; });
  }
  function providerHealthHistoryDays(bounds) {
    var rest = [];
    var floor = bounds.week < bounds.month ? bounds.week : bounds.month;
    for (var i = 1; i < 14; i++) {
      var day = shanghaiDayOffset(i);
      if (day >= floor && day < bounds.day) rest.push(day);
    }
    return rest;
  }
  function ensureProviderHealthHistory() {
    if (providerHealthHistoryState === 'api' || providerHealthHistoryState === 'ready' || providerHealthHistoryState === 'loading') return;
    var bounds = shanghaiTrafficBounds();
    var rest = providerHealthHistoryDays(bounds);
    if (!rest.length) { providerHealthHistoryState = 'ready'; return; }
    var seq = providerTrafficLoadSeq;
    var epoch = providerHealthEpoch;
    providerHealthHistoryState = 'loading';
    function pull(days, attempt) {
      Promise.all(days.map(function(day) {
        return fetchMemberHealthDay(day).then(function(data) { return { day: day, data: data }; });
      })).then(function(reports) {
        if (seq !== providerTrafficLoadSeq || epoch !== providerHealthEpoch || providerHealthHistoryState === 'api') return;
        var rows = providerHealthById || {};
        var failed = [];
        reports.forEach(function(item) {
          if (!item.data) { failed.push(item.day); return; }
          mergeHealthReport(rows, item.day, item.data, bounds);
        });
        providerHealthById = rows;
        if (failed.length && attempt < 1) {
          patchProviderTraffic();
          pull(failed, attempt + 1);
          return;
        }
        providerHealthHistoryState = failed.length ? '' : 'ready';
        if (failed.length && providerTrafficPeriod !== 'day' && providerHealthHistoryToastSeq !== seq) {
          providerHealthHistoryToastSeq = seq;
          toast(t('sgFailed'), 'error');
        }
        patchProviderTraffic();
      }).catch(function() {
        if (seq === providerTrafficLoadSeq && epoch === providerHealthEpoch && providerHealthHistoryState === 'loading') providerHealthHistoryState = '';
      });
    }
    pull(rest, 0);
  }
  async function loadProviderHealthFallback(seq, reportFailure) {
    var bounds = shanghaiTrafficBounds();
    var merged = {};
    var today = await fetchMemberHealthDay(bounds.day);
    if (!today && seq === providerTrafficLoadSeq) today = await fetchMemberHealthDay(bounds.day);
    if (seq !== providerTrafficLoadSeq) return;
    if (!today) {
      if (reportFailure && !Object.keys(providerHealthById || {}).length) toast(t('sgFailed'), 'error');
      if (providerTrafficPeriod !== 'day') ensureProviderHealthHistory();
      return;
    }
    mergeHealthReport(merged, bounds.day, today, bounds);
    providerHealthEpoch++;
    providerHealthById = merged;
    if (providerHealthHistoryState !== 'api') providerHealthHistoryState = '';
    if (providerTrafficPeriod !== 'day') ensureProviderHealthHistory();
  }
  async function loadProviderTraffic() {
    var seq = ++providerTrafficLoadSeq;
    providerHealthHistoryState = '';
    providerTrafficInFlight += 1;
    if (!providerTrafficReady) patchProviderTraffic();
    try {
      var data = await api('/api/admin/llm/providers/traffic');
      if (seq !== providerTrafficLoadSeq) return;
      var traffic = data && data.traffic;
      // Tolerate both shapes: a dense array of rows, and an id-keyed object.
      // Array.isArray(data.traffic) stays the primary read.
      if (Array.isArray(data.traffic)) {
        providerTrafficById = foldTrafficMap(data.traffic);
      } else {
        providerTrafficById = foldTrafficMap(traffic);
      }
      if (data && Object.prototype.hasOwnProperty.call(data, 'member_health')) {
        providerHealthEpoch++;
        providerHealthById = normalizeTrafficMap(data.member_health);
        providerHealthHistoryState = 'api';
      } else if (seq === providerTrafficLoadSeq) {
        await loadProviderHealthFallback(seq, true);
      }
      providerTrafficReady = true;
    } catch (e) {
      if (seq !== providerTrafficLoadSeq) return;
      providerTrafficReady = true;
      await loadProviderHealthFallback(seq, false);
      if (!Object.keys(providerTrafficById || {}).length && !Object.keys(providerHealthById || {}).length) {
        providerHealthHistoryToastSeq = seq;
        toast(e.message || t('sgFailed'), 'error');
      }
    } finally {
      providerTrafficInFlight = Math.max(0, providerTrafficInFlight - 1);
      if (seq === providerTrafficLoadSeq) patchProviderTraffic();
    }
  }
  function providerArrayByID(id) {
    id = String(id || '').trim();
    if (!providerArraysByID) providerArrayRecords();
    if (!id || !providerArraysByID) return null;
    if (providerArraysByID[id]) return providerArraysByID[id];
    var key = providerArrayLockKey(id);
    var ids = Object.keys(providerArraysByID);
    for (var i = 0; i < ids.length; i++) {
      if (providerArrayLockKey(ids[i]) === key) return providerArraysByID[ids[i]];
    }
    return null;
  }
  function providerArrayProtected(array) {
    if (!array) return false;
    if (array.system) return true;
    var id = String(array.id || '').trim().toLowerCase();
    return id === 'token_bank_low' || id === 'token_bank_mid' || id === 'token_bank_high';
  }
  function providerArrayBillingFields(record, member) {
    var src = record || {};
    var fallback = member || {};
    var schedule = (src.credit_multiplier_schedule && src.credit_multiplier_schedule.length) ? src.credit_multiplier_schedule : (fallback.credit_multiplier_schedule || []);
    var pricing = src.token_pricing;
    if (!pricing || !Object.keys(pricing).length) pricing = fallback.token_pricing || {};
    var multiplier = Number(src.credit_multiplier);
    if (!(multiplier > 0)) multiplier = Number(fallback.credit_multiplier);
    if (!(multiplier > 0)) multiplier = 1;
    return {
      timezone: src.timezone || fallback.timezone || 'Asia/Shanghai',
      credit_multiplier: multiplier,
      credit_multiplier_schedule: schedule,
      token_pricing: pricing
    };
  }
  function providerArrayRecords() {
    var meta = {};
    (providerArrays || []).forEach(function(array) {
      if (array && array.id) meta[array.id] = array;
    });
    var groups = [];
    var seen = {};
    sortedProviders().forEach(function(provider) {
      var id = provider.array_id || provider.id;
      if (!id || seen[id]) return;
      seen[id] = true;
      var record = meta[id] || { id: id, name: provider.name || id };
      var members = [];
      var included = {};
      (record.member_ids || []).forEach(function(memberID) {
        var item = providers.find(function(providerItem) { return providerItem.id === memberID; });
        if (!item || included[item.id]) return;
        included[item.id] = true;
        members.push(item);
      });
      providers.forEach(function(item) {
        if ((item.array_id || item.id) !== id || included[item.id]) return;
        included[item.id] = true;
        members.push(item);
      });
      if (!members.length) members = [provider];
      var billing = providerArrayBillingFields(record, members[0]);
      groups.push({
        id: id,
        name: record.name || (members[0] && (members[0].name || members[0].id)) || id,
        members: members,
        system: !!record.system,
        timezone: billing.timezone,
        credit_multiplier: billing.credit_multiplier,
        credit_multiplier_schedule: billing.credit_multiplier_schedule,
        token_pricing: billing.token_pricing
      });
    });
    (providerArrays || []).forEach(function(array) {
      if (!array || !array.id || seen[array.id]) return;
      seen[array.id] = true;
      var billing = providerArrayBillingFields(array, null);
      groups.push({
        id: array.id,
        name: array.name || array.id,
        members: [],
        system: !!array.system,
        timezone: billing.timezone,
        credit_multiplier: billing.credit_multiplier,
        credit_multiplier_schedule: billing.credit_multiplier_schedule,
        token_pricing: billing.token_pricing
      });
    });
    providerArraysByID = {};
    providerArrayByMember = {};
    groups.forEach(function(array) {
      providerArraysByID[array.id] = array;
      (array.members || []).forEach(function(member) {
        var id = String(member && member.id || '').trim().toLowerCase();
        if (id) providerArrayByMember[id] = array;
      });
    });
    return groups;
  }
  function providerCanaryActive(provider, now) {
    var raw = String(provider && provider.token_bank_canary_until || '').trim();
    if (!raw) return null;
    var until = new Date(raw);
    if (isNaN(until.getTime())) return null;
    if ((now || new Date()).getTime() >= until.getTime()) return null;
    return until;
  }
  function formatProviderCanaryUntil(until) {
    // Shanghai is UTC+8 all year. Shift then read UTC fields so midnight
    // stays on the right day. Intl hourCycle h23 can report 24:00 on the previous day.
    var shifted = new Date(until.getTime() + 8 * 60 * 60 * 1000);
    if (isNaN(shifted.getTime())) return '';
    var pad2 = function(value) { value = String(value); return value.length >= 2 ? value : ('0' + value).slice(-2); };
    return shifted.getUTCFullYear() + '-' + pad2(shifted.getUTCMonth() + 1) + '-' + pad2(shifted.getUTCDate()) + ' ' + pad2(shifted.getUTCHours()) + ':' + pad2(shifted.getUTCMinutes());
  }
  function providerCanaryBird(label) {
    return '<svg class="provider-canary-bird" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">'
      + '<title>' + esc(label || '') + '</title>'
      + '<path fill="#E0A31A" d="M.9 8.8c1.4-.9 2.6-.5 3.4.4-.6.6-1.6 1-2.7.6-.8-.3-1.2-.7-.7-1z"/>'
      + '<ellipse cx="7.2" cy="9.7" rx="3.3" ry="2.45" fill="#F5C542"/>'
      + '<path fill="#E2A31A" d="M5.3 8.7c1.2-.5 2.5.1 2.9 1.2.3.8 0 1.6-.8 1.9-1.1.4-2.3-.1-2.7-1.1-.3-.7-.1-1.5.6-2z"/>'
      + '<circle cx="10.8" cy="6.5" r="2.25" fill="#F7D15A"/>'
      + '<path fill="#D48412" d="M12.5 6.2 15.3 7.1 12.4 8z"/>'
      + '<circle cx="11.35" cy="6" r=".9" fill="#3d2b12"/>'
      + '<circle cx="11.6" cy="5.75" r=".28" fill="#fff8e6"/>'
      + '</svg>';
  }
  function providerCanaryMark(provider) {
    var until = providerCanaryActive(provider);
    if (!until) return '';
    var formatted = formatProviderCanaryUntil(until);
    if (!formatted) return '';
    var label = t('providerCanaryUntil').replace('{time}', formatted);
    return '<span class="provider-canary-mark" data-provider-id="' + esc(provider && provider.id || '') + '" role="img" title="' + esc(label) + '" aria-label="' + esc(label) + '">' + providerCanaryBird(label) + '</span>';
  }
  var providerCanaryTimer = 0;
  function scheduleProviderCanaryRefresh() {
    if (providerCanaryTimer) {
      clearTimeout(providerCanaryTimer);
      providerCanaryTimer = 0;
    }
    var now = Date.now();
    var nowDate = new Date(now);
    var next = 0;
    (providers || []).forEach(function(provider) {
      var until = providerCanaryActive(provider, nowDate);
      if (!until) return;
      var at = until.getTime();
      if (!next || at < next) next = at;
    });
    if (!next) return;
    var delay = next - now + 250;
    if (delay < 250) delay = 250;
    if (delay > 2147483647) delay = 2147483647;
    providerCanaryTimer = setTimeout(refreshProviderCanaryMarks, delay);
  }
  function refreshProviderCanaryMarks() {
    if (providerCanaryTimer) clearTimeout(providerCanaryTimer);
    providerCanaryTimer = 0;
    var root = document.getElementById('llmProvidersList');
    if (root) {
      var now = new Date();
      root.querySelectorAll('.provider-canary-mark').forEach(function(mark) {
        var id = mark.getAttribute('data-provider-id') || '';
        var provider = null;
        for (var i = 0; i < providers.length; i++) {
          if (id && providers[i] && providers[i].id === id) { provider = providers[i]; break; }
        }
        if (!providerCanaryActive(provider, now)) mark.remove();
      });
    }
    scheduleProviderCanaryRefresh();
  }
  document.addEventListener('visibilitychange', function() {
    if (document.visibilityState === 'hidden' || !providerCanaryTimer) return;
    refreshProviderCanaryMarks();
  });
  function renderProviderRow(p) {
    var testState = providerTestStates[p.id];
    var testHTML = renderProviderTestState(testState);
    var testDisabled = testState && testState.status === 'testing' ? ' disabled' : '';
    var providerArg = jsArg(p.id);
    var seq = Number(p.sequence || 0);
    var paused = !!p.paused;
    var dragHint = esc(t('providerArrayDragHint'));
    return '<div class="data-row' + (paused ? ' is-paused' : '') + '" data-provider-id="' + esc(p.id) + '">'
      + '<div class="provider-seq' + (seq > 0 ? '' : ' is-unset') + '" draggable="true" title="' + dragHint + '">' + esc(seq > 0 ? String(seq) : '-') + '</div>'
      + '<div class="data-row-main" draggable="true"><div class="data-row-title"><strong><span class="data-row-name" title="' + dragHint + '">' + esc(p.name||p.id) + '</span>' + providerCanaryMark(p) + '</strong>'
      + (paused ? '<span class="badge warn">' + esc(t('pauseProvider')) + '</span>' : '')
      + (p.lb_group && Number(p.lb_group_size||0) >= 2 ? '<span class="badge info">' + esc(t('lbGroup')) + ' ' + esc(p.lb_group) + '</span>' : '')
      + providerBillingBadge(p)
      + providerWorkBuddyBadge(p)
      + providerAccessBadge(p)
      + providerServeBadge(p)
      + '</div>'
      + '<span class="data-row-meta" title="' + dragHint + '">' + esc(p.api_url) + ' \u00b7 ' + esc(p.protocol||'openai')
      + (p.has_api_key ? ' \u00b7 key' : '') + (p.lb_group ? ' \u00b7 ' + esc(p.lb_group) : '')
      + (providerPriceSummary(p) ? ' \u00b7 ' + esc(providerPriceSummary(p)) : '')
      + (providerPriceScheduleCount(p) ? ' \u00b7 ' + esc(t('pricingSchedule')) + ': ' + providerPriceScheduleCount(p) : '') + '</span>'
      + testHTML + '</div>'
      + '<div class="provider-traffic' + (providerTrafficReady ? '' : ' is-pending') + '" data-provider-id="' + esc(p.id) + '"></div>'
      + '<div class="data-row-actions">'
      + '<button class="btn-ghost" onclick="moveLLMProvider(' + providerArg + ',-1)">\u2191</button>'
      + '<button class="btn-ghost" onclick="moveLLMProvider(' + providerArg + ',1)">\u2193</button>'
      + '<button class="btn-ghost" onclick="toggleLLMProviderPaused(' + providerArg + ')">' + esc(paused ? t('resumeProvider') : t('pauseProvider')) + '</button>'
      + '<button class="btn-ghost provider-test-btn" onclick="testLLMProvider(' + providerArg + ')"' + testDisabled + '>' + esc(testState && testState.status === 'testing' ? t('providerTesting') : t('testProvider')) + '</button>'
      + '<button class="btn-ghost" onclick="editLLMProvider(' + providerArg + ')">' + esc(t('editProvider')) + '</button>'
      + '<button class="btn-danger-ghost" onclick="deleteLLMProvider(' + providerArg + ')">' + esc(t('deleteProvider')) + '</button>'
      + '</div></div>';
  }
  function renderProviderArray(array) {
    var multi = array.members.length > 1;
    var open = !!providerArrayExpanded[array.id];
    var headTraffic = '';
    if (multi) headTraffic = '<div class="provider-traffic' + (providerTrafficReady ? '' : ' is-pending') + '" data-traffic-array="' + esc(array.id) + '"></div>';
    else if (!open && array.members[0]) headTraffic = '<div class="provider-traffic' + (providerTrafficReady ? '' : ' is-pending') + '" data-provider-id="' + esc(array.members[0].id) + '"></div>';
    var head = '<div class="provider-array-head"><div class="provider-array-title"><strong>' + esc(array.name || array.id) + '</strong>'
      + '<span class="badge">' + esc(t('providerArray')) + ' \u00b7 ' + array.members.length + '</span>'
      + (multi ? '<span class="badge info">' + esc(t('providerArrayShared')) + '</span>' : '')
      + (providerPriceSummary(array) ? '<span class="data-row-meta"> ' + esc(providerPriceSummary(array)) + '</span>' : '')
      + '</div>' + headTraffic + '<div class="data-row-actions">'
      + '<button class="btn-ghost" type="button" aria-expanded="' + (open ? 'true' : 'false') + '" onclick="toggleProviderArray(' + jsArg(array.id) + ')">' + esc(open ? t('providerArrayCollapse') : t('providerArrayExpand')) + '</button>'
      + '<button class="btn-ghost" type="button" onclick="showProviderDialog(\'create\', \'\', {arrayID:' + jsArg(array.id) + '})">' + esc(t('providerArrayAdd')) + '</button>'
      + '<button class="btn-ghost" type="button" onclick="editProviderArray(' + jsArg(array.id) + ')">' + esc(t('providerArrayEdit')) + '</button>'
      + (providerArrayProtected(array) ? '' : '<button class="btn-ghost" type="button" onclick="renameProviderArray(' + jsArg(array.id) + ')">' + esc(t('providerArrayRename')) + '</button>')
      + (providerArrayProtected(array) ? '' : '<button class="btn-danger-ghost" type="button" onclick="deleteProviderArray(' + jsArg(array.id) + ')">' + esc(t('providerArrayDelete')) + '</button>')
      + '</div></div>';
    var body = '';
    if (open) {
      body = array.members.length
        ? array.members.map(function(member) { return renderProviderRow(member); }).join('')
        : '<div class="hint">' + esc(t('providerArrayEmpty')) + '</div>';
    }
    return '<div class="provider-array' + (multi ? ' is-multi' : '') + (open ? '' : ' is-collapsed') + '" data-array-id="' + esc(array.id) + '">' + head + body + '</div>';
  }
  function renderProviders() {
    var el = document.getElementById('llmProvidersList');
    if (!el) return;
    syncProviderTrafficSwitch();
    var arrays = providerArrayRecords();
    var alive = {};
    arrays.forEach(function(array) { if (array && array.id) alive[array.id] = true; });
    Object.keys(providerArrayExpanded).forEach(function(id) { if (!alive[id]) delete providerArrayExpanded[id]; });
    if (!arrays.length) { el.innerHTML = '<div class="hint">' + esc(t('noProviders')) + '</div>'; renderAllArrayTraffic(); scheduleProviderCanaryRefresh(); return; }
    el.innerHTML = arrays.map(renderProviderArray).join('');
    bindProviderArrayDrag(el);
    patchProviderTraffic();
    scheduleProviderCanaryRefresh();
    startProviderServeBadgeClock();
  }
  window.toggleProviderArray = function(id) {
    if (!id) return;
    if (providerArrayExpanded[id]) delete providerArrayExpanded[id];
    else providerArrayExpanded[id] = true;
    renderProviders();
  };
  var providerArrayExpanded = {};
  var providerDragID = '';
  var providerDragHome = '';
  function dragEventNode(event) {
    var node = event && event.target;
    if (!node) return null;
    if (node.nodeType !== 1) node = node.parentElement;
    return node || null;
  }
  function clearProviderDragMarks(root) {
    if (!root) return;
    root.querySelectorAll('.is-dragging, .is-drop-target').forEach(function(node) {
      node.classList.remove('is-dragging');
      node.classList.remove('is-drop-target');
    });
  }
  function setProviderDragOver(root, card) {
    var current = root.querySelector('.provider-array.is-drop-target');
    if (current === card) return;
    if (current) current.classList.remove('is-drop-target');
    if (card) card.classList.add('is-drop-target');
  }
  function bindProviderArrayDrag(root) {
    if (!root || root.getAttribute('data-array-drag') === '1') return;
    root.setAttribute('data-array-drag', '1');
    root.addEventListener('dragstart', function(event) {
      var node = dragEventNode(event);
      var row = node && node.closest('.data-row');
      if (!row || !root.contains(row)) return;
      if (node.closest('button, a, input, select, textarea, label')) {
        event.preventDefault();
        return;
      }
      var id = row.getAttribute('data-provider-id') || '';
      if (!id) { event.preventDefault(); return; }
      var provider = providers.find(function(item) { return item.id === id; });
      if (!provider) { event.preventDefault(); return; }
      providerDragID = id;
      providerDragHome = providerArrayLockKey(provider.array_id || provider.id);
      row.classList.add('is-dragging');
      if (!event.dataTransfer) return;
      event.dataTransfer.effectAllowed = 'move';
      try { event.dataTransfer.setData('text/plain', id); } catch (e) {}
    });
    function allowProviderArrayDrop(event) {
      if (!providerDragID) return;
      var node = dragEventNode(event);
      var card = node && node.closest('.provider-array');
      var target = (card && card.getAttribute('data-array-id')) || '';
      if (!card || !target || providerDragHome === providerArrayLockKey(target)) {
        setProviderDragOver(root, null);
        return;
      }
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
      setProviderDragOver(root, card);
    }
    root.addEventListener('dragenter', allowProviderArrayDrop);
    root.addEventListener('dragover', allowProviderArrayDrop);
    root.addEventListener('drop', function(event) {
      var node = dragEventNode(event);
      var card = node && node.closest('.provider-array');
      var id = providerDragID;
      if (!id && event.dataTransfer) {
        try { id = event.dataTransfer.getData('text/plain') || ''; } catch (e) {}
      }
      var target = (card && card.getAttribute('data-array-id')) || '';
      if (!card || !id || !target || providerDragHome === providerArrayLockKey(target)) return;
      event.preventDefault();
      providerDragID = '';
      providerDragHome = '';
      clearProviderDragMarks(root);
      window.moveProviderToArray(id, target);
    });
    root.addEventListener('dragend', function() {
      providerDragID = '';
      providerDragHome = '';
      clearProviderDragMarks(root);
    });
  }
  function providerMovePayload(provider, arrayID) {
    var payload = copyProviderExtraFields(provider);
    payload.id = provider.id;
    payload.name = provider.name || provider.id;
    payload.api_url = provider.api_url || '';
    payload.protocol = provider.protocol || 'openai';
    payload.models = provider.models || [];
    payload.capability_tags = provider.capability_tags || [];
    payload.priority = provider.priority || 0;
    payload.sequence = provider.sequence || 0;
    payload.max_concurrency = provider.max_concurrency || 0;
    payload.upstream_timeout_sec = provider.upstream_timeout_sec || 0;
    payload.timezone = provider.timezone || 'Asia/Shanghai';
    payload.credit_multiplier = provider.credit_multiplier > 0 ? provider.credit_multiplier : 1;
    payload.credit_multiplier_schedule = provider.credit_multiplier_schedule || [];
    var pricing = provider.token_pricing || {};
    if (pricing.price_schedule && pricing.price_schedule.length && !String(pricing.timezone || '').trim()) {
      pricing = Object.assign({}, pricing, { timezone: provider.timezone || 'Asia/Shanghai' });
    }
    payload.token_pricing = pricing;
    if (provider.allowed_node_ids) payload.allowed_node_ids = provider.allowed_node_ids;
    payload.serve_windows = Array.isArray(provider.serve_windows) ? provider.serve_windows : [];
    payload.array_id = arrayID;
    return payload;
  }
  window.moveProviderToArray = async function(providerID, arrayID) {
    var provider = providers.find(function(item) { return item.id === providerID; });
    arrayID = String(arrayID || '').trim();
    if (!provider || !arrayID) return;
    var home = provider.array_id || provider.id;
    if (providerArrayLockKey(home) === providerArrayLockKey(arrayID)) return;
    if (!providerArrayByID(arrayID)) return;
    if (!beginProviderArrayAction(home)) { toast(t('providerArrayBusy'), 'error'); return; }
    if (!beginProviderArrayAction(arrayID)) { endProviderArrayAction(home); toast(t('providerArrayBusy'), 'error'); return; }
    try {
      await api('/api/admin/llm/providers/' + encodeURIComponent(provider.id), { method: 'PUT', body: JSON.stringify(providerMovePayload(provider, arrayID)) });
      providerArrayExpanded[arrayID] = true;
      toast(t('providerArrayMoved'), 'success');
      await loadProviders({ traffic: false });
      await loadServiceGroups();
    } catch (e) { toast(e.message, 'error'); }
    finally {
      endProviderArrayAction(home);
      endProviderArrayAction(arrayID);
    }
  };
  function renderProviderTestState(state) {
    if (!state) return '';
    if (state.status === 'testing') return '<span class="provider-test-status is-testing">' + esc(t('providerTesting')) + '</span>';
    if (state.status === 'ok') {
      return '<span class="provider-test-status is-ok"><span class="badge ok">' + esc(t('providerTestOK')) + '</span> '
        + esc(t('providerTestLatency')) + ': ' + esc(state.latency_ms) + 'ms \u00b7 '
        + esc(t('providerTestModels')) + ': ' + esc(state.model || '') + '</span>';
    }
    return '<span class="provider-test-status is-error"><span class="badge danger">' + esc(t('providerTestFailed')) + '</span> ' + esc(state.message || '') + '</span>';
  }
  window.testLLMProvider = async function(id) {
    var provider = providers.find(function(p){ return p.id === id; });
    if (!provider) return;
    var model = (provider.models && provider.models[0]) || '';
    if (!model) {
      providerTestStates[id] = { status: 'error', message: 'No model configured' };
      renderProviders();
      toast(t('providerTestFailed') + ': No model configured', 'error');
      return;
    }
    providerTestStates[id] = { status: 'testing' };
    renderProviders();
    var started = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
    try {
      var data = await api('/api/admin/llm/providers/test-chat', { method: 'POST', body: JSON.stringify({
        provider_id: provider.id, api_url: provider.api_url, model: model, protocol: provider.protocol || 'openai', wire_api: provider.wire_api || 'chat'
      }) });
      var ended = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
      if (!data.success) throw new Error(data.error || 'unknown');
      providerTestStates[id] = { status: 'ok', latency_ms: data.latency_ms || Math.max(1, Math.round(ended - started)), model: data.model || model };
      toast(t('providerTestOK') + ': ' + (provider.name || provider.id), 'success');
    } catch(e) {
      providerTestStates[id] = { status: 'error', message: e.message };
      toast(t('providerTestFailed') + ': ' + e.message, 'error');
    }
    renderProviders();
  };
  var monitorSaveBusy = false;
  var monitorConfigLoadSeq = 0;
  function setLLMProviderMonitorSaveEnabled(enabled) {
    var btn = document.getElementById('llmProviderMonitorSave');
    if (btn) btn.disabled = !enabled;
  }
  async function loadLLMProviderMonitorConfig(opts) {
    var silent = !!(opts && opts.silent);
    var force = !!(opts && opts.force);
    var seq = ++monitorConfigLoadSeq;
    try {
      var cfg = await api('/api/admin/llm/provider-monitor/config');
      if (seq !== monitorConfigLoadSeq) return;
      var enabledEl = document.getElementById('llmProviderMonitorEnabled');
      // Don't clobber in-progress edits: a stale load must not overwrite a
      // field while the admin is interacting with it. The save-error resync
      // passes force because Safari does not move focus to the save button,
      // which would otherwise leave the form stuck on never-persisted state.
      if (enabledEl && (force || document.activeElement !== enabledEl)) enabledEl.checked = !!(cfg && cfg.enabled);
      var intervalEl = document.getElementById('llmProviderMonitorInterval');
      if (intervalEl && (force || document.activeElement !== intervalEl)) intervalEl.value = (cfg && cfg.interval_hours) || 3;
      // Only a successful load makes the form authoritative enough to save.
      setLLMProviderMonitorSaveEnabled(true);
    } catch (e) {
      if (seq !== monitorConfigLoadSeq) return;
      setLLMProviderMonitorSaveEnabled(false);
      if (window.console && console.warn) console.warn('llm provider monitor config load failed', e);
      if (!silent) {
        var detail = e && e.message ? String(e.message).replace(/\.$/, '') : '';
        toast(t('monitorLoadRetry') + (detail ? ' (' + detail + ')' : ''), 'error');
      }
    }
  }
  window.saveLLMProviderMonitorConfig = async function() {
    if (monitorSaveBusy) return;
    var enabledEl = document.getElementById('llmProviderMonitorEnabled');
    var intervalEl = document.getElementById('llmProviderMonitorInterval');
    var hours = Number(intervalEl && intervalEl.value);
    if (!isFinite(hours) || hours !== Math.round(hours) || hours < 1) {
      toast(t('monitorInvalidInterval'), 'error');
      return;
    }
    monitorSaveBusy = true;
    setLLMProviderMonitorSaveEnabled(false);
    try {
      await api('/api/admin/llm/provider-monitor/config', { method: 'PUT', body: JSON.stringify({
        enabled: !!(enabledEl && enabledEl.checked), interval_hours: hours
      }) });
      toast(t('monitorSaved'), 'success');
      setLLMProviderMonitorSaveEnabled(true);
    } catch (e) {
      toast(e.message || t('sgFailed'), 'error');
      // The server rejects the save when the enable test email fails; reload
      // so the form reflects the persisted (unchanged) state. The reload
      // re-enables the button only when it succeeds; it stays silent so a
      // failing reload does not stack a second error toast over the save
      // error (it still logs to the console).
      await loadLLMProviderMonitorConfig({ silent: true, force: true });
    } finally {
      monitorSaveBusy = false;
    }
  };
  function uniqueProviderBillingDays(days) {
    var seen = {};
    var out = [];
    (days || []).forEach(function(day) {
      var n = Number(day);
      if (!isFinite(n) || n < 0 || n > 6 || n !== Math.round(n) || seen[n]) return;
      seen[n] = true;
      out.push(n);
    });
    return out;
  }
  function normalizeProviderBillingDays(days) {
    var raw = days || [];
    var hadDays = raw.length > 0;
    days = uniqueProviderBillingDays(raw);
    if (hadDays && !days.length) return null;
    return days.length === 7 ? [] : days;
  }
  function providerBillingDaysAreWeekdays(days) {
    var sorted = uniqueProviderBillingDays(days).slice().sort();
    return sorted.length === 5 && sorted.join(',') === '1,2,3,4,5';
  }
  function normalizeProviderBillingClock(value) {
    var match = /^(\d{1,2}):(\d{2})/.exec(String(value || '').trim());
    if (!match) return String(value || '').trim();
    var hour = Number(match[1]);
    var minute = Number(match[2]);
    if (!isFinite(hour) || !isFinite(minute) || hour !== Math.round(hour) || minute !== Math.round(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) {
      return String(value || '').trim();
    }
    return (hour < 10 ? '0' : '') + hour + ':' + (minute < 10 ? '0' : '') + minute;
  }
  function normalizeProviderMultiplierValue(value) {
    var n = Number(value);
    if (!isFinite(n) || n <= 0) return 1;
    return Math.round(n * 10000) / 10000;
  }
  function formatProviderMultiplier(value) {
    return '\u00d7' + String(normalizeProviderMultiplierValue(value));
  }
  function parseProviderBillingMinutes(value) {
    var clock = normalizeProviderBillingClock(value);
    var match = /^(\d{2}):(\d{2})$/.exec(clock);
    if (!match) return -1;
    return Number(match[1]) * 60 + Number(match[2]);
  }
  function providerBillingWeekdayMatches(days, weekday) {
    days = uniqueProviderBillingDays(days);
    if (!days.length) return true;
    return days.indexOf(weekday) >= 0;
  }
  function providerPriceSummary(p) {
    var tp = p && p.token_pricing || {};
    var input = tp.input_credits_per_10k;
    var output = tp.output_credits_per_10k;
    if (input === undefined && output === undefined) return '';
    return 'In ' + (input === undefined ? '-' : input) + ' / Out ' + (output === undefined ? '-' : output) + ' Credits/10k';
  }
  function providerPriceScheduleCount(p) {
    var windows = p && p.token_pricing && p.token_pricing.price_schedule;
    return Array.isArray(windows) ? windows.length : 0;
  }
  function providerBillingWindowMatches(window, weekday, minutes) {
    var start = parseProviderBillingMinutes(window && window.start);
    var end = parseProviderBillingMinutes(window && window.end);
    if (start < 0 || end < 0 || start === end) return false;
    if (start < end) return providerBillingWeekdayMatches(window.days, weekday) && minutes >= start && minutes < end;
    if (minutes >= start) return providerBillingWeekdayMatches(window.days, weekday);
    if (minutes < end) return providerBillingWeekdayMatches(window.days, weekday) || providerBillingWeekdayMatches(window.days, (weekday + 6) % 7);
    return false;
  }
  var providerClockFormatterCache = {};
  function providerBillingNowParts(timezone) {
    timezone = String(timezone || 'Asia/Shanghai').trim() || 'Asia/Shanghai';
    var now = new Date();
    function read(tz) {
      // Constructing Intl.DateTimeFormat dominates this helper; cache one
      // formatter per timezone since it is stateless between formatToParts.
      var formatter = providerClockFormatterCache[tz];
      if (!formatter) {
        formatter = new Intl.DateTimeFormat('en-US', {
          timeZone: tz, weekday: 'short', hour: '2-digit', minute: '2-digit', hour12: false, hourCycle: 'h23'
        });
        providerClockFormatterCache[tz] = formatter;
      }
      var parts = formatter.formatToParts(now);
      var map = {};
      parts.forEach(function(part) { map[part.type] = part.value; });
      var weekday = { Sun:0,Sunday:0,Mon:1,Monday:1,Tue:2,Tues:2,Tuesday:2,Wed:3,Wednesday:3,Thu:4,Thur:4,Thurs:4,Thursday:4,Fri:5,Friday:5,Sat:6,Saturday:6 }[String(map.weekday||'').replace(/\.$/, '')];
      var hour = Number(map.hour);
      var minute = Number(map.minute);
      if (map.dayPeriod) {
        var pm = /^p/i.test(map.dayPeriod);
        if (hour === 12) hour = pm ? 12 : 0;
        else if (hour < 12 && pm) hour += 12;
      }
      if (hour === 24) hour = 0;
      if (weekday == null || !isFinite(hour) || !isFinite(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) throw new Error('tz');
      return { weekday: weekday, minutes: hour * 60 + minute };
    }
    try { return read(timezone); }
    catch (e1) {
      try { if (timezone !== 'Asia/Shanghai') return read('Asia/Shanghai'); } catch (e2) {}
      return { weekday: now.getDay(), minutes: now.getHours() * 60 + now.getMinutes() };
    }
  }
  function resolveProviderBillingMultiplier(policy) {
    var parts = providerBillingNowParts(policy && policy.timezone);
    var windows = (policy && policy.credit_multiplier_schedule) || [];
    for (var i = 0; i < windows.length; i++) {
      if (providerBillingWindowMatches(windows[i], parts.weekday, parts.minutes)) {
        return normalizeProviderMultiplierValue(windows[i].multiplier);
      }
    }
    return normalizeProviderMultiplierValue(policy && policy.credit_multiplier);
  }
  function refreshProviderBillingNow() {
    var el = document.getElementById('llmPrvBillingNow');
    if (!el) return;
    el.textContent = t('billingCurrent') + ' ' + formatProviderMultiplier(resolveProviderBillingMultiplier(readProviderBilling()));
  }
  window.refreshProviderBillingNow = refreshProviderBillingNow;
  function startProviderBillingNowClock() {
    if (providerBillingNowTimer) clearInterval(providerBillingNowTimer);
    providerBillingNowTimer = setInterval(refreshProviderBillingNow, 30000);
  }
  function stopProviderBillingNowClock() {
    if (!providerBillingNowTimer) return;
    clearInterval(providerBillingNowTimer);
    providerBillingNowTimer = 0;
  }
  function providerBillingWindowInvalid(item) {
    var start = normalizeProviderBillingClock(item && item.start);
    var end = normalizeProviderBillingClock(item && item.end);
    return !start || !end || start === end;
  }
  function normalizeProviderBillingWindow(w) {
    var days = normalizeProviderBillingDays(w && w.days);
    if (days == null) return null;
    return {
      days: days,
      start: normalizeProviderBillingClock(w && w.start),
      end: normalizeProviderBillingClock(w && w.end),
      multiplier: Number(w && w.multiplier || 1) || 1
    };
  }
  function cloneProviderBillingSchedule(windows) {
    return (windows || []).map(normalizeProviderBillingWindow).filter(Boolean).map(function(next) {
      if (!next.start) next.start = '00:00';
      if (!next.end) next.end = '08:00';
      return next;
    });
  }
  function providerBillingWindowsHTML() {
    if (!(providerBillingSchedule || []).length) {
      return '<div class="provider-billing-empty">' + esc(t('billingEmpty')) + '</div>';
    }
    return providerBillingSchedule.map(providerBillingWindowHTML).join('');
  }
  function focusProviderBillingControl(id) {
    var node = document.getElementById(id);
    if (node && typeof node.focus === 'function') node.focus();
  }
  function providerBillingWindowHTML(item, index) {
    var days = uniqueProviderBillingDays(item && item.days);
    var everyday = !days.length;
    var weekdays = providerBillingDaysAreWeekdays(days);
    var chips = providerBillingWeekdayKeys.map(function(key, day) {
      var on = everyday || days.indexOf(day) >= 0;
      return '<button type="button" id="llmPrvBillDay' + index + '_' + day + '" class="provider-day-chip' + (on ? ' is-active' : '') + '" aria-pressed="' + (on ? 'true' : 'false') + '" onclick="toggleProviderBillingDay(' + index + ',' + day + ')">' + esc(t(key)) + '</button>';
    }).join('');
    return '<div class="provider-billing-window' + (providerBillingWindowInvalid(item) ? ' is-invalid' : '') + '">'
      + '<div class="provider-billing-presets">'
      + '<button type="button" id="llmPrvBillPreset' + index + '_everyday" class="provider-preset-chip' + (everyday ? ' is-active' : '') + '" aria-pressed="' + (everyday ? 'true' : 'false') + '" onclick="setProviderBillingPreset(' + index + ',\'everyday\')">' + esc(t('billingEveryday')) + '</button>'
      + '<button type="button" id="llmPrvBillPreset' + index + '_weekdays" class="provider-preset-chip' + (weekdays ? ' is-active' : '') + '" aria-pressed="' + (weekdays ? 'true' : 'false') + '" onclick="setProviderBillingPreset(' + index + ',\'weekdays\')">' + esc(t('billingWeekdays')) + '</button>'
      + '</div>'
      + '<div class="provider-billing-days">' + chips + '</div>'
      + '<div class="provider-billing-times">'
      + '<div><label for="llmPrvBillStart' + index + '">' + esc(t('billingStart')) + '</label><input id="llmPrvBillStart' + index + '" type="time" value="' + esc((item && item.start) || '00:00') + '" oninput="setProviderBillingField(' + index + ',\'start\',this.value)" onchange="setProviderBillingField(' + index + ',\'start\',this.value)"></div>'
      + '<div><label for="llmPrvBillEnd' + index + '">' + esc(t('billingEnd')) + '</label><input id="llmPrvBillEnd' + index + '" type="time" value="' + esc((item && item.end) || '08:00') + '" oninput="setProviderBillingField(' + index + ',\'end\',this.value)" onchange="setProviderBillingField(' + index + ',\'end\',this.value)"></div>'
      + '<div><label for="llmPrvBillMult' + index + '">' + esc(t('billingWindowMultiplier')) + '</label><input id="llmPrvBillMult' + index + '" type="number" min="0.01" step="0.05" value="' + esc(String((item && item.multiplier) || 1)) + '" oninput="setProviderBillingField(' + index + ',\'multiplier\',this.value)" onchange="setProviderBillingField(' + index + ',\'multiplier\',this.value)"></div>'
      + '<button class="btn-ghost" type="button" id="llmPrvBillRemove' + index + '" onclick="removeProviderBillingWindow(' + index + ')">' + esc(t('billingRemoveWindow')) + '</button>'
      + '</div></div>';
  }
  function renderProviderBillingWindows() {
    var el = document.getElementById('llmPrvBillingWindows');
    if (!el) return;
    el.innerHTML = providerBillingWindowsHTML();
  }
  function readProviderBilling() {
    var multiplier = Number(val('llmPrvMultiplier'));
    if (!isFinite(multiplier) || multiplier <= 0) multiplier = 1;
    return {
      timezone: val('llmPrvTimezone') || 'Asia/Shanghai',
      credit_multiplier: multiplier,
      credit_multiplier_schedule: (providerBillingSchedule || []).map(normalizeProviderBillingWindow).filter(function(w) {
        return w && w.start && w.end && w.start !== w.end;
      })
    };
  }
  function copyProviderExtraFields(src) {
    if (!src) return {};
    var out = {};
    ['wire_api','resolution_tier','max_queue_waiters','queue_timeout_ms','circuit_breaker_threshold','circuit_breaker_cooldown_ms','failure_backoff_base_ms','failure_backoff_max_ms'].forEach(function(k) {
      if (src[k] != null && src[k] !== '') out[k] = src[k];
    });
    return out;
  }
  function providerHasBillingWindows(p) {
    return !!(p && p.credit_multiplier_schedule && p.credit_multiplier_schedule.length);
  }
  function providerBillingBadge(p) {
    if (!p) return '';
    var current = Number(p.current_multiplier || p.credit_multiplier || 1);
    if ((!isFinite(current) || current === 1) && !providerHasBillingWindows(p)) return '';
    return '<span class="badge">' + esc(formatProviderMultiplier(current)) + '</span>';
  }
  function providerTokenPricingValue(p, key) {
    var tp = p && p.token_pricing;
    return tp && tp[key] !== undefined && tp[key] !== null ? String(tp[key]) : '';
  }
  function providerTokenPriceDays(days) {
    return normalizeProviderBillingDays(days || []);
  }
  function normalizeProviderTokenPriceWindow(window, index) {
    var days = providerTokenPriceDays(window && window.days);
    if (days == null) return null;
    var out = {
      id: String(window && window.id || 'price-' + (index + 1)).trim(),
      days: days,
      start: normalizeProviderBillingClock(window && window.start),
      end: normalizeProviderBillingClock(window && window.end)
    };
    ['input_credits_per_10k','output_credits_per_10k','cache_read_credits_per_10k','cache_write_credits_per_10k','input_rmb_per_10k','output_rmb_per_10k','cache_read_rmb_per_10k','cache_write_rmb_per_10k','minimum_request_credits'].forEach(function(key) {
      var value = window && window[key];
      if (value === '' || value === undefined || value === null) return;
      var number = Number(value);
      if (isFinite(number) && number >= 0) out[key] = number;
      else out[key] = value;
    });
    return out;
  }
  function cloneProviderTokenPricingSchedule(windows) {
    return (windows || []).map(normalizeProviderTokenPriceWindow).filter(Boolean).map(function(window) {
      if (!window.start) window.start = '00:00';
      if (!window.end) window.end = '08:00';
      return window;
    });
  }
  function providerTokenPriceWindowInvalid(window) {
    if (!window || !String(window.id || '').trim()) return true;
    var start = normalizeProviderBillingClock(window.start);
    var end = normalizeProviderBillingClock(window.end);
    if (!start || !end || start === end) return true;
    var hasPrice = false;
    var invalidPrice = false;
    ['input_credits_per_10k','output_credits_per_10k','cache_read_credits_per_10k','cache_write_credits_per_10k','input_rmb_per_10k','output_rmb_per_10k','cache_read_rmb_per_10k','cache_write_rmb_per_10k','minimum_request_credits'].forEach(function(key) {
      if (window[key] === undefined || window[key] === null || window[key] === '') return;
      var number = Number(window[key]);
      if (isFinite(number) && number >= 0) hasPrice = true;
      else invalidPrice = true;
    });
    return invalidPrice || !hasPrice;
  }
  function providerTokenPricingWindowsHTML() {
    if (!providerTokenPricingSchedule.length) return '<div class="provider-billing-empty">' + esc(t('billingEmpty')) + '</div>';
    return providerTokenPricingSchedule.map(function(window, index) {
      var days = uniqueProviderBillingDays(window.days);
      var everyday = !days.length;
      var weekdays = providerBillingDaysAreWeekdays(days);
      var chips = providerBillingWeekdayKeys.map(function(key, day) {
        var on = everyday || days.indexOf(day) >= 0;
        return '<button type="button" class="provider-day-chip' + (on ? ' is-active' : '') + '" aria-pressed="' + (on ? 'true' : 'false') + '" onclick="toggleProviderTokenPriceDay(' + index + ',' + day + ')">' + esc(t(key)) + '</button>';
      }).join('');
      function numberField(key, label) {
        var value = window[key] === undefined ? '' : String(window[key]);
        return '<div><label for="llmPrvPrice' + index + key + '">' + esc(label) + '</label><input id="llmPrvPrice' + index + key + '" type="number" min="0" step="0.01" value="' + esc(value) + '" oninput="setProviderTokenPriceField(' + index + ',' + jsArg(key) + ',this.value)"></div>';
      }
      return '<div class="provider-billing-window provider-token-price-window' + (providerTokenPriceWindowInvalid(window) ? ' is-invalid' : '') + '">'
        + '<div class="provider-billing-presets"><button type="button" class="provider-preset-chip' + (everyday ? ' is-active' : '') + '" onclick="setProviderTokenPricePreset(' + index + ',\'everyday\')">' + esc(t('billingEveryday')) + '</button>'
        + '<button type="button" class="provider-preset-chip' + (weekdays ? ' is-active' : '') + '" onclick="setProviderTokenPricePreset(' + index + ',\'weekdays\')">' + esc(t('billingWeekdays')) + '</button></div>'
        + '<div class="provider-billing-days">' + chips + '</div>'
        + '<div class="provider-billing-times"><div><label>' + esc(t('billingStart')) + '</label><input type="time" value="' + esc(window.start || '00:00') + '" oninput="setProviderTokenPriceField(' + index + ',\'start\',this.value)"></div>'
        + '<div><label>' + esc(t('billingEnd')) + '</label><input type="time" value="' + esc(window.end || '08:00') + '" oninput="setProviderTokenPriceField(' + index + ',\'end\',this.value)"></div>'
        + '<div><label>Window ID</label><input value="' + esc(window.id || '') + '" oninput="setProviderTokenPriceField(' + index + ',\'id\',this.value)"></div>'
        + '<button class="btn-ghost" type="button" onclick="removeProviderTokenPriceWindow(' + index + ')">' + esc(t('pricingRemoveWindow')) + '</button></div>'
        + '<div class="provider-billing-fields">'
        + numberField('input_credits_per_10k', t('fieldInputCredits'))
        + numberField('output_credits_per_10k', t('fieldOutputCredits'))
        + numberField('cache_read_credits_per_10k', t('fieldCacheReadCredits'))
        + numberField('cache_write_credits_per_10k', t('fieldCacheWriteCredits'))
        + numberField('input_rmb_per_10k', t('fieldInputRMB'))
        + numberField('output_rmb_per_10k', t('fieldOutputRMB'))
        + numberField('cache_read_rmb_per_10k', t('fieldCacheReadRMB'))
        + numberField('cache_write_rmb_per_10k', t('fieldCacheWriteRMB'))
        + numberField('minimum_request_credits', t('fieldMinimumCredits'))
        + '</div></div>';
    }).join('');
  }
  function renderProviderTokenPricingWindows() {
    var root = document.getElementById('llmPrvTokenPriceWindows');
    if (root) root.innerHTML = providerTokenPricingWindowsHTML();
  }
  function readProviderTokenPricing() {
    function num(id) {
      var raw = val(id);
      if (raw === '') return undefined;
      var n = Number(raw);
      return isFinite(n) && n >= 0 ? n : NaN;
    }
    var tp = {};
    var fields = { llmPrvTpIn: 'input_credits_per_10k', llmPrvTpOut: 'output_credits_per_10k', llmPrvTpCacheRead: 'cache_read_credits_per_10k', llmPrvTpCacheWrite: 'cache_write_credits_per_10k', llmPrvTpRmbIn: 'input_rmb_per_10k', llmPrvTpRmbOut: 'output_rmb_per_10k', llmPrvTpRmbCacheRead: 'cache_read_rmb_per_10k', llmPrvTpRmbCacheWrite: 'cache_write_rmb_per_10k', llmPrvTpMin: 'minimum_request_credits' };
    for (var id in fields) {
      if (!fields.hasOwnProperty(id)) continue;
      var n = num(id);
      if (isNaN(n)) return null;
      if (n !== undefined) tp[fields[id]] = n;
    }
    var tz = val('llmPrvTpTimezone');
    if (tz) tp.timezone = tz;
    var ver = val('llmPrvTpVersion');
    if (ver) tp.version = ver;
    var validSchedule = providerTokenPricingSchedule.map(normalizeProviderTokenPriceWindow).filter(function(window) {
      return window && !providerTokenPriceWindowInvalid(window);
    });
    if (validSchedule.length !== providerTokenPricingSchedule.length) return null;
    if (validSchedule.length) {
      tp.price_schedule = validSchedule;
      if (!tp.timezone) tp.timezone = val('llmPrvTimezone') || 'Asia/Shanghai';
    }
    return tp;
  }
  function providerTokenPricingSection(p) {
    var tp = (p && p.token_pricing) || {};
    var tz = tp.timezone || '';
    var options = providerBillingTimezoneOptions.slice();
    if (tz && options.indexOf(tz) < 0) options.unshift(tz);
    function numField(id, label, key, placeholder) {
      return '<div><label for="' + id + '">' + esc(label) + '</label><input id="' + id + '" type="number" min="0" step="0.01" value="' + esc(providerTokenPricingValue(p, key)) + '" placeholder="' + esc(placeholder) + '"></div>';
    }
    return '<div class="provider-billing provider-billing-top-compact"><div class="provider-billing-head"><div><div class="provider-billing-title"><strong>' + esc(t('tokenPricingTitle')) + '</strong></div>'
      + '<div class="provider-billing-hint">' + esc(t('tokenPricingHint')) + '</div></div></div>'
      + '<div class="provider-billing-fields">'
      + numField('llmPrvTpIn', t('fieldInputCredits'), 'input_credits_per_10k', '1')
      + numField('llmPrvTpOut', t('fieldOutputCredits'), 'output_credits_per_10k', '4')
      + numField('llmPrvTpCacheRead', t('fieldCacheReadCredits'), 'cache_read_credits_per_10k', 'input × 0.1')
      + numField('llmPrvTpCacheWrite', t('fieldCacheWriteCredits'), 'cache_write_credits_per_10k', 'input')
      + numField('llmPrvTpRmbIn', t('fieldInputRMB'), 'input_rmb_per_10k', '0.02')
      + numField('llmPrvTpRmbOut', t('fieldOutputRMB'), 'output_rmb_per_10k', '0.08')
      + numField('llmPrvTpRmbCacheRead', t('fieldCacheReadRMB'), 'cache_read_rmb_per_10k', 'input × 0.1')
      + numField('llmPrvTpRmbCacheWrite', t('fieldCacheWriteRMB'), 'cache_write_rmb_per_10k', 'input')
      + numField('llmPrvTpMin', t('fieldMinimumCredits'), 'minimum_request_credits', '0.1')
      + '<div><label for="llmPrvTpTimezone">' + esc(t('fieldPricingTimezone')) + '</label><select id="llmPrvTpTimezone"><option value="">-</option>'
      + options.map(function(v){ return '<option value="' + esc(v) + '"' + (v === tz ? ' selected' : '') + '>' + esc(v) + '</option>'; }).join('')
      + '</select></div>'
      + '<div><label for="llmPrvTpVersion">' + esc(t('fieldPricingVersion')) + '</label><input id="llmPrvTpVersion" value="' + esc(tp.version || '') + '" placeholder="2026-08-23-v1"></div>'
      + '</div><div class="provider-billing-head provider-token-price-head"><div><div class="provider-billing-title"><strong>' + esc(t('pricingSchedule')) + '</strong></div><div class="provider-billing-hint">' + esc(t('pricingWindowHint')) + '</div></div>'
      + '<button class="btn-ghost" type="button" onclick="addProviderTokenPriceWindow()">' + esc(t('pricingAddWindow')) + '</button></div><div id="llmPrvTokenPriceWindows">' + providerTokenPricingWindowsHTML() + '</div></div>';
  }
  window.addProviderTokenPriceWindow = function() {
    providerTokenPricingSchedule.push({ id: 'price-' + (providerTokenPricingSchedule.length + 1), days: [], start: '00:00', end: '08:00' });
    renderProviderTokenPricingWindows();
  };
  window.removeProviderTokenPriceWindow = function(index) {
    providerTokenPricingSchedule.splice(index, 1);
    renderProviderTokenPricingWindows();
  };
  window.setProviderTokenPricePreset = function(index, preset) {
    var window = providerTokenPricingSchedule[index];
    if (!window) return;
    window.days = preset === 'weekdays' ? [1,2,3,4,5] : [];
    renderProviderTokenPricingWindows();
  };
  window.toggleProviderTokenPriceDay = function(index, day) {
    var window = providerTokenPricingSchedule[index];
    if (!window) return;
    var days = uniqueProviderBillingDays(window.days);
    if (!days.length) days = [0,1,2,3,4,5,6];
    var position = days.indexOf(day);
    if (position >= 0) days.splice(position, 1); else days.push(day);
    window.days = normalizeProviderBillingDays(days) || [];
    renderProviderTokenPricingWindows();
  };
  window.setProviderTokenPriceField = function(index, key, value) {
    var window = providerTokenPricingSchedule[index];
    if (!window) return;
    if (key === 'id') window.id = String(value || '').trim();
    else if (key === 'start' || key === 'end') window[key] = normalizeProviderBillingClock(value);
    else if (value === '' || value == null) delete window[key];
    else {
      var number = Number(value);
      window[key] = isFinite(number) && number >= 0 ? number : value;
    }
    renderProviderTokenPricingWindows();
  };
  function providerBillingSection(p, opts) {
    var timezone = (opts && opts.timezone) || (p && p.timezone) || 'Asia/Shanghai';
    var multiplier = (opts && opts.multiplier != null && opts.multiplier !== '') ? opts.multiplier : ((p && p.credit_multiplier) || 1);
    var options = providerBillingTimezoneOptions.slice();
    if (timezone && options.indexOf(timezone) < 0) options.unshift(timezone);
    var nowRate = formatProviderMultiplier(resolveProviderBillingMultiplier({
      timezone: timezone, credit_multiplier: Number(multiplier) || 1, credit_multiplier_schedule: providerBillingSchedule
    }));
    return '<div class="provider-billing"><div class="provider-billing-head"><div><div class="provider-billing-title"><strong>' + esc(t('billingTitle')) + '</strong>'
      + '<span id="llmPrvBillingNow" class="badge info provider-billing-now">' + esc(t('billingCurrent') + ' ' + nowRate) + '</span></div>'
      + '<div class="provider-billing-hint">' + esc(t('billingHint')) + '</div></div>'
      + '<button class="btn-ghost" type="button" id="llmPrvBillAdd" onclick="addProviderBillingWindow()">' + esc(t('billingAddWindow')) + '</button></div>'
      + '<div class="provider-billing-fields">'
      + '<div><label for="llmPrvTimezone">' + esc(t('billingTimezone')) + '</label><select id="llmPrvTimezone" onchange="refreshProviderBillingNow()">'
      + options.map(function(v){ return '<option value="' + esc(v) + '"' + (v === timezone ? ' selected' : '') + '>' + esc(v) + '</option>'; }).join('')
      + '</select></div>'
      + '<div><label for="llmPrvMultiplier">' + esc(t('billingMultiplier')) + '</label><input id="llmPrvMultiplier" type="number" min="0.01" step="0.05" value="' + esc(String(multiplier)) + '" oninput="refreshProviderBillingNow()" onchange="refreshProviderBillingNow()"></div>'
      + '</div>'
      + '<div class="provider-billing-hint">' + esc(t('billingSchedule')) + ' \u2014 ' + esc(t('billingOvernight')) + '</div>'
      + '<div id="llmPrvBillingWindows">' + providerBillingWindowsHTML() + '</div></div>';
  }
  window.addProviderBillingWindow = function() {
    providerBillingSchedule.push({ days: [1,2,3,4,5], start: '00:30', end: '08:30', multiplier: 0.5 });
    renderProviderBillingWindows();
    refreshProviderBillingNow();
    focusProviderBillingControl('llmPrvBillStart' + (providerBillingSchedule.length - 1));
  };
  window.removeProviderBillingWindow = function(index) {
    providerBillingSchedule.splice(index, 1);
    renderProviderBillingWindows();
    refreshProviderBillingNow();
  };
  window.setProviderBillingPreset = function(index, preset) {
    var item = providerBillingSchedule[index];
    if (!item) return;
    item.days = preset === 'weekdays' ? [1,2,3,4,5] : [];
    renderProviderBillingWindows();
    refreshProviderBillingNow();
    focusProviderBillingControl('llmPrvBillPreset' + index + '_' + preset);
  };
  window.toggleProviderBillingDay = function(index, day) {
    var item = providerBillingSchedule[index];
    if (!item) return;
    var days = uniqueProviderBillingDays(item.days);
    if (!days.length) days = [0,1,2,3,4,5,6];
    var pos = days.indexOf(day);
    if (pos >= 0) days.splice(pos, 1);
    else days.push(day);
    item.days = normalizeProviderBillingDays(days) || [];
    renderProviderBillingWindows();
    refreshProviderBillingNow();
    focusProviderBillingControl('llmPrvBillDay' + index + '_' + day);
  };
  window.setProviderBillingField = function(index, key, value) {
    var item = providerBillingSchedule[index];
    if (!item) return;
    if (key === 'multiplier') {
      var n = Number(value);
      if (isFinite(n) && n > 0) item.multiplier = n;
    } else item[key] = normalizeProviderBillingClock(value);
    var root = document.querySelectorAll('#llmPrvBillingWindows .provider-billing-window')[index];
    if (root) root.classList.toggle('is-invalid', providerBillingWindowInvalid(item));
    refreshProviderBillingNow();
  };
  async function loadProviderAccessNodes() {
    try {
      var data = await api('/api/admin/llm/access-nodes');
      providerAccessNodes = data.nodes || [];
    } catch (e) {
      providerAccessNodes = [];
    }
    return providerAccessNodes;
  }
  function providerAccessSelectedIDs() {
    var seen = {};
    return Object.keys(providerAccessSelected).filter(function(id) {
      if (!providerAccessSelected[id]) return false;
      var key = String(id).toLowerCase();
      if (seen[key]) return false;
      seen[key] = true;
      return true;
    }).sort();
  }
  function providerAccessNodeSelected(id) {
    id = String(id || '').trim();
    if (!id) return false;
    if (providerAccessSelected[id]) return true;
    var key = id.toLowerCase();
    return Object.keys(providerAccessSelected).some(function(k) { return k.toLowerCase() === key && providerAccessSelected[k]; });
  }
  function providerAccessNodeCard(id, name, host, meta, extraClass) {
    var on = providerAccessNodeSelected(id);
    return '<button type="button" class="provider-access-node' + (on ? ' is-active' : '') + (extraClass ? ' ' + extraClass : '') + '" onclick="toggleProviderAccessNode(' + jsArg(id) + ')">'
      + '<span class="provider-access-node-name">' + esc(name || id) + '</span>'
      + (host ? '<span class="provider-access-node-host">' + esc(host) + '</span>' : '')
      + (meta ? '<span class="provider-access-node-meta">' + esc(meta) + '</span>' : '')
      + '</button>';
  }
  function parseProviderNodeList(raw) {
    var seen = {};
    return String(raw || '').split(/[,，;；\s]+/).map(function(part) { return part.trim(); }).filter(function(id) {
      if (!id) return false;
      var key = id.toLowerCase();
      if (seen[key]) return false;
      seen[key] = true;
      return true;
    });
  }
  function providerAccessNodeLabels() {
    return (providerAccessNodes || []).map(function(node) {
      var id = String(node.node_id || '').trim();
      if (!id) return '';
      var name = String(node.name || '').trim();
      if (name && name.toLowerCase() !== id.toLowerCase()) return id + ' (' + name + ')';
      return id;
    }).filter(Boolean);
  }
  function providerAccessScopeSection() {
    var modes = '<div class="provider-access-switch" role="radiogroup" aria-label="' + esc(t('accessScope')) + '">'
      + '<button type="button" class="provider-access-switch-btn' + (providerAccessMode !== 'nodes' ? ' is-active' : '') + '" onclick="setProviderAccessMode(\'all\')">' + esc(t('accessScopeAll')) + '</button>'
      + '<button type="button" class="provider-access-switch-btn' + (providerAccessMode === 'nodes' ? ' is-active' : '') + '" onclick="setProviderAccessMode(\'nodes\')">' + esc(t('accessScopeSelected')) + '</button>'
      + '</div>';
    var labels = providerAccessNodeLabels();
    var available = '<div class="provider-billing-hint">' + esc(t('accessScopeAvailable')) + (labels.length ? esc(labels.join('、')) : esc(t('accessScopeAvailableEmpty'))) + '</div>';
    var nodes = '';
    if (providerAccessMode === 'nodes') {
      var known = {};
      nodes = '<div class="provider-access-nodes">' + (providerAccessNodes || []).map(function(node) {
        var id = String(node.node_id || '').trim();
        if (!id) return '';
        known[id.toLowerCase()] = true;
        var meta = node.self ? t('accessScopeSelf') : (!node.reachable ? t('accessScopeUnreachable') : '');
        var label = node.name && String(node.name).toLowerCase() !== id.toLowerCase() ? id + ' · ' + node.name : id;
        return providerAccessNodeCard(id, label, node.host || '', meta, node.reachable || node.self ? '' : 'is-offline');
      }).join('') + providerAccessSelectedIDs().filter(function(id){ return !known[String(id).toLowerCase()]; }).map(function(id) {
        return providerAccessNodeCard(id, id, '', t('accessScopeOffline'), 'is-offline provider-access-stale');
      }).join('') + '</div>'
        + '<label for="llmPrvNodes">' + esc(t('accessScopeNodesInput')) + '</label>'
        + '<input id="llmPrvNodes" type="text" value="' + esc(providerAccessSelectedIDs().join(', ')) + '" placeholder="' + esc(t('accessScopeNodesPlaceholder')) + '" onchange="setProviderAccessNodesText(this.value)">';
    }
    return '<div class="provider-access-scope"><div class="provider-billing-title"><strong>' + esc(t('accessScope')) + '</strong></div>'
      + '<div class="provider-billing-hint">' + esc(t('accessScopeHint')) + '</div>'
      + available + modes + nodes + '</div>';
  }
  window.setProviderAccessNodesText = function(raw) {
    providerAccessMode = 'nodes';
    providerAccessSelected = {};
    parseProviderNodeList(raw).forEach(function(id) { providerAccessSelected[id] = true; });
    var root = document.querySelector('.provider-access-scope');
    if (root) root.outerHTML = providerAccessScopeSection();
  };
  window.setProviderAccessMode = function(mode) {
    providerAccessMode = mode === 'nodes' ? 'nodes' : 'all';
    if (providerAccessMode === 'nodes' && !providerAccessSelectedIDs().length) {
      (providerAccessNodes || []).forEach(function(node) {
        var id = String(node.node_id || '').trim();
        if (id) providerAccessSelected[id] = true;
      });
    }
    var root = document.querySelector('.provider-access-scope');
    if (root) root.outerHTML = providerAccessScopeSection();
  };
  window.toggleProviderAccessNode = function(id) {
    id = String(id || '').trim();
    if (!id) return;
    var key = id.toLowerCase();
    var matched = Object.keys(providerAccessSelected).filter(function(k) {
      return k.toLowerCase() === key && providerAccessSelected[k];
    });
    if (matched.length) matched.forEach(function(k) { delete providerAccessSelected[k]; });
    else providerAccessSelected[id] = true;
    var root = document.querySelector('.provider-access-scope');
    if (root) root.outerHTML = providerAccessScopeSection();
  };
  function providerAccessBadge(p) {
    var ids = (p && p.allowed_node_ids) || [];
    if (!ids.length) return '';
    return '<span class="badge">' + esc(t('accessScopeSelected') + ': ' + ids.join(', ')) + '</span>';
  }
  function providerServeWindowAllDay(item) {
    return (item && item.start) === '00:00' && (item && item.end) === '24:00';
  }
  function providerServeDaysInvalid(days) {
    if (!Array.isArray(days)) return true;
    var seen = {};
    for (var i = 0; i < days.length; i++) {
      var n = Number(days[i]);
      if (!isFinite(n) || n < 0 || n > 6 || n !== Math.round(n) || seen[n]) return true;
      seen[n] = true;
    }
    return false;
  }
  function normalizeProviderServeWindow(item) {
    var days = normalizeProviderBillingDays(item && item.days);
    if (days == null) return null;
    return {
      days: days,
      start: normalizeProviderBillingClock(item && item.start) || '00:00',
      end: normalizeProviderBillingClock(item && item.end) || '24:00'
    };
  }
  function cloneProviderServeWindows(windows) {
    // Broken rows stay visible and block the save: silently dropping them
    // would widen availability beyond the hours that were stored.
    return (windows || []).map(function(w) {
      return {
        days: (w && Array.isArray(w.days)) ? w.days.slice() : [],
        start: normalizeProviderBillingClock(w && w.start),
        end: normalizeProviderBillingClock(w && w.end)
      };
    }).filter(function(w) { return w.days.length || w.start || w.end; });
  }
  function providerServeWindowInvalid(item) {
    var start = parseProviderBillingMinutes(item && item.start);
    var end = parseProviderBillingMinutes(item && item.end);
    if (start < 0 || end < 0 || start === end) return true;
    return providerServeDaysInvalid(item && item.days);
  }
  function providerServeWindowsHTML() {
    if (!(providerServeWindowSchedule || []).length) {
      return '<div class="provider-billing-empty">' + esc(t('serveEmpty')) + '</div>';
    }
    return providerServeWindowSchedule.map(providerServeWindowHTML).join('');
  }
  function providerServeWindowHTML(item, index) {
    var days = uniqueProviderBillingDays(item && item.days);
    var everyday = !days.length;
    var weekdays = providerBillingDaysAreWeekdays(days);
    var allDay = providerServeWindowAllDay(item);
    var chips = providerServeWeekdayOrder.map(function(day, position) {
      var on = everyday || days.indexOf(day) >= 0;
      return '<button type="button" id="llmPrvServeDay' + index + '_' + day + '" class="provider-day-chip' + (on ? ' is-active' : '') + '" aria-pressed="' + (on ? 'true' : 'false') + '" onclick="toggleProviderServeDay(' + index + ',' + day + ')">' + esc(t(providerServeWeekdayKeys[position])) + '</button>';
    }).join('');
    function timeField(key) {
      var id = 'llmPrvServe' + (key === 'start' ? 'Start' : 'End') + index;
      var isEnd24 = key === 'end' && item && item.end === '24:00';
      // A native time input cannot hold 24:00, so the end of the day renders
      // as an editable text field instead.
      var input = isEnd24
        ? '<input id="' + id + '" type="text" inputmode="numeric" value="24:00"' + (allDay ? ' disabled' : '')
          + ' oninput="setProviderServeField(' + index + ',\'end\',this.value)" onchange="setProviderServeField(' + index + ',\'end\',this.value)">'
        : '<input id="' + id + '" type="time" value="' + esc((item && item[key]) || (key === 'start' ? '00:00' : '23:59')) + '"' + (allDay ? ' disabled' : '')
          + ' oninput="setProviderServeField(' + index + ',\'' + key + '\',this.value)" onchange="setProviderServeField(' + index + ',\'' + key + '\',this.value)">';
      return '<div><label for="' + id + '">' + esc(t(key === 'start' ? 'billingStart' : 'billingEnd')) + '</label>' + input + '</div>';
    }
    return '<div class="provider-billing-window' + (providerServeWindowInvalid(item) ? ' is-invalid' : '') + '">'
      + '<div class="provider-billing-presets">'
      + '<button type="button" id="llmPrvServePreset' + index + '_everyday" class="provider-preset-chip' + (everyday ? ' is-active' : '') + '" aria-pressed="' + (everyday ? 'true' : 'false') + '" onclick="setProviderServePreset(' + index + ',\'everyday\')">' + esc(t('serveEveryday')) + '</button>'
      + '<button type="button" id="llmPrvServePreset' + index + '_weekdays" class="provider-preset-chip' + (weekdays ? ' is-active' : '') + '" aria-pressed="' + (weekdays ? 'true' : 'false') + '" onclick="setProviderServePreset(' + index + ',\'weekdays\')">' + esc(t('serveWeekdays')) + '</button>'
      + '</div>'
      + '<div class="provider-billing-days">' + chips + '</div>'
      + '<div class="provider-billing-times provider-serve-times">'
      + '<div class="provider-serve-allday"><label for="llmPrvServeAllDay' + index + '">' + esc(t('serveAllDay')) + '</label>'
      + '<input id="llmPrvServeAllDay' + index + '" type="checkbox"' + (allDay ? ' checked' : '') + ' onchange="setProviderServeAllDay(' + index + ',this.checked)"></div>'
      + timeField('start') + timeField('end')
      + '<button class="btn-ghost" type="button" id="llmPrvServeRemove' + index + '" onclick="removeProviderServeWindow(' + index + ')">' + esc(t('serveRemoveWindow')) + '</button>'
      + '</div></div>';
  }
  function renderProviderServeWindows() {
    var el = document.getElementById('llmPrvServeWindows');
    if (!el) return;
    el.innerHTML = providerServeWindowsHTML();
  }
  function providerServeSection() {
    // Not provider-access-scope: the access handlers replace the first
    // element with that class, so the serve box must not match it.
    return '<div class="provider-serve-scope"><div class="provider-billing-title"><strong>' + esc(t('serveTitle')) + '</strong></div>'
      + '<div class="provider-billing-hint">' + esc(t('serveHint')) + '</div>'
      + '<div class="provider-billing-head"><span></span><button class="btn-ghost" type="button" onclick="addProviderServeWindow()">' + esc(t('serveAddWindow')) + '</button></div>'
      + '<div id="llmPrvServeWindows">' + providerServeWindowsHTML() + '</div></div>';
  }
  window.addProviderServeWindow = function() {
    providerServeWindowSchedule.push({ days: [1, 2, 3, 4, 5], start: '00:00', end: '24:00' });
    renderProviderServeWindows();
  };
  window.removeProviderServeWindow = function(index) {
    providerServeWindowSchedule.splice(index, 1);
    renderProviderServeWindows();
  };
  window.setProviderServePreset = function(index, preset) {
    var item = providerServeWindowSchedule[index];
    if (!item) return;
    item.days = preset === 'weekdays' ? [1, 2, 3, 4, 5] : [];
    renderProviderServeWindows();
    focusProviderBillingControl('llmPrvServePreset' + index + '_' + preset);
  };
  window.toggleProviderServeDay = function(index, day) {
    var item = providerServeWindowSchedule[index];
    if (!item) return;
    var days = uniqueProviderBillingDays(item.days);
    if (!days.length) days = [0, 1, 2, 3, 4, 5, 6];
    var pos = days.indexOf(day);
    if (pos >= 0) days.splice(pos, 1);
    else days.push(day);
    item.days = normalizeProviderBillingDays(days) || [];
    renderProviderServeWindows();
    focusProviderBillingControl('llmPrvServeDay' + index + '_' + day);
  };
  window.setProviderServeField = function(index, key, value) {
    var item = providerServeWindowSchedule[index];
    if (!item) return;
    item[key] = normalizeProviderBillingClock(value);
    var root = document.querySelectorAll('#llmPrvServeWindows .provider-billing-window')[index];
    if (root) root.classList.toggle('is-invalid', providerServeWindowInvalid(item));
  };
  window.setProviderServeAllDay = function(index, on) {
    var item = providerServeWindowSchedule[index];
    if (!item) return;
    if (on) {
      item.start = '00:00';
      item.end = '24:00';
    } else if (providerServeWindowAllDay(item)) {
      item.start = '09:00';
      item.end = '18:00';
    }
    renderProviderServeWindows();
  };
  function readProviderServeWindows() {
    return (providerServeWindowSchedule || []).map(normalizeProviderServeWindow).filter(Boolean).filter(function(w) {
      return !providerServeWindowInvalid(w);
    });
  }
  function providerServeWindowsDropped() {
    return readProviderServeWindows().length !== (providerServeWindowSchedule || []).length;
  }
  function providerHasServeWindows(p) {
    return !!(p && Array.isArray(p.serve_windows) && p.serve_windows.length);
  }
  function providerServeSummary(p) {
    return (p && p.serve_windows || []).map(function(w) {
      var days = uniqueProviderBillingDays(w.days);
      var dayLabel = days.length ? days.map(function(d) { return t(providerBillingWeekdayKeys[d]); }).join('/') : t('billingEveryday');
      return dayLabel + ' ' + (w.start || '00:00') + '\u2013' + (w.end || '24:00');
    }).join('; ');
  }
  function providerServeBadge(p) {
    if (!providerHasServeWindows(p)) return '';
    // The dial gate always evaluates in Asia/Shanghai on the backend, so the
    // badge must not follow the provider's billing timezone.
    var parts = providerBillingNowParts('Asia/Shanghai');
    var open = (p.serve_windows || []).some(function(w) { return providerBillingWindowMatches(w, parts.weekday, parts.minutes); });
    var title = esc(t('serveWindowBadge') + ': ' + providerServeSummary(p));
    var cls = 'badge provider-serve-badge' + (open ? ' info' : ' warn');
    var text = open ? t('serveWindowBadge') : t('serveWindowBadge') + ' \u00b7 ' + t('serveClosedNow');
    return '<span class="' + cls + '" data-provider-id="' + esc(p.id) + '" title="' + title + '">' + esc(text) + '</span>';
  }
  var providerServeBadgeTimer = 0;
  function refreshProviderServeBadges() {
    // The list rows re-render only on data loads; flip the badges in place so
    // a tab left open does not advertise a window that closed an hour ago.
    var root = document.getElementById('llmProvidersList');
    if (!root) return;
    var badges = root.querySelectorAll('.provider-serve-badge');
    if (!badges.length) return;
    var parts = providerBillingNowParts('Asia/Shanghai');
    badges.forEach(function(el) {
      var id = el.getAttribute('data-provider-id') || '';
      var provider = null;
      for (var i = 0; i < providers.length; i++) {
        if (id && providers[i] && providers[i].id === id) { provider = providers[i]; break; }
      }
      if (!providerHasServeWindows(provider)) return;
      var open = (provider.serve_windows || []).some(function(w) { return providerBillingWindowMatches(w, parts.weekday, parts.minutes); });
      var title = esc(t('serveWindowBadge') + ': ' + providerServeSummary(provider));
      el.className = 'badge provider-serve-badge' + (open ? ' info' : ' warn');
      el.textContent = open ? t('serveWindowBadge') : t('serveWindowBadge') + ' \u00b7 ' + t('serveClosedNow');
      el.setAttribute('title', title);
    });
  }
  function startProviderServeBadgeClock() {
    if (providerServeBadgeTimer) return;
    providerServeBadgeTimer = setInterval(function() {
      if (document.visibilityState === 'hidden') return;
      refreshProviderServeBadges();
    }, 60000);
  }
  window.showProviderDialog = async function(mode, id, opts) {
    var seq = ++providerDialogSeq;
    var p = mode === 'edit' ? providers.find(function(x){return x.id===id;}) : null;
    providerDialogID = mode === 'edit' ? (id || '') : '';
    stopWorkBuddyLoginPoll();
    workBuddySessionID = '';
    workBuddyModels = [];
    sgOpenKind = 'provider';
    if (!(opts && opts.keepBilling)) {
      providerBillingSchedule = cloneProviderBillingSchedule(p && p.credit_multiplier_schedule);
      providerTokenPricingSchedule = cloneProviderTokenPricingSchedule(p && p.token_pricing && p.token_pricing.price_schedule);
      providerServeWindowSchedule = cloneProviderServeWindows(p && p.serve_windows);
      var savedIDs = (p && p.allowed_node_ids) || [];
      providerAccessMode = savedIDs.length ? 'nodes' : 'all';
      providerAccessSelected = {};
      savedIDs.forEach(function(nodeID){ if (nodeID) providerAccessSelected[String(nodeID)] = true; });
    }
    await loadProviderAccessNodes();
    if (seq !== providerDialogSeq) return false;
    var title = mode === 'edit' ? t('providerDialogTitleEdit') : t('providerDialogTitleNew');
    var homeArray = p ? providerArrayByID(p.array_id || p.id) : null;
    providerDialogHomeArray = homeArray ? homeArray.id : (p ? (p.array_id || p.id) : '');
    var arrayChoiceSet = !!(opts && Object.prototype.hasOwnProperty.call(opts, 'arrayID'));
    var selectedArrayID = '';
    if (arrayChoiceSet) {
      var chosen = providerArrayByID(opts.arrayID);
      selectedArrayID = chosen ? chosen.id : String(opts.arrayID || '').trim();
    } else if (homeArray && homeArray.members.length > 1) {
      selectedArrayID = homeArray.id;
    }
    var html = sgDialogChrome(title,
      '<div class="sg-form-grid">'
      + field('llmPrvID', t('fieldID'), p ? p.id : '', mode==='edit')
      + field('llmPrvName', t('fieldName'), p ? p.name : '')
      + providerAuthField(p)
      + field('llmPrvURL', t('fieldURL'), p ? p.api_url : '')
      + field('llmPrvKey', t('fieldKey'), '', false, 'password')
      + '<div><label>' + esc(t('fieldProtocol')) + '</label><select id="llmPrvProtocol"><option value="openai"' + ((!p||p.protocol==='openai')?' selected':'') + '>OpenAI</option><option value="anthropic"' + (p&&p.protocol==='anthropic'?' selected':'') + '>Anthropic</option></select></div>'
      + providerModelsField(p ? (p.models||[]).join(', ') : '')
      + providerCapabilitiesField(p ? (p.capability_tags||[]).join(', ') : '')
      + field('llmPrvPriority', t('fieldPriority'), p ? String(p.priority||0) : '0', false, 'number')
      + '<div><label for="llmPrvSequence">' + esc(t('fieldSequence')) + '</label><input id="llmPrvSequence" type="number" value="' + esc(p ? String(p.sequence||0) : '0') + '"></div>'
      + field('llmPrvConc', t('fieldConcurrency'), p ? String(p.max_concurrency||10) : '10', false, 'number')
      + field('llmPrvTimeout', t('fieldTimeout'), p ? String(p.upstream_timeout_sec||900) : '900', false, 'number')
      + '</div><div class="hint">' + esc(t('sequenceHint')) + '</div>'
      + providerArraySection(p, selectedArrayID)
      + providerAccessScopeSection()
      + providerServeSection()
      + '<div id="llmPrvBillingNote" class="hint">' + esc(t('providerArrayBillingOnEdit')) + '</div>',
      '<button class="btn-primary" onclick="saveProvider(' + jsArg(mode==='edit'?id:'') + ')">' + esc(t('save')) + '</button>'
      + '<button class="btn-ghost" onclick="sgCloseCurrentDialog()">' + esc(t('cancel')) + '</button>');
    openDialog(html, 'sg-form-dialog');
    window.renderProviderCapabilityChips();
    window.setProviderDialogArray(selectedArrayID);
    window.onProviderAuthChange(true);
    return true;
  };
  function providerWorkBuddyBadge(p) {
    if (!p || p.auth_kind !== 'workbuddy') return '';
    var label = p.workbuddy_edition === 'china' ? t('authWorkBuddyChina') : (p.workbuddy_edition === 'global' ? t('authWorkBuddyGlobal') : 'WorkBuddy');
    return '<span class="badge">' + esc(label) + '</span>';
  }
  function providerAuthValue(p) {
    if (p && p.auth_kind === 'workbuddy' && (p.workbuddy_edition === 'china' || p.workbuddy_edition === 'global')) return p.workbuddy_edition;
    return '';
  }
  function providerAuthField(p) {
    var selected = providerAuthValue(p);
    return '<div><label for="llmPrvAuth">' + esc(t('fieldAuth')) + '</label><select id="llmPrvAuth" onchange="onProviderAuthChange()">'
      + '<option value=""' + (selected === '' ? ' selected' : '') + '>' + esc(t('authAPIKey')) + '</option>'
      + '<option value="china"' + (selected === 'china' ? ' selected' : '') + '>' + esc(t('authWorkBuddyChina')) + '</option>'
      + '<option value="global"' + (selected === 'global' ? ' selected' : '') + '>' + esc(t('authWorkBuddyGlobal')) + '</option>'
      + '</select></div>'
      + '<div id="llmPrvWorkBuddy" class="grid-span-all" hidden>'
      + '<div class="hint">' + esc(t('workBuddyHint')) + '</div>'
      + '<div class="provider-model-tools"><button class="btn-ghost provider-probe-btn" type="button" id="llmPrvWorkBuddyLogin" onclick="startWorkBuddyLogin()">' + esc(t('workBuddyLogin')) + '</button></div>'
      + '<div id="llmPrvWorkBuddyStatus" class="provider-probe-status"></div>'
      + '<div id="llmPrvWorkBuddyChoices" class="provider-model-results"></div></div>';
  }
  function stopWorkBuddyLoginPoll() {
    workBuddyPollGen += 1;
    if (workBuddyPollTimer) {
      clearTimeout(workBuddyPollTimer);
      workBuddyPollTimer = 0;
    }
  }
  window.onProviderAuthChange = function(keepIdentity) {
    var edition = val('llmPrvAuth');
    var panel = document.getElementById('llmPrvWorkBuddy');
    var key = document.getElementById('llmPrvKey');
    var url = document.getElementById('llmPrvURL');
    var probe = document.querySelector('#llmPrvModels') && document.querySelector('#llmPrvModels').parentNode.querySelector('.provider-probe-btn');
    var workbuddy = edition === 'china' || edition === 'global';
    if (panel) panel.hidden = !workbuddy;
    if (key && key.parentNode) key.parentNode.hidden = workbuddy;
    if (probe) probe.hidden = workbuddy;
    if (url) url.readOnly = workbuddy;
    if (!workbuddy) {
      stopWorkBuddyLoginPoll();
      return;
    }
    var preset = workBuddyEditions[edition];
    if (!preset) return;
    var idEl = document.getElementById('llmPrvID');
    var nameEl = document.getElementById('llmPrvName');
    if (!keepIdentity) {
      var other = edition === 'china' ? workBuddyEditions.global : workBuddyEditions.china;
      if (idEl && (!idEl.value || idEl.value === other.id)) idEl.value = preset.id;
      if (nameEl && (!nameEl.value || nameEl.value === other.name)) nameEl.value = preset.name;
    } else {
      if (idEl && !idEl.value) idEl.value = preset.id;
      if (nameEl && !nameEl.value) nameEl.value = preset.name;
    }
    if (url) url.value = preset.url;
    var protocol = document.getElementById('llmPrvProtocol');
    if (protocol) protocol.value = 'openai';
  };
  function renderWorkBuddyModelChoices() {
    var choices = document.getElementById('llmPrvWorkBuddyChoices');
    if (!choices) return;
    var selected = csvValues('llmPrvModels');
    choices.innerHTML = (workBuddyModels || []).map(function(model) {
      var id = model && model.id;
      if (!id) return '';
      var on = selected.indexOf(id) >= 0;
      var label = model.name && model.name !== id ? model.name + ' (' + id + ')' : id;
      return '<button type="button" class="provider-cap-chip' + (on ? ' is-active' : '') + '" onclick="toggleWorkBuddyModel(' + jsArg(id) + ')">' + esc(label) + '</button>';
    }).join('');
  }
  window.toggleWorkBuddyModel = function(id) {
    var values = csvValues('llmPrvModels');
    var idx = values.indexOf(id);
    if (idx >= 0) values.splice(idx, 1); else values.push(id);
    setCSVValues('llmPrvModels', values);
    renderWorkBuddyModelChoices();
  };
  window.startWorkBuddyLogin = async function() {
    var edition = val('llmPrvAuth');
    if (edition !== 'china' && edition !== 'global') return;
    var status = document.getElementById('llmPrvWorkBuddyStatus');
    var button = document.getElementById('llmPrvWorkBuddyLogin');
    stopWorkBuddyLoginPoll();
    var gen = workBuddyPollGen;
    if (workBuddySessionID) {
      api('/api/admin/llm/providers/workbuddy/login/' + encodeURIComponent(workBuddySessionID), { method: 'DELETE' }).catch(function(){});
      workBuddySessionID = '';
    }
    workBuddyModels = [];
    renderWorkBuddyModelChoices();
    if (status) status.textContent = t('workBuddyWaiting');
    if (button) button.disabled = true;
    try {
      var started = await api('/api/admin/llm/providers/workbuddy/login', { method: 'POST', body: JSON.stringify({ edition: edition }) });
      if (gen !== workBuddyPollGen) return;
      workBuddySessionID = started.session_id || '';
      if (started.auth_url) {
        var popup = window.open(started.auth_url, '_blank', 'noopener');
        if (!popup && status) status.innerHTML = esc(t('workBuddyWaiting')) + ' <a href="' + esc(started.auth_url) + '" target="_blank" rel="noopener">' + esc(t('workBuddyOpen')) + '</a>';
      }
      pollWorkBuddyLogin(gen);
    } catch (e) {
      if (status) status.textContent = t('workBuddyFailed') + ': ' + e.message;
      if (button) button.disabled = false;
    }
  };
  function pollWorkBuddyLogin(gen) {
    if (gen !== workBuddyPollGen || !workBuddySessionID) return;
    workBuddyPollTimer = setTimeout(async function() {
      if (gen !== workBuddyPollGen) return;
      var status = document.getElementById('llmPrvWorkBuddyStatus');
      var button = document.getElementById('llmPrvWorkBuddyLogin');
      try {
        var data = await api('/api/admin/llm/providers/workbuddy/login/' + encodeURIComponent(workBuddySessionID));
        if (gen !== workBuddyPollGen) return;
        if (data.status === 'ready') {
          workBuddyModels = data.models || [];
          renderWorkBuddyModelChoices();
          var note = t('workBuddyReady');
          if (data.catalog_warning) note += ' ' + t('workBuddyCatalogWarn');
          if (status) status.textContent = note;
          if (button) button.disabled = false;
          return;
        }
        if (data.status === 'error') {
          if (status) status.textContent = t('workBuddyFailed') + (data.error ? ': ' + data.error : '');
          if (button) button.disabled = false;
          workBuddySessionID = '';
          return;
        }
        pollWorkBuddyLogin(gen);
      } catch (e) {
        if (gen !== workBuddyPollGen) return;
        if (status) status.textContent = t('workBuddyFailed') + ': ' + e.message;
        if (button) button.disabled = false;
      }
    }, 1500);
  }
  var providerDialogHomeArray = '';
  function sameProviderArrayID(a, b) {
    var left = providerArrayLockKey(a);
    var right = providerArrayLockKey(b);
    return !!left && left === right;
  }
  function providerArraySection(provider, selectedID) {
    var own = provider ? providerArrayByID(provider.array_id || provider.id) : null;
    var options = '<option value="">' + esc(t('providerArrayOwn')) + '</option>';
    providerArrayRecords().forEach(function(array) {
      if (own && array.members.length < 2 && sameProviderArrayID(array.id, own.id)) return;
      var label = array.name || array.id;
      if (array.members.length > 1) label += ' (' + array.members.length + ')';
      options += '<option value="' + esc(array.id) + '"' + (sameProviderArrayID(selectedID, array.id) ? ' selected' : '') + '>' + esc(label) + '</option>';
    });
    return '<div class="provider-array-field"><label for="llmPrvArray">' + esc(t('providerArrayJoin')) + '</label><select id="llmPrvArray" onchange="setProviderDialogArray(this.value)">' + options + '</select>'
      + '<div class="hint">' + esc(t('providerArrayHint')) + '</div>'
      + '<div id="llmPrvArraySharedNote" class="hint" hidden>' + esc(t('providerArrayUseShared')) + '</div></div>';
  }
  window.setProviderDialogArray = function(id) {
    var joining = !!providerArrayLockKey(id) && !sameProviderArrayID(id, providerDialogHomeArray);
    var note = document.getElementById('llmPrvArraySharedNote');
    var billingNote = document.getElementById('llmPrvBillingNote');
    if (note) note.hidden = !joining;
    if (billingNote) billingNote.hidden = joining;
  };
  window.editLLMProvider = function(id) { window.showProviderDialog('edit', id); };
  function providerModelsField(value) {
    return '<div><label for="llmPrvModels">' + esc(t('fieldModels')) + '</label>'
      + '<div class="provider-model-tools"><input id="llmPrvModels" list="llmPrvModelOptions" value="' + esc(value || '') + '" placeholder="gpt-4o, deepseek-chat"><button class="btn-ghost provider-probe-btn" type="button" onclick="probeProviderModels()">' + esc(t('providerProbeModels')) + '</button></div>'
      + '<datalist id="llmPrvModelOptions"></datalist><div id="llmPrvModelChoices" class="provider-model-results"></div><div id="llmPrvProbeStatus" class="provider-probe-status"></div></div>';
  }
  function providerCapabilitiesField(value) {
    return '<div class="provider-cap-field"><label for="llmPrvCaps">' + esc(t('fieldCapabilities')) + '</label>'
      + '<div id="llmPrvCapChips" class="provider-cap-picker" aria-label="' + esc(t('providerCapabilityPreset')) + '"></div>'
      + '<input id="llmPrvCaps" value="' + esc(value || '') + '" placeholder="tools, vision, reasoning" oninput="renderProviderCapabilityChips()"></div>';
  }
  function csvValues(id) { return csv(id).map(function(v){return v.trim();}).filter(Boolean); }
  function setCSVValues(id, values) {
    var el = document.getElementById(id);
    if (!el) return;
    var seen = {};
    el.value = (values || []).map(function(v){return String(v || '').trim();}).filter(function(v){if(!v || seen[v]) return false; seen[v] = true; return true;}).join(', ');
  }
  function addCSVValue(id, value) {
    value = String(value || '').trim();
    if (!value) return;
    var values = csvValues(id);
    if (values.indexOf(value) < 0) values.push(value);
    setCSVValues(id, values);
  }
  window.addProviderModel = function(model) { addCSVValue('llmPrvModels', model); };
  window.toggleProviderCapability = function(cap) {
    var values = csvValues('llmPrvCaps');
    var idx = values.indexOf(cap);
    if (idx >= 0) values.splice(idx, 1); else values.push(cap);
    setCSVValues('llmPrvCaps', values);
    renderProviderCapabilityChips();
  };
  window.renderProviderCapabilityChips = function() {
    var root = document.getElementById('llmPrvCapChips');
    if (!root) return;
    var active = csvValues('llmPrvCaps');
    root.innerHTML = providerCapabilityOptions.map(function(cap) {
      var on = active.indexOf(cap) >= 0;
      return '<button type="button" class="provider-cap-chip' + (on ? ' is-active' : '') + '" onclick="toggleProviderCapability(' + jsArg(cap) + ')">' + esc(cap) + '</button>';
    }).join('');
  };
  window.probeProviderModels = async function() {
    var status = document.getElementById('llmPrvProbeStatus');
    var choices = document.getElementById('llmPrvModelChoices');
    var list = document.getElementById('llmPrvModelOptions');
    if (status) status.textContent = t('providerProbing');
    if (choices) choices.innerHTML = '';
    try {
      var data = await api('/api/admin/llm/providers/probe-models', { method: 'POST', body: JSON.stringify({
        provider_id: providerDialogID, api_url: val('llmPrvURL'), api_key: val('llmPrvKey'), protocol: val('llmPrvProtocol') || 'openai'
      }) });
      var models = data.models || [];
      if (list) list.innerHTML = models.map(function(m){ return '<option value="' + esc(m) + '"></option>'; }).join('');
      if (choices) choices.innerHTML = models.map(function(m){ return '<button type="button" class="provider-model-choice" onclick="addProviderModel(' + jsArg(m) + ')">' + esc(m) + '</button>'; }).join('');
      if (status) status.textContent = models.length ? '' : t('providerProbeEmpty');
    } catch(e) {
      if (status) status.textContent = t('providerProbeFailed') + ': ' + e.message;
    }
  };
  window.saveProvider = async function(editID) {
    var existing = editID ? providers.find(function(x){ return x.id === editID; }) : null;
    var arrayID = val('llmPrvArray');
    // The billing editor writes the array's multiplier schedule, so a member
    // save must carry it through unchanged or the member row would silently
    // drop it on the next write.
    var billing = readProviderBilling();
    var payload = copyProviderExtraFields(existing);
    payload.id = val('llmPrvID');
    payload.name = val('llmPrvName');
    payload.api_url = val('llmPrvURL');
    payload.protocol = val('llmPrvProtocol');
    payload.models = csv('llmPrvModels');
    payload.capability_tags = csv('llmPrvCaps');
    payload.priority = num('llmPrvPriority');
    payload.sequence = num('llmPrvSequence');
    payload.max_concurrency = num('llmPrvConc');
    payload.upstream_timeout_sec = num('llmPrvTimeout');
    if (existing) {
      payload.timezone = existing.timezone || 'Asia/Shanghai';
      payload.credit_multiplier = existing.credit_multiplier > 0 ? existing.credit_multiplier : 1;
      payload.credit_multiplier_schedule = existing.credit_multiplier_schedule || [];
      payload.token_pricing = existing.token_pricing || {};
    }
    // The billing editor is authoritative for the multiplier and its windows.
    // An empty or half-filled window would silently widen the multiplier to
    // "always on", so the save is refused rather than repaired.
    if ((providerBillingSchedule || []).length !== billing.credit_multiplier_schedule.length) {
      toast(t('billingDroppedWindows'), 'error');
      return;
    }
    payload.timezone = billing.timezone;
    payload.credit_multiplier = billing.credit_multiplier;
    payload.credit_multiplier_schedule = billing.credit_multiplier_schedule;
    // The serve editor is authoritative for the dial windows. An empty or
    // half-filled window would silently widen availability to "always on",
    // so the save is refused rather than repaired.
    if (providerServeWindowsDropped()) {
      toast(t('serveDroppedWindows'), 'error');
      return;
    }
    payload.serve_windows = readProviderServeWindows();
    if (providerAccessMode === 'nodes') {
      var nodeInput = document.getElementById('llmPrvNodes');
      if (nodeInput) {
        providerAccessSelected = {};
        parseProviderNodeList(nodeInput.value).forEach(function(id) { providerAccessSelected[id] = true; });
      }
      var nodeIDs = providerAccessSelectedIDs();
      if (!nodeIDs.length) { toast(t('accessScopeNeedNode'), 'error'); return; }
      payload.allowed_node_ids = nodeIDs;
    } else {
      payload.allowed_node_ids = [];
    }
    var edition = val('llmPrvAuth');
    if (edition === 'china' || edition === 'global') {
      payload.auth_kind = 'workbuddy';
      payload.workbuddy_edition = edition;
      if (workBuddySessionID) payload.workbuddy_session_id = workBuddySessionID;
      else if (!editID) { toast(t('workBuddyNeedLogin'), 'error'); return; }
      if (!payload.models.length) { toast(t('workBuddyNeedModel'), 'error'); return; }
    } else {
      payload.auth_kind = 'api_key';
    }
    var key = val('llmPrvKey');
    if (key && payload.auth_kind !== 'workbuddy') payload.api_key = key;
    if (arrayID) {
      var chosenArray = providerArrayByID(arrayID);
      payload.array_id = chosenArray ? chosenArray.id : arrayID;
    } else payload.array_independent = true;
    try {
      if (editID) await api('/api/admin/llm/providers/' + encodeURIComponent(editID), { method: 'PUT', body: JSON.stringify(payload) });
      else await api('/api/admin/llm/providers', { method: 'POST', body: JSON.stringify(payload) });
      var openID = payload.array_id || payload.id;
      if (openID) providerArrayExpanded[openID] = true;
      sgCloseCurrentDialog(); toast(t('saved'), 'success'); loadProviders({ traffic: false });
    } catch(e) { toast(e.message, 'error'); }
  };
  window.deleteLLMProvider = async function(id) {
    var provider = providers.find(function(item) { return item.id === id; });
    if (!provider) return;
    var array = providerArrayByID(provider.array_id || provider.id);
    var arrayID = (array && array.id) || provider.array_id || provider.id;
    if (!array || (array.members.length <= 1 && !providerArrayProtected(array))) {
      return window.deleteProviderArray(arrayID);
    }
    var lockID = array.id;
    if (!beginProviderArrayAction(lockID)) return;
    try {
      var removeMsg = providerArrayProtected(array) ? t('providerArrayRemoveProtected') : t('providerArrayRemove');
      var referenced = sgRoutesReferencingMemberModel(provider);
      if (referenced.length) removeMsg += '\n\n' + t('sgMemberModelReferenced') + '\n' + referenced.join('\n');
      if (!sgConfirm(removeMsg)) return;
      await api('/api/admin/llm/providers/' + encodeURIComponent(id) + '?prune=1', { method: 'DELETE' });
      toast(t('deleted'), 'success');
      await loadProviders({ traffic: false });
      await loadServiceGroups();
    } catch (e) { toast(e.message, 'error'); }
    finally { endProviderArrayAction(lockID); }
  };
  function providerArrayGroupLines(groups) {
    return (groups || []).map(function(g) {
      if (g && typeof g === 'object') return '- ' + (g.name || g.id || '');
      var text = String(g || '').trim();
      return text ? '- ' + text : '';
    }).filter(Boolean).join('\n');
  }
  function alertProviderArrayInUse(groups) {
    var lines = providerArrayGroupLines(groups);
    window.alert(t('providerArrayDeleteInUse') + (lines ? '\n' + lines : ''));
  }
  var providerArrayRenameID = '';
  var providerArrayEditID = '';
  var providerArrayDialogMode = 'edit';
  var providerArrayBusy = {};
  function providerArrayLockKey(id) { return String(id || '').trim().toLowerCase(); }
  function beginProviderArrayAction(id) {
    id = providerArrayLockKey(id);
    if (!id || providerArrayBusy[id]) return false;
    providerArrayBusy[id] = true;
    return true;
  }
  function endProviderArrayAction(id) { delete providerArrayBusy[providerArrayLockKey(id)]; }
  function providerArrayNameInvalid(name) {
    return !name || /[\u0000-\u001f\u007f]/.test(name);
  }
  function providerArrayIDInvalid(id) {
    return !id || /[\u0000-\u0020\u007f\/\\?#]/.test(id);
  }
  function renderProviderArrayRenameDialog() {
    var array = providerArrayByID(providerArrayRenameID);
    if (!array) return;
    var current = document.getElementById('llmArrayName');
    var typed = current ? current.value : '';
    var html = sgDialogChrome(t('providerArrayRenameTitle'),
      '<form onsubmit="saveProviderArrayName();return false;">' + field('llmArrayName', t('providerArrayName'), typed || array.name || array.id) + '</form>',
      '<button class="btn-primary" type="button" onclick="saveProviderArrayName()">' + esc(t('save')) + '</button><button class="btn-ghost" type="button" onclick="sgCloseCurrentDialog()">' + esc(t('cancel')) + '</button>');
    html = html.replace('id="llmArrayName"', 'id="llmArrayName" maxlength="80"');
    openDialog(html, 'sg-form-dialog');
    sgOpenKind = 'array-rename';
    var input = document.getElementById('llmArrayName');
    if (input) { input.focus(); if (!typed && typeof input.select === 'function') input.select(); }
  }
  window.renameProviderArray = function(id) {
    if (providerArrayProtected(providerArrayByID(id) || { id: id })) {
      toast(t('providerArrayRenameProtected'), 'error');
      return;
    }
    providerArrayRenameID = String(id || '');
    if (!providerArrayByID(providerArrayRenameID)) return;
    renderProviderArrayRenameDialog();
  };
  window.saveProviderArrayName = async function() {
    var id = providerArrayRenameID;
    var name = val('llmArrayName');
    if (providerArrayProtected(providerArrayByID(id) || { id: id })) {
      toast(t('providerArrayRenameProtected'), 'error');
      return;
    }
    if (!id || providerArrayNameInvalid(name)) { toast(t('providerArrayRenameNeedName'), 'error'); return; }
    if (Array.from(name).length > 80) { toast(t('providerArrayRenameTooLong'), 'error'); return; }
    var current = providerArrayByID(id);
    if (current && name === String(current.name || '').trim()) { sgCloseCurrentDialog(); return; }
    if (!beginProviderArrayAction(id)) return;
    try {
      await api('/api/admin/llm/provider-arrays/' + encodeURIComponent(id), { method: 'PUT', body: JSON.stringify({ name: name }) });
      sgCloseCurrentDialog();
      toast(t('saved'), 'success');
      await loadProviders({ traffic: false });
    } catch (e) { toast(e.message, 'error'); }
    finally { endProviderArrayAction(id); }
  };
  function blankProviderArrayShape() {
    return { id: '', name: '', timezone: 'Asia/Shanghai', credit_multiplier: 1, credit_multiplier_schedule: [], token_pricing: { timezone: 'Asia/Shanghai' } };
  }
  function renderProviderArrayEditDialog(opts) {
    var creating = providerArrayDialogMode === 'create';
    var array = creating ? blankProviderArrayShape() : providerArrayByID(providerArrayEditID);
    if (!array) return;
    if (!(opts && opts.keepBilling)) {
      providerBillingSchedule = cloneProviderBillingSchedule(array.credit_multiplier_schedule);
      providerTokenPricingSchedule = cloneProviderTokenPricingSchedule(array.token_pricing && array.token_pricing.price_schedule);
    }
    var name = (opts && opts.name != null) ? opts.name : (array.name || '');
    var idValue = (opts && opts.arrayID != null) ? opts.arrayID : (array.id || '');
    var html = sgDialogChrome(creating ? t('providerArrayCreateTitle') : t('providerArrayEditTitle'),
      '<form onsubmit="saveProviderArraySettings();return false;">'
      + field('llmArrayEditID', t('providerArrayID'), idValue, !creating)
      + field('llmArrayEditName', t('providerArrayName'), name, !creating && providerArrayProtected(array))
      + '<div class="hint">' + esc(t('providerArrayHint')) + '</div>'
      + providerTokenPricingSection(array)
      + providerBillingSection(array, opts)
      + '</form>',
      '<button class="btn-primary" type="button" onclick="saveProviderArraySettings()">' + esc(t('save')) + '</button><button class="btn-ghost" type="button" onclick="sgCloseCurrentDialog()">' + esc(t('cancel')) + '</button>');
    html = html.replace('id="llmArrayEditID"', 'id="llmArrayEditID" maxlength="80"');
    html = html.replace('id="llmArrayEditName"', 'id="llmArrayEditName" maxlength="80"');
    openDialog(html, 'sg-form-dialog');
    sgOpenKind = 'array-edit';
    startProviderBillingNowClock();
    if (!(opts && opts.keepBilling)) {
      var input = document.getElementById(creating ? 'llmArrayEditID' : 'llmArrayEditName');
      if (input) { input.focus(); if (typeof input.select === 'function') input.select(); }
    }
  }
  window.showProviderArrayDialog = function() {
    providerArrayDialogMode = 'create';
    providerArrayEditID = '';
    renderProviderArrayEditDialog();
  };
  window.editProviderArray = function(id) {
    providerArrayDialogMode = 'edit';
    providerArrayEditID = String(id || '');
    if (!providerArrayByID(providerArrayEditID)) return;
    renderProviderArrayEditDialog();
  };
  window.saveProviderArraySettings = async function() {
    var creating = providerArrayDialogMode === 'create';
    var id = creating ? val('llmArrayEditID') : providerArrayEditID;
    var name = val('llmArrayEditName');
    if (creating && (!id || providerArrayIDInvalid(id) || Array.from(id).length > 80)) { toast(t('providerArrayNeedID'), 'error'); return; }
    if (!creating && !id) { toast(t('providerArrayNeedID'), 'error'); return; }
    if (providerArrayNameInvalid(name)) { toast(t('providerArrayRenameNeedName'), 'error'); return; }
    if (Array.from(name).length > 80) { toast(t('providerArrayRenameTooLong'), 'error'); return; }
    if (!creating && providerArrayProtected(providerArrayByID(id) || { id: id })) {
      var kept = providerArrayByID(id);
      if (!kept || name !== String(kept.name || '').trim()) {
        toast(t('providerArrayRenameProtected'), 'error');
        return;
      }
    }
    var billing = readProviderBilling();
    if ((providerBillingSchedule || []).length !== billing.credit_multiplier_schedule.length) {
      toast(t('billingDroppedWindows'), 'error');
      return;
    }
    var tokenPricing = readProviderTokenPricing();
    if (tokenPricing === null) {
      var dropped = (providerTokenPricingSchedule || []).map(normalizeProviderTokenPriceWindow).filter(function(window) {
        return window && !providerTokenPriceWindowInvalid(window);
      }).length !== (providerTokenPricingSchedule || []).length;
      toast(t(dropped ? 'pricingDroppedWindows' : 'billingInvalid'), 'error');
      return;
    }
    if (!beginProviderArrayAction(id)) return;
    var body = {
      name: name,
      billing: {
        timezone: billing.timezone,
        credit_multiplier: billing.credit_multiplier,
        credit_multiplier_schedule: billing.credit_multiplier_schedule,
        token_pricing: tokenPricing
      }
    };
    try {
      if (creating) {
        body.id = id;
        await api('/api/admin/llm/provider-arrays', { method: 'POST', body: JSON.stringify(body) });
      } else {
        await api('/api/admin/llm/provider-arrays/' + encodeURIComponent(id), { method: 'PUT', body: JSON.stringify(body) });
      }
      sgCloseCurrentDialog();
      toast(t('saved'), 'success');
      await loadProviders({ traffic: false });
    } catch (e) { toast(e.message, 'error'); }
    finally { endProviderArrayAction(id); }
  };
  function sgRelabelArrayEditDialog() {
    var focus = sgSnapFocus(document.getElementById('llmDialogContent'));
    var snap = {
      arrayID: val('llmArrayEditID'),
      name: val('llmArrayEditName'),
      timezone: val('llmPrvTimezone'),
      multiplier: val('llmPrvMultiplier'),
      tpIn: val('llmPrvTpIn'), tpOut: val('llmPrvTpOut'),
      tpCacheRead: val('llmPrvTpCacheRead'), tpCacheWrite: val('llmPrvTpCacheWrite'),
      tpRmbIn: val('llmPrvTpRmbIn'), tpRmbOut: val('llmPrvTpRmbOut'),
      tpRmbCacheRead: val('llmPrvTpRmbCacheRead'), tpRmbCacheWrite: val('llmPrvTpRmbCacheWrite'),
      tpMin: val('llmPrvTpMin'), tpTimezone: val('llmPrvTpTimezone'), tpVersion: val('llmPrvTpVersion')
    };
    renderProviderArrayEditDialog({ keepBilling: true, name: snap.name, arrayID: snap.arrayID, timezone: snap.timezone, multiplier: snap.multiplier });
    function set(id, value) { var node = document.getElementById(id); if (node && value != null) node.value = value; }
    set('llmArrayEditID', snap.arrayID);
    set('llmArrayEditName', snap.name);
    set('llmPrvTimezone', snap.timezone);
    set('llmPrvMultiplier', snap.multiplier);
    set('llmPrvTpIn', snap.tpIn); set('llmPrvTpOut', snap.tpOut);
    set('llmPrvTpCacheRead', snap.tpCacheRead); set('llmPrvTpCacheWrite', snap.tpCacheWrite);
    set('llmPrvTpRmbIn', snap.tpRmbIn); set('llmPrvTpRmbOut', snap.tpRmbOut);
    set('llmPrvTpRmbCacheRead', snap.tpRmbCacheRead); set('llmPrvTpRmbCacheWrite', snap.tpRmbCacheWrite);
    set('llmPrvTpMin', snap.tpMin); set('llmPrvTpTimezone', snap.tpTimezone); set('llmPrvTpVersion', snap.tpVersion);
    if (typeof refreshProviderBillingNow === 'function') refreshProviderBillingNow();
    sgRestoreFocus(focus);
  }
  window.deleteProviderArray = async function(id) {
    if (providerArrayProtected(providerArrayByID(id) || { id: id })) {
      toast(t('providerArrayProtected'), 'error');
      return;
    }
    if (!beginProviderArrayAction(id)) return;
    var array = providerArrayByID(id);
    var label = (array && (array.name || array.id)) || id;
    try {
      var refData = await api('/api/admin/llm/provider-arrays/' + encodeURIComponent(id) + '/references');
      var groups = (refData && refData.groups) || [];
      if (groups.length) { alertProviderArrayInUse(groups); return; }
      if (!sgConfirm(t('providerArrayDeleteConfirm') + '\n' + label)) return;
      await api('/api/admin/llm/provider-arrays/' + encodeURIComponent(id), { method: 'DELETE' });
      toast(t('deleted'), 'success');
      await loadProviders({ traffic: false });
      await loadServiceGroups();
    } catch (e) {
      if (e && (e.message === 'provider_in_use' || e.status === 409)) {
        var stuck = null;
        try { stuck = (JSON.parse(e.responseBody || '{}').groups) || null; } catch (ignore) {}
        if (!stuck || !stuck.length) {
          try {
            var again = await api('/api/admin/llm/provider-arrays/' + encodeURIComponent(id) + '/references');
            stuck = (again && again.groups) || stuck;
          } catch (ignore) {}
        }
        alertProviderArrayInUse(stuck);
        return;
      }
      toast(e.message, 'error');
    } finally { endProviderArrayAction(id); }
  };
  window.moveLLMProvider = async function(id, delta) {
    providersLoadSeq += 1;
    var list = sortedProviders();
    var from = list.findIndex(function(p){ return p.id === id; });
    if (from < 0) return;
    var to = from + Number(delta || 0);
    if (to < 0 || to >= list.length) return;
    var item = list.splice(from, 1)[0];
    list.splice(to, 0, item);
    var sequences = {};
    list.forEach(function(p, i) {
      var seq = i + 1;
      sequences[p.id] = seq;
      providerSequenceInFlight[p.id] = seq;
      p.sequence = seq;
    });
    renderProviders();
    try {
      await api('/api/admin/llm/providers/sequences', { method: 'PUT', body: JSON.stringify({ sequences: sequences }) });
      providersLoadSeq += 1;
      providerSequenceInFlight = {};
    } catch (e) {
      toast(e.message, 'error');
      providerSequenceInFlight = {};
      loadProviders({ traffic: false });
    }
  };
  window.toggleLLMProviderPaused = async function(id) {
    var p = providers.find(function(x){ return x.id === id; });
    if (!p) return;
    var next = !p.paused;
    p.paused = next;
    providersLoadSeq += 1;
    renderProviders();
    try {
      await api('/api/admin/llm/providers/' + encodeURIComponent(id) + '/paused', { method: 'PUT', body: JSON.stringify({ paused: next }) });
    } catch (e) {
      p.paused = !next;
      toast(e.message, 'error');
      renderProviders();
    }
  };
  async function loadAgents() {
    try { var data = await api('/api/admin/llm/agents'); agents = data.agents || []; }
    catch(e) {
      toast(e.message || t('sgFailed'), 'error');
      if (!agents.length) renderAgents();
      return;
    }
    renderAgents();
  }
  function renderAgents() {
    var el = document.getElementById('llmAgentsList');
    if (!el) return;
    if (!agents.length) { el.innerHTML = '<div class="hint">' + esc(t('noAgents')) + '</div>'; return; }
    el.innerHTML = agents.map(function(a) {
      var locked = a.id === 'maclaw_official';
      var status = a.enabled === false ? '<span class="badge warn">Disabled</span>' : '<span class="badge ok">Enabled</span>';
      return '<div class="data-row llm-agent-row"><div class="data-row-main"><strong>' + esc(a.name || a.id) + '</strong> ' + status
        + '<span class="data-row-meta">' + esc(a.id) + (a.contact ? ' \u00b7 ' + esc(a.contact) : '') + (a.description ? ' \u00b7 ' + esc(a.description) : '') + '</span></div>'
        + '<div class="data-row-actions">'
        + '<button class="btn-ghost" onclick="showLLMAgentDialog(\'edit\',' + jsArg(a.id) + ')">' + esc(t('editAgent')) + '</button>'
        + (locked ? '' : '<button class="btn-danger-ghost" onclick="deleteLLMAgent(' + jsArg(a.id) + ')">' + esc(t('deleteAgent')) + '</button>')
        + '</div></div>';
    }).join('');
  }
  window.showLLMAgentDialog = function(mode, id) {
    var a = mode === 'edit' ? agents.find(function(x){return x.id===id;}) : null;
    sgOpenKind = 'agent';
    var html = sgDialogChrome(mode === 'edit' ? t('agentDialogTitleEdit') : t('agentDialogTitleNew'),
      '<div class="sg-form-grid">'
      + field('llmAgentID', t('fieldAgentID'), a ? a.id : '', mode === 'edit')
      + field('llmAgentName', t('fieldAgentName'), a ? a.name : '')
      + field('llmAgentContact', t('fieldAgentContact'), a ? a.contact : '')
      + field('llmAgentSettlement', t('fieldAgentSettlement'), a ? a.settlement : '')
      + '</div><div class="sg-block-xs"><label for="llmAgentDesc">' + esc(t('fieldAgentDesc')) + '</label><textarea id="llmAgentDesc" rows="3">' + esc(a ? a.description : '') + '</textarea></div>',
      '<button class="btn-primary" onclick="saveLLMAgent(' + jsArg(mode === 'edit' ? id : '') + ')">' + esc(t('save')) + '</button><button class="btn-ghost" onclick="sgCloseCurrentDialog()">' + esc(t('cancel')) + '</button>');
    openDialog(html, 'sg-form-dialog');
  };
  window.saveLLMAgent = async function(editID) {
    var payload = { id: val('llmAgentID'), name: val('llmAgentName'), contact: val('llmAgentContact'), settlement: val('llmAgentSettlement'), description: val('llmAgentDesc'), enabled: true };
    try {
      if (editID) await api('/api/admin/llm/agents/' + encodeURIComponent(editID), { method: 'PUT', body: JSON.stringify(payload) });
      else await api('/api/admin/llm/agents', { method: 'POST', body: JSON.stringify(payload) });
      sgCloseCurrentDialog(); toast(t('saved'), 'success'); await loadAgents(); await loadServiceGroups();
    } catch(e) { toast(e.message, 'error'); }
  };
  window.deleteLLMAgent = async function(id) {
    if (!sgConfirm(t('deleteAgent') + ': ' + id + '?')) return;
    try { await api('/api/admin/llm/agents/' + encodeURIComponent(id), { method: 'DELETE' }); toast(t('deleted'), 'success'); await loadAgents(); await loadServiceGroups(); }
    catch(e) { toast(e.message, 'error'); }
  };

  function sgModelHasProvider(model) {
    if (!model) return false;
    var configs = model.provider_configs || [];
    var providerID = (configs[0] && configs[0].provider_id) || ((model.provider_ids || [])[0]) || '';
    return !!String(providerID || '').trim();
  }
  function sgFindGroupModel(group, name) {
    var want = sgCanonicalModelName(name);
    var models = (group && group.models) || [];
    for (var i = 0; i < models.length; i++) {
      if (models[i] && sgCanonicalModelName(models[i].name) === want) return models[i];
    }
    return null;
  }
  // A dynamic group's auto row is not a dial target. An unclassified client
  // request uses the balanced route, then the same-group availability chain.
  function sgDynamicStatusModel(group) {
    var selected = 'official-mid';
    var routes = (group && group.routes) || [];
    for (var i = 0; i < routes.length; i++) {
      if (String(routes[i] && routes[i].class || '').trim() !== 'balanced') continue;
      var name = sgCanonicalModelName(routes[i].model || '');
      if (name && name !== 'auto') selected = name;
      break;
    }
    var quality = sgOfficialBandQuality(selected);
    var fallback = quality === 'high'
      ? ['official-high', 'official-mid', 'official-low']
      : quality === 'low'
        ? ['official-low', 'official-mid', 'official-high']
        : ['official-mid', 'official-low', 'official-high'];
    var chain = [selected];
    fallback.forEach(function(name) { if (chain.indexOf(name) < 0) chain.push(name); });
    for (var j = 0; j < chain.length; j++) {
      var model = sgFindGroupModel(group, chain[j]);
      if (sgModelHasProvider(model)) return model;
    }
    return null;
  }
  window.testLLMServiceGroup = async function(groupId) {
    if (_testingGroupId) { toast(isZh() ? '\u6d4b\u8bd5\u8fdb\u884c\u4e2d\uff0c\u8bf7\u7a0d\u5019...' : 'Test in progress, please wait...', 'info'); return; }
    var group = serviceGroups.find(function(g) { return g.id === groupId; });
    if (!group) { toast(t('sgFailed'), 'error'); return; }
    // Routes store a provider array id. auto / official-* are billing bands,
    // not upstream model ids. The server picks a live member and that member's model.
    var picked = null;
    function pickRoute(model, logicalName) {
      var configs = model.provider_configs || [];
      var providerID = (configs[0] && configs[0].provider_id) || ((model.provider_ids || [])[0]) || '';
      if (!String(providerID || '').trim()) return false;
      var logicalModel = logicalName || model.name || '';
      picked = { providerID: providerID, routeModel: (configs[0] && configs[0].model) || '', logicalModel: logicalModel };
      return true;
    }
    var models = group.models || [];
    if (group.kind === 'dynamic') {
      var routed = sgDynamicStatusModel(group);
      if (routed) pickRoute(routed, sgCanonicalModelName(routed.name));
    }
    if (!picked) models.some(function(model) { return pickRoute(model); });
    if (!picked) { toast(t('sgRouteNeedsProvider'), 'error'); return; }
    _testingGroupId = groupId;
    try {
      var data = await api('/api/admin/llm/providers/test-chat', { method: 'POST', body: JSON.stringify({
        provider_id: picked.providerID,
        model: picked.routeModel || picked.logicalModel,
        route_model: picked.routeModel,
        logical_model: picked.logicalModel
      }) });
      if (data.success) toast(t('providerTestOK') + (data.model ? ' ' + data.model : '') + ' ' + (data.latency_ms || 0) + 'ms', 'success');
      else toast(t('providerTestFailed') + ': ' + (data.error || 'unknown'), 'error');
    } catch(e) { toast(e.message, 'error'); }
    finally { _testingGroupId = ''; }
  };
  async function loadServiceGroups() {
    var seq = ++serviceGroupsLoadSeq;
    try {
      var data = await api('/api/admin/llm/service-groups');
      if (seq !== serviceGroupsLoadSeq) return;
      serviceGroups = data.service_groups || [];
      defaultServiceGroupId = data.default_service_group_id || '';
    } catch(e) {
      if (seq !== serviceGroupsLoadSeq) return;
      toast(e.message || t('sgFailed'), 'error');
      if (!serviceGroups.length) {
        defaultServiceGroupId = '';
        renderServiceGroups();
      } else {
        sgPendingGroupScroll = '';
      }
      return;
    }
    renderServiceGroups();
    loadServiceGroupTraffic();
    sgSyncHeadScoreGroupSelect();
  }
  function renderServiceGroups() {
    var el = document.getElementById('llmServiceGroupsList');
    if (!el) return;
    syncServiceGroupTrafficSwitch();
    if (!serviceGroups.length) { el.innerHTML = '<div class="hint">' + esc(t('noGroups')) + '</div>'; sgPendingGroupScroll = ''; return; }
    el.innerHTML = serviceGroups.map(function(g) {
      var modelNames = (g.models||[]).map(function(m){return m.name;}).join(', ');
      var policyBadge = g.access_policy === 'grant_required' ? '<span class="badge warn">'+esc(sgPolicyLabel('grant_required'))+'</span>' : '<span class="badge ok">'+esc(sgPolicyLabel('free'))+'</span>';
      var agentName = g.agent_name || agentNameByID(g.agent_id) || '-';
      var isOfficial = sgIsOfficialGroup(g.id);
      var isDefault = String(defaultServiceGroupId||'') === String(g.id||'');
      var isDynamic = String(g.kind||'') === 'dynamic';
      var tags = '<div class="sg-group-tags">' + policyBadge
        + (isOfficial ? '<span class="badge info">' + esc(t('sgSystemBadge')) + '</span>' : '')
        + (isDefault ? '<span class="badge ok">' + esc(t('sgDefaultBadge')) + '</span>' : '')
        + (isDynamic ? '<span class="badge">' + esc(t('sgKindDynamic')) + '</span>' : '<span class="badge">' + esc(t('sgKindStatic')) + '</span>')
        + '</div>';
      return '<div class="data-row llm-service-group-row" tabindex="-1" data-sg-group-id="' + esc(g.id) + '"><div class="data-row-main"><strong>' + esc(g.name||g.id) + '</strong> ' + tags
        + '<span class="data-row-meta">' + esc(agentName) + ' \u00b7 ' + esc(g.description||'') + ' \u00b7 ' + esc(modelNames||'no models')
        + ' \u00b7 ' + (g.models||[]).length + ' route(s)</span></div>'
        + '<div class="data-row-actions">'
        + (isDynamic ? '<button class="btn-ghost" onclick="editLLMClassTraffic('+jsArg(g.id)+')">' + esc(t('sgClassTrafficOpen')) + '</button>' : '')
        + '<button class="btn-ghost" onclick="testLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('testProvider')) + '</button>'
        + '<button class="btn-ghost" onclick="editLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('editGroup')) + '</button>'
        + (isDefault ? '' : '<button class="btn-ghost" onclick="setDefaultLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('sgSetDefault')) + '</button>')
        + (isOfficial?'':'<button class="btn-danger-ghost" onclick="deleteLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('deleteGroup')) + '</button>')
        + '</div>'
        + '<div class="service-group-traffic' + (serviceGroupTrafficReady ? '' : ' is-pending') + '" data-service-group-id="' + esc(g.id) + '"></div></div>';
    }).join('');
    patchServiceGroupTraffic();
    if (sgPendingGroupScroll) {
      var revealId = sgPendingGroupScroll;
      sgPendingGroupScroll = '';
      sgScrollServiceGroupRow(revealId);
    }
  }
  function sgRememberSavedGroup(payload) {
    if (!payload || !String(payload.id || '').trim()) return;
    var saved = sgCloneGroup(payload);
    var id = String(saved.id || '');
    var i;
    for (i = 0; i < serviceGroups.length; i++) {
      if (String(serviceGroups[i] && serviceGroups[i].id || '') === id) {
        serviceGroups[i] = saved;
        return;
      }
    }
    serviceGroups.push(saved);
  }
  function sgServiceGroupRow(id) {
    var want = String(id || '');
    if (!want) return null;
    var rows = document.querySelectorAll('#llmServiceGroupsList .llm-service-group-row');
    var i;
    for (i = 0; i < rows.length; i++) {
      if (rows[i].getAttribute('data-sg-group-id') === want) return rows[i];
    }
    return null;
  }
  function sgScrollServiceGroupRow(id) {
    var want = String(id || '');
    if (!want) return;
    function go() {
      var row = sgServiceGroupRow(want);
      if (!row) return;
      // content-visibility:auto can ignore scrollIntoView until the row is forced visible.
      row.classList.add('sg-group-reveal');
      var marked = document.querySelectorAll('#llmServiceGroupsList .sg-group-reveal');
      var i;
      for (i = 0; i < marked.length; i++) {
        if (marked[i] !== row) marked[i].classList.remove('sg-group-reveal');
      }
      if (row.scrollIntoView) row.scrollIntoView({block:'nearest', inline:'nearest'});
      var active = document.activeElement;
      var list = document.getElementById('llmServiceGroupsList');
      if (row.focus && (!active || active === document.body || active === document.documentElement || active === list)) row.focus({preventScroll:true});
    }
    go();
    requestAnimationFrame(function(){ go(); requestAnimationFrame(go); });
  }
  window.setDefaultLLMServiceGroup = async function(id) {
    try {
      await api('/api/admin/llm/service-groups/' + encodeURIComponent(id) + '/default', { method: 'PUT', body: '{}' });
      toast(t('sgDefaultSaved'), 'success');
      await loadServiceGroups();
    } catch(e) { toast(e.message, 'error'); }
  };
  window.deleteLLMServiceGroup = async function(id) {
    if (sgIsOfficialGroup(id)) { toast(t('sgOfficialNoDelete'), 'error'); return; }
    if (String(defaultServiceGroupId||'') === String(id||'')) { toast(t('sgDeleteDefaultBlocked'), 'error'); return; }
    if (!sgConfirm(t('deleteGroup') + ': ' + id + '?')) return;
    try { await api('/api/admin/llm/service-groups/' + encodeURIComponent(id), { method: 'DELETE' }); toast(t('deleted'), 'success'); loadServiceGroups(); }
    catch(e) { toast(e.message, 'error'); }
  };
  function sgIsOfficialGroup(id){ return String(id||'').trim().toLowerCase()==='maclaw-official'; }
  function agentNameByID(id){var a=agents.find(function(x){return x.id===id;});return a&&(a.name||a.id);}
  function sgPolicyLabel(policy){return policy==='grant_required'?(isZh()?'\u9700\u5151\u6362\u5361':'Card Required'):(isZh()?'\u514d\u8d39\u901a\u884c':'Free Access');}
  function sgProviderIDsFromModel(m) {
    var ids=[], configs=m&&m.provider_configs||[], i, id;
    for(i=0;i<configs.length;i++){
      id=configs[i]&&String(configs[i].provider_id||'').trim();
      if(id&&ids.indexOf(id)<0) ids.push(id);
    }
    if(ids.length) return ids;
    var legacy=m&&m.provider_ids||[];
    for(i=0;i<legacy.length;i++){ id=String(legacy[i]||'').trim(); if(id&&ids.indexOf(id)<0) ids.push(id); }
    return ids;
  }
  function sgProviderByID(id){return providers.find(function(x){return x.id===id;})||null;}
  function sgProviderModels(id) {
    var array = providerArrayByID(id);
    var source = array && array.members.length ? array.members : [];
    if (!source.length) {
      var single = sgProviderByID(id);
      source = single ? [single] : [];
    }
    var seen = {};
    var out = [];
    source.forEach(function(provider) {
      (provider.models || []).forEach(function(model) {
        model = String(model || '').trim();
        if (!model || seen[model]) return;
        seen[model] = true;
        out.push(model);
      });
    });
    return out;
  }
  function sgIsBillingBand(name) {
    switch (String(name || '').trim().toLowerCase()) {
      case 'auto': case 'default': case 'low': case 'mid': case 'high':
      case 'official-low': case 'official-mid': case 'official-high':
        return true;
      default:
        return false;
    }
  }
  function sgRoutesWithUnmatchedUpstream(group) {
    var lines = [];
    (group && group.models || []).forEach(function(model) {
      (model.provider_configs || []).forEach(function(pc) {
        var upstream = String(pc && pc.model || '').trim();
        if (!upstream || sgIsBillingBand(upstream)) return;
        var offered = sgProviderModels(pc.provider_id);
        if (!offered.length) return;
        var lower = upstream.toLowerCase();
        var ok = offered.some(function(name) { return String(name || '').trim().toLowerCase() === lower; });
        if (ok) return;
        lines.push((group.name || group.id || '') + ' / ' + (model.name || '') + ' / ' + (sgProviderName(pc.provider_id) || pc.provider_id) + ' / ' + upstream);
      });
    });
    return lines;
  }
  function sgRoutesReferencingMemberModel(provider) {
    if (!provider) return [];
    var names = {};
    (provider.models || []).forEach(function(model) {
      model = String(model || '').trim().toLowerCase();
      if (model) names[model] = true;
    });
    if (!Object.keys(names).length) return [];
    var memberID = String(provider.id || '').trim().toLowerCase();
    var arrayID = String(provider.array_id || provider.id || '').trim().toLowerCase();
    var lines = [];
    (serviceGroups || []).forEach(function(group) {
      (group.models || []).forEach(function(model) {
        (model.provider_configs || []).forEach(function(pc) {
          var upstream = String(pc && pc.model || '').trim();
          if (!upstream || !names[upstream.toLowerCase()]) return;
          var pid = String(pc.provider_id || '').trim().toLowerCase();
          if (pid !== arrayID && pid !== memberID) return;
          lines.push((group.name || group.id || '') + ' / ' + (model.name || '') + ' / ' + upstream);
        });
      });
    });
    return lines;
  }
  function sgEffectiveRouteModel(c){var m=(c&&c.model||'').trim();if(m)return m;var models=sgProviderModels(c&&c.provider_id||'');return models.length===1?models[0]:'';}
  function sgRouteKey(c){return (c&&c.provider_id||'').trim()+'\u0000'+sgEffectiveRouteModel(c);}
  function sgNormalizeTokenPricing(src){
    var p = src && typeof src === 'object' ? src : {};
    var out = {};
    function num(k){ var v = p[k]; if(v===undefined||v===null||v==='') return undefined; var n=Number(v); if(!isFinite(n)||n<0) return undefined; return n; }
    var v;
    v=num('input_credits_per_10k'); if(v!==undefined) out.input_credits_per_10k=v;
    v=num('output_credits_per_10k'); if(v!==undefined) out.output_credits_per_10k=v;
    v=num('cache_read_credits_per_10k'); if(v!==undefined) out.cache_read_credits_per_10k=v;
    v=num('cache_write_credits_per_10k'); if(v!==undefined) out.cache_write_credits_per_10k=v;
    v=num('input_rmb_per_10k'); if(v!==undefined) out.input_rmb_per_10k=v;
    v=num('output_rmb_per_10k'); if(v!==undefined) out.output_rmb_per_10k=v;
    v=num('cache_read_rmb_per_10k'); if(v!==undefined) out.cache_read_rmb_per_10k=v;
    v=num('cache_write_rmb_per_10k'); if(v!==undefined) out.cache_write_rmb_per_10k=v;
    v=num('minimum_request_credits'); if(v!==undefined) out.minimum_request_credits=v;
    if(p.timezone) out.timezone=String(p.timezone).trim();
    if(p.version) out.version=String(p.version).trim();
    if(p.price_schedule && Array.isArray(p.price_schedule) && p.price_schedule.length) {
      try { out.price_schedule=JSON.parse(JSON.stringify(p.price_schedule)); } catch(e){ out.price_schedule=p.price_schedule.slice(); }
    }
    return out;
  }
  function sgFormatPricingBrief(tp){
    if(!tp) return '';
    var inC = tp.input_credits_per_10k, outC = tp.output_credits_per_10k;
    if(inC===undefined && outC===undefined) return '';
    return (inC!==undefined?String(inC):'-')+'/'+(outC!==undefined?String(outC):'-')+' Credits/10k';
  }
  function sgProviderConfigsFromModel(m){
    var configs=(m&&m.provider_configs||[]).map(function(c){return{provider_id:(c.provider_id||'').trim(),model:(c.model||'').trim(),billing_mode:String(c.billing_mode||'').trim(),capability_tags:(c.capability_tags||[]).slice(),priority:c.priority||0,resolution_tier:c.resolution_tier||0,credit_multiplier:c.credit_multiplier||1,token_pricing_override:c.token_pricing_override===true,token_pricing:sgNormalizeTokenPricing(c.token_pricing)};}).filter(function(c){return c.provider_id;});
    if(!configs.length) configs=(m&&m.provider_ids||[]).map(function(pid){return{provider_id:pid,model:'',billing_mode:'',capability_tags:[],priority:0,resolution_tier:0,credit_multiplier:1,token_pricing:{}};});
    return configs;
  }
  function sgRouteDuplicateIndex(model,cfg,skipIndex){
    var key=sgRouteKey(cfg);
    for(var i=0;i<(model.provider_configs||[]).length;i++){ if(i===skipIndex)continue; if(sgRouteKey(model.provider_configs[i])===key)return i; }
    return -1;
  }
  function sgDuplicateRouteMessage(cfg){return 'Duplicate route: '+sgProviderName(cfg.provider_id)+' / '+((cfg.model||'').trim()||'provider default');}
  function sgCloneGroup(g) {
    return {id:(g&&g.id||'').trim(),name:(g&&g.name||'').trim(),description:(g&&g.description||'').trim(),
      agent_id:(g&&g.agent_id)||'maclaw_official',agent_name:(g&&g.agent_name)||agentNameByID(g&&g.agent_id)||'',
      access_policy:g&&g.access_policy||'free', kind:String(g&&g.kind||'')==='dynamic'?'dynamic':'static',
      quality_floor:String(g&&g.quality_floor||'').trim(),
      exposed_models:(g&&g.exposed_models||[]).map(function(n){return String(n||'').trim();}).filter(Boolean),
      routes:(g&&g.routes||[]).map(function(r){return{class:String(r&&r.class||'').trim(),model:String(r&&r.model||'').trim(),quality:String(r&&r.quality||'').trim()};}),
      models:(g&&g.models||[]).map(function(m){return{name:m.name||'auto',provider_ids:sgProviderIDsFromModel(m),provider_configs:sgProviderConfigsFromModel(m),capability_tags:(m.capability_tags||[]).slice(),priority:m.priority||50,resolution_tier:m.resolution_tier||0,credit_multiplier:m.credit_multiplier||1,billing_multiplier:sgEffectiveBillingMultiplier(m)};})};
  }
  function sgEmptyGroup(){return{id:'',name:'',description:'',agent_id:'maclaw_official',agent_name:agentNameByID('maclaw_official')||'MaClaw official',access_policy:'free',kind:'dynamic',quality_floor:'',exposed_models:[],routes:sgDefaultDynamicRoutes(),models:[sgEmptyModel('auto')]};}
  function sgProviderName(id){
    var array = providerArrayByID(id);
    if (array) {
      var name = array.name || array.id;
      if (array.members.length > 1) return name + ' \u00b7 ' + array.members.length;
      return name;
    }
    var p=providers.find(function(x){return x.id===id;});
    return p?(p.name||p.id):id;
  }
  function sgGetProviderConfig(model,routeIndex){if(!model)return null;model.provider_configs=model.provider_configs||[];return model.provider_configs[routeIndex]||null;}
  function sgOfficialBandNames(){return ['official-high','official-mid','official-low'];}
  function sgNormName(n){return String(n||'').trim().toLowerCase();}
  function sgOfficialBandName(n){ n=sgNormName(n); if(n==='official-high'||n==='official-mid'||n==='official-low')return n; return ''; }
  function sgCanonicalModelName(n){ n=String(n||'').trim(); var band=sgOfficialBandName(n); if(band)return band; if(sgNormName(n)==='auto'||!n)return 'auto'; return n; }
  function sgOfficialBandQuality(n){ n=sgOfficialBandName(n)||sgNormName(n); if(n==='official-high'||n==='high')return 'high'; if(n==='official-mid'||n==='mid')return 'mid'; if(n==='official-low'||n==='low')return 'low'; return ''; }
  function sgCapLabel(tag){ var map={reasoning:'sgFeat_reasoning',tools:'sgFeat_tools',document:'sgFeat_document',vision:'sgFeat_vision',audio:'sgFeat_audio',code:'sgFeat_code',search:'sgFeat_search'}; return map[tag]?t(map[tag]):tag; }
  function sgFormatCaps(tags){ return (tags||[]).map(function(x){return sgCapLabel(x);}).join(', ')||'-'; }
  function sgModelLabel(n){ n=sgCanonicalModelName(n); if(n==='official-high')return t('sgTierHigh'); if(n==='official-mid')return t('sgTierMid'); if(n==='official-low')return t('sgTierLow'); return n; }
  function sgDefaultDynamicRoutes(){
    return [
      {class:'plan',model:'official-high',quality:'high'},
      {class:'design',model:'official-high',quality:'high'},
      {class:'review',model:'official-high',quality:'high'},
      {class:'doc_write',model:'official-mid',quality:'mid'},
      {class:'code',model:'official-mid',quality:'mid'},
      {class:'ops',model:'official-mid',quality:'mid'},
      {class:'balanced',model:'official-mid',quality:'mid'},
      {class:'chat',model:'official-low',quality:'low'},
      {class:'classify',model:'official-low',quality:'low'}
    ];
  }
  function sgDefaultBillingMultiplier(name){
    name=sgCanonicalModelName(name);
    if(name==='official-low'||name==='low') return 0.5;
    if(name==='official-high'||name==='high') return 2;
    return 1;
  }
  function sgEffectiveBillingMultiplier(model){
    var n=Number(model&&model.billing_multiplier);
    if(n>0&&isFinite(n)) return n;
    return sgDefaultBillingMultiplier(model&&model.name);
  }
  function sgEmptyModel(name){name=name||'auto';return{name:name,provider_ids:[],provider_configs:[],capability_tags:[],priority:50,resolution_tier:0,credit_multiplier:1,billing_multiplier:sgDefaultBillingMultiplier(name)};}
  function sgEnsureModel(d,name){
    name=sgCanonicalModelName(name);
    if(!d)return null;
    d.models=d.models||[];
    for(var i=0;i<d.models.length;i++){ if(sgCanonicalModelName(d.models[i].name)===name){ d.models[i].name=name; return d.models[i]; } }
    var m=sgEmptyModel(name); d.models.push(m); return m;
  }
  function sgEnsureModelsForRoutes(d){
    if(!d)return;
    sgEnsureModel(d,'auto');
    (d.routes||[]).forEach(function(r){ if(r&&r.model) sgEnsureModel(d,r.model); });
    sgOfficialBandNames().forEach(function(n){ sgEnsureModel(d,n); });
  }
  function sgDedupeModels(d){
    if(!d)return;
    var seen={}, keep=[];
    (d.models||[]).forEach(function(m){
      var name=sgCanonicalModelName(m&&m.name);
      if(seen[name])return;
      seen[name]=true; m.name=name; keep.push(m);
    });
    d.models=keep;
  }
  function sgIsLockedModelName(name){ name=sgCanonicalModelName(name); return name==='auto'||!!sgOfficialBandName(name); }
  function sgFillEmptyOfficialBandsFromAuto(d){
    if(!d)return;
    var auto=sgEnsureModel(d,'auto');
    var src=sgProviderConfigsFromModel(auto);
    if(!src.length)return;
    sgOfficialBandNames().forEach(function(name){
      var m=sgEnsureModel(d,name);
      if((m.provider_configs||[]).length)return;
      m.provider_configs=src.map(function(c){return{provider_id:c.provider_id,model:c.model,billing_mode:c.billing_mode,capability_tags:(c.capability_tags||[]).slice(),priority:c.priority,resolution_tier:c.resolution_tier,credit_multiplier:c.credit_multiplier,token_pricing_override:c.token_pricing_override===true,token_pricing:sgNormalizeTokenPricing(c.token_pricing)};});
      m.provider_ids=sgProviderIDsFromModel(m);
    });
  }
  function sgSyncOfficialRouteQuality(d){
    (d&&d.routes||[]).forEach(function(r){
      var q=sgOfficialBandQuality(r.model);
      if(q) r.quality=q;
      if((r.class==='plan'||r.class==='design') && q==='low'){ r.model='official-high'; r.quality='high'; }
    });
  }
  function sgSortModels(d){
    if(!d||!d.models)return;
    var order={'auto':0,'official-high':1,'official-mid':2,'official-low':3};
    d.models.sort(function(a,b){ var an=sgCanonicalModelName(a.name), bn=sgCanonicalModelName(b.name); return (order[an]!=null?order[an]:10)-(order[bn]!=null?order[bn]:10)||an.localeCompare(bn); });
  }
  function sgPrepareDynamicDraft(d,opts){
    if(!d||d.kind!=='dynamic')return;
    if(!(d.routes||[]).length)d.routes=sgDefaultDynamicRoutes();
    sgDedupeModels(d);
    sgEnsureModelsForRoutes(d);
    if(opts&&opts.fillEmptyOfficial)sgFillEmptyOfficialBandsFromAuto(d);
    sgSyncOfficialRouteQuality(d);
    sgSortModels(d);
  }
  function sgModelsNeedingProvider(d){
    return (d&&d.models||[]).filter(function(m){ return !sgProviderConfigsFromModel(m).length; }).map(function(m){ return m.name; });
  }
  function sgWorkloadModelChoices(d,selected,cls){
    var names=['official-high','official-mid','official-low'];
    if(cls==='plan'||cls==='design') names=['official-high'];
    return names.map(function(n){ return '<option value="'+esc(n)+'"'+(sgCanonicalModelName(selected)===n?' selected':'')+'>'+esc(sgModelLabel(n))+'</option>'; }).join('');
  }
  function sgFrozenClasses(){ return ['plan','design','review','doc_write','code','ops','chat','classify']; }
  function sgClassLabel(cls){ var key='sgClass_'+String(cls||''); var v=t(key); return v===key?String(cls||''):v; }
  function sgSourceLabel(src){ var key='sgSrc_'+String(src||''); var v=t(key); return v===key?String(src||''):v; }
  function sgQualityLabel(q){ return q||''; }
  function sgSectionHead(title, hint){ return '<div class="sg-section-head"><div class="sg-section-copy"><div class="sg-section-title">'+esc(title)+'</div>'+(hint?'<div class="sg-section-hint">'+esc(hint)+'</div>':'')+'</div></div>'; }

  var sgRouteDrag=null;
  var sgRouteDropTo=null;
  var sgRouteDragScrollAt=0;
  function sgRouteInsertAt(list, clientY){
    var cards=list?list.querySelectorAll('.sg-provider-card'):[];
    for(var i=0;i<cards.length;i++){
      var rect=cards[i].getBoundingClientRect();
      if(clientY<rect.top+rect.height/2) return i;
    }
    return cards.length;
  }
  function sgRouteFinalIndex(from, insertAt, length){
    if(insertAt>from) insertAt-=1;
    if(insertAt<0) insertAt=0;
    if(length<1) return 0;
    if(insertAt>length-1) insertAt=length-1;
    return insertAt;
  }
  function clearSgRouteDropMarks(root){
    if(!root) return;
    root.querySelectorAll('.sg-provider-card.is-drop-before, .sg-provider-card.is-drop-after').forEach(function(node){
      node.classList.remove('is-drop-before');
      node.classList.remove('is-drop-after');
    });
  }
  function clearSgRouteDragMarks(root){
    if(!root) return;
    root.querySelectorAll('.sg-provider-card.is-dragging').forEach(function(node){ node.classList.remove('is-dragging'); });
    clearSgRouteDropMarks(root);
  }
  function sgRouteDragAutoScroll(event){
    var now=Date.now();
    if(now-sgRouteDragScrollAt<40) return;
    var scroller=document.querySelector('#llmDialogContent .sg-dialog-body');
    if(!scroller) return;
    var rect=scroller.getBoundingClientRect();
    var edge=18;
    var dy=0;
    if(event.clientY<rect.top+edge) dy=-Math.ceil((rect.top+edge-event.clientY)/2);
    else if(event.clientY>rect.bottom-edge) dy=Math.ceil((event.clientY-(rect.bottom-edge))/2);
    if(!dy) return;
    if(dy>24) dy=24;
    if(dy<-24) dy=-24;
    sgRouteDragScrollAt=now;
    scroller.scrollTop+=dy;
  }
  function sgRoutePressStartsDrag(node){
    return !(node&&node.closest('button, a, input, select, textarea, label, .sg-provider-meta'));
  }
  function markSgRouteGap(list, insertAt){
    if(!list) return;
    list.querySelectorAll('.is-drop-before, .is-drop-after').forEach(function(node){
      node.classList.remove('is-drop-before');
      node.classList.remove('is-drop-after');
    });
    var cards=list.querySelectorAll('.sg-provider-card');
    if(!cards.length) return;
    if(insertAt<=0) cards[0].classList.add('is-drop-before');
    else if(insertAt>=cards.length) cards[cards.length-1].classList.add('is-drop-after');
    else cards[insertAt].classList.add('is-drop-before');
  }
  function bindServiceGroupRouteDrag(){
    var root=document.getElementById('llmDialogOverlay');
    if(!root||root.getAttribute('data-sg-route-drag')==='1') return;
    root.setAttribute('data-sg-route-drag','1');
    var sgRouteIgnoreClickUntil=0;
    document.addEventListener('pointerdown', function(){
      sgRouteIgnoreClickUntil=0;
    }, true);
    document.addEventListener('click', function(event){
      if(Date.now()>sgRouteIgnoreClickUntil) return;
      sgRouteIgnoreClickUntil=0;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
    }, true);
    root.addEventListener('pointerdown', function(event){
      var node=dragEventNode(event);
      var card=node&&node.closest('.sg-provider-card');
      if(!card||!root.contains(card)||!card.classList.contains('is-draggable')) return;
      card.setAttribute('draggable', sgRoutePressStartsDrag(node)?'true':'false');
    }, true);
    root.addEventListener('dragstart', function(event){
      var node=dragEventNode(event);
      var card=node&&node.closest('.sg-provider-card');
      if(!card||!root.contains(card)) return;
      if(!sgRoutePressStartsDrag(node)){ event.preventDefault(); return; }
      if(card.getAttribute('draggable')!=='true'){ event.preventDefault(); return; }
      var row=card.getAttribute('data-sg-row');
      var index=Number(card.getAttribute('data-sg-index'));
      if(row==null||!isFinite(index)){ event.preventDefault(); return; }
      sgRouteDropTo=null;
      sgRouteDragScrollAt=0;
      sgRouteDrag={row:String(row), index:index};
      card.classList.add('is-dragging');
      if(!event.dataTransfer) return;
      event.dataTransfer.effectAllowed='move';
      try{ event.dataTransfer.setData('text/plain', row+':'+index); }catch(e){}
    });
    function allowSgRouteDrop(event){
      if(!sgRouteDrag) return;
      sgRouteDragAutoScroll(event);
      var node=dragEventNode(event);
      var list=node&&node.closest('.sg-provider-list');
      if(!list||!root.contains(list)||list.getAttribute('data-sg-row')!==sgRouteDrag.row){
        clearSgRouteDropMarks(root);
        return;
      }
      event.preventDefault();
      if(event.dataTransfer) event.dataTransfer.dropEffect='move';
      markSgRouteGap(list, sgRouteInsertAt(list, event.clientY));
    }
    root.addEventListener('dragenter', allowSgRouteDrop);
    root.addEventListener('dragover', allowSgRouteDrop);
    root.addEventListener('drop', function(event){
      if(!sgRouteDrag) return;
      var node=dragEventNode(event);
      var list=node&&node.closest('.sg-provider-list');
      var same=!!(list&&root.contains(list)&&list.getAttribute('data-sg-row')===sgRouteDrag.row);
      if(!same){ sgRouteDropTo=null; return; }
      event.preventDefault();
      sgRouteDropTo=sgRouteFinalIndex(sgRouteDrag.index, sgRouteInsertAt(list, event.clientY), list.querySelectorAll('.sg-provider-card').length);
    });
    root.addEventListener('dragend', function(){
      var drag=sgRouteDrag;
      var to=sgRouteDropTo;
      sgRouteDrag=null;
      sgRouteDropTo=null;
      clearSgRouteDragMarks(root);
      if(drag&&to!=null&&to!==drag.index) window.sgMoveProviderTo(Number(drag.row), drag.index, to);
      if(drag) sgRouteIgnoreClickUntil=Date.now()+80;
    });
  }
  function sgRenderProviderCard(rowIndex,routeIndex,total){
    var model=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];
    var cfg=sgGetProviderConfig(model,routeIndex);
    if(!cfg)return '';
    var features=sgFormatCaps(cfg&&cfg.capability_tags);
    var billingLabel = cfg.billing_mode ? cfg.billing_mode : 'legacy';
    var pricingBrief = sgFormatPricingBrief(cfg.token_pricing);
    var pricingMeta = pricingBrief ? ' \u00b7 '+esc(billingLabel)+' \u00b7 '+esc(pricingBrief) : ' \u00b7 '+esc(billingLabel);
    var array = providerArrayByID(cfg.provider_id);
    var memberMeta = array && array.members.length > 1 ? ' \u00b7 '+esc(array.members.map(function(member){ return member.name || member.id; }).join(', ')) : '';
    var canDrag=total>1;
    return '<div class="sg-provider-card'+(canDrag?' is-draggable':'')+'"'+(canDrag?' draggable="true"':'')+' data-sg-row="'+rowIndex+'" data-sg-index="'+routeIndex+'">'
      +(canDrag?'<span class="sg-provider-grip" title="'+esc(t('sgArrayDragHint'))+'" aria-hidden="true"></span>':'')
      +'<div class="sg-provider-body"><div class="sg-row-head"><strong>'+esc(sgProviderName(cfg.provider_id))+' #'+(routeIndex+1)+'</strong>'
      +'<div class="sg-actions"><button class="btn-ghost sg-tiny-btn" onclick="sgEditProviderConfig('+rowIndex+','+routeIndex+')">'+esc(t('editGroup'))+'</button>'
      +(routeIndex>0?'<button type="button" class="btn-ghost sg-icon-btn" data-sg-move="-1" aria-label="'+esc(t('sgMoveEarlier'))+'" onclick="sgMoveProvider('+rowIndex+','+routeIndex+',-1)">\u2191</button>':'')
      +(routeIndex<total-1?'<button type="button" class="btn-ghost sg-icon-btn" data-sg-move="1" aria-label="'+esc(t('sgMoveLater'))+'" onclick="sgMoveProvider('+rowIndex+','+routeIndex+',1)">\u2193</button>':'')
      +'<button class="btn-danger-ghost sg-tiny-btn" onclick="sgRemoveProvider('+rowIndex+','+routeIndex+')">\u2715</button></div></div>'
      +'<div class="sg-provider-meta">Upstream: '+esc((cfg&&cfg.model)||'-')+' \u00b7 '+esc(t('fieldCapabilities'))+': '+esc(features)+' \u00b7 P:'+(cfg&&cfg.priority||0)+pricingMeta+memberMeta+'</div></div></div>';
  }
  function sgRenderRouteRow(model,rowIndex){
    model.provider_configs=sgProviderConfigsFromModel(model);
    var locked=sgDraft&&sgDraft.kind==='dynamic'&&sgIsLockedModelName(model.name);
    var total=(model.provider_configs||[]).length;
    var cards=(model.provider_configs||[]).map(function(cfg,pi){return sgRenderProviderCard(rowIndex,pi,total);}).join('');
    var routeArrays=providerArrayRecords().filter(function(array){ return array.members && array.members.length; });
    var providerOptions=!routeArrays.length?'<option value="">('+esc(t('noProviders'))+')</option>'
      :'<option value="">-- '+esc(t('chooseProvider'))+' --</option>'+routeArrays.map(function(array){
        var label = array.name || array.id;
        if (array.members.length > 1) label += ' (' + array.members.length + ')';
        return '<option value="'+esc(array.id)+'">'+esc(label)+'</option>';
      }).join('');
    var fee=sgEffectiveBillingMultiplier(model);
    return '<div class="sg-route-card"><div class="sg-row-head"><div><strong>'+esc(sgModelLabel(model.name||'auto'))+'</strong><span class="sg-route-hint">'+esc(t('sgRouteHint'))+'</span></div>'
      +(locked?'':'<button class="btn-danger-ghost sg-remove-route" onclick="sgRemoveRoute('+rowIndex+')">'+esc(t('sgRemoveRoute'))+'</button>')
      +'</div><div class="sg-route-grid"><div><label class="sg-label-sm">'+esc(t('sgExposedModel'))+'</label>'
      +'<input class="sg-field-full" value="'+esc(model.name||'auto')+'"'+(locked?' disabled':'')+' oninput="sgSetRouteField('+rowIndex+',\'name\',this.value)"></div>'
      +'<div><label class="sg-label-sm">'+esc(t('sgFeeMultiplier'))+'</label><input class="sg-field-full" type="number" min="0.01" step="0.01" value="'+esc(String(fee))+'" oninput="sgSetRouteField('+rowIndex+',\'billing_multiplier\',this.value)"></div>'
      +'<div class="sg-provider-add"><select id="sgProviderAdd'+rowIndex+'">'+providerOptions+'</select><button class="btn-ghost" onclick="sgAddProviderToRoute('+rowIndex+')">+</button></div></div>'
      +(cards?'<div class="sg-provider-list" data-sg-row="'+rowIndex+'">'+cards+'</div>':'<div class="sg-empty-provider">'+esc(t('sgNoProviders'))+'</div>')+'</div>';
  }
  function sgRenderWorkloadTable(d){
    var rows=(d.routes||sgDefaultDynamicRoutes()).map(function(r,i){
      return '<div class="sg-wl-row"><div class="sg-wl-class"><strong>'+esc(sgClassLabel(r.class))+'</strong><span class="mono">'+esc(r.class)+'</span></div>'
        +'<select onchange="sgSetWorkloadRoute('+i+',this.value)">'+sgWorkloadModelChoices(d,r.model,r.class)+'</select>'
        +'<div>'+esc(sgOfficialBandQuality(r.model)||r.quality||'')+'</div></div>';
    }).join('');
    return '<div class="sg-section">'+sgSectionHead(t('sgWorkloadTitle'), t('sgWorkloadHint'))+'<div class="sg-wl-table"><div class="sg-wl-head"><span>Class</span><span>Band</span><span>Quality</span></div>'+rows+'</div></div>';
  }
  function sgRenderCatalog(d){
    var names=(d.exposed_models||[]).join(', ');
    return '<div class="sg-section">'+sgSectionHead(t('sgCatalogTitle'), t('sgCatalogHint'))
      +'<div class="sg-form-grid"><div><label>'+esc(t('sgExposedModel'))+'</label><input id="sgFieldExposed" value="'+esc(names)+'" oninput="sgSetExposedModels(this.value)"></div>'
      +'<div><label>'+esc(t('sgQualityFloor'))+'</label><select id="sgFieldFloor" onchange="sgSetField(\'quality_floor\',this.value)">'
      +'<option value=""'+(!d.quality_floor?' selected':'')+'>'+esc(t('sgQualityFloorNone'))+'</option>'
      +'<option value="high"'+(d.quality_floor==='high'?' selected':'')+'>high</option>'
      +'<option value="mid"'+(d.quality_floor==='mid'?' selected':'')+'>mid</option>'
      +'<option value="low"'+(d.quality_floor==='low'?' selected':'')+'>low</option></select></div></div></div>';
  }
  function sgRenderGroupDialog(opts){
    var d=sgDraft||sgEmptyGroup();
    if(d.kind==='dynamic') sgPrepareDynamicDraft(d);
    var title=sgMode==='edit'?t('groupDialogTitleEdit'):t('groupDialogTitleNew');
    var agentOptions = agents.map(function(a){return '<option value="'+esc(a.id)+'"'+(d.agent_id===a.id?' selected':'')+'>'+esc(a.name||a.id)+'</option>';}).join('');
    var dragHint='';
    var routeModels=d.models||[];
    for(var ri=0;ri<routeModels.length;ri++){
      if(sgProviderConfigsFromModel(routeModels[ri]).length>1){ dragHint=t('sgArrayOrderHint'); break; }
    }
    var rows=routeModels.map(function(m,i){return sgRenderRouteRow(m,i);}).join('');
    var body='<div class="sg-form-grid">'
      +'<div><label>'+esc(t('fieldGroupID'))+'</label><input id="sgFieldID" value="'+esc(d.id)+'"'+(sgMode==='edit'?' readonly class="sg-readonly"':'')+' oninput="sgSetField(\'id\',this.value)"></div>'
      +'<div><label>'+esc(t('fieldGroupName'))+'</label><input id="sgFieldName" value="'+esc(d.name)+'" oninput="sgSetField(\'name\',this.value)"></div></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupAgent'))+'</label><select id="sgFieldAgent" class="sg-field-full" onchange="sgSetField(\'agent_id\',this.value)"><option value="">--</option>'+agentOptions+'</select></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupDesc'))+'</label><input id="sgFieldDesc" class="sg-field-full" value="'+esc(d.description)+'" oninput="sgSetField(\'description\',this.value)"></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('sgAccessPolicy'))+'</label><select id="sgFieldPolicy" onchange="sgSetField(\'access_policy\',this.value)"><option value="free"'+(d.access_policy!=='grant_required'?' selected':'')+'>'+esc(sgPolicyLabel('free'))+' ('+esc(t('sgPolicyFreeHint'))+')</option><option value="grant_required"'+(d.access_policy==='grant_required'?' selected':'')+'>'+esc(sgPolicyLabel('grant_required'))+' ('+esc(t('sgPolicyGrantHint'))+')</option></select></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupKind'))+'</label><select id="sgFieldKind" onchange="sgSetKind(this.value)"><option value="dynamic"'+(d.kind==='dynamic'?' selected':'')+'>'+esc(t('sgKindDynamic'))+'</option><option value="static"'+(d.kind!=='dynamic'?' selected':'')+'>'+esc(t('sgKindStatic'))+'</option></select></div>'
      +(d.kind==='dynamic'?sgRenderWorkloadTable(d)+sgRenderCatalog(d):'')
      +'<div class="sg-section">'+sgSectionHead(t('sgRoutes'), dragHint)+'<div class="sg-flex-between"><span></span><button class="btn-ghost" onclick="sgAddRoute()">'+esc(t('sgAddRoute'))+'</button></div>'+rows+'</div>';
    sgOpenKind='group';
    var reveal=opts&&opts.revealRoute;
    var keepScroll=!!(opts&&opts.keepScroll);
    var scrollTop=0;
    var prevBody=(keepScroll&&!reveal)?document.querySelector('#llmDialogContent .sg-dialog-body'):null;
    if(prevBody) scrollTop=prevBody.scrollTop;
    if(reveal&&isFinite(Number(reveal.top))) scrollTop=Number(reveal.top);
    openDialog(sgDialogChrome(title, body, '<button class="btn-primary" onclick="sgSaveGroup()">'+esc(t('save'))+'</button><button class="btn-ghost" onclick="sgCloseCurrentDialog()">'+esc(t('cancel'))+'</button>'), 'sg-form-dialog', (keepScroll||reveal)?false:true);
    bindServiceGroupRouteDrag();
    if(!keepScroll&&!reveal) return;
    var next=document.querySelector('#llmDialogContent .sg-dialog-body');
    if(!next) return;
    next.scrollTop=scrollTop;
    var pinned=scrollTop;
    var fm=opts&&opts.focusMove;
    if(fm){
      var moved=document.querySelector('#llmDialogContent .sg-provider-card[data-sg-row="'+Number(fm.row)+'"][data-sg-index="'+Number(fm.index)+'"]');
      var again=moved&&moved.querySelector('[data-sg-move="'+(Number(fm.delta)<0?'-1':'1')+'"]');
      if(again&&again.focus) again.focus({preventScroll:true});
      else if(moved&&moved.focus){ moved.setAttribute('tabindex','-1'); moved.focus({preventScroll:true}); }
    }
    if(reveal&&isFinite(Number(reveal.row))&&isFinite(Number(reveal.index))){
      var card=document.querySelector('#llmDialogContent .sg-provider-card[data-sg-row="'+Number(reveal.row)+'"][data-sg-index="'+Number(reveal.index)+'"]');
      if(card){
        var bodyRect=next.getBoundingClientRect();
        var cardRect=card.getBoundingClientRect();
        var pad=12;
        var top=next.scrollTop;
        if(cardRect.top<bodyRect.top+pad) top+=cardRect.top-bodyRect.top-pad;
        else if(cardRect.bottom>bodyRect.bottom-pad) top+=cardRect.bottom-bodyRect.bottom+pad;
        if(top<0) top=0;
        next.scrollTop=top;
        pinned=next.scrollTop;
        if(!fm&&card.focus){ card.setAttribute('tabindex','-1'); card.focus({preventScroll:true}); }
      }
    }
    if(reveal&&!fm){
      var shell=document.getElementById('llmDialogContent');
      var active=document.activeElement;
      if(shell&&(!active||!shell.contains(active))&&shell.focus){
        shell.setAttribute('tabindex','-1');
        shell.focus({preventScroll:true});
      }
    }
    requestAnimationFrame(function(){ if(next.isConnected) next.scrollTop=pinned; });
  }
  function sgCaptureEditingPlace(){
    var body=document.querySelector('#llmDialogContent .sg-dialog-body');
    return {top:body?body.scrollTop:0};
  }
  function sgReturnToGroupEditor(){
    var back=sgGroupReturn;
    sgGroupReturn=null;
    sgProviderDraft=null;
    sgOpenKind='group';
    sgRenderGroupDialog(back?{revealRoute:back}:null);
  }
  window.showGroupDialog=function(mode,id){
    var g=mode==='edit'?serviceGroups.find(function(x){return x.id===id;}):null;
    sgMode=mode==='edit'?'edit':'create';
    sgDraft=g?sgCloneGroup(g):sgEmptyGroup();
    if(sgDraft.kind==='dynamic') sgPrepareDynamicDraft(sgDraft,{fillEmptyOfficial:true});
    var mem=sgMode==='edit'?sgGroupScrollMemory[String(sgDraft.id||'')]:null;
    sgRenderGroupDialog(mem?{revealRoute:mem}:null);
  };
  window.editLLMServiceGroup=function(id){window.showGroupDialog('edit',id);};
  window.showLLMGroupEditor=function(){window.showGroupDialog('create');};
  window.sgSetField=function(k,v){if(sgDraft)sgDraft[k]=typeof v==='string'?v.trim():v;};
  window.sgSetKind=function(v){ if(!sgDraft)return; sgDraft.kind=v==='dynamic'?'dynamic':'static'; if(sgDraft.kind==='dynamic')sgPrepareDynamicDraft(sgDraft,{fillEmptyOfficial:true}); sgRenderGroupDialog(); };
  window.sgSetExposedModels=function(v){ if(sgDraft) sgDraft.exposed_models=String(v||'').split(/[,;\s]+/).map(function(x){return x.trim();}).filter(Boolean); };
  window.sgSetWorkloadRoute=function(i,v){
    if(!sgDraft||!sgDraft.routes||!sgDraft.routes[i])return;
    if((sgDraft.routes[i].class==='plan'||sgDraft.routes[i].class==='design')&&sgOfficialBandQuality(v)==='low'){ toast(t('sgPlanDesignNoLow'),'error'); sgRenderGroupDialog(); return; }
    sgDraft.routes[i].model=v; sgDraft.routes[i].quality=sgOfficialBandQuality(v);
    sgEnsureModel(sgDraft,v); sgPrepareDynamicDraft(sgDraft); sgRenderGroupDialog();
  };
  window.sgSetRouteField=function(i,k,v){
    if(!sgDraft||!sgDraft.models||!sgDraft.models[i])return;
    if(k==='name'&&sgIsLockedModelName(sgDraft.models[i].name)){ toast(t('sgProtectedModel'),'info'); return; }
    if(k==='billing_multiplier'){ var n=Number(v); sgDraft.models[i].billing_multiplier=(isFinite(n)&&n>0)?n:0; return; }
    sgDraft.models[i][k]=String(v||'').trim();
  };
  window.sgAddRoute=function(){if(!sgDraft)sgDraft=sgEmptyGroup();sgDraft.models.push(sgEmptyModel('auto'));sgRenderGroupDialog();};
  window.sgRemoveRoute=function(i){var m=sgDraft&&sgDraft.models&&sgDraft.models[i]; if(m&&sgIsLockedModelName(m.name)){toast(t('sgProtectedModel'),'info');return;} if(sgDraft&&sgDraft.models){sgDraft.models.splice(i,1);sgRenderGroupDialog();}};
  window.sgAddProviderToRoute=function(i){var sel=document.getElementById('sgProviderAdd'+i);var id=sel&&sel.value;if(!id){toast(t('chooseProvider'),'info');return;}var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;m.provider_configs=sgProviderConfigsFromModel(m);if(sgRouteDuplicateIndex(m,{provider_id:id,model:''},-1)>=0){toast(sgDuplicateRouteMessage({provider_id:id,model:''}),'error');return;}m.provider_configs.push({provider_id:id,model:'',billing_mode:'',capability_tags:[],priority:0,resolution_tier:0,credit_multiplier:1,token_pricing:{}});m.provider_ids=sgProviderIDsFromModel(m);if(sgDraft.kind==='dynamic')sgFillEmptyOfficialBandsFromAuto(sgDraft);sgRenderGroupDialog();};
  window.sgMoveProviderTo=function(rowIndex,fromIndex,toIndex,focusMove){
    var m=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];
    if(!m) return;
    m.provider_configs=sgProviderConfigsFromModel(m);
    var n=m.provider_configs.length;
    fromIndex=Number(fromIndex);
    toIndex=Number(toIndex);
    if(!isFinite(fromIndex)||!isFinite(toIndex)) return;
    if(fromIndex<0||toIndex<0||fromIndex>=n||toIndex>=n||fromIndex===toIndex) return;
    var item=m.provider_configs.splice(fromIndex,1)[0];
    m.provider_configs.splice(toIndex,0,item);
    m.provider_ids=sgProviderIDsFromModel(m);
    sgRenderGroupDialog({keepScroll:true, focusMove:focusMove||null});
  };
  window.sgMoveProvider=function(i,routeIndex,delta){
    var to=Number(routeIndex)+Number(delta);
    window.sgMoveProviderTo(i, routeIndex, to, {row:Number(i), index:to, delta:Number(delta)});
  };
  window.sgRemoveProvider=function(i,routeIndex){var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;m.provider_configs=sgProviderConfigsFromModel(m);m.provider_configs.splice(routeIndex,1);m.provider_ids=sgProviderIDsFromModel(m);sgRenderGroupDialog();};
  function sgClonePricingForDraft(tp){
    var p=sgNormalizeTokenPricing(tp||{});
    var out={input_credits_per_10k:p.input_credits_per_10k,output_credits_per_10k:p.output_credits_per_10k,cache_read_credits_per_10k:p.cache_read_credits_per_10k,cache_write_credits_per_10k:p.cache_write_credits_per_10k,input_rmb_per_10k:p.input_rmb_per_10k,output_rmb_per_10k:p.output_rmb_per_10k,cache_read_rmb_per_10k:p.cache_read_rmb_per_10k,cache_write_rmb_per_10k:p.cache_write_rmb_per_10k,minimum_request_credits:p.minimum_request_credits,timezone:p.timezone||'',version:p.version||''};
    if(p.price_schedule) out.price_schedule=p.price_schedule;
    return out;
  }
  window.sgEditProviderConfig=function(rowIndex,routeIndex){var model=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];var cfg=sgGetProviderConfig(model,routeIndex);if(!cfg)return;var body=document.querySelector('#llmDialogContent .sg-dialog-body');sgGroupReturn={top:body?body.scrollTop:0,row:Number(rowIndex),index:Number(routeIndex)};sgOpenKind='provider-config';sgProviderDraft={rowIndex:rowIndex,routeIndex:routeIndex,providerID:cfg.provider_id,draft:{model:cfg&&cfg.model||'',billing_mode:String(cfg&&cfg.billing_mode||'').trim(),capability_tags:(cfg&&cfg.capability_tags||[]).slice(),priority:cfg&&cfg.priority||0,resolution_tier:cfg&&cfg.resolution_tier||0,credit_multiplier:cfg&&cfg.credit_multiplier||1,token_pricing_override:cfg&&cfg.token_pricing_override===true,token_pricing:sgClonePricingForDraft(cfg.token_pricing)}};sgRenderProviderDialog();};
  function sgRenderProviderDialog(){
    if(!sgProviderDraft)return;var d=sgProviderDraft.draft;
    var tp=d.token_pricing||{};
    var featureChecks=sgCapabilityOptions.map(function(f){var checked=(d.capability_tags||[]).indexOf(f)>=0?' checked':'';return'<label class="sg-feature-check"><input type="checkbox"'+checked+' onchange="sgToggleFeature(\''+f+'\',this.checked)">'+esc(sgCapLabel(f))+'</label>';}).join('');
    var extraTags=(d.capability_tags||[]).filter(function(v){return sgCapabilityOptions.indexOf(v)<0;}).join(', ');
    var modelOptions=sgProviderModels(sgProviderDraft.providerID);
    var modelField=modelOptions.length
      ? '<select class="sg-field-full" onchange="sgSetProviderField(\'model\',this.value)"><option value="">provider default</option>'+modelOptions.map(function(v){return'<option value="'+esc(v)+'"'+(d.model===v?' selected':'')+'>'+esc(v)+'</option>';}).join('')+'</select>'
      : '<input class="sg-field-full" value="'+esc(d.model||'')+'" oninput="sgSetProviderField(\'model\',this.value)">';
    var billingMode = String(d.billing_mode||'').trim();
    var html=sgDialogChrome(t('sgProviderConfigTitle')+': '+sgProviderName(sgProviderDraft.providerID),
      '<div class="hint">'+esc(t('sgCapabilityHint'))+'</div>'
      +'<div class="sg-block-xs"><label class="sg-label-sm">Upstream model</label>'+modelField+'</div>'
      +'<div class="sg-block-sm"><label class="sg-label-strong">'+esc(t('sgCapabilityTags'))+'</label><div class="sg-block-xs">'+featureChecks+'</div></div>'
      +'<div class="sg-block-xs"><label class="sg-label-sm">'+esc(t('sgExtraTags'))+'</label><input class="sg-field-full" value="'+esc(extraTags)+'" oninput="sgSetExtraTags(this.value)"></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('sgPriority'))+'</label><select onchange="sgSetProviderField(\'priority\',Number(this.value))">'+sgPriorityOptions.map(function(v){return'<option value="'+v+'"'+(d.priority===v?' selected':'')+'>'+v+'</option>';}).join('')+'</select></div>'
      +'<div><label class="sg-label-sm">'+esc(t('sgResolutionTier'))+'</label><select onchange="sgSetProviderField(\'resolution_tier\',Number(this.value))">'+sgResolutionOptions.map(function(v){return'<option value="'+v+'"'+(d.resolution_tier===v?' selected':'')+'>'+v+'</option>';}).join('')+'</select></div></div>'
      +'<div class="sg-block-xs"><label class="sg-label-sm">'+esc(t('sgCreditMultiplier'))+'</label><select onchange="sgSetProviderField(\'credit_multiplier\',Number(this.value))">'+sgMultiplierOptions.map(function(v){return'<option value="'+v+'"'+(d.credit_multiplier===v?' selected':'')+'>'+v+'</option>';}).join('')+'</select></div>'
      +'<div class="sg-block-sm sg-token-pricing-section"><div class="sg-label-strong">'+esc(t('tokenPricingTitle'))+'</div><div class="hint">'+esc(t('tokenPricingHint'))+'</div>'
      +'<label class="sg-feature-check"><input type="checkbox"'+(d.token_pricing_override?' checked':'')+' onchange="sgSetProviderField(\'token_pricing_override\',this.checked)">'+esc(t('sgPricingOverride'))+'</label>'
       +'<div class="sg-form-grid sg-form-grid-tight"><div><label class="sg-label-sm">'+esc(t('sgBillingMode'))+'</label><select onchange="sgSetProviderField(\'billing_mode\',this.value)"><option value=""'+(billingMode===''?' selected':'')+'>'+esc(t('sgBillingModeLegacy'))+'</option><option value="paid"'+(billingMode==='paid'?' selected':'')+'>'+esc(t('sgBillingModePaid'))+'</option><option value="free"'+(billingMode==='free'?' selected':'')+'>'+esc(t('sgBillingModeFree'))+'</option></select><div class="hint">'+esc(t('sgBillingModeHint'))+'</div></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldPricingTimezone'))+'</label><input class="sg-field-full" value="'+esc(tp.timezone||'')+'" placeholder="Asia/Shanghai" oninput="sgSetTokenPricingField(\'timezone\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldInputCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.input_credits_per_10k!==undefined?String(tp.input_credits_per_10k):'')+'" placeholder="1" oninput="sgSetTokenPricingField(\'input_credits_per_10k\',this.value)"></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldOutputCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.output_credits_per_10k!==undefined?String(tp.output_credits_per_10k):'')+'" placeholder="4" oninput="sgSetTokenPricingField(\'output_credits_per_10k\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldCacheReadCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.cache_read_credits_per_10k!==undefined?String(tp.cache_read_credits_per_10k):'')+'" placeholder="input × 0.1" oninput="sgSetTokenPricingField(\'cache_read_credits_per_10k\',this.value)"></div><div><label class="sg-label-sm">'+esc(t('fieldCacheWriteCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.cache_write_credits_per_10k!==undefined?String(tp.cache_write_credits_per_10k):'')+'" placeholder="input" oninput="sgSetTokenPricingField(\'cache_write_credits_per_10k\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldInputRMB'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.input_rmb_per_10k!==undefined?String(tp.input_rmb_per_10k):'')+'" placeholder="0.02" oninput="sgSetTokenPricingField(\'input_rmb_per_10k\',this.value)"></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldOutputRMB'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.output_rmb_per_10k!==undefined?String(tp.output_rmb_per_10k):'')+'" placeholder="0.08" oninput="sgSetTokenPricingField(\'output_rmb_per_10k\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldCacheReadRMB'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.cache_read_rmb_per_10k!==undefined?String(tp.cache_read_rmb_per_10k):'')+'" placeholder="input × 0.1" oninput="sgSetTokenPricingField(\'cache_read_rmb_per_10k\',this.value)"></div><div><label class="sg-label-sm">'+esc(t('fieldCacheWriteRMB'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.cache_write_rmb_per_10k!==undefined?String(tp.cache_write_rmb_per_10k):'')+'" placeholder="input" oninput="sgSetTokenPricingField(\'cache_write_rmb_per_10k\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldMinimumCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.minimum_request_credits!==undefined?String(tp.minimum_request_credits):'')+'" placeholder="0.1" oninput="sgSetTokenPricingField(\'minimum_request_credits\',this.value)"></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldPricingVersion'))+'</label><input class="sg-field-full" value="'+esc(tp.version||'')+'" placeholder="2026-08-23-v1" oninput="sgSetTokenPricingField(\'version\',this.value)"></div></div>'
      +'</div>',
      '<button class="btn-primary" onclick="sgSaveProviderConfig()">'+esc(t('save'))+'</button><button class="btn-ghost" onclick="sgCancelProviderConfig()">'+esc(t('cancel'))+'</button>');
    openDialog(html, 'sg-form-dialog');
  }
  window.sgToggleFeature=function(f,on){if(!sgProviderDraft)return;var s=new Set(sgProviderDraft.draft.capability_tags||[]);if(on)s.add(f);else s.delete(f);sgProviderDraft.draft.capability_tags=Array.from(s);};
  window.sgSetExtraTags=function(v){if(!sgProviderDraft)return;var keep=(sgProviderDraft.draft.capability_tags||[]).filter(function(x){return sgCapabilityOptions.indexOf(x)>=0;});var extra=v.split(/[,;\s]+/).map(function(x){return x.trim();}).filter(Boolean);sgProviderDraft.draft.capability_tags=Array.from(new Set(keep.concat(extra)));};
  window.sgSetProviderField=function(k,v){if(sgProviderDraft)sgProviderDraft.draft[k]=(k==='model'||k==='billing_mode'?String(v||'').trim():v);};
  window.sgSetTokenPricingField=function(k,v){
    if(!sgProviderDraft||!sgProviderDraft.draft) return;
    var tp=sgProviderDraft.draft.token_pricing||(sgProviderDraft.draft.token_pricing={});
    if(k==='timezone' || k==='version'){ tp[k]=String(v||'').trim(); return; }
    if(v===''||v==null){ delete tp[k]; return; }
    var n=Number(v);
    if(!isFinite(n)||n<0){ tp[k]=v; return; }
    tp[k]=n;
  };
  window.sgSaveProviderConfig=function(){
    if(!sgProviderDraft||!sgDraft)return;var model=sgDraft.models&&sgDraft.models[sgProviderDraft.rowIndex];if(!model)return;var cfg=sgGetProviderConfig(model,sgProviderDraft.routeIndex);if(!cfg)return;
    var next={provider_id:cfg.provider_id,model:(sgProviderDraft.draft.model||'').trim()};if(sgRouteDuplicateIndex(model,next,sgProviderDraft.routeIndex)>=0){toast(sgDuplicateRouteMessage(next),'error');return;}
    // validate token pricing numbers
    var tp=sgProviderDraft.draft.token_pricing||{};
    for(var kk in tp){ if(tp.hasOwnProperty(kk) && (kk==='input_credits_per_10k'||kk==='output_credits_per_10k'||kk==='cache_read_credits_per_10k'||kk==='cache_write_credits_per_10k'||kk==='input_rmb_per_10k'||kk==='output_rmb_per_10k'||kk==='cache_read_rmb_per_10k'||kk==='cache_write_rmb_per_10k'||kk==='minimum_request_credits')){ if(tp[kk]!==''&&tp[kk]!==undefined){ var nn=Number(tp[kk]); if(!isFinite(nn)||nn<0){ toast(t('billingInvalid'),'error'); return; } } } }
    var billingMode=String(sgProviderDraft.draft.billing_mode||'').trim();
    if(billingMode==='paid'){
      var hasCredits = (tp.input_credits_per_10k!==undefined&&isFinite(tp.input_credits_per_10k)&&tp.input_credits_per_10k>0) || (tp.output_credits_per_10k!==undefined&&isFinite(tp.output_credits_per_10k)&&tp.output_credits_per_10k>0) || (tp.cache_read_credits_per_10k!==undefined&&isFinite(tp.cache_read_credits_per_10k)&&tp.cache_read_credits_per_10k>0) || (tp.cache_write_credits_per_10k!==undefined&&isFinite(tp.cache_write_credits_per_10k)&&tp.cache_write_credits_per_10k>0) || (tp.minimum_request_credits!==undefined&&isFinite(tp.minimum_request_credits)&&tp.minimum_request_credits>0);
      if(!hasCredits){ toast(t('billingPaidNeedsCredits'),'error'); return; }
    }
    cfg.model=next.model;cfg.billing_mode=billingMode;cfg.capability_tags=(sgProviderDraft.draft.capability_tags||[]).slice();cfg.priority=sgProviderDraft.draft.priority||0;cfg.resolution_tier=sgProviderDraft.draft.resolution_tier||0;cfg.credit_multiplier=sgProviderDraft.draft.credit_multiplier||1;cfg.token_pricing_override=sgProviderDraft.draft.token_pricing_override===true;
    var cleaned={}; for(var k in tp){ if(tp.hasOwnProperty(k)){ var val=tp[k]; if(val!==''&&val!==undefined&&val!==null){ if(k==='timezone'||k==='version'){ if(String(val).trim()) cleaned[k]=String(val).trim(); } else if(k==='price_schedule' && Array.isArray(val) && val.length){ try{ cleaned[k]=JSON.parse(JSON.stringify(val)); }catch(e){ cleaned[k]=val.slice(); } } else if(isFinite(Number(val))) cleaned[k]=Number(val); } } }
    if(cleaned.price_schedule && !cleaned.timezone) cleaned.timezone='Asia/Shanghai';
    if(cleaned.input_credits_per_10k===undefined && cleaned.output_credits_per_10k===undefined && cleaned.cache_read_credits_per_10k===undefined && cleaned.cache_write_credits_per_10k===undefined && cleaned.minimum_request_credits===undefined && cleaned.input_rmb_per_10k===undefined && cleaned.output_rmb_per_10k===undefined && cleaned.cache_read_rmb_per_10k===undefined && cleaned.cache_write_rmb_per_10k===undefined && !cleaned.timezone && !cleaned.version && !cleaned.price_schedule){
      // keep empty object for legacy; omit pricing entirely to stay legacy-compatible
      // but preserve an explicit empty to avoid sending stray keys
    }
    cfg.token_pricing=cleaned;
    sgReturnToGroupEditor();};
  window.sgCancelProviderConfig=function(){sgReturnToGroupEditor();};
  window.sgSaveGroup=async function(){
    if(sgSaveBusy||sgOpenKind!=='group')return;
    if(!sgDraft||!sgDraft.id||!sgDraft.name){toast(t('sgIDNameRequired'),'error');return;}
    if(!sgDraft.agent_id){toast(t('sgAgentRequired'),'error');return;}
    if(sgDraft.kind==='dynamic') sgPrepareDynamicDraft(sgDraft,{fillEmptyOfficial:true});
    if(sgMode!=='edit'){
      for(var r=0;r<(sgDraft.models||[]).length;r++){if(!(sgProviderConfigsFromModel(sgDraft.models[r])||[]).length){toast(t('sgRouteNeedsProvider'),'error');return;}}
    }
    var missing=sgModelsNeedingProvider(sgDraft);
    if(sgDraft.kind==='dynamic'&&missing.length){toast(t('sgRouteNeedsProvider'),'error');return;}
    var payload=sgCloneGroup(sgDraft);
    for(var i=0;i<(payload.models||[]).length;i++){
      var model=payload.models[i];
      model.provider_configs=sgProviderConfigsFromModel(model);
      for(var ri=0;ri<model.provider_configs.length;ri++){
        var dup=sgRouteDuplicateIndex(model,model.provider_configs[ri],ri);
        if(dup>=0){toast(sgDuplicateRouteMessage(model.provider_configs[ri]),'error');return;}
      }
      model.provider_ids=sgProviderIDsFromModel(model);
    }
    var unmatched=sgRoutesWithUnmatchedUpstream(payload);
    if(unmatched.length && !sgConfirm(t('sgRouteModelUnmatched')+'\n'+unmatched.join('\n')+'\n\n'+t('sgRouteModelUnmatchedAsk'))) return;
    sgSaveBusy=true;
    try{
      if(sgMode==='edit'){await api('/api/admin/llm/service-groups/'+encodeURIComponent(payload.id),{method:'PUT',body:JSON.stringify(payload)});}
      else{await api('/api/admin/llm/service-groups',{method:'POST',body:JSON.stringify(payload)});}
      sgGroupScrollMemory[String(payload.id||'')]=sgCaptureEditingPlace();
      sgRememberSavedGroup(payload);
      sgCloseCurrentDialog();
      sgPendingGroupScroll=String(payload.id||'');
      renderServiceGroups();
      sgPendingGroupScroll=String(payload.id||'');
      sgScrollServiceGroupRow(payload.id);
      toast(t('saved'),'success');
      loadServiceGroups();
    }catch(e){toast(e.message,'error');}
    finally{sgSaveBusy=false;}
  };

  function sgDialogAlive(kind,id){
    var overlay=document.getElementById('llmDialogOverlay');
    return !!(overlay&&overlay.classList.contains('show')&&sgOpenKind===kind&&sgDraft&&String(sgDraft.id||'')===String(id||''));
  }
  function sgTrafficDialogAlive(id){ return sgDialogAlive('traffic',id); }
  function sgTrafficWindowName(name){
    name=String(name||'');
    if(name==='week'||name==='7d') return 'week';
    if(name==='month'||name==='30d') return 'month';
    return 'day';
  }
  function sgWinButtons(id){
    var cur=sgTrafficWindowName(_sgTrafficDataWin||serviceGroupTrafficPeriod||'day');
    return ['day','week','month'].map(function(win){
      return '<button class="btn-ghost sg-traffic-win'+(cur===win?' is-active':'')+'" type="button" data-win="'+win+'" onclick="sgLoadClassTraffic('+jsArg(id)+','+jsArg(win)+')">'+esc(providerTrafficPeriodLabel(win))+'</button>';
    }).join('');
  }
  function sgFmtTryResult(data){
    if(!data||typeof data!=='object')return '<pre class="hint">'+esc(String(data||''))+'</pre>';
    if(data.error)return '<div class="sg-callout is-err">'+esc(String(data.error))+'</div>';
    var rows=[];
    function add(label,value){if(value)rows.push('<div class="sg-try-row"><span>'+esc(label)+'</span><strong>'+esc(value)+'</strong></div>');}
    add(t('sgTryClass'),sgClassLabel(data.class||data.routed_class||''));
    add(t('sgTrySource'),sgSourceLabel(data.class_source||''));
    add(t('sgTryModel'),data.resolved_model||'');
    add(t('sgTryQuality'),sgQualityLabel(data.quality||''));
    return '<div class="sg-try-result">'+rows.join('')+'<details class="sg-try-json"><summary>'+esc(t('sgTryRaw'))+'</summary><pre class="hint">'+esc(JSON.stringify(data,null,2))+'</pre></details></div>';
  }
  function sgFormatClassTrafficBoard(data){
    var rows=(data&&data.rows)||[];
    var sources=(data&&data.sources)||{};
    var samples=(data&&data.samples)||[];
    var table;
    if(!rows.length) table='<div class="hint">'+esc(t('sgClassEmpty'))+'</div>';
    else {
      table='<table class="sg-traffic-table"><thead><tr><th>'+esc(t('sgClassCol'))+'</th><th>'+esc(t('sgClassReq'))+'</th><th>'+esc(t('sgClassIn'))+'</th><th>'+esc(t('sgClassOut'))+'</th><th>'+esc(t('sgClassTok'))+'</th></tr></thead><tbody>'
        +rows.map(function(row){return '<tr'+(row.class==='total'?' class="is-total"':'')+'><td>'+esc(row.class==='total'?t('sgClassTotal'):sgClassLabel(row.class||''))+'</td><td>'+(row.requests||0)+'</td><td>'+(row.input_tokens||0)+'</td><td>'+(row.output_tokens||0)+'</td><td>'+(row.total_tokens||0)+'</td></tr>';}).join('')
        +'</tbody></table>';
    }
    var sourceKeys=Object.keys(sources);
    var sourceHtml=sourceKeys.length?'<div class="sg-mix"><div class="item-meta">'+esc(t('sgSourceMix'))+'</div><div class="sg-chip-row">'+sourceKeys.map(function(k){return '<span class="sg-chip">'+esc(sgSourceLabel(k))+' <em>'+sources[k]+'</em></span>';}).join('')+'</div></div>':'';
    var sampleHtml='<div class="sg-mix"><div class="item-meta">'+esc(t('sgNoHintSamples'))+'</div>';
    if(samples.length){
      sampleHtml+=samples.map(function(sample){
        var when=sample.at?String(sample.at).replace('T',' ').replace(/\.\d+Z$/,'Z'):'';
        return '<div class="sg-traffic-sample"><span class="badge">'+esc(sgClassLabel(sample.class||''))+'</span><div class="sg-traffic-sample-body">'
          +(when?'<div class="sg-traffic-sample-time">'+esc(when)+'</div>':'')
          +'<div class="sg-sample-preview">'+esc(sample.preview||'')+'</div></div></div>';
      }).join('');
    } else sampleHtml+='<div class="hint">'+esc(t('sgNoHintSamplesEmpty'))+'</div>';
    return table+sourceHtml+sampleHtml+'</div>';
  }
  function sgSnapTraffic(){
    var tryBox=document.querySelector('.sg-try-box');
    function valOf(id){var el=document.getElementById(id);return el?el.value:'';}
    var out=document.getElementById('sgTryRunOut');
    var board=document.getElementById('sgClassTraffic');
    return {tryOpen:!!(tryBox&&tryBox.open),tryText:valOf('sgTryRunText'),tryWorkflow:valOf('sgTryWorkflow'),tryPhase:valOf('sgTryPhase'),tryTask:valOf('sgTryTask'),tryOut:out?out.innerHTML:'',win:sgTrafficWindowName(_sgTrafficDataWin||serviceGroupTrafficPeriod||'day'),board:board?board.innerHTML:''};
  }
  function sgRestoreTraffic(snap){
    if(!snap)return;
    var tryBox=document.querySelector('.sg-try-box');
    if(tryBox)tryBox.open=!!snap.tryOpen;
    function set(id,v){var el=document.getElementById(id);if(el&&v!=null)el.value=v;}
    set('sgTryRunText',snap.tryText); set('sgTryWorkflow',snap.tryWorkflow); set('sgTryPhase',snap.tryPhase); set('sgTryTask',snap.tryTask);
    var out=document.getElementById('sgTryRunOut'); if(out&&snap.tryOut)out.innerHTML=snap.tryOut;
    var board=document.getElementById('sgClassTraffic'); if(board&&snap.board)board.innerHTML=snap.board;
    if(snap.win)_sgTrafficDataWin=sgTrafficWindowName(snap.win);
  }
  function sgRenderTrafficDialog(opts){
    var d=sgDraft||{};
    var id=String(d.id||'');
    sgOpenKind='traffic';
    var snap=opts&&opts.snap;
    if(snap&&snap.win) _sgTrafficDataWin=sgTrafficWindowName(snap.win);
    else _sgTrafficDataWin=sgTrafficWindowName(serviceGroupTrafficPeriod||'day');
    var body='<div class="sg-train-block"><div class="sg-section-title">'+esc(t('sgClassTraffic'))+'</div>'
      +'<div class="hint">'+esc(t('sgClassTrafficHint'))+'</div>'
      +'<div class="sg-inline-tools">'+sgWinButtons(id)+'</div>'
      +'<div id="sgClassTraffic" class="hint">'+esc(t('trafficLoading'))+'</div>'
      +'<details class="sg-advanced sg-try-box"'+(snap&&snap.tryOpen?' open':'')+'><summary>'+esc(t('sgTryRules'))+'</summary><div class="sg-advanced-body">'
      +'<textarea id="sgTryRunText" rows="3" class="sg-field-full" placeholder="'+esc(t('sgTryPlaceholder'))+'" onkeydown="if((event.ctrlKey||event.metaKey)&&event.key===\'Enter\'){event.preventDefault();sgTryClassify('+jsArg(id)+');}">'+(snap?esc(snap.tryText||''):'')+'</textarea>'
      +'<div class="sg-try-grid"><input id="sgTryWorkflow" placeholder="'+esc(t('sgTryWorkflow'))+'"><input id="sgTryPhase" placeholder="'+esc(t('sgTryPhase'))+'"><input id="sgTryTask" placeholder="'+esc(t('sgTryTask'))+'"></div>'
      +'<button class="btn-ghost" type="button" onclick="sgTryClassify('+jsArg(id)+')">'+esc(t('sgTryRun'))+'</button>'
      +'<div id="sgTryRunOut" class="hint"></div></div></details></div>';
    openDialog(sgDialogChrome((d.name||d.id||'')+' \u00b7 '+t('sgClassTraffic'), body, '<button class="btn-ghost" onclick="sgCloseCurrentDialog()">'+esc(t('sgClose'))+'</button>'), 'sg-form-dialog sg-traffic-dialog');
    if(snap) sgRestoreTraffic(snap);
    if(id) sgLoadClassTraffic(id, _sgTrafficDataWin);
  }
  window.editLLMClassTraffic=function(id){
    var g=serviceGroups.find(function(x){return x.id===id;});
    if(!g){toast(t('sgFailed'),'error');return;}
    sgDraft=sgCloneGroup(g);
    sgRenderTrafficDialog();
  };
  window.sgLoadClassTraffic=async function(id, windowName){
    var el=document.getElementById('sgClassTraffic');
    if(!el)return;
    windowName=sgTrafficWindowName(windowName||_sgTrafficDataWin||serviceGroupTrafficPeriod||'day');
    _sgTrafficDataWin=windowName;
    var seq=++sgTrafficSeq;
    var buttons=document.querySelectorAll('.sg-traffic-dialog button.sg-traffic-win[data-win]');
    for(var i=0;i<buttons.length;i++) buttons[i].classList.toggle('is-active', buttons[i].getAttribute('data-win')===windowName);
    var hasBoard=!!el.querySelector('table,.sg-mix,.sg-traffic-table');
    if(!hasBoard) el.textContent=t('trafficLoading');
    try{
      var data=await api('/api/admin/llm/class-traffic?service_group_id='+encodeURIComponent(id)+'&window='+encodeURIComponent(windowName));
      if(seq!==sgTrafficSeq||!sgDialogAlive('traffic',id))return;
      el.innerHTML=sgFormatClassTrafficBoard(data);
    }catch(e){
      if(seq!==sgTrafficSeq||!sgDialogAlive('traffic',id))return;
      if(hasBoard){toast(e.message||t('sgFailed'),'error');return;}
      el.textContent=e.message||t('sgFailed');
    }
  };
  window.sgTryClassify=async function(id){
    var text=(document.getElementById('sgTryRunText')&&document.getElementById('sgTryRunText').value)||'';
    var out=document.getElementById('sgTryRunOut');
    if(!out)return;
    var seq=++sgTrySeq;
    out.textContent=t('trafficLoading');
    try{
      var headers={};
      var workflow=(document.getElementById('sgTryWorkflow')&&document.getElementById('sgTryWorkflow').value||'').trim();
      var phase=(document.getElementById('sgTryPhase')&&document.getElementById('sgTryPhase').value||'').trim();
      var task=(document.getElementById('sgTryTask')&&document.getElementById('sgTryTask').value||'').trim();
      if(workflow)headers['X-MaClaw-Workflow-Type']=workflow;
      if(phase)headers['X-MaClaw-Phase-Kind']=phase;
      if(task)headers['X-MaClaw-Task-Type']=task;
      var data=await api('/api/admin/llm/classify-preview?service_group_id='+encodeURIComponent(id),{method:'POST',body:JSON.stringify({headers:headers,body:{model:'auto',messages:[{role:'user',content:text}]}})});
      if(seq!==sgTrySeq||!sgDialogAlive('traffic',id))return;
      out=document.getElementById('sgTryRunOut'); if(out)out.innerHTML=sgFmtTryResult(data);
    }catch(e){if(seq===sgTrySeq&&sgDialogAlive('traffic',id)&&(out=document.getElementById('sgTryRunOut')))out.textContent=e.message||t('sgFailed');}
  };
  function sgHeadSamplePage(){ return Math.max(1, Number(window._sgHeadSamplePage||1)); }
  function sgHeadSamplePages(data){
    var pages=Number(data&&data.sample_pages||0);
    if(pages>0)return pages;
    var total=Number(data&&data.sample_total||0);
    var limit=Number(data&&data.sample_limit||30)||30;
    if(total<=0)return 1;
    return Math.max(1, Math.ceil(total/limit));
  }
  function sgHeadIsOfficial(data){
    var id=String(data&&data.group_id||'').trim();
    return !id || sgIsOfficialGroup(id);
  }
  function sgClassHeadQS(){ return '?page='+encodeURIComponent(sgHeadSamplePage()); }
  function llmClassHeadViewVisible(){ var view=document.getElementById('llmSubViewClassHead'); return !!(view && !view.classList.contains('hidden-view')); }
  function sgHeadPageAlive(){ return llmClassHeadViewVisible(); }
  function sgHeadActing(){ return sgHeadBusy||sgTrainBusy; }
  function sgParseGoldClass(raw){
    raw=String(raw||'').trim(); if(!raw)return '';
    if(raw==='__clear__')return '__clear__';
    var lower=raw.toLowerCase().replace(/\s+/g,'_');
    var aliases={docs:'doc_write',doc:'doc_write',document:'doc_write'};
    if(aliases[lower])return aliases[lower];
    var keys=sgFrozenClasses();
    for(var i=0;i<keys.length;i++){ if(lower===keys[i]||raw===sgClassLabel(keys[i]))return keys[i]; }
    return '';
  }
  function sgGoldSelect(sample){
    var cur=String(sample&&sample.gold_class||'');
    return '<select class="sg-gold-sel" aria-label="'+esc(t('sgGoldPick'))+'" onchange="sgReviewClassHead('+jsArg(sample&&sample.id||'')+',this.value)">'
      +'<option value="">'+esc(t('sgGoldPick'))+'</option>'
      +'<option value="__clear__">'+esc(t('sgGoldClear'))+'</option>'
      +sgFrozenClasses().map(function(k){return '<option value="'+k+'"'+(k===cur?' selected':'')+'>'+esc(sgClassLabel(k))+'</option>';}).join('')
      +'</select>';
  }
  function sgSampleActions(sample){
    return '<div class="sg-sample-action">'+sgGoldSelect(sample)
      +'<button class="btn-danger-ghost sg-tiny-btn" type="button" onclick="sgDeleteClassHeadSample('+jsArg(sample&&sample.id||'')+')">'+esc(t('sgSampleDelete'))+'</button></div>';
  }
  function sgFmtSamplePager(data){
    var page=Number(data&&data.sample_page||sgHeadSamplePage()||1);
    var pages=sgHeadSamplePages(data);
    var total=Number(data&&data.sample_total||0);
    return '<div class="sg-inline-tools sg-sample-pager"><button class="btn-ghost" type="button"'+(page<=1?' disabled':'')+' onclick="sgHeadSamplePageTo(-1)">\u2190</button><span>'+page+' / '+pages+(total?' \u00b7 '+total:'')+'</span><button class="btn-ghost" type="button"'+(page>=pages?' disabled':'')+' onclick="sgHeadSamplePageTo(1)">\u2192</button></div>';
  }
  window.sgHeadSamplePageTo=function(delta){
    if(sgHeadActing())return;
    var pages=sgHeadSamplePages(window._sgHeadData||{});
    var next=Math.max(1, Math.min(pages, sgHeadSamplePage()+Number(delta||0)));
    if(next===sgHeadSamplePage())return;
    window._sgHeadSamplePage=next;
    if(typeof window.sgLoadClassHead==='function')window.sgLoadClassHead();
  };
  function sgHeadIsUnused(data){
    return String(data&&data.status||'unused')==='unused' && String(data&&data.pipeline||'off')==='off' && !(data&&data.reviews) && !(data&&data.human_reviews);
  }
  function sgHeadDistributing(data){
    if(!data)return false;
    if(String(data.distribute_status||'')==='distributing'||String(data.status||'')==='distributing')return true;
    var ack=data.distribute_ack||{};
    return Object.keys(ack).some(function(id){return String(ack[id]||'')!=='acked';});
  }
  function sgHeadHasSamples(data){ return Number(data&&data.sample_total||0)>0 || !!((data&&data.samples||[]).length); }
  function sgHeadNeedServing(data){ return String(data&&data.pipeline||'off')==='off' && !data.artifact_ready; }
  function sgHeadNeedShadow(data){ return String(data&&data.pipeline||'off')==='off'; }
  function sgHeadNeedDistribute(data){ return sgHeadDistributing(data); }
  function sgHeadAdoptReady(data){ return !!(data&&data.artifact_ready&&String(data.status||'')!=='training' && String(data.pipeline||'off')==='off'); }
  function sgHeadStatusLabel(status, data){
    if(status==='unused' && data && data.artifact_ready) return t('sgHeadSt_unused_trained');
    var key='sgHeadSt_'+String(status||'unused');
    var v=t(key); return v===key?String(status||'unused'):v;
  }
  function sgHeadLiveLabel(data){
    var pipe=String(data&&data.pipeline||'off');
    return t('sgHeadPipeline')+': '+t('sgPipe_'+pipe);
  }
  function sgHeadJobLabel(status){
    if(status==='training')return t('sgHeadSt_training');
    if(status==='distributing')return t('sgHeadSt_distributing');
    return '';
  }
  function sgHeadStatusHint(data){
    var status=String(data&&data.status||'unused');
    if(status==='unused') return t('sgHeadStHint_unused');
    return '';
  }
  function sgHeadStatusTone(status){
    if(status==='promoted')return 'ok';
    if(status==='canary'||status==='shadow'||status==='training'||status==='distributing')return 'info';
    if(status==='gates_failed'||status==='rolled_back')return 'warn';
    return '';
  }
  function sgHeadCallout(data){
    if(sgHeadIsUnused(data)){
      if(sgHeadHasSamples(data)) return {text:t('sgHeadHasSamples'),tone:'info'};
      return {text:t(sgHeadIsOfficial(data)?'sgHeadUnusedOfficial':'sgHeadUnused'),tone:'info'};
    }
    if(sgHeadAdoptReady(data)) return {text:t('sgHeadAdoptReady'),tone:'info'};
    if(sgHeadDistributing(data)) return {text:t('sgHeadDistributing'),tone:'info'};
    if(data&&data.suggestion) return {text:String(data.suggestion),tone:(data.gates&&data.gates.can_promote)?'ok':'warn'};
    return null;
  }
  function sgTrainerNodes(data){
    var nodes=(data&&data.cluster_nodes||[]).slice();
    if(data&&data.trainer_node_id&&nodes.indexOf(data.trainer_node_id)<0) nodes=nodes.concat([data.trainer_node_id]);
    return nodes;
  }
  function sgHeadNeedsPoll(data){
    var status=String(data&&data.status||'');
    var training=status==='training';
    return training || sgHeadDistributing(data) || (data&&data.warming);
  }
  function sgHeadSamplesKey(data){
    return ((data&&data.samples)||[]).map(function(s){ return [s&&s.id,s&&s.gold_class,s&&s.head_class,s&&s.rule_class,s&&s.at].join(':'); }).join(',');
  }
  function sgHeadPollKey(data){ return [data&&data.status,data&&data.pipeline,data&&data.version,data&&data.distribute_status,data&&data.sample_page,data&&data.sample_total,data&&data.reviews,data&&data.human_reviews,data&&data.accuracy,data&&data.artifact_ready,data&&data.last_train_error,data&&data.embedder_ready,data&&data.trainer_node_id,sgHeadSamplesKey(data)].join('|'); }
  function sgHeadPollBlocked(){ return sgHeadActing() || !sgHeadPageAlive(); }
  function scheduleClassHeadPoll(){
    if(sgHeadPollTimer) clearTimeout(sgHeadPollTimer);
    sgHeadPollTimer=setTimeout(function(){
      if(!sgHeadPageAlive())return;
      if(sgHeadActing()){ scheduleClassHeadPoll(); return; }
      window.sgLoadClassHead({quiet:true});
    }, 2500);
  }
  function sgFmtHeadVersions(data){
    var versions=(data&&data.versions)||[];
    if(!versions.length)return '';
    var rows=versions.map(function(item){
      return '<tr><td><span class="badge'+(item.role==='current'?' ok':(item.role==='previous'?' info':''))+'">'+esc(item.role||'')+'</span></td>'
        +'<td class="mono">v'+esc(String(item.version||0))+'</td><td>'+esc(item.trained_at||'\u2014')+'</td>'
        +'<td>'+esc(item.source||'\u2014')+'</td><td>'+esc(item.tau!=null?Number(item.tau).toFixed(2):'\u2014')+'</td><td>'+esc(item.retired_at||'\u2014')+'</td></tr>';
    }).join('');
    return '<div class="sg-head-versions"><div class="item-meta">'+esc(t('sgHeadVersions'))+'</div>'
      +'<table class="sg-version-table"><thead><tr><th>'+esc(t('sgHeadRole'))+'</th><th>'+esc(t('sgHeadVersion'))+'</th><th>'+esc(t('sgHeadTrainedAt'))+'</th><th>'+esc(t('sgHeadSource'))+'</th><th>'+esc(t('sgHeadTau'))+'</th><th>'+esc(t('sgHeadRetired'))+'</th></tr></thead><tbody>'+rows+'</tbody></table></div>';
  }
  function sgSyncHeadScoreGroupSelect(){
    var sel=document.getElementById('sgHeadScoreGroup');
    if(!sel)return;
    var cur=sel.value||'';
    var opts='<option value="">'+esc(t('sgHeadScoreGroupAuto'))+'</option>'
      +(serviceGroups||[]).map(function(g){return '<option value="'+esc(g.id)+'"'+(cur===g.id?' selected':'')+'>'+esc(g.name||g.id)+'</option>';}).join('');
    sel.innerHTML=opts;
  }
  function sgFmtHeadTest(data){
    var versions=(data&&data.versions)||[];
    var live=versions.filter(function(item){return item.role==='current'||item.role==='previous';});
    var ready=!!(data&&data.embedder_ready);
    var opts=live.map(function(item){return '<option value="'+esc(item.role)+'">'+esc((item.role||'')+' v'+item.version)+'</option>';}).join('');
    return '<div class="sg-head-test"><div class="item-meta">'+esc(t('sgHeadTest'))+'</div><div class="hint">'+esc(t('sgHeadTestHint'))+'</div>'
      +'<textarea id="sgHeadTestText" rows="3" class="sg-field-full" placeholder="'+esc(t('sgTryPlaceholder'))+'" onkeydown="if((event.ctrlKey||event.metaKey)&&event.key===\'Enter\'){event.preventDefault();sgScoreClassHead();}"></textarea>'
      +'<div class="sg-try-grid"><input id="sgHeadTestWorkflow" placeholder="'+esc(t('sgTryWorkflow'))+'"><input id="sgHeadTestPhase" placeholder="'+esc(t('sgTryPhase'))+'"><input id="sgHeadTestTask" placeholder="'+esc(t('sgTryTask'))+'"></div>'
      +'<div class="sg-head-toolbar"><label for="sgHeadTestSlot">'+esc(t('sgHeadTestSlot'))+'</label><select id="sgHeadTestSlot">'+opts+'</select>'
      +'<label for="sgHeadScoreGroup">'+esc(t('sgHeadScoreGroup'))+'</label><select id="sgHeadScoreGroup"></select>'
      +'<button id="sgHeadTestBtn" class="btn-secondary" type="button" onclick="sgScoreClassHead()"'+(ready?'':' disabled')+'>'+esc(t('sgHeadTestRun'))+'</button>'
      +(live.length>1?'<button id="sgHeadTestCompareBtn" class="btn-ghost" type="button" onclick="sgScoreClassHead(true)"'+(ready?'':' disabled')+'>'+esc(t('sgHeadTestCompare'))+'</button>':'')
      +'</div>'+((data&&data.embedder_ready)?'':'<div class="hint">'+esc(t('sgHeadEmbedderOff'))+'</div>')
      +'<div id="sgHeadTestOut" class="hint"></div></div>';
  }
  function sgFmtHeadScore(data){
    if(!data||typeof data!=='object')return '<pre class="hint">'+esc(String(data||''))+'</pre>';
    if(data.error)return '<div class="sg-callout is-err">'+esc(String(data.error))+'</div>';
    var rows=[];
    function add(label,value){if(value)rows.push('<div class="sg-try-row"><span>'+esc(label)+'</span><strong>'+esc(value)+'</strong></div>');}
    add(t('sgHeadTestSlot'), data.slot||'');
    add(t('sgHeadVersion'), data.version?('v'+data.version):'');
    add(t('sgSampleRule'), sgClassLabel(data.rule_class||''));
    add(t('sgSampleHead'), sgClassLabel(data.head_class||''));
    add(t('sgHeadIfLive'), sgClassLabel(data.if_live_class||''));
    add(t('sgHeadTestGroup'), data.group_id||'');
    return '<div class="sg-try-result">'+rows.join('')+'</div>';
  }
  function sgFmtHead(data){
    if(!data)return '<div class="hint">'+esc(t('sgHeadNoData'))+'</div>';
    var status=String(data.status||'unused');
    var training=status==='training';
    var gates=data.gates||{};
    var pipe=String(data&&data.pipeline||'off');
    var canPromote=!!gates.can_promote;
    var serving=!!(data.artifact_ready&&String(data.status||'')!=='training');
    var distributing=sgHeadDistributing(data);
    var pipeline=['off','shadow','canary','on'].map(function(mode){
      var upgrading=(mode==='canary'&&pipe!=='canary'&&pipe!=='on')||(mode==='on'&&pipe!=='on');
      var locked=(mode==='shadow'&&pipe==='off'&&!serving)||(upgrading&&(!canPromote||pipe==='off'||distributing));
      var cls=(pipe===mode?' is-active':'')+(mode==='on'&&pipe==='on'?' is-live':'')+(locked?' is-locked':'');
      return '<button class="sg-pipe-btn'+cls+'" type="button" onclick="sgSetClassHeadPipeline(\''+mode+'\')">'+esc(t('sgPipe_'+mode))+'</button>';
    }).join('');
    var sampleTotal=Number(data.sample_total||0);
    var sampleRows=(data.samples||[]).map(function(sample){
      return '<div class="sg-sample"><div class="sg-sample-main"><div class="sg-sample-tags">'
        +'<span class="badge">'+esc(t('sgSampleRule')+' \u00b7 '+sgClassLabel(sample.rule_class||'-'))+'</span>'
        +'<span class="badge'+(sample.gold_class?' ok':'')+'">'+esc(t('sgSampleGold')+' \u00b7 '+(sample.gold_class?sgClassLabel(sample.gold_class):'\u2014'))+'</span>'
        +(sample.head_class?'<span class="badge info">'+esc(t('sgSampleHead')+' \u00b7 '+sgClassLabel(sample.head_class))+'</span>':'')
        +'</div><div class="sg-sample-preview">'+esc(sample.preview||'')+'</div></div>'
        +(sample.id?sgSampleActions(sample):'')+'</div>';
    }).join('');
    var sampleBody=sampleRows||(sampleTotal?'<div class="hint">'+esc(t('sgHeadSamplesEmpty'))+'</div>':'');
    var showEval=!!(data.version||data.reviews||data.human_reviews||data.artifact_ready);
    var canTrain=!!(data.version||data.artifact_ready||data.reviews||data.human_reviews||sampleTotal);
    var nodes=sgTrainerNodes(data);
    var opts='<option value="">'+esc(t('sgTrainerEmpty'))+'</option>'+nodes.map(function(id){return '<option value="'+esc(id)+'"'+(id===data.trainer_node_id?' selected':'')+'>'+esc(id)+(id===data.local_node_id?' ('+t('sgTrainerLocalTag')+')':'')+'</option>';}).join('');
    var callout=sgHeadCallout(data);
    var statusHint=sgHeadStatusHint(data);
    var job=sgHeadJobLabel(status);
    var ack=data.distribute_ack||{};
    var ackRows=Object.keys(ack).map(function(id){return '<div class="sg-ack-row"><span class="mono">'+esc(id)+'</span><span class="badge '+(ack[id]==='acked'?'ok':'warn')+'">'+esc(ack[id]==='acked'?t('sgAck_acked'):t('sgAck_pending'))+'</span></div>';}).join('');
    var gateItems=[['review_coverage',gates.review_coverage],['accuracy',gates.accuracy],['recall',gates.recall],['two_windows',gates.two_windows],['artifact',gates.artifact]];
    var gateHtml=gateItems.map(function(item){ return '<span class="badge '+(item[1]?'ok':'warn')+'">'+esc(t('sgGate_'+item[0]))+'</span>'; }).join('');
    return '<div class="sg-head-dash">'
      +'<div class="sg-head-status"><span class="badge '+sgHeadStatusTone(status)+'"'+(statusHint?' title="'+esc(statusHint)+'"':'')+'>'+esc(sgHeadStatusLabel(status,data))+'</span>'
      +(job?'<span class="hint">'+esc(job)+'</span>':'')
      +(data.version?'<span class="sg-head-ver">'+esc(t('sgHeadVersion'))+' v'+esc(String(data.version))+'</span>':'')
      +'<span class="hint">'+esc(sgHeadLiveLabel(data))+'</span></div>'
      +sgFmtHeadVersions(data)
      +'<div class="sg-pipe-steps">'+pipeline+'</div>'
      +'<div class="sg-head-toolbar">'
      +'<button class="btn-primary" type="button"'+(canTrain&&!training?'':' disabled title="'+esc(t(sgHeadIsOfficial(data)?'sgTrainNeedDataOfficial':'sgTrainNeedData'))+'"')+' onclick="sgTrainClassHead()">'+esc(training?t('sgTraining'):t('sgTrainThis'))+'</button>'
      +'<button class="btn-ghost" type="button" onclick="sgLoadClassHead()">'+esc(t('sgRefreshHead'))+'</button>'
      +'<button class="btn-ghost" type="button"'+(serving?'':' disabled')+' onclick="sgDistributeClassHead()">'+esc(t('sgDistribute'))+'</button>'
      +'<button class="btn-ghost" type="button" onclick="sgRollbackClassHead()">'+esc(t('sgRollBack'))+'</button>'
      +(sgHeadIsOfficial(data)?'':'<button class="btn-ghost" type="button" onclick="sgPullOfficialClassHead()">'+esc(t('sgPullOfficial'))+'</button>')
      +'</div>'
      +'<div class="sg-trainer-row"><label for="sgHeadTrainer">'+esc(t('sgHeadTrainer'))+'</label><select id="sgHeadTrainer">'+opts+'</select><button class="btn-ghost" type="button" onclick="sgSetClassHeadTrainer()">'+esc(t('sgApplyTrainer'))+'</button></div>'
      +'<p class="sg-trainer-hint">'+esc(t('sgTrainerHint'))+'</p>'
      +(callout?'<div class="sg-callout is-'+callout.tone+'">'+esc(callout.text)+'</div>':'')
      +(data.last_train_error?'<div class="sg-callout is-err">'+esc(data.last_train_error)+'</div>':'')
      +(showEval?'<div class="sg-head-metrics">'
        +'<div class="sg-metric"><div class="sg-metric-label">'+esc(t('sgHeadAccuracy'))+'</div><div class="sg-metric-value">'+(Number(data.accuracy||0)*100).toFixed(1)+'%</div></div>'
        +'<div class="sg-metric"><div class="sg-metric-label">'+esc(t('sgHeadPlanRecall'))+'</div><div class="sg-metric-value">'+(Number(data.plan_recall||0)*100).toFixed(1)+'%</div></div>'
        +'<div class="sg-metric"><div class="sg-metric-label">'+esc(t('sgHeadRuleAgreement'))+'</div><div class="sg-metric-value">'+(Number(data.rule_agreement||0)*100).toFixed(1)+'%</div></div>'
        +'<div class="sg-metric"><div class="sg-metric-label">'+esc(t('sgHeadReviews'))+'</div><div class="sg-metric-value">'+(data.reviews||0)+'<span class="sg-metric-sub"> / '+esc(t('sgHeadHuman'))+' '+(data.human_reviews||0)+'</span></div></div>'
        +'</div><div class="sg-gate-head">'+esc(t('sgHeadGates'))+' '+(gates.passed||0)+'/'+(gates.total||5)+'</div><div class="sg-gate-list">'+gateHtml+'</div>':'')
      +(ackRows?'<div class="sg-ack-block"><div class="item-meta">'+esc(t('sgHeadAck'))+'</div>'+ackRows+'</div>':'')
      +(sampleTotal||sampleRows?'<div class="sg-sample-block"><div class="item-title">'+esc(t('sgHeadSamples'))+'</div>'+sampleBody+sgFmtSamplePager(data)+'</div>':'')
      +sgFmtHeadTest(data)
      +'</div>';
  }
  function sgShowPromoteForm(mode){
    var steps=document.querySelector('#sgClassHead .sg-pipe-steps');
    if(!steps)return;
    var old=document.getElementById('sgPromoteForm'); if(old)old.remove();
    var box=document.createElement('div');
    box.id='sgPromoteForm';
    box.className='sg-promote-form';
    box.setAttribute('data-mode', mode);
    box.innerHTML='<form onsubmit="sgConfirmPromote('+jsArg(mode)+');return false;">'
      +'<div class="sg-promote-grid"><label class="sg-field"><span>'+esc(t('sgPromptReason'))+'</span><input id="sgPromoteReason" required></label>'
      +'<label class="sg-field"><span>'+esc(t('sgPromptOverride'))+'</span><input id="sgPromoteToken" required placeholder="PROMOTE"></label></div>'
      +'<div class="sg-head-toolbar"><button class="btn-primary" type="submit">'+esc(t('sgPromoteGo'))+'</button>'
      +'<button class="btn-ghost" type="button" onclick="sgCancelPromote()">'+esc(t('cancel'))+'</button></div></form>';
    steps.insertAdjacentElement('afterend',box);
  }
  window.sgCancelPromote=function(){ var el=document.getElementById('sgPromoteForm'); if(el)el.remove(); };
  window.sgConfirmPromote=async function(mode){
    var reason=String((document.getElementById('sgPromoteReason')||{}).value||'').trim();
    var token=String((document.getElementById('sgPromoteToken')||{}).value||'').trim();
    if(!reason){toast(t('sgPromptReason'),'error');return;}
    if(token.toUpperCase()!=='PROMOTE'){toast(t('sgPromoteNeed'),'error');return;}
    window.sgCancelPromote();
    await sgPostPipeline(mode,'PROMOTE',reason);
  };
  async function sgPostPipeline(mode,override,reason){
    if(sgHeadActing())return;
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/pipeline'+sgClassHeadQS(),{method:'POST',body:JSON.stringify({mode:mode,override:override||'',reason:reason||''})}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  }
  window.sgSetClassHeadPipeline=async function(mode){
    if(sgHeadActing())return;
    var data=window._sgHeadData||{};
    var current=String(data.pipeline||'off');
    if(current===mode){ window.sgCancelPromote(); return; }
    var canPromote=!!(data.gates&&data.gates.can_promote);
    if(mode==='shadow'&&current==='off'&&!data.artifact_ready){ toast(t('sgHeadNeedServing'),'error'); return; }
    if(mode==='canary'||mode==='on'){
      if(current==='off'){ toast(t('sgHeadNeedShadow'),'error'); return; }
      if(sgHeadDistributing(data)){ toast(t('sgHeadNeedDistribute'),'error'); return; }
      if(!canPromote){ sgShowPromoteForm(mode); return; }
      if(mode==='on'&&!sgConfirm(t('sgConfirmLive')))return;
    }
    window.sgCancelPromote();
    await sgPostPipeline(mode,'','');
  };
  window.sgTrainClassHead=async function(){
    if(sgHeadActing())return;
    sgTrainBusy=true;
    try{ await api('/api/admin/llm/class-head/train'+sgClassHeadQS(),{method:'POST',body:'{}'}); }
    catch(e){ sgTrainBusy=false; toast(e.message,'error'); window.sgLoadClassHead(); return; }
    setTimeout(function(){
      sgTrainBusy=false;
      if(sgHeadPageAlive()) window.sgLoadClassHead({quiet:true});
    },800);
  };
  window.sgRollbackClassHead=async function(){
    if(sgHeadActing())return;
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/rollback'+sgClassHeadQS(),{method:'POST',body:'{}'}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  };
  window.sgDistributeClassHead=async function(){
    if(sgHeadActing())return;
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/distribute'+sgClassHeadQS(),{method:'POST',body:'{}'}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  };
  window.sgPullOfficialClassHead=async function(){
    if(sgHeadActing())return;
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/pull-official'+sgClassHeadQS(),{method:'POST',body:'{}'}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  };
  window.sgSetClassHeadTrainer=async function(){
    if(sgHeadActing())return;
    var sel=document.getElementById('sgHeadTrainer');
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/trainer'+sgClassHeadQS(),{method:'POST',body:JSON.stringify({node_id:sel?sel.value:''})}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  };
  window.sgReviewClassHead=async function(sampleId,goldPrefill){
    if(sgHeadActing()){window.sgLoadClassHead();return;}
    if(!String(goldPrefill||'').trim()){window.sgLoadClassHead();return;}
    if(goldPrefill==='__clear__') goldPrefill='';
    var gold=goldPrefill===''?'':sgParseGoldClass(goldPrefill);
    if(goldPrefill&&!gold){toast(t('sgGoldInvalid'),'error');window.sgLoadClassHead();return;}
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/review'+sgClassHeadQS(),{method:'POST',body:JSON.stringify({sample_id:sampleId,gold_class:gold})}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  };
  window.sgDeleteClassHeadSample=async function(sampleId){
    if(sgHeadActing())return;
    if(!sgConfirm(t('sgSampleDeleteConfirm')))return;
    sgHeadBusy=true;
    try{ await api('/api/admin/llm/class-head/sample/delete'+sgClassHeadQS(),{method:'POST',body:JSON.stringify({sample_id:sampleId})}); }
    catch(e){ toast(e.message,'error'); }
    finally{ sgHeadBusy=false; }
    window.sgLoadClassHead();
  };
  window.sgScoreClassHead=async function(compare){
    var out=document.getElementById('sgHeadTestOut');
    if(!out)return;
    var text=(document.getElementById('sgHeadTestText')&&document.getElementById('sgHeadTestText').value)||'';
    if(!String(text).trim()){out.textContent=t('sgHeadNeedText');return;}
    var slot=(document.getElementById('sgHeadTestSlot')&&document.getElementById('sgHeadTestSlot').value)||'current';
    var groupId=(document.getElementById('sgHeadScoreGroup')&&document.getElementById('sgHeadScoreGroup').value)||'';
    var headers={};
    var workflow=(document.getElementById('sgHeadTestWorkflow')&&document.getElementById('sgHeadTestWorkflow').value||'').trim();
    var phase=(document.getElementById('sgHeadTestPhase')&&document.getElementById('sgHeadTestPhase').value||'').trim();
    var task=(document.getElementById('sgHeadTestTask')&&document.getElementById('sgHeadTestTask').value||'').trim();
    if(workflow)headers['X-MaClaw-Workflow-Type']=workflow;
    if(phase)headers['X-MaClaw-Phase-Kind']=phase;
    if(task)headers['X-MaClaw-Task-Type']=task;
    function post(nextSlot){
      return api('/api/admin/llm/class-head/score'+sgClassHeadQS(),{method:'POST',body:JSON.stringify({slot:nextSlot,text:text,headers:headers,group_id:groupId})});
    }
    out.textContent=t('trafficLoading');
    try{
      var html;
      if(compare){ var pair=await Promise.all([post('current'),post('previous')]); html=sgFmtHeadScore(pair[0])+sgFmtHeadScore(pair[1]); }
      else html=sgFmtHeadScore(await post(slot));
      out=document.getElementById('sgHeadTestOut'); if(out)out.innerHTML=html;
    }catch(e){ if((out=document.getElementById('sgHeadTestOut'))) out.textContent=e.message||t('sgFailed'); }
  };
  function sgSnapHead(){
    function valOf(id){var el=document.getElementById(id);return el?el.value:'';}
    var out=document.getElementById('sgHeadTestOut');
    var root=document.getElementById('sgClassHead');
    var promote=document.getElementById('sgPromoteForm');
    return {
      text:valOf('sgHeadTestText'), workflow:valOf('sgHeadTestWorkflow'), phase:valOf('sgHeadTestPhase'),
      task:valOf('sgHeadTestTask'), slot:valOf('sgHeadTestSlot'), group:valOf('sgHeadScoreGroup'),
      out:out?out.innerHTML:'', scroll:root?root.scrollTop:0,
      promoteMode:promote?String(promote.getAttribute('data-mode')||''):'',
      promoteReason:valOf('sgPromoteReason'), promoteToken:valOf('sgPromoteToken'),
      focus:sgSnapFocus(root)
    };
  }
  function sgRestoreHead(snap){
    if(!snap)return;
    function set(id,v){var el=document.getElementById(id);if(el&&v!=null)el.value=v;}
    set('sgHeadTestText',snap.text); set('sgHeadTestWorkflow',snap.workflow); set('sgHeadTestPhase',snap.phase);
    set('sgHeadTestTask',snap.task); set('sgHeadTestSlot',snap.slot); set('sgHeadScoreGroup',snap.group);
    var out=document.getElementById('sgHeadTestOut'); if(out&&snap.out)out.innerHTML=snap.out;
    if(snap.promoteMode){
      sgShowPromoteForm(snap.promoteMode);
      set('sgPromoteReason',snap.promoteReason);
      set('sgPromoteToken',snap.promoteToken);
    }
    var root=document.getElementById('sgClassHead'); if(root&&snap.scroll)root.scrollTop=snap.scroll;
    sgRestoreFocus(snap.focus);
  }
  window.sgLoadClassHead=async function(opts){
    var el=document.getElementById('sgClassHead');
    if(!el)return;
    var quiet=!!(opts&&opts.quiet);
    var relabel=!!(opts&&opts.relabel);
    var hasDash=!!el.querySelector('.sg-head-dash');
    if(relabel && window._sgHeadData && hasDash){
      var keep=sgSnapHead();
      el.innerHTML=sgFmtHead(window._sgHeadData);
      sgSyncHeadScoreGroupSelect();
      if(keep) sgRestoreHead(keep);
      return;
    }
    var seq=++sgHeadSeq;
    if(!quiet && !hasDash) el.textContent=t('trafficLoading');
    try{
      var data=await api('/api/admin/llm/class-head'+sgClassHeadQS());
      if(seq!==sgHeadSeq||!sgHeadPageAlive())return;
      window._sgHeadData=data;
      if(data&&data.sample_page) window._sgHeadSamplePage=Math.max(1,Number(data.sample_page)||1);
      var nextKey=sgHeadPollKey(data);
      if(quiet && !relabel && hasDash && nextKey && nextKey===window._sgHeadPollKey){
        if(sgHeadNeedsPoll(data)) scheduleClassHeadPoll();
        return;
      }
      window._sgHeadPollKey=nextKey;
      var snap=el.querySelector('.sg-head-dash')?sgSnapHead():null;
      el.innerHTML=sgFmtHead(data);
      sgSyncHeadScoreGroupSelect();
      if(data&&data.group_id){
        var sel=document.getElementById('sgHeadScoreGroup');
        if(sel&&!sel.value) sel.value=data.group_id;
      }
      if(snap) sgRestoreHead(snap);
      if(sgHeadNeedsPoll(data)) scheduleClassHeadPoll();
    }catch(e){
      if(seq!==sgHeadSeq)return;
      if(hasDash){ toast(e.message||t('sgFailed'),'error'); return; }
      el.textContent=e.message||t('sgFailed');
    }
  };
  window.sgReloadClassHeadPage=function(){
    if(typeof loadLLMEmbeddingModelRuntime==='function')loadLLMEmbeddingModelRuntime({ silent: true });
    if(typeof window.sgLoadClassHead==='function')window.sgLoadClassHead();
  };

  function embeddingRuntimeNeedsPoll(st){ return !!(st && (st.downloading || st.warming)); }
  function embeddingRuntimeBadge(status) {
    var label = t('runtimeMissing'); var cls = 'danger';
    if (status === 'ready') { label = t('runtimeReady'); cls = 'ok'; }
    else if (status === 'downloading') { label = t('runtimeDownloading'); cls = 'info'; }
    else if (status === 'warming') { label = t('runtimeWarming'); cls = 'info'; }
    else if (status === 'partial') { label = t('runtimePartial'); cls = 'warn'; }
    return '<span class="badge '+cls+'">'+esc(label)+'</span>';
  }
  function renderLLMEmbeddingModelRuntime() {
    var root = document.getElementById('llmEmbeddingModelCard');
    if (!root) return;
    var data = llmEmbeddingModelRuntimeCache || {};
    var ready = !!(data.ready && data.embedder_ready);
    var status = data.status || (ready ? 'ready' : (data.downloading ? 'downloading' : (data.warming ? 'warming' : (data.ready ? 'partial' : 'missing'))));
    var path = data.serving_path || data.model_dir || '';
    root.innerHTML = '<div class="item-head"><div class="llm-embed-title"><div class="item-title" data-icon="cpu">'+esc(t('runtimeTitle'))+'</div>'+embeddingRuntimeBadge(status)
      +'<div class="item-meta">'+esc(t('runtimeDesc'))+'</div></div>'
      +'<div class="actions"><button class="btn-ghost" type="button" onclick="loadLLMEmbeddingModelRuntime()">'+esc(t('runtimeRefresh'))+'</button>'
      +'<button class="btn-secondary" type="button" onclick="triggerLLMEmbeddingModelDownload()">'+esc(t('runtimeTrigger'))+'</button></div></div>'
      +'<div class="llm-embed-meta"><span class="llm-embed-k">'+esc(t('runtimeDir'))+'</span><span class="llm-embed-path mono">'+esc(path||'-')+'</span></div>'
      +(data.last_download_error?'<p class="llm-embed-error">'+esc(data.last_download_error)+'</p>':'');
  }
  async function loadLLMEmbeddingModelRuntime(opts) {
    var seq = ++llmEmbeddingLoadSeq;
    var silent = !!(opts && opts.silent);
    try {
      var data = await api('/api/admin/model_download/status');
      if (seq !== llmEmbeddingLoadSeq) return;
      llmEmbeddingModelRuntimeCache = data;
      if (data.ready) { /* keep badge ready even before embedder warms */ }
      renderLLMEmbeddingModelRuntime();
      var waitEmbedder = llmClassHeadViewVisible() && !!(data.ready) && !data.embedder_ready;
      if (embeddingRuntimeNeedsPoll(data) || waitEmbedder) {
        if (llmEmbeddingRuntimePollTimer) clearTimeout(llmEmbeddingRuntimePollTimer);
        llmEmbeddingRuntimePollTimer = setTimeout(function(){ loadLLMEmbeddingModelRuntime({ silent: true }); }, 2500);
      }
    } catch (e) {
      if (seq !== llmEmbeddingLoadSeq) return;
      if (!silent) toast(e.message || t('sgFailed'), 'error');
    }
  }
  window.loadLLMEmbeddingModelRuntime = loadLLMEmbeddingModelRuntime;
  async function triggerLLMEmbeddingModelDownload() {
    var st = llmEmbeddingModelRuntimeCache || {};
    if (st.downloading || st.warming) { toast(t('runtimeAlreadyRunning'), 'info'); return; }
    try {
      await api('/api/admin/model_download/trigger', { method: 'POST', body: '{}' });
      await loadLLMEmbeddingModelRuntime();
    } catch (e) { toast(e.message || t('sgFailed'), 'error'); }
  }
  window.triggerLLMEmbeddingModelDownload = triggerLLMEmbeddingModelDownload;
  function maybeAutoSyncEmbeddingModel() {
    var st = llmEmbeddingModelRuntimeCache;
    if (!st) { loadLLMEmbeddingModelRuntime({ silent: true }); return; }
    if (st.ready && st.embedder_ready) return;
    if (st.downloading || st.warming) return;
    triggerLLMEmbeddingModelDownload();
  }

  function sgSnapFocus(root) {
    var el = document.activeElement;
    if (!el || !el.id || (root && !root.contains(el))) return null;
    var snap = { id: el.id };
    if (typeof el.selectionStart === 'number') { snap.start = el.selectionStart; snap.end = el.selectionEnd; }
    return snap;
  }
  function sgRestoreFocus(snap) {
    if (!snap || !snap.id) return;
    var node = document.getElementById(snap.id);
    if (!node || typeof node.focus !== 'function') return;
    node.focus();
    if (snap.start != null && typeof node.setSelectionRange === 'function') {
      try { node.setSelectionRange(snap.start, snap.end == null ? snap.start : snap.end); } catch (e) {}
    }
  }
  function sgDialogChrome(title, body, actions) {
    return '<div class="sg-dialog-head"><h3>' + esc(title) + '</h3></div><div class="sg-dialog-body">' + body + '</div>'
      + (actions ? '<div class="actions sg-form-actions">' + actions + '</div>' : '');
  }
  function sgRelabelProviderDialog() {
    var focus = sgSnapFocus(document.getElementById('llmDialogContent'));
    var snap = {
      id: val('llmPrvID'), name: val('llmPrvName'), url: val('llmPrvURL'), key: val('llmPrvKey'),
      protocol: val('llmPrvProtocol'), models: val('llmPrvModels'), caps: val('llmPrvCaps'),
      priority: val('llmPrvPriority'), sequence: val('llmPrvSequence'), conc: val('llmPrvConc'), timeout: val('llmPrvTimeout'),
      timezone: val('llmPrvTimezone'), multiplier: val('llmPrvMultiplier'),
      tpIn: val('llmPrvTpIn'), tpOut: val('llmPrvTpOut'), tpRmbIn: val('llmPrvTpRmbIn'), tpRmbOut: val('llmPrvTpRmbOut'),
      tpMin: val('llmPrvTpMin'), tpTimezone: val('llmPrvTpTimezone'), tpVersion: val('llmPrvTpVersion'),
      probe: (document.getElementById('llmPrvProbeStatus') || {}).textContent || '',
      choices: (document.getElementById('llmPrvModelChoices') || {}).innerHTML || '',
      options: (document.getElementById('llmPrvModelOptions') || {}).innerHTML || '',
      accessMode: providerAccessMode,
      accessSelected: Object.assign({}, providerAccessSelected),
      serveWindows: JSON.parse(JSON.stringify(providerServeWindowSchedule || [])),
      arrayID: val('llmPrvArray')
    };
    var opened = window.showProviderDialog(providerDialogID ? 'edit' : 'create', providerDialogID, {keepBilling:true, arrayID:snap.arrayID, timezone:snap.timezone, multiplier:snap.multiplier});
    function restore() {
      function set(id, value) { var node = document.getElementById(id); if (node && value != null) node.value = value; }
      set('llmPrvID', snap.id); set('llmPrvName', snap.name); set('llmPrvURL', snap.url); set('llmPrvKey', snap.key);
      set('llmPrvProtocol', snap.protocol); set('llmPrvModels', snap.models); set('llmPrvCaps', snap.caps);
      set('llmPrvPriority', snap.priority); set('llmPrvSequence', snap.sequence); set('llmPrvConc', snap.conc); set('llmPrvTimeout', snap.timeout);
      set('llmPrvArray', snap.arrayID);
      if (typeof window.setProviderDialogArray === 'function') window.setProviderDialogArray(snap.arrayID || '');
      set('llmPrvTimezone', snap.timezone); set('llmPrvMultiplier', snap.multiplier);
      set('llmPrvTpIn', snap.tpIn); set('llmPrvTpOut', snap.tpOut); set('llmPrvTpRmbIn', snap.tpRmbIn); set('llmPrvTpRmbOut', snap.tpRmbOut);
      set('llmPrvTpMin', snap.tpMin); set('llmPrvTpTimezone', snap.tpTimezone); set('llmPrvTpVersion', snap.tpVersion);
      providerAccessMode = snap.accessMode === 'nodes' ? 'nodes' : 'all';
      providerAccessSelected = snap.accessSelected || {};
      var accessRoot = document.querySelector('.provider-access-scope');
      if (accessRoot) accessRoot.outerHTML = providerAccessScopeSection();
      providerServeWindowSchedule = snap.serveWindows || [];
      renderProviderServeWindows();
      var status = document.getElementById('llmPrvProbeStatus');
      var choices = document.getElementById('llmPrvModelChoices');
      var list = document.getElementById('llmPrvModelOptions');
      if (status && snap.probe) status.textContent = snap.probe;
      if (choices && snap.choices) choices.innerHTML = snap.choices;
      if (list && snap.options) list.innerHTML = snap.options;
      if (typeof window.renderProviderCapabilityChips === 'function') window.renderProviderCapabilityChips();
      sgRestoreFocus(focus);
    }
    if (opened && typeof opened.then === 'function') {
      opened.then(function(openedOk) {
        if (openedOk === false) return;
        restore();
      }).catch(function() {});
    } else restore();
  }
  function sgRelabelAgentDialog() {
    var focus = sgSnapFocus(document.getElementById('llmDialogContent'));
    var idEl = document.getElementById('llmAgentID');
    var snap = { id: val('llmAgentID'), name: val('llmAgentName'), contact: val('llmAgentContact'), settlement: val('llmAgentSettlement'), desc: val('llmAgentDesc') };
    window.showLLMAgentDialog(idEl && idEl.readOnly ? 'edit' : 'create', snap.id);
    function set(id, value) { var node = document.getElementById(id); if (node && value != null) node.value = value; }
    set('llmAgentID', snap.id); set('llmAgentName', snap.name); set('llmAgentContact', snap.contact);
    set('llmAgentSettlement', snap.settlement); set('llmAgentDesc', snap.desc);
    sgRestoreFocus(focus);
  }
  function sgRerenderOpenDialog() {
    if (sgOpenKind === 'provider-config' && sgProviderDraft) { sgRenderProviderDialog(); return; }
    if (sgOpenKind === 'traffic') {
      var trafficFocus = sgSnapFocus(document.getElementById('llmDialogContent'));
      sgRenderTrafficDialog({ snap: sgSnapTraffic() });
      sgRestoreFocus(trafficFocus);
      return;
    }
    if (sgOpenKind === 'group') {
      var groupFocus = sgSnapFocus(document.getElementById('llmDialogContent'));
      sgRenderGroupDialog();
      sgRestoreFocus(groupFocus);
      return;
    }
    if (typeof renderProviders === 'function') renderProviders();
    if (typeof renderAgents === 'function') renderAgents();
    if (typeof renderServiceGroups === 'function') renderServiceGroups();
    if (typeof renderLLMEmbeddingModelRuntime === 'function') renderLLMEmbeddingModelRuntime();
    if (llmClassHeadViewVisible() && typeof window.sgLoadClassHead === 'function') window.sgLoadClassHead({quiet:true, relabel:true});
    if (sgOpenKind === 'array-rename') { renderProviderArrayRenameDialog(); return; }
    if (sgOpenKind === 'array-edit') { sgRelabelArrayEditDialog(); return; }
    if (sgOpenKind === 'provider') sgRelabelProviderDialog();
    if (sgOpenKind === 'agent') sgRelabelAgentDialog();
  }
  window.sgRerenderOpenDialog = sgRerenderOpenDialog;
  function sgFocusable(root) {
    if (!root) return [];
    return Array.prototype.slice.call(root.querySelectorAll('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'));
  }
  var sgSwallowEsc = false;
  function sgOnDialogKeydown(event) {
    var overlay = document.getElementById('llmDialogOverlay');
    if (!overlay || !overlay.classList.contains('show')) return;
    if (event.key === 'Escape') {
      if (sgSwallowEsc) { event.preventDefault(); sgSwallowEsc = false; return; }
      // Escape cancels the in-progress drag. Leave the editor open.
      if (sgRouteDrag) { sgRouteDropTo = null; return; }
      event.preventDefault();
      sgCloseCurrentDialog();
      return;
    }
    if (event.key !== 'Tab') return;
    var nodes = sgFocusable(document.getElementById('llmDialogContent'));
    if (!nodes.length) return;
    var first = nodes[0], last = nodes[nodes.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
  function openDialog(html, extraClass, focusFirst) {
    stopProviderBillingNowClock();
    var overlay = document.getElementById('llmDialogOverlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'llmDialogOverlay';
      overlay.className = 'session-modal-overlay';
      overlay.innerHTML = '<div class="session-modal cm-dialog-lg" id="llmDialogContent"></div>';
      if (typeof window.installOverlayDismiss === 'function') window.installOverlayDismiss(overlay, sgCloseCurrentDialog);
      else {
        var startedOnOverlay = false;
        overlay.addEventListener('pointerdown', function(e) { startedOnOverlay = e.target === overlay; });
        overlay.addEventListener('click', function(e) { if (startedOnOverlay && e.target === overlay) sgCloseCurrentDialog(); startedOnOverlay = false; });
      }
      document.body.appendChild(overlay);
      document.addEventListener('keydown', sgOnDialogKeydown);
    }
    var content = document.getElementById('llmDialogContent');
    content.className = 'session-modal cm-dialog-lg' + (extraClass ? ' ' + extraClass : '');
    content.innerHTML = html;
    overlay.classList.add('show');
    if (!extraClass || extraClass.indexOf('sg-form-dialog') < 0) sgOpenKind = '';
    if (focusFirst !== false) {
      var focus = sgFocusable(content);
      if (focus[0]) focus[0].focus();
    }
  }
  function closeDialog() {
    stopProviderBillingNowClock();
    stopWorkBuddyLoginPoll();
    sgRouteDrag = null;
    sgRouteDropTo = null;
    sgGroupReturn = null;
    var o = document.getElementById('llmDialogOverlay');
    var active = document.activeElement;
    var groupId = '';
    if ((sgOpenKind === 'group' || sgOpenKind === 'provider-config') && sgDraft) groupId = String(sgDraft.id || '').trim();
    var row = groupId ? sgServiceGroupRow(groupId) : null;
    var parked = false;
    if (row && row.focus) {
      row.classList.add('sg-group-reveal');
      row.focus({preventScroll:true});
      parked = true;
    } else if (groupId) {
      var list = document.getElementById('llmServiceGroupsList');
      if (list && list.focus) {
        list.setAttribute('tabindex', '-1');
        list.focus({preventScroll:true});
        parked = true;
      }
    }
    if (!parked && active && active.blur && o && o.contains(active)) active.blur();
    if (o) o.classList.remove('show');
    sgOpenKind = '';
    sgProviderDraft = null;
    if (groupId) sgScrollServiceGroupRow(groupId);
  }
  function sgCloseCurrentDialog() { closeDialog(); }
  window.closeDialog = closeDialog;
  window.sgCloseCurrentDialog = sgCloseCurrentDialog;
  function sgAlert(msg) { toast(msg, 'info'); }
  function sgConfirm(msg) { return window.confirm(msg); }
  function sgPrompt(msg, def) { sgSwallowEsc = true; return window.prompt(msg, def || ''); }

  function field(id, label, value, readonly, type) {
    return '<div><label for="' + id + '">' + esc(label) + '</label><input id="' + id + '" type="' + (type||'text') + '" value="' + esc(value||'') + '"' + (readonly ? ' readonly class="sg-readonly"' : '') + '></div>';
  }
  var llmSubTabNames = ['providers', 'agents', 'groups', 'classHead'];
  function llmSubTabButton(name) {
    return document.getElementById('llmSubTab' + name.charAt(0).toUpperCase() + name.slice(1));
  }
  window.switchLLMSubTab = function(tab) {
    if (llmSubTabNames.indexOf(tab) < 0) tab = 'providers';
    llmSubTabNames.forEach(function(name) {
      var view = document.getElementById('llmSubView' + name.charAt(0).toUpperCase() + name.slice(1));
      var btn = llmSubTabButton(name);
      var active = (name === tab);
      if (view) view.classList.toggle('hidden-view', !active);
      if (btn) {
        btn.className = active ? 'btn-secondary' : 'btn-ghost';
        btn.setAttribute('aria-pressed', String(active));
        btn.setAttribute('aria-selected', String(active));
        btn.tabIndex = active ? 0 : -1;
      }
    });
    if (tab === 'groups' && serviceGroups.length && !serviceGroupTrafficInFlight) loadServiceGroupTraffic();
    if (tab === 'classHead' && typeof window.sgReloadClassHeadPage === 'function') window.sgReloadClassHeadPage();
  };
  window.onLLMSubTabKeydown = function(event) {
    if (!event || ['ArrowLeft', 'ArrowRight', 'Home', 'End'].indexOf(event.key) < 0) return;
    var current = event.target && event.target.getAttribute ? event.target.getAttribute('id') : '';
    var index = llmSubTabNames.findIndex(function(name) { return llmSubTabButton(name) && llmSubTabButton(name).id === current; });
    if (index < 0) return;
    event.preventDefault();
    if (event.key === 'Home') index = 0;
    else if (event.key === 'End') index = llmSubTabNames.length - 1;
    else index = (index + (event.key === 'ArrowRight' ? 1 : -1) + llmSubTabNames.length) % llmSubTabNames.length;
    var next = llmSubTabNames[index];
    switchLLMSubTab(next);
    var nextButton = llmSubTabButton(next);
    if (nextButton) nextButton.focus();
  };
  window.openLLMClassHeadTab = function(){ switchLLMSubTab('classHead'); };
  window.showLLMProviderEditor = function() { window.showProviderArrayDialog(); };
  window.hideLLMProviderEditor = function() { sgCloseCurrentDialog(); };
  window.hideLLMGroupEditor = function() { sgCloseCurrentDialog(); };
  window.setProviderTrafficPeriod = setProviderTrafficPeriod;
  window.setServiceGroupTrafficPeriod = setServiceGroupTrafficPeriod;

  function val(id) { var el = document.getElementById(id); return el ? el.value.trim() : ''; }
  function num(id) { return Number(val(id)) || 0; }
  function csv(id) { return val(id).split(/[,\uff0c]+/).map(function(s){return s.trim();}).filter(Boolean); }
  function toast(msg, type) { if (window.showToast) window.showToast(msg, type); else alert(msg); }

  if (document.getElementById('tab-llmservice') && document.getElementById('tab-llmservice').classList.contains('active')) {
    setTimeout(window.initLLMServiceTab, 0);
  }
  maybeAutoSyncEmbeddingModel();
  if (typeof applyI18n === 'function' && !applyI18n._llmAdminAPIKey) {
    var llmApplyI18n = applyI18n;
    applyI18n = function() {
      llmApplyI18n();
      paintLLMAdminAPIKeyChrome();
      syncProviderTrafficSwitch();
      renderAllArrayTraffic();
    };
    applyI18n._llmAdminAPIKey = true;
  }
})();

// Credits redemption-card administration is kept in the Platform tab bundle
// so the HubCenter shell retains its established static asset layout.
(function () {
  'use strict';
  function enhanceButtonTypes(root = document) { root.querySelectorAll('button:not([type])').forEach(btn => { btn.type='button'; }); }
  function enhanceFormAccessibility(node) { enhanceButtonTypes(node); }
  document.addEventListener('DOMContentLoaded', () => { applyI18n();enhanceFormAccessibility();enhanceButtonTypes();enhanceStatusHints(); });
  if(typeof enhanceButtonTypes==='function')enhanceButtonTypes();
  function api(path, options) { return window.api(path, options || {}); }
  function status(message, isError) { var target=document.getElementById('redeemCardsStatusMessage'); if(target){target.textContent=message||'';target.className='sm-status'+(isError?' error':'');} }
  function trRedeem(key, vars) { var value=(typeof tr==='function'?tr(key):key); return Object.keys(vars||{}).reduce(function(result,name){return result.replace('{'+name+'}',String(vars[name]));},value); }
  function esc(value) { return String(value||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;'); }
  function statusLabel(card) { return card.status==='active'?trRedeem('redeemCardsUnused'):card.status==='redeemed'?trRedeem('redeemCardsRedeemed'):trRedeem('redeemCardsRevoked'); }
  function render(cards) { var root=document.getElementById('redeemCardsList'); if(!root)return; if(!cards.length){root.innerHTML='<div class="hint">'+esc(trRedeem('redeemCardsNone'))+'</div>';return;} root.innerHTML=cards.map(function(card){var detail=card.status==='redeemed'?trRedeem('redeemCardsRedeemedBy')+': '+(card.redeemed_by_email||card.redeemed_by_user_id||'–')+' · '+(card.redeemed_at||''):trRedeem('redeemCardsIssued')+': '+(card.issued_at||'');var flag=card.status==='active'&&!card.exported_at?'<span class="badge warn">'+esc(trRedeem('redeemCardsNotExported'))+'</span>':'';var revoke=card.status==='active'?'<button type="button" class="btn-danger-ghost" data-redeem-card-id="'+esc(card.id)+'">'+esc(trRedeem('redeemCardsRevoke'))+'</button>':'';return '<div class="data-row"><div class="data-row-main"><strong class="mono">'+esc(card.code)+'</strong><span class="data-row-meta">'+esc(detail)+'</span></div><div class="data-row-actions"><span>'+Number(card.credits||0)+' Credits</span><span class="badge info">'+esc(statusLabel(card))+'</span>'+flag+revoke+'</div></div>';}).join(''); }
  window.loadCreditRedeemCards=async function(){var filter=document.getElementById('redeemCardsStatus'),suffix=filter&&filter.value?'?status='+encodeURIComponent(filter.value):'';try{var result=await api('/api/v1/admin/credits/redeem-cards'+suffix);render(result.cards||[]);}catch(err){status(err.message||String(err),true);}};
  window.initRedeemCardsTab=window.loadCreditRedeemCards;
  document.addEventListener('click', function(event) { var button=event.target.closest('[data-redeem-card-id]'); if(button) window.revokeCreditRedeemCard(button.dataset.redeemCardId); });
  window.issueCreditRedeemCards=async function(){try{var result=await api('/api/v1/admin/credits/redeem-cards',{method:'POST',body:JSON.stringify({credits:Number(document.getElementById('redeemCardCredits').value||100),count:Number(document.getElementById('redeemCardCount').value||1)})});status(trRedeem('redeemCardsIssuedSuccess',{count:(result.cards||[]).length}));window.loadCreditRedeemCards();}catch(err){status(err.message||String(err),true);}};
  window.revokeCreditRedeemCard=async function(id){if(!confirm(trRedeem('redeemCardsConfirmRevoke')))return;try{await api('/api/v1/admin/credits/redeem-cards/'+encodeURIComponent(id)+'/revoke',{method:'POST'});window.loadCreditRedeemCards();}catch(err){status(err.message||String(err),true);}};
  window.exportCreditRedeemCards=async function(mode){try{var result=await api('/api/v1/admin/credits/redeem-cards/export',{method:'POST',body:JSON.stringify({mode:mode})}),rows=['code,credits'];(result.cards||[]).forEach(function(card){rows.push('="'+String(card.code||'').replace(/"/g,'""')+'",'+Number(card.credits||0));});var link=document.createElement('a');var href=URL.createObjectURL(new Blob([rows.join('\n')],{type:'text/csv;charset=utf-8'}));link.href=href;link.download='maclaw-credits-redeem-cards.csv';link.click();URL.revokeObjectURL(href);status(trRedeem('redeemCardsExportedSuccess',{count:(result.cards||[]).length}));window.loadCreditRedeemCards();}catch(err){status(err.message||String(err),true);}};
}());
