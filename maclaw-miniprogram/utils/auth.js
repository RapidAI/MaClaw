const { request } = require('./request.js')

function normalizeHub(base) {
  return String(base || '').trim().replace(/\/+$/, '')
}

function sendEmailCode(hubUrl, email, tenantId) {
  return request({
    hubUrl,
    path: '/api/miniprogram/auth/email/send-code',
    method: 'POST',
    data: tenantId ? { email, tenant_id: tenantId } : { email }
  })
}

function verifyEmailCode(hubUrl, email, code, clientId) {
  return request({
    hubUrl,
    path: '/api/miniprogram/auth/email/verify',
    method: 'POST',
    data: { email, verify_code: code, client_id: clientId }
  }).then(buildSession)
}

function sendPhoneCode(hubUrl, phoneNumber, tenantId) {
  return request({
    hubUrl,
    path: '/api/miniprogram/auth/phone/send-code',
    method: 'POST',
    data: tenantId ? { phone_number: phoneNumber, tenant_id: tenantId } : { phone_number: phoneNumber }
  })
}

function verifyPhoneCode(hubUrl, phoneNumber, code, clientId) {
  return request({
    hubUrl,
    path: '/api/miniprogram/auth/phone/verify',
    method: 'POST',
    data: { phone_number: phoneNumber, verify_code: code, client_id: clientId }
  }).then(buildSession)
}

/**
 * Turns a verify response into the locally persisted session. hubUrl always
 * comes from the Hub that authenticated us, so subsequent gateway traffic goes
 * to the node that actually owns the account.
 */
function buildSession(payload) {
  if (!payload || !payload.access_token) {
    const err = new Error(payload && payload.message ? payload.message : '登录失败：服务端未返回令牌')
    err.code = (payload && payload.error) || 'NO_TOKEN'
    throw err
  }
  const hub = payload.hub || {}
  return {
    hubUrl: normalizeHub(payload.hub_url || hub.base_url || hub.url),
    hubId: payload.hub_id || hub.id || '',
    tenantId: payload.tenant_id || '',
    accessToken: payload.access_token,
    userId: payload.user_id || '',
    sn: payload.sn || '',
    email: payload.email || '',
    expiresAt: Date.now() + (payload.expires_in ? payload.expires_in * 1000 : 30 * 24 * 3600 * 1000)
  }
}

/**
 * Revalidates a persisted session. Returns null when the Hub no longer
 * accepts the token, so callers can drop it and show sign-in again.
 */
function revalidateSession(hubUrl, accessToken) {
  return request({
    hubUrl,
    path: '/api/miniprogram/session',
    method: 'GET',
    token: accessToken,
    tolerateStatus: [401, 403]
  }).then(payload => {
    if (!payload || payload.ok !== true) return null
    return {
      hubId: payload.hub_id || '',
      tenantId: payload.tenant_id || '',
      userId: payload.user_id || '',
      sn: payload.sn || '',
      email: payload.email || ''
    }
  }).catch(err => {
    if (err && (err.code === 'UNAUTHORIZED' || err.code === 'TOKEN_EXPIRED')) return null
    throw err
  })
}

module.exports = {
  normalizeHub,
  sendEmailCode,
  verifyEmailCode,
  sendPhoneCode,
  verifyPhoneCode,
  revalidateSession
}
