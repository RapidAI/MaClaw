import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CloudWorkspaceEntitlement, CreateCloudWorkspace, CreateLatexDocument, CreateRunWindowTask, CreateTaskUnified, ListLatexTemplates, RenameCloudWorkspace, SetTabWorkingDir, SuggestNewTaskExpert, SuggestNewTaskType } from '../../../../../wailsjs/go/main/App';
import { useTaskConfigWiring, type TaskConfigWiringOptions } from '../useTaskConfigWiring';
import { defaultTaskDraft, withCloudWorkspace, withExpert, withLocalWorkspace, withRemoteWorkspace, withRunWindow, withWorkflow, WORKFLOW_AUTO } from '../taskDraft';
import { LATEX_EXPERT_ID } from '../../../../utils/latexTemplates';
import { EVENT_NEW_TASK_WIZARD_BLOCKED, EVENT_OPEN_NEW_TASK_WIZARD, EVENT_OPEN_TASK_LAUNCH } from '../../../../constants/events';

const selectWorkingDirMock = vi.fn();
vi.mock('../../../../../wailsjs/go/main/App', () => ({
    ListExperts: vi.fn().mockResolvedValue('[]'),
    ListManagedIndustryExperts: vi.fn().mockResolvedValue(''),
    ListWorkflowTemplateSummaries: vi.fn().mockResolvedValue([]),
    ListLatexTemplates: vi.fn().mockResolvedValue('{"categories":[],"templates":[]}'),
    CloudWorkspaceEntitlement: vi.fn().mockResolvedValue({ workspaces: [] }),
    CreateCloudWorkspace: vi.fn(),
    RenameCloudWorkspace: vi.fn(),
    CreateTaskUnified: vi.fn(),
    SuggestNewTaskType: vi.fn().mockResolvedValue({ suggested: '', confident: false }),
    SuggestNewTaskExpert: vi.fn().mockResolvedValue({ expertId: '', confident: false }),
    CreateRunWindowTask: vi.fn(),
    ListRunWindowTasks: vi.fn().mockResolvedValue([]),
    ClaimRunWindowTask: vi.fn(),
    ReleaseRunWindowTask: vi.fn(),
    MarkRunWindowTaskStarted: vi.fn(),
    MarkRunWindowTaskFailed: vi.fn(),
    CreateLatexDocument: vi.fn(),
    SetTabWorkingDir: vi.fn(),
    EnsureCodingWorkbenchArmed: vi.fn().mockResolvedValue(undefined),
    SelectWorkingDir: (...args: unknown[]) => selectWorkingDirMock(...args),
}));

vi.mock('../../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => () => {}),
}));

function makeOptions(overrides?: Partial<TaskConfigWiringOptions>): TaskConfigWiringOptions {
    return {
        activeTab: { id: 'tab-1' },
        isLocalTabActive: true,
        lang: 'zh',
        taskListProp: [{ working_dir: 'D:/from/tasklist' }],
        inputRef: { current: null },
        inputValue: '',
        composeAction: null,
        inputLocked: false,
        activeChatVisible: false,
        handleSend: vi.fn(),
        handleWelcomePromptSend: vi.fn(),
        clearComposerDraft: vi.fn(),
        getTabs: () => [{ id: 'tab-1', type: 'local' }],
        getTabState: () => null,
        saveTabState: vi.fn(),
        activateTab: vi.fn(),
        setQueueInteractionStarted: vi.fn(),
        setQueueEditDraftActive: vi.fn(),
        setEditingEntryId: vi.fn(),
        ...overrides,
    };
}

describe('useTaskConfigWiring onBrowseLocal', () => {
    afterEach(() => {
        cleanup();
        selectWorkingDirMock.mockReset();
    });

    it('returns the picked path and prepends it to recentLocalPaths', async () => {
        selectWorkingDirMock.mockResolvedValue('D:/picked/dir');
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));

        let picked: string | null | undefined;
        await act(async () => {
            picked = await result.current.taskConfig.onBrowseLocal();
        });
        expect(picked).toBe('D:/picked/dir');
        expect(result.current.taskConfig.recentLocalPaths[0]).toBe('D:/picked/dir');
        expect(result.current.taskConfig.recentLocalPaths).toContain('D:/from/tasklist');
    });

    it('returns null on cancel and leaves recentLocalPaths unchanged', async () => {
        selectWorkingDirMock.mockResolvedValue('');
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));

        let picked: string | null | undefined = 'sentinel';
        await act(async () => {
            picked = await result.current.taskConfig.onBrowseLocal();
        });
        expect(picked).toBeNull();
        expect(result.current.taskConfig.recentLocalPaths).toEqual(['D:/from/tasklist']);
    });

    it('dedupes a re-picked path to the front instead of duplicating it', async () => {
        selectWorkingDirMock.mockResolvedValue('D:/from/tasklist');
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));
        await waitFor(() => {
            expect(result.current.taskConfig.recentLocalPaths).toEqual(['D:/from/tasklist']);
        });

        let picked: string | null | undefined;
        await act(async () => {
            picked = await result.current.taskConfig.onBrowseLocal();
        });
        expect(picked).toBe('D:/from/tasklist');
        expect(result.current.taskConfig.recentLocalPaths).toEqual(['D:/from/tasklist']);
    });
});

