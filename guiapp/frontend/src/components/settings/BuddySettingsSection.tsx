import { useCallback, useEffect, useRef, useState } from 'react';
import {
    BuddyGetStatus,
    BuddyListPorts,
    BuddyRefreshWeather,
    BuddyStart,
    BuddyStop,
} from '../../../wailsjs/go/main/App';
import { localizeText } from '../../i18n';
import { startVisibleInterval } from '../../utils/visibleInterval';

type BuddyPort = {
    name: string;
    description?: string;
    vid?: string;
    pid?: string;
    serial_number?: string;
    is_buddy: boolean;
};

type BuddyStatus = {
    enabled: boolean;
    port?: string;
    connected: boolean;
    device_fw?: string;
    device_board?: string;
    device_touch?: boolean;
    page?: string;
    last_state_at?: string;
    last_ack_at?: string;
    missing_glyphs?: number;
    last_error?: string;
    snapshot_age_s?: number;
};

const POLL_MS = 3000;
const PORT_POLL_MS = 30000;

/**
 * USB companion display settings. The service lives in the Go process and
 * auto-detects a board, so this panel only shows state and lets the person
 * pick a port or disable the link before flashing.
 */
export function BuddySettingsSection({ lang }: { lang: string }) {
    const [status, setStatus] = useState<BuddyStatus | null>(null);
    const [ports, setPorts] = useState<BuddyPort[]>([]);
    const [selectedPort, setSelectedPort] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [weatherNote, setWeatherNote] = useState('');
    const statusRequest = useRef<Promise<void> | null>(null);
    const portsRequest = useRef<Promise<void> | null>(null);

    const refreshStatus = useCallback(() => {
        if (statusRequest.current) return statusRequest.current;
        const request = (async () => {
            try {
                const resolved = (await BuddyGetStatus()) as BuddyStatus;
                setStatus(resolved);
                setSelectedPort((current) => current || resolved.port || '');
                setError('');
            } catch (err) {
                setError(err instanceof Error ? err.message : String(err));
            } finally {
                statusRequest.current = null;
            }
        })();
        statusRequest.current = request;
        return request;
    }, []);

    const refreshPorts = useCallback(() => {
        if (portsRequest.current) return portsRequest.current;
        const request = (async () => {
            try {
                setPorts(((await BuddyListPorts()) as BuddyPort[]) || []);
                setError('');
            } catch (err) {
                setError(err instanceof Error ? err.message : String(err));
            } finally {
                portsRequest.current = null;
            }
        })();
        portsRequest.current = request;
        return request;
    }, []);

    const refresh = useCallback(async () => {
        await Promise.all([refreshStatus(), refreshPorts()]);
    }, [refreshPorts, refreshStatus]);

    useEffect(() => {
        void refresh();
        const stopStatus = startVisibleInterval(() => void refreshStatus(), POLL_MS);
        const stopPorts = startVisibleInterval(() => void refreshPorts(), PORT_POLL_MS);
        return () => {
            stopStatus();
            stopPorts();
        };
    }, [refresh, refreshPorts, refreshStatus]);

    const toggle = async (enabled: boolean) => {
        setBusy(true);
        setError('');
        try {
            if (enabled) {
                await BuddyStart(selectedPort);
            } else {
                await BuddyStop();
            }
            await refresh();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const changePort = async (port: string) => {
        setSelectedPort(port);
        if (!status?.enabled) return;
        setBusy(true);
        setError('');
        try {
            await BuddyStart(port);
            await refresh();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const refreshWeather = async () => {
        setBusy(true);
        setError('');
        setWeatherNote('');
        try {
            const location = await BuddyRefreshWeather();
            setWeatherNote(
                localizeText(lang, `Weather sent (${location})`, `天气已推送（${location}）`, `天氣已推送（${location}）`),
            );
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const stateText = status?.connected
        ? localizeText(lang, 'Connected', '已连接', '已連線')
        : localizeText(lang, 'Not connected', '未连接', '未連線');
    const staleNote =
        typeof status?.snapshot_age_s === 'number' && status.snapshot_age_s > 60
            ? ` · ${localizeText(lang, 'stale', '数据陈旧', '資料陳舊')}`
            : '';
    const touchNote =
        status?.connected && status.device_touch === false
            ? ` · ${localizeText(lang, 'touch not detected', '触摸未检测到', '觸控未偵測到')}`
            : '';

    return (
        <section className="general-settings-card general-settings-card--compact" aria-labelledby="buddy-settings-title">
            <div className="buddy-settings">
                <div>
                    <h3 id="buddy-settings-title" className="buddy-settings__title">
                        {localizeText(lang, 'USB companion display (Buddy)', 'USB 伴侣屏（Buddy）', 'USB 伴侶螢幕（Buddy）')}
                    </h3>
                    <p className="buddy-settings__hint">
                        {localizeText(
                            lang,
                            'Shows task counts on an ESP32-S3 board over USB; shows the pet and weather when nothing is running.',
                            '通过 USB 在 ESP32-S3 小屏上显示任务计数；没有进行中的任务时显示宠物与天气。',
                            '透過 USB 在 ESP32-S3 小螢幕上顯示任務計數；沒有進行中的任務時顯示寵物與天氣。',
                        )}
                    </p>
                </div>

                <label className="buddy-settings__row">
                    <input
                        type="checkbox"
                        checked={!!status?.enabled}
                        disabled={busy || status === null}
                        onChange={(event) => void toggle(event.target.checked)}
                    />
                    <span>{localizeText(lang, 'Enable companion link', '启用伴侣屏连接', '啟用伴侶螢幕連線')}</span>
                </label>

                <label className="buddy-settings__row">
                    <span>{localizeText(lang, 'Port', '端口', '連接埠')}</span>
                    <select
                        className="buddy-settings__select"
                        value={selectedPort}
                        disabled={busy || !status?.enabled}
                        onChange={(event) => void changePort(event.target.value)}
                    >
                        <option value="">{localizeText(lang, 'Auto-detect', '自动检测', '自動偵測')}</option>
                        {ports.map((port) => (
                            <option key={port.name} value={port.name}>
                                {port.name}
                                {port.description ? ` — ${port.description}` : ''}
                                {port.is_buddy ? ' ✓' : ''}
                            </option>
                        ))}
                    </select>
                    <button type="button" disabled={busy} onClick={() => void refresh()}>
                        {localizeText(lang, 'Refresh', '刷新', '重新整理')}
                    </button>
                </label>

                <div className="buddy-settings__status">
                    {stateText}
                    {status?.port ? ` · ${status.port}` : ''}
                    {status?.device_board ? ` · ${status.device_board}` : ''}
                    {status?.device_fw ? ` · ${status.device_fw}` : ''}
                    {status?.page ? ` · ${status.page}` : ''}
                    {touchNote}
                    {staleNote}
                </div>

                <div className="buddy-settings__row">
                    <button type="button" disabled={busy || !status?.enabled} onClick={() => void refreshWeather()}>
                        {localizeText(lang, 'Send weather now', '立即推送天气', '立即推送天氣')}
                    </button>
                    {weatherNote && <span className="buddy-settings__note">{weatherNote}</span>}
                </div>

                {typeof status?.missing_glyphs === 'number' && status.missing_glyphs > 0 && (
                    <div className="buddy-settings__warn">
                        {localizeText(
                            lang,
                            `${status.missing_glyphs} unsupported characters on the device`,
                            `设备端有 ${status.missing_glyphs} 个字缺字`,
                            `裝置端有 ${status.missing_glyphs} 個字缺字`,
                        )}
                    </div>
                )}

                {(error || status?.last_error) && (
                    <div className="buddy-settings__error">{error || status?.last_error}</div>
                )}

                <p className="buddy-settings__footnote">
                    {localizeText(
                        lang,
                        'Disable the link before flashing the board with esptool; the serial port is exclusive.',
                        '用 esptool 烧录前请先关闭连接：串口是独占的。',
                        '用 esptool 燒錄前請先關閉連線：序列埠是獨占的。',
                    )}
                </p>
            </div>
        </section>
    );
}
