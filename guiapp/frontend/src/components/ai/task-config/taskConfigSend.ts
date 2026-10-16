/**
 * 新任务配置条（TaskConfigBar）发送拦截的纯逻辑：
 * draft → TaskCreateOptions 映射 + 创建/导航编排。
 *
 * 语义对齐 docs/design/new-task-wizard-design-zh.md §4/§6：
 * - 全默认 draft（isDraftDefault）绝不进入本路径，保持旧行为（硬要求）。
 * - 专家分支由后端 CreateExpertTask 处理，前端随后打开专家页签并把
 *   输入文本作为首条消息发送（后端契约：专家/工作流分支依赖首消息触发）。
 * - 其余分支打开新任务页签并 autoSend 首条消息；工作流 = 「无」时首条消息
 *   带 no_workflow_interception 跳过语义拦截，「自动判断」保持现状语义拦截。
 */
import { describeTaskTitle } from "../describeTaskTitle";
import { isLatexExpertId, LATEX_BLANK_TEMPLATE_ID } from "../../../utils/latexTemplates";
import { isDraftDefault, WORKFLOW_AUTO, type RemoteTarget, type TaskDraft } from "./taskDraft";

/** 与 wailsjs main.TaskCreateOptions 字段对齐（普通对象，直接可序列化）。 */
export interface UnifiedTaskCreateOptions {
    name: string;
    mode: string;
    workingDir: string;
    remote?: RemoteTarget;
    cloudWorkspaceId: string;
    expertId: string;
    expertName: string;
    workflowTemplateId: string;
    params?: Record<string, string>;
}

/** 打开新任务页签并自动发送首条消息的导航载荷（EVENT_OPEN_TASK_LAUNCH）。 */
export interface TaskLaunchNavigation {
    projectPath: string;
    taskTitle: string;
    initialMessage: string;
    agentMode?: "coding_dev" | "remote_coding_dev";
    cloudWorkspaceId?: string;
    remoteHost?: string;
    remoteSafety?: "diagnosis";
    /**
     * 远程环境武装失败（CreateTaskUnified 返回 warning）时为 true：打开页签
     * 后首条消息延迟到 SSH 重连成功再发（复用既有 remoteNeedsReconnect 链路）。
     */
    remoteNeedsReconnect?: boolean;
    /**
     * 创建后次级步骤（工作流启动/远程武装）失败的降级说明，展示在新页签内。
     */
    warning?: string;
    /**
     * 工作流 = 「无」：首条消息带 no_workflow_interception，跳过工作流
     * 语义拦截/启动（additive；不带该键的消息行为不变）。
     */
    noWorkflowInterception?: boolean;
}

/** 是否需要走 CreateTaskUnified 路径（false = 原路径一行不动）。 */
export function shouldCreateUnifiedTask(draft: TaskDraft): boolean {
    return !isDraftDefault(draft);
}

/**
 * draft → TaskCreateOptions（设计 §4 映射）：
 * 会话×本地→chat、会话×云端→cloud、编程×本地→coding_dev、
 * 本地/远程（含会话×远程，走远程 SSH 链路）→remote_coding_dev。
 *
 * 工作流维度：null（无）→ 空模板 id + 首条消息跳过语义拦截；
 * WORKFLOW_AUTO（自动判断）→ 空模板 id（现状语义拦截）；模板 id → 直传。
 */
/** Short rewritten description of the first instruction, not the raw command. */
export function shortTaskLaunchTitle(text: string): string {
    return describeTaskTitle(text);
}

