import { describe, expect, it } from 'vitest';
import { cloudWorkspaceShareJoinLooksLikeDeadLink, cloudWorkspaceShareJoinLooksLikeOwn } from '../CloudWorkspaceShareDialog';

describe('cloud workspace share join error matching', () => {
    it('recognizes own-workspace copy in en/zh/zh-Hant', () => {
        expect(cloudWorkspaceShareJoinLooksLikeOwn('This is your own cloud workspace. You don\'t need the share link to open it.')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeOwn('这是你自己的云端工作区，无需通过分享链接加入。')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeOwn('這是你自己的雲端工作區，無需透過分享連結加入。')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeOwn('cannot accept your own cloud workspace share')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeOwn('The share password is incorrect.')).toBe(false);
    });

    it('recognizes expired and revoked copy', () => {
        expect(cloudWorkspaceShareJoinLooksLikeDeadLink('This share link has expired.')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeDeadLink('分享链接已过期')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeDeadLink('该分享链接已停止分享')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeDeadLink('This share link is no longer active.')).toBe(true);
        expect(cloudWorkspaceShareJoinLooksLikeDeadLink('The share password is incorrect.')).toBe(false);
    });
});