describe('useTaskConfigWiring cloud workspace name', () => {
    afterEach(() => {
        cleanup();
        vi.mocked(CloudWorkspaceEntitlement).mockReset();
        vi.mocked(CloudWorkspaceEntitlement).mockResolvedValue({ workspaces: [] } as never);
        vi.mocked(CreateCloudWorkspace).mockReset();
        vi.mocked(RenameCloudWorkspace).mockReset();
    });

    it('creates a named workspace and selects it on the draft', async () => {
        vi.mocked(CreateCloudWorkspace).mockResolvedValue({ id: 'cws-new', name: '标书项目' } as never);
        vi.mocked(CloudWorkspaceEntitlement).mockResolvedValue({
            workspaces: [{ id: 'cws-new', name: '标书项目', status: 'active', retained_bytes: 0 }],
        } as never);
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));

        await act(async () => {
            await result.current.taskConfig.onCreateCloud?.('  标书项目  ');
        });

        expect(CreateCloudWorkspace).toHaveBeenCalledWith('标书项目');
        expect(result.current.taskConfig.draft.workspace).toEqual({
            kind: 'cloud',
            cloudWorkspaceId: 'cws-new',
            cloudName: '标书项目',
        });
        expect(result.current.taskConfig.cloudWorkspaces?.[0]).toMatchObject({ id: 'cws-new', name: '标书项目' });
        expect(result.current.taskConfig.error).toBe('');
    });

    it('renames the selected cloud workspace and keeps the draft name in sync', async () => {
        vi.mocked(RenameCloudWorkspace).mockResolvedValue({ id: 'cws-1', name: '标书项目' } as never);
        vi.mocked(CloudWorkspaceEntitlement).mockResolvedValue({
            workspaces: [{ id: 'cws-1', name: '标书项目', status: 'active', retained_bytes: 0 }],
        } as never);
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));
        act(() => {
            result.current.taskConfig.onDraftChange(withCloudWorkspace(defaultTaskDraft(), 'cws-1', '工作区 1'));
        });

        await act(async () => {
            await result.current.taskConfig.onRenameCloud?.('cws-1', '标书项目');
        });

        expect(RenameCloudWorkspace).toHaveBeenCalledWith('cws-1', '标书项目');
        expect(result.current.taskConfig.draft.workspace).toEqual({
            kind: 'cloud',
            cloudWorkspaceId: 'cws-1',
            cloudName: '标书项目',
        });
    });

    it('opens the first cloud task on that workspace conversation', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/first-cloud' });
        const launches: CustomEvent[] = [];
        const listener = (event: Event) => launches.push(event as CustomEvent);
        window.addEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                localGuideVisible: true,
                inputValue: '整理讲义',
            })));
            act(() => {
                result.current.taskConfig.onDraftChange(withCloudWorkspace(defaultTaskDraft(), 'cws-math', '数学'));
            });
            await act(async () => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => expect(launches.length).toBeGreaterThan(0));
            const detail = launches.at(-1)?.detail as { projectPath?: string; cloudWorkspaceId?: string; fileWorkspaceId?: string };
            expect(detail.projectPath).toBe('D:/tasks/first-cloud');
            expect(detail.cloudWorkspaceId).toBe('cws-math');
            expect(detail.fileWorkspaceId).toBeUndefined();
        } finally {
            window.removeEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
            create.mockReset();
        }
    });

    it('keeps a second cloud task on its own tab and still names the shared files', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/second-cloud', ownConversation: true });
        const launches: CustomEvent[] = [];
        const listener = (event: Event) => launches.push(event as CustomEvent);
        window.addEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                localGuideVisible: true,
                inputValue: '再出一份习题',
            })));
            act(() => {
                result.current.taskConfig.onDraftChange(withCloudWorkspace(defaultTaskDraft(), 'cws-math', '数学'));
            });
            await act(async () => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => expect(launches.length).toBeGreaterThan(0));
            const detail = launches.at(-1)?.detail as { projectPath?: string; cloudWorkspaceId?: string; fileWorkspaceId?: string };
            expect(detail.projectPath).toBe('D:/tasks/second-cloud');
            expect(detail.cloudWorkspaceId).toBeUndefined();
            expect(detail.fileWorkspaceId).toBe('cws-math');
        } finally {
            window.removeEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
            create.mockReset();
        }
    });

    it('keeps a created workspace visible when the entitlement refresh omits it', async () => {
        vi.mocked(CreateCloudWorkspace).mockResolvedValue({ id: 'cws-new', name: '标书项目' } as never);
        vi.mocked(CloudWorkspaceEntitlement).mockResolvedValue({ workspaces: [] } as never);
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));

        await act(async () => {
            await result.current.taskConfig.onCreateCloud?.('标书项目');
        });

        expect(result.current.taskConfig.cloudWorkspaces?.some((row) => row.id === 'cws-new' && row.name === '标书项目')).toBe(true);
    });

    it('labels Hub status active as 可用', async () => {
        vi.mocked(CloudWorkspaceEntitlement).mockResolvedValue({
            workspaces: [{ id: 'cws-1', name: '工作区 1', status: 'active', retained_bytes: 0 }],
        } as never);
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions()));
        await waitFor(() => {
            expect(result.current.taskConfig.cloudWorkspaces?.[0]?.state).toBe('可用');
        });
        expect(result.current.taskConfig.cloudWorkspaces?.[0]?.spec).toBe('0 B');
    });
});

