// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectSearchPanel } from "../ProjectSearchPanel";
import { lightTheme } from "../aiAssistantPanelTheme";
import { GetArchivedExperience, ResumeProject } from "../../../../wailsjs/go/main/App";

const { getArchivedExperienceMock, resumeProjectMock, renameTaskMock, pinTaskMock, hideTaskMock, archiveProjectMock } = vi.hoisted(() => ({
    getArchivedExperienceMock: vi.fn(),
    resumeProjectMock: vi.fn(),
    renameTaskMock: vi.fn(),
    pinTaskMock: vi.fn(),
    hideTaskMock: vi.fn(),
    archiveProjectMock: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SearchProjects: vi.fn().mockResolvedValue([]),
    GetArchivedExperience: getArchivedExperienceMock,
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

    it("opens active tasks in a project tab when tab creation is available", () => {
        const search = makeSearch([{ id: "p1", name: "Active task", project_path: "D:/p/live" }]);
        const onCreateProjectTab = vi.fn();

        renderPanel(search, { onCreateProjectTab });
        fireEvent.click(screen.getByText("Active task"));

        expect(search.close).toHaveBeenCalled();
        expect(onCreateProjectTab).toHaveBeenCalledWith("D:/p/live", "Active task");
        expect(ResumeProject).not.toHaveBeenCalled();
    });

    it("falls back to ResumeProject when project tabs are unavailable", async () => {
        const search = makeSearch([{ id: "p2", name: "Fallback task", project_path: "D:/p/fallback" }]);
        const onProjectSwitch = vi.fn();
        resumeProjectMock.mockResolvedValue("resumed");

        renderPanel(search, { onProjectSwitch });
        fireEvent.click(screen.getByText("Fallback task"));

        await waitFor(() => expect(ResumeProject).toHaveBeenCalledWith("D:/p/fallback"));
        expect(onProjectSwitch).toHaveBeenCalledWith("resumed");
    });
});
