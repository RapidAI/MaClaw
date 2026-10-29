import { useState } from 'react';
import { isChineseLang } from '../../../i18n/langSelect';
import { tailText } from '../../../i18n/mnTail';
import { humanPrompt, needsHuman, shorten, stateLabel, statusRowText, botName, type Assignment, type BotRef } from './botCollab';

/** One-line "who is working on what", the prompts that need the person, and the assignment list. */
export function BotCollabStatus({ assignments, bots, lang = 'zh-Hans' }: { assignments: Assignment[]; bots: BotRef[]; lang?: string }) {
    const [open, setOpen] = useState(false);
    const line = statusRowText(assignments, bots, 3, lang);
    const waiting = assignments.filter(needsHuman);
    const zh = isChineseLang(lang);
    if (!line && !waiting.length && !assignments.length) return null;
    return (
        <div className="bot-collab" data-testid="bot-collab">
            {waiting.map(a => {
                const p = humanPrompt(a, bots, lang);
                return p ? (
                    <div key={a.id} className={`bot-collab__prompt bot-collab__prompt--${a.state}`} role="alert" data-testid={`bot-collab-prompt-${a.state}`}>
                        <strong>{p.title}</strong>
                        <span>{p.body}</span>
                    </div>
                ) : null;
            })}
            <button type="button" className="bot-collab__row" data-testid="bot-collab-row" aria-expanded={open} onClick={() => setOpen(v => !v)}>
                <i className={line ? 'is-working' : ''} aria-hidden="true" />
                <span>{line || tailText(lang, 'No bots are busy', '没有机器人在忙')}</span>
                <small>{open ? tailText(lang, 'Hide tasks', '收起任务') : tailText(lang, `${assignments.length} tasks`, `任务 ${assignments.length}`)}</small>
            </button>
            {open ? (
                <ul className="bot-collab__list" data-testid="bot-collab-list">
                    {assignments.map(a => (
                        <li key={a.id} data-testid={`bot-collab-item-${a.id}`}>
                            <span className={`bot-collab__state bot-collab__state--${a.state}`}>{stateLabel(a.state, lang)}</span>
                            <span className="bot-collab__who">{botName(bots, a.owner_bot, lang)} → {botName(bots, a.target_bot, lang)}</span>
                            <span className="bot-collab__task" title={a.task}>{shorten(a.task, 40)}</span>
                            {a.last_report ? <small className="bot-collab__report">{tailText(lang, `Latest: ${shorten(a.last_report, 60)}`, `最新：${shorten(a.last_report, 60)}`)}</small> : null}
                            {a.reviewer ? <small>{tailText(lang, `Reviewer: ${botName(bots, a.reviewer, lang)}`, `审核人：${botName(bots, a.reviewer, lang)}`)}</small> : null}
                            {a.review ? (
                                <small className="bot-collab__review" data-testid={`bot-collab-review-${a.id}`}>
                                    {a.review === 'accepted' ? tailText(lang, 'Accepted', '已验收') : tailText(lang, 'Returned', '已退回')}{a.review_note ? `${zh ? '：' : ': '}${a.review_note}` : ''}
                                </small>
                            ) : null}
                            {a.artifacts?.length ? <small>{tailText(lang, `Outputs: ${a.artifacts.map(p => p.split(/[\\/]/).pop()).join(zh ? '、' : ', ')}`, `产物：${a.artifacts.map(p => p.split(/[\\/]/).pop()).join('、')}`)}</small> : null}
                        </li>
                    ))}
                </ul>
            ) : null}
        </div>
    );
}
