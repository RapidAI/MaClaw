/**
 * ChannelBridge — loads OpenClaw channel plugins and bridges them to MaClaw Hub.
 *
 * Inbound:  OpenClaw plugin receives IM message → normalize → POST to Hub webhook
 * Outbound: Hub POSTs to bridge /outbound → route to correct plugin's send method
 *
 * This module interfaces with OpenClaw's plugin system. Since OpenClaw's
 * internal APIs may evolve, the bridge uses a thin adapter layer that can
 * be adjusted without rewriting the core logic.
 */

import { HubClient } from "./hub-client.js";
import type { BridgeConfig, HubOutboundPayload } from "./types.js";

// ---------------------------------------------------------------------------
// OpenClaw plugin type stubs (minimal subset we actually use)
// ---------------------------------------------------------------------------

interface OCChannelPlugin {
  id: string;
  capabilities?: { chatTypes?: string[] };
  gateway?: {
    start?: (account: unknown) => Promise<void>;
    stop?: (account: unknown) => Promise<void>;
  };
  outbound?: {
    sendText?: (opts: OCOutboundOpts) => Promise<void>;
    sendMedia?: (opts: OCOutboundOpts & { mediaPath: string }) => Promise<void>;
  };
}

interface OCOutboundOpts {
  accountId: string;
  target: string;
  text: string;
  threadId?: string;
}

interface OCPluginApi {
  registerChannel: (reg: { plugin: OCChannelPlugin; dock?: unknown }) => void;
  // OpenClaw plugins may also register tools, hooks, etc. — we ignore those.
  registerTool?: (...args: unknown[]) => void;
  registerHook?: (...args: unknown[]) => void;
  registerService?: (...args: unknown[]) => void;
  registerProvider?: (...args: unknown[]) => void;
}

type OCPluginInit = (api: OCPluginApi) => void | Promise<void>;

// ---------------------------------------------------------------------------
// ChannelBridge
// ---------------------------------------------------------------------------

export class ChannelBridge {
  private hubClient: HubClient;
  private plugins = new Map<string, OCChannelPlugin>();

  constructor(private config: BridgeConfig) {
    this.hubClient = new HubClient(config.hub.webhookUrl, config.hub.secret);
  }

  /** Load and start all enabled channel plugins */
  async start(): Promise<void> {
    for (const [channelId, channelCfg] of Object.entries(this.config.channels)) {
      if (!channelCfg.enabled) continue;
      try {
        await this.loadChannel(channelId, channelCfg);
        console.log(`[bridge] ✓ channel "${channelId}" loaded`);
      } catch (err) {
        console.error(`[bridge] ✗ failed to load channel "${channelId}":`, err);
      }
    }
  }

  /** Gracefully stop all loaded channel plugins */
  async stop(): Promise<void> {
    for (const [channelId, plugin] of this.plugins) {
      try {
        if (plugin.gateway?.stop) {
          await plugin.gateway.stop({ accountId: "default" });
        }
        console.log(`[bridge] channel "${channelId}" stopped`);
      } catch (err) {
        console.error(`[bridge] error stopping channel "${channelId}":`, err);
      }
    }
    this.plugins.clear();
  }

  /** Handle outbound message from Hub → route to correct channel plugin */
  async handleOutbound(channelId: string, payload: HubOutboundPayload): Promise<void> {
    const plugin = this.plugins.get(channelId);
    if (!plugin) {
      throw new Error(`No plugin loaded for channel "${channelId}"`);
    }

    const target = payload.target.platform_uid;

    if (payload.type === "text" && plugin.outbound?.sendText) {
      await plugin.outbound.sendText({
        accountId: "default",
        target,
        text: payload.text ?? "",
      });
    } else if (payload.type === "card" && plugin.outbound?.sendText) {
      // Cards degrade to text — use fallback_text or body
      const text =
        payload.message?.fallback_text ||
        [payload.message?.title, payload.message?.body].filter(Boolean).join("\n\n");
      await plugin.outbound.sendText({
        accountId: "default",
        target,
        text,
      });
    } else if (payload.type === "image" && plugin.outbound?.sendMedia) {
      await plugin.outbound.sendMedia({
        accountId: "default",
        target,
        text: payload.caption ?? "",
        mediaPath: payload.image_key ?? "",
      });
    } else {
      console.warn(`[bridge] unsupported outbound type "${payload.type}" for channel "${channelId}"`);
    }
  }

  /** Forward an inbound IM message to Hub (called by plugin interceptor) */
  forwardToHub(channelId: string, senderId: string, text: string, raw?: unknown): void {
    this.hubClient
      .sendMessage({
        platform_name: "openclaw",
        platform_uid: `${channelId}:${senderId}`,
        message_type: "text",
        text,
        raw_payload: raw,
        timestamp: new Date().toISOString(),
      })
      .catch((err) => {
        console.error(`[bridge] failed to forward ${channelId}:${senderId} → Hub:`, err);
      });
  }

  getLoadedChannels(): string[] {
    return [...this.plugins.keys()];
  }

  // -------------------------------------------------------------------------
  // Private — plugin loading
  // -------------------------------------------------------------------------

  private async loadChannel(channelId: string, channelCfg: Record<string, unknown>): Promise<void> {
    // Try to import the OpenClaw channel plugin package.
    // Convention: @openclaw/<channelId> or openclaw-channel-<channelId>
    let pluginModule: { init?: OCPluginInit; default?: OCPluginInit };
    try {
      pluginModule = await import(`@openclaw/${channelId}`);
    } catch {
      try {
        pluginModule = await import(`openclaw-channel-${channelId}`);
      } catch {
        // Fallback: try loading from openclaw's bundled extensions
        pluginModule = await import(`openclaw/extensions/${channelId}`);
      }
    }

    const initFn = pluginModule.init ?? pluginModule.default;
    if (typeof initFn !== "function") {
      throw new Error(`Channel plugin "${channelId}" does not export init() or default()`);
    }

    // Create a mock PluginApi that captures the channel registration.
    // We provide no-op stubs for non-channel registrations so plugins
    // that also register tools/hooks/etc. don't crash.
    let registeredPlugin: OCChannelPlugin | null = null;
    const noop = () => {};

    const api: OCPluginApi = {
      registerChannel: (reg) => {
        registeredPlugin = reg.plugin;
      },
      registerTool: noop,
      registerHook: noop,
      registerService: noop,
      registerProvider: noop,
    };

    await initFn(api);

    if (!registeredPlugin) {
      throw new Error(`Channel plugin "${channelId}" did not register a channel`);
    }

    this.plugins.set(channelId, registeredPlugin);

    // Start the plugin gateway (connects to platform API).
    const plugin = registeredPlugin as OCChannelPlugin;
    if (plugin.gateway?.start) {
      await plugin.gateway.start({
        accountId: "default",
        ...channelCfg,
        // Inject our bridge's inbound dispatch so the plugin routes
        // received messages to us instead of OpenClaw's agent layer.
        _bridgeDispatch: (senderId: string, text: string, raw?: unknown) => {
          this.forwardToHub(channelId, senderId, text, raw);
        },
      });
    }
  }
}
