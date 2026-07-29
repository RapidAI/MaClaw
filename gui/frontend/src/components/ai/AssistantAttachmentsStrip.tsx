import type { CSSProperties, Dispatch, SetStateAction } from "react";
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

function attachmentKindLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    if (!raw) return "FILE";
    return raw.slice(0, 4).toUpperCase();
}

function attachmentFileName(filePath: string): string {
    return filePath.split(/[\/\\]/).pop() || filePath;
}

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
                width: "34px",
                height: "34px",
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

function attachmentRemoveLabel(lang: string, fileName: string): string {
    return lang.startsWith("en") ? `Remove ${fileName}` : `移除 ${fileName}`;
}

function attachmentRowStyle(theme: Theme): CSSProperties {
    return {
        display: "flex",
        alignItems: "center",
        gap: "8px",
        minWidth: 0,
        padding: "6px 8px",
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
                minWidth: 0,
                maxHeight: "184px",
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
                    const typeLabel = attachmentKindLabel(fileName, att.extension);
                    return (
                        <div key={att.filePath + "-" + index} className="ai-attachment-row" role="listitem" title={att.filePath} style={attachmentRowStyle(t)}>
                            {att.thumbnailDataUrl ? (
                                <img
                                    src={att.thumbnailDataUrl}
                                    alt={fileName || "pasted image"}
                                    style={{ width: "34px", height: "34px", objectFit: "cover", borderRadius: "4px", flexShrink: 0 }}
                                />
                            ) : (
                                <AttachmentTypeBadge label={typeLabel} theme={t} />
                            )}
                            <span title={att.filePath} aria-label={fileName} style={{ minWidth: 0, flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>{displayName}</span>
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
                        <div key={filePath + index} className="ai-attachment-row" role="listitem" title={filePath} style={attachmentRowStyle(t)}>
                            <AttachmentTypeBadge label={isImageFilePath(filePath) ? "IMG" : attachmentKindLabel(fileName, "")} theme={t} />
                            <span title={filePath} aria-label={fileName} style={{ minWidth: 0, flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>{displayName}</span>
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
