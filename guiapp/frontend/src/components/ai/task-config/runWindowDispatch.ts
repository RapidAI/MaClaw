import {
    AbandonUnopenedFreshLatexTask,
    ClaimRunWindowTask,
    CreateTaskUnified,
    EnsureCodingWorkbenchArmed,
    ListRunWindowTasks,
    MarkRunWindowTaskFailed,
    MarkRunWindowTaskStarted,
    ReleaseRunWindowTask,
    SetTabWorkingDir,
} from "../../../../wailsjs/go/main/App";
import { EVENT_OPEN_TASK_LAUNCH, type OpenTaskLaunchDetail } from "../../../constants/events";
import {
    createLatexDocumentForTask,
    LATEX_EXPERT_ID,
    latexPaperTaskMessage,
} from "../../../utils/latexTemplates";
import { openExpertConversation } from "../../../utils/expertConversationNavigation";
import { builtinExpertIcon, expertTabId } from "../expertTypes";
import { startCloudAgentFromNewTask } from "../../layout/cloudAgentAssistant";
import { runTaskConfigSend } from "./taskConfigSend";
import { isRunWindowDue, taskDraftFromRunWindow, type RunWindowTaskRecord } from "./runWindow";
import { tailText } from "../../../i18n/mnTail"

function callable<T extends (...args: never[]) => unknown>(fn: T | undefined): fn is T {
    return typeof fn === "function";
}

let runWindowDispatchTail: Promise<void> = Promise.resolve();
let runWindowDispatchQueued = false;
let runWindowDispatchLaunch: ((task: RunWindowTaskRecord) => Promise<RunWindowLaunchResult>) | null = null;
let runWindowDispatchNow = new Date();
const runWindowRetryAfter = new Map<string, number>();
const runWindowRetryGapMs = 60_000;

/** A string is the created task identity. An object can name that identity and why the turn was not accepted. */
export type RunWindowLaunchResult = string | { projectPath: string; error?: string };

function launchIdentity(value: RunWindowLaunchResult | null | undefined): { projectPath: string; error: string } {
    if (typeof value === 'string' || value == null) {
        return { projectPath: String(value || '').trim(), error: '' };
    }
    return {
        projectPath: String(value.projectPath || '').trim(),
        error: String(value.error || '').trim(),
    };
}

export function dispatchDueRunWindows(launch: (task: RunWindowTaskRecord) => Promise<RunWindowLaunchResult>, now = new Date()): Promise<void> {
    runWindowDispatchLaunch = launch;
    runWindowDispatchNow = now;
    if (runWindowDispatchQueued) return runWindowDispatchTail;
    runWindowDispatchQueued = true;
    const run = runWindowDispatchTail.then(async () => {
        runWindowDispatchQueued = false;
        const launchNow = runWindowDispatchLaunch;
        const at = runWindowDispatchNow;
        runWindowDispatchLaunch = null;
        if (!launchNow) return;
        await dispatchDueRunWindowsOnce(launchNow, at);
    });
    runWindowDispatchTail = run.then(() => undefined, () => undefined);
    return run;
}

async function dispatchDueRunWindowsOnce(launch: (task: RunWindowTaskRecord) => Promise<RunWindowLaunchResult>, now = new Date()): Promise<void> {
    if (!callable(ListRunWindowTasks) || !callable(ClaimRunWindowTask)) return;
    const listed = await ListRunWindowTasks();
    const tasks = (Array.isArray(listed) ? listed : []) as RunWindowTaskRecord[];
    for (const task of tasks) {
        if (!isRunWindowDue(task, now)) continue;
        // A saved edit clears the error and should run on the next check.
        // Until then, a failed create waits out the gap instead of relaunching
        // on every poll.
        if (task.lastError && (runWindowRetryAfter.get(task.id) || 0) > now.getTime()) continue;
        let claimed: RunWindowTaskRecord;
        try {
            claimed = await ClaimRunWindowTask(task.id) as RunWindowTaskRecord;
        } catch {
            continue;
        }
        let projectPath = "";
        try {
            const launched = launchIdentity(await launch(claimed));
            projectPath = launched.projectPath;
            // The identity is known before this throw, so a turn that was not
            // accepted fails the row instead of being launched again.
            if (launched.error) throw new Error(launched.error);
            if (!projectPath) throw new Error("未返回任务路径");
            if (!callable(MarkRunWindowTaskStarted)) throw new Error("无法记录任务启动");
            await MarkRunWindowTaskStarted(claimed.id, projectPath);
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err || "启动失败");
            if (!projectPath) {
                // Nothing was created. Put the row back and wait briefly before
                // the next try, so a lasting failure does not relaunch every poll.
                runWindowRetryAfter.set(claimed.id, now.getTime() + runWindowRetryGapMs);
                if (callable(ReleaseRunWindowTask)) {
                    try { await ReleaseRunWindowTask(claimed.id, message); } catch { /* the row may already be gone */ }
                }
                continue;
            }
            if (callable(MarkRunWindowTaskFailed)) {
                try {
                    await MarkRunWindowTaskFailed(claimed.id, message);
                } catch {
                    if (callable(ReleaseRunWindowTask)) {
                        try { await ReleaseRunWindowTask(claimed.id, message); } catch { /* the row may already be gone */ }
                    }
                }
            }
        }
    }
}

