import { useRef, useState } from 'react';
import { ExportTaskResultFile, OpenFileOrShowInFolder, ShowItemInFolder } from '../../../wailsjs/go/main/App';
import { chromeText } from '../../i18n/chromeText';
import { ArtifactFileIcon } from '../ai/assistantTaskArtifacts';
import { dispatchPreviewTaskResult, localizeTaskResultExportError } from '../ai/taskResultPreview';
import { formatArtifactSize, formatArtifactWhen, type BotArtifactRow } from './botFileArtifacts';

// One file the bubble can open once it is on this computer. ensurePath writes
// or fetches that copy. onOpen is the fallback when the bytes cannot be saved
// (the browser downloads them), used by 查看文档 only.
export type BotArtifactCardFile = {
    name: string;
    testId?: string;
    ensurePath?: () => Promise<string>;
    onOpen?: () => Promise<void>;
};

function errorText(err: unknown, lang: string): string {
    const raw = err instanceof Error ? err.message : String(err || '');
    const detail = raw.trim();
    const fallback = chromeText(lang, 'The file could not be fetched', '文件取回失败', '檔案取回失敗');
    if (!detail) return fallback;
    return detail.length > 160 ? `${detail.slice(0, 160)}…` : detail;
}

async function openLocal(path: string): Promise<void> {
    await OpenFileOrShowInFolder(path).catch(() => ShowItemInFolder(path));
}

export function BotFileArtifactCard({
    files,
    messageId,
    lang,
}: {
    files: BotArtifactCardFile[];
    messageId: string;
    lang: string;
}) {
    const [busy, setBusy] = useState<Record<string, string>>({});
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [exported, setExported] = useState<Record<string, string>>({});
    // Set before the state update, so a second click on the same file cannot
    // start another fetch. Another file on the card stays usable.
    const inflight = useRef(new Set<string>());
    if (!files.length) return null;
    const fail = (name: string, err: unknown) => {
        setErrors(prev => ({ ...prev, [name]: errorText(err, lang) }));
    };
    const run = async (file: BotArtifactCardFile, key: string, action: string, act: (path: string) => Promise<void> | void, allowFallback: boolean) => {
        if (inflight.current.has(key)) return;
        inflight.current.add(key);
        setBusy(prev => ({ ...prev, [key]: action }));
        setErrors(prev => ({ ...prev, [key]: '' }));
        try {
            // A download is only the fallback for bytes that never landed on
            // disk. Once a path exists, opening it is the action: a failure
            // there must not also save a second copy through the browser.
            let path = '';
            if (file.ensurePath) {
                try {
                    path = String(await file.ensurePath() || '').trim();
                } catch (err) {
                    if (!allowFallback || !file.onOpen) throw err;
                }
            }
            if (path) {
                await act(path);
                return;
            }
            if (allowFallback && file.onOpen) {
                await file.onOpen();
                return;
            }
            throw new Error(chromeText(lang, 'The file could not be fetched', '文件取回失败', '檔案取回失敗'));
        } catch (err) {
            fail(key, err);
        } finally {
            inflight.current.delete(key);
            setBusy(prev => {
                if (!prev[key]) return prev;
                const next = { ...prev };
                delete next[key];
                return next;
            });
        }
    };
    return (
        <div className="mc-task-artifacts" data-testid={`bot-artifact-card-${messageId}`}>
            <div className="mc-task-artifacts__head">
                <span className="mc-task-artifacts__glyph" aria-hidden="true">◈</span>
                <span className="mc-task-artifacts__label">{chromeText(lang, 'Artifacts', '产出物', '產出物')}</span>
                <span className="mc-task-artifacts__count">{files.length}</span>
            </div>
            <div className="mc-task-artifacts__row">
                {files.map((file, index) => {
                    const suffix = index > 0 ? `-${index}` : '';
                    const key = `${index}:${file.name}`;
                    const fileBusy = busy[key] != null;
                    const saved = exported[key];
                    const exportLabel = fileBusy && busy[key] === 'export'
                        ? chromeText(lang, 'Exporting...', '导出中...', '匯出中...')
                        : saved
                            ? chromeText(lang, 'Exported', '已导出', '已匯出')
                            : chromeText(lang, 'Export', '导出', '匯出');
                    return (
                        <div key={`${file.name}-${index}`} className="mc-task-artifact-item">
                            <div className="mc-task-artifact-item__head">
                                <a
                                    className="mc-task-artifact"
                                    href="#"
                                    title={file.name}
                                    onClick={event => {
                                        event.preventDefault();
                                        void run(file, key, 'open', openLocal, true);
                                    }}
                                >
                                    <ArtifactFileIcon path={file.name} />
                                    <span className="mc-task-artifact__name">{file.name}</span>
                                    <span className="mc-task-artifact__open" aria-hidden="true">↗</span>
                                </a>
                            </div>
                            <div className="mc-task-result-card__actions mc-task-artifact-item__actions">
                                <button
                                    type="button"
                                    className="mc-artifact-btn--fill"
                                    data-testid={`bot-artifact-preview${suffix}`}
                                    disabled={fileBusy}
                                    onClick={event => {
                                        event.stopPropagation();
                                        void run(file, key, 'preview', path => { dispatchPreviewTaskResult(path, messageId); }, false);
                                    }}
                                >{chromeText(lang, 'Preview', '预览', '預覽')}</button>
                                <button
                                    type="button"
                                    data-testid={file.testId || `bot-artifact-view${suffix}`}
                                    disabled={fileBusy}
                                    onClick={event => {
                                        event.stopPropagation();
                                        void run(file, key, 'open', openLocal, true);
                                    }}
                                >{chromeText(lang, 'View document', '查看文档', '查看文件')}</button>
                                <button
                                    type="button"
                                    data-testid={`bot-artifact-export${suffix}`}
                                    title={saved ? chromeText(lang, `Exported to ${saved}`, `已复制到 ${saved}`, `已複製到 ${saved}`) : chromeText(lang, 'Save a copy of this file', '将文档复制到所选路径', '將文件複製到所選路徑')}
                                    disabled={fileBusy}
                                    onClick={event => {
                                        event.stopPropagation();
                                        void run(file, key, 'export', async path => {
                                            if (typeof ExportTaskResultFile !== 'function') throw new Error(chromeText(lang, 'Export failed', '导出失败', '匯出失敗'));
                                            const dest = String(await ExportTaskResultFile(path) || '').trim();
                                            if (dest) setExported(prev => ({ ...prev, [key]: dest }));
                                        }, false);
                                    }}
                                >{exportLabel}</button>
                                <button
                                    type="button"
                                    data-testid={`bot-artifact-reveal${suffix}`}
                                    disabled={fileBusy}
                                    onClick={event => {
                                        event.stopPropagation();
                                        void run(file, key, 'reveal', path => ShowItemInFolder(path), false);
                                    }}
                                >{chromeText(lang, 'Reveal', '定位', '定位')}</button>
                            </div>
                            {errors[key] ? <p className="bot-file-artifact__error" role="alert">{localizeTaskResultExportError(errors[key], lang)}</p> : null}
                        </div>
                    );
                })}
            </div>
        </div>
    );
}

