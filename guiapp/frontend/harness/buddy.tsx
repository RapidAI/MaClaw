// Dev-only visual harness for BuddySettingsSection: stubs the Wails bridge
// with canned data and renders the settings hardware page standalone.
// Load via `npx vite` at /harness/buddy.html. Not part of the app build.
// Note: the page imports the assembled src/App.css, so after editing a
// partial run `node scripts/assemble-app-css.mjs` before checking styles.
import React from 'react';
import { createRoot } from 'react-dom/client';
import '../src/App.css';
import { BuddySettingsSection } from '../src/components/settings/BuddySettingsSection';

const status = {
    enabled: true,
    connected: true,
    port: 'COM3',
    device_board: 'esp32-s3',
    device_fw: 'v1.4.2',
    device_touch: false,
    device_key: true,
    device_sound: true,
    device_width: 240,
    device_height: 280,
    device_round: false,
    device_panel: 'st7789',
    device_font: 'misans',
    sound_master: true,
    state_latency_ms: 12,
    page: 'tasks',
    snapshot_age_s: 3,
};

const ports = [
    { name: 'COM3', description: 'USB 串行设备 (COM3)', is_buddy: true },
    { name: 'COM7', description: 'USB 串行设备 (COM7)', is_buddy: false },
];

const sound = {
    master: true,
    running: { enabled: true, effect: 0 },
    complete: { enabled: true, effect: 1 },
    standby: { enabled: false, effect: 2 },
    attention: { enabled: true, effect: 3 },
};

const delay = <T,>(value: T, ms = 120): Promise<T> => new Promise((resolve) => setTimeout(() => resolve(value), ms));

window.go = {
    main: {
        App: {
            BuddyGetStatus: () => delay(status),
            BuddyListPorts: () => delay(ports),
            BuddyGetSoundConfig: () => delay(sound),
            BuddySetSoundConfig: (next: unknown) => {
                Object.assign(sound, next);
                return delay(undefined);
            },
            BuddyStart: (port: string) => {
                status.enabled = true;
                status.connected = true;
                status.port = port || status.port;
                return delay(undefined);
            },
            BuddyStop: () => {
                status.enabled = false;
                status.connected = false;
                return delay(undefined);
            },
            BuddyRefreshWeather: () => delay('上海'),
            BuddyRefreshPet: () => delay(undefined),
        },
    },
} as typeof window.go;

function Harness() {
    return (
        <div
            className="settings-content settings-panel"
            style={{ maxWidth: 860, margin: '24px auto', padding: '0 20px' }}
        >
            <div className="settings-hardware-stack">
                <BuddySettingsSection lang="zh-Hans" />
            </div>
        </div>
    );
}

createRoot(document.getElementById('harness-root') as HTMLElement).render(<Harness />);
