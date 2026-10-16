import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { MarkdownLink } from '../common/MarkdownLink';

interface TutorialPageProps {
    lang: string;
    refreshStatus: string;
    refreshKey: number;
    tutorialContent: string;
    switchTool: (tool: string) => void;
}

export const TutorialPage = ({ lang, refreshStatus, refreshKey, tutorialContent, switchTool }: TutorialPageProps) => (
                        <div className="secondary-page-shell tutorial-page" style={{
                            width: '100%',
                            padding: '0 15px',
                            boxSizing: 'border-box'
                        }}>
                            <div style={{ marginBottom: '8px' }}>
                                <button
                                    className="btn-link"
                                    onClick={() => switchTool('ai')}
                                    style={{
                                        fontSize: '0.8rem',
                                        padding: '4px 12px',
                                        cursor: 'pointer',
                                        display: 'inline-flex',
                                        alignItems: 'center',
                                        gap: '4px',
                                    }}
                                >
                                    ← {lang === 'en' ? 'Back to AI Assistant' : lang === 'zh-Hant' ? '返回 AI 助手' : '返回 AI 助手'}
                                </button>
                            </div>
                            <div style={{
                                position: 'relative',
                                marginBottom: '5px'
                            }}>
                                {refreshStatus && (
                                    <div className="tutorial-refresh-status">
                                        {refreshStatus}
                                    </div>
                                )}
                            </div>

                            <div className="markdown-content" style={{
                                backgroundColor: 'var(--theme-surface, #fff)',
                                padding: '20px',
                                borderRadius: '8px',
                                border: '1px solid var(--theme-border)',
                                fontFamily: 'inherit',
                                fontSize: '0.75rem',
                                lineHeight: '1.6',
                                color: 'var(--theme-text-primary, #374151)',
                                marginBottom: '20px',
                                textAlign: 'left'
                            }}>
                                <ReactMarkdown
                                    key={refreshKey}
                                    remarkPlugins={[remarkGfm]}
                                    // @ts-ignore - rehype-raw type compatibility
                                    components={{ a: MarkdownLink }}
                                >
                                    {tutorialContent}
                                </ReactMarkdown>
                            </div>
                        </div>
);
