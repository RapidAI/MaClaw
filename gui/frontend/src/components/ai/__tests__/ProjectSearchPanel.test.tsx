// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectSearchPanel } from "../ProjectSearchPanel";
import { lightTheme } from "../aiAssistantPanelTheme";
import { ForkRecentTask, GetArchivedExperience, GetProjectScene, OpenFileOrShowInFolder, ResumeProject } from "../../../../wailsjs/go/main/App";

const { forkRecentTaskMock, getArchivedExperienceMock, getProjectSceneMock, openFileOrShowInFolderMock, resumeProjectMock, renameTaskMock, pinTaskMock, hideTaskMock, archiveProjectMock } = vi.hoisted(() => ({
    forkRecentTaskMock: vi.fn(),
    getArchivedExperienceMock: vi.fn(),
    getProjectSceneMock: vi.fn(),
    openFileOrShowInFolderMock: vi.fn(),
    resumeProjectMock: vi.fn(),
    renameTaskMock: vi.fn(),
    pinTaskMock: vi.fn(),
    hideTaskMock: vi.fn(),
    archiveProjectMock: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SearchProjects: vi.fn().mockResolvedValue([]),
    ForkRecentTask: forkRecentTaskMock,
    GetArchivedExperience: getArchivedExperienceMock,
    GetProjectScene: getProjectSceneMock,
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    ResumeProject: resumeProjectMock,
    RenameTask: renameTaskMock,
    PinTask: pinTaskMock,
    HideTask: hideTaskMock,
    ArchiveProject: archiveProjectMock,
}));

function makeSearch(results: any[]) {
    return {
        open: true,
        query: "",
        results,
        loading: false,
        toggle: vi.fn(),
        close: vi.fn(),
        onQueryChange: vi.fn(),
        refresh: vi.fn(),
        formatTime: vi.fn(() => "just now"),
    } as any;
}

