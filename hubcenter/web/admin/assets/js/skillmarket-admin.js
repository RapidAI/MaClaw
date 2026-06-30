// Skill market review, purchases, trial, and upload auth configuration.
// SkillMarket Admin
const SM_ADMIN_TEXT = {
  en: { reviewTitle:"Capability Review", reviewDesc:"Approve or reject pending capabilities.", refresh:"Refresh", approve:"Approve", reject:"Reject", approved:"Approved.", rejected:"Rejected.", reviewEmpty:"No capabilities pending review.", purchaseTitle:"Purchase Records", purchaseDesc:"Review active orders, refund history, and buyer-side entitlement activity.", purchaseFilter:"Filter by buyer email or capability_id", search:"Search", refund:"Refund", refunded:"Refunded", refundReason:"Refund reason:", refundOk:"Refund successful.", trialTitle:"Trial Configuration", trialDays:"Trial Days", threshold:"Auto-publish Threshold", maxUploads:"Max Uploads/Hour", maxUploadsHint:"0 = use tier default", save:"Save", configSaved:"Configuration saved.", colName:"Name", colVersion:"Version", colAuthor:"Author", colUploaded:"Uploaded", colRating:"Rating", colRatings:"Ratings", colActions:"Actions", colBuyer:"Buyer", colSkill:"Capability", colAmount:"Amount", colType:"Type", colStatus:"Status", purchaseEmpty:"No purchase records.", trialDesc:"Capability Market operational defaults", trialDaysHint:"Days granted to a trial before it expires.", thresholdHint:"Minimum signal required for automatic publishing.", reviewKicker:"Review Queue", purchaseKicker:"Commerce Ledger", configKicker:"Governance Defaults", uploadAuthKicker:"Security", uploadAuthTitle:"Upload Authentication Mode", uploadAuthDesc:"Controls how capability uploads are authenticated. Default: both (token preferred, email fallback).", uploadAuthMode:"Auth Mode", uploadAuthModeHint:"both = token + email fallback, token = strict token only, email = legacy email only", uploadAuthBoth:"Both (token preferred, email fallback)", uploadAuthToken:"Token only (strict)", uploadAuthEmail:"Email only (legacy)", uploadAuthSaved:"Upload authentication mode saved: {mode}", uploadAuthStatusSaved:"Saved: {description}", uploadAuthStatusError:"Error: {error}", items:"{count} items", status:"Status", lifecycle:"Lifecycle", active:"Active", pendingReview:"Pending Review", trial:"Trial", refundedStatus:"Refunded", canceled:"Canceled", expired:"Expired", typeTrial:"Trial", typeSubscription:"Subscription", typeOneTime:"One-time", configLoadFailed:"Load trial configuration failed: {error}", configInvalid:"Trial days and auto-publish threshold must be greater than 0. Max uploads/hour must be 0 or greater.", saving:"Saving...", loading:"Loading...", reviewLoadFailed:"Load review queue failed: {error}", purchaseLoadFailed:"Load purchase records failed: {error}", page:"Page {current} / {total}" },
  zh: { reviewTitle:"\u80fd\u529b\u5ba1\u6838", reviewDesc:"\u5ba1\u6279\u5f85\u5ba1\u6838\u7684\u80fd\u529b\u3002", refresh:"\u5237\u65b0", approve:"\u6279\u51c6", reject:"\u62d2\u7edd", approved:"\u5df2\u6279\u51c6\u3002", rejected:"\u5df2\u62d2\u7edd\u3002", reviewEmpty:"\u6682\u65e0\u5f85\u5ba1\u6838\u7684\u80fd\u529b\u3002", purchaseTitle:"\u8d2d\u4e70\u8bb0\u5f55", purchaseFilter:"\u6309\u4e70\u5bb6\u90ae\u7bb1\u6216\u80fd\u529b ID \u8fdb\u884c\u7b5b\u9009", search:"\u67e5\u8be2", refund:"\u9000\u6b3e", refunded:"\u5df2\u9000\u6b3e", refundReason:"\u9000\u6b3e\u539f\u56e0\uff1a", refundOk:"\u9000\u6b3e\u6210\u529f\u3002", trialTitle:"\u80fd\u529b\u5e02\u573a\u53c2\u6570\u8bbe\u7f6e", trialDays:"\u8bd5\u7528\u5929\u6570", threshold:"\u81ea\u52a8\u4e0a\u67b6\u9608\u503c", maxUploads:"\u6bcf\u5c0f\u65f6\u4e0a\u4f20\u9650\u5236", maxUploadsHint:"0 = \u4f7f\u7528\u5f53\u524d\u7b49\u7ea7\u7684\u9ed8\u8ba4\u503c", save:"\u4fdd\u5b58", configSaved:"\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002", colName:"\u540d\u79f0", colVersion:"\u7248\u672c", colAuthor:"\u4f5c\u8005", colUploaded:"\u4e0a\u4f20\u65f6\u95f4", colRating:"\u8bc4\u5206", colRatings:"\u8bc4\u4ef7\u6570", colActions:"\u64cd\u4f5c", colBuyer:"\u8d2d\u4e70\u8005", colSkill:"\u80fd\u529b", colAmount:"\u91d1\u989d", colType:"\u7c7b\u578b", colStatus:"\u72b6\u6001", purchaseEmpty:"\u6682\u65e0\u8d2d\u4e70\u8bb0\u5f55\u3002", trialDesc:"\u80fd\u529b\u5e02\u573a\u7684\u8fd0\u884c\u9ed8\u8ba4\u53c2\u6570", trialDaysHint:"\u8bd5\u7528\u80fd\u529b\u5230\u671f\u524d\u53ef\u4f7f\u7528\u7684\u5929\u6570\u3002", thresholdHint:"\u8fbe\u5230\u8be5\u9608\u503c\u540e\u53ef\u81ea\u52a8\u4e0a\u67b6\u3002", reviewKicker:"\u5ba1\u6838\u961f\u5217", purchaseKicker:"\u4ea4\u6613\u53f0\u8d26\u660e\u7ec6", configKicker:"\u9ed8\u8ba4\u89c4\u5219", uploadAuthKicker:"\u5b89\u5168", uploadAuthTitle:"\u4e0a\u4f20\u8ba4\u8bc1\u65b9\u5f0f", uploadAuthDesc:"\u63a7\u5236\u80fd\u529b\u4e0a\u4f20\u65f6\u7684\u8ba4\u8bc1\u65b9\u5f0f\u3002\u9ed8\u8ba4\uff1a\u53cc\u6a21\u5f0f\uff08\u4f18\u5148\u4f7f\u7528\u4ee4\u724c\uff0c\u90ae\u7bb1\u515c\u5e95\uff09\u3002", uploadAuthMode:"\u8ba4\u8bc1\u6a21\u5f0f", uploadAuthModeHint:"\u53cc\u6a21\u5f0f = \u4ee4\u724c + \u90ae\u7bb1\u515c\u5e95\uff1b\u4ee4\u724c = \u4ec5\u5141\u8bb8\u4ee4\u724c\uff1b\u90ae\u7bb1 = \u4ec5\u4f7f\u7528\u65e7\u7248\u90ae\u7bb1\u8ba4\u8bc1", uploadAuthBoth:"\u53cc\u6a21\u5f0f\uff08\u4f18\u5148\u4ee4\u724c\uff0c\u90ae\u7bb1\u515c\u5e95\uff09", uploadAuthToken:"\u4ec5\u4ee4\u724c\uff08\u4e25\u683c\u6a21\u5f0f\uff09", uploadAuthEmail:"\u4ec5\u90ae\u7bb1\uff08\u65e7\u7248\u517c\u5bb9\uff09", uploadAuthSaved:"\u4e0a\u4f20\u8ba4\u8bc1\u65b9\u5f0f\u5df2\u4fdd\u5b58\uff1a{mode}", uploadAuthStatusSaved:"\u5df2\u4fdd\u5b58\uff1a{description}", uploadAuthStatusError:"\u9519\u8bef\uff1a{error}", items:"{count} \u9879", status:"\u72b6\u6001", lifecycle:"\u751f\u547d\u5468\u671f", active:"\u6709\u6548", pendingReview:"\u5f85\u5ba1\u6838", trial:"\u8bd5\u7528", refundedStatus:"\u5df2\u9000\u6b3e", canceled:"\u5df2\u53d6\u6d88", expired:"\u5df2\u8fc7\u671f", typeTrial:"\u8bd5\u7528", typeSubscription:"\u8ba2\u9605", typeOneTime:"\u5355\u6b21\u8d2d\u4e70", configLoadFailed:"\u52a0\u8f7d\u8bd5\u7528\u53c2\u6570\u5931\u8d25\uff1a{error}", configInvalid:"\u8bd5\u7528\u5929\u6570\u548c\u81ea\u52a8\u4e0a\u67b6\u9608\u503c\u5fc5\u987b\u5927\u4e8e 0\uff0c\u6bcf\u5c0f\u65f6\u4e0a\u4f20\u9650\u5236\u5fc5\u987b\u5927\u4e8e\u7b49\u4e8e 0\u3002", saving:"\u4fdd\u5b58\u4e2d...", loading:"\u52a0\u8f7d\u4e2d...", reviewLoadFailed:"\u52a0\u8f7d\u5ba1\u6838\u961f\u5217\u5931\u8d25\uff1a{error}", purchaseLoadFailed:"\u52a0\u8f7d\u8d2d\u4e70\u8bb0\u5f55\u5931\u8d25\uff1a{error}", purchaseDesc:"\u67e5\u770b\u6709\u6548\u8ba2\u5355\u3001\u9000\u6b3e\u8bb0\u5f55\u4ee5\u53ca\u4e70\u65b9\u4fa7\u7684\u6743\u76ca\u6d41\u8f6c\u3002", page:"\u7b2c {current} / {total} \u9875" }
};
const smtr = (key, vars={}) => ((SM_ADMIN_TEXT[currentLang] || SM_ADMIN_TEXT.en)[key] || SM_ADMIN_TEXT.en[key] || key).replace(/\{(\w+)\}/g, (_, n) => vars[n] ?? '');
const smJsArg = value => JSON.stringify(String(value ?? ''));
let smReviewInFlight = null;
let smPurchasesInFlight = null;
let smPurchasesInFlightKey = '';
let smTrialConfigInFlight = null;
let smUploadAuthConfigInFlight = null;
function smStatusText(value) {
  const key = String(value || '').trim().toLowerCase();
  const map = { active:'active', pending_review:'pendingReview', pending:'pendingReview', trial:'trial', refunded:'refundedStatus', canceled:'canceled', cancelled:'canceled', expired:'expired' };
  return map[key] ? smtr(map[key]) : (value || '-');
}
function smPurchaseTypeText(value) {
  const key = String(value || '').trim().toLowerCase();
  const map = { trial:'typeTrial', subscription:'typeSubscription', one_time:'typeOneTime', onetime:'typeOneTime' };
  return map[key] ? smtr(map[key]) : (value || '-');
}
function smUploadAuthModeText(value) {
  const key = String(value || '').trim().toLowerCase();
  const map = { both:'uploadAuthBoth', token:'uploadAuthToken', email:'uploadAuthEmail' };
  return map[key] ? smtr(map[key]) : (value || '-');
}
function smAdminLabel(key) {
  const labels = {
    en: { appSkill:'MaClaw App', appPreview:'App Preview', appCategory:'Category', inputMode:'Input', outputModes:'Outputs', artifactContract:'Artifact Contract', artifactRequired:'Required', presentation:'Presentation', permissions:'Permissions', requiredEnv:'Required Env', requiresGUI:'GUI', securityLabels:'Security', testEvidence:'Test Evidence', manifestPreview:'Manifest Preview', runID:'Run ID', verifiedAt:'Verified', artifact:'Artifact', definitionHash:'Definition SHA256', reviewEvidence:'Review Evidence', approval:'Approval', progress:'Progress', dependencies:'Dependencies', workspaceLayout:'Layout', dataSrv:'DataSrv', workflowContract:'Workflow', resultContract:'Result', testProtocol:'Test', resultCoverage:'Coverage', outputs:'Outputs', yes:'Yes', no:'No', none:'None' },
    zh: { appSkill:'MaClaw App', appPreview:'App 预览', appCategory:'分类', inputMode:'输入', outputModes:'输出', artifactContract:'产物契约', artifactRequired:'必须产出', presentation:'呈现方式', permissions:'权限', requiredEnv:'环境变量', requiresGUI:'GUI', securityLabels:'安全标签', testEvidence:'测试证据', manifestPreview:'Manifest 预览', runID:'运行 ID', verifiedAt:'验证时间', artifact:'产物', definitionHash:'描述文件 SHA256', reviewEvidence:'审核证据', approval:'审批', progress:'进度', dependencies:'依赖', workspaceLayout:'布局', dataSrv:'DataSrv', workflowContract:'工作流', resultContract:'结果', testProtocol:'测试', resultCoverage:'覆盖', outputs:'输出', yes:'是', no:'否', none:'无' }
  };
  return (labels[currentLang] || labels.en)[key] || labels.en[key] || key;
}
function smValueList(values) {
  return (Array.isArray(values) ? values : []).map(v => String(v || '').trim()).filter(Boolean);
}
function smReviewKV(label, value) {
  const shown = value === undefined || value === null || value === '' ? smAdminLabel('none') : String(value);
  return `<div class="item-meta"><strong>${escapeHtml(label)}:</strong> ${escapeHtml(shown)}</div>`;
}
function smEvidenceObject(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
function smEvidenceList(value) {
  return Array.isArray(value) ? value : (value && typeof value === 'object' ? [value] : []);
}
function smEvidenceValue() {
  for (let i = 0; i < arguments.length; i += 1) {
    const value = arguments[i];
    if (value !== undefined && value !== null && value !== '') return value;
  }
  return '';
}
function smEvidenceState(value) {
  const normalized = String(value || '').trim().toLowerCase();
  if (['approved', 'passed', 'pass', 'ready', 'ok', 'success', 'synced', 'complete', 'completed'].includes(normalized)) return 'ready';
  if (['rejected', 'failed', 'error', 'blocked', 'missing', 'deny', 'denied'].includes(normalized)) return 'failed';
  if (['pending', 'running', 'submitted', 'partial', 'in_progress', 'in-progress', 'attention', 'requires_input', 'requires-input'].includes(normalized)) return 'partial';
  return 'skipped';
}
function smEvidenceChip(label, value, state) {
  const shown = value === undefined || value === null || value === '' ? smAdminLabel('none') : String(value);
  return `<span class="sm-review-evidence-chip" data-state="${escapeHtml(state || smEvidenceState(shown))}"><strong>${escapeHtml(label)}</strong><em>${escapeHtml(shown)}</em></span>`;
}
function smRenderEnterpriseReviewEvidence(s, evidence) {
  evidence = smEvidenceObject(evidence);
  const approval = smEvidenceObject(smEvidenceValue(evidence.approval_instance, evidence.approvalInstance, evidence.approval));
  const progress = smEvidenceList(smEvidenceValue(evidence.progress_instances, evidence.progressInstances, evidence.workflow_progress, evidence.workflowProgress, evidence.approval_progress, evidence.approvalProgress));
  const dependency = smEvidenceObject(smEvidenceValue(evidence.dependency_verification, evidence.dependencyVerification, s.dependency_verification, s.dependencyVerification));
  const layout = smEvidenceObject(smEvidenceValue(evidence.workspace_layout, evidence.workspaceLayout, s.workspace_layout, s.workspaceLayout));
  const dataSrv = smEvidenceObject(smEvidenceValue(evidence.datasrv_registration, evidence.dataSrvRegistration, evidence.datasrvRegistration, s.datasrv_registration, s.dataSrvRegistration));
  const workflow = smEvidenceObject(smEvidenceValue(evidence.workflow_contract, evidence.workflowContract, s.workflow_contract, s.workflowContract));
  const resultContract = smEvidenceObject(smEvidenceValue(evidence.result_contract, evidence.resultContract, s.result_contract, s.resultContract));
  const resultCoverage = smEvidenceObject(smEvidenceValue(evidence.result_coverage, evidence.resultCoverage, s.test_evidence_result_coverage));
  const approvalStatus = smEvidenceValue(approval.status, approval.approval_status, evidence.approval_status, s.test_evidence_approval_status);
  const approvalNode = smEvidenceValue(approval.current_node, approval.currentNode, evidence.current_node, s.test_evidence_current_node);
  const approvalText = [approvalStatus, approvalNode].filter(Boolean).join(' · ');
  const progressCount = smEvidenceValue(evidence.progress_count, evidence.progressCount, progress.length ? progress.length : '');
  const dependencyCount = smEvidenceValue(dependency.dependency_count, dependency.dependencyCount, Array.isArray(dependency.dependencies) ? dependency.dependencies.length : '');
  const blocking = smEvidenceValue(dependency.has_blocking_dependency, dependency.hasBlockingDependency, dependency.has_missing_required, dependency.hasMissingRequired);
  const layoutStudio = layout.studio && typeof layout.studio === 'object' ? layout.studio : {};
  const layoutSaved = smEvidenceValue(evidence.workspace_layout_studio_saved_in_manifest, evidence.workspace_saved_in_manifest, layout.studio_saved_in_manifest, layoutStudio.savedInManifest, layoutStudio.saved_in_manifest);
  const layoutUpdatedBy = smEvidenceValue(evidence.workspace_layout_studio_updated_by, evidence.workspace_updated_by, layout.studio_updated_by, layoutStudio.updatedBy, layoutStudio.updated_by);
  const layoutText = [layout.template, layout.entry, layout.density, layoutSaved === true ? 'saved' : '', layoutUpdatedBy].filter(Boolean).join(' · ');
  const dataSrvText = smEvidenceValue(dataSrv.status, dataSrv.state, dataSrv.registration_status, evidence.datasrv_registration_status, s.datasrv_registration_status);
  const workflowText = smEvidenceValue(workflow.version, workflow.schema, workflow.workflowVersion, evidence.workflow_contract_version, s.workflow_contract_version);
  const resultPrimary = smEvidenceValue(resultContract.primary, resultContract.primary_result, evidence.result_contract_primary, s.result_contract_primary, s.maclaw_app_primary_result, evidence.primary_result);
  const resultTypes = smEvidenceValue(evidence.result_contract_type_count, resultContract.type_count, Array.isArray(resultContract.types) ? resultContract.types.length : '');
  const resultText = [resultPrimary, resultTypes !== '' ? resultTypes + ' types' : ''].filter(Boolean).join(' / ');
  const testProtocol = smEvidenceValue(evidence.test_protocol_fingerprint, evidence.testProtocolFingerprint, evidence.test_protocol_hash, evidence.definition_fingerprint, s.test_evidence_test_protocol_fingerprint, s.maclaw_app_definition_sha256);
  const coverageOK = smEvidenceValue(resultCoverage.ok, resultCoverage.covered, evidence.result_coverage_ok, s.test_evidence_result_coverage_ok);
  const coveragePrimary = smEvidenceValue(resultCoverage.primary, resultCoverage.primary_result, evidence.result_coverage_primary, s.test_evidence_result_coverage_primary);
  const coverageCovered = smEvidenceValue(evidence.result_coverage_covered_count, evidence.resultCoverageCoveredCount, resultCoverage.covered_count, Array.isArray(resultCoverage.coveredTypes) ? resultCoverage.coveredTypes.length : '', Array.isArray(resultCoverage.covered_types) ? resultCoverage.covered_types.length : '');
  const coverageMissing = smEvidenceValue(evidence.result_coverage_missing_count, resultCoverage.missing_count, Array.isArray(resultCoverage.missingTypes) ? resultCoverage.missingTypes.length : '', Array.isArray(resultCoverage.missing_types) ? resultCoverage.missing_types.length : '');
  const coverageText = [coveragePrimary, coverageOK !== '' ? (String(coverageOK) === 'true' ? 'ok' : String(coverageOK)) : '', coverageCovered !== '' ? 'covered ' + coverageCovered : '', coverageMissing !== '' ? 'missing ' + coverageMissing : ''].filter(Boolean).join(' / ');
  const outputCount = smEvidenceValue(evidence.output_count, evidence.outputCount, s.test_evidence_output_count, Array.isArray(evidence.outputs) ? evidence.outputs.length : '');
  const artifactCount = smEvidenceValue(evidence.artifact_count, evidence.artifactCount, s.test_evidence_artifact_count, Array.isArray(evidence.artifacts) ? evidence.artifacts.length : '');
  const outputText = [outputCount !== '' ? outputCount + ' outputs' : '', artifactCount !== '' ? artifactCount + ' artifacts' : ''].filter(Boolean).join(' / ');
  const chips = [];
  if (approvalText) chips.push(smEvidenceChip(smAdminLabel('approval'), approvalText, smEvidenceState(approvalStatus || approvalText)));
  if (progressCount !== '') chips.push(smEvidenceChip(smAdminLabel('progress'), progressCount, Number(progressCount) > 0 ? 'partial' : 'skipped'));
  if (dependencyCount !== '') chips.push(smEvidenceChip(smAdminLabel('dependencies'), dependencyCount, String(blocking) === 'true' ? 'failed' : 'ready'));
  if (layoutText) chips.push(smEvidenceChip(smAdminLabel('workspaceLayout'), layoutText, 'ready'));
  if (dataSrvText) chips.push(smEvidenceChip(smAdminLabel('dataSrv'), dataSrvText, smEvidenceState(dataSrvText)));
  if (workflowText) chips.push(smEvidenceChip(smAdminLabel('workflowContract'), workflowText, 'ready'));
  if (resultText) chips.push(smEvidenceChip(smAdminLabel('resultContract'), resultText, resultPrimary ? 'ready' : 'partial'));
  if (testProtocol) chips.push(smEvidenceChip(smAdminLabel('testProtocol'), testProtocol, 'ready'));
  if (coverageText) chips.push(smEvidenceChip(smAdminLabel('resultCoverage'), coverageText, String(coverageOK) === 'false' || Number(coverageMissing) > 0 ? 'failed' : 'ready'));
  if (outputText) chips.push(smEvidenceChip(smAdminLabel('outputs'), outputText, 'ready'));
  if (!chips.length) return '';
  return `<div class="item-meta"><strong>${escapeHtml(smAdminLabel('reviewEvidence'))}</strong></div><div class="sm-review-evidence-strip" role="list" aria-label="${escapeHtml(smAdminLabel('reviewEvidence'))}">${chips.join('')}</div>`;
}
function smRenderReviewDetail(s) {
  const isApp = !!(s && (s.is_maclaw_app || s.product_kind === 'maclaw_app_skill'));
  if (!isApp) return '';
  const outputs = smValueList(s.maclaw_app_output_modes).join(', ') || smAdminLabel('none');
  const contractOutputs = smValueList(s.artifact_contract_output_modes).join(', ') || smAdminLabel('none');
  const permissions = smValueList(s.permissions).join(', ') || smAdminLabel('none');
  const env = smValueList(s.required_env).join(', ') || smAdminLabel('none');
  const labels = smValueList(s.security_labels).join(', ') || smAdminLabel('none');
  const evidence = s.maclaw_app_test_evidence || {};
  const evidenceHTML = [
    smReviewKV(smAdminLabel('runID'), evidence.run_id || '-'),
    smReviewKV(smAdminLabel('verifiedAt'), evidence.verified_at || '-'),
    smReviewKV(smAdminLabel('artifact'), evidence.artifact_present ? (evidence.artifact_name || smAdminLabel('yes')) : smAdminLabel('no')),
  ].join('');
  const enterpriseEvidenceHTML = smRenderEnterpriseReviewEvidence(s, evidence);
  const manifestHTML = s.maclaw_app_manifest_preview ? `<div class="item-meta"><strong>${escapeHtml(smAdminLabel('manifestPreview'))}</strong></div><pre class="sm-manifest-preview">${escapeHtml(JSON.stringify(s.maclaw_app_manifest_preview, null, 2))}</pre>` : '';
  return `<div class="sm-stat-grid"><div class="sm-stat"><label>${escapeHtml(smAdminLabel('appPreview'))}</label><strong title="${escapeHtml(s.maclaw_app_name || s.name || '-')}">${escapeHtml(s.maclaw_app_name || s.name || '-')}</strong></div><div class="sm-stat"><label>${escapeHtml(smAdminLabel('appCategory'))}</label><strong>${escapeHtml(s.maclaw_app_category || '-')}</strong></div><div class="sm-stat"><label>${escapeHtml(smAdminLabel('inputMode'))}</label><strong>${escapeHtml(s.maclaw_app_input_mode || '-')}</strong></div></div><div class="item-meta">${escapeHtml(s.maclaw_app_description || s.description || '')}</div><div class="sm-stat-grid"><div class="sm-stat"><label>${escapeHtml(smAdminLabel('outputModes'))}</label><strong>${escapeHtml(outputs)}</strong></div><div class="sm-stat"><label>${escapeHtml(smAdminLabel('artifactRequired'))}</label><strong>${escapeHtml(s.artifact_contract_required ? smAdminLabel('yes') : smAdminLabel('no'))}</strong></div><div class="sm-stat"><label>${escapeHtml(smAdminLabel('presentation'))}</label><strong>${escapeHtml(s.artifact_contract_presentation || '-')}</strong></div></div><div class="item-meta"><strong>${escapeHtml(smAdminLabel('artifactContract'))}:</strong> ${escapeHtml(contractOutputs)}</div><div class="item-meta"><strong>${escapeHtml(smAdminLabel('permissions'))}:</strong> ${escapeHtml(permissions)}</div><div class="item-meta"><strong>${escapeHtml(smAdminLabel('requiredEnv'))}:</strong> ${escapeHtml(env)} | <strong>${escapeHtml(smAdminLabel('requiresGUI'))}:</strong> ${escapeHtml(s.requires_gui ? smAdminLabel('yes') : smAdminLabel('no'))}</div><div class="item-meta"><strong>${escapeHtml(smAdminLabel('securityLabels'))}:</strong> ${escapeHtml(labels)}</div><div class="item-meta"><strong>${escapeHtml(smAdminLabel('definitionHash'))}:</strong> ${escapeHtml(s.maclaw_app_definition_sha256 || '-')}</div><div class="item-meta"><strong>${escapeHtml(smAdminLabel('testEvidence'))}</strong></div>${evidenceHTML}${enterpriseEvidenceHTML}${manifestHTML}`;
}
function smRenderReviewCard(s) {
  const name=escapeHtml(s.name||'-'); const version=escapeHtml(s.version||'-'); const author=escapeHtml(s.author||'-'); const uploaded=escapeHtml(s.created_at?fmtDate(s.created_at):'-'); const rating=(s.avg_rating||0).toFixed(1); const ratings=s.rating_count||0; const rawStatus=s.status||'-'; const status=escapeHtml(smStatusText(rawStatus)); const idArg=smJsArg(s.id); const appBadge=(s.is_maclaw_app||s.product_kind==='maclaw_app_skill')?`<span class="badge info">${escapeHtml(smAdminLabel('appSkill'))}</span>`:'';
  return `<div class="item sm-card"><div class="sm-card-top"><div class="sm-card-main"><div class="sm-title-row"><div class="item-title sm-title" title="${name}">${name}</div><span class="sm-pill">v${version}</span>${appBadge}</div><div class="item-meta sm-detail" title="${smtr('colAuthor')}: ${author}">${smtr('colAuthor')}: ${author}</div></div><span class="badge warn">${status}</span></div><div class="sm-stat-grid"><div class="sm-stat"><label>${smtr('colRating')}</label><strong>${rating}</strong></div><div class="sm-stat"><label>${smtr('colRatings')}</label><strong>${ratings}</strong></div><div class="sm-stat"><label>${smtr('colUploaded')}</label><strong title="${uploaded}">${uploaded}</strong></div></div>${smRenderReviewDetail(s)}<div class="actions"><button class="btn-secondary" onclick="smApprove(${idArg}, this)">${smtr('approve')}</button><button class="btn-danger" onclick="smReject(${idArg}, this)">${smtr('reject')}</button></div></div>`;
}

function setSmSectionState(section, mode, message) {
  const status = document.getElementById(section === 'review' ? 'smReviewStatus' : 'smPurchaseStatus');
  if (!status) return;
  status.className = 'sm-status';
  if (!mode || !message) {
    status.textContent = '';
    return;
  }
  status.classList.add('show', mode);
  status.textContent = message;
}

async function loadSkillmarketReview() {
  if (smReviewInFlight) return smReviewInFlight;
  smReviewInFlight = (async function() {
  const root = document.getElementById('smReviewList');
  const counter = document.getElementById('smReviewCount');
  const refreshBtn = document.getElementById('smRefreshBtn');
  const prevRefreshText = refreshBtn ? refreshBtn.textContent : '';
  if (refreshBtn) { refreshBtn.disabled = true; refreshBtn.textContent = smtr('loading'); }
  if (counter) counter.textContent = smtr('loading');
  setSmSectionState('review', 'loading', smtr('loading'));
  root.innerHTML = '';
  try {
    const data = await api('/api/v1/admin/skillmarket/review?top_n=1000');
    const list = (data.results || data || []).filter(s => s.status === 'pending_review' || s.status === 'trial');
    if (counter) counter.textContent = smtr('items', {count: list.length});
    if (!list.length) {
      root.innerHTML = '';
      setSmSectionState('review', 'empty', smtr('reviewEmpty'));
      return;
    }
    setSmSectionState('review');
    root.innerHTML = list.map(smRenderReviewCard).join('');
  } catch (err) {
    if (counter) counter.textContent = smtr('items', {count: 0});
    root.innerHTML = '';
    const msg = smtr('reviewLoadFailed', {error: err.message});
    setSmSectionState('review', 'error', msg);
    showToast(msg, 'error');
  } finally {
    if (refreshBtn) { refreshBtn.disabled = false; refreshBtn.textContent = prevRefreshText || smtr('refresh'); }
  }
  })();
  try { return await smReviewInFlight; }
  finally { smReviewInFlight = null; }
}
async function smApprove(id, btnEl) {
  const btn = btnEl instanceof HTMLButtonElement ? btnEl : null;
  const prev = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = smtr('loading'); }
  try { await api('/api/v1/admin/skillmarket/'+encodeURIComponent(id)+'/approve', {method:'POST'}); showToast(smtr('approved'), 'success'); loadSkillmarketReview(); } catch(err) { showToast(err.message, 'error'); } finally { if (btn) { btn.disabled = false; btn.textContent = prev || smtr('approve'); } }
}

