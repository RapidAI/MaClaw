#!/usr/bin/env node
/**
 * OpenClaw IM Bridge — entry point
 *
 * Loads OpenClaw channel plugins and bridges them to MaClaw Hub
 * via the OpenClaw IM webhook protocol.
 *
 * Usage:
 *   BRIDGE_CONFIG=./config.json node dist/index.js
 *   # or
 *   npx tsx src/index.ts
 */

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { ChannelBridge } from "./channel-bridge.js";
import { createOutboundServer } from "./outbound-server.js";
import type { BridgeConfig } from "./types.js";

function loadConfig(): BridgeConfig {
  const configPath = resolve(process.env.BRIDGE_CONFIG ?? "./config.json");
  try {
    return JSON.parse(readFileSync(configPath, "utf-8"));
  } catch (err) {
    console.error(`Failed to load config from ${configPath}:`, err);
    console.error("Copy config.example.json to config.json and fill in your values.");
    return process.exit(1);
  }
}

async function main() {
  const config = loadConfig();

  console.log("[bridge] MaClaw Hub webhook:", config.hub.webhookUrl);
  console.log("[bridge] Enabled channels:", Object.entries(config.channels)
    .filter(([, c]) => c.enabled)
    .map(([id]) => id)
    .join(", ") || "(none)");

  // Create and start the channel bridge
  const bridge = new ChannelBridge(config);
  await bridge.start();

  // Start the outbound HTTP server (receives messages from Hub)
  const app = createOutboundServer(config, bridge);
  const { port, host } = config.bridge;

  const server = app.listen(port, host, () => {
    console.log(`[bridge] Outbound server listening on http://${host}:${port}`);
    console.log("[bridge] Ready.");
  });

  // Graceful shutdown
  const shutdown = async () => {
    console.log("\n[bridge] Shutting down...");
    server.close();
    await bridge.stop();
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

main().catch((err) => {
  console.error("[bridge] Fatal:", err);
  process.exit(1);
});
