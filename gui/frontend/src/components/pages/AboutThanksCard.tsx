import { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { ReadThanks } from '../../../wailsjs/go/main/App';
import { MarkdownLink } from '../common/MarkdownLink';

type AboutThanksCardProps = {
    t: (key: string) => string;
};

export const AboutThanksCard = ({ t }: AboutThanksCardProps) => {
    const [content, setContent] = useState('');

    useEffect(() => {
        let cancelled = false;
        ReadThanks()
            .then(text => { if (!cancelled) setContent(text || ''); })
            .catch(err => {
                console.error('Failed to read thanks content:', err);
                if (!cancelled) setContent('');
            });
        return () => { cancelled = true; };
    }, []);

    if (!content.trim()) return null;

    return (
        <section className="about-card about-thanks-card">
            <div className="about-thanks-header">
                <h3 className="about-thanks-title">{t('thanks')}</h3>
            </div>
            <div className="about-thanks-content">
                <ReactMarkdown remarkPlugins={[remarkGfm]}
                    // @ts-ignore -- rehype-raw pulls a nested vfile type in this dependency tree.
                    rehypePlugins={[rehypeRaw]}
                    components={{ a: MarkdownLink }}>
                    {content}
                </ReactMarkdown>
            </div>
        </section>
    );
};
