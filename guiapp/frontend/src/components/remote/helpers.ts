import type {
    RemoteActivationStatus,
    RemoteConnectionStatus,
    RemoteSessionView,
    RemoteSmokeReportView,
    RemoteSuggestedAction,
    RemoteToolLaunchProbeView,
    RemoteToolMetadataView,
    BuiltinRemoteToolName,
    RemoteToolName,
    RemoteToolReadinessView,
} from "./types";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;
type Translate = (key: string) => string;
type FormatText = (key: string, vars?: Record<string, string>) => string;

export const fallbackRemoteToolMeta: Record<BuiltinRemoteToolName, { label: string; configHint: string; smokeHint: string }> = {
    claude: {
        label: "Claude",
        configHint: "Checks Anthropic-compatible auth, Claude launch command, and remote PTY readiness.",
        smokeHint: "Runs registration, PTY, launch, real session start, and Hub visibility verification for Claude.",
    },
    codex: {
        label: "Codex",
        configHint: "Checks OpenAI-compatible auth, Codex command resolution, and remote PTY readiness.",
        smokeHint: "Runs registration, PTY, launch, real session start, and Hub visibility verification for Codex.",
    },
    opencode: {
        label: "OpenCode",
        configHint: "Checks OpenCode config sync, OpenAI-compatible endpoints, and isolated session config.",
        smokeHint: "Runs registration, PTY, launch, real session start, and Hub visibility verification for OpenCode.",
    },
    codebuddy: {
        label: "CodeBuddy",
        configHint: "Checks CodeBuddy config sync, OpenAI-compatible endpoints, and remote PTY readiness.",
        smokeHint: "Runs registration, PTY, launch, real session start, and Hub visibility verification for CodeBuddy.",
    },
    iflow: {
        label: "iFlow",
        configHint: "Checks iFlow config sync plus IFLOW and OpenAI-compatible environment wiring.",
        smokeHint: "Runs registration, PTY, launch, real session start, and Hub visibility verification for iFlow.",
    },
    kilo: {
        label: "Kilo",
        configHint: "Checks Kilo config sync plus KILO and OpenAI-compatible environment wiring.",
        smokeHint: "Runs registration, PTY, launch, real session start, and Hub visibility verification for Kilo.",
    },
};

const normalizeRemoteMetaName = (name: string): string => name.trim().toLowerCase();

const removedRemoteToolNames = new Set(["cursor", "gemini"]);

const isRemovedRemoteToolName = (name: string): boolean => removedRemoteToolNames.has(normalizeRemoteMetaName(name));

const isFallbackRemoteToolName = (name: string): name is BuiltinRemoteToolName => (
    Object.prototype.hasOwnProperty.call(fallbackRemoteToolMeta, name)
);

export function buildRemoteToolMetaByName(remoteToolMetadata: RemoteToolMetadataView[]): Record<string, RemoteToolMetadataView> {
    const mapped: Record<string, RemoteToolMetadataView> = {};
    for (const item of remoteToolMetadata) {
        const name = normalizeRemoteMetaName(item.name);
        if (isRemovedRemoteToolName(name)) continue;
        mapped[name] = { ...item, name };
    }
    return mapped;
}

export function buildVisibleRemoteTools(remoteToolMetadata: RemoteToolMetadataView[]): RemoteToolMetadataView[] {
    if (remoteToolMetadata.length > 0) {
        return remoteToolMetadata
            .map((tool) => ({ ...tool, name: normalizeRemoteMetaName(tool.name) }))
            .filter((tool) => tool.visible !== false && !isRemovedRemoteToolName(tool.name));
    }
    return Object.keys(fallbackRemoteToolMeta).map((name) => {
        const toolName = name as BuiltinRemoteToolName;
        return {
            name: toolName,
            display_name: fallbackRemoteToolMeta[toolName].label,
            visible: true,
            can_start: true,
        };
    });
}

export function getRemoteToolLabel(tool: string, remoteToolMetaByName: Record<string, RemoteToolMetadataView>): string {
    const normalized = normalizeRemoteMetaName(tool);
    const fallbackLabel = isFallbackRemoteToolName(normalized) ? fallbackRemoteToolMeta[normalized].label : undefined;
    return remoteToolMetaByName[normalized]?.display_name ?? fallbackLabel ?? tool;
}

