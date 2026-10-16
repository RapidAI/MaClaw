import { cloneElement, isValidElement, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { GetMaclawLLMProviders } from '../../wailsjs/go/main/App';
import { localizeText } from '../i18n/langSelect';
import { tokenBankDisplayURL } from '../utils/hubcenterTokenBank';
import { TokenBankShareChipButton, useTokenBankDepositBusy } from './remote/TokenBankShareChipButton';
import { PROVIDER_LOGOS, isLobsterAIProvider, isQoderProvider, isTraeProvider } from './remote/providerLogos';
import {
    HUB_SERVICE_PROVIDER_NAME,
    listProviderModelsForShare,
    probeProviderModelForShare,
    type LLMProvider,
} from './remote/LLMConfigPanelShared';

type TokenBankShareableProvidersProps = {
    lang: string;
    /** Bumped by the panel after each Token Bank refresh, including the first load. */
    reloadToken: number;
    showToastMessage?: (message: string) => void;
    onRequestVerification?: () => void;
    onShared?: () => void;
};

const COPILOT_PROVIDER_NAME = 'GitHub Copilot';
const LIST_UNAVAILABLE = 'provider-list-unavailable';
const LOGO_KEYS = Object.keys(PROVIDER_LOGOS).sort((a, b) => b.length - a.length);

/**
 * A saved provider can be deposited only after it is configured and its own
 * connection test passed, and never when it is the platform's MaClaw Official
 * service. Configured means a name, an endpoint, and a credential the share
 * dialog can publish. An OAuth token counts; GitHub Copilot's GitHub token
 * does not.
 */
export function isTokenBankShareableProvider(provider: LLMProvider): boolean {
    const name = String(provider.name || '').trim();
    if (!name || !String(provider.url || '').trim()) return false;
    if (!tokenBankShareCredential(provider)) return false;
    if (provider.connection_test_passed !== true) return false;
    if (provider.is_hub_service === true) return false;
    if (name === HUB_SERVICE_PROVIDER_NAME) return false;
    return true;
}

/**
 * Credential the share dialog will publish.
 * Prefer the saved API key. OAuth providers often keep the usable token in
 * oauth_access_token instead. GitHub Copilot's oauth field is the long-lived
 * GitHub token, not the Copilot API token, so it is not a share secret.
 */
export function tokenBankShareCredential(provider: LLMProvider): string {
    const name = String(provider.name || '').trim();
    // Qoder device tokens are machine-bound (the refresh grants live on the
    // depositor's machine), so a deposited copy cannot stay usable. Trae's
    // device fingerprint and LobsterAI's install uuid ride the account
    // session the same way. The badge gate reads the same rule via
    // isProviderShareExcluded.
    if (isQoderProvider(name) || isTraeProvider(name) || isLobsterAIProvider(name)) return '';
    const key = String(provider.key || '').trim();
    if (key) return key;
    if (name === COPILOT_PROVIDER_NAME) return '';
    return String(provider.oauth_access_token || '').trim();
}

function logoBoundary(name: string, index: number): boolean {
    if (index >= name.length) return true;
    const ch = name[index];
    return ch === ' ' || ch === '(' || ch === '（' || ch === '-' || ch === '/';
}

function providerLogo(name: string) {
    const trimmed = name.trim();
    if (PROVIDER_LOGOS[trimmed]) return PROVIDER_LOGOS[trimmed];
    // "Kimi (月之暗面)" and "OpenAI Official" reuse the brand mark. A substring
    // such as "NotKimi" must not.
    for (const key of LOGO_KEYS) {
        if (trimmed.startsWith(key) && logoBoundary(trimmed, key.length)) return PROVIDER_LOGOS[key];
    }
    return null;
}

/**
 * PROVIDER_LOGOS stores one element per brand. The closed face and every open
 * row render at the same time, and WorkBuddy's two rows must not share a node,
 * so each mark clones the element before mounting it.
 */
function ProviderMark({ name }: { name: string }) {
    const logo = providerLogo(name);
    const glyph = isValidElement(logo) ? cloneElement(logo) : null;
    const letter = Array.from(name)[0] || '?';
    return (
        <span className={`tbk-provider-pick__mark${glyph ? '' : ' tbk-provider-pick__mark--fallback'}`} aria-hidden="true">
            {glyph ?? <span className="tbk-provider-pick__monogram">{letter}</span>}
        </span>
    );
}

type ProviderMenuAnchor = {
    left: number;
    top: number;
    bottom: number;
    width: number;
};

type ProviderMenuBox = {
    left: number;
    width: number;
    minWidth: number;
    maxWidth: number;
    maxHeight: number;
    top: number | 'auto';
    bottom: number | 'auto';
};

/**
 * The open list is as wide as its longest label, and at least as wide as the
 * closed control, then shifts left so that width stays inside the viewport.
 * widthCap is the control's own max-width. The floor stops there, so a
 * flex-grown anchor cannot paint a pane-wide list. Before the first measure
 * and without a cap, the anchor width is only a stand-in so the frame is not
 * zero. The far edge keeps the same 8px margin. A fixed minimum height would
 * paint past that edge when the window is short.
 */
export function placeProviderMenu(
    anchor: ProviderMenuAnchor,
    menuWidth: number,
    viewportWidth: number,
    viewportHeight: number,
    widthCap = 0,
): ProviderMenuBox {
    const gap = 4;
    const margin = 8;
    const maxWidth = Math.max(0, viewportWidth - margin * 2);
    const anchorWidth = Math.max(0, Math.min(Math.max(0, anchor.width), maxWidth));
    const measured = Math.max(0, menuWidth);
    const cap = widthCap > 0 ? Math.min(widthCap, maxWidth) : 0;
    const floor = cap > 0 ? Math.min(anchorWidth, cap) : (measured > 0 ? 0 : anchorWidth);
    const width = Math.min(Math.max(measured, floor), maxWidth);
    const minWidth = width;
    const spaceBelow = Math.max(0, viewportHeight - anchor.bottom - gap - margin);
    const spaceAbove = Math.max(0, anchor.top - gap - margin);
    const openUp = spaceBelow < 160 && spaceAbove > spaceBelow;
    const available = openUp ? spaceAbove : spaceBelow;
    return {
        left: Math.max(margin, Math.min(anchor.left, viewportWidth - width - margin)),
        width,
        minWidth,
        maxWidth,
        maxHeight: Math.min(360, available),
        top: openUp ? 'auto' : anchor.bottom + gap,
        bottom: openUp ? viewportHeight - anchor.top + gap : 'auto',
    };
}

function sameProviderMenuBox(current: ProviderMenuBox | null, next: ProviderMenuBox): boolean {
    return !!current
        && current.left === next.left
        && current.width === next.width
        && current.minWidth === next.minWidth
        && current.maxWidth === next.maxWidth
        && current.maxHeight === next.maxHeight
        && current.top === next.top
        && current.bottom === next.bottom;
}

/** Used max-width of the closed control, in pixels. "none" or an unparsed
 *  min() leaves 0, and the menu then follows the measured labels alone. */
export function controlWidthCap(anchor: HTMLElement): number {
    const parsed = Number.parseFloat(getComputedStyle(anchor).maxWidth);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

/** A vertical scrollbar sits inside the border box and would ellipsize the
 *  last letters. Overlay scrollbars report a zero gutter and need nothing. */
export function fitMenuWidth(
    content: number,
    scrollHeight: number,
    clientHeight: number,
    offsetWidth: number,
    clientWidth: number,
    border: number,
): number {
    if (!(content > 0)) return 0;
    if (scrollHeight <= clientHeight + 1) return content;
    return content + Math.max(0, offsetWidth - clientWidth - border);
}

/** Text width of the widest row, plus the mark, padding, and border.
 *  The label is stretched to the menu, so its box width is not the text. */
function measuredMenuWidth(list: HTMLElement): number {
    const labels = list.querySelectorAll<HTMLElement>('.tbk-provider-pick__option-label');
    if (labels.length === 0) return 0;
    let text = 0;
    const range = document.createRange();
    const measure = typeof range.getBoundingClientRect === 'function'
        ? () => range.getBoundingClientRect().width
        : () => 0;
    for (const label of labels) {
        range.selectNodeContents(label);
        const width = measure();
        if (Number.isFinite(width)) text = Math.max(text, width);
    }
    if (!(text > 0)) return 0;
    const option = labels[0].closest<HTMLElement>('.tbk-provider-pick__option');
    const mark = option?.querySelector<HTMLElement>('.tbk-provider-pick__mark');
    const optionStyle = option ? getComputedStyle(option) : null;
    const listStyle = getComputedStyle(list);
    const px = (value: string | undefined) => {
        const parsed = Number.parseFloat(value ?? '');
        return Number.isFinite(parsed) ? parsed : 0;
    };
    const gap = optionStyle && optionStyle.columnGap !== 'normal'
        ? px(optionStyle.columnGap)
        : px(optionStyle?.gap);
    const border = px(listStyle.borderLeftWidth) + px(listStyle.borderRightWidth);
    const chrome = (mark?.getBoundingClientRect().width ?? 0)
        + gap
        + px(optionStyle?.paddingLeft)
        + px(optionStyle?.paddingRight)
        + px(listStyle.paddingLeft)
        + px(listStyle.paddingRight)
        + border;
    return fitMenuWidth(
        Math.ceil(text + chrome),
        list.scrollHeight,
        list.clientHeight,
        list.offsetWidth,
        list.clientWidth,
        border,
    );
}

function endpointHint(url: string, withScheme: boolean): string {
    const trimmed = url.trim();
    try {
        const parsed = new URL(trimmed);
        const path = parsed.pathname.replace(/\/+$/, '');
        if (!parsed.host) return tokenBankDisplayURL(trimmed);
        const hostPath = path ? `${parsed.host}${path}` : parsed.host;
        return withScheme ? `${parsed.protocol}//${hostPath}` : hostPath;
    } catch {
        return tokenBankDisplayURL(trimmed);
    }
}

function labelCounts(labels: string[]): Map<string, number> {
    const counts = new Map<string, number>();
    for (const label of labels) counts.set(label, (counts.get(label) ?? 0) + 1);
    return counts;
}

function upgradeCrowded(current: string[], next: string[]): string[] {
    const counts = labelCounts(current);
    let crowded = false;
    for (const count of counts.values()) {
        if (count > 1) crowded = true;
    }
    if (!crowded) return current;
    return current.map((label, index) => ((counts.get(label) ?? 0) > 1 ? next[index] : label));
}

function optionLabels(providers: LLMProvider[]): string[] {
    const names = providers.map((item) => String(item?.name || '').trim());
    const nameCount = labelCounts(names);
    const build = (withScheme: boolean) => names.map((name, index) => {
        if ((nameCount.get(name) ?? 0) < 2) return name;
        const hint = endpointHint(String(providers[index]?.url || ''), withScheme);
        return hint ? `${name} · ${hint}` : name;
    });
    // Host and path are enough when the endpoints differ. The same host can
    // still be http and https, or differ only by a query. The redacted URL
    // keeps that query and drops a password. Whatever is still identical gets
    // a number, which is not a secret.
    let labels = upgradeCrowded(build(false), build(true));
    const displayed = names.map((name, index) => {
        if ((nameCount.get(name) ?? 0) < 2) return name;
        const hint = tokenBankDisplayURL(String(providers[index]?.url || ''));
        return hint ? `${name} · ${hint}` : name;
    });
    labels = upgradeCrowded(labels, displayed);
    const counts = labelCounts(labels);
    if (![...counts.values()].some((count) => count > 1)) return labels;
    const seen = new Map<string, number>();
    return labels.map((label) => {
        if ((counts.get(label) ?? 0) < 2) return label;
        const n = (seen.get(label) ?? 0) + 1;
        seen.set(label, n);
        return `${label} · ${n}`;
    });
}

function providerRowKey(providers: LLMProvider[], index: number): string {
    const provider = providers[index];
    const id = String(provider?.id || '').trim();
    // Index used to be part of every fallback key, so a refresh that inserted a
    // provider above the selection moved the key and jumped back to the first row.
    const base = id || `${String(provider?.name || '').trim()}\n${String(provider?.url || '').trim()}`;
    let matches = 0;
    for (const item of providers) {
        const itemID = String(item?.id || '').trim();
        const itemBase = itemID || `${String(item?.name || '').trim()}\n${String(item?.url || '').trim()}`;
        if (itemBase === base) matches += 1;
    }
    return matches > 1 ? `${base}#${index}` : base;
}

export function TokenBankShareableProviders({
    lang,
    reloadToken,
    showToastMessage,
    onRequestVerification,
    onShared,
}: TokenBankShareableProvidersProps) {
    const t = useCallback(
        (en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans),
        [lang],
    );
    const [providers, setProviders] = useState<LLMProvider[] | null>(null);
    const [error, setError] = useState('');

    useEffect(() => {
        if (!reloadToken) return;
        let cancelled = false;
        void (async () => {
            try {
                const data = await GetMaclawLLMProviders() as { providers?: unknown } | null;
                if (!data || !Array.isArray(data.providers)) {
                    if (!cancelled) {
                        setError(LIST_UNAVAILABLE);
                        setProviders((current) => current ?? []);
                    }
                    return;
                }
                if (!cancelled) {
                    setProviders(data.providers as LLMProvider[]);
                    setError('');
                }
            } catch (err) {
                if (cancelled) return;
                setError(err instanceof Error ? err.message : String(err));
                setProviders((current) => current ?? []);
            }
        })();
        return () => { cancelled = true; };
    }, [reloadToken]);

    const shareable = useMemo(
        () => (providers ?? []).filter(isTokenBankShareableProvider),
        [providers],
    );
    const [selectedKey, setSelectedKey] = useState('');
    const resolvedIndex = shareable.findIndex((_, index) => providerRowKey(shareable, index) === selectedKey);
    const activeIndex = resolvedIndex >= 0 ? resolvedIndex : 0;
    const selected = shareable[activeIndex];
    const shownError = error === LIST_UNAVAILABLE
        ? t('Provider list is unavailable.', '服务商列表不可用。', '服務商列表不可用。')
        : error;

    return (
        <section className="tbk-section" data-testid="tbk-shareable-providers">
            <h4>{t('Providers you can share', '可分享的服务商', '可分享的服務商')}</h4>
            {shownError ? <div className="tbk-error" role="alert">{shownError}</div> : null}
            {providers === null ? (
                <div className="tbk-empty">{t('Loading…', '加载中…', '載入中…')}</div>
            ) : shareable.length === 0 || !selected ? (
                shownError ? null : (
                    <div className="tbk-empty">
                        {t(
                            'No provider has passed its connection test yet. Test one in LLM settings first.',
                            '还没有测试可用的服务商。请先在大模型配置里完成连接测试。',
                            '還沒有測試可用的服務商。請先在大模型配置裡完成連接測試。',
                        )}
                    </div>
                )
            ) : (
                <ShareableProviderPick
                    lang={lang}
                    t={t}
                    providers={shareable}
                    provider={selected}
                    providerKey={providerRowKey(shareable, activeIndex)}
                    onSelect={setSelectedKey}
                    showToastMessage={showToastMessage}
                    onRequestVerification={onRequestVerification}
                    onShared={onShared}
                />
            )}
        </section>
    );
}

function ShareableProviderPick({
    lang,
    t,
    providers,
    provider,
    providerKey,
    onSelect,
    showToastMessage,
    onRequestVerification,
    onShared,
}: {
    lang: string;
    t: (en: string, zhHans: string, zhHant?: string) => string;
    providers: LLMProvider[];
    provider: LLMProvider;
    providerKey: string;
    onSelect: (key: string) => void;
    showToastMessage?: (message: string) => void;
    onRequestVerification?: () => void;
    onShared?: () => void;
}) {
    const depositBusy = useTokenBankDepositBusy();
    const name = provider.name.trim();
    const apiURL = provider.url.trim();
    const protocol = String(provider.protocol || '').trim();
    const credential = tokenBankShareCredential(provider);
    const shareProvider = { ...provider, name, url: apiURL, key: credential, protocol };
    const labels = optionLabels(providers);
    const selectedIndex = providers.findIndex((_, index) => providerRowKey(providers, index) === providerKey);
    const selectedLabel = labels[selectedIndex] ?? name;
    const listId = useId();
    const fieldId = `${listId}-field`;
    const valueId = `${listId}-value`;
    const controlRef = useRef<HTMLDivElement>(null);
    const buttonRef = useRef<HTMLButtonElement>(null);
    const [open, setOpen] = useState(false);
    const [activeIndex, setActiveIndex] = useState(0);
    const [menuBox, setMenuBox] = useState<ProviderMenuBox | null>(null);
    const typedAhead = useRef({ text: '', at: 0 });
    const roster = providers.map((_, index) => providerRowKey(providers, index)).join('\0');
    const highlight = providers.length === 0 ? 0 : Math.min(Math.max(activeIndex, 0), providers.length - 1);
    const depositLabel = t('Provider to deposit', '要存入的服务商', '要存入的服務商');
    // Close before paint. The confirm dialog sits at z-index 2000; leaving the
    // portaled list up until an effect would cover that dialog for a frame.
    if (depositBusy && open) setOpen(false);
    else if (!open && menuBox) setMenuBox(null);

    const openAt = (index: number) => {
        if (depositBusy || providers.length === 0) return;
        // Arrow and Home/End move the highlight. That is a new search, so a
        // letter typed just before must not stay in the prefix.
        typedAhead.current = { text: '', at: 0 };
        const count = providers.length;
        const next = ((index % count) + count) % count;
        setActiveIndex(next);
        setOpen(true);
    };
    const choose = (index: number) => {
        // The chip is keyed by provider. Changing it unmounts an open confirm,
        // and the global dialog would then apply to nobody.
        if (depositBusy || index < 0 || index >= providers.length) return;
        onSelect(providerRowKey(providers, index));
        setOpen(false);
    };

    useEffect(() => {
        setOpen(false);
    }, [roster]);

    // Opening or closing starts a new typeahead. A click between two letters
    // must not search for the concatenation of both.
    useEffect(() => {
        typedAhead.current = { text: '', at: 0 };
    }, [open]);

    useEffect(() => {
        if (!open) return;
        const onPointerDown = (event: PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node)) return;
            if (controlRef.current?.contains(target)) return;
            const list = document.getElementById(listId);
            if (list?.contains(target)) return;
            setOpen(false);
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            event.preventDefault();
            event.stopPropagation();
            setOpen(false);
            buttonRef.current?.focus();
        };
        document.addEventListener('pointerdown', onPointerDown);
        document.addEventListener('keydown', onKeyDown);
        return () => {
            document.removeEventListener('pointerdown', onPointerDown);
            document.removeEventListener('keydown', onKeyDown);
        };
    }, [open, listId]);

    const menuReady = open && menuBox !== null;
    useLayoutEffect(() => {
        if (!open) return;
        const place = () => {
            const anchor = controlRef.current;
            if (!anchor) return;
            const rect = anchor.getBoundingClientRect();
            const list = document.getElementById(listId);
            const measured = list ? measuredMenuWidth(list) : 0;
            const next = placeProviderMenu(
                { left: rect.left, top: rect.top, bottom: rect.bottom, width: rect.width },
                measured,
                window.innerWidth,
                window.innerHeight,
                controlWidthCap(anchor),
            );
            setMenuBox((current) => (sameProviderMenuBox(current, next) ? current : next));
        };
        place();
        let frame = 0;
        const onMove = (event: Event) => {
            const list = document.getElementById(listId);
            if (list && event.target instanceof Node && (event.target === list || list.contains(event.target))) return;
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(place);
        };
        window.addEventListener('resize', onMove);
        window.addEventListener('scroll', onMove, true);
        // The deposit label changes width with the provider. Scroll does not
        // fire for that, and the fixed menu would stay on the old box.
        const observed = controlRef.current;
        let observer: ResizeObserver | null = null;
        if (typeof ResizeObserver !== 'undefined' && observed) {
            observer = new ResizeObserver(() => place());
            observer.observe(observed);
        }
        return () => {
            cancelAnimationFrame(frame);
            observer?.disconnect();
            window.removeEventListener('resize', onMove);
            window.removeEventListener('scroll', onMove, true);
        };
    }, [open, menuReady, listId, providers.length, providerKey]);

    const scrolledOption = useRef('');
    useEffect(() => {
        if (!open || !menuBox) {
            scrolledOption.current = '';
            return;
        }
        const token = `${listId}:${highlight}`;
        if (scrolledOption.current === token) return;
        scrolledOption.current = token;
        const option = document.getElementById(`${listId}-opt-${highlight}`);
        const list = document.getElementById(listId);
        if (!option || !list) return;
        const top = option.offsetTop;
        const bottom = top + option.offsetHeight;
        if (top < list.scrollTop) list.scrollTop = top;
        else if (bottom > list.scrollTop + list.clientHeight) list.scrollTop = bottom - list.clientHeight;
    }, [open, menuBox, listId, highlight]);

    const menu = open && menuBox && typeof document !== 'undefined'
        ? createPortal(
            <div
                id={listId}
                role="listbox"
                aria-label={depositLabel}
                className="tbk-provider-pick__menu"
                style={{
                    position: 'fixed',
                    left: menuBox.left,
                    top: menuBox.top,
                    bottom: menuBox.bottom,
                    width: menuBox.width,
                    minWidth: menuBox.minWidth,
                    maxWidth: menuBox.maxWidth,
                    maxHeight: menuBox.maxHeight,
                }}
                onMouseDown={(event) => {
                    if (event.target === event.currentTarget) return;
                    event.preventDefault();
                }}
            >
                {providers.map((item, index) => {
                    const rowName = String(item.name || '').trim();
                    const active = index === highlight;
                    return (
                        <button
                            key={providerRowKey(providers, index)}
                            id={`${listId}-opt-${index}`}
                            type="button"
                            role="option"
                            tabIndex={-1}
                            aria-selected={index === selectedIndex}
                            className={`tbk-provider-pick__option${active ? ' tbk-provider-pick__option--active' : ''}`}
                            title={labels[index] !== rowName ? labels[index] : undefined}
                            onMouseEnter={() => setActiveIndex(index)}
                            onClick={() => choose(index)}
                        >
                            <ProviderMark name={rowName} />
                            <span className="tbk-provider-pick__option-label">{labels[index]}</span>
                        </button>
                    );
                })}
            </div>,
            document.body,
        )
        : null;

    return (
        <div className="tbk-provider-pick">
            <div className="tbk-provider-pick__control" ref={controlRef}>
                <button
                    ref={buttonRef}
                    type="button"
                    className="tbk-provider-pick__face"
                    role="combobox"
                    aria-labelledby={`${fieldId} ${valueId}`}
                    aria-haspopup="listbox"
                    aria-expanded={open}
                    aria-controls={listId}
                    aria-activedescendant={open && menuBox ? `${listId}-opt-${highlight}` : undefined}
                    title={tokenBankDisplayURL(apiURL)}
                    disabled={depositBusy}
                    onBlur={(event) => {
                        const next = event.relatedTarget;
                        if (next instanceof Node) {
                            if (controlRef.current?.contains(next)) return;
                            const list = document.getElementById(listId);
                            if (list?.contains(next)) return;
                        }
                        setOpen(false);
                    }}
                    onClick={() => {
                        if (depositBusy) return;
                        if (open) {
                            setOpen(false);
                            return;
                        }
                        openAt(selectedIndex >= 0 ? selectedIndex : 0);
                    }}
                    onKeyDown={(event) => {
                        if (depositBusy) return;
                        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                            event.preventDefault();
                            if (!open) {
                                openAt(selectedIndex >= 0 ? selectedIndex : 0);
                                return;
                            }
                            const delta = event.key === 'ArrowDown' ? 1 : -1;
                            openAt(highlight + delta);
                            return;
                        }
                        if (event.key === 'Home' || event.key === 'End') {
                            event.preventDefault();
                            openAt(event.key === 'Home' ? 0 : providers.length - 1);
                            return;
                        }
                        if ((event.key === 'Enter' || event.key === ' ') && open) {
                            event.preventDefault();
                            choose(highlight);
                            return;
                        }
                        if (event.key === 'Escape' && open) {
                            event.preventDefault();
                            event.stopPropagation();
                            setOpen(false);
                            return;
                        }
                        // The closed control used to be a native select, which jumps
                        // to a name as the user types. One letter cycles; a quick
                        // second letter narrows the prefix. Space activates the
                        // button, so it must not become part of that prefix.
                        if (event.key === ' ') return;
                        if (event.key.length !== 1 || event.altKey || event.ctrlKey || event.metaKey) return;
                        if (event.nativeEvent.isComposing || event.key === 'Process') return;
                        const now = Date.now();
                        const fresh = now - typedAhead.current.at > 700;
                        const prefix = `${fresh ? '' : typedAhead.current.text}${event.key.toLocaleLowerCase()}`;
                        typedAhead.current = { text: prefix, at: now };
                        const count = labels.length;
                        if (count === 0) return;
                        const origin = open ? highlight : Math.max(selectedIndex, 0);
                        const begin = prefix.length === 1 ? origin + 1 : origin;
                        for (let step = 0; step < count; step += 1) {
                            const index = (begin + step) % count;
                            if (!(labels[index] || '').toLocaleLowerCase().startsWith(prefix)) continue;
                            event.preventDefault();
                            setActiveIndex(index);
                            onSelect(providerRowKey(providers, index));
                            return;
                        }
                    }}
                >
                    <span id={fieldId} className="tbk-provider-pick__field">{depositLabel}</span>
                    <ProviderMark name={name} />
                    <span id={valueId} className="tbk-provider-pick__label">{selectedLabel}</span>
                    <span className={`tbk-provider-pick__chevron${open ? ' tbk-provider-pick__chevron--open' : ''}`} aria-hidden="true" />
                </button>
            </div>
            {menu}
            <TokenBankShareChipButton
                key={providerKey}
                appearance="deposit"
                lang={lang}
                providerName={name}
                apiURL={apiURL}
                apiKey={credential}
                protocol={protocol}
                listModels={() => listProviderModelsForShare(shareProvider)}
                probeModel={(model) => probeProviderModelForShare(shareProvider, model, t)}
                showToast={showToastMessage}
                onRequestVerification={onRequestVerification}
                onShared={() => onShared?.()}
            />
        </div>
    );
}
