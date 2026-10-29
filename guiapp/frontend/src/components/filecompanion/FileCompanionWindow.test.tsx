// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FileCompanionWindow, {
    clampCompanionChatWidth,
    clampCompanionTabScroll,
    companionLibraryPollFast,
    companionAnnotationMarkdown,
    companionMessageReview,
    companionOpenSetPaths,
    companionTabEndsFromBoxes,
    companionTabNudgeDelta,
    companionTabScrollEnds,
    revealCompanionTab,
} from "./FileCompanionWindow";
import { normalizeInlineListMarkers } from "../ai/aiAssistantMarkdownNormalize";
import { finishCompanionReply, splitCompanionStreamDelta } from "./companionReasoning";
import { ToastProvider } from "../Toast";
import { DialogProvider } from "../CustomDialog";
import { loadPdfMarks, savePdfMarks, hydratePdfMarks } from "./pdfMarks";
import { hydrateChatMarks, loadChatMarks, saveChatMarks } from "./chatMarks";
import { hydratePdfTranslations, loadPdfTranslations, notifyPdfAnnotationsChanged, savePdfTranslations } from "./pdfTranslations";
import { hydratePdfDrawings, loadPdfDrawings, notifyPdfDrawingsChanged, savePdfDrawings } from "./pdfDrawings";
import { chatTextSpans } from "./chatMarkAnchors";
import { fileCompanionInputHistory, hydrateFileCompanionInputHistory } from "./fileCompanionInputHistory";

const boot = vi.fn();
const resume = vi.fn();
const setOpenSet = vi.fn();
const open = vi.fn();
const save = vi.fn();
const send = vi.fn();
const importKnowledge = vi.fn();
const uploadCloud = vi.fn();
const scanPDF = vi.fn(async (..._args: any[]): Promise<{ scan: boolean; path: string }> => ({ scan: false, path: "" }));
const convertScan = vi.fn(async (..._args: any[]): Promise<{ ok: boolean; scan: boolean; complete?: boolean; pending?: boolean; started?: boolean; save_failed?: boolean; path?: string; message?: string }> => ({ ok: false, scan: false, path: "", message: "" }));
const chooseConvertDest = vi.fn(async (..._args: unknown[]) => "C:/out/chosen.pdf");
const ready = vi.fn();
const choose = vi.fn();
const openArxiv = vi.fn(async (..._args: any[]): Promise<any> => undefined);
const searchArxiv = vi.fn(async (..._args: any[]): Promise<any> => [
    { id: "2510.16079", title: "Attention Is All You Need", abstract: "We propose a new network architecture.", authors: "Ashish Vaswani" },
]);
const replaceOffice = vi.fn();
const clearChat = vi.fn();
const quit = vi.fn();
const hideWindow = vi.fn();
const windowFullscreen = vi.fn();
const windowUnfullscreen = vi.fn();
const windowGetSize = vi.fn(async () => ({ w: 1280, h: 800 }));
const windowGetPosition = vi.fn(async () => ({ x: 40, y: 30 }));
const windowSetSize = vi.fn();
const setScreenPosition = vi.fn();
const openResult = vi.fn();
const saveResult = vi.fn();
const figureDataURL = vi.fn();
const exportPdf = vi.fn();
const exportProject = vi.fn();
const exportChatPdf = vi.fn();
const exportChatCloud = vi.fn();
const exportWebChatCloud = vi.fn();
const showItemInFolder = vi.fn();
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
type ListLibraryResult = { items: Array<Record<string, unknown>>; notice: string; updates?: Array<Record<string, unknown>>; removed?: string[]; removedHosts?: Record<string, string>; notesReply?: boolean; notesReview?: boolean };
const listLibrary = vi.fn<(args: unknown[]) => Promise<ListLibraryResult>>(async () => ({ items: [], notice: "" }));
const shareDocument = vi.fn(async () => ({ url: "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", name: "a.md", visibility: "public", permanent: true }));
const importShare = vi.fn(async (..._args: any[]): Promise<{ id: string; name: string; path: string; source: string; highlights?: string; chatMarks?: string; annotations?: string; drawings?: string }> => ({ id: "cd_0123456789abcdef0123456789abcdef", name: "shared.pdf", path: "C:/library/shared.pdf", source: "cloud" }));
const listGroups = vi.fn(async () => [] as Array<Record<string, unknown>>);
const syncShare = vi.fn(async () => ({ revision: 2 }));
const updateShare = vi.fn(async (..._args: unknown[]) => ({ url: "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", name: "论文.pdf", visibility: "private", permanent: false, expiresAt: "2026-11-24T00:00:00Z" }));
// AddFileCompanionShareNote returns the legacy notes array or the newer
// { notes, notesRevision } object; companionNotePost accepts both.
type AddShareNoteResult = Array<Record<string, unknown>> | { notes?: Array<Record<string, unknown>>; notesRevision?: number };
const addShareNote = vi.fn(async (): Promise<AddShareNoteResult> => []);
const reviewShareNote = vi.fn(async (): Promise<AddShareNoteResult> => []);
const createGroup = vi.fn(async (name: string) => ({ id: "cg_0123456789abcdef0123456789abcdef", name, admin: true, members: [] as Array<Record<string, unknown>> }));
const addGroupMember = vi.fn(async () => ({ id: "cg_0123456789abcdef0123456789abcdef", name: "论文组", admin: true, members: [] as Array<Record<string, unknown>> }));
const removeGroupMember = vi.fn(async () => ({ id: "cg_0123456789abcdef0123456789abcdef", name: "论文组", admin: true, members: [] as Array<Record<string, unknown>> }));
const updateGroup = vi.fn(async (id: string, name: string, icon: string) => ({ id, name, icon, admin: true, members: [] as Array<Record<string, unknown>> }));
const deleteGroup = vi.fn(async () => undefined);
const deleteProject = vi.fn(async (..._args: unknown[]) => ({ id: "", path: "", name: "", cleared: false }));
const deleteShare = vi.fn(async (..._args: unknown[]): Promise<void> => undefined);
const transferHost = vi.fn(async (..._args: unknown[]): Promise<void> => undefined);
const revokeHost = vi.fn(async (..._args: unknown[]): Promise<void> => undefined);
const leaveGroup = vi.fn(async () => undefined);

vi.mock("../../../wailsjs/go/main/App", () => ({
    GetFileCompanionBoot: (...args: unknown[]) => boot(...args),
    FileCompanionResumePaths: (...args: unknown[]) => resume(...args),
    SetFileCompanionOpenSet: (...args: unknown[]) => setOpenSet(...args),
    FileCompanionOpen: (...args: unknown[]) => open(...args),
    FileCompanionSaveText: (...args: unknown[]) => save(...args),
    SendFileCompanionMessage: (...args: unknown[]) => send(...args),
    FileCompanionImportKnowledge: (...args: unknown[]) => importKnowledge(...args),
    FileCompanionUploadCloud: (...args: unknown[]) => uploadCloud(...args),
    FileCompanionPDFScan: (...args: unknown[]) => scanPDF(...args),
    FileCompanionChooseConvertDestination: (...args: unknown[]) => chooseConvertDest(...args),
    FileCompanionConvertScan: (...args: unknown[]) => convertScan(...args),
    FileCompanionReplaceOffice: (...args: unknown[]) => replaceOffice(...args),
    ClearFileCompanionChat: (...args: unknown[]) => clearChat(...args),
    FileCompanionUIReady: (...args: unknown[]) => ready(...args),
    ChooseFileCompanionFiles: (...args: unknown[]) => choose(...args),
    FileCompanionOpenArxiv: (...args: unknown[]) => openArxiv(...args),
    FileCompanionSearchArxiv: (...args: unknown[]) => searchArxiv(...args),
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
    FileCompanionExportWebTeX: (...args: Parameters<typeof exportWebTex>) => exportWebTex(...args),
    FileCompanionExportWebChatTeX: (...args: Parameters<typeof exportWebChatTex>) => exportWebChatTex(...args),
    OpenFileOrShowInFolder: (...args: Parameters<typeof openResult>) => openResult(...args),
    ExportTaskResultFile: (...args: Parameters<typeof saveResult>) => saveResult(...args),
    FileCompanionFigureDataURL: (...args: Parameters<typeof figureDataURL>) => figureDataURL(...args),
    FileCompanionExportPDF: (...args: Parameters<typeof exportPdf>) => exportPdf(...args),
    FileCompanionExportProject: (...args: Parameters<typeof exportProject>) => exportProject(...args),
    ListFileCompanionLibrary: (...args: Parameters<typeof listLibrary>) => listLibrary(...args),
    ShareFileCompanionDocument: (...args: Parameters<typeof shareDocument>) => shareDocument(...args),
    UpdateFileCompanionShare: (...args: Parameters<typeof updateShare>) => updateShare(...args),
    ImportFileCompanionShare: (...args: Parameters<typeof importShare>) => importShare(...args),
    ListFileCompanionGroups: (...args: Parameters<typeof listGroups>) => listGroups(...args),
    SyncFileCompanionShare: (...args: Parameters<typeof syncShare>) => syncShare(...args),
    AddFileCompanionShareNote: (...args: Parameters<typeof addShareNote>) => addShareNote(...args),
    ReviewFileCompanionShareNote: (...args: Parameters<typeof reviewShareNote>) => reviewShareNote(...args),
    CreateFileCompanionGroup: (...args: Parameters<typeof createGroup>) => createGroup(...args),
    AddFileCompanionGroupMember: (...args: Parameters<typeof addGroupMember>) => addGroupMember(...args),
    RemoveFileCompanionGroupMember: (...args: Parameters<typeof removeGroupMember>) => removeGroupMember(...args),
    UpdateFileCompanionGroup: (...args: Parameters<typeof updateGroup>) => updateGroup(...args),
    DeleteFileCompanionGroup: (...args: Parameters<typeof deleteGroup>) => deleteGroup(...args),
    DeleteFileCompanionProject: (...args: unknown[]) => deleteProject(...args),
    DeleteFileCompanionShare: (...args: unknown[]) => deleteShare(...args),
    TransferFileCompanionShareHost: (...args: unknown[]) => transferHost(...args),
    RevokeFileCompanionShareHost: (...args: unknown[]) => revokeHost(...args),
    LeaveFileCompanionGroup: (...args: Parameters<typeof leaveGroup>) => leaveGroup(...args),
    FileCompanionExportChatPDF: (...args: Parameters<typeof exportChatPdf>) => exportChatPdf(...args),
    FileCompanionExportChatCloud: (...args: Parameters<typeof exportChatCloud>) => exportChatCloud(...args),
    FileCompanionExportWebChatCloud: (...args: Parameters<typeof exportWebChatCloud>) => exportWebChatCloud(...args),
    ShowItemInFolder: (...args: unknown[]) => showItemInFolder(...args),
    PreviewTaskResultFile: async (path: string) => {
        const name = String(path).toLowerCase();
        if (name.endsWith(".mp4") || name.endsWith(".mp3")) {
            return { kind: name.endsWith(".mp3") ? "audio" : "video", preview_url: "/maclaw-preview/v1/file?t=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" };
        }
        return { kind: "text", content: "" };
    },
    GetBrandInfo: (...args: unknown[]) => getBrandInfo(...args),
    // Absent in older mocks. Vitest 4 throws on a missing export, and these cases still expect Chinese.
    GetCurrentLanguage: undefined,
    SetMainWindowScreenPosition: (...args: unknown[]) => setScreenPosition(...args),
}));