export function getRemoteToolConfigHint(
    tool: string,
    remoteToolMetaByName: Record<string, RemoteToolMetadataView>,
    localizeText: LocalizeText,
): string {
    return remoteToolMetaByName[normalizeRemoteMetaName(tool)]?.readiness_hint ?? localizeText(
        `Checks ${getRemoteToolLabel(tool, remoteToolMetaByName)} command resolution, configuration wiring, and remote PTY readiness.`,
        `检查 ${getRemoteToolLabel(tool, remoteToolMetaByName)} 的命令解析、配置连通性，以及远程 PTY 就绪情况。`,
        `檢查 ${getRemoteToolLabel(tool, remoteToolMetaByName)} 的命令解析、配置連通性，以及遠端 PTY 就緒情況。`,
    );
}

export function getRemoteToolSmokeHint(
    tool: string,
    remoteToolMetaByName: Record<string, RemoteToolMetadataView>,
    localizeText: LocalizeText,
): string {
    return remoteToolMetaByName[normalizeRemoteMetaName(tool)]?.smoke_hint ?? localizeText(
        `Runs registration, PTY, launch, real session start, and Hub visibility verification for ${getRemoteToolLabel(tool, remoteToolMetaByName)}.`,
        `执行 ${getRemoteToolLabel(tool, remoteToolMetaByName)} 的注册、PTY、启动、真实会话启动与 Hub 可见性验证。`,
        `執行 ${getRemoteToolLabel(tool, remoteToolMetaByName)} 的註冊、PTY、啟動、真實會話啟動與 Hub 可見性驗證。`,
    );
}

export function getRemoteReadinessDetail(args: {
    selectedRemoteTool: RemoteToolName;
    remoteToolReadiness: RemoteToolReadinessView | null;
    remoteToolMetaByName: Record<string, RemoteToolMetadataView>;
    getSelectedProjectForRemote: () => string;
    translate: Translate;
}): string {
    const { selectedRemoteTool, remoteToolReadiness, remoteToolMetaByName, getSelectedProjectForRemote, translate } = args;
    if (!remoteToolReadiness) {
        return getSelectedProjectForRemote() || translate("remoteNoProjectSelected");
    }
    const parts: string[] = [];
    parts.push(remoteToolReadiness.tool_installed ? translate("installed") : translate("remoteNotInstalled"));
    parts.push(remoteToolReadiness.model_configured ? translate("saved") : translate("remoteConfigureModelStep"));
    if (remoteToolReadiness.ready) {
        parts.push(translate("remoteReady"));
    } else if (remoteToolReadiness.issues && remoteToolReadiness.issues.length > 0) {
        parts.push(remoteToolReadiness.issues[0]);
    }
    return `${getRemoteToolLabel(selectedRemoteTool, remoteToolMetaByName)}: ${parts.join(" | ")}`;
}

export function getRemoteLaunchDetail(args: {
    selectedRemoteTool: RemoteToolName;
    remoteToolLaunchProbe: RemoteToolLaunchProbeView | null;
    remoteToolMetaByName: Record<string, RemoteToolMetadataView>;
    formatText: FormatText;
}): string {
    const { selectedRemoteTool, remoteToolLaunchProbe, remoteToolMetaByName, formatText } = args;
    if (!remoteToolLaunchProbe) {
        return formatText("remoteLaunchProbePending", { tool: getRemoteToolLabel(selectedRemoteTool, remoteToolMetaByName) });
    }
    if (remoteToolLaunchProbe.command_path) {
        return remoteToolLaunchProbe.command_path;
    }
    return remoteToolLaunchProbe.message || formatText("remoteLaunchProbePending", { tool: getRemoteToolLabel(selectedRemoteTool, remoteToolMetaByName) });
}

