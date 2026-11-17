import React, { useRef, useState } from "react";
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
    host?: string;
    attention?: boolean;
    confirmItem?: string;
    confirmShop?: string;
    confirmAddress?: string;
    confirmAmount?: string;
    confirmDetail?: string;
    secretMethod?: string;
    secretDigits?: number;
    secretSubmission?: string;
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
        host: "host" in next ? String(next.host || "") : (prev?.host || ""),
        attention: typeof next.attention === "boolean" ? next.attention : !!prev?.attention,
        confirmItem: "confirmItem" in next ? String(next.confirmItem || "") : (prev?.confirmItem || ""),
        confirmShop: "confirmShop" in next ? String(next.confirmShop || "") : (prev?.confirmShop || ""),
        confirmAddress: "confirmAddress" in next ? String(next.confirmAddress || "") : (prev?.confirmAddress || ""),
        confirmAmount: "confirmAmount" in next ? String(next.confirmAmount || "") : (prev?.confirmAmount || ""),
        confirmDetail: "confirmDetail" in next ? String(next.confirmDetail || "") : (prev?.confirmDetail || ""),
        secretMethod: "secretMethod" in next ? String(next.secretMethod || "") : (prev?.secretMethod || ""),
        secretDigits: "secretDigits" in next ? Number(next.secretDigits || 0) : (prev?.secretDigits || 0),
        secretSubmission: "secretSubmission" in next ? String(next.secretSubmission || "") : (prev?.secretSubmission || ""),
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
    AgentBrowserConfirm?: (approve: boolean) => Promise<unknown>;
    AgentBrowserSubmitSecret?: (secret: string, approve: boolean) => Promise<unknown>;
    AgentBrowserFinish?: (done: boolean) => Promise<unknown>;
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

export function callAgentBrowserConfirm(approve: boolean) {
    const fn = stageCaller()?.AgentBrowserConfirm;
    if (typeof fn === "function") void fn(approve);
}

export function callAgentBrowserSubmitSecret(secret: string, approve: boolean) {
    const fn = stageCaller()?.AgentBrowserSubmitSecret;
    if (typeof fn === "function") return fn(secret, approve);
    return Promise.resolve();
}

export function callAgentBrowserFinish(done: boolean) {
    const fn = stageCaller()?.AgentBrowserFinish;
    if (typeof fn === "function") void fn(done);
}

function askCopy(reason: string, lang: string): string {
    if (reason === "captcha_widget" || reason === "captcha") {
        return chromeText(lang, "Finish the check on this page. The code stays here, not in the chat.", "请在这个页面上完成验证。验证码留在页面里，不要发到对话。");
    }
    if (reason === "secret") {
        return chromeText(lang, "Enter the password on this page. It is not sent to the chat.", "请在这个页面上输入密码。密码不会发到对话。");
    }
    if (reason === "login") {
        return chromeText(lang, "Sign in on this page, then mark it done.", "请在这个页面上登录，完成后点「我已完成」。");
    }
    if (reason === "passkey") {
        return chromeText(lang, "Finish the passkey on this page. Nothing is sent to the chat.", "请在这个页面上完成通行密钥。不要把结果发到对话。");
    }
    if (reason === "qr") {
        return chromeText(lang, "Scan the code on this page, then mark it done.", "请在这个页面上完成扫码，然后点「我已完成」。");
    }
    if (reason && reason !== "confirm" && reason !== "preview") {
        return chromeText(lang, "Finish this step on the page, then mark it done.", "请在这个页面上完成这一步，然后点「我已完成」。");
    }
    return "";
}

