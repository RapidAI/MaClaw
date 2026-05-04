import type { Dispatch, SetStateAction } from "react";
import type { AttachmentInfo } from "./useBufferQueue";
import { isImageFilePath } from "./useAIAssistant";
import { baseActionBtnStyle, type Theme } from "./aiAssistantPanelTheme";

interface AssistantAttachmentsStripProps {
    cancelPending: boolean;
    clearSelectedFile?: () => void;
    lang: string;
    pendingAttachments: AttachmentInfo[];
    removeSelectedFile?: (index: number) => void;
    selectedFilePaths: string[];
    setPendingAttachments: Dispatch<SetStateAction<AttachmentInfo[]>>;
    theme: Theme;
}

export function AssistantAttachmentsStrip({
    cancelPending,
    clearSelectedFile,
    lang,
    pendingAttachments,
    removeSelectedFile,
    selectedFilePaths,
    setPendingAttachments,
    theme: t,
}: AssistantAttachmentsStripProps) {
    return (
        <>
            {pendingAttachments.length > 0 && (
                <div data-testid="ai-pending-attachments" style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
                    {pendingAttachments.map((att, index) => {
                        const showImageOnly = !!att.thumbnailDataUrl && att.isImage;
                        return (
                            <div
                                key={att.filePath + "-" + index}
                                style={{
                                    display: "flex",
                                    alignItems: "center",
                                    gap: showImageOnly ? "5px" : "6px",
                                    maxWidth: showImageOnly ? "72px" : "220px",
                                    padding: showImageOnly ? "4px 5px" : "5px 7px",
                                    borderRadius: "7px",
                                    background: t.codeBlockBg,
                                    border: `1px solid ${t.codeBlockBorder}`,
                                    color: t.text,
                                    fontSize: "11px",
                                }}
                                title={att.filePath}
                            >
                                {att.thumbnailDataUrl ? (
                                    <img
                                        src={att.thumbnailDataUrl}
                                        alt={att.fileName || "pasted image"}
                                        style={{ width: "34px", height: "34px", objectFit: "cover", borderRadius: "4px", flexShrink: 0 }}
                                    />
                                ) : (
                                    <span>{"file"}</span>
                                )}
                                {!showImageOnly && (
                                    <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{att.fileName}</span>
                                )}
                                <button type="button" onClick={() => setPendingAttachments(prev => prev.filter((_, i) => i !== index))} style={{ border: "none", background: "transparent", color: t.textMuted, cursor: "pointer", padding: showImageOnly ? "0 2px" : undefined }}>{"x"}</button>
                            </div>
                        );
                    })}
                </div>
            )}
            {selectedFilePaths.length > 0 && (
                <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                    {selectedFilePaths.map((filePath: string, index: number) => {
                        const fileName = filePath.split(/[\/\\]/).pop() || filePath;
                        return (
                            <div key={filePath + index} style={{
                                display: "flex",
                                alignItems: "center",
                                gap: "8px",
                                minWidth: 0,
                                padding: "6px 8px",
                                borderRadius: "6px",
                                background: t.codeBlockBg,
                                border: `1px solid ${t.codeBlockBorder}`,
                                color: t.text,
                                fontSize: "12px",
                            }}>
                                <span style={{ color: t.pathColor, flexShrink: 0 }}>{isImageFilePath(filePath) ? "img" : "file"}</span>
                                <div style={{ minWidth: 0, flex: 1 }} title={filePath}>
                                    <div style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", fontWeight: 600 }}>{fileName}</div>
                                    <div style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", color: t.textMuted, fontSize: "11px" }}>{filePath}</div>
                                </div>
                                <button
                                    type="button"
                                    onClick={() => removeSelectedFile ? removeSelectedFile(index) : clearSelectedFile?.()}
                                    disabled={cancelPending}
                                    style={{
                                        ...baseActionBtnStyle,
                                        color: t.errorText,
                                        border: `1px solid ${t.errorBorder}`,
                                        background: "transparent",
                                        opacity: cancelPending ? 0.5 : 1,
                                    }}
                                    title={lang === "en" ? "Clear selected file" : "\u6e05\u9664\u5df2\u9009\u6587\u4ef6"}
                                >
                                    {"x"}
                                </button>
                            </div>
                        );
                    })}
                </div>
            )}
        </>
    );
}
