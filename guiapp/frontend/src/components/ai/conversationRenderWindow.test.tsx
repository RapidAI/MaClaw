import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";
import {
    CONVERSATION_RENDER_MIN_MOUNTED_ROWS,
    conversationRenderCanWindow,
    conversationRenderLoadStart,
    conversationRenderRowHeightPx,
    conversationRenderTrimCeilingPx,
    conversationRenderTrimStart,
    conversationRenderTurnStart,
    scrollTopDeltaCssPx,
} from "./conversationRenderWindow";
import { useConversationRenderWindow } from "./useConversationRenderWindow";

interface Row {
    id: string;
    role: string;
    content: string;
}

function userRows(count: number): Row[] {
    return Array.from({ length: count }, (_, index) => ({ id: `m${index}`, role: "user", content: `body ${index}` }));
}

function alternating(count: number): Row[] {
    return Array.from({ length: count }, (_, index) => ({
        id: `m${index}`,
        role: index % 2 === 0 ? "user" : "assistant",
        content: `body ${index}`,
    }));
}

describe("conversationRenderTrimCeilingPx", () => {
    it("allows a long hot window and rejects the million-pixel balloon", () => {
        expect(conversationRenderTrimCeilingPx(758, 40)).toBe(758 * 40 * 8);
        expect(conversationRenderTrimCeilingPx(5000, 400)).toBe(1_000_000);
    });
});

describe("scrollTopDeltaCssPx", () => {
    function scaledBox(visualHeight: number, cssHeight: number): HTMLElement {
        const el = document.createElement("div");
        el.getBoundingClientRect = () => ({
            top: 0, bottom: visualHeight, left: 0, right: 10, width: 10, height: visualHeight, x: 0, y: 0, toJSON() { return {}; },
        });
        Object.defineProperty(el, "clientHeight", { value: cssHeight });
        return el;
    }

    it("converts a viewport wheel delta into local CSS pixels", () => {
        const box = scaledBox(200, 100);
        expect(scrollTopDeltaCssPx(40, box)).toBe(20);
        expect(scrollTopDeltaCssPx(-40, box)).toBe(-20);
        expect(scrollTopDeltaCssPx(40, scaledBox(100, 100))).toBe(40);
        expect(scrollTopDeltaCssPx(Number.NaN, box)).toBe(0);
    });
});

describe("conversation row paint", () => {
    it("keeps mounted transcript rows painted instead of skipping them", () => {
        const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../../styles/partials/05-conversation-layout.css"), "utf8");
        expect(css).toMatch(/\.mc-conversation-body > \[data-testid\^='assistant-chat-user-'\],\s*\.mc-conversation-body > \[data-testid\^='assistant-chat-ai-'\] \{\s*content-visibility: visible;\s*\}/);
    });
});

describe("conversationRenderTurnStart", () => {
    it("walks back to the user row that opens the turn", () => {
        expect(conversationRenderTurnStart(alternating(20), 9)).toBe(8);
        expect(conversationRenderTurnStart(alternating(20), 8)).toBe(8);
    });

    it("floors at the transcript head when no user row precedes the index", () => {
        const messages = Array.from({ length: 5 }, (_, index) => ({ id: `a${index}`, role: "assistant" }));
        expect(conversationRenderTurnStart(messages, 3)).toBe(0);
        expect(conversationRenderTurnStart(messages, 0)).toBe(0);
    });
});

