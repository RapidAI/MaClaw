import type { CSSProperties } from "react";

export const remoteCardStyle: CSSProperties = {
    border: "1px solid #e2e8f0",
    borderRadius: "12px",
    padding: "12px",
    background: "#fff",
};

export const remoteMutedCardStyle: CSSProperties = {
    padding: "10px",
    borderRadius: "10px",
    background: "#f8fafc",
    border: "1px solid #e2e8f0",
};

export const remoteSessionCardStyle: CSSProperties = {
    border: "1px solid rgba(148, 163, 184, 0.22)",
    borderRadius: "16px",
    padding: "14px",
    background: "linear-gradient(180deg, #fafbff 0%, #f5f7fc 100%)",
    boxShadow: "0 4px 12px rgba(15, 23, 42, 0.04)",
};

export const remotePanelGridStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
    gap: "12px",
    marginBottom: "14px",
};

export const remoteSectionTitleStyle: CSSProperties = {
    fontSize: "0.84rem",
    fontWeight: 700,
    color: "#1e293b",
    marginBottom: "10px",
    letterSpacing: "-0.01em",
};

export const remoteLabelStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#64748b",
    marginBottom: "4px",
};

export const remoteMetaLabelStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#94a3b8",
    textTransform: "uppercase",
    letterSpacing: "0.04em",
    marginBottom: "8px",
};

export const remoteBodyTextStyle: CSSProperties = {
    fontSize: "0.76rem",
    color: "#64748b",
};

export const remoteActionButtonStyle: CSSProperties = {
    fontSize: "0.75rem",
    padding: "4px 10px",
};

export const remoteToolbarCardStyle: CSSProperties = {
    border: "1px solid #e0e7ff",
    borderRadius: "14px",
    padding: "14px 16px",
    background: "linear-gradient(180deg, #fafbff 0%, #eef2ff 100%)",
    boxShadow: "0 4px 12px rgba(99, 102, 241, 0.06)",
};

export const remoteSessionMetricCardStyle: CSSProperties = {
    borderRadius: "12px",
    border: "1px solid rgba(148, 163, 184, 0.18)",
    background: "rgba(255,255,255,0.85)",
    padding: "10px 12px",
    minHeight: "84px",
};

export const remoteSessionSummaryCardStyle: CSSProperties = {
    borderRadius: "14px",
    border: "1px solid rgba(99, 102, 241, 0.12)",
    background: "linear-gradient(180deg, rgba(238,242,255,0.95) 0%, rgba(255,255,255,0.9) 100%)",
    padding: "12px 13px",
    marginBottom: "12px",
};
