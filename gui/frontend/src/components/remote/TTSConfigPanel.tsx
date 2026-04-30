import { useState, useEffect, useCallback } from 'react';
import { GetTTSEnabled, SetTTSEnabled, CheckTTSModel, DownloadTTSModel } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { ModelStatusBox } from "./ModelStatusBox";

type Props = { lang: string };

export function TTSConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const [enabled, setEnabled] = useState(false);
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
                setModelExists(info.exists);
                setModelSize(info.size || 0);
                const on = await GetTTSEnabled();
                if (info.exists && !on) {
                    await SetTTSEnabled(true);
                    setEnabled(true);
                } else {
                    setEnabled(on);
                }
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
            // Detect background download in progress (started before panel opened)
            if (pct > 0 && pct < 100) { setDownloading(true); }
            if (pct >= 100) {
                setDownloading(false);
                setModelExists(true);
                setModelSize(data.downloaded || 0);
                setEnabled(true);
            }
        };
        EventsOn('tts-download-progress', handler);
        return () => { EventsOff('tts-download-progress'); };
    }, []);

    const handleToggle = async (on: boolean) => {
        setEnabled(on);
        setError('');
        try { await SetTTSEnabled(on); } catch (e: any) { setError(e?.message || String(e)); return; }
        if (on && !modelExists && !downloading) { startDownload(); }
    };

    const startDownload = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadTTSModel(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e)));
            setDownloading(false);
        }
    };

    if (loading) return <div style={{ padding: 20, color: colors.textMuted }}>{t('Loading...', '加载中...', '加載中...')}</div>;

    const accentColor = 'var(--theme-info, #409eff)';

    return (
        <div style={{ padding: '0 2px', marginTop: 20 }}>
            <h4 style={{ fontSize: '0.8rem', color: accentColor, marginBottom: 12, marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                {t('TTS Model (Voice Playback)', 'TTS 语音合成模型', 'TTS 語音合成模型')}
            </h4>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16 }}>
                <label style={{ fontSize: '0.82rem', color: colors.text, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
                    <input type='checkbox' checked={enabled} onChange={e => handleToggle(e.target.checked)} disabled={downloading} style={{ width: 16, height: 16, cursor: 'pointer' }} />
                    {t('Enable TTS Voice Playback', '启用语音播报', '啟用語音播報')}
                </label>
            </div>
            <p style={{ fontSize: '0.76rem', color: colors.textSecondary, margin: '0 0 16px 0', lineHeight: 1.5 }}>
                {t(
                    'TTS uses MeloTTS model for local voice synthesis. When enabled, task completion status will be read aloud. The model (~100MB) will be downloaded from GitHub or Hub.',
                    'TTS 使用 MeloTTS 模型进行本地语音合成。开启后，任务完成状态将自动语音播报。模型文件约 100MB，将从 GitHub 或 Hub 下载到本地。',
                    'TTS 使用 MeloTTS 模型進行本地語音合成。開啟後，任務完成狀態將自動語音播報。模型文件約 100MB，將從 GitHub 或 Hub 下載到本地。'
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
