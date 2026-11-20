// @vitest-environment jsdom
import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantInputComposer } from "../AssistantInputComposer";
import type { AssistantInputComposerProps } from "../AssistantInputComposerTypes";
import { overlayTheme } from "../aiAssistantPanelTheme";
import {
    composerShortcutHint,
    composerShortcutHintFits,
    readComposerShortcutHintFit,
} from "../composerShortcutHint";

function flushMutations(): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, 0));
}

const room = {
    toolbarWidth: 800,
    endWidth: 180,
    toolbarGap: 4,
    toolbarGaps: 2,
    trailingFixedWidth: 64,
    hintGap: 6,
    hintNaturalWidth: 160,
};

describe("composerShortcutHintFits", () => {
    it("keeps the sentence until layout has measured the row", () => {
        expect(composerShortcutHintFits({ ...room, toolbarWidth: 0 })).toBe(true);
        expect(composerShortcutHintFits({ ...room, hintNaturalWidth: 0 })).toBe(true);
    });

    it("keeps the sentence when the model slot and the send cluster can give it its width", () => {
        // 180 + 8 + 64 + 6 + 160 = 418, inside 800.
        expect(composerShortcutHintFits(room)).toBe(true);
        expect(composerShortcutHintFits({ ...room, toolbarWidth: 418 })).toBe(true);
    });

    it("drops the sentence when that cluster cannot give it its width", () => {
        // 418 is the sentence's own total. 417 still clips the send control.
        expect(composerShortcutHintFits({ ...room, toolbarWidth: 417 })).toBe(false);
        expect(composerShortcutHintFits({ ...room, toolbarWidth: 416 })).toBe(false);
        // A long model name takes the slot; the sentence yields before the send control is clipped.
        expect(composerShortcutHintFits({ ...room, endWidth: 420, toolbarWidth: 600 })).toBe(false);
    });

    it("does not charge a hint gap when the sentence is the only trailing item", () => {
        // The toolbar still pays the gap between the left group and the cluster: 4 + 160.
        const onlySentence = {
            ...room,
            endWidth: 0,
            toolbarGaps: 1,
            trailingFixedWidth: 0,
            hintGap: 0,
            hintNaturalWidth: 160,
        };
        expect(composerShortcutHintFits({ ...onlySentence, toolbarWidth: 164 })).toBe(true);
        expect(composerShortcutHintFits({ ...onlySentence, toolbarWidth: 163 })).toBe(false);
    });
});

describe("readComposerShortcutHintFit", () => {
    function setWidth(el: HTMLElement, width: number) {
        Object.defineProperty(el, "offsetWidth", { configurable: true, value: width });
        Object.defineProperty(el, "clientWidth", { configurable: true, value: width });
        Object.defineProperty(el, "offsetHeight", { configurable: true, value: width > 0 ? 16 : 0 });
    }

    function toolbar(options: { toolbar: number; model?: number; send: number; sentence: number; sentenceInRow: boolean }) {
        const root = document.createElement("div");
        root.style.gap = "4px";
        setWidth(root, options.toolbar);

        const start = document.createElement("div");
        start.setAttribute("data-toolbar-slot", "start");
        start.style.gap = "6px";
        const chip = document.createElement("button");
        setWidth(chip, 240);
        start.append(chip);
        root.append(start);

        if (options.model !== undefined) {
            const end = document.createElement("div");
            end.dataset.testid = "ai-input-toolbar-end";
            setWidth(end, options.model);
            root.append(end);
        }

        const trailing = document.createElement("div");
        trailing.setAttribute("data-toolbar-slot", "trailing");
        trailing.style.gap = "6px";
        const measure = document.createElement("span");
        measure.setAttribute("data-shortcut-measure", "");
        measure.style.position = "absolute";
        setWidth(measure, options.sentence);
        trailing.append(measure);
        if (options.sentenceInRow) {
            const hint = document.createElement("span");
            hint.setAttribute("data-shortcut-hint", "");
            setWidth(hint, options.sentence);
            trailing.append(hint);
        }
        const send = document.createElement("span");
        setWidth(send, options.send);
        trailing.append(send);
        root.append(trailing);
        return root;
    }

    it("ignores the left chips and the out-of-flow measure, and uses the sentence's intrinsic width", () => {
        // Model 180 + gaps 8 + send 64 + hint gap 6 + sentence 160 = 418.
        const wide = toolbar({ toolbar: 800, model: 180, send: 64, sentence: 160, sentenceInRow: true });
        expect(readComposerShortcutHintFit(wide)).toBe(true);

        const narrow = toolbar({ toolbar: 400, model: 180, send: 64, sentence: 160, sentenceInRow: false });
        expect(readComposerShortcutHintFit(narrow)).toBe(false);
    });

    it("does not count the sentence twice when it is already on the row", () => {
        const root = toolbar({ toolbar: 418, model: 180, send: 64, sentence: 160, sentenceInRow: true });
        expect(readComposerShortcutHintFit(root)).toBe(true);
    });

    it("uses the toolbar content box after horizontal padding", () => {
        // 418 of content needs more than 436 - 20 of padding.
        const root = toolbar({ toolbar: 436, model: 180, send: 64, sentence: 160, sentenceInRow: false });
        root.style.padding = "2px 10px 0 10px";
        expect(readComposerShortcutHintFit(root)).toBe(false);
    });

    it("does not reserve the measure host even when that host reports a size", () => {
        const root = toolbar({ toolbar: 800, model: 180, send: 64, sentence: 160, sentenceInRow: true });
        const host = document.createElement("span");
        host.setAttribute("data-shortcut-measure-host", "");
        setWidth(host, 900);
        root.querySelector("[data-toolbar-slot='trailing']")!.append(host);
        expect(readComposerShortcutHintFit(root)).toBe(true);
    });
});

