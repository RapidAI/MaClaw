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
        <div className="modal-content elegant-scrollbar thanks-modal">
            <h3>{t("thanks")}</h3>
            <div className="markdown-content thanks-modal__content">
                <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    // @ts-ignore
                    rehypePlugins={[rehypeRaw]}
                    components={{ a: MarkdownLink }}
                >
                    {content}
                </ReactMarkdown>
            </div>
            <button onClick={onClose} className="btn-secondary thanks-modal__close">
                {t("close")}
            </button>
        </div>
    </div>
);
