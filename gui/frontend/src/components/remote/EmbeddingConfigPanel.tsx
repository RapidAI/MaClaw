import { useState, useEffect, useCallback } from 'react';
import {
    GetVectorSearchEnabled, SetVectorSearchEnabled, CheckEmbeddingModel, DownloadEmbeddingModel,
    GetScreenParsingEnabled, SetScreenParsingEnabled, CheckYOLOModel, DownloadYOLOModel,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { ModelStatusBox } from "./ModelStatusBox";

// --- Main panel ---

type Props = { lang: string };

export function EmbeddingConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

    // --- Embedding model state ---
    const [embEnabled, setEmbEnabled] = useState(false);
    const [embModelExists, setEmbModelExists] = useState(false);
    const [embModelSize, setEmbModelSize] = useState(0);
    const [embDownloading, setEmbDownloading] = useState(false);
    const [embProgress, setEmbProgress] = useState(0);
    const [embDownloaded, setEmbDownloaded] = useState(0);
    const [embTotal, setEmbTotal] = useState(0);
    const [embError, setEmbError] = useState('');

    // --- OminiParser-V2 (screen parsing) state ---
    const [spEnabled, setSpEnabled] = useState(true); // default on
    const [spModelExists, setSpModelExists] = useState(false);
    const [spModelSize, setSpModelSize] = useState(0);
    const [spDownloading, setSpDownloading] = useState(false);
    const [spProgress, setSpProgress] = useState(0);
    const [spDownloaded, setSpDownloaded] = useState(0);
    const [spTotal, setSpTotal] = useState(0);
    const [spError, setSpError] = useState('');

    const [loading, setLoading] = useState(true);

    // --- Init: load both model states ---
    useEffect(() => {
        (async () => {
            try {
                const embOn = await GetVectorSearchEnabled();
                setEmbEnabled(embOn);
                const embInfo: any = await CheckEmbeddingModel();
                setEmbModelExists(embInfo.exists);
                setEmbModelSize(embInfo.size || 0);
            } catch {}
            try {
                const spOn = await GetScreenParsingEnabled();
                setSpEnabled(spOn);
                const spInfo: any = await CheckYOLOModel();
                setSpModelExists(spInfo.exists);
                setSpModelSize(spInfo.size || 0);
            } catch {}
            setLoading(false);
        })();
    }, []);

    // --- Embedding download progress ---
    useEffect(() => {
        EventsOn('embedding-download-progress', (data: any) => {
            if (data.error) { setEmbError(data.error); setEmbDownloading(false); return; }
            const pct = data.percent || 0;
            setEmbProgress(pct);
            setEmbDownloaded(data.downloaded || 0);
            setEmbTotal(data.total || 0);
            if (pct > 0 && pct < 100) { setEmbDownloading(true); }
            if (pct >= 100) {
                setEmbDownloading(false);
                setEmbModelExists(true);
                setEmbModelSize(data.downloaded || 0);
            }
        });
        return () => { EventsOff('embedding-download-progress'); };
    }, []);

    // --- YOLO download progress ---
    useEffect(() => {
        EventsOn('yolo-download-progress', (data: any) => {
            if (data.error) { setSpError(data.error); setSpDownloading(false); return; }
            const pct = data.percent || 0;
            setSpProgress(pct);
            setSpDownloaded(data.downloaded || 0);
            setSpTotal(data.total || 0);
            if (pct > 0 && pct < 100) { setSpDownloading(true); }
            if (pct >= 100) {
                setSpDownloading(false);
                setSpModelExists(true);
                setSpModelSize(data.downloaded || 0);
            }
        });
        return () => { EventsOff('yolo-download-progress'); };
    }, []);

    // --- Handlers ---
    const handleEmbToggle = async (on: boolean) => {
        setEmbEnabled(on);
        setEmbError('');
        try { await SetVectorSearchEnabled(on); } catch (e: any) { setEmbError(e?.message || String(e)); return; }
        if (on && !embModelExists && !embDownloading) { startEmbDownload(); }
    };

    const startEmbDownload = async () => {
        setEmbDownloading(true); setEmbProgress(0); setEmbDownloaded(0); setEmbTotal(0); setEmbError('');
        try {
            await DownloadEmbeddingModel();
        } catch (e: any) {
            setEmbError(prev => prev || (e?.message || String(e)));
            setEmbDownloading(false);
        }
    };

    const handleSpToggle = async (on: boolean) => {
        setSpEnabled(on);
        setSpError('');
        try { await SetScreenParsingEnabled(on); } catch (e: any) { setSpError(e?.message || String(e)); return; }
        if (on && !spModelExists && !spDownloading) { startSpDownload(); }
    };

    const startSpDownload = async () => {
        setSpDownloading(true); setSpProgress(0); setSpDownloaded(0); setSpTotal(0); setSpError('');
        try {
            await DownloadYOLOModel();
        } catch (e: any) {
            setSpError(prev => prev || (e?.message || String(e)));
            setSpDownloading(false);
        }
    };

    if (loading) return <div style={{ padding: 20, color: colors.textMuted }}>{t('Loading...', '加载中...', '加載中...')}</div>;

    return (
        <div style={{ padding: '0 2px' }}>
            {/* ===== Section 1: OminiParser-V2 (Screen Parsing) ===== */}
            <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-warning, #e6a23c)', marginBottom: 12, marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                {t('OminiParser-V2 (Screen Parsing)', 'OminiParser-V2（屏幕解析）', 'OminiParser-V2（螢幕解析）')}
            </h4>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16 }}>
                <label style={{ fontSize: '0.82rem', color: colors.text, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
                    <input type='checkbox' checked={spEnabled} onChange={e => handleSpToggle(e.target.checked)} disabled={spDownloading} style={{ width: 16, height: 16, cursor: 'pointer' }} />
                    {t('Enable Screen Parsing', '启用屏幕解析', '啟用螢幕解析')}
                </label>
            </div>
            <p style={{ fontSize: '0.76rem', color: colors.textSecondary, margin: '0 0 16px 0', lineHeight: 1.5 }}>
                {t(
                    'OminiParser-V2 uses a YOLO-based model to detect and parse UI elements on screen, enabling vision-based interaction. Model file ~77MB, downloaded from Hub.',
                    'OminiParser-V2 使用 YOLO 模型检测和解析屏幕上的 UI 元素，实现基于视觉的界面交互。模型文件约 77MB，将从 Hub 下载到本地。',
                    'OminiParser-V2 使用 YOLO 模型檢測和解析螢幕上的 UI 元素，實現基於視覺的界面交互。模型文件約 77MB，將從 Hub 下載到本地。'
                )}
            </p>
            {spEnabled && (
                <ModelStatusBox
                    exists={spModelExists} downloading={spDownloading} size={spModelSize}
                    progress={spProgress} downloaded={spDownloaded} total={spTotal}
                    error={spError} onDownload={startSpDownload} onRetry={startSpDownload}
                    accentColor="var(--theme-warning, #e6a23c)" t={t}
                />
            )}

            {/* ===== Section 2: Embedding Model (Vector Search) ===== */}
            <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: 12, marginTop: 24, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                {t('Embedding Model (Vector Search)', '嵌入模型（向量搜索）', '嵌入模型（向量搜索）')}
            </h4>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16 }}>
                <label style={{ fontSize: '0.82rem', color: colors.text, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
                    <input type='checkbox' checked={embEnabled} onChange={e => handleEmbToggle(e.target.checked)} disabled={embDownloading} style={{ width: 16, height: 16, cursor: 'pointer' }} />
                    {t('Enable Vector Search', '启用向量搜索', '啟用向量搜索')}
                </label>
            </div>
            <p style={{ fontSize: '0.76rem', color: colors.textSecondary, margin: '0 0 16px 0', lineHeight: 1.5 }}>
                {t(
                    'Vector search uses EmbeddingGemma 300M model to generate semantic vectors for memory and documents, improving search accuracy. Model file ~300MB, downloaded from Hub.',
                    '向量搜索使用 EmbeddingGemma 300M 模型为记忆和文档生成语义向量，提升搜索精度。模型文件约 300MB，将从 Hub 下载到本地。',
                    '向量搜索使用 EmbeddingGemma 300M 模型為記憶和文檔生成語義向量，提升搜索精度。模型文件約 300MB，將從 Hub 下載到本地。'
                )}
            </p>
            {embEnabled && (
                <ModelStatusBox
                    exists={embModelExists} downloading={embDownloading} size={embModelSize}
                    progress={embProgress} downloaded={embDownloaded} total={embTotal}
                    error={embError} onDownload={startEmbDownload} onRetry={startEmbDownload}
                    accentColor="var(--theme-primary)" t={t}
                />
            )}
        </div>
    );
}
