/*
 * Hub Center admin module.
 * ASCII only.
 */
(function(global) {
  var centerPollTimer = null;

  function startCenterPoll() {
    if (centerPollTimer) return;
    centerPollTimer = setInterval(function() {
      if (!token()) {
        stopCenterPoll();
        return;
      }
      global.loadCenterStatus();
    }, 10000);
  }

  function stopCenterPoll() {
    if (centerPollTimer) {
      clearInterval(centerPollTimer);
      centerPollTimer = null;
    }
  }

  global.renderCenterStatus = function renderCenterStatus(data) {
    const registered = !!data.registered;
    const pending = !!data.pending_confirmation;
    const disabled = !!data.disabled;
    const detail = data.advertised_base_url ? tr('advertisedAs', { url: data.advertised_base_url }) : tr('noAdvertisedUrl');
    const statusTitle = disabled ? ctr('disabled') : registered ? ctr('registeredOnline') : pending ? ctr('pending') : tr('notRegistered');
    const statusHint = disabled ? ctr('disabledMetricHint') : registered ? ctr('registeredMetricHint') : pending ? ctr('pendingMetricHint') : tr('syncMissing');
    document.getElementById('centerStatusHero').textContent = statusTitle;
    document.getElementById('centerStatusHint').textContent = statusHint;
    document.getElementById('centerStatusDetail').textContent = detail;
    document.getElementById('centerStatusDetailTab').textContent = detail;
    document.getElementById('centerAdvertisedURL').textContent = data.advertised_base_url || tr('notConfigured');
    document.getElementById('centerAdvertisedHost').textContent = data.host || tr('na');
    document.getElementById('centerAdvertisedPort').textContent = data.port ? String(data.port) : tr('na');
    document.getElementById('visibilityHero').textContent = data.visibility === 'shared' ? tr('visibilityShared') : tr('visibilityPrivate');
    document.getElementById('modeHero').textContent = data.enrollment_mode === 'approval' ? tr('enrollmentApproval') : data.enrollment_mode === 'manual' ? tr('enrollmentManual') : tr('enrollmentOpen');
    document.getElementById('centerBaseURL').value = data.base_url || '';
    document.getElementById('centerPublicBaseURL').value = data.public_base_url || '';
    document.getElementById('centerPublicBaseURLHero').textContent = data.public_base_url || tr('notConfigured');
    document.getElementById('centerVisibility').value = data.visibility === 'shared' ? 'shared' : 'private';
    document.getElementById('centerEnrollmentMode').value = data.enrollment_mode || 'open';
    document.getElementById('centerConfigForm').classList.toggle('hidden', registered || pending || disabled);
    document.getElementById('centerRegisteredNotice').classList.toggle('hidden', !(registered || pending || disabled));
    document.getElementById('centerRegisteredTitle').textContent = disabled ? ctr('disabled') : registered ? ctr('registeredOnline') : ctr('pending');
    document.getElementById('centerRegisteredHint').textContent = disabled ? ctr('disabledHint') : registered ? ctr('registeredOnlineHint') : ctr('pendingHint');
    if (pending) {
      startCenterPoll();
    } else {
      stopCenterPoll();
    }
  };

  global.saveCenterConfig = async function saveCenterConfig() {
    try {
      const data = await api('/api/admin/center/config', {
        method: 'POST',
        body: JSON.stringify({
          base_url: document.getElementById('centerBaseURL').value.trim(),
          public_base_url: document.getElementById('centerPublicBaseURL').value.trim(),
          visibility: document.getElementById('centerVisibility').value,
          enrollment_mode: document.getElementById('centerEnrollmentMode').value
        })
      });
      global.renderCenterStatus(data);
      const msg = tr('centerSaved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = tr('centerSaveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.registerWithCenter = async function registerWithCenter() {
    try {
      const data = await api('/api/admin/center/register', { method: 'POST', body: JSON.stringify({}) });
      global.renderCenterStatus(data);
      const msg = data.pending_confirmation ? ctr('registerSubmitted') : tr('centerRegisteredMsg');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      const msg = tr('centerRegisterFailed', { error: err.message });
      document.getElementById('centerStatusDetail').textContent = msg;
      document.getElementById('centerStatusDetailTab').textContent = msg;
      document.getElementById('centerRegisteredNotice').classList.add('hidden');
      const actions = document.getElementById('centerRegisterActions');
      if (actions) actions.classList.remove('hidden');
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.loadCenterStatus = async function loadCenterStatus() {
    try {
      const data = await api('/api/admin/center/status');
      global.renderCenterStatus(data);
    } catch (err) {
      setOutput(tr('centerLoadFailed', { error: err.message }));
    }
  };

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'center',
      onOpen: function() {
        if (token()) global.loadCenterStatus();
      }
    });
  }

  global.addEventListener('beforeunload', stopCenterPoll);
})(window);
