// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";

const cloudEntitlement = vi.hoisted(() => vi.fn(async () => ({ workspaces: [] as Array<{ id: string; name: string }> })));
vi.mock("../../../../wailsjs/go/main/App", () => ({
    CloudWorkspaceEntitlement: () => cloudEntitlement(),
}));
import type { AITab } from "../AITabTypes";
import { TaskExecutionHeading } from "../TaskExecutionHeading";
import { __resetCloudWorkspaceDisplayNamesForTests, rememberCloudWorkspaceDisplayName } from "../codingTaskMode";
import { listedTaskTitle } from "../describeTaskTitle";
import { assistantTaskTitle } from "../taskListTitle";

const listedName = "agnes 视频 生成模型信息保存到知识库：https://api.agnes.ai.cn/v1";

const tab: AITab = {
    id: "proj-agnes",
    type: "project",
    title: "v1",
    projectPath: "C:/Users/ma139/.maclaw/data/tasks/agnes-model",
    closable: true,
};

describe("assistant task title", () => {
    it("uses the sidebar task name instead of a tab title peeled from a URL", () => {
        const tasks = [{ name: listedName, project_path: tab.projectPath }];
        expect(listedTaskTitle(tasks[0])).toBe(listedName);
        expect(assistantTaskTitle(tab, "zh-CN", tasks)).toBe(listedName);
        expect(assistantTaskTitle(tab, "zh-CN", tasks)).not.toBe("v1");
    });

    it("matches a cloud task by workspace id when the cache path differs", () => {
        const cloudTab: AITab = {
            ...tab,
            title: "v1",
            projectPath: "E:/new-cache/cloud-workspaces/tenant/cws-42",
            cloudWorkspaceId: "cws-42",
        };
        const tasks = [{ name: "标书项目", project_path: "D:/old-cache/cloud-workspaces/tenant/cws-42", tags: ["cloud_workspace:cws-42"] }];
        expect(assistantTaskTitle(cloudTab, "zh-CN", tasks)).toBe("标书项目");
    });

    it("uses the cached cloud workspace name when the task row has no name", () => {
        rememberCloudWorkspaceDisplayName("cws-empty", "标书项目");
        try {
            const cloudTab: AITab = {
                ...tab,
                title: "v1",
                projectPath: "E:/cache/cloud-workspaces/tenant/cws-empty",
                cloudWorkspaceId: "cws-empty",
            };
            const tasks = [{ name: "", project_path: cloudTab.projectPath, tags: ["cloud_workspace:cws-empty"], has_output: true }];
            expect(assistantTaskTitle(cloudTab, "zh-CN", tasks)).toBe("标书项目");
        } finally {
            __resetCloudWorkspaceDisplayNamesForTests();
        }
    });

    it("uses the named cloud row the sidebar keeps when the open path is an empty alias", () => {
        const cloudTab: AITab = {
            ...tab,
            title: "v1",
            projectPath: "E:/new-cache/cloud-workspaces/tenant/cws-42",
            cloudWorkspaceId: "cws-42",
        };
        const tasks = [
            { name: "", project_path: cloudTab.projectPath, tags: ["cloud_workspace:cws-42"], has_output: true },
            { name: "标书项目", project_path: "D:/old-cache/cloud-workspaces/tenant/cws-42", tags: ["cloud_workspace:cws-42"], has_output: true },
        ];
        expect(assistantTaskTitle(cloudTab, "zh-CN", tasks)).toBe("标书项目");
    });

    it("keeps the tab title when the task list has no matching row", () => {
        expect(assistantTaskTitle(tab, "zh-CN", [])).toBe("v1");
    });

    it("uses the list title when the open path is the task working directory", () => {
        const tasks = [{
            name: listedName,
            project_path: "C:/Users/me/.maclaw/data/tasks/agnes-model",
            working_dir: "C:/Users/me/.maclaw/data/tasks/agnes-model/workspace",
        }];
        const workTab: AITab = { ...tab, title: "v1", projectPath: tasks[0].working_dir };
        expect(assistantTaskTitle(workTab, "zh-CN", tasks)).toBe(listedName);
    });

    it("prefers the task whose project path matches over another task's working directory", () => {
        const tasks = [
            { name: "其他任务", project_path: "D:/elsewhere", working_dir: tab.projectPath },
            { name: listedName, project_path: tab.projectPath, working_dir: `${tab.projectPath}/workspace` },
        ];
        expect(assistantTaskTitle(tab, "zh-CN", tasks)).toBe(listedName);
    });

    it("matches a task path with different slashes and drive case", () => {
        const windowsTab: AITab = { ...tab, projectPath: "C:\\Users\\me\\tasks\\agnes" };
        const tasks = [{ name: listedName, project_path: "c:/users/me/tasks/agnes/" }];
        expect(assistantTaskTitle(windowsTab, "zh-CN", tasks)).toBe(listedName);
    });
});

