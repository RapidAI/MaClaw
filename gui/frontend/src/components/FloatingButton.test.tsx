/** @vitest-environment jsdom */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EventsOn } from '../../wailsjs/runtime';

import { FloatingButton } from './FloatingButton';

vi.mock('../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

const loadConfigMock = vi.fn();
const patchConfigFieldsMock = vi.fn();
const openPetSettingsMock = vi.fn();
const quitAppMock = vi.fn();
const clickedMock = vi.fn();
const draggedMock = vi.fn();

describe('FloatingButton context menu', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        loadConfigMock.mockResolvedValue({
            pet_enabled: true,
            pet_skin: 'clawmate',
            pet_size: 88,
            pet_motion_enabled: true,
            pet_motion_sound_enabled: true,
            pet_motion_sound_preset: 'classic',
            pet_quiet_mode: false,
            pet_interaction_mode: 'balanced',
        });
        patchConfigFieldsMock.mockResolvedValue({
            pet_enabled: true,
            pet_skin: 'clawmate',
            pet_size: 88,
            pet_motion_enabled: true,
            pet_motion_sound_enabled: false,
            pet_motion_sound_preset: 'classic',
            pet_quiet_mode: false,
            pet_interaction_mode: 'balanced',
        });
        openPetSettingsMock.mockResolvedValue(undefined);
        quitAppMock.mockResolvedValue(undefined);
        clickedMock.mockResolvedValue(undefined);
        draggedMock.mockResolvedValue(undefined);
        window.go = {
            main: {
                App: {
                    LoadConfig: loadConfigMock,
                    PatchConfigFields: patchConfigFieldsMock,
                    OpenPetSettingsFromMenu: openPetSettingsMock,
                    QuitApp: quitAppMock,
                    OnFloatingButtonClicked: clickedMock,
                    OnFloatingButtonDragged: draggedMock,
                },
            },
        };
    });

    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        delete window.go;
    });

    it('opens pet settings from the right-click menu', async () => {
        const { container } = render(<FloatingButton />);

        fireEvent.contextMenu(container.firstElementChild as Element, { clientX: 20, clientY: 20 });
        fireEvent.click(await screen.findByRole('menuitem', { name: '\u8bbe\u7f6e' }));

        expect(openPetSettingsMock).toHaveBeenCalledTimes(1);
        await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
    });

    it('uses config events instead of periodic polling when runtime events are available', async () => {
        vi.useFakeTimers();

        render(<FloatingButton />);
        await vi.runOnlyPendingTimersAsync();
        loadConfigMock.mockClear();

        await vi.advanceTimersByTimeAsync(120000);

        expect(loadConfigMock).not.toHaveBeenCalled();
    });

    it('falls back to slow polling when runtime config events are unavailable', async () => {
        vi.useFakeTimers();
        vi.mocked(EventsOn).mockImplementationOnce(() => { throw new Error('runtime unavailable'); });
        vi.mocked(EventsOn).mockImplementationOnce(() => { throw new Error('runtime unavailable'); });

        render(<FloatingButton />);
        await vi.runOnlyPendingTimersAsync();
        loadConfigMock.mockClear();

        await vi.advanceTimersByTimeAsync(59999);
        expect(loadConfigMock).not.toHaveBeenCalled();

        await vi.advanceTimersByTimeAsync(1);
        expect(loadConfigMock).toHaveBeenCalledTimes(1);
    });

    it('patches motion sound from context menu without full config save', async () => {
        const { container } = render(<FloatingButton />);

        fireEvent.contextMenu(container.firstElementChild as Element, { clientX: 20, clientY: 20 });
        fireEvent.click(await screen.findByRole('menuitemcheckbox'));

        await waitFor(() => {
            expect(patchConfigFieldsMock).toHaveBeenCalledWith({ pet_motion_sound_enabled: false });
        });
        expect(patchConfigFieldsMock).toHaveBeenCalledTimes(1);
    });
});
