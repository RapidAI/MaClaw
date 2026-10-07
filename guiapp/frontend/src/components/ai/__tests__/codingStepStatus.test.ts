import { describe, it, expect } from "vitest";
import {
    codingStepIsActive,
    codingStepIsDone,
    codingStepIsFailed,
    codingStepIsSettled,
    codingStepIsSkipped,
} from "../codingStepStatus";
import { codingStepGlyph } from "../CodingWorkbenchControlPanel";

/**
 * The canonical set the coding workbench actually emits
 * (guiapp/coding_workbench_align.go: codingStepPending/Running/Passed/
 * Failed/Skipped/VerifyFail). If the backend adds a seventh status, this suite
 * is the place that should fail first and loudly.
 */
const CANONICAL = ["pending", "running", "passed", "failed", "skipped", "verify_failed"] as const;

describe("coding step vocabulary — canonical contract", () => {
    it("buckets every status the workbench emits", () => {
        const bucketOf = (s: string) =>
            codingStepIsDone(s)
                ? "done"
                : codingStepIsFailed(s)
                  ? "failed"
                  : codingStepIsActive(s)
                    ? "active"
                    : codingStepIsSkipped(s)
                      ? "skipped"
                      : "pending";
        expect(bucketOf("pending")).toBe("pending");
        expect(bucketOf("running")).toBe("active");
        expect(bucketOf("passed")).toBe("done");
        expect(bucketOf("failed")).toBe("failed");
        expect(bucketOf("skipped")).toBe("skipped");
        expect(bucketOf("verify_failed")).toBe("failed");
    });

    it("covers the canonical set with no unknown leftovers", () => {
        for (const s of CANONICAL) {
            const known =
                codingStepIsDone(s) ||
                codingStepIsFailed(s) ||
                codingStepIsActive(s) ||
                codingStepIsSkipped(s) ||
                s === "pending";
            expect(known, `${s} fell through every predicate`).toBe(true);
        }
    });

    it("gives every canonical status a distinct checklist mark", () => {
        expect(codingStepGlyph("pending")).toBe("☐");
        expect(codingStepGlyph("running")).toBe("…");
        expect(codingStepGlyph("passed")).toBe("☑");
        expect(codingStepGlyph("failed")).toBe("✗");
        expect(codingStepGlyph("verify_failed")).toBe("✗");
        expect(codingStepGlyph("skipped")).toBe("–");
    });
});

describe("coding step vocabulary — other producers", () => {
    it("accepts the agent-view runner spellings", () => {
        // guiapp/agent_view_step_status.go normalizes to done/running/error/pending.
        expect(codingStepIsDone("done")).toBe(true);
        expect(codingStepIsFailed("error")).toBe(true);
        expect(codingStepIsActive("running")).toBe(true);
    });

    it("accepts legacy spellings", () => {
        expect(codingStepIsDone("completed")).toBe(true);
        expect(codingStepIsDone("success")).toBe(true);
        expect(codingStepIsDone("succeeded")).toBe(true);
        expect(codingStepIsActive("in_progress")).toBe(true);
        expect(codingStepIsActive("started")).toBe(true);
        expect(codingStepIsSkipped("cancelled")).toBe(true);
        expect(codingStepIsSkipped("canceled")).toBe(true);
    });

    it("treats a non-canonical string as pending rather than throwing", () => {
        for (const s of ["", "unknown", "PASSÉD"]) {
            expect(codingStepIsDone(s)).toBe(false);
            expect(codingStepIsFailed(s)).toBe(false);
            expect(codingStepIsActive(s)).toBe(false);
        }
    });

    it("is case-insensitive", () => {
        expect(codingStepIsDone("Passed")).toBe(true);
        expect(codingStepIsFailed("Verify_Failed")).toBe(true);
        expect(codingStepIsActive("IN_PROGRESS")).toBe(true);
    });
});

describe("codingStepIsSettled", () => {
    it("covers done, failed and skipped but not live states", () => {
        expect(codingStepIsSettled("passed")).toBe(true);
        expect(codingStepIsSettled("failed")).toBe(true);
        expect(codingStepIsSettled("skipped")).toBe(true);
        expect(codingStepIsSettled("running")).toBe(false);
        expect(codingStepIsSettled("pending")).toBe(false);
    });
});
