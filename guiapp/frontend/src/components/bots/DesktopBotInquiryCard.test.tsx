import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { askUserAllowsMultiple, DesktopBotInquiryCard } from './DesktopBotInquiryCard';

describe('DesktopBotInquiryCard', () => {
    it('single-select submits the option text immediately', () => {
        const onSubmit = vi.fn();
        render(
            <DesktopBotInquiryCard
                question="InferX 怎么处理?"
                options={['继续用 InferX', '换成本地路径']}
                lang="zh-Hans"
                onSubmit={onSubmit}
            />,
        );
        expect(screen.getByTestId('desktop-bot-inquiry-prompt').textContent).toBe('InferX 怎么处理?');
        fireEvent.click(screen.getAllByTestId('desktop-bot-inquiry-option')[1]);
        expect(onSubmit).toHaveBeenCalledWith('换成本地路径');
    });

    it('multi-select waits for confirm and joins with a Chinese semicolon', () => {
        const onSubmit = vi.fn();
        render(
            <DesktopBotInquiryCard
                question="哪些都要?"
                options={['A 路径', 'B 路径', 'C 路径']}
                allowMultiple
                lang="zh-Hans"
                onSubmit={onSubmit}
            />,
        );
        const buttons = screen.getAllByTestId('desktop-bot-inquiry-option');
        fireEvent.click(buttons[0]);
        fireEvent.click(buttons[2]);
        expect(onSubmit).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('desktop-bot-inquiry-submit'));
        expect(onSubmit).toHaveBeenCalledWith('A 路径；C 路径');
    });

    it('detects multi_choice from input_type', () => {
        expect(askUserAllowsMultiple('multi_choice')).toBe(true);
        expect(askUserAllowsMultiple('choice')).toBe(false);
        expect(askUserAllowsMultiple('confirm')).toBe(false);
    });
});
