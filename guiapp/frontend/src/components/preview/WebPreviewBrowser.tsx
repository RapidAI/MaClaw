import React, { useEffect, useRef, useState } from 'react';
import { CloseWebPreview, OpenWebPreview } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import type { CodePreviewTheme } from '../ai/FileTabBar';
import {
    classifyWebPreviewAddress,
    joinPreviewDisplay,
    localPreviewURL,
    decodePreviewPath,
    splitPreviewLocation,
    standaloneHTTPURL,
    webPreviewSandbox,
    type WebPreviewAddress,
} from './webPreviewAddress';

type WebPreviewInfo = {
    id?: string;
    origin?: string;
    token?: string;
    root?: string;
    entry_url?: string;
    entry_path?: string;
};

type HistoryEntry = {
    url: string;
    label: string;
    rel: string;
    mode: 'local' | 'remote' | 'srcdoc';
};

export function WebPreviewBrowser({
    absPath,
    content,
    url,
    theme,
    lang,
}: {
    absPath?: string;
    content?: string;
    /** http(s) page. Takes precedence over a local file. */
    url?: string;
    theme: CodePreviewTheme;
    lang: string;
}) {
    const isZh = lang.startsWith('zh');
    const contentRef = useRef(content);
    contentRef.current = content;
    const sessionRef = useRef<WebPreviewInfo | null>(null);
    const historyRef = useRef<HistoryEntry[]>([]);
    const indexRef = useRef(0);
    const addressFocused = useRef(false);
    const navGenRef = useRef(0);
    const initialRemote = standaloneHTTPURL(url) || standaloneHTTPURL(absPath);
    const [address, setAddress] = useState(initialRemote);
    const [error, setError] = useState('');
    const [notice, setNotice] = useState('');
    const [histIndex, setHistIndex] = useState(0);
    const [histLength, setHistLength] = useState(0);
    const [reloadNonce, setReloadNonce] = useState(0);
    const [frame, setFrame] = useState(() => (
        initialRemote
            ? { mode: 'url' as const, src: initialRemote, srcDoc: '' }
            : { mode: 'srcdoc' as const, src: '', srcDoc: String(content || '') }
    ));

    const remoteHint = isZh
        ? '若页面空白，该网站可能禁止嵌入。可以在系统浏览器中打开。'
        : 'If this page stays blank, the site may refuse to be embedded. Open it in the system browser.';
    const assetHint = isZh
        ? '相对资源未能从本地目录加载，当前显示的是文件内容。'
        : 'Relative assets could not be loaded. Showing the file contents.';

    const setAddressIfIdle = (next: string) => {
        if (!addressFocused.current) setAddress(next);
    };

    const publishHistory = (entries: HistoryEntry[], index: number) => {
        historyRef.current = entries;
        indexRef.current = index;
        setHistIndex(index);
        setHistLength(entries.length);
        const current = entries[index];
        if (current) setAddressIfIdle(current.label);
    };

    const showEntry = (entry: HistoryEntry) => {
        if (entry.mode === 'srcdoc' || !entry.url) {
            setFrame({
                mode: 'srcdoc',
                src: '',
                srcDoc: String(contentRef.current || ''),
            });
            setNotice('');
            return;
        }
        setFrame({ mode: 'url', src: entry.url, srcDoc: '' });
        setNotice(entry.mode === 'remote' ? remoteHint : '');
    };

    const pushEntry = (entry: HistoryEntry) => {
        const entries = historyRef.current.slice(0, indexRef.current + 1);
        entries.push(entry);
        publishHistory(entries, entries.length - 1);
        showEntry(entry);
    };

    useEffect(() => () => {
        navGenRef.current += 1;
    }, []);

    useEffect(() => {
        const remote = standaloneHTTPURL(url) || standaloneHTTPURL(absPath);
        const marker = { current: true };
        if (remote) {
            sessionRef.current = null;
            const entry: HistoryEntry = { url: remote, label: remote, rel: remote, mode: 'remote' };
            publishHistory([entry], 0);
            showEntry(entry);
            setError('');
            return () => {
                marker.current = false;
            };
        }
        const path = String(absPath || '').trim();
        if (!path) {
            sessionRef.current = null;
            const provided = String(contentRef.current || '');
            const entry: HistoryEntry = {
                url: '',
                label: isZh ? '页面内容' : 'Page content',
                rel: '',
                mode: 'srcdoc',
            };
            publishHistory([entry], 0);
            setFrame({ mode: 'srcdoc', src: '', srcDoc: provided });
            setNotice('');
            setError(provided.trim() ? '' : (isZh ? '没有可预览的页面' : 'Nothing to preview'));
            return () => {
                marker.current = false;
            };
        }
        setError('');
        setNotice('');
        setFrame({
            mode: 'srcdoc',
            src: '',
            srcDoc: String(contentRef.current || ''),
        });
        void (async () => {
            let info: WebPreviewInfo | null = null;
            try {
                if (typeof OpenWebPreview === 'function') {
                    info = await OpenWebPreview(path) as WebPreviewInfo;
                }
            } catch {
                info = null;
            }
            if (!marker.current) {
                if (info?.id && typeof CloseWebPreview === 'function') void CloseWebPreview(info.id);
                return;
            }
            if (!info?.entry_url || !info.token || !info.origin || !info.root) {
                const provided = String(contentRef.current || '').trim();
                if (!provided) setError(isZh ? '无法打开这个页面' : 'This page cannot be opened');
                else setNotice(assetHint);
                return;
            }
            sessionRef.current = info;
            const label = joinPreviewDisplay(info.root, info.entry_path || 'index.html');
            const entry: HistoryEntry = {
                url: info.entry_url,
                label,
                rel: info.entry_path || 'index.html',
                mode: 'local',
            };
            publishHistory([entry], 0);
            showEntry(entry);
        })();
        return () => {
            marker.current = false;
            const id = sessionRef.current?.id;
            sessionRef.current = null;
            if (id && typeof CloseWebPreview === 'function') {
                void Promise.resolve(CloseWebPreview(id)).catch(() => undefined);
            }
        };
    }, [absPath, assetHint, isZh, remoteHint, url]);

    useEffect(() => {
        const onMessage = (event: MessageEvent) => {
            const session = sessionRef.current;
            if (!session?.origin || event.origin !== session.origin) return;
            const data = event.data as { source?: string; type?: string; path?: string } | null;
            if (!data || data.source !== 'mc-web-preview' || data.type !== 'location') return;
            // The page has already navigated. Record that for the address bar
            // and back button, but do not assign iframe src again — that reloads
            // the document and drops the state the user is looking at.
            const reported = decodePreviewPath(String(data.path || ''));
            const current = historyRef.current[indexRef.current];
            if (current?.mode === 'local' && decodePreviewPath(current.rel) === reported) {
                if (!addressFocused.current) setAddress(current.label);
                return;
            }
            const { path, suffix } = splitPreviewLocation(reported);
            const filePath = path || session.entry_path || 'index.html';
            if (!filePath || filePath.split(/[\\/]/).includes('..')) return;
            const entry: HistoryEntry = {
                url: localPreviewURL(session.origin, session.token || '', filePath) + suffix,
                label: joinPreviewDisplay(session.root || '', filePath) + suffix,
                rel: filePath + suffix,
                mode: 'local',
            };
            const entries = historyRef.current.slice(0, indexRef.current + 1);
            entries.push(entry);
            historyRef.current = entries;
            indexRef.current = entries.length - 1;
            setHistIndex(indexRef.current);
            setHistLength(entries.length);
            if (!addressFocused.current) setAddress(entry.label);
        };
        window.addEventListener('message', onMessage);
        return () => window.removeEventListener('message', onMessage);
    }, []);

    const submitAddress = (event: React.FormEvent) => {
        event.preventDefault();
        void navigateAddress(address);
    };

    const navigateAddress = async (raw: string) => {
        const gen = ++navGenRef.current;
        const classified: WebPreviewAddress = classifyWebPreviewAddress(raw);
        if (classified.kind === 'invalid') {
            setError(isZh ? '无法打开这个地址' : 'This address cannot be opened');
            return;
        }
        setError('');
        if (classified.kind === 'remote') {
            pushEntry({ url: classified.url, label: classified.url, rel: classified.url, mode: 'remote' });
            return;
        }
        if (classified.kind === 'file') {
            let info: WebPreviewInfo | null = null;
            try {
                info = await OpenWebPreview(classified.path) as WebPreviewInfo;
            } catch (err) {
                if (gen !== navGenRef.current) return;
                const message = err instanceof Error ? err.message : String(err || '');
                setError(message || (isZh ? '无法打开这个文件' : 'This file cannot be opened'));
                return;
            }
            if (gen !== navGenRef.current) {
                if (info?.id && typeof CloseWebPreview === 'function') void CloseWebPreview(info.id);
                return;
            }
            if (!info?.entry_url || !info.origin || !info.token || !info.root) {
                setError(isZh ? '无法打开这个文件' : 'This file cannot be opened');
                return;
            }
            const previous = sessionRef.current?.id;
            if (previous && typeof CloseWebPreview === 'function') {
                void Promise.resolve(CloseWebPreview(previous)).catch(() => undefined);
            }
            sessionRef.current = info;
            pushEntry({
                url: info.entry_url,
                label: joinPreviewDisplay(info.root, info.entry_path || 'index.html'),
                rel: info.entry_path || 'index.html',
                mode: 'local',
            });
            return;
        }
        const session = sessionRef.current;
        if (!session?.origin || !session.token) {
            setError(isZh ? '先打开一个本地页面，或输入 http(s) 网址' : 'Open a local page first, or enter an http(s) URL');
            return;
        }
        const { path, suffix } = splitPreviewLocation(classified.path);
        pushEntry({
            url: localPreviewURL(session.origin, session.token, path) + suffix,
            label: joinPreviewDisplay(session.root || '', path) + suffix,
            rel: `${path}${suffix}`,
            mode: 'local',
        });
    };

    const goHistory = (delta: number) => {
        const next = indexRef.current + delta;
        const entry = historyRef.current[next];
        if (!entry) return;
        publishHistory(historyRef.current, next);
        showEntry(entry);
        setReloadNonce((value) => value + 1);
    };

    const reloadPreview = () => {
        const current = historyRef.current[indexRef.current];
        if (current?.url && current.mode !== 'srcdoc') {
            setFrame({ mode: 'url', src: current.url, srcDoc: '' });
        }
        setReloadNonce((value) => value + 1);
    };

    const openExternal = () => {
        const current = historyRef.current[indexRef.current];
        if (!current?.url || typeof BrowserOpenURL !== 'function') return;
        BrowserOpenURL(current.url);
    };

    const buttonStyle = (disabled = false): React.CSSProperties => ({
        border: `1px solid ${theme.border}`,
        background: theme.bg,
        color: disabled ? theme.lineNumText : theme.text,
        borderRadius: 6,
        height: 26,
        minWidth: 26,
        padding: '0 7px',
        cursor: disabled ? 'default' : 'pointer',
        font: 'inherit',
        fontSize: 13,
        lineHeight: '24px',
        flexShrink: 0,
    });

    const hasFrame = frame.mode === 'url' ? Boolean(frame.src) : Boolean(frame.srcDoc.trim());
    const externalURL = historyRef.current[histIndex]?.url || '';

    return (
        <div
            data-testid="web-preview-browser"
            style={{
                display: 'flex',
                flexDirection: 'column',
                flex: 1,
                height: '100%',
                minHeight: 0,
                minWidth: 0,
                background: theme.bg,
                color: theme.text,
            }}
        >
            <form
                data-testid="web-preview-toolbar"
                onSubmit={submitAddress}
                style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 4,
                    padding: '6px 8px',
                    borderBottom: `1px solid ${theme.border}`,
                    background: theme.tabBg,
                    flexShrink: 0,
                }}
            >
                <button
                    type="button"
                    data-testid="web-preview-back"
                    aria-label={isZh ? '后退' : 'Back'}
                    title={isZh ? '后退' : 'Back'}
                    disabled={histIndex <= 0}
                    onClick={() => goHistory(-1)}
                    style={buttonStyle(histIndex <= 0)}
                >
                    ‹
                </button>
                <button
                    type="button"
                    data-testid="web-preview-forward"
                    aria-label={isZh ? '前进' : 'Forward'}
                    title={isZh ? '前进' : 'Forward'}
                    disabled={histIndex >= histLength - 1}
                    onClick={() => goHistory(1)}
                    style={buttonStyle(histIndex >= histLength - 1)}
                >
                    ›
                </button>
                <button
                    type="button"
                    data-testid="web-preview-reload"
                    aria-label={isZh ? '刷新' : 'Reload'}
                    title={isZh ? '刷新' : 'Reload'}
                    onClick={reloadPreview}
                    style={buttonStyle(false)}
                >
                    ↻
                </button>
                <input
                    data-testid="web-preview-address"
                    aria-label={isZh ? '网址' : 'Address'}
                    value={address}
                    spellCheck={false}
                    placeholder={isZh ? '网址、本地 HTML 路径，或页面内的相对路径' : 'URL, local HTML path, or a relative path'}
                    onChange={(event) => setAddress(event.target.value)}
                    onFocus={() => { addressFocused.current = true; }}
                    onBlur={() => { addressFocused.current = false; }}
                    style={{
                        flex: 1,
                        minWidth: 0,
                        height: 26,
                        boxSizing: 'border-box',
                        border: `1px solid ${theme.border}`,
                        borderRadius: 6,
                        background: theme.bg,
                        color: theme.text,
                        padding: '0 8px',
                        font: 'inherit',
                        fontSize: 12,
                        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
                    }}
                />
                <button type="submit" data-testid="web-preview-go" style={buttonStyle(false)}>
                    {isZh ? '打开' : 'Open'}
                </button>
                <button
                    type="button"
                    data-testid="web-preview-external"
                    aria-label={isZh ? '在系统浏览器中打开' : 'Open in system browser'}
                    title={isZh ? '在系统浏览器中打开' : 'Open in system browser'}
                    disabled={!externalURL}
                    onClick={openExternal}
                    style={buttonStyle(!externalURL)}
                >
                    {isZh ? '系统浏览器' : 'Browser'}
                </button>
            </form>
            {error ? (
                <div data-testid="web-preview-error" role="alert" style={{ padding: '6px 12px', color: theme.diffDeleteText, fontSize: 12, flexShrink: 0 }}>
                    {error}
                </div>
            ) : null}
            {notice ? (
                <div data-testid="web-preview-notice" role="status" style={{ padding: '6px 12px', color: theme.textMuted, fontSize: 12, flexShrink: 0 }}>
                    {notice}
                </div>
            ) : null}
            {hasFrame ? (
                <iframe
                    key={`${frame.mode}:${frame.src}:${reloadNonce}`}
                    data-testid="html-preview-panel"
                    title={isZh ? '网页预览' : 'Web preview'}
                    sandbox={webPreviewSandbox(frame.mode === 'srcdoc' ? 'srcdoc' : 'framed')}
                    referrerPolicy="no-referrer"
                    src={frame.mode === 'url' ? frame.src : undefined}
                    srcDoc={frame.mode === 'srcdoc' ? frame.srcDoc : undefined}
                    style={{ flex: 1, minHeight: 0, width: '100%', border: 'none', background: '#fff' }}
                />
            ) : null}
        </div>
    );
}
