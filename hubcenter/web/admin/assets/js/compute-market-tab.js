// HubCenter Admin: Compute Market Tab
// Manages card types, orders, and usage statistics for the compute power marketplace.

// Register i18n keys (must happen before applyI18n runs on this tab's elements)
if (typeof I18N_EN !== 'undefined') {
  Object.assign(I18N_EN, {computeMarketStatsHub:'Hub Filter',computeMarketStatsTenant:'Tenant Filter',computeMarketStatsPeriod:'Period',computeMarketPeriodDaily:'Daily',computeMarketPeriodWeekly:'Weekly',computeMarketPeriodMonthly:'Monthly',computeMarketStartDate:'Start Date',computeMarketEndDate:'End Date',computeMarketQuery:'Query',computeMarketStatsHint:'Select filters and click Query.',computeMarketStatsEmpty:'No usage data.',computeMarketStatsError:'Query failed',computeMarketStatsTotalInput:'Input Tokens',computeMarketStatsTotalOutput:'Output Tokens',computeMarketStatsTotalCredits:'Credits Used',computeMarketStatsTotalRequests:'Requests',computeMarketStatsCacheHitRate:'Cache Hit Rate',computeMarketStatsPeriodStart:'Period',computeMarketStatsAllHubs:'All Hubs',computeMarketStatsAllTenants:'All Tenants',computeMarketStatusEnabled:'Enabled',computeMarketStatusDisabled:'Disabled',computeMarketEdit:'Edit',computeMarketCardNameRequired:'Card name is required',computeMarketCardGroupRequired:'Service group is required',computeMarketCardCreditsRequired:'Credits must be greater than 0',computeMarketCardPriceRequired:'Price must be greater than 0',computeMarketConfirmOrderPrompt:'Confirm this order is paid? Credits will be added immediately.',computeMarketOrderConfirmed:'Order confirmed.',computeMarketArchiveOrder:'Archive',computeMarketArchiveOrderPrompt:'Archive this order? It will leave the active order list.',computeMarketOrderArchived:'Order archived.',computeMarketViewArchivedOrders:'View archived orders',computeMarketViewActiveOrders:'View active orders',computeMarketArchivedOrdersDesc:'Viewing archived old orders. These orders are hidden from the active queue.',computeMarketArchivedAt:'Archived at',computeMarketCardDescription:'Detailed Description',computeMarketCardDescriptionHint:'Visible in admin list and storefront. Include suitable tenant, duration, model capability, and usage boundary.',computeMarketCardDescriptionEmpty:'No description yet',computeMarketCardServiceGroup:'Service group',computeMarketCardAgent:'Compute agent',computeMarketCardCreditsUnit:'credits',computeMarketCardPriceUnit:'RMB'});
  Object.assign(I18N_ZH, {computeMarketStatsHub:'Hub \u7b5b\u9009',computeMarketStatsTenant:'\u79df\u6237\u7b5b\u9009',computeMarketStatsPeriod:'\u7edf\u8ba1\u5468\u671f',computeMarketPeriodDaily:'\u6309\u65e5',computeMarketPeriodWeekly:'\u6309\u5468',computeMarketPeriodMonthly:'\u6309\u6708',computeMarketStartDate:'\u5f00\u59cb\u65e5\u671f',computeMarketEndDate:'\u7ed3\u675f\u65e5\u671f',computeMarketQuery:'\u67e5\u8be2',computeMarketStatsHint:'\u9009\u62e9\u7b5b\u9009\u6761\u4ef6\u540e\u70b9\u51fb\u67e5\u8be2\u3002',computeMarketStatsEmpty:'\u6240\u9009\u671f\u95f4\u6682\u65e0\u7528\u91cf\u6570\u636e\u3002',computeMarketStatsError:'\u67e5\u8be2\u5931\u8d25',computeMarketStatsTotalInput:'\u8f93\u5165 Token',computeMarketStatsTotalOutput:'\u8f93\u51fa Token',computeMarketStatsTotalCredits:'\u6d88\u8017\u989d\u5ea6',computeMarketStatsTotalRequests:'\u8bf7\u6c42\u6570',computeMarketStatsCacheHitRate:'\u7f13\u5b58\u547d\u4e2d\u7387',computeMarketStatsPeriodStart:'\u65f6\u6bb5',computeMarketStatsAllHubs:'\u5168\u90e8 Hub',computeMarketStatsAllTenants:'\u5168\u90e8\u79df\u6237',computeMarketStatusEnabled:'\u4e0a\u67b6',computeMarketStatusDisabled:'\u4e0b\u67b6',computeMarketEdit:'\u7f16\u8f91',computeMarketCardNameRequired:'\u8bf7\u586b\u5199\u5361\u540d\u79f0',computeMarketCardGroupRequired:'\u8bf7\u5148\u9009\u62e9\u670d\u52a1\u7ec4',computeMarketCardCreditsRequired:'\u7b97\u529b\u989d\u5ea6\u5fc5\u987b\u5927\u4e8e 0',computeMarketCardPriceRequired:'\u4ef7\u683c\u5fc5\u987b\u5927\u4e8e 0',computeMarketConfirmOrderPrompt:'\u786e\u8ba4\u8be5\u8ba2\u5355\u5df2\u5230\u8d26\uff1f\u5c06\u7acb\u5373\u4e3a\u79df\u6237\u5145\u503c\u3002',computeMarketOrderConfirmed:'\u5df2\u786e\u8ba4\u3002',computeMarketArchiveOrder:'\u5f52\u6863',computeMarketArchiveOrderPrompt:'\u5f52\u6863\u8be5\u8ba2\u5355\uff1f\u5b83\u5c06\u4e0d\u518d\u663e\u793a\u5728\u5f53\u524d\u8ba2\u5355\u5217\u8868\u3002',computeMarketOrderArchived:'\u5df2\u5f52\u6863\u3002',computeMarketViewArchivedOrders:'\u67e5\u770b\u5f52\u6863\u8ba2\u5355',computeMarketViewActiveOrders:'\u67e5\u770b\u5f53\u524d\u8ba2\u5355',computeMarketArchivedOrdersDesc:'\u6b63\u5728\u67e5\u770b\u5df2\u5f52\u6863\u7684\u65e7\u8ba2\u5355\uff0c\u8fd9\u4e9b\u8ba2\u5355\u4e0d\u4f1a\u51fa\u73b0\u5728\u5f53\u524d\u961f\u5217\u3002',computeMarketArchivedAt:'\u5f52\u6863\u65f6\u95f4',computeMarketCardDescription:'\u8be6\u7ec6\u63cf\u8ff0',computeMarketCardDescriptionHint:'\u4f1a\u5728\u7ba1\u7406\u5217\u8868\u548c\u8d2d\u4e70\u9875\u663e\u793a\uff0c\u53ef\u5199\u9002\u7528\u79df\u6237\u3001\u5468\u671f\u3001\u6a21\u578b\u80fd\u529b\u548c\u4f7f\u7528\u8fb9\u754c\u3002',computeMarketCardDescriptionEmpty:'\u6682\u65e0\u8be6\u7ec6\u63cf\u8ff0',computeMarketCardServiceGroup:'\u670d\u52a1\u7ec4',computeMarketCardAgent:'\u7b97\u529b\u4ee3\u7406\u5546',computeMarketCardCreditsUnit:'\u70b9',computeMarketCardPriceUnit:'\u5143'});
  Object.assign(I18N_EN, {computeMarketDeleteArchivedOrder:'Delete',computeMarketDeleteArchivedOrderPrompt:'Delete this archived unpaid order? This cannot be undone.',computeMarketOrderDeleted:'Order deleted.'});
  Object.assign(I18N_ZH, {computeMarketDeleteArchivedOrder:'\u5220\u9664',computeMarketDeleteArchivedOrderPrompt:'\u5220\u9664\u8fd9\u7b14\u5df2\u5f52\u6863\u4e14\u672a\u652f\u4ed8\u7684\u8ba2\u5355\uff1f\u6b64\u64cd\u4f5c\u4e0d\u53ef\u6062\u590d\u3002',computeMarketOrderDeleted:'\u8ba2\u5355\u5df2\u5220\u9664\u3002'});
  Object.assign(I18N_EN, {computeMarketStatsIdentity:'Scope',computeMarketStatsHubLabel:'Hub',computeMarketStatsTenantLabel:'Tenant',computeMarketStatsLegacyIdentity:'Historical record without identity',computeMarketStatsLegacyHint:'Hub and tenant were not captured on this usage row.',computeMarketStatsRows:'Rows'});
  Object.assign(I18N_ZH, {computeMarketStatsIdentity:'\u7edf\u8ba1\u5bf9\u8c61',computeMarketStatsHubLabel:'Hub',computeMarketStatsTenantLabel:'\u79df\u6237',computeMarketStatsLegacyIdentity:'\u5386\u53f2\u672a\u8bb0\u5f55\u8eab\u4efd',computeMarketStatsLegacyHint:'\u8be5\u7528\u91cf\u8bb0\u5f55\u672a\u91c7\u96c6 Hub \u548c\u79df\u6237\u4fe1\u606f\u3002',computeMarketStatsRows:'\u884c\u6570'});
}

