import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { lightTheme } from "./aiAssistantPanelTheme";
import { ChatSessionLocator } from "./ChatSessionLocator";

const turns = [
    { id: "long", label: "根据以下宠物规范，开发一个奇安信公司老虎形象，要求写实" },
    { id: "short", label: "继续" },
];

describe("ChatSessionLocator", () => {
    it("opens every user command on hover and jumps when one is clicked", () => {
        const onJump = vi.fn();
        const onWarm = vi.fn();
        render(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={onJump} onWarm={onWarm} />);
        const rail = screen.getByTestId("chat-session-locator");
        expect(rail.className).not.toContain("is-open");
        expect(onWarm).not.toHaveBeenCalled();
        const label = rail.querySelector(".chat-session-locator-label") as HTMLElement;
        expect(label.textContent).toBe(turns[0].label);
        expect(label.className).toContain("chat-session-locator-label");

        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        fireEvent.mouseEnter(list);
        expect(rail.className).toContain("is-open");
        expect(onWarm).toHaveBeenCalledTimes(1);
        fireEvent.focus(list);
        expect(onWarm).toHaveBeenCalledTimes(2);
        fireEvent.click(screen.getAllByTestId("chat-session-locator-turn")[1]);
        expect(onJump).toHaveBeenCalledWith("short");
        expect(rail.className).not.toContain("is-open");
    });

    it("leaves focus where it was when a command is clicked", () => {
        const onJump = vi.fn();
        render(
            <>
                <button type="button" data-testid="composer">输入</button>
                <ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={onJump} />
            </>,
        );
        const composer = screen.getByTestId("composer");
        act(() => { composer.focus(); });
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        fireEvent.mouseEnter(list);
        const row = screen.getAllByTestId("chat-session-locator-turn")[1];
        fireEvent.mouseDown(row);
        fireEvent.click(row);
        expect(onJump).toHaveBeenCalledWith("short");
        expect(rail.className).not.toContain("is-open");
        expect(document.activeElement).toBe(composer);
    });

    it("blurs the rail before the jump so a closing dialog cannot reopen it", () => {
        render(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        fireEvent.mouseEnter(list);
        const row = screen.getAllByTestId("chat-session-locator-turn")[0];
        act(() => { row.focus(); });
        fireEvent.click(row);
        expect(rail.className).not.toContain("is-open");
        expect(list.contains(document.activeElement)).toBe(false);
    });

    it("asks for older commands on mount only when no command is on screen", () => {
        const onWarm = vi.fn();
        const { rerender } = render(<ChatSessionLocator turns={[]} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} onWarm={onWarm} />);
        expect(screen.queryByTestId("chat-session-locator")).toBeNull();
        expect(onWarm).toHaveBeenCalledTimes(1);
        rerender(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} onWarm={onWarm} />);
        expect(screen.getByTestId("chat-session-locator")).toBeTruthy();
        expect(onWarm).toHaveBeenCalledTimes(1);
    });

    it("closes on Escape without the focus return opening it again", () => {
        render(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        fireEvent.mouseEnter(list);
        const button = screen.getAllByTestId("chat-session-locator-turn")[0];
        act(() => { button.focus(); });
        fireEvent.keyDown(button, { key: "Escape" });
        expect(rail.className).not.toContain("is-open");
        expect(document.activeElement).toBe(list);
    });

    it("rolls the transcript from the ticks, and keeps the wheel inside an overflowing list", () => {
        const onTranscriptWheel = vi.fn();
        render(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} onTranscriptWheel={onTranscriptWheel} />);
        const list = screen.getByTestId("chat-session-locator").querySelector(".chat-session-locator-list") as HTMLElement;
        fireEvent.wheel(list, { deltaY: -80 });
        expect(onTranscriptWheel).toHaveBeenCalledWith(-80);
        fireEvent.mouseEnter(list);
        Object.defineProperty(list, "scrollHeight", { configurable: true, value: 400 });
        Object.defineProperty(list, "clientHeight", { configurable: true, value: 80 });
        fireEvent.wheel(list, { deltaY: 40 });
        expect(onTranscriptWheel).toHaveBeenCalledTimes(1);
        fireEvent.wheel(list, { deltaY: -40 });
        expect(onTranscriptWheel).toHaveBeenLastCalledWith(-40);
    });

    it("keeps the latest command in view until the list is scrolled upward", () => {
        const many = Array.from({ length: 30 }, (_, index) => ({ id: `t${index}`, label: `命令 ${index}` }));
        const { rerender } = render(<ChatSessionLocator turns={many} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const list = screen.getByTestId("chat-session-locator").querySelector(".chat-session-locator-list") as HTMLElement;
        Object.defineProperty(list, "scrollHeight", { configurable: true, value: 800 });
        Object.defineProperty(list, "clientHeight", { configurable: true, value: 200 });
        list.scrollTop = 40;
        fireEvent.scroll(list);
        expect(list.scrollTop).toBe(800);
        rerender(<ChatSessionLocator turns={[...many, { id: "new", label: "新的" }]} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        expect(list.scrollTop).toBe(800);
        fireEvent.mouseEnter(list);
        list.scrollTop = 40;
        fireEvent.scroll(list);
        rerender(<ChatSessionLocator turns={[...many, { id: "new", label: "新的" }, { id: "newer", label: "更新" }]} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        expect(list.scrollTop).toBe(40);
    });

    it("renders nothing when the conversation has no user commands", () => {
        render(<ChatSessionLocator turns={[]} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        expect(screen.queryByTestId("chat-session-locator")).toBeNull();
    });
});
