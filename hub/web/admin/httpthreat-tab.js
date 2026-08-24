/*
 * HTTP threat class-head admin. Separate from LLM routing class-head.
 * ASCII only; Chinese uses unicode escapes.
 */
(function(global) {
  var I18N = {
    en: {
      httpThreatTabTitle: 'HTTP Threat Head',
      httpThreatTabSubtitle: 'Label weak-rule HTTP samples, train a candidate head, then shadow before canary/on.',
      httpThreatNavDesc: 'Rules plus generalization head'
    },
    zh: {
      httpThreatTabTitle: '\u6076\u610f HTTP \u5206\u7c7b\u5934',
      httpThreatTabSubtitle: '\u5f31\u89c4\u5219\u8865\u6807\u3001\u8bad candidate\uff0c\u5148 shadow \u518d canary/on\u3002',
      httpThreatNavDesc: '\u89c4\u5219 + \u6cdb\u5316\u5934'
    }
  };
  function zh(en, cn) { return String(global.currentLang || 'en') === 'zh' ? cn : en; }
  function $id(id) { return document.getElementById(id); }
  function esc(v) { return typeof global.escapeHtml === 'function' ? global.escapeHtml(v || '') : String(v || ''); }
  function jsArg(v) { return JSON.stringify(String(v || '')).replace(/</g, '\\u003c'); }
  function api(path, opts) { return global.api(path, opts); }
  function notify(message, kind) { if (typeof global.showToast === 'function') global.showToast(message, kind || 'info'); }
  function classLabel(c) {
    var map = {
      benign: zh('benign / allow', '\u6b63\u5e38 / \u653e\u884c'),
      scan: zh('scan / observe', '\u626b\u63cf / \u89c2\u5bdf'),
      exploit: zh('exploit / block', '\u5229\u7528 / \u963b\u65ad'),
      auth_abuse: zh('auth_abuse / challenge', '\u8ba4\u8bc1\u6ee5\u7528 / \u6311\u6218'),
      malware: zh('malware / block', '\u6076\u610f\u8f6f\u4ef6 / \u963b\u65ad'),
      exfil: zh('exfil / block', '\u5916\u5e26 / \u963b\u65ad'),
      fraud: zh('fraud / block', '\u8bc8\u9a97 / \u963b\u65ad'),
      abuse: zh('abuse / ratelimit', '\u6ee5\u7528 / \u9650\u901f'),
      unknown: zh('unknown / keep rule', '\u62d2\u8bc6 / \u4fdd\u6301\u89c4\u5219')
    };
    return map[c] || c;
  }
  function whyText(code) {
    var map = {
      no_serving: zh('No serving weights yet.', '\u8fd8\u6ca1\u6709 serving'),
      distribution_incomplete: zh('Distribution is incomplete.', '\u5206\u53d1\u672a\u9f50'),
      encoder_not_ready: zh('Encoder is not ready.', '\u7f16\u7801\u5668\u672a\u5c31\u7eea'),
      coverage: zh('Review coverage is below 200 or a trainable class is missing.', '\u8986\u76d6\u4e0d\u8db3 200 \u6216\u53ef\u8bad\u7c7b\u7f3a\u5931'),
      accuracy: zh('P3/P4 human accuracy is below 0.85.', '\u4eba\u5de5\u51c6\u786e\u7387\u4e0d\u8db3 0.85'),
      recall: zh('High-risk recall is below 0.80 in a 7-day window.', '\u9ad8\u5371\u53ec\u56de\u4e0d\u8db3 0.80'),
      empty_targets: zh('No target detect nodes. Cannot raise canary/on.', '\u76ee\u6807\u8282\u70b9\u96c6\u5408\u4e3a\u7a7a\uff0c\u4e0d\u80fd\u5347 canary/on'),
      no_gold: zh('No gold labels yet (need auto or human).', '\u8fd8\u6ca1\u6709\u91d1\u6807\uff08\u7f3a\u81ea\u52a8\u6216\u4eba\u5de5\uff09'),
      corpus_unwritable: zh('Corpus is not writable.', '\u8bed\u6599\u5e93\u4e0d\u53ef\u5199'),
      windows: zh('Two 7-day windows are not both green.', '\u4e24\u4e2a 7 \u65e5\u7a97\u53e3\u5c1a\u672a\u90fd\u7eff')
    };
    return map[code] || code;
  }
  function applyI18nKeys() {
    var lang = String(global.currentLang || 'en') === 'zh' ? 'zh' : 'en';
    var pack = I18N[lang];
    if (global.I18N) {
      global.I18N.en = Object.assign({}, global.I18N.en || {}, I18N.en);
      global.I18N.zh = Object.assign({}, global.I18N.zh || {}, I18N.zh);
    }
    ['httpThreatTitle', 'navHTTPThreat'].forEach(function(id) { var el = $id(id); if (el) el.textContent = pack.httpThreatTabTitle; });
    ['httpThreatSubtitle'].forEach(function(id) { var el = $id(id); if (el) el.textContent = pack.httpThreatTabSubtitle; });
    var navDesc = $id('navHTTPThreatDesc'); if (navDesc) navDesc.textContent = pack.httpThreatNavDesc;
    var obs = $id('httpThreatObserveTitle'); if (obs) obs.textContent = zh('Bypass observation', '\u65c1\u8def\u89c2\u5bdf');
    var note = $id('httpThreatObserveNote'); if (note) note.textContent = zh('Observation only. Agreement is not a promote gate.', '\u89c2\u5bdf\u9879\uff0c\u4e0d\u662f\u8f6c\u6b63\u95e8\u7981');
    var gate = $id('httpThreatGateTitle'); if (gate) gate.textContent = zh('Gates', '\u95e8\u7981');
    var q = $id('httpThreatQueueTitle'); if (q) q.textContent = zh('Review queue', '\u5f85\u8865\u6807');
  }
  function renderGate(label, g) {
    g = g || {};
    var bits = [
      zh('coverage ', '\u8986\u76d6 ') + (g.review_coverage ? 'ok' : 'no'),
      zh('accuracy ', '\u51c6\u786e\u7387 ') + (g.accuracy ? 'ok' : 'no'),
      zh('recall ', '\u53ec\u56de ') + (g.recall ? 'ok' : 'no'),
      zh('windows ', '\u4e24\u7a97 ') + (g.two_windows ? 'ok' : 'no'),
      zh('artifact ', '\u5236\u54c1 ') + (g.artifact ? 'ok' : 'no')
    ];
    return '<div><strong>' + esc(label) + '</strong> ' + (g.can_promote ? 'ready' : 'blocked') + '<div class="item-meta">' + esc(bits.join(' / ')) + '</div></div>';
  }
  function renderStatus(st, servingGate, candGate) {
    var root = $id('httpThreatStatus'); if (!root) return;
    var isAdmin = String(st.role || '') !== 'analyst';
    var why = (st.why_not_promote || []).map(function(code, i) {
      return '<a href="#httpThreatGateAnchor" onclick="return true">' + esc(whyText(code)) + '</a>';
    });
    var actions = [];
    if (isAdmin) {
      actions = [
        '<button class="btn-secondary" type="button" ' + (st.can_train && !st.training ? '' : 'disabled ') + 'onclick="httpThreatTrain()">' + esc(st.training ? zh('Training\u2026', '\u8bad\u7ec3\u4e2d\u2026') : zh('Train candidate', '\u8bad\u7ec3 candidate')) + '</button>',
        '<button class="btn-secondary" type="button" onclick="httpThreatAdopt(false)">' + esc(zh('Adopt serving', '\u91c7\u7528 serving')) + '</button>',
        '<button class="btn-ghost" type="button" onclick="httpThreatAdopt(true)">' + esc(zh('Force adopt (report stays red)', '\u5f3a\u5236\u91c7\u7528\uff08\u62a5\u544a\u4ecd\u662f\u7ea2\u7684\uff09')) + '</button>',
        '<button class="btn-ghost" type="button" onclick="httpThreatPipeline(\'shadow\')">' + esc(zh('Shadow', '\u964d shadow')) + '</button>',
        '<button class="btn-ghost" type="button" onclick="httpThreatPipeline(\'canary\')">' + esc('Canary') + '</button>',
        '<button class="btn-primary" type="button" onclick="httpThreatPipeline(\'on\')">' + esc(zh('On', '\u5168\u91cf')) + '</button>',
        '<button class="btn-ghost" type="button" onclick="httpThreatPipeline(\'off\')">' + esc('Off') + '</button>',
        '<button class="btn-ghost" type="button" onclick="httpThreatRollback()">' + esc(zh('Rollback', '\u56de\u6eda')) + '</button>',
        '<button class="btn-ghost" type="button" onclick="httpThreatForcePromote()">' + esc(zh('Force promote (report stays red)', '\u5f3a\u5236\u8f6c\u6b63\uff08\u62a5\u544a\u4ecd\u662f\u7ea2\u7684\uff09')) + '</button>'
      ];
    } else {
      actions = ['<div class="item-meta">' + esc(zh('Analyst: label only. Adopt / promote / rollback need an admin.', '\u5206\u6790\u5e08\u53ea\u80fd\u8865\u6807\uff1b\u91c7\u7528/\u8f6c\u6b63/\u56de\u6eda\u9700\u7ba1\u7406\u5458\u3002')) + '</div>'];
    }
    root.innerHTML = '<div class="item-title">' + esc(zh('Now: ', '\u73b0\u5728\uff1a')) + esc(st.pipeline || 'off') +
      (st.serving_ready ? ' / serving ready' : ' / no serving') +
      (st.distributed ? ' / distributed' : ' / waiting ACK') +
      (st.training ? zh(' / training', ' / \u8bad\u7ec3\u4e2d') : '') + '</div>' +
      '<div class="item-meta">' + esc(zh('Encoder ', '\u7f16\u7801\u5668 ') + (st.encoder_id || '-') +
      (st.encoder_ready ? zh(' ready', ' \u5c31\u7eea') : zh(' not ready', ' \u672a\u5c31\u7eea')) +
      (st.llm_ready ? zh(' / LLM ready', ' / LLM \u5df2\u914d\u7f6e') : zh(' / LLM not configured', ' / LLM \u672a\u914d\u7f6e'))) + '</div>' +
      (st.train_error ? '<div class="item-meta">' + esc(zh('Last train failed: ', '\u4e0a\u6b21\u8bad\u7ec3\u5931\u8d25\uff1a') + st.train_error) + '</div>' : '') +
      '<div class="item-meta">' + (why.length ? why.join(' ') : esc(zh('No extra blockers listed.', '\u65e0\u989d\u5916\u963b\u6ede\u9879'))) + esc(
      ' Queue ' + (st.queue_count || 0) +
      zh(' gold auto/human/llm ', ' \u91d1\u6807 \u81ea\u52a8/\u4eba\u5de5/LLM ') + (st.gold_auto || 0) + '/' + (st.gold_human || 0) + '/' + (st.gold_llm || 0)) + '</div>' +
      ((st.cannot_train || []).length ? '<div class="item-meta">' + esc(zh('Cannot train: ', '\u4e0d\u80fd\u5f00\u8bad\uff1a') + (st.cannot_train || []).map(whyText).join(' ')) + '</div>' : '') +
      ((st.target_nodes || []).length ? '<div class="item-meta">' + esc(zh('Targets: ', '\u76ee\u6807\u8282\u70b9\uff1a')) + (st.target_nodes || []).map(function(n) {
        return esc(n) + (isAdmin ? ' <a href="#" onclick="httpThreatRemoveTarget(' + jsArg(n) + ');return false;">x</a>' : '');
      }).join(', ') + '</div>' : '') +
      ((st.pending_nodes || []).length ? '<div class="item-meta">' + esc(zh('Waiting ACK: ', '\u672a\u786e\u8ba4\u8282\u70b9\uff1a') + (st.pending_nodes || []).join(', ')) + '</div>' : '') +
      (isAdmin ? '<div style="display:flex;gap:8px;margin-top:8px;flex-wrap:wrap"><input id="httpThreatTargetAdd" class="input" type="text" placeholder="' + esc(zh('Add detect node', '\u6dfb\u52a0\u68c0\u6d4b\u8282\u70b9')) + '" style="max-width:200px">' +
      '<button class="btn-ghost" type="button" onclick="httpThreatAddTarget()">' + esc(zh('Add target', '\u52a0\u5165\u96c6\u5408')) + '</button>' +
      '<input id="httpThreatCap" class="input" type="number" min="1" value="' + esc(String(st.corpus_cap || 4000)) + '" style="max-width:100px">' +
      '<button class="btn-ghost" type="button" onclick="httpThreatSetCap()">' + esc(zh('Set corpus cap', '\u8bbe\u8bed\u6599\u4e0a\u9650')) + '</button>' +
      '<button class="btn-ghost" type="button" onclick="httpThreatEnableExport()">' + esc(zh('Enable export', '\u5f00\u5bfc\u51fa')) + '</button>' +
      '<button class="btn-ghost" type="button" onclick="httpThreatDownloadExport()">' + esc(zh('Download export', '\u4e0b\u8f7d\u5bfc\u51fa')) + '</button>' +
      '<button class="btn-ghost" type="button" onclick="httpThreatImport()">' + esc(zh('Import candidate', '\u5bfc\u5165 candidate')) + '</button></div>' : '') +
      (st.drop_count ? '<div class="item-meta">' + esc(zh('Dropped samples: ', '\u6295\u9012\u4e22\u5f03\uff1a') + st.drop_count) + '</div>' : '') +
      (st.safety_valve ? '<div class="item-meta">' + esc(zh('Safety valve tripped: demoted to shadow. This is not a gate.', '\u5b89\u5168\u9600\u5df2\u964d shadow\uff0c\u8fd9\u4e0d\u662f\u95e8\u7981\u3002')) + '</div>' : '') +
      (st.nat_hint ? '<div class="item-meta">' + esc(zh('NAT enlarges the canary bucket; prefer site or tenant hash.', 'NAT \u4f1a\u653e\u5927\u91d1\u4e1d\u96c0\u6876\uff0c\u4f18\u5148 site/tenant')) + '</div>' : '') +
      (st.map_version ? '<div class="item-meta">' + esc(zh('Map ', '\u6620\u5c04 ') + st.map_version) + '</div>' : '') +
      '<div class="item-meta">' + esc(zh('Corpus cap ', '\u8bed\u6599\u4e0a\u9650 ') + (st.corpus_cap || 0) +
      (st.corpus_writable ? '' : zh(' / not writable', ' / \u4e0d\u53ef\u5199')) +
      (st.export_enabled ? zh(' / export on', ' / \u5bfc\u51fa\u5df2\u5f00') : zh(' / export off', ' / \u5bfc\u51fa\u5173'))) + '</div>' +
      '<div style="display:flex;flex-wrap:wrap;gap:8px;margin-top:10px">' + actions.join('') + '</div>' +
      '<div id="httpThreatGateAnchor" style="margin-top:10px;display:grid;gap:8px">' + renderGate(zh('Serving gate', 'serving \u95e8\u7981'), servingGate) + renderGate(zh('Candidate gate', 'candidate \u95e8\u7981'), candGate) + '</div>';
  }
  function renderObserve(rep, online, runs) {
    var root = $id('httpThreatObserve'); if (!root) return;
    rep = rep || {};
    online = online || {};
    var cross = rep.cross || {};
    var rows = Object.keys(cross).sort().map(function(rule) {
      var cells = Object.keys(cross[rule] || {}).sort().map(function(pred) {
        return esc(pred) + ':' + Number(cross[rule][pred] || 0);
      }).join(' ');
      return '<div class="item-meta">' + esc(zh('rule ', '\u89c4\u5219 ') + rule + ' \u2192 ' + cells) + '</div>';
    }).join('');
    var last = (runs && runs.length) ? runs[runs.length - 1] : null;
    var runIds = last && last.sample_ids ? last.sample_ids.slice(0, 20) : [];
    var runLine = last ? esc(zh('Last train ', '\u4e0a\u6b21\u8bad\u7ec3 ') + (last.trained_at || '') + ' / ' + ((last.sample_ids || []).length) + ' ids') +
      (runIds.length ? ' ' + runIds.map(function(sid) {
        return '<a href="#" onclick="httpThreatFillRun(' + jsArg(sid) + ');return false;">' + esc(sid.slice(0, 8)) + '</a>';
      }).join(' ') : '') : '';
    root.innerHTML = '<div class="item-meta">' + esc(zh('Compared ', '\u5bf9\u6bd4 ') + (rep.compared || 0) +
      zh(', agree ', '\uff0c\u4e00\u81f4 ') + (rep.agree || 0) +
      zh(', would rewrite ', '\uff0c\u4f1a\u6539\u5199 ') + (rep.would_rewrite || 0) +
      zh(', low confidence ', '\uff0c\u4f4e\u7f6e\u4fe1 ') + (rep.low_confidence || 0) +
      zh(', rewrite rate ', '\uff0c\u6539\u5199\u7387 ') + Number(rep.rewrite_rate || 0).toFixed(2) +
      zh(', block rate ', '\uff0c\u963b\u65ad\u7387 ') + Number(rep.block_rate || 0).toFixed(2) +
      zh(', unlabel rate ', '\uff0c\u64a4\u6807\u7387 ') + Number(rep.unlabel_rate || 0).toFixed(2) +
      zh(' (replay, observation only)', ' \uff08\u56de\u653e\u89c2\u5bdf\u9879\uff09')) + '</div>' +
      '<div class="item-meta">' + esc(zh('Online ', '\u5728\u7ebf ') + (online.compared || 0) +
      zh(', would rewrite ', '\uff0c\u4f1a\u6539\u5199 ') + (online.would_rewrite || 0) +
      zh(' (observation only)', ' \uff08\u89c2\u5bdf\u9879\uff09')) + '</div>' +
      (rows || '<div class="item-meta">' + esc(zh('No cross table yet.', '\u5c1a\u65e0\u4ea4\u53c9\u8868')) + '</div>') +
      (runLine ? '<div class="item-meta">' + runLine + '</div>' : '') +
      '<div id="httpThreatRunFill" class="item-meta"></div>';
  }
  function reasonText(code) {
    var map = {
      coverage: zh('Coverage gap', '\u8986\u76d6\u7f3a\u53e3'),
      disagree: zh('Rule / head disagree', '\u89c4\u5219\u4e0e\u5934\u5206\u6b67'),
      hot: zh('Hot cross-table cell', '\u4ea4\u53c9\u8868\u70ed\u683c'),
      recent: zh('Newest first', '\u6309\u6700\u65b0')
    };
    return map[code] || code;
  }
  function renderQueue(items) {
    var root = $id('httpThreatQueue'); if (!root) return;
    items = items || [];
    var classes = ['benign', 'scan', 'exploit', 'auth_abuse', 'malware', 'exfil', 'fraud', 'abuse'];
    var batchOpts = classes.map(function(c) { return '<option value="' + esc(c) + '">' + esc(classLabel(c)) + '</option>'; }).join('');
    var toolbar = '<div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:10px">' +
      '<input id="httpThreatRuleFilter" class="input" type="text" placeholder="' + esc(zh('Filter rule id', '\u6309\u89c4\u5219 ID \u7b5b')) + '" style="max-width:180px">' +
      '<input id="httpThreatSiteFilter" class="input" type="text" placeholder="' + esc(zh('Filter site', '\u6309\u7ad9\u70b9\u7b5b')) + '" style="max-width:140px">' +
      '<button class="btn-ghost" type="button" onclick="loadHTTPThreatTab()">' + esc(zh('Apply filter', '\u5e94\u7528\u7b5b\u9009')) + '</button>' +
      '<select id="httpThreatBatchGold">' + batchOpts + '</select>' +
      '<button class="btn-secondary" type="button" onclick="httpThreatLabelBatch()">' + esc(zh('Label selected (max 50)', '\u6279\u91cf\u6807\u6210\uff08\u6700\u591a 50\uff09')) + '</button>' +
      '<span class="item-meta">' + esc(zh('Same class only, current filter.', '\u4ec5\u540c\u7b5b\u9009\u3001\u540c\u4e00\u76ee\u6807\u7c7b\u3002')) + '</span></div>';
    if (!items.length) {
      root.innerHTML = toolbar + '<div class="hint">' + esc(zh('No P3/P4 samples waiting for a human label.', '\u6ca1\u6709\u5f85\u4eba\u5de5\u8865\u6807\u7684 P3/P4 \u6837\u672c\u3002')) + '</div>';
      return;
    }
    root.innerHTML = toolbar + items.map(function(s) {
      var sid = s.ID || s.id;
      var opts = classes.map(function(c) { return '<option value="' + esc(c) + '">' + esc(classLabel(c)) + '</option>'; }).join('');
      var human = (s.GoldSource || s.gold_source) === 'human';
      var llmClass = s.LLMClass || s.llm_class || '';
      var llmReason = s.LLMReason || s.llm_reason || '';
      var needHuman = !!(s.NeedHuman || s.need_human);
      var probs = s.head_probs || s.HeadProbs || [];
      var top = (probs || []).slice(0, 3).map(function(p) {
        return (p.class || p.Class || '') + '=' + Number(p.p || p.P || 0).toFixed(2);
      }).join(' ');
      var would = s.would_action || s.WouldAction || '';
      var reason = s.queue_reason || s.QueueReason || '';
      return '<article class="item" style="padding:10px;margin-bottom:8px"><label style="display:flex;gap:8px;align-items:flex-start">' +
        '<input type="checkbox" class="httpThreatPick" value="' + esc(sid) + '">' +
        '<pre style="white-space:pre-wrap;font-size:12px;margin:0 0 8px;flex:1">' + esc(s.Preview || s.preview || '') + '</pre></label>' +
        '<div class="item-meta">' + esc(zh('rule ', '\u89c4\u5219 ') + (s.RuleClass || s.rule_class || '-') + ' / ' + (s.RuleSource || s.rule_source || '-') +
        ((s.RuleID || s.rule_id) ? ' / ' + (s.RuleID || s.rule_id) : '') +
        zh(' head ', ' \u5934 ') + (s.HeadClass || s.head_class || '-') +
        (s.HeadMaxP || s.head_max_p ? ' max_p=' + Number(s.HeadMaxP || s.head_max_p).toFixed(2) : '')) +
        (top ? esc(zh(' top ', ' top ') + top) : '') +
        (would ? esc(zh(' / if promoted: ', ' / \u82e5\u8f6c\u6b63\u5c06\u53d8\u6210 ') + would) : '') +
        (reason ? esc(' / ' + reasonText(reason)) : '') +
        (needHuman ? esc(zh(' / needs human', ' / \u987b\u4eba\u5de5')) : '') +
        (llmClass || llmReason ? esc(zh(' / LLM ', ' / LLM ') + (llmClass || 'abstain') + (llmReason ? ' (' + llmReason + ')' : '')) : '') + '</div>' +
        '<div style="display:flex;gap:8px;margin-top:8px;align-items:center;flex-wrap:wrap"><select id="httpThreatGold-' + esc(sid) + '">' + opts + '</select>' +
        '<button class="btn-secondary" type="button" ' + (global.__httpThreatWritable === false ? 'disabled ' : '') + 'onclick="httpThreatLabel(' + jsArg(sid) + ')">' + esc(zh('Label', '\u6807\u6210')) + '</button>' +
        '<button class="btn-ghost" type="button" onclick="httpThreatAbstain(' + jsArg(sid) + ')">' + esc(zh('Abstain', '\u5f03\u6743')) + '</button>' +
        '<button class="btn-ghost" type="button" onclick="httpThreatUnlabel(' + jsArg(sid) + ')">' + esc(zh('Clear', '\u64a4\u6807')) + '</button>' +
        '<button class="btn-ghost" type="button" onclick="httpThreatBusiness(' + jsArg(sid) + ')">' + esc(zh('This is business', '\u8fd9\u662f\u4e1a\u52a1')) + '</button>' +
        (human ? '' : '<button class="btn-ghost" type="button" onclick="httpThreatArbitrate(' + jsArg(sid) + ')">' + esc(zh('Ask LLM', 'LLM \u5efa\u8bae')) + '</button>') +
        (llmClass ? '<button class="btn-secondary" type="button" onclick="httpThreatPromoteLLM(' + jsArg(sid) + ')">' + esc(zh('Promote to human', '\u5347\u4eba\u5de5')) + '</button><button class="btn-ghost" type="button" onclick="httpThreatRejectLLM(' + jsArg(sid) + ')">' + esc(zh('Reject LLM', '\u6253\u56de')) + '</button>' : '') +
        '</div></article>';
    }).join('');
  }
  function renderAudit(rows) {
    var root = $id('httpThreatAudit'); if (!root) return;
    rows = rows || [];
    if (!rows.length) {
      root.innerHTML = '<div class="hint">' + esc(zh('No recent dispositions.', '\u5c1a\u65e0\u6700\u8fd1\u5904\u7f6e\u3002')) + '</div>';
      return;
    }
    root.innerHTML = rows.slice(0, 30).map(function(r) {
      var sid = r.id || r.ID;
      return '<div class="item" style="padding:8px;margin-bottom:6px"><div class="item-meta">' +
        esc((r.action || '-') + ' / ' + (r.rule_class || '-') + (r.rule_id ? ' / ' + r.rule_id : '') +
        (r.head_class ? zh(' head ', ' \u5934 ') + r.head_class : '')) +
        ' <button class="btn-ghost" type="button" onclick="httpThreatBusiness(' + jsArg(sid) + ')">' +
        esc(zh('This is business', '\u8fd9\u662f\u4e1a\u52a1')) + '</button></div>' +
        '<pre style="white-space:pre-wrap;font-size:12px;margin:6px 0 0">' + esc(r.preview || '') + '</pre></div>';
    }).join('');
  }
  function renderRules(view, isAdmin) {
    var root = $id('httpThreatRules'); if (!root) return;
    view = view || {};
    var rows = view.entries || view.Entries || [];
    var form = isAdmin ? '<div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:8px">' +
      '<input id="httpThreatMapRule" class="input" type="text" placeholder="' + esc(zh('Rule id', '\u89c4\u5219 ID')) + '" style="max-width:180px">' +
      '<select id="httpThreatMapClass">' +
      ['benign','scan','exploit','auth_abuse','malware','exfil','fraud','abuse'].map(function(c) {
        return '<option value="' + esc(c) + '">' + esc(classLabel(c)) + '</option>';
      }).join('') + '</select>' +
      '<button class="btn-ghost" type="button" onclick="httpThreatRemap()">' + esc(zh('Remap (voids auto gold)', '\u6539\u6620\u5c04\uff08\u4f5c\u5e9f\u81ea\u52a8\u91d1\u6807\uff09')) + '</button></div>' : '';
    var list = rows.length ? rows.map(function(r) {
      return '<div class="item-meta">' + esc((r.rule_id || r.RuleID || '-') + ' / ' + (r.class || r.Class || '-') +
        zh(' hits ', ' \u547d\u4e2d ') + (r.hits || r.Hits || 0)) + '</div>';
    }).join('') : '<div class="item-meta">' + esc(zh('No rule hits yet.', '\u5c1a\u65e0\u89c4\u5219\u547d\u4e2d\u3002')) + '</div>';
    var intel = view.intel || view.Intel || [];
    var sites = view.sites || view.Sites || [];
    var intelForm = isAdmin ? '<div style="display:flex;gap:8px;flex-wrap:wrap;margin:10px 0 8px">' +
      '<input id="httpThreatIntelHost" class="input" type="text" placeholder="' + esc(zh('Malicious host', '\u6076\u610f Host')) + '" style="max-width:180px">' +
      '<select id="httpThreatIntelClass">' +
      ['exploit','malware','exfil','fraud','scan','abuse','auth_abuse','benign'].map(function(c) {
        return '<option value="' + esc(c) + '">' + esc(classLabel(c)) + '</option>';
      }).join('') + '</select>' +
      '<button class="btn-ghost" type="button" onclick="httpThreatSetIntel()">' + esc(zh('Register P1 host', '\u767b\u8bb0 P1 Host')) + '</button></div>' : '';
    var intelList = intel.length ? intel.map(function(h) {
      var host = h.host || h.Host || '';
      return '<div class="item-meta">' + esc(host + ' / ' + (h.class || h.Class || '-')) +
        (isAdmin ? ' <a href="#" onclick="httpThreatClearIntel(' + jsArg(host) + ');return false;">x</a>' : '') + '</div>';
    }).join('') : '<div class="item-meta">' + esc(zh('No intel hosts.', '\u5c1a\u65e0\u60c5\u62a5 Host\u3002')) + '</div>';
    var siteForm = isAdmin ? '<div style="display:flex;gap:8px;flex-wrap:wrap;margin:10px 0 8px">' +
      '<input id="httpThreatSiteAdd" class="input" type="text" placeholder="' + esc(zh('site_id (this tenant)', '\u672c\u79df\u6237 site_id')) + '" style="max-width:180px">' +
      '<button class="btn-ghost" type="button" onclick="httpThreatAddSite()">' + esc(zh('Bind site for canary', '\u7ed1\u5b9a\u91d1\u4e1d\u96c0\u7ad9\u70b9')) + '</button></div>' : '';
    var siteList = sites.length ? sites.map(function(s) {
      return '<div class="item-meta">' + esc(s) +
        (isAdmin ? ' <a href="#" onclick="httpThreatRemoveSite(' + jsArg(s) + ');return false;">x</a>' : '') + '</div>';
    }).join('') : '<div class="item-meta">' + esc(zh('No bound sites. Canary falls back to tenant hash.', '\u672a\u7ed1\u5b9a\u7ad9\u70b9\uff0c\u91d1\u4e1d\u96c0\u9000\u56de\u79df\u6237\u54c8\u5e0c\u3002')) + '</div>';
    root.innerHTML = form + list +
      '<div class="item-title" style="margin-top:12px">' + esc(zh('P1 intel hosts', 'P1 \u60c5\u62a5 Host')) + '</div>' + intelForm + intelList +
      '<div class="item-title" style="margin-top:12px">' + esc(zh('Canary sites', '\u91d1\u4e1d\u96c0\u7ad9\u70b9')) + '</div>' + siteForm + siteList;
  }
  async function loadHTTPThreatTab() {
    applyI18nKeys();
    var auditTitle = $id('httpThreatAuditTitle'); if (auditTitle) auditTitle.textContent = zh('Disposition audit', '\u5904\u7f6e\u5ba1\u8ba1');
    var auditNote = $id('httpThreatAuditNote'); if (auditNote) auditNote.textContent = zh('Recent detect actions. Mark business false positives here. Does not change serving.', '\u6700\u8fd1\u68c0\u6d4b\u5904\u7f6e\u3002\u8fd9\u91cc\u6807\u300c\u8fd9\u662f\u4e1a\u52a1\u300d\u53ea\u5199\u4eba\u5de5 benign\uff0c\u4e0d\u6539 serving\u3002');
    var rulesTitle = $id('httpThreatRulesTitle'); if (rulesTitle) rulesTitle.textContent = zh('Rule ops', '\u89c4\u5219\u8fd0\u8425');
    var rulesNote = $id('httpThreatRulesNote'); if (rulesNote) rulesNote.textContent = zh('Hits and rule-id to class map. Training stays on the head page above. Remap voids auto gold only.', '\u547d\u4e2d\u4e0e\u89c4\u5219 ID\u2192class\u3002\u8bad\u5934\u4ecd\u5728\u4e0a\u65b9\u5206\u7c7b\u5934\u533a\u3002\u6362\u6620\u5c04\u53ea\u4f5c\u5e9f\u81ea\u52a8\u91d1\u6807\u3002');
    try {
      var st = await api('/api/admin/httpthreat/status');
      var serving = await api('/api/admin/httpthreat/gate?which=serving');
      var cand = await api('/api/admin/httpthreat/gate?which=candidate');
      var observe = await api('/api/admin/httpthreat/observe?kind=replay');
      var online = await api('/api/admin/httpthreat/observe?kind=online');
      var runs = await api('/api/admin/httpthreat/runs');
      var audit = await api('/api/admin/httpthreat/audit');
      var rules = await api('/api/admin/httpthreat/map');
      var ruleQ = ($id('httpThreatRuleFilter') && $id('httpThreatRuleFilter').value) || '';
      var siteQ = ($id('httpThreatSiteFilter') && $id('httpThreatSiteFilter').value) || '';
      var qpath = '/api/admin/httpthreat/queue';
      var qs = [];
      if (ruleQ) qs.push('rule_id=' + encodeURIComponent(ruleQ));
      if (siteQ) qs.push('site_id=' + encodeURIComponent(siteQ));
      if (qs.length) qpath += '?' + qs.join('&');
      var queue = await api(qpath);
      global.__httpThreatWritable = !st || st.corpus_writable !== false;
      renderStatus(st || {}, serving, cand);
      renderObserve(observe, online, Array.isArray(runs) ? runs : []);
      renderQueue(Array.isArray(queue) ? queue : []);
      renderAudit(Array.isArray(audit) ? audit : []);
      renderRules(rules || {}, String((st || {}).role || '') !== 'analyst');
      if (st && st.training) { setTimeout(loadHTTPThreatTab, 1200); }
      if ($id('httpThreatRuleFilter')) $id('httpThreatRuleFilter').value = ruleQ;
      if ($id('httpThreatSiteFilter')) $id('httpThreatSiteFilter').value = siteQ;
    } catch (e) {
      notify(e && e.message ? e.message : String(e), 'error');
    }
  }
  async function postJSON(path, body) {
    await api(path, { method: 'POST', body: JSON.stringify(body || {}) });
    await loadHTTPThreatTab();
  }
  global.loadHTTPThreatTab = loadHTTPThreatTab;
  global.httpThreatTrain = function() {
    api('/api/admin/httpthreat/train', { method: 'POST', body: JSON.stringify({}) }).then(function() {
      return loadHTTPThreatTab();
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatRemap = function() {
    var rid = $id('httpThreatMapRule') ? String($id('httpThreatMapRule').value || '').trim() : '';
    var cls = $id('httpThreatMapClass') ? $id('httpThreatMapClass').value : 'benign';
    if (!rid) { notify(zh('Enter a rule id.', '\u5148\u586b\u89c4\u5219 ID'), 'error'); return; }
    postJSON('/api/admin/httpthreat/map', { rule_id: rid, class: cls }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatSetIntel = function() {
    var host = $id('httpThreatIntelHost') ? String($id('httpThreatIntelHost').value || '').trim() : '';
    var cls = $id('httpThreatIntelClass') ? $id('httpThreatIntelClass').value : 'exploit';
    if (!host) { notify(zh('Enter a host.', '\u5148\u586b Host'), 'error'); return; }
    postJSON('/api/admin/httpthreat/intel', { host: host, class: cls }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatClearIntel = function(host) {
    postJSON('/api/admin/httpthreat/intel', { host: host, class: '' }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatAddSite = function() {
    var site = $id('httpThreatSiteAdd') ? String($id('httpThreatSiteAdd').value || '').trim() : '';
    if (!site) { notify(zh('Enter a site id.', '\u5148\u586b site_id'), 'error'); return; }
    postJSON('/api/admin/httpthreat/sites', { add: [site] }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatRemoveSite = function(site) {
    postJSON('/api/admin/httpthreat/sites', { remove: [site] }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatAdopt = function(force) {
    if (!force) {
      postJSON('/api/admin/httpthreat/adopt', {}).catch(function(e) { notify(e.message, 'error'); });
      return;
    }
    var promptDialog = global.AdminUI && global.AdminUI.promptDialog;
    if (typeof promptDialog !== 'function') { notify(zh('Dialog unavailable', '\u5bf9\u8bdd\u6846\u4e0d\u53ef\u7528'), 'error'); return; }
    promptDialog(zh('Force-adopt reason (10-2000 chars). Report stays red.', '\u5f3a\u5236\u91c7\u7528\u539f\u56e0\uff0810-2000 \u5b57\uff09\u3002\u62a5\u544a\u4ecd\u662f\u7ea2\u7684\u3002'), {
      title: zh('Force adopt', '\u5f3a\u5236\u91c7\u7528'), placeholder: zh('Required reason', '\u5fc5\u586b\u539f\u56e0'), required: true
    }).then(function(reason) {
      if (!reason) return null;
      return postJSON('/api/admin/httpthreat/adopt', { override: 'PROMOTE', reason: reason });
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatRollback = function() { postJSON('/api/admin/httpthreat/rollback', {}).catch(function(e) { notify(e.message, 'error'); }); };
  global.httpThreatPipeline = function(mode) {
    var body = { mode: mode };
    if (mode === 'canary' || mode === 'on') {
      var promptDialog = global.AdminUI && global.AdminUI.promptDialog;
      if (typeof promptDialog === 'function') {
        promptDialog(zh('Optional force-promote reason (10-2000 chars). Leave empty to use gates.', '\u5f3a\u5236\u8f6c\u6b63\u539f\u56e0\uff0810-2000 \u5b57\uff09\uff0c\u7a7a\u7740\u8d70\u95e8\u7981\u3002'), {
          title: zh('Promote', '\u8f6c\u6b63'), placeholder: zh('Reason', '\u539f\u56e0'), required: false
        }).then(function(reason) {
          if (reason) { body.override = 'PROMOTE'; body.reason = reason; }
          return postJSON('/api/admin/httpthreat/pipeline', body);
        }).catch(function(e) { notify(e.message, 'error'); });
        return;
      }
    }
    postJSON('/api/admin/httpthreat/pipeline', body).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatLabel = function(id) {
    var sel = $id('httpThreatGold-' + id);
    postJSON('/api/admin/httpthreat/label', { sample_id: id, SampleID: id, gold_class: sel ? sel.value : 'benign', GoldClass: sel ? sel.value : 'benign' }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatUnlabel = function(id) {
    postJSON('/api/admin/httpthreat/label', { sample_id: id, SampleID: id, gold_class: '', GoldClass: '' }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatAbstain = function(id) {
    postJSON('/api/admin/httpthreat/label', { sample_id: id, abstain: true }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatLabelBatch = function() {
    var boxes = document.querySelectorAll('.httpThreatPick:checked');
    var ids = [];
    boxes.forEach(function(box) { if (box.value) ids.push(box.value); });
    if (!ids.length) { notify(zh('Select samples first.', '\u5148\u52fe\u9009\u6837\u672c'), 'error'); return; }
    if (ids.length > 50) { notify(zh('Batch is capped at 50.', '\u6279\u91cf\u6700\u591a 50 \u6761'), 'error'); return; }
    var sel = $id('httpThreatBatchGold');
    postJSON('/api/admin/httpthreat/label/batch', { sample_ids: ids, gold_class: sel ? sel.value : 'benign' }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatBusiness = function(id) {
    postJSON('/api/admin/httpthreat/label', { sample_id: id, gold_class: 'benign', gold_source: 'human' }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatArbitrate = function(id) {
    postJSON('/api/admin/httpthreat/arbitrate', { sample_id: id }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatPromoteLLM = function(id) {
    postJSON('/api/admin/httpthreat/llm/promote', { sample_id: id }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatRejectLLM = function(id) {
    postJSON('/api/admin/httpthreat/llm/reject', { sample_id: id }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatRemoveTarget = function(node) {
    postJSON('/api/admin/httpthreat/targets', { remove: [node] }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatSetCap = function() {
    var el = $id('httpThreatCap');
    var cap = el ? Number(el.value || 0) : 0;
    postJSON('/api/admin/httpthreat/cap', { cap: cap }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatEnableExport = function() {
    var promptDialog = global.AdminUI && global.AdminUI.promptDialog;
    if (typeof promptDialog !== 'function') { notify(zh('Dialog unavailable', '\u5bf9\u8bdd\u6846\u4e0d\u53ef\u7528'), 'error'); return; }
    promptDialog(zh('Enable export reason (10-2000 chars). Still redacted previews only.', '\u5f00\u5bfc\u51fa\u539f\u56e0\uff0810-2000 \u5b57\uff09\u3002\u4ecd\u53ea\u51fa\u8131\u654f\u9884\u89c8\u3002'), {
      title: zh('Enable export', '\u5f00\u5bfc\u51fa'), placeholder: zh('Required reason', '\u5fc5\u586b\u539f\u56e0'), required: true
    }).then(function(reason) {
      if (!reason) return null;
      return postJSON('/api/admin/httpthreat/export', { enabled: true, reason: reason });
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatDownloadExport = function() {
    api('/api/admin/httpthreat/export').then(function(rows) {
      var blob = new Blob([JSON.stringify(rows || [], null, 2)], { type: 'application/json' });
      var a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = 'httpthreat-export.json';
      a.click();
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatFillRun = function(id) {
    api('/api/admin/httpthreat/sample?id=' + encodeURIComponent(id)).then(function(s) {
      var box = $id('httpThreatRunFill');
      if (!box) return;
      box.textContent = zh('Snapshot fill (read-only): ', '\u5feb\u7167\u56de\u586b\uff08\u53ea\u8bfb\uff09\uff1a') + (s.preview || '') +
        ' / ' + (s.gold_class || '-') + ' / ' + (s.gold_source || '-');
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatImport = function() {
    var promptDialog = global.AdminUI && global.AdminUI.promptDialog;
    if (typeof promptDialog !== 'function') { notify(zh('Dialog unavailable', '\u5bf9\u8bdd\u6846\u4e0d\u53ef\u7528'), 'error'); return; }
    promptDialog(zh('Paste official head JSON. trained_at is rewritten on this trainer.', '\u7c98\u8d34\u5b98\u65b9\u5934 JSON\u3002trained_at \u4ee5\u672c\u4fa7\u8bad\u7ec3\u5668\u65f6\u949f\u4e3a\u51c6\u3002'), {
      title: zh('Import candidate', '\u5bfc\u5165 candidate'), placeholder: '{...}', required: true
    }).then(function(raw) {
      if (!raw) return null;
      var head;
      try { head = JSON.parse(raw); } catch (e) { notify(zh('Invalid JSON', '\u4e0d\u662f JSON'), 'error'); return null; }
      return postJSON('/api/admin/httpthreat/import', head);
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatAddTarget = function() {
    var el = $id('httpThreatTargetAdd');
    var node = el ? String(el.value || '').trim() : '';
    if (!node) { notify(zh('Enter a node id.', '\u5148\u586b\u8282\u70b9 ID'), 'error'); return; }
    postJSON('/api/admin/httpthreat/targets', { add: [node] }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.httpThreatForcePromote = function() {
    var promptDialog = global.AdminUI && global.AdminUI.promptDialog;
    if (typeof promptDialog !== 'function') { notify(zh('Dialog unavailable', '\u5bf9\u8bdd\u6846\u4e0d\u53ef\u7528'), 'error'); return; }
    promptDialog(zh('Force-promote reason (10-2000 chars). Report stays red.', '\u5f3a\u5236\u8f6c\u6b63\u539f\u56e0\uff0810-2000 \u5b57\uff09\u3002\u62a5\u544a\u4ecd\u662f\u7ea2\u7684\u3002'), {
      title: zh('Force promote', '\u5f3a\u5236\u8f6c\u6b63'), placeholder: zh('Required reason', '\u5fc5\u586b\u539f\u56e0'), required: true
    }).then(function(reason) {
      if (!reason) return null;
      var confirmDialog = global.AdminUI && global.AdminUI.confirmDialog;
      var next = Promise.resolve(true);
      if (typeof confirmDialog === 'function') {
        next = confirmDialog(zh('Force promote only bypasses the check. The gate report stays red.', '\u5f3a\u5236\u8f6c\u6b63\u53ea\u7ed5\u8fc7\u68c0\u67e5\uff0c\u95e8\u7981\u62a5\u544a\u4ecd\u662f\u7ea2\u7684\u3002'), {
          title: zh('Confirm force promote', '\u4e8c\u6b21\u786e\u8ba4'), danger: true
        });
      }
      return next.then(function(ok) {
        if (!ok) return null;
        return postJSON('/api/admin/httpthreat/pipeline', { mode: 'on', override: 'PROMOTE', reason: reason });
      });
    }).catch(function(e) { notify(e.message, 'error'); });
  };
  global.adminTenantOnlyTabs = Object.assign({}, global.adminTenantOnlyTabs || {}, { httpthreat: true });
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'httpthreat',
      title: function() { return zh('HTTP Threat Head', '\u6076\u610f HTTP \u5206\u7c7b\u5934'); },
      subtitle: function() { return zh('Rules plus generalization head', '\u89c4\u5219 + \u6cdb\u5316\u5934'); },
      onOpen: loadHTTPThreatTab
    });
    if (typeof global.AdminTabRegistry.onLanguageChange === 'function') {
      global.AdminTabRegistry.onLanguageChange(applyI18nKeys);
    }
  }
})(window);
