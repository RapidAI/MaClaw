// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { TaskWorkspaceTransferDialog, type TaskTransferCloudWorkspace } from '../TaskWorkspaceTransferDialog';

/**
 * Direct coverage for the transfer dialog's own behaviour. The parent-side
 * flows (menu entry, picker cancel, copy ordering, source deletion) live in
 * SidebarTaskManagement.test.tsx; everything here is dialog-internal: the
 * selection rules, the Escape guards, and the create-workspace mini flow.
 */

const ws = (id: string, name?: string, boundTaskTitle?: string): TaskTransferCloudWorkspace => (
    boundTaskTitle ? { id, name: name || id, boundTaskTitle } : { id, name: name || id }
);

/** Dispatches an Escape keydown flagged as IME composition (keyCode 229). */
const dispatchImeEscape = () => {
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
    Object.defineProperty(event, 'keyCode', { value: 229 });
    act(() => { window.dispatchEvent(event); });
};

describe('TaskWorkspaceTransferDialog', () => {
    it('renders nothing while closed', () => {
        render(
            <TaskWorkspaceTransferDialog
                open={false}
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                busy={false}
                error=""
                onConfirm={vi.fn()}
                onClose={vi.fn()}
            />,
        );
        expect(screen.queryByTestId('task-transfer-dialog')).toBeNull();
    });

    it('preselects the first free workspace and confirms with it; bound rows are inert', () => {
        const onConfirm = vi.fn();
        render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_b', 'Archive', 'Old notes')]}
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        const dialog = screen.getByTestId('task-transfer-dialog');
        // The free row carries the check mark; the bound one announces itself.
        expect(within(dialog).getByTestId('task-transfer-workspace-cws_a').getAttribute('aria-checked')).toBe('true');
        expect(within(dialog).getByTestId('task-transfer-workspace-bound-cws_b')).toBeTruthy();
        const boundRow = within(dialog).getByTestId('task-transfer-workspace-cws_b') as HTMLButtonElement;
        expect(boundRow.disabled).toBe(true);
        expect(boundRow.title).toContain('Old notes');

        fireEvent.click(within(dialog).getByTestId('task-transfer-confirm'));
        expect(onConfirm).toHaveBeenCalledWith({ kind: 'cloud', workspaceId: 'cws_a', workspaceName: 'Research' });
    });

    it('re-derives the selection when the picked workspace becomes bound before confirm', () => {
        const onConfirm = vi.fn();
        const view = render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_b', 'Fresh')]}
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        const dialog = () => screen.getByTestId('task-transfer-dialog');
        fireEvent.click(within(dialog()).getByTestId('task-transfer-workspace-cws_b'));

        // Another surface bound cws_b while the dialog was open: the explicit
        // pick must not survive into the confirm.
        view.rerender(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_b', 'Fresh', 'Someone else')]}
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        fireEvent.click(within(dialog()).getByTestId('task-transfer-confirm'));
        expect(onConfirm).toHaveBeenCalledWith({ kind: 'cloud', workspaceId: 'cws_a', workspaceName: 'Research' });
    });

    it('closes on Escape but not on an IME-composing Escape', () => {
        const onClose = vi.fn();
        render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a')]}
                busy={false}
                error=""
                onConfirm={vi.fn()}
                onClose={onClose}
            />,
        );
        // A live IME converts the keystroke, so it must not dismiss the dialog.
        dispatchImeEscape();
        expect(onClose).not.toHaveBeenCalled();
        // A plain Escape still closes: the listener survived the filtered event.
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('ignores Escape and blocks confirm while a move is in flight', () => {
        const onClose = vi.fn();
        const onConfirm = vi.fn();
        render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a')]}
                busy
                error=""
                onConfirm={onConfirm}
                onClose={onClose}
            />,
        );
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).not.toHaveBeenCalled();
        expect((screen.getByTestId('task-transfer-confirm') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId('task-transfer-cancel') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId('task-transfer-close') as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByTestId('task-transfer-confirm').textContent).toContain('Moving');
    });

    it('creates a workspace, selects it, and confirms with the created id', async () => {
        const onConfirm = vi.fn();
        const onCreateCloudWorkspace = vi.fn().mockResolvedValue({ id: 'cws_new', name: 'Sprint 42' });
        const view = render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research')]}
                busy={false}
                error=""
                onCreateCloudWorkspace={onCreateCloudWorkspace}
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        const dialog = () => screen.getByTestId('task-transfer-dialog');
        fireEvent.click(within(dialog()).getByTestId('task-transfer-create-workspace'));
        const input = within(dialog()).getByTestId('task-transfer-create-name') as HTMLInputElement;
        fireEvent.change(input, { target: { value: 'Sprint 42' } });
        fireEvent.click(within(dialog()).getByTestId('task-transfer-create-submit'));

        await waitFor(() => expect(screen.queryByTestId('task-transfer-create-name')).toBeNull());

        // The parent list now carries the created workspace (listed last). The
        // confirm must emit it — only submitCreate's setPickedId explains a
        // cws_new target when cws_a sits first in the list.
        view.rerender(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_new', 'Sprint 42')]}
                busy={false}
                error=""
                onCreateCloudWorkspace={onCreateCloudWorkspace}
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        fireEvent.click(within(dialog()).getByTestId('task-transfer-confirm'));
        expect(onCreateCloudWorkspace).toHaveBeenCalledWith('Sprint 42');
        expect(onConfirm).toHaveBeenCalledWith({ kind: 'cloud', workspaceId: 'cws_new', workspaceName: 'Sprint 42' });
    });

    it('keeps the name editor open with an error when creation is refused', async () => {
        const onCreateCloudWorkspace = vi.fn().mockResolvedValue(null);
        render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research')]}
                busy={false}
                error=""
                onCreateCloudWorkspace={onCreateCloudWorkspace}
                onConfirm={vi.fn()}
                onClose={vi.fn()}
            />,
        );
        const dialog = screen.getByTestId('task-transfer-dialog');
        fireEvent.click(within(dialog).getByTestId('task-transfer-create-workspace'));
        fireEvent.change(within(dialog).getByTestId('task-transfer-create-name'), { target: { value: 'Sprint 42' } });
        fireEvent.click(within(dialog).getByTestId('task-transfer-create-submit'));

        const alert = await screen.findByTestId('task-transfer-create-error');
        expect(alert.textContent).toContain('Another cloud action is still running');
        // The editor stays up so the typed name is not lost.
        expect((within(dialog).getByTestId('task-transfer-create-name') as HTMLInputElement).value).toBe('Sprint 42');
    });

    it('confirms the picked local folder and stays disabled without one', () => {
        const onConfirm = vi.fn();
        const view = render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-local"
                taskName="Cloud research task"
                localDir=""
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        expect((screen.getByTestId('task-transfer-confirm') as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByTestId('task-transfer-local-dir').textContent).toContain('No folder selected');

        view.rerender(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-local"
                taskName="Cloud research task"
                localDir="D:/work/target"
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        fireEvent.click(screen.getByTestId('task-transfer-confirm'));
        expect(onConfirm).toHaveBeenCalledWith({ kind: 'local', dir: 'D:/work/target' });
    });

    it('resets the selection when the dialog is reopened', () => {
        const onConfirm = vi.fn();
        const view = render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_b', 'Fresh')]}
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        fireEvent.click(within(screen.getByTestId('task-transfer-dialog')).getByTestId('task-transfer-workspace-cws_b'));

        view.rerender(
            <TaskWorkspaceTransferDialog
                open={false}
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_b', 'Fresh')]}
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        view.rerender(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-cloud"
                taskName="Build dashboard"
                cloudWorkspaces={[ws('cws_a', 'Research'), ws('cws_b', 'Fresh')]}
                busy={false}
                error=""
                onConfirm={onConfirm}
                onClose={vi.fn()}
            />,
        );
        fireEvent.click(within(screen.getByTestId('task-transfer-dialog')).getByTestId('task-transfer-confirm'));
        // A previous transfer's pick must not leak into the next one.
        expect(onConfirm).toHaveBeenCalledWith({ kind: 'cloud', workspaceId: 'cws_a', workspaceName: 'Research' });
    });

    it('surfaces the caller error through the alert region', () => {
        render(
            <TaskWorkspaceTransferDialog
                open
                lang="en"
                direction="to-local"
                taskName="Cloud research task"
                localDir="D:/work/target"
                busy={false}
                error="The cloud workspace could not be read."
                onConfirm={vi.fn()}
                onClose={vi.fn()}
            />,
        );
        const alert = screen.getByTestId('task-transfer-error');
        expect(alert.getAttribute('role')).toBe('alert');
        expect(alert.textContent).toContain('could not be read');
    });
});
