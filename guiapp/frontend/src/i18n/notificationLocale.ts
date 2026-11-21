import { normalizeLang } from "./langSelect";

/** One stored language of an admin notice. The link is not translated. */
export interface NotificationLocaleCopy {
  title?: string;
  content?: string;
  button_text?: string;
}

/** Fields the desktop keeps so a language switch can pick without a re-pull. */
export interface NotificationTextSource {
  title?: string;
  content?: string;
  button_text?: string;
  source_lang?: string;
  locales?: Record<string, NotificationLocaleCopy | undefined>;
}

export interface NotificationText {
  title: string;
  content: string;
  button_text: string;
}

function asText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** Keep locale rows the Hub sent. Unknown shapes become no map. */
export function normalizeNotificationLocales(raw: unknown): NotificationTextSource["locales"] {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const out: Record<string, NotificationLocaleCopy> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (!value || typeof value !== "object" || Array.isArray(value)) continue;
    const row = value as Record<string, unknown>;
    out[key] = {
      title: asText(row.title),
      content: asText(row.content),
      button_text: asText(row.button_text),
    };
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

/**
 * Title, body, and button for the current UI language.
 * A missing language, or a copy with neither title nor body, shows the
 * original text. An empty field inside a real copy falls back on its own.
 */
export function pickNotificationText(lang: string | undefined | null, item: NotificationTextSource | null | undefined): NotificationText {
  const source: NotificationText = {
    title: item?.title || "",
    content: item?.content || "",
    button_text: item?.button_text || "",
  };
  const copy = item?.locales?.[normalizeLang(lang)];
  if (!copy) return source;
  const title = (copy.title || "").trim();
  const content = (copy.content || "").trim();
  if (!title && !content) return source;
  const button = (copy.button_text || "").trim();
  return {
    title: title || source.title,
    content: content || source.content,
    button_text: button || source.button_text,
  };
}
