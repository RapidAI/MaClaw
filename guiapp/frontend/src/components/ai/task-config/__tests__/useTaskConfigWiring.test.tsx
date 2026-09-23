import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CloudWorkspaceEntitlement, CreateCloudWorkspace, CreateTaskUnified, RenameCloudWorkspace } from '../../../../../wailsjs/go/main/App';
import { useTaskConfigWiring, type TaskConfigWiringOptions } from '../useTaskConfigWiring';
import { defaultTaskDraft, withCloudWorkspace } from '../taskDraft';
import { EVENT_NEW_TASK_WIZARD_BLOCKED, EVENT_OPEN_NEW_TASK_WIZARD, EVENT_OPEN_TASK_LAUNCH } from '../../../../constants/events';

const selectWorkingDirMock = vi.fn();
vi.mock('../../../../../wailsjs/go/main/App', () => ({
    ListExperts: vi.fn().mockResolvedValue('[]'),
    ListManagedIndustryExperts: vi.fn().mockResolvedValue(''),
    ListWorkflowTemplateSummaries: vi.fn().mockResolvedValue([]),
    CloudWorkspaceEntitlement: vi.fn().mockResolvedValue({ workspaces: [] }),
    CreateCloudWorkspace: vi.fn(),
    RenameCloudWorkspace: vi.fn(),
    CreateTaskUnified: vi.fn(),
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
    afterEach(() => {
        cleanup();
    });

    it('keeps the guide draft when the wizard is opened from another tab', async () => {
        const clearComposerDraft = vi.fn();
        const activateTab = vi.fn();
        const saveTabState = vi.fn();
        renderHook(() => useTaskConfigWiring(makeOptions({
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

        await waitFor(() => {
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        });
        expect(activateTab).toHaveBeenCalledWith('tab-1');
        expect(clearComposerDraft).not.toHaveBeenCalled();
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

        await waitFor(() => {
            expect(result.current.wizardOverlay).toBe(true);
        });
        expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });

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
