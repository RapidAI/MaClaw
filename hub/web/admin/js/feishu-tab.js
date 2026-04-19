/*
 * Feishu admin module.
 * ASCII only.
 */
(function(global) {
  function feishuState() {
    if (!global.__feishuAdminState) {
      global.__feishuAdminState = {
        bindingsPage: 1,
        bindingsPageSize: 12,
        bindingsSearch: '',
        bindingsDebounce: null
      };
    }
    return global.__feishuAdminState;
  }

  global.loadFeishuConfig = async function loadFeishuConfig() {
    try {
      const data = await api('/api/admin/feishu/config');
      document.getElementById('feishuEnabled').checked = !!data.enabled;
      document.getElementById('feishuAppId').value = data.app_id || '';
      document.getElementById('feishuAppSecret').value = data.app_secret || '';
      global.loadFeishuBindings();
      global.loadFeishuAutoEnroll();
    } catch (err) {
      const msg = fsh('loadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.saveFeishuConfig = async function saveFeishuConfig() {
    try {
      const payload = {
        enabled: document.getElementById('feishuEnabled').checked,
        app_id: document.getElementById('feishuAppId').value.trim(),
        app_secret: document.getElementById('feishuAppSecret').value
      };
      const data = await api('/api/admin/feishu/config', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('feishuEnabled').checked = !!data.enabled;
      document.getElementById('feishuAppId').value = data.app_id || '';
      document.getElementById('feishuAppSecret').value = data.app_secret || '';
      const msg = fsh('saved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = fsh('saveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadFeishuAutoEnroll = async function loadFeishuAutoEnroll() {
    try {
      const data = await api('/api/admin/feishu/auto-enroll');
      document.getElementById('feishuAutoEnrollEnabled').checked = !!data.enabled;
      document.getElementById('feishuDepartmentId').value = data.department_id || '0';
      document.getElementById('feishuUseLark').checked = !!data.use_lark;
      document.getElementById('feishuEmployeeType').value = String(data.employee_type || 1);
    } catch (err) {
      const msg = fsh('autoEnrollLoadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.saveFeishuAutoEnroll = async function saveFeishuAutoEnroll() {
    try {
      const payload = {
        enabled: document.getElementById('feishuAutoEnrollEnabled').checked,
        department_id: document.getElementById('feishuDepartmentId').value.trim() || '0',
        use_lark: document.getElementById('feishuUseLark').checked,
        employee_type: parseInt(document.getElementById('feishuEmployeeType').value, 10) || 1
      };
      const data = await api('/api/admin/feishu/auto-enroll', { method: 'POST', body: JSON.stringify(payload) });
      document.getElementById('feishuAutoEnrollEnabled').checked = !!data.enabled;
      document.getElementById('feishuDepartmentId').value = data.department_id || '0';
      document.getElementById('feishuUseLark').checked = !!data.use_lark;
      document.getElementById('feishuEmployeeType').value = String(data.employee_type || 1);
      const msg = fsh('autoEnrollSaved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = fsh('autoEnrollSaveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadFeishuBindings = async function loadFeishuBindings(page) {
    const state = feishuState();
    if (typeof page === 'number') state.bindingsPage = page;
    try {
      const params = new URLSearchParams({ page: String(state.bindingsPage), page_size: String(state.bindingsPageSize) });
      if (state.bindingsSearch) params.set('search', state.bindingsSearch);
      const data = await api('/api/admin/feishu/bindings?' + params.toString());
      const bindings = data.bindings || [];
      const total = data.total || 0;
      const root = document.getElementById('feishuBindingsList');
      if (!root) return;
      const statsEl = document.getElementById('feishuBindingsStats');
      if (statsEl) {
        statsEl.textContent = fsh('bindingsStats', { total: String(total) });
        statsEl.style.display = total > 0 ? '' : 'none';
      }
      if (!bindings.length) {
        root.innerHTML = '<div class="hint">' + (state.bindingsSearch ? fsh('bindingsNoResults') : fsh('noBindings')) + '</div>';
      } else {
        root.innerHTML = '<div class="feishu-bind-grid">' + bindings.map(function(b) {
          const safeEmail = escapeHtml(b.email);
          const safeOid = escapeHtml(b.open_id);
          return '<div class="feishu-bind-card"><div class="fbc-email">' + safeEmail + '</div><div class="fbc-oid">' + safeOid + '</div><div style="font-size:12px;color:var(--muted)">' + escapeHtml(b.mobile || '-') + '</div><button class="btn-danger" style="height:28px;font-size:11px;padding:0 8px;margin-top:auto;align-self:flex-start" data-email="' + safeEmail + '" onclick="unbindFeishu(this.dataset.email)">' + fsh('unbind') + '</button></div>';
        }).join('') + '</div>';
      }
      const pager = document.getElementById('feishuBindingsPager');
      if (pager) {
        const totalPages = Math.ceil(total / state.bindingsPageSize);
        if (totalPages > 1) {
          pager.classList.remove('hidden');
          const meta = document.getElementById('feishuBindingsPagerMeta');
          if (meta) meta.textContent = fsh('bindingsPage', { page: String(state.bindingsPage), total: String(totalPages) });
          const prevBtn = document.getElementById('feishuBindingsPrevBtn');
          const nextBtn = document.getElementById('feishuBindingsNextBtn');
          if (prevBtn) prevBtn.disabled = state.bindingsPage <= 1;
          if (nextBtn) nextBtn.disabled = state.bindingsPage >= totalPages;
        } else {
          pager.classList.add('hidden');
        }
      }
    } catch (err) {
      const msg = fsh('bindingsLoadFailed', { error: err.message });
      setOutput(msg);
    }
  };

  global.changeFeishuBindingsPage = function changeFeishuBindingsPage(delta) {
    const state = feishuState();
    global.loadFeishuBindings(state.bindingsPage + delta);
  };

  global.onFeishuBindingsSearch = function onFeishuBindingsSearch() {
    const state = feishuState();
    state.bindingsSearch = (document.getElementById('feishuBindingsSearchInput') || {}).value || '';
    state.bindingsPage = 1;
    global.loadFeishuBindings();
  };

  global.onFeishuBindingsSearchInput = function onFeishuBindingsSearchInput() {
    const state = feishuState();
    clearTimeout(state.bindingsDebounce);
    state.bindingsDebounce = setTimeout(global.onFeishuBindingsSearch, 400);
  };

  global.unbindFeishu = async function unbindFeishu(email) {
    const state = feishuState();
    if (!confirm(fsh('unbindConfirm', { email: email }))) return;
    try {
      await api('/api/admin/feishu/bindings?email=' + encodeURIComponent(email), { method: 'DELETE' });
      const msg = fsh('unbindSuccess');
      setOutput(msg);
      showToast(msg, 'success');
      const root = document.getElementById('feishuBindingsList');
      const remaining = root ? root.querySelectorAll('.feishu-bind-card').length : 1;
      if (remaining <= 1 && state.bindingsPage > 1) state.bindingsPage -= 1;
      global.loadFeishuBindings(state.bindingsPage);
    } catch (err) {
      const msg = fsh('unbindFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };
})(window);