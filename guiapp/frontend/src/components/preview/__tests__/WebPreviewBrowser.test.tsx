import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { lightCodePreviewTheme } from '../../ai/CodePreviewPanel';
import { WebPreviewBrowser } from '../WebPreviewBrowser';
import {
    classifyWebPreviewAddress,
    joinPreviewDisplay,
    localPreviewURL,
    decodePreviewPath,
    webPreviewSandbox,
    agentWebPreviewRequestKey,
    webPreviewTargetFromFile,
} from '../webPreviewAddress';
import { BrowserOpenURL } from '../../../../wailsjs/runtime';
import { CloseWebPreview, OpenWebPreview } from '../../../../wailsjs/go/main/App';

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    OpenWebPreview: vi.fn(async (absPath: string) => ({
        id: 'abc',
        origin: 'http://127.0.0.1:9',
        token: 'abc',
        root: String(absPath).replace(/[\\/][^\\/]+$/, ''),
        entry_url: 'http://127.0.0.1:9/web-preview/v1/abc/index.html',
        entry_path: 'index.html',
    })),
    CloseWebPreview: vi.fn(async () => undefined),
}));

describe('webPreviewAddress', () => {
    it('accepts http(s), bare hosts, local files, and relative paths', () => {
        expect(classifyWebPreviewAddress('https://example.com/a')).toEqual({ kind: 'remote', url: 'https://example.com/a' });
        expect(classifyWebPreviewAddress('example.com/docs')).toEqual({ kind: 'remote', url: 'https://example.com/docs' });
        expect(classifyWebPreviewAddress('javascript:alert(1)')).toEqual({ kind: 'invalid' });
        expect(classifyWebPreviewAddress('file:///C:/secret.html')).toEqual({ kind: 'invalid' });
        expect(classifyWebPreviewAddress('D:/site/index.html')).toEqual({ kind: 'file', path: 'D:/site/index.html' });
        expect(classifyWebPreviewAddress('../secret.html')).toEqual({ kind: 'invalid' });
        expect(classifyWebPreviewAddress('css/app.css')).toEqual({ kind: 'relative', path: 'css/app.css' });
    });

    it('selects web preview for HTML, SVG, and http(s) URLs only', () => {
        expect(webPreviewTargetFromFile({ fileName: 'index.html', absPath: 'D:/site/index.html' })).toEqual({
            kind: 'file', format: 'html', path: 'D:/site/index.html',
        });
        expect(webPreviewTargetFromFile({ fileName: 'icon.svg', absPath: 'D:/site/icon.svg', language: 'image' })).toEqual({
            kind: 'file', format: 'svg', path: 'D:/site/icon.svg',
        });
        expect(webPreviewTargetFromFile({ fileName: 'page.xhtml', filePath: '/tmp/page.xhtml' })).toEqual({
            kind: 'file', format: 'html', path: '/tmp/page.xhtml',
        });
        expect(webPreviewTargetFromFile({ fileName: 'docs', filePath: 'https://example.com/docs' })).toEqual({
            kind: 'remote', url: 'https://example.com/docs',
        });
        expect(webPreviewTargetFromFile({ fileName: 'icon.svg', filePath: 'https://cdn.example/icon.svg' })).toEqual({
            kind: 'remote', url: 'https://cdn.example/icon.svg',
        });
        expect(webPreviewTargetFromFile({ fileName: 'note.txt', content: 'https://example.com/a' })).toEqual({
            kind: 'remote', url: 'https://example.com/a',
        });
        expect(webPreviewTargetFromFile({ fileName: 'readme.md', content: 'https://example.com is a site' })).toBeNull();
        expect(webPreviewTargetFromFile({ fileName: 'photo.png', absPath: 'D:/photo.png' })).toBeNull();
        expect(webPreviewTargetFromFile({ fileName: 'main.ts', content: 'const x = 1' })).toBeNull();
        expect(agentWebPreviewRequestKey({ fileName: 'index.html', filePath: 'site/index.html', opType: 'create', updatedAt: 4, content: 'hi' })).toBe('site/index.html:4:2');
        expect(agentWebPreviewRequestKey({ fileName: 'icon.svg', filePath: 'icon.svg', opType: 'modify', updatedAt: 5 })).toBe('icon.svg:5:0');
        expect(agentWebPreviewRequestKey({ fileName: 'index.html', filePath: 'site/index.html', opType: 'read', updatedAt: 4 })).toBe('');
        expect(agentWebPreviewRequestKey({ fileName: 'main.ts', filePath: 'main.ts', opType: 'create', updatedAt: 4 })).toBe('');
        expect(agentWebPreviewRequestKey({ fileName: 'docs', filePath: 'https://example.com/docs', opType: 'read', forceOpen: true, updatedAt: 8 })).toBe('https://example.com/docs:8:0');
    });

    it('keeps srcdoc off the assistant origin and framed pages on their own origin', () => {
        expect(webPreviewSandbox('srcdoc')).not.toContain('allow-same-origin');
        expect(webPreviewSandbox('srcdoc')).not.toContain('allow-top-navigation');
        expect(webPreviewSandbox('framed')).toContain('allow-same-origin');
        expect(webPreviewSandbox('framed')).toContain('allow-scripts');
        expect(webPreviewSandbox('framed')).not.toContain('allow-top-navigation');
    });

    it('builds a loopback url and a display path without the token', () => {
        expect(localPreviewURL('http://127.0.0.1:9', 'abc', 'css/app.css')).toBe('http://127.0.0.1:9/web-preview/v1/abc/css/app.css');
        expect(localPreviewURL('http://127.0.0.1:9', 'abc', '../secret.html')).toBe('http://127.0.0.1:9/web-preview/v1/abc/secret.html');
        expect(joinPreviewDisplay('D:\\site', 'css/app.css')).toBe('D:\\site\\css\\app.css');
        expect(decodePreviewPath('my%20page.html')).toBe('my page.html');
        expect(decodePreviewPath('%')).toBe('%');
    });
});

