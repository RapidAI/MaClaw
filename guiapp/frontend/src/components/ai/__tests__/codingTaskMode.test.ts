import { afterEach, describe, expect, it, vi } from "vitest";
import {
    agentModeForCloudWorkspace,
    agentModeFromTaskTags,
    cloudWorkspaceIdFromTags,
    cloudWorkspaceIdFromPath,
    cloudWorkspaceIdFromTab,
    cloudWorkspaceIdFromTaskFields,
    cloudWorkspaceLeaseEnsured,
    collapseCloudWorkspaceTasks,
    ensureCloudWorkspaceLeaseBeforeSend,
    forgetCloudWorkspaceLeaseEnsured,
    markCloudWorkspaceLeaseEnsured,
    __resetCloudWorkspaceLeaseEnsureForTests,
    cloudWorkspaceNameFromEntitlement,
    __resetCloudWorkspaceDisplayNamesForTests,
    lookupCloudWorkspaceDisplayName,
    rememberCloudWorkspaceDisplayName,
    rememberCloudWorkspaceDisplayNames,
    isCloudWorkspacePath,
    isCloudWorkspaceFilePath,
    isCloudWorkspaceTask,
    isTaskManagementTaskRow,
    isVisibleTaskRow,
    visibleTaskRows,
    cloudWorkspaceRevealMatchesTab,
    cloudWorkingDirForActiveTab,
    isActiveCloudWorkspacePreview,
    nextTabWorkingDir,
    cloudSafePathLabel,
    cloudWorkspaceRelativePath,
    cloudWorkspaceRootFromPath,
    FOCUS_CLOUD_WORKSPACE_TREE_EVENT,
    CLOUD_WORKSPACE_FILES_CHANGED_EVENT,
    parseWailsEventObject,
    REVEAL_CLOUD_WORKSPACE_FILES_EVENT,
    scrubCloudWorkspaceError,
    isCodingWorkflowSourceTags,
    isPureCodingTaskTags,
    isRemoteMaintenanceTaskTags,
    remoteCodingMetaFromTaskTags,
    remoteHostFromTaskTags,
} from "../codingTaskMode";

