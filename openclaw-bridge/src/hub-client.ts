import { sign } from "./hmac.js";
import type { HubIncomingMessage } from "./types.js";

const MAX_RETRIES = 2;
const RETRY_DELAY_MS = 500;

/**
 * HubClient forwards inbound IM messages to MaClaw Hub via the
 * OpenClaw IM webhook protocol (POST /api/openclaw_im/webhook).
 */
export class HubClient {
  constructor(
    private webhookUrl: string,
    private secret: string
  ) {}

  async sendMessage(msg: HubIncomingMessage): Promise<void> {
    const body = JSON.stringify(msg);
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.secret) {
      headers["X-OpenClaw-Signature"] = sign(body, this.secret);
    }

    let lastErr: Error | undefined;
    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
      try {
        const resp = await fetch(this.webhookUrl, {
          method: "POST",
          headers,
          body,
          signal: AbortSignal.timeout(10_000),
        });

        if (resp.ok) return;

        // Don't retry client errors (4xx)
        if (resp.status >= 400 && resp.status < 500) {
          const text = await resp.text().catch(() => "");
          throw new Error(`Hub returned HTTP ${resp.status}: ${text}`);
        }

        lastErr = new Error(`Hub returned HTTP ${resp.status}`);
      } catch (err: any) {
        lastErr = err;
        if (err.name === "AbortError") {
          lastErr = new Error("Hub request timed out");
        }
      }

      // Wait before retry (skip wait on last attempt)
      if (attempt < MAX_RETRIES) {
        await new Promise((r) => setTimeout(r, RETRY_DELAY_MS * (attempt + 1)));
      }
    }

    throw lastErr ?? new Error("Hub request failed");
  }
}
