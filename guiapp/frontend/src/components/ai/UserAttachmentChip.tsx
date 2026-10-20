import React from "react";
import { AIAssistantAttachmentPreviewDataURL } from "../../../wailsjs/go/main/App";
import { attachmentHasDocumentPreview } from "../preview/filePreviewKind";
import type { ChatAttachment, ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { AttachmentDocumentPreview } from "./AttachmentDocumentPreview";
import { AttachmentImageThumbnail } from "./AttachmentImagePreview";

function compactAttachmentLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    return raw ? raw.slice(0, 4).toUpperCase() : "FILE";
}

/** Document shots render beside the text bubble so the 76% text width cannot shrink them. */
export function partitionUserAttachments(attachments: readonly ChatAttachment[]): { inline: ChatAttachment[]; documents: ChatAttachment[] } {
    const inline: ChatAttachment[] = [];
    const documents: ChatAttachment[] = [];
    for (const attachment of attachments) {
        if (!attachment.isImage && attachmentHasDocumentPreview(attachment.fileName || attachment.filePath)) documents.push(attachment);
        else inline.push(attachment);
    }
    return { inline, documents };
}

export function UserAttachmentChip({ attachment, theme, lang }: { attachment: NonNullable<ChatMessage["attachments"]>[number]; theme: Theme; lang: string }) {
    // Composer object URLs are revoked when the message is sent, so they must
    // not cross into the transcript. Always resolve a fresh data URL from the
    // saved local attachment for reliable rendering and history restoration.
    const [thumbnail, setThumbnail] = React.useState("");
    const [previewFailed, setPreviewFailed] = React.useState(false);
    const [documentFailed, setDocumentFailed] = React.useState(false);
    const showDocument = !attachment.isImage && attachmentHasDocumentPreview(attachment.fileName || attachment.filePath) && !documentFailed;

    React.useEffect(() => {
        setDocumentFailed(false);
    }, [attachment.filePath, attachment.fileName]);

    React.useEffect(() => {
        if (!attachment.isImage) {
            setThumbnail("");
            setPreviewFailed(false);
            return;
        }
        let active = true;
        setPreviewFailed(false);
        void AIAssistantAttachmentPreviewDataURL(attachment.filePath)
            .then(dataUrl => {
                if (!active) return;
                const resolved = String(dataUrl || "");
                setThumbnail(resolved);
                setPreviewFailed(!resolved);
            })
            .catch(() => {
                if (!active) return;
                setThumbnail("");
                setPreviewFailed(true);
            });
        return () => { active = false; };
    }, [attachment.filePath, attachment.isImage]);

    // An attached picture is user content, not metadata: the transcript shows a
    // readable preview (click zooms to the full image) instead of a file badge.
    // A document that cannot be previewed keeps the compact type badge.
    const previewStyle: React.CSSProperties = {
        display: "inline-flex",
        maxWidth: "100%",
        alignItems: "center",
        justifyContent: "center",
        overflow: "hidden",
        flexShrink: 0,
        borderRadius: 8,
        border: `1px solid ${theme.codeBlockBorder}`,
        background: "transparent",
    };

    const chipStyle: React.CSSProperties = {
        display: "inline-flex",
        width: 30,
        height: 30,
        alignItems: "center",
        justifyContent: "center",
        overflow: "hidden",
        flexShrink: 0,
        borderRadius: 4,
        background: theme.codeBlockBg,
        border: `1px solid ${theme.codeBlockBorder}`,
        color: theme.pathColor,
        fontSize: 8,
        fontWeight: 800,
        lineHeight: 1,
    };

    if (showDocument) {
        return (
            <AttachmentDocumentPreview
                filePath={attachment.filePath}
                fileName={attachment.fileName}
                extension={attachment.extension}
                theme={theme}
                lang={lang}
                variant="shot"
                onFail={() => setDocumentFailed(true)}
            />
        );
    }

    if (thumbnail) {
        return (
            <AttachmentImageThumbnail
                src={thumbnail}
                filePath={attachment.filePath}
                fileName={attachment.fileName}
                lang={lang}
                theme={theme}
                frameStyle={previewStyle}
                imageStyle={{ width: "auto", height: "auto", maxHeight: "140px", maxWidth: "100%", objectFit: "contain", display: "block" }}
                title={attachment.filePath}
                // Resolved bytes can still fail to decode in the <img>; fall
                // back to the type badge instead of a broken-image glyph.
                onImageError={() => { setThumbnail(""); setPreviewFailed(true); }}
            />
        );
    }
    if (attachment.isImage && !previewFailed) {
        return <span aria-label={attachment.fileName} style={chipStyle} />;
    }
    return (
        <span title={attachment.filePath} aria-label={attachment.fileName} style={chipStyle}>
            {compactAttachmentLabel(attachment.fileName, attachment.extension)}
        </span>
    );
}
