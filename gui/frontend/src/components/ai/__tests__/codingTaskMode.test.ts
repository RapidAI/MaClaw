import { describe, expect, it } from "vitest";
import {
    agentModeFromTaskTags,
    cloudWorkspaceIdFromTags,
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
