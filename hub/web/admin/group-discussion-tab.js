/*
 * Current-Hub MaClaw group discussion admin module.
 * Keep this file ASCII-only so localized strings survive Windows shells.
 */
(function(global) {
  const copy = {
    en: {
      nav: 'Group Discussion',
      navDesc: 'Current-Hub MaClaw experts',
      title: 'Group Discussion',
      subtitle: 'View current-Hub MaClaw experts, discussion topics, participants, and results.',
      refresh: 'Refresh Data',
      kicker: 'Admin Console',
      connected: 'Connected',
      overviewTitle: 'Group Discussion Overview',
      refreshing: 'Refreshing...',
      scope: 'Only shows MaClaw experts, discussion topics, participants, and results on the current Hub. Cross-Hub, Hub Center, and AgentNet are not included in this list.',
      activeExperts: 'Active Experts',
      totalExperts: 'Total Experts',
      activeDiscussions: 'Open Discussions',
      readyDiscussions: 'Ready',
      historyCount: 'History',
      expertsTitle: 'Current Active MaClaw Experts',
      expertsDesc: 'Only MaClaws that enabled group discussion and discovery on this Hub appear here. Private files, paths, secrets, and local context are not exposed.',
      history: 'Discussion History',
      historyDesc: 'Topics, participants, progress, and readiness for current-Hub consultations.',
      resultTitle: 'Discussion Results',
      resultDesc: 'Summaries submitted back to the Hub after expert discussion.',
      emptyExperts: 'No active MaClaw experts. Enable Group Discussion and discovery in MaClaw settings.',
      emptyHistory: 'No group discussions yet.',
      emptyResults: 'No discussion results yet.',
      source: 'Data source: Hub API / scope: current Hub',
      loading: 'Loading...',
      failed: 'Failed to load group discussions: {error}',
      expert: 'Expert',
      skills: 'Specialties',
      model: 'Model',
      languages: 'Languages',
      availability: 'Status',
      updated: 'Last heartbeat',
      topic: 'Topic / Question',
      participants: 'Participants',
      status: 'Status',
      progress: 'Progress',
      result: 'Result Summary',
      completed: 'Completed',
      ready: 'ready to summarize',
      open: 'Discussing',
      decided: 'Completed',
      escalated: 'Escalated',
      closed: 'Closed',
      available: 'Online',
      discoverable: 'Discoverable',
      hidden: 'Hidden'
    },
    zh: {
      nav: '\u7fa4\u7ec4\u8ba8\u8bba',
      navDesc: '\u5f53\u524d Hub MaClaw \u4e13\u5bb6',
      title: '\u7fa4\u7ec4\u8ba8\u8bba',
      subtitle: '\u67e5\u770b\u5f53\u524d Hub \u5185 MaClaw \u4e13\u5bb6\u3001\u5386\u53f2\u8ba8\u8bba\u4e3b\u9898\u3001\u53c2\u4e0e\u8005\u548c\u8ba8\u8bba\u7ed3\u679c\u3002',
      refresh: '\u5237\u65b0\u6570\u636e',
      kicker: 'ADMIN CONSOLE',
      connected: '\u5df2\u8fde\u63a5',
      overviewTitle: '\u7fa4\u7ec4\u8ba8\u8bba\u603b\u89c8',
      refreshing: '\u5237\u65b0\u4e2d...',
      scope: '\u4ec5\u5c55\u793a\u5f53\u524d Hub \u5185 MaClaw \u4e13\u5bb6\u3001\u8ba8\u8bba\u4e3b\u9898\u3001\u53c2\u4e0e\u8005\u548c\u5df2\u4ea7\u51fa\u7684\u8ba8\u8bba\u7ed3\u679c\u3002\u8de8 Hub\u3001HubCenter \u548c AgentNet \u4e0d\u53c2\u4e0e\u6b64\u5217\u8868\u3002',
      activeExperts: '\u6d3b\u8dc3\u4e13\u5bb6',
      totalExperts: '\u4e13\u5bb6\u603b\u6570',
      activeDiscussions: '\u8ba8\u8bba\u4e2d',
      readyDiscussions: '\u53ef\u6536\u5c3e',
      historyCount: '\u5386\u53f2\u8ba8\u8bba',
      expertsTitle: '\u5f53\u524d\u6d3b\u8dc3 MaClaw \u4e13\u5bb6',
      expertsDesc: '\u5f00\u542f\u7fa4\u7ec4\u8ba8\u8bba\u4e14\u53ef\u53d1\u73b0\u7684 MaClaw \u4f1a\u51fa\u73b0\u5728\u8fd9\u91cc\uff1bHub \u53ea\u5c55\u793a\u5176\u516c\u5f00\u4e13\u5bb6\u8eab\u4efd\uff0c\u4e0d\u5c55\u793a\u672c\u673a\u8def\u5f84\u3001\u5bc6\u94a5\u6216\u79c1\u6709\u4e0a\u4e0b\u6587\u3002',
      history: '\u8ba8\u8bba\u5386\u53f2',
      historyDesc: '\u6309\u6700\u8fd1\u66f4\u65b0\u65f6\u95f4\u5c55\u793a\u5f53\u524d Hub \u5185\u7684\u8ba8\u8bba\u4e3b\u9898\u548c\u53c2\u4e0e\u8005\u3002',
      resultTitle: '\u8ba8\u8bba\u7ed3\u679c',
      resultDesc: '\u5c55\u793a\u5df2\u5f62\u6210\u5efa\u8bae\u3001\u51b3\u7b56\u6216\u5347\u7ea7\u7ed3\u8bba\u7684\u8ba8\u8bba\u6458\u8981\u3002',
      emptyExperts: '\u6682\u65e0\u6d3b\u8dc3 MaClaw \u4e13\u5bb6\u3002\u8bf7\u5728 MaClaw \u8bbe\u7f6e\u4e2d\u5f00\u542f\u201c\u7fa4\u7ec4\u8ba8\u8bba\u201d\u548c\u201c\u53ef\u88ab\u53d1\u73b0\u201d\u3002',
      emptyHistory: '\u6682\u65e0\u5386\u53f2\u8ba8\u8bba\u3002',
      emptyResults: '\u6682\u65e0\u8ba8\u8bba\u7ed3\u679c\u3002',
      source: '\u6570\u636e\u6765\u6e90\uff1aHub API / \u4f5c\u7528\u57df\uff1a\u5f53\u524d Hub',
      loading: '\u52a0\u8f7d\u4e2d...',
      failed: '\u52a0\u8f7d\u7fa4\u7ec4\u8ba8\u8bba\u5931\u8d25\uff1a{error}',
      expert: '\u4e13\u5bb6',
      skills: '\u64c5\u957f\u65b9\u5411',
      model: '\u6a21\u578b\u80fd\u529b',
      languages: '\u8bed\u8a00',
      availability: '\u72b6\u6001',
      updated: '\u6700\u8fd1\u5fc3\u8df3',
      topic: '\u4e3b\u9898 / \u95ee\u9898',
      participants: '\u53c2\u4e0e\u8005',
      status: '\u72b6\u6001',
      progress: '\u7b54\u590d\u8fdb\u5ea6',
      result: '\u7ed3\u679c\u6458\u8981',
      completed: '\u5b8c\u6210\u65f6\u95f4',
      ready: '\u53ef\u6536\u5c3e',
      open: '\u8ba8\u8bba\u4e2d',
      decided: '\u5df2\u5b8c\u6210',
      escalated: '\u5df2\u5347\u7ea7',
      closed: '\u5df2\u5173\u95ed',
      available: '\u5728\u7ebf\u53ef\u7528',
      discoverable: '\u53ef\u53d1\u73b0',
      hidden: '\u9690\u85cf'
    }
  };

  function gd(key) {
    const lang = global.currentLang === 'zh' ? 'zh' : 'en';
    return (copy[lang] && copy[lang][key]) || copy.en[key] || key;
  }

  function html(value) {
    if (typeof global.escapeHtml === 'function') return global.escapeHtml(value || '');
    return String(value || '').replace(/[&<>"']/g, function(ch) { return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[ch]; });
  }

  function setText(id, value) {
    const el = document.getElementById(id);
    if (el) el.textContent = value || '';
  }

  function badge(value, tone, title) {
    const tooltip = title ? ' title="' + html(title) + '"' : '';
    return '<span class="badge ' + html(tone || 'info') + '"' + tooltip + '>' + html(value || '-') + '</span>';
  }

  function renderMetric(label, value, tone) {
    return '<div class="metric ' + html(tone || '') + '"><label>' + html(label) + '</label><strong>' + html(String(value || 0)) + '</strong></div>';
  }

  function renderSimpleHint(message) {
    return '<div class="hint">' + html(message) + '</div>';
  }

  function joinList(value) {
    return Array.isArray(value) && value.length ? value.join(global.currentLang === 'zh' ? '\u3001' : ', ') : '-';
  }

  function formatTime(value) {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return String(value);
    return date.toLocaleString();
  }

  function statusLabel(status) {
    return gd(status || '') || status || '-';
  }

  function availabilityLabel(item) {
    if (item.available) return gd('available');
    if (item.discoverable) return gd('discoverable');
    return gd('hidden');
  }

  function renderExperts(items) {
    const root = document.getElementById('groupDiscussionExperts');
    if (!root) return;
    if (!items.length) {
      root.innerHTML = renderSimpleHint(gd('emptyExperts'));
      return;
    }
    root.innerHTML = '<div class="row header" style="grid-template-columns:1fr 1.2fr .75fr .75fr .7fr .85fr"><div>' + gd('expert') + '</div><div>' + gd('skills') + '</div><div>' + gd('model') + '</div><div>' + gd('languages') + '</div><div>' + gd('availability') + '</div><div>' + gd('updated') + '</div></div>' + items.map(function(item) {
      const skills = joinList(item.skills);
      const name = item.display_name || item.agent_id || 'MaClaw';
      const statusTone = item.available ? 'ok' : (item.discoverable ? 'info' : 'warn');
      return '<div class="row" style="grid-template-columns:1fr 1.2fr .75fr .75fr .7fr .85fr"><div><strong>' + html(name) + '</strong><div class="item-meta mono">' + html(item.agent_id || '') + '</div></div><div class="item-meta">' + html(skills === '-' ? (item.description || '-') : skills) + '</div><div class="item-meta mono">' + html(item.model_class || '-') + '</div><div class="item-meta">' + html(joinList(item.languages)) + '</div><div>' + badge(availabilityLabel(item), statusTone) + '</div><div class="item-meta">' + html(formatTime(item.updated_at)) + '</div></div>';
    }).join('');
  }

  function discussionProgress(item) {
    const answer = Number(item.answer_count || 0);
    const expected = Number(item.expected_answer_count || 0);
    const base = expected > 0 ? (answer + '/' + expected) : String(answer);
    return item.ready_to_summarize ? (base + ' - ' + gd('ready')) : base;
  }

  function renderHistory(items) {
    const root = document.getElementById('groupDiscussionHistory');
    if (!root) return;
    if (!items.length) {
      root.innerHTML = renderSimpleHint(gd('emptyHistory'));
      return;
    }
    root.innerHTML = '<div class="row header" style="grid-template-columns:1.35fr 1fr .7fr .7fr .85fr"><div>' + gd('topic') + '</div><div>' + gd('participants') + '</div><div>' + gd('progress') + '</div><div>' + gd('status') + '</div><div>' + gd('updated') + '</div></div>' + items.map(function(item) {
      const participants = joinList(item.participant_ids);
      const progressTone = item.ready_to_summarize ? 'ok' : 'info';
      const statusTone = item.status === 'open' ? 'info' : (item.status === 'escalated' ? 'warn' : 'ok');
      return '<div class="row" style="grid-template-columns:1.35fr 1fr .7fr .7fr .85fr"><div><strong>' + html(item.topic || item.question || item.id || '-') + '</strong><div class="item-meta">' + html(item.question || '') + '</div></div><div class="item-meta">' + html(participants) + '</div><div>' + badge(discussionProgress(item), progressTone, item.readiness_reason || '') + '</div><div>' + badge(statusLabel(item.status), statusTone) + '</div><div class="item-meta">' + html(formatTime(item.updated_at || item.created_at)) + '</div></div>';
    }).join('');
  }

  function renderResults(items) {
    const root = document.getElementById('groupDiscussionResults');
    if (!root) return;
    const results = items.filter(function(item) { return !!item.result_summary; });
    if (!results.length) {
      root.innerHTML = renderSimpleHint(gd('emptyResults'));
      return;
    }
    root.innerHTML = '<div class="row header" style="grid-template-columns:1.1fr 1.5fr 1fr .85fr"><div>' + gd('topic') + '</div><div>' + gd('result') + '</div><div>' + gd('participants') + '</div><div>' + gd('completed') + '</div></div>' + results.map(function(item) {
      return '<div class="row" style="grid-template-columns:1.1fr 1.5fr 1fr .85fr"><div><strong>' + html(item.topic || item.question || item.id || '-') + '</strong></div><div class="item-meta">' + html(item.result_summary || '-') + '</div><div class="item-meta">' + html(joinList(item.participant_ids)) + '</div><div class="item-meta">' + html(formatTime(item.updated_at || item.created_at)) + '</div></div>';
    }).join('');
  }

  function applyText() {
    setText('navGroupDiscussion', gd('nav'));
    setText('navGroupDiscussionDesc', gd('navDesc'));
    setText('groupDiscussionTitle', gd('title'));
    setText('groupDiscussionSubtitle', gd('subtitle'));
    setText('groupDiscussionRefreshBtn', gd('refresh'));
    setText('groupDiscussionOverviewRefreshBtn', gd('refresh'));
    setText('groupDiscussionKicker', gd('kicker'));
    setText('groupDiscussionConnectedBadge', gd('connected'));
    setText('groupDiscussionOverviewTitle', gd('overviewTitle'));
    setText('groupDiscussionScopeHint', gd('scope'));
    setText('groupDiscussionExpertsTitle', gd('expertsTitle'));
    setText('groupDiscussionExpertsDesc', gd('expertsDesc'));
    setText('groupDiscussionHistoryTitle', gd('history'));
    setText('groupDiscussionHistoryDesc', gd('historyDesc'));
    setText('groupDiscussionResultsTitle', gd('resultTitle'));
    setText('groupDiscussionResultsDesc', gd('resultDesc'));
    setText('groupDiscussionSource', gd('source'));
  }

  global.loadGroupDiscussion = async function loadGroupDiscussion() {
    const stats = document.getElementById('groupDiscussionStats');
    const refresh = document.getElementById('groupDiscussionRefreshBtn');
    if (stats) stats.innerHTML = renderMetric(gd('activeExperts'), gd('loading'));
    if (refresh) refresh.textContent = gd('refreshing');
    const overviewRefresh = document.getElementById('groupDiscussionOverviewRefreshBtn');
    if (overviewRefresh) overviewRefresh.textContent = gd('refreshing');
    try {
      const data = await global.api('/api/admin/a2a/group-discussions');
      const experts = Array.isArray(data.experts) ? data.experts : [];
      const discussions = Array.isArray(data.discussions) ? data.discussions : [];
      const activeExperts = Number.isFinite(Number(data.active_experts)) ? Number(data.active_experts) : experts.filter(function(item) { return !!item.available; }).length;
      const totalExperts = Number.isFinite(Number(data.total_experts)) ? Number(data.total_experts) : experts.length;
      const activeDiscussions = discussions.filter(function(item) { return item.status === 'open'; }).length;
      const ready = discussions.filter(function(item) { return !!item.ready_to_summarize; }).length;
      if (stats) stats.innerHTML = [
        renderMetric(gd('activeExperts'), activeExperts, 'ok'),
        renderMetric(gd('totalExperts'), totalExperts),
        renderMetric(gd('activeDiscussions'), activeDiscussions, activeDiscussions > 0 ? 'warn' : ''),
        renderMetric(gd('readyDiscussions'), ready, ready > 0 ? 'ok' : ''),
        renderMetric(gd('historyCount'), discussions.length)
      ].join('');
      renderExperts(experts);
      renderHistory(discussions);
      renderResults(discussions);
      setText('groupDiscussionSource', gd('source'));
    } catch (err) {
      if (stats) stats.innerHTML = renderSimpleHint(gd('failed').replace('{error}', err.message || String(err)));
    } finally {
      if (refresh) refresh.textContent = gd('refresh');
      if (overviewRefresh) overviewRefresh.textContent = gd('refresh');
    }
  };

  if (global.AdminTabRegistry) {
    global.AdminTabRegistry.registerTab({
      id: 'groupdiscussion',
      title: function() { return gd('title'); },
      subtitle: function() { return gd('subtitle'); },
      onOpen: function() { global.loadGroupDiscussion(); }
    });
    global.AdminTabRegistry.onLanguageChange(function() {
      applyText();
      if (typeof global.loadGroupDiscussion === 'function') global.loadGroupDiscussion();
    });
  }

  applyText();
})(window);
