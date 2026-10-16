/**
 * Terminal Node Configuration Panel
 *
 * Provides the configuration UI for Terminal (End) nodes in the workflow editor.
 * Handles:
 * - Result Executor user search + add (Hub user directory)
 * - Notifier user search + add
 * - Per-executor: timeout_hours (1-720, default 48), max_reminders (1-10, default 3)
 * - Per-notifier: timeout_hours (1-720, default 72), max_reminders (1-10, default 2)
 * - Warning banner when no executors configured (yellow, non-blocking)
 * - Inline validation errors for out-of-range values
 *
 * Config model (matches Go TerminalNodeConfig):
 *   {
 *     result_executors: [{user_id, timeout_hours, max_reminders, reminder_interval_hours}],
 *     notifiers: [{user_id, timeout_hours, max_reminders, reminder_interval_hours}]
 *   }
 *
 * Requirements: 14.1, 14.2, 14.3, 14.4, 14.5
 */
(function () {
  'use strict';

  // --- Constants ---
  var EXECUTOR_DEFAULTS = { timeout_hours: 48, max_reminders: 3, reminder_interval_hours: 24 };
  var NOTIFIER_DEFAULTS = { timeout_hours: 72, max_reminders: 2, reminder_interval_hours: 24 };
  var TIMEOUT_MIN = 1;
  var TIMEOUT_MAX = 720;
  var REMINDERS_MIN = 1;
  var REMINDERS_MAX = 10;
  var FALLBACK_TEXT = {
    noExecutorWarning: 'No Result Executor configured. The workflow will complete without notifying anyone to take action.',
    resultExecutors: 'Result Executors',
    resultExecutorsDesc: 'People who need to take action after workflow completion',
    notifiers: 'Notifiers',
    notifiersDesc: 'People who should be informed of the workflow outcome',
    searchUsers: 'Search users by name or ID...',
    noExecutorsAdded: 'No executors added yet.',
    noNotifiersAdded: 'No notifiers added yet.',
    remove: 'Remove',
    timeoutShort: 'Timeout (hours)',
    maxReminders: 'Max Reminders',
    noUsersFound: 'No users found',
    mustBeBetween: 'Must be between {min} and {max}'
  };

  function tr(key, vars) {
    if (window.ApprovalWorkflowI18n && typeof window.ApprovalWorkflowI18n.t === 'function') return window.ApprovalWorkflowI18n.t(key, vars);
    return String(FALLBACK_TEXT[key] || key).replace(/\{([^}]+)\}/g, function (_, name) {
      return vars && vars[name] !== undefined ? String(vars[name]) : '';
    });
  }

  // --- User search (mock for Hub user directory) ---
  // In production, this calls GET /api/v1/users/search?q=...
  var defaultSearchUsers = function (query) {
    return fetch('/api/v1/users/search?q=' + encodeURIComponent(query))
      .then(function (resp) {
        if (!resp.ok) return [];
        return resp.json();
      })
      .then(function (data) {
        // Expect [{id, name}] from API
        return Array.isArray(data) ? data : (data.users || []);
      })
      .catch(function () { return []; });
  };

  // --- Exported: Build terminal node config form HTML ---
  // Called by workflow-editor.js when a terminal node is selected.
  window.buildTerminalNodeConfigForm = function (node, searchUsers) {
    var config = node.config || {};
    if (!config.result_executors) config.result_executors = [];
    if (!config.notifiers) config.notifiers = [];
    node.config = config;

    var html = '';

    // Warning banner: no executors
    html += '<div id="terminalNoExecutorWarning" class="terminal-warning" style="' +
      (config.result_executors.length === 0 ? '' : 'display:none;') + '">' +
      '<span class="terminal-warning-icon" aria-hidden="true">!</span>' +
      '<span>' + escapeHtml(tr('noExecutorWarning')) + '</span>' +
      '</div>';

    // --- Result Executors Section ---
    html += '<div class="terminal-section">';
    html += '<div class="terminal-section-title">' + escapeHtml(tr('resultExecutors')) + '</div>';
    html += '<div class="terminal-section-desc">' + escapeHtml(tr('resultExecutorsDesc')) + '</div>';
    html += '<div class="terminal-user-search">';
    html += '<input type="text" id="terminalExecutorSearch" placeholder="' + escapeAttr(tr('searchUsers')) + '" autocomplete="off">';
    html += '<div id="terminalExecutorSearchResults" class="terminal-search-results"></div>';
    html += '</div>';
    html += '<div id="terminalExecutorList" class="terminal-user-list">';
    html += buildUserListHTML(config.result_executors, 'executor');
    html += '</div>';
    html += '</div>';

    // --- Notifiers Section ---
    html += '<div class="terminal-section">';
    html += '<div class="terminal-section-title">' + escapeHtml(tr('notifiers')) + '</div>';
    html += '<div class="terminal-section-desc">' + escapeHtml(tr('notifiersDesc')) + '</div>';
    html += '<div class="terminal-user-search">';
    html += '<input type="text" id="terminalNotifierSearch" placeholder="' + escapeAttr(tr('searchUsers')) + '" autocomplete="off">';
    html += '<div id="terminalNotifierSearchResults" class="terminal-search-results"></div>';
    html += '</div>';
    html += '<div id="terminalNotifierList" class="terminal-user-list">';
    html += buildUserListHTML(config.notifiers, 'notifier');
    html += '</div>';
    html += '</div>';

    return html;
  };

  // --- Exported: Attach event listeners for terminal node config ---
  window.attachTerminalNodeConfigListeners = function (node, searchUsers, onChange) {
    var searchFn = searchUsers || defaultSearchUsers;
    var markChanged = typeof onChange === 'function' ? onChange : function () {};
    var config = node.config;

    // Executor search
    setupUserSearch(
      'terminalExecutorSearch',
      'terminalExecutorSearchResults',
      searchFn,
      function (user) {
        // Check duplicate
        var exists = config.result_executors.some(function (e) { return e.user_id === user.id; });
        if (exists) return;
        config.result_executors.push({
          user_id: user.id,
          user_name: user.name,
          timeout_hours: EXECUTOR_DEFAULTS.timeout_hours,
          max_reminders: EXECUTOR_DEFAULTS.max_reminders,
          reminder_interval_hours: EXECUTOR_DEFAULTS.reminder_interval_hours,
        });
        markChanged();
        rerenderExecutorList(node, markChanged);
        updateWarningBanner(config);
      }
    );

    // Notifier search
    setupUserSearch(
      'terminalNotifierSearch',
      'terminalNotifierSearchResults',
      searchFn,
      function (user) {
        var exists = config.notifiers.some(function (n) { return n.user_id === user.id; });
        if (exists) return;
        config.notifiers.push({
          user_id: user.id,
          user_name: user.name,
          timeout_hours: NOTIFIER_DEFAULTS.timeout_hours,
          max_reminders: NOTIFIER_DEFAULTS.max_reminders,
          reminder_interval_hours: NOTIFIER_DEFAULTS.reminder_interval_hours,
        });
        markChanged();
        rerenderNotifierList(node, markChanged);
      }
    );

    // Attach input listeners for existing items
    attachItemListeners(node, 'executor', markChanged);
    attachItemListeners(node, 'notifier', markChanged);
  };

  // --- Build user list HTML ---
  function buildUserListHTML(items, type) {
    if (!items || items.length === 0) {
      return '<div class="terminal-empty-list">' + escapeHtml(tr(type === 'executor' ? 'noExecutorsAdded' : 'noNotifiersAdded')) + '</div>';
    }
    var html = '';
    items.forEach(function (item, idx) {
      html += buildUserItemHTML(item, idx, type);
    });
    return html;
  }

  function buildUserItemHTML(item, idx, type) {
    var defaults = type === 'executor' ? EXECUTOR_DEFAULTS : NOTIFIER_DEFAULTS;
    var prefix = 'terminal_' + type + '_' + idx;

    var html = '<div class="terminal-user-item" data-type="' + type + '" data-index="' + idx + '">';
    html += '<div class="terminal-user-item-header">';
    html += '<span class="terminal-user-name">' + escapeHtml(item.user_name || item.user_id) + '</span>';
    html += '<span class="terminal-user-id">' + escapeHtml(item.user_id) + '</span>';
    html += '<button class="terminal-remove-btn" data-type="' + type + '" data-index="' + idx + '" title="' + escapeAttr(tr('remove')) + '">&times;</button>';
    html += '</div>';
    html += '<div class="terminal-user-item-fields">';

    // timeout_hours
    html += '<div class="terminal-inline-field">';
    html += '<label for="' + prefix + '_timeout">' + escapeHtml(tr('timeoutShort')) + '</label>';
    html += '<input type="number" id="' + prefix + '_timeout" min="' + TIMEOUT_MIN + '" max="' + TIMEOUT_MAX + '" value="' + (item.timeout_hours || defaults.timeout_hours) + '" data-field="timeout_hours">';
    html += '<span class="terminal-field-hint">' + TIMEOUT_MIN + '-' + TIMEOUT_MAX + '</span>';
    html += '<span class="terminal-field-error" id="' + prefix + '_timeout_error"></span>';
    html += '</div>';

    // max_reminders
    html += '<div class="terminal-inline-field">';
    html += '<label for="' + prefix + '_reminders">' + escapeHtml(tr('maxReminders')) + '</label>';
    html += '<input type="number" id="' + prefix + '_reminders" min="' + REMINDERS_MIN + '" max="' + REMINDERS_MAX + '" value="' + (item.max_reminders || defaults.max_reminders) + '" data-field="max_reminders">';
    html += '<span class="terminal-field-hint">' + REMINDERS_MIN + '-' + REMINDERS_MAX + '</span>';
    html += '<span class="terminal-field-error" id="' + prefix + '_reminders_error"></span>';
    html += '</div>';

    html += '</div>';
    html += '</div>';
    return html;
  }

  // --- User search setup ---
  function setupUserSearch(inputId, resultsId, searchFn, onSelect) {
    var input = document.getElementById(inputId);
    var resultsContainer = document.getElementById(resultsId);
    if (!input || !resultsContainer) return;

    var debounceTimer = null;

    input.addEventListener('input', function () {
      var query = input.value.trim();
      if (query.length < 1) {
        resultsContainer.innerHTML = '';
        resultsContainer.style.display = 'none';
        return;
      }

      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(function () {
        var result = searchFn(query);
        // Handle both Promise and direct array
        if (result && typeof result.then === 'function') {
          result.then(function (users) {
            renderSearchResults(resultsContainer, users, input, onSelect);
          });
        } else {
          renderSearchResults(resultsContainer, result || [], input, onSelect);
        }
      }, 300);
    });

    input.addEventListener('blur', function () {
      // Delay to allow click on results
      setTimeout(function () {
        resultsContainer.style.display = 'none';
      }, 200);
    });

    input.addEventListener('focus', function () {
      if (resultsContainer.innerHTML) {
        resultsContainer.style.display = 'block';
      }
    });
  }

  function renderSearchResults(container, users, input, onSelect) {
    if (!users || users.length === 0) {
      container.innerHTML = '<div class="terminal-search-empty">' + escapeHtml(tr('noUsersFound')) + '</div>';
      container.style.display = 'block';
      return;
    }

    var html = '';
    users.forEach(function (user) {
      html += '<div class="terminal-search-result-item" data-user-id="' + escapeAttr(user.id) + '" data-user-name="' + escapeAttr(user.name) + '">';
      html += '<span class="terminal-search-result-name">' + escapeHtml(user.name) + '</span>';
      html += '<span class="terminal-search-result-id">' + escapeHtml(user.id) + '</span>';
      html += '</div>';
    });
    container.innerHTML = html;
    container.style.display = 'block';

    // Attach click handlers
    container.querySelectorAll('.terminal-search-result-item').forEach(function (el) {
      el.addEventListener('mousedown', function (e) {
        e.preventDefault(); // Prevent blur
        var userId = el.getAttribute('data-user-id');
        var userName = el.getAttribute('data-user-name');
        onSelect({ id: userId, name: userName });
        input.value = '';
        container.innerHTML = '';
        container.style.display = 'none';
      });
    });
  }

  // --- Re-render lists ---
  function rerenderExecutorList(node, onChange) {
    var container = document.getElementById('terminalExecutorList');
    if (container) {
      container.innerHTML = buildUserListHTML(node.config.result_executors, 'executor');
      attachItemListeners(node, 'executor', onChange);
    }
  }

  function rerenderNotifierList(node, onChange) {
    var container = document.getElementById('terminalNotifierList');
    if (container) {
      container.innerHTML = buildUserListHTML(node.config.notifiers, 'notifier');
      attachItemListeners(node, 'notifier', onChange);
    }
  }

  // --- Attach listeners for user items (remove buttons + input validation) ---
  function attachItemListeners(node, type, onChange) {
    var markChanged = typeof onChange === 'function' ? onChange : function () {};
    var config = node.config;
    var listId = type === 'executor' ? 'terminalExecutorList' : 'terminalNotifierList';
    var container = document.getElementById(listId);
    if (!container) return;

    // Remove buttons
    container.querySelectorAll('.terminal-remove-btn[data-type="' + type + '"]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var idx = parseInt(btn.getAttribute('data-index'), 10);
        if (type === 'executor') {
          config.result_executors.splice(idx, 1);
          markChanged();
          rerenderExecutorList(node, markChanged);
          updateWarningBanner(config);
        } else {
          config.notifiers.splice(idx, 1);
          markChanged();
          rerenderNotifierList(node, markChanged);
        }
      });
    });

    // Input validation
    container.querySelectorAll('input[type="number"]').forEach(function (input) {
      input.addEventListener('input', function () {
        var itemEl = input.closest('.terminal-user-item');
        var idx = parseInt(itemEl.getAttribute('data-index'), 10);
        var field = input.getAttribute('data-field');
        var value = parseInt(input.value, 10);
        var items = type === 'executor' ? config.result_executors : config.notifiers;
        markChanged();

        if (validateField(input, field, value) && idx >= 0 && idx < items.length) {
          items[idx][field] = value;
        }
      });
    });
  }

  // --- Field validation ---
  function validateField(input, field, value) {
    var errorEl = document.getElementById(input.id + '_error');
    if (!errorEl) return;

    var error = '';
    if (field === 'timeout_hours') {
      if (isNaN(value) || value < TIMEOUT_MIN || value > TIMEOUT_MAX) {
        error = tr('mustBeBetween', { min: TIMEOUT_MIN, max: TIMEOUT_MAX });
      }
    } else if (field === 'max_reminders') {
      if (isNaN(value) || value < REMINDERS_MIN || value > REMINDERS_MAX) {
        error = tr('mustBeBetween', { min: REMINDERS_MIN, max: REMINDERS_MAX });
      }
    }

    errorEl.textContent = error;
    input.classList.toggle('terminal-field-invalid', error !== '');
    input.setAttribute('aria-invalid', error !== '' ? 'true' : 'false');
    return error === '';
  }

  // --- Warning banner ---
  function updateWarningBanner(config) {
    var banner = document.getElementById('terminalNoExecutorWarning');
    if (banner) {
      banner.style.display = config.result_executors.length === 0 ? '' : 'none';
    }
  }

  // --- Utility ---
  function escapeHtml(str) {
    return String(str || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function escapeAttr(str) {
    return String(str || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  // --- Exported: Get default config for terminal node ---
  window.getTerminalNodeDefaultConfig = function () {
    return {
      result_executors: [],
      notifiers: [],
    };
  };

})();
