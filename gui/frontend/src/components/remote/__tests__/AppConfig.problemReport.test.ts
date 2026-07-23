import { describe, expect, it } from "vitest";

import { main } from "../../../../wailsjs/go/models";

describe("Wails AppConfig problem-report fields", () => {
    it("retains a restored diagnostic collection session", () => {
        const config = new main.AppConfig({
            bug_report_enabled: true,
            bug_report_previous_trajectory: false,
            bug_report_previous_log_detail: true,
            llm_trajectory_logging: true,
            log_detail_enabled: true,
        });

        expect(config.bug_report_enabled).toBe(true);
        expect(config.bug_report_previous_trajectory).toBe(false);
        expect(config.bug_report_previous_log_detail).toBe(true);
    });

    it("does not let an empty source override restored fields", () => {
        const restored = new main.AppConfig({
            bug_report_enabled: true,
            bug_report_previous_trajectory: true,
            bug_report_previous_log_detail: false,
        });

        const updated = new main.AppConfig({ ...restored });

        expect(updated.bug_report_enabled).toBe(true);
        expect(updated.bug_report_previous_trajectory).toBe(true);
        expect(updated.bug_report_previous_log_detail).toBe(false);
    });
});
