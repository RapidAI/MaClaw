import { useCallback, useState } from "react";
import type { CSSProperties } from "react";

// --- Types ---

export interface Confirmation {
    id: string;
    instance_id: string;
    recipient_id: string;
    type: "executor" | "notifier";
    status: "pending" | "confirmed" | "auto_closed";
    notes: string;
    confirmed_at?: string;
    auto_closed_at?: string;
    auto_close_reason?: string;
}

export interface ApprovalDecision {
    approver: string;
    decision: string;
    rationale: string;
    timestamp: string;
}

export interface ConfirmationPanelProps {
    confirmation: Confirmation;
    formData?: Record<string, unknown>;
    approvalDecisions?: ApprovalDecision[];
    result?: string;
    onConfirm: (confirmationId: string, notes: string) => Promise<void>;
}

// --- Constants ---

const NOTES_MAX_LENGTH = 2000;

// --- Component ---

export function ConfirmationPanel({
    confirmation,
    formData,
    approvalDecisions,
    result,
    onConfirm,
}: ConfirmationPanelProps) {
    const [notes, setNotes] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState(false);

    const handleConfirm = useCallback(async () => {
        setSubmitting(true);
        setError(null);
        setSuccess(false);
        try {
            await onConfirm(confirmation.id, notes);
            setSuccess(true);
        } catch (err: unknown) {
            const msg = err instanceof Error ? err.message : String(err);
            setError(msg);
        } finally {
            setSubmitting(false);
        }
    }, [confirmation.id, notes, onConfirm]);

    const handleNotesChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
        const value = e.target.value;
        if (value.length <= NOTES_MAX_LENGTH) {
            setNotes(value);
        }
    };

    return (
        <div style={containerStyle}>
            {/* Status Banner */}
            <ConfirmationStatusBanner confirmation={confirmation} />

            {/* Content based on type */}
            {confirmation.type === "executor" ? (
                <ExecutorView
                    formData={formData}
                    approvalDecisions={approvalDecisions}
                    result={result}
                />
            ) : (
                <NotifierView result={result} />
            )}

            {/* Action Area (only for pending status) */}
            {confirmation.status === "pending" && (
                <div style={actionAreaStyle}>
                    {/* Notes input (executor only) */}
                    {confirmation.type === "executor" && (
                        <div style={notesContainerStyle}>
                            <label style={notesLabelStyle} htmlFor="confirmation-notes">
                                操作备注（选填）
                            </label>
                            <textarea
                                id="confirmation-notes"
                                value={notes}
                                onChange={handleNotesChange}
                                placeholder="请描述已执行的操作..."
                                style={notesTextareaStyle}
                                disabled={submitting}
                                maxLength={NOTES_MAX_LENGTH}
                                aria-label="操作备注"
                                aria-describedby="notes-counter"
                            />
                            <div id="notes-counter" style={notesCounterStyle}>
                                {notes.length} / {NOTES_MAX_LENGTH}
                            </div>
                        </div>
                    )}

                    {/* Confirm Button */}
                    <button
                        onClick={handleConfirm}
                        disabled={submitting}
                        style={submitting ? confirmBtnDisabledStyle : confirmBtnStyle}
                        aria-label={confirmation.type === "executor" ? "确认已操作" : "确认已知会"}
                        aria-busy={submitting}
                    >
                        {submitting
                            ? "提交中..."
                            : confirmation.type === "executor"
                                ? "✅ 确认已操作"
                                : "✅ 确认已知会"
                        }
                    </button>

                    {/* Feedback */}
                    {error && (
                        <div style={errorStyle} role="alert">
                            ❌ 提交失败: {error}
                        </div>
                    )}
                    {success && (
                        <div style={successStyle} role="status">
                            ✅ 确认成功
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

// --- Sub-components ---

function ConfirmationStatusBanner({ confirmation }: { confirmation: Confirmation }) {
    if (confirmation.status === "confirmed") {
        return (
            <div style={statusBannerConfirmedStyle} role="status" aria-label="确认状态">
                <div style={statusIconStyle}>✅ 已确认</div>
                {confirmation.confirmed_at && (
                    <div style={statusTimestampStyle}>
                        确认时间: {formatTimestamp(confirmation.confirmed_at)}
                    </div>
                )}
                {confirmation.notes && (
                    <div style={statusNotesStyle}>
                        <span style={statusNotesLabelStyle}>备注:</span> {confirmation.notes}
                    </div>
                )}
            </div>
        );
    }

    if (confirmation.status === "auto_closed") {
        return (
            <div style={statusBannerAutoClosedStyle} role="status" aria-label="确认状态">
                <div style={statusIconStyle}>⏰ 已自动关闭</div>
                {confirmation.auto_closed_at && (
                    <div style={statusTimestampStyle}>
                        关闭时间: {formatTimestamp(confirmation.auto_closed_at)}
                    </div>
                )}
                {confirmation.auto_close_reason && (
                    <div style={statusReasonStyle}>
                        原因: {getAutoCloseReasonLabel(confirmation.auto_close_reason)}
                    </div>
                )}
            </div>
        );
    }

    // pending
    return (
        <div style={statusBannerPendingStyle} role="status" aria-label="确认状态">
            <div style={statusIconStyle}>⏳ 待确认</div>
            <div style={statusDescStyle}>
                {confirmation.type === "executor"
                    ? "请确认您已完成相关操作"
                    : "请确认您已知悉审批结果"
                }
            </div>
        </div>
    );
}

function ExecutorView({
    formData,
    approvalDecisions,
    result,
}: {
    formData?: Record<string, unknown>;
    approvalDecisions?: ApprovalDecision[];
    result?: string;
}) {
    return (
        <div style={contentSectionStyle}>
            {/* Result summary */}
            {result && (
                <div style={resultSummaryStyle}>
                    <span style={resultLabelStyle}>审批结果:</span>
                    <ResultBadge result={result} />
                </div>
            )}

            {/* Form Data */}
            {formData && Object.keys(formData).length > 0 && (
                <div style={sectionStyle}>
                    <h3 style={sectionTitleStyle}>表单数据</h3>
                    <FormDataDisplay data={formData} />
                </div>
            )}

            {/* Approval Decisions */}
            {approvalDecisions && approvalDecisions.length > 0 && (
                <div style={sectionStyle}>
                    <h3 style={sectionTitleStyle}>审批决策</h3>
                    <ApprovalDecisionList decisions={approvalDecisions} />
                </div>
            )}
        </div>
    );
}

function NotifierView({ result }: { result?: string }) {
    return (
        <div style={contentSectionStyle}>
            <div style={notifierSummaryStyle}>
                <h3 style={sectionTitleStyle}>审批结果摘要</h3>
                {result ? (
                    <div style={resultSummaryStyle}>
                        <span style={resultLabelStyle}>最终结果:</span>
                        <ResultBadge result={result} />
                    </div>
                ) : (
                    <div style={noDataStyle}>暂无结果信息</div>
                )}
            </div>
        </div>
    );
}

function FormDataDisplay({ data }: { data: Record<string, unknown> }) {
    const entries = Object.entries(data);
    if (entries.length === 0) return null;

    return (
        <div style={formDataTableStyle} role="table" aria-label="表单数据">
            {entries.map(([key, value]) => (
                <div key={key} style={formDataRowStyle} role="row">
                    <div style={formDataKeyStyle} role="cell">{key}</div>
                    <div style={formDataValueStyle} role="cell">{formatFieldValue(value)}</div>
                </div>
            ))}
        </div>
    );
}

function ApprovalDecisionList({ decisions }: { decisions: ApprovalDecision[] }) {
    return (
        <div style={decisionsListStyle} aria-label="审批决策列表">
            {decisions.map((decision, idx) => (
                <div key={idx} style={decisionItemStyle}>
                    <div style={decisionHeaderStyle}>
                        <span style={decisionApproverStyle}>{decision.approver}</span>
                        <DecisionBadge decision={decision.decision} />
                        <span style={decisionTimestampStyle}>
                            {formatTimestamp(decision.timestamp)}
                        </span>
                    </div>
                    {decision.rationale && (
                        <div style={decisionRationaleStyle}>
                            {decision.rationale}
                        </div>
                    )}
                </div>
            ))}
        </div>
    );
}

function ResultBadge({ result }: { result: string }) {
    const style = result === "approved"
        ? resultBadgeApprovedStyle
        : result === "rejected"
            ? resultBadgeRejectedStyle
            : resultBadgeDefaultStyle;
    const label = result === "approved" ? "通过" : result === "rejected" ? "驳回" : result;
    return <span style={style}>{label}</span>;
}

function DecisionBadge({ decision }: { decision: string }) {
    const style = decision === "approved"
        ? decisionBadgeApprovedStyle
        : decision === "rejected"
            ? decisionBadgeRejectedStyle
            : decisionBadgeDefaultStyle;
    const label = decision === "approved" ? "同意" : decision === "rejected" ? "拒绝" : decision;
    return <span style={style}>{label}</span>;
}

// --- Helpers ---

function formatTimestamp(isoStr: string): string {
    if (!isoStr) return "";
    try {
        const d = new Date(isoStr);
        const pad = (n: number) => String(n).padStart(2, "0");
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
    } catch {
        return isoStr;
    }
}

function formatFieldValue(value: unknown): string {
    if (value === null || value === undefined) return "-";
    if (typeof value === "object") return JSON.stringify(value);
    return String(value);
}

function getAutoCloseReasonLabel(reason: string): string {
    switch (reason) {
        case "notifier_timeout": return "知会确认超时";
        case "executor_timeout": return "执行确认超时";
        default: return reason;
    }
}

// --- Styles ---

const containerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "16px",
    padding: "16px",
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    maxWidth: "800px",
};

const statusBannerPendingStyle: CSSProperties = {
    padding: "12px 16px",
    borderRadius: "8px",
    background: "#fff3e0",
    border: "1px solid #ffe0b2",
};

const statusBannerConfirmedStyle: CSSProperties = {
    padding: "12px 16px",
    borderRadius: "8px",
    background: "#e8f5e9",
    border: "1px solid #c8e6c9",
};

const statusBannerAutoClosedStyle: CSSProperties = {
    padding: "12px 16px",
    borderRadius: "8px",
    background: "#f3e5f5",
    border: "1px solid #e1bee7",
};

const statusIconStyle: CSSProperties = {
    fontSize: "1rem",
    fontWeight: 600,
    marginBottom: "4px",
};

const statusDescStyle: CSSProperties = {
    fontSize: "0.82rem",
    color: "#666",
};

const statusTimestampStyle: CSSProperties = {
    fontSize: "0.78rem",
    color: "#757575",
    marginTop: "4px",
};

const statusNotesStyle: CSSProperties = {
    fontSize: "0.82rem",
    color: "#424242",
    marginTop: "8px",
    padding: "8px",
    background: "rgba(255,255,255,0.6)",
    borderRadius: "4px",
};

const statusNotesLabelStyle: CSSProperties = {
    fontWeight: 600,
    color: "#616161",
};

const statusReasonStyle: CSSProperties = {
    fontSize: "0.78rem",
    color: "#6a1b9a",
    marginTop: "4px",
};

const contentSectionStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "16px",
};

const sectionStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const sectionTitleStyle: CSSProperties = {
    fontSize: "0.88rem",
    fontWeight: 600,
    color: "#333",
    margin: 0,
    paddingBottom: "4px",
    borderBottom: "1px solid #eee",
};

const resultSummaryStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    padding: "8px 12px",
    background: "#f5f5f5",
    borderRadius: "6px",
};

const resultLabelStyle: CSSProperties = {
    fontSize: "0.82rem",
    fontWeight: 500,
    color: "#555",
};

const resultBadgeApprovedStyle: CSSProperties = {
    display: "inline-block",
    padding: "3px 10px",
    borderRadius: "12px",
    fontSize: "0.78rem",
    fontWeight: 600,
    background: "#e8f5e9",
    color: "#2e7d32",
};

const resultBadgeRejectedStyle: CSSProperties = {
    display: "inline-block",
    padding: "3px 10px",
    borderRadius: "12px",
    fontSize: "0.78rem",
    fontWeight: 600,
    background: "#fce4ec",
    color: "#c62828",
};

const resultBadgeDefaultStyle: CSSProperties = {
    display: "inline-block",
    padding: "3px 10px",
    borderRadius: "12px",
    fontSize: "0.78rem",
    fontWeight: 500,
    background: "#f5f5f5",
    color: "#616161",
};

const notifierSummaryStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "12px",
};

const noDataStyle: CSSProperties = {
    fontSize: "0.82rem",
    color: "#999",
    fontStyle: "italic",
};

const formDataTableStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    border: "1px solid #e0e0e0",
    borderRadius: "6px",
    overflow: "hidden",
};

