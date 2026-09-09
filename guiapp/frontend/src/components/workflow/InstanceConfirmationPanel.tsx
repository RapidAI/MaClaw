import { useCallback, useState } from "react";
import type { CSSProperties } from "react";

// --- Types ---

export interface ApprovalDecision {
    approver_name: string;
    decision: "approved" | "rejected";
    rationale: string;
    decided_at: string;
}

export interface ConfirmationRecord {
    id: string;
    instance_id: string;
    recipient_id: string;
    recipient_name?: string;
    type: "executor" | "notifier";
    status: "pending" | "confirmed" | "auto_closed";
    notes?: string;
    confirmed_at?: string;
    auto_closed_at?: string;
    created_at: string;
}

export interface InstanceConfirmationData {
    instance_id: string;
    workflow_name: string;
    result: "approved" | "rejected";
    form_data?: Record<string, unknown>;
    approval_decisions?: ApprovalDecision[];
    confirmations: ConfirmationRecord[];
    current_user_id: string;
    current_user_role: "executor" | "notifier";
}

export interface InstanceConfirmationPanelProps {
    data: InstanceConfirmationData;
    /** Called when user confirms */
    onConfirm?: (confirmationId: string, notes: string) => Promise<void>;
}

// --- Constants ---

const MAX_NOTES_LENGTH = 2000;

// --- Component ---

export function InstanceConfirmationPanel({ data, onConfirm }: InstanceConfirmationPanelProps) {
    const { current_user_id, current_user_role } = data;

    // Find the current user's confirmation record
    const myConfirmation = data.confirmations.find(
        (c) => c.recipient_id === current_user_id && c.type === current_user_role
    );

    const isExecutor = current_user_role === "executor";

    return (
        <div style={containerStyle} role="region" aria-label="确认操作面板">
            {/* Header */}
            <div style={headerStyle}>
                <h3 style={headingStyle}>{data.workflow_name}</h3>
                <ResultBadge result={data.result} />
            </div>

            {/* Content based on role */}
            {isExecutor ? (
                <ExecutorView
                    data={data}
                    myConfirmation={myConfirmation}
                    onConfirm={onConfirm}
                />
            ) : (
                <NotifierView
                    data={data}
                    myConfirmation={myConfirmation}
                    onConfirm={onConfirm}
                />
            )}

            {/* All confirmations status */}
            <ConfirmationStatusList confirmations={data.confirmations} />
        </div>
    );
}

// --- Executor View ---

function ExecutorView({
    data,
    myConfirmation,
    onConfirm,
}: {
    data: InstanceConfirmationData;
    myConfirmation?: ConfirmationRecord;
    onConfirm?: (confirmationId: string, notes: string) => Promise<void>;
}) {
    const [notes, setNotes] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleConfirm = useCallback(async () => {
        if (!myConfirmation || !onConfirm) return;
        setSubmitting(true);
        setError(null);
        try {
            await onConfirm(myConfirmation.id, notes);
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : "确认失败");
        } finally {
            setSubmitting(false);
        }
    }, [myConfirmation, onConfirm, notes]);

    const isPending = myConfirmation?.status === "pending";

    return (
        <div style={viewContainerStyle}>
            {/* Form Data */}
            {data.form_data && (
                <section style={sectionStyle} aria-label="表单数据">
                    <h4 style={sectionHeadingStyle}>表单数据</h4>
                    <div style={formDataContainerStyle}>
                        {Object.entries(data.form_data).map(([key, value]) => (
                            <div key={key} style={formDataRowStyle}>
                                <span style={formDataKeyStyle}>{key}</span>
                                <span style={formDataValueStyle}>{String(value)}</span>
                            </div>
                        ))}
                    </div>
                </section>
            )}

            {/* Approval Decisions */}
            {data.approval_decisions && data.approval_decisions.length > 0 && (
                <section style={sectionStyle} aria-label="审批决策">
                    <h4 style={sectionHeadingStyle}>审批决策</h4>
                    <ul style={decisionListStyle}>
                        {data.approval_decisions.map((decision, idx) => (
                            <li key={idx} style={decisionItemStyle}>
                                <div style={decisionHeaderStyle}>
                                    <span style={decisionApproverStyle}>{decision.approver_name}</span>
                                    <DecisionBadge decision={decision.decision} />
                                    <span style={decisionDateStyle}>{formatDate(decision.decided_at)}</span>
                                </div>
                                {decision.rationale && (
                                    <p style={decisionRationaleStyle}>{decision.rationale}</p>
                                )}
                            </li>
                        ))}
                    </ul>
                </section>
            )}

            {/* Confirm action */}
            {isPending && (
                <section style={confirmSectionStyle} aria-label="确认操作">
                    <h4 style={sectionHeadingStyle}>确认已操作</h4>
                    <div style={notesContainerStyle}>
                        <label style={notesLabelStyle} htmlFor="executor-notes">
                            操作备注（可选，最多 {MAX_NOTES_LENGTH} 字）
                        </label>
                        <textarea
                            id="executor-notes"
                            value={notes}
                            onChange={(e) => setNotes(e.target.value.slice(0, MAX_NOTES_LENGTH))}
                            placeholder="描述已执行的操作..."
                            style={notesTextareaStyle}
                            maxLength={MAX_NOTES_LENGTH}
                            aria-describedby="notes-counter"
                        />
                        <span id="notes-counter" style={notesCounterStyle}>
                            {notes.length}/{MAX_NOTES_LENGTH}
                        </span>
                    </div>
                    {error && <p style={errorStyle} role="alert">{error}</p>}
                    <button
                        onClick={handleConfirm}
                        disabled={submitting}
                        style={submitting ? confirmButtonDisabledStyle : confirmButtonStyle}
                        aria-label="确认已操作"
                    >
                        {submitting ? "提交中..." : "确认已操作"}
                    </button>
                </section>
            )}
        </div>
    );
}

