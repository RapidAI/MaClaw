import { describe, expect, it, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { sanitizeProjectTabTitle, useAITabManager } from "../useAITabManager";

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    LoadProjectTabIndex: vi.fn().mockResolvedValue([]),
    CloseAssistantTabSession: vi.fn().mockResolvedValue(undefined),
    CreateProjectTabSession: vi.fn().mockResolvedValue(undefined),
    SaveProjectTabConversation: vi.fn().mockResolvedValue(undefined),
    LoadProjectTabConversation: vi.fn().mockResolvedValue([]),
    ClearAIAssistantHistoryForSession: vi.fn().mockResolvedValue(undefined),
}));

const modelInfoTitle = "agnes 视频 生成模型信息保存到知识库：https://api.agnes.ai.cn/v1";

describe("sanitizeProjectTabTitle", () => {
    it("keeps a task name that cites an API URL", () => {
        expect(sanitizeProjectTabTitle(modelInfoTitle, "C:/Users/me/.maclaw/data/tasks/agnes-model")).toBe(modelInfoTitle);
        expect(sanitizeProjectTabTitle("https://api.agnes.ai.cn/v1/a/17904877952824845000/workspace")).toBe("https://api.agnes.ai.cn/v1/a/17904877952824845000/workspace");
    });

    it("still shortens a bare task directory path", () => {
        expect(sanitizeProjectTabTitle(
            "C:\\Users\\ma139\\.maclaw\\data\\tasks\\agnes-model-17904877952824845000",
        )).toBe("agnes-model");
        expect(sanitizeProjectTabTitle("D:/work/tasks/some-very-long-directory/unopened-task")).toBe("unopened-task");
    });
});

describe("createProjectTab title repair", () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it("replaces a collapsed URL tail when the task is opened with its real name", () => {
        const { result } = renderHook(() => useAITabManager());
        const projectPath = "C:/Users/me/.maclaw/data/tasks/agnes-model";
        act(() => {
            result.current.createProjectTab(projectPath, "v1");
        });
        act(() => {
            result.current.createProjectTab(projectPath, modelInfoTitle);
        });
        expect(result.current.tabState.tabs.find((tab) => tab.projectPath === "C:/Users/me/.maclaw/data/tasks/agnes-model")?.title).toBe(modelInfoTitle);
    });

    it("does not replace a directory slug with a weaker token", () => {
        const { result } = renderHook(() => useAITabManager());
        const projectPath = "C:/Users/me/.maclaw/data/tasks/agnes-model";
        act(() => {
            result.current.createProjectTab(projectPath, "agnes-model");
        });
        act(() => {
            result.current.createProjectTab(projectPath, "v1");
        });
        expect(result.current.activeTab.title).toBe("agnes-model");
    });

    it("keeps a short real title that is not a URL tail", () => {
        const { result } = renderHook(() => useAITabManager());
        const projectPath = "D:/work/tasks/build-dashboard";
        act(() => {
            result.current.createProjectTab(projectPath, "notes");
        });
        act(() => {
            result.current.createProjectTab(projectPath, "Weekly notes");
        });
        expect(result.current.activeTab.title).toBe("notes");
    });

    it("does not replace a real title with a short path tail", () => {
        const { result } = renderHook(() => useAITabManager());
        const projectPath = "D:/work/tasks/build-dashboard";
        act(() => {
            result.current.createProjectTab(projectPath, "Build dashboard");
        });
        act(() => {
            result.current.createProjectTab(projectPath, "v1");
        });
        expect(result.current.activeTab.title).toBe("Build dashboard");
    });
});
