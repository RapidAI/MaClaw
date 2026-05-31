import { render, fireEvent, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AssistantWorkflowMaximizeSuggestion } from '../AssistantWorkflowMaximizeSuggestion';
import { lightTheme } from '../aiAssistantPanelTheme';

describe('AssistantWorkflowMaximizeSuggestion', () => {
    it('uses maximize copy and toggles maximized view', () => {
        const onToggleMaximize = vi.fn();
        const onDismiss = vi.fn();

        render(
            <AssistantWorkflowMaximizeSuggestion
                inline
                lang="en"
                maximized={false}
                onDismiss={onDismiss}
                onToggleMaximize={onToggleMaximize}
                suggestMaximize
                theme={lightTheme}
                themeMode="light"
            />
        );

        expect(screen.getByText('Workflow is starting. Maximized view is recommended.')).toBeTruthy();
        expect(screen.queryByText('Fullscreen')).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: 'Maximize' }));

        expect(onToggleMaximize).toHaveBeenCalledTimes(1);
        expect(onDismiss).toHaveBeenCalledTimes(1);
    });
});
