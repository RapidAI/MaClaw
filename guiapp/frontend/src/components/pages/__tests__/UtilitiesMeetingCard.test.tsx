// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

describe('UtilitiesPage meeting card', () => {
    it('shows survey and meeting cards on the home grid', () => {
        render(<UtilitiesPage lang="zh-Hans" />);
        expect(screen.getByTestId('utilities-page')).toBeTruthy();
        expect(screen.getByTestId('utilities-survey-card')).toBeTruthy();
        const meetingCard = screen.getByTestId('utilities-meeting-card') as HTMLButtonElement;
        expect(meetingCard).toBeTruthy();
        expect(meetingCard.disabled).toBe(true); // no handler in isolation
        expect(screen.getByText('会议记录')).toBeTruthy();
    });

    it('invokes onStartMeetingRecord on click', async () => {
        const onStartMeetingRecord = vi.fn().mockResolvedValue(undefined);
        render(<UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />);
        fireEvent.click(screen.getByTestId('utilities-meeting-card'));
        await waitFor(() => expect(onStartMeetingRecord).toHaveBeenCalledTimes(1));
    });

    it('ignores double-clicks while starting', async () => {
        let resolveStart: (() => void) | undefined;
        const onStartMeetingRecord = vi.fn(
            () => new Promise<void>((resolve) => { resolveStart = resolve; }),
        );
        render(<UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />);
        const card = screen.getByTestId('utilities-meeting-card') as HTMLButtonElement;
        fireEvent.click(card);
        fireEvent.click(card);
        fireEvent.click(card);
        expect(onStartMeetingRecord).toHaveBeenCalledTimes(1);
        expect(card.disabled).toBe(true);
        expect(card.getAttribute('aria-busy')).toBe('true');
        expect(screen.getByText('启动中…')).toBeTruthy();
        resolveStart?.();
        await waitFor(() => expect(card.disabled).toBe(false));
        expect(card.getAttribute('aria-busy')).toBeNull();
        expect(screen.getByText('开始')).toBeTruthy();
    });

    it('re-enables after failure', async () => {
        const onStartMeetingRecord = vi.fn().mockRejectedValue(new Error('create failed'));
        render(<UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />);
        const card = screen.getByTestId('utilities-meeting-card') as HTMLButtonElement;
        fireEvent.click(card);
        await waitFor(() => expect(onStartMeetingRecord).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(card.disabled).toBe(false));
    });

    it('skips setState after unmount (navigate-away mid-start)', async () => {
        let resolveStart: (() => void) | undefined;
        const onStartMeetingRecord = vi.fn(
            () => new Promise<void>((resolve) => { resolveStart = resolve; }),
        );
        const { unmount } = render(
            <UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />,
        );
        fireEvent.click(screen.getByTestId('utilities-meeting-card'));
        expect(onStartMeetingRecord).toHaveBeenCalledTimes(1);
        unmount();
        resolveStart?.();
        await Promise.resolve();
    });
});
