import { localizeText } from "./aiAssistantI18n";
import { type Theme } from "./aiAssistantPanelTheme";

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
    theme: Theme;
    approval: ScopeApprovalRequest;
    isHighRisk: boolean;
    isRemoteHighRisk: boolean;
    isRemoteMaintenance: boolean;
    countdown: number;
    onResolve: (decision: ScopeApprovalDecision) => void;
}

/** Modal approval dialog shown when CodingSubAgent/maintenance exceeds its scope. */
export function ScopeApprovalDialog({ lang, theme: t, approval, isHighRisk, isRemoteHighRisk, isRemoteMaintenance, countdown, onResolve }: ScopeApprovalDialogProps) {
    return (
        <div className="aap-scope-backdrop" data-testid="scope-approval-backdrop">
            <div role="alertdialog" aria-modal="true" aria-labelledby="scope-approval-title" style={{ width: 440, maxWidth: "calc(100vw - 32px)", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 14, boxShadow: (t.bg.startsWith("#0") || t.bg.startsWith("#1") || t.bg.startsWith("#2")) ? "0 1px 2px rgba(0, 0, 0, 0.30), 0 4px 12px -2px rgba(0, 0, 0, 0.40)" : "0 1px 2px rgba(30, 58, 95, 0.05), 0 4px 12px -2px rgba(30, 58, 95, 0.10)", color: t.text, overflow: "hidden" }} onMouseDown={e => e.stopPropagation()}>
                <div style={{ padding: "12px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                    <h3 className="aap-scope-title" id="scope-approval-title">{isHighRisk ? (isRemoteHighRisk ? localizeText(lang, "Remote Command Approval", "远程命令确认", "遠程命令確認") : localizeText(lang, "Command Approval", "命令确认", "命令確認")) : isRemoteMaintenance ? localizeText(lang, "Remote Maintenance Approval", "远程维护确认", "遠端維護確認") : localizeText(lang, "Scope Approval", "目录越权确认", "目錄越權確認")}</h3>
                </div>
                <div className="aap-scope-body">
                    <div className="aap-scope-gap">{isHighRisk ? (isRemoteMaintenance ? localizeText(lang, "Remote maintenance is requesting a high-risk command:", "远程维护请求执行高风险命令：", "遠端維護請求執行高風險命令：") : isRemoteHighRisk ? localizeText(lang, "Remote CodingSubAgent is trying to run a blocked high-risk command:", "远程编码 SubAgent 尝试执行被拦截的高风险命令：", "遠程編碼 SubAgent 嘗試執行被攔截的高風險命令：") : localizeText(lang, "CodingSubAgent is trying to run a blocked high-risk command:", "编码 SubAgent 尝试执行被拦截的高风险命令：", "編碼 SubAgent 嘗試執行被攔截的高風險命令：")) : isRemoteMaintenance ? localizeText(lang, "Remote maintenance is requesting access outside the project scope:", "远程维护请求访问项目范围外的路径：", "遠端維護請求存取專案範圍外的路徑：") : localizeText(lang, "CodingSubAgent is trying to access a path outside the project:", "编码 SubAgent 尝试访问项目目录外的路径：", "編碼 SubAgent 嘗試訪問項目目錄外的路徑：")}</div>
                    <div style={{ background: t.fieldBg, borderRadius: 4, padding: "6px 8px", fontSize: 12, fontFamily: "monospace", wordBreak: "break-all", marginBottom: 6 }}>
                        <div><strong>{localizeText(lang, "Tool", "工具", "工具")}:</strong> {approval.tool}</div>
                        <div><strong>{isHighRisk ? localizeText(lang, "Command", "命令", "命令") : localizeText(lang, "Path", "路径", "路徑")}:</strong> {approval.path}</div>
                        <div><strong>{isHighRisk ? localizeText(lang, "Working dir", "工作目录", "工作目錄") : localizeText(lang, "Project", "项目范围", "項目範圍")}:</strong> {approval.projectPath}</div>
                    </div>
                    <div style={{ fontSize: 12, color: t.textMuted }}>{isHighRisk ? localizeText(lang, "Only approve this if you understand the command and trust its effect. If you do nothing, it will be rejected.", "仅在你理解该命令并信任其影响时放行。不操作将自动拒绝。", "僅在你理解該命令並信任其影響時放行。不操作將自動拒絕。") : localizeText(lang, `Allow directory "${approval.directory}" for the remainder of this task?`, `允许目录「${approval.directory}」在本任务中后续操作？`, `允許目錄「${approval.directory}」在本任務中後續操作？`)}</div>
                </div>
                <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, padding: "10px 14px", borderTop: `1px solid ${t.titleBarBorder}` }}>
                    <button type="button" onClick={() => void onResolve("deny")} style={{ padding: "5px 14px", borderRadius: 4, border: `1px solid ${t.fieldBorder}`, background: "transparent", color: t.text, fontSize: 12, cursor: "pointer" }}>{localizeText(lang, "Deny", "拒绝", "拒絕")}</button>
                    <button type="button" onClick={() => void onResolve("full_access")} style={{ padding: "5px 14px", borderRadius: 4, border: `1px solid ${t.fieldBorder}`, background: "transparent", color: "var(--theme-success, #4f7f6f)", fontSize: 12, cursor: "pointer" }}>{isHighRisk ? localizeText(lang, "Allow Later", "以后放行", "以後放行") : localizeText(lang, "Full Access", "完全访问", "完全訪問")}</button>
                    <button type="button" onClick={() => void onResolve(isHighRisk ? "allow_once" : "allow_dir")} className="aap-scope-approve">{isHighRisk ? localizeText(lang, `Allow Once (${countdown}s)`, `本次放行 (${countdown}s)`, `本次放行 (${countdown}s)`) : localizeText(lang, `Allow Directory (${countdown}s)`, `允许该目录 (${countdown}s)`, `允許該目錄 (${countdown}s)`)}</button>
                </div>
            </div>
        </div>
    );
}
