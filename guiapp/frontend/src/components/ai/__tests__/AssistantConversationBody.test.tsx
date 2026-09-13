/** @vitest-environment jsdom */
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AssistantConversationBody } from '../AssistantConversationBody';
import { overlayTheme } from '../aiAssistantPanelTheme';

function renderSplash(overrides: Partial<Parameters<typeof AssistantConversationBody>[0]> = {}) {
    return render(
        <AssistantConversationBody
            initLabel="Initializing..."
            lang="zh-Hans"
            messages={[]}
            pinnedNews={[]}
            ready={false}
            renderedOtherMessages={null}
            renderedProgressMessages={null}
            theme={overlayTheme}
            {...overrides}
        />,
    );
}

describe('AssistantConversationBody brand splash', () => {
    it('shows the default MaClaw generation line while initializing', () => {
        renderSplash();
        expect(screen.getByTestId('assistant-brand-splash').getAttribute('aria-label')).toBe('码卡龙 8 企缘');
        expect(screen.getByText('码卡龙')).toBeTruthy();
        expect(screen.getByText('企缘')).toBeTruthy();
    });

    it('uses traditional glyphs for zh-Hant', () => {
        renderSplash({ lang: 'zh-Hant' });
        expect(screen.getByTestId('assistant-brand-splash').getAttribute('aria-label')).toBe('碼卡龍 8 企緣');
        expect(screen.getByText('碼卡龍')).toBeTruthy();
        expect(screen.getByText('企緣')).toBeTruthy();
    });

    it('shows the QAgent OEM name on the init splash', () => {
        renderSplash({ brandId: 'qianxin', brandDisplayNameCN: '虎爪' });
        expect(screen.getByTestId('assistant-brand-splash').getAttribute('aria-label')).toBe('虎爪 8 企缘');
        expect(screen.getByText('虎爪')).toBeTruthy();
        expect(screen.queryByText('码卡龙')).toBeNull();
    });

    it('shows the MetaStaff OEM name on the init splash', () => {
        renderSplash({ brandId: 'metastaff', brandDisplayNameCN: '智员' });
        expect(screen.getByTestId('assistant-brand-splash').getAttribute('aria-label')).toBe('智员 8 企缘');
        expect(screen.getByText('智员')).toBeTruthy();
    });

    it('uses traditional generation for OEM names in zh-Hant', () => {
        renderSplash({ lang: 'zh-Hant', brandId: 'qianxin', brandDisplayNameCN: '虎爪' });
        expect(screen.getByTestId('assistant-brand-splash').getAttribute('aria-label')).toBe('虎爪 8 企緣');
        expect(screen.getByText('企緣')).toBeTruthy();
        expect(screen.queryByText('码卡龙')).toBeNull();
        expect(screen.queryByText('碼卡龍')).toBeNull();
    });
});

describe('AssistantConversationBody standalone live header', () => {
    it('puts live sheen on a trailing activity title when no assistant bubble owns it', () => {
        renderSplash({
            ready: true,
            liveActivityLabel: '正在提取网页',
        });
        const panel = screen.getByTestId('assistant-reasoning-panel');
        const label = screen.getByTestId('assistant-reasoning-label');
        expect(panel.getAttribute('data-live')).toBe('true');
        expect(label.textContent).toBe('正在提取网页');
        expect(label.className).toContain('assistant-reasoning-live-label');
        expect(screen.queryByText('有什么可以帮你的？')).toBeNull();
    });
});
