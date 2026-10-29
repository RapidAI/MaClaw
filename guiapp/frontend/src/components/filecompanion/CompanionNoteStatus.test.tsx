// @vitest-environment jsdom
import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setCompanionUiLang } from "../../i18n/mnCompanion";
import { CompanionStatus } from "./CompanionNoteStatus";
import { companionNoteStatus, noteStatusPercent } from "./noteStatusModel";

afterEach(() => {
    vi.useRealTimers();
    setCompanionUiLang("zh-Hans");
});

describe("CompanionStatus", () => {
    it("renders the meter for a note and a plain line otherwise", () => {
        const { rerender } = render(<CompanionStatus text="正在记录声音" kind="web" />);
        expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("35");
        expect(screen.getByText("声音")).toBeTruthy();
        rerender(<CompanionStatus text="临时进度" kind="web" />);
        expect(screen.queryByRole("progressbar")).toBeNull();
        expect(screen.getByTestId("file-companion-progress").textContent).toBe("临时进度");
    });

    it("keeps the note clock and starts the next phrase from its own mark", () => {
        vi.useFakeTimers();
        const start = Date.now();
        const { rerender } = render(<CompanionStatus text="正在记录声音" kind="web" noteStartedAt={start} phaseStartedAt={start} />);
        act(() => { vi.advanceTimersByTime(5 * 60 * 1000); });
        expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("05:00");
        rerender(<CompanionStatus text="正在整理笔记" kind="web" noteStartedAt={start} phaseStartedAt={Date.now()} />);
        expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("05:00");
        expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe(
            String(noteStatusPercent(companionNoteStatus("正在整理笔记", "web")!, 0)),
        );
    });

    it("shows Khalkha chrome while the stage still follows the Chinese status", () => {
        render(<CompanionStatus text="正在记录声音" kind="web" lang="mn" />);
        const bar = screen.getByRole("progressbar");
        expect(bar.getAttribute("aria-valuenow")).toBe("35");
        expect(bar.getAttribute("aria-valuetext")).toBe("Дуу бичиж байна");
        expect(screen.getByText("Тэмдэглэл")).toBeTruthy();
        expect(screen.getByText("Дуу")).toBeTruthy();
        expect(screen.queryByText("声音")).toBeNull();
        expect(screen.queryByText("笔记")).toBeNull();
    });
});
