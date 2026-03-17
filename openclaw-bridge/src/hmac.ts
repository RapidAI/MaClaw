import { createHmac, timingSafeEqual } from "node:crypto";

/** Generate HMAC-SHA256 signature matching Hub's X-OpenClaw-Signature format */
export function sign(body: Buffer | string, secret: string): string {
  const mac = createHmac("sha256", secret);
  mac.update(body);
  return "sha256=" + mac.digest("hex");
}

/** Verify HMAC-SHA256 signature from Hub */
export function verify(
  body: Buffer | string,
  signature: string,
  secret: string
): boolean {
  if (!secret) return true;
  if (!signature) return false;
  const raw = signature.startsWith("sha256=") ? signature.slice(7) : signature;
  const mac = createHmac("sha256", secret);
  mac.update(body);
  const expected = mac.digest("hex");
  try {
    return timingSafeEqual(Buffer.from(raw, "hex"), Buffer.from(expected, "hex"));
  } catch {
    return false;
  }
}
