const { DEFAULT_BOOTSTRAP_HUB, STORAGE_KEYS } = require('./config.js')
const { readSession, writeSession, clearSession, readBinding, clearBinding } = require('./utils/store.js')
const { revalidateSession } = require('./utils/auth.js')

App({
  globalData: {
    // bootstrap server used before we know which Hub owns the account
    hubUrl: '',
    session: null,
    binding: null,
    // bootSessionId is regenerated once per app launch: the GUI uses it to tell
    // a fresh handshake from a reconnect of the same boot.
    bootSessionId: 'boot-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10)
  },

  onLaunch() {
    const server = wx.getStorageSync(STORAGE_KEYS.server)
    this.globalData.hubUrl = (server || DEFAULT_BOOTSTRAP_HUB).replace(/\/+$/, '')
    this.globalData.session = readSession()
    this.globalData.binding = readBinding()
  },

  /**
   * Returns the stored session when it still validates against its Hub.
   * A stale or revoked token drops the local session so the caller can send
   * the user back to sign-in.
   */
  verifySession() {
    const session = readSession()
    if (!session || !session.hubUrl || !session.accessToken) return Promise.resolve(null)

    return revalidateSession(session.hubUrl, session.accessToken).then(fresh => {
      if (!fresh) {
        clearSession()
        this.globalData.session = null
        return null
      }
      const merged = Object.assign({}, session, fresh)
      writeSession(merged)
      this.globalData.session = merged
      return merged
    }).catch(() => null)
  },

  setSession(session) {
    writeSession(session)
    this.globalData.session = session
  },

  setBinding(binding) {
    this.globalData.binding = binding
  },

  logout() {
    clearSession()
    clearBinding()
    this.globalData.session = null
    this.globalData.binding = null
  }
})
