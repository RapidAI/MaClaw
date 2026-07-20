import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import {
    ChatBubbleFrame,
    CHAT_BUBBLE_TAIL_INSET,
    CHAT_BUBBLE_TAIL_SIZE,
    CHAT_BUBBLE_TAIL_TOP,
    CHAT_SPEAKER_LABEL_GAP,
    chatBubbleTailStyle,
    localIntroChatBubbleBackground,
    sanitizeChatBubbleLayoutStyle,
    tintedChatBubbleBackground,
    userChatBubbleBackground,
} from "../ChatBubbleFrame";

describe("sanitizeChatBubbleLayoutStyle", () => {
    it("strips chrome keys and overflow when a tail is shown", () => {
        const { layoutStyle, padding, borderRadius } = sanitizeChatBubbleLayoutStyle(
            {
                position: "static",
                background: "red",
                border: "5px solid lime",
                overflow: "hidden",
                maxWidth: "80%",
                padding: 4,
                borderRadius: 99,
            },
            { showTail: true },
        );

        expect(layoutStyle.position).toBeUndefined();
        expect(layoutStyle.background).toBeUndefined();
        expect(layoutStyle.border).toBeUndefined();
        expect(layoutStyle.overflow).toBeUndefined();
        expect(layoutStyle.maxWidth).toBe("80%");
        expect(padding).toBe(4);
        expect(borderRadius).toBe(99);
    });

    it("keeps overflow when the tail is hidden", () => {
        const { layoutStyle } = sanitizeChatBubbleLayoutStyle(
            { overflow: "hidden", color: "blue" },
            { showTail: false },
        );
        expect(layoutStyle.overflow).toBe("hidden");
        expect(layoutStyle.color).toBe("blue");
    });

    it("drops undefined layout values", () => {
        const { layoutStyle } = sanitizeChatBubbleLayoutStyle(
            { maxWidth: "80%", color: undefined },
            { showTail: true },
        );
        expect(layoutStyle.maxWidth).toBe("80%");
        expect(Object.prototype.hasOwnProperty.call(layoutStyle, "color")).toBe(false);
    });
});

describe("chatBubbleTailStyle", () => {
    it("places a top-pointing diamond under the name (same as AI assistant)", () => {
        const left = chatBubbleTailStyle("left", "#111", "#222");
        const right = chatBubbleTailStyle("right", "#111", "#222");

        expect(left.top).toBe(CHAT_BUBBLE_TAIL_TOP);
        expect(left.top).toBeLessThan(0);
        expect(left.left).toBe(CHAT_BUBBLE_TAIL_INSET);
        expect(left.right).toBeUndefined();

        expect(right.right).toBe(CHAT_BUBBLE_TAIL_INSET);
        expect(right.left).toBeUndefined();
        expect(right.top).toBe(CHAT_BUBBLE_TAIL_TOP);

        expect(left.width).toBe(CHAT_BUBBLE_TAIL_SIZE);
        expect(left.transform).toBe("rotate(45deg)");
        expect(left.borderTop).toContain("#222");
        expect(left.borderLeft).toContain("#222");
    });
});

describe("ChatBubbleFrame topRight", () => {
    it("renders an optional top-right action slot", () => {
        render(
            <ChatBubbleFrame
                side="left"
                background="#111"
                borderColor="#222"
                data-testid="bubble-with-action"
                topRight={<button type="button" data-testid="slot-btn">x</button>}
            >
                body
            </ChatBubbleFrame>,
        );
        expect(screen.getByTestId("bubble-with-action-top-right")).toBeTruthy();
        expect(screen.getByTestId("slot-btn")).toBeTruthy();
    });

    it("omits the top-right slot when not provided", () => {
        render(
            <ChatBubbleFrame side="left" background="#111" borderColor="#222" data-testid="bubble-plain">
                body
            </ChatBubbleFrame>,
        );
        expect(screen.queryByTestId("bubble-plain-top-right")).toBeNull();
    });
});

describe("background helpers", () => {
    it("matches the AI assistant user fill formula", () => {
        expect(userChatBubbleBackground("#2f5f98", "#f8fafc")).toBe(
            "color-mix(in srgb, #2f5f98 12%, #f8fafc)",
        );
        expect(userChatBubbleBackground("#2f5f98", "#f8fafc")).toBe(
            tintedChatBubbleBackground("#2f5f98", "#f8fafc", 12),
        );
        expect(localIntroChatBubbleBackground("#2f5f98", "#f8fafc")).toBe(
            tintedChatBubbleBackground("#2f5f98", "#f8fafc", 5),
        );
    });

    it("keeps name gap aligned with the top tail height", () => {
        // Tail protrudes CHAT_BUBBLE_TAIL_TOP above the border; label gap must be positive.
        expect(CHAT_SPEAKER_LABEL_GAP).toBeGreaterThan(0);
        expect(CHAT_SPEAKER_LABEL_GAP).toBeLessThanOrEqual(Math.abs(CHAT_BUBBLE_TAIL_TOP));
    });
});

