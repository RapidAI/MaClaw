import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent } from "react";
import { createPortal } from "react-dom";
import { CopyChatMediaToClipboard, SaveChatMediaAs } from "../../../wailsjs/go/main/App";
import { tailText } from "../../i18n/mnTail";
import { isCloudWorkspacePath } from "./codingTaskMode";

export interface ChatMediaTarget {
    /** "image" pastes a picture. "file" pastes the file itself. */
    kind: "image" | "file";
    /** Absolute path, data:image URL, or http(s) image URL. */
    source: string;
    fileName: string;
}

interface MenuState {
    x: number;
    y: number;
    target: ChatMediaTarget;
}

const OPEN_EVENT = "chat-media-menu-open";

type NoDragStyle = CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

function menuLang(lang?: string): string {
    if (lang) return lang;
    if (typeof document === "undefined") return "zh-Hans";
    return document.documentElement.lang || "zh-Hans";
}

/** Prefer the real file over a downscaled thumbnail. Cloud paths are not local files. */
export function chatMediaImageSource(filePath: string, displayedSrc: string): string {
    const path = String(filePath || "").trim();
    if (path && !isCloudWorkspacePath(path)) return path;
    return String(displayedSrc || "").trim() || path;
}

function chatMediaErrorText(err: unknown, lang: string): string {
    const fallback = tailText(lang, "Couldn't complete that", "操作失败", "操作失敗");
    if (typeof err === "string") return err.trim() || fallback;
    if (err && typeof err === "object" && "message" in err) {
        const text = String((err as { message?: unknown }).message || "").trim();
        if (text && text !== "[object Object]") return text;
    }
    return fallback;
}

function clampChatMediaMenu(x: number, y: number, width: number, height: number, viewW: number, viewH: number): { left: number; top: number } {
    const pad = 8;
    let left = x;
    let top = y;
    if (left + width > viewW - pad) left = Math.max(pad, viewW - width - pad);
    if (top + height > viewH - pad) top = Math.max(pad, viewH - height - pad);
    return { left, top };
}

