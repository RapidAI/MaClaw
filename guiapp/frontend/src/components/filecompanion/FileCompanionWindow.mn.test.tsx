// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { setCompanionUiLang } from "../../i18n/mnCompanion";
import { DialogProvider } from "../CustomDialog";
import { ToastProvider } from "../Toast";
import { GetCurrentLanguage } from "../../../wailsjs/go/main/App";
import FileCompanionWindow, { COMPANION_UI_LANGUAGE_EVENT } from "./FileCompanionWindow";

vi.mock("../../../wailsjs/go/main/App", () => ({
    GetCurrentLanguage: vi.fn(async () => "mn"),
    GetFileCompanionBoot: vi.fn(async () => ({ paths: [] })),
    GetBrandInfo: vi.fn(async () => ({ displayName: "MaClaw" })),
    ListFileCompanionLibrary: vi.fn(async () => ({ items: [], notice: "登录后可以查看云端分享" })),
    ListFileCompanionGroups: vi.fn(async () => [{
        id: "cg_0123456789abcdef0123456789abcdef",
        name: "уншлагын бүлэг",
        owner: "me@example.com",
        admin: true,
        icon: "book",
        members: [],
    }]),
    FileCompanionResumePaths: vi.fn(async () => []),
    FileCompanionUIReady: vi.fn(async () => []),
    FileCompanionPlayerSupported: vi.fn(async () => false),
    ChooseFileCompanionFiles: vi.fn(async () => []),
    FileCompanionOpen: vi.fn(async () => ({})),
    SendFileCompanionMessage: vi.fn(async () => undefined),
}));

vi.mock("../../../wailsjs/runtime", () => ({
    Quit: vi.fn(),
    WindowHide: vi.fn(),
    WindowFullscreen: vi.fn(),
    WindowUnfullscreen: vi.fn(),
    WindowGetSize: vi.fn(async () => ({ w: 1280, h: 800 })),
    WindowGetPosition: vi.fn(async () => ({ x: 0, y: 0 })),
    WindowSetSize: vi.fn(),
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

vi.mock("../ai/PptxPreviewPanel", () => ({
    PptxPreviewPanel: () => null,
}));
vi.mock("../ai/DocxPreviewPanel", () => ({
    DocxPreviewPanel: () => null,
}));
vi.mock("../ai/PdfPreviewPanel", () => ({
    PdfPreviewPanel: () => null,
}));
vi.mock("../ai/CompanionPdfReader", () => ({
    CompanionPdfReader: () => null,
}));

type EventCb = (payload: unknown) => void;

function installRuntime() {
    const events = new Map<string, EventCb>();
    (window as unknown as { runtime?: unknown }).runtime = {
        EventsOn: (name: string, cb: EventCb) => { events.set(name, cb); },
        EventsOff: (name: string) => { events.delete(name); },
    };
    return {
        emit: (name: string, payload: unknown) => events.get(name)?.(payload),
    };
}

afterEach(() => {
    cleanup();
    setCompanionUiLang("zh-Hans");
    vi.mocked(GetCurrentLanguage).mockResolvedValue("mn");
    delete (window as unknown as { runtime?: unknown }).runtime;
});

describe("file companion Mongolian chrome", () => {
    it("uses Cyrillic for the open control and the library once the language arrives", async () => {
        render(
            <ToastProvider>
                <DialogProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </DialogProvider>
            </ToastProvider>,
        );
        await waitFor(() => expect(screen.getByTestId("file-companion-choose").textContent).toBe("Нээх"));
        expect(screen.getByTestId("file-companion-brand").textContent).toBe("MaClaw дагалдан унших");
        fireEvent.click(screen.getByTestId("file-companion-library"));
        await waitFor(() => expect(screen.getByTestId("file-companion-library-panel").textContent || "").toContain("Баримтын сан"));
        const panel = screen.getByTestId("file-companion-library-panel").textContent || "";
        expect(panel).not.toMatch(/文档管理|还没有本机项目/);
        expect(screen.getByTestId("file-companion-library-notice").textContent).toBe("Нэвтэрсний дараа үүлэн хуваалцалтыг харж болно");
        fireEvent.click(screen.getByTestId("file-companion-groups-open"));
        const star = await screen.findByTestId("file-companion-group-icon-cg_0123456789abcdef0123456789abcdef-star");
        expect(star.getAttribute("aria-label")).toBe("Од");
        expect(star.getAttribute("title")).toBe("Од");
        expect(screen.getByTestId("file-companion-group-icon-cg_0123456789abcdef0123456789abcdef-book").getAttribute("aria-label")).toBe("Ном");
    });

    it("returns to Chinese when the main window switches the language back", async () => {
        const runtime = installRuntime();
        render(
            <ToastProvider>
                <DialogProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </DialogProvider>
            </ToastProvider>,
        );
        await waitFor(() => expect(screen.getByTestId("file-companion-choose").textContent).toBe("Нээх"));
        expect(screen.getByTestId("file-companion-brand").textContent).toBe("MaClaw дагалдан унших");
        act(() => {
            runtime.emit(COMPANION_UI_LANGUAGE_EVENT, "zh-Hans");
        });
        expect(screen.getByTestId("file-companion-choose").textContent).toBe("打开");
        expect(screen.getByTestId("file-companion-brand").textContent).toBe("MaClaw伴读");
    });

    it("applies a language event delivered as a one-element array", async () => {
        const runtime = installRuntime();
        render(
            <ToastProvider>
                <DialogProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </DialogProvider>
            </ToastProvider>,
        );
        await waitFor(() => expect(screen.getByTestId("file-companion-choose").textContent).toBe("Нээх"));
        act(() => {
            runtime.emit(COMPANION_UI_LANGUAGE_EVENT, ["zh-Hans"]);
        });
        expect(screen.getByTestId("file-companion-choose").textContent).toBe("打开");
        expect(screen.getByTestId("file-companion-brand").textContent).toBe("MaClaw伴读");
    });

    it("keeps the live language when the startup read finishes later", async () => {
        let resolveLang: (value: string) => void = () => undefined;
        vi.mocked(GetCurrentLanguage).mockImplementation(() => new Promise((resolve) => {
            resolveLang = resolve;
        }));
        const runtime = installRuntime();
        render(
            <ToastProvider>
                <DialogProvider>
                    <FileCompanionWindow askOnOpen={false} />
                </DialogProvider>
            </ToastProvider>,
        );
        await waitFor(() => {
            runtime.emit(COMPANION_UI_LANGUAGE_EVENT, "zh-Hans");
            expect(screen.getByTestId("file-companion-choose").textContent).toBe("打开");
        });
        await act(async () => {
            resolveLang("mn");
        });
        expect(screen.getByTestId("file-companion-choose").textContent).toBe("打开");
        expect(screen.getByTestId("file-companion-brand").textContent).toBe("MaClaw伴读");
    });
});