// --- Notifier View ---

function NotifierView({
    data,
    myConfirmation,
    onConfirm,
}: {
    data: InstanceConfirmationData;
    myConfirmation?: ConfirmationRecord;
    onConfirm?: (confirmationId: string, notes: string) => Promise<void>;
}) {
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleConfirm = useCallback(async () => {
        if (!myConfirmation || !onConfirm) return;
        setSubmitting(true);
        setError(null);
        try {
            await onConfirm(myConfirmation.id, "");
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : "确认失败");
        } finally {
            setSubmitting(false);
        }
    }, [myConfirmation, onConfirm]);

    const isPending = myConfirmation?.status === "pending";

    return (
        <div style={viewContainerStyle}>
            {/* Approval result summary */}
            <section style={sectionStyle} aria-label="审批结果摘要">
                <h4 style={sectionHeadingStyle}>审批结果摘要</h4>
                <div style={summaryBoxStyle}>
                    <div style={summaryRowStyle}>
                        <span style={summaryLabelStyle}>工作流</span>
                        <span style={summaryValueStyle}>{data.workflow_name}</span>
                    </div>
                    <div style={summaryRowStyle}>
                        <span style={summaryLabelStyle}>结果</span>
                        <ResultBadge result={data.result} />
                    </div>
                    {data.approval_decisions && data.approval_decisions.length > 0 && (
                        <div style={summaryRowStyle}>
                            <span style={summaryLabelStyle}>审批人数</span>
                            <span style={summaryValueStyle}>{data.approval_decisions.length} 人</span>
                        </div>
                    )}
                </div>
            </section>

            {/* Confirm acknowledgment */}
            {isPending && (
                <section style={confirmSectionStyle} aria-label="确认知会">
                    {error && <p style={errorStyle} role="alert">{error}</p>}
                    <button
                        onClick={handleConfirm}
                        disabled={submitting}
                        style={submitting ? confirmButtonDisabledStyle : confirmButtonNotifierStyle}
                        aria-label="确认已知会"
                    >
                        {submitting ? "提交中..." : "确认已知会"}
                    </button>
                </section>
            )}
        </div>
    );
}

// --- Confirmation Status List ---