async function smReject(id, btnEl) {
  const btn = btnEl instanceof HTMLButtonElement ? btnEl : null;
  const prev = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = smtr('loading'); }
  try { await api('/api/v1/admin/skillmarket/'+encodeURIComponent(id)+'/reject', {method:'POST'}); showToast(smtr('rejected'), 'success'); loadSkillmarketReview(); } catch(err) { showToast(err.message, 'error'); } finally { if (btn) { btn.disabled = false; btn.textContent = prev || smtr('reject'); } }
}

let smPurchasePage = 1; const smPurchasePageSize = 100; let smPurchaseFilterState = '';
async function loadSmPurchases(page) {
  if (page) smPurchasePage = page;
  const filterInput = document.getElementById('smPurchaseFilter');
  const filter = filterInput.value.trim();
  if (filter !== smPurchaseFilterState) { smPurchaseFilterState = filter; smPurchasePage = 1; }
  const requestKey = smPurchasePage + '|' + filter;
  if (smPurchasesInFlight && smPurchasesInFlightKey === requestKey) return smPurchasesInFlight;
  smPurchasesInFlightKey = requestKey;
  smPurchasesInFlight = (async function() {
  const root = document.getElementById('smPurchaseList');
  const counter = document.getElementById('smPurchaseCount');
  const pager = document.getElementById('smPurchasePager');
  const pageInfo = document.getElementById('smPurchasePageInfo');
  const prevBtn = document.getElementById('smPurchasePrevBtn');
  const nextBtn = document.getElementById('smPurchaseNextBtn');
  const searchBtn = document.getElementById('smSearchBtn');
  const prevSearchText = searchBtn ? searchBtn.textContent : '';
  if (searchBtn) { searchBtn.disabled = true; searchBtn.textContent = smtr('loading'); }
  if (filterInput) filterInput.disabled = true;
  if (counter) counter.textContent = smtr('loading');
  if (pager) pager.classList.remove('is-visible');
  setSmSectionState('purchase', 'loading', smtr('loading'));
  root.innerHTML = '';
  try {
    let url = '/api/v1/admin/purchases?offset=' + ((smPurchasePage-1) * smPurchasePageSize) + '&limit=' + smPurchasePageSize;
    if (filter) {
      const queryKey = filter.includes('@') ? 'buyer_email' : 'skill_id';
      url += '&' + queryKey + '=' + encodeURIComponent(filter);
    }
    const payload = await api(url);
    const records = Array.isArray(payload) ? payload : (payload.records || []);
    const total = typeof payload?.total === 'number' ? payload.total : records.length;
    const totalPages = Math.max(1, Math.ceil(total / smPurchasePageSize));
    if (!records.length && total > 0 && smPurchasePage > totalPages) {
      smPurchasePage = totalPages;
      return loadSmPurchases(smPurchasePage);
    }
    if (counter) counter.textContent = smtr('items', {count: total});
    if (!records.length) {
      root.innerHTML = '';
      setSmSectionState('purchase', 'empty', smtr('purchaseEmpty'));
      return;
    }
    setSmSectionState('purchase');
    root.innerHTML = records.map(p => { const buyer=escapeHtml(p.buyer_email||'-'); const skill=escapeHtml(p.skill_id||'-'); const amount=escapeHtml(String(p.amount_paid??'-')); const rawType=p.purchase_type||'-'; const rawStatus=p.status||'-'; const type=escapeHtml(smPurchaseTypeText(rawType)); const status=escapeHtml(smStatusText(rawStatus)); const active=p.status === 'active'; const refundBtn = active ? `<button class="btn-danger" onclick="smRefund(${smJsArg(p.id)}, this)">${smtr('refund')}</button>` : `<span class="badge info">${smtr('refunded')}</span>`; return `<div class="item sm-card"><div class="sm-card-top"><div class="sm-card-main"><div class="sm-title-row"><div class="item-title sm-title" title="${buyer}">${buyer}</div><span class="sm-pill">${type}</span></div><div class="item-meta sm-detail" title="${smtr('colSkill')}: ${skill}">${smtr('colSkill')}: ${skill}</div></div><span class="badge ${active?'ok':'info'}">${active?smtr('active'):status}</span></div><div class="sm-stat-grid"><div class="sm-stat"><label>${smtr('colAmount')}</label><strong>${amount}</strong></div><div class="sm-stat"><label>${smtr('colType')}</label><strong title="${type}">${type}</strong></div><div class="sm-stat"><label>${smtr('colStatus')}</label><strong title="${status}">${status}</strong></div></div><div class="actions">${refundBtn}</div></div>`; }).join('');
    if (pager) pager.classList.toggle('is-visible', totalPages > 1);
    if (pageInfo) pageInfo.textContent = smtr('page', {current: smPurchasePage, total: totalPages});
    if (prevBtn) prevBtn.disabled = smPurchasePage <= 1;
    if (nextBtn) nextBtn.disabled = smPurchasePage >= totalPages;
  } catch (err) {
    if (counter) counter.textContent = smtr('items', {count: 0});
    root.innerHTML = '';
    if (pager) pager.classList.remove('is-visible');
    const msg = smtr('purchaseLoadFailed', {error: err.message});
    setSmSectionState('purchase', 'error', msg);
    showToast(msg, 'error');
  } finally {
    if (searchBtn) { searchBtn.disabled = false; searchBtn.textContent = prevSearchText || smtr('search'); }
    if (filterInput) filterInput.disabled = false;
  }
  })();
  try { return await smPurchasesInFlight; }
  finally { smPurchasesInFlight = null; smPurchasesInFlightKey = ''; }
}
function changeSmPurchasePage(delta) { loadSmPurchases(smPurchasePage + delta); }
async function smRefund(purchaseID, btnEl) {
  const reason = prompt(smtr('refundReason'));
  if (!reason) return;
  const btn = btnEl instanceof HTMLButtonElement ? btnEl : null;
  const prev = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = smtr('loading'); }
  try {
    await api('/api/v1/admin/refund', {method:'POST', body: JSON.stringify({purchase_record_id: purchaseID, reason})});
    showToast(smtr('refundOk'), 'success');
    loadSmPurchases(smPurchasePage);
  } catch(err) { showToast(err.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.textContent = prev || smtr('refund'); } }
}

