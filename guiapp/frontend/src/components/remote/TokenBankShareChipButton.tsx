import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { TokenBankListShareModels, TokenBankListShares } from '../../../wailsjs/go/main/App';
import { TokenBankShareDialog, type TokenBankShareRequest } from '../TokenBankShareDialog';
import { localizeText } from '../../i18n/langSelect';
import { useDialog } from '../CustomDialog';
import {
    displayProviderLabel,
    extractTokenBankModels,
    extractTokenBankShares,
    fingerprintProviderKey,
    sameTokenBankAPI,
} from '../../utils/hubcenterTokenBank';
import { HUB_SERVICE_PROVIDER_NAME, type LLMProvider } from './LLMConfigPanelShared';
import { isLobsterAIProvider, isQoderProvider, isTraeProvider } from './providerLogos';

/**
 * Names whose credential must not be deposited even when a connection test
 * passed. Qoder device tokens are bound to the login machine_id: the refresh
 * rounds live on the depositor's machine, and the copy inside a deposit can
 * silently expire with no server-side re-exchange to save it. Trae (device
 * fingerprint) and LobsterAI (install uuid keyfrom pair) ride the same
 * account-session binding. Both share surfaces (the deposit list and the
 * config-card badge) read this predicate so they can never disagree.
 */
export function isProviderShareExcluded(provider: { name?: unknown }): boolean {
    const name = String(provider.name || '').trim();
    return isQoderProvider(name) || isTraeProvider(name) || isLobsterAIProvider(name);
}

// One confirm and one share dialog at a time. A second row used to replace the
// open confirm, which resolved the first row as cancelled.
let activeDeposit: symbol | null = null;
const depositListeners = new Set<() => void>();

function subscribeDeposit(listener: () => void) {
    depositListeners.add(listener);
    return () => { depositListeners.delete(listener); };
}

function depositHeld() {
    return activeDeposit !== null;
}

function holdDeposit(session: symbol) {
    activeDeposit = session;
    depositListeners.forEach((listener) => listener());
}

function releaseDeposit(session: symbol) {
    if (activeDeposit !== session) return;
    activeDeposit = null;
    depositListeners.forEach((listener) => listener());
}

/** True from the moment a deposit confirm starts until that dialog closes. */
export function useTokenBankDepositBusy(): boolean {
    return useSyncExternalStore(subscribeDeposit, depositHeld, depositHeld);
}

/**
 * The Token Bank entry point on a provider chip (design doc §7.1).
 *
 * Kept as its own component rather than inlined into LLMConfigPanel: that file
 * sits against a hard 1600-line guard, and the §7.1 flow (confirm → probe →
 * submit) is a self-contained unit. The chip only renders a button and hands
 * over this component.
 *
 * The provider key is passed straight through from the caller's own state — this
 * component never copies it into its own state, so a re-render cannot leak it
 * and a devtools snapshot cannot show it.
 */

type TokenBankShareChipButtonProps = {
    lang: string;
    providerName: string;
    apiURL: string;
    apiKey: string;
    protocol: string;
    /** Discovers the provider's model ids. Owned by the caller. */
    listModels: () => Promise<string[]>;
    /** Probes one model. Owned by the caller. */
    probeModel: (model: string) => Promise<{ ok: boolean; latencyMs?: number; error?: string }>;
    onShared?: (shareID: string) => void;
    showToast?: (message: string) => void;
    onRequestVerification?: () => void;
    /** `chip` is the small grid mark. `badge` is the labeled mark on the config title. `deposit` is the text button on the Token Bank provider list. */
    appearance?: 'chip' | 'badge' | 'deposit';
};

