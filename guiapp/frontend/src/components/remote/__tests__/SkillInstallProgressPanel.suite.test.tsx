import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SkillInstallProgressPanel } from "../SkillInstallProgressPanel";

let listeners: Record<string, (payload: any) => void> = {};
vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((name: string, callback: (payload: any) => void) => {
        listeners[name] = callback;
        return () => { delete listeners[name]; };
    }),
}));

const localize = (en: string) => en;

describe("SkillInstallProgressPanel Suite events", () => {
    beforeEach(() => { listeners = {}; });

    it("renders progress for each Suite member", async () => {
        render(<SkillInstallProgressPanel active localizeText={localize} />);
        listeners["skill-suite-install-progress"]({ suite_id: "office", skill: "pdf", index: 1, total: 2, phase: "installing", status: "Installing" });
        listeners["skill-suite-install-progress"]({ suite_id: "office", skill: "sheets", index: 2, total: 2, phase: "done", status: "Done" });
        expect(await screen.findByText(/pdf: Installing/)).toBeTruthy();
        expect(await screen.findByText(/sheets: Done/)).toBeTruthy();
    });
});
