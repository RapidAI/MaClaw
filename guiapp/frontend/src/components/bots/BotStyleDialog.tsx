import { useEffect, useId, useState } from 'react';
import type { GetDesktopBotStyle, RollbackDesktopBotStyle, SaveDesktopBotStyle } from '../../../wailsjs/go/main/App';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { tailText } from '../../i18n/mnTail';

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
    const t = (en: string, zh: string) => tailText(lang, en, zh);
    return {
        title: t('Character and voice', '性格与说话风格'),
        soul: t('Character (SOUL)', '性格（SOUL）'),
        soulHint: t('Empty uses the default. Applies from the next task.', '留空用默认性格。下一次任务起生效。'),
        memory: t('What it remembers (MEMORY)', '记住的事（MEMORY）'),
        memoryHint: t('Empty means nothing extra to remember.', '留空表示没有额外要记的事。'),
        parts: t('Advanced: style parts', '高级：分段规则'),
        partsHint: t('Empty keeps the default; - turns the part off.', '留空用默认；填 - 关闭这一段。'),
        overridden: (n: number) => t(`${n} overridden`, `已改写 ${n} 段`),
        off: t('Off', '已关闭'),
        baseChanged: t('The defaults changed since you wrote these overrides.', '默认规则已更新，你的改写是基于旧版本写的。'),
        disabled: t('The style layer is off on the server; saved text is not used yet.', '服务端已关闭风格层，保存的内容暂不生效。'),
        history: t('History', '历史版本'),
        restore: t('Restore', '恢复'),
        save: t('Save', '保存'),
        cancel: t('Cancel', '取消'),
        loading: t('Loading…', '读取中…'),
        failed: t('Could not load', '读取失败'),
    };
}

/* Friendly names for the hub-defined parts; unknown ids fall back to the raw id. */
const PART_NAMES: Record<string, [string, string]> = {
    facts: ['Facts', '事实'],
    honesty: ['Honesty', '诚实'],
    mainbot_lead: ['Main-bot lead', '主 Bot 开场'],
    progress: ['Progress updates', '进度播报'],
    quiet: ['Quiet mode', '安静模式'],
    subbot_speech: ['Sub-bot speech', '子 Bot 说话'],
    voice: ['Voice', '语气'],
};

function partName(lang: string, id: string) {
    const hit = PART_NAMES[id];
    return hit ? tailText(lang, hit[0], hit[1]) : id;
}

/** One override card: friendly name, raw id chip, counter and the textarea. */
function PartField({ id, name, offLabel, value, placeholder, disabled, onChange }: {
    id: string;
    name: string;
    offLabel: string;
    value: string;
    placeholder: string;
    disabled: boolean;
    onChange: (v: string) => void;
}) {
    const inputId = useId();
    const off = value.trim() === '-';
    return (
        <div className="bot-style-dialog__part">
            <div className="bot-style-dialog__part-head">
                <label className="bot-style-dialog__part-name" htmlFor={inputId}>{name}</label>
                <code className="bot-style-dialog__part-id">{id}</code>
                {off ? <span className="bot-style-dialog__part-off">{offLabel}</span> : null}
                <span className="bot-style-dialog__count">{value.length} / {PART_MAX}</span>
            </div>
            <textarea id={inputId} className="bot-style-dialog__textarea bot-style-dialog__textarea--part"
                data-testid={`bot-style-part-${id}`} rows={2} maxLength={PART_MAX} value={value}
                placeholder={placeholder} disabled={disabled} onChange={e => onChange(e.target.value)} />
        </div>
    );
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
    const soulId = useId();
    const memoryId = useId();
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

    const overriddenCount = Object.values(parts).filter(v => v.trim()).length;
    const partIds = view ? Object.keys(view.default_parts).sort() : [];

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-edit-dialog-backdrop" {...backdropProps}>
            <div className="modal-content custom-dialog bot-edit-dialog bot-style-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-style-dialog" {...dialogProps}>
                <div className="modal-header custom-dialog__header">
                    <h3 id={titleId} className="custom-dialog__title">{t.title} · {botTitle}</h3>
                </div>
                <div className="modal-body custom-dialog__body">
                    {!view ? <p className="bot-style-dialog__loading" role={notice ? 'alert' : undefined}>{notice || t.loading}</p> : (
                        <div className="bot-style-dialog__form">
                            {!view.enabled ? <p className="bot-style-dialog__warn" data-testid="bot-style-disabled">{t.disabled}</p> : null}

                            <section>
                                <div className="bot-style-dialog__section-head">
                                    <label className="bot-style-dialog__label" htmlFor={soulId}>{t.soul}</label>
                                    <span className="bot-style-dialog__count">{soul.length} / {SOUL_MAX}</span>
                                </div>
                                <textarea id={soulId} className="bot-style-dialog__textarea" data-testid="bot-style-soul" rows={6} maxLength={SOUL_MAX}
                                    value={soul} placeholder={view.default_soul} disabled={saving} onChange={e => setSoul(e.target.value)} />
                                <p className="bot-style-dialog__hint">{t.soulHint}</p>
                            </section>

                            <section>
                                <div className="bot-style-dialog__section-head">
                                    <label className="bot-style-dialog__label" htmlFor={memoryId}>{t.memory}</label>
                                    <span className="bot-style-dialog__count">{memory.length} / {MEMORY_MAX}</span>
                                </div>
                                <textarea id={memoryId} className="bot-style-dialog__textarea" data-testid="bot-style-memory" rows={3} maxLength={MEMORY_MAX}
                                    value={memory} disabled={saving} onChange={e => setMemory(e.target.value)} />
                                <p className="bot-style-dialog__hint">{t.memoryHint}</p>
                            </section>

                            <details className="bot-style-dialog__details">
                                <summary className="bot-style-dialog__summary">
                                    {t.parts}
                                    {overriddenCount ? <span className="bot-style-dialog__summary-chip">{t.overridden(overriddenCount)}</span> : null}
                                </summary>
                                <div className="bot-style-dialog__details-body">
                                    <p className="bot-style-dialog__hint bot-style-dialog__hint--flush">{t.partsHint}</p>
                                    {view.base_changed ? <p className="bot-style-dialog__warn" data-testid="bot-style-base-changed">{t.baseChanged}</p> : null}
                                    {partIds.map(id => (
                                        <PartField key={id} id={id} name={partName(lang, id)} offLabel={t.off}
                                            value={parts[id] || ''} placeholder={view.default_parts[id]} disabled={saving}
                                            onChange={v => setParts(prev => ({ ...prev, [id]: v }))} />
                                    ))}
                                </div>
                            </details>

                            {view.history && view.history.length ? (
                                <details className="bot-style-dialog__details">
                                    <summary className="bot-style-dialog__summary">
                                        {t.history}
                                        <span className="bot-style-dialog__summary-chip">{view.history.length}</span>
                                    </summary>
                                    <div className="bot-style-dialog__details-body">
                                        <ul className="bot-style-dialog__history">
                                            {[...view.history].reverse().map(h => (
                                                <li key={h.version}>
                                                    <span className="bot-style-dialog__version">v{h.version}</span>
                                                    <span className="bot-style-dialog__history-time">{h.updated_at ? new Date(h.updated_at).toLocaleString() : ''}</span>
                                                    <button type="button" className="bot-style-dialog__restore"
                                                        data-testid={`bot-style-restore-${h.version}`} disabled={saving}
                                                        onClick={() => { void run(() => api.rollback(botId, h.version)); }}>{t.restore}</button>
                                                </li>
                                            ))}
                                        </ul>
                                    </div>
                                </details>
                            ) : null}

                            {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
                        </div>
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
