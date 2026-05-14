// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { VirtualEmployeeSettingsPanel } from '../VirtualEmployeeSettingsPanel';

const RegisterVirtualEmployeeMock = vi.fn();
const UpdateVESettingsMock = vi.fn();
const GetVEStatusMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    RegisterVirtualEmployee: (...args: unknown[]) => RegisterVirtualEmployeeMock(...args),
    UpdateVESettings: (...args: unknown[]) => UpdateVESettingsMock(...args),
    GetVEStatus: (...args: unknown[]) => GetVEStatusMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe('VirtualEmployeeSettingsPanel', () => {
    // ─── Conditional Rendering ───────────────────────────────────────────

    describe('conditional rendering', () => {
        it('does not render when remoteMachineId is empty', () => {
            GetVEStatusMock.mockResolvedValue({ registered: false });
            const { container } = render(
                <VirtualEmployeeSettingsPanel remoteMachineId="" />
            );
            expect(container.querySelector('[data-testid="ve-settings-panel"]')).toBeNull();
        });

        it('renders when remoteMachineId is non-empty', () => {
            GetVEStatusMock.mockResolvedValue({ registered: false });
            render(
                <VirtualEmployeeSettingsPanel remoteMachineId="machine-123" />
            );
            expect(screen.getByTestId('ve-settings-panel')).toBeTruthy();
        });
    });

    // ─── Form Validation ─────────────────────────────────────────────────

    describe('form validation', () => {
        beforeEach(() => {
            GetVEStatusMock.mockResolvedValue({ registered: false });
        });

        it('shows error when name is empty on submit', async () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.click(screen.getByTestId('ve-submit-btn'));

            expect(screen.getByTestId('name-error')).toBeTruthy();
            expect(screen.getByTestId('name-error').textContent).toBe('名称不能为空');
            expect(RegisterVirtualEmployeeMock).not.toHaveBeenCalled();
        });

        it('shows error when name exceeds 50 characters', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            const nameInput = screen.getByLabelText('名称');
            // The input has maxLength=50, but we test the validation logic
            fireEvent.change(nameInput, { target: { value: 'a'.repeat(51) } });

            expect(screen.getByTestId('name-error').textContent).toBe('名称不能超过50个字符');
        });

        it('shows error when skill description is empty on submit', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            // Fill name but leave skill empty
            fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'Test VE' } });
            fireEvent.click(screen.getByTestId('ve-submit-btn'));

            expect(screen.getByTestId('skill-error')).toBeTruthy();
            expect(screen.getByTestId('skill-error').textContent).toBe('技能描述不能为空');
        });

        it('shows error when skill description exceeds 500 characters', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            const skillInput = screen.getByLabelText('技能描述');
            fireEvent.change(skillInput, { target: { value: 'x'.repeat(501) } });

            expect(screen.getByTestId('skill-error').textContent).toBe('技能描述不能超过500个字符');
        });

        it('shows error when access policy is not selected on submit', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'Test VE' } });
            fireEvent.change(screen.getByLabelText('技能描述'), { target: { value: 'Some skill' } });
            fireEvent.click(screen.getByTestId('ve-submit-btn'));

            expect(screen.getByTestId('policy-error')).toBeTruthy();
            expect(screen.getByTestId('policy-error').textContent).toBe('请选择访问策略');
        });

        it('clears validation errors when valid input is provided', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            // Trigger errors
            fireEvent.click(screen.getByTestId('ve-submit-btn'));
            expect(screen.getByTestId('name-error')).toBeTruthy();

            // Fix name
            fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'Valid Name' } });
            expect(screen.queryByTestId('name-error')).toBeNull();
        });
    });

    // ─── Policy Switch Behavior ──────────────────────────────────────────

    describe('policy switch behavior', () => {
        beforeEach(() => {
            GetVEStatusMock.mockResolvedValue({ registered: false });
        });

        it('shows whitelist editor when "whitelist" is selected', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'whitelist' } });

            expect(screen.getByTestId('list-editor')).toBeTruthy();
            expect(screen.getByText('白名单')).toBeTruthy();
        });

        it('shows blacklist editor when "blacklist" is selected', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'blacklist' } });

            expect(screen.getByTestId('list-editor')).toBeTruthy();
            expect(screen.getByText('黑名单')).toBeTruthy();
        });

        it('hides list editor when "public" is selected', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            // First select whitelist to show editor
            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'whitelist' } });
            expect(screen.getByTestId('list-editor')).toBeTruthy();

            // Switch to public
            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'public' } });
            expect(screen.queryByTestId('list-editor')).toBeNull();
        });

        it('hides list editor when "per_request" is selected', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            // First select blacklist to show editor
            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'blacklist' } });
            expect(screen.getByTestId('list-editor')).toBeTruthy();

            // Switch to per_request
            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'per_request' } });
            expect(screen.queryByTestId('list-editor')).toBeNull();
        });

        it('allows adding items to whitelist', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'whitelist' } });

            const input = screen.getByTestId('list-input');
            fireEvent.change(input, { target: { value: 'user-abc' } });
            fireEvent.click(screen.getByTestId('list-add-btn'));

            expect(screen.getByText('user-abc')).toBeTruthy();
        });

        it('allows removing items from blacklist', () => {
            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'blacklist' } });

            // Add an item
            const input = screen.getByTestId('list-input');
            fireEvent.change(input, { target: { value: 'blocked-user' } });
            fireEvent.click(screen.getByTestId('list-add-btn'));
            expect(screen.getByText('blocked-user')).toBeTruthy();

            // Remove it
            fireEvent.click(screen.getByTestId('remove-blocked-user'));
            expect(screen.queryByText('blocked-user')).toBeNull();
        });
    });

    // ─── Status Display ──────────────────────────────────────────────────

    describe('status display', () => {
        it('shows "审核中" badge when status is pending', async () => {
            GetVEStatusMock.mockResolvedValue({
                registered: true,
                employee: { name: 'VE1', skill_description: 'skill', access_policy: 'public', status: 'pending' },
            });

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            await waitFor(() => {
                expect(screen.getByTestId('ve-status-badge').textContent).toBe('审核中');
            });
        });

        it('shows "已激活" badge when status is active', async () => {
            GetVEStatusMock.mockResolvedValue({
                registered: true,
                employee: { name: 'VE1', skill_description: 'skill', access_policy: 'public', status: 'active' },
            });

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            await waitFor(() => {
                expect(screen.getByTestId('ve-status-badge').textContent).toBe('已激活');
            });
        });

        it('shows "已禁用" badge when status is disabled', async () => {
            GetVEStatusMock.mockResolvedValue({
                registered: true,
                employee: { name: 'VE1', skill_description: 'skill', access_policy: 'public', status: 'disabled' },
            });

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            await waitFor(() => {
                expect(screen.getByTestId('ve-status-badge').textContent).toBe('已禁用');
            });
        });

        it('shows "已拒绝" badge when status is rejected', async () => {
            GetVEStatusMock.mockResolvedValue({
                registered: true,
                employee: { name: 'VE1', skill_description: 'skill', access_policy: 'public', status: 'rejected' },
            });

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            await waitFor(() => {
                expect(screen.getByTestId('ve-status-badge').textContent).toBe('已拒绝');
            });
        });

        it('does not show status badge when not registered', () => {
            GetVEStatusMock.mockResolvedValue({ registered: false });

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            expect(screen.queryByTestId('ve-status-badge')).toBeNull();
        });
    });

    // ─── Registration / Update Submission ────────────────────────────────

    describe('form submission', () => {
        it('calls RegisterVirtualEmployee with correct args on first registration', async () => {
            GetVEStatusMock.mockResolvedValue({ registered: false });
            RegisterVirtualEmployeeMock.mockResolvedValue(undefined);

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'My VE' } });
            fireEvent.change(screen.getByLabelText('技能描述'), { target: { value: 'AI assistant' } });
            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'public' } });

            fireEvent.click(screen.getByTestId('ve-submit-btn'));

            await waitFor(() => {
                expect(RegisterVirtualEmployeeMock).toHaveBeenCalledWith('My VE', 'AI assistant', 'public', []);
            });
        });

        it('calls UpdateVESettings when already registered', async () => {
            GetVEStatusMock.mockResolvedValue({
                registered: true,
                employee: { name: 'Old Name', skill_description: 'old skill', access_policy: 'public', status: 'active' },
            });
            UpdateVESettingsMock.mockResolvedValue(undefined);

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            await waitFor(() => {
                expect(screen.getByTestId('ve-status-badge')).toBeTruthy();
            });

            // Update name
            fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'New Name' } });
            fireEvent.click(screen.getByTestId('ve-submit-btn'));

            await waitFor(() => {
                expect(UpdateVESettingsMock).toHaveBeenCalledWith('New Name', 'old skill', 'public', []);
            });
        });

        it('passes whitelist array when policy is whitelist', async () => {
            GetVEStatusMock.mockResolvedValue({ registered: false });
            RegisterVirtualEmployeeMock.mockResolvedValue(undefined);

            render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

            fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'VE' } });
            fireEvent.change(screen.getByLabelText('技能描述'), { target: { value: 'skill' } });
            fireEvent.change(screen.getByLabelText('访问策略'), { target: { value: 'whitelist' } });

            // Add whitelist items
            const input = screen.getByTestId('list-input');
            fireEvent.change(input, { target: { value: 'user-a' } });
            fireEvent.click(screen.getByTestId('list-add-btn'));
            fireEvent.change(input, { target: { value: 'user-b' } });
            fireEvent.click(screen.getByTestId('list-add-btn'));

            fireEvent.click(screen.getByTestId('ve-submit-btn'));

            await waitFor(() => {
                expect(RegisterVirtualEmployeeMock).toHaveBeenCalledWith('VE', 'skill', 'whitelist', ['user-a', 'user-b']);
            });
        });
    });
});
