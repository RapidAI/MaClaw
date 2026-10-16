const { request } = require('./request.js')
const { PROTOCOL } = require('../config.js')
const { uuid } = require('./store.js')

/**
 * Client for the MaClaw third-party access protocol — the same surface the
 * ESP32 pet devices speak:
 *
 *   Device Gateway v1  POST /api/device-gateway/v1/pair      -> gatewayToken
 *   IM Gateway v1.1    POST /api/im-gateway/v1/handshake     -> declare caps
 *                      POST /api/im-gateway/v1/incoming      -> send a message
 *                      GET  /api/im-gateway/v1/outgoing      -> cursor long poll
 *                      POST /api/im-gateway/v1/ack           -> delivery receipt
 *                      POST /api/im-gateway/v1/tool-result   -> tool output
 *                      POST /api/im-gateway/v1/media/upload-url
 *
 * Every authenticated call carries `Authorization: Bearer <gatewayToken>`.
 */

class GatewayClient {
  constructor(options) {
    this.hubUrl = String(options.hubUrl || '').replace(/\/+$/, '')
    this.clientId = options.clientId
    this.clientName = options.clientName || 'WeChat Mini Program'
    this.token = options.gatewayToken || ''
    this.conversationId = options.conversationId || PROTOCOL.defaultConversationId
    this.bootSessionId = options.bootSessionId || ''
    this.handshaken = false
  }

  get basePath() {
    return PROTOCOL.basePath
  }

  call(method, endpoint, data, extra) {
    return request(
      Object.assign(
        {
          hubUrl: this.hubUrl,
          path: this.basePath + endpoint,
          method,
          data,
          token: this.token,
          timeout: 40000
        },
        extra || {}
      )
    )
  }

  /**
   * Exchanges a six-digit pairing code for the long-lived gateway token.
   * Static because it runs before a client instance has a token.
   */
  static pair(hubUrl, clientId, pairCode, clientName) {
    return request({
      hubUrl,
      path: PROTOCOL.pairPath,
      method: 'POST',
      data: { clientId, pairCode, clientName },
      timeout: 20000
    }).then(body => {
      if (!body || !body.gatewayToken) {
        const err = new Error('配对失败：' + ((body && body.message) || '未返回网关令牌'))
        err.code = (body && body.error) || 'PAIR_FAILED'
        err.status = 201
        throw err
      }
      return { gatewayToken: body.gatewayToken, clientId: body.clientId || clientId }
    })
  }

  handshake() {
    const body = {
      clientId: this.clientId,
      clientName: this.clientName,
      protocolVersion: PROTOCOL.version,
      bootSessionId: this.bootSessionId,
      userId: this.userId || '',
      // Both shapes are sent: modern hubs read input/output capabilities,
      // older ones read the flat feature flags. Extra keys are ignored.
      capabilities: {
        input: { modalities: ['text'], features: { text: true } },
        output: { modalities: ['text'], features: { text: true } },
        text: true,
        longPolling: true,
        ack: true
      }
    }
    return this.call('POST', '/handshake', body).then(res => {
      this.handshaken = true
      return res
    })
  }

  sendText(text) {
    return this.call('POST', '/incoming', {
      clientId: this.clientId,
      eventId: uuid(),
      messageId: uuid(),
      conversationId: this.conversationId,
      message: { type: 'text', text }
    })
  }

  /**
   * Cursor long poll. Returns immediately when messages are already queued,
   * otherwise holds the request server-side up to `timeout` seconds.
   */
  poll(cursor, timeoutSec, limit) {
    const query =
      '?clientId=' + encodeURIComponent(this.clientId) +
      '&cursor=' + encodeURIComponent(String(cursor || 0)) +
      '&limit=' + encodeURIComponent(String(limit || PROTOCOL.pollLimit)) +
      '&timeout=' + encodeURIComponent(String(timeoutSec || PROTOCOL.pollTimeoutSec))
    return this.call('GET', '/outgoing' + query, null, {
      timeout: ((timeoutSec || PROTOCOL.pollTimeoutSec) + 8) * 1000
    }).then(body => {
      const payload = body || {}
      return {
        messages: Array.isArray(payload.messages) ? payload.messages : [],
        nextCursor: payload.nextCursor != null ? String(payload.nextCursor) : String(cursor || 0),
        hasMore: payload.hasMore === true
      }
    })
  }

  ack(messageIds) {
    if (!messageIds || !messageIds.length) return Promise.resolve(null)
    return this.call('POST', '/ack', {
      clientId: this.clientId,
      messageIds: messageIds
    })
  }

  sendToolResult(toolCallId, status, result) {
    return this.call('POST', '/tool-result', {
      clientId: this.clientId,
      resultId: uuid(),
      conversationId: this.conversationId,
      toolCallId: toolCallId,
      status: status || 'success',
      result: result || {}
    })
  }

  /**
   * Reserves a media slot and returns the signed upload URL. Reserved for the
   * voice/image follow-up; kept here so the protocol surface stays in one file.
   */
  prepareUpload(type, fileName, mimeType, sizeBytes) {
    return this.call('POST', '/media/upload-url', {
      clientId: this.clientId,
      type,
      fileName,
      mimeType,
      sizeBytes
    })
  }

  mediaUrl(url) {
    if (!url) return ''
    if (/^https?:\/\//i.test(url)) return url
    return this.hubUrl + (url.indexOf('/') === 0 ? url : '/' + url)
  }
}

module.exports = { GatewayClient }
