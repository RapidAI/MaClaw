/**
 * Compact Computer Use controls near the chat input (next to stop-generation UX).
 * Visible while a CU session is active or paused; hidden once stopped or idle
 * so the console does not linger after use. A new CU task re-opens it — the
 * backend lifts the stale stop on fresh activation.
 */
import { useCallback, useEffect, useState, type CSSProperties } from "react";
import {
    ComputerUsePause,
    ComputerUseReset,
    ComputerUseResume,
    ComputerUseStop,
    GetComputerUseStatus,
} from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import {
    EVENT_COMPUTER_USE_ACTION,
    EVENT_COMPUTER_USE_CONTROL,
    EVENT_COMPUTER_USE_OBSERVE,
} from "../../constants/events";
import { localizeText } from "../../i18n";
import type { Theme } from "./aiAssistantPanelTheme";

type Props = {
    lang: string;
    theme: Theme;
    themeMode?: "light" | "dark";
};

type CUState = {
    visible: boolean;
    paused: boolean;
    steps: number;
    elements: number;
    label: string;
};

const empty: CUState = {
    visible: false,
    paused: false,
    steps: 0,
    elements: 0,
    label: "",
};

export function ComputerUseQuickBar({ lang, theme: t }: Props) {
    const tr = useCallback(
        (en: string, zh: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant),
        [lang]
    );
    const [st, setSt] = useState<CUState>(empty);
    const [busy, setBusy] = useState(false);

    const refresh = useCallback(async () => {
        try {
            const s: any = await GetComputerUseStatus();
            if (!s || s.enabled === false) {
                setSt(empty);
                return;
            }
            const paused = !!s.paused && !s.stopped;
            // Hide once control is no longer in use: a stopped session, or stale
            // step counters after the sticky window expired, must not pin the bar.
            const active = !!s.session_active || paused;
            if (s.stopped || !active) {
                setSt(empty);
                return;
            }
            let label = tr("Desktop control", "桌面操控", "桌面操控");
            if (paused) label = tr("Desktop control · paused", "桌面操控 · 已暂停", "桌面操控 · 已暫停");
            else label = tr("Desktop control · active", "桌面操控 · 活动中", "桌面操控 · 活動中");
            const backend = typeof s.uia_sidecar_backend === "string" && s.uia_sidecar_backend
                ? s.uia_sidecar_backend
                : "";
            if (backend) label = `${label} · ${backend}`;
            setSt({
                visible: true,
                paused,
                steps: s.step_count ?? 0,
                elements: s.element_count ?? 0,
                label,
            });
        } catch {
            /* ignore */
        }
    }, [tr]);

    useEffect(() => {
        void refresh();
        const id = window.setInterval(() => void refresh(), 4000);
        EventsOn(EVENT_COMPUTER_USE_OBSERVE, () => void refresh());
        EventsOn(EVENT_COMPUTER_USE_ACTION, () => void refresh());
        EventsOn(EVENT_COMPUTER_USE_CONTROL, () => void refresh());
        return () => {
            window.clearInterval(id);
            EventsOff(EVENT_COMPUTER_USE_OBSERVE);
            EventsOff(EVENT_COMPUTER_USE_ACTION);
            EventsOff(EVENT_COMPUTER_USE_CONTROL);
        };
    }, [refresh]);

    if (!st.visible) return null;

    const btn = (danger = false): CSSProperties => ({
        border: danger ? "1px solid rgba(242,139,130,0.5)" : `1px solid ${t.divider}`,
        background: danger ? "rgba(242,139,130,0.12)" : "transparent",
        color: t.text,
        borderRadius: 6,
        padding: "2px 8px",
        fontSize: 11,
        cursor: busy ? "wait" : "pointer",
        lineHeight: 1.4,
    });

    const run = async (fn: () => Promise<void>) => {
        setBusy(true);
        try {
            await fn();
            await refresh();
        } catch {
            await refresh();
        } finally {
            setBusy(false);
        }
    };

    return (
        <div
            data-testid="computer-use-quick-bar"
            style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                flexWrap: "wrap",
                padding: "6px 10px",
                borderTop: `1px solid ${t.inputBarBorder || t.divider}`,
                background: t.inputBarBg || t.bg,
                color: t.textMuted || t.text,
                fontSize: 11,
            }}
        >
            <span style={{ fontWeight: 600, color: t.headingColor || t.text }}>
                {st.label}
                {st.steps > 0 ? ` · ${st.steps}` : ""}
                {st.elements > 0 ? ` · e${st.elements}` : ""}
            </span>
            <span style={{ flex: 1 }} />
            <button
                type="button"
                disabled={busy || st.paused}
                onClick={() => void run(() => ComputerUsePause())}
                style={btn()}
                title={tr("Pause desktop actions", "暂停桌面动作", "暫停桌面動作")}
            >
                {tr("Pause", "暂停", "暫停")}
            </button>
            <button
                type="button"
                disabled={busy || !st.paused}
                onClick={() => void run(() => ComputerUseResume())}
                style={btn()}
                title={tr("Resume desktop actions", "继续桌面动作", "繼續桌面動作")}
            >
                {tr("Resume", "继续", "繼續")}
            </button>
            <button
                type="button"
                disabled={busy}
                onClick={() => void run(() => ComputerUseStop())}
                style={btn(true)}
                title={tr("Stop desktop control and cancel generation", "停止桌面操控并取消生成", "停止桌面操控並取消生成")}
            >
                {tr("Stop CU", "停止操控", "停止操控")}
            </button>
            {st.paused && (
                <button
                    type="button"
                    disabled={busy}
                    onClick={() => void run(() => ComputerUseReset())}
                    style={btn()}
                    title={tr("Reset control state", "复位控制状态", "復位控制狀態")}
                >
                    {tr("Reset", "复位", "復位")}
                </button>
            )}
        </div>
    );
}

export default ComputerUseQuickBar;
