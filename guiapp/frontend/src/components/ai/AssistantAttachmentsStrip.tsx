import { useEffect, useState, type CSSProperties, type Dispatch, type SetStateAction } from "react";
import { AIAssistantAttachmentPreviewDataURL, AIAssistantFileTypeIconDataURL } from "../../../wailsjs/go/main/App";
import { AttachmentImageThumbnail } from "./AttachmentImagePreview";
import type { AttachmentInfo } from "./useBufferQueue";
import { isImageFilePath } from "./useAIAssistant";
import { type Theme } from "./aiAssistantPanelTheme";
import "./ensureAIAssistantPanelStyles";

interface AssistantAttachmentsStripProps {
    cancelPending: boolean;
    clearSelectedFile?: () => void;
    lang: string;
    pendingAttachmentsTestId?: string;
    pendingAttachments: AttachmentInfo[];
    removeSelectedFile?: (filePath: string) => void;
    selectedFilePaths: string[];
    setPendingAttachments: Dispatch<SetStateAction<AttachmentInfo[]>>;
    theme: Theme;
}

const fileTypeIconPromises = new Map<string, Promise<string>>();
const resolvedFileTypeIcons = new Map<string, string>();

export function resetAttachmentFileTypeIconCacheForTests(): void {
    fileTypeIconPromises.clear();
    resolvedFileTypeIcons.clear();
}

// Own-icon extensions match fileTypeIconUsesOwnIcon in guiapp/file_type_icon.go.
const ownFileTypeIconExtensions = new Set(["", ".exe", ".lnk", ".ico", ".url", ".scr"]);

// fileTypeIconExtension matches path/filepath.Ext: the suffix from the last dot
// in the final path element, including a name that itself starts with a dot.
function fileTypeIconExtension(filePath: string): string {
    const name = filePath.split(/[\\/]/).pop() || filePath;
    const dot = name.lastIndexOf(".");
    if (dot < 0) return "";
    return name.slice(dot).toLowerCase();
}

function fileTypeIconCacheKey(filePath: string): string {
    const trimmed = filePath.trim();
    const ext = fileTypeIconExtension(trimmed);
    if (ownFileTypeIconExtensions.has(ext)) return trimmed.toLowerCase();
    return ext;
}

function attachmentKindLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    if (!raw) return "FILE";
    return raw.slice(0, 4).toUpperCase();
}

function attachmentFileName(filePath: string): string {
    return filePath.split(/[\/\\]/).pop() || filePath;
}

function loadFileTypeIcon(filePath: string): Promise<string> {
    const key = fileTypeIconCacheKey(filePath);
    const ready = resolvedFileTypeIcons.get(key);
    if (ready) return Promise.resolve(ready);
    const cached = fileTypeIconPromises.get(key);
    if (cached) return cached;
    const pending = (async () => {
        try {
            return String(await AIAssistantFileTypeIconDataURL(filePath) || "");
        } catch {
            return "";
        }
    })();
    fileTypeIconPromises.set(key, pending);
    void pending.then(url => {
        // A newer lookup, or a test reset, may have replaced this entry.
        if (fileTypeIconPromises.get(key) !== pending) return;
        if (url) resolvedFileTypeIcons.set(key, url);
        fileTypeIconPromises.delete(key);
    });
    return pending;
}

const iconFrameStyle: CSSProperties = {
    width: "32px",
    height: "32px",
    flexShrink: 0,
    objectFit: "contain",
    display: "block",
};

function AttachmentTypeBadge({ label, theme }: { label: string; theme: Theme }) {
    return (
        <span
            aria-hidden="true"
            style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: "32px",
                height: "32px",
                flexShrink: 0,
                borderRadius: "6px",
                background: theme.codeBlockBorder,
                color: theme.pathColor,
                fontSize: label.length > 3 ? "8px" : "10px",
                fontWeight: 800,
                letterSpacing: 0,
                lineHeight: 1,
            }}
        >
            {label}
        </span>
    );
}

function AttachmentImageFallback({ filePath, fileName, lang, theme }: {
    filePath: string;
    fileName: string;
    lang: string;
    theme: Theme;
}) {
    const [preview, setPreview] = useState("");
    const [previewFailed, setPreviewFailed] = useState(false);
    useEffect(() => {
        let active = true;
        setPreviewFailed(false);
        void AIAssistantAttachmentPreviewDataURL(filePath)
            .then(dataUrl => {
                if (!active) return;
                const resolved = String(dataUrl || "");
                setPreview(resolved);
                setPreviewFailed(!resolved);
            })
            .catch(() => {
                if (!active) return;
                setPreview("");
                setPreviewFailed(true);
            });
        return () => { active = false; };
    }, [filePath]);

    if (preview) {
        return <AttachmentImageThumbnail src={preview} filePath={filePath} fileName={fileName || "attached image"} lang={lang} theme={theme} title={filePath} frameStyle={{ width: "32px", height: "32px", borderRadius: "4px", flexShrink: 0, border: "none", background: "transparent" }} onImageError={() => { setPreview(""); setPreviewFailed(true); }} />;
    }
    if (!previewFailed) {
        return <span aria-hidden="true" className="aas-thumb-slot" style={{ width: "32px", height: "32px" }} />;
    }
    return <AttachmentTypeBadge label="IMG" theme={theme} />;
}

