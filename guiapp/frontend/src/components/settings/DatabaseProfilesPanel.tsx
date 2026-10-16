import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
    DatabaseProfileCopy,
    DatabaseProfileDelete,
    DatabaseProfileSave,
    DatabaseProfileSetDisabled,
    DatabaseProfileSetSecret,
    DatabaseProfileTest,
    DatabaseProfilesList,
    SelectDatabaseProfileFile,
} from '../../../wailsjs/go/main/App';
import { t } from '../../i18n';

type Props = {
    lang: string;
};

// Keep this projection in sync with the summary emitted by
// guiapp/app_database_profiles.go (databaseProfileSummary).
export type DatabaseProfileSummary = {
    id: string;
    name?: string;
    type: string;
    status?: string;
    host?: string;
    port?: number;
    database?: string;
    username?: string;
    default_schema?: string;
    file_path?: string;
    sheet?: string;
    schema_version?: number;
    read_only?: boolean;
    write_enabled?: boolean;
    allowed_tables?: string[];
    masked_columns?: string[];
    has_secret_ref?: boolean;
    has_dsn?: boolean;
    allow_external_host?: boolean;
    disabled?: boolean;
    allow_ddl?: boolean;
    tls_mode?: string;
    data_classification?: string;
    max_affected_rows?: number;
    ssh_session_id?: string;
    replica_host?: string;
    replica_port?: number;
    replica_ssh_session_id?: string;
};

export type DatabaseProfileTestResult = {
    status?: string;
    capabilities?: {
        read?: boolean;
        write?: boolean;
        transactions?: boolean;
        cursors?: boolean;
        explain?: boolean;
        max_page_size?: number;
    };
    error_class?: string;
    error?: string;
    odbc_hint?: string;
};

type ProfileForm = {
    id: string;
    name: string;
    type: string;
    host: string;
    port: string;
    database: string;
    username: string;
    file_path: string;
    sheet: string;
    dsn: string;
    read_only: boolean;
    write_enabled: boolean;
    allowed_tables: string;
    masked_columns: string;
    secret: string;
    allow_external_host: boolean;
    allow_ddl: boolean;
    tls_mode: string;
    data_classification: string;
    max_affected_rows: string;
    ssh_session_id: string;
    replica_host: string;
    replica_port: string;
    replica_ssh_session_id: string;
};

const SQL_TYPES = ['mysql', 'postgres', 'sqlserver'];
const PROFILE_TYPES = [...SQL_TYPES, 'access', 'excel'];

export const emptyForm = (): ProfileForm => ({
    id: '',
    name: '',
    type: 'mysql',
    host: '',
    port: '',
    database: '',
    username: '',
    file_path: '',
    sheet: '',
    dsn: '',
    read_only: true,
    write_enabled: false,
    allowed_tables: '',
    masked_columns: '',
    secret: '',
    allow_external_host: false,
    allow_ddl: false,
    tls_mode: '',
    data_classification: '',
    max_affected_rows: '',
    ssh_session_id: '',
    replica_host: '',
    replica_port: '',
    replica_ssh_session_id: '',
});

function formFromSummary(p: DatabaseProfileSummary): ProfileForm {
    return {
        ...emptyForm(),
        id: p.id || '',
        name: p.name || '',
        type: p.type || 'mysql',
        host: p.host || '',
        port: p.port ? String(p.port) : '',
        database: p.database || '',
        username: p.username || '',
        file_path: p.file_path || '',
        sheet: p.sheet || '',
        dsn: '',
        write_enabled: !!p.write_enabled,
        read_only: p.write_enabled ? false : p.read_only !== false,
        allowed_tables: (p.allowed_tables || []).join(', '),
        masked_columns: (p.masked_columns || []).join(', '),
        secret: '',
        allow_external_host: !!p.allow_external_host,
        allow_ddl: !!p.write_enabled && !!p.allow_ddl,
        tls_mode: p.tls_mode || '',
        data_classification: p.data_classification || '',
        max_affected_rows: p.max_affected_rows ? String(p.max_affected_rows) : '',
        ssh_session_id: p.ssh_session_id || '',
        replica_host: p.replica_host || '',
        replica_port: p.replica_port ? String(p.replica_port) : '',
        replica_ssh_session_id: p.replica_ssh_session_id || '',
    };
}

function parseCsv(value: string): string[] {
    return value.split(',').map((item) => item.trim()).filter(Boolean);
}

