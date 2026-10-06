import { useEffect, useId, useRef } from "react";
import { localizeText } from "./aiAssistantI18n";

/** Shape of the pending scope-approval request (matches the panel state). */
export interface ScopeApprovalRequest {
    id: string;
    tool: string;
    path: string;
    projectPath: string;
    directory: string;
    timeoutSeconds: number;
    kind: string;
    message: string;
    autoAllow: boolean;
    maintenance: boolean;
}

export type ScopeApprovalDecision = "allow_once" | "allow_dir" | "deny" | "full_access";

interface ScopeApprovalDialogProps {
    lang?: string;
    approval: ScopeApprovalRequest;
    isHighRisk: boolean;
    isRemoteHighRisk: boolean;
    isRemoteMaintenance: boolean;
    countdown: number;
    onResolve: (decision: ScopeApprovalDecision) => void;
}

function FactRow({ label, value }: { label: string; value: string }) {
    if (!value.trim()) return null;
    return (
        <div className="aap-scope-fact">
            <span className="aap-scope-fact-k">{label}</span>
            <span className="aap-scope-fact-v">{value}</span>
        </div>
    );
}

function ApprovalMark() {
    return (
        <span className="custom-dialog__mark custom-dialog__mark--info" aria-hidden="true">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
                <path d="M10 2.4 17.6 16.4H2.4L10 2.4Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
                <path d="M10 7.6v3.8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                <circle cx="10" cy="13.7" r="0.8" fill="currentColor" />
            </svg>
        </span>
    );
}

