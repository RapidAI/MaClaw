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
      },
    },
  };
}

describe('MobileDocumentsPanel cloud drive folders', () => {
  it('keeps Documents, Audio, Video, and Other, and nests documents by type', async () => {
    installLibrary([
      { id: 'pdf', title: '规格', source_filename: 'spec.pdf' },
      { id: 'doc', title: '说明', source_filename: 'note.docx' },
      { id: 'song', title: '录音', type: 'audio' },
      { id: 'clip', title: '片头', source_filename: 'intro.mp4' },
      { id: 'pic', title: '封面', source_filename: 'cover.png' },
    ]);

    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();
    expect(screen.getByTestId('cloud-folder-documents').textContent).toContain('文档');
    expect(screen.getByTestId('cloud-folder-audio').textContent).toContain('音频');
    expect(screen.getByTestId('cloud-folder-video').textContent).toContain('视频');
    expect(screen.getByTestId('cloud-folder-other').textContent).toContain('其它');
    expect(screen.getByTestId('cloud-folder-documents:pdf').textContent).toContain('PDF');
    expect(screen.getByTestId('cloud-folder-documents:word').textContent).toContain('Word');
    expect(screen.getByText('规格')).toBeTruthy();
    expect(screen.getByText('说明')).toBeTruthy();
    expect(screen.getByText('录音')).toBeTruthy();
    expect(screen.getByText('片头')).toBeTruthy();
    expect(screen.getByText('封面')).toBeTruthy();
    expect(screen.getByText('5 个文件')).toBeTruthy();
  });

  it('still shows the four folders when the drive is empty', async () => {
    installLibrary([]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    expect(await screen.findByTestId('cloud-folder-documents')).toBeTruthy();
    expect(screen.getByTestId('cloud-folder-audio')).toBeTruthy();
    expect(screen.getByTestId('cloud-folder-video')).toBeTruthy();
    expect(screen.getByTestId('cloud-folder-other')).toBeTruthy();
    expect(screen.getByText('此文件夹为空')).toBeTruthy();
    expect(screen.getByText('0 个文件')).toBeTruthy();
  });

  it('collapses a folder without removing it', async () => {
    installLibrary([{ id: 'pdf', title: '规格', source_filename: 'spec.pdf' }]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    expect(await screen.findByText('规格')).toBeTruthy();
    fireEvent.click(screen.getByTestId('cloud-folder-documents'));
    expect(screen.queryByText('规格')).toBeNull();
    expect(screen.getByTestId('cloud-folder-documents')).toBeTruthy();
  });

  it('collapses a matching folder while a search is active', async () => {
    installLibrary([
      { id: 'pdf', title: '规格', source_filename: 'spec.pdf' },
      { id: 'doc', title: '说明', source_filename: 'note.docx' },
    ]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    expect(await screen.findByText('规格')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('搜索文件…'), { target: { value: '规格' } });
    expect(screen.queryByText('说明')).toBeNull();
    fireEvent.click(screen.getByTestId('cloud-folder-documents'));
    expect(screen.queryByText('规格')).toBeNull();
    expect(screen.getByTestId('cloud-folder-documents')).toBeTruthy();
  });

  it('finds a spreadsheet from its type name', async () => {
    installLibrary([
      { id: 'pdf', title: '规格', source_filename: 'spec.pdf' },
      { id: 'sheet', title: '预算', source_filename: '预算.xlsx' },
    ]);
    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    expect(await screen.findByText('预算')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('搜索文件…'), { target: { value: '表格' } });
    expect(screen.getByText('预算')).toBeTruthy();
    expect(screen.queryByText('规格')).toBeNull();
    expect(screen.queryByTestId('cloud-folder-audio')).toBeNull();
    expect(screen.queryByTestId('cloud-folder-video')).toBeNull();
    expect(screen.queryByTestId('cloud-folder-other')).toBeNull();
  });

  it('keeps the later file when an earlier detail request finishes last', async () => {
    let resolveFirst: (value: unknown) => void = () => undefined;
    let resolveSecond: (value: unknown) => void = () => undefined;
    installLibrary([
      { id: 'pdf', title: '规格', source_filename: 'spec.pdf' },
      { id: 'doc', title: '说明', source_filename: 'note.docx' },
    ]);
    const app = (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go.main.App;
    app.GetMobileLibraryItem = (id: string) => new Promise((resolve) => {
      if (id === 'pdf') resolveFirst = resolve;
      else resolveSecond = resolve;
    });

    render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, { wrapper: DialogProvider });

    fireEvent.click(await screen.findByText('规格'));
    fireEvent.click(screen.getByText('说明'));
    resolveFirst({ id: 'pdf', title: '规格全文', source_filename: 'spec.pdf', markdown: 'pdf' });
    resolveSecond({ id: 'doc', title: '说明全文', source_filename: 'note.docx', markdown: 'doc' });

    expect(await screen.findByText('说明全文')).toBeTruthy();
    expect(screen.queryByText('规格全文')).toBeNull();
  });
});
