import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { MarkdownLink } from '../common/MarkdownLink';

interface TutorialPageProps {
    lang: string;
    refreshStatus: string;
    refreshKey: number;
    tutorialContent: string;
    switchTool: (tool: string) => void;
}

export const TutorialPage = ({ lang, refreshStatus, refreshKey, tutorialContent, switchTool }: TutorialPageProps) => (
                        <div style={{
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
                                    <div style={{
                                        position: 'absolute',
                                        top: '0',
                                        right: '0',
                                        zIndex: 100,
                                        padding: '4px 12px',
                                        backgroundColor: 'var(--theme-info-bg, rgba(224, 242, 254, 0.95))',
                                        borderRadius: '16px',
                                        color: 'var(--theme-primary, #0369a1)',
                                        fontSize: '0.75rem',
                                        fontWeight: 'bold',
                                        boxShadow: '0 4px 6px rgba(0,0,0,0.1)',
                                        backdropFilter: 'blur(4px)',
                                        animation: 'fadeIn 0.3s ease-out'
                                    }}>
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
                                    rehypePlugins={[rehypeRaw]}
                                    components={{ a: MarkdownLink }}
                                >
                                    {tutorialContent}
                                </ReactMarkdown>
                            </div>
                        </div>
);
