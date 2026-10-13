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
    MailboxAccountTest: vi.fn(),
    MailboxAccountsList: vi.fn(),
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
    MailboxAccountTest,
    MailboxAccountsList,
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

    it('offers a connection test only for a configured direction', async () => {
        vi.mocked(MailboxAccountsList).mockResolvedValue([{ ...account, has_outbound: false }] as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');
        expect(screen.getByRole('button', { name: '测试收件' })).toBeTruthy();
        expect(screen.queryByRole('button', { name: '测试发件' })).toBeNull();
        expect(screen.getByText(/收件密码/)).toBeTruthy();
        expect(screen.queryByText(/发件密码/)).toBeNull();
        const passwords = screen.getAllByPlaceholderText('密码或授权码，留空则不修改') as HTMLInputElement[];
        expect(passwords[0].autocomplete).toBe('new-password');
        expect(passwords[1].autocomplete).toBe('new-password');
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

    it('drops a connection result after the mailbox is saved again', async () => {
        vi.mocked(MailboxAccountTest).mockResolvedValue('收件连接正常' as never);
        render(<EmailSettingsPanel lang="zh-Hans" />);
        await screen.findByText('Work');
        fireEvent.click(screen.getByRole('button', { name: '测试收件' }));
        expect(await screen.findByText('收件连接正常')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '编辑' }));
        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => expect(MailboxAccountSave).toHaveBeenCalled());
        await waitFor(() => expect(screen.queryByText('收件连接正常')).toBeNull());
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
});