const formDataRowStyle: CSSProperties = {
    display: "flex",
    borderBottom: "1px solid #f0f0f0",
};

const formDataKeyStyle: CSSProperties = {
    flex: "0 0 140px",
    padding: "8px 12px",
    fontSize: "0.78rem",
    fontWeight: 500,
    color: "#555",
    background: "#fafafa",
    borderRight: "1px solid #f0f0f0",
    wordBreak: "break-word",
};

const formDataValueStyle: CSSProperties = {
    flex: 1,
    padding: "8px 12px",
    fontSize: "0.82rem",
    color: "#333",
    wordBreak: "break-word",
};

const decisionsListStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const decisionItemStyle: CSSProperties = {
    padding: "10px 12px",
    border: "1px solid #e8e8e8",
    borderRadius: "6px",
    background: "#fafafa",
};

const decisionHeaderStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    flexWrap: "wrap",
};

const decisionApproverStyle: CSSProperties = {
    fontSize: "0.82rem",
    fontWeight: 600,
    color: "#333",
};

const decisionBadgeApprovedStyle: CSSProperties = {
    display: "inline-block",
    padding: "2px 8px",
    borderRadius: "10px",
    fontSize: "0.72rem",
    fontWeight: 500,
    background: "#e8f5e9",
    color: "#2e7d32",
};

