import { useCallback, useMemo, useState } from 'react';
import {
    DistillDesktopDemoRecording,
    GetDesktopDemoShot,
    NarrateDesktopDemoRecording,
    RedistillDesktopDemoSkill,
    StartDesktopDemoRecording,
    StopDesktopDemoRecording,
    UpdateDesktopDemoSkill,
} from '../../../wailsjs/go/main/App';

type TimelineEvent = {
    at?: string;
    kind?: string;
    action?: string;
    text?: string;
    app?: string;
    window?: string;
    x?: number;
    y?: number;
    ref?: string;
};

type Timeline = {
    id?: string;
    title?: string;
    started_at?: string;
    events?: TimelineEvent[];
};

type Props = {
    botId: string;
    labels: {
        demoSkill: string;
        start: string;
        stop: string;
        narrate: string;
        pushToTalk: string;
        distill: string;
        edit: string;
        redistill: string;
        titlePlaceholder: string;
        narratePlaceholder: string;
        notesPlaceholder: string;
        busy: string;
    };
};

/* btoa over a small window; fromCharCode spread blows the stack on long audio. */
function bytesToBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    let binary = '';
    for (let i = 0; i < bytes.length; i += 0x8000) {
        binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    }
    return btoa(binary);
}

/**
 * Demonstrate-and-narrate recording panel (MACLAW_DEMO_SKILL stage 4).
 * Start/stop on an isolated bot desktop, typed or spoken narration, thumbnail
 * strip + timeline preview, then distill / edit / re-distill.
 */
