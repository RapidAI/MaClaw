/** @vitest-environment jsdom */
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { main } from '../../../../wailsjs/go/models';
import { MaclawRolePanel } from '../MaclawRolePanel';

function renderPanel(lang: string) {
    const config = new main.AppConfig({
        maclaw_role_name: 'MaClaw',
        maclaw_role_description: '一个尽心尽责无所不能的软件开发管家',
        gossip_auto_publish: true,
    });

    render(
        <MaclawRolePanel
            config={config}
            saveRemoteConfigField={vi.fn()}
            lang={lang}
        />,
    );
}

describe('MaclawRolePanel localization', () => {
    afterEach(() => cleanup());

    it('renders role settings in Simplified Chinese', () => {
        renderPanel('zh-Hans');

        expect(screen.getByText('角色名称')).toBeTruthy();
        expect(screen.getByText('角色描述')).toBeTruthy();
        expect(screen.getByRole('button', { name: '保存' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '恢复默认' })).toBeTruthy();
        expect(screen.getByText('聊天八卦自动发帖')).toBeTruthy();
        expect(screen.queryByText('Role Name')).toBeNull();
        expect(screen.queryByText('Auto-post Chat Gossip')).toBeNull();
    });

    it('renders role settings in English', () => {
        renderPanel('en');

        expect(screen.getByText('Role Name')).toBeTruthy();
        expect(screen.getByText('Role Description')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Reset Default' })).toBeTruthy();
        expect(screen.getByText('Auto-post Chat Gossip')).toBeTruthy();
    });
});
