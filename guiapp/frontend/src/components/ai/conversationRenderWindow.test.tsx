import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";
import {
    CONVERSATION_RENDER_MIN_MOUNTED_ROWS,
    conversationRenderCanWindow,
    conversationRenderLoadStart,
    conversationRenderRowHeightPx,
    conversationRenderTrimStart,
    conversationRenderTurnStart,
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

function Probe({ messages }: { messages: Row[] }) {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const scrolledUpRef = useRef(false);
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
function mockPaneLayout(container: HTMLElement) {
    Object.defineProperty(container, "clientHeight", { configurable: true, value: 500 });
    Object.defineProperty(container, "scrollHeight", {
        configurable: true,
        get: () => container.querySelectorAll('[data-testid^="row-"]').length * 100
            + (parseFloat((container.querySelector('[data-testid="spacer"]') as HTMLElement).style.height) || 0),
    });
    const body = container.querySelector(".mc-conversation-body") as HTMLElement;
    body.getBoundingClientRect = () => ({ top: 0, bottom: 4000, left: 0, right: 0, width: 0, height: 4000, x: 0, y: 0, toJSON: () => ({}) });
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
});
