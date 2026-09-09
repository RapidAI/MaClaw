import { knowledge } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import type { ExperienceDraft } from './codingKnowledgeHelpers';

const textForLang = localizeText;

export function CodingKnowledgeEditorDialog({
    lang,
    draft,
    editorSaving,
    onChange,
    onCancel,
    onSave,
}: {
    lang: string;
    draft: ExperienceDraft;
    editorSaving: boolean;
    onChange: (draft: ExperienceDraft) => void;
    onCancel: () => void;
    onSave: () => void;
}) {
    return (
        <div className="prog-tools__kb-editor" role="dialog" aria-label={textForLang(lang, 'Edit Experience', '\u7f16\u8f91\u7ecf\u9a8c', '\u7de8\u8f2f\u7d93\u9a57')}>
            <h4>{textForLang(lang, 'Edit Experience', '\u7f16\u8f91\u7ecf\u9a8c', '\u7de8\u8f2f\u7d93\u9a57')}</h4>
            <label>
                {textForLang(lang, 'Title', '\u6807\u9898', '\u6a19\u984c')}
                <input value={draft.title} onChange={(e) => onChange({ ...draft, title: e.target.value })} />
            </label>
            <label>
                {textForLang(lang, 'Content', '\u5185\u5bb9', '\u5167\u5bb9')}
                <textarea value={draft.content} onChange={(e) => onChange({ ...draft, content: e.target.value })} />
            </label>
            <div className="prog-tools__kb-editor-actions">
                <button type="button" className="prog-tools__kb-btn" onClick={onCancel} disabled={editorSaving}>
                    {textForLang(lang, 'Cancel', '\u53d6\u6d88', '\u53d6\u6d88')}
                </button>
                <button type="button" className="prog-tools__kb-btn prog-tools__kb-btn--confirm" onClick={onSave} disabled={editorSaving}>
                    {textForLang(lang, 'Save', '\u4fdd\u5b58', '\u4fdd\u5b58')}
                </button>
            </div>
        </div>
    );
}

export function CodingKnowledgeAuditDialog({
    lang,
    experience,
    events,
    onClose,
}: {
    lang: string;
    experience: any;
    events: knowledge.CodingExperienceLifecycleEvent[];
    onClose: () => void;
}) {
    return (
        <div className="prog-tools__kb-editor" role="dialog" aria-label={textForLang(lang, 'Experience audit', '\u7ecf\u9a8c\u5ba1\u8ba1', '\u7d93\u9a57\u7a3d\u6838')}>
            <h4>{textForLang(lang, 'Experience audit', '\u7ecf\u9a8c\u5ba1\u8ba1', '\u7d93\u9a57\u7a3d\u6838')}</h4>
            <p>{experience.title}</p>
            <p>{textForLang(lang, 'Origin', '\u6765\u6e90', '\u4f86\u6e90')}: {experience.created_by || textForLang(lang, 'legacy', '\u65e7\u8bb0\u5f55', '\u820a\u8a18\u9304')}</p>
            <p>{textForLang(lang, 'Last reviewed', '\u6700\u8fd1\u5ba1\u6838', '\u6700\u8fd1\u5be9\u6838')}: {experience.last_reviewed_at ? new Date(experience.last_reviewed_at).toLocaleString() : textForLang(lang, 'Not reviewed', '\u672a\u5ba1\u6838', '\u672a\u5be9\u6838')}</p>
            {events.length === 0 ? (
                <p>{textForLang(lang, 'No lifecycle events recorded.', '\u5c1a\u65e0\u751f\u547d\u5468\u671f\u8bb0\u5f55\u3002', '\u5c1a\u7121\u751f\u547d\u9031\u671f\u8a18\u9304\u3002')}</p>
            ) : (
                <ul>
                    {events.map((event, index) => (
                        <li key={`${event.occurred_at || 'event'}-${index}`}>
                            <strong>{event.action}</strong>
                            {event.reason ? ` \u2014 ${event.reason}` : ''}
                            {event.related_id ? ` (${event.related_id})` : ''}
                            {event.occurred_at ? ` \u00b7 ${new Date(event.occurred_at).toLocaleString()}` : ''}
                        </li>
                    ))}
                </ul>
            )}
            <div className="prog-tools__kb-editor-actions">
                <button type="button" className="prog-tools__kb-btn" onClick={onClose}>
                    {textForLang(lang, 'Close', '\u5173\u95ed', '\u95dc\u9589')}
                </button>
            </div>
        </div>
    );
}