function ConfirmationStatusList({ confirmations }: { confirmations: ConfirmationRecord[] }) {
    if (confirmations.length === 0) return null;

    return (
        <section style={sectionStyle} aria-label="确认状态列表">
            <h4 style={sectionHeadingStyle}>确认状态</h4>
            <ul style={statusListStyle}>
                {confirmations.map((conf) => (
                    <li key={conf.id} style={statusItemStyle}>
                        <div style={statusItemHeaderStyle}>
                            <span style={statusItemNameStyle}>
                                {conf.recipient_name || conf.recipient_id}
                            </span>
                            <span style={statusItemTypeStyle}>
                                {conf.type === "executor" ? "执行人" : "知会人"}
                            </span>
                            <ConfirmStatusBadge status={conf.status} />
                        </div>
                        <div style={statusItemDetailsStyle}>
                            {conf.status === "confirmed" && conf.confirmed_at && (
                                <span>确认时间: {formatDate(conf.confirmed_at)}</span>
                            )}
                            {conf.status === "auto_closed" && conf.auto_closed_at && (
                                <span>自动关闭: {formatDate(conf.auto_closed_at)}</span>
                            )}
                            {conf.status === "pending" && (
                                <span>等待确认中</span>
                            )}
                        </div>
                        {conf.notes && (
                            <p style={statusItemNotesStyle}>备注: {conf.notes}</p>
                        )}
                    </li>
                ))}
            </ul>
        </section>
    );
}

// --- Sub-components ---

function ResultBadge({ result }: { result: string }) {
    const isApproved = result === "approved";
    return (
        <span style={{
            ...badgeBaseStyle,
            background: isApproved ? "var(--theme-success-bg, rgba(79, 127, 111, 0.10))" : "var(--theme-danger-bg, #fbf1f0)",
            color: isApproved ? "var(--theme-success, #4f7f6f)" : "var(--theme-danger, #c43d34)",
        }}>
            {isApproved ? "通过" : "驳回"}
        </span>
    );
}

function DecisionBadge({ decision }: { decision: string }) {
    const isApproved = decision === "approved";
    return (
        <span style={{
            fontSize: "0.72rem",
            fontWeight: 500,
            color: isApproved ? "var(--theme-success, #4f7f6f)" : "var(--theme-danger, #c43d34)",
        }}>
            {isApproved ? "通过" : "驳回"}
        </span>
    );
}

function ConfirmStatusBadge({ status }: { status: string }) {
    const colors: Record<string, CSSProperties> = {
        pending: { background: "var(--theme-info-bg, #f3f7fb)", color: "var(--theme-primary, #2f5f98)" },
        confirmed: { background: "var(--theme-success-bg, #f3f7f5)", color: "var(--theme-success, #4f7f6f)" },
        auto_closed: { background: "var(--theme-surface-muted, #f7f8fa)", color: "var(--theme-text-muted, #64748b)" },
    };
    const labels: Record<string, string> = {
        pending: "待确认",
        confirmed: "已确认",
        auto_closed: "已自动关闭",
    };
    return (
        <span style={{ ...badgeBaseStyle, ...(colors[status] || {}) }}>
            {labels[status] || status}
        </span>
    );
}

// --- Helpers ---

function formatDate(isoStr: string): string {
    if (!isoStr) return "";
    try {
        const d = new Date(isoStr);
        const pad = (n: number) => String(n).padStart(2, "0");
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
    } catch {
        return isoStr;
    }
}

// --- Styles ---

const containerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "20px",
    padding: "20px",
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    fontSize: "0.85rem",
    maxWidth: "700px",
};

const headerStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "12px",
};

const headingStyle: CSSProperties = {
    margin: 0,
    fontSize: "1.1rem",
    fontWeight: 600,
    color: "var(--theme-text-primary, #1c2733)",
};

const badgeBaseStyle: CSSProperties = {
    display: "inline-block",
    padding: "3px 10px",
    borderRadius: "12px",
    fontSize: "0.75rem",
    fontWeight: 500,
    lineHeight: "1.4",
};

const viewContainerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "16px",
};

const sectionStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const sectionHeadingStyle: CSSProperties = {
    margin: 0,
    fontSize: "0.88rem",
    fontWeight: 600,
    color: "var(--theme-text-primary, #1c2733)",
};

const formDataContainerStyle: CSSProperties = {
    background: "var(--theme-surface-muted, #f7f8fa)",
    borderRadius: "6px",
    padding: "12px",
    border: "1px solid var(--theme-border-subtle, #edf2f7)",
};

const formDataRowStyle: CSSProperties = {
    display: "flex",
    justifyContent: "space-between",
    padding: "4px 0",
    borderBottom: "1px solid var(--theme-border-subtle, #edf2f7)",
};

const formDataKeyStyle: CSSProperties = {
    fontWeight: 500,
    color: "var(--theme-text-secondary, #44546a)",
    fontSize: "0.8rem",
};

const formDataValueStyle: CSSProperties = {
    color: "var(--theme-text-primary, #1c2733)",
    fontSize: "0.8rem",
};

