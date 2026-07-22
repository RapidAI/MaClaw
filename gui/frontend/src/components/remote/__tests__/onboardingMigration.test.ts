import { describe, expect, it } from "vitest";
import { vi } from "vitest";
import {
    completeOnboardingAfterMigration,
    findOnboardingMigrationPackage,
    isMigrationJobRunning,
    isTerminalMigrationJob,
    migrationErrorMessage,
    migrationJobId,
    migrationJobStatus,
    migrationProgressPercent,
    optimisticMigrationRunningJob,
    pollUntilMigrationJobTerminal,
    shouldShowMigrationPassword,
} from "../onboardingMigration";

describe("onboarding migration helpers", () => {
    it("finds a ready package from machine instances", () => {
        expect(findOnboardingMigrationPackage({}, { instances: [{
            has_export: true,
            export_id: "mig-1",
            export_status: "ready",
            machine_id: "old-machine",
            machine_name: "Office PC",
            export_size: 2048,
        }] })).toMatchObject({ exportId: "mig-1", sourceMachineName: "Office PC", size: 2048 });
    });

    it("falls back to the current export and ignores unavailable packages", () => {
        expect(findOnboardingMigrationPackage({ current_export: {
            export_id: "mig-current",
            status: "ready",
            source_machine_name: "Old Mac",
        } }, { instances: [] })?.exportId).toBe("mig-current");
        expect(findOnboardingMigrationPackage({ current_export: {
            export_id: "mig-busy",
            status: "importing",
        } }, { instances: [] })).toBeNull();
    });

    it("resumes an importing package only when claimed by the current machine", () => {
        const row = {
            has_export: true,
            export_id: "mig-resume",
            export_status: "importing",
            export_claimed_by_machine_id: "machine-new",
        };
        expect(findOnboardingMigrationPackage({ machine_id: "machine-new" }, { instances: [row] })?.exportId).toBe("mig-resume");
        expect(findOnboardingMigrationPackage({ machine_id: "machine-other" }, { instances: [row] })).toBeNull();
    });

    it("normalizes job completion, progress, and password visibility", () => {
        expect(isTerminalMigrationJob("succeeded")).toBe(true);
        expect(isTerminalMigrationJob("running")).toBe(false);
        expect(migrationJobStatus(" Running ")).toBe("running");
        expect(isMigrationJobRunning("running")).toBe(true);
        expect(isMigrationJobRunning("failed", true)).toBe(true);
        expect(shouldShowMigrationPassword(null)).toBe(true);
        expect(shouldShowMigrationPassword({ status: "running" })).toBe(false);
        expect(shouldShowMigrationPassword({ status: "failed" })).toBe(true);
        expect(migrationJobId({ id: "  mig_1  " })).toBe("mig_1");
        expect(migrationProgressPercent(1.4)).toBe(100);
        expect(migrationProgressPercent(0.456)).toBe(46);
        expect(migrationErrorMessage(new Error("password is incorrect"))).toBe("password is incorrect");
        expect(migrationErrorMessage({ message: "hub down" })).toBe("hub down");
        expect(optimisticMigrationRunningJob({ id: "old-job", status: "failed", progress: 0.55, progress_text: "downloading" })).toEqual({
            status: "running",
            error: "",
            progress: 0.55,
            progress_text: "downloading",
        });
        expect(migrationJobId(optimisticMigrationRunningJob({ id: "old-job" }))).toBe("");
    });

    it("polls until the migration job reaches a terminal status", async () => {
        const updates: string[] = [];
        const getJob = vi.fn()
            .mockResolvedValueOnce({ id: "job-1", status: "running", progress: 0.2 })
            .mockResolvedValueOnce({ id: "job-1", status: "succeeded", progress: 1 });

        const result = await pollUntilMigrationJobTerminal("job-1", getJob, {
            intervalMs: 0,
            sleep: async () => {},
            initialJob: { id: "job-1", status: "running", progress: 0 },
            onUpdate: (job) => updates.push(String(job.status)),
        });

        expect(result).toMatchObject({ id: "job-1", status: "succeeded", progress: 1 });
        expect(getJob).toHaveBeenCalledTimes(2);
        expect(updates).toEqual(["running", "succeeded"]);
    });

    it("tolerates transient poll failures before succeeding", async () => {
        const getJob = vi.fn()
            .mockRejectedValueOnce(new Error("temporary bridge error"))
            .mockResolvedValueOnce(null)
            .mockResolvedValueOnce({ id: "job-2", status: "succeeded", progress: 1 });

        const result = await pollUntilMigrationJobTerminal("job-2", getJob, {
            intervalMs: 0,
            maxFailures: 3,
            sleep: async () => {},
            initialJob: { id: "job-2", status: "running" },
        });

        expect(result.status).toBe("succeeded");
        expect(getJob).toHaveBeenCalledTimes(3);
    });

    it("fails after exhausting consecutive poll failures", async () => {
        const getJob = vi.fn().mockRejectedValue(new Error("migration job job-3 not found"));

        await expect(pollUntilMigrationJobTerminal("job-3", getJob, {
            intervalMs: 0,
            maxFailures: 2,
            sleep: async () => {},
            initialJob: { id: "job-3", status: "running" },
        })).rejects.toThrow(/not found/);

        expect(getJob).toHaveBeenCalledTimes(2);
    });

    it("rejects mismatched job ids as unavailable status", async () => {
        const getJob = vi.fn().mockResolvedValue({ id: "other-job", status: "succeeded", progress: 1 });

        await expect(pollUntilMigrationJobTerminal("job-5", getJob, {
            intervalMs: 0,
            maxFailures: 1,
            sleep: async () => {},
            initialJob: { id: "job-5", status: "running" },
        })).rejects.toThrow(/status unavailable/);

        expect(getJob).toHaveBeenCalledTimes(1);
    });

    it("stops polling when cancelled without throwing", async () => {
        let cancelled = false;
        const getJob = vi.fn().mockImplementation(async () => {
            cancelled = true;
            return { id: "job-4", status: "running", progress: 0.4 };
        });

        const result = await pollUntilMigrationJobTerminal("job-4", getJob, {
            intervalMs: 0,
            sleep: async () => {},
            isCancelled: () => cancelled,
            initialJob: { id: "job-4", status: "running", progress: 0.1 },
            onUpdate: () => {},
        });

        expect(result).toMatchObject({ id: "job-4", status: "running", progress: 0.4 });
        expect(getJob).toHaveBeenCalledTimes(1);
    });

    it("closes after durable completion and isolates synchronous refresh failures", async () => {
        const events: string[] = [];
        const onRefreshError = vi.fn();
        await completeOnboardingAfterMigration({
            markComplete: async () => { events.push("complete"); },
            close: () => { events.push("close"); },
            refresh: () => { throw new Error("refresh failed"); },
            onRefreshError,
        });

        expect(events).toEqual(["complete", "close"]);
        expect(onRefreshError).toHaveBeenCalledWith(expect.objectContaining({ message: "refresh failed" }));
    });

    it("does not close when durable completion fails", async () => {
        const close = vi.fn();
        await expect(completeOnboardingAfterMigration({
            markComplete: () => Promise.reject(new Error("save failed")),
            close,
        })).rejects.toThrow("save failed");
        expect(close).not.toHaveBeenCalled();
    });
});
