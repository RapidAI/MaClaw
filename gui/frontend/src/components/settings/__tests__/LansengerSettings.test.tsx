// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LansengerSettings } from '../LansengerSettings';

const RestartLansengerMock = vi.fn();
const SetLansengerLocalModeMock = vi.fn();
const LoadConfigMock = vi.fn();
const ListLansengerGroupsMock = vi.fn();
const KnowledgeListSourcesMock = vi.fn();
const SelectVEAllowedDirectoryMock = vi.fn();

beforeEach(() => {
    KnowledgeListSourcesMock.mockReset();
    SelectVEAllowedDirectoryMock.mockReset();
    KnowledgeListSourcesMock.mockResolvedValue([]);
});

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    RestartLansenger: (...args: unknown[]) => RestartLansengerMock(...args),
    SetLansengerLocalMode: (...args: unknown[]) => SetLansengerLocalModeMock(...args),
    ListLansengerGroups: (...args: unknown[]) => ListLansengerGroupsMock(...args),
    KnowledgeListSources: (...args: unknown[]) => KnowledgeListSourcesMock(...args),
    SelectVEAllowedDirectory: (...args: unknown[]) => SelectVEAllowedDirectoryMock(...args),
    SetLansengerGroupIgnored: vi.fn().mockResolvedValue(undefined),
    SetLansengerGroupAllowed: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../pages/UtilitiesWatchPanel', () => ({
    UtilitiesWatchPanel: ({
        onBack,
        compactHeader,
    }: {
        isZh: boolean;
        onBack: () => void;
        compactHeader?: boolean;
    }) => (
        <div data-testid="watch-page" data-compact={compactHeader ? '1' : '0'}>
            <button type="button" onClick={onBack}>
                back
            </button>
        </div>
    ),
}));

const baseProps = () => ({
    config: {
        lansenger_enabled: true,
        lansenger_app_id: 'app-id',
        lansenger_app_secret: 'secret',
        lansenger_gateway_url: 'https://apigw.lx.qianxin.com',
    } as any,
    setConfig: vi.fn(),
    lang: 'zh-Hans',
    saveRemoteConfigField: vi.fn(),
    lansengerStatus: 'connected',
    setLansengerStatus: vi.fn(),
    lansengerLocalMode: true,
    setLansengerLocalModeState: vi.fn(),
    setIMAuditPlatform: vi.fn(),
});

describe('LansengerSettings', () => {
    it('saves fields and exposes restart and audit actions', async () => {
        const props = baseProps();
        RestartLansengerMock.mockResolvedValue('connected');

        render(<LansengerSettings {...props} />);

        fireEvent.click(screen.getByLabelText('\u542f\u7528\u84dd\u4fe1'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_enabled: false });

        fireEvent.change(screen.getByDisplayValue('app-id'), { target: { value: 'new-app' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_app_id: 'new-app' });

        fireEvent.click(screen.getByRole('button', { name: '\u91cd\u542f' }));
        await waitFor(() => expect(props.setLansengerStatus).toHaveBeenCalledWith('connected'));

        fireEvent.click(screen.getByRole('button', { name: '\u76d1\u770b' }));
        expect(props.setIMAuditPlatform).toHaveBeenCalledWith('lansenger');

        // Group-chat options must go through saveRemoteConfigField (atomic whitelist).
        fireEvent.change(screen.getByLabelText('\u7fa4\u804a\u7b56\u7565'), { target: { value: 'allowlist' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_policy: 'allowlist' });
        fireEvent.click(screen.getByText('\u9700\u8981 @\u63d0\u53ca'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_require_mention: false });
    });

    it('edits the local-only group permission allowlists', async () => {
        const props = baseProps();
        KnowledgeListSourcesMock.mockResolvedValue([{ id: 'knowledge-a', title: '知识库 A', kind: 'folder' }]);
        SelectVEAllowedDirectoryMock.mockResolvedValue('D:\\approved');

        const first = render(<LansengerSettings {...props} />);

        const source = await screen.findByText('知识库 A');
        fireEvent.click(source);
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_knowledge_source_ids: ['knowledge-a'] });

        fireEvent.click(screen.getByText('允许所有目录'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_allow_all_directories: true });

        // A fresh mount models the persisted controlled config after toggling.
        first.unmount();
        const directoryProps = baseProps();
        render(<LansengerSettings {...directoryProps} config={{ ...directoryProps.config, lansenger_group_allowed_directories: [] } as any} />);
        fireEvent.click(screen.getByRole('button', { name: '添加目录' }));
        await waitFor(() => expect(SelectVEAllowedDirectoryMock).toHaveBeenCalled());
        await waitFor(() => expect(directoryProps.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_allowed_directories: ['D:\\approved'] }));
    });

    it('shows Follow only when Lansenger is connected, after Watch', async () => {
        const { rerender } = render(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();

        rerender(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        const follow = screen.getByTestId('lansenger-follow-button');
        expect(follow).toBeTruthy();
        expect(follow.textContent).toBe('\u76ef\u4eba');

        // Follow sits after 监看 in the toolbar
        const watchBtn = screen.getByRole('button', { name: '\u76d1\u770b' });
        expect(watchBtn.compareDocumentPosition(follow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(follow);
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
    });

    it('closes Follow panel when connection drops', async () => {
        const { rerender } = render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        rerender(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        await waitFor(() => {
            expect(screen.queryByTestId('watch-page')).toBeNull();
            expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        });
    });

    it('closes Follow panel via Escape', async () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
    });

    it('opens Follow in compact embed mode and closes via header ×', async () => {
        ListLansengerGroupsMock.mockResolvedValue({ total: 0, groups: [] });
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        const page = await screen.findByTestId('watch-page');
        expect(page.getAttribute('data-compact')).toBe('1');
        expect(screen.getByRole('dialog', { name: '\u76ef\u4eba' })).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '\u5173\u95ed' }));
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
    });

    it('does not open Follow when disconnected', () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        expect(screen.queryByTestId('watch-page')).toBeNull();
    });

    it('locks body scroll while Follow dialog is open', async () => {
        const prev = document.body.style.overflow;
        document.body.style.overflow = '';
        try {
            render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
            fireEvent.click(screen.getByTestId('lansenger-follow-button'));
            await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
            expect(document.body.style.overflow).toBe('hidden');

            fireEvent.keyDown(window, { key: 'Escape' });
            await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
            expect(document.body.style.overflow).toBe('');
        } finally {
            document.body.style.overflow = prev;
        }
    });

    it('does not close Follow on Escape during IME composition', async () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        fireEvent.keyDown(window, { key: 'Escape', isComposing: true });
        expect(screen.getByTestId('watch-page')).toBeTruthy();

        fireEvent.keyDown(window, { key: 'Escape', keyCode: 229 });
        expect(screen.getByTestId('watch-page')).toBeTruthy();
    });
});
