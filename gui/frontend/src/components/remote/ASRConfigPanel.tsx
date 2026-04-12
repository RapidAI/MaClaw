import { useState, useEffect, useCallback } from 'react';
import { GetASREnabled, SetASREnabled, CheckASRModel, DownloadASRModel } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";

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

    const [asrDownloading, setAsrDownloading] = useState(false);

    useEffect(() => {
        const handler = (data: any) => {
            if (data.error) { setError(data.error); setAsrDownloading(false); return; }
            setProgress(data.percent || 0);
            setDownloaded(data.downloaded || 0);
            setTotal(data.total || 0);
            if (data.percent >= 100) {
                setAsrDownloading(false);
                setModelExists(true);
                setModelSize(data.downloaded || 0);
                setEnabled(true); // auto-enable after download
            }
        };
        // downloadModelFrom emits embedding-download-progress; emitASRProgress emits asr-*
        EventsOn('asr-download-progress', handler);
        EventsOn('embedding-download-progress', handler);
        return () => { EventsOff('asr-download-progress'); EventsOff('embedding-download-progress'); };
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
            if (!error) setError(e?.message || String(e));
            setDownloading(false);
        }
    };

    const formatBytes = (bytes: number) => {
        if (bytes <= 0) return '0 B';
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / 1024 / 1024).toFixed(1) + ' MB';
    };

    if (loading) return <div style={{ padding: 20, color: colors.textMuted }}>{t('Loading...', '加载中...', '加載中...')}</div>;

    return (
        <div style={{ padding: '0 2px', marginTop: 20 }}>
            <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-success)', marginBottom: 12, marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
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
                <div style={{ background: colors.surface, border: `1px solid ${colors.border}`, borderRadius: 6, padding: '12px 14px' }}>
                    {modelExists && !downloading && (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                            <span style={{ color: 'var(--theme-success)', fontSize: '1rem' }}>✓</span>
                            <span style={{ fontSize: '0.8rem', color: colors.text }}>{t('Model Ready', '模型已就绪', '模型已就緒')}</span>
                            <span style={{ fontSize: '0.74rem', color: colors.textMuted, marginLeft: 'auto' }}>{formatBytes(modelSize)}</span>
                        </div>
                    )}
                    {downloading && (
                        <div>
                            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                                <span style={{ fontSize: '0.78rem', color: colors.text }}>{t('Downloading model...', '正在下载模型...', '正在下載模型...')}</span>
                                <span style={{ fontSize: '0.74rem', color: colors.textMuted }}>{progress}% — {formatBytes(downloaded)} / {total > 0 ? formatBytes(total) : '?'}</span>
                            </div>
                            <div style={{ width: '100%', height: 6, background: colors.border, borderRadius: 3, overflow: 'hidden' }}>
                                <div style={{ width: `${progress}%`, height: '100%', background: 'var(--theme-success)', borderRadius: 3, transition: 'width 0.3s ease' }} />
                            </div>
                        </div>
                    )}
                    {!modelExists && !downloading && (
                        <div>
                            <div style={{ fontSize: '0.78rem', color: colors.textSecondary, marginBottom: 8 }}>
                                {t('Model file not found. Download required.', '模型文件未找到，需要下载。', '模型文件未找到，需要下載。')}
                            </div>
                            <button onClick={startDownload} style={{ padding: '6px 16px', fontSize: '0.78rem', background: 'var(--theme-success)', color: 'var(--theme-on-primary)', border: 'none', borderRadius: 4, cursor: 'pointer' }}>
                                {t('Download Model', '下载模型', '下載模型')}
                            </button>
                        </div>
                    )}
                    {error && (
                        <div style={{ marginTop: 8 }}>
                            <span style={{ fontSize: '0.76rem', color: 'var(--theme-danger)' }}>{t('Error: ', '错误：', '錯誤：')}{error}</span>
                            {!downloading && (
                                <button onClick={startDownload} style={{ marginLeft: 10, padding: '4px 12px', fontSize: '0.74rem', background: colors.surface, color: colors.text, border: `1px solid ${colors.border}`, borderRadius: 4, cursor: 'pointer' }}>
                                    {t('Retry', '重试', '重試')}
                                </button>
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
