type SystemDiagnosticsTableProps = {
    diagnostics: Array<[string, string]>;
};

// Dark-mode header surface is defined in CSS as var(--theme-surface-muted).

export const SystemDiagnosticsTable = ({ diagnostics }: SystemDiagnosticsTableProps) => (
    <div className="system-diagnostics-table-wrap">
        <table className="system-diagnostics-table">
            <tbody>
                {diagnostics.map(([label, value]) => (
                    <tr key={label}>
                        <th>{label}</th>
                        <td>{value}</td>
                    </tr>
                ))}
            </tbody>
        </table>
    </div>
);
