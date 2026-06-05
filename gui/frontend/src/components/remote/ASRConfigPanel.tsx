import { useState, useEffect, useCallback, useRef } from 'react';
import { GetASREnabled, SetASREnabled, CheckASRModel, DownloadASRModel, LoadConfig, PatchConfigFields } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { ModelStatusBox } from "./ModelStatusBox";

type Props = { lang: string };

// ── Calibration constants ──
const SILENCE_PHASE_MS = 3000;     // Phase 1: 3 seconds of silence sampling
const SPEECH_PHASE_MS = 5000;      // Phase 2: 5 seconds of speech sampling
const CALIBRATION_CHUNK_SIZE = 4096;
const TARGET_SAMPLE_RATE = 16000;

// Sentences for the user to read aloud during speech calibration.
// Short, natural, covering different tones. Displayed one at a time.
const READING_SENTENCES: Record<string, string[]> = {
    en: [
        '"The quick brown fox jumps over the lazy dog."',
        '"She sells seashells by the seashore."',
    ],
    'zh-Hans': [
        '"今天天气真不错，适合出去走走。"',
        '"我想预约明天下午三点的会议室。"',
    ],
    'zh-Hant': [
        '"今天天氣真不錯，適合出去走走。"',
        '"我想預約明天下午三點的會議室。"',
    ],
};

type CalibrationPhase = 'idle' | 'silence' | 'speech' | 'done';

