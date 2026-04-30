import { useState, useEffect, useCallback } from 'react';
import { GetASREnabled, SetASREnabled, CheckASRModel, DownloadASRModel } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { ModelStatusBox } from "./ModelStatusBox";

type Props = { lang: string };

export function ASRConfigPanel({ lang }: Props) {
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
                const info: any = await CheckASRModel();
                setModelExists(info.exists);
                setModelSize(info.size || 0);
                const on = await GetASREnabled();
                // If model exists, always show as enabled (auto-enable)
                if (info.exists && !on) {
                    await SetASREnabled(true);
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
                setEnabled(true); // auto-enable after download
            }
        };
        EventsOn('asr-download-progress', handler);
        return () => { EventsOff('asr-download-progress'); };
    }, []);

    const handleToggle = async (on: boolean) => {
        setEnabled(on);
        setError('');
        try { await SetASREnabled(on); } catch (e: any) { setError(e?.message || String(e)); return; }
        if (on && !modelExists && !downloading) { startDownload(); }
    };

    const startDownload = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadASRModel(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e)));
            setDownloading(false);
        }
    };

    if (loading) return <div style={{ padding: 20, color: colors.textMuted }}>{t('Loading...', '加载中...', '加載中...')}</div>;

    const accentColor = 'var(--theme-success)';

    return (
        <div style={{ padding: '0 2px', marginTop: 20 }}>
            <h4 style={{ fontSize: '0.8rem', color: accentColor, marginBottom: 12, marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                {t('Speech Recognition Model', '语音识别模型', '語音識別模型')}
            </h4>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16 }}>
                <label style={{ fontSize: '0.82rem', color: colors.text, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
                    <input type='checkbox' checked={enabled} onChange={e => handleToggle(e.target.checked)} disabled={downloading} style={{ width: 16, height: 16, cursor: 'pointer' }} />
                    {t('Enable Speech Recognition', '启用语音识别', '啟用語音識別')}
                </label>
            </div>
            <p style={{ fontSize: '0.76rem', color: colors.textSecondary, margin: '0 0 16px 0', lineHeight: 1.5 }}>
                {t(
                    'Speech recognition uses Moonshine Base Chinese model to transcribe IM voice messages. The model (~200MB) will be downloaded from GitHub or Hub.',
                    '语音识别使用 Moonshine Base 中文模型，将 IM 语音消息自动转为文字。模型文件约 200MB，将从 GitHub 或 Hub 下载到本地。',
                    '語音識別使用 Moonshine Base 中文模型，將 IM 語音消息自動轉為文字。模型文件約 200MB，將從 GitHub 或 Hub 下載到本地。'
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
