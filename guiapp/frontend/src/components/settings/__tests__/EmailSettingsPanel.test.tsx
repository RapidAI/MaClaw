// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { showConfirmMock } = vi.hoisted(() => ({
    showConfirmMock: vi.fn().mockResolvedValue(false),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    MailboxAccountDelete: vi.fn(),
    MailboxAccountSave: vi.fn(),
    MailboxAccountSetDefault: vi.fn(),
    MailboxAccountSetEnabled: vi.fn(),
    MailboxAccountSetSecret: vi.fn(),
    MailboxAccountTestDraft: vi.fn(),
    MailboxAccountsList: vi.fn(),
    MailboxBrowseFolders: vi.fn(),
    MailboxBrowseMessages: vi.fn(),
    MailboxBrowseRead: vi.fn(),
    MailboxBrowseStart: vi.fn(),
    MailboxOAuthLogin: vi.fn(),
    MailboxSignatureDelete: vi.fn(),
    MailboxSignatureSave: vi.fn(),
    MailboxSignatureSetDefault: vi.fn(),
    MailboxSignaturesList: vi.fn(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: showConfirmMock }),
}));

import {
    MailboxAccountDelete,
    MailboxAccountSave,
    MailboxAccountSetEnabled,
    MailboxAccountSetSecret,
    MailboxAccountTestDraft,
    MailboxAccountsList,
    MailboxBrowseMessages,
    MailboxBrowseRead,
    MailboxBrowseStart,
    MailboxOAuthLogin,
    MailboxSignatureDelete,
    MailboxSignatureSave,
    MailboxSignatureSetDefault,
    MailboxSignaturesList,
} from '../../../../wailsjs/go/main/App';
import { EmailSettingsPanel } from '../EmailSettingsPanel';

const account = {
    id: 'mb1',
    name: 'Work',
    address: 'w@ex.com',
    enabled: true,
    default: true,
    has_inbound: true,
    has_outbound: true,
    has_inbound_secret: true,
    has_outbound_secret: true,
    signature_id: '',
    inbound: { host: 'imap.example.com', port: 993, security: 'tls', username: 'w', box: 'Sent' },
    outbound: { host: 'smtp.example.com', port: 587, security: 'starttls', username: 'w', from_address: 'w@ex.com' },
};

