/**
 * Computer Use operator preview — local OmniParser observe/action stream
 * for supervising a text-primary desktop agent (no multimodal screenshots).
 *
 * Modes:
 * - Floating (default): appears on activity, can dismiss
 * - Pinned: stays visible; idle state shows status from backend
 * - Docked: right-edge panel spanning more height (better for long sessions)
 */
import { useCallback, useEffect, useState } from "react";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import {
    ComputerUseE2EInteract,
    ComputerUseE2ESmoke,
    ComputerUsePause,
    ComputerUseReset,
    ComputerUseResume,
    ComputerUseSmokeCheck,
    ComputerUseStop,
    CopyComputerUsePath,
    ExportComputerUseDiagnostics,
    ExportComputerUseObserveHistoryCSV,
    GetComputerUseLastE2E,
    GetComputerUseLastError,
    GetComputerUseLastObserveMetrics,
    GetComputerUseObserveHistory,
    GetComputerUseStatus,
    GetComputerUseLogPrunePolicy,
    ListComputerUseLogArtifacts,
    OpenComputerUseLastDiagnostics,
    OpenComputerUseLogsFolder,
    OpenComputerUsePermissionSettings,
    PruneComputerUseLogArtifacts,
} from "../../../wailsjs/go/main/App";
import {
    EVENT_COMPUTER_USE_ACTION,
    EVENT_COMPUTER_USE_CONTROL,
    EVENT_COMPUTER_USE_ERROR,
    EVENT_COMPUTER_USE_LOGS,
    EVENT_COMPUTER_USE_OBSERVE,
} from "../../constants/events";
import { localizeText } from "../../i18n";

const STORAGE_PIN = "maclaw.computer_use.operator.pinned";
const STORAGE_DOCK = "maclaw.computer_use.operator.docked";

type TimingMs = {
    screenshot?: number;
    yolo?: number;
    a11y?: number;
    ocr?: number;
    commit?: number;
    total?: number;
    [k: string]: number | undefined;
};

type ObservePayload = {
    at?: string;
    ok?: boolean;
    element_count?: number;
    window_count?: number;
    windows?: string[];
    ocr_excerpt?: string;
    elements?: Array<{
        ref?: string;
        name?: string;
        type?: string;
        center?: number[];
        conf?: number;
        source?: string;
    }>;
    text_preview?: string;
    meta?: { width?: number; height?: number; scale_factor?: number };
    yolo_count?: number;
    a11y_count?: number;
    ocr_count?: number;
    timing_ms?: TimingMs;
    total_ms?: number;
    error?: string;
    guidance?: string;
    action?: string;
    stage?: string;
};

type ActionPayload = {
    at?: string;
    action?: string;
    detail?: string;
    ok?: boolean;
    error?: string;
};

type LastError = {
    at?: string;
    stage?: string;
    error?: string;
    guidance?: string;
    action?: string;
};

type LastE2E = {
    ok?: boolean;
    interact?: boolean;
    ms?: number;
    error?: string;
    token_found?: boolean;
    type_ok?: boolean;
    soft_fail?: boolean;
    skip_reason?: string;
    token_unconfirmed?: boolean;
    at?: string;
    diagnostics_path?: string;
    history_csv_path?: string;
    focus_retry?: boolean;
};

type Props = {
    lang?: string;
};

function readBool(key: string, fallback = false): boolean {
    try {
        const v = localStorage.getItem(key);
        if (v === null) return fallback;
        return v === "1" || v === "true";
    } catch {
        return fallback;
    }
}

function writeBool(key: string, value: boolean) {
    try {
        localStorage.setItem(key, value ? "1" : "0");
    } catch {
        /* ignore */
    }
}

