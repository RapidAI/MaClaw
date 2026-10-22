// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FileCompanionWindow, {
    clampCompanionChatWidth,
    clampCompanionTabScroll,
    companionOpenSetPaths,
    companionTabEndsFromBoxes,
    companionTabNudgeDelta,
    companionTabScrollEnds,
    revealCompanionTab,
} from "./FileCompanionWindow";
import { finishCompanionReply, splitCompanionStreamDelta } from "./companionReasoning";
import { ToastProvider } from "../Toast";

const boot = vi.fn();
const resume = vi.fn();
const setOpenSet = vi.fn();
const open = vi.fn();
const save = vi.fn();
const send = vi.fn();
const importKnowledge = vi.fn();
const uploadCloud = vi.fn();
const ready = vi.fn();
const choose = vi.fn();
const openArxiv = vi.fn(async (..._args: any[]): Promise<any> => undefined);
const replaceOffice = vi.fn();
const clearChat = vi.fn();
const quit = vi.fn();
const hideWindow = vi.fn();
const openResult = vi.fn();
const saveResult = vi.fn();
const figureDataURL = vi.fn();
const exportPdf = vi.fn();
const exportChatPdf = vi.fn();
const exportTex = vi.fn();
const exportChatTex = vi.fn();
const exportWebPdf = vi.fn();
const exportWebChatPdf = vi.fn();
const exportWebTex = vi.fn();
const exportWebChatTex = vi.fn();
const openWeb = vi.fn(async (..._args: any[]): Promise<any> => undefined);
const sendWeb = vi.fn();
const clearWeb = vi.fn();
const cancelTurn = vi.fn();
const cancelWeb = vi.fn();
const stopWeb = vi.fn();
const playerLeft = vi.fn();
const playerSupported = vi.fn(async (..._args: any[]): Promise<any> => true);
const playerPlace = vi.fn(async (..._args: any[]): Promise<any> => undefined);
const playerHide = vi.fn(async (..._args: any[]): Promise<any> => undefined);
const discardWeb = vi.fn();
const getBrandInfo = vi.fn();
const clipboardWrite = vi.fn();

vi.mock("../../../wailsjs/go/main/App", () => ({
    GetFileCompanionBoot: (...args: unknown[]) => boot(...args),
    FileCompanionResumePaths: (...args: unknown[]) => resume(...args),
    SetFileCompanionOpenSet: (...args: unknown[]) => setOpenSet(...args),
    FileCompanionOpen: (...args: unknown[]) => open(...args),
    FileCompanionSaveText: (...args: unknown[]) => save(...args),
    SendFileCompanionMessage: (...args: unknown[]) => send(...args),
    FileCompanionImportKnowledge: (...args: unknown[]) => importKnowledge(...args),
    FileCompanionUploadCloud: (...args: unknown[]) => uploadCloud(...args),
    FileCompanionReplaceOffice: (...args: unknown[]) => replaceOffice(...args),
    ClearFileCompanionChat: (...args: unknown[]) => clearChat(...args),
    FileCompanionUIReady: (...args: unknown[]) => ready(...args),
    ChooseFileCompanionFiles: (...args: unknown[]) => choose(...args),
    FileCompanionOpenArxiv: (...args: unknown[]) => openArxiv(...args),
    FileCompanionOpenWebPage: (...args: unknown[]) => openWeb(...args),
    FileCompanionPlayerSupported: (...args: unknown[]) => playerSupported(...args),
    FileCompanionPlayerPlace: (...args: unknown[]) => playerPlace(...args),
    FileCompanionPlayerHide: (...args: unknown[]) => playerHide(...args),
    SendFileCompanionWebMessage: (...args: unknown[]) => sendWeb(...args),
    ClearFileCompanionWebChat: (...args: unknown[]) => clearWeb(...args),
    CancelFileCompanionTurn: (...args: unknown[]) => cancelTurn(...args),
    CancelFileCompanionWebTurn: (...args: unknown[]) => cancelWeb(...args),
    StopFileCompanionWebNote: (...args: unknown[]) => stopWeb(...args),
    FileCompanionWebPlayerLeft: (...args: unknown[]) => playerLeft(...args),
    FileCompanionDiscardWebNote: (...args: unknown[]) => discardWeb(...args),
    FileCompanionWebHistory: vi.fn(async () => []),
    FileCompanionExportTeX: (...args: unknown[]) => exportTex(...args),
    FileCompanionExportChatTeX: (...args: unknown[]) => exportChatTex(...args),
    FileCompanionExportWebPDF: (...args: unknown[]) => exportWebPdf(...args),
    FileCompanionExportWebChatPDF: (...args: unknown[]) => exportWebChatPdf(...args),
    FileCompanionExportWebTeX: (...args: unknown[]) => exportWebTex(...args),
    FileCompanionExportWebChatTeX: (...args: unknown[]) => exportWebChatTex(...args),
    OpenFileOrShowInFolder: (...args: unknown[]) => openResult(...args),
    ExportTaskResultFile: (...args: unknown[]) => saveResult(...args),
    FileCompanionFigureDataURL: (...args: unknown[]) => figureDataURL(...args),
    FileCompanionExportPDF: (...args: unknown[]) => exportPdf(...args),
    FileCompanionExportChatPDF: (...args: unknown[]) => exportChatPdf(...args),
    PreviewTaskResultFile: async (path: string) => {
        const name = String(path).toLowerCase();
        if (name.endsWith(".mp4") || name.endsWith(".mp3")) {
            return { kind: name.endsWith(".mp3") ? "audio" : "video", preview_url: "/maclaw-preview/v1/file?t=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" };
        }
        return { kind: "text", content: "" };
    },
    GetBrandInfo: (...args: unknown[]) => getBrandInfo(...args),
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

// 打开 is a menu now: the 打开文件 row is what reaches the system dialog.
// An empty boot already has the menu open, so a click on the trigger would
// close it before the row is reached.
function pickLocalFile() {
    if (!screen.queryByTestId("file-companion-open-menu")) {
        fireEvent.click(screen.getByTestId("file-companion-choose"));
    }
    fireEvent.click(screen.getByTestId("file-companion-open-local"));
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

// Only the drawing is stubbed. jsdom has no canvas, so the real renderer would
// always fail here and the interesting behaviour — what the export does with a
// drawn diagram, and with one that will not draw — would never be exercised.
// isMermaidCodeFence stays real, so the fence detection under test is the same
// code the app runs.
const renderMermaidFenceToPNGDataURL = vi.fn();
vi.mock("../ai/AssistantMermaidDiagram", async (importOriginal) => {
    const actual = await importOriginal<typeof import("../ai/AssistantMermaidDiagram")>();
    return {
        ...actual,
        renderMermaidFenceToPNGDataURL: (...args: unknown[]) => renderMermaidFenceToPNGDataURL(...args),
    };
});

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
    resume.mockReset();
    resume.mockResolvedValue([]);
    setOpenSet.mockReset();
    setOpenSet.mockResolvedValue(undefined);
    open.mockReset();
    save.mockReset();
    send.mockReset();
    importKnowledge.mockReset();
    uploadCloud.mockReset();
    ready.mockReset();
    choose.mockReset();
    openArxiv.mockReset();
    openArxiv.mockResolvedValue("C:/papers/2510.16079.pdf");
    replaceOffice.mockReset();
    clearChat.mockReset();
    quit.mockReset();
    hideWindow.mockReset();
    openResult.mockReset();
    saveResult.mockReset();
    figureDataURL.mockReset();
    getBrandInfo.mockReset();
    getBrandInfo.mockResolvedValue({ displayName: "MaClaw" });
    exportPdf.mockReset();
    exportPdf.mockResolvedValue("");
    exportChatPdf.mockReset();
    exportChatPdf.mockResolvedValue("");
    exportTex.mockReset();
    exportTex.mockResolvedValue("");
    exportChatTex.mockReset();
    exportChatTex.mockResolvedValue("");
    exportWebPdf.mockReset();
    exportWebChatPdf.mockReset();
    exportWebTex.mockReset();
    exportWebChatTex.mockReset();
    openWeb.mockReset();
    sendWeb.mockReset();
    sendWeb.mockResolvedValue(undefined);
    clearWeb.mockReset();
    cancelTurn.mockReset();
    cancelTurn.mockResolvedValue(undefined);
    cancelWeb.mockReset();
    cancelWeb.mockResolvedValue(undefined);
    stopWeb.mockReset();
    stopWeb.mockResolvedValue(undefined);
    playerLeft.mockReset();
    playerLeft.mockResolvedValue(undefined);
    playerSupported.mockReset();
    playerSupported.mockResolvedValue(true);
    playerPlace.mockReset();
    playerPlace.mockResolvedValue(undefined);
    playerHide.mockReset();
    playerHide.mockResolvedValue(undefined);
    discardWeb.mockReset();
    discardWeb.mockResolvedValue(undefined);
    clipboardWrite.mockReset();
    clipboardWrite.mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText: clipboardWrite } });
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
    figureDataURL.mockImplementation(async (path: string) => `data:image/png;base64,${path}`);
    // Default: a diagram that will not draw, so a test only sees a picture when
    // it asks for one.
    renderMermaidFenceToPNGDataURL.mockReset();
    renderMermaidFenceToPNGDataURL.mockResolvedValue(null);
});