describe('useTaskConfigWiring new-task wizard opening', () => {
    afterEach(async () => {
        // The cover settles on a timer. Flush it inside act so a later test
        // does not see that setState.
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 0));
            await new Promise((resolve) => setTimeout(resolve, 0));
        });
        cleanup();
    });

    it('keeps the guide draft when the wizard is opened from another tab', async () => {
        const clearComposerDraft = vi.fn();
        const activateTab = vi.fn();
        const saveTabState = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            activeTab: { id: 'proj-1', type: 'project' },
            isLocalTabActive: false,
            inputValue: 'half written',
            clearComposerDraft,
            activateTab,
            saveTabState,
            getTabs: () => [{ id: 'tab-1', type: 'local' }, { id: 'proj-1', type: 'project' }],
            getTabState: () => ({ history: [] }),
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        expect(result.current.wizardOverlay).toBe(true);
        expect(clearComposerDraft).not.toHaveBeenCalled();
        expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        expect(activateTab).not.toHaveBeenCalled();
    });

    it('opens over another tab without switching or clearing its draft', async () => {
        const activateTab = vi.fn();
        const clearComposerDraft = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            activeTab: { id: 'proj-1', type: 'project' },
            isLocalTabActive: false,
            assistantBusy: true,
            inputValue: 'follow up',
            clearComposerDraft,
            activateTab,
            getTabs: () => [{ id: 'tab-1', type: 'local' }, { id: 'proj-1', type: 'project' }],
            getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        expect(result.current.wizardOverlay).toBe(true);
        expect(activateTab).not.toHaveBeenCalled();
        expect(clearComposerDraft).not.toHaveBeenCalled();
    });

    it('creates a task from the cover before the local tab is active', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/new' });
        const handleSend = vi.fn();
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                activeTab: { id: 'proj-1', type: 'project' },
                isLocalTabActive: false,
                inputValue: 'project follow up',
                guideInputValue: '写一份周报',
                handleSend,
                getTabs: () => [{ id: 'tab-1', type: 'local' }, { id: 'proj-1', type: 'project' }],
                getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
            })));

            act(() => {
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
            });
            expect(result.current.wizardOverlay).toBe(true);
            await act(async () => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => expect(create).toHaveBeenCalled());
            expect(handleSend).not.toHaveBeenCalled();
            expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ name: '写一份周报' }));
        } finally {
            create.mockReset();
        }
    });

    it('prefills the new-task draft from a LaTeX template or a workflow', async () => {
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({ localGuideVisible: true })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD, {
                detail: {
                    expertId: LATEX_EXPERT_ID,
                    expertName: 'LaTeX 论文专家',
                    latexTemplateId: 'tpl-ieee',
                    latexTemplateName: 'IEEE',
                },
            }));
        });
        await waitFor(() => {
            expect(result.current.taskConfig.draft.expertId).toBe(LATEX_EXPERT_ID);
        });
        expect(result.current.taskConfig.draft.latexTemplateId).toBe('tpl-ieee');
        expect(result.current.taskConfig.draft.latexTemplateName).toBe('IEEE');
        expect(result.current.taskConfig.draft.workflowTemplateId).toBeNull();

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD, {
                detail: { workflowTemplateId: 'coding' },
            }));
        });
        await waitFor(() => {
            expect(result.current.taskConfig.draft.workflowTemplateId).toBe('coding');
        });
        expect(result.current.taskConfig.draft.expertId).toBeNull();
    });

    it('loads LaTeX templates again after the first catalogue read fails', async () => {
        let fail = true;
        vi.mocked(ListLatexTemplates).mockImplementation(async () => {
            if (fail) throw new Error('busy');
            return JSON.stringify({ categories: [], templates: [{ id: 'tpl-ieee', name: 'IEEE' }] });
        });
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({ localGuideVisible: true })));
            act(() => {
                result.current.taskConfig.onDraftChange(withExpert(defaultTaskDraft(), LATEX_EXPERT_ID, 'LaTeX 论文专家'));
            });
            await waitFor(() => {
                expect(vi.mocked(ListLatexTemplates).mock.calls.length).toBeGreaterThan(0);
            });
            fail = false;
            act(() => {
                result.current.taskConfig.onPrepareLatexTemplates?.();
            });
            await waitFor(() => {
                expect(result.current.taskConfig.latexTemplates?.some((template) => template.id === 'tpl-ieee')).toBe(true);
            });
        } finally {
            vi.mocked(ListLatexTemplates).mockReset();
            vi.mocked(ListLatexTemplates).mockResolvedValue('{"categories":[],"templates":[]}');
        }
    });

    it('points the LaTeX expert at the folder the document was written into', async () => {
        vi.mocked(CreateTaskUnified).mockResolvedValue({ projectPath: 'D:/tasks/expert' } as never);
        vi.mocked(CreateLatexDocument).mockResolvedValue(JSON.stringify({
            project_path: 'D:/tasks/expert',
            relative_path: 'main.tex',
            main_file: 'main.tex',
            template_id: 'blank',
            workspace_path: 'F:/latex-test',
            created: false,
        }));
        vi.mocked(SetTabWorkingDir).mockResolvedValue(undefined as never);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                inputValue: '改一下摘要',
                localGuideVisible: true,
            })));
            act(() => {
                result.current.taskConfig.onDraftChange(withLocalWorkspace(
                    withExpert(defaultTaskDraft(), LATEX_EXPERT_ID, 'LaTeX 论文专家'),
                    'F:/latex-test',
                ));
            });
            act(() => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => {
                expect(SetTabWorkingDir).toHaveBeenCalledWith('expert-builtin-latex-paper', 'F:/latex-test');
            });
        } finally {
            vi.mocked(CreateTaskUnified).mockReset();
            vi.mocked(CreateLatexDocument).mockReset();
            vi.mocked(SetTabWorkingDir).mockReset();
        }
    });

    it('marks the clean local tab as a wizard page when the sidebar button is clicked', async () => {
        const saveTabState = vi.fn();
        renderHook(() => useTaskConfigWiring(makeOptions({ saveTabState })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        await waitFor(() => {
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        });
    });

    it('does not cover the guide because some other session still has messages', async () => {
        const saveTabState = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            isLocalTabActive: true,
            activeChatVisible: false,
            getTabState: () => ({ history: [] }),
            saveTabState,
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        await waitFor(() => {
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        });
        expect(result.current.wizardOverlay).toBe(false);
    });

    it('does not reset the guide when new task is opened while that page is already showing', async () => {
        const saveTabState = vi.fn();
        const clearComposerDraft = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            inputValue: 'half written',
            clearComposerDraft,
            getTabState: () => ({ history: [{ role: 'user', content: 'old' }] }),
            saveTabState,
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        await waitFor(() => {
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        });
        expect(result.current.wizardOverlay).toBe(false);
        expect(clearComposerDraft).not.toHaveBeenCalled();
    });

    it('unlocks the visible guide while another task is running without clearing the draft', async () => {
        const clearComposerDraft = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            assistantBusy: true,
            inputLocked: true,
            inputValue: 'half written',
            clearComposerDraft,
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        await waitFor(() => {
            expect(result.current.wizardOverlay).toBe(true);
        });
        expect(clearComposerDraft).not.toHaveBeenCalled();
        expect(result.current.taskConfig.disabled).toBe(false);
    });

    it('parks an unsent draft when a turn is running before any transcript exists', async () => {
        const showComposerText = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            assistantBusy: true,
            localGuideVisible: false,
            activeChatVisible: false,
            inputValue: 'follow up',
            getTabState: () => ({ history: [] }),
            showComposerText,
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        await waitFor(() => {
            expect(result.current.wizardOverlay).toBe(true);
        });

        act(() => {
            result.current.dismissWizardOverlay();
        });
        expect(showComposerText).toHaveBeenCalledWith('follow up');
    });

    it('does not leave the guide when Enter is pressed with an empty composer', async () => {
        const handleSend = vi.fn();
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            inputValue: '   ',
            handleSend,
        })));
        act(() => {
            result.current.handleSendWithTaskConfig();
        });
        expect(handleSend).not.toHaveBeenCalled();
        expect(create).not.toHaveBeenCalled();
    });

    it('creates a task from an attachment when the guide has no typed text', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/file' });
        const handleSend = vi.fn();
        const launches: CustomEvent[] = [];
        const listener = (event: Event) => launches.push(event as CustomEvent);
        window.addEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                localGuideVisible: true,
                handleSend,
                pendingAttachments: [{ filePath: 'D:/cases/contract.pdf' }],
            })));
            await act(async () => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => expect(create).toHaveBeenCalled());
            expect(handleSend).not.toHaveBeenCalled();
            expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ name: 'contract.pdf' }));
            expect(String((launches.at(-1)?.detail as { initialMessage?: string })?.initialMessage || '')).toContain('D:/cases/contract.pdf');
        } finally {
            window.removeEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
            create.mockReset();
        }
    });

    it('returns covered attachments to the conversation after the new task is created', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/new' });
        const attachment = { filePath: 'D:/notes/a.txt' };
        const replacePendingAttachments = vi.fn();
        const replaceSelectedFilePaths = vi.fn();
        const showComposerText = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            assistantBusy: true,
            inputLocked: true,
            inputValue: 'follow up',
            pendingAttachments: [attachment],
            selectedFilePaths: ['D:/picked/spec.md'],
            replacePendingAttachments,
            replaceSelectedFilePaths,
            showComposerText,
            getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });
        await waitFor(() => expect(result.current.wizardOverlay).toBe(true));

        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(showComposerText).toHaveBeenCalledWith('follow up');
        expect(replacePendingAttachments).toHaveBeenCalledWith([attachment]);
        expect(replaceSelectedFilePaths).toHaveBeenCalledWith(['D:/picked/spec.md']);
        create.mockReset();
    });

    it('puts a scheduled new task on the plan list instead of starting it', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        const schedule = CreateRunWindowTask as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        schedule.mockReset();
        schedule.mockResolvedValue('rw-1');
        const showAlert = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            inputValue: '夜间整理笔记',
            showAlert,
        })));
        act(() => {
            result.current.taskConfig.onDraftChange(withRunWindow(defaultTaskDraft(), {
                mode: 'scheduled',
                startAt: '2099-01-02T01:00',
                endAt: '2099-01-02T06:30',
            }));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(schedule).toHaveBeenCalled());
        expect(create).not.toHaveBeenCalled();
        expect(schedule.mock.calls[0]?.[0]).toEqual(expect.objectContaining({
            prompt: '夜间整理笔记',
            startAt: '2099-01-02T01:00',
            endAt: '2099-01-02T06:30',
        }));
        expect(showAlert).toHaveBeenCalledWith(
            '定时任务「夜间整理笔记」已经创建，将于 2099-01-02 01:00 启动。\n2099-01-02 06:30 如果任务未完成，将暂停。',
            '定时任务已创建',
        );
    });

    it('does not create another scheduled task while the confirmation is open', async () => {
        const schedule = CreateRunWindowTask as unknown as ReturnType<typeof vi.fn>;
        const handleSend = vi.fn();
        schedule.mockReset();
        let resolveAlert: () => void = () => {};
        schedule.mockResolvedValue('rw-1');
        const showAlert = vi.fn(() => new Promise<void>((resolve) => {
            resolveAlert = resolve;
        }));
        const clearComposerDraft = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            inputValue: '夜间整理笔记',
            showAlert,
            handleSend,
            clearComposerDraft,
        })));
        act(() => {
            result.current.taskConfig.onDraftChange(withRunWindow(defaultTaskDraft(), {
                mode: 'scheduled',
                startAt: '2099-01-02T01:00',
                endAt: '',
            }));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(showAlert).toHaveBeenCalledTimes(1));
        expect(schedule).toHaveBeenCalledTimes(1);
        expect(clearComposerDraft).not.toHaveBeenCalled();

        act(() => {
            result.current.handleSendWithTaskConfig();
        });
        expect(schedule).toHaveBeenCalledTimes(1);
        expect(handleSend).not.toHaveBeenCalled();

        await act(async () => {
            resolveAlert();
        });
        await waitFor(() => expect(clearComposerDraft).toHaveBeenCalled());
        expect(result.current.taskConfig.draft.runWindow?.mode).toBe('immediate');
    });

    it('creates a task when the guide send types /goal', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/typed-goal' });
        const handleSend = vi.fn();
        const launches: CustomEvent[] = [];
        const listener = (event: Event) => launches.push(event as CustomEvent);
        window.addEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                localGuideVisible: true,
                inputValue: '/goal 整理周报',
                handleSend,
            })));
            await act(async () => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => expect(create).toHaveBeenCalled());
            expect(handleSend).not.toHaveBeenCalled();
            expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ name: '整理周报' }));
            expect(String((launches.at(-1)?.detail as { initialMessage?: string })?.initialMessage || '')).toBe('/goal 整理周报');
        } finally {
            window.removeEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
            create.mockReset();
        }
    });

    it('keeps /goal status on the current conversation', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        const handleSend = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            inputValue: '/goal status',
            handleSend,
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        expect(handleSend).toHaveBeenCalledTimes(1);
        expect(create).not.toHaveBeenCalled();
    });

    it('creates a task when the guide send is in compose mode', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/goal' });
        const handleSend = vi.fn();
        const launches: CustomEvent[] = [];
        const listener = (event: Event) => launches.push(event as CustomEvent);
        window.addEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                localGuideVisible: true,
                composeAction: 'goal',
                inputValue: '整理周报',
                handleSend,
            })));
            await act(async () => {
                result.current.handleSendWithTaskConfig();
            });
            await waitFor(() => expect(create).toHaveBeenCalled());
            expect(handleSend).not.toHaveBeenCalled();
            expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ name: '整理周报' }));
            expect(String((launches.at(-1)?.detail as { initialMessage?: string })?.initialMessage || '')).toBe('/goal 整理周报');
        } finally {
            window.removeEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
            create.mockReset();
        }
    });

    it('does not create a task for a reset command typed under compose mode', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        const handleSend = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            composeAction: 'goal',
            inputValue: '/clear',
            handleSend,
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        expect(handleSend).toHaveBeenCalledTimes(1);
        expect(create).not.toHaveBeenCalled();
    });

    it('creates a goal task when a welcome card is sent in compose mode', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockResolvedValue({ projectPath: 'D:/tasks/goal-card' });
        const handleWelcomePromptSend = vi.fn();
        const launches: CustomEvent[] = [];
        const listener = (event: Event) => launches.push(event as CustomEvent);
        window.addEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                localGuideVisible: true,
                composeAction: 'goal',
                handleWelcomePromptSend,
            })));
            await act(async () => {
                await result.current.handleWelcomePromptSendWithTaskConfig('整理周报');
            });
            await waitFor(() => expect(create).toHaveBeenCalled());
            expect(handleWelcomePromptSend).not.toHaveBeenCalled();
            expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ name: '整理周报' }));
            expect(String((launches.at(-1)?.detail as { initialMessage?: string })?.initialMessage || '')).toBe('/goal 整理周报');
        } finally {
            window.removeEventListener(EVENT_OPEN_TASK_LAUNCH, listener);
            create.mockReset();
        }
    });

    it('applies compose mode when a welcome card stays on the current page', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        const handleWelcomePromptSend = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            isLocalTabActive: false,
            localGuideVisible: false,
            composeAction: 'goal',
            handleWelcomePromptSend,
        })));
        await act(async () => {
            await result.current.handleWelcomePromptSendWithTaskConfig('整理周报');
        });
        expect(handleWelcomePromptSend).toHaveBeenCalledWith('/goal 整理周报', undefined);
        expect(create).not.toHaveBeenCalled();
    });

    it('routes a fullwidth slash welcome card to the command path', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        const handleWelcomePromptSend = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            handleWelcomePromptSend,
        })));
        await act(async () => {
            await result.current.handleWelcomePromptSendWithTaskConfig('／clear');
        });
        expect(handleWelcomePromptSend).toHaveBeenCalledWith('/clear', undefined);
        expect(create).not.toHaveBeenCalled();
    });

    it('does not create a task for a fullwidth reset command on the guide', async () => {
        const create = CreateTaskUnified as unknown as ReturnType<typeof vi.fn>;
        create.mockClear();
        const handleSend = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            localGuideVisible: true,
            inputValue: '／clear',
            handleSend,
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        expect(handleSend).toHaveBeenCalledTimes(1);
        expect(create).not.toHaveBeenCalled();
    });

    it('opens the wizard over a running conversation without clearing it', async () => {
        let wizard = false;
        const saveTabState = vi.fn((_id: string, patch: { newTaskWizard: boolean }) => {
            wizard = patch.newTaskWizard;
        });
        const clearComposerDraft = vi.fn();
        const setQueueInteractionStarted = vi.fn();
        const setEditingEntryId = vi.fn();
        const showComposerText = vi.fn();
        const replacePendingAttachments = vi.fn();
        const replaceSelectedFilePaths = vi.fn();
        const blockedSpy = vi.fn();
        const attachment = { fileName: 'note.txt' };
        window.addEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                assistantBusy: true,
                inputLocked: true,
                inputValue: 'follow up',
                pendingAttachments: [attachment],
                selectedFilePaths: ['D:/picked/spec.md'],
                showComposerText,
                replacePendingAttachments,
                replaceSelectedFilePaths,
                getTabState: () => ({ history: [{ role: 'user', content: 'hi' }], newTaskWizard: wizard }),
                saveTabState,
                clearComposerDraft,
                setQueueInteractionStarted,
                setEditingEntryId,
            })));

            act(() => {
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
            });

            await waitFor(() => {
                expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
            });
            expect(result.current.wizardOverlay).toBe(true);
            expect(result.current.taskConfig.disabled).toBe(false);
            expect(setQueueInteractionStarted).not.toHaveBeenCalled();
            expect(setEditingEntryId).not.toHaveBeenCalled();
            expect(clearComposerDraft).toHaveBeenCalledTimes(1);
            expect(blockedSpy).not.toHaveBeenCalled();

            act(() => {
                result.current.dismissWizardOverlay();
            });
            expect(result.current.wizardOverlay).toBe(false);
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: false });
            expect(showComposerText).toHaveBeenCalledWith('follow up');
            expect(replacePendingAttachments).toHaveBeenCalledWith([attachment]);
            expect(replaceSelectedFilePaths).toHaveBeenCalledWith(['D:/picked/spec.md']);
        } finally {
            window.removeEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        }
    });

    it('covers a finished conversation instead of deleting it', async () => {
        const saveTabState = vi.fn();
        const showComposerText = vi.fn();
        const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
            assistantBusy: false,
            inputLocked: false,
            inputValue: 'next thought',
            getTabState: () => ({ history: [{ role: 'assistant', content: 'done' }] }),
            saveTabState,
            showComposerText,
        })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        expect(result.current.wizardOverlay).toBe(true);
        await waitFor(() => {
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        });

        act(() => {
            result.current.dismissWizardOverlay();
        });
        expect(result.current.wizardOverlay).toBe(false);
        expect(showComposerText).toHaveBeenCalledWith('next thought');
    });

    it('explains itself instead of covering a live recording', async () => {
        const saveTabState = vi.fn();
        const blockedSpy = vi.fn();
        window.addEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        try {
            const { result } = renderHook(() => useTaskConfigWiring(makeOptions({
                assistantBusy: true,
                keepExecutionSurface: true,
                inputLocked: true,
                getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
                saveTabState,
            })));

            act(() => {
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
            });

            await waitFor(() => {
                expect(blockedSpy).toHaveBeenCalled();
            });
            expect(saveTabState).not.toHaveBeenCalled();
            expect(result.current.wizardOverlay).toBe(false);
        } finally {
            window.removeEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        }
    });

    it('leaves no stale wizard flag for non-busy input locks (e.g. recording)', async () => {
        const saveTabState = vi.fn();
        const blockedSpy = vi.fn();
        window.addEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        try {
            renderHook(() => useTaskConfigWiring(makeOptions({
                inputLocked: true,
                assistantBusy: false,
                keepExecutionSurface: true,
                getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
                saveTabState,
            })));

            act(() => {
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
            });

            await new Promise((resolve) => setTimeout(resolve, 10));
            expect(saveTabState).not.toHaveBeenCalled();
            expect(blockedSpy).not.toHaveBeenCalled();
        } finally {
            window.removeEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        }
    });
});

