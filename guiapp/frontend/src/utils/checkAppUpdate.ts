import { CheckUpdate, CheckUpdateBeta } from '../../wailsjs/go/main/App';

/** Run the correct update check for the user's beta preference. */
export function checkAppUpdate(appVersion: string, preferBetaChannel?: boolean) {
    return preferBetaChannel ? CheckUpdateBeta(appVersion) : CheckUpdate(appVersion);
}