describe("companionOpenSetPaths", () => {
    it("keeps a file that could not be read and drops one that is gone", () => {
        const paths = companionOpenSetPaths([
            { path: "C:/notes/gone.pdf", error: "file not found" },
            { path: "C:/notes/locked.pdf", error: "file is not readable" },
            { path: "C:/notes/open.md", error: "" },
        ], "C:/notes/open.md");
        expect(paths).toEqual(["C:/notes/open.md", "C:/notes/locked.pdf"]);
    });
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

    it("opens the companion chooser when the window is empty, and again when shown with no tab", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-empty")).toBeTruthy();
        expect(screen.getByTestId("file-companion-choose").textContent).toBe("打开");
        expect(screen.getByTestId("file-companion-arxiv-input")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        runtime.emit("file-companion:show", null);
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
    });

    it("opens a menu with a local-file row and an arXiv row", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        const menu = await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        // No native player is on screen, so the panel stays centred.
        expect(menu.className).not.toContain("file-companion-open-menu--beside-player");
        expect(screen.getByTestId("file-companion-open-local").textContent).toContain("打开文件");
        const input = screen.getByTestId("file-companion-arxiv-input") as HTMLInputElement;
        expect(input.placeholder).toBe("如 2510.16079、arxiv.org/abs/…，或 youtube 等视频页面地址");
        // Nothing typed yet: 确定 has nothing to fetch.
        expect((screen.getByTestId("file-companion-arxiv-go") as HTMLButtonElement).disabled).toBe(true);
        // The bar clips its row; the menu has to un-clip it or it is cut off.
        expect(screen.getByTestId("file-companion-titlebar").className).toContain("file-companion-titlebar--menu-open");
        // The cluster sits in the window-drag region. On Windows the drag
        // handler ignores --wails-draggable and only honors this attribute, so
        // a shaky click on the arXiv hint would move the window.
        expect(screen.getByTestId("file-companion-open").hasAttribute("data-window-no-drag")).toBe(true);
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        expect(screen.getByTestId("file-companion-titlebar").className).not.toContain("file-companion-titlebar--menu-open");
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        // A click on the PDF never reaches document, because the preview is an
        // iframe. The scrim is what receives that click.
        fireEvent.mouseDown(screen.getByTestId("file-companion-open-scrim"));
        // React keeps a module-level mouseDown flag; a real pointer always
        // releases, so pair the press or every later onSelect in this file
        // sees stale "mouse dragging" state and swallows selection events.
        // The mouseup must fire on a node that is still inside the React root:
        // the scrim unmounts with the menu on mousedown, the titlebar survives.
        fireEvent.mouseUp(screen.getByTestId("file-companion-titlebar"));
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
    });

    it("本地文件 opens the system dialog and closes the menu", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-open-local"));
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
    });

    it("downloads an arXiv paper and opens it as a tab", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { editable: false, readOnly: true }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), {
            target: { value: "https://arxiv.org/abs/2510.16079v2" },
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        await waitFor(() => expect(openArxiv).toHaveBeenCalledWith("https://arxiv.org/abs/2510.16079v2"));
        await screen.findByTestId("file-companion-tab-2510.16079.pdf");
        expect(open).toHaveBeenCalledWith("C:/papers/2510.16079.pdf");
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
    });

    it("keeps the menu open and shows the reason when the download fails", async () => {
        boot.mockResolvedValue({ paths: [] });
        openArxiv.mockRejectedValueOnce(new Error("下载失败：找不到这篇论文（404）"));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "2510.99999" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-hint").textContent).toContain("404");
        });
        expect(screen.getByTestId("file-companion-arxiv-hint").dataset.state).toBe("error");
        // The panel stays so the ID can be corrected, and no tab was created.
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect((screen.getByTestId("file-companion-arxiv-input") as HTMLInputElement).value).toBe("2510.99999");
        expect(open).not.toHaveBeenCalled();
    });

    // Closing the panel mid-flight would drop the failure into an unmounted
    // panel: the user would see the spinner stop and nothing else.
    it("pins the menu open during a download so the outcome stays visible", async () => {
        boot.mockResolvedValue({ paths: [] });
        let release: (path: string) => void = () => undefined;
        openArxiv.mockReturnValueOnce(new Promise<string>((resolve) => { release = resolve; }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId("file-companion-arxiv-input")));
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "2510.16079" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        await waitFor(() => expect(screen.getByTestId("file-companion-arxiv-go").textContent).toBe("下载中…"));
        fireEvent.keyDown(document, { key: "Escape" });
        fireEvent.mouseDown(document.body);
        // mouseup must enter a React root container for React's module-level
        // mouseDown flag to reset; <body> itself is above every root.
        fireEvent.mouseUp(screen.getByTestId("file-companion-window"));
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        release("");
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-hint").textContent).toContain("没有拿到文件路径");
        });
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
    });

    // Closing with Escape must hand keyboard focus back to the trigger;
    // dropping it to <body> would restart the tab order at the document top.
    // An outside click must NOT do the same, or it would steal focus from
    // wherever the user just clicked.
    it("returns keyboard focus to the trigger on Escape but not on an outside click", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        const trigger = screen.getByTestId("file-companion-choose");
        await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId("file-companion-arxiv-input")));
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        expect(document.activeElement).toBe(trigger);

        fireEvent.click(trigger);
        fireEvent.mouseDown(document.body);
        // Outside click must NOT return focus to the trigger; the paired
        // mouseup has to be inside the React root to reset React's flag.
        fireEvent.mouseUp(screen.getByTestId("file-companion-window"));
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        expect(document.activeElement).not.toBe(trigger);
    });

    // The same drop-to-<body> defect reaches the success path: Enter submits
    // from the input, the download takes tens of seconds, and the panel then
    // unmounts the very element holding focus. openPath's focus flag only
    // activates the new tab, it does not move DOM focus.
    it("returns keyboard focus to the trigger after a successful arXiv open", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { editable: false, readOnly: true }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        const trigger = screen.getByTestId("file-companion-choose");
        const input = screen.getByTestId("file-companion-arxiv-input") as HTMLInputElement;
        await waitFor(() => expect(document.activeElement).toBe(input));
        expect(document.activeElement).toBe(input);
        fireEvent.change(input, { target: { value: "2510.16079" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await screen.findByTestId("file-companion-tab-2510.16079.pdf");
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        expect(document.activeElement).toBe(trigger);
    });

    // A download lasts long enough for the user to click into the document.
    // Closing the panel must not yank focus back to the trigger then.
    it("leaves focus where the user moved it during a download", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { editable: false, readOnly: true }));
        let release: (path: string) => void = () => undefined;
        openArxiv.mockReturnValueOnce(new Promise<string>((resolve) => { release = resolve; }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        const input = screen.getByTestId("file-companion-arxiv-input") as HTMLInputElement;
        fireEvent.change(input, { target: { value: "2510.16079" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        await waitFor(() => expect(screen.getByTestId("file-companion-arxiv-go").textContent).toBe("下载中…"));
        // The user gives up waiting and clicks into the document.
        const elsewhere = screen.getByTestId("file-companion-close") as HTMLButtonElement;
        elsewhere.focus();
        expect(document.activeElement).toBe(elsewhere);
        release("C:/papers/2510.16079.pdf");
        await screen.findByTestId("file-companion-tab-2510.16079.pdf");
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        // Focus stays where the user put it; it must not be yanked to the trigger.
        expect(document.activeElement).toBe(elsewhere);
    });

    // Enter on an empty box must say so: a silent no-op looks like a broken
    // button, which is the same class of bug as a swallowed download failure.
    it("explains an empty arXiv box instead of ignoring Enter", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.keyDown(screen.getByTestId("file-companion-arxiv-input"), { key: "Enter" });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-hint").textContent).toContain("请填写");
        });
        expect(openArxiv).not.toHaveBeenCalled();
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
        expect(openTab.closest("[data-testid='file-companion-titlebar']")).toBe(bar);
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

    it("restores remembered files and does not ask", async () => {
        boot.mockResolvedValue({ paths: [] });
        resume.mockResolvedValue(["C:/notes/a.md", "C:/notes/b.md"]);
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: path.endsWith("b.md") ? "beta" : "alpha" }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        await screen.findByTestId("file-companion-tab-b.md");
        expect(choose).not.toHaveBeenCalled();
        await waitFor(() => expect(setOpenSet).toHaveBeenCalled());
        const saved = setOpenSet.mock.calls.at(-1)?.[0] as string[];
        expect(saved).toEqual(["C:/notes/a.md", "C:/notes/b.md"]);
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
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
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
        expect(chooseButton.closest("[data-testid='file-companion-titlebar']")).toBe(screen.getByTestId("file-companion-titlebar"));
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
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        await screen.findByTestId("file-companion-empty");
        choose.mockResolvedValue(["C:/notes/a.md", "C:/notes/b.md"]);
        pickLocalFile();
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
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        runtime.emit("file-companion:show", null);
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
    });

    it("keeps the empty window usable when the file dialog fails", async () => {
        boot.mockResolvedValue({ paths: [] });
        choose.mockRejectedValueOnce(new Error("dialog failed"));
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        await screen.findByTestId("file-companion-open-menu");
        expect(choose).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-open-local"));
        await waitFor(() => expect(choose).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("file-companion-empty")).toBeTruthy();
        choose.mockResolvedValue(["C:/notes/a.md"]);
        pickLocalFile();
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
        pickLocalFile();
        await screen.findByTestId("file-companion-tab-c.md");
        const strip = screen.getByTestId("file-companion-titlebar-files");
        const chooseButton = screen.getByTestId("file-companion-choose");
        expect(chooseButton.closest("[data-testid='file-companion-titlebar']")).toBe(screen.getByTestId("file-companion-titlebar"));
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
        pickLocalFile();
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

    // The window ships under every OEM brand, so the title must follow the
    // backend's brand instead of a literal.
    it("titles the window with the build's brand", async () => {
        getBrandInfo.mockResolvedValue({ displayName: "TigerClaw" });
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        const brand = await screen.findByTestId("file-companion-brand");
        await waitFor(() => expect(brand.textContent).toBe("TigerClaw伴读"));
        expect(brand.getAttribute("aria-label")).toBe("TigerClaw伴读");
    });

    it("keeps the default title when the brand call fails", async () => {
        getBrandInfo.mockRejectedValue(new Error("backend unavailable"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        const brand = await screen.findByTestId("file-companion-brand");
        expect(brand.textContent).toBe("MaClaw伴读");
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
        expect(close.textContent).toBe("隐藏");
        expect(close.querySelector("svg.file-companion-tool-icon")?.getAttribute("data-icon")).toBe("hide");
        expect(close.querySelector("svg.file-companion-tool-icon")?.getAttribute("aria-hidden")).toBe("true");
        expect(close.getAttribute("title")).toBe("隐藏窗口，已打开的文件会保留");
        expect(close.parentElement).toBe(bar);
        expect(choose.closest("[data-testid='file-companion-titlebar']")).toBe(bar);
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

    it("hides the window without stopping a web note", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        playerPlace.mockClear();
        playerHide.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        discardWeb.mockClear();
        fireEvent.click(screen.getByTestId("file-companion-close"));
        expect(hideWindow).toHaveBeenCalledTimes(1);
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false }); });
        window.dispatchEvent(new Event("focus"));
        window.dispatchEvent(new Event("resize"));
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(playerLeft).not.toHaveBeenCalled();
        expect(discardWeb).not.toHaveBeenCalled();
        expect(playerHide).not.toHaveBeenCalled();
        expect(playerPlace).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-tab-www.youtube.com")).toBeTruthy();
        expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        release();
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
    });

    it("treats a hidden document as a hide, then places the player once the slot is real", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        const frame = await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
        playerPlace.mockClear();
        playerHide.mockClear();
        cancelWeb.mockClear();
        const prior = Object.getOwnPropertyDescriptor(Document.prototype, "visibilityState")
            ?? Object.getOwnPropertyDescriptor(document, "visibilityState");
        let state: DocumentVisibilityState = "hidden";
        Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
        try {
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false }); });
            window.dispatchEvent(new Event("focus"));
            window.dispatchEvent(new Event("resize"));
            expect(cancelWeb).not.toHaveBeenCalled();
            expect(playerHide).not.toHaveBeenCalled();
            expect(playerPlace).not.toHaveBeenCalled();
            expect(hideWindow).not.toHaveBeenCalled();
            state = "visible";
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).not.toHaveBeenCalled();
            const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
            frame.getBoundingClientRect = () => ({
                x: 8, y: 12, left: 8, top: 12, width: 640, height: 360, right: 648, bottom: 372, toJSON() { return {}; },
            }) as DOMRect;
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(playerPlace).toHaveBeenCalledWith("https://www.youtube.com/embed/abc", 8 * ratio, 12 * ratio, 640 * ratio, 360 * ratio);
            expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        } finally {
            if (prior) Object.defineProperty(document, "visibilityState", prior);
            release();
        }
    });

    it("a fullscreen exit marked windowVisible false does not stop the note while the document is still visible", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        const frame = await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", [{ fullscreen: true, windowVisible: true }]); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
        playerPlace.mockClear();
        playerHide.mockClear();
        cancelWeb.mockClear();
        // Alt+F4 / minimize: the host reports the window going away on the
        // same fullscreen event. visibilityState has not flipped, and the
        // title-bar button was not clicked.
        expect(document.visibilityState).not.toBe("hidden");
        act(() => { runtime.emit("file-companion:webview-fullscreen", [{ fullscreen: false, windowVisible: false }]); });
        window.dispatchEvent(new Event("resize"));
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(playerHide).not.toHaveBeenCalled();
        expect(playerPlace).not.toHaveBeenCalled();
        expect(hideWindow).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        const prior = Object.getOwnPropertyDescriptor(Document.prototype, "visibilityState")
            ?? Object.getOwnPropertyDescriptor(document, "visibilityState");
        Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
        try {
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).not.toHaveBeenCalled();
            const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
            frame.getBoundingClientRect = () => ({
                x: 8, y: 12, left: 8, top: 12, width: 640, height: 360, right: 648, bottom: 372, toJSON() { return {}; },
            }) as DOMRect;
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(playerPlace).toHaveBeenCalledWith("https://www.youtube.com/embed/abc", 8 * ratio, 12 * ratio, 640 * ratio, 360 * ratio);
            expect(cancelWeb).not.toHaveBeenCalled();
            expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        } finally {
            if (prior) Object.defineProperty(document, "visibilityState", prior);
            release();
        }
    });

    it("arms the next player place when the window is shown with no web page open", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        choose.mockResolvedValue(["C:/notes/a.md"]);
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        pickLocalFile();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-close"));
        expect(hideWindow).toHaveBeenCalledTimes(1);
        const prior = Object.getOwnPropertyDescriptor(Document.prototype, "visibilityState")
            ?? Object.getOwnPropertyDescriptor(document, "visibilityState");
        let state: DocumentVisibilityState = "hidden";
        Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        try {
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            state = "visible";
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            playerPlace.mockClear();
            playerHide.mockClear();
            fireEvent.click(screen.getByTestId("file-companion-choose"));
            fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
            openWeb.mockResolvedValue({
                kind: "web",
                frameUrl: "https://www.youtube.com/embed/abc",
                sessionId: "web-abc",
            });
            fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
            const frame = await screen.findByTestId("file-companion-web-frame");
            // The slot is still empty. The armed reveal must wait for a real box.
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).not.toHaveBeenCalled();
            expect(playerHide).not.toHaveBeenCalled();
            const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
            frame.getBoundingClientRect = () => ({
                x: 8, y: 12, left: 8, top: 12, width: 640, height: 360, right: 648, bottom: 372, toJSON() { return {}; },
            }) as DOMRect;
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(playerPlace).toHaveBeenCalledWith("https://www.youtube.com/embed/abc", 8 * ratio, 12 * ratio, 640 * ratio, 360 * ratio);
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
            fireEvent.click(screen.getByTestId("file-companion-video-note"));
            await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false }); });
            expect(cancelWeb).toHaveBeenCalledTimes(1);
            expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        } finally {
            if (prior) Object.defineProperty(document, "visibilityState", prior);
            release();
        }
    });

    it("a host show places the player while the document never becomes hidden", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        const frame = await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
        playerPlace.mockClear();
        playerHide.mockClear();
        cancelWeb.mockClear();
        const prior = Object.getOwnPropertyDescriptor(Document.prototype, "visibilityState")
            ?? Object.getOwnPropertyDescriptor(document, "visibilityState");
        try {
            expect(document.visibilityState).not.toBe("hidden");
            act(() => { runtime.emit("file-companion:window-shown", [{ shown: false }]); });
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false }); });
            window.dispatchEvent(new Event("resize"));
            expect(cancelWeb).not.toHaveBeenCalled();
            expect(playerHide).not.toHaveBeenCalled();
            expect(playerPlace).not.toHaveBeenCalled();
            expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
            act(() => { runtime.emit("file-companion:window-shown", { shown: true }); });
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).not.toHaveBeenCalled();
            const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
            frame.getBoundingClientRect = () => ({
                x: 8, y: 12, left: 8, top: 12, width: 640, height: 360, right: 648, bottom: 372, toJSON() { return {}; },
            }) as DOMRect;
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(playerPlace).toHaveBeenCalledWith("https://www.youtube.com/embed/abc", 8 * ratio, 12 * ratio, 640 * ratio, 360 * ratio);
            playerPlace.mockClear();
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
            let state: DocumentVisibilityState = "hidden";
            Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(document.visibilityState).toBe("hidden");
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false, windowVisible: true }); });
            expect(cancelWeb).toHaveBeenCalledTimes(1);
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(playerHide).not.toHaveBeenCalled();
            expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        } finally {
            if (prior) Object.defineProperty(document, "visibilityState", prior);
            release();
        }
    });

    it("a visible fullscreen exit stops the note without placing an empty slot", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        const frame = await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
        playerPlace.mockClear();
        playerHide.mockClear();
        cancelWeb.mockClear();
        const prior = Object.getOwnPropertyDescriptor(Document.prototype, "visibilityState")
            ?? Object.getOwnPropertyDescriptor(document, "visibilityState");
        Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
        try {
            act(() => { runtime.emit("file-companion:window-shown", { shown: false }); });
            act(() => { document.dispatchEvent(new Event("visibilitychange")); });
            act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false, windowVisible: true }); });
            expect(cancelWeb).toHaveBeenCalledTimes(1);
            expect(playerPlace).not.toHaveBeenCalled();
            expect(playerHide).not.toHaveBeenCalled();
            expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
            const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
            frame.getBoundingClientRect = () => ({
                x: 8, y: 12, left: 8, top: 12, width: 640, height: 360, right: 648, bottom: 372, toJSON() { return {}; },
            }) as DOMRect;
            window.dispatchEvent(new Event("resize"));
            expect(playerPlace).toHaveBeenCalledTimes(1);
            expect(playerPlace).toHaveBeenCalledWith("https://www.youtube.com/embed/abc", 8 * ratio, 12 * ratio, 640 * ratio, 360 * ratio);
            expect(playerHide).not.toHaveBeenCalled();
            expect(cancelWeb).toHaveBeenCalledTimes(1);
        } finally {
            if (prior) Object.defineProperty(document, "visibilityState", prior);
            release();
        }
    });

    it("a hide-owned fullscreen exit after the window is shown keeps the note and the slot", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        const frame = await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledTimes(1));
        playerPlace.mockClear();
        playerHide.mockClear();
        cancelWeb.mockClear();
        act(() => { runtime.emit("file-companion:window-shown", { shown: false }); });
        act(() => { runtime.emit("file-companion:window-shown", { shown: true }); });
        expect(playerPlace).not.toHaveBeenCalled();
        // The callback for the hide's fullscreen drop runs after the show.
        // The window is on screen. Treating that as a hide would drop the
        // reveal, and the slot would never be placed.
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false, windowVisible: true, hideExit: true }); });
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(playerHide).not.toHaveBeenCalled();
        expect(playerPlace).not.toHaveBeenCalled();
        const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
        frame.getBoundingClientRect = () => ({
            x: 8, y: 12, left: 8, top: 12, width: 640, height: 360, right: 648, bottom: 372, toJSON() { return {}; },
        }) as DOMRect;
        window.dispatchEvent(new Event("resize"));
        expect(playerPlace).toHaveBeenCalledTimes(1);
        expect(playerPlace).toHaveBeenCalledWith("https://www.youtube.com/embed/abc", 8 * ratio, 12 * ratio, 640 * ratio, 360 * ratio);
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止");
        release();
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
        expect(choose.textContent).toBe("打开");
        expect(choose.closest("[data-testid='file-companion-titlebar']")).toBe(screen.getByTestId("file-companion-titlebar"));
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
        // 导出 PDF sits immediately to the right of 论文解读, then 导入知识库.
        expect(screen.getByTestId("file-companion-paper").nextElementSibling).toBe(screen.getByTestId("file-companion-export-chat"));
        expect(screen.getByTestId("file-companion-export-chat").nextElementSibling).toBe(screen.getByTestId("file-companion-import"));
        expect(screen.getByTestId("file-companion-import").textContent).toContain("导入知识库");
        expect(screen.getByTestId("file-companion-upload").textContent).toContain("上传到云盘");
        for (const id of ["file-companion-paper", "file-companion-export-chat", "file-companion-import", "file-companion-upload"]) {
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

    it("sends from another file while a reply is running", async () => {
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
        expect(screen.getByTestId("file-companion-send").textContent).toBe("处理中");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "问 B" } });
        const sendButton = screen.getByTestId("file-companion-send");
        expect(sendButton.getAttribute("aria-busy")).not.toBe("true");
        expect(sendButton.textContent).toBe("发送");
        expect((screen.getByTestId("file-companion-paper") as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(sendButton);
        await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
        const second = send.mock.calls[1][0] as { path?: string; text?: string };
        expect(second.path).toBe("C:/notes/b.md");
        expect(second.text).toBe("问 B");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1") || "").toContain("问 B");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect(screen.getByTestId("file-companion-send").textContent).toBe("处理中");
        expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).toBe("true");
        await act(async () => { release(); });
    });

    it("keeps 正在解读 on each file and gives concurrent turns different request ids", async () => {
        const now = Date.now();
        const clock = vi.spyOn(Date, "now").mockReturnValue(now);
        const pending: Array<() => void> = [];
        send.mockImplementation(() => new Promise<void>((resolve) => { pending.push(resolve); }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-b.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        fireEvent.click(screen.getByTestId("file-companion-paper"));
        await waitFor(() => expect(screen.getByTestId("file-companion-paper").textContent).toBe("正在解读…"));
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect(screen.getByTestId("file-companion-paper").textContent).toBe("论文解读");
        expect((screen.getByTestId("file-companion-paper") as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(screen.getByTestId("file-companion-paper"));
        await waitFor(() => expect(screen.getByTestId("file-companion-paper").textContent).toBe("正在解读…"));
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect(screen.getByTestId("file-companion-paper").textContent).toBe("正在解读…");
        expect((screen.getByTestId("file-companion-paper") as HTMLButtonElement).disabled).toBe(true);
        const ids = send.mock.calls.map((call) => String((call[0] as { request_id?: string }).request_id));
        expect(ids).toHaveLength(2);
        expect(new Set(ids).size).toBe(2);
        clock.mockRestore();
        await act(async () => { pending.forEach((release) => release()); });
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
            expect(screen.getByTestId("file-companion-send").textContent).toBe("处理中");
        });
        await act(async () => { release(); await Promise.resolve(); });
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在整理上下文并准备模型请求", request_id: "r1" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).toBe("true");
            expect(screen.getByTestId("file-companion-send").textContent).toBe("处理中");
        });
        runtime.emit("ai-assistant-response", JSON.stringify({ session_key: "file-companion:a.md", text: "好的", request_id: "r1" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-send").getAttribute("aria-busy")).not.toBe("true");
            expect(screen.getByTestId("file-companion-send").textContent).toBe("发送");
        });
    });

    it("shows the drawing an answer refers to, and zoom it on click", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/figs.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "总结一下" },
                {
                    role: "assistant",
                    text: "整体如图1所示。",
                    figures: [{ label: "图1", caption: "整体架构", path: "C:/notes/img/archured.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("整体如图1所示。");
        await waitFor(() => expect(figureDataURL).toHaveBeenCalledWith("C:/notes/img/archured.png"));
        await waitFor(() => expect(screen.getByTestId("file-companion-figure-image")).toBeTruthy());
        expect(screen.getByText(/原文配图/)).toBeTruthy();
        expect((screen.getByTestId("file-companion-figure-image") as HTMLImageElement).getAttribute("src")).toBe("data:image/png;base64,C:/notes/img/archured.png");
        fireEvent.click(screen.getByTestId("file-companion-figure-image"));
        const zoom = screen.getByTestId("file-companion-figure-zoom");
        expect(zoom.getAttribute("role")).toBe("dialog");
        expect(zoom.getAttribute("aria-modal")).toBe("true");
        expect(zoom.getAttribute("aria-label")).toBe("图1 · 整体架构");
        fireEvent.click(screen.getByTestId("file-companion-figure-zoom"));
        expect(screen.queryByTestId("file-companion-figure-zoom")).toBeNull();
    });

    it("shows figures that arrive with a streamed reply", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/streamed.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        fireEvent.change(await screen.findByTestId("file-companion-input"), { target: { value: "这篇讲了什么" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:streamed.md",
                text: "方法如图2所示。",
                request_id: "figs",
                file_companion_figures: [{ index: 2, label: "图2", caption: "训练流程", path: "C:/notes/img/trained.png" }],
            }));
        });
        await screen.findByText("方法如图2所示。");
        await waitFor(() => expect(screen.getByTestId("file-companion-figure-image")).toBeTruthy());
        expect(figureDataURL).toHaveBeenCalledWith("C:/notes/img/trained.png");
    });

    it("shows no drawings when the reply names none", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/plain.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "assistant", text: "这段文字没有问题。" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这段文字没有问题。");
        expect(screen.queryByTestId("file-companion-figure-image")).toBeNull();
        expect(figureDataURL).not.toHaveBeenCalled();
    });

    it("says so when a drawing cannot be fetched", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/broken.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "总结一下" },
                { role: "assistant", text: "整体如图1所示。", figures: [{ label: "图1", caption: "整体架构", path: "C:/notes/img/gone.png" }] },
            ],
        }));
        figureDataURL.mockRejectedValue(new Error("gone"));
        installRuntime();
        renderWindow();
        await screen.findByText("整体如图1所示。");
        await waitFor(() => expect(screen.getByText("无法取图")).toBeTruthy());
        expect(screen.queryByTestId("file-companion-figure-image")).toBeNull();
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

    it("opens a document named open-file and reports a missing one", async () => {
        boot.mockResolvedValue({ paths: ["C:/papers/2510.02373v1.pdf"] });
        open.mockImplementation(async (path: string) => {
            if (path === "C:/papers/open-file" || path.endsWith("missing.md")) return doc(path, { error: "file not found", canExport: false });
            return doc(path);
        });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-2510.02373v1.pdf");
        expect(screen.queryByTestId("file-companion-tab-open-file")).toBeNull();
        expect(open).toHaveBeenCalledTimes(1);
        expect(open).toHaveBeenCalledWith("C:/papers/2510.02373v1.pdf");

        runtime.emit("file-companion:open", ["C:/notes/open-file", "C:/notes/missing.md", "C:/papers/open-file"]);
        await screen.findByTestId("file-companion-tab-missing.md");
        await waitFor(() => {
            expect(screen.getAllByTestId("file-companion-tab-open-file").map((tab) => tab.getAttribute("title"))).toEqual([
                "C:/notes/open-file",
                "C:/papers/open-file",
            ]);
        });
        expect(screen.getByTestId("file-companion-file-error").textContent).toContain("找不到文件");
        fireEvent.click(screen.getAllByTestId("file-companion-tab-open-file")[0]);
        expect(screen.queryByTestId("file-companion-file-error")).toBeNull();
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
        expect(screen.queryByRole("progressbar")).toBeNull();
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

    it("shows the note stages and moves the bar when speech segments arrive", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在记录声音", request_id: "r1" }));
        const panel = await screen.findByTestId("file-companion-progress");
        expect(panel.textContent).toContain("正在记录声音");
        expect(panel.textContent).toContain("声音");
        expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("00:00");
        const listening = Number(screen.getByRole("progressbar").getAttribute("aria-valuenow"));
        expect(listening).toBeGreaterThan(10);
        runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在识别语音（第 2/4 段）", request_id: "r1" }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-progress").textContent).toContain("第 2/4 段");
            expect(Number(screen.getByRole("progressbar").getAttribute("aria-valuenow"))).toBeGreaterThan(listening);
        });
        expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("00:00");
    });

    it("keeps each document's note clock when the visible tab changes", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-b.md");
        const now = { t: Date.now() };
        const clock = vi.spyOn(Date, "now").mockImplementation(() => now.t);
        try {
            act(() => {
                runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:a.md", text: "正在记录声音", request_id: "r1" }));
            });
            expect(screen.getByTestId("file-companion-progress").textContent).toContain("正在记录声音");
            expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("00:00");
            now.t += 5 * 60 * 1000;
            fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
            expect(screen.queryByTestId("file-companion-progress")).toBeNull();
            fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
            expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("05:00");
            act(() => {
                runtime.emit("ai-assistant-progress", JSON.stringify({ session_key: "file-companion:b.md", text: "正在检查文件", request_id: "r2" }));
            });
            fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
            expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("00:00");
            expect(screen.getByTestId("file-companion-progress").textContent).toContain("正在检查文件");
            fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
            expect(screen.getByTestId("file-companion-note-elapsed").textContent).toBe("05:00");
            expect(screen.getByTestId("file-companion-progress").textContent).toContain("正在记录声音");
        } finally {
            clock.mockRestore();
        }
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
        // Leaving the tail is a gesture, not a distance reading: the upward
        // wheel is what the follow logic latches on.
        fireEvent.wheel(log, { deltaY: -40 });
        fireEvent.scroll(log);
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "later", request_id: "r1" }));
        await waitFor(() => {
            expect(log.textContent).toContain("later");
        });
        expect(log.scrollTop).toBe(0);
    });

    it("keeps following a long answer when a scroll arrives without a gesture", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1" }));
        const runtime = installRuntime();
        renderWindow();
        const log = await screen.findByTestId("file-companion-chat-log");
        Object.defineProperty(log, "scrollHeight", { configurable: true, value: 400 });
        Object.defineProperty(log, "clientHeight", { configurable: true, value: 80 });
        log.scrollTop = 0;
        // No wheel/touch/pointer gesture: this scroll reports the position
        // after content grew, not the reader leaving the tail.
        fireEvent.scroll(log);
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "later", request_id: "r1" }));
        await waitFor(() => {
            expect(log.textContent).toContain("later");
        });
        await waitFor(() => {
            expect(log.scrollTop).toBe(400);
        });
    });

    it("re-pins when only the thinking trail grows", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1" }));
        const runtime = installRuntime();
        renderWindow();
        const log = await screen.findByTestId("file-companion-chat-log");
        Object.defineProperty(log, "scrollHeight", { configurable: true, value: 400 });
        Object.defineProperty(log, "clientHeight", { configurable: true, value: 80 });
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "first", request_id: "r1" }));
        await waitFor(() => {
            expect(log.textContent).toContain("first");
        });
        log.scrollTop = 0;
        // U+0001 marks the reasoning lane, so the answer text stays put while
        // the thought grows the bubble.
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "\u0001still thinking", request_id: "r1" }));
        await waitFor(() => {
            expect(log.scrollTop).toBe(400);
        });
    });

    it("opens the newly selected file's chat at its newest message", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "v1" }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        await screen.findByTestId("file-companion-tab-b.md");
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        const log = await screen.findByTestId("file-companion-chat-log");
        Object.defineProperty(log, "scrollHeight", { configurable: true, value: 400 });
        Object.defineProperty(log, "clientHeight", { configurable: true, value: 80 });
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "answer for a", request_id: "r1" }));
        await waitFor(() => {
            expect(log.textContent).toContain("answer for a");
        });
        log.scrollTop = 0;
        fireEvent.wheel(log, { deltaY: -40 });
        fireEvent.scroll(log);
        // One scroller serves every file, so the switch must not inherit the
        // previous file's scrolled-up position.
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        await waitFor(() => {
            expect(log.scrollTop).toBe(400);
        });
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

    it("asks each new document on its own, without waiting for the other reply", async () => {
        const pending: Array<() => void> = [];
        send.mockImplementation(() => new Promise<void>((resolve) => { pending.push(resolve); }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        installRuntime();
        renderWindow({ askOnOpen: true });
        await screen.findByTestId("file-companion-tab-a.md");
        await screen.findByTestId("file-companion-tab-b.md");
        await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
        const paths = send.mock.calls.map((call) => (call[0] as { path?: string }).path);
        expect(paths).toEqual(["C:/notes/a.md", "C:/notes/b.md"]);
        for (const call of send.mock.calls) {
            const sent = call[0] as Record<string, unknown>;
            expect(sent.text).toBe("这篇文档是什么？");
            expect(sent.selection).toBeUndefined();
        }
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        const input = screen.getByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "我的问题" } });
        expect(input.value).toBe("我的问题");
        expect(localStorage.getItem("maclaw.fileCompanionInputHistory.v1")).toBeNull();
        expect(screen.getByTestId("file-companion-send").textContent).toBe("处理中");
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("这篇文档是什么？");
        expect(screen.getByTestId("file-companion-send").textContent).toBe("处理中");
        expect(screen.queryByTestId("file-companion-chat-empty")).toBeNull();
        await act(async () => { pending.forEach((release) => release()); await Promise.resolve(); });
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        expect((screen.getByTestId("file-companion-input") as HTMLTextAreaElement).value).toBe("我的问题");
    });

    it("copies an answer as Markdown, unchanged by the bubble rendering", async () => {
        const answer = "## 小结\n\n**重点** 在第二段。\n\n- 第一点\n- 第二点\n";
        exportPdf.mockResolvedValue("");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "user", text: "总结一下" }, { role: "assistant", text: answer }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("小结");

        const copy = screen.getByTestId("file-companion-copy-restored:1");
        expect(copy.getAttribute("title")).toBe("复制为 Markdown");
        fireEvent.click(copy);
        // The bubble shows the answer trimmed, and the copy carries what the
        // bubble shows, Markdown markers and all.
        await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith(answer.trim()));
        await waitFor(() => expect(copy.getAttribute("data-state")).toBe("ok"));
        expect(copy.getAttribute("title")).toBe("已复制");
        // The own question carries no controls: only answers are copied out.
        expect(screen.queryAllByTestId(/^file-companion-copy-/)).toHaveLength(1);
        expect(screen.queryAllByTestId(/^file-companion-export-pdf-/)).toHaveLength(1);
    });

    it("exports an answer to the PDF the reader names", async () => {
        const answer = "# 摘要\n\n这是正文。\n";
        exportPdf.mockResolvedValue("C:/out/a-回答.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "user", text: "总结一下" }, { role: "assistant", text: answer }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("摘要");

        const pdf = screen.getByTestId("file-companion-export-pdf-restored:1");
        expect(pdf.getAttribute("title")).toBe("导出为 PDF");
        fireEvent.click(pdf);
        await waitFor(() => expect(exportPdf).toHaveBeenCalledWith("C:/notes/a.md", answer.trim()));
        await screen.findByText("已导出 PDF");
        expect(pdf.getAttribute("data-state")).toBe("ok");
    });

    // The single-answer export reads the same Markdown as the transcript one,
    // so a diagram in it has to be drawn here too — otherwise this control
    // quietly produces a page of Mermaid source.
    it("exports one answer's diagram as the diagram it draws", async () => {
        exportPdf.mockResolvedValue("C:/out/a-回答.pdf");
        renderMermaidFenceToPNGDataURL.mockResolvedValue("data:image/png;base64,DIAGRAM");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "画个图" },
                { role: "assistant", text: "如下：\n\n```mermaid\nflowchart LR\n  A --> B\n```\n" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/如下/);

        fireEvent.click(screen.getByTestId("file-companion-export-pdf-restored:1"));
        await waitFor(() => expect(exportPdf).toHaveBeenCalledWith("C:/notes/a.md", "如下：\n\n![](data:image/png;base64,DIAGRAM)"));
    });

    // The window shows a reply's figures beneath it, so exporting that reply
    // has to carry them. Text alone would be a different document from the one
    // on screen.
    it("exports one answer with the figures shown beneath it", async () => {
        exportPdf.mockResolvedValue("C:/out/a-回答.pdf");
        figureDataURL.mockResolvedValue("data:image/png;base64,FIG");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "见下图。",
                    figures: [{ label: "图 1", caption: "结果", path: "C:/papers/fig.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("见下图。");

        fireEvent.click(screen.getByTestId("file-companion-export-pdf-restored:0"));
        await waitFor(() => expect(exportPdf).toHaveBeenCalledWith("C:/notes/a.md", "见下图。\n\n![图 1 结果](data:image/png;base64,FIG)"));
    });

    // And when a figure cannot be read, the reader is told rather than handed a
    // document that looks complete.
    it("tells the reader when one answer's figure could not be included", async () => {
        exportPdf.mockResolvedValue("C:/out/a-回答.pdf");
        figureDataURL.mockRejectedValue(new Error("that image is not part of the open file"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "见下图。",
                    figures: [{ label: "图 1", caption: "结果", path: "C:/papers/fig.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("见下图。");

        fireEvent.click(screen.getByTestId("file-companion-export-pdf-restored:0"));
        await waitFor(() => expect(exportPdf).toHaveBeenCalledWith("C:/notes/a.md", "见下图。"));
        await screen.findByText("已导出 PDF，但 1 张配图未能读取，未包含在内");
    });

    it("reports a failed export instead of claiming success", async () => {
        exportPdf.mockRejectedValue(new Error("没有可用的中文字体，无法生成 PDF"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        const pdf = screen.getByTestId("file-companion-export-pdf-restored:0");
        fireEvent.click(pdf);
        await screen.findByText("没有可用的中文字体，无法生成 PDF");
        await waitFor(() => expect(pdf.getAttribute("data-state")).toBe("err"));
        expect(pdf.getAttribute("title")).toBe("导出失败");
    });

    it("stays quiet when the export dialog is cancelled", async () => {
        exportPdf.mockResolvedValue("");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        const pdf = screen.getByTestId("file-companion-export-pdf-restored:0");
        fireEvent.click(pdf);
        await waitFor(() => expect(exportPdf).toHaveBeenCalledTimes(1));
        expect(screen.queryByText("已导出 PDF")).toBeNull();
        expect(pdf.getAttribute("data-state")).toBe("idle");
    });

    // The bytes travel with the text. A path alone would point at a file the
    // reading side cannot open, which is exactly what "export the picture"
    // must not become.
    // The bytes travel with the text, and so does the label. A picture with no
    // caption leaves the reader matching "图 8.1" in the answer to a drawing by
    // guesswork.
    it("carries a reply's figures into the transcript with their captions", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        figureDataURL.mockResolvedValue("data:image/png;base64,AAAA");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "展示图 8.1" },
                {
                    role: "assistant",
                    text: "图中给出结果。",
                    figures: [{ label: "图 8.1", caption: "结果", path: "C:/papers/fig.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("图中给出结果。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "展示图 8.1" },
            { role: "assistant", content: "图中给出结果。\n\n![图 8.1 结果](data:image/png;base64,AAAA)" },
        ]));
        expect(figureDataURL).toHaveBeenCalledWith("C:/papers/fig.png");
    });

    // A caption is not allowed to end the alt text early: Markdown stops it at
    // the first bracket or parenthesis, which would silently truncate the label.
    it("keeps a caption whose text contains markdown punctuation", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        figureDataURL.mockResolvedValue("data:image/png;base64,AAAA");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "见下图。",
                    figures: [{ label: "图 2", caption: "损失曲线 (baseline)", path: "C:/papers/fig.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("见下图。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "见下图。\n\n![图 2 损失曲线 baseline](data:image/png;base64,AAAA)" },
        ]));
    });

    // A figure that cannot be read must be reported. Dropping it silently
    // produces a document that looks complete and is not.
    it("tells the reader when a figure could not be included", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        figureDataURL.mockRejectedValue(new Error("that image is not part of the open file"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "图中给出结果。",
                    figures: [{ label: "图 8.1", caption: "结果", path: "C:/papers/fig.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("图中给出结果。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        // The text still exports; the missing picture is named.
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "图中给出结果。" },
        ]));
        await screen.findByText("已导出 PDF，但 1 张配图未能读取，未包含在内");
    });

    // The window draws Mermaid diagrams; the document layer cannot, having no
    // JavaScript. A fence that reached the PDF as source is the difference
    // between a diagram and a page of arrows.
    it("exports a mermaid fence as the diagram it draws", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        renderMermaidFenceToPNGDataURL.mockResolvedValue("data:image/png;base64,DIAGRAM");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "画个流程图" },
                {
                    role: "assistant",
                    text: "三大抓手：\n\n```mermaid\nflowchart LR\n  A[压缩] --> B[架构层]\n```\n\n如上。",
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/三大抓手/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "画个流程图" },
            { role: "assistant", content: "三大抓手：\n\n![](data:image/png;base64,DIAGRAM)\n\n如上。" },
        ]));
        // The fence body is what gets drawn, without the fence markers.
        expect(renderMermaidFenceToPNGDataURL).toHaveBeenCalledWith("flowchart LR\n  A[压缩] --> B[架构层]", expect.anything());
    });

    // A diagram that will not draw keeps its source: the reader still gets what
    // the screen would have shown, and is told the picture is missing.
    it("keeps the source when a diagram will not draw, and says so", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        renderMermaidFenceToPNGDataURL.mockResolvedValue(null);
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "```mermaid\nflowchart LR\n  A --> B\n```\n",
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/flowchart LR/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "```mermaid\nflowchart LR\n  A --> B\n```" },
        ]));
        await screen.findByText("已导出 PDF，但 1 张流程图未能读取，未包含在内");
    });

    // Only Mermaid fences are drawn. A code block the reader meant as code has
    // to survive verbatim.
    it("leaves other code fences alone", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "示例：\n\n```python\nprint(1)\n```\n",
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/print\(1\)/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "示例：\n\n```python\nprint(1)\n```" },
        ]));
        expect(renderMermaidFenceToPNGDataURL).not.toHaveBeenCalled();
    });

    // Figures are fetched together, so their order in the document has to come
    // from the list, not from whichever request finished first. A scrambled
    // order would put 图 1's caption under 图 2.
    it("keeps figure order when several are fetched at once", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        // The first request is the slowest, so completion order is the reverse
        // of document order.
        figureDataURL.mockImplementation(async (path: string) => {
            if (path.endsWith("one.png")) {
                await new Promise((resolve) => setTimeout(resolve, 40));
            }
            return `data:image/png;base64,${path}`;
        });
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "两张图。",
                    figures: [
                        { label: "图 1", caption: "甲", path: "C:/papers/one.png" },
                        { label: "图 2", caption: "乙", path: "C:/papers/two.png" },
                        { label: "图 3", caption: "丙", path: "C:/papers/three.png" },
                    ],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("两张图。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            {
                role: "assistant",
                content: [
                    "两张图。",
                    "",
                    "![图 1 甲](data:image/png;base64,C:/papers/one.png)",
                    "",
                    "![图 2 乙](data:image/png;base64,C:/papers/two.png)",
                    "",
                    "![图 3 丙](data:image/png;base64,C:/papers/three.png)",
                ].join("\n"),
            },
        ]));
    });

    // Fences come in more shapes than ```mermaid. Each of these is a form the
    // renderer accepts, so the export has to accept it too or the reader gets
    // source for a diagram that was drawn on screen.
    it("draws diagrams written with tilde fences or indentation", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        renderMermaidFenceToPNGDataURL.mockResolvedValue("data:image/png;base64,DIAGRAM");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "assistant", text: "~~~mermaid\nflowchart LR\n  A --> B\n~~~\n" },
                { role: "assistant", text: "  ```mermaid\nflowchart LR\n  C --> D\n  ```\n" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/flowchart LR/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "![](data:image/png;base64,DIAGRAM)" },
            { role: "assistant", content: "![](data:image/png;base64,DIAGRAM)" },
        ]));
    });

    // A fence still being written is not a diagram yet. Leaving the source in
    // place keeps the text the reader can see; counting it as missing would
    // warn about a picture that was never promised.
    it("leaves an unclosed mermaid fence as source, without warning", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "开始画：\n\n```mermaid\nflowchart LR\n  A --> B\n" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/开始画/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "开始画：\n\n```mermaid\nflowchart LR\n  A --> B" },
        ]));
        expect(renderMermaidFenceToPNGDataURL).not.toHaveBeenCalled();
        expect(screen.queryByText(/未能读取/)).toBeNull();
    });

    // A longer fence may carry extra text after the language, and the diagram
    // is everything between the markers.
    it("reads a fence whose language line carries extra text", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        renderMermaidFenceToPNGDataURL.mockResolvedValue("data:image/png;base64,DIAGRAM");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "````mermaid title=x\nflowchart LR\n  A --> B\n````\n" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/flowchart LR/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(renderMermaidFenceToPNGDataURL).toHaveBeenCalledTimes(1));
        const [code] = renderMermaidFenceToPNGDataURL.mock.calls[0] as [string];
        expect(code).toBe("flowchart LR\n  A --> B");
    });

    // A closing run shorter than the opening one does not close the fence —
    // the same rule the on-screen renderer applies, so a diagram that keeps
    // rendering as code on screen is not silently drawn in the PDF.
    it("keeps a fence open when the closing run is shorter", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "````mermaid\nflowchart LR\n  A --> B\n```\n还在围栏里\n" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/还在围栏里/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        expect(renderMermaidFenceToPNGDataURL).not.toHaveBeenCalled();
        const [, turns] = exportChatPdf.mock.calls[0] as [string, { content: string }[]];
        expect(turns[0].content).toContain("````mermaid");
    });

    // A fence that is not Mermaid must survive verbatim, including when it
    // contains a line that looks like another fence.
    it("leaves a non-mermaid fence untouched even when it holds fence-like lines", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "说明：\n\n```text\n示例 ```mermaid 只是文字\n```\n" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/说明/);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "说明：\n\n```text\n示例 ```mermaid 只是文字\n```" },
        ]));
        expect(renderMermaidFenceToPNGDataURL).not.toHaveBeenCalled();
    });

    // A long session must export whole. This pins the turn list against any
    // future change that stops walking it early (a `return` where a `continue`
    // belongs behaves identically while the streaming reply is last, so the
    // difference only shows up as missing history).
    it("keeps every earlier turn when the newest reply is still writing", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "第一个问题" },
                { role: "assistant", text: "第一个回答。" },
                { role: "user", text: "第二个问题" },
                { role: "assistant", text: "第二个回答。" },
                { role: "user", text: "第三个问题" },
                { role: "assistant", text: "半句正在写的答案" },
            ],
        }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByText("第一个回答。");
        // The window is inside a turn: nothing has settled.
        let release: () => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "追加提问" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        act(() => {
            runtime.emit("ai-assistant-token", JSON.stringify({
                session_key: "file-companion:a.md",
                text: "刚到的半句",
                request_id: "req-live",
            }));
        });
        await screen.findByText("刚到的半句");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "第一个问题" },
            { role: "assistant", content: "第一个回答。" },
            { role: "user", content: "第二个问题" },
            { role: "assistant", content: "第二个回答。" },
            { role: "user", content: "第三个问题" },
            { role: "assistant", content: "半句正在写的答案" },
            { role: "user", content: "追加提问" },
        ]));
        await act(async () => { release(); await Promise.resolve(); });
    });

    it("exports the conversation even when a figure cannot be fetched", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        figureDataURL.mockRejectedValue(new Error("图片不可用"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "解释如下。",
                    figures: [{ label: "图", caption: "", path: "C:/papers/gone.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("解释如下。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        // One unreachable picture must not cost the reader the whole answer.
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "解释如下。" },
        ]));
    });

    // An export in flight has to look like one. The flight set already stops a
    // second request reaching the backend; without the button also going
    // disabled, the reader sees a control that answers nothing, which reads as
    // broken rather than busy.
    it("shows an in-flight export instead of a button that answers nothing", async () => {
        exportChatPdf.mockImplementation(() => new Promise<string>((resolve) => {
            setTimeout(() => resolve("C:/out/a-伴读对话.pdf"), 150);
        }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "展示图 8.1" },
                {
                    role: "assistant",
                    text: "图中给出结果。",
                    figures: [{ label: "图 8.1", caption: "结果", path: "C:/papers/fig.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("图中给出结果。");

        const before = screen.getByTestId("file-companion-export-chat");
        expect(before.hasAttribute("disabled")).toBe(false);
        fireEvent.click(before);
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat").getAttribute("aria-busy")).toBe("true"));

        const busy = screen.getByTestId("file-companion-export-chat");
        expect(busy.hasAttribute("disabled")).toBe(true);
        expect(busy.textContent).toBe("正在导出…");
        // A second click must not reach the backend either way.
        fireEvent.click(busy);
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
    });

    // Busy is per file, so opening another document while one export runs must
    // offer that file's own export — and the export already under way has to
    // stay bound to the document it started from, not the one now on screen.
    //
    // What this pins is the observable outcome (the right document's path and
    // content reach the backend), not the mechanism: replacing activeTab.path
    // with an equivalent lookup leaves it passing, so treat it as a guard
    // against the wiring changing, not against every way it could.
    it("keeps a running export bound to its own file when tabs change", async () => {
        exportChatPdf.mockImplementation(() => new Promise<string>((resolve) => {
            setTimeout(() => resolve("C:/out/a-伴读对话.pdf"), 150);
        }));
        // Distinct content per file, so an export that picked up the wrong
        // document is caught in what it wrote, not only in which path it sent.
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "user", text: path === "C:/notes/a.md" ? "AAA 的问题" : "BBB 的问题" }],
        }));
        installRuntime();
        renderWindow();
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat")).toBeTruthy());

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat").getAttribute("aria-busy")).toBe("true"));
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        // The other file is free to export; its button is not the busy one.
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat").getAttribute("aria-busy")).toBe("false"));

        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        const [sentPath, sentTurns] = exportChatPdf.mock.calls[0] as [string, { role: string; content: string }[]];
        expect(sentPath).toBe("C:/notes/a.md");
        expect(sentTurns[0].content).toBe("AAA 的问题");
    });

    it("exports the whole conversation, questions included", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "这段讲了什么？" },
                { role: "assistant", text: "## 要点\n\n第一点最重要。" },
                { role: "user", text: "第二点呢？" },
                { role: "assistant", text: "第二点讲的是边界。" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("要点");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        // Both sides travel together: an answer alone cannot be read on its own.
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "这段讲了什么？" },
            { role: "assistant", content: "## 要点\n\n第一点最重要。" },
            { role: "user", content: "第二点呢？" },
            { role: "assistant", content: "第二点讲的是边界。" },
        ]));
        await screen.findByText("已导出 PDF");
    });

    // A token that arrives after the request settles is a finished answer as
    // far as the window is concerned — its copy control is already showing.
    // The half-written case is the request that has not settled yet, which is
    // what this exercises.
    it("leaves a still-writing answer out of the transcript", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { messages: [] }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-chat-empty");
        // Held open rather than resolved, so the window stays inside the turn.
        let release: () => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "第二点呢？" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        act(() => {
            runtime.emit("ai-assistant-token", JSON.stringify({
                session_key: "file-companion:a.md",
                text: "半句答案",
                request_id: "req-live",
            }));
        });
        await screen.findByText("半句答案");
        // The window agrees this answer is unfinished: it offers no controls.
        expect(screen.queryAllByTestId(/^file-companion-copy-/)).toHaveLength(0);

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "第二点呢？" },
        ]));
        await act(async () => { release(); await Promise.resolve(); });
    });

    it("keeps the export button idle over an empty conversation", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { messages: [] }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-chat-empty");

        const button = screen.getByTestId("file-companion-export-chat");
        expect(button.hasAttribute("disabled")).toBe(true);
        fireEvent.click(button);
        expect(exportChatPdf).not.toHaveBeenCalled();
    });

    it("reports a failed conversation export instead of claiming success", async () => {
        exportChatPdf.mockRejectedValue(new Error("磁盘已满"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await screen.findByText("磁盘已满");
        expect(screen.queryByText("已导出 PDF")).toBeNull();
        // The button must become usable again, or one full disk ends exporting.
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat").textContent).toBe("导出 PDF"));
    });

    it("stays quiet when the conversation export dialog is cancelled", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        expect(screen.queryByText("已导出 PDF")).toBeNull();
    });

    it("offers no answer controls while the reply is still streaming", async () => {
        let release: () => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        fireEvent.change(await screen.findByTestId("file-companion-input"), { target: { value: "总结一下" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        act(() => {
            runtime.emit("ai-assistant-token", JSON.stringify({
                session_key: "file-companion:a.md",
                text: "正在整理答案。",
                request_id: "copy-live",
            }));
        });
        await screen.findByText("正在整理答案。");
        // A streaming answer is not finished Markdown yet, so it offers neither
        // control; they appear once the reply settles.
        expect(screen.queryAllByTestId(/^file-companion-copy-/)).toHaveLength(0);
        expect(screen.queryAllByTestId(/^file-companion-export-pdf-/)).toHaveLength(0);
        await act(async () => { release(); await Promise.resolve(); });
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:a.md",
                text: "整理好了。",
                request_id: "copy-live",
            }));
        });
        await waitFor(() => expect(screen.queryAllByTestId(/^file-companion-copy-/)).toHaveLength(1));
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

    it("seeks a local preview from stamps left after the English recap is peeled", async () => {
        const proto = HTMLMediaElement.prototype;
        const time = new WeakMap<HTMLMediaElement, number>();
        const ready = new WeakMap<HTMLMediaElement, number>();
        const length = new WeakMap<HTMLMediaElement, number>();
        const prior = {
            currentTime: Object.getOwnPropertyDescriptor(proto, "currentTime"),
            readyState: Object.getOwnPropertyDescriptor(proto, "readyState"),
            duration: Object.getOwnPropertyDescriptor(proto, "duration"),
        };
        const define = (key: "currentTime" | "readyState" | "duration", map: WeakMap<HTMLMediaElement, number>, fallback: number) => {
            Object.defineProperty(proto, key, {
                configurable: true,
                get() { return map.has(this) ? map.get(this)! : fallback; },
                set(value: number) { map.set(this, value); },
            });
        };
        define("currentTime", time, 0);
        define("readyState", ready, 0);
        define("duration", length, Number.NaN);
        const restore = () => {
            (["currentTime", "readyState", "duration"] as const).forEach((key) => {
                const desc = prior[key];
                if (desc) Object.defineProperty(proto, key, desc);
                else delete (proto as unknown as Record<string, unknown>)[key];
            });
        };
        try {
            const plan = [
                "- User asks: \"这篇文档是什么？\" (What is this document?)",
                "- 12:05 only in the plan",
                "- System Prompt Instructions:",
                "  - Role: AI personal assistant.",
                "- The file extension is .mp4, which is a video file, not a document.",
                "",
                "这是一个名为 clip.mp4 的视频文件。",
                "12:05 讲了板书",
                "1:02:03 讲了下一节",
                "12:99 不是时间",
                "99:99 也不是",
            ].join("\n");
            boot.mockResolvedValue({ paths: ["C:/media/clip.mp4"] });
            open.mockImplementation(async (path: string) => doc(path));
            const runtime = installRuntime();
            renderWindow();
            await screen.findByTestId("file-companion-video-note");
            expect(screen.queryByTestId("file-companion-paper")).toBeNull();
            expect((screen.getByTestId("file-companion-export-chat") as HTMLButtonElement).disabled).toBe(true);
            const video = await screen.findByTestId("media-preview-panel").then(() => document.querySelector("video") as HTMLMediaElement);
            expect(video).toBeTruthy();
            Object.defineProperty(video, 'readyState', { value: 1, configurable: true });
            Object.defineProperty(video, 'duration', { value: 10000, configurable: true });
            act(() => {
                runtime.emit("ai-assistant-response", JSON.stringify({
                    session_key: "file-companion:clip.mp4",
                    text: plan,
                    request_id: "seek-1",
                }));
            });
            await screen.findByText("这是一个名为 clip.mp4 的视频文件。");
            const stamps = screen.getAllByTestId("file-companion-seek");
            expect(stamps.map((node) => node.textContent)).toEqual(["12:05", "1:02:03"]);
            expect(stamps.map((node) => node.getAttribute("data-seconds"))).toEqual(["725", "3723"]);
            expect(screen.getByText("12:05 only in the plan")).toBeTruthy();
            expect(stamps.some((node) => (node.parentElement?.textContent || "").includes("only in the plan"))).toBe(false);
            fireEvent.click(stamps[0]);
            expect(video.currentTime).toBe(725);
            fireEvent.click(stamps[1]);
            expect(video.currentTime).toBe(3723);

            runtime.emit("file-companion:open", ["C:/media/talk.mp3"]);
            await screen.findByText("talk.mp3");
            const audio = await waitFor(() => {
                const node = document.querySelector("audio") as HTMLMediaElement | null;
                expect(node).toBeTruthy();
                return node!;
            });
            Object.defineProperty(audio, 'readyState', { value: 1, configurable: true });
            Object.defineProperty(audio, 'duration', { value: 10000, configurable: true });
            act(() => {
                runtime.emit("ai-assistant-response", JSON.stringify({
                    session_key: "file-companion:talk.mp3",
                    text: "1:02:03 后半段",
                    request_id: "seek-audio",
                }));
            });
            const audioStamp = await screen.findByTestId("file-companion-seek");
            expect(audioStamp.getAttribute("data-seconds")).toBe("3723");
            fireEvent.click(audioStamp);
            expect(audio.currentTime).toBe(3723);
        } finally {
            restore();
        }
    });

    it("renders formulas on a seekable note line and keeps a display block intact", async () => {
        boot.mockResolvedValue({ paths: ["C:/media/clip.mp4"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-video-note");
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: [
                    "## 章节",
                    "0:00–2:00 令 $x$ 成立",
                    "$$",
                    "a+b",
                    "$$",
                    "12:05 「$y^2$」",
                ].join("\n"),
                request_id: "formula-1",
            }));
        });
        const answer = await screen.findByTestId("file-companion-answer");
        const stamps = answer.querySelectorAll('[data-testid="file-companion-seek"]');
        expect(Array.from(stamps).map((node) => node.textContent)).toEqual(["0:00–2:00", "12:05"]);
        expect(Array.from(stamps).map((node) => node.getAttribute("data-seconds"))).toEqual(["0", "725"]);
        expect(answer.querySelectorAll('[data-testid="assistant-inline-math"]')).toHaveLength(2);
        expect(answer.querySelector('[data-testid="assistant-display-math"]')?.textContent).toContain("a");
        expect(answer.textContent).not.toContain("$x$");
        expect(answer.textContent).not.toContain("$y^2$");
        expect(answer.textContent).not.toContain("$$");
    });

    it("keeps a timed formula row inside the display block", async () => {
        boot.mockResolvedValue({ paths: ["C:/media/clip.mp4"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-video-note");
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: [
                    "## 章节",
                    "0:00–2:00 分段",
                    "$$",
                    "0:00 & t_0",
                    "2:00 & t_1",
                    "$$",
                    "12:05 下一句",
                ].join("\n"),
                request_id: "formula-row-1",
            }));
        });
        const answer = await screen.findByTestId("file-companion-answer");
        const stamps = answer.querySelectorAll('[data-testid="file-companion-seek"]');
        expect(Array.from(stamps).map((node) => node.textContent)).toEqual(["0:00–2:00", "12:05"]);
        expect(Array.from(stamps).map((node) => node.getAttribute("data-seconds"))).toEqual(["0", "725"]);
        expect(answer.querySelector('[data-testid="assistant-display-math"]')?.textContent).toContain("t_0");
        expect(answer.querySelector('[data-testid="assistant-display-math"]')?.textContent).toContain("t_1");
    });

    it("keeps a symbolic timed formula row inside the display block", async () => {
        boot.mockResolvedValue({ paths: ["C:/media/clip.mp4"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-video-note");
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: [
                    "## 章节",
                    "0:00–2:00 分段",
                    "$$",
                    "0:00 = t_0",
                    "2:00 = t_1",
                    "$$",
                    "12:05 下一句",
                ].join("\n"),
                request_id: "formula-symbolic-1",
            }));
        });
        const answer = await screen.findByTestId("file-companion-answer");
        const stamps = answer.querySelectorAll('[data-testid="file-companion-seek"]');
        expect(Array.from(stamps).map((node) => node.textContent)).toEqual(["0:00–2:00", "12:05"]);
        expect(answer.querySelector('[data-testid="assistant-display-math"]')?.textContent).toContain("t_0");
        expect(answer.querySelector('[data-testid="assistant-display-math"]')?.textContent).toContain("t_1");
    });

    it("typesets a display formula written inside a chapter sentence", async () => {
        boot.mockResolvedValue({ paths: ["C:/media/clip.mp4"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-video-note");
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: ["## 章节", "0:00–2:00 令 $$E=mc^2$$ 成立", "12:05 下一句"].join("\n"),
                request_id: "formula-sentence-1",
            }));
        });
        const answer = await screen.findByTestId("file-companion-answer");
        const stamps = answer.querySelectorAll('[data-testid="file-companion-seek"]');
        expect(Array.from(stamps).map((node) => node.textContent)).toEqual(["0:00–2:00", "12:05"]);
        const math = answer.querySelector('[data-testid="assistant-inline-math"]');
        expect(math?.textContent || "").toContain("E");
        expect(math?.textContent || "").not.toContain("$");
        expect(answer.textContent).not.toContain("$$");
        expect(answer.textContent).toContain("成立");
    });

    it("keeps PDF as the default export and writes LaTeX only from the same control", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { content: "hello", editable: true, readOnly: false }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-export-chat");
        expect((screen.getByTestId("file-companion-export-chat") as HTMLButtonElement).disabled).toBe(true);
        expect(screen.queryByText("导出公式")).toBeNull();
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:a.md",
                text: "行内 $x^2$ 结束",
                request_id: "export-1",
            }));
        });
        await screen.findByTestId("file-companion-answer");
        const chatExport = screen.getByTestId("file-companion-export-chat");
        expect(chatExport.textContent).toBe("导出 PDF");
        expect(screen.getByTestId("file-companion-paper").nextElementSibling).toBe(chatExport);
        expect(chatExport.nextElementSibling).toBe(screen.getByTestId("file-companion-import"));
        fireEvent.click(chatExport);
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        expect(exportChatTex).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-export-chat-menu"));
        fireEvent.click(screen.getByTestId("file-companion-export-chat-tex"));
        await waitFor(() => expect(exportChatTex).toHaveBeenCalledTimes(1));
        expect(exportChatPdf).toHaveBeenCalledTimes(1);
        const answerPdf = screen.getByTestId(/^file-companion-export-pdf-/);
        fireEvent.click(answerPdf);
        await waitFor(() => expect(exportPdf).toHaveBeenCalledTimes(1));
        expect(exportTex).not.toHaveBeenCalled();
        const answerCopy = screen.getByTestId(/^file-companion-copy-/);
        expect(answerCopy.querySelector("svg")?.getAttribute("data-icon")).toBe("markdown");
        expect(screen.queryByTestId("file-companion-export-menu")).toBeNull();
        const answerTex = screen.getByTestId("file-companion-export-tex");
        expect(answerTex.querySelector("svg")?.getAttribute("data-icon")).toBe("latex");
        expect(answerTex.getAttribute("aria-label")).toBe("导出为 LaTeX");
        fireEvent.click(answerTex);
        await waitFor(() => expect(exportTex).toHaveBeenCalledTimes(1));
        expect(screen.queryByText("导出公式")).toBeNull();
    });

    it("shows a local note prompt, writes a note card, and does not call the model for that prompt", async () => {
        boot.mockResolvedValue({ paths: ["C:/media/clip.mp4"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow({ askOnOpen: true });
        await screen.findByText("这个视频还没有笔记。点「生成笔记」，或发送「生成笔记」。");
        expect(send).not.toHaveBeenCalled();
        expect(screen.queryByTestId("file-companion-message-user")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(send).toHaveBeenCalledWith(expect.objectContaining({
            path: "C:/media/clip.mp4",
            text: "生成笔记",
            purpose: "video-note",
        })));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("生成笔记"));
        let releaseNote: () => void = () => undefined;
        send.mockImplementationOnce(() => new Promise<void>((resolve) => { releaseNote = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("取消"));
        const stopping = screen.getByTestId("file-companion-video-note") as HTMLButtonElement;
        expect(stopping.getAttribute("aria-busy")).not.toBe("true");
        expect(stopping.disabled).toBe(false);
        fireEvent.click(stopping);
        await waitFor(() => expect(cancelTurn).toHaveBeenCalledWith("C:/media/clip.mp4"));
        expect(screen.getByTestId("file-companion-video-note").textContent).toContain("取消");
        expect((screen.getByTestId("file-companion-video-note") as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId("file-companion-video-note").getAttribute("aria-busy")).not.toBe("true");
        releaseNote();
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("生成笔记"));
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "不要做笔记" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(3));
        const ordinary = send.mock.calls[2][0] as { purpose?: string; text?: string };
        expect(ordinary.text).toBe("不要做笔记");
        expect(ordinary.purpose).toBeUndefined();
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: "## 概要\n12:05 公式",
                request_id: "note-1",
                note_card_path: "C:/data/clip-笔记.pdf",
            }));
        });
        await screen.findByTestId("file-companion-note-import");
        expect(screen.queryByText("已生成论文解读")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-note-import"));
        await waitFor(() => expect(importKnowledge).toHaveBeenCalledWith("C:/data/clip-笔记.md"));
        fireEvent.click(screen.getByTestId("file-companion-note-upload"));
        await waitFor(() => expect(uploadCloud).toHaveBeenCalledWith("C:/data/clip-笔记.pdf"));
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: "普通回答不带走卡片",
                request_id: "ordinary-1",
            }));
        });
        await screen.findByText("普通回答不带走卡片");
        expect(screen.getByTestId("task-result-card-note-1:assistant")).toBeTruthy();
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:clip.mp4",
                text: "## 概要\n第二版正文",
                request_id: "note-2",
                note_card_path: "C:/data/clip-笔记.pdf",
            }));
        });
        await screen.findByText("第二版正文");
        expect(screen.queryByTestId("task-result-card-note-1:assistant")).toBeNull();
        const card = screen.getByTestId("task-result-card-note-2:assistant");
        expect(card.querySelector("a")?.getAttribute("title")).toBe("C:/data/clip-笔记.pdf");
        expect(screen.getAllByTestId("task-result-view-btn")).toHaveLength(1);
        const earlier = screen.getAllByTestId("file-companion-message-assistant").find((node) => node.textContent?.includes("公式"));
        expect(earlier?.querySelector('[data-testid="file-companion-seek"]')?.textContent).toBe("12:05");
        expect(earlier?.querySelector('[data-testid="task-result-view-btn"]')).toBeNull();
        fireEvent.click(screen.getByLabelText("关闭 clip.mp4"));
        await waitFor(() => expect(cancelTurn).toHaveBeenCalledWith("C:/media/clip.mp4"));
        expect(discardWeb).not.toHaveBeenCalled();
    });

    it("stops a web note from the title button without the busy cursor", async () => {
        boot.mockResolvedValue({ paths: [] });
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "https://www.youtube.com/watch?v=abc" } });
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        await screen.findByTestId("file-companion-web-frame");
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("停止"));
        // The centred menu would hang under the player HWND. While that
        // player is up, including during the note, it hangs over the chat.
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        expect(screen.getByTestId("file-companion-open-menu").className).toContain("file-companion-open-menu--beside-player");
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        const stopping = screen.getByTestId("file-companion-video-note") as HTMLButtonElement;
        expect(stopping.getAttribute("aria-busy")).not.toBe("true");
        expect(stopping.disabled).toBe(false);
        fireEvent.click(stopping);
        await waitFor(() => expect(stopWeb).toHaveBeenCalledWith("web-abc", expect.any(Number)));
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(stopping.textContent).toContain("停止");
        expect(stopping.disabled).toBe(false);
        expect(stopping.getAttribute("aria-busy")).not.toBe("true");
        release();
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
    });

    it("opens a video page without the local-file path and notes from the player", async () => {
        boot.mockResolvedValue({ paths: [] });
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        const openField = async (value: string) => {
            if (!screen.queryByTestId("file-companion-open-menu")) {
                fireEvent.click(screen.getByTestId("file-companion-choose"));
            }
            fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value } });
            fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        };
        openWeb.mockResolvedValue({
            kind: "web",
            frameUrl: "https://www.youtube.com/embed/abc",
            sessionId: "web-abc",
        });
        await openField("https://www.youtube.com/watch?v=abc&t=120");
        const frame = await screen.findByTestId("file-companion-web-frame");
        expect(frame.tagName).toBe("DIV");
        expect(frame.querySelector("iframe")).toBeNull();
        expect(frame.getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        expect(frame.getAttribute("data-url")).not.toContain("t=");
        await waitFor(() => expect(playerPlace).toHaveBeenCalledWith(
            "https://www.youtube.com/embed/abc",
            expect.any(Number),
            expect.any(Number),
            expect.any(Number),
            expect.any(Number),
        ));
        expect(open).not.toHaveBeenCalled();
        expect(openArxiv).not.toHaveBeenCalled();
        expect(send).not.toHaveBeenCalled();
        expect(sendWeb).not.toHaveBeenCalled();
        expect((screen.getByTestId("file-companion-import") as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            text: "记笔记",
            purpose: "video-note",
            fullscreen: false,
        })));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:web-abc",
                text: "12:05 从录制开始",
                request_id: "web-reply",
            }));
        });
        const stamp = await screen.findByTestId("file-companion-seek");
        const seen: Array<{ path?: string; seconds?: number }> = [];
        const onSeek = (event: Event) => {
            seen.push((event as CustomEvent<{ path?: string; seconds?: number }>).detail);
        };
        window.addEventListener("file-companion:seek", onSeek);
        fireEvent.click(stamp);
        expect(seen).toEqual([]);
        await screen.findByText("网页笔记不能跳回播放位置");
        window.removeEventListener("file-companion:seek", onSeek);
        let release: () => void = () => undefined;
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", [{ fullscreen: true }]); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            text: "记笔记",
            purpose: "video-note",
            fullscreen: true,
            rectX: 0,
            rectY: 0,
            rectW: 0,
            rectH: 0,
        })));
        act(() => { runtime.emit("file-companion:webview-fullscreen", [{ fullscreen: false }]); });
        await waitFor(() => expect(cancelWeb).toHaveBeenCalledWith("web-abc", expect.any(Number)));
        expect(playerLeft).not.toHaveBeenCalled();
        release();
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        cancelWeb.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            text: "记笔记",
            purpose: "video-note",
            fullscreen: true,
        })));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false }); });
        await waitFor(() => expect(cancelWeb).toHaveBeenCalledWith("web-abc", expect.any(Number)));
        expect(playerLeft).not.toHaveBeenCalled();
        release();
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        sendWeb.mockClear();
        cancelWeb.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "web-abc" })));
        openWeb.mockResolvedValue({ kind: "web", frameUrl: "https://example.com/other", sessionId: "web-other" });
        await openField("https://example.com/other");
        await screen.findByTestId("file-companion-tab-example.com");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledWith("web-abc", expect.any(Number)));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: false }); });
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(playerLeft.mock.calls.some((call) => call[0] === "web-other")).toBe(false);
        release();
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // Leaving the player stops the recording and still writes the note.
        // A later fullscreenchange must not retarget another session.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        const liveFrame = await screen.findByTestId("file-companion-web-frame");
        const priorFull = Object.getOwnPropertyDescriptor(document, "fullscreenElement");
        let currentFull: Element | null = liveFrame;
        Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => currentFull });
        try {
            act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
            fireEvent.click(screen.getByTestId("file-companion-video-note"));
            await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "web-abc" })));
            open.mockImplementation(async (path: string) => doc(path));
            runtime.emit("file-companion:open", ["C:/notes/a.md"]);
            await screen.findByTestId("file-companion-tab-a.md");
            await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
            await waitFor(() => expect(playerLeft).toHaveBeenCalledWith("web-abc", expect.any(Number)));
            currentFull = null;
            act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
            expect(cancelWeb).not.toHaveBeenCalled();
            expect(playerLeft.mock.calls.some((call) => call[0] === "web-other")).toBe(false);
        } finally {
            if (priorFull) Object.defineProperty(document, "fullscreenElement", priorFull);
            else Reflect.deleteProperty(document, "fullscreenElement");
        }
        release();
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        sendWeb.mockResolvedValue(undefined);
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            text: "记笔记",
            fullscreen: false,
        })));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // A note that never entered fullscreen still stops when the player
        // leaves the screen. The capture must not keep photographing the file
        // that replaced it.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            fullscreen: false,
        })));
        open.mockImplementation(async (path: string) => doc(path));
        let releaseLeave: () => void = () => undefined;
        playerLeft.mockImplementation(() => new Promise<void>((resolve) => { releaseLeave = resolve; }));
        runtime.emit("file-companion:open", ["C:/notes/leave.md"]);
        await screen.findByTestId("file-companion-tab-leave.md");
        const held = await screen.findByTestId("file-companion-web-frame");
        expect(held.getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledWith("web-abc", expect.any(Number)));
        expect(screen.getByTestId("file-companion-web-frame").getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        expect(cancelWeb).not.toHaveBeenCalled();
        await act(async () => { releaseLeave(); });
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        playerLeft.mockResolvedValue(undefined);
        release();
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // A rejected leave has not retired the watch. The next page stays
        // unpainted, and the failed call is not retried. The picture drops
        // when the note call returns, because capture has finished.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            fullscreen: false,
        })));
        open.mockImplementation(async (path: string) => doc(path));
        playerLeft.mockRejectedValue(new Error("bridge"));
        runtime.emit("file-companion:open", ["C:/notes/reject.md"]);
        await screen.findByTestId("file-companion-tab-reject.md");
        const rejected = await screen.findByTestId("file-companion-web-frame");
        expect(rejected.getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledTimes(1));
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
        expect(screen.getByTestId("file-companion-web-frame").getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        expect(playerLeft).toHaveBeenCalledTimes(1);
        expect(cancelWeb).not.toHaveBeenCalled();
        await act(async () => { release(); });
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());

        // A leave that never settles holds the same way. Fulfilling it after
        // the note has returned, and after a later recording has pinned,
        // must not release that later picture.
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "web-abc" })));
        let releaseHung: () => void = () => undefined;
        playerLeft.mockImplementation(() => new Promise<void>((resolve) => { releaseHung = resolve; }));
        runtime.emit("file-companion:open", ["C:/notes/hung.md"]);
        await screen.findByTestId("file-companion-tab-hung.md");
        const hung = await screen.findByTestId("file-companion-web-frame");
        expect(hung.getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledTimes(1));
        await act(async () => { release(); });
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "web-abc" })));
        let releaseNext: () => void = () => undefined;
        playerLeft.mockImplementation(() => new Promise<void>((resolve) => { releaseNext = resolve; }));
        runtime.emit("file-companion:open", ["C:/notes/later.md"]);
        await screen.findByTestId("file-companion-tab-later.md");
        const later = await screen.findByTestId("file-companion-web-frame");
        expect(later.getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledTimes(1));
        await act(async () => { releaseHung(); });
        expect(screen.getByTestId("file-companion-web-frame").getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        expect(playerLeft).toHaveBeenCalledTimes(1);
        await act(async () => { releaseNext(); });
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        await act(async () => { release(); });
        playerLeft.mockResolvedValue(undefined);
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // A card from another note is not this capture finishing. The held
        // player stays. This attempt's own reply is after capture, so the
        // picture can change.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "web-abc" })));
        const liveRequest = (sendWeb.mock.calls[sendWeb.mock.calls.length - 1][0] as { requestId: string }).requestId;
        let releaseCardLeave: () => void = () => undefined;
        playerLeft.mockImplementation(() => new Promise<void>((resolve) => { releaseCardLeave = resolve; }));
        open.mockImplementation(async (path: string) => doc(path));
        runtime.emit("file-companion:open", ["C:/notes/card.md"]);
        await screen.findByTestId("file-companion-tab-card.md");
        expect(screen.getByTestId("file-companion-web-frame").getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledTimes(1));
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:web-abc",
                text: "上一份笔记",
                request_id: "stale-card",
                note_card_path: "C:/data/stale.pdf",
            }));
        });
        expect(screen.getByTestId("file-companion-web-frame").getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:web-abc",
                text: "这一份笔记",
                request_id: liveRequest,
                note_card_path: "C:/data/live.pdf",
            }));
        });
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        await act(async () => { releaseCardLeave(); });
        expect(screen.queryByTestId("file-companion-web-frame")).toBeNull();
        await act(async () => { release(); });
        playerLeft.mockResolvedValue(undefined);
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // Once this leave has been accepted, capture has stopped. Looking
        // back at the player and leaving again must show the selected page
        // without waiting on another leave.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "web-abc" })));
        let releaseAccepted: () => void = () => undefined;
        playerLeft.mockImplementation(() => new Promise<void>((resolve) => { releaseAccepted = resolve; }));
        open.mockImplementation(async (path: string) => doc(path));
        runtime.emit("file-companion:open", ["C:/notes/stay.md"]);
        await screen.findByTestId("file-companion-tab-stay.md");
        expect(screen.getByTestId("file-companion-web-frame").getAttribute("data-url")).toBe("https://www.youtube.com/embed/abc");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledTimes(1));
        await act(async () => { releaseAccepted(); });
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        const acceptedLeaves = playerLeft.mock.calls.length;
        let releaseAgain: () => void = () => undefined;
        playerLeft.mockImplementation(() => new Promise<void>((resolve) => { releaseAgain = resolve; }));
        runtime.emit("file-companion:open", ["C:/notes/again.md"]);
        await screen.findByTestId("file-companion-tab-again.md");
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        expect(playerLeft).toHaveBeenCalledTimes(acceptedLeaves);
        releaseAgain();
        await act(async () => { release(); });
        playerLeft.mockResolvedValue(undefined);
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // The Windows flag belongs to the player that entered fullscreen.
        // Leaving that player drops it even when no fullscreen event arrives,
        // so the next note is not marked fullscreen.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            fullscreen: true,
        })));
        open.mockImplementation(async (path: string) => doc(path));
        runtime.emit("file-companion:open", ["C:/notes/flag.md"]);
        await screen.findByTestId("file-companion-tab-flag.md");
        await waitFor(() => expect(playerLeft).toHaveBeenCalledWith("web-abc", expect.any(Number)));
        expect(cancelWeb).not.toHaveBeenCalled();
        release();
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        sendWeb.mockResolvedValue(undefined);
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            text: "记笔记",
            fullscreen: false,
        })));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));

        // The Windows fullscreen flag can stay true after the iframe is gone.
        // Leaving the player stops this attempt. Opening the page again starts
        // a note from the visible player; the leftover flag must not keep the
        // previous recording running.
        sendWeb.mockClear();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        sendWeb.mockImplementation(() => new Promise<void>((resolve) => { release = resolve; }));
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        act(() => { runtime.emit("file-companion:webview-fullscreen", { fullscreen: true }); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            captureEpoch: expect.any(Number),
        })));
        const stuckEpoch = (sendWeb.mock.calls[sendWeb.mock.calls.length - 1][0] as { captureEpoch: number }).captureEpoch;
        open.mockImplementation(async (path: string) => doc(path));
        runtime.emit("file-companion:open", ["C:/notes/b.md"]);
        await screen.findByTestId("file-companion-tab-b.md");
        await waitFor(() => expect(screen.queryByTestId("file-companion-web-frame")).toBeNull());
        await waitFor(() => expect(playerLeft).toHaveBeenCalledWith("web-abc", stuckEpoch));
        const priorStuck = Object.getOwnPropertyDescriptor(document, "fullscreenElement");
        Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => null });
        try {
            act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
            expect(playerLeft).toHaveBeenCalledWith("web-abc", stuckEpoch);
            expect(cancelWeb).not.toHaveBeenCalled();
        } finally {
            if (priorStuck) Object.defineProperty(document, "fullscreenElement", priorStuck);
            else Reflect.deleteProperty(document, "fullscreenElement");
        }
        release();
        fireEvent.click(screen.getByTestId("file-companion-tab-www.youtube.com"));
        await screen.findByTestId("file-companion-web-frame");
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        sendWeb.mockClear();
        sendWeb.mockResolvedValue(undefined);
        act(() => { document.dispatchEvent(new Event("fullscreenchange")); });
        fireEvent.click(screen.getByTestId("file-companion-video-note"));
        await waitFor(() => expect(sendWeb).toHaveBeenCalledWith(expect.objectContaining({
            sessionId: "web-abc",
            text: "记笔记",
            fullscreen: false,
        })));
        await waitFor(() => expect(screen.getByTestId("file-companion-video-note").textContent).toContain("记笔记"));
        open.mockClear();

        openWeb.mockReset();
        openWeb.mockRejectedValue(new Error("本地文件请用「打开文件」，这里不接受 file:// 和其他协议"));
        await openField("file:///C:/media/clip.mp4");
        await screen.findByText("本地文件请用「打开文件」，这里不接受 file:// 和其他协议");
        expect(open).not.toHaveBeenCalled();
        openWeb.mockRejectedValue(new Error("这个地址是伴读自己的页面，不能当作视频网页打开"));
        await openField("http://wails.localhost/index.html");
        await screen.findByText("这个地址是伴读自己的页面，不能当作视频网页打开");
        expect(open).not.toHaveBeenCalled();
        openArxiv.mockClear();
        openWeb.mockReset();
        openWeb.mockResolvedValue({ kind: "web", frameUrl: "https://youtu.be/xyz", sessionId: "web-xyz" });
        await openField("https://youtu.be/xyz");
        await waitFor(() => expect(openWeb).toHaveBeenCalledWith("https://youtu.be/xyz"));
        expect(openArxiv).not.toHaveBeenCalled();
        cancelWeb.mockClear();
        playerLeft.mockClear();
        fireEvent.click(screen.getByLabelText("关闭 youtu.be"));
        await waitFor(() => expect(discardWeb).toHaveBeenCalledWith("web-xyz"));
        expect(cancelWeb).not.toHaveBeenCalled();
        expect(playerLeft).not.toHaveBeenCalled();
        await openField("2510.16079");
        await waitFor(() => expect(openArxiv).toHaveBeenCalledWith("2510.16079"));
    });
});


