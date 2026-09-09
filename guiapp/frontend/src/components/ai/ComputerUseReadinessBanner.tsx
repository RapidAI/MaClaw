/**
 * Shows setup / recovery hints when Computer Use needs attention:
 * missing YOLO weights, OS permissions, or recent observe failures.
 * Each issue can be dismissed independently (localStorage).
 */
import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import {
    ComputerUseE2EInteract,
    ComputerUseSelfCheck,
    ComputerUseSmokeCheck,
    DownloadYOLOModel,
    ExportComputerUseDiagnostics,
    GetComputerUseReadiness,
    OpenComputerUseLastDiagnostics,
    OpenComputerUseLogsFolder,
    OpenComputerUsePermissionSettings,
} from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import {
    EVENT_COMPUTER_USE_ERROR,
    EVENT_COMPUTER_USE_OBSERVE,
    EVENT_COMPUTER_USE_WARMUP,
} from "../../constants/events";
import { localizeText } from "../../i18n";
import type { Theme } from "./aiAssistantPanelTheme";

type Props = {
    lang: string;
    theme: Theme;
};

type Issue = {
    id: string;
    severity?: string;
    message?: string;
    action?: string;
    stage?: string;
};

const DISMISS_IDS_KEY = "maclaw.computer_use.readiness.dismissed_ids";

function loadDismissedIds(): Set<string> {
    try {
        const raw = localStorage.getItem(DISMISS_IDS_KEY);
        if (!raw) return new Set();
        const arr = JSON.parse(raw);
        if (!Array.isArray(arr)) return new Set();
        return new Set(arr.filter((x) => typeof x === "string"));
    } catch {
        return new Set();
    }
}

function saveDismissedIds(ids: Set<string>) {
    try {
        localStorage.setItem(DISMISS_IDS_KEY, JSON.stringify([...ids]));
    } catch {
        /* ignore */
    }
}

