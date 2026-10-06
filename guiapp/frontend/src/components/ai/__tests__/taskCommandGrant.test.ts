import { describe, expect, it } from "vitest";
import { commandGrantAppliesToVisibleTab, visibleCodingProject } from "../taskCommandGrant";

describe("task command grant tab", () => {
    it("reads the project path only from a coding task tab", () => {
        expect(visibleCodingProject({ type: "project", agentMode: "coding_dev", projectPath: "D:/task-a" })).toBe("D:/task-a");
        expect(visibleCodingProject({ type: "project", agentMode: "remote_coding_dev", projectPath: "D:/task-b" })).toBe("D:/task-b");
        expect(visibleCodingProject({ type: "project", agentMode: "chat", projectPath: "D:/task-a" })).toBe("");
        expect(visibleCodingProject({ type: "local", agentMode: "coding_dev", projectPath: "D:/task-a" })).toBe("");
        expect(visibleCodingProject(null)).toBe("");
    });

    it("keeps 以后允许 on the tab that opened the dialog", () => {
        expect(commandGrantAppliesToVisibleTab("D:/task-a", "D:/task-a")).toBe(true);
        expect(commandGrantAppliesToVisibleTab("D:/task-a", "D:/task-b")).toBe(false);
        expect(commandGrantAppliesToVisibleTab("", "D:/task-a")).toBe(false);
    });
});
