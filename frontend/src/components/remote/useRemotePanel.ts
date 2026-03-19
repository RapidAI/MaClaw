import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import {
    ActivateRemote,
    CheckToolsStatus,
    ClearRemoteActivation,
    GetLastRemoteSmokeReport,
    GetRemoteActivationStatus,
    GetRemoteConnectionStatus,
    GetRemotePTYProbe,
    GetRemoteToolLaunchProbe,
    GetRemoteToolReadiness,
    InstallToolOnDemand,
    ListRemoteSessions,
    ListRemoteToolMetadata,
    ListValidProviders,
    ProbeRemoteHub,
    ReconnectRemoteHub,
    RunRemoteToolSmoke,
    SaveConfig,
    SendRemoteSessionInput,
    InterruptRemoteSession as InterruptRemoteSessionAPI,
    KillRemoteSession as KillRemoteSessionAPI,
    StartRemoteHandoffSession,
    StartRemoteSession,
    VerifyRemoteActivation,
} from "../../../wailsjs/go/main/App";
import { main } from "../../../wailsjs/go/models";
import {
    buildRemoteToolMetaByName,
    buildVisibleRemoteTools,
    getRemoteLaunchDetail as buildRemoteLaunchDetail,
    getRemoteReadinessDetail as buildRemoteReadinessDetail,
    getRemoteSmokeDetail as buildRemoteSmokeDetail,
    getRemoteSuggestedAction as buildRemoteSuggestedAction,
    getRemoteToolConfigHint as buildRemoteToolConfigHint,
    getRemoteToolLabel as buildRemoteToolLabel,
    getRemoteToolSmokeHint as buildRemoteToolSmokeHint,
    getSelectedRemoteToolBadges,
} from "./helpers";
import {
    TERMINAL_SESSION_STATUSES,
    type RemoteActivationStatus,
    type RemoteConnectionStatus,
    type RemoteSessionView,
    type RemoteSmokeReportView,
    type RemoteSuggestedAction,
    type RemoteToolLaunchProbeView,
    type RemoteToolMetadataView,
    type RemoteToolName,
    type RemoteToolReadinessView,
} from "./types";

type Translate = (key: string) => string;
type FormatText = (key: string, values?: Record<string, string>) => string;
type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;
type ShowToast = (message: string, duration?: number) => void;

type UseRemotePanelParams = {
    config: main.AppConfig | null;
    setConfig: (config: main.AppConfig) => void;
    setToolStatuses: (statuses: any[]) => void;
    getSelectedProjectForRemote: () => string;
    selectedProjectForLaunch: string;
    navTab: string;
    translate: Translate;
    formatText: FormatText;
    localizeText: LocalizeText;
    showToastMessage: ShowToast;
    onDemandInstallingTool: string;
    setOnDemandInstallingTool: (tool: string) => void;
};