describe("conversationRenderTrimStart", () => {
    it("stands down while the transcript still fits four screens", () => {
        const messages = userRows(40);
        expect(conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 2000 })).toBeNull();
        expect(conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 1000 })).toBeNull();
    });

    it("keeps about three screens of rows and drops the rest", () => {
        const messages = userRows(40);
        const next = conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 4000 });
        expect(next).toBe(25);
        expect(messages.length - (next as number)).toBe(15);
    });

    it("never starts the window inside a turn", () => {
        const messages = alternating(40);
        const next = conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 4000 });
        // Candidate 25 is an assistant row; the turn's user row is 24.
        expect(next).toBe(24);
        expect(messages[next as number].role).toBe("user");
    });

    it("keeps a long final turn whole", () => {
        const messages: Row[] = [
            ...userRows(20),
            { id: "turn", role: "user", content: "long turn" },
            ...Array.from({ length: 19 }, (_, index) => ({ id: `a${index}`, role: "assistant", content: "step" })),
        ];
        const next = conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 4000 });
        expect(next).toBe(20);
    });

    it("refuses to unmount down to a couple of rows", () => {
        const messages = userRows(CONVERSATION_RENDER_MIN_MOUNTED_ROWS);
        expect(conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 40000 })).toBeNull();
    });

    it("does nothing when the whole transcript is already mounted above the window", () => {
        const messages = userRows(40);
        expect(conversationRenderTrimStart({ messages, windowStart: 38, viewportPx: 500, contentPx: 4000 })).toBeNull();
    });

    it("needs a measurable viewport", () => {
        const messages = userRows(40);
        expect(conversationRenderCanWindow(0)).toBe(false);
        expect(conversationRenderTrimStart({ messages, windowStart: 0, viewportPx: 0, contentPx: 4000 })).toBeNull();
    });
});

describe("conversationRenderLoadStart", () => {
    it("pulls about three screens of older rows per batch", () => {
        const messages = userRows(40);
        // 16 mounted rows, 1600px: ~100px rows, so a 1500px batch is 15 rows.
        expect(conversationRenderLoadStart({ messages, windowStart: 24, viewportPx: 500, contentPx: 1600 })).toBe(9);
    });

    it("aligns the batch to a user turn start", () => {
        const messages = alternating(40);
        const next = conversationRenderLoadStart({ messages, windowStart: 24, viewportPx: 500, contentPx: 1600 });
        expect(next).toBe(8);
        expect(messages[next as number].role).toBe("user");
    });

    it("stops at the head of the transcript", () => {
        const messages = userRows(40);
        expect(conversationRenderLoadStart({ messages, windowStart: 0, viewportPx: 500, contentPx: 1600 })).toBeNull();
        expect(conversationRenderLoadStart({ messages, windowStart: 3, viewportPx: 500, contentPx: 1600 })).toBe(0);
    });

    it("floors the row height so a degenerate layout cannot divide by zero", () => {
        expect(conversationRenderRowHeightPx(0, 10)).toBeGreaterThan(0);
        const messages = userRows(40);
        // contentPx 0 means "nothing measured yet": the floor turns the batch
        // into the whole prefix instead of a NaN index.
        expect(conversationRenderLoadStart({ messages, windowStart: 30, viewportPx: 500, contentPx: 0 })).toBe(0);
    });
});

function Probe({ messages, latched }: { messages: Row[]; latched?: boolean }) {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const scrolledUpRef = useRef(false);
    if (latched !== undefined) scrolledUpRef.current = latched;
    const window = useConversationRenderWindow({
        containerRef,
        messages,
        conversationKey: "probe",
        scrolledUpRef,
        enabled: true,
    });
    return (
        <div ref={containerRef} data-testid="scroll">
            <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
            <div className="mc-conversation-body">
                {messages.slice(window.windowStart).map((message) => (
                    <div key={message.id} data-testid={`row-${message.id}`}>{message.content}</div>
                ))}
            </div>
            <span data-testid="start">{window.windowStart}</span>
            <button type="button" data-testid="reveal" onClick={() => window.revealMessage("m10")} />
        </div>
    );
}

/** 500px viewport, 100px per mounted row: four screens is 2000px. */
function mockPaneLayout(container: HTMLElement, bodyTop = 0) {
    Object.defineProperty(container, "clientHeight", { configurable: true, value: 500 });
    Object.defineProperty(container, "scrollHeight", {
        configurable: true,
        get: () => container.querySelectorAll('[data-testid^="row-"]').length * 100
            + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
    });
    const body = container.querySelector(".mc-conversation-body") as HTMLElement;
    body.getBoundingClientRect = () => ({ top: bodyTop, bottom: bodyTop + 4000, left: 0, right: 0, width: 0, height: 4000, x: 0, y: 0, toJSON: () => ({}) });
    container.getBoundingClientRect = () => ({ top: 0, bottom: 500, left: 0, right: 0, width: 0, height: 500, x: 0, y: 0, toJSON: () => ({}) });
}

