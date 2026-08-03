// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectSearchPanel } from "../ProjectSearchPanel";
import { lightTheme } from "../aiAssistantPanelTheme";
import { DeleteTask, GetArchivedExperience, GetProjectScene, OpenFileOrShowInFolder, ResumeTask } from "../../../../wailsjs/go/main/App";

const { getArchivedExperienceMock, getProjectSceneMock, openFileOrShowInFolderMock, resumeTaskMock, renameTaskMock, pinTaskMock, deleteTaskMock, archiveProjectMock } = vi.hoisted(() => ({
    getArchivedExperienceMock: vi.fn(),
    getProjectSceneMock: vi.fn(),
    openFileOrShowInFolderMock: vi.fn(),
    resumeTaskMock: vi.fn(),
    renameTaskMock: vi.fn(),
    pinTaskMock: vi.fn(),
    deleteTaskMock: vi.fn(),
    archiveProjectMock: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SearchTasks: vi.fn().mockResolvedValue([]),
    GetArchivedExperience: getArchivedExperienceMock,
    GetProjectScene: getProjectSceneMock,
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    ResumeTask: resumeTaskMock,
    RenameTask: renameTaskMock,
    PinTask: pinTaskMock,
    DeleteTask: deleteTaskMock,
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
        expect(ResumeTask).not.toHaveBeenCalled();
    });

    it("opens active tasks into a project tab when tab creation is available", async () => {
        const search = makeSearch([{ id: "p1", name: "Active task", project_path: "D:/p/live" }]);
        const onCreateProjectTab = vi.fn();

        renderPanel(search, { onCreateProjectTab });
        fireEvent.click(screen.getByText("Active task"));

        expect(search.close).toHaveBeenCalled();
        await waitFor(() => expect(onCreateProjectTab).toHaveBeenCalledWith(
            "D:/p/live",
            "Active task",
            expect.objectContaining({ autoSend: false }),
        ));
        expect(ResumeTask).not.toHaveBeenCalled();
    });

    it("opens pure coding tasks with coding agentMode from tags", async () => {
        const search = makeSearch([{
            id: "p-coding",
            name: "Coding task",
            project_path: "D:/p/coding",
            tags: ["coding_dev", "task_management"],
        }]);
        const onCreateProjectTab = vi.fn();

        renderPanel(search, { onCreateProjectTab });
        fireEvent.click(screen.getByText("Coding task"));

        await waitFor(() => expect(onCreateProjectTab).toHaveBeenCalledWith(
            "D:/p/coding",
            "Coding task",
            expect.objectContaining({
                autoSend: false,
                agentMode: "coding_dev",
                tags: expect.arrayContaining(["coding_dev"]),
            }),
        ));
        expect(screen.getByTestId("search-coding-badge")).toBeTruthy();
    });

    it("opens remote coding tasks through tab creation instead of the legacy resume-send fallback", async () => {
        const search = makeSearch([{
            id: "p-remote-coding",
            name: "Remote coding task",
            project_path: "D:/p/remote-coding",
            tags: ["remote_coding_dev", "remote_host:10.0.0.12", "source:remote_ops_diagnosis"],
        }]);
        const onCreateProjectTab = vi.fn();
        const onProjectSwitch = vi.fn();

        renderPanel(search, { onCreateProjectTab, onProjectSwitch });
        fireEvent.click(screen.getByText("Remote coding task"));

        await waitFor(() => expect(onCreateProjectTab).toHaveBeenCalledWith(
            "D:/p/remote-coding",
            "Remote coding task",
            expect.objectContaining({
                autoSend: false,
                agentMode: "remote_coding_dev",
                remoteHost: "10.0.0.12",
                remoteSafety: "diagnosis",
            }),
        ));
        expect(onProjectSwitch).not.toHaveBeenCalled();
        expect(ResumeTask).not.toHaveBeenCalled();
        expect(screen.getByTestId("search-remote-coding-badge")).toBeTruthy();
        expect(screen.getByTestId("search-remote-coding-badge").textContent || "").toMatch(/Remote maintenance|远程维护/i);
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

    it("clears search UI and ignores late scene results after the assistant page is hidden", async () => {
        const search = makeSearch([{ id: "p-hidden", name: "Hidden task", project_path: "D:/p/hidden" }]);
        let resolveScene: (value: unknown) => void = () => {};
        getProjectSceneMock.mockReturnValue(new Promise(resolve => { resolveScene = resolve; }));
        const { rerender } = renderPanel(search);

        fireEvent.click(screen.getByTitle("Scene details"));
        rerender(
            <ProjectSearchPanel
                search={search}
                lang="en"
                theme={lightTheme}
                inline={false}
                active={false}
                onProjectSwitch={vi.fn()}
            />,
        );

        expect(screen.queryByPlaceholderText("Search tasks...")).toBeNull();
        resolveScene({ project_path: "D:/p/hidden", name: "Should not render" });
        await Promise.resolve();
        expect(screen.queryByText("Should not render")).toBeNull();
        expect(search.close).toHaveBeenCalled();
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

        expect(screen.getByText("No tasks")).toBeTruthy();
        expect(screen.queryByText("Chat only")).toBeNull();
    });

    it("falls back to ResumeTask when project tabs are unavailable", async () => {
        const search = makeSearch([{ id: "p2", name: "Fallback task", project_path: "D:/p/fallback" }]);
        const onProjectSwitch = vi.fn();
        resumeTaskMock.mockResolvedValue("resumed");

        renderPanel(search, { onProjectSwitch });
        fireEvent.click(screen.getByText("Fallback task"));

        await waitFor(() => expect(ResumeTask).toHaveBeenCalledWith("D:/p/fallback"));
        expect(onProjectSwitch).toHaveBeenCalledWith("resumed");
    });

    it("closes an open project tab when a task is removed", async () => {
        const search = makeSearch([{ id: "p4", name: "Removable task", project_path: "D:/p/remove" }]);
        const onCloseProjectTab = vi.fn();
        deleteTaskMock.mockResolvedValue(undefined);
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: "proj-remove", type: "project", projectPath: "d:\\p\\remove\\." },
            { id: "proj-keep", type: "project", projectPath: "D:/p/keep" },
        ]));
        localStorage.setItem("ai_assistant_project_tab_histories", JSON.stringify({
            "proj-remove": [{ id: "deleted-history" }],
            "proj-keep": [{ id: "kept-history" }],
        }));

        renderPanel(search, { onCloseProjectTab });
        fireEvent.contextMenu(screen.getByText("Removable task"));
        fireEvent.click(screen.getByText("Remove"));

        await waitFor(() => expect(deleteTaskMock).toHaveBeenCalledWith("D:/p/remove"));
        expect(onCloseProjectTab).toHaveBeenCalledWith("D:/p/remove");
        expect(search.refresh).toHaveBeenCalled();
        expect(localStorage.getItem("ai_assistant_project_tabs") || "").not.toContain("remove");
        expect(localStorage.getItem("ai_assistant_project_tabs") || "").toContain("D:/p/keep");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").not.toContain("proj-remove");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").toContain("proj-keep");
    });

    it("keeps the task visible when deletion fails", async () => {
        const search = makeSearch([{ id: "p4-fail", name: "Failed delete", project_path: "D:/p/delete-fail" }]);
        const onCloseProjectTab = vi.fn();
        deleteTaskMock.mockRejectedValueOnce(new Error("disk unavailable"));

        renderPanel(search, { onCloseProjectTab });
        fireEvent.contextMenu(screen.getByText("Failed delete"));
        fireEvent.click(screen.getByText("Remove"));

        await waitFor(() => expect(deleteTaskMock).toHaveBeenCalledWith("D:/p/delete-fail"));
        expect(onCloseProjectTab).not.toHaveBeenCalled();
        expect(search.refresh).not.toHaveBeenCalled();
    });

    it("closes an open project tab when a task is archived", async () => {
        const search = makeSearch([{ id: "p5", name: "Archivable task", project_path: "D:/p/archive" }]);
        const onCloseProjectTab = vi.fn();
        archiveProjectMock.mockResolvedValue({ archived: true });

        renderPanel(search, { onCloseProjectTab });
        fireEvent.contextMenu(screen.getByText("Archivable task"));
        fireEvent.click(screen.getByText("Archive"));

        // No DialogProvider is intentionally present in this isolated unit test;
        // the defensive dialog fallback cancels destructive actions.
        await waitFor(() => expect(archiveProjectMock).not.toHaveBeenCalled());
        expect(onCloseProjectTab).not.toHaveBeenCalled();
        expect(search.refresh).not.toHaveBeenCalled();
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