/** Replay a scheduled new-task through the same create-and-open path as an immediate send. */
export async function launchRunWindowTask(task: RunWindowTaskRecord, lang = "zh-Hans"): Promise<RunWindowLaunchResult> {
    const draft = taskDraftFromRunWindow(task);
    const isZh = lang.startsWith("zh");
    let projectPath = "";
    const result = await runTaskConfigSend({
        text: task.prompt,
        initialMessage: task.initialMessage || task.prompt,
        draft,
        force: true,
        isZh,
        lang,
        bindings: {
            createTaskUnified: async (opts) => {
                // The next scheduled paper is allocated before its turn can
                // start. Cutting the shared session over here would cancel the
                // paper that is still running. The open path clears it later.
                const created = await CreateTaskUnified({ ...opts, keepExpertSession: true } as unknown as Parameters<typeof CreateTaskUnified>[0]);
                projectPath = String(created?.projectPath || "").trim();
                return { projectPath, warning: created?.warning, ownConversation: created?.ownConversation === true };
            },
            abandonFreshLatexTask: (path) => AbandonUnopenedFreshLatexTask(path),
            ensureCodingArmed: (path) => EnsureCodingWorkbenchArmed(path),
            openTaskLaunch: (nav) => {
                projectPath = String(nav.projectPath || projectPath).trim();
                const detail: OpenTaskLaunchDetail = {
                    projectPath: nav.projectPath,
                    taskTitle: nav.taskTitle,
                    initialMessage: nav.initialMessage,
                    agentMode: nav.agentMode,
                    cloudWorkspaceId: nav.cloudWorkspaceId,
                    fileWorkspaceId: nav.fileWorkspaceId,
                    remoteHost: nav.remoteHost,
                    remoteSafety: nav.remoteSafety,
                    remoteNeedsReconnect: nav.remoteNeedsReconnect,
                    warning: nav.warning,
                    noWorkflowInterception: nav.noWorkflowInterception,
                    deliverInitialMessage: true,
                    backgroundLaunch: true,
                    expertId: nav.expertId,
                    expertName: nav.expertName,
                    expertIcon: nav.expertIcon || builtinExpertIcon(nav.expertId || "") || undefined,
                };
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_TASK_LAUNCH, { detail }));
            },
            openExpert: (nav) => {
                openExpertConversation(
                    {
                        id: nav.id,
                        name: nav.name,
                        description: nav.description,
                        icon: builtinExpertIcon(nav.id),
                    },
                    nav.initialMessage,
                    nav.latexDocument,
                    nav.projectPath,
                    { backgroundLaunch: true },
                );
            },
            createCloudAgentTask: async ({ name, kind, text, workflowTemplateId, workflowParams, expertId }) => {
                const created = await startCloudAgentFromNewTask(name, kind, text, lang, { workflowTemplateId, workflowParams, expertId });
                const id = String(created?.id || '').trim();
                // This is the task identity the schedule row stores. It is not a
                // local directory. An empty path is what tells the dispatcher
                // that nothing was created and the claim may be launched again.
                if (id && !id.includes('/') && !id.includes('\\') && !id.includes(':')) {
                    projectPath = `cloud-agent:${id}`;
                }
                return created;
            },
            materializeLatexDocument: async ({ projectPath: path, templateId, templateName, userText }) => {
                let failure = "";
                const document = await createLatexDocumentForTask(path, templateId, "", lang, (message) => {
                    failure = message;
                });
                if (!document?.relative_path) {
                    throw new Error(failure || (tailText(lang, "The LaTeX document could not be created", "无法创建 LaTeX 文档")));
                }
                const workspacePath = String(document.workspace_path || "").trim();
                if (workspacePath) {
                    await SetTabWorkingDir(expertTabId(LATEX_EXPERT_ID), workspacePath);
                }
                return {
                    relativePath: document.relative_path,
                    initialMessage: latexPaperTaskMessage(lang, {
                        id: document.template_id || templateId,
                        name: document.template_name || templateName,
                    }, document, userText),
                };
            },
        },
    });
    if (!result.ok) {
        throw new Error(result.error || (tailText(lang, "Failed to create task", "创建任务失败")));
    }
    if (!projectPath) throw new Error(tailText(lang, "Failed to create task: no task path returned", "创建任务失败：未返回任务路径"));
    // The instance exists, but the turn was not accepted. Hand the identity
    // back with the reason so the schedule row fails instead of being created again.
    // A local create warning still means the task opened.
    if (projectPath.startsWith('cloud-agent:') && result.warning) {
        return { projectPath, error: result.warning };
    }
    return projectPath;
}
