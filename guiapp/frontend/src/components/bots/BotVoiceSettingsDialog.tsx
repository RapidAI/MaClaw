import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';
import { localizeText } from '../../i18n';
import {
    GetTTSEnabled,
    GetTTSVoiceID,
    LoadConfig,
    PatchConfigFields,
    SetTTSEnabled,
    SetTTSVoiceID,
    SynthesizeTTSPreview,
} from '../../../wailsjs/go/main/App';
import { ttsVoiceOptions } from '../../constants/ttsVoices';

function words(lang: string) {
    const L = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    return {
        title: L('Voice', '语音', '語音'),
        hint: L(
            'Speech runs on this computer with the local Kokoro model. The same voice is shared with the desktop pet and the assistant.',
            '语音由这台电脑上的本地 Kokoro 模型合成，音色与桌宠、助手共用。',
            '語音由這台電腦上的本機 Kokoro 模型合成，音色與桌寵、助手共用。'),
        speak: L('Speak bot replies aloud', '语音播报', '語音播報'),
        speakHint: L('Master switch for voice playback on this computer.', '这台电脑上所有语音播放的总开关。', '這台電腦上所有語音播放的總開關。'),
        readback: L('Read new bot replies aloud', '自动朗读 Bot 回复', '自動朗讀 Bot 回覆'),
        readbackHint: L('When a reply lands, this computer reads the bubble text aloud.', '收到 Bot 回复时，朗读气泡里的文字。', '收到 Bot 回覆時，朗讀氣泡裡的文字。'),
        voice: L('Voice', '音色', '音色'),
        preview: L('Preview', '试听', '試聽'),
        previewing: L('Generating…', '生成中…', '生成中…'),
        previewLabel: L('Preview text', '试听文本', '試聽文本'),
        previewDefault: L('Hello! Welcome to MaClaw.', '你好，我是 MaClaw。', '你好，我是 MaClaw。'),
        loading: L('Loading…', '加载中…', '載入中…'),
        close: L('Close', '关闭', '關閉'),
    };
}

type Props = {
    heading: string;
    tabs?: ReactNode;
    lang: string;
    onClose: () => void;
};

/**
 * The 语音 tab of the Bot 管理 Settings dialog. Everything applies at once —
 * the toggles and the voice id are desktop config, saved by the backend the
 * moment they change.
 */
