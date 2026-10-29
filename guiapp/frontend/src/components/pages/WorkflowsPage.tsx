import { useState, useMemo, useCallback, useRef } from 'react';
import { localizeText as textForLang } from '../../i18n';
import { chromeText } from '../../i18n/chromeText';
import { getAllWorkflowShortcuts, WorkflowShortcutIcon } from '../remote/WorkflowShortcutsSection';
import './WorkflowsPage.css';

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

const localizeTextForLang = (lang?: string): LocalizeText =>
    (en, zhHans, zhHant) => textForLang(lang, en, zhHans, zhHant);

export const WorkflowsPage = ({ lang, onStartWorkflow }: { lang?: string; onStartWorkflow: (workflowType: string, label: string) => Promise<void> | void }) => {
    const localizeText = useMemo(() => localizeTextForLang(lang), [lang]);
    const allGroups = useMemo(() => getAllWorkflowShortcuts(localizeText), [localizeText]);
    const [query, setQuery] = useState('');
    const [startingType, setStartingType] = useState<string | null>(null);
    const [startError, setStartError] = useState<string | null>(null);
    const startingTypeRef = useRef<string | null>(null);

    const title = chromeText(lang, 'Workflows', '工作流');
    const subtitle = chromeText(lang, 'Select a workflow template to continue in the AI assistant', '选择一个工作流模板，点击后在 AI 助手中继续');
    const searchPlaceholder = chromeText(lang, 'Search workflows...', '搜索工作流...');

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

    const handleClick = useCallback(async (workflowType: string) => {
        // State updates commit after this event handler returns. Keep a ref as
        // the immediate guard so double-clicks cannot enqueue two workflows
        // before React has rendered the disabled state.
        if (startingTypeRef.current) return;

        const item = allGroups.flatMap(g => g.items).find(i => i.type === workflowType);
        const label = item?.label || workflowType;
        startingTypeRef.current = workflowType;
        setStartingType(workflowType);
        setStartError(null);

        try {
            await onStartWorkflow(workflowType, label);
        } catch (error) {
            console.warn('[WorkflowsPage] failed to open workflow assistant tab:', error);
            setStartError(chromeText(
                lang,
                'Unable to open this workflow. Please try again.',
                '暂时无法打开工作流，请重试。',
            ));
        } finally {
            if (startingTypeRef.current === workflowType) {
                startingTypeRef.current = null;
                setStartingType(null);
            }
        }
    }, [onStartWorkflow, allGroups, lang]);

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
                {startError && (
                    <div className="workflows-page__start-error" role="alert">
                        {startError}
                    </div>
                )}
                {filteredGroups.length === 0 && (
                    <div className="workflows-page__empty">
                        {chromeText(lang, 'No matching workflows', '没有匹配的工作流')}
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
                                    <span className="workflows-page__tile-icon" aria-hidden="true">
                                        <WorkflowShortcutIcon name={item.icon} size={22} />
                                    </span>
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
