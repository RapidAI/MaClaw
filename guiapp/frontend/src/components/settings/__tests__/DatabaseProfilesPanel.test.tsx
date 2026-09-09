import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildProfilePayload, DatabaseProfilesPanel, emptyForm, nextCopyID } from '../DatabaseProfilesPanel';

const listMock = vi.hoisted(() => vi.fn());
const saveMock = vi.hoisted(() => vi.fn());
const deleteMock = vi.hoisted(() => vi.fn());
const copyMock = vi.hoisted(() => vi.fn());
const setSecretMock = vi.hoisted(() => vi.fn());
const testMock = vi.hoisted(() => vi.fn());
const selectFileMock = vi.hoisted(() => vi.fn());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    DatabaseProfilesList: (...args: unknown[]) => listMock(...args),
    DatabaseProfileCopy: (...args: unknown[]) => copyMock(...args),
    DatabaseProfileSetDisabled: async () => undefined,
    DatabaseProfileSave: (...args: unknown[]) => saveMock(...args),
    DatabaseProfileDelete: (...args: unknown[]) => deleteMock(...args),
    DatabaseProfileSetSecret: (...args: unknown[]) => setSecretMock(...args),
    DatabaseProfileTest: (...args: unknown[]) => testMock(...args),
    SelectDatabaseProfileFile: (...args: unknown[]) => selectFileMock(...args),
}));

const mysqlProfile = {
    id: 'crm',
    name: 'CRM',
    type: 'mysql',
    status: 'configured',
    host: '127.0.0.1',
    port: 3306,
    database: 'crm',
    username: 'reader',
    read_only: true,
    write_enabled: false,
    has_secret_ref: true,
    has_dsn: false,
    allowed_tables: ['orders'],
    masked_columns: ['phone'],
};

const excelProfile = {
    id: 'reports',
    name: 'Reports',
    type: 'excel',
    status: 'configured',
    file_path: 'C:/data/reports.xlsx',
    read_only: true,
    write_enabled: true,
    has_secret_ref: false,
    has_dsn: false,
};