export function DemoSkillPanel({ botId, labels }: Props) {
    const [title, setTitle] = useState('');
    const [recording, setRecording] = useState(false);
    const [narration, setNarration] = useState('');
    const [timeline, setTimeline] = useState<Timeline | null>(null);
    const [thumbs, setThumbs] = useState<Record<string, string>>({});
    const [skillName, setSkillName] = useState('');
    const [skillContent, setSkillContent] = useState('');
    const [notes, setNotes] = useState('');
    const [status, setStatus] = useState('');
    const [busy, setBusy] = useState(false);
    const [asrAvailable, setAsrAvailable] = useState(false);

    const shotRefs = useMemo(
        () => (timeline?.events || []).filter(e => e.kind === 'screenshot' && e.ref).map(e => e.ref as string),
        [timeline],
    );

    const loadThumbs = useCallback(async (id: string, refs: string[]) => {
        const next: Record<string, string> = {};
        for (const ref of refs.slice(0, 24)) {
            try {
                const out = await GetDesktopDemoShot(botId, id, ref) as { data?: string; mime?: string; error?: string };
                if (out?.data && !out.error) {
                    next[ref] = `data:${out.mime || 'image/png'};base64,${out.data}`;
                }
            } catch {
                /* ignore missing shots */
            }
        }
        setThumbs(next);
    }, [botId]);

    const run = async (fn: () => Promise<void>) => {
        setBusy(true);
        setStatus('');
        try {
            await fn();
        } catch (e) {
            setStatus(e instanceof Error && e.message ? e.message : String(e));
        } finally {
            setBusy(false);
        }
    };

    const onStart = () => run(async () => {
        const out = await StartDesktopDemoRecording(botId, title) as { error?: string; asr_available?: boolean; id?: string };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setAsrAvailable(!!out.asr_available);
        setRecording(true);
        setTimeline(null);
        setSkillName('');
        setSkillContent('');
    });

    const onNarrate = () => run(async () => {
        const text = narration.trim();
        if (!text) return;
        const out = await NarrateDesktopDemoRecording(botId, text, '', '') as { error?: string };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setNarration('');
    });

    const onPushToTalk = () => run(async () => {
        // Browser MediaRecorder → wav base64 when the Hub ASR is available.
        if (!asrAvailable || typeof MediaRecorder === 'undefined') {
            setStatus(labels.narratePlaceholder);
            return;
        }
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        const rec = new MediaRecorder(stream);
        const chunks: BlobPart[] = [];
        const done = new Promise<Blob>(resolve => {
            rec.ondataavailable = e => chunks.push(e.data);
            rec.onstop = () => resolve(new Blob(chunks, { type: rec.mimeType || 'audio/webm' }));
        });
        rec.start();
        await new Promise(r => setTimeout(r, 4000));
        rec.stop();
        stream.getTracks().forEach(t => t.stop());
        const blob = await done;
        const buf = await blob.arrayBuffer();
        const b64 = bytesToBase64(buf);
        const out = await NarrateDesktopDemoRecording(botId, '', b64, blob.type || 'audio/webm') as { error?: string; text?: string };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setStatus(out.text || labels.pushToTalk);
    });

    const onStop = () => run(async () => {
        const out = await StopDesktopDemoRecording(botId) as { error?: string; timeline?: Timeline };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setRecording(false);
        const tl = out.timeline || null;
        setTimeline(tl);
        if (tl?.id) {
            void loadThumbs(tl.id, (tl.events || []).filter(e => e.kind === 'screenshot' && e.ref).map(e => e.ref as string));
        }
    });

    const onDistill = () => run(async () => {
        if (!timeline) return;
        const out = await DistillDesktopDemoRecording(botId, timeline as Record<string, unknown>, notes) as {
            error?: string; item?: { name?: string; content?: string; description?: string };
        };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setSkillName(out.item?.name || '');
        setSkillContent(out.item?.content || '');
    });

    const onSaveEdit = () => run(async () => {
        if (!skillName) return;
        const out = await UpdateDesktopDemoSkill(botId, skillName, skillContent, '', []) as { error?: string };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setStatus(labels.edit);
    });

    const onRedistill = () => run(async () => {
        if (!skillName) return;
        const out = await RedistillDesktopDemoSkill(botId, skillName, notes) as {
            error?: string; item?: { name?: string; content?: string };
        };
        if (out?.error) {
            setStatus(out.error);
            return;
        }
        setSkillContent(out.item?.content || skillContent);
        setStatus(labels.redistill);
    });

    return (
        <section className="demo-skill" aria-label={labels.demoSkill}>
            <div className="demo-skill__head">
                <span className={'demo-skill__badge' + (recording ? ' is-recording' : '')} aria-hidden="true" />
                <span className="demo-skill__title">{labels.demoSkill}</span>
            </div>

            <div className="demo-skill__row">
                <input
                    className="demo-skill__input"
                    value={title}
                    onChange={e => setTitle(e.target.value)}
                    placeholder={labels.titlePlaceholder}
                    disabled={recording || busy}
                />
                {!recording ? (
                    <button type="button" className="demo-skill__btn demo-skill__btn--primary" disabled={busy || !botId} onClick={onStart}>{labels.start}</button>
                ) : (
                    <button type="button" className="demo-skill__btn demo-skill__btn--danger" disabled={busy} onClick={onStop}>{labels.stop}</button>
                )}
            </div>

            {recording && (
                <div className="demo-skill__row">
                    <input
                        className="demo-skill__input"
                        value={narration}
                        onChange={e => setNarration(e.target.value)}
                        placeholder={labels.narratePlaceholder}
                        onKeyDown={e => { if (e.key === 'Enter') onNarrate(); }}
                    />
                    <button type="button" className="demo-skill__btn" disabled={busy} onClick={onNarrate}>{labels.narrate}</button>
                    <button type="button" className="demo-skill__btn" disabled={busy || !asrAvailable} onClick={onPushToTalk}>{labels.pushToTalk}</button>
                </div>
            )}

            {timeline && (
                <div className="demo-skill__result">
                    {shotRefs.length ? (
                        <div className="demo-skill__thumbs">
                            {shotRefs.map(ref => (
                                <img key={ref} src={thumbs[ref]} alt={ref} title={ref} />
                            ))}
                        </div>
                    ) : null}
                    <pre className="demo-skill__timeline">
                        {(timeline.events || []).slice(0, 80).map((e, i) => {
                            const label = e.kind === 'screenshot' ? `画面 ${e.ref}` : e.kind === 'narration' ? `旁白 ${e.text}` :
                                e.kind === 'focus' ? `焦点 ${e.app || ''} ${e.window || ''}` :
                                    e.kind === 'input' ? `输入 ${e.action || ''} ${e.text || ''}`.trim() : e.kind;
                            return `${i + 1}. ${label}\n`;
                        }).join('')}
                    </pre>
                    <div className="demo-skill__row">
                        <input
                            className="demo-skill__input"
                            value={notes}
                            onChange={e => setNotes(e.target.value)}
                            placeholder={labels.notesPlaceholder}
                        />
                        <button type="button" className="demo-skill__btn demo-skill__btn--primary" disabled={busy} onClick={onDistill}>{labels.distill}</button>
                    </div>
                </div>
            )}

            {skillName && (
                <div className="demo-skill__result">
                    <div className="demo-skill__skill-name">{skillName}</div>
                    <textarea
                        className="demo-skill__textarea"
                        value={skillContent}
                        onChange={e => setSkillContent(e.target.value)}
                        rows={8}
                    />
                    <div className="demo-skill__row">
                        <button type="button" className="demo-skill__btn" disabled={busy} onClick={onSaveEdit}>{labels.edit}</button>
                        <button type="button" className="demo-skill__btn" disabled={busy} onClick={onRedistill}>{labels.redistill}</button>
                    </div>
                </div>
            )}

            {(status || busy) && (
                <p className="demo-skill__status" role="status">{busy ? labels.busy : status}</p>
            )}
        </section>
    );
}