const decisionListStyle: CSSProperties = {
    listStyle: "none",
    margin: 0,
    padding: 0,
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const decisionItemStyle: CSSProperties = {
    padding: "10px 12px",
    background: "var(--theme-surface-muted, #f7f8fa)",
    borderRadius: "6px",
    border: "1px solid var(--theme-border-subtle, #edf2f7)",
};

const decisionHeaderStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    marginBottom: "4px",
};

const decisionApproverStyle: CSSProperties = {
    fontWeight: 600,
    color: "var(--theme-text-primary, #1c2733)",
    fontSize: "0.82rem",
};

const decisionDateStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "var(--theme-text-muted, #657384)",
    marginLeft: "auto",
};

const decisionRationaleStyle: CSSProperties = {
    margin: "4px 0 0",
    fontSize: "0.78rem",
    color: "var(--theme-text-secondary, #44546a)",
    fontStyle: "italic",
};

const confirmSectionStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "10px",
    padding: "16px",
    background: "var(--theme-info-bg, #f3f7fb)",
    borderRadius: "8px",
    border: "1px solid var(--theme-primary-soft, #dce7f4)",
};

const notesContainerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
};

const notesLabelStyle: CSSProperties = {
    fontSize: "0.75rem",
    color: "var(--theme-text-secondary, #44546a)",
    fontWeight: 500,
};

const notesTextareaStyle: CSSProperties = {
    width: "100%",
    minHeight: "80px",
    padding: "8px 12px",
    fontSize: "0.82rem",
    border: "1px solid var(--theme-border, #d9e1ec)",
    borderRadius: "6px",
    outline: "none",
    resize: "vertical",
    boxSizing: "border-box",
    fontFamily: "inherit",
};

const notesCounterStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: "var(--theme-text-muted, #657384)",
    textAlign: "right",
};

const errorStyle: CSSProperties = {
    margin: 0,
    fontSize: "0.78rem",
    color: "var(--theme-danger, #c43d34)",
};

const confirmButtonStyle: CSSProperties = {
    padding: "10px 24px",
    fontSize: "0.85rem",
    fontWeight: 600,
    background: "var(--theme-primary, #2f5f98)",
    color: "#fff",
    border: "none",
    borderRadius: "6px",
    cursor: "pointer",
    alignSelf: "flex-start",
};

const confirmButtonDisabledStyle: CSSProperties = {
    ...confirmButtonStyle,
    opacity: 0.5,
    cursor: "not-allowed",
};

const confirmButtonNotifierStyle: CSSProperties = {
    ...confirmButtonStyle,
    background: "var(--theme-success, #4f7f6f)",
};

const summaryBoxStyle: CSSProperties = {
    background: "var(--theme-surface-muted, #f7f8fa)",
    borderRadius: "6px",
    padding: "12px",
    border: "1px solid var(--theme-border-subtle, #edf2f7)",
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const summaryRowStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "12px",
};

const summaryLabelStyle: CSSProperties = {
    fontSize: "0.78rem",
    color: "var(--theme-text-muted, #657384)",
    minWidth: "60px",
};

const summaryValueStyle: CSSProperties = {
    fontSize: "0.82rem",
    color: "var(--theme-text-primary, #1c2733)",
    fontWeight: 500,
};

const statusListStyle: CSSProperties = {
    listStyle: "none",
    margin: 0,
    padding: 0,
    display: "flex",
    flexDirection: "column",
    gap: "6px",
};

const statusItemStyle: CSSProperties = {
    padding: "8px 12px",
    background: "var(--theme-surface-muted, #f7f8fa)",
    borderRadius: "6px",
    border: "1px solid var(--theme-border-subtle, #edf2f7)",
};

const statusItemHeaderStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    marginBottom: "4px",
};

const statusItemNameStyle: CSSProperties = {
    fontWeight: 500,
    color: "var(--theme-text-primary, #1c2733)",
    fontSize: "0.8rem",
};

const statusItemTypeStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: "var(--theme-text-muted, #657384)",
    background: "var(--theme-surface, #ffffff)",
    padding: "1px 6px",
    borderRadius: "8px",
};

const statusItemDetailsStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "var(--theme-text-muted, #657384)",
};

const statusItemNotesStyle: CSSProperties = {
    margin: "4px 0 0",
    fontSize: "0.75rem",
    color: "var(--theme-text-secondary, #44546a)",
    fontStyle: "italic",
};
