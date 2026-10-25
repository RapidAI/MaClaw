// @vitest-environment jsdom
// The cloud-drive list shows the icon the desktop shell associates with a file
// type. These tests pin the row wiring: the host icon replaces the drawn glyph
// once it arrives, a type the host cannot answer for keeps the drawn glyph, and
// one lookup serves every row of that type.
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from '../../CustomDialog';
import { MobileDocumentsPanel } from '../MobileDocumentsPanel';
import { peekCloudDriveSystemIcon, resetCloudDriveSystemIconCacheForTests } from '../cloudDriveSystemIcons';

vi.mock('../../../../wailsjs/runtime', () => ({
  EventsOn: () => () => undefined,
  EventsOff: () => undefined,
}));

function installLibrary(items: unknown[], iconFor: (name: string) => Promise<string> | string) {
  const iconCalls: string[] = [];
  (window as unknown as { go: unknown }).go = {
    main: {
      App: {
        ListMobileLibraryItems: async () => items,
        GetMobileDocumentQuota: async () => null,
        AIAssistantFileTypeIconDataURLIfAssociated: async (name: string) => {
          iconCalls.push(name);
          return iconFor(name);
        },
      },
    },
  };
  return iconCalls;
}

const rows = [
  { id: 'pdf', title: '规格', source_filename: 'spec.pdf' },
  { id: 'doc', title: '说明', source_filename: 'note.docx' },
];