describe('EmailSettingsPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        showConfirmMock.mockResolvedValue(false);
        vi.mocked(MailboxAccountsList).mockResolvedValue([account] as never);
        vi.mocked(MailboxSignaturesList).mockResolvedValue([] as never);
        vi.mocked(MailboxAccountSave).mockResolvedValue('mb1' as never);
    });

    it('loads the saved servers into the editor and does not use window.confirm', async () => {
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        const hosts = screen.getAllByPlaceholderText('主机') as HTMLInputElement[];
        expect(hosts[0].value).toBe('imap.example.com');
        expect(hosts[1].value).toBe('smtp.example.com');
        expect((screen.getByPlaceholderText('文件夹') as HTMLInputElement).value).toBe('Sent');

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        const payload = vi.mocked(MailboxAccountSave).mock.calls[0][0] as {
            inbound?: { host?: string; box?: string };
            outbound?: { host?: string };
            default?: boolean;
        };
        expect(payload.inbound?.host).toBe('imap.example.com');
        expect(payload.inbound?.box).toBe('Sent');
        expect(payload.outbound?.host).toBe('smtp.example.com');
        expect(payload.default).toBe(true);

        fireEvent.click(screen.getByRole('button', { name: '删除' }));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(String(showConfirmMock.mock.calls[0][0])).toContain('Work');
        expect(confirm).not.toHaveBeenCalled();
        expect(MailboxAccountDelete).not.toHaveBeenCalled();
        confirm.mockRestore();
    });

    it('sends default false when the public-default checkbox is cleared', async () => {
        vi.mocked(MailboxSignaturesList).mockResolvedValue([
            { id: 's1', name: '公共', body: 'hello', default: false },
            { id: 's2', name: '个人', body: 'yo', default: true },
        ] as never);
        vi.mocked(MailboxSignatureSave).mockResolvedValue('s2' as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findAllByText('个人');

        const editButtons = screen.getAllByRole('button', { name: '编辑' });
        // Mailboxes render before signatures, so the signature rows are the last two edit buttons.
        fireEvent.click(editButtons[2]);
        fireEvent.click(screen.getByRole('checkbox', { name: '公共默认签名' }));
        fireEvent.click(screen.getByRole('button', { name: '保存签名' }));

        await waitFor(() => expect(MailboxSignatureSave).toHaveBeenCalled());
        expect(vi.mocked(MailboxSignatureSave).mock.calls[0][0]).toMatchObject({
            id: 's2',
            name: '个人',
            body: 'yo',
            default: false,
        });
    });

    it('drops a deleted mailbox from the editor so the next save does not reuse its id', async () => {
        let listed = [account];
        vi.mocked(MailboxAccountsList).mockImplementation(async () => listed as never);
        vi.mocked(MailboxAccountDelete).mockImplementation(async () => {
            listed = [];
        });
        showConfirmMock.mockResolvedValue(true);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        expect(screen.getByText('正在编辑“Work”')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '取消' }));
        expect(screen.queryByText('正在编辑“Work”')).toBeNull();
        expect((screen.getAllByPlaceholderText('主机')[0] as HTMLInputElement).value).toBe('');

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        fireEvent.click(screen.getByRole('button', { name: '删除' }));
        await waitFor(() => expect(MailboxAccountDelete).toHaveBeenCalledWith('mb1'));
        await waitFor(() => expect(screen.queryByText('正在编辑“Work”')).toBeNull());

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        const payload = vi.mocked(MailboxAccountSave).mock.calls.at(-1)?.[0] as { id?: string };
        expect(payload.id).toBeUndefined();
    });

    it('retries a password against the mailbox just created when the keyring write fails', async () => {
        const listed = [account];
        vi.mocked(MailboxAccountsList).mockImplementation(async () => listed as never);
        vi.mocked(MailboxAccountSave).mockImplementation(async () => {
            if (!listed.some(item => item.id === 'mb-new')) {
                listed.push({ ...account, id: 'mb-new', name: 'Home', address: 'h@ex.com' });
            }
            return 'mb-new' as never;
        });
        vi.mocked(MailboxAccountSetSecret).mockRejectedValue(new Error('系统密钥环不可用，密码没有保存'));
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.change(screen.getByRole('textbox', { name: '名字' }), { target: { value: 'Home' } });
        fireEvent.change(screen.getByRole('textbox', { name: '邮箱地址' }), { target: { value: 'h@ex.com' } });
        fireEvent.change(screen.getAllByPlaceholderText('主机')[0], { target: { value: 'imap.example.com' } });
        fireEvent.change(screen.getAllByPlaceholderText('密码或授权码，留空则不修改')[0], { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await screen.findByText('系统密钥环不可用，密码没有保存');

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalledTimes(2));
        expect(vi.mocked(MailboxAccountSave).mock.calls[1][0]).toMatchObject({ id: 'mb-new', name: 'Home' });
    });

    it('keeps a pause when the open mailbox is saved afterwards', async () => {
        let listed = [account];
        vi.mocked(MailboxAccountsList).mockImplementation(async () => listed as never);
        vi.mocked(MailboxAccountSetEnabled).mockImplementation(async () => {
            listed = [{ ...account, enabled: false, default: false }];
        });
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        fireEvent.click(screen.getByRole('button', { name: '停用' }));
        await waitFor(() => expect((screen.getByRole('checkbox', { name: '启用' }) as HTMLInputElement).checked).toBe(false));

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        expect(vi.mocked(MailboxAccountSave).mock.calls.at(-1)?.[0]).toMatchObject({
            id: 'mb1',
            enabled: false,
            default: false,
        });
    });

    it('does not save a signature that was deleted while the mailbox stayed open', async () => {
        let signatures = [{ id: 's2', name: '个人', body: 'yo', default: true }];
        const listed = [{ ...account, signature_id: 's2' }];
        vi.mocked(MailboxSignaturesList).mockImplementation(async () => signatures as never);
        vi.mocked(MailboxAccountsList).mockImplementation(async () => listed as never);
        vi.mocked(MailboxSignatureDelete).mockImplementation(async () => {
            signatures = [];
            listed[0] = { ...account, signature_id: '' };
        });
        showConfirmMock.mockResolvedValue(true);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findAllByText('个人');

        // Mailboxes render before signatures, so the account row owns the first edit/delete buttons.
        fireEvent.click(screen.getAllByRole('button', { name: '编辑' })[0]);
        const signatureSelect = () => screen.getByRole('option', { name: '使用公共默认签名' }).closest('select') as HTMLSelectElement;
        expect(signatureSelect().value).toBe('s2');
        fireEvent.click(screen.getAllByRole('button', { name: '删除' })[1]);
        await waitFor(() => expect(MailboxSignatureDelete).toHaveBeenCalledWith('s2'));
        await waitFor(() => expect(signatureSelect().value).toBe(''));

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        expect(vi.mocked(MailboxAccountSave).mock.calls.at(-1)?.[0]).toMatchObject({
            id: 'mb1',
            signature_id: '',
        });
    });

    it('copies a new public default into the open signature before the next save', async () => {
        let signatures = [
            { id: 's1', name: '公共', body: 'hello', default: true },
            { id: 's2', name: '个人', body: 'yo', default: false },
        ];
        vi.mocked(MailboxAccountsList).mockResolvedValue([] as never);
        vi.mocked(MailboxSignaturesList).mockImplementation(async () => signatures as never);
        vi.mocked(MailboxSignatureSetDefault).mockImplementation(async (id: string) => {
            signatures = signatures.map(sig => ({ ...sig, default: sig.id === id }));
        });
        vi.mocked(MailboxSignatureSave).mockResolvedValue('s1' as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findAllByText('公共');

        fireEvent.click(screen.getAllByRole('button', { name: '编辑' })[0]);
        expect((screen.getByRole('checkbox', { name: '公共默认签名' }) as HTMLInputElement).checked).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: '设为公共默认' }));
        await waitFor(() => expect((screen.getByRole('checkbox', { name: '公共默认签名' }) as HTMLInputElement).checked).toBe(false));

        fireEvent.click(screen.getByRole('button', { name: '保存签名' }));
        await waitFor(() => expect(MailboxSignatureSave).toHaveBeenCalled());
        expect(vi.mocked(MailboxSignatureSave).mock.calls.at(-1)?.[0]).toMatchObject({
            id: 's1',
            name: '公共',
            body: 'hello',
            default: false,
        });
    });

    it('keeps a promoted public default on the signature that stayed open', async () => {
        let signatures = [
            { id: 's1', name: '公共', body: 'hello', default: true },
            { id: 's2', name: '个人', body: 'yo', default: false },
        ];
        vi.mocked(MailboxAccountsList).mockResolvedValue([] as never);
        vi.mocked(MailboxSignaturesList).mockImplementation(async () => signatures as never);
        vi.mocked(MailboxSignatureDelete).mockImplementation(async (id: string) => {
            signatures = signatures.filter(sig => sig.id !== id);
            if (signatures.length > 0 && !signatures.some(sig => sig.default)) {
                signatures = [{ ...signatures[0], default: true }, ...signatures.slice(1)];
            }
        });
        vi.mocked(MailboxSignatureSave).mockResolvedValue('s2' as never);
        showConfirmMock.mockResolvedValue(true);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findAllByText('个人');

        fireEvent.click(screen.getAllByRole('button', { name: '编辑' })[1]);
        expect((screen.getByRole('checkbox', { name: '公共默认签名' }) as HTMLInputElement).checked).toBe(false);
        fireEvent.click(screen.getAllByRole('button', { name: '删除' })[0]);
        await waitFor(() => expect((screen.getByRole('checkbox', { name: '公共默认签名' }) as HTMLInputElement).checked).toBe(true));

        fireEvent.click(screen.getByRole('button', { name: '保存签名' }));
        await waitFor(() => expect(MailboxSignatureSave).toHaveBeenCalled());
        expect(vi.mocked(MailboxSignatureSave).mock.calls.at(-1)?.[0]).toMatchObject({
            id: 's2',
            default: true,
        });
    });

    it('keeps a promoted mailbox default on the account that stayed open', async () => {
        const home = { ...account, id: 'mb2', name: 'Home', address: 'h@ex.com', default: false };
        let listed = [account, home];
        vi.mocked(MailboxAccountsList).mockImplementation(async () => listed as never);
        vi.mocked(MailboxAccountDelete).mockImplementation(async (id: string) => {
            listed = listed.filter(item => item.id !== id);
            if (listed.length > 0 && !listed.some(item => item.default)) {
                listed = [{ ...listed[0], default: true }, ...listed.slice(1)];
            }
        });
        showConfirmMock.mockResolvedValue(true);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Home');

        fireEvent.click(screen.getAllByRole('button', { name: '编辑' })[1]);
        expect((screen.getByRole('checkbox', { name: '默认邮箱' }) as HTMLInputElement).checked).toBe(false);
        fireEvent.click(screen.getAllByRole('button', { name: '删除' })[0]);
        await waitFor(() => expect((screen.getByRole('checkbox', { name: '默认邮箱' }) as HTMLInputElement).checked).toBe(true));

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        expect(vi.mocked(MailboxAccountSave).mock.calls.at(-1)?.[0]).toMatchObject({
            id: 'mb2',
            enabled: true,
            default: true,
        });
    });

    it('does not save an unreadable port as the protocol default', async () => {
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');
        fireEvent.change(screen.getAllByPlaceholderText('主机')[0], { target: { value: 'imap.example.com' } });
        fireEvent.change(screen.getAllByPlaceholderText('端口')[0], { target: { value: '993abc' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        expect(await screen.findByTestId('email-settings-notice')).toHaveProperty('textContent', '端口必须在 1 到 65535 之间');
        expect(MailboxAccountSave).not.toHaveBeenCalled();
    });

    it('probes the typed draft from the editor footer before saving', async () => {
        vi.mocked(MailboxAccountsList).mockResolvedValue([{ ...account, outbound: undefined, has_outbound: false }] as never);
        vi.mocked(MailboxAccountTestDraft).mockResolvedValue('收件连接正常' as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        // A blank editor offers no probes; they follow the server values in.
        expect(screen.queryByRole('button', { name: '测试收件' })).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: '编辑' }));

        // The fixture has no outbound servers, so only the receive probe shows.
        expect(screen.getByRole('button', { name: '测试收件' })).toBeTruthy();
        expect(screen.queryByRole('button', { name: '测试发件' })).toBeNull();
        expect(screen.getByText(/收件密码/)).toBeTruthy();
        expect(screen.queryByText(/发件密码/)).toBeNull();
        // Both endpoint halves of the editor always exist; only the probe
        // buttons follow the configured direction.
        const passwords = screen.getAllByPlaceholderText('密码或授权码，留空则不修改') as HTMLInputElement[];
        expect(passwords[0].autocomplete).toBe('new-password');

        fireEvent.click(screen.getByRole('button', { name: '测试收件' }));
        expect(await screen.findByText('收件连接正常')).toBeTruthy();
    });

    it('drops the draft probe result after the mailbox is saved again', async () => {
        vi.mocked(MailboxAccountTestDraft).mockResolvedValue('收件连接正常' as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');
        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        fireEvent.click(screen.getByRole('button', { name: '测试收件' }));
        expect(await screen.findByText('收件连接正常')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        // The editor reset to a new mailbox; the stale probe result must go.
        await waitFor(() => expect(screen.queryByText('收件连接正常')).toBeNull());
    });

    it('autofills the protocol default port on a security change and keeps custom ports', async () => {
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        const securities = screen.getAllByRole('combobox', { name: '安全' });
        const ports = () => screen.getAllByPlaceholderText('端口') as HTMLInputElement[];

        fireEvent.change(securities[0], { target: { value: 'starttls' } });
        expect(ports()[0].value).toBe('143');

        fireEvent.change(ports()[0], { target: { value: '1460' } });
        fireEvent.change(securities[0], { target: { value: 'none' } });
        expect(ports()[0].value).toBe('1460');

        fireEvent.change(ports()[0], { target: { value: '993' } });
        fireEvent.change(securities[0], { target: { value: 'starttls' } });
        expect(ports()[0].value).toBe('143');

        fireEvent.change(securities[1], { target: { value: 'tls' } });
        expect(ports()[1].value).toBe('465');
        fireEvent.change(securities[1], { target: { value: 'starttls' } });
        expect(ports()[1].value).toBe('587');
    });

    it('fills the known provider servers and username on preset selection', async () => {
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.change(screen.getByRole('textbox', { name: '邮箱地址' }), { target: { value: 'me@qq.com' } });
        fireEvent.change(screen.getByRole('combobox', { name: '服务商' }), { target: { value: 'qq' } });

        expect((screen.getAllByPlaceholderText('主机')[0] as HTMLInputElement).value).toBe('imap.qq.com');
        expect((screen.getAllByPlaceholderText('端口')[0] as HTMLInputElement).value).toBe('993');
        expect((screen.getAllByPlaceholderText('主机')[1] as HTMLInputElement).value).toBe('smtp.qq.com');
        expect((screen.getAllByPlaceholderText('端口')[1] as HTMLInputElement).value).toBe('465');
        expect((screen.getAllByRole('combobox', { name: '安全' })[1] as HTMLSelectElement).value).toBe('tls');
        // Both usernames mirror the still-blank username fields from the address.
        const usernames = screen.getAllByPlaceholderText('用户名') as HTMLInputElement[];
        expect(usernames[0].value).toBe('me@qq.com');
        expect(usernames[1].value).toBe('me@qq.com');
    });

    it('recognizes the saved provider when editing a matching mailbox and stays custom otherwise', async () => {
        vi.mocked(MailboxAccountsList).mockResolvedValue([
            {
                ...account,
                name: 'QQWork',
                inbound: { host: 'imap.qq.com', port: 993, security: 'tls', username: 'w', box: 'INBOX' },
                outbound: { host: 'smtp.qq.com', port: 465, security: 'tls', username: 'w', from_address: 'w@ex.com' },
            },
            account,
        ] as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.click(screen.getAllByRole('button', { name: '编辑' })[0]);
        expect((screen.getByRole('combobox', { name: '服务商' }) as HTMLSelectElement).value).toBe('qq');

        // Cancel, then edit the non-matching account: it must stay custom.
        fireEvent.click(screen.getByRole('button', { name: '取消' }));
        fireEvent.click(screen.getAllByRole('button', { name: '编辑' })[1]);
        expect((screen.getByRole('combobox', { name: '服务商' }) as HTMLSelectElement).value).toBe('custom');
    });

    it('ignores a second save while the first save is still in flight', async () => {
        const listed = [account];
        let release: ((id: string) => void) | undefined;
        vi.mocked(MailboxAccountsList).mockImplementation(async () => listed as never);
        vi.mocked(MailboxAccountSave).mockImplementation(() => new Promise(resolve => {
            release = (id: string) => {
                if (!listed.some(item => item.id === id)) {
                    listed.push({ ...account, id, name: 'Home', address: 'h@ex.com', default: false });
                }
                resolve(id as never);
            };
        }));
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.change(screen.getByRole('textbox', { name: '名字' }), { target: { value: 'Home' } });
        fireEvent.change(screen.getByRole('textbox', { name: '邮箱地址' }), { target: { value: 'h@ex.com' } });
        const save = screen.getByRole('button', { name: '保存' });
        fireEvent.click(save);
        fireEvent.click(save);
        expect(MailboxAccountSave).toHaveBeenCalledTimes(1);

        release?.('mb-new');
        await waitFor(() => expect(screen.getByTestId('email-settings-notice').textContent).toBe('已保存'));
        expect(MailboxAccountSave).toHaveBeenCalledTimes(1);
    });

    it('resets the editor to a fresh mailbox when Add is clicked mid-edit', async () => {
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        fireEvent.change(screen.getByRole('textbox', { name: '名字' }), { target: { value: '草稿' } });
        fireEvent.click(screen.getByRole('button', { name: '新增邮箱' }));

        expect((screen.getByRole('textbox', { name: '名字' }) as HTMLInputElement).value).toBe('');
        expect((screen.getByRole('checkbox', { name: '启用' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: '默认邮箱' }) as HTMLInputElement).checked).toBe(false);
        expect(screen.queryByText(/正在编辑/)).toBeNull();
        const editorEl = screen.getByRole('textbox', { name: '名字' }).closest('.email-settings-editor');
        expect(editorEl?.querySelector('.email-settings-editing')?.textContent).toBe('新增邮箱');
    });

    it('starts Gmail and Hotmail sign-in from the browser buttons', async () => {
        vi.mocked(MailboxOAuthLogin).mockResolvedValue('Gmail 邮箱 a@gmail.com 已登录' as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');

        fireEvent.click(screen.getByTestId('mailbox-login-gmail'));
        await waitFor(() => expect(MailboxOAuthLogin).toHaveBeenCalledWith('gmail', ''));
        expect(screen.getByTestId('email-settings-notice').textContent).toBe('Gmail 邮箱 a@gmail.com 已登录');

        vi.mocked(MailboxOAuthLogin).mockResolvedValue('Hotmail 邮箱 me@hotmail.com 已登录' as never);
        await waitFor(() => expect((screen.getByTestId('mailbox-login-hotmail') as HTMLButtonElement).disabled).toBe(false));
        fireEvent.click(screen.getByTestId('mailbox-login-hotmail'));
        await waitFor(() => expect(MailboxOAuthLogin).toHaveBeenLastCalledWith('hotmail', ''));
    });

    it('hides servers for a browser-login mailbox', async () => {
        vi.mocked(MailboxAccountsList).mockResolvedValue([{
            ...account,
            name: '个人邮箱',
            address: 'a@gmail.com',
            auth: 'oauth',
            provider: 'gmail',
        }] as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('个人邮箱');
        expect(screen.getByText('网页登录')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        expect(screen.queryByPlaceholderText('主机')).toBeNull();
        expect(screen.getByText('这个邮箱通过浏览器登录。要更换授权，请重新登录。')).toBeTruthy();
        expect((screen.getByRole('textbox', { name: '邮箱地址' }) as HTMLInputElement).readOnly).toBe(true);

        vi.mocked(MailboxOAuthLogin).mockResolvedValue('Gmail 邮箱 a@gmail.com 已登录' as never);
        fireEvent.click(screen.getByRole('button', { name: '重新登录' }));
        await waitFor(() => expect(MailboxOAuthLogin).toHaveBeenCalledWith('gmail', 'a@gmail.com'));
    });

    it('opens mailbox management from the row and reads a message in the selected folder', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
            ],
            box: 'INBOX',
            messages: [{ uid: 3, from: 'a@ex.com', subject: 'Hello', date: '2026-10-01', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseMessages).mockImplementation(async (_id: string, box: string) => {
            if (box === 'Junk') {
                return [{ uid: 9, from: 'spam@ex.com', subject: 'Capacity', date: '2026-10-14', seen: false }] as never;
            }
            return [{ uid: 3, from: 'a@ex.com', subject: 'Hello', date: '2026-10-01', seen: true }] as never;
        });
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 9, from: 'spam@ex.com', subject: 'Capacity', date: '2026-10-14', body: 'mailbox full',
        } as never);

        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');
        fireEvent.click(screen.getByTestId('mailbox-manage-mb1'));

        expect(await screen.findByRole('dialog', { name: '邮箱管理' })).toBeTruthy();
        expect(await screen.findByText('Hello')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '垃圾邮件' }));
        expect(await screen.findByText('Capacity')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: /Capacity/ }));
        expect(await screen.findByText('mailbox full')).toBeTruthy();
        expect(MailboxBrowseRead).toHaveBeenCalledWith('mb1', 'Junk', 9);
        expect(screen.getByRole('button', { name: /Capacity/ }).className).toContain('mailbox-manage__message--current');
    });

    it('retries opening a mailbox after the first login fails', async () => {
        vi.mocked(MailboxBrowseStart)
            .mockRejectedValueOnce(new Error('登录失败，请检查密码或授权码'))
            .mockResolvedValue({
                folders: [{ name: 'INBOX', special: 'inbox' }],
                box: 'INBOX',
                messages: [{ uid: 1, from: 'a@ex.com', subject: 'Hello', seen: true }],
            } as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');
        fireEvent.click(screen.getByTestId('mailbox-manage-mb1'));

        expect(await screen.findByRole('alert')).toHaveProperty('textContent', '登录失败，请检查密码或授权码');
        fireEvent.click(screen.getByRole('button', { name: '再试一次' }));
        expect(await screen.findByText('Hello')).toBeTruthy();
        expect(MailboxBrowseStart).toHaveBeenCalledTimes(2);
    });
});
