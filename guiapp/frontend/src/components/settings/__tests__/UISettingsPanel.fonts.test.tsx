/** @vitest-environment jsdom */
import { StrictMode, useRef, useState } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetChatFontFamily: vi.fn().mockResolvedValue("system"),
    GetChatFontSize: vi.fn().mockResolvedValue(14),
    GetCodingFontFamily: vi.fn().mockResolvedValue("system"),
    SetChatFontFamily: vi.fn().mockResolvedValue(undefined),
    SetChatFontSize: vi.fn().mockResolvedValue(undefined),
    SetCodingFontFamily: vi.fn().mockResolvedValue(undefined),
    SetUIZoomFactor: vi.fn().mockResolvedValue(undefined),
}));

import { GetChatFontFamily, GetChatFontSize, GetCodingFontFamily, SetChatFontFamily, SetChatFontSize, SetCodingFontFamily } from "../../../../wailsjs/go/main/App";
import { ApiStorePage } from "../../pages/ApiStorePage";
import { acceptDraftedValue, chatFontSizeSaveQueue, codingFontFamilySaveQueue, decideConfigChatFontSize, normalizeAssistantFontId, resetAssistantFontSaveQueues, serialNewestHasStarted, snapshotConfirmsDraft } from "../../ai/assistantFonts";
import { CHAT_FONT_SIZE_PERSIST_DEBOUNCE_MS } from "../useChatFontSizeCommit";
import { UISettingsPanel } from "../UISettingsPanel";

const props = {
    config: null,
    lang: "zh-Hans",
    t: (key: string) => key,
    uiZoom: 1,
    setUiZoom: vi.fn(),
    uiZoomAuto: true,
    setUiZoomAuto: vi.fn(),
    chatFontSize: 14,
    setChatFontSize: vi.fn(),
    chatFontFamily: "system",
    setChatFontFamily: vi.fn(),
    codingFontFamily: "system",
    setCodingFontFamily: vi.fn(),
    darkSchemeId: "graphite" as const,
    setDarkSchemeId: vi.fn(),
    lightSchemeId: "default" as const,
    setLightSchemeId: vi.fn(),
};

function restoreFontMocks() {
    resetAssistantFontSaveQueues();
    vi.mocked(GetChatFontSize).mockReset();
    vi.mocked(GetChatFontSize).mockResolvedValue(14);
    vi.mocked(GetChatFontFamily).mockReset();
    vi.mocked(GetChatFontFamily).mockResolvedValue("system");
    vi.mocked(GetCodingFontFamily).mockReset();
    vi.mocked(GetCodingFontFamily).mockResolvedValue("system");
    vi.mocked(SetChatFontSize).mockReset();
    vi.mocked(SetChatFontSize).mockResolvedValue(undefined);
    vi.mocked(SetChatFontFamily).mockReset();
    vi.mocked(SetChatFontFamily).mockResolvedValue(undefined);
    vi.mocked(SetCodingFontFamily).mockReset();
    vi.mocked(SetCodingFontFamily).mockResolvedValue(undefined);
}

function codingInput(id: string): HTMLInputElement {
    return screen.getByTestId(`assistant-font-coding-${id}`).querySelector("input") as HTMLInputElement;
}

