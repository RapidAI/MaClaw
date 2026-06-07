type MiddleTab = 'tasks' | 'employees' | 'history';

type SidebarMiddleTabsProps = {
    active: MiddleTab;
    labels: Record<MiddleTab, string>;
    onChange: (tab: MiddleTab) => void;
    visibleTabs?: MiddleTab[];
};

export const SidebarMiddleTabs = ({ active, labels, onChange, visibleTabs = ['tasks', 'employees', 'history'] }: SidebarMiddleTabsProps) => (
    <div style={{ display: 'flex', borderBottom: '1px solid var(--theme-border)', padding: '0 8px', flexShrink: 0 }}>
        {visibleTabs.map(tab => (
            <button key={tab} type="button" onClick={() => onChange(tab)} title={labels[tab]} style={{ flex: 1, border: 'none', borderBottom: active === tab ? '2px solid var(--theme-primary)' : '2px solid transparent', marginBottom: '-1px', background: 'transparent', color: active === tab ? 'var(--theme-primary)' : 'var(--theme-text-secondary)', cursor: 'pointer', fontSize: '0.72rem', fontWeight: active === tab ? 700 : 500, padding: '8px 4px 6px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', transition: 'color 150ms, border-color 150ms' }}>
                {labels[tab]}
            </button>
        ))}
    </div>
);
