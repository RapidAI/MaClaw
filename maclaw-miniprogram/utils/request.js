/**
 * Thin wx.request wrapper shared by the sign-in API and the device gateway.
 *
 * Everything resolves to the decoded JSON body and rejects with a normalised
 * Error carrying `.status` and `.code` (the Hub error token), so callers can
 * distinguish "token expired, re-pair" from "network unreachable".
 */

function joinUrl(hubUrl, path) {
  return String(hubUrl || '').replace(/\/+$/, '') + path
}

function request(options) {
  const {
    hubUrl,
    path,
    method = 'GET',
    data = null,
    token = '',
    timeout = 30000,
    tolerateStatus = [],
    header = {}
  } = options

  if (!hubUrl) {
    return Promise.reject(withMeta(new Error('尚未设置服务器地址'), 0, 'NO_SERVER'))
  }

  const requestHeader = Object.assign({ 'content-type': 'application/json' }, header)
  if (token) requestHeader['Authorization'] = 'Bearer ' + token

  return new Promise((resolve, reject) => {
    wx.request({
      url: joinUrl(hubUrl, path),
      method,
      data: data || undefined,
      header: requestHeader,
      timeout,
      success: res => {
        const status = res.statusCode
        const body = decodeBody(res.data)
        if (status >= 200 && status < 300) {
          resolve(body)
          return
        }
        if (tolerateStatus.indexOf(status) >= 0) {
          resolve(body)
          return
        }
        const message = (body && (body.message || body.error)) || describeStatus(status)
        reject(withMeta(new Error(message), status, (body && body.error) || describeStatus(status), body))
      },
      fail: err => {
        reject(withMeta(new Error(describeFailure(err)), 0, 'NETWORK_ERROR'))
      }
    })
  })
}

function decodeBody(raw) {
  if (raw == null) return null
  if (typeof raw === 'object') return raw
  if (typeof raw === 'string') {
    try { return JSON.parse(raw) } catch (e) { return { raw } }
  }
  return raw
}

function describeStatus(status) {
  if (status === 401 || status === 403) return '登录已失效'
  if (status === 404) return '接口不存在，请确认 Hub 已开启小程序支持'
  if (status === 409) return '账号属于集群中的另一台 Hub'
  if (status === 429) return '操作过于频繁，请稍后再试'
  if (status >= 500) return '服务器异常'
  return '请求失败（' + status + '）'
}

function describeFailure(err) {
  const message = (err && err.errMsg) || ''
  if (message.indexOf('url not in domain list') >= 0) {
    return '域名不在小程序合法域名列表中，请先在小程序后台配置'
  }
  if (message.indexOf('timeout') >= 0) return '请求超时'
  if (message.indexOf('certificate') >= 0) return '服务器证书不受信任'
  return message || '网络请求失败'
}

function withMeta(error, status, code, body) {
  error.status = status
  error.code = code
  if (body) error.body = body
  return error
}

module.exports = { request, joinUrl }
