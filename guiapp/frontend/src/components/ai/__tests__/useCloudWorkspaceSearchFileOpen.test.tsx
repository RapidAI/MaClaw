// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GetCodingWorkbenchFilePreview, OpenCodingWorkbenchFileLocally } from "../../../../wailsjs/go/main/App";
import { clearParkedCloudWorkspaceFileOpen, parkCloudWorkspaceFileOpen } from "../cloudWorkspaceFileOpen";
import { useCloudWorkspaceSearchFileOpen } from "../useCloudWorkspaceSearchFileOpen";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetCodingWorkbenchFilePreview: vi.fn(),
    OpenCodingWorkbenchFileLocally: vi.fn(),
}));

function Harness(props: { ready: boolean; projectPath: string }) {
    useCloudWorkspaceSearchFileOpen({
        ready: props.ready,
        projectPath: props.projectPath,
        workspaceId: "cws_a",
        openWorkspaceFile: openFile,
        reopenPreview,
    });
    return null;
}

const openFile = vi.fn();
const reopenPreview = vi.fn();

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    clearParkedCloudWorkspaceFileOpen();
    delete (window as unknown as { go?: unknown }).go;
});

describe("useCloudWorkspaceSearchFileOpen", () => {
    it("waits until the cloud workspace tab is ready, then opens the text file", async () => {
        vi.mocked(GetCodingWorkbenchFilePreview).mockResolvedValue({
            path: "papers/brief.md",
            content: "竞品分析",
            language: "markdown",
            truncated: false,
            abs_path: "",
        } as Awaited<ReturnType<typeof GetCodingWorkbenchFilePreview>>);
        const view = render(<Harness ready={false} projectPath="D:/tasks/other" />);
        parkCloudWorkspaceFileOpen({
            projectPath: "D:/tasks/notes",
            relativePath: "papers/brief.md",
            workspaceId: "cws_a",
            fileName: "brief.md",
        });
        expect(GetCodingWorkbenchFilePreview).not.toHaveBeenCalled();

        view.rerender(<Harness ready projectPath="D:/tasks/notes" />);
        await waitFor(() => expect(GetCodingWorkbenchFilePreview).toHaveBeenCalledWith("D:/tasks/notes", "papers/brief.md"));
        await waitFor(() => expect(openFile).toHaveBeenCalledWith(expect.objectContaining({
            filePath: "papers/brief.md",
            content: "竞品分析",
            language: "markdown",
        })));
        expect(reopenPreview).toHaveBeenCalled();
        expect(OpenCodingWorkbenchFileLocally).not.toHaveBeenCalled();
    });

    it("opens a pdf with the local app instead of the text preview", async () => {
        render(<Harness ready projectPath="D:/tasks/notes" />);
        parkCloudWorkspaceFileOpen({
            projectPath: "D:/tasks/notes",
            relativePath: "slides/报告.pdf",
            workspaceId: "cws_a",
            fileName: "报告.pdf",
        });
        await waitFor(() => expect(OpenCodingWorkbenchFileLocally).toHaveBeenCalledWith("D:/tasks/notes", "slides/报告.pdf"));
        expect(GetCodingWorkbenchFilePreview).not.toHaveBeenCalled();
        expect(reopenPreview).not.toHaveBeenCalled();
        expect(openFile).not.toHaveBeenCalled();
    });

    it("opens a data directory file from the task sandbox before the cloud preview is ready", async () => {
        const preview = vi.fn().mockResolvedValue({ path: "papers/brief.md", content: "竞品分析", language: "markdown", truncated: false });
        const openLocal = vi.fn();
        const holdPreview = vi.fn();
        (window as unknown as { go: unknown }).go = { main: { App: { GetDataDirectoryWorkspaceFilePreview: preview, OpenDataDirectoryWorkspaceFileLocally: openLocal } } };
        function DataHarness() {
            useCloudWorkspaceSearchFileOpen({
                ready: false,
                projectPath: "D:/data/tasks/notes-1",
                openWorkspaceFile: openFile,
                reopenPreview,
                holdPreview,
            });
            return null;
        }
        render(<DataHarness />);
        parkCloudWorkspaceFileOpen({
            projectPath: "D:/data/tasks/notes-1",
            relativePath: "papers/brief.md",
            workspaceId: "",
            fileName: "brief.md",
            source: "data",
        });
        await waitFor(() => expect(preview).toHaveBeenCalledWith("D:/data/tasks/notes-1", "papers/brief.md"));
        await waitFor(() => expect(openFile).toHaveBeenCalledWith(expect.objectContaining({
            filePath: "papers/brief.md",
            content: "竞品分析",
        })));
        expect(holdPreview).toHaveBeenCalled();
        expect(reopenPreview).toHaveBeenCalled();
        expect(GetCodingWorkbenchFilePreview).not.toHaveBeenCalled();
        expect(openLocal).not.toHaveBeenCalled();
        delete (window as unknown as { go?: unknown }).go;
    });

    it("opens a data directory pdf locally without opening the text preview", async () => {
        const preview = vi.fn();
        const openLocal = vi.fn().mockResolvedValue(undefined);
        const holdPreview = vi.fn();
        (window as unknown as { go: unknown }).go = { main: { App: { GetDataDirectoryWorkspaceFilePreview: preview, OpenDataDirectoryWorkspaceFileLocally: openLocal } } };
        function PdfHarness() {
            useCloudWorkspaceSearchFileOpen({
                ready: false,
                projectPath: "D:/data/tasks/notes-1",
                openWorkspaceFile: openFile,
                reopenPreview,
                holdPreview,
            });
            return null;
        }
        render(<PdfHarness />);
        parkCloudWorkspaceFileOpen({
            projectPath: "D:/data/tasks/notes-1",
            relativePath: "slides/报告.pdf",
            workspaceId: "",
            fileName: "报告.pdf",
            source: "data",
        });
        await waitFor(() => expect(openLocal).toHaveBeenCalledWith("D:/data/tasks/notes-1", "slides/报告.pdf"));
        expect(preview).not.toHaveBeenCalled();
        expect(holdPreview).not.toHaveBeenCalled();
        expect(reopenPreview).not.toHaveBeenCalled();
        expect(openFile).not.toHaveBeenCalled();
    });

    it("does not open the preview pane when the data directory reader is missing", async () => {
        const holdPreview = vi.fn();
        function MissingHarness() {
            useCloudWorkspaceSearchFileOpen({
                ready: false,
                projectPath: "D:/data/tasks/notes-1",
                openWorkspaceFile: openFile,
                reopenPreview,
                holdPreview,
            });
            return null;
        }
        render(<MissingHarness />);
        parkCloudWorkspaceFileOpen({
            projectPath: "D:/data/tasks/notes-1",
            relativePath: "papers/brief.md",
            workspaceId: "",
            fileName: "brief.md",
            source: "data",
        });
        expect(holdPreview).not.toHaveBeenCalled();
        expect(reopenPreview).not.toHaveBeenCalled();
        expect(openFile).not.toHaveBeenCalled();
    });
});