export function ComputerUseOperatorPanel({ lang = "en" }: Props) {
    const t = useCallback(
        (en: string, zh: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant),
        [lang]
    );
    const [pinned, setPinned] = useState(() => readBool(STORAGE_PIN, false));
    const [docked, setDocked] = useState(() => readBool(STORAGE_DOCK, false));
    const [open, setOpen] = useState(() => readBool(STORAGE_PIN, false));
    const [observe, setObserve] = useState<ObservePayload | null>(null);
    const [actions, setActions] = useState<ActionPayload[]>([]);
    const [flash, setFlash] = useState(false);
    const [statusLine, setStatusLine] = useState("");
    const [paused, setPaused] = useState(false);
    const [stopped, setStopped] = useState(false);
    const [ctrlBusy, setCtrlBusy] = useState(false);
    const [ctrlError, setCtrlError] = useState("");
    const [lastError, setLastError] = useState<LastError | null>(null);
    const [smokeBusy, setSmokeBusy] = useState(false);
    const [historyLine, setHistoryLine] = useState("");
    const [historyTotals, setHistoryTotals] = useState<number[]>([]);
    const [exportMsg, setExportMsg] = useState("");
    const [lastE2E, setLastE2E] = useState<LastE2E | null>(null);
    const [artifactCount, setArtifactCount] = useState(0);

    const formatTiming = (tm?: TimingMs, total?: number) => {
        if (!tm && total == null) return "";
        const parts: string[] = [];
        if (tm?.screenshot != null) parts.push(`shot ${tm.screenshot}ms`);
        if (tm?.yolo != null) parts.push(`yolo ${tm.yolo}ms`);
        if (tm?.a11y != null) parts.push(`a11y ${tm.a11y}ms`);
        if (tm?.ocr != null) parts.push(`ocr ${tm.ocr}ms`);
        const tot = total ?? tm?.total;
        if (tot != null) parts.push(`Σ ${tot}ms`);
        return parts.join(" · ");
    };

    const applyStatus = useCallback(
        (st: any) => {
            if (!st) return;
            setPaused(!!st.paused && !st.stopped);
            setStopped(!!st.stopped);
            const backend =
                typeof st.uia_sidecar_backend === "string" && st.uia_sidecar_backend
                    ? ` · a11y=${st.uia_sidecar_backend}`
                    : st.uia_sidecar_alive
                      ? " · a11y=on"
                      : "";
            const obsMs =
                st.last_observe?.total_ms != null ? ` · last ${st.last_observe.total_ms}ms` : "";
            if (st.stopped) {
                setStatusLine(`${t("Stopped", "已停止", "已停止")} · steps=${st.step_count ?? 0}${backend}${obsMs}`);
            } else if (st.paused) {
                setStatusLine(`${t("Paused", "已暂停", "已暫停")} · steps=${st.step_count ?? 0}${backend}${obsMs}`);
            } else if (st.session_active) {
                setStatusLine(
                    `${t("Active", "活动中", "活動中")} · steps=${st.step_count ?? 0} · els=${st.element_count ?? 0}${backend}${obsMs}`
                );
            } else if (st.enabled === false) {
                setStatusLine(t("Disabled in settings", "设置中已关闭", "設定中已關閉"));
            } else {
                setStatusLine(
                    t("Idle — waiting for desktop task", "空闲 — 等待桌面任务", "空閒 — 等待桌面任務") +
                        backend +
                        obsMs
                );
            }
            if (st.last_error && (st.last_error.error || st.last_error.guidance)) {
                setLastError(st.last_error as LastError);
            }
            if (st.last_observe) {
                // Seed panel metrics when no live observe event yet.
                setObserve((prev) => prev || (st.last_observe as ObservePayload));
            }
            if (st.last_e2e) {
                setLastE2E(st.last_e2e as LastE2E);
            }
            const hs = st.observe_history_summary;
            if (hs && (hs.count ?? 0) > 0) {
                setHistoryLine(
                    `${t("History", "历史", "歷史")}: n=${hs.count} ok=${hs.ok_count ?? "?"} avg=${hs.avg_total_ms ?? "?"}ms min=${hs.min_total_ms ?? "?"} max=${hs.max_total_ms ?? "?"}`
                );
            }
        },
        [t]
    );

    const refreshLastE2E = useCallback(async () => {
        try {
            const e: any = await GetComputerUseLastE2E();
            if (e && Object.keys(e).length) setLastE2E(e as LastE2E);
            const arts: any = await ListComputerUseLogArtifacts("all", 40);
            setArtifactCount(Number(arts?.count || 0));
        } catch {
            /* ignore */
        }
    }, []);

    const refreshHistorySparkline = useCallback(async () => {
        try {
            const hist: any = await GetComputerUseObserveHistory();
            const items = Array.isArray(hist?.items) ? hist.items : [];
            const totals = items
                .map((it: any) => Number(it?.total_ms ?? it?.timing_ms?.total ?? 0))
                .filter((n: number) => Number.isFinite(n) && n >= 0);
            setHistoryTotals(totals);
            if (hist?.summary) {
                const hs = hist.summary;
                setHistoryLine(
                    `${t("History", "历史", "歷史")}: n=${hs.count} ok=${hs.ok_count ?? "?"} avg=${hs.avg_total_ms ?? "?"}ms min=${hs.min_total_ms ?? "?"} max=${hs.max_total_ms ?? "?"}`
                );
            }
        } catch {
            /* ignore */
        }
    }, [t]);

    useEffect(() => {
        EventsOn(EVENT_COMPUTER_USE_OBSERVE, (data: ObservePayload) => {
            setObserve(data || null);
            if (data?.ok === false) {
                setLastError({
                    at: data.at,
                    stage: data.stage,
                    error: data.error,
                    guidance: data.guidance,
                    action: data.action,
                });
            } else if (data?.ok === true) {
                setLastError(null);
            }
            setOpen(true);
            setFlash(true);
            window.setTimeout(() => setFlash(false), 600);
            void refreshHistorySparkline();
        });
        EventsOn(EVENT_COMPUTER_USE_ACTION, (data: ActionPayload) => {
            setActions((prev) => [data, ...prev].slice(0, 16));
            setOpen(true);
        });
        EventsOn(EVENT_COMPUTER_USE_ERROR, (data: LastError) => {
            setLastError(data || null);
            setOpen(true);
        });
        EventsOn(EVENT_COMPUTER_USE_CONTROL, (data: any) => {
            setPaused(!!data?.paused && !data?.stopped);
            setStopped(!!data?.stopped);
            setOpen(true);
            if (data?.stopped) {
                setStatusLine(`${t("Stopped", "已停止", "已停止")} · steps=${data?.steps ?? 0}`);
            } else if (data?.paused) {
                setStatusLine(`${t("Paused", "已暂停", "已暫停")} · steps=${data?.steps ?? 0}`);
            } else {
                setStatusLine(`${t("Running", "运行中", "運行中")} · steps=${data?.steps ?? 0}`);
            }
        });
        EventsOn(EVENT_COMPUTER_USE_LOGS, (data: any) => {
            if (!data) return;
            const op = String(data.op || "logs");
            const n = Number(data.deleted_n ?? 0);
            const errN = Number(data.remove_error_n ?? data.error_n ?? 0);
            setExportMsg(
                errN > 0
                    ? `${op}: deleted=${n} · errors=${errN}`
                    : `${op}: deleted=${n} · freed=${data.freed_bytes ?? 0}`
            );
            void refreshLastE2E();
        });
        return () => {
            EventsOff(EVENT_COMPUTER_USE_OBSERVE);
            EventsOff(EVENT_COMPUTER_USE_ACTION);
            EventsOff(EVENT_COMPUTER_USE_ERROR);
            EventsOff(EVENT_COMPUTER_USE_CONTROL);
            EventsOff(EVENT_COMPUTER_USE_LOGS);
        };
    }, [t, refreshHistorySparkline, refreshLastE2E]);

    // Refresh status when pinned or after controls.
    useEffect(() => {
        if (!pinned && !open) return;
        let cancelled = false;
        const load = async () => {
            try {
                const st: any = await GetComputerUseStatus();
                if (!cancelled) applyStatus(st);
            } catch {
                /* ignore */
            }
        };
        void load();
        void refreshHistorySparkline();
        void refreshLastE2E();
        const id = window.setInterval(load, 8000);
        return () => {
            cancelled = true;
            window.clearInterval(id);
        };
    }, [pinned, open, applyStatus, refreshHistorySparkline, refreshLastE2E]);

    const runControl = async (fn: () => Promise<void>) => {
        setCtrlBusy(true);
        setCtrlError("");
        try {
            await fn();
            const st: any = await GetComputerUseStatus();
            applyStatus(st);
        } catch (e: any) {
            setCtrlError(e?.message || String(e));
        } finally {
            setCtrlBusy(false);
        }
    };

    const togglePin = () => {
        setPinned((p) => {
            const next = !p;
            writeBool(STORAGE_PIN, next);
            if (next) setOpen(true);
            return next;
        });
    };

    const toggleDock = () => {
        setDocked((d) => {
            const next = !d;
            writeBool(STORAGE_DOCK, next);
            return next;
        });
    };

    if (!open && !pinned && !observe && actions.length === 0) {
        return null;
    }

    const elements = observe?.elements || [];
    const w = observe?.meta?.width || 0;
    const h = observe?.meta?.height || 0;
    const visible = open || pinned;

    const shellStyle: import("react").CSSProperties = docked
        ? {
              position: "fixed",
              top: 56,
              right: 0,
              bottom: 0,
              width: 340,
              zIndex: 12000,
              borderRadius: "12px 0 0 0",
              border: flash ? "1px solid var(--theme-primary, #2f5f98)" : "1px solid rgba(127,127,127,0.35)",
              borderRight: "none",
              background: "var(--theme-panel-bg, rgba(20,22,28,0.96))",
              color: "var(--theme-text, #e8eaed)",
              boxShadow: "-6px 0 24px rgba(0,0,0,0.22)",
              fontSize: 12,
              display: "flex",
              flexDirection: "column",
              overflow: "hidden",
          }
        : {
              position: "fixed",
              right: 16,
              bottom: 88,
              width: 320,
              maxHeight: "50vh",
              zIndex: 12000,
              borderRadius: 12,
              border: flash ? "1px solid var(--theme-primary, #2f5f98)" : "1px solid rgba(127,127,127,0.35)",
              background: "var(--theme-panel-bg, rgba(20,22,28,0.94))",
              color: "var(--theme-text, #e8eaed)",
              boxShadow: "0 8px 28px rgba(0,0,0,0.28)",
              fontSize: 12,
              display: "flex",
              flexDirection: "column",
              overflow: "hidden",
          };

    const iconBtn: import("react").CSSProperties = {
        background: "transparent",
        border: "none",
        color: "inherit",
        cursor: "pointer",
        opacity: 0.85,
        padding: "0 4px",
        fontSize: 13,
    };

    return (
        <div style={shellStyle} data-testid="computer-use-operator-panel">
            <div
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "8px 10px",
                    borderBottom: "1px solid rgba(127,127,127,0.25)",
                    fontWeight: 600,
                    gap: 6,
                    flexShrink: 0,
                }}
            >
                <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {t("Computer Use", "桌面操控", "桌面操控")}
                    {observe?.element_count != null ? ` · ${observe.element_count}` : ""}
                </span>
                <span style={{ display: "flex", alignItems: "center", flexShrink: 0 }}>
                    <button
                        type="button"
                        title={pinned ? t("Unpin", "取消固定", "取消固定") : t("Pin", "固定", "固定")}
                        onClick={togglePin}
                        style={{ ...iconBtn, opacity: pinned ? 1 : 0.7, color: pinned ? "var(--theme-primary, #8ab4f8)" : "inherit" }}
                    >
                        📌
                    </button>
                    <button
                        type="button"
                        title={docked ? t("Float", "浮窗", "浮窗") : t("Dock right", "靠右停靠", "靠右停靠")}
                        onClick={toggleDock}
                        style={{ ...iconBtn, opacity: docked ? 1 : 0.7 }}
                    >
                        ▥
                    </button>
                    <button type="button" onClick={() => setOpen((v) => !v)} style={iconBtn}>
                        {visible && open ? "−" : "+"}
                    </button>
                </span>
            </div>
            {open && (
                <div
                    style={{
                        padding: 10,
                        overflow: "auto",
                        display: "flex",
                        flexDirection: "column",
                        gap: 8,
                        flex: docked ? 1 : undefined,
                    }}
                >
                    {(pinned || statusLine) && statusLine && (
                        <div style={{ opacity: 0.85, fontSize: 11 }}>{statusLine}</div>
                    )}
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                        <button
                            type="button"
                            disabled={ctrlBusy || stopped || paused}
                            onClick={() => void runControl(() => ComputerUsePause())}
                            style={ctrlBtnStyle(false)}
                        >
                            {t("Pause", "暂停", "暫停")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || stopped || !paused}
                            onClick={() => void runControl(() => ComputerUseResume())}
                            style={ctrlBtnStyle(false)}
                        >
                            {t("Resume", "继续", "繼續")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || stopped}
                            onClick={() => void runControl(() => ComputerUseStop())}
                            style={ctrlBtnStyle(true)}
                        >
                            {t("Stop", "停止", "停止")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy}
                            onClick={() => void runControl(() => ComputerUseReset())}
                            style={ctrlBtnStyle(false)}
                        >
                            {t("Reset", "复位", "復位")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || smokeBusy}
                            onClick={() => {
                                void (async () => {
                                    setSmokeBusy(true);
                                    setCtrlError("");
                                    try {
                                        const sm: any = await ComputerUseSmokeCheck();
                                        if (sm) {
                                            setObserve({
                                                ok: !!sm.ok,
                                                element_count: sm.element_count,
                                                window_count: sm.window_count,
                                                yolo_count: sm.yolo_count,
                                                a11y_count: sm.a11y_count,
                                                timing_ms: sm.timing_ms,
                                                total_ms: sm.ms,
                                                meta: { width: sm.width, height: sm.height },
                                                error: sm.error,
                                                guidance: sm.guidance,
                                                action: sm.action,
                                                stage: "smoke",
                                            });
                                            if (!sm.ok) {
                                                setLastError({
                                                    stage: "smoke",
                                                    error: sm.error,
                                                    guidance: sm.guidance,
                                                    action: sm.action,
                                                });
                                            } else {
                                                setLastError(null);
                                            }
                                            setOpen(true);
                                        }
                                        const st: any = await GetComputerUseStatus();
                                        applyStatus(st);
                                        await refreshHistorySparkline();
                                    } catch (e: any) {
                                        setCtrlError(e?.message || String(e));
                                    } finally {
                                        setSmokeBusy(false);
                                    }
                                })();
                            }}
                            style={ctrlBtnStyle(false)}
                            title={t("Run smoke observe", "运行观察冒烟", "執行觀察冒煙")}
                        >
                            {smokeBusy
                                ? t("Smoke…", "冒烟中…", "冒煙中…")
                                : t("Smoke", "冒烟", "冒煙")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || smokeBusy}
                            onClick={() => {
                                void (async () => {
                                    setSmokeBusy(true);
                                    setCtrlError("");
                                    setExportMsg("");
                                    try {
                                        const e2e: any = await ComputerUseE2ESmoke();
                                        setExportMsg(
                                            e2e?.ok
                                                ? t(
                                                      `E2E ok · ${e2e.ms ?? "?"}ms · steps=${(e2e.steps || []).length}`,
                                                      `E2E 通过 · ${e2e.ms ?? "?"}ms · 步骤=${(e2e.steps || []).length}`,
                                                      `E2E 通過 · ${e2e.ms ?? "?"}ms · 步驟=${(e2e.steps || []).length}`
                                                  )
                                                : t(
                                                      `E2E failed: ${e2e?.error || "unknown"}`,
                                                      `E2E 失败: ${e2e?.error || "未知"}`,
                                                      `E2E 失敗: ${e2e?.error || "未知"}`
                                                  )
                                        );
                                        const st: any = await GetComputerUseStatus();
                                        applyStatus(st);
                                        await refreshHistorySparkline();
                                        setOpen(true);
                                    } catch (e: any) {
                                        setCtrlError(e?.message || String(e));
                                    } finally {
                                        setSmokeBusy(false);
                                    }
                                })();
                            }}
                            style={ctrlBtnStyle(false)}
                            title={t(
                                "E2E: smoke + launch simple editor + observe",
                                "E2E：冒烟 + 启动简单编辑器 + 观察",
                                "E2E：冒煙 + 啟動簡單編輯器 + 觀察"
                            )}
                        >
                            {t("E2E", "E2E", "E2E")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || smokeBusy}
                            onClick={() => {
                                void (async () => {
                                    setSmokeBusy(true);
                                    setCtrlError("");
                                    setExportMsg("");
                                    try {
                                        const e2e: any = await ComputerUseE2EInteract();
                                        const extra =
                                            e2e?.token != null
                                                ? ` · token=${e2e.token}${e2e.token_found ? "✓" : "?"}`
                                                : e2e?.soft_fail
                                                  ? ` · soft_fail${e2e.skip_reason ? `=${e2e.skip_reason}` : ""}`
                                                  : "";
                                        setExportMsg(
                                            e2e?.ok
                                                ? t(
                                                      `E2E interact ok · ${e2e.ms ?? "?"}ms${extra}`,
                                                      `E2E 交互通过 · ${e2e.ms ?? "?"}ms${extra}`,
                                                      `E2E 交互通過 · ${e2e.ms ?? "?"}ms${extra}`
                                                  )
                                                : t(
                                                      `E2E interact failed: ${e2e?.error || e2e?.skip_reason || "unknown"}${extra}`,
                                                      `E2E 交互失败: ${e2e?.error || e2e?.skip_reason || "未知"}${extra}`,
                                                      `E2E 交互失敗: ${e2e?.error || e2e?.skip_reason || "未知"}${extra}`
                                                  )
                                        );
                                        if (e2e) setLastE2E(e2e as LastE2E);
                                        const st: any = await GetComputerUseStatus();
                                        applyStatus(st);
                                        await refreshHistorySparkline();
                                        setOpen(true);
                                    } catch (e: any) {
                                        setCtrlError(e?.message || String(e));
                                    } finally {
                                        setSmokeBusy(false);
                                    }
                                })();
                            }}
                            style={ctrlBtnStyle(false)}
                            title={t(
                                "E2E with type into editor (moves cursor/focus)",
                                "E2E 并输入到编辑器（会移动光标/焦点）",
                                "E2E 並輸入到編輯器（會移動游標/焦點）"
                            )}
                        >
                            {t("E2E+", "E2E+", "E2E+")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || smokeBusy}
                            onClick={() => {
                                void (async () => {
                                    setSmokeBusy(true);
                                    setCtrlError("");
                                    setExportMsg("");
                                    try {
                                        const exp: any = await ExportComputerUseDiagnostics();
                                        if (exp?.ok) {
                                            setExportMsg(
                                                t(
                                                    `Exported: ${exp.path}`,
                                                    `已导出: ${exp.path}`,
                                                    `已匯出: ${exp.path}`
                                                )
                                            );
                                        } else {
                                            setCtrlError(exp?.error || t("Export failed", "导出失败", "匯出失敗"));
                                        }
                                    } catch (e: any) {
                                        setCtrlError(e?.message || String(e));
                                    } finally {
                                        setSmokeBusy(false);
                                    }
                                })();
                            }}
                            style={ctrlBtnStyle(false)}
                            title={t("Export diagnostics JSON", "导出诊断 JSON", "匯出診斷 JSON")}
                        >
                            {t("Export", "导出", "匯出")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || smokeBusy}
                            onClick={() => {
                                void (async () => {
                                    setSmokeBusy(true);
                                    setCtrlError("");
                                    setExportMsg("");
                                    try {
                                        const exp: any = await ExportComputerUseObserveHistoryCSV();
                                        if (exp?.ok) {
                                            setExportMsg(
                                                t(
                                                    `CSV: ${exp.path} (${exp.rows ?? 0})`,
                                                    `CSV: ${exp.path}（${exp.rows ?? 0}）`,
                                                    `CSV: ${exp.path}（${exp.rows ?? 0}）`
                                                )
                                            );
                                        } else {
                                            setCtrlError(exp?.error || t("CSV export failed", "CSV 导出失败", "CSV 匯出失敗"));
                                        }
                                    } catch (e: any) {
                                        setCtrlError(e?.message || String(e));
                                    } finally {
                                        setSmokeBusy(false);
                                    }
                                })();
                            }}
                            style={ctrlBtnStyle(false)}
                            title={t("Export observe history CSV", "导出 observe 历史 CSV", "匯出 observe 歷史 CSV")}
                        >
                            {t("CSV", "CSV", "CSV")}
                        </button>
                        <button
                            type="button"
                            disabled={ctrlBusy || smokeBusy}
                            onClick={() => {
                                void (async () => {
                                    setCtrlError("");
                                    setExportMsg("");
                                    try {
                                        const r: any = await OpenComputerUseLastDiagnostics();
                                        if (r?.ok) {
                                            setExportMsg(t(`Opened: ${r.path}`, `已打开: ${r.path}`, `已打開: ${r.path}`));
                                        } else {
                                            const logs: any = await OpenComputerUseLogsFolder();
                                            if (logs?.ok) {
                                                setExportMsg(t(`Logs: ${logs.path}`, `日志: ${logs.path}`, `日誌: ${logs.path}`));
                                            } else {
                                                setCtrlError(r?.error || logs?.error || "open failed");
                                            }
                                        }
                                    } catch (e: any) {
                                        setCtrlError(e?.message || String(e));
                                    }
                                })();
                            }}
                            style={ctrlBtnStyle(false)}
                            title={t("Open last diagnostics / logs folder", "打开最近诊断 / 日志目录", "打開最近診斷 / 日誌目錄")}
                        >
                            {t("Folder", "目录", "目錄")}
                        </button>
                    </div>
                    {ctrlError && (
                        <div style={{ color: "#f28b82", fontSize: 11 }}>{ctrlError}</div>
                    )}
                    {exportMsg && (
                        <div style={{ opacity: 0.85, fontSize: 11, wordBreak: "break-all" }}>{exportMsg}</div>
                    )}
                    {lastE2E && (lastE2E.ok === true || lastE2E.ok === false) && (
                        <div
                            data-testid="cu-last-e2e-card"
                            style={{
                                border: lastE2E.ok
                                    ? "1px solid rgba(129, 201, 149, 0.45)"
                                    : "1px solid rgba(242,139,130,0.45)",
                                background: lastE2E.ok
                                    ? "rgba(129, 201, 149, 0.08)"
                                    : "rgba(242,139,130,0.1)",
                                borderRadius: 8,
                                padding: "6px 8px",
                                fontSize: 11,
                                display: "flex",
                                flexDirection: "column",
                                gap: 4,
                            }}
                        >
                            <div style={{ fontWeight: 600 }}>
                                {t("Last E2E", "最近 E2E", "最近 E2E")}
                                {lastE2E.interact ? "+" : ""}:{" "}
                                <span
                                    style={{
                                        color: lastE2E.ok
                                            ? "#81c993"
                                            : lastE2E.soft_fail
                                              ? "#fdd663"
                                              : "#f28b82",
                                    }}
                                >
                                    {lastE2E.ok ? "ok" : lastE2E.soft_fail ? "soft_fail" : "fail"}
                                </span>
                                {lastE2E.ms != null ? ` · ${lastE2E.ms}ms` : ""}
                                {lastE2E.token_found === true
                                    ? ` · token✓`
                                    : lastE2E.token_found === false || lastE2E.token_unconfirmed
                                      ? ` · token?`
                                      : ""}
                                {lastE2E.focus_retry ? ` · retry` : ""}
                                {lastE2E.skip_reason ? ` · ${lastE2E.skip_reason}` : ""}
                            </div>
                            {lastE2E.error ? (
                                <div style={{ opacity: 0.9 }}>{String(lastE2E.error).slice(0, 220)}</div>
                            ) : null}
                            {(lastE2E.diagnostics_path || lastE2E.history_csv_path) && (
                                <div style={{ opacity: 0.75, wordBreak: "break-all" }}>
                                    {lastE2E.diagnostics_path
                                        ? `diag: …/${String(lastE2E.diagnostics_path).split(/[/\\]/).pop()}`
                                        : ""}
                                    {lastE2E.history_csv_path
                                        ? `${lastE2E.diagnostics_path ? " · " : ""}csv: …/${String(lastE2E.history_csv_path).split(/[/\\]/).pop()}`
                                        : ""}
                                </div>
                            )}
                            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                                {lastE2E.diagnostics_path ? (
                                    <button
                                        type="button"
                                        style={ctrlBtnStyle(false)}
                                        onClick={() => {
                                            void CopyComputerUsePath("diagnostics").then((r: any) => {
                                                setExportMsg(
                                                    r?.ok
                                                        ? t("Diag path copied", "诊断路径已复制", "診斷路徑已複製")
                                                        : r?.error || "copy failed"
                                                );
                                            });
                                        }}
                                    >
                                        {t("Copy diag", "复制诊断", "複製診斷")}
                                    </button>
                                ) : null}
                                {lastE2E.history_csv_path ? (
                                    <button
                                        type="button"
                                        style={ctrlBtnStyle(false)}
                                        onClick={() => {
                                            void CopyComputerUsePath("csv").then((r: any) => {
                                                setExportMsg(
                                                    r?.ok
                                                        ? t("CSV path copied", "CSV 路径已复制", "CSV 路徑已複製")
                                                        : r?.error || "copy failed"
                                                );
                                            });
                                        }}
                                    >
                                        {t("Copy CSV", "复制 CSV", "複製 CSV")}
                                    </button>
                                ) : null}
                                <button
                                    type="button"
                                    style={ctrlBtnStyle(false)}
                                    onClick={() => {
                                        void OpenComputerUseLastDiagnostics().then((r: any) => {
                                            if (r?.ok) setExportMsg(t(`Opened: ${r.path}`, `已打开: ${r.path}`, `已打開: ${r.path}`));
                                        });
                                    }}
                                >
                                    {t("Open", "打开", "打開")}
                                </button>
                                <button
                                    type="button"
                                    style={ctrlBtnStyle(false)}
                                    title={t("Prune using saved policy (confirm)", "按已保存策略清理（需确认）", "按已保存策略清理（需確認）")}
                                    onClick={() => {
                                        void (async () => {
                                            let keep = 10;
                                            let age = 0;
                                            try {
                                                const pol: any = await GetComputerUseLogPrunePolicy();
                                                keep = Number(pol?.keep_newest ?? 10) || 10;
                                                age = Number(pol?.max_age_days ?? 0) || 0;
                                            } catch {
                                                /* defaults */
                                            }
                                            const ok = window.confirm(
                                                t(
                                                    `Delete CU logs beyond newest ${keep} per kind${age > 0 ? ` or older than ${age}d` : ""}?`,
                                                    `删除超出最新 ${keep} 个${age > 0 ? `或超过 ${age} 天` : ""} 的 CU 日志？`,
                                                    `刪除超出最新 ${keep} 個${age > 0 ? `或超過 ${age} 天` : ""} 的 CU 日誌？`
                                                )
                                            );
                                            if (!ok) return;
                                            const r: any = await PruneComputerUseLogArtifacts(keep, age);
                                            if (r?.ok) {
                                                const errN = Number(r.remove_error_n ?? 0);
                                                setExportMsg(
                                                    t(
                                                        `Pruned ${r.deleted_n ?? 0}` +
                                                            (errN ? ` (${errN} remove errors)` : "") +
                                                            ` · logs≈${artifactCount}`,
                                                        `已清理 ${r.deleted_n ?? 0}` +
                                                            (errN ? `（${errN} 删除失败）` : "") +
                                                            ` · 日志≈${artifactCount}`,
                                                        `已清理 ${r.deleted_n ?? 0}` +
                                                            (errN ? `（${errN} 刪除失敗）` : "") +
                                                            ` · 日誌≈${artifactCount}`
                                                    )
                                                );
                                                await refreshLastE2E();
                                            }
                                        })();
                                    }}
                                >
                                    {t("Prune", "清理", "清理")}
                                    {artifactCount ? ` (${artifactCount})` : ""}
                                </button>
                            </div>
                        </div>
                    )}
                    {(historyLine || historyTotals.length > 0) && (
                        <div data-testid="cu-observe-history">
                            {historyLine && (
                                <div style={{ opacity: 0.8, fontSize: 11 }}>{historyLine}</div>
                            )}
                            {historyTotals.length > 0 && (
                                <ObserveHistorySparkline values={historyTotals} />
                            )}
                        </div>
                    )}
                    {lastError && (lastError.error || lastError.guidance) && (
                        <div
                            style={{
                                border: "1px solid rgba(242,139,130,0.45)",
                                background: "rgba(242,139,130,0.1)",
                                borderRadius: 8,
                                padding: "6px 8px",
                                fontSize: 11,
                                display: "flex",
                                flexDirection: "column",
                                gap: 4,
                            }}
                        >
                            <div style={{ fontWeight: 600, color: "#f28b82" }}>
                                {t("Last error", "最近错误", "最近錯誤")}
                                {lastError.stage ? ` · ${lastError.stage}` : ""}
                            </div>
                            {lastError.guidance && <div>{lastError.guidance}</div>}
                            {lastError.error && !lastError.guidance && <div>{lastError.error}</div>}
                            {lastError.error && lastError.guidance && (
                                <div style={{ opacity: 0.75 }}>{lastError.error}</div>
                            )}
                            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                                {lastError.action ? (
                                    <button
                                        type="button"
                                        style={ctrlBtnStyle(false)}
                                        onClick={() => {
                                            const act = lastError.action || "";
                                            const target =
                                                act === "open_screen_recording"
                                                    ? "screen_recording"
                                                    : act === "open_privacy"
                                                      ? "privacy"
                                                      : "accessibility";
                                            void OpenComputerUsePermissionSettings(target).catch(() => undefined);
                                        }}
                                    >
                                        {t("Open settings", "打开设置", "打開設定")}
                                    </button>
                                ) : null}
                                <button
                                    type="button"
                                    style={ctrlBtnStyle(false)}
                                    onClick={() => {
                                        setLastError(null);
                                        void GetComputerUseLastError().catch(() => undefined);
                                    }}
                                >
                                    {t("Dismiss", "忽略", "忽略")}
                                </button>
                            </div>
                        </div>
                    )}
                    {observe && (observe.timing_ms || observe.total_ms != null) && (
                        <div style={{ opacity: 0.8, fontSize: 11 }} data-testid="cu-observe-timing">
                            {t("Timing", "耗时", "耗時")}: {formatTiming(observe.timing_ms, observe.total_ms)}
                            {observe.yolo_count != null || observe.a11y_count != null || observe.ocr_count != null
                                ? ` · y=${observe.yolo_count ?? "?"} a=${observe.a11y_count ?? "?"} o=${observe.ocr_count ?? "?"}`
                                : ""}
                        </div>
                    )}
                    {!observe && actions.length === 0 && pinned && !lastError && (
                        <div style={{ opacity: 0.7 }}>
                            {t(
                                "No activity yet. Ask the agent to operate the desktop (@computer), or run Smoke.",
                                "尚无活动。可让助手操作桌面（@computer），或点「冒烟」。",
                                "尚無活動。可讓助手操作桌面（@computer），或點「冒煙」。"
                            )}
                        </div>
                    )}
                    {w > 0 && h > 0 && elements.length > 0 && (
                        <div
                            style={{
                                position: "relative",
                                width: "100%",
                                aspectRatio: `${w} / ${h}`,
                                maxHeight: docked ? 200 : 140,
                                background: "rgba(0,0,0,0.25)",
                                borderRadius: 8,
                                overflow: "hidden",
                                flexShrink: 0,
                            }}
                            title={t("Element map (no screenshot)", "元素分布（无截图）", "元素分布（無截圖）")}
                        >
                            {elements.slice(0, 48).map((el) => {
                                const [cx = 0, cy = 0] = el.center || [];
                                const left = Math.min(98, Math.max(0, (cx / w) * 100));
                                const top = Math.min(98, Math.max(0, (cy / h) * 100));
                                return (
                                    <div
                                        key={el.ref}
                                        title={`${el.ref} ${el.name || ""}`}
                                        style={{
                                            position: "absolute",
                                            left: `${left}%`,
                                            top: `${top}%`,
                                            transform: "translate(-50%, -50%)",
                                            width: 8,
                                            height: 8,
                                            borderRadius: 2,
                                            background: "var(--theme-primary, #5b9bd5)",
                                            boxShadow: "0 0 0 1px rgba(255,255,255,0.4)",
                                            fontSize: 0,
                                        }}
                                    />
                                );
                            })}
                        </div>
                    )}
                    {elements.length > 0 && (
                        <div style={{ maxHeight: docked ? 220 : 120, overflow: "auto" }}>
                            {elements.slice(0, docked ? 32 : 16).map((el) => (
                                <div key={el.ref} style={{ opacity: 0.92, lineHeight: 1.35 }}>
                                    <code style={{ color: "var(--theme-primary, #8ab4f8)" }}>{el.ref}</code>{" "}
                                    {el.name || el.type || "—"}
                                    {el.source ? (
                                        <span style={{ opacity: 0.55 }}> · {el.source}</span>
                                    ) : null}
                                </div>
                            ))}
                        </div>
                    )}
                    {observe?.windows && observe.windows.length > 0 && (
                        <div style={{ opacity: 0.75, fontSize: 11 }}>
                            {t("Windows", "窗口", "視窗")}: {observe.windows.slice(0, 6).join(" · ")}
                        </div>
                    )}
                    {observe?.ocr_excerpt && (
                        <div style={{ opacity: 0.75, fontSize: 11 }}>
                            OCR: {observe.ocr_excerpt.slice(0, 200)}
                            {observe.ocr_excerpt.length > 200 ? "…" : ""}
                        </div>
                    )}
                    {actions.length > 0 && (
                        <div>
                            <div style={{ fontWeight: 600, marginBottom: 4 }}>
                                {t("Recent actions", "最近动作", "最近動作")}
                            </div>
                            {actions.map((a, i) => (
                                <div
                                    key={`${a.at}-${i}`}
                                    style={{
                                        color: a.ok === false ? "#f28b82" : "inherit",
                                        lineHeight: 1.35,
                                    }}
                                >
                                    {a.action}: {a.detail}
                                    {a.error ? ` — ${a.error}` : ""}
                                </div>
                            ))}
                        </div>
                    )}
                    <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
                        {!pinned && (
                            <button
                                type="button"
                                onClick={() => {
                                    setOpen(false);
                                    setObserve(null);
                                    setActions([]);
                                }}
                                style={{
                                    background: "transparent",
                                    border: "1px solid rgba(127,127,127,0.4)",
                                    borderRadius: 6,
                                    color: "inherit",
                                    padding: "2px 8px",
                                    cursor: "pointer",
                                }}
                            >
                                {t("Dismiss", "关闭", "關閉")}
                            </button>
                        )}
                        {pinned && (
                            <button
                                type="button"
                                onClick={() => {
                                    setObserve(null);
                                    setActions([]);
                                    setLastError(null);
                                }}
                                style={{
                                    background: "transparent",
                                    border: "1px solid rgba(127,127,127,0.4)",
                                    borderRadius: 6,
                                    color: "inherit",
                                    padding: "2px 8px",
                                    cursor: "pointer",
                                }}
                            >
                                {t("Clear log", "清空日志", "清空日誌")}
                            </button>
                        )}
                        <button
                            type="button"
                            onClick={() => {
                                void (async () => {
                                    try {
                                        const m: any = await GetComputerUseLastObserveMetrics();
                                        if (m && Object.keys(m).length) setObserve(m as ObservePayload);
                                        const e: any = await GetComputerUseLastError();
                                        if (e && (e.error || e.guidance)) setLastError(e);
                                    } catch {
                                        /* ignore */
                                    }
                                })();
                            }}
                            style={{
                                background: "transparent",
                                border: "1px solid rgba(127,127,127,0.4)",
                                borderRadius: 6,
                                color: "inherit",
                                padding: "2px 8px",
                                cursor: "pointer",
                            }}
                        >
                            {t("Refresh metrics", "刷新指标", "刷新指標")}
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}

function ctrlBtnStyle(danger: boolean): import("react").CSSProperties {
    return {
        background: danger ? "rgba(242,139,130,0.15)" : "transparent",
        border: danger ? "1px solid rgba(242,139,130,0.55)" : "1px solid rgba(127,127,127,0.4)",
        borderRadius: 6,
        color: "inherit",
        padding: "3px 8px",
        cursor: "pointer",
        fontSize: 11,
        opacity: 0.95,
    };
}

/** Compact bar sparkline for observe total_ms history (oldest → newest). */
function ObserveHistorySparkline({ values }: { values: number[] }) {
    if (!values.length) return null;
    const max = Math.max(...values, 1);
    const last = values[values.length - 1] ?? 0;
    return (
        <div
            data-testid="cu-history-sparkline"
            title={values.map((v, i) => `#${i + 1}: ${v}ms`).join("\n")}
            style={{
                display: "flex",
                alignItems: "flex-end",
                gap: 2,
                height: 28,
                marginTop: 4,
                padding: "2px 0",
            }}
        >
            {values.map((v, i) => {
                const h = Math.max(2, Math.round((v / max) * 26));
                const isLast = i === values.length - 1;
                return (
                    <div
                        key={`${i}-${v}`}
                        style={{
                            width: 5,
                            height: h,
                            borderRadius: 1,
                            background: isLast
                                ? "var(--theme-primary, #8ab4f8)"
                                : "rgba(138, 180, 248, 0.45)",
                        }}
                    />
                );
            })}
            <span style={{ marginLeft: 6, fontSize: 10, opacity: 0.75, alignSelf: "center" }}>
                {last}ms
            </span>
        </div>
    );
}

export default ComputerUseOperatorPanel;
