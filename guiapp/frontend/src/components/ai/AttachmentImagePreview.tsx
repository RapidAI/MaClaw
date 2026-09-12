import { useCallback, useRef, useState, type CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { ImagePreviewOverlay } from "./AttachmentImagePreviewOverlay";

export interface AttachmentImageThumbnailProps {
    /** Thumbnail bytes already resolved by the caller. */
    src: string;
    filePath: string;
    fileName: string;
    lang: string;
    theme: Theme;
    /** Box the thumbnail fills, e.g. the chat chip or composer chip frame. */
    frameStyle: CSSProperties;
    /** Overrides for the image inside the frame; defaults to a cropped fill. */
    imageStyle?: CSSProperties;
    /** Hover text; defaults to the preview action label. */
    title?: string;
    /** Called when `src` cannot be displayed, so the caller can re-resolve it. */
    onImageError?: () => void;
}

/** Attachment thumbnail that opens a full-image preview when clicked. */
export function AttachmentImageThumbnail({ src, filePath, fileName, lang, theme: t, frameStyle, imageStyle, title, onImageError }: AttachmentImageThumbnailProps) {
    const [previewOpen, setPreviewOpen] = useState(false);
    const triggerRef = useRef<HTMLButtonElement | null>(null);
    const isZh = !lang.startsWith("en");
    const label = isZh ? `预览图片 ${fileName}` : `Preview image ${fileName}`;
    const close = useCallback(() => {
        setPreviewOpen(false);
        triggerRef.current?.focus();
    }, []);

    return (
        <>
            <button
                ref={triggerRef}
                type="button"
                onClick={() => setPreviewOpen(true)}
                title={title || label}
                aria-label={label}
                data-testid="attachment-image-thumbnail"
                style={{
                    ...frameStyle,
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    padding: 0,
                    overflow: "hidden",
                    cursor: "zoom-in",
                }}
            >
                <img src={src} alt="" draggable={false} onError={onImageError} style={{ width: "100%", height: "100%", objectFit: "cover", display: "block", ...imageStyle }} />
            </button>
            {previewOpen && (
                <ImagePreviewOverlay
                    filePath={filePath}
                    fileName={fileName}
                    thumbnailSrc={src}
                    lang={lang}
                    theme={t}
                    onClose={close}
                />
            )}
        </>
    );
}