export function useRemotePanel(params: UseRemotePanelParams) {
    const {
        config,
        setConfig,
        setToolStatuses,
        getSelectedProjectForRemote,
        selectedProjectForLaunch,
        navTab,
        translate,
        formatText,
        localizeText,
        showToastMessage,
        onDemandInstallingTool,
        setOnDemandInstallingTool,
    } = params;

    const [remoteActivationStatus, setRemoteActivationStatus] = useState<RemoteActivationStatus | null>(null);
    const [remoteConnectionStatus, setRemoteConnectionStatus] = useState<RemoteConnectionStatus | null>(null);
    const [remoteToolReadiness, setRemoteToolReadiness] = useState<RemoteToolReadinessView | null>(null);
    const [remotePTYProbe, setRemotePTYProbe] = useState<{ supported?: boolean; message?: string } | null>(null);
    const [remoteToolLaunchProbe, setRemoteToolLaunchProbe] = useState<RemoteToolLaunchProbeView | null>(null);
    const [remoteSmokeReport, setRemoteSmokeReport] = useState<RemoteSmokeReportView | null>(null);
    const [remoteSessions, setRemoteSessions] = useState<RemoteSessionView[]>([]);
    const [remoteToolMetadata, setRemoteToolMetadata] = useState<RemoteToolMetadataView[]>([]);
    const [remoteInputDrafts, setRemoteInputDrafts] = useState<Record<string, string>>({});
    const [remoteBusy, setRemoteBusy] = useState<string>("");
    const [selectedRemoteTool, setSelectedRemoteTool] = useState<RemoteToolName>("claude");
    const [invitationCodeRequired, setInvitationCodeRequired] = useState(false);
    const [invitationCode, setInvitationCodeRaw] = useState("");
    const [invitationCodeError, setInvitationCodeError] = useState("");
    const [providers, setProviders] = useState<Array<{name: string; model_id: string; is_default: boolean}>>([]);
    const [selectedProvider, setSelectedProvider] = useState<string>("");

    // Track session IDs that have been killed locally but may not yet be
    // reflected by the backend.  refreshRemotePanel will filter these out
    // so the optimistic removal isn't overwritten by a stale server response.
    const killedSessionIdsRef = useRef<Set<string>>(new Set());

    // Exponential backoff for server-side activation verification.
    // Starts at 30s, doubles on each consecutive failure (network error),
    // resets to 30s on success, caps at 5 minutes.
    const verifyNextAtRef = useRef<number>(0);
    const verifyIntervalRef = useRef<number>(30_000);

    const setInvitationCode = (code: string) => {
        setInvitationCodeRaw(code);
        if (invitationCodeError) setInvitationCodeError("");
    };

    const remoteToolMetaByName = useMemo(() => buildRemoteToolMetaByName(remoteToolMetadata), [remoteToolMetadata]);
    const visibleRemoteTools = useMemo(() => buildVisibleRemoteTools(remoteToolMetadata), [remoteToolMetadata]);
    const selectedRemoteToolInfo = remoteToolMetaByName[selectedRemoteTool];
    const selectedRemoteToolCanStart = selectedRemoteToolInfo?.can_start !== false;
    const selectedRemoteToolUnavailableReason = selectedRemoteToolInfo?.unavailable_reason || localizeText("cannot start", "无法启动", "無法啟動");

    const getRemoteToolLabel = (tool: string): string => buildRemoteToolLabel(tool, remoteToolMetaByName);
    const getRemoteToolConfigHint = (tool: string): string => buildRemoteToolConfigHint(tool, remoteToolMetaByName, localizeText);
    const getRemoteToolSmokeHint = (tool: string): string => buildRemoteToolSmokeHint(tool, remoteToolMetaByName, localizeText);
    const getRemoteReadinessDetail = (): string => buildRemoteReadinessDetail({
        selectedRemoteTool,
        remoteToolReadiness,
        remoteToolMetaByName,
        getSelectedProjectForRemote,
        translate,
    });
    const getRemoteLaunchDetail = (): string => buildRemoteLaunchDetail({
        selectedRemoteTool,
        remoteToolLaunchProbe,
        remoteToolMetaByName,
        formatText,
    });
    const getRemoteSmokeDetail = (): string => buildRemoteSmokeDetail({
        remoteSmokeReport,
        translate,
    });
    const selectedRemoteToolBadges = getSelectedRemoteToolBadges(selectedRemoteToolInfo, translate, localizeText);
    const remoteSuggestedAction: RemoteSuggestedAction | null = buildRemoteSuggestedAction({
        selectedRemoteTool,
        selectedRemoteToolInfo,
        selectedRemoteToolCanStart,
        remoteActivationStatus,
        remoteConnectionStatus,
        remoteToolReadiness,
        remoteToolMetaByName,
        formatText,
        translate,
        config,
    });

    const getUseProxy = (): boolean => !!config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.use_proxy;

    // Lightweight refresh: only fetches session list (used for high-frequency events)
    const refreshSessionsOnly = useCallback(async () => {
        try {
            const sessions = await ListRemoteSessions();
            const sessionList = Array.isArray(sessions) ? sessions : [];
            for (const id of killedSessionIdsRef.current) {
                const s = sessionList.find((sess: RemoteSessionView) => sess.id === id);
                if (!s || TERMINAL_SESSION_STATUSES.has(String(s.status || s.summary?.status || "").toLowerCase())) {
                    killedSessionIdsRef.current.delete(id);
                }
            }
            setRemoteSessions(
                sessionList.filter((sess: RemoteSessionView) => {
                    // Only filter out sessions that were killed locally (optimistic removal)
                    if (killedSessionIdsRef.current.has(sess.id)) return false;
                    return true;
                })
            );
        } catch (err) {
            console.error("Failed to refresh sessions:", err);
        }
    }, []);

    const refreshRemotePanel = async () => {
        try {
            const [activation, connection, sessions, smokeSnapshot] = await Promise.all([
                GetRemoteActivationStatus(),
                GetRemoteConnectionStatus(),
                ListRemoteSessions(),
                GetLastRemoteSmokeReport(),
            ]);
            setRemoteActivationStatus(activation);
            setRemoteConnectionStatus(connection);
            const sessionList = Array.isArray(sessions) ? sessions : [];
            // Remove killed IDs from the tracking set once the backend confirms
            // they are truly inactive (or gone), so the set doesn't grow forever.
            for (const id of killedSessionIdsRef.current) {
                const s = sessionList.find((sess: RemoteSessionView) => sess.id === id);
                if (!s || TERMINAL_SESSION_STATUSES.has(String(s.status || s.summary?.status || "").toLowerCase())) {
                    killedSessionIdsRef.current.delete(id);
                }
            }
            // Filter out sessions that were killed locally but the backend
            // still reports as active (race condition).
            setRemoteSessions(
                sessionList.filter((sess: RemoteSessionView) => {
                    if (killedSessionIdsRef.current.has(sess.id)) return false;
                    return true;
                })
            );
            if (smokeSnapshot?.exists && smokeSnapshot?.report) {
                setRemoteSmokeReport(smokeSnapshot.report);
            }

            // Probe hub for invitation code requirement
            const hubURL = config?.remote_hub_url?.trim();
            const email = config?.remote_email?.trim();
            if (hubURL && email && !activation?.activated) {
                try {
                    const probeResult = await ProbeRemoteHub(hubURL, email);
                    setInvitationCodeRequired(!!probeResult?.invitation_code_required);
                } catch {
                    // Probe failure is non-critical; don't block the panel
                }
            }

            // Verify activation with server: if the user was unbound or blocked
            // on the server side, clear local state so the UI reflects reality.
            // Uses exponential backoff: 30s → 60s → 120s → ... → 5min cap on
            // consecutive failures; resets to 30s on success.
            if (activation?.activated && Date.now() >= verifyNextAtRef.current) {
                try {
                    const stillValid = await VerifyRemoteActivation();
                    if (!stillValid) {
                        // Local state was cleared by the backend; re-fetch activation status
                        const freshActivation = await GetRemoteActivationStatus();
                        setRemoteActivationStatus(freshActivation);
                    }
                    // Success — reset interval to base
                    verifyIntervalRef.current = 30_000;
                } catch {
                    // Network/timeout — back off
                    verifyIntervalRef.current = Math.min(verifyIntervalRef.current * 2, 300_000);
                }
                verifyNextAtRef.current = Date.now() + verifyIntervalRef.current;
            }
        } catch (err) {
            console.error("Failed to refresh remote panel:", err);
        }
    };

    const refreshRemoteReadiness = async () => {
        const projectDir = getSelectedProjectForRemote();
        setRemoteBusy("readiness");
        try {
            const result = await GetRemoteToolReadiness(selectedRemoteTool, projectDir, getUseProxy());
            setRemoteToolReadiness(result);
        } catch (err) {
            console.error(`Failed to get remote ${selectedRemoteTool} readiness:`, err);
            showToastMessage(formatText("remoteReadinessFailed", { error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const refreshRemotePTYProbe = async () => {
        setRemoteBusy("pty");
        try {
            const result = await GetRemotePTYProbe();
            setRemotePTYProbe(result);
        } catch (err) {
            console.error("Failed to run ConPTY probe:", err);
            showToastMessage(formatText("remoteConptyFailed", { error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const refreshRemoteLaunchProbe = async () => {
        const projectDir = getSelectedProjectForRemote();
        setRemoteBusy("launch");
        try {
            const result = await GetRemoteToolLaunchProbe(selectedRemoteTool, projectDir, getUseProxy());
            setRemoteToolLaunchProbe(result);
        } catch (err) {
            console.error(`Failed to run ${selectedRemoteTool} launch probe:`, err);
            showToastMessage(formatText("remoteLaunchProbeFailedToast", { tool: getRemoteToolLabel(selectedRemoteTool), error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const runRemoteSmoke = async () => {
        const projectDir = getSelectedProjectForRemote();
        setRemoteBusy("smoke");
        try {
            const result = await RunRemoteToolSmoke(selectedRemoteTool, projectDir, getUseProxy());
            setRemoteSmokeReport(result);
            await refreshRemotePanel();
            showToastMessage(formatText("remoteSmokeCompleted", { tool: getRemoteToolLabel(selectedRemoteTool) }), 3000);
        } catch (err) {
            console.error(`Failed to run remote ${selectedRemoteTool} smoke:`, err);
            showToastMessage(formatText("remoteSmokeFailed", { tool: getRemoteToolLabel(selectedRemoteTool), error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const activateRemoteWithEmail = async () => {
        if (!config?.remote_email?.trim()) {
            showToastMessage(translate("remoteEmailRequired"), 3000);
            return false;
        }
        setRemoteBusy("activate");
        setInvitationCodeError("");
        try {
            await ActivateRemote(config.remote_email.trim(), invitationCode.trim(), (config as any).remote_mobile?.trim() || "");
            await refreshRemotePanel();
            showToastMessage(translate("remoteActivationCompleted"), 3000);
            return true;
        } catch (err) {
            const errMsg = String(err);
            if (errMsg.includes("INVITATION_CODE_REQUIRED")) {
                setInvitationCodeRequired(true);
                showToastMessage("请输入邀请码后重试", 3000);
            } else if (errMsg.includes("INVALID_INVITATION_CODE")) {
                setInvitationCodeRequired(true);
                setInvitationCodeError("邀请码无效或已被使用");
            } else if (errMsg.includes("INVITATION_EXPIRED")) {
                setInvitationCodeRequired(true);
                let expiredMsg = "用户已失效，请使用新的邀请码重新绑定";
                const expiresAtMatch = errMsg.match(/expires_at[:\s]*(\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}[^\s"]*)/i);
                if (expiresAtMatch) {
                    const expiresDate = new Date(expiresAtMatch[1]);
                    if (!isNaN(expiresDate.getTime())) {
                        expiredMsg += `（过期时间：${expiresDate.toLocaleDateString()}）`;
                    }
                }
                setInvitationCodeError(expiredMsg);
                showToastMessage(expiredMsg, 4000);
            } else {
                console.error("Failed to activate remote:", err);
                showToastMessage(formatText("remoteActivationFailed", { error: errMsg }), 4000);
            }
            return false;
        } finally {
            setRemoteBusy("");
        }
    };

    const reconnectRemote = async () => {
        setRemoteBusy("reconnect");
        try {
            await ReconnectRemoteHub();
            await refreshRemotePanel();
            showToastMessage(translate("remoteReconnectHub"), 3000);
        } catch (err) {
            console.error("Failed to reconnect remote hub:", err);
            showToastMessage(formatText("remoteReconnectFailed", { error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const startRemoteSession = async () => {
        const projectDir = getSelectedProjectForRemote();
        if (!projectDir) {
            showToastMessage(translate("remoteSelectProjectFirst"), 3000);
            return;
        }
        if (!selectedRemoteToolCanStart) {
            showToastMessage(formatText("remoteUnavailable", { reason: `${getRemoteToolLabel(selectedRemoteTool)} ${selectedRemoteToolUnavailableReason}` }), 4000);
            return;
        }
        setRemoteBusy("start-session");
        try {
            await StartRemoteSession(selectedRemoteTool, projectDir, getUseProxy(), selectedProvider, "desktop");
            await refreshRemotePanel();
            showToastMessage(formatText("remoteStartTool", { tool: getRemoteToolLabel(selectedRemoteTool) }), 3000);
        } catch (err) {
            console.error(`Failed to start remote ${selectedRemoteTool} session:`, err);
            showToastMessage(formatText("remoteStartFailed", { error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const quickStartRemoteSession = async (tool: RemoteToolName, launchSource: "desktop" | "handoff" = "desktop") => {
        const projectDir = getSelectedProjectForRemote();
        if (!projectDir) {
            showToastMessage(translate("remoteSelectProjectFirst"), 3000);
            return;
        }
        if (!config?.remote_hub_url?.trim()) {
            showToastMessage(translate("remoteServerRequired"), 3000);
            return;
        }
        if (!config?.remote_email?.trim()) {
            showToastMessage(translate("remoteEmailRequired"), 3000);
            return;
        }
        if (!remoteActivationStatus?.activated) {
            showToastMessage(translate("remoteActivateFirst"), 3000);
            return;
        }

        setSelectedRemoteTool(tool);
        setRemoteBusy("quick-start");
        try {
            if (launchSource === "handoff") {
                await StartRemoteHandoffSession(tool, projectDir, getUseProxy(), selectedProvider, "handoff");
            } else {
                await StartRemoteSession(tool, projectDir, getUseProxy(), selectedProvider, "desktop");
            }
            await refreshRemotePanel();
            showToastMessage(formatText("remoteStartTool", { tool: getRemoteToolLabel(tool) }), 3000);
        } catch (err) {
            console.error(`Failed to quick start remote ${tool} session:`, err);
            showToastMessage(formatText("remoteStartFailed", { error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    const installSelectedRemoteTool = async () => {
        if (selectedRemoteToolInfo?.installed) {
            showToastMessage(localizeText(
                `${getRemoteToolLabel(selectedRemoteTool)} is already installed`,
                `${getRemoteToolLabel(selectedRemoteTool)} 已安装，无需重复安装`,
                `${getRemoteToolLabel(selectedRemoteTool)} 已安裝，無需重複安裝`,
            ), 2500);
            return;
        }
        setOnDemandInstallingTool(selectedRemoteTool);
        setRemoteBusy("install-remote-tool");
        try {
            await InstallToolOnDemand(selectedRemoteTool);
            const [updatedStatuses, metadata] = await Promise.all([
                CheckToolsStatus(),
                ListRemoteToolMetadata(),
            ]);
            setToolStatuses(updatedStatuses);
            setRemoteToolMetadata(metadata || []);
            await refreshRemoteReadiness();
            showToastMessage(localizeText(
                `${getRemoteToolLabel(selectedRemoteTool)} installed`,
                `${getRemoteToolLabel(selectedRemoteTool)} 已安装`,
                `${getRemoteToolLabel(selectedRemoteTool)} 已安裝`,
            ), 3000);
        } catch (err) {
            console.error(`Failed to install remote tool ${selectedRemoteTool}:`, err);
            showToastMessage(formatText("remoteInstallFailed", { error: String(err) }), 4000);
        } finally {
            setOnDemandInstallingTool("");
            setRemoteBusy("");
        }
    };

    const saveRemoteConfigField = async (patch: Partial<main.AppConfig>) => {
        if (!config) return;
        const newConfig = new main.AppConfig({ ...config, ...patch });
        setConfig(newConfig);
        try {
            await SaveConfig(newConfig);
        } catch (err) {
            console.error("Failed to save remote config:", err);
            showToastMessage(formatText("remoteSaveFailed", { error: String(err) }), 4000);
        }
    };

    const sendRemoteInput = async (sessionID: string): Promise<boolean> => {
        const text = (remoteInputDrafts[sessionID] || "").trim();
        if (!text) return false;
        try {
            console.log(`[remote] sending input to ${sessionID}: ${JSON.stringify(text)}`);
            await SendRemoteSessionInput(sessionID, text + "\n");
            console.log(`[remote] input sent successfully to ${sessionID}`);
            setRemoteInputDrafts((prev) => ({ ...prev, [sessionID]: "" }));
            // Staggered refreshes so the user sees the tool's response sooner.
            for (const d of [300, 1000, 2500]) setTimeout(() => refreshSessionsOnly(), d);
            return true;
        } catch (err) {
            console.error("Failed to send remote input:", err);
            showToastMessage(formatText("remoteSendFailed", { error: String(err) }), 4000);
            return false;
        }
    };

    const interruptRemoteSession = async (sessionID: string) => {
        await InterruptRemoteSessionAPI(sessionID);
        setRemoteSessions((prev) => prev.map((session) => (
            session.id === sessionID
                ? {
                    ...session,
                    status: "waiting_input",
                    summary: { ...session.summary, status: "waiting_input" },
                }
                : session
        )));
        // Lightweight refresh to pick up the actual state after interrupt
        setTimeout(() => refreshSessionsOnly(), 800);
    };

    const killRemoteSession = async (sessionID: string) => {
        try {
            await KillRemoteSessionAPI(sessionID);
        } catch (err) {
            console.warn("KillRemoteSession error (may already be stopped):", err);
        }
        killedSessionIdsRef.current.add(sessionID);
        setRemoteSessions((prev) => prev.filter((session) => session.id !== sessionID));
        setRemoteInputDrafts((prev) => {
            const next = { ...prev };
            delete next[sessionID];
            return next;
        });
        // Lightweight refresh — the "closed" event from backend will also
        // trigger a full refresh, but this ensures immediate UI cleanup.
        setTimeout(() => refreshSessionsOnly(), 500);
    };

    const clearRemoteActivationState = async () => {
        setRemoteBusy("clear");
        try {
            await ClearRemoteActivation();
            await refreshRemotePanel();
            setRemoteToolReadiness(null);
            setRemoteToolLaunchProbe(null);
            setRemoteSmokeReport(null);
            showToastMessage(translate("remoteActivationCleared"), 3000);
        } catch (err) {
            console.error("Failed to clear remote activation:", err);
            showToastMessage(formatText("remoteClearFailed", { error: String(err) }), 4000);
        } finally {
            setRemoteBusy("");
        }
    };

    // Load remote tool metadata for any tool tab so the local/remote toggle renders correctly
    useEffect(() => {
        if (remoteToolMetadata.length > 0) return; // already loaded
        ListRemoteToolMetadata()
            .then((list) => {
                const tools = list || [];
                setRemoteToolMetadata(tools);
                const nextVisibleTools = tools.filter((tool) => tool.visible !== false);
                if (nextVisibleTools.length > 0 && !nextVisibleTools.some((tool) => tool.name === selectedRemoteTool)) {
                    setSelectedRemoteTool(nextVisibleTools[0].name as RemoteToolName);
                }
            })
            .catch((err) => console.error("Failed to load remote tool metadata:", err));
    }, []);

    useEffect(() => {
        if (navTab !== "settings" && navTab !== "remote") return;
        ListRemoteToolMetadata()
            .then((list) => {
                const tools = list || [];
                setRemoteToolMetadata(tools);
                const nextVisibleTools = tools.filter((tool) => tool.visible !== false);
                if (nextVisibleTools.length > 0 && !nextVisibleTools.some((tool) => tool.name === selectedRemoteTool)) {
                    setSelectedRemoteTool(nextVisibleTools[0].name as RemoteToolName);
                }
            })
            .catch((err) => console.error("Failed to load remote tool metadata:", err));
        refreshRemotePanel();
        refreshRemoteReadiness();
        if (!remotePTYProbe) {
            refreshRemotePTYProbe();
        }
    }, [navTab]);

    // Auto-poll remote panel status every 10 seconds as a fallback
    // (real-time updates are driven by the event listener below)
    useEffect(() => {
        if (navTab !== "settings" && navTab !== "remote") return;
        const timer = setInterval(() => {
            refreshRemotePanel();
        }, 10000);
        return () => {
            clearInterval(timer);
        };
    }, [navTab]);

    // Listen for real-time session change events from the Go backend.
    // The backend emits: ("remote-session-changed", eventType, sessionID)
    // where eventType is one of: "created", "summary", "preview_delta",
    // "important_event", "input", "interrupt", "kill", "closed".
    //
    // Strategy:
    //  - "input" / "interrupt" / "kill": already handled optimistically, skip
    //  - "preview_delta" / "summary" / "important_event": lightweight session
    //    refresh with debounce (high frequency, only need session data)
    //  - "created" / "closed": full panel refresh (affects counts, connection)
    const sessionRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        if (!config?.remote_enabled) return;
        const cleanup = EventsOn("remote-session-changed", (...args: any[]) => {
            const eventType = typeof args[0] === "string" ? args[0] : "";

            // Skip events that are already handled optimistically by the UI
            if (eventType === "input" || eventType === "interrupt" || eventType === "kill") {
                return;
            }

            // Structural changes: full refresh
            if (eventType === "created" || eventType === "closed") {
                refreshRemotePanel();
                return;
            }

            // High-frequency data events: debounced lightweight refresh
            // (preview_delta, summary, important_event)
            if (sessionRefreshTimerRef.current) {
                clearTimeout(sessionRefreshTimerRef.current);
            }
            sessionRefreshTimerRef.current = setTimeout(() => {
                sessionRefreshTimerRef.current = null;
                refreshSessionsOnly();
            }, 300);
        });
        return () => {
            if (sessionRefreshTimerRef.current) {
                clearTimeout(sessionRefreshTimerRef.current);
                sessionRefreshTimerRef.current = null;
            }
            if (typeof cleanup === "function") cleanup();
            else EventsOff("remote-session-changed");
        };
    }, [config?.remote_enabled, refreshSessionsOnly]);

    // Listen for local PTY output changes.  The Go backend emits
    // "remote-state-changed" whenever a local session's output, summary,
    // or events change.  This fires *before* the hub relay, so it is the
    // fastest path for updating the UI when the desktop app owns the
    // session.  We debounce at 200ms to avoid excessive re-renders.
    const localStateTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        if (!config?.remote_enabled) return;
        const cleanup = EventsOn("remote-state-changed", () => {
            if (localStateTimerRef.current) {
                clearTimeout(localStateTimerRef.current);
            }
            localStateTimerRef.current = setTimeout(() => {
                localStateTimerRef.current = null;
                refreshSessionsOnly();
            }, 200);
        });
        return () => {
            if (localStateTimerRef.current) {
                clearTimeout(localStateTimerRef.current);
                localStateTimerRef.current = null;
            }
            if (typeof cleanup === "function") cleanup();
            else EventsOff("remote-state-changed");
        };
    }, [config?.remote_enabled, refreshSessionsOnly]);

    // Auto-restore activation status on startup when remote was previously enabled.
    // Depends on config?.remote_enabled so that it fires once config is loaded
    // (config is null on initial mount because LoadConfig is async).
    useEffect(() => {
        if (config?.remote_enabled) {
            GetRemoteActivationStatus()
                .then((activation) => setRemoteActivationStatus(activation))
                .catch((err) => console.error("Failed to check remote activation on startup:", err));
        }
    }, [config?.remote_enabled]);

    useEffect(() => {
        setRemoteToolReadiness(null);
        setRemoteToolLaunchProbe(null);
        setRemoteSmokeReport(null);
    }, [selectedRemoteTool]);

    useEffect(() => {
        // Clear stale provider state immediately so a mid-flight "Start"
        // won't send a provider name that belongs to the previous tool.
        setProviders([]);
        setSelectedProvider("");
        ListValidProviders(selectedRemoteTool)
            .then((list) => {
                const providerList = Array.isArray(list) ? list : [];
                setProviders(providerList);
                const defaultProvider = providerList.find((p: any) => p.is_default);
                setSelectedProvider(defaultProvider?.name || (providerList.length > 0 ? providerList[0].name : ""));
            })
            .catch((err) => console.error("Failed to load providers:", err));
    }, [selectedRemoteTool]);

    return {
        remoteActivationStatus,
        remoteConnectionStatus,
        remoteToolReadiness,
        remotePTYProbe,
        remoteToolLaunchProbe,
        remoteSmokeReport,
        remoteSessions,
        remoteInputDrafts,
        setRemoteInputDrafts,
        remoteBusy,
        selectedRemoteTool,
        setSelectedRemoteTool,
        remoteToolMetadata,
        visibleRemoteTools,
        selectedRemoteToolInfo,
        selectedRemoteToolCanStart,
        selectedRemoteToolUnavailableReason,
        selectedRemoteToolBadges,
        remoteSuggestedAction,
        getRemoteToolLabel,
        getRemoteToolConfigHint,
        getRemoteToolSmokeHint,
        getRemoteReadinessDetail,
        getRemoteLaunchDetail,
        getRemoteSmokeDetail,
        refreshRemotePanel,
        refreshSessionsOnly,
        refreshRemoteReadiness,
        refreshRemotePTYProbe,
        refreshRemoteLaunchProbe,
        runRemoteSmoke,
        activateRemoteWithEmail,
        reconnectRemote,
        startRemoteSession,
        quickStartRemoteSession,
        installSelectedRemoteTool,
        saveRemoteConfigField,
        sendRemoteInput,
        interruptRemoteSession,
        killRemoteSession,
        clearRemoteActivationState,
        onDemandInstallingTool,
        invitationCodeRequired,
        invitationCode,
        setInvitationCode,
        invitationCodeError,
        providers,
        selectedProvider,
        setSelectedProvider,
    };
}
