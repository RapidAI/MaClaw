/** @vitest-environment jsdom */
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PetCompanionSurface } from "./PetCompanionSurface";

const handlers = new Map<string, (payload?: unknown) => void>();

vi.mock("../../../wailsjs/runtime", () => ({
    EventsOn: (name: string, cb: (payload?: unknown) => void) => {
        handlers.set(name, cb);
        return () => handlers.delete(name);
    },
}));

vi.mock("../../../wailsjs/go/main/App", () => ({
    GetPetCompanionTranscript: vi.fn().mockResolvedValue("你：写一份周报\n码卡龙：周报好了。"),
}));

describe("PetCompanionSurface", () => {
    beforeEach(() => {
        handlers.clear();
    });

    it("opens the pet record without replacing the main conversation", async () => {
        render(<PetCompanionSurface />);
        await act(async () => {
            handlers.get("open-pet-conversation")?.();
        });
        expect(await screen.findByRole("dialog", { name: "宠物对话" })).toBeTruthy();
        expect(screen.getByText(/周报好了/)).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "关闭" }));
        expect(screen.queryByRole("dialog", { name: "宠物对话" })).toBeNull();
    });

    it("shows a quiet caption and lets it expire", async () => {
        vi.useFakeTimers();
        render(<PetCompanionSurface />);
        act(() => {
            handlers.get("pet-companion-bubble")?.({ text: "文稿先留在这儿" });
        });
        expect(screen.getByRole("status").textContent).toContain("文稿先留在这儿");
        act(() => {
            vi.advanceTimersByTime(4000);
        });
        expect(screen.queryByRole("status")).toBeNull();
        vi.useRealTimers();
    });
});
