import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { lightCodePreviewTheme } from '../../ai/CodePreviewPanel';
import { WebPreviewBrowser } from '../WebPreviewBrowser';
import {
    classifyWebPreviewAddress,
    joinPreviewDisplay,
    localPreviewURL,
    webPreviewSandbox,
} from '../webPreviewAddress';
import { BrowserOpenURL } from '../../../../wailsjs/runtime';
import { OpenWebPreview } from '../../../../wailsjs/go/main/App';

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

    it('keeps srcdoc off the assistant origin and framed pages on their own origin', () => {
        expect(webPreviewSandbox('srcdoc')).not.toContain('allow-same-origin');
        expect(webPreviewSandbox('srcdoc')).not.toContain('allow-top-navigation');
        expect(webPreviewSandbox('framed')).toContain('allow-same-origin');
        expect(webPreviewSandbox('framed')).toContain('allow-scripts');
        expect(webPreviewSandbox('framed')).not.toContain('allow-top-navigation');
    });

    it('builds a loopback url and a display path without the token', () => {
        expect(localPreviewURL('http://127.0.0.1:9', 'abc', 'css/app.css')).toBe('http://127.0.0.1:9/web-preview/v1/abc/css/app.css');
        expect(joinPreviewDisplay('D:\\site', 'css/app.css')).toBe('D:\\site\\css\\app.css');
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
});
