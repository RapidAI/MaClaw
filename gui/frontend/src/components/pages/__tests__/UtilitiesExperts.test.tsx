// @vitest-environment jsdom
/**
 * Tests for the AI expert section on the utilities home view:
 * card rendering (builtin vs user), new-expert entry, delete confirmation flow.
 *
 * UtilitiesPage/ExpertEditorDialog reach the backend through a dynamic
 * `await import('wailsjs/go/main/App')` (getApp) which vi.mock cannot intercept,
 * so the tests drive the real generated bindings via the injected `window.go`
 * bridge — exactly how Wails exposes them at runtime.
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
    BrowserOpenURL: vi.fn(),
}));

const builtinExpert = {
    id: 'builtin-paper-polish',
    name: '论文润色',
    description: '学术语言润色，保持原意',
    icon: '📝',
    system_prompt: '你是论文润色专家',
    tools: [],
    skills: [],
    builtin: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
};

const userExpert = {
    id: 'user-exp-1',
    name: '我的助手',
    description: '自定义专家',
    icon: '',
    system_prompt: '自定义',
    tools: ['fs_read'],
    skills: [],
    builtin: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
};

type AppSpies = {
    ListExperts: ReturnType<typeof vi.fn>;
    DeleteExpert: ReturnType<typeof vi.fn>;
    ResetBuiltinExpert: ReturnType<typeof vi.fn>;
    SaveExpert: ReturnType<typeof vi.fn>;
    GenerateExpertProfile: ReturnType<typeof vi.fn>;
    ListAvailableToolNames: ReturnType<typeof vi.fn>;
    ListNLSkills: ReturnType<typeof vi.fn>;
    GetACPHostStatus: ReturnType<typeof vi.fn>;
};

function installAppSpies(experts: unknown[] = [builtinExpert, userExpert]): AppSpies {
    const spies: AppSpies = {
        ListExperts: vi.fn().mockResolvedValue(JSON.stringify(experts)),
        DeleteExpert: vi.fn().mockResolvedValue(undefined),
        ResetBuiltinExpert: vi.fn().mockResolvedValue(undefined),
        SaveExpert: vi.fn(),
        GenerateExpertProfile: vi.fn(),
        ListAvailableToolNames: vi.fn().mockResolvedValue('[]'),
        ListNLSkills: vi.fn().mockResolvedValue([]),
        GetACPHostStatus: vi.fn().mockRejectedValue(new Error('no backend')),
    };
    (window as any).go = { main: { App: spies } };
    return spies;
}

describe('UtilitiesPage AI expert section', () => {
    beforeEach(() => {
        installAppSpies();
    });

    afterEach(() => {
        delete (window as any).go;
    });

    it('renders expert cards from ListExperts with icon, name and description', async () => {
        render(<UtilitiesPage lang="zh-Hans" />);
        expect(screen.getByTestId('utilities-experts-section')).toBeTruthy();
        await waitFor(() => expect(screen.getByTestId('utilities-expert-card-builtin-paper-polish')).toBeTruthy());
        expect(screen.getByTestId('utilities-expert-card-user-exp-1')).toBeTruthy();
        expect(screen.getByText('论文润色')).toBeTruthy();
        expect(screen.getByText('学术语言润色，保持原意')).toBeTruthy();
        // Empty icon falls back to the default emoji.
        expect(screen.getByTestId('utilities-expert-card-user-exp-1').textContent).toContain('🤖');
        // Section header (三语: zh-Hans here).
        expect(screen.getByText('AI 专家')).toBeTruthy();
    });

    it('always shows the dashed new-expert card', async () => {
        installAppSpies([]);
        render(<UtilitiesPage lang="en" />);
        await waitFor(() => expect((window as any).go.main.App.ListExperts).toHaveBeenCalled());
        expect(screen.getByTestId('utilities-expert-new-card')).toBeTruthy();
        expect(screen.getByText('New expert')).toBeTruthy();
    });

    it('clicking an expert card invokes onOpenExpert with the full definition', async () => {
        const onOpenExpert = vi.fn();
        render(<UtilitiesPage lang="zh-Hans" onOpenExpert={onOpenExpert} />);
        await waitFor(() => expect(screen.getByTestId('utilities-expert-card-builtin-paper-polish')).toBeTruthy());
        fireEvent.click(screen.getByText('论文润色'));
        expect(onOpenExpert).toHaveBeenCalledTimes(1);
        expect(onOpenExpert.mock.calls[0][0].id).toBe('builtin-paper-polish');
    });

    it('builtin cards expose edit + reset-default, user cards expose edit + delete', async () => {
        render(<UtilitiesPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getByTestId('utilities-expert-card-builtin-paper-polish')).toBeTruthy());
        expect(screen.getByTestId('utilities-expert-edit-builtin-paper-polish')).toBeTruthy();
        expect(screen.getByTestId('utilities-expert-reset-builtin-paper-polish')).toBeTruthy();
        expect(screen.queryByTestId('utilities-expert-delete-builtin-paper-polish')).toBeNull();
        expect(screen.getByTestId('utilities-expert-edit-user-exp-1')).toBeTruthy();
        expect(screen.getByTestId('utilities-expert-delete-user-exp-1')).toBeTruthy();
        expect(screen.queryByTestId('utilities-expert-reset-user-exp-1')).toBeNull();
    });

    it('delete goes through the confirm dialog before calling DeleteExpert', async () => {
        const deletedEvents: string[] = [];
        window.addEventListener('maclaw:expert-deleted', ((e: CustomEvent) => {
            deletedEvents.push(String(e?.detail?.expertId || ''));
        }) as EventListener);
        render(<UtilitiesPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getByTestId('utilities-expert-delete-user-exp-1')).toBeTruthy());
        const spies = (window as any).go.main.App as AppSpies;

        fireEvent.click(screen.getByTestId('utilities-expert-delete-user-exp-1'));
        // Confirmation shown; nothing deleted yet.
        expect(screen.getByText('删除专家')).toBeTruthy();
        expect(spies.DeleteExpert).not.toHaveBeenCalled();

        fireEvent.click(screen.getByText('取消'));
        expect(spies.DeleteExpert).not.toHaveBeenCalled();

        // Re-open and confirm.
        fireEvent.click(screen.getByTestId('utilities-expert-delete-user-exp-1'));
        const confirmButton = document.querySelector('.confirm-dialog__button--danger') as HTMLButtonElement;
        expect(confirmButton).toBeTruthy();
        fireEvent.click(confirmButton);
        await waitFor(() => expect(spies.DeleteExpert).toHaveBeenCalledWith('user-exp-1'));
        // List refreshes after deletion and open tabs are notified.
        await waitFor(() => expect(spies.ListExperts.mock.calls.length).toBeGreaterThanOrEqual(2));
        expect(deletedEvents).toContain('user-exp-1');
    });

    it('reset-default asks for confirmation, then calls ResetBuiltinExpert and refreshes', async () => {
        const deletedEvents: string[] = [];
        window.addEventListener('maclaw:expert-deleted', ((e: CustomEvent) => {
            deletedEvents.push(String(e?.detail?.expertId || ''));
        }) as EventListener);
        render(<UtilitiesPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getByTestId('utilities-expert-reset-builtin-paper-polish')).toBeTruthy());
        const spies = (window as any).go.main.App as AppSpies;

        fireEvent.click(screen.getByTestId('utilities-expert-reset-builtin-paper-polish'));
        // Confirmation first — the override may hold substantial user edits.
        expect(screen.getByText('恢复默认专家')).toBeTruthy();
        expect(spies.ResetBuiltinExpert).not.toHaveBeenCalled();

        const confirmButton = document.querySelector('.confirm-dialog__button--danger') as HTMLButtonElement;
        expect(confirmButton).toBeTruthy();
        fireEvent.click(confirmButton);
        await waitFor(() => expect(spies.ResetBuiltinExpert).toHaveBeenCalledWith('builtin-paper-polish'));
        await waitFor(() => expect(spies.ListExperts.mock.calls.length).toBeGreaterThanOrEqual(2));
        expect(deletedEvents).toContain('builtin-paper-polish');
    });

    it('opens the editor dialog in create mode from the new-expert card', async () => {
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-expert-new-card'));
        await waitFor(() => expect(screen.getByTestId('expert-editor-overlay')).toBeTruthy());
        expect(screen.getByTestId('expert-idea-input')).toBeTruthy();
        expect(screen.getByTestId('expert-generate-button')).toBeTruthy();
    });

    it('opens the editor dialog in edit mode (no idea generator row)', async () => {
        render(<UtilitiesPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getByTestId('utilities-expert-edit-user-exp-1')).toBeTruthy());
        fireEvent.click(screen.getByTestId('utilities-expert-edit-user-exp-1'));
        await waitFor(() => expect(screen.getByTestId('expert-editor-overlay')).toBeTruthy());
        expect(screen.queryByTestId('expert-idea-input')).toBeNull();
        expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('我的助手');
    });
});
