import { memo, useEffect, useMemo, useRef, type CSSProperties, type MutableRefObject, type ReactNode } from "react";
import { localizeText } from "./aiAssistantI18n";
import { resolvePrimaryFilledColors, type Theme } from "./aiAssistantPanelTheme";
import { isFormFieldTarget, isInsideAriaHidden } from "./codingUiGuards";
import { detectLanguage, tokenizeLine, type HighlightToken } from "./syntaxHighlight";
import { createCodePreviewTheme, type CodePreviewTheme } from "./CodePreviewPanel";

export type CodingConflictItem = {
    id: string;
    step_index?: number;
    path?: string;
    kind?: string;
    files?: string[];
};

export type CodingConflictDiff = {
    path: string;
    status: string;
    unified?: string;
    three_way?: string;
    base_head?: string;
};

export type CodingConflictPreview = {
    side: string;
    path: string;
    content: string;
    truncated?: boolean;
    missing?: boolean;
};

export type CodingConflictTriple = {
    main?: { content?: string; missing?: boolean };
    theirs?: { content?: string; missing?: boolean };
    base?: { content?: string; missing?: boolean };
};

export type CodingConflictSidePanelProps = {
    lang?: string;
    theme: Theme;
    /** When true, parent (preview pane) owns resize/split chrome. */
    embedded?: boolean;
    splitRatio?: number;
    startPreviewResize?: () => void;
    busy: boolean;
    /** Peak conflict count in this wave (for progress). */
    progressTotal?: number;
    conflicts: CodingConflictItem[];
    activeId: string;
    diffs: CodingConflictDiff[];
    selected: string[];
    focusFile: string;
    preview: CodingConflictPreview | null;
    previewSide: "main" | "theirs" | "base";
    editDraft: string;
    triple: CodingConflictTriple | null;
    conflictLog: string[];
    onClose: () => void;
    onOpenConflict: (id: string) => void;
    onDiscardAll: () => void;
    onDiscard: (id: string) => void;
    onResolveBatch: (id: string, action: "adopt" | "keep" | "base") => void;
    onToggleFile: (path: string) => void;
    onSelectAll: (paths: string[]) => void;
    onClearSelection: () => void;
    onResolveSelected: (action: "adopt" | "keep" | "base") => void;
    onAdoptFile: (id: string, path: string) => void;
    onKeepMainFile: (id: string, path: string) => void;
    onAdoptBaseFile: (id: string, path: string) => void;
    onOpenFile: (path: string, side: "main" | "theirs") => void;
    onLoadPreview: (path: string, side: "main" | "theirs" | "base") => void;
    onApplyPreviewSide: () => void;
    onWriteEdit: () => void;
    onEditDraftChange: (value: string) => void;
    onExportLog: () => void;
    onClearLog: () => void;
    syncTripleScroll: (source: "base" | "main" | "theirs", scrollTop: number, scrollLeft: number) => void;
    tripleScrollRefs: MutableRefObject<Record<"base" | "main" | "theirs", HTMLDivElement | null>>;
};

function tokenColor(type: HighlightToken["type"], codeTheme: CodePreviewTheme): string {
    switch (type) {
        case "keyword": return codeTheme.syntaxKeyword;
        case "string": return codeTheme.syntaxString;
        case "comment": return codeTheme.syntaxComment;
        case "number": return codeTheme.syntaxNumber;
        case "function": return codeTheme.syntaxFunction;
        case "type": return codeTheme.syntaxType;
        case "operator": return codeTheme.syntaxOperator;
        default: return codeTheme.text;
    }
}

const HighlightedCodeLine = memo(function HighlightedCodeLine({
    line,
    language,
    codeTheme,
}: {
    line: string;
    language: string;
    codeTheme: CodePreviewTheme;
}) {
    const tokens = useMemo(() => tokenizeLine(line, language), [line, language]);
    if (!line) return <span>{"\u00a0"}</span>;
    if (tokens.length === 0) return <span>{line}</span>;
    return (
        <>
            {tokens.map((tok, i) => (
                <span key={i} style={{ color: tokenColor(tok.type, codeTheme) }}>{tok.text}</span>
            ))}
        </>
    );
});

