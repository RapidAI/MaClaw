// ---------------------------------------------------------------------------
// MaClaw Hub OpenClaw IM protocol types — mirrors hub/internal/im/adapter.go
// ---------------------------------------------------------------------------

/** Inbound message POSTed to Hub's /api/openclaw_im/webhook */
export interface HubIncomingMessage {
  platform_name: string;
  platform_uid: string;
  unified_user_id?: string;
  message_type: string; // "text" | "image" | "interactive"
  text: string;
  raw_payload?: unknown;
  timestamp: string; // RFC3339
}

/** Outbound payload Hub POSTs to the bridge's /outbound endpoint */
export interface HubOutboundPayload {
  type: "text" | "card" | "image";
  target: {
    platform_uid: string;
    unified_user_id?: string;
  };
  text?: string;
  message?: {
    title: string;
    body: string;
    fields?: { label: string; value: string }[];
    actions?: { label: string; command: string; style: string }[];
    status_code: number;
    status_icon: string;
    fallback_text: string;
  };
  image_key?: string;
  caption?: string;
}

/** Bridge config file schema */
export interface BridgeConfig {
  hub: {
    webhookUrl: string;
    secret: string;
  };
  bridge: {
    port: number;
    host: string;
  };
  channels: Record<string, ChannelConfig>;
}

export interface ChannelConfig {
  enabled: boolean;
  [key: string]: unknown;
}