function ChatMediaContextMenu({
    menu,
    lang,
    onClose,
}: {
    menu: MenuState;
    lang: string;
    onClose: () => void;
}) {
    const ref = useRef<HTMLDivElement | null>(null);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const busyRef = useRef(false);
    const [pos, setPos] = useState(() => ({ left: menu.x, top: menu.y }));
    const copyLabel = tailText(lang, "Copy", "复制", "複製");
    const saveLabel = tailText(lang, "Save", "保存", "儲存");
    const menuLabel = tailText(lang, "Image and file menu", "图片和附件菜单", "圖片和附件選單");

    useLayoutEffect(() => {
        const el = ref.current;
        if (!el) return;
        const rect = el.getBoundingClientRect();
        const next = clampChatMediaMenu(menu.x, menu.y, rect.width, rect.height, window.innerWidth, window.innerHeight);
        setPos(current => (current.left === next.left && current.top === next.top ? current : next));
    }, [menu.x, menu.y, error, busy]);

    useEffect(() => {
        const items = ref.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]');
        items?.[0]?.focus();
    }, []);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopPropagation();
                if (!busyRef.current) onClose();
                return;
            }
            if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
            const items = [...(ref.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') || [])];
            if (items.length === 0) return;
            event.preventDefault();
            const current = items.indexOf(document.activeElement as HTMLButtonElement);
            const next = event.key === "ArrowDown"
                ? (current + 1) % items.length
                : (current <= 0 ? items.length - 1 : current - 1);
            items[next]?.focus();
        };
        const onPointer = (event: PointerEvent) => {
            if (busyRef.current) return;
            if (ref.current?.contains(event.target as Node)) return;
            onClose();
        };
        // The transcript moves itself while a reply streams. A scroll event
        // from that would dismiss the menu before the user can choose. A
        // wheel or touch gesture is the user leaving the menu.
        const onUserScroll = (event: Event) => {
            if (busyRef.current) return;
            const target = event.target;
            if (target instanceof Node && ref.current?.contains(target)) return;
            onClose();
        };
        window.addEventListener("keydown", onKey, true);
        window.addEventListener("pointerdown", onPointer, true);
        window.addEventListener("wheel", onUserScroll, true);
        window.addEventListener("touchmove", onUserScroll, true);
        window.addEventListener("resize", onUserScroll);
        return () => {
            window.removeEventListener("keydown", onKey, true);
            window.removeEventListener("pointerdown", onPointer, true);
            window.removeEventListener("wheel", onUserScroll, true);
            window.removeEventListener("touchmove", onUserScroll, true);
            window.removeEventListener("resize", onUserScroll);
        };
    }, [onClose]);

    const run = (action: () => Promise<unknown>) => {
        if (busyRef.current) return;
        busyRef.current = true;
        setBusy(true);
        setError("");
        void Promise.resolve()
            .then(action)
            .then(
                () => onClose(),
                (err: unknown) => {
                    busyRef.current = false;
                    setBusy(false);
                    setError(chatMediaErrorText(err, lang));
                },
            );
    };

    const box: NoDragStyle = {
        position: "fixed",
        left: pos.left,
        top: pos.top,
        zIndex: 115000,
        minWidth: 132,
        padding: "4px 0",
        margin: 0,
        background: "var(--theme-surface, #fff)",
        border: "1px solid var(--theme-border, rgba(15,23,42,0.12))",
        borderRadius: 8,
        boxShadow: "0 8px 24px rgba(15, 23, 42, 0.18)",
        WebkitAppRegion: "no-drag",
        "--wails-draggable": "no-drag",
    };

    const node = (
        <div
            ref={ref}
            role="menu"
            aria-label={menuLabel}
            data-chat-media-menu=""
            data-testid="chat-media-context-menu"
            style={box}
            onContextMenu={(event) => { event.preventDefault(); event.stopPropagation(); }}
            onMouseDown={(event) => event.stopPropagation()}
        >
            <button
                type="button"
                role="menuitem"
                className="context-menu-item"
                data-testid="chat-media-copy"
                disabled={busy}
                style={itemStyle}
                onClick={() => run(() => CopyChatMediaToClipboard(menu.target.kind, menu.target.source))}
            >
                {copyLabel}
            </button>
            <button
                type="button"
                role="menuitem"
                className="context-menu-item"
                data-testid="chat-media-save"
                disabled={busy}
                style={itemStyle}
                onClick={() => run(() => SaveChatMediaAs(menu.target.kind, menu.target.source, menu.target.fileName || "file", saveLabel))}
            >
                {saveLabel}
            </button>
            {error ? (
                <div role="alert" data-testid="chat-media-menu-error" style={{ padding: "4px 12px 6px", fontSize: 12, color: "var(--theme-danger, #c0392b)", maxWidth: 240, overflowWrap: "anywhere" }}>
                    {error}
                </div>
            ) : null}
        </div>
    );
    if (typeof document === "undefined") return node;
    return createPortal(node, document.body);
}

const itemStyle: CSSProperties = {
    display: "block",
    width: "100%",
    border: "none",
    background: "transparent",
    textAlign: "left",
};

/** Right-click menu for a chat image or attachment icon. */
export function useChatMediaMenu(lang?: string) {
    const [menu, setMenu] = useState<MenuState | null>(null);
    const resolved = menuLang(lang);
    useEffect(() => {
        const close = (event: Event) => {
            const detail = (event as CustomEvent<MenuState>).detail;
            setMenu(current => current === detail ? current : null);
        };
        window.addEventListener(OPEN_EVENT, close);
        return () => window.removeEventListener(OPEN_EVENT, close);
    }, []);
    const onContextMenu = useCallback((event: ReactMouseEvent, target: ChatMediaTarget) => {
        const source = String(target.source || "").trim();
        if (!source) return;
        event.preventDefault();
        event.stopPropagation();
        const next = { x: event.clientX, y: event.clientY, target: { ...target, source } };
        setMenu(next);
        window.dispatchEvent(new CustomEvent(OPEN_EVENT, { detail: next }));
    }, []);
    const close = useCallback(() => setMenu(null), []);
    const node = menu ? <ChatMediaContextMenu menu={menu} lang={resolved} onClose={close} /> : null;
    return { onContextMenu, menu: node };
}
