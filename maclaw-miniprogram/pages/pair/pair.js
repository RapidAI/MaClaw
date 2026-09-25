const app = getApp()
const { GatewayClient } = require('../../utils/gateway.js')
const { ensureClientId, readBinding, writeBinding, clearBinding, uuid } = require('../../utils/store.js')

Page({
  data: {
    pairCode: '',
    bindingInProgress: false,
    error: '',
    hubUrl: '',
    clientId: '',
    accountLabel: '',
    binding: null
  },

  onLoad() {
    ensureClientId()
  },

  onShow() {
    const session = app.globalData.session
    if (!session) {
      wx.redirectTo({ url: '/pages/login/login' })
      return
    }
    const binding = readBinding()
    this.setData({
      binding,
      hubUrl: session.hubUrl,
      clientId: ensureClientId(),
      accountLabel: session.email || session.sn || session.userId || '—'
    })
  },

  onPairCodeInput(event) {
    // Keep digits only; pasting an SMS sentence is the common failure mode.
    const digits = String(event.detail.value || '').replace(/\D/g, '').slice(0, 6)
    this.setData({ pairCode: digits, error: '' })
  },

  bind() {
    const code = this.data.pairCode
    if (this.data.bindingInProgress || code.length !== 6) return
    const session = app.globalData.session
    if (!session) {
      wx.redirectTo({ url: '/pages/login/login' })
      return
    }

    this.setData({ bindingInProgress: true, error: '' })
    wx.showLoading({ title: '绑定中', mask: true })

    const clientId = ensureClientId()
    GatewayClient
      .pair(session.hubUrl, clientId, code, '微信小程序')
      .then(paired => {
        const binding = {
          clientId: paired.clientId,
          gatewayToken: paired.gatewayToken,
          hubUrl: session.hubUrl,
          pairedAt: Date.now()
        }
        const client = new GatewayClient({
          hubUrl: session.hubUrl,
          clientId: binding.clientId,
          gatewayToken: binding.gatewayToken,
          bootSessionId: app.globalData.bootSessionId
        })
        return client.handshake().then(() => {
          writeBinding(binding)
          app.setBinding(binding)
          wx.hideLoading()
          this.setData({ bindingInProgress: false, pairCode: '' })
          wx.showToast({ title: '绑定成功', icon: 'success' })
          setTimeout(() => wx.switchTab({ url: '/pages/chat/chat' }), 500)
        })
      })
      .catch(err => {
        wx.hideLoading()
        this.setData({ bindingInProgress: false, error: this.describeError(err) })
      })
  },

  unbind() {
    wx.showModal({
      title: '解除绑定',
      content: '小程序将清除本地令牌。桌面端硬件列表中仍会保留该设备，可在桌面端删除。确定继续？',
      success: res => {
        if (!res.confirm) return
        clearBinding()
        app.setBinding(null)
        this.setData({ binding: null, pairCode: '', error: '' })
        wx.showToast({ title: '已解绑', icon: 'success' })
      }
    })
  },

  describeError(err) {
    if (!err) return '未知错误'
    switch (err.code) {
      case 'NETWORK_ERROR':
        return '无法连接 Hub：' + err.message
      case 'PAIR_FAILED':
        return err.message
      default:
        return err.message || '绑定失败'
    }
  }
})
