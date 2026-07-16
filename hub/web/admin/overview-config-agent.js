/*
 * overview-config-agent.js
 * Tenant overview: system-free readiness gate + config assistant chat shell.
 * ASCII only - use \uXXXX for CJK.
 */
(function(global) {
  'use strict';

  var CAI18N = {
    en: {
      title: 'Config Assistant',
      subtitle: 'Describe tenant settings in natural language. Review plan and confirm before apply.',
      placeholder: 'e.g. Add LLM provider DeepSeek, url https://..., model deepseek-chat, key sk-...',
      send: 'Send',
      sending: 'Sending...',
      you: 'You',
      noPlan: 'No active plan to execute',
      moreActions: 'More actions',
      confirm: 'Confirm execute',
      cancel: 'Cancel plan',
      empty: 'Examples: add LLM provider | set system-free provider | test system-free',
      planning: 'Understanding request...',
      planTitle: 'Proposed plan',
      missing: 'Missing fields',
      risk: 'Risk',
      executing: 'Executing...',
      done: 'Done',
      failed: 'Failed',
      copy: 'Copy JSON',
      copied: 'Copied',
      download: 'Download .txt',
      copyCodes: 'Copy codes',
      autoRead: 'Read-only plan auto-run',
      eligible: 'Eligible',
      ineligible: 'Not eligible',
      routes: 'Billing routes',
      bindings: 'User bindings',
      grants: 'Active grants',
      exportedCodes: 'Exported codes',
      steps: 'Steps',
      raw: 'Raw JSON',
      bindSystemFree: 'Bind to system-free',
      suggestBind: 'No system-free binding or some routes blocked. Bind this user?',
      historyDetail: 'History detail',
      replan: 'Re-plan from this',
      code: 'Code',
      preview: 'Preview',
      vip: 'VIP',
      bindDiff: 'Binding change',
      current: 'Current',
      target: 'Target',
      added: 'Added',
      unchanged: 'Already bound (no change)',
      kept: 'Kept',
      removed: 'Removed',
      unknownGroups: 'Unknown service groups',
      availableGroups: 'Available',
      rerun: 'Re-run now',
      reDiagnose: 'Re-checking entitlement after bind\u2026',
      unbindSystemFree: 'Unbind system-free',
      unbind: 'Unbind',
      before: 'Before',
      after: 'After',
      change: 'Change',
      group: 'Group',
      assumptions: 'Assumptions',
      useGroup: 'Use',
      copyId: 'Copy id',
      pickGroup: 'Service groups (click to use)',
      rememberedEmail: 'Email',
      selectedGroups: 'Selected groups',
      clearGroups: 'Clear groups',
      clearEmail: 'Clear email',
      emailPlaceholder: 'user@example.com',
      sendBind: 'Send bind plan',
      sendUnbind: 'Send unbind plan',
      modeBind: 'bind',
      modeUnbind: 'unbind',
      needEmail: 'Enter email first',
      diagnoseEmail: 'Diagnose email',
      listGroups: 'List groups',
      barHint: 'Pick groups below (Tools / list groups), then send',
      recentGroups: 'Recent groups',
      clearRecent: 'Clear recent',
      lastDiag: 'Last diagnose',
      pinHint: 'Alt+click pin \u00b7 Ctrl+click move left \u00b7 Shift+click move right',
      clearDiag: 'Clear summary',
      showBadRoutes: 'Show blocked routes',
      hideBadRoutes: 'Hide blocked routes',
      noBadRoutes: 'No blocked routes',
      copyBlocked: 'Copy blocked',
      copyBlockedMd: 'Copy MD table',
      copyDiag: 'Copy summary',
      downloadDiag: 'Download summary',
      copySupport: 'Copy for support',
      downloadSupport: 'Download support JSON',
      downloadPlan: 'Download plan JSON',
      clearChat: 'Clear chat',
      recentPlans: 'Recent plans',
      noHistory: 'No recent plans yet.',
      historyExported: 'History exported',
      supportPacksInExport: 'support packs',
      groupBinds: 'Group bindings',
      securityGroups: 'Security groups',
      sessionActive: 'Multi-turn session',
      endSession: 'End session',
      newSession: 'New session',
      sessionTTL: 'expires',
      sessionExpired: 'Session expired',
      planTTL: 'Confirm before',
      planExpired: 'Plan expired - re-plan',
      planSuperseded: 'This plan is no longer active. Use the latest plan card.',
      busy: 'Another request is in progress\u2026',
      quickFilters: 'Quick',
      useTool: 'Use',
      groupBySession: 'Group by session',
      ungrouped: 'No session',
      sessionGroup: 'Session',
      shortcuts: 'Shortcuts: Esc cancel \u00b7 Ctrl+Enter send/confirm \u00b7 Ctrl+Shift+T tools \u00b7 Ctrl+K focus \u00b7 Ctrl+/ help',
      shortcutsHelp: 'Keyboard shortcuts',
      shortcutsClose: 'Close',
      domain: 'Domain',
      expandAll: 'Expand all',
      collapseAll: 'Collapse all',
      catalogSearch: 'Filter tools\u2026',
      noCatalogMatch: 'No tools match this filter.',
      favorites: 'Favorites',
      recentCmds: 'Recent',
      addFavorite: '\u2605 Favorite',
      removeFavorite: '\u2606 Unfavorite',
      clearFavorites: 'Clear favorites',
      starRecent: '\u2605',
      favHint: 'Star catalog tools or Alt+click examples to pin commands here.',
      exportPrefs: 'Export favorites/recent',
      importPrefs: 'Import',
      prefsExported: 'Prefs exported',
      prefsImported: 'Prefs imported',
      continueFill: 'Continue with values',
      fillMissing: 'Fill missing fields',
      useRemembered: 'Use remembered email',
      roleMember: 'member',
      roleAdmin: 'admin',
      handoff: 'Copy handoff text',
      handoffMail: 'Email handoff',
      sessionTurns: 'Session turns',
      pickProvider: 'Providers (click to use)',
      wizardDiagnose: '1 Diagnose',
      wizardBind: '2 Bind',
      wizardRecheck: '3 Re-check',
      wizardDone: 'done',
      missingHint: 'Reply with values or use quick-fill below (same session).',
      gateTitle: 'System free LLM is not ready',
      gateDesc: 'Hub / MaClawSrv server-side agents require system-free. Configure a provider, then test.',
      gateTest: 'Test system-free',
      gateConfig: 'Open Model Services',
      gateLater: 'Remind me later',
      gateReady: 'system-free is ready',
      providers: 'Providers'
    },
    zh: {
      title: '\u914d\u7f6e\u52a9\u624b',
      subtitle: '\u7528\u81ea\u7136\u8bed\u8a00\u63cf\u8ff0\u79df\u6237\u914d\u7f6e\u3002\u5148\u770b\u65b9\u6848\u4e0e\u6a21\u62df\uff0c\u786e\u8ba4\u540e\u518d\u6267\u884c\u3002',
      placeholder: '\u4f8b\u5982\uff1a\u6dfb\u52a0 LLM \u670d\u52a1\u5546 DeepSeek\uff0c\u5730\u5740 https://...\uff0c\u6a21\u578b deepseek-chat\uff0ckey sk-...',
      send: '\u53d1\u9001',
      sending: '\u6b63\u5728\u53d1\u9001...',
      you: '\u4f60',
      noPlan: '\u6ca1\u6709\u53ef\u6267\u884c\u7684\u5f53\u524d\u65b9\u6848',
      moreActions: '\u66f4\u591a\u64cd\u4f5c',
      confirm: '\u786e\u8ba4\u6267\u884c',
      cancel: '\u53d6\u6d88\u65b9\u6848',
      empty: '\u793a\u4f8b\uff1a\u6dfb\u52a0 LLM \u670d\u52a1\u5546 | \u4fee\u6539 system-free \u670d\u52a1\u5546 | \u6d4b\u8bd5 system-free',
      planning: '\u6b63\u5728\u7406\u89e3\u8bf7\u6c42\u2026',
      planTitle: '\u62df\u6267\u884c\u65b9\u6848',
      missing: '\u7f3a\u5931\u5b57\u6bb5',
      risk: '\u98ce\u9669',
      executing: '\u6267\u884c\u4e2d\u2026',
      done: '\u5b8c\u6210',
      failed: '\u5931\u8d25',
      copy: '\u590d\u5236 JSON',
      copied: '\u5df2\u590d\u5236',
      download: '\u4e0b\u8f7d .txt',
      copyCodes: '\u590d\u5236\u9080\u8bf7\u7801',
      autoRead: '\u53ea\u8bfb\u65b9\u6848\u5df2\u81ea\u52a8\u6267\u884c',
      eligible: '\u53ef\u7528',
      ineligible: '\u4e0d\u53ef\u7528',
      routes: '\u8ba1\u8d39\u8def\u7531',
      bindings: '\u7528\u6237\u7ed1\u5b9a',
      grants: '\u6709\u6548\u6388\u6743',
      exportedCodes: '\u5df2\u5bfc\u51fa\u9080\u8bf7\u7801',
      steps: '\u6b65\u9aa4',
      raw: '\u539f\u59cb JSON',
      bindSystemFree: '\u7ed1\u5b9a\u5230 system-free',
      suggestBind: '\u672a\u7ed1\u5b9a system-free \u6216\u90e8\u5206\u8def\u7531\u4e0d\u53ef\u7528\u3002\u662f\u5426\u7ed1\u5b9a\u8be5\u7528\u6237\uff1f',
      historyDetail: '\u5386\u53f2\u8be6\u60c5',
      replan: '\u4ece\u6b64\u91cd\u65b0\u89c4\u5212',
      code: '\u9080\u8bf7\u7801',
      preview: '\u9884\u89c8',
      vip: 'VIP',
      bindDiff: '\u7ed1\u5b9a\u53d8\u66f4',
      current: '\u5f53\u524d',
      target: '\u76ee\u6807',
      added: '\u65b0\u589e',
      unchanged: '\u5df2\u7ed1\u5b9a\uff08\u65e0\u53d8\u66f4\uff09',
      kept: '\u4fdd\u7559',
      removed: '\u79fb\u9664',
      unknownGroups: '\u672a\u77e5\u670d\u52a1\u7ec4',
      availableGroups: '\u53ef\u7528\u670d\u52a1\u7ec4',
      rerun: '\u7acb\u5373\u91cd\u8dd1',
      reDiagnose: '\u7ed1\u5b9a\u540e\u6b63\u5728\u91cd\u65b0\u8bca\u65ad\u6743\u9650\u2026',
      unbindSystemFree: '\u89e3\u7ed1 system-free',
      unbind: '\u89e3\u7ed1',
      before: '\u53d8\u66f4\u524d',
      after: '\u53d8\u66f4\u540e',
      change: '\u53d8\u5316',
      group: '\u670d\u52a1\u7ec4',
      assumptions: '\u5047\u8bbe',
      useGroup: '\u4f7f\u7528',
      copyId: '\u590d\u5236 id',
      pickGroup: '\u670d\u52a1\u7ec4\uff08\u70b9\u51fb\u4f7f\u7528\uff09',
      rememberedEmail: '\u90ae\u7bb1',
      selectedGroups: '\u5df2\u9009\u670d\u52a1\u7ec4',
      clearGroups: '\u6e05\u7a7a\u670d\u52a1\u7ec4',
      clearEmail: '\u6e05\u7a7a\u90ae\u7bb1',
      emailPlaceholder: 'user@example.com',
      sendBind: '\u53d1\u9001\u7ed1\u5b9a\u65b9\u6848',
      sendUnbind: '\u53d1\u9001\u89e3\u7ed1\u65b9\u6848',
      modeBind: 'bind',
      modeUnbind: 'unbind',
      needEmail: '\u8bf7\u5148\u586b\u5199\u90ae\u7bb1',
      diagnoseEmail: '\u8bca\u65ad\u90ae\u7bb1',
      listGroups: '\u5217\u670d\u52a1\u7ec4',
      barHint: '\u5148\u5728 Tools / \u5217\u8868\u4e2d\u70b9\u9009\u670d\u52a1\u7ec4\uff0c\u518d\u53d1\u9001',
      recentGroups: '\u6700\u8fd1\u4f7f\u7528',
      clearRecent: '\u6e05\u7a7a\u6700\u8fd1',
      lastDiag: '\u6700\u8fd1\u8bca\u65ad',
      pinHint: 'Alt+\u56fa\u5b9a \u00b7 Ctrl+\u5de6\u79fb \u00b7 Shift+\u53f3\u79fb',
      clearDiag: '\u6e05\u9664\u6458\u8981',
      showBadRoutes: '\u5c55\u5f00\u4e0d\u53ef\u7528\u8def\u7531',
      hideBadRoutes: '\u6536\u8d77\u4e0d\u53ef\u7528\u8def\u7531',
      noBadRoutes: '\u65e0\u4e0d\u53ef\u7528\u8def\u7531',
      copyBlocked: '\u590d\u5236\u4e0d\u53ef\u7528',
      copyBlockedMd: '\u590d\u5236 MD \u8868\u683c',
      copyDiag: '\u590d\u5236\u6458\u8981',
      downloadDiag: '\u4e0b\u8f7d\u6458\u8981',
      copySupport: '\u590d\u5236\u7ed9\u652f\u6301',
      downloadSupport: '\u4e0b\u8f7d\u652f\u6301\u5305 JSON',
      downloadPlan: '\u4e0b\u8f7d\u65b9\u6848 JSON',
      clearChat: '\u6e05\u7a7a\u5bf9\u8bdd',
      recentPlans: '\u6700\u8fd1\u65b9\u6848',
      noHistory: '\u6682\u65e0\u5386\u53f2\u8bb0\u5f55\u3002',
      historyExported: '\u5386\u53f2\u5df2\u5bfc\u51fa',
      supportPacksInExport: '\u652f\u6301\u5305',
      groupBinds: '\u5b89\u5168\u7ec4\u7ed1\u5b9a',
      securityGroups: '\u5b89\u5168\u7ec4',
      sessionActive: '\u591a\u8f6e\u4f1a\u8bdd',
      endSession: '\u7ed3\u675f\u4f1a\u8bdd',
      newSession: '\u65b0\u4f1a\u8bdd',
      sessionTTL: '\u5269\u4f59',
      sessionExpired: '\u4f1a\u8bdd\u5df2\u8fc7\u671f',
      planTTL: '\u8bf7\u5728\u6b64\u524d\u786e\u8ba4',
      planExpired: '\u65b9\u6848\u5df2\u8fc7\u671f - \u8bf7\u91cd\u65b0\u89c4\u5212',
      planSuperseded: '\u8be5\u65b9\u6848\u5df2\u5931\u6548\uff0c\u8bf7\u4f7f\u7528\u6700\u65b0\u65b9\u6848\u5361\u3002',
      busy: '\u53e6\u4e00\u8bf7\u6c42\u8fdb\u884c\u4e2d\u2026',
      quickFilters: '\u5feb\u901f\u7b5b\u9009',
      useTool: '\u4f7f\u7528',
      groupBySession: '\u6309\u4f1a\u8bdd\u5206\u7ec4',
      ungrouped: '\u65e0\u4f1a\u8bdd',
      sessionGroup: '\u4f1a\u8bdd',
      shortcuts: '\u5feb\u6377\u952e: Esc \u53d6\u6d88 \u00b7 Ctrl+Enter \u53d1\u9001/\u786e\u8ba4 \u00b7 Ctrl+Shift+T \u5de5\u5177 \u00b7 Ctrl+K \u805a\u7126 \u00b7 Ctrl+/ \u5e2e\u52a9',
      shortcutsHelp: '\u5feb\u6377\u952e\u5e2e\u52a9',
      shortcutsClose: '\u5173\u95ed',
      domain: '\u9886\u57df',
      expandAll: '\u5168\u90e8\u5c55\u5f00',
      collapseAll: '\u5168\u90e8\u6298\u53e0',
      catalogSearch: '\u8fc7\u6ee4\u5de5\u5177\u2026',
      noCatalogMatch: '\u6ca1\u6709\u5339\u914d\u7684\u5de5\u5177\u3002',
      favorites: '\u6536\u85cf\u547d\u4ee4',
      recentCmds: '\u6700\u8fd1\u547d\u4ee4',
      addFavorite: '\u2605 \u6536\u85cf',
      removeFavorite: '\u2606 \u53d6\u6d88\u6536\u85cf',
      clearFavorites: '\u6e05\u7a7a\u6536\u85cf',
      starRecent: '\u2605',
      favHint: '\u70b9\u51fb catalog \u661f\u6807\uff0c\u6216 Alt+\u70b9\u51fb\u793a\u4f8b\u6309\u94ae\u53ef\u6536\u85cf\u547d\u4ee4\u3002',
      exportPrefs: '\u5bfc\u51fa\u6536\u85cf/\u6700\u8fd1',
      importPrefs: '\u5bfc\u5165',
      prefsExported: '\u504f\u597d\u5df2\u5bfc\u51fa',
      prefsImported: '\u504f\u597d\u5df2\u5bfc\u5165',
      continueFill: '\u7528\u8be5\u503c\u7ee7\u7eed',
      fillMissing: '\u8865\u5168\u7f3a\u5931\u5b57\u6bb5',
      useRemembered: '\u4f7f\u7528\u8bb0\u4f4f\u7684\u90ae\u7bb1',
      roleMember: 'member',
      roleAdmin: 'admin',
      handoff: '\u590d\u5236\u4ea4\u63a5\u6587\u6848',
      handoffMail: '\u90ae\u4ef6\u4ea4\u63a5',
      sessionTurns: '\u4f1a\u8bdd\u8f6e\u6b21',
      pickProvider: '\u670d\u52a1\u5546\uff08\u70b9\u51fb\u4f7f\u7528\uff09',
      wizardDiagnose: '1 \u8bca\u65ad',
      wizardBind: '2 \u7ed1\u5b9a',
      wizardRecheck: '3 \u91cd\u68c0',
      wizardDone: '\u5b8c\u6210',
      missingHint: '\u8bf7\u56de\u590d\u7f3a\u5931\u503c\uff0c\u6216\u7528\u4e0b\u65b9\u5feb\u901f\u586b\u5145\uff08\u540c\u4e00 session\uff09\u3002',
      gateTitle: '\u7cfb\u7edf\u514d\u8d39 LLM\uff08system-free\uff09\u672a\u5c31\u7eea',
      gateDesc: 'Hub / MaClawSrv \u670d\u52a1\u7aef Agent \u4f9d\u8d56 system-free\u3002\u8bf7\u914d\u7f6e\u670d\u52a1\u5546\u5e76\u6d4b\u8bd5\u3002',
      gateTest: '\u6d4b\u8bd5 system-free',
      gateConfig: '\u6253\u5f00\u6a21\u578b\u670d\u52a1',
      gateLater: '\u7a0d\u540e\u63d0\u9192',
      gateReady: 'system-free \u5df2\u5c31\u7eea',
      providers: '\u670d\u52a1\u5546'
    }
  };

  function cat(key) {
    var lang = global.currentLang === 'zh' ? 'zh' : 'en';
    return (CAI18N[lang] && CAI18N[lang][key]) || (CAI18N.en && CAI18N.en[key]) || key;
  }
  function byID(id) { return document.getElementById(id); }
  function esc(s) {
    return typeof global.escapeHtml === 'function'
      ? global.escapeHtml(String(s || ''))
      : String(s || '').replace(/[&<>"']/g, function(c) {
        return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c];
      });
  }

  var pendingPlan = null;
  var configAgentSessionID = '';
  var sessionTurns = []; // multi-turn user messages for current session
  var sessionExpiresAtMs = 0; // epoch ms; 0 = unknown
  var sessionTTLTimer = null;
  var planExpiresAtMs = 0;
  var planTTLTimer = null;
  var gateDismissedAt = 0;
  var HISTORY_QUICK_FILTERS = [
    { label: 'diagnose', q: 'diagnose' },
    { label: 'bind', q: 'user_bind' },
    { label: 'unbind', q: 'user_unbind' },
    { label: 'invite', q: 'invitation' },
    { label: 'export', q: 'export' },
    { label: 'migration', q: 'migration' }
  ];
  var planHistory = [];
  var lastServiceGroups = []; // [{id,name,system_free}]
  var lastProviders = []; // [{id,name}]
  var lastRememberedEmail = '';
  var recentGroupIds = [];
  var pinnedGroupIds = [];
  var lastDiagnoseHint = null; // { email, suggestBind, hasSystemFree, ok, bad, binds, grants, badRoutes, okRoutes, at }
  var diagRoutesExpanded = false;
  var EMAIL_STORAGE_KEY = 'hub.configAgent.lastEmail';
  var RECENT_GROUPS_KEY = 'hub.configAgent.recentGroups';
  var PINNED_GROUPS_KEY = 'hub.configAgent.pinnedGroups';
  var DIAG_HINT_STORAGE_KEY = 'hub.configAgent.lastDiagnoseHint';
  var MAX_RECENT_GROUPS = 8;
  var DIAG_HINT_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

  function loadRememberedEmail() {
    try {
      lastRememberedEmail = String(localStorage.getItem(EMAIL_STORAGE_KEY) || '').trim();
    } catch (_) {
      lastRememberedEmail = '';
    }
  }

  function loadDiagnoseHint() {
    try {
      var raw = localStorage.getItem(DIAG_HINT_STORAGE_KEY);
      if (!raw) return;
      var hint = JSON.parse(raw);
      if (!hint || !hint.email) return;
      if (hint.at && (Date.now() - Number(hint.at)) > DIAG_HINT_MAX_AGE_MS) {
        localStorage.removeItem(DIAG_HINT_STORAGE_KEY);
        return;
      }
      lastDiagnoseHint = hint;
    } catch (_) {
      lastDiagnoseHint = null;
    }
  }

  function saveDiagnoseHint(hint) {
    lastDiagnoseHint = hint || null;
    try {
      if (!lastDiagnoseHint) {
        localStorage.removeItem(DIAG_HINT_STORAGE_KEY);
      } else {
        localStorage.setItem(DIAG_HINT_STORAGE_KEY, JSON.stringify(lastDiagnoseHint));
      }
    } catch (_) {}
  }

  function clearDiagnoseHint() {
    saveDiagnoseHint(null);
    diagRoutesExpanded = false;
  }

  /** Build status-bar / support-pack hint from a diagnose API result. */
  function hintFromDiagnoseResult(diagnose, suggestEmail) {
    if (!diagnose) return null;
    var routes = diagnose.billing_routes || [];
    var okRoutes = routes.filter(function(x) { return x && x.eligible; });
    var badRoutes = routes.filter(function(x) { return x && !x.eligible; });
    var suggest = suggestEmail != null ? !!suggestEmail : !hasSystemFreeBinding(diagnose);
    return {
      email: String(diagnose.email || ''),
      suggestBind: suggest,
      hasSystemFree: hasSystemFreeBinding(diagnose),
      ok: okRoutes.length,
      bad: badRoutes.length,
      binds: (diagnose.direct_user_bindings || []).length,
      grants: (diagnose.active_grants || []).length,
      securityGroupIds: (diagnose.resolved_security_group_ids || []).slice(0, 20),
      bindingDetails: (diagnose.direct_user_bindings || []).slice(0, 10).map(function(b) {
        return {
          email: b.email || diagnose.email,
          service_group_ids: (b.service_group_ids || []).slice()
        };
      }),
      groupBindings: (diagnose.matched_group_bindings || []).slice(0, 20).map(function(b) {
        return {
          group_id: b.group_id,
          service_group_ids: (b.service_group_ids || []).slice()
        };
      }),
      grantDetails: (diagnose.active_grants || []).slice(0, 20).map(function(g) {
        return {
          id: g.id,
          service_group_id: g.service_group_id,
          source: g.source,
          credits_total: g.credits_total,
          credits_used: g.credits_used,
          starts_at: g.starts_at,
          expires_at: g.expires_at
        };
      }),
      badRoutes: badRoutes.slice(0, 20).map(function(x) {
        return {
          model_name: x.model_name,
          provider_id: x.provider_id,
          reason_code: x.reason_code,
          reason_message: x.reason_message,
          service_group_ids: x.service_group_ids
        };
      }),
      okRoutes: okRoutes.slice(0, 10).map(function(x) {
        return {
          model_name: x.model_name,
          provider_id: x.provider_id,
          service_group_ids: x.service_group_ids
        };
      }),
      at: Date.now()
    };
  }

  function extractDiagnoseFromPayload(payload) {
    if (!payload) return null;
    var results = payload.results || [];
    for (var i = 0; i < results.length; i++) {
      var r = results[i] || {};
      if (String(r.tool || '') === 'llm.services.diagnose' && r.result) return r.result;
    }
    return null;
  }

  function supportPackFromDiagnoseResult(diagnose) {
    var hint = hintFromDiagnoseResult(diagnose);
    if (!hint) return null;
    try {
      return JSON.parse(formatSupportPackJSON(hint));
    } catch (_) {
      return null;
    }
  }

  function loadRecentGroups() {
    try {
      var raw = localStorage.getItem(RECENT_GROUPS_KEY);
      var arr = raw ? JSON.parse(raw) : [];
      recentGroupIds = Array.isArray(arr) ? arr.map(String).filter(Boolean) : [];
    } catch (_) {
      recentGroupIds = [];
    }
    try {
      var rawPin = localStorage.getItem(PINNED_GROUPS_KEY);
      var pins = rawPin ? JSON.parse(rawPin) : [];
      pinnedGroupIds = Array.isArray(pins) ? pins.map(String).filter(Boolean) : [];
    } catch (_) {
      pinnedGroupIds = [];
    }
    if (!recentGroupIds.length && !pinnedGroupIds.length) {
      recentGroupIds = ['system-free'];
    }
    // Keep pinned at front of display list.
    recentGroupIds = mergePinnedFront(recentGroupIds, pinnedGroupIds);
  }

  function mergePinnedFront(recent, pinned) {
    var seen = {};
    var out = [];
    (pinned || []).forEach(function(id) {
      id = String(id || '').trim();
      if (!id || seen[id]) return;
      seen[id] = true;
      out.push(id);
    });
    (recent || []).forEach(function(id) {
      id = String(id || '').trim();
      if (!id || seen[id]) return;
      seen[id] = true;
      out.push(id);
    });
    return out.slice(0, MAX_RECENT_GROUPS);
  }

  function persistRecentGroups() {
    try {
      localStorage.setItem(RECENT_GROUPS_KEY, JSON.stringify(recentGroupIds.slice(0, MAX_RECENT_GROUPS)));
    } catch (_) {}
    try {
      localStorage.setItem(PINNED_GROUPS_KEY, JSON.stringify(pinnedGroupIds.slice(0, MAX_RECENT_GROUPS)));
    } catch (_) {}
  }

  function rememberGroups(ids, opts) {
    opts = opts || {};
    var list = Array.isArray(ids) ? ids : [ids];
    list.forEach(function(id) {
      id = String(id || '').trim();
      if (!id) return;
      // Don't shuffle pinned order unless it's a new id.
      if (pinnedGroupIds.indexOf(id) >= 0) return;
      recentGroupIds = recentGroupIds.filter(function(x) { return x !== id; });
      // Insert after pinned block
      var unpinned = recentGroupIds.filter(function(x) { return pinnedGroupIds.indexOf(x) < 0; });
      unpinned.unshift(id);
      recentGroupIds = mergePinnedFront(unpinned, pinnedGroupIds);
    });
    recentGroupIds = recentGroupIds.slice(0, MAX_RECENT_GROUPS);
    persistRecentGroups();
    if (!opts.silent) updateSelectedBar();
  }

  function pinGroup(id) {
    id = String(id || '').trim();
    if (!id) return;
    if (pinnedGroupIds.indexOf(id) >= 0) {
      pinnedGroupIds = pinnedGroupIds.filter(function(x) { return x !== id; });
    } else {
      pinnedGroupIds = [id].concat(pinnedGroupIds.filter(function(x) { return x !== id; }));
      pinnedGroupIds = pinnedGroupIds.slice(0, MAX_RECENT_GROUPS);
    }
    recentGroupIds = mergePinnedFront(recentGroupIds, pinnedGroupIds);
    persistRecentGroups();
    updateSelectedBar();
  }

  function movePinnedGroup(id, delta) {
    id = String(id || '').trim();
    var idx = pinnedGroupIds.indexOf(id);
    if (idx < 0) return;
    var j = idx + delta;
    if (j < 0 || j >= pinnedGroupIds.length) return;
    var copy = pinnedGroupIds.slice();
    var tmp = copy[idx];
    copy[idx] = copy[j];
    copy[j] = tmp;
    pinnedGroupIds = copy;
    recentGroupIds = mergePinnedFront(recentGroupIds, pinnedGroupIds);
    persistRecentGroups();
    updateSelectedBar();
  }

  function clearRecentGroups() {
    recentGroupIds = pinnedGroupIds.length ? pinnedGroupIds.slice() : ['system-free'];
    if (!pinnedGroupIds.length) recentGroupIds = ['system-free'];
    persistRecentGroups();
    updateSelectedBar();
  }

  function rememberEmail(email, opts) {
    opts = opts || {};
    email = String(email || '').trim();
    if (!email || email.indexOf('@') < 0) return;
    lastRememberedEmail = email;
    try { localStorage.setItem(EMAIL_STORAGE_KEY, email); } catch (_) {}
    if (!opts.silent) updateSelectedBar();
  }

  function clearRememberedEmail() {
    lastRememberedEmail = '';
    try { localStorage.removeItem(EMAIL_STORAGE_KEY); } catch (_) {}
    updateSelectedBar();
  }

  function extractEmailFromText(text) {
    var m = String(text || '').match(/[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}/);
    return m ? m[0] : '';
  }

  function splitGroupTokens(raw) {
    raw = String(raw || '').trim();
    if (!raw) return [];
    raw = raw.replace(/\s+and\s+/gi, ',').replace(/\s+\u4e0e\s+/g, ',').replace(/\s+\u548c\s+/g, ',');
    return raw.split(/[,\uff0c\u3001]+/).map(function(s) {
      return String(s || '').trim();
    }).filter(function(s) {
      return !!s && /^[A-Za-z0-9._\-]+$/.test(s);
    });
  }

  function parseBindLine(text) {
    var t = String(text || '').trim();
    var bind = t.match(/^bind\s+user\s+(\S*)\s+to\s+([\s\S]*)$/i);
    if (bind) {
      return {
        mode: 'bind',
        email: bind[1] && bind[1].indexOf('@') >= 0 ? bind[1] : '',
        groups: splitGroupTokens(bind[2])
      };
    }
    var unbind = t.match(/^unbind\s+user\s+(\S*)\s+from\s+([\s\S]*)$/i);
    if (unbind) {
      return {
        mode: 'unbind',
        email: unbind[1] && unbind[1].indexOf('@') >= 0 ? unbind[1] : '',
        groups: splitGroupTokens(unbind[2])
      };
    }
    return null;
  }

  function buildBindLine(mode, email, groups) {
    email = String(email || '').trim();
    groups = groups || [];
    var groupPart = groups.join(', ');
    if (mode === 'unbind') {
      return 'unbind user ' + (email || '') + ' from ' + groupPart;
    }
    return 'bind user ' + (email || '') + ' to ' + groupPart;
  }

  function ensureSelectedBar() {
    if (byID('configAgentSelectedBar')) return;
    var input = byID('configAgentInput');
    if (!input || !input.parentNode) return;
    var bar = document.createElement('div');
    bar.id = 'configAgentSelectedBar';
    bar.style.cssText = 'margin-top:8px;padding:8px 10px;display:flex;flex-wrap:wrap;gap:6px;align-items:center;'
      + 'background:rgba(47,128,237,.04);border:1px solid rgba(47,128,237,.12);border-radius:8px';
    input.parentNode.appendChild(bar);
  }

  function syncInputFromBarState(email, groups, mode) {
    var input = byID('configAgentInput');
    if (!input) return;
    email = String(email || '').trim();
    groups = groups || [];
    mode = mode === 'unbind' ? 'unbind' : 'bind';
    if (!email && !groups.length) {
      // Keep free-form text if not a bind line.
      if (parseBindLine(input.value)) input.value = '';
      return;
    }
    input.value = buildBindLine(mode, email, groups);
  }

  function focusEmailField(flash) {
    var emailEl = byID('configAgentEmailInput');
    if (!emailEl) return;
    emailEl.focus();
    if (flash) {
      emailEl.style.borderColor = '#b42318';
      setTimeout(function() { if (emailEl) emailEl.style.borderColor = ''; }, 1200);
    }
  }

  function requireBarEmail() {
    var emailEl = byID('configAgentEmailInput');
    var email = (emailEl && emailEl.value.trim()) || lastRememberedEmail || '';
    if (!email || email.indexOf('@') < 0) {
      focusEmailField(true);
      if (typeof global.showToast === 'function') global.showToast(cat('needEmail'), 'error');
      return '';
    }
    return email;
  }

  function sendSelectedBinding() {
    var input = byID('configAgentInput');
    var emailEl = byID('configAgentEmailInput');
    var parsed = parseBindLine(input && input.value);
    var mode = (parsed && parsed.mode) || 'bind';
    var groups = (parsed && parsed.groups) || [];
    var email = requireBarEmail();
    if (!groups.length) return;
    if (!email) return;
    rememberEmail(email);
    if (input) input.value = buildBindLine(mode, email, groups);
    updateSelectedBar();
    submitConfigAgent();
  }

  function diagnoseBarEmail() {
    var email = requireBarEmail();
    if (!email) return;
    rememberEmail(email);
    var input = byID('configAgentInput');
    if (input) input.value = 'diagnose LLM service for ' + email;
    updateSelectedBar();
    submitConfigAgent();
  }

  function listGroupsQuick() {
    var input = byID('configAgentInput');
    if (input) input.value = 'list service groups';
    submitConfigAgent();
  }

  function formatBlockedRoutesText(hint) {
    hint = hint || lastDiagnoseHint;
    if (!hint) return '';
    var lines = [];
    lines.push('Blocked routes for ' + (hint.email || ''));
    lines.push('Eligible: ' + (hint.ok != null ? hint.ok : '?') + ' | Ineligible: ' + (hint.bad != null ? hint.bad : '?'));
    lines.push('---');
    var bad = hint.badRoutes || [];
    if (!bad.length) {
      lines.push('(none)');
    } else {
      bad.forEach(function(x, i) {
        lines.push(
          (i + 1) + '. ' + (x.model_name || '?') + ' @ ' + (x.provider_id || '?')
          + (x.reason_code ? ' [' + x.reason_code + ']' : '')
          + (x.reason_message ? ' ' + x.reason_message : '')
        );
      });
    }
    return lines.join('\n');
  }

  function formatBlockedRoutesMarkdown(hint) {
    hint = hint || lastDiagnoseHint;
    if (!hint) return '';
    var lines = [];
    lines.push('### Blocked routes \u2014 `' + (hint.email || '') + '`');
    lines.push('');
    lines.push('| # | Model | Provider | Reason | Message |');
    lines.push('|---|-------|----------|--------|---------|');
    var bad = hint.badRoutes || [];
    if (!bad.length) {
      lines.push('| \u2014 | \u2014 | \u2014 | none | \u2014 |');
    } else {
      bad.forEach(function(x, i) {
        var msg = String(x.reason_message || '').replace(/\|/g, '\\|').replace(/\n/g, ' ');
        lines.push(
          '| ' + (i + 1)
          + ' | ' + (x.model_name || '?')
          + ' | ' + (x.provider_id || '?')
          + ' | ' + (x.reason_code || '')
          + ' | ' + msg
          + ' |'
        );
      });
    }
    lines.push('');
    lines.push('Eligible: **' + (hint.ok != null ? hint.ok : '?') + '** \u00b7 Ineligible: **' + (hint.bad != null ? hint.bad : '?') + '**');
    return lines.join('\n');
  }

  function formatDiagnoseSummaryText(hint) {
    hint = hint || lastDiagnoseHint;
    if (!hint) return '';
    var lines = [];
    lines.push('Config Agent diagnose summary');
    lines.push('Email: ' + (hint.email || ''));
    lines.push('Eligible: ' + (hint.ok != null ? hint.ok : '?'));
    lines.push('Ineligible: ' + (hint.bad != null ? hint.bad : '?'));
    lines.push('Bindings: ' + (hint.binds != null ? hint.binds : 0));
    lines.push('Grants: ' + (hint.grants != null ? hint.grants : 0));
    lines.push('Suggest bind system-free: ' + (hint.suggestBind ? 'yes' : 'no'));
    lines.push('Has system-free binding: ' + (hint.hasSystemFree ? 'yes' : 'no'));
    if (hint.at) {
      try { lines.push('At: ' + new Date(hint.at).toISOString()); } catch (_) {}
    }
    var sec = hint.securityGroupIds || [];
    lines.push('');
    lines.push('=== Security groups ===');
    lines.push(sec.length ? sec.join(', ') : '(none)');
    var bindings = hint.bindingDetails || [];
    lines.push('');
    lines.push('=== User bindings ===');
    if (!bindings.length) {
      lines.push('(none)');
    } else {
      bindings.forEach(function(b, i) {
        var ids = (b.service_group_ids || []).join(', ') || '(empty)';
        lines.push((i + 1) + '. ' + (b.email || hint.email || '') + ' \u2192 [' + ids + ']');
      });
    }
    var gbinds = hint.groupBindings || [];
    lines.push('');
    lines.push('=== Matched group bindings ===');
    if (!gbinds.length) {
      lines.push('(none)');
    } else {
      gbinds.forEach(function(b, i) {
        var ids = (b.service_group_ids || []).join(', ') || '(empty)';
        lines.push((i + 1) + '. security_group=' + (b.group_id || '?') + ' \u2192 [' + ids + ']');
      });
    }
    var grants = hint.grantDetails || [];
    lines.push('');
    lines.push('=== Active grants ===');
    if (!grants.length) {
      lines.push('(none)');
    } else {
      grants.forEach(function(g, i) {
        lines.push(
          (i + 1) + '. ' + (g.service_group_id || '?')
          + ' source=' + (g.source || '')
          + ' credits=' + (g.credits_total != null ? g.credits_total : '') + '/' + (g.credits_used != null ? g.credits_used : '')
          + (g.expires_at ? ' exp=' + g.expires_at : '')
        );
      });
    }
    lines.push('');
    lines.push('=== Blocked routes ===');
    lines.push(formatBlockedRoutesText(hint).split('\n').slice(3).join('\n'));
    var ok = hint.okRoutes || [];
    if (ok.length) {
      lines.push('');
      lines.push('=== Eligible sample ===');
      ok.forEach(function(x, i) {
        lines.push((i + 1) + '. ' + (x.model_name || '?') + ' @ ' + (x.provider_id || '?'));
      });
    }
    lines.push('');
    lines.push('=== Blocked routes (Markdown) ===');
    lines.push(formatBlockedRoutesMarkdown(hint));
    return lines.join('\n');
  }

  function formatSupportPackJSON(hint) {
    hint = hint || lastDiagnoseHint;
    if (!hint) return '';
    var pack = {
      kind: 'config-agent-diagnose-support-pack',
      version: 1,
      generated_at: new Date().toISOString(),
      email: hint.email || '',
      summary: {
        eligible: hint.ok,
        ineligible: hint.bad,
        bindings: hint.binds,
        grants: hint.grants,
        suggest_bind_system_free: !!hint.suggestBind,
        has_system_free_binding: !!hint.hasSystemFree
      },
      security_group_ids: hint.securityGroupIds || [],
      user_bindings: hint.bindingDetails || [],
      matched_group_bindings: hint.groupBindings || [],
      active_grants: hint.grantDetails || [],
      blocked_routes: hint.badRoutes || [],
      eligible_sample: hint.okRoutes || [],
      diagnosed_at: hint.at ? new Date(hint.at).toISOString() : null
    };
    return JSON.stringify(pack, null, 2);
  }

  function downloadSupportPackJson(hint) {
    var text = formatSupportPackJSON(hint);
    if (!text) return false;
    var email = (hint && hint.email) || (lastDiagnoseHint && lastDiagnoseHint.email) || 'user';
    var safe = String(email).replace(/[^A-Za-z0-9._\-@]+/g, '_');
    var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
    downloadTextFile('diagnose_support_' + safe + '_' + stamp + '.json', text, 'application/json;charset=utf-8');
    if (typeof global.showToast === 'function') global.showToast(cat('downloadSupport'), 'success');
    return true;
  }

  /** Plain-text handoff for tickets / chat with support (not raw JSON). */
  function formatSupportHandoffText(hint) {
    hint = hint || lastDiagnoseHint;
    if (!hint) return '';
    var lines = [];
    lines.push('Config Agent \u2014 diagnose handoff');
    lines.push('Generated: ' + new Date().toISOString());
    lines.push('');
    lines.push('User email: ' + (hint.email || '(unknown)'));
    lines.push('Eligible routes: ' + (hint.ok != null ? hint.ok : '?'));
    lines.push('Blocked routes: ' + (hint.bad != null ? hint.bad : '?'));
    lines.push('User bindings: ' + (hint.binds != null ? hint.binds : 0));
    lines.push('Active grants: ' + (hint.grants != null ? hint.grants : 0));
    lines.push('Has system-free binding: ' + (hint.hasSystemFree ? 'yes' : 'no'));
    lines.push('Suggest bind system-free: ' + (hint.suggestBind ? 'yes' : 'no'));
    if ((hint.securityGroupIds || []).length) {
      lines.push('Security groups: ' + hint.securityGroupIds.join(', '));
    }
    if ((hint.bindingDetails || []).length) {
      lines.push('');
      lines.push('Bindings:');
      hint.bindingDetails.forEach(function(b) {
        lines.push('  - ' + ((b.service_group_ids || []).join(', ') || '(empty)'));
      });
    }
    if ((hint.badRoutes || []).length) {
      lines.push('');
      lines.push('Blocked:');
      hint.badRoutes.forEach(function(x) {
        lines.push('  - ' + (x.model_name || '?') + ' @ ' + (x.provider_id || '?')
          + (x.reason_code ? ' [' + x.reason_code + ']' : '')
          + (x.reason_message ? ' ' + x.reason_message : ''));
      });
    }
    lines.push('');
    lines.push('Attach diagnose_support_*.json for full machine-readable pack.');
    return lines.join('\n');
  }

  function openSupportMailto(hint) {
    hint = hint || lastDiagnoseHint;
    var body = formatSupportHandoffText(hint);
    if (!body) return false;
    var email = (hint && hint.email) || 'user';
    var subject = 'Config Agent diagnose: ' + email;
    // Keep body under common mailto length limits.
    if (body.length > 1600) body = body.slice(0, 1600) + '\n\u2026(truncated; attach support JSON)';
    var url = 'mailto:?subject=' + encodeURIComponent(subject) + '&body=' + encodeURIComponent(body);
    try {
      var a = document.createElement('a');
      a.href = url;
      a.style.display = 'none';
      document.body.appendChild(a);
      a.click();
      a.remove();
    } catch (_) {
      try { global.location.href = url; } catch (__) {}
    }
    if (typeof global.showToast === 'function') global.showToast(cat('handoffMail'), 'success');
    return true;
  }

  function renderSessionTimeline(turns) {
    turns = turns || sessionTurns || [];
    if (!turns.length) return '';
    var items = turns.slice(-8).map(function(t, i) {
      var n = turns.length > 8 ? (turns.length - 8 + i + 1) : (i + 1);
      var text = String(t || '');
      if (text.length > 120) text = text.slice(0, 120) + '\u2026';
      return '<div class="item-meta mono" style="margin-top:3px">'
        + '<span style="display:inline-block;min-width:18px;color:#667085">' + n + '.</span> '
        + esc(text) + '</div>';
    }).join('');
    return '<div style="margin-top:8px;padding:8px;border-radius:8px;background:rgba(47,128,237,.05);border:1px solid rgba(47,128,237,.12)">'
      + '<div class="item-meta"><strong>' + esc(cat('sessionTurns')) + '</strong>'
      + ' (' + turns.length + ')</div>'
      + items + '</div>';
  }

  function ensureProvidersCached() {
    if (lastProviders.length) return Promise.resolve(lastProviders);
    return global.api('/api/admin/llm/providers').then(function(data) {
      var list = (data && data.providers) || [];
      lastProviders = list.map(function(p) {
        return { id: String(p.id || p.provider_id || '').trim(), name: String(p.name || '') };
      }).filter(function(p) { return !!p.id; });
      return lastProviders;
    }).catch(function() {
      lastProviders = [];
      return lastProviders;
    });
  }

  function providerChipLabel(p) {
    if (!p) return '';
    var id = String(p.id || '');
    var name = String(p.name || '').trim();
    if (name && name !== id) {
      // Prefer short readable name; keep id in title attribute.
      return name.length > 18 ? name.slice(0, 16) + '\u2026' : name;
    }
    return id;
  }

  function formatSessionTTL(msLeft) {
    if (msLeft <= 0) return cat('sessionExpired');
    var sec = Math.floor(msLeft / 1000);
    var m = Math.floor(sec / 60);
    var s = sec % 60;
    if (m >= 60) {
      var h = Math.floor(m / 60);
      m = m % 60;
      return h + 'h ' + m + 'm';
    }
    if (m > 0) return m + 'm ' + (s < 10 ? '0' : '') + s + 's';
    return s + 's';
  }

  function stopSessionTTLTimer() {
    if (sessionTTLTimer) {
      clearInterval(sessionTTLTimer);
      sessionTTLTimer = null;
    }
  }

  function stopPlanTTLTimer() {
    if (planTTLTimer) {
      clearInterval(planTTLTimer);
      planTTLTimer = null;
    }
  }

  function startSessionTTLTimer() {
    stopSessionTTLTimer();
    if (!configAgentSessionID || !sessionExpiresAtMs) return;
    sessionTTLTimer = setInterval(function() {
      var el = byID('configAgentSessionTTL');
      var left = sessionExpiresAtMs - Date.now();
      if (left <= 0) {
        if (el) el.textContent = cat('sessionExpired');
        // Auto-clear expired client session state.
        clearConfigAgentSession({ silent: true, reason: 'expired' });
        return;
      }
      if (el) el.textContent = cat('sessionTTL') + ' ' + formatSessionTTL(left);
    }, 1000);
  }

  function setPlanExpiryFromPlan(plan) {
    stopPlanTTLTimer();
    planExpiresAtMs = 0;
    if (plan && plan.confirm_token && plan.expires_at) {
      var t = Date.parse(String(plan.expires_at));
      if (!isNaN(t)) planExpiresAtMs = t;
    }
    if (!planExpiresAtMs) return;
    planTTLTimer = setInterval(function() {
      var el = byID('configAgentPlanTTL');
      var left = planExpiresAtMs - Date.now();
      if (left <= 0) {
        if (el) {
          el.textContent = cat('planExpired');
          el.style.color = '#b42318';
        }
        stopPlanTTLTimer();
        if (pendingPlan && pendingPlan.confirm_token) {
          pendingPlan = null;
          planExpiresAtMs = 0;
          if (typeof global.showToast === 'function') global.showToast(cat('planExpired'), 'error');
          updateSelectedBar();
        }
        return;
      }
      if (el) el.textContent = cat('planTTL') + ' ' + formatSessionTTL(left);
    }, 1000);
  }

  function toolNameToExample(name) {
    var n = String(name || '');
    // Prefer server-provided example from last catalog fetch (O(1)).
    if (lastCatalogExampleByName[n]) return lastCatalogExampleByName[n];
    var map = {
      'llm.services.diagnose': 'diagnose LLM service for demo@example.com',
      'llm.services.list': 'list service groups',
      'llm.services.user_bind': 'bind user demo@example.com to system-free',
      'llm.services.user_unbind': 'unbind user demo@example.com from system-free',
      'invitation_codes.export': 'export invitation codes',
      'invitation_codes.list': 'list invitation codes',
      'invitation_codes.required.get': 'show invitation code required status',
      'users.invite.create': 'invite user demo@example.com as member',
      'system_free.get': 'show system-free status',
      'system_free.test': 'test system-free',
      'migration.settings.get': 'show migration settings',
      'migration.settings.update': 'set migration max to 200MB',
      'feishu.auto_enroll.get': 'show feishu auto enroll',
      'card_store.config.get': 'show card store config',
      'mail.sender_name.get': 'show mail sender name',
      'llm.providers.get': 'list LLM providers'
    };
    if (map[n]) return map[n];
    if (/\.get$/.test(n) || /\.list$/.test(n) || /\.test$/.test(n)) {
      return 'show ' + n.replace(/\./g, ' ').replace(/ get$| list$| test$/, '');
    }
    return n.replace(/\./g, ' ');
  }

  function setSessionFromPlanResponse(data) {
    if (!data) return;
    if (data.session_id) {
      configAgentSessionID = String(data.session_id || '');
    }
    if (Array.isArray(data.session_turns)) {
      sessionTurns = data.session_turns.map(String);
    }
    if (data.session_expires_at) {
      var t = Date.parse(String(data.session_expires_at));
      sessionExpiresAtMs = isNaN(t) ? 0 : t;
    } else if (configAgentSessionID) {
      // Fallback: 15 minutes from now if server omitted expires_at.
      sessionExpiresAtMs = Date.now() + 15 * 60 * 1000;
    }
    // Already expired payload (clock skew / stale tab) - clear client state.
    if (configAgentSessionID && sessionExpiresAtMs && sessionExpiresAtMs <= Date.now()) {
      clearConfigAgentSession({ silent: true, reason: 'expired' });
      return;
    }
    startSessionTTLTimer();
  }

  function clearConfigAgentSession(opts) {
    opts = opts || {};
    var had = !!configAgentSessionID;
    configAgentSessionID = '';
    sessionTurns = [];
    sessionExpiresAtMs = 0;
    stopSessionTTLTimer();
    if (!opts.keepPending) {
      pendingPlan = null;
      stopPlanTTLTimer();
      planExpiresAtMs = 0;
    }
    if (had && typeof global.showToast === 'function') {
      if (opts.reason === 'expired') {
        global.showToast(cat('sessionExpired'), 'error');
      } else if (!opts.silent) {
        global.showToast(opts.toast || cat('newSession'), 'success');
      }
    }
    if (!opts.skipBar) updateSelectedBar();
  }

  function setConfigAgentComposerBusy(busy) {
    var composer = byID('configAgentComposer');
    var input = byID('configAgentInput');
    var send = byID('configAgentSendBtn');
    if (composer) composer.setAttribute('aria-busy', busy ? 'true' : 'false');
    if (input) input.disabled = !!busy;
    if (send) {
      send.disabled = !!busy;
      if (busy) {
        send.setAttribute('aria-busy', 'true');
        send.setAttribute('aria-label', cat('sending'));
        send.setAttribute('title', cat('sending'));
      } else {
        send.removeAttribute('aria-busy');
        send.setAttribute('aria-label', cat('send'));
        send.setAttribute('title', cat('send'));
      }
    }
  }

  function renderDiagnoseWizardStrip(opts) {
    opts = opts || {};
    var step = opts.step || 1; // 1 diagnose done, 2 bind available, 3 recheck after bind
    var canBind = !!opts.canBind;
    var canUnbind = !!opts.canUnbind;
    var email = opts.email || '';
    function pill(label, active, done) {
      var bg = done ? 'rgba(2,122,72,.12)' : (active ? 'rgba(47,128,237,.12)' : 'rgba(20,24,36,.04)');
      var color = done ? '#027a48' : (active ? '#175cd3' : '#667085');
      return '<span style="display:inline-flex;align-items:center;padding:3px 8px;border-radius:999px;background:'
        + bg + ';color:' + color + ';font-size:11px;font-weight:650">'
        + esc(label) + (done ? ' \u2713' : '') + '</span>';
    }
    var html = '<div class="item-meta" style="margin-top:10px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
      + pill(cat('wizardDiagnose'), false, step >= 1)
      + '<span style="opacity:.4">\u2192</span>'
      + pill(cat('wizardBind'), step === 2 || (step === 1 && !!opts.canBind), step >= 3)
      + '<span style="opacity:.4">\u2192</span>'
      + pill(cat('wizardRecheck'), step === 3, step >= 3)
      + '</div>';
    if (canBind || canUnbind) {
      html += '<div class="actions" style="margin-top:6px;gap:6px;flex-wrap:wrap">';
      if (canBind) {
        html += '<button type="button" class="btn-primary" style="height:28px;font-size:11px;padding:0 10px" data-ca-bind-sf="1">'
          + esc(cat('bindSystemFree')) + (email ? ' \u00b7 ' + esc(email) : '') + '</button>';
      }
      if (canUnbind) {
        html += '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" data-ca-unbind-sf="1">'
          + esc(cat('unbindSystemFree')) + '</button>';
      }
      html += '</div>';
    }
    return html;
  }

  function renderMissingFieldsPanel(plan) {
    var missing = (plan && plan.missing_fields) || [];
    if (!missing.length) return '';
    var needEmail = missing.some(function(f) {
      return f === 'email' || f === 'id_or_email';
    });
    var needGroup = missing.some(function(f) {
      return f === 'service_group_id' || f === 'service_group_ids';
    });
    var needRole = missing.indexOf('role') >= 0;
    var needProvider = missing.indexOf('provider_id') >= 0;
    var other = missing.filter(function(f) {
      return ['email', 'id_or_email', 'service_group_id', 'service_group_ids', 'role', 'provider_id'].indexOf(f) < 0;
    });

    var html = '<div style="margin-top:10px;padding:10px;border-radius:8px;border:1px solid rgba(180,70,20,.25);background:rgba(255,247,237,.9)">'
      + '<div class="item-meta"><strong>' + esc(cat('fillMissing')) + '</strong>: '
      + esc(missing.join(', ')) + '</div>'
      + '<div class="item-meta" style="margin-top:4px;opacity:.85">' + esc(cat('missingHint')) + '</div>';

    if (needEmail) {
      html += '<div style="margin-top:8px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
        + '<input id="configAgentMissingEmail" type="email" value="' + esc(lastRememberedEmail || '') + '" '
        + 'placeholder="' + esc(cat('emailPlaceholder')) + '" '
        + 'style="height:28px;font-size:12px;padding:0 8px;min-width:200px;border:1px solid rgba(20,24,36,.15);border-radius:6px">'
        + (lastRememberedEmail
          ? '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" data-ca-fill-email="1">'
            + esc(cat('useRemembered')) + '</button>'
          : '')
        + '</div>';
    }
    if (needRole) {
      html += '<div style="margin-top:8px;display:flex;gap:6px;flex-wrap:wrap">'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" data-ca-fill-role="member">'
        + esc(cat('roleMember')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" data-ca-fill-role="admin">'
        + esc(cat('roleAdmin')) + '</button>'
        + '</div>';
    }
    if (needGroup) {
      var groupOpts = [];
      var seenG = {};
      function pushG(id) {
        id = String(id || '').trim();
        if (!id || seenG[id]) return;
        seenG[id] = true;
        groupOpts.push(id);
      }
      pushG('system-free');
      (recentGroupIds || []).forEach(pushG);
      (lastServiceGroups || []).forEach(function(g) { pushG(g && g.id); });
      html += '<div style="margin-top:8px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
        + '<span class="item-meta">' + esc(cat('pickGroup')) + ':</span>'
        + groupOpts.slice(0, 10).map(function(gid) {
          return '<button type="button" class="btn-ghost mono" style="height:26px;font-size:11px;padding:0 8px" data-ca-fill-group="'
            + esc(gid) + '">' + esc(gid) + '</button>';
        }).join('')
        + '</div>';
    }
    if (needProvider) {
      html += '<div style="margin-top:8px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
        + '<input id="configAgentMissingProvider" type="text" placeholder="provider_id" '
        + 'style="height:28px;font-size:12px;padding:0 8px;min-width:160px;border:1px solid rgba(20,24,36,.15);border-radius:6px">'
        + '</div>';
      if (lastProviders.length) {
        html += '<div style="margin-top:6px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
          + '<span class="item-meta">' + esc(cat('pickProvider')) + ':</span>'
          + lastProviders.slice(0, 12).map(function(p) {
            var full = p.name && p.name !== p.id ? (p.id + ' \u2014 ' + p.name) : p.id;
            var short = providerChipLabel(p);
            return '<button type="button" class="btn-ghost mono" style="height:26px;font-size:11px;padding:0 8px" data-ca-fill-provider="'
              + esc(p.id) + '" title="' + esc(full) + '">' + esc(short) + '</button>';
          }).join('')
          + '</div>';
      }
    }
    if (other.length) {
      html += other.map(function(f) {
        return '<div style="margin-top:6px">'
          + '<label class="item-meta">' + esc(f) + '</label> '
          + '<input type="text" data-ca-missing-field="' + esc(f) + '" placeholder="' + esc(f) + '" '
          + 'style="height:28px;font-size:12px;padding:0 8px;min-width:160px;border:1px solid rgba(20,24,36,.15);border-radius:6px">'
          + '</div>';
      }).join('');
    }
    html += '<div class="actions" style="margin-top:10px;gap:8px">'
      + '<button type="button" class="btn-primary" style="height:30px;font-size:12px;padding:0 12px" data-ca-continue-fill="1">'
      + esc(cat('continueFill')) + '</button>'
      + '</div></div>';
    return html;
  }

  function composeFollowUpFromFill(row, plan) {
    var parts = [];
    var missing = (plan && plan.missing_fields) || [];
    var emailEl = row.querySelector('#configAgentMissingEmail');
    if (emailEl && String(emailEl.value || '').trim()) {
      parts.push('email ' + String(emailEl.value).trim());
    }
    var roleBtn = row.querySelector('[data-ca-fill-role].btn-secondary');
    if (roleBtn) {
      parts.push('role ' + (roleBtn.getAttribute('data-ca-fill-role') || 'member'));
    }
    var groupBtns = row.querySelectorAll('[data-ca-fill-group].btn-secondary');
    if (groupBtns && groupBtns.length) {
      var gids = [];
      groupBtns.forEach(function(b) {
        var g = b.getAttribute('data-ca-fill-group');
        if (g) gids.push(g);
      });
      if (gids.length) parts.push('service groups ' + gids.join(' '));
    }
    var provBtn = row.querySelector('[data-ca-fill-provider].btn-secondary');
    var prov = row.querySelector('#configAgentMissingProvider');
    var provVal = (provBtn && provBtn.getAttribute('data-ca-fill-provider'))
      || (prov && String(prov.value || '').trim())
      || '';
    if (provVal) {
      parts.push('provider_id ' + provVal);
    }
    row.querySelectorAll('[data-ca-missing-field]').forEach(function(inp) {
      var f = inp.getAttribute('data-ca-missing-field');
      var v = String(inp.value || '').trim();
      if (f && v) parts.push(f + ' ' + v);
    });
    // Fallback: if only email missing and we have remembered, use it.
    if (!parts.length && missing.indexOf('email') >= 0 && lastRememberedEmail) {
      parts.push('email ' + lastRememberedEmail);
    }
    return parts.join(', ');
  }

  function updateSelectedBar() {
    ensureSelectedBar();
    var bar = byID('configAgentSelectedBar');
    var input = byID('configAgentInput');
    if (!bar) return;
    var parsed = parseBindLine(input && input.value);
    var email = (parsed && parsed.email) || lastRememberedEmail || extractEmailFromText(input && input.value) || '';
    var groups = (parsed && parsed.groups) || [];
    var mode = (parsed && parsed.mode) || 'bind';
    // Always visible workspace for email + binding helpers.
    bar.style.display = 'flex';
    var bindActive = mode !== 'unbind';
    var keepEmailFocus = document.activeElement && document.activeElement.id === 'configAgentEmailInput';
    var keepEmailPos = keepEmailFocus ? document.activeElement.selectionStart : null;
    var html = ''
      + '<label class="item-meta" style="display:inline-flex;align-items:center;gap:6px">'
      + esc(cat('rememberedEmail')) + ':'
      + '<input id="configAgentEmailInput" type="email" value="' + esc(email) + '" placeholder="' + esc(cat('emailPlaceholder')) + '" '
      + 'autocomplete="email" '
      + 'style="height:28px;font-size:12px;padding:0 8px;min-width:180px;border:1px solid rgba(20,24,36,.15);border-radius:6px">'
      + '</label>'
      + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentClearEmailBtn">' + esc(cat('clearEmail')) + '</button>'
      + '<button type="button" class="btn-secondary" style="height:28px;font-size:11px;padding:0 10px" id="configAgentDiagnoseEmailBtn">' + esc(cat('diagnoseEmail')) + '</button>'
      + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" id="configAgentListGroupsBtn">' + esc(cat('listGroups')) + '</button>';

    // Session chip + TTL + new session (any active multi-turn session)
    if (configAgentSessionID) {
      var needsFill = !!(pendingPlan && (pendingPlan.missing_fields || []).length);
      var ttlLeft = sessionExpiresAtMs ? (sessionExpiresAtMs - Date.now()) : 0;
      var ttlText = sessionExpiresAtMs
        ? (ttlLeft > 0 ? (cat('sessionTTL') + ' ' + formatSessionTTL(ttlLeft)) : cat('sessionExpired'))
        : '';
      var chipBg = needsFill ? 'rgba(180,70,20,.1)' : 'rgba(47,128,237,.08)';
      var chipBd = needsFill ? 'rgba(180,70,20,.25)' : 'rgba(47,128,237,.2)';
      html += '<span class="item-meta" style="display:inline-flex;align-items:center;gap:6px;padding:2px 8px;border-radius:999px;background:'
        + chipBg + ';border:1px solid ' + chipBd + '">'
        + esc(cat('sessionActive'))
        + ' \u00b7 <span class="mono" style="font-size:10px">' + esc(String(configAgentSessionID).slice(0, 14)) + '</span>'
        + (sessionTurns.length ? ' \u00b7 ' + sessionTurns.length + ' turn' + (sessionTurns.length > 1 ? 's' : '') : '')
        + (ttlText ? ' \u00b7 <span id="configAgentSessionTTL" class="mono" style="font-size:10px">' + esc(ttlText) + '</span>' : '')
        + '</span>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentNewSessionBtn">'
        + esc(cat('newSession')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentEndSessionBtn">'
        + esc(cat('endSession')) + '</button>';
    }

    // Mode toggle + groups when selected
    if (groups.length) {
      html += '<span class="item-meta" style="margin-left:2px">'
        + '<button type="button" class="' + (bindActive ? 'btn-secondary' : 'btn-ghost') + '" style="height:28px;font-size:11px;padding:0 8px" id="configAgentModeBindBtn">' + esc(cat('modeBind')) + '</button>'
        + '<button type="button" class="' + (!bindActive ? 'btn-secondary' : 'btn-ghost') + '" style="height:28px;font-size:11px;padding:0 8px;margin-left:4px" id="configAgentModeUnbindBtn">' + esc(cat('modeUnbind')) + '</button>'
        + '</span>';
      html += '<span class="item-meta">' + esc(cat('selectedGroups')) + ':</span> '
        + groups.map(function(g) {
          return '<span class="mono" style="display:inline-flex;align-items:center;gap:4px;padding:1px 6px;border-radius:999px;background:rgba(47,128,237,.1);font-size:11px">'
            + esc(g)
            + ' <button type="button" data-ca-drop-group="' + esc(g) + '" style="border:0;background:transparent;cursor:pointer;padding:0;line-height:1;color:#b42318" title="remove">\u00d7</button>'
            + '</span>';
        }).join(' ')
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentClearGroupsBtn">' + esc(cat('clearGroups')) + '</button>'
        + '<button type="button" class="btn-primary" style="height:28px;font-size:11px;padding:0 10px" id="configAgentSendBindingBtn">'
        + esc(bindActive ? cat('sendBind') : cat('sendUnbind'))
        + '</button>';
    } else {
      html += '<span class="item-meta" style="opacity:.7">' + esc(cat('barHint')) + '</span>';
    }

    // Recent group quick chips (always, for one-click pick; Alt+click pins)
    if (recentGroupIds.length) {
      html += '<span class="item-meta" style="width:100%;margin-top:2px"></span>';
      html += '<span class="item-meta" title="' + esc(cat('pinHint')) + '">' + esc(cat('recentGroups')) + ':</span> '
        + recentGroupIds.map(function(gid) {
          var selected = groups.indexOf(gid) >= 0;
          var pinned = pinnedGroupIds.indexOf(gid) >= 0;
          return '<button type="button" class="' + (selected ? 'btn-secondary' : 'btn-ghost') + ' mono" '
            + 'style="height:26px;font-size:11px;padding:0 8px' + (pinned ? ';border-color:rgba(180,120,20,.45)' : '') + '" '
            + 'title="' + esc(cat('pinHint')) + '" data-ca-recent-group="' + esc(gid) + '">'
            + (pinned ? '\u2605 ' : '') + esc(gid) + '</button>';
        }).join('')
        + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentClearRecentBtn">'
        + esc(cat('clearRecent')) + '</button>';
    }

    // Diagnose follow-up summary + actions on bar
    if (lastDiagnoseHint && lastDiagnoseHint.email) {
      html += '<span class="item-meta" style="width:100%;margin-top:2px"></span>';
      var okN = lastDiagnoseHint.ok != null ? lastDiagnoseHint.ok : '?';
      var badN = lastDiagnoseHint.bad != null ? lastDiagnoseHint.bad : '?';
      var badColor = Number(badN) > 0 ? '#b42318' : '#027a48';
      html += '<button type="button" id="configAgentDiagSummaryBtn" class="item-meta mono" '
        + 'style="padding:4px 8px;border-radius:6px;background:rgba(20,24,36,.04);border:1px solid rgba(20,24,36,.08);cursor:pointer;text-align:left">'
        + esc(cat('lastDiag')) + ': ' + esc(String(lastDiagnoseHint.email))
        + ' \u00b7 <span style="color:#027a48">' + esc(String(okN)) + ' ' + esc(cat('eligible')) + '</span>'
        + ' / <span style="color:' + badColor + '">' + esc(String(badN)) + ' ' + esc(cat('ineligible')) + '</span>'
        + ' \u00b7 ' + esc(cat('bindings')) + ' ' + esc(String(lastDiagnoseHint.binds != null ? lastDiagnoseHint.binds : 0))
        + ' \u00b7 ' + esc(cat('grants')) + ' ' + esc(String(lastDiagnoseHint.grants != null ? lastDiagnoseHint.grants : 0))
        + ' \u00b7 ' + esc(diagRoutesExpanded ? cat('hideBadRoutes') : cat('showBadRoutes'))
        + '</button>';
      if (lastDiagnoseHint.suggestBind) {
        html += '<button type="button" class="btn-primary" style="height:28px;font-size:11px;padding:0 10px" id="configAgentBarBindSfBtn">'
          + esc(cat('bindSystemFree')) + '</button>';
      }
      if (lastDiagnoseHint.hasSystemFree) {
        html += '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" id="configAgentBarUnbindSfBtn">'
          + esc(cat('unbindSystemFree')) + '</button>';
      }
      html += '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentCopyDiagBtn">'
        + esc(cat('copyDiag')) + '</button>'
        + '<button type="button" class="btn-secondary" style="height:28px;font-size:11px;padding:0 8px" id="configAgentCopySupportBtn">'
        + esc(cat('copySupport')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentCopyHandoffBtn">'
        + esc(cat('handoff')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentMailtoHandoffBtn">'
        + esc(cat('handoffMail')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentDownloadSupportBtn">'
        + esc(cat('downloadSupport')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentDownloadDiagBtn">'
        + esc(cat('downloadDiag')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentClearDiagBtn">'
        + esc(cat('clearDiag')) + '</button>';
      if (diagRoutesExpanded) {
        var badList = lastDiagnoseHint.badRoutes || [];
        var okList = lastDiagnoseHint.okRoutes || [];
        html += '<div style="width:100%;margin-top:4px;padding:8px;border-radius:8px;background:#fff;border:1px solid rgba(20,24,36,.08);max-height:240px;overflow:auto">';
        html += '<div class="actions" style="margin-bottom:6px;gap:6px;flex-wrap:wrap">'
          + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentCopyBlockedBtn">'
          + esc(cat('copyBlocked')) + '</button>'
          + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentCopyBlockedMdBtn">'
          + esc(cat('copyBlockedMd')) + '</button>'
          + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentCopySupportInlineBtn">'
          + esc(cat('copySupport')) + '</button>'
          + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentCopyHandoffInlineBtn">'
          + esc(cat('handoff')) + '</button>'
          + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentMailtoHandoffInlineBtn">'
          + esc(cat('handoffMail')) + '</button>'
          + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentDownloadSupportInlineBtn">'
          + esc(cat('downloadSupport')) + '</button>'
          + '</div>';
        var secIds = lastDiagnoseHint.securityGroupIds || [];
        if (secIds.length) {
          html += '<div class="item-meta" style="margin-bottom:4px"><strong>' + esc(cat('securityGroups')) + '</strong>: '
            + '<span class="mono">' + esc(secIds.join(', ')) + '</span></div>';
        }
        var bindDetails = lastDiagnoseHint.bindingDetails || [];
        if (bindDetails.length) {
          html += '<div class="item-meta" style="margin-bottom:4px"><strong>' + esc(cat('bindings')) + '</strong></div>';
          html += bindDetails.slice(0, 6).map(function(b) {
            var ids = (b.service_group_ids || []).join(', ') || '(empty)';
            return '<div class="item-meta mono" style="margin-top:2px">' + esc(ids) + '</div>';
          }).join('');
        }
        var gbinds = lastDiagnoseHint.groupBindings || [];
        if (gbinds.length) {
          html += '<div class="item-meta" style="margin-top:6px"><strong>' + esc(cat('groupBinds')) + '</strong></div>';
          html += gbinds.slice(0, 8).map(function(b) {
            var ids = (b.service_group_ids || []).join(', ') || '(empty)';
            return '<div class="item-meta mono" style="margin-top:2px">'
              + esc(b.group_id || '?') + ' \u2192 ' + esc(ids) + '</div>';
          }).join('');
        }
        var grants = lastDiagnoseHint.grantDetails || [];
        if (grants.length) {
          html += '<div class="item-meta" style="margin-top:6px"><strong>' + esc(cat('grants')) + '</strong></div>';
          html += grants.slice(0, 8).map(function(g) {
            return '<div class="item-meta mono" style="margin-top:2px">'
              + esc(g.service_group_id || '?')
              + (g.source ? ' \u00b7 ' + esc(g.source) : '')
              + (g.credits_total != null ? ' \u00b7 credits ' + esc(String(g.credits_used != null ? g.credits_used : 0)) + '/' + esc(String(g.credits_total)) : '')
              + '</div>';
          }).join('');
        }
        if (badList.length) {
          html += '<div class="item-meta" style="margin-top:6px"><strong>' + esc(cat('ineligible')) + '</strong></div>';
          html += badList.slice(0, 12).map(function(x) {
            return '<div class="item-meta" style="margin-top:3px;color:#b42318">'
              + esc((x.model_name || '?') + ' @ ' + (x.provider_id || '?'))
              + (x.reason_code ? ' \u2014 ' + esc(x.reason_code) : '')
              + (x.reason_message ? ': ' + esc(x.reason_message) : '')
              + '</div>';
          }).join('');
        } else {
          html += '<div class="item-meta" style="margin-top:6px;color:#027a48">' + esc(cat('noBadRoutes')) + '</div>';
        }
        if (okList.length) {
          html += '<div class="item-meta" style="margin-top:6px;opacity:.8">' + esc(cat('eligible')) + ':</div>'
            + okList.slice(0, 5).map(function(x) {
              return '<div class="item-meta" style="margin-top:2px;color:#027a48">'
                + esc((x.model_name || '?') + ' @ ' + (x.provider_id || '?'))
                + '</div>';
            }).join('');
        }
        html += '</div>';
      }
    }

    bar.innerHTML = html;

    var emailEl = byID('configAgentEmailInput');
    if (emailEl) {
      if (keepEmailFocus) {
        try {
          emailEl.focus();
          if (keepEmailPos != null && emailEl.setSelectionRange) {
            var pos = Math.min(keepEmailPos, String(emailEl.value || '').length);
            emailEl.setSelectionRange(pos, pos);
          }
        } catch (_) {}
      }
      emailEl.oninput = function() {
        var v = String(emailEl.value || '').trim();
        var p = parseBindLine(input && input.value);
        var gs = (p && p.groups) || groups;
        var m = (p && p.mode) || mode;
        if (v && v.indexOf('@') >= 0) {
          lastRememberedEmail = v;
          try { localStorage.setItem(EMAIL_STORAGE_KEY, v); } catch (_) {}
        } else if (!v) {
          lastRememberedEmail = '';
          try { localStorage.removeItem(EMAIL_STORAGE_KEY); } catch (_) {}
        }
        if (input && (p || gs.length)) {
          input.value = buildBindLine(m, v, gs);
        }
      };
      emailEl.onkeydown = function(e) {
        if (e.key === 'Enter') {
          e.preventDefault();
          if (groups.length) sendSelectedBinding();
          else diagnoseBarEmail();
        }
      };
    }

    var clearEmailBtn = byID('configAgentClearEmailBtn');
    if (clearEmailBtn) {
      clearEmailBtn.onclick = function() {
        clearRememberedEmail();
        if (emailEl) emailEl.value = '';
        if (input) {
          var p = parseBindLine(input.value);
          if (p) input.value = buildBindLine(p.mode, '', p.groups);
        }
        updateSelectedBar();
      };
    }

    var diagBtn = byID('configAgentDiagnoseEmailBtn');
    if (diagBtn) diagBtn.onclick = function() { diagnoseBarEmail(); };
    var listBtn = byID('configAgentListGroupsBtn');
    if (listBtn) listBtn.onclick = function() { listGroupsQuick(); };

    var endSessBtn = byID('configAgentEndSessionBtn');
    if (endSessBtn) {
      endSessBtn.onclick = function() {
        clearConfigAgentSession({ toast: cat('endSession') });
      };
    }
    var newSessBtn = byID('configAgentNewSessionBtn');
    if (newSessBtn) {
      newSessBtn.onclick = function() {
        clearConfigAgentSession({ toast: cat('newSession') });
      };
    }

    var modeBindBtn = byID('configAgentModeBindBtn');
    if (modeBindBtn) {
      modeBindBtn.onclick = function() {
        var em = (emailEl && emailEl.value.trim()) || lastRememberedEmail;
        syncInputFromBarState(em, groups, 'bind');
        updateSelectedBar();
      };
    }
    var modeUnbindBtn = byID('configAgentModeUnbindBtn');
    if (modeUnbindBtn) {
      modeUnbindBtn.onclick = function() {
        var em = (emailEl && emailEl.value.trim()) || lastRememberedEmail;
        syncInputFromBarState(em, groups, 'unbind');
        updateSelectedBar();
      };
    }

    var clearGroupsBtn = byID('configAgentClearGroupsBtn');
    if (clearGroupsBtn) {
      clearGroupsBtn.onclick = function() {
        var em = (emailEl && emailEl.value.trim()) || lastRememberedEmail || '';
        if (input) {
          // Clear groups but keep free-form if no email/groups remain.
          if (em) input.value = buildBindLine(mode, em, []);
          else if (parseBindLine(input.value)) input.value = '';
        }
        updateSelectedBar();
      };
    }

    var sendBtn = byID('configAgentSendBindingBtn');
    if (sendBtn) sendBtn.onclick = function() { sendSelectedBinding(); };

    bar.querySelectorAll('[data-ca-drop-group]').forEach(function(btn) {
      btn.onclick = function() {
        var drop = btn.getAttribute('data-ca-drop-group');
        var em = (emailEl && emailEl.value.trim()) || lastRememberedEmail || '';
        var next = (groups || []).filter(function(g) { return g !== drop; });
        if (input) input.value = next.length || em ? buildBindLine(mode, em, next) : '';
        updateSelectedBar();
      };
    });

    bar.querySelectorAll('[data-ca-recent-group]').forEach(function(btn) {
      btn.onclick = function(e) {
        var gid = btn.getAttribute('data-ca-recent-group');
        if (e && (e.altKey || e.metaKey)) {
          pinGroup(gid);
          return;
        }
        // Ctrl/Cmd+click: move pinned left; Shift+click: move pinned right
        if (e && e.ctrlKey && !e.shiftKey) {
          if (pinnedGroupIds.indexOf(gid) < 0) pinGroup(gid);
          else movePinnedGroup(gid, -1);
          return;
        }
        if (e && e.shiftKey && !e.altKey) {
          if (pinnedGroupIds.indexOf(gid) < 0) pinGroup(gid);
          else movePinnedGroup(gid, 1);
          return;
        }
        applyGroupToInput(gid, bindActive ? 'bind' : 'unbind');
      };
    });
    var clearRecentBtn = byID('configAgentClearRecentBtn');
    if (clearRecentBtn) clearRecentBtn.onclick = function() { clearRecentGroups(); };

    var barBindSf = byID('configAgentBarBindSfBtn');
    if (barBindSf && lastDiagnoseHint && lastDiagnoseHint.email) {
      barBindSf.onclick = function() {
        rememberEmail(lastDiagnoseHint.email, { silent: true });
        applyGroupToInput('system-free', 'bind');
        submitConfigAgent();
      };
    }
    var barUnbindSf = byID('configAgentBarUnbindSfBtn');
    if (barUnbindSf && lastDiagnoseHint && lastDiagnoseHint.email) {
      barUnbindSf.onclick = function() {
        rememberEmail(lastDiagnoseHint.email, { silent: true });
        applyGroupToInput('system-free', 'unbind');
        submitConfigAgent();
      };
    }
    var clearDiagBtn = byID('configAgentClearDiagBtn');
    if (clearDiagBtn) {
      clearDiagBtn.onclick = function() {
        clearDiagnoseHint();
        updateSelectedBar();
      };
    }
    var copyDiagBtn = byID('configAgentCopyDiagBtn');
    if (copyDiagBtn) {
      copyDiagBtn.onclick = function() {
        copyText(formatDiagnoseSummaryText(lastDiagnoseHint), copyDiagBtn);
      };
    }
    var copySupportBtn = byID('configAgentCopySupportBtn');
    if (copySupportBtn) {
      copySupportBtn.onclick = function() {
        copyText(formatSupportPackJSON(lastDiagnoseHint), copySupportBtn);
      };
    }
    var copyHandoffBtn = byID('configAgentCopyHandoffBtn');
    if (copyHandoffBtn) {
      copyHandoffBtn.onclick = function() {
        copyText(formatSupportHandoffText(lastDiagnoseHint), copyHandoffBtn);
      };
    }
    var mailtoHandoffBtn = byID('configAgentMailtoHandoffBtn');
    if (mailtoHandoffBtn) {
      mailtoHandoffBtn.onclick = function() { openSupportMailto(lastDiagnoseHint); };
    }
    var dlSupportBtn = byID('configAgentDownloadSupportBtn');
    if (dlSupportBtn) {
      dlSupportBtn.onclick = function() { downloadSupportPackJson(lastDiagnoseHint); };
    }
    var dlDiagBtn = byID('configAgentDownloadDiagBtn');
    if (dlDiagBtn) {
      dlDiagBtn.onclick = function() {
        var text = formatDiagnoseSummaryText(lastDiagnoseHint);
        if (!text) return;
        var email = (lastDiagnoseHint && lastDiagnoseHint.email) || 'user';
        var safe = String(email).replace(/[^A-Za-z0-9._\-@]+/g, '_');
        var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
        downloadTextFile('diagnose_' + safe + '_' + stamp + '.txt', text);
        if (typeof global.showToast === 'function') global.showToast(cat('downloadDiag'), 'success');
      };
    }
    var copyBlockedBtn = byID('configAgentCopyBlockedBtn');
    if (copyBlockedBtn) {
      copyBlockedBtn.onclick = function() {
        copyText(formatBlockedRoutesText(lastDiagnoseHint), copyBlockedBtn);
      };
    }
    var copyBlockedMdBtn = byID('configAgentCopyBlockedMdBtn');
    if (copyBlockedMdBtn) {
      copyBlockedMdBtn.onclick = function() {
        copyText(formatBlockedRoutesMarkdown(lastDiagnoseHint), copyBlockedMdBtn);
      };
    }
    var copySupportInlineBtn = byID('configAgentCopySupportInlineBtn');
    if (copySupportInlineBtn) {
      copySupportInlineBtn.onclick = function() {
        copyText(formatSupportPackJSON(lastDiagnoseHint), copySupportInlineBtn);
      };
    }
    var copyHandoffInlineBtn = byID('configAgentCopyHandoffInlineBtn');
    if (copyHandoffInlineBtn) {
      copyHandoffInlineBtn.onclick = function() {
        copyText(formatSupportHandoffText(lastDiagnoseHint), copyHandoffInlineBtn);
      };
    }
    var mailtoHandoffInlineBtn = byID('configAgentMailtoHandoffInlineBtn');
    if (mailtoHandoffInlineBtn) {
      mailtoHandoffInlineBtn.onclick = function() { openSupportMailto(lastDiagnoseHint); };
    }
    var dlSupportInlineBtn = byID('configAgentDownloadSupportInlineBtn');
    if (dlSupportInlineBtn) {
      dlSupportInlineBtn.onclick = function() { downloadSupportPackJson(lastDiagnoseHint); };
    }
    var diagSummaryBtn = byID('configAgentDiagSummaryBtn');
    if (diagSummaryBtn) {
      diagSummaryBtn.onclick = function() {
        diagRoutesExpanded = !diagRoutesExpanded;
        updateSelectedBar();
      };
    }
  }

  var MAX_CHAT_ROWS = 80;

  function removeMarkedChatBubble(log, selector) {
    if (!log || !selector) return false;
    var mark = log.querySelector(selector);
    if (!mark) return false;
    var row = mark;
    while (row && row.parentNode !== log) row = row.parentNode;
    if (row && row.parentNode === log) {
      try {
        log.removeChild(row);
        return true;
      } catch (_) {}
    }
    return false;
  }

  function latestChatRow(log) {
    if (!log) return null;
    var rows = log.querySelectorAll('[data-ca-chat-row]');
    return rows.length ? rows[rows.length - 1] : null;
  }

  function appendChat(role, html) {
    var log = byID('configAgentChatLog');
    if (!log) return;
    // Follow the conversation only when the operator was already at its end.
    // This preserves their reading position when they scroll back, and avoids
    // landing in the expanded history disclosure that follows the chat rows.
    var followConversation = log.scrollHeight - log.scrollTop - log.clientHeight < 48;
    var empty = byID('configAgentEmptyHint');
    if (empty) empty.style.display = 'none';
    var row = document.createElement('div');
    row.className = 'item config-agent-message config-agent-message-' + (role === 'user' ? 'user' : 'assistant');
    row.setAttribute('data-ca-chat-row', '1');
    row.style.cssText = 'margin-bottom:8px;padding:10px 12px;' + (role === 'user' ? 'background:#f4f7ff;border-color:rgba(47,128,237,.2)' : '');
    row.innerHTML = html;
    var historyDisclosure = log.querySelector('.overview-assistant-history-disclosure');
    if (historyDisclosure) log.insertBefore(row, historyDisclosure);
    else log.appendChild(row);
    // Cap chat DOM growth; keep structural nodes (examples/history/empty).
    var rows = log.querySelectorAll('[data-ca-chat-row]');
    var overflow = rows.length - MAX_CHAT_ROWS;
    for (var i = 0; i < overflow; i++) {
      if (rows[i] && rows[i].parentNode === log) {
        try { log.removeChild(rows[i]); } catch (_) {}
      }
    }
    if (followConversation) {
      var rowBottom = row.offsetTop + row.offsetHeight;
      log.scrollTop = Math.max(0, rowBottom - log.clientHeight + 12);
    }
  }

  function renderPlanCard(plan) {
    var steps = (plan.steps || []).map(function(s, i) {
      var api = s.api_preview || {};
      return '<div class="item-meta" style="margin-top:6px"><strong>' + (i + 1) + '.</strong> [' + esc(s.mode) + '] ' + esc(s.tool)
        + (s.purpose ? ' - ' + esc(s.purpose) : '')
        + (api.method ? ' <span class="mono">' + esc(api.method + ' ' + (api.path || '')) + '</span>' : '')
        + (s.optional ? ' <em>(optional)</em>' : '')
        + '</div>';
    }).join('');
    var hasMissing = !!(plan.missing_fields || []).length;
    var missing = hasMissing
      ? '<div class="item-meta" style="margin-top:8px;color:#b42318">' + esc(cat('missing')) + ': ' + esc((plan.missing_fields || []).join(', ')) + '</div>'
      : '';
    // Interactive multi-turn fill only for live incomplete plans (confirm_token present).
    var fillPanel = (hasMissing && plan.confirm_token) ? renderMissingFieldsPanel(plan) : '';
    var assumptions = (plan.assumptions || []).length
      ? '<div class="item-meta" style="margin-top:8px;color:#b54708"><strong>' + esc(cat('assumptions')) + '</strong><ul style="margin:4px 0 0 16px;padding:0">'
        + (plan.assumptions || []).map(function(a) {
          return '<li style="margin-top:2px">' + esc(String(a)) + '</li>';
        }).join('')
        + '</ul></div>'
      : '';
    var simHtml = renderSimulatedPreview(plan.simulated);
    var actions = '<div class="actions" style="margin-top:10px;gap:8px;flex-wrap:wrap">';
    // History replay has no confirm_token; only live plans can execute.
    if (!hasMissing && plan.confirm_token) {
      actions += '<button class="btn-primary" type="button" data-ca-confirm="1">' + esc(cat('confirm')) + '</button>'
        + '<button class="btn-ghost" type="button" data-ca-cancel="1">' + esc(cat('cancel')) + '</button>';
    }
    if (hasMissing && plan.confirm_token) {
      actions += '<button class="btn-ghost" type="button" data-ca-cancel="1">' + esc(cat('cancel')) + '</button>';
    }
    if (configAgentSessionID && plan.confirm_token) {
      actions += '<button class="btn-ghost" type="button" data-ca-new-session="1">' + esc(cat('newSession')) + '</button>';
    }
    actions += '<button class="btn-ghost" type="button" data-ca-copy-plan="1">' + esc(cat('copy')) + '</button>'
      + '<button class="btn-ghost" type="button" data-ca-download-plan="1">' + esc(cat('downloadPlan')) + '</button></div>';
    var planner = plan.planner ? (' | planner: <span class="mono">' + esc(plan.planner) + '</span>') : '';
    var autoHint = planIsAutoExecutable(plan)
      ? ' <span class="item-meta" style="color:#027a48">[' + esc(cat('autoRead')) + ']</span>'
      : '';
    var sessHint = (configAgentSessionID && (hasMissing || (sessionTurns && sessionTurns.length > 1)))
      ? '<div class="item-meta" style="margin-top:4px;color:#b54708">' + esc(cat('sessionActive'))
        + ': <span class="mono">' + esc(String(configAgentSessionID).slice(0, 18)) + '</span></div>'
      : '';
    var timeline = (sessionTurns && sessionTurns.length > 1) ? renderSessionTimeline(sessionTurns) : '';
    var planTTLHtml = '';
    if (plan.confirm_token && plan.expires_at) {
      var expMs = Date.parse(String(plan.expires_at));
      if (!isNaN(expMs)) {
        var left0 = expMs - Date.now();
        planTTLHtml = '<div class="item-meta mono" id="configAgentPlanTTL" style="margin-top:4px;color:'
          + (left0 > 0 ? '#b54708' : '#b42318') + '">'
          + esc(left0 > 0 ? (cat('planTTL') + ' ' + formatSessionTTL(left0)) : cat('planExpired'))
          + '</div>';
      }
    }
    return '<div class="item-title">' + esc(cat('planTitle')) + '</div>'
      + '<div class="item-meta" style="margin-top:4px">' + esc(plan.summary || plan.intent || '') + autoHint + '</div>'
      + '<div class="item-meta" style="margin-top:4px">' + esc(cat('risk')) + ': ' + esc(plan.risk_level || '-') + ' | intent: <span class="mono">' + esc(plan.intent || '') + '</span>' + planner + '</div>'
      + planTTLHtml + sessHint + timeline + missing + fillPanel + assumptions + steps + simHtml + actions;
  }

  function renderChipList(ids, color) {
    ids = ids || [];
    if (!ids.length) return '<span class="item-meta" style="opacity:.6">(none)</span>';
    return ids.map(function(id) {
      return '<span class="mono" style="display:inline-block;margin:2px 4px 2px 0;padding:1px 6px;border-radius:999px;background:'
        + (color || 'rgba(47,128,237,.08)') + ';font-size:11px">' + esc(String(id)) + '</span>';
    }).join('');
  }

  function idSet(list) {
    var s = {};
    (list || []).forEach(function(id) { s[String(id)] = true; });
    return s;
  }

  function renderBindCompareTable(sim) {
    var cur = sim.current_service_group_ids || [];
    var tgt = sim.target_service_group_ids || [];
    var add = idSet(sim.added_service_group_ids || []);
    var rem = idSet(sim.removed_service_group_ids || []);
    var all = {};
    cur.forEach(function(id) { all[String(id)] = true; });
    tgt.forEach(function(id) { all[String(id)] = true; });
    Object.keys(add).forEach(function(id) { all[id] = true; });
    Object.keys(rem).forEach(function(id) { all[id] = true; });
    var ids = Object.keys(all).sort();
    if (!ids.length && !sim.email) return '';
    var rows = ids.map(function(id) {
      var before = cur.indexOf(id) >= 0 || cur.indexOf(String(id)) >= 0;
      // also check string equality in arrays
      before = (cur || []).some(function(x) { return String(x) === id; });
      var after = (tgt || []).some(function(x) { return String(x) === id; });
      var change = '\u2014';
      var color = '#667085';
      if (add[id] || (after && !before)) {
        change = '+' + cat('added');
        color = '#027a48';
      } else if (rem[id] || (before && !after)) {
        change = '\u2212' + cat('removed');
        color = '#b42318';
      } else if (before && after) {
        change = cat('kept');
        color = '#667085';
      }
      return '<tr>'
        + '<td class="mono" style="padding:4px 6px">' + esc(id) + '</td>'
        + '<td style="padding:4px 6px;text-align:center">' + (before ? '\u2713' : '\u00b7') + '</td>'
        + '<td style="padding:4px 6px;text-align:center">' + (after ? '\u2713' : '\u00b7') + '</td>'
        + '<td style="padding:4px 6px;color:' + color + '">' + esc(change) + '</td>'
        + '</tr>';
    }).join('');
    return '<div style="margin-top:8px;border:1px solid rgba(20,24,36,.08);border-radius:8px;overflow:hidden">'
      + '<table style="width:100%;border-collapse:collapse;font-size:11px">'
      + '<thead><tr style="background:#f8fafc;text-align:left">'
      + '<th style="padding:6px">' + esc(cat('group')) + '</th>'
      + '<th style="padding:6px;text-align:center">' + esc(cat('before')) + '</th>'
      + '<th style="padding:6px;text-align:center">' + esc(cat('after')) + '</th>'
      + '<th style="padding:6px">' + esc(cat('change')) + '</th>'
      + '</tr></thead><tbody>'
      + (rows || '<tr><td colspan="4" style="padding:6px;opacity:.6">(none)</td></tr>')
      + '</tbody></table></div>';
  }

  function renderSimulatedPreview(sim) {
    if (!sim || typeof sim !== 'object') return '';
    // Binding diff (user_bind / user_unbind)
    if (Object.prototype.hasOwnProperty.call(sim, 'current_service_group_ids') ||
        Object.prototype.hasOwnProperty.call(sim, 'target_service_group_ids')) {
      var cur = sim.current_service_group_ids || [];
      var tgt = sim.target_service_group_ids || [];
      var add = sim.added_service_group_ids || [];
      var html = '<div class="item-meta" style="margin-top:8px"><strong>' + esc(cat('bindDiff')) + '</strong>'
        + (sim.email ? ' <span class="mono">' + esc(String(sim.email)) + '</span>' : '')
        + '</div>';
      if (sim.unchanged) {
        html += '<div class="item-meta" style="margin-top:4px;color:#027a48">' + esc(cat('unchanged')) + '</div>';
      }
      html += renderBindCompareTable(sim);
      // compact chips under table
      html += '<div class="item-meta" style="margin-top:6px">' + esc(cat('current')) + ': ' + renderChipList(cur) + '</div>';
      if (add.length) {
        html += '<div class="item-meta" style="margin-top:4px">' + esc(cat('added')) + ': '
          + renderChipList(add, 'rgba(2,122,72,.12)') + '</div>';
      }
      var rem = sim.removed_service_group_ids || [];
      if (rem.length) {
        html += '<div class="item-meta" style="margin-top:4px">' + esc(cat('removed')) + ': '
          + renderChipList(rem, 'rgba(180,35,24,.12)') + '</div>';
      }
      html += '<div class="item-meta" style="margin-top:4px">' + esc(cat('target')) + ': '
        + renderChipList(tgt, 'rgba(47,128,237,.12)') + '</div>';
      var unknown = sim.unknown_service_group_ids || [];
      if (unknown.length) {
        html += '<div class="item-meta" style="margin-top:6px;color:#b42318"><strong>' + esc(cat('unknownGroups')) + '</strong>: '
          + renderChipList(unknown, 'rgba(180,35,24,.15)') + '</div>';
        var known = sim.known_service_group_ids || [];
        if (known.length) {
          html += '<div class="item-meta" style="margin-top:4px">' + esc(cat('availableGroups')) + ': '
            + renderClickableGroupIds(known) + '</div>';
        }
      }
      if (sim.merge) {
        html += '<div class="item-meta" style="margin-top:4px;opacity:.75">merge = true</div>';
      }
      if (sim.remove_all) {
        html += '<div class="item-meta" style="margin-top:4px;opacity:.75">remove_all = true</div>';
      }
      return html;
    }
    return '<pre class="mono" style="margin-top:8px;white-space:pre-wrap;font-size:11px;max-height:160px;overflow:auto">'
      + esc(JSON.stringify(sim, null, 2)) + '</pre>';
  }

  function renderClickableGroupIds(ids) {
    ids = ids || [];
    if (!ids.length) return '<span class="item-meta" style="opacity:.6">(none)</span>';
    return ids.map(function(id) {
      return '<button type="button" class="btn-ghost mono" style="height:24px;font-size:11px;padding:0 8px;margin:2px 4px 2px 0" data-ca-use-group="'
        + esc(String(id)) + '">' + esc(String(id)) + '</button>';
    }).join('');
  }

  function applyGroupToInput(groupId, mode) {
    groupId = String(groupId || '').trim();
    if (!groupId) return;
    var input = byID('configAgentInput');
    if (!input) return;
    mode = mode === 'unbind' ? 'unbind' : 'bind';
    var cur = String(input.value || '');
    var parsed = parseBindLine(cur);
    var email = extractEmailFromText(cur) || lastRememberedEmail || '';
    var groups = [];
    if (parsed) {
      // Same mode: accumulate. Different mode: start fresh list for new mode.
      if (parsed.mode === mode) {
        groups = (parsed.groups || []).slice();
      }
      if (parsed.email) email = parsed.email;
    }
    if (groups.indexOf(groupId) < 0) groups.push(groupId);
    if (email) rememberEmail(email, { silent: true });
    rememberGroups([groupId], { silent: true });
    input.value = buildBindLine(mode, email, groups);
    input.focus();
    // Place caret for missing email: after "bind user " / "unbind user "
    if (!email && input.setSelectionRange) {
      var marker = mode === 'unbind' ? 'unbind user ' : 'bind user ';
      var pos = marker.length;
      input.setSelectionRange(pos, pos);
    }
    updateSelectedBar();
  }

  function bindGroupPickers(row) {
    if (!row) return;
    row.querySelectorAll('[data-ca-use-group]').forEach(function(btn) {
      btn.onclick = function() {
        applyGroupToInput(btn.getAttribute('data-ca-use-group'), 'bind');
      };
    });
    row.querySelectorAll('[data-ca-unbind-group]').forEach(function(btn) {
      btn.onclick = function() {
        applyGroupToInput(btn.getAttribute('data-ca-unbind-group'), 'unbind');
      };
    });
    row.querySelectorAll('[data-ca-copy-group]').forEach(function(btn) {
      btn.onclick = function() {
        copyText(btn.getAttribute('data-ca-copy-group') || '', btn);
      };
    });
  }

  function reconstructRerunMessage(payload, plan, item) {
    var src = String((payload && payload.source_message) || (plan && plan.source_message) || '');
    if (src) return src;
    var intent = String((payload && payload.intent) || (plan && plan.intent) || (item && item.intent) || '');
    var results = (payload && payload.results) || [];
    var first = results[0] && results[0].result ? results[0].result : null;
    if (intent === 'llm.services.diagnose' || (results[0] && results[0].tool === 'llm.services.diagnose')) {
      var email = (first && first.email) || (plan && plan.steps && plan.steps[0] && plan.steps[0].args && plan.steps[0].args.email) || '';
      if (email) return 'diagnose LLM service for ' + email;
      return 'diagnose LLM entitlement';
    }
    if (intent === 'llm.services.list') return 'list service groups';
    if (intent === 'invitation_codes.list') return 'list invitation codes';
    if (intent === 'invitation_codes.required.get') return 'show invitation code required status';
    if (intent === 'migration.settings.get') return 'show migration settings';
    if (intent === 'system_free.get' || intent === 'system_free.test') return 'test system-free';
    if (intent === 'mail.sender_name.get') return 'show mail sender name';
    if (intent === 'card_store.config.get') return 'show card store config';
    if (intent === 'feishu.auto_enroll.get') return 'show feishu auto enroll';
    if (intent === 'feishu.config.get') return 'show feishu config';
    if (intent.indexOf('.get') > 0 || intent.indexOf('.list') > 0) {
      return 'show ' + intent.replace(/\./g, ' ').replace(/ get$| list$/, '');
    }
    return '';
  }

  function isRerunnableRead(payload, plan) {
    var steps = (plan && plan.steps) || [];
    if (steps.length && steps.every(function(s) {
      var m = String(s.mode || '');
      return m === 'read' || m === 'probe';
    })) return true;
    var intent = String((payload && payload.intent) || (plan && plan.intent) || '');
    if (!intent) return false;
    return /\.(get|list|diagnose|test)$/.test(intent) || intent.indexOf('list_') >= 0;
  }

  function copyText(text, btn) {
    var done = function() {
      if (btn) {
        var prev = btn.textContent;
        btn.textContent = cat('copied');
        setTimeout(function() { btn.textContent = prev; }, 1200);
      }
      if (typeof global.showToast === 'function') global.showToast(cat('copied'), 'success');
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(function() {
        // fallback below
        try {
          var ta = document.createElement('textarea');
          ta.value = text;
          ta.style.position = 'fixed';
          ta.style.left = '-9999px';
          document.body.appendChild(ta);
          ta.select();
          document.execCommand('copy');
          ta.remove();
          done();
        } catch (_) {}
      });
      return;
    }
    try {
      var ta2 = document.createElement('textarea');
      ta2.value = text;
      ta2.style.position = 'fixed';
      ta2.style.left = '-9999px';
      document.body.appendChild(ta2);
      ta2.select();
      document.execCommand('copy');
      ta2.remove();
      done();
    } catch (_) {}
  }

  function downloadTextFile(filename, text, mimeType) {
    var blob = new Blob([text || ''], { type: mimeType || 'text/plain;charset=utf-8' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = filename || 'download.txt';
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(function() { URL.revokeObjectURL(url); }, 1000);
  }

  function planIsAutoExecutable(plan) {
    if (!plan || !plan.plan_id || !plan.confirm_token) return false;
    if ((plan.missing_fields || []).length) return false;
    var steps = plan.steps || [];
    if (!steps.length) return false;
    return steps.every(function(s) {
      var mode = String(s.mode || '');
      return mode === 'read' || mode === 'probe';
    });
  }

  function configAgentIsBusy() {
    return !!(submitConfigAgent._busy || executePendingPlan._busy);
  }

  function firstStepResult(data, toolName) {
    var results = (data && data.results) || [];
    var want = String(toolName || '');
    // Exact tool match only (prefix matching can collide across tool names).
    if (want) {
      for (var i = 0; i < results.length; i++) {
        var r = results[i] || {};
        if (String(r.tool || '') === want) return r.result;
      }
      return null;
    }
    return results.length ? (results[0] && results[0].result) : null;
  }

  function hasSystemFreeBinding(diag) {
    var binds = (diag && diag.direct_user_bindings) || [];
    for (var i = 0; i < binds.length; i++) {
      var ids = (binds[i] && binds[i].service_group_ids) || [];
      for (var j = 0; j < ids.length; j++) {
        if (String(ids[j]) === 'system-free') return true;
      }
    }
    return false;
  }

  function shouldSuggestBind(diag) {
    if (!diag || !diag.email) return false;
    if (hasSystemFreeBinding(diag)) return false;
    var binds = diag.direct_user_bindings || [];
    var bad = (diag.billing_routes || []).filter(function(x) { return x && !x.eligible; });
    return binds.length === 0 || bad.length > 0;
  }

  function renderDiagnoseSummary(diag) {
    if (!diag || typeof diag !== 'object') return { html: '', suggestEmail: '' };
    var routes = diag.billing_routes || [];
    var okRoutes = routes.filter(function(x) { return x && x.eligible; });
    var badRoutes = routes.filter(function(x) { return x && !x.eligible; });
    var bindCount = (diag.direct_user_bindings || []).length;
    var grantCount = (diag.active_grants || []).length;
    var head = '<div class="item-meta" style="margin-top:8px"><strong>' + esc(cat('routes')) + '</strong>: '
      + okRoutes.length + ' ' + esc(cat('eligible')) + ' / '
      + badRoutes.length + ' ' + esc(cat('ineligible'))
      + ' | ' + esc(cat('bindings')) + ': ' + bindCount
      + ' | ' + esc(cat('grants')) + ': ' + grantCount
      + '</div>';
    if (diag.email) {
      head = '<div class="item-meta mono" style="margin-top:6px">email: ' + esc(diag.email) + '</div>' + head;
    }
    var badLines = badRoutes.slice(0, 8).map(function(x) {
      return '<div class="item-meta" style="margin-top:4px;color:#b42318">'
        + esc((x.model_name || '?') + ' @ ' + (x.provider_id || '?'))
        + ' - ' + esc(x.reason_code || '')
        + (x.reason_message ? ': ' + esc(x.reason_message) : '')
        + '</div>';
    }).join('');
    var okLines = okRoutes.slice(0, 5).map(function(x) {
      return '<div class="item-meta" style="margin-top:3px;color:#027a48">'
        + esc((x.model_name || '?') + ' @ ' + (x.provider_id || '?'))
        + (x.credits_available ? ' (credits ' + esc(String(x.credits_available)) + ')' : '')
        + '</div>';
    }).join('');
    var suggest = '';
    var suggestEmail = '';
    if (shouldSuggestBind(diag)) {
      suggestEmail = String(diag.email || '');
      suggest = '<div class="item-meta" style="margin-top:8px;color:#b54708">' + esc(cat('suggestBind')) + '</div>';
    }
    return { html: head + okLines + badLines + suggest, suggestEmail: suggestEmail };
  }

  function renderExportSummary(exp) {
    if (!exp || typeof exp !== 'object') return { html: '', text: '' };
    var codes = Array.isArray(exp.codes) ? exp.codes : [];
    var count = exp.count != null ? exp.count : codes.length;
    var text = String(exp.text || '');
    if (!text && codes.length) {
      text = codes.map(function(c) { return c && c.code ? c.code : ''; }).filter(Boolean).join('\n');
    }
    var html = '<div class="item-meta" style="margin-top:8px"><strong>' + esc(cat('exportedCodes')) + '</strong>: '
      + esc(String(count))
      + (exp.vip_only ? ' (vip)' : '')
      + (exp.exported ? ' | filter=' + esc(String(exp.exported)) : '')
      + '</div>'
      + (exp.note ? '<div class="item-meta" style="margin-top:4px;color:#b54708">' + esc(String(exp.note)) + '</div>' : '');
    if (codes.length) {
      var rows = codes.slice(0, 20).map(function(c) {
        return '<tr>'
          + '<td class="mono" style="padding:2px 6px">' + esc(c && c.code || '') + '</td>'
          + '<td style="padding:2px 6px">' + (c && c.vip ? esc(cat('vip')) : '') + '</td>'
          + '<td class="mono" style="padding:2px 6px;opacity:.7">' + esc(c && c.id || '') + '</td>'
          + '</tr>';
      }).join('');
      html += '<div class="item-meta" style="margin-top:8px"><strong>' + esc(cat('preview')) + '</strong>'
        + (codes.length > 20 ? ' (20/' + codes.length + ')' : '') + '</div>'
        + '<div style="margin-top:4px;max-height:160px;overflow:auto;border:1px solid rgba(20,24,36,.08);border-radius:6px">'
        + '<table style="width:100%;border-collapse:collapse;font-size:11px">'
        + '<thead><tr style="text-align:left;background:#f8fafc">'
        + '<th style="padding:4px 6px">' + esc(cat('code')) + '</th>'
        + '<th style="padding:4px 6px">' + esc(cat('vip')) + '</th>'
        + '<th style="padding:4px 6px">id</th>'
        + '</tr></thead><tbody>' + rows + '</tbody></table></div>';
    } else if (text) {
      var lines = text.split(/\r?\n/).filter(Boolean).slice(0, 20);
      html += '<div class="item-meta mono" style="margin-top:6px;white-space:pre-wrap">'
        + esc(lines.join('\n'))
        + (text.split(/\r?\n/).filter(Boolean).length > 20 ? '\n...' : '')
        + '</div>';
    }
    return { html: html, text: text };
  }

  function renderExecuteResult(data, opts) {
    opts = opts || {};
    // History replay must not clobber live diagnose status bar / wizard flags.
    var replay = !!opts.replay;
    var ok = !!(data && data.ok);
    var intent = (data && data.intent) || '';
    var resultJSON = JSON.stringify(data, null, 2);
    var summary = '';
    var actions = '<div class="actions" style="margin-top:8px;gap:8px;flex-wrap:wrap">'
      + '<button class="btn-ghost" type="button" data-ca-copy-result="1">' + esc(cat('copy')) + '</button>';

    var diagnose = firstStepResult(data, 'llm.services.diagnose');
    var exportRes = firstStepResult(data, 'invitation_codes.export');
    var listSvc = firstStepResult(data, 'llm.services.list');
    var bindRes = firstStepResult(data, 'llm.services.user_bind');
    var unbindRes = firstStepResult(data, 'llm.services.user_unbind');
    var exportText = '';
    var suggestEmail = '';
    var unbindEmail = '';
    // Snapshot of diagnose used for support-pack actions on this card (not always lastDiagnoseHint).
    var cardDiagnoseHint = null;

    if (diagnose) {
      var dsum = renderDiagnoseSummary(diagnose);
      summary += dsum.html || '';
      suggestEmail = dsum.suggestEmail || '';
      if (!replay && diagnose.email) rememberEmail(diagnose.email, { silent: true });
      // Pull binding group ids into recent chips.
      var bindIds = [];
      (diagnose.direct_user_bindings || []).forEach(function(b) {
        (b.service_group_ids || []).forEach(function(id) { bindIds.push(id); });
      });
      if (!replay && bindIds.length) rememberGroups(bindIds, { silent: true });
      var hint = hintFromDiagnoseResult(diagnose, suggestEmail);
      cardDiagnoseHint = hint;
      if (!replay) {
        saveDiagnoseHint(hint);
        // Auto-expand when there are blocked routes so admin sees issues immediately.
        if (hint && Number(hint.bad) > 0) diagRoutesExpanded = true;
      }
      var canBind = !!suggestEmail;
      var canUnbind = hasSystemFreeBinding(diagnose) && !!diagnose.email;
      if (canUnbind) unbindEmail = String(diagnose.email);
      // Diagnose \u2192 bind \u2192 re-check wizard (step 3 when coming from auto re-diagnose).
      var wizardStep = (!replay && wizardExpectRecheck) ? 3 : 1;
      if (!replay && wizardExpectRecheck) wizardExpectRecheck = false;
      summary += renderDiagnoseWizardStrip({
        step: wizardStep,
        canBind: canBind && !replay,
        canUnbind: canUnbind && !replay,
        email: String(diagnose.email || '')
      });
      actions += '<button class="btn-secondary" type="button" data-ca-copy-support="1">' + esc(cat('copySupport')) + '</button>'
        + '<button class="btn-ghost" type="button" data-ca-copy-handoff="1">' + esc(cat('handoff')) + '</button>'
        + '<button class="btn-ghost" type="button" data-ca-mailto-handoff="1">' + esc(cat('handoffMail')) + '</button>'
        + '<button class="btn-ghost" type="button" data-ca-download-support="1">' + esc(cat('downloadSupport')) + '</button>';
    }
    if (!replay && configAgentSessionID) {
      actions += '<button class="btn-ghost" type="button" data-ca-new-session="1">' + esc(cat('newSession')) + '</button>';
    }
    if (bindRes) {
      if (!replay && bindRes.email) rememberEmail(bindRes.email, { silent: true });
      var added = bindRes.added_service_group_ids || bindRes.service_group_ids || [];
      if (!replay && added.length) rememberGroups(added, { silent: true });
      summary += renderSimulatedPreview(bindRes);
    }
    if (unbindRes) {
      if (!replay && unbindRes.email) rememberEmail(unbindRes.email, { silent: true });
      summary += renderSimulatedPreview(unbindRes);
    }
    if (bindRes || unbindRes) {
      // After bind/unbind, show wizard step 2\u21923 (re-check will auto-run).
      var be = (bindRes && bindRes.email) || (unbindRes && unbindRes.email) || '';
      summary += renderDiagnoseWizardStrip({
        step: 2,
        canBind: false,
        canUnbind: false,
        email: String(be || '')
      });
    }
    if (exportRes) {
      var esum = renderExportSummary(exportRes);
      summary += esum.html || '';
      exportText = esum.text || '';
      if (exportText) {
        actions += '<button class="btn-ghost" type="button" data-ca-copy-codes="1">' + esc(cat('copyCodes')) + '</button>'
          + '<button class="btn-secondary" type="button" data-ca-download-codes="1">' + esc(cat('download')) + '</button>';
      }
    }
    if (listSvc && listSvc.model_service_groups) {
      var groups = listSvc.model_service_groups || [];
      if (!replay) {
        lastServiceGroups = groups.map(function(g) {
          return { id: g.id, name: g.name, system_free: !!g.system_free };
        });
        rememberGroups(groups.map(function(g) { return g.id; }).filter(Boolean).slice(0, 6), { silent: true });
      }
      summary += '<div class="item-meta" style="margin-top:8px"><strong>' + esc(cat('pickGroup')) + '</strong>: '
        + groups.length
        + (listSvc.system_default_service_group ? ' | default=' + esc(String(listSvc.system_default_service_group)) : '')
        + '</div>';
      summary += groups.slice(0, 20).map(function(g) {
        var gid = String(g.id || '');
        return '<div class="item-meta" style="margin-top:4px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
          + '<span class="mono">' + esc(gid)
          + (g.system_free ? ' [system-free]' : '')
          + ' | ' + esc(g.access_policy || '')
          + ' | models=' + esc(String(g.model_count != null ? g.model_count : (g.models || []).length))
          + '</span>'
          + (replay ? '' : (
            '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-use-group="' + esc(gid) + '">' + esc(cat('useGroup')) + '</button>'
            + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-unbind-group="' + esc(gid) + '">' + esc(cat('unbind')) + '</button>'
          ))
          + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-copy-group="' + esc(gid) + '">' + esc(cat('copyId')) + '</button>'
          + '</div>';
      }).join('');
    }

    // Step status strip
    var steps = (data && data.results) || [];
    if (steps.length) {
      summary += '<div class="item-meta" style="margin-top:8px"><strong>' + esc(cat('steps')) + '</strong></div>';
      summary += steps.map(function(s) {
        var color = s.ok ? '#027a48' : (s.skipped ? '#667085' : '#b42318');
        return '<div class="item-meta mono" style="margin-top:3px;color:' + color + '">'
          + esc(s.tool || s.step_id || '')
          + (s.skipped ? ' skipped' : (s.ok ? ' ok' : ' fail'))
          + (s.error ? ' - ' + esc(s.error) : '')
          + '</div>';
      }).join('');
    }

    actions += '</div>';
    return {
      html: '<div class="item-title">' + esc(ok ? cat('done') : cat('failed')) + '</div>'
        + (intent ? '<div class="item-meta mono" style="margin-top:4px">intent: ' + esc(intent) + '</div>' : '')
        + summary + actions
        + '<details style="margin-top:8px"><summary class="item-meta">' + esc(cat('raw')) + '</summary>'
        + '<pre class="mono" style="margin-top:6px;white-space:pre-wrap;font-size:11px;max-height:200px;overflow:auto">'
        + esc(resultJSON) + '</pre></details>',
      resultJSON: resultJSON,
      exportText: exportText,
      suggestEmail: replay ? '' : suggestEmail,
      unbindEmail: replay ? '' : unbindEmail,
      hasDiagnose: !!diagnose,
      diagnoseHint: cardDiagnoseHint,
      replay: replay
    };
  }

  function bindExecuteResultActions(row, payload) {
    if (!row || !payload) return;
    bindGroupPickers(row);
    var copyBtn = row.querySelector('[data-ca-copy-result]');
    if (copyBtn) copyBtn.onclick = function() { copyText(payload.resultJSON, copyBtn); };
    var copyCodes = row.querySelector('[data-ca-copy-codes]');
    if (copyCodes && payload.exportText) {
      copyCodes.onclick = function() { copyText(payload.exportText, copyCodes); };
    }
    var dl = row.querySelector('[data-ca-download-codes]');
    if (dl && payload.exportText) {
      dl.onclick = function() {
        var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
        downloadTextFile('invitation_codes_' + stamp + '.txt', payload.exportText);
      };
    }
    var supportHint = payload.diagnoseHint || lastDiagnoseHint;
    var copySupportChat = row.querySelector('[data-ca-copy-support]');
    if (copySupportChat && payload.hasDiagnose) {
      copySupportChat.onclick = function() {
        copyText(formatSupportPackJSON(supportHint), copySupportChat);
      };
    }
    var copyHandoffChat = row.querySelector('[data-ca-copy-handoff]');
    if (copyHandoffChat && payload.hasDiagnose) {
      copyHandoffChat.onclick = function() {
        copyText(formatSupportHandoffText(supportHint), copyHandoffChat);
      };
    }
    var mailtoHandoffChat = row.querySelector('[data-ca-mailto-handoff]');
    if (mailtoHandoffChat && payload.hasDiagnose) {
      mailtoHandoffChat.onclick = function() { openSupportMailto(supportHint); };
    }
    var dlSupportChat = row.querySelector('[data-ca-download-support]');
    if (dlSupportChat && payload.hasDiagnose) {
      dlSupportChat.onclick = function() { downloadSupportPackJson(supportHint); };
    }
    // Wizard / result may have multiple bind-sf buttons (strip + legacy).
    row.querySelectorAll('[data-ca-bind-sf]').forEach(function(bindBtn) {
      if (!payload.suggestEmail) return;
      bindBtn.onclick = function() {
        rememberEmail(payload.suggestEmail);
        applyGroupToInput('system-free', 'bind');
        submitConfigAgent();
      };
    });
    row.querySelectorAll('[data-ca-unbind-sf]').forEach(function(unbindBtn) {
      if (!payload.unbindEmail) return;
      unbindBtn.onclick = function() {
        rememberEmail(payload.unbindEmail);
        applyGroupToInput('system-free', 'unbind');
        submitConfigAgent();
      };
    });
    var newSess = row.querySelector('[data-ca-new-session]');
    if (newSess) {
      newSess.onclick = function() {
        clearConfigAgentSession({ toast: cat('newSession') });
      };
    }
  }

  function pushPlanHistory(plan, status) {
    if (!plan) return;
    planHistory.unshift({
      at: new Date().toISOString(),
      intent: plan.intent || '',
      summary: plan.summary || '',
      planner: plan.planner || '',
      status: status || 'planned',
      plan_id: plan.plan_id || '',
      audit_id: plan.audit_id || ''
    });
    if (planHistory.length > 20) planHistory = planHistory.slice(0, 20);
    renderPlanHistory();
  }

  var historyActionFilter = '';
  var historyIntentFilter = '';
  var historyGroupBySession = false;
  var catalogCollapsedDomains = {}; // domain -> true when collapsed
  var lastCatalogTools = []; // cached catalog tools for client-side filter
  var lastCatalogExampleByName = {}; // name -> example (O(1) lookup)
  var lastCatalogExamples = [];
  var catalogFilterQuery = '';
  var catalogSearchTimer = null;
  var favoriteCommands = []; // [{text, label}]
  var recentCommands = []; // [{text, label, at}]
  var GROUP_BY_SESSION_KEY = 'hub.configAgent.historyGroupBySession';
  var CATALOG_COLLAPSED_KEY = 'hub.configAgent.catalogCollapsedDomains';
  var FAVORITES_KEY = 'hub.configAgent.favorites';
  var RECENT_CMDS_KEY = 'hub.configAgent.recentCommands';
  var MAX_FAVORITES = 10;
  var MAX_RECENT_CMDS = 8;

  function loadUIPrefs() {
    try {
      historyGroupBySession = localStorage.getItem(GROUP_BY_SESSION_KEY) === '1';
    } catch (_) {
      historyGroupBySession = false;
    }
    try {
      var raw = localStorage.getItem(CATALOG_COLLAPSED_KEY);
      var obj = raw ? JSON.parse(raw) : {};
      catalogCollapsedDomains = (obj && typeof obj === 'object' && !Array.isArray(obj)) ? obj : {};
    } catch (_) {
      catalogCollapsedDomains = {};
    }
    loadFavorites();
    loadRecentCommands();
  }

  function loadFavorites() {
    try {
      var raw = localStorage.getItem(FAVORITES_KEY);
      var arr = raw ? JSON.parse(raw) : [];
      if (!Array.isArray(arr)) {
        favoriteCommands = [];
        return;
      }
      favoriteCommands = arr.map(function(it) {
        if (typeof it === 'string') return { text: it, label: it };
        return {
          text: String((it && it.text) || '').trim(),
          label: String((it && it.label) || (it && it.text) || '').trim()
        };
      }).filter(function(it) { return !!it.text; }).slice(0, MAX_FAVORITES);
    } catch (_) {
      favoriteCommands = [];
    }
  }

  function persistFavorites() {
    try {
      localStorage.setItem(FAVORITES_KEY, JSON.stringify(favoriteCommands.slice(0, MAX_FAVORITES)));
    } catch (_) {}
  }

  function isFavoriteCommand(text) {
    text = String(text || '').trim();
    if (!text) return false;
    return favoriteCommands.some(function(f) { return f.text === text; });
  }

  function toggleFavoriteCommand(text, label) {
    text = String(text || '').trim();
    if (!text) return false;
    label = String(label || text).trim() || text;
    var idx = -1;
    for (var i = 0; i < favoriteCommands.length; i++) {
      if (favoriteCommands[i].text === text) { idx = i; break; }
    }
    if (idx >= 0) {
      favoriteCommands.splice(idx, 1);
      persistFavorites();
      renderFavoritesBar();
      if (typeof global.showToast === 'function') global.showToast(cat('removeFavorite'), 'success');
      return false;
    }
    favoriteCommands.unshift({ text: text, label: label.length > 36 ? label.slice(0, 34) + '\u2026' : label });
    if (favoriteCommands.length > MAX_FAVORITES) favoriteCommands = favoriteCommands.slice(0, MAX_FAVORITES);
    persistFavorites();
    renderFavoritesBar();
    if (typeof global.showToast === 'function') global.showToast(cat('addFavorite'), 'success');
    return true;
  }

  function shortCommandLabel(text) {
    text = String(text || '').trim().replace(/\s+/g, ' ');
    if (text.length <= 36) return text;
    return text.slice(0, 34) + '\u2026';
  }

  function loadRecentCommands() {
    try {
      var raw = localStorage.getItem(RECENT_CMDS_KEY);
      var arr = raw ? JSON.parse(raw) : [];
      if (!Array.isArray(arr)) {
        recentCommands = [];
        return;
      }
      recentCommands = arr.map(function(it) {
        if (typeof it === 'string') return { text: it, label: shortCommandLabel(it), at: 0 };
        return {
          text: String((it && it.text) || '').trim(),
          label: String((it && it.label) || '').trim() || shortCommandLabel((it && it.text) || ''),
          at: Number((it && it.at) || 0)
        };
      }).filter(function(it) { return !!it.text; }).slice(0, MAX_RECENT_CMDS);
    } catch (_) {
      recentCommands = [];
    }
  }

  function persistRecentCommands() {
    try {
      localStorage.setItem(RECENT_CMDS_KEY, JSON.stringify(recentCommands.slice(0, MAX_RECENT_CMDS)));
    } catch (_) {}
  }

  function rememberRecentCommand(text) {
    text = String(text || '').trim();
    if (!text || text.length > 240) return;
    // Skip pure follow-up fragments that look like field fills only.
    if (/^(email|role|provider_id|service groups)\b/i.test(text) && text.length < 48) return;
    recentCommands = recentCommands.filter(function(c) { return c.text !== text; });
    recentCommands.unshift({ text: text, label: shortCommandLabel(text), at: Date.now() });
    if (recentCommands.length > MAX_RECENT_CMDS) recentCommands = recentCommands.slice(0, MAX_RECENT_CMDS);
    persistRecentCommands();
    renderFavoritesBar();
  }

  function ensureFavoritesBar() {
    var examples = byID('configAgentExamples');
    if (!examples || !examples.parentNode) return null;
    var bar = byID('configAgentFavorites');
    if (!bar) {
      bar = document.createElement('div');
      bar.id = 'configAgentFavorites';
      bar.style.cssText = 'margin-top:8px;display:none;flex-direction:column;gap:6px';
      examples.parentNode.insertBefore(bar, examples);
    }
    return bar;
  }

  function renderCommandChipRow(items, opts) {
    opts = opts || {};
    var runAttr = opts.runAttr || 'data-ca-cmd-run';
    var delAttr = opts.delAttr || 'data-ca-cmd-del';
    var starAttr = opts.starAttr || '';
    var btnClass = opts.btnClass || 'btn-ghost';
    return items.map(function(f, i) {
      var starBtn = starAttr
        ? '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 6px" ' + starAttr + '="'
          + i + '" title="' + esc(cat('addFavorite')) + '">' + esc(cat('starRecent')) + '</button>'
        : '';
      return '<span style="display:inline-flex;align-items:center;gap:2px">'
        + '<button type="button" class="' + btnClass + '" style="height:28px;font-size:11px;padding:0 10px" ' + runAttr + '="'
        + i + '" title="' + esc(f.text) + '">' + esc(f.label || f.text) + '</button>'
        + starBtn
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 6px;color:#b42318" ' + delAttr + '="'
        + i + '" title="remove">\u00d7</button>'
        + '</span>';
    }).join('');
  }

  function runCommandText(text) {
    text = String(text || '').trim();
    if (!text) return;
    var input = byID('configAgentInput');
    if (input) input.value = text;
    var em = extractEmailFromText(text);
    if (em) rememberEmail(em);
    submitConfigAgent();
  }

  function exportCommandPrefs() {
    var pack = {
      kind: 'config-agent-command-prefs',
      version: 1,
      exported_at: new Date().toISOString(),
      favorites: favoriteCommands.slice(),
      recent: recentCommands.slice()
    };
    var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
    downloadTextFile('config-agent-prefs_' + stamp + '.json',
      JSON.stringify(pack, null, 2), 'application/json;charset=utf-8');
    if (typeof global.showToast === 'function') global.showToast(cat('prefsExported'), 'success');
  }

  function importCommandPrefsFromObject(obj) {
    if (!obj || typeof obj !== 'object') return false;
    var favs = obj.favorites;
    var recents = obj.recent || obj.recent_commands;
    var changed = false;
    if (Array.isArray(favs)) {
      favoriteCommands = favs.map(function(it) {
        if (typeof it === 'string') return { text: it, label: shortCommandLabel(it) };
        return {
          text: String((it && it.text) || '').trim(),
          label: String((it && it.label) || '').trim() || shortCommandLabel((it && it.text) || '')
        };
      }).filter(function(it) { return !!it.text; }).slice(0, MAX_FAVORITES);
      persistFavorites();
      changed = true;
    }
    if (Array.isArray(recents)) {
      recentCommands = recents.map(function(it) {
        if (typeof it === 'string') return { text: it, label: shortCommandLabel(it), at: Date.now() };
        return {
          text: String((it && it.text) || '').trim(),
          label: String((it && it.label) || '').trim() || shortCommandLabel((it && it.text) || ''),
          at: Number((it && it.at) || Date.now())
        };
      }).filter(function(it) { return !!it.text; }).slice(0, MAX_RECENT_CMDS);
      persistRecentCommands();
      changed = true;
    }
    if (changed) renderFavoritesBar();
    return changed;
  }

  function importCommandPrefsFromFile(file) {
    if (!file) return;
    var reader = new FileReader();
    reader.onload = function() {
      try {
        var obj = JSON.parse(String(reader.result || '{}'));
        if (importCommandPrefsFromObject(obj)) {
          if (typeof global.showToast === 'function') global.showToast(cat('prefsImported'), 'success');
        }
      } catch (err) {
        if (typeof global.showToast === 'function') {
          global.showToast(String(err && err.message || err), 'error');
        }
      }
    };
    reader.readAsText(file);
  }

  function renderFavoritesBar() {
    var bar = ensureFavoritesBar();
    if (!bar) return;
    bar.style.display = 'flex';
    var html = '';
    if (favoriteCommands.length) {
      html += '<div style="display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
        + '<span class="item-meta" style="font-weight:650">' + esc(cat('favorites')) + ':</span>'
        + renderCommandChipRow(favoriteCommands, {
          runAttr: 'data-ca-fav-run',
          delAttr: 'data-ca-fav-del',
          btnClass: 'btn-secondary'
        })
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentClearFavsBtn">'
        + esc(cat('clearFavorites')) + '</button>'
        + '</div>';
    }
    if (recentCommands.length) {
      html += '<div style="display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
        + '<span class="item-meta" style="font-weight:650">' + esc(cat('recentCmds')) + ':</span>'
        + renderCommandChipRow(recentCommands, {
          runAttr: 'data-ca-recent-run',
          delAttr: 'data-ca-recent-del',
          starAttr: 'data-ca-recent-star',
          btnClass: 'btn-ghost'
        })
        + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 8px" id="configAgentClearRecentCmdsBtn">'
        + esc(cat('clearRecent')) + '</button>'
        + '</div>';
    }
    // Always show import/export for prefs portability.
    html += '<div style="display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
      + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentExportPrefsBtn">'
      + esc(cat('exportPrefs')) + '</button>'
      + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentImportPrefsBtn">'
      + esc(cat('importPrefs')) + '</button>'
      + '<input type="file" id="configAgentImportPrefsFile" accept="application/json,.json" style="display:none">'
      + '</div>';
    bar.innerHTML = html;

    bar.querySelectorAll('[data-ca-fav-run]').forEach(function(btn) {
      btn.onclick = function() {
        var f = favoriteCommands[Number(btn.getAttribute('data-ca-fav-run'))];
        if (f) runCommandText(f.text);
      };
    });
    bar.querySelectorAll('[data-ca-fav-del]').forEach(function(btn) {
      btn.onclick = function() {
        var i = Number(btn.getAttribute('data-ca-fav-del'));
        if (i >= 0 && i < favoriteCommands.length) {
          favoriteCommands.splice(i, 1);
          persistFavorites();
          renderFavoritesBar();
        }
      };
    });
    var clearBtn = byID('configAgentClearFavsBtn');
    if (clearBtn) {
      clearBtn.onclick = function() {
        favoriteCommands = [];
        persistFavorites();
        renderFavoritesBar();
      };
    }
    bar.querySelectorAll('[data-ca-recent-run]').forEach(function(btn) {
      btn.onclick = function() {
        var f = recentCommands[Number(btn.getAttribute('data-ca-recent-run'))];
        if (f) runCommandText(f.text);
      };
    });
    bar.querySelectorAll('[data-ca-recent-del]').forEach(function(btn) {
      btn.onclick = function() {
        var i = Number(btn.getAttribute('data-ca-recent-del'));
        if (i >= 0 && i < recentCommands.length) {
          recentCommands.splice(i, 1);
          persistRecentCommands();
          renderFavoritesBar();
        }
      };
    });
    bar.querySelectorAll('[data-ca-recent-star]').forEach(function(btn) {
      btn.onclick = function() {
        var f = recentCommands[Number(btn.getAttribute('data-ca-recent-star'))];
        if (f) toggleFavoriteCommand(f.text, f.label || f.text);
      };
    });
    var clearRecentBtn = byID('configAgentClearRecentCmdsBtn');
    if (clearRecentBtn) {
      clearRecentBtn.onclick = function() {
        recentCommands = [];
        persistRecentCommands();
        renderFavoritesBar();
      };
    }
    var expBtn = byID('configAgentExportPrefsBtn');
    if (expBtn) expBtn.onclick = function() { exportCommandPrefs(); };
    var impBtn = byID('configAgentImportPrefsBtn');
    var impFile = byID('configAgentImportPrefsFile');
    if (impBtn && impFile) {
      impBtn.onclick = function() { impFile.click(); };
      impFile.onchange = function() {
        var f = impFile.files && impFile.files[0];
        if (f) importCommandPrefsFromFile(f);
        impFile.value = '';
      };
    }
  }

  function persistGroupBySession() {
    try {
      localStorage.setItem(GROUP_BY_SESSION_KEY, historyGroupBySession ? '1' : '0');
    } catch (_) {}
  }

  function persistCatalogCollapsed() {
    try {
      localStorage.setItem(CATALOG_COLLAPSED_KEY, JSON.stringify(catalogCollapsedDomains || {}));
    } catch (_) {}
  }

  function historyStatusColor(status) {
    var s = String(status || '');
    if (s === 'executed' || s === 'done') return '#027a48';
    if (s === 'failed') return '#b42318';
    if (s === 'needs_input') return '#b54708';
    return '#667085';
  }

  function historyItemRowHtml(h, idx) {
    var stColor = historyStatusColor(h.status);
    var emailBit = h.email ? ' \u00b7 ' + esc(h.email) : '';
    var turnBit = h.turn_count > 1 ? ' \u00b7 ' + h.turn_count + ' turns' : '';
    return '<div class="item-meta mono" style="margin-top:4px;cursor:pointer;text-decoration:underline" data-ca-history-idx="' + idx + '">'
      + esc((h.at || '').replace('T', ' ').slice(0, 19))
      + ' | <span style="color:' + stColor + '">' + esc(h.status) + '</span>'
      + ' | ' + esc(h.intent)
      + emailBit
      + turnBit
      + (h.summary ? ' - ' + esc(h.summary) : '')
      + '</div>';
  }

  function groupHistoryBySession(items) {
    var groups = {};
    var order = [];
    (items || []).forEach(function(h, idx) {
      var sid = String(h.session_id || '').trim();
      var key = sid || ('_solo_' + idx);
      if (!groups[key]) {
        groups[key] = { session_id: sid, items: [], indices: [] };
        order.push(key);
      }
      groups[key].items.push(h);
      groups[key].indices.push(idx);
    });
    return order.map(function(k) { return groups[k]; });
  }

  function renderPlanHistory() {
    var el = byID('configAgentHistory');
    if (!el) return;
    if (!planHistory.length) {
      el.innerHTML = historyToolbarHtml()
        + '<div class="item-meta" style="margin-top:6px">' + esc(cat('noHistory')) + '</div>'
        + '<div class="item-meta" style="margin-top:6px;opacity:.7;font-size:11px;display:flex;flex-wrap:wrap;gap:8px;align-items:center">'
        + '<span>' + esc(cat('shortcuts')) + '</span>'
        + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" id="configAgentShortcutsHelpBtn">'
        + esc(cat('shortcutsHelp')) + '</button></div>';
      bindHistoryToolbar();
      var helpBtn0 = byID('configAgentShortcutsHelpBtn');
      if (helpBtn0) helpBtn0.onclick = function() { showShortcutsHelp(); };
      return;
    }
    var bodyHtml = '';
    if (historyGroupBySession) {
      var groups = groupHistoryBySession(planHistory);
      bodyHtml = groups.map(function(g) {
        var title = g.session_id
          ? (cat('sessionGroup') + ' ' + g.session_id.slice(0, 18) + (g.session_id.length > 18 ? '\u2026' : ''))
          : cat('ungrouped');
        var rows = g.items.map(function(h, j) {
          return historyItemRowHtml(h, g.indices[j]);
        }).join('');
        return '<div style="margin-top:8px;padding:8px;border-radius:8px;border:1px solid rgba(20,24,36,.08);background:rgba(20,24,36,.02)">'
          + '<div class="item-meta" style="font-weight:650">' + esc(title)
          + ' <span style="opacity:.65;font-weight:500">(' + g.items.length + ')</span></div>'
          + rows + '</div>';
      }).join('');
    } else {
      bodyHtml = planHistory.map(function(h, idx) {
        return historyItemRowHtml(h, idx);
      }).join('');
    }
    el.innerHTML = historyToolbarHtml() + bodyHtml
      + '<div class="item-meta" style="margin-top:8px;opacity:.7;font-size:11px;display:flex;flex-wrap:wrap;gap:8px;align-items:center">'
      + '<span>' + esc(cat('shortcuts')) + '</span>'
      + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" id="configAgentShortcutsHelpBtn">'
      + esc(cat('shortcutsHelp')) + '</button>'
      + '</div>';
    bindHistoryToolbar();
    el.querySelectorAll('[data-ca-history-idx]').forEach(function(node) {
      node.onclick = function() {
        var i = Number(node.getAttribute('data-ca-history-idx') || -1);
        if (i >= 0 && planHistory[i]) openHistoryDetail(planHistory[i]);
      };
    });
    var helpBtn = byID('configAgentShortcutsHelpBtn');
    if (helpBtn) helpBtn.onclick = function() { showShortcutsHelp(); };
  }

  function historyToolbarHtml() {
    var allSel = historyActionFilter === '' ? ' selected' : '';
    var planSel = historyActionFilter === 'plan' ? ' selected' : '';
    var execSel = historyActionFilter === 'execute' ? ' selected' : '';
    var quick = HISTORY_QUICK_FILTERS.map(function(f) {
      var active = historyIntentFilter === f.q;
      return '<button type="button" class="' + (active ? 'btn-secondary' : 'btn-ghost') + '" '
        + 'style="height:26px;font-size:11px;padding:0 8px" data-ca-hist-quick="' + esc(f.q) + '">'
        + esc(f.label) + '</button>';
    }).join('');
    return '<div class="item-head" style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin-bottom:6px;flex-wrap:wrap">'
      + '<div class="item-title">' + esc(cat('recentPlans')) + '</div>'
      + '<div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">'
      + '<select id="configAgentHistoryActionFilter" style="height:28px;font-size:11px;padding:0 6px">'
      + '<option value=""' + allSel + '>all</option>'
      + '<option value="plan"' + planSel + '>plan</option>'
      + '<option value="execute"' + execSel + '>execute</option>'
      + '</select>'
      + '<input id="configAgentHistoryIntentFilter" type="search" value="' + esc(historyIntentFilter) + '" placeholder="intent / email / q" style="height:28px;font-size:11px;padding:0 8px;width:140px">'
      + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" id="configAgentCatalogBtn">Tools</button>'
      + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" id="configAgentHistoryReloadBtn">Reload</button>'
      + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" id="configAgentHistoryExportBtn">Export</button>'
      + '<button type="button" class="' + (historyGroupBySession ? 'btn-secondary' : 'btn-ghost') + '" style="height:28px;font-size:11px;padding:0 10px" id="configAgentHistGroupBtn">'
      + esc(cat('groupBySession')) + '</button>'
      + '<button type="button" class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" id="configAgentClearChatBtn">' + esc(cat('clearChat')) + '</button>'
      + '</div></div>'
      + '<div style="display:flex;flex-wrap:wrap;gap:6px;align-items:center;margin-bottom:6px">'
      + '<span class="item-meta">' + esc(cat('quickFilters')) + ':</span>'
      + quick
      + (historyIntentFilter
        ? '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" id="configAgentHistQuickClear">\u00d7</button>'
        : '')
      + '</div>';
  }

  function bindHistoryToolbar() {
    var filter = byID('configAgentHistoryActionFilter');
    if (filter) {
      filter.onchange = function() {
        historyActionFilter = String(filter.value || '');
        loadPlanHistoryFromServer();
      };
    }
    var intentEl = byID('configAgentHistoryIntentFilter');
    if (intentEl) {
      intentEl.onkeydown = function(e) {
        if (e.key === 'Enter') {
          e.preventDefault();
          historyIntentFilter = String(intentEl.value || '').trim();
          loadPlanHistoryFromServer();
        }
      };
      intentEl.onchange = function() {
        historyIntentFilter = String(intentEl.value || '').trim();
      };
    }
    var btn = byID('configAgentHistoryReloadBtn');
    if (btn) btn.onclick = function() {
      if (intentEl) historyIntentFilter = String(intentEl.value || '').trim();
      loadPlanHistoryFromServer();
    };
    var catBtn = byID('configAgentCatalogBtn');
    if (catBtn) catBtn.onclick = function() { showToolCatalog(); };
    var expBtn = byID('configAgentHistoryExportBtn');
    if (expBtn) expBtn.onclick = function() {
      if (intentEl) historyIntentFilter = String(intentEl.value || '').trim();
      exportPlanHistory();
    };
    var clearChatBtn = byID('configAgentClearChatBtn');
    if (clearChatBtn) clearChatBtn.onclick = function() { clearConfigAgentChat(); };
    var groupBtn = byID('configAgentHistGroupBtn');
    if (groupBtn) {
      groupBtn.onclick = function() {
        historyGroupBySession = !historyGroupBySession;
        persistGroupBySession();
        renderPlanHistory();
      };
    }
    var histEl = byID('configAgentHistory');
    if (histEl) {
      histEl.querySelectorAll('[data-ca-hist-quick]').forEach(function(btn) {
        btn.onclick = function() {
          historyIntentFilter = String(btn.getAttribute('data-ca-hist-quick') || '');
          loadPlanHistoryFromServer();
        };
      });
    }
    var clearQuick = byID('configAgentHistQuickClear');
    if (clearQuick) {
      clearQuick.onclick = function() {
        historyIntentFilter = '';
        loadPlanHistoryFromServer();
      };
    }
  }

  function clearConfigAgentChat() {
    var log = byID('configAgentChatLog');
    if (!log) return;
    // Keep history toolbar host; rebuild empty chat + examples + history.
    var historyHost = byID('configAgentHistory');
    var examples = byID('configAgentExamples');
    var empty = byID('configAgentEmptyHint');
    // Remove only chat message rows (class item that are not structural).
    Array.prototype.slice.call(log.children).forEach(function(child) {
      if (child === historyHost || child === examples || child === empty) return;
      if (child.classList && child.classList.contains('item')) child.remove();
    });
    if (empty) empty.style.display = '';
    clearConfigAgentSession({ silent: true, skipBar: true });
    pendingPlan = null;
    renderPlanHistory();
    updateSelectedBar();
  }

  function bindHistoryPlanActions(row, plan) {
    if (!row || !plan) return;
    bindGroupPickers(row);
    var safePlan = function() {
      var safe = Object.assign({}, plan);
      if (safe.confirm_token) safe.confirm_token = '';
      return safe;
    };
    var copyBtn = row.querySelector('[data-ca-copy-plan]');
    if (copyBtn) {
      copyBtn.onclick = function() {
        copyText(JSON.stringify(safePlan(), null, 2), copyBtn);
      };
    }
    var dlBtn = row.querySelector('[data-ca-download-plan]');
    if (dlBtn) {
      dlBtn.onclick = function() {
        var safe = safePlan();
        var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
        var intent = String(safe.intent || 'plan').replace(/[^A-Za-z0-9._\-]+/g, '_');
        downloadTextFile('config_agent_plan_' + intent + '_' + stamp + '.json',
          JSON.stringify(safe, null, 2), 'application/json;charset=utf-8');
        if (typeof global.showToast === 'function') global.showToast(cat('downloadPlan'), 'success');
      };
    }
  }

  async function openHistoryDetail(h) {
    if (!h) return;
    if (h.audit_id) {
      try {
        var data = await global.api('/api/admin/config-agent/history?id=' + encodeURIComponent(h.audit_id));
        var item = data && data.item;
        if (item) {
          var payload = item.payload || {};
          var plan = payload.plan || null;
          var action = String(item.action || '');
          var body = '';
          var execPayload = null;
          var rerunMsg = reconstructRerunMessage(payload, plan, h);
          // Session turns from plan audit (multi-turn fill history).
          var histTurns = payload.session_turns;
          if (Array.isArray(histTurns) && histTurns.length) {
            body += renderSessionTimeline(histTurns.map(String));
          }
          if (payload.session_id) {
            body += '<div class="item-meta mono" style="margin-top:4px">' + esc(cat('sessionActive'))
              + ': ' + esc(String(payload.session_id)) + '</div>';
          }
          // Execute history: reuse smart result cards (diagnose/export/list/bind).
          // replay:true avoids clobbering live diagnose status / recent chips.
          if (action.indexOf('execute') >= 0 && Array.isArray(payload.results)) {
            execPayload = renderExecuteResult({
              ok: payload.ok !== false,
              intent: payload.intent || h.intent || '',
              plan_id: payload.plan_id || h.plan_id || '',
              results: payload.results,
              error: payload.error
            }, { replay: true });
            body += execPayload.html;
          } else if (plan) {
            body += renderPlanCard(Object.assign({}, plan, {
              confirm_token: '',
              plan_id: plan.plan_id || payload.plan_id || ''
            }));
          }
          var actionBtns = '';
          if (rerunMsg) {
            actionBtns += '<button type="button" class="btn-secondary" data-ca-replan="1">' + esc(cat('replan')) + '</button>';
            if (isRerunnableRead(payload, plan) || action.indexOf('execute') < 0) {
              actionBtns += '<button type="button" class="btn-primary" data-ca-rerun="1">' + esc(cat('rerun')) + '</button>';
            }
          }
          if (Array.isArray(histTurns) && histTurns.length) {
            actionBtns += '<button type="button" class="btn-ghost" data-ca-copy-turns="1">' + esc(cat('copy')) + ' turns</button>';
          }
          if (actionBtns) {
            body += '<div class="actions" style="margin-top:10px;gap:8px;flex-wrap:wrap">' + actionBtns + '</div>';
          }
          body += '<details style="margin-top:8px"><summary class="item-meta">' + esc(cat('raw')) + '</summary>'
            + '<pre class="mono" style="margin-top:6px;white-space:pre-wrap;font-size:11px;max-height:220px;overflow:auto">'
            + esc(JSON.stringify(payload, null, 2)) + '</pre></details>';
          appendChat('assistant',
            '<div class="item-title">' + esc(cat('historyDetail')) + '</div>'
            + '<div class="item-meta mono" style="margin-top:4px">' + esc(action) + ' @ ' + esc(item.created_at || '') + '</div>'
            + body);
          var log = byID('configAgentChatLog');
          var row = latestChatRow(log);
          if (execPayload) bindExecuteResultActions(row, execPayload);
          if (plan && row) {
            bindHistoryPlanActions(row, plan);
          }
          if (rerunMsg && row) {
            var btn = row.querySelector('[data-ca-replan]');
            if (btn) {
              btn.onclick = function() {
                var input = byID('configAgentInput');
                if (input) input.value = rerunMsg;
                submitConfigAgent();
              };
            }
            var rerun = row.querySelector('[data-ca-rerun]');
            if (rerun) {
              rerun.onclick = function() {
                var input = byID('configAgentInput');
                if (input) input.value = rerunMsg;
                // Force a fresh run (read plans auto-execute).
                submitConfigAgent();
              };
            }
          }
          if (row && Array.isArray(histTurns) && histTurns.length) {
            var copyTurns = row.querySelector('[data-ca-copy-turns]');
            if (copyTurns) {
              copyTurns.onclick = function() {
                copyText(histTurns.map(String).join('\n'), copyTurns);
              };
            }
          }
          return;
        }
      } catch (err) {
        appendChat('assistant', '<div class="item-meta" style="color:#b42318">' + esc(String(err && err.message || err)) + '</div>');
        return;
      }
    }
    appendChat('assistant',
      '<div class="item-title">' + esc(cat('historyDetail')) + '</div>'
      + '<div class="item-meta mono" style="margin-top:4px">' + esc(JSON.stringify(h, null, 2)) + '</div>');
  }

  function toolDomain(name) {
    var n = String(name || '');
    var i = n.indexOf('.');
    return i > 0 ? n.slice(0, i) : (n || 'other');
  }

  function renderCatalogToolRow(t) {
    var tname = String(t.name || '');
    var domain = String(t.domain || toolDomain(tname));
    var ex = String(t.example || toolNameToExample(tname));
    var fav = isFavoriteCommand(ex);
    return '<div class="item-meta ca-catalog-tool-row" style="margin-top:3px;display:flex;flex-wrap:wrap;gap:6px;align-items:center" data-ca-tool-name="'
      + esc(tname.toLowerCase()) + '" data-ca-tool-mode="' + esc(String(t.mode || '').toLowerCase()) + '">'
      + '<span class="mono">' + esc(tname) + '</span>'
      + '<span class="item-meta" style="opacity:.65;font-size:10px">[' + esc(t.mode || '') + ' \u00b7 ' + esc(domain) + ']</span>'
      + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-catalog-tool="'
      + esc(ex) + '">' + esc(cat('useTool')) + '</button>'
      + '<button type="button" class="' + (fav ? 'btn-secondary' : 'btn-ghost') + '" style="height:24px;font-size:11px;padding:0 8px" '
      + 'data-ca-catalog-fav="' + esc(ex) + '" data-ca-catalog-fav-label="' + esc(tname) + '" title="'
      + esc(fav ? cat('removeFavorite') : cat('addFavorite')) + '">'
      + (fav ? '\u2605' : '\u2606') + '</button>'
      + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-copy-group="'
      + esc(tname) + '">' + esc(cat('copyId')) + '</button>'
      + '</div>';
  }

  function filterCatalogTools(tools, q) {
    q = String(q || '').trim().toLowerCase();
    if (!q) return tools || [];
    return (tools || []).filter(function(t) {
      var name = String(t.name || '').toLowerCase();
      var mode = String(t.mode || '').toLowerCase();
      var domain = String(t.domain || toolDomain(t.name)).toLowerCase();
      var ex = String(t.example || toolNameToExample(t.name)).toLowerCase();
      return name.indexOf(q) >= 0 || mode.indexOf(q) >= 0 || domain.indexOf(q) >= 0 || ex.indexOf(q) >= 0;
    });
  }

  function buildCatalogSectionsHtml(tools) {
    var byDomain = {};
    var domainOrder = [];
    tools.forEach(function(t) {
      var d = String(t.domain || toolDomain(t.name));
      if (!byDomain[d]) {
        byDomain[d] = [];
        domainOrder.push(d);
      }
      byDomain[d].push(t);
    });
    domainOrder.sort();
    if (!domainOrder.length) {
      return '<div class="item-meta" style="margin-top:8px;color:#b54708">' + esc(cat('noCatalogMatch')) + '</div>';
    }
    return domainOrder.map(function(domain) {
      var list = byDomain[domain] || [];
      if (!list.length) return '';
      var modeRank = { read: 0, probe: 1, write: 2 };
      list = list.slice().sort(function(a, b) {
        var ra = modeRank[a.mode] != null ? modeRank[a.mode] : 9;
        var rb = modeRank[b.mode] != null ? modeRank[b.mode] : 9;
        if (ra !== rb) return ra - rb;
        return String(a.name || '').localeCompare(String(b.name || ''));
      });
      // When filtering, force open matching domains.
      var forceOpen = !!String(catalogFilterQuery || '').trim();
      var open = (forceOpen || !catalogCollapsedDomains[domain]) ? ' open' : '';
      return '<details class="ca-catalog-domain" data-ca-domain="' + esc(domain) + '"' + open
        + ' style="margin-top:8px;border:1px solid rgba(20,24,36,.08);border-radius:8px;padding:6px 10px;background:#fff">'
        + '<summary class="item-meta" style="cursor:pointer;font-weight:650">'
        + esc(cat('domain')) + ': <span class="mono">' + esc(domain) + '</span>'
        + ' <span style="opacity:.65;font-weight:500">(' + list.length + ')</span>'
        + '</summary>'
        + list.map(renderCatalogToolRow).join('')
        + '</details>';
    }).join('');
  }

  function rebindCatalogSections(row) {
    if (!row) return;
    var host = row.querySelector('#configAgentCatalogSections') || row;
    host.querySelectorAll('[data-ca-catalog-tool]').forEach(function(node) {
      node.onclick = function() {
        var input = byID('configAgentInput');
        if (input) input.value = node.getAttribute('data-ca-catalog-tool') || '';
        submitConfigAgent();
      };
    });
    host.querySelectorAll('[data-ca-catalog-fav]').forEach(function(node) {
      node.onclick = function() {
        var text = node.getAttribute('data-ca-catalog-fav') || '';
        var label = node.getAttribute('data-ca-catalog-fav-label') || text;
        var nowFav = toggleFavoriteCommand(text, label);
        node.textContent = nowFav ? '\u2605' : '\u2606';
        node.className = nowFav ? 'btn-secondary' : 'btn-ghost';
        node.style.height = '24px';
        node.style.fontSize = '11px';
        node.style.padding = '0 8px';
        node.title = nowFav ? cat('removeFavorite') : cat('addFavorite');
      };
    });
    host.querySelectorAll('details.ca-catalog-domain').forEach(function(det) {
      det.ontoggle = function() {
        var d = det.getAttribute('data-ca-domain') || '';
        if (!d) return;
        if (det.open) delete catalogCollapsedDomains[d];
        else catalogCollapsedDomains[d] = true;
        persistCatalogCollapsed();
      };
    });
  }

  function bindCatalogRowActions(row) {
    if (!row) return;
    bindGroupPickers(row);
    row.querySelectorAll('[data-ca-catalog-ex]').forEach(function(node) {
      node.onclick = function() {
        var input = byID('configAgentInput');
        if (input) input.value = node.getAttribute('data-ca-catalog-ex') || '';
        submitConfigAgent();
      };
    });
    rebindCatalogSections(row);
    var expAll = row.querySelector('[data-ca-catalog-expand]');
    if (expAll) {
      expAll.onclick = function() {
        catalogCollapsedDomains = {};
        persistCatalogCollapsed();
        row.querySelectorAll('details.ca-catalog-domain').forEach(function(d) { d.open = true; });
      };
    }
    var colAll = row.querySelector('[data-ca-catalog-collapse]');
    if (colAll) {
      colAll.onclick = function() {
        row.querySelectorAll('details.ca-catalog-domain').forEach(function(d) {
          d.open = false;
          var dn = d.getAttribute('data-ca-domain');
          if (dn) catalogCollapsedDomains[dn] = true;
        });
        persistCatalogCollapsed();
      };
    }
    var searchEl = row.querySelector('#configAgentCatalogSearch');
    if (searchEl && !searchEl.__caBound) {
      searchEl.__caBound = true;
      searchEl.oninput = function() {
        catalogFilterQuery = String(searchEl.value || '');
        if (catalogSearchTimer) clearTimeout(catalogSearchTimer);
        catalogSearchTimer = setTimeout(function() {
          catalogSearchTimer = null;
          var host = row.querySelector('#configAgentCatalogSections');
          if (!host) return;
          var filtered = filterCatalogTools(lastCatalogTools, catalogFilterQuery);
          host.innerHTML = buildCatalogSectionsHtml(filtered);
          var title = row.querySelector('#configAgentCatalogTitle');
          if (title) {
            var domains = {};
            filtered.forEach(function(t) {
              domains[String(t.domain || toolDomain(t.name))] = true;
            });
            title.textContent = 'Tool catalog (' + filtered.length + '/' + lastCatalogTools.length
              + ' \u00b7 ' + Object.keys(domains).length + ' domains)';
          }
          rebindCatalogSections(row);
        }, 120);
      };
    }
  }

  async function showToolCatalog() {
    try {
      var data = await global.api('/api/admin/config-agent/catalog');
      var tools = (data && data.tools) || [];
      lastCatalogTools = tools;
      lastCatalogExampleByName = {};
      tools.forEach(function(t) {
        if (t && t.name && t.example) {
          lastCatalogExampleByName[String(t.name)] = String(t.example);
        }
      });
      lastCatalogExamples = (data && data.examples) || [];
      catalogFilterQuery = '';
      var filtered = filterCatalogTools(tools, catalogFilterQuery);
      var domainCount = {};
      filtered.forEach(function(t) {
        domainCount[String(t.domain || toolDomain(t.name))] = true;
      });
      var sections = buildCatalogSectionsHtml(filtered);
      var foldBar = '<div class="actions" style="margin-top:8px;gap:6px;flex-wrap:wrap;align-items:center">'
        + '<input id="configAgentCatalogSearch" type="search" value="" placeholder="' + esc(cat('catalogSearch')) + '" '
        + 'style="height:28px;font-size:12px;padding:0 8px;min-width:180px;border:1px solid rgba(20,24,36,.15);border-radius:6px">'
        + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" data-ca-catalog-expand="1">'
        + esc(cat('expandAll')) + '</button>'
        + '<button type="button" class="btn-ghost" style="height:26px;font-size:11px;padding:0 8px" data-ca-catalog-collapse="1">'
        + esc(cat('collapseAll')) + '</button>'
        + '</div>';
      var examples = lastCatalogExamples;
      var ex = examples.length
        ? '<div class="item-meta" style="margin-top:10px"><strong>examples</strong></div>'
          + examples.slice(0, 12).map(function(e) {
            return '<div class="item-meta mono" style="margin-top:2px;cursor:pointer;text-decoration:underline" data-ca-catalog-ex="'
              + esc(e) + '">- ' + esc(e) + '</div>';
          }).join('')
        : '';
      // Service group picker (live or cached)
      var groupSection = '';
      try {
        if (!lastServiceGroups.length) {
          var svc = await global.api('/api/admin/llm/services');
          var gs = (svc && svc.model_service_groups) || [];
          lastServiceGroups = gs.map(function(g) {
            return { id: g.id, name: g.name, system_free: !!g.system_free || String(g.id) === 'system-free' };
          });
        }
      } catch (_) { /* optional */ }
      if (lastServiceGroups.length) {
        groupSection = '<div class="item-meta" style="margin-top:10px"><strong>' + esc(cat('pickGroup')) + '</strong> ('
          + lastServiceGroups.length + ')</div>'
          + lastServiceGroups.map(function(g) {
            var gid = String(g.id || '');
            return '<div class="item-meta" style="margin-top:4px;display:flex;flex-wrap:wrap;gap:6px;align-items:center">'
              + '<span class="mono">' + esc(gid) + (g.system_free ? ' [system-free]' : '')
              + (g.name ? ' \u2014 ' + esc(String(g.name)) : '') + '</span>'
              + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-use-group="' + esc(gid) + '">' + esc(cat('useGroup')) + '</button>'
              + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-unbind-group="' + esc(gid) + '">' + esc(cat('unbind')) + '</button>'
              + '<button type="button" class="btn-ghost" style="height:24px;font-size:11px;padding:0 8px" data-ca-copy-group="' + esc(gid) + '">' + esc(cat('copyId')) + '</button>'
              + '</div>';
          }).join('');
      }
      appendChat('assistant',
        '<div class="item-title" id="configAgentCatalogTitle">Tool catalog (' + filtered.length + ' \u00b7 '
        + Object.keys(domainCount).length + ' domains)</div>'
        + foldBar
        + '<div id="configAgentCatalogSections">' + sections + '</div>'
        + groupSection + ex);
      var log = byID('configAgentChatLog');
      var row = latestChatRow(log);
      bindCatalogRowActions(row);
      var searchFocus = row && row.querySelector('#configAgentCatalogSearch');
      if (searchFocus) searchFocus.focus();
    } catch (err) {
      appendChat('assistant', '<div class="item-meta" style="color:#b42318">' + esc(String(err && err.message || err)) + '</div>');
    }
  }

  function ensureShortcutsHelpModal() {
    if (byID('configAgentShortcutsOverlay')) return;
    var overlay = document.createElement('div');
    overlay.id = 'configAgentShortcutsOverlay';
    overlay.className = 'hidden';
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(20,24,36,.45);z-index:10000;display:none;align-items:center;justify-content:center;padding:20px';
    overlay.innerHTML = ''
      + '<div class="item" role="dialog" aria-modal="true" aria-labelledby="configAgentShortcutsTitle" '
      + 'style="max-width:480px;width:100%;padding:20px;background:#fff;border-radius:12px;box-shadow:0 16px 48px rgba(20,24,36,.18)">'
      + '  <div class="item-title" id="configAgentShortcutsTitle"></div>'
      + '  <div id="configAgentShortcutsBody" style="margin-top:12px"></div>'
      + '  <div class="actions" style="margin-top:16px">'
      + '    <button class="btn-primary" type="button" id="configAgentShortcutsCloseBtn"></button>'
      + '  </div>'
      + '</div>';
    document.body.appendChild(overlay);
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) hideShortcutsHelp();
    });
    byID('configAgentShortcutsCloseBtn').onclick = function() { hideShortcutsHelp(); };
  }

  function hideShortcutsHelp() {
    var overlay = byID('configAgentShortcutsOverlay');
    if (overlay) {
      overlay.classList.add('hidden');
      overlay.style.display = 'none';
    }
  }

  function showShortcutsHelp() {
    ensureShortcutsHelpModal();
    var overlay = byID('configAgentShortcutsOverlay');
    if (!overlay) return;
    byID('configAgentShortcutsTitle').textContent = cat('shortcutsHelp');
    byID('configAgentShortcutsCloseBtn').textContent = cat('shortcutsClose');
    var rows = [
      ['Esc', cat('cancel')],
      ['Ctrl+Enter', cat('send') + ' / ' + cat('confirm')],
      ['Ctrl+Shift+T', 'Tools / catalog'],
      ['Ctrl+K', 'Focus input'],
      ['Ctrl+/', cat('shortcutsHelp')]
    ];
    byID('configAgentShortcutsBody').innerHTML =
      '<table style="width:100%;border-collapse:collapse;font-size:13px">'
      + rows.map(function(r) {
        return '<tr>'
          + '<td class="mono" style="padding:6px 8px;border-bottom:1px solid rgba(20,24,36,.06);white-space:nowrap;font-weight:650">'
          + esc(r[0]) + '</td>'
          + '<td style="padding:6px 8px;border-bottom:1px solid rgba(20,24,36,.06)">' + esc(r[1]) + '</td>'
          + '</tr>';
      }).join('')
      + '</table>';
    overlay.classList.remove('hidden');
    overlay.style.display = 'flex';
    byID('configAgentShortcutsCloseBtn').focus();
  }

  async function loadPlanHistoryFromServer() {
    try {
      var q = new URLSearchParams({ limit: '20' });
      if (historyActionFilter) q.set('action', historyActionFilter);
      if (historyIntentFilter) {
        // Prefer exact-ish intent filter; also pass q for broader search.
        if (historyIntentFilter.indexOf('.') >= 0 || historyIntentFilter.indexOf('_') >= 0) {
          q.set('intent', historyIntentFilter);
        } else {
          q.set('q', historyIntentFilter);
        }
      }
      var data = await global.api('/api/admin/config-agent/history?' + q.toString());
      var items = (data && data.items) || [];
      planHistory = items.map(function(it) {
        var payload = it.payload || {};
        var action = String(it.action || '');
        var status = action.indexOf('execute') >= 0
          ? (payload.ok === false ? 'failed' : 'executed')
          : ((payload.missing_fields && payload.missing_fields.length) ? 'needs_input' : 'planned');
        var email = '';
        try {
          var src = String(payload.source_message || payload.summary || '');
          email = extractEmailFromText(src) || '';
          if (!email) {
            var diag = extractDiagnoseFromPayload(payload);
            if (diag && diag.email) email = String(diag.email);
          }
          if (!email && payload.plan && payload.plan.simulated && payload.plan.simulated.email) {
            email = String(payload.plan.simulated.email);
          }
        } catch (_) {}
        var turns = payload.session_turns;
        var turnCount = Array.isArray(turns) ? turns.length : 0;
        return {
          at: it.created_at || '',
          intent: payload.intent || '',
          summary: payload.summary || payload.error || '',
          planner: payload.planner || '',
          status: status,
          plan_id: payload.plan_id || '',
          audit_id: it.id || '',
          email: email,
          turn_count: turnCount,
          session_id: payload.session_id || ''
        };
      });
      renderPlanHistory();
    } catch (err) {
      // keep local history
      renderPlanHistory();
    }
  }

  async function exportPlanHistory() {
    try {
      var q = new URLSearchParams({ limit: '100', export: '1' });
      if (historyActionFilter) q.set('action', historyActionFilter);
      if (historyIntentFilter) {
        if (historyIntentFilter.indexOf('.') >= 0 || historyIntentFilter.indexOf('_') >= 0) {
          q.set('intent', historyIntentFilter);
        } else {
          q.set('q', historyIntentFilter);
        }
      }
      var data = await global.api('/api/admin/config-agent/history?' + q.toString());
      // Derive support packs from diagnose execute rows for easier handoff.
      var supportPacks = [];
      ((data && data.items) || []).forEach(function(it) {
        var payload = (it && it.payload) || {};
        var diag = extractDiagnoseFromPayload(payload);
        if (!diag) return;
        var pack = supportPackFromDiagnoseResult(diag);
        if (!pack) return;
        pack.history_id = it.id || '';
        pack.history_at = it.created_at || '';
        supportPacks.push(pack);
      });
      var out = Object.assign({}, data || {}, {
        kind: 'config-agent-history-export',
        version: 1,
        exported_at: new Date().toISOString(),
        support_packs: supportPacks,
        support_pack_count: supportPacks.length
      });
      var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
      downloadTextFile('config-agent-history_' + stamp + '.json',
        JSON.stringify(out, null, 2), 'application/json;charset=utf-8');
      var toast = cat('historyExported');
      if (supportPacks.length) toast += ' \u00b7 ' + supportPacks.length + ' ' + cat('supportPacksInExport');
      if (typeof global.showToast === 'function') global.showToast(toast, 'success');
    } catch (err) {
      appendChat('assistant', '<div class="item-meta" style="color:#b42318">' + esc(String(err && err.message || err)) + '</div>');
    }
  }

  async function submitConfigAgent() {
    var input = byID('configAgentInput');
    var text = String(input && input.value || '').trim();
    if (!text) return;
    // Block concurrent plan/execute (avoids pendingPlan races).
    if (configAgentIsBusy()) {
      if (typeof global.showToast === 'function') global.showToast(cat('busy'), 'error');
      return;
    }
    submitConfigAgent._busy = true;
    setConfigAgentComposerBusy(true);
    var em = extractEmailFromText(text);
    if (em) rememberEmail(em);
    if (input) input.value = '';
    updateSelectedBar();
    appendChat('user', '<div class="config-agent-message-role">' + esc(cat('you')) + '</div><div class="config-agent-message-body">' + esc(text) + '</div>');
    appendChat('assistant', '<div class="item-meta" data-ca-planning="1">' + esc(cat('planning')) + '</div>');
    try {
      var data = await global.api('/api/admin/config-agent/plan', {
        method: 'POST',
        body: JSON.stringify({ message: text, session_id: configAgentSessionID || undefined })
      });
      // Remove planning bubble (prefer marked node).
      var log = byID('configAgentChatLog');
      var latestPlanRow = latestChatRow(log);
      if (!removeMarkedChatBubble(log, '[data-ca-planning]') && latestPlanRow) {
        try { log.removeChild(latestPlanRow); } catch (_) {}
      }
      if (data) setSessionFromPlanResponse(data);
      if (!data || !data.ok || !data.plan) {
        var examples = (data && data.examples || []).map(function(e) {
          return '<div class="item-meta mono" style="margin-top:4px">- ' + esc(e) + '</div>';
        }).join('');
        appendChat('assistant', '<div class="item-meta">' + esc((data && data.message) || cat('failed')) + '</div>' + examples);
        pendingPlan = null;
        return;
      }
      pendingPlan = data.plan;
      if (data.planner && pendingPlan && !pendingPlan.planner) {
        pendingPlan.planner = data.planner;
      }
      // Track successful user NL commands for quick re-run.
      rememberRecentCommand(text);
      // Remember email from plan args / simulated.
      try {
        var simEmail = pendingPlan.simulated && pendingPlan.simulated.email;
        if (simEmail) rememberEmail(simEmail);
        (pendingPlan.steps || []).forEach(function(s) {
          if (s && s.args && s.args.email) rememberEmail(s.args.email);
        });
      } catch (_) {}
      pushPlanHistory(pendingPlan, data.needs_input ? 'needs_input' : 'planned');

      function showPlanCard(withAutoHint, opts) {
        opts = opts || {};
        var html = renderPlanCard(pendingPlan);
        if (withAutoHint) {
          html += '<div class="item-meta" style="margin-top:8px;color:#027a48">' + esc(cat('autoRead')) + '</div>';
        }
        appendChat('assistant', html);
        bindPlanActions();
        // Skip confirm TTL timer when we will auto-execute immediately.
        if (!opts.skipExpiry) setPlanExpiryFromPlan(pendingPlan);
        else {
          stopPlanTTLTimer();
          planExpiresAtMs = 0;
        }
        updateSelectedBar();
      }

      function maybeLoadProvidersThenShow(withAutoHint, thenAutoExec) {
        var planSnap = pendingPlan;
        var needsProv = planSnap && (planSnap.missing_fields || []).indexOf('provider_id') >= 0;
        var done = function() {
          // User may have cancelled while providers were loading.
          if (!pendingPlan || pendingPlan !== planSnap) return;
          showPlanCard(withAutoHint, { skipExpiry: !!thenAutoExec });
          if (thenAutoExec) {
            // Defer until submitConfigAgent finally clears _busy (avoids busy deadlock).
            setTimeout(function() {
              if (!pendingPlan || pendingPlan !== planSnap) return;
              executePendingPlan(true);
            }, 0);
          }
        };
        if (needsProv && !lastProviders.length) {
          ensureProvidersCached().then(done).catch(done);
        } else {
          done();
        }
      }

      // Prefer authoritative missing_fields over needs_input flag alone.
      var needsInput = !!(data.needs_input || (pendingPlan.missing_fields || []).length);
      if (needsInput) {
        maybeLoadProvidersThenShow(false, false);
      } else if (planIsAutoExecutable(pendingPlan)) {
        maybeLoadProvidersThenShow(true, true);
      } else {
        maybeLoadProvidersThenShow(false, false);
      }
    } catch (err) {
      var log2 = byID('configAgentChatLog');
      var latestPlanErrorRow = latestChatRow(log2);
      if (!removeMarkedChatBubble(log2, '[data-ca-planning]') && latestPlanErrorRow) {
        try { log2.removeChild(latestPlanErrorRow); } catch (_) {}
      }
      appendChat('assistant', '<div class="item-meta" style="color:#b42318">' + esc(cat('failed') + ': ' + (err && err.message || err)) + '</div>');
      pendingPlan = null;
      stopPlanTTLTimer();
      planExpiresAtMs = 0;
    } finally {
      submitConfigAgent._busy = false;
      setConfigAgentComposerBusy(!!executePendingPlan._busy);
    }
  }

  function bindPlanActions() {
    var log = byID('configAgentChatLog');
    var row = latestChatRow(log);
    if (!row) return;
    // Snapshot the plan bound to this card so older cards cannot act on a newer pendingPlan.
    var boundPlan = pendingPlan ? Object.assign({}, pendingPlan) : null;
    var boundPlanId = boundPlan && boundPlan.plan_id ? String(boundPlan.plan_id) : '';
    var boundPlanJSON = '';
    try {
      if (boundPlan) {
        var safeBound = Object.assign({}, boundPlan);
        if (safeBound.confirm_token) safeBound.confirm_token = '';
        boundPlanJSON = JSON.stringify(safeBound, null, 2);
      }
    } catch (_) { boundPlanJSON = ''; }
    bindGroupPickers(row);
    var confirmBtn = row.querySelector('[data-ca-confirm]');
    var cancelBtn = row.querySelector('[data-ca-cancel]');
    var copyBtn = row.querySelector('[data-ca-copy-plan]');
    if (confirmBtn) {
      confirmBtn.onclick = function() {
        if (!pendingPlan || String(pendingPlan.plan_id || '') !== boundPlanId) {
          if (typeof global.showToast === 'function') global.showToast(cat('planSuperseded'), 'error');
          return;
        }
        if (configAgentIsBusy()) return;
        confirmBtn.disabled = true;
        confirmBtn.setAttribute('aria-busy', 'true');
        if (cancelBtn) cancelBtn.disabled = true;
        executePendingPlan(true);
      };
    }
    if (cancelBtn) {
      cancelBtn.onclick = function() {
        // Only cancel if this card still owns the active plan (or no plan left).
        if (pendingPlan && boundPlanId && String(pendingPlan.plan_id || '') !== boundPlanId) {
          if (typeof global.showToast === 'function') global.showToast(cat('planSuperseded'), 'error');
          return;
        }
        clearConfigAgentSession({ toast: cat('cancel') });
        appendChat('assistant', '<div class="item-meta">' + esc(cat('cancel')) + '</div>');
      };
    }
    if (copyBtn) {
      copyBtn.onclick = function() {
        var text = boundPlanJSON;
        if (!text && pendingPlan) {
          var safe = Object.assign({}, pendingPlan);
          if (safe.confirm_token) safe.confirm_token = '';
          text = JSON.stringify(safe, null, 2);
        }
        copyText(text || '{}', copyBtn);
      };
    }
    var dlPlanBtn = row.querySelector('[data-ca-download-plan]');
    if (dlPlanBtn) {
      dlPlanBtn.onclick = function() {
        var text = boundPlanJSON;
        var intent = 'plan';
        if (!text && pendingPlan) {
          var safe2 = Object.assign({}, pendingPlan);
          if (safe2.confirm_token) safe2.confirm_token = '';
          text = JSON.stringify(safe2, null, 2);
          intent = String(safe2.intent || 'plan');
        } else if (boundPlan) {
          intent = String(boundPlan.intent || 'plan');
        }
        var stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '');
        intent = intent.replace(/[^A-Za-z0-9._\-]+/g, '_');
        downloadTextFile('config_agent_plan_' + intent + '_' + stamp + '.json',
          text || '{}', 'application/json;charset=utf-8');
        if (typeof global.showToast === 'function') global.showToast(cat('downloadPlan'), 'success');
      };
    }
    // Multi-turn quick-fill interactions
    var fillEmailBtn = row.querySelector('[data-ca-fill-email]');
    if (fillEmailBtn) {
      fillEmailBtn.onclick = function() {
        var el = row.querySelector('#configAgentMissingEmail');
        if (el && lastRememberedEmail) el.value = lastRememberedEmail;
      };
    }
    row.querySelectorAll('[data-ca-fill-role]').forEach(function(btn) {
      btn.onclick = function() {
        row.querySelectorAll('[data-ca-fill-role]').forEach(function(b) {
          b.className = 'btn-ghost';
          b.style.height = '28px';
          b.style.fontSize = '11px';
          b.style.padding = '0 10px';
        });
        btn.className = 'btn-secondary';
        btn.style.height = '28px';
        btn.style.fontSize = '11px';
        btn.style.padding = '0 10px';
      };
    });
    row.querySelectorAll('[data-ca-fill-group]').forEach(function(btn) {
      btn.onclick = function() {
        // Multi-select toggle for service groups.
        if (btn.className.indexOf('btn-secondary') >= 0) {
          btn.className = 'btn-ghost mono';
        } else {
          btn.className = 'btn-secondary mono';
        }
        btn.style.height = '26px';
        btn.style.fontSize = '11px';
        btn.style.padding = '0 8px';
      };
    });
    row.querySelectorAll('[data-ca-fill-provider]').forEach(function(btn) {
      btn.onclick = function() {
        row.querySelectorAll('[data-ca-fill-provider]').forEach(function(b) {
          b.className = 'btn-ghost mono';
          b.style.height = '26px';
          b.style.fontSize = '11px';
          b.style.padding = '0 8px';
        });
        btn.className = 'btn-secondary mono';
        btn.style.height = '26px';
        btn.style.fontSize = '11px';
        btn.style.padding = '0 8px';
        var el = row.querySelector('#configAgentMissingProvider');
        if (el) el.value = btn.getAttribute('data-ca-fill-provider') || '';
      };
    });
    var continueBtn = row.querySelector('[data-ca-continue-fill]');
    if (continueBtn) {
      continueBtn.onclick = function() {
        var follow = composeFollowUpFromFill(row, pendingPlan);
        if (!follow) {
          if (typeof global.showToast === 'function') global.showToast(cat('fillMissing'), 'error');
          return;
        }
        var em = extractEmailFromText(follow);
        if (em) rememberEmail(em, { silent: true });
        var input = byID('configAgentInput');
        if (input) input.value = follow;
        submitConfigAgent();
      };
    }
    var newSessPlan = row.querySelector('[data-ca-new-session]');
    if (newSessPlan) {
      newSessPlan.onclick = function() {
        clearConfigAgentSession({ toast: cat('newSession') });
        appendChat('assistant', '<div class="item-meta">' + esc(cat('newSession')) + '</div>');
      };
    }
  }

  async function executePendingPlan(runOptional) {
    if (!pendingPlan || !pendingPlan.plan_id || !pendingPlan.confirm_token) {
      appendChat('assistant', '<div class="item-meta">' + esc(cat('noPlan')) + '</div>');
      return;
    }
    if (planExpiresAtMs && planExpiresAtMs <= Date.now()) {
      pendingPlan = null;
      stopPlanTTLTimer();
      planExpiresAtMs = 0;
      if (typeof global.showToast === 'function') global.showToast(cat('planExpired'), 'error');
      updateSelectedBar();
      return;
    }
    // Guard double-click; allow only one execute at a time.
    // Note: submitConfigAgent may still be unwinding; auto-exec defers with setTimeout(0).
    if (executePendingPlan._busy) return;
    if (submitConfigAgent._busy) {
      if (typeof global.showToast === 'function') global.showToast(cat('busy'), 'error');
      return;
    }
    executePendingPlan._busy = true;
    setConfigAgentComposerBusy(true);
    // Snapshot plan so a concurrent successful plan response cannot be wiped on finish.
    var planSnap = pendingPlan;
    var planId = String(planSnap.plan_id || '');
    var confirmToken = String(planSnap.confirm_token || '');
    var plannedIntent = planSnap.intent || '';
    var sessionSnap = configAgentSessionID || undefined;
    appendChat('assistant', '<div class="item-meta" data-ca-executing="1">' + esc(cat('executing')) + '</div>');
    try {
      var data = await global.api('/api/admin/config-agent/execute', {
        method: 'POST',
        body: JSON.stringify({
          plan_id: planId,
          confirm_token: confirmToken,
          run_optional: !!runOptional,
          session_id: sessionSnap
        })
      });
      // Only clear pending if it is still the plan we executed.
      if (pendingPlan && String(pendingPlan.plan_id || '') === planId) {
        pendingPlan = null;
        stopPlanTTLTimer();
        planExpiresAtMs = 0;
      }
      var log = byID('configAgentChatLog');
      var latestExecuteRow = latestChatRow(log);
      if (!removeMarkedChatBubble(log, '[data-ca-executing]') && latestExecuteRow) {
        try { log.removeChild(latestExecuteRow); } catch (_) {}
      }
      var ok = !!(data && data.ok);
      if (data && !data.intent && plannedIntent) data.intent = plannedIntent;
      pushPlanHistory({ intent: data && data.intent, summary: ok ? 'executed' : 'execute failed', plan_id: data && data.plan_id || planId }, ok ? 'executed' : 'failed');
      var rendered = renderExecuteResult(data);
      appendChat('assistant', rendered.html);
      var resRow = latestChatRow(log);
      bindExecuteResultActions(resRow, rendered);
      // Refresh status bar (email / recent groups / diagnose quick actions).
      updateSelectedBar();
      if (ok) {
        // Fire-and-forget refreshes; do not block UI on slow tabs.
        [
          'loadLlmProviders',
          'loadLlmServiceGroups',
          'loadTenantSystemLLMDefaults',
          'loadOverviewSystemFreeStatus',
          'loadTenantMigrationSettings',
          'loadTenantMailSenderName'
        ].forEach(function(fn) {
          if (typeof global[fn] === 'function') {
            try { Promise.resolve(global[fn]()).catch(function() {}); } catch (_) {}
          }
        });
        maybeShowSystemFreeGate(true);
        // Closed loop: after a successful user bind, auto re-diagnose entitlement.
        maybeAutoRediagnoseAfterBind(data);
      }
    } catch (err) {
      if (pendingPlan && String(pendingPlan.plan_id || '') === planId) {
        pendingPlan = null;
        stopPlanTTLTimer();
        planExpiresAtMs = 0;
      }
      var logErr = byID('configAgentChatLog');
      var latestExecuteErrorRow = latestChatRow(logErr);
      if (!removeMarkedChatBubble(logErr, '[data-ca-executing]') && latestExecuteErrorRow) {
        try { logErr.removeChild(latestExecuteErrorRow); } catch (_) {}
      }
      appendChat('assistant', '<div class="item-meta" style="color:#b42318">' + esc(cat('failed') + ': ' + (err && err.message || err)) + '</div>');
      updateSelectedBar();
    } finally {
      executePendingPlan._busy = false;
      setConfigAgentComposerBusy(!!submitConfigAgent._busy);
    }
  }

  var autoRediagnoseBusy = false;
  var wizardExpectRecheck = false;
  function maybeAutoRediagnoseAfterBind(data) {
    if (autoRediagnoseBusy) return;
    if (!data || !data.ok) return;
    var intent = String(data.intent || '');
    if (intent !== 'llm.services.user_bind' && intent !== 'llm.services.user_unbind') return;
    var bindRes = firstStepResult(data, 'llm.services.user_bind') || firstStepResult(data, 'llm.services.user_unbind');
    var email = bindRes && bindRes.email ? String(bindRes.email).trim() : '';
    if (!email) return;
    autoRediagnoseBusy = true;
    wizardExpectRecheck = true;
    appendChat('assistant', '<div class="item-meta" style="color:#027a48">' + esc(cat('reDiagnose')) + '</div>');
    var attempts = 0;
    function tryRediagnose() {
      attempts += 1;
      if (configAgentIsBusy()) {
        if (attempts < 8) {
          setTimeout(tryRediagnose, 250);
          return;
        }
        autoRediagnoseBusy = false;
        return;
      }
      try {
        var input = byID('configAgentInput');
        if (input) input.value = 'diagnose LLM service for ' + email;
        submitConfigAgent();
      } finally {
        // Release lock after nested auto-read can finish.
        setTimeout(function() { autoRediagnoseBusy = false; }, 4000);
      }
    }
    setTimeout(tryRediagnose, 350);
  }

  function applyConfigAgentI18n() {
    var t = byID('configAgentTitle');
    var s = byID('configAgentSubtitle');
    var input = byID('configAgentInput');
    var send = byID('configAgentSendBtn');
    var sendLabel = byID('configAgentSendLabel');
    if (t) t.textContent = cat('title');
    if (s) s.textContent = cat('subtitle');
    if (input) {
      input.placeholder = cat('placeholder');
      // Keep validator ownership of blank placeholder in index.html.
      if (typeof global._s === 'function') {
        try { global._s('configAgentInput', 'placeholder', cat('placeholder')); } catch (_) {}
      }
    }
    if (send) {
      send.setAttribute('aria-label', cat('send'));
      send.setAttribute('title', cat('send'));
    }
    if (sendLabel) sendLabel.textContent = cat('send');
    var morePrompts = byID('configAgentMorePrompts');
    if (morePrompts) morePrompts.textContent = cat('moreActions');
    var historySummary = byID('configAgentHistorySummary');
    if (historySummary) historySummary.textContent = cat('recentPlans');
    var empty = byID('configAgentEmptyHint');
    if (empty) empty.textContent = cat('empty');
  }

  function ensureGateModal() {
    if (byID('systemFreeGateOverlay')) return;
    var overlay = document.createElement('div');
    overlay.id = 'systemFreeGateOverlay';
    overlay.className = 'hidden';
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(20,24,36,.45);z-index:9999;display:flex;align-items:center;justify-content:center;padding:20px';
    overlay.innerHTML = ''
      + '<div class="item" style="max-width:520px;width:100%;padding:20px;background:#fff;border-radius:12px;box-shadow:0 16px 48px rgba(20,24,36,.18)">'
      + '  <div class="item-title" id="systemFreeGateTitle"></div>'
      + '  <div class="item-meta" id="systemFreeGateDesc" style="margin-top:8px"></div>'
      + '  <div class="item-meta mono" id="systemFreeGateDetail" style="margin-top:10px"></div>'
      + '  <div class="actions" style="margin-top:16px;gap:8px;flex-wrap:wrap">'
      + '    <button class="btn-primary" type="button" id="systemFreeGateTestBtn"></button>'
      + '    <button class="btn-secondary" type="button" id="systemFreeGateConfigBtn"></button>'
      + '    <button class="btn-ghost" type="button" id="systemFreeGateLaterBtn"></button>'
      + '  </div>'
      + '</div>';
    document.body.appendChild(overlay);
    byID('systemFreeGateTestBtn').onclick = function() {
      if (typeof global.testTenantSystemFreeLLM === 'function') {
        global.testTenantSystemFreeLLM().then(function(data) {
          // Prefer cache from test payload; force network only when status was absent.
          return maybeShowSystemFreeGate(!(data && data.status));
        }).catch(function() {
          maybeShowSystemFreeGate(true);
        });
      }
    };
    byID('systemFreeGateConfigBtn').onclick = function() {
      hideGateModal();
      if (typeof global.openSystemFreeServiceGroup === 'function') {
        Promise.resolve(global.openSystemFreeServiceGroup()).catch(function() {});
      } else if (typeof global.openTab === 'function') {
        global.openTab('modelservices');
      }
    };
    byID('systemFreeGateLaterBtn').onclick = function() {
      gateDismissedAt = Date.now();
      hideGateModal();
    };
  }

  function hideGateModal() {
    var overlay = byID('systemFreeGateOverlay');
    if (overlay) {
      overlay.classList.add('hidden');
      overlay.style.display = 'none';
    }
  }

  function showGateModal(status) {
    ensureGateModal();
    var overlay = byID('systemFreeGateOverlay');
    if (!overlay) return;
    byID('systemFreeGateTitle').textContent = cat('gateTitle');
    byID('systemFreeGateDesc').textContent = cat('gateDesc');
    byID('systemFreeGateTestBtn').textContent = cat('gateTest');
    byID('systemFreeGateConfigBtn').textContent = cat('gateConfig');
    byID('systemFreeGateLaterBtn').textContent = cat('gateLater');
    var ids = (status && status.provider_ids || []).join(', ') || '-';
    var reasons = (status && status.reasons || []).join(', ');
    byID('systemFreeGateDetail').textContent = cat('providers') + ': ' + ids + (reasons ? ' | ' + reasons : '');
    overlay.classList.remove('hidden');
    overlay.style.display = 'flex';
  }

  async function maybeShowSystemFreeGate(forceRefresh) {
    // Only for tenant-scoped admins after login.
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    if (!profile) return;
    var isTenant = String(profile.scope || '').toLowerCase() === 'tenant';
    // Global admins also benefit when viewing a tenant, but gate primarily for tenant ops.
    if (!isTenant && !forceRefresh) {
      // still refresh overview status if available
    }
    if (gateDismissedAt && (Date.now() - gateDismissedAt) < 30 * 60 * 1000 && !forceRefresh) {
      return;
    }
    try {
      var cached = typeof global.getTenantSystemFreeCache === 'function'
        ? global.getTenantSystemFreeCache()
        : global.tenantSystemFreeStatusCache;
      var st;
      if (forceRefresh || !cached) {
        st = typeof global.fetchTenantSystemFreeStatus === 'function'
          ? await global.fetchTenantSystemFreeStatus()
          : await global.api('/api/admin/llm/system-free');
      } else {
        st = cached;
      }
      if (typeof global.setTenantSystemFreeCache === 'function') {
        st = global.setTenantSystemFreeCache(st || {});
      } else {
        global.tenantSystemFreeStatusCache = st || {};
      }
      if (st && st.ready) {
        hideGateModal();
        return;
      }
      // Soft-block: show modal once after login / when not ready.
      showGateModal(st || {});
    } catch (err) {
      showGateModal({ reasons: [String(err && err.message || err || 'load failed')] });
    }
  }

  function isConfigAgentContext(el) {
    var root = byID('overviewConfigAgent');
    if (!root) return false;
    if (!el) return false;
    try {
      return root === el || root.contains(el);
    } catch (_) {
      return false;
    }
  }

  function planIsReadyToConfirm(plan) {
    if (!plan || !plan.plan_id || !plan.confirm_token) return false;
    if ((plan.missing_fields || []).length) return false;
    // Mirror plan expiry: do not offer confirm on stale client plan.
    if (planExpiresAtMs && planExpiresAtMs <= Date.now()) return false;
    return true;
  }

  function onConfigAgentGlobalKeydown(e) {
    if (!e) return;
    var tag = (e.target && e.target.tagName) ? String(e.target.tagName).toLowerCase() : '';
    var typing = tag === 'input' || tag === 'textarea' || tag === 'select' || (e.target && e.target.isContentEditable);
    var inAgent = isConfigAgentContext(e.target);

    // Help remains dismissible while a request is in flight; it is purely local UI.
    if (e.key === 'Escape' || e.key === 'Esc') {
      var activeHelpOverlay = byID('configAgentShortcutsOverlay');
      if (activeHelpOverlay && activeHelpOverlay.style.display === 'flex') {
        e.preventDefault();
        hideShortcutsHelp();
        return;
      }
    }

    if (configAgentIsBusy()) {
      if (inAgent && ((e.ctrlKey || e.metaKey) && (e.key === 'Enter' || e.key === 'k' || e.key === 'K'))) {
        e.preventDefault();
      }
      if (inAgent && (e.key === 'Escape' || e.key === 'Esc')) e.preventDefault();
      return;
    }

    // Esc: close help first, else cancel pending plan.
    if (e.key === 'Escape' || e.key === 'Esc') {
      if (!(pendingPlan && pendingPlan.confirm_token)) return;
      if (!inAgent && typing) return; // don't steal Esc from other forms
      e.preventDefault();
      e.stopPropagation();
      clearConfigAgentSession({ toast: cat('cancel') });
      appendChat('assistant', '<div class="item-meta">' + esc(cat('cancel')) + ' <span style="opacity:.6">(Esc)</span></div>');
      return;
    }

    // Ctrl+/ or ?: open shortcuts help
    if ((e.ctrlKey || e.metaKey) && (e.key === '/' || e.key === '?')) {
      if (!inAgent && !pendingPlan && !byID('overviewConfigAgent')) return;
      e.preventDefault();
      showShortcutsHelp();
      return;
    }

    // Ctrl/Cmd+Shift+T: open tool catalog
    if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'T' || e.key === 't')) {
      if (!inAgent && !pendingPlan) return;
      e.preventDefault();
      showToolCatalog();
      return;
    }

    // Ctrl/Cmd+Enter: confirm plan if ready, else send message (when focused in agent).
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      if (!inAgent && !planIsReadyToConfirm(pendingPlan)) return;
      e.preventDefault();
      if (planIsReadyToConfirm(pendingPlan)) {
        executePendingPlan(true);
      } else {
        submitConfigAgent();
      }
      return;
    }

    // Ctrl/Cmd+K: focus config agent input
    if ((e.ctrlKey || e.metaKey) && !e.shiftKey && (e.key === 'k' || e.key === 'K')) {
      var root = byID('overviewConfigAgent');
      if (!root) return;
      // Only when overview tab is somewhat visible / agent exists.
      if (root.offsetParent === null && root.getClientRects().length === 0) return;
      e.preventDefault();
      var inp = byID('configAgentInput');
      if (inp) {
        inp.focus();
        if (inp.setSelectionRange) {
          var len = String(inp.value || '').length;
          try { inp.setSelectionRange(len, len); } catch (_) {}
        }
      }
    }
  }

  function initConfigAgent() {
    applyConfigAgentI18n();
    loadUIPrefs();
    loadRememberedEmail();
    loadRecentGroups();
    loadDiagnoseHint();
    // Warm provider cache for multi-turn provider_id quick-fill.
    ensureProvidersCached().catch(function() {});
    var send = byID('configAgentSendBtn');
    var input = byID('configAgentInput');
    if (send) send.onclick = function() { submitConfigAgent(); };
    if (input) {
      input.onkeydown = function(e) {
        // Plain Enter (no modifiers) sends; Ctrl+Enter handled globally (confirm if plan ready).
        if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
          e.preventDefault();
          submitConfigAgent();
        }
      };
      input.oninput = function() {
        var em = extractEmailFromText(input.value);
        if (em) rememberEmail(em);
        else updateSelectedBar();
      };
      if (!input.getAttribute('title')) {
        input.setAttribute('title', cat('shortcuts'));
      }
    }
    var examples = byID('configAgentExamples');
    if (examples) {
      examples.onclick = function(e) {
        var t = e.target;
        if (t && t.getAttribute && t.getAttribute('data-ca-example')) {
          var msg = t.getAttribute('data-ca-example') || '';
          // Alt+click (or middle-click via alt) stars as favorite without running.
          if (e.altKey) {
            e.preventDefault();
            toggleFavoriteCommand(msg, t.textContent || msg);
            return;
          }
          if (input) input.value = msg;
          var em = extractEmailFromText(input && input.value);
          if (em) rememberEmail(em);
          submitConfigAgent();
        }
      };
      if (!examples.getAttribute('title')) {
        examples.setAttribute('title', cat('favHint'));
      }
    }
    renderFavoritesBar();
    if (!global.__configAgentKeysBound) {
      document.addEventListener('keydown', onConfigAgentGlobalKeydown, true);
      global.__configAgentKeysBound = true;
    }
    updateSelectedBar();
    loadPlanHistoryFromServer();
  }

  global.initConfigAgent = initConfigAgent;
  global.applyConfigAgentI18n = applyConfigAgentI18n;
  global.maybeShowSystemFreeGate = maybeShowSystemFreeGate;
  global.submitConfigAgent = submitConfigAgent;

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initConfigAgent);
  } else {
    initConfigAgent();
  }
})(window);
