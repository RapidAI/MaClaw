// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FileCompanionWindow, {
    clampCompanionChatWidth,
    clampCompanionTabScroll,
    companionTabEndsFromBoxes,
    companionTabNudgeDelta,
    companionTabScrollEnds,
    revealCompanionTab,
} from "./FileCompanionWindow";
import { finishCompanionReply, splitCompanionStreamDelta } from "./companionReasoning";
import { ToastProvider } from "../Toast";

const boot = vi.fn();
const open = vi.fn();
const save = vi.fn();
const send = vi.fn();
const importKnowledge = vi.fn();
const uploadCloud = vi.fn();
const ready = vi.fn();
const choose = vi.fn();
const replaceOffice = vi.fn();
const clearChat = vi.fn();
const quit = vi.fn();
const hideWindow = vi.fn();
const openResult = vi.fn();
const saveResult = vi.fn();

vi.mock("../../../wailsjs/go/main/App", () => ({
    GetFileCompanionBoot: (...args: unknown[]) => boot(...args),
    FileCompanionOpen: (...args: unknown[]) => open(...args),
    FileCompanionSaveText: (...args: unknown[]) => save(...args),
    SendFileCompanionMessage: (...args: unknown[]) => send(...args),
    FileCompanionImportKnowledge: (...args: unknown[]) => importKnowledge(...args),
    FileCompanionUploadCloud: (...args: unknown[]) => uploadCloud(...args),
    FileCompanionReplaceOffice: (...args: unknown[]) => replaceOffice(...args),
    ClearFileCompanionChat: (...args: unknown[]) => clearChat(...args),
    FileCompanionUIReady: (...args: unknown[]) => ready(...args),
    ChooseFileCompanionFiles: (...args: unknown[]) => choose(...args),
    OpenFileOrShowInFolder: (...args: unknown[]) => openResult(...args),
    ExportTaskResultFile: (...args: unknown[]) => saveResult(...args),
}));

vi.mock("../../../wailsjs/runtime", () => ({
    Quit: () => quit(),
    WindowHide: () => hideWindow(),
    EventsOn: () => {},
    EventsOff: () => {},
}));

// askOnOpen defaults off here. The window itself defaults it on. These cases
// open an empty file and count SendFileCompanionMessage calls.
function renderWindow(options?: { askOnOpen?: boolean }) {
    return render(
        <ToastProvider>
            <FileCompanionWindow askOnOpen={options?.askOnOpen ?? false} />
        </ToastProvider>,
    );
}

vi.mock("../ai/PptxPreviewPanel", () => ({
    PptxPreviewPanel: ({ absPath }: { absPath: string }) => <div data-testid="pptx-preview-panel">{absPath}</div>,
}));

vi.mock("../ai/DocxPreviewPanel", () => ({
    DocxPreviewPanel: ({ absPath }: { absPath: string }) => <div data-testid="docx-preview-panel">{absPath}</div>,
}));

vi.mock("../ai/PdfPreviewPanel", () => ({
    PdfPreviewPanel: ({ absPath }: { absPath: string }) => <div data-testid="pdf-preview-panel">{absPath}</div>,
}));

type DropCb = (x: number, y: number, paths: string[]) => void;
type EventCb = (payload: unknown) => void;

function installRuntime() {
    let drop: DropCb | null = null;
    const events = new Map<string, EventCb>();
    (window as unknown as { runtime: unknown }).runtime = {
        EventsOn: (name: string, cb: EventCb) => { events.set(name, cb); },
        EventsOff: (name: string) => { events.delete(name); },
        OnFileDrop: (cb: DropCb) => { drop = cb; },
        OnFileDropOff: () => { drop = null; },
    };
    return {
        fire: (paths: string[]) => drop?.(10, 10, paths),
        emit: (name: string, payload: unknown) => events.get(name)?.(payload),
    };
}

function doc(path: string, extra: Record<string, unknown> = {}) {
    const name = path.split(/[/\\]/).pop() || path;
    return { path, name, editable: false, readOnly: true, canExport: true, content: "", loadedHash: "h", sessionId: name, size: 12, error: "", ...extra };
}

beforeEach(() => {
    boot.mockReset();
    open.mockReset();
    save.mockReset();
    send.mockReset();
    importKnowledge.mockReset();
    uploadCloud.mockReset();
    ready.mockReset();
    choose.mockReset();
    replaceOffice.mockReset();
    clearChat.mockReset();
    quit.mockReset();
    hideWindow.mockReset();
    openResult.mockReset();
    saveResult.mockReset();
    localStorage.removeItem("maclaw.fileCompanionInputHistory.v1");
    ready.mockResolvedValue(undefined);
    replaceOffice.mockResolvedValue(undefined);
    clearChat.mockResolvedValue(undefined);
    send.mockResolvedValue(undefined);
    save.mockResolvedValue({ saved: true, diskHash: "next" });
    importKnowledge.mockResolvedValue({ importedFiles: 1 });
    uploadCloud.mockResolvedValue({ id: "draft" });
    choose.mockResolvedValue([]);
    openResult.mockResolvedValue(undefined);
    saveResult.mockResolvedValue("D:/out/paper.pdf");
});

describe("revealCompanionTab", () => {
    const rect = (left: number, right: number) => ({
        x: left, y: 0, width: right - left, height: 30, top: 0, right, bottom: 30, left, toJSON: () => ({}),
    });

    it("scrolls a filename that is past the row, and only that row", () => {
        const scrollport = {
            scrollLeft: 0,
            getBoundingClientRect: () => rect(100, 300),
        };
        const tab = { getBoundingClientRect: () => rect(420, 500) };

        revealCompanionTab(scrollport, tab);

        expect(scrollport.scrollLeft).toBe(200);
    });

    it("scrolls a filename that is left of the row back into view", () => {
        const scrollport = {
            scrollLeft: 80,
            getBoundingClientRect: () => rect(100, 300),
        };
        const tab = { getBoundingClientRect: () => rect(40, 90) };

        revealCompanionTab(scrollport, tab);

        expect(scrollport.scrollLeft).toBe(20);
    });

    it("leaves the row alone when the filename is already visible", () => {
        const scrollport = {
            scrollLeft: 40,
            getBoundingClientRect: () => rect(100, 400),
        };
        const tab = { getBoundingClientRect: () => rect(120, 200) };

        revealCompanionTab(scrollport, tab);

        expect(scrollport.scrollLeft).toBe(40);
    });
});

describe("companion tab nudges", () => {
    const port = { left: 0, right: 300 };
    const tabs = [
        { left: 0, right: 140 },
        { left: 146, right: 286 },
        { left: 292, right: 432 },
        { left: 438, right: 578 },
    ];

    it("brings the next clipped filename fully into the row", () => {
        expect(companionTabNudgeDelta(port, tabs, 1)).toBe(132);
    });

    it("skips a sliver so one click shows a new filename", () => {
        const barely = [
            { left: 0, right: 140 },
            { left: 146, right: 310 },
            { left: 316, right: 456 },
        ];
        expect(companionTabNudgeDelta(port, barely, 1)).toBe(156);
    });

    it("brings a filename that starts left of the row back in", () => {
        const shifted = tabs.map((tab) => ({ left: tab.left - 200, right: tab.right - 200 }));
        expect(companionTabNudgeDelta(port, shifted, -1)).toBe(54);
    });

    it("skips a sliver on the left so one click shows the filename before it", () => {
        const shifted = tabs.map((tab) => ({ left: tab.left - 160, right: tab.right - 160 }));
        expect(companionTabNudgeDelta(port, shifted, -1)).toBe(160);
    });

    it("does not move when every filename is already inside the row", () => {
        expect(companionTabNudgeDelta(port, tabs.slice(0, 2), 1)).toBe(0);
        expect(companionTabNudgeDelta(port, tabs.slice(0, 2), -1)).toBe(0);
    });

    it("reports the ends from the scroll range and clamps the next position", () => {
        expect(companionTabScrollEnds(0, 900, 300)).toEqual({ overflow: true, canLeft: false, canRight: true });
        expect(companionTabScrollEnds(600, 900, 300)).toEqual({ overflow: true, canLeft: true, canRight: false });
        expect(companionTabScrollEnds(0, 300, 300)).toEqual({ overflow: false, canLeft: false, canRight: false });
        expect(clampCompanionTabScroll(132, 50)).toBe(50);
        expect(clampCompanionTabScroll(-4, 50)).toBe(0);
    });

    it("hides the end buttons when spare scroll pixels do not clip a filename", () => {
        const fitted = [tabs[0], tabs[1]];
        expect(companionTabEndsFromBoxes(0, 8, port, fitted)).toEqual({ overflow: false, canLeft: false, canRight: false });
    });

    it("disables the end that can no longer move a filename", () => {
        expect(companionTabEndsFromBoxes(0, 200, port, tabs)).toEqual({ overflow: true, canLeft: false, canRight: true });
        const shifted = tabs.map((tab) => ({ left: tab.left - 200, right: tab.right - 200 }));
        expect(companionTabEndsFromBoxes(200, 200, port, shifted)).toEqual({ overflow: true, canLeft: true, canRight: false });
    });
});