export function draftToTaskCreateOptions(text: string, draft: TaskDraft): UnifiedTaskCreateOptions {
    const name = describeTaskTitle(text) || (text || "").trim();
    const workspace = draft.workspace;
    const workflowId = (draft.workflowTemplateId || "").trim();
    const base: UnifiedTaskCreateOptions = {
        name,
        mode: "chat",
        workingDir: "",
        cloudWorkspaceId: "",
        expertId: (draft.expertId || "").trim(),
        expertName: (draft.expertName || "").trim(),
        workflowTemplateId: workflowId === WORKFLOW_AUTO ? "" : workflowId,
    };
    if (base.workflowTemplateId) {
        // v1：参数槽弹窗未接入，带空参数对象创建，缺槽位由后端 SubmitForm 兜底。
        base.params = { ...draft.workflowParams };
    }
    if (workspace.kind === "cloud") {
        return { ...base, mode: "cloud", cloudWorkspaceId: (workspace.cloudWorkspaceId || "").trim() };
    }
    if (workspace.kind === "remote" && workspace.remote) {
        return {
            ...base,
            mode: "remote_coding_dev",
            remote: {
                host: workspace.remote.host,
                port: workspace.remote.port,
                user: workspace.remote.user,
                password: workspace.remote.password,
                workDir: workspace.remote.workDir,
            },
        };
    }
    const localPath = (workspace.localPath || "").trim();
    if (draft.taskType === "coding") {
        return { ...base, mode: "coding_dev", workingDir: localPath };
    }
    return { ...base, mode: "chat", workingDir: localPath };
}

/** 成功后专家分支的导航参数。 */
export interface ExpertNavigation {
    id: string;
    name: string;
    description?: string;
    initialMessage: string;
    /** Set when a LaTeX document was already written, so the tab reopens that file. */
    latexDocument?: { relativePath: string };
    /** LaTeX task directory. Used to roll the paper back if the tab never opens. */
    projectPath?: string;
}

export interface LatexDocumentPrep {
    relativePath: string;
    initialMessage: string;
}

export interface TaskConfigSendBindings {
    /**
     * CreateTaskUnified 包装。硬失败 reject；次级步骤失败（工作流启动/远程
     * 武装）时 resolve {projectPath, warning}——任务已建、可打开、已降级。
     */
    createTaskUnified: (opts: UnifiedTaskCreateOptions) => Promise<{ projectPath: string; warning?: string }>;
    /** coding_dev 本地任务的后端统一创建不预置环境，这里补一次武装（幂等）。 */
    ensureCodingArmed?: (projectPath: string) => Promise<unknown>;
    openTaskLaunch: (nav: TaskLaunchNavigation) => void;
    openExpert: (nav: ExpertNavigation) => void;
    /**
     * LaTeX expert: write the chosen template into the task workspace before
     * the expert tab opens. Absent in tests that do not exercise LaTeX.
     */
    materializeLatexDocument?: (args: {
        projectPath: string;
        templateId: string;
        templateName: string;
        userText: string;
    }) => Promise<LatexDocumentPrep | null>;
    /**
     * Drop a LaTeX paper that was recorded but never opened, and put the
     * shared expert transcript back on the previous paper.
     */
    abandonFreshLatexTask?: (projectPath: string) => Promise<unknown>;
}

export type TaskConfigSendResult =
    | { ok: true; warning?: string }
    | { ok: false; error: string };

function errorMessage(err: unknown): string {
    if (err instanceof Error) return err.message || String(err);
    return String(err || "");
}

async function abandonFreshLatexPaper(bindings: TaskConfigSendBindings, projectPath: string): Promise<void> {
    if (!bindings.abandonFreshLatexTask) return;
    try {
        await bindings.abandonFreshLatexTask(projectPath);
    } catch (err) {
        console.warn("[task-config] abandon unopened latex paper failed", err);
    }
}

/**
 * 非默认 draft 的完整发送流程：创建任务 → 打开页签 → 首条消息。
 * 成功 resolve { ok: true }；任何一步失败 resolve { ok: false, error }，
 * 由调用方决定是否展示错误并保留输入。
 *
 * force（新建任务向导页签）：即使 draft 全默认也创建任务——
 * TaskCreateOptions 只有 Name（+ chat 模式），创建后走 EVENT_OPEN_TASK_LAUNCH。
 */