export function BotVoiceSettingsDialog({ heading, tabs, lang, onClose }: Props) {
    const w = words(lang);
    const titleId = useId();
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const [loading, setLoading] = useState(true);
    const [notice, setNotice] = useState('');
    const [enabled, setEnabled] = useState(false);
    const [readback, setReadback] = useState(false);
    const [voiceID, setVoiceID] = useState('');
    const [previewText, setPreviewText] = useState('');
    const [previewing, setPreviewing] = useState(false);
    const [busy, setBusy] = useState(false);
    const previewAudioRef = useRef<HTMLAudioElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !busy });
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!busy) onClose(); } });

    useEffect(() => {
        let live = true;
        setPreviewText(w.previewDefault);
        Promise.all([GetTTSEnabled(), GetTTSVoiceID(), LoadConfig()])
            .then(([on, id, cfg]) => {
                if (!live) return;
                setEnabled(!!on);
                setVoiceID(String(id || ''));
                setReadback(!!cfg?.bot_voice_readback_enabled);
            })
            .catch((error: unknown) => {
                if (live) setNotice(error instanceof Error ? error.message : String(error));
            })
            .finally(() => { if (live) setLoading(false); });
        return () => {
            live = false;
            previewAudioRef.current?.pause();
            previewAudioRef.current = null;
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const fail = (error: unknown) => setNotice(error instanceof Error ? error.message : String(error));

    const toggleSpeak = async (next: boolean) => {
        const prev = enabled;
        setEnabled(next);
        setNotice('');
        setBusy(true);
        try {
            await SetTTSEnabled(next);
        } catch (error) {
            setEnabled(prev);
            fail(error);
        } finally {
            setBusy(false);
        }
    };

    const toggleReadback = async (next: boolean) => {
        const prev = readback;
        setReadback(next);
        setNotice('');
        setBusy(true);
        try {
            await PatchConfigFields({ bot_voice_readback_enabled: next });
        } catch (error) {
            setReadback(prev);
            fail(error);
        } finally {
            setBusy(false);
        }
    };

    const changeVoice = async (next: string) => {
        const prev = voiceID;
        setVoiceID(next);
        setNotice('');
        setBusy(true);
        try {
            await SetTTSVoiceID(next);
        } catch (error) {
            setVoiceID(prev);
            fail(error);
        } finally {
            setBusy(false);
        }
    };

    const previewVoice = async () => {
        const text = previewText.trim();
        if (!text || previewing) return;
        setPreviewing(true);
        setNotice('');
        try {
            const source = await SynthesizeTTSPreview(text);
            previewAudioRef.current?.pause();
            const audio = new Audio(source);
            previewAudioRef.current = audio;
            // Stay in previewing until playback actually ends so repeated
            // clicks cannot stack overlapping previews (TTSConfigPanel does
            // the same). Resolve on error too so the button unsticks.
            await new Promise<void>((resolve) => {
                const finish = () => {
                    audio.onended = null;
                    audio.onerror = null;
                    if (previewAudioRef.current === audio) previewAudioRef.current = null;
                    resolve();
                };
                audio.onended = finish;
                audio.onerror = finish;
                void audio.play().catch(() => finish());
            });
        } catch (error) {
            fail(error);
        } finally {
            setPreviewing(false);
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div className="modal-content custom-dialog bot-llm-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-voice-dialog" tabIndex={-1} {...dialogProps}>
                <div className="bot-llm-dialog__head">
                    <div className="modal-header custom-dialog__header">
                        <div className="custom-dialog__heading">
                            <h3 id={titleId} className="custom-dialog__title">{heading}</h3>
                        </div>
                        <button type="button" className="btn-close custom-dialog__close" aria-label={w.close} disabled={busy} onClick={onClose}>
                            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
                        </button>
                    </div>
                    {tabs}
                </div>
                <div className="modal-body custom-dialog__body">
                    <h4 className="bot-llm-dialog__section">{w.title}</h4>
                    <p className="bot-llm-dialog__hint">{w.hint}</p>
                    {loading ? <p className="bot-llm-dialog__subhint" data-testid="bot-voice-loading">{w.loading}</p> : (
                        <>
                            <label className="bot-llm-dialog__radio">
                                <input type="checkbox" checked={enabled} disabled={busy} data-testid="bot-voice-enabled" onChange={e => { void toggleSpeak(e.target.checked); }} />
                                <span>{w.speak}</span>
                            </label>
                            <p className="bot-llm-dialog__subhint">{w.speakHint}</p>
                            <label className="bot-llm-dialog__radio">
                                <input type="checkbox" checked={readback} disabled={busy} data-testid="bot-voice-readback" onChange={e => { void toggleReadback(e.target.checked); }} />
                                <span>{w.readback}</span>
                            </label>
                            <p className="bot-llm-dialog__subhint">{w.readbackHint}</p>
                            <label className="bot-local-dialog__field">
                                <span>{w.voice}</span>
                                <select className="form-input" value={voiceID} disabled={busy} data-testid="bot-voice-select" onChange={e => { void changeVoice(e.target.value); }}>
                                    {ttsVoiceOptions.map(option => (
                                        <option key={option.id} value={option.id}>{lang.startsWith('zh') ? `${option.label} - ${option.zh}` : option.label}</option>
                                    ))}
                                </select>
                            </label>
                            <label className="bot-local-dialog__field bot-local-dialog__field--block">
                                <span>{w.previewLabel}</span>
                                <input className="form-input" value={previewText} maxLength={200} disabled={busy} data-testid="bot-voice-preview-text" onChange={e => setPreviewText(e.target.value)} />
                            </label>
                            <button type="button" className="btn-secondary" data-testid="bot-voice-preview" disabled={busy || previewing || !previewText.trim()} onClick={() => { void previewVoice(); }}>
                                {previewing ? w.previewing : w.preview}
                            </button>
                        </>
                    )}
                    {notice ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-voice-notice">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-primary" onClick={onClose} disabled={busy}>{w.close}</button>
                </div>
            </div>
        </div>
    );
}
