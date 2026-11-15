import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { BotCapabilitySettingsDialog } from './BotCapabilitySettingsDialog';

const { getPage, update, importMCP } = vi.hoisted(() => ({
    getPage: vi.fn(),
    update: vi.fn(),
    importMCP: vi.fn(),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetDesktopBotCapabilityPage: () => getPage(),
    UpdateDesktopBotCapabilities: (body: unknown) => update(body),
    ImportDesktopBotMCP: (source: string) => importMCP(source),
}));

const page = {
    mcp: [{ id: 'search', name: 'Search', transport: 'http', url: 'https://mcp.example/mcp', secret_set: true }],
    skills: [{ name: 'draft', description: 'save a draft', local: true, enabled: false }],
};

beforeEach(() => {
    getPage.mockReset();
    update.mockReset();
    importMCP.mockReset();
    getPage.mockResolvedValue(page);
    update.mockResolvedValue(page);
    importMCP.mockResolvedValue(page);
});

describe('BotCapabilitySettingsDialog', () => {
    it('copies MCP from this computer and enables a local skill', async () => {
        render(<BotCapabilitySettingsDialog heading="设置" lang="zh" onClose={() => {}} />);
        const row = await screen.findByTestId('bot-capability-mcp-search');
        expect(row.textContent).toContain('https://mcp.example/mcp');
        fireEvent.click(screen.getByTestId('bot-capability-copy'));
        await waitFor(() => expect(importMCP).toHaveBeenCalledWith('gui'));

        fireEvent.click(screen.getByTestId('bot-capability-skill-toggle-draft'));
        await waitFor(() => expect(update).toHaveBeenCalledWith({ enable_skills: ['draft'] }));

        fireEvent.click(screen.getByTestId('bot-capability-mcp-remove-search'));
        await waitFor(() => expect(update).toHaveBeenCalledWith({ remove_mcp: ['search'] }));
    });

    it('installs a new skill from text', async () => {
        render(<BotCapabilitySettingsDialog heading="设置" lang="zh" onClose={() => {}} />);
        await screen.findByTestId('bot-capability-mcp-search');
        fireEvent.click(screen.getByTestId('bot-capability-new-skill'));
        fireEvent.change(screen.getByTestId('bot-capability-skill-name'), { target: { value: 'notes' } });
        fireEvent.change(screen.getByTestId('bot-capability-skill-body'), { target: { value: 'Keep a note.' } });
        fireEvent.click(screen.getByTestId('bot-capability-skill-save'));
        await waitFor(() => expect(update).toHaveBeenCalledWith({
            add_skills: [{ name: 'notes', description: '', body: 'Keep a note.' }],
        }));
    });
});
