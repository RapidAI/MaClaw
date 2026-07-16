// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { LansengerSettings } from '../LansengerSettings';

const RestartLansengerMock = vi.fn();
const SetLansengerLocalModeMock = vi.fn();
const LoadConfigMock = vi.fn();
const ListLansengerGroupsMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    RestartLansenger: (...args: unknown[]) => RestartLansengerMock(...args),
    SetLansengerLocalMode: (...args: unknown[]) => SetLansengerLocalModeMock(...args),
    ListLansengerGroups: (...args: unknown[]) => ListLansengerGroupsMock(...args),
}));

vi.mock('../../pages/UtilitiesWatchPanel', () => ({
    UtilitiesWatchPanel: ({
        onBack,
        compactHeader,
    }: {
        isZh: boolean;
        onBack: () => void;
        compactHeader?: boolean;
    }) => (
        <div data-testid="watch-page" data-compact={compactHeader ? '1' : '0'}>
            <button type="button" onClick={onBack}>
                back
            </button>
        </div>
    ),
}));

const baseProps = () => ({
    config: {
        lansenger_enabled: true,
        lansenger_app_id: 'app-id',
        lansenger_app_secret: 'secret',
        lansenger_gateway_url: 'https://apigw.lx.qianxin.com',
    } as any,
    setConfig: vi.fn(),
    lang: 'zh-Hans',
    saveRemoteConfigField: vi.fn(),
    lansengerStatus: 'connected',
    setLansengerStatus: vi.fn(),
    lansengerLocalMode: true,
    setLansengerLocalModeState: vi.fn(),
    setIMAuditPlatform: vi.fn(),
});

describe('LansengerSettings', () => {
    it('saves fields and exposes restart and audit actions', async () => {
        const props = baseProps();
        RestartLansengerMock.mockResolvedValue('connected');

        render(<LansengerSettings {...props} />);

        fireEvent.click(screen.getByLabelText('\u542f\u7528\u84dd\u4fe1'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_enabled: false });

        fireEvent.change(screen.getByDisplayValue('app-id'), { target: { value: 'new-app' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_app_id: 'new-app' });

        fireEvent.click(screen.getByRole('button', { name: '\u91cd\u542f' }));
        await waitFor(() => expect(props.setLansengerStatus).toHaveBeenCalledWith('connected'));

        fireEvent.click(screen.getByRole('button', { name: '\u76d1\u770b' }));
        expect(props.setIMAuditPlatform).toHaveBeenCalledWith('lansenger');
    });

    it('shows Follow only when Lansenger is connected, after Watch', async () => {
        const { rerender } = render(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();

        rerender(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        const follow = screen.getByTestId('lansenger-follow-button');
        expect(follow).toBeTruthy();
        expect(follow.textContent).toBe('\u5173\u6ce8');

        // Follow sits after 监看 in the toolbar
        const watchBtn = screen.getByRole('button', { name: '\u76d1\u770b' });
        expect(watchBtn.compareDocumentPosition(follow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(follow);
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
    });

    it('closes Follow panel when connection drops', async () => {
        const { rerender } = render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        rerender(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        await waitFor(() => {
            expect(screen.queryByTestId('watch-page')).toBeNull();
            expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        });
    });

    it('closes Follow panel via Escape', async () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
    });

    it('opens Follow in compact embed mode and closes via header ×', async () => {
        ListLansengerGroupsMock.mockResolvedValue({ total: 0, groups: [] });
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        const page = await screen.findByTestId('watch-page');
        expect(page.getAttribute('data-compact')).toBe('1');
        expect(screen.getByRole('dialog', { name: '\u5173\u6ce8' })).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '\u5173\u95ed' }));
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
    });

    it('does not open Follow when disconnected', () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        expect(screen.queryByTestId('watch-page')).toBeNull();
    });

    it('locks body scroll while Follow dialog is open', async () => {
        const prev = document.body.style.overflow;
        document.body.style.overflow = '';
        try {
            render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
            fireEvent.click(screen.getByTestId('lansenger-follow-button'));
            await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
            expect(document.body.style.overflow).toBe('hidden');

            fireEvent.keyDown(window, { key: 'Escape' });
            await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
            expect(document.body.style.overflow).toBe('');
        } finally {
            document.body.style.overflow = prev;
        }
    });

    it('does not close Follow on Escape during IME composition', async () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        fireEvent.keyDown(window, { key: 'Escape', isComposing: true });
        expect(screen.getByTestId('watch-page')).toBeTruthy();

        fireEvent.keyDown(window, { key: 'Escape', keyCode: 229 });
        expect(screen.getByTestId('watch-page')).toBeTruthy();
    });
});
