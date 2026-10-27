// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
    applyChatHighlights,
    chatMarkAtOffset,
    chatMarkOffsets,
    chatMessageIndexAt,
    chatRangeForMark,
    chatSelectionSlices,
    chatTextSpans,
    clearChatHighlights,
} from "./chatMarkAnchors";
import type { ChatMark } from "./chatMarks";

function messageRoot(html: string): HTMLElement {
    const root = document.createElement("div");
    root.innerHTML = html;
    document.body.appendChild(root);
    return root;
}

function selectText(node: Node, start: number, end: number): Range {
    const range = document.createRange();
    range.setStart(node, start);
    range.setEnd(node, end);
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    return range;
}

const mark: ChatMark = { message: 1, color: "yellow", start: 3, end: 8, text: "lo wo" };

let registry: Map<string, Set<Range>>;

beforeEach(() => {
    registry = new Map();
    // The real Highlight is a Set of ranges taking them as constructor
    // arguments, not as one iterable. The stub mirrors that shape.
    (globalThis as unknown as { Highlight: unknown }).Highlight = class Highlight {
        readonly ranges: Set<Range>;
        constructor(...ranges: Range[]) {
            this.ranges = new Set(ranges);
        }
        get size() {
            return this.ranges.size;
        }
    };
    (globalThis as unknown as { CSS: unknown }).CSS = {
        highlights: {
            set: (name: string, value: Set<Range>) => registry.set(name, value),
            delete: (name: string) => registry.delete(name),
            get: (name: string) => registry.get(name),
        },
    };
});

afterEach(() => {
    document.body.innerHTML = "";
    clearChatHighlights();
    delete (globalThis as unknown as { Highlight?: unknown }).Highlight;
    const css = (globalThis as unknown as { CSS?: { highlights?: unknown } }).CSS;
    if (css) delete css.highlights;
    window.getSelection()?.removeAllRanges();
});

describe("chat mark anchors", () => {
    it("counts only the answer text: controls and the reasoning panel are skipped", () => {
        const root = messageRoot(
            `<div data-chat-message="0"><p>Hello world</p><button>复制</button>` +
            `<div data-chat-mark-skip><p>思考过程</p></div></div>`,
        );
        const spans = chatTextSpans(root.querySelector<HTMLElement>("[data-chat-message]")!);
        expect(spans.map((span) => span.node.nodeValue).join("")).toBe("Hello world");
    });

    it("maps a selection inside one answer to a stored range", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        const text = root.querySelector("p")!.firstChild!;
        const range = selectText(text, 3, 8);
        expect(chatSelectionSlices(root, range)).toEqual([{ message: 1, start: 3, end: 8, text: "lo wo" }]);
    });

    it("splits a selection that spans two answers into one slice each", () => {
        const root = messageRoot(
            `<div data-chat-message="0"><p>First answer</p></div>` +
            `<div data-chat-message="1"><p>Second answer</p></div>`,
        );
        const first = root.querySelectorAll("p")[0].firstChild!;
        const second = root.querySelectorAll("p")[1].firstChild!;
        const range = document.createRange();
        range.setStart(first, 6);
        range.setEnd(second, 6);
        expect(chatSelectionSlices(root, range)).toEqual([
            { message: 0, start: 6, end: 12, text: "answer" },
            { message: 1, start: 0, end: 6, text: "Second" },
        ]);
    });

    it("rebuilds the DOM range for a stored mark", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        const range = chatRangeForMark(root, mark);
        expect(range?.toString()).toBe("lo wo");
    });

    it("re-anchors on the quote when the offsets moved", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        const spans = chatTextSpans(root.querySelector<HTMLElement>("[data-chat-message]")!);
        expect(chatMarkOffsets(spans, { ...mark, start: 0, end: 5, text: "lo wo" })).toEqual({ start: 3, end: 8 });
    });

    it("picks the occurrence nearest the old offsets when the quote repeats", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>alpha beta alpha beta</p></div>`);
        const spans = chatTextSpans(root.querySelector<HTMLElement>("[data-chat-message]")!);
        // Both "alpha" spots would match; the stored offset 18 points at the
        // second one, so the re-anchor must not jump to the first.
        expect(chatMarkOffsets(spans, { message: 1, color: "yellow", start: 18, end: 23, text: "alpha" })).toEqual({ start: 11, end: 16 });
    });

    it("does not loop on an empty quote", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello</p></div>`);
        const spans = chatTextSpans(root.querySelector<HTMLElement>("[data-chat-message]")!);
        expect(chatMarkOffsets(spans, { ...mark, text: "" })).toBeNull();
    });

    it("skips a mark whose quote no longer exists", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        expect(chatRangeForMark(root, { ...mark, text: "gone" })).toBeNull();
    });

    it("finds the message a click sits in and the mark covering it", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        const paragraph = root.querySelector("p")!;
        expect(chatMessageIndexAt(paragraph)).toBe(1);
        expect(chatMarkAtOffset([mark], 1, 4)).toBe(0);
        expect(chatMarkAtOffset([mark], 1, 20)).toBe(-1);
        expect(chatMarkAtOffset([mark], 0, 4)).toBe(-1);
    });

    it("paints one registry entry per colour and clears them", () => {
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        const blue: ChatMark = { message: 1, color: "blue", start: 0, end: 5, text: "Hello" };
        expect(applyChatHighlights(root, [mark, blue])).toBe(true);
        expect(registry.get("companion-chat-yellow")?.size).toBe(1);
        expect(registry.get("companion-chat-blue")?.size).toBe(1);
        clearChatHighlights();
        expect(registry.size).toBe(0);
    });

    it("reports unsupported when the WebView has no Custom Highlight API", () => {
        delete (globalThis as unknown as { Highlight?: unknown }).Highlight;
        const root = messageRoot(`<div data-chat-message="1"><p>Hello world</p></div>`);
        expect(applyChatHighlights(root, [mark])).toBe(false);
    });
});
