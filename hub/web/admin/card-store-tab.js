/* Card store admin tab. ASCII only. */
(function(global) {
  const I18N = {
    en: {
      nav: 'Online Store', navDesc: 'Sell service cards and credits', title: 'Online Store', subtitle: 'Configure payment, product pricing, and sales reporting for /card_store.', reload: 'Reload', save: 'Save Store', saveSettings: 'Save Settings', open: 'Preview Storefront', manageTab: 'Store Management', paymentTab: 'Payment Settings', salesTab: 'Sales Management', manageTitle: 'Store Controls', manageDesc: 'Enable the tenant store and choose default service groups for generated cards.', paymentTitle: 'Payment', paymentDesc: 'Choose Payment FM, Alipay direct, or personal QR confirmation for card-store orders.', paymentMode: 'Payment mode', personalAdminEmails: 'Store owner confirmation email', personalInstruction: 'Personal payment instructions', alipayEnabled: 'Enable Alipay QR', wechatEnabled: 'Enable WeChat QR', alipayAppID: 'Alipay App ID', alipayGateway: 'Alipay Gateway', alipayPrivateKey: 'App Private Key', alipayPublicKey: 'Alipay Public Key', alipayPaymentMethod: 'Alipay product', alipaySubjectPrefix: 'Order subject prefix', alipayNotifyURL: 'Alipay notify URL', alipayReturnURL: 'Alipay return URL (auto)', enabled: 'Enable online store', apiBase: 'API Base URL', merchant: 'Merchant No.', accessKey: 'Access Key', payType: 'Pay Type', notifyUrl: 'Notify URL', serviceGroups: 'Default service groups', serviceGroupsHint: 'Selected {count} of {total}. Issued cards use product groups first, then these default groups, then Model Services default new-user groups.', noServiceGroups: 'No model service groups found. Create one in Model Services first.', productsTitle: 'Products', productsDesc: 'Set price and enable state for service exchange cards and pure credit cards.', price: 'Price', active: 'Enabled', salesTitle: 'Sales Statistics', salesDesc: 'Paid order totals grouped by day or month for this tenant. Sold service cards are listed below.', day: 'By day', month: 'By month', orders: 'Paid orders', revenue: 'Revenue', cards: 'Cards issued', emptySales: 'No paid sales in this period.', code: 'Code', buyer: 'Buyer', status: 'Status', issueFailed: 'Issue failed', paid: 'Paid', confirmPaid: 'Confirm paid', continuePaid: 'Complete paid order', deleteOrder: 'Delete order', deleteConfirm: 'Delete this unpaid/abandoned order?', reject: 'Reject', pendingConfirm: 'Pending confirm', pendingPayment: 'Pending payment', paymentFailed: 'Payment failed', abandoned: 'Abandoned', orderRemark: 'Remark', orderChannel: 'Channel', approveConfirm: 'Confirm received payment and issue card?', completeConfirm: 'Manually mark this order as paid and continue issuing/auto-credit flow?', rejectConfirm: 'Reject this order?', orderLabel: 'Order', amountLabel: 'Amount', paidAt: 'Paid at', redeemedAccount: 'Redeemed account', redeemedAt: 'Redeemed at', autoRedeemedAt: 'Auto credited at', autoRedeemFailed: 'Auto credit failed', notRedeemed: 'Not redeemed', qrUploaded: 'QR uploaded. Click Save Settings to apply.', saved: 'Card store saved.', saveFailed: 'Save card store failed: {error}', loadFailed: 'Load card store failed: {error}', salesLoadFailed: 'Load sales failed: {error}'
    },
    zh: {
      nav: '\u5728\u7ebf\u5546\u5e97', navDesc: '\u9500\u552e\u670d\u52a1\u5361\u548c\u70b9\u5361', title: '\u5728\u7ebf\u5546\u5e97', subtitle: '\u914d\u7f6e\u652f\u4ed8\u65b9\u5f0f\u3001/card_store \u5546\u54c1\u4ef7\u683c\u4e0e\u9500\u552e\u7edf\u8ba1\u3002', reload: '\u91cd\u65b0\u52a0\u8f7d', save: '\u4fdd\u5b58\u5546\u5e97', saveSettings: '\u4fdd\u5b58\u8bbe\u7f6e', open: '\u9884\u89c8\u524d\u53f0\u5546\u5e97', manageTab: '\u5546\u5e97\u7ba1\u7406', paymentTab: '\u652f\u4ed8\u8bbe\u7f6e', salesTab: '\u9500\u552e\u7ba1\u7406', manageTitle: '\u5546\u5e97\u63a7\u5236', manageDesc: '\u542f\u7528\u5f53\u524d\u79df\u6237\u5546\u5e97\uff0c\u5e76\u4ece\u5217\u8868\u9009\u62e9\u751f\u6210\u5361\u7684\u9ed8\u8ba4\u670d\u52a1\u7ec4\u3002', paymentTitle: '\u652f\u4ed8\u8bbe\u7f6e', paymentDesc: '\u9009\u62e9 Payment FM\u3001\u652f\u4ed8\u5b9d\u7535\u8111\u7f51\u7ad9\u652f\u4ed8\u6216\u4e2a\u4eba\u6536\u6b3e\u7801\u786e\u8ba4\u3002', paymentMode: '\u652f\u4ed8\u6a21\u5f0f', personalAdminEmails: '\u5e97\u4e3b\u786e\u8ba4\u90ae\u7bb1', personalInstruction: '\u4e2a\u4eba\u6536\u6b3e\u8bf4\u660e', alipayEnabled: '\u542f\u7528\u652f\u4ed8\u5b9d\u6536\u6b3e\u7801', wechatEnabled: '\u542f\u7528\u5fae\u4fe1\u6536\u6b3e\u7801', enabled: '\u542f\u7528\u5728\u7ebf\u5546\u5e97', apiBase: '\u63a5\u53e3\u6839\u5730\u5740', merchant: '\u5546\u6237\u53f7', accessKey: '\u63a5\u5165\u5bc6\u94a5', payType: '\u652f\u4ed8\u65b9\u5f0f', notifyUrl: '\u5f02\u6b65\u901a\u77e5\u5730\u5740', serviceGroups: '\u9ed8\u8ba4\u670d\u52a1\u7ec4', serviceGroupsHint: '\u5df2\u9009 {count}/{total}\u3002\u53d1\u5361\u65f6\u4f18\u5148\u4f7f\u7528\uff1a\u5546\u54c1\u4e13\u5c5e\u670d\u52a1\u7ec4 > \u8fd9\u91cc\u7684\u9ed8\u8ba4\u670d\u52a1\u7ec4 > \u6a21\u578b\u670d\u52a1\u9ed8\u8ba4\u65b0\u7528\u6237\u670d\u52a1\u7ec4\u3002', noServiceGroups: '\u6682\u65e0\u6a21\u578b\u670d\u52a1\u7ec4\uff0c\u8bf7\u5148\u5728\u6a21\u578b\u670d\u52a1\u4e2d\u521b\u5efa\u3002', productsTitle: '\u5546\u54c1', productsDesc: '\u8bbe\u7f6e\u670d\u52a1\u5151\u6362\u5361\u548c\u7eaf\u70b9\u5361\u7684\u4ef7\u683c\u4e0e\u542f\u7528\u72b6\u6001\u3002', price: '\u4ef7\u683c', active: '\u542f\u7528', salesTitle: '\u9500\u552e\u7edf\u8ba1', salesDesc: '\u6309\u5929\u6216\u6309\u6708\u7edf\u8ba1\u5f53\u524d\u79df\u6237\u7684\u5df2\u652f\u4ed8\u8ba2\u5355\uff0c\u4e0b\u65b9\u4ee5\u5361\u7247\u5f62\u5f0f\u5217\u51fa\u8ba2\u5355\u4e0e\u5df2\u552e\u670d\u52a1\u5361\u3002', day: '\u6309\u5929', month: '\u6309\u6708', orders: '\u5df2\u652f\u4ed8\u8ba2\u5355', revenue: '\u9500\u552e\u989d', cards: '\u53d1\u5361\u6570', emptySales: '\u8be5\u65f6\u6bb5\u6682\u65e0\u9500\u552e\u8ba2\u5355\u3002', code: '\u5151\u6362\u7801', buyer: '\u8d2d\u4e70\u8d26\u6237', status: '\u72b6\u6001', issueFailed: '\u53d1\u5361\u5931\u8d25', paid: '\u5df2\u652f\u4ed8', confirmPaid: '\u786e\u8ba4\u5230\u8d26', continuePaid: '\u5b8c\u6210\u5df2\u652f\u4ed8\u6d41\u7a0b', deleteOrder: '\u5220\u9664\u8ba2\u5355', deleteConfirm: '\u5220\u9664\u8be5\u672a\u652f\u4ed8/\u5df2\u653e\u5f03\u8ba2\u5355\uff1f', reject: '\u62d2\u7edd', pendingConfirm: '\u5f85\u786e\u8ba4', pendingPayment: '\u5f85\u652f\u4ed8', paymentFailed: '\u652f\u4ed8\u5931\u8d25', abandoned: '\u5df2\u653e\u5f03', orderRemark: '\u5907\u6ce8', orderChannel: '\u6e20\u9053', approveConfirm: '\u786e\u8ba4\u5df2\u6536\u5230\u4ed8\u6b3e\u5e76\u53d1\u5361\uff1f', completeConfirm: '\u624b\u5de5\u6807\u8bb0\u8be5\u8ba2\u5355\u4e3a\u5df2\u652f\u4ed8\uff0c\u5e76\u7ee7\u7eed\u53d1\u5361/\u81ea\u52a8\u5145\u503c\u6d41\u7a0b\uff1f', rejectConfirm: '\u62d2\u7edd\u8be5\u8ba2\u5355\uff1f', orderLabel: '\u8ba2\u5355', amountLabel: '\u91d1\u989d', paidAt: '\u652f\u4ed8\u65f6\u95f4', redeemedAccount: '\u5151\u6362\u5e10\u6237', redeemedAt: '\u5151\u6362\u65f6\u95f4', autoRedeemedAt: '\u81ea\u52a8\u5145\u503c\u65f6\u95f4', autoRedeemFailed: '\u81ea\u52a8\u5145\u503c\u5931\u8d25', notRedeemed: '\u672a\u5151\u6362', qrUploaded: '\u6536\u6b3e\u7801\u5df2\u4e0a\u4f20\uff0c\u8bf7\u70b9\u51fb\u4fdd\u5b58\u8bbe\u7f6e\u751f\u6548\u3002', saved: '\u5728\u7ebf\u5546\u5e97\u5df2\u4fdd\u5b58\u3002', saveFailed: '\u4fdd\u5b58\u5728\u7ebf\u5546\u5e97\u5931\u8d25\uff1a{error}', loadFailed: '\u52a0\u8f7d\u5728\u7ebf\u5546\u5e97\u5931\u8d25\uff1a{error}', salesLoadFailed: '\u52a0\u8f7d\u9500\u552e\u7edf\u8ba1\u5931\u8d25\uff1a{error}'
    }
  };
  const EXTRA_I18N = {
    en: { alipayAppID: 'Alipay AppID', alipayPrivateKey: 'App private key', alipayPublicKey: 'Alipay public key', alipayGateway: 'Alipay gateway', alipaySubjectPrefix: 'Subject prefix', alipayReturnURL: 'Return URL (auto)', alipayNotifyURL: 'Notify URL', alipayPaymentMethod: 'Alipay product' },
    zh: { alipayAppID: '\u652f\u4ed8\u5b9d AppID', alipayPrivateKey: '\u5e94\u7528\u79c1\u94a5', alipayPublicKey: '\u652f\u4ed8\u5b9d\u516c\u94a5', alipayGateway: '\u652f\u4ed8\u5b9d\u7f51\u5173', alipaySubjectPrefix: '\u8ba2\u5355\u6807\u9898\u524d\u7f00', alipayReturnURL: '\u540c\u6b65\u8fd4\u56de\u5730\u5740\uff08\u81ea\u52a8\uff09', alipayNotifyURL: '\u5f02\u6b65\u901a\u77e5\u5730\u5740', alipayPaymentMethod: '\u652f\u4ed8\u5b9d\u4ea7\u54c1' }
  };
  Object.assign(I18N.en, { bulkDelete: 'Delete selected', bulkDeleteSelected: 'Delete selected ({count})', bulkDeleteConfirm: 'Delete {count} selected unpaid/abandoned orders?', noSelectedOrders: 'Select unpaid orders first.', selectOrder: 'Select order' });
  Object.assign(I18N.zh, { bulkDelete: '\u6279\u91cf\u5220\u9664', bulkDeleteSelected: '\u6279\u91cf\u5220\u9664\uff08{count}\uff09', bulkDeleteConfirm: '\u5220\u9664\u5df2\u9009\u7684 {count} \u4e2a\u672a\u652f\u4ed8/\u5df2\u653e\u5f03\u8ba2\u5355\uff1f', noSelectedOrders: '\u8bf7\u5148\u9009\u62e9\u672a\u5b8c\u6210\u8ba2\u5355\u3002', selectOrder: '\u9009\u62e9\u8ba2\u5355' });
  let currentConfig = null;
  let serviceGroups = [];
  let activeSubtab = 'manage';
  let selectedSalesOrders = new Set();
  const defaultPaymentAPIBaseURL = 'https://api-4z7jye7ftfr4.zhifu.fm.it88168.com/api';
  const defaultPaymentMerchantNum = '655219576405377024';
  function defaultNotifyURL() { return global.location.origin + '/api/zhifuxpay/notify'; }
  function currentAdminTenantID() { const profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null; return profile && String(profile.scope || '').toLowerCase() === 'tenant' ? String(profile.tenant_id || '').trim() : ''; }
  function publicStoreURL() { const tenantID = currentAdminTenantID(); return tenantID ? '/card_store?tenant_id=' + encodeURIComponent(tenantID) : '/card_store'; }
  function alipayReturnPreviewURL() { const tenantID = currentAdminTenantID(); return global.location.origin + '/api/card-store/orders/{orderNo}/alipay/return' + (tenantID ? '?tenant_id=' + encodeURIComponent(tenantID) : ''); }
  function lang() { return global.currentLang === 'zh' ? 'zh' : 'en'; }
  function t(key, vars) { let text = (I18N[lang()] && I18N[lang()][key]) || I18N.en[key] || key; Object.keys(vars || {}).forEach(function(k) { text = text.replace('{' + k + '}', vars[k]); }); return text; }
  function cardStoreLabel(key, fallback) { return (I18N[lang()] && I18N[lang()][key]) || (EXTRA_I18N[lang()] && EXTRA_I18N[lang()][key]) || I18N.en[key] || EXTRA_I18N.en[key] || fallback || key; }
  function setText(id, text) { const el = document.getElementById(id); if (el) el.textContent = text; }
  function updateStorefrontPreviewLink() { const openLink = document.getElementById('cardStoreOpenLink'); if (!openLink) return; openLink.href = publicStoreURL(); openLink.title = t('open'); openLink.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M7 7h10v10"/><path d="M7 17 17 7"/><path d="M5 5h5"/><path d="M5 5v5"/></svg><span>' + t('open') + '</span>'; }
  function money(value) { return Number(value || 0).toFixed(2); }
  function dateText(value) { if (!value) return '-'; const d = new Date(value); return Number.isNaN(d.getTime()) ? value : d.toLocaleString(); }
  function jsString(value) { return String(value == null ? '' : value).replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/[\r\n]+/g, ' '); }
  function normalizeServiceGroups(data) { return (data && data.model_service_groups || []).filter(function(group) { return String(group && group.id || '').trim(); }); }
  function serviceGroupLabel(group) { const id = String(group && group.id || '').trim(); const name = String(group && group.name || '').trim(); return name && name !== id ? id + ' - ' + name : id; }
  function personalChannel(id) { const channels = currentConfig && currentConfig.personal_payment && currentConfig.personal_payment.channels || []; return channels.find(function(ch) { return String(ch.id || '') === id; }) || { id: id }; }
  function splitEmails(value) { return String(value || '').split(/[;,\s]+/).map(function(v) { return v.trim().toLowerCase(); }).filter(Boolean); }
  function setQRPreview(channel, url) { const prefix = channel === 'wechat' ? 'Wechat' : 'Alipay'; const img = document.getElementById('cardStore' + prefix + 'QRPreview'); if (!img) return; const value = String(url || '').trim(); img.style.display = value ? 'block' : 'none'; if (value) img.src = value; }
  function selectedPaymentMethods() { const mode = document.getElementById('cardStorePaymentMode'); return [mode && mode.value || 'payment_fm']; }
  function setSelectedPaymentMethods(methods) { const mode = document.getElementById('cardStorePaymentMode'); if (mode && methods && methods.length) mode.value = methods[0] || mode.value || 'payment_fm'; }
  function syncPaymentMethodSelection(preferred) { const mode = document.getElementById('cardStorePaymentMode'); if (mode && preferred) mode.value = preferred; return selectedPaymentMethods(); }
  function setFieldVisible(id, visible) { const el = document.getElementById(id); const wrap = el && el.closest('div'); if (wrap) wrap.style.display = visible ? '' : 'none'; }
  function updatePaymentModeUI() {
    const mode = document.getElementById('cardStorePaymentMode');
    const methods = syncPaymentMethodSelection();
    const hasMethod = function(value) { return methods.indexOf(value) >= 0 || (mode && mode.value === value); };
    const useFM = hasMethod('payment_fm');
    const useManual = hasMethod('personal_semimanual');
    const useAlipay = hasMethod('alipay_direct');
    ['cardStorePayType', 'cardStoreMerchantNum', 'cardStorePaymentAPIBaseURL', 'cardStoreAccessKey', 'cardStoreNotifyURL'].forEach(function(id) { setFieldVisible(id, useFM); });
    ['cardStorePersonalAdminEmails', 'cardStorePersonalInstruction', 'cardStoreAlipayEnabled', 'cardStoreWechatEnabled'].forEach(function(id) { setFieldVisible(id, useManual); });
    ['cardStoreAlipayAppID', 'cardStoreAlipayGateway', 'cardStoreAlipayPrivateKey', 'cardStoreAlipayPublicKey', 'cardStoreAlipayPaymentMethod', 'cardStoreAlipaySubjectPrefix', 'cardStoreAlipayNotifyURL', 'cardStoreAlipayReturnURL'].forEach(function(id) { setFieldVisible(id, useAlipay); });
  }
  function selectedServiceGroupIDs() { return Array.prototype.slice.call(document.querySelectorAll('[data-card-store-service-group]:checked')).map(function(input) { return input.value; }).filter(Boolean); }
  function updateServiceGroupSelectionUI() {
    const select = document.getElementById('cardStoreServiceGroups');
    const hint = document.getElementById('cardStoreServiceGroupsHint');
    if (!select) return;
    const boxes = Array.prototype.slice.call(select.querySelectorAll('[data-card-store-service-group]'));
    const count = boxes.filter(function(input) { return input.checked; }).length;
    boxes.forEach(function(input) {
      const label = input.closest('label');
      if (!label) return;
      label.style.borderColor = input.checked ? 'rgba(239,106,124,.42)' : 'rgba(31,34,48,.08)';
      label.style.background = input.checked ? '#fff0f2' : '#fff';
      label.style.boxShadow = input.checked ? '0 8px 18px rgba(239,106,124,.08)' : 'none';
    });
    if (hint) hint.textContent = boxes.length ? t('serviceGroupsHint', { count: count, total: boxes.length }) : t('noServiceGroups');
  }
  function renderServiceGroupOptions(preserveLive) {
    const select = document.getElementById('cardStoreServiceGroups');
    const hint = document.getElementById('cardStoreServiceGroupsHint');
    if (!select) return;
    const liveSelected = preserveLive ? selectedServiceGroupIDs() : [];
    const selectedSource = preserveLive ? liveSelected : (currentConfig && currentConfig.service_group_ids || []);
    const selected = new Set(selectedSource.map(function(id) { return String(id || '').trim(); }).filter(Boolean));
    const known = new Set(serviceGroups.map(function(group) { return String(group.id || '').trim().toLowerCase(); }));
    const missing = Array.prototype.slice.call(selected).filter(function(id) { return !known.has(id.toLowerCase()); }).map(function(id) { return { id: id, name: id }; });
    const groups = serviceGroups.concat(missing);
    select.innerHTML = groups.map(function(group) { const id = String(group.id || '').trim(); const checked = selected.has(id) ? ' checked' : ''; return '<label title="' + escapeHtml(serviceGroupLabel(group)) + '" style="display:flex;align-items:center;gap:10px;margin:0;padding:10px 12px;border:1px solid rgba(31,34,48,.08);border-radius:10px;background:#fff;cursor:pointer;transition:border-color .15s ease,background .15s ease,box-shadow .15s ease"><input type="checkbox" data-card-store-service-group value="' + escapeHtml(id) + '"' + checked + ' style="width:17px;height:17px"><span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:700">' + escapeHtml(serviceGroupLabel(group)) + '</span></label>'; }).join('');
    select.onchange = updateServiceGroupSelectionUI;
    updateServiceGroupSelectionUI();
  }
  function productIconHTML(product) {
    const isCredit = String(product && product.kind || '').toLowerCase() === 'credits';
    const isTest = String(product && product.id || '').toLowerCase().indexOf('test') >= 0;
    const color = isTest ? '#a855f7' : (isCredit ? '#0ea5e9' : '#16a34a');
    const path = isTest ? '<path d="M10 3v4l-3.8 6.4A4 4 0 0 0 9.6 19h4.8a4 4 0 0 0 3.4-5.6L14 7V3"/><path d="M9 11h6"/>' : (isCredit ? '<circle cx="12" cy="12" r="7"/><path d="M9 10.5h5.2a2 2 0 0 1 0 4H9"/><path d="M11 8v8"/>' : '<rect x="5" y="6" width="14" height="12" rx="3"/><path d="M8 10h8"/><path d="M8 14h5"/>');
    return '<span aria-hidden="true" style="width:34px;height:34px;border-radius:8px;background:' + color + '14;color:' + color + ';display:inline-flex;align-items:center;justify-content:center;flex:0 0 auto"><svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + path + '</svg></span>';
  }
  function applyCardStoreI18N() {
    setText('navCardStore', t('nav')); setText('navCardStoreDesc', t('navDesc'));
    setText('cardStoreTitle', t('title')); setText('cardStoreSubtitle', t('subtitle'));
    setText('cardStoreReloadBtn', t('reload')); setText('cardStoreSaveBtn', t('save')); setText('cardStorePaymentSaveBtn', t('saveSettings')); updateStorefrontPreviewLink();
    setText('cardStoreManageSubtab', t('manageTab')); setText('cardStorePaymentSubtab', t('paymentTab')); setText('cardStoreSalesSubtab', t('salesTab'));
    setText('cardStoreManageTitle', t('manageTitle')); setText('cardStoreManageDesc', t('manageDesc'));
    setText('cardStorePaymentTitle', t('paymentTitle')); setText('cardStorePaymentDesc', t('paymentDesc'));
    setText('cardStorePaymentModeLabel', cardStoreLabel('paymentMode', 'Payment mode')); setText('cardStorePersonalAdminEmailsLabel', cardStoreLabel('personalAdminEmails', 'Store owner confirmation email'));
    setText('cardStorePersonalInstructionLabel', cardStoreLabel('personalInstruction', 'Personal payment instructions')); setText('cardStoreAlipayEnabledLabel', cardStoreLabel('alipayEnabled', 'Enable Alipay QR')); setText('cardStoreWechatEnabledLabel', cardStoreLabel('wechatEnabled', 'Enable WeChat QR'));
    setText('cardStoreAlipayAppIDLabel', cardStoreLabel('alipayAppID', 'Alipay App ID')); setText('cardStoreAlipayGatewayLabel', cardStoreLabel('alipayGateway', 'Alipay Gateway')); setText('cardStoreAlipayPrivateKeyLabel', cardStoreLabel('alipayPrivateKey', 'App Private Key')); setText('cardStoreAlipayPublicKeyLabel', cardStoreLabel('alipayPublicKey', 'Alipay Public Key'));
    setText('cardStoreAlipayPaymentMethodLabel', cardStoreLabel('alipayPaymentMethod', 'Alipay product')); setText('cardStoreAlipaySubjectPrefixLabel', cardStoreLabel('alipaySubjectPrefix', 'Order subject prefix')); setText('cardStoreAlipayNotifyURLLabel', cardStoreLabel('alipayNotifyURL', 'Alipay notify URL')); setText('cardStoreAlipayReturnURLLabel', cardStoreLabel('alipayReturnURL', 'Alipay return URL'));
    setText('cardStoreEnabledLabel', t('enabled')); setText('cardStorePaymentAPIBaseURLLabel', t('apiBase'));
    setText('cardStoreMerchantNumLabel', t('merchant')); setText('cardStoreAccessKeyLabel', t('accessKey'));
    setText('cardStorePayTypeLabel', t('payType')); setText('cardStoreNotifyURLLabel', t('notifyUrl'));
    setText('cardStoreServiceGroupsLabel', t('serviceGroups')); setText('cardStoreProductsTitle', t('productsTitle'));
    setText('cardStoreProductsDesc', t('productsDesc')); setText('cardStoreSalesTitle', t('salesTitle'));
    setText('cardStoreSalesDesc', t('salesDesc')); setText('cardStoreSalesPeriodDay', t('day')); setText('cardStoreSalesPeriodMonth', t('month'));
    setText('cardStoreSalesReloadBtn', t('reload')); setText('cardStoreSalesOrdersLabel', t('orders')); setText('cardStoreSalesRevenueLabel', t('revenue')); setText('cardStoreSalesCardsLabel', t('cards'));
    updateBulkDeleteButton();
    updateStorefrontPreviewLink();
    renderServiceGroupOptions(true);
    renderProductsEditor();
  }
  function switchCardStoreSubtab(name) {
    activeSubtab = name === 'sales' ? 'sales' : (name === 'payment' ? 'payment' : 'manage');
    const manage = document.getElementById('cardStoreManagePane'); const payment = document.getElementById('cardStorePaymentPane'); const sales = document.getElementById('cardStoreSalesPane');
    const manageBtn = document.getElementById('cardStoreManageSubtab'); const paymentBtn = document.getElementById('cardStorePaymentSubtab'); const salesBtn = document.getElementById('cardStoreSalesSubtab');
    if (manage) manage.classList.toggle('hidden', activeSubtab !== 'manage');
    if (payment) payment.classList.toggle('hidden', activeSubtab !== 'payment');
    if (sales) sales.classList.toggle('hidden', activeSubtab !== 'sales');
    if (manageBtn) manageBtn.classList.toggle('active', activeSubtab === 'manage');
    if (paymentBtn) paymentBtn.classList.toggle('active', activeSubtab === 'payment');
    if (salesBtn) salesBtn.classList.toggle('active', activeSubtab === 'sales');
    if (activeSubtab === 'sales') loadCardStoreSales();
  }
  function fillConfig(cfg) {
    currentConfig = cfg || {};
    document.getElementById('cardStoreEnabled').checked = !!currentConfig.enabled;
    document.getElementById('cardStorePaymentMode').value = currentConfig.payment_mode || 'payment_fm';
    setSelectedPaymentMethods(currentConfig.payment_methods || [currentConfig.payment_mode || 'payment_fm']);
    document.getElementById('cardStorePaymentAPIBaseURL').value = currentConfig.payment_api_base_url || defaultPaymentAPIBaseURL;
    document.getElementById('cardStoreMerchantNum').value = currentConfig.merchant_num || defaultPaymentMerchantNum;
    document.getElementById('cardStoreAccessKey').value = '';
    document.getElementById('cardStoreAccessKey').placeholder = '********';
    document.getElementById('cardStorePayType').value = currentConfig.pay_type || 'aloop';
    document.getElementById('cardStoreNotifyURL').value = currentConfig.notify_url || defaultNotifyURL();
    const alipayDirect = currentConfig.alipay_direct || {};
    document.getElementById('cardStoreAlipayAppID').value = alipayDirect.app_id || '';
    document.getElementById('cardStoreAlipayGateway').value = alipayDirect.gateway_url || 'https://openapi.alipay.com/gateway.do';
    document.getElementById('cardStoreAlipayPrivateKey').value = alipayDirect.private_key || '';
    document.getElementById('cardStoreAlipayPrivateKey').placeholder = '********';
    document.getElementById('cardStoreAlipayPublicKey').value = alipayDirect.alipay_public_key || '';
    document.getElementById('cardStoreAlipayPaymentMethod').value = alipayDirect.payment_method || 'page';
    document.getElementById('cardStoreAlipaySubjectPrefix').value = alipayDirect.subject_prefix || 'MaClaw Hub';
    document.getElementById('cardStoreAlipayNotifyURL').value = alipayDirect.notify_url || (global.location.origin + '/api/card-store/payment/notify');
    const alipayReturnInput = document.getElementById('cardStoreAlipayReturnURL');
    alipayReturnInput.value = alipayReturnPreviewURL();
    alipayReturnInput.readOnly = true;
    alipayReturnInput.title = alipayReturnInput.value;
    const personal = currentConfig.personal_payment || {};
    document.getElementById('cardStorePersonalAdminEmails').value = (personal.admin_emails || []).join(', ');
    document.getElementById('cardStorePersonalInstruction').value = personal.instruction || '';
    const alipay = personalChannel('alipay');
    const wechat = personalChannel('wechat');
    document.getElementById('cardStoreAlipayEnabled').checked = !!alipay.enabled;
    document.getElementById('cardStoreAlipayPayee').value = alipay.payee || '';
    document.getElementById('cardStoreAlipayQR').value = alipay.image_url || '';
    setQRPreview('alipay', alipay.image_url || '');
    document.getElementById('cardStoreAlipayUserID').value = alipay.alipay_user_id || '';
    document.getElementById('cardStoreWechatEnabled').checked = !!wechat.enabled;
    document.getElementById('cardStoreWechatPayee').value = wechat.payee || '';
    document.getElementById('cardStoreWechatQR').value = wechat.image_url || '';
    setQRPreview('wechat', wechat.image_url || '');
    document.getElementById('cardStoreAlipayQR').oninput = function() { setQRPreview('alipay', this.value); };
    document.getElementById('cardStoreWechatQR').oninput = function() { setQRPreview('wechat', this.value); };
    document.getElementById('cardStorePaymentMode').onchange = function() { syncPaymentMethodSelection(document.getElementById('cardStorePaymentMode').value); updatePaymentModeUI(); };
    updatePaymentModeUI();
    updateStorefrontPreviewLink();
    renderServiceGroupOptions(false);
    renderProductsEditor();
  }
  function renderProductsEditor() {
    const root = document.getElementById('cardStoreProductsEditor');
    if (!root || !currentConfig) return;
    root.innerHTML = '';
    (currentConfig.products || []).forEach(function(product) {
      const row = document.createElement('div');
      row.className = 'item'; row.style.padding = '10px 12px';
      const desc = product.description ? '<div class="item-meta">' + escapeHtml(product.description) + '</div>' : '';
      row.innerHTML = '<div class="grid3" style="grid-template-columns:auto minmax(0,1fr) 120px;align-items:center;gap:12px"><label style="display:flex;gap:8px;align-items:center;margin:0;white-space:nowrap"><input type="checkbox" data-card-store-enabled="' + escapeHtml(product.id) + '"><span>' + t('active') + '</span></label><div style="display:flex;gap:10px;align-items:center;min-width:0">' + productIconHTML(product) + '<div style="min-width:0"><label style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(product.label || product.id) + '</label><div class="item-meta">' + escapeHtml(product.kind || '') + ' / ' + escapeHtml(String(product.duration_days || 0)) + ' days / ' + escapeHtml(String(product.credits || 0)) + ' credits</div>' + desc + '</div></div><div><label>' + t('price') + '</label><input type="number" min="0" step="0.01" data-card-store-price="' + escapeHtml(product.id) + '"></div></div>';
      root.appendChild(row);
      row.querySelector('[data-card-store-enabled]').checked = !!product.enabled;
      row.querySelector('[data-card-store-price]').value = product.price || 0;
    });
  }
  async function loadCardStoreConfig() {
    try { const cfg = await api('/api/admin/card-store/config'); let services = null; try { services = await api('/api/admin/llm/services?include_cards=false'); } catch (_) { services = null; } serviceGroups = normalizeServiceGroups(services); fillConfig(cfg); if (activeSubtab === 'sales') loadCardStoreSales(); }
    catch (err) { showToast(t('loadFailed', { error: err.message }), 'error'); }
  }
  async function saveCardStoreConfig(scope) {
    scope = scope === 'payment' ? 'payment' : 'store';
    const products = (currentConfig && currentConfig.products || []).map(function(product) {
      const price = document.querySelector('[data-card-store-price="' + CSS.escape(product.id) + '"]');
      const enabled = document.querySelector('[data-card-store-enabled="' + CSS.escape(product.id) + '"]');
      return Object.assign({}, product, { price: Number(price && price.value || 0), enabled: !!(enabled && enabled.checked) });
    });
    if (scope === 'store') {
      const storePayload = Object.assign({}, currentConfig || {}, { enabled: document.getElementById('cardStoreEnabled').checked, service_group_ids: selectedServiceGroupIDs(), products: products });
      try { fillConfig(await api('/api/admin/card-store/config?scope=store', { method: 'PUT', body: JSON.stringify(storePayload) })); showToast(t('saved'), 'success'); }
      catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
      return;
    }
    const methods = syncPaymentMethodSelection();
    const useManualPayment = methods.indexOf('personal_semimanual') >= 0;
    try {
      if (useManualPayment && document.getElementById('cardStoreAlipayEnabled').checked) await uploadCardStoreQRFile('alipay');
      if (useManualPayment && document.getElementById('cardStoreWechatEnabled').checked) await uploadCardStoreQRFile('wechat');
    } catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); return; }
    const personalPayment = { admin_emails: splitEmails(document.getElementById('cardStorePersonalAdminEmails').value), instruction: document.getElementById('cardStorePersonalInstruction').value, channels: [
      { id: 'alipay', label: 'Alipay', enabled: document.getElementById('cardStoreAlipayEnabled').checked, payee: document.getElementById('cardStoreAlipayPayee').value, image_url: document.getElementById('cardStoreAlipayQR').value, alipay_user_id: document.getElementById('cardStoreAlipayUserID').value, deep_link_mode: 'to_account' },
      { id: 'wechat', label: 'WeChat Pay', enabled: document.getElementById('cardStoreWechatEnabled').checked, payee: document.getElementById('cardStoreWechatPayee').value, image_url: document.getElementById('cardStoreWechatQR').value }
    ] };
    if (scope === 'payment' && methods.indexOf('alipay_direct') >= 0) {
      const required = ['cardStoreAlipayAppID', 'cardStoreAlipayPrivateKey', 'cardStoreAlipayPublicKey'];
      for (let i = 0; i < required.length; i++) {
        const field = document.getElementById(required[i]);
        if (field && !String(field.value || '').trim() && !String(field.placeholder || '').includes('*')) { field.focus(); showToast(t('saveFailed', { error: 'Alipay AppID and keys are required.' }), 'error'); return; }
      }
    }
    const alipayDirect = Object.assign({}, currentConfig && currentConfig.alipay_direct || {}, { app_id: document.getElementById('cardStoreAlipayAppID').value, gateway_url: document.getElementById('cardStoreAlipayGateway').value, private_key: document.getElementById('cardStoreAlipayPrivateKey').value, alipay_public_key: document.getElementById('cardStoreAlipayPublicKey').value, payment_method: 'page', subject_prefix: document.getElementById('cardStoreAlipaySubjectPrefix').value, notify_url: document.getElementById('cardStoreAlipayNotifyURL').value, return_url: '' });
    const payload = Object.assign({}, currentConfig || {}, { enabled: document.getElementById('cardStoreEnabled').checked, payment_mode: document.getElementById('cardStorePaymentMode').value, payment_methods: methods, payment_api_base_url: document.getElementById('cardStorePaymentAPIBaseURL').value, merchant_num: document.getElementById('cardStoreMerchantNum').value, access_key: document.getElementById('cardStoreAccessKey').value, pay_type: document.getElementById('cardStorePayType').value, notify_url: document.getElementById('cardStoreNotifyURL').value, personal_payment: personalPayment, alipay_direct: alipayDirect, service_group_ids: selectedServiceGroupIDs(), products: products });
    try { fillConfig(await api('/api/admin/card-store/config?scope=payment', { method: 'PUT', body: JSON.stringify(payload) })); showToast(t('saved'), 'success'); }
    catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
  }
  async function loadCardStoreSales() {
    const period = document.getElementById('cardStoreSalesPeriod') && document.getElementById('cardStoreSalesPeriod').value || 'day';
    try { renderCardStoreSales(await api('/api/admin/card-store/sales?period=' + encodeURIComponent(period))); }
    catch (err) { showToast(t('salesLoadFailed', { error: err.message }), 'error'); }
  }
  async function uploadCardStoreQRFile(channel) {
    const prefix = channel === 'wechat' ? 'Wechat' : 'Alipay';
    const input = document.getElementById('cardStore' + prefix + 'QRFile');
    const target = document.getElementById('cardStore' + prefix + 'QR');
    const file = input && input.files && input.files[0];
    if (!file) return String(target && target.value || '').trim();
    const form = new FormData();
    form.append('channel', channel);
    form.append('file', file);
    const headers = {};
    const authToken = typeof global.token === 'function' ? global.token() : '';
    if (authToken) headers.Authorization = 'Bearer ' + authToken;
    const res = await fetch('/api/admin/card-store/payment-qr/upload', { method: 'POST', headers: headers, body: form });
    const body = await res.json().catch(function() { return {}; });
    if (!res.ok) throw new Error(body.message || res.statusText || 'Upload failed');
    const imageURL = body.image_url || body.url || '';
    if (!imageURL) throw new Error('Upload did not return QR image URL');
    if (target) target.value = imageURL;
    setQRPreview(channel, imageURL);
    if (input) input.value = '';
    return imageURL;
  }
  async function uploadCardStoreQR(channel) {
    const prefix = channel === 'wechat' ? 'Wechat' : 'Alipay';
    const input = document.getElementById('cardStore' + prefix + 'QRFile');
    const target = document.getElementById('cardStore' + prefix + 'QR');
    const file = input && input.files && input.files[0];
    if (!file && !(target && target.value)) { showToast(t('saveFailed', { error: 'Choose a QR image first.' }), 'error'); return; }
    try { await uploadCardStoreQRFile(channel); showToast(t('qrUploaded'), 'success'); }
    catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
  }
  function renderCardStoreSales(data) {
    selectedSalesOrders = new Set();
    updateBulkDeleteButton();
    setText('cardStoreSalesOrders', String(data.total_orders || 0)); setText('cardStoreSalesRevenue', money(data.total_revenue)); setText('cardStoreSalesCards', String(data.total_cards || 0));
    const root = document.getElementById('cardStoreSalesRows'); if (!root) return; root.innerHTML = '';
    const rows = data.rows || [];
    rows.slice().reverse().forEach(function(row) {
      if (!row.orders && !row.revenue && !row.cards) return;
      const item = document.createElement('div'); item.className = 'item'; item.style.padding = '10px 12px';
      item.innerHTML = '<div class="grid4" style="align-items:center"><strong>' + escapeHtml(row.bucket) + '</strong><div><div class="item-meta">' + t('orders') + '</div><strong>' + Number(row.orders || 0) + '</strong></div><div><div class="item-meta">' + t('revenue') + '</div><strong>' + money(row.revenue) + '</strong></div><div><div class="item-meta">' + t('cards') + '</div><strong>' + Number(row.cards || 0) + '</strong></div></div>';
      root.appendChild(item);
    });
    if (!root.children.length) root.innerHTML = '<div class="hint">' + t('emptySales') + '</div>';
    renderSoldCards(data.cards || []);
  }
  function renderSoldCards(cards) {
    const root = document.getElementById('cardStoreSoldCards'); if (!root) return; root.innerHTML = '';
    (cards || []).forEach(function(card) {
      const item = document.createElement('div'); item.className = 'item'; item.style.padding = '12px 14px';
      const redeemed = card.redeemed_email ? escapeHtml(card.redeemed_email) : t('notRedeemed');
      const rawStatus = String(card.status || '').toLowerCase();
      const statusText = cardStoreOrderStatusText(rawStatus);
      const selectable = cardStoreCanBulkDeleteStatus(rawStatus);
      const checkbox = selectable ? '<label title="' + escapeHtml(t('selectOrder')) + '" style="position:absolute;right:12px;top:12px;display:inline-flex;align-items:center;justify-content:center;width:30px;height:30px;border:1px solid rgba(31,34,48,.12);border-radius:8px;background:#fff;box-shadow:0 6px 18px rgba(15,23,42,.08);cursor:pointer"><input type="checkbox" data-card-store-order-select value="' + escapeHtml(card.order_no || '') + '" onchange="toggleCardStoreOrderSelection(this)" style="width:17px;height:17px;margin:0"></label>' : '';
      const detail = card.message || card.mail_error || '';
      const autoLine = card.auto_redeemed_at ? '<div class="item-meta">' + t('autoRedeemedAt') + ': ' + escapeHtml(dateText(card.auto_redeemed_at)) + '</div>' : (card.auto_redeem_error ? '<div class="item-meta">' + t('autoRedeemFailed') + ': ' + escapeHtml(card.auto_redeem_error) + '</div>' : '');
      const actions = cardStoreOrderActionsHTML(card, rawStatus);
      item.style.position = selectable ? 'relative' : item.style.position;
      item.innerHTML = checkbox + '<div class="item-title" style="padding-right:' + (selectable ? '34px' : '0') + '">' + escapeHtml(card.product_label || card.product_id || '-') + '</div><div class="item-meta">' + t('buyer') + ': ' + escapeHtml(card.email || '-') + '</div><div class="item-meta">' + t('status') + ': ' + escapeHtml(statusText) + '</div>' + (card.pay_code ? '<div class="item-meta">' + t('orderRemark') + ': <strong>' + escapeHtml(card.pay_code) + '</strong></div>' : '') + (card.pay_channel_label ? '<div class="item-meta">' + t('orderChannel') + ': ' + escapeHtml(card.pay_channel_label) + '</div>' : '') + (detail ? '<div class="item-meta">' + escapeHtml(detail) + '</div>' : '') + '<div class="mono" style="margin-top:8px;padding:8px 10px">' + t('code') + ': ' + escapeHtml(card.code || '-') + '</div><div class="grid2" style="margin-top:8px"><div><div class="item-meta">' + t('price') + '</div><strong>' + money(card.amount) + '</strong></div><div><div class="item-meta">credits</div><strong>' + Number(card.credits || 0) + '</strong></div></div><div class="item-meta" style="margin-top:8px">' + t('paidAt') + ': ' + escapeHtml(dateText(card.paid_at)) + '</div>' + autoLine + '<div class="item-meta">' + t('redeemedAccount') + ': ' + redeemed + '</div><div class="item-meta">' + t('redeemedAt') + ': ' + escapeHtml(card.redeemed_at ? dateText(card.redeemed_at) : '-') + '</div><div class="item-meta">' + escapeHtml(card.order_no || '') + '</div>' + actions;
      root.appendChild(item);
    });
  }
  function cardStoreCanBulkDeleteStatus(status) { return status === 'created' || status === 'payment_started' || status === 'payment_failed' || status === 'personal_created' || status === 'personal_opened' || status === 'personal_rejected'; }
  function updateBulkDeleteButton() {
    const btn = document.getElementById('cardStoreBulkDeleteBtn');
    if (!btn) return;
    const count = selectedSalesOrders.size;
    btn.textContent = count ? t('bulkDeleteSelected', { count: count }) : t('bulkDelete');
    btn.disabled = count === 0;
  }
  function toggleCardStoreOrderSelection(input) {
    const orderNo = String(input && input.value || '').trim();
    if (!orderNo) return;
    if (input.checked) selectedSalesOrders.add(orderNo); else selectedSalesOrders.delete(orderNo);
    updateBulkDeleteButton();
  }
  function cardStoreOrderStatusText(status) {
    if (status === 'issue_failed') return t('issueFailed');
    if (status === 'personal_opened') return t('pendingConfirm');
    if (status === 'payment_failed') return t('paymentFailed');
    if (status === 'personal_rejected') return t('abandoned');
    if (status === 'created' || status === 'payment_started' || status === 'personal_created') return t('pendingPayment');
    if (status === 'paid') return t('paid');
    return status || '-';
  }
  function cardStoreOrderActionsHTML(card, status) {
    const orderNo = jsString(card.order_no || '');
    if (status === 'personal_opened') {
      return '<div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" onclick="approveCardStoreOrder(\'' + orderNo + '\',' + Number(card.amount || 0) + ',\'' + jsString(card.pay_code || '') + '\')">' + t('confirmPaid') + '</button><button class="btn-ghost" type="button" onclick="rejectCardStoreOrder(\'' + orderNo + '\')">' + t('reject') + '</button></div>';
    }
    if (status === 'created' || status === 'payment_started' || status === 'payment_failed' || status === 'personal_created') {
      return '<div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" onclick="completeCardStoreOrder(\'' + orderNo + '\',' + Number(card.amount || 0) + ')">' + t('continuePaid') + '</button><button class="btn-ghost" type="button" onclick="deleteCardStoreOrder(\'' + orderNo + '\')">' + t('deleteOrder') + '</button></div>';
    }
    if (status === 'personal_rejected') {
      return '<div class="actions" style="margin-top:10px"><button class="btn-ghost" type="button" onclick="deleteCardStoreOrder(\'' + orderNo + '\')">' + t('deleteOrder') + '</button></div>';
    }
    return '';
  }
  async function approveCardStoreOrder(orderNo, amount, payCode) {
    const detail = t('amountLabel') + ': ' + money(amount) + (payCode ? '\n' + t('orderRemark') + ': ' + payCode : '') + '\n' + t('orderLabel') + ': ' + orderNo;
    if (!orderNo || !global.confirm(t('approveConfirm') + '\n\n' + detail)) return;
    try { await api('/api/admin/card-store/orders/' + encodeURIComponent(orderNo) + '/approve', { method: 'POST', body: JSON.stringify({}) }); showToast(t('saved'), 'success'); loadCardStoreSales(); }
    catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
  }
  async function rejectCardStoreOrder(orderNo) {
    if (!orderNo || !global.confirm(t('rejectConfirm'))) return;
    try { await api('/api/admin/card-store/orders/' + encodeURIComponent(orderNo) + '/reject', { method: 'POST', body: JSON.stringify({ note: 'Rejected by store owner' }) }); showToast(t('saved'), 'success'); loadCardStoreSales(); }
    catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
  }
  async function completeCardStoreOrder(orderNo, amount) {
    const detail = t('amountLabel') + ': ' + money(amount) + '\n' + t('orderLabel') + ': ' + orderNo;
    if (!orderNo || !global.confirm(t('completeConfirm') + '\n\n' + detail)) return;
    try { await api('/api/admin/card-store/orders/' + encodeURIComponent(orderNo) + '/complete', { method: 'POST', body: JSON.stringify({ amount: Number(amount || 0) }) }); showToast(t('saved'), 'success'); loadCardStoreSales(); }
    catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
  }
  async function deleteCardStoreOrder(orderNo) {
    if (!orderNo || !global.confirm(t('deleteConfirm'))) return;
    try { await api('/api/admin/card-store/orders/' + encodeURIComponent(orderNo), { method: 'DELETE' }); showToast(t('saved'), 'success'); loadCardStoreSales(); }
    catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); }
  }
  async function deleteSelectedCardStoreOrders() {
    const orderNos = Array.from(selectedSalesOrders).filter(Boolean);
    if (!orderNos.length) { showToast(t('noSelectedOrders'), 'error'); return; }
    if (!global.confirm(t('bulkDeleteConfirm', { count: orderNos.length }))) return;
    try {
      for (let i = 0; i < orderNos.length; i++) {
        await api('/api/admin/card-store/orders/' + encodeURIComponent(orderNos[i]), { method: 'DELETE' });
      }
      selectedSalesOrders = new Set();
      showToast(t('saved'), 'success');
      loadCardStoreSales();
    } catch (err) { showToast(t('saveFailed', { error: err.message }), 'error'); loadCardStoreSales(); }
  }
  if (global.AdminTabRegistry) {
    global.AdminTabRegistry.registerTab({ id: 'cardstore', title: () => t('title'), subtitle: () => t('subtitle'), onOpen: loadCardStoreConfig });
    global.AdminTabRegistry.onLanguageChange(applyCardStoreI18N);
  }
  global.adminTenantOnlyTabs = Object.assign({}, global.adminTenantOnlyTabs || {}, { cardstore: true });
  Object.assign(global, { loadCardStoreConfig, saveCardStoreConfig, switchCardStoreSubtab, loadCardStoreSales, approveCardStoreOrder, rejectCardStoreOrder, completeCardStoreOrder, deleteCardStoreOrder, deleteSelectedCardStoreOrders, toggleCardStoreOrderSelection, uploadCardStoreQR });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', applyCardStoreI18N); else applyCardStoreI18N();
})(window);
