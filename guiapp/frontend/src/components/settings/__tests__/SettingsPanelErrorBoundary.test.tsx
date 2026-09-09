import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { SettingsPanelErrorBoundary } from '../SettingsPanelErrorBoundary';

function Boom({ fail }: { fail: boolean }) {
    if (fail) throw new Error('chunk boom');
    return <div data-testid="ok">ok</div>;
}

describe('SettingsPanelErrorBoundary', () => {
    it('shows a retry UI when a panel throws and recovers when resetKey changes', async () => {
        const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
        const { rerender } = render(
            <SettingsPanelErrorBoundary lang="en" resetKey="llm">
                <Boom fail />
            </SettingsPanelErrorBoundary>,
        );

        expect(await screen.findByText(/Failed to load this settings panel/i)).toBeTruthy();
        expect(screen.getByRole('button', { name: /Retry/i })).toBeTruthy();

        // Switching tabs (resetKey) must clear the error so the next panel can render.
        rerender(
            <SettingsPanelErrorBoundary lang="en" resetKey="general">
                <Boom fail={false} />
            </SettingsPanelErrorBoundary>,
        );
        expect(await screen.findByTestId('ok')).toBeTruthy();
        spy.mockRestore();
    });

    it('remounts children on Retry so a healed panel can render', async () => {
        const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
        let shouldFail = true;
        function ToggleBoom() {
            if (shouldFail) throw new Error('chunk boom');
            return <div data-testid="ok">ok</div>;
        }
        render(
            <SettingsPanelErrorBoundary lang="en" resetKey="llm">
                <ToggleBoom />
            </SettingsPanelErrorBoundary>,
        );
        const retry = await screen.findByRole('button', { name: /Retry/i });
        shouldFail = false;
        fireEvent.click(retry);
        expect(await screen.findByTestId('ok')).toBeTruthy();
        spy.mockRestore();
    });
});