export function getRemoteSmokeDetail(args: {
    remoteSmokeReport: RemoteSmokeReportView | null;
    translate: Translate;
}): string {
    const { remoteSmokeReport, translate } = args;
    if (!remoteSmokeReport) {
        return translate("remoteFullSmokeNotRun");
    }
    if (remoteSmokeReport.recommended_next) {
        const prefix = remoteSmokeReport.success ? translate("remoteVerified") : translate("remoteFailed");
        const phase = remoteSmokeReport.phase ? ` (${remoteSmokeReport.phase})` : "";
        return `${prefix}${phase}: ${remoteSmokeReport.recommended_next}`;
    }
    if (remoteSmokeReport.phase) {
        const prefix = remoteSmokeReport.success ? translate("remoteVerified") : translate("remoteFailed");
        return `${prefix}: ${remoteSmokeReport.phase}`;
    }
    if (remoteSmokeReport.hub_visibility?.verified) {
        return remoteSmokeReport.started_session?.id || translate("remoteVerified");
    }
    return remoteSmokeReport.hub_visibility?.message || remoteSmokeReport.started_session?.id || translate("remoteFailed");
}

export function getSelectedRemoteToolBadges(
    selectedRemoteToolInfo: RemoteToolMetadataView | undefined,
    translate: Translate,
    localizeText: LocalizeText,
): string[] {
    return [
        selectedRemoteToolInfo?.installed ? translate("installed") : translate("remoteNotInstalled"),
        selectedRemoteToolInfo?.uses_openai_compat ? localizeText("OpenAI Compat", "OpenAI 兼容", "OpenAI 相容") : localizeText("Native Protocol", "原生协议", "原生協定"),
        selectedRemoteToolInfo?.requires_session_config ? localizeText("Session Config", "需要会话配置", "需要會話配置") : localizeText("Stateless Launch", "无状态启动", "無狀態啟動"),
        selectedRemoteToolInfo?.supports_proxy ? localizeText("Proxy Aware", "支持代理", "支援代理") : localizeText("No Proxy Support", "不支持代理", "不支援代理"),
    ];
}

export function getRemoteSuggestedAction(args: {
    selectedRemoteTool: RemoteToolName;
    selectedRemoteToolInfo: RemoteToolMetadataView | undefined;
    selectedRemoteToolCanStart: boolean;
    remoteActivationStatus: RemoteActivationStatus | null;
    remoteConnectionStatus: RemoteConnectionStatus | null;
    remoteToolReadiness: RemoteToolReadinessView | null;
    remoteToolMetaByName: Record<string, RemoteToolMetadataView>;
    remoteSessions?: RemoteSessionView[];
    formatText: FormatText;
    translate: Translate;
    config?: { remote_hub_url?: string } | null;
}): RemoteSuggestedAction | null {
    const {
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
    } = args;

    if (!selectedRemoteToolInfo?.installed && selectedRemoteToolCanStart) {
        return {
            label: formatText("remoteInstallTool", { tool: getRemoteToolLabel(selectedRemoteTool, remoteToolMetaByName) }),
            description: formatText("remoteInstallTool", { tool: getRemoteToolLabel(selectedRemoteTool, remoteToolMetaByName) }),
            action: "install",
        };
    }
    if (!selectedRemoteToolCanStart) {
        return null;
    }
    if (!remoteActivationStatus?.activated) {
        return {
            label: translate("remoteActivateStep"),
            description: translate("remoteActivateStepDesc"),
            action: "activate",
        };
    }
    if (remoteConnectionStatus && !remoteConnectionStatus.connected && config?.remote_hub_url) {
        return {
            label: translate("remoteReconnectStep"),
            description: translate("remoteReconnectStepDesc"),
            action: "reconnect",
        };
    }
    if (remoteToolReadiness && !remoteToolReadiness.model_configured) {
        return {
            label: translate("remoteConfigureModelStep"),
            description: translate("remoteConfigureModelStepDesc"),
            action: "configure",
        };
    }
    if (!remoteToolReadiness) {
        return {
            label: translate("remoteRunReadinessStep"),
            description: translate("remoteRunReadinessStepDesc"),
            action: "readiness",
        };
    }
    if (!remoteToolReadiness.ready) {
        return {
            label: translate("remoteRunReadinessAgain"),
            description: translate("remoteRunReadinessAgainDesc"),
            action: "readiness",
        };
    }
    if (selectedRemoteToolCanStart) {
        return {
            label: formatText("remoteStartTool", { tool: getRemoteToolLabel(selectedRemoteTool, remoteToolMetaByName) }),
            description: translate("remoteLaunchProject"),
            action: "start",
        };
    }
    return null;
}