function SecretFillCard({
    state,
    lang,
    onSecret,
}: {
    state: AgentBrowserStageState;
    lang: string;
    onSecret?: (secret: string, approve: boolean) => void | Promise<unknown>;
}) {
    const [value, setValue] = useState("");
    const [failed, setFailed] = useState(false);
    const [sent, setSent] = useState(false);
    const method = state.secretMethod || "password";
    const digits = Number(state.secretDigits || 0);
    const submit = state.secretSubmission === "enter" || state.secretSubmission === "click";
    const title = method === "otp"
        ? chromeText(lang, "Enter the code", "输入验证码")
        : method === "authenticator"
            ? chromeText(lang, "Enter the authenticator code", "输入验证器码")
            : chromeText(lang, "Enter the password", "输入密码");
    const ready = method === "password"
        ? value.length > 0
        : (digits > 0 ? value.length === digits && /^\d+$/.test(value) : /^\d{4,12}$/.test(value));
    const host = state.host || "";
    return (
        <form
            className="agent-browser-confirm agent-browser-secret"
            data-testid="agent-browser-secret-card"
            onSubmit={(event) => {
                event.preventDefault();
                if (!ready || sent) return;
                const typed = value;
                setSent(true);
                void Promise.resolve(onSecret?.(typed, true)).then(() => {
                    setValue("");
                    setFailed(false);
                }).catch(() => {
                    setSent(false);
                    setFailed(true);
                });
            }}
        >
            <h3>{title}</h3>
            <p className="agent-browser-secret__note">
                {host
                    ? chromeText(lang, `Filled only on ${host}. It is not sent to the chat.`, `只填入 ${host}，不会发到对话。`)
                    : chromeText(lang, "Filled only on this page. It is not sent to the chat.", "只填入这个页面，不会发到对话。")}
            </p>
            <input
                type={method === "password" ? "password" : "text"}
                inputMode={method === "password" ? undefined : "numeric"}
                autoComplete="off"
                data-testid="agent-browser-secret-input"
                aria-label={title}
                value={value}
                maxLength={method === "password" ? 256 : (digits || 12)}
                disabled={sent}
                onChange={(event) => {
                    setFailed(false);
                    setValue(event.target.value);
                }}
            />
            {failed ? (
                <p className="agent-browser-secret__note" role="status">
                    {chromeText(lang, "That entry was not accepted. Try again on this card.", "这一位没有通过，请在这张卡上再填一次。")}
                </p>
            ) : null}
            <div className="agent-browser-confirm__actions">
                <button type="button" data-testid="agent-browser-secret-cancel" disabled={sent} onClick={() => { void onSecret?.("", false); }}>
                    {chromeText(lang, "Cancel", "取消")}
                </button>
                <button type="submit" data-testid="agent-browser-secret-submit" disabled={!ready || sent}>
                    {submit
                        ? chromeText(lang, "Fill and submit", "填入并提交")
                        : chromeText(lang, "Fill in", "填入")}
                </button>
            </div>
        </form>
    );
}

function CheckoutConfirmCard({
    state,
    lang,
    onConfirm,
}: {
    state: AgentBrowserStageState;
    lang: string;
    onConfirm?: (approve: boolean) => void;
}) {
    const [checked, setChecked] = useState(false);
    const rows = [
        { label: chromeText(lang, "Item", "商品"), value: [state.confirmItem, state.confirmShop].filter(Boolean).join(" · ") },
        { label: chromeText(lang, "Ship to", "收货地址"), value: state.confirmAddress || "" },
        { label: chromeText(lang, "Total due", "应付总额"), value: state.confirmAmount ? `¥${state.confirmAmount}` : "" },
        { label: chromeText(lang, "Note", "备注"), value: state.confirmDetail || "" },
    ].filter((row) => row.value);
    return (
        <form
            className="agent-browser-confirm"
            data-testid="agent-browser-confirm-card"
            onSubmit={(event) => {
                event.preventDefault();
                if (!checked) return;
                onConfirm?.(true);
            }}
        >
            <h3>{chromeText(lang, `Confirm payment ¥${state.confirmAmount}`, `确认支付 ¥${state.confirmAmount}`)}</h3>
            <dl>
                {rows.map((row) => (
                    <div key={row.label}>
                        <dt>{row.label}</dt>
                        <dd>{row.value}</dd>
                    </div>
                ))}
            </dl>
            <label className="agent-browser-confirm__check">
                <input
                    type="checkbox"
                    data-testid="agent-browser-confirm-check"
                    checked={checked}
                    onChange={(event) => setChecked(event.target.checked)}
                />
                {chromeText(lang, "Confirm payment", "确认支付")}
            </label>
            <div className="agent-browser-confirm__actions">
                <button type="button" data-testid="agent-browser-confirm-cancel" onClick={() => onConfirm?.(false)}>
                    {chromeText(lang, "Cancel", "取消")}
                </button>
                <button type="submit" data-testid="agent-browser-confirm-submit" disabled={!checked}>
                    {chromeText(lang, "Confirm payment", "确认支付")}
                </button>
            </div>
        </form>
    );
}