async function loadSmTrialConfig() {
  if (smTrialConfigInFlight) return smTrialConfigInFlight;
  smTrialConfigInFlight = (async function() {
  try {
    const cfg = await api('/api/v1/admin/config/trial');
    if (cfg.trial_duration_days !== undefined) document.getElementById('smTrialDays').value = cfg.trial_duration_days;
    if (cfg.auto_publish_threshold !== undefined) document.getElementById('smThreshold').value = cfg.auto_publish_threshold;
    if (cfg.max_uploads_per_hour !== undefined) document.getElementById('smMaxUploadsPerHour').value = cfg.max_uploads_per_hour;
  } catch(err) {
    showToast(smtr('configLoadFailed', {error: err.message}), 'error');
  }
  })();
  try { return await smTrialConfigInFlight; }
  finally { smTrialConfigInFlight = null; }
}

async function saveSmTrialConfig() {
  const days = parseInt(document.getElementById('smTrialDays').value, 10);
  const threshold = parseInt(document.getElementById('smThreshold').value, 10);
  const maxUploads = parseInt(document.getElementById('smMaxUploadsPerHour').value || '0', 10);
  if (!Number.isFinite(days) || !Number.isFinite(threshold) || !Number.isFinite(maxUploads) || days <= 0 || threshold <= 0 || maxUploads < 0) {
    showToast(smtr('configInvalid'), 'error');
    return;
  }
  const saveBtn = document.getElementById('smSaveBtn');
  if (saveBtn) { saveBtn.disabled = true; saveBtn.textContent = smtr('saving'); }
  try {
    await api('/api/v1/admin/config/trial', {method:'PUT', body: JSON.stringify({trial_duration_days: days, auto_publish_threshold: threshold, max_uploads_per_hour: maxUploads})});
    showToast(smtr('configSaved'), 'success');
    await loadSmTrialConfig();
  } catch(err) { showToast(err.message, 'error'); }
  finally {
    if (saveBtn) { saveBtn.disabled = false; saveBtn.textContent = smtr('save'); }
  }
}

