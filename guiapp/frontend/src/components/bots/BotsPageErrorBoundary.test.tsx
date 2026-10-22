import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotsPageErrorBoundary } from './BotsPageErrorBoundary';

function Boom(): JSX.Element {
    throw new Error('render failed');
}

describe('BotsPageErrorBoundary', () => {
    it('keeps a Bot page crash inside the page and reports it', () => {
        const log = vi.fn(async () => undefined);
        (window as any).go = { main: { App: { LogFrontendDiagnostic: log } } };
        const quiet = vi.spyOn(console, 'error').mockImplementation(() => undefined);
        render(<BotsPageErrorBoundary lang="zh-Hans"><Boom /></BotsPageErrorBoundary>);
        expect(screen.getByTestId('bots-page-crash')).toBeTruthy();
        expect(log).toHaveBeenCalledWith(expect.objectContaining({ tag: 'frontend-issue', area: 'bots-page-crash' }));
        quiet.mockRestore();
        delete (window as any).go;
    });
});