// buildProfilePayload assembles the non-secret profile body for
// DatabaseProfileSave. The secret never becomes part of this payload — it is
// submitted separately through DatabaseProfileSetSecret (OS keyring only).
export function buildProfilePayload(form: ProfileForm): Record<string, any> {
    const payload: Record<string, any> = {
        id: form.id.trim(),
        name: form.name.trim(),
        type: form.type,
        read_only: form.read_only,
        write_enabled: form.write_enabled,
        allow_external_host: SQL_TYPES.includes(form.type) && form.allow_external_host,
        allow_ddl: form.allow_ddl,
    };
    // Always send the fields this form owns. DatabaseProfileSave preserves
    // absent keys, so omitting an emptied TLS/classification/max-rows/SSH
    // value would keep the previous setting instead of clearing it.
    payload.data_classification = form.data_classification.trim();
    const maxRows = parseInt(form.max_affected_rows, 10);
    payload.max_affected_rows = Number.isFinite(maxRows) && maxRows > 0 ? maxRows : 0;
    payload.allowed_tables = parseCsv(form.allowed_tables);
    payload.masked_columns = parseCsv(form.masked_columns);
    if (SQL_TYPES.includes(form.type)) {
        payload.tls = { mode: form.tls_mode.trim() };
        payload.host = form.host.trim();
        const port = parseInt(form.port, 10);
        payload.port = Number.isFinite(port) ? port : 0;
        payload.database = form.database.trim();
        payload.username = form.username.trim();
        payload.ssh_session_id = form.ssh_session_id.trim();
        payload.replica_host = form.replica_host.trim();
        const replicaPort = parseInt(form.replica_port, 10);
        payload.replica_port = Number.isFinite(replicaPort) && replicaPort > 0 ? replicaPort : 0;
        payload.replica_ssh_session_id = form.replica_ssh_session_id.trim();
    } else if (form.type === 'access') {
        payload.tls = { mode: '' };
        payload.file_path = form.file_path.trim();
        if (form.dsn.trim()) {
            payload.dsn = form.dsn.trim();
        }
    } else if (form.type === 'excel') {
        payload.tls = { mode: '' };
        payload.file_path = form.file_path.trim();
        payload.sheet = form.sheet.trim();
    }
    return payload;
}