// jsdom has no Wails runtime, so nothing here can reproduce this: the titlebar
// is a window-drag region and --wails-draggable inherits, which made the 打开
// panel draggable as well. Wails defers the drag to the first mousemove, so a
// slightly shaky click on the arXiv box moved the window instead of focusing
// the input. Only a source assertion can pin that down.
describe("companion titlebar keeps its controls out of the window drag region", () => {
    const css = () => readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../../App.css"), "utf8");

    // A selector can appear in several rules (layout first, opt-out later), so
    // every match has to be considered — checking only the first would read the
    // layout rule and report a false negative.
    const declaresNoDrag = (source: string, selector: string) => {
        const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        const rule = new RegExp(`([^{}]*${escaped}[^{}]*)\\{([^}]*)\\}`, "g");
        for (const match of source.matchAll(rule)) {
            if (/--wails-draggable:\s*no-drag/.test(match[2])) return true;
        }
        return false;
    };

    it("declares no-drag on the trigger wrapper, which the panel inherits", () => {
        expect(css()).toMatch(/\.file-companion-open\s*\{[^}]*--wails-draggable:\s*no-drag/);
    });

    it("hangs the open menu over the chat when a native player would cover the centre", () => {
        expect(css()).toMatch(/\.file-companion-open-menu--beside-player\s*\{[^}]*left:\s*auto/);
        expect(css()).toMatch(/\.file-companion-open-menu--beside-player\s*\{[^}]*right:\s*8px/);
    });

    // jsdom never runs Wails' drop filter. OnFileDrop is registered with
    // useDropTarget, and that filter keeps the file only when the element
    // under the pointer has this property. The empty hint is inside the
    // window, and the property inherits, so a drop there has to open.
    it("marks the companion window as the Wails file-drop target", () => {
        expect(css()).toMatch(/\.file-companion-window\s*\{[^}]*--wails-drop-target:\s*drop/);
    });

    it("registers the drop on that target", async () => {
        let useDropTarget: boolean | undefined;
        (window as unknown as { runtime: unknown }).runtime = {
            EventsOn: () => {},
            EventsOff: () => {},
            OnFileDrop: (_cb: DropCb, flag?: boolean) => { useDropTarget = flag; },
            OnFileDropOff: () => {},
        };
        boot.mockResolvedValue({ paths: [] });
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        expect(useDropTarget).toBe(true);
    });

    // The open panel hangs over the document. The PDF preview is an iframe in
    // that document, and an iframe paints above an overflowing sibling that
    // has no stacking context — the panel opened and then vanished under it.
    // The document pane itself must stay out of a stacking context: a z-index
    // there would pin the figure lightbox under the title bar.
    it("stacks the title bar above the document so the open panel stays visible", () => {
        const source = css();
        expect(source).toMatch(/\.file-companion-titlebar\s*\{[^}]*z-index:\s*40/);
        const split = source.match(/\.file-companion-split\s*\{[^}]*\}/);
        expect(split, "document pane rule").toBeTruthy();
        expect(split![0]).not.toMatch(/z-index/);
    });

    it("declares no-drag on every clickable region of the bar", () => {
        const source = css();
        for (const selector of [
            ".file-companion-titlebar-files",
            ".file-companion-titlebar-tabs",
            ".file-companion-titlebar-actions",
            ".file-companion-tool--close",
        ]) {
            expect(declaresNoDrag(source, selector), `${selector} must opt out of the drag region`).toBe(true);
        }
    });
});
