import { useCallback, useEffect, useState } from "react";
import { localizeText } from "../../i18n";
import {
    ListBotCatalog,
    InstallBotCatalogItem,
    UninstallBotCatalogItem,
    ListDesktopBots,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";

type CatalogItem = {
    id: string;
    kind: string;
    name: string;
    description?: string;
    source?: string;
    tags?: string[];
    installed: boolean;
};

type BotOpt = { id: string; title: string };

/** Browsable Skills/MCP directory with one-click install per bot. */
export function BotCatalogPanel({ lang }: { lang: string }) {
    const t = useCallback((en: string, zh: string) => localizeText(lang, en, zh, zh), [lang]);
    const [bots, setBots] = useState<BotOpt[]>([]);
    const [botID, setBotID] = useState("");
    const [query, setQuery] = useState("");
    const [items, setItems] = useState<CatalogItem[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");

    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const list = await ListDesktopBots();
                if (cancelled) return;
                const opts = (Array.isArray(list) ? list : []).map((b: any) => ({
                    id: String(b.id || ""),
                    title: String(b.title || b.id || ""),
                })).filter((b) => b.id);
                setBots(opts);
                setBotID((prev) => prev || (opts[0] ? opts[0].id : ""));
            } catch {
                if (!cancelled) setBots([]);
            }
        })();
        return () => { cancelled = true; };
    }, []);

    const load = useCallback(async () => {
        if (!botID) { setItems([]); return; }
        setBusy(true); setError("");
        try {
            const list = await ListBotCatalog(botID, query);
            setItems(Array.isArray(list) ? list : []);
        } catch (e) {
            setError(String(e));
            setItems([]);
        } finally {
            setBusy(false);
        }
    }, [botID, query]);

    // Debounce: query edits should not fire a backend call per keystroke.
    useEffect(() => {
        const timer = window.setTimeout(() => { void load(); }, 250);
        return () => window.clearTimeout(timer);
    }, [load]);

    const toggle = async (item: CatalogItem) => {
        if (!botID || busy) return;
        setBusy(true); setError("");
        try {
            if (item.installed) await UninstallBotCatalogItem(botID, item.kind, item.id);
            else await InstallBotCatalogItem(botID, item.kind, item.id);
            await load();
        } catch (e) {
            setError(String(e));
            setBusy(false);
        }
    };

    return (
        <div data-testid="bot-catalog-panel" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
                <select
                    data-testid="bot-catalog-bot"
                    value={botID}
                    onChange={(e) => setBotID(e.target.value)}
                    aria-label={t("Bot", "机器人")}
                    style={{ minWidth: 160, padding: "6px 8px", borderRadius: radius.md, border: `1px solid ${colors.border}`, background: colors.surface, color: colors.text }}
                >
                    {bots.length === 0 ? <option value="">{t("No bots", "暂无机器人")}</option> : null}
                    {bots.map((b) => <option key={b.id} value={b.id}>{b.title || b.id}</option>)}
                </select>
                <input
                    data-testid="bot-catalog-search"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder={t("Search skills & MCP…", "搜索技能与 MCP…")}
                    style={{ flex: 1, minWidth: 180, padding: "6px 8px", borderRadius: radius.md, border: `1px solid ${colors.border}`, background: colors.surface, color: colors.text }}
                />
                <button type="button" className="btn-secondary" disabled={busy || !botID} onClick={() => void load()}>
                    {t("Refresh", "刷新")}
                </button>
            </div>
            {error ? <div role="alert" style={{ color: colors.danger, fontSize: "0.78rem" }}>{error}</div> : null}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "60vh", overflowY: "auto", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 8 }}>
                {items.length === 0 && !busy ? (
                    <div style={{ color: colors.textMuted, fontSize: "0.78rem", textAlign: "center", padding: 16 }}>
                        {t("No catalog items", "目录为空")}
                    </div>
                ) : null}
                {items.map((item) => (
                    <div key={`${item.kind}:${item.id}`} data-testid={`bot-catalog-item-${item.kind}-${item.id}`} style={{ display: "flex", gap: 10, alignItems: "flex-start", padding: "8px 6px", borderBottom: `1px solid ${colors.border}` }}>
                        <div style={{ flex: 1, minWidth: 0 }}>
                            <div style={{ display: "flex", gap: 8, alignItems: "baseline" }}>
                                <strong style={{ fontSize: "0.86rem" }}>{item.name}</strong>
                                <span style={{ fontSize: "0.7rem", color: colors.textMuted, textTransform: "uppercase" }}>{item.kind}</span>
                            </div>
                            {item.description ? <div style={{ fontSize: "0.74rem", color: colors.textSecondary, marginTop: 2 }}>{item.description}</div> : null}
                            {item.source ? <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: 2 }}>{item.source}</div> : null}
                        </div>
                        <button
                            type="button"
                            className={item.installed ? "btn-secondary" : "btn-primary"}
                            data-testid={`bot-catalog-toggle-${item.kind}-${item.id}`}
                            disabled={busy || !botID}
                            onClick={() => void toggle(item)}
                        >
                            {item.installed ? t("Uninstall", "卸载") : t("Install", "安装")}
                        </button>
                    </div>
                ))}
            </div>
        </div>
    );
}