export async function runTaskConfigSend(args: {
    text: string;
    draft: TaskDraft;
    bindings: TaskConfigSendBindings;
    isZh?: boolean;
    force?: boolean;
    /** First message on the new task. Defaults to `text`. Attachments stay out of the task title. */
    initialMessage?: string;
}): Promise<TaskConfigSendResult> {
    const { text, draft, bindings } = args;
    const isZh = args.isZh !== false;
    const trimmed = (text || "").trim();
    const initialMessage = (args.initialMessage || "").trim() || trimmed;
    if (!trimmed) return { ok: false, error: isZh ? "请先输入任务内容" : "Enter a task first" };
    if (!shouldCreateUnifiedTask(draft) && args.force !== true) return { ok: true };
    const opts = draftToTaskCreateOptions(trimmed, draft);
    let projectPath = "";
    let warning = "";
    try {
        const created = await bindings.createTaskUnified(opts);
        projectPath = (created.projectPath || "").trim();
        warning = (created.warning || "").trim();
    } catch (err) {
        return { ok: false, error: errorMessage(err) || (isZh ? "创建任务失败" : "Failed to create task") };
    }
    if (!projectPath) {
        return { ok: false, error: isZh ? "创建任务失败：未返回任务路径" : "Failed to create task: no task path returned" };
    }
    if (opts.mode === "coding_dev" && bindings.ensureCodingArmed) {
        try {
            await bindings.ensureCodingArmed(projectPath);
        } catch (err) {
            // 武装失败不阻断：任务记录已创建，打开后由重连/准备链路兜底。
            console.warn("[task-config] EnsureCodingWorkbenchArmed failed", err);
        }
    }
    if (opts.expertId) {
        let expertMessage = initialMessage;
        let latexDocument: ExpertNavigation["latexDocument"];
        if (isLatexExpertId(opts.expertId) && bindings.materializeLatexDocument) {
            let prepared: LatexDocumentPrep | null = null;
            try {
                prepared = await bindings.materializeLatexDocument({
                    projectPath,
                    templateId: (draft.latexTemplateId || "").trim() || LATEX_BLANK_TEMPLATE_ID,
                    templateName: (draft.latexTemplateName || "").trim(),
                    userText: initialMessage,
                });
            } catch (err) {
                await abandonFreshLatexPaper(bindings, projectPath);
                return { ok: false, error: errorMessage(err) || (isZh ? "无法创建 LaTeX 文档" : "The LaTeX document could not be created") };
            }
            if (!prepared?.relativePath) {
                await abandonFreshLatexPaper(bindings, projectPath);
                return { ok: false, error: isZh ? "无法创建 LaTeX 文档" : "The LaTeX document could not be created" };
            }
            expertMessage = prepared.initialMessage || initialMessage;
            latexDocument = { relativePath: prepared.relativePath };
        }
        bindings.openExpert({
            id: opts.expertId,
            name: opts.expertName || opts.expertId,
            initialMessage: expertMessage,
            ...(isLatexExpertId(opts.expertId) ? { projectPath } : {}),
            ...(latexDocument ? { latexDocument } : {}),
        });
    } else {
        bindings.openTaskLaunch({
            projectPath,
            // describeTaskTitle returns "" for raw commands it cannot rewrite;
            // fall back to the first line so the tab shows the instruction
            // instead of the project path until the backend's LLM title
            // (project-task:renamed) arrives.
            taskTitle: shortTaskLaunchTitle(trimmed) || trimmed.split(/\r?\n/)[0].slice(0, 40),
            initialMessage,
            ...(opts.mode === "coding_dev" || opts.mode === "remote_coding_dev"
                ? { agentMode: opts.mode as TaskLaunchNavigation["agentMode"] }
                : {}),
            ...(opts.mode === "cloud" ? { cloudWorkspaceId: opts.cloudWorkspaceId } : {}),
            ...(opts.remote ? { remoteHost: opts.remote.host } : {}),
            // 远程武装失败：首条消息延迟到 SSH 重连成功再发。
            ...(opts.mode === "remote_coding_dev" && warning ? { remoteNeedsReconnect: true } : {}),
            ...(warning ? { warning } : {}),
            // 工作流 = 「无」时首条消息必须跳过语义拦截；「自动判断」与零配置
            // 行为一致（现状语义拦截），不带该键。
            ...(draft.workflowTemplateId === null ? { noWorkflowInterception: true } : {}),
        });
    }
    return { ok: true, ...(warning ? { warning } : {}) };
}
