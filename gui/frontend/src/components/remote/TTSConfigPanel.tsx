import { useState, useEffect, useCallback } from 'react';
import { GetTTSEnabled, SetTTSEnabled, GetTTSVoiceID, SetTTSVoiceID, CheckTTSModel, DownloadTTSModel } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { ModelStatusBox } from "./ModelStatusBox";

type Props = { lang: string };

const ttsVoiceOptions = [
    { id: 'zf_xiaoyi', label: 'zf_xiaoyi', zh: '小艺，中文女声，默认' },
    { id: 'zf_xiaoxiao', label: 'zf_xiaoxiao', zh: '晓晓，中文女声' },
    { id: 'zm_yunxi', label: 'zm_yunxi', zh: '云希，中文男声' },
    { id: 'zm_yunyang', label: 'zm_yunyang', zh: '云扬，中文男声' },
];

export function TTSConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const [enabled, setEnabled] = useState(false);
    const [voiceID, setVoiceID] = useState('zf_xiaoyi');
    const [modelExists, setModelExists] = useState(false);
    const [modelSize, setModelSize] = useState(0);
    const [downloading, setDownloading] = useState(false);
    const [progress, setProgress] = useState(0);
    const [downloaded, setDownloaded] = useState(0);
    const [total, setTotal] = useState(0);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        (async () => {
            try {
                const info: any = await CheckTTSModel();
                setModelExists(!!info.exists);
                setModelSize(info.size || 0);
                const selectedVoice = await GetTTSVoiceID();
                setVoiceID(selectedVoice || 'zf_xiaoyi');
                const on = await GetTTSEnabled();
                setEnabled(!!on);
            } catch {}
            setLoading(false);
        })();
    }, []);

    useEffect(() => {
        const handler = (data: any) => {
            if (data.error) { setError(data.error); setDownloading(false); return; }
            const pct = data.percent || 0;
            setProgress(pct);
            setDownloaded(data.downloaded || 0);
            setTotal(data.total || 0);
            if (pct > 0 && pct < 100) { setDownloading(true); }
            if (pct >= 100) {
                setDownloading(false);
                setModelExists(true);
                setModelSize(data.downloaded || 0);
            }
        };
        EventsOn('tts-download-progress', handler);
        return () => { EventsOff('tts-download-progress'); };
    }, []);

    const handleToggle = async (on: boolean) => {
        setEnabled(on);
        setError('');
        if (on && !modelExists) {
            setDownloading(true);
        } else if (!on) {
            setDownloading(false);
        }
        try {
            await SetTTSEnabled(on);
        } catch (e: any) {
            setEnabled(!on);
            setDownloading(false);
            setError(e?.message || String(e));
            return;
        }
    };

    const handleVoiceChange = async (nextVoiceID: string) => {
        const previousVoiceID = voiceID;
        setVoiceID(nextVoiceID);
        setError('');
        try {
            await SetTTSVoiceID(nextVoiceID);
        } catch (e: any) {
            setVoiceID(previousVoiceID);
            setError(e?.message || String(e));
        }
    };

    const startDownload = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadTTSModel(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e)));
            setDownloading(false);
        }
    };

    if (loading) return <div className="model-config-loading">{t('Loading...', '加载中...', '載入中...')}</div>;

    const accentColor = 'var(--theme-primary, #2f6fbc)';

    return (
        <div className="model-config-panel model-config-panel--spaced">
            <h4 className="model-config-heading model-config-heading--info">
                {t('TTS Model (Voice Playback)', 'TTS 语音合成模型', 'TTS 語音合成模型')}
            </h4>
            <div className="model-config-toggle-row model-config-toggle-row--compact">
                <label className="model-config-check">
                    <input type='checkbox' checked={enabled} onChange={e => handleToggle(e.target.checked)} />
                    {t('Enable TTS Voice Playback', '启用语音播报', '啟用語音播報')}
                </label>
            </div>
            <div className="model-config-inline-field">
                <label htmlFor='tts-voice-select' className="model-config-inline-label">
                    {t('Voice', '音色', '音色')}
                </label>
                <select
                    id='tts-voice-select'
                    value={voiceID}
                    onChange={e => handleVoiceChange(e.target.value)}
                    disabled={downloading}
                    className="model-config-select"
                >
                    {ttsVoiceOptions.map(option => (
                        <option key={option.id} value={option.id}>
                            {lang === 'en' ? option.label : `${option.label} - ${option.zh}`}
                        </option>
                    ))}
                </select>
            </div>
            <p className="model-config-copy">
                {t(
                    'TTS uses the local Kokoro-82M q8 model for voice playback. The model and selected voice files are downloaded from GitHub first, with Hub as fallback.',
                    'TTS 使用本地 Kokoro-82M q8 模型进行语音播报。模型和音色文件会优先从 GitHub 下载，无法访问时从 Hub 回退下载。',
                    'TTS 使用本地 Kokoro-82M q8 模型進行語音播報。模型和音色檔案會優先從 GitHub 下載，無法存取時從 Hub 回退下載。'
                )}
            </p>
            {enabled && (
                <ModelStatusBox
                    exists={modelExists} downloading={downloading} size={modelSize}
                    progress={progress} downloaded={downloaded} total={total}
                    error={error} onDownload={startDownload} onRetry={startDownload}
                    accentColor={accentColor} t={t}
                />
            )}
        </div>
    );
}
