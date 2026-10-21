import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { DesktopBotOptionChips } from './DesktopBotOptionChips';

const setup = (lang = 'zh') => {
    const onPick = vi.fn();
    const onOther = vi.fn();
    render(<DesktopBotOptionChips options={['发笔记', '评论']} lang={lang} onPick={onPick} onOther={onOther} />);
    return { onPick, onOther };
};

describe('DesktopBotOptionChips', () => {
    afterEach(cleanup);
    it('picks an option; the default one is disabled', () => {
        const { onPick } = setup();
        const chips = screen.getAllByTestId('desktop-bot-option');
        expect((chips[0] as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(chips[1]);
        expect(onPick).toHaveBeenCalledWith('评论');
    });

    it('其它 opens an input; Enter sends trimmed text, empty does nothing', () => {
        const { onOther } = setup();
        expect(screen.queryByTestId('desktop-bot-other-input')).toBeNull();
        fireEvent.click(screen.getByTestId('desktop-bot-option-other'));
        const input = screen.getByTestId('desktop-bot-other-input');
        fireEvent.keyDown(input, { key: 'Enter' });
        expect(onOther).not.toHaveBeenCalled();
        expect((screen.getByTestId('desktop-bot-other-send') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(input, { target: { value: '  转发到朋友圈 ' } });
        fireEvent.keyDown(input, { key: 'Enter' });
        expect(onOther).toHaveBeenCalledWith('转发到朋友圈');
        expect(screen.queryByTestId('desktop-bot-other-input')).toBeNull();
    });

    it('send button submits and Esc cancels', () => {
        const { onOther } = setup('en');
        const other = screen.getByTestId('desktop-bot-option-other');
        expect(other.textContent).toBe('Other');
        fireEvent.click(other);
        fireEvent.change(screen.getByTestId('desktop-bot-other-input'), { target: { value: 'x' } });
        fireEvent.keyDown(screen.getByTestId('desktop-bot-other-input'), { key: 'Escape' });
        expect(screen.queryByTestId('desktop-bot-other-input')).toBeNull();
        expect(onOther).not.toHaveBeenCalled();
        fireEvent.click(other);
        fireEvent.change(screen.getByTestId('desktop-bot-other-input'), { target: { value: 'reshare it' } });
        fireEvent.click(screen.getByTestId('desktop-bot-other-send'));
        expect(onOther).toHaveBeenCalledWith('reshare it');
    });
});
