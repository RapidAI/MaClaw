const app = getApp()
const { GatewayClient } = require('../../utils/gateway.js')
const { readBinding, clearBinding, ensureClientId, uuid } = require('../../utils/store.js')
const { PROTOCOL } = require('../../config.js')

const MAX_MESSAGES = 300

Page({
  data: {
    messages: [],
    draft: '',
    canSend: false,
    statusText: '正在连接…',
    statusTone: 'waiting',
    binding: false,
    toView: ''
  },

  cursor: '0',
  running: false,
  timer: null,
  client: null,
  seen: {},

  onLoad() {
    ensureClientId()
    this.seen = {}
  },

  onShow() {
    const session = app.globalData.session
    if (!session) {
      wx.redirectTo({ url: '/pages/login/login' })
      return
    }
    const binding = readBinding()
    if (!binding || !binding.gatewayToken) {
      wx.redirectTo({ url: '/pages/pair/pair' })
      return
    }
    this.setData({ binding: true })
    app.setBinding(binding)
    this.start(binding, session)
  },

  onHide() {
    this.stopLoop()
  },

  onUnload() {
    this.stopLoop()
  },

  buildClient(binding, session) {
    return new GatewayClient({
      hubUrl: session.hubUrl,
      clientId: binding.clientId,
      gatewayToken: binding.gatewayToken,
      bootSessionId: app.globalData.bootSessionId,
      userId: session.sn || session.userId || ''
    })
  },

  start(binding, session) {
    this.client = this.buildClient(binding, session)
    if (this.running) return

    this.running = true
    this.setStatus('waiting', '正在握手…')
    this.client
      .handshake()
      .then(() => {
        this.setStatus('online', '已连接到 ' + session.hubUrl)
        this.startLoop()
      })
      .catch(err => {
        if (err.status === 401 || err.status === 403) {
          this.handleAuthFailure()
          return
        }
        this.setStatus('error', '握手失败：' + (err.message || '无法连接 Hub'))
        // Keep retrying in the background; a desktop GUI that is asleep is the
        // most common cause and it resolves itself when the user returns.
        this.startLoop()
      })
  },

  stopLoop() {
    this.running = false
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = null
    }
  },

  startLoop() {
    if (!this.running || this.timer) return
    this.timer = setTimeout(() => {
      this.timer = null
      this.pollOnce()
    }, 10)
  },

  pollOnce() {
    if (!this.running || !this.client) return
    this.client
      .poll(this.cursor, PROTOCOL.pollTimeoutSec, PROTOCOL.pollLimit)
      .then(res => {
        this.cursor = res.nextCursor
        if (res.messages && res.messages.length) {
          this.appendIncoming(res.messages)
          const ids = res.messages.map(item => item.id).filter(Boolean)
          this.client.ack(ids).catch(() => {})
        }
        if (this.data.statusTone !== 'online') {
          this.setStatus('online', '已连接')
        }
      })
      .catch(err => {
        if (err.status === 401 || err.status === 403) {
          this.handleAuthFailure()
          return
        }
        this.setStatus('waiting', '连接中断，正在重试…')
      })
      .then(() => {
        if (this.running) {
          this.timer = setTimeout(() => {
            this.timer = null
            this.pollOnce()
          }, PROTOCOL.pollRetryDelayMs)
        }
      })
  },

  handleAuthFailure() {
    this.stopLoop()
    clearBinding()
    app.setBinding(null)
    this.setData({ binding: false })
    this.setStatus('error', '令牌已失效')
    wx.showModal({
      title: '需要重新绑定',
      content: '当前令牌已被桌面端撤销或重置，请在新页面重新输入 6 位配对码。',
      showCancel: false,
      success: () => wx.redirectTo({ url: '/pages/pair/pair' })
    })
  },

  setStatus(tone, text) {
    this.setData({ statusTone: tone, statusText: text })
  },

  onDraftInput(event) {
    const draft = event.detail.value
    this.setData({ draft, canSend: !!draft.trim() })
  },

  send() {
    const text = (this.data.draft || '').trim()
    if (!text || !this.client) return

    const localId = 'local-' + uuid()
    const entry = {
      id: localId,
      role: 'me',
      kind: 'text',
      text,
      state: 'sending',
      time: formatTime(Date.now())
    }
    this.pushMessages([entry])
    this.setData({ draft: '', canSend: false })

    this.client
      .sendText(text)
      .then(() => {
        this.updateMessage(localId, { state: 'sent' })
        // Replies are streamed back through /outgoing; poll immediately so the
        // answer does not wait for the current long poll to time out.
        this.kick()
      })
      .catch(err => {
        this.updateMessage(localId, { state: 'failed' })
        wx.showToast({ title: err.message || '发送失败', icon: 'none' })
      })
  },

  // Restart the poll chain right away after sending.
  kick() {
    if (!this.running) return
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = null
    }
    this.timer = setTimeout(() => {
      this.timer = null
      this.pollOnce()
    }, 10)
  },

  appendIncoming(list) {
    const entries = []
    list.forEach(raw => {
      if (!raw) return
      const id = raw.id || raw.messageId || raw.eventId
      if (id && this.seen[id]) return
      if (id) this.seen[id] = true
      const info = describeMessage(raw)
      if (!info.text) return
      entries.push({
        id: id || 'in-' + uuid(),
        role: 'them',
        kind: info.kind,
        text: info.text,
        time: formatTime(raw.createdAt || raw.timestamp || Date.now())
      })
    })
    if (entries.length) this.pushMessages(entries)
  },

  pushMessages(entries) {
    const next = this.data.messages.concat(entries)
    const trimmed = next.length > MAX_MESSAGES ? next.slice(next.length - MAX_MESSAGES) : next
    this.setData({ messages: trimmed })
    this.scrollToTail()
  },

  updateMessage(id, patch) {
    const messages = this.data.messages.map(item => (item.id === id ? Object.assign({}, item, patch) : item))
    this.setData({ messages })
  },

  scrollToTail() {
    this.setData({ toView: 'msg-tail' })
  },

  goPair() {
    wx.navigateTo({ url: '/pages/pair/pair' })
  }
})

/**
 * Poll results come from whatever the desktop agent produced, so the payload is
 * normalised rather than trusted to hold one exact shape.
 */
function describeMessage(raw) {
  const type = String(raw.type || '')
  const text = firstText(raw)
  if (text) return { kind: 'text', text }
  if (type.indexOf('tool') === 0) {
    return { kind: 'tool', text: raw.name || raw.toolName || raw.title || type }
  }
  if (raw.attachments && raw.attachments.length) {
    return { kind: 'text', text: '[附件 ' + raw.attachments.length + ' 个，暂不支持在小程序显示]' }
  }
  return { kind: 'text', text: '' }
}

function firstText(raw) {
  const candidates = [raw.text, raw.content, raw.message && raw.message.text, raw.reply]
  for (let i = 0; i < candidates.length; i++) {
    const value = candidates[i]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function formatTime(input) {
  const value = typeof input === 'number' ? new Date(input) : new Date(String(input || '').replace(' ', 'T'))
  if (isNaN(value.getTime())) return ''
  const pad = n => (n < 10 ? '0' + n : String(n))
  return pad(value.getHours()) + ':' + pad(value.getMinutes())
}
