import { describe, expect, it } from 'vitest';
import {
    clearWorkspaceLaunchIssue,
    formatWorkspaceLaunchError,
    formatWorkspaceLaunchIssue,
    isEnterpriseAppKindForWorkspace,
    setWorkspaceLaunchIssue,
} from '../appsWorkspaceLaunch';

describe('appsWorkspaceLaunch helpers', () => {
    it('detects enterprise kinds for one-click workspace', () => {
        expect(isEnterpriseAppKindForWorkspace('enterprise_normal_app')).toBe(true);
        expect(isEnterpriseAppKindForWorkspace('enterprise_approval_app')).toBe(true);
        expect(isEnterpriseAppKindForWorkspace('tool_app')).toBe(false);
        expect(isEnterpriseAppKindForWorkspace('')).toBe(false);
    });

    it('returns null when workspace opened', () => {
        expect(formatWorkspaceLaunchIssue('客户', { app_view_opened: true }, 'zh')).toBeNull();
        expect(formatWorkspaceLaunchIssue('Cust', null, 'en')).toBeNull();
    });

    it('formats no_approval_instances in zh/en', () => {
        expect(formatWorkspaceLaunchIssue('报销', { app_view_opened: false, reason: 'no_approval_instances' }, 'zh'))
            .toContain('暂无待办审批');
        expect(formatWorkspaceLaunchIssue('Expense', { app_view_opened: false, reason: 'no_approval_instances' }, 'en'))
            .toContain('no pending approval');
    });

    it('formats generic reason and fallback', () => {
        expect(formatWorkspaceLaunchIssue('客户', { app_view_opened: false, reason: 'boom' }, 'zh'))
            .toBe('「客户」工作区未打开：boom');
        expect(formatWorkspaceLaunchIssue('Cust', { app_view_opened: false }, 'en'))
            .toContain('MIS may be off');
        expect(formatWorkspaceLaunchIssue('客户', { app_view_error: 'x' }, 'zh'))
            .toContain('：x');
    });

    it('formats bridge errors', () => {
        expect(formatWorkspaceLaunchError('客户', new Error('MIS disabled'), 'zh'))
            .toBe('「客户」工作区打开失败：MIS disabled');
        expect(formatWorkspaceLaunchError('Cust', 'fail', 'en'))
            .toBe('"Cust" workspace open failed: fail');
    });

    it('set/clear per-app issue map', () => {
        const a = setWorkspaceLaunchIssue({}, 'app-1', 'oops');
        expect(a['app-1']).toBe('oops');
        const b = clearWorkspaceLaunchIssue(a, 'app-1');
        expect(b['app-1']).toBeUndefined();
        expect(clearWorkspaceLaunchIssue({}, 'x')).toEqual({});
    });
});