export function DatabaseProfilesPanel({ lang }: Props) {
    const [profiles, setProfiles] = useState<DatabaseProfileSummary[]>([]);
    const [loaded, setLoaded] = useState(false);
    const [editing, setEditing] = useState<ProfileForm | null>(null);
    // Whether the form was entered via "Add" (create) or "Edit" (update).
    // Deriving this from the id matching an existing profile would silently
    // turn an id collision in the create flow into an overwrite update.
    const [editingIsNew, setEditingIsNew] = useState(false);
    const [editingHasSecret, setEditingHasSecret] = useState(false);
    const [editingHasDsn, setEditingHasDsn] = useState(false);
    const [testResults, setTestResults] = useState<Record<string, DatabaseProfileTestResult>>({});
    const [busy, setBusy] = useState('');
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const [advancedOpen, setAdvancedOpen] = useState(false);
    const browseInFlightRef = useRef(false);

    const tr = (key: string) => t(key, lang);

    const reload = () => DatabaseProfilesList()
        .then((items) => {
            setProfiles((items || []) as DatabaseProfileSummary[]);
            setLoaded(true);
        })
        .catch((err: any) => {
            setMessage('');
            setError(err?.message || String(err));
            setLoaded(true);
        });

    useEffect(() => {
        let mounted = true;
        setBusy('load');
        DatabaseProfilesList()
            .then((items) => {
                if (mounted) {
                    setProfiles((items || []) as DatabaseProfileSummary[]);
                    setLoaded(true);
                }
            })
            .catch((err: any) => {
                if (!mounted) return;
                setLoaded(true);
                setMessage('');
                setError(err?.message || String(err));
            })
            .finally(() => mounted && setBusy(''));
        return () => { mounted = false; };
    }, []);

    const run = async (label: string, fn: () => Promise<void>) => {
        setBusy(label);
        setMessage('');
        setError('');
        try {
            await fn();
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setBusy('');
        }
    };

    const startAdd = () => {
        setEditing(emptyForm());
        setEditingIsNew(true);
        setEditingHasSecret(false);
        setEditingHasDsn(false);
        setAdvancedOpen(false);
        setMessage('');
        setError('');
    };

    const startEdit = (profile: DatabaseProfileSummary) => {
        setEditing(formFromSummary(profile));
        setEditingIsNew(false);
        setEditingHasSecret(!!profile.has_secret_ref);
        setEditingHasDsn(!!profile.has_dsn);
        setAdvancedOpen(!!(profile.ssh_session_id || profile.replica_host || profile.replica_ssh_session_id));
        setMessage('');
        setError('');
    };

    const save = () => {
        if (!editing) return;
        const payload = buildProfilePayload(editing);
        // Create mode must never overwrite an existing profile: a colliding
        // id would replace the whole record (and the Go side would keep the
        // existing bindings), so block it and ask for a different id.
        if (editingIsNew && profiles.some((p) => p.id === payload.id)) {
            setMessage('');
            setError(tr('dbProfilesIdExists'));
            return;
        }
        run('save', async () => {
            await DatabaseProfileSave(payload as any);
            // The config changed: any cached test conclusion for this id was
            // produced against the old settings and must not keep showing.
            setTestResults((prev) => {
                const next = { ...prev };
                delete next[payload.id];
                return next;
            });
            // Secret travels only through the keyring binding — never in the
            // profile payload, never persisted to the config file.
            if (editing.secret !== '') {
                try {
                    await DatabaseProfileSetSecret(payload.id, editing.secret);
                } catch (err: any) {
                    // Partial failure: the profile is already persisted, only
                    // the keyring write failed. Keep the form open with the
                    // secret input intact so the user can retry SetSecret by
                    // saving again without retyping anything.
                    setError(`${tr('dbProfilesSavedSecretFailed')} (${err?.message || String(err)})`);
                    await reload();
                    return;
                }
            }
            setEditing(null);
            setMessage(tr('dbProfilesSaved'));
            await reload();
        });
    };

    const remove = (profile: DatabaseProfileSummary) => run(`delete:${profile.id}`, async () => {
        if (!window.confirm(tr('dbProfilesDeleteConfirm'))) {
            return;
        }
        await DatabaseProfileDelete(profile.id);
        setTestResults((prev) => {
            const next = { ...prev };
            delete next[profile.id];
            return next;
        });
        setMessage(tr('dbProfilesDeleted'));
        await reload();
    });

    const test = (id: string) => run(`test:${id}`, async () => {
        const result = await DatabaseProfileTest(id) as DatabaseProfileTestResult;
        setTestResults((prev) => ({ ...prev, [id]: result || {} }));
    });

    const updateForm = (patch: Partial<ProfileForm>) => {
        setEditing((prev) => (prev ? { ...prev, ...patch } : prev));
        setError('');
    };

    const browseFilePath = () => {
        if (!editing || busy || browseInFlightRef.current) return;
        if (editing.type !== 'access' && editing.type !== 'excel') return;
        const kind = editing.type;
        const currentPath = editing.file_path;
        browseInFlightRef.current = true;
        setBusy('browse');
        SelectDatabaseProfileFile(kind, currentPath)
            .then((path) => {
                if (!path) return;
                setEditing((prev) => {
                    if (!prev || (prev.type !== 'access' && prev.type !== 'excel')) return prev;
                    return { ...prev, file_path: path };
                });
                setError('');
            })
            .catch((err: any) => {
                setError(err?.message || String(err));
            })
            .finally(() => {
                browseInFlightRef.current = false;
                setBusy('');
            });
    };

    const renderTestResult = (id: string) => {
        const result = testResults[id];
        if (!result) return null;
        if (result.status === 'ok') {
            const caps = result.capabilities || {};
            const enabled = [
                caps.read ? tr('dbProfilesCapRead') : '',
                caps.write ? tr('dbProfilesCapWrite') : '',
                caps.transactions ? tr('dbProfilesCapTransactions') : '',
                caps.cursors ? tr('dbProfilesCapCursors') : '',
                caps.explain ? tr('dbProfilesCapExplain') : '',
            ].filter(Boolean).join(', ');
            return (
                <div role="status" className="database-profiles-alert database-profiles-alert--success">
                    {tr('dbProfilesTestOk')} — {tr('dbProfilesCapabilities')}: {enabled || '-'}
                    {result.odbc_hint ? <div>{result.odbc_hint}</div> : null}
                </div>
            );
        }
        return (
            <div role="alert" className="database-profiles-alert database-profiles-alert--error">
                {tr('dbProfilesTestFailed')}
                {result.error_class ? <span className="database-profiles-error-class"> [{result.error_class}]</span> : null}
                {result.error ? <div>{result.error}</div> : null}
                {result.odbc_hint ? <div>{result.odbc_hint}</div> : null}
            </div>
        );
    };

    const renderForm = () => {
        if (!editing) return null;
        const isNew = editingIsNew;
        const isSql = SQL_TYPES.includes(editing.type);
        const canAct = !busy;
        const portPlaceholder = editing.type === 'postgres' ? '5432' : editing.type === 'sqlserver' ? '1433' : '3306';
        return (
            <form
                className="database-profiles-card"
                onSubmit={(event) => {
                    event.preventDefault();
                    if (canAct) save();
                }}
            >
                <div className="database-profiles-card-title">
                    {isNew ? tr('dbProfilesNew') : tr('dbProfilesEdit')}
                </div>

                <FormSection title={tr('dbProfilesSectionIdentity')}>
                    <div className="database-profiles-grid database-profiles-grid--identity">
                        <Field label={tr('dbProfilesFieldId')} hint={isNew ? tr('dbProfilesFieldIdHint') : ''}>
                            <input
                                aria-label={tr('dbProfilesFieldId')}
                                value={editing.id}
                                disabled={!isNew}
                                onChange={(e) => updateForm({ id: e.target.value })}
                                placeholder="crm-prod"
                            />
                        </Field>
                        <Field label={tr('dbProfilesFieldName')}>
                            <input aria-label={tr('dbProfilesFieldName')} value={editing.name} onChange={(e) => updateForm({ name: e.target.value })} placeholder="CRM" />
                        </Field>
                        <Field label={tr('dbProfilesFieldType')}>
                            <select aria-label={tr('dbProfilesFieldType')} value={editing.type} onChange={(e) => updateForm({ type: e.target.value })}>
                                {PROFILE_TYPES.map((type) => <option key={type} value={type}>{type}</option>)}
                            </select>
                        </Field>
                    </div>
                </FormSection>

                <FormSection title={tr('dbProfilesSectionConnection')}>
                    {isSql && (
                        <>
                            <div className="database-profiles-host-row">
                                <Field label={tr('dbProfilesFieldHost')}>
                                    <input aria-label={tr('dbProfilesFieldHost')} value={editing.host} onChange={(e) => updateForm({ host: e.target.value })} placeholder="127.0.0.1" />
                                </Field>
                                <Field label={tr('dbProfilesFieldPort')}>
                                    <input aria-label={tr('dbProfilesFieldPort')} value={editing.port} onChange={(e) => updateForm({ port: e.target.value })} placeholder={portPlaceholder} inputMode="numeric" />
                                </Field>
                            </div>
                            <div className="database-profiles-grid">
                                <Field label={tr('dbProfilesFieldDatabase')}>
                                    <input aria-label={tr('dbProfilesFieldDatabase')} value={editing.database} onChange={(e) => updateForm({ database: e.target.value })} />
                                </Field>
                                <Field label={tr('dbProfilesFieldUsername')}>
                                    <input aria-label={tr('dbProfilesFieldUsername')} value={editing.username} onChange={(e) => updateForm({ username: e.target.value })} autoComplete="off" />
                                </Field>
                                <Field label={tr('dbProfilesFieldSecret')} hint={tr('dbProfilesSecretHint')} className="database-profiles-span-2">
                                    <input
                                        aria-label={tr('dbProfilesFieldSecret')}
                                        type="password"
                                        value={editing.secret}
                                        onChange={(e) => updateForm({ secret: e.target.value })}
                                        placeholder={editingHasSecret ? tr('dbProfilesSecretSaved') : tr('dbProfilesSecretUnset')}
                                        autoComplete="new-password"
                                    />
                                </Field>
                            </div>
                        </>
                    )}
                    {(editing.type === 'access' || editing.type === 'excel') && (
                        <div className="database-profiles-grid">
                            <Field
                                label={tr('dbProfilesFieldFilePath')}
                                className="database-profiles-span-2"
                                trailing={(
                                    <button type="button" onClick={browseFilePath} disabled={!canAct}>
                                        {tr('dbProfilesBrowse')}
                                    </button>
                                )}
                            >
                                <input
                                    aria-label={tr('dbProfilesFieldFilePath')}
                                    value={editing.file_path}
                                    onChange={(e) => updateForm({ file_path: e.target.value })}
                                />
                            </Field>
                            {editing.type === 'access' && (
                                <Field label={tr('dbProfilesFieldDsn')} hint={editingHasDsn ? tr('dbProfilesDsnKeepHint') : ''} className="database-profiles-span-2">
                                    <input aria-label={tr('dbProfilesFieldDsn')} value={editing.dsn} onChange={(e) => updateForm({ dsn: e.target.value })} autoComplete="off" />
                                </Field>
                            )}
                            {editing.type === 'excel' && (
                                <Field label={tr('dbProfilesFieldSheet')} className="database-profiles-span-2">
                                    <input aria-label={tr('dbProfilesFieldSheet')} value={editing.sheet} onChange={(e) => updateForm({ sheet: e.target.value })} />
                                </Field>
                            )}
                        </div>
                    )}
                </FormSection>

                <FormSection title={tr('dbProfilesSectionPermissions')} hint={tr('dbProfilesSectionPermissionsHint')}>
                    <div className="database-profiles-permissions" role="group" aria-label={tr('dbProfilesSectionPermissions')}>
                        <PermissionToggle
                            checked={editing.read_only}
                            onChange={(checked) => updateForm(checked
                                ? { read_only: true, write_enabled: false, allow_ddl: false }
                                : { read_only: false, write_enabled: true })}
                            label={tr('dbProfilesReadOnly')}
                            hint={tr('dbProfilesReadOnlyHint')}
                        />
                        <PermissionToggle
                            checked={editing.write_enabled}
                            onChange={(checked) => updateForm(checked
                                ? { write_enabled: true, read_only: false }
                                : { write_enabled: false, allow_ddl: false, read_only: true })}
                            label={tr('dbProfilesWriteEnabled')}
                            hint={tr('dbProfilesWriteEnabledHint')}
                        />
                        {isSql && (
                            <PermissionToggle
                                checked={editing.allow_external_host}
                                onChange={(checked) => updateForm({ allow_external_host: checked })}
                                label={tr('dbProfilesAllowExternalHost')}
                                hint={tr('dbProfilesAllowExternalHostHint')}
                            />
                        )}
                        <PermissionToggle
                            checked={editing.allow_ddl}
                            onChange={(checked) => updateForm(checked
                                ? { allow_ddl: true, write_enabled: true, read_only: false }
                                : { allow_ddl: false })}
                            label={tr('dbProfilesAllowDdl')}
                            hint={tr('dbProfilesAllowDdlHint')}
                        />
                    </div>
                </FormSection>

                <FormSection title={tr('dbProfilesSectionPolicy')}>
                    <div className="database-profiles-grid">
                        <Field label={tr('dbProfilesFieldAllowedTables')} hint={tr('dbProfilesCsvHint')}>
                            <input aria-label={tr('dbProfilesFieldAllowedTables')} value={editing.allowed_tables} onChange={(e) => updateForm({ allowed_tables: e.target.value })} placeholder="orders, customers" />
                        </Field>
                        <Field label={tr('dbProfilesFieldMaskedColumns')} hint={tr('dbProfilesCsvHint')}>
                            <input aria-label={tr('dbProfilesFieldMaskedColumns')} value={editing.masked_columns} onChange={(e) => updateForm({ masked_columns: e.target.value })} placeholder="phone, id_card" />
                        </Field>
                    </div>
                </FormSection>

                <FormSection title={tr('dbProfilesSectionSecurity')}>
                    <div className={isSql ? 'database-profiles-grid database-profiles-grid--security' : 'database-profiles-grid'}>
                        {isSql && (
                            <Field label={tr('dbProfilesTlsMode')}>
                                <select aria-label={tr('dbProfilesTlsMode')} value={editing.tls_mode} onChange={(e) => updateForm({ tls_mode: e.target.value })}>
                                    <option value="">{tr('dbProfilesTlsDefault')}</option>
                                    <option value="disable">disable</option>
                                    <option value="require">require</option>
                                    <option value="verify-full">verify-full</option>
                                </select>
                            </Field>
                        )}
                        <Field label={tr('dbProfilesClassification')}>
                            <input aria-label={tr('dbProfilesClassification')} value={editing.data_classification} onChange={(e) => updateForm({ data_classification: e.target.value })} placeholder="internal" />
                        </Field>
                        <Field label={tr('dbProfilesMaxAffected')}>
                            <input aria-label={tr('dbProfilesMaxAffected')} value={editing.max_affected_rows} onChange={(e) => updateForm({ max_affected_rows: e.target.value })} inputMode="numeric" />
                        </Field>
                    </div>
                </FormSection>

                {isSql && (
                    <details
                        className="database-profiles-advanced"
                        open={advancedOpen}
                        onToggle={(event) => {
                            const next = (event.currentTarget as HTMLDetailsElement).open;
                            if (next !== advancedOpen) setAdvancedOpen(next);
                        }}
                    >
                        <summary>{tr('dbProfilesSectionAdvanced')}</summary>
                        <div className="database-profiles-advanced-body">
                            <div className="database-profiles-grid">
                                <Field label={tr('dbProfilesFieldSSHSession')} hint={tr('dbProfilesSSHSessionHint')} className="database-profiles-span-2">
                                    <input aria-label={tr('dbProfilesFieldSSHSession')} value={editing.ssh_session_id} onChange={(e) => updateForm({ ssh_session_id: e.target.value })} placeholder="ssh-session-id" autoComplete="off" />
                                </Field>
                            </div>
                            <div className="database-profiles-host-row">
                                <Field label={tr('dbProfilesFieldReplicaHost')} hint={tr('dbProfilesReplicaHint')}>
                                    <input aria-label={tr('dbProfilesFieldReplicaHost')} value={editing.replica_host} onChange={(e) => updateForm({ replica_host: e.target.value })} placeholder="replica.internal" autoComplete="off" />
                                </Field>
                                <Field label={tr('dbProfilesFieldReplicaPort')}>
                                    <input aria-label={tr('dbProfilesFieldReplicaPort')} value={editing.replica_port} onChange={(e) => updateForm({ replica_port: e.target.value })} placeholder={portPlaceholder} inputMode="numeric" />
                                </Field>
                            </div>
                            <div className="database-profiles-grid">
                                <Field label={tr('dbProfilesFieldReplicaSSH')} hint={tr('dbProfilesReplicaSSHHint')} className="database-profiles-span-2">
                                    <input aria-label={tr('dbProfilesFieldReplicaSSH')} value={editing.replica_ssh_session_id} onChange={(e) => updateForm({ replica_ssh_session_id: e.target.value })} placeholder="ssh-replica-session" autoComplete="off" />
                                </Field>
                            </div>
                        </div>
                    </details>
                )}

                <div className="database-profiles-actions">
                    <button type="button" onClick={() => setEditing(null)} disabled={!canAct}>
                        {tr('dbProfilesCancel')}
                    </button>
                    <button type="submit" disabled={!canAct} className="database-profiles-primary">
                        {busy === 'save' ? tr('dbProfilesSaving') : tr('dbProfilesSave')}
                    </button>
                </div>
            </form>
        );
    };

    return (
        <div className="database-profiles-panel">
            <div className="database-profiles-header">
                <div>
                    <h2>{tr('dbProfilesTitle')}</h2>
                    <p>{tr('dbProfilesDesc')}</p>
                </div>
                {!editing && (
                    <button type="button" onClick={startAdd} disabled={!!busy} className="database-profiles-primary">
                        {tr('dbProfilesAdd')}
                    </button>
                )}
            </div>

            {error && <div role="alert" className="database-profiles-alert database-profiles-alert--error">{error}</div>}
            {message && <div role="status" className="database-profiles-alert database-profiles-alert--success">{message}</div>}

            {editing ? renderForm() : (
                <section className="database-profiles-card">
                    {loaded && profiles.length === 0 && (
                        <div className="database-profiles-empty">{tr('dbProfilesEmpty')}</div>
                    )}
                    {profiles.map((profile) => {
                        const endpoint = profileEndpoint(profile);
                        const title = profile.name || profile.id;
                        return (
                        <div key={profile.id} className="database-profiles-row" data-testid={`database-profile-${profile.id}`} data-disabled={profile.disabled ? 'true' : undefined}>
                            <div className="database-profiles-row-body">
                                <div className="database-profiles-row-main">
                                    <div className="database-profiles-row-title">
                                        <span className="database-profiles-type-badge" data-type={profile.type}>{profile.type}</span>
                                        <strong>{title}</strong>
                                        {title !== profile.id ? (
                                            <span className="database-profiles-row-id">{profile.id}</span>
                                        ) : null}
                                        <span className="database-profiles-mode-badge" data-write={profile.write_enabled ? 'true' : 'false'}>
                                            {profile.write_enabled ? tr('dbProfilesListBadgeRW') : tr('dbProfilesListBadgeRO')}
                                        </span>
                                        {profile.disabled && (
                                            <span className="database-profiles-status-badge" data-state="disabled">{tr('dbProfilesDisabled')}</span>
                                        )}
                                        {profile.status === 'invalid' && (
                                            <span className="database-profiles-status-badge" data-state="invalid">{tr('dbProfilesStatusInvalid')}</span>
                                        )}
                                    </div>
                                    {endpoint ? (
                                        <div className="database-profiles-row-meta">{endpoint}</div>
                                    ) : null}
                                </div>
                                <div className="database-profiles-actions">
                                    <button type="button" onClick={() => test(profile.id)} disabled={!!busy}>
                                        {busy === `test:${profile.id}` ? tr('dbProfilesTesting') : tr('dbProfilesTestBtn')}
                                    </button>
                                    <button type="button" onClick={() => startEdit(profile)} disabled={!!busy}>
                                        {tr('dbProfilesEditBtn')}
                                    </button>
                                    <button type="button" onClick={() => run(`copy:${profile.id}`, async () => {
                                        await DatabaseProfileCopy(profile.id, nextCopyID(profile.id, profiles));
                                        await reload();
                                        setMessage(tr('dbProfilesCopied'));
                                    })} disabled={!!busy}>
                                        {tr('dbProfilesCopyBtn')}
                                    </button>
                                    <button type="button" onClick={() => run(`disable:${profile.id}`, async () => {
                                        await DatabaseProfileSetDisabled(profile.id, !profile.disabled);
                                        await reload();
                                    })} disabled={!!busy}>
                                        {profile.disabled ? tr('dbProfilesEnableBtn') : tr('dbProfilesDisableBtn')}
                                    </button>
                                    <button type="button" className="database-profiles-danger" onClick={() => remove(profile)} disabled={!!busy}>
                                        {tr('dbProfilesDeleteBtn')}
                                    </button>
                                </div>
                            </div>
                            {renderTestResult(profile.id)}
                        </div>
                        );
                    })}
                </section>
            )}
        </div>
    );
}