const decisionBadgeRejectedStyle: CSSProperties = {
    display: "inline-block",
    padding: "2px 8px",
    borderRadius: "10px",
    fontSize: "0.72rem",
    fontWeight: 500,
    background: "#fce4ec",
    color: "#c62828",
};

const decisionBadgeDefaultStyle: CSSProperties = {
    display: "inline-block",
    padding: "2px 8px",
    borderRadius: "10px",
    fontSize: "0.72rem",
    fontWeight: 500,
    background: "#f5f5f5",
    color: "#616161",
};

const decisionTimestampStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#9e9e9e",
    marginLeft: "auto",
};

const decisionRationaleStyle: CSSProperties = {
    fontSize: "0.78rem",
    color: "#555",
    marginTop: "6px",
    paddingTop: "6px",
    borderTop: "1px solid #eee",
    lineHeight: 1.5,
};

const actionAreaStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "12px",
    padding: "16px",
    background: "#fafafa",
    borderRadius: "8px",
    border: "1px solid #e0e0e0",
};

const notesContainerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
};

const notesLabelStyle: CSSProperties = {
    fontSize: "0.78rem",
    fontWeight: 500,
    color: "#555",
};

const notesTextareaStyle: CSSProperties = {
    width: "100%",
    minHeight: "80px",
    padding: "10px 12px",
    fontSize: "0.82rem",
    border: "1px solid #ddd",
    borderRadius: "6px",
    resize: "vertical",
    fontFamily: "inherit",
    lineHeight: 1.5,
    outline: "none",
    boxSizing: "border-box",
};

const notesCounterStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#999",
    textAlign: "right",
};

const confirmBtnStyle: CSSProperties = {
    padding: "10px 24px",
    fontSize: "0.88rem",
    fontWeight: 600,
    border: "none",
    borderRadius: "6px",
    background: "#1565c0",
    color: "#fff",
    cursor: "pointer",
    transition: "background 0.15s",
    alignSelf: "flex-start",
};

const confirmBtnDisabledStyle: CSSProperties = {
    ...confirmBtnStyle,
    background: "#90caf9",
    cursor: "not-allowed",
    opacity: 0.7,
};

const errorStyle: CSSProperties = {
    fontSize: "0.82rem",
    color: "#c62828",
    padding: "8px 12px",
    background: "#fce4ec",
    borderRadius: "4px",
};

const successStyle: CSSProperties = {
    fontSize: "0.82rem",
    color: "#2e7d32",
    padding: "8px 12px",
    background: "#e8f5e9",
    borderRadius: "4px",
};
