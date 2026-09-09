// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { SidebarSystemStatus } from '../SidebarSystemStatus';
import type { SidebarHubCredits } from '../../../types/appShell';

const baseCredits: SidebarHubCredits = {
    authorized: true,
    total: 100,
    used: 10,
    remaining: 90,
    available: 90,
    showPeriodAvailable: false,
    tokensPerCredit: 0,
    expiresAt: '2026-05-06T00:00:00Z',
    unlimited: false,
    status: '',
    retryAfterSeconds: 0,
    retryAfterAt: '',
};

function renderStatus(credits: SidebarHubCredits, options: { showHubCreditAction?: boolean; isHubService?: boolean; onOpenBackgroundTasks?: () => void } = {}) {
    const openServiceRedeemPage = vi.fn();
    const openHubCreditsPage = vi.fn();
    const openLLMSettingsPage = vi.fn();
    const openHubCardStorePage = vi.fn();
    const rendered = render(
        <SidebarSystemStatus
            lang="zh-Hans"
            maclawLLMOnline={false}
            remoteActivationStatus={{ activated: false }}
            qqBotStatus=""
            telegramStatus=""
            weixinStatus=""
            lansengerStatus=""
            backgroundTaskCount={3}
            onOpenBackgroundTasks={options.onOpenBackgroundTasks}
            sidebarCurrentProviderTokenUsage={{ provider: options.isHubService === false ? '\u79c1\u6709\u670d\u52a1\u5546' : 'MaClaw\u5b98\u65b9', isHubService: options.isHubService ?? true, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
            sidebarHubCredits={credits}
            formatSidebarTokens={(value) => String(value)}
            formatSidebarHubExpiry={() => '05/06/26'}
            formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
            formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
            formatSidebarCredit={(value) => String(value)}
            unlimitedHubCreditText="\u65e0\u9650"
            noHubAuthorizationText="\u65e0"
            showHubCreditAction={options.showHubCreditAction ?? false}
            openHubCreditsPage={openHubCreditsPage}
            openServiceRedeemPage={openServiceRedeemPage}
            openLLMSettingsPage={openLLMSettingsPage}
            openHubCardStorePage={openHubCardStorePage}
        />,
    );
    return { ...rendered, openServiceRedeemPage, openHubCreditsPage, openLLMSettingsPage, openHubCardStorePage };
}

describe('SidebarSystemStatus Hub credits', () => {
    it('shows the image-input SVG before a provider whose active model supports vision', () => {
        render(
            <SidebarSystemStatus
                lang="en" maclawLLMOnline remoteActivationStatus={{}} qqBotStatus="" telegramStatus="" weixinStatus="" lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'Vision Provider', isHubService: false, supportsVision: true, input: 0, output: 0, total: 0 }} sidebarHubCredits={null}
                formatSidebarTokens={String} formatSidebarHubExpiry={() => ''} formatSidebarHubTotalCredits={() => ''} formatSidebarHubUsedCredits={() => ''} formatSidebarCredit={String}
                unlimitedHubCreditText="Unlimited" noHubAuthorizationText="None" showHubCreditAction={false} openHubCreditsPage={vi.fn()} openLLMSettingsPage={vi.fn()}
            />,
        );

        const provider = screen.getByRole('button', { name: 'Vision Provider' });
        expect(provider.querySelector('.sidebar-system-status__provider-vision-icon')).toBeTruthy();
        expect(provider.textContent).toBe('Vision Provider');
        expect(provider.title).toContain('Supports image input');
    });

    it('omits the image-input SVG for a text-only provider', () => {
        renderStatus(baseCredits, { isHubService: false });

        expect(document.querySelector('.sidebar-system-status__provider-vision-icon')).toBeNull();
    });

    it('does not infer image input from an unknown capability value', () => {
        render(
            <SidebarSystemStatus
                lang="en" maclawLLMOnline remoteActivationStatus={{}} qqBotStatus="" telegramStatus="" weixinStatus="" lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'Unverified Provider', isHubService: false, input: 0, output: 0, total: 0 }} sidebarHubCredits={null}
                formatSidebarTokens={String} formatSidebarHubExpiry={() => ''} formatSidebarHubTotalCredits={() => ''} formatSidebarHubUsedCredits={() => ''} formatSidebarCredit={String}
                unlimitedHubCreditText="Unlimited" noHubAuthorizationText="None" showHubCreditAction={false} openHubCreditsPage={vi.fn()} openLLMSettingsPage={vi.fn()}
            />,
        );

        expect(document.querySelector('.sidebar-system-status__provider-vision-icon')).toBeNull();
    });

    it('always shows assistant and coding effective-profile summaries together', () => {
        render(
            <SidebarSystemStatus
                lang="en" maclawLLMOnline={true} remoteActivationStatus={{}} qqBotStatus="" telegramStatus="" weixinStatus="" lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'Assistant Provider', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }} sidebarHubCredits={null}
                formatSidebarTokens={String} formatSidebarHubExpiry={() => ''} formatSidebarHubTotalCredits={() => ''} formatSidebarHubUsedCredits={() => ''} formatSidebarCredit={String}
                unlimitedHubCreditText="Unlimited" noHubAuthorizationText="None" showHubCreditAction={false} openHubCreditsPage={vi.fn()}
                profileSummaries={{
                    assistant: { profile: 'assistant', provider_name: 'Assistant Provider', model: 'assistant-model', health: 'configured' },
                    coding: { profile: 'coding', provider_name: 'Assistant Provider', model: 'assistant-model', inherit_assistant: true, health: 'configured' },
                }}
                activeProfile="coding"
            />,
        );
        const block = screen.getByTestId('sidebar-llm-profile-statuses');
        expect(block.textContent).toContain('Assistant Provider · assistant-model');
        expect(block.textContent).toContain('Follows assistant');
        expect(screen.getByText('Follows assistant').textContent).toBe('Follows assistant');
        expect(screen.queryByTestId('sidebar-llm-active-profile')).toBeNull();
        const codingProfile = screen.getByRole('button', { name: /Coding: Follows assistant.*Assistant Provider.*assistant-model.*current/ });
        expect(codingProfile.getAttribute('title')).toContain('Follows assistant · Assistant Provider · assistant-model');
    });

    it('derives the LLM headline from the active profile instead of assistant-only online state', () => {
        render(
            <SidebarSystemStatus
                lang="en" maclawLLMOnline remoteActivationStatus={{}} qqBotStatus="" telegramStatus="" weixinStatus="" lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'Coding Provider', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }} sidebarHubCredits={null}
                formatSidebarTokens={String} formatSidebarHubExpiry={() => ''} formatSidebarHubTotalCredits={() => ''} formatSidebarHubUsedCredits={() => ''} formatSidebarCredit={String}
                unlimitedHubCreditText="Unlimited" noHubAuthorizationText="None" showHubCreditAction={false} openHubCreditsPage={vi.fn()} openLLMSettingsPage={vi.fn()}
                profileSummaries={{
                    assistant: { profile: 'assistant', provider_name: 'Assistant Provider', model: 'assistant-model', health: 'configured' },
                    coding: { profile: 'coding', provider_name: 'Coding Provider', model: 'coding-model', health: 'unavailable' },
                }}
                activeProfile="coding"
            />,
        );
        expect(screen.getByText('LLM · Coding')).toBeTruthy();
        expect(screen.getByLabelText('LLM: Coding · Unavailable')).toBeTruthy();
        expect(screen.getByTestId('sidebar-llm-profile-statuses').textContent).toContain('Coding Provider · coding-model · Unavailable');
    });

    it('uses the connectivity result when the active task has no execution profile', () => {
        render(
            <SidebarSystemStatus
                lang="en" maclawLLMOnline remoteActivationStatus={{}} qqBotStatus="" telegramStatus="" weixinStatus="" lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'Assistant Provider', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }} sidebarHubCredits={null}
                formatSidebarTokens={String} formatSidebarHubExpiry={() => ''} formatSidebarHubTotalCredits={() => ''} formatSidebarHubUsedCredits={() => ''} formatSidebarCredit={String}
                unlimitedHubCreditText="Unlimited" noHubAuthorizationText="None" showHubCreditAction={false} openHubCreditsPage={vi.fn()}
                profileSummaries={{
                    assistant: { profile: 'assistant', provider_name: 'Assistant Provider', model: 'assistant-model', health: 'configured' },
                    coding: { profile: 'coding', provider_name: 'Coding Provider', model: 'coding-model', health: 'unverified' },
                }}
                activeProfile="none"
            />,
        );

        expect(screen.getByLabelText('LLM: Online').getAttribute('data-online')).toBe('true');
    });

    it('keeps service health and background tasks in one fixed row', () => {
        renderStatus(baseCredits);

        const signals = screen.getByLabelText('System status');
        const llm = signals.querySelector('.sidebar-system-status__signal--llm');
        const hub = signals.querySelector('.sidebar-system-status__signal--hub');
        const im = signals.querySelector('.sidebar-system-status__signal--im');
        const backgroundTasks = signals.querySelector('.sidebar-system-status__background-tasks');
        expect(llm).toBeTruthy();
        expect(hub).toBeTruthy();
        expect(im).toBeTruthy();
        expect(backgroundTasks).toBeTruthy();
        expect(signals.querySelectorAll('.sidebar-system-status__signal:not(.sidebar-system-status__background-tasks)')).toHaveLength(3);
        expect(backgroundTasks?.classList.contains('sidebar-system-status__background-tasks')).toBe(true);
    });

    it('opens the background task monitor from the background-task status', () => {
        const onOpenBackgroundTasks = vi.fn();
        const { unmount } = renderStatus(baseCredits, { onOpenBackgroundTasks });

        fireEvent.click(screen.getByRole('button', { name: '打开后台任务： 3' }));

        expect(onOpenBackgroundTasks).toHaveBeenCalledTimes(1);
        unmount();
    });

    it('does not expose a dead background-task control when no navigation handler is available', () => {
        const { unmount } = renderStatus(baseCredits);

        expect(screen.getByRole('button', { name: '打开后台任务： 3' }).hasAttribute('disabled')).toBe(true);
        unmount();
    });

    it('opens service redeem from official provider name and card store from cart', () => {
        const { openServiceRedeemPage, openHubCardStorePage, openLLMSettingsPage } = renderStatus(baseCredits);

        fireEvent.click(screen.getByRole('button', { name: /MaClaw\u5b98\u65b9/ }));
        fireEvent.click(screen.getByRole('button', { name: /\u6253\u5f00 MaClaw \u670d\u52a1\u5361\u5546\u5e97/ }));

        expect(screen.getByRole('button', { name: /MaClaw\u5b98\u65b9/ }).getAttribute('title')).toContain('MaClaw\u5b98\u65b9');
        expect(screen.getByRole('button', { name: /MaClaw\u5b98\u65b9/ }).getAttribute('title')).toContain('\u67e5\u770b\u6216\u5151\u6362');
        expect(openServiceRedeemPage).toHaveBeenCalledTimes(1);
        expect(openHubCardStorePage).toHaveBeenCalledTimes(1);
        expect(openLLMSettingsPage).not.toHaveBeenCalled();
    });

    it('opens LLM settings from non-official provider name without showing cart', () => {
        const { openServiceRedeemPage, openHubCardStorePage, openLLMSettingsPage } = renderStatus(baseCredits, { isHubService: false });

        fireEvent.click(screen.getByRole('button', { name: /\u79c1\u6709\u670d\u52a1\u5546/ }));

        expect(screen.queryByRole('button', { name: /MaClaw \u670d\u52a1\u5361\u5546\u5e97/ })).toBeNull();
        expect(openLLMSettingsPage).toHaveBeenCalledTimes(1);
        expect(openServiceRedeemPage).not.toHaveBeenCalled();
        expect(openHubCardStorePage).not.toHaveBeenCalled();
    });

    it('shows account remaining plus period available and recovery state', () => {
        renderStatus({
            ...baseCredits,
            remaining: 90,
            available: 0,
            showPeriodAvailable: true,
            status: 'period_limited',
            retryAfterSeconds: 3600,
        });

        // Lifetime account remaining stays visible for Total/Used/Left accounting.
        expect(screen.getByText('90')).toBeTruthy();
        expect(screen.getByText(/\u53ef\u7528/)).toBeTruthy();
        expect(screen.getByText(/\u5468\u671f\u9650\u989d/)).toBeTruthy();
        expect(screen.getByText(/\u7ea6 1 \u5c0f\u65f6\u540e\u6062\u590d/)).toBeTruthy();
    });

    it('shows the active new-user benefit credit limits in system status', () => {
        renderStatus({
            ...baseCredits,
            unlimited: true,
            newUserLimitCards: [{
                serviceGroupID: 'welcome', fiveHourLimit: 10, fiveHourUsed: 3, fiveHourRolling: true, fiveHourResetAt: '2026-05-06T10:00:00Z',
                dailyLimit: 25, dailyUsed: 7, dailyResetAt: '2026-05-07T00:00:00Z',
                permanent: true, expiresAt: '', status: 'active', retryAfterSeconds: 0, retryAfterAt: '',
            }],
        });

        const benefit = screen.getByText('新用户福利').closest('.sidebar-system-status__credits');
        expect(benefit?.textContent).toContain('近5小时 7/10 · 今日 18/25');
        expect(benefit?.textContent).not.toContain('不是总点数');
        expect(benefit?.textContent).not.toContain('长期有效');
        expect(benefit?.getAttribute('title')).toBeNull();
    });

    it('labels independent new-user allowances by service group instead of adding them together', () => {
        renderStatus({
            ...baseCredits,
            unlimited: true,
            newUserLimitCards: [
                { serviceGroupID: 'welcome-a', fiveHourLimit: 10, fiveHourUsed: 3, fiveHourRolling: true, fiveHourResetAt: '', dailyLimit: 0, dailyUsed: 0, dailyResetAt: '', permanent: true, expiresAt: '', status: 'active', retryAfterSeconds: 0, retryAfterAt: '' },
                { serviceGroupID: 'welcome-b', fiveHourLimit: 20, fiveHourUsed: 5, fiveHourRolling: true, fiveHourResetAt: '', dailyLimit: 0, dailyUsed: 0, dailyResetAt: '', permanent: true, expiresAt: '', status: 'active', retryAfterSeconds: 0, retryAfterAt: '' },
            ],
        });

        const benefit = screen.getByText('新用户福利').closest('.sidebar-system-status__credits');
        expect(benefit?.textContent).toContain('welcome-a · 近5小时 7/10');
        expect(benefit?.textContent).toContain('welcome-b · 近5小时 15/20');
        expect(benefit?.textContent).not.toContain('22/30');
    });

    it('shows a stopped quota badge that opens service redeem for period-limited official service', () => {
        const { openServiceRedeemPage } = renderStatus({ ...baseCredits, serviceActive: false, status: 'period_limited', retryAfterSeconds: 3600 });

        const badge = screen.getByRole('button', { name: /\u5df2\u505c\u6b62/ });
        expect(badge.textContent).toContain('\u5df2\u505c\u6b62');
        expect(badge.getAttribute('data-state')).toBe('stopped');
        expect(badge.getAttribute('title')).toContain('MaClaw \u5b98\u65b9\u670d\u52a1\u5df2\u505c\u6b62');
        expect(badge.getAttribute('title')).toContain('\u672c\u5468\u671f\u989d\u5ea6\u5df2\u7528\u5c3d');
        expect(badge.getAttribute('title')).toContain('\u7ea6 1 \u5c0f\u65f6\u540e\u6062\u590d');
        expect(badge.getAttribute('title')).not.toContain('\u9884\u8ba1 \u7ea6');
        expect(badge.getAttribute('title')).toContain('\u670d\u52a1\u5151\u6362');

        fireEvent.click(badge);
        expect(openServiceRedeemPage).toHaveBeenCalledTimes(1);
    });

    it('still shows official period limit badge when the current provider is not the Hub route', () => {
        const { openServiceRedeemPage } = renderStatus(
            { ...baseCredits, serviceActive: false, status: 'period_limited', retryAfterSeconds: 3600 },
            { isHubService: false },
        );

        const badge = screen.getByRole('button', { name: /\u5df2\u505c\u6b62/ });
        expect(screen.queryByText('05/06/26')).toBeNull();

        fireEvent.click(badge);
        expect(openServiceRedeemPage).toHaveBeenCalledTimes(1);
    });

    it('shows remaining credits when another official route keeps service active', () => {
        renderStatus({ ...baseCredits, serviceActive: true, status: 'active', retryAfterSeconds: 0 });

        expect(screen.queryByRole('button', { name: /\u9650\u989d/ })).toBeNull();
        expect(screen.getByText('90')).toBeTruthy();
    });

    it('routes the sidebar buy action to card store while service remains active', () => {
        const lowCredits = { ...baseCredits, serviceActive: true, status: 'active', remaining: 10 };
        const { openServiceRedeemPage, openHubCreditsPage, openHubCardStorePage } = renderStatus(
            lowCredits,
            { showHubCreditAction: true },
        );

        const buy = screen.getByRole('button', { name: '\u8d2d\u4e70' });

        fireEvent.click(buy);

        expect(openHubCardStorePage).toHaveBeenCalledTimes(1);
        expect(openServiceRedeemPage).not.toHaveBeenCalled();
        expect(openHubCreditsPage).not.toHaveBeenCalled();
    });

    it('routes the sidebar buy action to card store even while official service is period-limited', () => {
        const { openServiceRedeemPage, openHubCreditsPage, openHubCardStorePage } = renderStatus(
            { ...baseCredits, serviceActive: false, status: 'period_limited', retryAfterSeconds: 3600 },
            { showHubCreditAction: true },
        );

        const buy = screen.getByRole('button', { name: '\u8d2d\u4e70' });
        expect(buy.getAttribute('title')).toContain('MaClaw \u670d\u52a1\u5361\u5546\u5e97');

        fireEvent.click(buy);

        expect(openHubCardStorePage).toHaveBeenCalledTimes(1);
        expect(openServiceRedeemPage).not.toHaveBeenCalled();
        expect(openHubCreditsPage).not.toHaveBeenCalled();
    });

    it('shows queued state with activation time alongside account remaining', () => {
        renderStatus({ ...baseCredits, status: 'queued', retryAfterSeconds: 7200 });

        expect(screen.getByText('90')).toBeTruthy();
        expect(screen.getByText(/\u5f85\u751f\u6548/)).toBeTruthy();
        expect(screen.getByText(/\u7ea6 2 \u5c0f\u65f6\u540e\u751f\u6548/)).toBeTruthy();
    });

    it('shows expired state alongside account remaining', () => {
        renderStatus({ ...baseCredits, status: 'expired' });

        expect(screen.getByText('90')).toBeTruthy();
        expect(screen.getByText(/\u6388\u6743\u5df2\u8fc7\u671f/)).toBeTruthy();
    });

    it('shows prompt cache hit rate beside token usage', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 100, output: 20, total: 120, cachedInput: 40, cacheWrite: 30, requests: 5, cachedRequests: 2 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => `${value}`}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        expect(screen.getByText(/cache 40%/)).toBeTruthy();
        const cacheTitles = screen.getAllByTitle(/Cache hit: 40%/);
        expect(cacheTitles[0].getAttribute('title')).toContain('Read 40');
        expect(cacheTitles[0].getAttribute('title')).toContain('Write 30');
    });

    it('hides cache rate for MaClaw official service', () => {
        render(
            <SidebarSystemStatus
                lang="zh-Hans"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw\u5b98\u65b9', isHubService: true, input: 100, output: 20, total: 120, cachedInput: 100, cacheWrite: 0, requests: 3, cachedRequests: 3 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => `${value}`}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="\u65e0\u9650"
                noHubAuthorizationText="\u65e0"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        expect(screen.queryByText(/\u7f13\u5b58/)).toBeNull();
        expect(screen.getByText('120')).toBeTruthy();
    });

    it('shows zero cache hit rate when local cache is enabled before first hit', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                localLLMCacheEnabled
                sidebarCurrentProviderTokenUsage={{ provider: 'Local', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => `${value}`}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        expect(screen.getByText(/cache 0%/)).toBeTruthy();
    });

    it('uses local cache counters for non-hub providers when local cache is enabled', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                localLLMCacheEnabled
                sidebarCurrentProviderTokenUsage={{ provider: 'Local', isHubService: false, input: 100, output: 20, total: 120, cachedInput: 0, cacheWrite: 0, requests: 10, cachedRequests: 9, localCacheRequests: 4, localCacheHits: 1 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => `${value}`}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        expect(screen.getByText(/cache 25%/)).toBeTruthy();
        const cacheTitles = screen.getAllByTitle(/Local cache hit: 25%/);
        expect(cacheTitles[0].getAttribute('title')).toContain('Hits 1/4');
    });

    it('shows background task count immediately after IM status', () => {
        render(
            <SidebarSystemStatus
                lang="zh-Hans"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                backgroundTaskCount={3}
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="\u65e0\u9650"
                noHubAuthorizationText="\u65e0"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        const signals = screen.getByLabelText('System status');
        expect(signals.textContent).toContain('\u540e\u53f0\u4efb\u52a1\uff1a 3');
        expect(signals.textContent?.indexOf('IM')).toBeLessThan(signals.textContent?.indexOf('\u540e\u53f0\u4efb\u52a1\uff1a 3') ?? -1);
        expect(signals.textContent?.indexOf('HUB')).toBeLessThan(signals.textContent?.indexOf('IM') ?? -1);
    });

    it('shows the active coding agent task status in the sidebar monitor', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }}
            />,
        );

        const status = screen.getByTestId('sidebar-coding-agent-status');
        expect(status.textContent).toContain('Coding');
        expect(status.textContent).not.toContain('Task status');
        expect(status.textContent).toContain('Running');
        expect(status.textContent).toContain('T2');
        expect(status.textContent).toContain('Fix stale edit guard');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toContain('Fix stale edit guard');
        expect(status.getAttribute('aria-label')).toMatch(/Coding\s*\u00b7\s*Running/);
        // No snapshot: compact header alone, no empty detail body.
        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.querySelector('[data-testid="sidebar-coding-agent-tool-trace"]')).toBeNull();
        // Group still has an accessible name from the compact header line.
        expect(card.getAttribute('aria-label')).toMatch(/Coding\s*\u00b7\s*Running/);
        expect(card.getAttribute('aria-label')).toContain('Fix stale edit guard');
    });

    it('uses soft amber (not red) for failed coding-agent sidebar status; dark brightens', () => {
        const { rerender } = render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'failed', taskID: 'T1', title: 'Compile check' }}
            />,
        );
        let status = screen.getByTestId('sidebar-coding-agent-status');
        expect(status.getAttribute('data-tone-accent')).toBe('#a16207');
        expect(status.getAttribute('data-tone-accent')).not.toMatch(/#c43d34/i);

        rerender(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'failed', taskID: 'T1', title: 'Compile check' }}
                isDark
            />,
        );
        status = screen.getByTestId('sidebar-coding-agent-status');
        expect(status.getAttribute('data-tone-accent')).toBe('#e0b253');
    });

    it('shows coding agent turn details in the sidebar monitor card', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'result', taskID: 'T2', title: 'Fix stale edit guard', event: 'diff_summary', detail: '2 files', count: 2, files: ['a.go', 'b.go'] }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'result', taskID: 'T2', title: 'Fix stale edit guard', event: 'diff_summary', detail: '2 files', count: 2, files: ['a.go', 'b.go'] },
                    turnID: 'turn-2',
                    taskID: 'T2',
                    title: 'Fix stale edit guard',
                    phase: 'result',
                    tool: 'git_diff',
                    toolOutcome: 'success',
                    toolDurationMs: 1250,
                    tools: [
                        { name: 'read_file', outcome: 'success', durationMs: 80 },
                        { name: 'git_diff', outcome: 'success', durationMs: 1250 },
                    ],
                    guardrailStatus: 'blocked',
                    guardrailSummary: 'blocked | bash | category:git',
                    guardrailCount: 1,
                    commandStatus: 'failed',
                    commandSummary: '2 bash commands run, 1 failed: npm test',
                    commandCount: 2,
                    fileActivityStatus: 'changed',
                    fileActivitySummary: 'read 2 / modified 1 / created 1; changed: a.go, b.go',
                    fileActivityCount: 4,
                    fileActivityDetail: 'read 2 / modified 1 / created 1',
                    qualityStatus: 'warning',
                    qualitySummary: 'verification not run',
                    qualityCount: 1,
                    explorationStatus: 'explored',
                    explorationSummary: 'searched before editing',
                    explorationCount: 2,
                    verificationStatus: 'passed',
                    verificationSummary: 'go test ./gui passed',
                    verificationCount: 1,
                    diffCheckStatus: 'checked',
                    diffCheckSummary: 'diff --git a/a.go b/a.go',
                    changeCount: 2,
                    files: ['a.go', 'b.go'],
                    diffSummary: '2 files',
                }}
            />,
        );

        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.getAttribute('role')).toBe('group');
        expect(card.getAttribute('aria-label')).toContain('Tool: git_diff');
        expect(card.getAttribute('aria-label')).toContain('Files: a.go, b.go');
        expect(card.getAttribute('title')).toContain('Diff: 2 files');
        expect(card.getAttribute('data-turn-id')).toBe('turn-2');
        expect(card.getAttribute('data-tool')).toBe('git_diff');
        expect(card.getAttribute('data-tool-outcome')).toBe('success');
        expect(card.getAttribute('data-tool-outcome-state')).toBe('success');
        expect(card.getAttribute('data-tool-duration-ms')).toBe('1250');
        expect(card.getAttribute('data-tool-count')).toBe('2');
        expect(card.getAttribute('data-guardrail-status')).toBe('blocked');
        expect(card.getAttribute('data-guardrail-state')).toBe('blocked');
        expect(card.getAttribute('data-guardrail-count')).toBe('1');
        expect(card.getAttribute('data-command-status')).toBe('failed');
        expect(card.getAttribute('data-command-state')).toBe('failed');
        expect(card.getAttribute('data-command-count')).toBe('2');
        expect(card.getAttribute('data-file-activity-status')).toBe('changed');
        expect(card.getAttribute('data-file-activity-state')).toBe('changed');
        expect(card.getAttribute('data-file-activity-count')).toBe('4');
        expect(card.getAttribute('data-quality-status')).toBe('warning');
        expect(card.getAttribute('data-quality-state')).toBe('warning');
        expect(card.getAttribute('data-quality-count')).toBe('1');
        expect(card.getAttribute('data-exploration-status')).toBe('explored');
        expect(card.getAttribute('data-exploration-state')).toBe('explored');
        expect(card.getAttribute('data-exploration-count')).toBe('2');
        expect(card.getAttribute('data-verification-status')).toBe('passed');
        expect(card.getAttribute('data-verification-state')).toBe('passed');
        expect(card.getAttribute('data-verification-count')).toBe('1');
        expect(card.getAttribute('data-diff-check-status')).toBe('checked');
        expect(card.getAttribute('data-diff-check-state')).toBe('checked');
        expect(card.getAttribute('data-change-count')).toBe('2');
        expect(card.getAttribute('data-file-count')).toBe('2');
        // Dense layout: files + tool trail chips + metric chips (no stacked Tool/Result/Duration rows).
        expect(card.textContent).toContain('Files');
        expect(card.textContent).toContain('a.go, b.go');
        expect(card.textContent).toContain('git_diff');
        expect(card.textContent).toContain('Success');
        expect(card.textContent).toContain('1.3s');
        const trace = screen.getByTestId('sidebar-coding-agent-tool-trace');
        expect(trace.getAttribute('aria-label')).toBe('read_file Success 80ms -> git_diff Success 1.3s');
        expect(trace.querySelector('[data-tool-trace-name="read_file"]')?.getAttribute('data-tool-trace-outcome-state')).toBe('success');
        expect(trace.querySelector('[data-tool-trace-name="git_diff"]')?.getAttribute('data-tool-trace-outcome-state')).toBe('success');
        expect(card.textContent).toContain('Guard');
        expect(card.textContent).toContain('Blocked (1)');
        expect(screen.getByTestId('sidebar-coding-agent-guardrail').getAttribute('data-guardrail-summary')).toBe('blocked | bash | category:git');
        expect(card.textContent).toMatch(/Cmds|Commands/);
        expect(card.textContent).toContain('Failed (2)');
        expect(screen.getByTestId('sidebar-coding-agent-commands').getAttribute('data-command-summary')).toBe('2 bash commands run, 1 failed: npm test');
        expect(card.textContent).toContain('Activity');
        expect(card.textContent).toContain('Changed (read 2 / modified 1 / created 1)');
        expect(screen.getByTestId('sidebar-coding-agent-file-activity').getAttribute('data-file-activity-summary')).toBe('read 2 / modified 1 / created 1; changed: a.go, b.go');
        expect(screen.getByTestId('sidebar-coding-agent-file-activity').getAttribute('data-file-activity-detail')).toBe('read 2 / modified 1 / created 1');
        expect(card.textContent).toContain('Quality');
        expect(card.textContent).toContain('Warning (1)');
        expect(screen.getByTestId('sidebar-coding-agent-quality').getAttribute('data-quality-summary')).toBe('verification not run');
        expect(card.textContent).toContain('Explore');
        expect(card.textContent).toContain('Explored (2)');
        expect(screen.getByTestId('sidebar-coding-agent-exploration').getAttribute('data-exploration-summary')).toBe('searched before editing');
        expect(card.textContent).toContain('Verify');
        expect(card.textContent).toContain('Passed (1)');
        expect(screen.getByTestId('sidebar-coding-agent-verification').getAttribute('data-verification-summary')).toBe('go test ./gui passed');
        expect(card.textContent).toContain('Diff check');
        expect(card.textContent).toContain('Checked');
        expect(screen.getByTestId('sidebar-coding-agent-diff-check').getAttribute('data-diff-check-summary')).toBe('diff --git a/a.go b/a.go');
        expect(card.textContent).toContain('Diff');
        expect(card.textContent).toContain('2 files');
        expect(card.className).toContain('coding-agent-turn-card--success');
    });

    it('shows blocked tool outcome as a semantic sidebar badge', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'running', taskID: 'T3', title: 'Guard command', event: 'tool_finished', detail: 'bash', outcome: 'blocked' }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'running', taskID: 'T3', title: 'Guard command', event: 'tool_finished', detail: 'bash', outcome: 'blocked' },
                    turnID: 'turn-3',
                    taskID: 'T3',
                    title: 'Guard command',
                    phase: 'running',
                    tool: 'bash',
                    toolOutcome: 'blocked',
                    tools: [{ name: 'bash', outcome: 'blocked', summary: 'command refused' }],
                    commandStatus: 'none',
                    commandCount: 0,
                    qualityStatus: 'passed',
                    qualityCount: 0,
                }}
            />,
        );

        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.getAttribute('data-tool-outcome-state')).toBe('blocked');
        expect(card.getAttribute('data-command-count')).toBe('0');
        expect(card.getAttribute('data-quality-count')).toBe('0');
        expect(card.className).toContain('coding-agent-turn-card--blocked');
        expect(card.textContent).toContain('Blocked');
        expect(card.textContent).toContain('None (0)');
        expect(card.textContent).toContain('Passed (0)');
        const blockedTool = screen.getByTestId('sidebar-coding-agent-tool-trace').querySelector('[data-tool-trace-name="bash"]');
        expect(blockedTool?.getAttribute('data-tool-trace-outcome-state')).toBe('blocked');
        expect(blockedTool?.getAttribute('data-tool-trace-summary')).toBe('command refused');
        expect(blockedTool?.getAttribute('title')).toContain('command refused');
    });

    it('shows exploratory bash failures as neutral command checks in the sidebar details', () => {
        render(
            <SidebarSystemStatus
                lang="zh-Hans"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'result', taskID: 'T5', title: 'Run expected red tests', event: 'command_summary', outcome: 'failed', summary: 'All tests should FAIL (red light) until implementation is complete.', count: 1 }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'result', taskID: 'T5', title: 'Run expected red tests', event: 'command_summary', outcome: 'failed', summary: 'All tests should FAIL (red light) until implementation is complete.', count: 1 },
                    turnID: 'turn-5',
                    taskID: 'T5',
                    title: 'Run expected red tests',
                    phase: 'result',
                    commandStatus: 'failed',
                    commandCount: 1,
                    commandSummary: 'All tests should FAIL (red light) until implementation is complete.',
                }}
            />,
        );

        const commands = screen.getByTestId('sidebar-coding-agent-commands');
        expect(commands.textContent).toContain('\u68c0\u67e5');
        expect(commands.style.color).toBe('rgb(100, 116, 139)');
        expect(commands.style.border).toBe('1px solid rgba(100, 116, 139, 0.2)');
    });

    it('keeps zero-length coding agent sidebar counts visible in data attributes', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'result', taskID: 'T4', title: 'No-op coding task', event: 'diff_summary', count: 0, files: [] }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'result', taskID: 'T4', title: 'No-op coding task', event: 'diff_summary', count: 0, files: [] },
                    turnID: 'turn-4',
                    taskID: 'T4',
                    title: 'No-op coding task',
                    phase: 'result',
                    toolDurationMs: 0,
                    tools: [],
                    changeCount: 0,
                    files: [],
                }}
            />,
        );

        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.getAttribute('data-tool-duration-ms')).toBe('0');
        expect(card.getAttribute('data-tool-count')).toBe('0');
        expect(card.getAttribute('data-change-count')).toBe('0');
        expect(card.getAttribute('data-file-count')).toBe('0');
    });
});