describe('cloud drive row icons', () => {
  afterEach(() => {
    resetCloudDriveSystemIconCacheForTests();
    delete (window as unknown as { go?: unknown }).go;
  });

  it('replaces the drawn glyph with the host icon for that file type', async () => {
    const iconCalls = installLibrary(
      [...rows, { id: 'pdf2', title: '附录', source_filename: 'appendix.PDF' }],
      () => 'data:image/png;base64,PDFICON',
    );
    const { container } = render(
      <MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />,
      { wrapper: DialogProvider },
    );

    expect(await screen.findByText('规格')).toBeTruthy();
    const row = container.querySelector('[data-library-id="pdf"]') as HTMLElement;
    const img = await waitFor(() => {
      const found = row.querySelector('img.mdoc-file-glyph-img') as HTMLImageElement | null;
      expect(found).toBeTruthy();
      return found as HTMLImageElement;
    });
    expect(img.getAttribute('src')).toBe('data:image/png;base64,PDFICON');
    expect(img.getAttribute('alt')).toBe('');
    // The system badge keeps the row's type as data-kind but drops the tint.
    const badge = img.closest('.mdoc-file-glyph') as HTMLElement;
    expect(badge.getAttribute('data-kind')).toBe('pdf');
    expect(badge.className).toContain('mdoc-file-glyph--system');
    // The drawn glyph is gone rather than stacked under the image.
    expect(badge.querySelector('svg')).toBeNull();
    // One association lookup per type: the second PDF row reuses the first.
    const second = container.querySelector('[data-library-id="pdf2"]') as HTMLElement;
    await waitFor(() => expect(second.querySelector('img.mdoc-file-glyph-img')).toBeTruthy());
    expect(iconCalls.filter((name) => name === 'file.pdf')).toHaveLength(1);
    expect(iconCalls).toContain('file.docx');
  });

  it('keeps the drawn glyph when the host has no icon for the type', async () => {
    installLibrary(rows, () => '');
    const { container } = render(
      <MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />,
      { wrapper: DialogProvider },
    );

    expect(await screen.findByText('规格')).toBeTruthy();
    const row = container.querySelector('[data-library-id="pdf"]') as HTMLElement;
    await waitFor(() => expect(row.querySelector('svg')).toBeTruthy());
    expect(row.querySelector('img')).toBeNull();
    const badge = row.querySelector('.mdoc-file-glyph') as HTMLElement;
    expect(badge.className).not.toContain('mdoc-file-glyph--system');
    expect(badge.getAttribute('data-kind')).toBe('pdf');
  });

  it('does not ask for an icon for a recording', async () => {
    const iconCalls = installLibrary(
      [{ id: 'rec', title: '周会', type: 'audio', audio: { content_type: 'audio/mp4', available: true } }],
      () => 'data:image/png;base64,VIDEOICON',
    );
    const { container } = render(
      <MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />,
      { wrapper: DialogProvider },
    );

    expect(await screen.findByText('周会')).toBeTruthy();
    const row = container.querySelector('[data-library-id="rec"]') as HTMLElement;
    await waitFor(() => expect(row.querySelector('svg')).toBeTruthy());
    expect(row.querySelector('.mdoc-file-glyph')?.getAttribute('data-kind')).toBe('audio');
    expect(iconCalls).toEqual([]);
  });

  it('never paints one type icon on a row that switched to another type', async () => {
    // Both suffixes stay in the Other folder, so the row keeps its component
    // instance across the change — the case where a stale URL would leak. The
    // .jpg lookup is held open, so an icon can only appear by keeping the .png
    // one.
    let releaseJpg: (url: string) => void = () => undefined;
    const iconCalls: string[] = [];
    (window as unknown as { go: unknown }).go = {
      main: {
        App: {
          ListMobileLibraryItems: async () => [{ id: 'row', title: '切换测试', source_filename: 'a.png' }],
          GetMobileDocumentQuota: async () => null,
          AIAssistantFileTypeIconDataURLIfAssociated: async (name: string) => {
            iconCalls.push(name);
            if (name === 'file.jpg') {
              return new Promise<string>((resolve) => { releaseJpg = resolve; });
            }
            return 'data:image/png;base64,PNGICON';
          },
        },
      },
    };
    const { container, rerender } = render(
      <MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />,
      { wrapper: DialogProvider },
    );

    expect(await screen.findByText('切换测试')).toBeTruthy();
    const row = container.querySelector('[data-library-id="row"]') as HTMLElement;
    await waitFor(() => expect(row.querySelector('img.mdoc-file-glyph-img')).toBeTruthy());

    // Swap the suffix without remounting the row.
    (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go.main.App.ListMobileLibraryItems =
      async () => [{ id: 'row', title: '切换测试', source_filename: 'a.jpg' }];
    rerender(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: '刷新' }));

    await waitFor(() => expect(iconCalls).toContain('file.jpg'));
    const swapped = container.querySelector('[data-library-id="row"]') as HTMLElement;
    // The .png icon must be gone while the .jpg lookup is still open.
    expect(swapped.querySelector('img.mdoc-file-glyph-img')).toBeNull();
    expect(swapped.querySelector('svg')).toBeTruthy();

    releaseJpg('data:image/png;base64,JPGICON');
    await waitFor(() => {
      expect(swapped.querySelector('img.mdoc-file-glyph-img')?.getAttribute('src')).toBe('data:image/png;base64,JPGICON');
    });
  });

  it('falls back to the drawn glyph when the WebView cannot decode the icon', async () => {
    installLibrary(rows, () => 'data:image/png;base64,NOT-A-REAL-PNG');
    const { container } = render(
      <MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />,
      { wrapper: DialogProvider },
    );

    expect(await screen.findByText('规格')).toBeTruthy();
    const row = container.querySelector('[data-library-id="pdf"]') as HTMLElement;
    const img = (await waitFor(() => {
      const found = row.querySelector('img.mdoc-file-glyph-img') as HTMLImageElement | null;
      expect(found).toBeTruthy();
      return found as HTMLImageElement;
    }))!;
    // jsdom does not decode images, so the error is delivered by hand. That is
    // the same event a corrupt payload raises in the WebView.
    fireEvent.error(img);
    await waitFor(() => expect(row.querySelector('svg')).toBeTruthy());
    expect(row.querySelector('img')).toBeNull();
    expect(row.querySelector('.mdoc-file-glyph')?.className).not.toContain('mdoc-file-glyph--system');
    // A corrupt answer must not be retried on every render.
    expect(peekCloudDriveSystemIcon('.pdf')).toBeNull();
  });
});
