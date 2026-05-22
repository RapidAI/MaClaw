// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { LansengerSettings } from '../LansengerSettings';

const RestartLansengerMock = vi.fn();
const SetLansengerLocalModeMock = vi.fn();
const LoadConfigMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    RestartLansenger: (...args: unknown[]) => RestartLansengerMock(...args),
    SetLansengerLocalMode: (...args: unknown[]) => SetLansengerLocalModeMock(...args),
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
});
