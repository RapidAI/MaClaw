/**
 * 新任务引导页配置条（TaskConfigBar）共享的草稿类型与纯函数。
 *
 * 语义对齐 docs/design/new-task-wizard-design-zh.md：
 * - expertId = null 表示「通用专家」（不指定领域专家，系统通用能力处理）
 * - workflowTemplateId = null 表示「无」（默认，不执行工作流）
 * - workflowTemplateId = WORKFLOW_AUTO（'auto'）表示「自动判断」（显式选择；
 *   发送后建任务但模板 id 为空，仍由语义拦截匹配工作流）
 * - workspace.kind = 'local' 且 localPath 为空 = 默认工作目录
 * - 专家 × 工作流互斥（§7）：工作流选择 ≠ 无（含「自动判断」）即锁定专家；
 *   指定专家则工作流回到「无」
 * - 专家 × 工作空间：云端/远程不可用并归约为本地；本地目录会写入任务并作为执行目录
 */

import { isLatexExpertId, LATEX_BLANK_TEMPLATE_ID } from "../../../utils/latexTemplates";

/** 「自动判断」哨兵值：发送后由系统语义拦截匹配工作流。 */
export const WORKFLOW_AUTO = 'auto';

export type WorkspaceKind = 'local' | 'cloud' | 'remote';

export interface RemoteTarget {
    host: string;
    port: number;
    user: string;
    password: string;
    workDir: string;
}

export interface WorkspaceTarget {
    kind: WorkspaceKind;
    /** 本地目录；空 / 未设置 = 默认工作目录 */
    localPath?: string;
    cloudWorkspaceId?: string;
    cloudName?: string;
    remote?: RemoteTarget;
}

export type TaskType = 'chat' | 'coding';

export interface TaskDraft {
    name: string;
    taskType: TaskType;
    /** null = 通用专家（默认） */
    expertId: string | null;
    expertName: string | null;
    /** null = 无（默认，不执行工作流）；WORKFLOW_AUTO = 自动判断（语义拦截）；其余 = 模板 id */
    workflowTemplateId: string | null;
    workspace: WorkspaceTarget;
    workflowParams: Record<string, string>;
    /** LaTeX 论文模板。仅在专家为内置 LaTeX 专家时有效；blank / 空 = 空白模板。 */
    latexTemplateId: string | null;
    latexTemplateName: string | null;
}

/** 从模板库、工作流页或专家卡片带入新建任务页的预填。 */
export interface NewTaskWizardSeed {
    expertId?: string | null;
    expertName?: string | null;
    workflowTemplateId?: string | null;
    latexTemplateId?: string | null;
    latexTemplateName?: string | null;
}

export function defaultTaskDraft(): TaskDraft {
    return {
        name: '',
        taskType: 'chat',
        expertId: null,
        expertName: null,
        workflowTemplateId: null,
        workspace: { kind: 'local' },
        workflowParams: {},
        latexTemplateId: null,
        latexTemplateName: null,
    };
}

export function isExpertSpecified(draft: TaskDraft): boolean {
    return !!draft.expertId;
}

/** 工作流是否做出了非「无」的选择（含「自动判断」）——chip 高亮用。 */
export function isWorkflowChosen(draft: TaskDraft): boolean {
    return draft.workflowTemplateId !== null;
}

/**
 * 未选工作流（= 无）时才允许指定专家（互斥，§7）。
 * 「自动判断」也算指定了工作流行为，同样锁定专家。
 */
export function canSpecifyExpert(draft: TaskDraft): boolean {
    return draft.workflowTemplateId === null;
}

/** 未指定专家时才允许选择工作流（含「自动判断」）（互斥，§7）。 */
export function canSpecifyWorkflow(draft: TaskDraft): boolean {
    return !isExpertSpecified(draft);
}

/** 专家还要再选一项参数（目前只有 LaTeX 模板）。此时工作流条让位给该参数。 */
export function expertNeedsSetup(draft: TaskDraft): boolean {
    return isLatexExpertId(draft.expertId);
}

/**
 * 全默认（零配置）= 旧行为：会话 / 通用专家 / 无工作流 / 本地默认目录。
 * 「自动判断」是显式选择，不算全默认——发送时经 CreateTaskUnified 建任务，
 * 但模板 id 仍映射为空（语义拦截不变，见 taskConfigSend）。
 */
export function isDraftDefault(draft: TaskDraft): boolean {
    return draft.taskType === 'chat'
        && !isExpertSpecified(draft)
        && !isWorkflowChosen(draft)
        && draft.workspace.kind === 'local'
        && !draft.workspace.localPath;
}

/** 指定专家（id = null 表示回到通用专家）；指定专家把工作流回到「无」，
 * 且云端/远程工作空间归约为本地默认（专家任务不携带工作空间，§7）。 */
