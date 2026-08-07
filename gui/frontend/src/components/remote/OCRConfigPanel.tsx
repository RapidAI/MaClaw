import { useCallback, useEffect, useState } from 'react';
import { CheckOCRModel, DownloadOCRModel, GetOCREnabled, SetOCREnabled } from '../../../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { ModelStatusBox } from './ModelStatusBox';

type Props = { lang: string };

// OCRConfigPanel manages the local PP-OCRv6 detection/recognition models
// used for screen text extraction (computer use, GUI automation). It mirrors
// the ASR/Diarization panels: toggle + download state driven by the
// 'ocr-download-progress' backend event.
export function OCRConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const [enabled, setEnabled] = useState(true);
    const [exists, setExists] = useState(false);
    const [size, setSize] = useState(0);
    const [downloading, setDownloading] = useState(false);
    const [progress, setProgress] = useState(0);
    const [downloaded, setDownloaded] = useState(0);
    const [total, setTotal] = useState(0);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);

    const refresh = useCallback(async () => {
        const info: any = await CheckOCRModel();
        setExists(!!info.exists);
        setSize(info.size || 0);
    }, []);

    useEffect(() => {
        Promise.all([refresh(), GetOCREnabled().then(setEnabled)]).catch(() => {}).finally(() => setLoading(false));
    }, [refresh]);

    useEffect(() => {
        const handler = (data: any) => {
            if (data.error) { setError(data.error); setDownloading(false); return; }
            const pct = data.percent || 0;
            setProgress(pct); setDownloaded(data.downloaded || 0); setTotal(data.total || 0);
            if (pct > 0 && pct < 100) setDownloading(true);
            if (pct >= 100) {
                setDownloading(false);
                void refresh();
            }
        };
        EventsOn('ocr-download-progress', handler);
        return () => { EventsOff('ocr-download-progress'); };
    }, [refresh]);

    const toggle = async (next: boolean) => {
        setEnabled(next); setError('');
        if (next && !exists) setDownloading(true);
        if (!next) setDownloading(false);
        try { await SetOCREnabled(next); } catch (e: any) {
            setEnabled(!next); setDownloading(false); setError(e?.message || String(e));
        }
    };
    const download = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadOCRModel(); await refresh(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e))); setDownloading(false);
        }
    };

    if (loading) return <div className="model-config-loading">{t('Loading...', '加载中...', '加載中...')}</div>;
    return <div className="model-config-panel model-config-panel--spaced">
        <h4 className="model-config-heading model-config-heading--success">{t('OCR Model', 'OCR 文字识别模型', 'OCR 文字識別模型')}</h4>
        <div className="model-config-toggle-row"><label className="model-config-check">
            <input type="checkbox" checked={enabled} onChange={e => void toggle(e.target.checked)} />
            {t('Enable OCR', '启用文字识别', '啟用文字識別')}
        </label></div>
        <p className="model-config-copy">{t(
            'PP-OCRv6 reads text from screenshots for computer use and GUI automation, fully on-device. It is enabled by default and downloads from HuggingFace or your Hub mirror.',
            'PP-OCRv6 在本地识别截图中的文字，用于电脑操作与界面自动化。默认启用，模型从 HuggingFace 或 Hub 镜像下载。',
            'PP-OCRv6 在本機識別截圖中的文字，用於電腦操作與介面自動化。預設啟用，模型從 HuggingFace 或 Hub 鏡像下載。'
        )}</p>
        {enabled && <ModelStatusBox exists={exists} downloading={downloading} size={size} progress={progress} downloaded={downloaded} total={total} error={error} onDownload={() => void download()} onRetry={() => void download()} accentColor="var(--theme-success)" t={t} />}
    </div>;
}
