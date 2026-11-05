import React, { useCallback, useContext, useEffect, useRef, useState, type CSSProperties, type MouseEvent, type ReactNode } from "react";
import { BrowserOpenURL } from "../../../wailsjs/runtime";

/**
 * Chat images and links are painted by the markdown renderer.
 * They are not agent tools: a click only opens a URL the model already wrote,
 * or shows that same URL in the preview surface the host provides.
 */

const DATA_IMAGE_SRC = /^data:image\/(?:png|jpeg|gif|webp);base64,[a-z0-9+/=\s]+$/i;
const DATA_IMAGE_MAX = 1_500_000;

/** http(s) URL with no embedded credentials. Empty when the text is not one. */
export function safeHttpURL(raw: string): string {
    const text = String(raw || "").trim();
    if (!/^https?:\/\//i.test(text)) return "";
    try {
        const url = new URL(text);
        if (url.protocol !== "http:" && url.protocol !== "https:") return "";
        if (url.username || url.password) return "";
        return url.href;
    } catch {
        return "";
    }
}

/**
 * Image src the chat may assign to an <img>.
 * Remote http(s) and small raster data URLs only. Scriptable schemes and
 * local filesystem paths stay out of model-authored markup.
 */
export function safeMarkdownImageSrc(raw: string): string {
    const text = String(raw || "").trim();
    if (!text || /^(javascript|vbscript|file|blob):/i.test(text)) return "";
    const http = safeHttpURL(text);
    if (http) return http;
    if (!DATA_IMAGE_SRC.test(text) || text.length > DATA_IMAGE_MAX) return "";
    return text.replace(/\s+/g, "");
}

/**
 * Document markdown may also name a relative image next to the file.
 * Scheme-bearing and parent-traversal paths stay rejected.
 */
export function documentMarkdownImageSrc(raw: string): string {
    const safe = safeMarkdownImageSrc(raw);
    if (safe) return safe;
    const text = String(raw || "").trim();
    if (!text || /[\s<>"']/.test(text) || text.includes("..") || text.includes("\\") || text.includes("\0")) return "";
    // Protocol-relative URLs would load a remote host from document markdown.
    if (text.startsWith("//")) return "";
    if (/^[a-z][a-z0-9+.-]*:/i.test(text)) return "";
    return text;
}

/** Remote image the assistant preview pane can show without a workspace file. */
export function safeRemoteImageURL(raw: string): string {
    return safeHttpURL(raw);
}

/** Nested emphasis only needs another markdown pass when it can hide a link, image, URL, or more emphasis. */
export function inlineHasNestedMarkup(inner: string): boolean {
    return inner.includes("[") || inner.includes("://") || inner.includes("*") || inner.includes("`") || inner.includes("~~") || inner.includes("==");
}

/** A label that is only emphasis marks is not a label. Show the URL instead. */
export function visibleMarkdownLinkLabel(label: string, href: string): string {
    const trimmed = String(label || "").trim();
    const wrapped = trimmed.match(/^(\*+|_+)([\s\S]+?)\1$/);
    if (wrapped && wrapped[2].trim()) return wrapped[2].trim();
    if (!trimmed || /^[*_\s]+$/.test(trimmed)) return href;
    return trimmed;
}

/** Drop sentence punctuation that the autolink scan would otherwise swallow. */
export function trimAutolink(raw: string): string {
    let href = raw;
    while (/[.,;:!?，。；：！？、）】》」』]$/.test(href)) href = href.slice(0, -1);
    while (href.endsWith(")") && href.split(")").length > href.split("(").length) href = href.slice(0, -1);
    return href;
}

/**
 * Markdown destination starting at the opening `(`.
 * Balanced parentheses stay in the URL. A quoted title after the URL is dropped.
 */
export function readMarkdownDestination(text: string, openParen: number): { url: string; end: number } | null {
    if (text[openParen] !== "(") return null;
    let i = openParen + 1;
    while (i < text.length && (text[i] === " " || text[i] === "\t")) i++;
    if (i >= text.length) return null;
    if (text[i] === "<") {
        const close = text.indexOf(">", i + 1);
        if (close < 0) return null;
        const url = text.slice(i + 1, close).trim();
        let j = close + 1;
        while (j < text.length && text[j] !== ")" && text[j] !== "\n") j++;
        if (text[j] !== ")") return null;
        return url ? { url, end: j + 1 } : null;
    }
    const start = i;
    let depth = 1;
    while (i < text.length) {
        const c = text[i];
        if (c === "\n") return null;
        if (c === "(") depth++;
        else if (c === ")") {
            depth--;
            if (depth === 0) {
                let url = text.slice(start, i).trim();
                const titleAt = url.search(/\s+["']/);
                if (titleAt >= 0) url = url.slice(0, titleAt).trim();
                if (!url) return null;
                return { url, end: i + 1 };
            }
        }
        i++;
    }
    return null;
}

/** Link with a balanced destination. The URL is not required to be http(s). */
export function readMarkdownLinkAt(text: string, index: number): { label: string; url: string; end: number } | null {
    if (text[index] !== "[") return null;
    const labelEnd = text.indexOf("]", index + 1);
    if (labelEnd < 0 || text[labelEnd + 1] !== "(") return null;
    const label = text.slice(index + 1, labelEnd);
    if (!label) return null;
    const dest = readMarkdownDestination(text, labelEnd + 1);
    if (!dest) return null;
    return { label, url: dest.url, end: dest.end };
}

/** http(s) link with a balanced destination, so a `)` inside the URL is kept. */
export function parseMarkdownLinkAt(text: string, index: number): { label: string; href: string; end: number } | null {
    const link = readMarkdownLinkAt(text, index);
    if (!link) return null;
    const href = safeHttpURL(link.url);
    if (!href) return null;
    return { label: link.label, href, end: link.end };
}

/**
 * `[![alt](image)](page)`.
 * The first `]` belongs to the image, so a normal link read would stop there.
 */
export function readWrappedMarkdownImage(text: string, index: number): { alt: string; url: string; pageURL: string; end: number } | null {
    if (text[index] !== "[" || text[index + 1] !== "!") return null;
    const image = readMarkdownImageAt(text, index + 1);
    if (!image || text[image.end] !== "]" || text[image.end + 1] !== "(") return null;
    const page = readMarkdownDestination(text, image.end + 1);
    if (!page) return null;
    return { alt: image.alt, url: image.url, pageURL: page.url, end: page.end };
}

/** Image syntax with a balanced destination. The src may still be unsafe. */
export function readMarkdownImageAt(text: string, index: number): { alt: string; url: string; end: number } | null {
    if (text[index] !== "!" || text[index + 1] !== "[") return null;
    const altEnd = text.indexOf("]", index + 2);
    if (altEnd < 0 || text[altEnd + 1] !== "(") return null;
    const dest = readMarkdownDestination(text, altEnd + 1);
    if (!dest) return null;
    return { alt: text.slice(index + 2, altEnd), url: dest.url, end: dest.end };
}

export function parseMarkdownImageAt(
    text: string,
    index: number,
    acceptSrc: (raw: string) => string = safeMarkdownImageSrc,
): { alt: string; src: string; end: number } | null {
    const image = readMarkdownImageAt(text, index);
    if (!image) return null;
    const src = acceptSrc(image.url);
    if (!src) return null;
    return { alt: image.alt, src, end: image.end };
}

/**
 * The next image that starts before `until`.
 * An unsafe src comes back empty so the caller can skip the whole syntax
 * instead of leaving a tail or turning the inner URL into a link.
 */
export function findMarkdownImage(text: string, from: number, until: number): { alt: string; src: string; index: number; end: number } | null {
    let i = text.indexOf("![", from);
    while (i !== -1 && i < until) {
        const raw = readMarkdownImageAt(text, i);
        if (raw && raw.end > i) return { alt: raw.alt, src: safeMarkdownImageSrc(raw.url), index: i, end: raw.end };
        i = text.indexOf("![", i + 2);
    }
    return null;
}

function shortHash(value: string): string {
    let hash = 5381;
    for (let i = 0; i < value.length; i++) hash = ((hash * 33) ^ value.charCodeAt(i)) >>> 0;
    return hash.toString(16);
}

function imagePreviewFileName(alt: string): string {
    const cleaned = alt.replace(/[\\/:*?"<>|\r\n#]+/g, " ").replace(/\s+/g, " ").trim().slice(0, 40);
    return `${cleaned || "image"}.png`;
}

/**
 * Identity for a chat image opened in the assistant preview.
 * The path is not a URL and not a workspace file, so the web preview and the
 * file loader do not treat it as a page or a disk read.
 */
export function chatImagePreviewIdentity(src: string, alt: string): { filePath: string; fileName: string; remoteImageURL: string } {
    const remoteImageURL = safeRemoteImageURL(src);
    return {
        filePath: `chat-image-${shortHash(remoteImageURL || src)}.png`,
        fileName: imagePreviewFileName(alt),
        remoteImageURL,
    };
}

export type MarkdownImagePreviewHandler = (src: string, alt: string) => boolean;

const MarkdownImagePreviewContext = React.createContext<MarkdownImagePreviewHandler | null>(null);

export function MarkdownImagePreviewProvider({
    onPreviewImage,
    children,
}: {
    onPreviewImage: MarkdownImagePreviewHandler;
    children: React.ReactNode;
}) {
    return (
        <MarkdownImagePreviewContext.Provider value={onPreviewImage}>
            {children}
        </MarkdownImagePreviewContext.Provider>
    );
}

export function MarkdownExternalLink({
    href,
    color,
    style,
    children,
}: {
    href: string;
    color?: string;
    style?: CSSProperties;
    children: React.ReactNode;
}) {
    const safe = safeHttpURL(href);
    if (!safe) {
        return <span style={{ color, ...style }}>{children}</span>;
    }
    const open = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        BrowserOpenURL(safe);
    };
    return (
        <a
            data-testid="markdown-external-link"
            href={safe}
            title={safe}
            rel="noreferrer"
            onClick={open}
            onAuxClick={open}
            style={{ color, textDecoration: "underline", cursor: "pointer", ...style }}
        >
            {children}
        </a>
    );
}

const zoomCloseButtonStyle: CSSProperties = {
    position: "absolute",
    top: 16,
    right: 16,
    zIndex: 1,
    width: 36,
    height: 36,
    border: "none",
    borderRadius: 18,
    background: "#ffffff",
    color: "#0f172a",
    fontSize: 22,
    lineHeight: 1,
    cursor: "pointer",
};

/** Full-screen image preview. The picture does not dismiss it; the corner button, the backdrop, and Escape do. */
export function ImageZoomOverlay({
    testId,
    closeTestId,
    closeLabel,
    label,
    onClose,
    children,
}: {
    testId: string;
    closeTestId: string;
    closeLabel: string;
    label: string;
    onClose: () => void;
    children: ReactNode;
}) {
    const closeRef = useRef<HTMLButtonElement>(null);
    useEffect(() => {
        // Capture so this dialog owns Escape before a window-level handler
        // (伴读 fullscreen exit, an open menu) sees the same keypress.
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            onClose();
        };
        window.addEventListener("keydown", onKey, true);
        closeRef.current?.focus();
        return () => window.removeEventListener("keydown", onKey, true);
    }, [onClose]);
    return (
        <div
            role="dialog"
            aria-modal="true"
            aria-label={label}
            data-testid={testId}
            onClick={onClose}
            style={{
                position: "fixed",
                inset: 0,
                background: "rgba(15,23,42,0.72)",
                display: "grid",
                placeItems: "center",
                zIndex: 80,
                padding: 24,
            }}
        >
            <div onClick={(event) => event.stopPropagation()} style={{ width: "max-content", height: "max-content", maxWidth: "90vw", maxHeight: "86vh" }}>
                {children}
            </div>
            <button
                ref={closeRef}
                type="button"
                data-testid={closeTestId}
                aria-label={closeLabel}
                title={closeLabel}
                onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    onClose();
                }}
                style={zoomCloseButtonStyle}
            >
                ×
            </button>
        </div>
    );
}

export function MarkdownImagePreview({
    src,
    alt,
    block = false,
    className,
    document = false,
}: {
    src: string;
    alt: string;
    block?: boolean;
    className?: string;
    /** Relative document images. Chat markup stays on http(s) and raster data URLs. */
    document?: boolean;
}) {
    const previewImage = useContext(MarkdownImagePreviewContext);
    const [zoom, setZoom] = useState(false);
    const [failed, setFailed] = useState(false);
    const closeZoom = useCallback(() => setZoom(false), []);
    const safe = document ? documentMarkdownImageSrc(src) : safeMarkdownImageSrc(src);
    if (!safe) return alt ? <span>{alt}</span> : null;
    const open = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (safeRemoteImageURL(safe) && previewImage?.(safe, alt)) return;
        setZoom(true);
    };
    if (failed) {
        return (
            <MarkdownExternalLink href={safe}>
                {alt || safe}
            </MarkdownExternalLink>
        );
    }
    return (
        <>
            <button
                type="button"
                data-testid="markdown-image-preview"
                onClick={open}
                title={alt || safe}
                aria-label={alt || safe}
                style={{
                    display: block ? "block" : "inline-block",
                    margin: block ? "6px 0" : "2px 0",
                    padding: 0,
                    border: "none",
                    background: "transparent",
                    maxWidth: "100%",
                    cursor: "zoom-in",
                    verticalAlign: "middle",
                }}
            >
                <img
                    src={safe}
                    alt={alt}
                    className={className}
                    referrerPolicy="no-referrer"
                    draggable={false}
                    loading="lazy"
                    onError={() => setFailed(true)}
                    style={{
                        display: "block",
                        maxWidth: "100%",
                        maxHeight: block ? "320px" : "180px",
                        objectFit: "contain",
                        borderRadius: 6,
                        background: "#ffffff",
                    }}
                />
            </button>
            {block && alt ? (
                <div data-testid="markdown-image-caption" style={{ fontSize: 12, marginTop: 4 }}>{alt}</div>
            ) : null}
            {zoom ? (
                <ImageZoomOverlay
                    testId="markdown-image-zoom"
                    closeTestId="markdown-image-zoom-close"
                    closeLabel="关闭"
                    label={alt || safe}
                    onClose={closeZoom}
                >
                    <img
                        src={safe}
                        alt={alt}
                        referrerPolicy="no-referrer"
                        draggable={false}
                        style={{ display: "block", maxWidth: "90vw", maxHeight: "86vh", background: "#ffffff", borderRadius: 6 }}
                    />
                </ImageZoomOverlay>
            ) : null}
        </>
    );
}

/** A linked image stays a picture. The page URL is a separate link, so the picture click still previews. */
export function MarkdownLinkedImage({
    alt,
    src,
    pageURL,
    block = false,
    className,
    document = false,
    linkColor,
    linkStyle,
}: {
    alt: string;
    src: string;
    pageURL: string;
    block?: boolean;
    className?: string;
    document?: boolean;
    linkColor?: string;
    linkStyle?: CSSProperties;
}) {
    const page = safeHttpURL(pageURL);
    const imageSrc = document ? documentMarkdownImageSrc(src) : safeMarkdownImageSrc(src);
    const label = page ? visibleMarkdownLinkLabel(alt, page) : "";
    return (
        <span style={block ? { display: "block" } : undefined}>
            {imageSrc ? <MarkdownImagePreview src={imageSrc} alt={alt} block={block} className={className} document={document} /> : null}
            {imageSrc && page ? " " : null}
            {page ? <MarkdownExternalLink href={page} color={linkColor} style={linkStyle}>{label}</MarkdownExternalLink> : (!imageSrc && alt ? <span>{alt}</span> : null)}
        </span>
    );
}
