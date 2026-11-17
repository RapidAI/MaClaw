import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentBrowserStage, containedImagePoint, mergeAgentBrowserStage, type AgentBrowserStageState } from "./AgentBrowserStage";

const base: AgentBrowserStageState = {
    open: true,
    sessionId: "browser-session-1",
    url: "https://example.com",
    title: "Example",
    frame: "",
    width: 1280,
    height: 800,
    userHold: false,
    askReason: "",
    seq: 1,
};

describe("containedImagePoint", () => {
    it("maps a point inside the letterboxed image to a fraction", () => {
        const point = containedImagePoint(200, 100, 100, 100, 75, 50);
        expect(point).toEqual({ x: 0.25, y: 0.5 });
    });

    it("ignores clicks in the empty bars", () => {
        expect(containedImagePoint(200, 100, 100, 100, 10, 50)).toBeNull();
    });
});

describe("mergeAgentBrowserStage", () => {
    it("keeps the previous frame when a newer event omits it", () => {
        const prev = { ...base, frame: "abc", seq: 2 };
        const next = mergeAgentBrowserStage(prev, { open: true, seq: 3, url: "https://example.com/next" });
        expect(next?.frame).toBe("abc");
        expect(next?.url).toBe("https://example.com/next");
    });

    it("drops an older sequence", () => {
        const prev = { ...base, seq: 5, title: "now" };
        expect(mergeAgentBrowserStage(prev, { open: true, seq: 4, title: "old" })?.title).toBe("now");
    });
});

describe("AgentBrowserStage", () => {
    it("offers watch and take control, and reports the choice", () => {
        const onSetHold = vi.fn();
        render(<AgentBrowserStage state={base} lang="zh-Hans" onSetHold={onSetHold} />);
        expect(screen.getByTestId("agent-browser-watch")).toBeTruthy();
        fireEvent.click(screen.getByTestId("agent-browser-take-control"));
        expect(onSetHold).toHaveBeenCalledWith(true);
        expect(screen.getByTestId("agent-browser-shield")).toBeTruthy();
    });

    it("sends a pointer fraction only while the person holds the page", () => {
        const onInput = vi.fn();
        const box = { left: 0, top: 0, right: 100, bottom: 100, width: 100, height: 100, x: 0, y: 0, toJSON() { return {}; } };
        const { rerender } = render(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100 }} lang="en" onInput={onInput} />);
        const img = screen.getByTestId("agent-browser-frame") as HTMLImageElement;
        img.getBoundingClientRect = () => box;
        fireEvent.pointerDown(img, { clientX: 25, clientY: 40, pointerId: 1 });
        expect(onInput).not.toHaveBeenCalled();
        rerender(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100, userHold: true }} lang="en" onInput={onInput} />);
        const held = screen.getByTestId("agent-browser-frame") as HTMLImageElement;
        held.getBoundingClientRect = () => box;
        fireEvent.pointerDown(held, { clientX: 25, clientY: 40, pointerId: 1 });
        expect(onInput).toHaveBeenCalled();
        const payload = JSON.parse(String(onInput.mock.calls[0][0]));
        expect(payload.kind).toBe("down");
        expect(payload.x).toBeCloseTo(0.25);
        expect(payload.y).toBeCloseTo(0.4);
    });
});
