/*
 * Hub Center admin module.
 * ASCII only.
 */
(function(global) {
  var centerPollTimer = null;
  var CENTER_TAB_TEXT = {
    en: {
      corporateEmailDomain: 'Primary Corporate Email Domain',
      corporateEmailDomainPlaceholder: 'rapidai.tech',
      corporateEmailDomains: 'Corporate Email Domains',
      corporateEmailDomainsPlaceholder: 'rapidai.tech, subsidiary.example',
      corporateEmailDomainsHint: 'Comma or newline separated. First domain is treated as the primary route.',
      acceptPublicSignup: 'Accept Public Signup',
      acceptPublicSignupHint: 'Enable this only for the catch-all hub that should accept users outside configured enterprise domains.',
      defaultCorporateHub: 'Default Catch-all Hub',
      publicSignupEnabled: 'Enabled',
      publicSignupDisabled: 'Disabled',
      digitalEmployeeQuota: 'Digital Employee Quota',
      digitalEmployeeExpires: 'Authorization Expires',
      deNotEnabled: 'Not Enabled',
      deInactive: 'Inactive',
      deExpired: 'Expired',
      deExpiresOn: 'Expires',
      de_disabled: 'Disabled by admin',
      de_expired: 'Authorization expired',
      de_quota_zero: 'No quota allocated',
      de_not_subscribed: 'Not subscribed',
      syncPhoneRoutes: 'Sync Phone Routes',
      syncPhoneRoutesBusy: 'Syncing...',
      syncPhoneRoutesRunning: 'Syncing verified phone routes...',
      syncPhoneRoutesDone: 'Synced {count} phone route(s)',
      syncPhoneRoutesFailed: 'Sync phone routes failed: {error}'
    },
    zh: {
      corporateEmailDomain: '\u4e3b\u4f01\u4e1a\u90ae\u7bb1\u57df\u540d',
      corporateEmailDomainPlaceholder: 'rapidai.tech',
      corporateEmailDomains: '\u4f01\u4e1a\u90ae\u7bb1\u57df\u540d\u5217\u8868',
      corporateEmailDomainsPlaceholder: 'rapidai.tech, subsidiary.example',
      corporateEmailDomainsHint: '\u652f\u6301\u9017\u53f7\u6216\u6362\u884c\u5206\u9694\uff0c\u7b2c\u4e00\u4e2a\u57df\u540d\u4f5c\u4e3a\u4e3b\u8def\u7531\u57df\u540d\u3002',
      acceptPublicSignup: '\u5141\u8bb8\u6563\u6237\u6ce8\u518c',
      acceptPublicSignupHint: '\u53ea\u6709\u4f5c\u4e3a\u9ed8\u8ba4\u515c\u5e95 Hub \u65f6\u624d\u5e94\u542f\u7528\u8fd9\u4e2a\u5f00\u5173\u3002',
      defaultCorporateHub: '\u9ed8\u8ba4\u515c\u5e95 Hub',
      publicSignupEnabled: '\u5df2\u542f\u7528',
      publicSignupDisabled: '\u672a\u542f\u7528',
      digitalEmployeeQuota: '\u6570\u5b57\u5458\u5de5\u6388\u6743\u6570',
      digitalEmployeeExpires: '\u6388\u6743\u6709\u6548\u671f',
      deNotEnabled: '\u672a\u5f00\u901a',
      deInactive: '\u672a\u6fc0\u6d3b',
      deExpired: '\u5df2\u8fc7\u671f',
      deExpiresOn: '\u6709\u6548\u671f\u81f3',
      de_disabled: '\u5df2\u88ab\u7ba1\u7406\u5458\u7981\u7528',
      de_expired: '\u6388\u6743\u5df2\u8fc7\u671f',
      de_quota_zero: '\u672a\u5206\u914d\u914d\u989d',
      de_not_subscribed: '\u672a\u8ba2\u9605',
      syncPhoneRoutes: '\u540c\u6b65\u624b\u673a\u8def\u7531',
      syncPhoneRoutesBusy: '\u540c\u6b65\u4e2d...',
      syncPhoneRoutesRunning: '\u6b63\u5728\u540c\u6b65\u5df2\u9a8c\u8bc1\u624b\u673a\u8def\u7531...',
      syncPhoneRoutesDone: '\u5df2\u540c\u6b65 {count} \u6761\u624b\u673a\u8def\u7531',
      syncPhoneRoutesFailed: '\u540c\u6b65\u624b\u673a\u8def\u7531\u5931\u8d25: {error}'
    }
  };

  function centerTabText(key) {
    var lang = (global.currentLang === 'zh' || global.currentLang === 'en') ? global.currentLang : 'en';
    return (CENTER_TAB_TEXT[lang] && CENTER_TAB_TEXT[lang][key]) || CENTER_TAB_TEXT.en[key] || key;
  }

  function normalizeDomainList(value) {
    return String(value || '')
      .split(/[\n,]/)
      .map(function(item) { return item.trim(); })
      .filter(function(item) { return !!item; });
  }

  function formatDomainList(value) {
    return normalizeDomainList(value).join('\n');
  }

  function domainHeroText(data) {
    var domains = Array.isArray(data.corporate_email_domains) ? data.corporate_email_domains.filter(Boolean) : [];
    if (domains.length) return domains.join(', ');
    if (data.corporate_email_domain) return data.corporate_email_domain;
    return centerTabText('defaultCorporateHub');
  }

  function publicSignupText(enabled) {
    return enabled ? centerTabText('publicSignupEnabled') : centerTabText('publicSignupDisabled');
  }

  function looksLikeRemovedRegistration(lastError) {
    var text = String(lastError || '').toLowerCase();
    if (!text) return false;
    return text.indexOf('removed by hub center') >= 0 ||
      text.indexOf('not registered') >= 0 ||
      text.indexOf('\u5df2\u88ab hub center \u79fb\u9664') >= 0 ||
      text.indexOf('\u672a\u6ce8\u518c') >= 0;
  }

  function applyCenterTabI18n() {
    var mapping = {
      centerCorporateEmailDomainLabel: 'corporateEmailDomain',
      centerCorporateEmailDomainHeroLabel: 'corporateEmailDomain',
      centerCorporateEmailDomainsLabel: 'corporateEmailDomains',
      centerCorporateEmailDomainsHeroLabel: 'corporateEmailDomains',
      centerCorporateEmailDomainsHint: 'corporateEmailDomainsHint',
      centerAcceptPublicSignupLabel: 'acceptPublicSignup',
      centerAcceptPublicSignupHeroLabel: 'acceptPublicSignup',
      centerAcceptPublicSignupHint: 'acceptPublicSignupHint',
      centerDigitalEmployeeQuotaLabel: 'digitalEmployeeQuota',
      centerDigitalEmployeeExpiresLabel: 'digitalEmployeeExpires',
      centerSyncPhoneRoutesBtn: 'syncPhoneRoutes'
    };
    Object.keys(mapping).forEach(function(id) {
      var el = document.getElementById(id);
      if (el) el.textContent = centerTabText(mapping[id]);
    });
    var primaryInput = document.getElementById('centerCorporateEmailDomain');
    var domainsInput = document.getElementById('centerCorporateEmailDomains');
    if (primaryInput) primaryInput.placeholder = centerTabText('corporateEmailDomainPlaceholder');
    if (domainsInput) domainsInput.placeholder = centerTabText('corporateEmailDomainsPlaceholder');
  }

  function startCenterPoll() {
    if (centerPollTimer) return;
    centerPollTimer = setInterval(function() {
      if (!token() || !centerGlobalScoped()) {
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

  function centerGlobalScoped() {
    var profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !profile || String(profile.scope || '').toLowerCase() !== 'tenant';
  }

  global.renderCenterStatus = function renderCenterStatus(data) {
    var registered = !!data.registered;
    var pending = !!data.pending_confirmation;
    var disabled = !!data.disabled;
    var removed = !registered && !disabled && pending && looksLikeRemovedRegistration(data.last_error);
    var editable = !registered && !disabled;
    var detail = data.advertised_base_url ? tr('advertisedAs', { url: data.advertised_base_url }) : tr('noAdvertisedUrl');
    var statusTitle = removed ? tr('notRegistered') : disabled ? ctr('disabled') : registered ? ctr('registeredOnline') : pending ? ctr('pending') : tr('notRegistered');
    var statusHint = removed ? tr('syncMissing') : disabled ? ctr('disabledMetricHint') : registered ? ctr('registeredMetricHint') : pending ? ctr('pendingMetricHint') : tr('syncMissing');
    var domainList = Array.isArray(data.corporate_email_domains) ? data.corporate_email_domains : [];

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
    document.getElementById('centerCorporateEmailDomain').value = data.corporate_email_domain || '';
    document.getElementById('centerCorporateEmailDomains').value = domainList.length ? domainList.join('\n') : formatDomainList(data.corporate_email_domain || '');
    document.getElementById('centerCorporateEmailDomainHero').textContent = data.corporate_email_domain || centerTabText('defaultCorporateHub');
    document.getElementById('centerCorporateEmailDomainsHero').textContent = domainHeroText(data);
    document.getElementById('centerAcceptPublicSignup').checked = !!data.accept_public_signup;
    document.getElementById('centerAcceptPublicSignupHero').textContent = publicSignupText(!!data.accept_public_signup);

    // Digital Employee Authorization
    var deAuth = data.digital_employee_authorization;
    var quotaEl = document.getElementById('centerDigitalEmployeeQuotaHero');
    var expiresEl = document.getElementById('centerDigitalEmployeeExpiresHero');
    var deHeroEl = document.getElementById('digitalEmployeeHero');
    var deHintEl = document.getElementById('digitalEmployeeHint');

    // Top-level metric card
    if (deHeroEl) {
      if (!deAuth) {
        deHeroEl.textContent = '-';
      } else if (deAuth.active) {
        deHeroEl.textContent = String(deAuth.quota || 0);
      } else {
        deHeroEl.textContent = '0';
      }
    }
    if (deHintEl) {
      if (!deAuth) {
        deHintEl.textContent = tr('metricDigitalEmployeeHint');
      } else if (deAuth.active && deAuth.expires_at) {
        var hintDate = new Date(deAuth.expires_at);
        deHintEl.textContent = !isNaN(hintDate.getTime()) ? centerTabText('deExpiresOn') + ' ' + hintDate.toLocaleDateString() : tr('metricDigitalEmployeeHint');
      } else if (!deAuth.active && deAuth.reason) {
        var reasonKey = 'de_' + deAuth.reason;
        var reasonText = centerTabText(reasonKey);
        deHintEl.textContent = (reasonText !== reasonKey) ? reasonText : deAuth.reason;
      } else {
        deHintEl.textContent = tr('metricDigitalEmployeeHint');
      }
    }

    // Detail fields in registration status section
    if (quotaEl) {
      if (!deAuth) {
        quotaEl.textContent = centerTabText('deNotEnabled');
      } else if (deAuth.active) {
        quotaEl.textContent = String(deAuth.quota || 0);
      } else {
        quotaEl.textContent = String(deAuth.quota || 0) + ' (' + centerTabText('deInactive') + ')';
      }
    }
    if (expiresEl) {
      expiresEl.style.color = '';
      if (!deAuth) {
        expiresEl.textContent = centerTabText('deNotEnabled');
      } else if (deAuth.expires_at) {
        var expDate = new Date(deAuth.expires_at);
        if (!isNaN(expDate.getTime())) {
          var now = new Date();
          var expired = expDate < now;
          expiresEl.textContent = expDate.toLocaleDateString() + (expired ? ' (' + centerTabText('deExpired') + ')' : '');
          if (expired) expiresEl.style.color = '#ef4444';
        } else {
          expiresEl.textContent = deAuth.expires_at;
        }
      } else {
        expiresEl.textContent = centerTabText('deNotEnabled');
      }
    }
    document.getElementById('centerConfigForm').classList.toggle('hidden', !editable);
    document.getElementById('centerRegisteredNotice').classList.toggle('hidden', !(registered || disabled || (pending && !editable)));
    document.getElementById('centerRegisteredTitle').textContent = removed ? tr('notRegistered') : disabled ? ctr('disabled') : registered ? ctr('registeredOnline') : ctr('pending');
    document.getElementById('centerRegisteredHint').textContent = removed ? tr('syncMissing') : disabled ? ctr('disabledHint') : registered ? ctr('registeredOnlineHint') : ctr('pendingHint');
    if (pending && !removed) {
      startCenterPoll();
    } else {
      stopCenterPoll();
    }
  };

  global.saveCenterConfig = async function saveCenterConfig() {
    try {
      var corporateDomains = normalizeDomainList(document.getElementById('centerCorporateEmailDomains').value);
      var primaryDomain = document.getElementById('centerCorporateEmailDomain').value.trim();
      if (!corporateDomains.length && primaryDomain) corporateDomains = [primaryDomain];
      if (!primaryDomain && corporateDomains.length) primaryDomain = corporateDomains[0];
      var data = await api('/api/admin/center/config', {
        method: 'POST',
        body: JSON.stringify({
          base_url: document.getElementById('centerBaseURL').value.trim(),
          public_base_url: document.getElementById('centerPublicBaseURL').value.trim(),
          visibility: document.getElementById('centerVisibility').value,
          enrollment_mode: document.getElementById('centerEnrollmentMode').value,
          corporate_email_domain: primaryDomain,
          corporate_email_domains: corporateDomains,
          accept_public_signup: !!document.getElementById('centerAcceptPublicSignup').checked
        })
      });
      global.renderCenterStatus(data);
      var msg = tr('centerSaved');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      var msg = tr('centerSaveFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.registerWithCenter = async function registerWithCenter() {
    try {
      var data = await api('/api/admin/center/register', { method: 'POST', body: JSON.stringify({}) });
      global.renderCenterStatus(data);
      var msg = data.pending_confirmation ? ctr('registerSubmitted') : tr('centerRegisteredMsg');
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      var msg = tr('centerRegisterFailed', { error: err.message });
      document.getElementById('centerStatusDetail').textContent = msg;
      document.getElementById('centerStatusDetailTab').textContent = msg;
      document.getElementById('centerRegisteredNotice').classList.add('hidden');
      var actions = document.getElementById('centerRegisterActions');
      if (actions) actions.classList.remove('hidden');
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  global.syncVerifiedPhoneRoutesFromCenter = async function syncVerifiedPhoneRoutesFromCenter(button) {
    if (!centerGlobalScoped()) return;
    var original = button ? button.textContent : '';
    try {
      if (button) {
        button.disabled = true;
        button.textContent = centerTabText('syncPhoneRoutesBusy');
      }
      showToast(centerTabText('syncPhoneRoutesRunning'), 'info');
      var data = await api('/api/admin/routing/sync-verified-phone-routes', { method: 'POST' });
      var msg = centerTabText('syncPhoneRoutesDone').replace('{count}', String(data.synced_count || 0));
      setOutput(msg);
      showToast(msg, 'success');
    } catch (err) {
      var failMsg = centerTabText('syncPhoneRoutesFailed').replace('{error}', err.message);
      setOutput(failMsg);
      showToast(failMsg, 'error');
    } finally {
      if (button) {
        button.disabled = false;
        button.textContent = original || centerTabText('syncPhoneRoutes');
      }
    }
  };

  global.loadCenterStatus = async function loadCenterStatus() {
    if (!centerGlobalScoped()) {
      stopCenterPoll();
      return;
    }
    try {
      var data = await api('/api/admin/center/status');
      global.renderCenterStatus(data);
    } catch (err) {
      setOutput(tr('centerLoadFailed', { error: err.message }));
    }
  };

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'center',
      onOpen: function() {
        applyCenterTabI18n();
        if (token()) global.loadCenterStatus();
      }
    });
  }

  applyCenterTabI18n();
  global._stopCenterPoll = stopCenterPoll;
  global.addEventListener('beforeunload', stopCenterPoll);
})(window);
