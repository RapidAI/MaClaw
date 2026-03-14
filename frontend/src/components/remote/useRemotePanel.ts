import { useEffect, useMemo, useState } from "react";
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
    ProbeRemoteHub,
    ReconnectRemoteHub,
    RunRemoteToolSmoke,
    SaveConfig,
    SendRemoteSessionInput,
    InterruptRemoteSession as InterruptRemoteSessionAPI,
    KillRemoteSession as KillRemoteSessionAPI,
    StartRemoteHandoffSession,
    StartRemoteSession,
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
import type {
    RemoteActivationStatus,
    RemoteConnectionStatus,
    RemoteSessionView,
    RemoteSmokeReportView,
    RemoteSuggestedAction,
    RemoteToolLaunchProbeView,
    RemoteToolMetadataView,
    RemoteToolName,
    RemoteToolReadinessView,
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
            setRemoteSessions(Array.isArray(sessions) ? sessions : []);
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
            await ActivateRemote(config.remote_email.trim(), invitationCode.trim());
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
            await StartRemoteSession(selectedRemoteTool, projectDir, getUseProxy());
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
                await StartRemoteHandoffSession(tool, projectDir, getUseProxy());
            } else {
                await StartRemoteSession(tool, projectDir, getUseProxy());
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

    const sendRemoteInput = async (sessionID: string) => {
        const text = (remoteInputDrafts[sessionID] || "").trim();
        if (!text) return;
        try {
            await SendRemoteSessionInput(sessionID, text + "\n");
            setRemoteInputDrafts((prev) => ({ ...prev, [sessionID]: "" }));
        } catch (err) {
            console.error("Failed to send remote input:", err);
            showToastMessage(formatText("remoteSendFailed", { error: String(err) }), 4000);
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
        void refreshRemotePanel();
    };

    const killRemoteSession = async (sessionID: string) => {
        try {
            await KillRemoteSessionAPI(sessionID);
        } catch (err) {
            console.warn("KillRemoteSession error (may already be stopped):", err);
        }
        setRemoteSessions((prev) => prev.filter((session) => session.id !== sessionID));
        setRemoteInputDrafts((prev) => {
            const next = { ...prev };
            delete next[sessionID];
            return next;
        });
        void refreshRemotePanel();
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

    // Auto-restore activation status on startup when remote was previously enabled
    useEffect(() => {
        if (config?.remote_enabled) {
            GetRemoteActivationStatus()
                .then((activation) => setRemoteActivationStatus(activation))
                .catch((err) => console.error("Failed to check remote activation on startup:", err));
        }
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        setRemoteToolReadiness(null);
        setRemoteToolLaunchProbe(null);
        setRemoteSmokeReport(null);
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
    };
}