async function loadSmUploadAuthConfig() {
  if (smUploadAuthConfigInFlight) return smUploadAuthConfigInFlight;
  smUploadAuthConfigInFlight = (async function() {
  try {
    const data = await api('/api/v1/admin/config/upload-auth');
    const sel = document.getElementById('smUploadAuthMode');
    if (sel) sel.value = data.mode || 'both';
  } catch(err) { console.warn('load upload auth config:', err); }
  })();
  try { return await smUploadAuthConfigInFlight; }
  finally { smUploadAuthConfigInFlight = null; }
}

async function saveSmUploadAuthConfig() {
  const mode = document.getElementById('smUploadAuthMode').value;
  const statusEl = document.getElementById('smUploadAuthStatus');
  const saveBtn = document.getElementById('smUploadAuthSaveBtn');
  if (saveBtn) { saveBtn.disabled = true; saveBtn.textContent = smtr('saving'); }
  try {
    const data = await api('/api/v1/admin/config/upload-auth', {method:'PUT', body: JSON.stringify({mode: mode})});
    const desc = currentLang === 'zh' ? smUploadAuthModeText(mode) : (data.description || smUploadAuthModeText(mode));
    if (statusEl) { statusEl.className = 'sm-status show'; statusEl.textContent = smtr('uploadAuthStatusSaved', {description: desc}); }
    showToast(smtr('uploadAuthSaved', {mode: smUploadAuthModeText(mode)}), 'success');
  } catch(err) {
    const message = smtr('uploadAuthStatusError', {error: err.message});
    if (statusEl) { statusEl.className = 'sm-status show error'; statusEl.textContent = message; }
    showToast(message, 'error');
  } finally {
    if (saveBtn) { saveBtn.disabled = false; saveBtn.textContent = smtr('save'); }
  }
}