export function TokenBankShareChipButton({
    lang,
    providerName,
    apiURL,
    apiKey,
    protocol,
    listModels,
    probeModel,
    onShared,
    showToast,
    onRequestVerification,
    appearance = 'chip',
}: TokenBankShareChipButtonProps) {
    const t = useCallback(
        (en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans),
        [lang],
    );
    const { showAlert, showConfirm } = useDialog();
    const [request, setRequest] = useState<TokenBankShareRequest | null>(null);
    const [alreadyShared, setAlreadyShared] = useState<string[]>([]);
    const [preparing, setPreparing] = useState(false);
    const preparingRef = useRef(false);
    const mountedRef = useRef(true);
    const buttonRef = useRef<HTMLButtonElement>(null);
    const returnFocusTimer = useRef<number | null>(null);
    const sessionRef = useRef<symbol | null>(null);
    if (!sessionRef.current) sessionRef.current = Symbol();
    const depositBusy = useTokenBankDepositBusy();

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            if (returnFocusTimer.current !== null) window.clearTimeout(returnFocusTimer.current);
            if (sessionRef.current) releaseDeposit(sessionRef.current);
        };
    }, []);

    const closeShareDialog = () => {
        setRequest(null);
        if (sessionRef.current) releaseDeposit(sessionRef.current);
        // Opening the dialog moved focus onto its backdrop and disabled this
        // button. Put focus back once the button is enabled again.
        const button = buttonRef.current;
        if (returnFocusTimer.current !== null) window.clearTimeout(returnFocusTimer.current);
        returnFocusTimer.current = window.setTimeout(() => {
            returnFocusTimer.current = null;
            if (!button?.isConnected) return;
            // Verification opens its own screen from this same close. If that
            // screen already took focus, leave it there.
            const active = document.activeElement;
            if (active && active !== document.body && active !== document.documentElement && active.isConnected) return;
            button.focus();
        }, 0);
    };

    const start = async () => {
        const session = sessionRef.current;
        // The confirm dialog is async. A second click, including one on another
        // provider row, would replace that confirm and cancel the first share.
        if (!session || preparingRef.current || activeDeposit) return;
        preparingRef.current = true;
        holdDeposit(session);
        setPreparing(true);
        let opened = false;
        try {
            if (!apiKey.trim()) {
                await showAlert(t('Add the provider key first.', '请先填写该服务商的密钥。', '請先填寫該服務商的密鑰。'));
                return;
            }
            // Confirm before touching the network: the message explains what the
            // bank does with the credential, and the §12 sentence is included so
            // nobody expects to cash credits out.
            const confirmed = await showConfirm(
                t(
                    `Token Bank will save this provider's access information to the MaClaw server (over a secure connection).\n` +
                        `When other users use this provider and consume tokens, the credits are credited to your account.\n` +
                        `Different credit amounts are earned per model tier. Credits never expire.\n\n` +
                        `After you pull them onto this machine, the assistant spends them as service-group quota. They cannot buy compute cards and cannot be cashed out as fiat.`,
                    `Token 银行功能将把当前服务商的访问信息保存到 MaClaw 服务器（安全传输）。\n` +
                        `其他用户访问该服务商并消耗 token 后，将存到您的账户。\n` +
                        `根据模型档次获得不同的积分，积分终身有效，今后将可随时消耗使用。\n\n` +
                        `提取到本机后由助手消耗服务组额度。不能购买算力卡，不可提现，不可转让为人民币。`,
                    `Token 銀行功能將把當前服務商的訪問資訊保存到 MaClaw 伺服器（安全傳輸）。\n` +
                        `其他使用者訪問該服務商並消耗 token 後，將存到您的帳戶。\n` +
                        `根據模型檔次獲得不同的積分，積分終身有效，今後將可隨時消耗使用。\n\n` +
                        `提取到本機後由助手消耗服務組額度。不能購買算力卡，不可提現，不可轉讓為人民幣。`,
                ),
                t('Token Bank', 'Token 银行', 'Token 銀行'),
                { confirmText: t('Confirm share', '确认分享', '確認分享') },
            );
            // Leaving the page unmounts this button. The confirm can still be
            // answered afterwards; do not fetch models or open a dialog for it.
            if (!confirmed || !mountedRef.current) return;

            // Resolve the fingerprint before opening the dialog so a SubtleCrypto
            // failure surfaces here rather than mid-submit.
            await fingerprintProviderKey(apiKey, apiURL);
            if (!mountedRef.current) return;
            const shared = await alreadySharedModelNames(apiURL);
            if (!mountedRef.current) return;
            const models = await listModels();
            if (!mountedRef.current) return;
            if (models.length === 0) {
                await showAlert(
                    t('No models were found for this provider.', '未发现该服务商下的模型。', '未發現該服務商下的模型。'),
                    t('Token Bank', 'Token 银行', 'Token 銀行'),
                );
                return;
            }
            setAlreadyShared(shared);
            setRequest({ providerName, apiURL, apiKey, protocol, models });
            opened = true;
        } catch (error) {
            if (!mountedRef.current) return;
            const message = error instanceof Error ? error.message : String(error);
            await showAlert(message, t('Token Bank', 'Token 银行', 'Token 銀行'));
        } finally {
            preparingRef.current = false;
            setPreparing(false);
            if (!opened) releaseDeposit(session);
        }
    };

    const badge = appearance === 'badge';
    const deposit = appearance === 'deposit';
    const label = deposit
        ? t(
            `Deposit ${providerName} into Token Bank`,
            `将${providerName}存入银行`,
            `將${providerName}存入銀行`,
        )
        : badge
            ? t('Deposit this provider into Token Bank', '将此服务商存入 Token 银行', '將此服務商存入 Token 銀行')
            : t('Share this provider to Token Bank', '分享此服务商到 Token 银行', '分享此服務商到 Token 銀行');

    return (
        <>
            <button
                ref={buttonRef}
                className={deposit ? 'btn-primary tbk-provider-pick__deposit' : badge ? 'llm-config-tokenbank-badge' : 'llm-config-tokenbank-btn'}
                type="button"
                aria-label={label}
                title={deposit ? undefined : displayProviderLabel(t('Token Bank', 'Token 银行', 'Token 銀行'), providerName)}
                disabled={preparing || depositBusy}
                onClick={(event) => {
                    // The chip itself is a button that selects the provider;
                    // this nested button must not also select it.
                    event.stopPropagation();
                    void start();
                }}
            >
                {deposit ? null : <TokenBankDepositIcon size={badge ? 16 : 14} />}
                {deposit ? <span>{preparing ? t('Please wait', '请稍候', '請稍候') : t('Deposit', '存入银行', '存入銀行')}</span> : null}
                {badge ? <span>{t('Token Bank', 'Token 银行', 'Token 銀行')}</span> : null}
            </button>
            {request ? (
                <TokenBankShareDialog
                    lang={lang}
                    request={request}
                    probe={probeModel}
                    alreadySharedModels={alreadyShared}
                    onClose={closeShareDialog}
                    onRequestVerification={onRequestVerification}
                    onShared={(shareID: string) => {
                        onShared?.(shareID);
                        showToast?.(
                            t(
                                'Provider shared. Credits from other users will arrive in your account.',
                                '分享成功。其他用户消耗产生的积分会进入您的账户。',
                                '分享成功。其他使用者消耗產生的積分会進入您的帳戶。',
                            ),
                        );
                    }}
                />
            ) : null}
        </>
    );
}

