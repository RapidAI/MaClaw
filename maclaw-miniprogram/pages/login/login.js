const app = getApp()
const { sendEmailCode, verifyEmailCode, sendPhoneCode, verifyPhoneCode, normalizeHub } = require('../../utils/auth.js')
const { ensureClientId, readServer, writeServer } = require('../../utils/store.js')
const { DEFAULT_BOOTSTRAP_HUB } = require('../../config.js')

const COUNTDOWN_SECONDS = 60

Page({
  data: {
    mode: 'email',
    account: '',
    code: '',
    sending: false,
    submitting: false,
    countdown: 0,
    codeMinutes: 5,
    error: '',
    hubUrl: '',
    serverInput: '',
    showServer: false
  },

  onLoad() {
    const hubUrl = readServer() || app.globalData.hubUrl || DEFAULT_BOOTSTRAP_HUB
    this.setData({ hubUrl: normalizeHub(hubUrl), serverInput: normalizeHub(hubUrl) })
  },

  onShow() {
    // Already signed in? Skip straight to the previous page.
    if (app.globalData.session) {
      app.verifySession().then(session => {
        if (session) this.navigateForward()
      })
    }
  },

  onUnload() {
    this.stopCountdown()
  },

  onHide() {
    this.stopCountdown()
  },

  switchMode(event) {
    const mode = event.currentTarget.dataset.mode
    if (mode === this.data.mode) return
    this.setData({ mode, account: '', code: '', error: '' })
    this.stopCountdown()
  },

  onAccountInput(event) {
    this.setData({ account: event.detail.value.trim(), error: '' })
  },

  onCodeInput(event) {
    this.setData({ code: event.detail.value.trim(), error: '' })
  },

  onServerInput(event) {
    this.setData({ serverInput: event.detail.value.trim() })
  },

  toggleServerPanel() {
    this.setData({ showServer: !this.data.showServer })
  },

  saveServer() {
    const url = normalizeHub(this.data.serverInput)
    if (!/^https:\/\/.+/.test(url)) {
      this.setData({ error: '服务器地址必须是 https:// 开头的域名（微信小程序不允许 http 或 IP）' })
      return
    }
    writeServer(url)
    app.globalData.hubUrl = url
    this.setData({ hubUrl: url, error: '' })
    wx.showToast({ title: '已保存', icon: 'success' })
  },

  currentHubUrl() {
    return normalizeHub(app.globalData.hubUrl || DEFAULT_BOOTSTRAP_HUB)
  },

  sendCode() {
    if (this.data.sending || this.data.countdown > 0 || !this.data.account) return
    const useEmail = this.data.mode === 'email'
    this.setData({ sending: true, error: '' })

    const call = useEmail
      ? sendEmailCode(this.currentHubUrl(), this.data.account)
      : sendPhoneCode(this.currentHubUrl(), this.data.account)

    call.then(res => {
      const minutes = (res && res.expires_min) || 5
      this.setData({ sending: false, codeMinutes: minutes })
      this.startCountdown((res && res.resend_cooldown_seconds) || COUNTDOWN_SECONDS)
      wx.showToast({ title: '验证码已发送', icon: 'success' })
    }).catch(err => {
      this.setData({ sending: false, error: this.describeError(err) })
    })
  },

  startCountdown(seconds) {
    this.stopCountdown()
    this.setData({ countdown: seconds })
    this.timer = setInterval(() => {
      const next = this.data.countdown - 1
      if (next <= 0) {
        this.stopCountdown()
        this.setData({ countdown: 0 })
        return
      }
      this.setData({ countdown: next })
    }, 1000)
  },

  stopCountdown() {
    if (this.timer) {
      clearInterval(this.timer)
      this.timer = null
    }
  },

  submit() {
    if (this.data.submitting || !this.data.account || !this.data.code) return
    this.setData({ submitting: true, error: '' })
    wx.showLoading({ title: '登录中', mask: true })

    const hubUrl = this.currentHubUrl()
    const clientId = ensureClientId()
    const useEmail = this.data.mode === 'email'
    const call = useEmail
      ? verifyEmailCode(hubUrl, this.data.account, this.data.code, clientId)
      : verifyPhoneCode(hubUrl, this.data.account, this.data.code, clientId)

    call.then(session => {
      wx.hideLoading()
      this.setData({ submitting: false })
      app.setSession(session)
      wx.showToast({ title: '登录成功', icon: 'success' })
      setTimeout(() => this.navigateForward(), 500)
    }).catch(err => {
      wx.hideLoading()
      this.setData({ submitting: false, error: this.describeError(err) })
    })
  },

  navigateForward() {
    if (app.globalData.binding && app.globalData.binding.gatewayToken) {
      wx.switchTab({ url: '/pages/chat/chat' })
    } else {
      wx.redirectTo({ url: '/pages/pair/pair' })
    }
  },

  describeError(err) {
    if (!err) return '未知错误'
    switch (err.code) {
      case 'ACCOUNT_NOT_FOUND':
        return '该邮箱或手机号还没有 MaClaw 账号。请先在桌面端码卡龙注册并完成开户。'
      case 'ACCOUNT_INACTIVE':
        return '账号未激活，请联系管理员审批。'
      case 'EMAIL_ROUTED_TO_ANOTHER_HUB': {
        const target = err.body && err.body.hub_id ? '（目标 Hub：' + err.body.hub_id + '）' : ''
        return '这个账号由集群中的另一台 Hub 服务' + target + '，请在服务器设置中改填那台 Hub 的地址。'
      }
      case 'MAIL_NOT_CONFIGURED':
        return '该 Hub 未配置邮件服务，请改用手机号登录。'
      case 'PHONE_REGISTRATION_DISABLED':
        return '该 Hub 未启用手机号登录，请改用邮箱登录。'
      case 'RATE_LIMITED':
        return '发送过于频繁，请稍后再试。'
      case 'INVALID_VERIFY_CODE':
        return '验证码错误或已过期。'
      case 'VERIFY_LOCKED':
        return '尝试次数过多，请重新获取验证码。'
      case 'NO_SERVER':
        return '尚未设置服务器地址。'
      case 'NETWORK_ERROR':
        return err.message
      default:
        return err.message || '登录失败'
    }
  }
})
