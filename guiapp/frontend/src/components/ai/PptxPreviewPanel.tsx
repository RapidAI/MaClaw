import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { AIAssistantAttachmentFullDataURL, PptxPreviewEnsure, PptxSlideThumbnailDataURL } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";

type PptxPreviewResult = {
    file_path?: string;
    output_dir?: string;
    slide_count?: number;
    rendered_count?: number;
    width?: number;
    format?: string;
    images?: string[];
    truncated?: boolean;
};

/** .pptx is the only deck format the pure-Go renderer supports. */
export function isPptxFileName(name: string): boolean {
    return name.toLowerCase().endsWith(".pptx");
}

export interface PptxPreviewPanelProps {
    /** Local absolute path of the .pptx deck. */
    absPath: string;
    theme: CodePreviewTheme;
    lang: string;
}

/**
 * Slide preview for a .pptx deck: left column lists per-page thumbnails, the
 * right side shows the selected page large. The first page is selected by
 * default. Rendering happens on demand in the Go backend (cached next to the
 * deck); slide images stream in as data URLs.
 */
export function PptxPreviewPanel({ absPath, theme, lang }: PptxPreviewPanelProps) {
    const isZh = lang.startsWith("zh");
    // Messages captured inside the fetch effect read lang through a ref so a
    // language toggle does not re-render every slide.
    const langRef = useRef(lang);
    useEffect(() => {
        langRef.current = lang;
    }, [lang]);
    const [images, setImages] = useState<string[] | null>(null);
    const [error, setError] = useState("");
    const [thumbs, setThumbs] = useState<ReadonlyMap<number, string>>(new Map());
    const [fullUrls, setFullUrls] = useState<ReadonlyMap<number, string>>(new Map());
    const [selected, setSelected] = useState(0);
    const versionRef = useRef(0);

    useEffect(() => {
        const version = ++versionRef.current;
        setImages(null);
        setError("");
        setThumbs(new Map());
        setFullUrls(new Map());
        setSelected(0);
        let cancelled = false;
        const alive = () => !cancelled && version === versionRef.current;
        const zh = () => langRef.current.startsWith("zh");
        void (async () => {
            try {
                const result = await PptxPreviewEnsure(absPath) as PptxPreviewResult;
                if (!alive()) return;
                const list = Array.isArray(result?.images) ? result.images.filter(Boolean) : [];
                if (list.length === 0) {
                    setError(zh() ? "该 PPT 没有可预览的页面" : "This deck has no slides to preview");
                    return;
                }
                setImages(list);
                // Thumbnails are small (≤320px), so the whole strip streams in
                // without holding one full-resolution data URL per page. A small
                // worker pool keeps a long deck from queueing serially while
                // thumbnails still fill in progressively.
                let nextIndex = 0;
                const workers = Array.from({ length: Math.min(6, list.length) }, async () => {
                    while (nextIndex < list.length && alive()) {
                        const index = nextIndex++;
                        try {
                            const url = String(await PptxSlideThumbnailDataURL(list[index]) || "");
                            if (!alive()) return;
                            if (!url) continue;
                            setThumbs(prev => {
                                const next = new Map(prev);
                                next.set(index, url);
                                return next;
                            });
                        } catch {
                            // A single undecodable slide should not break the rest.
                        }
                    }
                });
                await Promise.all(workers);
            } catch (err) {
                if (alive()) setError(err instanceof Error ? err.message : String(err || ""));
            }
        })();
        return () => { cancelled = true; };
    }, [absPath]);

    // The full-fidelity slide loads on demand for the selected page only.
    useEffect(() => {
        if (!images || images.length === 0) return;
        const index = Math.min(selected, images.length - 1);
        if (fullUrls.has(index)) return;
        const version = versionRef.current;
        let cancelled = false;
        void (async () => {
            try {
                const url = String(await AIAssistantAttachmentFullDataURL(images[index]) || "");
                if (cancelled || version !== versionRef.current || !url) return;
                setFullUrls(prev => {
                    const next = new Map(prev);
                    next.set(index, url);
                    return next;
                });
            } catch {
                // Keep the placeholder; the user can reselect to retry.
            }
        })();
        return () => { cancelled = true; };
    }, [selected, images, fullUrls]);

    if (error) {
        return (
            <div data-testid="pptx-preview-error" style={{ padding: 20, color: theme.diffDeleteText, fontSize: 13, lineHeight: 1.6 }}>
                {error}
            </div>
        );
    }
    if (!images) {
        return (
            <div data-testid="pptx-preview-loading" role="status" style={{ padding: 20, color: theme.textMuted, fontSize: 13 }}>
                {isZh ? "正在渲染 PPT 预览…" : "Rendering slide preview…"}
            </div>
        );
    }

    const current = Math.min(selected, images.length - 1);
    const currentUrl = fullUrls.get(current) || "";
    const handleThumbsKeyDown = (event: KeyboardEvent) => {
        let next = current;
        if (event.key === "ArrowDown" || event.key === "ArrowRight") next = Math.min(images.length - 1, current + 1);
        else if (event.key === "ArrowUp" || event.key === "ArrowLeft") next = Math.max(0, current - 1);
        else if (event.key === "Home") next = 0;
        else if (event.key === "End") next = images.length - 1;
        else return;
        event.preventDefault();
        setSelected(next);
    };

    return (
        <div data-testid="pptx-preview-panel" style={{ display: "flex", height: "100%", minHeight: 0, background: theme.bg }}>
            <div
                data-testid="pptx-preview-thumbnails"
                role="listbox"
                aria-label={isZh ? "页面列表" : "Slides"}
                onKeyDown={handleThumbsKeyDown}
                style={{
                    width: 150,
                    flexShrink: 0,
                    overflowY: "auto",
                    borderRight: `1px solid ${theme.border}`,
                    padding: 8,
                    display: "flex",
                    flexDirection: "column",
                    gap: 8,
                    boxSizing: "border-box",
                }}
            >
                {images.map((imagePath, index) => {
                    const active = index === current;
                    const url = thumbs.get(index) || "";
                    return (
                        <button
                            key={imagePath}
                            type="button"
                            role="option"
                            aria-selected={active}
                            data-testid="pptx-preview-thumbnail"
                            data-active={active ? "true" : "false"}
                            onClick={() => setSelected(index)}
                            title={isZh ? `第 ${index + 1} 页` : `Slide ${index + 1}`}
                            style={{
                                position: "relative",
                                display: "block",
                                width: "100%",
                                border: `2px solid ${active ? theme.tabActiveText : theme.border}`,
                                borderRadius: 6,
                                background: theme.tabBg,
                                cursor: "pointer",
                                padding: 0,
                                overflow: "hidden",
                                flexShrink: 0,
                            }}
                        >
                            {url ? (
                                <img src={url} alt="" draggable={false} style={{ display: "block", width: "100%", height: "auto" }} />
                            ) : (
                                <span style={{ display: "block", width: "100%", aspectRatio: "16 / 9", background: theme.lineNumBg }} />
                            )}
                            <span style={{
                                position: "absolute",
                                left: 4,
                                bottom: 4,
                                padding: "0 5px",
                                borderRadius: 4,
                                background: "rgba(15, 23, 42, 0.65)",
                                color: "#fff",
                                fontSize: 10,
                                lineHeight: "16px",
                            }}>
                                {index + 1}
                            </span>
                        </button>
                    );
                })}
            </div>
            <div style={{ flex: 1, minWidth: 0, minHeight: 0, display: "flex", flexDirection: "column" }}>
                <div style={{ flex: 1, minHeight: 0, display: "flex", alignItems: "center", justifyContent: "center", padding: 12, boxSizing: "border-box" }}>
                    {currentUrl ? (
                        <img
                            src={currentUrl}
                            alt={isZh ? `第 ${current + 1} 页` : `Slide ${current + 1}`}
                            draggable={false}
                            data-testid="pptx-preview-slide"
                            style={{ maxWidth: "100%", maxHeight: "100%", objectFit: "contain", borderRadius: 4, boxShadow: "0 2px 12px rgba(15, 23, 42, 0.18)" }}
                        />
                    ) : thumbs.get(current) ? (
                        // Full-fidelity bytes still on the way: hold the page
                        // in place with the thumbnail so a fast flip feels instant.
                        <img
                            src={thumbs.get(current)}
                            alt={isZh ? `第 ${current + 1} 页（预览）` : `Slide ${current + 1} (preview)`}
                            draggable={false}
                            data-testid="pptx-preview-slide-placeholder"
                            style={{ maxWidth: "100%", maxHeight: "100%", objectFit: "contain", borderRadius: 4, opacity: 0.8 }}
                        />
                    ) : (
                        <span data-testid="pptx-preview-slide-loading" role="status" style={{ color: theme.textMuted, fontSize: 12 }}>
                            {isZh ? "正在加载页面…" : "Loading slide…"}
                        </span>
                    )}
                </div>
                <div data-testid="pptx-preview-page-indicator" style={{ padding: "6px 0 8px", textAlign: "center", color: theme.textMuted, fontSize: 12, flexShrink: 0 }}>
                    {current + 1} / {images.length}
                </div>
            </div>
        </div>
    );
}