describe('DatabaseProfilesPanel', () => {
    beforeEach(() => {
        listMock.mockReset().mockResolvedValue([mysqlProfile, excelProfile]);
        saveMock.mockReset().mockResolvedValue(undefined);
        deleteMock.mockReset().mockResolvedValue(undefined);
        copyMock.mockReset().mockResolvedValue(undefined);
        setSecretMock.mockReset().mockResolvedValue(undefined);
        testMock.mockReset();
        selectFileMock.mockReset();
    });

    it('renders the profile list with type badges and read/write state', async () => {
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        expect(screen.getByTestId('database-profile-crm').textContent).toContain('mysql');
        expect(screen.getByTestId('database-profile-crm').textContent).toContain('RO');
        expect(screen.getByTestId('database-profile-crm').textContent).toContain('reader@127.0.0.1:3306 / crm');
        expect(screen.getByTestId('database-profile-reports').textContent).toContain('excel');
        expect(screen.getByTestId('database-profile-reports').textContent).toContain('RW');
        expect(screen.getByTestId('database-profile-reports').textContent).toContain('C:/data/reports.xlsx');
    });

    it('switches form fields by profile type', async () => {
        listMock.mockResolvedValue([]);
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        // Grouped form: identity / connection / permissions, with SSH/replica in Advanced.
        expect(screen.getByText('Identity')).toBeTruthy();
        expect(screen.getByText('Connection')).toBeTruthy();
        // ID/Name/Type must share the identity grid so CSS can keep the controls on one baseline.
        const identityGrid = screen.getByLabelText('ID').closest('.database-profiles-grid--identity');
        expect(identityGrid).toBeInstanceOf(HTMLElement);
        expect(identityGrid?.contains(screen.getByLabelText('Name'))).toBe(true);
        expect(identityGrid?.contains(screen.getByLabelText('Type'))).toBe(true);
        expect(screen.getByText('Permissions')).toBeTruthy();
        expect(screen.getByText('SSH tunnel and read replica')).toBeTruthy();
        // Default mysql: host/port/database/username + secret.
        expect(screen.getByLabelText('Host')).toBeTruthy();
        expect(screen.getByLabelText('Port')).toBeTruthy();
        expect(screen.getByLabelText('Database')).toBeTruthy();
        expect(screen.getByLabelText('Password / Secret')).toBeTruthy();
        expect(screen.getByLabelText('Read replica host')).toBeTruthy();
        expect(screen.getByLabelText('Replica SSH session')).toBeTruthy();
        expect(screen.getByRole('group', { name: 'Permissions' })).toBeTruthy();
        expect(screen.queryByRole('button', { name: 'Browse' })).toBeNull();

        // Excel: file path + sheet, no host and no secret.
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'excel' } });
        expect(screen.queryByLabelText('Host')).toBeNull();
        expect(screen.queryByLabelText('Replica SSH session')).toBeNull();
        expect(screen.getByLabelText('File Path')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Browse' })).toBeTruthy();
        expect(screen.getByLabelText('Sheet')).toBeTruthy();
        expect(screen.queryByLabelText('Password / Secret')).toBeNull();

        // Access: file path + optional DSN; no secret field — the driver
        // builds its DSN from file_path/dsn and never reads the keyring.
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'access' } });
        expect(screen.getByLabelText('File Path')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Browse' })).toBeTruthy();
        expect(screen.getByLabelText('DSN (optional)')).toBeTruthy();
        expect(screen.queryByLabelText('Password / Secret')).toBeNull();
    });

    it('fills the file path from the native picker for file databases', async () => {
        listMock.mockResolvedValue([]);
        selectFileMock.mockResolvedValue('C:/data/crm.accdb');
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'access' } });
        fireEvent.click(screen.getByRole('button', { name: 'Browse' }));

        await waitFor(() => expect(selectFileMock).toHaveBeenCalledWith('access', ''));
        await waitFor(() => {
            expect((screen.getByLabelText('File Path') as HTMLInputElement).value).toBe('C:/data/crm.accdb');
        });
    });

    it('keeps the typed file path when the picker is cancelled', async () => {
        listMock.mockResolvedValue([]);
        selectFileMock.mockResolvedValue('');
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'excel' } });
        fireEvent.change(screen.getByLabelText('File Path'), { target: { value: 'keep.xlsx' } });
        fireEvent.click(screen.getByRole('button', { name: 'Browse' }));

        await waitFor(() => expect(selectFileMock).toHaveBeenCalledWith('excel', 'keep.xlsx'));
        expect((screen.getByLabelText('File Path') as HTMLInputElement).value).toBe('keep.xlsx');
    });

    it('does not open a second picker while one is already open', async () => {
        listMock.mockResolvedValue([]);
        let resolvePath: (value: string) => void = () => undefined;
        selectFileMock.mockImplementation(() => new Promise<string>((resolve) => { resolvePath = resolve; }));
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'excel' } });
        const browse = screen.getByRole('button', { name: 'Browse' });
        fireEvent.click(browse);
        fireEvent.click(browse);

        expect(selectFileMock).toHaveBeenCalledTimes(1);
        resolvePath('C:/data/reports.xlsx');
        await waitFor(() => {
            expect((screen.getByLabelText('File Path') as HTMLInputElement).value).toBe('C:/data/reports.xlsx');
        });
    });

    it('ignores a picked file after the type is switched away from a file database', async () => {
        listMock.mockResolvedValue([]);
        let resolvePath: (value: string) => void = () => undefined;
        selectFileMock.mockImplementation(() => new Promise<string>((resolve) => { resolvePath = resolve; }));
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'access' } });
        fireEvent.click(screen.getByRole('button', { name: 'Browse' }));
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'mysql' } });
        resolvePath('C:/data/crm.accdb');

        await waitFor(() => {
            expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false);
        });
        expect(screen.queryByLabelText('File Path')).toBeNull();
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'access' } });
        expect((screen.getByLabelText('File Path') as HTMLInputElement).value).toBe('');
    });

    it('saves with the correct payload and never includes the secret in it', async () => {
        listMock.mockResolvedValue([]);
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('ID'), { target: { value: 'crm' } });
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CRM' } });
        fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'db.internal' } });
        fireEvent.change(screen.getByLabelText('Port'), { target: { value: '3306' } });
        fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'crm' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'reader' } });
        fireEvent.change(screen.getByLabelText('Read replica host'), { target: { value: 'replica.internal' } });
        fireEvent.change(screen.getByLabelText('Replica SSH session'), { target: { value: 'ssh-replica' } });
        fireEvent.change(screen.getByLabelText('Password / Secret'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Allowed tables'), { target: { value: 'orders, customers' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
        const payload = saveMock.mock.calls[0][0] as Record<string, any>;
        expect(payload).toMatchObject({
            id: 'crm',
            name: 'CRM',
            type: 'mysql',
            host: 'db.internal',
            port: 3306,
            database: 'crm',
            username: 'reader',
            replica_host: 'replica.internal',
            replica_ssh_session_id: 'ssh-replica',
            read_only: true,
            write_enabled: false,
            allowed_tables: ['orders', 'customers'],
        });
        // The secret must only travel through the keyring binding.
        expect(payload).not.toHaveProperty('secret');
        expect(payload).not.toHaveProperty('password');
        expect(JSON.stringify(payload)).not.toContain('s3cret');
        await waitFor(() => expect(setSecretMock).toHaveBeenCalledWith('crm', 's3cret'));
    });

    it('does not call SetSecret when the secret field stays empty', async () => {
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getAllByRole('button', { name: 'Edit' })[0]);
        // Secret input is write-only: never prefilled, placeholder shows state.
        const secretInput = screen.getByLabelText('Password / Secret') as HTMLInputElement;
        expect(secretInput.value).toBe('');
        expect(secretInput.placeholder).toBe('Saved in keyring — enter to replace');

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
        expect(setSecretMock).not.toHaveBeenCalled();
    });

    it('deletes a profile only after confirmation', async () => {
        const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
        await waitFor(() => expect(confirmSpy).toHaveBeenCalled());
        expect(deleteMock).not.toHaveBeenCalled();

        confirmSpy.mockReturnValue(true);
        fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
        await waitFor(() => expect(deleteMock).toHaveBeenCalledWith('crm'));
        confirmSpy.mockRestore();
    });

    it('shows capabilities on a successful test and error_class on failure', async () => {
        testMock.mockResolvedValueOnce({
            status: 'ok',
            capabilities: { read: true, write: false, transactions: false, cursors: true, explain: false, max_page_size: 5000 },
        });
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getAllByRole('button', { name: 'Test Connection' })[0]);
        await waitFor(() => expect(testMock).toHaveBeenCalledWith('crm'));
        const okRow = await screen.findByText(/Connection OK/);
        expect(okRow.textContent).toContain('read');
        expect(okRow.textContent).toContain('cursors');

        testMock.mockResolvedValueOnce({ status: 'failed', error_class: 'authentication', error: 'access denied' });
        fireEvent.click(screen.getAllByRole('button', { name: 'Test Connection' })[1]);
        await waitFor(() => expect(testMock).toHaveBeenCalledWith('reports'));
        const failed = await screen.findByText(/Connection failed/);
        expect(failed.textContent).toContain('authentication');
        expect(failed.textContent).toContain('access denied');
    });

    it('keeps the form and secret when save succeeds but SetSecret fails', async () => {
        listMock.mockResolvedValue([]);
        setSecretMock.mockRejectedValueOnce(new Error('keyring locked'));
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('ID'), { target: { value: 'crm' } });
        fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'db.internal' } });
        fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'crm' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'reader' } });
        fireEvent.change(screen.getByLabelText('Password / Secret'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        // Partial failure: the profile was saved, only the keyring write
        // failed — the message must say exactly that.
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('Configuration saved, but the credential was not written to the OS keyring');
        expect(alert.textContent).toContain('keyring locked');
        expect(saveMock).toHaveBeenCalledTimes(1);
        // The form stays open with the secret intact so saving again retries
        // SetSecret without retyping.
        const secretInput = screen.getByLabelText('Password / Secret') as HTMLInputElement;
        expect(secretInput.value).toBe('s3cret');

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await waitFor(() => expect(setSecretMock).toHaveBeenCalledTimes(2));
        expect(setSecretMock).toHaveBeenLastCalledWith('crm', 's3cret');
        await screen.findByText('Saved');
    });

    it('blocks saving when a new profile reuses an existing id', async () => {
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getByRole('button', { name: '+ Add Data Source' }));
        fireEvent.change(screen.getByLabelText('ID'), { target: { value: 'crm' } });
        fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'db.internal' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        // A colliding id in create mode must not silently overwrite the
        // existing profile.
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('A data source with this ID already exists');
        expect(saveMock).not.toHaveBeenCalled();
        expect(setSecretMock).not.toHaveBeenCalled();
    });

    it('allows updating an existing profile from the edit flow', async () => {
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getAllByRole('button', { name: 'Edit' })[0]);
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CRM Renamed' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
        expect(saveMock.mock.calls[0][0]).toMatchObject({ id: 'crm', name: 'CRM Renamed' });
        await screen.findByText('Saved');
    });

    it('invalidates the cached test result of a profile after saving it', async () => {
        testMock.mockResolvedValueOnce({
            status: 'ok',
            capabilities: { read: true, write: false, transactions: false, cursors: true, explain: false, max_page_size: 5000 },
        });
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getAllByRole('button', { name: 'Test Connection' })[0]);
        await screen.findByText(/Connection OK/);

        fireEvent.click(screen.getAllByRole('button', { name: 'Edit' })[0]);
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CRM Renamed' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));

        // The saved config may differ from what was last probed, so the old
        // conclusion must be gone once the list re-renders.
        await screen.findByTestId('database-profile-crm');
        expect(screen.queryByText(/Connection OK/)).toBeNull();
    });

    it('treats read-only and writes as exclusive and hides SQL-only flags for excel', async () => {
        listMock.mockResolvedValue([]);
        render(<DatabaseProfilesPanel lang="en" />);

        fireEvent.click(await screen.findByRole('button', { name: '+ Add Data Source' }));
        const readOnly = screen.getByLabelText('Read-only') as HTMLInputElement;
        const writes = screen.getByLabelText('Enable writes') as HTMLInputElement;
        expect(readOnly.checked).toBe(true);
        expect(writes.checked).toBe(false);

        fireEvent.click(writes);
        expect(writes.checked).toBe(true);
        expect(readOnly.checked).toBe(false);
        expect((screen.getByLabelText('Allow DDL') as HTMLInputElement).checked).toBe(false);

        fireEvent.click(screen.getByLabelText('Allow DDL'));
        expect((screen.getByLabelText('Allow DDL') as HTMLInputElement).checked).toBe(true);
        expect(writes.checked).toBe(true);
        expect(readOnly.checked).toBe(false);

        fireEvent.click(readOnly);
        expect(readOnly.checked).toBe(true);
        expect(writes.checked).toBe(false);
        expect((screen.getByLabelText('Allow DDL') as HTMLInputElement).checked).toBe(false);

        expect(screen.getByLabelText('Allow public/external hosts')).toBeTruthy();
        expect(screen.getByLabelText('TLS mode')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'excel' } });
        expect(screen.queryByLabelText('Allow public/external hosts')).toBeNull();
        expect(screen.queryByLabelText('TLS mode')).toBeNull();
        expect(screen.getByLabelText('Allow DDL')).toBeTruthy();
    });

    it('picks a free copy id when id-copy already exists', async () => {
        listMock.mockResolvedValue([mysqlProfile, { ...mysqlProfile, id: 'crm-copy', name: 'CRM copy' }]);
        render(<DatabaseProfilesPanel lang="en" />);

        await screen.findByTestId('database-profile-crm');
        fireEvent.click(screen.getAllByRole('button', { name: 'Copy' })[0]);
        await waitFor(() => expect(copyMock).toHaveBeenCalledWith('crm', 'crm-copy-2'));
    });

    it('still shows the empty state after the initial list load fails', async () => {
        listMock.mockRejectedValue(new Error('disk unavailable'));
        render(<DatabaseProfilesPanel lang="en" />);

        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('disk unavailable');
        expect(screen.getByText('No data sources yet. Add one to let the agent query your databases.')).toBeTruthy();
    });
});