export function BotArtifactTable({
    rows,
    nameLabel,
    sizeLabel,
    timeLabel,
    emptyLabel,
    notice,
    noticeError,
    onOpen,
}: {
    rows: BotArtifactRow[];
    nameLabel: string;
    sizeLabel: string;
    timeLabel: string;
    emptyLabel: string;
    notice?: string;
    noticeError?: boolean;
    onOpen: (row: BotArtifactRow) => void;
}) {
    if (!rows.length) {
        return notice
            ? <p className={`desktop-bot-panel__empty${noticeError ? ' is-error' : ''}`} data-testid="desktop-bot-artifact-notice" role={noticeError ? 'alert' : 'status'}>{notice}</p>
            : <p className="desktop-bot-panel__empty" data-testid="desktop-bot-artifact-empty">{emptyLabel}</p>;
    }
    return (
        <>
            {notice ? <p className={`desktop-bot-panel__empty${noticeError ? ' is-error' : ''}`} data-testid="desktop-bot-artifact-notice" role={noticeError ? 'alert' : 'status'}>{notice}</p> : null}
            <table className="desktop-bot-artifacts" data-testid="desktop-bot-artifact-table">
                <thead>
                    <tr>
                        <th scope="col">{nameLabel}</th>
                        <th scope="col">{sizeLabel}</th>
                        <th scope="col">{timeLabel}</th>
                    </tr>
                </thead>
                <tbody>
                    {rows.map(row => {
                        const when = formatArtifactWhen(row.whenMs);
                        const size = formatArtifactSize(row.size);
                        return (
                            <tr
                                key={row.key}
                                className="desktop-bot-artifacts__row"
                                data-testid="desktop-bot-artifact-row"
                                aria-label={row.name}
                                tabIndex={0}
                                onClick={() => onOpen(row)}
                                onKeyDown={event => {
                                    if (event.key !== 'Enter' && event.key !== ' ') return;
                                    event.preventDefault();
                                    onOpen(row);
                                }}
                            >
                                <td><span className="desktop-bot-artifacts__name" title={row.source.kind === 'path' ? row.source.path : row.name}>{row.name}</span></td>
                                <td className="desktop-bot-artifacts__meta" title={size || undefined}>{size || '—'}</td>
                                <td className="desktop-bot-artifacts__meta" title={when || undefined}>{when || '—'}</td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
        </>
    );
}
