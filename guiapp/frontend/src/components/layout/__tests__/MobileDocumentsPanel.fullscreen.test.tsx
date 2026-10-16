// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DialogProvider } from '../../CustomDialog';
import { MobileDocumentsPanel } from '../MobileDocumentsPanel';

vi.mock('../../../../wailsjs/runtime', () => ({
  EventsOn: () => () => undefined,
  EventsOff: () => undefined,
}));

function installLibrary(items: unknown[]) {
  (window as unknown as { go: unknown }).go = {
    main: {
      App: {
        ListMobileLibraryItems: async () => items,
        GetMobileDocumentQuota: async () => null,
        GetMobileLibraryItem: async (id: string) => items.find((item) => (item as { id: string }).id === id) || null,
      },
    },
  };
}

describe('MobileDocumentsPanel preview fullscreen', () => {
  it('toggles the preview host between the embedded slot and document.body', async () => {
    installLibrary([
      { id: 'pdf', title: '规格', source_filename: 'spec.pdf', markdown: '正文' },
    ]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText('规格'));
    expect(await screen.findByTestId('mobile-documents-preview')).toBeTruthy();
    const slot = screen.getByTestId('mobile-documents-preview-slot');
    // Embedded: the portal host lives inside the slot.
    expect(slot.querySelector('.mdoc-preview-slot-host')).toBeTruthy();
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '全屏' }));
    // Fullscreen: the same host element moved under document.body…
    const host = document.body.querySelector(':scope > .mdoc-preview-slot-host--fullscreen');
    expect(host).toBeTruthy();
    // Inline page fullscreen sits below modals (50000+) opened later.
    expect((host as HTMLElement).style.zIndex).toBe('49000');
    // …carrying the still-mounted preview pane with it (no remount).
    expect(host?.contains(screen.getByTestId('mobile-documents-preview'))).toBe(true);
    expect(screen.getByRole('button', { name: '恢复' })).toBeTruthy();

    // Esc restores the embedded pane first; it must not close the panel.
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();
    expect(screen.getByTestId('mobile-documents-preview')).toBeTruthy();
    expect(screen.getByTestId('mobile-documents-inline')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '全屏' }));
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host--fullscreen')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '恢复' }));
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();
    expect(screen.getByTestId('mobile-documents-preview')).toBeTruthy();
  });

  it('keeps the cloud drive open after Esc restores from fullscreen', async () => {
    installLibrary([
      { id: 'doc', title: '说明', source_filename: 'note.docx', markdown: '内容' },
    ]);
    const onClose = vi.fn();
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={onClose} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText('说明'));
    expect(await screen.findByTestId('mobile-documents-preview')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '全屏' }));
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('survives modal open toggles with a stable hook order', async () => {
    installLibrary([
      { id: 'doc', title: '说明', source_filename: 'note.docx', markdown: '内容' },
    ]);
    const onClose = vi.fn();
    const view = render(<MobileDocumentsPanel lang="zh-Hans" open onClose={onClose} />, { wrapper: DialogProvider });

    // Close (early return renders fewer JSX but must not unmount hooks)…
    view.rerender(<MobileDocumentsPanel lang="zh-Hans" open={false} onClose={onClose} />);
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();
    // …and reopen: the preview host must reattach and the pane keep working.
    view.rerender(<MobileDocumentsPanel lang="zh-Hans" open onClose={onClose} />);

    fireEvent.click(await screen.findByText('说明'));
    expect(await screen.findByTestId('mobile-documents-preview')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '全屏' }));
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host--fullscreen')).toBeTruthy();
    // Modal panel fullscreen must cover its own 50000 overlay.
    const modalHost = document.body.querySelector(':scope > .mdoc-preview-slot-host--fullscreen') as HTMLElement;
    expect(modalHost.style.zIndex).toBe('60000');
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();
    expect(screen.getByTestId('mobile-documents-panel')).toBeTruthy();
  });

  it('offers the fullscreen toggle for audio previews too', async () => {
    installLibrary([
      { id: 'song', title: '录音', type: 'audio', audio: { available: true } },
    ]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    // The audio row renders the title twice (title + meta); wait for the
    // async list load, then pick the first.
    const songRows = await screen.findAllByText('录音');
    fireEvent.click(songRows[0]);
    expect(await screen.findByTestId('mobile-documents-preview')).toBeTruthy();
    expect(screen.getByRole('button', { name: '全屏' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '全屏' }));
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host--fullscreen')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '恢复' }));
    expect(document.body.querySelector(':scope > .mdoc-preview-slot-host')).toBeNull();
  });

  it('restores focus after the host move blurs it', async () => {
    installLibrary([
      { id: 'doc', title: '说明', source_filename: 'note.docx', markdown: '内容' },
    ]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText('说明'));
    expect(await screen.findByTestId('mobile-documents-preview')).toBeTruthy();
    // Focus lives inside the movable host subtree (the toggle button itself).
    const toggle = screen.getByRole('button', { name: '全屏' }) as HTMLElement;
    toggle.focus();
    expect(document.activeElement).toBe(toggle);

    // The cleanup detaches the host before the next effect runs — the old
    // implementation recorded document.activeElement *after* that detach,
    // when focus had already reset to body, so restore never fired.
    fireEvent.click(toggle);
    // The toggle button itself moves with the host (same DOM node), so focus
    // must land back on it (label now reads 恢复).
    expect(document.activeElement).toBe(toggle);

    fireEvent.click(screen.getByRole('button', { name: '恢复' }));
    expect(document.activeElement).toBe(toggle);
  });
});
