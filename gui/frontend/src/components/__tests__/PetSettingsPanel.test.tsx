/** @vitest-environment jsdom */
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { main } from '../../../wailsjs/go/models';
import { PetSettingsPanel } from '../PetSettingsPanel';

function renderPetSettings(lang: string) {
    const config = new main.AppConfig({
        pet_enabled: true,
        pet_skin: 'clawmate',
        pet_size: 88,
        pet_interaction_mode: 'balanced',
        pet_conversation_mode: 'text-first',
        pet_readback_mode: 'summary',
        pet_motion_enabled: true,
        pet_text_interaction_enabled: true,
        pet_file_drop_enabled: true,
    });

    render(
        <PetSettingsPanel
            config={config}
            lang={lang}
            setConfig={vi.fn()}
            saveConfig={vi.fn().mockResolvedValue(undefined)}
        />,
    );
}

describe('PetSettingsPanel localization', () => {
    afterEach(() => cleanup());

    it('renders pet-specific controls in Simplified Chinese', () => {
        renderPetSettings('zh-Hans');

        [
            '桌面宠物',
            '启用桌面宠物',
            '待机',
            '聆听',
            '思考',
            '说话',
            '默认 MaClaw 爪爪伙伴',
            '抓住问题，把有效信号拎出来。',
            '安静',
            '平衡',
            '活跃',
            '文字优先',
            '语音轮次',
            '连续对话',
            '关闭',
            '摘要',
            '全文',
            '仅完成时',
        ].forEach((label) => {
            expect(screen.queryAllByText(label).length).toBeGreaterThan(0);
        });

        [
            'Idle',
            'Listen',
            'Think',
            'Speak',
            'Quiet',
            'Active',
            'Text First',
            'Voice Turn',
            'Continuous',
            'Done Only',
        ].forEach((label) => {
            expect(screen.queryByText(label)).toBeNull();
        });
    });

    it('keeps pet-specific controls in English when English is selected', () => {
        renderPetSettings('en');

        expect(screen.getByText('Desktop Pet')).toBeTruthy();
        expect(screen.getByText('Enable Desktop Pet')).toBeTruthy();
        expect(screen.getByText('Idle')).toBeTruthy();
        expect(screen.getByText('Text First')).toBeTruthy();
        expect(screen.getByText('Done Only')).toBeTruthy();
        expect(screen.getByText('Desktop Entry')).toBeTruthy();
        expect(screen.getByLabelText('ASR not enabled')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Balanced' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByRole('button', { name: 'Classic: The current comic motion sound.' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByLabelText('Size').getAttribute('aria-valuetext')).toBe('88px');
        expect(screen.getByLabelText('Continuous Timeout').getAttribute('aria-valuetext')).toBe('30s');
    });
});