describe("ChatBubbleFrame", () => {
    it("renders a top-left tail for assistant bubbles (points at name)", () => {
        render(
            <ChatBubbleFrame
                side="left"
                background="#2a2a2a"
                borderColor="#444"
                data-testid="bubble"
            >
                hi
            </ChatBubbleFrame>
        );

        const bubble = screen.getByTestId("bubble");
        const tail = screen.getByTestId("bubble-tail");

        expect(tail.getAttribute("data-side")).toBe("left");
        expect(tail.getAttribute("aria-hidden")).toBe("true");
        expect(tail.style.left).toBe(`${CHAT_BUBBLE_TAIL_INSET}px`);
        expect(tail.style.right).toBe("");
        expect(tail.style.top).toBe(`${CHAT_BUBBLE_TAIL_TOP}px`);
        expect(tail.style.width).toBe(`${CHAT_BUBBLE_TAIL_SIZE}px`);
        expect(tail.style.transform).toBe("rotate(45deg)");
        expect(bubble.style.borderRadius).toBe("8px");
        expect(bubble.style.boxSizing).toBe("border-box");
        expect(bubble.style.isolation).toBe("isolate");
        expect(bubble.style.overflow).toBe("visible");
        expect(bubble.textContent).toContain("hi");
    });

    it("renders a top-right tail for user bubbles (points at name)", () => {
        render(
            <ChatBubbleFrame
                side="right"
                background={userChatBubbleBackground("#1a3a5c", "#111827")}
                borderColor="#3a6a9c"
                data-testid="user-bubble"
            >
                me
            </ChatBubbleFrame>
        );

        const bubble = screen.getByTestId("user-bubble");
        const tail = screen.getByTestId("user-bubble-tail");

        expect(tail.getAttribute("data-side")).toBe("right");
        expect(tail.style.right).toBe(`${CHAT_BUBBLE_TAIL_INSET}px`);
        expect(tail.style.left).toBe("");
        expect(tail.style.top).toBe(`${CHAT_BUBBLE_TAIL_TOP}px`);
        expect(bubble.style.borderRadius).toBe("8px");
        expect(tail.style.background).toBe(bubble.style.background);
    });

    it("supports a custom tailTestId for AI assistant parity", () => {
        render(
            <ChatBubbleFrame
                side="left"
                background="#111"
                borderColor="#222"
                tailTestId="assistant-chat-tail-ai-x"
            >
                body
            </ChatBubbleFrame>
        );
        expect(screen.getByTestId("assistant-chat-tail-ai-x")).toBeTruthy();
    });

    it("keeps structural chrome when style tries to override it", () => {
        render(
            <ChatBubbleFrame
                side="left"
                background="#111"
                borderColor="#222"
                data-testid="locked"
                style={{
                    position: "static",
                    background: "red",
                    border: "5px solid lime",
                    overflow: "hidden",
                    maxWidth: "80%",
                    overflowWrap: "anywhere",
                    whiteSpace: "pre-wrap",
                }}
            >
                body
            </ChatBubbleFrame>
        );

        const bubble = screen.getByTestId("locked") as HTMLElement;
        expect(bubble.style.position).toBe("relative");
        expect(bubble.style.background).toBe("rgb(17, 17, 17)");
        expect(bubble.style.border).toContain("1px");
        expect(bubble.style.overflow).toBe("visible");
        expect(bubble.style.maxWidth).toBe("80%");
        expect(bubble.style.overflowWrap).toBe("anywhere");
        expect(bubble.style.whiteSpace).toBe("pre-wrap");
    });

    it("can hide the tail", () => {
        render(
            <ChatBubbleFrame
                side="left"
                background="#111"
                borderColor="#222"
                data-testid="plain"
                showTail={false}
                style={{ overflow: "auto" }}
            >
                plain
            </ChatBubbleFrame>
        );

        expect(screen.queryByTestId("plain-tail")).toBeNull();
        expect((screen.getByTestId("plain") as HTMLElement).style.borderRadius).toBe("8px");
        expect((screen.getByTestId("plain") as HTMLElement).style.overflow).toBe("auto");
    });

    it("falls back safely when fill/stroke tokens are blank", () => {
        render(
            <ChatBubbleFrame
                side="left"
                background="  "
                borderColor=""
                data-testid="blank"
            >
                x
            </ChatBubbleFrame>
        );
        const bubble = screen.getByTestId("blank") as HTMLElement;
        const tail = screen.getByTestId("blank-tail") as HTMLElement;
        expect(bubble.style.background).toBe("transparent");
        expect(bubble.style.border).toContain("transparent");
        expect(tail.style.background).toBe(bubble.style.background);
    });
});