describe("UISettingsPanel assistant fonts", () => {
    afterEach(() => {
        cleanup();
        restoreFontMocks();
        vi.clearAllMocks();
    });

    it("keeps conversation and coding fonts independent, defaulting to the current face", () => {
        render(<UISettingsPanel {...props} />);

        expect((screen.getByTestId("assistant-font-conversation-system").querySelector("input") as HTMLInputElement).checked).toBe(true);
        expect((screen.getByTestId("assistant-font-coding-system").querySelector("input") as HTMLInputElement).checked).toBe(true);
        expect(screen.getByTestId("assistant-font-conversation-dengxian")).toBeTruthy();
        expect(screen.queryByTestId("assistant-font-conversation-jetbrains-mono")).toBeNull();
        expect(screen.getByTestId("assistant-font-coding-jetbrains-mono")).toBeTruthy();
        expect(screen.queryByTestId("assistant-font-coding-dengxian")).toBeNull();

        fireEvent.click(screen.getByTestId("assistant-font-coding-maple-mono").querySelector("input") as HTMLInputElement);
        expect(props.setCodingFontFamily).toHaveBeenCalledWith("maple-mono");
        expect(SetCodingFontFamily).toHaveBeenCalledWith("maple-mono");
        expect(props.setChatFontFamily).not.toHaveBeenCalled();
        expect(SetChatFontFamily).not.toHaveBeenCalled();
    });

    it("keeps the chosen face when an older config snapshot arrives before the save settles", async () => {
        render(<FamilyEchoHarness />);
        fireEvent.click(codingInput("maple-mono"));
        expect(codingInput("maple-mono").checked).toBe(true);
        fireEvent.click(screen.getByRole("button", { name: "echo-system" }));
        expect(codingInput("maple-mono").checked).toBe(true);
        await act(async () => {
            await vi.mocked(SetCodingFontFamily).mock.results[0]?.value;
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-system" }));
        expect(codingInput("system").checked).toBe(true);
    });

    it("returns to the stored face after the save fails, and choosing it again saves", async () => {
        vi.mocked(SetCodingFontFamily).mockRejectedValueOnce(new Error("disk"));
        render(<FamilyEchoHarness />);
        fireEvent.click(codingInput("maple-mono"));
        await act(async () => {
            await vi.mocked(SetCodingFontFamily).mock.results[0]?.value.catch(() => undefined);
            await codingFontFamilySaveQueue.tail;
        });
        expect(codingInput("system").checked).toBe(true);
        fireEvent.click(codingInput("maple-mono"));
        expect(SetCodingFontFamily).toHaveBeenCalledTimes(2);
        expect(SetCodingFontFamily).toHaveBeenLastCalledWith("maple-mono");
    });

    it("keeps the chosen face when the save and the stored read both fail", async () => {
        vi.mocked(SetCodingFontFamily).mockRejectedValueOnce(new Error("disk"));
        vi.mocked(GetCodingFontFamily).mockRejectedValueOnce(new Error("unreadable"));
        render(<FamilyEchoHarness />);
        fireEvent.click(codingInput("maple-mono"));
        await act(async () => {
            await vi.mocked(SetCodingFontFamily).mock.results[0]?.value.catch(() => undefined);
            await codingFontFamilySaveQueue.tail;
        });
        expect(codingInput("maple-mono").checked).toBe(true);
        fireEvent.click(codingInput("maple-mono"));
        expect(SetCodingFontFamily).toHaveBeenCalledTimes(2);
        expect(SetCodingFontFamily).toHaveBeenLastCalledWith("maple-mono");
    });

    it("accepts the previous face again after the save fails", async () => {
        vi.mocked(SetCodingFontFamily).mockRejectedValueOnce(new Error("disk"));
        render(<FamilyEchoHarness />);
        fireEvent.click(codingInput("maple-mono"));
        expect(codingInput("maple-mono").checked).toBe(true);
        await act(async () => {
            await vi.mocked(SetCodingFontFamily).mock.results[0]?.value.catch(() => undefined);
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-system" }));
        expect(codingInput("system").checked).toBe(true);
    });

    it("sends the newer face after the older save, and ignores the older snapshot until then", async () => {
        let releaseMaple: () => void = () => {};
        let releaseJetBrains: () => void = () => {};
        vi.mocked(SetCodingFontFamily)
            .mockImplementationOnce(() => new Promise<void>((resolve) => {
                releaseMaple = resolve;
            }))
            .mockImplementationOnce(() => new Promise<void>((resolve) => {
                releaseJetBrains = resolve;
            }));
        render(<FamilyEchoHarness />);
        fireEvent.click(codingInput("maple-mono"));
        fireEvent.click(codingInput("jetbrains-mono"));
        expect(SetCodingFontFamily).toHaveBeenCalledTimes(1);
        expect(SetCodingFontFamily).toHaveBeenCalledWith("maple-mono");
        expect(codingInput("jetbrains-mono").checked).toBe(true);
        fireEvent.click(screen.getByRole("button", { name: "echo-maple" }));
        expect(codingInput("jetbrains-mono").checked).toBe(true);
        await act(async () => {
            releaseMaple();
        });
        expect(SetCodingFontFamily).toHaveBeenCalledTimes(2);
        expect(SetCodingFontFamily).toHaveBeenLastCalledWith("jetbrains-mono");
        fireEvent.click(screen.getByRole("button", { name: "echo-maple" }));
        expect(codingInput("jetbrains-mono").checked).toBe(true);
        await act(async () => {
            releaseJetBrains();
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-maple" }));
        expect(codingInput("maple-mono").checked).toBe(true);
    });

    it("sends the same face again when the earlier save fails after another choice of it", async () => {
        let rejectFirst: (error: Error) => void = () => {};
        let releaseAgain: () => void = () => {};
        vi.mocked(SetCodingFontFamily)
            .mockImplementationOnce(() => new Promise<void>((_resolve, reject) => {
                rejectFirst = reject;
            }))
            .mockImplementationOnce(() => new Promise<void>((resolve) => {
                releaseAgain = resolve;
            }));
        render(<FamilyEchoHarness />);
        fireEvent.click(codingInput("maple-mono"));
        fireEvent.click(codingInput("jetbrains-mono"));
        fireEvent.click(codingInput("maple-mono"));
        expect(SetCodingFontFamily).toHaveBeenCalledTimes(1);
        await act(async () => {
            rejectFirst(new Error("disk"));
        });
        expect(SetCodingFontFamily).toHaveBeenCalledTimes(2);
        expect(SetCodingFontFamily).toHaveBeenLastCalledWith("maple-mono");
        fireEvent.click(screen.getByRole("button", { name: "echo-system" }));
        expect(codingInput("maple-mono").checked).toBe(true);
        await act(async () => {
            releaseAgain();
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-system" }));
        expect(codingInput("system").checked).toBe(true);
    });
});

function FamilyEchoHarness() {
    const draftRef = useRef<string | null>(null);
    const [codingFontFamily, setCodingFontFamily] = useState("system");
    const applySnapshot = (incoming: string) => {
        const id = normalizeAssistantFontId("coding", incoming);
        if (acceptDraftedValue(id, draftRef.current) == null) return;
        if (snapshotConfirmsDraft(id, draftRef.current, serialNewestHasStarted(codingFontFamilySaveQueue))) {
            draftRef.current = null;
        }
        setCodingFontFamily(id);
    };
    return (
        <>
            <button type="button" onClick={() => applySnapshot("system")}>echo-system</button>
            <button type="button" onClick={() => applySnapshot("maple-mono")}>echo-maple</button>
            <UISettingsPanel
                {...props}
                codingFontFamily={codingFontFamily}
                setCodingFontFamily={setCodingFontFamily}
                codingFontFamilyDraftRef={draftRef}
            />
        </>
    );
}

function FontSizeHarness() {
    const [chatFontSize, setChatFontSize] = useState(14);
    return <UISettingsPanel {...props} chatFontSize={chatFontSize} setChatFontSize={setChatFontSize} />;
}

function ApiStoreFontSizeHarness() {
    const [chatFontSize, setChatFontSize] = useState(14);
    return <ApiStorePage lang="zh-Hans" t={(key) => key} chatFontSize={chatFontSize} setChatFontSize={setChatFontSize} />;
}

function EchoHarness({ initial = 14 }: { initial?: number }) {
    const draftRef = useRef<number | null>(null);
    const [chatFontSize, setChatFontSize] = useState(initial);
    const applySnapshot = (incoming: number) => {
        const decision = decideConfigChatFontSize(incoming, draftRef.current);
        if (decision.action !== "apply") return;
        if (snapshotConfirmsDraft(decision.size, draftRef.current, serialNewestHasStarted(chatFontSizeSaveQueue))) {
            draftRef.current = null;
        }
        setChatFontSize(decision.size);
    };
    return (
        <>
            <span data-testid="size-now">{chatFontSize}</span>
            <button type="button" onClick={() => applySnapshot(14)}>echo-old</button>
            <button type="button" onClick={() => applySnapshot(18)}>echo-18</button>
            <button type="button" onClick={() => applySnapshot(22)}>echo-22</button>
            <UISettingsPanel {...props} chatFontSize={chatFontSize} setChatFontSize={setChatFontSize} chatFontSizeDraftRef={draftRef} />
        </>
    );
}

function ExternalSizeHarness() {
    const [chatFontSize, setChatFontSize] = useState(18);
    return (
        <>
            <button type="button" onClick={() => setChatFontSize(22)}>external-size</button>
            <UISettingsPanel {...props} chatFontSize={chatFontSize} setChatFontSize={setChatFontSize} />
        </>
    );
}

describe("chat font size commit", () => {
    afterEach(() => {
        cleanup();
        vi.clearAllTimers();
        vi.useRealTimers();
        restoreFontMocks();
        vi.clearAllMocks();
    });

    it("saves on keyboard and pointer release, and flushes a drag that leaves the page", async () => {
        vi.useFakeTimers();
        const { unmount } = render(<FontSizeHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });

        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.keyUp(slider, { key: "Tab" });
        expect(SetChatFontSize).not.toHaveBeenCalled();

        fireEvent.keyUp(slider, { key: "ArrowRight" });
        expect(SetChatFontSize).toHaveBeenCalledTimes(1);
        expect(SetChatFontSize).toHaveBeenCalledWith(18);
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value;
        });
        fireEvent.keyUp(slider, { key: "ArrowRight" });
        expect(SetChatFontSize).toHaveBeenCalledTimes(1);

        fireEvent.change(slider, { target: { value: "20" } });
        await act(async () => {
            await vi.advanceTimersByTimeAsync(CHAT_FONT_SIZE_PERSIST_DEBOUNCE_MS);
        });
        expect(SetChatFontSize).toHaveBeenCalledWith(20);

        fireEvent.change(slider, { target: { value: "16" } });
        fireEvent.pointerUp(slider);
        expect(SetChatFontSize).toHaveBeenLastCalledWith(16);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(CHAT_FONT_SIZE_PERSIST_DEBOUNCE_MS);
        });
        expect(SetChatFontSize).toHaveBeenCalledTimes(3);

        fireEvent.click(screen.getByRole("button", { name: "重置" }));
        expect(SetChatFontSize).toHaveBeenLastCalledWith(14);

        fireEvent.change(slider, { target: { value: "22" } });
        expect(SetChatFontSize).toHaveBeenCalledTimes(4);
        unmount();
        await act(async () => {
            await chatFontSizeSaveQueue.tail;
        });
        expect(SetChatFontSize).toHaveBeenCalledWith(22);
    });

    it("returns to the stored size after a failed save, and choosing it again saves", async () => {
        vi.useFakeTimers();
        render(<FontSizeHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        vi.mocked(SetChatFontSize).mockRejectedValueOnce(new Error("disk"));
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.keyUp(slider, { key: "ArrowRight" });
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value.catch(() => undefined);
            await chatFontSizeSaveQueue.tail;
        });
        expect((screen.getByRole("slider", { name: "AI 助手面板字号" }) as HTMLInputElement).value).toBe("14");
        fireEvent.change(screen.getByRole("slider", { name: "AI 助手面板字号" }), { target: { value: "18" } });
        fireEvent.keyUp(screen.getByRole("slider", { name: "AI 助手面板字号" }), { key: "ArrowRight" });
        expect(SetChatFontSize).toHaveBeenCalledTimes(2);
        expect(SetChatFontSize).toHaveBeenLastCalledWith(18);
    });

    it("saves a size again after another writer replaces the stored size", async () => {
        vi.useFakeTimers();
        render(<ExternalSizeHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "16" } });
        fireEvent.keyUp(slider, { key: "ArrowLeft" });
        expect(SetChatFontSize).toHaveBeenCalledWith(16);
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value;
        });
        vi.mocked(SetChatFontSize).mockClear();
        fireEvent.click(screen.getByRole("button", { name: "external-size" }));
        fireEvent.change(slider, { target: { value: "16" } });
        fireEvent.keyUp(slider, { key: "ArrowLeft" });
        expect(SetChatFontSize).toHaveBeenCalledWith(16);
    });

    it("keeps the newer size when an older config snapshot arrives during the drag", async () => {
        vi.useFakeTimers();
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        expect(screen.getByTestId("size-now").textContent).toBe("18");
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("18");
        fireEvent.change(slider, { target: { value: "20" } });
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value;
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("20");
    });

    it("accepts a config snapshot again after the save fails", async () => {
        vi.useFakeTimers();
        vi.mocked(SetChatFontSize).mockRejectedValueOnce(new Error("disk"));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value.catch(() => undefined);
            await chatFontSizeSaveQueue.tail;
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("14");
    });

    it("holds the failed size while the stored read is in flight", async () => {
        vi.useFakeTimers();
        let releaseRead: (size: number) => void = () => {};
        vi.mocked(GetChatFontSize).mockImplementationOnce(() => new Promise((resolve) => {
            releaseRead = resolve;
        }));
        vi.mocked(SetChatFontSize).mockRejectedValueOnce(new Error("disk"));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value.catch(() => undefined);
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-22" }));
        expect(screen.getByTestId("size-now").textContent).toBe("18");
        await act(async () => {
            releaseRead(14);
            await chatFontSizeSaveQueue.tail;
        });
        expect(screen.getByTestId("size-now").textContent).toBe("14");
    });

    it("still saves the size already on screen when StrictMode runs the effect twice", () => {
        vi.useFakeTimers();
        render(<StrictMode><EchoHarness initial={18} /></StrictMode>);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.pointerUp(slider);
        expect(SetChatFontSize).toHaveBeenCalledTimes(1);
        expect(SetChatFontSize).toHaveBeenCalledWith(18);
    });

    it("keeps the shown size while a write of that same size is still in flight", async () => {
        vi.useFakeTimers();
        render(<EchoHarness initial={18} />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.pointerUp(slider);
        expect(SetChatFontSize).toHaveBeenCalledTimes(1);
        expect(SetChatFontSize).toHaveBeenCalledWith(18);
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("18");
        await act(async () => {
            await vi.mocked(SetChatFontSize).mock.results[0]?.value;
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("14");
    });

    it("sends only the latest size after an older save, and keeps that size on screen", async () => {
        vi.useFakeTimers();
        let releaseFirst: () => void = () => {};
        vi.mocked(SetChatFontSize).mockImplementationOnce(() => new Promise<void>((resolve) => {
            releaseFirst = resolve;
        }));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        fireEvent.change(slider, { target: { value: "20" } });
        fireEvent.pointerUp(slider);
        fireEvent.change(slider, { target: { value: "22" } });
        fireEvent.pointerUp(slider);
        expect(SetChatFontSize).toHaveBeenCalledTimes(1);
        expect(SetChatFontSize).toHaveBeenCalledWith(18);
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("22");
        await act(async () => {
            releaseFirst();
        });
        expect(SetChatFontSize).toHaveBeenCalledTimes(2);
        expect(SetChatFontSize).toHaveBeenLastCalledWith(22);
        expect(screen.getByTestId("size-now").textContent).toBe("22");
    });

    it("saves a size again when a newer snapshot landed while the older save was in flight", async () => {
        vi.useFakeTimers();
        let releaseFirst: () => void = () => {};
        vi.mocked(SetChatFontSize).mockImplementationOnce(() => new Promise<void>((resolve) => {
            releaseFirst = resolve;
        }));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        fireEvent.click(screen.getByRole("button", { name: "echo-18" }));
        fireEvent.click(screen.getByRole("button", { name: "echo-22" }));
        expect(screen.getByTestId("size-now").textContent).toBe("22");
        await act(async () => {
            releaseFirst();
        });
        vi.mocked(SetChatFontSize).mockClear();
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        expect(SetChatFontSize).toHaveBeenCalledWith(18);
    });

    it("saves the size the user returned to after a newer snapshot, when the older save resolves late", async () => {
        vi.useFakeTimers();
        let releaseFirst: () => void = () => {};
        vi.mocked(SetChatFontSize).mockImplementationOnce(() => new Promise<void>((resolve) => {
            releaseFirst = resolve;
        }));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        fireEvent.click(screen.getByRole("button", { name: "echo-18" }));
        fireEvent.click(screen.getByRole("button", { name: "echo-22" }));
        fireEvent.change(slider, { target: { value: "18" } });
        await act(async () => {
            releaseFirst();
        });
        expect(SetChatFontSize).toHaveBeenCalledTimes(1);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(CHAT_FONT_SIZE_PERSIST_DEBOUNCE_MS);
        });
        expect(SetChatFontSize).toHaveBeenCalledTimes(2);
        expect(SetChatFontSize).toHaveBeenLastCalledWith(18);
    });

    it("keeps a queued return to the same size when the earlier snapshot matches it", async () => {
        vi.useFakeTimers();
        let releaseFirst: () => void = () => {};
        vi.mocked(SetChatFontSize).mockImplementationOnce(() => new Promise<void>((resolve) => {
            releaseFirst = resolve;
        }));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        fireEvent.change(slider, { target: { value: "20" } });
        fireEvent.pointerUp(slider);
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        fireEvent.click(screen.getByRole("button", { name: "echo-18" }));
        fireEvent.click(screen.getByRole("button", { name: "echo-22" }));
        expect(screen.getByTestId("size-now").textContent).toBe("18");
        await act(async () => {
            releaseFirst();
        });
        expect(SetChatFontSize).toHaveBeenCalledTimes(2);
        expect(SetChatFontSize).toHaveBeenLastCalledWith(18);
        expect(screen.getByTestId("size-now").textContent).toBe("18");
    });

    it("keeps a debounced retry when the earlier save of that size fails", async () => {
        vi.useFakeTimers();
        let rejectFirst: (error: Error) => void = () => {};
        vi.mocked(SetChatFontSize).mockImplementationOnce(() => new Promise<void>((_resolve, reject) => {
            rejectFirst = reject;
        }));
        render(<EchoHarness />);
        const slider = screen.getByRole("slider", { name: "AI 助手面板字号" });
        fireEvent.change(slider, { target: { value: "18" } });
        fireEvent.pointerUp(slider);
        fireEvent.change(slider, { target: { value: "20" } });
        fireEvent.change(slider, { target: { value: "18" } });
        await act(async () => {
            rejectFirst(new Error("disk"));
        });
        fireEvent.click(screen.getByRole("button", { name: "echo-old" }));
        expect(screen.getByTestId("size-now").textContent).toBe("18");
    });

    it("saves the API store slider on keyboard commit", () => {
        vi.useFakeTimers();
        render(<ApiStoreFontSizeHarness />);
        const slider = screen.getByRole("slider", { name: "聊天字体大小" });
        fireEvent.change(slider, { target: { value: "18" } });
        expect(SetChatFontSize).not.toHaveBeenCalled();
        fireEvent.keyUp(slider, { key: "ArrowRight" });
        expect(SetChatFontSize).toHaveBeenCalledWith(18);
    });
});
