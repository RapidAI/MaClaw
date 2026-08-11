import { useCallback, useEffect, useState } from 'react';
import { CheckOCRModel, DownloadOCRModel, GetOCREnabled, SetOCREnabled, SetOCRModelTier } from '../../../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { ModelStatusBox } from './ModelStatusBox';

type Props = { lang: string };

// OCRConfigPanel manages the local PP-OCRv6 detection/recognition models
// used for screen text extraction (computer use, GUI automation). It mirrors
// the ASR/Diarization panels: toggle + tier selector + download state driven
// by the 'ocr-download-progress' backend event.
export function OCRConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const [enabled, setEnabled] = useState(true);
    const [tier, setTier] = useState('small');
    const [exists, setExists] = useState(false);
    const [size, setSize] = useState(0);
    const [downloading, setDownloading] = useState(false);
    const [progress, setProgress] = useState(0);
    const [downloaded, setDownloaded] = useState(0);
    const [total, setTotal] = useState(0);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);

    const tierOptions = [
        { id: 'tiny', en: 'Tiny (~6MB, fastest)', zh: 'Tiny（~6MB，最快）', zhHant: 'Tiny（~6MB，最快）' },
        { id: 'small', en: 'Small (default, balanced, ~31MB)', zh: 'Small（默认，均衡 ~31MB）', zhHant: 'Small（預設，均衡 ~31MB）' },
        { id: 'medium', en: 'Medium (~139MB, most accurate)', zh: 'Medium（~139MB，最准）', zhHant: 'Medium（~139MB，最準）' },
    ];

    const refresh = useCallback(async () => {
        const info: any = await CheckOCRModel();
        setExists(!!info.exists);
        setSize(info.size || 0);
        if (info.tier) setTier(info.tier);
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
    const handleTierChange = async (nextTier: string) => {
        const previousTier = tier;
        setTier(nextTier); setError('');
        try {
            await SetOCRModelTier(nextTier);
            // The Go side kicks a background download when the new tier's
            // models are missing and OCR is enabled; reflect that state.
            const info: any = await CheckOCRModel();
            setExists(!!info.exists);
            setSize(info.size || 0);
            if (enabled && !info.exists) setDownloading(true);
        } catch (e: any) {
            setTier(previousTier); setError(e?.message || String(e));
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
        <div className="model-config-inline-field">
            <label htmlFor="ocr-tier-select" className="model-config-inline-label">
                {t('Model tier', '模型档位', '模型檔位')}
            </label>
            <select
                id="ocr-tier-select"
                value={tier}
                onChange={e => void handleTierChange(e.target.value)}
                disabled={downloading}
                className="model-config-select"
            >
                {tierOptions.map(option => (
                    <option key={option.id} value={option.id}>
                        {lang === 'en' ? option.en : lang === 'zh-Hant' ? option.zhHant : option.zh}
                    </option>
                ))}
            </select>
        </div>
        <p className="model-config-copy">{t(
            'PP-OCRv6 reads text from screenshots for computer use and GUI automation, fully on-device. When the main model supports image input, its own multimodal vision is used first and OCR serves as the fallback; models without vision rely on this engine. It is enabled by default and downloads from HuggingFace or your Hub mirror. Switching tiers downloads the new tier in the background when its models are missing.',
            'PP-OCRv6 在本地识别截图中的文字，用于电脑操作与界面自动化。当主模型支持图片输入时，优先使用模型自身的多模态视觉识别，OCR 作为兜底；不支持图片的模型则由本引擎识别。默认启用，模型从 HuggingFace 或 Hub 镜像下载。切换档位时，若新档位模型缺失会在后台自动下载。',
            'PP-OCRv6 在本機識別截圖中的文字，用於電腦操作與介面自動化。當主模型支援圖片輸入時，優先使用模型自身的多模態視覺識別，OCR 作為兜底；不支援圖片的模型則由本引擎識別。預設啟用，模型從 HuggingFace 或 Hub 鏡像下載。切換檔位時，若新檔位模型缺失會在背景自動下載。'
        )}</p>
        {enabled && <ModelStatusBox exists={exists} downloading={downloading} size={size} progress={progress} downloaded={downloaded} total={total} error={error} onDownload={() => void download()} onRetry={() => void download()} accentColor="var(--theme-success)" t={t} />}
    </div>;
}
