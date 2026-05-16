export function AgentNetTabContainer({ lang }: { lang: string }) {
    return (
        <div style={{ padding: '24px', textAlign: 'center', color: '#6b7280' }}>
            {lang === 'zh-Hans' ? 'AgentNet \u529f\u80fd\u5f00\u53d1\u4e2d...' : 'AgentNet coming soon...'}
        </div>
    );
}
