import { useEffect, useState, type CSSProperties, type Dispatch, type SetStateAction } from "react";
import { AIAssistantAttachmentPreviewDataURL } from "../../../wailsjs/go/main/App";
import { attachmentHasDocumentPreview } from "../preview/filePreviewKind";
import { AttachmentDocumentPreview } from "./AttachmentDocumentPreview";
import { AttachmentImageThumbnail } from "./AttachmentImagePreview";
import type { AttachmentInfo } from "./useBufferQueue";
import { isImageFilePath } from "./useAIAssistant";
import { ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT } from "./attachmentOpeningPreview";
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

function attachmentKindLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    if (!raw) return "FILE";
    return raw.slice(0, 4).toUpperCase();
}

function showDocumentRow(isImage: boolean, name: string): boolean {
    return !isImage && attachmentHasDocumentPreview(name);
}

function attachmentFileName(filePath: string): string {
    return filePath.split(/[\/\\]/).pop() || filePath;
}

const visuallyHiddenStyle: CSSProperties = {
    position: "absolute",
    width: 1,
    height: 1,
    padding: 0,
    margin: -1,
    overflow: "hidden",
    clip: "rect(0, 0, 0, 0)",
    whiteSpace: "nowrap",
    border: 0,
};

function textGraphemes(value: string): string[] {
    // Segment by user-perceived characters so emoji sequences, combining marks,
    // and CJK names remain intact when a filename is abbreviated. The desktop
    // runtime supports Intl.Segmenter; keep the fallback defensive for older
    // WebViews that expose Intl but not Segmenter.
    const Segmenter = (Intl as typeof Intl & { Segmenter?: typeof Intl.Segmenter }).Segmenter;
    if (typeof Segmenter === "function") {
        return Array.from(new Segmenter().segment(value), ({ segment }) => segment);
    }
    return Array.from(value);
}

function abbreviateAttachmentFileName(fileName: string, maxLength = 42): string {
    const fileNameCharacters = textGraphemes(fileName);
    if (fileNameCharacters.length <= maxLength) return fileName;

    const extensionIndex = fileName.lastIndexOf(".");
    const extension = extensionIndex > 0 ? fileName.slice(extensionIndex) : "";
    const stem = extension ? fileName.slice(0, extensionIndex) : fileName;
    const extensionCharacters = textGraphemes(extension);
    const stemCharacters = textGraphemes(stem);
    const availableStemLength = maxLength - extensionCharacters.length - 3;

    if (availableStemLength < 6) return `${fileNameCharacters.slice(0, Math.max(1, maxLength - 3)).join("")}...`;

    const headLength = Math.ceil(availableStemLength * 0.62);
    const tailLength = availableStemLength - headLength;
    return `${stemCharacters.slice(0, headLength).join("")}...${stemCharacters.slice(-tailLength).join("")}${extension}`;
}