function renderPanel(search: any, props: Partial<React.ComponentProps<typeof ProjectSearchPanel>> = {}) {
    return render(
        <ProjectSearchPanel
            search={search}
            lang="en"
            theme={lightTheme}
            inline={false}
            onProjectSwitch={vi.fn()}
            {...props}
        />
    );
}

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe("ProjectSearchPanel", () => {
    it("opens archived tasks as a read-only experience preview", async () => {
        const search = makeSearch([{ id: "a1", name: "Archived task", project_path: "D:/p/a", archived: true }]);
        getArchivedExperienceMock.mockResolvedValue("Saved decisions");

        renderPanel(search);
        fireEvent.click(screen.getByText("Archived task"));

        expect(search.close).toHaveBeenCalled();
        expect(await screen.findByText("Loading...")).toBeTruthy();
        expect(await screen.findByText("Saved decisions")).toBeTruthy();
        expect(screen.getByText("This task has been archived and cannot be continued.")).toBeTruthy();
        expect(GetArchivedExperience).toHaveBeenCalledWith("D:/p/a");
        expect(ResumeProject).not.toHaveBeenCalled();
    });

    it("forks active tasks into a project tab when tab creation is available", async () => {
        const search = makeSearch([{ id: "p1", name: "Active task", project_path: "D:/p/live" }]);
        const onCreateProjectTab = vi.fn();
        forkRecentTaskMock.mockResolvedValue({ project_path: "D:/p/forked", name: "Active task" });

        renderPanel(search, { onCreateProjectTab });
        fireEvent.click(screen.getByText("Active task"));

        expect(search.close).toHaveBeenCalled();
        await waitFor(() => expect(onCreateProjectTab).toHaveBeenCalledWith("D:/p/forked", "Active task", { autoSend: false }));
        expect(ForkRecentTask).toHaveBeenCalledWith("D:/p/live");
        expect(ResumeProject).not.toHaveBeenCalled();
    });

    it("loads scene evidence and opens artifact sources", async () => {
        const search = makeSearch([{ id: "p3", name: "Evidence task", project_path: "D:/p/evidence" }]);
        getProjectSceneMock.mockResolvedValue({
            project_path: "D:/p/evidence",
            name: "Evidence task",
            entry_count: 2,
            recent_artifacts: [{ title: "Decision note", source_url: "D:/refs/decision.md", source_hint: "full: read_file" }],
        });

        renderPanel(search);
        fireEvent.click(screen.getByTitle("Scene details"));

        expect(await screen.findByText("Decision note")).toBeTruthy();
        expect(GetProjectScene).toHaveBeenCalledWith("D:/p/evidence");

        fireEvent.click(screen.getByLabelText("Open artifact source"));
        expect(OpenFileOrShowInFolder).toHaveBeenCalledWith("D:/refs/decision.md");
    });

    it("hides search results without tangible output", () => {
        const search = makeSearch([
            { id: "chat", name: "Chat only", project_path: "D:/p/chat", has_output: false },
            { id: "out", name: "Saved output", project_path: "D:/p/output", has_output: true },
        ]);

        renderPanel(search);

        expect(screen.queryByText("Chat only")).toBeNull();
        expect(screen.getByText("Saved output")).toBeTruthy();
    });

    it("shows an empty search state when only non-output records remain", () => {
        const search = makeSearch([{ id: "chat", name: "Chat only", project_path: "D:/p/chat", has_output: false }]);

        renderPanel(search);

        expect(screen.getByText("No recent tasks")).toBeTruthy();
        expect(screen.queryByText("Chat only")).toBeNull();
    });

    it("falls back to ResumeProject when project tabs are unavailable", async () => {
        const search = makeSearch([{ id: "p2", name: "Fallback task", project_path: "D:/p/fallback" }]);
        const onProjectSwitch = vi.fn();
        forkRecentTaskMock.mockResolvedValue({ project_path: "D:/p/forked", name: "Fallback task" });
        resumeProjectMock.mockResolvedValue("resumed");

        renderPanel(search, { onProjectSwitch });
        fireEvent.click(screen.getByText("Fallback task"));

        await waitFor(() => expect(ResumeProject).toHaveBeenCalledWith("D:/p/forked"));
        expect(ForkRecentTask).toHaveBeenCalledWith("D:/p/fallback");
        expect(onProjectSwitch).toHaveBeenCalledWith("resumed");
    });

    it("closes an open project tab when a task is removed", async () => {
        const search = makeSearch([{ id: "p4", name: "Removable task", project_path: "D:/p/remove" }]);
        const onCloseProjectTab = vi.fn();
        hideTaskMock.mockResolvedValue(undefined);

        renderPanel(search, { onCloseProjectTab });
        fireEvent.contextMenu(screen.getByText("Removable task"));
        fireEvent.click(screen.getByText("Remove"));

        await waitFor(() => expect(hideTaskMock).toHaveBeenCalledWith("D:/p/remove"));
        expect(onCloseProjectTab).toHaveBeenCalledWith("D:/p/remove");
        expect(search.refresh).toHaveBeenCalled();
    });

    it("closes an open project tab when a task is archived", async () => {
        const search = makeSearch([{ id: "p5", name: "Archivable task", project_path: "D:/p/archive" }]);
        const onCloseProjectTab = vi.fn();
        archiveProjectMock.mockResolvedValue({ archived: true });
        const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

        renderPanel(search, { onCloseProjectTab });
        fireEvent.contextMenu(screen.getByText("Archivable task"));
        fireEvent.click(screen.getByText("Archive"));

        await waitFor(() => expect(archiveProjectMock).toHaveBeenCalledWith("D:/p/archive"));
        expect(onCloseProjectTab).toHaveBeenCalledWith("D:/p/archive");
        expect(search.refresh).toHaveBeenCalled();
        confirmSpy.mockRestore();
    });

    it("creates a task from current chat with inline naming instead of browser prompt", () => {
        const search = makeSearch([]);
        const onForkCurrentChat = vi.fn();
        const promptSpy = vi.spyOn(window, "prompt");

        renderPanel(search, { onForkCurrentChat });
        fireEvent.click(screen.getByTitle("New task from current chat"));
        fireEvent.change(screen.getByPlaceholderText("Task name (optional)"), { target: { value: "Research task" } });
        fireEvent.click(screen.getByText("Create"));

        expect(promptSpy).not.toHaveBeenCalled();
        expect(search.close).toHaveBeenCalled();
        expect(onForkCurrentChat).toHaveBeenCalledWith("Research task");
        promptSpy.mockRestore();
    });
});
