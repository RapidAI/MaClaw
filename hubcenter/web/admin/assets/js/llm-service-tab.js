/**
 * HubCenter Admin: LLM Service Tab
 * - Providers (CRUD, sequence, traffic, pause)
 * - Compute agents
 * - Service groups (dynamic official bands, default, traffic dialog)
 * - Classification head + embedding runtime
 * ASCII only. Chinese via \uXXXX or data-i18n.
 */

if (typeof I18N_EN !== 'undefined') {
  Object.assign(I18N_EN, {sgRouteHint:'Exposed model alias with provider failover',providerProbeModels:'Probe',providerProbing:'Probing models...',providerProbeEmpty:'No models returned.',providerProbeFailed:'Probe failed',providerCapabilityPreset:'Preset capabilities'});
}
if (typeof I18N_ZH !== 'undefined') {
  Object.assign(I18N_ZH, {sgRouteHint:'\u66b4\u9732\u6a21\u578b\u522b\u540d\uff0c\u6309\u670d\u52a1\u5546\u4f18\u5148\u7ea7\u5b9e\u73b0\u6545\u969c\u8f6c\u79fb',providerProbeModels:'\u63a2\u6d4b',providerProbing:'\u6b63\u5728\u63a2\u6d4b\u6a21\u578b...',providerProbeEmpty:'\u672a\u8fd4\u56de\u6a21\u578b\u5217\u8868\u3002',providerProbeFailed:'\u63a2\u6d4b\u5931\u8d25',providerCapabilityPreset:'\u9884\u7f6e\u80fd\u529b'});
}