function FormSection({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
    return (
        <div className="database-profiles-section">
            <div className="database-profiles-section-head">
                <h3>{title}</h3>
                {hint ? <p>{hint}</p> : null}
            </div>
            {children}
        </div>
    );
}

function PermissionToggle({ checked, onChange, label, hint }: { checked: boolean; onChange: (checked: boolean) => void; label: string; hint: string }) {
    return (
        <label className="database-profiles-toggle">
            <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} aria-label={label} />
            <span>
                <strong>{label}</strong>
                <small>{hint}</small>
            </span>
        </label>
    );
}

export function nextCopyID(id: string, existing: { id: string }[]): string {
    const taken = new Set(existing.map((item) => item.id));
    const base = `${id}-copy`;
    if (!taken.has(base)) return base;
    for (let n = 2; n < 1000; n += 1) {
        const candidate = `${id}-copy-${n}`;
        if (!taken.has(candidate)) return candidate;
    }
    return `${id}-copy-${Date.now()}`;
}

function profileEndpoint(profile: DatabaseProfileSummary): string {
    if (SQL_TYPES.includes(profile.type)) {
        const host = (profile.host || '').trim();
        const port = profile.port ? `:${profile.port}` : '';
        const database = (profile.database || '').trim();
        const username = (profile.username || '').trim();
        const hostPart = host ? `${host}${port}` : '';
        const authPart = username && hostPart ? `${username}@${hostPart}` : hostPart;
        if (!authPart && !database) return '';
        return database ? (authPart ? `${authPart} / ${database}` : database) : authPart;
    }
    return (profile.file_path || '').trim();
}

function Field({ label, hint, children, className, trailing }: { label: string; hint?: string; children: ReactNode; className?: string; trailing?: ReactNode }) {
    const control = trailing ? <div className="database-profiles-file-row">{children}{trailing}</div> : children;
    const Tag = trailing ? 'div' : 'label';
    return (
        <Tag className={['database-profiles-field', className].filter(Boolean).join(' ')}>
            <span>{label}</span>
            {control}
            {hint ? <small>{hint}</small> : null}
        </Tag>
    );
}
