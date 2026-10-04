/*
 * Device <-> brain link health tab (plan N0-3). ASCII only; Chinese is
 * represented through Unicode escapes because the admin static bundle enforces
 * ASCII source.
 *
 * Answers the three questions the companion-terminal plan needs before any
 * offline-resilience work can be judged: how often a paired MaClaw GUI was
 * actually reachable, how often the Hub answered 503 gui_offline, and what
 * happened to the device events that reached the Hub.
 */
(function(global) {
  function zh(en, cn) { return String(global.currentLang || 'en') === 'zh' ? cn : en; }
  function $id(id) { return document.getElementById(id); }
  function esc(v) { return typeof global.escapeHtml === 'function' ? global.escapeHtml(v == null ? '' : String(v)) : String(v == null ? '' : v); }
  function api(path, opts) { return global.api(path, opts); }
  function notify(message, kind) { if (typeof global.showToast === 'function') global.showToast(message, kind || 'info'); }

  var state = { loading: false, snapshot: null, error: '' };

  function pct(value) {
    var n = Number(value);
    if (!isFinite(n)) return '-';
    return (n * 100).toFixed(1) + '%';
  }
  function num(value) { return new Intl.NumberFormat().format(Number(value || 0)); }
  function duration(value) {
    var s = Math.max(0, Math.floor(Number(value || 0)));
    if (s < 60) return s + 's';
    var m = Math.floor(s / 60);
    if (m < 60) return m + 'm';
    var h = Math.floor(m / 60);
    if (h < 24) return h + 'h ' + (m % 60) + 'm';
    return Math.floor(h / 24) + 'd ' + (h % 24) + 'h';
  }

  function T() {
    return {
      title: zh('Link health', '\u94fe\u8def\u5065\u5eb7\u5ea6'),
      subtitle: zh('How often the paired MaClaw GUI was reachable, and what happened to device events.',
        '\u914d\u5bf9\u7684 MaClaw GUI \u6709\u591a\u4e45\u662f\u53ef\u8fde\u7684\uff0c\u4ee5\u53ca\u8bbe\u5907\u4e8b\u4ef6\u6700\u7ec8\u53bb\u4e86\u54ea\u91cc\u3002'),
      nav: zh('Link health', '\u94fe\u8def\u5065\u5eb7\u5ea6'),
      navDesc: zh('Brain reachability', '\u5927\u8111\u53ef\u8fbe\u6027'),
      refresh: zh('Refresh', '\u5237\u65b0'),
      loading: zh('Loading...', '\u52a0\u8f7d\u4e2d...'),
      empty: zh('No observations yet. Link health fills in as devices talk to the Hub.',
        '\u6682\u65e0\u89c2\u6d4b\u6570\u636e\u3002\u8bbe\u5907\u4e0e Hub \u901a\u4fe1\u540e\u5373\u4f1a\u51fa\u73b0\u3002'),
      fleetTitle: zh('Fleet summary', '\u6574\u4f53\u6982\u89c8'),
      fleetDesc: zh('Aggregate across every tenant this Hub serves.',
        '\u672c Hub \u6240\u6709\u79df\u6237\u7684\u6c47\u603b\u3002'),
      tenantsTitle: zh('Per-tenant detail', '\u5206\u79df\u6237\u660e\u7ec6'),
      tenantsDesc: zh('A single blended number would hide one tenant being offline the whole time.',
        '\u53ea\u770b\u6c47\u603b\u6570\u4f1a\u63a9\u76d6\u67d0\u4e2a\u79df\u6237\u957f\u671f\u79bb\u7ebf\u3002'),
      onlineRatio: zh('GUI online ratio', 'GUI \u5728\u7ebf\u7387'),
      onlineRatioHint: zh('Share of the observed window the brain was connected.',
        '\u89c2\u6d4b\u7a97\u53e3\u5185\u5927\u8111\u5904\u4e8e\u8fde\u63a5\u72b6\u6001\u7684\u5360\u6bd4\u3002'),
      offlineRejections: zh('gui_offline rejections', 'gui_offline \u62d2\u7edd\u6b21\u6570'),
      offlineRejectionsHint: zh('Requests refused because no GUI was connected (F3).',
        '\u56e0\u65e0 GUI \u8fde\u63a5\u800c\u88ab\u62d2\u7684\u8bf7\u6c42\u6570\uff08F3\uff09\u3002'),
      eventSuccess: zh('Event delivery success', '\u4e8b\u4ef6\u6295\u9012\u6210\u529f\u7387'),
      eventSuccessHint: zh('Accepted or replayed, over everything that reached the Hub.',
        '\u5df2\u63a5\u6536\u6216\u91cd\u653e\u5360\u5230\u8fbe Hub \u7684\u5168\u90e8\u4e8b\u4ef6\u7684\u6bd4\u4f8b\u3002'),
      observed: zh('Observed window', '\u89c2\u6d4b\u65f6\u957f'),
      observedHint: zh('Time since this Hub process started counting.',
        '\u672c Hub \u8fdb\u7a0b\u5f00\u59cb\u8ba1\u6570\u4ee5\u6765\u7684\u65f6\u957f\u3002'),
      onlineNow: zh('Online now', '\u5f53\u524d\u5728\u7ebf'),
      onlineNowHint: zh('Tenants whose GUI currently holds the gateway.',
        '\u5f53\u524d\u6301\u6709\u7f51\u5173\u7684\u79df\u6237\u6570\u3002'),
      tenant: zh('Tenant', '\u79df\u6237'),
      online: zh('GUI online', 'GUI \u5728\u7ebf'),
      onlineTime: zh('Online time', '\u5728\u7ebf\u65f6\u957f'),
      offlineRej: zh('Offline rejections', '\u79bb\u7ebf\u62d2\u7edd'),
      accepted: zh('Accepted', '\u5df2\u63a5\u6536'),
      duplicate: zh('Replay', '\u91cd\u653e'),
      rejected: zh('Rejected', '\u88ab\u62d2'),
      yes: zh('yes', '\u662f'),
      no: zh('no', '\u5426'),
      rejectReasons: zh('Rejection reasons', '\u62d2\u7edd\u539f\u56e0'),
      hint: zh('gui_offline means the device had no brain to talk to. Replay counts as a delivery success: the device only retries when it never saw our response.',
        'gui_offline \u8868\u793a\u8bbe\u5907\u65e0\u5927\u8111\u53ef\u8bf4\u8bdd\u3002\u91cd\u653e\u8ba1\u4e3a\u6295\u9012\u6210\u529f\uff1a\u8bbe\u5907\u53ea\u5728\u6ca1\u6536\u5230\u6211\u4eec\u54cd\u5e94\u65f6\u624d\u4f1a\u91cd\u8bd5\u3002'),
      loadFailed: zh('Failed to load link health', '\u52a0\u8f7d\u94fe\u8def\u5065\u5eb7\u5ea6\u5931\u8d25'),
      sessions: zh('claims', '\u6b21\u63a5\u7ba1')
    };
  }

  function card(label, value, hint) {
    return '<div class="lh-card"><div class="lh-label">' + esc(label) +
      '</div><div class="lh-value">' + esc(value) +
      '</div><div class="lh-note">' + esc(hint) + '</div></div>';
  }

  function renderRejectReasons(reasons, t) {
    var keys = Object.keys(reasons || {});
    if (!keys.length) return '';
    keys.sort(function(a, b) { return Number(reasons[b] || 0) - Number(reasons[a] || 0); });
    var rows = keys.map(function(key) {
      return '<span class="lh-chip">' + esc(key) + ' <b>' + esc(num(reasons[key])) + '</b></span>';
    }).join('');
    return '<div class="lh-reasons"><div class="lh-label">' + esc(t.rejectReasons) +
      '</div><div class="lh-chips">' + rows + '</div></div>';
  }

  function renderTenants(tenants, t) {
    if (!tenants || !tenants.length) {
      return '<p class="hint">' + esc(t.empty) + '</p>';
    }
    var head = '<tr><th>' + esc(t.tenant) + '</th><th>' + esc(t.online) + '</th><th>' +
      esc(t.onlineRatio) + '</th><th>' + esc(t.onlineTime) + '</th><th>' +
      esc(t.offlineRej) + '</th><th>' + esc(t.accepted) + '</th><th>' +
      esc(t.duplicate) + '</th><th>' + esc(t.rejected) + '</th></tr>';
    var body = tenants.map(function(tenant) {
      var badge = tenant.gui_online
        ? '<span class="lh-on">' + esc(t.yes) + '</span>'
        : '<span class="lh-off">' + esc(t.no) + '</span>';
      return '<tr><td>' + esc(tenant.tenant_id) + '</td><td>' + badge + '</td><td>' +
        esc(pct(tenant.online_ratio)) + '</td><td>' + esc(duration(tenant.online_seconds)) +
        '</td><td>' + esc(num(tenant.offline_rejections)) + '</td><td>' +
        esc(num(tenant.events_accepted)) + '</td><td>' + esc(num(tenant.events_duplicate)) +
        '</td><td>' + esc(num(tenant.events_rejected)) + '</td></tr>';
    }).join('');
    return '<table class="lh-table"><thead>' + head + '</thead><tbody>' + body + '</tbody></table>';
  }

  function render() {
    var container = $id('linkHealthBody');
    if (!container) return;
    var t = T();
    applyText(t);

    if (state.loading) {
      container.innerHTML = '<p class="hint">' + esc(t.loading) + '</p>';
      return;
    }
    if (state.error) {
      container.innerHTML = '<p class="error">' + esc(state.error) + '</p>';
      return;
    }
    var snap = state.snapshot;
    if (!snap) {
      container.innerHTML = '<p class="hint">' + esc(t.empty) + '</p>';
      return;
    }

    var fleet = '<div class="lh-grid">' +
      card(t.onlineRatio, pct(snap.gui_online_ratio), t.onlineRatioHint) +
      card(t.offlineRejections, num(snap.gui_offline_rejections), t.offlineRejectionsHint) +
      card(t.eventSuccess, pct(snap.device_event_success_ratio), t.eventSuccessHint) +
      card(t.onlineNow, num(snap.gui_online_tenants), t.onlineNowHint) +
      card(t.observed, duration(snap.observed_seconds), t.observedHint) +
      '</div>' + renderRejectReasons(snap.device_event_rejected_by_reason, t);

    container.innerHTML =
      '<h3>' + esc(t.fleetTitle) + '</h3><p class="hint">' + esc(t.fleetDesc) + '</p>' +
      fleet +
      '<h3>' + esc(t.tenantsTitle) + '</h3><p class="hint">' + esc(t.tenantsDesc) + '</p>' +
      renderTenants(snap.tenants, t) +
      '<p class="hint lh-hint">' + esc(t.hint) + '</p>';
  }

  function applyText(t) {
    var labels = t || T();
    var el = $id('linkHealthTitle');
    if (el) el.textContent = labels.title;
    var sub = $id('linkHealthSubtitle');
    if (sub) sub.textContent = labels.subtitle;
    var nav = $id('navLinkHealth');
    if (nav) nav.textContent = labels.nav;
    var navDesc = $id('navLinkHealthDesc');
    if (navDesc) navDesc.textContent = labels.navDesc;
    var refresh = $id('linkHealthRefresh');
    if (refresh) refresh.textContent = labels.refresh;
  }

  function loadLinkHealth() {
    state.loading = true;
    state.error = '';
    render();
    return api('/api/admin/link-health/metrics').then(function(data) {
      state.snapshot = data || null;
      state.loading = false;
      render();
      return data;
    }).catch(function(err) {
      state.loading = false;
      state.error = (err && err.message) ? err.message : T().loadFailed;
      render();
      notify(state.error, 'error');
      throw err;
    });
  }

  function registerLinkHealthTab() {
    if (!global.AdminTabRegistry || typeof global.AdminTabRegistry.registerTab !== 'function') return;
    global.AdminTabRegistry.registerTab({
      id: 'linkhealth',
      title: function() { return T().title; },
      subtitle: function() { return T().subtitle; },
      onOpen: function() { loadLinkHealth(); }
    });
  }

  // Link health is a cross-tenant fleet view (the Hub endpoint is guarded by
  // requireGlobalAdmin), so a tenant admin must never be offered the tab.
  global.adminGlobalOnlyTabs = Object.assign({}, global.adminGlobalOnlyTabs || {}, { linkhealth: true });

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() { render(); });
  }
  global.loadLinkHealth = loadLinkHealth;
  registerLinkHealthTab();
  applyText();
})(window);
