// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DialogProvider } from '../../CustomDialog';
import { MobileDocumentsPanel } from '../MobileDocumentsPanel';

vi.mock('../../../../wailsjs/runtime', () => ({
  EventsOn: () => () => undefined,
  EventsOff: () => undefined,
}));

function installLibrary(items: unknown[], extra: Record<string, unknown> = {}) {
  (window as unknown as { go: unknown }).go = {
    main: {
      App: {
        ListMobileLibraryItems: async () => items,
        GetMobileDocumentQuota: async () => null,
        GetMobileLibraryItem: async (id: string) => items.find((item) => (item as { id: string }).id === id) || null,
        ...extra,
      },
    },
  };
}

const shellCss = readFileSync(join(process.cwd(), 'src/styles/partials/110-mc-app-shell.css'), 'utf8');

describe('MobileDocumentsPanel preview header', () => {
  it('puts file actions on their own row under the title', async () => {
    const title = '2609.21686_cipl_a_channel_aware_framework_for_recov';
    installLibrary([
      {
        id: 'pdf',
        title,
        source_filename: `${title}.pdf`,
        source_size: 980992,
        has_original: true,
        markdown: 'body',
      },
    ]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText(title));
    const header = document.querySelector('.mdoc-preview-header') as HTMLElement;
    const heading = header.querySelector('.mdoc-preview-heading');
    const actions = header.querySelector('.mdoc-preview-actions');
    expect(heading).toBeTruthy();
    expect(actions).toBeTruthy();
    expect(heading?.nextElementSibling).toBe(actions);
    expect(heading?.textContent).toContain(title);
    expect(heading?.textContent).toContain('原件');
    expect(actions?.textContent).toContain('复制');
    expect(actions?.textContent).toContain('打开原件');
    expect(actions?.textContent).toContain('保存原件');
    expect(actions?.textContent).toContain('已共享到手机');
    expect(actions?.querySelector('.mdoc-preview-title')).toBeNull();
    const share = screen.getByRole('button', { name: '已共享到手机' });
    expect(share.className).toContain('mdoc-toolbar-btn--accent');
    expect(share.className.includes('primary')).toBe(false);
    expect(screen.getByRole('button', { name: '全屏' })).toBeTruthy();
    const companion = screen.getByRole('button', { name: '伴读' });
    expect(companion.querySelector('svg')).toBeTruthy();
    expect(companion.textContent).toContain('伴读');
    expect(companion.className.includes('primary')).toBe(false);
    const fullscreen = screen.getByRole('button', { name: '全屏' });
    expect(fullscreen.compareDocumentPosition(companion) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('opens a supported original in the companion and hides the button for other types', async () => {
    let releaseCompanion: (path: string) => void = () => undefined;
    const openCompanion = vi.fn(() => new Promise<string>((resolve) => {
      releaseCompanion = resolve;
    }));
    installLibrary([
      {
        id: 'pdf',
        title: 'paper',
        source_filename: 'paper.pdf',
        has_original: true,
        markdown: 'extract',
      },
      {
        id: 'src',
        title: 'app.go',
        source_filename: 'app.go',
        has_original: true,
        markdown: 'package main',
      },
      {
        id: 'note',
        title: '会议纪要',
        markdown: '# 纪要',
      },
    ], { OpenMobileDocumentInFileCompanion: openCompanion });
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText('paper'));
    fireEvent.click(await screen.findByTestId('cloud-drive-companion'));
    expect(await screen.findByText('正在用伴读打开…')).toBeTruthy();
    expect(screen.getByRole('button', { name: '选择文件' }).textContent).toBe('选择文件');
    expect((screen.getByTestId('cloud-drive-companion') as HTMLButtonElement).disabled).toBe(true);
    releaseCompanion('C:/temp/paper.pdf');
    await waitFor(() => {
      expect(openCompanion).toHaveBeenCalledWith('pdf');
    });
    expect((await screen.findByText(/已用伴读打开/)).textContent).toBe('已用伴读打开');

    fireEvent.click(screen.getByText('app.go'));
    await screen.findByRole('button', { name: '打开原件' });
    expect(screen.queryByTestId('cloud-drive-companion')).toBeNull();

    fireEvent.click(screen.getByText('会议纪要'));
    fireEvent.click(await screen.findByTestId('cloud-drive-companion'));
    await waitFor(() => {
      expect(openCompanion).toHaveBeenLastCalledWith('note');
    });
  });

  it('drops the companion status when the user switches documents while it opens', async () => {
    let releaseCompanion: (path: string) => void = () => undefined;
    const openCompanion = vi.fn(() => new Promise<string>((resolve) => {
      releaseCompanion = resolve;
    }));
    installLibrary([
      {
        id: 'pdf',
        title: 'paper',
        source_filename: 'paper.pdf',
        has_original: true,
        markdown: 'extract',
      },
      {
        id: 'note',
        title: '会议纪要',
        markdown: '# 纪要',
      },
    ], { OpenMobileDocumentInFileCompanion: openCompanion });
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText('paper'));
    fireEvent.click(await screen.findByTestId('cloud-drive-companion'));
    expect(await screen.findByText('正在用伴读打开…')).toBeTruthy();
    fireEvent.click(screen.getByText('会议纪要'));
    releaseCompanion('C:/temp/paper.pdf');
    await waitFor(() => {
      expect(openCompanion).toHaveBeenCalledWith('pdf');
    });
    await waitFor(() => {
      expect(screen.queryByText('正在用伴读打开…')).toBeNull();
    });
    expect(screen.queryByText('已用伴读打开')).toBeNull();
  });

  it('keeps the preview toolbar from collapsing into the title row', () => {
    const header = shellCss.slice(shellCss.indexOf('.mdoc-preview-header {'));
    expect(header.startsWith('.mdoc-preview-header {')).toBe(true);
    expect(header.slice(0, 280)).toContain('flex-direction: column');
    expect(shellCss).toContain('.mdoc-preview-actions {\n  display: flex;\n  flex-wrap: wrap;');
    expect(shellCss).toContain('.mdoc-toolbar-group {\n  display: inline-flex;\n  flex-wrap: wrap;');
    expect(shellCss).toContain('.mdoc-toolbar-btn--accent {');
    expect(shellCss).not.toContain('mdoc-toolbar-btn--primary');
    expect(shellCss).toContain('.mdoc-toolbar-btn:not(:has(span)):has(svg)');
    expect(shellCss).not.toContain('.mdoc-toolbar-btn:not(:has(span)) {');
    const responsive = readFileSync(join(process.cwd(), 'src/styles/partials/120-app-responsive.css'), 'utf8');
    expect(responsive).not.toContain('.mobile-documents-preview-header > .mobile-documents-btn');
    expect(responsive).toContain('.mdoc-preview-actions .mdoc-toolbar-btn { height: 38px;');
  });
});
