import { useEffect, useRef, useState } from 'react';
import { SubmitExpertMarketListing } from '../../wailsjs/go/main/App';
import { useDialog } from './CustomDialog';
import type { ExpertDefinition } from './ai/expertTypes';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;

const t = (lang: Lang, zh: string, en: string) => lang.startsWith('zh') ? zh : en;
const message = (err: unknown) => err instanceof Error ? err.message : String(err || 'Unknown error');

/**
 * Dedicated submission surface opened from a local expert card.  Keeping this
 * separate from the marketplace avoids making a card action navigate through
 * an unrelated marketplace tab and makes the selected expert unambiguous.
 */
export function ExpertShareDialog({ lang, expert, onClose, onSubmitted }: {
    lang: Lang;
    expert: ExpertDefinition;
    onClose: () => void;
    /**
     * The native response is authoritative immediately after a successful
     * submission. Passing it back lets the card hide Share before a later
     * account refresh completes (or fails).
     */
    onSubmitted?: (listing: Record<string, unknown>) => void;
}) {
    const { showAlert } = useDialog();
    const [version, setVersion] = useState('1.0.0');
    const [price, setPrice] = useState('0');
    const [visibility, setVisibility] = useState<'public' | 'private'>('public');
    const [submitting, setSubmitting] = useState(false);
    const dialogRef = useRef<HTMLElement | null>(null);
    const closeRef = useRef(onClose);
    closeRef.current = onClose;

    useEffect(() => {
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusTimer = window.setTimeout(() => dialogRef.current?.querySelector<HTMLElement>('input:not([disabled]), button:not([disabled])')?.focus(), 0);
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopImmediatePropagation();
                closeRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
            const items = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled])') || []);
            if (!items.length) return;
            const currentIndex = items.indexOf(document.activeElement as HTMLElement);
            const nextIndex = event.shiftKey
                ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
            event.preventDefault();
            event.stopImmediatePropagation();
            items[nextIndex].focus();
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => {
            window.clearTimeout(focusTimer);
            window.removeEventListener('keydown', onKeyDown, true);
            if (previousFocus?.isConnected) previousFocus.focus();
        };
    }, []);

    const submit = async () => {
        const parsedPrice = Number(price);
        if (!Number.isInteger(parsedPrice) || parsedPrice < 0 || parsedPrice > 999999) {
            await showAlert(t(lang, '请输入 0 到 999999 之间的整数 Credits。', 'Enter an integer number of Credits between 0 and 999999.'), t(lang, '价格无效', 'Invalid price'));
            return;
        }
        setSubmitting(true);
        try {
            const listing = await SubmitExpertMarketListing(expert.id, version.trim() || '1.0.0', parsedPrice, visibility);
            onSubmitted?.(listing || {});
            onClose();
            if (visibility === 'private') {
                await showAlert(t(lang, '私有分享无需审核，只有上传该专家的用户可以看到。', 'Private sharing skips review and is visible only to the uploader.'), t(lang, '已私有分享', 'Shared privately'));
                return;
            }
            await showAlert(t(lang, '专家已提交审核，审核通过后会出现在市场中。', 'The expert was submitted for review and will appear in the market once approved.'), t(lang, '已提交审核', 'Submitted for review'));
        } catch (err) {
            await showAlert(message(err), t(lang, '提交失败', 'Submission failed'));
        } finally {
            setSubmitting(false);
        }
    };

    return <div className="expert-share-overlay" role="presentation">
        <section ref={dialogRef} className="expert-share-shell" role="dialog" aria-modal="true" aria-label={t(lang, '分享 AI 专家', 'Share AI expert')}>
            <header className="expert-share-header">
                <div><h2>{t(lang, '分享 AI 专家', 'Share AI expert')}</h2><p>{visibility === 'public' ? t(lang, '公开分享默认需要 HubCenter 审核，审核通过后会在市场展示。', 'Public sharing is the default and requires HubCenter review before market listing.') : t(lang, '私有分享无需审核，只有上传该专家的用户可以看到。', 'Private sharing skips review and is visible only to the uploader.')}</p></div>
                <button className="expert-share-close" type="button" aria-label={t(lang, '关闭', 'Close')} onClick={onClose}>×</button>
            </header>
            <main className="expert-share-body">
                <section className="expert-share-preview" aria-label={t(lang, '专家信息预览', 'Expert details preview')}>
                    <div className="expert-share-preview__heading"><span aria-hidden>{expert.icon || '🤖'}</span><div><strong>{expert.name}</strong><small>{t(lang, '本地专家信息', 'Local expert details')}</small></div></div>
                    <dl>
                        <div><dt>{t(lang, '介绍', 'Description')}</dt><dd>{expert.description || t(lang, '暂无介绍', 'No description')}</dd></div>
                        <div><dt>{t(lang, '工具', 'Tools')}</dt><dd>{expert.tools?.length ? expert.tools.join('、') : t(lang, '未限制', 'No restriction')}</dd></div>
                        <div><dt>{t(lang, '技能', 'Skills')}</dt><dd>{expert.skills?.length ? expert.skills.join('、') : t(lang, '未声明', 'None declared')}</dd></div>
                    </dl>
                </section>
                <div className="expert-share-fields">
                    <fieldset>
                        <legend>{t(lang, '展示范围', 'Visibility')}</legend>
                        <label><input type="radio" name="expert-visibility" checked={visibility === 'public'} onChange={() => setVisibility('public')} />{t(lang, '公开（默认，需审核）', 'Public (default, review required)')}</label>
                        <label><input type="radio" name="expert-visibility" checked={visibility === 'private'} onChange={() => setVisibility('private')} />{t(lang, '私有（无需审核，仅自己可见）', 'Private (no review, only you can see it)')}</label>
                    </fieldset>
                    <label>{t(lang, '版本', 'Version')}<input aria-label={t(lang, '版本', 'Version')} value={version} onChange={event => setVersion(event.target.value)} /></label>
                    <label>{t(lang, '价格（Credits，0 为免费）', 'Price (Credits, 0 is free)')}<input aria-label={t(lang, '价格（Credits，0 为免费）', 'Price (Credits, 0 is free)')} inputMode="numeric" value={price} onChange={event => setPrice(event.target.value)} /></label>
                </div>
            </main>
            <footer className="expert-share-footer"><button type="button" className="btn-secondary" disabled={submitting} onClick={onClose}>{t(lang, '取消', 'Cancel')}</button><button className="btn-primary" type="button" disabled={submitting} onClick={() => void submit()}>{submitting ? t(lang, '提交中…', 'Submitting…') : t(lang, '提交到 AI 专家市场', 'Submit to AI Expert Market')}</button></footer>
        </section>
    </div>;
}
