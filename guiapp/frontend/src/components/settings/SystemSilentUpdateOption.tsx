import { corelib } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

type SystemSilentUpdateOptionProps = {
    config: corelib.AppConfig | null;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => void;
};

// Silent-update preference toggle. Silent mode downloads the installer in the
// background and then waits for the user's install-now/defer/next-startup
// choice; only the OS UAC prompt remains.
export const SystemSilentUpdateOption = ({ config, lang, saveRemoteConfigField }: SystemSilentUpdateOptionProps) => (
    <label className="system-settings-option">
        <input
            type="checkbox"
            aria-label={localizeText(lang, 'Silent updates', '静默更新', '靜默更新')}
            checked={config?.silent_update_enabled !== false}
            onChange={(e) => {
                saveRemoteConfigField({ silent_update_enabled: e.target.checked } as any);
            }}
        />
        <span>
            {localizeText(lang, 'Silent updates', '静默更新', '靜默更新')}
        </span>
        <small>
            {localizeText(lang, 'Automatically download updates in the background. You choose when to install; Windows may still show the system UAC prompt.', '后台自动下载新版本；下载完成后由你选择安装时间，安装时仅保留系统 UAC 确认。', '背景自動下載新版本；下載完成後由你選擇安裝時間，安裝時僅保留系統 UAC 確認。')}
        </small>
    </label>
);
