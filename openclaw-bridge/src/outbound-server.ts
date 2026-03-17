/**
 * OutboundServer — HTTP server that receives outbound messages from Hub
 * and routes them to the correct OpenClaw channel plugin.
 *
 * Hub POSTs to: http://<bridge-host>:<bridge-port>/outbound
 * Headers: X-OpenClaw-Event: message, X-OpenClaw-Signature: sha256=...
 */

import express from "express";
import { verify } from "./hmac.js";
import type { ChannelBridge } from "./channel-bridge.js";
import type { BridgeConfig, HubOutboundPayload } from "./types.js";

export function createOutboundServer(
  config: BridgeConfig,
  bridge: ChannelBridge
): express.Express {
  const app = express();

  // Raw body buffer for HMAC verification
  app.use(
    express.json({
      limit: "64kb",
      verify: (req: any, _res, buf) => {
        req.rawBody = buf;
      },
    })
  );

  // Health check
  app.get("/health", (_req, res) => {
    res.json({
      ok: true,
      channels: bridge.getLoadedChannels(),
    });
  });

  // Hub → Bridge outbound endpoint
  // Handles both "message" events and "ping" test events.
  app.post("/outbound", async (req: any, res) => {
    const rawBody: Buffer | undefined = req.rawBody;
    if (!rawBody) {
      res.status(400).json({ error: "EMPTY_BODY" });
      return;
    }

    // Verify HMAC signature
    const signature = req.headers["x-openclaw-signature"] as string;
    if (!verify(rawBody, signature, config.hub.secret)) {
      res.status(401).json({ error: "INVALID_SIGNATURE" });
      return;
    }

    // Handle ping events from Hub's "Test Webhook" button
    const event = req.headers["x-openclaw-event"] as string;
    if (event === "ping") {
      res.json({ ok: true, bridge: "openclaw-im-bridge" });
      return;
    }

    const payload = req.body as HubOutboundPayload;
    if (!payload.target?.platform_uid) {
      res.status(400).json({ error: "MISSING_TARGET" });
      return;
    }

    // platform_uid format: "<channelId>:<senderId>"
    const uid = payload.target.platform_uid;
    const sepIdx = uid.indexOf(":");
    if (sepIdx < 0) {
      res.status(400).json({ error: "INVALID_PLATFORM_UID_FORMAT" });
      return;
    }

    const channelId = uid.slice(0, sepIdx);
    const rawSenderId = uid.slice(sepIdx + 1);

    try {
      // Pass a copy with rewritten target so we don't mutate the original
      await bridge.handleOutbound(channelId, {
        ...payload,
        target: { ...payload.target, platform_uid: rawSenderId },
      });
      res.json({ ok: true });
    } catch (err: any) {
      console.error(`[outbound] delivery failed:`, err);
      res.status(500).json({ error: err.message });
    }
  });

  return app;
}