describe("composerShortcutHint text", () => {
    it("uses one sentence for Chinese and English", () => {
        expect(composerShortcutHint("zh-Hans")).toBe("Enter 发送 · Ctrl/⌘+Enter 换行");
        expect(composerShortcutHint("en")).toContain("Enter to send");
        expect(composerShortcutHint("en")).toContain("Ctrl/⌘+Enter");
    });
});

vi.mock("../AssistantAttachmentsStrip", () => ({
    AssistantAttachmentsStrip: () => null,
}));
vi.mock("../InputHistoryAutocomplete", () => ({
    InputHistoryAutocomplete: () => null,
}));
vi.mock("../MemoryUsageRing", () => ({
    MemoryUsageRing: () => null,
}));

function renderComposer(lang = "zh-Hans") {
    return render(<AssistantInputComposer {...composerProps(lang)} />);
}

describe("AssistantInputComposer shortcut sentence", () => {
    function box(el: HTMLElement, width: number, height = 16) {
        Object.defineProperty(el, "offsetWidth", { configurable: true, value: width });
        Object.defineProperty(el, "clientWidth", { configurable: true, value: width });
        Object.defineProperty(el, "offsetHeight", { configurable: true, value: height });
    }

    function layOut(toolbarWidth: number, sentenceWidth: number, sendWidth: number, modelWidth?: number) {
        const toolbar = screen.getByTestId("ai-input-toolbar");
        box(toolbar, toolbarWidth);
        const measure = toolbar.querySelector<HTMLElement>("[data-shortcut-measure]")!;
        box(measure, sentenceWidth);
        const send = toolbar.querySelector<HTMLElement>(".ai-composer-send")!;
        box(send.parentElement as HTMLElement, sendWidth);
        const end = toolbar.querySelector<HTMLElement>("[data-testid='ai-input-toolbar-end']");
        if (end && modelWidth !== undefined) box(end, modelWidth);
        return toolbar;
    }

    class RecordingResizeObserver {
        static current: RecordingResizeObserver | null = null;
        readonly observed: Element[] = [];
        constructor(private readonly callback: ResizeObserverCallback) {
            RecordingResizeObserver.current = this;
        }
        observe(target: Element) { this.observed.push(target); }
        unobserve() {}
        disconnect() {}
        fire() {
            this.callback([], this as unknown as ResizeObserver);
        }
    }

    it("moves the sentence onto the send tooltip when the row cannot hold it, and puts it back when the row grows", async () => {
        vi.stubGlobal("ResizeObserver", RecordingResizeObserver);
        try {
            const sentence = composerShortcutHint("zh-Hans");
            render(<AssistantInputComposer {...composerProps()} toolbarEndContent={<span>助手</span>} />);
            const fire = () => act(() => {
                RecordingResizeObserver.current!.fire();
            });

            layOut(800, 160, 64, 120);
            fire();
            expect(RecordingResizeObserver.current!.observed.some(el => el.hasAttribute("data-shortcut-measure"))).toBe(true);
            expect(screen.getByTestId("ai-composer-shortcut-hint").textContent).toBe(sentence);
            expect(screen.getByRole("button", { name: "发送" }).getAttribute("title")).toBe("发送 (Enter)");

            layOut(800, 2000, 64, 120);
            fire();
            expect(screen.queryByTestId("ai-composer-shortcut-hint")).toBeNull();

            layOut(360, 160, 64, 200);
            fire();
            expect(screen.queryByTestId("ai-composer-shortcut-hint")).toBeNull();
            expect(screen.getByRole("button", { name: "发送" }).getAttribute("title")).toBe(sentence);
            await act(async () => {
                await flushMutations();
            });
            expect(readComposerShortcutHintFit(screen.getByTestId("ai-input-toolbar"))).toBe(false);
            expect(screen.queryByTestId("ai-composer-shortcut-hint")).toBeNull();

            layOut(800, 160, 64, 120);
            fire();
            expect(screen.getByTestId("ai-composer-shortcut-hint").textContent).toBe(sentence);
            expect(screen.getByRole("button", { name: "发送" }).getAttribute("title")).toBe("发送 (Enter)");
        } finally {
            vi.unstubAllGlobals();
        }
    });

    it("measures a model slot that mounts after the row", async () => {
        vi.stubGlobal("ResizeObserver", RecordingResizeObserver);
        const descriptors = stubBoxSizes();
        try {
            const sentence = composerShortcutHint("zh-Hans");
            const { rerender } = renderComposer();
            expect(screen.getByTestId("ai-composer-shortcut-hint")).toBeTruthy();

            rerender(<AssistantInputComposer {...composerProps()} toolbarEndContent={<span>助手</span>} />);
            await act(async () => {
                await flushMutations();
            });
            expect(screen.queryByTestId("ai-composer-shortcut-hint")).toBeNull();
            expect(screen.getByRole("button", { name: "发送" }).getAttribute("title")).toBe(sentence);
        } finally {
            restoreBoxSizes(descriptors);
            vi.unstubAllGlobals();
        }
    });

    it("shows the sentence before the first measurement", () => {
        const { unmount } = renderComposer();
        const measure = document.querySelector<HTMLElement>("[data-shortcut-measure]")!;
        const host = document.querySelector<HTMLElement>("[data-shortcut-measure-host]")!;
        expect(measure.style.width).toBe("max-content");
        expect(host.style.width).toBe("0px");
        expect(host.style.overflow).toBe("hidden");
        expect(screen.getByTestId("ai-composer-shortcut-hint").textContent).toBe(composerShortcutHint("zh-Hans"));
        expect(screen.getByRole("button", { name: "发送" }).getAttribute("title")).toBe("发送 (Enter)");
        unmount();
    });
});