/** Cap highlighted lines in each triple pane to keep large files responsive. */
const TRIPLE_HIGHLIGHT_LINE_CAP = 400;

/**
 * Isolation conflict resolution + three-way compare.
 * - Standalone: owns split resize chrome (legacy)
 * - embedded: content only for AssistantPreviewPane conflict tab
 */
export function CodingConflictSidePanel({
    lang,
    theme: t,
    embedded = false,
    splitRatio = 0.55,
    startPreviewResize,
    busy,
    progressTotal = 0,
    conflicts,
    activeId,
    diffs,
    selected,
    focusFile,
    preview,
    previewSide,
    editDraft,
    triple,
    conflictLog,
    onClose,
    onOpenConflict,
    onDiscardAll,
    onDiscard,
    onResolveBatch,
    onToggleFile,
    onSelectAll,
    onClearSelection,
    onResolveSelected,
    onAdoptFile,
    onKeepMainFile,
    onAdoptBaseFile,
    onOpenFile,
    onLoadPreview,
    onApplyPreviewSide,
    onWriteEdit,
    onEditDraftChange,
    onExportLog,
    onClearLog,
    syncTripleScroll,
    tripleScrollRefs,
}: CodingConflictSidePanelProps) {
    // Escape closes the panel unless focus is in an editor field.
    // When embedded as a preview tab that is aria-hidden (user switched to SRC/WF),
    // do not own Esc — even if focus is still stuck under the hidden slot.
    const rootRef = useRef<HTMLDivElement | null>(null);
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    const embeddedRef = useRef(embedded);
    embeddedRef.current = embedded;
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (isFormFieldTarget(e.target)) return;
            if (embeddedRef.current) {
                const root = rootRef.current;
                if (!root || isInsideAriaHidden(root)) return;
            }
            e.preventDefault();
            e.stopPropagation();
            onCloseRef.current();
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, []);

    const codeTheme = useMemo(() => createCodePreviewTheme(t), [t]);
    const total = Math.max(progressTotal, conflicts.length, 0);
    const remaining = conflicts.length;
    const resolved = Math.max(0, total - remaining);
    const progressRatio = total > 0 ? resolved / total : 0;

    const paneStyle: CSSProperties = embedded
        ? {
            height: "100%",
            display: "flex",
            flexDirection: "column",
            minWidth: 0,
            background: t.bg,
        }
        : {
            flex: Math.max(0.28, 1 - splitRatio),
            minWidth: 320,
            maxWidth: "70%",
            height: "100%",
            display: "flex",
            flexDirection: "row",
            position: "relative",
            background: t.bg,
            borderLeft: `1px solid ${t.divider || t.titleBarBorder}`,
        };

    const primaryFilled = resolvePrimaryFilledColors(t);
    const dangerColor = t.errorText;
    const dangerSurface = `color-mix(in srgb, ${dangerColor} 10%, ${t.fieldBg})`;
    const dangerBorder = `color-mix(in srgb, ${dangerColor} 40%, ${t.fieldBorder})`;
    const dangerSelectedSurface = `color-mix(in srgb, ${dangerColor} 12%, ${t.fieldBg})`;
    const dangerSelectedBorder = `color-mix(in srgb, ${dangerColor} 48%, ${t.fieldBorder})`;
    const btnSm = (opts?: { primary?: boolean; danger?: boolean; muted?: boolean }): CSSProperties => ({
        height: 24,
        padding: "0 8px",
        borderRadius: 4,
        border: opts?.primary ? "none" : `1px solid ${opts?.danger ? dangerBorder : t.fieldBorder}`,
        // Primary CTAs use sendBtn* pair (dark btnColor is light accent, not a fill)
        background: opts?.primary ? primaryFilled.bg : "transparent",
        color: opts?.primary ? primaryFilled.fg : (opts?.danger ? dangerColor : (opts?.muted ? (t.fieldLabel || t.textMuted || t.promptColor) : t.text)),
        fontSize: 11,
        cursor: busy ? "wait" : "pointer",
        opacity: busy ? 0.7 : 1,
    });

    const body: ReactNode = (
            <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", overflow: "hidden" }}>
                <div
                    style={{
                        flexShrink: 0,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: 8,
                        padding: "8px 12px",
                        borderBottom: `1px solid ${t.divider || t.titleBarBorder}`,
                        background: t.titleBarBg || t.bg,
                    }}
                >
                    <div style={{ minWidth: 0, flex: 1 }}>
                        <div style={{ fontWeight: 700, color: dangerColor, fontSize: 13 }}>
                            {localizeText(lang, "Isolation conflicts", "隔离冲突", "隔離衝突")}
                            <span style={{ marginLeft: 8, fontWeight: 600, opacity: 0.85 }}>
                                {remaining}
                                {total > remaining ? ` / ${total}` : ""}
                            </span>
                        </div>
                        <div style={{ fontSize: 11, color: t.textMuted || t.promptColor, marginTop: 2 }}>
                            {localizeText(
                                lang,
                                "Resolve worktree isolation changes before continuing.",
                                "请处理 worktree 隔离变更后再继续。",
                                "請處理 worktree 隔離變更後再繼續。",
                            )}
                        </div>
                        {total > 0 ? (
                            <div data-testid="coding-conflict-progress" style={{ marginTop: 8 }} title={`${resolved}/${total}`}>
                                <div style={{ display: "flex", justifyContent: "space-between", fontSize: 10, color: t.textMuted || t.promptColor, marginBottom: 3 }}>
                                    <span>{localizeText(lang, "Resolution progress", "解决进度", "解決進度")}</span>
                                    <span data-testid="coding-conflict-progress-label">
                                        {localizeText(lang, `${resolved} of ${total} resolved`, `已解决 ${resolved}/${total}`, `已解決 ${resolved}/${total}`)}
                                    </span>
                                </div>
                                <div
                                    style={{
                                        height: 6,
                                        borderRadius: 999,
                                        background: dangerSurface,
                                        overflow: "hidden",
                                    }}
                                    role="progressbar"
                                    aria-valuemin={0}
                                    aria-valuemax={total}
                                    aria-valuenow={resolved}
                                >
                                    <div
                                        data-testid="coding-conflict-progress-bar"
                                        style={{
                                            height: "100%",
                                            width: `${Math.round(progressRatio * 100)}%`,
                                            borderRadius: "inherit",
                                            background: progressRatio >= 1 ? codeTheme.diffAddText : t.btnColor,
                                            transition: "width 0.2s ease",
                                        }}
                                    />
                                </div>
                            </div>
                        ) : null}
                    </div>
                    <div style={{ display: "flex", gap: 6, flexShrink: 0 }}>
                        <button
                            type="button"
                            data-testid="coding-conflict-discard-all"
                            disabled={busy || conflicts.length === 0}
                            onClick={onDiscardAll}
                            style={btnSm({ danger: true })}
                        >
                            {localizeText(lang, "Discard all", "清理全部", "清理全部")}
                        </button>
                        <button
                            type="button"
                            data-testid="coding-conflict-side-close"
                            onClick={onClose}
                            style={btnSm({ muted: true })}
                            title={localizeText(lang, "Close conflict panel", "关闭冲突侧栏", "關閉衝突側欄")}
                        >
                            {localizeText(lang, "Close", "关闭", "關閉")}
                        </button>
                    </div>
                </div>

                <div style={{ flex: 1, minHeight: 0, overflow: "auto", padding: "10px 12px" }}>
                    {conflicts.length === 0 ? (
                        <div data-testid="coding-conflict-side-empty" style={{ fontSize: 12, color: t.textMuted || t.promptColor, padding: "12px 4px" }}>
                            {localizeText(lang, "No isolation conflicts right now.", "当前没有隔离冲突。", "目前沒有隔離衝突。")}
                        </div>
                    ) : null}
                    <div style={{ display: "flex", flexDirection: "column", gap: 8, marginBottom: 12 }}>
                        {conflicts.map((c) => (
                            <div
                                key={c.id}
                                style={{
                                    display: "flex",
                                    flexWrap: "wrap",
                                    gap: 6,
                                    alignItems: "center",
                                    padding: "8px 10px",
                                    borderRadius: 6,
                                    border: `1px solid ${activeId === c.id ? dangerBorder : t.fieldBorder}`,
                                    background: activeId === c.id
                                        ? dangerSurface
                                        : t.fieldBg,
                                }}
                            >
                                <button
                                    type="button"
                                    onClick={() => onOpenConflict(c.id)}
                                    style={{
                                        border: "none",
                                        background: "transparent",
                                        color: t.headingColor || t.btnColor,
                                        cursor: "pointer",
                                        fontSize: 12,
                                        padding: 0,
                                        fontWeight: 700,
                                        textAlign: "left",
                                    }}
                                >
                                    {c.id || c.path} {c.step_index ? `(T${c.step_index})` : ""}
                                </button>
                                <button type="button" disabled={busy} onClick={() => onResolveBatch(c.id, "adopt")} style={btnSm({ primary: true })}>
                                    {localizeText(lang, "Adopt all", "全部采纳", "全部採納")}
                                </button>
                                <button
                                    type="button"
                                    data-testid="coding-conflict-keep-main-all"
                                    disabled={busy}
                                    onClick={() => onResolveBatch(c.id, "keep")}
                                    style={btnSm({ muted: true })}
                                    title={localizeText(lang, "Keep main tree for all files", "全部保留主树版本", "全部保留主樹版本")}
                                >
                                    {localizeText(lang, "Keep main", "保留主树", "保留主樹")}
                                </button>
                                <button
                                    type="button"
                                    data-testid="coding-conflict-base-all"
                                    disabled={busy}
                                    onClick={() => onResolveBatch(c.id, "base")}
                                    style={btnSm({ muted: true })}
                                    title={localizeText(lang, "Write merge-base for all files", "全部写回 merge-base", "全部寫回 merge-base")}
                                >
                                    {localizeText(lang, "Take base all", "全部取 base", "全部取 base")}
                                </button>
                                <button type="button" disabled={busy} onClick={() => onDiscard(c.id)} style={btnSm({ muted: true })}>
                                    {localizeText(lang, "Discard", "丢弃", "丟棄")}
                                </button>
                            </div>
                        ))}
                    </div>

                    {activeId && diffs.length === 0 && conflicts.length > 0 ? (
                        <div data-testid="coding-conflict-side-loading" style={{ fontSize: 12, color: t.textMuted || t.promptColor, padding: "8px 4px" }}>
                            {busy
                                ? localizeText(lang, "Working…", "处理中…", "處理中…")
                                : localizeText(lang, "Loading file diffs…", "正在加载文件差异…", "正在載入檔案差異…")}
                        </div>
                    ) : null}
                    {activeId && diffs.length > 0 && (
                        <div data-testid="coding-conflict-panel">
                            <div
                                data-testid="coding-conflict-selected-bar"
                                style={{
                                    display: "flex",
                                    flexWrap: "wrap",
                                    gap: 6,
                                    alignItems: "center",
                                    marginBottom: 10,
                                    paddingBottom: 8,
                                    borderBottom: `1px solid ${t.fieldBorder || "rgba(127,127,127,0.2)"}`,
                                }}
                            >
                                <label style={{ display: "flex", alignItems: "center", gap: 4, cursor: "pointer", fontSize: 12 }}>
                                    <input
                                        type="checkbox"
                                        data-testid="coding-conflict-select-all"
                                        checked={selected.length > 0 && selected.length === diffs.length}
                                        onChange={(e) => {
                                            if (e.target.checked) onSelectAll(diffs.map((x) => x.path).filter(Boolean));
                                            else onClearSelection();
                                        }}
                                    />
                                    <span>
                                        {selected.length > 0
                                            ? localizeText(lang, `Selected ${selected.length}`, `已选 ${selected.length}`, `已選 ${selected.length}`)
                                            : localizeText(lang, "Select files", "选择文件", "選擇檔案")}
                                    </span>
                                </label>
                                <button type="button" data-testid="coding-conflict-selected-adopt" disabled={busy || selected.length === 0} onClick={() => onResolveSelected("adopt")} style={{ ...btnSm({ primary: true }), opacity: selected.length ? 1 : 0.5, cursor: (busy || !selected.length) ? "not-allowed" : "pointer" }}>
                                    {localizeText(lang, "Adopt selected", "采纳所选", "採納所選")}
                                </button>
                                <button type="button" data-testid="coding-conflict-selected-keep" disabled={busy || selected.length === 0} onClick={() => onResolveSelected("keep")} style={{ ...btnSm({ muted: true }), opacity: selected.length ? 1 : 0.5, cursor: (busy || !selected.length) ? "not-allowed" : "pointer" }}>
                                    {localizeText(lang, "Keep selected", "保留所选主树", "保留所選主樹")}
                                </button>
                                <button type="button" data-testid="coding-conflict-selected-base" disabled={busy || selected.length === 0} onClick={() => onResolveSelected("base")} style={{ ...btnSm({ muted: true }), opacity: selected.length ? 1 : 0.5, cursor: (busy || !selected.length) ? "not-allowed" : "pointer" }}>
                                    {localizeText(lang, "Base selected", "所选取 base", "所選取 base")}
                                </button>
                            </div>

                            {diffs.map((d) => (
                                <div
                                    key={d.path}
                                    style={{
                                        marginBottom: 12,
                                        padding: 10,
                                        borderRadius: 6,
                                        border: `1px solid ${selected.includes(d.path) ? dangerSelectedBorder : (t.fieldBorder || "rgba(127,127,127,0.25)")}`,
                                        background: selected.includes(d.path)
                                            ? dangerSelectedSurface
                                            : (t.fieldBg || "transparent"),
                                    }}
                                >
                                    <div style={{ display: "flex", justifyContent: "space-between", gap: 8, marginBottom: 6, alignItems: "center" }}>
                                        <label style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0, cursor: "pointer" }}>
                                            <input
                                                type="checkbox"
                                                data-testid={`coding-conflict-file-check-${d.path}`}
                                                checked={selected.includes(d.path)}
                                                onChange={() => onToggleFile(d.path)}
                                            />
                                            <strong style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: 12 }}>{d.path}</strong>
                                        </label>
                                        <span style={{ opacity: 0.8, flexShrink: 0, fontSize: 11 }}>
                                            {d.status}
                                            {d.three_way || d.base_head ? ` · ${localizeText(lang, "3-way", "三路", "三路")}` : ""}
                                        </span>
                                    </div>
                                    {d.three_way ? (
                                        <pre data-testid="coding-conflict-three-way" style={{ margin: "0 0 8px", whiteSpace: "pre-wrap", fontSize: 11, maxHeight: 120, overflow: "auto" }}>{d.three_way}</pre>
                                    ) : d.unified ? (
                                        <pre style={{ margin: "0 0 8px", whiteSpace: "pre-wrap", fontSize: 11, maxHeight: 100, overflow: "auto" }}>{d.unified}</pre>
                                    ) : null}
                                    <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                                        <button type="button" disabled={busy} onClick={() => onAdoptFile(activeId, d.path)} style={btnSm({ primary: true })}>
                                            {localizeText(lang, "Adopt theirs", "采纳隔离侧", "採納隔離側")}
                                        </button>
                                        <button type="button" data-testid="coding-conflict-keep-main" disabled={busy} onClick={() => onKeepMainFile(activeId, d.path)} style={btnSm({ muted: true })}>
                                            {localizeText(lang, "Keep main", "保留主树", "保留主樹")}
                                        </button>
                                        <button type="button" data-testid="coding-conflict-adopt-base" disabled={busy} onClick={() => onAdoptBaseFile(activeId, d.path)} title={localizeText(lang, "Write merge-base content to main", "将 merge-base 写回主树", "將 merge-base 寫回主樹")} style={btnSm({ muted: true })}>
                                            {localizeText(lang, "Take base", "取 base", "取 base")}
                                        </button>
                                        <button type="button" data-testid="coding-conflict-open-main" disabled={busy} onClick={() => onOpenFile(d.path, "main")} title={localizeText(lang, "Open main-tree file", "打开主树文件", "開啟主樹檔案")} style={btnSm({ muted: true })}>
                                            {localizeText(lang, "Open main", "打开主树", "開啟主樹")}
                                        </button>
                                        <button type="button" data-testid="coding-conflict-open-theirs" disabled={busy} onClick={() => onOpenFile(d.path, "theirs")} title={localizeText(lang, "Open isolate-side file", "打开隔离侧文件", "開啟隔離側檔案")} style={btnSm({ muted: true })}>
                                            {localizeText(lang, "Open theirs", "打开隔离侧", "開啟隔離側")}
                                        </button>
                                        <button
                                            type="button"
                                            data-testid="coding-conflict-preview"
                                            disabled={busy}
                                            onClick={() => onLoadPreview(d.path, previewSide)}
                                            style={{
                                                ...btnSm({ muted: true }),
                                                background: focusFile === d.path ? dangerSelectedSurface : "transparent",
                                            }}
                                        >
                                            {localizeText(lang, "3-way preview", "三路预览", "三路預覽")}
                                        </button>
                                    </div>

                                    {focusFile === d.path && preview && preview.path === d.path ? (
                                        <div data-testid="coding-conflict-preview-panel" style={{ marginTop: 10 }}>
                                            <div style={{ display: "flex", gap: 4, marginBottom: 8, flexWrap: "wrap", alignItems: "center" }}>
                                                {(["main", "theirs", "base"] as const).map((side) => (
                                                    <button
                                                        key={side}
                                                        type="button"
                                                        data-testid={`coding-conflict-preview-side-${side}`}
                                                        onClick={() => onLoadPreview(d.path, side)}
                                                        style={{
                                                            height: 22,
                                                            padding: "0 8px",
                                                            borderRadius: 4,
                                                            border: `1px solid ${previewSide === side ? dangerSelectedBorder : (t.fieldBorder || "rgba(127,127,127,0.3)")}`,
                                                            background: previewSide === side ? dangerSelectedSurface : "transparent",
                                                            fontSize: 11,
                                                            cursor: "pointer",
                                                        }}
                                                    >
                                                        {side}
                                                    </button>
                                                ))}
                                                {preview.truncated ? (
                                                    <span style={{ fontSize: 11, opacity: 0.75 }}>{localizeText(lang, "truncated", "已截断", "已截斷")}</span>
                                                ) : null}
                                                <button
                                                    type="button"
                                                    data-testid="coding-conflict-apply-preview-side"
                                                    disabled={busy || !!preview.missing}
                                                    onClick={onApplyPreviewSide}
                                                    title={localizeText(lang, `Apply ${previewSide} to main tree`, `将 ${previewSide} 写到主树`, `將 ${previewSide} 寫到主樹`)}
                                                    style={{ ...btnSm({ primary: true }), opacity: preview.missing ? 0.5 : 1, cursor: (busy || preview.missing) ? "not-allowed" : "pointer" }}
                                                >
                                                    {localizeText(lang, `Apply ${previewSide}`, `应用 ${previewSide}`, `套用 ${previewSide}`)}
                                                </button>
                                                <button
                                                    type="button"
                                                    data-testid="coding-conflict-write-edit"
                                                    disabled={busy || !!preview.missing}
                                                    onClick={onWriteEdit}
                                                    title={localizeText(lang, "Write edited text to main tree", "将编辑后的内容写回主树", "將編輯後的內容寫回主樹")}
                                                    style={{ ...btnSm({ muted: true }), opacity: preview.missing ? 0.5 : 1, cursor: (busy || preview.missing) ? "not-allowed" : "pointer" }}
                                                >
                                                    {localizeText(lang, "Write edit", "写回编辑", "寫回編輯")}
                                                </button>
                                            </div>

                                            {triple ? (() => {
                                                // All three panes show the same file, so resolve the
                                                // language once rather than once per column/render.
                                                const language = detectLanguage(d.path);
                                                return (
                                                <div data-testid="coding-conflict-triple" style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 6, marginBottom: 8 }}>
                                                    {([
                                                        { key: "base" as const, label: "base", data: triple.base },
                                                        { key: "main" as const, label: "main", data: triple.main },
                                                        { key: "theirs" as const, label: "theirs", data: triple.theirs },
                                                    ] as const).map((col) => {
                                                        const raw = col.data?.missing ? "" : String(col.data?.content || "");
                                                        const allLines = col.data?.missing ? ["—"] : (raw.length ? raw.split("\n") : ["(empty)"]);
                                                        const truncated = allLines.length > TRIPLE_HIGHLIGHT_LINE_CAP;
                                                        const lines = truncated ? allLines.slice(0, TRIPLE_HIGHLIGHT_LINE_CAP) : allLines;
                                                        const gutterW = Math.max(2, String(allLines.length).length) + 1;
                                                        return (
                                                            <div
                                                                key={col.key}
                                                                style={{
                                                                    border: `1px solid ${previewSide === col.key ? dangerSelectedBorder : (t.fieldBorder || "rgba(127,127,127,0.25)")}`,
                                                                    borderRadius: 4,
                                                                    overflow: "hidden",
                                                                    minWidth: 0,
                                                                }}
                                                            >
                                                                <button
                                                                    type="button"
                                                                    onClick={() => onLoadPreview(d.path, col.key)}
                                                                    style={{
                                                                        width: "100%",
                                                                        border: "none",
                                                                        background: previewSide === col.key ? dangerSelectedSurface : "transparent",
                                                                        fontSize: 11,
                                                                        fontWeight: 600,
                                                                        padding: "4px 6px",
                                                                        cursor: "pointer",
                                                                        textAlign: "left",
                                                                        color: t.text,
                                                                    }}
                                                                >
                                                                    {col.label}{col.data?.missing ? " · ∅" : ""} · {allLines.length}L{truncated ? "+" : ""}
                                                                </button>
                                                                <div
                                                                    ref={(el) => { tripleScrollRefs.current[col.key] = el; }}
                                                                    data-testid={`coding-conflict-triple-scroll-${col.key}`}
                                                                    onScroll={(e) => {
                                                                        const el = e.currentTarget;
                                                                        syncTripleScroll(col.key, el.scrollTop, el.scrollLeft);
                                                                    }}
                                                                    style={{
                                                                        margin: 0,
                                                                        maxHeight: 280,
                                                                        overflow: "auto",
                                                                        opacity: col.data?.missing ? 0.55 : 0.95,
                                                                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                                                                        fontSize: 11,
                                                                        lineHeight: 1.4,
                                                                        background: codeTheme.bg,
                                                                        color: codeTheme.text,
                                                                    }}
                                                                >
                                                                    <table style={{ borderCollapse: "collapse", width: "100%", tableLayout: "fixed" }}>
                                                                        <tbody>
                                                                            {lines.map((line, li) => (
                                                                                <tr key={`${col.key}-${li}`}>
                                                                                    <td
                                                                                        data-testid={li === 0 ? `coding-conflict-triple-ln-${col.key}` : undefined}
                                                                                        style={{
                                                                                            width: `${gutterW}ch`,
                                                                                            minWidth: `${gutterW}ch`,
                                                                                            padding: "0 4px 0 2px",
                                                                                            textAlign: "right",
                                                                                            userSelect: "none",
                                                                                            opacity: 0.45,
                                                                                            verticalAlign: "top",
                                                                                            whiteSpace: "nowrap",
                                                                                            borderRight: `1px solid ${codeTheme.border}`,
                                                                                            color: codeTheme.lineNumText,
                                                                                            background: codeTheme.lineNumBg,
                                                                                        }}
                                                                                    >
                                                                                        {col.data?.missing ? "" : li + 1}
                                                                                    </td>
                                                                                    <td
                                                                                        data-testid={li === 0 ? `coding-conflict-triple-code-${col.key}` : undefined}
                                                                                        style={{ padding: "0 4px", whiteSpace: "pre", overflowWrap: "normal", wordBreak: "keep-all", verticalAlign: "top" }}
                                                                                    >
                                                                                        {col.data?.missing || line === "(empty)" || line === "—"
                                                                                            ? (line || " ")
                                                                                            : <HighlightedCodeLine line={line} language={language} codeTheme={codeTheme} />}
                                                                                    </td>
                                                                                </tr>
                                                                            ))}
                                                                            {truncated ? (
                                                                                <tr>
                                                                                    <td colSpan={2} style={{ padding: "4px 6px", fontSize: 10, opacity: 0.7, color: codeTheme.textMuted }}>
                                                                                        {localizeText(lang, `… ${allLines.length - TRIPLE_HIGHLIGHT_LINE_CAP} more lines (open file for full content)`, `… 另有 ${allLines.length - TRIPLE_HIGHLIGHT_LINE_CAP} 行（完整内容请打开文件）`, `… 另有 ${allLines.length - TRIPLE_HIGHLIGHT_LINE_CAP} 行（完整內容請開啟檔案）`)}
                                                                                    </td>
                                                                                </tr>
                                                                            ) : null}
                                                                        </tbody>
                                                                    </table>
                                                                </div>
                                                            </div>
                                                        );
                                                    })}
                                                </div>
                                                );
                                            })() : null}

                                            {preview.missing ? (
                                                <pre style={{ margin: 0, whiteSpace: "pre-wrap", fontSize: 11, maxHeight: 160, overflow: "auto", opacity: 0.7 }}>
                                                    {localizeText(lang, "(missing on this side)", "（此侧不存在）", "（此側不存在）")}
                                                </pre>
                                            ) : (
                                                <textarea
                                                    data-testid="coding-conflict-edit-draft"
                                                    value={editDraft}
                                                    onChange={(e) => onEditDraftChange(e.target.value)}
                                                    rows={12}
                                                    spellCheck={false}
                                                    style={{
                                                        width: "100%",
                                                        boxSizing: "border-box",
                                                        margin: 0,
                                                        fontSize: 11,
                                                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                                                        lineHeight: 1.4,
                                                        minHeight: 160,
                                                        maxHeight: 320,
                                                        resize: "vertical",
                                                        borderRadius: 4,
                                                        border: `1px solid ${t.fieldBorder || "rgba(127,127,127,0.3)"}`,
                                                        background: t.fieldBg || "transparent",
                                                        color: t.text || "inherit",
                                                        padding: 8,
                                                    }}
                                                />
                                            )}
                                        </div>
                                    ) : null}
                                </div>
                            ))}
                        </div>
                    )}

                    {conflictLog.length > 0 ? (
                        <div data-testid="coding-conflict-log" style={{ marginTop: 12, fontSize: 11, color: t.textMuted || t.promptColor, opacity: 0.95 }}>
                            <div style={{ display: "flex", justifyContent: "space-between", gap: 8, marginBottom: 4, alignItems: "center" }}>
                                <span style={{ fontWeight: 600 }}>{localizeText(lang, "Conflict log", "冲突日志", "衝突日誌")}</span>
                                <span style={{ display: "flex", gap: 8 }}>
                                    <button type="button" data-testid="coding-conflict-log-export" onClick={onExportLog} style={{ border: "none", background: "transparent", color: t.headingColor || t.btnColor || t.textMuted, fontSize: 11, cursor: "pointer", padding: 0 }}>
                                        {localizeText(lang, "Export", "导出", "匯出")}
                                    </button>
                                    <button type="button" data-testid="coding-conflict-log-clear" onClick={onClearLog} style={{ border: "none", background: "transparent", color: t.textMuted, fontSize: 11, cursor: "pointer", padding: 0 }}>
                                        {localizeText(lang, "Clear", "清空", "清空")}
                                    </button>
                                </span>
                            </div>
                            {conflictLog.slice().reverse().slice(0, 8).map((line, i) => (
                                <div key={`${i}-${line.slice(0, 24)}`} style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{line}</div>
                            ))}
                        </div>
                    ) : null}
                </div>
            </div>
    );

    if (embedded) {
        return (
            <div
                ref={rootRef}
                data-testid="coding-conflict-side-panel"
                role="complementary"
                aria-label={localizeText(lang, "Isolation conflict side panel", "隔离冲突侧栏", "隔離衝突側欄")}
                style={paneStyle}
            >
                {body}
            </div>
        );
    }

    return (
        <div
            ref={rootRef}
            data-testid="coding-conflict-side-panel"
            role="complementary"
            aria-label={localizeText(lang, "Isolation conflict side panel", "隔离冲突侧栏", "隔離衝突側欄")}
            style={paneStyle}
        >
            <div
                data-testid="coding-conflict-side-resize"
                onMouseDown={(e) => {
                    e.preventDefault();
                    startPreviewResize?.();
                }}
                style={{
                    width: 6,
                    cursor: "col-resize",
                    flexShrink: 0,
                    background: "transparent",
                }}
                title={localizeText(lang, "Drag to resize", "拖动调整宽度", "拖曳調整寬度")}
            />
            {body}
        </div>
    );
}
