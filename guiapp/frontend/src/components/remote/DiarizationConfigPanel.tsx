import { useCallback, useEffect, useState } from 'react';
import { CheckDiarizationModel, DownloadDiarizationModel, GetDiarizationEnabled, SetDiarizationEnabled } from '../../../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { ModelStatusBox } from './ModelStatusBox';

type Props = { lang: string };

// DiarizationConfigPanel manages the optional CAM++ speaker-embedding model
// used to label each meeting segment before it is sent to SenseVoice ASR.
export function DiarizationConfigPanel({ lang }: Props) {
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
        const info: any = await CheckDiarizationModel();
        setExists(!!info.exists);
        setSize(info.size || 0);
    }, []);

    useEffect(() => {
        Promise.all([refresh(), GetDiarizationEnabled().then(setEnabled)]).catch(() => {}).finally(() => setLoading(false));
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
        EventsOn('diarization-download-progress', handler);
        return () => { EventsOff('diarization-download-progress'); };
    }, [refresh]);

    const toggle = async (next: boolean) => {
        setEnabled(next); setError('');
        if (next && !exists) setDownloading(true);
        if (!next) setDownloading(false);
        try { await SetDiarizationEnabled(next); } catch (e: any) {
            setEnabled(!next); setDownloading(false); setError(e?.message || String(e));
        }
    };
    const download = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadDiarizationModel(); await refresh(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e))); setDownloading(false);
        }
    };

    if (loading) return <div className="model-config-loading">{t('Loading...', '加载中...', '加載中...')}</div>;
    return <div className="model-config-panel model-config-panel--spaced">
        <h4 className="model-config-heading model-config-heading--success">{t('Speaker Diarization Model', '说话人分离模型', '說話人分離模型')}</h4>
        <div className="model-config-toggle-row"><label className="model-config-check">
            <input type="checkbox" checked={enabled} onChange={e => void toggle(e.target.checked)} />
            {t('Enable Speaker Diarization', '启用说话人分离', '啟用說話人分離')}
        </label></div>
        <p className="model-config-copy">{t(
            'CAM++ separates and labels local speakers in a meeting recording before each segment is transcribed. It is enabled by default and downloads from GitHub or your Hub cache.',
            'CAM++ 会先为会议录音分离并标记本地说话人，再分别转写。默认启用，模型从 GitHub 或 Hub 缓存下载。',
            'CAM++ 會先為會議錄音分離並標記本地說話人，再分別轉寫。預設啟用，模型從 GitHub 或 Hub 快取下載。'
        )}</p>
        {enabled && <ModelStatusBox exists={exists} downloading={downloading} size={size} progress={progress} downloaded={downloaded} total={total} error={error} onDownload={() => void download()} onRetry={() => void download()} accentColor="var(--theme-success)" t={t} />}
    </div>;
}
