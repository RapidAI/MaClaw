import React, { useRef } from "react";
import { chromeText } from "../../i18n/chromeText";
import "./AgentBrowserStage.css";

export interface AgentBrowserStageState {
    open: boolean;
    sessionId: string;
    url: string;
    title: string;
    frame: string;
    width: number;
    height: number;
    userHold: boolean;
    askReason: string;
    seq: number;
}

export function mergeAgentBrowserStage(
    prev: AgentBrowserStageState | null,
    next: Partial<AgentBrowserStageState> | null | undefined,
): AgentBrowserStageState | null {
    if (!next || typeof next !== "object") return prev;
    const seq = Number(next.seq || 0);
    if (prev && seq > 0 && prev.seq > seq) return prev;
    const frame = typeof next.frame === "string" && next.frame ? next.frame : (prev?.frame || "");
    return {
        open: !!next.open,
        sessionId: String(next.sessionId || prev?.sessionId || ""),
        url: String(next.url || prev?.url || ""),
        title: String(next.title || prev?.title || ""),
        frame,
        width: Number(next.width || prev?.width || 0),
        height: Number(next.height || prev?.height || 0),
        userHold: !!next.userHold,
        askReason: String(next.askReason || ""),
        seq: seq || prev?.seq || 0,
    };
}

export function frameDataURL(frame: string): string {
    const raw = String(frame || "").trim();
    if (!raw) return "";
    if (raw.startsWith("data:")) return raw;
    return `data:image/jpeg;base64,${raw}`;
}

/** Point inside an object-fit:contain image, as fractions of the image. */
export function containedImagePoint(
    boxW: number,
    boxH: number,
    imgW: number,
    imgH: number,
    localX: number,
    localY: number,
): { x: number; y: number } | null {
    if (boxW <= 0 || boxH <= 0 || imgW <= 0 || imgH <= 0) return null;
    const scale = Math.min(boxW / imgW, boxH / imgH);
    const drawnW = imgW * scale;
    const drawnH = imgH * scale;
    const offsetX = (boxW - drawnW) / 2;
    const offsetY = (boxH - drawnH) / 2;
    if (localX < offsetX || localY < offsetY || localX > offsetX + drawnW || localY > offsetY + drawnH) {
        return null;
    }
    return { x: (localX - offsetX) / drawnW, y: (localY - offsetY) / drawnH };
}

type StageCaller = {
    AgentBrowserState?: () => Promise<Partial<AgentBrowserStageState>>;
    AgentBrowserSetControl?: (hold: boolean) => Promise<unknown>;
    AgentBrowserInput?: (payload: string) => Promise<unknown>;
    AgentBrowserClose?: () => Promise<unknown>;
};

function stageCaller(): StageCaller | null {
    const app = (window as unknown as { go?: { main?: { App?: StageCaller } } }).go?.main?.App;
    return app || null;
}

export function bindAgentBrowserStage(onState: (state: AgentBrowserStageState) => void): () => void {
    const app = stageCaller();
    const pull = app?.AgentBrowserState;
    if (typeof pull === "function") {
        void pull().then((raw) => {
            const merged = mergeAgentBrowserStage(null, raw);
            if (merged) onState(merged);
        }).catch(() => { /* preview state is optional until a session starts */ });
    }
    return () => { /* caller owns the event subscription */ };
}

export function callAgentBrowserSetControl(hold: boolean) {
    const fn = stageCaller()?.AgentBrowserSetControl;
    if (typeof fn === "function") void fn(hold);
}

export function callAgentBrowserInput(payload: string) {
    const fn = stageCaller()?.AgentBrowserInput;
    if (typeof fn === "function") void fn(payload);
}

export function callAgentBrowserClose() {
    const fn = stageCaller()?.AgentBrowserClose;
    if (typeof fn === "function") void fn();
}

function askCopy(reason: string, lang: string): string {
    if (reason === "captcha_widget" || reason === "captcha") {
        return chromeText(lang, "This page needs you to finish a check. Take control, then hand it back.", "这一页需要你完成验证。接管后做完，再交还。");
    }
    if (reason) {
        return chromeText(lang, "This page needs you to sign in or confirm. Take control, then hand it back.", "这一页需要你登录或确认。接管后完成，再交还。");
    }
    return "";
}

