// Token Bank admin tab (design doc §6.2).
//
// The tab is a view over endpoints that already exist; nothing here computes a
// price. That is deliberate — the fee rate, the price book and the tier grade
// are money, and money has exactly one implementation, in the Go store. The UI
// collects a value, sends it, and then re-reads what the server accepted, so a
// clamped or repaired setting is what the operator sees rather than what they
// typed.
//
// Layout follows the llmservice tab: a horizontal sub-tab strip over one panel,
// with each sub-view rendering into its own container.
(function () {
  'use strict';

  function t(key, fallback) {
    var lang = (window.currentLang || 'en').startsWith('zh') ? 'zh' : 'en';
    var table = TBK_I18N[lang] || TBK_I18N.en;
    return table[key] || TBK_I18N.en[key] || fallback || key;
  }
  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }
  // Escape for embedding inside a single-quoted JS string in an onclick
  // attribute. JSON.stringify already quotes and escapes, but the result still
  // has to survive HTML attribute parsing, so quotes are escaped too.
  function jsArg(s) { return esc(JSON.stringify(String(s == null ? '' : s))); }
  function num(v, digits) {
    var n = Number(v);
    if (!isFinite(n)) return '0';
    return n.toLocaleString(undefined, { maximumFractionDigits: digits == null ? 2 : digits });
  }
  // Credits are stored as integer microcredits. Every credit figure the operator
  // sees goes through here, so the 1e6 scale appears once rather than at each
  // of the twenty places a number is printed.
  function creditsFixed(micro, digits) { return num(Number(micro || 0) / 1e6, digits == null ? 2 : digits); }
  function pct(rate) { return num(Number(rate || 0) * 100, 2) + '%'; }
  // Available is a boolean. Printing it with String() shows "true"/"false" in
  // the Chinese UI. The tier buttons stay on the wire values low/mid/high.
  function yesNo(v) { return t(v ? 'tbkYes' : 'tbkNo'); }
  // Never format a value that goes back into a control: `num()` inserts locale
  // thousands separators, and separators are invalid inside <input type=number>.
  // A separator that reaches an input makes the browser render it empty, so the
  // admin sees a blank field for a setting that does have a value.
  function rawNum(v, fallback) {
    var n = Number(v);
    if (!isFinite(n)) return fallback == null ? '' : String(fallback);
    return String(n);
  }
  function fmtTime(v) {
    if (!v) return '—';
    var d = new Date(v);
    if (isNaN(d.getTime())) return String(v);
    return d.toLocaleString();
  }

  var state = {
    sub: 'overview',
    settings: null,
    overview: null,
    overviewFeeRate: null,
    overviewCreditShareMaxRatio: null,
    users: { page: 0, size: 20, rows: [], total: null },
    shares: { page: 0, size: 20, rows: [], status: '', owner: '' },
    price: { rows: [], editing: null, draft: null },
    creditShares: { rows: [], page: 0, size: 20, more: false },
    margins: { rows: [], days: 30, group: 'model', page: 0 },
    leaders: [],
    loadSeq: 0,
    // Bumps only when a settings PUT is accepted. A settings GET that started
    // earlier must not paint the pre-save form over that result: the operator
    // would otherwise save the old numbers and undo the write.
    settingsEpoch: 0,
    busy: {}
  };

  // --- HTTP -----------------------------------------------------------------
  // window.api is installed by admin-core and already attaches the admin bearer
  // token and normalises error shapes, so this layer only has to shape results.
  async function apiGet(path) {
    var res = await window.api(path, { method: 'GET' });
    return res || {};
  }
  async function apiSend(path, method, body) {
    var res = await window.api(path, { method: method, body: body === undefined ? undefined : JSON.stringify(body) });
    return res || {};
  }

  function toast(msg, kind) {
    if (typeof window.showToast === 'function') { window.showToast(msg, kind || 'info'); return; }
    var out = document.getElementById('output');
    if (out) out.textContent = String(msg);
  }

  function busy(key, on) {
    state.busy[key] = !!on;
  }
  function isBusy(key) { return !!state.busy[key]; }
  function disableWhileBusy(btn, key) {
    if (!btn) return;
    btn.disabled = isBusy(key);
    btn.setAttribute('aria-busy', isBusy(key) ? 'true' : 'false');
  }

  // One generation for every read on this tab. A slower response must not paint
  // over a newer page, filter, or save, and its error must not replace that
  // newer paint. Switching sub-tabs bumps the same counter: the hidden view's
  // late response is dropped, and the visible load owns the screen.
  function noteLoad() { return ++state.loadSeq; }
  function loadLive(seq) { return seq === state.loadSeq; }
  function loadErrorText(err) {
    var msg = String(err && err.message ? err.message : (err == null ? '' : err));
    // A reverse proxy can return a full HTML status page. Dumping that into the
    // card hides the failure; keep a short line the operator can read. A long
    // plain API error stays intact.
    if (/<\s*html[\s>]/i.test(msg) || /<\s*title[\s>]/i.test(msg) || /<\s*body[\s>]/i.test(msg)) {
      msg = msg.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim().slice(0, 180);
    }
    return t('tbkLoadFailed', 'Failed: ') + msg;
  }
  function showLoadError(seq, root, err) {
    if (!loadLive(seq) || !root) return;
    root.innerHTML = '<div class="hint">' + esc(loadErrorText(err)) + '</div>';
  }

  // --- chrome ---------------------------------------------------------------
  var SUBS = ['overview', 'settings', 'users', 'shares', 'price', 'creditshares', 'margins', 'leaderboard'];

  function switchTokenBankSubTab(name) {
    if (SUBS.indexOf(name) < 0) name = 'overview';
    state.sub = name;
    SUBS.forEach(function (key) {
      var btn = document.getElementById('tbkSubTab' + cap(key));
      var view = document.getElementById('tbkSubView' + cap(key));
      var active = key === name;
      if (btn) {
        btn.className = active ? 'btn-secondary' : 'btn-ghost';
        btn.setAttribute('aria-selected', active ? 'true' : 'false');
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
        btn.tabIndex = active ? 0 : -1;
      }
      if (view) {
        // .hidden-view is display:none. Clearing an inline style leaves that
        // class in place, so every sub-tab except 总览 rendered as an empty panel.
        view.classList.toggle('hidden-view', !active);
        view.style.display = '';
      }
    });
    applyTokenBankI18n();
    loadTokenBankSubView(name);
  }
  function cap(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

  function loadTokenBankSubView(name) {
    switch (name) {
      case 'overview': return loadTokenBankOverview();
      case 'settings': return loadTokenBankSettings();
      // Do not pass 0. That reset 分享用户 to the first page on every visit,
      // including a return from another sub-tab. Shares already keeps its page.
      case 'users': return loadTokenBankUsers();
      case 'shares': return loadTokenBankShares();
      case 'price': return loadTokenBankPriceBook();
      case 'creditshares': return loadTokenBankCreditShares();
      case 'margins': return loadTokenBankMargins();
      case 'leaderboard': return loadTokenBankLeaderboard();
    }
  }

  // Set when any init has started a sub-view load. openTab can call init once
  // this file has assigned it, and the startup resume below can call it too.
  // The flag keeps those two startup paths from each firing a request.
  var startupLoadStarted = false;
  function initTokenBankTab() {
    startupLoadStarted = true;
    // switchTokenBankSubTab applies the tab dictionary itself.
    switchTokenBankSubTab(state.sub || 'overview');
  }

  // --- i18n -----------------------------------------------------------------
  function applyTokenBankI18n() {
    var nodes = document.querySelectorAll('#tab-tokenbank [data-tbk-i18n]');
    for (var i = 0; i < nodes.length; i++) {
      var key = nodes[i].getAttribute('data-tbk-i18n');
      var text = t(key);
      if (nodes[i].tagName === 'INPUT' || nodes[i].tagName === 'TEXTAREA') {
        nodes[i].placeholder = text;
      } else {
        nodes[i].textContent = text;
      }
    }
  }

  // --- overview -------------------------------------------------------------
  async function loadTokenBankOverview() {
    var seq = noteLoad();
    busy('overview', true);
    var root = document.getElementById('tbkOverviewBody');
    // The first visit has nothing to keep on screen. A later reload keeps the
    // figures and dims them, so the click is visible without flashing 加载中….
    if (root && state.overview == null) {
      root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
      root.style.opacity = '';
      root.removeAttribute('aria-busy');
    } else if (root) {
      root.setAttribute('aria-busy', 'true');
      root.style.opacity = '0.55';
    }
    try {
      var res = await apiGet('/api/admin/token-bank/overview');
      if (!loadLive(seq)) return;
      state.overview = res.overview || {};
      state.overviewFeeRate = res.fee_rate;
      state.overviewCreditShareMaxRatio = res.credit_share_max_ratio;
      renderTokenBankOverview();
    } catch (e) {
      // A failed refresh must not replace figures already on screen. The first
      // visit still has nowhere else to put the error.
      if (state.overview == null) showLoadError(seq, root, e);
      else if (loadLive(seq)) toast(loadErrorText(e), 'error');
    } finally {
      if (loadLive(seq)) busy('overview', false);
      if (loadLive(seq) && root) {
        root.removeAttribute('aria-busy');
        root.style.opacity = '';
      }
    }
  }

  function metricCard(labelKey, value, hintKey) {
    return '<div class="item tbk-stat"><div class="item-title">' + esc(t(labelKey)) + '</div><strong>' + esc(value) +
      '</strong>' + (hintKey ? '<div class="item-meta">' + esc(t(hintKey)) + '</div>' : '') + '</div>';
  }

  function marginFigure(labelKey, value, extra) {
    return '<div><span>' + esc(t(labelKey)) + '</span><strong title="' + esc(value) + '">' + esc(value) + '</strong>' +
      (extra ? '<span>' + esc(extra) + '</span>' : '') + '</div>';
  }

  function renderTokenBankOverview() {
    var root = document.getElementById('tbkOverviewBody');
    if (!root) return;
    var o = state.overview || {};
    var cards =
      metricCard('tbkMetricOwners', num(o.ShareOwners, 0), 'tbkMetricOwnersHint') +
      metricCard('tbkMetricShares', num(o.ShareCount, 0), 'tbkMetricSharesHint') +
      metricCard('tbkMetricModels', num(o.ModelCount, 0), 'tbkMetricModelsHint') +
      metricCard('tbkMetricUsedTokens', num(o.UsedTokens, 0), 'tbkMetricUsedTokensHint') +
      metricCard('tbkMetricEarned', creditsFixed(o.EarnedMicro, 2), 'tbkMetricEarnedHint') +
      metricCard('tbkMetricGranted', creditsFixed(o.GrantedMicro, 2), 'tbkMetricGrantedHint') +
      metricCard('tbkMetricWithdrawn', creditsFixed(o.WithdrawnMicro, 2), 'tbkMetricWithdrawnHint') +
      metricCard('tbkMetricFrozen', creditsFixed(o.FrozenMicro, 2), 'tbkMetricFrozenHint');
    var rates = '';
    if (state.overviewFeeRate != null) {
      rates += '<div class="item tbk-stat"><div class="item-title">' + esc(t('tbkFeeRate')) + '</div><strong>' + esc(pct(state.overviewFeeRate)) +
        '</strong><div class="item-meta">' + esc(t('tbkFeeRateHint')) + '</div></div>';
    }
    if (state.overviewCreditShareMaxRatio != null) {
      rates += '<div class="item tbk-stat"><div class="item-title">' + esc(t('tbkCreditShareMaxRatio')) + '</div><strong>' + esc(pct(state.overviewCreditShareMaxRatio)) +
        '</strong><div class="item-meta">' + esc(t('tbkCreditShareMaxRatioHint')) + '</div></div>';
    }
    root.innerHTML = '<div class="tbk-metrics">' + cards + rates + '</div>';
  }

  // --- margins (§9 #20) ----------------------------------------------------
  //
  // The platform's gross margin: what consumers paid, what sharers received,
  // and the difference. §13 names this the standing control against billing
  // inversion, so the inversion columns are not decoration — they are the only
  // place inversion is visible at all. The margin itself can never go negative
  // (§5 ⑥ clamps the payout to the charge), so watching the margin for a
  // negative number would never fire.

  function marginGroupLabel(group) {
    if (group === 'share') return t('tbkMarginGroupShare');
    if (group === 'day') return t('tbkMarginGroupDay');
    return t('tbkMarginGroupModel');
  }

  // The day/group controls stay on the shell's .inline-actions. Stat grids
  // and record-row density are #tab-tokenbank rules in admin-responsive.css:
  // the shell .item is a full-width hero card, and that is what made every
  // number sit in a tall empty row.
  function marginControlsHtml() {
    var days = [7, 30, 90];
    var daysHtml = days.map(function (d) {
      var on = state.margins.days === d;
      // btn-secondary and btn-ghost are both white. aria-pressed is the selected mark.
      return '<button class="' + (on ? 'btn-secondary' : 'btn-ghost') +
        '" type="button" aria-pressed="' + (on ? 'true' : 'false') +
        '" onclick="setTokenBankMarginDays(' + d + ')">' + d + '</button>';
    }).join('');
    var groups = ['model', 'share', 'day'];
    var groupHtml = groups.map(function (g) {
      var on = state.margins.group === g;
      return '<button class="' + (on ? 'btn-secondary' : 'btn-ghost') +
        '" type="button" aria-pressed="' + (on ? 'true' : 'false') +
        '" onclick="setTokenBankMarginGroup(\'' + g + '\')">' + esc(marginGroupLabel(g)) + '</button>';
    }).join('');
    return '<div class="inline-actions section-bottom">' +
      '<span class="item-meta">' + esc(t('tbkMarginWindow')) + '</span>' + daysHtml +
      '<span class="item-meta">' + esc(t('tbkMarginGroupBy')) + '</span>' + groupHtml +
      '<button class="btn-ghost" type="button" onclick="loadTokenBankMargins()">' + esc(t('tbkReload')) + '</button>' +
      '</div>';
  }

  async function loadTokenBankMargins() {
    var seq = noteLoad();
    busy('margins', true);
    var root = document.getElementById('tbkMarginsBody');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      var res = await apiGet('/api/admin/token-bank/margins?days=' + encodeURIComponent(state.margins.days) +
        '&group_by=' + encodeURIComponent(state.margins.group));
      if (!loadLive(seq)) return;
      state.margins.rows = res.margins || [];
      renderTokenBankMargins();
    } catch (e) {
      showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq)) busy('margins', false);
    }
  }

  function setTokenBankMarginDays(days) {
    state.margins.days = Number(days) || 30;
    state.margins.page = 0;
    return loadTokenBankMargins();
  }

  function setTokenBankMarginGroup(group) {
    state.margins.group = group || 'model';
    state.margins.page = 0;
    return loadTokenBankMargins();
  }

  function renderTokenBankMargins() {
    var root = document.getElementById('tbkMarginsBody');
    if (!root) return;
    var rows = state.margins.rows || [];
    // The server always appends the grand total last, with an empty key. It is
    // rendered as its own summary rather than as a table row so it cannot be
    // mistaken for one of the groups.
    var sum = rows.length ? rows[rows.length - 1] : null;
    var groups = sum && rows.length > 1 ? rows.slice(0, rows.length - 1) : [];

    var html = marginControlsHtml();
    if (!sum) {
      root.innerHTML = html + '<div class="hint">' + esc(t('tbkMarginsEmpty')) + '</div>';
      return;
    }

    var cards =
      metricCard('tbkMarginCharged', creditsFixed(sum.charged_micro, 2), 'tbkMarginChargedHint') +
      metricCard('tbkMarginNet', creditsFixed(sum.net_micro, 2), 'tbkMarginNetHint') +
      metricCard('tbkMarginGross', creditsFixed(sum.gross_micro, 2), 'tbkMarginGrossHint') +
      metricCard('tbkMarginValue', creditsFixed(sum.margin_micro, 2), 'tbkMarginValueHint') +
      metricCard('tbkMarginRate', pct(sum.margin_rate), 'tbkMarginRateHint');
    html += '<div class="tbk-metrics">' + cards + '</div>';

    // Inversion is called out above the list, not left as a number to notice.
    // Shortfall is what the clamp absorbed, and it is what makes the risk
    // concrete: "we inverted 40 times" is unactionable without a cost.
    if (Number(sum.inverted_count) > 0) {
      var invertedBody = t('tbkMarginInvertedBody')
        .replace('{count}', num(sum.inverted_count, 0))
        .replace('{clamped}', num(sum.clamped_count, 0));
      if (Number(sum.shortfall_micro) > 0) {
        invertedBody += t('tbkMarginInvertedOverpay').replace('{shortfall}', creditsFixed(sum.shortfall_micro, 2));
      }
      html += '<div class="hint section-bottom"><span class="danger">' +
        esc(t('tbkMarginInvertedTitle')) + '</span> ' + esc(invertedBody) + '</div>';
    }

    if (!groups.length) {
      html += '<div class="hint">' + esc(t('tbkMarginsEmpty')) + '</div>';
      root.innerHTML = html;
      return;
    }

    // Four cards a row, twenty a page. The summary above stays put; only this
    // list pages, and a window or grouping change starts again at the first page.
    var pageSize = 20;
    var pages = Math.max(1, Math.ceil(groups.length / pageSize));
    var page = state.margins.page || 0;
    if (page > pages - 1) page = pages - 1;
    if (page < 0) page = 0;
    state.margins.page = page;
    var slice = groups.slice(page * pageSize, page * pageSize + pageSize);
    html += '<div class="tbk-margin-grid">';
    for (var i = 0; i < slice.length; i++) {
      var r = slice[i] || {};
      var inverted = Number(r.inverted_count) > 0;
      var key = r.key || '';
      var titleAttr = key ? ' title="' + esc(key) + '"' : '';
      var invertedCount = esc(num(r.inverted_count, 0));
      if (inverted) invertedCount = '<span class="tbk-margin-hot">' + invertedCount + '</span>';
      html += '<div class="item tbk-margin-card">' +
        '<div class="item-title"' + titleAttr + '>' + (inverted ? '<span class="tbk-margin-hot">⚠ </span>' : '') + esc(key || '—') + '</div>' +
        '<div class="tbk-margin-stats">' +
        marginFigure('tbkMarginColCharged', creditsFixed(r.charged_micro, 2)) +
        marginFigure('tbkMarginColGross', creditsFixed(r.gross_micro, 2)) +
        marginFigure('tbkMarginColMargin', creditsFixed(r.margin_micro, 2), pct(r.margin_rate)) +
        '</div>' +
        '<div class="item-meta">' +
        esc(t('tbkMarginColFee')) + ' ' + esc(creditsFixed(r.fee_micro, 2)) + ' · ' +
        esc(t('tbkMarginColNet')) + ' ' + esc(creditsFixed(r.net_micro, 2)) +
        '</div>' +
        '<div class="item-meta">' +
        esc(t('tbkMarginColCalls')) + ' ' + esc(num(r.calls, 0)) + ' · ' +
        esc(t('tbkMarginColInverted')) + ' ' + invertedCount + ' · ' +
        esc(t('tbkMarginColClamped')) + ' ' + esc(num(r.clamped_count, 0)) +
        (inverted && Number(r.shortfall_micro) > 0
          ? ' · ' + esc(t('tbkMarginShortfall')) + ' ' + esc(creditsFixed(r.shortfall_micro, 2))
          : '') +
        '</div></div>';
    }
    html += '</div>';
    if (pages > 1) html += marginPager(page, pages);
    root.innerHTML = html;
  }

  function marginPager(page, pages) {
    return '<div class="inline-actions section-gap">' +
      '<button class="btn-ghost" type="button" onclick="tokenBankMarginsPage(-1)"' + (page <= 0 ? ' disabled' : '') + '>' + esc(t('tbkPrev')) + '</button>' +
      '<span class="item-meta">' + esc(t('tbkPage')) + ' ' + esc(String(page + 1)) + ' / ' + esc(String(pages)) + '</span>' +
      '<button class="btn-ghost" type="button" onclick="tokenBankMarginsPage(1)"' + (page >= pages - 1 ? ' disabled' : '') + '>' + esc(t('tbkNext')) + '</button>' +
      '</div>';
  }
  function tokenBankMarginsPage(delta) {
    var next = (state.margins.page || 0) + Number(delta || 0);
    if (next < 0) return;
    state.margins.page = next;
    renderTokenBankMargins();
  }

  // --- settings -------------------------------------------------------------
  async function loadTokenBankSettings() {
    var seq = noteLoad();
    var epoch = state.settingsEpoch;
    busy('settings', true);
    var root = document.getElementById('tbkSettingsForm');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      var res = await apiGet('/api/admin/token-bank/settings');
      if (!loadLive(seq)) return;
      if (epoch !== state.settingsEpoch) return;
      state.settings = res.settings || {};
      renderTokenBankSettings();
    } catch (e) {
      if (epoch === state.settingsEpoch) showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq) && epoch === state.settingsEpoch) busy('settings', false);
    }
  }

  function settingsField(id, labelKey, hintKey, value, step, extra) {
    return '<div class="item tbk-field"><label class="item-title" for="' + id + '">' + esc(t(labelKey)) + '</label>' +
      '<input id="' + id + '" type="number" step="' + esc(step) + '" min="0" value="' + esc(rawNum(value)) + '"' + (extra || '') + '>' +
      '<div class="item-meta">' + esc(t(hintKey)) + '</div></div>';
  }

  function renderTokenBankSettings() {
    var root = document.getElementById('tbkSettingsForm');
    if (!root) return;
    var s = state.settings || {};
    // The fallbacks mirror sqlite.DefaultTokenBankSettings() exactly. They only
    // appear before the first successful load, and a wrong one would show the
    // operator a number that is not what the server would use.
    var d = TBK_SETTINGS_DEFAULTS;
    root.innerHTML =
      '<div class="tbk-settings">' +
      settingsField('tbkSetFeeRate', 'tbkFeeRate', 'tbkFeeRateHint', pick(s.fee_rate, d.fee_rate), '0.01', ' max="1"') +
      settingsField('tbkSetDefIn', 'tbkDefaultUnitIn', 'tbkDefaultUnitInHint', pick(s.default_unit_input_credits_per_10k, d.default_unit_input_credits_per_10k), '0.0001') +
      settingsField('tbkSetDefOut', 'tbkDefaultUnitOut', 'tbkDefaultUnitOutHint', pick(s.default_unit_output_credits_per_10k, d.default_unit_output_credits_per_10k), '0.0001') +
      settingsField('tbkSetDefCacheRead', 'tbkDefaultUnitCacheRead', 'tbkDefaultUnitCacheReadHint', pick(s.default_unit_cached_read_credits_per_10k, d.default_unit_cached_read_credits_per_10k), '0.0001') +
      settingsField('tbkSetDefCacheWrite', 'tbkDefaultUnitCacheWrite', 'tbkDefaultUnitCacheWriteHint', pick(s.default_unit_cache_write_credits_per_10k, d.default_unit_cache_write_credits_per_10k), '0.0001') +
      settingsField('tbkSetMaxShares', 'tbkMaxSharesPerUser', 'tbkMaxSharesPerUserHint', pick(s.max_shares_per_user, d.max_shares_per_user), '1') +
      settingsField('tbkSetCreditRatio', 'tbkCreditShareMaxRatio', 'tbkCreditShareMaxRatioHint', pick(s.credit_share_max_ratio, d.credit_share_max_ratio), '0.05', ' max="1"') +
      settingsField('tbkSetCreditTTL', 'tbkCreditShareTTL', 'tbkCreditShareTTLHint', pick(s.credit_share_link_ttl_hours, d.credit_share_link_ttl_hours), '1') +
      settingsField('tbkSetCreditDaily', 'tbkCreditShareDailyLimit', 'tbkCreditShareDailyLimitHint', pick(s.credit_share_daily_limit, d.credit_share_daily_limit), '1') +
      settingsField('tbkSetCreditMin', 'tbkCreditShareMin', 'tbkCreditShareMinHint', pick(s.credit_share_min_credits, d.credit_share_min_credits), '0.5') +
      settingsField('tbkSetAutoPause', 'tbkAutoPause', 'tbkAutoPauseHint', pick(s.auto_pause_consecutive_failures, d.auto_pause_consecutive_failures), '1') +
      settingsField('tbkSetCanaryHours', 'tbkCanaryWindow', 'tbkCanaryWindowHint', pick(s.canary_window_hours, d.canary_window_hours), '1') +
      '</div>' +
      '<div class="inline-actions section-bottom">' +
      '<button class="btn-primary" type="button" id="tbkSettingsSave" onclick="saveTokenBankSettings()" data-tbk-i18n="tbkSave"></button>' +
      '<button class="btn-ghost" type="button" onclick="loadTokenBankSettings()" data-tbk-i18n="tbkReload"></button>' +
      '</div>' +
      '<div class="hint">' + esc(t('tbkFeeTargetNote')) + '</div>';
    applyTokenBankI18n();
  }

  // `pick` treats null/undefined as "unset" only. Zero is a legitimate value for
  // every one of these fields (a free model, a zero fee), so `||` would silently
  // replace a real zero with the default.
  function pick(value, fallback) {
    return value == null ? fallback : value;
  }

  // A stored cache unit of 0 is not the price settlement uses. The settler
  // keeps the platform default for that direction, so the list must not print 0.
  function cacheUnitLabel(value) {
    var n = Number(value);
    if (isFinite(n) && n > 0) return num(n, 4);
    return t('tbkPriceUsesDefault');
  }

  // Kept in step with sqlite.DefaultTokenBankSettings().
  var TBK_SETTINGS_DEFAULTS = {
    fee_rate: 0.1,
    default_unit_input_credits_per_10k: 3.0,
    default_unit_output_credits_per_10k: 6.0,
    default_unit_cached_read_credits_per_10k: 0.3,
    default_unit_cache_write_credits_per_10k: 3.75,
    max_shares_per_user: 20,
    credit_share_max_ratio: 0.5,
    credit_share_link_ttl_hours: 168,
    credit_share_daily_limit: 10,
    credit_share_min_credits: 1,
    auto_pause_consecutive_failures: 5,
    canary_window_hours: 24
  };

  // null means "not set". JSON.stringify keeps null, and the price-book
  // decoder treats JSON null as a nil pointer. 0 is a set value.
  function priceUnit(el) {
    if (!el) return null;
    var raw = String(el.value).trim();
    if (raw === '') return null;
    var n = parseFloat(raw);
    return isFinite(n) ? n : null;
  }

  function readNum(id, fallback) {
    var el = document.getElementById(id);
    if (!el) return fallback;
    var v = parseFloat(el.value);
    return isFinite(v) ? v : fallback;
  }

  async function saveTokenBankSettings() {
    if (isBusy('settingsSave')) return;
    busy('settingsSave', true);
    var btn = document.getElementById('tbkSettingsSave');
    disableWhileBusy(btn, 'settingsSave');
    // The PUT used to replace the whole blob. Copy the loaded settings first
    // and overlay only the fields this form edits, so clearing_node_id,
    // provider_denylist, and any later field survive a save. Guessing a default
    // for a field the operator cannot see would rewrite it.
    var prev = state.settings || {};
    var d = TBK_SETTINGS_DEFAULTS;
    var payload = Object.assign({}, prev, {
      fee_rate: readNum('tbkSetFeeRate', pick(prev.fee_rate, d.fee_rate)),
      default_unit_input_credits_per_10k: readNum('tbkSetDefIn', pick(prev.default_unit_input_credits_per_10k, d.default_unit_input_credits_per_10k)),
      default_unit_output_credits_per_10k: readNum('tbkSetDefOut', pick(prev.default_unit_output_credits_per_10k, d.default_unit_output_credits_per_10k)),
      default_unit_cached_read_credits_per_10k: readNum('tbkSetDefCacheRead', pick(prev.default_unit_cached_read_credits_per_10k, d.default_unit_cached_read_credits_per_10k)),
      default_unit_cache_write_credits_per_10k: readNum('tbkSetDefCacheWrite', pick(prev.default_unit_cache_write_credits_per_10k, d.default_unit_cache_write_credits_per_10k)),
      max_shares_per_user: Math.round(readNum('tbkSetMaxShares', pick(prev.max_shares_per_user, d.max_shares_per_user))),
      credit_share_max_ratio: readNum('tbkSetCreditRatio', pick(prev.credit_share_max_ratio, d.credit_share_max_ratio)),
      credit_share_link_ttl_hours: Math.round(readNum('tbkSetCreditTTL', pick(prev.credit_share_link_ttl_hours, d.credit_share_link_ttl_hours))),
      credit_share_daily_limit: Math.round(readNum('tbkSetCreditDaily', pick(prev.credit_share_daily_limit, d.credit_share_daily_limit))),
      credit_share_min_credits: readNum('tbkSetCreditMin', pick(prev.credit_share_min_credits, d.credit_share_min_credits)),
      auto_pause_consecutive_failures: Math.round(readNum('tbkSetAutoPause', pick(prev.auto_pause_consecutive_failures, d.auto_pause_consecutive_failures))),
      canary_window_hours: Math.round(readNum('tbkSetCanaryHours', pick(prev.canary_window_hours, d.canary_window_hours))),
      // Not edited by this form; echoed back so the server's validator sees the
      // value it already had. §5 pins fee_target to "provider".
      fee_target: prev.fee_target || 'provider',
      require_review_before_online: !!prev.require_review_before_online,
      require_verified_identity: prev.require_verified_identity == null ? true : !!prev.require_verified_identity
    });
    try {
      var res = await apiSend('/api/admin/token-bank/settings', 'PUT', { settings: payload });
      // The accepted body is the new baseline. A GET that started before this
      // line captured the old epoch and will not paint over the form.
      state.settingsEpoch++;
      state.settings = res.settings || payload;
      renderTokenBankSettings();
      toast(t('tbkSettingsSaved'), 'ok');
    } catch (e) {
      toast(t('tbkSaveFailed', 'Save failed: ') + (e && e.message ? e.message : e), 'error');
    } finally {
      busy('settingsSave', false);
      disableWhileBusy(btn, 'settingsSave');
    }
  }

  // --- users ----------------------------------------------------------------
  async function loadTokenBankUsers(page) {
    if (page != null) state.users.page = Math.max(0, page);
    var seq = noteLoad();
    busy('users', true);
    var root = document.getElementById('tbkUsersBody');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      var q = '?limit=' + state.users.size + '&offset=' + state.users.page * state.users.size;
      var res = await apiGet('/api/admin/token-bank/users' + q);
      if (!loadLive(seq)) return;
      state.users.rows = res.users || [];
      renderTokenBankUsers();
    } catch (e) {
      showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq)) busy('users', false);
    }
  }

  function renderTokenBankUsers() {
    var root = document.getElementById('tbkUsersBody');
    if (!root) return;
    var rows = state.users.rows || [];
    if (!rows.length) {
      root.innerHTML = '<div class="hint">' + esc(t('tbkUsersEmpty')) + '</div>' + usersPager(0);
      applyTokenBankI18n();
      return;
    }
    var html = '<div class="list">';
    rows.forEach(function (u) {
      html += '<div class="item">' +
        '<div class="item-title">' + esc(u.OwnerEmail || u.OwnerUserID || '—') + '</div>' +
        '<div class="item-meta mono">' + esc(u.OwnerUserID || '') + '</div>' +
        '<div class="item-meta">' + esc(t('tbkColShares')) + ': ' + esc(num(u.ShareCount, 0)) +
        ' · ' + esc(t('tbkColModels')) + ': ' + esc(num(u.ModelCount, 0)) +
        ' · ' + esc(t('tbkColUsedTokens')) + ': ' + esc(num(u.UsedTokens, 0)) +
        ' · ' + esc(t('tbkColEarned')) + ': ' + esc(creditsFixed(u.EarnedMicro, 2)) + '</div>' +
        '</div>';
    });
    html += '</div>' + usersPager(rows.length);
    root.innerHTML = html;
    applyTokenBankI18n();
  }

  function usersPager(loaded) {
    var page = state.users.page;
    var atEnd = loaded < state.users.size;
    return '<div class="inline-actions section-bottom">' +
      '<button class="btn-ghost" type="button" onclick="tokenBankUsersPage(-1)"' + (page <= 0 ? ' disabled' : '') + ' data-tbk-i18n="tbkPrev"></button>' +
      '<span class="item-meta">' + esc(t('tbkPage')) + ' ' + esc(String(page + 1)) + '</span>' +
      '<button class="btn-ghost" type="button" onclick="tokenBankUsersPage(1)"' + (atEnd ? ' disabled' : '') + ' data-tbk-i18n="tbkNext"></button>' +
      '</div>';
  }
  function tokenBankUsersPage(delta) { loadTokenBankUsers(state.users.page + Number(delta || 0)); }

  // --- shares ---------------------------------------------------------------
  async function loadTokenBankShares() {
    var seq = noteLoad();
    busy('shares', true);
    var root = document.getElementById('tbkSharesBody');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      // The admin list endpoint takes a generous page because the operator's job
      // here is to find one share to pause or regrade, not to browse. A filter
      // reduces it to something readable. The owner filter is named `user_id`
      // server-side; `owner` would be silently ignored and show everything.
      var q = '?limit=' + state.shares.size +
        '&offset=' + state.shares.page * state.shares.size +
        (state.shares.status ? '&status=' + encodeURIComponent(state.shares.status) : '') +
        (state.shares.owner ? '&user_id=' + encodeURIComponent(state.shares.owner) : '');
      var res = await apiGet('/api/admin/token-bank/shares' + q);
      if (!loadLive(seq)) return;
      state.shares.rows = res.shares || res.items || [];
      renderTokenBankShares();
    } catch (e) {
      showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq)) busy('shares', false);
    }
  }

  var TIERS = ['low', 'mid', 'high'];
  // TokenBankShareStatusActive is the string "active", not "available". The
  // store uses active/paused/revoked, and a mismatched literal here silently
  // (a) filters to zero rows and (b) flips every action button to "Resume".
  var SHARE_STATUSES = ['active', 'paused', 'revoked'];

  function shareStatus(s) { return String((s && s.Status) || '').toLowerCase(); }
  function shareIsLive(s) { return shareStatus(s) === 'active'; }

  function renderTokenBankShares() {
    var root = document.getElementById('tbkSharesBody');
    if (!root) return;
    var rows = state.shares.rows || [];
    var filter = '<div class="inline-actions section-bottom">' +
      '<label class="sr-only" for="tbkShareStatus">' + esc(t('tbkFilterStatus')) + '</label>' +
      '<select id="tbkShareStatus" onchange="tokenBankShareFilter()">' +
      [''].concat(SHARE_STATUSES).map(function (v) {
        return '<option value="' + esc(v) + '"' + (state.shares.status === v ? ' selected' : '') + '>' +
          esc(v === '' ? t('tbkFilterAll') : t('tbkStatus_' + v, v)) + '</option>';
      }).join('') +
      '</select>' +
      '<label class="sr-only" for="tbkShareOwner">' + esc(t('tbkFilterOwner')) + '</label>' +
      '<input id="tbkShareOwner" type="text" placeholder="' + esc(t('tbkFilterOwner')) + '" value="' + esc(state.shares.owner) + '" onkeydown="if(event.key===\'Enter\'){event.preventDefault();tokenBankShareFilter();}">' +
      '<button class="btn-secondary" type="button" onclick="tokenBankShareFilter()" data-tbk-i18n="tbkFilterApply"></button>' +
      '<button class="btn-ghost" type="button" onclick="loadTokenBankShares()" data-tbk-i18n="tbkReload"></button>' +
      '</div>' +
      sharesPager(rows.length);
    if (!rows.length) {
      root.innerHTML = filter + '<div class="hint">' + esc(t('tbkSharesEmpty')) + '</div>';
      applyTokenBankI18n();
      return;
    }
    var html = filter + '<div class="list">';
    rows.forEach(function (s) {
      var models = s.models || [];
      var live = shareIsLive(s);
      var status = shareStatus(s);
      html += '<div class="item">' +
        '<div class="item-title">' + esc(s.DisplayName || s.ID || '—') +
        ' <span class="badge ' + (live ? 'ok' : 'warn') + '">' + esc(t('tbkStatus_' + status, status)) + '</span></div>' +
        '<div class="item-meta mono">' + esc(s.ID || '') + '</div>' +
        '<div class="item-meta">' + esc(t('tbkColOwner')) + ': ' + esc(s.owner_email_masked || s.OwnerEmail || s.OwnerUserID || '—') +
        ' · ' + esc(t('tbkColEarned')) + ': ' + esc(creditsFixed(s.TotalEarnedMicro, 2)) + '</div>' +
        (s.LastError ? '<div class="item-meta">' + esc(t('tbkColLastError')) + ': ' + esc(s.LastError) + '</div>' : '') +
        '<div class="inline-actions">' +
        // A revoked share is gone; offering "Resume" on it would be a button
        // that always fails. Only live/paused shares get the switch.
        (status === 'revoked' ? '' :
          '<button class="btn-ghost" type="button" onclick="toggleTokenBankSharePaused(' + jsArg(s.ID) + ',' + live + ')">' +
          esc(live ? t('tbkPause') : t('tbkResume')) + '</button>') +
        '<button class="btn-ghost" type="button" onclick="takeOutTokenBankShare(' + jsArg(s.ID) + ')">' + esc(t('tbkTakeOut')) + '</button>' +
        '</div>';
      if (models.length) {
        html += '<div class="tbk-model-grid">';
        models.forEach(function (m) {
          var modelName = m.ModelName || '';
          html += '<div class="item tbk-model-card">' +
            '<div class="item-title mono" title="' + esc(modelName) + '">' + esc(modelName) + '</div>' +
            '<div class="item-meta tbk-model-facts">' +
            '<span>' + esc(t('tbkColTier')) + ' ' + esc(m.Tier || 'mid') + ' ×' + esc(num(m.TierMultiplier, 2)) + '</span>' +
            '<span>' + esc(t('tbkColAvailable')) + ' ' + esc(yesNo(m.Available)) + '</span>' +
            '<span>' + esc(t('tbkColEarned')) + ' ' + esc(creditsFixed(m.EarnedMicro, 2)) + '</span>' +
            '</div>' +
            '<div class="inline-actions">' +
            TIERS.map(function (tier) {
              var on = String(m.Tier || 'mid').toLowerCase() === tier;
              return '<button class="' + (on ? 'btn-secondary' : 'btn-ghost') + '" type="button" aria-pressed="' + (on ? 'true' : 'false') + '" onclick="gradeTokenBankModel(' +
                jsArg(s.ID) + ',' + jsArg(m.ModelName) + ',' + jsArg(tier) + ')">' + esc(tier) + '</button>';
            }).join('') +
            '</div></div>';
        });
        html += '</div>';
      }
      html += '</div>';
    });
    html += '</div>';
    root.innerHTML = html;
    applyTokenBankI18n();
  }

  function tokenBankShareFilter() {
    var st = document.getElementById('tbkShareStatus');
    var ow = document.getElementById('tbkShareOwner');
    state.shares.status = st ? st.value : '';
    state.shares.owner = ow ? ow.value.trim() : '';
    // A new filter is a new result set; staying on page 3 of the old one would
    // show an empty page and look like the filter matched nothing.
    state.shares.page = 0;
    loadTokenBankShares();
  }

  // The list endpoint caps at 200 and defaults to 20, so without a pager an
  // install with 50 shares would silently show the first 20 forever.
  function sharesPager(loaded) {
    var page = state.shares.page;
    var atEnd = loaded < state.shares.size;
    return '<div class="inline-actions section-bottom">' +
      '<button class="btn-ghost" type="button" onclick="tokenBankSharesPage(-1)"' + (page <= 0 ? ' disabled' : '') + ' data-tbk-i18n="tbkPrev"></button>' +
      '<span class="item-meta">' + esc(t('tbkPage')) + ' ' + esc(String(page + 1)) + '</span>' +
      '<button class="btn-ghost" type="button" onclick="tokenBankSharesPage(1)"' + (atEnd ? ' disabled' : '') + ' data-tbk-i18n="tbkNext"></button>' +
      '</div>';
  }
  function tokenBankSharesPage(delta) {
    var next = state.shares.page + Number(delta || 0);
    if (next < 0) return;
    state.shares.page = next;
    loadTokenBankShares();
    var root = document.getElementById('tbkSharesBody');
    if (root) root.scrollIntoView({ block: 'nearest' });
  }

  async function toggleTokenBankSharePaused(shareID, pause) {
    if (isBusy('share:' + shareID)) return;
    busy('share:' + shareID, true);
    try {
      await apiSend('/api/admin/token-bank/shares/' + encodeURIComponent(shareID) + '/paused', 'PUT', { paused: !!pause });
      toast(pause ? t('tbkPaused') : t('tbkResumed'), 'ok');
      await loadTokenBankShares();
    } catch (e) {
      toast(t('tbkActionFailed', 'Failed: ') + (e && e.message ? e.message : e), 'error');
    } finally {
      busy('share:' + shareID, false);
    }
  }

  async function takeOutTokenBankShare(shareID) {
    if (!shareID || isBusy('share:' + shareID)) return;
    if (!window.confirm(t('tbkTakeOutConfirm'))) return;
    busy('share:' + shareID, true);
    try {
      await apiSend('/api/admin/token-bank/shares/' + encodeURIComponent(shareID), 'DELETE');
      toast(t('tbkTakenOut'), 'ok');
      await loadTokenBankShares();
    } catch (e) {
      toast(t('tbkActionFailed', 'Failed: ') + (e && e.message ? e.message : e), 'error');
    } finally {
      busy('share:' + shareID, false);
    }
  }

  async function gradeTokenBankModel(shareID, modelName, tier) {
    if (!shareID || !modelName) return;
    if (isBusy('grade:' + shareID + ':' + modelName)) return;
    busy('grade:' + shareID + ':' + modelName, true);
    try {
      await apiSend('/api/admin/token-bank/shares/' + encodeURIComponent(shareID) + '/models/' +
        encodeURIComponent(modelName) + '/tier', 'PUT', { tier: tier });
      toast(t('tbkGraded'), 'ok');
      await loadTokenBankShares();
    } catch (e) {
      toast(t('tbkActionFailed', 'Failed: ') + (e && e.message ? e.message : e), 'error');
    } finally {
      busy('grade:' + shareID + ':' + modelName, false);
    }
  }

  // --- price book -----------------------------------------------------------
  async function loadTokenBankPriceBook() {
    var seq = noteLoad();
    busy('price', true);
    var root = document.getElementById('tbkPriceBody');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      var res = await apiGet('/api/admin/token-bank/price-book');
      if (!loadLive(seq)) return;
      state.price.rows = res.rules || res.price_book || [];
      renderTokenBankPriceBook();
    } catch (e) {
      showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq)) busy('price', false);
    }
  }

  function renderTokenBankPriceBook() {
    var root = document.getElementById('tbkPriceBody');
    if (!root) return;
    var rows = state.price.rows || [];
    var html = '<div class="hint">' + esc(t('tbkPriceNote')) + '</div>';
    html += '<div class="list">';
    html += '<div class="item">' +
      '<label class="item-title" for="tbkPricePattern">' + esc(t('tbkPricePattern')) + '</label>' +
      '<input id="tbkPricePattern" type="text" placeholder="gpt-4o-*" autocomplete="off">' +
      '<div class="item-meta">' + esc(t('tbkPricePatternHint')) + '</div>' +
      '<div class="tbk-price-fields">' +
      '<label class="sr-only" for="tbkPriceIn">' + esc(t('tbkPriceIn')) + '</label>' +
      '<input id="tbkPriceIn" type="number" step="0.0001" min="0" placeholder="' + esc(t('tbkPriceIn')) + '">' +
      '<label class="sr-only" for="tbkPriceOut">' + esc(t('tbkPriceOut')) + '</label>' +
      '<input id="tbkPriceOut" type="number" step="0.0001" min="0" placeholder="' + esc(t('tbkPriceOut')) + '">' +
      '<label class="sr-only" for="tbkPriceCacheRead">' + esc(t('tbkPriceCacheRead')) + '</label>' +
      '<input id="tbkPriceCacheRead" type="number" step="0.0001" min="0" placeholder="' + esc(t('tbkPriceCacheRead')) + '">' +
      '<label class="sr-only" for="tbkPriceCacheWrite">' + esc(t('tbkPriceCacheWrite')) + '</label>' +
      '<input id="tbkPriceCacheWrite" type="number" step="0.0001" min="0" placeholder="' + esc(t('tbkPriceCacheWrite')) + '">' +
      '</div>' +
      '<div class="inline-actions">' +
      '<button class="btn-primary" type="button" onclick="upsertTokenBankPriceRule()" data-tbk-i18n="tbkPriceAdd"></button>' +
      '</div></div>';
    if (!rows.length) {
      html += '</div><div class="hint">' + esc(t('tbkPriceEmpty')) + '</div>';
      root.innerHTML = html;
      applyTokenBankI18n();
      return;
    }
    rows.forEach(function (r) {
      html += '<div class="item">' +
        '<div class="item-title mono">' + esc(r.ModelPattern || r.model_pattern || '') + '</div>' +
        '<div class="item-meta">' + esc(t('tbkPriceIn')) + ': ' + esc(num(r.UnitInputPer10K, 4)) +
        ' · ' + esc(t('tbkPriceOut')) + ': ' + esc(num(r.UnitOutputPer10K, 4)) +
        ' · ' + esc(t('tbkPriceCacheRead')) + ': ' + esc(cacheUnitLabel(r.UnitCachedReadPer10K)) +
        ' · ' + esc(t('tbkPriceCacheWrite')) + ': ' + esc(cacheUnitLabel(r.UnitCacheWritePer10K)) + '</div>' +
        '<div class="item-meta">' + esc(t('tbkPriceUpdated')) + ': ' + esc(fmtTime(r.UpdatedAt || r.updated_at)) + '</div>' +
        '<div class="inline-actions">' +
        '<button class="btn-ghost" type="button" onclick="deleteTokenBankPriceRule(' + jsArg(r.ID || r.id) + ')">' + esc(t('tbkDelete')) + '</button>' +
        '</div></div>';
    });
    html += '</div>';
    root.innerHTML = html;
    applyTokenBankI18n();
  }

  async function upsertTokenBankPriceRule() {
    if (isBusy('priceSave')) return;
    var patternEl = document.getElementById('tbkPricePattern');
    var inEl = document.getElementById('tbkPriceIn');
    var outEl = document.getElementById('tbkPriceOut');
    var cacheReadEl = document.getElementById('tbkPriceCacheRead');
    var cacheWriteEl = document.getElementById('tbkPriceCacheWrite');
    var pattern = patternEl ? patternEl.value.trim() : '';
    if (!pattern) { toast(t('tbkPricePatternRequired'), 'error'); return; }
    busy('priceSave', true);
    try {
      // Empty is null, not 0. The server keeps the stored unit on update and
      // stores null as 0 only for a pattern that does not exist yet. A typed 0
      // must be sent as 0. Input or output 0 is free. Cache 0 keeps the
      // platform default cache price. `|| 0` cannot tell a blank field from a
      // typed 0, and it used to wipe the other three prices.
      await apiSend('/api/admin/token-bank/price-book', 'POST', {
        model_pattern: pattern,
        unit_input_credits_per_10k: priceUnit(inEl),
        unit_output_credits_per_10k: priceUnit(outEl),
        unit_cached_read_credits_per_10k: priceUnit(cacheReadEl),
        unit_cache_write_credits_per_10k: priceUnit(cacheWriteEl)
      });
      toast(t('tbkPriceSaved'), 'ok');
      [patternEl, inEl, outEl, cacheReadEl, cacheWriteEl].forEach(function (el) { if (el) el.value = ''; });
      await loadTokenBankPriceBook();
    } catch (e) {
      toast(t('tbkSaveFailed', 'Save failed: ') + (e && e.message ? e.message : e), 'error');
    } finally {
      busy('priceSave', false);
    }
  }

  async function deleteTokenBankPriceRule(id) {
    if (!id) return;
    if (!window.confirm(t('tbkPriceDeleteConfirm'))) return;
    try {
      await apiSend('/api/admin/token-bank/price-book/' + encodeURIComponent(id), 'DELETE');
      toast(t('tbkDeleted'), 'ok');
      await loadTokenBankPriceBook();
    } catch (e) {
      toast(t('tbkActionFailed', 'Failed: ') + (e && e.message ? e.message : e), 'error');
    }
  }

  // --- credit share links (audit) -------------------------------------------
  async function loadTokenBankCreditShares() {
    var seq = noteLoad();
    busy('creditshares', true);
    var root = document.getElementById('tbkCreditSharesBody');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      // Ask for one extra row so a full page of 20 can tell "there is another
      // page" from "this page is the last". Showing that extra row would make
      // the grid 21 cards, and the next offset would repeat it.
      var page = state.creditShares;
      var q = '?limit=' + (page.size + 1) + '&offset=' + (page.page * page.size);
      var res = await apiGet('/api/admin/token-bank/credit-shares' + q);
      if (!loadLive(seq)) return;
      var raw = res.links || res.credit_shares || [];
      state.creditShares.more = raw.length > page.size;
      state.creditShares.rows = raw.slice(0, page.size);
      renderTokenBankCreditShares();
    } catch (e) {
      showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq)) busy('creditshares', false);
    }
  }

  function renderTokenBankCreditShares() {
    var root = document.getElementById('tbkCreditSharesBody');
    if (!root) return;
    var rows = state.creditShares.rows || [];
    var note = '<div class="hint">' + esc(t('tbkCreditSharesNote')) + '</div>';
    if (!rows.length) {
      // Page 0 with nothing to show has no pager. A later empty page still
      // does, or the operator cannot step back from past the last page.
      var back = state.creditShares.page > 0 ? creditSharesPager() : '';
      root.innerHTML = note + '<div class="hint">' + esc(t('tbkCreditSharesEmpty')) + '</div>' + back;
      applyTokenBankI18n();
      return;
    }
    // This endpoint returns a purpose-built wire struct with snake_case tags
    // (unlike the shares/price views, which marshal store structs and therefore
    // come back PascalCase). The two spellings are read in that order so a tag
    // change on either side degrades to a blank cell rather than a wrong one.
    var html = note + '<div class="tbk-gift-grid">';
    rows.forEach(function (l) {
      var id = l.id || l.ID || '';
      var status = String(l.status || l.Status || '');
      var amountMicro = l.credits_micro != null ? l.credits_micro : (l.CreditsMicro != null ? l.CreditsMicro : l.amount_micro);
      var reason = giftFreezeReason(l, status);
      var revokedAt = l.revoked_at || l.RevokedAt || '';
      html += '<div class="item tbk-gift-card">' +
        '<div class="item-title mono">' + esc(l.code || l.Code || id) +
        ' <span class="badge ' + giftStatusClass(status) + '">' + esc(giftStatusLabel(status)) + '</span></div>' +
        (reason ? '<div class="item-meta tbk-gift-reason">' + esc(reason) + '</div>' : '') +
        '<div class="item-meta">' + esc(t('tbkColSender')) + ': ' + esc(l.sender_email_masked || l.SenderEmail || '') +
        ' → ' + esc(t('tbkColReceiver')) + ': ' + esc(l.claimed_by_email_masked || l.ClaimedByEmail || '—') + '</div>' +
        '<div class="item-meta">' + esc(t('tbkColAmount')) + ': ' + esc(creditsFixed(amountMicro, 6)) +
        ' · ' + esc(t('tbkColCreated')) + ': ' + esc(fmtTime(l.created_at || l.CreatedAt)) + '</div>' +
        (status === 'revoked' && revokedAt ? '<div class="item-meta">' + esc(t('tbkGiftFrozenAt')) + ': ' + esc(fmtTime(revokedAt)) + '</div>' : '') +
        // Settled, expired, and already-revoked links are not revocable. A
        // claimed link still is, because the credits have not moved. Hiding
        // the button is how the server's `revocable` flag is honoured.
        (l.revocable ? '<div class="inline-actions"><button class="btn-ghost" type="button" onclick="revokeTokenBankCreditShare(' + jsArg(id) + ')">' + esc(t('tbkRevoke')) + '</button></div>' : '') +
        '</div>';
    });
    html += '</div>' + creditSharesPager();
    root.innerHTML = html;
    applyTokenBankI18n();
  }

  function creditSharesPager() {
    var page = state.creditShares.page;
    var atEnd = !state.creditShares.more;
    return '<div class="inline-actions section-bottom">' +
      '<button class="btn-ghost" type="button" onclick="tokenBankCreditSharesPage(-1)"' + (page <= 0 ? ' disabled' : '') + ' data-tbk-i18n="tbkPrev"></button>' +
      '<span class="item-meta">' + esc(t('tbkPage')) + ' ' + esc(String(page + 1)) + '</span>' +
      '<button class="btn-ghost" type="button" onclick="tokenBankCreditSharesPage(1)"' + (atEnd ? ' disabled' : '') + ' data-tbk-i18n="tbkNext"></button>' +
      '</div>';
  }
  function tokenBankCreditSharesPage(delta) {
    var next = state.creditShares.page + Number(delta || 0);
    if (next < 0) return;
    state.creditShares.page = next;
    loadTokenBankCreditShares();
    var root = document.getElementById('tbkCreditSharesBody');
    if (root) root.scrollIntoView({ block: 'nearest' });
  }

  function giftWasClaimed(row) {
    if (!row) return false;
    var email = String(row.claimed_by_email_masked || row.ClaimedByEmail || '').trim();
    var at = row.claimed_at || row.ClaimedAt || '';
    return !!(email || at);
  }

  // "已冻结" alone does not say whether the sender cancelled, an admin froze
  // the link, or the row predates the reason column. A blank historical row
  // still says whether anyone had claimed it, which is the only fact stored.
  function giftFreezeReason(row, status) {
    if (status !== 'revoked') return '';
    var by = String((row && (row.revoked_by || row.RevokedBy)) || '');
    var reason = String((row && (row.revoke_reason || row.RevokeReason)) || '').trim();
    if (by === 'sender') return t('tbkGiftReasonSender');
    if (by === 'admin') {
      return t('tbkGiftReasonAdmin').replace('{reason}', reason || t('tbkGiftReasonMissing'));
    }
    return t(giftWasClaimed(row) ? 'tbkGiftReasonUnknownClaimed' : 'tbkGiftReasonUnknownOpen');
  }

  // Gift-link status is not the share status. Share "revoked" is 已取出; a
  // revoked gift link was frozen and the credits went back to the sender.
  // The server never sends "frozen"; that check left every revoked link unstyled.
  function giftStatusLabel(status) {
    if (!status) return '';
    return t('tbkGift_' + status, status);
  }
  function giftStatusClass(status) {
    if (status === 'claimed' || status === 'settled') return 'ok';
    if (status === 'revoked' || status === 'expired' || status === 'frozen') return 'warn';
    return '';
  }

  function badgeLabel(kind) {
    if (!kind) return '';
    return t('tbkBadge_' + kind, kind);
  }

  async function loadTokenBankLeaderboard() {
    var seq = noteLoad();
    busy('leaderboard', true);
    var root = document.getElementById('tbkLeaderboardBody');
    if (root) root.innerHTML = '<div class="hint">' + esc(t('tbkLoading')) + '</div>';
    try {
      var res = await apiGet('/api/admin/token-bank/leaderboard?limit=50');
      if (!loadLive(seq)) return;
      state.leaders = res.leaders || [];
      renderTokenBankLeaderboard();
    } catch (e) {
      showLoadError(seq, root, e);
    } finally {
      if (loadLive(seq)) busy('leaderboard', false);
    }
  }

  function renderTokenBankLeaderboard() {
    var root = document.getElementById('tbkLeaderboardBody');
    if (!root) return;
    var rows = state.leaders || [];
    if (!rows.length) {
      root.innerHTML = '<div class="hint">' + esc(t('tbkLeadersEmpty')) + '</div>';
      return;
    }
    var html = '<div class="list">';
    rows.forEach(function (row) {
      var badges = [badgeLabel(row.rank_badge), badgeLabel(row.lifetime_badge)].filter(Boolean).join(' · ');
      html += '<div class="item"><div class="item-title">#' + esc(row.rank) + ' ' + esc(row.owner_email || row.owner_user_id || '—') +
        '</div><div class="item-meta">' + esc(row.owner_user_id || '') +
        (badges ? ' · ' + esc(badges) : '') + '</div><strong>' +
        esc(creditsFixed(row.earned_micro, 2)) + ' · ' + esc(num(row.calls, 0)) + ' ' + esc(t('tbkLeaderCalls')) +
        ' · ' + esc(num(row.tokens, 0)) + ' tokens</strong></div>';
    });
    html += '</div>';
    root.innerHTML = html;
  }

  // Match tokenBankGiftRevokeReason: collapse whitespace, then count runes.
  // The HTTP error is an English sentence, so an over-long note is refused
  // here and the Chinese page can say why.
  function giftRevokeNote(raw) {
    return String(raw == null ? '' : raw).trim().split(/\s+/).filter(function (part) { return part; }).join(' ');
  }

  async function revokeTokenBankCreditShare(id) {
    // The button stays on the card until the reload. A second click during
    // the request would open another dialog and then 409, because the first
    // freeze already returned the credits.
    if (!id || isBusy('creditRevoke')) return;
    busy('creditRevoke', true);
    try {
      if (!window.confirm(t('tbkRevokeConfirm'))) return;
      // Cancel leaves the link alone. A blank note is refused here so the
      // request is not sent only to come back as reason_required.
      var typed = window.prompt(t('tbkGiftReasonPrompt'));
      if (typed == null) return;
      var reason = giftRevokeNote(typed);
      if (!reason) {
        toast(t('tbkGiftReasonRequired'), 'error');
        return;
      }
      if (Array.from(reason).length > 200) {
        toast(t('tbkGiftReasonTooLong'), 'error');
        return;
      }
      await apiSend('/api/admin/token-bank/credit-shares/' + encodeURIComponent(id) + '/revoke', 'POST', { reason: reason });
      toast(t('tbkRevoked'), 'ok');
      await loadTokenBankCreditShares();
    } catch (e) {
      var code = e && e.code;
      if (code === 'reason_too_long') toast(t('tbkGiftReasonTooLong'), 'error');
      else if (code === 'reason_required') toast(t('tbkGiftReasonRequired'), 'error');
      else toast(t('tbkActionFailed', 'Failed: ') + (e && e.message ? e.message : e), 'error');
    } finally {
      busy('creditRevoke', false);
    }
  }

  // --- i18n table -----------------------------------------------------------
  var TBK_I18N = {
    en: {
      tbkTitle: 'Token Bank',
      tbkDesc: 'Platform price book, fee rate, shared models, and the audit view for credit transfers.',
      tbkSubOverview: 'Overview',
      tbkSubSettings: 'Settings',
      tbkSubUsers: 'Sharing Users',
      tbkSubShares: 'Shared Models',
      tbkSubPrice: 'Price Book',
      tbkSubCreditShares: 'Credit Transfers',
      tbkSubMargins: 'Gross Margin',
      tbkSubLeaderboard: 'Leaderboard',
      tbkLeadersEmpty: 'No settled sharing yet.',
      tbkLeaderCalls: 'calls',
      tbkBadge_gold: 'Gold',
      tbkBadge_silver: 'Silver',
      tbkBadge_bronze: 'Bronze',
      tbkBadge_pillar: 'Pillar',
      tbkBadge_steady: 'Steady',
      tbkBadge_contributor: 'Contributor',
      tbkBadge_sprout: 'Sprout',
      tbkMarginWindow: 'Window (days)',
      tbkMarginGroupBy: 'Group by',
      tbkMarginGroupModel: 'Model',
      tbkMarginGroupShare: 'Share',
      tbkMarginGroupDay: 'Day',
      tbkMarginCharged: 'Consumer paid',
      tbkMarginChargedHint: 'What consumers were charged, in Credits.',
      tbkMarginNet: 'Sharer received',
      tbkMarginNetHint: 'Credited to sharers after the platform fee.',
      tbkMarginGross: 'List price',
      tbkMarginGrossHint: 'Price book × tier, before the fee. Above "consumer paid" means inversion.',
      tbkMarginValue: 'Gross margin',
      tbkMarginValueHint: 'Consumer paid − sharer received. Never negative: §5 ⑥ clamps the payout.',
      tbkMarginRate: 'Margin rate',
      tbkMarginRateHint: 'Gross margin ÷ consumer paid.',
      tbkMarginInvertedTitle: 'Billing inversion detected.',
      tbkMarginInvertedBody: '{count} calls priced below the list price ({clamped} clamped to zero margin).',
      tbkMarginInvertedOverpay: ' Without the clamp the platform would have overpaid {shortfall} Credits.',
      tbkMarginColCharged: 'Paid',
      tbkMarginColGross: 'List',
      tbkMarginColFee: 'Fee',
      tbkMarginColNet: 'Net',
      tbkMarginColMargin: 'Margin',
      tbkMarginColRate: 'Rate',
      tbkMarginColCalls: 'Calls',
      tbkMarginColInverted: 'Inverted',
      tbkMarginColClamped: 'Clamped',
      tbkMarginShortfall: 'Overpay',
      tbkMarginsEmpty: 'No settled usage in this window.',
      tbkLoading: 'Loading…',
      tbkLoadFailed: 'Failed to load: ',
      tbkSave: 'Save',
      tbkReload: 'Reload',
      tbkPrev: 'Previous',
      tbkNext: 'Next',
      tbkPage: 'Page',
      tbkDelete: 'Delete',
      tbkPause: 'Pause',
      tbkResume: 'Resume',
      tbkTakeOut: 'Take Out',
      tbkRevoke: 'Freeze',
      tbkFilterApply: 'Apply',
      tbkFilterAll: 'All',
      tbkFilterStatus: 'Status',
      tbkFilterOwner: 'Owner user ID',
      tbkStatus_active: 'Active',
      tbkStatus_paused: 'Paused',
      tbkStatus_revoked: 'Taken out',
      tbkColShares: 'Shares',
      tbkColModels: 'Models',
      tbkColUsedTokens: 'Used tokens',
      tbkColEarned: 'Earned (Credits)',
      tbkColOwner: 'Owner',
      tbkColTier: 'Tier',
      tbkColAvailable: 'Available',
      tbkYes: 'Yes',
      tbkNo: 'No',
      tbkColLastError: 'Last error',
      tbkColSender: 'Sender',
      tbkColReceiver: 'Receiver',
      tbkColAmount: 'Amount (Credits)',
      tbkColCreated: 'Created',
      tbkMetricOwners: 'Sharing users',
      tbkMetricOwnersHint: 'Distinct owners with at least one live share.',
      tbkMetricShares: 'Live shares',
      tbkMetricSharesHint: 'Shares that are not taken out.',
      tbkMetricModels: 'Shared models',
      tbkMetricModelsHint: 'Models reachable through the routing pool.',
      tbkMetricUsedTokens: 'Tokens served',
      tbkMetricUsedTokensHint: 'Input plus output tokens across all shared models.',
      tbkMetricEarned: 'Earned by sharers',
      tbkMetricEarnedHint: 'Net credits credited to sharers after the platform fee.',
      tbkMetricGranted: 'Gifted away',
      tbkMetricGrantedHint: 'Credits sent to other users through transfer links.',
      tbkMetricWithdrawn: 'Withdrawn to hubs',
      tbkMetricWithdrawnHint: 'Credits taken out to a Hub to spend as an allowance.',
      tbkMetricFrozen: 'Frozen',
      tbkMetricFrozenHint: 'Credits held by unclaimed transfer links.',
      tbkFeeRate: 'Platform fee rate',
      tbkFeeRateHint: 'Taken from the sharer\'s earnings. 0.1 means the sharer keeps 90%.',
      tbkFeeTargetNote: 'The fee is always taken from the sharer\'s earnings; consumers never see it.',
      tbkDefaultUnitIn: 'Default input price',
      tbkDefaultUnitInHint: 'Credits per 10K tokens, used when the price book has no match.',
      tbkDefaultUnitOut: 'Default output price',
      tbkDefaultUnitOutHint: 'Credits per 10K tokens, used when the price book has no match.',
      tbkDefaultUnitCacheRead: 'Default cache-read price',
      tbkDefaultUnitCacheReadHint: 'Credits per 10K cached input tokens. 0 bills those tokens at the input price.',
      tbkDefaultUnitCacheWrite: 'Default cache-write price',
      tbkDefaultUnitCacheWriteHint: 'Credits per 10K cache-write tokens. 0 bills those tokens at the input price.',
      tbkMaxSharesPerUser: 'Max shares per user',
      tbkMaxSharesPerUserHint: 'Upper bound on live shares one account may hold.',
      tbkCreditShareMaxRatio: 'Max transfer ratio',
      tbkCreditShareMaxRatioHint: 'A single transfer may not exceed this fraction of available credits.',
      tbkCreditShareTTL: 'Transfer link lifetime (hours)',
      tbkCreditShareTTLHint: 'An unclaimed link is released after this many hours.',
      tbkCreditShareDailyLimit: 'Daily transfer limit',
      tbkCreditShareDailyLimitHint: 'Maximum transfer links one account may create per day.',
      tbkCreditShareMin: 'Minimum transfer (Credits)',
      tbkCreditShareMinHint: 'Transfers below this amount are rejected.',
      tbkAutoPause: 'Auto-pause after failures',
      tbkAutoPauseHint: 'Consecutive probe failures before a share is paused.',
      tbkCanaryWindow: 'Canary window (hours)',
      tbkCanaryWindowHint: 'A newly shared model takes about 5% of traffic for this many hours. 0 skips the window. Models already published keep their deadline.',
      tbkSettingsSaved: 'Settings saved.',
      tbkSaveFailed: 'Save failed: ',
      tbkUsersEmpty: 'No sharing users yet.',
      tbkSharesEmpty: 'No shares match the current filter.',
      tbkPaused: 'Share paused.',
      tbkResumed: 'Share resumed.',
      tbkTakenOut: 'Share taken out.',
      tbkGraded: 'Tier updated. New requests settle at the new rate.',
      tbkActionFailed: 'Action failed: ',
      tbkTakeOutConfirm: 'Take this share out? The registry member is removed immediately and the history is kept.',
      tbkPriceNote: 'Prices are Credits per 10,000 tokens. An exact model name wins; otherwise the longest matching prefix wins. On an existing pattern, a blank unit keeps the saved price; a new pattern stores a blank unit as 0. Input or output at 0 is free. Cache read or write at 0 keeps the platform default for that direction.',
      tbkPricePattern: 'Model pattern',
      tbkPricePatternHint: 'An exact model name, or a trailing-* prefix such as gpt-4o-*.',
      tbkPriceIn: 'Input / 10K',
      tbkPriceOut: 'Output / 10K',
      tbkPriceUsesDefault: 'default',
      tbkPriceCacheRead: 'Cache read / 10K',
      tbkPriceCacheWrite: 'Cache write / 10K',
      tbkPriceUpdated: 'Updated',
      tbkPriceAdd: 'Add or Update Rule',
      tbkPriceEmpty: 'No price rules yet. Shared models settle at the platform defaults until one is added.',
      tbkPriceSaved: 'Price rule saved.',
      tbkPricePatternRequired: 'Enter a model pattern first.',
      tbkPriceDeleteConfirm: 'Delete this price rule?',
      tbkDeleted: 'Deleted.',
      tbkCreditSharesEmpty: 'No transfer links yet.',
      tbkCreditSharesNote: 'Unclaimed and claimed links still hold the sender\'s credits. Freeze a link to return them. The card keeps the reason.',
      tbkRevokeConfirm: 'Freeze this transfer link and return the credits to the sender?',
      tbkRevoked: 'Link frozen.',
      tbkGiftReasonPrompt: 'Why is this link being frozen? The reason is shown on the card.',
      tbkGiftReasonRequired: 'A freeze reason is required.',
      tbkGiftReasonTooLong: 'A freeze reason can be at most 200 characters.',
      tbkGiftReasonSender: 'Reason: the sender cancelled the link. Credits were returned.',
      tbkGiftReasonAdmin: 'Reason: an admin froze the link. {reason}',
      tbkGiftReasonMissing: '(no note)',
      tbkGiftReasonUnknownOpen: 'Reason: not recorded. The link was frozen before anyone claimed it. Credits were returned to the sender.',
      tbkGiftReasonUnknownClaimed: 'Reason: not recorded. The link was frozen after it was claimed and before withdrawal. Credits were returned to the sender.',
      tbkGiftFrozenAt: 'Frozen at',
      tbkGift_active: 'Unclaimed',
      tbkGift_claimed: 'Claimed',
      tbkGift_revoked: 'Frozen',
      tbkGift_expired: 'Expired',
      tbkGift_settled: 'Withdrawn',
      tbkGift_frozen: 'Frozen'
    },
    zh: {
      tbkTitle: 'Token 银行',
      tbkDesc: '平台定价表、手续费率、共享模型，以及积分转赠审计。',
      tbkSubOverview: '总览',
      tbkSubSettings: '设置',
      tbkSubUsers: '分享用户',
      tbkSubShares: '共享模型',
      tbkSubPrice: '定价表',
      tbkSubCreditShares: '积分转赠',
      tbkSubMargins: '毛利透视',
      tbkSubLeaderboard: '排行榜',
      tbkLeadersEmpty: '还没有已结算的分享。',
      tbkLeaderCalls: '次调用',
      tbkBadge_gold: '金',
      tbkBadge_silver: '银',
      tbkBadge_bronze: '铜',
      tbkBadge_pillar: '支柱',
      tbkBadge_steady: '稳定',
      tbkBadge_contributor: '贡献',
      tbkBadge_sprout: '新芽',
      tbkMarginWindow: '窗口（天）',
      tbkMarginGroupBy: '分组',
      tbkMarginGroupModel: '模型',
      tbkMarginGroupShare: '分享',
      tbkMarginGroupDay: '按天',
      tbkMarginCharged: '消费者实付',
      tbkMarginChargedHint: '消费者被收取的积分。',
      tbkMarginNet: '分享者所得',
      tbkMarginNetHint: '扣除平台手续费后计入分享者的积分。',
      tbkMarginGross: '定价表毛额',
      tbkMarginGrossHint: '定价表 × 档位倍率，扣费前。高于「消费者实付」即为倒挂。',
      tbkMarginValue: '平台毛利',
      tbkMarginValueHint: '消费者实付 − 分享者所得。永不为负：§5 ⑥ 会把支付额夹到实付。',
      tbkMarginRate: '毛利率',
      tbkMarginRateHint: '平台毛利 ÷ 消费者实付。',
      tbkMarginInvertedTitle: '检测到计费倒挂。',
      tbkMarginInvertedBody: '有 {count} 笔调用的定价低于定价表毛额（其中 {clamped} 笔已被夹到零毛利）。',
      tbkMarginInvertedOverpay: '若无夹取，平台将多付 {shortfall} 积分。',
      tbkMarginColCharged: '实付',
      tbkMarginColGross: '毛额',
      tbkMarginColFee: '手续费',
      tbkMarginColNet: '所得',
      tbkMarginColMargin: '毛利',
      tbkMarginColRate: '毛利率',
      tbkMarginColCalls: '调用数',
      tbkMarginColInverted: '倒挂',
      tbkMarginColClamped: '夹取',
      tbkMarginShortfall: '多付',
      tbkMarginsEmpty: '该窗口内没有已结算的用量。',
      tbkLoading: '加载中…',
      tbkLoadFailed: '加载失败：',
      tbkSave: '保存',
      tbkReload: '重新加载',
      tbkPrev: '上一页',
      tbkNext: '下一页',
      tbkPage: '第',
      tbkDelete: '删除',
      tbkPause: '暂停',
      tbkResume: '恢复',
      tbkTakeOut: '取出',
      tbkRevoke: '冻结',
      tbkFilterApply: '筛选',
      tbkFilterAll: '全部',
      tbkFilterStatus: '状态',
      tbkFilterOwner: '拥有者用户 ID',
      tbkStatus_active: '生效中',
      tbkStatus_paused: '已暂停',
      tbkStatus_revoked: '已取出',
      tbkColShares: '分享数',
      tbkColModels: '模型数',
      tbkColUsedTokens: '已消耗 tokens',
      tbkColEarned: '已赚积分',
      tbkColOwner: '拥有者',
      tbkColTier: '档位',
      tbkColAvailable: '可用',
      tbkYes: '是',
      tbkNo: '否',
      tbkColLastError: '最近错误',
      tbkColSender: '发送方',
      tbkColReceiver: '接收方',
      tbkColAmount: '数量（积分）',
      tbkColCreated: '创建时间',
      tbkMetricOwners: '分享用户数',
      tbkMetricOwnersHint: '至少持有一个有效分享的账户数。',
      tbkMetricShares: '有效分享数',
      tbkMetricSharesHint: '尚未被取出的分享。',
      tbkMetricModels: '共享模型数',
      tbkMetricModelsHint: '已进入调度池、可被路由到的模型。',
      tbkMetricUsedTokens: '已服务 tokens',
      tbkMetricUsedTokensHint: '全部共享模型的输入加输出 token 总量。',
      tbkMetricEarned: '分享者已赚',
      tbkMetricEarnedHint: '扣除平台手续费后实际入账给分享者的积分。',
      tbkMetricGranted: '已转赠',
      tbkMetricGrantedHint: '通过转赠链接送给其他用户的积分。',
      tbkMetricWithdrawn: '已提取到节点',
      tbkMetricWithdrawnHint: '提取到 Hub、可作为额度消费的积分。',
      tbkMetricFrozen: '冻结中',
      tbkMetricFrozenHint: '被未领取的转赠链接占用的积分。',
      tbkFeeRate: '平台手续费率',
      tbkFeeRateHint: '从分享者所得中扣除。0.1 表示分享者实得 90%。',
      tbkFeeTargetNote: '手续费始终从分享者所得中扣，消费者无感。',
      tbkDefaultUnitIn: '默认输入单价',
      tbkDefaultUnitInHint: '每 1 万 token 的积分；定价表无匹配时使用。',
      tbkDefaultUnitOut: '默认输出单价',
      tbkDefaultUnitOutHint: '每 1 万 token 的积分；定价表无匹配时使用。',
      tbkDefaultUnitCacheRead: '默认缓存读单价',
      tbkDefaultUnitCacheReadHint: '每 1 万缓存读 token 的积分。填 0 则这些 token 按输入单价计。',
      tbkDefaultUnitCacheWrite: '默认缓存写单价',
      tbkDefaultUnitCacheWriteHint: '每 1 万缓存写 token 的积分。填 0 则这些 token 按输入单价计。',
      tbkMaxSharesPerUser: '单用户最大分享数',
      tbkMaxSharesPerUserHint: '一个账号最多可持有的有效分享数量。',
      tbkCreditShareMaxRatio: '单笔转赠上限比例',
      tbkCreditShareMaxRatioHint: '单笔转赠不得超过可用积分的这个比例。',
      tbkCreditShareTTL: '转赠链接有效期（小时）',
      tbkCreditShareTTLHint: '超过该时长未领取的链接会自动解冻退回。',
      tbkCreditShareDailyLimit: '每日转赠次数上限',
      tbkCreditShareDailyLimitHint: '一个账号每天最多可创建的转赠链接数。',
      tbkCreditShareMin: '最小转赠额（积分）',
      tbkCreditShareMinHint: '低于该数额的转赠会被拒绝。',
      tbkAutoPause: '连续失败自动暂停',
      tbkAutoPauseHint: '连续探测失败达到该次数后自动暂停分享。',
      tbkCanaryWindow: '金丝雀时长（小时）',
      tbkCanaryWindowHint: '新分享的模型在这段时间内大约只承接 5% 的流量。填 0 则新模型直接全量。已经上线的模型保持原截止时间。',
      tbkSettingsSaved: '设置已保存。',
      tbkSaveFailed: '保存失败：',
      tbkUsersEmpty: '暂无分享用户。',
      tbkSharesEmpty: '当前筛选条件下没有分享。',
      tbkPaused: '已暂停。',
      tbkResumed: '已恢复。',
      tbkTakenOut: '已取出。',
      tbkGraded: '档位已更新。新请求将按新倍率结算。',
      tbkActionFailed: '操作失败：',
      tbkTakeOutConfirm: '确认取出该分享？调度成员会立即移除，历史记录保留。',
      tbkPriceNote: '单价单位为「每 1 万 token 的积分」。精确模型名优先，其次是最长匹配前缀。更新已有规则时，留空的单价保持原值；新规则的留空单价按 0 保存。输入或输出填 0 为免费。缓存读或缓存写为 0 时，沿用平台默认的缓存单价。',
      tbkPricePattern: '模型匹配式',
      tbkPricePatternHint: '精确模型名，或结尾带 * 的前缀，例如 gpt-4o-*。',
      tbkPriceIn: '输入 / 1 万',
      tbkPriceOut: '输出 / 1 万',
      tbkPriceUsesDefault: '默认',
      tbkPriceCacheRead: '缓存读 / 1 万',
      tbkPriceCacheWrite: '缓存写 / 1 万',
      tbkPriceUpdated: '更新时间',
      tbkPriceAdd: '新增或更新规则',
      tbkPriceEmpty: '暂无定价规则。未匹配的模型按平台默认单价结算。',
      tbkPriceSaved: '定价规则已保存。',
      tbkPricePatternRequired: '请先填写模型匹配式。',
      tbkPriceDeleteConfirm: '确认删除该定价规则？',
      tbkDeleted: '已删除。',
      tbkCreditSharesEmpty: '暂无转赠链接。',
      tbkCreditSharesNote: '待领取或已领取的链接仍占用发送方积分。冻结会把积分退回，并在卡片上写明原因。',
      tbkRevokeConfirm: '确认冻结该转赠链接并把积分退回发送方？',
      tbkRevoked: '链接已冻结。',
      tbkGiftReasonPrompt: '请填写冻结原因。原因会显示在这张卡片上。',
      tbkGiftReasonRequired: '冻结必须填写原因。',
      tbkGiftReasonTooLong: '冻结原因不能超过 200 字。',
      tbkGiftReasonSender: '原因：发送方撤销，积分已退回。',
      tbkGiftReasonAdmin: '原因：管理员冻结。{reason}',
      tbkGiftReasonMissing: '（未填写说明）',
      tbkGiftReasonUnknownOpen: '原因未记录。链接未被领取即被冻结，积分已退回发送方。',
      tbkGiftReasonUnknownClaimed: '原因未记录。链接在领取后、提现前被冻结，积分已退回发送方。',
      tbkGiftFrozenAt: '冻结时间',
      tbkGift_active: '待领取',
      tbkGift_claimed: '已领取',
      tbkGift_revoked: '已冻结',
      tbkGift_expired: '已过期',
      tbkGift_settled: '已提现',
      tbkGift_frozen: '已冻结'
    }
  };

  // admin-core calls restoreTab() before this file runs, so a refresh that lands
  // on Token 银行 shows the panel and the static 加载中… placeholder without
  // ever calling initTokenBankTab. 重新加载 works only because the function
  // exists by the time the user clicks. If a later restore does call init, the
  // startup flag above makes this resume a no-op instead of a second request.
  function registerTokenBankTab() {
    var panel = document.getElementById('tab-tokenbank');
    if (!panel) return;
    var signedIn = typeof window.token === 'function' && !!window.token();
    if (panel.classList.contains('active') && signedIn) {
      if (!startupLoadStarted) initTokenBankTab();
      return;
    }
    applyTokenBankI18n();
  }

  window.initTokenBankTab = initTokenBankTab;
  window.switchTokenBankSubTab = switchTokenBankSubTab;
  window.loadTokenBankOverview = loadTokenBankOverview;
  window.loadTokenBankSettings = loadTokenBankSettings;
  window.saveTokenBankSettings = saveTokenBankSettings;
  window.loadTokenBankUsers = loadTokenBankUsers;
  window.tokenBankUsersPage = tokenBankUsersPage;
  window.loadTokenBankShares = loadTokenBankShares;
  window.tokenBankShareFilter = tokenBankShareFilter;
  window.tokenBankSharesPage = tokenBankSharesPage;
  window.toggleTokenBankSharePaused = toggleTokenBankSharePaused;
  window.takeOutTokenBankShare = takeOutTokenBankShare;
  window.gradeTokenBankModel = gradeTokenBankModel;
  window.loadTokenBankPriceBook = loadTokenBankPriceBook;
  window.upsertTokenBankPriceRule = upsertTokenBankPriceRule;
  window.deleteTokenBankPriceRule = deleteTokenBankPriceRule;
  window.loadTokenBankCreditShares = loadTokenBankCreditShares;
  window.tokenBankCreditSharesPage = tokenBankCreditSharesPage;
  window.revokeTokenBankCreditShare = revokeTokenBankCreditShare;
  window.loadTokenBankMargins = loadTokenBankMargins;
  window.loadTokenBankLeaderboard = loadTokenBankLeaderboard;
  window.setTokenBankMarginDays = setTokenBankMarginDays;
  window.setTokenBankMarginGroup = setTokenBankMarginGroup;
  window.tokenBankMarginsPage = tokenBankMarginsPage;

  if (document.readyState === 'loading' || document.readyState === 'interactive') {
    // Deferred scripts run at interactive, before DOMContentLoaded. Resume now
    // so the overview request starts during startup, and once more on
    // DOMContentLoaded in case the panel was activated after this file.
    if (document.readyState !== 'loading') registerTokenBankTab();
    document.addEventListener('DOMContentLoaded', registerTokenBankTab);
  } else {
    registerTokenBankTab();
  }
})();
