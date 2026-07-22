import { describe, expect, it } from "vitest";
import { buildTaskMetaRows } from "../ScheduledTasksPanel";

const t = (en: string, zh: string) => zh;

describe("buildTaskMetaRows", () => {
    const baseTask = {
        id: "t1",
        name: "daily-build",
        action: "rebuild project",
        hour: 20,
        minute: 0,
        day_of_week: -1,
        day_of_month: 0,
        interval_minutes: 0,
        start_date: "2026-07-20",
        end_date: "2026-07-31",
        task_type: "reminder",
        status: "active",
        created_at: "",
        updated_at: "",
        next_run_at: "2026-07-22T20:00:00Z",
        last_run_at: null,
        last_result: "ok",
        last_error: "",
        run_count: 7,
        delivery: null,
    } as any;

    it("builds left-aligned meta rows for core fields", () => {
        const rows = buildTaskMetaRows(baseTask, "zh-Hans", t);
        expect(rows.map((r) => r.label)).toEqual(["执行", "计划", "下次", "已执行"]);
        expect(rows[0].value).toBe("rebuild project");
        expect(rows.find((r) => r.label === "已执行")?.value).toBe("7");
    });

    it("includes push row when delivery text is provided", () => {
        const rows = buildTaskMetaRows(baseTask, "zh-Hans", t, "lansenger → 群:ops");
        expect(rows.some((r) => r.label === "推送" && r.value.includes("ops"))).toBe(true);
    });

    it("omits next/runs when empty", () => {
        const rows = buildTaskMetaRows({
            ...baseTask,
            next_run_at: null,
            run_count: 0,
            action: "",
        }, "en", (en) => en);
        expect(rows.map((r) => r.label)).toEqual(["Action", "Schedule"]);
        expect(rows[0].value).toBe("-");
    });
});