function AttachmentChipIcon({ filePath, fileName, isImage, lang, theme }: {
    filePath: string;
    fileName: string;
    isImage: boolean;
    lang: string;
    theme: Theme;
}) {
    const cacheKey = fileTypeIconCacheKey(filePath);
    const [iconURL, setIconURL] = useState<string | null>(() => resolvedFileTypeIcons.get(cacheKey) ?? null);
    useEffect(() => {
        let active = true;
        const known = resolvedFileTypeIcons.get(cacheKey);
        if (known) {
            setIconURL(known);
            return;
        }
        setIconURL(null);
        void loadFileTypeIcon(filePath).then(url => {
            if (active) setIconURL(url);
        });
        return () => { active = false; };
    }, [cacheKey, filePath]);

    if (iconURL) {
        return <img data-testid="attachment-file-type-icon" src={iconURL} alt="" draggable={false} style={iconFrameStyle} />;
    }
    if (iconURL === null) {
        return <span aria-hidden="true" className="aas-thumb-slot" style={{ width: "32px", height: "32px" }} />;
    }
    if (isImage) {
        return <AttachmentImageFallback filePath={filePath} fileName={fileName} lang={lang} theme={theme} />;
    }
    return <AttachmentTypeBadge label={attachmentKindLabel(fileName, "")} theme={theme} />;
}

function attachmentRemoveLabel(lang: string, fileName: string): string {
    return lang.startsWith("en") ? `Remove ${fileName}` : `移除 ${fileName}`;
}

function attachmentRowStyle(theme: Theme): CSSProperties {
    return {
        display: "flex",
        flexDirection: "row",
        alignItems: "center",
        gap: "8px",
        minWidth: 0,
        maxWidth: "100%",
        padding: "4px 4px 4px 6px",
        borderRadius: "8px",
        background: theme.codeBlockBg,
        border: `1px solid ${theme.codeBlockBorder}`,
        color: theme.text,
        fontSize: "13px",
        lineHeight: 1.3,
    };
}

function removeButtonStyle(theme: Theme, disabled = false): CSSProperties {
    return {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        width: "22px",
        height: "22px",
        flexShrink: 0,
        padding: 0,
        border: "none",
        borderRadius: "4px",
        background: "transparent",
        color: theme.actionBtnColor,
        cursor: disabled ? "not-allowed" : "pointer",
        fontSize: "17px",
        fontWeight: 400,
        lineHeight: 1,
        opacity: disabled ? 0.5 : 1,
    };
}

function AttachmentChip({ filePath, fileName, isImage, lang, theme, disabled, onRemove }: {
    filePath: string;
    fileName: string;
    isImage: boolean;
    lang: string;
    theme: Theme;
    disabled: boolean;
    onRemove: () => void;
}) {
    return (
        <div className="ai-attachment-row" role="listitem" title={filePath} style={attachmentRowStyle(theme)}>
            <AttachmentChipIcon filePath={filePath} fileName={fileName} isImage={isImage} lang={lang} theme={theme} />
            <span className="ai-attachment-name" title={fileName} aria-label={fileName}>{fileName}</span>
            <button
                type="button"
                className="ai-attachment-remove"
                onClick={onRemove}
                disabled={disabled}
                style={removeButtonStyle(theme, disabled)}
                title={attachmentRemoveLabel(lang, fileName)}
                aria-label={attachmentRemoveLabel(lang, fileName)}
            >
                {"×"}
            </button>
        </div>
    );
}

export function AssistantAttachmentsStrip({
    cancelPending,
    clearSelectedFile,
    lang,
    pendingAttachmentsTestId = "ai-pending-attachments",
    pendingAttachments,
    removeSelectedFile,
    selectedFilePaths,
    setPendingAttachments,
    theme: t,
}: AssistantAttachmentsStripProps) {
    const hasAttachments = pendingAttachments.length > 0 || selectedFilePaths.length > 0;

    if (!hasAttachments) return null;

    return (
        <div
            className="ai-attachment-strip"
            data-testid={pendingAttachmentsTestId}
            role="list"
            aria-label={lang.startsWith("en") ? "Attached files" : "已附加文件"}
            style={{
                gap: "6px",
                alignSelf: "flex-start",
                flexShrink: 0,
                minWidth: 0,
                maxHeight: "120px",
                overflowY: "auto",
                overscrollBehavior: "contain",
                paddingRight: "4px",
                scrollbarGutter: "stable",
                ["--ai-attachment-remove-hover-bg" as string]: t.errorBg,
                ["--ai-attachment-remove-hover-color" as string]: t.errorText,
                ["--ai-attachment-focus-color" as string]: t.btnColor,
            }}
        >
            {pendingAttachments.map((att, index) => {
                const fileName = att.fileName || attachmentFileName(att.filePath);
                return (
                    <AttachmentChip
                        key={att.filePath + "-" + index}
                        filePath={att.filePath}
                        fileName={fileName}
                        isImage={att.isImage}
                        lang={lang}
                        theme={t}
                        disabled={cancelPending}
                        onRemove={() => setPendingAttachments(prev => prev.filter(candidate => candidate !== att))}
                    />
                );
            })}
            {selectedFilePaths.map((filePath: string, index: number) => {
                const fileName = attachmentFileName(filePath);
                return (
                    <AttachmentChip
                        key={filePath + index}
                        filePath={filePath}
                        fileName={fileName}
                        isImage={isImageFilePath(filePath)}
                        lang={lang}
                        theme={t}
                        disabled={cancelPending}
                        onRemove={() => removeSelectedFile ? removeSelectedFile(filePath) : clearSelectedFile?.()}
                    />
                );
            })}
        </div>
    );
}
