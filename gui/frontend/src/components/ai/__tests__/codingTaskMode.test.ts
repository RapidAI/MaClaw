import { describe, expect, it } from "vitest";
import {
    agentModeFromTaskTags,
    cloudWorkspaceIdFromTags,
    cloudWorkspaceIdFromPath,
    cloudWorkspaceIdFromTaskFields,
    collapseCloudWorkspaceTasks,
    cloudWorkspaceNameFromEntitlement,
    __resetCloudWorkspaceDisplayNamesForTests,
    lookupCloudWorkspaceDisplayName,
    rememberCloudWorkspaceDisplayName,
    rememberCloudWorkspaceDisplayNames,
    isCloudWorkspacePath,
    isCloudWorkspaceFilePath,
    isCloudWorkspaceTask,
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
        expect(isCloudWorkspaceTask(null)).toBe(false);
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
