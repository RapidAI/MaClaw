export type InstallSkillText = (en: string, zhHans: string, zhHant: string) => string;

export const currentInstallSkillLang = (lang?: string) => (lang || document.documentElement.lang || 'zh-Hans').trim().toLowerCase();

export const isEnglishInstallSkillLang = (lang?: string) => {
    const normalized = currentInstallSkillLang(lang);
    return normalized === 'en' || normalized.startsWith('en-');
};

export const isTraditionalInstallSkillLang = (lang?: string) => {
    const normalized = currentInstallSkillLang(lang);
    return normalized.startsWith('zh-hant') || normalized.startsWith('zh-tw') || normalized.startsWith('zh-hk');
};

export const installSkillText = (lang: string | undefined, en: string, zhHans: string, zhHant: string): string => {
    if (isEnglishInstallSkillLang(lang)) return en;
    return isTraditionalInstallSkillLang(lang) ? zhHant : zhHans;
};

export const installSkillTextForCurrentLang = (en: string, zhHans: string, zhHant: string): string =>
    installSkillText(undefined, en, zhHans, zhHant);

export const formatInstallCount = (template: string, successCount: number, failCount: number) =>
    template.replace('{success}', String(successCount)).replace('{failed}', String(failCount));

export const localizeSkillInstallRiskLevel = (level: string | undefined, lang?: string) => {
    const normalized = (level || '').trim().toLowerCase();
    switch (normalized) {
        case 'critical': return installSkillText(lang, 'critical', '严重', '嚴重');
        case 'high': return installSkillText(lang, 'high', '高', '高');
        case 'medium': return installSkillText(lang, 'medium', '中', '中');
        case 'low': return installSkillText(lang, 'low', '低', '低');
        default: return level || '';
    }
};

