/** @vitest-environment jsdom */
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AssistantWarmupSplash } from '../AssistantWarmupSplash';
import type { AIAssistantInitStatus } from '../useAIAssistant';

function renderSplash(initStatus?: AIAssistantInitStatus, lang = 'zh-Hans') {
    return render(
        <AssistantWarmupSplash
            initLabel="正在连接 Hub..."
            initStatus={initStatus}
            lang={lang}
            brandLine={<div data-testid="warmup-brand">BRAND</div>}
        />,
    );
}

const stepStates = (container: HTMLElement) =>
    Array.from(container.querySelectorAll('.acb-warmup-step')).map((el) =>
        /acb-warmup-step--(done|active|todo)/.exec(el.className)?.[1],
    );

describe('AssistantWarmupSplash stages', () => {
    it('defaults to the connecting stage and shows the live status line', () => {
        const { container } = renderSplash();
        expect(stepStates(container)).toEqual(['active', 'todo', 'todo']);
        expect(container.querySelector('.acb-warmup-progress__fill')?.getAttribute('style')).toContain('18%');
        expect(screen.getByText('正在连接 Hub...')).toBeTruthy();
        expect(screen.getByTestId('warmup-brand')).toBeTruthy();
    });

    it('marks earlier steps done and the current step active while loading', () => {
        const { container } = renderSplash('loading');
        expect(stepStates(container)).toEqual(['done', 'active', 'todo']);
        expect(container.querySelector('.acb-warmup-progress__fill')?.getAttribute('style')).toContain('58%');
        expect(container.querySelector('.acb-warmup-step--done svg')).toBeTruthy();
        expect(container.querySelector('.acb-warmup-step--active')?.getAttribute('aria-current')).toBe('step');
    });

    it('activates the final warm-up step with its progress share', () => {
        const { container } = renderSplash('warming');
        expect(stepStates(container)).toEqual(['done', 'done', 'active']);
        expect(container.querySelector('.acb-warmup-progress__fill')?.getAttribute('style')).toContain('88%');
    });

    it('falls back to the connecting stage for ready/degraded (never rendered when ready)', () => {
        const { container } = renderSplash('degraded');
        expect(stepStates(container)).toEqual(['active', 'todo', 'todo']);
    });

    it('localizes step labels for zh-Hans, en and zh-Hant', () => {
        const zh = renderSplash('connecting', 'zh-Hans').container;
        expect(Array.from(zh.querySelectorAll('.acb-warmup-step__label')).map((el) => el.textContent))
            .toEqual(['连接服务', '加载组件', '预热就绪']);
        const en = renderSplash('connecting', 'en').container;
        expect(Array.from(en.querySelectorAll('.acb-warmup-step__label')).map((el) => el.textContent))
            .toEqual(['Connect', 'Load components', 'Warm up']);
        const zhHant = renderSplash('connecting', 'zh-Hant').container;
        expect(Array.from(zhHant.querySelectorAll('.acb-warmup-step__label')).map((el) => el.textContent))
            .toEqual(['連線服務', '載入元件', '預熱就緒']);
    });

    it('exposes the whole panel as a polite status region', () => {
        renderSplash();
        const panel = screen.getByRole('status');
        expect(panel.getAttribute('aria-live')).toBe('polite');
        expect(panel.className).toContain('acb-warmup-panel');
    });
});
