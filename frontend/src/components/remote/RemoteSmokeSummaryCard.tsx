import { colors, radius } from "./styles";

type Props = {
    remoteSmokeReport: any;
    getRemoteSmokeDetail: () => string;
};

export function RemoteSmokeSummaryCard({ remoteSmokeReport, getRemoteSmokeDetail }: Props) {
    return (
        <div
            style={{
                marginTop: "16px",
                border: `1px solid ${colors.borderLight}`,
                borderRadius: radius.lg,
                padding: "14px 16px",
                background: "rgba(248, 250, 252, 0.92)",
            }}
        >
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "12px" }}>
                <div>
                    <div style={{ fontWeight: 700, color: colors.text }}>Latest Full Demo</div>
                    <div style={{ fontSize: "12px", color: colors.textSecondary, marginTop: "4px" }}>
                        {remoteSmokeReport?.last_updated || "No recorded full demo yet"}
                    </div>
                </div>
                <div
                    style={{
                        padding: "4px 10px",
                        borderRadius: radius.pill,
                        fontSize: "12px",
                        fontWeight: 700,
                        color: remoteSmokeReport?.success ? colors.success : colors.danger,
                        background: remoteSmokeReport?.success ? "rgba(34,197,94,0.12)" : "rgba(244,63,94,0.12)",
                    }}
                >
                    {remoteSmokeReport ? (remoteSmokeReport.success ? "Success" : "Needs Attention") : "Not Run"}
                </div>
            </div>

            <div style={{ marginTop: "12px", fontSize: "13px", color: colors.text, lineHeight: 1.6 }}>
                <div><strong>Phase:</strong> {remoteSmokeReport?.phase || "idle"}</div>
                <div><strong>Summary:</strong> {getRemoteSmokeDetail()}</div>
                {remoteSmokeReport?.recommended_next ? (
                    <div><strong>Next:</strong> {remoteSmokeReport.recommended_next}</div>
                ) : null}
                {remoteSmokeReport?.started_session?.id ? (
                    <div><strong>Session:</strong> {remoteSmokeReport.started_session.id}</div>
                ) : null}
                {remoteSmokeReport?.hub_visibility ? (
                    <div><strong>Hub Visible:</strong> {remoteSmokeReport.hub_visibility.verified ? "Yes" : "No"}</div>
                ) : null}
            </div>
        </div>
    );
}
