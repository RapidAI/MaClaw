import { useEffect, useRef, useState, type MouseEvent } from "react";
import { FileCompanionFigureDataURL } from "../../../wailsjs/go/main/App";

/** One drawing from the open file, placed under the answer that talks about it. */
export type CompanionFigure = {
    label: string;
    caption: string;
    path: string;
};

// One answer can be re-rendered on every streamed token. Image bytes are asked
// for once per path and then kept, so scrolling a long reply stays cheap.
const figureDataURLs = new Map<string, string>();
// Two answers naming the same drawing must not start two parallel fetches.
const figureDataURLPromises = new Map<string, Promise<string>>();
// Data URLs carry whole-image base64. An on-screen <img> keeps its own string
// through component state, so dropping the oldest entries never blanks a
// figure that is already rendered.
const FIGURE_DATA_URL_CACHE_MAX = 48;

function cacheFigureDataURL(path: string, dataURL: string): void {
    figureDataURLs.delete(path);
    figureDataURLs.set(path, dataURL);
    while (figureDataURLs.size > FIGURE_DATA_URL_CACHE_MAX) {
        const oldest = figureDataURLs.keys().next().value;
        if (oldest === undefined) break;
        figureDataURLs.delete(oldest);
    }
}

function loadFigureDataURL(path: string): Promise<string> {
    const cached = figureDataURLs.get(path);
    if (cached) {
        cacheFigureDataURL(path, cached);
        return Promise.resolve(cached);
    }
    if (typeof FileCompanionFigureDataURL !== "function") return Promise.resolve("");
    const pending = figureDataURLPromises.get(path);
    if (pending) return pending;
    // A failed fetch leaves the map empty, so a later render can retry it.
    const request = FileCompanionFigureDataURL(path)
        .then((dataURL) => {
            if (dataURL) cacheFigureDataURL(path, dataURL);
            return dataURL || "";
        })
        .catch(() => "")
        .finally(() => figureDataURLPromises.delete(path));
    figureDataURLPromises.set(path, request);
    return request;
}

export function CompanionFigureStrip({ figures, messageId }: { figures: CompanionFigure[]; messageId: string }) {
    const [loaded, setLoaded] = useState<Record<string, string>>({});
    const [failed, setFailed] = useState<Record<string, boolean>>({});
    const [zoomed, setZoomed] = useState<string>("");
    const requested = useRef<Set<string>>(new Set());

    useEffect(() => {
        let live = true;
        figures.forEach((figure) => {
            if (!figure.path || requested.current.has(figure.path)) return;
            requested.current.add(figure.path);
            void loadFigureDataURL(figure.path).then((dataURL) => {
                if (!live) return;
                if (dataURL) {
                    setLoaded((current) => ({ ...current, [figure.path]: dataURL }));
                } else {
                    setFailed((current) => ({ ...current, [figure.path]: true }));
                }
            });
        });
        return () => { live = false; };
    }, [figures]);

    useEffect(() => {
        if (!zoomed) return;
        const close = (event: KeyboardEvent) => {
            if (event.key === "Escape") setZoomed("");
        };
        window.addEventListener("keydown", close);
        return () => window.removeEventListener("keydown", close);
    }, [zoomed]);

    if (figures.length === 0) return null;
    const noteFrames = figures.every((figure) => figure.label.startsWith("板书") || figure.label.startsWith("关键帧"));
    const heading = noteFrames ? "关键帧" : "原文配图";
    const open = (event: MouseEvent, path: string) => {
        event.preventDefault();
        event.stopPropagation();
        setZoomed(path);
    };
    const zoomedFigure = figures.find((figure) => figure.path === zoomed);
    const zoomLabel = [zoomedFigure?.label, zoomedFigure?.caption].filter(Boolean).join(" · ") || heading;

    return (
        <div style={{ marginTop: 10 }} data-testid={`file-companion-figures-${messageId}`}>
            <div style={{ fontSize: 12, color: "var(--theme-text-muted, #5b6b80)", marginBottom: 6 }}>{heading}</div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                {figures.map((figure) => {
                    const dataURL = loaded[figure.path] || figureDataURLs.get(figure.path) || "";
                    const title = [figure.label, figure.caption, figure.path].filter(Boolean).join(" · ");
                    return (
                        <figure key={figure.path} style={{ margin: 0, width: 168, border: "1px solid var(--theme-border, #e2e8f0)", borderRadius: 8, overflow: "hidden", background: "var(--theme-surface-muted, #f8fafc)" }}>
                            {dataURL ? (
                                <button
                                    type="button"
                                    title={title}
                                    onClick={(event) => open(event, figure.path)}
                                    style={{ display: "block", width: "100%", padding: 0, border: "none", background: "#ffffff", cursor: "zoom-in" }}
                                >
                                    <img
                                        src={dataURL}
                                        alt={figure.caption || figure.label || heading}
                                        data-testid="file-companion-figure-image"
                                        style={{ display: "block", width: "100%", height: 112, objectFit: "contain", background: "#ffffff" }}
                                    />
                                </button>
                            ) : (
                                <div style={{ height: 112, display: "grid", placeItems: "center", color: "var(--theme-text-muted, #94a3b8)", fontSize: 12 }}>
                                    {failed[figure.path] ? "无法取图" : "正在取图…"}
                                </div>
                            )}
                            <figcaption style={{ padding: "4px 6px", fontSize: 11, color: "var(--theme-text-secondary, #475569)", lineHeight: 1.4, overflowWrap: "anywhere" }}>
                                {figure.label ? `${figure.label}` : ""}{figure.caption ? ` ${figure.caption}` : ""}
                            </figcaption>
                        </figure>
                    );
                })}
            </div>
            {zoomed ? (
                <div
                    role="dialog"
                    aria-modal="true"
                    aria-label={zoomLabel}
                    data-testid="file-companion-figure-zoom"
                    onClick={() => setZoomed("")}
                    style={{ position: "fixed", inset: 0, background: "rgba(15,23,42,0.72)", display: "grid", placeItems: "center", zIndex: 40, padding: 24 }}
                >
                    <img
                        src={loaded[zoomed] || figureDataURLs.get(zoomed) || ""}
                        alt={zoomLabel}
                        style={{ maxWidth: "90vw", maxHeight: "86vh", background: "#ffffff", borderRadius: 6 }}
                    />
                </div>
            ) : null}
        </div>
    );
}