vi.mock("../../../wailsjs/runtime", () => ({
    Quit: () => quit(),
    WindowHide: () => hideWindow(),
    WindowFullscreen: () => windowFullscreen(),
    WindowUnfullscreen: () => windowUnfullscreen(),
    WindowGetSize: () => windowGetSize(),
    WindowGetPosition: () => windowGetPosition(),
    WindowSetSize: (width: number, height: number) => windowSetSize(width, height),
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

// 导出 PDF opens a destination panel. 导出文件 is the save-dialog path.
function exportChatAsFile() {
    fireEvent.click(screen.getByTestId("file-companion-export-chat"));
    fireEvent.click(screen.getByTestId("file-companion-export-chat-file"));
}

// 打开 is a menu now: the 打开文档或导入项目 row is what reaches the system dialog.
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

vi.mock("../ai/CompanionPdfReader", () => ({
    CompanionPdfReader: ({ absPath }: { absPath: string }) => <div data-testid="pdf-preview-panel">{absPath}</div>,
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
    localStorage.removeItem("ai_assistant_theme_mode");
    localStorage.removeItem("maclaw.ai.themeMode");
    localStorage.removeItem("maclaw.ai.darkScheme");
    localStorage.removeItem("maclaw.ai.lightScheme");
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
    scanPDF.mockReset();
    scanPDF.mockResolvedValue({ scan: false, path: "" });
    convertScan.mockReset();
    convertScan.mockResolvedValue({ ok: false, scan: false, path: "", message: "" });
    chooseConvertDest.mockReset();
    chooseConvertDest.mockResolvedValue("C:/out/chosen.pdf");
    ready.mockReset();
    choose.mockReset();
    openArxiv.mockReset();
    openArxiv.mockResolvedValue("C:/papers/2510.16079.pdf");
    searchArxiv.mockReset();
    searchArxiv.mockResolvedValue([
        { id: "2510.16079", title: "Attention Is All You Need", abstract: "We propose a new network architecture.", authors: "Ashish Vaswani" },
    ]);
    replaceOffice.mockReset();
    clearChat.mockReset();
    quit.mockReset();
    hideWindow.mockReset();
    windowFullscreen.mockReset();
    windowUnfullscreen.mockReset();
    windowGetSize.mockReset();
    windowGetSize.mockResolvedValue({ w: 1280, h: 800 });
    windowGetPosition.mockReset();
    windowGetPosition.mockResolvedValue({ x: 40, y: 30 });
    windowSetSize.mockReset();
    setScreenPosition.mockReset();
    openResult.mockReset();
    saveResult.mockReset();
    figureDataURL.mockReset();
    getBrandInfo.mockReset();
    getBrandInfo.mockResolvedValue({ displayName: "MaClaw" });
    exportPdf.mockReset();
    exportPdf.mockResolvedValue("");
    exportProject.mockReset();
    exportProject.mockResolvedValue("");
    listLibrary.mockReset();
    listLibrary.mockResolvedValue({ items: [], notice: "" });
    deleteProject.mockReset();
    deleteProject.mockResolvedValue({ id: "", path: "", name: "", cleared: false });
    deleteShare.mockReset();
    deleteShare.mockResolvedValue(undefined);
    transferHost.mockReset();
    transferHost.mockResolvedValue(undefined);
    revokeHost.mockReset();
    revokeHost.mockResolvedValue(undefined);
    shareDocument.mockReset();
    shareDocument.mockResolvedValue({ url: "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", name: "a.md", visibility: "public", permanent: true });
    importShare.mockReset();
    importShare.mockResolvedValue({ id: "cd_0123456789abcdef0123456789abcdef", name: "shared.pdf", path: "C:/library/shared.pdf", source: "cloud" });
    listGroups.mockReset();
    listGroups.mockResolvedValue([]);
    syncShare.mockReset();
    syncShare.mockResolvedValue({ revision: 2 });
    updateShare.mockReset();
    updateShare.mockResolvedValue({ url: "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", name: "论文.pdf", visibility: "private", permanent: false, expiresAt: "2026-11-24T00:00:00Z" });
    addShareNote.mockReset();
    addShareNote.mockResolvedValue([]);
    reviewShareNote.mockReset();
    reviewShareNote.mockResolvedValue([]);
    createGroup.mockReset();
    createGroup.mockImplementation(async (name: string) => ({ id: "cg_0123456789abcdef0123456789abcdef", name, admin: true, members: [] }));
    addGroupMember.mockReset();
    addGroupMember.mockResolvedValue({ id: "cg_0123456789abcdef0123456789abcdef", name: "论文组", admin: true, members: [] });
    removeGroupMember.mockReset();
    removeGroupMember.mockResolvedValue({ id: "cg_0123456789abcdef0123456789abcdef", name: "论文组", admin: true, members: [] });
    updateGroup.mockReset();
    updateGroup.mockImplementation(async (id: string, name: string, icon: string) => ({ id, name, icon, admin: true, members: [] }));
    deleteGroup.mockReset();
    deleteGroup.mockResolvedValue(undefined);
    leaveGroup.mockReset();
    leaveGroup.mockResolvedValue(undefined);
    exportChatPdf.mockReset();
    exportChatPdf.mockResolvedValue("");
    exportChatCloud.mockReset();
    exportChatCloud.mockResolvedValue({ id: "draft-1", title: "a-伴读对话.pdf" });
    exportWebChatCloud.mockReset();
    showItemInFolder.mockReset();
    showItemInFolder.mockResolvedValue(undefined);
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
    // The reader stores live in SQLite in the app; tests start from an empty
    // in-memory mirror.
    hydrateFileCompanionInputHistory([]);
    hydratePdfMarks({});
    hydrateChatMarks({});
    hydratePdfTranslations({});
    hydratePdfDrawings({});
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
    it("follows the main window dark theme", () => {
        localStorage.setItem("ai_assistant_theme_mode", "dark");
        localStorage.setItem("maclaw.ai.darkScheme", "violet");
        installRuntime();
        renderWindow();
        const win = screen.getByTestId("file-companion-window");
        expect(win.getAttribute("data-ai-theme")).toBe("dark");
        expect(win.getAttribute("data-ai-dark-scheme")).toBe("violet");
        expect(document.documentElement.getAttribute("data-ai-theme")).toBe("dark");
        expect(document.documentElement.getAttribute("data-ai-dark-scheme")).toBe("violet");
        expect(document.documentElement.style.backgroundColor).toBe("");
        expect(document.documentElement.style.getPropertyValue("--theme-page-bg")).toBe("#0e0a18");
        expect(document.documentElement.style.colorScheme).toBe("dark");
    });

    it("switches to dark when the shared appearance changes", () => {
        const runtime = installRuntime();
        renderWindow();
        expect(screen.getByTestId("file-companion-window").getAttribute("data-ai-theme")).toBe("light");
        act(() => {
            runtime.emit("ui-appearance:changed", { themeMode: "dark", darkScheme: "ember", lightScheme: "fluent" });
        });
        const win = screen.getByTestId("file-companion-window");
        expect(win.getAttribute("data-ai-theme")).toBe("dark");
        expect(win.getAttribute("data-ai-dark-scheme")).toBe("ember");
        expect(document.documentElement.getAttribute("data-ai-dark-scheme")).toBe("ember");
        expect(document.documentElement.style.backgroundColor).toBe("");
        expect(document.documentElement.style.getPropertyValue("--theme-page-bg")).toBe("#140e0a");
        expect(localStorage.getItem("ai_assistant_theme_mode")).toBe("dark");
    });

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
        await waitFor(() => expect(fileCompanionInputHistory()).toContain("文件里有什么"));
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
        expect(fileCompanionInputHistory()).toContain("文件里有什么");
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
        expect(fileCompanionInputHistory()).toEqual([]);
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
        expect(screen.getByTestId("file-companion-open-local").textContent).toContain("打开文档或导入项目");
        const input = screen.getByTestId("file-companion-arxiv-input") as HTMLInputElement;
        expect(input.placeholder).toBe("如 2510.16079、arxiv.org/abs/…、视频页面，或伴读分享链接");
        // Nothing typed yet: 确定 has nothing to fetch.
        expect((screen.getByTestId("file-companion-arxiv-go") as HTMLButtonElement).disabled).toBe(true);
        const search = screen.getByTestId("file-companion-arxiv-search");
        expect(search.textContent).toBe("搜索");
        expect((search as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId("file-companion-arxiv-go").compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
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

    const shareLink = "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef";

    async function pasteOpenBox(value: string) {
        if (!screen.queryByTestId("file-companion-open-menu")) {
            fireEvent.click(screen.getByTestId("file-companion-choose"));
        }
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value } });
        // The click starts an async import and does not return that promise.
        // act waits out the rejection so the dialog state lands inside the test.
        await act(async () => {
            fireEvent.click(screen.getByTestId("file-companion-arxiv-go"));
        });
    }

    it("imports a companion share from the open box", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await waitFor(() => expect(importShare).toHaveBeenCalledWith(shareLink, ""));
        await screen.findByTestId("file-companion-tab-shared.pdf");
        expect(open).toHaveBeenCalledWith("C:/library/shared.pdf");
        expect(openWeb).not.toHaveBeenCalled();
        expect(openArxiv).not.toHaveBeenCalled();
        expect(screen.getByRole("alert").textContent).toContain("已导入文档库");
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        expect(screen.queryByTestId("file-companion-share-password")).toBeNull();
    });

    it("restores an imported project's notes and drawings from the open box", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path));
        const annotation = { id: "a1", kind: "annotation" as const, text: "原文", translation: "导入的批注", parts: [{ page: 1, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }] }] };
        const drawing = { id: "d1", page: 1, color: "green" as const, tool: "box" as const, points: [{ x: 0.1, y: 0.1 }, { x: 0.5, y: 0.4 }] };
        importShare.mockResolvedValue({
            id: "cd_0123456789abcdef0123456789abcdef",
            name: "shared.pdf",
            path: "C:/library/shared.pdf",
            source: "cloud",
            highlights: "[]",
            chatMarks: "[]",
            annotations: JSON.stringify([annotation]),
            drawings: JSON.stringify([drawing]),
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await screen.findByTestId("file-companion-tab-shared.pdf");
        expect(loadPdfTranslations("C:/library/shared.pdf")[0]).toMatchObject({ id: "a1", shared: true });
        expect(loadPdfDrawings("C:/library/shared.pdf")[0]).toMatchObject({ id: "d1", tool: "box", shared: true });
    });

    it("asks for a share password, then imports with it", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path));
        importShare.mockImplementation(async (_link: string, password: string) => {
            if (password !== "secret") throw new Error("需要分享密码");
            return { id: "cd_0123456789abcdef0123456789abcdef", name: "shared.pdf", path: "C:/library/shared.pdf", source: "cloud" };
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        const box = await screen.findByTestId("file-companion-share-password");
        expect(box.getAttribute("role")).toBe("dialog");
        expect(open).not.toHaveBeenCalled();
        expect(importShare).toHaveBeenCalledTimes(1);
        const input = screen.getByTestId("file-companion-share-password-input") as HTMLInputElement;
        expect(input.type).toBe("password");
        expect(screen.getByRole("heading", { name: "输入分享密码" })).toBeTruthy();
        expect(screen.getByText("该文档为私密分享，需要密码才能导入。")).toBeTruthy();
        expect(input.autocomplete).toBe("new-password");
        const reveal = screen.getByRole("button", { name: "显示密码" });
        const press = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
        reveal.dispatchEvent(press);
        expect(press.defaultPrevented).toBe(true);
        reveal.focus();
        fireEvent.click(reveal);
        expect(input.type).toBe("text");
        expect(document.activeElement).toBe(input);
        fireEvent.click(screen.getByRole("button", { name: "隐藏密码" }));
        expect(input.type).toBe("password");
        expect(document.activeElement).toBe(input);
        fireEvent.click(screen.getByTestId("file-companion-share-password-confirm"));
        expect(screen.getByTestId("file-companion-share-password-error").textContent).toContain("请输入分享密码");
        expect(importShare).toHaveBeenCalledTimes(1);
        fireEvent.change(input, { target: { value: "secret" } });
        await act(async () => {
            fireEvent.click(screen.getByTestId("file-companion-share-password-confirm"));
        });
        await waitFor(() => expect(importShare).toHaveBeenCalledWith(shareLink, "secret"));
        await screen.findByTestId("file-companion-tab-shared.pdf");
        expect(screen.queryByTestId("file-companion-share-password")).toBeNull();
    });

    it("keeps the password box open when the share password is wrong", async () => {
        boot.mockResolvedValue({ paths: [] });
        importShare.mockImplementation(async (_link: string, password: string) => {
            if (!password) throw new Error("需要分享密码");
            throw new Error("分享密码不正确");
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await screen.findByTestId("file-companion-share-password");
        fireEvent.change(screen.getByTestId("file-companion-share-password-input"), { target: { value: "nope" } });
        await act(async () => {
            fireEvent.click(screen.getByTestId("file-companion-share-password-confirm"));
        });
        await screen.findByTestId("file-companion-share-password-error");
        expect(screen.getByTestId("file-companion-share-password-error").textContent).toContain("分享密码不正确");
        expect(screen.getByTestId("file-companion-share-password")).toBeTruthy();
        expect(open).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
    });

    it("keeps the caret in the password field while the import is in flight", async () => {
        let rejectImport: (err: unknown) => void = () => undefined;
        boot.mockResolvedValue({ paths: [] });
        importShare.mockImplementation((_link: string, password: string) => {
            if (!password) return Promise.reject(new Error("需要分享密码"));
            return new Promise((_resolve, reject) => { rejectImport = reject; });
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        const input = await screen.findByTestId("file-companion-share-password-input") as HTMLInputElement;
        fireEvent.change(input, { target: { value: "nope" } });
        fireEvent.click(screen.getByTestId("file-companion-share-password-confirm"));
        await waitFor(() => expect(input.readOnly).toBe(true));
        expect(input.disabled).toBe(false);
        expect(document.activeElement).toBe(input);
        fireEvent.keyDown(window, { key: "Tab" });
        expect(document.activeElement).toBe(input);
        expect(screen.getByTestId("file-companion-arxiv-input")).not.toBe(document.activeElement);
        fireEvent.keyDown(input, { key: "Escape" });
        expect(screen.getByTestId("file-companion-share-password")).toBeTruthy();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(importShare).toHaveBeenCalledTimes(2);
        await act(async () => { rejectImport(new Error("分享密码不正确")); });
        await screen.findByTestId("file-companion-share-password-error");
        expect(input.readOnly).toBe(false);
        expect(document.activeElement).toBe(input);
        expect(input.value).toBe("nope");
    });

    it("closes the password box on Escape without a second import", async () => {
        boot.mockResolvedValue({ paths: [] });
        importShare.mockRejectedValue(new Error("需要分享密码"));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await screen.findByTestId("file-companion-share-password");
        fireEvent.keyDown(screen.getByTestId("file-companion-share-password-input"), { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("file-companion-share-password")).toBeNull());
        expect(importShare).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(open).not.toHaveBeenCalled();
        expect(document.activeElement).toBe(screen.getByTestId("file-companion-arxiv-input"));
    });

    // Wails rejects a Go error as a string, not an Error. An Error-only
    // check would show 导入失败 and never ask for the password.
    it("opens the password box when the host rejects with the password sentence as text", async () => {
        boot.mockResolvedValue({ paths: [] });
        importShare.mockRejectedValueOnce("需要分享密码");
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await screen.findByTestId("file-companion-share-password");
        expect(screen.getByTestId("file-companion-arxiv-hint").textContent).not.toContain("导入失败");
        fireEvent.change(screen.getByTestId("file-companion-share-password-input"), { target: { value: "nope" } });
        importShare.mockRejectedValueOnce("分享密码不正确");
        await act(async () => {
            fireEvent.click(screen.getByTestId("file-companion-share-password-confirm"));
        });
        await screen.findByTestId("file-companion-share-password-error");
        expect(screen.getByTestId("file-companion-share-password-error").textContent).toContain("分享密码不正确");
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(open).not.toHaveBeenCalled();
    });

    it("shows a text share failure on the open box", async () => {
        boot.mockResolvedValue({ paths: [] });
        importShare.mockRejectedValueOnce("暂时无法连接云端");
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-hint").textContent).toContain("暂时无法连接云端");
        });
        expect(screen.queryByTestId("file-companion-share-password")).toBeNull();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(open).not.toHaveBeenCalled();
    });

    it("keeps the import when opening the saved file throws", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockResolvedValue(undefined);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-hint").textContent).toContain("文件已导入，但没有打开");
        });
        expect(screen.getByRole("alert").textContent).toContain("已导入文档库");
        expect(screen.queryByTestId("file-companion-share-password")).toBeNull();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-tab-shared.pdf")).toBeNull();
    });

    it("keeps the password box over the chat while a native player is showing", async () => {
        boot.mockResolvedValue({ paths: [] });
        importShare.mockRejectedValueOnce("需要分享密码");
        installRuntime();
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
        await pasteOpenBox(shareLink);
        const box = await screen.findByTestId("file-companion-share-password");
        expect(box.className).toContain("file-companion-share-password--beside-player");
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
    });

    it("keeps a second import from the url box while the password box is open", async () => {
        boot.mockResolvedValue({ paths: [] });
        importShare.mockRejectedValue("需要分享密码");
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await screen.findByTestId("file-companion-share-password");
        fireEvent.keyDown(screen.getByTestId("file-companion-arxiv-input"), { key: "Enter" });
        expect(importShare).toHaveBeenCalledTimes(1);
        fireEvent.keyDown(screen.getByTestId("file-companion-share-password-input"), { key: "Tab", shiftKey: true });
        expect(document.activeElement).toBe(screen.getByTestId("file-companion-share-password-confirm"));
    });

    it("shows the host sentence when opening the imported file is rejected as text", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockRejectedValue("文件已被占用");
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        await pasteOpenBox(shareLink);
        await screen.findByText("文件已被占用");
        expect(screen.getByRole("alert").textContent).toContain("已导入文档库");
        expect(screen.queryByText("打不开这个文件")).toBeNull();
        expect(screen.queryByTestId("file-companion-share-password")).toBeNull();
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

    async function openArxivSearchDialog() {
        if (!screen.queryByTestId("file-companion-open-menu")) {
            fireEvent.click(screen.getByTestId("file-companion-choose"));
        }
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search"));
        return screen.findByTestId("file-companion-arxiv-search-dialog");
    }

    it("opens an arXiv search dialog from the button beside 确定", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-input"), { target: { value: "transformer" } });
        const dialog = await openArxivSearchDialog();
        expect(dialog.getAttribute("role")).toBe("dialog");
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect((screen.getByTestId("file-companion-arxiv-search-input") as HTMLInputElement).value).toBe("transformer");
        expect(dialog.textContent).toContain("描述研究目标");
        expect(dialog.textContent).not.toContain("按相关度");
        expect(searchArxiv).not.toHaveBeenCalled();
        expect(openArxiv).not.toHaveBeenCalled();
    });

    it("asks for a keyword before searching arXiv", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("请描述研究目标或输入关键字");
        });
        expect(searchArxiv).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
    });

    it("says it is searching while the arXiv request is in flight", async () => {
        boot.mockResolvedValue({ paths: [] });
        let release: (hits: unknown) => void = () => undefined;
        searchArxiv.mockReturnValueOnce(new Promise((resolve) => { release = resolve; }));
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("正在理解研究目标并检索");
        });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).not.toContain("输入关键字");
        expect(screen.getByTestId("file-companion-arxiv-search-skeleton")).toBeTruthy();
        release([]);
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("检索完成，没有结果");
        });
        expect(screen.queryByTestId("file-companion-arxiv-search-skeleton")).toBeNull();
    });

    it("says when an arXiv search has no papers, and clears that once the keyword changes", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockResolvedValueOnce([]);
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "zzzz-not-a-paper" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("检索完成，没有结果");
        });
        const empty = screen.getByTestId("file-companion-arxiv-search-empty");
        expect(empty.textContent).toContain("后检索");
        expect(empty.textContent).not.toContain("确定");
        expect(screen.queryByRole("list", { name: "搜索结果" })).toBeNull();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("描述研究目标");
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).not.toContain("检索完成，没有结果");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "zzzz-not-a-paper" } });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("检索完成，没有结果");
    });

    it("lists arXiv titles and abstracts, and opens the paper that is clicked", async () => {
        boot.mockResolvedValue({ paths: [] });
        open.mockImplementation(async (path: string) => doc(path, { editable: false, readOnly: true }));
        searchArxiv.mockResolvedValue([
            { id: "2510.16079", title: "Attention Is All You Need", abstract: "We propose a new network architecture.", authors: "Ashish Vaswani" },
            { id: "cs/0701001", title: "An Old Paper", abstract: "From the archive.", authors: "Grace Hopper" },
        ]);
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await waitFor(() => expect(searchArxiv).toHaveBeenCalledWith("attention", 1, true));
        const first = await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        expect(first.textContent).toContain("Attention Is All You Need");
        expect(first.textContent).toContain("We propose a new network architecture.");
        expect(screen.getByTestId("file-companion-arxiv-hit-cs_0701001").textContent).toContain("From the archive.");
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("约 2 篇");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention mechanism" } });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("目标已修改");
        expect(screen.getByTestId("file-companion-arxiv-hit-2510.16079")).toBeTruthy();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("约 2 篇");
        fireEvent.click(first);
        await waitFor(() => expect(openArxiv).toHaveBeenCalledWith("2510.16079"));
        await screen.findByTestId("file-companion-tab-2510.16079.pdf");
        expect(open).toHaveBeenCalledWith("C:/papers/2510.16079.pdf");
        expect(screen.queryByTestId("file-companion-arxiv-search-dialog")).toBeNull();
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
    });

    it("shows the citation line of each hit: id, subjects, and date", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockResolvedValue([
            {
                id: "2510.16079",
                title: "Attention Is All You Need",
                abstract: "We propose a new network architecture.",
                authors: "Ashish Vaswani",
                published: "2025-10-20",
                categories: ["cs.CL", "cs.LG"],
            },
        ]);
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        const hit = await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        expect(hit.textContent).toContain("2510.16079");
        expect(hit.textContent).toContain("cs.CL");
        expect(hit.textContent).toContain("cs.LG");
        expect(hit.textContent).toContain("2025-10-20");
        // The row is numbered, so the list reads as a bibliography.
        expect(hit.querySelector(".file-companion-arxiv-hit-index")?.textContent).toBe("1");
    });

    it("pages a research goal twenty at a time and keeps the list while the next page loads", async () => {
        boot.mockResolvedValue({ paths: [] });
        let releasePage: ((value: unknown) => void) | undefined;
        searchArxiv.mockImplementation(async (_query: string, page: number) => {
            if (page === 2) {
                return new Promise((resolve) => { releasePage = resolve; });
            }
            return {
                hits: [{ id: "2510.16079", title: "Attention Is All You Need", abstract: "Abstract.", authors: "Ashish Vaswani" }],
                page: 1,
                pageSize: 20,
                total: 40,
                interpreted: "注意力机制",
                groups: [{ terms: ["attention", "transformer"], category: "cs.CL" }],
                fallback: false,
            };
        });
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "我想找注意力机制的论文" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("约 40 篇，第 1 / 2 页");
        expect(screen.getByTestId("file-companion-arxiv-search-groups").textContent).toContain("attention · transformer");
        expect(screen.getByTestId("file-companion-arxiv-search-groups").textContent).toContain("cs.CL");
        expect(screen.getByTestId("file-companion-arxiv-search-groups").textContent).toContain("注意力机制");
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-next"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("正在检索");
        });
        expect(screen.getByTestId("file-companion-arxiv-hit-2510.16079")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-arxiv-search-skeleton")).toBeNull();
        // The rows stay up while the next page loads, and they must not start
        // a download that the new list is about to throw away.
        fireEvent.click(screen.getByTestId("file-companion-arxiv-hit-2510.16079"));
        expect(openArxiv).not.toHaveBeenCalled();
        expect(searchArxiv).toHaveBeenLastCalledWith("我想找注意力机制的论文", 2, false);
        await act(async () => {
            releasePage?.({
                hits: [{ id: "1706.03762", title: "Second Page", abstract: "More.", authors: "A" }],
                page: 2,
                pageSize: 20,
                total: 40,
                interpreted: "注意力机制",
                groups: [{ terms: ["attention", "transformer"], category: "cs.CL" }],
            });
        });
        const second = await screen.findByTestId("file-companion-arxiv-hit-1706.03762");
        expect(second.querySelector(".file-companion-arxiv-hit-index")?.textContent).toBe("21");
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("第 2 / 2 页");
        expect(screen.queryByTestId("file-companion-arxiv-hit-2510.16079")).toBeNull();
        const scroller = screen.getByRole("list", { name: "搜索结果" }).parentElement as HTMLElement;
        scroller.scrollTop = 180;
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-prev"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        expect(scroller.scrollTop).toBe(0);
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "换一个目标" } });
        expect((screen.getByTestId("file-companion-arxiv-search-next") as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId("file-companion-arxiv-search-prev") as HTMLButtonElement).disabled).toBe(true);
    });

    it("keeps the pager when a later page comes back empty", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockImplementation(async (_query: string, page: number) => {
            if (page === 2) {
                return { hits: [], page: 2, pageSize: 20, total: 20, fallback: false };
            }
            return {
                hits: [{ id: "2510.16079", title: "First Page", abstract: "Abstract.", authors: "A" }],
                page: 1,
                pageSize: 20,
                total: 21,
                fallback: false,
            };
        });
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-next"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("这一页没有论文");
        });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).not.toContain("第 1 / 1 页");
        expect(screen.getByTestId("file-companion-arxiv-search-empty").textContent).toContain("回到上一页");
        expect((screen.getByTestId("file-companion-arxiv-search-prev") as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByTestId("file-companion-arxiv-search-next") as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention mechanism" } });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("目标已修改，请重新检索");
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).not.toContain("描述研究目标");
        expect(screen.queryByTestId("file-companion-arxiv-search-empty")).toBeNull();
        expect((screen.getByTestId("file-companion-arxiv-search-prev") as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("这一页没有论文");
        expect((screen.getByTestId("file-companion-arxiv-search-prev") as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-prev"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        expect(searchArxiv).toHaveBeenLastCalledWith("attention", 1, false);
    });

    it("drops a cleared search instead of calling the empty box a changed goal", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockImplementation(async (_query: string, page: number) => {
            if (page === 2) {
                return {
                    hits: [{ id: "1706.03762", title: "Second Page", abstract: "More.", authors: "A" }],
                    page: 2,
                    pageSize: 20,
                    total: 40,
                };
            }
            return {
                hits: [{ id: "2510.16079", title: "First Page", abstract: "Abstract.", authors: "A" }],
                page: 1,
                pageSize: 20,
                total: 40,
            };
        });
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-next"));
        await screen.findByTestId("file-companion-arxiv-hit-1706.03762");
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "   " } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("请描述研究目标或输入关键字");
        expect(screen.queryByTestId("file-companion-arxiv-hit-1706.03762")).toBeNull();
        expect(screen.queryByTestId("file-companion-arxiv-search-pager")).toBeNull();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "a" } });
        const status = screen.getByTestId("file-companion-arxiv-search-status").textContent ?? "";
        expect(status).toContain("描述研究目标");
        expect(status).not.toContain("目标已修改");
    });

    it("says when the goal could not be expanded and the typed words were used", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockResolvedValueOnce({
            hits: [{ id: "2510.16079", title: "Attention Is All You Need", abstract: "Abstract.", authors: "A" }],
            page: 1,
            pageSize: 20,
            total: 1,
            groups: [{ terms: ["注意力"] }],
            fallback: true,
        });
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "注意力" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        const status = screen.getByTestId("file-companion-arxiv-search-status").textContent ?? "";
        expect(status).toContain("未能扩展关键词，已用原文中的英文词检索");
        expect(status).toContain("约 1 篇");
        expect(screen.getByTestId("file-companion-arxiv-search-groups").textContent).toContain("注意力");
    });

    it("lists a hit that carries no date or subjects", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockResolvedValue([
            { id: "cs/0701001", title: "An Old Paper", abstract: "From the archive.", authors: "Grace Hopper" },
        ]);
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "archive" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        const hit = await screen.findByTestId("file-companion-arxiv-hit-cs_0701001");
        expect(hit.textContent).toContain("An Old Paper");
        expect(hit.textContent).toContain("cs/0701001");
        expect(hit.querySelectorAll(".file-companion-arxiv-hit-tag").length).toBe(0);
        expect(hit.querySelector(".file-companion-arxiv-hit-date")).toBeNull();
    });

    it("holds the list's shape with placeholder rows while a search runs", async () => {
        boot.mockResolvedValue({ paths: [] });
        let resolveSearch: ((value: unknown) => void) | undefined;
        searchArxiv.mockImplementationOnce(() => new Promise((resolve) => { resolveSearch = resolve; }));
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        const skeleton = await screen.findByTestId("file-companion-arxiv-search-skeleton");
        // Placeholders are decoration, not content: a screen reader must not
        // announce four empty rows.
        expect(skeleton.getAttribute("aria-hidden")).toBe("true");
        expect(screen.getByTestId("file-companion-arxiv-search-go").textContent).toBe("检索中…");
        await act(async () => { resolveSearch?.([]); });
        expect(screen.queryByTestId("file-companion-arxiv-search-skeleton")).toBeNull();
    });

    it("offers a next step when a search finds nothing", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockResolvedValueOnce([]);
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "zzzz-not-a-paper" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        const empty = await screen.findByTestId("file-companion-arxiv-search-empty");
        // The panel names the query that came back empty, so the reader can
        // see what was actually searched instead of a second "no results".
        expect(empty.textContent).toContain("没有与「zzzz-not-a-paper」匹配的论文");
        expect(empty.textContent).toContain("arXiv 编号");
        // The empty state replaces the list, and goes away once a search runs.
        expect(screen.queryByRole("list", { name: "搜索结果" })).toBeNull();
        // It also goes away on a keystroke: it describes the query it came
        // from, and the status line has already returned to its idle sentence.
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        expect(screen.queryByTestId("file-companion-arxiv-search-empty")).toBeNull();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "zzzz-not-a-paper" } });
        searchArxiv.mockResolvedValueOnce([
            { id: "2510.16079", title: "Attention Is All You Need", abstract: "Abstract.", authors: "Ashish Vaswani" },
        ]);
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        expect(screen.queryByTestId("file-companion-arxiv-search-empty")).toBeNull();
    });

    it("keeps the search dialog open when arXiv search fails", async () => {
        boot.mockResolvedValue({ paths: [] });
        searchArxiv.mockRejectedValueOnce(new Error("搜索失败：连接 arXiv 超时，请稍后再试"));
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("超时");
        });
        expect(screen.getByTestId("file-companion-arxiv-search-status").dataset.state).toBe("error");
        expect(screen.getByTestId("file-companion-arxiv-search-dialog")).toBeTruthy();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(openArxiv).not.toHaveBeenCalled();
    });

    it("keeps the search dialog open when the chosen paper does not download", async () => {
        boot.mockResolvedValue({ paths: [] });
        openArxiv.mockRejectedValueOnce(new Error("下载失败：找不到这篇论文（404）"));
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        fireEvent.change(screen.getByTestId("file-companion-arxiv-search-input"), { target: { value: "attention" } });
        fireEvent.click(screen.getByTestId("file-companion-arxiv-search-go"));
        const first = await screen.findByTestId("file-companion-arxiv-hit-2510.16079");
        fireEvent.click(first);
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-arxiv-search-status").textContent).toContain("404");
        });
        expect(screen.getByTestId("file-companion-arxiv-search-dialog")).toBeTruthy();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(open).not.toHaveBeenCalled();
    });

    it("closes arXiv search on Escape and leaves the open menu up", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await openArxivSearchDialog();
        const search = screen.getByTestId("file-companion-arxiv-search");
        fireEvent.keyDown(screen.getByTestId("file-companion-arxiv-search-input"), { key: "Escape" });
        expect(screen.queryByTestId("file-companion-arxiv-search-dialog")).toBeNull();
        expect(screen.getByTestId("file-companion-open-menu")).toBeTruthy();
        expect(document.activeElement).toBe(search);
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

    it("fills the companion window from the title bar and restores that size", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        const button = screen.getByTestId("file-companion-fullscreen");
        const bar = screen.getByTestId("file-companion-titlebar");
        const gap = screen.getByTestId("file-companion-titlebar-gap");
        const close = screen.getByTestId("file-companion-close");
        expect(button.parentElement).toBe(bar);
        expect(gap.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(button.compareDocumentPosition(close) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(button.textContent).toBe("全屏");
        expect(button.getAttribute("title")).toBe("全屏阅读");
        expect(button.getAttribute("aria-pressed")).toBe("false");
        expect(button.querySelector("svg.file-companion-tool-icon")?.getAttribute("data-icon")).toBe("fullscreen");
        expect(button.querySelector("svg.file-companion-tool-icon")?.getAttribute("aria-hidden")).toBe("true");
        fireEvent.click(button);
        await waitFor(() => expect(windowFullscreen).toHaveBeenCalledTimes(1));
        expect(windowGetSize).toHaveBeenCalled();
        expect(windowGetPosition).toHaveBeenCalled();
        expect(windowUnfullscreen).not.toHaveBeenCalled();
        expect(hideWindow).not.toHaveBeenCalled();
        const expanded = screen.getByTestId("file-companion-fullscreen");
        expect(expanded.textContent).toBe("恢复原始大小");
        expect(expanded.getAttribute("title")).toBe("恢复进入全屏前的窗口大小");
        expect(expanded.getAttribute("aria-pressed")).toBe("true");
        expect(expanded.querySelector("svg.file-companion-tool-icon")?.getAttribute("data-icon")).toBe("restore");
        fireEvent.click(expanded);
        await waitFor(() => expect(windowUnfullscreen).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(windowSetSize).toHaveBeenCalledWith(1280, 800));
        expect(setScreenPosition).toHaveBeenCalledWith(40, 30);
        expect(screen.getByTestId("file-companion-fullscreen").textContent).toBe("全屏");
        fireEvent.keyDown(window, { key: "F11" });
        await waitFor(() => expect(windowFullscreen).toHaveBeenCalledTimes(2));
        fireEvent.keyDown(window, { key: "Escape" });
        await waitFor(() => expect(windowUnfullscreen).toHaveBeenCalledTimes(2));
        expect(screen.getByTestId("file-companion-fullscreen").textContent).toBe("全屏");
        // The restore helper writes the saved size again after the OS settles.
        await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    });

    it("lists local and cloud documents and can share or import one", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        listLibrary.mockResolvedValue({
            items: [
                { id: "local:a", name: "a.md", path: "C:/notes/a.md", source: "local", openedAt: "2026-10-25T00:00:00Z" },
                { id: "cd_0123456789abcdef0123456789abcdef", name: "shared.pdf", source: "cloud", shareUrl: "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", owner: "other@example.com", visibility: "public", permanent: true, mine: false },
            ],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        const choose = screen.getByTestId("file-companion-choose");
        const library = screen.getByTestId("file-companion-library");
        const gap = screen.getByTestId("file-companion-titlebar-gap");
        expect(library.textContent).toContain("文档管理");
        expect(choose.compareDocumentPosition(library) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(library.compareDocumentPosition(gap) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        fireEvent.click(library);
        await screen.findByTestId("file-companion-library-panel");
        await waitFor(() => expect(screen.getByTestId("file-companion-library-local").textContent).toContain("a.md"));
        expect(screen.getByTestId("file-companion-library-cloud").textContent).toContain("shared.pdf");
        expect(screen.getByTestId("file-companion-library-cloud").textContent).toContain("https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef");
        // 分享 is its own popup, opened from the row or the title bar.
        fireEvent.click(screen.getByTestId("file-companion-library-share-action-local:a"));
        await screen.findByTestId("file-companion-share-dialog");
        fireEvent.click(screen.getByTestId("file-companion-share-private"));
        fireEvent.change(screen.getByTestId("file-companion-share-password"), { target: { value: "secret" } });
        fireEvent.click(screen.getByTestId("file-companion-share-months"));
        fireEvent.change(screen.getByTestId("file-companion-share-amount"), { target: { value: "2" } });
        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        await waitFor(() => expect(shareDocument).toHaveBeenCalledWith("C:/notes/a.md", "private", "secret", "", JSON.stringify({ highlights: [], chatMarks: [], annotations: [], drawings: [] }), 60));
        expect(screen.getByTestId("file-companion-library-link").textContent).toContain("/hub/documents/shares/");
        fireEvent.click(screen.getByTestId("file-companion-share-dialog-close"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-share-dialog")).toBeNull());
        // 导入分享 is its own popup too.
        fireEvent.click(screen.getByTestId("file-companion-library-import-open"));
        await screen.findByTestId("file-companion-import-dialog");
        fireEvent.change(screen.getByTestId("file-companion-library-import-link"), { target: { value: "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef" } });
        fireEvent.change(screen.getByTestId("file-companion-library-import-password"), { target: { value: "secret" } });
        fireEvent.click(screen.getByTestId("file-companion-library-import"));
        await waitFor(() => expect(importShare).toHaveBeenCalledWith("https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", "secret"));
        await waitFor(() => expect(open).toHaveBeenCalledWith("C:/library/shared.pdf"));
    });

    it("opens 文档管理 with no document open and lists accepted shares as projects", async () => {
        boot.mockResolvedValue({ paths: [] });
        listLibrary.mockResolvedValue({
            items: [
                { id: "local:a", name: "a.md", path: "C:/notes/a.md", source: "local", openedAt: "2026-10-25T00:00:00Z", size: 2048 },
                { id: "cd_0123456789abcdef0123456789abcdef", name: "shared.pdf", path: "C:/library/shared.pdf", source: "cloud", owner: "other@example.com", visibility: "group", permanent: true, mine: false, size: 4096, notes: [{ id: "n1", author: "x", text: "不错" }] },
            ],
            notice: "",
        });
        installRuntime();
        renderWindow();
        // No document is open, so the empty hint is what the reader sees.
        await screen.findByTestId("file-companion-empty");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        // Each row is one project: the document, its annotations, its discussion.
        const localCard = await screen.findByTestId("file-companion-library-card-local:a");
        expect(localCard.textContent).toContain("a.md");
        expect(localCard.textContent).toContain("本机");
        expect(localCard.textContent).toContain("2 KB");
        const cloudCard = screen.getByTestId("file-companion-library-card-cd_0123456789abcdef0123456789abcdef");
        expect(cloudCard.textContent).toContain("shared.pdf");
        expect(cloudCard.textContent).toContain("云端");
        expect(cloudCard.textContent).toContain("群组");
        expect(cloudCard.textContent).toContain("讨论 1");
        expect(cloudCard.textContent).toContain("4 KB");
        // 导出项目 is per project, not per open tab.
        fireEvent.click(screen.getByTestId("file-companion-library-export-local:a"));
        await waitFor(() => expect(exportProject).toHaveBeenCalledWith("C:/notes/a.md", expect.any(String)));
    });

    it("the library counts only reader notes still waiting for the host", async () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{
                id: token,
                token,
                name: "shared.pdf",
                path: "C:/library/shared.pdf",
                source: "cloud",
                owner: "me@example.com",
                visibility: "group",
                permanent: true,
                mine: true,
                notesRevision: 3,
                notes: [
                    { id: "cn_pending", author: "reader@example.com", text: "待审核", createdAt: "2026-10-25T00:00:00Z", decision: "pending" },
                    { id: "cn_approved", author: "reader@example.com", text: "已通过", createdAt: "2026-10-25T00:01:00Z", decision: "approved" },
                    { id: "cn_ignored", author: "reader@example.com", text: "已忽略", createdAt: "2026-10-25T00:02:00Z", decision: "ignored" },
                    { id: "cn_host", author: "me@example.com", text: "主持人回答", createdAt: "2026-10-25T00:03:00Z", host: true, replyTo: "cn_approved" },
                ],
            }],
            notice: "",
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-empty");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        expect((await screen.findByTestId(`file-companion-library-pending-${token}`)).textContent).toContain("待审核 1");
    });

    it("marks an already shared cloud project instead of offering 分享 again, and edits that link", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [
                { id: "local:a", name: "论文.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" },
                {
                    id: token,
                    token,
                    name: "论文.pdf",
                    path: "C:/notes/a.pdf",
                    source: "cloud",
                    owner: "me@example.com",
                    visibility: "public",
                    permanent: false,
                    expiresAt: "2026-11-24T00:00:00Z",
                    shareUrl: `https://hub.example/hub/documents/shares/${token}`,
                    openedAt: "2026-10-25T00:00:00Z",
                    mine: true,
                },
            ],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        // The title bar reads the same share: 修改分享, not 分享.
        const shareTool = screen.getByTestId("file-companion-share-open");
        expect(shareTool.getAttribute("aria-label")).toBe("修改分享");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");

        // A cloud row is already a share: it carries a mark, not a button.
        expect(screen.getByTestId(`file-companion-library-shared-mark-${token}`).textContent).toContain("已分享");
        expect(screen.queryByTestId(`file-companion-library-share-action-${token}`)).toBeNull();
        // The local row for the same paper is shared too, so it must not
        // offer a second 分享 either.
        expect(screen.getByTestId("file-companion-library-shared-mark-local:a").textContent).toContain("已分享");
        expect(screen.queryByTestId("file-companion-library-share-action-local:a")).toBeNull();

        // 修改 opens on the link that already exists.
        fireEvent.click(screen.getByTestId(`file-companion-library-edit-share-${token}`));
        const dialog = await screen.findByTestId("file-companion-share-dialog");
        expect(dialog.textContent).toContain("修改分享");
        expect(dialog.textContent).toContain("当前 公开 · 至 2026-11-24");
        expect((screen.getByTestId("file-companion-share-public") as HTMLButtonElement).getAttribute("aria-pressed")).toBe("true");
        expect(screen.getByTestId("file-companion-library-link").textContent).toContain(token);

        // Changing only the audience must not resend the expiry: -1 keeps it.
        fireEvent.click(screen.getByTestId("file-companion-share-private"));
        fireEvent.change(screen.getByTestId("file-companion-share-password"), { target: { value: "secret" } });
        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        await waitFor(() => expect(updateShare).toHaveBeenCalledWith(token, "private", "secret", "", -1));
        // The header echoes what was just saved, not the snapshot it opened on.
        await waitFor(() => expect(screen.getByTestId("file-companion-share-dialog").textContent).toContain("当前 私密 · 至 2026-11-24"));

        // Touching 有效期 does send the new one.
        updateShare.mockClear();
        fireEvent.click(screen.getByTestId("file-companion-share-forever"));
        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        await waitFor(() => expect(updateShare).toHaveBeenCalledWith(token, "private", "", "", 0));
        // 修改 does not publish a second link.
        expect(shareDocument).not.toHaveBeenCalled();
    });

    it("toasts a successful share and reports an already shared paper and range", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        listLibrary.mockResolvedValue({
            items: [{ id: "local:a", name: "a.md", path: "C:/notes/a.md", source: "local", openedAt: "2026-10-25T00:00:00Z" }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-share-open"));
        await screen.findByTestId("file-companion-share-dialog");

        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        await waitFor(() => expect(shareDocument).toHaveBeenCalled());
        expect(await screen.findByText("分享成功，链接已生成")).toBeTruthy();

        shareDocument.mockRejectedValueOnce(new Error("这份文档已经以相同的范围分享过了，请先删除原分享，或修改原分享"));
        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        expect(await screen.findByText("这份文档已经以相同的范围分享过了，请先删除原分享，或修改原分享")).toBeTruthy();
    });

    it("offers sharing again after the previous share expired", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [
                { id: "local:a", name: "a.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" },
                {
                    id: token,
                    token,
                    name: "a.pdf",
                    path: "C:/notes/a.pdf",
                    source: "cloud",
                    owner: "me@example.com",
                    visibility: "public",
                    permanent: false,
                    expiresAt: "2026-01-01T00:00:00Z",
                    expired: true,
                    mine: true,
                },
            ],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        // The title bar reads an expired link as no live share.
        expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("分享");

        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        const expiredMark = await screen.findByTestId(`file-companion-library-expired-mark-${token}`);
        expect(expiredMark.textContent).toContain("已过期");
        // The local row offers 分享 again, and 修改 is gone with the lapsed link.
        expect(screen.getByTestId("file-companion-library-share-action-local:a")).toBeTruthy();
        expect(screen.queryByTestId(`file-companion-library-edit-share-${token}`)).toBeNull();
    });

    it("repaints an open 文档管理 when a poll finds the share expired", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        const localItem = { id: "local:a", name: "a.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        const cloudItem = {
            id: token,
            token,
            name: "a.pdf",
            path: "C:/notes/a.pdf",
            source: "cloud",
            owner: "me@example.com",
            visibility: "public",
            permanent: false,
            expiresAt: "2026-11-24T00:00:00Z",
            mine: true,
            openedAt: "2026-10-25T00:00:00Z",
        };
        listLibrary.mockResolvedValue({ items: [localItem, cloudItem], notice: "" });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        expect(await screen.findByTestId(`file-companion-library-edit-share-${token}`)).toBeTruthy();

        // The same row arrives from a later poll with the link lapsed. The
        // open list must follow without being reopened.
        listLibrary.mockResolvedValue({
            items: [localItem, { ...cloudItem, expiresAt: "2026-01-01T00:00:00Z", expired: true }],
            notice: "",
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        const expiredMark = await screen.findByTestId(`file-companion-library-expired-mark-${token}`);
        expect(expiredMark.textContent).toContain("已过期");
        expect(screen.queryByTestId(`file-companion-library-edit-share-${token}`)).toBeNull();
    });

    it("transfers the host seat from 修改分享 and waits for acceptance", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        const localItem = { id: "local:a", name: "a.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        const cloudItem = {
            id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud",
            owner: "me@example.com", visibility: "group", permanent: true, mine: true,
            admin: true, host: true, hostLabel: "me@example.com", openedAt: "2026-10-25T00:00:00Z",
        };
        listLibrary.mockResolvedValue({ items: [localItem, cloudItem], notice: "" });
        installRuntime();
        render(
            <DialogProvider>
                <ToastProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </ToastProvider>
            </DialogProvider>,
        );
        await screen.findByTestId("file-companion-tab-a.pdf");
        fireEvent.click(screen.getByTestId("file-companion-share-open"));
        const dialog = await screen.findByTestId("file-companion-share-dialog");
        expect(within(dialog).getByTestId("file-companion-share-host-current").textContent).toContain("me@example.com");
        fireEvent.change(within(dialog).getByTestId("file-companion-share-host-target"), { target: { value: "reader@example.com" } });
        transferHost.mockImplementation(async () => {
            listLibrary.mockResolvedValue({
                items: [localItem, { ...cloudItem, pendingHost: true, pendingHostLabel: "reader@example.com" }],
                notice: "",
            });
        });
        fireEvent.click(within(dialog).getByTestId("file-companion-share-host-transfer"));
        await waitFor(() => expect(transferHost).toHaveBeenCalledWith(token, "reader@example.com"));
        expect(await screen.findByTestId("file-companion-share-host-pending")).toBeTruthy();
        expect(screen.getByText("已发送主持人转移邀请，等待对方接受")).toBeTruthy();

        // Cancelling the pending nomination goes through the revoke binding.
        fireEvent.click(screen.getByTestId("file-companion-share-host-cancel"));
        const confirm = await screen.findByRole("dialog", { name: "取消转移" });
        fireEvent.click(within(confirm).getByRole("button", { name: "确认" }));
        await waitFor(() => expect(revokeHost).toHaveBeenCalledWith(token, "cancel"));
    });

    it("shows 收回 after a poll moves the host seat off this account", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        const localItem = { id: "local:a", name: "a.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        const cloudItem = {
            id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud",
            owner: "me@example.com", visibility: "group", permanent: true, mine: true,
            admin: true, host: true, hostLabel: "me@example.com", openedAt: "2026-10-25T00:00:00Z",
        };
        listLibrary.mockResolvedValue({ items: [localItem, cloudItem], notice: "" });
        installRuntime();
        render(
            <DialogProvider>
                <ToastProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </ToastProvider>
            </DialogProvider>,
        );
        await screen.findByTestId("file-companion-tab-a.pdf");
        fireEvent.click(screen.getByTestId("file-companion-share-open"));
        const dialog = await screen.findByTestId("file-companion-share-dialog");
        expect(within(dialog).queryByTestId("file-companion-share-host-revoke")).toBeNull();

        listLibrary.mockResolvedValue({
            items: [localItem, {
                ...cloudItem,
                host: undefined,
                hostKnown: true,
                hostLabel: "reader@example.com",
            }],
            notice: "",
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        expect((await within(dialog).findByTestId("file-companion-share-host-current")).textContent).toContain("reader@example.com");
        expect(within(dialog).getByTestId("file-companion-share-host-revoke")).toBeTruthy();
    });

    it("offers sync instead of 分享 to a transferred host", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_fedcba9876543210fedcba9876543210";
        listLibrary.mockResolvedValue({
            items: [{
                id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud",
                owner: "other@example.com", visibility: "group", permanent: true, mine: false,
                admin: false, host: true, hostLabel: "me@example.com", openedAt: "2026-10-25T00:00:00Z",
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        expect(await screen.findByTestId("file-companion-host-sync")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-share-open")).toBeNull();
        expect(screen.getByTestId("file-companion-host-badge").textContent).toBe("你是主持人");
    });

    it("keeps 分享 but drops 同步 for the admin who handed the seat over", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_aaaabbbbccccddddeeeeffff00001111";
        listLibrary.mockResolvedValue({
            items: [{
                id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud",
                owner: "me@example.com", visibility: "group", permanent: true, mine: true,
                admin: true, hostKnown: true, hostLabel: "new@example.com",
                openedAt: "2026-10-25T00:00:00Z",
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        // host is absent because Go omits a false flag, so only hostKnown tells
        // a transferred admin apart from a row written before host transfer
        // existed, where the creator was the host.
        expect(screen.queryByTestId("file-companion-host-sync")).toBeNull();
        await waitFor(() => expect(screen.queryByTestId("file-companion-share-open")).toBeTruthy());
    });

    it("cancels a host's cloud share from 文档管理 and keeps the local copy", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const mark = { id: "m1", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "重点" };
        savePdfMarks("C:/notes/a.pdf", [mark]);
        const token = "cd_0123456789abcdef0123456789abcdef";
        const localItem = { id: "local:a", name: "a.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        const cloudItem = {
            id: token,
            token,
            name: "a.pdf",
            path: "C:/notes/a.pdf",
            source: "cloud",
            owner: "me@example.com",
            visibility: "public",
            permanent: true,
            mine: true,
            openedAt: "2026-10-25T00:00:00Z",
        };
        listLibrary.mockResolvedValue({ items: [localItem, cloudItem], notice: "" });
        deleteShare.mockImplementation(async () => {
            listLibrary.mockResolvedValue({ items: [localItem], notice: "" });
        });
        installRuntime();
        render(
            <DialogProvider>
                <ToastProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </ToastProvider>
            </DialogProvider>,
        );
        await screen.findByTestId("file-companion-tab-a.pdf");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId(`file-companion-library-delete-${token}`);
        fireEvent.click(screen.getByTestId(`file-companion-library-delete-${token}`));
        const confirm = await screen.findByRole("dialog", { name: "取消分享" });
        expect(within(confirm).getByText(/所有参与者会从文档管理移除/)).toBeTruthy();
        fireEvent.click(within(confirm).getByRole("button", { name: "取消分享" }));

        await waitFor(() => expect(deleteShare).toHaveBeenCalledWith(token));
        expect(deleteProject).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.queryByTestId(`file-companion-library-card-${token}`)).toBeNull());
        expect(screen.getByTestId("file-companion-library-card-local:a")).toBeTruthy();
        // The local card can share the paper again; only the cloud link is gone.
        await screen.findByTestId("file-companion-library-share-action-local:a");
        // The host's own paper and marks stay; only the share is gone.
        expect(loadPdfMarks("C:/notes/a.pdf")).toEqual([mark]);
        expect(screen.getByTestId("file-companion-tab-a.pdf")).toBeTruthy();
    });

    it("deletes a share from the edit dialog after confirmation and refreshes the library", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        const localItem = { id: "local:a", name: "a.pdf", path: "C:/notes/a.pdf", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        const cloudItem = {
            id: token,
            token,
            name: "a.pdf",
            path: "C:/notes/a.pdf",
            source: "cloud",
            owner: "me@example.com",
            visibility: "public",
            permanent: true,
            mine: true,
            openedAt: "2026-10-25T00:00:00Z",
        };
        listLibrary.mockResolvedValue({ items: [localItem, cloudItem], notice: "" });
        deleteShare.mockImplementation(async () => {
            listLibrary.mockResolvedValue({ items: [localItem], notice: "" });
        });
        installRuntime();
        render(
            <DialogProvider>
                <ToastProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </ToastProvider>
            </DialogProvider>,
        );
        await screen.findByTestId("file-companion-tab-a.pdf");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId(`file-companion-library-edit-share-${token}`);
        fireEvent.click(screen.getByTestId(`file-companion-library-edit-share-${token}`));

        const dialog = await screen.findByTestId("file-companion-share-dialog");
        fireEvent.click(within(dialog).getByTestId("file-companion-share-delete"));
        const confirm = await screen.findByRole("dialog", { name: "删除分享" });
        fireEvent.click(within(confirm).getByRole("button", { name: "删除" }));

        await waitFor(() => expect(deleteShare).toHaveBeenCalledWith(token));
        await waitFor(() => expect(screen.queryByTestId("file-companion-share-dialog")).toBeNull());
        expect(await screen.findByText("已删除分享")).toBeTruthy();
        // The dialog's list is pushed into the open screen: the row is gone
        // and the local card can share the paper again.
        await waitFor(() => expect(screen.queryByTestId(`file-companion-library-card-${token}`)).toBeNull());
        expect(screen.getByTestId("file-companion-library-share-action-local:a")).toBeTruthy();
    });

    it("marks the host on the tab and names the host on a participant's screen", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf", "C:/notes/b.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const mineToken = "cd_0123456789abcdef0123456789abcdef";
        const guestToken = "cd_fedcba9876543210fedcba9876543210";
        listLibrary.mockResolvedValue({
            items: [
                { id: mineToken, token: mineToken, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true },
                { id: guestToken, token: guestToken, name: "b.pdf", path: "C:/notes/b.pdf", source: "cloud", owner: "zhang@example.com", visibility: "group", permanent: true, mine: false },
            ],
            notice: "",
        });
        installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-tab-a.pdf"));
        // The host seat shows on the tab and at the top, and the tab also
        // carries the share mark.
        const mineHost = await screen.findByTestId("file-companion-tab-host-a.pdf");
        expect(mineHost.textContent).toBe("主持");
        expect(mineHost.getAttribute("title")).toBe("你是主持人");
        expect(screen.getByTestId("file-companion-tab-share-a.pdf").getAttribute("data-state")).toBe("mine");
        await waitFor(() => expect(screen.getByTestId("file-companion-host-badge").textContent).toBe("你是主持人"));

        // A participant sees the share mark and the host named at the top; the
        // tab stays clean because the title bar already names the host.
        fireEvent.click(screen.getByTestId("file-companion-tab-b.pdf"));
        await waitFor(() => expect(screen.getByTestId("file-companion-host-badge").textContent).toBe("主持人：zhang@example.com"));
        expect(screen.queryByTestId("file-companion-tab-host-b.pdf")).toBeNull();
        expect(screen.getByTestId("file-companion-tab-share-b.pdf").getAttribute("data-state")).toBe("received");
    });

    it("pushes the host's new question to the share without waiting for 同步", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        const input = await screen.findByTestId("file-companion-input") as HTMLTextAreaElement;
        // The host seat has to be known before the send; 修改分享 is that fact.
        await waitFor(() => expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("修改分享"));
        fireEvent.change(input, { target: { value: "这个方法的效果如何" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        // The question reaches the hub on its own, not through the 同步 button.
        await waitFor(() => expect(syncShare).toHaveBeenCalled(), { timeout: 4000 });
        const args = syncShare.mock.calls[0] as unknown as [string, string];
        expect(args[0]).toBe("C:/notes/a.pdf");
        expect(JSON.parse(args[1])).toEqual({ highlights: [], chatMarks: [], annotations: [], drawings: [] });
    });

    it("pushes the host's own highlight change to the share", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await waitFor(() => expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("修改分享"));
        const mark = { id: "m1", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "重点" };
        act(() => { savePdfMarks("C:/notes/a.pdf", [mark], true); });
        await waitFor(() => expect(syncShare).toHaveBeenCalled(), { timeout: 4000 });
        const args = syncShare.mock.calls.at(-1) as unknown as [string, string];
        expect(args[0]).toBe("C:/notes/a.pdf");
        expect(JSON.parse(args[1])).toEqual({ highlights: [mark], chatMarks: [], annotations: [], drawings: [] });
    });

    it("pushes the host's answer highlight to the share", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await waitFor(() => expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("修改分享"));
        const chatMark = { message: 0, color: "yellow" as const, start: 0, end: 2, text: "重点" };
        act(() => { saveChatMarks("C:/notes/a.pdf", [chatMark], true); });
        await waitFor(() => expect(syncShare).toHaveBeenCalled(), { timeout: 4000 });
        const args = syncShare.mock.calls.at(-1) as unknown as [string, string];
        expect(args[0]).toBe("C:/notes/a.pdf");
        expect(JSON.parse(args[1])).toEqual({ highlights: [], chatMarks: [chatMark], annotations: [], drawings: [] });
    });

    it("pushes the host's placed annotation to the share", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await waitFor(() => expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("修改分享"));
        const annotation = {
            id: "a1",
            kind: "annotation" as const,
            text: "重点段落",
            translation: "这里需要复核",
            parts: [{ page: 1, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }] }],
        };
        act(() => {
            savePdfTranslations("C:/notes/a.pdf", [annotation]);
            notifyPdfAnnotationsChanged("C:/notes/a.pdf");
        });
        await waitFor(() => expect(syncShare).toHaveBeenCalled(), { timeout: 4000 });
        const args = syncShare.mock.calls.at(-1) as unknown as [string, string];
        expect(args[0]).toBe("C:/notes/a.pdf");
        expect(JSON.parse(args[1])).toEqual({ highlights: [], chatMarks: [], annotations: [annotation], drawings: [] });
    });

    it("pushes the host's freehand drawing to the share", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await waitFor(() => expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("修改分享"));
        const drawing = {
            id: "d1",
            page: 1,
            color: "yellow" as const,
            tool: "line" as const,
            points: [{ x: 0.1, y: 0.2 }, { x: 0.3, y: 0.4 }],
        };
        act(() => {
            savePdfDrawings("C:/notes/a.pdf", [drawing]);
            notifyPdfDrawingsChanged("C:/notes/a.pdf");
        });
        await waitFor(() => expect(syncShare).toHaveBeenCalled(), { timeout: 4000 });
        const args = syncShare.mock.calls.at(-1) as unknown as [string, string];
        expect(args[0]).toBe("C:/notes/a.pdf");
        expect(JSON.parse(args[1])).toEqual({ highlights: [], chatMarks: [], annotations: [], drawings: [drawing] });
    });

    it("pushes a highlight painted before the library list names the host", async () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        let releaseList: (value: ListLibraryResult) => void = () => undefined;
        listLibrary.mockReturnValue(new Promise<ListLibraryResult>((resolve) => { releaseList = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-input");
        const mark = { id: "m1", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "重点" };
        act(() => { savePdfMarks("C:/notes/a.pdf", [mark], true); });
        expect(syncShare).not.toHaveBeenCalled();
        await act(async () => {
            releaseList({
                items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
                notice: "",
            });
        });
        await waitFor(() => expect(syncShare).toHaveBeenCalled(), { timeout: 4000 });
        const args = syncShare.mock.calls.at(-1) as unknown as [string, string];
        expect(JSON.parse(args[1])).toEqual({ highlights: [mark], chatMarks: [], annotations: [], drawings: [] });
    });

    it("warns once when the automatic share push fails", async () => {
        syncShare.mockRejectedValue(new Error("offline"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const token = "cd_0123456789abcdef0123456789abcdef";
        listLibrary.mockResolvedValue({
            items: [{ id: token, token, name: "a.pdf", path: "C:/notes/a.pdf", source: "cloud", owner: "me@example.com", visibility: "public", permanent: true, mine: true }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        const input = await screen.findByTestId("file-companion-input") as HTMLTextAreaElement;
        await waitFor(() => expect(screen.getByTestId("file-companion-share-open").getAttribute("aria-label")).toBe("修改分享"));
        fireEvent.change(input, { target: { value: "还在吗" } });
        fireEvent.keyDown(input, { key: "Enter" });
        // The readers are not getting the transcript, so the host has to hear
        // about it once instead of the push failing silently.
        expect(await screen.findByText(/自动同步到分享失败/, undefined, { timeout: 4000 })).toBeTruthy();
    });

    it("paints and clears a highlight on the agent's answer from the right-click menu", async () => {
        // jsdom has no Custom Highlight API. The menu only offers 高亮 when the
        // WebView can paint it, so this case installs a stub first.
        const registry = new Map<string, { size: number }>();
        class FakeHighlight {
            readonly ranges: Set<Range>;
            constructor(...ranges: Range[]) {
                this.ranges = new Set(ranges);
            }
            get size() {
                return this.ranges.size;
            }
        }
        (globalThis as unknown as { Highlight: unknown }).Highlight = FakeHighlight;
        const css = (globalThis as unknown as { CSS?: Record<string, unknown> }).CSS ?? {};
        css.highlights = {
            set: (name: string, value: { size: number }) => registry.set(name, value),
            delete: (name: string) => registry.delete(name),
        };
        (globalThis as unknown as { CSS: unknown }).CSS = css;
        try {
            boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
            open.mockResolvedValue(doc("C:/notes/a.pdf", {
                messages: [{ role: "user", text: "这段讲什么" }, { role: "assistant", text: "讲方法" }],
            }));
            installRuntime();
            renderWindow();
            const answer = await screen.findByTestId("file-companion-answer");
            // The same text walk the highlight uses, so the test anchors the
            // selection exactly where the menu does.
            const bubble = answer.closest<HTMLElement>("[data-chat-message]")!;
            const spans = chatTextSpans(bubble);
            expect(spans.length).toBeGreaterThan(0);
            const text = spans[0].node;
            expect(text.nodeValue).toContain("讲方法");
            const range = document.createRange();
            range.setStart(text, 0);
            range.setEnd(text, 2);
            const selection = window.getSelection()!;
            selection.removeAllRanges();
            selection.addRange(range);

            fireEvent.contextMenu(answer, { clientX: 40, clientY: 40 });
            expect(await screen.findByTestId("chat-highlight-menu")).toBeTruthy();
            expect(screen.getByTestId("chat-menu-quote").textContent).toContain("讲方");
            fireEvent.click(screen.getByTestId("chat-highlight-yellow"));

            await waitFor(() => expect(loadChatMarks("C:/notes/a.pdf")).toEqual([
                { message: 1, color: "yellow", start: 0, end: 2, text: "讲方" },
            ]));
            // The mark paints through the Custom Highlight registry.
            expect(registry.get("companion-chat-yellow")?.size).toBe(1);

            // A right-click on the painted phrase offers 取消高亮. jsdom has no
            // caretRangeFromPoint, so the collapsed caret is stubbed here.
            (document as unknown as { caretRangeFromPoint: () => Range }).caretRangeFromPoint = () => {
                const caret = document.createRange();
                caret.setStart(text, 1);
                caret.collapse(true);
                return caret;
            };
            fireEvent.contextMenu(answer, { clientX: 40, clientY: 40 });
            fireEvent.click(await screen.findByTestId("chat-highlight-clear"));
            await waitFor(() => expect(loadChatMarks("C:/notes/a.pdf")).toEqual([]));
            expect(registry.get("companion-chat-yellow")).toBeUndefined();
        } finally {
            delete (globalThis as unknown as { Highlight?: unknown }).Highlight;
            delete css.highlights;
            delete (document as unknown as { caretRangeFromPoint?: unknown }).caretRangeFromPoint;
            window.getSelection()?.removeAllRanges();
        }
    });

    it("clears the answer highlights with the chat they belong to", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockResolvedValue(doc("C:/notes/a.pdf", {
            messages: [{ role: "user", text: "这段讲什么" }, { role: "assistant", text: "讲方法" }],
        }));
        saveChatMarks("C:/notes/a.pdf", [{ message: 1, color: "yellow", start: 0, end: 2, text: "讲方" }]);
        installRuntime();
        renderWindow();
        const input = await screen.findByTestId("file-companion-input") as HTMLTextAreaElement;
        // The mark is on screen before the reset; /clear removes the answers it
        // was painted on, so it must not outlive them in the store.
        await waitFor(() => expect(loadChatMarks("C:/notes/a.pdf")).toHaveLength(1));
        fireEvent.change(input, { target: { value: "/clear" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(loadChatMarks("C:/notes/a.pdf")).toEqual([]));
        expect(screen.queryByTestId("file-companion-answer")).toBeNull();
    });

    it("keeps copy available when the WebView cannot paint highlights", async () => {
        // jsdom has no Custom Highlight API, which is exactly the old-WebView
        // case: the quote and 复制 stay, the colour row is not offered.
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockResolvedValue(doc("C:/notes/a.pdf", {
            messages: [{ role: "user", text: "这段讲什么" }, { role: "assistant", text: "讲方法" }],
        }));
        installRuntime();
        renderWindow();
        const answer = await screen.findByTestId("file-companion-answer");
        const bubble = answer.closest<HTMLElement>("[data-chat-message]")!;
        const spans = chatTextSpans(bubble);
        const range = document.createRange();
        range.setStart(spans[0].node, 0);
        range.setEnd(spans[0].node, 2);
        const selection = window.getSelection()!;
        selection.removeAllRanges();
        selection.addRange(range);
        fireEvent.contextMenu(answer, { clientX: 40, clientY: 40 });
        expect(await screen.findByTestId("chat-menu-copy")).toBeTruthy();
        expect(screen.queryByTestId("chat-highlight-yellow")).toBeNull();
        fireEvent.click(screen.getByTestId("chat-menu-copy"));
        await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith("讲方"));
    });

    it("asks with the custom dialog before deleting companion data and keeps the tab when cancelled", async () => {        const mark = { id: "m1", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "Hello" };
        const drawing = { id: "d1", page: 1, color: "yellow" as const, tool: "line" as const, points: [{ x: 0.1, y: 0.2 }, { x: 0.3, y: 0.4 }] };
        savePdfMarks("C:/notes/a.md", [mark]);
        savePdfDrawings("C:/notes/a.md", [drawing]);
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        const item = { id: "local:a", name: "a.md", path: "C:/notes/a.md", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        listLibrary.mockResolvedValue({ items: [item], notice: "" });
        deleteProject.mockImplementation(async () => {
            listLibrary.mockResolvedValue({ items: [], notice: "" });
            return { id: "local:a", path: "C:/notes/a.md", name: "a.md", cleared: true };
        });
        installRuntime();
        render(
            <DialogProvider>
                <ToastProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </ToastProvider>
            </DialogProvider>,
        );
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-delete-local:a");
        fireEvent.click(screen.getByTestId("file-companion-library-delete-local:a"));
        const dialog = await screen.findByRole("dialog", { name: "删除伴读数据" });
        expect(dialog.className).toContain("custom-dialog");
        expect(within(dialog).getByText("确认删除？")).toBeTruthy();
        expect(within(dialog).getByText("a.md").className).toContain("custom-dialog__subject-name");
        expect(within(dialog).getByText("源文件会保留。本机的高亮、讨论和阅读记录会被清除。").className).toContain("custom-dialog__follow");
        expect(within(dialog).getByText("此操作不可撤销").className).toContain("custom-dialog__notice");
        fireEvent.click(within(dialog).getByRole("button", { name: "取消" }));
        await waitFor(() => expect(screen.queryByRole("dialog", { name: "删除伴读数据" })).toBeNull());
        expect(deleteProject).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-tab-a.md")).toBeTruthy();
        expect(loadPdfMarks("C:/notes/a.md")).toEqual([mark]);
        expect(loadPdfDrawings("C:/notes/a.md")).toEqual([drawing]);

        fireEvent.click(screen.getByTestId("file-companion-library-delete-local:a"));
        const again = await screen.findByRole("dialog", { name: "删除伴读数据" });
        fireEvent.click(within(again).getByRole("button", { name: "删除" }));
        await waitFor(() => expect(deleteProject).toHaveBeenCalledWith("local:a"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-a.md")).toBeNull());
        await waitFor(() => expect(screen.queryByTestId("file-companion-library-card-local:a")).toBeNull());
        expect(loadPdfMarks("C:/notes/a.md")).toEqual([]);
        expect(loadPdfDrawings("C:/notes/a.md")).toEqual([]);
    });

    it("drops a library list that started before delete instead of restoring marks", async () => {
        const mark = { id: "m1", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "Hello" };
        savePdfMarks("C:/notes/a.md", [mark]);
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        const item = { id: "local:a", name: "a.md", path: "C:/notes/a.md", source: "local", openedAt: "2026-10-25T00:00:00Z" };
        const stale = {
            items: [item],
            notice: "",
            updates: [{
                path: "C:/notes/a.md",
                highlights: JSON.stringify([mark]),
                messages: [{ role: "assistant", text: "回来了" }],
                project: true,
            }],
        };
        let phase: "normal" | "hold" | "after" = "normal";
        const held: Array<(value: ListLibraryResult) => void> = [];
        listLibrary.mockImplementation(async () => {
            if (phase === "hold") return new Promise((resolve) => { held.push(resolve); });
            if (phase === "after") return { items: [], notice: "" };
            return { items: [item], notice: "" };
        });
        deleteProject.mockResolvedValue({ id: "local:a", path: "C:/notes/a.md", name: "a.md", cleared: true });
        installRuntime();
        render(
            <DialogProvider>
                <ToastProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </ToastProvider>
            </DialogProvider>,
        );
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-delete-local:a");
        phase = "hold";
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(held.length).toBeGreaterThan(0));
        fireEvent.click(screen.getByTestId("file-companion-library-delete-local:a"));
        const dialog = await screen.findByRole("dialog", { name: "删除伴读数据" });
        phase = "after";
        fireEvent.click(within(dialog).getByRole("button", { name: "删除" }));
        await waitFor(() => expect(deleteProject).toHaveBeenCalledWith("local:a"));
        await waitFor(() => expect(loadPdfMarks("C:/notes/a.md")).toEqual([]));
        await waitFor(() => expect(screen.queryByTestId("file-companion-library-card-local:a")).toBeNull());
        await act(async () => {
            for (const resolve of held) resolve(stale);
        });
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-a.md")).toBeNull());
        expect(loadPdfMarks("C:/notes/a.md")).toEqual([]);
        expect(screen.queryByTestId("file-companion-library-card-local:a")).toBeNull();
    });

    it("moves focus into a dialog and gives it back when the dialog closes", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        // 分享 focuses its first audience tile, so the dialog is usable from
        // the keyboard without tabbing through the title bar again.
        const shareButton = screen.getByTestId("file-companion-share-open");
        shareButton.focus();
        fireEvent.click(shareButton);
        await screen.findByTestId("file-companion-share-dialog");
        await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId("file-companion-share-public")));
        // Closing returns focus to the control that opened it.
        fireEvent.keyDown(document, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("file-companion-share-dialog")).toBeNull());
        await waitFor(() => expect(document.activeElement).toBe(shareButton));
    });

    it("keeps 文档管理 and 阅读群组 as separate screens over the reader", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        listGroups.mockResolvedValue([]);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        // 文档管理 and 阅读群组 are the only flex items in their anchor, so the
        // anchor gap is the space between those two squares.
        const anchor = screen.getByTestId("file-companion-library-anchor");
        expect(Array.from(anchor.children, (node) => (node as HTMLElement).dataset.testid)).toEqual([
            "file-companion-library",
            "file-companion-groups-open",
        ]);
        // The strip between the squares is not a button. On Windows the drag
        // handler ignores it only while this attribute stays on the anchor.
        expect(anchor.hasAttribute("data-window-no-drag")).toBe(true);
        // Title-bar tools are icon-only now: tooltip and aria-label carry the
        // text, and the one label span keeps the screen reader and tests fed.
        const paper = screen.getByTestId("file-companion-paper");
        expect(paper.className).toContain("file-companion-tool--icon");
        expect(paper.getAttribute("aria-label")).toBe("论文解读");
        expect(paper.getAttribute("title")).toContain("论文解读");
        expect(paper.textContent).toBe("论文解读");
        const upload = screen.getByTestId("file-companion-upload");
        expect(upload.className).toContain("file-companion-tool--icon");
        expect(upload.getAttribute("title")).toContain("上传到云盘");

        // 文档管理 is a full screen. The reader stays mounted underneath, so a
        // placed native player keeps its slot and a running note is not stopped.
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        expect(screen.getByTestId("file-companion-library").getAttribute("aria-pressed")).toBe("true");
        expect(screen.getByTestId("file-companion-split")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-library-back"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-library-panel")).toBeNull());
        expect(screen.getByTestId("file-companion-library").getAttribute("aria-pressed")).toBe("false");

        // 阅读群组 is its own screen with its own entry.
        fireEvent.click(screen.getByTestId("file-companion-groups-open"));
        await screen.findByTestId("file-companion-groups-view");
        expect(screen.getByTestId("file-companion-groups-open").getAttribute("aria-pressed")).toBe("true");
        fireEvent.change(screen.getByTestId("file-companion-group-name"), { target: { value: "新组" } });
        fireEvent.click(screen.getByTestId("file-companion-group-create"));
        await waitFor(() => expect(createGroup).toHaveBeenCalledWith("新组"));
        fireEvent.click(screen.getByTestId("file-companion-groups-back"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-groups-view")).toBeNull());
    });

    it("shares with a person or a group from the share popup", async () => {
        const groupID = "cg_0123456789abcdef0123456789abcdef";
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        listGroups.mockResolvedValue([{
            id: groupID,
            name: "论文组",
            owner: "me@example.com",
            admin: true,
            members: [{ contact: "reader@example.com", status: "pending" }],
        }]);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        // 分享 opens from the title bar for the open document.
        fireEvent.click(screen.getByTestId("file-companion-share-open"));
        await screen.findByTestId("file-companion-share-dialog");
        fireEvent.click(screen.getByTestId("file-companion-share-group"));
        await waitFor(() => expect(screen.getByTestId("file-companion-share-group-pick").querySelectorAll("option").length).toBeGreaterThan(1));
        fireEvent.change(screen.getByTestId("file-companion-share-group-pick"), { target: { value: groupID } });
        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        await waitFor(() => expect(shareDocument).toHaveBeenCalledWith("C:/notes/a.md", "group", "", groupID, JSON.stringify({ highlights: [], chatMarks: [], annotations: [], drawings: [] }), 0));
        await waitFor(() => expect(screen.getByTestId("file-companion-library-share").textContent).toBe("分享到云端"));
        fireEvent.click(screen.getByTestId("file-companion-share-person"));
        fireEvent.change(screen.getByTestId("file-companion-share-target"), { target: { value: "reader@example.com" } });
        fireEvent.click(screen.getByTestId("file-companion-library-share"));
        await waitFor(() => expect(shareDocument).toHaveBeenCalledWith("C:/notes/a.md", "person", "", "reader@example.com", JSON.stringify({ highlights: [], chatMarks: [], annotations: [], drawings: [] }), 0));
        await waitFor(() => expect(screen.getByTestId("file-companion-library-share").textContent).toBe("分享到云端"));
        fireEvent.click(screen.getByTestId("file-companion-library-sync"));
        await waitFor(() => expect(syncShare).toHaveBeenCalledWith("C:/notes/a.md", JSON.stringify({ highlights: [], chatMarks: [], annotations: [], drawings: [] })));
    });

    it("selects a newly created group and clears the selection when it is dissolved", async () => {
        const firstID = "cg_11111111111111111111111111111111";
        const secondID = "cg_22222222222222222222222222222222";
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        listGroups.mockResolvedValue([{ id: firstID, name: "论文组", owner: "me@example.com", admin: true, members: [] }]);
        createGroup.mockResolvedValue({ id: secondID, name: "新组", admin: true, members: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-groups-open"));
        await screen.findByTestId(`file-companion-group-${firstID}`);
        // The first group is selected by default.
        expect(screen.getByTestId(`file-companion-group-${firstID}`).getAttribute("aria-selected")).toBe("true");

        // Creating a group selects it, so the pane shows the group just made
        // instead of keeping the previous one.
        fireEvent.change(screen.getByTestId("file-companion-group-name"), { target: { value: "新组" } });
        fireEvent.click(screen.getByTestId("file-companion-group-create"));
        await waitFor(() => expect(createGroup).toHaveBeenCalledWith("新组"));
        await waitFor(() => expect(screen.getByTestId(`file-companion-group-${secondID}`).getAttribute("aria-selected")).toBe("true"));
        expect(screen.getByTestId(`file-companion-group-${firstID}`).getAttribute("aria-selected")).toBe("false");

        // Dissolving it drops the selection; the remaining group takes over.
        listGroups.mockResolvedValue([{ id: firstID, name: "论文组", owner: "me@example.com", admin: true, members: [] }]);
        fireEvent.click(screen.getByTestId(`file-companion-group-dissolve-${secondID}`));
        fireEvent.click(await screen.findByTestId(`file-companion-group-dissolve-confirm-${secondID}`));
        await waitFor(() => expect(deleteGroup).toHaveBeenCalledWith(secondID));
        await waitFor(() => expect(screen.queryByTestId(`file-companion-group-${secondID}`)).toBeNull());
        // Exactly one row stays selected, never zero.
        await waitFor(() => expect(screen.getByTestId(`file-companion-group-${firstID}`).getAttribute("aria-selected")).toBe("true"));
    });

    it("lets an admin manage members and rename a group on the 阅读群组 screen", async () => {
        const groupID = "cg_0123456789abcdef0123456789abcdef";
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha" }));
        listGroups.mockResolvedValue([{
            id: groupID,
            name: "论文组",
            owner: "me@example.com",
            admin: true,
            members: [{ contact: "reader@example.com", status: "pending" }],
        }]);
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-groups-open"));
        // The rail lists the group; the member list lives in the detail pane.
        await screen.findByTestId(`file-companion-group-${groupID}`);
        const detail = screen.getByTestId("file-companion-groups");
        expect(detail.textContent).toContain("reader@example.com");
        expect(detail.textContent).toContain("待确认");
        fireEvent.change(screen.getByTestId("file-companion-group-name"), { target: { value: "新组" } });
        fireEvent.click(screen.getByTestId("file-companion-group-create"));
        await waitFor(() => expect(createGroup).toHaveBeenCalledWith("新组"));
        await waitFor(() => expect(screen.getByTestId("file-companion-group-create").hasAttribute("disabled")).toBe(false));
        fireEvent.change(screen.getByTestId(`file-companion-group-contact-${groupID}`), { target: { value: "13800138000" } });
        fireEvent.click(screen.getByTestId(`file-companion-group-add-${groupID}`));
        await waitFor(() => expect(addGroupMember).toHaveBeenCalledWith(groupID, "13800138000"));
        await waitFor(() => expect(screen.getByTestId("file-companion-group-create").hasAttribute("disabled")).toBe(false));
        fireEvent.click(screen.getByTestId(`file-companion-group-remove-${groupID}-reader@example.com`));
        await waitFor(() => expect(removeGroupMember).toHaveBeenCalledWith(groupID, "reader@example.com"));
        await waitFor(() => expect(screen.getByTestId(`file-companion-group-save-${groupID}`).hasAttribute("disabled")).toBe(false));
        fireEvent.change(screen.getByTestId(`file-companion-group-rename-${groupID}`), { target: { value: "   " } });
        fireEvent.click(screen.getByTestId(`file-companion-group-save-${groupID}`));
        expect(updateGroup).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId(`file-companion-group-rename-${groupID}`), { target: { value: "名".repeat(41) } });
        fireEvent.click(screen.getByTestId(`file-companion-group-save-${groupID}`));
        expect(updateGroup).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId(`file-companion-group-rename-${groupID}`), { target: { value: "新论文组" } });
        fireEvent.click(screen.getByTestId(`file-companion-group-icon-${groupID}-star`));
        const save = screen.getByTestId(`file-companion-group-save-${groupID}`);
        fireEvent.click(save);
        fireEvent.click(save);
        await waitFor(() => expect(updateGroup).toHaveBeenCalledTimes(1));
        expect(updateGroup).toHaveBeenCalledWith(groupID, "新论文组", "star");
        await waitFor(() => expect(screen.getByTestId(`file-companion-group-dissolve-${groupID}`).hasAttribute("disabled")).toBe(false));
        expect(screen.queryByTestId(`file-companion-group-leave-${groupID}`)).toBeNull();
        fireEvent.click(screen.getByTestId(`file-companion-group-dissolve-${groupID}`));
        const dissolve = await screen.findByTestId(`file-companion-group-dissolve-confirm-${groupID}`);
        fireEvent.click(dissolve);
        fireEvent.click(dissolve);
        await waitFor(() => expect(deleteGroup).toHaveBeenCalledTimes(1));
        expect(deleteGroup).toHaveBeenCalledWith(groupID);
    });

    it("lets a member leave a group and shows the group icon", async () => {
        const groupID = "cg_0123456789abcdef0123456789abcdef";
        boot.mockResolvedValue({ paths: [] });
        listGroups.mockResolvedValue([{
            id: groupID,
            name: "论文组",
            owner: "host@example.com",
            admin: false,
            icon: "star",
            members: [],
        }]);
        installRuntime();
        renderWindow();
        fireEvent.click(screen.getByTestId("file-companion-groups-open"));
        const mark = await screen.findByTestId(`file-companion-group-mark-${groupID}`);
        expect(mark.getAttribute("data-icon")).toBe("star");
        expect(screen.getByTestId(`file-companion-group-leave-${groupID}`)).toBeTruthy();
        expect(screen.queryByTestId(`file-companion-group-dissolve-${groupID}`)).toBeNull();
        expect(screen.queryByTestId(`file-companion-group-rename-${groupID}`)).toBeNull();
        fireEvent.click(screen.getByTestId(`file-companion-group-leave-${groupID}`));
        const leave = await screen.findByTestId(`file-companion-group-leave-confirm-${groupID}`);
        fireEvent.click(leave);
        fireEvent.click(leave);
        await waitFor(() => expect(leaveGroup).toHaveBeenCalledTimes(1));
        expect(leaveGroup).toHaveBeenCalledWith(groupID);
    });

    it("keeps a renamed or dissolved group when the group list reload fails", async () => {
        const groupID = "cg_0123456789abcdef0123456789abcdef";
        boot.mockResolvedValue({ paths: [] });
        listGroups.mockResolvedValue([{
            id: groupID,
            name: "论文组",
            owner: "me@example.com",
            admin: true,
            icon: "book",
            members: [],
        }]);
        installRuntime();
        renderWindow();
        fireEvent.click(screen.getByTestId("file-companion-groups-open"));
        const rename = await screen.findByTestId(`file-companion-group-rename-${groupID}`);
        listGroups.mockRejectedValueOnce(new Error("暂时无法连接云端"));
        fireEvent.change(rename, { target: { value: "新论文组" } });
        fireEvent.click(screen.getByTestId(`file-companion-group-icon-${groupID}-star`));
        fireEvent.click(screen.getByTestId(`file-companion-group-save-${groupID}`));
        await waitFor(() => expect((screen.getByTestId(`file-companion-group-rename-${groupID}`) as HTMLInputElement).value).toBe("新论文组"));
        expect(screen.getByTestId(`file-companion-group-mark-${groupID}`).getAttribute("data-icon")).toBe("star");
        expect(screen.getByTestId("file-companion-groups-notice").textContent).toContain("暂时无法连接云端");
        listGroups.mockRejectedValueOnce(new Error("暂时无法连接云端"));
        fireEvent.click(screen.getByTestId(`file-companion-group-dissolve-${groupID}`));
        fireEvent.click(await screen.findByTestId(`file-companion-group-dissolve-confirm-${groupID}`));
        await waitFor(() => expect(deleteGroup).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(screen.queryByTestId(`file-companion-group-${groupID}`)).toBeNull());
    });

    it("closes the management screens and popups on Escape without leaving fullscreen", async () => {
        boot.mockResolvedValue({ paths: [] });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-open-menu");
        const choose = screen.getByTestId("file-companion-choose");
        const library = screen.getByTestId("file-companion-library");
        const gap = screen.getByTestId("file-companion-titlebar-gap");
        expect(screen.queryByTestId("file-companion-export")).toBeNull();
        expect(choose.compareDocumentPosition(library) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(library.compareDocumentPosition(gap) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        fireEvent.click(library);
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        await screen.findByTestId("file-companion-library-panel");
        fireEvent.click(screen.getByTestId("file-companion-fullscreen"));
        await waitFor(() => expect(windowFullscreen).toHaveBeenCalledTimes(1));
        // The 文档管理 screen owns Escape: it closes, the window stays full.
        fireEvent.keyDown(document, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("file-companion-library-panel")).toBeNull());
        expect(windowUnfullscreen).not.toHaveBeenCalled();
        // With no screen up, Escape leaves fullscreen.
        fireEvent.keyDown(window, { key: "Escape" });
        await waitFor(() => expect(windowUnfullscreen).toHaveBeenCalledTimes(1));
        // 打开 and 文档管理 are exclusive.
        fireEvent.click(screen.getByTestId("file-companion-choose"));
        await screen.findByTestId("file-companion-open-menu");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        expect(screen.queryByTestId("file-companion-open-menu")).toBeNull();
        await screen.findByTestId("file-companion-library-panel");
    });

    it("lets the share popup own Escape before the screen and the fullscreen exit", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        fireEvent.click(screen.getByTestId("file-companion-share-open"));
        await screen.findByTestId("file-companion-share-dialog");
        fireEvent.keyDown(document, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("file-companion-share-dialog")).toBeNull());
        // The popup did not also close the screen underneath it.
        expect(screen.getByTestId("file-companion-split")).toBeTruthy();
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
        // 导出 PDF sits immediately to the right of 论文解读, then 导出项目 and 导入知识库.
        // The destination panel hangs from an anchor beside the button.
        const chatAnchor = screen.getByTestId("file-companion-export-anchor");
        expect(screen.getByTestId("file-companion-paper").nextElementSibling).toBe(chatAnchor);
        expect(chatAnchor.contains(screen.getByTestId("file-companion-export-chat"))).toBe(true);
        expect(chatAnchor.nextElementSibling).toBe(screen.getByTestId("file-companion-export-project"));
        expect(screen.getByTestId("file-companion-export-project").nextElementSibling).toBe(screen.getByTestId("file-companion-import"));
        expect(screen.getByTestId("file-companion-import").textContent).toContain("导入知识库");
        expect(screen.getByTestId("file-companion-upload").textContent).toContain("上传到云盘");
        for (const id of ["file-companion-paper", "file-companion-export-chat", "file-companion-export-project", "file-companion-import", "file-companion-upload"]) {
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

    it("hides 转换 unless the open PDF is a scan", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false }));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        installRuntime();
        const view = renderWindow();
        await screen.findByTestId("file-companion-paper");
        expect(screen.queryByTestId("file-companion-convert")).toBeNull();
        expect(scanPDF).not.toHaveBeenCalled();
        view.unmount();

        boot.mockResolvedValue({ paths: ["C:/docs/paper.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: false, path: "" });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-paper.pdf");
        await waitFor(() => expect(scanPDF).toHaveBeenCalledWith("C:/docs/paper.pdf"));
        expect(screen.queryByTestId("file-companion-convert")).toBeNull();
        expect(screen.getByTestId("file-companion-paper").nextElementSibling).toBe(screen.getByTestId("file-companion-export-anchor"));
    });

    it("does not convert when the save dialog is cancelled", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        chooseConvertDest.mockResolvedValue("");
        installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-convert"));
        await waitFor(() => expect(chooseConvertDest).toHaveBeenCalledWith("C:/scans/book.pdf"));
        expect(convertScan).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换");
        expect(screen.getByTestId("file-companion-convert").textContent).not.toContain("正在转换");
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("reports a failed save and still selects the session PDF", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        convertScan.mockResolvedValue({
            ok: true,
            scan: true,
            complete: true,
            save_failed: true,
            path: "C:/data/searchable.pdf",
            message: "无法保存到该位置",
        });
        installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-convert"));
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("ERR") && text.includes("无法保存到该位置"))).toBe(true);
            expect(notes.some((text) => text.includes("已按原版式生成新的 PDF"))).toBe(false);
        });
        expect(showItemInFolder).toHaveBeenCalledWith("C:/data/searchable.pdf");
        expect(screen.getByTestId("file-companion-convert-result").textContent).toContain("无法保存到该位置");
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换");
        expect(screen.getByTestId("file-companion-convert").textContent).not.toContain("正在转换");
    });

    it("reports a failed save from the background copy", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        convertScan.mockResolvedValue({ ok: true, scan: true, started: true });
        const runtime = installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-convert"));
        await waitFor(() => expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换…"));
        act(() => {
            runtime.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: true,
                scan: true,
                complete: true,
                save_failed: true,
                pdf: "C:/data/searchable.pdf",
                message: "无法保存到该位置",
            });
        });
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("ERR") && text.includes("无法保存到该位置"))).toBe(true);
            expect(notes.some((text) => text.includes("已按原版式生成新的 PDF"))).toBe(false);
        });
        expect(showItemInFolder).toHaveBeenCalledWith("C:/data/searchable.pdf");
        expect(screen.getByTestId("file-companion-convert-result").textContent).toContain("无法保存到该位置");
        expect((screen.getByTestId("file-companion-convert") as HTMLButtonElement).disabled).toBe(false);
    });

    it("converts a scanned PDF in the background and notices when it finishes", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        let converts = 0;
        convertScan.mockImplementation(async () => {
            converts += 1;
            if (converts > 1) {
                return {
                    ok: true,
                    scan: true,
                    complete: true,
                    path: "C:/out/searchable.pdf",
                    message: "已按原版式生成新的 PDF，原文件没有改动。共 17 页。",
                };
            }
            return { ok: true, scan: true, started: true, message: "正在后台转换，完成后会提示。" };
        });
        const runtime = installRuntime();
        renderWindow();
        const button = await screen.findByTestId("file-companion-convert");
        expect(button.textContent).toContain("转换");
        expect(button.querySelector("svg.file-companion-tool-icon")?.getAttribute("aria-hidden")).toBe("true");
        expect(button.querySelector("svg.file-companion-tool-icon")?.getAttribute("data-icon")).toBe("convert");
        expect(button.nextElementSibling).toBe(screen.getByTestId("file-companion-paper"));
        fireEvent.click(button);
        await waitFor(() => expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换…"));
        expect((screen.getByTestId("file-companion-convert") as HTMLButtonElement).disabled).toBe(true);
        expect(chooseConvertDest).toHaveBeenCalledWith("C:/scans/book.pdf");
        expect(convertScan).toHaveBeenCalledWith("C:/scans/book.pdf", "C:/out/chosen.pdf");
        expect(screen.queryByTestId("file-companion-convert-result")).toBeNull();
        expect(screen.queryByRole("alert")).toBeNull();
        act(() => { runtime.emit("file-companion:convert-progress", { path: "C:/scans/book.pdf", done: 4, total: 17 }); });
        expect(screen.getByTestId("file-companion-convert").getAttribute("title")).toContain("4/17");
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换…");
        act(() => {
            runtime.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: true,
                scan: true,
                complete: true,
                pdf: "C:/out/searchable.pdf",
                message: "已按原版式生成新的 PDF，原文件没有改动。共 17 页。",
            });
        });
        await waitFor(() => expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换"));
        expect((screen.getByTestId("file-companion-convert") as HTMLButtonElement).disabled).toBe(false);
        const card = await screen.findByTestId("file-companion-convert-result");
        expect(card.textContent).toContain("已按原版式生成新的 PDF，原文件没有改动。");
        expect(screen.getByTestId("task-result-view-btn").textContent).toBe("查看文档");
        fireEvent.click(screen.getByTestId("task-result-view-btn"));
        expect(openResult).toHaveBeenCalledWith("C:/out/searchable.pdf");
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("「book.pdf」") && text.includes("已按原版式生成新的 PDF，原文件没有改动。"))).toBe(true);
            expect(notes.some((text) => text.includes("共 17 页"))).toBe(false);
        });
        expect(showItemInFolder).toHaveBeenCalledWith("C:/out/searchable.pdf");
        showItemInFolder.mockClear();
        fireEvent.click(screen.getByTestId("file-companion-convert"));
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.filter((text) => text.includes("「book.pdf」") && text.includes("已按原版式生成新的 PDF，原文件没有改动。")).length).toBeGreaterThan(1);
        });
        expect(showItemInFolder).toHaveBeenCalledWith("C:/out/searchable.pdf");
        expect(convertScan).toHaveBeenCalledTimes(2);
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换");
        expect(screen.getByTestId("file-companion-convert").textContent).not.toContain("正在转换");
    });

    it("toasts the server sentence when conversion is unfinished or still waiting on a model", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        convertScan.mockResolvedValue({ ok: true, scan: true, started: true });
        const runtime = installRuntime();
        const view = renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-convert"));
        await waitFor(() => expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换…"));
        expect(screen.queryByRole("alert")).toBeNull();
        act(() => {
            runtime.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: true,
                scan: true,
                complete: false,
                pdf: "C:/out/searchable.pdf",
                message: "已按原版式生成新的 PDF，原文件没有改动。后面还有 3 页。请再点「转换」继续。",
            });
        });
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("后面还有 3 页") && text.includes("再点「转换」"))).toBe(true);
        });
        expect(showItemInFolder).toHaveBeenCalledWith("C:/out/searchable.pdf");
        expect(screen.getByTestId("file-companion-convert-result").textContent).toContain("后面还有 3 页");
        fireEvent.click(screen.getByTestId("file-companion-convert"));
        await waitFor(() => expect(convertScan).toHaveBeenCalledTimes(2));
        view.unmount();

        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        convertScan.mockResolvedValue({ ok: true, scan: true, started: true });
        const again = installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-convert"));
        await waitFor(() => expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换…"));
        act(() => {
            again.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: true,
                scan: true,
                complete: true,
                pending: true,
                pdf: "C:/out/searchable.pdf",
                message: "已按原版式生成新的 PDF，原文件没有改动。共 17 页。表格模型还在下载。下载完成后再点「转换」，这些页会补上表格结构。",
            });
        });
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("表格模型还在下载") && text.includes("再点「转换」"))).toBe(true);
        });
    });

    it("toasts when a background conversion fails and leaves the button ready", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        convertScan.mockResolvedValue({ ok: true, scan: true, started: true });
        const runtime = installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-convert"));
        await waitFor(() => expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换…"));
        act(() => {
            runtime.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: false,
                scan: true,
                message: "本机识别尚未就绪",
            });
        });
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("「book.pdf」") && text.includes("本机识别尚未就绪"))).toBe(true);
        });
        expect(screen.queryByTestId("file-companion-convert-result")).toBeNull();
        expect(showItemInFolder).not.toHaveBeenCalled();
        expect((screen.getByTestId("file-companion-convert") as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换");
    });

    it("locates a finished PDF from the cache and drops the card when that PDF is gone", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf", "C:/scans/other.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        const ready = new Map<string, string>([["C:/scans/book.pdf", "C:/out/searchable.pdf"]]);
        scanPDF.mockImplementation(async (path: string) => ({ scan: true, path: ready.get(String(path)) || "" }));
        convertScan.mockResolvedValue({
            ok: true,
            scan: true,
            complete: true,
            path: "C:/out/searchable.pdf",
            message: "已按原版式生成新的 PDF，原文件没有改动。共 17 页。",
        });
        installRuntime();
        renderWindow();
        const card = await screen.findByTestId("file-companion-convert-result");
        expect(card.textContent).toContain("已按原版式生成新的 PDF，原文件没有改动。");
        expect(showItemInFolder).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-convert"));
        await waitFor(() => {
            const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
            expect(notes.some((text) => text.includes("「book.pdf」") && text.includes("已按原版式生成新的 PDF，原文件没有改动。"))).toBe(true);
            expect(notes.some((text) => text.includes("共 17 页"))).toBe(false);
        });
        expect(showItemInFolder).toHaveBeenCalledTimes(1);
        expect(showItemInFolder).toHaveBeenCalledWith("C:/out/searchable.pdf");
        expect(convertScan).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId("file-companion-convert").textContent).not.toContain("正在转换");

        ready.delete("C:/scans/book.pdf");
        showItemInFolder.mockClear();
        convertScan.mockResolvedValue({ ok: true, scan: true, started: true, message: "正在后台转换，完成后会提示。" });
        fireEvent.click(screen.getByTestId("file-companion-tab-other.pdf"));
        await screen.findByTestId("file-companion-tab-other.pdf");
        fireEvent.click(screen.getByTestId("file-companion-tab-book.pdf"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-convert-result")).toBeNull());
        fireEvent.click(screen.getByTestId("file-companion-convert"));
        await waitFor(() => expect(convertScan).toHaveBeenCalledTimes(2));
        expect(showItemInFolder).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("正在转换");
    });

    it("hides 转换 when the open file is no longer a scan", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf", "C:/scans/other.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        let bookIsScan = true;
        scanPDF.mockImplementation(async (path: string) => {
            if (String(path).endsWith("book.pdf") && bookIsScan) return { scan: true, path: "C:/out/searchable.pdf" };
            if (String(path).endsWith("book.pdf")) return { scan: false, path: "" };
            return { scan: true, path: "" };
        });
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-convert-result")).textContent).toContain("已按原版式生成新的 PDF");
        bookIsScan = false;
        fireEvent.click(screen.getByTestId("file-companion-tab-other.pdf"));
        fireEvent.click(screen.getByTestId("file-companion-tab-book.pdf"));
        await waitFor(() => {
            expect(screen.queryByTestId("file-companion-convert")).toBeNull();
            expect(screen.queryByTestId("file-companion-convert-result")).toBeNull();
        });
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("keeps 转换 when the scan probe cannot read the file", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf", "C:/scans/other.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        let bookUnreadable = false;
        scanPDF.mockImplementation(async (path: string) => {
            if (String(path).endsWith("book.pdf") && bookUnreadable) throw new Error("读取文档失败");
            if (String(path).endsWith("book.pdf")) return { scan: true, path: "C:/out/searchable.pdf" };
            return { scan: true, path: "" };
        });
        installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-convert-result")).textContent).toContain("已按原版式生成新的 PDF");
        const callsBefore = scanPDF.mock.calls.length;
        bookUnreadable = true;
        fireEvent.click(screen.getByTestId("file-companion-tab-other.pdf"));
        fireEvent.click(screen.getByTestId("file-companion-tab-book.pdf"));
        await waitFor(() => expect(scanPDF.mock.calls.length).toBeGreaterThan(callsBefore));
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换");
        expect(screen.getByTestId("file-companion-convert-result").textContent).toContain("已按原版式生成新的 PDF");
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("drops the result when conversion finds the file is no longer a scan", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "C:/out/searchable.pdf" });
        const runtime = installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-convert-result")).textContent).toContain("已按原版式生成新的 PDF");
        act(() => {
            runtime.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: true,
                scan: false,
                message: "这份 PDF 已有文字层，可以直接选择和搜索，不需要再恢复。",
            });
        });
        await waitFor(() => {
            expect(screen.queryByTestId("file-companion-convert")).toBeNull();
            expect(screen.queryByTestId("file-companion-convert-result")).toBeNull();
        });
        const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
        expect(notes.some((text) => text.includes("「book.pdf」") && text.includes("已有文字层"))).toBe(true);
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("drops the card when a failed conversion no longer has a PDF", async () => {
        boot.mockResolvedValue({ paths: ["C:/scans/book.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        scanPDF.mockResolvedValue({ scan: true, path: "C:/out/searchable.pdf" });
        const runtime = installRuntime();
        renderWindow();
        expect((await screen.findByTestId("file-companion-convert-result")).textContent).toContain("已按原版式生成新的 PDF");
        scanPDF.mockResolvedValue({ scan: true, path: "" });
        act(() => {
            runtime.emit("file-companion:convert-done", {
                path: "C:/scans/book.pdf",
                ok: false,
                scan: true,
                message: "文档内容为空",
            });
        });
        await waitFor(() => expect(screen.queryByTestId("file-companion-convert-result")).toBeNull());
        expect(screen.getByTestId("file-companion-convert").textContent).toContain("转换");
        const notes = screen.getAllByRole("alert").map((node) => node.textContent || "");
        expect(notes.some((text) => text.includes("「book.pdf」") && text.includes("文档内容为空"))).toBe(true);
        expect(showItemInFolder).not.toHaveBeenCalled();
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

    it("a reader's discussion is posted as a note and other readers' notes show on the paper", async () => {
        send.mockResolvedValue(undefined);
        addShareNote.mockResolvedValue([
            { id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" },
            { id: "cn_def", author: "me@example.com", text: "我也这么看", createdAt: "2026-10-25T00:01:00Z" },
        ]);
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notes: [{ id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" }],
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByText("方法部分值得再看");
        expect(screen.getByTestId("file-companion-annotations").textContent).toContain("批注");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "我也这么看" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(addShareNote).toHaveBeenCalledWith("C:/library/shared.pdf", "我也这么看"));
        // A received share is answered by the host's agent: this machine posts
        // the line and does not run a private turn of its own.
        expect(send).not.toHaveBeenCalled();
        expect((await screen.findByTestId("file-companion-annotation-cn_def")).textContent).toContain("我也这么看");

        const hostReply = { id: "cn_host", author: "host@example.com", text: "主持人 Agent 的回答", createdAt: "2026-10-25T00:02:00Z", host: true, replyTo: "cn_def" };
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 5,
                notes: [
                    { id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" },
                    { id: "cn_def", author: "me@example.com", text: "我也这么看", createdAt: "2026-10-25T00:01:00Z" },
                    hostReply,
                ],
            }],
            notice: "",
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        const reply = await screen.findByTestId("file-companion-share-reply-cn_host");
        expect(reply.textContent).toContain("主持人 Agent 的回答");
        expect(reply.textContent).toContain("主持人回复");
        expect(reply.textContent).toContain("回复 me@example.com：我也这么看");
        expect((screen.getByTestId("file-companion-annotation-cn_host")).textContent).toContain("主持人");
    });

    it("the host can pass a reader note before the agent answers it", async () => {
        const note = { id: "cn_pending", author: "reader@example.com", text: "这条批注需要主持人审核", createdAt: "2026-10-25T00:00:00Z", decision: "pending" };
        reviewShareNote.mockResolvedValue({
            notesRevision: 2,
            notes: [{ ...note, decision: "approved" }],
        });
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: true,
                notesRevision: 1,
                notes: [note],
            }],
            notice: "",
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByText("这条批注需要主持人审核");
        expect(screen.getByTestId("file-companion-annotations").textContent).toContain("待审核 1");
        fireEvent.click(screen.getByTestId("file-companion-annotation-approve-cn_pending"));
        await waitFor(() => expect(reviewShareNote).toHaveBeenCalledWith("cd_0123456789abcdef0123456789abcdef", "cn_pending", "approve"));
        expect((await screen.findByTestId("file-companion-annotation-approved-cn_pending")).textContent).toContain("已通过");
        expect(screen.queryByTestId("file-companion-annotation-approve-cn_pending")).toBeNull();
    });

    it("the host sees pass and ignore when the review flag is missing but the note is pending", async () => {
        const note = { id: "cn_phone", author: "phone:17090134628", text: "作者是谁？", createdAt: "2026-10-25T00:00:00Z", decision: "pending" };
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: true,
                host: true,
                notesRevision: 1,
                notes: [note],
            }],
            notice: "",
            notesReply: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByText("作者是谁？");
        expect(screen.getByTestId("file-companion-host-badge").textContent).toBe("你是主持人");
        expect(screen.getByTestId("file-companion-annotation-approve-cn_phone")).toBeTruthy();
        expect(screen.getByTestId("file-companion-annotation-ignore-cn_phone")).toBeTruthy();
        expect(screen.getByTestId("file-companion-annotations").textContent).toContain("待审核 1");
    });

    it("renders markdown in the annotation panel", async () => {
        const note = { id: "cn_md", author: "364479@qq.com", text: "这篇综述是**单作者**完成的。\n**Daniel Garcia\nPeña**", createdAt: "2026-10-25T00:00:00Z", host: true };
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 1,
                notes: [note],
            }],
            notice: "",
            notesReply: true,
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        const body = await screen.findByTestId("file-companion-annotation-body-cn_md");
        const strong = [...body.querySelectorAll("strong")].map((node) => node.textContent);
        expect(strong).toContain("单作者");
        expect(strong).toContain("Daniel Garcia Peña");
        expect(body.textContent).not.toContain("**");
        expect(screen.getByTestId("file-companion-annotation-cn_md").textContent).toContain("主持人");
    });

    it("the host can ignore a reader note and the participant sees the marker", async () => {
        const note = { id: "cn_ignored", author: "me@example.com", text: "这条会被忽略", createdAt: "2026-10-25T00:00:00Z", decision: "pending" };
        reviewShareNote.mockResolvedValue({
            notesRevision: 2,
            notes: [{ ...note, decision: "ignored" }],
        });
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: true,
                notesRevision: 1,
                notes: [note],
            }],
            notice: "",
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByText("这条会被忽略");
        fireEvent.click(screen.getByTestId("file-companion-annotation-ignore-cn_ignored"));
        await waitFor(() => expect(reviewShareNote).toHaveBeenCalledWith("cd_0123456789abcdef0123456789abcdef", "cn_ignored", "ignore"));
        expect((await screen.findByTestId("file-companion-annotation-ignored-cn_ignored")).textContent).toContain("已忽略");

        // The same decision is what a participant receives on the next list.
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 3,
                notes: [{ ...note, decision: "ignored" }],
            }],
            notice: "",
            notesReview: true,
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        expect((await screen.findByTestId("file-companion-annotation-ignored-cn_ignored")).textContent).toContain("主持人已忽略");
        expect(screen.queryByTestId("file-companion-annotation-approve-cn_ignored")).toBeNull();
    });

    it("the host can take an ignored note back before the agent answers", async () => {
        const note = { id: "cn_reopen", author: "reader@example.com", text: "先忽略再通过", createdAt: "2026-10-25T00:00:00Z", decision: "ignored" };
        reviewShareNote.mockResolvedValue({
            notesRevision: 3,
            notes: [{ ...note, decision: "approved" }],
        });
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: true,
                notesRevision: 2,
                notes: [note],
            }],
            notice: "",
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByText("先忽略再通过");
        expect(screen.getByTestId("file-companion-annotation-ignored-cn_reopen")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-annotation-reopen-cn_reopen"));
        await waitFor(() => expect(reviewShareNote).toHaveBeenCalledWith("cd_0123456789abcdef0123456789abcdef", "cn_reopen", "approve"));
        expect((await screen.findByTestId("file-companion-annotation-approved-cn_reopen")).textContent).toContain("已通过");
        expect(screen.queryByTestId("file-companion-annotation-reopen-cn_reopen")).toBeNull();
        expect(screen.queryByTestId("file-companion-annotation-ignored-cn_reopen")).toBeNull();
    });

    it("the host can withdraw an approval before the agent answers", async () => {
        const note = { id: "cn_pass", author: "reader@example.com", text: "先通过再忽略", createdAt: "2026-10-25T00:00:00Z", decision: "approved" };
        reviewShareNote.mockResolvedValue({
            notesRevision: 3,
            notes: [{ ...note, decision: "ignored" }],
        });
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: true,
                notesRevision: 2,
                notes: [note],
            }],
            notice: "",
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByText("先通过再忽略");
        expect(screen.getByTestId("file-companion-annotations").textContent).not.toMatch(/待审核 [1-9]/);
        fireEvent.click(screen.getByTestId("file-companion-annotation-withdraw-cn_pass"));
        await waitFor(() => expect(reviewShareNote).toHaveBeenCalledWith("cd_0123456789abcdef0123456789abcdef", "cn_pass", "ignore"));
        expect((await screen.findByTestId("file-companion-annotation-ignored-cn_pass")).textContent).toContain("已忽略");
        expect(screen.queryByTestId("file-companion-annotation-withdraw-cn_pass")).toBeNull();
    });

    it("a participant sees the ignore mark on the line they sent", async () => {
        const note = { id: "cn_line", author: "me@example.com", text: "这段先不要回答", createdAt: "2026-10-25T00:00:00Z", decision: "pending" };
        addShareNote.mockResolvedValue({
            notesRevision: 2,
            notes: [note],
        });
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 1,
                notes: [],
            }],
            notice: "",
            notesReply: true,
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-input");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "这段先不要回答" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        expect((await screen.findByTestId("file-companion-message-review-cn_line")).textContent).toContain("等待主持人审核");

        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 3,
                notes: [{ ...note, decision: "ignored" }],
            }],
            notice: "",
            notesReply: true,
            notesReview: true,
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        expect((await screen.findByTestId("file-companion-message-review-cn_line")).textContent).toContain("主持人已忽略");
        expect((await screen.findByTestId("file-companion-annotation-ignored-cn_line")).textContent).toContain("主持人已忽略");
    });

    it("polls an open host share quickly so a new reader note is not stuck on the slow timer", () => {
        expect(companionLibraryPollFast([{ shareToken: "cd_abc", shareMine: true }])).toBe(true);
        expect(companionLibraryPollFast([{ shareToken: "cd_abc", shareMine: false }])).toBe(true);
        expect(companionLibraryPollFast([{ shareMine: true }])).toBe(false);
        const used = new Set<string>();
        expect(companionMessageReview("这段先不要回答", [
            { id: "cn_line", author: "me", text: "这段先不要回答", createdAt: "2026-10-28T00:00:00Z", decision: "ignored" },
        ], used)?.label).toContain("已忽略");
        expect(companionMessageReview("这段先不要回答", [
            { id: "cn_line", author: "me", text: "这段先不要回答", createdAt: "2026-10-28T00:00:00Z", decision: "ignored" },
        ], used)).toBeNull();
    });

    it("a participant waits for host review instead of answering locally", async () => {
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 1,
                notes: [{ id: "cn_wait", author: "me@example.com", text: "我的问题", createdAt: "2026-10-25T00:00:00Z", decision: "pending" }],
            }],
            notice: "",
            notesReview: true,
        });
        installRuntime();
        renderWindow();
        await screen.findByText("我的问题");
        expect((await screen.findByTestId("file-companion-annotation-pending-cn_wait")).textContent).toContain("等待主持人审核");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "我的问题" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(addShareNote).toHaveBeenCalledWith("C:/library/shared.pdf", "我的问题"));
        expect(send).not.toHaveBeenCalled();
    });

    it("keeps the reader's own answer when the Hub cannot record host replies", async () => {
        send.mockResolvedValue(undefined);
        addShareNote.mockResolvedValue([
            { id: "cn_def", author: "me@example.com", text: "我也这么看", createdAt: "2026-10-25T00:01:00Z" },
        ]);
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notes: [],
            }],
            notice: "",
            notesReply: false,
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-input");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "我也这么看" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        // An older Hub has no host reply channel, so the note still goes out
        // but this reader's own turn answers it as before.
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(addShareNote).toHaveBeenCalledWith("C:/library/shared.pdf", "我也这么看"));
    });

    it("does not stack library reads while one is still in flight", async () => {
        const pending: Array<(value: ListLibraryResult) => void> = [];
        listLibrary.mockImplementation(() => new Promise<ListLibraryResult>((resolve) => { pending.push(resolve); }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        await waitFor(() => expect(listLibrary).toHaveBeenCalledTimes(1));

        // A focus while the first read is pending must not queue a second one:
        // a slow project download would otherwise stack a call per tick.
        window.dispatchEvent(new Event("focus"));
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(listLibrary).toHaveBeenCalledTimes(1);

        pending[0]({ items: [], notice: "" });
        await new Promise((resolve) => setTimeout(resolve, 0));
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(listLibrary).toHaveBeenCalledTimes(2));
    });

    it("a newer annotation list is not replaced by an older note or an older poll", async () => {
        send.mockResolvedValue(undefined);
        addShareNote.mockResolvedValue({
            notesRevision: 1,
            notes: [{ id: "cn_old", author: "reader@example.com", text: "旧批注", createdAt: "2026-10-25T00:00:00Z" }],
        });
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:\\Library\\shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 2,
                notes: [{ id: "cn_later", author: "other@example.com", text: "后来的讨论", createdAt: "2026-10-25T00:02:00Z" }],
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByText("后来的讨论");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "我也这么看" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(addShareNote).toHaveBeenCalledWith("C:/library/shared.pdf", "我也这么看"));
        expect(screen.queryByText("旧批注")).toBeNull();
        const calls = listLibrary.mock.calls.length;
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notesRevision: 1,
                notes: [{ id: "cn_old", author: "reader@example.com", text: "旧批注", createdAt: "2026-10-25T00:00:00Z" }],
            }],
            notice: "",
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(listLibrary.mock.calls.length).toBeGreaterThan(calls));
        expect(screen.queryByText("旧批注")).toBeNull();
        expect(screen.getByText("后来的讨论")).toBeTruthy();
    });

    it("notes from every share of the open file stay on the page", async () => {
        send.mockResolvedValue(undefined);
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [
                {
                    path: "C:/library/shared.pdf",
                    source: "cloud",
                    token: "cd_0123456789abcdef0123456789abcdef",
                    mine: true,
                    notesRevision: 2,
                    notes: [{ id: "cn_new", author: "a@example.com", text: "新分享里的批注", createdAt: "2026-10-25T00:02:00Z" }],
                },
                {
                    path: "C:\\library\\shared.pdf",
                    source: "cloud",
                    token: "cd_abcdef0123456789abcdef0123456789",
                    mine: true,
                    notesRevision: 4,
                    notes: [{ id: "cn_oldshare", author: "b@example.com", text: "另一份分享的批注", createdAt: "2026-10-25T00:01:00Z" }],
                },
            ],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByText("新分享里的批注");
        expect(screen.getByText("另一份分享的批注")).toBeTruthy();
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "主持人的新讨论" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(addShareNote).not.toHaveBeenCalled();
        expect(screen.getByText("新分享里的批注")).toBeTruthy();
        expect(screen.getByText("另一份分享的批注")).toBeTruthy();
    });

    it("a reader's line sent before the library list is still posted as a note", async () => {
        send.mockResolvedValue(undefined);
        addShareNote.mockResolvedValue([
            { id: "cn_def", author: "me@example.com", text: "我也这么看", createdAt: "2026-10-25T00:01:00Z" },
        ]);
        let releaseList: (value: ListLibraryResult) => void = () => undefined;
        listLibrary.mockReturnValue(new Promise((resolve) => { releaseList = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-input");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "我也这么看" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(addShareNote).not.toHaveBeenCalled();
        await act(async () => {
            releaseList({
                items: [{
                    path: "C:/library/shared.pdf",
                    source: "cloud",
                    token: "cd_0123456789abcdef0123456789abcdef",
                    mine: false,
                    notes: [],
                }],
                notice: "",
            });
        });
        await waitFor(() => expect(addShareNote).toHaveBeenCalledWith("C:/library/shared.pdf", "我也这么看"));
        expect(addShareNote).toHaveBeenCalledTimes(1);
        expect((await screen.findByTestId("file-companion-annotation-cn_def")).textContent).toContain("我也这么看");
    });

    it("a host line sent before the library list is not posted as a note", async () => {
        send.mockResolvedValue(undefined);
        let releaseList: (value: ListLibraryResult) => void = () => undefined;
        listLibrary.mockReturnValue(new Promise((resolve) => { releaseList = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-input");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "主持人的新讨论" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(addShareNote).not.toHaveBeenCalled();
        await act(async () => {
            releaseList({
                items: [{
                    path: "C:/notes/a.md",
                    source: "cloud",
                    token: "cd_0123456789abcdef0123456789abcdef",
                    mine: true,
                    notes: [{ id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" }],
                }],
                notice: "",
            });
        });
        await screen.findByText("方法部分值得再看");
        expect(addShareNote).not.toHaveBeenCalled();
    });

    it("the host chat is not posted as a note", async () => {
        send.mockResolvedValue(undefined);
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/notes/a.md",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: true,
                notes: [{ id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" }],
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByText("方法部分值得再看");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "主持人的新讨论" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(addShareNote).not.toHaveBeenCalled();
    });

    it("annotations leave the page when the library list no longer has that share", async () => {
        const note = { id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" };
        const share = {
            path: "C:/library/shared.pdf",
            source: "cloud",
            token: "cd_0123456789abcdef0123456789abcdef",
            mine: false,
            notes: [note],
        };
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({ items: [share], notice: "" });
        installRuntime();
        renderWindow();
        await screen.findByText("方法部分值得再看");
        let calls = listLibrary.mock.calls.length;
        listLibrary.mockResolvedValue({ items: [], notice: "" });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(listLibrary.mock.calls.length).toBeGreaterThan(calls));
        await waitFor(() => expect(screen.queryByText("方法部分值得再看")).toBeNull());
        expect(screen.queryByTestId("file-companion-annotations")).toBeNull();
        calls = listLibrary.mock.calls.length;
        listLibrary.mockResolvedValue({ items: [share], notice: "" });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(listLibrary.mock.calls.length).toBeGreaterThan(calls));
        await screen.findByText("方法部分值得再看");
        calls = listLibrary.mock.calls.length;
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/other.pdf",
                source: "cloud",
                token: "cd_abcdef0123456789abcdef0123456789",
                mine: false,
                notes: [{ id: "cn_other", author: "reader@example.com", text: "另一篇的批注", createdAt: "2026-10-25T00:03:00Z" }],
            }],
            notice: "",
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(listLibrary.mock.calls.length).toBeGreaterThan(calls));
        await waitFor(() => expect(screen.queryByText("方法部分值得再看")).toBeNull());
        expect(screen.queryByText("另一篇的批注")).toBeNull();
    });

    it("annotations leave the page when the document library reloads without that share", async () => {
        const note = { id: "cn_abc", author: "reader@example.com", text: "方法部分值得再看", createdAt: "2026-10-25T00:00:00Z" };
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notes: [note],
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByText("方法部分值得再看");
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/other.pdf",
                source: "cloud",
                token: "cd_abcdef0123456789abcdef0123456789",
                mine: false,
                notes: [],
            }],
            notice: "",
        });
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        await waitFor(() => expect(screen.queryByText("方法部分值得再看")).toBeNull());
        expect(screen.getByTestId("file-companion-tab-shared.pdf")).toBeTruthy();
    });

    it("a removed copy closes the open tab and names the host it already saw", async () => {
        const path = "C:/library/shared.pdf";
        savePdfMarks(path, [{ id: "m1", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "重点" }]);
        saveChatMarks(path, [{ message: 0, color: "yellow" as const, start: 0, end: 2, text: "hi" }]);
        boot.mockResolvedValue({ paths: [path] });
        open.mockImplementation(async (filePath: string) => doc(filePath));
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:\\library\\shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                hostLabel: "王老师",
                notes: [],
            }],
            notice: "",
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-shared.pdf");
        await waitFor(() => expect(screen.getByTestId("file-companion-tab-share-shared.pdf").getAttribute("data-state")).toBe("received"));
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            removed: ["C:\\library\\shared.pdf"],
        });
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-shared.pdf")).toBeNull());
        // A revoked share is withdrawn like a local delete, not just hidden.
        expect(loadPdfMarks(path)).toEqual([]);
        expect(loadChatMarks(path)).toEqual([]);
        const notice = screen.getByRole("alert").textContent ?? "";
        expect(notice).toContain("该文章已经取消分享");
        expect(notice).toContain("如果需要继续阅读");
        expect(notice).toContain("王老师");
        expect(notice).not.toContain("本地副本");
    });

    it("a removed copy names the host carried by the revocation list", async () => {
        const path = "C:/library/shared.pdf";
        boot.mockResolvedValue({ paths: [path] });
        open.mockImplementation(async (filePath: string) => doc(filePath));
        listLibrary.mockResolvedValue({ items: [], notice: "" });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-shared.pdf");
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            removed: ["C:\\library\\shared.pdf"],
            removedHosts: { "C:\\library\\shared.pdf": "李老师" },
        });
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-shared.pdf")).toBeNull());
        expect(screen.getByRole("alert").textContent ?? "").toContain("李老师");
    });

    it("a revoked active tab hands the reader to the next tab", async () => {
        boot.mockResolvedValue({ paths: ["C:/library/first.pdf", "C:/library/second.pdf", "C:/library/third.pdf"] });
        open.mockImplementation(async (filePath: string) => doc(filePath));
        listLibrary.mockResolvedValue({ items: [], notice: "" });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-third.pdf");
        expect(screen.getByTestId("file-companion-tab-first.pdf").getAttribute("aria-selected")).toBe("true");
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            removed: ["C:\\library\\first.pdf"],
            removedHosts: { "C:\\library\\first.pdf": "王老师" },
        });
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-first.pdf")).toBeNull());
        // Even without a write in flight, the reader lands on a neighbouring
        // document instead of an empty window.
        expect(screen.getByTestId("file-companion-tab-second.pdf").getAttribute("aria-selected")).toBe("true");
        expect(screen.getByTestId("file-companion-tab-third.pdf").getAttribute("aria-selected")).toBe("false");
    });

    it("one refresh names every host of the articles it closes", async () => {
        boot.mockResolvedValue({ paths: ["C:/library/first.pdf", "C:/library/second.pdf", "C:/library/third.pdf"] });
        open.mockImplementation(async (filePath: string) => doc(filePath));
        listLibrary.mockResolvedValue({ items: [], notice: "" });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-third.pdf");
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            removed: ["C:\\library\\first.pdf", "C:\\library\\second.pdf"],
            removedHosts: { "C:\\library\\first.pdf": "王老师", "C:\\library\\second.pdf": "李老师" },
        });
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-first.pdf")).toBeNull());
        expect(screen.queryByTestId("file-companion-tab-second.pdf")).toBeNull();
        const notice = screen.getByRole("alert").textContent ?? "";
        expect(notice).toContain("这些文章已经取消分享");
        expect(notice).toContain("王老师");
        expect(notice).toContain("李老师");
        expect(screen.getByTestId("file-companion-tab-third.pdf").getAttribute("aria-selected")).toBe("true");
    });

    it("a removed copy without a known host still explains what happened", async () => {
        const path = "C:/library/shared.pdf";
        boot.mockResolvedValue({ paths: [path] });
        open.mockImplementation(async (filePath: string) => doc(filePath));
        listLibrary.mockResolvedValue({ items: [], notice: "" });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-shared.pdf");
        listLibrary.mockResolvedValue({ items: [], notice: "", removed: ["C:\\library\\shared.pdf"] });
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-shared.pdf")).toBeNull());
        const notice = screen.getByRole("alert").textContent ?? "";
        expect(notice).toContain("该文章已经取消分享");
        expect(notice).toContain("如果需要继续阅读，请联系分享的主持人");
    });

    it("does not toast when the cancelled article is not an open tab", async () => {
        boot.mockResolvedValue({ paths: ["C:/library/other.pdf"] });
        open.mockImplementation(async (filePath: string) => doc(filePath));
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            removed: ["C:\\library\\shared.pdf"],
            removedHosts: { "C:\\library\\shared.pdf": "王老师" },
        });
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-other.pdf");
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(listLibrary.mock.calls.length).toBeGreaterThan(1));
        expect(screen.queryByRole("alert")).toBeNull();
        expect(screen.getByTestId("file-companion-tab-other.pdf")).toBeTruthy();
    });

    it("closes a tab that finishes opening after the share was already withdrawn", async () => {
        const path = "C:/library/shared.pdf";
        let releaseOpen: (value: unknown) => void = () => undefined;
        open.mockImplementation((filePath: string) => new Promise((resolve) => {
            releaseOpen = () => resolve(doc(filePath));
        }));
        boot.mockResolvedValue({ paths: [path] });
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            removed: ["C:\\library\\shared.pdf"],
            removedHosts: { "C:\\library\\shared.pdf": "王老师" },
        });
        installRuntime();
        renderWindow();
        await waitFor(() => expect(listLibrary).toHaveBeenCalled());
        await act(async () => { releaseOpen(undefined); });
        await waitFor(() => expect(screen.queryByTestId("file-companion-tab-shared.pdf")).toBeNull());
        const notice = screen.getByRole("alert").textContent ?? "";
        expect(notice).toContain("该文章已经取消分享");
        expect(notice).toContain("如果需要继续阅读");
        expect(notice).toContain("王老师");
    });

    it("a line sent before any library list survives an empty list", async () => {
        send.mockResolvedValue(undefined);
        addShareNote.mockResolvedValue([
            { id: "cn_def", author: "me@example.com", text: "我也这么看", createdAt: "2026-10-25T00:01:00Z" },
        ]);
        let releaseFirst: (value: ListLibraryResult) => void = () => undefined;
        listLibrary.mockReset();
        listLibrary.mockResolvedValue({
            items: [{
                path: "C:/library/shared.pdf",
                source: "cloud",
                token: "cd_0123456789abcdef0123456789abcdef",
                mine: false,
                notes: [],
            }],
            notice: "",
        });
        listLibrary.mockImplementationOnce(() => new Promise((resolve) => { releaseFirst = resolve; }));
        boot.mockResolvedValue({ paths: ["C:/library/shared.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-input");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "我也这么看" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(addShareNote).not.toHaveBeenCalled();
        await act(async () => { releaseFirst({ items: [], notice: "" }); });
        expect(addShareNote).not.toHaveBeenCalled();
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(addShareNote).toHaveBeenCalledWith("C:/library/shared.pdf", "我也这么看"));
        expect(addShareNote).toHaveBeenCalledTimes(1);
        expect((await screen.findByTestId("file-companion-annotation-cn_def")).textContent).toContain("我也这么看");
    });

    it("reloads the document library when a sync fails", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        syncShare.mockRejectedValue(new Error("只有分享者可以同步更新"));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.md");
        // 同步 lives in the 分享 popup; the list underneath still refreshes
        // after a failed sync, so the revision that did land is shown.
        fireEvent.click(screen.getByTestId("file-companion-share-open"));
        await screen.findByTestId("file-companion-share-dialog");
        const calls = listLibrary.mock.calls.length;
        fireEvent.click(screen.getByTestId("file-companion-library-sync"));
        await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("只有分享者可以同步更新"));
        await waitFor(() => expect(listLibrary.mock.calls.length).toBeGreaterThan(calls));
        await waitFor(() => expect((screen.getByTestId("file-companion-library-sync") as HTMLButtonElement).disabled).toBe(false));
    });

    it("applies the host's highlights from a newer share project", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.pdf"] });
        open.mockImplementation(async (path: string) => doc(path));
        installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-a.pdf");
        const mark = { id: "m_share", page: 1, color: "yellow" as const, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "分享者标注" };
        const chatMark = { message: 0, color: "yellow" as const, start: 0, end: 2, text: "答" };
        // A translation and a note this reader made stay local; a host note from
        // the previous revision is replaced by the newer project.
        const localTranslation = { id: "t_local", text: "本地原文", translation: "本地译文", parts: [{ page: 1, rects: [{ x: 0.2, y: 0.3, w: 0.2, h: 0.03 }] }] };
        const localAnnotation = { id: "a_mine", kind: "annotation" as const, text: "我的原文", translation: "我的本地批注", parts: [{ page: 1, rects: [{ x: 0.3, y: 0.4, w: 0.2, h: 0.03 }] }] };
        const staleHostAnnotation = { id: "a_old", kind: "annotation" as const, shared: true, text: "旧原文", translation: "主持人上一版批注", parts: [{ page: 1, rects: [{ x: 0.35, y: 0.45, w: 0.2, h: 0.03 }] }] };
        const annotation = { id: "a_share", kind: "annotation" as const, text: "分享原文", translation: "分享者批注", parts: [{ page: 1, rects: [{ x: 0.4, y: 0.5, w: 0.2, h: 0.03 }] }] };
        const localDrawing = { id: "d_mine", page: 1, color: "green" as const, tool: "line" as const, points: [{ x: 0.2, y: 0.2 }, { x: 0.4, y: 0.3 }] };
        const staleHostDrawing = { id: "d_old", page: 1, color: "pink" as const, tool: "box" as const, shared: true, points: [{ x: 0.1, y: 0.1 }, { x: 0.2, y: 0.2 }] };
        const drawing = { id: "d_share", page: 1, color: "blue" as const, tool: "box" as const, points: [{ x: 0.3, y: 0.3 }, { x: 0.5, y: 0.5 }] };
        savePdfTranslations("C:/notes/a.pdf", [localTranslation, localAnnotation, staleHostAnnotation]);
        savePdfDrawings("C:/notes/a.pdf", [localDrawing, staleHostDrawing]);
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            updates: [{
                path: "C:/notes/a.pdf",
                project: true,
                highlights: JSON.stringify([mark]),
                chatMarks: JSON.stringify([chatMark]),
                annotations: JSON.stringify([annotation]),
                drawings: JSON.stringify([drawing]),
                messages: [{ role: "assistant", text: "分享者的新回答" }],
            }],
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await waitFor(() => expect(loadPdfMarks("C:/notes/a.pdf")).toEqual([mark]));
        expect(loadChatMarks("C:/notes/a.pdf")).toEqual([chatMark]);
        await waitFor(() => expect(loadPdfTranslations("C:/notes/a.pdf").map((item) => item.id)).toEqual(["t_local", "a_mine", "a_share"]));
        const restored = loadPdfTranslations("C:/notes/a.pdf");
        expect(restored[2]).toMatchObject({ kind: "annotation", translation: "分享者批注", shared: true });
        expect(restored[1].shared).toBeUndefined();
        await waitFor(() => expect(loadPdfDrawings("C:/notes/a.pdf").map((item) => item.id)).toEqual(["d_mine", "d_share"]));
        const restoredDrawings = loadPdfDrawings("C:/notes/a.pdf");
        expect(restoredDrawings[1]).toMatchObject({ tool: "box", color: "blue", shared: true });
        expect(restoredDrawings[0].shared).toBeUndefined();
    });

    it("a synced transcript replaces the open chat and drops the reply that was already running", async () => {
        let rejectSend: (err: Error) => void = () => undefined;
        send.mockImplementation(() => new Promise<void>((_resolve, reject) => { rejectSend = reject; }));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "user", text: "旧问题" }, { role: "assistant", text: "旧回答" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("旧回答");
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "还在问" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        expect(screen.getByText("还在问")).toBeTruthy();
        listLibrary.mockResolvedValue({
            items: [],
            notice: "",
            updates: [{
                path: "C:/notes/a.md",
                project: true,
                highlights: "[]",
                messages: [
                    { role: "user", text: "分享者的新问题" },
                    { role: "assistant", text: "分享者的新回答" },
                ],
            }],
        });
        await act(async () => { window.dispatchEvent(new Event("focus")); });
        await screen.findByText("分享者的新回答");
        expect(screen.queryByText("旧回答")).toBeNull();
        expect(screen.queryByText("还在问")).toBeNull();
        await act(async () => { rejectSend(new Error("cancelled")); });
        expect(screen.queryByText("cancelled")).toBeNull();
        await waitFor(() => expect(screen.getByTestId("file-companion-send").textContent).toBe("发送"));
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
        expect(fileCompanionInputHistory()).toContain("问 B");
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

    it("labels note frames as 关键帧", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/frames.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "开场先讲了频率派。",
                    figures: [{ label: "板书 0:03", caption: "", path: "C:/notes/img/frame.png" }],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("开场先讲了频率派。");
        await waitFor(() => expect(screen.getByText("关键帧")).toBeTruthy());
        expect(screen.queryByText("原文配图")).toBeNull();
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

    it("shows the file the answer produced", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/generated.md"] });
        open.mockImplementation(async (path: string) => doc(path));
        const runtime = installRuntime();
        renderWindow();
        fireEvent.change(await screen.findByTestId("file-companion-input"), { target: { value: "整理成笔记 PDF" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        act(() => {
            runtime.emit("ai-assistant-response", JSON.stringify({
                session_key: "file-companion:generated.md",
                text: "笔记 PDF 已经生成。",
                request_id: "gen",
                file_companion_files: [{ label: "复习笔记.pdf", path: "C:/maclaw/data/file-companion/results/x/复习笔记.pdf", size: 2048 }],
            }));
        });
        await screen.findByText("笔记 PDF 已经生成。");
        await waitFor(() => expect(screen.getByTestId("companion-file-card-gen:assistant")).toBeTruthy());
        expect(screen.getByText("复习笔记.pdf")).toBeTruthy();
    });

    it("saves the produced file where the reader wants it", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/generated.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "assistant", text: "笔记 PDF 已经生成。", files: [{ label: "复习笔记.pdf", path: "C:/maclaw/data/file-companion/results/x/复习笔记.pdf" }] },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("笔记 PDF 已经生成。");
        await waitFor(() => expect(screen.getByTestId("companion-file-card-restored:0")).toBeTruthy());
        fireEvent.click(screen.getByTestId("companion-file-save"));
        await waitFor(() => {
            expect(saveResult).toHaveBeenCalledWith("C:/maclaw/data/file-companion/results/x/复习笔记.pdf");
        });
        await waitFor(() => {
            expect(screen.getByTestId("companion-file-save").textContent).toBe("已保存");
        });
    });

    it("offers every file a turn produced on its own row", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/generated.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                {
                    role: "assistant",
                    text: "两份笔记都生成了。",
                    files: [
                        { label: "复习笔记.pdf", path: "C:/out/复习笔记.pdf" },
                        { label: "讲义.pdf", path: "C:/out/讲义.pdf" },
                    ],
                },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("两份笔记都生成了。");
        await waitFor(() => expect(screen.getByTestId("companion-file-card-restored:0")).toBeTruthy());
        expect(screen.getByText("复习笔记.pdf")).toBeTruthy();
        expect(screen.getByText("讲义.pdf")).toBeTruthy();
        const saves = screen.getAllByTestId("companion-file-save");
        expect(saves.length).toBe(2);
        fireEvent.click(saves[1]);
        await waitFor(() => {
            expect(saveResult).toHaveBeenCalledWith("C:/out/讲义.pdf");
        });
    });

    it("shows no drawings when the reply names none", async () => {        boot.mockResolvedValue({ paths: ["C:/notes/plain.md"] });
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

    it("packs the open file with its highlights and restores an imported project", async () => {
        hydratePdfMarks({
            "c:/paper.pdf": [{ id: "m1", page: 1, color: "yellow", rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }], text: "方法" }],
        });
        const annotation = { id: "a1", kind: "annotation" as const, text: "方法", translation: "这一段要重点复核", parts: [{ page: 1, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }] }] };
        hydratePdfTranslations({ "c:/paper.pdf": [annotation] });
        const drawing = { id: "d1", page: 1, color: "yellow" as const, tool: "line" as const, points: [{ x: 0.1, y: 0.2 }, { x: 0.3, y: 0.4 }] };
        hydratePdfDrawings({ "c:/paper.pdf": [drawing] });
        boot.mockResolvedValue({ paths: ["C:/paper.pdf"] });
        open.mockResolvedValue(doc("C:/paper.pdf", {
            messages: [{ role: "user", text: "这段讲什么" }, { role: "assistant", text: "讲方法", reasoning: "先看摘要" }],
        }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-tab-paper.pdf");
        fireEvent.click(screen.getByTestId("file-companion-export-project"));
        await waitFor(() => expect(exportProject).toHaveBeenCalledWith("C:/paper.pdf", expect.stringContaining("yellow")));
        const packed = JSON.parse(exportProject.mock.calls[0][1] as string);
        expect(packed.highlights).toHaveLength(1);
        expect(packed.chatMarks).toEqual([]);
        expect(packed.annotations).toEqual([annotation]);
        expect(packed.drawings).toEqual([drawing]);
        expect(screen.getByText("这段讲什么")).toBeTruthy();

        hydratePdfMarks({});
        const importedChatMark = { message: 0, color: "green" as const, start: 0, end: 2, text: "答" };
        open.mockResolvedValue(doc("C:/restored/paper.pdf", {
            messages: [{ role: "user", text: "打包里的问题" }, { role: "assistant", text: "打包里的回答" }],
            highlights: [{ id: "m9", page: 1, color: "pink", rects: [{ x: 0.2, y: 0.3, w: 0.2, h: 0.05 }], text: "方法" }],
            chatMarks: [importedChatMark],
            annotations: [annotation],
            drawings: [drawing],
            imported: true,
        }));
        runtime.emit("file-companion:open", ["C:/project.zip"]);
        await screen.findByText("打包里的问题");
        expect(open).toHaveBeenCalledWith("C:/project.zip");
        const restored = loadPdfMarks("C:/restored/paper.pdf");
        expect(restored).toHaveLength(1);
        expect(restored[0].color).toBe("pink");
        expect(loadChatMarks("C:/restored/paper.pdf")).toEqual([importedChatMark]);
        expect(loadPdfTranslations("C:/restored/paper.pdf")[0]).toMatchObject({ id: "a1", kind: "annotation", shared: true });
        expect(loadPdfDrawings("C:/restored/paper.pdf")[0]).toMatchObject({ id: "d1", shared: true });
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
        expect(fileCompanionInputHistory()).toContain("先问这句");
        expect(fileCompanionInputHistory()).not.toContain("改成下一句");
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
        expect(fileCompanionInputHistory()).toEqual([]);
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
        expect(fileCompanionInputHistory()).toEqual([]);
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

    it("treats a typed paper command as the same paper turn as the button", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { editable: true, readOnly: false, content: "alpha", sessionId: "a.md" }));
        let release: (value?: unknown) => void = () => undefined;
        send.mockImplementation(() => new Promise((resolve) => { release = resolve; }));
        const runtime = installRuntime();
        renderWindow();
        await screen.findByTestId("file-companion-editor");
        const input = screen.getByTestId("file-companion-input") as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: "read the paper" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
        const sent = send.mock.calls[0][0] as Record<string, unknown>;
        expect(sent.text).toBe("read the paper");
        expect(sent.purpose).toBe("paper");
        expect(sent.selection).toBeUndefined();
        expect(screen.getByTestId("file-companion-message-user").textContent).toBe("read the paper");
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-paper").textContent).toBe("正在解读…");
        });
        const requestID = String(sent.request_id);
        runtime.emit("ai-assistant-token", JSON.stringify({ session_key: "file-companion:a.md", text: "SECRET_MARKDOWN", request_id: requestID }));
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-answer").textContent).toContain("正在生成论文解读");
        });
        expect(screen.getByTestId("file-companion-answer").textContent).not.toContain("SECRET_MARKDOWN");
        runtime.emit("ai-assistant-response", JSON.stringify({
            session_key: "file-companion:a.md",
            text: "SECRET_MARKDOWN",
            local_file_path: "C:/maclaw/data/file-companion/results/a.md/a-论文解读.pdf",
            request_id: requestID,
        }));
        await act(async () => { release(); await Promise.resolve(); });
        await waitFor(() => {
            expect(screen.getByTestId("file-companion-answer").textContent).toContain("已生成论文解读");
        });
        expect(screen.getByTestId("file-companion-answer").textContent).not.toContain("SECRET_MARKDOWN");
        expect(screen.getByTestId("task-result-view-btn")).toBeTruthy();
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
        expect(fileCompanionInputHistory()).toEqual([]);
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
        const history = fileCompanionInputHistory();
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
        expect(fileCompanionInputHistory()).toEqual([]);
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "assistant", content: "开始画：\n\n```mermaid\nflowchart LR\n  A --> B" },
        ]));
        expect(renderMermaidFenceToPNGDataURL).not.toHaveBeenCalled();
        expect(screen.queryByText(/未能读取/)).toBeNull();
    });

    // A marker inside an unclosed fence is still part of that block. Drawing
    // it would put a picture in the PDF where the window shows source.
    it("does not draw a diagram that sits inside an unclosed fence", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        renderMermaidFenceToPNGDataURL.mockResolvedValue("data:image/png;base64,DIAGRAM");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{
                role: "assistant",
                text: "~~~mermaid\nintro\n```mermaid\nflowchart LR\n  A --> B\n```\n",
            }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText(/intro/);

        exportChatAsFile();
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        expect(renderMermaidFenceToPNGDataURL).not.toHaveBeenCalled();
        const [, turns] = exportChatPdf.mock.calls[0] as [string, { content: string }[]];
        expect(turns[0].content).toContain("~~~mermaid");
        expect(turns[0].content).toContain("```mermaid");
        expect(turns[0].content).toContain("flowchart LR");
        expect(turns[0].content).not.toContain("![](");
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
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
        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
        // Both sides travel together: an answer alone cannot be read on its own.
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "这段讲了什么？" },
            { role: "assistant", content: "## 要点\n\n第一点最重要。" },
            { role: "user", content: "第二点呢？" },
            { role: "assistant", content: "第二点讲的是边界。" },
        ]));
        await screen.findByText("已导出 PDF");
        expect(showItemInFolder).toHaveBeenCalledWith("C:/out/a-伴读对话.pdf");
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

        exportChatAsFile();
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

        exportChatAsFile();
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

        exportChatAsFile();
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        await act(async () => { await Promise.resolve(); });
        expect(screen.queryByText("已导出 PDF")).toBeNull();
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("asks where to save, then selects the file in Explorer", async () => {
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        expect(exportChatPdf).not.toHaveBeenCalled();
        const menu = screen.getByTestId("file-companion-export-chat-dest");
        expect(menu.textContent).toContain("导出文件");
        expect(menu.textContent).toContain("导出到云盘");
        expect(screen.getByTestId("file-companion-titlebar").className).toContain("file-companion-titlebar--menu-open");
        fireEvent.mouseDown(screen.getByTestId("file-companion-export-chat-scrim"));
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();
        expect(exportChatPdf).not.toHaveBeenCalled();

        exportChatAsFile();
        await screen.findByText("已导出 PDF");
        expect(showItemInFolder).toHaveBeenCalledWith("C:/out/a-伴读对话.pdf");
    });

    it("closes the destination panel on Escape and on a click outside it", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");
        fireEvent.click(screen.getByTestId("file-companion-fullscreen"));
        await waitFor(() => expect(screen.getByTestId("file-companion-fullscreen").textContent).toBe("恢复原始大小"));

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        expect(screen.getByTestId("file-companion-export-chat-file").tagName).toBe("BUTTON");
        expect(screen.getByTestId("file-companion-export-chat-cloud").tagName).toBe("BUTTON");
        fireEvent.mouseDown(screen.getByTestId("file-companion-brand"));
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();
        expect(windowUnfullscreen).not.toHaveBeenCalled();
        expect(exportChatPdf).not.toHaveBeenCalled();
        expect(document.activeElement).toBe(screen.getByTestId("file-companion-export-chat"));
    });

    it("does not bring the destination panel back after switching files", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "user", text: path.endsWith("a.md") ? "甲的问题" : "乙的问题" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("甲的问题");
        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        expect(screen.getByTestId("file-companion-export-chat-dest")).toBeTruthy();

        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        await screen.findByText("乙的问题");
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();

        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        await screen.findByText("甲的问题");
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();
        expect(exportChatPdf).not.toHaveBeenCalled();

        fireEvent.click(screen.getByTestId("file-companion-export-chat-menu"));
        expect(screen.getByTestId("file-companion-export-chat-tex")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        await screen.findByText("乙的问题");
        expect(screen.queryByTestId("file-companion-export-chat-tex")).toBeNull();
        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        await screen.findByText("甲的问题");
        expect(screen.queryByTestId("file-companion-export-chat-tex")).toBeNull();
    });

    it("closes the LaTeX row when the chat is cleared", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是摘要" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是摘要");

        fireEvent.click(screen.getByTestId("file-companion-export-chat-menu"));
        expect(screen.getByTestId("file-companion-export-chat-tex")).toBeTruthy();
        fireEvent.change(screen.getByTestId("file-companion-input"), { target: { value: "  ／CLEAR  " } });
        fireEvent.keyDown(screen.getByTestId("file-companion-input"), { key: "Enter" });
        await waitFor(() => expect(screen.queryByTestId("file-companion-export-chat-tex")).toBeNull());
        expect(screen.getByTestId("file-companion-chat-empty")).toBeTruthy();
    });

    it("leaves export disabled when the chat is only a paper card", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "已生成论文解读", resultPath: "C:/out/paper.pdf" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("已生成论文解读");

        const button = screen.getByTestId("file-companion-export-chat");
        expect(button.hasAttribute("disabled")).toBe(true);
        expect(button.getAttribute("title")).toBe("没有可导出的正文");
        fireEvent.click(button);
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();
        expect(screen.queryByText("没有可导出的内容")).toBeNull();
        expect(exportChatPdf).not.toHaveBeenCalled();
        expect(button.textContent).toBe("导出 PDF");
    });

    it("exports the question beside a paper card and leaves the card out", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "这篇讲什么？" },
                { role: "assistant", text: "已生成论文解读", resultPath: "C:/out/paper.pdf" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这篇讲什么？");

        exportChatAsFile();
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "这篇讲什么？" },
        ]));
    });

    it("says the page is not open when a web export has no session", async () => {
        exportChatCloud.mockRejectedValue(new Error("web session is not open"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        fireEvent.click(screen.getByTestId("file-companion-export-chat-cloud"));
        await screen.findByText("这个页面没有打开");
        expect(screen.queryByText("web session is not open")).toBeNull();
    });

    it("says the file is not open when the export has no grant", async () => {
        exportChatPdf.mockRejectedValue(new Error("path is outside the open file"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        exportChatAsFile();
        await screen.findByText("这个文件没有打开");
        expect(screen.queryByText("path is outside the open file")).toBeNull();
    });

    it("marks the conversation export busy before its figures finish loading", async () => {
        const pending: Array<(value: string) => void> = [];
        figureDataURL.mockImplementation(() => new Promise<string>((resolve) => { pending.push(resolve); }));
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        exportChatCloud.mockResolvedValue({ id: "draft-1", title: "a.pdf", duplicate: false });
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{
                role: "assistant",
                text: "图中给出结果。",
                figures: [{ label: "图 1", caption: "结果", path: "C:/papers/busy-export.png" }],
            }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("图中给出结果。");

        exportChatAsFile();
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat").getAttribute("aria-busy")).toBe("true"));
        expect(exportChatPdf).not.toHaveBeenCalled();
        expect(exportChatCloud).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        expect(screen.queryByTestId("file-companion-export-chat-dest")).toBeNull();

        await act(async () => { pending.forEach((resolve) => resolve("data:image/png;base64,QQ==")); });
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        expect(exportChatCloud).not.toHaveBeenCalled();
    });

    it("keeps an in-flight export busy on its own file", async () => {
        const pending: Array<(value: string) => void> = [];
        figureDataURL.mockImplementation(() => new Promise<string>((resolve) => { pending.push(resolve); }));
        exportChatPdf.mockResolvedValue("C:/out/a-伴读对话.pdf");
        boot.mockResolvedValue({ paths: ["C:/notes/a.md", "C:/notes/b.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: path.endsWith("a.md")
                ? [{ role: "assistant", text: "图中给出结果。", figures: [{ label: "图 1", caption: "结果", path: "C:/papers/busy-two-tabs.png" }] }]
                : [{ role: "assistant", text: "乙的回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("图中给出结果。");

        exportChatAsFile();
        await waitFor(() => expect(screen.getByTestId("file-companion-export-chat").textContent).toBe("正在导出…"));

        fireEvent.click(screen.getByTestId("file-companion-tab-b.md"));
        await screen.findByText("乙的回复。");
        const other = screen.getByTestId("file-companion-export-chat");
        expect(other.textContent).toBe("导出 PDF");
        expect(other.getAttribute("aria-busy")).toBe("false");
        expect(other.hasAttribute("disabled")).toBe(false);

        fireEvent.click(screen.getByTestId("file-companion-tab-a.md"));
        await screen.findByText("图中给出结果。");
        expect(screen.getByTestId("file-companion-export-chat").textContent).toBe("正在导出…");

        await act(async () => { pending.forEach((resolve) => resolve("data:image/png;base64,QQ==")); });
        await waitFor(() => expect(exportChatPdf).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("file-companion-export-chat").textContent).toBe("导出 PDF");
    });

    it("uploads the generated transcript to the cloud drive", async () => {
        exportChatCloud.mockResolvedValue({ id: "draft-1", title: "a-伴读对话.pdf", duplicate: false });
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [
                { role: "user", text: "这段讲了什么？" },
                { role: "assistant", text: "第一点最重要。" },
            ],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("第一点最重要。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        fireEvent.click(screen.getByTestId("file-companion-export-chat-cloud"));
        await waitFor(() => expect(exportChatCloud).toHaveBeenCalledWith("C:/notes/a.md", [
            { role: "user", content: "这段讲了什么？" },
            { role: "assistant", content: "第一点最重要。" },
        ]));
        expect(exportChatPdf).not.toHaveBeenCalled();
        expect(uploadCloud).not.toHaveBeenCalled();
        expect(showItemInFolder).not.toHaveBeenCalled();
        await screen.findByText("已上传到云盘，手机端「文档」可打开");
    });

    it("names the existing cloud copy when the transcript was already uploaded", async () => {
        exportChatCloud.mockResolvedValue({ id: "draft-9", title: "a-伴读对话.pdf", duplicate: true, duplicate_of_title: "旧对话" });
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        fireEvent.click(screen.getByTestId("file-companion-export-chat-cloud"));
        await screen.findByText("云盘中已有相同内容（旧对话），未再次上传");
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("says the cloud upload failed when the drive rejects the transcript", async () => {
        exportChatCloud.mockRejectedValue(new Error("upload mobile document failed: HTTP 500 no"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        fireEvent.click(screen.getByTestId("file-companion-export-chat-cloud"));
        await screen.findByText("上传到云盘失败，请稍后重试");
        expect(screen.queryByText(/upload mobile document failed/)).toBeNull();
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("says the save location failed when the folder name is invalid", async () => {
        exportChatPdf.mockRejectedValue(new Error("创建目录失败: mkdir C:\\Users\\Director\\out: The directory name is invalid."));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        exportChatAsFile();
        await screen.findByText("无法保存到该位置");
        expect(screen.queryByText("不支持上传文件夹")).toBeNull();
        expect(screen.queryByText(/directory name is invalid/)).toBeNull();
        expect(showItemInFolder).not.toHaveBeenCalled();
    });

    it("says the disk is full when the PDF cannot be written", async () => {
        exportChatPdf.mockRejectedValue(new Error("写入 PDF 失败: write C:\\out\\a.pdf: There is not enough space on the disk."));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        exportChatAsFile();
        await screen.findByText("磁盘已满");
        expect(screen.queryByText("无法保存到该位置")).toBeNull();
        expect(screen.queryByText(/not enough space/)).toBeNull();
    });

    it("says there is no write permission when the PDF is refused", async () => {
        exportChatPdf.mockRejectedValue(new Error("写入 PDF 失败: open C:\\out\\a.pdf: Access is denied."));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        exportChatAsFile();
        await screen.findByText("没有写入权限");
        expect(screen.queryByText("无法保存到该位置")).toBeNull();
        expect(screen.queryByText(/Access is denied/)).toBeNull();
    });

    it("says a folder cannot be uploaded", async () => {
        uploadCloud.mockRejectedValue(new Error("path is a directory"));
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, { canExport: true }));
        installRuntime();
        renderWindow();
        fireEvent.click(await screen.findByTestId("file-companion-upload"));
        await screen.findByText("不支持上传文件夹");
    });

    it("closes the document library when the export panel opens", async () => {
        boot.mockResolvedValue({ paths: ["C:/notes/a.md"] });
        open.mockImplementation(async (path: string) => doc(path, {
            messages: [{ role: "assistant", text: "这是回复。" }],
        }));
        installRuntime();
        renderWindow();
        await screen.findByText("这是回复。");

        fireEvent.click(screen.getByTestId("file-companion-library"));
        await screen.findByTestId("file-companion-library-panel");
        fireEvent.click(screen.getByTestId("file-companion-export-chat"));
        expect(screen.getByTestId("file-companion-export-chat-dest")).toBeTruthy();
        expect(screen.queryByTestId("file-companion-library-panel")).toBeNull();
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
        const chatAnchor = screen.getByTestId("file-companion-export-anchor");
        expect(screen.getByTestId("file-companion-paper").nextElementSibling).toBe(chatAnchor);
        expect(chatAnchor.contains(chatExport)).toBe(true);
        expect(chatAnchor.nextElementSibling).toBe(screen.getByTestId("file-companion-export-project"));
        expect(screen.getByTestId("file-companion-export-project").nextElementSibling).toBe(screen.getByTestId("file-companion-import"));
        fireEvent.click(chatExport);
        expect(exportChatPdf).not.toHaveBeenCalled();
        expect(screen.getByTestId("file-companion-export-chat-dest")).toBeTruthy();
        fireEvent.click(screen.getByTestId("file-companion-export-chat-file"));
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
        openWeb.mockRejectedValue(new Error("本地文件请用「打开文档或导入项目」，这里不接受 file:// 和其他协议"));
        await openField("file:///C:/media/clip.mp4");
        await screen.findByText("本地文件请用「打开文档或导入项目」，这里不接受 file:// 和其他协议");
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

    // jsdom runs no layout, so only a source assertion can pin this: the narrow
    // inset beside a native player is 268px, and the download-progress label is
    // the only sign that a click started a download. Hiding the whole action
    // column there made a slow download look like a dead click.
    it("keeps the download-progress label in the narrow player inset", () => {
        const source = css();
        // Only the unconditional rule is a defect. A rule that drops the column
        // when the row is idle is the intended shape: 打开 is a hover hint, and
        // the label appears the moment aria-busy flips.
        const unconditional = /\.file-companion-arxiv-search--beside-player \.file-companion-arxiv-hit-action\s*\{([^}]*)\}/.exec(source);
        const hidesUnconditionally = unconditional ? /display:\s*none/.test(unconditional[1]) : false;
        expect(hidesUnconditionally, "no rule may hide the action column unconditionally").toBe(false);
        // The rail and the idle hover hint may go; the busy state must survive.
        expect(source).toMatch(/\.file-companion-arxiv-search--beside-player \.file-companion-arxiv-hit-index\s*\{[^}]*display:\s*none/);
        expect(source).toMatch(/\.file-companion-arxiv-search--beside-player \.file-companion-arxiv-hit:not\(\[aria-busy="true"\]\) \.file-companion-arxiv-hit-action\s*\{[^}]*display:\s*none/);
        // The column must not take width from the citation at 268px: it wraps
        // under it instead, so the title stays one line and the abstract keeps
        // the card's full inner width.
        expect(source).toMatch(/\.file-companion-arxiv-search--beside-player \.file-companion-arxiv-hit\s*\{[^}]*flex-wrap:\s*wrap/);
        expect(source).toMatch(/\.file-companion-arxiv-search--beside-player \.file-companion-arxiv-hit-body\s*\{[^}]*flex-basis:\s*100%/);
    });

    it("hangs the open menu over the chat when a native player would cover the centre", () => {
        expect(css()).toMatch(/\.file-companion-open-menu--beside-player\s*\{[^}]*left:\s*auto/);
        expect(css()).toMatch(/\.file-companion-open-menu--beside-player\s*\{[^}]*right:\s*8px/);
    });

    it("hangs the share password box over the chat when a native player would cover the centre", () => {
        expect(css()).toMatch(/\.file-companion-share-password--beside-player\s*\{[^}]*justify-content:\s*flex-end/);
        expect(css()).toMatch(/\.file-companion-share-password--beside-player\s*\{[^}]*padding-right:\s*8px/);
        expect(css()).toMatch(/\.file-companion-share-password--beside-player \.file-companion-share-password-card\s*\{[^}]*width:\s*min\(268px/);
        // The card is the shared dialog shell. A padding or gap on this
        // rule insets the header and footer a second time.
        const cardRule = css().match(/(?:^|})\s*\.file-companion-share-password-card\s*\{([^}]*)\}/);
        expect(cardRule?.[1] ?? "").not.toMatch(/padding|gap/);
        expect(css()).toMatch(/\.file-companion-dialog-input::-ms-reveal,[\s\S]*?\{[^}]*display:\s*none/);
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
            ".file-companion-library-anchor",
            ".file-companion-tool--fullscreen",
            ".file-companion-tool--close",
        ]) {
            expect(declaresNoDrag(source, selector), `${selector} must opt out of the drag region`).toBe(true);
        }
    });

    // The bar's flex gap does not apply inside the anchor, so the pair
    // carries the same 8px itself. The strip stays no-drag.
    it("separates 文档管理 and 阅读群组 by the title-bar tool gap", () => {
        const source = css();
        expect(source).toMatch(/\.file-companion-library-anchor\s*\{[^}]*gap:\s*8px/);
        expect(declaresNoDrag(source, ".file-companion-library-anchor")).toBe(true);
    });
});

describe("companionAnnotationMarkdown", () => {
    it("joins a name broken across a line and keeps CJK bold together", () => {
        expect(companionAnnotationMarkdown("**Daniel Garcia\nPeña**")).toBe("**Daniel Garcia Peña**");
        expect(companionAnnotationMarkdown("**单一作\n者**")).toBe("**单一作者**");
    });

    it("does not glue a following list or a fenced block into the bold", () => {
        expect(companionAnnotationMarkdown("**未闭合\n- 列表项")).toBe("**未闭合\n- 列表项");
        expect(companionAnnotationMarkdown("**未闭合\n\n下一段")).toBe("**未闭合\n\n下一段");
        expect(companionAnnotationMarkdown("```\n**keep\nme**\n```")).toBe("```\n**keep\nme**\n```");
    });

    it("keeps a wrapped name bold after the chat list normalizer", () => {
        const raw = "这篇综述是**单一作者**完成的： **Daniel Garcia\nPeña** - 联系邮箱： info@iamdgarcia.com - ORCID：\n0000-0002-1456-4785 - 封面日期： June 16, 2026 -";
        const prepared = normalizeInlineListMarkers(companionAnnotationMarkdown(raw));
        expect(prepared).toContain("**Daniel Garcia Peña**");
        expect(prepared).toContain("**单一作者**");
    });
});