describe('WebPreviewBrowser', () => {
    it('loads a local HTML directory and navigates to a typed URL', async () => {
        render(
            <WebPreviewBrowser
                absPath="D:/site/index.html"
                content="<h1>Hello</h1>"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        const frame = () => screen.getByTestId('html-preview-panel');
        await waitFor(() => expect(frame().getAttribute('src')).toBe('http://127.0.0.1:9/web-preview/v1/abc/index.html'));
        expect(frame().getAttribute('sandbox')).not.toContain('allow-top-navigation');
        expect(screen.getByTestId('web-preview-address').getAttribute('value') || (screen.getByTestId('web-preview-address') as HTMLInputElement).value).toContain('index.html');
        expect((screen.getByTestId('web-preview-address') as HTMLInputElement).value).not.toContain('/web-preview/');

        fireEvent.change(screen.getByTestId('web-preview-address'), { target: { value: 'https://example.com/docs' } });
        fireEvent.click(screen.getByTestId('web-preview-go'));
        await waitFor(() => expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('https://example.com/docs'));
        expect(screen.getByTestId('web-preview-notice').textContent).toContain('禁止嵌入');

        fireEvent.change(screen.getByTestId('web-preview-address'), { target: { value: 'javascript:alert(1)' } });
        fireEvent.click(screen.getByTestId('web-preview-go'));
        expect(screen.getByTestId('web-preview-error').textContent).toContain('无法打开');
        expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('https://example.com/docs');

        fireEvent.click(screen.getByTestId('web-preview-back'));
        await waitFor(() => expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('http://127.0.0.1:9/web-preview/v1/abc/index.html'));

        fireEvent.click(screen.getByTestId('web-preview-external'));
        expect(vi.mocked(BrowserOpenURL)).toHaveBeenCalledWith('http://127.0.0.1:9/web-preview/v1/abc/index.html');
        expect(vi.mocked(OpenWebPreview)).toHaveBeenCalledWith('D:/site/index.html');
    });

    it('records an in-page move without reloading the preview', async () => {
        render(
            <WebPreviewBrowser
                absPath="D:/site/index.html"
                content="<h1>Hello</h1>"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        await waitFor(() => expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('http://127.0.0.1:9/web-preview/v1/abc/index.html'));
        const moved = new MessageEvent('message', {
            data: { source: 'mc-web-preview', type: 'location', path: 'about%20page.html#top' },
        });
        Object.defineProperty(moved, 'origin', { value: 'http://127.0.0.1:9' });
        act(() => { window.dispatchEvent(moved); });
        expect((screen.getByTestId('web-preview-address') as HTMLInputElement).value).toContain('about page.html#top');
        expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('http://127.0.0.1:9/web-preview/v1/abc/index.html');

        fireEvent.click(screen.getByTestId('web-preview-reload'));
        await waitFor(() => expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('http://127.0.0.1:9/web-preview/v1/abc/about%20page.html#top'));

        const escaped = new MessageEvent('message', {
            data: { source: 'mc-web-preview', type: 'location', path: '../secret.html' },
        });
        Object.defineProperty(escaped, 'origin', { value: 'http://127.0.0.1:9' });
        act(() => { window.dispatchEvent(escaped); });
        expect((screen.getByTestId('web-preview-address') as HTMLInputElement).value).toContain('about page.html#top');

        fireEvent.click(screen.getByTestId('web-preview-back'));
        await waitFor(() => {
            expect((screen.getByTestId('web-preview-address') as HTMLInputElement).value).toContain('index.html');
        });
        expect((screen.getByTestId('web-preview-address') as HTMLInputElement).value).not.toContain('about');
    });

    it('drops a file open that finishes after a newer address', async () => {
        const open = vi.mocked(OpenWebPreview);
        const original = open.getMockImplementation();
        type OpenedWebPreview = Awaited<ReturnType<typeof OpenWebPreview>>;
        let release: (value: OpenedWebPreview) => void = () => {};
        open.mockImplementation(async (absPath: string) => {
            if (absPath === 'D:/other/page.html') {
                return new Promise<OpenedWebPreview>((resolve) => { release = resolve; });
            }
            return {
                id: 'abc',
                origin: 'http://127.0.0.1:9',
                token: 'abc',
                root: String(absPath).replace(/[\\/][^\\/]+$/, ''),
                entry_url: 'http://127.0.0.1:9/web-preview/v1/abc/index.html',
                entry_path: 'index.html',
            };
        });
        try {
            render(
                <WebPreviewBrowser
                    absPath="D:/site/index.html"
                    content="<h1>Hello</h1>"
                    theme={lightCodePreviewTheme}
                    lang="zh"
                />,
            );
            await waitFor(() => expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('http://127.0.0.1:9/web-preview/v1/abc/index.html'));
            fireEvent.change(screen.getByTestId('web-preview-address'), { target: { value: 'D:/other/page.html' } });
            fireEvent.click(screen.getByTestId('web-preview-go'));
            fireEvent.change(screen.getByTestId('web-preview-address'), { target: { value: 'https://example.com/now' } });
            fireEvent.click(screen.getByTestId('web-preview-go'));
            await waitFor(() => expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('https://example.com/now'));
            await act(async () => {
                release({
                    id: 'late',
                    origin: 'http://127.0.0.1:9',
                    token: 'late',
                    root: 'D:/other',
                    entry_url: 'http://127.0.0.1:9/web-preview/v1/late/page.html',
                    entry_path: 'page.html',
                });
            });
            expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('https://example.com/now');
            expect(vi.mocked(CloseWebPreview)).toHaveBeenCalledWith('late');
        } finally {
            if (original) open.mockImplementation(original);
        }
    });
});