export function withExpert(draft: TaskDraft, id: string | null, name: string | null): TaskDraft {
    const latex = !!(id && isLatexExpertId(id));
    const keepTemplate = latex && isLatexExpertId(draft.expertId);
    const next: TaskDraft = {
        ...draft,
        expertId: id,
        expertName: name,
        workflowTemplateId: null,
        workflowParams: id ? {} : draft.workflowParams,
        latexTemplateId: latex
            ? (keepTemplate ? (draft.latexTemplateId || LATEX_BLANK_TEMPLATE_ID) : LATEX_BLANK_TEMPLATE_ID)
            : null,
        latexTemplateName: keepTemplate ? (draft.latexTemplateName || null) : null,
    };
    if (id && next.workspace.kind !== 'local') {
        next.workspace = { kind: 'local' };
    }
    return next;
}

/** 选择 LaTeX 模板。空 id 归约为空白模板。 */
export function withLatexTemplate(draft: TaskDraft, id: string | null, name: string | null): TaskDraft {
    const templateId = String(id || "").trim() || LATEX_BLANK_TEMPLATE_ID;
    const blank = templateId === LATEX_BLANK_TEMPLATE_ID;
    return {
        ...draft,
        latexTemplateId: templateId,
        latexTemplateName: blank ? null : (String(name || "").trim() || null),
    };
}

/** 模板库 / 工作流 / 专家卡片点进来时的草稿。专家与工作流仍互斥，专家优先。 */
export function draftFromWizardSeed(seed: NewTaskWizardSeed | null | undefined): TaskDraft {
    const expertId = String(seed?.expertId || "").trim();
    const workflowId = String(seed?.workflowTemplateId || "").trim();
    if (expertId) {
        let draft = withExpert(defaultTaskDraft(), expertId, String(seed?.expertName || "").trim() || expertId);
        if (isLatexExpertId(expertId)) {
            draft = withLatexTemplate(draft, seed?.latexTemplateId || LATEX_BLANK_TEMPLATE_ID, seed?.latexTemplateName || null);
        }
        return draft;
    }
    if (workflowId) return withWorkflow(defaultTaskDraft(), workflowId);
    return defaultTaskDraft();
}

/**
 * 当前 draft 下可用的工作空间位置（§7）：指定专家时专家任务不携带工作空间，
 * 仅本地可用；通用专家时三种位置皆可。
 */
export function workspaceKindsForExpert(draft: TaskDraft): WorkspaceKind[] {
    return isExpertSpecified(draft) ? ['local'] : ['local', 'cloud', 'remote'];
}

/**
 * 指定工作流（id = null 表示「无」；WORKFLOW_AUTO 表示「自动判断」）。
 * 任何 ≠ 无 的选择都会强制专家回到「通用专家」并清空参数槽
 * （参数由父级在参数弹窗中重新回填）。
 */
export function withWorkflow(draft: TaskDraft, id: string | null): TaskDraft {
    if (id === null) {
        return {
            ...draft,
            workflowTemplateId: null,
            workflowParams: {},
        };
    }
    return {
        ...draft,
        workflowTemplateId: id,
        workflowParams: {},
        expertId: null,
        expertName: null,
        latexTemplateId: null,
        latexTemplateName: null,
    };
}

export function withWorkspace(draft: TaskDraft, workspace: WorkspaceTarget): TaskDraft {
    return { ...draft, workspace };
}

/** 本地目录；path 为空 = 默认工作目录。 */
export function withLocalWorkspace(draft: TaskDraft, path: string): TaskDraft {
    return withWorkspace(draft, { kind: 'local', localPath: path.trim() || undefined });
}

export function withCloudWorkspace(draft: TaskDraft, id: string, name: string): TaskDraft {
    return withWorkspace(draft, { kind: 'cloud', cloudWorkspaceId: id, cloudName: name });
}

export function withRemoteWorkspace(draft: TaskDraft, remote: RemoteTarget): TaskDraft {
    return withWorkspace(draft, { kind: 'remote', remote });
}

/** 切换任务类型；编程类型下云端位置自动退回本地默认（§7.2）。 */
export function withTaskType(draft: TaskDraft, taskType: TaskType): TaskDraft {
    return normalizeDraftForTaskType({ ...draft, taskType });
}

/** 编程类型下云端位置不可用；从云端退回本地默认。 */
export function normalizeDraftForTaskType(draft: TaskDraft): TaskDraft {
    if (draft.taskType === 'coding' && draft.workspace.kind === 'cloud') {
        return withWorkspace(draft, { kind: 'local' });
    }
    return draft;
}

/** 编程 + 本地且未选目录 → 需要用户补目录（chip 高亮「请选择目录」）。 */
export function needsLocalPath(draft: TaskDraft): boolean {
    return draft.taskType === 'coding'
        && draft.workspace.kind === 'local'
        && !draft.workspace.localPath;
}

/** 工作空间 chip 上显示的摘要值。 */
export function workspaceSummaryLabel(draft: TaskDraft, isZh = true): string {
    const ws = draft.workspace;
    if (ws.kind === 'local') {
        if (ws.localPath) return ws.localPath;
        return needsLocalPath(draft)
            ? (isZh ? '请选择目录' : 'Select directory')
            : (isZh ? '默认' : 'Default');
    }
    if (ws.kind === 'cloud') return ws.cloudName || (isZh ? '云端工作区' : 'Cloud workspace');
    return ws.remote?.host || (isZh ? '远程服务器' : 'Remote server');
}
