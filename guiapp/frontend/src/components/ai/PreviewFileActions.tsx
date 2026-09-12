import { useEffect, useRef, useState, type CSSProperties } from "react";
import { ImportMobileDocumentFromPath, ShowItemInFolder } from "../../../wailsjs/go/main/App";

function CloudUploadIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path
                d="M5 12.3h6a2.75 2.75 0 0 0 .9-5.4 3.55 3.55 0 0 0-6.6-.95 2.5 2.5 0 0 0-1.7 3.65A1.55 1.55 0 0 0 5 12.3Z"
                stroke="currentColor"
                strokeWidth="1.2"
                strokeLinejoin="round"
            />
            <path d="M8 11.2V7.4M6.3 9.1 8 7.4l1.7 1.7" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

function ShareIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M10 3H4.5A1.5 1.5 0 0 0 3 4.5v7A1.5 1.5 0 0 0 4.5 13h7a1.5 1.5 0 0 0 1.5-1.5V6.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
            <path d="M9.5 2.5H13.5V6.5M13 3 8.5 7.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

function FolderIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path
                d="M2 4.6A1.6 1.6 0 0 1 3.6 3h2.9l1.6 2h4.3A1.6 1.6 0 0 1 14 6.6v4.8a1.6 1.6 0 0 1-1.6 1.6H3.6A1.6 1.6 0 0 1 2 11.4V4.6Z"
                stroke="currentColor"
                strokeWidth="1.2"
                strokeLinejoin="round"
            />
        </svg>
    );
}

async function copyTextToClipboard(text: string): Promise<boolean> {
    try {
        if (navigator.clipboard?.writeText) {
            await navigator.clipboard.writeText(text);
            return true;
        }
    } catch {
        // fall through to the legacy path
    }
    try {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.left = "-9999px";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        return true;
    } catch {
        return false;
    }
}

export interface PreviewFileActionsProps {
    /** Local absolute path of the previewed file; renders nothing without one. */
    absPath?: string;
    lang: string;
    /** Icon color; defaults to inheriting the host's text color. */
    color?: string;
    /** Extra per-button overrides (e.g. size in compact headers). */
    buttonStyle?: CSSProperties;
}

/**
 * Action cluster shared by every file preview surface (code preview, image
 * preview, ...): upload to the mobile documents library, share (copy the file
 * path), and reveal the file in its OS folder.
 */
export function PreviewFileActions({ absPath, lang, color, buttonStyle }: PreviewFileActionsProps) {
    const isZh = !lang.startsWith("en");
    const [uploading, setUploading] = useState(false);
    const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
    const noticeTimer = useRef<number | undefined>(undefined);

    useEffect(() => () => {
        if (noticeTimer.current !== undefined) window.clearTimeout(noticeTimer.current);
    }, []);

    if (!absPath) return null;

    const flash = (ok: boolean, text: string) => {
        setNotice({ ok, text });
        if (noticeTimer.current !== undefined) window.clearTimeout(noticeTimer.current);
        noticeTimer.current = window.setTimeout(() => setNotice(null), 3000);
    };

    const handleUpload = async () => {
        if (uploading) return;
        setUploading(true);
        try {
            await ImportMobileDocumentFromPath(absPath);
            flash(true, isZh ? "已上传到文稿库" : "Uploaded to documents library");
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err || "");
            flash(false, message || (isZh ? "上传失败" : "Upload failed"));
        } finally {
            setUploading(false);
        }
    };

    const handleShare = async () => {
        const copied = await copyTextToClipboard(absPath);
        flash(copied, copied
            ? (isZh ? "已复制文件路径，可粘贴分享" : "File path copied — paste to share")
            : (isZh ? "复制失败" : "Copy failed"));
    };

    const handleReveal = () => {
        void ShowItemInFolder(absPath).catch(() => undefined);
    };

    const baseButtonStyle: CSSProperties = {
        width: 26,
        height: 26,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        border: "none",
        borderRadius: 6,
        background: "transparent",
        color: color || "inherit",
        cursor: "pointer",
        padding: 0,
        flexShrink: 0,
        ...buttonStyle,
    };

    const uploadLabel = isZh ? "上传到文稿库" : "Upload to documents library";
    const shareLabel = isZh ? "分享（复制文件路径）" : "Share (copy file path)";
    const revealLabel = isZh ? "打开文件夹" : "Show in folder";

    return (
        <span data-testid="preview-file-actions" style={{ display: "inline-flex", alignItems: "center", gap: 2, flexShrink: 0 }}>
            <button
                type="button"
                data-testid="preview-file-action-upload"
                onClick={() => { void handleUpload(); }}
                disabled={uploading}
                title={uploadLabel}
                aria-label={uploadLabel}
                style={{ ...baseButtonStyle, opacity: uploading ? 0.5 : 1, cursor: uploading ? "default" : "pointer" }}
            >
                <CloudUploadIcon />
            </button>
            <button
                type="button"
                data-testid="preview-file-action-share"
                onClick={() => { void handleShare(); }}
                title={shareLabel}
                aria-label={shareLabel}
                style={baseButtonStyle}
            >
                <ShareIcon />
            </button>
            <button
                type="button"
                data-testid="preview-file-action-reveal"
                onClick={handleReveal}
                title={revealLabel}
                aria-label={revealLabel}
                style={baseButtonStyle}
            >
                <FolderIcon />
            </button>
            {notice && (
                <span
                    data-testid="preview-file-actions-notice"
                    role="status"
                    style={{
                        fontSize: 11,
                        lineHeight: 1.3,
                        color: notice.ok ? (color || "inherit") : "#c0392b",
                        opacity: notice.ok ? 0.85 : 1,
                        whiteSpace: "nowrap",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        maxWidth: 260,
                        marginLeft: 4,
                    }}
                >
                    {notice.text}
                </span>
            )}
        </span>
    );
}
