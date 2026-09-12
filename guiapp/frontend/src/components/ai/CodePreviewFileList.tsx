import { useEffect, useMemo, useState } from "react";
import { getDisplayFilePaths, type CodeFile } from "./useCodePreviewState";
import { formatCodeLanguageLabel } from "./codePreviewFindHelpers";
import { extractFileName, type CodePreviewTheme } from "./FileTabBar";

/** Rows listed before the "show more" expansion. */
const FILE_LIST_PAGE_SIZE = 5;

export interface CodePreviewFileListButtonProps {
    files: Map<string, CodeFile>;
    pinnedPaths: string[];
    activeFilePath: string;
    theme: CodePreviewTheme;
    lang: string;
    /** Selects a file and switches the preview off the workspace tab. */
    onSelectFile: (filePath: string) => void;
}

/**
 * Leftmost header button (≡) opening the preview file list: every file in the
 * preview, active one highlighted, paginated behind "show more". The pin keeps
 * the panel open across file selections and outside clicks.
 */
export function CodePreviewFileListButton({ files, pinnedPaths, activeFilePath, theme, lang, onSelectFile }: CodePreviewFileListButtonProps) {
    const isZh = lang.startsWith("zh");
    const [open, setOpen] = useState(false);
    const [pinned, setPinned] = useState(false);
    const [showAll, setShowAll] = useState(false);
    const paths = useMemo(() => getDisplayFilePaths(files, pinnedPaths), [files, pinnedPaths]);

    useEffect(() => {
        if (!open) return;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.stopPropagation();
                setOpen(false);
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, [open]);

    if (paths.length === 0) return null;

    const visiblePaths = showAll ? paths : paths.slice(0, FILE_LIST_PAGE_SIZE);
    const hiddenCount = paths.length - visiblePaths.length;
    const close = () => {
        setOpen(false);
        setShowAll(false);
    };
    const select = (filePath: string) => {
        onSelectFile(filePath);
        if (!pinned) close();
    };

    const listLabel = isZh ? "文件列表" : "File list";
    const pinLabel = isZh ? (pinned ? "取消固定面板" : "固定面板") : (pinned ? "Unpin panel" : "Pin panel");

    return (
        <span style={{ position: "relative", flexShrink: 0, display: "inline-flex", alignItems: "center" }} data-preview-no-maximize="true">
            <button
                type="button"
                data-testid="code-preview-file-list-toggle"
                aria-expanded={open}
                aria-label={listLabel}
                title={listLabel}
                onClick={() => (open ? close() : setOpen(true))}
                style={{
                    width: 28,
                    height: 28,
                    marginLeft: 4,
                    alignSelf: "center",
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    border: "none",
                    borderRadius: 6,
                    background: open ? theme.tabActiveBg : "transparent",
                    color: open ? theme.tabActiveText : theme.textMuted,
                    cursor: "pointer",
                    fontSize: 15,
                    lineHeight: 1,
                    padding: 0,
                    flexShrink: 0,
                }}
            >
                ≡
            </button>
            {open && (
                <>
                    {!pinned && (
                        <div
                            data-testid="code-preview-file-list-backdrop"
                            style={{ position: "fixed", inset: 0, zIndex: 9998 }}
                            onClick={close}
                        />
                    )}
                    <div
                        data-testid="code-preview-file-list-panel"
                        role="menu"
                        aria-label={listLabel}
                        style={{
                            position: "absolute",
                            top: "100%",
                            left: 0,
                            marginTop: 4,
                            zIndex: 9999,
                            width: 280,
                            maxHeight: "60vh",
                            display: "flex",
                            flexDirection: "column",
                            background: theme.bg,
                            border: `1px solid ${theme.border}`,
                            borderRadius: 10,
                            boxShadow: "0 12px 32px rgba(15, 23, 42, 0.18)",
                            overflow: "hidden",
                            fontFamily: "inherit",
                        }}
                    >
                        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "10px 8px 6px 14px" }}>
                            <span style={{ fontSize: 13, fontWeight: 700, color: theme.text }}>{isZh ? "概览" : "Overview"}</span>
                            <button
                                type="button"
                                data-testid="code-preview-file-list-pin"
                                data-active={pinned ? "true" : "false"}
                                onClick={() => setPinned(v => !v)}
                                title={pinLabel}
                                aria-label={pinLabel}
                                aria-pressed={pinned}
                                style={{
                                    width: 24,
                                    height: 24,
                                    display: "inline-flex",
                                    alignItems: "center",
                                    justifyContent: "center",
                                    border: "none",
                                    borderRadius: 6,
                                    background: pinned ? theme.tabActiveBg : "transparent",
                                    color: pinned ? theme.tabActiveText : theme.textMuted,
                                    cursor: "pointer",
                                    fontSize: 12,
                                    padding: 0,
                                }}
                            >
                                ⌖
                            </button>
                        </div>
                        <div style={{ padding: "2px 14px 6px", fontSize: 11, fontWeight: 600, color: theme.textMuted }}>
                            {isZh ? `产物 (${paths.length})` : `Artifacts (${paths.length})`}
                        </div>
                        <div style={{ flex: 1, minHeight: 0, overflowY: "auto", padding: "0 6px 6px" }}>
                            {visiblePaths.map(filePath => {
                                const file = files.get(filePath);
                                const active = filePath === activeFilePath;
                                return (
                                    <button
                                        key={filePath}
                                        type="button"
                                        role="menuitem"
                                        data-testid="code-preview-file-list-item"
                                        data-file-path={filePath}
                                        data-active={active ? "true" : "false"}
                                        onClick={() => select(filePath)}
                                        title={file?.absPath || filePath}
                                        style={{
                                            display: "flex",
                                            alignItems: "center",
                                            gap: 8,
                                            width: "100%",
                                            border: "none",
                                            borderRadius: 6,
                                            padding: "6px 8px",
                                            background: active ? theme.tabActiveBg : "transparent",
                                            color: active ? theme.tabActiveText : theme.text,
                                            cursor: "pointer",
                                            fontSize: 12,
                                            fontFamily: "inherit",
                                            textAlign: "left",
                                        }}
                                    >
                                        <span style={{
                                            flexShrink: 0,
                                            padding: "0 5px",
                                            borderRadius: 4,
                                            background: active ? theme.tabActiveBg : theme.tabBg,
                                            border: `1px solid ${theme.border}`,
                                            color: theme.tabActiveText,
                                            fontSize: 9,
                                            fontWeight: 700,
                                            lineHeight: "16px",
                                            letterSpacing: "0.02em",
                                        }}>
                                            {formatCodeLanguageLabel(file?.language) || "FILE"}
                                        </span>
                                        <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                            {extractFileName(filePath)}
                                        </span>
                                    </button>
                                );
                            })}
                            {hiddenCount > 0 && (
                                <button
                                    type="button"
                                    data-testid="code-preview-file-list-more"
                                    onClick={() => setShowAll(true)}
                                    style={{
                                        display: "block",
                                        width: "100%",
                                        border: "none",
                                        borderRadius: 6,
                                        padding: "6px 8px",
                                        background: "transparent",
                                        color: theme.textMuted,
                                        cursor: "pointer",
                                        fontSize: 12,
                                        fontFamily: "inherit",
                                        textAlign: "left",
                                    }}
                                >
                                    {isZh ? `展开更多（${hiddenCount}）` : `Show more (${hiddenCount})`}
                                </button>
                            )}
                        </div>
                    </div>
                </>
            )}
        </span>
    );
}