describe("FileCompanionWindow", () => {
    it("switching tabs changes the visible chat and the input target", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: path.endsWith("a.md") ? "alpha" : "beta" }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        await screen.findByTestId("file-companion-tab-b.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "only-a" } });
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "only-b" } });
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("only-a");
    });

    it("one tab's message is absent from the other", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "hello from a" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        expect(screen.getByTestId("file-companion-chat").textContent).toContain("hello from a");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect(screen.getByTestId("file-companion-chat").textContent ?? "").not.toContain("hello from a");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "hello from b" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        expect(screen.getByTestId("file-companion-chat").textContent).toContain("hello from b");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect(screen.getByTestId("file-companion-chat").textContent).toContain("hello from a");
        expect(screen.getByTestId("file-companion-chat").textContent ?? "").not.toContain("hello from b");
    });

    it("sends on Enter and leaves Shift+Enter and IME confirmation alone", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        installRuntime();
        renderWindow();
        const input = await screen.findByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "第一行" } });
        fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
        fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
        expect(send).not.toHaveBeenCalled();
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        const bubble = screen.getByTestId("file-companion-message-user");
        expect(bubble.textContent).toBe("第一行");
        expect(bubble.style.borderRadius).not.toBe("");
    });

    it("remembers a sent question while the reply is still running", async () => {
        let release: () => void = () => {};
        send.mockReturnValue(new Promise<void>((resolve) => { release = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        const input = await screen.findByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "文件里有什么" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toContain("文件里有什么"));
        expect(send).toHaveBeenCalledTimes(1);
        fireEvent.change(input, { target: { value: "" } });
        input.setSelectionRange(0, 0);
        fireEvent.keyDown(input, { key: "ArrowUp" });
        await waitFor(() => expect(input.value).toBe("文件里有什么"));
        await act(async () => { release(); });
    });

    it("recalls a sent question with ArrowUp and leaves the assistant history alone", async () => {
        localStorage.setItem("ai-assistant-prompt-history", JSON.stringify(["助手里的一句"]));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        const input = await screen.findByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "文件里有什么" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        fireEvent.change(input, { target: { value: "" } });
        input.setSelectionRange(0, 0);
        fireEvent.keyDown(input, { key: "ArrowUp" });
        await waitFor(() => expect(input.value).toBe("文件里有什么"));
        expect(JSON.parse(localStorage.getItem("ai-assistant-prompt-history") || "[]")).toEqual(["助手里的一句"]);
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toContain("文件里有什么");
    });

    it("does not store the paper button as input history", async () => {
        localStorage.setItem("ai-assistant-prompt-history", JSON.stringify(["助手里的一句"]));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-paper");
        fireEvent.click(screen.getByTestId("file-companion-paper"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toBeNull();
        expect(JSON.parse(localStorage.getItem("ai-assistant-prompt-history") || "[]")).toEqual(["助手里的一句"]);
    });

    it("shows a later file instead of leaving the first one on screen", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: path.endsWith("b.md") ? "beta" : "alpha" }));
        const runtime = installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
        runtime.emit("file-companion:open", ["C:/notes/b.md"]);
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("beta");
        });
    });

    it("asks for a file when the window opens empty, and again when shown with no tab", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
        await act(async () => {
            await Promise.resolve();
        });
        expect(screen.getByTestId("file-companion-empty")).toBeTruthy();
        expect(screen.getByTestId("file-companion-choose").textContent).toBe("打开文件");
        choose.mockClear();
        runtime.emit("file-companion:show", null);
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
    });

    it("shows an already open file without asking again", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        const runtime = installRuntime();
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        expect(choose).not.toHaveBeenCalled();
        runtime.emit("file-companion:show", null);
        await act(async () => {
            await Promise.resolve();
        });
        expect(choose).not.toHaveBeenCalled();
        const strip = screen.getByTestId("file-companion-titlebar-files");
        const openTab = screen.getByTestId("file-companion-choose");
        const bar = screen.getByTestId("file-companion-titlebar");
        expect(openTab.parentElement).toBe(bar);
        expect(strip.compareDocumentPosition(openTab) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(openTab.closest("[role='tablist']")).toBeNull();
        expect(strip.contains(openTab)).toBe(false);
        const gap = screen.getByTestId("file-companion-titlebar-gap");
        expect(gap.parentElement).toBe(bar);
        expect(openTab.compareDocumentPosition(gap) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(gap.compareDocumentPosition(screen.getByTestId("file-companion-export")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(screen.getByTestId("file-companion-tab-a.md").querySelector(".file-companion-tab-close")).toBeTruthy();
    });

    it("does not ask when show arrives before boot files are applied", async () => {
        let releaseBoot: (value: { paths: string[] }) => void = () => undefined;
        boot.mockImplementation(() => new Promise((resolve) => { releaseBoot = resolve; }));
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        const runtime = installRuntime();
        renderWindow();
        await act(async () => { await Promise.resolve(); });
        runtime.emit("file-companion:show", null);
        expect(choose).not.toHaveBeenCalled();
        await act(async () => { releaseBoot({ paths: ["C:/notes/a.md"] }); });
        await screen.findByTestId("file-companion-tab-a.md");
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
        expect(choose).not.toHaveBeenCalled();
    });

    it("asks once when show arrives before an empty boot settles", async () => {
        let releaseBoot: (value: { paths: string[] }) => void = () => undefined;
        boot.mockImplementation(() => new Promise((resolve) => { releaseBoot = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        await act(async () => { await Promise.resolve(); });
        runtime.emit("file-companion:show", null);
        expect(choose).not.toHaveBeenCalled();
        await act(async () => { releaseBoot({ paths: [] }); });
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("file-companion-empty")).toBeTruthy();
    });

    it("does not ask when a file is delivered as the window becomes ready", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        ready.mockResolvedValue(["C:/notes/a.md"]);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
        expect(choose).not.toHaveBeenCalled();
    });

    it("shows the file that arrived after the boot read", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: path.endsWith("b.md") ? "beta" : "alpha" }));
        ready.mockResolvedValue(["C:/notes/b.md"]);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        await screen.findByTestId("file-companion-tab-b.md");
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("beta");
        const files = screen.getByTestId("file-companion-titlebar-files");
        const chooseButton = screen.getByTestId("file-companion-choose");
        expect(chooseButton.parentElement).toBe(screen.getByTestId("file-companion-titlebar"));
        expect(files.compareDocumentPosition(chooseButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(files.contains(chooseButton)).toBe(false);
        expect(screen.getByTestId("file-companion-tab-a.md").parentElement).toBe(screen.getByTestId("file-companion-titlebar-tabs"));
        expect(choose).not.toHaveBeenCalled();
    });

    it("opens dialog files in selection order and shows the last one", async () => {
        boot.mockResolvedValue({ paths: [] });
        let releaseFirst: (value: ReturnType<typeof doc>) => void = () => undefined;
        open.mockImplementation((path: string) => {
            if (String(path).endsWith("a.md")) return new Promise((resolve) => { releaseFirst = resolve; });
            return Promise.resolve(doc(String(path), { editable: true, readOnly: false, content: "beta" }));
        });
        installRuntime();
        renderWindow();
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
        await screen.findByTestId("file-companion-empty");
        choose.mockResolvedValue(["C:/notes/a.md", "C:/notes/b.md"]);
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        await waitFor(() => expect(open).toHaveBeenCalled());
        expect(open.mock.calls.map((call) => call[0])).toEqual(["C:/notes/a.md"]);
        await act(async () => {
            releaseFirst(doc("C:/notes/a.md", { editable: true, readOnly: false, content: "alpha" }));
        });
        await screen.findByTestId("file-companion-tab-b.md");
        expect(open.mock.calls.map((call) => call[0])).toEqual(["C:/notes/a.md", "C:/notes/b.md"]);
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("beta");
    });

    it("keeps a file that finishes opening after another tab is closed", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        let releaseNext: (value: ReturnType<typeof doc>) => void = () => undefined;
        open.mockImplementation((path: string) => {
            if (String(path).endsWith("b.md")) return new Promise((resolve) => { releaseNext = resolve; });
            return Promise.resolve(doc(String(path), { editable: true, readOnly: false, content: "alpha" }));
        });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        runtime.emit("file-companion:open", ["C:/notes/b.md"]);
        fireEvent.click(screen.getByLabelText("关闭 a.md"));
        expect(screen.queryByTestId("file-companion-tab-a.md")).toBeNull();
        await act(async () => {
            releaseNext(doc("C:/notes/b.md", { editable: true, readOnly: false, content: "beta" }));
        });
        await screen.findByTestId("file-companion-tab-b.md");
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("beta");
    });

    it("asks again after the last tab is closed and the window is shown", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByLabelText("关闭 a.md"));
        await screen.findByTestId("file-companion-empty");
        expect(choose).not.toHaveBeenCalled();
        runtime.emit("file-companion:show", null);
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
    });

    it("keeps the empty window usable when the file dialog fails", async () => {
        boot.mockResolvedValue({ paths: [] });
        choose.mockRejectedValueOnce(new Error("dialog failed"));
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
        choose.mockResolvedValue(["C:/notes/a.md"]);
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        await screen.findByTestId("file-companion-tab-a.md");
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
    });

    it("opens another file from the last tab", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: path.endsWith("c.md") ? "gamma" : "alpha" }));
        choose.mockResolvedValue(["C:/notes/c.md"]);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        await screen.findByTestId("file-companion-tab-c.md");
        const strip = screen.getByTestId("file-companion-titlebar-files");
        const chooseButton = screen.getByTestId("file-companion-choose");
        expect(chooseButton.parentElement).toBe(screen.getByTestId("file-companion-titlebar"));
        expect(strip.compareDocumentPosition(chooseButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("gamma");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
    });

    it("shows the file picked from the dialog while another file is open", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: path.endsWith("c.md") ? "gamma" : "alpha" }));
        choose.mockResolvedValue(["C:/notes/c.md"]);
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("gamma");
        });
    });

    it("places the companion title to the left of the file path", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        const brand = await screen.findByTestId("file-companion-brand");
        const tab = await screen.findByTestId("file-companion-tab-a.md");
        const bar = screen.getByTestId("file-companion-titlebar");
        expect(brand.textContent).toBe("MaClaw伴读");
        expect(brand.parentElement).toBe(bar);
        expect(tab.parentElement).toBe(screen.getByTestId("file-companion-titlebar-tabs"));
        expect(brand.compareDocumentPosition(tab) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it("moves the filename row from the end buttons when the names overflow", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md", "C:/notes/c.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-c.md");
        expect(screen.queryByTestId("file-companion-tabs-left")).toBeNull();
        expect(screen.queryByTestId("file-companion-tabs-right")).toBeNull();

        const row = screen.getByTestId("file-companion-titlebar-tabs");
        let scrolled = 0;
        Object.defineProperty(row, "scrollWidth", { configurable: true, value: 350 });
        Object.defineProperty(row, "clientWidth", { configurable: true, value: 300 });
        Object.defineProperty(row, "scrollLeft", {
            configurable: true,
            get: () => scrolled,
            set: (value: number) => { scrolled = value; },
        });
        row.getBoundingClientRect = () => ({ x: 0, y: 0, width: 300, height: 30, top: 0, bottom: 30, left: 0, right: 300, toJSON: () => ({}) });
        const boxes = [
            { left: 0, right: 140 },
            { left: 146, right: 286 },
            { left: 292, right: 432 },
        ];
        ["a.md", "b.md", "c.md"].forEach((name, index) => {
            const box = boxes[index];
            screen.getByTestId(`file-companion-tab-${name}`).getBoundingClientRect = () => ({
                x: box.left - scrolled,
                y: 0,
                width: box.right - box.left,
                height: 30,
                top: 0,
                bottom: 30,
                left: box.left - scrolled,
                right: box.right - scrolled,
                toJSON: () => ({}),
            });
        });
        fireEvent.scroll(row);

        const left = screen.getByTestId("file-companion-tabs-left");
        const right = screen.getByTestId("file-companion-tabs-right");
        const files = screen.getByTestId("file-companion-titlebar-files");
        expect(left.textContent).toBe("<<");
        expect(right.textContent).toBe(">>");
        expect(left.closest("[role='tablist']")).toBeNull();
        expect(right.closest("[role='tablist']")).toBeNull();
        expect(files.contains(left)).toBe(true);
        expect(files.contains(right)).toBe(true);
        expect(left.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(row.compareDocumentPosition(right) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect((left as HTMLButtonElement).disabled).toBe(true);
        expect((right as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(right);
        expect(row.scrollLeft).toBe(50);
        expect((screen.getByTestId("file-companion-tabs-left") as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByTestId("file-companion-tabs-right") as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByTestId("file-companion-tab-a.md").parentElement).toBe(row);
    });

    it("hides the companion window from the title bar and keeps the open file", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
        const choose = screen.getByTestId("file-companion-choose");
        const close = screen.getByTestId("file-companion-close");
        const bar = screen.getByTestId("file-companion-titlebar");
        expect(close.textContent).toBe("关闭");
        expect(close.querySelector("svg.file-companion-tool-icon")?.getAttribute("aria-hidden")).toBe("true");
        expect(close.getAttribute("title")).toBe("隐藏窗口，已打开的文件会保留");
        expect(close.parentElement).toBe(bar);
        expect(choose.parentElement).toBe(bar);
        expect(screen.getByTestId("file-companion-titlebar-files").compareDocumentPosition(choose) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        const gap = screen.getByTestId("file-companion-titlebar-gap");
        expect(gap.parentElement).toBe(bar);
        expect(choose.compareDocumentPosition(gap) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(gap.compareDocumentPosition(close) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(choose.className).toContain("file-companion-tool");
        expect(close.className).toContain("file-companion-tool--close");
        fireEvent.click(close);
        expect(hideWindow).toHaveBeenCalledTimes(1);
        expect(quit).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-tab-a.md")).toBeTruthy();
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha");
    });

    it("the two buttons invoke knowledge import and ImportMobileDocumentFromPath with the current tab path", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, canExport: true }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-b.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        const choose = screen.getByTestId("file-companion-choose");
        const exportBox = screen.getByTestId("file-companion-export");
        const files = screen.getByTestId("file-companion-titlebar-files");
        expect(choose.textContent).toBe("打开文件");
        expect(choose.parentElement).toBe(screen.getByTestId("file-companion-titlebar"));
        expect(files.compareDocumentPosition(choose) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(files.contains(choose)).toBe(false);
        expect(exportBox.parentElement).toBe(screen.getByTestId("file-companion-titlebar"));
        expect(choose.compareDocumentPosition(exportBox) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        const gap = screen.getByTestId("file-companion-titlebar-gap");
        expect(choose.compareDocumentPosition(gap) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(gap.compareDocumentPosition(exportBox) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(screen.getByTestId("file-companion-import").className).toContain("file-companion-tool");
        expect(screen.getByTestId("file-companion-upload").className).toContain("file-companion-tool");
        expect(screen.getByTestId("file-companion-paper").textContent).toContain("论文解读");
        expect(screen.getByTestId("file-companion-import").textContent).toContain("导入知识库");
        expect(screen.getByTestId("file-companion-upload").textContent).toContain("上传到云盘");
        for (const id of ["file-companion-paper", "file-companion-import", "file-companion-upload"]) {
            expect(screen.getByTestId(id).querySelector("svg.file-companion-tool-icon")?.getAttribute("aria-hidden")).toBe("true");
        }
        fireEvent.click(screen.getByTestId("file-companion-import"));
        fireEvent.click(screen.getByTestId("file-companion-upload"));
        await waitFor(() => {
            expect(importKnowledge).toHaveBeenCalledWith("C:/notes/b.md");
            expect(uploadCloud).toHaveBeenCalledWith("C:/notes/b.md");
        });
        expect(importKnowledge).not.toHaveBeenCalledWith("C:/notes/a.md");
        expect(uploadCloud).not.toHaveBeenCalledWith("C:/notes/a.md");
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("已上传到云盘，手机端「文档」可打开"))).toBe(true);
        });
        expect(screen.queryByTestId("file-companion-export-status")).toBeNull();
        expect(screen.getByTestId("file-companion-titlebar").textContent || "").not.toContain("已上传到云盘");
    });

    it("shows knowledge import progress, then the desktop library result", async () => {
        let finish: (value: unknown) => void = () => {};
        importKnowledge.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { canExport: true }));
        installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-import"));
        expect(screen.getByTestId("file-companion-import").textContent).toBe("正在导入…");
        expect((screen.getByTestId("file-companion-import") as HTMLButtonElement).disabled).toBe(true);
        expect(screen.queryByRole("alert")).toBeNull();
        await act(async () => { finish({ imported_files: 1, failed_files: 0, skipped_files: 0 }); });
        expect(screen.getByRole("alert").textContent).toContain("已进入桌面知识库");
        expect((screen.getByTestId("file-companion-import") as HTMLButtonElement).disabled).toBe(false);
    });

    it("does not upload the same file a second time", async () => {
        uploadCloud.mockResolvedValue({ id: "draft-1" });
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { canExport: true }));
        installRuntime();
        renderWindow();
        const button = await screen.findByTestId("file-companion-upload") as HTMLButtonElement;
        fireEvent.click(button);
        await waitFor(() => {
            expect(screen.getByRole("alert").textContent).toContain("已上传到云盘，手机端「文档」可打开");
        });
        expect(button.disabled).toBe(true);
        fireEvent.click(button);
        expect(uploadCloud).toHaveBeenCalledTimes(1);
    });

    it("does not send from another file while a reply is running", async () => {
        let release: () => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "问 A" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "问 B" } });
        const sendButton = screen.getByTestId("file-companion-send");
        expect(sendButton.getAttribute("aria-busy")).toBe("true");
        expect((screen.getByTestId("file-companion-paper") as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(sendButton);
        fireEvent.keyDown(screen.getByTestId("file-companion-input"), { key: "Enter" });
        expect(send).toHaveBeenCalledTimes(1);
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("问 B");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1") || "").not.toContain("问 B");
        await act(async () => { release(); });
    });

    it("spins the send button while a reply is running", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        let release: (value?: unknown) => void = () => undefined;
        send.mockImplementation(() => new Promise((resolve) => { release = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        fireEvent.change(await screen.findByTestId("file-companion-input"), { target: { value: "你好" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).toBe("true");
        });
        await act(async () => { release(); await Promise.resolve(); });
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在整理上下文并准备模型请求", request_id: "r1" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).toBe("true");
        });
        runtime.emit("ai-assistant-response", JSON.stringify({ session_key: "file-companion:a.md", text: "好的", request_id: "r1" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).not.toBe("true");
        });
    });

    it("clears the right chat on /clear and does not ask the model", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "总结一下" },
                { role: "assistant", text: "这是摘要" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是摘要");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        await screen.findByText("这是摘要");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "  ／CLEAR  " } });
        fireEvent.keyDown(screen.getByTestId("file-companion-input"), { key: "Enter" });
        await waitFor(() => expect(clearChat).toHaveBeenCalledWith("C:/notes/a.md"));
        expect(send).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-chat-empty")).toBeTruthy();
        expect(screen.queryByText("这是摘要")).toBeNull();
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect(screen.getByText("这是摘要")).toBeTruthy();
    });

    it("drops a reply that arrives after /clear", async () => {
        let release: () => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "旧回答" }],
        }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByText("旧回答");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "继续" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在整理上下文并准备模型请求", request_id: "r1" }));
        await screen.findByTestId("file-companion-progress");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "/clear" } });
        fireEvent.keyDown(screen.getByTestId("file-companion-input"), { key: "Enter" });
        await waitFor(() => expect(clearChat).toHaveBeenCalledTimes(1));
        expect(screen.queryByText("旧回答")).toBeNull();
        expect(screen.queryByText("继续")).toBeNull();
        const sent = send.mock.calls[0][0] as { request_id?: string };
        const requestID = sent.request_id || "r1";
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "不该出现", request_id: requestID }));
        runtime.emit("ai-assistant-response", JSON.stringify({ session_key: "file-companion:a.md", text: "不该出现", request_id: requestID }));
        await act(async () => { release(); await Promise.resolve(); });
        expect(screen.queryByText("不该出现")).toBeNull();
        expect(screen.getByTestId("file-companion-chat-empty")).toBeTruthy();
        expect(send).toHaveBeenCalledTimes(1);
    });

    it("sends /clear now as a question", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        fireEvent.change(await screen.findByTestId("file-companion-input"), { target: { value: "/clear now" } });
        fireEvent.keyDown(screen.getByTestId("file-companion-input"), { key: "Enter" });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(clearChat).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-message-user").textContent).toContain("/clear now");
    });

    it("shows the cloud upload error instead of staying silent", async () => {
        uploadCloud.mockRejectedValue("MaClaw Hub login is required to share documents to Mobile");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { canExport: true }));
        installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-upload"));
        await waitFor(() => {
            expect(screen.getByRole("alert").textContent).toContain("需要先在主窗口登录，才能上传到云盘");
        });
        expect((screen.getByTestId("file-companion-upload") as HTMLButtonElement).disabled).toBe(false);
    });

    it("dropping a directory does not create a tab", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { error: "directories are not opened as tabs", canExport: false }));
        const drop = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        await waitFor(() => {
            drop.fire(["C:/notes"]);
            expect(open).toHaveBeenCalledWith("C:/notes");
        });
        expect(screen.getByTestId("file-companion-drop-error").textContent).toContain("不支持打开文件夹");
        expect(screen.queryByTestId("file-companion-tab-notes")).toBeNull();
        expect(screen.getByTestId("file-companion-empty")).toBeTruthy();
    });

    it("pptx, docx, and pdf are handed to the existing preview panels", async () => {
        boot.mockResolvedValue({ paths: ["C:/deck.pptx", "C:/memo.docx", "C:/paper.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("pptx-preview-panel")).textContent).toContain("C:/deck.pptx");
        fireEvent.click(screen.getByTestId("file-companion-tab-memo.docx"));
        expect((await screen.findByTestId("docx-preview-panel")).textContent).toContain("C:/memo.docx");
        fireEvent.click(screen.getByTestId("file-companion-tab-paper.pdf"));
        expect((await screen.findByTestId("pdf-preview-panel")).textContent).toContain("C:/paper.pdf");
    });

    it("shows the assistant reply on the matching session_key only", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "临时进度", request_id: "r1" }));
        expect((await screen.findByTestId("file-companion-progress")).textContent).toContain("临时进度");
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "answer ", request_id: "r1" }));
        runtime.emit("ai-assistant-response", JSON.stringify({ session_key: "file-companion:a.md", text: "answer for a", request_id: "r1" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-chat").textContent).toContain("answer for a");
        });
        expect(screen.queryByTestId("file-companion-progress")).toBeNull();
        expect(screen.getByTestId("file-companion-chat").textContent ?? "").not.toContain("临时进度");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect(screen.getByTestId("file-companion-chat").textContent ?? "").not.toContain("answer for a");
        runtime.emit("ai-assistant-response", JSON.stringify({ session_key: "file-companion:b.md", text: "answer for b", request_id: "r2" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-chat").textContent).toContain("answer for b");
        });
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect(screen.getByTestId("file-companion-chat").textContent).toContain("answer for a");
        expect(screen.getByTestId("file-companion-chat").textContent ?? "").not.toContain("answer for b");
    });

    it("renders assistant markdown and leaves the user bubble as typed text", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "核心是 **问题背景**。\n\n1. 第一点",
            request_id: "md1",
        }));
        const bubble = await screen.findByTestId("file-companion-message-assistant");
        await waitFor(() => {
            expect(bubble.querySelector("strong")?.textContent).toBe("问题背景");
        });
        expect(bubble.textContent ?? "").not.toContain("**");
        expect(bubble.textContent).toContain("1.");
        expect(bubble.textContent).toContain("第一点");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "保留 **原文**" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("保留 **原文**");
    });

    it("submits an office payload when the reply has no local_file_path", async () => {
        boot.mockResolvedValue({ paths: ["C:/sheet/book.xlsx"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-book.xlsx");
        const payload = JSON.stringify({ sheets: [{ name: "Sheet1", rows: [[{ value: "n" }]] }] });
        runtime.emit("ai-assistant-response", JSON.stringify({ session_key: "file-companion:book.xlsx", text: payload, request_id: "office" }));
        await waitFor(() => {
            expect(replaceOffice).toHaveBeenCalledWith("C:/sheet/book.xlsx", payload);
        });
        replaceOffice.mockClear();
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:book.xlsx",
            text: payload,
            request_id: "office-done",
            local_file_path: "C:/sheet/book.xlsx",
        }));
        expect(replaceOffice).not.toHaveBeenCalled();
    });

    it("shows a read-only editor and the no-write-permission note", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/locked.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: false,
            readOnly: true,
            readOnlyReason: "permission",
            content: "# Title\nkeep",
        }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.readOnly).toBe(true);
        expect(editor.value).toContain("keep");
        expect(screen.getByTestId("file-companion-readonly-note").textContent).toContain("没有写入权限");
    });

    it("shows the too-large save note on a truncated text file", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/big.txt"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: false,
            readOnly: true,
            readOnlyReason: "too-large",
            content: "truncated preview",
        }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.readOnly).toBe(true);
        expect(editor.value).toContain("truncated preview");
        expect(screen.getByTestId("file-companion-readonly-note").textContent).toContain("文件过大，为避免截断写回，已禁止保存");
    });

    it("restores the transcript returned with the opened file", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            messages: [
                { role: "user", text: "上次的问题" },
                { role: "assistant", text: "上次的回答" },
            ],
        }));
        installRuntime();
        renderWindow();
        const chat = await screen.findByTestId("file-companion-chat");
        expect(chat.textContent).toContain("上次的问题");
        expect(chat.textContent).toContain("上次的回答");
    });

    it("shows an office refusal and does not write again", async () => {
        boot.mockResolvedValue({ paths: ["C:/sheet/book.xlsx"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-book.xlsx");
        const payload = JSON.stringify({ sheets: [{ name: "Sheet1", rows: [[{ value: "n" }]] }] });
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:book.xlsx",
            text: payload,
            error: "truncated extract cannot be written",
            request_id: "office-refuse",
        }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-chat").textContent).toContain("摘录被截断，已拒绝写回");
        });
        expect(replaceOffice).not.toHaveBeenCalled();
    });

    it("reloads the editor after a selection rewrite", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        let opens = 0;
        open.mockImplementation(async (path: string) => {
            opens += 1;
            return doc(path, { editable: true, readOnly: false, content: opens === 1 ? "alpha beta" : "alpha beta\n\ngamma", loadedHash: opens === 1 ? "h1" : "h2" });
        });
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.value).toContain("alpha beta");
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "gamma",
            local_file_path: "C:/notes/a.md",
            request_id: "rewrite",
        }));
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toContain("gamma");
        });
    });

    it("keeps unsaved edits when a rewrite lands on disk", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        let opens = 0;
        open.mockImplementation(async (path: string) => {
            opens += 1;
            return doc(path, {
                editable: true,
                readOnly: false,
                content: opens === 1 ? "alpha" : "alpha\n\nrewritten",
                loadedHash: opens === 1 ? "h1" : "h2",
            });
        });
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "alpha local" } });
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "rewritten",
            local_file_path: "C:/notes/a.md",
            request_id: "rewrite-dirty",
        }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-conflict").textContent).toContain("文件在磁盘上已被修改");
        });
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("alpha local");
    });

    it("keeps a draft typed while the send is still saving", async () => {
        const pendingSaves: Array<(value: { saved: boolean; diskHash: string }) => void> = [];
        save.mockImplementation(() => new Promise((resolve) => { pendingSaves.push(resolve); }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        const input = screen.getByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "先问这句" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(save).toHaveBeenCalled());
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "改成下一句" } });
        await act(async () => {
            // Autosave can sit ahead of this send on the same chain. Resolve
            // each waiter that appears until the message itself is handed off.
            for (let step = 0; step < 6 && send.mock.calls.length === 0; step += 1) {
                pendingSaves.splice(0).forEach((resolve) => resolve({ saved: true, diskHash: "h2" }));
                await Promise.resolve();
                await Promise.resolve();
            }
        });
        await waitFor(() => expect(send).toHaveBeenCalledWith(expect.objectContaining({ text: "先问这句" })));
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("改成下一句");
        expect(screen.getByTestId("file-companion-chat").textContent).toContain("先问这句");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toContain("先问这句");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).not.toContain("改成下一句");
    });

    it("saves a dirty editor before sending", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "改这一段" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1");
        expect(save.mock.invocationCallOrder[0]).toBeLessThan(send.mock.invocationCallOrder[0]);
    });

    it("sends the later offset when the selection text appears twice", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.txt"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha beta alpha" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        editor.setSelectionRange(11, 16);
        fireEvent.select(editor);
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "改这一段" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        expect(send).toHaveBeenCalledWith(expect.objectContaining({
            path: "C:/notes/a.txt",
            text: "改这一段",
            selection: "alpha",
            selection_start: 11,
        }));
    });

    it("follows the editor when the selected text changes", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.txt"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha beta alpha" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        editor.setSelectionRange(11, 16);
        fireEvent.select(editor);
        fireEvent.change(editor, { target: { value: "alpha beta ALPHA", selectionStart: 11, selectionEnd: 16 } });
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "改这一段" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        expect(send).toHaveBeenCalledWith(expect.objectContaining({
            selection: "ALPHA",
            selection_start: 11,
        }));
    });

    it("clears the selection when a markdown edit collapses it", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha beta alpha" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        editor.setSelectionRange(11, 16);
        fireEvent.select(editor);
        fireEvent.change(editor, { target: { value: "alpha beta alpha!", selectionStart: 17, selectionEnd: 17 } });
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "只讨论" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        const payload = send.mock.calls[0][0] as { selection?: string; selection_start?: number };
        expect(payload.selection).toBe("");
        expect(payload.selection_start).toBeUndefined();
    });

    it("drops a selection that is not in the reloaded file", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.txt"] });
        let opens = 0;
        open.mockImplementation(async (path: string) => {
            opens += 1;
            return doc(path, { editable: true, readOnly: false, content: opens === 1 ? "alpha beta alpha" : "no alpha here" });
        });
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        editor.setSelectionRange(11, 16);
        fireEvent.select(editor);
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.txt",
            text: "done",
            local_file_path: "C:/notes/a.txt",
            request_id: "reload-selection",
        }));
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("no alpha here");
        });
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "再问一句" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        const payload = send.mock.calls[0][0] as { selection?: string; selection_start?: number };
        expect(payload.selection).toBe("");
        expect(payload.selection_start).toBeUndefined();
    });

    it("keeps a selection that still matches after the rewrite is reloaded", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.txt"] });
        let opens = 0;
        open.mockImplementation(async (path: string) => {
            opens += 1;
            return doc(path, { editable: true, readOnly: false, content: opens === 1 ? "alpha beta alpha" : "alpha beta alpha\n\n译文" });
        });
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        editor.setSelectionRange(11, 16);
        fireEvent.select(editor);
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.txt",
            text: "译文",
            local_file_path: "C:/notes/a.txt",
            request_id: "reload-keep",
        }));
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toContain("译文");
        });
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "再改一次" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalled());
        expect(send).toHaveBeenCalledWith(expect.objectContaining({
            selection: "alpha",
            selection_start: 11,
        }));
    });

    it("does not send while a dirty edit is still in conflict", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        save.mockResolvedValue({ saved: false, conflict: true, diskHash: "disk" });
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-conflict").textContent).toContain("文件在磁盘上已被修改");
        }, { timeout: 2000 });
        const saves = save.mock.calls.length;
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "请改写" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(send).not.toHaveBeenCalled();
        expect(save.mock.calls.length).toBe(saves);
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("请改写");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toBeNull();
    });

    it("does not send when the file conflicts while the send is waiting to save", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        let opens = 0;
        open.mockImplementation(async (path: string) => {
            opens += 1;
            return doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: opens === 1 ? "h1" : "h2" });
        });
        let release: (value: { saved: boolean; diskHash: string }) => void = () => undefined;
        save.mockImplementation(() => new Promise((resolve) => { release = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1"), { timeout: 2000 });
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "请改写" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "note",
            local_file_path: "C:/notes/a.md",
            request_id: "reload-while-saving",
        }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-conflict").textContent).toContain("文件在磁盘上已被修改");
        });
        release({ saved: true, diskHash: "h2" });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-tab-a.md").textContent ?? "").not.toContain("*");
        });
        expect(send).not.toHaveBeenCalled();
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("v2");
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("请改写");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toBeNull();
    });

    it("ignores a slower open after a newer open of the same path", async () => {
        boot.mockResolvedValue({ paths: [] });
        let releaseFirst: (value: ReturnType<typeof doc>) => void = () => undefined;
        let calls = 0;
        open.mockImplementation((path: string) => {
            calls += 1;
            if (calls === 1) return new Promise((resolve) => { releaseFirst = resolve; });
            return Promise.resolve(doc(path, { editable: true, readOnly: false, content: "new", loadedHash: "h-new" }));
        });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        expect((await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("new");
        await act(async () => {
            releaseFirst(doc("C:/notes/a.md", { editable: true, readOnly: false, content: "old", loadedHash: "h-old" }));
            await Promise.resolve();
            await Promise.resolve();
        });
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("new");
        fireEvent.change(screen.getByTestId("file-companion-editor"), { target: { value: "typed" } });
        await waitFor(() => expect(save).toHaveBeenCalledWith("C:/notes/a.md", "typed", "h-new"), { timeout: 2000 });
    });

    it("saves a dirty tab as soon as the user leaves it", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        expect(save).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1");
    });

    it("saves a dirty tab when that tab is closed", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        expect(save).not.toHaveBeenCalled();
        fireEvent.click(screen.getByLabelText("关闭 a.md"));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1");
        expect(screen.getByTestId("file-companion-empty")).toBeTruthy();
    });

    it("puts the saved text back when an older reload arrives before the write finishes", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        let opens = 0;
        open.mockImplementation(async (path: string) => {
            opens += 1;
            if (opens === 1) return doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" });
            return doc(path, { editable: true, readOnly: false, content: "stale", loadedHash: "h-stale" });
        });
        let releaseSave: (value: { saved: boolean; diskHash: string }) => void = () => undefined;
        save.mockImplementation(() => new Promise((resolve) => { releaseSave = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1"), { timeout: 2000 });
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("stale");
        });
        releaseSave({ saved: true, diskHash: "h2" });
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("v2");
        });
        expect(save.mock.calls.map((call) => call[1])).not.toContain("stale");
    });

    it("reads the file again when a save lands while it is opening", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        let releaseOpen: (value: ReturnType<typeof doc>) => void = () => undefined;
        let opens = 0;
        open.mockImplementation((path: string) => {
            opens += 1;
            if (opens === 1) return Promise.resolve(doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
            if (opens === 2) return new Promise((resolve) => { releaseOpen = resolve; });
            return Promise.resolve(doc(path, { editable: true, readOnly: false, content: "v2", loadedHash: "h2" }));
        });
        let releaseSave: (value: { saved: boolean; diskHash: string }) => void = () => undefined;
        save.mockImplementation(() => new Promise((resolve) => { releaseSave = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1"), { timeout: 2000 });
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        await act(async () => {
            releaseSave({ saved: true, diskHash: "h2" });
            await Promise.resolve();
            await Promise.resolve();
        });
        await act(async () => {
            releaseOpen(doc("C:/notes/a.md", { editable: true, readOnly: false, content: "old", loadedHash: "h-old" }));
            await Promise.resolve();
            await Promise.resolve();
        });
        await waitFor(() => {
            expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("v2");
        });
        expect(opens).toBe(3);
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).not.toBe("old");
    });

    it("keeps the chat position when the reader has scrolled up", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1" }));
        const runtime = installRuntime();
        renderWindow();
        const log = await screen.findByTestId("file-companion-chat-log");
        Object.defineProperty(log, "scrollHeight", { configurable: true, value: 400 });
        Object.defineProperty(log, "clientHeight", { configurable: true, value: 80 });
        log.scrollTop = 0;
        fireEvent.scroll(log);
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "later", request_id: "r1" }));
        await waitFor(() => {
            expect(log.textContent).toContain("later");
        });
        expect(log.scrollTop).toBe(0);
    });

    it("keeps a later edit when an autosave finishes late", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        let release: (value: { saved: boolean; diskHash: string }) => void = () => undefined;
        save.mockImplementation(() => new Promise((resolve) => { release = resolve; }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => expect(save).toHaveBeenCalledWith("C:/notes/a.md", "v2", "h1"), { timeout: 2000 });
        fireEvent.change(screen.getByTestId("file-companion-editor"), { target: { value: "v3" } });
        release({ saved: true, diskHash: "h2" });
        await waitFor(() => expect(save.mock.calls.some((call) => call[1] === "v3")).toBe(true), { timeout: 2000 });
    });

    it("renders a completed heading in place and keeps the active line as source", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            content: "intro\n# Title\nkeep",
        }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.value).toBe("intro");
        expect(screen.getByTestId("file-companion-atx-line").textContent).toContain("Title");
        fireEvent.change(editor, { target: { value: "intro2" } });
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("intro2");
        expect(screen.getByTestId("file-companion-atx-line").textContent).toContain("Title");
        fireEvent.click(screen.getByTestId("file-companion-atx-line"));
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("intro2\n# Title\nkeep");
        expect(screen.queryByTestId("file-companion-atx-line")).toBeNull();
    });

    it("tooltips the full path and maps missing, directory, and unreadable tabs", async () => {
        boot.mockResolvedValue({ paths: ["C:/missing.txt", "C:/notes", "D:/locked.bin"] });
        open.mockImplementation(async (path: string) => {
            if (path.endsWith("missing.txt")) return doc(path, { error: "file not found", canExport: false });
            if (path.endsWith("notes")) return doc(path, { error: "directories are not opened as tabs", canExport: false });
            return doc(path, { error: "path is not readable", canExport: false });
        });
        installRuntime();
        renderWindow();
        const missing = await screen.findByTestId("file-companion-tab-missing.txt");
        expect(missing.getAttribute("title")).toBe("C:/missing.txt");
        expect(screen.getByTestId("file-companion-file-error").textContent).toContain("找不到文件");
        fireEvent.click(screen.getByTestId("file-companion-tab-notes"));
        expect(screen.getByTestId("file-companion-tab-notes").getAttribute("title")).toBe("C:/notes");
        expect(screen.getByTestId("file-companion-file-error").textContent).toContain("不支持打开文件夹");
        fireEvent.click(screen.getByTestId("file-companion-tab-locked.bin"));
        expect(screen.getByTestId("file-companion-file-error").textContent).toContain("没有读取权限");
    });

    it("shows an empty preview file without mounting the panel", async () => {
        boot.mockResolvedValue({ paths: ["C:/paper.pdf"] });
        open.mockImplementation(async (path: string) => doc(path, { size: 0, content: "" }));
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-empty-file")).textContent).toContain("文件是空的");
        expect(screen.queryByTestId("pdf-preview-panel")).toBeNull();
        expect(screen.queryByTestId("file-companion-preview")).toBeNull();
    });

    it("shows the unsupported preview message with the name and size", async () => {
        boot.mockResolvedValue({ paths: ["C:/src/app.go"] });
        open.mockImplementation(async (path: string) => doc(path, { size: 40, content: "", editable: false }));
        installRuntime();
        renderWindow();
        const note = await screen.findByTestId("file-companion-unsupported");
        expect(note.textContent).toContain("此文件类型暂不支持预览");
        expect(note.textContent).toContain("app.go");
        expect(note.textContent).toContain("40");
        expect(screen.queryByTestId("file-companion-editor")).toBeNull();
    });

    it("renders a leading heading outside the source editor", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            content: "# Title\nkeep",
        }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.value).toBe("keep");
        expect(screen.getByTestId("file-companion-atx-line").textContent).toContain("Title");
        fireEvent.click(screen.getByTestId("file-companion-atx-line"));
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("# Title\nkeep");
        expect(screen.queryByTestId("file-companion-atx-line")).toBeNull();
    });

    it("keeps the editor visible when a save fails", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        save.mockRejectedValue(new Error("file is not writable"));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-save-error").textContent).toContain("没有写入权限");
        }, { timeout: 2000 });
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("v2");
        expect(screen.queryByTestId("file-companion-file-error")).toBeNull();
    });

    it("does not overwrite on Ctrl+S while the file is in conflict", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1", loadedHash: "h1" }));
        save.mockResolvedValue({ saved: false, conflict: true, diskHash: "disk" });
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        fireEvent.change(editor, { target: { value: "v2" } });
        await waitFor(() => expect(save).toHaveBeenCalled(), { timeout: 2000 });
        expect(screen.getByTestId("file-companion-conflict").textContent).toContain("文件在磁盘上已被修改");
        save.mockClear();
        fireEvent.keyDown(window, { key: "s", ctrlKey: true });
        expect(save).not.toHaveBeenCalled();
    });

    it("switches an HTML file between source and preview", async () => {
        boot.mockResolvedValue({ paths: ["C:/page.html"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            content: "<html><body>Hello</body></html>",
        }));
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement).value).toContain("Hello");
        expect(screen.queryByTestId("html-preview-panel")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-html-toggle"));
        expect(await screen.findByTestId("html-preview-panel")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-editor")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-html-toggle"));
        expect((await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement).value).toContain("Hello");
    });

    it("keeps a fenced heading in the source and renders the heading after the fence", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            content: "```\n# Hidden\n```\n# Shown\nkeep",
        }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.value).toContain("# Hidden");
        expect(editor.value).not.toContain("# Shown");
        expect(screen.getByTestId("file-companion-atx-line").textContent).toContain("Shown");
    });

    it("keeps a heading inside a longer fence that contains a shorter marker", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            content: "````\n```\n# Hidden\n````\n# Shown\nkeep",
        }));
        installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        expect(editor.value).toContain("# Hidden");
        expect(editor.value).not.toContain("# Shown");
        expect(screen.getByTestId("file-companion-atx-line").textContent).toContain("Shown");
    });

    it("resets the heading caret when switching files", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            editable: true,
            readOnly: false,
            content: path.endsWith("a.md") ? "# One\nkeep" : "# Two\nbody",
        }));
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("keep");
        fireEvent.click(screen.getByTestId("file-companion-atx-line"));
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("# One\nkeep");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect((screen.getByTestId("file-companion-editor") as HTMLTextAreaElement).value).toBe("body");
        expect(screen.getByTestId("file-companion-atx-line").textContent).toContain("Two");
    });

    it("clamps the chat column so the preview keeps room", () => {
        expect(clampCompanionChatWidth(360, 1000)).toBe(360);
        expect(clampCompanionChatWidth(200, 1000)).toBe(280);
        expect(clampCompanionChatWidth(900, 1000)).toBe(748);
        expect(clampCompanionChatWidth(500, 400)).toBe(280);
    });

    it("drags the preview seam to resize the chat and keeps that width across tabs", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        const split = await screen.findByTestId("file-companion-split");
        const handle = screen.getByTestId("file-companion-split-handle");
        const chat = screen.getByTestId("file-companion-chat") as HTMLElement;
        expect(chat.style.width).toBe("360px");
        expect(handle.getAttribute("role")).toBe("separator");
        expect(handle.getAttribute("aria-orientation")).toBe("vertical");
        expect(handle.getAttribute("aria-valuemin")).toBe("280");
        expect(handle.getAttribute("aria-valuenow")).toBe("360");
        expect(handle.previousElementSibling?.classList.contains("file-companion-split-preview")).toBe(true);
        expect(handle.nextElementSibling).toBe(chat);
        Object.defineProperty(split, "clientWidth", { configurable: true, value: 1000 });

        fireEvent.pointerDown(handle, { button: 2, clientX: 500, pointerId: 7 });
        fireEvent.pointerMove(handle, { clientX: 400, pointerId: 7 });
        expect(chat.style.width).toBe("360px");

        fireEvent.pointerDown(handle, { button: 0, clientX: 500, pointerId: 1 });
        fireEvent.pointerMove(handle, { clientX: 420, pointerId: 1 });
        expect(chat.style.width).toBe("440px");
        expect(handle.getAttribute("aria-valuenow")).toBe("440");
        expect(handle.getAttribute("aria-valuemax")).toBe("748");
        fireEvent.pointerMove(handle, { clientX: 580, pointerId: 1 });
        expect(chat.style.width).toBe("280px");
        fireEvent.pointerMove(handle, { clientX: 700, pointerId: 1 });
        expect(chat.style.width).toBe("280px");
        fireEvent.pointerUp(handle, { clientX: 700, pointerId: 1 });
        fireEvent.pointerMove(handle, { clientX: 400, pointerId: 1 });
        expect(chat.style.width).toBe("280px");

        fireEvent.keyDown(handle, { key: "ArrowLeft" });
        expect(chat.style.width).toBe("296px");
        fireEvent.keyDown(handle, { key: "ArrowRight" });
        expect(chat.style.width).toBe("280px");
        fireEvent.keyDown(handle, { key: "End" });
        expect(chat.style.width).toBe("748px");
        fireEvent.keyDown(handle, { key: "Home" });
        expect(chat.style.width).toBe("280px");

        fireEvent.click(await screen.findByTestId("file-companion-tab-b.md"));
        expect((screen.getByTestId("file-companion-chat") as HTMLElement).style.width).toBe("280px");
    });

    it("strips reasoning-lane markers and square glyphs before they reach the bubble", () => {
        expect(splitCompanionStreamDelta("\x01The\x01 user\u25A1 wants")).toEqual({ reasoning: "The user wants", text: "" });
        expect(splitCompanionStreamDelta("正文\uFFFD答案")).toEqual({ reasoning: "", text: "正文答案" });
        expect(finishCompanionReply("The user wants", "The user wants", "正文答案", "The user wants the list.")).toEqual({
            text: "正文答案",
            reasoning: "The user wants the list.",
        });
    });

    it("shows restored thinking in a folded panel and keeps the answer free of squares", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "已有正文\u25A1", reasoning: "\x01先看\u25A1目录" }],
        }));
        installRuntime();
        renderWindow();
        const answer = await screen.findByTestId("file-companion-answer");
        const panel = screen.getByTestId("assistant-reasoning-panel");
        expect(screen.getByTestId("assistant-reasoning-label").textContent).toBe("思考过程");
        expect(panel.getAttribute("data-live")).toBe("false");
        expect((panel as HTMLDetailsElement).open).toBe(false);
        expect(screen.getByTestId("assistant-reasoning-body").textContent).toContain("先看目录");
        expect(screen.getByTestId("assistant-reasoning-body").textContent).not.toContain("□");
        expect(answer.textContent).toContain("已有正文");
        expect(answer.textContent).not.toContain("先看");
        expect(answer.textContent).not.toContain("□");
    });

    it("keeps a live thought in the thinking panel and the answer in the bubble", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-chat");
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在整理上下文并准备模型请求", request_id: "r9" }));
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "\x01The\x01 user\u25A1 wants", request_id: "r9" }));
        await waitFor(() => {
            expect(screen.getByTestId("assistant-reasoning-label").textContent).toBe("正在思考");
        });
        expect(screen.getByTestId("assistant-reasoning-panel").getAttribute("data-live")).toBe("true");
        expect((screen.getByTestId("assistant-reasoning-panel") as HTMLDetailsElement).open).toBe(true);
        expect(screen.getByTestId("assistant-reasoning-body").textContent).toContain("The user wants");
        expect(screen.getByTestId("assistant-reasoning-body").textContent).not.toContain("□");
        expect(screen.getByTestId("assistant-reasoning-body").textContent).not.toContain("\u0001");
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "正文答案", request_id: "r9" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-answer").textContent).toContain("正文答案");
        });
        expect(screen.getByTestId("file-companion-answer").textContent).not.toContain("The user wants");
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "正文答案",
            reasoning: "The user wants the chapter list.",
            request_id: "r9",
        }));
        await waitFor(() => {
            expect(screen.getByTestId("assistant-reasoning-label").textContent).toBe("思考过程");
        });
        expect(screen.getByTestId("assistant-reasoning-panel").getAttribute("data-live")).toBe("false");
        expect(screen.getByTestId("assistant-reasoning-body").textContent).toContain("The user wants the chapter list.");
        expect(screen.getByTestId("file-companion-answer").textContent).toContain("正文答案");
        expect(screen.getByTestId("file-companion-answer").textContent).not.toContain("chapter list");
    });

    it("hides 论文解读 until a file is open", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        expect(screen.queryByTestId("file-companion-paper")).toBeNull();
    });

    it("sends a paper turn and shows an openable result without preview", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha", sessionId: "a.md" }));
        let release: (value?: unknown) => void = () => undefined;
        send.mockImplementation(() => new Promise((resolve) => { release = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        const editor = await screen.findByTestId("file-companion-editor") as HTMLTextAreaElement;
        editor.setSelectionRange(0, 5);
        fireEvent.select(editor);
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "草稿不要发出去" } });
        fireEvent.click(screen.getByTestId("file-companion-paper"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-paper").textContent).toBe("正在解读…");
        });
        expect(send).toHaveBeenCalledTimes(1);
        const sent = send.mock.calls[0][0] as Record<string, unknown>;
        expect(sent.path).toBe("C:/notes/a.md");
        expect(sent.text).toBe("论文解读");
        expect(sent.purpose).toBe("paper");
        expect(sent.selection).toBeUndefined();
        expect(sent.selection_start).toBeUndefined();
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("草稿不要发出去");
        const requestID = String(sent.request_id);
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "SECRET_MARKDOWN", request_id: requestID }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-answer").textContent).toContain("正在生成论文解读");
        });
        expect(screen.getByTestId("file-companion-answer").textContent).not.toContain("SECRET_MARKDOWN");
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "SECRET_MARKDOWN\n\nhttps://example.com/data",
            local_file_path: "C:/maclaw/data/file-companion/results/a.md/a-论文解读.pdf",
            request_id: requestID,
        }));
        await act(async () => { release(); await Promise.resolve(); });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-answer").textContent).toContain("已生成论文解读");
        });
        expect(screen.getByTestId("file-companion-answer").textContent).not.toContain("SECRET_MARKDOWN");
        expect(screen.queryByTestId("task-result-preview-btn")).toBeNull();
        expect(screen.queryByTestId("task-result-continue-btn")).toBeNull();
        expect(screen.getByTestId("task-result-view-btn").textContent).toBe("查看文档");
        expect(open).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByTestId("task-result-view-btn"));
        expect(openResult).toHaveBeenCalledWith("C:/maclaw/data/file-companion/results/a.md/a-论文解读.pdf");
        fireEvent.click(screen.getByTestId("file-companion-paper-save"));
        await waitFor(() => {
            expect(saveResult).toHaveBeenCalledWith("C:/maclaw/data/file-companion/results/a.md/a-论文解读.pdf");
        });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-paper-save").textContent).toBe("已保存");
        });
    });

    it("asks what a new document is, once, and leaves that question out of input history", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        const runtime = installRuntime();
        renderWindow({ askOnOpen: true });
        await screen.findByTestId("file-companion-tab-a.md");
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        const sent = send.mock.calls[0][0] as Record<string, unknown>;
        expect(sent.path).toBe("C:/notes/a.md");
        expect(sent.text).toBe("这篇文档是什么？");
        expect(sent.selection).toBeUndefined();
        expect(sent.selection_start).toBeUndefined();
        expect(sent.purpose).toBeUndefined();
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("这篇文档是什么？");
        expect(screen.queryByTestId("file-companion-chat-empty")).toBeNull();
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toBeNull();
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        await waitFor(() => expect(open).toHaveBeenCalledTimes(2));
        await act(async () => { await Promise.resolve(); });
        expect(send).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("这篇文档是什么？");
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).not.toBe("true");
        });
        const input = screen.getByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "第一行" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
        const follow = send.mock.calls[1][0] as Record<string, unknown>;
        expect(follow.text).toBe("第一行");
        expect(follow.path).toBe("C:/notes/a.md");
        const history = localStorage.getItem("maclaw.fileCompanionInputHistory.v1") || "";
        expect(history).toContain("第一行");
        expect(history).not.toContain("这篇文档是什么？");
    });

    it("does not ask again after /clear", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow({ askOnOpen: true });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("这篇文档是什么？");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "/clear" } });
        fireEvent.keyDown(screen.getByTestId("file-companion-input"), { key: "Enter" });
        await waitFor(() => expect(clearChat).toHaveBeenCalledWith("C:/notes/a.md"));
        expect(screen.getByTestId("file-companion-chat-empty")).toBeTruthy();
        expect(screen.queryByText("这篇文档是什么？")).toBeNull();
        await act(async () => { await Promise.resolve(); });
        expect(send).toHaveBeenCalledTimes(1);
    });

    it("does not ask when the opened file already has a chat or could not be opened", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/missing.txt"] });
        open.mockImplementation(async (path: string) => {
            if (String(path).endsWith("missing.txt")) return doc(String(path), { error: "file not found", canExport: false });
            return doc(String(path), {
                messages: [
                    { role: "user", text: "总结一下" },
                    { role: "assistant", text: "这是摘要" },
                ],
            });
        });
        installRuntime();
        renderWindow({ askOnOpen: true });
        await screen.findByText("这是摘要");
        await screen.findByTestId("file-companion-tab-missing.txt");
        fireEvent.click(screen.getByTestId("file-companion-tab-missing.txt"));
        expect(screen.getByTestId("file-companion-file-error").textContent).toContain("找不到文件");
        await act(async () => { await Promise.resolve(); });
        expect(send).not.toHaveBeenCalled();
    });

    it("asks the next new document only after the first reply finishes", async () => {
        let release: () => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        installRuntime();
        renderWindow({ askOnOpen: true });
        await screen.findByTestId("file-companion-tab-a.md");
        await screen.findByTestId("file-companion-tab-b.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        const first = send.mock.calls[0][0] as Record<string, unknown>;
        expect(first.path).toBe("C:/notes/a.md");
        expect(first.text).toBe("这篇文档是什么？");
        expect(first.selection).toBeUndefined();
        const input = screen.getByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "我的问题" } });
        expect(input.value).toBe("我的问题");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect(screen.getByTestId("file-companion-chat-empty")).toBeTruthy();
        expect(screen.queryByText("这篇文档是什么？")).toBeNull();
        expect(send).toHaveBeenCalledTimes(1);
        await act(async () => { release(); await Promise.resolve(); });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
        const second = send.mock.calls[1][0] as Record<string, unknown>;
        expect(second.path).toBe("C:/notes/b.md");
        expect(second.text).toBe("这篇文档是什么？");
        expect(second.selection).toBeUndefined();
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-message-user").textContent).toBe("这篇文档是什么？");
        });
        expect(screen.queryByTestId("file-companion-chat-empty")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("我的问题");
    });

    it("asks again when a closed empty chat is opened, and skips a chat that was restored", async () => {
        let restore = false;
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, restore ? {
            messages: [{ role: "assistant", text: "这是摘要" }],
        } : {}));
        const runtime = installRuntime();
        renderWindow({ askOnOpen: true });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByLabelText("关闭 a.md"));
        await screen.findByTestId("file-companion-empty");
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("这篇文档是什么？");
        fireEvent.click(screen.getByLabelText("关闭 a.md"));
        await screen.findByTestId("file-companion-empty");
        restore = true;
        runtime.emit("file-companion:open", ["C:/notes/a.md"]);
        await screen.findByText("这是摘要");
        await act(async () => { await Promise.resolve(); });
        expect(send).toHaveBeenCalledTimes(2);
    });
});