function rmsF32(data: Float32Array): number {
    let sum = 0;
    for (let i = 0; i < data.length; i++) sum += data[i] * data[i];
    return Math.sqrt(sum / data.length);
}

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

    // Calibration state
    const [calibPhase, setCalibPhase] = useState<CalibrationPhase>('idle');
    const [calibratedValue, setCalibratedValue] = useState<number>(0);
    const [speechLevelValue, setSpeechLevelValue] = useState<number>(0);
    const [calibrationMsg, setCalibrationMsg] = useState('');
    const [calibLevelBar, setCalibLevelBar] = useState(0); // real-time level during calibration (0-1)
    const calibrationCleanupRef = useRef<(() => void) | null>(null);

    useEffect(() => {
        (async () => {
            try {
                const info: any = await CheckASRModel();
                setModelExists(info.exists);
                setModelSize(info.size || 0);
                const on = await GetASREnabled();
                setEnabled(on);
                // Load existing calibration values
                const cfg = await LoadConfig();
                if (cfg.noise_floor_calibrated && cfg.noise_floor_calibrated > 0) {
                    setCalibratedValue(cfg.noise_floor_calibrated);
                }
                if (cfg.speech_level_calibrated && cfg.speech_level_calibrated > 0) {
                    setSpeechLevelValue(cfg.speech_level_calibrated);
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
            }
        };
        EventsOn('asr-download-progress', handler);
        return () => { EventsOff('asr-download-progress'); };
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
            await SetASREnabled(on);
        } catch (e: any) {
            setEnabled(!on);
            setDownloading(false);
            setError(e?.message || String(e));
            return;
        }
    };

    const startDownload = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadASRModel(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e)));
            setDownloading(false);
        }
    };

    // Cleanup calibration resources on unmount
    useEffect(() => () => { calibrationCleanupRef.current?.(); }, []);

    /**
     * Two-phase microphone calibration:
     *   Phase 1 (silence): 3s — measure ambient noise floor (minimum RMS)
     *   Phase 2 (speech):  5s — user reads sentences aloud, measure speech energy
     *   Result: noise floor + speech level → threshold = noise + 30% of gap
     */
    const startCalibration = useCallback(async () => {
        if (calibPhase !== 'idle') return;

        setCalibPhase('silence');
        setCalibrationMsg('');
        setCalibLevelBar(0);

        let stream: MediaStream | null = null;
        let ctx: AudioContext | null = null;
        let processor: ScriptProcessorNode | null = null;
        let source: MediaStreamAudioSourceNode | null = null;

        const cleanup = () => {
            processor?.disconnect();
            source?.disconnect();
            ctx?.close().catch(() => {});
            stream?.getTracks().forEach(tr => tr.stop());
            calibrationCleanupRef.current = null;
        };
        calibrationCleanupRef.current = cleanup;

        try {
            stream = await navigator.mediaDevices.getUserMedia({
                audio: { channelCount: 1, sampleRate: { ideal: TARGET_SAMPLE_RATE }, echoCancellation: true, noiseSuppression: true },
            });
            ctx = new AudioContext({ sampleRate: TARGET_SAMPLE_RATE });
            source = ctx.createMediaStreamSource(stream);
            processor = ctx.createScriptProcessor(CALIBRATION_CHUNK_SIZE, 1, 1);

            // Shared sample collector
            let rmsValues: number[] = [];
            processor.onaudioprocess = (e) => {
                const val = rmsF32(e.inputBuffer.getChannelData(0));
                rmsValues.push(val);
                setCalibLevelBar(Math.min(1, val * 12));
            };
            source.connect(processor);
            processor.connect(ctx.destination);

            // ── Phase 1: Silence ──
            await new Promise(resolve => setTimeout(resolve, SILENCE_PHASE_MS));

            const silenceRms = rmsValues.length > 0
                ? rmsValues.reduce((a, b) => a < b ? a : b)
                : 0.006;
            const noiseFloor = Math.min(0.08, Math.max(0.001, silenceRms));

            // ── Phase 2: Speech ──
            rmsValues = []; // reset for speech phase
            setCalibPhase('speech');

            await new Promise(resolve => setTimeout(resolve, SPEECH_PHASE_MS));

            cleanup();

            // Compute speech level: only use chunks clearly above noise floor.
            // This is mechanistically correct: phase 1 measured the noise, phase 2
            // only needs the "clearly above noise" portion. Using a fixed percentile
            // (top 60%) fails when the user speaks softly — pauses still dominate.
            const speechCutoff = noiseFloor * 1.5;
            let speechLevel = 0;
            if (rmsValues.length > 0) {
                const speechChunks = rmsValues.filter(v => v > speechCutoff);
                if (speechChunks.length >= 3) {
                    // Enough speech samples — compute average
                    speechLevel = speechChunks.reduce((a, b) => a + b, 0) / speechChunks.length;
                } else {
                    // User barely spoke or was too quiet — calibration unreliable
                    setCalibPhase('idle');
                    setCalibLevelBar(0);
                    setCalibrationMsg(t(
                        'Not enough speech detected. Please try again and read the sentences aloud.',
                        '未检测到足够的语音，请重试并朗读屏幕上的文字。',
                        '未檢測到足夠的語音，請重試並朗讀螢幕上的文字。',
                    ));
                    return;
                }
            }
            const clampedSpeech = Math.max(noiseFloor * 2, speechLevel); // at least 2× noise

            await PatchConfigFields({
                noise_floor_calibrated: noiseFloor,
                speech_level_calibrated: clampedSpeech,
            });

            setCalibratedValue(noiseFloor);
            setSpeechLevelValue(clampedSpeech);
            setCalibPhase('done');

            // Compute the effective threshold for display (same formula as useVoiceInput)
            const gap = clampedSpeech - noiseFloor;
            const threshold = noiseFloor + gap * 0.3;
            setCalibrationMsg(t(
                `Done! Noise: ${noiseFloor.toFixed(4)}, Speech: ${clampedSpeech.toFixed(4)}, Threshold: ${threshold.toFixed(4)}`,
                `校准完成！噪声：${noiseFloor.toFixed(4)}，语音：${clampedSpeech.toFixed(4)}，阈值：${threshold.toFixed(4)}`,
                `校準完成！噪聲：${noiseFloor.toFixed(4)}，語音：${clampedSpeech.toFixed(4)}，閾值：${threshold.toFixed(4)}`,
            ));

            // Reset phase after a moment so button becomes available again
            setTimeout(() => setCalibPhase('idle'), 100);
        } catch (err: any) {
            cleanup();
            setCalibPhase('idle');
            const msg = err?.message || String(err);
            if (msg.includes('Permission') || msg.includes('NotAllowed')) {
                setCalibrationMsg(t('Microphone access denied', '麦克风权限被拒绝', '麥克風權限被拒絕'));
            } else {
                setCalibrationMsg(t(`Calibration failed: ${msg}`, `校准失败：${msg}`, `校準失敗：${msg}`));
            }
        }
        setCalibLevelBar(0);
    }, [calibPhase, t]);

    /** Clear calibration and revert to auto-detection. */
    const clearCalibration = useCallback(async () => {
        try {
            await PatchConfigFields({
                noise_floor_calibrated: 0,
                speech_level_calibrated: 0,
            });
            setCalibratedValue(0);
            setSpeechLevelValue(0);
            setCalibrationMsg(t('Calibration cleared. Using auto-detection.', '已清除校准，使用自动检测。', '已清除校準，使用自動檢測。'));
        } catch {}
    }, [t]);

    if (loading) return <div className="model-config-loading">{t('Loading...', '加载中...', '加載中...')}</div>;

    const accentColor = 'var(--theme-success)';

    return (
        <div className="model-config-panel model-config-panel--spaced">
            <h4 className="model-config-heading model-config-heading--success">
                {t('Speech Recognition Model', '语音识别模型', '語音識別模型')}
            </h4>
            <div className="model-config-toggle-row">
                <label className="model-config-check">
                    <input type='checkbox' checked={enabled} onChange={e => handleToggle(e.target.checked)} />
                    {t('Enable Speech Recognition', '启用语音识别', '啟用語音識別')}
                </label>
            </div>
            <p className="model-config-copy">
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

            {/* Microphone two-phase calibration */}
            {enabled && modelExists && (
                <div className="model-config-calibration">
                    <div className="model-config-calibration__title">
                        {t('Microphone Calibration', '麦克风校准', '麥克風校準')}
                    </div>
                    <p className="model-config-calibration__copy">
                        {t(
                            'Calibrate your microphone for better speech detection. The process takes about 8 seconds: first measuring background noise, then your voice level.',
                            '校准麦克风以获得更好的语音检测效果。整个过程约 8 秒：先测量背景噪声，再测量您的语音音量。',
                            '校準麥克風以獲得更好的語音檢測效果。整個過程約 8 秒：先測量背景噪聲，再測量您的語音音量。',
                        )}
                    </p>

                    {/* Phase indicator & reading prompt */}
                    {calibPhase === 'silence' && (
                        <div className="model-config-phase-card">
                            <div className="model-config-phase-title model-config-phase-title--warning">
                                🤫 {t('Phase 1/2 — Measuring background noise...', '第 1 步（共 2 步）— 正在测量背景噪声...', '第 1 步（共 2 步）— 正在測量背景噪聲...')}
                            </div>
                            <div className="model-config-phase-copy">
                                {t('Please stay quiet for 3 seconds.', '请保持安静 3 秒。', '請保持安靜 3 秒。')}
                            </div>
                        </div>
                    )}
                    {calibPhase === 'speech' && (
                        <div className="model-config-phase-card">
                            <div className="model-config-phase-title model-config-phase-title--success">
                                🎤 {t('Phase 2/2 — Please read aloud:', '第 2 步（共 2 步）— 请用正常音量朗读：', '第 2 步（共 2 步）— 請用正常音量朗讀：')}
                            </div>
                            <div className="model-config-reading-list">
                                {(READING_SENTENCES[lang] || READING_SENTENCES['zh-Hans']).map((s, i) => (
                                    <div key={i}>{s}</div>
                                ))}
                            </div>
                        </div>
                    )}

                    {/* Real-time level bar during calibration */}
                    {(calibPhase === 'silence' || calibPhase === 'speech') && (
                        <div className="model-config-level-track">
                            <div style={{
                                width: `${Math.round(calibLevelBar * 100)}%`,
                            }} className={`model-config-level-fill ${calibPhase === 'speech' ? 'model-config-level-fill--success' : 'model-config-level-fill--warning'}`} />
                        </div>
                    )}

                    <div className="model-config-actions">
                        <button
                            onClick={startCalibration}
                            disabled={calibPhase !== 'idle'}
                            className="model-config-button model-config-button--accent"
                        >
                            {calibPhase !== 'idle'
                                ? t('Calibrating...', '校准中...', '校準中...')
                                : t('🎙️ Calibrate Microphone', '🎙️ 校准麦克风', '🎙️ 校準麥克風')}
                        </button>
                        {(calibratedValue > 0 || speechLevelValue > 0) && calibPhase === 'idle' && (
                            <button
                                onClick={clearCalibration}
                                className="model-config-button"
                            >
                                {t('Clear', '清除', '清除')}
                            </button>
                        )}
                    </div>
                    {calibrationMsg && (
                        <div className={`model-config-calibration__message ${calibratedValue > 0 ? 'is-success' : ''}`}>
                            {calibrationMsg}
                        </div>
                    )}
                    {calibratedValue > 0 && !calibrationMsg && (
                        <div className="model-config-calibration__message">
                            {t(
                                `Noise: ${calibratedValue.toFixed(4)}` + (speechLevelValue > 0 ? `, Speech: ${speechLevelValue.toFixed(4)}` : ''),
                                `噪声基线：${calibratedValue.toFixed(4)}` + (speechLevelValue > 0 ? `，语音音量：${speechLevelValue.toFixed(4)}` : ''),
                                `噪聲基線：${calibratedValue.toFixed(4)}` + (speechLevelValue > 0 ? `，語音音量：${speechLevelValue.toFixed(4)}` : ''),
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