(function() {
  'use strict';

  var I18N = {
    en: {
      llmTabTitle: 'LLM Service', llmTabDesc: 'Manage LLM providers, compute agents, and model service groups.',
      providersTitle: 'LLM Providers', providersDesc: 'Backend LLM API endpoints for model routing.',
      addProvider: 'Add Provider', editProvider: 'Edit', deleteProvider: 'Delete', noProviders: 'No providers configured.',
      providerDialogTitleNew: 'New Provider', providerDialogTitleEdit: 'Edit Provider',
      fieldID: 'Provider ID', fieldName: 'Name', fieldURL: 'API URL', fieldKey: 'API Key',
      fieldProtocol: 'Protocol', fieldModels: 'Models (comma-separated)', fieldCapabilities: 'Capabilities',
      fieldPriority: 'Priority', fieldConcurrency: 'Max Concurrency', fieldTimeout: 'Timeout (sec)',
      fieldSequence: 'Sequence', sequenceHint: 'Lower numbers are tried first. 0 means unset.',
      lbGroup: 'LB group', pauseProvider: 'Pause', resumeProvider: 'Resume',
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
      sgRemoveRoute: 'Remove', sgExposedModel: 'Exposed Model', sgNoProviders: 'No providers assigned. Add a provider above.',
      sgAccessPolicy: 'Access Policy', sgPolicyFreeHint: 'no grant needed', sgPolicyGrantHint: 'needs card/grant',
      sgRoutes: 'Provider Routes', sgAddRoute: '+ Add Route',
      sgProviderAlreadyAdded: 'Provider already added to this route.',
      sgProviderConfigTitle: 'Provider Config', sgCapabilityTags: 'Capability Tags',
      sgCapabilityHint: 'Capabilities of this upstream model. Tags steer routing when the request asks for tools, vision, or similar.',
      sgExtraTags: 'Extra Tags (custom)', sgPriority: 'Priority',
      sgResolutionTier: 'Resolution Tier', sgCreditMultiplier: 'Credit Multiplier',
      sgBillingMode: 'Billing Mode', sgBillingModeHint: 'paid = charge Credits, free = no user charge, empty = legacy',
      sgBillingModePaid: 'Paid', sgBillingModeFree: 'Free', sgBillingModeLegacy: 'Legacy (empty)',
      tokenPricingTitle: 'Token Pricing (per 10k tokens)', tokenPricingHint: 'Credits fields are billed; RMB fields are display-only and never affect Credits.',
      fieldInputCredits: 'Input Credits / 10k', fieldOutputCredits: 'Output Credits / 10k',
      fieldInputRMB: 'Input RMB / 10k (ref)', fieldOutputRMB: 'Output RMB / 10k (ref)',
      fieldMinimumCredits: 'Minimum Credits / request', fieldPricingTimezone: 'Pricing Timezone', fieldPricingVersion: 'Pricing Version',
      providerPriceSummary: 'Base price', providerSetPrice: 'Set price', pricingSchedule: 'Time-of-use prices', pricingAddWindow: 'Add price window', pricingRemoveWindow: 'Remove',
      pricingWindowHint: 'A matching window overrides only the prices entered below; blank values keep the base price. The first matching window wins.',
      pricingDroppedWindows: 'Fix incomplete price windows before saving.',
      billingInvalid: 'Fix invalid pricing numbers before saving.',
      billingPaidNeedsCredits: 'Paid billing requires at least one positive Credits price (input, output, or minimum).',
      sgIDNameRequired: 'ID and Name are required.', sgRouteNeedsProvider: 'Each route needs at least one provider.',
      chooseProvider: 'Choose Provider',
      fieldHubID: 'Hub ID', fieldTenantID: 'Tenant ID',
      fieldHubRequired: 'Select a Hub', fieldTenantRequired: 'Select a tenant', noHubs: 'No registered Hubs.', defaultTenant: 'Default tenant',
      save: 'Save', cancel: 'Cancel', confirm: 'Confirm', delete: 'Delete',
      saved: 'Saved successfully.', deleted: 'Deleted.', error: 'Error', sgFailed: 'Failed',
      testProvider: 'Test Status', providerTesting: 'Testing...', providerTestOK: 'Available', providerTestFailed: 'Unavailable',
      providerTestLatency: 'Latency', providerTestModels: 'Models',
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
      sgWin_24h: '24h', sgWin_7d: '7d', sgWin_30d: '30d',
      sgTierHigh: 'Official high (official-high)', sgTierMid: 'Official mid (official-mid)', sgTierLow: 'Official low (official-low)',
      sgPlanDesignNoLow: 'Plan and design cannot use the low band.',
      sgProtectedModel: 'auto and official quality bands cannot be renamed or removed while they are in use.',
      sgCatalogTitle: 'Catalog & floor', sgCatalogHint: 'Empty catalog lists only auto. Clients that pin official-high, official-mid, or official-low skip L1 and go straight to that band.',
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
      weekdaySun: 'Sun', weekdayMon: 'Mon', weekdayTue: 'Tue', weekdayWed: 'Wed', weekdayThu: 'Thu', weekdayFri: 'Fri', weekdaySat: 'Sat'
    },
    zh: {
      llmTabTitle: 'LLM \u670d\u52a1', llmTabDesc: '\u7ba1\u7406 LLM \u670d\u52a1\u5546\u3001\u7b97\u529b\u4ee3\u7406\u5546\u548c\u6a21\u578b\u670d\u52a1\u7ec4\u3002',
      providersTitle: 'LLM \u670d\u52a1\u5546', providersDesc: '\u540e\u7aef LLM API \u7aef\u70b9\u914d\u7f6e\u3002',
      addProvider: '\u6dfb\u52a0\u670d\u52a1\u5546', editProvider: '\u7f16\u8f91', deleteProvider: '\u5220\u9664', noProviders: '\u672a\u914d\u7f6e\u670d\u52a1\u5546\u3002',
      providerDialogTitleNew: '\u65b0\u5efa\u670d\u52a1\u5546', providerDialogTitleEdit: '\u7f16\u8f91\u670d\u52a1\u5546',
      fieldID: '\u670d\u52a1\u5546 ID', fieldName: '\u540d\u79f0', fieldURL: 'API \u5730\u5740', fieldKey: 'API \u5bc6\u94a5',
      fieldProtocol: '\u534f\u8bae', fieldModels: '\u6a21\u578b\uff08\u9017\u53f7\u5206\u9694\uff09', fieldCapabilities: '\u80fd\u529b\u6807\u7b7e',
      fieldPriority: '\u4f18\u5148\u7ea7', fieldConcurrency: '\u6700\u5927\u5e76\u53d1', fieldTimeout: '\u8d85\u65f6\uff08\u79d2\uff09',
      fieldSequence: '\u5e8f\u5217', sequenceHint: '\u6570\u5b57\u8d8a\u5c0f\u8d8a\u5148\u8bd5\u30020 \u8868\u793a\u672a\u8bbe\u3002',
      lbGroup: 'LB \u7ec4', pauseProvider: '\u6682\u505c', resumeProvider: '\u6062\u590d',
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
      sgRemoveRoute: '\u5220\u9664', sgExposedModel: '\u66b4\u9732\u6a21\u578b\u540d', sgNoProviders: '\u672a\u5206\u914d\u670d\u52a1\u5546\u3002\u8bf7\u5728\u4e0a\u65b9\u6dfb\u52a0\u3002',
      sgAccessPolicy: '\u8bbf\u95ee\u7b56\u7565', sgPolicyFreeHint: '\u65e0\u9700\u6388\u6743', sgPolicyGrantHint: '\u9700\u8981\u5361/\u6388\u6743',
      sgRoutes: '\u670d\u52a1\u5546\u8def\u7531', sgAddRoute: '+ \u6dfb\u52a0\u8def\u7531',
      sgProviderAlreadyAdded: '\u8be5\u670d\u52a1\u5546\u5df2\u6dfb\u52a0\u3002',
      sgProviderConfigTitle: '\u670d\u52a1\u5546\u914d\u7f6e', sgCapabilityTags: '\u80fd\u529b\u6807\u7b7e',
      sgCapabilityHint: '\u8fd9\u6761\u4e0a\u6e38\u6a21\u578b\u7684\u80fd\u529b\u3002\u8bf7\u6c42\u8981 tools / vision \u7b49\u65f6\u4f1a\u6309\u6807\u7b7e\u8def\u7531\u3002',
      sgExtraTags: '\u989d\u5916\u6807\u7b7e\uff08\u81ea\u5b9a\u4e49\uff09', sgPriority: '\u4f18\u5148\u7ea7',
      sgResolutionTier: '\u89e3\u6790\u5c42\u7ea7', sgCreditMultiplier: '\u989d\u5ea6\u500d\u7387',
      sgBillingMode: '\u8ba1\u8d39\u6a21\u5f0f', sgBillingModeHint: 'paid \u6263\u8d39\uff0cfree \u514d\u8d39\uff0c\u7a7a\u4e3a\u517c\u5bb9',
      sgBillingModePaid: '\u6536\u8d39', sgBillingModeFree: '\u514d\u8d39', sgBillingModeLegacy: '\u517c\u5bb9\uff08\u7a7a\uff09',
      tokenPricingTitle: 'Token \u8ba1\u8d39\uff08\u6bcf\u4e07 Token\uff09', tokenPricingHint: 'Credits \u5b57\u6bb5\u53c2\u4e0e\u6263\u8d39\uff1bRMB \u4ec5\u5c55\u793a\u3002',
      fieldInputCredits: '\u8f93\u5165 Credits / \u4e07', fieldOutputCredits: '\u8f93\u51fa Credits / \u4e07',
      fieldInputRMB: '\u8f93\u5165 RMB / \u4e07\uff08\u53c2\u8003\uff09', fieldOutputRMB: '\u8f93\u51fa RMB / \u4e07\uff08\u53c2\u8003\uff09',
      fieldMinimumCredits: '\u5355\u6b21\u6700\u4f4e\u6d88\u8d39 Credits', fieldPricingTimezone: '\u8ba1\u8d39\u65f6\u533a', fieldPricingVersion: '\u8ba1\u8d39\u7248\u672c',
      providerPriceSummary: '\u57fa\u7840\u5355\u4ef7', providerSetPrice: '\u8bbe\u7f6e\u5355\u4ef7', pricingSchedule: '\u5206\u65f6\u5355\u4ef7', pricingAddWindow: '\u6dfb\u52a0\u5355\u4ef7\u65f6\u6bb5', pricingRemoveWindow: '\u5220\u9664',
      pricingWindowHint: '\u547d\u4e2d\u65f6\u6bb5\u4ec5\u8986\u76d6\u5df2\u586b\u5355\u4ef7\uff0c\u7559\u7a7a\u7684\u65b9\u5411\u7ee7\u7eed\u4f7f\u7528\u57fa\u7840\u5355\u4ef7\uff1b\u9996\u4e2a\u547d\u4e2d\u7684\u65f6\u6bb5\u751f\u6548\u3002',
      pricingDroppedWindows: '\u8bf7\u5148\u4fee\u6b63\u4e0d\u5b8c\u6574\u7684\u5206\u65f6\u5355\u4ef7\u540e\u518d\u4fdd\u5b58\u3002',
      billingInvalid: '\u8bf7\u4fee\u6b63\u65e0\u6548\u7684\u8ba1\u8d39\u6570\u503c\u540e\u518d\u4fdd\u5b58\u3002',
      billingPaidNeedsCredits: '\u6536\u8d39\u6a21\u5f0f\u9700\u81f3\u5c11\u4e00\u9879 Credits \u6b63\u6570\uff08\u8f93\u5165/\u8f93\u51fa/\u6700\u4f4e\u6d88\u8d39\uff09\u3002',
      sgIDNameRequired: 'ID \u548c\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a\u3002', sgRouteNeedsProvider: '\u6bcf\u4e2a\u8def\u7531\u81f3\u5c11\u9700\u8981\u4e00\u4e2a\u670d\u52a1\u5546\u3002',
      chooseProvider: '\u9009\u62e9\u670d\u52a1\u5546',
      fieldHubID: 'Hub ID', fieldTenantID: '\u79df\u6237 ID',
      fieldHubRequired: '\u8bf7\u9009\u62e9 Hub', fieldTenantRequired: '\u8bf7\u9009\u62e9\u79df\u6237', noHubs: '\u6682\u65e0\u5df2\u6ce8\u518c Hub\u3002', defaultTenant: '\u9ed8\u8ba4\u79df\u6237',
      save: '\u4fdd\u5b58', cancel: '\u53d6\u6d88', confirm: '\u786e\u8ba4', delete: '\u5220\u9664',
      saved: '\u4fdd\u5b58\u6210\u529f\u3002', deleted: '\u5df2\u5220\u9664\u3002', error: '\u9519\u8bef', sgFailed: '\u5931\u8d25',
      testProvider: '\u6d4b\u8bd5\u72b6\u6001', providerTesting: '\u6d4b\u8bd5\u4e2d...', providerTestOK: '\u53ef\u7528', providerTestFailed: '\u5f02\u5e38',
      providerTestLatency: '\u8017\u65f6', providerTestModels: '\u6a21\u578b',
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
      sgWin_24h: '24h', sgWin_7d: '7d', sgWin_30d: '30d',
      sgTierHigh: '\u5b98\u65b9\u9ad8\u6863\uff08official-high\uff09', sgTierMid: '\u5b98\u65b9\u4e2d\u6863\uff08official-mid\uff09', sgTierLow: '\u5b98\u65b9\u4f4e\u6863\uff08official-low\uff09',
      sgPlanDesignNoLow: '\u89c4\u5212\u548c\u8bbe\u8ba1\u4e0d\u80fd\u8d70\u4f4e\u6863\u3002',
      sgProtectedModel: '\u52a8\u6001\u7ec4\u91cc\uff0cauto \u548c\u6b63\u5728\u4f7f\u7528\u7684\u5b98\u65b9\u6863\u4e0d\u80fd\u6539\u540d\u6216\u5220\u6389\u3002',
      sgCatalogTitle: '\u76ee\u5f55\u4e0e\u8d28\u91cf\u4e0b\u9650', sgCatalogHint: '\u76ee\u5f55\u4e3a\u7a7a\u65f6\u53ea\u5217\u51fa auto\u3002\u5ba2\u6237\u7aef\u76f4\u63a5\u6307 official-high / mid / low \u4f1a\u8df3\u8fc7 L1\u3002',
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
      weekdaySun: '\u65e5', weekdayMon: '\u4e00', weekdayTue: '\u4e8c', weekdayWed: '\u4e09', weekdayThu: '\u56db', weekdayFri: '\u4e94', weekdaySat: '\u516d'
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

  var providers = [], agents = [], serviceGroups = [];
  var defaultServiceGroupId = '';
  var providerTestStates = {};
  var providerDialogID = '';
  var providerBillingSchedule = [];
  var providerTokenPricingSchedule = [];
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
  var sgCapabilityOptions = ['reasoning','tools','document','vision','audio','code','search'];
  var sgPriorityOptions = [0,10,20,30,40,50,60,70,80,90,100];
  var sgResolutionOptions = [0,1,2,3,4,5];
  var sgMultiplierOptions = [0.25,0.5,0.75,1,1.5,2,3,5,10];
  var sgTrafficSeq = 0, sgTrySeq = 0, sgHeadSeq = 0, sgHeadPollTimer = 0;
  var _sgTrafficDataWin = '24h';
  var _testingGroupId = '';

  window.initLLMServiceTab = async function() {
    if (llmInitInFlight) return llmInitInFlight;
    llmInitInFlight = (async function() {
      await Promise.all([loadProviders(), loadAgents(), loadServiceGroups()]);
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
    return (n / 1000000).toFixed(n < 10000000 ? 1 : 0) + 'M';
  }
  function formatTrafficExact(value) { return String(Math.round(Number(value || 0))); }
  function patchProviderTraffic() {
    var root = document.getElementById('llmProvidersList');
    if (!root) return;
    root.querySelectorAll('#llmProvidersList .provider-traffic[data-provider-id]').forEach(function(node) {
      applyProviderTrafficNode(node, node.getAttribute('data-provider-id'));
    });
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
    var want = String(id || '');
    if (providerTrafficById[want]) return providerTrafficById[want];
    var keys = Object.keys(providerTrafficById || {});
    var lower = want.toLowerCase();
    for (var i = 0; i < keys.length; i++) {
      if (String(keys[i]).toLowerCase() === lower) return providerTrafficById[keys[i]];
    }
    return null;
  }
  function providerTrafficWindow(row) {
    if (!row) return null;
    if (providerTrafficPeriod === 'week') return row.week;
    if (providerTrafficPeriod === 'month') return row.month;
    return row.day;
  }
  function applyProviderTrafficNode(node, id) {
    if (!node) return;
    var row = providerTrafficRow(id);
    var hasData = !!(row && (row.day || row.week || row.month));
    var pending = !hasData && (!providerTrafficReady || providerTrafficInFlight);
    node.className = 'provider-traffic' + (pending ? ' is-pending' : '');
    var win = providerTrafficWindow(row) || {};
    var input = Number(win.input_tokens || 0);
    var output = Number(win.output_tokens || 0);
    var total = Number(win.total_tokens || 0) || (input + output);
    function line(label, value, totalCls) {
      var tip = providerTrafficPeriodLabel(providerTrafficPeriod) + ' \u00b7 ' + label + ' \u00b7 ' + formatTrafficExact(value);
      return '<div class="provider-traffic-line"><span class="k">' + esc(label) + '</span><span class="v' + (totalCls ? ' total' : '') + '" title="' + esc(tip) + '">' + esc(pending ? t('trafficLoading') : formatTrafficTokens(value)) + '</span></div>';
    }
    node.innerHTML = '<div class="provider-traffic-col">'
      + line(t('trafficIn'), input)
      + line(t('trafficOut'), output)
      + line(t('trafficTotal'), total, true)
      + '</div>';
  }
  function setProviderTrafficPeriod(period) {
    var next = period === 'week' || period === 'month' ? period : 'day';
    var changed = next !== providerTrafficPeriod;
    providerTrafficPeriod = next;
    syncProviderTrafficSwitch();
    if (changed) patchProviderTraffic();
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
  async function loadProviderTraffic() {
    var seq = ++providerTrafficLoadSeq;
    providerTrafficInFlight += 1;
    if (!providerTrafficReady) patchProviderTraffic();
    try {
      var data = await api('/api/admin/llm/providers/traffic');
      if (seq !== providerTrafficLoadSeq) return;
      var rows = {};
      if (Array.isArray(data.traffic)) {
        data.traffic.forEach(function(item) { if (item && item.provider_id) rows[item.provider_id] = item; });
      } else {
        rows = data.traffic || {};
      }
      providerTrafficById = rows;
      providerTrafficReady = true;
    } catch (e) {
      if (seq !== providerTrafficLoadSeq) return;
      providerTrafficReady = true;
      if (!Object.keys(providerTrafficById || {}).length) toast(e.message || t('sgFailed'), 'error');
    } finally {
      providerTrafficInFlight = Math.max(0, providerTrafficInFlight - 1);
      if (seq === providerTrafficLoadSeq) patchProviderTraffic();
    }
  }
  function renderProviders() {
    var el = document.getElementById('llmProvidersList');
    if (!el) return;
    syncProviderTrafficSwitch();
    if (!providers.length) { el.innerHTML = '<div class="hint">' + esc(t('noProviders')) + '</div>'; return; }
    el.innerHTML = sortedProviders().map(function(p) {
      var testState = providerTestStates[p.id];
      var testHTML = renderProviderTestState(testState);
      var testDisabled = testState && testState.status === 'testing' ? ' disabled' : '';
      var providerArg = jsArg(p.id);
      var seq = Number(p.sequence || 0);
      var paused = !!p.paused;
      return '<div class="data-row' + (paused ? ' is-paused' : '') + '">'
        + '<div class="provider-seq' + (seq > 0 ? '' : ' is-unset') + '">' + esc(seq > 0 ? String(seq) : '-') + '</div>'
        + '<div class="data-row-main"><div class="data-row-title"><strong>' + esc(p.name||p.id) + '</strong>'
        + (paused ? '<span class="badge warn">' + esc(t('pauseProvider')) + '</span>' : '')
        + (p.lb_group && Number(p.lb_group_size||0) >= 2 ? '<span class="badge info">' + esc(t('lbGroup')) + ' ' + esc(p.lb_group) + '</span>' : '')
        + providerBillingBadge(p)
        + '</div>'
        + '<span class="data-row-meta">' + esc(p.api_url) + ' \u00b7 ' + esc(p.protocol||'openai')
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
    }).join('');
    patchProviderTraffic();
  }
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
  function providerBillingNowParts(timezone) {
    timezone = String(timezone || 'Asia/Shanghai').trim() || 'Asia/Shanghai';
    var now = new Date();
    function read(tz) {
      var parts = new Intl.DateTimeFormat('en-US', {
        timeZone: tz, weekday: 'short', hour: '2-digit', minute: '2-digit', hour12: false, hourCycle: 'h23'
      }).formatToParts(now);
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
    ['input_credits_per_10k','output_credits_per_10k','input_rmb_per_10k','output_rmb_per_10k','minimum_request_credits'].forEach(function(key) {
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
    ['input_credits_per_10k','output_credits_per_10k','input_rmb_per_10k','output_rmb_per_10k','minimum_request_credits'].forEach(function(key) {
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
        + numberField('input_rmb_per_10k', t('fieldInputRMB'))
        + numberField('output_rmb_per_10k', t('fieldOutputRMB'))
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
    var fields = { llmPrvTpIn: 'input_credits_per_10k', llmPrvTpOut: 'output_credits_per_10k', llmPrvTpRmbIn: 'input_rmb_per_10k', llmPrvTpRmbOut: 'output_rmb_per_10k', llmPrvTpMin: 'minimum_request_credits' };
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
    if (validSchedule.length) tp.price_schedule = validSchedule;
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
    return '<div class="provider-billing" style="margin-top:12px"><div class="provider-billing-head"><div><div class="provider-billing-title"><strong>' + esc(t('tokenPricingTitle')) + '</strong></div>'
      + '<div class="provider-billing-hint">' + esc(t('tokenPricingHint')) + '</div></div></div>'
      + '<div class="provider-billing-fields">'
      + numField('llmPrvTpIn', t('fieldInputCredits'), 'input_credits_per_10k', '1')
      + numField('llmPrvTpOut', t('fieldOutputCredits'), 'output_credits_per_10k', '4')
      + numField('llmPrvTpRmbIn', t('fieldInputRMB'), 'input_rmb_per_10k', '0.02')
      + numField('llmPrvTpRmbOut', t('fieldOutputRMB'), 'output_rmb_per_10k', '0.08')
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
  window.showProviderDialog = function(mode, id, opts) {
    var p = mode === 'edit' ? providers.find(function(x){return x.id===id;}) : null;
    providerDialogID = mode === 'edit' ? (id || '') : '';
    sgOpenKind = 'provider';
    if (!(opts && opts.keepBilling)) {
      providerBillingSchedule = cloneProviderBillingSchedule(p && p.credit_multiplier_schedule);
      providerTokenPricingSchedule = cloneProviderTokenPricingSchedule(p && p.token_pricing && p.token_pricing.price_schedule);
    }
    var title = mode === 'edit' ? t('providerDialogTitleEdit') : t('providerDialogTitleNew');
    var html = sgDialogChrome(title,
      '<div class="sg-form-grid">'
      + field('llmPrvID', t('fieldID'), p ? p.id : '', mode==='edit')
      + field('llmPrvName', t('fieldName'), p ? p.name : '')
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
      + providerTokenPricingSection(p)
      + providerBillingSection(p, opts),
      '<button class="btn-primary" onclick="saveProvider(' + jsArg(mode==='edit'?id:'') + ')">' + esc(t('save')) + '</button>'
      + '<button class="btn-ghost" onclick="sgCloseCurrentDialog()">' + esc(t('cancel')) + '</button>');
    openDialog(html, 'sg-form-dialog');
    window.renderProviderCapabilityChips();
    startProviderBillingNowClock();
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
    var billing = readProviderBilling();
    if ((providerBillingSchedule || []).length !== billing.credit_multiplier_schedule.length) {
      toast(t('billingDroppedWindows'), 'error');
      return;
    }
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
    payload.timezone = billing.timezone;
    payload.credit_multiplier = billing.credit_multiplier;
    payload.credit_multiplier_schedule = billing.credit_multiplier_schedule;
    var tokenPricing = readProviderTokenPricing();
    if (tokenPricing === null) { toast(t('billingInvalid'), 'error'); return; }
    payload.token_pricing = tokenPricing;
    var key = val('llmPrvKey');
    if (key) payload.api_key = key;
    try {
      if (editID) await api('/api/admin/llm/providers/' + encodeURIComponent(editID), { method: 'PUT', body: JSON.stringify(payload) });
      else await api('/api/admin/llm/providers', { method: 'POST', body: JSON.stringify(payload) });
      sgCloseCurrentDialog(); toast(t('saved'), 'success'); loadProviders({ traffic: false });
    } catch(e) { toast(e.message, 'error'); }
  };
  window.deleteLLMProvider = async function(id) {
    if (!sgConfirm(t('deleteProvider') + ': ' + id + '?')) return;
    try { await api('/api/admin/llm/providers/' + encodeURIComponent(id), { method: 'DELETE' }); toast(t('deleted'), 'success'); loadProviders({ traffic: false }); }
    catch(e) { toast(e.message, 'error'); }
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

  window.testLLMServiceGroup = async function(groupId) {
    if (_testingGroupId) { toast(isZh() ? '\u6d4b\u8bd5\u8fdb\u884c\u4e2d\uff0c\u8bf7\u7a0d\u5019...' : 'Test in progress, please wait...', 'info'); return; }
    var group = serviceGroups.find(function(g) { return g.id === groupId; });
    if (!group) { toast(t('sgFailed'), 'error'); return; }
    var firstModel = (group.models || [])[0];
    var providerID = '';
    var routeModel = '';
    if (firstModel) {
      var pids = firstModel.provider_ids || [];
      var pconfigs = firstModel.provider_configs || [];
      if (pconfigs.length) {
        providerID = pconfigs[0].provider_id || '';
        routeModel = pconfigs[0].model || '';
      }
      if (!providerID && pids.length) providerID = pids[0];
    }
    if (!providerID) { toast(t('sgRouteNeedsProvider'), 'error'); return; }
    var provider = providers.find(function(p) { return p.id === providerID; });
    if (!provider) { toast(t('sgFailed'), 'error'); return; }
    _testingGroupId = groupId;
    try {
      var data = await api('/api/admin/llm/providers/test-chat', { method: 'POST', body: JSON.stringify({
        provider_id: provider.id, api_url: provider.api_url, model: routeModel || ((provider.models && provider.models.length === 1) ? provider.models[0] : firstModel.name) || '', protocol: provider.protocol || 'openai', wire_api: provider.wire_api || 'chat'
      }) });
      if (data.success) toast(t('providerTestOK') + ' ' + (data.latency_ms || 0) + 'ms', 'success');
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
    if (!serviceGroups.length) { el.innerHTML = '<div class="hint">' + esc(t('noGroups')) + '</div>'; return; }
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
      return '<div class="data-row llm-service-group-row"><div class="data-row-main"><strong>' + esc(g.name||g.id) + '</strong> ' + tags
        + '<span class="data-row-meta">' + esc(agentName) + ' \u00b7 ' + esc(g.description||'') + ' \u00b7 ' + esc(modelNames||'no models')
        + ' \u00b7 ' + (g.models||[]).length + ' route(s)</span></div>'
        + '<div class="service-group-traffic' + (serviceGroupTrafficReady ? '' : ' is-pending') + '" data-service-group-id="' + esc(g.id) + '"></div>'
        + '<div class="data-row-actions">'
        + (isDynamic ? '<button class="btn-ghost" onclick="editLLMClassTraffic('+jsArg(g.id)+')">' + esc(t('sgClassTrafficOpen')) + '</button>' : '')
        + '<button class="btn-ghost" onclick="testLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('testProvider')) + '</button>'
        + '<button class="btn-ghost" onclick="editLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('editGroup')) + '</button>'
        + (isDefault ? '' : '<button class="btn-ghost" onclick="setDefaultLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('sgSetDefault')) + '</button>')
        + (isOfficial?'':'<button class="btn-danger-ghost" onclick="deleteLLMServiceGroup('+jsArg(g.id)+')">' + esc(t('deleteGroup')) + '</button>')
        + '</div></div>';
    }).join('');
    patchServiceGroupTraffic();
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
    var ids = [];
    (m && m.provider_ids || []).forEach(function(id){ if (id && ids.indexOf(id) < 0) ids.push(id); });
    (m && m.provider_configs || []).forEach(function(c){ if (c && c.provider_id && ids.indexOf(c.provider_id) < 0) ids.push(c.provider_id); });
    return ids;
  }
  function sgProviderByID(id){return providers.find(function(x){return x.id===id;})||null;}
  function sgProviderModels(id){var p=sgProviderByID(id);return (p&&p.models||[]).map(function(x){return String(x||'').trim();}).filter(Boolean);}
  function sgEffectiveRouteModel(c){var m=(c&&c.model||'').trim();if(m)return m;var models=sgProviderModels(c&&c.provider_id||'');return models.length===1?models[0]:'';}
  function sgRouteKey(c){return (c&&c.provider_id||'').trim()+'\u0000'+sgEffectiveRouteModel(c);}
  function sgNormalizeTokenPricing(src){
    var p = src && typeof src === 'object' ? src : {};
    var out = {};
    function num(k){ var v = p[k]; if(v===undefined||v===null||v==='') return undefined; var n=Number(v); if(!isFinite(n)||n<0) return undefined; return n; }
    var v;
    v=num('input_credits_per_10k'); if(v!==undefined) out.input_credits_per_10k=v;
    v=num('output_credits_per_10k'); if(v!==undefined) out.output_credits_per_10k=v;
    v=num('input_rmb_per_10k'); if(v!==undefined) out.input_rmb_per_10k=v;
    v=num('output_rmb_per_10k'); if(v!==undefined) out.output_rmb_per_10k=v;
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
    var configs=(m&&m.provider_configs||[]).map(function(c){return{provider_id:(c.provider_id||'').trim(),model:(c.model||'').trim(),billing_mode:String(c.billing_mode||'').trim(),capability_tags:(c.capability_tags||[]).slice(),priority:c.priority||0,resolution_tier:c.resolution_tier||0,credit_multiplier:c.credit_multiplier||1,token_pricing:sgNormalizeTokenPricing(c.token_pricing)};}).filter(function(c){return c.provider_id;});
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
      models:(g&&g.models||[]).map(function(m){return{name:m.name||'auto',provider_ids:sgProviderIDsFromModel(m),provider_configs:sgProviderConfigsFromModel(m),capability_tags:(m.capability_tags||[]).slice(),priority:m.priority||50,resolution_tier:m.resolution_tier||0,credit_multiplier:m.credit_multiplier||1};})};
  }
  function sgEmptyGroup(){return{id:'',name:'',description:'',agent_id:'maclaw_official',agent_name:agentNameByID('maclaw_official')||'MaClaw official',access_policy:'free',kind:'dynamic',quality_floor:'',exposed_models:[],routes:sgDefaultDynamicRoutes(),models:[{name:'auto',provider_ids:[],provider_configs:[],capability_tags:[],priority:50,resolution_tier:0,credit_multiplier:1}]};}
  function sgProviderName(id){var p=providers.find(function(x){return x.id===id;});return p?(p.name||p.id):id;}
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
  function sgEmptyModel(name){return{name:name||'auto',provider_ids:[],provider_configs:[],capability_tags:[],priority:50,resolution_tier:0,credit_multiplier:1};}
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
      m.provider_configs=src.map(function(c){return{provider_id:c.provider_id,model:c.model,billing_mode:c.billing_mode,capability_tags:(c.capability_tags||[]).slice(),priority:c.priority,resolution_tier:c.resolution_tier,credit_multiplier:c.credit_multiplier,token_pricing:sgNormalizeTokenPricing(c.token_pricing)};});
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

  function sgRenderProviderCard(rowIndex,routeIndex,total){
    var model=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];
    var cfg=sgGetProviderConfig(model,routeIndex);
    if(!cfg)return '';
    var features=sgFormatCaps(cfg&&cfg.capability_tags);
    var billingLabel = cfg.billing_mode ? cfg.billing_mode : 'legacy';
    var pricingBrief = sgFormatPricingBrief(cfg.token_pricing);
    var pricingMeta = pricingBrief ? ' \u00b7 '+esc(billingLabel)+' \u00b7 '+esc(pricingBrief) : ' \u00b7 '+esc(billingLabel);
    return '<div class="sg-provider-card"><div class="sg-row-head"><strong>'+esc(sgProviderName(cfg.provider_id))+' #'+(routeIndex+1)+'</strong>'
      +'<div class="sg-actions"><button class="btn-ghost sg-tiny-btn" onclick="sgEditProviderConfig('+rowIndex+','+routeIndex+')">'+esc(t('editGroup'))+'</button>'
      +(routeIndex>0?'<button class="btn-ghost sg-icon-btn" onclick="sgMoveProvider('+rowIndex+','+routeIndex+',-1)">\u2191</button>':'')
      +(routeIndex<total-1?'<button class="btn-ghost sg-icon-btn" onclick="sgMoveProvider('+rowIndex+','+routeIndex+',1)">\u2193</button>':'')
      +'<button class="btn-danger-ghost sg-tiny-btn" onclick="sgRemoveProvider('+rowIndex+','+routeIndex+')">\u2715</button></div></div>'
      +'<div class="sg-provider-meta">Upstream: '+esc((cfg&&cfg.model)||'-')+' \u00b7 '+esc(t('fieldCapabilities'))+': '+esc(features)+' \u00b7 P:'+(cfg&&cfg.priority||0)+pricingMeta+'</div></div>';
  }
  function sgRenderRouteRow(model,rowIndex){
    model.provider_configs=sgProviderConfigsFromModel(model);
    var locked=sgDraft&&sgDraft.kind==='dynamic'&&sgIsLockedModelName(model.name);
    var cards=(model.provider_configs||[]).map(function(cfg,pi){return sgRenderProviderCard(rowIndex,pi,(model.provider_configs||[]).length);}).join('');
    var providerOptions=!providers.length?'<option value="">('+esc(t('noProviders'))+')</option>'
      :'<option value="">-- '+esc(t('chooseProvider'))+' --</option>'+providers.map(function(p){return'<option value="'+esc(p.id)+'">'+esc(p.name||p.id)+'</option>';}).join('');
    return '<div class="sg-route-card"><div class="sg-row-head"><div><strong>'+esc(sgModelLabel(model.name||'auto'))+'</strong><span class="sg-route-hint">'+esc(t('sgRouteHint'))+'</span></div>'
      +(locked?'':'<button class="btn-danger-ghost sg-remove-route" onclick="sgRemoveRoute('+rowIndex+')">'+esc(t('sgRemoveRoute'))+'</button>')
      +'</div><div class="sg-route-grid"><div><label class="sg-label-sm">'+esc(t('sgExposedModel'))+'</label>'
      +'<input class="sg-field-full" value="'+esc(model.name||'auto')+'"'+(locked?' disabled':'')+' oninput="sgSetRouteField('+rowIndex+',\'name\',this.value)"></div>'
      +'<div class="sg-provider-add"><select id="sgProviderAdd'+rowIndex+'">'+providerOptions+'</select><button class="btn-ghost" onclick="sgAddProviderToRoute('+rowIndex+')">+</button></div></div>'
      +(cards||'<div class="sg-empty-provider">'+esc(t('sgNoProviders'))+'</div>')+'</div>';
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
  function sgRenderGroupDialog(){
    var d=sgDraft||sgEmptyGroup();
    if(d.kind==='dynamic') sgPrepareDynamicDraft(d);
    var title=sgMode==='edit'?t('groupDialogTitleEdit'):t('groupDialogTitleNew');
    var agentOptions = agents.map(function(a){return '<option value="'+esc(a.id)+'"'+(d.agent_id===a.id?' selected':'')+'>'+esc(a.name||a.id)+'</option>';}).join('');
    var rows=(d.models||[]).map(function(m,i){return sgRenderRouteRow(m,i);}).join('');
    var body='<div class="sg-form-grid">'
      +'<div><label>'+esc(t('fieldGroupID'))+'</label><input id="sgFieldID" value="'+esc(d.id)+'"'+(sgMode==='edit'?' readonly class="sg-readonly"':'')+' oninput="sgSetField(\'id\',this.value)"></div>'
      +'<div><label>'+esc(t('fieldGroupName'))+'</label><input id="sgFieldName" value="'+esc(d.name)+'" oninput="sgSetField(\'name\',this.value)"></div></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupAgent'))+'</label><select id="sgFieldAgent" class="sg-field-full" onchange="sgSetField(\'agent_id\',this.value)"><option value="">--</option>'+agentOptions+'</select></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupDesc'))+'</label><input id="sgFieldDesc" class="sg-field-full" value="'+esc(d.description)+'" oninput="sgSetField(\'description\',this.value)"></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('sgAccessPolicy'))+'</label><select id="sgFieldPolicy" onchange="sgSetField(\'access_policy\',this.value)"><option value="free"'+(d.access_policy!=='grant_required'?' selected':'')+'>'+esc(sgPolicyLabel('free'))+' ('+esc(t('sgPolicyFreeHint'))+')</option><option value="grant_required"'+(d.access_policy==='grant_required'?' selected':'')+'>'+esc(sgPolicyLabel('grant_required'))+' ('+esc(t('sgPolicyGrantHint'))+')</option></select></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupKind'))+'</label><select id="sgFieldKind" onchange="sgSetKind(this.value)"><option value="dynamic"'+(d.kind==='dynamic'?' selected':'')+'>'+esc(t('sgKindDynamic'))+'</option><option value="static"'+(d.kind!=='dynamic'?' selected':'')+'>'+esc(t('sgKindStatic'))+'</option></select></div>'
      +(d.kind==='dynamic'?sgRenderWorkloadTable(d)+sgRenderCatalog(d):'')
      +'<div class="sg-section">'+sgSectionHead(t('sgRoutes'), '')+'<div class="sg-flex-between"><span></span><button class="btn-ghost" onclick="sgAddRoute()">'+esc(t('sgAddRoute'))+'</button></div>'+rows+'</div>';
    sgOpenKind='group';
    openDialog(sgDialogChrome(title, body, '<button class="btn-primary" onclick="sgSaveGroup()">'+esc(t('save'))+'</button><button class="btn-ghost" onclick="sgCloseCurrentDialog()">'+esc(t('cancel'))+'</button>'), 'sg-form-dialog');
  }
  window.showGroupDialog=function(mode,id){
    var g=mode==='edit'?serviceGroups.find(function(x){return x.id===id;}):null;
    sgMode=mode==='edit'?'edit':'create';
    sgDraft=g?sgCloneGroup(g):sgEmptyGroup();
    if(sgDraft.kind==='dynamic') sgPrepareDynamicDraft(sgDraft,{fillEmptyOfficial:true});
    sgRenderGroupDialog();
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
    sgDraft.models[i][k]=v.trim();
  };
  window.sgAddRoute=function(){if(!sgDraft)sgDraft=sgEmptyGroup();sgDraft.models.push(sgEmptyModel('auto'));sgRenderGroupDialog();};
  window.sgRemoveRoute=function(i){var m=sgDraft&&sgDraft.models&&sgDraft.models[i]; if(m&&sgIsLockedModelName(m.name)){toast(t('sgProtectedModel'),'info');return;} if(sgDraft&&sgDraft.models){sgDraft.models.splice(i,1);sgRenderGroupDialog();}};
  window.sgAddProviderToRoute=function(i){var sel=document.getElementById('sgProviderAdd'+i);var id=sel&&sel.value;if(!id){toast(t('chooseProvider'),'info');return;}var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;m.provider_configs=sgProviderConfigsFromModel(m);var models=sgProviderModels(id);var chosen='';if(models.length){for(var mi=0;mi<models.length;mi++){if(sgRouteDuplicateIndex(m,{provider_id:id,model:models[mi]},-1)<0){chosen=models[mi];break;}}if(!chosen){toast(sgDuplicateRouteMessage({provider_id:id,model:models[0]}),'error');return;}}else if(sgRouteDuplicateIndex(m,{provider_id:id,model:''},-1)>=0){toast(sgDuplicateRouteMessage({provider_id:id,model:''}),'error');return;}m.provider_configs.push({provider_id:id,model:chosen,billing_mode:'',capability_tags:[],priority:0,resolution_tier:0,credit_multiplier:1,token_pricing:{}});m.provider_ids=sgProviderIDsFromModel(m);if(sgDraft.kind==='dynamic')sgFillEmptyOfficialBandsFromAuto(sgDraft);sgRenderGroupDialog();};
  window.sgMoveProvider=function(i,routeIndex,delta){var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;m.provider_configs=sgProviderConfigsFromModel(m);var to=routeIndex+delta;if(routeIndex<0||to<0||routeIndex>=m.provider_configs.length||to>=m.provider_configs.length)return;var item=m.provider_configs.splice(routeIndex,1)[0];m.provider_configs.splice(to,0,item);m.provider_ids=sgProviderIDsFromModel(m);sgRenderGroupDialog();};
  window.sgRemoveProvider=function(i,routeIndex){var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;m.provider_configs=sgProviderConfigsFromModel(m);m.provider_configs.splice(routeIndex,1);m.provider_ids=sgProviderIDsFromModel(m);sgRenderGroupDialog();};
  function sgClonePricingForDraft(tp){
    var p=sgNormalizeTokenPricing(tp||{});
    var out={input_credits_per_10k:p.input_credits_per_10k,output_credits_per_10k:p.output_credits_per_10k,input_rmb_per_10k:p.input_rmb_per_10k,output_rmb_per_10k:p.output_rmb_per_10k,minimum_request_credits:p.minimum_request_credits,timezone:p.timezone||'',version:p.version||''};
    if(p.price_schedule) out.price_schedule=p.price_schedule;
    return out;
  }
  window.sgEditProviderConfig=function(rowIndex,routeIndex){var model=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];var cfg=sgGetProviderConfig(model,routeIndex);if(!cfg)return;sgOpenKind='provider-config';sgProviderDraft={rowIndex:rowIndex,routeIndex:routeIndex,providerID:cfg.provider_id,draft:{model:cfg&&cfg.model||'',billing_mode:String(cfg&&cfg.billing_mode||'').trim(),capability_tags:(cfg&&cfg.capability_tags||[]).slice(),priority:cfg&&cfg.priority||0,resolution_tier:cfg&&cfg.resolution_tier||0,credit_multiplier:cfg&&cfg.credit_multiplier||1,token_pricing:sgClonePricingForDraft(cfg.token_pricing)}};sgRenderProviderDialog();};
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
      +'<div class="sg-block-sm" style="margin-top:12px;border-top:1px solid var(--line);padding-top:12px"><div class="sg-label-strong">'+esc(t('tokenPricingTitle'))+'</div><div class="hint">'+esc(t('tokenPricingHint'))+'</div>'
      +'<div class="sg-form-grid" style="margin-top:8px"><div><label class="sg-label-sm">'+esc(t('sgBillingMode'))+'</label><select onchange="sgSetProviderField(\'billing_mode\',this.value)"><option value=""'+(billingMode===''?' selected':'')+'>'+esc(t('sgBillingModeLegacy'))+'</option><option value="paid"'+(billingMode==='paid'?' selected':'')+'>'+esc(t('sgBillingModePaid'))+'</option><option value="free"'+(billingMode==='free'?' selected':'')+'>'+esc(t('sgBillingModeFree'))+'</option></select><div class="hint">'+esc(t('sgBillingModeHint'))+'</div></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldPricingTimezone'))+'</label><input class="sg-field-full" value="'+esc(tp.timezone||'')+'" placeholder="Asia/Shanghai" oninput="sgSetTokenPricingField(\'timezone\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldInputCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.input_credits_per_10k!==undefined?String(tp.input_credits_per_10k):'')+'" placeholder="1" oninput="sgSetTokenPricingField(\'input_credits_per_10k\',this.value)"></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldOutputCredits'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.output_credits_per_10k!==undefined?String(tp.output_credits_per_10k):'')+'" placeholder="4" oninput="sgSetTokenPricingField(\'output_credits_per_10k\',this.value)"></div></div>'
      +'<div class="sg-form-grid"><div><label class="sg-label-sm">'+esc(t('fieldInputRMB'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.input_rmb_per_10k!==undefined?String(tp.input_rmb_per_10k):'')+'" placeholder="0.02" oninput="sgSetTokenPricingField(\'input_rmb_per_10k\',this.value)"></div>'
      +'<div><label class="sg-label-sm">'+esc(t('fieldOutputRMB'))+'</label><input type="number" min="0" step="0.01" value="'+esc(tp.output_rmb_per_10k!==undefined?String(tp.output_rmb_per_10k):'')+'" placeholder="0.08" oninput="sgSetTokenPricingField(\'output_rmb_per_10k\',this.value)"></div></div>'
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
    for(var kk in tp){ if(tp.hasOwnProperty(kk) && (kk==='input_credits_per_10k'||kk==='output_credits_per_10k'||kk==='input_rmb_per_10k'||kk==='output_rmb_per_10k'||kk==='minimum_request_credits')){ if(tp[kk]!==''&&tp[kk]!==undefined){ var nn=Number(tp[kk]); if(!isFinite(nn)||nn<0){ toast(t('billingInvalid'),'error'); return; } } } }
    var billingMode=String(sgProviderDraft.draft.billing_mode||'').trim();
    if(billingMode==='paid'){
      var hasCredits = (tp.input_credits_per_10k!==undefined&&isFinite(tp.input_credits_per_10k)&&tp.input_credits_per_10k>0) || (tp.output_credits_per_10k!==undefined&&isFinite(tp.output_credits_per_10k)&&tp.output_credits_per_10k>0) || (tp.minimum_request_credits!==undefined&&isFinite(tp.minimum_request_credits)&&tp.minimum_request_credits>0);
      if(!hasCredits){ toast(t('billingPaidNeedsCredits'),'error'); return; }
    }
    cfg.model=next.model;cfg.billing_mode=billingMode;cfg.capability_tags=(sgProviderDraft.draft.capability_tags||[]).slice();cfg.priority=sgProviderDraft.draft.priority||0;cfg.resolution_tier=sgProviderDraft.draft.resolution_tier||0;cfg.credit_multiplier=sgProviderDraft.draft.credit_multiplier||1;
    var cleaned={}; for(var k in tp){ if(tp.hasOwnProperty(k)){ var val=tp[k]; if(val!==''&&val!==undefined&&val!==null){ if(k==='timezone'||k==='version'){ if(String(val).trim()) cleaned[k]=String(val).trim(); } else if(k==='price_schedule' && Array.isArray(val) && val.length){ try{ cleaned[k]=JSON.parse(JSON.stringify(val)); }catch(e){ cleaned[k]=val.slice(); } } else if(isFinite(Number(val))) cleaned[k]=Number(val); } } }
    if(cleaned.price_schedule && !cleaned.timezone) cleaned.timezone='Asia/Shanghai';
    if(cleaned.input_credits_per_10k===undefined && cleaned.output_credits_per_10k===undefined && cleaned.minimum_request_credits===undefined && cleaned.input_rmb_per_10k===undefined && cleaned.output_rmb_per_10k===undefined && !cleaned.timezone && !cleaned.version && !cleaned.price_schedule){
      // keep empty object for legacy; omit pricing entirely to stay legacy-compatible
      // but preserve an explicit empty to avoid sending stray keys
    }
    cfg.token_pricing=cleaned;
    sgProviderDraft=null;sgOpenKind='group';sgRenderGroupDialog();};
  window.sgCancelProviderConfig=function(){sgProviderDraft=null;sgOpenKind='group';sgRenderGroupDialog();};
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
    sgSaveBusy=true;
    try{
      if(sgMode==='edit'){await api('/api/admin/llm/service-groups/'+encodeURIComponent(payload.id),{method:'PUT',body:JSON.stringify(payload)});}
      else{await api('/api/admin/llm/service-groups',{method:'POST',body:JSON.stringify(payload)});}
      sgCloseCurrentDialog();toast(t('saved'),'success');loadServiceGroups();
    }catch(e){toast(e.message,'error');}
    finally{sgSaveBusy=false;}
  };

  function sgDialogAlive(kind,id){
    var overlay=document.getElementById('llmDialogOverlay');
    return !!(overlay&&overlay.classList.contains('show')&&sgOpenKind===kind&&sgDraft&&String(sgDraft.id||'')===String(id||''));
  }
  function sgTrafficDialogAlive(id){ return sgDialogAlive('traffic',id); }
  function sgWinButtons(id){
    var cur=_sgTrafficDataWin||'24h';
    return ['24h','7d','30d'].map(function(win){
      return '<button class="btn-ghost sg-traffic-win'+(cur===win?' is-active':'')+'" type="button" data-win="'+win+'" onclick="sgLoadClassTraffic('+jsArg(id)+','+jsArg(win)+')">'+esc(t('sgWin_'+win))+'</button>';
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
    return {tryOpen:!!(tryBox&&tryBox.open),tryText:valOf('sgTryRunText'),tryWorkflow:valOf('sgTryWorkflow'),tryPhase:valOf('sgTryPhase'),tryTask:valOf('sgTryTask'),tryOut:out?out.innerHTML:'',win:_sgTrafficDataWin||'24h',board:board?board.innerHTML:''};
  }
  function sgRestoreTraffic(snap){
    if(!snap)return;
    var tryBox=document.querySelector('.sg-try-box');
    if(tryBox)tryBox.open=!!snap.tryOpen;
    function set(id,v){var el=document.getElementById(id);if(el&&v!=null)el.value=v;}
    set('sgTryRunText',snap.tryText); set('sgTryWorkflow',snap.tryWorkflow); set('sgTryPhase',snap.tryPhase); set('sgTryTask',snap.tryTask);
    var out=document.getElementById('sgTryRunOut'); if(out&&snap.tryOut)out.innerHTML=snap.tryOut;
    var board=document.getElementById('sgClassTraffic'); if(board&&snap.board)board.innerHTML=snap.board;
    if(snap.win)_sgTrafficDataWin=snap.win;
  }
  function sgRenderTrafficDialog(opts){
    var d=sgDraft||{};
    var id=String(d.id||'');
    sgOpenKind='traffic';
    var snap=opts&&opts.snap;
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
    windowName=windowName||_sgTrafficDataWin||'24h';
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
    root.innerHTML = '<div class="item-head"><div class="llm-embed-title"><div class="item-title">'+esc(t('runtimeTitle'))+'</div>'+embeddingRuntimeBadge(status)
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
      options: (document.getElementById('llmPrvModelOptions') || {}).innerHTML || ''
    };
    window.showProviderDialog(providerDialogID ? 'edit' : 'create', providerDialogID, {keepBilling:true, timezone:snap.timezone, multiplier:snap.multiplier});
    function set(id, value) { var node = document.getElementById(id); if (node && value != null) node.value = value; }
    set('llmPrvID', snap.id); set('llmPrvName', snap.name); set('llmPrvURL', snap.url); set('llmPrvKey', snap.key);
    set('llmPrvProtocol', snap.protocol); set('llmPrvModels', snap.models); set('llmPrvCaps', snap.caps);
    set('llmPrvPriority', snap.priority); set('llmPrvSequence', snap.sequence); set('llmPrvConc', snap.conc); set('llmPrvTimeout', snap.timeout);
    set('llmPrvTimezone', snap.timezone); set('llmPrvMultiplier', snap.multiplier);
    set('llmPrvTpIn', snap.tpIn); set('llmPrvTpOut', snap.tpOut); set('llmPrvTpRmbIn', snap.tpRmbIn); set('llmPrvTpRmbOut', snap.tpRmbOut);
    set('llmPrvTpMin', snap.tpMin); set('llmPrvTpTimezone', snap.tpTimezone); set('llmPrvTpVersion', snap.tpVersion);
    var status = document.getElementById('llmPrvProbeStatus');
    var choices = document.getElementById('llmPrvModelChoices');
    var list = document.getElementById('llmPrvModelOptions');
    if (status && snap.probe) status.textContent = snap.probe;
    if (choices && snap.choices) choices.innerHTML = snap.choices;
    if (list && snap.options) list.innerHTML = snap.options;
    if (typeof window.renderProviderCapabilityChips === 'function') window.renderProviderCapabilityChips();
    sgRestoreFocus(focus);
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
  function openDialog(html, extraClass) {
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
    var focus = sgFocusable(content);
    if (focus[0]) focus[0].focus();
  }
  function closeDialog() {
    stopProviderBillingNowClock();
    var o = document.getElementById('llmDialogOverlay');
    if (o) o.classList.remove('show');
    sgOpenKind = '';
    sgProviderDraft = null;
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
  window.showLLMProviderEditor = function() { window.showProviderDialog('create'); };
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
