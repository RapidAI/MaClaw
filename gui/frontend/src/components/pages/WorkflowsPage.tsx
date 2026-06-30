import { useState, useMemo, useCallback } from 'react';
import { StartWorkflowDirect } from '../../../wailsjs/go/main/App';
import { getAllWorkflowShortcuts } from '../remote/WorkflowShortcutsSection';
import './WorkflowsPage.css';

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

const localizeTextForLang = (lang?: string): LocalizeText => {
    if (!lang || lang.startsWith('zh-Hans') || (lang.startsWith('zh') && !lang.startsWith('zh-Hant')))
        return (_en, zhHans, _zhHant) => zhHans;
    if (lang.startsWith('zh-Hant'))
        return (_en, _zhHans, zhHant) => zhHant;
    return (en) => en;
};

export const WorkflowsPage = ({ lang, switchToAI }: { lang?: string; switchToAI?: () => void }) => {
    const localizeText = useMemo(() => localizeTextForLang(lang), [lang]);
    const allGroups = useMemo(() => getAllWorkflowShortcuts(localizeText), [localizeText]);
    const [query, setQuery] = useState('');
    const [startingType, setStartingType] = useState<string | null>(null);

    const isZh = !lang || lang.startsWith('zh');
    const title = isZh ? '工作流' : 'Workflows';
    const subtitle = isZh ? '选择一个工作流模板，点击即可直接启动' : 'Select a workflow template to start immediately';
    const searchPlaceholder = isZh ? '搜索工作流...' : 'Search workflows...';

    const filteredGroups = useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return allGroups;
        return allGroups
            .map(group => ({
                ...group,
                items: group.items.filter(item =>
                    item.label.toLowerCase().includes(q) ||
                    item.description.toLowerCase().includes(q) ||
                    item.type.toLowerCase().includes(q)
                ),
            }))
            .filter(group => group.items.length > 0);
    }, [allGroups, query]);

    const handleClick = useCallback((workflowType: string) => {
        if (startingType) return;
        setStartingType(workflowType);

        // Find the label for this workflow type to show in the status message
        const item = allGroups.flatMap(g => g.items).find(i => i.type === workflowType);
        const label = item?.label || workflowType;

        // Store starting state so the AI assistant panel can pick it up on mount/render.
        sessionStorage.setItem('maclaw:workflow-starting', JSON.stringify({
            workflowType, label, ts: Date.now(), activateLocal: true,
        }));

        // Switch to AI assistant panel and activate the local tab.
        if (switchToAI) switchToAI();

        // Nudge the panel to consume sessionStorage (for the already-mounted case)
        setTimeout(() => window.dispatchEvent(new Event('maclaw:workflow-starting-nudge')), 0);
        setTimeout(() => {
            StartWorkflowDirect(workflowType, '')
                .catch(err => console.warn('[WorkflowsPage] StartWorkflowDirect failed:', err))
                .finally(() => setTimeout(() => setStartingType(null), 2000));
        }, 50);
    }, [startingType, switchToAI, allGroups]);

    return (
        <div className="workflows-page">
            <div className="workflows-page__header">
                <div>
                    <h1 className="workflows-page__title">{title}</h1>
                    <p className="workflows-page__subtitle">{subtitle}</p>
                </div>
                <div className="workflows-page__search-wrap">
                    <input
                        className="workflows-page__search"
                        value={query}
                        onChange={e => setQuery(e.target.value)}
                        placeholder={searchPlaceholder}
                        spellCheck={false}
                    />
                    {query && (
                        <button className="workflows-page__search-clear" type="button" onClick={() => setQuery('')}>×</button>
                    )}
                </div>
            </div>

            <div className="workflows-page__body elegant-scrollbar">
                {filteredGroups.length === 0 && (
                    <div className="workflows-page__empty">
                        {isZh ? '没有匹配的工作流' : 'No matching workflows'}
                    </div>
                )}
                {filteredGroups.map(group => (
                    <section key={group.category} className="workflows-page__section">
                        <h3 className="workflows-page__section-title">{group.category}</h3>
                        <div className="workflows-page__grid">
                            {group.items.map(item => (
                                <button
                                    key={item.type}
                                    type="button"
                                    className={`workflows-page__tile ${startingType === item.type ? 'is-starting' : ''}`}
                                    title={item.description}
                                    disabled={!!startingType}
                                    onClick={() => handleClick(item.type)}
                                >
                                    <span className="workflows-page__tile-icon">{item.icon}</span>
                                    <span className="workflows-page__tile-label">{item.label}</span>
                                    <span className="workflows-page__tile-desc">{item.description}</span>
                                </button>
                            ))}
                        </div>
                    </section>
                ))}
            </div>
        </div>
    );
};

export default WorkflowsPage;
