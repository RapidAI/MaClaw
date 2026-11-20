import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { lightTheme } from "./aiAssistantPanelTheme";
import { ChatSessionLocator } from "./ChatSessionLocator";

const turns = [
    { id: "long", label: "根据以下宠物规范，开发一个奇安信公司老虎形象，要求写实" },
    { id: "short", label: "继续" },
];

describe("ChatSessionLocator", () => {
    it("opens the rail on hover and jumps when a tick is clicked", () => {
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
        expect(screen.queryByTestId("chat-session-locator-current")).toBeNull();
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

    it("closes when the pointer leaves after Escape left focus on the list", () => {
        render(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        fireEvent.mouseEnter(list);
        const button = screen.getAllByTestId("chat-session-locator-turn")[0];
        act(() => { button.focus(); });
        fireEvent.keyDown(button, { key: "Escape" });
        expect(document.activeElement).toBe(list);
        fireEvent.mouseEnter(list);
        fireEvent.mouseEnter(button);
        expect(rail.className).toContain("is-open");
        fireEvent.mouseLeave(list);
        expect(rail.className).not.toContain("is-open");
        expect(screen.queryByTestId("chat-session-locator-current")).toBeNull();
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

    it("draws each tick at 10px, with the closed row still 18px wide", () => {
        const css = readFileSync(resolve(process.cwd(), "src/components/ai/ChatSessionLocator.css"), "utf8");
        expect(css).toContain("width: 10px;");
        expect(css).toContain("height: 2px;");
        expect(css).toContain("padding: 3px 4px;");
        expect(css).toContain('width: 13px;');
        expect(css).toContain('width: 16px;');
        expect(css).toContain('width: 20px;');
        expect(css).not.toContain("width: 14px;");
        expect(css).not.toContain("max-height: 100%;");
        expect(css).not.toMatch(/\.chat-session-locator\.is-open \.chat-session-locator-label/);
        expect(css).toContain(".chat-session-locator-current");
        expect(css).toContain('data-current-visible="false"');
    });

    it("brightens the pointed tick, lengthens the two beside it, and shows only that topic", () => {
        const many = ["一", "二", "三", "当前话题", "五", "六", "七"].map((label, index) => ({ id: `t${index}`, label }));
        render(<ChatSessionLocator turns={many} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        const rows = screen.getAllByTestId("chat-session-locator-turn");
        const box = (top: number, height: number) => ({
            top, height, left: 0, right: 0, bottom: top + height, width: 0, x: 0, y: top, toJSON() { return {}; },
        }) as DOMRect;
        // A 2x ancestor transform inflates client rects. The line uses offsetTop.
        vi.spyOn(rail, "getBoundingClientRect").mockReturnValue(box(100, 800));
        Object.defineProperty(rail, "offsetHeight", { configurable: true, value: 400 });
        vi.spyOn(rows[3], "getBoundingClientRect").mockReturnValue(box(260, 16));
        Object.defineProperty(rows[3], "offsetTop", { configurable: true, value: 80 });
        Object.defineProperty(rows[3], "offsetHeight", { configurable: true, value: 8 });
        Object.defineProperty(rows[3], "offsetParent", { configurable: true, get: () => rail });
        fireEvent.mouseEnter(list);
        fireEvent.mouseEnter(rows[3]);
        expect(rows.map((row) => row.getAttribute("data-near"))).toEqual([null, "2", "1", "0", "1", "2", null]);
        expect(rows[3].getAttribute("aria-current")).toBe("true");
        expect(rows.filter((row) => row.getAttribute("aria-current") === "true")).toHaveLength(1);
        const current = screen.getByTestId("chat-session-locator-current");
        expect(current.textContent).toBe("当前话题");
        expect(current.getAttribute("title")).toBe("当前话题");
        expect(rail.querySelectorAll(".chat-session-locator-current")).toHaveLength(1);
        expect(rail.style.getPropertyValue("--csl-current-top")).toBe("84px");

        Object.defineProperty(rows[3], "offsetTop", { configurable: true, value: 130 });
        let scrolled = 10;
        Object.defineProperty(list, "scrollTop", {
            configurable: true,
            get: () => scrolled,
            set: (value) => { scrolled = Number(value); },
        });
        fireEvent.scroll(list);
        expect(rail.style.getPropertyValue("--csl-current-top")).toBe("124px");

        act(() => { rows[4].focus(); });
        expect(rows[4].getAttribute("data-near")).toBe("0");
        expect(rows[3].getAttribute("data-near")).toBe("1");
        expect(screen.getByTestId("chat-session-locator-current").textContent).toBe("五");

        act(() => { (document.activeElement as HTMLElement).blur(); });
        fireEvent.mouseEnter(list);
        fireEvent.mouseEnter(rows[3]);
        fireEvent.mouseLeave(list);
        expect(rail.className).not.toContain("is-open");
        expect(rows.every((row) => row.getAttribute("data-near") == null)).toBe(true);
        expect(screen.queryByTestId("chat-session-locator-current")).toBeNull();
    });

    it("keeps the topic while the pointer moves onto it, and jumps from that line", () => {
        const onJump = vi.fn();
        render(<ChatSessionLocator turns={turns} lang="zh-Hans" theme={lightTheme} onJump={onJump} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        const rows = screen.getAllByTestId("chat-session-locator-turn");
        fireEvent.mouseEnter(list);
        fireEvent.mouseEnter(rows[1]);
        const current = screen.getByTestId("chat-session-locator-current");
        fireEvent.mouseLeave(list, { relatedTarget: current });
        expect(rail.className).toContain("is-open");
        expect(current.textContent).toBe("继续");
        act(() => { rows[1].focus(); });
        fireEvent.mouseLeave(current);
        expect(rail.className).toContain("is-open");
        fireEvent.mouseDown(current);
        fireEvent.click(current);
        expect(onJump).toHaveBeenCalledWith("short");
        expect(rail.className).not.toContain("is-open");
        expect(list.contains(document.activeElement)).toBe(false);
        expect(screen.queryByTestId("chat-session-locator-current")).toBeNull();
    });

    it("keeps a scrolled tick window when the pointer returns from the topic line", () => {
        const many = Array.from({ length: 30 }, (_, index) => ({ id: `t${index}`, label: `命令 ${index}` }));
        const { rerender } = render(<ChatSessionLocator turns={many} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        Object.defineProperty(list, "scrollHeight", { configurable: true, value: 800 });
        Object.defineProperty(list, "clientHeight", { configurable: true, value: 200 });
        fireEvent.mouseEnter(list);
        list.scrollTop = 40;
        fireEvent.scroll(list);
        fireEvent.mouseEnter(screen.getAllByTestId("chat-session-locator-turn")[0]);
        const current = screen.getByTestId("chat-session-locator-current");
        fireEvent.mouseLeave(list, { relatedTarget: current });
        fireEvent.mouseEnter(list);
        rerender(<ChatSessionLocator turns={[...many, { id: "new", label: "新的" }]} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        expect(list.scrollTop).toBe(40);
    });

    it("hides the topic when its tick scrolls out of the window", () => {
        const many = ["一", "二", "三", "当前话题", "五", "六", "七"].map((label, index) => ({ id: `t${index}`, label }));
        render(<ChatSessionLocator turns={many} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        const rail = screen.getByTestId("chat-session-locator");
        const list = rail.querySelector(".chat-session-locator-list") as HTMLElement;
        const row = screen.getAllByTestId("chat-session-locator-turn")[3];
        Object.defineProperty(list, "offsetTop", { configurable: true, value: 40 });
        Object.defineProperty(list, "clientHeight", { configurable: true, value: 80 });
        Object.defineProperty(list, "offsetParent", { configurable: true, get: () => rail });
        Object.defineProperty(row, "offsetTop", { configurable: true, value: 64 });
        Object.defineProperty(row, "offsetHeight", { configurable: true, value: 8 });
        Object.defineProperty(row, "offsetParent", { configurable: true, get: () => rail });
        fireEvent.mouseEnter(list);
        fireEvent.mouseEnter(row);
        expect(rail.dataset.currentVisible).toBe("true");
        Object.defineProperty(row, "offsetTop", { configurable: true, value: 200 });
        fireEvent.scroll(list);
        expect(rail.dataset.currentVisible).toBe("false");
        expect(screen.getByTestId("chat-session-locator-current").textContent).toBe("当前话题");
    });

    it("renders nothing when the conversation has no user commands", () => {
        render(<ChatSessionLocator turns={[]} lang="zh-Hans" theme={lightTheme} onJump={vi.fn()} />);
        expect(screen.queryByTestId("chat-session-locator")).toBeNull();
    });
});
