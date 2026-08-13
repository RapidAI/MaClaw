// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetHubUserInvitationsPage: vi.fn(),
  RotateHubUserInvitation: vi.fn(),
}));

import { GetHubUserInvitationsPage, RotateHubUserInvitation } from '../../../wailsjs/go/main/App';
import { HubInvitationDialog } from '../HubInvitationDialog';

type InvitationPage = Awaited<ReturnType<typeof GetHubUserInvitationsPage>>;

const invitationPage = (page = 1) => ({
  enabled: true,
  invite_url: 'https://hub.example/invite/demo',
  inviter_credits: 20,
  invitee_credits: 10,
  duration_days: 30,
  invitees: [{ user_id: `invitee-${page}`, contact: `user${page}@example.com`, registered_at: '2026-08-13T01:00:00Z' }],
  total: 21,
  page,
} as InvitationPage);

beforeEach(() => {
  vi.mocked(GetHubUserInvitationsPage).mockReset();
  vi.mocked(RotateHubUserInvitation).mockReset();
  vi.mocked(GetHubUserInvitationsPage).mockResolvedValue(invitationPage());
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('HubInvitationDialog', () => {
  it('keeps focus inside the dialog and restores it to its trigger on close', async () => {
    const onClose = vi.fn();
    const { rerender } = render(<><button type="button">Open invitation dialog</button><HubInvitationDialog open={false} onClose={onClose} lang="en" /></>);

    const trigger = screen.getByRole('button', { name: 'Open invitation dialog' });
    trigger.focus();
    rerender(<><button type="button">Open invitation dialog</button><HubInvitationDialog open onClose={onClose} lang="en" /></>);

    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' })));
    const close = screen.getByRole('button', { name: 'Close' });
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Next' }));

    rerender(<><button type="button">Open invitation dialog</button><HubInvitationDialog open={false} onClose={onClose} lang="en" /></>);
    expect(document.activeElement?.textContent).toBe('Open invitation dialog');
    expect(close).toBeTruthy();
  });

  it('does not let an earlier page response replace a newer page', async () => {
    let resolveFirst: (value: ReturnType<typeof invitationPage>) => void = () => {};
    let resolveSecond: (value: ReturnType<typeof invitationPage>) => void = () => {};
    vi.mocked(GetHubUserInvitationsPage)
      .mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve; }))
      .mockReturnValueOnce(new Promise((resolve) => { resolveSecond = resolve; }));

    render(<HubInvitationDialog open onClose={vi.fn()} lang="en" />);
    await act(async () => { resolveFirst(invitationPage(1)); });
    await waitFor(() => expect(screen.getByText('user1@example.com')).toBeTruthy());

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled')).toBe(true);
    await act(async () => { resolveSecond(invitationPage(2)); });
    await waitFor(() => expect(screen.getByText('user2@example.com')).toBeTruthy());
    expect(screen.queryByText('user1@example.com')).toBeNull();
  });

  it('leaves the current page visible when a later page request fails', async () => {
    vi.mocked(GetHubUserInvitationsPage)
      .mockResolvedValueOnce(invitationPage(1))
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(invitationPage(2));

    render(<HubInvitationDialog open onClose={vi.fn()} lang="en" />);
    await waitFor(() => expect(screen.getByText('user1@example.com')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy());
    expect(screen.getByText('user1@example.com')).toBeTruthy();
    expect(screen.getByText('Page 1 / 2')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(screen.getByText('user2@example.com')).toBeTruthy());
    expect(vi.mocked(GetHubUserInvitationsPage).mock.calls.map(([requestedPage]) => requestedPage)).toEqual([1, 2, 2]);
  });

  it('offers a retry when the initial invitation request fails', async () => {
    vi.mocked(GetHubUserInvitationsPage)
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(invitationPage(1));

    render(<HubInvitationDialog open onClose={vi.fn()} lang="en" />);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

    await waitFor(() => expect(screen.getByText('user1@example.com')).toBeTruthy());
  });

  it('does not retain an old invitation link when reopening fails', async () => {
    vi.mocked(GetHubUserInvitationsPage)
      .mockResolvedValueOnce(invitationPage(1))
      .mockRejectedValueOnce(new Error('network unavailable'));
    const onClose = vi.fn();
    const { rerender } = render(<HubInvitationDialog open onClose={onClose} lang="en" />);

    await waitFor(() => expect(screen.getByDisplayValue('https://hub.example/invite/demo')).toBeTruthy());
    rerender(<HubInvitationDialog open={false} onClose={onClose} lang="en" />);
    rerender(<HubInvitationDialog open onClose={onClose} lang="en" />);

    await waitFor(() => expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy());
    expect(screen.queryByDisplayValue('https://hub.example/invite/demo')).toBeNull();
  });

  it('keeps the invitation visible when link rotation fails', async () => {
    vi.mocked(RotateHubUserInvitation).mockResolvedValueOnce({
      enabled: true,
      error: 'rotation failed',
    } as InvitationPage);
    vi.spyOn(window, 'confirm').mockReturnValueOnce(true);

    render(<HubInvitationDialog open onClose={vi.fn()} lang="en" />);
    await waitFor(() => expect(screen.getByDisplayValue('https://hub.example/invite/demo')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));

    await waitFor(() => expect(screen.getByText('Could not replace the invitation link. Your current link is still active.')).toBeTruthy());
    expect(screen.getByDisplayValue('https://hub.example/invite/demo')).toBeTruthy();
  });

  it('removes a link that becomes unavailable while it is open', async () => {
    vi.mocked(RotateHubUserInvitation).mockResolvedValueOnce({
      enabled: false,
      error: 'invitations disabled',
    } as InvitationPage);
    vi.spyOn(window, 'confirm').mockReturnValueOnce(true);

    render(<HubInvitationDialog open onClose={vi.fn()} lang="en" />);
    await waitFor(() => expect(screen.getByDisplayValue('https://hub.example/invite/demo')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));

    await waitFor(() => expect(screen.getByText('Invitations are unavailable.')).toBeTruthy());
    expect(screen.queryByDisplayValue('https://hub.example/invite/demo')).toBeNull();
  });

  it('does not apply a rotation response after the dialog has closed', async () => {
    let resolveRotate: (value: InvitationPage) => void = () => {};
    vi.mocked(RotateHubUserInvitation).mockReturnValueOnce(new Promise((resolve) => { resolveRotate = resolve; }));
    vi.spyOn(window, 'confirm').mockReturnValueOnce(true);
    const onClose = vi.fn();
    const { rerender } = render(<HubInvitationDialog open onClose={onClose} lang="en" />);

    await waitFor(() => expect(screen.getByRole('button', { name: 'Rotate' })).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));
    rerender(<HubInvitationDialog open={false} onClose={onClose} lang="en" />);
    await act(async () => { resolveRotate({ ...invitationPage(), invite_url: 'https://hub.example/invite/new' } as InvitationPage); });

    expect(screen.queryByDisplayValue('https://hub.example/invite/new')).toBeNull();
  });
});
