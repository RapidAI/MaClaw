import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PDFStylesSettingsPanel } from '../PDFStylesSettingsPanel';

const list = vi.fn();
const choose = vi.fn();
const generate = vi.fn();
const remove = vi.fn();

const gazette = {
    id: 'gazette',
    label: '公报',
    label_en: 'Gazette',
    summary: '海军蓝顶栏',
    summary_en: 'Navy masthead',
    motif: 'masthead',
    builtin: true,
    active: true,
    navy: '0E2A47',
    accent: 'C2410C',
    gold: 'A16207',
    ink: '1C1917',
};

const harbor = {
    id: 'harbor',
    label: '港务',
    label_en: 'Harbor',
    summary: '左侧立柱',
    summary_en: 'Left rail',
    motif: 'rail',
    builtin: false,
    active: false,
    navy: '134E4A',
    accent: '0F766E',
    gold: 'B45309',
    ink: '1C1917',
};

const catalog: Array<typeof gazette> = [];

function resetCatalog(rows: Array<typeof gazette>) {
    catalog.splice(0, catalog.length, ...rows.map((row) => ({ ...row })));
}

beforeEach(() => {
    list.mockReset();
    choose.mockReset();
    generate.mockReset();
    remove.mockReset();
    resetCatalog([gazette, harbor]);
    list.mockImplementation(async () => ({ styles: catalog.map((row) => ({ ...row })) }));
    choose.mockImplementation(async (id: string) => {
        catalog.forEach((row) => {
            row.active = row.id === id;
        });
    });
    generate.mockResolvedValue({ ...harbor, id: 'night', label: '夜航', active: false });
    remove.mockImplementation(async (id: string) => {
        const index = catalog.findIndex((row) => row.id === id);
        if (index < 0) return;
        const wasActive = catalog[index].active;
        catalog.splice(index, 1);
        if (wasActive) {
            catalog.forEach((row, rowIndex) => {
                row.active = rowIndex === 0;
            });
        }
    });
    (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go = {
        main: {
            App: {
                ListPDFStyles: list,
                SetPDFStyle: choose,
                GeneratePDFStyle: generate,
                DeletePDFStyle: remove,
            },
        },
    };
});

describe('PDFStylesSettingsPanel', () => {
    it('shows the style gallery and marks the active style', async () => {
        render(<PDFStylesSettingsPanel lang="zh-Hans" />);
        const gazette = await screen.findByRole('button', { name: /公报/ });
        expect(gazette.getAttribute('aria-pressed')).toBe('true');
        expect(gazette.textContent).toContain('当前');
        expect(screen.getByRole('button', { name: /^港务/ })).toBeTruthy();
        expect(screen.getByRole('button', { name: /删除港务/ })).toBeTruthy();
    });

    it('selects a style for later PDFs', async () => {
        render(<PDFStylesSettingsPanel lang="en" />);
        fireEvent.click(await screen.findByRole('button', { name: /Gazette/ }));
        await waitFor(() => expect(choose).toHaveBeenCalledWith('gazette'));
    });

    it('asks the model for another style and can delete a custom one', async () => {
        render(<PDFStylesSettingsPanel lang="zh-Hans" />);
        await screen.findByRole('button', { name: /^港务/ });
        fireEvent.click(screen.getByRole('button', { name: /删除港务/ }));
        await waitFor(() => expect(remove).toHaveBeenCalledWith('harbor'));
        await waitFor(() => expect(screen.queryByRole('button', { name: /^港务/ })).toBeNull());

        fireEvent.change(screen.getByLabelText('描述想要的风格'), { target: { value: '酒红案卷' } });
        fireEvent.click(screen.getByRole('button', { name: '生成风格' }));
        await waitFor(() => expect(generate).toHaveBeenCalledWith('酒红案卷'));
    });

    it('shows the built-in gallery when the style file cannot be read', async () => {
        list.mockResolvedValueOnce({
            styles: [{ ...gazette }],
            warning: '风格文件无法读取，已暂时使用默认公报。原文件未改动。',
        });
        render(<PDFStylesSettingsPanel lang="zh-Hans" />);
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('风格文件无法读取');
        expect(screen.getByRole('button', { name: /公报/ }).getAttribute('aria-pressed')).toBe('true');
    });

    it('returns to the first style when the active custom style is deleted', async () => {
        resetCatalog([{ ...gazette, active: false }, { ...harbor, active: true }]);
        render(<PDFStylesSettingsPanel lang="zh-Hans" />);
        fireEvent.click(await screen.findByRole('button', { name: /删除港务/ }));
        await waitFor(() => {
            expect(screen.getByRole('button', { name: /公报/ }).getAttribute('aria-pressed')).toBe('true');
        });
        expect(screen.queryByRole('button', { name: /^港务/ })).toBeNull();
    });
});
