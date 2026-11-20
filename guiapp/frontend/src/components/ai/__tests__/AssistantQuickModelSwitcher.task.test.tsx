// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AssistantQuickModelSwitcher } from '../AssistantQuickModelSwitcher';
import { overlayTheme } from '../aiAssistantPanelTheme';

vi.mock('../../../wailsjs/go/main/App', () => ({}));

describe('open-task model menu', () => {
    afterEach(() => cleanup());

    it('puts Use model assignment first and leaves the global menu unchanged', () => {
        const onFollow = vi.fn();
        render(
            <AssistantQuickModelSwitcher
                lang="zh"
                theme={overlayTheme}
                availableProviders={[
                    { id: 'devin', name: 'Devin', url: '', configured: true, isHubService: true },
                    { id: 'local', name: 'Local', url: '', configured: true, isHubService: false },
                ]}
                currentModel="swe-2-max"
                modelOptions={['swe-2-max']}
                onSwitchProvider={vi.fn()}
                onSwitchModel={vi.fn()}
                onAddCustomModel={vi.fn()}
                onFollowAssignment={onFollow}
                followsAssignment
                labelOverride="本任务 · Devin · swe-2-max"
                chipTestId="composer-model-chip"
            />,
        );
        expect(screen.getByTestId('composer-model-chip').textContent).toContain('本任务');
        fireEvent.click(screen.getByTestId('composer-model-chip'));
        const rows = Array.from(screen.getByTestId('qs-model-menu').querySelectorAll('[role="option"]'));
        expect(rows[0].getAttribute('data-testid')).toBe('qs-model-menu-follow');
        expect(rows[0].textContent).toContain('使用模型分配');
        const add = screen.getByTestId('qs-model-menu-add-custom');
        expect(rows.indexOf(add)).toBe(2);
        fireEvent.click(rows[0]);
        expect(onFollow).toHaveBeenCalledTimes(1);
        cleanup();

        render(
            <AssistantQuickModelSwitcher
                lang="zh"
                theme={overlayTheme}
                availableProviders={[
                    { id: 'devin', name: 'Devin', url: '', configured: true, isHubService: true },
                    { id: 'local', name: 'Local', url: '', configured: true, isHubService: false },
                ]}
                currentModel="swe-2-max"
                modelOptions={['swe-2-max']}
                onSwitchProvider={vi.fn()}
                onSwitchModel={vi.fn()}
                onAddCustomModel={vi.fn()}
            />,
        );
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        expect(screen.queryByTestId('qs-model-menu-follow')).toBeNull();
        const plain = Array.from(screen.getByTestId('qs-model-menu').querySelectorAll('[role="option"]'));
        expect(plain[0].textContent).toContain('Local');
        expect(screen.getByTestId('qs-model-menu-add-custom')).toBe(plain[1]);
    });
});
