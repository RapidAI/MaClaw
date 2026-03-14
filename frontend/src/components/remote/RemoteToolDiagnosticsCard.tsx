import type { RemoteToolLaunchProbeView, RemoteToolName, RemoteToolReadinessView, RemoteSmokeReportView } from "./types";
import {
    colors,
    remoteBodyTextStyle,
    remoteCardStyle,
    remoteMutedCardStyle,
    remoteSectionTitleStyle,
    remoteSubHeadingStyle,
    remoteDetailTextStyle,
} from "./styles";

type Props = {
    selectedRemoteTool: RemoteToolName;
    remoteToolReadiness: RemoteToolReadinessView | null;
    remotePTYProbe: { supported?: boolean; message?: string } | null;
    remoteToolLaunchProbe: RemoteToolLaunchProbeView | null;
    remoteSmokeReport: RemoteSmokeReportView | null;
    getSelectedProjectForRemote: () => string;
    getRemoteToolLabel: (tool: string) => string;
    getRemoteToolConfigHint: (tool: string) => string;
    getRemoteToolSmokeHint: (tool: string) => string;
    normalizeIssueItems: (items: unknown) => string[];
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
};

export function RemoteToolDiagnosticsCard(props: Props) {
    const {
        selectedRemoteTool,
        remoteToolReadiness,
        remotePTYProbe,
        remoteToolLaunchProbe,
        remoteSmokeReport,
        getSelectedProjectForRemote,
        getRemoteToolLabel,
        getRemoteToolConfigHint,
        getRemoteToolSmokeHint,
        normalizeIssueItems,
        translate,
        formatText,
    } = props;

    return (
        <div style={remoteCardStyle}>
            <div style={remoteSectionTitleStyle}>{translate("remoteDiagnosticsTitle")}</div>
            <div style={{ ...remoteBodyTextStyle, marginBottom: "6px" }}>
                {translate("remoteLaunchProject")}: <span style={{ color: colors.text, fontWeight: 600 }}>{getSelectedProjectForRemote() || translate("remoteNoProjectSelected")}</span>
            </div>
            <div style={{ ...remoteBodyTextStyle, marginBottom: "6px" }}>{getRemoteToolConfigHint(selectedRemoteTool)}</div>
            <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                <div style={remoteMutedCardStyle}>
                    <div style={remoteSubHeadingStyle}>{translate("remoteReadinessWarnings")}</div>
                    {(normalizeIssueItems(remoteToolReadiness?.warnings).length > 0 || normalizeIssueItems(remoteToolReadiness?.issues).length > 0) ? (
                        <ul style={{ margin: 0, paddingLeft: "16px", color: colors.textSecondary, fontSize: "0.72rem", display: "flex", flexDirection: "column", gap: "2px" }}>
                            {normalizeIssueItems(remoteToolReadiness?.issues).map((item, idx) => <li key={`issue-${idx}`} style={{ color: colors.danger }}>{item}</li>)}
                            {normalizeIssueItems(remoteToolReadiness?.warnings).map((item, idx) => <li key={`warning-${idx}`}>{item}</li>)}
                        </ul>
                    ) : <div style={{ ...remoteDetailTextStyle, color: colors.success }}>{translate("remoteNoReadinessIssues")}</div>}
                </div>

                <div style={remoteMutedCardStyle}>
                    <div style={remoteSubHeadingStyle}>{translate("remoteRunConpty")}</div>
                    <div style={{ ...remoteDetailTextStyle, color: remotePTYProbe?.supported ? colors.success : colors.danger }}>
                        {remotePTYProbe
                            ? (remotePTYProbe.supported
                                ? formatText("remoteConptyAvailable", { tool: getRemoteToolLabel(selectedRemoteTool) })
                                : (remotePTYProbe.message || translate("remoteConptyUnavailable")))
                            : translate("remoteProbeNotRun")}
                    </div>
                </div>

                <div style={remoteMutedCardStyle}>
                    <div style={remoteSubHeadingStyle}>
                        {formatText("remoteLaunchProbeTitle", { tool: getRemoteToolLabel(selectedRemoteTool) })}
                    </div>
                    <div style={{ ...remoteDetailTextStyle, color: remoteToolLaunchProbe?.ready ? colors.success : colors.textSecondary, wordBreak: "break-word" }}>
                        {remoteToolLaunchProbe
                            ? (remoteToolLaunchProbe.ready
                                ? formatText("remoteCommandReady", { value: remoteToolLaunchProbe.command_path || `${getRemoteToolLabel(selectedRemoteTool)} executable resolved` })
                                : (remoteToolLaunchProbe.message || translate("remoteLaunchProbeFailed")))
                            : formatText("remoteLaunchProbePending", { tool: getRemoteToolLabel(selectedRemoteTool) })}
                    </div>
                </div>

                <div style={remoteMutedCardStyle}>
                    <div style={remoteSubHeadingStyle}>{translate("remoteFullSmoke")}</div>
                    <div style={{ fontSize: "0.7rem", color: colors.textSecondary, marginBottom: "6px" }}>{getRemoteToolSmokeHint(selectedRemoteTool)}</div>
                    {remoteSmokeReport ? (
                        <div style={{ display: "flex", flexDirection: "column", gap: "4px", ...remoteDetailTextStyle }}>
                            <div><span style={{ fontWeight: 700, color: colors.text }}>{translate("remoteTool")}:</span> {getRemoteToolLabel(remoteSmokeReport.tool || selectedRemoteTool)}</div>
                            <div><span style={{ fontWeight: 700, color: colors.text }}>{translate("remoteActivation")}:</span> {remoteSmokeReport.activation?.email || "n/a"} {remoteSmokeReport.activation?.machine_id ? `(${remoteSmokeReport.activation.machine_id})` : ""}</div>
                            <div><span style={{ fontWeight: 700, color: colors.text }}>{translate("remotePty")}:</span> {remoteSmokeReport.pty_probe?.supported ? translate("remoteSupported") : (remoteSmokeReport.pty_probe?.message || translate("remoteUnavailableShort"))}</div>
                            <div style={{ wordBreak: "break-word" }}><span style={{ fontWeight: 700, color: colors.text }}>{translate("remoteLaunch")}:</span> {remoteSmokeReport.launch_probe?.ready ? (remoteSmokeReport.launch_probe?.command_path || translate("remoteReady")) : (remoteSmokeReport.launch_probe?.message || translate("remoteFailed"))}</div>
                            <div><span style={{ fontWeight: 700, color: colors.text }}>{translate("remoteSession")}:</span> {remoteSmokeReport.started_session?.id || "n/a"} {remoteSmokeReport.started_session?.status ? `(${remoteSmokeReport.started_session.status})` : ""}</div>
                            <div><span style={{ fontWeight: 700, color: colors.text }}>{translate("remoteHubVisibility")}:</span> {remoteSmokeReport.hub_visibility?.verified ? translate("remoteVerified") : translate("remoteNotVerified")}</div>
                            {remoteSmokeReport.hub_visibility?.message ? <div style={{ color: remoteSmokeReport.hub_visibility?.verified ? colors.success : colors.danger }}>{remoteSmokeReport.hub_visibility.message}</div> : null}
                        </div>
                    ) : <div style={remoteDetailTextStyle}>{translate("remoteFullSmokeNotRun")}</div>}
                </div>
            </div>
        </div>
    );
}
