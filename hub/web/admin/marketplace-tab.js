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
      marketplaceSubtabSettings: 'Market Settings',
      marketplacePolicyTitle: 'Enterprise Policy', marketplacePolicyDesc: 'Control how MaClaw clients search and install capabilities.',
      marketplaceEnterpriseOnlyInstall: 'Only install from enterprise Hub', marketplaceEnterpriseOnlySearch: 'Only search enterprise Hub', marketplaceViewMode: 'View mode', marketplaceSavePolicy: 'Save Policy',
      marketplaceRequestsTitle: 'Purchase Approvals', marketplaceRequestsDesc: 'Paid HubCenter capabilities wait here until an admin approves online purchase.', marketplaceRequestsStatus: 'Status', marketplaceReload: 'Reload', marketplaceApprove: 'Approve', marketplaceReject: 'Reject', marketplaceNoRequests: 'No acquisition requests match this status.',
      marketplaceCatalogTitle: 'Enterprise Capabilities', marketplaceCatalogDesc: 'Capabilities available from this Hub marketplace.', marketplaceCapabilityType: 'Type', marketplaceCapabilityAll: 'All', marketplaceMakeRequired: 'Set Required', marketplaceMakeRecommended: 'Recommend', marketplaceNoCapabilities: 'No capabilities have been imported yet.',
      marketplaceSearchTitle: 'Search External Markets', marketplaceSearchDesc: 'Hub admins can search HubCenter, ClawHub, and GitHub. HubCenter paid items create purchase requests.', marketplaceSource: 'Source', marketplaceQuery: 'Keyword', marketplaceSearch: 'Search', marketplaceImport: 'Import / Request', marketplaceNoResults: 'No external results.',
      marketplaceMCPTitle: 'MCP JSON Editor', marketplaceMCPDesc: 'Create or update an enterprise MCP capability from JSON.', marketplaceMCPId: 'Capability ID', marketplaceMCPName: 'Display name', marketplaceMCPPublisher: 'Publisher', marketplaceMCPVersion: 'Version', marketplaceMCPJson: 'MCP server JSON', marketplaceMCPSecrets: 'Secret requirements JSON', marketplaceMCPPricing: 'Pricing JSON', marketplaceMCPLicense: 'License JSON', marketplaceSaveMCP: 'Save MCP', marketplaceUseSelected: 'Use Selected', marketplaceMCPNew: '+ New MCP', marketplaceMCPType: 'Type', marketplaceMCPTypeRemote: 'Remote (HTTP/SSE)', marketplaceMCPTypeLocal: 'Local (stdio)', marketplaceMCPCommand: 'Command', marketplaceMCPArgs: 'Arguments (JSON array)', marketplaceMCPEnv: 'Environment variables (JSON)',
      marketplaceBillingTitle: 'HubCenter Billing', marketplaceBillingDesc: 'Hub signs purchases with Hub customer id and admin email.', marketplaceLoadBilling: 'Load Billing', marketplaceNoLicenses: 'No HubCenter licenses returned.',
      marketplaceOutputReady: 'Marketplace admin module ready.', marketplaceLoadFailed: 'Marketplace load failed: {error}', marketplaceSaveFailed: 'Marketplace save failed: {error}', marketplaceSearchFailed: 'Marketplace search failed: {error}', marketplaceImportFailed: 'Marketplace import failed: {error}', marketplaceActionDone: 'Marketplace action completed.', marketplacePolicySaved: 'Marketplace policy saved.', marketplaceMcpSaved: 'MCP capability saved.', marketplaceInvalidJson: 'Invalid JSON: {error}',
      marketplaceTestMCP: 'Test Connection', marketplaceTestMCPTesting: 'Testing...', marketplaceTestMCPSuccess: 'Connected. {count} tool(s) available.', marketplaceTestMCPFailed: 'Connection failed: {error}',
      marketplaceShowingFirst: '{total} items total, showing first {count}'
    });
    I18N.zh = Object.assign({}, I18N.zh, {
      navMarketplace: '\u80fd\u529b\u5e02\u573a', navMarketplaceDesc: 'Skill\u3001MCP\u3001\u8d2d\u4e70\u548c\u4e0b\u53d1', marketplaceTabTitle: '\u80fd\u529b\u5e02\u573a', marketplaceTabSubtitle: '\u7ba1\u7406\u4f01\u4e1a Skill/MCP \u7b56\u7565\u3001\u4ed8\u8d39\u5ba1\u6279\u3001\u5bfc\u5165\u548c MCP \u5b9a\u4e49\u3002',
      marketplaceSubtabMarket: '\u4f01\u4e1a\u80fd\u529b\u5e02\u573a', marketplaceSubtabSettings: '\u80fd\u529b\u5e02\u573a\u8bbe\u7f6e',
      marketplacePolicyTitle: '\u4f01\u4e1a\u7b56\u7565', marketplacePolicyDesc: '\u63a7\u5236 MaClaw \u5ba2\u6237\u7aef\u641c\u7d22\u548c\u5b89\u88c5\u80fd\u529b\u7684\u65b9\u5f0f\u3002', marketplaceEnterpriseOnlyInstall: '\u53ea\u5141\u8bb8\u4ece\u4f01\u4e1a Hub \u5b89\u88c5', marketplaceEnterpriseOnlySearch: '\u53ea\u5141\u8bb8\u641c\u7d22\u4f01\u4e1a Hub', marketplaceViewMode: '\u89c6\u56fe\u6a21\u5f0f', marketplaceSavePolicy: '\u4fdd\u5b58\u7b56\u7565',
      marketplaceRequestsTitle: '\u8d2d\u4e70\u5ba1\u6279', marketplaceRequestsDesc: '\u4ed8\u8d39 HubCenter \u80fd\u529b\u5728\u7ba1\u7406\u5458\u5ba1\u6279\u540e\u624d\u4f1a\u53d1\u8d77\u5728\u7ebf\u8d2d\u4e70\u3002', marketplaceRequestsStatus: '\u72b6\u6001', marketplaceReload: '\u5237\u65b0', marketplaceApprove: '\u6279\u51c6', marketplaceReject: '\u62d2\u7edd', marketplaceNoRequests: '\u5f53\u524d\u72b6\u6001\u4e0b\u6ca1\u6709\u7533\u8bf7\u3002',
      marketplaceCatalogTitle: '\u4f01\u4e1a\u80fd\u529b', marketplaceCatalogDesc: '\u672c Hub \u80fd\u529b\u5e02\u573a\u53ef\u7528\u7684\u80fd\u529b\u3002', marketplaceCapabilityType: '\u7c7b\u578b', marketplaceCapabilityAll: '\u5168\u90e8', marketplaceMakeRequired: '\u8bbe\u4e3a\u5fc5\u88c5', marketplaceMakeRecommended: '\u8bbe\u4e3a\u63a8\u8350', marketplaceNoCapabilities: '\u6682\u65e0\u5df2\u5bfc\u5165\u80fd\u529b\u3002',
      marketplaceSearchTitle: '\u641c\u7d22\u5916\u90e8\u5e02\u573a', marketplaceSearchDesc: 'Hub \u7ba1\u7406\u5458\u53ef\u641c\u7d22 HubCenter\u3001ClawHub \u548c GitHub\u3002HubCenter \u4ed8\u8d39\u9879\u4f1a\u751f\u6210\u8d2d\u4e70\u7533\u8bf7\u3002', marketplaceSource: '\u6765\u6e90', marketplaceQuery: '\u5173\u952e\u5b57', marketplaceSearch: '\u641c\u7d22', marketplaceImport: '\u5bfc\u5165 / \u7533\u8bf7', marketplaceNoResults: '\u6682\u65e0\u5916\u90e8\u7ed3\u679c\u3002',
      marketplaceMCPTitle: 'MCP JSON \u7f16\u8f91', marketplaceMCPDesc: '\u901a\u8fc7 JSON \u521b\u5efa\u6216\u66f4\u65b0\u4f01\u4e1a MCP \u80fd\u529b\u3002', marketplaceMCPId: '\u80fd\u529b ID', marketplaceMCPName: '\u663e\u793a\u540d\u79f0', marketplaceMCPPublisher: '\u53d1\u5e03\u8005', marketplaceMCPVersion: '\u7248\u672c', marketplaceMCPJson: 'MCP \u670d\u52a1\u5668 JSON', marketplaceMCPSecrets: 'Secret \u9700\u6c42 JSON', marketplaceMCPPricing: '\u8ba1\u8d39 JSON', marketplaceMCPLicense: '\u8bb8\u53ef JSON', marketplaceSaveMCP: '\u4fdd\u5b58 MCP', marketplaceUseSelected: '\u5957\u7528\u5230\u7f16\u8f91\u5668', marketplaceMCPNew: '+ \u65b0\u5efa MCP', marketplaceMCPType: '\u7c7b\u578b', marketplaceMCPTypeRemote: '\u8fdc\u7a0b (HTTP/SSE)', marketplaceMCPTypeLocal: '\u672c\u5730 (stdio)', marketplaceMCPCommand: '\u547d\u4ee4', marketplaceMCPArgs: '\u53c2\u6570 (JSON \u6570\u7ec4)', marketplaceMCPEnv: '\u73af\u5883\u53d8\u91cf (JSON)',
      marketplaceBillingTitle: 'HubCenter \u8d26\u6237', marketplaceBillingDesc: 'Hub \u4f7f\u7528 Hub \u5ba2\u6237 ID \u548c\u7ba1\u7406\u5458\u90ae\u7bb1\u5b8c\u6210\u8d2d\u4e70\u3002', marketplaceLoadBilling: '\u52a0\u8f7d\u8d26\u6237', marketplaceNoLicenses: '\u6682\u65e0 HubCenter \u8bb8\u53ef\u3002', marketplaceOutputReady: '\u80fd\u529b\u5e02\u573a\u7ba1\u7406\u6a21\u5757\u5df2\u5c31\u7eea\u3002', marketplaceLoadFailed: '\u52a0\u8f7d\u80fd\u529b\u5e02\u573a\u5931\u8d25\uff1a{error}', marketplaceSaveFailed: '\u4fdd\u5b58\u80fd\u529b\u5e02\u573a\u5931\u8d25\uff1a{error}', marketplaceSearchFailed: '\u641c\u7d22\u80fd\u529b\u5e02\u573a\u5931\u8d25\uff1a{error}', marketplaceImportFailed: '\u5bfc\u5165\u80fd\u529b\u5931\u8d25\uff1a{error}', marketplaceActionDone: '\u64cd\u4f5c\u5df2\u5b8c\u6210\u3002', marketplacePolicySaved: '\u80fd\u529b\u5e02\u573a\u7b56\u7565\u5df2\u4fdd\u5b58\u3002', marketplaceMcpSaved: 'MCP \u80fd\u529b\u5df2\u4fdd\u5b58\u3002', marketplaceInvalidJson: 'JSON \u65e0\u6548\uff1a{error}',
      marketplaceTestMCP: '\u6d4b\u8bd5\u8fde\u63a5', marketplaceTestMCPTesting: '\u6d4b\u8bd5\u4e2d...', marketplaceTestMCPSuccess: '\u8fde\u63a5\u6210\u529f\u3002{count} \u4e2a\u5de5\u5177\u53ef\u7528\u3002', marketplaceTestMCPFailed: '\u8fde\u63a5\u5931\u8d25\uff1a{error}',
      marketplaceShowingFirst: '\u5171 {total} \u9879\uff0c\u663e\u793a\u524d {count} \u9879'
    });
  }
  const state = { policy: null, capabilities: [], requests: [], externalResults: [], billing: null };
  function mp(k, v) { return typeof tr === 'function' ? tr(k, v) : k; }
  function esc(v) { return typeof escapeHtml === 'function' ? escapeHtml(String(v == null ? '' : v)) : String(v == null ? '' : v); }
  function el(id) { return document.getElementById(id); }
  function bool(v, fallback) { return typeof v === 'boolean' ? v : fallback; }
  function jsonText(text, fallback) { const raw = String(text || '').trim(); return raw ? JSON.parse(raw) : fallback; }
  function pretty(v) { return JSON.stringify(v, null, 2); }
  function firstID(item) { return item.id || item.capability_id || item.skill_id || item.name || item.key || ''; }
  function firstName(item) { return item.display_name || item.name || item.title || item.id || item.capability_id || item.skill_id || '-'; }
  function pricing(item) { const p = item.pricing || item.price_type || item.billing || item.charge_type || ''; return typeof p === 'string' ? (p || 'free') : (p && (p.type || p.mode || p.pricing)) || 'free'; }
  function priceObject(item) { return item.price && typeof item.price === 'object' ? item.price : (item.pricing && typeof item.pricing === 'object' ? item.pricing : null); }
  function licenseObject(item) { return item.license && typeof item.license === 'object' ? item.license : null; }
  function renderPolicy() {
    const root = el('marketplacePolicyBody'); if (!root) return;
    const p = state.policy || {}, md = p.managed_deployment || {}, rc = p.recommended_capability || {};
    root.innerHTML = '<div class="grid2"><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceEnterpriseOnlyInstall"> ' + esc(mp('marketplaceEnterpriseOnlyInstall')) + '</label><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceEnterpriseOnlySearch"> ' + esc(mp('marketplaceEnterpriseOnlySearch')) + '</label><div><label>' + esc(mp('marketplaceViewMode')) + '</label><select id="marketplaceViewMode"><option value="merged">merged</option><option value="enterprise_first">enterprise_first</option><option value="enterprise_only">enterprise_only</option></select></div><div><label>managed retry minutes</label><input id="marketplaceRetryMinutes" type="number" min="5" step="5"></div><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceManagedEnabled"> managed deployment enabled</label><label class="toggle-label" style="margin:0;text-transform:none;letter-spacing:0"><input type="checkbox" id="marketplaceRecommendedEnabled"> recommendations enabled</label></div><div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" onclick="saveMarketplacePolicy()">' + esc(mp('marketplaceSavePolicy')) + '</button></div>';
    el('marketplaceEnterpriseOnlyInstall').checked = bool(p.enterprise_only_install, true); el('marketplaceEnterpriseOnlySearch').checked = bool(p.enterprise_only_search, false); el('marketplaceViewMode').value = p.view_mode || 'merged'; el('marketplaceRetryMinutes').value = String(md.retry_interval_minutes || 60); el('marketplaceManagedEnabled').checked = bool(md.enabled, true); el('marketplaceRecommendedEnabled').checked = bool(rc.enabled, true);
  }
  function renderRequests() {
    const root = el('marketplaceRequestsList'); if (!root) return;
    if (!state.requests.length) { root.innerHTML = '<div class="hint">' + esc(mp('marketplaceNoRequests')) + '</div>'; return; }
    root.innerHTML = state.requests.map(function(item) { const pending = item.status === 'pending_review' || item.status === 'pending' || item.status === 'approved'; const actions = pending ? '<button class="btn-primary" type="button" onclick="approveMarketplaceRequest(\'' + esc(item.id) + '\')">' + esc(mp('marketplaceApprove')) + '</button><button class="btn-danger" type="button" onclick="rejectMarketplaceRequest(\'' + esc(item.id) + '\')">' + esc(mp('marketplaceReject')) + '</button>' : ''; return '<div class="item"><div class="item-head"><div><div class="item-title">' + esc(item.source_capability_key || item.id) + '</div><div class="item-meta">' + esc(item.capability_type) + ' | ' + esc(item.source) + ' | ' + esc(item.request_kind) + ' | ' + esc(item.status) + '</div></div><span class="badge ' + (item.status === 'completed' ? 'ok' : item.status === 'rejected' ? 'danger' : 'warn') + '">' + esc(item.status) + '</span></div><div class="item-meta">' + esc(item.reason || item.created_at || '') + '</div><div class="actions" style="margin-top:8px">' + actions + '</div></div>'; }).join('');
  }
  function renderCapabilities() {
    const root = el('marketplaceCapabilitiesList'); if (!root) return;
    if (!state.capabilities.length) { root.innerHTML = '<div class="hint" style="grid-column:1/-1">' + esc(mp('marketplaceNoCapabilities')) + '</div>'; return; }
    const maxShow = 20;
    const items = state.capabilities.slice(0, maxShow);
    const hasMore = state.capabilities.length > maxShow;
    root.innerHTML = items.map(function(item) { return '<div class="item" style="padding:12px 14px;border-radius:14px;gap:6px;min-height:160px;transition:transform .15s ease,box-shadow .15s ease,border-color .15s ease"><div class="item-head" style="margin-bottom:0"><div style="min-width:0"><div class="item-title" style="font-size:13px;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(item.display_name || item.capability_id || item.id) + '">' + esc(item.display_name || item.capability_id || item.id) + '</div></div><span class="badge info" style="font-size:9px;padding:3px 7px">' + esc(item.capability_type) + '</span></div><div class="item-meta mono" style="font-size:10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(item.id) + '">' + esc(item.id) + '</div><div class="item-meta" style="font-size:10px">' + esc(item.source || '-') + ' | ' + esc(item.status || '-') + ' | ' + esc(item.current_version_key || '-') + '</div><div class="actions" style="margin-top:auto;padding-top:4px;gap:4px;flex-wrap:wrap"><button class="btn-secondary" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="createMarketplaceDeployment(\'' + esc(item.id) + '\',\'' + esc(item.current_version_key || '') + '\')">' + esc(mp('marketplaceMakeRequired')) + '</button><button class="btn-ghost" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="createMarketplaceRecommendation(\'' + esc(item.id) + '\',\'' + esc(item.current_version_key || '') + '\')">' + esc(mp('marketplaceMakeRecommended')) + '</button><button class="btn-ghost" style="height:26px;padding:0 8px;font-size:11px;border-radius:8px" type="button" onclick="useCapabilityForMCP(\'' + esc(item.id) + '\')">' + esc(mp('marketplaceUseSelected')) + '</button></div></div>'; }).join('') + (hasMore ? '<div class="hint" style="grid-column:1/-1;text-align:center;font-size:12px">' + esc(mp('marketplaceShowingFirst', {total: state.capabilities.length, count: maxShow})) + '</div>' : '');
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
  function rerenderMarketplace() { renderPolicy(); renderRequests(); renderCapabilities(); renderExternalResults(); renderBilling(); }
  async function loadPolicy() { const data = await api('/api/admin/capability-market/policy'); state.policy = data.policy || {}; renderPolicy(); }
  async function loadCapabilities() { const type = el('marketplaceCapabilityType') ? el('marketplaceCapabilityType').value : ''; const data = await api('/api/capabilities' + (type ? '?type=' + encodeURIComponent(type) : '')); state.capabilities = Array.isArray(data.items) ? data.items : []; renderCapabilities(); }
  async function loadRequests() { const status = el('marketplaceRequestStatus') ? el('marketplaceRequestStatus').value : 'pending_review'; const data = await api('/api/admin/capability-market/acquisition-requests' + (status ? '?status=' + encodeURIComponent(status) : '')); state.requests = Array.isArray(data.items) ? data.items : []; renderRequests(); }
  global.loadMarketplace = async function() { if (typeof token === 'function' && !token()) return; if (!el('tab-marketplace')) return; try { await Promise.all([loadPolicy(), loadCapabilities(), loadRequests()]); renderBilling(); setOutput(mp('marketplaceOutputReady')); } catch (err) { const msg = mp('marketplaceLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.saveMarketplacePolicy = async function() { try { const p = state.policy || {}; p.enterprise_only_install = !!el('marketplaceEnterpriseOnlyInstall').checked; p.enterprise_only_search = !!el('marketplaceEnterpriseOnlySearch').checked; p.view_mode = el('marketplaceViewMode').value || 'merged'; p.managed_deployment = Object.assign({}, p.managed_deployment || {}, { enabled: !!el('marketplaceManagedEnabled').checked, retry_interval_minutes: Math.max(5, Number(el('marketplaceRetryMinutes').value || 60) || 60), reinstall_if_removed: true }); p.recommended_capability = Object.assign({}, p.recommended_capability || {}, { enabled: !!el('marketplaceRecommendedEnabled').checked, allow_user_dismiss: true }); const data = await api('/api/admin/capability-market/policy', { method: 'PUT', body: JSON.stringify({ policy: p }) }); state.policy = data.policy || p; renderPolicy(); setOutput(mp('marketplacePolicySaved')); showToast(mp('marketplacePolicySaved'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.approveMarketplaceRequest = async function(id) { try { await api('/api/admin/capability-market/acquisition-requests/' + encodeURIComponent(id) + '/approve', { method: 'POST', body: JSON.stringify({ approval: { mode: 'admin_approved_online_purchase' } }) }); await Promise.all([loadRequests(), loadCapabilities()]); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
  global.rejectMarketplaceRequest = async function(id) { try { await api('/api/admin/capability-market/acquisition-requests/' + encodeURIComponent(id) + '/reject', { method: 'POST', body: JSON.stringify({ approval: { mode: 'admin_rejected' } }) }); await loadRequests(); showToast(mp('marketplaceActionDone'), 'success'); } catch (err) { const msg = mp('marketplaceSaveFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };
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
  global.useCapabilityForMCP = function(id) { var item = state.capabilities.find(function(cap) { return cap.id === id; }); if (!item) return; el('marketplaceMCPId').value = item.capability_id || item.id || ''; el('marketplaceMCPName').value = item.display_name || item.capability_id || ''; el('marketplaceMCPVersion').value = item.current_version_key || '1.0.0'; var mcp = item.mcp || item.metadata && item.metadata.mcp || null; if (mcp && mcp.transport === 'stdio') { el('marketplaceMCPTypeSelect').value = 'local'; global.switchMCPEditorType('local'); el('marketplaceMCPCommand').value = mcp.command || ''; el('marketplaceMCPArgs').value = Array.isArray(mcp.args) ? JSON.stringify(mcp.args) : '[]'; el('marketplaceMCPEnv').value = mcp.env && typeof mcp.env === 'object' ? JSON.stringify(mcp.env, null, 2) : '{}'; } else { el('marketplaceMCPTypeSelect').value = 'remote'; global.switchMCPEditorType('remote'); if (mcp) el('marketplaceMCPJson').value = JSON.stringify(mcp, null, 2); } global.openMCPEditorDialog(); };
  global.loadMarketplaceBilling = async function() { try { const account = await api('/api/admin/billing/customer-account'); const licensesData = await api('/api/admin/billing/licenses'); state.billing = { account: account, licenses: Array.isArray(licensesData.items) ? licensesData.items : (Array.isArray(licensesData.licenses) ? licensesData.licenses : []) }; renderBilling(); } catch (err) { const msg = mp('marketplaceLoadFailed', { error: err.message }); setOutput(msg); showToast(msg, 'error'); } };

  function marketplaceTenantScopedRefresh() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') global.AdminTabRegistry.registerTab({ id: 'marketplace', title: function() { return mp('marketplaceTabTitle'); }, subtitle: function() { return mp('marketplaceTabSubtitle'); }, onOpen: function() { global.loadMarketplace(); } });
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') global.AdminTabRegistry.onLanguageChange(function() { rerenderMarketplace(); });
  if (typeof global.refreshAll === 'function') { const baseRefreshAll = global.refreshAll; global.refreshAll = async function() { if (marketplaceTenantScopedRefresh()) await Promise.all([baseRefreshAll(), global.loadMarketplace()]); else await baseRefreshAll(); }; }
  global.addEventListener('keydown', function(event) { if (event.key === 'Enter' && event.target && event.target.id === 'marketplaceSearchQuery') global.searchMarketplaceExternal(); });
  // Sub-tab switching
  global.switchMarketplaceSubtab = function(tab) {
    var marketPanel = el('marketplace-subtab-market');
    var settingsPanel = el('marketplace-subtab-settings');
    var marketBtn = el('subtab-market');
    var settingsBtn = el('subtab-settings');
    if (!marketPanel || !settingsPanel) return;
    if (tab === 'market') {
      marketPanel.style.display = ''; settingsPanel.style.display = 'none';
      marketBtn.classList.add('active'); settingsBtn.classList.remove('active');
    } else {
      marketPanel.style.display = 'none'; settingsPanel.style.display = '';
      marketBtn.classList.remove('active'); settingsBtn.classList.add('active');
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
  global.addEventListener('keydown', function(event) { if (event.key === 'Escape') { var overlay = el('mcpEditorOverlay'); if (overlay && overlay.style.display === 'flex') { global.closeMCPEditorDialog(); event.stopPropagation(); } } });
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
