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
      publicSignupDisabled: 'Disabled'
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
      publicSignupDisabled: '\u672a\u542f\u7528'
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
      centerAcceptPublicSignupHint: 'acceptPublicSignupHint'
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

  global.loadCenterStatus = async function loadCenterStatus() {
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
  global.addEventListener('beforeunload', stopCenterPoll);
})(window);