function AttachmentTypeBadge({ label, theme }: { label: string; theme: Theme }) {
    return (
        <span
            aria-hidden="true"
            style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: "30px",
                height: "30px",
                flexShrink: 0,
                borderRadius: "5px",
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

function AttachmentVisual({ filePath, fileName, isImage, lang, theme }: {
    filePath: string;
    fileName: string;
    isImage: boolean;
    lang: string;
    theme: Theme;
}) {
    const [preview, setPreview] = useState("");
    const [previewFailed, setPreviewFailed] = useState(false);
    const [documentFailed, setDocumentFailed] = useState(false);
    const showDocument = !isImage && attachmentHasDocumentPreview(fileName || filePath) && !documentFailed;

    useEffect(() => {
        setDocumentFailed(false);
    }, [filePath, fileName]);

    useEffect(() => {
        if (!isImage) {
            setPreview("");
            setPreviewFailed(false);
            return;
        }
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
    }, [filePath, isImage]);

    if (showDocument) {
        return (
            <AttachmentDocumentPreview
                filePath={filePath}
                fileName={fileName}
                theme={theme}
                lang={lang}
                onFail={() => setDocumentFailed(true)}
            />
        );
    }

    if (preview) {
        // Resolved bytes can still fail to decode in the <img>; fall back to
        // the type badge instead of a broken-image glyph.
        return <AttachmentImageThumbnail src={preview} filePath={filePath} fileName={fileName || "attached image"} lang={lang} theme={theme} title={filePath} frameStyle={{ width: "30px", height: "30px", borderRadius: "4px", flexShrink: 0, border: "none", background: "transparent" }} onImageError={() => { setPreview(""); setPreviewFailed(true); }} />;
    }
    if (isImage && !previewFailed) {
        return <span aria-label={fileName} className="aas-thumb-slot" />;
    }
    return <AttachmentTypeBadge label={isImage ? "IMG" : attachmentKindLabel(fileName, "")} theme={theme} />;
}

function attachmentRemoveLabel(lang: string, fileName: string): string {
    return lang.startsWith("en") ? `Remove ${fileName}` : `移除 ${fileName}`;
}

function attachmentItemStyle(theme: Theme, documentRow: boolean): CSSProperties {
    const row = attachmentRowStyle(theme);
    if (!documentRow) return { ...row, alignItems: "flex-start" };
    // Hug the card so the remove button stays on its corner. max-content can
    // still shrink to the composer width; a 1fr track would pin the button to
    // the far edge once the preview falls back to a small badge.
    return {
        ...row,
        alignItems: "start",
        width: "max-content",
        maxWidth: "100%",
        minWidth: 0,
        boxSizing: "border-box",
        padding: 0,
        background: "transparent",
        border: "none",
        display: "grid",
        gridTemplateColumns: "minmax(0, max-content) auto",
        gap: 6,
    };
}

function attachmentRowStyle(theme: Theme): CSSProperties {
    return {
        display: "flex",
        alignItems: "center",
        gap: "5px",
        minWidth: 0,
        padding: "3px",
        borderRadius: "6px",
        background: theme.codeBlockBg,
        border: `1px solid ${theme.codeBlockBorder}`,
        color: theme.text,
        fontSize: "12px",
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
    const hasDocumentPreview = pendingAttachments.some((att) => !att.isImage && attachmentHasDocumentPreview(att.fileName || att.filePath))
        || selectedFilePaths.some((filePath) => !isImageFilePath(filePath) && attachmentHasDocumentPreview(filePath));

    if (!hasAttachments) return null;

    return (
        <div
            className="ai-attachment-strip"
            data-testid={pendingAttachmentsTestId}
            role="list"
            aria-label={lang.startsWith("en") ? "Attached files" : "已附加文件"}
            style={{
                display: hasDocumentPreview ? "flex" : undefined,
                flexDirection: hasDocumentPreview ? "column" : undefined,
                width: hasDocumentPreview ? "min(100%, 680px)" : undefined,
                gap: "5px",
                alignSelf: "flex-start",
                minWidth: 0,
                maxHeight: hasDocumentPreview ? `${ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT + 48}px` : "120px",
                overflowY: "auto",
                overscrollBehavior: "contain",
                paddingRight: "4px",
                scrollbarGutter: "stable",
                ["--ai-attachment-remove-hover-bg" as string]: t.errorBg,
                ["--ai-attachment-remove-hover-color" as string]: t.errorText,
                ["--ai-attachment-focus-color" as string]: t.btnColor,
            }}
        >
            {pendingAttachments.length > 0 && (
                pendingAttachments.map((att, index) => {
                    const fileName = att.fileName || attachmentFileName(att.filePath);
                    const displayName = abbreviateAttachmentFileName(fileName);
                    return (
                        <div key={att.filePath + "-" + index} className="ai-attachment-row" role="listitem" title={att.filePath} style={attachmentItemStyle(t, showDocumentRow(att.isImage, att.fileName || att.filePath))}>
                            <AttachmentVisual filePath={att.filePath} fileName={fileName} isImage={att.isImage} lang={lang} theme={t} />
                            <span title={att.filePath} aria-label={fileName} style={visuallyHiddenStyle}>{displayName}</span>
                            <button
                                type="button"
                                className="ai-attachment-remove"
                                onClick={() => setPendingAttachments(prev => prev.filter(candidate => candidate !== att))}
                                disabled={cancelPending}
                                style={removeButtonStyle(t, cancelPending)}
                                title={attachmentRemoveLabel(lang, fileName)}
                                aria-label={attachmentRemoveLabel(lang, fileName)}
                            >
                                {"×"}
                            </button>
                        </div>
                    );
                })
            )}
            {selectedFilePaths.length > 0 && (
                selectedFilePaths.map((filePath: string, index: number) => {
                    const fileName = attachmentFileName(filePath);
                    const displayName = abbreviateAttachmentFileName(fileName);
                    return (
                        <div key={filePath + index} className="ai-attachment-row" role="listitem" title={filePath} style={attachmentItemStyle(t, showDocumentRow(isImageFilePath(filePath), filePath))}>
                            <AttachmentVisual filePath={filePath} fileName={fileName} isImage={isImageFilePath(filePath)} lang={lang} theme={t} />
                            <span title={filePath} aria-label={fileName} style={visuallyHiddenStyle}>{displayName}</span>
                            <button
                                type="button"
                                className="ai-attachment-remove"
                                onClick={() => removeSelectedFile ? removeSelectedFile(filePath) : clearSelectedFile?.()}
                                disabled={cancelPending}
                                style={removeButtonStyle(t, cancelPending)}
                                title={attachmentRemoveLabel(lang, fileName)}
                                aria-label={attachmentRemoveLabel(lang, fileName)}
                            >
                                {"×"}
                            </button>
                        </div>
                    );
                })
            )}
        </div>
    );
}
