const { STORAGE_KEYS } = require('../config.js')

function readJSON(key, fallback) {
  try {
    const raw = wx.getStorageSync(key)
    if (!raw) return fallback
    if (typeof raw === 'object') return raw
    return JSON.parse(raw)
  } catch (e) {
    return fallback
  }
}

function writeJSON(key, value) {
  try {
    wx.setStorageSync(key, value)
  } catch (e) {
    console.warn('[store] write failed', key, e)
  }
}

function readSession() {
  return readJSON(STORAGE_KEYS.session, null)
}

function writeSession(session) {
  writeJSON(STORAGE_KEYS.session, session)
}

function clearSession() {
  try { wx.removeStorageSync(STORAGE_KEYS.session) } catch (e) {}
}

function readBinding() {
  return readJSON(STORAGE_KEYS.binding, null)
}

function writeBinding(binding) {
  writeJSON(STORAGE_KEYS.binding, binding)
}

function clearBinding() {
  try { wx.removeStorageSync(STORAGE_KEYS.binding) } catch (e) {}
}

/**
 * Stable device identity. The Hub treats this as the device primary key, so it
 * must survive reinstalls of the WeChat client cache but stay unique per phone.
 */
function ensureClientId() {
  let clientId = ''
  try { clientId = wx.getStorageSync(STORAGE_KEYS.clientId) || '' } catch (e) {}
  if (clientId) return clientId
  clientId = 'wxmp-' + randomHex(12)
  try { wx.setStorageSync(STORAGE_KEYS.clientId, clientId) } catch (e) {}
  return clientId
}

function randomHex(length) {
  const alphabet = '0123456789abcdef'
  let out = ''
  for (let i = 0; i < length; i++) out += alphabet[Math.floor(Math.random() * 16)]
  return out
}

function uuid() {
  return Date.now().toString(36) + '-' + randomHex(8) + '-' + randomHex(4)
}

function readServer() {
  try { return wx.getStorageSync(STORAGE_KEYS.server) || '' } catch (e) { return '' }
}

function writeServer(url) {
  try { wx.setStorageSync(STORAGE_KEYS.server, url) } catch (e) {}
}

module.exports = {
  readJSON,
  writeJSON,
  readSession,
  writeSession,
  clearSession,
  readBinding,
  writeBinding,
  clearBinding,
  ensureClientId,
  randomHex,
  uuid,
  readServer,
  writeServer
}
