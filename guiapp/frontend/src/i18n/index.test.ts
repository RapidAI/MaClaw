import { describe, expect, it, vi } from 'vitest';
import { isChineseLang, isTraditionalLang, localizeText, normalizeLang, t } from './index';
import { chromeText } from './chromeText';
import { appsMn, appsOneClickErrorText, appsPreflightCheckText, appsPublishQueueChannelLabel, appsPublishQueueEventStatusLabel, appsPublishQueueMessage, appsPublishQueueStatusLabel, appsReviewIssueText } from './mnAppsLabels';
import { tailText } from './mnTail';

describe('i18n/index', () => {
    describe('normalizeLang', () => {
        it('maps common language codes', () => {
            expect(normalizeLang('en')).toBe('en');
            expect(normalizeLang('en-US')).toBe('en');
            expect(normalizeLang('en-GB')).toBe('en');
            expect(normalizeLang('zh-Hans')).toBe('zh-Hans');
            expect(normalizeLang('zh-CN')).toBe('zh-Hans');
            expect(normalizeLang('zh')).toBe('zh-Hans');
            expect(normalizeLang('zh-Hant')).toBe('zh-Hant');
            expect(normalizeLang('zh-TW')).toBe('zh-Hant');
            expect(normalizeLang('zh-HK')).toBe('zh-Hant');
            expect(normalizeLang('zh-MO')).toBe('zh-Hant');
            expect(normalizeLang('mn')).toBe('mn');
            expect(normalizeLang('mn-MN')).toBe('mn');
            expect(normalizeLang('mn-Cyrl-MN')).toBe('mn');
        });

        it('handles empty/null/undefined as English', () => {
            expect(normalizeLang('')).toBe('en');
            expect(normalizeLang(null)).toBe('en');
            expect(normalizeLang(undefined)).toBe('en');
        });

        it('uses English for anything except Simplified, Traditional, and Cyrillic Mongolian', () => {
            expect(normalizeLang('ja')).toBe('en');
            expect(normalizeLang('fr')).toBe('en');
            expect(normalizeLang('garbage')).toBe('en');
            expect(normalizeLang('mn-Mong')).toBe('en');
            expect(normalizeLang('mn-Mong-CN')).toBe('en');
        });

        it('is case-insensitive', () => {
            expect(normalizeLang('EN')).toBe('en');
            expect(normalizeLang('ZH-HANS')).toBe('zh-Hans');
            expect(normalizeLang('ZH-TW')).toBe('zh-Hant');
        });
    });

    describe('isChineseLang', () => {
        it('is true only for simplified and traditional tags', () => {
            expect(isChineseLang('zh-Hans')).toBe(true);
            expect(isChineseLang('zh-CN')).toBe(true);
            expect(isChineseLang('zh-SG')).toBe(true);
            expect(isChineseLang('chinese')).toBe(true);
            expect(isChineseLang('ZH-CN')).toBe(true);
            expect(isChineseLang('zh_CN')).toBe(true);
            expect(isChineseLang('zh-Hant')).toBe(true);
            expect(isChineseLang('zh-TW')).toBe(true);
            expect(isChineseLang('zh-HK')).toBe(true);
            expect(isChineseLang('mn')).toBe(false);
            expect(isChineseLang('mn-Mong')).toBe(false);
            expect(isChineseLang('ja')).toBe(false);
            expect(isChineseLang('')).toBe(false);
            expect(isChineseLang(undefined)).toBe(false);
            expect(isChineseLang('zh-yue')).toBe(false);
        });
    });

    describe('isTraditionalLang', () => {
        it('is true only for traditional tags', () => {
            expect(isTraditionalLang('zh-Hant')).toBe(true);
            expect(isTraditionalLang('zh-TW')).toBe(true);
            expect(isTraditionalLang('zh-HK')).toBe(true);
            expect(isTraditionalLang('zh-MO')).toBe(true);
            expect(isTraditionalLang('zh-Hans')).toBe(false);
            expect(isTraditionalLang('zh-CN')).toBe(false);
            expect(isTraditionalLang('zh-SG')).toBe(false);
            expect(isTraditionalLang('mn')).toBe(false);
            expect(isTraditionalLang(undefined)).toBe(false);
        });
    });

    describe('localizeText', () => {
        it('selects correct text by language', () => {
            expect(localizeText('en', 'Hello', '你好', '你好')).toBe('Hello');
            expect(localizeText('zh-Hans', 'Hello', '你好', '你好')).toBe('你好');
            expect(localizeText('zh-Hant', 'Hello', '你好', '您好')).toBe('您好');
        });

        it('falls back zhHant to zhHans when zhHant not provided', () => {
            expect(localizeText('zh-TW', 'Hello', '你好')).toBe('你好');
        });

        it('handles undefined lang as English', () => {
            expect(localizeText(undefined, 'Hello', '你好')).toBe('Hello');
            expect(localizeText(null, 'Hello', '你好')).toBe('Hello');
        });

        it('uses Mongolian inline copy and otherwise English', () => {
            expect(localizeText('mn', 'Close', '关闭', '關閉')).toBe('Хаах');
            expect(localizeText('mn', 'Not in the overlay', '关闭')).toBe('Not in the overlay');
            expect(localizeText('mn', 'Keep enabled during rollout so an OfficeRead failure can use the existing reader.', '回退')).toBe('Шилжилтийн үед асаалттай үлдээнэ. OfficeRead амжилтгүй бол одоогийн уншигчийг ашиглана.');
            expect(tailText('mn', 'Keep coding', '保持编程')).toBe('Програмчлалыг хэвээр үлдээх');
            expect(tailText('mn', 'Keep chat', '保持会话')).toBe('Чатыг хэвээр үлдээх');
            expect(localizeText('mn', 'Connect chat, email and hardware', '连接')).toBe('Чат, имэйл, техникийг холбох');
            expect(localizeText('mn', 'Evolution', '进化')).toBe('Хувьсал');
            expect(localizeText('mn', 'Repair now', '立即修复')).toBe('Одоо засах');
            expect(localizeText('mn', 'Optimize now', '立即优化')).toBe('Одоо оновчлох');
            expect(chromeText('mn', 'Experts', '专家', '專家')).toBe('Эксперт');
            expect(chromeText('en', 'Experts', '专家', '專家')).toBe('Experts');
            expect(chromeText('zh-Hans', 'Experts', '专家', '專家')).toBe('专家');
            expect(chromeText('mn', 'Creating project session', '正在创建项目会话')).toBe('Төслийн сесс үүсгэж байна');
            expect(chromeText('mn', 'Session working directory: ', '会话工作目录：')).toBe('Сессийн ажлын хавтас: ');
            expect(chromeText('mn', 'Open containing folder', '打开所在目录')).toBe('Агуулж буй хавтас нээх');
            expect(chromeText('mn', 'The HubCenter session expired or could not be refreshed. Reconnect this device and try again.', '登录已过期')).toBe('HubCenter сессийн хугацаа дууссан эсвэл шинэчлэгдсэнгүй. Энэ төхөөрөмжийг дахин холбоод оролдоно уу.');
            expect(chromeText('mn', 'Expert: LaTeX', '专家：LaTeX', '專家：LaTeX')).toBe('Эксперт: LaTeX');
            expect(chromeText('en', 'Expert: LaTeX', '专家：LaTeX', '專家：LaTeX')).toBe('Expert: LaTeX');
            expect(chromeText('zh-Hans', 'Expert: LaTeX', '专家：LaTeX', '專家：LaTeX')).toBe('专家：LaTeX');
            expect(localizeText('mn', 'Expert type', '专家类型')).toBe('Экспертийн төрөл');
            expect(localizeText('mn', 'Local', '本地', '本地')).toBe('Локал');
            expect(localizeText('mn', 'Remote', '远程', '遠端')).toBe('Алсын');
            expect(localizeText('mn', 'Local pure coding', '本地纯编程')).toBe('Локал цэвэр код бичих');
            expect(tailText('mn', 'Default — Chat · General expert · No workflow · Local', '默认')).toBe('Анхдагч — Чат · Ерөнхий эксперт · Ажлын урсгалгүй · Локал');
            expect(localizeText('mn', 'Local changes may already exist; review the workspace before continuing.', '本地修改可能已存在；继续前请检查工作区。')).toBe('Локал өөрчлөлт аль хэдийн байж болно. Үргэлжлүүлэхийн өмнө ажлын талбарыг нягтална уу.');
            expect(localizeText('mn', 'Connecting SSH (source preview + local Skill/MCP)…', '正在连接 SSH（源码预览 + 本机 Skill/MCP）…')).toBe('SSH холбож байна (эх кодын урьдчилсан харалт + локал Skill/MCP)…');
            expect(appsMn('mn', 'Local queue')).toBe('Локал дараалал');
            expect(appsMn('mn', 'No local apps ready for publishing yet')).toBe('Нийтлэхэд бэлэн локал апп одоогоор алга');
            expect(appsMn('mn', 'DataSrv is disabled; showing local approval instances only.')).toBe('DataSrv идэвхгүй; зөвхөн локал баталгааны тохиолдлыг харуулж байна.');
            expect(appsMn('en', 'Local queue')).toBe('Local queue');
            expect(appsPublishQueueChannelLabel('mn', 'local')).toBe('Локал');
            expect(appsPublishQueueChannelLabel('mn', 'hub')).toBe('Hub');
            expect(appsPublishQueueChannelLabel('en', 'local')).toBe('local');
            expect(appsPublishQueueChannelLabel('zh-Hans', 'local')).toBe('local');
            expect(appsPublishQueueStatusLabel('mn', 'submitted', 'local')).toBe('Локал синк хүлээгдэж байна');
            expect(appsPublishQueueStatusLabel('mn', 'submitted', 'hub')).toBe('Байгууллагын зах зээлийн хяналтыг хүлээж байна');
            expect(appsPublishQueueStatusLabel('mn', 'pending_review')).toBe('Байгууллагын зах зээлийн хяналтыг хүлээж байна');
            expect(appsPublishQueueStatusLabel('en', 'submitted', 'local')).toBe('submitted');
            expect(appsPublishQueueEventStatusLabel('mn', 'submitted')).toBe('Илгээсэн');
            expect(appsPublishQueueEventStatusLabel('mn', 'pending_review')).toBe('Байгууллагын зах зээлийн хяналтыг хүлээж байна');
            expect(appsPublishQueueEventStatusLabel('en', 'submitted')).toBe('submitted');
            expect(appsPublishQueueEventStatusLabel('zh-Hans', 'submitted')).toBe('submitted');
            expect(appsPublishQueueMessage('mn', 'queued locally for enterprise market sync')).toBe('Байгууллагын зах зээлийн синкт зориулж локал дараалалд оруулсан');
            expect(appsPublishQueueMessage('zh-Hans', 'queued locally for enterprise market sync')).toBe('queued locally for enterprise market sync');
            expect(appsPublishQueueMessage('mn', 'queued locally; enterprise hub pack submitted; skill market upload ok (sm-1)')).toBe('Локал дараалалд оруулсан; Байгууллагын Hub багц илгээгдсэн; SkillMarket байршуулалт амжилттай (sm-1)');
            expect(appsPublishQueueMessage('en', 'queued locally; enterprise hub pack failed: offline; skill market failed: no remote_email')).toBe('queued locally; enterprise hub pack failed: offline; skill market failed: no remote_email');
            expect(appsPublishQueueMessage('mn', 'local queue ready; enterprise hub pack already synced; skill market upload ok (sm-2), stamped 2 dep ref')).toBe('Локал дараалал бэлэн; Байгууллагын Hub багц аль хэдийн синк хийгдсэн; SkillMarket байршуулалт амжилттай (sm-2), 2 хамаарлын лавлагаа тэмдэглэсэн');
            expect(appsPublishQueueMessage('mn', 'queued locally; enterprise hub pack failed: offline; skill market failed: no remote_email')).toBe('Локал дараалалд оруулсан; Байгууллагын Hub багц амжилтгүй: offline; SkillMarket амжилтгүй: no remote_email');
            expect(appsPublishQueueMessage('mn', 'local queue OK; SkillMarket may fail')).toBe('Локал дараалал бэлэн; SkillMarket амжилтгүй байж болно');
            expect(appsPublishQueueMessage('mn', 'queued locally; enterprise hub pack submitted · deps need bundle or skill upload first')).toBe('Локал дараалалд оруулсан; Байгууллагын Hub багц илгээгдсэн · Эхлээд хамаарлыг багцлах эсвэл Skill байршуулах хэрэгтэй');
            expect(appsPublishQueueMessage('mn', 'local queue OK; SkillMarket may fail · re-login SkillMarket / Hub session; partial failed')).toBe('Локал дараалал бэлэн; SkillMarket амжилтгүй байж болно · SkillMarket / Hub сессэд дахин нэвтэрнэ үү; хэсэгчлэн амжилтгүй');
            expect(appsPublishQueueMessage('en', 'queued locally; enterprise hub pack submitted · deps need bundle or skill upload first')).toBe('queued locally; enterprise hub pack submitted · deps need bundle or skill upload first');
            const queueSummary = appsPublishQueueMessage('mn', 'queued locally; enterprise hub pack submitted; skill market upload ok (sm-1)');
            expect(appsPublishQueueMessage('mn', queueSummary)).toBe(queueSummary);
            expect(appsPublishQueueMessage('en', 'local queue OK; SkillMarket may fail')).toBe('local queue OK; SkillMarket may fail');
            expect(appsPublishQueueMessage('mn', 'enterprise Hub approved this MaClaw App')).toBe('Enterprise Hub энэ MaClaw аппыг зөвшөөрлөө');
            expect(appsPreflightCheckText('mn', 'missing_run_evidence')).toBe('Амжилттай гүйлтийн нотолгоо алга. «Засвар» товчоор аппыг нэг удаа бүтэн ажиллуулаад, нийтлэхийн тулд энд буцаж ирнэ үү.');
            expect(appsPreflightCheckText('en', 'hub_tls')).toBe('The Enterprise Hub TLS certificate is unusable. Renew the certificate or correct the server clock, then retry.');
            expect(appsPreflightCheckText('zh-Hans', 'hub_tls')).toBe('Enterprise Hub 的 TLS 证书不可用。请更新证书或校正服务器时间后重试。');
            expect(appsPreflightCheckText('zh-Hant', 'missing_run_evidence')).toContain('缺少一次成功執行證據');
            expect(appsOneClickErrorText('mn', 'next_step')).toBe('Дараагийн алхам');
            expect(appsOneClickErrorText('en', 'next_step')).toBe('Next step');
            expect(appsOneClickErrorText('zh-Hans', 'technical_details')).toBe('技术详情');
            expect(appsOneClickErrorText('zh-Hant', 'technical_details')).toBe('技術詳情');
            expect(appsOneClickErrorText('mn', 'remote_publish_failed')).toBe('Алсын нийтлэл амжилтгүй.');
            expect(appsOneClickErrorText('zh-Hans', 'remote_publish_failed')).toBe('远程发布失败。');
            expect(appsOneClickErrorText('en', 'hub_tls_rejected')).toBe('Enterprise Hub rejected the connection because its TLS certificate is expired or not yet valid.');
            expect(appsReviewIssueText('mn', 'verified required dependency must include install_ref')).toBe('Шалгасан шаардлагатай хамааралд install_ref байх ёстой');
            expect(appsReviewIssueText('en', 'verified required dependency must include install_ref')).toBe('verified required dependency must include install_ref');
            expect(appsReviewIssueText('zh-Hans', 'package not ready')).toBe('package not ready');
            expect(appsReviewIssueText('mn', 'workflow contract must include required output workflow_result')).toBe('Ажлын урсгалын гэрээнд шаардлагатай гаралт орсон байх ёстой: workflow_result');
            expect(appsReviewIssueText('mn', 'approval workflow Skill invoice is installed but not active')).toBe('Баталгааны ажлын урсгалын Skill invoice суусан боловч идэвхгүй');
            expect(appsReviewIssueText('mn', 'Definition changed; run evidence is stale — rerun the app to refresh it')).toBe('Тодорхойлолт өөрчлөгдсөн; гүйлтийн нотолгоо хуучирсан. Шинэчлэхийн тулд аппыг дахин ажиллуулна уу');
            expect(appsReviewIssueText('en', 'Definition changed; run evidence is stale — rerun the app to refresh it')).toBe('Definition changed; run evidence is stale — rerun the app to refresh it');
            expect(appsReviewIssueText('mn', 'not a known review sentence xyz')).toBe('not a known review sentence xyz');
            expect(appsMn('mn', 'Event')).toBe('Үйл явдал');
            expect(appsMn('mn', 'Health')).toBe('Эрүүл байдал');
        });
    });

    describe('t', () => {
        it('looks up keys from translation tables', () => {
            // 'close' exists in both en and zh-Hans tables
            expect(t('close', 'en')).toBe('Close');
            expect(t('close', 'zh-Hans')).toBe('关闭');
        });

        it('falls back to zh-Hans when key missing in target language', () => {
            // Use a key that only exists in zh-Hans
            const zhOnly = t('langName', 'zh-Hans');
            expect(zhOnly).toBe('简体中文');
            // If zh-Hant doesn't have this key, should fall back to zh-Hans
            const hantResult = t('langName', 'zh-Hant');
            expect(typeof hantResult).toBe('string');
            expect(hantResult.length).toBeGreaterThan(0);
        });

        it('returns key itself when not found in any table', () => {
            const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
            expect(t('nonexistent.key.xyz', 'en')).toBe('nonexistent.key.xyz');
            expect(spy).toHaveBeenCalledWith('[i18n] missing key: "nonexistent.key.xyz"');
            spy.mockRestore();
        });

        it('uses English when the language is not Chinese or Mongolian', () => {
            expect(t('close', undefined)).toBe('Close');
            expect(t('close', 'ja')).toBe('Close');
            expect(t('close', 'mn')).toBe('Хаах');
        });

        it('falls back to English when a Mongolian key is missing', () => {
            expect(t('title', 'mn')).not.toBe('码卡龙 8');
        });

        it('handles undefined lang', () => {
            expect(t('close', undefined)).toBe('Close');
            expect(t('close', null)).toBe('Close');
        });
    });
});
