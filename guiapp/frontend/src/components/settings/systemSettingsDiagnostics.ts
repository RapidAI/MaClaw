import { corelib } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

const textForLang = localizeText;

export const emptySystemValue = (lang: string, kind: 'inactive' | 'unset' | 'generated') => {
    if (kind === 'generated') return textForLang(lang, '(not generated)', '(\u672a\u751f\u6210)', '(\u672a\u751f\u6210)');
    if (kind === 'unset') return textForLang(lang, '(not set)', '(\u672a\u8bbe\u7f6e)', '(\u672a\u8a2d\u7f6e)');
    return textForLang(lang, '(not activated)', '(\u672a\u6fc0\u6d3b)', '(\u672a\u555f\u7528)');
};

export const buildSystemDiagnostics = (config: corelib.AppConfig | null, lang: string): Array<[string, string]> => [
    ['Machine ID', config?.remote_machine_id || emptySystemValue(lang, 'inactive')],
    ['User ID', config?.remote_user_id || emptySystemValue(lang, 'inactive')],
    ['Client ID', config?.remote_client_id || emptySystemValue(lang, 'generated')],
    ['SN', config?.remote_sn || emptySystemValue(lang, 'inactive')],
    ['Hub URL', config?.remote_hub_url || emptySystemValue(lang, 'unset')],
    [textForLang(lang, 'Account', '\u8d26\u6237', '\u5e33\u6236'), config?.remote_email || emptySystemValue(lang, 'unset')],
    ['WeChat Mode', (config as any)?.weixin_local_mode === false ? textForLang(lang, 'Multi-device (Hub)', '\u591a\u673a (Hub)', '\u591a\u6a5f (Hub)') : textForLang(lang, 'Single-device (Local)', '\u5355\u673a (Local)', '\u55ae\u6a5f (Local)')],
];
