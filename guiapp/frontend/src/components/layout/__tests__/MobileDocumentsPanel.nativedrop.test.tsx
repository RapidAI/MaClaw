// @vitest-environment jsdom
// Tests for the Wails native file-drop channel of the cloud drive panel:
// publishing by absolute path, cross-channel de-dup (HTML5 + native), and
// re-arming the de-dup after a fully failed import so an immediate retry
// of the same drop is not silently swallowed.
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from '../../CustomDialog';
import { MobileDocumentsPanel } from '../MobileDocumentsPanel';

vi.mock('../../../../wailsjs/runtime', () => ({
  EventsOn: () => () => undefined,
  EventsOff: () => undefined,
}));

type DropCb = (x: number, y: number, paths: string[]) => void;

function installRuntime() {
  let cb: DropCb | null = null;
  const onFileDropOff = vi.fn();
  (window as unknown as { runtime: unknown }).runtime = {
    // The real Wails runtime wraps the callback with an elementFromPoint
    // drop-target filter before registering it; the unit test calls the raw
    // callback directly, which models "a drop that passed the filter".
    OnFileDrop: (fn: DropCb) => {
      cb = fn;
    },
    OnFileDropOff: onFileDropOff,
  };
  return {
    fire: (paths: string[]) => cb?.(100, 100, paths),
    onFileDropOff,
  };
}

function installApp(initial: {
  items?: unknown[];
  failImport?: boolean;
  importResult?: unknown | ((path: string) => unknown);
}) {
  const importCalls: string[] = [];
  const bytesCalls: string[] = [];
  let failImport = !!initial.failImport;
  (window as unknown as { go: unknown }).go = {
    main: {
      App: {
        ListMobileLibraryItems: async () => initial.items ?? [],
        GetMobileDocumentQuota: async () => null,
        GetMobileLibraryItem: async (id: string) => ({ id, title: '文件', source_filename: 'file' }),
        ImportMobileDocumentFromPath: async (path: string) => {
          importCalls.push(path);
          if (failImport) throw new Error('import failed');
          const custom = typeof initial.importResult === 'function'
            ? initial.importResult(path)
            : initial.importResult;
          if (custom) return custom;
          return { id: `draft-${importCalls.length}`, title: '文件', source_filename: path.split(/[\\/]/).pop() };
        },
        ImportMobileDocumentBytes: async (name: string, _b64: string) => {
          bytesCalls.push(name);
          if (failImport) throw new Error('import failed');
          return { id: `draft-b${bytesCalls.length}`, title: '文件', source_filename: name };
        },
      },
    },
  };
  return {
    importCalls,
    bytesCalls,
    setFailImport: (v: boolean) => {
      failImport = v;
    },
  };
}

function renderPanel() {
  return render(<MobileDocumentsPanel lang="zh-Hans" open inline onClose={() => undefined} />, {
    wrapper: DialogProvider,
  });
}

afterEach(() => {
  delete (window as unknown as Record<string, unknown>).runtime;
  delete (window as unknown as Record<string, unknown>).go;
});

describe('MobileDocumentsPanel native file-drop channel', () => {
  it('imports a natively dropped file by path, then dedups the same drop', async () => {
    const rt = installRuntime();
    const app = installApp({});
    renderPanel();
    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();

    rt.fire(['C:\\下载\\季度报告.pdf']);
    await waitFor(() => expect(app.importCalls).toEqual(['C:\\下载\\季度报告.pdf']));
    expect(await screen.findByText(/已添加 1 个文件/)).toBeTruthy();

    // Same physical drop arriving again through the other channel / instance
    // within the dedup window must not import a second time.
    rt.fire(['C:\\下载\\季度报告.pdf']);
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(app.importCalls).toEqual(['C:\\下载\\季度报告.pdf']);
  });

  it('publishes an HTML5 drop only once even when the native channel fires too', async () => {
    const rt = installRuntime();
    const app = installApp({});
    renderPanel();
    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();

    const file = new File(['hello'], '发布会纪要.docx', { type: 'application/msword' });
    fireEvent.drop(screen.getByTestId('mobile-documents-drop-zone'), {
      dataTransfer: { files: [file] },
    });
    await waitFor(() => expect(app.bytesCalls).toEqual(['发布会纪要.docx']));
    expect(app.importCalls).toEqual([]);

    // The native channel reports the same physical drop (matching basenames):
    // the shared signature dedup must suppress the second import.
    rt.fire(['C:\\Users\\me\\Downloads\\发布会纪要.docx']);
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(app.importCalls).toEqual([]);
    expect(app.bytesCalls).toEqual(['发布会纪要.docx']);
  });

  it('re-arms the dedup after a fully failed import so a retry works', async () => {
    const rt = installRuntime();
    const app = installApp({ failImport: true });
    renderPanel();
    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();

    rt.fire(['D:\\资料\\数据表.xlsx']);
    await waitFor(() => expect(app.importCalls).toEqual(['D:\\资料\\数据表.xlsx']));
    expect(await screen.findByText('import failed')).toBeTruthy();

    // Immediate retry of the same drop: the failed run must have reset the
    // dedup signature so this second attempt goes through.
    app.setFailImport(false);
    rt.fire(['D:\\资料\\数据表.xlsx']);
    await waitFor(() => expect(app.importCalls).toHaveLength(2));
    expect(await screen.findByText(/已添加 1 个文件/)).toBeTruthy();
  });

  it('skips a duplicate upload and names the file already in the cloud drive', async () => {
    const rt = installRuntime();
    installApp({
      items: [{ id: 'existing-1', title: '已有文件', source_filename: '已有文件.pdf' }],
      importResult: {
        id: 'existing-1',
        title: '已有文件',
        duplicate: true,
        duplicate_of_title: '已有文件',
      },
    });
    renderPanel();
    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();

    rt.fire(['C:\\下载\\新文件.pdf']);
    expect(await screen.findByText('「新文件.pdf」与云盘中的「已有文件」内容相同，已跳过上传，避免重复占用空间。')).toBeTruthy();
    expect(screen.queryByText(/已添加/)).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('reports a new file and a skipped duplicate together', async () => {
    const rt = installRuntime();
    installApp({
      items: [{ id: 'existing-1', title: '已有文件', source_filename: '已有文件.pdf' }],
      importResult: (path: string) => (
        path.endsWith('重复.pdf')
          ? { id: 'existing-1', title: '已有文件', duplicate: true, duplicate_of_title: '已有文件' }
          : { id: 'new-1', title: '季度报告', source_filename: '季度报告.pdf' }
      ),
    });
    renderPanel();
    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();

    rt.fire(['C:\\下载\\季度报告.pdf', 'C:\\下载\\重复.pdf']);
    const banner = await screen.findByText(/已添加 1 个文件/);
    expect(banner.textContent).toContain('「重复.pdf」与云盘中的「已有文件」内容相同，已跳过上传。');
    expect(banner.textContent).not.toContain('避免重复占用空间');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('registers the native channel while open and releases it on close', async () => {
    const rt = installRuntime();
    installApp({});
    const view = renderPanel();
    expect(await screen.findByRole('region', { name: '云盘' })).toBeTruthy();

    view.unmount();
    await waitFor(() => expect(rt.onFileDropOff).toHaveBeenCalled());
  });
});
