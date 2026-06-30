/*
 * Capability marketplace admin module.
 * ASCII only.
 */
(function(global) {
  if (typeof I18N !== 'undefined') {
    I18N.en = Object.assign({}, I18N.en, {
      navMarketplace: 'Marketplace',
      navMarketplaceDesc: 'Skills, MCP, purchases, and rollout',
      marketplaceTabTitle: 'Capability Marketplace',
      marketplaceTabSubtitle: 'Manage enterprise Skill/MCP policy, paid approvals, imports, and MCP definitions.',
      marketplaceSubtabMarket: 'Enterprise Capability Market',
      marketplaceSubtabWorkflowReviews: 'Workflow Reviews',
      marketplaceSubtabSettings: 'Market Settings',
      workflowReviewsTitle: 'Approval Workflow Reviews', workflowReviewsDesc: 'Review submitted approval workflows before publishing them into the enterprise capability market.', workflowReviewsEmpty: 'No workflow submissions are waiting for review.', workflowReviewOpen: 'Review', workflowReviewApprove: 'Publish', workflowReviewReject: 'Reject', workflowReviewRejectPrompt: 'Rejection reason (10-2000 characters)', workflowReviewRejectTitle: 'Reject workflow submission', workflowReviewRejectDesc: 'Give the author a clear reason so they can revise and resubmit.', workflowReviewRejectReason: 'Rejection reason', workflowReviewRejectReasonInvalid: 'Reason must be 10-2000 characters.', workflowReviewDetailTitle: 'Review detail', workflowReviewGraphSummary: '{nodes} nodes, {edges} lines', workflowReviewNodesTitle: 'Node configuration', workflowReviewOpenDesigner: 'Open Designer', workflowReviewPublished: 'Workflow published to capability market.', workflowReviewRejected: 'Workflow submission rejected.', workflowReviewLoadFailed: 'Workflow reviews failed: {error}', maclawAppApprove: 'Approve App', maclawAppReject: 'Reject App', maclawAppApproved: 'MaClaw App approved.', maclawAppRejected: 'MaClaw App returned for revision.',
      marketplacePolicyTitle: 'Enterprise Policy', marketplacePolicyDesc: 'Control how MaClaw clients search and install capabilities.',
      marketplaceEnterpriseOnlyInstall: 'Only install from enterprise Hub', marketplaceEnterpriseOnlySearch: 'Only search enterprise Hub', marketplaceViewMode: 'View mode', marketplacePreferredUploadTarget: 'Preferred upload target', marketplaceUploadHubCenter: 'HubCenter', marketplaceUploadEnterpriseHub: 'Enterprise Hub', marketplaceSavePolicy: 'Save Policy',
      marketplaceRequestsTitle: 'Purchase Approvals', marketplaceRequestsDesc: 'Paid HubCenter capabilities wait here until an admin approves online purchase.', marketplaceRequestsStatus: 'Status', marketplaceReload: 'Reload', marketplaceApprove: 'Approve', marketplaceReject: 'Reject', marketplaceNoRequests: 'No acquisition requests match this status.',
      marketplaceCatalogTitle: 'Enterprise Capabilities', marketplaceCatalogDesc: 'Capabilities available from this Hub marketplace.', marketplaceCapabilityType: 'Type', marketplaceCapabilityAll: 'All', marketplaceMakeRequired: 'Set Required', marketplaceMakeRecommended: 'Recommend', marketplaceNoCapabilities: 'No capabilities have been imported yet.',
      marketplaceSearchTitle: 'Search External Markets', marketplaceSearchDesc: 'Hub admins can search HubCenter, ClawHub, and GitHub. HubCenter paid items create purchase requests.', marketplaceSource: 'Source', marketplaceQuery: 'Keyword', marketplaceSearch: 'Search', marketplaceImport: 'Import / Request', marketplaceNoResults: 'No external results.',
      marketplaceMCPTitle: 'MCP JSON Editor', marketplaceMCPDesc: 'Create or update an enterprise MCP capability from JSON.', marketplaceMCPId: 'Capability ID', marketplaceMCPName: 'Display name', marketplaceMCPPublisher: 'Publisher', marketplaceMCPVersion: 'Version', marketplaceMCPJson: 'MCP server JSON', marketplaceMCPSecrets: 'Secret requirements JSON', marketplaceMCPPricing: 'Pricing JSON', marketplaceMCPLicense: 'License JSON', marketplaceSaveMCP: 'Save MCP', marketplaceUseSelected: 'Use Selected', marketplaceMCPNew: '+ New MCP', marketplaceMCPType: 'Type', marketplaceMCPTypeRemote: 'Remote (HTTP/SSE)', marketplaceMCPTypeLocal: 'Local (stdio)', marketplaceMCPCommand: 'Command', marketplaceMCPArgs: 'Arguments (JSON array)', marketplaceMCPEnv: 'Environment variables (JSON)',
      marketplaceBillingTitle: 'HubCenter Billing', marketplaceBillingDesc: 'Hub signs purchases with Hub customer id and admin email.', marketplaceLoadBilling: 'Load Billing', marketplaceNoLicenses: 'No HubCenter licenses returned.', marketplaceCancel: 'Cancel',
      marketplaceOutputReady: 'Marketplace admin module ready.', marketplaceLoadFailed: 'Marketplace load failed: {error}', marketplaceSaveFailed: 'Marketplace save failed: {error}', marketplaceSearchFailed: 'Marketplace search failed: {error}', marketplaceImportFailed: 'Marketplace import failed: {error}', marketplaceActionDone: 'Marketplace action completed.', marketplacePolicySaved: 'Marketplace policy saved.', marketplaceMcpSaved: 'MCP capability saved.', marketplaceInvalidJson: 'Invalid JSON: {error}',
      marketplaceTestMCP: 'Test Connection', marketplaceTestMCPTesting: 'Testing...', marketplaceTestMCPSuccess: 'Connected. {count} tool(s) available.', marketplaceTestMCPFailed: 'Connection failed: {error}',
      marketplaceShowingFirst: '{total} items total, showing first {count}'
    });
    I18N.zh = Object.assign({}, I18N.zh, {
      navMarketplace: '\u80fd\u529b\u5e02\u573a', navMarketplaceDesc: 'Skill\u3001MCP\u3001\u8d2d\u4e70\u548c\u4e0b\u53d1', marketplaceTabTitle: '\u80fd\u529b\u5e02\u573a', marketplaceTabSubtitle: '\u7ba1\u7406\u4f01\u4e1a Skill/MCP \u7b56\u7565\u3001\u4ed8\u8d39\u5ba1\u6279\u3001\u5bfc\u5165\u548c MCP \u5b9a\u4e49\u3002',
      marketplaceSubtabMarket: '\u4f01\u4e1a\u80fd\u529b\u5e02\u573a', marketplaceSubtabWorkflowReviews: '\u5de5\u4f5c\u6d41\u5ba1\u6838', marketplaceSubtabSettings: '\u80fd\u529b\u5e02\u573a\u8bbe\u7f6e',
      workflowReviewsTitle: '\u5ba1\u6279\u5de5\u4f5c\u6d41\u5ba1\u6838', workflowReviewsDesc: '\u5ba1\u6838\u5df2\u63d0\u4ea4\u7684\u5ba1\u6279\u5de5\u4f5c\u6d41\uff0c\u901a\u8fc7\u540e\u53d1\u5e03\u5230\u4f01\u4e1a\u80fd\u529b\u5e02\u573a\u3002', workflowReviewsEmpty: '\u6682\u65e0\u5f85\u5ba1\u6838\u7684\u5de5\u4f5c\u6d41\u63d0\u4ea4\u3002', workflowReviewOpen: '\u5ba1\u6838', workflowReviewApprove: '\u53d1\u5e03', workflowReviewReject: '\u62d2\u7edd', workflowReviewRejectPrompt: '\u62d2\u7edd\u539f\u56e0\uff0810-2000 \u5b57\uff09', workflowReviewRejectTitle: '\u62d2\u7edd\u5de5\u4f5c\u6d41\u63d0\u4ea4', workflowReviewRejectDesc: '\u8bf7\u7ed9\u4f5c\u8005\u660e\u786e\u539f\u56e0\uff0c\u4fbf\u4e8e\u4fee\u6539\u540e\u91cd\u65b0\u63d0\u4ea4\u3002', workflowReviewRejectReason: '\u62d2\u7edd\u539f\u56e0', workflowReviewRejectReasonInvalid: '\u539f\u56e0\u5fc5\u987b\u4e3a 10-2000 \u4e2a\u5b57\u7b26\u3002', workflowReviewDetailTitle: '\u5ba1\u6838\u8be6\u60c5', workflowReviewGraphSummary: '{nodes} \u4e2a\u8282\u70b9\uff0c{edges} \u6761\u8fde\u7ebf', workflowReviewNodesTitle: '\u8282\u70b9\u914d\u7f6e', workflowReviewOpenDesigner: '\u6253\u5f00\u8bbe\u8ba1\u5668', workflowReviewPublished: '\u5de5\u4f5c\u6d41\u5df2\u53d1\u5e03\u5230\u80fd\u529b\u5e02\u573a\u3002', workflowReviewRejected: '\u5de5\u4f5c\u6d41\u63d0\u4ea4\u5df2\u62d2\u7edd\u3002', workflowReviewLoadFailed: '\u5de5\u4f5c\u6d41\u5ba1\u6838\u52a0\u8f7d\u5931\u8d25\uff1a{error}', maclawAppApprove: '\u901a\u8fc7\u5e94\u7528', maclawAppReject: '\u9000\u56de\u5e94\u7528', maclawAppApproved: 'MaClaw App \u5df2\u901a\u8fc7\u5ba1\u6838\u3002', maclawAppRejected: 'MaClaw App \u5df2\u9000\u56de\u4fee\u6539\u3002',
      marketplacePolicyTitle: '\u4f01\u4e1a\u7b56\u7565', marketplacePolicyDesc: '\u63a7\u5236 MaClaw \u5ba2\u6237\u7aef\u641c\u7d22\u548c\u5b89\u88c5\u80fd\u529b\u7684\u65b9\u5f0f\u3002', marketplaceEnterpriseOnlyInstall: '\u53ea\u5141\u8bb8\u4ece\u4f01\u4e1a Hub \u5b89\u88c5', marketplaceEnterpriseOnlySearch: '\u53ea\u5141\u8bb8\u641c\u7d22\u4f01\u4e1a Hub', marketplaceViewMode: '\u89c6\u56fe\u6a21\u5f0f', marketplacePreferredUploadTarget: '\u9996\u9009\u4e0a\u4f20\u4f4d\u7f6e', marketplaceUploadHubCenter: 'HubCenter', marketplaceUploadEnterpriseHub: '\u4f01\u4e1a Hub', marketplaceSavePolicy: '\u4fdd\u5b58\u7b56\u7565',
      marketplaceRequestsTitle: '\u8d2d\u4e70\u5ba1\u6279', marketplaceRequestsDesc: '\u4ed8\u8d39 HubCenter \u80fd\u529b\u5728\u7ba1\u7406\u5458\u5ba1\u6279\u540e\u624d\u4f1a\u53d1\u8d77\u5728\u7ebf\u8d2d\u4e70\u3002', marketplaceRequestsStatus: '\u72b6\u6001', marketplaceReload: '\u5237\u65b0', marketplaceApprove: '\u6279\u51c6', marketplaceReject: '\u62d2\u7edd', marketplaceNoRequests: '\u5f53\u524d\u72b6\u6001\u4e0b\u6ca1\u6709\u7533\u8bf7\u3002',
      marketplaceCatalogTitle: '\u4f01\u4e1a\u80fd\u529b', marketplaceCatalogDesc: '\u672c Hub \u80fd\u529b\u5e02\u573a\u53ef\u7528\u7684\u80fd\u529b\u3002', marketplaceCapabilityType: '\u7c7b\u578b', marketplaceCapabilityAll: '\u5168\u90e8', marketplaceMakeRequired: '\u8bbe\u4e3a\u5fc5\u88c5', marketplaceMakeRecommended: '\u8bbe\u4e3a\u63a8\u8350', marketplaceNoCapabilities: '\u6682\u65e0\u5df2\u5bfc\u5165\u80fd\u529b\u3002',
      marketplaceSearchTitle: '\u641c\u7d22\u5916\u90e8\u5e02\u573a', marketplaceSearchDesc: 'Hub \u7ba1\u7406\u5458\u53ef\u641c\u7d22 HubCenter\u3001ClawHub \u548c GitHub\u3002HubCenter \u4ed8\u8d39\u9879\u4f1a\u751f\u6210\u8d2d\u4e70\u7533\u8bf7\u3002', marketplaceSource: '\u6765\u6e90', marketplaceQuery: '\u5173\u952e\u5b57', marketplaceSearch: '\u641c\u7d22', marketplaceImport: '\u5bfc\u5165 / \u7533\u8bf7', marketplaceNoResults: '\u6682\u65e0\u5916\u90e8\u7ed3\u679c\u3002',
      marketplaceMCPTitle: 'MCP JSON \u7f16\u8f91', marketplaceMCPDesc: '\u901a\u8fc7 JSON \u521b\u5efa\u6216\u66f4\u65b0\u4f01\u4e1a MCP \u80fd\u529b\u3002', marketplaceMCPId: '\u80fd\u529b ID', marketplaceMCPName: '\u663e\u793a\u540d\u79f0', marketplaceMCPPublisher: '\u53d1\u5e03\u8005', marketplaceMCPVersion: '\u7248\u672c', marketplaceMCPJson: 'MCP \u670d\u52a1\u5668 JSON', marketplaceMCPSecrets: 'Secret \u9700\u6c42 JSON', marketplaceMCPPricing: '\u8ba1\u8d39 JSON', marketplaceMCPLicense: '\u8bb8\u53ef JSON', marketplaceSaveMCP: '\u4fdd\u5b58 MCP', marketplaceUseSelected: '\u5957\u7528\u5230\u7f16\u8f91\u5668', marketplaceMCPNew: '+ \u65b0\u5efa MCP', marketplaceMCPType: '\u7c7b\u578b', marketplaceMCPTypeRemote: '\u8fdc\u7a0b (HTTP/SSE)', marketplaceMCPTypeLocal: '\u672c\u5730 (stdio)', marketplaceMCPCommand: '\u547d\u4ee4', marketplaceMCPArgs: '\u53c2\u6570 (JSON \u6570\u7ec4)', marketplaceMCPEnv: '\u73af\u5883\u53d8\u91cf (JSON)', marketplaceCancel: '\u53d6\u6d88',
      marketplaceBillingTitle: 'HubCenter \u8d26\u6237', marketplaceBillingDesc: 'Hub \u4f7f\u7528 Hub \u5ba2\u6237 ID \u548c\u7ba1\u7406\u5458\u90ae\u7bb1\u5b8c\u6210\u8d2d\u4e70\u3002', marketplaceLoadBilling: '\u52a0\u8f7d\u8d26\u6237', marketplaceNoLicenses: '\u6682\u65e0 HubCenter \u8bb8\u53ef\u3002', marketplaceOutputReady: '\u80fd\u529b\u5e02\u573a\u7ba1\u7406\u6a21\u5757\u5df2\u5c31\u7eea\u3002', marketplaceLoadFailed: '\u52a0\u8f7d\u80fd\u529b\u5e02\u573a\u5931\u8d25\uff1a{error}', marketplaceSaveFailed: '\u4fdd\u5b58\u80fd\u529b\u5e02\u573a\u5931\u8d25\uff1a{error}', marketplaceSearchFailed: '\u641c\u7d22\u80fd\u529b\u5e02\u573a\u5931\u8d25\uff1a{error}', marketplaceImportFailed: '\u5bfc\u5165\u80fd\u529b\u5931\u8d25\uff1a{error}', marketplaceActionDone: '\u64cd\u4f5c\u5df2\u5b8c\u6210\u3002', marketplacePolicySaved: '\u80fd\u529b\u5e02\u573a\u7b56\u7565\u5df2\u4fdd\u5b58\u3002', marketplaceMcpSaved: 'MCP \u80fd\u529b\u5df2\u4fdd\u5b58\u3002', marketplaceInvalidJson: 'JSON \u65e0\u6548\uff1a{error}',
      marketplaceTestMCP: '\u6d4b\u8bd5\u8fde\u63a5', marketplaceTestMCPTesting: '\u6d4b\u8bd5\u4e2d...', marketplaceTestMCPSuccess: '\u8fde\u63a5\u6210\u529f\u3002{count} \u4e2a\u5de5\u5177\u53ef\u7528\u3002', marketplaceTestMCPFailed: '\u8fde\u63a5\u5931\u8d25\uff1a{error}',
      marketplaceShowingFirst: '\u5171 {total} \u9879\uff0c\u663e\u793a\u524d {count} \u9879'
    });
  }
  const state = { policy: null, capabilities: [], requests: [], externalResults: [], billing: null, workflowReviews: [], workflowReviewDetail: null, rejectingWorkflowReviewId: '', workflowReviewBusy: {} };
  function mp(k, v) { return typeof tr === 'function' ? tr(k, v) : k; }
  function esc(v) { return typeof escapeHtml === 'function' ? escapeHtml(String(v == null ? '' : v)) : String(v == null ? '' : v); }
  function jsArg(v) { return String(v == null ? '' : v).replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\r/g, '\\r').replace(/\n/g, '\\n').replace(/</g, '\\x3c').replace(/>/g, '\\x3e').replace(/&/g, '\\x26'); }
  function el(id) { return document.getElementById(id); }
  function bool(v, fallback) { return typeof v === 'boolean' ? v : fallback; }
  function jsonText(text, fallback) { const raw = String(text || '').trim(); return raw ? JSON.parse(raw) : fallback; }
  function pretty(v) { return JSON.stringify(v, null, 2); }
  function metadataOf(item) { if (!item) return {}; if (item.metadata && typeof item.metadata === 'object') return item.metadata; try { return item.metadata_json ? JSON.parse(item.metadata_json) : {}; } catch (_) { return {}; } }
  function isMaclawAppCapability(item, metadata) { metadata = metadata || metadataOf(item); return !!(metadata.is_maclaw_app || item.is_maclaw_app || metadata.product_kind === 'maclaw_app_skill' || item.product_kind === 'maclaw_app_skill' || metadata.maclaw_app_id || metadata.maclaw_app_name || metadata.x_maclaw_apps_preview); }
  function maclawAppEvidenceValue(metadata, keys) { for (var i = 0; i < keys.length; i += 1) { var value = metadata && metadata[keys[i]]; if (value !== undefined && value !== null && value !== '') return value; } return ''; }
  function maclawAppEvidenceObject(metadata, keys) { var value = maclawAppEvidenceValue(metadata, keys); return value && typeof value === 'object' ? value : {}; }
  function maclawAppEvidenceList(value) { return Array.isArray(value) ? value : (value && typeof value === 'object' ? [value] : []); }
  function maclawAppEvidenceFirst() { for (var i = 0; i < arguments.length; i += 1) { var value = arguments[i]; if (value !== undefined && value !== null && value !== '') return value; } return ''; }
  function maclawAppPreview(metadata) { var preview = metadata && metadata.x_maclaw_apps_preview; return Array.isArray(preview) && preview.length && preview[0] && typeof preview[0] === 'object' ? preview[0] : {}; }
  function maclawAppEvidenceSummary(item, metadata) {
    metadata = metadata || metadataOf(item);
    if (!isMaclawAppCapability(item, metadata)) return '';
    var preview = maclawAppPreview(metadata);
    var appID = maclawAppEvidenceValue(metadata, ['maclaw_app_id', 'app_id']) || preview.id || '';
    var appName = maclawAppEvidenceValue(metadata, ['maclaw_app_name', 'app_name']) || preview.name || firstName(item);
    var reviewEvidence = maclawAppEvidenceObject(metadata, ['review_evidence', 'reviewEvidence', 'maclaw_app_review_evidence']);
    var reviewAppEvidence = appID && reviewEvidence[appID] && typeof reviewEvidence[appID] === 'object' ? reviewEvidence[appID] : (appName && reviewEvidence[appName] && typeof reviewEvidence[appName] === 'object' ? reviewEvidence[appName] : reviewEvidence);
    var layout = maclawAppEvidenceObject(metadata, ['workspace_layout', 'workspaceLayout']);
    var result = maclawAppEvidenceObject(metadata, ['result_contract', 'resultContract']);
    var evidence = maclawAppEvidenceObject(metadata, ['test_evidence', 'testEvidence', 'maclaw_app_test_evidence']);
    var layoutStudio = layout.studio && typeof layout.studio === 'object' ? layout.studio : {};
    var layoutSaved = maclawAppEvidenceFirst(metadata.workspace_layout_studio_saved_in_manifest, metadata.workspace_saved_in_manifest, layout.studio_saved_in_manifest, layoutStudio.savedInManifest, layoutStudio.saved_in_manifest);
    var layoutUpdatedBy = maclawAppEvidenceFirst(metadata.workspace_layout_studio_updated_by, metadata.workspace_updated_by, layout.studio_updated_by, layoutStudio.updatedBy, layoutStudio.updated_by);
    var layoutText = [layout.entry, layout.template, layout.density, layoutSaved === true ? 'saved' : '', layoutUpdatedBy].filter(Boolean).join(' / ');
    var resultTypes = maclawAppEvidenceFirst(reviewAppEvidence.result_contract_type_count, Array.isArray(result.types) ? result.types.length : 0);
    var resultPrimary = maclawAppEvidenceFirst(reviewAppEvidence.result_contract_primary, result.primary, result.primary_result, metadata.result_contract_primary, metadata.maclaw_app_primary_result, evidence.primary_result);
    var resultText = [resultPrimary || '', resultTypes ? resultTypes + ' types' : ''].filter(Boolean).join(' / ');
    var testText = [evidence.runId || evidence.run_id || reviewAppEvidence.run_id || '', reviewAppEvidence.test_protocol_fingerprint || evidence.testProtocolFingerprint || evidence.test_protocol_fingerprint || metadata.test_evidence_test_protocol_fingerprint || evidence.definition_fingerprint || ''].filter(Boolean).join(' / ');
    var coverageText = [maclawAppEvidenceFirst(reviewAppEvidence.result_coverage_primary, metadata.test_evidence_result_coverage_primary), maclawAppEvidenceFirst(reviewAppEvidence.result_coverage_ok, metadata.test_evidence_result_coverage_ok) === true ? 'ok' : '', maclawAppEvidenceFirst(reviewAppEvidence.result_coverage_covered_count, metadata.test_evidence_result_coverage_covered_count) !== '' ? 'covered ' + maclawAppEvidenceFirst(reviewAppEvidence.result_coverage_covered_count, metadata.test_evidence_result_coverage_covered_count) : '', reviewAppEvidence.result_coverage_missing_count !== undefined && reviewAppEvidence.result_coverage_missing_count !== null ? 'missing ' + reviewAppEvidence.result_coverage_missing_count : ''].filter(Boolean).join(' / ');
    var outputText = [maclawAppEvidenceFirst(reviewAppEvidence.output_count, evidence.output_count, metadata.test_evidence_output_count) !== '' ? maclawAppEvidenceFirst(reviewAppEvidence.output_count, evidence.output_count, metadata.test_evidence_output_count) + ' outputs' : '', maclawAppEvidenceFirst(reviewAppEvidence.artifact_count, evidence.artifact_count, metadata.test_evidence_artifact_count) !== '' ? maclawAppEvidenceFirst(reviewAppEvidence.artifact_count, evidence.artifact_count, metadata.test_evidence_artifact_count) + ' artifacts' : ''].filter(Boolean).join(' / ');
    var deps = maclawAppEvidenceObject(metadata, ['dependency_verification', 'dependencyVerification']);
    var depText = deps.dependency_count || deps.dependencyCount ? 'deps ' + (deps.dependency_count || deps.dependencyCount) : '';
    var approval = evidence.approval_instance && typeof evidence.approval_instance === 'object' ? evidence.approval_instance : (evidence.approvalInstance && typeof evidence.approvalInstance === 'object' ? evidence.approvalInstance : {});
    var progress = maclawAppEvidenceList(maclawAppEvidenceFirst(evidence.progress_instances, evidence.progressInstances, evidence.workflow_progress, evidence.workflowProgress, evidence.approval_progress, evidence.approvalProgress));
    var dataSrv = evidence.datasrv_registration && typeof evidence.datasrv_registration === 'object' ? evidence.datasrv_registration : (metadata.datasrv_registration && typeof metadata.datasrv_registration === 'object' ? metadata.datasrv_registration : {});
    var workflow = evidence.workflow_contract && typeof evidence.workflow_contract === 'object' ? evidence.workflow_contract : maclawAppEvidenceObject(metadata, ['workflow_contract', 'workflowContract']);
    var approvalText = [maclawAppEvidenceFirst(approval.status, approval.approval_status, evidence.approval_status, metadata.test_evidence_approval_status), maclawAppEvidenceFirst(approval.current_node, approval.currentNode, evidence.current_node, metadata.test_evidence_current_node)].filter(Boolean).join(' / ');
    var progressText = maclawAppEvidenceFirst(evidence.progress_count, evidence.progressCount, progress.length ? progress.length : '');
    var dataSrvText = maclawAppEvidenceFirst(dataSrv.status, dataSrv.state, dataSrv.registration_status, evidence.datasrv_registration_status, metadata.datasrv_registration_status);
    var workflowText = maclawAppEvidenceFirst(workflow.version, workflow.schema, workflow.workflowVersion, evidence.workflow_contract_version, metadata.workflow_contract_version);
    var rows = [
      ['App', [appName, appID].filter(Boolean).join(' / ')],
      ['Layout', layoutText],
      ['Result', resultText],
      ['Test', testText],
      ['Coverage', coverageText],
      ['Outputs', outputText],
      ['Approval', approvalText],
      ['Progress', progressText],
      ['Dependencies', depText],
      ['DataSrv', dataSrvText],
      ['Workflow', workflowText]
    ].filter(function(row) { return row[1] !== undefined && row[1] !== null && row[1] !== ''; });
    if (!rows.length) return '';
    return '<div class="item-meta maclaw-app-evidence" style="display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:5px;margin-top:4px;font-size:10px">' + rows.map(function(row) { return '<span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"><strong>' + esc(row[0]) + ':</strong> ' + esc(row[1]) + '</span>'; }).join('') + '</div>';
  }
  function firstID(item) { return item.id || item.capability_id || item.skill_id || item.name || item.key || ''; }
  function firstName(item) { return item.display_name || item.name || item.title || item.id || item.capability_id || item.skill_id || '-'; }
  function pricing(item) { const p = item.pricing || item.price_type || item.billing || item.charge_type || ''; return typeof p === 'string' ? (p || 'free') : (p && (p.type || p.mode || p.pricing)) || 'free'; }
  function priceObject(item) { return item.price && typeof item.price === 'object' ? item.price : (item.pricing && typeof item.pricing === 'object' ? item.pricing : null); }
  function licenseObject(item) { return item.license && typeof item.license === 'object' ? item.license : null; }
  function renderPolicy() {
    const root = el('marketplacePolicyBody'); if (!root) return;
    const p = state.policy || {}, md = p.managed_deployment || {}, rc = p.recommended_capability || {};
    root.innerHTML = '<div class="grid2"><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceEnterpriseOnlyInstall"> ' + esc(mp('marketplaceEnterpriseOnlyInstall')) + '</label><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceEnterpriseOnlySearch"> ' + esc(mp('marketplaceEnterpriseOnlySearch')) + '</label><div><label>' + esc(mp('marketplaceViewMode')) + '</label><select id="marketplaceViewMode"><option value="merged">merged</option><option value="enterprise_first">enterprise_first</option><option value="enterprise_only">enterprise_only</option></select></div><div><label>' + esc(mp('marketplacePreferredUploadTarget')) + '</label><select id="marketplacePreferredUploadTarget"><option value="hubcenter">' + esc(mp('marketplaceUploadHubCenter')) + '</option><option value="enterprise_hub">' + esc(mp('marketplaceUploadEnterpriseHub')) + '</option></select></div><div><label>managed retry minutes</label><input id="marketplaceRetryMinutes" type="number" min="5" step="5"></div><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceManagedEnabled"> managed deployment enabled</label><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceRecommendedEnabled"> recommendations enabled</label></div><div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" onclick="saveMarketplacePolicy()">' + esc(mp('marketplaceSavePolicy')) + '</button></div>';
    el('marketplaceEnterpriseOnlyInstall').checked = bool(p.enterprise_only_install, false); el('marketplaceEnterpriseOnlySearch').checked = bool(p.enterprise_only_search, false); el('marketplaceViewMode').value = p.view_mode || 'merged'; el('marketplacePreferredUploadTarget').value = p.preferred_upload_target || 'hubcenter'; el('marketplaceRetryMinutes').value = String(md.retry_interval_minutes || 60); el('marketplaceManagedEnabled').checked = bool(md.enabled, true); el('marketplaceRecommendedEnabled').checked = bool(rc.enabled, true);
  }
  function renderRequests() {
    const root = el('marketplaceRequestsList'); if (!root) return;
    if (!state.requests.length) { root.innerHTML = '<div class="hint">' + esc(mp('marketplaceNoRequests')) + '</div>'; return; }
    root.innerHTML = state.requests.map(function(item) { const pending = item.status === 'pending_review' || item.status === 'pending' || item.status === 'approved'; const requestId = jsArg(item.id); const actions = pending ? '<button class="btn-primary" type="button" onclick="approveMarketplaceRequest(\'' + requestId + '\')">' + esc(mp('marketplaceApprove')) + '</button><button class="btn-danger" type="button" onclick="rejectMarketplaceRequest(\'' + requestId + '\')">' + esc(mp('marketplaceReject')) + '</button>' : ''; return '<div class="item"><div class="item-head"><div><div class="item-title">' + esc(item.source_capability_key || item.id) + '</div><div class="item-meta">' + esc(item.capability_type) + ' | ' + esc(item.source) + ' | ' + esc(item.request_kind) + ' | ' + esc(item.status) + '</div></div><span class="badge ' + (item.status === 'completed' ? 'ok' : item.status === 'rejected' ? 'danger' : 'warn') + '">' + esc(item.status) + '</span></div><div class="item-meta">' + esc(item.reason || item.created_at || '') + '</div><div class="actions" style="margin-top:8px">' + actions + '</div></div>'; }).join('');
  }
  function renderCapabilities() {
    const root = el('marketplaceCapabilitiesList'); if (!root) return;
    if (!state.capabilities.length) { root.innerHTML = '<div class="hint" style="grid-column:1/-1">' + esc(mp('marketplaceNoCapabilities')) + '</div>'; return; }
    const maxShow = 20;
    const items = state.capabilities.slice(0, maxShow);
    const hasMore = state.capabilities.length > maxShow;
    root.innerHTML = items.map(function(item) {
      var metadata = metadataOf(item);
      var workflowId = item.capability_type === 'approval_workflow' ? (metadata.workflow_id || item.capability_id || '') : '';
      var maclawEvidence = maclawAppEvidenceSummary(item, metadata);
      var workflowAction = workflowId ? '<a class="btn-ghost" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px;display:inline-flex;align-items:center;text-decoration:none" href="/approval_workflow/?workflow_id=' + encodeURIComponent(workflowId) + '">' + esc(mp('workflowReviewOpenDesigner')) + '</a>' : '';
      var itemId = jsArg(item.id);
      var versionKey = jsArg(item.current_version_key || '');
      var mcpAction = item.capability_type === 'mcp' ? '<button class="btn-ghost" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="useCapabilityForMCP(\'' + itemId + '\')">' + esc(mp('marketplaceUseSelected')) + '</button>' : '';
      var maclawReviewAction = isMaclawAppCapability(item, metadata) && (item.status === 'pending_review' || item.status === 'review_failed') ? '<button class="btn-primary" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="approveMaclawAppCapability(\'' + itemId + '\')">' + esc(mp('maclawAppApprove')) + '</button><button class="btn-danger" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="rejectMaclawAppCapability(\'' + itemId + '\')">' + esc(mp('maclawAppReject')) + '</button>' : '';
      return '<div class="item" style="padding:12px 14px;border-radius:14px;gap:6px;min-height:160px;transition:transform .15s ease,box-shadow .15s ease,border-color .15s ease"><div class="item-head" style="margin-bottom:0"><div style="min-width:0"><div class="item-title" style="font-size:13px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(item.display_name || item.capability_id || item.id) + '">' + esc(item.display_name || item.capability_id || item.id) + '</div></div><span class="badge info" style="font-size:9px;padding:3px 7px">' + esc(item.capability_type) + '</span></div><div class="item-meta mono" style="font-size:10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(item.id) + '">' + esc(item.id) + '</div><div class="item-meta" style="font-size:10px">' + esc(item.source || '-') + ' | ' + esc(item.status || '-') + ' | ' + esc(item.current_version_key || '-') + '</div>' + maclawEvidence + '<div class="actions" style="margin-top:auto;padding-top:4px;gap:4px;flex-wrap:wrap">' + maclawReviewAction + '<button class="btn-secondary" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="createMarketplaceDeployment(\'' + itemId + '\',\'' + versionKey + '\')">' + esc(mp('marketplaceMakeRequired')) + '</button><button class="btn-ghost" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="createMarketplaceRecommendation(\'' + itemId + '\',\'' + versionKey + '\')">' + esc(mp('marketplaceMakeRecommended')) + '</button>' + mcpAction + workflowAction + '</div></div>';
    }).join('') + (hasMore ? '<div class="hint" style="grid-column:1/-1;text-align:center;font-size:12px">' + esc(mp('marketplaceShowingFirst', {total: state.capabilities.length, count: maxShow})) + '</div>' : '');
  }
  function renderExternalResults() {
    const root = el('marketplaceSearchResults'); if (!root) return;
    if (!state.externalResults.length) { root.innerHTML = '<div class="hint" style="grid-column:1/-1">' + esc(mp('marketplaceNoResults')) + '</div>'; return; }
    root.innerHTML = state.externalResults.map(function(item, idx) { const type = item.capability_type || el('marketplaceSearchType').value || 'skill'; const p = pricing(item); return '<div class="item" style="padding:12px 14px;border-radius:14px;gap:6px;min-height:140px;transition:transform .15s ease,box-shadow .15s ease,border-color .15s ease"><div class="item-head" style="margin-bottom:0"><div style="min-width:0"><div class="item-title" style="font-size:13px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(firstName(item)) + '">' + esc(firstName(item)) + '</div></div><span class="badge ' + (p === 'free' ? 'ok' : 'warn') + '" style="font-size:9px;padding:3px 7px">' + esc(p) + '</span></div><div class="item-meta mono" style="font-size:10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(firstID(item)) + '</div><div class="item-meta" style="font-size:10px">' + esc(type) + ' | ' + esc(item.source || el('marketplaceSearchSource').value || 'hubcenter') + '</div><div class="desc" style="font-size:11px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;min-height:32px">' + esc(item.description || item.summary || '') + '</div><div class="actions" style="margin-top:auto;padding-top:4px"><button class="btn-primary" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="importMarketplaceResult(' + idx + ')">' + esc(mp('marketplaceImport')) + '</button></div></div>'; }).join('');
  }
  function renderBilling() {
    const root = el('marketplaceBillingBody'); if (!root) return;
    if (!state.billing) { root.innerHTML = '<div class="hint">' + esc(mp('marketplaceBillingDesc')) + '</div>'; return; }
    const a = state.billing.account || {}, list = state.billing.licenses || [];
    const links = [['login', a.login_url], ['billing', a.billing_portal_url], ['renew', a.renewal_url]].filter(function(pair) { return pair[1]; }).map(function(pair) { return '<a class="btn-ghost" href="' + esc(pair[1]) + '" target="_blank" rel="noreferrer">' + esc(pair[0]) + '</a>'; }).join('');
    const accountMeta = [a.status || '-', a.admin_email || a.email || '-', a.customer_id || '-', a.hubcenter || ''].filter(Boolean).join(' | ');
    root.innerHTML = '<div class="item"><div class="item-head"><div><div class="item-title">' + esc(a.hub_id || a.customer_id || '-') + '</div><div class="item-meta">' + esc(accountMeta) + '</div></div><span class="badge ' + (a.status === 'configured' ? 'ok' : 'warn') + '">' + esc(a.status || '-') + '</span></div>' + (links ? '<div class="actions" style="margin-top:8px">' + links + '</div>' : '') + '</div>' + (list.length ? list.map(function(item) { const price = item.pricing && typeof item.pricing === 'object' ? (item.pricing.mode || item.pricing.type || '') : ''; return '<div class="item"><div class="item-title">' + esc(item.capability_id || item.skill_id || item.id || '-') + '</div><div class="item-meta">' + esc(item.capability_type || item.type || '-') + ' | ' + esc(item.status || '-') + ' | ' + esc(price) + ' | ' + esc(item.expires_at || item.created_at || '') + '</div></div>'; }).join('') : '<div class="hint">' + esc(mp('marketplaceNoLicenses')) + '</div>');
  }
  function renderWorkflowReviews() {
    const root = el('marketplaceWorkflowReviewsList'); if (!root) return;
    const detail = el('marketplaceWorkflowReviewDetail'); if (detail && !state.workflowReviewDetail) detail.innerHTML = '';
    if (!state.workflowReviews.length) { root.innerHTML = '<div class="hint">' + esc(mp('workflowReviewsEmpty')) + '</div>'; return; }
    root.innerHTML = state.workflowReviews.map(function(item) {
      const ver = item.version || {}, graph = ver.graph || {}, nodes = Array.isArray(graph.nodes) ? graph.nodes.length : 0, edges = Array.isArray(graph.edges) ? graph.edges.length : 0;
      var busy = !!state.workflowReviewBusy[ver.id];
      var disabled = busy ? ' disabled aria-disabled="true"' : '';
      var previewLink = ver.id ? '<a class="btn-ghost" style="height:28px;padding:0 9px;font-size:12px;border-radius:8px;display:inline-flex;align-items:center;text-decoration:none" href="/approval_workflow/?review_version_id=' + encodeURIComponent(ver.id) + '" target="_blank" rel="noopener">' + esc(mp('workflowReviewOpenDesigner')) + '</a>' : '';
      var versionId = jsArg(ver.id);
      return '<div class="item"><div class="item-head"><div><div class="item-title">' + esc(item.workflow_name || ver.workflow_id || ver.id) + '</div><div class="item-meta">' + esc(ver.version_number || '-') + ' | ' + esc(item.author_id || '-') + ' | ' + esc(mp('workflowReviewGraphSummary', { nodes: nodes, edges: edges })) + '</div></div><span class="badge warn">' + esc(ver.status || 'pending_review') + '</span></div><div class="actions" style="margin-top:8px"><button class="btn-secondary" type="button" onclick="openWorkflowReviewDetail(\'' + versionId + '\')"' + disabled + '>' + esc(mp('workflowReviewOpen')) + '</button>' + previewLink + '<button class="btn-primary" type="button" onclick="approveWorkflowReview(\'' + versionId + '\')"' + disabled + '>' + esc(mp('workflowReviewApprove')) + '</button><button class="btn-danger" type="button" onclick="rejectWorkflowReview(\'' + versionId + '\')"' + disabled + '>' + esc(mp('workflowReviewReject')) + '</button></div></div>';
    }).join('');
  }
  function renderWorkflowReviewDetail() {
    const root = el('marketplaceWorkflowReviewDetail'); if (!root) return;
    const detail = state.workflowReviewDetail;
    if (!detail) { root.innerHTML = ''; return; }
    const graph = detail.graph || {}, nodes = Array.isArray(graph.nodes) ? graph.nodes.length : 0, edges = Array.isArray(graph.edges) ? graph.edges.length : 0;
    const configs = Array.isArray(detail.node_configs) ? detail.node_configs : [];
    var previewLink = detail.version && detail.version.id ? '<a class="btn-ghost" style="height:28px;padding:0 9px;font-size:12px;border-radius:8px;display:inline-flex;align-items:center;text-decoration:none" href="/approval_workflow/?review_version_id=' + encodeURIComponent(detail.version.id) + '" target="_blank" rel="noopener">' + esc(mp('workflowReviewOpenDesigner')) + '</a>' : '';
    root.innerHTML = '<div class="item"><div class="item-head"><div><div class="item-title">' + esc(mp('workflowReviewDetailTitle')) + ': ' + esc(detail.workflow_name || detail.version && detail.version.workflow_id || '-') + '</div><div class="item-meta">' + esc(detail.author_id || '-') + ' | ' + esc(mp('workflowReviewGraphSummary', { nodes: nodes, edges: edges })) + '</div></div><span class="badge info">' + esc(detail.version && detail.version.version_number || '-') + '</span></div><div class="actions" style="margin-top:8px">' + previewLink + '</div><div class="desc" style="margin-top:6px">' + esc(detail.workflow_description || '') + '</div><div class="item-title" style="margin-top:10px">' + esc(mp('workflowReviewNodesTitle')) + '</div><div style="display:grid;gap:6px;margin-top:6px">' + configs.map(function(cfg) { return '<details class="hint" style="background:#fff"><summary><strong>' + esc(cfg.label || cfg.node_id) + '</strong> <span class="item-meta">' + esc(cfg.node_type || '') + '</span></summary><pre style="white-space:pre-wrap;overflow:auto;margin:8px 0 0;font-size:11px">' + esc(JSON.stringify(cfg.config || {}, null, 2)) + '</pre></details>'; }).join('') + '</div></div>';
  }
  function setWorkflowRejectError(message) { var box = el('workflowRejectReasonError'); var input = el('workflowRejectReason'); if (!box || !input) return; box.textContent = message || ''; box.style.display = message ? '' : 'none'; input.setAttribute('aria-invalid', message ? 'true' : 'false'); }
  function workflowRejectReason() { var input = el('workflowRejectReason'); return input ? String(input.value || '').trim() : ''; }
  function validateWorkflowRejectReason() { var reason = workflowRejectReason(); if (reason.length < 10 || reason.length > 2000) { setWorkflowRejectError(mp('workflowReviewRejectReasonInvalid')); return false; } setWorkflowRejectError(''); return true; }
  function rerenderMarketplace() { renderPolicy(); renderRequests(); renderCapabilities(); renderExternalResults(); renderBilling(); renderWorkflowReviews(); renderWorkflowReviewDetail(); }
  async function loadPolicy() { const data = await api('/api/admin/capability-market/policy'); state.policy = data.policy || {}; renderPolicy(); }
  async function loadCapabilities() { const type = el('marketplaceCapabilityType') ? el('marketplaceCapabilityType').value : ''; const data = await api('/api/admin/capabilities' + (type ? '?type=' + encodeURIComponent(type) : '')); state.capabilities = Array.isArray(data.items) ? data.items : []; renderCapabilities(); }
  async function loadRequests() { const status = el('marketplaceRequestStatus') ? el('marketplaceRequestStatus').value : 'pending_review'; const data = await api('/api/admin/capability-market/acquisition-requests' + (status ? '?status=' + encodeURIComponent(status) : '')); state.requests = Array.isArray(data.items) ? data.items : []; renderRequests(); }
  async function loadWorkflowReviewsInternal() { const data = await api('/api/v1/admin/reviews?page=1'); state.workflowReviews = Array.isArray(data.submissions) ? data.submissions : []; renderWorkflowReviews(); }
  async function loadWorkflowReviewsQuietly() { try { await loadWorkflowReviewsInternal(); } catch (_) { state.workflowReviews = []; renderWorkflowReviews(); } }
  global.loadWorkflowReviews = async function() { try { await loadWorkflowReviewsInternal(); } catch (err) { const msg = mp('workflowReviewLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.loadMarketplace = async function() { if (typeof token === 'function' && !token()) return; if (!el('tab-marketplace')) return; try { await Promise.all([loadPolicy(), loadCapabilities(), loadRequests(), loadWorkflowReviewsQuietly()]); renderBilling(); setOutput(mp('marketplaceOutputReady')); } catch (err) { const msg = mp('marketplaceLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.saveMarketplacePolicy = async function() { try { const p = state.policy || {}; p.enterprise_only_install = !!el('marketplaceEnterpriseOnlyInstall').checked; p.enterprise_only_search = !!el('marketplaceEnterpriseOnlySearch').checked; p.view_mode = el('marketplaceViewMode').value || 'merged'; p.preferred_upload_target = el('marketplacePreferredUploadTarget').value || 'hubcenter'; p.managed_deployment = Object.assign({}, p.managed_deployment || {}, { enabled: !!el('marketplaceManagedEnabled').checked, retry_interval_minutes: Math.max(5, Number(el('marketplaceRetryMinutes').value || 60) || 60), reinstall_if_removed: true }); p.recommended_capability = Object.assign({}, p.recommended_capability || {}, { enabled: !!el('marketplaceRecommendedEnabled').checked, allow_user_dismiss: true }); const data = await api('/api/admin/capability-market/policy', { method: 'PUT', body: JSON.stringify({ policy: p }) }); state.policy = data.policy || p; renderPolicy(); setOutput(mp('marketplacePolicySaved')); showToast(mp('marketplacePolicySaved'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.approveMarketplaceRequest = async function(id) { try { await api('/api/admin/capability-market/acquisition-requests/' + encodeURIComponent(id) + '/approve', { method: 'POST', body: JSON.stringify({ approval: { mode: 'admin_approved_online_purchase' } }) }); await Promise.all([loadRequests(), loadCapabilities()]); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.rejectMarketplaceRequest = async function(id) { try { await api('/api/admin/capability-market/acquisition-requests/' + encodeURIComponent(id) + '/reject', { method: 'POST', body: JSON.stringify({ approval: { mode: 'admin_rejected' } }) }); await loadRequests(); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.approveMaclawAppCapability = async function(id) { try { await api('/api/admin/capabilities/maclaw-apps/' + encodeURIComponent(id) + '/approve', { method: 'POST', body: JSON.stringify({ reviewer: 'hub-admin' }) }); await loadCapabilities(); showToast(mp('maclawAppApproved'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.rejectMaclawAppCapability = async function(id) { try { var reason = 'Rejected by Hub admin. Please revise the MaClaw App package and resubmit.'; await api('/api/admin/capabilities/maclaw-apps/' + encodeURIComponent(id) + '/reject', { method: 'POST', body: JSON.stringify({ reviewer: 'hub-admin', reason: reason, review_issues: [{ path: 'app.governance', severity: 'error', message: reason, suggestion: 'Revise the package and submit it again from App Studio.' }] }) }); await loadCapabilities(); showToast(mp('maclawAppRejected'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.openWorkflowReviewDetail = async function(id) { try { const data = await api('/api/v1/admin/reviews/' + encodeURIComponent(id)); state.workflowReviewDetail = data; renderWorkflowReviewDetail(); } catch (err) { const msg = mp('workflowReviewLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.approveWorkflowReview = async function(id) { if (state.workflowReviewBusy[id]) return; state.workflowReviewBusy[id] = true; renderWorkflowReviews(); try { await api('/api/v1/admin/reviews/' + encodeURIComponent(id) + '/approve', { method: 'POST', body: JSON.stringify({}) }); state.workflowReviewDetail = null; await Promise.all([loadWorkflowReviewsInternal(), loadCapabilities()]); renderWorkflowReviewDetail(); showToast(mp('workflowReviewPublished'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } finally { delete state.workflowReviewBusy[id]; renderWorkflowReviews(); } };
  global.rejectWorkflowReview = function(id) { global.openWorkflowRejectDialog(id); };
  global.openWorkflowRejectDialog = function(id) { if (state.workflowReviewBusy[id]) return; var overlay = el('workflowRejectOverlay'); var input = el('workflowRejectReason'); if (!overlay || !input) return; state.rejectingWorkflowReviewId = id || ''; input.value = ''; setWorkflowRejectError(''); overlay.classList.add('show'); setTimeout(function() { input.focus(); }, 0); };
  global.closeWorkflowRejectDialog = function() { var overlay = el('workflowRejectOverlay'); if (!overlay) return; overlay.classList.remove('show'); state.rejectingWorkflowReviewId = ''; setWorkflowRejectError(''); };
  global.submitWorkflowRejectDialog = async function() { var id = state.rejectingWorkflowReviewId; if (!id || state.workflowReviewBusy[id] || !validateWorkflowRejectReason()) return; var reason = workflowRejectReason(); state.workflowReviewBusy[id] = true; renderWorkflowReviews(); try { await api('/api/v1/admin/reviews/' + encodeURIComponent(id) + '/reject', { method: 'POST', body: JSON.stringify({ reason: reason }) }); global.closeWorkflowRejectDialog(); state.workflowReviewDetail = null; await loadWorkflowReviewsInternal(); renderWorkflowReviewDetail(); showToast(mp('workflowReviewRejected'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } finally { delete state.workflowReviewBusy[id]; renderWorkflowReviews(); } };
  global.createMarketplaceDeployment = async function(id, versionKey) { try { await api('/api/admin/capability-market/managed-deployments', { method: 'POST', body: JSON.stringify({ capability_ref: id, capability_version_key: versionKey || '', deployment_policy: 'required', reinstall_if_removed: true, retry_interval_minutes: 60, enabled: true }) }); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.createMarketplaceRecommendation = async function(id, versionKey) { try { await api('/api/admin/capability-market/recommendations', { method: 'POST', body: JSON.stringify({ capability_ref: id, capability_version_key: versionKey || '', recommendation_reason: 'admin_recommended', allow_user_dismiss: true, enabled: true }) }); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.searchMarketplaceExternal = async function() { try { const params = new URLSearchParams({ type: el('marketplaceSearchType').value || 'skill' }); var src = el('marketplaceSearchSource').value || ''; if (src) params.set('source', src); if (el('marketplaceSearchQuery').value.trim()) params.set('q', el('marketplaceSearchQuery').value.trim()); const data = await api('/api/admin/capabilities/external-search?' + params.toString()); state.externalResults = Array.isArray(data.items) ? data.items : []; renderExternalResults(); } catch (err) { const msg = mp('marketplaceSearchFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.importMarketplaceResult = async function(index) { const item = state.externalResults[index]; if (!item) return; try { const payload = { capability_id: firstID(item), capability_type: item.capability_type || el('marketplaceSearchType').value || 'skill', display_name: firstName(item), description: item.description || item.summary || '', version: item.version_key || item.version || '', source: item.source || el('marketplaceSearchSource').value || 'hubcenter', pricing: pricing(item), price: priceObject(item), license: licenseObject(item), metadata: item, user_reason: 'admin_marketplace_import' }; const data = await api('/api/admin/capabilities/import-intent', { method: 'POST', body: JSON.stringify(payload) }); await Promise.all([loadRequests(), loadCapabilities()]); setOutput(pretty(data)); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceImportFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  // Collect MCP definition from the editor form (single source of truth)
  function collectMCPFromForm() {
    var mcpType = el('marketplaceMCPTypeSelect') ? el('marketplaceMCPTypeSelect').value : 'remote';
    if (mcpType === 'local') {
      var cmd = (el('marketplaceMCPCommand').value || '').trim();
      if (!cmd) throw new Error('command is required for local MCP');
      var argsRaw = (el('marketplaceMCPArgs').value || '').trim();
      var envRaw = (el('marketplaceMCPEnv').value || '').trim();
      var args = argsRaw ? JSON.parse(argsRaw) : [];
      var env = envRaw ? JSON.parse(envRaw) : {};
      return { transport: 'stdio', command: cmd, args: Array.isArray(args) ? args : [], env: env };
    }
    return jsonText(el('marketplaceMCPJson').value, {});
  }
  // Build test payload from collected MCP object
  function buildTestPayload(mcp) {
    if (mcp.transport === 'stdio') return { transport: 'stdio', command: mcp.command, args: mcp.args || [], env: mcp.env || {} };
    var endpoint = mcp.endpoint_url || mcp.url || '';
    if (!endpoint) throw new Error('endpoint_url is required in MCP JSON');
    var authType = mcp.auth_type || 'none', authSecret = mcp.auth_secret || '';
    var headers = mcp.headers && typeof mcp.headers === 'object' ? mcp.headers : {};
    if (!authSecret && headers['Authorization']) {
      var authHeader = headers['Authorization'];
      if (authHeader.toLowerCase().indexOf('bearer ') === 0) { authType = 'bearer'; authSecret = authHeader.slice(7); }
      else { authType = 'api_key'; authSecret = authHeader; }
    }
    return { endpoint_url: endpoint, auth_type: authType, auth_secret: authSecret, headers: headers };
  }
  // Render test result (shared by both modes)
  function renderTestResult(resultDiv, data) {
    if (data.success) {
      var tools = Array.isArray(data.tools) ? data.tools : [];
      var toolList = tools.length ? '<div style="margin-top:6px;max-height:150px;overflow-y:auto;font-size:0.82em">' + tools.map(function(t) { return '<div style="padding:2px 0"><strong>' + esc(t.name) + '</strong>' + (t.description ? ' <span style="opacity:0.7">' + esc(t.description.length > 80 ? t.description.slice(0, 80) + '...' : t.description) + '</span>' : '') + '</div>'; }).join('') + '</div>' : '';
      resultDiv.innerHTML = '<div class="badge ok" style="display:inline-block">' + esc(mp('marketplaceTestMCPSuccess', { count: tools.length })) + (data.latency_ms != null ? ' (' + data.latency_ms + 'ms)' : '') + '</div>' + toolList;
    } else {
      resultDiv.innerHTML = '<div class="badge danger">' + esc(mp('marketplaceTestMCPFailed', { error: data.message || 'unknown error' })) + '</div>';
    }
  }
  global.saveMarketplaceMCP = async function() { try { var mcp = collectMCPFromForm(); var secrets = jsonText(el('marketplaceMCPSecrets').value, []); var pricingVal = jsonText(el('marketplaceMCPPricing') ? el('marketplaceMCPPricing').value : '', null), license = jsonText(el('marketplaceMCPLicense') ? el('marketplaceMCPLicense').value : '', null); var capId = (el('marketplaceMCPId').value || '').trim() || mcp.id || mcp.name || ''; if (!capId) { showToast('Capability ID is required', 'error'); return; } var payload = { publisher: el('marketplaceMCPPublisher').value || 'enterprise', capability_id: capId, display_name: el('marketplaceMCPName').value || mcp.name || mcp.id || '', version: el('marketplaceMCPVersion').value || '1.0.0', mcp: mcp, secret_requirements: Array.isArray(secrets) ? secrets : [], pricing: pricingVal, license: license }; var data = await api('/api/admin/capability-market/mcp', { method: 'POST', body: JSON.stringify(payload) }); await loadCapabilities(); setOutput(pretty(data)); showToast(mp('marketplaceMcpSaved'), 'success'); global.closeMCPEditorDialog(); } catch (err) { var msg = mp(err instanceof SyntaxError ? 'marketplaceInvalidJson' : 'marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.useCapabilityForMCP = function(id) { var item = state.capabilities.find(function(cap) { return cap.id === id; }); if (!item) return; var metadata = metadataOf(item); el('marketplaceMCPId').value = item.capability_id || item.id || ''; el('marketplaceMCPName').value = item.display_name || item.capability_id || ''; el('marketplaceMCPVersion').value = item.current_version_key || '1.0.0'; var mcp = item.mcp || metadata.mcp || null; if (mcp && mcp.transport === 'stdio') { el('marketplaceMCPTypeSelect').value = 'local'; global.switchMCPEditorType('local'); el('marketplaceMCPCommand').value = mcp.command || ''; el('marketplaceMCPArgs').value = Array.isArray(mcp.args) ? JSON.stringify(mcp.args) : '[]'; el('marketplaceMCPEnv').value = mcp.env && typeof mcp.env === 'object' ? JSON.stringify(mcp.env, null, 2) : '{}'; } else { el('marketplaceMCPTypeSelect').value = 'remote'; global.switchMCPEditorType('remote'); if (mcp) el('marketplaceMCPJson').value = JSON.stringify(mcp, null, 2); } global.openMCPEditorDialog(); };
  global.loadMarketplaceBilling = async function() { try { const account = await api('/api/admin/billing/customer-account'); const licensesData = await api('/api/admin/billing/licenses'); state.billing = { account: account, licenses: Array.isArray(licensesData.items) ? licensesData.items : (Array.isArray(licensesData.licenses) ? licensesData.licenses : []) }; renderBilling(); } catch (err) { const msg = mp('marketplaceLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') global.AdminTabRegistry.registerTab({ id: 'marketplace', title: function() { return mp('marketplaceTabTitle'); }, subtitle: function() { return mp('marketplaceTabSubtitle'); }, onOpen: function() { global.loadMarketplace(); } });
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') global.AdminTabRegistry.onLanguageChange(function() { rerenderMarketplace(); });
  global.addEventListener('keydown', function(event) { if (event.key === 'Enter' && event.target && event.target.id === 'marketplaceSearchQuery') global.searchMarketplaceExternal(); });
  // Sub-tab switching
  global.switchMarketplaceSubtab = function(tab) {
    var marketPanel = el('marketplace-subtab-market');
    var workflowsPanel = el('marketplace-subtab-workflows');
    var settingsPanel = el('marketplace-subtab-settings');
    var marketBtn = el('subtab-market');
    var workflowsBtn = el('subtab-workflows');
    var settingsBtn = el('subtab-settings');
    if (!marketPanel || !settingsPanel || !workflowsPanel) return;
    if (tab === 'market') {
      marketPanel.style.display = ''; workflowsPanel.style.display = 'none'; settingsPanel.style.display = 'none';
      marketBtn.classList.add('active'); workflowsBtn.classList.remove('active'); settingsBtn.classList.remove('active');
    } else if (tab === 'workflows') {
      marketPanel.style.display = 'none'; workflowsPanel.style.display = ''; settingsPanel.style.display = 'none';
      marketBtn.classList.remove('active'); workflowsBtn.classList.add('active'); settingsBtn.classList.remove('active');
      global.loadWorkflowReviews();
    } else {
      marketPanel.style.display = 'none'; workflowsPanel.style.display = 'none'; settingsPanel.style.display = '';
      marketBtn.classList.remove('active'); workflowsBtn.classList.remove('active'); settingsBtn.classList.add('active');
    }
  };
  // Test MCP connection from the MCP JSON editor
  global.testMarketplaceMCP = async function() {
    var resultDiv = el('marketplaceMCPTestResult'); if (!resultDiv) return;
    var mcp, payload;
    try { mcp = collectMCPFromForm(); payload = buildTestPayload(mcp); } catch (err) { resultDiv.innerHTML = '<div class="badge danger">' + esc(err instanceof SyntaxError ? mp('marketplaceInvalidJson', { error: err.message }) : err.message) + '</div>'; return; }
    resultDiv.innerHTML = '<div class="badge warn">' + esc(mp('marketplaceTestMCPTesting')) + '</div>';
    try {
      var data = await api('/api/admin/capability-market/mcp/test', { method: 'POST', body: JSON.stringify(payload) });
      renderTestResult(resultDiv, data);
    } catch (err) {
      resultDiv.innerHTML = '<div class="badge danger">' + esc(mp('marketplaceTestMCPFailed', { error: err.message })) + '</div>';
    }
  };
  // MCP Editor Dialog open/close
  global.openMCPEditorDialog = function(resetForm) {
    var overlay = el('mcpEditorOverlay'); if (!overlay) return;
    if (resetForm) {
      el('marketplaceMCPId').value = ''; el('marketplaceMCPName').value = ''; el('marketplaceMCPPublisher').value = 'enterprise'; el('marketplaceMCPVersion').value = '1.0.0';
      el('marketplaceMCPTypeSelect').value = 'remote'; global.switchMCPEditorType('remote');
      el('marketplaceMCPJson').value = '{"id":"","name":"","endpoint_url":"https://","auth_type":"none","headers":{}}';
      el('marketplaceMCPCommand').value = ''; el('marketplaceMCPArgs').value = '[]'; el('marketplaceMCPEnv').value = '{}';
      el('marketplaceMCPSecrets').value = '[]'; el('marketplaceMCPPricing').value = '{"mode":"free"}'; el('marketplaceMCPLicense').value = '{}';
      var resultDiv = el('marketplaceMCPTestResult'); if (resultDiv) resultDiv.innerHTML = '';
    }
    overlay.style.display = 'flex';
  };
  global.closeMCPEditorDialog = function() {
    var overlay = el('mcpEditorOverlay'); if (!overlay) return;
    overlay.style.display = 'none';
  };
  var workflowRejectReasonInput = el('workflowRejectReason');
  if (workflowRejectReasonInput) workflowRejectReasonInput.addEventListener('input', function() { if (workflowRejectReason().length >= 10) setWorkflowRejectError(''); });
  global.addEventListener('keydown', function(event) { if (event.key === 'Escape') { var rejectOverlay = el('workflowRejectOverlay'); if (rejectOverlay && rejectOverlay.classList.contains('show')) { global.closeWorkflowRejectDialog(); event.stopPropagation(); return; } var overlay = el('mcpEditorOverlay'); if (overlay && overlay.style.display === 'flex') { global.closeMCPEditorDialog(); event.stopPropagation(); } } });
  // MCP Editor type switching (remote vs local)
  global.switchMCPEditorType = function(type) {
    var remoteFields = el('mcpEditorRemoteFields');
    var localFields = el('mcpEditorLocalFields');
    if (!remoteFields || !localFields) return;
    if (type === 'local') {
      remoteFields.style.display = 'none';
      localFields.style.display = '';
    } else {
      remoteFields.style.display = '';
      localFields.style.display = 'none';
    }
  };
})(window);