describe("TaskExecutionHeading", () => {
    afterEach(() => { cleanup(); });

    it("shows the title the panel already resolved from the task list", () => {
        render(
            <TaskExecutionHeading
                activeTab={tab}
                lang="zh-CN"
                status={{ tone: "running", label: "进行中" }}
                title={listedName}
            />,
        );
        expect(screen.getByRole("heading", { level: 2 }).textContent).toBe(listedName);
    });

    it("shows a local path, a remote server, and a cloud workspace name", () => {
        const status = { tone: "done", label: "已完成" };
        const { rerender } = render(
            <TaskExecutionHeading
                activeTab={{ ...tab, type: "project" }}
                lang="zh-CN"
                status={status}
                taskCreatedLabel="10/03 05:15"
                workingDirPath="D:/work/app"
                title="北京天气"
            />,
        );
        const localMeta = screen.getByTestId("task-execution-meta");
        expect(localMeta.textContent).toContain("D:/work/app");
        expect(localMeta.textContent).not.toContain("本地");
        expect(localMeta.getAttribute("title")).toContain("D:/work/app");

        rerender(
            <TaskExecutionHeading
                activeTab={{ ...tab, type: "project" }}
                lang="zh-CN"
                status={status}
                taskCreatedLabel="10/03 05:15"
                workingDirPath="C:\\Users\\me\\.maclaw\\data\\tasks\\你好呀-1\\workspace"
                remoteWorkspace={{ host: "www.driverdevelopment.com", workDir: "/home/ubuntu/app", port: 2222 }}
                remoteWorkspaceLabel="www.driverdevelopment.com:2222/home/ubuntu/app"
                title="你好呀"
            />,
        );
        const remoteMeta = screen.getByTestId("task-execution-meta");
        expect(remoteMeta.textContent).toContain("www.driverdevelopment.com:2222/home/ubuntu/app");
        expect(remoteMeta.textContent).not.toContain("你好呀-1");
        expect(remoteMeta.getAttribute("title")).toBe("由你创建 · 10/03 05:15 · www.driverdevelopment.com:2222/home/ubuntu/app");

        rerender(
            <TaskExecutionHeading
                activeTab={{ ...tab, type: "project" }}
                lang="zh-CN"
                status={status}
                taskCreatedLabel="10/03 05:15"
                workingDirPath="C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant\\cws_abc"
                remoteWorkspace={{ host: "home.rapidai.tech", workDir: "/home/rapidrec", port: 55 }}
                remoteWorkspaceLabel="home.rapidai.tech:55/home/rapidrec"
                title="远程目录"
            />,
        );
        const remoteOverCloud = screen.getByTestId("task-execution-meta");
        expect(remoteOverCloud.textContent).toContain("home.rapidai.tech:55/home/rapidrec");
        expect(remoteOverCloud.textContent).not.toMatch(/cloud-workspaces|云端工作区/);

        rememberCloudWorkspaceDisplayName("cws_abc", "标书项目");
        try {
            rerender(
                <TaskExecutionHeading
                    activeTab={{ ...tab, type: "project" }}
                    lang="zh-CN"
                    status={status}
                    taskCreatedLabel="10/03 05:15"
                    workingDirPath="C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant\\cws_abc"
                    title="标书"
                />,
            );
            const cloudMeta = screen.getByTestId("task-execution-meta");
            expect(cloudMeta.textContent).toContain("标书项目");
            expect(cloudMeta.textContent).not.toMatch(/cloud-workspaces/);
            act(() => { rememberCloudWorkspaceDisplayName("cws_abc", "新名称"); });
            expect(screen.getByTestId("task-execution-meta").textContent).toContain("新名称");
        } finally {
            act(() => { __resetCloudWorkspaceDisplayNamesForTests(); });
        }
    });

    it("shows a shared cloud workspace name when entitlement arrives", async () => {
        __resetCloudWorkspaceDisplayNamesForTests();
        let resolveEnt: (value: unknown) => void = () => {};
        cloudEntitlement.mockReturnValue(new Promise((resolve) => { resolveEnt = resolve as (value: unknown) => void; }));
        try {
            render(
                <TaskExecutionHeading
                    activeTab={{ ...tab, type: "project" }}
                    lang="zh-CN"
                    status={{ tone: "done", label: "已完成" }}
                    workingDirPath="C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_shared"
                    title="分享任务"
                />,
            );
            expect(screen.getByTestId("task-execution-meta").textContent).toContain("云端工作区");
            await act(async () => {
                resolveEnt({ shared: [{ id: "cws_shared", name: "共享标书" }] });
            });
            expect(screen.getByTestId("task-execution-meta").textContent).toContain("共享标书");
            expect(screen.getByTestId("task-execution-meta").textContent).not.toMatch(/cloud-workspaces/);
        } finally {
            cloudEntitlement.mockReset();
            cloudEntitlement.mockResolvedValue({ workspaces: [] });
            act(() => { __resetCloudWorkspaceDisplayNamesForTests(); });
        }
    });
});
