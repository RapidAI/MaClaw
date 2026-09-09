/** @vitest-environment jsdom */
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { corelib, main } from '../../../../wailsjs/go/models';
import { MaclawRolePanel } from '../MaclawRolePanel';

function renderPanel(lang: string) {
	const saveRemoteConfigField = vi.fn();
    const config = new corelib.AppConfig({
        maclaw_role_name: 'MaClaw',
        maclaw_role_description: '你的全能数智伴侣MaClaw',
    });

    render(
        <MaclawRolePanel
            config={config}
			saveRemoteConfigField={saveRemoteConfigField}
            lang={lang}
        />,
    );

	return saveRemoteConfigField;
}

describe('MaclawRolePanel localization', () => {
    afterEach(() => cleanup());

    it('renders role settings in Simplified Chinese', () => {
        renderPanel('zh-Hans');

        expect(screen.getByText('角色名称')).toBeTruthy();
        expect(screen.getByText('角色描述')).toBeTruthy();
        expect(screen.getByRole('button', { name: '保存' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '恢复默认' })).toBeTruthy();
        expect(screen.queryByText('Role Name')).toBeNull();
        expect(screen.queryByText('Auto-post Chat Gossip')).toBeNull();
        expect(screen.queryByText('聊天八卦自动发帖')).toBeNull();
    });

    it('renders role settings in English', () => {
        renderPanel('en');

        expect(screen.getByText('Role Name')).toBeTruthy();
        expect(screen.getByText('Role Description')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Reset Default' })).toBeTruthy();
        expect(screen.queryByText('Auto-post Chat Gossip')).toBeNull();
    });

	it('restores the configured default identity', () => {
		const saveRemoteConfigField = renderPanel('en');

		screen.getByRole('button', { name: 'Reset Default' }).click();

		expect(saveRemoteConfigField).toHaveBeenCalledWith(expect.objectContaining({
			maclaw_role_name: 'MaClaw',
			maclaw_role_description: '你的全能数智伴侣MaClaw',
		}));
	});
});