describe("codingTaskMode", () => {
    afterEach(() => {
        __resetCloudWorkspaceLeaseEnsureForTests();
        __resetCloudWorkspaceDisplayNamesForTests();
    });

    it("recovers the cloud workspace lease on the first send and skips later ones", async () => {
        const resume = vi.fn().mockResolvedValue({ project_path: "D:/tasks/cloud-math" });
        const first = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_math",
            projectPath: "D:/tasks/cloud-math",
            resume,
        });
        expect(first).toEqual({ ok: true, skipped: false, projectPath: "D:/tasks/cloud-math" });
        expect(resume).toHaveBeenCalledTimes(1);
        expect(cloudWorkspaceLeaseEnsured("cws_math")).toBe(true);

        const second = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_math",
            projectPath: "D:/tasks/cloud-math",
            resume,
        });
        expect(second).toEqual({ ok: true, skipped: true, projectPath: "D:/tasks/cloud-math" });
        expect(resume).toHaveBeenCalledTimes(1);
        forgetCloudWorkspaceLeaseEnsured("cws_math");
        expect(cloudWorkspaceLeaseEnsured("cws_math")).toBe(false);
        const third = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_math",
            projectPath: "D:/tasks/cloud-math",
            resume,
        });
        expect(third).toEqual({ ok: true, skipped: false, projectPath: "D:/tasks/cloud-math" });
        expect(resume).toHaveBeenCalledTimes(2);
    });

    it("coalesces concurrent first-command lease recovery", async () => {
        __resetCloudWorkspaceLeaseEnsureForTests();
        let resolveResume: (value: { project_path: string }) => void = () => {};
        const resume = vi.fn(() => new Promise<{ project_path: string }>((resolve) => {
            resolveResume = resolve;
        }));
        const first = ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_live",
            projectPath: "D:/tasks/live",
            resume,
        });
        const second = ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_live",
            projectPath: "D:/tasks/live",
            resume,
        });
        expect(resume).toHaveBeenCalledTimes(1);
        resolveResume({ project_path: "D:/tasks/live" });
        await expect(first).resolves.toEqual({ ok: true, skipped: false, projectPath: "D:/tasks/live" });
        await expect(second).resolves.toEqual({ ok: true, skipped: false, projectPath: "D:/tasks/live" });
        markCloudWorkspaceLeaseEnsured("cws_other");
        expect(cloudWorkspaceLeaseEnsured("cws_other")).toBe(true);
    });

    it("does not mark the lease held when resume is cancelled", async () => {
        const resume = vi.fn().mockRejectedValue(new Error("已取消打开云端工作区"));
        const result = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_busy",
            projectPath: "D:/tasks/busy",
            resume,
        });
        expect(result).toEqual({ ok: false, cancelled: true, error: "已取消打开云端工作区" });
        expect(cloudWorkspaceLeaseEnsured("cws_busy")).toBe(false);
    });

    it("does not treat an unrelated cancelled request as a user dismiss", async () => {
        const resume = vi.fn().mockRejectedValue(new Error("请求已取消，请重试"));
        const result = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_net",
            projectPath: "D:/tasks/net",
            resume,
        });
        expect(result.ok).toBe(false);
        if (!result.ok) {
            expect(result.cancelled).toBe(false);
            expect(result.error).toBe("请求已取消，请重试");
        }
    });

    it("treats an empty resume binding as a recoverable failure", async () => {
        const resume = vi.fn().mockResolvedValue({ project_path: "" });
        const result = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_empty",
            projectPath: "D:/tasks/empty",
            resume,
        });
        expect(result.ok).toBe(false);
        if (!result.ok) {
            expect(result.cancelled).toBe(false);
            expect(result.error).toMatch(/unable to open the cloud workspace task/i);
        }
        expect(cloudWorkspaceLeaseEnsured("cws_empty")).toBe(false);
    });

    it("skips resume when the send is not a cloud workspace", async () => {
        const resume = vi.fn();
        await expect(ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "",
            projectPath: "D:/tasks/local",
            resume,
        })).resolves.toEqual({ ok: true, skipped: true, projectPath: "D:/tasks/local" });
        expect(resume).not.toHaveBeenCalled();
    });

    it("force-resumes even when the process already marked the lease held", async () => {
        markCloudWorkspaceLeaseEnsured("cws_stale");
        const resume = vi.fn().mockResolvedValue({ project_path: "D:/tasks/stale" });
        const result = await ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_stale",
            projectPath: "D:/tasks/stale",
            resume,
            force: true,
        });
        expect(result).toEqual({ ok: true, skipped: false, projectPath: "D:/tasks/stale" });
        expect(resume).toHaveBeenCalledTimes(1);
    });

    it("does not keep the lease marked if the tab is forgotten while resume is in flight", async () => {
        let resolveResume: (value: { project_path: string }) => void = () => {};
        const resume = vi.fn(() => new Promise<{ project_path: string }>((resolve) => {
            resolveResume = resolve;
        }));
        const pending = ensureCloudWorkspaceLeaseBeforeSend({
            workspaceId: "cws_closed",
            projectPath: "D:/tasks/closed",
            resume,
        });
        forgetCloudWorkspaceLeaseEnsured("cws_closed");
        resolveResume({ project_path: "D:/tasks/closed" });
        await expect(pending).resolves.toEqual({
            ok: false,
            cancelled: true,
            error: "已取消打开云端工作区",
        });
        expect(cloudWorkspaceLeaseEnsured("cws_closed")).toBe(false);
    });

    it("reads a cloud workspace id from tab identity before cache path", () => {
        expect(cloudWorkspaceIdFromTab({
            cloudWorkspaceId: "cws_tab",
            projectPath: "C:/data/cloud-workspaces/tenant/cws_path",
        })).toBe("cws_tab");
        expect(cloudWorkspaceIdFromTab({
            projectPath: "D:/tasks/math",
            workingDir: "C:/data/cloud-workspaces/tenant/cws_dir",
        })).toBe("cws_dir");
        expect(cloudWorkspaceIdFromTab({ projectPath: "D:/tasks/local" })).toBe("");
    });

    it("extracts cloud workspace id from tags", () => {
        expect(cloudWorkspaceIdFromTags(["cloud_workspace:cws_demo", "coding_dev"])).toBe("cws_demo");
        expect(cloudWorkspaceIdFromTags(["coding_dev"])).toBe("");
        expect(cloudWorkspaceIdFromTags([])).toBe("");
    });

    it("prefers working_dir over accumulated cloud workspace tags", () => {
        expect(cloudWorkspaceIdFromTaskFields({
            tags: ["cloud_workspace:cws_a", "cloud_workspace:cws_b"],
            project_path: "D:/tasks/legacy",
            working_dir: "C:/data/cloud-workspaces/tenant_default/cws_b",
        })).toBe("cws_b");
        expect(cloudWorkspaceIdFromTaskFields({
            tags: ["cloud_workspace:cws_a"],
            project_path: "D:/tasks/tagged",
        })).toBe("cws_a");
    });

    it("collapses duplicate cloud workspace rows to the named task", () => {
        const collapsed = collapseCloudWorkspaceTasks([
            { name: "新建云端工作区任务", tags: ["cloud_workspace:cws_x"], project_path: "D:/tasks/generic" },
            { name: "长江学者申请", tags: ["cloud_workspace:cws_x"], project_path: "D:/tasks/named" },
            { name: "local", project_path: "D:/tasks/local" },
        ]);
        expect(collapsed).toHaveLength(2);
        expect(collapsed[0].project_path).toBe("D:/tasks/named");
        expect(collapsed[1].project_path).toBe("D:/tasks/local");
    });

    it("detects local cache paths of cloud workspaces", () => {
        expect(isCloudWorkspacePath("C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc")).toBe(true);
        expect(isCloudWorkspacePath("/home/me/.maclaw/data/cloud-workspaces/tenant_default/cws_abc")).toBe(true);
        expect(isCloudWorkspacePath("D:/work/tasks/local-project")).toBe(false);
        expect(isCloudWorkspacePath("")).toBe(false);
        expect(isCloudWorkspacePath(undefined)).toBe(false);
        expect(isCloudWorkspacePath("open C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\t\\cws\\a.md: EOF")).toBe(true);
    });

    it("detects cloud workspace tasks from tags or cache paths", () => {
        expect(isCloudWorkspaceTask({ tags: ["cloud_workspace:cws_a"], project_path: "D:/tasks/cloud-1" })).toBe(true);
        expect(isCloudWorkspaceTask({
            projectPath: "D:/tasks/cloud-1",
            workingDir: "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a",
        })).toBe(true);
        expect(isCloudWorkspaceTask({ project_path: "D:/tasks/local", working_dir: "D:/work/app" })).toBe(false);
        expect(isCloudWorkspaceTask({ project_path: "C:/Users/me/.maclaw/data/cloud-workspaces/" })).toBe(true);
        expect(isCloudWorkspaceTask(null)).toBe(false);
    });

    it("detects durable task-management rows from identity tags", () => {
        expect(isTaskManagementTaskRow({ tags: ["task_management", "coding_dev"] })).toBe(true);
        expect(isTaskManagementTaskRow({ tags: ["task_user_created"] })).toBe(true);
        expect(isTaskManagementTaskRow({ tags: ["task_user_saved"] })).toBe(true);
        expect(isTaskManagementTaskRow({ tags: ["manual_task", "recent_task"] })).toBe(true);
        expect(isTaskManagementTaskRow({ tags: ["manual_task"] })).toBe(false);
        expect(isTaskManagementTaskRow({ tags: ["cloud_workspace:cws_a"] })).toBe(false);
        expect(isTaskManagementTaskRow({ tags: [] })).toBe(false);
        expect(isTaskManagementTaskRow(null)).toBe(false);
    });

    it("applies one visibility rule for sidebar, switcher and search", () => {
        const rows = [
            { name: "out", project_path: "D:/tasks/out" },
            { name: "no-output-auto", project_path: "D:/tasks/auto", has_output: false },
            { name: "no-output-managed", project_path: "D:/tasks/managed", has_output: false, tags: ["task_management"] },
            { name: "no-output-cloud", project_path: "D:/tasks/cloud", has_output: false, tags: ["cloud_workspace:cws_v"] },
        ];
        expect(rows.filter(isVisibleTaskRow).map(r => r.name)).toEqual(["out", "no-output-managed", "no-output-cloud"]);
        expect(visibleTaskRows(rows).map(r => r.name)).toEqual(["out", "no-output-managed", "no-output-cloud"]);
        expect(isVisibleTaskRow(rows[1])).toBe(false);
    });

    it("does not treat a remote SSH task with a stray cloud tag as a cloud workspace", () => {
        const remote = {
            tags: ["remote_coding_dev", "remote_host:www.driverdevelop.com", "cloud_workspace:cws_a"],
            project_path: "D:/tasks/remote-fix",
            working_dir: "/var/www/app",
        };
        expect(cloudWorkspaceIdFromTaskFields(remote)).toBe("");
        expect(isCloudWorkspaceTask(remote)).toBe(false);
        const collapsed = collapseCloudWorkspaceTasks([
            remote,
            { name: "人工智能数学基础", tags: ["cloud_workspace:cws_a"], project_path: "D:/tasks/cloud-math" },
        ]);
        expect(collapsed).toHaveLength(2);
        expect(collapsed[0].project_path).toBe("D:/tasks/remote-fix");
        expect(collapsed[1].project_path).toBe("D:/tasks/cloud-math");
    });

    it("remaps remote coding mode when opening a cloud workspace", () => {
        expect(agentModeForCloudWorkspace("remote_coding_dev", "cws_math")).toBe("coding_dev");
        expect(agentModeForCloudWorkspace("coding_dev", "cws_math")).toBe("coding_dev");
        expect(agentModeForCloudWorkspace("remote_coding_dev", "")).toBe("remote_coding_dev");
        expect(agentModeForCloudWorkspace(undefined, "cws_math")).toBeUndefined();
    });

    it("matches a cloud reveal against either the task path or the cache root", () => {
        const taskPath = "C:/Users/me/.maclaw/data/tasks/cloud-1";
        const cacheRoot = "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a";
        expect(cloudWorkspaceRevealMatchesTab(
            { projectPath: taskPath, workingDir: cacheRoot },
            { projectPath: taskPath },
        )).toBe(true);
        expect(cloudWorkspaceRevealMatchesTab(
            { projectPath: cacheRoot },
            { projectPath: taskPath, workingDir: cacheRoot },
        )).toBe(true);
        expect(cloudWorkspaceRevealMatchesTab(
            { projectPath: `${cacheRoot}/docs/a.md` },
            { projectPath: taskPath, workingDir: cacheRoot },
        )).toBe(true);
        expect(cloudWorkspaceRevealMatchesTab(
            { projectPath: cacheRoot },
            { projectPath: "D:/tasks/other", workingDir: "D:/work/app" },
        )).toBe(false);
        expect(cloudWorkspaceRevealMatchesTab(
            { projectPath: taskPath, workingDir: cacheRoot },
            { projectPath: "D:/tasks/cloud-other", workingDir: "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a/docs" },
        )).toBe(true);
        expect(cloudWorkspaceRevealMatchesTab(null, { projectPath: taskPath })).toBe(false);
    });

    it("keeps a cloud cache working dir when a later local default arrives", () => {
        const cache = "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a";
        const prev = nextTabWorkingDir(null, "proj-1", cache);
        expect(prev).toEqual({ tabId: "proj-1", path: cache });
        expect(nextTabWorkingDir(prev, "proj-1", "D:/Users/me/Desktop")).toEqual(prev);
        expect(nextTabWorkingDir(prev, "proj-1", cache)).toBe(prev);
        expect(nextTabWorkingDir(prev, "proj-2", "D:/work/app")).toEqual({ tabId: "proj-2", path: "D:/work/app" });
        expect(nextTabWorkingDir(prev, "", cache)).toEqual(prev);
    });

    it("uses a pending double-click reveal as the cloud dir before GetTabWorkingDir returns", () => {
        const taskPath = "C:/Users/me/.maclaw/data/tasks/cloud-1";
        const cache = "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a";
        expect(cloudWorkingDirForActiveTab({
            tabId: "proj-1",
            projectPath: taskPath,
            pending: { projectPath: taskPath, workingDir: cache },
        })).toBe(cache);
        expect(isActiveCloudWorkspacePreview({
            isProjectTab: true,
            projectPath: taskPath,
            pendingReveal: { projectPath: taskPath, workingDir: cache },
        })).toBe(true);
        expect(cloudWorkingDirForActiveTab({
            tabId: "proj-2",
            projectPath: "D:/tasks/other",
            pending: { projectPath: taskPath, workingDir: cache },
        })).toBe("");
        expect(isActiveCloudWorkspacePreview({
            isProjectTab: true,
            projectPath: "D:/tasks/other",
            pendingReveal: { projectPath: taskPath, workingDir: cache },
        })).toBe(false);
        expect(isActiveCloudWorkspacePreview({
            isProjectTab: false,
            projectPath: taskPath,
            pendingReveal: { projectPath: taskPath, workingDir: cache },
        })).toBe(false);
    });

    it("scrubs local cache paths from cloud workspace errors", () => {
        expect(scrubCloudWorkspaceError(
            "open C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\t\\cws\\a.md: EOF",
            "无法加载云端文件。",
        )).toBe("无法加载云端文件。");
        expect(scrubCloudWorkspaceError("permission denied", "无法加载云端文件。")).toBe("permission denied");
        expect(REVEAL_CLOUD_WORKSPACE_FILES_EVENT).toBe("ai-reveal-cloud-workspace-files");
        expect(FOCUS_CLOUD_WORKSPACE_TREE_EVENT).toBe("ai-focus-cloud-workspace-tree");
        expect(CLOUD_WORKSPACE_FILES_CHANGED_EVENT).toBe("cloud-workspace-files-changed");
        expect(parseWailsEventObject('{"session_key":"desktop-user:D:/tasks/cloud","text":"done"}').session_key).toBe("desktop-user:D:/tasks/cloud");
        expect(parseWailsEventObject({ workspace_id: "cws_a", path: "/cache" }).workspace_id).toBe("cws_a");
        expect(parseWailsEventObject("not-json")).toEqual({});
    });

    it("exposes a relative cloud path and cache root without the local prefix", () => {
        const win = "C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc\\docs\\a.md";
        const posix = "/home/me/.maclaw/data/cloud-workspaces/tenant_default/cws_abc/docs/a.md";
        expect(cloudWorkspaceRelativePath(win)).toBe("docs/a.md");
        expect(cloudWorkspaceRelativePath(posix)).toBe("docs/a.md");
        expect(cloudWorkspaceRootFromPath(win)).toBe("C:/Users/me/.maclaw/data/cloud-workspaces/tenant_default/cws_abc");
        expect(cloudSafePathLabel(win)).toBe("docs/a.md");
        expect(cloudSafePathLabel("C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc")).toBe("cloud");
        expect(cloudSafePathLabel("D:/work/app/main.go")).toBe("D:/work/app/main.go");
    });

    it("treats a relative cache file with an extension as a cloud file that can be opened locally", () => {
        const root = "C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc";
        expect(isCloudWorkspaceFilePath(`${root}\\人工智能数学入门教程.pdf`)).toBe(true);
        expect(isCloudWorkspaceFilePath(`${root}/docs/a.md`)).toBe(true);
        expect(isCloudWorkspaceFilePath(root)).toBe(false);
        expect(isCloudWorkspaceFilePath(`${root}\\docs`)).toBe(false);
        expect(isCloudWorkspaceFilePath(`${root}/book.pdf/`)).toBe(false);
        expect(isCloudWorkspaceFilePath(`${root}/.env`)).toBe(false);
        expect(isCloudWorkspaceFilePath("D:/work/app/main.go")).toBe(false);
    });

    it("extracts the Hub workspace id from a cache mount path", () => {
        expect(cloudWorkspaceIdFromPath("C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc\\docs\\a.md")).toBe("cws_abc");
        expect(cloudWorkspaceIdFromPath("/home/me/.maclaw/data/cloud-workspaces/tenant_default/cws_abc")).toBe("cws_abc");
        expect(cloudWorkspaceIdFromPath("D:/work/app")).toBe("");
    });

    it("resolves the Hub workspace name from entitlement rows", () => {
        const ent = {
            workspaces: [{ id: "cws_abc", name: "标书项目" }],
            deleted: [{ ID: "cws_old", Name: "旧项目" }],
        };
        expect(cloudWorkspaceNameFromEntitlement(ent, "cws_abc")).toBe("标书项目");
        expect(cloudWorkspaceNameFromEntitlement({ Workspaces: [{ ID: "cws_abc", Name: "标书项目" }] }, "cws_abc")).toBe("标书项目");
        expect(cloudWorkspaceNameFromEntitlement(ent, "cws_old")).toBe("旧项目");
        expect(cloudWorkspaceNameFromEntitlement(ent, "missing")).toBe("");
        expect(cloudWorkspaceNameFromEntitlement(null, "cws_abc")).toBe("");
    });

    it("caches Hub workspace display names for instant preview headers", () => {
        __resetCloudWorkspaceDisplayNamesForTests();
        rememberCloudWorkspaceDisplayNames({
            workspaces: [{ id: "cws_abc", name: "标书项目" }],
        });
        expect(lookupCloudWorkspaceDisplayName("cws_abc", "任务标题")).toBe("标书项目");
        rememberCloudWorkspaceDisplayName("cws_abc", "投标文件");
        expect(lookupCloudWorkspaceDisplayName("cws_abc")).toBe("投标文件");
        expect(lookupCloudWorkspaceDisplayName("missing", "任务标题")).toBe("任务标题");
        __resetCloudWorkspaceDisplayNamesForTests();
        expect(lookupCloudWorkspaceDisplayName("cws_abc", "任务标题")).toBe("任务标题");
    });

    it("detects local and remote pure coding tags", () => {
        expect(agentModeFromTaskTags(["coding_dev"])).toBe("coding_dev");
        expect(agentModeFromTaskTags(["remote_coding_dev", "remote_host:10.0.0.1"])).toBe("remote_coding_dev");
        expect(agentModeFromTaskTags(["task_management"])).toBeUndefined();
        expect(isPureCodingTaskTags(["coding_dev"])).toBe(true);
        expect(isPureCodingTaskTags([])).toBe(false);
    });

    it("detects coding workflow source tag", () => {
        expect(isCodingWorkflowSourceTags(["remote_coding_dev", "source:coding_workflow"])).toBe(true);
        expect(isCodingWorkflowSourceTags(["coding_dev"])).toBe(false);
        expect(isCodingWorkflowSourceTags([])).toBe(false);
    });

    it("detects remote maintenance task origin", () => {
        expect(isRemoteMaintenanceTaskTags(["remote_coding_dev", "source:remote_ops_diagnosis"])).toBe(true);
        expect(isRemoteMaintenanceTaskTags(["remote_coding_dev"])).toBe(false);
    });

    it("extracts remote host from tags", () => {
        expect(remoteHostFromTaskTags(["remote_host:10.0.0.8", "coding_dev"])).toBe("10.0.0.8");
        expect(remoteHostFromTaskTags(["coding_dev"])).toBeUndefined();
        expect(remoteHostFromTaskTags(["remote_host:2001:db8::1"])).toBe("2001:db8::1");
    });

    it("parses full remote meta including IPv6 host", () => {
        const meta = remoteCodingMetaFromTaskTags([
            "remote_coding_dev",
            "remote_host:2001:db8::1",
            "remote_user:ubuntu",
            "remote_port:2222",
            "remote_workdir:/home/ubuntu/app",
        ]);
        expect(meta).toEqual({
            host: "2001:db8::1",
            user: "ubuntu",
            port: 2222,
            workDir: "/home/ubuntu/app",
        });
    });
});