(function () {
  'use strict';

  let cmCurrentSubTab = 'cards';
  let cmCardTypes = [];
  let cmEditingCardID = '';
  let cmInitInFlight = null;
  let cmOrdersArchived = false;

  var CONFIRMABLE_STATUSES = ['pending', 'personal_created', 'personal_opened'];

  function esc(s) { return String(s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;'); }

  function adminToken() {
    return (typeof window.token === 'function' ? window.token() : '') || localStorage.getItem('maclawHubCenterAdminToken') || sessionStorage.getItem('maclawHubCenterAdminToken') || '';
  }

  function apiErrorMessage(err, fallback) {
    if (err && typeof err.error === 'object' && err.error && err.error.message) return err.error.message;
    if (err && typeof err.error === 'string') return err.error;
    return (err && err.message) || fallback;
  }

  async function api(path, opts) {
    if (typeof window.api === 'function') return window.api(path, opts || {});
    const token = adminToken();
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers.Authorization = 'Bearer ' + token;
    const resp = await fetch(path, Object.assign({ headers: headers }, opts));
    if (!resp.ok) { const err = await resp.json().catch(function () { return { error: resp.statusText }; }); throw new Error(apiErrorMessage(err, resp.statusText)); }
    return resp.json();
  }

  // ---------------------------------------------------------------------------
  // Sub-tab switching
  // ---------------------------------------------------------------------------

  function switchComputeMarketSubTab(tab, preloadedOrders) {
    cmCurrentSubTab = tab;
    ['cards', 'orders', 'stats', 'payment'].forEach(function (t) {
      var view = document.getElementById('cmSubView' + t.charAt(0).toUpperCase() + t.slice(1));
      var btn = document.getElementById('cmSubTab' + t.charAt(0).toUpperCase() + t.slice(1));
      if (view) view.classList.toggle('hidden-view', t !== tab);
      if (btn) { btn.className = t === tab ? 'btn-secondary' : 'btn-ghost'; btn.setAttribute('aria-pressed', t === tab ? 'true' : 'false'); }
    });
    if (tab === 'orders') {
      if (preloadedOrders) { renderComputeOrders(preloadedOrders); }
      else { loadComputeOrders(); }
    }
    if (tab === 'payment' && window.loadCMPaymentConfig) loadCMPaymentConfig();
  }

  // ---------------------------------------------------------------------------
  // Card Types
  // ---------------------------------------------------------------------------

  async function loadComputeCardTypes() {
    try {
      const data = await api('/api/admin/cardstore/types');
      cmCardTypes = data.card_types || [];
      renderComputeCards(cmCardTypes);
      const groupSelect = document.getElementById('cmCardGroup');
      if (groupSelect) {
        try {
          const gd = await api('/api/admin/llm/service-groups');
          groupSelect.innerHTML = (gd.service_groups || []).map(function (g) { return '<option value="' + esc(g.id) + '">' + esc(g.name) + '</option>'; }).join('');
        } catch (e) { groupSelect.innerHTML = ''; }
      }
    } catch (e) { cmCardTypes = []; renderComputeCards([]); }
  }

  function renderComputeCards(types) {
    const container = document.getElementById('cmCardTypesList');
    if (!container) return;
    if (!types.length) { container.innerHTML = '<div class="hint">' + tr('computeMarketNoCards') + '</div>'; return; }
    container.innerHTML = types.map(function (ct) {
      var statusBadge = ct.enabled ? '<span class="badge ok">' + tr('computeMarketStatusEnabled') + '</span>' : '<span class="badge warn">' + tr('computeMarketStatusDisabled') + '</span>';
      var tmpl = CARD_TEMPLATES.find(function(t) { return t.id === ct.template; }) || CARD_TEMPLATES[0];
      var art = buildCardTemplateSVG(tmpl, 168, 100);
      var desc = (ct.description || '').trim();
      var meta = esc((ct.credits || 0).toLocaleString()) + ' ' + tr('computeMarketCardCreditsUnit') + ' | ' + esc(ct.period || '') + ' | ' + tr('computeMarketCardPriceUnit') + ' ' + esc(ct.price_rmb || 0);
      var groupName = ct.service_group || ct.service_group_id || '';
      var agentName = ct.agent_name || '';
      var group = agentName ? '<span class="cm-card-group">' + tr('computeMarketCardAgent') + ': ' + esc(agentName) + (groupName ? ' | ' + tr('computeMarketCardServiceGroup') + ': ' + esc(groupName) : '') + '</span>' : (groupName ? '<span class="cm-card-group">' + tr('computeMarketCardServiceGroup') + ': ' + esc(groupName) + '</span>' : '');
      return '<article class="cm-card-tile">'
        + '<div class="cm-card-visual">' + art + '</div>'
        + '<div class="cm-card-body">'
        + '<div class="cm-card-top"><strong>' + esc(ct.label) + '</strong>' + statusBadge + '</div>'
        + '<p class="cm-card-desc">' + esc(desc || tr('computeMarketCardDescriptionEmpty')) + '</p>'
        + '<div class="cm-card-foot"><span class="data-row-meta">' + meta + '</span>' + group + '</div>'
        + '</div>'
        + '<div class="cm-card-actions"><button class="btn-ghost" onclick="editComputeCard(\'' + esc(ct.id) + '\')">' + tr('computeMarketEdit') + '</button></div>'
        + '</article>';
    }).join('');
  }

  function showComputeCardEditor() {
    cmEditingCardID = '';
    // Build card editor as a modal dialog
    var groupOptionsHTML = '';
    api('/api/admin/llm/service-groups').then(function(gd) {
      groupOptionsHTML = (gd.service_groups || []).map(function(g) { return '<option value="' + esc(g.id) + '">' + esc(g.name) + '</option>'; }).join('');
      renderCardEditorDialog(groupOptionsHTML, null);
    }).catch(function() { renderCardEditorDialog('', null); });
  }

  function editComputeCard(id) {
    var card = cmCardTypes.find(function (ct) { return ct.id === id; });
    if (!card) return;
    cmEditingCardID = id;
    api('/api/admin/llm/service-groups').then(function(gd) {
      var groupOptionsHTML = (gd.service_groups || []).map(function(g) {
        return '<option value="' + esc(g.id) + '"' + (g.id === card.service_group_id ? ' selected' : '') + '>' + esc(g.name) + '</option>';
      }).join('');
      renderCardEditorDialog(groupOptionsHTML, card);
    }).catch(function() { renderCardEditorDialog('', card); });
  }

  function renderCardEditorDialog(groupOptionsHTML, card) {
    var overlay = document.getElementById('cmCardEditorOverlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'cmCardEditorOverlay';
      overlay.className = 'session-modal-overlay';
      if (typeof window.installOverlayDismiss === 'function') {
        window.installOverlayDismiss(overlay, hideComputeCardEditor);
      } else {
        var startedOnOverlay = false;
        overlay.addEventListener('pointerdown', function(e) { startedOnOverlay = e.target === overlay; });
        overlay.addEventListener('click', function(e) { if (startedOnOverlay && e.target === overlay) hideComputeCardEditor(); startedOnOverlay = false; });
      }
      document.body.appendChild(overlay);
    }
    overlay.innerHTML = '<div class="session-modal cm-dialog-sm" id="cmCardEditorContent"></div>';
    card = card || {};
    var html = '<h3>' + (cmEditingCardID ? tr('computeMarketEdit') : tr('computeMarketAddCard')) + '</h3>'
      + '<div class="grid2 cm-form-top">'
      + '<div><label>' + tr('computeMarketCardName') + '</label><input id="cmCardName" value="' + esc(card.label || '') + '" placeholder="e.g. Monthly Pro"></div>'
      + '<div><label>' + tr('computeMarketCardCredits') + '</label><input id="cmCardCredits" type="number" value="' + esc(card.credits || '') + '" placeholder="100000"></div>'
      + '</div>'
      + '<div class="grid2 cm-form-gap">'
      + '<div><label>' + tr('computeMarketCardPeriod') + '</label><select id="cmCardPeriod"><option value="month"' + ((card.period || 'month') === 'month' ? ' selected' : '') + '>' + tr('computeMarketMonth') + '</option><option value="quarter"' + (card.period === 'quarter' ? ' selected' : '') + '>' + tr('computeMarketQuarter') + '</option><option value="year"' + (card.period === 'year' ? ' selected' : '') + '>' + tr('computeMarketYear') + '</option></select></div>'
      + '<div><label>' + tr('computeMarketCardPrice') + '</label><input id="cmCardPrice" type="number" step="0.01" value="' + esc(card.price_rmb || '') + '" placeholder="99.00"></div>'
      + '</div>'
      + '<div class="grid2 cm-form-gap">'
      + '<div><label>' + tr('computeMarketCardGroup') + '</label><select id="cmCardGroup">' + groupOptionsHTML + '</select></div>'
      + '<div class="cm-enabled-field"><input type="checkbox" id="cmCardEnabled"' + (card.enabled === false ? '' : ' checked') + '><label>' + tr('computeMarketCardEnabled') + '</label></div>'
      + '</div>'
      + '<div class="cm-form-gap cm-wide-field"><label>' + tr('computeMarketCardDescription') + '</label><textarea id="cmCardDescription" rows="4" placeholder="' + esc(tr('computeMarketCardDescriptionHint')) + '">' + esc(card.description || '') + '</textarea><div class="hintline">' + esc(tr('computeMarketCardDescriptionHint')) + '</div></div>'
      + '<div class="cm-template-wrap"><label>' + tr('computeMarketCardTemplate') + '</label><div id="cmCardTemplateGrid" class="card-template-grid"></div><input type="hidden" id="cmCardTemplate" value="' + esc(card.template || 'circuit_navy') + '"></div>'
      + '<div class="actions cm-template-actions"><button class="btn-primary" onclick="saveComputeCard()">' + tr('computeMarketSave') + '</button><button class="btn-ghost" onclick="hideComputeCardEditor()">' + tr('computeMarketCancel') + '</button></div>';
    document.getElementById('cmCardEditorContent').innerHTML = html;
    overlay.classList.add('show');
    renderCardTemplateGrid();
  }

  function hideComputeCardEditor() {
    var overlay = document.getElementById('cmCardEditorOverlay');
    if (overlay) overlay.classList.remove('show');
  }

  // Card face template selector with proper SVG artwork (styled like Hub storefront)
  var CARD_TEMPLATES = [
    { id: 'enterprise_monthly_blue', name: '\u4f01\u4e1a\u6708\u5ea6\u84dd', image: '/compute-store/assets/cards/enterprise-monthly-blue.svg', tones: ['#081424', '#123d73', '#0b7768'] },
    { id: 'enterprise_quarter_emerald', name: '\u4f01\u4e1a\u5b63\u5ea6\u7eff', image: '/compute-store/assets/cards/enterprise-quarter-emerald.svg', tones: ['#061a18', '#075f57', '#1f6feb'] },
    { id: 'enterprise_annual_slate', name: '\u4f01\u4e1a\u5e74\u5ea6\u94f6\u7070', image: '/compute-store/assets/cards/enterprise-annual-slate.svg', tones: ['#050816', '#172033', '#334155'] },
    { id: 'enterprise_pool_indigo', name: '\u4f01\u4e1a\u7b97\u529b\u6c60', image: '/compute-store/assets/cards/enterprise-pool-indigo.svg', tones: ['#090b2b', '#233d8f', '#0f766e'] },
    { id: 'enterprise_ha_teal', name: '\u9ad8\u53ef\u7528\u9752\u84dd', image: '/compute-store/assets/cards/enterprise-ha-teal.svg', tones: ['#06121f', '#0b4c6d', '#115e59'] },
    { id: 'circuit_navy', name: '\u6df1\u84dd\u7535\u8def', tones: ['#102a43', '#1f5f99', '#0b7768'] },
    { id: 'emerald_wave', name: '\u7fe1\u7fe0\u6ce2', tones: ['#064e3b', '#047857', '#34d399'] },
    { id: 'slate_tech', name: '\u94f6\u7070\u79d1\u6280', tones: ['#0f172a', '#334155', '#64748b'] }
  ];

  // Generate SVG card art for a template (matching Hub storefront style)
  // Uses a counter suffix to avoid SVG ID collisions when multiple cards share the same template.
  var _svgIdCounter = 0;
  function buildCardTemplateSVG(tmpl, width, height) {
    var w = width || 320, h = height || 190;
    if (tmpl.image) {
      return '<img src="' + esc(tmpl.image) + '" alt="' + esc(tmpl.name || tmpl.id) + '" width="' + w + '" height="' + h + '" class="cm-card-art-img">';
    }
    var id = tmpl.id + '-' + (++_svgIdCounter);
    var t = tmpl.tones;
    return '<svg viewBox="0 0 ' + w + ' ' + h + '" width="' + w + '" height="' + h + '" xmlns="http://www.w3.org/2000/svg">'
      + '<defs>'
      + '<linearGradient id="g-' + id + '" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="' + t[0] + '"/><stop offset=".52" stop-color="' + t[1] + '"/><stop offset="1" stop-color="' + t[2] + '"/></linearGradient>'
      + '<radialGradient id="glow-' + id + '" cx="75%" cy="20%" r="70%"><stop offset="0" stop-color="#fff" stop-opacity=".5"/><stop offset=".38" stop-color="#fff" stop-opacity=".14"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></radialGradient>'
      + '<pattern id="circuit-' + id + '" width="32" height="32" patternUnits="userSpaceOnUse"><path d="M0 16h12M20 16h12M16 0v12M16 20v12" stroke="rgba(255,255,255,.12)" stroke-width="1" fill="none"/><circle cx="16" cy="16" r="1.5" fill="rgba(255,255,255,.25)"/></pattern>'
      + '</defs>'
      + '<rect width="' + w + '" height="' + h + '" fill="url(#g-' + id + ')"/>'
      + '<rect width="' + w + '" height="' + h + '" fill="url(#glow-' + id + ')"/>'
      + '<rect width="' + w + '" height="' + h + '" fill="url(#circuit-' + id + ')"/>'
      + '<path d="M' + Math.round(w*0.1) + ' ' + Math.round(h*0.76) + 'C' + Math.round(w*0.25) + ' ' + Math.round(h*0.55) + ' ' + Math.round(w*0.36) + ' ' + Math.round(h*0.88) + ' ' + Math.round(w*0.5) + ' ' + Math.round(h*0.66) + 'S' + Math.round(w*0.73) + ' ' + Math.round(h*0.34) + ' ' + Math.round(w*0.91) + ' ' + Math.round(h*0.45) + '" fill="none" stroke="#fff" stroke-width="' + Math.max(Math.round(w*0.08),6) + '" stroke-opacity=".07" stroke-linecap="round"/>'
      + '<path d="M' + Math.round(w*0.09) + ' ' + Math.round(h*0.28) + 'h' + Math.round(w*0.18) + 'l' + Math.round(w*0.06) + ' ' + Math.round(h*0.09) + 'h' + Math.round(w*0.17) + 'l' + Math.round(w*0.056) + ' -' + Math.round(h*0.09) + 'h' + Math.round(w*0.23) + '" fill="none" stroke="#fff" stroke-width="1.5" stroke-opacity=".2" stroke-linecap="round"/>'
      + '<path d="M' + Math.round(w*0.17) + ' ' + Math.round(h*0.7) + 'h' + Math.round(w*0.14) + 'l' + Math.round(w*0.056) + ' -' + Math.round(h*0.09) + 'h' + Math.round(w*0.19) + 'l' + Math.round(w*0.075) + ' ' + Math.round(h*0.13) + 'h' + Math.round(w*0.19) + '" fill="none" stroke="#fff" stroke-width="1.5" stroke-opacity=".2" stroke-linecap="round"/>'
      + '<text x="' + Math.round(w*0.06) + '" y="' + Math.round(h*0.19) + '" font-size="' + Math.max(Math.round(w*0.035),8) + '" fill="rgba(255,255,255,0.7)" font-family="sans-serif" font-weight="700">MaClaw Compute</text>'
      + '<text x="' + Math.round(w*0.06) + '" y="' + Math.round(h*0.55) + '" font-size="' + Math.max(Math.round(w*0.056),12) + '" fill="rgba(255,255,255,0.92)" font-family="sans-serif" font-weight="900">CREDITS</text>'
      + '<circle cx="' + Math.round(w*0.84) + '" cy="' + Math.round(h*0.22) + '" r="' + Math.round(w*0.05) + '" fill="none" stroke="rgba(255,255,255,.3)" stroke-width="2"/>'
      + '<circle cx="' + Math.round(w*0.9) + '" cy="' + Math.round(h*0.35) + '" r="' + Math.round(w*0.03) + '" fill="rgba(255,255,255,.12)"/>'
      + '</svg>';
  }

  function renderCardTemplateGrid() {
    var grid = document.getElementById('cmCardTemplateGrid');
    if (!grid) return;
    var current = document.getElementById('cmCardTemplate') && document.getElementById('cmCardTemplate').value || 'circuit_navy';
    grid.innerHTML = CARD_TEMPLATES.map(function(t) {
      var selected = t.id === current;
      return '<div onclick="selectCardTemplate(\'' + t.id + '\')" class="cm-template-tile' + (selected ? ' is-selected' : '') + '">'
        + '<div class="cm-template-preview">'
        + buildCardTemplateSVG(t, 120, 72)
        + '</div>'
        + '<div class="cm-template-name">' + esc(t.name) + '</div>'
        + (selected ? '<div class="cm-template-selected">\u2713 \u5df2\u9009</div>' : '')
        + '</div>';
    }).join('');
  }

  window.selectCardTemplate = function(id) {
    var input = document.getElementById('cmCardTemplate');
    if (input) input.value = id;
    renderCardTemplateGrid();
  };

  async function saveComputeCard() {
    const name = (document.getElementById('cmCardName') || {}).value || '';
    const credits = parseInt((document.getElementById('cmCardCredits') || {}).value || '0', 10);
    const period = (document.getElementById('cmCardPeriod') || {}).value || 'month';
    const price = parseFloat((document.getElementById('cmCardPrice') || {}).value || '0');
    const group = (document.getElementById('cmCardGroup') || {}).value || '';
    const description = ((document.getElementById('cmCardDescription') || {}).value || '').trim();
    const template = (document.getElementById('cmCardTemplate') || {}).value || 'circuit_navy';
    const enabled = !!(document.getElementById('cmCardEnabled') || {}).checked;
    if (!name) { if (window.showToast) showToast(tr('computeMarketCardNameRequired'), 'error'); return; }
    if (!group) { if (window.showToast) showToast(tr('computeMarketCardGroupRequired'), 'error'); return; }
    if (!Number.isFinite(credits) || credits <= 0) { if (window.showToast) showToast(tr('computeMarketCardCreditsRequired'), 'error'); return; }
    if (!Number.isFinite(price) || price <= 0) { if (window.showToast) showToast(tr('computeMarketCardPriceRequired'), 'error'); return; }
    try {
      var payload = { label: name, description: description, credits: credits, period: period, price_rmb: price, service_group_id: group, template: template, enabled: enabled };
      var path = cmEditingCardID ? '/api/admin/cardstore/types/' + encodeURIComponent(cmEditingCardID) : '/api/admin/cardstore/types';
      await api(path, { method: cmEditingCardID ? 'PUT' : 'POST', body: JSON.stringify(payload) });
      hideComputeCardEditor();
      cmEditingCardID = '';
      loadComputeCardTypes();
      if (window.showToast) showToast(tr('computeMarketSaved'), 'success');
    } catch (e) { if (window.showToast) showToast(e.message, 'error'); }
  }

  // ---------------------------------------------------------------------------
  // Orders
  // ---------------------------------------------------------------------------

  async function loadComputeOrders() {
    try {
      updateComputeOrdersArchiveUI();
      const data = await api('/api/admin/cardstore/orders?limit=50' + (cmOrdersArchived ? '&archived=1' : ''));
      renderComputeOrders(data.orders || []);
    } catch (e) { renderComputeOrders([]); }
  }

  function updateComputeOrdersArchiveUI() {
    var btn = document.getElementById('cmArchivedOrdersBtn');
    if (btn) btn.textContent = cmOrdersArchived ? tr('computeMarketViewActiveOrders') : tr('computeMarketViewArchivedOrders');
    var desc = document.getElementById('cmOrdersDesc');
    if (desc) desc.textContent = cmOrdersArchived ? tr('computeMarketArchivedOrdersDesc') : tr('computeMarketOrdersDesc');
  }

  function renderComputeOrders(orders) {
    const container = document.getElementById('cmOrdersList');
    if (!container) return;
    updateComputeOrdersArchiveUI();
    // Update pending badge only when rendering the active list (not archived)
    if (!cmOrdersArchived) {
      var pendingCount = (orders || []).filter(function (o) {
        return CONFIRMABLE_STATUSES.indexOf(o.status) >= 0;
      }).length;
      updatePendingBadge(pendingCount);
    }
    if (!orders.length) { container.innerHTML = '<div class="hint">' + tr('computeMarketNoOrders') + '</div>'; return; }
    container.innerHTML = orders.map(function (o) {
      var statusClass = o.status === 'activated' ? 'ok' : (CONFIRMABLE_STATUSES.indexOf(o.status) >= 0 ? 'warn' : '');
      var confirmBtn = (!cmOrdersArchived && CONFIRMABLE_STATUSES.indexOf(o.status) >= 0) ? '<button class="btn-primary compact-btn" onclick="confirmComputeOrder(\'' + esc(o.order_no) + '\')">' + tr('computeMarketConfirmOrder') + '</button>' : '';
      var archiveBtn = !cmOrdersArchived ? '<button class="btn-ghost compact-btn" onclick="archiveComputeOrder(\'' + esc(o.order_no) + '\')">' + tr('computeMarketArchiveOrder') + '</button>' : '';
      var deleteBtn = (cmOrdersArchived && CONFIRMABLE_STATUSES.indexOf(o.status) >= 0) ? '<button class="btn-danger-ghost compact-btn" onclick="deleteArchivedComputeOrder(\'' + esc(o.order_no) + '\')">' + tr('computeMarketDeleteArchivedOrder') + '</button>' : '';
      var agent = o.agent_name ? ' \u00b7 ' + tr('computeMarketCardAgent') + ': ' + esc(o.agent_name) : '';
      var archivedMeta = cmOrdersArchived && o.archived_at ? ' \u00b7 ' + tr('computeMarketArchivedAt') + ': ' + esc(new Date(o.archived_at).toLocaleString()) : '';
      return '<div class="data-row"><div class="data-row-main"><strong>' + esc(o.order_no) + '</strong> <span class="badge ' + statusClass + '">' + esc(o.status) + '</span><span class="data-row-meta">' + esc(o.email || '') + ' \u00b7 \u00a5' + (o.amount || 0) + ' \u00b7 ' + esc(o.product_label || o.product_id || '') + agent + archivedMeta + '</span></div><div class="data-row-actions">' + confirmBtn + archiveBtn + deleteBtn + '</div></div>';
    }).join('');
  }

  function updatePendingBadge(count) {
    var badge = document.getElementById('cmOrdersPendingBadge');
    if (!badge) return;
    if (count > 0) { badge.textContent = String(count); badge.classList.remove('hidden-view'); }
    else { badge.classList.add('hidden-view'); }
  }

  async function toggleComputeArchivedOrders() {
    cmOrdersArchived = !cmOrdersArchived;
    await loadComputeOrders();
  }

  async function confirmComputeOrder(orderNo) {
    if (!confirm(tr('computeMarketConfirmOrderPrompt'))) return;
    try {
      await api('/api/admin/cardstore/orders/' + encodeURIComponent(orderNo) + '/confirm', { method: 'POST' });
      loadComputeOrders();
      if (window.showToast) showToast(tr('computeMarketOrderConfirmed'), 'success');
    } catch (e) { if (window.showToast) showToast(e.message, 'error'); }
  }

  async function archiveComputeOrder(orderNo) {
    if (!confirm(tr('computeMarketArchiveOrderPrompt'))) return;
    try {
      await api('/api/admin/cardstore/orders/' + encodeURIComponent(orderNo) + '/archive', { method: 'POST' });
      loadComputeOrders();
      if (window.showToast) showToast(tr('computeMarketOrderArchived'), 'success');
    } catch (e) { if (window.showToast) showToast(e.message, 'error'); }
  }

  async function deleteArchivedComputeOrder(orderNo) {
    if (!confirm(tr('computeMarketDeleteArchivedOrderPrompt'))) return;
    try {
      await api('/api/admin/cardstore/orders/' + encodeURIComponent(orderNo), { method: 'DELETE' });
      loadComputeOrders();
      if (window.showToast) showToast(tr('computeMarketOrderDeleted'), 'success');
    } catch (e) { if (window.showToast) showToast(e.message, 'error'); }
  }
  // ---------------------------------------------------------------------------
  // Usage Stats
  // ---------------------------------------------------------------------------

  async function loadStatsFilters() {
    // Populate Hub dropdown from registered hubs
    try {
      var data = await api('/api/admin/hubs');
      var hubs = data.hubs || data || [];
      var hubSelect = document.getElementById('cmStatsHub');
      if (hubSelect && Array.isArray(hubs)) {
        hubSelect.innerHTML = '<option value="">' + tr('computeMarketStatsAllHubs') + '</option>' + hubs.map(function(h) {
          return '<option value="' + esc(h.id || h.hub_id || '') + '">' + esc(h.name || h.id || h.hub_id || '') + '</option>';
        }).join('');
      }
    } catch (e) { /* best-effort */ }

    // Populate Tenant dropdown from compute orders. Compute authorization is
    // managed from Hub node configuration, not from this LLM access page.
    try {
      var data = await api('/api/admin/cardstore/orders?limit=200');
      var orders = data.orders || [];
      var seen = {};
      var tenants = [];
      orders.forEach(function(o) {
        if (!o || !o.tenant_id) return;
        var key = (o.hub_id || '') + '/' + (o.tenant_id || '');
        if (!seen[key]) {
          seen[key] = true;
          tenants.push({ hub_id: o.hub_id, tenant_id: o.tenant_id });
        }
      });
      var tenantSelect = document.getElementById('cmStatsTenant');
      if (tenantSelect) {
        tenantSelect.innerHTML = '<option value="">' + tr('computeMarketStatsAllTenants') + '</option>' + tenants.map(function(t) {
          return '<option value="' + esc(t.tenant_id) + '">' + esc(t.hub_id + ' / ' + t.tenant_id) + '</option>';
        }).join('');
      }
    } catch (e) { /* best-effort */ }
  }

  function formatCMNumber(value) {
    var n = cmNumber(value);
    return Number.isFinite(n) ? n.toLocaleString() : '0';
  }

  function formatCMCredits(value) {
    var n = cmNumber(value);
    return Number.isFinite(n) ? n.toLocaleString(undefined, { maximumFractionDigits: 1 }) : '0';
  }

  function cmNumber(value) {
    var n = Number(value || 0);
    return Number.isFinite(n) ? n : 0;
  }

  function cmText(value) {
    return String(value || '').trim();
  }

  function cmRows(data) {
    var rows = data && (data.rows || data.usage);
    return Array.isArray(rows) ? rows : [];
  }

  function renderCMIdentity(r) {
    r = r || {};
    var hubID = cmText(r.hub_id);
    var tenantID = cmText(r.tenant_id);
    if (!hubID && !tenantID) {
      return '<div class="cm-stats-identity legacy"><strong>' + esc(tr('computeMarketStatsLegacyIdentity')) + '</strong><span>' + esc(tr('computeMarketStatsLegacyHint')) + '</span></div>';
    }
    return '<div class="cm-stats-identity">'
      + '<strong>' + esc(hubID || '-') + '</strong>'
      + '<span>' + esc(tr('computeMarketStatsTenantLabel')) + ': ' + esc(tenantID || '-') + '</span>'
      + '</div>';
  }

  function renderComputeStatsTable(rows) {
    var body = rows.map(function(r) {
      r = r || {};
      return '<tr>'
        + '<td class="cm-stats-scope-cell">' + renderCMIdentity(r) + '</td>'
        + '<td class="cm-stats-period-cell">' + esc(r.period_start || '-') + '</td>'
        + '<td class="num">' + formatCMNumber(r.input_tokens) + '</td>'
        + '<td class="num">' + formatCMNumber(r.output_tokens) + '</td>'
        + '<td class="num">' + formatCMCredits(r.total_credits) + '</td>'
        + '<td class="num strong">' + formatCMNumber(r.total_requests) + '</td>'
        + '</tr>';
    }).join('');
    return '<div class="cm-stats-table-shell">'
      + '<table class="data-table cm-data-table" aria-label="' + esc(tr('computeMarketStatsTitle')) + '"><colgroup><col class="cm-col-scope"><col class="cm-col-period"><col class="cm-col-number"><col class="cm-col-number"><col class="cm-col-number"><col class="cm-col-number"></colgroup><thead><tr>'
      + '<th>' + tr('computeMarketStatsIdentity') + '</th>'
      + '<th>' + tr('computeMarketStatsPeriodStart') + '</th>'
      + '<th class="num">' + tr('computeMarketStatsTotalInput') + '</th>'
      + '<th class="num">' + tr('computeMarketStatsTotalOutput') + '</th>'
      + '<th class="num">' + tr('computeMarketStatsTotalCredits') + '</th>'
      + '<th class="num">' + tr('computeMarketStatsTotalRequests') + '</th>'
      + '</tr></thead><tbody>' + body + '</tbody></table></div>';
  }

  async function queryComputeStats() {
    const hub = (document.getElementById('cmStatsHub') || {}).value || '';
    const tenant = (document.getElementById('cmStatsTenant') || {}).value || '';
    const period = (document.getElementById('cmStatsPeriod') || {}).value || 'daily';
    const start = (document.getElementById('cmStatsStart') || {}).value || '';
    const end = (document.getElementById('cmStatsEnd') || {}).value || '';
    const container = document.getElementById('cmStatsResult');
    const summaryEl = document.getElementById('cmStatsSummary');
    if (!container) return;
    try {
      var params = new URLSearchParams({ period: period });
      if (hub) params.set('hub_id', hub);
      if (tenant) params.set('tenant_id', tenant);
      if (start) params.set('start', start);
      if (end) params.set('end', end);
      const data = await api('/api/admin/llm/usage?' + params.toString());
      const rows = cmRows(data);
      if (!rows.length) { container.innerHTML = '<div class="hint">' + tr('computeMarketStatsEmpty') + '</div>'; if (summaryEl) summaryEl.innerHTML = ''; return; }
      // Summary
      var totalInput = 0, totalOutput = 0, totalCredits = 0, totalReqs = 0, totalCacheHits = 0;
      rows.forEach(function(r) {
        r = r || {};
        totalInput += cmNumber(r.input_tokens);
        totalOutput += cmNumber(r.output_tokens);
        totalCredits += cmNumber(r.total_credits);
        totalReqs += cmNumber(r.total_requests);
        totalCacheHits += cmNumber(r.cache_hits);
      });
      if (summaryEl) {
        summaryEl.innerHTML = '<div class="metric"><label>' + tr('computeMarketStatsRows') + '</label><strong>' + formatCMNumber(rows.length) + '</strong></div>'
          + '<div class="metric"><label>' + tr('computeMarketStatsTotalInput') + '</label><strong>' + formatCMNumber(totalInput) + '</strong></div>'
          + '<div class="metric"><label>' + tr('computeMarketStatsTotalOutput') + '</label><strong>' + formatCMNumber(totalOutput) + '</strong></div>'
          + '<div class="metric"><label>' + tr('computeMarketStatsTotalCredits') + '</label><strong>' + formatCMCredits(totalCredits) + '</strong></div>'
          + '<div class="metric"><label>' + tr('computeMarketStatsTotalRequests') + '</label><strong>' + formatCMNumber(totalReqs) + '</strong></div>'
          + '<div class="metric"><label>' + tr('computeMarketStatsCacheHitRate') + '</label><strong>' + (totalReqs > 0 ? ((totalCacheHits*100/totalReqs).toFixed(1) + '%') : '0%') + '</strong></div>';
      }
      container.innerHTML = renderComputeStatsTable(rows);
    } catch (e) { container.innerHTML = '<div class="hint">' + tr('computeMarketStatsError') + ': ' + esc(e.message) + '</div>'; }
  }

  // ---------------------------------------------------------------------------
  // Tab init
  // ---------------------------------------------------------------------------

  function applyComputeMarketPlaceholders() {
    var zh = (document.documentElement.lang || '').toLowerCase().startsWith('zh');
    var values = {
      cmPaymentInstruction: zh ? '\u626b\u7801\u652f\u4ed8\u540e\u8bf7\u7b49\u5f85\u7ba1\u7406\u5458\u786e\u8ba4\uff0c\u901a\u5e38 1-24 \u5c0f\u65f6\u5185\u5904\u7406\u3002' : 'After scanning to pay, wait for admin confirmation. Processing usually takes 1-24 hours.',
      cmPaymentAlipayPayee: zh ? '\u6536\u6b3e\u4eba\u59d3\u540d' : 'Payee name',
      cmPaymentWechatPayee: zh ? '\u6536\u6b3e\u4eba\u59d3\u540d' : 'Payee name',
      cmPaymentBankName: zh ? '\u4e2d\u56fd\u94f6\u884c' : 'Bank of China',
      cmPaymentBankHolder: zh ? '\u5f20\u4e09' : 'Account holder'
    };
    Object.keys(values).forEach(function(id) {
      var el = document.getElementById(id);
      if (el) el.placeholder = values[id];
    });
  }

  async function initComputeMarketTab() {
    if (cmInitInFlight) return cmInitInFlight;
    cmInitInFlight = (async function () {
    // Re-apply i18n for dynamically registered keys
    if (typeof applyI18n === 'function') applyI18n();
    applyComputeMarketPlaceholders();
    loadComputeCardTypes();
    loadStatsFilters();
    // Ensure payment mode UI is correct on first load
    window.updateCMPaymentModeUI();
    // Update summary counts
    try {
      const pd = await api('/api/admin/llm/providers').catch(function () { return { providers: [] }; });
      const gd = await api('/api/admin/llm/service-groups').catch(function () { return { service_groups: [] }; });
      const odSummary = await api('/api/admin/cardstore/orders?limit=50').catch(function () { return { orders: [] }; });
      var el1 = document.getElementById('cmProviderCount'); if (el1) el1.textContent = String((pd.providers || []).length);
      var el2 = document.getElementById('cmGroupCount'); if (el2) el2.textContent = String((gd.service_groups || []).length);
      var el3 = document.getElementById('cmPendingOrderCount'); if (el3) el3.textContent = String((odSummary.orders || []).filter(function (o) { return CONFIRMABLE_STATUSES.indexOf(o.status) >= 0; }).length);
    } catch (e) { /* best-effort */ }
    // Pre-check for pending orders and auto-switch to Orders tab if any exist
    try {
      const od = await api('/api/admin/cardstore/orders?limit=50');
      var allOrders = od.orders || [];
      var pendingCount = allOrders.filter(function (o) {
        return CONFIRMABLE_STATUSES.indexOf(o.status) >= 0;
      }).length;
      updatePendingBadge(pendingCount);
      if (pendingCount > 0) {
        switchComputeMarketSubTab('orders', allOrders);
      }
    } catch (e) { /* best-effort */ }
    })();
    try { return await cmInitInFlight; }
    finally { cmInitInFlight = null; }
  }

  function tr(key) {
    if (window.I18N) {
      var lang = (document.documentElement.lang || '').startsWith('zh') ? 'zh' : 'en';
      return (window.I18N[lang] || {})[key] || (window.I18N.en || {})[key] || key;
    }
    return key;
  }

  // Expose to global scope
  window.initComputeMarketTab = initComputeMarketTab;
  window.switchComputeMarketSubTab = switchComputeMarketSubTab;
  window.showComputeCardEditor = showComputeCardEditor;
  window.editComputeCard = editComputeCard;
  window.hideComputeCardEditor = hideComputeCardEditor;
  window.saveComputeCard = saveComputeCard;
  window.loadComputeOrders = loadComputeOrders;
  window.confirmComputeOrder = confirmComputeOrder;
  window.archiveComputeOrder = archiveComputeOrder;
  window.deleteArchivedComputeOrder = deleteArchivedComputeOrder;
  window.toggleComputeArchivedOrders = toggleComputeArchivedOrders;
  window.queryComputeStats = queryComputeStats;
  window.loadStatsFilters = loadStatsFilters;
  window.CARD_TEMPLATES = CARD_TEMPLATES;
  window.buildCardTemplateSVG = buildCardTemplateSVG;

  if (document.getElementById('tab-computemarket')?.classList.contains('active')) {
    setTimeout(initComputeMarketTab, 0);
  }

  // --- Payment Settings (Hub-style per-field visibility) ---
  var cmManualFields = ['cmPF_adminEmails','cmPF_instruction','cmPF_alipayQR','cmPF_wechatQR','cmPF_bankName','cmPF_bankAccount','cmPF_bankHolder','cmPF_contact'];
  var cmAlipayFields = ['cmPF_alipayAppID','cmPF_alipayGateway','cmPF_alipayPrivateKey','cmPF_alipayPublicKey','cmPF_alipayNotifyURL','cmPF_alipaySubject'];

  window.updateCMPaymentModeUI = function() {
    var mode = (document.getElementById('cmPaymentMode') || {}).value || 'personal_semimanual';
    var useManual = mode === 'personal_semimanual';
    var useAlipay = mode === 'alipay_direct';
    cmManualFields.forEach(function(id) { var el = document.getElementById(id); if (el) el.style.display = useManual ? '' : 'none'; });
    cmAlipayFields.forEach(function(id) { var el = document.getElementById(id); if (el) el.style.display = useAlipay ? '' : 'none'; });
  };

  window.saveCMPaymentConfig = async function() {
    var mode = (document.getElementById('cmPaymentMode') || {}).value || 'personal_semimanual';
    var payload = {
      payment_mode: mode,
      personal_payment: {
        admin_emails: (document.getElementById('cmPaymentAdminEmails') || {}).value ? (document.getElementById('cmPaymentAdminEmails').value).split(/[,;\s]+/).filter(Boolean) : [],
        instruction: (document.getElementById('cmPaymentInstruction') || {}).value || '',
        channels: [
          { id: 'alipay', enabled: !!(document.getElementById('cmPaymentAlipayEnabled') || {}).checked, payee: (document.getElementById('cmPaymentAlipayPayee') || {}).value || '', image_url: (document.getElementById('cmPaymentAlipayQR') || {}).value || '' },
          { id: 'wechat', enabled: !!(document.getElementById('cmPaymentWechatEnabled') || {}).checked, payee: (document.getElementById('cmPaymentWechatPayee') || {}).value || '', image_url: (document.getElementById('cmPaymentWechatQR') || {}).value || '' },
          { id: 'bank_transfer', enabled: true, bank_name: (document.getElementById('cmPaymentBankName') || {}).value || '', bank_account: (document.getElementById('cmPaymentBankAccount') || {}).value || '', bank_holder: (document.getElementById('cmPaymentBankHolder') || {}).value || '', contact_info: (document.getElementById('cmPaymentContact') || {}).value || '' }
        ]
      },
      alipay_direct: {
        app_id: (document.getElementById('cmPaymentAlipayAppID') || {}).value || '',
        gateway_url: (document.getElementById('cmPaymentAlipayGateway') || {}).value || '',
        private_key: (document.getElementById('cmPaymentAlipayPrivateKey') || {}).value || '',
        alipay_public_key: (document.getElementById('cmPaymentAlipayPublicKey') || {}).value || '',
        notify_url: (document.getElementById('cmPaymentAlipayNotifyURL') || {}).value || '',
        subject_prefix: (document.getElementById('cmPaymentAlipaySubject') || {}).value || ''
      }
    };
    try {
      await api('/api/admin/llm/payment-config', { method: 'PUT', body: JSON.stringify(payload) });
      if (window.showToast) window.showToast(tr('computeMarketSaved'), 'success');
    } catch (e) { if (window.showToast) window.showToast(e.message, 'error'); else alert(e.message); }
  };

  // --- QR Code Upload (matches Hub's uploadCardStoreQR pattern) ---
  window.uploadCMPaymentQR = function(channel) {
    var prefix = channel === 'wechat' ? 'cmPaymentWechat' : 'cmPaymentAlipay';
    var fileInput = document.getElementById(prefix + 'QRFile');
    var hiddenInput = document.getElementById(prefix + 'QR');
    var preview = document.getElementById(prefix + 'QRPreview');
    if (!fileInput || !fileInput.files || !fileInput.files[0]) return;
    var file = fileInput.files[0];
    if (file.size > 2 * 1024 * 1024) {
      if (window.showToast) window.showToast('Image must be under 2MB', 'error');
      return;
    }
    // Convert to base64 data URL for storage (same as Hub's approach for small QR images)
    var reader = new FileReader();
    reader.onload = function(e) {
      var dataURL = e.target.result;
      if (hiddenInput) hiddenInput.value = dataURL;
      if (preview) { preview.src = dataURL; preview.style.display = 'block'; }
    };
    reader.readAsDataURL(file);
  };

  // --- Load existing payment config and populate form ---
  window.loadCMPaymentConfig = async function() {
    try {
      var data = await api('/api/admin/llm/payment-config');
      var cfg = data || {};
      var modeEl = document.getElementById('cmPaymentMode');
      if (modeEl && cfg.payment_mode) modeEl.value = cfg.payment_mode;
      if (cfg.personal_payment) {
        var pp = cfg.personal_payment;
        var adminEmailsEl = document.getElementById('cmPaymentAdminEmails');
        if (adminEmailsEl) adminEmailsEl.value = (pp.admin_emails || []).join(', ');
        var instrEl = document.getElementById('cmPaymentInstruction');
        if (instrEl) instrEl.value = pp.instruction || '';
        var channels = pp.channels || [];
        var alipay = channels.find(function(c){return c.id==='alipay';}) || {};
        var wechat = channels.find(function(c){return c.id==='wechat';}) || {};
        var bank = channels.find(function(c){return c.id==='bank_transfer';}) || {};
        var alipayEn = document.getElementById('cmPaymentAlipayEnabled');
        if (alipayEn) alipayEn.checked = !!alipay.enabled;
        var alipayPayee = document.getElementById('cmPaymentAlipayPayee');
        if (alipayPayee) alipayPayee.value = alipay.payee || '';
        var alipayQR = document.getElementById('cmPaymentAlipayQR');
        if (alipayQR) alipayQR.value = alipay.image_url || '';
        var alipayPrev = document.getElementById('cmPaymentAlipayQRPreview');
        if (alipayPrev && alipay.image_url) { alipayPrev.src = alipay.image_url; alipayPrev.style.display = 'block'; }
        var wechatEn = document.getElementById('cmPaymentWechatEnabled');
        if (wechatEn) wechatEn.checked = !!wechat.enabled;
        var wechatPayee = document.getElementById('cmPaymentWechatPayee');
        if (wechatPayee) wechatPayee.value = wechat.payee || '';
        var wechatQR = document.getElementById('cmPaymentWechatQR');
        if (wechatQR) wechatQR.value = wechat.image_url || '';
        var wechatPrev = document.getElementById('cmPaymentWechatQRPreview');
        if (wechatPrev && wechat.image_url) { wechatPrev.src = wechat.image_url; wechatPrev.style.display = 'block'; }
        var bankNameEl = document.getElementById('cmPaymentBankName');
        if (bankNameEl) bankNameEl.value = bank.bank_name || '';
        var bankAcctEl = document.getElementById('cmPaymentBankAccount');
        if (bankAcctEl) bankAcctEl.value = bank.bank_account || '';
        var bankHolderEl = document.getElementById('cmPaymentBankHolder');
        if (bankHolderEl) bankHolderEl.value = bank.bank_holder || '';
        var contactEl = document.getElementById('cmPaymentContact');
        if (contactEl) contactEl.value = bank.contact_info || '';
      }
      if (cfg.alipay_direct) {
        var ad = cfg.alipay_direct;
        var fields = {cmPaymentAlipayAppID:'app_id',cmPaymentAlipayGateway:'gateway_url',cmPaymentAlipayPrivateKey:'private_key',cmPaymentAlipayPublicKey:'alipay_public_key',cmPaymentAlipayNotifyURL:'notify_url',cmPaymentAlipaySubject:'subject_prefix'};
        Object.keys(fields).forEach(function(elId){var el=document.getElementById(elId);if(el)el.value=ad[fields[elId]]||el.value||'';});
      }
      window.updateCMPaymentModeUI();
    } catch(e) { /* best-effort on load */ }
  };
})();
