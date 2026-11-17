// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

const { showConfirm } = vi.hoisted(() => ({
    showConfirm: vi.fn().mockResolvedValue(true),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showConfirm,
        showAlert: async () => undefined,
        showPrompt: async () => null,
    }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
    BrowserOpenURL: vi.fn(),
    ClipboardSetText: vi.fn().mockResolvedValue(true),
}));

const offStatus = { enabled: false, running: false };
const onStatus = {
    enabled: true,
    running: true,
    url: 'http://127.0.0.1:18790/mcp',
    port: 18790,
    apiKey: 'mck_test_key_value',
    command: 'MaClaw.exe',
    stdioConfig: '{"mcpServers":{"maclaw":{"command":"MaClaw.exe","args":["mcp"]}}}',
    httpConfig: '{"mcpServers":{"maclaw":{"url":"http://127.0.0.1:18790/mcp","headers":{"Authorization":"Bearer mck_test_key_value"}}}}',
};

function install(status: Record<string, unknown> = offStatus) {
    const patch = vi.fn().mockResolvedValue({});
    const generate = vi.fn().mockResolvedValue({ ...onStatus, apiKey: 'mck_rotated_key' });
    const getStatus = vi.fn().mockResolvedValue(status);
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                ListExperts: vi.fn().mockResolvedValue('[]'),
                GetACPHostStatus: vi.fn().mockRejectedValue(new Error('no backend')),
                GetMCPHostStatus: getStatus,
                PatchConfigFields: patch,
                GenerateMCPHostAPIKey: generate,
            },
        },
    };
    return { patch, generate, getStatus };
}

describe('MCP server tool card', () => {
    afterEach(() => {
        delete (window as unknown as { go?: unknown }).go;
        showConfirm.mockReset();
        showConfirm.mockResolvedValue(true);
    });

    it('opens the panel and hides the key while the server is off', async () => {
        install(offStatus);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        expect(screen.getByTestId('utilities-mcp-server-card')).toBeTruthy();
        expect(screen.getByText('MCP服务器')).toBeTruthy();
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-panel')).toBeTruthy());
        expect(screen.queryByTestId('mcp-server-apikey')).toBeNull();
        expect(screen.queryByTestId('mcp-server-stdio')).toBeNull();
        expect(screen.queryByTestId('mcp-server-http')).toBeNull();
        expect(screen.getByText(/会话服务/)).toBeTruthy();
    });

    it('shows stdio and http config after the server is listening', async () => {
        install(onStatus);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-stdio')).toBeTruthy());
        expect((screen.getByTestId('mcp-server-apikey') as HTMLInputElement).value).toContain('mck_test_key_value');
        expect((screen.getByTestId('mcp-server-http') as HTMLTextAreaElement).value).toContain('Authorization');
        expect(screen.getByTestId('mcp-server-tools').textContent).toContain('list_models');
        expect(screen.getByTestId('mcp-server-tools').textContent).toContain('create_task');
    });

    it('patches the switch and confirms before generating a key', async () => {
        const { patch, generate } = install(onStatus);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-enabled')).toBeTruthy());
        fireEvent.click(screen.getByTestId('mcp-server-enabled'));
        await waitFor(() => expect(patch).toHaveBeenCalledWith({ mcp_host_enabled: false }));
        fireEvent.click(screen.getByTestId('mcp-server-generate-key'));
        await waitFor(() => expect(showConfirm).toHaveBeenCalled());
        await waitFor(() => expect(generate).toHaveBeenCalled());
    });

    it('does not generate a key when confirmation is cancelled', async () => {
        showConfirm.mockResolvedValue(false);
        const { generate } = install(onStatus);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-generate-key')).toBeTruthy());
        fireEvent.click(screen.getByTestId('mcp-server-generate-key'));
        await waitFor(() => expect(showConfirm).toHaveBeenCalled());
        expect(generate).not.toHaveBeenCalled();
    });
});
