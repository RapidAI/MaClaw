import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { ProjectSearchIcon } from "./ProjectSearchIcon";

export function SearchSectionLabel({ lang, theme: t, en, zh, zhHant, testId }: { lang: string; theme: Theme; en: string; zh: string; zhHant?: string; testId?: string }) {
    const label = localizeText(lang, en, zh, zhHant);
    const han = /[\u3400-\u9fff]/.test(label);
    return <div data-testid={testId} style={{ padding: "8px 10px 4px", fontSize: "10px", fontWeight: 700, letterSpacing: han ? 0 : "0.06em", textTransform: han ? "none" : "uppercase", color: t.textMuted, opacity: 0.72 }}>{label}</div>;
}

export function LibrarySearchRow({ kind, title, preview, mark, theme: t, testId, onSelect }: {
    kind: string; title: string; preview?: string; mark?: string; lang?: string; theme: Theme; testId: string; onSelect: () => void;
}) {
    return <div data-testid={testId} onClick={onSelect} onKeyDown={event => { if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); onSelect(); } }} role="button" tabIndex={0} aria-label={title} className="pslr-row" onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}>
        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: preview ? "2px" : 0 }}>
            <span style={{ minWidth: "26px", textAlign: "center", fontSize: "10px", fontWeight: 700, color: t.textMuted, border: `1px solid ${t.titleBarBorder}`, borderRadius: "4px", padding: "1px 4px", flexShrink: 0 }}>{kind}</span>
            <span style={{ fontSize: "13px", fontWeight: 600, color: t.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{title}</span>
            <span style={{ color: t.headingColor, opacity: 0.7, flexShrink: 0 }}>{mark || (kind === "KNOW" ? <ProjectSearchIcon name="book" /> : <ProjectSearchIcon name="folder" />)}</span>
        </div>
        {preview ? <div style={{ fontSize: "11px", color: t.text, opacity: 0.45, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "34px" }}>{preview}</div> : null}
    </div>;
}