describe('buildProfilePayload', () => {
    it('always sends owned fields so emptying them clears the stored profile', () => {
        const payload = buildProfilePayload({
            ...emptyForm(),
            id: 'crm',
            host: 'db.internal',
            database: 'crm',
        });
        expect(payload.tls).toEqual({ mode: '' });
        expect(payload.data_classification).toBe('');
        expect(payload.max_affected_rows).toBe(0);
        expect(payload.ssh_session_id).toBe('');
        expect(payload.replica_host).toBe('');
        expect(payload.replica_port).toBe(0);
        expect(payload.replica_ssh_session_id).toBe('');
        expect(payload.allow_external_host).toBe(false);
        expect(payload.allowed_tables).toEqual([]);
        expect(payload.masked_columns).toEqual([]);
    });

    it('keeps replica, tls and classification when set', () => {
        const payload = buildProfilePayload({
            ...emptyForm(),
            id: 'crm',
            host: 'db.internal',
            database: 'crm',
            tls_mode: 'require',
            data_classification: 'internal',
            max_affected_rows: '50',
            ssh_session_id: 'ssh-1',
            replica_host: 'replica.internal',
            replica_port: '3307',
            replica_ssh_session_id: 'ssh-replica',
            allow_external_host: true,
        });
        expect(payload.tls).toEqual({ mode: 'require' });
        expect(payload.data_classification).toBe('internal');
        expect(payload.max_affected_rows).toBe(50);
        expect(payload.ssh_session_id).toBe('ssh-1');
        expect(payload.replica_host).toBe('replica.internal');
        expect(payload.replica_port).toBe(3307);
        expect(payload.replica_ssh_session_id).toBe('ssh-replica');
        expect(payload.allow_external_host).toBe(true);
    });

    it('does not keep a public-host flag on excel profiles', () => {
        const payload = buildProfilePayload({
            ...emptyForm(),
            id: 'reports',
            type: 'excel',
            file_path: 'C:/data/reports.xlsx',
            allow_external_host: true,
            ssh_session_id: 'ssh-1',
            tls_mode: 'require',
        });
        expect(payload.allow_external_host).toBe(false);
        expect(payload.tls).toEqual({ mode: '' });
        expect(payload.sheet).toBe('');
        expect(payload).not.toHaveProperty('host');
        expect(payload).not.toHaveProperty('ssh_session_id');
        expect(payload.file_path).toBe('C:/data/reports.xlsx');
    });
});

describe('nextCopyID', () => {
    it('uses id-copy then numeric suffixes', () => {
        expect(nextCopyID('crm', [])).toBe('crm-copy');
        expect(nextCopyID('crm', [{ id: 'crm-copy' }])).toBe('crm-copy-2');
        expect(nextCopyID('crm', [{ id: 'crm-copy' }, { id: 'crm-copy-2' }])).toBe('crm-copy-3');
    });
});