export const localizeSkillInstallStatus = (status: string | undefined, summary: string | undefined, phase: string | undefined, lang?: string) => {
    const value = (status || summary || '').trim();
    const normalized = value.toLowerCase();
    const exact: Record<string, string> = {
        'preparing selected skills for installation.': installSkillText(lang, 'Preparing selected skills for installation.', '正在准备安装所选 Skill。', '正在準備安裝所選 Skill。'),
        'queued for install.': installSkillText(lang, 'Queued for install.', '已加入安装队列。', '已加入安裝佇列。'),
        'installing approved skill package.': installSkillText(lang, 'Installing approved skill package.', '正在安装已批准的 Skill 包。', '正在安裝已核准的 Skill 套件。'),
        'skill installed successfully.': installSkillText(lang, 'Skill installed successfully.', 'Skill 安装成功。', 'Skill 安裝成功。'),
        'starting pre-install security scan.': installSkillText(lang, 'Starting pre-install security scan.', '正在启动安装前安全扫描。', '正在啟動安裝前安全掃描。'),
        'high risk found. waiting for your allow or reject decision.': installSkillText(lang, 'High risk found. Waiting for your allow or reject decision.', '发现高风险，正在等待你允许或拒绝。', '發現高風險，正在等待你允許或拒絕。'),
        'critical risk found. waiting for your allow or reject decision.': installSkillText(lang, 'Critical risk found. Waiting for your allow or reject decision.', '发现严重风险，正在等待你允许或拒绝。', '發現嚴重風險，正在等待你允許或拒絕。'),
        'extracted skill package for pre-install security scan.': installSkillText(lang, 'Extracted skill package for pre-install security scan.', '已解压 Skill 包，准备进行安装前安全扫描。', '已解壓縮 Skill 套件，準備進行安裝前安全掃描。'),
        'starting managed capability skill security scan.': installSkillText(lang, 'Starting managed capability skill security scan.', '正在启动托管能力 Skill 安全扫描。', '正在啟動託管能力 Skill 安全掃描。'),
        'managed capability skill blocked by pre-install security scan.': installSkillText(lang, 'Managed capability skill blocked by pre-install security scan.', '托管能力 Skill 已被安装前安全扫描阻止。', '託管能力 Skill 已被安裝前安全掃描封鎖。'),
        'managed capability skill installed successfully.': installSkillText(lang, 'Managed capability skill installed successfully.', '托管能力 Skill 安装成功。', '託管能力 Skill 安裝成功。'),
        'managed capability skill scan did not produce a report; current policy allows installation.': installSkillText(lang, 'Managed capability skill scan did not produce a report; current policy allows installation.', '托管能力 Skill 扫描未生成报告；当前策略允许安装。', '託管能力 Skill 掃描未產生報告；目前策略允許安裝。'),
        'managed capability skill scan recorded risk and allowed installation by current policy.': installSkillText(lang, 'Managed capability skill scan recorded risk and allowed installation by current policy.', '托管能力 Skill 扫描已记录风险，当前策略允许安装。', '託管能力 Skill 掃描已記錄風險，目前策略允許安裝。'),
        'crafted script blocked before execution by security scan.': installSkillText(lang, 'Crafted script blocked before execution by security scan.', '生成脚本已在执行前被安全扫描阻止。', '生成腳本已在執行前被安全掃描封鎖。'),
        'security scanning generated script before execution.': installSkillText(lang, 'Security scanning generated script before execution.', '正在执行前安全扫描生成脚本。', '正在執行前安全掃描生成腳本。'),
        'generated script security scan did not produce a report; current policy allows execution.': installSkillText(lang, 'Generated script security scan did not produce a report; current policy allows execution.', '生成脚本安全扫描未生成报告；当前策略允许执行。', '生成腳本安全掃描未產生報告；目前策略允許執行。'),
        'developer mode enabled; generated script scan will not block execution.': installSkillText(lang, 'Developer mode enabled; generated script scan will not block execution.', '开发者模式已启用；生成脚本扫描不会阻止执行。', '開發者模式已啟用；生成腳本掃描不會封鎖執行。'),
        'generated script security scan passed.': installSkillText(lang, 'Generated script security scan passed.', '生成脚本安全扫描已通过。', '生成腳本安全掃描已通過。'),
        'generated script security scan recorded risk and allowed execution by current policy.': installSkillText(lang, 'Generated script security scan recorded risk and allowed execution by current policy.', '生成脚本安全扫描已记录风险，当前策略允许执行。', '生成腳本安全掃描已記錄風險，目前策略允許執行。'),
        'security scan did not produce a report; current policy allows installation.': installSkillText(lang, 'Security scan did not produce a report; current policy allows installation.', '安全扫描未生成报告；当前策略允许安装。', '安全掃描未產生報告；目前策略允許安裝。'),
        'security scan did not produce a report. installation blocked by policy.': installSkillText(lang, 'Security scan did not produce a report. Installation blocked by policy.', '安全扫描未生成报告。安装已被策略阻止。', '安全掃描未產生報告。安裝已被策略封鎖。'),
        'security scan passed.': installSkillText(lang, 'Security scan passed.', '安全扫描已通过。', '安全掃描已通過。'),
        'installation rejected.': installSkillText(lang, 'Installation rejected.', '安装已拒绝。', '安裝已拒絕。'),
        'user approved high-risk installation.': installSkillText(lang, 'User approved high-risk installation.', '用户已批准高风险安装。', '使用者已核准高風險安裝。'),
        'security scan recorded risk and allowed installation by current policy.': installSkillText(lang, 'Security scan recorded risk and allowed installation by current policy.', '安全扫描已记录风险，当前策略允许安装。', '安全掃描已記錄風險，目前策略允許安裝。'),
        'developer mode enabled; security scan will not block installation.': installSkillText(lang, 'Developer mode enabled; security scan will not block installation.', '开发者模式已启用；安全扫描不会阻止安装。', '開發者模式已啟用；安全掃描不會封鎖安裝。'),
        'developer mode enabled; high-risk scan result allowed.': installSkillText(lang, 'Developer mode enabled; high-risk scan result allowed.', '开发者模式已启用；高风险扫描结果已允许。', '開發者模式已啟用；高風險掃描結果已允許。'),
    };
    if (exact[normalized]) return exact[normalized];
    if (normalized.startsWith('security review required:')) {
        const rest = value.slice('Security review required:'.length).trim();
        return installSkillText(lang, 'Security review required: ' + rest, '需要安全审查：' + rest, '需要安全審查：' + rest);
    }
    if (value) return value;
    switch (phase) {
        case 'queued': return installSkillText(lang, 'Queued...', '已加入队列...', '已加入佇列...');
        case 'scan-start': return installSkillText(lang, 'Starting security scan...', '正在启动安全扫描...', '正在啟動安全掃描...');
        case 'extract': return installSkillText(lang, 'Extracting package...', '正在解压包...', '正在解壓縮套件...');
        case 'scanning': return installSkillText(lang, 'Scanning before install...', '安装前扫描中...', '安裝前掃描中...');
        case 'awaiting-confirmation': return installSkillText(lang, 'Awaiting confirmation...', '正在等待确认...', '正在等待確認...');
        case 'approved': return installSkillText(lang, 'Approved.', '已批准。', '已核准。');
        case 'installing': return installSkillText(lang, 'Installing...', '正在安装...', '正在安裝...');
        case 'done': return installSkillText(lang, 'Done.', '已完成。', '已完成。');
        case 'blocked': return installSkillText(lang, 'Blocked.', '已阻止。', '已封鎖。');
        case 'rejected': return installSkillText(lang, 'Rejected.', '已拒绝。', '已拒絕。');
        default: return installSkillText(lang, 'Working...', '处理中...', '處理中...');
    }
};
