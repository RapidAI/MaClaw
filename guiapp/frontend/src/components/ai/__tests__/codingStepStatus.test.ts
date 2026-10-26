import { describe, it, expect } from "vitest";
import {
    codingChecklistAllDone,
    codingStepIsActive,
    codingStepIsDone,
    codingStepIsFailed,
    codingStepIsSettled,
    codingStepIsSkipped,
    presentCodingAgentChecklist,
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

describe("presentCodingAgentChecklist", () => {
    it("drops a runner failure painted onto an agent checklist", () => {
        const painted = [
            { index: 1, title: "定位通道", status: "failed", summary: "邮件已成功发送" },
            { index: 2, title: "编写脚本", status: "passed" },
            { index: 3, title: "验证发送", status: "passed" },
        ];
        const got = presentCodingAgentChecklist("", painted);
        expect(got.map((step) => step.status)).toEqual(["passed", "passed", "passed"]);
        expect(got[0].summary).toBe("");
        expect(painted[0].status).toBe("failed");
        expect(painted[0].summary).toBe("邮件已成功发送");
    });

    it("keeps a lone failure, a real plan failure, and a later skip", () => {
        expect(presentCodingAgentChecklist("", [
            { index: 1, status: "failed", summary: "guardrail" },
        ])[0].status).toBe("failed");
        expect(presentCodingAgentChecklist("### T1: 定位\n### T2: 发送", [
            { index: 1, status: "failed", summary: "guardrail" },
            { index: 2, status: "passed" },
        ])[0].summary).toBe("guardrail");
        const skipped = presentCodingAgentChecklist("", [
            { index: 1, status: "failed", summary: "guardrail" },
            { index: 2, status: "skipped", summary: "prior step failed" },
        ]);
        expect(skipped.map((step) => step.status)).toEqual(["failed", "skipped"]);
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

describe("codingChecklistAllDone", () => {
    it("retires a checklist whose rows all closed successfully", () => {
        expect(codingChecklistAllDone([
            { index: 1, status: "passed" },
            { index: 2, status: "done" },
            { index: 3, status: "skipped" },
        ])).toBe(true);
        expect(codingChecklistAllDone([{ index: 1, status: "passed" }])).toBe(true);
    });

    it("keeps a checklist with live, open, or failed rows", () => {
        expect(codingChecklistAllDone([
            { index: 1, status: "passed" },
            { index: 2, status: "running" },
        ])).toBe(false);
        expect(codingChecklistAllDone([
            { index: 1, status: "passed" },
            { index: 2, status: "pending" },
        ])).toBe(false);
        expect(codingChecklistAllDone([
            { index: 1, status: "failed" },
            { index: 2, status: "skipped" },
        ])).toBe(false);
        expect(codingChecklistAllDone([
            { index: 1, status: "verify_failed" },
            { index: 2, status: "passed" },
        ])).toBe(false);
    });

    it("does not treat an empty list as finished", () => {
        // An empty checklist still renders the requirement-understanding card.
        expect(codingChecklistAllDone([])).toBe(false);
        expect(codingChecklistAllDone(undefined)).toBe(false);
    });
});
