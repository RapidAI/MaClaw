import { describe, expect, it } from 'vitest';
import { localizeText } from './langSelect';
import { tailText } from './mnTail';
import { mnTailPackF } from './mnTailPackF';

describe('mnTailPackF', () => {
    it('shows Mongolian for the filled UI sentences and leaves English and Chinese alone', () => {
        expect(localizeText('mn', 'Select', '选择', '選擇')).toBe('Сонгох');
        expect(localizeText('en', 'Select', '选择', '選擇')).toBe('Select');
        expect(localizeText('zh-Hans', 'Select', '选择', '選擇')).toBe('选择');
        expect(localizeText('zh-Hant', 'Select', '选择', '選擇')).toBe('選擇');
        expect(tailText('mn', 'Add row', '添加行', '新增列')).toBe('Мөр нэмэх');
        expect(localizeText('mn', 'Computer Use', '桌面操控', '桌面操控')).toBe('Компьютерийн удирдлага');
        expect(tailText('mn', 'Computer Use', '桌面操控', '桌面操控')).toBe('Компьютерийн удирдлага');
        expect(localizeText('mn', 'Apply mapping', '应用映射')).toBe('Харгалзуулалтыг хэрэгжүүлэх');
        expect(tailText('mn', 'Apply mapping', '应用映射')).toBe('Харгалзуулалтыг хэрэгжүүлэх');
        expect(localizeText('mn', 'AI expert', 'AI 专家', 'AI 專家')).toBe('AI эксперт');
        expect(localizeText('mn', 'AI experts', 'AI 专家', 'AI 專家')).toBe('AI эксперт');
        expect(localizeText('mn', 'Expert type', '专家类型', '專家類型')).toBe('Экспертийн төрөл');
        expect(tailText('mn', 'Configured bot instances', '已配置机器人实例', '已設定機器人實例')).toBe('Тохируулсан Bot нэгж');
        for (const [en, mn] of Object.entries(mnTailPackF)) {
            expect(mn.length).toBeGreaterThan(0);
            expect(localizeText('mn', en, '中文', '繁體')).toBe(mn);
            expect(localizeText('en', en, '中文', '繁體')).toBe(en);
            expect(localizeText('zh-Hans', en, '中文', '繁體')).toBe('中文');
        }
    });
});
