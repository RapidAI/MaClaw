import { useEffect, useRef, useState } from 'react';
import { SubmitExpertMarketListing, SubmitExpertMarketListingToUsers, SubmitExpertMarketListingWithDistribution } from '../../wailsjs/go/main/App';
import { useDialog } from './CustomDialog';
import type { ExpertDefinition } from './ai/expertTypes';
import { tailText } from '../i18n/mnTail'

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;

const t = (lang: Lang, zh: string, en: string) => tailText(lang, en, zh);
const message = (err: unknown) => err instanceof Error ? err.message : String(err || 'Unknown error');
const SHARE_TARGET_LIMIT = 32;

function shareNationalDigits(digits: string): string {
    if (digits.startsWith('0086') && /^1[3-9]\d{9}$/.test(digits.slice(4))) return digits.slice(4);
    if (digits.startsWith('86') && /^1[3-9]\d{9}$/.test(digits.slice(2))) return digits.slice(2);
    return digits;
}

function shareContactKey(value: string): string {
    const text = value.trim();
    if (text.includes('@')) return `email:${text.toLowerCase()}`;
    return `phone:${shareNationalDigits(text.replace(/\D/g, ''))}`;
}

function parseShareTargets(raw: string): { targets: string[]; error: 'empty' | 'invalid' | 'limit' | null; invalid?: string } {
    const parts = raw.split(/[,，;；、\n\r]+/).map(part => part.trim()).filter(Boolean);
    if (!parts.length) return { targets: [], error: 'empty' };
    if (parts.length > SHARE_TARGET_LIMIT * 4) return { targets: [], error: 'limit' };
    const seen = new Set<string>();
    const targets: string[] = [];
    for (const part of parts) {
        if (!isShareContact(part)) return { targets: [], error: 'invalid', invalid: part };
        const key = shareContactKey(part);
        if (seen.has(key)) continue;
        seen.add(key);
        targets.push(part);
        if (targets.length > SHARE_TARGET_LIMIT) return { targets: [], error: 'limit' };
    }
    return { targets, error: null };
}

function isShareContact(value: string): boolean {
    const text = value.trim();
    if (text.includes('@')) return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(text) && text.length <= 254;
    if (/[^\d+\-().\s]/.test(text)) return false;
    const digits = text.replace(/\D/g, '');
    return digits.length >= 6 && digits.length <= 20;
}

