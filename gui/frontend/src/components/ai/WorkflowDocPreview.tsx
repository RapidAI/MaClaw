import React, { useEffect, useState } from "react";
import type { QualityGateResult } from "./useWorkflowState";

interface WorkflowDocPreviewProps {
    phaseDocuments: Map<string, string>;
    currentPhaseID: string;
    gateResults: Map<string, QualityGateResult>;
    onClose: () => void;
}

/**
 * WorkflowDocPreview renders the right-side document preview panel
 * during workflow execution. Shows the current phase's Markdown document
 * with phase tabs for switching between phases and quality gate results.
 */
export function WorkflowDocPreview({
    phaseDocuments,
    currentPhaseID,
    gateResults,
    onClose,
}: WorkflowDocPreviewProps) {
    const [viewingPhaseID, setViewingPhaseID] = useState(currentPhaseID);

    // Sync to current phase when workflow advances
    useEffect(() => {
        if (currentPhaseID) setViewingPhaseID(currentPhaseID);
    }, [currentPhaseID]);
    const activePhaseID = viewingPhaseID || currentPhaseID;
    const content = phaseDocuments.get(activePhaseID) || "";
    const gateResult = gateResults.get(activePhaseID);
    const phaseIDs = Array.from(phaseDocuments.keys());

    return (
        <div style={{
            display: "flex",
            flexDirection: "column",
            height: "100%",
            borderLeft: "1px solid var(--border-color, #e0e0e0)",
            background: "var(--bg-secondary, #fafafa)",
        }}>
            {/* Header: phase tabs + close button */}
            <div style={{
                display: "flex",
                alignItems: "center",
                padding: "8px 12px",
                borderBottom: "1px solid var(--border-color, #e0e0e0)",
                gap: "4px",
                flexWrap: "wrap",
            }}>
                {phaseIDs.map(pid => (
                    <button
                        key={pid}
                        onClick={() => setViewingPhaseID(pid)}
                        style={{
                            padding: "4px 10px",
                            fontSize: "12px",
                            border: pid === activePhaseID ? "1px solid var(--accent-color, #4a90d9)" : "1px solid transparent",
                            borderRadius: "4px",
                            background: pid === activePhaseID ? "var(--accent-bg, #e8f0fe)" : "transparent",
                            cursor: "pointer",
                            color: pid === activePhaseID ? "var(--accent-color, #4a90d9)" : "inherit",
                        }}
                    >
                        {pid}
                    </button>
                ))}
                <div style={{ flex: 1 }} />
                <button
                    onClick={onClose}
                    style={{
                        background: "none",
                        border: "none",
                        cursor: "pointer",
                        fontSize: "16px",
                        padding: "2px 6px",
                        borderRadius: "4px",
                    }}
                    title="关闭文档预览"
                >
                    ×
                </button>
            </div>

            {/* Quality gate banner */}
            {gateResult && (
                <div style={{
                    padding: "6px 12px",
                    fontSize: "12px",
                    borderBottom: "1px solid var(--border-color, #e0e0e0)",
                    background: gateResult.passed ? "#e8f5e9" : "#fff3e0",
                }}>
                    {gateResult.passed ? "✅" : "⚠️"} 质量门禁：
                    {gateResult.items.map((item, i) => (
                        <span key={i} style={{ marginLeft: "8px" }}>
                            {item.passed ? "✅" : "⚠️"} {item.description}
                        </span>
                    ))}
                </div>
            )}

            {/* Document content */}
            <div style={{
                flex: 1,
                overflow: "auto",
                padding: "16px",
                fontSize: "14px",
                lineHeight: "1.6",
                whiteSpace: "pre-wrap",
                fontFamily: "inherit",
            }}>
                {content || <span style={{ color: "#999" }}>暂无文档内容</span>}
            </div>
        </div>
    );
}
