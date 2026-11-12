import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotChipContext, BotMessageBody, botMessageHasChips, botMessageIsStructured, splitBotBeats } from './botMessageBody';
import { desktopBotCorrectAttachClaim } from './desktopBotDelivery';

describe('reply cards and beats', () => {
    it('turns links, paths and commit ids into cards', () => {
        const openLink = vi.fn();
        const openPath = vi.fn();
        const text = '已推送 1faa7af6f，报告在 /Users/dan/Desktop/report.pdf，详情 https://github.com/RapidAI/CodeClaw/pull/12。';
        expect(botMessageIsStructured(text)).toBe(true);
        render(<BotChipContext.Provider value={{ openLink, openPath }}><BotMessageBody text={text} /></BotChipContext.Provider>);
        expect(screen.getByTestId('bot-chip-commit').textContent).toBe('1faa7af6f');
        const path = screen.getByTestId('bot-chip-path');
        expect(path.textContent).toBe('report.pdf');
        fireEvent.click(path);
        expect(openPath.mock.calls[0][1]).toBe('/Users/dan/Desktop/report.pdf');
        const link = screen.getByTestId('bot-chip-link');
        expect(link.textContent).toBe('github.com/RapidAI/CodeClaw/pull/12');
        fireEvent.click(link);
        expect(openLink).toHaveBeenCalledWith('https://github.com/RapidAI/CodeClaw/pull/12');
    });

    it('leaves ordinary words, numbers and short slashes alone', () => {
        expect(botMessageHasChips('明天 1234567 次，a/b 两种，颜色 #a1b2c3d，deadbeef')).toBe(false);
    });

    it('reads a code span holding a path as a card and a Markdown link by its label', () => {
        render(<BotMessageBody text={'看 `/home/me/out.csv` 和 [合并请求](https://example.com/pr/3)'} />);
        expect(screen.getByTestId('bot-chip-path').textContent).toBe('out.csv');
        expect(screen.getByTestId('bot-chip-link').textContent).toBe('合并请求');
    });

    it('splits beats on a bare --- line outside code', () => {
        expect(splitBotBeats('做完了。\n---\n你需要重启 GUI。')).toEqual(['做完了。', '你需要重启 GUI。']);
        expect(splitBotBeats('```\n---\n```')).toEqual(['```\n---\n```']);
        expect(splitBotBeats('只有一句')).toEqual(['只有一句']);
        expect(splitBotBeats('a\n---\nb\n---\nc\n---\nd\n---\ne')).toEqual(['a', 'b', 'c', 'd\n\ne']);
    });

    it('corrects a reply that claims an attachment nothing delivered', () => {
        const said = '两页 PDF 就附在这条消息里。';
        expect(desktopBotCorrectAttachClaim(said, false, 'zh-Hans')).toContain('更正：这条消息实际没有收到附件');
        expect(desktopBotCorrectAttachClaim(said, true, 'zh-Hans')).toBe(said);
        expect(desktopBotCorrectAttachClaim('文件在 /tmp/a.pdf', false, 'zh-Hans')).toBe('文件在 /tmp/a.pdf');
        const srvFixed = said + '\n（更正：这条消息实际没有带附件，文件留在电脑上，路径见上文。）';
        expect(desktopBotCorrectAttachClaim(srvFixed, false, 'zh-Hans')).toBe(srvFixed);
    });
});