describe('useTaskConfigWiring task type suggestion', () => {
    const suggest = SuggestNewTaskType as unknown as ReturnType<typeof vi.fn>;
    const suggestExpert = SuggestNewTaskExpert as unknown as ReturnType<typeof vi.fn>;

    afterEach(() => {
        cleanup();
        suggest.mockReset();
        suggest.mockResolvedValue({ suggested: '', confident: false });
        suggestExpert.mockReset();
        suggestExpert.mockResolvedValue({ expertId: '', confident: false });
        vi.mocked(CreateTaskUnified).mockReset();
        vi.mocked(CreateRunWindowTask).mockReset();
    });

    function guide(overrides?: Partial<TaskConfigWiringOptions>) {
        return makeOptions({
            localGuideVisible: true,
            inputValue: '修复登录空指针',
            ...overrides,
        });
    }

    it('creates a coding task when the user accepts the switch', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'local' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/fix' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({ showConfirm })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggest).toHaveBeenCalledWith('修复登录空指针');
        expect(showConfirm).toHaveBeenCalledWith(
            expect.stringContaining('「编程」'),
            '改为编程任务？',
            expect.objectContaining({ confirmText: '改为编程', cancelText: '保持会话' }),
        );
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ mode: 'coding_dev' }));
    });

    it('keeps the selected type and still creates when the user refuses', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'llm' });
        const showConfirm = vi.fn().mockResolvedValue(false);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/chat' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({ showConfirm })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ mode: 'chat' }));
    });

    it('does not ask when the command already matches the selected type', async () => {
        suggest.mockResolvedValue({ suggested: 'chat', confident: true, source: 'local' });
        const showConfirm = vi.fn();
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/chat' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '总结会议纪要',
            showConfirm,
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(showConfirm).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ mode: 'chat' }));
    });

    it('still creates the selected type when classification fails', async () => {
        suggest.mockRejectedValue(new Error('classifier down'));
        const showConfirm = vi.fn();
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/chat' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({ showConfirm })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(showConfirm).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ mode: 'chat' }));
    });

    it('drops the cloud workspace when a coding switch is accepted', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'local' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/code' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({ showConfirm })));
        act(() => {
            result.current.taskConfig.onDraftChange(withCloudWorkspace(defaultTaskDraft(), 'cws-1', '讲义'));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(showConfirm.mock.calls[0]?.[0]).toContain('云端工作区');
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({
            mode: 'coding_dev',
            cloudWorkspaceId: '',
        }));
    });

    it('switches a coding draft to chat and keeps the chosen directory', async () => {
        suggest.mockResolvedValue({ suggested: 'chat', confident: true, source: 'local' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/mail' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '写一封请假邮件',
            showConfirm,
        })));
        act(() => {
            result.current.taskConfig.onDraftChange(withLocalWorkspace(
                { ...defaultTaskDraft(), taskType: 'coding' },
                'D:/repo',
            ));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(showConfirm).toHaveBeenCalledWith(
            expect.stringContaining('「会话」'),
            '改为会话任务？',
            expect.objectContaining({ confirmText: '改为会话', cancelText: '保持编程' }),
        );
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({
            mode: 'chat',
            workingDir: 'D:/repo',
        }));
    });

    it('classifies the objective of a /goal command', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'local' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/goal' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '/goal 修复登录空指针',
            showConfirm,
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggest).toHaveBeenCalledWith('修复登录空指针');
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ mode: 'coding_dev' }));
    });

    it('does not retype an expert task or a remote workspace', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'local' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/expert' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '修复登录空指针',
            showConfirm,
        })));
        act(() => {
            result.current.taskConfig.onDraftChange(withExpert(defaultTaskDraft(), 'expert-1', '通用'));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggest).not.toHaveBeenCalled();
        expect(suggestExpert).not.toHaveBeenCalled();
        expect(showConfirm).not.toHaveBeenCalled();

        await waitFor(() => expect(result.current.taskConfig.sending).toBe(false));
        suggest.mockClear();
        create.mockClear();
        act(() => {
            result.current.taskConfig.onDraftChange(withRemoteWorkspace(defaultTaskDraft(), {
                host: 'dev.example',
                port: 22,
                user: 'me',
                password: 'secret',
                workDir: '/src',
            }));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggest).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ mode: 'remote_coding_dev' }));
    });

    it('does not classify a filename standing in for an empty composer', async () => {
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/file' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '',
            pendingAttachments: [{ filePath: 'D:/cases/contract.pdf' }],
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggest).not.toHaveBeenCalled();
    });

    it('stores the switched type on a scheduled task', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'llm' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const schedule = vi.mocked(CreateRunWindowTask);
        schedule.mockResolvedValue('rw-1' as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            showConfirm,
            showAlert: vi.fn(),
        })));
        act(() => {
            result.current.taskConfig.onDraftChange(withRunWindow(defaultTaskDraft(), {
                mode: 'scheduled',
                startAt: '2099-01-02T01:00',
                endAt: '',
            }));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(schedule).toHaveBeenCalled());
        expect(vi.mocked(CreateTaskUnified)).not.toHaveBeenCalled();
        expect(schedule.mock.calls[0]?.[0]).toEqual(expect.objectContaining({
            draft: expect.objectContaining({ taskType: 'coding' }),
        }));
    });

    it('does not create a second task while the type question is open', async () => {
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'local' });
        let answer: (accepted: boolean) => void = () => {};
        const showConfirm = vi.fn(() => new Promise<boolean>((resolve) => {
            answer = resolve;
        }));
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/one' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({ showConfirm })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(showConfirm).toHaveBeenCalledTimes(1));
        expect(create).not.toHaveBeenCalled();
        act(() => {
            result.current.handleSendWithTaskConfig();
        });
        expect(showConfirm).toHaveBeenCalledTimes(1);
        await act(async () => {
            answer(true);
        });
        await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
        expect(create.mock.calls[0]?.[0]).toEqual(expect.objectContaining({ mode: 'coding_dev' }));
    });
});

