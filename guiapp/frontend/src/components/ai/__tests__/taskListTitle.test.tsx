// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
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
});