export function ComputerUseReadinessBanner({ lang, theme: t }: Props) {
    const tr = useCallback(
        (en: string, zh: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant),
        [lang]
    );
    const [allIssues, setAllIssues] = useState<Issue[]>([]);
    const [enabled, setEnabled] = useState(true);
    const [dismissedIds, setDismissedIds] = useState<Set<string>>(loadDismissedIds);
    const [busy, setBusy] = useState(false);

    const refresh = useCallback(async () => {
        try {
            const r: any = await GetComputerUseReadiness();
            if (!r || r.enabled === false) {
                setEnabled(false);
                setAllIssues([]);
                return;
            }
            setEnabled(true);
            const list = Array.isArray(r.issues) ? (r.issues as Issue[]) : [];
            const visible = list.filter((i) => i.severity === "warn" || i.severity === "error");
            setAllIssues(visible);

            // Drop dismiss entries for issues that no longer exist (can re-show later).
            setDismissedIds((prev) => {
                if (prev.size === 0) return prev;
                const active = new Set(visible.map((i) => i.id));
                let changed = false;
                const next = new Set<string>();
                prev.forEach((id) => {
                    if (active.has(id)) next.add(id);
                    else changed = true;
                });
                if (changed) saveDismissedIds(next);
                return changed ? next : prev;
            });
        } catch {
            /* ignore */
        }
    }, []);

    useEffect(() => {
        void refresh();
        const id = window.setInterval(() => void refresh(), 15000);
        EventsOn(EVENT_COMPUTER_USE_WARMUP, () => void refresh());
        EventsOn(EVENT_COMPUTER_USE_ERROR, () => void refresh());
        EventsOn(EVENT_COMPUTER_USE_OBSERVE, (data: any) => {
            if (data && data.ok === false) void refresh();
        });
        return () => {
            window.clearInterval(id);
            EventsOff(EVENT_COMPUTER_USE_WARMUP);
            EventsOff(EVENT_COMPUTER_USE_ERROR);
            EventsOff(EVENT_COMPUTER_USE_OBSERVE);
        };
    }, [refresh]);

    const issues = useMemo(
        () => allIssues.filter((i) => !dismissedIds.has(i.id)),
        [allIssues, dismissedIds]
    );

    // Show whenever there are undismissed warn/error issues (even if backend ready=false).
    if (!enabled || issues.length === 0) {
        return null;
    }

    const runAction = async (action?: string) => {
        if (!action) return;
        setBusy(true);
        try {
            if (action === "download_yolo") {
                await DownloadYOLOModel();
            } else if (action === "open_accessibility" || action === "open_privacy") {
                await OpenComputerUsePermissionSettings(
                    action === "open_privacy" ? "privacy" : "accessibility"
                );
            } else if (action === "open_screen_recording") {
                await OpenComputerUsePermissionSettings("screen_recording");
            } else if (action === "export_diagnostics") {
                await ExportComputerUseDiagnostics();
            } else if (action === "open_diagnostics") {
                const r: any = await OpenComputerUseLastDiagnostics();
                if (!r?.ok) await OpenComputerUseLogsFolder();
            } else if (action === "open_logs_folder") {
                await OpenComputerUseLogsFolder();
            } else if (action === "run_e2e_interact") {
                await ComputerUseE2EInteract();
            }
            await ComputerUseSelfCheck().catch(() => undefined);
            await refresh();
        } catch {
            await refresh();
        } finally {
            setBusy(false);
        }
    };

    const dismissOne = (id: string) => {
        setDismissedIds((prev) => {
            const next = new Set(prev);
            next.add(id);
            saveDismissedIds(next);
            return next;
        });
    };

    const dismissAll = () => {
        setDismissedIds((prev) => {
            const next = new Set(prev);
            issues.forEach((i) => next.add(i.id));
            saveDismissedIds(next);
            return next;
        });
    };

    const wrap: CSSProperties = {
        display: "flex",
        flexDirection: "column",
        gap: 6,
        padding: "8px 12px",
        borderTop: `1px solid ${t.divider}`,
        background: "rgba(234, 179, 8, 0.08)",
        color: t.textMuted || t.text,
        fontSize: 12,
        lineHeight: 1.45,
    };
    const btn: CSSProperties = {
        border: `1px solid ${t.divider}`,
        background: "transparent",
        color: t.text,
        borderRadius: 6,
        padding: "2px 8px",
        fontSize: 11,
        cursor: busy ? "wait" : "pointer",
        whiteSpace: "nowrap",
    };
    const row: CSSProperties = {
        display: "flex",
        alignItems: "flex-start",
        gap: 8,
        flexWrap: "wrap",
    };

    const msgFor = (iss: Issue) => {
        switch (iss.id) {
            case "yolo_missing":
                return tr(
                    "OmniParser weights missing — desktop parsing will use a11y/OCR only.",
                    "缺少 OmniParser 权重，桌面解析将仅依赖 a11y/OCR。",
                    "缺少 OmniParser 權重，桌面解析將僅依賴 a11y/OCR。"
                );
            case "accessibility":
                return tr(
                    "Grant macOS Accessibility so Computer Use can read UI trees.",
                    "请授予 macOS「辅助功能」权限，以便 Computer Use 读取界面树。",
                    "請授予 macOS「輔助功能」權限，以便 Computer Use 讀取介面樹。"
                );
            case "screen_recording":
                return tr(
                    "Grant Screen Recording so screenshots work for OmniParser/OCR.",
                    "请授予「屏幕录制」权限，以便截图供 OmniParser/OCR 使用。",
                    "請授予「螢幕錄製」權限，以便截圖供 OmniParser/OCR 使用。"
                );
            case "last_e2e_failed":
                return (
                    iss.message ||
                    tr(
                        "Last Computer Use E2E check failed — export diagnostics or fix permissions.",
                        "最近一次 Computer Use E2E 失败 — 可导出诊断或检查权限。",
                        "最近一次 Computer Use E2E 失敗 — 可匯出診斷或檢查權限。"
                    )
                );
            case "last_e2e_soft_fail":
                return (
                    iss.message ||
                    tr(
                        "Last E2E+ could not complete interact (focus/launch). Re-run with editor open.",
                        "最近 E2E+ 交互未完成（焦点/启动）。请打开记事本后重跑。",
                        "最近 E2E+ 交互未完成（焦點/啟動）。請打開記事本後重跑。"
                    )
                );
            case "last_e2e_token_unverified":
                return tr(
                    "E2E typed a token but OCR did not confirm it (often OK).",
                    "E2E 已输入 token，但 OCR 未确认（通常可忽略）。",
                    "E2E 已輸入 token，但 OCR 未確認（通常可忽略）。"
                );
            default:
                if (iss.id.startsWith("last_error_")) {
                    return (
                        iss.message ||
                        tr(
                            "Recent Computer Use observe failed — see guidance.",
                            "最近一次桌面观察失败 — 请按引导处理。",
                            "最近一次桌面觀察失敗 — 請按引導處理。"
                        )
                    );
                }
                return iss.message || iss.id;
        }
    };

    const actionLabel = (action?: string) => {
        switch (action) {
            case "download_yolo":
                return tr("Download weights", "下载权重", "下載權重");
            case "open_accessibility":
                return tr("Open Accessibility", "打开辅助功能", "打開輔助功能");
            case "open_screen_recording":
                return tr("Open Screen Recording", "打开屏幕录制", "打開螢幕錄製");
            case "open_privacy":
                return tr("Open privacy settings", "打开隐私设置", "打開隱私設定");
            case "export_diagnostics":
                return tr("Export diagnostics", "导出诊断", "匯出診斷");
            case "open_diagnostics":
                return tr("Open diagnostics", "打开诊断", "打開診斷");
            case "open_logs_folder":
                return tr("Open logs folder", "打开日志目录", "打開日誌目錄");
            case "run_e2e_interact":
                return tr("Re-run E2E+", "重跑 E2E+", "重跑 E2E+");
            default:
                return tr("Fix", "处理", "處理");
        }
    };

    return (
        <div data-testid="computer-use-readiness-banner" style={wrap}>
            <div style={{ ...row, justifyContent: "space-between" }}>
                <strong style={{ color: t.headingColor || t.text }}>
                    {tr("Computer Use setup", "Computer Use 准备", "Computer Use 準備")}
                    {issues.length > 1 ? ` · ${issues.length}` : ""}
                </strong>
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                    <button
                        type="button"
                        disabled={busy}
                        style={btn}
                        title={tr("Run smoke observe", "运行观察冒烟", "執行觀察冒煙")}
                        onClick={() => {
                            void (async () => {
                                setBusy(true);
                                try {
                                    await ComputerUseSmokeCheck();
                                    await refresh();
                                } catch {
                                    await refresh();
                                } finally {
                                    setBusy(false);
                                }
                            })();
                        }}
                    >
                        {tr("Smoke", "冒烟", "冒煙")}
                    </button>
                    <button
                        type="button"
                        disabled={busy}
                        style={btn}
                        title={tr("Run self-check", "运行自检", "執行自檢")}
                        onClick={() => {
                            void (async () => {
                                setBusy(true);
                                try {
                                    await ComputerUseSelfCheck();
                                    await refresh();
                                } catch {
                                    await refresh();
                                } finally {
                                    setBusy(false);
                                }
                            })();
                        }}
                    >
                        {tr("Self-check", "自检", "自檢")}
                    </button>
                    <button type="button" style={{ ...btn, opacity: 0.75 }} onClick={dismissAll}>
                        {tr("Dismiss all", "全部关闭", "全部關閉")}
                    </button>
                </div>
            </div>
            {issues.map((iss) => (
                <div key={iss.id} style={row} data-testid={`cu-issue-${iss.id}`}>
                    <div style={{ flex: 1, minWidth: 160 }}>{msgFor(iss)}</div>
                    {iss.action ? (
                        <button type="button" disabled={busy} style={btn} onClick={() => void runAction(iss.action)}>
                            {busy ? tr("Working…", "处理中…", "處理中…") : actionLabel(iss.action)}
                        </button>
                    ) : null}
                    <button
                        type="button"
                        style={{ ...btn, opacity: 0.7 }}
                        title={tr("Dismiss this issue", "关闭此项", "關閉此項")}
                        onClick={() => dismissOne(iss.id)}
                    >
                        ×
                    </button>
                </div>
            ))}
        </div>
    );
}

export default ComputerUseReadinessBanner;