export function AgentBrowserStage({
    state,
    lang,
    onSetHold,
    onInput,
}: {
    state: AgentBrowserStageState;
    lang: string;
    onSetHold?: (hold: boolean) => void;
    onInput?: (payload: string) => void;
}) {
    const imgRef = useRef<HTMLImageElement | null>(null);
    const moveAt = useRef(0);
    const src = frameDataURL(state.frame);
    const caption = state.title || state.url || chromeText(lang, "Browser", "浏览器");
    const ask = askCopy(state.askReason, lang);
    const hold = !!state.userHold;

    const send = (payload: Record<string, unknown>) => {
        onInput?.(JSON.stringify(payload));
    };

    const pointFromEvent = (event: React.PointerEvent<HTMLImageElement>) => {
        const img = imgRef.current;
        if (!img) return null;
        const rect = img.getBoundingClientRect();
        return containedImagePoint(
            rect.width,
            rect.height,
            img.naturalWidth || state.width || rect.width,
            img.naturalHeight || state.height || rect.height,
            event.clientX - rect.left,
            event.clientY - rect.top,
        );
    };

    return (
        <div className={"agent-browser-stage" + (hold ? " is-user" : " is-bot")} data-testid="agent-browser-stage">
            <div className="agent-browser-stage__bar">
                <p title={caption}>{hold
                    ? chromeText(lang, "You have the page", "你正在操作这个页面")
                    : chromeText(lang, "Agent is browsing", "代理正在浏览")}</p>
            </div>
            <div className="agent-browser-stage__frame">
                {src ? (
                    <img
                        ref={imgRef}
                        src={src}
                        alt={caption}
                        draggable={false}
                        data-testid="agent-browser-frame"
                        onPointerDown={(event) => {
                            if (!hold) return;
                            const point = pointFromEvent(event);
                            if (!point) return;
                            event.currentTarget.setPointerCapture?.(event.pointerId);
                            send({ kind: "down", x: point.x, y: point.y, button: "left", click_count: 1 });
                        }}
                        onPointerMove={(event) => {
                            if (!hold) return;
                            const now = Date.now();
                            if (now - moveAt.current < 40) return;
                            moveAt.current = now;
                            const point = pointFromEvent(event);
                            if (!point) return;
                            send({ kind: "move", x: point.x, y: point.y });
                        }}
                        onPointerUp={(event) => {
                            if (!hold) return;
                            const point = pointFromEvent(event);
                            if (!point) return;
                            send({ kind: "up", x: point.x, y: point.y, button: "left", click_count: 1 });
                        }}
                        onWheel={(event) => {
                            if (!hold) return;
                            event.preventDefault();
                            const point = containedImagePoint(
                                imgRef.current?.clientWidth || 1,
                                imgRef.current?.clientHeight || 1,
                                imgRef.current?.naturalWidth || state.width || 1,
                                imgRef.current?.naturalHeight || state.height || 1,
                                event.nativeEvent.offsetX,
                                event.nativeEvent.offsetY,
                            );
                            send({
                                kind: "wheel",
                                x: point?.x ?? 0.5,
                                y: point?.y ?? 0.5,
                                delta_x: event.deltaX,
                                delta_y: event.deltaY,
                            });
                        }}
                    />
                ) : (
                    <div className="agent-browser-stage__loading" data-testid="agent-browser-loading">
                        {chromeText(lang, "Opening the page…", "正在打开页面…")}
                    </div>
                )}
                {hold ? null : <div className="agent-browser-stage__shield" data-testid="agent-browser-shield" aria-hidden="true" />}
            </div>
            {ask ? <p className="agent-browser-stage__ask" role="status">{ask}</p> : null}
            <div className="agent-browser-stage__dock">
                <div className="agent-browser-stage__mode" role="group" aria-label={chromeText(lang, "Page control", "页面控制")}>
                    <button
                        type="button"
                        className="agent-browser-stage__mode-btn"
                        data-testid="agent-browser-watch"
                        aria-pressed={!hold}
                        onClick={() => onSetHold?.(false)}
                    >
                        {chromeText(lang, "Watch", "只看")}
                    </button>
                    <button
                        type="button"
                        className="agent-browser-stage__mode-btn"
                        data-testid="agent-browser-take-control"
                        aria-pressed={hold}
                        onClick={() => onSetHold?.(true)}
                    >
                        {chromeText(lang, "Take control", "接管")}
                    </button>
                </div>
            </div>
            <p className="agent-browser-stage__caption" title={state.url || caption}>{caption}</p>
            {hold ? (
                <input
                    className="agent-browser-stage__keys"
                    data-testid="agent-browser-keys"
                    aria-label={chromeText(lang, "Type on the page", "在页面上输入")}
                    placeholder={chromeText(lang, "Type here, Enter sends", "在这里输入，回车发送")}
                    onKeyDown={(event) => {
                        if (event.key === "Enter") {
                            event.preventDefault();
                            const text = event.currentTarget.value;
                            if (text) send({ kind: "text", text });
                            send({ kind: "keydown", key: "Enter" });
                            send({ kind: "keyup", key: "Enter" });
                            event.currentTarget.value = "";
                            return;
                        }
                        if (event.key === "Backspace") {
                            event.preventDefault();
                            send({ kind: "keydown", key: "Backspace" });
                            send({ kind: "keyup", key: "Backspace" });
                            return;
                        }
                        if (event.key === "Tab" || event.key === "Escape") {
                            event.preventDefault();
                            send({ kind: "keydown", key: event.key });
                            send({ kind: "keyup", key: event.key });
                        }
                    }}
                />
            ) : null}
        </div>
    );
}
