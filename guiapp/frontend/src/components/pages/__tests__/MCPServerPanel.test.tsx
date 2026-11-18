// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

const { showConfirm, mcpEventHandlers } = vi.hoisted(() => ({
    showConfirm: vi.fn().mockResolvedValue(true),
    mcpEventHandlers: [] as Array<{ name: string; handler: (...args: unknown[]) => void }>,
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showConfirm,
        showAlert: async () => undefined,
        showPrompt: async () => null,
    }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((name: string, handler: (...args: unknown[]) => void) => {
        mcpEventHandlers.push({ name, handler });
        return () => {
            const at = mcpEventHandlers.findIndex((entry) => entry.name === name && entry.handler === handler);
            if (at >= 0) mcpEventHandlers.splice(at, 1);
        };
    }),
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
const lanStatus = {
    ...onStatus,
    lanEnabled: true,
    lanURL: 'http://192.168.1.20:18790/mcp',
    lanHTTPConfig: '{"mcpServers":{"maclaw":{"url":"http://192.168.1.20:18790/mcp","headers":{"Authorization":"Bearer mck_test_key_value"}}}}',
};

const accessEntries = [
    {
        time: '2026-11-18 10:00:00',
        ip: '192.168.1.30',
        apiKey: 'mck_access…6789',
        action: 'send_command',
        task: '修复登录页',
        projectPath: 'D:\\work\\app',
    },
    {
        time: '2026-11-18 09:55:00',
        ip: '127.0.0.1',
        apiKey: 'mck_012345…0123',
        action: 'create_task',
        task: '访问日志任务',
        projectPath: '',
    },
];

function install(status: Record<string, unknown> = offStatus, entries: Record<string, unknown>[] = []) {
    const patch = vi.fn().mockResolvedValue({});
    const generate = vi.fn().mockResolvedValue({ ...onStatus, apiKey: 'mck_rotated_key' });
    const getStatus = vi.fn().mockResolvedValue(status);
    let accessLog = entries;
    // Return a copy each call: the backend builds a fresh slice per request,
    // and a same-reference resolve would let React bail out of the re-render.
    const getAccessLog = vi.fn(() => Promise.resolve(accessLog.slice()));
    const clearAccessLog = vi.fn(async () => { accessLog = []; });
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                ListExperts: vi.fn().mockResolvedValue('[]'),
                GetACPHostStatus: vi.fn().mockRejectedValue(new Error('no backend')),
                GetMCPHostStatus: getStatus,
                PatchConfigFields: patch,
                GenerateMCPHostAPIKey: generate,
                GetMCPAccessLog: getAccessLog,
                ClearMCPAccessLog: clearAccessLog,
            },
        },
    };
    return { patch, generate, getStatus, getAccessLog, clearAccessLog };
}

describe('MCP server tool card', () => {
    afterEach(() => {
        delete (window as unknown as { go?: unknown }).go;
        showConfirm.mockReset();
        showConfirm.mockResolvedValue(true);
        mcpEventHandlers.length = 0;
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
        expect(screen.getByTestId('mcp-server-tools').textContent).toContain('send_command');
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

    it('shows the LAN http config while lan access is on and patches the lan switch', async () => {
        const { patch } = install(lanStatus);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-http-lan')).toBeTruthy());
        expect((screen.getByTestId('mcp-server-http-lan') as HTMLTextAreaElement).value).toContain('192.168.1.20:18790');
        expect(screen.getByTestId('mcp-server-lan').getAttribute('aria-checked')).toBe('true');
        expect(screen.getByTestId('mcp-server-lan-block').textContent).toContain('HTTP（局域网）');
        fireEvent.click(screen.getByTestId('mcp-server-lan'));
        await waitFor(() => expect(patch).toHaveBeenCalledWith({ mcp_host_lan_enabled: false }));
    });

    it('keeps the lan config hidden while lan access is off', async () => {
        install(onStatus);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-http')).toBeTruthy());
        expect(screen.getByTestId('mcp-server-lan').getAttribute('aria-checked')).toBe('false');
        expect(screen.queryByTestId('mcp-server-http-lan')).toBeNull();
        expect(screen.queryByTestId('mcp-server-lan-block')).toBeNull();
    });

    it('reports a missing lan address instead of an empty block', async () => {
        install({ ...lanStatus, lanURL: undefined, lanHTTPConfig: undefined });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-lan-block')).toBeTruthy());
        expect(screen.queryByTestId('mcp-server-http-lan')).toBeNull();
        expect(screen.getByTestId('mcp-server-lan-block').textContent).toContain('没有在这台电脑上找到局域网地址');
    });

    it('renders access entries with caller ip, masked key, and task name', async () => {
        install(onStatus, accessEntries);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-access-log')).toBeTruthy());
        const rows = screen.getAllByTestId('mcp-server-access-row');
        expect(rows.length).toBe(2);
        expect(rows[0].textContent).toContain('192.168.1.30');
        expect(rows[0].textContent).toContain('mck_access…6789');
        expect(rows[0].textContent).toContain('修复登录页');
        expect(rows[1].textContent).toContain('127.0.0.1');
        expect(rows[1].textContent).toContain('create_task');
        expect(screen.queryByTestId('mcp-server-access-empty')).toBeNull();
        expect((screen.getByTestId('mcp-server-access-clear') as HTMLButtonElement).disabled).toBe(false);
    });

    it('confirms before clearing the access log and shows the empty state after', async () => {
        const { clearAccessLog } = install(onStatus, accessEntries);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getAllByTestId('mcp-server-access-row').length).toBe(2));
        fireEvent.click(screen.getByTestId('mcp-server-access-clear'));
        await waitFor(() => expect(showConfirm).toHaveBeenCalled());
        await waitFor(() => expect(clearAccessLog).toHaveBeenCalled());
        await waitFor(() => expect(screen.getByTestId('mcp-server-access-empty')).toBeTruthy());
        expect(screen.queryByTestId('mcp-server-access-row')).toBeNull();
    });

    it('keeps the access log when the clear confirmation is cancelled', async () => {
        showConfirm.mockResolvedValue(false);
        const { clearAccessLog } = install(onStatus, accessEntries);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-access-clear')).toBeTruthy());
        fireEvent.click(screen.getByTestId('mcp-server-access-clear'));
        await waitFor(() => expect(showConfirm).toHaveBeenCalled());
        expect(clearAccessLog).not.toHaveBeenCalled();
        expect(screen.getAllByTestId('mcp-server-access-row').length).toBe(2);
    });

    it('shows the access empty state when nothing was recorded yet', async () => {
        install(onStatus, []);
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getByTestId('mcp-server-access-empty')).toBeTruthy());
        expect((screen.getByTestId('mcp-server-access-clear') as HTMLButtonElement).disabled).toBe(true);
    });

    it('refreshes the access log when a mcp-host-message arrives and unsubscribes on unmount', async () => {
        const list = [accessEntries[0]];
        install(onStatus, list);
        const { unmount } = render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-mcp-server-card'));
        await waitFor(() => expect(screen.getAllByTestId('mcp-server-access-row').length).toBe(1));
        const handler = mcpEventHandlers.find((entry) => entry.name === 'mcp-host-message');
        expect(handler).toBeTruthy();
        list.push(accessEntries[1]);
        handler?.handler();
        await waitFor(() => expect(screen.getAllByTestId('mcp-server-access-row').length).toBe(2));
        expect(screen.getByText('访问日志任务')).toBeTruthy();
        unmount();
        expect(mcpEventHandlers.find((entry) => entry.name === 'mcp-host-message')).toBeUndefined();
    });
});
