// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { RemoteSessionList } from '../RemoteSessionList';

vi.mock('../../layout/backgroundTaskCount', () => ({ countActiveBackgroundLoops: () => 0 }));
vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListBackgroundLoops: vi.fn().mockResolvedValue([]),
    StopBackgroundLoop: vi.fn(),
    StopAllBackgroundLoops: vi.fn(),
    StopAllBackgroundTasks: vi.fn(),
    DismissRemoteSession: vi.fn(),
    ContinueBackgroundLoop: vi.fn(),
    GetBackgroundLoopOutput: vi.fn().mockResolvedValue([]),
}));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: vi.fn(), EventsOff: vi.fn() }));

describe('RemoteSessionList initial tab', () => {
    it('opens the background task tab when requested by another surface', () => {
        const props = {
            lang: "zh-Hans",
            remoteSessions: [],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            interruptRemoteSession: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            translate: (key: string) => key,
            formatText: (key: string) => key,
            localizeText: (en: string, zhHans: string) => zhHans || en,
        };
        render(
            <RemoteSessionList
                initialSessionTab="background"
                {...props}
            />,
        );

        expect(screen.getByRole('button', { name: '后台' }).style.fontWeight).toBe('700');
    });
});
