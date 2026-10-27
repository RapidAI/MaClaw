import { useEffect, useId, useState } from 'react';
import type { GetDesktopBotStyle, RollbackDesktopBotStyle, SaveDesktopBotStyle } from '../../../wailsjs/go/main/App';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

type StyleView = Awaited<ReturnType<typeof GetDesktopBotStyle>>;

export type BotStyleApi = {
    get: typeof GetDesktopBotStyle;
    save: typeof SaveDesktopBotStyle;
    rollback: typeof RollbackDesktopBotStyle;
};

// Loaded on use, so screens that only render the list need no binding.
const bindings = () => import('../../../wailsjs/go/main/App');
const defaultApi: BotStyleApi = {
    get: async id => (await bindings()).GetDesktopBotStyle(id),
    save: async (id, soul, memory, parts) => (await bindings()).SaveDesktopBotStyle(id, soul, memory, parts),
    rollback: async (id, version) => (await bindings()).RollbackDesktopBotStyle(id, version),
};

const SOUL_MAX = 3000;
const MEMORY_MAX = 2000;
const PART_MAX = 1500;

function labelsFor(lang: string) {
    const zh = /^zh/i.test(lang);
    return {
        title: zh ? '性格与说话风格' : 'Character and voice',
        soul: zh ? '性格（SOUL）' : 'Character (SOUL)',
        soulHint: zh ? '留空用默认性格。下一次任务起生效。' : 'Empty uses the default. Applies from the next task.',
        memory: zh ? '记住的事（MEMORY）' : 'What it remembers (MEMORY)',
        parts: zh ? '高级：分段规则' : 'Advanced: style parts',
        partsHint: zh ? '留空用默认；填 - 关闭这一段。' : 'Empty keeps the default; - turns the part off.',
        baseChanged: zh ? '默认规则已更新，你的改写是基于旧版本写的。' : 'The defaults changed since you wrote these overrides.',
        disabled: zh ? '服务端已关闭风格层，保存的内容暂不生效。' : 'The style layer is off on the server; saved text is not used yet.',
        history: zh ? '历史版本' : 'History',
        restore: zh ? '恢复' : 'Restore',
        save: zh ? '保存' : 'Save',
        cancel: zh ? '取消' : 'Cancel',
        loading: zh ? '读取中…' : 'Loading…',
        failed: zh ? '读取失败' : 'Could not load',
    };
}

/** Per-bot editor for the Hub-kept SOUL, MEMORY and prompt-part overrides, with history. */
export function BotStyleDialog({ botId, botTitle, lang, onClose, api = defaultApi }: {
    botId: string;
    botTitle: string;
    lang: string;
    onClose: () => void;
    api?: BotStyleApi;
}) {
    const t = labelsFor(lang);
    const titleId = useId();
    const [view, setView] = useState<StyleView | null>(null);
    const [soul, setSoul] = useState('');
    const [memory, setMemory] = useState('');
    const [parts, setParts] = useState<Record<string, string>>({});
    const [notice, setNotice] = useState('');
    const [saving, setSaving] = useState(false);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });

    const show = (v: StyleView) => {
        setView(v);
        setSoul(v.current.soul || '');
        setMemory(v.current.memory || '');
        setParts({ ...(v.current.parts || {}) });
    };

    useEffect(() => {
        let live = true;
        api.get(botId).then(v => { if (live) show(v); }).catch(e => { if (live) setNotice(e instanceof Error && e.message ? e.message : t.failed); });
        return () => { live = false; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [botId]);

    const run = async (work: () => Promise<StyleView>) => {
        if (saving) return;
        setSaving(true);
        setNotice('');
        try {
            show(await work());
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : t.failed);
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-edit-dialog-backdrop" {...backdropProps}>
            <div className="modal-content custom-dialog bot-edit-dialog bot-style-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-style-dialog" {...dialogProps}>
                <div className="modal-header custom-dialog__header">
                    <h3 id={titleId} className="custom-dialog__title">{t.title} · {botTitle}</h3>
                </div>
                <div className="modal-body custom-dialog__body">
                    {!view ? <p>{notice || t.loading}</p> : (
                        <>
                            {!view.enabled ? <p className="bot-style-dialog__warn" data-testid="bot-style-disabled">{t.disabled}</p> : null}
                            <label className="custom-dialog__label">{t.soul}
                                <textarea data-testid="bot-style-soul" rows={5} maxLength={SOUL_MAX} value={soul} placeholder={view.default_soul} disabled={saving} onChange={e => setSoul(e.target.value)} />
                            </label>
                            <small>{t.soulHint}</small>
                            <label className="custom-dialog__label">{t.memory}
                                <textarea data-testid="bot-style-memory" rows={3} maxLength={MEMORY_MAX} value={memory} disabled={saving} onChange={e => setMemory(e.target.value)} />
                            </label>
                            <details>
                                <summary>{t.parts}</summary>
                                <small>{t.partsHint}</small>
                                {view.base_changed ? <p className="bot-style-dialog__warn" data-testid="bot-style-base-changed">{t.baseChanged}</p> : null}
                                {Object.keys(view.default_parts).sort().map(id => (
                                    <label key={id} className="custom-dialog__label">{id}
                                        <textarea data-testid={`bot-style-part-${id}`} rows={2} maxLength={PART_MAX} value={parts[id] || ''} placeholder={view.default_parts[id]} disabled={saving}
                                            onChange={e => setParts(prev => ({ ...prev, [id]: e.target.value }))} />
                                    </label>
                                ))}
                            </details>
                            {view.history && view.history.length ? (
                                <details>
                                    <summary>{t.history}</summary>
                                    <ul>
                                        {[...view.history].reverse().map(h => (
                                            <li key={h.version}>
                                                v{h.version} {h.updated_at ? new Date(h.updated_at).toLocaleString() : ''}
                                                <button type="button" data-testid={`bot-style-restore-${h.version}`} disabled={saving} onClick={() => { void run(() => api.rollback(botId, h.version)); }}>{t.restore}</button>
                                            </li>
                                        ))}
                                    </ul>
                                </details>
                            ) : null}
                            {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
                        </>
                    )}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" disabled={saving} onClick={onClose}>{t.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-style-save" disabled={saving || !view}
                        onClick={() => {
                            const clean: Record<string, string> = {};
                            for (const [k, v] of Object.entries(parts)) if (v.trim()) clean[k] = v.trim();
                            void run(() => api.save(botId, soul.trim(), memory.trim(), clean));
                        }}>{t.save}</button>
                </div>
            </div>
        </div>
    );
}
