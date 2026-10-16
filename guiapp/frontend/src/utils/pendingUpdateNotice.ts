import { DismissPendingUpdate, GetPendingUpdateNotice, ShowItemInFolder } from '../../wailsjs/go/main/App';

export type PendingUpdateDialogSpec = {
    show: boolean;
    title: string;
    message: string;
    confirmText?: string;
    onConfirm: () => void;
    onCancel?: () => void;
};

type BuildPendingUpdateDialogOptions = {
    translate: (key: string) => string;
    callBackend: <T>(operation: () => Promise<T>) => Promise<T>;
    closeDialog: () => void;
};

// A silent install has no channel to report failure: the app quits before the
// installer runs, and the installer shows nothing. This asks the backend
// whether the last launch ever landed and, when it did not, returns a dialog
// spec so the next start can say so instead of quietly running the old
// version as if nothing had happened.
export async function buildPendingUpdateDialog(
    options: BuildPendingUpdateDialogOptions,
): Promise<PendingUpdateDialogSpec | null> {
    const { translate, callBackend, closeDialog } = options;

    let notice: any = null;
    try {
        notice = await callBackend(() => GetPendingUpdateNotice());
    } catch {
        return null;
    }
    const targetVersion = String(notice?.target_version || '').trim();
    if (!targetVersion) return null;

    const leftoverInstaller = String(notice?.installer_path || '');
    const revealInstaller = Boolean(notice?.installer_still_present) && leftoverInstaller !== '';
    const close = () => {
        closeDialog();
        void callBackend(() => DismissPendingUpdate());
    };

    return {
        show: true,
        title: translate('updateIncompleteTitle'),
        message: translate('updateIncompleteMessage').replace('{version}', targetVersion),
        confirmText: revealInstaller ? translate('updateIncompleteShowInstaller') : undefined,
        onConfirm: () => {
            close();
            if (revealInstaller) {
                void callBackend(() => ShowItemInFolder(leftoverInstaller));
            }
        },
        onCancel: close,
    };
}
