import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";

export function ProjectSearchArchivedPanel({ name, content, loading, lang, theme: t, panelRef, onClose }: {
    name: string;
    content: string;
    loading: boolean;
    lang: string;
    theme: Theme;
    panelRef: React.RefObject<HTMLDivElement>;
    onClose: () => void;
}) {
    return (
        <div ref={panelRef} style={{ flexShrink: 0, borderBottom: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, zIndex: 30000, position: "relative", overflow: "visible" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "8px 12px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                <span style={{ fontSize: "10px", fontWeight: 700, color: t.textMuted, border: `1px solid ${t.titleBarBorder}`, borderRadius: "4px", padding: "1px 5px", flexShrink: 0 }}>ARC</span>
                <span style={{ fontSize: "13px", fontWeight: 600, color: t.text, flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{name}</span>
                <span style={{ fontSize: "10px", padding: "2px 8px", borderRadius: "999px", background: "rgba(100,116,139,0.10)", color: t.textMuted, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{localizeText(lang, "Archived", "\u5df2\u5f52\u6863")}</span>
                <button onClick={onClose} style={{ background: "none", border: "none", cursor: "pointer", color: t.text, opacity: 0.5, fontSize: "12px", padding: "2px 4px", lineHeight: 1, flexShrink: 0 }} title={localizeText(lang, "Close", "\u5173\u95ed")}>{"x"}</button>
            </div>
            <div style={{ maxHeight: "400px", overflowY: "auto", padding: "12px 16px" }}>
                {loading ? <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{localizeText(lang, "Loading...", "\u52a0\u8f7d\u4e2d...")}</div> : <div style={{ fontSize: "12px", color: t.text, lineHeight: 1.7, whiteSpace: "pre-wrap", opacity: 0.85 }}>{content}</div>}
            </div>
            <div style={{ padding: "8px 16px", borderTop: `1px solid ${t.titleBarBorder}`, fontSize: "11px", color: t.text, opacity: 0.4, textAlign: "center" }}>{localizeText(lang, "This task has been archived and cannot be continued.", "\u6b64\u4efb\u52a1\u5df2\u5f52\u6863\uff0c\u4e0d\u53ef\u7ee7\u7eed\u3002")}</div>
        </div>
    );
}