/** Modal approval dialog shown when CodingSubAgent/maintenance exceeds its scope. */
export function ScopeApprovalDialog({ lang, approval, isHighRisk, isRemoteHighRisk, isRemoteMaintenance, countdown, onResolve }: ScopeApprovalDialogProps) {
    const title = isHighRisk
        ? (isRemoteHighRisk
            ? localizeText(lang, "Remote Command Approval", "远程命令确认", "遠程命令確認")
            : localizeText(lang, "Command Approval", "命令确认", "命令確認"))
        : isRemoteMaintenance
            ? localizeText(lang, "Remote Maintenance Approval", "远程维护确认", "遠端維護確認")
            : localizeText(lang, "Scope Approval", "目录越权确认", "目錄越權確認");
    const lead = isHighRisk
        ? (isRemoteMaintenance
            ? localizeText(lang, "Remote maintenance is requesting a high-risk command:", "远程维护请求执行高风险命令：", "遠端維護請求執行高風險命令：")
            : isRemoteHighRisk
                ? localizeText(lang, "Remote CodingSubAgent is trying to run a blocked high-risk command:", "远程编码 SubAgent 尝试执行被拦截的高风险命令：", "遠程編碼 SubAgent 嘗試執行被攔截的高風險命令：")
                : localizeText(lang, "CodingSubAgent is trying to run a blocked high-risk command:", "编码 SubAgent 尝试执行被拦截的高风险命令：", "編碼 SubAgent 嘗試執行被攔截的高風險命令："))
        : isRemoteMaintenance
            ? localizeText(lang, "Remote maintenance is requesting access outside the project scope:", "远程维护请求访问项目范围外的路径：", "遠端維護請求存取專案範圍外的路徑：")
            : localizeText(lang, "CodingSubAgent is trying to access a path outside the project:", "编码 SubAgent 尝试访问项目目录外的路径：", "編碼 SubAgent 嘗試訪問項目目錄外的路徑：");
    const note = isHighRisk
        ? localizeText(lang, "Only approve this if you understand the command and trust its effect. Allow for this task skips later command prompts in the current task. If you do nothing, it will be rejected.", "仅在你理解该命令并信任其影响时放行。以后允许后，当前任务不再询问，直接放行。不操作将自动拒绝。", "僅在你理解該命令並信任其影響時放行。以後允許後，當前任務不再詢問，直接放行。不操作將自動拒絕。")
        : localizeText(lang, `Allow "${approval.directory}" for the rest of this task. Full Access is remembered for later tasks. If you do nothing, it will be rejected.`, `允许目录「${approval.directory}」在本任务中继续使用。完全访问会一直记住。不操作将自动拒绝。`, `允許目錄「${approval.directory}」在本任務中繼續使用。完全訪問會一直記住。不操作將自動拒絕。`);
    const alwaysLabel = isHighRisk
        ? localizeText(lang, "Allow for this task", "以后允许", "以後允許")
        : localizeText(lang, "Full Access", "完全访问", "完全訪問");
    const onceLabel = isHighRisk
        ? localizeText(lang, `Allow Once (${countdown}s)`, `本次放行 (${countdown}s)`, `本次放行 (${countdown}s)`)
        : localizeText(lang, `Allow Directory (${countdown}s)`, `允许该目录 (${countdown}s)`, `允許該目錄 (${countdown}s)`);
    const showFacts = [approval.tool, approval.path, approval.projectPath].some(value => value.trim() !== "");
    const titleId = useId();
    const leadId = useId();
    const factsId = useId();
    const noteId = useId();
    const dialogRef = useRef<HTMLDivElement>(null);
    const denyRef = useRef<HTMLButtonElement>(null);
    const onResolveRef = useRef(onResolve);
    const resolvedRef = useRef(false);
    onResolveRef.current = onResolve;
    const resolve = (decision: ScopeApprovalDecision) => {
        if (resolvedRef.current) return;
        resolvedRef.current = true;
        onResolveRef.current(decision);
    };

    useEffect(() => {
        const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        denyRef.current?.focus({ preventScroll: true });
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopImmediatePropagation();
                if (resolvedRef.current) return;
                resolvedRef.current = true;
                onResolveRef.current("deny");
                return;
            }
            if (event.key !== "Tab") return;
            const focusable = dialogRef.current?.querySelectorAll<HTMLElement>("button:not([disabled])");
            if (!focusable?.length) return;
            const items = Array.from(focusable);
            const current = items.indexOf(document.activeElement as HTMLElement);
            const next = event.shiftKey
                ? (current <= 0 ? items.length - 1 : current - 1)
                : (current < 0 || current === items.length - 1 ? 0 : current + 1);
            event.preventDefault();
            event.stopImmediatePropagation();
            items[next]?.focus({ preventScroll: true });
        };
        window.addEventListener("keydown", onKeyDown, true);
        return () => {
            window.removeEventListener("keydown", onKeyDown, true);
            if (previous?.isConnected) previous.focus({ preventScroll: true });
        };
    }, []);

    return (
        <div
            className="modal-backdrop custom-dialog-backdrop aap-scope-backdrop"
            data-testid="scope-approval-backdrop"
            onMouseDown={e => e.stopPropagation()}
            onClick={e => e.stopPropagation()}
        >
            <div
                ref={dialogRef}
                className="modal-content custom-dialog aap-scope-dialog"
                role="alertdialog"
                aria-modal="true"
                aria-labelledby={titleId}
                aria-describedby={[leadId, showFacts ? factsId : "", noteId].filter(Boolean).join(" ")}
                onMouseDown={e => e.stopPropagation()}
            >
                <div className="modal-header custom-dialog__header">
                    <div className="custom-dialog__heading">
                        <ApprovalMark />
                        <h3 className="custom-dialog__title" id={titleId}>{title}</h3>
                    </div>
                </div>
                <div className="modal-body custom-dialog__body">
                    <p className="custom-dialog__lead" id={leadId}>{lead}</p>
                    {showFacts && (
                    <div className="aap-scope-facts" id={factsId}>
                        <FactRow label={localizeText(lang, "Tool", "工具", "工具")} value={approval.tool} />
                        <FactRow label={isHighRisk ? localizeText(lang, "Command", "命令", "命令") : localizeText(lang, "Path", "路径", "路徑")} value={approval.path} />
                        <FactRow label={isHighRisk ? localizeText(lang, "Working dir", "工作目录", "工作目錄") : localizeText(lang, "Project", "项目范围", "項目範圍")} value={approval.projectPath} />
                    </div>
                    )}
                    <p className="custom-dialog__notice" id={noteId} role="note">{note}</p>
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button ref={denyRef} type="button" className="btn-secondary" onClick={() => resolve("deny")}>{localizeText(lang, "Deny", "拒绝", "拒絕")}</button>
                    <button type="button" className="btn-secondary aap-scope-always" onClick={() => resolve("full_access")}>{alwaysLabel}</button>
                    <button type="button" className="btn-primary" onClick={() => resolve(isHighRisk ? "allow_once" : "allow_dir")}>{onceLabel}</button>
                </div>
            </div>
        </div>
    );
}