function shareErrorText(lang: Lang, err: unknown) {
    const raw = message(err).toLowerCase();
    if (raw.includes('cannot share an expert with yourself')) return t(lang, '不能把专家分享给自己。', 'You cannot share an expert with yourself.');
    if (raw.includes('invalid phone number or email')) return t(lang, '请输入有效的手机号或邮箱。', 'Enter a valid phone number or email.');
    if (raw.includes('at least one phone') || raw.includes('at least one')) return t(lang, '请至少填写一个手机号或邮箱。', 'Enter at least one phone number or email.');
    if (raw.includes('too many share targets')) return t(lang, `最多指定 ${SHARE_TARGET_LIMIT} 个用户。`, `You can share with at most ${SHARE_TARGET_LIMIT} users.`);
    return message(err);
}

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
    const [visibility, setVisibility] = useState<'public' | 'private' | 'users'>('public');
    const [shareTargets, setShareTargets] = useState('');
    // A public share opts into industry distribution by default. Keep an
    // author's public preference separate from the effective permission: a
    // private share must submit false, but changing back to public should
    // restore the author's prior choice (or the default), not lose it.
    const [publicDistributionPreference, setPublicDistributionPreference] = useState<boolean | null>(null);
    const platformDistribution = visibility === 'public' && (publicDistributionPreference ?? true);
    const [submitting, setSubmitting] = useState(false);
    // setState does not disable the button until the next render. A second
    // click in that gap would submit the same package again and surface the
    // conflict as a failure after the first request had already succeeded.
    const submittingRef = useRef(false);
    const dialogRef = useRef<HTMLElement | null>(null);
    const closeRef = useRef(onClose);
    closeRef.current = onClose;

    useEffect(() => {
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusTimer = window.setTimeout(() => dialogRef.current?.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), button:not([disabled])')?.focus(), 0);
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopImmediatePropagation();
                // Do not allow an in-flight submission to disappear. The
                // request can still succeed after an impatient close, leaving
                // the user without the success result or a way to retry.
                if (!submittingRef.current) closeRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
            const items = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), textarea:not([disabled])') || []);
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
    }, [submitting]);

    const submit = async () => {
        const parsedPrice = Number(price);
        if (visibility !== 'users' && (!Number.isInteger(parsedPrice) || parsedPrice < 0 || parsedPrice > 999999)) {
            await showAlert(t(lang, '请输入 0 到 999999 之间的整数 Credits。', 'Enter an integer number of Credits between 0 and 999999.'), t(lang, '价格无效', 'Invalid price'));
            return;
        }
        const parsedTargets = visibility === 'users' ? parseShareTargets(shareTargets) : null;
        if (parsedTargets?.error === 'empty') {
            await showAlert(t(lang, '请至少填写一个手机号或邮箱。可以一次分享给多个用户。', 'Enter at least one phone number or email. You can share with more than one user.'), t(lang, '缺少指定用户', 'Recipients required'));
            return;
        }
        if (parsedTargets?.error === 'invalid') {
            await showAlert(t(lang, `“${parsedTargets.invalid}”不是有效的手机号或邮箱。`, `"${parsedTargets.invalid}" is not a valid phone number or email.`), t(lang, '联系方式无效', 'Invalid contact'));
            return;
        }
        if (parsedTargets?.error === 'limit') {
            await showAlert(t(lang, `最多指定 ${SHARE_TARGET_LIMIT} 个用户。`, `You can share with at most ${SHARE_TARGET_LIMIT} users.`), t(lang, '指定用户过多', 'Too many recipients'));
            return;
        }
        if (submittingRef.current) return;
        submittingRef.current = true;
        setSubmitting(true);
        try {
            const listing = visibility === 'users'
                ? await SubmitExpertMarketListingToUsers(expert.id, version.trim() || '1.0.0', parsedTargets?.targets.join('\n') || '')
                : platformDistribution
                ? await SubmitExpertMarketListingWithDistribution(expert.id, version.trim() || '1.0.0', parsedPrice, visibility, true)
                : await SubmitExpertMarketListing(expert.id, version.trim() || '1.0.0', parsedPrice, visibility);
            onSubmitted?.(listing || {});
            onClose();
            if (visibility === 'users') {
                await showAlert(t(lang, '已分享给指定用户。对方用该手机号或邮箱登录后，可在专家市场的“我的库”中安装，不限 Hub 或租户。', 'Shared with the selected users. After they sign in with that phone number or email, they can install it from My library in the AI Expert Market. This is not limited to a Hub or tenant.'), t(lang, '已分享给指定用户', 'Shared with selected users'));
                return;
            }
            if (visibility === 'private') {
                await showAlert(t(lang, '私有分享无需审核，只有上传该专家的用户可以看到。', 'Private sharing skips review and is visible only to the uploader.'), t(lang, '已私有分享', 'Shared privately'));
                return;
            }
            await showAlert(t(lang, '专家已提交审核，审核通过后会出现在市场中。', 'The expert was submitted for review and will appear in the market once approved.'), t(lang, '已提交审核', 'Submitted for review'));
        } catch (err) {
            await showAlert(shareErrorText(lang, err), t(lang, '提交失败', 'Submission failed'));
        } finally {
            submittingRef.current = false;
            setSubmitting(false);
        }
    };

    return <div className="expert-share-overlay" role="presentation">
        <section ref={dialogRef} className="expert-share-shell" role="dialog" aria-modal="true" aria-label={t(lang, '分享 AI 专家', 'Share AI expert')} aria-describedby="expert-share-description">
            <header className="expert-share-header">
                <div><h2>{t(lang, '分享 AI 专家', 'Share AI expert')}</h2><p id="expert-share-description">{visibility === 'public' ? t(lang, '公开分享默认需要 HubCenter 审核，审核通过后会在市场展示。', 'Public sharing is the default and requires HubCenter review before market listing.') : visibility === 'private' ? t(lang, '私有分享无需审核，只有上传该专家的用户可以看到。', 'Private sharing skips review and is visible only to the uploader.') : t(lang, '指定用户无需审核。用手机号或邮箱标识对方，可一次分享给多人，不限 Hub 或租户。', 'Sharing with selected users skips review. Identify each person by phone number or email. You can share with several people, and this is not limited to a Hub or tenant.')}</p></div>
                <button className="expert-share-close" type="button" disabled={submitting} aria-label={t(lang, '关闭', 'Close')} onClick={onClose}>×</button>
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
                    <fieldset className="expert-share-visibility">
                        <legend>{t(lang, '展示范围', 'Visibility')}</legend>
                        <div className="expert-share-visibility__options">
                            <label><input type="radio" name="expert-visibility" disabled={submitting} aria-label={t(lang, '公开（默认，需审核）', 'Public (default, review required)')} checked={visibility === 'public'} onChange={() => setVisibility('public')} /><span><b>{t(lang, '公开', 'Public')}</b><small>{t(lang, '需审核后在市场展示', 'Listed after review')}</small></span></label>
                            <label><input type="radio" name="expert-visibility" disabled={submitting} aria-label={t(lang, '私有（无需审核，仅自己可见）', 'Private (no review, only you can see it)')} checked={visibility === 'private'} onChange={() => setVisibility('private')} /><span><b>{t(lang, '私有', 'Private')}</b><small>{t(lang, '无需审核，仅自己可见', 'No review, visible only to you')}</small></span></label>
                            <label><input type="radio" name="expert-visibility" disabled={submitting} aria-label={t(lang, '指定用户（无需审核，可填写多个手机号或邮箱）', 'Selected users (no review, enter one or more phone numbers or emails)')} checked={visibility === 'users'} onChange={() => setVisibility('users')} /><span><b>{t(lang, '指定用户', 'Selected users')}</b><small>{t(lang, '无需审核，仅这些账号可见', 'No review, visible only to these accounts')}</small></span></label>
                        </div>
                    </fieldset>
                    {visibility === 'users' ? <label className="expert-share-targets">{t(lang, '手机号或邮箱', 'Phone numbers or emails')}<textarea aria-label={t(lang, '指定用户的手机号或邮箱', 'Phone numbers or emails of selected users')} disabled={submitting} value={shareTargets} placeholder={t(lang, '13800138000，name@example.com', '13800138000, name@example.com')} onChange={event => setShareTargets(event.target.value)} /><small>{t(lang, '可填写多个，用逗号或换行分隔。按手机号或邮箱匹配账号，不限 Hub 或租户。', 'Enter more than one, separated by commas or new lines. Accounts are matched by phone number or email, with no Hub or tenant limit.')}</small></label> : null}
                    <label className="expert-share-distribution" title={t(lang, '允许 HubCenter 将审核通过的公开版本获取为行业默认专家资产；不会替用户购买或授予收费专家。', 'Allow HubCenter to acquire an approved public version as an industry-default expert asset. This never purchases or grants a paid expert to users.')}>
                        <input type="checkbox" checked={platformDistribution} disabled={submitting || visibility !== 'public'} onChange={event => setPublicDistributionPreference(event.target.checked)} />
                        <span>{t(lang, '允许平台按行业分发此专家', 'Allow platform distribution by industry')}<small>{t(lang, '仅适用于审核通过的公开版本', 'Available for approved public versions only')}</small></span>
                    </label>
                    <div className="expert-share-meta-fields">
                        <label>{t(lang, '版本', 'Version')}<input aria-label={t(lang, '版本', 'Version')} disabled={submitting} value={version} onChange={event => setVersion(event.target.value)} /></label>
                        <label>{t(lang, '价格（Credits，0 为免费）', 'Price (Credits, 0 is free)')}<input aria-label={t(lang, '价格（Credits，0 为免费）', 'Price (Credits, 0 is free)')} disabled={submitting || visibility === 'users'} inputMode="numeric" value={visibility === 'users' ? '0' : price} onChange={event => setPrice(event.target.value)} /></label>
                    </div>
                </div>
            </main>
            <footer className="expert-share-footer"><button type="button" className="btn-secondary" disabled={submitting} onClick={onClose}>{t(lang, '取消', 'Cancel')}</button><button className="btn-primary" type="button" disabled={submitting} aria-busy={submitting || undefined} onClick={() => void submit()}>{submitting ? t(lang, '提交中…', 'Submitting…') : visibility === 'users' ? t(lang, '分享给指定用户', 'Share with selected users') : t(lang, '提交到 AI 专家市场', 'Submit to AI Expert Market')}</button></footer>
        </section>
    </div>;
}
