import { describe, expect, it } from "vitest";
import { normalizeCodingTaskLaunch } from "../codingTaskLaunch";
import { canDispatchCodingIntent, resolveCodingTaskPhase } from "../codingTaskRuntime";

describe("normalizeCodingTaskLaunch", () => {
    it("normalizes a remote launch without credentials and preserves its reconnect gate", () => {
        expect(normalizeCodingTaskLaunch({
            projectPath: " D:/tasks/remote ",
            taskTitle: "  Fix release  ",
            agentMode: "remote_coding_dev",
            remoteHost: " ssh.example.test ",
            remoteNeedsReconnect: true,
        })).toEqual(expect.objectContaining({
            projectPath: "D:/tasks/remote",
            taskTitle: "Fix release",
            agentMode: "remote_coding_dev",
            remoteHost: "ssh.example.test",
            remoteNeedsReconnect: true,
        }));
    });

    it("drops stale remote reconnect state for local and ordinary project launches", () => {
        expect(normalizeCodingTaskLaunch({ projectPath: "D:/tasks/local", taskTitle: "Local", agentMode: "coding_dev", remoteNeedsReconnect: true })?.remoteNeedsReconnect).toBeUndefined();
        expect(normalizeCodingTaskLaunch({ projectPath: "D:/tasks/plain", taskTitle: "Plain", remoteNeedsReconnect: true })?.remoteNeedsReconnect).toBeUndefined();
    });

    it("preserves a trimmed caller correlation ID", () => {
        expect(normalizeCodingTaskLaunch({
            launchId: " skill-run-42 ",
            projectPath: "D:/tasks/skill",
            taskTitle: "Run skill",
        })?.launchId).toBe("skill-run-42");
    });

    it("preserves diagnosis safety only for remote launches", () => {
        expect(normalizeCodingTaskLaunch({
            projectPath: "D:/tasks/incident",
            taskTitle: "Diagnose incident",
            agentMode: "remote_coding_dev",
            remoteSafety: "diagnosis",
        })?.remoteSafety).toBe("diagnosis");
        expect(normalizeCodingTaskLaunch({
            projectPath: "D:/tasks/local",
            taskTitle: "Local",
            agentMode: "coding_dev",
            remoteSafety: "diagnosis",
        })?.remoteSafety).toBeUndefined();
    });

    it("keeps only trimmed, display-safe new-task context", () => {
        expect(normalizeCodingTaskLaunch({
            projectPath: " D:/tasks/new ",
            taskTitle: " New task ",
            agentMode: "remote_coding_dev",
            newTaskContext: {
                kind: "new-task",
                workingDir: " D:/local-workspace ",
                remoteWorkDir: " /srv/project ",
                remoteUser: " deploy ",
                remotePort: 2222,
            },
        })?.newTaskContext).toEqual({
            kind: "new-task",
            workingDir: "D:/local-workspace",
            remoteWorkDir: "/srv/project",
            remoteUser: "deploy",
            remotePort: 2222,
        });
    });
    it("drops unsafe remote port values from new-task context", () => {
        expect(normalizeCodingTaskLaunch({
            projectPath: "D:/tasks/new",
            taskTitle: "New task",
            newTaskContext: { kind: "new-task", remotePort: 70000 },
        })?.newTaskContext?.remotePort).toBeUndefined();
    });
    it("rejects an empty project path", () => {
        expect(normalizeCodingTaskLaunch({ taskTitle: "No path" })).toBeNull();
    });
});

describe("coding task runtime", () => {
    it("uses one lifecycle gate for both preparing and disconnected remote tasks", () => {
        expect(resolveCodingTaskPhase({ preparing: true, remoteNeedsReconnect: false })).toBe("preparing");
        expect(resolveCodingTaskPhase({ agentMode: "remote_coding_dev", preparing: true, remoteNeedsReconnect: false })).toBe("preparing");
        expect(resolveCodingTaskPhase({ agentMode: "remote_coding_dev", preparing: false, remoteNeedsReconnect: true })).toBe("reconnect_required");
        expect(canDispatchCodingIntent("preparing")).toBe(false);
        expect(canDispatchCodingIntent("reconnect_required")).toBe(false);
        expect(canDispatchCodingIntent("ready")).toBe(true);
    });
});