async function alreadySharedModelNames(apiURL: string): Promise<string[]> {
    try {
        const shares = extractTokenBankShares(await TokenBankListShares('all')).filter(
            (share) => share.status !== 'revoked' && sameTokenBankAPI(share.api_url, apiURL),
        );
        const names: string[] = [];
        for (const share of shares) {
            const models = extractTokenBankModels(await TokenBankListShareModels(share.id, 'all'));
            for (const model of models) {
                if (model.model_name) names.push(model.model_name);
            }
        }
        return names;
    } catch {
        return [];
    }
}

/**
 * Monoline deposit mark, same 1.5 stroke as the sidebar icons.
 * A token sits above a bank pediment; the chevron points into the doorway.
 */
export function TokenBankDepositIcon({ size = 16 }: { size?: number }) {
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true" focusable="false" data-icon="token-bank-deposit">
            <g data-part="token" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
                <circle cx="12" cy="4.95" r="1.7" />
                <path d="M10.95 4.95h2.1" />
            </g>
            <path data-part="arrow" d="M12 7.4v.9M10.95 7.7 12 8.85 13.05 7.7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            <g data-part="bank" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3.75 13.2 12 9.85 20.25 13.2" />
                <path d="M6.85 13.2v5.85M17.15 13.2v5.85" />
                <path d="M3.75 20.1h16.5" />
            </g>
        </svg>
    );
}

/** Title row of a provider config card. A tested, non-hub provider gets the deposit badge. */
export function LLMConfigProviderShareHeading({
    lang,
    provider,
    listModels,
    probeModel,
    onRequestVerification,
}: {
    lang: string;
    provider: LLMProvider;
    listModels: () => Promise<string[]>;
    probeModel: (model: string) => Promise<{ ok: boolean; latencyMs?: number; error?: string }>;
    onRequestVerification?: () => void;
}) {
    const t = useCallback(
        (en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans),
        [lang],
    );
    const title = provider.import_source
        ? `${provider.name} ${t('(imported)', '（已导入）')}`
        : provider.is_custom
            ? t('Custom Provider Configuration', '自定义服务商配置')
            : `${provider.name} ${t('Configuration', '配置')}`;
    const shareable = provider.connection_test_passed === true
        && provider.is_hub_service !== true
        && provider.name !== HUB_SERVICE_PROVIDER_NAME
        && !isProviderShareExcluded(provider);

    return (
        <div className="llm-config-form-card__heading">
            <div className="llm-config-form-card__title">{title}</div>
            {shareable ? (
                <TokenBankShareChipButton
                    key={provider.id || provider.name}
                    appearance="badge"
                    lang={lang}
                    providerName={provider.name}
                    apiURL={provider.url}
                    apiKey={provider.key}
                    protocol={provider.protocol ?? ''}
                    listModels={listModels}
                    probeModel={probeModel}
                    onRequestVerification={onRequestVerification}
                />
            ) : null}
        </div>
    );
}