describe("useConversationRenderWindow", () => {
    it("keeps every row mounted when the pane has no layout (jsdom, hidden tab)", () => {
        const messages = userRows(40);
        render(<Probe messages={messages} />);
        expect(screen.getByTestId("start").textContent).toBe("0");
        expect(screen.getByTestId("row-m0")).toBeTruthy();
        expect(screen.getByTestId("spacer").style.height).toBe("0px");
    });

    it("trims the head once a measurable, bottom-pinned pane exceeds four screens", async () => {
        const messages = userRows(40);
        const view = render(<Probe messages={messages} />);
        mockPaneLayout(screen.getByTestId("scroll"));

        view.rerender(<Probe messages={[...messages]} />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));
        expect(screen.queryByTestId("row-m0")).toBeNull();
        expect(screen.getByTestId("row-m39")).toBeTruthy();
        expect(parseFloat(screen.getByTestId("spacer").style.height)).toBeGreaterThan(0);
    });

    it("reveals an unmounted row by expanding the window to its turn", async () => {
        const messages = userRows(40);
        const view = render(<Probe messages={messages} />);
        mockPaneLayout(screen.getByTestId("scroll"));
        view.rerender(<Probe messages={[...messages]} />);
        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));

        fireEvent.click(screen.getByTestId("reveal"));

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("10"));
        expect(screen.getByTestId("row-m10")).toBeTruthy();
        // The 25 unmounted rows above the old window carried 2500px; the 10
        // left above the revealed turn keep their share instead of zeroing.
        expect(screen.getByTestId("spacer").style.height).toBe("1000px");
    });

    it("mounts the whole prefix when the reader drags into the spacer", async () => {
        const messages = userRows(40);
        const view = render(<Probe messages={messages} />);
        mockPaneLayout(screen.getByTestId("scroll"));
        view.rerender(<Probe messages={[...messages]} />);
        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));

        // Parked 4000px below the body top: inside the spacer itself.
        mockPaneLayout(screen.getByTestId("scroll"), 4000);
        view.rerender(<Probe messages={[...messages]} latched />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("0"));
        expect(screen.getByTestId("row-m0")).toBeTruthy();
        expect(screen.getByTestId("spacer").style.height).toBe("0px");
    });

    it("does not treat a scaled gap inside the blank threshold as the spacer", async () => {
        const messages = userRows(40);
        const view = render(<Probe messages={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        view.rerender(<Probe messages={[...messages]} />);
        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));

        // Visual gap is 50px, but the scroller's box is twice its CSS height
        // (ui-scale 2). 25 CSS px is inside the 40px blank threshold, so this
        // is one batch. Once that batch mounts, the body top jumps above the
        // viewport the way production does when new rows replace spacer pixels.
        const body = container.querySelector(".mc-conversation-body") as HTMLElement;
        body.getBoundingClientRect = () => {
            const start = Number(container.querySelector('[data-testid="start"]')?.textContent || "0");
            const top = start >= 25 ? 50 : -2000;
            return { top, bottom: top + 4000, left: 0, right: 0, width: 0, height: 4000, x: 0, y: top, toJSON: () => ({}) };
        };
        container.getBoundingClientRect = () => ({ top: 0, bottom: 1000, left: 0, right: 0, width: 0, height: 1000, x: 0, y: 0, toJSON: () => ({}) });
        view.rerender(<Probe messages={[...messages]} latched />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("10"));
        // 15 rows came back (1500px). The spacer keeps the other 1000px.
        // A blank mount would have zeroed it and jumped straight to 0.
        expect(screen.getByTestId("spacer").style.height).toBe("1000px");
    });

    it("grows the spacer in the same commit that unmounts the prefix", async () => {
        const messages = userRows(40);
        const hole = { current: false };
        function MeasuredProbe({ rows }: { rows: Row[] }) {
            const containerRef = useRef<HTMLDivElement | null>(null);
            const scrolledUpRef = useRef(false);
            const window = useConversationRenderWindow({
                containerRef,
                messages: rows,
                conversationKey: "probe",
                scrolledUpRef,
                enabled: true,
            });
            if (window.windowStart > 0 && window.spacerPx <= 0) hole.current = true;
            return (
                <div ref={containerRef} data-testid="scroll">
                    <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
                    <div className="mc-conversation-body">
                        {rows.slice(window.windowStart).map((message) => (
                            <div key={message.id} data-testid={`assistant-chat-user-${message.id}`}>{message.content}</div>
                        ))}
                    </div>
                    <span data-testid="start">{window.windowStart}</span>
                </div>
            );
        }
        const view = render(<MeasuredProbe rows={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        Object.defineProperty(container, "scrollHeight", {
            configurable: true,
            get: () => container.querySelectorAll('[data-testid^="assistant-chat-user-"]').length * 100
                + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
        });
        const body = container.querySelector(".mc-conversation-body") as HTMLElement;
        messages.forEach((message, index) => {
            const row = body.querySelector(`[data-testid="assistant-chat-user-${message.id}"]`) as HTMLElement;
            row.getBoundingClientRect = () => ({
                top: index * 100,
                bottom: (index + 1) * 100,
                left: 0,
                right: 0,
                width: 0,
                height: 100,
                x: 0,
                y: index * 100,
                toJSON: () => ({}),
            });
        });

        view.rerender(<MeasuredProbe rows={[...messages]} />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));
        // 25 prefix rows at 100px, applied with the unmount rather than after a collapsed frame.
        expect(screen.getByTestId("spacer").style.height).toBe("2500px");
        expect(hole.current).toBe(false);
        expect(screen.queryByTestId("assistant-chat-user-m0")).toBeNull();
        expect(screen.getByTestId("assistant-chat-user-m39")).toBeTruthy();
    });

    it("stores the prefix in CSS pixels when the app scale doubles visual rects", async () => {
        const messages = userRows(40);
        const hole = { current: false };
        function ScaledProbe({ rows }: { rows: Row[] }) {
            const containerRef = useRef<HTMLDivElement | null>(null);
            const scrolledUpRef = useRef(false);
            const window = useConversationRenderWindow({
                containerRef,
                messages: rows,
                conversationKey: "probe",
                scrolledUpRef,
                enabled: true,
            });
            if (window.windowStart > 0 && window.spacerPx <= 0) hole.current = true;
            return (
                <div ref={containerRef} data-testid="scroll">
                    <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
                    <div className="mc-conversation-body">
                        {rows.slice(window.windowStart).map((message) => (
                            <div key={message.id} data-testid={`assistant-chat-user-${message.id}`}>{message.content}</div>
                        ))}
                    </div>
                    <span data-testid="start">{window.windowStart}</span>
                </div>
            );
        }
        const view = render(<ScaledProbe rows={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        Object.defineProperty(container, "scrollHeight", {
            configurable: true,
            get: () => container.querySelectorAll('[data-testid^="assistant-chat-user-"]').length * 100
                + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
        });
        const body = container.querySelector(".mc-conversation-body") as HTMLElement;
        messages.forEach((message, index) => {
            const row = body.querySelector(`[data-testid="assistant-chat-user-${message.id}"]`) as HTMLElement;
            Object.defineProperty(row, "offsetHeight", { configurable: true, value: 100 });
            row.getBoundingClientRect = () => ({
                top: index * 200,
                bottom: index * 200 + 200,
                left: 0,
                right: 0,
                width: 0,
                height: 200,
                x: 0,
                y: index * 200,
                toJSON: () => ({}),
            });
        });

        view.rerender(<ScaledProbe rows={[...messages]} />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));
        // Visual span is 25 * 200. offsetHeight/rect is 100/200, so the spacer stays 2500 CSS px.
        expect(screen.getByTestId("spacer").style.height).toBe("2500px");
        expect(hole.current).toBe(false);
    });

    it("prefers offsetTop CSS pixels over a doubled visual rect", async () => {
        const messages = userRows(40);
        function OffsetProbe({ rows }: { rows: Row[] }) {
            const containerRef = useRef<HTMLDivElement | null>(null);
            const scrolledUpRef = useRef(false);
            const window = useConversationRenderWindow({
                containerRef,
                messages: rows,
                conversationKey: "probe",
                scrolledUpRef,
                enabled: true,
            });
            return (
                <div ref={containerRef} data-testid="scroll">
                    <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
                    <div className="mc-conversation-body">
                        {rows.slice(window.windowStart).map((message) => (
                            <div key={message.id} data-testid={`assistant-chat-user-${message.id}`}>{message.content}</div>
                        ))}
                    </div>
                    <span data-testid="start">{window.windowStart}</span>
                </div>
            );
        }
        const view = render(<OffsetProbe rows={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        Object.defineProperty(container, "scrollHeight", {
            configurable: true,
            get: () => container.querySelectorAll('[data-testid^="assistant-chat-user-"]').length * 100
                + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
        });
        const body = container.querySelector(".mc-conversation-body") as HTMLElement;
        messages.forEach((message, index) => {
            const row = body.querySelector(`[data-testid="assistant-chat-user-${message.id}"]`) as HTMLElement;
            Object.defineProperty(row, "offsetParent", { configurable: true, value: body });
            Object.defineProperty(row, "offsetHeight", { configurable: true, value: 100 });
            Object.defineProperty(row, "offsetTop", { configurable: true, value: index * 100 });
            row.getBoundingClientRect = () => ({
                top: index * 200,
                bottom: index * 200 + 200,
                left: 0,
                right: 0,
                width: 0,
                height: 200,
                x: 0,
                y: index * 200,
                toJSON: () => ({}),
            });
        });

        view.rerender(<OffsetProbe rows={[...messages]} />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));
        // 25 rows at offsetTop 100px. The doubled rects must not win.
        expect(screen.getByTestId("spacer").style.height).toBe("2500px");
    });

    it("adds the leading row margin the border box does not include", async () => {
        const messages = userRows(40);
        function MarginProbe({ rows }: { rows: Row[] }) {
            const containerRef = useRef<HTMLDivElement | null>(null);
            const scrolledUpRef = useRef(false);
            const window = useConversationRenderWindow({
                containerRef,
                messages: rows,
                conversationKey: "probe",
                scrolledUpRef,
                enabled: true,
            });
            return (
                <div ref={containerRef} data-testid="scroll">
                    <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
                    <div className="mc-conversation-body">
                        {rows.slice(window.windowStart).map((message, index) => (
                            <div key={message.id} data-testid={`assistant-chat-user-${message.id}`} style={{ margin: "18px 0" }}>{message.content}{index}</div>
                        ))}
                    </div>
                    <span data-testid="start">{window.windowStart}</span>
                </div>
            );
        }
        const view = render(<MarginProbe rows={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        Object.defineProperty(container, "scrollHeight", {
            configurable: true,
            get: () => container.querySelectorAll('[data-testid^="assistant-chat-user-"]').length * 100
                + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
        });
        const body = container.querySelector(".mc-conversation-body") as HTMLElement;
        messages.forEach((message, index) => {
            const row = body.querySelector(`[data-testid="assistant-chat-user-${message.id}"]`) as HTMLElement;
            // 100px border box, 18px collapsed gap. The first margin sits above the span.
            row.getBoundingClientRect = () => ({
                top: index * 118,
                bottom: index * 118 + 100,
                left: 0,
                right: 0,
                width: 0,
                height: 100,
                x: 0,
                y: index * 118,
                toJSON: () => ({}),
            });
        });

        view.rerender(<MarginProbe rows={[...messages]} />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));
        // 25 boxes + 24 gaps + the leading 18px. The trailing 18px collapses with the kept row.
        expect(screen.getByTestId("spacer").style.height).toBe("2950px");
    });

    it("measures a wrapped row from its direct child, not the nested testid", async () => {
        const messages = userRows(40);
        const hole = { current: false };
        function WrappedProbe({ rows }: { rows: Row[] }) {
            const containerRef = useRef<HTMLDivElement | null>(null);
            const scrolledUpRef = useRef(false);
            const window = useConversationRenderWindow({
                containerRef,
                messages: rows,
                conversationKey: "probe",
                scrolledUpRef,
                enabled: true,
            });
            if (window.windowStart > 0 && window.spacerPx <= 0) hole.current = true;
            return (
                <div ref={containerRef} data-testid="scroll">
                    <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
                    <div className="mc-conversation-body">
                        {rows.slice(window.windowStart).map((message) => (
                            <div key={message.id} data-wrap={message.id}>
                                <div data-testid={`assistant-chat-user-${message.id}`}>{message.content}</div>
                            </div>
                        ))}
                    </div>
                    <span data-testid="start">{window.windowStart}</span>
                </div>
            );
        }
        const view = render(<WrappedProbe rows={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        Object.defineProperty(container, "scrollHeight", {
            configurable: true,
            get: () => container.querySelectorAll("[data-wrap]").length * 100
                + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
        });
        const body = container.querySelector(".mc-conversation-body") as HTMLElement;
        messages.forEach((message, index) => {
            const row = body.querySelector(`[data-wrap="${message.id}"]`) as HTMLElement;
            row.getBoundingClientRect = () => ({
                top: index * 100,
                bottom: (index + 1) * 100,
                left: 0,
                right: 0,
                width: 0,
                height: 100,
                x: 0,
                y: index * 100,
                toJSON: () => ({}),
            });
        });

        view.rerender(<WrappedProbe rows={[...messages]} />);

        await waitFor(() => expect(screen.getByTestId("start").textContent).toBe("25"));
        expect(screen.getByTestId("spacer").style.height).toBe("2500px");
        expect(hole.current).toBe(false);
    });

    it("puts the prefix back when the scroll-height delta is corrupt", async () => {
        const messages = userRows(40);
        const trimmed = { current: false };
        function WatchingProbe({ rows }: { rows: Row[] }) {
            const containerRef = useRef<HTMLDivElement | null>(null);
            const scrolledUpRef = useRef(false);
            const window = useConversationRenderWindow({
                containerRef,
                messages: rows,
                conversationKey: "probe",
                scrolledUpRef,
                enabled: true,
            });
            if (window.windowStart > 0) trimmed.current = true;
            return (
                <div ref={containerRef} data-testid="scroll">
                    <div ref={window.spacerRef} style={{ height: window.spacerPx }} data-testid="spacer" />
                    <div className="mc-conversation-body">
                        {rows.slice(window.windowStart).map((message) => (
                            <div key={message.id} data-testid={`row-${message.id}`}>{message.content}</div>
                        ))}
                    </div>
                    <span data-testid="start">{window.windowStart}</span>
                </div>
            );
        }
        const view = render(<WatchingProbe rows={messages} />);
        const container = screen.getByTestId("scroll");
        mockPaneLayout(container);
        Object.defineProperty(container, "scrollHeight", {
            configurable: true,
            get: () => (container.querySelectorAll('[data-testid^="row-"]').length > 20 ? 2_000_000 : 1000),
        });

        view.rerender(<WatchingProbe rows={[...messages]} />);

        await waitFor(() => expect(trimmed.current).toBe(true));
        expect(screen.getByTestId("start").textContent).toBe("0");
        expect(screen.getByTestId("spacer").style.height).toBe("0px");
        expect(screen.getByTestId("row-m0")).toBeTruthy();
        expect(screen.getByTestId("row-m39")).toBeTruthy();
    });
});
