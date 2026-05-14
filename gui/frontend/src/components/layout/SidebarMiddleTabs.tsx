type MiddleTab = 'tasks' | 'employees' | 'history';

type SidebarMiddleTabsProps = {
    active: MiddleTab;
    labels: Record<MiddleTab, string>;
    onChange: (tab: MiddleTab) => void;
    visibleTabs?: MiddleTab[];
};

export const SidebarMiddleTabs = ({ active, labels, onChange, visibleTabs = ['tasks', 'employees', 'history'] }: SidebarMiddleTabsProps) => (
    <div style={{ display: 'grid', gridTemplateColumns: `repeat(${visibleTabs.length}, minmax(0, 1fr))`, gap: '4px', padding: '8px 8px 0', flexShrink: 0 }}>
        {visibleTabs.map(tab => (
            <button key={tab} type="button" onClick={() => onChange(tab)} title={labels[tab]} style={{ border: '1px solid var(--theme-border)', borderRadius: '6px', background: active === tab ? 'var(--theme-primary)' : 'var(--theme-surface)', color: active === tab ? '#fff' : 'var(--theme-text-primary)', cursor: 'pointer', fontSize: '0.68rem', fontWeight: 700, padding: '5px 4px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {labels[tab]}
            </button>
        ))}
    </div>
);