export function AgentBrowserStage({
    state,
    lang,
    onSetHold,
    onInput,
    onConfirm,
    onSecret,
    onFinish,
}: {
    state: AgentBrowserStageState;
    lang: string;
    onSetHold?: (hold: boolean) => void;
    onInput?: (payload: string) => void;
    onConfirm?: (approve: boolean) => void;
    onSecret?: (secret: string, approve: boolean) => void | Promise<unknown>;
    onFinish?: (done: boolean) => void;
}) {
    const imgRef = useRef<HTMLImageElement | null>(null);
    const moveAt = useRef(0);
    const src = frameDataURL(state.frame);
    const caption = state.title || state.url || chromeText(lang, "Browser", "浏览器");
    const hold = !!state.userHold;
    const confirming = !!state.confirmAmount;
    const filling = !!state.secretMethod && !confirming;
    const attention = !!state.attention && hold && !confirming && !filling;
    const interactive = hold && !confirming && !filling;
    const ask = confirming || filling ? "" : askCopy(state.askReason, lang);
    const host = state.host || "";
    const hostLine = host
        ? chromeText(lang, `Finish this step on ${host}`, `请在 ${host} 上完成该步骤`)
        : chromeText(lang, "Finish this step on the page", "请在这个页面上完成该步骤");

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

    const stageClass = "agent-browser-stage"
        + (interactive ? " is-user" : " is-bot")
        + (attention ? " is-attention" : "")
        + (confirming ? " is-confirm" : "")
        + (filling ? " is-secret" : "");

    return (
        <div className={stageClass} data-testid="agent-browser-stage">
            <div className="agent-browser-stage__bar">
                {attention ? (
                    <p className="agent-browser-stage__wait" data-testid="agent-browser-waiting">
                        {chromeText(lang, "Waiting for you", "等待你操作")}
                    </p>
                ) : null}
                <p title={caption}>{confirming
                    ? chromeText(lang, "Confirm this payment", "请确认这笔支付")
                    : filling
                        ? chromeText(lang, "Fill this in on the card", "请在这张卡上填写")
                        : attention
                            ? hostLine
                            : hold
                                ? chromeText(lang, "You have the page", "你正在操作这个页面")
                                : chromeText(lang, "Agent is browsing", "代理正在浏览")}</p>
            </div>
            {confirming ? (
                <CheckoutConfirmCard
                    key={state.confirmAmount + "|" + (state.confirmItem || "")}
                    state={state}
                    lang={lang}
                    onConfirm={onConfirm}
                />
            ) : null}
            {filling ? (
                <SecretFillCard
                    key={(state.secretMethod || "") + "|" + String(state.secretDigits || 0) + "|" + (state.host || "")}
                    state={state}
                    lang={lang}
                    onSecret={onSecret}
                />
            ) : null}
            <div className={"agent-browser-stage__frame" + (attention ? " is-focal" : "")}>
                {src ? (
                    <img
                        ref={imgRef}
                        src={src}
                        alt={caption}
                        draggable={false}
                        data-testid="agent-browser-frame"
                        onPointerDown={(event) => {
                            if (!interactive) return;
                            const point = pointFromEvent(event);
                            if (!point) return;
                            event.currentTarget.setPointerCapture?.(event.pointerId);
                            send({ kind: "down", x: point.x, y: point.y, button: "left", click_count: 1 });
                        }}
                        onPointerMove={(event) => {
                            if (!interactive) return;
                            const now = Date.now();
                            if (now - moveAt.current < 40) return;
                            moveAt.current = now;
                            const point = pointFromEvent(event);
                            if (!point) return;
                            send({ kind: "move", x: point.x, y: point.y });
                        }}
                        onPointerUp={(event) => {
                            if (!interactive) return;
                            const point = pointFromEvent(event);
                            if (!point) return;
                            send({ kind: "up", x: point.x, y: point.y, button: "left", click_count: 1 });
                        }}
                        onWheel={(event) => {
                            if (!interactive) return;
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
                {interactive ? null : <div className="agent-browser-stage__shield" data-testid="agent-browser-shield" aria-hidden="true" />}
            </div>
            {ask ? <p className="agent-browser-stage__ask" role="status">{ask}</p> : null}
            {attention ? (
                <p className="agent-browser-stage__status" data-testid="agent-browser-attention-status">
                    {chromeText(lang, "Waiting for you in the browser", "等待你在浏览器中操作")}
                </p>
            ) : null}
            <div className="agent-browser-stage__dock">
                {attention ? (
                    <div className="agent-browser-stage__mode" role="group" aria-label={chromeText(lang, "Page control", "页面控制")}>
                        <button
                            type="button"
                            className="agent-browser-stage__mode-btn"
                            data-testid="agent-browser-attention-cancel"
                            onClick={() => onFinish?.(false)}
                        >
                            {chromeText(lang, "Cancel", "取消")}
                        </button>
                        <button
                            type="button"
                            className="agent-browser-stage__mode-btn agent-browser-stage__mode-btn--done"
                            data-testid="agent-browser-attention-done"
                            onClick={() => onFinish?.(true)}
                        >
                            {chromeText(lang, "I'm done", "我已完成")}
                        </button>
                    </div>
                ) : confirming || filling ? null : (
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
                )}
            </div>
            <p className="agent-browser-stage__caption" title={state.url || caption}>{caption}</p>
            {interactive ? (
                <input
                    className="agent-browser-stage__keys"
                    data-testid="agent-browser-keys"
                    aria-label={chromeText(lang, "Type on the page", "在页面上输入")}
                    placeholder={attention
                        ? chromeText(lang, "Password stays on the page", "密码在这里输入，不会发到对话")
                        : chromeText(lang, "Type here, Enter sends", "在这里输入，回车发送")}
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