function stubBoxSizes(): Array<[prototype: object, name: string, previous: PropertyDescriptor | undefined]> {
    const read = function (this: HTMLElement) {
        if (this.dataset.testid === "ai-input-toolbar") return 500;
        if (this.dataset.testid === "ai-input-toolbar-end") return 400;
        if (this.hasAttribute("data-shortcut-measure")) return 160;
        return 0;
    };
    const saved: Array<[object, string, PropertyDescriptor | undefined]> = [];
    for (const name of ["offsetWidth", "clientWidth", "offsetHeight"] as const) {
        const previous = Object.getOwnPropertyDescriptor(HTMLElement.prototype, name);
        saved.push([HTMLElement.prototype, name, previous]);
        Object.defineProperty(HTMLElement.prototype, name, { configurable: true, get: name === "offsetHeight" ? () => 16 : read });
    }
    return saved;
}

function restoreBoxSizes(saved: Array<[prototype: object, name: string, previous: PropertyDescriptor | undefined]>) {
    for (const [prototype, name, previous] of saved) {
        if (previous) Object.defineProperty(prototype, name, previous);
        else delete (prototype as Record<string, unknown>)[name];
    }
}

function composerProps(lang = "zh-Hans"): AssistantInputComposerProps {
    return {
        browseFile: vi.fn(),
        canSend: true,
        cancelPending: false,
        exitHistoryBrowsing: vi.fn(() => false),
        finishVoicePointer: vi.fn(),
        handleCancel: vi.fn(),
        handleClearInput: vi.fn(),
        handleDragOver: vi.fn(),
        handleDrop: vi.fn(),
        handlePaste: vi.fn(),
        handleSend: vi.fn(),
        handleVoiceClick: vi.fn(),
        handleVoicePointerDown: vi.fn(),
        handleVoicePointerLeave: vi.fn(),
        inputAreaHeight: null,
        inputLocked: false,
        inputRef: { current: null },
        inputValue: "草稿",
        inline: false,
        isBusy: false,
        isSelectionCollapsedAtBoundary: vi.fn(() => false),
        lang,
        pendingAttachments: [],
        placeholderText: "Ask AI",
        ready: true,
        recallHistory: vi.fn(() => false),
        rememberHistoryEdit: vi.fn(),
        resizeInput: vi.fn(),
        selectedFilePaths: [],
        setPendingAttachments: vi.fn(),
        showBusySpinner: false,
        showMemoryUsage: false,
        showVoiceInput: false,
        theme: overlayTheme,
        themeMode: "dark",
        updateInputValue: vi.fn(),
        voiceInput: {} as AssistantInputComposerProps["voiceInput"],
    };
}
