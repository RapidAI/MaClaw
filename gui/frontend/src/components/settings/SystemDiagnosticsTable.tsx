type SystemDiagnosticsTableProps = {
    diagnostics: Array<[string, string]>;
};

export const SystemDiagnosticsTable = ({ diagnostics }: SystemDiagnosticsTableProps) => (
    <div style={{ overflowX: 'auto', border: '1px solid var(--theme-border)', borderRadius: '6px', background: 'var(--theme-surface)' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.72rem', color: 'var(--theme-text-secondary)', fontFamily: 'monospace' }}>
            <tbody>
                {diagnostics.map(([label, value]) => (
                    <tr key={label}>
                        <th style={{ textAlign: 'left', width: '140px', padding: '6px 10px', borderBottom: '1px solid var(--theme-border)', color: 'var(--theme-text-muted)', fontWeight: 600, background: 'var(--theme-surface-muted)' }}>{label}</th>
                        <td style={{ padding: '6px 10px', borderBottom: '1px solid var(--theme-border)', wordBreak: 'break-all' }}>{value}</td>
                    </tr>
                ))}
            </tbody>
        </table>
    </div>
);
