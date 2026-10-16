import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { PPTStyleChooser } from '../PPTStyleChooser';

const styles = [
    { id: 'business', label: '商务汇报', keywords: ['商务'], accent: '0E7C73' },
    { id: 'warm', label: '温馨纪念', keywords: ['生日'], accent: 'C4785A' },
    { id: 'academic', label: '学术答辩', keywords: ['答辩'], accent: '7C2F3B' },
];

describe('PPTStyleChooser', () => {
    it('highlights the purpose match and lets the user pick another style', async () => {
        const onChoice = vi.fn();
        (window as unknown as { go: { main: { App: { ListPPTStyleChoices: () => Promise<typeof styles> } } } }).go = {
            main: { App: { ListPPTStyleChoices: () => Promise.resolve(styles) } },
        };
        const view = render(<PPTStyleChooser lang="zh-Hans" inputValue="生日纪念" onChoice={onChoice} />);
        expect((await screen.findByRole('radio', { name: '温馨纪念，推荐' })).getAttribute('aria-checked')).toBe('true');
        fireEvent.click(screen.getByRole('radio', { name: '学术答辩' }));
        expect(screen.getByRole('radio', { name: '学术答辩' }).getAttribute('aria-checked')).toBe('true');
        expect(onChoice).toHaveBeenLastCalledWith({ id: 'academic', label: '学术答辩' });

        view.rerender(<PPTStyleChooser lang="zh-Hans" inputValue="答辩提纲" onChoice={onChoice} />);
        expect(screen.getByRole('radio', { name: '学术答辩，推荐' }).getAttribute('aria-checked')).toBe('true');
        fireEvent.click(screen.getByRole('button', { name: '跟随用途' }));
        expect(screen.getByRole('radio', { name: '学术答辩，推荐' }).getAttribute('aria-checked')).toBe('true');
    });
});
