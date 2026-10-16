import { UnregisterFileCompanionLinuxDesktop } from '../../../wailsjs/go/main/App';

function isLinuxFileCompanionHost(): boolean {
    if (typeof navigator === 'undefined') return false;
    const platform = navigator.platform || '';
    const agent = navigator.userAgent || '';
    if (/win/i.test(platform)) return false;
    return /linux/i.test(platform) || (/linux/i.test(agent) && !/win/i.test(agent));
}

export function LinuxFileCompanionSettings() {
    if (!isLinuxFileCompanionHost()) return null;
    return (
        <section className="general-settings-card">
            <button
                type="button"
                className="btn btn-sm"
                data-testid="remove-file-companion-menu"
                onClick={() => { void UnregisterFileCompanionLinuxDesktop(); }}
            >
                移除右键打开项
            </button>
        </section>
    );
}
