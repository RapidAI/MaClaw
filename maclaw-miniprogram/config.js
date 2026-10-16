// Bootstrap configuration. The mini program asks this server who owns the
// account, then pins all Device Gateway / IM Gateway traffic to whatever Hub
// the login response routes it to.
//
// Must be HTTPS with a trusted certificate, and must be added to the
// request-legitimate-domains list of the mini program, otherwise WeChat blocks
// every wx.request on a real device.

const DEFAULT_BOOTSTRAP_HUB = 'https://hubs.maclaw.top'

const STORAGE_KEYS = {
  session: 'maclaw.session',
  binding: 'maclaw.binding',
  clientId: 'maclaw.clientId',
  server: 'maclaw.server'
}

// Wire protocol constants shared with the MaClaw Hub gateways.
const PROTOCOL = {
  version: '1.1',
  pairPath: '/api/device-gateway/v1/pair',
  basePath: '/api/im-gateway/v1',
  defaultConversationId: 'default',
  // Long-poll budget. WeChat caps a request at 60s; stay well under it and let
  // the server clamp the value as well.
  pollTimeoutSec: 20,
  pollLimit: 20,
  // Wait this long before opening the next poll so the hub is never hammered
  // by a tight loop when a request fails instantly.
  pollRetryDelayMs: 800
}

module.exports = {
  DEFAULT_BOOTSTRAP_HUB,
  STORAGE_KEYS,
  PROTOCOL
}
