import React from "react";
import { AIAssistantAttachmentPreviewDataURL } from "../../../wailsjs/go/main/App";
import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { AttachmentImageThumbnail } from "./AttachmentImagePreview";

function compactAttachmentLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    return raw ? raw.slice(0, 4).toUpperCase() : "FILE";
}

export function UserAttachmentChip({ attachment, theme, lang }: { attachment: NonNullable<ChatMessage["attachments"]>[number]; theme: Theme; lang: string }) {
    // Composer object URLs are revoked when the message is sent, so they must
    // not cross into the transcript. Always resolve a fresh data URL from the
    // saved local attachment for reliable rendering and history restoration.
    const [thumbnail, setThumbnail] = React.useState("");
    const [previewFailed, setPreviewFailed] = React.useState(false);

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

    if (thumbnail) {
        return (
            <AttachmentImageThumbnail
                src={thumbnail}
                filePath={attachment.filePath}
                fileName={attachment.fileName}
                lang={lang}
                theme={theme}
                frameStyle={chipStyle}
                title={attachment.filePath}
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
