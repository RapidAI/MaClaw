import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useTaskConfigWiring, type TaskConfigWiringOptions } from '../useTaskConfigWiring';
import { EVENT_NEW_TASK_WIZARD_BLOCKED, EVENT_OPEN_NEW_TASK_WIZARD } from '../../../../constants/events';

const selectWorkingDirMock = vi.fn();
vi.mock('../../../../../wailsjs/go/main/App', () => ({
    ListExperts: vi.fn().mockResolvedValue('[]'),
    ListManagedIndustryExperts: vi.fn().mockResolvedValue(''),
    ListWorkflowTemplateSummaries: vi.fn().mockResolvedValue([]),
    CloudWorkspaceEntitlement: vi.fn().mockResolvedValue({ workspaces: [] }),
    CreateCloudWorkspace: vi.fn(),
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
        messages: [],
        handleSend: vi.fn(),
        handleWelcomePromptSend: vi.fn(),
        clearComposerDraft: vi.fn(),
        clearActiveHistory: vi.fn(),
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

describe('useTaskConfigWiring new-task wizard opening', () => {
    afterEach(() => {
        cleanup();
    });

    it('marks the clean local tab as a wizard page when the sidebar button is clicked', async () => {
        const saveTabState = vi.fn();
        const clearActiveHistory = vi.fn();
        renderHook(() => useTaskConfigWiring(makeOptions({ saveTabState, clearActiveHistory })));

        act(() => {
            window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
        });

        await waitFor(() => {
            expect(saveTabState).toHaveBeenCalledWith('tab-1', { newTaskWizard: true });
        });
    });

    it('notifies and leaves the tab untouched when a running task blocks the wizard', async () => {
        const saveTabState = vi.fn();
        const clearActiveHistory = vi.fn();
        const blockedSpy = vi.fn();
        window.addEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        try {
            renderHook(() => useTaskConfigWiring(makeOptions({
                assistantBusy: true,
                getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
                saveTabState,
                clearActiveHistory,
            })));

            act(() => {
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
            });

            await waitFor(() => {
                expect(blockedSpy).toHaveBeenCalled();
            });
            expect(saveTabState).not.toHaveBeenCalled();
            expect(clearActiveHistory).not.toHaveBeenCalled();
        } finally {
            window.removeEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        }
    });

    it('leaves no stale wizard flag for non-busy input locks (e.g. recording)', async () => {
        const saveTabState = vi.fn();
        const clearActiveHistory = vi.fn();
        const blockedSpy = vi.fn();
        window.addEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        try {
            renderHook(() => useTaskConfigWiring(makeOptions({
                inputLocked: true,
                assistantBusy: false,
                getTabState: () => ({ history: [{ role: 'user', content: 'hi' }] }),
                saveTabState,
                clearActiveHistory,
            })));

            act(() => {
                window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD));
            });

            await new Promise((resolve) => setTimeout(resolve, 10));
            expect(saveTabState).not.toHaveBeenCalled();
            expect(clearActiveHistory).not.toHaveBeenCalled();
            expect(blockedSpy).not.toHaveBeenCalled();
        } finally {
            window.removeEventListener(EVENT_NEW_TASK_WIZARD_BLOCKED, blockedSpy);
        }
    });
});