describe('useTaskConfigWiring expert suggestion', () => {
    const suggest = SuggestNewTaskType as unknown as ReturnType<typeof vi.fn>;
    const suggestExpert = SuggestNewTaskExpert as unknown as ReturnType<typeof vi.fn>;

    afterEach(() => {
        cleanup();
        suggest.mockReset();
        suggest.mockResolvedValue({ suggested: '', confident: false });
        suggestExpert.mockReset();
        suggestExpert.mockResolvedValue({ expertId: '', confident: false });
        vi.mocked(CreateTaskUnified).mockReset();
        vi.mocked(CreateRunWindowTask).mockReset();
    });

    function guide(overrides?: Partial<TaskConfigWiringOptions>) {
        return makeOptions({
            localGuideVisible: true,
            inputValue: '把这篇论文翻译成英文',
            ...overrides,
        });
    }

    it('creates the matching expert and does not ask about the session type', async () => {
        suggestExpert.mockResolvedValue({
            expertId: 'builtin-paper-translate',
            expertName: '论文翻译专家',
            confident: true,
            source: 'local',
        });
        suggest.mockResolvedValue({ suggested: 'coding', confident: true, source: 'local' });
        const showConfirm = vi.fn().mockResolvedValue(true);
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/translate' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({ showConfirm })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggestExpert).toHaveBeenCalledWith('把这篇论文翻译成英文');
        expect(suggest).not.toHaveBeenCalled();
        expect(showConfirm).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({
            expertId: 'builtin-paper-translate',
            expertName: '论文翻译专家',
        }));
    });

    it('stays on 通用 and still classifies the session type when no expert fits', async () => {
        suggestExpert.mockResolvedValue({ expertId: '', confident: false });
        suggest.mockResolvedValue({ suggested: 'chat', confident: true, source: 'local' });
        const showConfirm = vi.fn();
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/chat' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '北京今天天气',
            showConfirm,
        })));
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggest).toHaveBeenCalledWith('北京今天天气');
        expect(showConfirm).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({
            expertId: '',
            mode: 'chat',
        }));
    });

    it('does not classify an expert when a workflow or a cloud workspace is already chosen', async () => {
        suggestExpert.mockResolvedValue({
            expertId: 'builtin-pptx-maker',
            expertName: 'PPT 制作专家',
            confident: true,
        });
        const create = vi.mocked(CreateTaskUnified);
        create.mockResolvedValue({ projectPath: 'D:/tasks/flow' } as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide()));
        act(() => {
            result.current.taskConfig.onDraftChange(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggestExpert).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ expertId: '' }));

        await waitFor(() => expect(result.current.taskConfig.sending).toBe(false));
        suggestExpert.mockClear();
        create.mockClear();
        act(() => {
            result.current.taskConfig.onDraftChange(withCloudWorkspace(defaultTaskDraft(), 'cws-1', '讲义'));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(create).toHaveBeenCalled());
        expect(suggestExpert).not.toHaveBeenCalled();
        expect(create.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({
            expertId: '',
            mode: 'cloud',
        }));
    });

    it('stores the matched expert on a scheduled task', async () => {
        suggestExpert.mockResolvedValue({
            expertId: 'builtin-pptx-maker',
            expertName: 'PPT 制作专家',
            confident: true,
            source: 'llm',
        });
        const schedule = vi.mocked(CreateRunWindowTask);
        schedule.mockResolvedValue('rw-1' as never);
        const { result } = renderHook(() => useTaskConfigWiring(guide({
            inputValue: '做一个项目介绍PPT',
            showAlert: vi.fn(),
        })));
        act(() => {
            result.current.taskConfig.onDraftChange(withRunWindow(defaultTaskDraft(), {
                mode: 'scheduled',
                startAt: '2099-01-02T01:00',
                endAt: '',
            }));
        });
        await act(async () => {
            result.current.handleSendWithTaskConfig();
        });
        await waitFor(() => expect(schedule).toHaveBeenCalled());
        expect(vi.mocked(CreateTaskUnified)).not.toHaveBeenCalled();
        expect(suggest).not.toHaveBeenCalled();
        expect(schedule.mock.calls[0]?.[0]).toEqual(expect.objectContaining({
            draft: expect.objectContaining({
                expertId: 'builtin-pptx-maker',
                expertName: 'PPT 制作专家',
            }),
        }));
    });
});
