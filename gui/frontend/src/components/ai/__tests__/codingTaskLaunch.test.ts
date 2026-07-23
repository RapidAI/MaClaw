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
