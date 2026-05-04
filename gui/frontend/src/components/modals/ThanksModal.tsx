import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { MarkdownLink } from '../common/MarkdownLink';

type ThanksModalProps = {
    content: string;
    t: (key: string) => string;
    onClose: () => void;
};

export const ThanksModal = ({ content, t, onClose }: ThanksModalProps) => (
    <div className="modal-overlay">
        <div className="modal-content elegant-scrollbar" style={{ maxWidth: '600px', maxHeight: '80vh', overflowY: 'auto' }}>
            <h3 style={{ marginTop: 0, marginBottom: '15px', color: '#6366f1' }}>{t("thanks")}</h3>
            <div className="markdown-content" style={{
                backgroundColor: '#fff',
                padding: '10px',
                borderRadius: '4px',
                border: '1px solid var(--border-color)',
                fontFamily: 'inherit',
                fontSize: '0.8rem',
                lineHeight: '1.6',
                color: '#374151',
                textAlign: 'left',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word'
            }}>
                <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    // @ts-ignore
                    rehypePlugins={[rehypeRaw]}
                    components={{ a: MarkdownLink }}
                >
                    {content}
                </ReactMarkdown>
            </div>
            <button onClick={onClose} className="btn-secondary" style={{ marginTop: '20px' }}>
                {t("close")}
            </button>
        </div>
    </div>
);