function applySmI18n() {
  const el = id => document.getElementById(id);
  if(el('smReviewKicker')) el('smReviewKicker').textContent = smtr('reviewKicker');
  if(el('smReviewCount') && !el('smReviewCount').textContent) el('smReviewCount').textContent = smtr('items', {count: 0});
  if(el('smPurchaseCount') && !el('smPurchaseCount').textContent) el('smPurchaseCount').textContent = smtr('items', {count: 0});
  if(el('smReviewTitle')) el('smReviewTitle').textContent = smtr('reviewTitle');
  if(el('smReviewDesc')) el('smReviewDesc').textContent = smtr('reviewDesc');
  if(el('smPurchaseKicker')) el('smPurchaseKicker').textContent = smtr('purchaseKicker');
  if(el('smPurchaseTitle')) el('smPurchaseTitle').textContent = smtr('purchaseTitle');
  if(el('smPurchaseDesc')) el('smPurchaseDesc').textContent = smtr('purchaseDesc');
  if(el('smConfigKicker')) el('smConfigKicker').textContent = smtr('configKicker');
  if(el('smTrialTitle')) el('smTrialTitle').textContent = smtr('trialTitle');
  if(el('smTrialDesc')) el('smTrialDesc').textContent = smtr('trialDesc');
  if(el('smTrialDaysLabel')) el('smTrialDaysLabel').textContent = smtr('trialDays');
  if(el('smTrialDaysHint')) el('smTrialDaysHint').textContent = smtr('trialDaysHint');
  if(el('smThresholdLabel')) el('smThresholdLabel').textContent = smtr('threshold');
  if(el('smThresholdHint')) el('smThresholdHint').textContent = smtr('thresholdHint');
  if(el('smMaxUploadsLabel')) el('smMaxUploadsLabel').textContent = smtr('maxUploads');
  if(el('smMaxUploadsHint')) el('smMaxUploadsHint').textContent = smtr('maxUploadsHint');
  if(el('smSaveBtn') && !el('smSaveBtn').disabled) el('smSaveBtn').textContent = smtr('save');
  if(el('smUploadAuthKicker')) el('smUploadAuthKicker').textContent = smtr('uploadAuthKicker');
  if(el('smUploadAuthTitle')) el('smUploadAuthTitle').textContent = smtr('uploadAuthTitle');
  if(el('smUploadAuthDesc')) el('smUploadAuthDesc').textContent = smtr('uploadAuthDesc');
  if(el('smUploadAuthModeLabel')) el('smUploadAuthModeLabel').textContent = smtr('uploadAuthMode');
  if(el('smUploadAuthModeHint')) el('smUploadAuthModeHint').textContent = smtr('uploadAuthModeHint');
  if(el('smUploadAuthSaveBtn') && !el('smUploadAuthSaveBtn').disabled) el('smUploadAuthSaveBtn').textContent = smtr('save');
  if(el('smUploadAuthMode')) Array.from(el('smUploadAuthMode').options).forEach(option => { option.textContent = smUploadAuthModeText(option.value); });
  if(el('smPurchaseFilter')) el('smPurchaseFilter').placeholder = smtr('purchaseFilter');
  if(el('smRefreshBtn')) el('smRefreshBtn').textContent = smtr('refresh');
  if(el('smSearchBtn')) el('smSearchBtn').textContent = smtr('search');
}
const _baseApplyI18nSM = applyI18n;
applyI18n = function() { _baseApplyI18nSM(); applySmI18n(); };
applySmI18n();

if (token() && document.getElementById('tab-skillmarket')?.classList.contains('active')) {
  setTimeout(function() {
    loadSkillmarketReview();
    loadSmPurchases(smPurchasePage);
    loadSmTrialConfig();
    loadSmUploadAuthConfig();
  }, 0);
}
