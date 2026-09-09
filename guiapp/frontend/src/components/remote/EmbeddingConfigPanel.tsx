import { useState, useEffect, useCallback } from 'react';
import {
    GetVectorSearchEnabled, SetVectorSearchEnabled, CheckEmbeddingModel, DownloadEmbeddingModel,
    GetScreenParsingEnabled, SetScreenParsingEnabled, CheckYOLOModel, DownloadYOLOModel,
    GetComputerUseEnabled, SetComputerUseEnabled, GetComputerUseStatus,
    ComputerUseSelfCheck, GetComputerUseLastWarmup, OpenComputerUsePermissionSettingsDefault,
    ExportComputerUseDiagnostics, ExportComputerUseObserveHistoryCSV,
    ComputerUseE2ESmoke, ComputerUseE2EInteract,
    OpenComputerUseLogsFolder, OpenComputerUseLastDiagnostics,
    CopyComputerUsePath, ListComputerUseLogArtifacts, PruneComputerUseLogArtifacts,
    GetComputerUseLogPrunePolicy, SetComputerUseLogPrunePolicy, SetComputerUseLogAutoPrune,
    OpenComputerUseLogArtifact, DeleteComputerUseLogArtifact, BatchDeleteComputerUseLogArtifacts,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { EVENT_COMPUTER_USE_WARMUP, EVENT_COMPUTER_USE_LOGS } from "../../constants/events";
import { ModelStatusBox } from "./ModelStatusBox";
import { useDialog } from "../CustomDialog";

// --- Main panel ---

type Props = { lang: string };

export function EmbeddingConfigPanel({ lang }: Props) {
    const { showConfirm } = useDialog();
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

    // --- Embedding model state ---
    const [embEnabled, setEmbEnabled] = useState(false);
    const [embModelExists, setEmbModelExists] = useState(false);
    const [embModelSize, setEmbModelSize] = useState(0);
    const [embDownloading, setEmbDownloading] = useState(false);
    const [embProgress, setEmbProgress] = useState(0);
    const [embDownloaded, setEmbDownloaded] = useState(0);
    const [embTotal, setEmbTotal] = useState(0);
    const [embError, setEmbError] = useState('');

    // --- OminiParser-V2 (screen parsing) state ---
    const [spEnabled, setSpEnabled] = useState(true); // default on
    const [spModelExists, setSpModelExists] = useState(false);
    const [spModelSize, setSpModelSize] = useState(0);
    const [spDownloading, setSpDownloading] = useState(false);
    const [spProgress, setSpProgress] = useState(0);
    const [spDownloaded, setSpDownloaded] = useState(0);
    const [spTotal, setSpTotal] = useState(0);
    const [spError, setSpError] = useState('');

    // --- Computer Use product toggle ---
    const [cuEnabled, setCuEnabled] = useState(true);
    const [cuError, setCuError] = useState('');
    const [cuStatus, setCuStatus] = useState<string>('');
    const [cuChecking, setCuChecking] = useState(false);
    const [cuCheckReport, setCuCheckReport] = useState('');
    const [cuActionMsg, setCuActionMsg] = useState('');
    const [cuDiagPath, setCuDiagPath] = useState('');
    const [cuCsvPath, setCuCsvPath] = useState('');
    const [cuArtifactCount, setCuArtifactCount] = useState(0);
    const [cuArtifacts, setCuArtifacts] = useState<Array<{
        name?: string; path?: string; size?: number; kind?: string; mod_time?: string;
    }>>([]);
    const [cuShowArtifacts, setCuShowArtifacts] = useState(false);
    const [cuArtifactFilter, setCuArtifactFilter] = useState<'all' | 'diag' | 'csv'>('all');
    const [cuKeepNewest, setCuKeepNewest] = useState(10);
    const [cuMaxAgeDays, setCuMaxAgeDays] = useState(0);
    const [cuAutoPrune, setCuAutoPrune] = useState(false);
    /** Selected artifact paths for batch delete. */
    const [cuSelectedPaths, setCuSelectedPaths] = useState<Record<string, boolean>>({});

    const [loading, setLoading] = useState(true);

    const refreshCuArtifacts = useCallback(async (kind: 'all' | 'diag' | 'csv' = cuArtifactFilter) => {
        try {
            const arts: any = await ListComputerUseLogArtifacts(kind === 'all' ? 'all' : kind, 40);
            setCuArtifactCount(Number(arts?.count || 0));
            const items = Array.isArray(arts?.items) ? arts.items : [];
            setCuArtifacts(items);
            // Drop selections that no longer exist.
            setCuSelectedPaths((prev) => {
                const next: Record<string, boolean> = {};
                for (const it of items) {
                    const p = String(it?.path || '');
                    if (p && prev[p]) next[p] = true;
                }
                return next;
            });
        } catch {
            /* ignore */
        }
    }, [cuArtifactFilter]);

    // --- Init: load both model states ---
    useEffect(() => {
        (async () => {
            try {
                const embOn = await GetVectorSearchEnabled();
                setEmbEnabled(embOn);
                const embInfo: any = await CheckEmbeddingModel();
                setEmbModelExists(embInfo.exists);
                setEmbModelSize(embInfo.size || 0);
            } catch {}
            try {
                const spOn = await GetScreenParsingEnabled();
                setSpEnabled(spOn);
                const spInfo: any = await CheckYOLOModel();
                setSpModelExists(spInfo.exists);
                setSpModelSize(spInfo.size || 0);
            } catch {}
            try {
                const cuOn = await GetComputerUseEnabled();
                setCuEnabled(cuOn);
                const st: any = await GetComputerUseStatus();
                if (st) {
                    const backend = st.uia_sidecar_backend ? ` · a11y=${st.uia_sidecar_backend}` : '';
                    setCuStatus(
                        st.session_active
                            ? `active · steps=${st.step_count ?? 0} · elements=${st.element_count ?? 0}${backend}`
                            : `idle${backend}`
                    );
                }
                try {
                    const warm: any = await GetComputerUseLastWarmup();
                    if (warm && (warm.ok === true || warm.ok === false)) {
                        const ub = warm.uia?.backend ? ` backend=${warm.uia.backend}` : '';
                        setCuCheckReport(`last warmup: ok=${warm.ok}${ub} ms=${warm.ms ?? '?'}`);
                    }
                } catch {}
                try {
                    const pol: any = await GetComputerUseLogPrunePolicy();
                    if (pol) {
                        setCuKeepNewest(Number(pol.keep_newest ?? 10) || 10);
                        setCuMaxAgeDays(Number(pol.max_age_days ?? 0) || 0);
                        setCuAutoPrune(!!pol.auto_prune);
                    }
                } catch {}
                await refreshCuArtifacts('all');
            } catch {}
            setLoading(false);
        })();
    }, [refreshCuArtifacts]);

    useEffect(() => {
        EventsOn(EVENT_COMPUTER_USE_WARMUP, (data: any) => {
            if (!data) return;
            const ub = data.uia?.backend ? ` backend=${data.uia.backend}` : '';
            setCuCheckReport(`warmup: ok=${data.ok}${ub} ms=${data.ms ?? '?'}`);
        });
        return () => { EventsOff(EVENT_COMPUTER_USE_WARMUP); };
    }, []);

    // Refresh file list when backend prune/delete/batch-delete finishes.
    useEffect(() => {
        EventsOn(EVENT_COMPUTER_USE_LOGS, (data: any) => {
            if (!data) return;
            const op = String(data.op || 'logs');
            const n = Number(data.deleted_n ?? 0);
            const errN = Number(data.remove_error_n ?? data.error_n ?? 0);
            if (data.ok !== false) {
                setCuActionMsg(
                    errN > 0
                        ? `${op}: deleted=${n} errors=${errN}`
                        : `${op}: deleted=${n} freed=${data.freed_bytes ?? 0}`
                );
            } else if (data.error) {
                setCuError(String(data.error));
            }
            if (cuShowArtifacts) {
                void refreshCuArtifacts(cuArtifactFilter);
            } else {
                void ListComputerUseLogArtifacts('all', 40).then((arts: any) => {
                    setCuArtifactCount(Number(arts?.count || 0));
                }).catch(() => { /* ignore */ });
            }
        });
        return () => { EventsOff(EVENT_COMPUTER_USE_LOGS); };
    }, [cuShowArtifacts, cuArtifactFilter, refreshCuArtifacts]);

    // --- Embedding download progress ---
    useEffect(() => {
        EventsOn('embedding-download-progress', (data: any) => {
            if (data.error) { setEmbError(data.error); setEmbDownloading(false); return; }
            const pct = data.percent || 0;
            setEmbProgress(pct);
            setEmbDownloaded(data.downloaded || 0);
            setEmbTotal(data.total || 0);
            if (pct > 0 && pct < 100) { setEmbDownloading(true); }
            if (pct >= 100) {
                setEmbDownloading(false);
                setEmbModelExists(true);
                setEmbModelSize(data.downloaded || 0);
            }
        });
        return () => { EventsOff('embedding-download-progress'); };
    }, []);

    // --- YOLO download progress ---
    useEffect(() => {
        EventsOn('yolo-download-progress', (data: any) => {
            if (data.error) { setSpError(data.error); setSpDownloading(false); return; }
            const pct = data.percent || 0;
            setSpProgress(pct);
            setSpDownloaded(data.downloaded || 0);
            setSpTotal(data.total || 0);
            if (pct > 0 && pct < 100) { setSpDownloading(true); }
            if (pct >= 100) {
                setSpDownloading(false);
                setSpModelExists(true);
                setSpModelSize(data.downloaded || 0);
            }
        });
        return () => { EventsOff('yolo-download-progress'); };
    }, []);

    // --- Handlers ---
    const handleEmbToggle = async (on: boolean) => {
        setEmbEnabled(on);
        setEmbError('');
        if (on && !embModelExists) {
            setEmbDownloading(true);
        } else if (!on) {
            setEmbDownloading(false);
        }
        try {
            await SetVectorSearchEnabled(on);
        } catch (e: any) {
            setEmbEnabled(!on);
            setEmbDownloading(false);
            setEmbError(e?.message || String(e));
            return;
        }
    };

    const startEmbDownload = async () => {
        setEmbDownloading(true); setEmbProgress(0); setEmbDownloaded(0); setEmbTotal(0); setEmbError('');
        try {
            await DownloadEmbeddingModel();
        } catch (e: any) {
            setEmbError(prev => prev || (e?.message || String(e)));
            setEmbDownloading(false);
        }
    };

    const handleSpToggle = async (on: boolean) => {
        setSpEnabled(on);
        setSpError('');
        if (on && !spModelExists) {
            setSpDownloading(true);
        } else if (!on) {
            setSpDownloading(false);
        }
        try {
            await SetScreenParsingEnabled(on);
        } catch (e: any) {
            setSpEnabled(!on);
            setSpDownloading(false);
            setSpError(e?.message || String(e));
            return;
        }
    };

    const startSpDownload = async () => {
        setSpDownloading(true); setSpProgress(0); setSpDownloaded(0); setSpTotal(0); setSpError('');
        try {
            await DownloadYOLOModel();
        } catch (e: any) {
            setSpError(prev => prev || (e?.message || String(e)));
            setSpDownloading(false);
        }
    };

    const handleCuToggle = async (on: boolean) => {
        setCuEnabled(on);
        setCuError('');
        try {
            await SetComputerUseEnabled(on);
        } catch (e: any) {
            setCuEnabled(!on);
            setCuError(e?.message || String(e));
        }
    };

    const handleCuSelfCheck = async () => {
        setCuChecking(true);
        setCuError('');
        setCuCheckReport(t('Running self-check…', '正在自检…', '正在自檢…'));
        try {
            const rep: any = await ComputerUseSelfCheck();
            const backend = rep?.status?.uia_sidecar_backend || rep?.uia?.backend_after || rep?.warmup?.uia?.backend || '';
            const wins = rep?.warmup?.uia?.windows ?? rep?.uia?.warmup?.windows;
            const yolo = rep?.yolo || rep?.warmup?.yolo || {};
            const yoloPart = yolo.exists
                ? ` yolo=${yolo.loaded || yolo.warm_ok ? 'loaded' : 'file'}(${yolo.warm_ms ?? '?'}ms)`
                : (yolo.enabled === false ? ' yolo=off' : ' yolo=missing');
            const ocr = rep?.ocr || rep?.warmup?.ocr || {};
            const ocrPart = ocr.ready
                ? ` ocr=ready`
                : (ocr.installed ? ' ocr=installed' : (ocr.skipped ? ' ocr=pending' : ''));
            const perm = rep?.permissions || rep?.warmup?.permissions || {};
            let permPart = '';
            if (perm.platform === 'darwin') {
                permPart = ` a11y=${perm.accessibility_trusted ? 'ok' : 'need'} screen=${perm.screen_recording === false ? 'need' : (perm.screen_recording ? 'ok' : '?')}`;
            } else if (perm.uia_backend) {
                permPart = ` uia=${perm.uia_backend}${perm.uia_alive ? '' : '?'}`;
            }
            const warns = Array.isArray(rep?.warnings) && rep.warnings.length
                ? ` warns=${rep.warnings.join(',')}`
                : '';
            const readyPart = rep?.readiness?.ready === false
                ? ` ready=no(${(rep.readiness.issues || []).length})`
                : (rep?.readiness?.ready === true ? ' ready=yes' : '');
            const smoke = rep?.smoke || {};
            const smokePart = smoke.screenshot_ok === true
                ? ` smoke=ok(e=${smoke.element_count ?? 0},w=${smoke.window_count ?? 0},${smoke.ms ?? '?'}ms)`
                : (smoke.screenshot_ok === false ? ` smoke=fail(${smoke.error || 'shot'})` : '');
            const e2e = rep?.last_e2e || {};
            let e2ePart = '';
            if (e2e && (e2e.ok === true || e2e.ok === false)) {
                e2ePart = ` e2e=${e2e.ok ? 'ok' : (e2e.soft_fail ? 'soft_fail' : 'fail')}`;
                if (e2e.interact) e2ePart += '+';
                if (e2e.skip_reason) e2ePart += `/${e2e.skip_reason}`;
                if (e2e.token_found === true) e2ePart += '/tok';
                if (e2e.token_found === false || e2e.token_unconfirmed) e2ePart += '/notok';
                if (e2e.ms != null) e2ePart += `(${e2e.ms}ms)`;
            }
            const pathPart = rep?.diagnostics_path
                ? ` diag=${String(rep.diagnostics_path).split(/[/\\\\]/).pop()}`
                : '';
            const csvPart = rep?.history_csv_path
                ? ` csv=${String(rep.history_csv_path).split(/[/\\\\]/).pop()}`
                : '';
            setCuCheckReport(
                `ok=${rep?.ok} backend=${backend || 'n/a'} windows=${wins ?? '?'}${yoloPart}${ocrPart}${permPart}${smokePart}${e2ePart}${pathPart}${csvPart}${readyPart}${warns} ms=${rep?.ms ?? '?'}`
            );
            setCuDiagPath(rep?.diagnostics_path || rep?.last_e2e?.diagnostics_path || '');
            setCuCsvPath(rep?.history_csv_path || rep?.last_e2e?.history_csv_path || '');
            await refreshCuArtifacts();
            const st: any = await GetComputerUseStatus();
            if (st) {
                const b = st.uia_sidecar_backend ? ` · a11y=${st.uia_sidecar_backend}` : '';
                setCuStatus(
                    st.session_active
                        ? `active · steps=${st.step_count ?? 0} · elements=${st.element_count ?? 0}${b}`
                        : `idle${b}`
                );
            }
        } catch (e: any) {
            setCuError(e?.message || String(e));
            setCuCheckReport('');
        } finally {
            setCuChecking(false);
        }
    };

    if (loading) return <div className="model-config-loading">{t('Loading...', '加载中...', '加載中...')}</div>;

    return (
        <div className="model-config-panel">
            {/* ===== Section 0: Computer Use ===== */}
            <h4 className="model-config-heading model-config-heading--primary">
                {t('Computer Use (Desktop Control)', 'Computer Use（桌面操控）', 'Computer Use（桌面操控）')}
            </h4>
            <div className="model-config-toggle-row">
                <label className="model-config-check">
                    <input type='checkbox' checked={cuEnabled} onChange={e => handleCuToggle(e.target.checked)} />
                    {t('Enable Computer Use', '启用 Computer Use', '啟用 Computer Use')}
                </label>
            </div>
            <p className="model-config-copy">
                {t(
                    'Lets the agent operate native desktop apps via local OmniParser + OCR (text-only models supported). Screenshots stay local; the model receives eN element lists, not images. Use @computer or ask to operate the desktop.',
                    '让 Agent 通过本地 OmniParser + OCR 操作本机桌面应用（纯文本模型也可用）。截图仅在本地解析，模型收到 eN 元素列表而非图片。可用 @computer 或描述桌面操作任务触发。',
                    '讓 Agent 透過本地 OmniParser + OCR 操作本機桌面應用（純文字模型也可用）。截圖僅在本地解析，模型收到 eN 元素列表而非圖片。可用 @computer 或描述桌面操作任務觸發。'
                )}
            </p>
            {cuStatus && <p className="model-config-copy" style={{ opacity: 0.85 }}>{cuStatus}</p>}
            <div className="model-config-toggle-row" style={{ gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => { void handleCuSelfCheck(); }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: cuChecking ? 'wait' : 'pointer' }}
                >
                    {cuChecking
                        ? t('Checking…', '自检中…', '自檢中…')
                        : t('Run self-check', '运行自检', '執行自檢')}
                </button>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => {
                        void OpenComputerUsePermissionSettingsDefault().catch((e: any) => {
                            setCuError(e?.message || String(e));
                        });
                    }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: 'pointer' }}
                    title={t(
                        'Open system privacy settings (Accessibility / Screen Recording on macOS)',
                        '打开系统隐私设置（macOS：辅助功能 / 屏幕录制）',
                        '打開系統隱私設定（macOS：輔助功能 / 螢幕錄製）'
                    )}
                >
                    {t('Open privacy settings', '打开隐私设置', '打開隱私設定')}
                </button>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => {
                        void (async () => {
                            setCuChecking(true);
                            setCuError('');
                            setCuActionMsg(t('Exporting…', '导出中…', '匯出中…'));
                            try {
                                const exp: any = await ExportComputerUseDiagnostics();
                                if (exp?.ok) {
                                    setCuActionMsg(t(`Exported: ${exp.path}`, `已导出: ${exp.path}`, `已匯出: ${exp.path}`));
                                } else {
                                    setCuError(exp?.error || t('Export failed', '导出失败', '匯出失敗'));
                                    setCuActionMsg('');
                                }
                            } catch (e: any) {
                                setCuError(e?.message || String(e));
                                setCuActionMsg('');
                            } finally {
                                setCuChecking(false);
                            }
                        })();
                    }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: cuChecking ? 'wait' : 'pointer' }}
                    title={t('Export diagnostics JSON (no screenshots)', '导出诊断 JSON（不含截图）', '匯出診斷 JSON（不含截圖）')}
                >
                    {t('Export diagnostics', '导出诊断', '匯出診斷')}
                </button>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => {
                        void (async () => {
                            setCuChecking(true);
                            setCuError('');
                            setCuActionMsg(t('Exporting CSV…', '导出 CSV…', '匯出 CSV…'));
                            try {
                                const exp: any = await ExportComputerUseObserveHistoryCSV();
                                if (exp?.ok) {
                                    setCuActionMsg(
                                        t(
                                            `History CSV: ${exp.path} (${exp.rows ?? 0} rows)`,
                                            `历史 CSV: ${exp.path}（${exp.rows ?? 0} 行）`,
                                            `歷史 CSV: ${exp.path}（${exp.rows ?? 0} 行）`
                                        )
                                    );
                                } else {
                                    setCuError(exp?.error || t('CSV export failed', 'CSV 导出失败', 'CSV 匯出失敗'));
                                    setCuActionMsg('');
                                }
                            } catch (e: any) {
                                setCuError(e?.message || String(e));
                                setCuActionMsg('');
                            } finally {
                                setCuChecking(false);
                            }
                        })();
                    }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: cuChecking ? 'wait' : 'pointer' }}
                    title={t('Export observe timing history as CSV', '导出 observe 耗时历史为 CSV', '匯出 observe 耗時歷史為 CSV')}
                >
                    {t('Export history CSV', '导出历史 CSV', '匯出歷史 CSV')}
                </button>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => {
                        void (async () => {
                            setCuError('');
                            try {
                                const r: any = await OpenComputerUseLastDiagnostics();
                                if (r?.ok) {
                                    setCuActionMsg(t(`Opened: ${r.path}`, `已打开: ${r.path}`, `已打開: ${r.path}`));
                                } else {
                                    const logs: any = await OpenComputerUseLogsFolder();
                                    if (logs?.ok) {
                                        setCuActionMsg(t(`Logs: ${logs.path}`, `日志目录: ${logs.path}`, `日誌目錄: ${logs.path}`));
                                    } else {
                                        setCuError(r?.error || logs?.error || t('Open failed', '打开失败', '打開失敗'));
                                    }
                                }
                            } catch (e: any) {
                                setCuError(e?.message || String(e));
                            }
                        })();
                    }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: 'pointer' }}
                    title={t('Open last diagnostics or logs folder', '打开最近诊断文件或日志目录', '打開最近診斷檔案或日誌目錄')}
                >
                    {t('Open diagnostics', '打开诊断', '打開診斷')}
                </button>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => {
                        void (async () => {
                            setCuChecking(true);
                            setCuError('');
                            setCuActionMsg(t('Running E2E…', 'E2E 中…', 'E2E 中…'));
                            try {
                                const e2e: any = await ComputerUseE2ESmoke();
                                if (e2e?.diagnostics_path) setCuDiagPath(String(e2e.diagnostics_path));
                                if (e2e?.history_csv_path) setCuCsvPath(String(e2e.history_csv_path));
                                setCuActionMsg(
                                    e2e?.ok
                                        ? t(`E2E ok · ${e2e.ms ?? '?'}ms`, `E2E 通过 · ${e2e.ms ?? '?'}ms`, `E2E 通過 · ${e2e.ms ?? '?'}ms`)
                                        : t(`E2E failed: ${e2e?.error || 'unknown'}`, `E2E 失败: ${e2e?.error || '未知'}`, `E2E 失敗: ${e2e?.error || '未知'}`)
                                );
                                await refreshCuArtifacts();
                            } catch (e: any) {
                                setCuError(e?.message || String(e));
                                setCuActionMsg('');
                            } finally {
                                setCuChecking(false);
                            }
                        })();
                    }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: cuChecking ? 'wait' : 'pointer' }}
                    title={t('Smoke + launch editor + observe', '冒烟 + 启动编辑器 + 观察', '冒煙 + 啟動編輯器 + 觀察')}
                >
                    {t('E2E smoke', 'E2E 冒烟', 'E2E 冒煙')}
                </button>
                <button
                    type="button"
                    disabled={cuChecking}
                    onClick={() => {
                        void (async () => {
                            setCuChecking(true);
                            setCuError('');
                            setCuActionMsg(t('Running E2E interact…', 'E2E 交互中…', 'E2E 交互中…'));
                            try {
                                const e2e: any = await ComputerUseE2EInteract();
                                if (e2e?.diagnostics_path) setCuDiagPath(String(e2e.diagnostics_path));
                                if (e2e?.history_csv_path) setCuCsvPath(String(e2e.history_csv_path));
                                const tok = e2e?.token ? ` token=${e2e.token}` : '';
                                const found =
                                    e2e?.token_found === true
                                        ? ' found'
                                        : e2e?.token_found === false || e2e?.token_unconfirmed
                                          ? ' not-found'
                                          : '';
                                const soft =
                                    e2e?.soft_fail
                                        ? ` soft_fail${e2e.skip_reason ? `=${e2e.skip_reason}` : ''}`
                                        : '';
                                setCuActionMsg(
                                    e2e?.ok
                                        ? t(
                                              `E2E interact ok · ${e2e.ms ?? '?'}ms${tok}${found}${soft}`,
                                              `E2E 交互通过 · ${e2e.ms ?? '?'}ms${tok}${found}${soft}`,
                                              `E2E 交互通過 · ${e2e.ms ?? '?'}ms${tok}${found}${soft}`
                                          )
                                        : t(
                                              `E2E interact failed: ${e2e?.error || e2e?.skip_reason || 'unknown'}${soft}`,
                                              `E2E 交互失败: ${e2e?.error || e2e?.skip_reason || '未知'}${soft}`,
                                              `E2E 交互失敗: ${e2e?.error || e2e?.skip_reason || '未知'}${soft}`
                                          )
                                );
                                await refreshCuArtifacts();
                            } catch (e: any) {
                                setCuError(e?.message || String(e));
                                setCuActionMsg('');
                            } finally {
                                setCuChecking(false);
                            }
                        })();
                    }}
                    className="model-config-btn"
                    style={{ padding: '4px 10px', cursor: cuChecking ? 'wait' : 'pointer' }}
                    title={t(
                        'E2E with type into Notepad/TextEdit (moves real focus/cursor briefly)',
                        'E2E 并在记事本/TextEdit 中输入（会短暂移动焦点/光标）',
                        'E2E 並在記事本/TextEdit 中輸入（會短暫移動焦點/游標）'
                    )}
                >
                    {t('E2E interact', 'E2E 交互', 'E2E 交互')}
                </button>
            </div>
            {cuCheckReport && <p className="model-config-copy" style={{ opacity: 0.85 }}>{cuCheckReport}</p>}
            {(cuDiagPath || cuCsvPath || cuArtifactCount > 0) && (
                <div className="model-config-toggle-row" style={{ gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                    {cuDiagPath ? (
                        <button
                            type="button"
                            className="model-config-btn"
                            style={{ padding: '2px 8px', fontSize: 12 }}
                            onClick={() => {
                                void CopyComputerUsePath('diagnostics').then((r: any) => {
                                    setCuActionMsg(
                                        r?.ok
                                            ? t('Diagnostics path copied', '已复制诊断路径', '已複製診斷路徑')
                                            : (r?.error || t('Copy failed', '复制失败', '複製失敗'))
                                    );
                                });
                            }}
                        >
                            {t('Copy diag path', '复制诊断路径', '複製診斷路徑')}
                        </button>
                    ) : null}
                    {cuCsvPath ? (
                        <button
                            type="button"
                            className="model-config-btn"
                            style={{ padding: '2px 8px', fontSize: 12 }}
                            onClick={() => {
                                void CopyComputerUsePath('csv').then((r: any) => {
                                    setCuActionMsg(
                                        r?.ok
                                            ? t('CSV path copied', '已复制 CSV 路径', '已複製 CSV 路徑')
                                            : (r?.error || t('Copy failed', '复制失败', '複製失敗'))
                                    );
                                });
                            }}
                        >
                            {t('Copy CSV path', '复制 CSV 路径', '複製 CSV 路徑')}
                        </button>
                    ) : null}
                    <button
                        type="button"
                        className="model-config-btn"
                        style={{ padding: '2px 8px', fontSize: 12 }}
                        onClick={() => {
                            setCuShowArtifacts((v) => !v);
                            if (!cuShowArtifacts) void refreshCuArtifacts(cuArtifactFilter);
                        }}
                    >
                        {cuShowArtifacts
                            ? t('Hide file list', '隐藏文件列表', '隱藏檔案列表')
                            : t(
                                `Show files${cuArtifactCount ? ` (${cuArtifactCount})` : ''}`,
                                `显示文件${cuArtifactCount ? `（${cuArtifactCount}）` : ''}`,
                                `顯示檔案${cuArtifactCount ? `（${cuArtifactCount}）` : ''}`
                            )}
                    </button>
                    <button
                        type="button"
                        className="model-config-btn"
                        style={{ padding: '2px 8px', fontSize: 12 }}
                        disabled={cuChecking}
                        title={t(
                            `Keep ${cuKeepNewest} newest each; max age ${cuMaxAgeDays || '∞'} days`,
                            `各保留最新 ${cuKeepNewest} 个；最长 ${cuMaxAgeDays || '不限'} 天`,
                            `各保留最新 ${cuKeepNewest} 個；最長 ${cuMaxAgeDays || '不限'} 天`
                        )}
                        onClick={async () => {
                            const ageLabel = cuMaxAgeDays > 0
                                ? t(`and older than ${cuMaxAgeDays} days`, `以及超过 ${cuMaxAgeDays} 天`, `以及超過 ${cuMaxAgeDays} 天`)
                                : t('(age unlimited)', '（不限天数）', '（不限天數）');
                            const ok = await showConfirm(
                                t(
                                    `Delete Computer Use log files beyond the newest ${cuKeepNewest} per kind ${ageLabel}? This cannot be undone.`,
                                    `将删除各类型超出最新 ${cuKeepNewest} 个 ${ageLabel} 的 Computer Use 日志，不可恢复。确定？`,
                                    `將刪除各類型超出最新 ${cuKeepNewest} 個 ${ageLabel} 的 Computer Use 日誌，不可恢復。確定？`
                                )
                            , t('Clean up logs', '清理日志', '清理日誌'), { confirmText: t('Delete', '删除', '刪除'), cancelText: t('Cancel', '取消', '取消'), confirmVariant: 'danger' });
                            if (!ok) return;
                            void (async () => {
                                setCuChecking(true);
                                try {
                                    const r: any = await PruneComputerUseLogArtifacts(cuKeepNewest, cuMaxAgeDays);
                                    if (r?.ok) {
                                        const errN = Number(r.remove_error_n ?? 0);
                                        setCuActionMsg(
                                            t(
                                                `Pruned ${r.deleted_n ?? 0} file(s), freed ${r.freed_bytes ?? 0} bytes` +
                                                    (errN ? ` (${errN} remove errors)` : ''),
                                                `已清理 ${r.deleted_n ?? 0} 个文件，释放 ${r.freed_bytes ?? 0} 字节` +
                                                    (errN ? `（${errN} 删除失败）` : ''),
                                                `已清理 ${r.deleted_n ?? 0} 個檔案，釋放 ${r.freed_bytes ?? 0} 位元組` +
                                                    (errN ? `（${errN} 刪除失敗）` : '')
                                            )
                                        );
                                        if (errN && r.error) setCuError(String(r.error));
                                        await refreshCuArtifacts(cuArtifactFilter);
                                    } else {
                                        setCuError(r?.error || t('Prune failed', '清理失败', '清理失敗'));
                                    }
                                } catch (e: any) {
                                    setCuError(e?.message || String(e));
                                } finally {
                                    setCuChecking(false);
                                }
                            })();
                        }}
                    >
                        {t(
                            `Prune old logs${cuArtifactCount ? ` (${cuArtifactCount})` : ''}`,
                            `清理旧日志${cuArtifactCount ? `（${cuArtifactCount}）` : ''}`,
                            `清理舊日誌${cuArtifactCount ? `（${cuArtifactCount}）` : ''}`
                        )}
                    </button>
                </div>
            )}
            <div className="model-config-toggle-row" style={{ gap: 10, flexWrap: 'wrap', alignItems: 'center', marginTop: 4 }}>
                <label className="model-config-copy" style={{ display: 'flex', alignItems: 'center', gap: 4, margin: 0 }}>
                    {t('Keep newest', '保留最新', '保留最新')}
                    <input
                        type="number"
                        min={1}
                        max={100}
                        value={cuKeepNewest}
                        onChange={(e) => setCuKeepNewest(Math.max(1, Math.min(100, Number(e.target.value) || 10)))}
                        style={{ width: 56 }}
                    />
                </label>
                <label className="model-config-copy" style={{ display: 'flex', alignItems: 'center', gap: 4, margin: 0 }}>
                    {t('Max age days (0=off)', '最长天数（0=不限）', '最長天數（0=不限）')}
                    <input
                        type="number"
                        min={0}
                        max={3650}
                        value={cuMaxAgeDays}
                        onChange={(e) => setCuMaxAgeDays(Math.max(0, Math.min(3650, Number(e.target.value) || 0)))}
                        style={{ width: 56 }}
                    />
                </label>
                <label className="model-config-check" style={{ margin: 0 }}>
                    <input
                        type="checkbox"
                        checked={cuAutoPrune}
                        onChange={(e) => {
                            const on = e.target.checked;
                            setCuAutoPrune(on);
                            void SetComputerUseLogAutoPrune(on).then((r: any) => {
                                if (!r?.ok) {
                                    setCuAutoPrune(!on);
                                    setCuError(r?.error || t('Save failed', '保存失败', '保存失敗'));
                                } else {
                                    setCuActionMsg(
                                        on
                                            ? t('Auto-prune on startup enabled', '已开启启动时自动清理', '已開啟啟動時自動清理')
                                            : t('Auto-prune on startup disabled', '已关闭启动时自动清理', '已關閉啟動時自動清理')
                                    );
                                }
                            });
                        }}
                    />
                    {t('Auto-prune on startup', '启动时自动清理', '啟動時自動清理')}
                </label>
                <button
                    type="button"
                    className="model-config-btn"
                    style={{ padding: '2px 8px', fontSize: 12 }}
                    disabled={cuChecking}
                    onClick={() => {
                        void (async () => {
                            setCuChecking(true);
                            setCuError('');
                            try {
                                // autoPrune 0 = leave unchanged (checkbox already persists)
                                const r: any = await SetComputerUseLogPrunePolicy(cuKeepNewest, cuMaxAgeDays, 0);
                                if (r?.ok) {
                                    setCuActionMsg(
                                        t(
                                            `Policy saved: keep=${r.keep_newest}, age=${r.max_age_days}d, auto=${r.auto_prune ? 'on' : 'off'}`,
                                            `策略已保存：保留=${r.keep_newest}，天数=${r.max_age_days}，自动=${r.auto_prune ? '开' : '关'}`,
                                            `策略已保存：保留=${r.keep_newest}，天數=${r.max_age_days}，自動=${r.auto_prune ? '開' : '關'}`
                                        )
                                    );
                                } else {
                                    setCuError(r?.error || t('Save failed', '保存失败', '保存失敗'));
                                }
                            } catch (e: any) {
                                setCuError(e?.message || String(e));
                            } finally {
                                setCuChecking(false);
                            }
                        })();
                    }}
                >
                    {t('Save policy', '保存策略', '保存策略')}
                </button>
            </div>
            {cuShowArtifacts && (
                <div
                    className="model-config-copy"
                    style={{
                        maxHeight: 200,
                        overflow: 'auto',
                        border: '1px solid var(--theme-border, rgba(127,127,127,0.35))',
                        borderRadius: 8,
                        padding: 8,
                        fontSize: 12,
                        marginTop: 6,
                    }}
                >
                    <div style={{ display: 'flex', gap: 6, marginBottom: 8, flexWrap: 'wrap' }}>
                        {(['all', 'diag', 'csv'] as const).map((k) => (
                            <button
                                key={k}
                                type="button"
                                className="model-config-btn"
                                style={{
                                    padding: '1px 8px',
                                    fontSize: 11,
                                    opacity: cuArtifactFilter === k ? 1 : 0.7,
                                    fontWeight: cuArtifactFilter === k ? 600 : 400,
                                }}
                                onClick={() => {
                                    setCuArtifactFilter(k);
                                    void refreshCuArtifacts(k);
                                }}
                            >
                                {k === 'all'
                                    ? t('All', '全部', '全部')
                                    : k === 'diag'
                                      ? t('Diagnostics', '诊断', '診斷')
                                      : t('History CSV', '历史 CSV', '歷史 CSV')}
                            </button>
                        ))}
                    </div>
                    {cuArtifacts.length === 0 ? (
                        <div style={{ opacity: 0.7 }}>{t('No Computer Use log files yet.', '暂无 Computer Use 日志文件。', '暫無 Computer Use 日誌檔案。')}</div>
                    ) : (
                        <>
                            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginBottom: 6 }}>
                                <button
                                    type="button"
                                    className="model-config-btn"
                                    style={{ padding: '1px 6px', fontSize: 11 }}
                                    onClick={() => {
                                        const all: Record<string, boolean> = {};
                                        for (const it of cuArtifacts) {
                                            const path = String(it.path || '');
                                            if (path) all[path] = true;
                                        }
                                        setCuSelectedPaths(all);
                                    }}
                                >
                                    {t('Select all', '全选', '全選')}
                                </button>
                                <button
                                    type="button"
                                    className="model-config-btn"
                                    style={{ padding: '1px 6px', fontSize: 11 }}
                                    onClick={() => setCuSelectedPaths({})}
                                >
                                    {t('Clear selection', '清除选择', '清除選擇')}
                                </button>
                                <button
                                    type="button"
                                    className="model-config-btn"
                                    style={{ padding: '1px 6px', fontSize: 11, color: 'var(--theme-danger, #c44)' }}
                                    disabled={cuChecking || Object.keys(cuSelectedPaths).filter((k) => cuSelectedPaths[k]).length === 0}
                                    onClick={async () => {
                                        const paths = Object.keys(cuSelectedPaths).filter((k) => cuSelectedPaths[k]);
                                        if (!paths.length) return;
                                        const ok = await showConfirm(
                                            t(
                                                `Delete ${paths.length} selected Computer Use log file(s)? This cannot be undone.`,
                                                `删除选中的 ${paths.length} 个 Computer Use 日志？不可恢复。`,
                                                `刪除選中的 ${paths.length} 個 Computer Use 日誌？不可恢復。`
                                            )
                                        , t('Delete selected logs', '删除选中日志', '刪除選取日誌'), { confirmText: t('Delete', '删除', '刪除'), cancelText: t('Cancel', '取消', '取消'), confirmVariant: 'danger' });
                                        if (!ok) return;
                                        void (async () => {
                                            setCuChecking(true);
                                            try {
                                                const r: any = await BatchDeleteComputerUseLogArtifacts(paths);
                                                if (r?.ok) {
                                                    setCuActionMsg(
                                                        t(
                                                            `Deleted ${r.deleted_n ?? paths.length} file(s), freed ${r.freed_bytes ?? 0} bytes` +
                                                                (r.error_n ? ` (${r.error_n} failed)` : ''),
                                                            `已删除 ${r.deleted_n ?? paths.length} 个文件，释放 ${r.freed_bytes ?? 0} 字节` +
                                                                (r.error_n ? `（${r.error_n} 失败）` : ''),
                                                            `已刪除 ${r.deleted_n ?? paths.length} 個檔案，釋放 ${r.freed_bytes ?? 0} 位元組` +
                                                                (r.error_n ? `（${r.error_n} 失敗）` : '')
                                                        )
                                                    );
                                                    if (r.error_n) setCuError(String(r.error || ''));
                                                    setCuSelectedPaths({});
                                                    await refreshCuArtifacts(cuArtifactFilter);
                                                } else {
                                                    setCuError(r?.error || t('Batch delete failed', '批量删除失败', '批量刪除失敗'));
                                                }
                                            } catch (e: any) {
                                                setCuError(e?.message || String(e));
                                            } finally {
                                                setCuChecking(false);
                                            }
                                        })();
                                    }}
                                >
                                    {t(
                                        `Delete selected (${Object.keys(cuSelectedPaths).filter((k) => cuSelectedPaths[k]).length})`,
                                        `删除选中（${Object.keys(cuSelectedPaths).filter((k) => cuSelectedPaths[k]).length}）`,
                                        `刪除選中（${Object.keys(cuSelectedPaths).filter((k) => cuSelectedPaths[k]).length}）`
                                    )}
                                </button>
                            </div>
                            {cuArtifacts.map((it) => {
                                const path = String(it.path || '');
                                const selected = !!(path && cuSelectedPaths[path]);
                                return (
                                    <div
                                        key={path || it.name}
                                        style={{
                                            display: 'flex',
                                            gap: 8,
                                            alignItems: 'center',
                                            flexWrap: 'wrap',
                                            padding: '3px 0',
                                            borderBottom: '1px solid rgba(127,127,127,0.15)',
                                        }}
                                    >
                                        <label style={{ display: 'flex', alignItems: 'center', margin: 0 }}>
                                            <input
                                                type="checkbox"
                                                checked={selected}
                                                disabled={!path}
                                                onChange={(e) => {
                                                    if (!path) return;
                                                    setCuSelectedPaths((prev) => {
                                                        const next = { ...prev };
                                                        if (e.target.checked) next[path] = true;
                                                        else delete next[path];
                                                        return next;
                                                    });
                                                }}
                                            />
                                        </label>
                                        <span style={{ opacity: 0.7, minWidth: 36 }}>{it.kind}</span>
                                        <button
                                            type="button"
                                            className="model-config-btn"
                                            style={{ padding: '1px 6px', fontSize: 11 }}
                                            title={path}
                                            onClick={() => {
                                                void OpenComputerUseLogArtifact(path).then((r: any) => {
                                                    if (!r?.ok) setCuError(r?.error || t('Open failed', '打开失败', '打開失敗'));
                                                });
                                            }}
                                        >
                                            {it.name}
                                        </button>
                                        <span style={{ opacity: 0.65 }}>
                                            {typeof it.size === 'number' ? `${Math.max(1, Math.round(it.size / 1024))}KB` : ''}
                                            {it.mod_time ? ` · ${String(it.mod_time).replace('T', ' ').slice(0, 16)}` : ''}
                                        </span>
                                        <button
                                            type="button"
                                            className="model-config-btn"
                                            style={{ padding: '1px 6px', fontSize: 11 }}
                                            onClick={() => {
                                                void navigator.clipboard?.writeText(path).then(
                                                    () => setCuActionMsg(t('Path copied', '路径已复制', '路徑已複製')),
                                                    () => {
                                                        void CopyComputerUsePath(it.kind === 'csv' ? 'csv' : 'diagnostics').then(() =>
                                                            setCuActionMsg(t('Path copied', '路径已复制', '路徑已複製'))
                                                        );
                                                    }
                                                );
                                            }}
                                        >
                                            {t('Copy', '复制', '複製')}
                                        </button>
                                        <button
                                            type="button"
                                            className="model-config-btn"
                                            style={{ padding: '1px 6px', fontSize: 11, color: 'var(--theme-danger, #c44)' }}
                                            onClick={async () => {
                                                const name = it.name || path || '';
                                                const ok = await showConfirm(
                                                    t(
                                                        `Delete ${name}? This cannot be undone.`,
                                                        `删除 ${name}？不可恢复。`,
                                                        `刪除 ${name}？不可恢復。`
                                                    )
                                                , t('Delete log', '删除日志', '刪除日誌'), { confirmText: t('Delete', '删除', '刪除'), cancelText: t('Cancel', '取消', '取消'), confirmVariant: 'danger' });
                                                if (!ok) return;
                                                void DeleteComputerUseLogArtifact(path).then(async (r: any) => {
                                                    if (r?.ok) {
                                                        setCuActionMsg(
                                                            t(
                                                                `Deleted ${name}`,
                                                                `已删除 ${name}`,
                                                                `已刪除 ${name}`
                                                            )
                                                        );
                                                        await refreshCuArtifacts(cuArtifactFilter);
                                                    } else {
                                                        setCuError(r?.error || t('Delete failed', '删除失败', '刪除失敗'));
                                                    }
                                                });
                                            }}
                                        >
                                            {t('Delete', '删除', '刪除')}
                                        </button>
                                    </div>
                                );
                            })}
                        </>
                    )}
                </div>
            )}
            {cuActionMsg && <p className="model-config-copy" style={{ opacity: 0.85 }}>{cuActionMsg}</p>}
            {cuError && <p className="model-config-error">{cuError}</p>}

            {/* ===== Section 1: OminiParser-V2 (Screen Parsing) ===== */}
            <h4 className="model-config-heading model-config-heading--warning model-config-heading--spaced">
                {t('OminiParser-V2 (Screen Parsing)', 'OminiParser-V2（屏幕解析）', 'OminiParser-V2（螢幕解析）')}
            </h4>
            <div className="model-config-toggle-row">
                <label className="model-config-check">
                    <input type='checkbox' checked={spEnabled} onChange={e => handleSpToggle(e.target.checked)} />
                    {t('Enable Screen Parsing', '启用屏幕解析', '啟用螢幕解析')}
                </label>
            </div>
            <p className="model-config-copy">
                {t(
                    'OminiParser-V2 uses a YOLO-based model to detect and parse UI elements on screen, enabling vision-based interaction. Model file ~77MB, downloaded from Hub.',
                    'OminiParser-V2 使用 YOLO 模型检测和解析屏幕上的 UI 元素，实现基于视觉的界面交互。模型文件约 77MB，将从 Hub 下载到本地。',
                    'OminiParser-V2 使用 YOLO 模型檢測和解析螢幕上的 UI 元素，實現基於視覺的界面交互。模型文件約 77MB，將從 Hub 下載到本地。'
                )}
            </p>
            {spEnabled && (
                <ModelStatusBox
                    exists={spModelExists} downloading={spDownloading} size={spModelSize}
                    progress={spProgress} downloaded={spDownloaded} total={spTotal}
                    error={spError} onDownload={startSpDownload} onRetry={startSpDownload}
                    accentColor="var(--theme-primary, #2f5f98)" t={t}
                />
            )}

            {/* ===== Section 2: Embedding Model (Vector Search) ===== */}
            <h4 className="model-config-heading model-config-heading--primary model-config-heading--spaced">
                {t('Embedding Model (Vector Search)', '嵌入模型（向量搜索）', '嵌入模型（向量搜索）')}
            </h4>
            <div className="model-config-toggle-row">
                <label className="model-config-check">
                    <input type='checkbox' checked={embEnabled} onChange={e => handleEmbToggle(e.target.checked)} />
                    {t('Enable Vector Search', '启用向量搜索', '啟用向量搜索')}
                </label>
            </div>
            <p className="model-config-copy">
                {t(
                    'Vector search uses EmbeddingGemma 300M model to generate semantic vectors for memory and documents, improving search accuracy. Model file ~300MB, downloaded from Hub.',
                    '向量搜索使用 EmbeddingGemma 300M 模型为记忆和文档生成语义向量，提升搜索精度。模型文件约 300MB，将从 Hub 下载到本地。',
                    '向量搜索使用 EmbeddingGemma 300M 模型為記憶和文檔生成語義向量，提升搜索精度。模型文件約 300MB，將從 Hub 下載到本地。'
                )}
            </p>
            {embEnabled && (
                <ModelStatusBox
                    exists={embModelExists} downloading={embDownloading} size={embModelSize}
                    progress={embProgress} downloaded={embDownloaded} total={embTotal}
                    error={embError} onDownload={startEmbDownload} onRetry={startEmbDownload}
                    accentColor="var(--theme-primary)" t={t}
                />
            )}
        </div>
    );
}
