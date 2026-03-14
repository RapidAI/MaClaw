import { main } from "../../../wailsjs/go/models";
import { RemoteRoutingCard } from "./RemoteRoutingCard";
import { RemoteToolDiagnosticsCard } from "./RemoteToolDiagnosticsCard";
import type {
    RemoteSuggestedAction,
    RemoteToolLaunchProbeView,
    RemoteToolMetadataView,
    RemoteToolName,
    RemoteToolReadinessView,
    RemoteSmokeReportView,
} from "./types";

type Props = {
    config: main.AppConfig | null;
    remoteBusy: string;
    selectedRemoteTool: RemoteToolName;
    visibleRemoteTools: RemoteToolMetadataView[];
    selectedRemoteToolInfo?: RemoteToolMetadataView;
    selectedRemoteToolCanStart: boolean;
    selectedRemoteToolUnavailableReason: string;
    selectedRemoteToolBadges: string[];
    remoteSuggestedAction: RemoteSuggestedAction | null;
    remoteToolReadiness: RemoteToolReadinessView | null;
    remotePTYProbe: { supported?: boolean; message?: string } | null;
    remoteToolLaunchProbe: RemoteToolLaunchProbeView | null;
    remoteSmokeReport: RemoteSmokeReportView | null;
    getSelectedProjectForRemote: () => string;
    getRemoteToolLabel: (tool: string) => string;
    getRemoteToolConfigHint: (tool: string) => string;
    getRemoteToolSmokeHint: (tool: string) => string;
    normalizeIssueItems: (items: unknown) => string[];
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    setSelectedRemoteTool: (tool: RemoteToolName) => void;
    activateRemoteWithEmail: () => void;
    startRemoteSession: () => void;
    installSelectedRemoteTool: () => void;
    reconnectRemote: () => void;
    clearRemoteActivationState: () => void;
    refreshRemoteReadiness: () => void;
    switchTool: (tool: string) => void;
    onDemandInstallingTool: string;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
};

export function RemoteDiagnosticsPanel(props: Props) {
    const {
        config,
        remoteBusy,
        selectedRemoteTool,
        visibleRemoteTools,
        selectedRemoteToolInfo,
        selectedRemoteToolCanStart,
        selectedRemoteToolUnavailableReason,
        selectedRemoteToolBadges,
        remoteSuggestedAction,
        remoteToolReadiness,
        remotePTYProbe,
        remoteToolLaunchProbe,
        remoteSmokeReport,
        getSelectedProjectForRemote,
        getRemoteToolLabel,
        getRemoteToolConfigHint,
        getRemoteToolSmokeHint,
        normalizeIssueItems,
        saveRemoteConfigField,
        setSelectedRemoteTool,
        activateRemoteWithEmail,
        startRemoteSession,
        installSelectedRemoteTool,
        reconnectRemote,
        clearRemoteActivationState,
        refreshRemoteReadiness,
        switchTool,
        onDemandInstallingTool,
        translate,
        formatText,
    } = props;

    return (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))", gap: "8px", marginBottom: "10px" }}>
            <RemoteRoutingCard
                config={config}
                remoteBusy={remoteBusy}
                selectedRemoteTool={selectedRemoteTool}
                visibleRemoteTools={visibleRemoteTools}
                selectedRemoteToolInfo={selectedRemoteToolInfo}
                selectedRemoteToolCanStart={selectedRemoteToolCanStart}
                selectedRemoteToolUnavailableReason={selectedRemoteToolUnavailableReason}
                selectedRemoteToolBadges={selectedRemoteToolBadges}
                remoteSuggestedAction={remoteSuggestedAction}
                getRemoteToolLabel={getRemoteToolLabel}
                saveRemoteConfigField={saveRemoteConfigField}
                setSelectedRemoteTool={setSelectedRemoteTool}
                activateRemoteWithEmail={activateRemoteWithEmail}
                startRemoteSession={startRemoteSession}
                installSelectedRemoteTool={installSelectedRemoteTool}
                reconnectRemote={reconnectRemote}
                clearRemoteActivationState={clearRemoteActivationState}
                refreshRemoteReadiness={refreshRemoteReadiness}
                switchTool={switchTool}
                onDemandInstallingTool={onDemandInstallingTool}
                translate={translate}
                formatText={formatText}
            />
            <RemoteToolDiagnosticsCard
                selectedRemoteTool={selectedRemoteTool}
                remoteToolReadiness={remoteToolReadiness}
                remotePTYProbe={remotePTYProbe}
                remoteToolLaunchProbe={remoteToolLaunchProbe}
                remoteSmokeReport={remoteSmokeReport}
                getSelectedProjectForRemote={getSelectedProjectForRemote}
                getRemoteToolLabel={getRemoteToolLabel}
                getRemoteToolConfigHint={getRemoteToolConfigHint}
                getRemoteToolSmokeHint={getRemoteToolSmokeHint}
                normalizeIssueItems={normalizeIssueItems}
                translate={translate}
                formatText={formatText}
            />
        </div>
    );
}
